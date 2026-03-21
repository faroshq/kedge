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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
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
			// workspace. Uses "kcp:6443" — CoreDNS rewrites "kcp" to
			// kcp.kcp.svc.cluster.local inside the cluster. The admin cert from the
			// hub's kcp kubeconfig has access to the workspace APIs.
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

			// Ensure a "default" namespace exists in the kcp tenant workspace.
			// The GraphQL gateway listener is configured with
			//   anchorResource=object.metadata.name == 'default'  (namespaces.v1)
			// so it only emits a schema for "/api/clusters/default" once it sees
			// a namespace with that name. Unlike regular Kubernetes, kcp workspaces
			// do not auto-create a "default" namespace, so we create it explicitly
			// here using the admin credentials we already extracted for the gateway.
			if ensureErr := ensureKCPDefaultNamespace(ctx, clusterEnv.HubAdminKubeconfig, clusterEnv.HubKubeconfig); ensureErr != nil {
				t.Fatalf("ensuring default namespace in kcp workspace: %v", ensureErr)
			}
			t.Logf("ensured default namespace in kcp tenant workspace")

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

			// Patch the gateway Deployment to mount the kcp workspace kubeconfig secret
			// in the listener container. The chart has no native support for kubeconfigSecret,
			// so --set listener.kubeconfigSecret=... is silently ignored. Without the kcp
			// kubeconfig the listener uses the pod's in-cluster service account (hub cluster),
			// generating a schema without kedge CRDs (which live in the kcp workspace).
			if err := patchGatewayDeploymentWithKCPKubeconfig(ctx, hubClient, graphqlNamespace, graphqlReleaseName, secretName, "kubeconfig"); err != nil {
				t.Fatalf("patching gateway deployment with kcp kubeconfig: %v", err)
			}
			t.Logf("gateway deployment patched with kcp kubeconfig, rollout complete")

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
			if err := waitForGraphQLReadyWithClient(ctx, tlsClient, gwBaseURL, 6*time.Minute); err != nil {
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
				if edgeResp.StatusCode != http.StatusOK || bytes.Contains(edgeBody, []byte("errors")) {
					t.Logf("edge poll: status=%d body=%s", edgeResp.StatusCode, edgeBody)
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

	// Build kubeconfig using the CoreDNS-rewritten "kcp" hostname (rewritten to
	// kcp.kcp.svc.cluster.local by the CoreDNS rewrite rule installed during setup).
	// The hub pod's in-cluster kubeconfig also uses "kcp:6443" for the same reason.
	inClusterServer := fmt.Sprintf("https://kcp:6443/clusters/%s", clusterID)
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

// patchGatewayDeploymentWithKCPKubeconfig patches the kubernetes-graphql-gateway
// Deployment so that the listener container uses the kcp workspace kubeconfig
// instead of the pod's in-cluster service-account config.
//
// The chart currently has no native support for kubeconfigSecret values, so the
// --set listener.kubeconfigSecret=... passed during helm install is silently ignored.
// Without the kcp kubeconfig the listener watches the hub kind cluster and generates
// a schema that lacks the kedge CRDs (which live in the kcp workspace).
//
// This function:
//  1. Adds the kcp kubeconfig secret as a volume on the Deployment
//  2. Mounts it at /kcp-kubeconfig in the listener container
//  3. Appends --kubeconfig=/kcp-kubeconfig/kubeconfig to listener args
//  4. Rolls out the Deployment and waits for the new pod to be Ready
func patchGatewayDeploymentWithKCPKubeconfig(ctx context.Context, hubClient kubernetes.Interface, namespace, releaseName, secretName, secretKey string) error {
	deployName := releaseName + "-kubernetes-graphql-gateway"

	deploy, err := hubClient.AppsV1().Deployments(namespace).Get(ctx, deployName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting gateway deployment %s: %w", deployName, err)
	}

	const mountPath = "/kcp-kubeconfig"
	const volumeName = "kcp-kubeconfig"
	kubeconfigFlag := "--kubeconfig=" + mountPath + "/" + secretKey

	// Idempotency: don't patch if already done.
	for _, v := range deploy.Spec.Template.Spec.Volumes {
		if v.Name == volumeName {
			return nil
		}
	}

	// Find the listener container index.
	listenerIdx := -1
	for i, c := range deploy.Spec.Template.Spec.Containers {
		if c.Name == "listener" {
			listenerIdx = i
			break
		}
	}
	if listenerIdx < 0 {
		return fmt.Errorf("listener container not found in deployment %s", deployName)
	}

	// Add volume.
	deploy.Spec.Template.Spec.Volumes = append(deploy.Spec.Template.Spec.Volumes, corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	})

	// Add volumeMount and --kubeconfig arg to listener.
	deploy.Spec.Template.Spec.Containers[listenerIdx].VolumeMounts = append(
		deploy.Spec.Template.Spec.Containers[listenerIdx].VolumeMounts,
		corev1.VolumeMount{
			Name:      volumeName,
			MountPath: mountPath,
			ReadOnly:  true,
		},
	)
	deploy.Spec.Template.Spec.Containers[listenerIdx].Args = append(
		deploy.Spec.Template.Spec.Containers[listenerIdx].Args,
		kubeconfigFlag,
	)

	if _, err := hubClient.AppsV1().Deployments(namespace).Update(ctx, deploy, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating gateway deployment: %w", err)
	}

	// Wait for the rollout: poll until observedGeneration >= generation AND all replicas ready.
	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for gateway deployment rollout: %w", waitCtx.Err())
		case <-time.After(2 * time.Second):
		}
		d, err := hubClient.AppsV1().Deployments(namespace).Get(waitCtx, deployName, metav1.GetOptions{})
		if err != nil {
			continue
		}
		if deploymentReady(d) {
			return nil
		}
	}
}

