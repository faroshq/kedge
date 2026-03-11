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

package cases

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/faroshq/faros-kedge/test/e2e/framework"
)

// mcpAgentKey is the context key for the Agent started in MCP tests.
type mcpAgentKey struct{}

// mcpClientKey is the context key for passing the mcpClient between Assess steps.
type mcpClientKey struct{}

// mcpClient encapsulates the HTTP boilerplate for the MCP streamable-HTTP protocol.
type mcpClient struct {
	baseURL    string // e.g. https://172.18.0.2:31443/services/mcp/<cluster>/mcp
	token      string // Bearer token
	sessionID  string // set after initialize
	httpClient *http.Client
}

// newMCPClient creates an mcpClient using the NodePort URL of the hub and the
// kcp cluster name derived from the hub kubeconfig.
func newMCPClient(hubKubeconfig, edgeName string) (*mcpClient, error) {
	// Resolve the NodePort base URL (reachable in CI via Docker network).
	nodePortBase := framework.HubNodePortURL()
	if nodePortBase == "" {
		return nil, fmt.Errorf("could not determine hub NodePort URL (docker inspect failed)")
	}

	// Extract the kcp cluster name from the hub kubeconfig server URL.
	clusterName, err := clusterNameFromKubeconfig(hubKubeconfig)
	if err != nil {
		return nil, fmt.Errorf("resolving cluster name from kubeconfig: %w", err)
	}

	// Extract the bearer token from the hub kubeconfig.
	restCfg, err := clientcmd.BuildConfigFromFlags("", hubKubeconfig)
	if err != nil {
		return nil, fmt.Errorf("building rest config from kubeconfig: %w", err)
	}
	token := restCfg.BearerToken

	// New per-edge MCP URL pattern:
	// /services/agent-proxy/{cluster}/apis/kedge.faros.sh/v1alpha1/edges/{edgeName}/mcp
	mcpURL := fmt.Sprintf("%s/services/agent-proxy/%s/apis/kedge.faros.sh/v1alpha1/edges/%s/mcp",
		nodePortBase, clusterName, edgeName)

	return &mcpClient{
		baseURL: mcpURL,
		token:   token,
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // e2e dev certs
			},
			Timeout: 30 * time.Second,
		},
	}, nil
}

// do sends a single JSON-RPC request to the MCP endpoint and returns the
// decoded response map.  If sessionID is set it is attached as Mcp-Session-Id.
// Pass id <= 0 for notifications (no id field).
func (c *mcpClient) do(ctx context.Context, method string, id int, params any) (map[string]any, error) {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if id > 0 {
		payload["id"] = id
	}
	if params != nil {
		payload["params"] = params
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	// Use application/json only — omitting text/event-stream forces the MCP
	// server to return plain JSON instead of SSE-wrapped responses, which is
	// what our mcpClient parser expects.
	req.Header.Set("Accept", "application/json")
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	// Notifications (id==0) may return 200 or 202 with no body.
	if id <= 0 {
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			return nil, fmt.Errorf("notification %q returned HTTP %d (body: %s)", method, resp.StatusCode, respBody)
		}
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("method %q returned HTTP %d (body: %s)", method, resp.StatusCode, respBody)
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decoding JSON response for %q: %w (body: %s)", method, err, respBody)
	}

	// Capture the session ID from the initialize response.
	if method == "initialize" {
		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			c.sessionID = sid
		}
	}

	return result, nil
}

// initialize performs the MCP initialize handshake and sets c.sessionID.
func (c *mcpClient) initialize(ctx context.Context) error {
	resp, err := c.do(ctx, "initialize", 1, map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "e2e-test",
			"version": "1.0",
		},
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	// Validate server responded with serverInfo.
	respJSON, _ := json.Marshal(resp)
	if !strings.Contains(string(respJSON), "serverInfo") {
		return fmt.Errorf("initialize response missing 'serverInfo': %s", respJSON)
	}

	// Send required notifications/initialized notification.
	if _, err := c.do(ctx, "notifications/initialized", 0, nil); err != nil {
		return fmt.Errorf("notifications/initialized: %w", err)
	}
	return nil
}

