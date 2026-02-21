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

package framework

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"
)

// ConditionFunc is a function that returns (done bool, err error).
// A nil error with done=false means retry; a non-nil error stops polling.
type ConditionFunc func(ctx context.Context) (bool, error)

// Poll calls condition repeatedly with the given interval until it returns
// (true, nil) or the context is done. Returns an error if the context expires
// or condition returns a non-nil error.
func Poll(ctx context.Context, interval, timeout time.Duration, condition ConditionFunc) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		done, err := condition(ctx)
		if err != nil {
			return fmt.Errorf("poll condition error: %w", err)
		}
		if done {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for condition", timeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// insecureHTTPClient skips TLS verification — used only in tests against
// self-signed dev certificates.
var insecureHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test only
	},
}

// HTTPGet performs a GET to url using an insecure client (self-signed certs in
// dev) and returns the HTTP status code.
func HTTPGet(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := insecureHTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close() //nolint:errcheck
	return resp.StatusCode, nil
}
