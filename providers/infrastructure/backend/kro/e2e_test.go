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

// E2E coverage for the seeded infrastructure Templates against a real, CLEAN kro
// cluster (one fresh cluster per run — see `make e2e-infrastructure`). Build-
// tagged `e2e` so it never runs in the normal `go test ./...` unit pass.
//
// For every Template the provider ships it proves two things unit tests can't
// (they only check buildRGD's output):
//
//  1. kro ACCEPTS the authored ResourceGraphDefinition (status GraphAccepted=
//     True) and establishes the per-template instance CRD. Catches malformed
//     graphs — an integer field fed a string, an includeWhen that isn't a
//     standalone CEL expression, etc.
//  2. kro can CREATE an instance's objects: a sample instance reconciles WITHOUT
//     an apply error. This is the exact failure that motivated the schema-default
//     image convention ("apply results contain errors: ... image: Required
//     value"). It does NOT require the images to pull — apply validates the spec.
package kro

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"

	infrav1alpha1 "github.com/faroshq/provider-infrastructure/apis/v1alpha1"
)

const (
	e2eGraphWait    = 90 * time.Second
	e2eCRDWait      = 60 * time.Second
	e2eInstanceWait = 120 * time.Second
	e2ePollEvery    = 2 * time.Second
)

// e2eMinimalSpecs supplies a valid instance spec for templates that ship no
// sampleValues (those that do — application, database — use them directly).
var e2eMinimalSpecs = map[string]map[string]any{
	"redis-cache":    {"name": "e2e-redis"},
	"simple-webapp":  {"name": "e2e-web"},
	"sandbox-runner": {"name": "kedge-sandbox-0000111122223333", "projectRef": "e2e"},
}

// e2eApplyErrorMarkers are substrings kro puts in an instance condition when it
// FAILS to apply a child resource (the bug class we guard against). Readiness
// waits (pods not up because an image can't pull in CI) do not contain these.
var e2eApplyErrorMarkers = []string{
	"apply results contain errors",
	"is invalid",
	"Required value",
	"failed to apply",
	"admission webhook",
}

