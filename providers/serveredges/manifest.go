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

// Package serveredges is the first-party provider that surfaces Linux
// server edges connected over SSH as a portal tab. Bootstraps a
// CatalogEntry into root:kedge:providers via init(); the actual SSH
// controllers live in pkg/hub/controllers/linuxmcp and related
// packages. The portal route /servers reuses EdgesPage.vue with a
// kind=server prop so the list filters down to server edges and the
// Create dialog locks the type to server.
//
// Imported for its side effect (RegisterBuiltin) from
// cmd/kedge-hub/main.go.
package serveredges

import (
	"github.com/faroshq/faros-kedge/pkg/apiurl"
	"github.com/faroshq/faros-kedge/pkg/hub/providers"
	seVirtual "github.com/faroshq/faros-kedge/providers/serveredges/virtual"
)

func init() {
	providers.RegisterBuiltin(providers.BuiltinSpec{
		Name:        "server-edges",
		DisplayName: "Servers",
		Description: "Manage Linux server edges connected over SSH.",
		Category:    "Edges",
		// No BuiltinRoute — the portal loads this provider through
		// ProviderFrame which fetches /ui/providers/server-edges/main.js.
		// The script defines <kedge-provider-server-edges>; rendered
		// inline by the host portal in light DOM.

		VirtualWorkspaceMount:   apiurl.PathPrefixLinuxMCP,
		VirtualWorkspaceHandler: seVirtual.Build,

		LocalUIAssets: localUIAssets(),
	})
}
