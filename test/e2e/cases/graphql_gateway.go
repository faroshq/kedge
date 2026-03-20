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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

			// Create Secret with kcp admin kubeconfig.
			kcpKubeconfigBytes, err := os.ReadFile(clusterEnv.KCPKubeconfig)
			if err != nil {
				t.Fatalf("failed to read kcp kubeconfig: %v", err)
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
			installCtx, installCancel := context.WithTimeout(ctx, 3*time.Minute)
			defer installCancel()

			helmArgs := []string{
				"install", graphqlReleaseName, chartPath,
				"--namespace", graphqlNamespace,
				"--create-namespace",
				"--kubeconfig", clusterEnv.HubAdminKubeconfig,
				"--wait",
				"--timeout", "2m",
				"--set", "schemaHandler=grpc",
				"--set", "listener.provider=kcp",
				"--set", "listener.kubeconfigSecret=" + secretName,
				"--set", "listener.kubeconfigSecretKey=kubeconfig",
				"--set", "listener.extraArgs[0]=--apiexport-endpoint-slice-name=kedge.faros.sh",
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

			// Port-forward the gateway service.
			pfCtx, pfCancel := context.WithCancel(ctx)
			defer pfCancel()

			svcName := graphqlReleaseName + "-kubernetes-graphql-gateway"
			pfCmd := exec.CommandContext(pfCtx, "kubectl",
				"--kubeconfig", clusterEnv.HubAdminKubeconfig,
				"port-forward",
				"-n", graphqlNamespace,
				"svc/"+svcName,
				fmt.Sprintf("%d:8080", graphqlLocalPort),
			)
			pfCmd.Stdout = io.Discard
			pfCmd.Stderr = io.Discard
			if err := pfCmd.Start(); err != nil {
				t.Fatalf("failed to start kubectl port-forward: %v", err)
			}
			defer pfCmd.Process.Kill() //nolint:errcheck

			// The gateway serves GraphQL at /api/clusters/{clusterName} where the cluster
			// name is determined by the kubernetes provider (defaults to "default" for a
			// single-cluster setup pointing at the tenant kcp workspace).
			gwBaseURL := fmt.Sprintf("http://localhost:%d/api/clusters/default", graphqlLocalPort)

			// Give the port-forward process a moment to establish the tunnel before polling.
			time.Sleep(2 * time.Second)

			// Wait for port-forward to be ready.
			if err := waitForGraphQLReady(ctx, gwBaseURL, 90*time.Second); err != nil {
				t.Fatalf("graphql gateway not ready after port-forward: %v", err)
			}
			t.Logf("graphql gateway reachable at %s", gwBaseURL)

			// --- Step 1: Introspection — verify kedge_faros_sh type is present ---
			introspectionQuery := `{"query": "{ __schema { queryType { fields { name } } } }"}`
			resp, err := http.Post(gwBaseURL, "application/json", strings.NewReader(introspectionQuery))
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

			// Obtain a kcp service-account token so the GraphQL gateway can forward
			// credentials to kcp when resolving resources.
			//
			// The gateway proxies the caller's Bearer token to kcp; in the e2e setup the
			// easiest credential to obtain programmatically is a SA token minted from the
			// hub cluster's kcp server.
			kcpToken, err := mintKCPToken(ctx, clusterEnv.KCPKubeconfig, clusterEnv.HubAdminKubeconfig)
			if err != nil {
				t.Fatalf("failed to obtain kcp token: %v", err)
			}

			// Poll until the edge appears in the GraphQL response (gateway may lag slightly
			// behind the kcp store).
			edgeQuery := `{"query":"{ kedge_faros_sh { v1alpha1 { Edges { items { metadata { name } } } } } }"}`
			var found bool
			deadline := time.Now().Add(30 * time.Second)
			for time.Now().Before(deadline) {
				req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, gwBaseURL, strings.NewReader(edgeQuery))
				if reqErr != nil {
					t.Fatalf("building edge query request: %v", reqErr)
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+kcpToken)

				edgeResp, doErr := http.DefaultClient.Do(req)
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

// waitForGraphQLReady polls the GraphQL endpoint until it returns a response
// or the timeout is reached.
func waitForGraphQLReady(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := http.Post(url, "application/json", bytes.NewBufferString(`{"query":"{ __typename }"}`))
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

// mintKCPToken creates a short-lived service-account token in the kcp tenant
// workspace (kedge-system/default) and returns it as a string.
//
// The KCP kubeconfig may point at the root workspace; we derive the tenant
// workspace path by reading the hub's user kubeconfig server URL, then issue a
// kubectl create token via the admin kubeconfig.  The token is subsequently
// forwarded as the Bearer token in GraphQL requests so kcp can authorise
// resource lookups.
func mintKCPToken(ctx context.Context, kcpKubeconfig, hubAdminKubeconfig string) (string, error) {
	// kubectl create token needs to run against the tenant workspace.
	// The kcp kubeconfig's server is the root workspace; pass the hub admin
	// kubeconfig which resolves to the user workspace via kcp's context.
	out, err := exec.CommandContext(ctx, "kubectl",
		"--kubeconfig", hubAdminKubeconfig,
		"create", "token", "default",
		"-n", "kedge-system",
		"--duration", "1h",
	).Output()
	if err != nil {
		// Fallback: try directly with kcp admin kubeconfig.
		out, err = exec.CommandContext(ctx, "kubectl",
			"--kubeconfig", kcpKubeconfig,
			"create", "token", "default",
			"-n", "kedge-system",
			"--duration", "1h",
		).Output()
		if err != nil {
			return "", fmt.Errorf("kubectl create token failed: %w", err)
		}
	}
	return strings.TrimSpace(string(out)), nil
}
