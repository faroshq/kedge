/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package kro

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	infrav1alpha1 "github.com/faroshq/faros-kedge/providers/infrastructure/apis/v1alpha1"
)

func TestOpenAPIToSimpleSchema(t *testing.T) {
	raw := []byte(`{
		"type": "object",
		"properties": {
			"name":       {"type": "string", "description": "logical name"},
			"size":       {"type": "string", "enum": ["small","medium","large"], "default": "small"},
			"replicas":   {"type": "integer", "default": 1, "minimum": 1, "maximum": 10},
			"persistent": {"type": "boolean", "default": false}
		},
		"required": ["name"]
	}`)

	got, err := openAPIToSimpleSchema(raw)
	if err != nil {
		t.Fatalf("openAPIToSimpleSchema: %v", err)
	}

	want := map[string]string{
		"name":       `string | required=true description="logical name"`,
		"size":       `string | enum="small,medium,large" default="small"`,
		"replicas":   `integer | default=1 minimum=1 maximum=10`,
		"persistent": `boolean | default=false`,
	}
	for field, exp := range want {
		gotStr, ok := got[field].(string)
		if !ok {
			t.Errorf("field %q: not a string leaf: %#v", field, got[field])
			continue
		}
		if gotStr != exp {
			t.Errorf("field %q:\n  got:  %s\n  want: %s", field, gotStr, exp)
		}
	}
}

func TestOpenAPIToSimpleSchemaNested(t *testing.T) {
	raw := []byte(`{
		"type": "object",
		"properties": {
			"tls": {"type": "object", "properties": {"enabled": {"type": "boolean", "default": true}}}
		}
	}`)
	got, err := openAPIToSimpleSchema(raw)
	if err != nil {
		t.Fatalf("openAPIToSimpleSchema: %v", err)
	}
	nested, ok := got["tls"].(map[string]any)
	if !ok {
		t.Fatalf("tls: expected nested map, got %#v", got["tls"])
	}
	if nested["enabled"] != `boolean | default=true` {
		t.Errorf("tls.enabled: got %v", nested["enabled"])
	}
}

