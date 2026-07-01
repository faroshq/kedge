# Databricks provider

A kedge provider that exposes imported Databricks SQL Warehouse tables to kedge
workspaces. The provider owns Databricks `Connection`, `Warehouse`, and `Table`
resources in the tenant workspace. App Studio can inspect existing `Table`
resources by `tableRef` for design-time guidance; generated apps do not yet have
a sanctioned runtime data-access bridge to Databricks.

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
  Execution for the provider portal and hub-federated Databricks MCP tool.
- Portal UX for creating and updating `Connection`, `Warehouse`, and `Table`
  handles, plus a read-only table preview.
- Multicluster controllers validate PAT credentials against the Databricks
  current-user API, validate SQL warehouse handles, refresh table schema status,
  and write `Validated` / `Ready` conditions.
- Tenant scoping deliberately splits authority: the caller token authorizes the
  requested `Table`, then the provider's accepted APIExport permission claims
  resolve referenced `Warehouse`, `Connection`, and credential `Secret`
  resources. Databricks credentials are never returned to App Studio, generated
  apps, or browser clients.

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

App Studio can then discover `order-history` as a `tableRef` for design-time
metadata and user-facing planning.

## Runtime query contract

The provider portal, backend, and hub-federated `databricks__query_table` tool
accept a structured request:

```json
{
  "columns": ["order_id", "total_amount"],
  "filters": [{ "column": "status", "operator": "=", "value": "shipped" }],
  "orderBy": [{ "column": "order_date", "direction": "desc" }],
  "limit": 100
}
```

The provider validates identifiers, caps `limit` at 1000, converts filters to
Databricks named parameters, authorizes the table reference as the caller,
resolves credentials through the provider's accepted permission claims, and
posts to `/api/2.0/sql/statements` with inline `JSON_ARRAY` results.

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
- Generated-app runtime access is not implemented yet. Do not hardcode provider
  backend URLs or Databricks credentials into App Studio-generated source.
- OAuth federation and service-principal token exchange should be reconciled
  into token-bearing Secrets before query execution.
