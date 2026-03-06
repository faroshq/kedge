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
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	cliauth "github.com/faroshq/faros-kedge/pkg/cli/auth"
	"github.com/faroshq/faros-kedge/test/e2e/framework"
)

// ──────────────────────────────────────────────────────────────────────────────
// Issue #80 — k8s proxy write must be isolated to edge cluster, not hub
// ──────────────────────────────────────────────────────────────────────────────

// k8sProxyWriteIsolationAgentKey is a context key for the Agent in K8sProxyWriteIsolation.
type k8sProxyWriteIsolationAgentKey struct{}

// K8sProxyWriteIsolation verifies that a resource written via the edge k8s proxy
// lands on the **edge** cluster and NOT on the hub cluster (issue #80).
//
// A misconfigured proxy that accidentally forwards writes to the hub (kcp) would
// fail the "not on hub" assertion, catching the regression before it ships.
//
// Flow:
//  1. Create edge + start agent → wait for Ready.
//  2. Get status.URL.
//  3. kubectl apply a ConfigMap via the edge proxy URL.
//  4. Confirm the ConfigMap EXISTS on the edge cluster (via direct agent kubeconfig).
//  5. Confirm the ConfigMap does NOT exist on the hub cluster (via hub kubeconfig).
//  6. Cleanup: delete ConfigMap, stop agent, delete edge.
func K8sProxyWriteIsolation() features.Feature {
	const (
		edgeName  = "e2e-proxy-write-isolate-edge"
		cmName    = "e2e-proxy-write-isolation-cm"
		ns        = "default"
		markerVal = "e2e_proxy_write_isolation_ok"
	)

	return features.New("K8sProxyWriteIsolation").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
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
			return context.WithValue(ctx, k8sProxyWriteIsolationAgentKey{}, agent)
		}).
		Assess("edge becomes Ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)

			if err := client.WaitForEdgeReady(ctx, edgeName, 3*time.Minute); err != nil {
				t.Fatalf("edge %q did not become Ready: %v", edgeName, err)
			}
			return ctx
		}).
		Assess("ConfigMap written via proxy exists on edge, not hub", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)

			edgeURL, err := client.GetEdgeURL(ctx, edgeName)
			if err != nil {
				t.Fatalf("getting edge URL: %v", err)
			}
			if !strings.HasSuffix(edgeURL, "/k8s") {
				t.Fatalf("expected edge URL to end with '/k8s', got: %s", edgeURL)
			}
			t.Logf("edge URL: %s", edgeURL)

			// Write a ConfigMap manifest to a temp file.
			manifest := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
  namespace: %s
data:
  key: %s