// toolsList calls tools/list and returns the list of tool names.
func (c *mcpClient) toolsList(ctx context.Context) ([]string, error) {
	resp, err := c.do(ctx, "tools/list", 2, map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	respJSON, _ := json.Marshal(resp)
	if !strings.Contains(string(respJSON), "tools") {
		return nil, fmt.Errorf("tools/list response missing 'tools': %s", respJSON)
	}

	// Navigate result.tools[].name
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected tools/list response shape (no result map): %s", respJSON)
	}
	toolsRaw, ok := result["tools"].([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected tools/list response shape (tools not array): %s", respJSON)
	}

	names := make([]string, 0, len(toolsRaw))
	for _, t := range toolsRaw {
		if toolMap, ok := t.(map[string]any); ok {
			if name, ok := toolMap["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names, nil
}

// toolsCall calls tools/call and returns the text content from the result.
func (c *mcpClient) toolsCall(ctx context.Context, toolName string, args map[string]any) (string, error) {
	resp, err := c.do(ctx, "tools/call", 3, map[string]any{
		"name":      toolName,
		"arguments": args,
	})
	if err != nil {
		return "", fmt.Errorf("tools/call %q: %w", toolName, err)
	}

	respJSON, _ := json.Marshal(resp)

	// Navigate result.content[].text
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("tools/call %q: unexpected response shape (no result map): %s", toolName, respJSON)
	}
	contentRaw, ok := result["content"].([]any)
	if !ok {
		return "", fmt.Errorf("tools/call %q: unexpected response shape (content not array): %s", toolName, respJSON)
	}

	var sb strings.Builder
	for _, c := range contentRaw {
		if item, ok := c.(map[string]any); ok {
			if text, ok := item["text"].(string); ok {
				sb.WriteString(text)
			}
		}
	}
	return sb.String(), nil
}

// MCPEndpoint verifies the per-tenant MCP endpoint with a full protocol flow:
// initialize → tools/list → tools/call namespaces_list → tools/call pods_list_in_namespace.
func MCPEndpoint() features.Feature {
	const edgeName = "e2e-mcp-edge"

	return features.New("MCP/Endpoint").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			if clusterEnv == nil {
				t.Fatal("cluster environment not found in context")
			}
			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)

			if err := client.Login(ctx, framework.DevToken); err != nil {
				t.Fatalf("login failed: %v", err)
			}
			if err := client.EdgeCreate(ctx, edgeName, "kubernetes", "env=e2e"); err != nil {
				t.Fatalf("edge create failed: %v", err)
			}

			edgeKubeconfigPath := filepath.Join(clusterEnv.WorkDir, "edge-"+edgeName+".kubeconfig")
			if err := client.ExtractEdgeKubeconfig(ctx, edgeName, edgeKubeconfigPath); err != nil {
				t.Fatalf("failed to extract edge kubeconfig: %v", err)
			}

			agent := framework.NewAgent(framework.RepoRoot(), edgeKubeconfigPath, clusterEnv.AgentKubeconfig, edgeName)
			if err := agent.Start(ctx); err != nil {
				t.Fatalf("failed to start agent: %v", err)
			}
			return context.WithValue(ctx, mcpAgentKey{}, agent)
		}).
		Assess("edge becomes Ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)

			if err := client.WaitForEdgeReady(ctx, edgeName, 3*time.Minute); err != nil {
				t.Fatalf("edge %q did not become Ready: %v", edgeName, err)
			}
			return ctx
		}).
		Assess("MCP initialize returns 200 with serverInfo", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)

			mcp, err := newMCPClient(clusterEnv.HubKubeconfig, edgeName)
			if err != nil {
				t.Fatalf("creating MCP client: %v", err)
			}
			t.Logf("MCP URL: %s", mcp.baseURL)

			if err := mcp.initialize(ctx); err != nil {
				t.Fatalf("MCP initialize failed: %v", err)
			}
			t.Logf("MCP session ID: %s", mcp.sessionID)

			// Store the initialised client for subsequent Assess steps.
			return context.WithValue(ctx, mcpClientKey{}, mcp)
		}).
		Assess("MCP tools/list returns namespaces_list and pods_list_in_namespace", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			mcp, ok := ctx.Value(mcpClientKey{}).(*mcpClient)
			if !ok || mcp == nil {
				t.Fatal("mcpClient not found in context — initialize step may have failed")
			}

			names, err := mcp.toolsList(ctx)
			if err != nil {
				t.Fatalf("MCP tools/list failed: %v", err)
			}
			t.Logf("MCP tools: %v", names)

			nameSet := make(map[string]bool, len(names))
			for _, n := range names {
				nameSet[n] = true
			}
			for _, required := range []string{"namespaces_list", "pods_list_in_namespace"} {
				if !nameSet[required] {
					t.Errorf("expected tool %q in tools/list, got: %v", required, names)
				}
			}
			return ctx
		}).
		Assess("MCP tools/call namespaces_list contains kube-system", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			mcp, ok := ctx.Value(mcpClientKey{}).(*mcpClient)
			if !ok || mcp == nil {
				t.Fatal("mcpClient not found in context — initialize step may have failed")
			}

			result, err := mcp.toolsCall(ctx, "namespaces_list", map[string]any{
				"edge": edgeName,
			})
			if err != nil {
				t.Fatalf("tools/call namespaces_list failed: %v", err)
			}
			t.Logf("namespaces_list result: %s", result)

			if !strings.Contains(result, "kube-system") {
				t.Errorf("expected namespaces_list to contain 'kube-system', got: %s", result)
			}
			return ctx
		}).
		Assess("MCP tools/call pods_list_in_namespace kube-system returns pods", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			mcp, ok := ctx.Value(mcpClientKey{}).(*mcpClient)
			if !ok || mcp == nil {
				t.Fatal("mcpClient not found in context — initialize step may have failed")
			}

			result, err := mcp.toolsCall(ctx, "pods_list_in_namespace", map[string]any{
				"namespace": "kube-system",
				"edge":      edgeName,
			})
			if err != nil {
				t.Fatalf("tools/call pods_list_in_namespace failed: %v", err)
			}
			t.Logf("pods_list_in_namespace result: %s", result)

			if strings.TrimSpace(result) == "" {
				t.Error("expected pods_list_in_namespace to return non-empty content for kube-system")
			}
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			if a, ok := ctx.Value(mcpAgentKey{}).(*framework.Agent); ok {
				a.Stop()
			}
			clusterEnv := framework.ClusterEnvFrom(ctx)
			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)
			_ = client.EdgeDelete(ctx, edgeName)
			return ctx
		}).
		Feature()
}