// deploymentReady returns true if the Deployment has completed its rollout and
// all desired replicas are available.
func deploymentReady(d *appsv1.Deployment) bool {
	if d.Status.ObservedGeneration < d.Generation {
		return false
	}
	desired := int32(1)
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	return d.Status.ReadyReplicas >= desired && d.Status.UpdatedReplicas >= desired
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

// ensureKCPDefaultNamespace creates a "default" namespace in the kcp tenant
// workspace if it does not already exist, and grants the default SA in
// kedge-system cluster-admin access in the kcp workspace so that the minted
// SA token used by the GraphQL edge query can list Edge resources.
//
// The GraphQL gateway listener uses
// anchorResource=object.metadata.name == 'default' (namespaces.v1) to detect
// which clusters to expose. kcp workspaces do not auto-create a "default"
// namespace (unlike plain Kubernetes), so without this the gateway never emits
// a schema for /api/clusters/default and the 3-minute readiness wait expires.
//
// We authenticate by exec-ing into the kedge-hub-0 pod (which has the kcp
// admin kubeconfig mounted) and running kubectl against the tenant workspace
// path identified from the HubKubeconfig server URL.
func ensureKCPDefaultNamespace(ctx context.Context, hubAdminKubeconfig, hubKubeconfig string) error {
	clusterID, err := clusterIDFromHubKubeconfig(hubKubeconfig)
	if err != nil {
		return fmt.Errorf("extracting cluster ID: %w", err)
	}

	// Try to create the namespace; ignore AlreadyExists.
	createNSCmd := fmt.Sprintf(
		`KUBECONFIG=/kcp-kubeconfig/admin.kubeconfig `+
			`kubectl create namespace default `+
			`--server https://kcp:6443/clusters/%s `+
			`--insecure-skip-tls-verify 2>&1 || true`,
		clusterID,
	)
	out, err := exec.CommandContext(ctx, "kubectl",
		"--kubeconfig", hubAdminKubeconfig,
		"exec", "-n", "kedge-system", "kedge-hub-0", "--",
		"sh", "-c", createNSCmd,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("exec creating default namespace in kcp workspace %s: %w (output: %s)", clusterID, err, out)
	}

	// Grant the default SA in kedge-system cluster-admin access in the kcp workspace.
	// The GraphQL edge-query poll uses a token minted for this SA; without RBAC it
	// cannot list Edge resources and the query silently returns empty results.
	createRBACCmd := fmt.Sprintf(
		`KUBECONFIG=/kcp-kubeconfig/admin.kubeconfig `+
			`kubectl create clusterrolebinding kedge-graphql-test-admin `+
			`--clusterrole=cluster-admin `+
			`--serviceaccount=kedge-system:default `+
			`--server https://kcp:6443/clusters/%s `+
			`--insecure-skip-tls-verify 2>&1 || true`,
		clusterID,
	)
	out, err = exec.CommandContext(ctx, "kubectl",
		"--kubeconfig", hubAdminKubeconfig,
		"exec", "-n", "kedge-system", "kedge-hub-0", "--",
		"sh", "-c", createRBACCmd,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("exec creating RBAC for default SA in kcp workspace %s: %w (output: %s)", clusterID, err, out)
	}

	return nil
}

// waitForGraphQLReadyWithClient polls the GraphQL endpoint until 200 OK.
// It logs the last HTTP status code and body snippet seen on timeout to aid debugging.
func waitForGraphQLReadyWithClient(ctx context.Context, client *http.Client, rawURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastStatus int
	var lastErr error
	logTicker := time.NewTicker(30 * time.Second)
	defer logTicker.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-logTicker.C:
			remaining := time.Until(deadline).Round(time.Second)
			if lastErr != nil {
				klog.Infof("waitForGraphQLReady: still waiting (%v remaining); last error: %v", remaining, lastErr)
			} else if lastStatus != 0 {
				klog.Infof("waitForGraphQLReady: still waiting (%v remaining); last HTTP status: %d", remaining, lastStatus)
			}
		default:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL,
			bytes.NewBufferString(`{"query":"{ __typename }"}`))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, doErr := client.Do(req)
		if doErr != nil {
			lastErr = doErr
			lastStatus = 0
		} else {
			lastErr = nil
			lastStatus = resp.StatusCode
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if lastErr != nil {
		return fmt.Errorf("graphql endpoint %s not ready after %v: last error: %w", rawURL, timeout, lastErr)
	}
	return fmt.Errorf("graphql endpoint %s not ready after %v: last HTTP status: %d", rawURL, timeout, lastStatus)
}
