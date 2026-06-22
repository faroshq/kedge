/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

func (s *Server) syncDevEnvironment(w http.ResponseWriter, r *http.Request) {
	s.controlOperation(w, r, "sync")
}

func (s *Server) restartDevEnvironment(w http.ResponseWriter, r *http.Request) {
	s.controlOperation(w, r, "restart")
}

func (s *Server) controlOperation(w http.ResponseWriter, r *http.Request, op string) {
	id, ok := identityFromRequest(w, r)
	if !ok {
		return
	}
	name := mux.Vars(r)["name"]
	env, ok := s.devEnvironment(w, r, id, name)
	if !ok {
		return
	}
	if s.runtimeConfig == nil {
		writeStatus(w, http.StatusNotImplemented, "NotImplemented", "runtime kubeconfig not configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "BadRequest", "read request body: "+err.Error())
		return
	}
	if op == "restart" && len(bytes.TrimSpace(body)) == 0 {
		body = []byte(`{}`)
	}
	respBody, status, err := s.postRuntimeService(r, runtimeClusterName(id.tenantPath, env), name, op, body)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, "BadGateway", err.Error())
		return
	}
	if op == "sync" && status >= 200 && status < 300 {
		respBody = s.syncResponseWithPreviewURL(respBody, id.tenantPath, runtimeClusterName(id.tenantPath, env), name)
		_ = s.touchLastSync(r, id, name)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(respBody)
}

func (s *Server) postRuntimeService(r *http.Request, clusterName, name, op string, body []byte) ([]byte, int, error) {
	url := s.runtimeConfig.Host + runtimeServicePath(clusterName, name, "control", op)
	transport, err := restTransport(s.runtimeConfig)
	if err != nil {
		return nil, 0, err
	}
	token, err := s.runtimeControlToken(r.Context(), clusterName, name)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Sandbox-Control-Token", token)
	client := &http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("POST runtime service %s: %w", op, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, 0, err
	}
	return raw, resp.StatusCode, nil
}

func (s *Server) touchLastSync(r *http.Request, id identity, name string) error {
	dyn, err := s.tenantFactory.For(id.tenantPath, id.token)
	if err != nil {
		return err
	}
	now := metav1.Now()
	patch := map[string]any{"status": map[string]any{"lastSyncTime": now.Format("2006-01-02T15:04:05Z07:00")}}
	raw, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = dyn.Resource(devEnvironmentGVR).Patch(r.Context(), name, types.MergePatchType, raw, metav1.PatchOptions{}, "status")
	return err
}

func (s *Server) runtimeControlToken(ctx context.Context, clusterName, name string) (string, error) {
	if s.runtimeClient == nil {
		return "", fmt.Errorf("runtime client is not configured")
	}
	secret, err := s.runtimeClient.CoreV1().Secrets(runtimeNamespace(clusterName)).Get(ctx, controlSecretName(name), metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	token := string(secret.Data["token"])
	if token == "" {
		return "", fmt.Errorf("runtime control token is empty")
	}
	return token, nil
}

func (s *Server) syncResponseWithPreviewURL(raw []byte, tenantPath, clusterName, name string) []byte {
	body := map[string]any{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return raw
	}
	if _, ok := body["previewURL"]; !ok {
		body["previewURL"] = s.signedPreviewURL(tenantPath, clusterName, name)
	}
	next, err := json.Marshal(body)
	if err != nil {
		return raw
	}
	return next
}

func (s *Server) signedPreviewURL(tenantPath, clusterName, name string) string {
	previewURL := externalPreviewPath(name)
	token, err := s.previewSigner.sign(previewTokenPayload{
		TenantPath:     tenantPath,
		ClusterName:    clusterName,
		DevEnvironment: name,
	})
	if err != nil {
		return previewURL
	}
	return previewURL + "?" + previewTokenQuery + "=" + url.QueryEscape(token)
}

func patchLastSync(ctx context.Context, dyn dynamic.Interface, name string, t metav1.Time) error {
	obj, err := dyn.Resource(devEnvironmentGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := unstructured.SetNestedField(obj.Object, t.Format("2006-01-02T15:04:05Z07:00"), "status", "lastSyncTime"); err != nil {
		return err
	}
	_, err = dyn.Resource(devEnvironmentGVR).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	return err
}
