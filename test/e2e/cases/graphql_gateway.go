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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/faroshq/faros-kedge/test/e2e/framework"
)

const (
	graphqlReleaseName = "kedge-graphql-gateway"
	graphqlNamespace   = "kedge-system"
	graphqlLocalPort   = 18080
)

// GraphQLGatewayIntegrated verifies that the kubernetes-graphql-gateway can be
// deployed against the kedge kcp instance and exposes a valid GraphQL schema
// including kedge resource types.
//
// Requirements:
//   - ClusterEnv.KCPKubeconfig must be set (external kcp setup or --with-external-kcp flag)
//   - ClusterEnv.HubAdminKubeconfig must be set (admin access to hub cluster for helm deploy)
//   - The kubernetes-graphql-gateway Helm chart must be available at
//     KEDGE_GRAPHQL_CHART_PATH env var, or at the default local path.
func GraphQLGatewayIntegrated() features.Feature {
	return features.New("GraphQL/GatewayIntegrated").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			if clusterEnv == nil {
				t.Fatal("cluster environment not found in context")
			}
			if clusterEnv.KCPKubeconfig == "" {
				t.Skip("KCPKubeconfig not set — skipping GraphQL gateway test (requires external kcp setup)")
			}
			if clusterEnv.HubAdminKubeconfig == "" {
				t.Skip("HubAdminKubeconfig not set — skipping GraphQL gateway test")
			}

			// Determine chart path.
			chartPath := os.Getenv("KEDGE_GRAPHQL_CHART_PATH")
			if chartPath == "" {
				// Default: local chart relative to repo root.
				chartPath = filepath.Join(framework.RepoRoot(), "..", "kubernetes-graphql-gateway",
					"deploy", "charts", "kubernetes-graphql-gateway")
			}
			if _, err := os.Stat(chartPath); err != nil {
				t.Skipf("graphql chart not found at %s — skipping (set KEDGE_GRAPHQL_CHART_PATH): %v", chartPath, err)
			}

			// Create a Kubernetes client for the hub cluster.
			hubRestCfg, err := clientcmd.BuildConfigFromFlags("", clusterEnv.HubAdminKubeconfig)
			if err != nil {
				t.Fatalf("failed to build hub rest config: %v", err)
			}
			hubClient, err := kubernetes.NewForConfig(hubRestCfg)
			if err != nil {
				t.Fatalf("failed to create hub k8s client: %v", err)
			}

			// Create namespace.
			_, _ = hubClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: graphqlNamespace},
			}, metav1.CreateOptions{})

			// Build an in-cluster kubeconfig for the graphql-gateway listener.
			//
			// The listener runs as a pod inside the hub cluster and needs to reach the kcp
			// workspace server. The HubKubeconfig (written after kedge login) has a server
			// URL of the form https://kedge.localhost:8443/clusters/<workspace-id> — this
			// hostname is only resolvable on the CI runner host, not inside pods.
			//
			// Solution: replace the external host with the hub's in-cluster service FQDN
			// (kedge-hub.kedge-system.svc.cluster.local) and authenticate with the static
			// dev-token that the hub accepts. The hub's kcp proxy forwards the request to
			// kcp on behalf of the caller.
			kcpKubeconfigBytes, err := buildInClusterHubKubeconfig(clusterEnv.HubKubeconfig, clusterEnv.Token)
			if err != nil {
				t.Fatalf("failed to build in-cluster hub kubeconfig: %v", err)
			}
			secretName := graphqlReleaseName + "-kcp-kubeconfig"
			_, err = hubClient.CoreV1().Secrets(graphqlNamespace).Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: graphqlNamespace,
				},
				Data: map[string][]byte{
					"kubeconfig": kcpKubeconfigBytes,
				},
			}, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("failed to create kcp kubeconfig secret: %v", err)
			}
			t.Logf("created kcp kubeconfig secret %s/%s", graphqlNamespace, secretName)

			// helm install the graphql gateway.
			// The context timeout must exceed the helm --timeout so the helm process
			// can surface a proper error rather than being killed mid-wait.
			installCtx, installCancel := context.WithTimeout(ctx, 6*time.Minute)
			defer installCancel()

			helmArgs := []string{
				"install", graphqlReleaseName, chartPath,
				"--namespace", graphqlNamespace,
				"--create-namespace",
				"--kubeconfig", clusterEnv.HubAdminKubeconfig,
				"--wait",
				"--timeout", "5m",
				"--set", "schemaHandler=grpc",
				// Use kubernetes provider pointing directly at the kcp tenant workspace.
				// The HubKubeconfig context already contains the workspace-scoped server URL
				// (/clusters/<workspace-id>) after kedge login. This avoids the auth complexity
				// of the kcp virtual workspace provider while still querying real kedge resources.
				"--set", "listener.provider=kubernetes",
				"--set", "listener.kubeconfigSecret=" + secretName,
				"--set", "listener.kubeconfigSecretKey=kubeconfig",
				// The anchorResource must be a valid CEL expression.
				"--set", "listener.anchorResource=object.metadata.name == 'default'",
				"--set", "listener.reconcilerGVR=namespaces.v1",
				"--set", "gateway.playground=false",
				// Use "latest" tag — the chart appVersion (v0.0.1) uses a "v" prefix that
				// doesn't match the published image tags (0.0.1, latest) in ghcr.io.
				"--set", "image.tag=latest",
			}

			cmd := exec.CommandContext(installCtx, "helm", helmArgs...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("helm install of graphql gateway failed: %v", err)
			}
			t.Logf("graphql gateway helm install completed")

			return ctx
		}).
		Assess("graphql_introspection_and_edge_query", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			if clusterEnv == nil {
				t.Fatal("cluster environment not found in context")
			}

			// GraphQL is proxied through the hub API server at /services/graphql/*.
			// The hub wires --graphql-gateway-url when the subchart is enabled, so no
			// second ingress or port-forward is needed.
			//
			// Path: https://<hub>/services/graphql/api/clusters/default
			// The hub TLS cert uses a self-signed CA; skip verification for e2e.
			hubURL := clusterEnv.HubURL
			if hubURL == "" {
				hubURL = framework.DefaultHubURL
			}
			gwBaseURL := hubURL + "/services/graphql/api/clusters/default"

			// Create an HTTP client that skips TLS verification (hub uses self-signed cert in e2e).
			tlsClient := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // e2e only
				},
			}

			// Wait for the hub proxy to start serving GraphQL.  The gateway pod needs a
			// moment after helm install to become Ready and register its cluster.
			t.Logf("waiting for GraphQL proxy at %s", gwBaseURL)
			if err := waitForGraphQLReadyWithClient(ctx, tlsClient, gwBaseURL, 6*time.Minute); err != nil {
				t.Fatalf("graphql gateway not ready via hub proxy: %v", err)
			}
			t.Logf("graphql gateway reachable via hub proxy at %s", gwBaseURL)

			// --- Step 1: Introspection — verify kedge_faros_sh type is present ---
			introspectionQuery := `{"query": "{ __schema { queryType { fields { name } } } }"}`
			resp, err := tlsClient.Post(gwBaseURL, "application/json", strings.NewReader(introspectionQuery))
			if err != nil {
				t.Fatalf("introspection query failed: %v", err)
			}
			defer resp.Body.Close() //nolint:errcheck

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("introspection returned status %d", resp.StatusCode)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read introspection response: %v", err)
			}

			var introspectResult struct {
				Data struct {
					Schema struct {
						QueryType struct {
							Fields []struct {
								Name string `json:"name"`
							} `json:"fields"`
						} `json:"queryType"`
					} `json:"__schema"`
				} `json:"data"`
			}
			if err := json.Unmarshal(body, &introspectResult); err != nil {
				t.Fatalf("failed to parse introspection response: %v\nbody: %s", err, body)
			}

			// Verify kedge_faros_sh field exists in the schema.
			var hasKedgeField bool
			for _, f := range introspectResult.Data.Schema.QueryType.Fields {
				if f.Name == "kedge_faros_sh" {
					hasKedgeField = true
					break
				}
			}
			if !hasKedgeField {
				fieldNames := make([]string, 0, len(introspectResult.Data.Schema.QueryType.Fields))
				for _, f := range introspectResult.Data.Schema.QueryType.Fields {
					fieldNames = append(fieldNames, f.Name)
				}
				t.Fatalf("kedge_faros_sh field not found in GraphQL schema; got: %v", fieldNames)
			}
			t.Logf("introspection confirmed: kedge_faros_sh field present in schema")

			// --- Step 2: Create a test edge and query it via GraphQL ---

			// Create an edge using the kedge client.
			kedgeCli := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, framework.DefaultHubURL)
			if err := kedgeCli.Login(ctx, framework.DevToken); err != nil {
				t.Fatalf("kedge login failed: %v", err)
			}
			const testEdgeName = "graphql-edge-test"
			if err := kedgeCli.EdgeCreate(ctx, testEdgeName, "kubernetes"); err != nil {
				t.Fatalf("failed to create test edge: %v", err)
			}
			t.Logf("created test edge %q", testEdgeName)
			defer func() {
				if err := kedgeCli.EdgeDelete(ctx, testEdgeName); err != nil {
					t.Logf("cleanup: failed to delete edge %q: %v", testEdgeName, err)
				}
			}()

			// The GraphQL gateway is configured with the kubernetes provider pointing at
			// the hub's kcp proxy (https://kedge-hub.kedge-system.svc.cluster.local:8443/clusters/<id>).
			// The gateway forwards the caller's Bearer token to that server; the hub's kcp
			// proxy authenticates it. Use the same static dev-token used for kedge login.
			kcpToken := clusterEnv.Token
			if kcpToken == "" {
				kcpToken = framework.DevToken
			}

			// Poll until the edge appears in the GraphQL response (gateway may lag slightly
			// behind the kcp store).
			edgeQuery := `{"query":"{ kedge_faros_sh { v1alpha1 { Edges { items { metadata { name } } } } } }"}`
			var found bool
			deadline := time.Now().Add(60 * time.Second)
			for time.Now().Before(deadline) {
				req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, gwBaseURL, strings.NewReader(edgeQuery))
				if reqErr != nil {
					t.Fatalf("building edge query request: %v", reqErr)
				}
				req.Header.Set("Content-Type", "application/json")
				if kcpToken != "" {
					req.Header.Set("Authorization", "Bearer "+kcpToken)
				}

				edgeResp, doErr := tlsClient.Do(req)
				if doErr != nil {
					time.Sleep(time.Second)
					continue
				}
				edgeBody, _ := io.ReadAll(edgeResp.Body)
				_ = edgeResp.Body.Close()

				if bytes.Contains(edgeBody, []byte(testEdgeName)) {
					found = true
					t.Logf("edge %q found in GraphQL response", testEdgeName)
					break
				}
				time.Sleep(time.Second)
			}
			if !found {
				t.Fatalf("edge %q not found in GraphQL response after 30s", testEdgeName)
			}

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			if clusterEnv == nil || clusterEnv.HubAdminKubeconfig == "" {
				return ctx
			}

			// Best-effort uninstall.
			uninstallCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(uninstallCtx, "helm",
				"uninstall", graphqlReleaseName,
				"--namespace", graphqlNamespace,
				"--kubeconfig", clusterEnv.HubAdminKubeconfig,
			)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				t.Logf("helm uninstall of graphql gateway failed (best-effort): %v", err)
			}
			return ctx
		}).
		Feature()
}

