/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package install

import (
	"io/fs"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestEmbeddedSeedTemplatesAreValid(t *testing.T) {
	entries, err := fs.ReadDir(seedTemplatesFS, "templates")
	if err != nil {
		t.Fatalf("read templates: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one embedded template")
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			raw, err := fs.ReadFile(seedTemplatesFS, "templates/"+entry.Name())
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var obj map[string]any
			if err := utilyaml.Unmarshal(raw, &obj); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			name, _, _ := unstructured.NestedString(obj, "metadata", "name")
			if name == "" {
				t.Fatal("metadata.name is required")
			}
			for _, field := range []string{"version", "backend", "category", "cloud"} {
				got, _, _ := unstructured.NestedString(obj, "spec", field)
				if got == "" {
					t.Fatalf("spec.%s is required for seed templates", field)
				}
			}
			resources, found, err := unstructured.NestedSlice(obj, "spec", "backendConfig", "resources")
			if err != nil {
				t.Fatalf("spec.backendConfig.resources: %v", err)
			}
			if !found || len(resources) == 0 {
				t.Fatal("spec.backendConfig.resources must be non-empty")
			}
		})
	}
}
