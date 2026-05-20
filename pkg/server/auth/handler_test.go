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

package auth

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return &Handler{
		stateKey:       key,
		hubExternalURL: "https://console.faros.sh",
	}
}

func TestStateSignVerifyRoundtrip(t *testing.T) {
	h := newTestHandler(t)
	payload := []byte(`{"redirectURL":"http://127.0.0.1:9999/callback","sid":"x","cv":"y"}`)

	state := h.signState(payload)
	got, err := h.verifyState(state)
	if err != nil {
		t.Fatalf("verifyState rejected freshly signed state: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload mismatch:\n got %q\nwant %q", got, payload)
	}
}

func TestStateVerifyRejectsTamperedPayload(t *testing.T) {
	// Regression test for the unsigned-state token-theft finding. An attacker
	// crafting a state payload that points RedirectURL at their own host MUST
	// be rejected at the callback step.
	h := newTestHandler(t)
	good := h.signState([]byte(`{"redirectURL":"http://127.0.0.1:9999/callback","sid":"x","cv":"y"}`))

	dot := strings.IndexByte(good, '.')
	sig := good[dot+1:]
	evilPayload := []byte(`{"redirectURL":"https://attacker.example/steal","sid":"x","cv":"y"}`)
	tampered := base64.RawURLEncoding.EncodeToString(evilPayload) + "." + sig

	if _, err := h.verifyState(tampered); err == nil {
		t.Fatal("verifyState accepted tampered payload; HMAC check is broken")
	}
}

func TestStateVerifyRejectsMissingSignature(t *testing.T) {
	h := newTestHandler(t)
	payload := []byte(`{"redirectURL":"http://127.0.0.1:9999/callback"}`)
	// No "." separator — this is the legacy unsigned format.
	legacy := base64.RawURLEncoding.EncodeToString(payload)
	if _, err := h.verifyState(legacy); err == nil {
		t.Fatal("verifyState accepted unsigned (legacy-format) state")
	}
}

func TestStateVerifyRejectsForeignKey(t *testing.T) {
	h1 := newTestHandler(t)
	h2 := newTestHandler(t)
	state := h1.signState([]byte(`{"redirectURL":"http://127.0.0.1:9999/callback"}`))
	if _, err := h2.verifyState(state); err == nil {
		t.Fatal("verifyState accepted state signed with a different key")
	}
}

func TestValidateCallbackRedirect(t *testing.T) {
	h := newTestHandler(t)
	tests := []struct {
		name      string
		url       string
		wantError bool
	}{
		{"cli localhost ipv4", "http://127.0.0.1:9999/callback", false},
		{"cli localhost name", "http://localhost:1234/callback", false},
		{"portal same origin", "https://console.faros.sh/auth/callback", false},
		{"portal sub-path", "https://console.faros.sh/ui/post-login", false},

		{"empty", "", true},
		{"relative", "/callback", true},
		{"localhost https", "https://127.0.0.1:9999/callback", true},
		{"localhost wrong path", "http://127.0.0.1:9999/anything", true},
		{"localhost no port", "http://localhost/callback", true},
		{"localhost out-of-range port", "http://127.0.0.1:99999/callback", true},
		{"different origin scheme", "http://console.faros.sh/auth/callback", true},
		{"different origin host", "https://attacker.example/cb", true},
		{"userinfo bypass", "https://console.faros.sh@attacker.example/cb", true},
		{"subdomain confusion", "https://console.faros.sh.attacker.example/cb", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := h.validateCallbackRedirect(tc.url)
			if tc.wantError && err == nil {
				t.Fatalf("expected error for %q, got nil", tc.url)
			}
			if !tc.wantError && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.url, err)
			}
		})
	}
}

func TestValidateRedirectURIRejectsLocalhost(t *testing.T) {
	// Portal flow must not accept arbitrary localhost URLs; those are only
	// permitted via the CLI flow (p=<port>), where the URL is constructed by
	// the hub itself rather than supplied by the caller.
	h := newTestHandler(t)
	for _, u := range []string{
		"http://localhost:9999/anything",
		"http://127.0.0.1:1/x",
		"https://127.0.0.1:8443/cb",
	} {
		if err := h.validateRedirectURI(u); err == nil {
			t.Fatalf("validateRedirectURI accepted %q (should reject in portal flow)", u)
		}
	}
}
