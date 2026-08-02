---
name: kedge-create-app
description: Build and host an app on kedge directly from the Claude Code harness using the kedge MCP — iterate in a live dev sandbox (kedgeMode development + dev_sync hot reload, no image builds), keep code in a kedge-managed repo (code provider), then ship by building an image (local docker or repo CI) and provisioning the template in production mode. Use when a kedge MCP server is connected and the user asks to create, deploy, host, or live-develop an app (instead of using the in-browser App Studio builder).
---

# Create a hosted app on kedge (from the Claude harness)

You are in Claude Code with local tools (files, Bash, git, docker) **and** a
kedge MCP server. That combination replaces App Studio's in-browser builder:

- **You** do what App Studio's assistant does — write the code, produce the image.
- **kedge** does the rest — the `code__*` tools manage the git repo and CI
  builds; the `infrastructure__*` tools provision a template instance and hand
  back a public URL.

Two providers are in play: **infrastructure** (always) and **code** (whenever
the app has a repo — which it should). Identity comes from the MCP bearer
token — never ask the user for a tenant path or workspace.

## The loop

```
intent → pick template → describe (contract)
  → DEV: provision kedgeMode=development → edit → dev_sync → dev_logs → preview URL   (iterate here)
  → SHIP: repo (code provider) → image (CI or local docker) → provision production → URL
```

Iterate in the **dev sandbox** (hot reload, no image builds); build an image
only when shipping for real.

### 1. Pick the template by app shape

Call `infrastructure__list_templates` first (also confirms the MCP link works).
Map the user's intent to a shape — don't ask them to choose infrastructure:

| User wants | Template | You supply |
|---|---|---|
| Web app with API + persistent data | `application` | 2 images (frontend + backend); Postgres is managed |
| Static site / SPA / single self-contained server | `simple-webapp` | 1 image |
| Bot, poller, queue consumer (no HTTP exposure) | `worker` | 1 image, runs forever |
| Runs on a schedule and exits | `cron-job` | 1 image + cron expr (UTC) |
| Just a database / cache next to something existing | `database` / `redis-cache` | nothing app-side |

### 2. `infrastructure__describe_template` — read the contract

Do this **before writing any code**. The response carries the inputs
JSON-schema plus `agent.usage` / `agent.prerequisites` / `agent.outputs` —
that text is the **environment contract**: what the platform injects, what your
image must do, where outputs land. Treat it as authoritative over anything in
this skill; never guess inputs.

If the response has a **`development` block**, the template supports a live
dev sandbox — use it as your inner loop (step 3). `development.components`
maps each component to the **workspace directory** it syncs from (e.g.
frontend → `web/`, backend → `api/`; `.` = workspace root). Lay your source
tree out by that map — files outside every component directory never reach
the sandbox.

### 3. Develop live in a dev sandbox (no image builds)

For development-capable templates, provision the sandbox **first** and iterate
against it:

1. `infrastructure__provision` with `values: {"kedgeMode": "development"}` —
   image inputs may be omitted; backing services (Postgres, `DATABASE_URL`)
   run exactly as in production. Poll `get_instance` until Ready; its
   `status.url` is the live preview on the same URL a production instance
   would get.
2. Write code locally (to the contract, step 4), laid out by the
   `development.components` directory map.
3. After **every edit batch**, push the changed files with
   `infrastructure__dev_sync` (workspace-relative paths; hot reload is
   automatic, `restart: "auto"` handles dependency changes). Sync only what
   changed — not the whole tree every time.
4. Broken? `infrastructure__dev_logs` (per component) shows the dev-server
   output; `infrastructure__dev_restart` force-restarts a wedged process.
5. Reload the preview `status.url` and confirm the change is live.

The dev servers are Node.js-based (vite / `npm run dev`) — for other stacks,
or when no `development` block exists, skip to the image path below.

### 4. Write the app locally, to the contract

Scaffold in a normal local project directory. The recurring contract rules —
violating these is the #1 cause of "provisioned but broken":

- HTTP servers bind **`0.0.0.0`** (never 127.0.0.1) and honor the injected
  **`$PORT`** (default 8080).