func TestBuildRGD(t *testing.T) {
	tmpl := &infrav1alpha1.Template{}
	tmpl.Name = "redis-cache"
	tmpl.Spec.Version = "0.1.0"
	tmpl.Spec.InstanceCRD = infrav1alpha1.TemplateInstanceCRD{
		Group:    "infrastructure.kedge.faros.sh",
		Version:  "v1alpha1",
		Resource: "rediscaches",
		Kind:     "RedisCache",
	}
	tmpl.Spec.Schema = &runtime.RawExtension{Raw: []byte(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`)}
	tmpl.Spec.BackendConfig = &runtime.RawExtension{Raw: []byte(`{"resources":[{"id":"statefulset","template":{"apiVersion":"apps/v1","kind":"StatefulSet"}}]}`)}

	rgd, err := buildRGD(tmpl)
	if err != nil {
		t.Fatalf("buildRGD: %v", err)
	}

	if rgd.GetAPIVersion() != rgdAPIVersion || rgd.GetKind() != rgdKind {
		t.Errorf("GVK = %s/%s", rgd.GetAPIVersion(), rgd.GetKind())
	}
	if rgd.GetName() != "redis-cache" {
		t.Errorf("name = %q", rgd.GetName())
	}
	if lbl := rgd.GetLabels()["kedge.faros.sh/template"]; lbl != "redis-cache" {
		t.Errorf("template label = %q", lbl)
	}

	assertNested := func(want string, fields ...string) {
		got, found, err := unstructured.NestedString(rgd.Object, fields...)
		if err != nil || !found {
			t.Errorf("%v: not found (err=%v)", fields, err)
			return
		}
		if got != want {
			t.Errorf("%v = %q, want %q", fields, got, want)
		}
	}
	assertNested("v1alpha1", "spec", "schema", "apiVersion")
	assertNested("infrastructure.kedge.faros.sh", "spec", "schema", "group")
	assertNested("RedisCache", "spec", "schema", "kind")
	assertNested("Cluster", "spec", "schema", "scope")

	resources, found, err := unstructured.NestedSlice(rgd.Object, "spec", "resources")
	if err != nil || !found || len(resources) != 1 {
		t.Fatalf("spec.resources: found=%v len=%d err=%v", found, len(resources), err)
	}
}

func TestBuildRGDRequiresBackendConfig(t *testing.T) {
	tmpl := &infrav1alpha1.Template{}
	tmpl.Name = "no-config"
	tmpl.Spec.InstanceCRD = infrav1alpha1.TemplateInstanceCRD{Group: "g", Version: "v1alpha1", Resource: "rs", Kind: "R"}
	tmpl.Spec.Schema = &runtime.RawExtension{Raw: []byte(`{"type":"object","properties":{"name":{"type":"string"}}}`)}
	// no BackendConfig
	if _, err := buildRGD(tmpl); err == nil {
		t.Fatal("expected error when backendConfig is missing")
	}
}

func TestBuildRGDFromCloudRunSeedTemplate(t *testing.T) {
	tmpl := loadSeedTemplate(t, "../../install/templates/gcp-cloud-run-service.yaml")
	rgd, err := buildRGD(tmpl)
	if err != nil {
		t.Fatalf("buildRGD: %v", err)
	}

	assertNested := func(want string, fields ...string) {
		got, found, err := unstructured.NestedString(rgd.Object, fields...)
		if err != nil || !found {
			t.Fatalf("%v: not found (err=%v)", fields, err)
		}
		if got != want {
			t.Fatalf("%v = %q, want %q", fields, got, want)
		}
	}
	assertNested("CloudRunService", "spec", "schema", "kind")
	assertNested("infrastructure.kedge.faros.sh", "spec", "schema", "group")

	resources, found, err := unstructured.NestedSlice(rgd.Object, "spec", "resources")
	if err != nil || !found {
		t.Fatalf("spec.resources: found=%v err=%v", found, err)
	}
	if len(resources) != 2 {
		t.Fatalf("resources len = %d, want 2", len(resources))
	}

	kinds := map[string]bool{}
	for _, res := range resources {
		rm, ok := res.(map[string]any)
		if !ok {
			t.Fatalf("resource is %T, want map", res)
		}
		tm, ok := rm["template"].(map[string]any)
		if !ok {
			t.Fatalf("template is %T, want map", rm["template"])
		}
		apiVersion, _ := tm["apiVersion"].(string)
		kind, _ := tm["kind"].(string)
		kinds[apiVersion+"/"+kind] = true
	}
	if !kinds["run.cnrm.cloud.google.com/v1beta1/RunService"] {
		t.Fatalf("missing RunService resource: %v", kinds)
	}
	if !kinds["iam.cnrm.cloud.google.com/v1beta1/IAMPolicyMember"] {
		t.Fatalf("missing IAMPolicyMember resource: %v", kinds)
	}
	assertNested("${service.status.url}", "spec", "schema", "status", "url")
}

func loadSeedTemplate(t *testing.T, path string) *infrav1alpha1.Template {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seed template: %v", err)
	}
	var obj map[string]any
	if err := utilyaml.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal seed template: %v", err)
	}
	name, _, _ := unstructured.NestedString(obj, "metadata", "name")
	if name == "" {
		t.Fatal("seed template missing metadata.name")
	}
	schemaRaw, err := marshalNested(obj, "spec", "schema")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	backendConfigRaw, err := marshalNested(obj, "spec", "backendConfig")
	if err != nil {
		t.Fatalf("backendConfig: %v", err)
	}
	group, _, _ := unstructured.NestedString(obj, "spec", "instanceCRD", "group")
	version, _, _ := unstructured.NestedString(obj, "spec", "instanceCRD", "version")
	resource, _, _ := unstructured.NestedString(obj, "spec", "instanceCRD", "resource")
	kind, _, _ := unstructured.NestedString(obj, "spec", "instanceCRD", "kind")
	templateVersion, _, _ := unstructured.NestedString(obj, "spec", "version")

	return &infrav1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: infrav1alpha1.TemplateSpec{
			Version: templateVersion,
			InstanceCRD: infrav1alpha1.TemplateInstanceCRD{
				Group:    group,
				Version:  version,
				Resource: resource,
				Kind:     kind,
			},
			Schema:        &runtime.RawExtension{Raw: schemaRaw},
			BackendConfig: &runtime.RawExtension{Raw: backendConfigRaw},
		},
	}
}

func marshalNested(obj map[string]any, fields ...string) ([]byte, error) {
	nested, found, err := unstructured.NestedFieldNoCopy(obj, fields...)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("missing %v", fields)
	}
	return json.Marshal(nested)
}
