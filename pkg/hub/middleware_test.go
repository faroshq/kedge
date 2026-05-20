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

package hub

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestSecurityHeadersSet(t *testing.T) {
	srv := httptest.NewServer(withSecurityHeaders(okHandler()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/anything")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	want := map[string]string{
		"Strict-Transport-Security": "max-age=63072000; includeSubDomains",
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "no-referrer",
	}
	for k, v := range want {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
}

func TestEncodedSlashRejected(t *testing.T) {
	// Regression test for the path-normalisation finding. Go's stdlib
	// normalises encoded "%2F" in the path and 301s to the cleaned URL,
	// giving any attacker a same-origin redirector usable in phishing.
	srv := httptest.NewServer(withSecurityHeaders(okHandler()))
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	cases := []string{
		"/services/agent-proxy/x/..%2F..%2Fhealthz",
		"/services/agent-proxy/x/..%2f..%2fhealthz",
		"/clusters/%2fapi/v1",
	}
	for _, p := range cases {
		req, err := http.NewRequest("GET", srv.URL+p, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("get %s: %v", p, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", p, resp.StatusCode)
		}
	}
}

func TestNormalPathsPassThrough(t *testing.T) {
	srv := httptest.NewServer(withSecurityHeaders(okHandler()))
	defer srv.Close()

	for _, p := range []string{"/", "/healthz", "/ui/", "/auth/callback", "/api/v1/namespaces"} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatalf("get %s: %v", p, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", p, resp.StatusCode)
		}
	}
}
