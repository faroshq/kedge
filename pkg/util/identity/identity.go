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

// Package identity defines the synthetic identity encoding kedge uses when a
// kcp ServiceAccount from one logical cluster is authorized in another.
//
// kcp ServiceAccount identities are logical-cluster-scoped: the username
// "system:serviceaccount:default:provider" means a DIFFERENT principal in
// every workspace, so binding RBAC to it in a tenant workspace would also
// match a same-named SA the tenant created themselves. Kedge therefore
// qualifies foreign SAs with their verified home cluster before running a
// SubjectAccessReview, and grants (e.g. the provider edges-proxy grant
// created on tenant Enable) bind the qualified form.
//
// Both sides — the SAR caller in pkg/virtual/builder and the grant writer in
// pkg/hub/kcp — MUST use these helpers so the encodings cannot drift.
package identity

import "strings"

// qualifiedSAPrefix deliberately does not collide with any kcp- or
// kubernetes-issued username prefix ("system:serviceaccount:",
// "system:kcp:", ...) so the qualified form can never be minted by an
// authenticator — only synthesized by kedge after a successful TokenReview.
const qualifiedSAPrefix = "system:kedge:foreign-sa:"

// QualifyServiceAccount converts a TokenReview-resolved ServiceAccount
// username ("system:serviceaccount:{ns}:{name}") plus its verified home
// logical cluster into the qualified form
// "system:kedge:foreign-sa:{homeCluster}:{ns}:{name}".
//
// Returns false when saUsername is not a ServiceAccount username.
func QualifyServiceAccount(homeCluster, saUsername string) (string, bool) {
	rest, ok := strings.CutPrefix(saUsername, "system:serviceaccount:")
	if !ok || homeCluster == "" || rest == "" {
		return "", false
	}
	return qualifiedSAPrefix + homeCluster + ":" + rest, true
}

// QualifiedServiceAccount builds the qualified username from parts. Used by
// the grant writer, which knows the SA's coordinates rather than holding a
// TokenReview result.
func QualifiedServiceAccount(homeCluster, namespace, name string) string {
	return qualifiedSAPrefix + homeCluster + ":" + namespace + ":" + name
}