func TestE2ESeedTemplates(t *testing.T) {
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
			applyRGD(t, dyn, rgd)
			t.Cleanup(func() { _ = dyn.Resource(rgdGVR).Delete(context.Background(), rgd.GetName(), metav1.DeleteOptions{}) })

			// 1. kro accepts the graph.
			if status, msg := waitGraphAccepted(t, dyn, rgd.GetName()); status != "True" {
				t.Fatalf("kro rejected RGD for template %q: GraphAccepted=%s: %s", tmpl.Name, status, msg)
			}
			t.Logf("template %q: RGD accepted", tmpl.Name)

			// 2. kro creates an instance's objects without an apply error.
			instGVR := schema.GroupVersionResource{
				Group:    tmpl.Spec.InstanceCRD.Group,
				Version:  tmpl.Spec.InstanceCRD.Version,
				Resource: tmpl.Spec.InstanceCRD.Resource,
			}
			inst := e2eInstance(t, tmpl)
			createInstance(t, dyn, instGVR, inst)
			t.Cleanup(func() {
				_ = dyn.Resource(instGVR).Delete(context.Background(), inst.GetName(), metav1.DeleteOptions{})
			})

			waitInstanceApplied(t, dyn, instGVR, inst.GetName(), tmpl.Name)
			t.Logf("template %q: instance reconciled (objects applied)", tmpl.Name)
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
		t.Skip("KUBECONFIG not set; this e2e needs a clean kro cluster (see make e2e-infrastructure)")
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

func applyRGD(t *testing.T, dyn dynamic.Interface, rgd *unstructured.Unstructured) {
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

// e2eInstance builds a sample instance: the template's sampleValues when present
// (the curated working example), otherwise a minimal valid spec.
func e2eInstance(t *testing.T, tmpl *infrav1alpha1.Template) *unstructured.Unstructured {
	t.Helper()
	spec := map[string]any{}
	if sv := tmpl.Spec.SampleValues; sv != nil && len(sv.Raw) > 0 {
		if err := json.Unmarshal(sv.Raw, &spec); err != nil {
			t.Fatalf("template %q: decode sampleValues: %v", tmpl.Name, err)
		}
	} else if min, ok := e2eMinimalSpecs[tmpl.Name]; ok {
		spec = min
	} else {
		t.Fatalf("template %q has no sampleValues and no e2eMinimalSpecs entry — add one", tmpl.Name)
	}
	name, _ := spec["name"].(string)
	if name == "" {
		name = "e2e-" + tmpl.Name
		spec["name"] = name
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": tmpl.Spec.InstanceCRD.Group + "/" + tmpl.Spec.InstanceCRD.Version,
		"kind":       tmpl.Spec.InstanceCRD.Kind,
		"metadata":   map[string]any{"name": name},
		"spec":       spec,
	}}
}

// createInstance retries Create until kro has established + served the
// per-template CRD (a fresh RGD takes a few seconds to register the kind).
func createInstance(t *testing.T, dyn dynamic.Interface, gvr schema.GroupVersionResource, inst *unstructured.Unstructured) {
	t.Helper()
	deadline := time.Now().Add(e2eCRDWait)
	for {
		_, err := dyn.Resource(gvr).Create(context.Background(), inst, metav1.CreateOptions{})
		if err == nil || apierrors.IsAlreadyExists(err) {
			return
		}
		// "no matches for kind" / NotFound while the CRD is still registering.
		if time.Now().After(deadline) {
			t.Fatalf("create %s instance %q: CRD never became servable: %v", gvr.Resource, inst.GetName(), err)
		}
		time.Sleep(e2ePollEvery)
	}
}

// waitInstanceApplied waits until kro has reconciled the instance and asserts it
// applied its objects without an apply error. A readiness wait (images not
// pulled in CI) is success — apply succeeded. An apply-error marker is failure.
func waitInstanceApplied(t *testing.T, dyn dynamic.Interface, gvr schema.GroupVersionResource, name, tmplName string) {
	t.Helper()
	deadline := time.Now().Add(e2eInstanceWait)
	var sawConditions bool
	for time.Now().Before(deadline) {
		obj, err := dyn.Resource(gvr).Get(context.Background(), name, metav1.GetOptions{})
		if err != nil {
			time.Sleep(e2ePollEvery)
			continue
		}
		conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
		for _, c := range conds {
			cond, ok := c.(map[string]any)
			if !ok {
				continue
			}
			sawConditions = true
			msg, _, _ := unstructured.NestedString(cond, "message")
			for _, marker := range e2eApplyErrorMarkers {
				if strings.Contains(msg, marker) {
					ctype, _, _ := unstructured.NestedString(cond, "type")
					t.Fatalf("template %q: kro failed to apply instance objects (%s): %s", tmplName, ctype, msg)
				}
			}
		}
		// kro reconciled it and recorded conditions, none of which are apply
		// errors → the objects were applied. (Readiness is out of scope: the
		// images may be unpullable in CI.)
		if sawConditions {
			return
		}
		time.Sleep(e2ePollEvery)
	}
	if !sawConditions {
		t.Fatalf("template %q: kro never reconciled instance %q within %s", tmplName, name, e2eInstanceWait)
	}
}

func waitGraphAccepted(t *testing.T, dyn dynamic.Interface, name string) (string, string) {
	t.Helper()
	deadline := time.Now().Add(e2eGraphWait)
	var lastStatus, lastMsg string
	for time.Now().Before(deadline) {
		obj, err := dyn.Resource(rgdGVR).Get(context.Background(), name, metav1.GetOptions{})
		if err == nil {
			lastStatus, lastMsg = conditionByType(obj, "GraphAccepted")
			if lastStatus == "True" || lastStatus == "False" {
				return lastStatus, lastMsg
			}
		}
		time.Sleep(e2ePollEvery)
	}
	t.Fatalf("timed out after %s waiting for RGD %q GraphAccepted (last=%q: %s)", e2eGraphWait, name, lastStatus, lastMsg)
	return lastStatus, lastMsg
}

func conditionByType(obj *unstructured.Unstructured, condType string) (string, string) {
	conds, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conds {
		cond, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if tp, _, _ := unstructured.NestedString(cond, "type"); tp != condType {
			continue
		}
		status, _, _ := unstructured.NestedString(cond, "status")
		msg, _, _ := unstructured.NestedString(cond, "message")
		return status, msg
	}
	return "", ""
}
