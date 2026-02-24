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

package builder

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/gorilla/websocket"
	gossh "golang.org/x/crypto/ssh"

	"k8s.io/klog/v2"

	utilssh "github.com/faroshq/faros-kedge/pkg/util/ssh"
)

// buildAgentProxyHandler creates the HTTP handler for accessing agent resources.
// Subresources: k8s (kubectl proxy), ssh (web terminal), exec, logs.
//
// Authentication: requires a valid bearer token (OIDC or SA). When kcp is
// configured and an SA token is provided, the token is verified against kcp
// by checking access to the site resource.
func (p *virtualWorkspaces) buildAgentProxyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Authenticate: require a valid bearer token.
		token := extractBearerToken(r)
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Parse path: /services/agent-proxy/{cluster}/apis/kedge.faros.sh/v1alpha1/sites/{name}/{subresource}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")

		if len(parts) < 2 {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		// Find subresource from path
		var clusterName, siteName, subresource string
		for i, part := range parts {
			if part == "sites" && i+2 < len(parts) {
				siteName = parts[i+1]
				subresource = parts[i+2]
				break
			}
		}

		// Get cluster from query or path
		clusterName = r.URL.Query().Get("cluster")
		if clusterName == "" {
			clusterName = "default"
		}

		if siteName == "" {
			http.Error(w, "site name required", http.StatusBadRequest)
			return
		}

		// 2. Delegated authorization: TokenReview + SubjectAccessReview via kcp.
		// Checks that the caller can "proxy" the site (same verb as faros-core agent proxy).
		if p.kcpConfig != nil {
			if claims, ok := parseServiceAccountToken(token); ok {
				if err := authorize(r.Context(), p.kcpConfig, token, claims.ClusterName, "proxy", "sites", siteName); err != nil {
					p.logger.Error(err, "agent proxy authorization failed", "site", siteName)
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			}
			// OIDC tokens pass through — delegated authorization for OIDC
			// users accessing agent resources will be added in a future iteration.
		}

		key := p.getKey(clusterName, siteName)

		switch subresource {
		case "k8s":
			p.k8sHandler(r.Context(), w, r, key)
		case "ssh":
			p.sshHandler(r.Context(), w, r, key)
		case "exec":
			p.k8sHandler(r.Context(), w, r, key)
		case "logs":
			p.k8sHandler(r.Context(), w, r, key)
		default:
			http.Error(w, fmt.Sprintf("unknown subresource: %s", subresource), http.StatusNotFound)
		}
	})
}

// k8sHandler reverse-proxies HTTP to the agent's local K8s API handler.
func (p *virtualWorkspaces) k8sHandler(ctx context.Context, w http.ResponseWriter, r *http.Request, key string) {
	logger := klog.FromContext(ctx)

	deviceConn, err := p.connManager.Dial(ctx, key)
	if err != nil {
		logger.Error(err, "failed to dial agent", "key", key)
		http.Error(w, "failed to connect to agent", http.StatusBadGateway)
		return
	}

	// Check if this is an upgrade request (exec, port-forward)
	if isUpgradeRequest(r) {
		p.handleK8sUpgrade(ctx, w, r, deviceConn)
		return
	}

	// Reverse proxy to agent
	transport := &deviceConnTransport{conn: deviceConn}
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "agent"
			// Strip the prefix path, forward just the k8s API path
			req.URL.Path = extractK8sPath(r.URL.Path)
		},
		Transport: transport,
	}
	proxy.ServeHTTP(w, r)
}

// sshHandler establishes an SSH session over WebSocket to the agent.
// The SSH username is derived from the caller's OIDC identity (email local-part,
// then sub), falling back to "root" for service-account or anonymous callers.
func (p *virtualWorkspaces) sshHandler(ctx context.Context, w http.ResponseWriter, r *http.Request, key string) {
	logger := klog.FromContext(ctx)

	// Derive SSH username from the bearer token before dialling the agent.
	token := extractBearerToken(r)
	sshUser := sshUsernameFromToken(token)

	// If a remote command was provided via query param, use SSH exec (no PTY).
	// This is the path for `kedge ssh <name> -- <cmd>`.
	remoteCmd := r.URL.Query().Get("cmd")

	logger.V(4).Info("SSH handler", "key", key, "sshUser", sshUser, "exec", remoteCmd != "")

	deviceConn, err := p.connManager.Dial(ctx, key)
	if err != nil {
		logger.Error(err, "failed to dial agent for SSH", "key", key)
		http.Error(w, "failed to connect to agent", http.StatusBadGateway)
		return
	}

	// Open the SSH tunnel: send HTTP upgrade request to the agent's /ssh handler,
	// receive 101 Switching Protocols, and return a raw pipe to the agent's sshd.
	sshConn, err := openAgentSSHTunnel(ctx, deviceConn)
	if err != nil {
		logger.Error(err, "failed to open SSH tunnel to agent", "key", key)
		http.Error(w, "failed to open SSH tunnel", http.StatusBadGateway)
		return
	}

	// Upgrade client connection to WebSocket
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(err, "failed to upgrade to WebSocket")
		return
	}
	defer wsConn.Close() //nolint:errcheck

	// Create SSH client over the tunnelled raw connection.
	sshClient, err := newSSHClient(ctx, sshConn, sshUser, logger)
	if err != nil {
		logger.Error(err, "failed to create SSH client")
		return
	}
	defer sshClient.Close() //nolint:errcheck

	if remoteCmd != "" {
		// Non-interactive exec mode: run the command, stream output, close.
		p.sshExec(ctx, wsConn, sshClient, remoteCmd, logger)
		return
	}

	// Interactive mode: PTY + shell session over WebSocket.
	session, err := utilssh.NewSocketSSHSession(logger, 120, 40, sshClient, wsConn)
	if err != nil {
		logger.Error(err, "failed to create SSH session")
		return
	}
	defer session.Close()

	if err := session.Run(ctx); err != nil {
		logger.Error(err, "SSH session error")
	}
}

