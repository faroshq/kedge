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
)

// GraphQLGatewayIntegrated verifies that the kubernetes-graphql-gateway can be
// deployed and that the hub proxies /services/graphql/* to it.
//
// The test:
//  1. Installs the gateway chart with file schema handler + kcp workspace kubeconfig
//  2. Patches the hub StatefulSet to add --graphql-gateway-url and bounces the pod
//  3. Waits for the proxy to serve the kedge GraphQL schema
//  4. Creates a test edge and verifies it appears in the GraphQL response
func GraphQLGatewayIntegrated() features.Feature {
	return features.New("GraphQL/GatewayIntegrated").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			if clusterEnv == nil {
				t.Fatal("cluster environment not found in context")
			}
			if clusterEnv.KCPKubeconfig == "" {
				t.Skip("KCPKubeconfig not set — skipping (requires external kcp setup)")
			}
			if clusterEnv.HubAdminKubeconfig == "" {
				t.Skip("HubAdminKubeconfig not set — skipping")
			}

			chartPath := os.Getenv("KEDGE_GRAPHQL_CHART_PATH")
			if chartPath == "" {
				chartPath = filepath.Join(framework.RepoRoot(), "..", "kubernetes-graphql-gateway",
					"deploy", "charts", "kubernetes-graphql-gateway")
			}
			if _, err := os.Stat(chartPath); err != nil {
				t.Skipf("graphql chart not found at %s — skipping (set KEDGE_GRAPHQL_CHART_PATH): %v", chartPath, err)
			}

			hubRestCfg, err := clientcmd.BuildConfigFromFlags("", clusterEnv.HubAdminKubeconfig)
			if err != nil {
				t.Fatalf("building hub rest config: %v", err)
			}
			hubClient, err := kubernetes.NewForConfig(hubRestCfg)
			if err != nil {
				t.Fatalf("creating hub k8s client: %v", err)
			}

			_, _ = hubClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: graphqlNamespace},
			}, metav1.CreateOptions{})

			// Build a kubeconfig for the gateway listener that points to the kcp
			// workspace. The kcp ExternalName service (kcp.kedge-system.svc.cluster.local)
			// resolves inside the hub cluster and the admin cert from the hub's kcp
			// kubeconfig has access to the workspace APIs.
			kcpKubeconfigBytes, err := buildKCPWorkspaceKubeconfig(ctx, clusterEnv.HubAdminKubeconfig, clusterEnv.HubKubeconfig)
			if err != nil {
				t.Fatalf("building kcp workspace kubeconfig: %v", err)
			}

			secretName := graphqlReleaseName + "-kcp-kubeconfig"
			_, err = hubClient.CoreV1().Secrets(graphqlNamespace).Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: graphqlNamespace},
				Data:       map[string][]byte{"kubeconfig": kcpKubeconfigBytes},
			}, metav1.CreateOptions{})
			if err != nil {
				t.Fatalf("creating kcp kubeconfig secret: %v", err)
			}
			t.Logf("created kcp kubeconfig secret %s/%s", graphqlNamespace, secretName)

			installCtx, installCancel := context.WithTimeout(ctx, 6*time.Minute)
			defer installCancel()

			helmArgs := []string{
				"install", graphqlReleaseName, chartPath,
				"--namespace", graphqlNamespace,
				"--create-namespace",
				"--kubeconfig", clusterEnv.HubAdminKubeconfig,
				"--wait",
				"--timeout", "5m",
				// Use file schema handler instead of gRPC to avoid a startup race condition:
				// with grpc mode, if the gateway container's Subscribe call fires before the
				// listener container has bound port 50051, gRPC returns an immediate
				// UNAVAILABLE error (FailFast=true default) and the gateway runs forever with
				// an empty schema registry, returning 404 for all cluster paths.
				// File mode uses a shared emptyDir + fsnotify which has no ordering dependency
				// between the listener and gateway containers.
				"--set", "schemaHandler=file",
				"--set", "listener.provider=kubernetes",
				"--set", "listener.anchorResource=object.metadata.name == 'default'",
				"--set", "listener.reconcilerGVR=namespaces.v1",
				"--set", "listener.kubeconfigSecret=" + secretName,
				"--set", "listener.kubeconfigSecretKey=kubeconfig",
				"--set", "gateway.playground=false",
				// Pin to v0.0.6 which includes the WaitForReady gRPC fix to tolerate
				// listener startup ordering (fixes /api/clusters/default 404).
				"--set", "image.tag=0.0.6",
			}
			cmd := exec.CommandContext(installCtx, "helm", helmArgs...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("helm install of graphql gateway failed: %v", err)
			}
			t.Logf("graphql gateway helm install completed")

			// Patch the hub StatefulSet to add --graphql-gateway-url and bounce the pod.
			gwServiceURL := fmt.Sprintf("http://%s-kubernetes-graphql-gateway.%s.svc.cluster.local:8080",
				graphqlReleaseName, graphqlNamespace)
			if err := patchHubStatefulSetGraphQLURL(ctx, hubClient, graphqlNamespace, gwServiceURL); err != nil {
				t.Fatalf("patching hub StatefulSet with --graphql-gateway-url: %v", err)
			}

			// Wait for hub to come back healthy.
			hubHealthCtx, hubHealthCancel := context.WithTimeout(ctx, 3*time.Minute)
			defer hubHealthCancel()
			if err := framework.WaitForHubReady(hubHealthCtx, framework.DefaultHubURL); err != nil {
				t.Fatalf("hub not healthy after patching --graphql-gateway-url: %v", err)
			}
			t.Logf("hub healthy after StatefulSet patch")

			return ctx
		}).
		Assess("graphql_introspection_and_edge_query", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			if clusterEnv == nil {
				t.Fatal("cluster environment not found in context")
			}

			// GraphQL is proxied through the hub at /services/graphql/*.
			hubURL := clusterEnv.HubURL
			if hubURL == "" {
				hubURL = framework.DefaultHubURL
			}
			gwBaseURL := hubURL + "/services/graphql/api/clusters/default"

			tlsClient := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
				},
			}

			t.Logf("waiting for GraphQL proxy at %s", gwBaseURL)
			if err := waitForGraphQLReadyWithClient(ctx, tlsClient, gwBaseURL, 3*time.Minute); err != nil {
				t.Fatalf("graphql gateway not ready via hub proxy: %v", err)
			}
			t.Logf("graphql gateway reachable via hub proxy")

			// Introspect — verify kedge_faros_sh field is present.
			resp, err := tlsClient.Post(gwBaseURL, "application/json",
				strings.NewReader(`{"query":"{ __schema { queryType { fields { name } } } }"}`))
			if err != nil {
				t.Fatalf("introspection query failed: %v", err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("introspection returned status %d: %s", resp.StatusCode, body)
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
				t.Fatalf("parsing introspection response: %v\nbody: %s", err, body)
			}

			var hasKedgeField bool
			for _, f := range introspectResult.Data.Schema.QueryType.Fields {
				if f.Name == "kedge_faros_sh" {
					hasKedgeField = true
					break
				}
			}
			if !hasKedgeField {
				names := make([]string, 0, len(introspectResult.Data.Schema.QueryType.Fields))
				for _, f := range introspectResult.Data.Schema.QueryType.Fields {
					names = append(names, f.Name)
				}
				t.Fatalf("kedge_faros_sh field not found in GraphQL schema; got: %v", names)
			}
			t.Logf("introspection confirmed: kedge_faros_sh present")

			// Create a test edge via kedge CLI.
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

			// Mint a kcp SA token for resource queries.
			// The gateway forwards the caller's Bearer token to kcp for resource access.
			kcpToken, tokenErr := mintKCPSAToken(ctx, clusterEnv.HubAdminKubeconfig, clusterEnv.HubKubeconfig)
			if tokenErr != nil {
				t.Logf("warning: could not mint kcp SA token (%v); edge query may fail", tokenErr)
			}

			// Poll until the test edge appears in the GraphQL response.
			edgeQuery := `{"query":"{ kedge_faros_sh { v1alpha1 { Edges { items { metadata { name } } } } } }"}`
			var found bool
			deadline := time.Now().Add(60 * time.Second)
			for time.Now().Before(deadline) {
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, gwBaseURL, strings.NewReader(edgeQuery))
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
				t.Fatalf("edge %q not found in GraphQL response after 60s", testEdgeName)
			}

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			if clusterEnv == nil || clusterEnv.HubAdminKubeconfig == "" {
				return ctx
			}

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
				t.Logf("helm uninstall failed (best-effort): %v", err)
			}
			return ctx
		}).
		Feature()
}

