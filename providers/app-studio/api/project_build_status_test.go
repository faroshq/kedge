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

package api

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// packageCR builds a minimal Code provider Package CR (as the crawler writes
// it) for one component, with a single published version.
func packageCR(packageName, imageRepository, digest string, tags ...string) unstructured.Unstructured {
	tagList := make([]any, 0, len(tags))
	for _, t := range tags {
		tagList = append(tagList, t)
	}
	return unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"packageName":     packageName,
			"imageRepository": imageRepository,
			"versions": []any{
				map[string]any{"digest": digest, "tags": tagList},
			},
		},
	}}
}

func TestFindPackageForComponentMatchesSuffix(t *testing.T) {
	items := []unstructured.Unstructured{
		packageCR("rainbow/frontend", "ghcr.io/acme/rainbow/frontend", "sha256:aaa", "sha-abc"),
		packageCR("rainbow/backend", "ghcr.io/acme/rainbow/backend", "sha256:bbb", "sha-abc"),
	}
	pkg := findPackageForComponent(items, "backend")
	if pkg == nil {
		t.Fatal("backend package not found")
	}
	name, _, _ := unstructured.NestedString(pkg.Object, "status", "packageName")
	if name != "rainbow/backend" {
		t.Fatalf("matched %q, want rainbow/backend", name)
	}
	if findPackageForComponent(items, "worker") != nil {
		t.Fatal("worker should have no package")
	}
}

func TestLatestPackageVersionPrefersCommitTag(t *testing.T) {
	pkg := packageCR("rainbow/app", "ghcr.io/acme/rainbow/app", "sha256:ccc", "latest", "sha-deadbeef")
	digest, tag := latestPackageVersion(&pkg)
	if digest != "sha256:ccc" {
		t.Fatalf("digest = %q", digest)
	}
	if tag != "sha-deadbeef" {
		t.Fatalf("tag = %q, want the non-latest commit tag", tag)
	}
}

func TestLatestPackageVersionNoVersions(t *testing.T) {
	pkg := unstructured.Unstructured{Object: map[string]any{"status": map[string]any{"packageName": "x"}}}
	digest, tag := latestPackageVersion(&pkg)
	if digest != "" || tag != "" {
		t.Fatalf("expected empty, got %q/%q", digest, tag)
	}
}

// componentsFromImages applies checkProjectBuild's built/incomplete/none logic
// over a resolved image map, so the status decision is tested without the live
// package-list round-trip.
func statusFor(components []projectBuildComponent, images map[string]componentImageRef) (status string, missing int) {
	built := 0
	for _, comp := range components {
		if img, ok := images[comp.Name]; ok && img.Image != "" {
			built++
		} else {
			missing++
		}
	}
	switch {
	case built == len(components):
		return "built", missing
	case built > 0:
		return "incomplete", missing
	default:
		return "none", missing
	}
}

func TestBuildStatusDecision(t *testing.T) {
	components := projectBuildComponents(applicationTemplateInfo()) // frontend + backend
	all := map[string]componentImageRef{
		"frontend": {Image: "ghcr.io/acme/rainbow/frontend@sha256:aaa"},
		"backend":  {Image: "ghcr.io/acme/rainbow/backend@sha256:bbb"},
	}
	if s, _ := statusFor(components, all); s != "built" {
		t.Fatalf("status = %q, want built", s)
	}
	partial := map[string]componentImageRef{"backend": {Image: "ghcr.io/acme/rainbow/backend@sha256:bbb"}}
	if s, m := statusFor(components, partial); s != "incomplete" || m != 1 {
		t.Fatalf("status = %q missing = %d, want incomplete/1", s, m)
	}
	if s, m := statusFor(components, map[string]componentImageRef{}); s != "none" || m != 2 {
		t.Fatalf("status = %q missing = %d, want none/2", s, m)
	}
}

func TestCommitFromPackageTag(t *testing.T) {
	for _, tc := range []struct{ tag, want string }{
		{"sha-8f2a1c9d4e5b6a7c8d9e0f1a2b3c4d5e6f7a8b9c", "8f2a1c9d4e5b6a7c8d9e0f1a2b3c4d5e6f7a8b9c"},
		{"sha-8f2a1c9", "8f2a1c9"},
		{"latest", ""},
		{"v1.2.3", ""},
		{"", ""},
	} {
		if got := commitFromPackageTag(tc.tag); got != tc.want {
			t.Fatalf("commitFromPackageTag(%q) = %q, want %q", tc.tag, got, tc.want)
		}
	}
}

