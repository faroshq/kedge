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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServerPhase describes the lifecycle phase of a Server.
type ServerPhase string

const (
	ServerPhaseConnected    ServerPhase = "Connected"
	ServerPhaseDisconnected ServerPhase = "Disconnected"
	ServerPhaseReady        ServerPhase = "Ready"
	ServerPhaseNotReady     ServerPhase = "NotReady"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Hostname",type="string",JSONPath=".spec.hostname"
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider"
// +kubebuilder:printcolumn:name="Region",type="string",JSONPath=".spec.region"
// +kubebuilder:printcolumn:name="Connected",type="boolean",JSONPath=".status.tunnelConnected"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Server represents a non-Kubernetes host (bare metal, VM, or systemd service)
// connected to the hub via a reverse WebSocket tunnel. SSH access is proxied
// through the tunnel to the host's sshd on localhost:22.
type Server struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ServerSpec   `json:"spec,omitempty"`
	Status            ServerStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ServerList is a list of Server resources.
type ServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Server `json:"items"`
}

// ServerSpec defines the desired state of a Server.
type ServerSpec struct {
	DisplayName string `json:"displayName,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	Provider    string `json:"provider,omitempty"` // aws, gcp, onprem, bare-metal
	Region      string `json:"region,omitempty"`
}

// ServerStatus defines the observed state of a Server.
type ServerStatus struct {
	Phase             ServerPhase        `json:"phase"`
	LastHeartbeatTime *metav1.Time       `json:"lastHeartbeatTime,omitempty"`
	TunnelConnected   bool               `json:"tunnelConnected"`
	SSHEnabled        bool               `json:"sshEnabled"`
	Conditions        []metav1.Condition `json:"conditions,omitempty"`
}