// waitForGraphQLReadyWithClient polls the GraphQL endpoint using the provided
// HTTP client until it returns a 200 OK or the timeout is reached.
func waitForGraphQLReadyWithClient(ctx context.Context, client *http.Client, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url,
			bytes.NewBufferString(`{"query":"{ __typename }"}`))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("graphql endpoint %s not ready after %v", url, timeout)
}

// buildInClusterHubKubeconfig reads the HubKubeconfig (which has a workspace-scoped
// server URL like https://kedge.localhost:8443/clusters/<id>), replaces the external
// hostname with the hub's in-cluster service FQDN, and returns a kubeconfig YAML
// suitable for use by pods running inside the hub cluster.
//
// Auth is via the static bearer token (dev-token in e2e) that the hub accepts.
func buildInClusterHubKubeconfig(hubKubeconfigPath, token string) ([]byte, error) {
	hubCfg, err := clientcmd.LoadFromFile(hubKubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("loading hub kubeconfig: %w", err)
	}

	ctx := hubCfg.CurrentContext
	if ctx == "" {
		for k := range hubCfg.Contexts {
			ctx = k
			break
		}
	}
	ctxObj := hubCfg.Contexts[ctx]
	if ctxObj == nil {
		return nil, fmt.Errorf("context %q not found in hub kubeconfig", ctx)
	}

	clusterObj := hubCfg.Clusters[ctxObj.Cluster]
	if clusterObj == nil {
		return nil, fmt.Errorf("cluster %q not found in hub kubeconfig", ctxObj.Cluster)
	}

	// Extract just the path component (/clusters/<id>) from the external server URL.
	serverURL := clusterObj.Server
	parsed, parseErr := url.Parse(serverURL)
	if parseErr != nil {
		return nil, fmt.Errorf("parsing hub server URL %q: %w", serverURL, parseErr)
	}

	// Build in-cluster URL: hub service FQDN + original path.
	inClusterServer := fmt.Sprintf("https://kedge-hub.kedge-system.svc.cluster.local:8443%s", parsed.Path)

	yaml := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    insecure-skip-tls-verify: true
    server: %s
  name: hub-incluster
contexts:
- context:
    cluster: hub-incluster
    user: dev
  name: hub-incluster
current-context: hub-incluster
users:
- name: dev
  user:
    token: %s
`, inClusterServer, token)

	return []byte(yaml), nil
}

// mintKCPToken creates a short-lived service-account token in the kcp tenant
// workspace (kedge-system/default) and returns it as a string.
//
// The KCP kubeconfig may point at the root workspace; we derive the tenant
// workspace path by reading the hub's user kubeconfig server URL, then issue a
// kubectl create token via the admin kubeconfig.  The token is subsequently
// forwarded as the Bearer token in GraphQL requests so kcp can authorise
// resource lookups.
