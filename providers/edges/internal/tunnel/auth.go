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

package tunnel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	authenticationv1 "k8s.io/api/authentication/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// saTokenClaims holds the claims extracted from a kcp ServiceAccount JWT.
type saTokenClaims struct {
	Issuer      string `json:"iss"`
	ClusterName string `json:"kubernetes.io/serviceaccount/clusterName"`
}

// parseServiceAccountToken decodes a JWT without signature verification and
// checks whether it is a kcp ServiceAccount token. kcp verifies the actual
// signature when the request is forwarded.
func parseServiceAccountToken(token string) (saTokenClaims, bool) {
	if token == "" {
		return saTokenClaims{}, false
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return saTokenClaims{}, false
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return saTokenClaims{}, false
	}

	var claims saTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return saTokenClaims{}, false
	}

	if claims.Issuer != "kubernetes/serviceaccount" || claims.ClusterName == "" {
		return saTokenClaims{}, false
	}

	return claims, true
}

// extractBearerToken extracts the bearer token from the Authorization header
// or, as a fallback, the "token" query parameter.  The query-parameter path
// exists because browsers cannot set headers on WebSocket connections.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Fallback for WebSocket upgrades from the browser terminal.
	if t := r.URL.Query().Get("token"); t != "" {
		return t
	}
	return ""
}

// authorize performs delegated authentication and authorization against the
// consumer workspace, following kcp's standard auth-delegator pattern:
//  1. TokenReview — authenticates the bearer token and extracts user identity.
//  2. SubjectAccessReview — checks whether that identity may perform verb on
//     the resource in the consumer workspace.
//
// tenantCfg MUST already target the consumer workspace through the provider's
// APIExport virtual workspace (see Server.tenantConfigFor). kcp serves both
// review APIs on that VW, scoped to the engaged cluster (kcp#4279 / kcp#4280),
// which is what the edges APIExport claims tokenreviews + subjectaccessreviews
// for. Because the review runs IN the consumer workspace, this handles both
// end-user OIDC tokens and ServiceAccount tokens the provider minted there (the
// agent credentials) with no home-cluster juggling or synthetic identity — the
// token authenticates natively where it was issued, and the resolved identity
// is authorized against that workspace's own RBAC.
//
// This replaces the earlier approach of re-rooting the provider's own
// workspace-scoped credential at /clusters/<consumer>, which the production hub
// proxy rejects with an opaque 404 ("the server could not find the requested
// resource") — the failure kcp#4279 documents.
func authorize(ctx context.Context, tenantCfg *rest.Config, token, verb, group, resource, name string) error {
	client, err := kubernetes.NewForConfig(tenantCfg)
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}

	// 1. Authenticate the token in the consumer workspace.
	tr, err := client.AuthenticationV1().TokenReviews().Create(ctx, &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{Token: token},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("token review: %w", err)
	}
	if !tr.Status.Authenticated {
		return fmt.Errorf("token not authenticated")
	}

	// 2. Authorize the resolved identity against the consumer workspace's RBAC.
	sar, err := client.AuthorizationV1().SubjectAccessReviews().Create(ctx, &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   tr.Status.User.Username,
			Groups: tr.Status.User.Groups,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Verb:     verb,
				Group:    group,
				Version:  "v1alpha1",
				Resource: resource,
				Name:     name,
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("subject access review: %w", err)
	}
	if !sar.Status.Allowed {
		return fmt.Errorf("access denied: %s", sar.Status.Reason)
	}

	return nil
}