- **Stateless** — no local/persistent disk; pods restart and scale. State goes
  in the template-managed Postgres.
- `application` backend: read **`DATABASE_URL`** from env (never a Secret),
  connect with **`sslmode=disable`** (in-cluster Postgres has no TLS — Node
  `pg`, JDBC etc. default to requiring it), **retry** the initial connection
  (Postgres boots alongside you), create schema / migrate **idempotently** —
  the DB starts empty.
- `application` routing: one public URL; `/` → frontend, `/api/*` → backend
  with the `/api` prefix **preserved**. The frontend calls the API same-origin
  (`fetch("/api/items")`) — there is no backend host env var; don't invent one.
- `worker`: the entrypoint runs forever; exiting is treated as a crash, not
  completion.

Write a Dockerfile per image. Keep it boring: small base, `EXPOSE` matches the
port input, `CMD` starts the server.

### 5. Get the code into a kedge-managed repo (code provider)

The code provider's model: a **Connection** holds the credential for one git
account (GitHub today); **Repositories** are created under it. The account
token is pasted in the **portal** — it never travels over MCP.

1. `code__list_connections` — need a validated connection. If none, stop and
   ask the user to connect a GitHub account in the portal; you cannot do it here.
2. `code__create_repository` under that connection (the provider creates it on
   GitHub and reports its URLs).
3. `code__commit_files` — commit the source, Dockerfile(s), and the CI
   workflow (step 6). UTF-8 text files only; binaries are skipped, so generate
   assets in CI rather than committing them.
4. Working with existing code: `code__checkout_repository` returns a repo's
   text tree at a ref — use it to import/inspect a repo the user already has
   under a connection, then commit changes back with `code__commit_files`.

**Gotcha:** committing anything under `.github/workflows/` requires the
connection's GitHub token to have the **`workflow`** scope — without it the
commit fails. If a workflow commit errors, check that first and tell the user
to re-issue the token with `workflow` scope in the portal.

### 6. Produce a pullable image

Two paths — prefer **A** when the repo exists (no local docker needed, and it
matches how App Studio builds):

**A. CI build in the repo (code provider).** Commit a GitHub Actions workflow
that builds and pushes on every push to the default branch (plus
`workflow_dispatch`, so `code__rebuild` can re-trigger it). The pattern App
Studio itself uses: build with Docker or Railpack, push to
**`ghcr.io/<owner>/<repo>`** (one image per component for multi-image apps,
e.g. `ghcr.io/<owner>/<repo>/frontend`), tagged **`sha-<commit>`**, with
`permissions: { contents: read, packages: write }` so the built-in
`GITHUB_TOKEN` can push — no extra registry secret needed. Then:

- Poll `code__build_status` (optionally per commit) — run status, per-job
  outcome, and a **log tail for any failed job**. Fix, re-commit; use
  `code__rebuild` only for flaky reruns without a code change.
- The provisionable image ref is `ghcr.io/<owner>/<repo>[/<component>]:sha-<commit>`.
- ghcr packages inherit **private** visibility from private repos. Zero-config
  hosting needs the package (or repo) public — otherwise the runtime cluster
  needs a pull secret; surface that to the user instead of provisioning
  something that can't pull.

**B. Local docker build.** When there's no repo or the user wants speed:

```bash
docker buildx build --platform linux/amd64 -t <registry>/<app>:<tag> --push .
```

`--platform linux/amd64` is **mandatory on Apple Silicon** — an arm64 image
manifests as `ImagePullBackOff`/`exec format error` on the runtime cluster.
Same pullability rule: public image or a pull secret.

Either path: use **immutable tags** (git SHA), not `:latest` — there is no
in-place instance update (step 7), and tags are how you reason about what's
running.

### 7. `infrastructure__provision` (production)

Provide `template`, `name`, and `values` matching the described schema.
Example for `application` with CI-built images:

```json
{
  "template": "application",
  "name": "todo",
  "values": {
    "frontendImage": "ghcr.io/acme/todo/frontend:sha-abc1234",
    "backendImage": "ghcr.io/acme/todo/backend:sha-abc1234",
    "oidc": { "mode": "none" }
  }
}
```

