# App Studio external-harness MCP surface (their harness, our tools)

Status: **Implemented (v1)** with a marked follow-up section.
Author: 2026-07-09
Related: [`app-studio-assistant-harness.md`](./app-studio-assistant-harness.md) (the in-house eino harness), [`code-provider-architecture.md`](./code-provider-architecture.md) (GitHub-backed repos), `providers/app-studio/api/mcp_server.go`, `providers/infrastructure/mcpserver/` (the pattern this mirrors), `providers/mcp/aggregate/provider_proxy.go` (federation).

## Summary

App Studio now exposes its app-building capabilities as **Model Context Protocol tools**, so an external coding harness — Claude Code, Cursor, Codex, any MCP client — can drive a kedge App Studio project with its own agent loop and its own local editor. This is the "**their harness, our tools**" path: the external harness brings planning, context management, edit tools, and verification UX; kedge brings the platform (projects, templates, sandbox, preview) as MCP tools.

It sits alongside the in-house App Studio assistant (the portal experience); it does not replace it. Non-technical users stay in the portal; developers drive kedge from their own tools.

## Design decision: git-native, no new git infra

A gap analysis established the file-editing model as the crux. kedge **hosts no git server** — project repositories live on **GitHub** (`providers/code` is a remote-API dispatcher; commits are GitHub Git Data API writes). So the natural fit is **git-native**:

1. The developer clones the project's **GitHub** repo locally with their GitHub credentials (kedge is not in that path).
2. They edit locally and push to GitHub using their harness's native tools.
3. kedge syncs the pushed commit into the dev sandbox and rebuilds the preview.

The only missing link was step 3: **kedge does not observe GitHub pushes** (there is no webhook/reconciler). Rather than build push-webhook infrastructure, v1 exposes an explicit **`sync_workspace_from_repo`** MCP tool the agent calls after pushing — which is a natural agent action and reuses the existing `hydrateWorkspaceFromRepository` path (repo → workspace → sandbox, already wired to fire the sandbox sync). An automatic GitHub-webhook auto-sync is a documented follow-up, not required for a complete loop.

## How it plugs in

- **New MCP server:** `providers/app-studio/api/mcp_server.go` — `Server.MCPHandler()` returns a stateless streamable-HTTP MCP handler (MCP Go SDK v1.3.1), building a fresh per-request server whose tool handlers close over the caller's tenant identity taken from the hub-proxy headers (`X-Kedge-Tenant` / `X-Kedge-Cluster` / `X-Kedge-User` + bearer). It lives in the `api` package so tool bodies reuse the existing project/workspace/runtime operations directly.
- **Mount:** `Server.Register` mounts it at `/mcp` (`api/server.go`). The hub forwards `/services/providers/app-studio/*` to App Studio's backend.
- **Federation (no catalog change):** the hub derives each ready provider's MCP URL as `backend.url + /mcp` (`pkg/hub/server.go`) and the aggregate MCPServer federates its tools with a provider-slug prefix (`providers/mcp/aggregate/provider_proxy.go`). Because App Studio's CatalogEntry already advertises `backend.url`, mounting `/mcp` is sufficient — the tools appear automatically as **`app-studio__*`** on the aggregate endpoint Claude/Cursor connect to (`kedge mcp url --mcpserver-name`), alongside `code__*` and `infrastructure__*`.

## Tools (v1)

All reuse existing App Studio operations; identity is taken from the bearer.

| Tool | Reuses | Risk |
|---|---|---|
| `list_projects` | `Projects().List` | read |
| `get_project` | `Projects().Get` + `projectRepositoryView` (GitHub clone URL) | read |
| `list_files` / `read_file` / `search_files` | `workspace.FileStore` | read |
| `sync_workspace_from_repo` | `hydrateWorkspaceFromRepository` (repo → workspace → sandbox) | mutating, idempotent |
| `verify_project` | `verifyProjectAssistantRuntime` | read |
| `get_runtime_logs` | `fetchProjectAssistantRuntimeLogs` | read |
| `get_runtime_status` / `get_preview_url` | runtime-status/preview graph lambdas | read |

## Packaging

`providers/app-studio/skills/kedge-app-studio/SKILL.md` — a ready-to-use Claude Code skill distilling the git-native loop and the domain rules from the App Studio system prompt (template = environment contract; don't provision what the template already provides; separate dev from production; cap fix-and-verify at ~3 attempts).

## What was NOT implemented (follow-ups)

- **Automatic push → sandbox sync** via a GitHub webhook receiver (or repo-HEAD poller) on `providers/code`, so a push rebuilds the preview without the explicit `sync_workspace_from_repo` call. v1 requires the agent to call the sync tool; the webhook is a convenience that removes that step. Needs webhook ingress + secret plumbing (cluster/GitHub-dependent, not unit-testable here).
- **Write/edit file tools over MCP** — intentionally omitted. Editing is git-native (local clone), so the MCP file tools are read-only; a server-side write path exists via `code__commit_files` for the no-clone case.
- **`deploy` on MCP** — production deployment handoff (`deploy_project_runtime`) is not yet exposed; it returns structured blockers until a runtime target is configured.
- **Live-cluster verification** — the tool wiring is unit-tested (identity, registration, summaries), but the end-to-end loop (clone → push → sync → preview) needs a live workspace + sandbox to exercise.

## Tests

`providers/app-studio/api/mcp_server_test.go`: header-identity parsing (incl. org/workspace derivation and empty-tenant), project-summary building, and that `MCPHandler()`/`newMCPServer` register all tools without panicking. `go build`, `go vet`, `go test ./...` pass.
