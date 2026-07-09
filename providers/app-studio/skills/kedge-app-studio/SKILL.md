---
name: kedge-app-studio
description: Build and ship apps on a kedge App Studio project from your own coding harness (Claude Code, Cursor, Codex). Use when the user wants to work on a kedge-hosted app — clone its repo, edit locally, push, sync the dev sandbox, and check the live preview — via the kedge MCP tools (app-studio__*, code__*, infrastructure__*).
---

# Building on kedge App Studio

kedge App Studio is a platform where each **project** is a chat-built application backed by a **GitHub repository** and a live **development sandbox** that serves a preview. This skill lets you drive a project from your own harness using kedge's MCP tools, instead of the App Studio web portal.

## Setup (once)

Get the aggregate MCP endpoint for your workspace and add it to your harness's MCP config:

```
kedge mcp url --mcpserver-name default
```

Add the printed URL as an MCP server (e.g. in `claude_desktop_config.json` or your Cursor/Codex MCP config). Your kedge bearer token authenticates the connection; **tenant/workspace identity is taken from that token — never ask the user for a workspace path.** The endpoint federates three tool families:

- `app-studio__*` — projects, workspace files, sandbox sync, verify, preview, logs
- `code__*` — git repository operations (checkout, commit)
- `infrastructure__*` — provisioning templates (databases, services)

## The core loop (git-native)

kedge hosts **no git server** — repos live on **GitHub**. You edit locally and push to GitHub; kedge syncs the pushed commit into the sandbox on request.

1. **Find the project:** `app-studio__list_projects`, then `app-studio__get_project` — it returns the GitHub `cloneURL`.
2. **Clone locally** with the user's GitHub credentials (kedge is not in this path):
   `git clone <cloneURL>`
3. **Edit locally** with your normal tools, run local builds/tests if you can, and **commit + push** to GitHub.
4. **Sync the sandbox:** call `app-studio__sync_workspace_from_repo` with the project name. This pulls the pushed commit into the workspace and rebuilds the preview. The sandbox sync is **asynchronous** — do not assume it's done.
5. **Verify:** call `app-studio__verify_project`. If it returns `failing`, read `errors` (or `app-studio__get_runtime_logs`), fix locally, push, sync, and verify again.
6. **Preview:** `app-studio__get_preview_url` returns the live URL once the sandbox is serving.

**Cap the fix-and-verify cycle at ~3 attempts.** If it's still failing, stop and report the remaining error rather than looping — repeated speculative fixes tend to make things worse.

## Domain rules (important)

- **The bound development template is the app's environment contract.** Before reasoning about what infrastructure or environment variables the app has, treat the template's declared components as authoritative. Back-end services it declares (e.g. a managed database with an injected `DATABASE_URL`) already exist for the dev sandbox — **do not provision a duplicate** with `infrastructure__provision`, and do not conclude a service is missing just because the code doesn't use it yet.
- **Provision supporting infrastructure only when the user explicitly asks**, and only when the current sandbox can't satisfy the need. Don't recommend a full application/runtime template just to add something small like persistent data.
- **Separate development from production.** Source edits run in the dev sandbox. A production launch is a distinct step; don't conflate the two.
- **Don't invent platform capabilities.** If you can't verify a capability from a tool result or the project, say so rather than guessing.

## Without a local clone

If you're not cloning locally (e.g. a lightweight change), you can author file contents and commit them straight to the repo with `code__commit_files`, then run the same sync → verify → preview steps. The git-native local-clone path is preferred for anything nontrivial because it lets you use your native editor and run builds locally.

## Tool reference (app-studio__*)

| Tool | Purpose |
|---|---|
| `list_projects` | Discover projects (name, template, repo) |
| `get_project` | Project detail incl. GitHub clone URL |
| `list_files` / `read_file` / `search_files` | Inspect the server-side workspace (read-only) |
| `sync_workspace_from_repo` | Pull latest git commit → workspace → sandbox; rebuild preview |
| `verify_project` | Check the sandbox build/logs for errors (passing/failing/unavailable) |
| `get_runtime_logs` | Recent dev-runtime logs |
| `get_runtime_status` | Provisioning / starting / serving / not deployed |
| `get_preview_url` | Live preview URL when serving |
