---
name: kedge-mcp
description: How to use the kedge aggregate MCP endpoint — what each provider (infrastructure, code, edges, kuery) does, how tools are namespaced, and the end-to-end flows for building, deploying, and operating a hosted app through MCP tools alone. Use when connected to a kedge MCP server, or when asked how to deploy/host/operate something on kedge.
---

# Using the kedge MCP

kedge exposes **one** MCP endpoint per tenant — the *aggregate MCPServer* — that
federates the tools of every enabled provider in the tenant workspace. You never
connect to providers individually.

## Connecting

```bash
# Prints the endpoint URL + ready-to-paste setup commands
kedge mcp url --name default

# Claude Code
claude mcp add --transport http kedge "<endpoint-url>" -H "Authorization: Bearer <token>"
```

Claude Desktop / other clients: HTTP transport, same URL, `Authorization: Bearer <token>` header.

Ground rules that apply to **every** tool:

- **Identity comes from the bearer token.** Tenant workspace, RBAC, everything.
  Never ask the user for a tenant path and never try to pass one.
- **Tools run as the caller.** There is no provider-wide identity; if a tool is
  denied, it is the caller's RBAC, not a provider outage.
- **Tool names are namespaced `<provider>__<tool>`** (e.g.
  `infrastructure__provision`, `code__commit_files`). The tool list is composed
  **per request** from providers that are currently enabled and healthy — if an
  expected tool is missing, re-list tools before concluding it doesn't exist;
  the provider may be down or the edge tunnel disconnected.
