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

package status

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	kedgev1alpha1 "github.com/faroshq/faros-kedge/apis/kedge/v1alpha1"
	kedgeclient "github.com/faroshq/faros-kedge/pkg/client"
)

// ServerReporter sends heartbeats for a Server resource (non-k8s mode).
// It marks the server as Ready once the tunnel is connected.
type ServerReporter struct {
	serverName      string
	hubClient       *kedgeclient.Client
	tunnelState     <-chan bool // receives true when tunnel connects, false when it drops
	tunnelConnected bool
}

// NewServerReporter creates a new ServerReporter.
// tunnelState is the channel produced by tunnel.StartProxyTunnel; pass nil to
// skip tunnel-state tracking (tunnelConnected will always report false).
func NewServerReporter(serverName string, hubClient *kedgeclient.Client, tunnelState <-chan bool) *ServerReporter {
	return &ServerReporter{
		serverName:  serverName,
		hubClient:   hubClient,
		tunnelState: tunnelState,
	}
}

// Run starts the server heartbeat reporter.
func (r *ServerReporter) Run(ctx context.Context) error {
	logger := klog.FromContext(ctx).WithName("server-status-reporter")
	logger.Info("Starting server status reporter", "serverName", r.serverName)

	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	// First heartbeat immediately.
	r.sendServerHeartbeat(ctx, logger)

	for {
		select {
		case <-ctx.Done():
			return nil
		case connected, ok := <-r.tunnelState:
			if ok {
				r.tunnelConnected = connected
				r.sendServerHeartbeat(ctx, logger)
			}
		case <-ticker.C:
			r.sendServerHeartbeat(ctx, logger)
		}
	}
}

func (r *ServerReporter) sendServerHeartbeat(ctx context.Context, logger klog.Logger) {
	now := metav1.Now()
	status := kedgev1alpha1.ServerStatus{
		Phase:             kedgev1alpha1.ServerPhaseReady,
		TunnelConnected:   r.tunnelConnected,
		SSHEnabled:        r.tunnelConnected,
		LastHeartbeatTime: &now,
	}

	patch := map[string]interface{}{
		"status": status,
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		logger.Error(err, "failed to marshal server status patch")
		return
	}

	_, err = r.hubClient.Servers().Patch(ctx, r.serverName,
		types.MergePatchType, patchBytes,
		metav1.PatchOptions{}, "status")
	if err != nil {
		logger.Error(err, "failed to update server status", "server", r.serverName)
		return
	}

	logger.V(4).Info("Server heartbeat sent", "server", r.serverName,
		fmt.Sprintf("phase=%s sshEnabled=true", kedgev1alpha1.ServerPhaseReady))
}
