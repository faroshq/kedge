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

// MCPEndpoint verifies that the per-tenant MCP endpoint responds to a valid
// initialize request and returns server information.
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

			// Resolve the kcp cluster name from the hub kubeconfig.
			clusterName, err := clusterNameFromKubeconfig(clusterEnv.HubKubeconfig)
			if err != nil {
				t.Fatalf("resolving cluster name: %v", err)
			}

			mcpURL := fmt.Sprintf("%s/services/mcp/%s/mcp", clusterEnv.HubURL, clusterName)
			t.Logf("MCP URL: %s", mcpURL)

			// Build an MCP initialize request payload.
			initPayload := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "initialize",
				"params": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]interface{}{},
					"clientInfo": map[string]interface{}{
						"name":    "e2e-test",
						"version": "1.0",
					},
				},
			}
			body, err := json.Marshal(initPayload)
			if err != nil {
				t.Fatalf("marshaling MCP payload: %v", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(body))
			if err != nil {
				t.Fatalf("creating HTTP request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+clusterEnv.Token)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")

			httpClient := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // e2e dev certs
				},
				Timeout: 30 * time.Second,
			}

			resp, err := httpClient.Do(req)
			if err != nil {
				t.Fatalf("MCP initialize request failed: %v", err)
			}
			defer resp.Body.Close() //nolint:errcheck

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("reading MCP response body: %v", err)
			}
			t.Logf("MCP initialize response status: %d", resp.StatusCode)
			t.Logf("MCP initialize response body: %s", string(respBody))

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected HTTP 200 from MCP endpoint, got %d (body: %s)", resp.StatusCode, string(respBody))
			}

			ct := resp.Header.Get("Content-Type")
			if !strings.Contains(ct, "application/json") {
				t.Errorf("expected Content-Type to contain application/json, got %q", ct)
			}

			// The response body must contain "result" and "serverInfo".
			bodyStr := string(respBody)
			if !strings.Contains(bodyStr, "result") {
				t.Errorf("expected MCP response to contain 'result', got: %s", bodyStr)
			}
			if !strings.Contains(bodyStr, "serverInfo") {
				t.Errorf("expected MCP response to contain 'serverInfo', got: %s", bodyStr)
			}
			return ctx
		}).
		Assess("MCP tools/list returns tools", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)

			clusterName, err := clusterNameFromKubeconfig(clusterEnv.HubKubeconfig)
			if err != nil {
				t.Fatalf("resolving cluster name: %v", err)
			}

			mcpURL := fmt.Sprintf("%s/services/mcp/%s/mcp", clusterEnv.HubURL, clusterName)

			listPayload := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      2,
				"method":  "tools/list",
				"params":  map[string]interface{}{},
			}
			body, err := json.Marshal(listPayload)
			if err != nil {
				t.Fatalf("marshaling tools/list payload: %v", err)
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(body))
			if err != nil {
				t.Fatalf("creating tools/list request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+clusterEnv.Token)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")

			httpClient := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // e2e dev certs
				},
				Timeout: 30 * time.Second,
			}

			resp, err := httpClient.Do(req)
			if err != nil {
				t.Fatalf("tools/list request failed: %v", err)
			}
			defer resp.Body.Close() //nolint:errcheck

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("reading tools/list response body: %v", err)
			}
			t.Logf("tools/list response status: %d, body: %s", resp.StatusCode, string(respBody))

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected HTTP 200 from tools/list, got %d", resp.StatusCode)
			}

			bodyStr := string(respBody)
			if !strings.Contains(bodyStr, "tools") {
				t.Errorf("expected tools/list response to contain 'tools', got: %s", bodyStr)
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

// MCPURL verifies that `kedge mcp url` prints a valid MCP endpoint URL.
func MCPURL() features.Feature {
	return features.New("MCP/URL").
		Assess("kedge mcp url prints expected URL", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			if clusterEnv == nil {
				t.Fatal("cluster environment not found in context")
			}

			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)

			out, err := client.Run(ctx, "mcp", "url")
			if err != nil {
				t.Fatalf("kedge mcp url failed: %v (output: %s)", err, out)
			}
			out = strings.TrimSpace(out)
			t.Logf("kedge mcp url output: %s", out)

			// The output must be a URL ending with /mcp.
			if !strings.HasPrefix(out, "https://") {
				t.Errorf("expected URL to start with https://, got: %s", out)
			}
			if !strings.Contains(out, "/services/mcp/") {
				t.Errorf("expected URL to contain /services/mcp/, got: %s", out)
			}
			if !strings.HasSuffix(out, "/mcp") {
				t.Errorf("expected URL to end with /mcp, got: %s", out)
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
