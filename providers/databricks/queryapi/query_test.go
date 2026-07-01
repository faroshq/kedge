// Copyright 2026 The Faros Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package queryapi

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildSelectSQLQuotesTableAndColumns(t *testing.T) {
	req := TableQueryRequest{
		Columns: []string{"order_date", "total_amount", "status"},
		Filters: []Filter{
			{Column: "status", Operator: "=", Value: "shipped"},
			{Column: "total_amount", Operator: ">=", Value: float64(100)},
		},
		OrderBy: []OrderBy{{Column: "order_date", Direction: "desc"}},
		Limit:   250,
	}

	got, args, err := BuildSelectSQL(TableRef{Catalog: "sales", Schema: "gold", Table: "order_history"}, req)
	if err != nil {
		t.Fatalf("BuildSelectSQL returned error: %v", err)
	}

	want := "SELECT `order_date`, `total_amount`, `status` FROM `sales`.`gold`.`order_history` WHERE `status` = :p0 AND `total_amount` >= :p1 ORDER BY `order_date` DESC LIMIT 250"
	if got != want {
		t.Fatalf("SQL = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(args, []any{"shipped", float64(100)}) {
		t.Fatalf("args = %#v", args)
	}
}

func TestBuildSelectSQLRejectsUnsafeIdentifier(t *testing.T) {
	_, _, err := BuildSelectSQL(TableRef{Catalog: "sales", Schema: "gold", Table: "order_history"}, TableQueryRequest{
		Columns: []string{"order_id; drop table orders"},
		Limit:   10,
	})
	if err == nil {
		t.Fatal("BuildSelectSQL returned nil error for unsafe identifier")
	}
	if !strings.Contains(err.Error(), "column") {
		t.Fatalf("error = %q, want column context", err.Error())
	}
}

func TestBuildSelectSQLCapsLimit(t *testing.T) {
	got, _, err := BuildSelectSQL(TableRef{Catalog: "sales", Schema: "gold", Table: "order_history"}, TableQueryRequest{Limit: 100000})
	if err != nil {
		t.Fatalf("BuildSelectSQL returned error: %v", err)
	}
	if !strings.HasSuffix(got, " LIMIT 1000") {
		t.Fatalf("SQL = %q, want capped limit 1000", got)
	}
}

func TestDescribeTableSQLQuotesQualifiedName(t *testing.T) {
	got, err := DescribeTableSQL(TableRef{Catalog: "sales", Schema: "gold", Table: "order_history"})
	if err != nil {
		t.Fatalf("DescribeTableSQL returned error: %v", err)
	}
	want := "DESCRIBE TABLE `sales`.`gold`.`order_history`"
	if got != want {
		t.Fatalf("SQL = %q, want %q", got, want)
	}
}

func TestDescribeTableSQLRejectsUnsafeIdentifier(t *testing.T) {
	_, err := DescribeTableSQL(TableRef{Catalog: "sales", Schema: "gold", Table: "order_history; drop table orders"})
	if err == nil {
		t.Fatal("DescribeTableSQL returned nil error for unsafe identifier")
	}
	if !strings.Contains(err.Error(), "table") {
		t.Fatalf("error = %q, want table context", err.Error())
	}
}
