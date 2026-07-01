// Copyright 2026 The Faros Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faroshq/provider-databricks/queryapi"
)

func TestStatementClientPostsStatementExecutionRequest(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotReq statementRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": {"state": "SUCCEEDED"},
			"manifest": {"schema": {"columns": [{"name": "order_id"}, {"name": "total_amount"}]}},
			"result": {"data_array": [["ord-1", 42.5]]}
		}`))
	}))
	defer server.Close()
	client := testStatementClient(server.Client())

	result, err := client.ExecuteQuery(context.Background(), queryapi.TableTarget{
		Connection: queryapi.ConnectionRef{Host: server.URL, AuthType: "pat"},
		Warehouse:  queryapi.WarehouseRef{WarehouseID: "wh-123"},
		Credential: queryapi.Credential{BearerToken: "pat-token"},
	}, `SELECT * FROM "sales"."gold"."order_history" WHERE "status" = :p0`, []any{"shipped"})
	if err != nil {
		t.Fatalf("ExecuteQuery returned error: %v", err)
	}
	if gotPath != "/api/2.0/sql/statements" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer pat-token" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotReq.WarehouseID != "wh-123" || gotReq.Statement == "" || gotReq.Format != "JSON_ARRAY" || gotReq.Disposition != "INLINE" {
		t.Fatalf("request = %#v", gotReq)
	}
	if len(gotReq.Parameters) != 1 || gotReq.Parameters[0].Name != "p0" || gotReq.Parameters[0].Value != "shipped" {
		t.Fatalf("parameters = %#v", gotReq.Parameters)
	}
	if len(result.Rows) != 1 || result.Rows[0]["order_id"] != "ord-1" || result.Rows[0]["total_amount"] != float64(42.5) {
		t.Fatalf("result = %#v", result)
	}
}

func TestStatementClientRejectsUntrustedHostBeforeSendingBearer(t *testing.T) {
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{
			"status": {"state": "SUCCEEDED"},
			"manifest": {"schema": {"columns": [{"name": "order_id"}]}},
			"result": {"data_array": [["ord-1"]]}
		}`))
	}))
	defer server.Close()
	client := StatementClient{HTTPClient: server.Client()}

	_, err := client.ExecuteQuery(context.Background(), queryapi.TableTarget{
		Connection: queryapi.ConnectionRef{Host: server.URL, AuthType: "pat"},
		Warehouse:  queryapi.WarehouseRef{WarehouseID: "wh-123"},
		Credential: queryapi.Credential{BearerToken: "pat-token"},
	}, `SELECT * FROM "sales"."gold"."order_history"`, nil)
	if err == nil {
		t.Fatal("ExecuteQuery returned nil error for untrusted host")
	}
	if requests != 0 {
		t.Fatalf("untrusted host received %d requests, want 0", requests)
	}
	if !strings.Contains(err.Error(), "not an allowed Databricks workspace host") {
		t.Fatalf("error = %q, want allowed-host rejection", err.Error())
	}
}

func TestStatementClientRejectsIncompleteTarget(t *testing.T) {
	client := NewStatementClient(nil)
	if _, err := client.ExecuteQuery(context.Background(), queryapi.TableTarget{}, "SELECT 1", nil); err == nil {
		t.Fatal("ExecuteQuery returned nil error for incomplete target")
	}
}

func TestStatementClientValidateTableDescribesColumns(t *testing.T) {
	var gotReq statementRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": {"state": "SUCCEEDED"},
			"manifest": {"schema": {"columns": [{"name": "col_name"}, {"name": "data_type"}, {"name": "comment"}]}},
			"result": {"data_array": [
				["order_id", "STRING", "Business order identifier"],
				["total_amount", "DECIMAL(10,2)", ""],
				["# Partition Information", "", ""]
			]}
		}`))
	}))
	defer server.Close()
	client := testStatementClient(server.Client())

	result, err := client.ValidateTable(context.Background(), queryapi.TableTarget{
		Table:      queryapi.TableRef{Catalog: "sales", Schema: "gold", Table: "order_history"},
		Connection: queryapi.ConnectionRef{Host: server.URL, AuthType: "pat"},
		Warehouse:  queryapi.WarehouseRef{WarehouseID: "wh-123"},
		Credential: queryapi.Credential{BearerToken: "pat-token"},
	})
	if err != nil {
		t.Fatalf("ValidateTable returned error: %v", err)
	}
	if !strings.HasPrefix(gotReq.Statement, "DESCRIBE TABLE `sales`.`gold`.`order_history`") {
		t.Fatalf("statement = %q, want DESCRIBE TABLE", gotReq.Statement)
	}
	if gotReq.WarehouseID != "wh-123" {
		t.Fatalf("warehouseID = %q, want wh-123", gotReq.WarehouseID)
	}
	if len(result.Columns) != 2 {
		t.Fatalf("columns = %#v, want 2", result.Columns)
	}
	if result.Columns[0].Name != "order_id" || result.Columns[0].Type != "STRING" || result.Columns[0].Comment != "Business order identifier" {
		t.Fatalf("first column = %#v", result.Columns[0])
	}
	if result.Columns[1].Name != "total_amount" || result.Columns[1].Type != "DECIMAL(10,2)" {
		t.Fatalf("second column = %#v", result.Columns[1])
	}
}
