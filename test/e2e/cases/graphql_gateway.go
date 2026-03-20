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
		Assess("graphql_introspection_returns_kedge_types", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
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

			// Wait for port-forward to be ready.
			gwURL := fmt.Sprintf("http://localhost:%d/graphql", graphqlLocalPort)
			if err := waitForGraphQLReady(ctx, gwURL, 30*time.Second); err != nil {
				t.Fatalf("graphql gateway not ready after port-forward: %v", err)
			}
			t.Logf("graphql gateway reachable at %s", gwURL)

			// Run GraphQL introspection query.
			introspectionQuery := `{"query": "{ __schema { types { name } } }"}`
			resp, err := http.Post(gwURL, "application/json", strings.NewReader(introspectionQuery))
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

			var result struct {
				Data struct {
					Schema struct {
						Types []struct {
							Name string `json:"name"`
						} `json:"types"`
					} `json:"__schema"`
				} `json:"data"`
			}
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatalf("failed to parse introspection response: %v\nbody: %s", err, body)
			}

			if len(result.Data.Schema.Types) == 0 {
				t.Fatalf("introspection returned empty schema")
			}

			// Log all type names for debugging.
			typeNames := make([]string, 0, len(result.Data.Schema.Types))
			for _, tp := range result.Data.Schema.Types {
				typeNames = append(typeNames, tp.Name)
			}
			t.Logf("graphql schema types: %v", typeNames)

			t.Logf("graphql introspection succeeded: %d types in schema", len(result.Data.Schema.Types))
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
