// Copyright 2026 The Faros Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package mcpserver

import (
	"context"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/faroshq/provider-databricks/queryapi"
)

type tableSummary struct {
	Name    string `json:"name"`
	Catalog string `json:"catalog"`
	Schema  string `json:"schema"`
	Table   string `json:"table"`
}

type listTablesOutput struct {
	Tables []tableSummary `json:"tables"`
}

type describeTableInput struct {
	TableRef string `json:"tableRef" jsonschema:"Imported kedge Table resource name, e.g. order-history"`
}

type queryTableInput struct {
	TableRef string                     `json:"tableRef" jsonschema:"Imported kedge Table resource name, e.g. order-history"`
	Query    queryapi.TableQueryRequest `json:"query" jsonschema:"Structured read request: columns, filters, orderBy, limit. Do not pass raw SQL."`
}

func registerTools(srv *mcp.Server, deps Deps, resolver queryapi.TableResolver) {
	safeRegister("list_tables", func() {
		mcp.AddTool(srv, &mcp.Tool{
			Name:        "list_tables",
			Title:       "List imported Databricks tables",
			Description: "List Databricks tables already imported into this kedge workspace. Use this before asking the user to pick a tableRef.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listTablesOutput, error) {
			tables, err := resolver.ListTables(ctx)
			if err != nil {
				return nil, listTablesOutput{}, err
			}
			out := make([]tableSummary, 0, len(tables))
			for name, ref := range tables {
				out = append(out, tableSummary{Name: name, Catalog: ref.Catalog, Schema: ref.Schema, Table: ref.Table})
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
			return nil, listTablesOutput{Tables: out}, nil
		})
	})

	safeRegister("describe_table", func() {
		mcp.AddTool(srv, &mcp.Tool{
			Name:        "describe_table",
			Title:       "Describe an imported Databricks table",
			Description: "Describe one imported kedge Databricks Table resource by tableRef. The resource is a pointer plus cached schema, not table data.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in describeTableInput) (*mcp.CallToolResult, tableSummary, error) {
			ref, ok, err := resolver.GetTable(ctx, in.TableRef)
			if err != nil {
				return nil, tableSummary{}, err
			}
			if !ok {
				return nil, tableSummary{}, fmt.Errorf("tableRef %q not found", in.TableRef)
			}
			return nil, tableSummary{Name: in.TableRef, Catalog: ref.Catalog, Schema: ref.Schema, Table: ref.Table}, nil
		})
	})

	safeRegister("query_table", func() {
		mcp.AddTool(srv, &mcp.Tool{
			Name:        "query_table",
			Title:       "Query an imported Databricks table",
			Description: "Run a bounded structured read against an imported kedge Databricks Table resource. Use tableRef; do not send raw SQL or credentials.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in queryTableInput) (*mcp.CallToolResult, queryapi.QueryResult, error) {
			target, ok, err := resolver.GetTableTarget(ctx, in.TableRef)
			if err != nil {
				return nil, queryapi.QueryResult{}, err
			}
			if !ok {
				return nil, queryapi.QueryResult{}, fmt.Errorf("tableRef %q not found", in.TableRef)
			}
			if deps.Backend == nil {
				return nil, queryapi.QueryResult{}, fmt.Errorf("databricks backend is not configured")
			}
			sql, args, err := queryapi.BuildSelectSQL(target.Table, in.Query)
			if err != nil {
				return nil, queryapi.QueryResult{}, err
			}
			result, err := deps.Backend.ExecuteQuery(ctx, target, sql, args)
			if err != nil {
				return nil, queryapi.QueryResult{}, err
			}
			return nil, result, nil
		})
	})
}
