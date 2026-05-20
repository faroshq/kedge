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
	"strings"
)

// withSecurityHeaders sets baseline browser-hardening headers on every
// response and rejects requests whose path uses encoded slashes (%2F).
//
// Encoded-slash handling: Go's stdlib normalises "%2F" in the path before
// route matching and redirects the client to the cleaned URL. Without this
// guard a URL like "/services/agent-proxy/x/..%2F..%2Fhealthz" 301s to
// "/healthz" — usable as an in-origin open-redirector for phishing. We reject
// such paths outright; legitimate kedge routes never need encoded slashes in
// the request path.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escaped := r.URL.EscapedPath()
		if strings.Contains(escaped, "%2F") || strings.Contains(escaped, "%2f") {
			http.Error(w, "encoded path separator not allowed", http.StatusBadRequest)
			return
		}

		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", "interest-cohort=()")

		next.ServeHTTP(w, r)
	})
}