- `oidc.mode: none` = public URL, right for demos/dev. For a login-gated app
  use `byo` — the user supplies `oidc.issuerURL` + `oidc.clientID` and puts the
  client secret in the `cloud-credentials` Secret (portal); register the
  callback from `status.redirectURL` with their IdP.
- App configuration goes in the `env` inputs (`env` on single-container
  templates; `frontendEnv` / `backendEnv` on `application`) — a plain string
  map, updatable later in place. **Not for secrets** (stored world-readable);
  platform vars (`PORT`, `DATABASE_URL`) win on name conflicts.
- Don't set platform-stamped fields (`expose.fqdn`, `credentialsSecretName`).
- `provision` fails with "already exists" if the name is taken — pick a new
  name or see step 7 for updates.

Then poll `infrastructure__get_instance` until Ready (per-tier readiness on
`application`: `frontendReady` / `backendReady` / `databaseReady`). The
deliverable is **`status.url`** — `curl` it from the harness and confirm the
app answers before declaring success.

### 8. Iterating on a live app

**Code iteration belongs in the dev sandbox (step 3)** — sync, reload, done.
Shipping a new **production** version: new commit → CI builds `sha-<newcommit>`
(path A) or a local rebuild with a new tag (path B), then roll it in place:

```json
infrastructure__update_instance
{ "name": "todo", "values": { "frontendImage": "ghcr.io/acme/todo/frontend:sha-def5678" } }
```

`update_instance` is a merge patch — send only what changes, `null` unsets —
and the backend reconciles the delta as a rolling update: **same instance,
same URL, managed Postgres and its data untouched**. It works for images,
`env` maps, ports, replicas, `schedule`, and `oidc.*`. Never delete+re-provision
just to roll a version.

Immutable (rejected with a reason): `name`, `kedgeMode` (dev↔production is a
separate instance, not an edit), platform-stamped fields, and
template-declared ones like `database.version` (a Postgres major upgrade is
not in-place). Those genuinely need a recreate — and deleting an `application`
instance deletes its managed Postgres, so warn the user first and suggest a
standalone `database` instance if the data matters.

## Troubleshooting

| Symptom | Likely cause / next call |
|---|---|
| `infrastructure__*` / `code__*` tools missing | Re-list MCP tools; provider disabled or unhealthy — not your call pattern |
| No usable connection | User must paste a GitHub token in the portal; MCP never transports tokens |
| Workflow-file commit fails | Connection token lacks the `workflow` scope — re-issue in the portal |
| Build red | `code__build_status` failed-job log tail; `code__rebuild` only for flakes |
| Instance stuck, no URL | `get_instance` conditions; URL only appears once reconciled |
| `ImagePullBackOff` | Private ghcr package without pull secret, wrong tag, or arm64 image (`--platform linux/amd64`) |
| URL up but 502 | App bound to 127.0.0.1 or wrong port — re-read the contract, fix, rebuild, redeploy |
| Backend crash-loops at start | DB not ready and app exits instead of retrying, or missing `sslmode=disable` |
| API 404s | Backend serving routes without the `/api` prefix, or frontend hardcoding a backend host |
| Cloud template errors | `cloud-credentials` Secret missing in the workspace default namespace (user creates it in the portal) |
| `update_instance` rejects a field | The field is immutable (name, kedgeMode, database.version, platform-stamped) — the error says why; recreate only for those |
| `dev_sync` rejects every file | Source tree doesn't match the `development.components` directory map — the error lists the expected layout; restructure, don't fight it |
| `dev_*` tools error "not in development mode" | Instance was provisioned without `kedgeMode: development` — provision a dev instance (new name) for iteration |
| Dev preview stale after sync | `dev_logs` for the component — the dev server may have crashed on the new code; `dev_restart` if wedged |

## When to hand off instead

If the user wants an **in-browser builder** — editing in the portal with a
persistent project assistant, no local tools at all — that is App Studio's
flow, not this one. Point them at the portal; this skill is the harness-native
path: local code, dev-sandbox iteration over MCP, repo + CI via the code
provider, MCP-provisioned hosting.
