/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTemplateCloudJSONRoundTrip(t *testing.T) {
	tmpl := Template{
		ObjectMeta: metav1.ObjectMeta{Name: "cloud-run"},
		Spec: TemplateSpec{
			DisplayName: "Cloud Run",
			Category:    "Serverless",
			Cloud:       "gcp",
			Version:     "0.1.0",
			Backend:     "kro",
			InstanceCRD: TemplateInstanceCRD{
				Group:    GroupName,
				Version:  Version,
				Resource: "cloudrunservices",
				Kind:     "CloudRunService",
			},
		},
	}

	data, err := json.Marshal(tmpl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got Template
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Spec.Cloud != "gcp" {
		t.Fatalf("cloud = %q, want gcp", got.Spec.Cloud)
	}
}
