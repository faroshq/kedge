/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	ConditionReady     = "Ready"
	ConditionSynced    = "Synced"
	ConditionProcessUp = "ProcessUp"

	DevEnvironmentPhaseProvisioning = "Provisioning"
	DevEnvironmentPhaseRunning      = "Running"
	DevEnvironmentPhaseFailed       = "Failed"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=devenvironments,singular=devenvironment,scope=Cluster,categories=kedge,shortName=devenv
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.projectRef`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Preview",type=string,JSONPath=`.status.previewURL`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DevEnvironment requests a live development runtime for a Project.
type DevEnvironment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DevEnvironmentSpec   `json:"spec,omitempty"`
	Status DevEnvironmentStatus `json:"status,omitempty"`
}

// DevEnvironmentSpec defines the provider-owned live runtime contract.
type DevEnvironmentSpec struct {
	ProjectRef string                `json:"projectRef,omitempty"`
	Runtime    DevEnvironmentRuntime `json:"runtime,omitempty"`
	Sync       DevEnvironmentSync    `json:"sync,omitempty"`
}

// DevEnvironmentRuntime describes the first POC pod runner.
type DevEnvironmentRuntime struct {
	Image        string `json:"image,omitempty"`
	WorkingDir   string `json:"workingDir,omitempty"`
	StartCommand string `json:"startCommand,omitempty"`
	Port         int32  `json:"port,omitempty"`
}

// DevEnvironmentSync describes how App Studio should sync workspace changes.
type DevEnvironmentSync struct {
	Mode string `json:"mode,omitempty"`
}

// DevEnvironmentStatus reports runtime readiness and provider routes.
type DevEnvironmentStatus struct {
	Phase              string             `json:"phase,omitempty"`
	PreviewURL         string             `json:"previewURL,omitempty"`
	LastSyncTime       *metav1.Time       `json:"lastSyncTime,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DevEnvironmentList contains a list of DevEnvironments.
type DevEnvironmentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DevEnvironment `json:"items"`
}