// MCPURL verifies that `kedge mcp url` prints a valid per-edge MCP endpoint URL.
func MCPURL() features.Feature {
	const edgeName = "e2e-mcp-edge"

	return features.New("MCP/URL").
		Assess("kedge mcp url --edge prints expected URL", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			if clusterEnv == nil {
				t.Fatal("cluster environment not found in context")
			}

			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)

			out, err := client.Run(ctx, "mcp", "url", "--edge", edgeName)
			if err != nil {
				t.Fatalf("kedge mcp url failed: %v (output: %s)", err, out)
			}
			out = strings.TrimSpace(out)
			t.Logf("kedge mcp url output: %s", out)

			// The output must be a valid per-edge MCP URL.
			if !strings.HasPrefix(out, "https://") {
				t.Errorf("expected URL to start with https://, got: %s", out)
			}
			if !strings.Contains(out, "/services/agent-proxy/") {
				t.Errorf("expected URL to contain /services/agent-proxy/, got: %s", out)
			}
			if !strings.Contains(out, "/edges/"+edgeName+"/mcp") {
				t.Errorf("expected URL to contain /edges/%s/mcp, got: %s", edgeName, out)
			}
			return ctx
		}).
		Feature()
}

// clusterNameFromKubeconfig extracts the kcp cluster name from the server URL
// in the current context of the given kubeconfig file.
// Returns the path segment after /clusters/ in the server URL.
func clusterNameFromKubeconfig(kubeconfigPath string) (string, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	)

	rawCfg, err := clientCfg.RawConfig()
	if err != nil {
		return "", fmt.Errorf("loading kubeconfig %q: %w", kubeconfigPath, err)
	}

	currentCtx := rawCfg.CurrentContext
	if currentCtx == "" {
		return "", fmt.Errorf("no current context in kubeconfig %q", kubeconfigPath)
	}

	ctxObj, ok := rawCfg.Contexts[currentCtx]
	if !ok {
		return "", fmt.Errorf("context %q not found in kubeconfig", currentCtx)
	}

	clusterObj, ok := rawCfg.Clusters[ctxObj.Cluster]
	if !ok {
		return "", fmt.Errorf("cluster %q not found in kubeconfig", ctxObj.Cluster)
	}

	serverURL := clusterObj.Server
	idx := strings.Index(serverURL, "/clusters/")
	if idx < 0 {
		return "", fmt.Errorf("server URL %q does not contain /clusters/ segment", serverURL)
	}
	clusterName := strings.TrimSuffix(strings.TrimPrefix(serverURL[idx:], "/clusters/"), "/")
	if clusterName == "" {
		return "", fmt.Errorf("empty cluster name in server URL %q", serverURL)
	}
	return clusterName, nil
}
