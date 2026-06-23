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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	asclient "github.com/faroshq/provider-app-studio/client"
	"github.com/faroshq/provider-app-studio/workspace"
)

const (
	projectDevelopmentEnvironmentName   = "development"
	projectDevelopmentBindingName       = "dev"
	projectDevelopmentProviderAppStudio = "app-studio"
	projectSandboxSyncTimeout           = 20 * time.Second
)

type projectDevelopmentSyncTargetInfo struct {
	EnvironmentName string
	BindingName     string
	Provider        string
	ResourceName    string
}

type projectSandboxSyncFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type projectSandboxSyncRequest struct {
	Files   []projectSandboxSyncFile `json:"files"`
	Restart string                   `json:"restart,omitempty"`
}

type projectDevelopmentSyncResponse struct {
	Target projectDevelopmentSyncTargetInfo `json:"target"`
	Result json.RawMessage                  `json:"result,omitempty"`
}

type projectDevelopmentPreviewAuthorizeResponse struct {
	Target                projectDevelopmentSyncTargetInfo `json:"target"`
	Ready                 bool                             `json:"ready"`
	PreviewURL            string                           `json:"previewURL,omitempty"`
	PreviewTokenExpiresAt string                           `json:"previewTokenExpiresAt,omitempty"`
	Message               string                           `json:"message,omitempty"`
	Reason                string                           `json:"reason,omitempty"`
}

type projectSandboxPreviewURLResponse struct {
	Ready                 bool   `json:"ready"`
	PreviewURL            string `json:"previewURL,omitempty"`
	PreviewTokenExpiresAt string `json:"previewTokenExpiresAt,omitempty"`
	Message               string `json:"message,omitempty"`
	Reason                string `json:"reason,omitempty"`
}

func projectDevelopmentSyncTarget(p *aiv1alpha1.Project, id identity) (projectDevelopmentSyncTargetInfo, bool) {
	if p == nil {
		return projectDevelopmentSyncTargetInfo{}, false
	}
	for _, env := range p.Spec.Environments {
		if strings.TrimSpace(env.Name) != projectDevelopmentEnvironmentName {
			continue
		}
		if env.Mode != "" && env.Mode != aiv1alpha1.ProjectEnvironmentModeLive {
			continue
		}
		for _, binding := range env.Bindings {
			if strings.TrimSpace(binding.Provider) != projectDevelopmentProviderAppStudio {
				continue
			}
			target := projectDevelopmentSyncTargetInfo{
				EnvironmentName: env.Name,
				BindingName:     binding.Name,
				Provider:        binding.Provider,
			}
			if target.BindingName == "" {
				target.BindingName = projectDevelopmentBindingName
			}
			values, _ := projectProviderBindingValues(binding)
			target.ResourceName = projectProviderBindingResourceName(p, binding, values, id)
			if target.ResourceName == "" {
				return projectDevelopmentSyncTargetInfo{}, false
			}
			return target, true
		}
	}
	return projectDevelopmentSyncTargetInfo{}, false
}

func (s *Server) syncProjectDevelopment(w http.ResponseWriter, r *http.Request) {
	c, id, p, ok := s.requireProjectWithClient(w, r)
	if !ok {
		return
	}
	target, ok := projectDevelopmentSyncTarget(p, id)
	if !ok {
		writeStatus(w, http.StatusBadRequest, "BadRequest", "project has no sandbox runner binding")
		return
	}
	result, err := s.syncProjectDevelopmentTarget(r.Context(), c, id, p, target)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, "BadGateway", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, projectDevelopmentSyncResponse{Target: target, Result: result})
}

func (s *Server) authorizeProjectDevelopmentPreview(w http.ResponseWriter, r *http.Request) {
	c, id, p, ok := s.requireProjectWithClient(w, r)
	if !ok {
		return
	}
	target, ok := projectDevelopmentSyncTarget(p, id)
	if !ok {
		writeStatus(w, http.StatusBadRequest, "BadRequest", "project has no sandbox runner binding")
		return
	}
	preview, err := s.authorizeProjectDevelopmentPreviewTarget(r.Context(), c, id, p, target)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, "BadGateway", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, projectDevelopmentPreviewAuthorizeResponse{
		Target:                target,
		Ready:                 preview.Ready,
		PreviewURL:            preview.PreviewURL,
		PreviewTokenExpiresAt: preview.PreviewTokenExpiresAt,
		Message:               preview.Message,
		Reason:                preview.Reason,
	})
}

