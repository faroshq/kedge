# App Studio provider — deep review & improvement plan (part 2)

Status: **Phases 0–2 complete; Phase 3 complete except the multi-replica lease;
Phase 4 partially complete (2026-07-31).**
**Two deliberate gaps carried: browser-side error capture (§Phase 2) and the
durable run lease (§Phase 3). Four Phase 4 items deferred with reasons below.**
Date: 2026-07-31 · Scope: `providers/app-studio/` (Go backend + Vue portal)
Companion to [agents-provider-improvement-plan.md](./agents-provider-improvement-plan.md) (part 1).

## Status at a glance

| Phase | Scope | State |
|---|---|---|
| 0 | Correctness and audit | **Done** |
| 1 | The missing half of built features | **Done** except git-side deletion |
| 2 | Closing the feedback loop | **Done** except browser-side error capture |
| 3 | Scale | **Done** except the multi-replica lease |
| 4 | Depth | **Partial** — 2 of 7 done, 1 partial |

## What is still missing

Ordered by what I would pick up next. Each says why it was not done, because in
several cases that reason is the useful part.

**1. Durable run lease — blocks multi-replica (§2.4).** Ownership is inferred
from one process's memory. A second replica would see the first's live run as
orphaned and interrupt it mid-mutation. Needs owner identity plus a heartbeat on
`store.AssistantRun` and reconciliation restricted to expired leases — a schema
migration and a correctness-critical distributed change. Deliberately not
half-built: an owner field without a heartbeat would look like multi-replica
safety without being it. The chart still refuses to render above one replica,
and the reconciler documents the requirement at the site.

**2. Git-side deletion (§1.2).** `delete_file` removes a file from the workspace
and the sandbox, but not from the repository — the Code provider's commit bundle
carries only path+content. Needs `deletePaths` through the bundle, the
RepositoryCommit CRD, and the GitHub backend, i.e. a CRD regeneration, which
[[project_make_crds_guts_core_apiexport]] currently makes hazardous. The tool
description states the limit rather than implying git is covered.

**3. Browser-side error capture (§5.1).** Server-side runtime, build, and
compile faults are captured and diagnosed; JavaScript errors in the previewed
app are not. There is no kedge-owned hop in the preview HTTP path to inject a
shim into (browser → preview host → Gateway → sandbox Service; the dev-agent
never sees app traffic), and adding one would undo the provider-isolation
boundary that makes BYO compute possible. Three options, all with real costs: a
proxy in the infrastructure provider's sandbox pod, a shim injected into the
user's own source, or a template-level opt-in.

**4. Phase inference from run-state counters (§6.1).** Phase is still re-derived
by string-matching the transcript, so summarization or reduction can regress a
verified run back into `mutate` and re-open edit tools after verification
passed. The authoritative counters already exist. Deferred because phase
inference is the subtlest logic in a 2,100-line file and rewriting its source of
truth deserves its own change with focused tests.

**5. Durable rolling project summary (§3.5).** Turn context is capped at 24
messages with no cross-turn memory beyond counts, so long projects silently lose
earlier constraints. The in-run summarizer's output already exists; it just is
not persisted and re-injected.

**6. Mid-turn checkpoint flush (§6.2).** Eino checkpoints stay in process memory
except at interrupts, so a crash mid-plan loses in-turn transcript and
verification state. `PersistRun` already supports the flush.

**7. Infrastructure-provider items (§5.7, §5.8, §5.4).** Sandbox TTLs and
pause/resume, default-deny egress with an allowlist proxy, and Playwright
self-testing are properties of the `SandboxRunner` template, its pod spec, and
NetworkPolicy — not App Studio's to own. Self-testing is additionally a feature
in its own right (browser in the image, driving protocol, cost controls).

**8. Remaining ACI adoptions (§5.5).** Edit-failure messages now teach the
recovery; windowed reads and search-returns-match-counts are still open.

**9. `llm.go` decomposition (§6.5).** 2,300 lines mixing settings CRUD, provider
wiring, prompt text, an MCP JSON-RPC client, and the turn entry point.
Mechanical but large.

**Also unaddressed from the review**, lower priority: the four inconsistent list
response shapes and `err.Error()` leaking into Status messages (§7); the
adjacent-action grouping drift, where six reads of *different* files still show
six rows (§7); dead A2UI portal machinery (§4); the API-response envelope and
pagination gaps (§7); and the several god-file/architecture items in §6.

---

# Part A — implementation record

What was built, in the order it was built. The original review findings
that motivated each item are in Part B, referenced by section number.

## What landed (2026-07-31)

Phase 0 in full, plus the autoscroll fix pulled forward from Phase 1:

- **0.1 Runtime graph tools normalized** (§2.1). `newProjectAssistantGraphWorkflowTools`
  now returns two planes: read-only graph tools go straight to the model, and
  every risk-bearing one is returned as a registry tool
  (`projectAssistantGraphBackedTool`) so it runs through
  `projectEinoAssistantTool` — permission barrier, approval-mode decision,
  `AdmitMutation`, and audit events all apply. The split is driven by
  `spec.Risk`, so a future mutating graph tool is normalized automatically, and
  a risk-bearing tool that cannot be routed through the audited path is a build
  error rather than a silent bypass. Regression test:
  `TestProjectAssistantGraphToolsRouteRiskBearingCallsThroughAuditedPath`.
- **0.2 Fail-open approval wrapper eliminated** (§2.1). With approval owned by
  the audited wrapper, `approvaltool.InvokableApprovableTool` has no remaining
  users and was removed rather than vendored — there is no longer any
  `eino-examples` code in a permission gate. (`graphtool` remains as a graph
  builder; the engine's now-unreachable `ApprovalInfo`/`ApprovalResult` resume
  mapping was left in place as defensive code.)
- **0.3 Promote gated on the commit** (§1.1). Build evidence now carries the
  commit recovered from the `sha-<commit>` tag; `checkProjectBuild` compares it
  against the project's latest successful commit and reports a new `stale`
  status when every component has an image but not from HEAD, which promote
  refuses. Unknown-on-either-side never blocks (images predating commit tagging
  still ship). The generated workflow no longer builds every branch — a
  job-level condition restricts publishing to the repository's default branch,
  read from the event rather than hardcoded, with `workflow_dispatch` still
  building any ref.
- **0.4 Truncated sync is now an error** (§1.3). `projectWorkspaceSyncFiles`
  fails instead of shipping an alphabetical prefix of the tree, and files the
  payload cannot carry (binary, oversized) are returned and surfaced —
  `skippedFiles` on the sync response, and a recorded sync failure on the
  background path so the assistant reports them instead of diagnosing a
  phantom.
- **0.5 Portal correctness** (§7). The fabricated `aborted` snapshot no longer
  claims a revision the server still owns (applied with
  `registerRevision: false`), so the authoritative abort is no longer discarded;
  a wholesale REST message replacement forgets the run's revision so the
  repairing snapshot is not refused as a duplicate; the SSE reader has a 45s
  inactivity deadline (three missed keepalives) so a silently dead connection
  reconnects instead of hanging a foreground tab; CRLF SSE framing is
  normalized.