- The server's `initialize` instructions carry per-provider and per-service
  guidance (e.g. a Home Assistant service's entity naming). Read them; they are
  authoritative for the current deployment.

## The providers

| Provider | What it does | Start with |
|---|---|---|
| `infrastructure` | Provisions curated kro templates (apps, databases, workers) into the tenant; instances get reconciled, exposed, and reported back with URLs | `list_templates` |
| `code` | Manages git repositories (GitHub today): create repos, commit files, watch CI builds | `list_connections` |
| `edges` | Live Kubernetes access to every connected edge cluster (pods, logs, exec, helm, apply) plus per-Service tools | `pods_list` |
| `kuery` | Fleet-wide indexed object search and impact analysis across all edges | `kuery_query` |

### infrastructure — provision things that run

Brokers a catalog of kro templates. The loop is always:

1. `infrastructure__list_templates` — what can be deployed (filter by category/cloud).
2. `infrastructure__describe_template` — the template's **inputs JSON-schema**
   plus its `agent.usage`, `agent.prerequisites`, and `agent.outputs`. **Treat
   `agent.usage` as the environment contract** — it tells you exactly what the
   platform injects (env vars, routing, DB) and what your images must do. Never
   guess inputs; describe first.
3. `infrastructure__provision` — creates the instance CR; the backend reconciles it.
4. `infrastructure__get_instance` — phase, conditions, child-resource status,
   and computed outputs (`status.url` for exposed apps). Poll this until Ready.
5. `infrastructure__list_instances` / `infrastructure__delete_instance` to manage.

Built-in template catalog (choose by shape, per each template's own guidance):

- **`application`** — 3-tier: frontend + backend + managed Postgres on ONE
  public URL (`/` → frontend, `/api/*` → backend), optional OIDC gate
  (oauth2-proxy). You supply exactly two images.
- **`simple-webapp`** — ONE container serving HTTP on one public URL. Static
  sites, SPAs, self-contained servers. Development-capable.
- **`worker`** — ONE container, long-running, **no network exposure** (bots,
  queue consumers, pollers). Entrypoint must run forever.
- **`cron-job`** — container that runs on a cron schedule (UTC) and exits. No URL.
- **`database`** — standalone PostgreSQL; consumers read the URI from Secret
  `<name>-db-credentials` key `uri`.
- **`redis-cache`** — ephemeral Redis (StatefulSet + Service).

Cloud-backed templates read credentials from a `cloud-credentials` Secret in
the workspace's default namespace; if missing, ask the user to create it in the
portal — don't invent an alternative.

### code — repositories and builds

Objects: a **Connection** holds the credential for one git account;
**Repositories**, **DeployKeys**, and **Collaborators** reference a Connection.

- Discover: `code__list_connections`, `code__list_repositories`.
- Create: `code__create_connection` (references a Secret — the token itself is
  pasted in the **portal**, never transported over MCP), `code__create_repository`.
- Content: `code__commit_files` (UTF-8 text files → provider-owned commit),
  `code__checkout_repository` (read a repo's text tree at a ref).
- CI: `code__build_status` (latest workflow run + per-job outcome + failed-job
  log tail), `code__rebuild` (re-dispatch without a code change).
- Access: `code__add_deploy_key`, `code__add_collaborator`, `code__remove_collaborator`.

Gotcha: committing `.github/workflows/*` files requires the GitHub connection
token to have the **`workflow`** scope — without it the commit 404s. If a
workflow commit fails, check that before anything else.

### edges — live Kubernetes on connected clusters

Kubernetes tools across every connected `KubernetesCluster` edge:
`pods_list`/`pods_get`/`pods_log`/`pods_exec`/`pods_run`/`pods_delete`/`pods_top`,
`resources_list`/`resources_get`/`resources_create_or_update`/`resources_delete`/`resources_scale`,
`helm_list`/`helm_install`/`helm_uninstall`, `nodes_top`/`nodes_log`,
`namespaces_list`, `events_list`, `configuration_view`.

Additionally, each **Ready Service** on an edge contributes tools named
`<service>_*` (e.g. a Home Assistant service `ha` gives `ha_states` /
`ha_call_service`; qBittorrent `qb` gives `qb_torrents` / `qb_add`). Service
tools exist **only while the edge tunnel is live** — a missing tool set means
the edge is disconnected, not that the service was deleted.

### kuery — fleet-wide search, one call

- `kuery__kuery_query` — "which edges run image X", "all deployments with label
  Y across the fleet" — answered from a local index in ONE call. **Prefer it
  over per-edge `pods_list` round-trips** for any cross-fleet question.
- `kuery__kuery_impact` — declared blast radius of one object (owners,
  descendants, spec references, selector matches). Reliable for
  "who consumes this ConfigMap"; it does NOT see network-level coupling.

Caveat: the index syncs from connected edges — a just-connected edge may not be
fully indexed yet. For real-time truth on one edge, use `edges` tools.

## Recipe: ship a hosted app end-to-end

Goal: user says "build me a todo app and host it".

**Phase 1 — decide the shape.** Map business intent to a template: needs an API
+ database → `application`; single container / static → `simple-webapp`;
headless bot → `worker`. Confirm with
`infrastructure__describe_template` and follow its `agent.usage` as the contract.

**Phase 2 — source & image** (skip if the user already has an image):

1. `code__list_connections` — need a validated GitHub connection. If none, the
   user must connect an account in the portal (paste token there); you cannot do
   it over MCP.
2. `code__create_repository` under that connection.
3. Write the app **to the template's contract** (see below), plus a Dockerfile
   and a `.github/workflows/` build workflow that pushes an image (e.g. to
   ghcr.io). Commit everything with `code__commit_files` (remember the
   `workflow`-scope gotcha).
4. Poll `code__build_status` until the build succeeds; on failure read the
   failed-job log tail, fix, re-commit (or `code__rebuild` for flakes).

**Phase 3 — provision:**

5. `infrastructure__provision` with the built image(s) as inputs
   (e.g. `application`: `frontendImage` + `backendImage`; `simple-webapp`: `image`).
6. Poll `infrastructure__get_instance` until Ready; give the user
   **`status.url`** — that is the hosted app.

**Phase 4 — verify & operate:**

7. `edges__pods_list` / `edges__pods_log` if something isn't healthy;
   `kuery__kuery_query` to find where it landed; `edges__events_list` for
   scheduling/pull errors. Private ghcr images need a pull secret — check
   events for `ImagePullBackOff`.

### App-code contract (what the platform expects of your images)

These come from the templates' own agent guidance — violating them is the #1
cause of "provisioned but broken":

- Bind to **`0.0.0.0`**, never 127.0.0.1; honor the injected **`$PORT`** (default 8080).
- **Stateless** — no local disk; state goes in Postgres (the template provisions it).
- `application` backend: read **`DATABASE_URL`** from env (never a Secret),
  append **`sslmode=disable`** (in-cluster Postgres has no TLS), **retry** the
  initial connection, and run migrations idempotently — the DB starts empty.
- `application` routing: backend receives paths **with** the `/api` prefix;
  frontend calls the API same-origin (`fetch("/api/…")`) — there is no separate
  backend URL, do not invent a backend host env var.
- `worker`: the entrypoint must run forever; exiting = crash-loop, not completion.

## Troubleshooting quick table

| Symptom | Check |
|---|---|
| Tool you expected is missing | Re-list tools; provider not Ready or edge tunnel down |
| `provision` succeeded but no URL | `get_instance` — phase/conditions; URL appears in `status.url` when reconciled |
| Workflow-file commit fails | GitHub token lacks `workflow` scope |
| Build red | `build_status` failed-job log tail; `rebuild` for flakes |
| Pods `ImagePullBackOff` | Private image without pull secret; `events_list` |
| App up but 502 | Bound to 127.0.0.1 or wrong port — re-read the template contract |
| Cloud template fails | `cloud-credentials` Secret missing in default namespace |
| Fleet question is slow | You're looping edges — use `kuery_query` instead |
