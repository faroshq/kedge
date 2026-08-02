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
	"context"
	"regexp"
	"strings"
	"time"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	asclient "github.com/faroshq/provider-app-studio/client"
)

// API view mapping: Project and environment/provider-binding views returned
// by the projects endpoints, plus the small string helpers they use.

func projectView(ctx context.Context, c *asclient.Client, p *aiv1alpha1.Project, id identity) ProjectView {
	p = projectWithLiveBindingStatus(ctx, c, p, id)
	view := ProjectView{
		Name:         p.Name,
		DisplayName:  p.Spec.DisplayName,
		Description:  p.Spec.Description,
		Phase:        p.Status.Phase,
		Repository:   projectRepositoryView(ctx, c, p),
		Memory:       p.Spec.Memory,
		Sharing:      effectiveProjectSharingSpec(p.Spec.Sharing),
		Environments: projectEnvironmentViews(p),
		CreatedAt:    p.CreationTimestamp.Time,
	}
	if p.Spec.Template != nil {
		view.Template = strings.TrimSpace(p.Spec.Template.Name)
	}
	if p.Status.UpdatedAt != nil {
		t := p.Status.UpdatedAt.Time
		view.UpdatedAt = &t
	}
	return view
}

func projectEnvironmentViews(p *aiv1alpha1.Project) []ProjectEnvironmentView {
	statusByName := map[string]aiv1alpha1.ProjectEnvironmentStatus{}
	for _, st := range p.Status.Environments {
		statusByName[st.Name] = st
	}
	views := make([]ProjectEnvironmentView, 0, len(p.Spec.Environments))
	for _, spec := range p.Spec.Environments {
		st := statusByName[spec.Name]
		mode := string(spec.Mode)
		if mode == "" && st.Mode != "" {
			mode = string(st.Mode)
		}
		if mode == "" {
			mode = string(aiv1alpha1.ProjectEnvironmentModeArtifact)
		}
		view := ProjectEnvironmentView{
			Name:     spec.Name,
			Mode:     mode,
			Phase:    st.Phase,
			Bindings: projectProviderBindingViews(spec.Bindings, st.Bindings),
		}
		views = append(views, view)
		delete(statusByName, spec.Name)
	}
	for _, st := range statusByName {
		mode := string(st.Mode)
		if mode == "" {
			mode = string(aiv1alpha1.ProjectEnvironmentModeArtifact)
		}
		views = append(views, ProjectEnvironmentView{
			Name:     st.Name,
			Mode:     mode,
			Phase:    st.Phase,
			Bindings: projectProviderBindingViews(nil, st.Bindings),
		})
	}
	return views
}

func projectProviderBindingViews(specs []aiv1alpha1.ProjectProviderBindingSpec, statuses []aiv1alpha1.ProjectProviderBindingStatus) []ProjectProviderBindingView {
	statusByName := map[string]aiv1alpha1.ProjectProviderBindingStatus{}
	for _, st := range statuses {
		statusByName[st.Name] = st
	}
	views := make([]ProjectProviderBindingView, 0, len(specs)+len(statuses))
	for _, spec := range specs {
		st := statusByName[spec.Name]
		views = append(views, ProjectProviderBindingView{
			Name:       spec.Name,
			Provider:   firstNonEmpty(st.Provider, spec.Provider),
			Phase:      st.Phase,
			URL:        st.URL,
			PreviewURL: st.PreviewURL,
			Outputs:    st.Outputs,
		})
		delete(statusByName, spec.Name)
	}
	for _, st := range statusByName {
		views = append(views, ProjectProviderBindingView{
			Name:       st.Name,
			Provider:   st.Provider,
			Phase:      st.Phase,
			URL:        st.URL,
			PreviewURL: st.PreviewURL,
			Outputs:    st.Outputs,
		})
	}
	return views
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func projectUpdatedAt(p *aiv1alpha1.Project) time.Time {
	if p.Status.UpdatedAt != nil {
		return p.Status.UpdatedAt.Time
	}
	return p.CreationTimestamp.Time
}

func emptyProjectMemory() aiv1alpha1.ProjectMemory {
	return aiv1alpha1.ProjectMemory{
		Goals:        []string{},
		Requirements: []string{},
		Constraints:  []string{},
	}
}

var invalidProjectNameChars = regexp.MustCompile(`[^a-z0-9-]+`)

func slugifyProjectName(str string) string {
	str = strings.ToLower(strings.TrimSpace(str))
	str = invalidProjectNameChars.ReplaceAllString(str, "-")
	str = strings.Trim(str, "-")
	for strings.Contains(str, "--") {
		str = strings.ReplaceAll(str, "--", "-")
	}
	if len(str) > 63 {
		str = strings.Trim(str[:63], "-")
	}
	return str
}