- **0.6 Scale** (§3.1, §3.2). Workspace mutations lock per project scope
  instead of store-wide, via a refcounted lock map that does not grow per
  project for the process lifetime. The provisional flag persists only on
  transition, ending the immediate durable snapshot (and SSE revision bump) per
  streamed chunk.
- **Pulled forward from Phase 1:** the transcript autoscroll now only follows
  when the reader is already within 80px of the bottom.

Verification: `go build ./...`, `go test ./...`, `go test -race ./api ./store
./workspace`, `go vet ./...`, portal `typecheck` + `build` + all seven test
suites. Two new Go test files' worth of regression coverage
(`TestFileStoreMutationsDoNotSerializeAcrossScopes`,
`TestFileStoreScopeLocksAreReleased`, `TestCommitFromPackageTag`,
`TestCommitsMatchToleratesAbbreviation`,
`TestBuildStatusStaleWhenImagesPredateHeadCommit`, the two graph-tool tests)
plus three new portal resilience tests.

**Behavioral changes to be aware of when this ships:** projects whose newest
image predates their latest commit will now see `stale` and a refused promote
where they previously promoted silently; feature-branch pushes stop publishing
images; and workspaces above 500 files now fail the dev sync loudly instead of
half-syncing.

## Phase 1 (2026-07-31)

- **Undo works, and now has a button** (§4). The headline find: `SnapshotID`
  was **never set by any production code path**, so `RestoreSnapshot` had
  nothing to restore and the undo endpoint could only ever answer "no source
  changes to undo". `write_file`, `apply_patch`, and the new `delete_file` now
  record the run's snapshot, and the portal renders a two-step "Undo file
  changes" control on any finished run that edited files (restoring is not
  redoable, so it confirms before acting). Two existing tests asserted
  `ErrSnapshotNotFound` — they were pinning the gap, and now assert the
  restore.
- **File deletion, workspace → sandbox** (§1.2). `FileStore.DeleteFile` plus a
  `delete_file` tool (same read-before-mutate rule as `apply_patch`,
  undoable via the run snapshot). Deletions are recorded as tombstones and
  replayed as `deletePaths` on every sync — the sandbox agent already accepted
  that field, App Studio simply never sent it. Tombstones are dropped when a
  file reappears, so delete-then-recreate does not delete it straight back out.
  **Not done: removal from git.** That needs `deletePaths` on the code
  provider's commit bundle, RepositoryCommit CRD, and GitHub backend — a
  cross-provider change with a CRD regeneration, which
  [[project_make_crds_guts_core_apiexport]] currently makes hazardous. The tool
  description states the limit rather than implying git is covered.
- **The no-progress bound is real** (§4). It stops at 6 repeats/no-progress
  model calls instead of sharing the 100-iteration ceiling, so a stuck model no
  longer burns the entire budget before reporting `max_iterations`. All the
  downstream handling (WorkItem suspended with reason `no_progress`, audit
  mapping, user-facing text) was already written and simply unreachable. Two
  tests that pinned the run-to-the-ceiling behavior now assert the early stop.
- **Tool-catalog degradation is visible** (§2.5). The MCP-need keyword scan
  covers the last three user messages instead of only the newest, so "add a
  postgres database" → "now do it" keeps the infrastructure tools; and a failed
  discovery records a `Degraded` reason and logs a warning instead of silently
  dropping `commit_project_files`.
- **Preview is the default workbench tab** (§7) rather than the launcher.

Verification: `go build`, `go vet`, `go test ./...`, `go test -race ./api
./store ./workspace`, portal typecheck + build + all seven suites. New
coverage: deletion/tombstone/undo round-trip in `workspace`, the MCP scan
window, discovery degradation, and the rewritten no-progress tests.

**Additional behavioral changes:** runs now stop after 6 unproductive model
calls (previously 100); `delete_file` appears in the assistant's catalog; and
mutations now write snapshot data per run under the workspace root, which grows
until project deletion (§3.7's retention sweeper is the Phase 3 follow-up and
matters more now that snapshots are actually being written).

## Phase 2 (2026-07-31) — closing the feedback loop

**The architecture constrained 2.1, and the constraint is worth recording.**
The plan assumed we could inject an error shim into previewed apps the way
Dyad/Lovable/Bolt do. Those products proxy preview traffic; we deliberately do
not. Preview goes browser → preview host → cluster Gateway → the sandbox
Service, and the dev-agent is a control sidecar (`/sync`, `/restart`, `/env`,
`/logs`) that never sees app HTTP. There is **no kedge-owned HTTP hop in the
preview path to inject into**, and adding one would undo the provider-isolation
boundary that makes BYO compute possible. So browser-side JavaScript errors
remain uncaptured. The options, none of them free, are: a proxy in the infra
provider's sandbox pod; injecting a shim into the user's own source (pollutes
their repo); or a template-level opt-in. Deferred deliberately rather than
half-built.

What landed instead is the part that fits the architecture — and covers build
failures, compile errors, unhandled server exceptions, and startup faults,
which is where most "my app is broken" cases actually live:

- **Runtime failures are diagnosed, not forwarded** (`api/runtime_issues.go`).
  Dev-server output is classified into structured `projectRuntimeIssue`s —
  missing module, missing script, syntax, compile, port-in-use,
  permission-denied, process-crash — each carrying the fault's subject (the
  unresolved module, the absent script), a workspace-relative `file:line` where
  the runtime named one, an occurrence count, and **a concrete remediation**.
  Previously a single substring match forwarded one raw log line and the model
  paid a call to work out what it meant.
- **Deduplication and locating.** A broken dev server reprints the same fault on
  every reload; issues are deduped by what the fault *is*, with occurrence
  counts. Sandbox path prefixes are stripped so locations match what the file
  tools accept, and `node_modules` frames are dropped so the assistant is never
  pointed at code it does not own.
- **Verification reports the diagnosis** (§5.2). `verify_development_runtime`
  now says *"cannot resolve `express` — add it to package.json dependencies"*
  instead of *"the logs contain a startup or compilation failure"*, and carries
  the structured issues alongside, so the repair lane acts on them rather than
  re-reading the same lines.
- **The fix ladder routes by fault class** (§5.2). Every log blocker used to
  open the source-repair lane, which sent the model editing code for faults no
  edit can fix — a port already bound, a denied filesystem operation. Only
  diagnosed *source* defects now route to repair; operational faults route to
  the operational lane. An unclassified blocker keeps the old behaviour rather
  than being silently downgraded.
- **Broken previews are visible** (§5.1, §7). The portal watches the two signals
  observable from outside a cross-origin frame: a frame that never fires `load`
  (15s), and repeated loads inside a 10s window — which is what the known CHIPS
  cookie failure looks like from outside. Either surfaces an explanation and an
  "Open in browser" action, replacing a blank panel behind a "Ready" badge. The
  decision logic lives in `previewState.ts` so it is unit-tested rather than
  buried in the component.

