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

// Personal org guardrails. Per O-8 + O-12 the user's personal org and
// the user themselves are 1:1 — soft-deleting the org separately or
// self-leaving from it must not succeed because there's no other admin
// to keep the org alive.

package cases

import (
	"context"
	"net/http"
	"testing"
	"time"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"

	"github.com/faroshq/faros-kedge/test/e2e/framework"
)

// TenancyPersonalOrgGuardrails discovers the caller's personal org (the
// bootstrap creates one on first OIDC login or first static-token call)
// and verifies the two destructive operations the API correctly refuses.
func TenancyPersonalOrgGuardrails() features.Feature {
	return features.New("Tenancy/PersonalOrgGuardrails").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			hubURL := tenancyHubURL(ctx, t)
			bearer := tenancyBearer(ctx, t)
			// The first /api/orgs call is what triggers the personal-org
			// bootstrap. Retry briefly so we don't race with the controller.
			var personalOrg string
			pollCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			pollErr := framework.Poll(pollCtx, 2*time.Second, 30*time.Second, func(ctx context.Context) (bool, error) {
				p, err := framework.FindPersonalOrgUUID(ctx, hubURL, bearer)
				if err != nil {
					return false, nil
				}
				personalOrg = p
				return true, nil
			})
			if pollErr != nil {
				t.Fatalf("personal org never appeared: %v", pollErr)
			}
			t.Logf("personal org uuid=%s", personalOrg)
			return context.WithValue(ctx, personalOrgCtxKey{}, &personalOrgData{
				hubURL:  hubURL,
				bearer:  bearer,
				orgUUID: personalOrg,
			})
		}).
		Assess("personal_org_cannot_be_deleted", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			d := ctx.Value(personalOrgCtxKey{}).(*personalOrgData)
			code, body, err := framework.DoRESTRequest(ctx, http.MethodDelete,
				orgURL(d.hubURL, d.orgUUID), d.bearer,
				orgHeaders(d.orgUUID), nil)
			if err != nil {
				t.Fatalf("DELETE personal org: %v", err)
			}
			// Spec says personal orgs cascade off the User CR — DELETE
			// against the org directly should be refused. Accept 4xx
			// (refused) but not 200/2xx (mutation accepted) or 500.
			if code >= 200 && code < 300 {
				t.Fatalf("DELETE personal org succeeded (%d): expected 4xx (body=%s)", code, body)
			}
			if code >= 500 {
				t.Fatalf("DELETE personal org returned 5xx (%d): %s", code, body)
			}
			t.Logf("DELETE personal org refused with %d (correct)", code)
			return ctx
		}).
		Assess("self_leave_personal_org_rejected", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			d := ctx.Value(personalOrgCtxKey{}).(*personalOrgData)
			code, body, err := framework.DoRESTRequest(ctx, http.MethodDelete,
				orgURL(d.hubURL, d.orgUUID)+"/memberships/me", d.bearer,
				orgHeaders(d.orgUUID), nil)
			if err != nil {
				t.Fatalf("self-leave personal org: %v", err)
			}
			if code >= 200 && code < 300 {
				t.Fatalf("self-leave personal org succeeded (%d): expected 4xx (body=%s)", code, body)
			}
			if code >= 500 {
				t.Fatalf("self-leave personal org returned 5xx (%d): %s", code, body)
			}
			t.Logf("self-leave personal org refused with %d (correct)", code)
			return ctx
		}).
		Feature()
}

type personalOrgCtxKey struct{}

type personalOrgData struct {
	hubURL  string
	bearer  string
	orgUUID string
}