// sshExec runs remoteCmd on the SSH client via a non-interactive exec channel
// and streams the combined stdout+stderr output as binary WebSocket messages.
// It closes the WebSocket when the command finishes (or on error).
func (p *virtualWorkspaces) sshExec(ctx context.Context, wsConn *websocket.Conn, sshClient *gossh.Client, remoteCmd string, logger klog.Logger) {
	sshSession, err := sshClient.NewSession()
	if err != nil {
		logger.Error(err, "failed to create SSH exec session")
		return
	}
	defer sshSession.Close() //nolint:errcheck

	// Pipe stdout+stderr to a goroutine that forwards chunks to the WebSocket.
	pr, pw := io.Pipe()
	sshSession.Stdout = pw
	sshSession.Stderr = pw

	// Forward pipe → WebSocket in the background.
	fwdDone := make(chan struct{})
	go func() {
		defer close(fwdDone)
		buf := make([]byte, 4096)
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				if werr := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					logger.V(4).Info("WebSocket write error during exec", "err", werr)
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Run the remote command (blocks until it exits).
	if err := sshSession.Run(remoteCmd); err != nil {
		logger.V(4).Info("SSH exec command finished", "cmd", remoteCmd, "err", err)
	}

	// Close the write end of the pipe so the forwarder goroutine sees EOF.
	pw.Close() //nolint:errcheck
	<-fwdDone  // wait for all output to be forwarded before closing the WebSocket
}

// handleK8sUpgrade handles upgrade requests (exec, port-forward) by hijacking
// the connection and doing bidirectional copy.
func (p *virtualWorkspaces) handleK8sUpgrade(ctx context.Context, w http.ResponseWriter, r *http.Request, deviceConn net.Conn) {
	logger := klog.FromContext(ctx)

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		logger.Error(err, "failed to hijack connection")
		return
	}
	defer clientConn.Close() //nolint:errcheck
	defer deviceConn.Close() //nolint:errcheck

	// Write the original request to the device connection
	if err := r.Write(deviceConn); err != nil {
		logger.Error(err, "failed to write request to agent")
		return
	}

	// Bidirectional copy
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(deviceConn, clientConn)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(clientConn, deviceConn)
		errc <- err
	}()

	// Wait for one side to finish
	<-errc
}

// deviceConnTransport implements http.RoundTripper using a device connection.
type deviceConnTransport struct {
	conn net.Conn
}

func (t *deviceConnTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Write(t.conn); err != nil {
		return nil, err
	}
	return http.ReadResponse(bufio.NewReader(t.conn), req)
}

// openAgentSSHTunnel sends an HTTP upgrade request to the agent's /ssh endpoint
// and returns a net.Conn providing raw TCP access to the agent's sshd.
//
// Protocol:
//
//  1. Hub sends:   GET /ssh HTTP/1.1\r\nUpgrade: ssh-tunnel\r\n...
//  2. Agent sends: HTTP/1.1 101 Switching Protocols\r\n...
//  3. Both sides switch to raw SSH byte stream.
//
// A bufferedConn is returned so that any bytes the bufio.Reader buffered past
// the 101 response headers (e.g. the SSH banner) are not lost.
func openAgentSSHTunnel(ctx context.Context, conn net.Conn) (net.Conn, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent/ssh", nil)
	if err != nil {
		return nil, fmt.Errorf("building SSH tunnel request: %w", err)
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "ssh-tunnel")

	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("writing SSH tunnel request: %w", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		return nil, fmt.Errorf("reading SSH tunnel response: %w", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("expected 101 Switching Protocols from agent, got %d", resp.StatusCode)
	}

	// Wrap conn so that bytes already buffered by the bufio.Reader (e.g. the
	// SSH banner that may have arrived before we finished reading the headers)
	// are not lost.
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

// bufferedConn wraps a net.Conn with a bufio.Reader so that bytes pre-buffered
// during HTTP response parsing are available via Read before the underlying
// connection is used directly.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (bc *bufferedConn) Read(b []byte) (int, error) {
	return bc.reader.Read(b)
}

// newSSHClient creates an SSH client through a device connection.
// sshUser is the Unix username to authenticate as on the remote host.
func newSSHClient(_ context.Context, deviceConn net.Conn, sshUser string, _ klog.Logger) (*gossh.Client, error) {
	sshConfig := &gossh.ClientConfig{
		User: sshUser,
		// Password("") allows connection to sshd configured with PermitEmptyPasswords.
		// TODO(#54): replace with key-based auth loaded from a Secret on the Server resource.
		Auth:            []gossh.AuthMethod{gossh.Password("")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec
	}

	sshConn, chans, reqs, err := gossh.NewClientConn(deviceConn, "agent:22", sshConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH client connection: %w", err)
	}

	return gossh.NewClient(sshConn, chans, reqs), nil
}

// isUpgradeRequest checks if the request is a protocol upgrade.
func isUpgradeRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Connection"), "Upgrade")
}

// extractK8sPath extracts the Kubernetes API path from the full URL path.
func extractK8sPath(path string) string {
	idx := strings.Index(path, "/k8s/")
	if idx >= 0 {
		return path[idx+4:]
	}
	return path
}