Verification: `go build`, `go vet`, `go test ./...`, `go test -race ./api
./workspace`, portal typecheck + build + all nine suites. New coverage: nine
classifier cases plus dedup/location/dependency-frame/routing/summary tests, and
four preview-frame health tests.

**Behavioral change:** a runtime failure that is diagnosed as operational
(port conflict, permission denial) no longer opens the source-repair lane. If a
project relied on the assistant editing code in response to those, it will now
restart/report instead — which is the intended correction.

## Phases 3 and 4 (2026-07-31)

### Phase 3 — scale

- **Undo snapshots are swept** (§3.7). Phase 1 made snapshots real, which made
  their absence of retention urgent: one snapshot per run, forever, on a volume
  shared by every tenant. `FileStore.SweepSnapshots` removes run snapshots past
  a retention window (default 72h, `APP_STUDIO_SNAPSHOT_RETENTION`, `0`
  disables), swept at startup and on a ticker. Deletion tombstones live in the
  same directory and are explicitly preserved — dropping them would resurrect
  deleted files in the sandbox on the next sync.
- **Delta sync** (§3.4). App Studio records the content hashes last confirmed
  for each component and ships only what changed; whole-workspace payloads (up
  to megabytes through the hub proxy inside a 20s budget) were re-sent on every
  mutation. Safety rests on sandbox files living on a PVC, so pod restarts
  preserve them. The manifest is dropped — forcing a full sync — whenever that
  stops holding: a failed or non-2xx sync, a template switch or hydrate that
  replaces the volume, or a provider restart (it is in-memory). A component
  with no changes and no deletions skips its round trip entirely.
- **Per-run metadata is bounded** (§3.3). Metadata is re-serialized on every
  tool event and streamed inside every SSE snapshot, so its size was paid O(n)
  times over a run — quadratic in bytes. Patch bodies are now stripped (they
  are already durable in the audit record and the undo snapshot) and events are
  capped at 64, keeping the first and the most recent.
- **Tenant GraphQL calls time out** (§3.6) at 30s. A hung hub connection pinned
  its goroutine forever, and the HTTP server sets only `ReadHeaderTimeout`, so
  those accumulated until the provider stopped serving.
- **Orphan-reconciler data race fixed** (§2.4). `active.run.ID` was read after
  releasing the supervisor lock while `update()`/`Stop()` mutate it under that
  lock; losing the race marks a live run interrupted.

**Deferred: the durable run lease (§2.4).** Multi-replica needs owner identity
plus a heartbeat on `store.AssistantRun`, with reconciliation restricted to
expired leases — a schema migration and a correctness-critical distributed
change. A half-lease (owner field, no heartbeat) would be worse than none: it
would look like multi-replica safety without providing it. The chart still
refuses to render above one replica, which is the honest enforcement point, and
the reconciler now documents the requirement at the site.

**Skipped: template caching (§3.6).** `fetchProjectTemplate` is a single keyed
Get, not the unpaginated list the review was concerned about (Packages and
Commits are already label-selected). Not worth the staleness risk.

### Phase 4 — depth

- **Trust boundary in the system prompt** (§2.2). Everything the tools return —
  file contents, repository docs, template `agent.usage`, MCP tool
  descriptions, build output, runtime logs — is now labelled as data the model
  has read, never as instructions, with explicit guidance to report rather than
  comply when fetched content tries to direct it. The prompt previously told
  the model to treat provider-authored template text as **authoritative**,
  which is an instruction channel into a session that can commit code and
  provision infrastructure.
- **Irreversible verbs keep their confirmation under auto-approve** (§2.3).
  `hydrate_workspace` (destroys uncommitted work with no snapshot to restore),
  `promote_project` (publishes to production), and `infrastructure__provision`
  (creates billable infrastructure) now always ask. Ordinary edits stay
  auto-approvable precisely because they are snapshotted and undoable. Matching
  is on the base tool name so a namespaced MCP tool cannot slip past.
- **Edit-failure messages teach the recovery** (§5.5). A failed edit roughly
  halves the odds that edit ever succeeds, because the model retries the same
  shape. Not-found, ambiguous-match, oversized, and binary patch failures now
  each state what to do instead.

**Deferred, with reasons:**

- **4.1 phase from run-state counters.** Correct, and still worth doing, but
  phase inference is the subtlest logic in a 2,100-line file and rewriting its
  source of truth deserves its own change with focused tests rather than the
  tail of a long session.
- **4.4 sandbox TTL trio / pause-resume, 4.5 default-deny egress.** Both belong
  to the infrastructure provider: they are properties of the `SandboxRunner`
  template, its pod spec, and NetworkPolicy, not of App Studio. Doing them here
  would mean App Studio reaching into runtime concerns it deliberately does not
  own.
