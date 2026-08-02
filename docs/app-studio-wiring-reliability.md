# App Studio: closing the wiring-reliability gap

Status: research + proposal (2026-07-31). Companion to
`docs/app-studio-template-sandboxes.md` and
`docs/app-studio-provider-improvement-plan.md`.

## Problem

Generated apps fail to run far too often, and the dominant failures are not
logic bugs — they are wiring: backend listening on the wrong port, frontend
calling a hardcoded backend URL, database config invented instead of read from
the injected `DATABASE_URL`, apps that work in the dev sandbox but crash when
the Railpack production image runs. This matches published benchmark data:
WebGen-Bench (NeurIPS 2025) found the best agent stacks pass only ~28% of
functional tests on build-from-scratch web tasks, with **more than half of all
failures being launch/serve failures, not business logic**.

## What the research says (three studies, one conclusion)

### 1. Successful vendors remove degrees of freedom; they don't prompt harder

- **Lovable / Bolt / v0** deleted the wiring problem structurally: fixed stack
  (Vite SPA or Next.js), no arbitrary backend (Supabase/BaaS or same-origin
  API routes), platform-provisioned services with credentials injected through
  one fixed channel. The LLM never writes a hostname because no hostname
  exists in its world.
- **Replit Agent** is the closest analog to kedge (arbitrary backend + real
  Postgres) and is instructive on every axis:
  - a **fixed port contract** (`bind 0.0.0.0:5000`, mapped to external 80)
    enforced by the deployment health check — the whole wrong-port failure
    class is eliminated by one convention the scaffolds hardcode;
  - **platform-provisioned Neon Postgres** with `DATABASE_URL`/`PG*`
    auto-added to secrets; the agent scaffolds the ORM, never the DSN;
  - a **closed verification loop**: a feedback tool loads the route, captures
    screenshot + logs, and a separate verifier agent decides "done" — since
    Agent 3, real browser-automation tests gate success;
  - **dev/prod DB separation with automated migration diffs** after their
    July 2025 prod-deletion incident.

### 2. Infra platforms converge on six principles

From Railway (reference variables, `RAILWAY_PRIVATE_DOMAIN`), Render
(`fromService`/`fromDatabase` in render.yaml, health-check-gated deploys),
Heroku/12-factor (`PORT`, `DATABASE_URL` by attachment), servicebinding.io
(`SERVICE_BINDING_ROOT`, typed bindings), Score/Radius (declared dependencies,
`CONNECTION_<NAME>_<PROPERTY>` injection):

a. **Injected env over generated config** — app code reads names, platform
   owns values.
b. **Well-known variable names** — `PORT`, `DATABASE_URL`, `PG*`; both a URL
   form and decomposed components; apps pick one, generate neither.
c. **References resolved at deploy time, not codegen time** — templates emit
   `${{Postgres.DATABASE_URL}}`-style references; the model literally cannot
   write a wrong hostname.
d. **Same-origin routing beats URL config** — Vercel/Netlify `/api/*` means
   the frontend needs zero configured URLs and zero CORS.
e. **Health checks define "deployed"** — Render only shifts traffic after the
   declared endpoint returns 200, and rolls back otherwise. This is also the
   objective, retryable success signal an agent iterates against.
f. **Detection infers run config from code** — buildpacks/Railpack normalize
   the start command onto `$PORT` even when the code forgot.

Platforms courting AI agents (Railway agents docs, Render MCP templates) add:
give the agent read tools for ground truth, ship an AGENTS.md of conventions
with the template, and use the health check as the machine-checkable
definition of success.

### 3. Our own audit: the contract exists, but only as prose, and nothing enforces it

The `application` template already implements most of the right design —
same-origin `/api/*` (no `BACKEND_URL` by design), injected `PORT` and
`DATABASE_URL` (secret-sourced), single-instance graph with fixed cluster DNS.
The failures come from the gap between that design and what the model/tools
actually see and verify:

| # | Gap | Evidence |
|---|-----|----------|
| 1 | The runtime contract (port 8080, `$PORT`, `/api` same-origin, `sslmode=disable`) exists **only as English prose** in `Template.spec.agent.usage` (`providers/infrastructure/install/templates/application.yaml:36-136`). The machine-readable snapshot (`providers/app-studio/api/project_template.go:77-101`) carries a *named* port ("backend"), not the number, and no env list. | audit §3 |
| 2 | The frontend dev shim forces `--port $PORT`; the **backend dev start command is bare `npm run dev \|\| npm start`** (`application.yaml:354`) — a backend that hardcodes `listen(3000)` 502s silently. No runtime-issue matcher covers it (`providers/app-studio/api/runtime_issues.go:152-236`). | audit §4.1 |
| 3 | **Preview "ready" is a lie**: `probePreviewEdge` treats any status < 520 as served — including the gateway 502 caused by gap 2 — and the tool result tells the model application readiness "is not independently reported" (`providers/app-studio/api/preview_edge.go:113-135`, `assistant_workflow.go:1019-1024`). The model concludes success while the app is down. | audit §4.9 |
| 4 | **No scaffolds.** `Template.spec.scaffold` exists (`types_template.go:269-275`) but no template ships one and no code reads it. Every project bootstraps from nothing — exactly the failure class WebGen-Bench says dominates. | audit §3 |
| 5 | **Dev ≠ prod entrypoint**: dev runs `npm run dev` (vite), prod runs whatever Railpack infers; a package.json without a `start` script builds an image that exits. Build gate checks only that a ghcr digest exists (`project_build_status.go:283-363`) — never that the image runs. | audit §4.5 |
| 6 | **Dev ≠ prod URL/auth**: `<project>-dev-<hash>` vs `<project>-prod-<hash>` hostnames; any absolute URL/redirect baked from preview breaks on promote. Promote with `oidc.mode=byo` also changes `/api` routing semantics (HTTPRoute dropped in favor of oauth2-proxy upstream). No parity check exists. | audit §4.3-4.4 |
| 7 | **Sibling services are not wired.** A separately provisioned `database` instance publishes `status.connectionSecretRef`, but nothing can inject it into an app: `set_runtime_env` is dev-only, in-memory, and blocks secret-looking names; nothing ever writes `frontendEnv`/`backendEnv`. There is no deploy-time reference mechanism (principle c). | audit §4.6, §2 |
| 8 | Doc drift: `docs/application-template-architecture.md:226` still documents `BACKEND_URL` + Ingress, contradicting the shipped template. Anything reading docs learns the wrong contract. | audit §4.11 |

## Proposal

Ordered by leverage. Phases 0–1 attack the two mechanisms every successful
vendor shares: **start from a working app** and **never report success that a
health check didn't confirm**.

### Phase 0 — Start green: scaffolds (highest single win)

Ship real scaffolds for the `application` and `simple-webapp` templates and
make project creation materialize them into the workspace before the first
model turn. The scaffold is a minimal but **already-deployable** app:

- backend: listens on `process.env.PORT`, `0.0.0.0`; db client built from
  `process.env.DATABASE_URL` with `sslmode=disable` handled; `/api/health`
  route; package.json with **both** `dev` and `start` scripts (Railpack-safe).
- frontend: relative `fetch('/api/...')` only; vite config with a dev `/api`
  proxy so local reasoning matches the gateway topology.
- a stamped `AGENTS.md` (Render's pattern) in the repo root stating the
  contract in imperative form: read `PORT`, read `DATABASE_URL`, call `/api/*`
  same-origin, never write absolute URLs, keep `dev` and `start` working.

The model's job shifts from "bootstrap a full-stack app" (the dominant
published failure mode) to "edit a working one". Wire `spec.scaffold` (the
field already exists) → materialize on project create → include `AGENTS.md`
content in the turn snapshot.

