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
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/faroshq/faros-kedge/test/e2e/framework"
)

// edgeURLAgentKey is a context key for the Agent in EdgeURLSet / K8sProxyAccess.
type edgeURLAgentKey struct{}

// EdgeURLSet verifies that status.URL is populated after a kubernetes-type edge
// connects and that the URL ends in "/k8s".
func EdgeURLSet() features.Feature {
	const edgeName = "e2e-url-edge"

	return features.New("edge URL set").
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
			return context.WithValue(ctx, edgeURLAgentKey{}, agent)
		}).
		Assess("edge becomes Ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)

			if err := client.WaitForEdgeReady(ctx, edgeName, 3*time.Minute); err != nil {
				t.Fatalf("edge %q did not become Ready: %v", edgeName, err)
			}
			return ctx
		}).
		Assess("status.URL is populated and ends with /k8s", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
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
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			if a, ok := ctx.Value(edgeURLAgentKey{}).(*framework.Agent); ok {
				a.Stop()
			}
			clusterEnv := framework.ClusterEnvFrom(ctx)
			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)
			_ = client.EdgeDelete(ctx, edgeName)
			return ctx
		}).
		Feature()
}

// k8sProxyAgentKey is a context key for the Agent in K8sProxyAccess.
type k8sProxyAgentKey struct{}

// K8sProxyAccess verifies that kubectl against status.URL returns the edge
// cluster's resources (nodes and namespaces).  This is an end-to-end test of
// the k8s proxy path through the hub.
//
// Only use this in suites that set up both a hub and at least one agent cluster.
func K8sProxyAccess() features.Feature {
	const edgeName = "e2e-proxy-edge"

	return features.New("k8s proxy access").
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
			return context.WithValue(ctx, k8sProxyAgentKey{}, agent)
		}).
		Assess("edge becomes Ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)

			if err := client.WaitForEdgeReady(ctx, edgeName, 3*time.Minute); err != nil {
				t.Fatalf("edge %q did not become Ready: %v", edgeName, err)
			}
			return ctx
		}).
		Assess("kubectl get nodes via status.URL returns edge cluster nodes", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)

			edgeURL, err := client.GetEdgeURL(ctx, edgeName)
			if err != nil {
				t.Fatalf("getting edge URL: %v", err)
			}
			if !strings.HasSuffix(edgeURL, "/k8s") {
				t.Fatalf("expected edge URL to end with '/k8s', got: %s", edgeURL)
			}

			out, err := client.KubectlWithURL(ctx, edgeURL, "get", "nodes")
			if err != nil {
				t.Fatalf("kubectl get nodes via edge proxy failed: %v", err)
			}
			if out == "" {
				t.Fatalf("expected non-empty node list via edge proxy, got empty output")
			}
			t.Logf("kubectl get nodes via edge proxy:\n%s", out)
			return ctx
		}).
		Assess("kubectl get namespaces via status.URL returns default namespace", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterEnv := framework.ClusterEnvFrom(ctx)
			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)

			edgeURL, err := client.GetEdgeURL(ctx, edgeName)
			if err != nil {
				t.Fatalf("getting edge URL: %v", err)
			}

			out, err := client.KubectlWithURL(ctx, edgeURL, "get", "namespaces")
			if err != nil {
				t.Fatalf("kubectl get namespaces via edge proxy failed: %v", err)
			}
			if !strings.Contains(out, "default") {
				t.Fatalf("expected 'default' namespace in proxy output, got:\n%s", out)
			}
			t.Logf("kubectl get namespaces via edge proxy:\n%s", out)
			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			if a, ok := ctx.Value(k8sProxyAgentKey{}).(*framework.Agent); ok {
				a.Stop()
			}
			clusterEnv := framework.ClusterEnvFrom(ctx)
			client := framework.NewKedgeClient(framework.RepoRoot(), clusterEnv.HubKubeconfig, clusterEnv.HubURL)
			_ = client.EdgeDelete(ctx, edgeName)
			return ctx
		}).
		Feature()
}
