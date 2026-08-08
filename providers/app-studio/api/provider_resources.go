/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	"github.com/faroshq/provider-app-studio/bindings"
	asclient "github.com/faroshq/provider-app-studio/client"
)

// Provider-resource bindings are CONVERGED BY THE PROJECT RECONCILER
// (controller/project), not by this package: handlers write Project spec and
// the reconciler creates/updates/deletes the instances under the provider's
// claimed identity, mirroring status into Project.status.environments.
//
// What remains here is read-through status (so a GET right after a spec write
// shows fresh instance state without waiting for the mirror), the best-effort
// teardown used when project CREATION fails halfway (the Project CR may not
// have gained the reconciler's finalizer yet), and the synchronous reconcile
// used by the integrations flow so a grant/revoke is applied before the
// request returns. The desired-state and status-fold logic itself lives in
// the bindings package, shared with the reconciler so the two can never
// disagree.

func (s *Server) reconcileProjectLiveBindings(ctx context.Context, c *asclient.Client, p *aiv1alpha1.Project, id identity) (*aiv1alpha1.Project, error) {
	if c == nil || p == nil {
		return p, nil
	}
	for _, env := range p.Spec.Environments {
		for _, binding := range env.Bindings {
			if binding.Kind == aiv1alpha1.ProjectBindingKindProviderReference {
				// A providerReference is deliberately non-owning. Reconcile may
				// observe it to publish truthful status, but must never call
				// ensureProjectProviderResource (which creates/updates and adds an
				// owner reference).
				if err := observeProjectProviderReference(ctx, c, binding); err != nil {
					return nil, err
				}
				continue
			}
			if env.Mode != aiv1alpha1.ProjectEnvironmentModeLive {
				continue
			}
			if binding.Kind != aiv1alpha1.ProjectBindingKindProviderResource || binding.ResourceRef == nil {
				continue
			}
			effectiveBinding := binding
			if strings.TrimSpace(env.Name) == projectDevelopmentEnvironmentName &&
				strings.TrimSpace(binding.Name) == projectDevelopmentBindingName &&
				binding.Provider == projectDevelopmentProviderAppStudio {
				var err error
				effectiveBinding, err = s.projectDevelopmentRuntimeBinding(binding, p, id)
				if err != nil {
					return nil, err
				}
			}
			if _, err := ensureProjectProviderResource(ctx, c, p, effectiveBinding, id); err != nil {
				// One provider's absence must not block another provider's
				// grant lifecycle: a revoke or grant change on a databricks
				// integration has to succeed even when the infrastructure
				// API backing the runtime binding is not (or no longer)
				// bound in this workspace. Absent-API failures degrade to
				// binding status; anything else still fails the request.
				if isProjectAPIInitializingError(err) {
					continue
				}
				return nil, err
			}
		}
	}
	return syncProjectLiveBindingStatus(ctx, c, p, id)
}

// projectDevelopmentRuntimeBinding refreshes reserved platform-owned action
// values immediately before instance reconciliation. The Project UID is not
// available on the pre-create binding, and the hub URL/tenant context can
// change independently of a persisted Project, so the provider must not trust
// stale or user-edited copies in spec.environments.
func (s *Server) projectDevelopmentRuntimeBinding(binding aiv1alpha1.ProjectProviderBindingSpec, p *aiv1alpha1.Project, id identity) (aiv1alpha1.ProjectProviderBindingSpec, error) {
	values, err := projectProviderBindingValues(binding)
	if err != nil {
		return binding, err
	}
	context, err := s.projectTemplateBindingContext(p, id)
	if err != nil {
		return binding, err
	}
	actionValues := map[string]string{
		"kedgeActionsExchangeURL": context.ActionsExchangeURL,
		"kedgeActionsBaseURL":     context.ActionsBaseURL,
		"kedgeActionsCABundle":    context.ActionsCABundle,
		"kedgeActionsTenantPath":  context.TenantPath,
		"kedgeActionsOrg":         context.Org,
		"kedgeActionsWorkspace":   context.Workspace,
		"kedgeActionsProject":     context.Project,
		"kedgeActionsProjectUID":  context.ProjectUID,
		"kedgeActionsEnvironment": context.Environment,
		"kedgeActionsInstance":    context.Instance,
	}
	// These fields are platform-owned. Remove any stale or user-edited copy
	// before applying the current trusted context; otherwise a grant revocation
	// or missing external URL could leave an old action endpoint on the runtime
	// binding even though the current context has no action transport.
	for key := range actionValues {
		delete(values, key)
	}
	for key, value := range actionValues {
		if strings.TrimSpace(value) != "" {
			values[key] = value
		}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return binding, err
	}
	binding.Values.Raw = raw
	return binding, nil
}

