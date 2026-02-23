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

package builder

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

	"github.com/faroshq/faros-kedge/pkg/hub/kcp"
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

// extractBearerToken extracts the bearer token from the Authorization header.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

// authorize performs delegated authentication and authorization against kcp.
// It follows the same pattern as kcp-dev/kcp's delegated.NewDelegatedAuthorizer:
//  1. TokenReview — authenticates the bearer token and extracts user identity.
//  2. SubjectAccessReview — checks if the authenticated user is allowed to
//     perform the given verb on the resource in the target workspace.
//
// Both calls use admin credentials scoped to the target workspace.
func authorize(ctx context.Context, kcpConfig *rest.Config, token, clusterName, verb, resource, name string) error {
	cfg := rest.CopyConfig(kcpConfig)
	cfg.Host = kcp.AppendClusterPath(cfg.Host, clusterName)

	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}

	// 1. Authenticate: TokenReview with admin creds.
	tr, err := client.AuthenticationV1().TokenReviews().Create(ctx, &authenticationv1.TokenReview{
		Spec: authenticationv1.TokenReviewSpec{Token: token},
	}, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("token review: %w", err)
	}
	if !tr.Status.Authenticated {
		return fmt.Errorf("token not authenticated")
	}

	// 2. Authorize: SubjectAccessReview with the resolved identity.
	sar, err := client.AuthorizationV1().SubjectAccessReviews().Create(ctx, &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   tr.Status.User.Username,
			Groups: tr.Status.User.Groups,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Verb:     verb,
				Group:    "kedge.faros.sh",
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

// oidcTokenClaims holds standard claims from an OIDC ID token.
type oidcTokenClaims struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
}

// parseOIDCToken decodes the JWT payload without signature verification and
// returns any OIDC claims present. Returns false if the token is not a valid
// JWT or lacks both sub and email claims.
func parseOIDCToken(token string) (oidcTokenClaims, bool) {
	if token == "" {
		return oidcTokenClaims{}, false
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return oidcTokenClaims{}, false
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return oidcTokenClaims{}, false
	}

	var claims oidcTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return oidcTokenClaims{}, false
	}

	if claims.Subject == "" && claims.Email == "" {
		return oidcTokenClaims{}, false
	}

	return claims, true
}

// sshUsernameFromToken derives a valid Unix username from a bearer token.
//
// Resolution order:
//  1. OIDC email local-part  (e.g. "alice@example.com" → "alice")
//  2. OIDC sub claim          (sanitized)
//  3. fallback                "root"
//
// The resulting string is lowercased, non-alphanumeric/underscore/hyphen
// characters replaced with underscores, and capped at 32 characters.
func sshUsernameFromToken(token string) string {
	if claims, ok := parseOIDCToken(token); ok {
		if claims.Email != "" {
			if at := strings.Index(claims.Email, "@"); at > 0 {
				return sanitizeUnixUsername(claims.Email[:at])
			}
		}
		if claims.Subject != "" {
			return sanitizeUnixUsername(claims.Subject)
		}
	}
	return "root"
}

// sanitizeUnixUsername lowercases s and replaces characters that are not
// alphanumeric, underscore, or hyphen with underscores. The result is
// truncated to 32 characters (Linux POSIX limit).
func sanitizeUnixUsername(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	result := b.String()
	if len(result) > 32 {
		result = result[:32]
	}
	if result == "" {
		return "root"
	}
	return result
}
