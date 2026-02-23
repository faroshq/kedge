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

package framework

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"k8s.io/client-go/tools/clientcmd"
)

// sshWsMsg mirrors the wsMsg type in pkg/util/ssh.
type sshWsMsg struct {
	Type string `json:"type"`
	Cmd  string `json:"cmd,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// SSHWebSocketClient is a programmatic WebSocket SSH client for use in e2e tests.
// It connects directly to the hub SSH subresource endpoint, bypassing the CLI,
// so that the interactive PTY path can be exercised without a real terminal.
type SSHWebSocketClient struct {
	conn *websocket.Conn
}

// DialSSH connects to the hub SSH WebSocket endpoint for the given server/site name.
// kubeconfig is used to extract the hub URL and bearer token.
func DialSSH(ctx context.Context, kubeconfig, name string) (*SSHWebSocketClient, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig: %w", err)
	}

	// Build WebSocket URL: https → wss, http → ws
	base := strings.TrimRight(cfg.Host, "/")
	wsURL := base
	switch {
	case strings.HasPrefix(base, "https://"):
		wsURL = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		wsURL = "ws://" + strings.TrimPrefix(base, "http://")
	}
	wsURL += fmt.Sprintf("/proxy/apis/kedge.faros.sh/v1alpha1/sites/%s/ssh", name)

	headers := http.Header{}
	if cfg.BearerToken != "" {
		headers.Set("Authorization", "Bearer "+cfg.BearerToken)
	}

	dialer := &websocket.Dialer{
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return nil, fmt.Errorf("dialling SSH WebSocket %s: %w", wsURL, err)
	}

	return &SSHWebSocketClient{conn: conn}, nil
}

// SendResize sends a terminal resize message.
func (c *SSHWebSocketClient) SendResize(cols, rows int) error {
	return c.sendMsg(sshWsMsg{Type: "resize", Cols: cols, Rows: rows})
}

// SendInput sends raw bytes as a cmd message (base64-encoded).
func (c *SSHWebSocketClient) SendInput(data []byte) error {
	return c.sendMsg(sshWsMsg{
		Type: "cmd",
		Cmd:  base64.StdEncoding.EncodeToString(data),
	})
}

// CollectOutput reads WebSocket messages for up to timeout and returns all
// output concatenated as a string.
func (c *SSHWebSocketClient) CollectOutput(ctx context.Context, timeout time.Duration) string {
	var sb strings.Builder
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		if err := c.conn.SetReadDeadline(time.Now().Add(remaining)); err != nil {
			break
		}
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		sb.Write(data)
	}

	// Reset deadline
	_ = c.conn.SetReadDeadline(time.Time{})
	return sb.String()
}

// Close closes the WebSocket connection.
func (c *SSHWebSocketClient) Close() error {
	return c.conn.Close()
}

func (c *SSHWebSocketClient) sendMsg(msg sshWsMsg) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, b)
}
