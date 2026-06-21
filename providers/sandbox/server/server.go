/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/faroshq/provider-sandbox/tenant"
)

var devEnvironmentGVR = schema.GroupVersionResource{
	Group:    "sandbox.kedge.faros.sh",
	Version:  "v1alpha1",
	Resource: "devenvironments",
}

const logicalClusterAnnotation = "kcp.io/cluster"

type Server struct {
	runtimeConfig *rest.Config
	runtimeClient kubernetes.Interface
	tenantFactory *tenant.ClientFactory
	mux           *mux.Router
}

func New(runtimeConfig *rest.Config, tenantFactory *tenant.ClientFactory) http.Handler {
	var runtimeClient kubernetes.Interface
	if runtimeConfig != nil {
		runtimeClient, _ = kubernetes.NewForConfig(runtimeConfig)
	}
	s := &Server{
		runtimeConfig: runtimeConfig,
		runtimeClient: runtimeClient,
		tenantFactory: tenantFactory,
		mux:           mux.NewRouter(),
	}
	s.mux.HandleFunc("/healthz", healthz).Methods(http.MethodGet)
	s.mux.HandleFunc("/api/dev-environments/{name}/sync", s.syncDevEnvironment).Methods(http.MethodPost)
	s.mux.HandleFunc("/api/dev-environments/{name}/restart", s.restartDevEnvironment).Methods(http.MethodPost)
	s.mux.HandleFunc("/api/dev-environments/{name}/logs", s.logsDevEnvironment).Methods(http.MethodGet)
	s.mux.HandleFunc("/api/dev-environments/{name}/status", s.statusDevEnvironment).Methods(http.MethodGet)
	s.mux.PathPrefix("/api/dev-environments/{name}/preview/").HandlerFunc(s.previewDevEnvironment).Methods(http.MethodGet, http.MethodHead)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type identity struct {
	tenantPath string
	token      string
}

func identityFromRequest(w http.ResponseWriter, r *http.Request) (identity, bool) {
	tenantPath := strings.TrimSpace(r.Header.Get("X-Kedge-Tenant"))
	if tenantPath == "" {
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", "tenant context missing")
		return identity{}, false
	}
	token := bearerToken(r)
	if token == "" {
		writeStatus(w, http.StatusUnauthorized, "Unauthorized", "bearer token missing")
		return identity{}, false
	}
	return identity{tenantPath: tenantPath, token: token}, true
}

func bearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[len("bearer "):])
	}
	return auth
}

func (s *Server) devEnvironment(w http.ResponseWriter, r *http.Request, id identity, name string) (*unstructured.Unstructured, bool) {
	dyn, err := s.tenantFactory.For(id.tenantPath, id.token)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "InternalError", err.Error())
		return nil, false
	}
	obj, err := dyn.Resource(devEnvironmentGVR).Get(r.Context(), name, metav1.GetOptions{})
	if err != nil {
		writeStatus(w, http.StatusNotFound, "NotFound", err.Error())
		return nil, false
	}
	return obj, true
}

func runtimeNamespace(tenantPath string) string {
	sum := sha256.Sum256([]byte(tenantPath))
	return "sandbox-" + hex.EncodeToString(sum[:])[:16]
}

func runtimeClusterName(tenantPath string, obj *unstructured.Unstructured) string {
	if obj != nil {
		if cluster := strings.TrimSpace(obj.GetAnnotations()[logicalClusterAnnotation]); cluster != "" {
			return cluster
		}
	}
	return tenantPath
}

func serviceName(name string) string {
	return name + "-preview"
}

func controlSecretName(name string) string {
	return name + "-control"
}

func writeStatus(w http.ResponseWriter, code int, reason, message string) {
	writeJSON(w, code, map[string]any{
		"kind":       "Status",
		"apiVersion": "v1",
		"metadata":   map[string]any{},
		"status":     "Failure",
		"message":    message,
		"reason":     reason,
		"code":       code,
	})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
