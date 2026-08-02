# NAVA — codebase reality check, adoption odds, and pitch focus (2026-07-30)

Internal memo. Companion to [`nava-research-2026-07.md`](./nava-research-2026-07.md)
(market/competitor research) — this one maps the **actual kedge codebase** against the
grant narrative and derives: chances of adoption, priority target areas, and pitch
framing. Snapshot taken at branch `agents-bigbang` (2026-07-30); repo stats: ~667 Go
files / ~183k Go LOC, 166 test files, 467 commits since 2026-01, ~2 human contributors.

---

## 1. Codebase vs. grant claims — what is real

**The grant's "unique architectural synthesis" is substantially implemented, not vaporware:**

| Grant claim | Code reality |
|---|---|
| Autonomous agents: tool loop, memory, schedules, heartbeats, triggers, channels, approvals, budgets (§3.1–3.6) | **Implemented** in `providers/agents` (~15k Go LOC): Eino engine with streaming + durable checkpoints (`engine/`), cron/wakeup/heartbeat via robfig/cron (`api/background.go`), webhook+GitHub triggers with filters, Telegram/Slack/Discord/SMTP outbound + inbound + OAuth, durable approvals inbox with pause/resume (`api/inbox.go`, `api/resume.go`), enforced USD/token budgets (`api/run.go`), memory tools, MCP pass-through. Postgres store. |
| Behind-firewall access to K8s **and plain Linux servers** (the verified-unoccupied wedge) | **Real**: revdial tunnel plane in `providers/edges/internal/tunnel/`, `KubernetesCluster` + `LinuxServer` (SSH) edge types. Constraint: in-process dialer map ⇒ single-replica tunnel plane. |
| Golden-path templates fused with agents (§ "auksinis kelias") | **Real**: `providers/infrastructure` — kro backend, 8 seeded templates (application, database, redis, cron-job, worker…), dev-sandbox loop (`dev-agent/` + `dev_sync`/`dev_logs`/`dev_restart` MCP tools), full MCP provisioning surface. |
| Pluggable provider architecture on kcp | **Load-bearing, not marketing**: CatalogEntry + heartbeat registry (`pkg/hub/providers/`), micro-frontend custom elements (not iframes), provider-side bootstrap (`provider-sdk/install/`), splitsh mirror repos. 11 providers exist; `quickstart` is the third-party template. |
| Digital twin / fleet query layer | `providers/kuery` (self-described Phase 2) — fleet search, relationship traversal, `kuery_query`/`kuery_impact` MCP tools. Thinnest of the core claims. |
| Multi-tenant control plane, embedded kcp, MCP aggregate | `pkg/hub/` — embedded kcp + etcd in one binary, MCP federation as `<provider>__<tool>`, OIDC/Dex, org/workspace quotas (count caps). |

**Provider size table** (Go LOC / portal TS LOC): app-studio 64.8k/1.9k · infrastructure
20k/1k · agents 15.4k/6.8k · edges 11.9k/1.1k · code 10k/0.9k · databricks 4.3k/0.8k ·
kuery 2k/2.2k · quickstart 0.4k/0.5k · kubernetesedges/serveredges/mcp **empty placeholders**.

## 2. The honest gaps (ranked by impact on adoption)

1. **OSS adoption funnel is broken.** `docs/getting-started.md` describes the 2025
   edges-only product; portal needs an undocumented `portal_embed` build tag; no
   aggregate install path for providers; Postgres requirements undocumented. A stranger
   self-hosting today gets the old product, not the AI platform. **Time-to-wow ≈ ∞.**
2. **Zero monetization rails.** No Stripe, no metering, no usage export anywhere in
   core. `pkg/hub/quota/quota.go` is org/workspace count caps only. The questionnaire's
   claim that quota pricing is "techniškai įgyvendinama, o ne deklaratyvi" is **not
   currently true** (see §5 fix list).
3. **Agents not proven end-to-end.** Own docs state "not yet driven end-to-end against
   a running hub — integration bugs expected" (`docs/agents-provider-architecture.md`,
   `agents-multi-channel.md`). No eval harness (the promised Gartner-risk mitigation).
   Missing: context compaction, file workspace, Gemini native support.
4. **Breadth sprawl.** app-studio is ~1/3 of the codebase in the most red-ocean category
   (Lovable/Qovery); databricks is niche; 3 empty placeholder provider dirs; several
   design docs contradict shipped code (`docs/providers.md` still "Design draft",
   getting-started stale, AGENTS.md references deleted `pkg/virtual/builder/`).
5. **Security story thin for sovereignty buyers.** `docs/security.md` is setup
   instructions, not a threat model. Two open security TODOs:
   `pkg/agent/tunnel/svc.go:182` (relaxed anti-SSRF boundary for LAN services),
   `pkg/hub/controllers/mcpserver/controller.go:232` (cluster-admin placeholder).