func ensureProjectProviderResource(ctx context.Context, c *asclient.Client, p *aiv1alpha1.Project, binding aiv1alpha1.ProjectProviderBindingSpec, id identity) (*unstructured.Unstructured, error) {
	if binding.Kind == aiv1alpha1.ProjectBindingKindProviderReference {
		if c == nil || binding.ResourceRef == nil {
			return nil, fmt.Errorf("provider reference %q requires resourceRef", binding.Name)
		}
		gvr, err := projectProviderResourceGVR(binding.ResourceRef)
		if err != nil {
			return nil, err
		}
		name := strings.TrimSpace(binding.ResourceRef.Name)
		if name == "" {
			return nil, fmt.Errorf("provider reference %q requires resourceRef.name", binding.Name)
		}
		// This compatibility guard keeps the helper non-owning even if a future
		// caller accidentally routes a providerReference through the owning
		// reconcile path.
		return c.Resource(providerBindingResource(gvr, binding.ResourceRef.Kind), "").Get(ctx, name, metav1.GetOptions{})
	}
	want, gvr, err := bindings.Desired(p, binding)
	if err != nil {
		return nil, err
	}
	res := c.Resource(providerBindingResource(gvr, binding.ResourceRef.Kind), "")
	existing, err := res.Get(ctx, want.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return res.Create(ctx, want, metav1.CreateOptions{})
	}
	if err != nil {
		return nil, err
	}
	existing.SetAPIVersion(binding.ResourceRef.APIVersion)
	existing.SetKind(binding.ResourceRef.Kind)
	existing.Object["spec"] = want.Object["spec"]
	labels := existing.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[bindings.ProjectLabel] = p.Name
	existing.SetLabels(labels)
	if owner := bindings.OwnerRef(p); owner != nil {
		existing.SetOwnerReferences([]metav1.OwnerReference{*owner})
	}
	return res.Update(ctx, existing, metav1.UpdateOptions{})
}

