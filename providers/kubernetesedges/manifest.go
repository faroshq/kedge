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

// Package kubernetesedges is the first-party provider that surfaces
// connected Kubernetes cluster edges as a portal tab. Bootstraps a
// CatalogEntry into root:kedge:providers via init(); the actual edge
// controllers + Vue pages live in pkg/hub/controllers/edge and
// portal/src/pages/EdgesPage.vue respectively.
//
// Imported for its side effect (RegisterBuiltin) from
// cmd/kedge-hub/main.go.
package kubernetesedges

import "github.com/faroshq/faros-kedge/pkg/hub/providers"

func init() {
	providers.RegisterBuiltin(providers.BuiltinSpec{
		Name:         "kubernetes-edges",
		DisplayName:  "Kubernetes",
		Description:  "Manage connected Kubernetes cluster edges.",
		Category:     "Edges",
		BuiltinRoute: "edges",
		// Workloads is conceptually a feature OF Kubernetes edges
		// (you deploy workloads to clusters), so it nests under this
		// provider rather than standing alone in the top-level nav.
		Children: []providers.BuiltinChild{
			{DisplayName: "Workloads", BuiltinRoute: "workloads"},
		},
	})
}
