// Copyright 2026 The Faros Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0

package tenant

import (
	"context"
	"encoding/base64"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestTableResolverBuildsRuntimeTargetFromTenantResources(t *testing.T) {
	dyn := fakeTenantClient(
		obj(tablesGVR.Group, tablesGVR.Version, "Table", "", "order-history", map[string]any{
			"connectionRef": "sales-workspace",
			"warehouseRef":  "sales-warehouse",
			"catalog":       "sales",
			"schema":        "gold",
			"table":         "order_history",
		}),
		obj(warehousesGVR.Group, warehousesGVR.Version, "Warehouse", "", "sales-warehouse", map[string]any{
			"connectionRef": "sales-workspace",
			"warehouseID":   "wh-123",
		}),
		obj(connectionsGVR.Group, connectionsGVR.Version, "Connection", "", "sales-workspace", map[string]any{
			"host":     "https://dbc.example.com",
			"authType": "pat",
			"secretRef": map[string]any{
				"name":      "sales-token",
				"namespace": "data-creds",
				"key":       "access_token",
			},
		}),
		secret("data-creds", "sales-token", "access_token", "pat-token"),
	)
	factory := &ClientFactory{hot: map[string]dynamic.Interface{
		"cluster-a:" + hashToken("caller-token"): dyn,
	}}
	resolver := tableResolver{
		factory: factory,
		identity: identity{
			tenantPath: "root:org:workspace",
			clusterID:  "cluster-a",
			token:      "caller-token",
		},
	}

	target, ok, err := resolver.GetTableTarget(context.Background(), "order-history")
	if err != nil {
		t.Fatalf("GetTableTarget returned error: %v", err)
	}
	if !ok {
		t.Fatal("GetTableTarget returned ok=false")
	}
	if target.Table.Catalog != "sales" || target.Table.Schema != "gold" || target.Table.Table != "order_history" {
		t.Fatalf("table = %#v", target.Table)
	}
	if target.Connection.Host != "https://dbc.example.com" || target.Connection.AuthType != "pat" {
		t.Fatalf("connection = %#v", target.Connection)
	}
	if target.Warehouse.WarehouseID != "wh-123" {
		t.Fatalf("warehouse = %#v", target.Warehouse)
	}
	if target.Credential.BearerToken != "pat-token" {
		t.Fatalf("credential token = %q", target.Credential.BearerToken)
	}
}

func TestTableResolverRejectsMismatchedWarehouseConnection(t *testing.T) {
	dyn := fakeTenantClient(
		obj(tablesGVR.Group, tablesGVR.Version, "Table", "", "order-history", map[string]any{
			"connectionRef": "sales-workspace",
			"warehouseRef":  "wrong-warehouse",
			"catalog":       "sales",
			"schema":        "gold",
			"table":         "order_history",
		}),
		obj(warehousesGVR.Group, warehousesGVR.Version, "Warehouse", "", "wrong-warehouse", map[string]any{
			"connectionRef": "other-workspace",
			"warehouseID":   "wh-123",
		}),
	)
	factory := &ClientFactory{hot: map[string]dynamic.Interface{
		"cluster-a:" + hashToken("caller-token"): dyn,
	}}
	resolver := tableResolver{
		factory: factory,
		identity: identity{
			tenantPath: "root:org:workspace",
			clusterID:  "cluster-a",
			token:      "caller-token",
		},
	}

	if _, _, err := resolver.GetTableTarget(context.Background(), "order-history"); err == nil {
		t.Fatal("GetTableTarget returned nil error for mismatched warehouse connection")
	}
}

func fakeTenantClient(objects ...runtime.Object) dynamic.Interface {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		tablesGVR:      "TableList",
		warehousesGVR:  "WarehouseList",
		connectionsGVR: "ConnectionList",
		secretsGVR:     "SecretList",
	}, objects...)
}

func obj(group, version, kind, namespace, name string, spec map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion(group, version),
		"kind":       kind,
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": spec,
	}}
}

func secret(namespace, name, key, value string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"data": map[string]any{
			key: base64.StdEncoding.EncodeToString([]byte(value)),
		},
	}}
}

func apiVersion(group, version string) string {
	if group == "" {
		return version
	}
	return group + "/" + version
}
