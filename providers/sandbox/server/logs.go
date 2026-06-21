/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package server

import (
	"io"
	"net/http"

	"github.com/gorilla/mux"
)

func (s *Server) logsDevEnvironment(w http.ResponseWriter, r *http.Request) {
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
	url := s.runtimeConfig.Host + runtimeServicePath(runtimeClusterName(id.tenantPath, env), name, "control", "logs")
	transport, err := restTransport(s.runtimeConfig)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	token, err := s.runtimeControlToken(r.Context(), runtimeClusterName(id.tenantPath, env), name)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, "BadGateway", err.Error())
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	req.Header.Set("X-Sandbox-Control-Token", token)
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		writeStatus(w, http.StatusBadGateway, "BadGateway", err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) statusDevEnvironment(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFromRequest(w, r)
	if !ok {
		return
	}
	name := mux.Vars(r)["name"]
	env, ok := s.devEnvironment(w, r, id, name)
	if !ok {
		return
	}
	status, _ := env.Object["status"].(map[string]any)
	writeJSON(w, http.StatusOK, status)
}
