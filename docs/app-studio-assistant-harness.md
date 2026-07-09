# App Studio assistant harness

Status: **Implemented** (this document describes the harness after PR #412) with a clearly-marked deferred section.
Author: 2026-07-09
Related: [`app-studio-sandbox-runtime.md`](./app-studio-sandbox-runtime.md) (the runtime data plane `verify_project` reads), [`app-studio-runtime-decoupling.md`](./app-studio-runtime-decoupling.md) (runtime credential direction), `providers/app-studio/api/` (all code referenced below).

## Summary

The App Studio assistant turns natural-language chat into an app-building agent over a per-project workspace. It is a **single-agent ReAct loop built on CloudWeGo eino ADK** (`github.com/cloudwego/eino/adk`), kept strictly behind an App Studio-owned interface (`projectAssistantEngine`, [`assistant_contract.go`](../providers/app-studio/api/assistant_contract.go)) so eino types never leak into REST, portal, or storage.

This document records the harness architecture, what a mid-2026 gap analysis against the leading coding/app-builder harnesses (Claude Code, OpenAI Codex, Cline, aider, goose, OpenHands, opencode, Lovable/v0/Bolt/Replit/Firebase/Cursor) recommended, **what was implemented**, and **what was deliberately deferred** with the exact entry points to pick it up.

All file references are under `providers/app-studio/api/` unless noted.

## Restore-from-reboot summary

- **Loop:** one eino `adk.NewChatModelAgent` per turn, `MaxIterations = 32`, tools execute sequentially. Interrupts (permission / plan-approval / follow-up) checkpoint to the `AssistantRun` store and resume via an atomic `ClaimAssistantRun`.
- **Routing:** a semantic turn classifier picks one of six profiles (discussion / guidance / exploration / debugging / debug_fix / implementation); the profile gates which tool bundles are available.
- **Recent work (PR #412):** cross-turn tool evidence, model-window-scaled compaction, a `verify_project` verification loop, an Anthropic/Claude provider, model retry/backoff, cheap-classifier routing, a batch multi-edit tool, token/cache observability, prompt-cache correctness, and opt-in context editing.

---

## 1. Architecture (as-is)

### 1.1 Agent loop

- Entry: SSE handler → `generateProjectAssistantStream` ([`llm.go`](../providers/app-studio/api/llm.go)) → `projectEinoAssistantEngine` ([`assistant_eino_engine.go`](../providers/app-studio/api/assistant_eino_engine.go)).
- Per turn, `newAgent` builds one `adk.NewChatModelAgent` with `MaxIterations = maxAssistantToolTurns` (32) and `ExecuteSequentially: true` (required for the permission barrier).
- On "exceeds max iterations" the loop does **not** error — it re-invokes the model with `ToolChoiceForbidden` to force a clean natural-language wrap-up (`projectEinoAssistantToolLoopFinalInstruction`).
- Middleware stack (in order): **context editing (opt-in)** → **summarization** → **tool-search** (when there are dynamic tools).

### 1.2 Tools

Local registry ([`assistant_tool_registry.go`](../providers/app-studio/api/assistant_tool_registry.go)): `list_project_files`, `read_project_file`, `search_project_files`, `ask_follow_up`, `request_project_plan_approval`, `write_file`, `apply_patch`, **`apply_patches`** (new), `mkdir`, `select_project_template`, `hydrate_workspace`, `commit_project_files`.

Graph-workflow tools ([`assistant_workflow.go`](../providers/app-studio/api/assistant_workflow.go), [`assistant_runtime_tools.go`](../providers/app-studio/api/assistant_runtime_tools.go), [`assistant_verify.go`](../providers/app-studio/api/assistant_verify.go)): `plan_project_changes`, `check_project_readiness`, `prepare_project_deployment`, `deploy_project_runtime`, `get_runtime_status`, `get_preview_url`, `get_runtime_logs`, **`verify_project`** (new), `restart_runtime`, `set_runtime_env`.

MCP tools (allowlisted, addressed by cluster ID): `infrastructure__*`, `databricks__*`. Discovery is gated per turn and large tool sets are hidden behind the eino `toolsearch` middleware (`tool_search` with `select:<name>`).

### 1.3 Turn routing & permissions

- Semantic router ([`assistant_turn_profile.go`](../providers/app-studio/api/assistant_turn_profile.go)) → six profiles → tool-bundle gating (`AllowsTool`). Escalate-only merge across recent messages keeps standing intent.
- Permission model ([`assistant_permission.go`](../providers/app-studio/api/assistant_permission.go)): read/input → allow; plan → ask once then allow; write → allow under an approved-plan envelope else ask; commit/runtime → always ask.
- Plan approval ([`assistant_approved_plan.go`](../providers/app-studio/api/assistant_approved_plan.go)) stores a path/operation envelope that survives across turns until the next commit.

### 1.4 Persistence

Postgres/in-memory `store` with encrypted message content; two entities scoped by `{org, workspace, project}`: **Messages** (transcript + `assistantActions` metadata) and **AssistantRun** (resumable checkpoint + audit). Two-tier checkpoint: eino gob blob wrapped in App Studio JSON.

---

## 2. What was implemented (PR #412, six commits)

### 2.1 Cross-turn tool evidence — `assistant_history_evidence.go`
The prompt assembly previously replayed only user/assistant prose, so the model forgot which files it had read/edited and re-read or hallucinated them. It now reconstructs a bounded tool-activity trail from the persisted `assistantActions` metadata (tool name + summarized args/results — never raw file contents or secrets). Emitted as a system message; capped at 12 turns / 8 actions / 4000 bytes.

### 2.2 Dynamic compaction trigger — `assistant_eino_model.go`
The eino summarization middleware fired at a fixed **24k tokens**, discarding most of a modern context window. It now scales to the model's window (`projectAssistantSummaryContextTokens`): **60% of a per-family window** (Claude 200k, GPT-5 400k, GPT-4.1/Gemini-2 1M, default 128k), floored at 24k, capped at 300k.

### 2.3 Prompt caching — `assistant_eino_model.go`, `llm.go`
- Anthropic cache breakpoints via the eino Claude component (`SetMessageCacheControl`).
- **Correctness fix:** the breakpoint initially sat on a system message whose content varied every turn (conversation mode, project metadata), so it rarely hit. The system prompt is now split into a **byte-stable guardrail preamble** (first system message, carries the breakpoint) and a per-turn dynamic message. A test asserts the preamble is identical across projects/profiles.
- No-op for OpenAI/Gemini (which cache large prefixes automatically).

### 2.4 Verification loop — `assistant_verify.go`
`verify_project` inspects the live dev-runtime logs (the SandboxRunner data plane has no build-exec verb), classifies build/compile/crash errors via signature heuristics, and returns structured `passing` / `failing` / `unavailable`. The builder prompt calls it after edits and, on `failing`, locates and fixes the offending file — **capped at three fix-and-verify cycles** (the documented Bolt/Replit failure mode is unbounded auto-fix loops degrading context). Reports `unavailable` honestly when no sandbox is bound.

### 2.5 Anthropic/Claude provider — `assistant_eino_model.go`
Native `eino-ext/components/model/claude` provider, selectable via `provider: anthropic`. Honors a custom base URL; sets a 16k `MaxTokens`.

### 2.6 Model retry/backoff — `assistant_model_retry.go`
Wraps model calls in bounded exponential backoff (3 attempts, 0.5s→8s) for transient failures (429/5xx/overloaded/timeout). `Stream` retries only the setup error to preserve streaming semantics; `WithTools` is preserved so ADK tool binding still works.

### 2.7 Cheap-classifier routing — `assistant_turn_profile.go`
Turn classification can route to a cheaper/faster model via `APP_STUDIO_ROUTER_MODEL` (same provider/credentials, model name swapped), reusing the main model when unset — safe for custom endpoints.

### 2.8 Batch multi-edit — `assistant_batch_edit.go`
`apply_patches` applies up to 40 exact-match edits across files in one call, cutting round-trips against the 32-iteration cap. Gated by the same write / plan-envelope path check as `apply_patch`, with **every** edit path required to be inside the approved envelope (stricter than single edits, not looser).

### 2.9 Token accounting + cache observability — `assistant_observability.go`, `assistant_eino_state.go`
Records per-model-call token usage (prompt / cached / completion / reasoning) via the eino callback and accumulates per-run totals. `APP_STUDIO_ASSISTANT_TRACE=1` logs per-call and cumulative figures plus a **prompt-cache hit ratio** (`CachedTokens / PromptTokens`) — the first in-process token accounting in the harness.

### 2.10 Context editing (opt-in) — `assistant_context_editing.go`
The eino-native `reduction` middleware in clear-only mode (no offload backend): once model input exceeds a threshold, the oldest tool results are replaced with placeholders, retaining the most recent exchanges. Runs **under** summarization at **75% of its trigger** so a tool-heavy session sheds old file reads before paying for a full summary. Collaboration tools (`ask_follow_up`, `request_project_plan_approval`) are excluded. **Gated behind `APP_STUDIO_CONTEXT_EDITING` (default off)** pending live validation of its interaction with the checkpoint/resume flow.

### 2.11 Configuration flags added

| Env var | Default | Effect |
|---|---|---|
| `APP_STUDIO_ROUTER_MODEL` | unset | Model name for turn classification (same provider/creds) |
| `APP_STUDIO_ASSISTANT_TRACE` | off | Log per-call + per-run token usage and cache-hit ratio |
| `APP_STUDIO_CONTEXT_EDITING` | off | Enable the reduction (clear-tool-results) middleware |
| `APP_STUDIO_TOOL_DISCLOSURE` | `summary` | Existing; `minimal` opaqueifies tool disclosure |

---

## 3. What was deliberately NOT implemented (deferred)

Each item is a real SOTA mechanism confirmed by the research, deferred for the stated reason. Exact entry points are given so a follow-up can pick it up directly.

### 3.1 Reasoning-effort routing — deferred (hot-path risk)
Confirmed available: `openai.WithReasoningEffort(...)` and gemini `Config.ThinkingConfig.ThinkingBudget`. Not wired because reasoning-effort only applies to reasoning models and **errors on non-reasoning models**; the turn classifier is a hot path where a silent per-turn error would degrade every turn to the keyword fallback. The cheap-classifier *model* routing (§2.7) is the safer lever for the same goal. Follow-up: apply `minimal`/`low` effort only when the classifier model is a known reasoning model.

### 3.2 Multi-agent decomposition — deferred (architectural, separate PR)
eino ships prebuilt constructors: `adk/prebuilt/deep` (**DeepAgent**: virtual filesystem `read_file`/`write_file`/`edit_file`/`glob`/`grep` + shell + `write_todos` + `task` sub-agents), `adk/prebuilt/planexecute` (**Plan-Execute-Replan**), and `adk/prebuilt/supervisor` (marked "not recommended" in favor of DeepAgent/AgentTool). Adopting these replaces the single-agent loop and is a large change warranting its own PR. The approved-plan envelope (§1.3) is today a *permission* construct, not a tracked todo executor.

### 3.3 Browser-level UI verification — deferred (the app-builder frontier)
Replit Agent 3 pairs a REPL with Playwright to catch "Potemkin interfaces" (UI that renders but does nothing); the generic pattern is Playwright/computer-use MCP → screenshot + console/AX-tree + runtime errors → back into the loop. Our `verify_project` (§2.4) reads runtime **logs** only. Adding browser verification needs a Playwright/computer-use surface in the sandbox data plane — a larger lift on the runtime side.

### 3.4 Tool-schema prompt caching — deferred (smaller follow-up)
Anthropic caches in `tools → system → messages` order; we cache the system prefix but not the tools block. The eino Claude component exposes `WithAutoCacheControl(*CacheControl)` (auto-manages up to 4 breakpoints, would cover tools+system) and `SetToolInfoCacheControl(*schema.ToolInfo, ...)`. Not adopted yet to avoid churning the tested manual system-prompt breakpoint; the tools block is also destabilized by the dynamic per-turn tool set (tool-search), reducing its cache value.

### 3.5 Native eino retry/failover — deferred (no regression, avoid churn)
`ChatModelAgentConfig.ModelRetryConfig` / `ModelFailoverConfig` provide retry + cross-model failover natively. Our hand-rolled `assistant_model_retry.go` (§2.6) covers retry and is tested; migrating to the native config (and adding failover across providers) is a clean follow-up but not a regression to leave as-is.

### 3.6 Context editing on by default — deferred (needs live validation)
The reduction middleware is implemented and wired (§2.10) but opt-in. Making it default requires validating that clearing tool results does not corrupt the permission-interrupt / checkpoint / resume reconstruction, which cannot be verified without a live model run.

### 3.7 Portal `anthropic` provider selector — deferred (backend works via API)
The portal LLM settings is a binary OpenAI/Google toggle in a large Vue file. The backend already accepts `provider: anthropic` via the REST PATCH; a correct 3-way selector (with anthropic base-URL/model normalization) is a self-contained UI change.

### 3.8 Read-truncation change — verified unnecessary
Initially flagged: `read_project_file` truncated to 1000 chars. **Verified false** — the 1000-char limit only applies to UI summaries; the read path returns up to 64KB (256KB on request) to the model uncut. No change made.

---

## 4. Research provenance

The gap analysis behind §2–§3 was produced by a multi-agent research pass (mid-2026) covering the eino ecosystem (ADK patterns, middlewares, model-provider option matrix, HITL/checkpoint, observability) and the SOTA harness landscape (Claude Code / Codex / Cline / aider / goose / OpenHands / opencode / Lovable / v0 / Bolt / Replit / Firebase / Cursor). Numbers cited in that research (e.g. "84% token savings with context editing", Replit "$0.20/session") are vendor/blog figures used for direction, not guarantees. The harness-side facts in §1–§2 are verified against the code and the test suite.

## 5. Tests

New unit tests ([`assistant_harness_upgrades_test.go`](../providers/app-studio/api/assistant_harness_upgrades_test.go)): dynamic window scaling, retry classification + backoff behavior, runtime error detection, batch path-envelope enforcement, history-evidence reconstruction, token accumulator + cache-hit ratio, env parsing, stable-preamble cacheability, and clear-before-summary threshold ordering. Two existing tests updated (model-wrapper unwrap, tool-registry order). `go build`, `go vet`, and `go test ./...` pass.
