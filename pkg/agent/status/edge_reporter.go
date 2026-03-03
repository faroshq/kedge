/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package status reports agent status back to the hub.
package status

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kedgev1alpha1 "github.com/faroshq/faros-kedge/apis/kedge/v1alpha1"
	kedgeclient "github.com/faroshq/faros-kedge/pkg/client"
)

// sshdHostKeyPaths is the ordered list of sshd host public key files to try.
var sshdHostKeyPaths = []string{
	"/etc/ssh/ssh_host_ed25519_key.pub",
	"/etc/ssh/ssh_host_ecdsa_key.pub",
	"/etc/ssh/ssh_host_rsa_key.pub",
}

// readSSHHostKey attempts to read the sshd host public key from well-known paths.
// It returns the key in authorized_keys format (without the trailing comment field),
// or an empty string if no key file could be read.
func readSSHHostKey(logger klog.Logger) string {
	for _, path := range sshdHostKeyPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// authorized_keys line: "<type> <base64> [comment]"
		// Strip the comment (optional third field) to normalise the stored value.
		line := strings.TrimSpace(string(data))
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			key := fields[0] + " " + fields[1]
			logger.V(4).Info("Read sshd host public key", "path", path, "keyType", fields[0])
			return key
		}
	}
	logger.V(4).Info("No sshd host public key found in well-known paths")
	return ""
}

const (
	// HeartbeatInterval is how often the agent sends heartbeats to the hub.
	HeartbeatInterval = 30 * time.Second
)

// EdgeReporter sends heartbeats for an Edge resource.
// It works for both EdgeTypeKubernetes and EdgeTypeServer.
type EdgeReporter struct {
	edgeName        string
	hubClient       *kedgeclient.Client
	tunnelState     <-chan bool // receives true on connect, false on disconnect; may be nil
	tunnelConnected bool
}

// NewEdgeReporter creates a new EdgeReporter.
// tunnelState is the channel produced by tunnel.StartProxyTunnel; pass nil to
// skip tunnel-state tracking (tunnelConnected will always report false).
func NewEdgeReporter(edgeName string, hubClient *kedgeclient.Client, tunnelState <-chan bool) *EdgeReporter {
	return &EdgeReporter{
		edgeName:    edgeName,
		hubClient:   hubClient,
		tunnelState: tunnelState,
	}
}

// Run starts the edge heartbeat reporter and blocks until ctx is cancelled.
func (r *EdgeReporter) Run(ctx context.Context) error {
	logger := klog.FromContext(ctx).WithName("edge-status-reporter")
	logger.Info("Starting edge status reporter", "edgeName", r.edgeName)

	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	// First heartbeat immediately.
	r.sendHeartbeat(ctx, logger)

	for {
		select {
		case <-ctx.Done():
			return nil
		case connected, ok := <-r.tunnelState:
			if ok {
				r.tunnelConnected = connected
				r.sendHeartbeat(ctx, logger)
			}
		case <-ticker.C:
			r.sendHeartbeat(ctx, logger)
		}
	}
}

func (r *EdgeReporter) sendHeartbeat(ctx context.Context, logger klog.Logger) {
	status := kedgev1alpha1.EdgeStatus{
		Phase:     kedgev1alpha1.EdgePhaseReady,
		Connected: r.tunnelConnected,
	}
	// The hub may set Hostname/WorkspaceURL; we only patch the fields we own.
	statusPatch := map[string]interface{}{
		"phase":     string(status.Phase),
		"connected": status.Connected,
	}

	// Report the sshd host public key so the hub can verify the agent's identity.
	if hostKey := readSSHHostKey(logger); hostKey != "" {
		statusPatch["sshHostKey"] = hostKey
	}

	patch := map[string]interface{}{
		"status": statusPatch,
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		logger.Error(err, "failed to marshal edge status patch")
		return
	}

	_, err = r.hubClient.Edges().Patch(ctx, r.edgeName,
		types.MergePatchType, patchBytes,
		metav1.PatchOptions{}, "status")
	if err != nil {
		logger.Error(err, "failed to update edge status", "edge", r.edgeName)
		return
	}

	logger.V(4).Info("Edge heartbeat sent", "edge", r.edgeName,
		"phase", string(status.Phase), "connected", status.Connected)
}