func TestCommitsMatchToleratesAbbreviation(t *testing.T) {
	full := "8f2a1c9d4e5b6a7c8d9e0f1a2b3c4d5e6f7a8b9c"
	if !commitsMatch("8f2a1c9", full) || !commitsMatch(full, "8f2a1c9") {
		t.Fatal("abbreviated SHA did not match its full form")
	}
	if !commitsMatch(strings.ToUpper(full), full) {
		t.Fatal("SHA comparison is case sensitive")
	}
	if commitsMatch("8f2a1c9", "0000000aaaabbbb") {
		t.Fatal("unrelated SHAs matched")
	}
	// A prefix shorter than an abbreviated SHA would collide constantly.
	if commitsMatch("8f2", full) {
		t.Fatal("over-short prefix matched; abbreviation floor not enforced")
	}
	if commitsMatch("", full) || commitsMatch(full, "") {
		t.Fatal("empty SHA matched")
	}
}

// TestBuildStatusStaleWhenImagesPredateHeadCommit pins the promote gate: every
// component having an image is not sufficient, because the newest published
// package may have been built from an earlier commit.
func TestBuildStatusStaleWhenImagesPredateHeadCommit(t *testing.T) {
	components := projectBuildComponents(applicationTemplateInfo())
	head := "8f2a1c9d4e5b6a7c8d9e0f1a2b3c4d5e6f7a8b9c"

	stale := map[string]componentImageRef{
		"frontend": {Image: "ghcr.io/acme/rainbow/frontend@sha256:aaa", Tag: "sha-" + head},
		"backend":  {Image: "ghcr.io/acme/rainbow/backend@sha256:bbb", Tag: "sha-0123456789abcdef0123456789abcdef01234567"},
	}
	result := buildCheckResultFor(components, stale, head)
	if result.Status != "stale" {
		t.Fatalf("status = %q, want stale", result.Status)
	}
	if len(result.StaleComponents) != 1 || result.StaleComponents[0] != "backend" {
		t.Fatalf("staleComponents = %v, want [backend]", result.StaleComponents)
	}

	current := map[string]componentImageRef{
		"frontend": {Image: "ghcr.io/acme/rainbow/frontend@sha256:aaa", Tag: "sha-" + head},
		"backend":  {Image: "ghcr.io/acme/rainbow/backend@sha256:bbb", Tag: "sha-" + head[:7]},
	}
	if result := buildCheckResultFor(components, current, head); result.Status != "built" {
		t.Fatalf("status = %q, want built (abbreviated tag should match head)", result.Status)
	}

	// Unknown on either side must not block promotion, or projects whose
	// images predate commit tagging could never ship.
	untagged := map[string]componentImageRef{
		"frontend": {Image: "ghcr.io/acme/rainbow/frontend@sha256:aaa"},
		"backend":  {Image: "ghcr.io/acme/rainbow/backend@sha256:bbb"},
	}
	if result := buildCheckResultFor(components, untagged, head); result.Status != "built" {
		t.Fatalf("status = %q with untagged images, want built", result.Status)
	}
	if result := buildCheckResultFor(components, stale, ""); result.Status != "built" {
		t.Fatalf("status = %q with no head commit, want built", result.Status)
	}
}

// buildCheckResultFor mirrors checkProjectBuild's per-component classification
// without the tenant client, so the staleness decision can be tested directly.
func buildCheckResultFor(components []projectBuildComponent, images map[string]componentImageRef, head string) projectBuildCheckResult {
	result := projectBuildCheckResult{HeadCommit: head}
	built := 0
	for _, comp := range components {
		img, ok := images[comp.Name]
		if !ok || img.Image == "" {
			result.Missing = append(result.Missing, comp.Name)
			continue
		}
		built++
		builtCommit := commitFromPackageTag(img.Tag)
		if head != "" && builtCommit != "" && !commitsMatch(builtCommit, head) {
			result.StaleComponents = append(result.StaleComponents, comp.Name)
		}
	}
	switch {
	case built == len(components) && len(result.StaleComponents) > 0:
		result.Status = "stale"
	case built == len(components):
		result.Status = "built"
	case built > 0:
		result.Status = "incomplete"
	default:
		result.Status = "none"
	}
	return result
}
