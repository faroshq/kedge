// Copyright 2026 The Faros Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package queryapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type Backend interface {
	ExecuteQuery(ctx context.Context, target TableTarget, sql string, args []any) (QueryResult, error)
}

type QueryResult struct {
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	Truncated bool             `json:"truncated,omitempty"`
}

type TableResolver interface {
	ListTables(ctx context.Context) (map[string]TableRef, error)
	GetTable(ctx context.Context, name string) (TableRef, bool, error)
	GetTableTarget(ctx context.Context, name string) (TableTarget, bool, error)
}

type StaticTableResolver map[string]TableRef

func (r StaticTableResolver) ListTables(context.Context) (map[string]TableRef, error) {
	out := make(map[string]TableRef, len(r))
	for name, ref := range r {
		out[name] = ref
	}
	return out, nil
}

func (r StaticTableResolver) GetTable(_ context.Context, name string) (TableRef, bool, error) {
	ref, ok := r[name]
	return ref, ok, nil
}

func (r StaticTableResolver) GetTableTarget(_ context.Context, name string) (TableTarget, bool, error) {
	ref, ok := r[name]
	return TableTarget{Table: ref}, ok, nil
}

type UnavailableResolver struct {
	Message string
}

func (r UnavailableResolver) ListTables(context.Context) (map[string]TableRef, error) {
	return nil, r.err()
}

func (r UnavailableResolver) GetTable(context.Context, string) (TableRef, bool, error) {
	return TableRef{}, false, r.err()
}

func (r UnavailableResolver) GetTableTarget(context.Context, string) (TableTarget, bool, error) {
	return TableTarget{}, false, r.err()
}

func (r UnavailableResolver) err() error {
	if strings.TrimSpace(r.Message) == "" {
		return errors.New("table resolver unavailable")
	}
	return errors.New(r.Message)
}

type Handler struct {
	Tables              map[string]TableRef
	Resolver            TableResolver
	ResolverFromRequest func(*http.Request) TableResolver
	Backend             Backend
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tableName := tableNameFromPath(r.URL.Path)
	if tableName == "" {
		http.Error(w, "table name is required", http.StatusBadRequest)
		return
	}
	target, ok, err := h.resolver(r).GetTableTarget(r.Context(), tableName)
	if err != nil {
		http.Error(w, "table lookup failed: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	if !ok {
		http.Error(w, "table not found", http.StatusNotFound)
		return
	}
	if h.Backend == nil {
		http.Error(w, "databricks backend is not configured", http.StatusServiceUnavailable)
		return
	}

	var req TableQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid query body: "+err.Error(), http.StatusBadRequest)
		return
	}
	sql, args, err := BuildSelectSQL(target.Table, req)
	if err != nil {
		http.Error(w, "invalid query: "+err.Error(), http.StatusBadRequest)
		return
	}
	result, err := h.Backend.ExecuteQuery(r.Context(), target, sql, args)
	if err != nil {
		http.Error(w, "query failed", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (h Handler) resolver(r *http.Request) TableResolver {
	if h.ResolverFromRequest != nil {
		if resolver := h.ResolverFromRequest(r); resolver != nil {
			return resolver
		}
	}
	if h.Resolver != nil {
		return h.Resolver
	}
	return StaticTableResolver(h.Tables)
}

func tableNameFromPath(path string) string {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "tables" && parts[i+2] == "query" {
			return parts[i+1]
		}
	}
	return ""
}
