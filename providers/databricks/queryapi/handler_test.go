// Copyright 2026 The Faros Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package queryapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeBackend struct {
	target TableTarget
	sql    string
	args   []any
}

func (f *fakeBackend) ExecuteQuery(_ context.Context, target TableTarget, sql string, args []any) (QueryResult, error) {
	f.target = target
	f.sql = sql
	f.args = args
	return QueryResult{
		Columns: []string{"order_id", "total_amount"},
		Rows: []map[string]any{
			{"order_id": "ord-1", "total_amount": float64(42)},
		},
	}, nil
}

type fakeResolver struct {
	tables map[string]TableRef
}

func (r fakeResolver) ListTables(context.Context) (map[string]TableRef, error) {
	return r.tables, nil
}

func (r fakeResolver) GetTable(_ context.Context, name string) (TableRef, bool, error) {
	ref, ok := r.tables[name]
	return ref, ok, nil
}

func (r fakeResolver) GetTableTarget(_ context.Context, name string) (TableTarget, bool, error) {
	ref, ok := r.tables[name]
	return TableTarget{Table: ref}, ok, nil
}

func TestHandlerQueriesImportedTableByName(t *testing.T) {
	backend := &fakeBackend{}
	handler := Handler{
		Tables: map[string]TableRef{
			"order-history": {Catalog: "sales", Schema: "gold", Table: "order_history"},
		},
		Backend: backend,
	}
	body, _ := json.Marshal(TableQueryRequest{Columns: []string{"order_id", "total_amount"}, Limit: 10})
	req := httptest.NewRequest(http.MethodPost, "/api/tables/order-history/query", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if backend.sql != "SELECT `order_id`, `total_amount` FROM `sales`.`gold`.`order_history` LIMIT 10" {
		t.Fatalf("sql = %q", backend.sql)
	}
	var got QueryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Rows) != 1 || got.Rows[0]["order_id"] != "ord-1" {
		t.Fatalf("rows = %#v", got.Rows)
	}
}

func TestHandlerUsesRequestScopedResolver(t *testing.T) {
	backend := &fakeBackend{}
	handler := Handler{
		Tables: map[string]TableRef{
			"wrong": {Catalog: "wrong", Schema: "wrong", Table: "wrong"},
		},
		ResolverFromRequest: func(r *http.Request) TableResolver {
			if r.Header.Get("X-Kedge-Cluster") != "cluster-a" {
				t.Fatalf("missing request identity header")
			}
			return fakeResolver{tables: map[string]TableRef{
				"order-history": {Catalog: "sales", Schema: "gold", Table: "order_history"},
			}}
		},
		Backend: backend,
	}
	req := httptest.NewRequest(http.MethodPost, "/api/tables/order-history/query", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Kedge-Cluster", "cluster-a")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if backend.sql != "SELECT * FROM `sales`.`gold`.`order_history` LIMIT 100" {
		t.Fatalf("sql = %q", backend.sql)
	}
}

func TestHandlerRejectsUnknownTable(t *testing.T) {
	handler := Handler{Tables: map[string]TableRef{}, Backend: &fakeBackend{}}
	req := httptest.NewRequest(http.MethodPost, "/api/tables/missing/query", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandlerFailsClosedWhenResolverUnavailable(t *testing.T) {
	handler := Handler{
		Resolver: UnavailableResolver{Message: "tenant client unavailable"},
		Backend:  &fakeBackend{},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/tables/order-history/query", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "tenant client unavailable") {
		t.Fatalf("body = %q, want resolver error", rec.Body.String())
	}
}