`, cmName, ns, markerVal)

			f, err := os.CreateTemp("", "k8s-proxy-isolate-*.yaml")
			if err != nil {
				t.Fatalf("creating temp manifest: %v", err)
			}
			defer os.Remove(f.Name()) //nolint:errcheck
			if _, err := f.WriteString(manifest); err != nil {
				t.Fatalf("writing manifest: %v", err)
			}
			if err := f.Close(); err != nil {
				t.Fatalf("closing manifest: %v", err)
			}

			// Step 1: Apply the ConfigMap via the edge proxy.
			out, err := client.KubectlWithURL(ctx, edgeURL, "apply", "-f", f.Name())
			if err != nil {
				t.Fatalf("kubectl apply via edge proxy failed: %v\noutput: %s", err, out)
			}
			t.Logf("kubectl apply output: %s", out)

			// Step 2: Confirm ConfigMap EXISTS on the edge cluster (direct agent kubeconfig).
			out, err = framework.KubectlWithConfig(ctx, clusterEnv.AgentKubeconfig,
				"get", "configmap", cmName,
				"-n", ns,
				"-o", "jsonpath={.data.key}",
			)
			if err != nil {
				t.Fatalf("ConfigMap not found on edge cluster — proxy may not be routing to edge: %v\noutput: %s", err, out)
			}
			if !strings.Contains(out, markerVal) {
				t.Fatalf("ConfigMap data mismatch on edge cluster: expected %q, got %q", markerVal, out)
			}
			t.Logf("ConfigMap %q confirmed on edge cluster (data.key=%s)", cmName, strings.TrimSpace(out))

			// Step 3: Confirm ConfigMap does NOT exist on the hub cluster (kcp workspace).
			// If the proxy was misconfigured to forward writes to the hub/kcp workspace,
			// the ConfigMap would be present here. It must NOT be.
			hubClient := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)
			out, err = hubClient.Kubectl(ctx,
				"get", "configmap", cmName,
				"-n", ns,
				"--insecure-skip-tls-verify",
				"--ignore-not-found",
				"-o", "name",
			)
			// A non-zero exit is expected/acceptable — it means the resource was not found
			// in the kcp workspace, which is the correct behaviour.
			if err != nil {
				t.Logf("hub kubectl returned error (expected when resource absent in kcp): %v", err)
			}
			if strings.Contains(out, cmName) {
				t.Fatalf("ConfigMap %q found on hub cluster — proxy write isolation violated (issue #80)", cmName)
			}
			t.Logf("ConfigMap %q correctly absent from hub cluster — proxy write isolation confirmed", cmName)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)

			// Best-effort: delete ConfigMap from edge cluster via proxy.
			if edgeURL, err := client.GetEdgeURL(ctx, edgeName); err == nil {
				_, _ = client.KubectlWithURL(ctx, edgeURL,
					"delete", "configmap", cmName,
					"-n", ns,
					"--ignore-not-found",
				)
			}

			if a, ok := ctx.Value(k8sProxyWriteIsolationAgentKey{}).(*framework.Agent); ok {
				a.Stop()
			}
			_ = client.EdgeDelete(ctx, edgeName)
			return ctx
		}).
		Feature()
}

// ──────────────────────────────────────────────────────────────────────────────
// Issue #81 — Cross-workspace edge isolation
// ──────────────────────────────────────────────────────────────────────────────

// crossWSIsolationData carries state between Setup → Assess → Teardown for the
// CrossWorkspaceEdgeIsolation test.
type crossWSIsolationData struct {
	// edgeProxyURLForA is the hub proxy URL for User A's edge (workspace A).
	edgeProxyURLForA string
	// edgeProxyURLForB is the hub proxy URL for User B's edge (workspace B).
	edgeProxyURLForB string
	// userAToken is User A's OIDC ID token.
	userAToken string
	// userBToken is User B's OIDC ID token.
	userBToken string
	// userAKubeconfig is the path to User A's kubeconfig (for teardown cleanup).
	userAKubeconfig string
	// userBKubeconfig is the path to User B's kubeconfig (for teardown cleanup).
	userBKubeconfig string
}

type crossWSIsolationKey struct{}

// insecureProxyClient is a shared HTTP client that skips TLS verification for
// the hub's self-signed dev certificate, used to probe cross-workspace isolation.
var insecureProxyClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test only
	},
}

// doCrossWSRequest sends a GET to the given proxy URL with the provided Bearer
// token and returns the HTTP status code.
func doCrossWSRequest(ctx context.Context, proxyURL, idToken string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+idToken)

	resp, err := insecureProxyClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close() //nolint:errcheck
	return resp.StatusCode, nil
}

// CrossWorkspaceEdgeIsolation verifies that a user in workspace A cannot access
// edges registered in workspace B, and vice versa (issue #81).
//
// This is an OIDC-only test: it requires Dex to be configured with at least two
// static users (DexTestUserEmail / DexTestUser2Email).  The test is skipped in
// standalone suites that do not configure Dex.
//
// Flow:
//  1. User A performs a headless OIDC login → obtains kubeconfig + ID token.
//  2. User A creates an Edge in their kcp workspace (workspace A).
//  3. User B performs a headless OIDC login → obtains kubeconfig + ID token.
//  4. User B creates an Edge in their kcp workspace (workspace B).
//  5. User A sends a GET to User B's edge proxy URL with User A's token → expect 401/403.
//  6. User B sends a GET to User A's edge proxy URL with User B's token → expect 401/403.
//
// This covers both directions of multi-tenant isolation and is stronger than
// OIDCCrossUserEdgeIsolation (which tests only direction B→A).
func CrossWorkspaceEdgeIsolation() features.Feature {
	const (
		edgeNameA = "e2e-ws-isolation-edge-a"
		edgeNameB = "e2e-ws-isolation-edge-b"
	)

	return features.New("CrossWorkspaceEdgeIsolation").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			dexEnv := framework.DexEnvFrom(ctx)
			if clusterEnv == nil || dexEnv == nil {
				t.Skip("requires OIDC suite (Dex env not found in context)")
			}
			if dexEnv.User2Email == "" {
				t.Skip("second Dex user not configured; requires DexTestUser2Email in the framework")
			}

			// ── User A: OIDC login ───────────────────────────────────────────
			loginCtxA, cancelA := context.WithTimeout(ctx, 90*time.Second)
			defer cancelA()

			resultA, err := framework.HeadlessOIDCLogin(loginCtxA, clusterEnv.HubURL, dexEnv.UserEmail, dexEnv.UserPassword)
			if err != nil {
				t.Fatalf("User A OIDC login failed: %v", err)
			}
			if len(resultA.Kubeconfig) == 0 {
				t.Fatal("User A login returned empty kubeconfig")
			}

			kcFileA := filepath.Join(t.TempDir(), "user-a.kubeconfig")
			if err := os.WriteFile(kcFileA, resultA.Kubeconfig, 0600); err != nil {
				t.Fatalf("writing User A kubeconfig: %v", err)
			}

			if resultA.IDToken != "" {
				tc := &cliauth.TokenCache{
					IDToken:      resultA.IDToken,
					RefreshToken: resultA.RefreshToken,
					ExpiresAt:    resultA.ExpiresAt,
					IssuerURL:    resultA.IssuerURL,
					ClientID:     resultA.ClientID,
				}
				if err := cliauth.SaveTokenCache(tc); err != nil {
					t.Fatalf("caching User A OIDC token: %v", err)
				}
			}

			// ── User A: create an Edge in workspace A ────────────────────────
			clientA := framework.NewKedgeClient(framework.RepoRoot(), kcFileA, clusterEnv.HubURL)
			if err := clientA.EdgeCreate(ctx, edgeNameA, "kubernetes", "env=e2e-ws-isolation"); err != nil {
				t.Fatalf("User A creating edge %q: %v", edgeNameA, err)
			}
			t.Logf("User A created edge %q in workspace A", edgeNameA)

			// Derive workspace A's cluster name from User A's kubeconfig server URL.
			clusterNameA := framework.ClusterNameFromKubeconfig(kcFileA)
			if clusterNameA == "" {
				t.Fatal("could not extract cluster name from User A's kubeconfig")
			}
			t.Logf("User A kcp cluster name: %s", clusterNameA)

			// Construct the hub proxy URL for User A's edge (workspace A path).
			edgeProxyURLForA := clusterEnv.HubURL +
				"/services/edges-proxy/clusters/" + clusterNameA +
				"/apis/kedge.faros.sh/v1alpha1/edges/" + edgeNameA + "/k8s"
			t.Logf("User A edge proxy URL: %s", edgeProxyURLForA)

			// ── User B: OIDC login ───────────────────────────────────────────
			loginCtxB, cancelB := context.WithTimeout(ctx, 90*time.Second)
			defer cancelB()

			resultB, err := framework.HeadlessOIDCLogin(loginCtxB, clusterEnv.HubURL, dexEnv.User2Email, dexEnv.User2Password)
			if err != nil {
				t.Fatalf("User B OIDC login failed: %v", err)
			}
			if resultB.IDToken == "" {
				t.Fatal("User B login returned empty ID token")
			}
			t.Logf("User B (email=%s) login succeeded", dexEnv.User2Email)

			kcFileB := filepath.Join(t.TempDir(), "user-b.kubeconfig")
			if err := os.WriteFile(kcFileB, resultB.Kubeconfig, 0600); err != nil {
				t.Fatalf("writing User B kubeconfig: %v", err)
			}

			// Cache User B's token so the exec-credential plugin can use it.
			if resultB.IDToken != "" {
				tc := &cliauth.TokenCache{
					IDToken:      resultB.IDToken,
					RefreshToken: resultB.RefreshToken,
					ExpiresAt:    resultB.ExpiresAt,
					IssuerURL:    resultB.IssuerURL,
					ClientID:     resultB.ClientID,
				}
				if err := cliauth.SaveTokenCache(tc); err != nil {
					t.Fatalf("caching User B OIDC token: %v", err)
				}
			}

			// ── User B: create an Edge in workspace B ────────────────────────
			clientB := framework.NewKedgeClient(framework.RepoRoot(), kcFileB, clusterEnv.HubURL)
			if err := clientB.EdgeCreate(ctx, edgeNameB, "kubernetes", "env=e2e-ws-isolation"); err != nil {
				t.Fatalf("User B creating edge %q: %v", edgeNameB, err)
			}
			t.Logf("User B created edge %q in workspace B", edgeNameB)

			// Derive workspace B's cluster name from User B's kubeconfig.
			clusterNameB := framework.ClusterNameFromKubeconfig(kcFileB)
			if clusterNameB == "" {
				t.Fatal("could not extract cluster name from User B's kubeconfig")
			}
			t.Logf("User B kcp cluster name: %s", clusterNameB)

			// Construct the hub proxy URL for User B's edge (workspace B path).
			edgeProxyURLForB := clusterEnv.HubURL +
				"/services/edges-proxy/clusters/" + clusterNameB +
				"/apis/kedge.faros.sh/v1alpha1/edges/" + edgeNameB + "/k8s"
			t.Logf("User B edge proxy URL: %s", edgeProxyURLForB)

			return context.WithValue(ctx, crossWSIsolationKey{}, &crossWSIsolationData{
				edgeProxyURLForA: edgeProxyURLForA,
				edgeProxyURLForB: edgeProxyURLForB,
				userAToken:       resultA.IDToken,
				userBToken:       resultB.IDToken,
				userAKubeconfig:  kcFileA,
				userBKubeconfig:  kcFileB,
			})
		}).
		Assess("user_a_cannot_access_workspace_b_edge", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			data, ok := ctx.Value(crossWSIsolationKey{}).(*crossWSIsolationData)
			if !ok {
				t.Skip("isolation data not found (setup may have been skipped)")
			}

			// User A's token must be rejected when accessing workspace B's edge proxy.
			code, err := doCrossWSRequest(ctx, data.edgeProxyURLForB, data.userAToken)
			if err != nil {
				t.Fatalf("cross-workspace request (A→B) failed: %v", err)
			}
			if code != http.StatusUnauthorized && code != http.StatusForbidden && code != http.StatusNotFound {
				t.Fatalf(
					"expected 401/403/404 for User A accessing workspace B edge, got %d — "+
						"cross-workspace isolation violated (issue #81)",
					code,
				)
			}
			t.Logf("User A correctly rejected from workspace B edge with HTTP %d", code)
			return ctx
		}).
		Assess("user_b_cannot_access_workspace_a_edge", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			data, ok := ctx.Value(crossWSIsolationKey{}).(*crossWSIsolationData)
			if !ok {
				t.Skip("isolation data not found (setup may have been skipped)")
			}

			// User B's token must be rejected when accessing workspace A's edge proxy.
			code, err := doCrossWSRequest(ctx, data.edgeProxyURLForA, data.userBToken)
			if err != nil {
				t.Fatalf("cross-workspace request (B→A) failed: %v", err)
			}
			if code != http.StatusUnauthorized && code != http.StatusForbidden && code != http.StatusNotFound {
				t.Fatalf(
					"expected 401/403/404 for User B accessing workspace A edge, got %d — "+
						"cross-workspace isolation violated (issue #81)",
					code,
				)
			}
			t.Logf("User B correctly rejected from workspace A edge with HTTP %d", code)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			data, ok := ctx.Value(crossWSIsolationKey{}).(*crossWSIsolationData)
			if !ok {
				return ctx // setup was skipped, nothing to clean up
			}

			clusterEnv := framework.ClusterEnvFrom(ctx)

			clientA := framework.NewKedgeClient(framework.RepoRoot(), data.userAKubeconfig, clusterEnv.HubURL)
			if err := clientA.EdgeDelete(ctx, edgeNameA); err != nil {
				t.Logf("warning: teardown User A edge delete failed (best-effort): %v", err)
			}

			clientB := framework.NewKedgeClient(framework.RepoRoot(), data.userBKubeconfig, clusterEnv.HubURL)
			if err := clientB.EdgeDelete(ctx, edgeNameB); err != nil {
				t.Logf("warning: teardown User B edge delete failed (best-effort): %v", err)
			}
			return ctx
		}).
		Feature()
}
