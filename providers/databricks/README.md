# Databricks provider

A kedge provider that exposes imported Databricks SQL Warehouse tables to App
Studio and generated apps. The provider owns Databricks `Connection`,
`Warehouse`, and `Table` resources in the tenant workspace. App Studio consumes
existing `Table` resources by `tableRef`; it does not import tables or handle
Databricks credentials.

## What works today

- Tenant-facing CRDs for:
  - `Connection`: Databricks workspace host plus a tenant Secret reference.
  - `Warehouse`: SQL warehouse handle.
  - `Table`: stable imported table handle with cached schema metadata.
- MCP tools at `/mcp` and `/mcp/sse`, federated through the hub as:
  - `databricks__list_tables`
  - `databricks__describe_table`
  - `databricks__query_table`
- Read-only structured query execution through Databricks SQL Statement
  Execution. Generated apps send `tableRef` plus a bounded structured query;
  they never receive Databricks credentials or raw warehouse auth config.
- Portal UX for creating and updating `Connection`, `Warehouse`, and `Table`
  handles, plus a read-only table preview that exercises the same provider query
  endpoint generated apps use.
- Multicluster controllers validate PAT credentials against the Databricks
  current-user API, validate SQL warehouse handles, refresh table schema status,
  and write `Validated` / `Ready` conditions.
- Tenant scoping follows the provider-code/provider-infrastructure pattern:
  the provider kubeconfig supplies host/TLS only, and every tenant resource read
  uses the caller bearer token and `X-Kedge-Cluster`.

## Current import path

Users can import a table from the provider portal by creating a Connection, a
Warehouse, and a Table handle. A user or admin can also create those tenant
resources directly:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: sales-databricks-token
  namespace: data-creds
type: Opaque
stringData:
  token: "<databricks bearer token>"
---
apiVersion: databricks.kedge.faros.sh/v1alpha1
kind: Connection
metadata:
  name: sales-workspace
spec:
  host: "https://dbc-xyz.cloud.databricks.com"
  authType: pat
  secretRef:
    name: sales-databricks-token
    namespace: data-creds
    key: token
---
apiVersion: databricks.kedge.faros.sh/v1alpha1
kind: Warehouse
metadata:
  name: sales-warehouse
spec:
  connectionRef: sales-workspace
  warehouseID: "abc123def456"
---
apiVersion: databricks.kedge.faros.sh/v1alpha1
kind: Table
metadata:
  name: order-history
spec:
  connectionRef: sales-workspace
  warehouseRef: sales-warehouse
  catalog: sales
  schema: gold
  table: order_history
```

App Studio can then discover and use `order-history` as the `tableRef`.

## Runtime query contract

The hub-federated `databricks__query_table` tool and
`POST /api/tables/{tableRef}/query` accept a structured request:

```json
{
  "columns": ["order_id", "total_amount"],
  "filters": [{ "column": "status", "operator": "=", "value": "shipped" }],
  "orderBy": [{ "column": "order_date", "direction": "desc" }],
  "limit": 100
}
```

The provider validates identifiers, caps `limit` at 1000, converts filters to
Databricks named parameters, resolves the table target as the caller, and posts
to `/api/2.0/sql/statements` with inline `JSON_ARRAY` results.

Connection hosts must be Databricks workspace root URLs over HTTPS. The backend
allows the standard Databricks workspace domains by default; set
`DATABRICKS_ALLOWED_HOST_SUFFIXES` only when the deployment deliberately supports
private Databricks workspace domains.

## Local development

```sh
make build-databricks-provider
make install-provider-databricks
make init-provider-databricks
make run-provider-databricks
```

For a no-kcp smoke test only, set `DATABRICKS_DEV_STATIC_TABLES=true`. That mode
uses a seeded `order-history` table and a stub backend; normal serve mode fails
closed if tenant table lookup or Databricks credentials are unavailable.

## Gaps

- Catalog/schema discovery is not implemented yet; the first UX imports a known
  table by reference.
- OAuth federation and service-principal token exchange should be reconciled
  into token-bearing Secrets before query execution.
