/*
Copyright 2026 The Faros Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package tenant

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type ClientFactory struct {
	baseHost string
	baseTLS  rest.TLSClientConfig

	mu  sync.RWMutex
	hot map[string]dynamic.Interface
}

func NewClientFactory(base *rest.Config) *ClientFactory {
	if base == nil {
		return nil
	}
	baseHost, err := stripClusterSuffix(base.Host)
	if err != nil {
		baseHost = strings.TrimRight(base.Host, "/")
	}
	tls := base.TLSClientConfig
	tls.CertData = nil
	tls.CertFile = ""
	tls.KeyData = nil
	tls.KeyFile = ""
	return &ClientFactory{baseHost: baseHost, baseTLS: tls, hot: map[string]dynamic.Interface{}}
}

func (f *ClientFactory) For(tenantPath, token string) (dynamic.Interface, error) {
	if f == nil {
		return nil, fmt.Errorf("tenant client factory is not configured")
	}
	if token == "" {
		return nil, fmt.Errorf("no bearer token on request")
	}
	key := tenantPath + ":" + hashToken(token)
	f.mu.RLock()
	if d, ok := f.hot[key]; ok {
		f.mu.RUnlock()
		return d, nil
	}
	f.mu.RUnlock()
	cfg := &rest.Config{
		Host:            f.baseHost + "/clusters/" + tenantPath,
		BearerToken:     token,
		TLSClientConfig: f.baseTLS,
	}
	d, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client for tenant %q: %w", tenantPath, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.hot[key]; ok {
		return existing, nil
	}
	f.hot[key] = d
	return d, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

func stripClusterSuffix(host string) (string, error) {
	u, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("parse base kubeconfig host %q: %w", host, err)
	}
	idx := strings.Index(u.Path, "/clusters/")
	if idx < 0 {
		return strings.TrimRight(host, "/"), nil
	}
	u.Path = u.Path[:idx]
	return strings.TrimRight(u.String(), "/"), nil
}