// buildKCPWorkspaceKubeconfig builds a kubeconfig for the graphql gateway listener
// that points to the kcp workspace via the ExternalName service in kedge-system.
// Uses the hub's kcp admin cert for authentication.
//
// The cluster ID is extracted from the HubKubeconfig server URL (set by kedge login
// to https://<hub>/clusters/<clusterID>) — this is always the correct workspace.
func buildKCPWorkspaceKubeconfig(ctx context.Context, hubAdminKubeconfig, hubKubeconfig string) ([]byte, error) {
	clusterID, err := clusterIDFromHubKubeconfig(hubKubeconfig)
	if err != nil {
		return nil, fmt.Errorf("extracting cluster ID from hub kubeconfig: %w", err)
	}

	// Get admin cert from the hub pod's kcp kubeconfig.
	cert, err := exec.CommandContext(ctx, "kubectl",
		"--kubeconfig", hubAdminKubeconfig,
		"exec", "-n", "kedge-system", "kedge-hub-0", "--",
		"sh", "-c",
		`KUBECONFIG=/kcp-kubeconfig/admin.kubeconfig `+
			`kubectl config view --raw -o jsonpath='{.users[0].user.client-certificate-data}' 2>/dev/null`,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("getting kcp admin cert: %w", err)
	}
	key, err := exec.CommandContext(ctx, "kubectl",
		"--kubeconfig", hubAdminKubeconfig,
		"exec", "-n", "kedge-system", "kedge-hub-0", "--",
		"sh", "-c",
		`KUBECONFIG=/kcp-kubeconfig/admin.kubeconfig `+
			`kubectl config view --raw -o jsonpath='{.users[0].user.client-key-data}' 2>/dev/null`,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("getting kcp admin key: %w", err)
	}

	// Build kubeconfig using the ExternalName service so it's reachable from pods.
	inClusterServer := fmt.Sprintf("https://kcp.kedge-system.svc.cluster.local:6443/clusters/%s", clusterID)
	kubeconfigYAML := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    insecure-skip-tls-verify: true
    server: %s
  name: kcp-workspace
contexts:
- context:
    cluster: kcp-workspace
    user: kcp-admin
  name: kcp-workspace
current-context: kcp-workspace
users:
- name: kcp-admin
  user:
    client-certificate-data: %s
    client-key-data: %s
`, inClusterServer, strings.TrimSpace(string(cert)), strings.TrimSpace(string(key)))

	return []byte(kubeconfigYAML), nil
}

// mintKCPSAToken mints a kcp service-account token for the default SA in the
// tenant workspace. Used to authenticate GraphQL resource queries to kcp.
func mintKCPSAToken(ctx context.Context, hubAdminKubeconfig, hubKubeconfig string) (string, error) {
	clusterID, err := clusterIDFromHubKubeconfig(hubKubeconfig)
	if err != nil {
		return "", fmt.Errorf("extracting cluster ID: %w", err)
	}

	tokenOut, err := exec.CommandContext(ctx, "kubectl",
		"--kubeconfig", hubAdminKubeconfig,
		"exec", "-n", "kedge-system", "kedge-hub-0", "--",
		"sh", "-c",
		`KUBECONFIG=/kcp-kubeconfig/admin.kubeconfig `+
			`kubectl create token default -n kedge-system --duration=1h `+
			`--server https://kcp:6443/clusters/`+clusterID+` 2>/dev/null`,
	).Output()
	if err != nil {
		return "", fmt.Errorf("creating kcp SA token: %w", err)
	}
	token := strings.TrimSpace(string(tokenOut))
	if token == "" {
		return "", fmt.Errorf("minted token is empty")
	}
	return token, nil
}