- **4.6 Playwright self-testing.** A genuine feature (Replit's reflection loop),
  needing a browser in the sandbox image, a driving protocol, and cost controls.
  Too large to append here.
- **4.7 `llm.go` decomposition.** Mechanical but large; it would bury this
  change set's behavioural diffs in a 2,300-line move.

Verification for both phases: `go build`, `go vet`, `go test ./...`,
`go test -race ./api ./store ./workspace`, portal typecheck + all nine suites.
New coverage: snapshot sweep (including tombstone survival), delta-sync change
detection and its invalidation cases, the auto-approve carve-out, and the trust
boundary.

**Behavioral changes:** undo snapshots older than 72h are removed (configurable);
`hydrate_workspace`, `promote_project`, and `provision` now prompt even in
auto-approve; syncs ship only changed files, so a sandbox mutated out-of-band
is no longer silently corrected by the next full sync (a template switch,
hydrate, or provider restart still forces a full sync).


## Follow-up: the WorkItem dead end (2026-07-31)

Found by hitting it in a live session, not by the review.

A project allows one active WorkItem, and a task left **active** by a run that
had died blocked every new edit plan with nothing able to clear it: cancelling
requires suspended + no active run, and the portal only listed suspended tasks,
so neither the UI nor the API could release it. The start conflict returned the
bare string `assistant work item conflict`, so the assistant invented a remedy
that does not exist ("clear the pending work item") and the user retried against
it repeatedly.

- **Release on read.** `listProjectAssistantWorkItems` and
  `cancelProjectAssistantWorkItem` now run orphan reconciliation before reading,
  so a dead-run task is suspended — and therefore continuable and discardable —
  automatically. Previously this only happened if something read the *run*,
  which the work-item list never did.
- **Conflicts name a reachable action.** The start conflict inspects the
  blocking task and says what applies: Stop for a running task, reload for a
  dead-run task, continue/discard for a suspended one. The cancel conflict
  distinguishes "still running" from a genuinely stale revision instead of
  blaming the revision for both. Test:
  `TestWorkItemConflictMessageNamesAReachableAction` asserts it names Stop and
  never says "clear".
- **An Activity tab** (`portal/src/activity.ts`, 8 tests) makes the state
  visible: the current run with Stop, and every task with its status and only
  the actions the backend accepts. Status is product language (Running / Paused
  / Done / Discarded) — "work item" is a durable-execution primitive and does
  not belong in a surface aimed at non-technical builders. Continue is the
  prominent action because a paused task keeps its approved plan; discarding
  throws that away.

**The root cause, found from production data.** The first round of this fix was
necessary but not sufficient. `reconcileOrphanedProjectAssistantRun` only ever
inspects `LatestAssistantRun`, so once ANY newer run existed and reached a
terminal status, the older run holding the stranded task was never examined
again — reloading, listing, and cancelling all called a reconciler that returned
immediately. `reconcileOrphanedProjectAssistantWorkItems` now walks the work
items rather than the run log: any active task whose owning run is absent or
terminal, and which this process is not executing, is suspended. It runs ahead
of the existing run reconciler, so all seven call sites inherit it.

Two properties are pinned by test: a task stranded behind a *superseded* run is
released (`TestReconcileReleasesWorkItemStrandedBehindSupersededRun`, which
reproduces the production shape — abandoned run, newer completed run), and a
genuinely running task is left alone (`TestReconcileLeavesLiveWorkItemAlone`),
because releasing one mid-mutation would abandon real work.

Still open: a run *history* list needs a new endpoint (App Studio has only
`runs/latest`), so per-run undo from the Activity tab is not wired yet.

## Follow-up: classifier defects found in production (2026-07-31)

A live session diagnosed as "cannot find module `/workspace/src/index.js`" while
that file existed in the sandbox. Two defects in the classifier shipped earlier
the same day, both found from real log data:

- **No matcher for backing-service failures.** The app could not reach the
  Postgres the template provisions, and with no matcher the fault fell through
  to the generic crash entry — whose remediation is "read the lines above the
  exit", which is exactly what the model had already failed to do. Added
  `backing_service`, covering SSL-unsupported, ECONNREFUSED, DNS, and auth
  failures, with a remediation that names the usual cause (the app's own client
  options, typically a TLS setting inferred from the host name) and explicitly
  protects the injected connection string from being "fixed".
- **Stale faults outranked live ones.** A dev server restarts repeatedly, so the
  log window holds faults it already recovered from. Ordering was
  blockers-then-first-appearance, which surfaced a "cannot find module" line
  emitted before the file was synced, over the connection failure the process
  was failing on right then. Now ordered by most recent occurrence.

- **Assistant instructions were rendered to the user.** The blocker list and
  verification summary printed `Issues[].Remediation` verbatim, so a
  non-technical builder was told to "read the lines above the exit for the
  cause, fix it, then restart the runtime" — three things they have no way to
  do, in a product whose stated audience is business users. Remediation is now
  explicitly model-only; issues carry a separate `userExplanation()` that says
  what broke and leaves the fixing to the assistant.
  `TestRuntimeIssueTextSeparatesUserFromAssistant` asserts the user-facing text
  never contains "restart the runtime", "read the lines above", "re-sync", or a
  tool name, while the assistant still receives its remediation.

- **The assistant was locked out of the fix it had diagnosed.** Adding the
  `backing_service` kind without adding it to `projectRuntimeIssueSourceFixable`
  left it in the `default: return false` branch, so verification routed it to
  the read-only operational lane — no edit tools. The run therefore produced a
  correct diagnosis, no fix, and a closing line asking the user to resolve it.
  A backing service the template provisions is reachable by construction, so a
  connection failure is always the app's own client configuration: source.
  `TestSourceDirectedRemediationsReachTheRepairLane` now asserts the general
  invariant — any issue whose remediation tells the assistant to change code
  must be source-fixable — and was verified to fail when the bug is
  reintroduced.

The underlying application bug was in generated code, not in kedge:
`ssl: databaseUrl.includes('localhost') ? false : { rejectUnauthorized: false }`
enables TLS against an in-cluster database that does not serve it. Worth noting
as a template-guidance gap — the heuristic "not localhost means managed cloud
Postgres" is wrong inside a kedge sandbox, and the generated app has no way to
know that unless the template says so.

## Follow-up: every new project failed its first sync (2026-07-31)

Found in provider logs, not from the assistant's own report — which claimed
template selection was unavailable when the logs show the template bound fine.

Binding a template writes App Studio's build config (`.kedge/build.json`) and CI
workflow to the workspace ROOT, because they describe the whole project rather
than one component. On a brand-new project those are the only files that exist,
so the "nothing routed to any component" guard counted 2 files, routed 0, and
failed:

    development sync after select_project_template failed: none of the 2
    workspace files are under a development component directory
    (backend -> api/, frontend -> web/)

That recorded a sync failure, which verification then reports as "the sandbox is
not running the latest code", and the assistant — seeing a failing sync and an
otherwise empty workspace — concluded it had no scaffold and refused to write
anything. Every new project hit this before writing a single line of app code.

The guard now counts only application files, ignoring App Studio's own managed
paths. Misplaced real source is still caught, which is what the guard is for.
`TestSyncGuardIgnoresAppStudioManagedFiles` covers all three cases.

**Worth noting for the next reviewer:** the assistant's account of this failure
was wrong in a way that cost several turns — it reported a phase/permission
problem ("template selection is unavailable in this mutation-only phase") when
the real fault was a sync guard. The model narrates the tools it cannot see
rather than the error it received, so its self-reported blockers should be
treated as a symptom, not a diagnosis. Provider logs settled it in one read.

## Follow-up: working apps reported as broken (2026-07-31)

The dominant cause of false "Incomplete" verifications, found by reading the
sandboxes directly while a run reported both components failed.

**Both were running.** The backend log ended `Builder Match API listening on
8080`; the frontend ended `VITE ready ... Local: http://localhost:8080/`. The
`process exited` lines above them were from before the workspace synced.

A sandbox log window is a HISTORY, not a snapshot. The classifier treated every
fault in the window as current, so the crashes a project recovers from during
normal startup — npm install before package.json exists, the dev server
restarting on first sync — were reported as blockers on a healthy app. Two
changes:

- **Recovery detection.** `projectRuntimeReadyMarkers` matches the lines a dev
  server prints when it is up (`listening on`, `ready in`, `Local: http://`,
  `compiled successfully`, …). A fault whose last occurrence precedes the last
  such marker is dropped: the process demonstrably started after it. A fault
  AFTER the last start is still reported, including in a start/fail/start/fail
  restart loop.
- **Cause before symptom.** A bare `process exited` says the process died, not
  why, and is always printed after whatever killed it — so ranking by recency
  put the symptom first and led with the one entry that has no actionable
  remediation. `process_crash` now sorts below any other blocker.

Regression tests are built from the verbatim live windows
(`TestRecoveredFaultsAreNotReportedAsBlockers`,
`TestFaultsAfterTheLastStartAreStillReported`), and the real windows were
replayed through the classifier to confirm they now yield zero blockers.

---

# Part B — the review

The findings this plan came from, kept as the reference for
everything still open.

Method: four adversarial code reviews (engine, permissions/safety,
lifecycle/data plane, API/portal) run against `HEAD` at review time
(post-#481/#484/#485), plus an external survey of how commercial and
open-source web harnesses are built in 2026. Every finding was confirmed by
reading source; the two headline claims (§1.1, §2.1) were independently
re-verified by hand.

## 0. Executive summary

App Studio's **durable execution core is genuinely strong** — arguably stronger
than most of the products surveyed. Server-lifecycle-owned runs, revision-CAS
snapshots, durable stop receipts, plan-grant tombstones, path-canonicalized
grants, WorkItem-bound execution authority, and a traversal-proof workspace
store are all real, tested, and better than the "vibe-coding" cohort ships.

The problems cluster in four places:

1. **One tool plane escaped normalization** — `restart_runtime` and
   `set_runtime_env` are raw eino graph tools with no admission check, no
   permission barrier, and **no audit trail at all**. This is the same bug class
   part 1 found in the agents provider.
2. **The build→promote pipeline can ship the wrong image**, cannot delete files,
   and silently truncates sync above 500 files. These are correctness bugs on
   the primary product path.
3. **Scale-and-time degradation**: per-chunk durable writes, one global mutation
   mutex across all tenants, unbounded per-run metadata, 24-message history.
   Fine in a demo, degrading on exactly the long mutation-heavy sessions this
   product exists for.
4. **We built the hard half of several features and skipped the cheap half.**
   Undo works end to end in the API and has no button. Provisional text streams
   from the engine and is discarded by every consumer. The no-progress
   termination machinery is three files of dead code. Preview runtime errors
   never reach the model at all.

Against the market: our **execution substrate and multi-tenant isolation are
ahead**; our **feedback loops are behind**. Every serious competitor closes the
loop from a broken preview back into the model automatically. We don't close it
at all.

---

## 1. Critical — correctness on the main path

### 1.1 Promote deploys "the latest published image", untethered from commit
`api/build_reconciler.go:270-278` generates a workflow triggering on
`branches: "**"`; `api/project_build_status.go:168-193` resolves build evidence
as `versions[0]` of the Package CR; `api/project_promote.go:204-226` promotes
that digest. Nothing ties the promoted image to a commit, a branch, or to what
the user validated in the sandbox. `checkProjectBuild` reports `built` if *any*
image was ever published.

Failure: user pushes an experimental branch, its CI finishes last, "Promote"
silently ships the experiment. Or: user commits a fix and promotes while CI is
still running — prod gets the previous digest behind a green gate.

Fix: record the commit SHA in build evidence (the `sha-<commit>` tag is already
published), resolve the digest by tag matching default-branch HEAD, gate promote
on `builtCommit == HEAD`, restrict the workflow trigger to the default branch,
and surface "build for HEAD still running" as its own status.

### 1.2 Deletes propagate nowhere
`workspace/store.go` has no delete API. `api/development_sync.go:104-107` sends
`Files` + `Restart` only. `api/llm.go:769-800` commits path+content only. The
runtime **already supports deletion** — `providers/infrastructure/dev-agent/main.go:272-278`
accepts `DeletePaths`; App Studio never sends it.

Failure: the assistant renames `routes/user.js` → `routes/users.js`. Both files
now run in the sandbox (duplicate route registration), both land in the prod
image, and undo leaves the undone file live. Stale `.kedge/build.json`
components linger across template switches.

Fix: `FileStore.DeleteFile` + an assistant tool, compute `DeletePaths` for sync,
use the code provider's delete-file capability on commit.

### 1.3 Silent partial sync above 500 files
`api/development_sync.go:454-471` calls `ListFiles` with
`workspace.MaxListLimit` (500) and **ignores `list.Truncated`**; binary and
>256 KB files are silently skipped. The sandbox receives an alphabetical prefix
of the tree and nobody is told. Trivial fix, critical impact — error or paginate
on `Truncated`, and report skipped files in the sync result.

---

## 2. High — safety and audit

### 2.1 Runtime graph tools bypass the normalized execution path
`api/assistant_runtime_tools.go:609-627` (`restart_runtime`) and `:699-717`
(`set_runtime_env`) are handed to the model as raw eino graph tools
(`api/assistant_eino_tool.go:70-74`), outside `projectEinoAssistantTool`.
Consequences, each confirmed in source:

- **No `AdmitMutation`** — a preempted or stopped run, or a run never
  WorkItem-bound, can still restart the sandbox and mutate its environment.
  Every other mutation-capable tool, including `infrastructure__provision`, is
  admission-checked.
- **No permission barrier** — while another call awaits approval, a parallel
  restart/env call in the same batch executes.
- **No audit events at all.** Audit entries come from `OnToolCall`; graph tools
  never emit them. In auto-approve mode there is also no decision record. An
  operator reviewing `run.Audit` cannot reconstruct that the sandbox
  environment changed, or to what.
- **Approval delegated to example code** — `approvaltool.InvokableApprovableTool`
  from `cloudwego/eino-examples`, whose final branch **executes the stored
  arguments** when a resume arrives with unexpected data. Type discipline in
  our engine prevents this today; the invariant lives entirely in the caller.

Fix: wrap both in `projectEinoAssistantTool` (their specs already carry
`Risk: runtime`), and vendor a fail-closed copy of the approval wrapper. Never
ship example code in a permission gate.

### 2.2 Untrusted content enters the prompt as authoritative instructions
`api/llm.go:2092-2093` and `:2195` instruct the model to treat a template's
`agent.usage` / `agent.outputs` as **authoritative** — that is provider-authored
content fetched over MCP. `:2302-2308` copies MCP tool descriptions and input
schemas verbatim into the model-visible catalog. `:2121-2124` re-injects project
memory every turn, and memory is written *by the model itself*
(`persistInitialProjectPlanMemory`) — a self-persisting instruction channel that
survives turns and profiles. Workspace files, runtime logs, and CI logs are fed
back raw; repo content enters wholesale via `hydrateWorkspaceFromRepository`.

Combined with auto-approve (§2.3) this is a complete injection chain: a template
author or anyone who can influence hydrated repo content plants instructions
that the system prompt explicitly elevates.

Fix: delimited "data, not instructions" framing for provider/tool/file-derived
text; drop the word "authoritative"; validate model-written memory entries.
VibeSDK's `sanitizeUserQueryForPrompt` (strips CommonMark link-reference
definitions — invisible in the UI, passed verbatim to the model) is worth
copying, as is the Claude SDK's practice of scanning agent-relayed output for
control-tag imitation.

### 2.3 Auto-approve authorizes destructive and externally-visible verbs
`api/assistant_permission.go:151-165` allows `RiskCommit`, `RiskRuntime`,
`select_template`, and `hydrate_workspace` in auto mode. `hydrate_workspace`'s
own description admits uncommitted work is lost; commit accepts a branch
override; promote reaches production; provision creates infrastructure. The
prompts say "confirm with the user first"; nothing structural enforces it.

Fix: carve destructive/externally-visible verbs out of auto-approve, or require
a plan grant that names them.

### 2.4 Multi-replica deployment corrupts live runs
`api/assistant_supervisor_http.go:1189-1236` decides a Running run is orphaned
purely by its absence from *this process's* in-memory map. A second replica — or
a rolling-deploy overlap — marks a live run `Interrupted` and suspends its
WorkItem while the owning pod keeps mutating the workspace. The chart hard-fails
on `replicaCount != 1`, which is honest, but it makes the whole assistant
surface a single point of failure with guaranteed downtime on every upgrade.
There is also a real data race at `:1205-1208` (reads `active.run.ID` after
unlocking).

Fix: stamp runs with an owner identity + heartbeat and only reconcile expired
owners; move the ID comparison inside the mutex. This is the prerequisite for
ever running more than one replica.

### 2.5 Silent tool-catalog degradation (the known turn-route trap, confirmed)
Three independent paths drop tools without telling anyone
(`api/assistant_eino_tool.go:108-208`): MCP discovery is gated on keyword
matching of **only the most recent user message** (early return at `:183`), so
"now provision it" after a rephrase loses the infra tools; a `DiscoverMCP` error
silently sets `IncludeCommitBridge=false`, making `commit_project_files` vanish;
discussion/guidance profiles are toolless by design and a durable run pins that
for the turn. All three present to the user as "the model refuses to work".
`ModelCalls.VisibleTools` already captures it in the audit — surface it to the
user, and widen MCP-need detection to the full turn window.

---

## 3. High — scale and time

Two independent reviewers found the same top blocker, which raises confidence.

| # | Finding | Where |
|---|---|---|
| 3.1 | **One global mutex serializes every workspace mutation across all tenants** | `workspace/store.go:162-165` — `mutationMu` on the whole `FileStore`, held across disk I/O *and* snapshot JSON writes. Per-scope striping is sufficient; the conflict checks already make broad locking unnecessary. |
| 3.2 | **Every streamed model chunk triggers an immediate durable snapshot write** | `api/assistant_eino_engine.go:830-836` → `persistMetadata(ctx, nil)` with `immediate=true`. The 250 ms coalescer applies only to text. A 2,000-token answer = hundreds of full run+message+metadata saves, revision bumps, and SSE fanouts. |
| 3.3 | **Unbounded per-run tool metadata, re-persisted whole on every event** | `api/projects.go:1426-1434` appends forever; each write/patch event embeds up to 16 KiB of patch. O(n²) bytes over a run, compounding 3.2. Strip patches from durable metadata (already in the undo snapshot) and cap retained events. |
| 3.4 | **Full-tree JSON sync on every mutation** | `api/development_sync.go` — up to 500 × 256 KB in flight, per component, through hub proxy + revdial, inside a 20 s budget. Needs delta sync via a hash manifest. |
| 3.5 | **Turn context capped at 24 messages with no cross-turn memory** | `api/llm.go:442-459`. In-run summarization compacts *within* a turn only; across turns anything older is simply absent, silently. Persist the summarizer's output as a durable rolling project summary. |
| 3.6 | **Unpaginated YAML list of all Packages/Commits on every checkpoint poll**, no Template caching, no HTTP client timeout | `tenant/graphql.go:52-61, 217-246` |
| 3.7 | Snapshots grow unbounded; only GC is project deletion; 1 Gi shared PVC | `workspace/snapshot.go`, `deploy/chart/values.yaml:82` |

Verdict: comfortable at tens of active projects, **not 500**.

---

## 4. Built-but-unwired (cheapest wins in the document)

- **Undo has no UI.** `POST /assistant/{run}/undo` is implemented end to end and
  `api.ts:355` defines `undoAssistantRun` — **zero callers anywhere in
  `portal/src`** (verified by grep). The portal even renders the *result*
  ("Restored N workspace files"). Every competitor's headline feature is one
  button away from working. Checkpoint-rollback is table stakes in 2026:
  Bolt puts it on every message, Lovable on every version, Cursor snapshots
  every agent action, Replit restores code *and* database together.
- **Provisional text is streamed and discarded.** The engine accumulates
  per-chunk text (`assistant_eino_engine.go:805-846`); both consumers have
  signature `func(_ string)` and drop it. The user sees nothing until a whole
  model message completes, while we pay the per-chunk persistence cost of 3.2.
- **The no-progress termination machinery is dead code.**
  `projectEinoAssistantNoProgressError` is constructed only in tests;
  `ConsecutiveNoProgressModelCalls` has zero production callers;
  `enforceRepeatedActionProgress` only injects a warning. The user-facing "did
  not make implementation progress" text, the `no_progress` WorkItem reason, and
  the audit mapping are all unreachable — a stuck model burns the full
  100-iteration budget. Either wire it to a real (small) limit or delete three
  files of misleading state.
- **Post-approval preview refresh is dead** (`applyPermissionResponse` returns
  `false` unconditionally), and **dead A2UI portal machinery** survives ~60 lines
  after the backend half was deleted.

---

## 5. What the market does that we don't

### 5.1 Nobody else lets a broken preview stay broken silently
This is our largest product gap. Dyad, Lovable, Bolt, and VibeSDK all inject a
script into the previewed app that hooks `window.onerror` /
`unhandledrejection` / console and `postMessage`s structured errors to the
builder shell, which appends them to the model's context. Dyad's fix button is
literally `streamMessage({prompt: "Fix error: " + msg})`. VibeSDK's sandbox
sidecar tails the dev server's JSON logs, dedupes by error hash, and exposes
`GET /errors` + clear-on-read.

We have neither end: the portal's iframe `@load` only checks token expiry
(`App.vue:2254`), so the known CHIPS cookie failure shows a gray frame with a
"Ready" badge; and no runtime error ever reaches the assistant. Our sandbox
runner and preview gateway are the natural injection points.

### 5.2 The fix ladder — don't send everything to the LLM
VibeSDK orders repair cheapest-first: deterministic AST fixes (no LLM) →
tiny per-file fix model fired the moment a file's stream closes → batched cheap
post-deploy pass → issues serialized into the *next* planning step → heavyweight
debug agent. v0 goes further: a dedicated RL-trained AutoFix model
(`vercel-autofixer-01`, 8,130 chars/sec — fast enough to outrun the base
model's stream) rewrites errors mid-stream, taking error-free generation from
64.7% (raw Sonnet 4) to 93.9%.

We go straight from "model wrote a file" to "model fixes it next turn", which is
the expensive rung.

### 5.3 Checkpoints must span code *and* data
Replit's App History rebuilds code + Neon copy-on-write DB branch + agent memory
at a timestamp; Dyad stamps a Neon timestamp per version. Lovable's revert
restores code but **not** database migrations — a documented sharp edge users
hit constantly. Our undo covers workspace files only; templates with a database
will hit Lovable's problem the day someone reverts.

### 5.4 Self-testing is now expected
Replit Agent 3's reflection loop drives Playwright *through the REPL* (model
writes JS with injected browser helpers) — 3× faster and 10× cheaper than
computer-use models, median $0.20/session, and it's what makes 200-minute
autonomy runs possible. It explicitly targets "Potemkin interfaces": UIs that
render but have no handlers. v0 shows screenshots from its own browser testing;
Devin attaches screen recordings to PRs. We verify via
`verify_development_runtime` status/logs only — we never look at the app.

### 5.5 Agent-computer interface design has measured answers
The SWE-agent ACI paper (arXiv 2405.15793) ablates the choices we make by
intuition: 100-line file windows beat both 30-line (−3.7 pts) and whole-file
(−5.3); summarized search beats iterative next/prev by 6 points (iterative is
*worse than no search at all* — agents exhaustively page through matches);
lint-guarded edits that atomically reject syntax-breaking changes are worth
3 points, and dropping the edit command entirely costs 7.7. Most striking for
us: **a single failed edit drops that edit's eventual success rate from 90.5% to
57.2%**, and cascading failed edits account for 23.4% of all failures — the
empirical case for validating before applying, which our patch-conflict recovery
lane already half-implements.

Cheap adoptions from the same corpus: search results as per-file match counts
with a hard refusal above 100 files; `PAGER=cat`/`TQDM_DISABLE=1` in the sandbox
to kill interactive noise at the source; an explicit "command produced no
output" message (silence confuses models); `bash -n` validation before executing
shell; truncation messages that *teach the remediation* ("use head/tail/grep,
do not use interactive pagers").

### 5.6 Condensation as an event over an immutable log
OpenHands never mutates its event log: the condenser emits
`CondensationAction{forgotten_start_id, forgotten_end_id, summary, offset}` and
views are recomputed by dropping the range and splicing the summary. Their V1
rewrite formalizes the invariants as property classes — `tool_call_matching`,
`tool_loop_atomicity`, `observation_uniqueness` — so compaction can never orphan
a tool call from its result. Compare our §6.1: phase is re-derived by
string-matching the transcript, so summarization can regress a verified run back
into `mutate`.

### 5.7 Sandbox lifecycle primitives we don't have
Daytona's TTL trio is the single most transferable API shape: `auto_stop_interval`
(inactivity → stop, default 15 min), `auto_archive_interval` (stopped → object
storage, default 7 days), `auto_delete_interval`, plus E2B's *renewable*
`setTimeout()` and an `onTimeout: pause|kill` policy. E2B's edge proxy resumes a
paused sandbox transparently on incoming traffic — scale-to-zero for previews.
Everyone treats snapshot/fork as the differentiating primitive (Morph forks a
*live* machine in <250 ms; Modal's `snapshot_filesystem()` returns an Image, so
the checkpoint *is* the template). Kubernetes has no native equivalent, which is
exactly why it's worth building on the infra provider's `SandboxRunner`.

### 5.8 Egress control is the settled answer to supply-chain risk
Default-deny egress with a proxy-enforced allowlist inside the sandbox is now
standard: Azure Container Apps Sandboxes ship a per-sandbox egress policy engine
where the proxy *injects* credentials so agent code never holds keys; Codex runs
internet-on during setup and off/allowlisted during the agent phase, with
HTTP-method restrictions to block write-verb exfiltration; Claude Code's cloud
sandbox keeps git credentials outside the sandbox behind a verifying git proxy.
Meanwhile the Shai-Hulud npm worm family specifically switched to preinstall
hooks on Bun *because* sandboxes instrument Node. Every `npm install` our
sandbox runs is a supply-chain event, and
`docs/app-studio-sandbox-runtime.md` already admits the runtime is
development-only.

### 5.9 Protocol conventions worth not reinventing
The typed-message-parts-over-SSE shape (Vercel AI SDK) or AG-UI's 17 typed
events are settled tech. Two mechanics we should copy regardless of format:
OpenHands' **replay from `latest_event_id + 1` on reconnect, holding the
state-changed event until last** so the client ends replay with correct status;
and their V1 refinement of **WS/SSE for push + a REST reconcile on reconnect**
so nothing is lost in the gap. Our snapshot-replace protocol is a defensible
alternative — but §7 lists two real holes in it.

---

## 6. Medium — architecture that will hurt later

1. **Phase is re-derived by string-parsing the transcript**
   (`assistant_eino_phase.go:1519-1676` matches `"tool call failed:"` prefixes)
   while authoritative counters (`sourceMutationRevision`,
   `verifiedMutationRevision`) sit unused for that purpose. After summarization
   or reduction fires, phase markers vanish and a verified-ready run re-derives
   as `mutate`, re-opening edit tools after verification passed — or a committed
   run loses its `report` terminality. Make the counters primary.
2. **No durable mid-turn checkpoint.** The Eino checkpoint store is per-turn and
   in-memory (`engine.go:139`); only interrupts persist. Crash after file 3 of an
   approved 5-file plan → Continue re-plans from ≤24 messages with verification
   state reset. `PersistRun` already supports flushing — do it after each
   successful mutation tool call.
3. **Middleware only wraps the invokable tool path.** Phase and lifecycle
   middlewares implement `WrapInvokableToolCall` only; the first `StreamableTool`
   anyone adds (streaming logs is the obvious one) silently escapes phase denial
   and commit-verification gating. Implement the other wrapper kinds as
   deny-unknown, or assert at build time that all tools are invokable.
4. **Background workers retain the originating `*http.Request`** and its bearer
   token (`assistant_supervisor_http.go:557,604,661`). A multi-minute run
   authenticates every MCP call with a token snapshot from the start request;
   expiry surfaces as a run-fatal generic tool failure.
5. **God files and hardcoded model knowledge.** `llm.go` (2,330 lines) mixes
   settings CRUD, provider wiring, prompt text, an MCP JSON-RPC client, and the
   turn entry point; `assistant_eino_phase.go` is 2,125 lines. Model capability
   is prefix string-matching (`gpt-5`/`o1`/`o3`) with fixed 24k summarization
   thresholds regardless of the model's real context window. Multi-model support
   needs a capability registry; extracting the MCP client and prompt corpus is
   the zero-risk first step.
6. **Three overlapping exclusivity layers** — run manager (in-memory preempt),
   supervisor reservations (in-memory), store CAS revisions (durable). The
   latter two are coherent and tested; the run manager mostly duplicates them.
7. **`RewriteCompletionAsVerification` destroys the model's text.** In
   mutate/repair with `NeedsCompletionVerification`, a tool-call-free answer —
   including a legitimate "I'm blocked because X" — is silently converted into
   another verification call, content dropped and never recorded. Preserve the
   text and allow a bounded "report blocker" escape.

---

## 7. Portal and API

**Correctness holes in the snapshot protocol** (both real, both narrow):
the portal fabricates an `aborted` snapshot that bumps `revision + 1` locally,
so the server's authoritative abort arrives at the *same* revision and is
rejected — abort metadata never lands until reload; and wholesale REST message
replacement doesn't reset `assistantRunRevisions`, so older DB state can
overwrite newer snapshot content and the repair snapshot is refused as a
duplicate. Add a client-side keepalive watchdog too — the server sends
`: keepalive` every 15 s and the reader never observes it, so a dead TCP path
hangs a foreground tab forever.

**Multi-user is a silent-data-loss trap** if projects are ever shared:
`latestProjectAssistantRun` returns 204 for a non-actor and a concurrent start
returns a bare 409, so the portal's handler removes the optimistic message,
restores the prompt, and never sets an error. User B's message simply vanishes.

**UX, ranked by daily irritation:** unconditional autoscroll
(`App.vue:1026-1029` pins to bottom on *every* messages mutation, ~4×/s during
streaming — scrolling up to reread is impossible); no diff view or file tree at
all (edits render as `Updated file X · +12 -3`); the workbench defaults to the
launcher tab rather than Preview, so the payoff moment needs a manual click;
iframe token renewal hard-reloads the preview on a timer, destroying in-app
state.

**API hygiene:** four different list-response shapes in one API (`{items}`,
`{templates}`, `{repositories}`, and a bare array for work-items); `writeError`
puts `err.Error()` verbatim into the Status message including the 500 default,
leaking internal DB/GraphQL strings — in contrast to the carefully allowlisted
action-feed diagnostics.

**Design-doc drift:** the thread-display redesign specified that six adjacent
file reads collapse into one `Read 6 project files` row; `groupAssistantActions`
only merges reads of the *same* target, so a 6-file inspection still shows 6
rows — exactly the "looks more complicated than the work" problem the redesign
set out to fix.

---

## 8. Suggested sequencing

**Phase 0 — correctness and audit — DONE (2026-07-31, see "What landed")**
0.1 ✅ Normalize `restart_runtime`/`set_runtime_env` onto the wrapper path (§2.1).
0.2 ✅ Fail-open approval wrapper removed outright (§2.1).
0.3 ✅ Gate promote on the commit SHA; restrict the workflow trigger (§1.1).
0.4 ✅ Error on truncated sync listings (§1.3).
0.5 ✅ Fix the two snapshot-revision holes + keepalive watchdog (§7).
0.6 ✅ Per-scope workspace locking (§3.1) and provisional persistence on
    transition only (§3.2).

**Phase 1 — the missing half of built features — DONE (2026-07-31)**
1.1 ✅ Undo wired end to end (including the `SnapshotID` gap that made it inert).
1.2 ✅ Autoscroll guard and Preview as the default tab.
1.3 ✅ No-progress machinery wired to a real bound.
1.4 ✅ Deletion for workspace + sandbox; **git deletion still open** (needs the
    code provider's commit API — carry into Phase 3/4).
1.5 ✅ Tool-catalog degradation surfaced; MCP scan window widened.

**Phase 2 — close the feedback loop — DONE (2026-07-31), except 2.1's client half**
2.1 ⚠️ **Server-side runtime errors captured and diagnosed; browser-side JS
    errors still uncaptured** — there is no kedge-owned hop in the preview HTTP
    path to inject a shim into. See the Phase 2 section for the three options.
2.2 ✅ Broken-preview detection (never-loads + redirect-loop) with a visible
    explanation and "Open in browser".
2.3 ✅ Fix ladder routes by diagnosed fault class; operational faults no longer
    open the source-repair lane.
2.4 ✅ Structured `projectRuntimeIssue`s with remediations replace free-text
    blockers through verification and into the repair lane.

**Phase 3 — scale — DONE except the lease (2026-07-31)**
3.1 ⚠️ Data race fixed; **durable owner lease still required before
    multi-replica** — see the Phase 3 section.
3.2 ✅ Delta sync via content hashes, with full-sync fallback on every event
    that invalidates the manifest.
3.3 ✅ Patches stripped from durable metadata; tool events capped.
3.4 ⬜ Durable rolling project summary — still open (§3.5).
3.5 ✅ Snapshot retention sweeper + HTTP client timeouts (template cache
    skipped as low value; tenant lists are already label-selected).
3.6 ⬜ Mid-turn checkpoint flush — still open (§6.2).

**Phase 4 — depth — partially done (2026-07-31)**
4.1 ⬜ Phase from run-state counters (§6.1) — deferred; deserves its own change.
4.2 ✅ Trust boundary in the prompt; irreversible verbs always confirm.
4.3 ◐ Edit-failure messages now teach the recovery; windowed reads and
    search-returns-counts still open (§5.5).
4.4 ⬜ Sandbox TTLs — belongs to the infrastructure provider (§5.7).
4.5 ⬜ Default-deny egress — belongs to the infrastructure provider (§5.8).
4.6 ⬜ Playwright self-testing — a feature in its own right (§5.4).
4.7 ⬜ `llm.go` decomposition — mechanical but large (§6.5).

---

## 9. What is genuinely good (calibration)

Worth stating plainly, because the list above is long:

- **Stop semantics are state-of-the-art** — durable `Stopping` persisted before
  cancellation, durable stop receipts making Stop idempotent and replay-safe,
  `AdmitMutation` as a serialized point of no return, and plan-grant tombstones
  so a resumed checkpoint cannot resurrect a retired commit grant. Tested.
- **Approval binding and replay defense are done right**: single-use request IDs
  claimed atomically, arguments frozen in the interrupt, stale-repository-binding
  detection, grants canonicalized on both create and check, write tools
  enumerated so a grant can never authorize commit or runtime verbs.
- **Cross-tenant containment holds.** Every call forwards the caller's own
  bearer token; MCP addresses the tenant's cluster ID; the MCP allowlist admits
  only infra list/describe/provision/get plus two reads — `pods_exec`,
  `resources_*`, and `delete_repository` are unreachable despite the aggregate
  endpoint federating them. The provider holds no runtime kubeconfig: the
  decoupling design is actually implemented, which is what makes BYO compute
  possible.
- **Workspace safety is thorough**: symlink rejection at every path component on
  read, write, and mkdir; atomic temp+fsync+rename; `O_EXCL` create-only;
  reserved-segment hygiene; conflict-detecting snapshots with rollback of a
  partially applied restore.
- **The supervisor design** — server-lifecycle-owned runs, `Reserve` closing the
  create/attach race, persist-before-publish, orphan reconciliation — is better
  than what most surveyed products describe publicly.
- **Postgres migrations** are versioned, transactional, and refuse unsafe
  backfill with an operator instruction rather than guessing.
- **Accessibility** in the plan popover and action log (focus trap, `inert`
  background, aria-live keyed to meaningful changes, reduced-motion) is well
  above typical for this class of tool.

The honest summary: we have built the parts that are hard to retrofit
(durability, isolation, audit) and skipped several that are cheap to add
(feedback loops, undo UI, delete). That is a much better position than the
reverse, and Phase 0 + Phase 1 are mostly small, well-scoped changes.