### Phase 1 — Honest signals: enforce and verify the contract

1. **Backend port shim**: give the backend dev command the same treatment as
   the frontend — force the port (`--port $PORT` for known toolchains) and/or
   have the dev-agent verify a listener exists on `KEDGE_DEV_PORT` after
   start, reporting a typed runtime issue ("process is listening on 3000,
   expected 8080") through the existing `runtime_issues.go` classifier set.
2. **App-level readiness, not edge reachability**: dev-agent probes
   `/api/health` (scaffold guarantees it exists); `probePreviewEdge` stops
   counting 502 as served; `verify_development_runtime` returns
   listener + health + recent-log evidence. "Preview ready" must mean the app
   answered, because it is the signal the model iterates against (principle e).
3. **Static sync gates**: extend `validateProjectSyncToolchains` to require a
   `start` script (closes gap 5 cheaply) and warn on absolute
   `http://localhost` / dev-hostname literals in synced source (closes most of
   gap 6 at authoring time, when the model can still fix it).
4. **Machine-readable contract in the snapshot**: add numeric port, injected
   env-var names, and the same-origin rule to `projectTemplateComponent` /
   `describe_template` structured output — stop relying on 12k chars of prose
   the model may skim.

### Phase 2 — Parity: promote must not change the app's world

- **Promote smoke test**: before flipping traffic, run the built image once
  (dev cluster job or ephemeral instance) and require `/api/health` 200 —
  Render's health-gated deploy, applied at promotion. This is the machine
  check for gap 5 that the static gate can't fully cover.
- **Parity lints at promote**: refuse/warn when synced source contains the dev
  hostname; surface the `oidc.mode` routing-semantics change explicitly in
  the promote result so the model (and user) know `/api` handling changed.
- **Persist runtime env**: `set_runtime_env` writes through to the instance's
  `frontendEnv`/`backendEnv` values (merge-patch via `update_instance`) so
  environment survives restarts and carries to production instead of silently
  evaporating.

### Phase 3 — Bindings: deploy-time references for sibling services

Adopt principle (c) for the multi-instance case: an `application` instance
declares consumed services, the platform resolves them.

```yaml
values:
  bindings:
    analytics-db:            # env prefix
      instance: metrics-db   # sibling infrastructure instance
```

The infrastructure controller resolves the target instance's
`status.connectionSecretRef` and projects it (`ANALYTICS_DB_URL`, or
servicebinding.io-style mounted files) into the workload — Railway's
`${{Postgres.DATABASE_URL}}` / Radius `CONNECTION_<NAME>_<PROPERTY>`, in kedge
terms. This makes the `database` template actually consumable and removes the
last place the model would hand-carry credentials.

### Phase 4 — Verification loop (Replit parity)

A post-change feedback tool that loads the preview route, captures a
screenshot + console/network errors + pod logs, and feeds them back before the
turn declares success; later, scripted browser checks gating promote. The
`browser` template already in the catalog is a plausible substrate.

### Cleanups

- Fix or delete the stale `docs/application-template-architecture.md`
  (documents `BACKEND_URL`/Ingress that don't exist).
- Backend `agent.usage` prose stays, but as commentary on the structured
  contract, not its only carrier.

## What we deliberately keep

Same-origin `/api/*` with no `BACKEND_URL` is the *correct* design (it is
exactly what Vercel/Netlify/Replit converge on) — the fix is enforcing and
verifying it, not adding a backend URL variable. Likewise single-instance
graphs with in-graph Postgres and injected `DATABASE_URL` match the
Heroku-lineage convention; bindings (Phase 3) extend it rather than replace it.

## Success metric

Track per-project: (a) first-preview health-check pass rate, (b) preview→
promote success rate, (c) count of turns spent on wiring-class runtime issues
(port/URL/db matchers). Phase 0+1 should move (a) dramatically — vendors that
adopted scaffold+health-gate report the serve-failure class essentially
disappearing from their error distribution.