// patchHubStatefulSetGraphQLURL patches the kedge-hub StatefulSet to add
// --graphql-gateway-url if not already present, then deletes the pod and waits
// for it to fully terminate so the StatefulSet controller creates a new one
// with the updated args before the caller calls WaitForHubReady.
//
// Without the termination wait there is a race: the old pod (without
// --graphql-gateway-url) can still answer /healthz during graceful shutdown,
// causing WaitForHubReady to return immediately. The assess phase then hits the
// old pod which has no /services/graphql/* route, and the 3-minute wait expires.
func patchHubStatefulSetGraphQLURL(ctx context.Context, hubClient kubernetes.Interface, namespace, gwURL string) error {
	ss, err := hubClient.AppsV1().StatefulSets(namespace).Get(ctx, "kedge-hub", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting kedge-hub StatefulSet: %w", err)
	}

	flag := "--graphql-gateway-url=" + gwURL
	var alreadySet bool
	for _, arg := range ss.Spec.Template.Spec.Containers[0].Args {
		if strings.HasPrefix(arg, "--graphql-gateway-url=") {
			alreadySet = true
			break
		}
	}
	if alreadySet {
		return nil
	}

	ss.Spec.Template.Spec.Containers[0].Args = append(
		ss.Spec.Template.Spec.Containers[0].Args, flag)
	if _, err := hubClient.AppsV1().StatefulSets(namespace).Update(ctx, ss, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating kedge-hub StatefulSet: %w", err)
	}

	// Record the old pod's UID so we can detect when the StatefulSet controller
	// has replaced it with a fresh pod carrying the updated args.
	oldPod, err := hubClient.CoreV1().Pods(namespace).Get(ctx, "kedge-hub-0", metav1.GetOptions{})
	oldUID := ""
	if err == nil {
		oldUID = string(oldPod.UID)
	}

	// Force-delete the pod so the StatefulSet controller recreates it with new args.
	// GracePeriodSeconds=0 ensures immediate termination rather than waiting the
	// default 30s grace period.
	gracePeriod := int64(0)
	_ = hubClient.CoreV1().Pods(namespace).Delete(ctx, "kedge-hub-0", metav1.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
	})

	// Wait for the StatefulSet controller to create a new pod (different UID).
	// We do NOT wait for the old pod to disappear — a StatefulSet immediately
	// recreates the pod, so the old pod may still be Terminating while the new
	// pod already exists.  Instead we poll until the UID changes, which means
	// the new pod (with --graphql-gateway-url) is in flight.
	waitCtx, waitCancel := context.WithTimeout(ctx, 3*time.Minute)
	defer waitCancel()
	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for kedge-hub-0 to be replaced: %w", waitCtx.Err())
		case <-time.After(2 * time.Second):
		}
		pod, err := hubClient.CoreV1().Pods(namespace).Get(waitCtx, "kedge-hub-0", metav1.GetOptions{})
		if err != nil {
			// Pod not yet recreated — keep waiting.
			continue
		}
		if string(pod.UID) != oldUID {
			// New pod has been created by the StatefulSet controller.
			return nil
		}
	}
}

