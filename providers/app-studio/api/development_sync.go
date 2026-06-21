/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"k8s.io/klog/v2"

	aiv1alpha1 "github.com/faroshq/provider-app-studio/apis/ai/v1alpha1"
	"github.com/faroshq/provider-app-studio/workspace"
)

const (
	projectDevelopmentEnvironmentName = "development"
	projectDevelopmentBindingName     = "dev"
	projectDevelopmentProviderSandbox = "sandbox"
	projectSandboxSyncTimeout         = 20 * time.Second
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

func projectDevelopmentSyncTarget(p *aiv1alpha1.Project) (projectDevelopmentSyncTargetInfo, bool) {
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
			if strings.TrimSpace(binding.Provider) != projectDevelopmentProviderSandbox {
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
			if name := projectProviderBindingResourceName(p, binding, values); name != "" {
				target.ResourceName = name
			} else {
				target.ResourceName = p.Name + "-dev"
			}
			if target.ResourceName == "" {
				return projectDevelopmentSyncTargetInfo{}, false
			}
			return target, true
		}
	}
	return projectDevelopmentSyncTargetInfo{}, false
}

func (s *Server) syncProjectDevelopment(w http.ResponseWriter, r *http.Request) {
	_, id, p, ok := s.requireProjectWithClient(w, r)
	if !ok {
		return
	}
	target, ok := projectDevelopmentSyncTarget(p)
	if !ok {
		writeStatus(w, http.StatusBadRequest, "BadRequest", "project has no sandbox development environment binding")
		return
	}
	result, err := s.syncProjectDevelopmentTarget(r.Context(), id, p, target)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, "BadGateway", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, projectDevelopmentSyncResponse{Target: target, Result: result})
}

func (s *Server) syncProjectDevelopmentTarget(ctx context.Context, id identity, p *aiv1alpha1.Project, target projectDevelopmentSyncTargetInfo) (json.RawMessage, error) {
	if s.workspaces == nil {
		return nil, fmt.Errorf("project workspace store is not configured")
	}
	endpoint, err := s.sandboxSyncEndpoint(target)
	if err != nil {
		return nil, err
	}
	files, err := s.projectWorkspaceSyncFiles(ctx, projectWorkspaceScope(id, p.Name))
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(projectSandboxSyncRequest{Files: files, Restart: "auto"})
	if err != nil {
		return nil, fmt.Errorf("encode sandbox sync payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("new sandbox sync request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if id.token != "" {
		req.Header.Set("Authorization", "Bearer "+id.token)
	}
	if id.tenantPath != "" {
		req.Header.Set("X-Kedge-Tenant", id.tenantPath)
	}

	client := &http.Client{Timeout: projectSandboxSyncTimeout, Transport: projectMCPTransport(s.mcpInsecureSkipTLSVerify)}
	resp, err := client.Do(req)
	if err != nil && projectMCPShouldRetryInsecure(endpoint, err, s.mcpInsecureSkipTLSVerify) {
		client = &http.Client{Timeout: projectSandboxSyncTimeout, Transport: projectMCPTransport(true)}
		resp, err = client.Do(req)
	}
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read sandbox sync body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("sandbox sync returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.RawMessage(body), nil
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

func (s *Server) sandboxSyncEndpoint(target projectDevelopmentSyncTargetInfo) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(s.hubBase), "/")
	if base == "" {
		return "", fmt.Errorf("hub base URL is not configured")
	}
	if target.ResourceName == "" {
		return "", fmt.Errorf("sandbox development environment name is empty")
	}
	return base + "/services/providers/sandbox/api/dev-environments/" + target.ResourceName + "/sync", nil
}

func shouldSyncDevelopmentAfterTool(name string) bool {
	switch projectToolBaseName(name) {
	case projectToolWriteFile, projectToolApplyPatch, projectToolMkdir:
		return true
	default:
		return false
	}
}

func (s *Server) syncDevelopmentAfterMutation(id identity, p *aiv1alpha1.Project, name string) {
	target, ok := projectDevelopmentSyncTarget(p)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), projectSandboxSyncTimeout)
	defer cancel()
	if _, err := s.syncProjectDevelopmentTarget(ctx, id, p, target); err != nil {
		klog.V(2).Infof("development sandbox sync after %s failed for project %s: %v", projectToolBaseName(name), p.Name, err)
	}
}