func (s *Server) syncProjectDevelopmentTarget(ctx context.Context, c *asclient.Client, id identity, p *aiv1alpha1.Project, target projectDevelopmentSyncTargetInfo) (json.RawMessage, error) {
	if s.workspaces == nil {
		return nil, fmt.Errorf("project workspace store is not configured")
	}
	files, err := s.projectWorkspaceSyncFiles(ctx, projectWorkspaceScope(id, p.Name))
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(projectSandboxSyncRequest{Files: files, Restart: "auto"})
	if err != nil {
		return nil, fmt.Errorf("encode sandbox sync payload: %w", err)
	}
	runtimeTarget, _, err := s.runtimeTargetForProject(ctx, c, target.ResourceName)
	if err != nil {
		return nil, err
	}
	body, status, err := s.postRuntimeService(ctx, runtimeTarget, "sync", payload)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("sandbox runtime sync returned %d: %s", status, strings.TrimSpace(string(body)))
	}
	_ = patchLastSync(ctx, c, target.ResourceName, metav1.Now())
	return json.RawMessage(s.syncResponseWithPreviewURL(body, id, p, target.ResourceName, runtimeTarget)), nil
}

func (s *Server) authorizeProjectDevelopmentPreviewTarget(ctx context.Context, c *asclient.Client, id identity, p *aiv1alpha1.Project, target projectDevelopmentSyncTargetInfo) (projectSandboxPreviewURLResponse, error) {
	runtimeTarget, _, err := s.runtimeTargetForProject(ctx, c, target.ResourceName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return projectSandboxPreviewURLResponse{
				Ready:   false,
				Reason:  "sandbox_runner_not_found",
				Message: "Preview is getting ready. The sandbox runner has not been created yet.",
			}, nil
		}
		return projectSandboxPreviewURLResponse{}, err
	}
	preview := s.previewReadiness(ctx, runtimeTarget)
	if preview.Ready {
		preview.PreviewURL, preview.PreviewTokenExpiresAt = s.signedProjectPreviewURLAndExpiry(p.Name, id.tenantPath, target.ResourceName, runtimeTarget)
	}
	return preview, nil
}

func (s *Server) projectWorkspaceSyncFiles(ctx context.Context, scope workspace.Scope) ([]projectSandboxSyncFile, error) {
	list, err := s.workspaces.ListFiles(ctx, scope, workspace.ListOptions{Limit: workspace.MaxListLimit})
	if err != nil {
		return nil, err
	}
	files := make([]projectSandboxSyncFile, 0, len(list.Files))
	for _, f := range list.Files {
		read, err := s.workspaces.ReadFile(ctx, scope, workspace.ReadOptions{Path: f.Path, MaxBytes: workspace.MaxWriteBytes})
		if err != nil {
			return nil, err
		}
		if read.Binary || read.Truncated {
			continue
		}
		files = append(files, projectSandboxSyncFile{Path: read.Path, Content: read.Content})
	}
	return files, nil
}

func (s *Server) projectWorkspaceSyncDigest(ctx context.Context, scope workspace.Scope) (string, error) {
	if s == nil || s.workspaces == nil {
		return "", nil
	}
	files, err := s.projectWorkspaceSyncFiles(ctx, scope)
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	hash := sha256.New()
	for _, file := range files {
		hash.Write([]byte(file.Path))
		hash.Write([]byte{0})
		hash.Write([]byte(file.Content))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Server) projectWorkspaceSyncDigestChanged(ctx context.Context, scope workspace.Scope, before string, beforeOK bool) bool {
	if !beforeOK {
		return false
	}
	after, err := s.projectWorkspaceSyncDigest(ctx, scope)
	return err == nil && after != before
}

func (s *Server) projectAssistantPreviewRefreshNeeded(ctx context.Context, scope workspace.Scope, before string, beforeOK bool, _ []projectToolCallStreamEvent) bool {
	return s.projectWorkspaceSyncDigestChanged(ctx, scope, before, beforeOK)
}

func shouldSyncDevelopmentAfterTool(name string) bool {
	switch projectToolBaseName(name) {
	case projectToolWriteFile, projectToolApplyPatch, projectToolMkdir:
		return true
	default:
		return false
	}
}
