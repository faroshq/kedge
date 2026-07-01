// Copyright 2026 The Faros Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Package backend isolates Databricks execution behind a small interface.
package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/faroshq/provider-databricks/queryapi"
)

const defaultStatementWaitTimeout = "10s"

type StatementClient struct {
	HTTPClient                   *http.Client
	WaitTimeout                  string
	AllowedWorkspaceHostSuffixes []string
	// AllowInsecureWorkspaceHost is only for loopback httptest URLs.
	AllowInsecureWorkspaceHost bool
}

func NewStatementClient(httpClient *http.Client) StatementClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return StatementClient{
		HTTPClient:  httpClient,
		WaitTimeout: defaultStatementWaitTimeout,
	}
}

func (c StatementClient) ExecuteQuery(ctx context.Context, target queryapi.TableTarget, sql string, args []any) (queryapi.QueryResult, error) {
	if strings.TrimSpace(target.Connection.Host) == "" {
		return queryapi.QueryResult{}, fmt.Errorf("databricks connection host is required")
	}
	if strings.TrimSpace(target.Warehouse.WarehouseID) == "" {
		return queryapi.QueryResult{}, fmt.Errorf("databricks warehouse_id is required")
	}
	if strings.TrimSpace(target.Credential.BearerToken) == "" {
		return queryapi.QueryResult{}, fmt.Errorf("databricks bearer token is required")
	}
	endpoint, err := c.statementExecutionURL(target.Connection.Host)
	if err != nil {
		return queryapi.QueryResult{}, err
	}
	body := statementRequest{
		Statement:   sql,
		WarehouseID: target.Warehouse.WarehouseID,
		WaitTimeout: c.waitTimeout(),
		Disposition: "INLINE",
		Format:      "JSON_ARRAY",
		Parameters:  statementParameters(args),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return queryapi.QueryResult{}, fmt.Errorf("encode statement request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return queryapi.QueryResult{}, fmt.Errorf("build statement request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+target.Credential.BearerToken)
	req.Header.Set("Content-Type", "application/json")

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return queryapi.QueryResult{}, fmt.Errorf("execute statement: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return queryapi.QueryResult{}, fmt.Errorf("databricks statement failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out statementResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return queryapi.QueryResult{}, fmt.Errorf("decode statement response: %w", err)
	}
	if state := strings.ToUpper(strings.TrimSpace(out.Status.State)); state != "" && state != "SUCCEEDED" {
		if out.Status.Error.Message != "" {
			return queryapi.QueryResult{}, fmt.Errorf("databricks statement %s: %s", state, out.Status.Error.Message)
		}
		return queryapi.QueryResult{}, fmt.Errorf("databricks statement did not complete: %s", state)
	}
	return queryResultFromStatement(out), nil
}

func (c StatementClient) waitTimeout() string {
	if strings.TrimSpace(c.WaitTimeout) == "" {
		return defaultStatementWaitTimeout
	}
	return c.WaitTimeout
}

type statementRequest struct {
	Statement   string               `json:"statement"`
	WarehouseID string               `json:"warehouse_id"`
	WaitTimeout string               `json:"wait_timeout"`
	Disposition string               `json:"disposition"`
	Format      string               `json:"format"`
	Parameters  []statementParameter `json:"parameters,omitempty"`
}

type statementParameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type statementResponse struct {
	Status struct {
		State string `json:"state"`
		Error struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	} `json:"status"`
	Manifest struct {
		Schema struct {
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
		} `json:"schema"`
		Truncated bool `json:"truncated,omitempty"`
	} `json:"manifest"`
	Result struct {
		DataArray [][]any `json:"data_array"`
		Truncated bool    `json:"truncated,omitempty"`
	} `json:"result"`
}

func statementExecutionURL(host string) (string, error) {
	return StatementClient{}.statementExecutionURL(host)
}

func (c StatementClient) statementExecutionURL(host string) (string, error) {
	u, err := c.workspaceURL(host)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/2.0/sql/statements"
	return u.String(), nil
}

func statementParameters(args []any) []statementParameter {
	if len(args) == 0 {
		return nil
	}
	out := make([]statementParameter, 0, len(args))
	for i, arg := range args {
		out = append(out, statementParameter{
			Name:  fmt.Sprintf("p%d", i),
			Value: fmt.Sprint(arg),
		})
	}
	return out
}

func queryResultFromStatement(resp statementResponse) queryapi.QueryResult {
	columns := make([]string, 0, len(resp.Manifest.Schema.Columns))
	for _, column := range resp.Manifest.Schema.Columns {
		columns = append(columns, column.Name)
	}
	rows := make([]map[string]any, 0, len(resp.Result.DataArray))
	for _, values := range resp.Result.DataArray {
		row := make(map[string]any, len(columns))
		for i, column := range columns {
			if i < len(values) {
				row[column] = values[i]
			}
		}
		rows = append(rows, row)
	}
	return queryapi.QueryResult{
		Columns:   columns,
		Rows:      rows,
		Truncated: resp.Manifest.Truncated || resp.Result.Truncated,
	}
}

// Stub is a local-development backend used only when DATABRICKS_DEV_STATIC_TABLES
// is explicitly enabled.
type Stub struct{}

func (Stub) ExecuteQuery(_ context.Context, _ queryapi.TableTarget, sql string, _ []any) (queryapi.QueryResult, error) {
	columns := []string{"sql"}
	if strings.Contains(sql, `"order_id"`) {
		columns = []string{"order_id", "total_amount"}
		return queryapi.QueryResult{
			Columns: columns,
			Rows: []map[string]any{
				{"order_id": "example-order", "total_amount": float64(42)},
			},
		}, nil
	}
	return queryapi.QueryResult{
		Columns: columns,
		Rows:    []map[string]any{{"sql": sql}},
	}, nil
}