6. No self-serve signup (OIDC/static tokens only); repo hygiene (committed binaries,
   kubeconfigs, `kuery.db` at repo root).

## 3. Chances of picking up

**As currently aimed — low; refocused — genuinely decent.**

- **Against:** the AI-SRE/agent-ops category is the most capital-saturated space in
  infra software ($190M Resolve, Datadog, Microsoft — see research memo §2/§4); team of
  ~2 cannot win a feature, marketing, or enterprise-sales race. "AI agent platform" as
  a headline is a dead pitch in 2026 (buyer numbness + Gartner >40% cancellation wave).
- **For:** four *verified* absences nobody occupies (OSS tunnels; plain-server targets;
  free self-hosted multi-tenancy; templates+agent fusion), a funded sovereignty
  tailwind (+83% YoY EU sovereign-cloud spend, "buy European OSS" procurement), kcp
  maintainer credibility competitors can't fake, and a codebase 12–18 months ahead of
  any new entrant.
- Small teams win one way: **a wedge the funded players structurally cannot follow.**
  Datadog will never open-source; kagent will never do plain servers; Hyground will
  never be multi-tenant OSS. That wedge is already in the code.

## 4. Priority target areas

1. **Fix the funnel first (weeks, not months).** One documented path: zero → hub +
   portal + agents + one connected edge in <30 min (docker-compose or umbrella Helm
   chart including provider charts). Rewrite getting-started for the 2026 product.
   Until then every growth effort leaks 100% of traffic. (This is funded Phase-1 work.)
2. **Lead with the fleet wedge, not "agents".** The accidental killer feature is the
   edges service catalog: 17 service types (Home Assistant, Proxmox, UniFi, Grafana,
   Pi-hole, qBittorrent, Jellyfin…) with auto-generated MCP tools through the tunnel.
   That is a homelab/self-hosted community magnet (r/selfhosted, HN) = cheapest
   distribution available. Bottom-up: homelabber → their employer's MSP/platform team →
   paid SaaS. Uncopyable story (needs tunnels + plain servers + OSS simultaneously).
3. **Prove the agent loop e2e + ship the eval harness.** It's the named mitigation for
   the evaluators' most likely objection and doubles as marketing content
   ("GPT vs. local Llama on real ops tasks" — exactly what sovereign buyers share).
4. **Metering before billing.** Usage counters per tenant/agent now; Stripe later.
   Needed for (a) the quota-pricing claim, (b) pilot proof of the ~36k EUR/yr
   saved-FTE metric, (c) the DNSH/consumption-governance story.
5. **Cut/park breadth.** Delete empty `kubernetesedges`/`serveredges`/`mcp` dirs; park
   databricks; reframe app-studio as *proof of the golden path* ("agent creates apps
   only from approved templates"), not a Lovable competitor.
6. **Sovereignty credibility package:** real threat-model doc, close the two security
   TODOs, Phase-4 independent review. Sovereignty buyers read security docs before code.

## 5. Pitch framing

Core line (fleet framing, not agent framing):

> **"Open-source AI operations for your entire fleet — every Kubernetes cluster, Linux
> server, and edge box behind any firewall. Agents that act with human approval, on any
> model you choose, in your own datacenter."**

Per audience:

- **Grant evaluators:** keep the sovereignty + synthesis narrative (it's in good
  shape). Two consistency fixes:
  - **TRL framing is falsifiable as written.** Docs claim a TRL 3–4 start with
    "references to the working system removed" — but `faroshq/kedge` is public with
    183k LOC of exactly this. Same risk class as the "nerasta" regional claim the
    research memo already fixed. Safer: *"validated open-source prototype; the project
    funds productization to TRL 8–9"* — still fundable, no longer falsifiable.
  - **Quota-pricing sentence** (1.18): either build minimal metering in Phase 3 as
    planned or soften "techniškai įgyvendinama" until it exists.
- **OSS/community launch:** "MCP tools for everything you run — your homelab, your
  servers, your clusters — through one secure tunnel, with an agent that watches it
  while you sleep." Demo Home Assistant/Proxmox, not the enterprise slide.
- **MSP/commercial:** "White-label multi-tenant AI ops. Your tenants, your margin, our
  platform — free multi-tenancy every competitor paywalls." (Research memo §5: 87% of
  MSPs raising AI spend; only channel giving a 2-person company sales leverage.)

**Bottom line:** the technology has already earned a better distribution story than it
has. The constraint on adoption is the funnel and the framing, not the code. Fix the
30-minute quickstart, launch on the fleet/homelab wedge, let the MSP channel carry the
commercial motion. Launched instead as "another AI agent platform" with a broken
self-host path, it won't pick up regardless of code quality.