// clusterIDFromHubKubeconfig extracts the kcp workspace cluster ID from the
// HubKubeconfig server URL. After kedge login, the server URL is set to
// https://<hub>/clusters/<clusterID> where <clusterID> is the logical cluster name.
func clusterIDFromHubKubeconfig(hubKubeconfigPath string) (string, error) {
	cfg, err := clientcmd.LoadFromFile(hubKubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("loading hub kubeconfig: %w", err)
	}
	ctxName := cfg.CurrentContext
	if ctxName == "" {
		for k := range cfg.Contexts {
			ctxName = k
			break
		}
	}
	ctxObj := cfg.Contexts[ctxName]
	if ctxObj == nil {
		return "", fmt.Errorf("context %q not found", ctxName)
	}
	clusterObj := cfg.Clusters[ctxObj.Cluster]
	if clusterObj == nil {
		return "", fmt.Errorf("cluster %q not found", ctxObj.Cluster)
	}
	// Extract /clusters/<id> from URL.
	serverURL := clusterObj.Server
	const clusterPrefix = "/clusters/"
	idx := strings.Index(serverURL, clusterPrefix)
	if idx < 0 {
		return "", fmt.Errorf("no /clusters/ in server URL: %s", serverURL)
	}
	tail := serverURL[idx+len(clusterPrefix):]
	clusterID := strings.SplitN(tail, "/", 2)[0]
	if clusterID == "" {
		return "", fmt.Errorf("cluster ID is empty in server URL: %s", serverURL)
	}
	return clusterID, nil
}

// waitForGraphQLReadyWithClient polls the GraphQL endpoint until 200 OK.
func waitForGraphQLReadyWithClient(ctx context.Context, client *http.Client, rawURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL,
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
	return fmt.Errorf("graphql endpoint %s not ready after %v", rawURL, timeout)
}
