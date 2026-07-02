// Copyright 2026 The Faros Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

// Package queryapi exposes the provider-owned structured query contract for
// imported Databricks Table resources. App Studio can use tableRefs for
// design-time metadata; generated apps do not call this provider contract until
// App Studio has a sanctioned runtime data-access bridge.
package queryapi

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	DefaultLimit = 100
	MaxLimit     = 1000
)

var identifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type TableRef struct {
	Catalog string `json:"catalog"`
	Schema  string `json:"schema"`
	Table   string `json:"table"`
}

type ConnectionRef struct {
	Name     string `json:"name,omitempty"`
	Host     string `json:"host"`
	AuthType string `json:"authType"`
}

type WarehouseRef struct {
	Name        string `json:"name,omitempty"`
	WarehouseID string `json:"warehouseID"`
}

type Credential struct {
	BearerToken string `json:"-"`
}

type TableTarget struct {
	Table      TableRef      `json:"table"`
	Connection ConnectionRef `json:"connection"`
	Warehouse  WarehouseRef  `json:"warehouse"`
	Credential Credential    `json:"-"`
}

type TableQueryRequest struct {
	Columns []string  `json:"columns,omitempty"`
	Filters []Filter  `json:"filters,omitempty"`
	OrderBy []OrderBy `json:"orderBy,omitempty"`
	Limit   int       `json:"limit,omitempty"`
}

type Filter struct {
	Column   string `json:"column"`
	Operator string `json:"operator"`
	Value    any    `json:"value"`
}

type OrderBy struct {
	Column    string `json:"column"`
	Direction string `json:"direction,omitempty"`
}

func BuildSelectSQL(ref TableRef, req TableQueryRequest) (string, []any, error) {
	from, err := qualifiedTable(ref)
	if err != nil {
		return "", nil, err
	}

	columns := "*"
	if len(req.Columns) > 0 {
		quoted := make([]string, 0, len(req.Columns))
		for _, column := range req.Columns {
			q, err := quoteIdent(column)
			if err != nil {
				return "", nil, fmt.Errorf("column %q: %w", column, err)
			}
			quoted = append(quoted, q)
		}
		columns = strings.Join(quoted, ", ")
	}

	var b strings.Builder
	b.WriteString("SELECT ")
	b.WriteString(columns)
	b.WriteString(" FROM ")
	b.WriteString(from)

	args := make([]any, 0, len(req.Filters))
	if len(req.Filters) > 0 {
		where := make([]string, 0, len(req.Filters))
		for i, filter := range req.Filters {
			column, err := quoteIdent(filter.Column)
			if err != nil {
				return "", nil, fmt.Errorf("filter column %q: %w", filter.Column, err)
			}
			op, err := normalizeOperator(filter.Operator)
			if err != nil {
				return "", nil, err
			}
			where = append(where, fmt.Sprintf("%s %s :p%d", column, op, i))
			args = append(args, filter.Value)
		}
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(where, " AND "))
	}

	if len(req.OrderBy) > 0 {
		parts := make([]string, 0, len(req.OrderBy))
		for _, order := range req.OrderBy {
			column, err := quoteIdent(order.Column)
			if err != nil {
				return "", nil, fmt.Errorf("orderBy column %q: %w", order.Column, err)
			}
			direction := strings.ToUpper(strings.TrimSpace(order.Direction))
			if direction == "" {
				direction = "ASC"
			}
			if direction != "ASC" && direction != "DESC" {
				return "", nil, fmt.Errorf("unsupported order direction %q", order.Direction)
			}
			parts = append(parts, column+" "+direction)
		}
		b.WriteString(" ORDER BY ")
		b.WriteString(strings.Join(parts, ", "))
	}

	limit := req.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	b.WriteString(fmt.Sprintf(" LIMIT %d", limit))

	return b.String(), args, nil
}

func DescribeTableSQL(ref TableRef) (string, error) {
	from, err := qualifiedTable(ref)
	if err != nil {
		return "", err
	}
	return "DESCRIBE TABLE " + from, nil
}

func qualifiedTable(ref TableRef) (string, error) {
	catalog, err := quoteIdent(ref.Catalog)
	if err != nil {
		return "", fmt.Errorf("catalog %q: %w", ref.Catalog, err)
	}
	schema, err := quoteIdent(ref.Schema)
	if err != nil {
		return "", fmt.Errorf("schema %q: %w", ref.Schema, err)
	}
	table, err := quoteIdent(ref.Table)
	if err != nil {
		return "", fmt.Errorf("table %q: %w", ref.Table, err)
	}
	return catalog + "." + schema + "." + table, nil
}

func quoteIdent(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !identifierRE.MatchString(value) {
		return "", fmt.Errorf("invalid identifier")
	}
	return "`" + value + "`", nil
}

func normalizeOperator(op string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(op)) {
	case "=", "!=", "<>", "<", "<=", ">", ">=":
		return strings.ToUpper(strings.TrimSpace(op)), nil
	default:
		return "", fmt.Errorf("unsupported filter operator %q", op)
	}
}