// observeProjectProviderReference performs the only reconciliation operation
// permitted for a non-owning binding: a GET of the provider-owned object.
// NotFound is represented by Pending status and is intentionally not a
// reconcile failure, so a project can be created before an integration's
// provider resource becomes available.
func observeProjectProviderReference(ctx context.Context, c *asclient.Client, binding aiv1alpha1.ProjectProviderBindingSpec) error {
	if binding.ResourceRef == nil {
		return fmt.Errorf("provider reference %q requires resourceRef", binding.Name)
	}
	gvr, err := projectProviderResourceGVR(binding.ResourceRef)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(binding.ResourceRef.Name)
	if name == "" {
		return fmt.Errorf("provider reference %q requires resourceRef.name", binding.Name)
	}
	_, err = c.Resource(providerBindingResource(gvr, binding.ResourceRef.Kind), "").Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (s *Server) deleteProjectProviderResources(ctx context.Context, c *asclient.Client, p *aiv1alpha1.Project, id identity) error {
	if c == nil || p == nil {
		return nil
	}
	for _, env := range p.Spec.Environments {
		for _, binding := range env.Bindings {
			if binding.Kind != aiv1alpha1.ProjectBindingKindProviderResource || binding.ResourceRef == nil {
				continue
			}
			gvr, err := projectProviderResourceGVR(binding.ResourceRef)
			if err != nil {
				return err
			}
			values, err := projectProviderBindingValues(binding)
			if err != nil {
				return err
			}
			name := projectProviderBindingResourceName(p, binding, values, id)
			if name == "" {
				continue
			}
			// Deleting the instance is enough: the infrastructure provider's
			// kro template owns the runtime namespace and garbage-collects it
			// (and every materialized workload) when the instance goes away.
			err = c.Resource(providerBindingResource(gvr, binding.ResourceRef.Kind), "").Delete(ctx, name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	return nil
}

func syncProjectLiveBindingStatus(ctx context.Context, c *asclient.Client, p *aiv1alpha1.Project, id identity) (*aiv1alpha1.Project, error) {
	statuses := projectLiveEnvironmentStatuses(ctx, c, p, id)
	if len(statuses) == 0 {
		return p, nil
	}
	patch := map[string]any{
		"status": map[string]any{
			"environments": statuses,
		},
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	return c.Projects().Patch(ctx, p.Name, types.MergePatchType, raw, metav1.PatchOptions{}, "status")
}

// projectWithLiveBindingStatus enriches a Project with freshly-observed live
// binding state for the response payload. Read-only — never patches the CR
// (the reconciler owns the durable mirror).
func projectWithLiveBindingStatus(ctx context.Context, c *asclient.Client, p *aiv1alpha1.Project, id identity) *aiv1alpha1.Project {
	if c == nil || p == nil {
		return p
	}
	statuses := projectLiveEnvironmentStatuses(ctx, c, p, id)
	if len(statuses) == 0 {
		return p
	}
	next := p.DeepCopy()
	next.Status.Environments = bindings.MergeEnvironmentStatuses(next.Status.Environments, statuses)
	return next
}

func projectLiveEnvironmentStatuses(ctx context.Context, c *asclient.Client, p *aiv1alpha1.Project, id identity) []aiv1alpha1.ProjectEnvironmentStatus {
	if c == nil || p == nil {
		return nil
	}
	statuses := []aiv1alpha1.ProjectEnvironmentStatus{}
	for _, env := range p.Spec.Environments {
		if env.Mode != aiv1alpha1.ProjectEnvironmentModeLive {
			continue
		}
		var bindingStatuses []aiv1alpha1.ProjectProviderBindingStatus
		for _, binding := range env.Bindings {
			if binding.Kind != aiv1alpha1.ProjectBindingKindProviderResource && binding.Kind != aiv1alpha1.ProjectBindingKindProviderReference {
				continue
			}
			if binding.ResourceRef == nil {
				continue
			}
			bindingStatuses = append(bindingStatuses, projectProviderBindingStatus(ctx, c, p, binding, id))
		}
		if len(bindingStatuses) == 0 {
			continue
		}
		statuses = append(statuses, bindings.FoldEnvironment(env, bindingStatuses))
	}
	return statuses
}

func projectProviderBindingStatus(ctx context.Context, c *asclient.Client, p *aiv1alpha1.Project, binding aiv1alpha1.ProjectProviderBindingSpec, id identity) aiv1alpha1.ProjectProviderBindingStatus {
	gvr, err := projectProviderResourceGVR(binding.ResourceRef)
	if err != nil {
		return bindings.InvalidStatus(binding)
	}
	name := ""
	if binding.Kind == aiv1alpha1.ProjectBindingKindProviderReference {
		// References resolve the provider-owned object by the explicitly bound
		// name. Never derive a project-owned name or mutate the target.
		name = strings.TrimSpace(binding.ResourceRef.Name)
	} else {
		values, valuesErr := projectProviderBindingValues(binding)
		if valuesErr != nil {
			return bindings.InvalidStatus(binding)
		}
		name = projectProviderBindingResourceName(p, binding, values, id)
	}
	if name == "" {
		return bindings.InvalidStatus(binding)
	}
	obj, err := c.Resource(providerBindingResource(gvr, binding.ResourceRef.Kind), "").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return bindings.StatusFromObject(binding, nil)
	}
	status := bindings.StatusFromObject(binding, obj)
	if status.Phase == "" && binding.Kind == aiv1alpha1.ProjectBindingKindProviderReference {
		// A referenced object that exists but publishes no phase of its own is
		// usable as-is: the reference is satisfied.
		status.Phase = "Ready"
	}
	return status
}

// Thin delegations — the shared bindings package owns the logic; these names
// keep the api package's existing call sites and tests stable.

func projectProviderResourceGVR(ref *aiv1alpha1.ProjectProviderResourceReference) (schema.GroupVersionResource, error) {
	return bindings.GVR(ref)
}

func projectProviderBindingValues(binding aiv1alpha1.ProjectProviderBindingSpec) (map[string]any, error) {
	return bindings.Values(binding)
}

func projectProviderBindingResourceName(p *aiv1alpha1.Project, binding aiv1alpha1.ProjectProviderBindingSpec, values map[string]any, _ identity) string {
	return bindings.ResourceName(p, binding, values)
}
