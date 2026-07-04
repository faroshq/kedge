/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package api

import (
	"encoding/json"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
)

func defaultProjectSpec(projectName, displayName, description string, repository *aiv1alpha1.ProjectRepositoryBinding) aiv1alpha1.ProjectSpec {
	return aiv1alpha1.ProjectSpec{
		DisplayName:  displayName,
		Description:  description,
		Repository:   repository,
		Memory:       emptyProjectMemory(),
		Sharing:      privateProjectSharingSpec(),
		Environments: []aiv1alpha1.ProjectEnvironmentSpec{defaultProjectDevelopmentEnvironment(projectName)},
	}
}

func privateProjectSharingSpec() aiv1alpha1.ProjectSharingSpec {
	return aiv1alpha1.ProjectSharingSpec{
		Preview: aiv1alpha1.ProjectSharingPolicy{
			Mode: aiv1alpha1.ProjectSharingModePrivate,
		},
		Publishing: aiv1alpha1.ProjectSharingPolicy{
			Mode: aiv1alpha1.ProjectSharingModePrivate,
		},
	}
}

// defaultProjectDevelopmentEnvironment is created WITHOUT a binding: nothing
// runs until a development template is bound — by the assistant's
// requirements interview (select_project_template), the portal's template
// picker, or PUT /template. Projects created from an imported repository work
// the same way. Replaces the legacy always-on SandboxRunner default
// (docs/app-studio-template-sandboxes.md §4.1; no-compat decision 2026-07-04).
func defaultProjectDevelopmentEnvironment(_ string) aiv1alpha1.ProjectEnvironmentSpec {
	return aiv1alpha1.ProjectEnvironmentSpec{
		Name:       "development",
		Mode:       aiv1alpha1.ProjectEnvironmentModeLive,
		AutoDeploy: false,
		Promotion:  aiv1alpha1.ProjectPromotionManual,
	}
}

// previewHTTPRouteBaseDomain is the public base domain used to validate (anti-
// spoof) the SandboxRunner's reported preview host: it must equal
// <runner>.<baseDomain>. The HTTPRoute itself is created by the infrastructure
// provider's SandboxRunner RGD (on the platform Gateway), so App Studio only
// needs the domain for this check — set APP_STUDIO_PREVIEW_BASE_DOMAIN to the
// same value as the infra provider's KEDGE_APP_BASE_DOMAIN.
func previewHTTPRouteBaseDomain() string {
	return envValue("APP_STUDIO_PREVIEW_BASE_DOMAIN")
}

func envValue(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func projectDeploymentJSONValues(values map[string]any) runtime.RawExtension {
	raw, _ := json.Marshal(values)
	return runtime.RawExtension{Raw: raw}
}
