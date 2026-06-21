/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package server

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"

	"github.com/gorilla/mux"
	"k8s.io/client-go/rest"
)

func (s *Server) previewDevEnvironment(w http.ResponseWriter, r *http.Request) {
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
	target, err := url.Parse(s.runtimeConfig.Host)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	transport, err := restTransport(s.runtimeConfig)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	prefix := "/api/dev-environments/" + name + "/preview/"
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = transport
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = runtimeServicePath(runtimeClusterName(id.tenantPath, env), name, "preview", strings.TrimPrefix(r.URL.Path, prefix))
		req.URL.RawQuery = r.URL.RawQuery
		req.Host = target.Host
		stripPreviewForwardedCredentials(req.Header)
	}
	proxy.ServeHTTP(w, r)
}

func stripPreviewForwardedCredentials(header http.Header) {
	header.Del("Authorization")
	header.Del("Cookie")
	header.Del("X-Kedge-Tenant")
	header.Del("X-Kedge-User")
	header.Del("X-Sandbox-Control-Token")
	for key := range header {
		if strings.HasPrefix(strings.ToLower(key), "x-kedge-") {
			header.Del(key)
		}
	}
}

func runtimeServicePath(tenantPath, name, portName, suffix string) string {
	cleanSuffix := strings.TrimPrefix(path.Clean("/"+suffix), "/")
	base := "/api/v1/namespaces/" + runtimeNamespace(tenantPath) + "/services/" + serviceName(name) + ":" + portName + "/proxy"
	if cleanSuffix == "" || cleanSuffix == "." {
		return base + "/"
	}
	return base + "/" + cleanSuffix
}

func restTransport(cfg *rest.Config) (http.RoundTripper, error) {
	transport, err := rest.TransportFor(cfg)
	if err != nil {
		return nil, err
	}
	return transport, nil
}
