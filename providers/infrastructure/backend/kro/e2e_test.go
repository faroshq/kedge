//go:build e2e

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

// E2E coverage for the seeded infrastructure Templates against a real kro
// cluster. Build-tagged `e2e` so it never runs in the normal `go test ./...`
// unit pass — it needs a running kro controller (see `make e2e-infrastructure`).
//
// What it proves: every Template the provider ships authors a kro
// ResourceGraphDefinition that kro ACCEPTS (status GraphAccepted=True). This is
// the exact class of failure that unit tests (which only check buildRGD output)
// miss — e.g. an integer field fed a string, an includeWhen that isn't a
// standalone CEL expression, or a container image that resolved to "". kro's
// graph/schema validation is independent of its watch source, so this works
// against any kro cluster (standalone kind in CI, or a kcp-wired dev cluster).
package kro

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	e2eRGDPrefix  = "e2e-"
	e2eAcceptWait = 90 * time.Second
	e2ePollEvery  = 2 * time.Second
)

// TestE2ESeedTemplatesAcceptedByKro builds each seeded Template's RGD with the
// real buildRGD path and asserts kro accepts the graph.
func TestE2ESeedTemplatesAcceptedByKro(t *testing.T) {
	dyn := e2eDynamicClient(t)
	dir := filepath.Join("..", "..", "install", "templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read seed templates dir: %v", err)
	}

	var seen int
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		seen++
		t.Run(entry.Name(), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", entry.Name(), err)
			}
			tmpl := decodeTemplate(t, raw)
			rgd, err := buildRGD(tmpl, testTokens())
			if err != nil {
				t.Fatalf("buildRGD(%s): %v", tmpl.Name, err)
			}
			// Apply under a throwaway name so the test doesn't clobber any
			// RGDs a dev cluster already seeded; GraphAccepted is name-
			// independent. The per-template instance CRD it would create may
			// collide with a seeded one (KindReady), which is fine — we only
			// assert the graph is accepted.
			name := e2eRGDPrefix + tmpl.Name
			rgd.SetName(name)
			applyRGDForTest(t, dyn, rgd)
			t.Cleanup(func() { deleteRGDForTest(dyn, name) })

			reason, msg := waitGraphAccepted(t, dyn, name)
			if reason != "True" {
				t.Fatalf("kro rejected RGD for template %q: GraphAccepted=%s: %s", tmpl.Name, reason, msg)
			}
			t.Logf("template %q: kro accepted the RGD", tmpl.Name)
		})
	}
	if seen == 0 {
		t.Fatal("no seed templates found")
	}
}

func e2eDynamicClient(t *testing.T) dynamic.Interface {
	t.Helper()
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		t.Skip("KUBECONFIG not set; this e2e needs a kro cluster (see make e2e-infrastructure)")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("build rest config from %s: %v", kubeconfig, err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("dynamic client: %v", err)
	}
	return dyn
}

func applyRGDForTest(t *testing.T, dyn dynamic.Interface, rgd *unstructured.Unstructured) {
	t.Helper()
	ctx := context.Background()
	existing, err := dyn.Resource(rgdGVR).Get(ctx, rgd.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err := dyn.Resource(rgdGVR).Create(ctx, rgd, metav1.CreateOptions{}); err != nil {
			t.Fatalf("create RGD %q: %v", rgd.GetName(), err)
		}
		return
	}
	if err != nil {
		t.Fatalf("get RGD %q: %v", rgd.GetName(), err)
	}
	rgd.SetResourceVersion(existing.GetResourceVersion())
	if _, err := dyn.Resource(rgdGVR).Update(ctx, rgd, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("update RGD %q: %v", rgd.GetName(), err)
	}
}

func deleteRGDForTest(dyn dynamic.Interface, name string) {
	_ = dyn.Resource(rgdGVR).Delete(context.Background(), name, metav1.DeleteOptions{})
}

// waitGraphAccepted polls the RGD until its GraphAccepted condition resolves to
// True or False (Unknown/AwaitingReconciliation keeps waiting), returning the
// final status + message.
func waitGraphAccepted(t *testing.T, dyn dynamic.Interface, name string) (string, string) {
	t.Helper()
	deadline := time.Now().Add(e2eAcceptWait)
	var lastStatus, lastMsg string
	for time.Now().Before(deadline) {
		obj, err := dyn.Resource(rgdGVR).Get(context.Background(), name, metav1.GetOptions{})
		if err == nil {
			lastStatus, lastMsg = graphAcceptedCondition(obj)
			if lastStatus == "True" || lastStatus == "False" {
				return lastStatus, lastMsg
			}
		}
		time.Sleep(e2ePollEvery)
	}
	t.Fatalf("timed out after %s waiting for RGD %q GraphAccepted (last=%q: %s)", e2eAcceptWait, name, lastStatus, lastMsg)
	return lastStatus, lastMsg
}

func graphAcceptedCondition(obj *unstructured.Unstructured) (string, string) {
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conds {
		cond, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if t, _, _ := unstructured.NestedString(cond, "type"); t != "GraphAccepted" {
			continue
		}
		status, _, _ := unstructured.NestedString(cond, "status")
		msg, _, _ := unstructured.NestedString(cond, "message")
		return status, msg
	}
	return "", ""
}
