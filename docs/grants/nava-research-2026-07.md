# NAVA — Deep research: competitors, alternatives, sustainability (2026-07-27)

Internal research memo synthesizing five parallel research passes (commercial AI SRE
vendors, open-source ecosystem, IDP/golden-path convergence, EU/Baltic landscape,
business sustainability). Sources inline. Companion to
`nava-klausimynas-1-produktas.md` — section 6 lists the concrete edits that memo
implies for the questionnaire. See also
[`nava-codebase-assessment-2026-07.md`](./nava-codebase-assessment-2026-07.md) —
codebase-vs-narrative reality check, adoption odds, and pitch-focus recommendations.

---

## 1. Verdict summary

| Claim in the questionnaire | Verdict |
|---|---|
| "No vendor combines open source + self-hosted + multi-tenant SaaS + action execution + behind-firewall access + golden-path templates" | **HOLDS** — verified across ~40 commercial vendors and ~30 OSS projects; nobody has all six. Closest partials: Robusta/HolmesGPT, Kubiya, Harness, Hyground (see §3). |
| "Prieiga už ugniasienės — ❌ nė vienas konkurentas" (no competitor has behind-firewall access) | **NEEDS NUANCE** — Datadog (Private Action Runner), PagerDuty (Rundeck runners), Kubiya (Local Runner), Harness (Delegate) all have outbound behind-firewall *execution runners*. What remains unoccupied: reverse-tunnel access to arbitrary K8s **and plain Linux servers** inside an **open-source multi-tenant** platform. No OSS AI-agent project has tunnels at all; none targets non-K8s servers. |
| "Regione (Lietuvoje ir Baltijos šalyse) ... nerasta" (no regional competitor) | **REFUTED as written** — Cast AI (Vilnius; Lithuania's 5th unicorn, Jan 2026) ships OpsPilot, an AI SRE agent with agentic runbooks. Claim survives only narrowed to: no *open-source, self-hostable, EU-jurisdiction* agent platform (Cast AI is closed SaaS, US-incorporated, K8s-cost-centric). |
| "Agentas ir auksinis kelias toje pačioje sistemoje — niekas kitas" | **SUBSTANTIALLY HOLDS** — but two encroachments must be named: Qovery AI Builder Portal (May 2026: blueprints + embedded coding agents + governance for non-technical users; proprietary, single-vendor, workspace-scoped) and Port ($100M Dec 2025 to become an "agentic engineering platform"; engineers-only). |
| Aurora as "closest open-source alternative" | **HOLDS but overweighted** — Aurora is nascent (378 GitHub stars, ~11 contributors, slowing releases). The real OSS threats are kagent, HolmesGPT, n8n. |
| Cleric read-only | **CONFIRMED** (vendor docs); public pricing exists: Team $2,000/mo, ~$20 per investigation. |
| Resolve AI figures | **CONFIRMED**: >$190M raised, $1.5B valuation (Apr 2026), Coinbase/DoorDash/MongoDB/Salesforce in production. |

**Missing from the questionnaire entirely (must add):** Hyground, Azure SRE Agent,
Datadog Bits AI, Cast AI, kagent, HolmesGPT-as-threat (currently only mentioned in
passing).

---

## 2. Landscape synthesis

### 2.1 Commercial AI SRE / ops agents (US-led, heavily funded)

- **Resolve AI** — $35M seed → $125M A at $1B (Feb 2026) → +$40M at **$1.5B** (Apr 2026); >$190M in <18 months. Investigation-first, moving into gated execution; SaaS + in-env "satellite" gateway; no self-host, no EU region commitment, no templates. ([TechCrunch](https://techcrunch.com/2026/02/04/ai-sre-resolve-ai-confirms-125m-raise-unicorn-valuation/), [PRNewswire](https://www.prnewswire.com/news-releases/resolve-ai-announces-series-a-extension-at-a-1-5b-valuation-and-launches-resolve-ai-labs-to-advance-ai-systems-for-complex-production-environments-302743888.html), [security](https://resolve.ai/security))
- **Datadog Bits AI** — SRE agent GA Dec 2025; DASH June 2026 added **Bits Remediation (preview)** and autonomous Infrastructure Operations with approval guardrails, on top of the existing **Private Action Runner** (self-hosted, 300+ actions behind the firewall). Pricing cut ~75% to ≈$6.50/investigation. The most complete big-vendor threat stack. FY2025 revenue $3.43B. ([GA PR](https://www.datadoghq.com/about/latest-news/press-releases/datadog-launches-bits-ai-sre-agent-to-resolve-incidents-faster/), [DASH 2026](https://www.datadoghq.com/blog/dash-2026-new-feature-roundup-keynote/), [private actions](https://docs.datadoghq.com/actions/private_actions/))
- **Microsoft Azure SRE Agent** — **GA Mar 10, 2026**; approval-gated and "privileged" autonomous modes; multi-cloud claims; hyperscaler distribution; 35,000+ incidents mitigated. Azure-only SaaS. ([GA](https://techcommunity.microsoft.com/blog/appsonazureblog/announcing-general-availability-for-the-azure-sre-agent/4500682))
- **Traversal** — $48M (Sequoia/Kleiner Perkins); "Workers" now execute remediation; SaaS/single-tenant/BYOC; Amex strategic investment. ([launch](https://www.traversal.com/blog/launch-announcement), [architecture](https://docs.traversal.com/architecture/intro.md))
- **Cleric** — $9.8M seed; read-only by design; public pricing $2,000/mo team / ~$20 per investigation; single-tenant GCP SaaS, EU region only on request. ([pricing](https://cleric.ai/pricing), [launch](https://www.businesswire.com/news/home/20251209625361/en/Cleric-Launches-the-First-Self-Learning-AI-SRE))
- **Kubiya** — executes with policy gates; **Local Runner = outbound-only HTTPS operator in the customer cluster** (closest commercial analog to NAVA's tunnel model); hybrid control plane can be self-hosted; $15K–72K+/yr public pricing; only $12M raised. ([runners](https://docs.kubiya.ai/docs/local-runners/installation), [pricing](https://www.kubiya.ai/pricing))
- **PagerDuty** — four-agent suite; SRE Agent as "virtual responder" (EA Q2 2026, autonomous H2 2026); behind-firewall execution via Rundeck runners; FY2026 revenue $493M, first profitable year; EU region (Frankfurt). ([Spring 2026](https://www.businesswire.com/news/home/20260312121276/en/), [FY26](https://www.pagerduty.com/newsroom/pagerduty-announces-fourth-quarter-full-year-fiscal-2026-financial-results/))
- **incident.io** — AI SRE (investigate + draft PRs, no infra execution); ~$400M valuation; **strongest EU data-residency posture** of the group (Belgium primary, "no customer data leaves Europe"). ([AI SRE](https://incident.io/ai-sre), [GDPR](https://incident.io/blog/incident-io-gdpr-compliance-guide))
- **Harness** — $5.5B valuation (Dec 2025), >$250M ARR; Autonomous Worker Agents GA June 2026 (pipeline-scoped execution); **the only vendor with real golden-path templates (Harness IDP, Backstage-based)** — but not fused with an ops agent, proprietary, delivery-centric. ([agents GA](http://www.prnewswire.com/news-releases/harness-launches-autonomous-worker-agents-for-software-delivery-302814180.html), [IDP](https://developer.harness.io/docs/internal-developer-portal/overview/))
- **Others**: New Relic Agentic Platform (Feb 2026, preview), Dynatrace Davis agentic remediation ($2B ARR), Lightrun AI SRE ($115M total), NeuBird Falcon, Ciroos ($21M), Komodor Klaudia, Deductive, Parity, Anyshift, Mirantis Lens Agents (agent-governance flank).

### 2.2 The most dangerous competitor for the *grant narrative*: Hyground

**Hyground (Hamburg)** — "sovereign, self-hosted AI SRE agent for Europe": runs
entirely in the customer's K8s cluster, air-gapped Helm install, BYO-model, resolves
incidents AND runs scheduled operations; €3M pre-seed Mar 2026 (Partech); production
at **Deutsche Bahn** (MTTR <5 min claimed). Occupies the exact "EU sovereign
self-hosted agent that executes" story. Differentiation NAVA retains: open source,
multi-tenant SaaS + platform (Hyground is single-org licenses), plain-server/edge
fleet via tunnels (Hyground is in-perimeter K8s only), golden-path templates,
provider extensibility. ([hyground.ai](https://hyground.ai/), [Partech](https://partechpartners.com/news/hyground-raises-3m-pre-seed-round-to-build-the-sovereign-sre-agent-for-enterprise-it-operatons))

### 2.3 Open-source ecosystem (threats and validation)

- **kagent** (Solo.io, CNCF Sandbox, Apache 2.0) — the only OSS project that already *is* a K8s agent platform: HITL tool-approval gates, event triggers (khook), Slack/Discord/Telegram, model-agnostic; 3.4k stars, ~165 contributors, near-daily releases. Weaknesses to exploit: **multi-tenancy is an acknowledged open problem** (reserved for Solo enterprise), in-cluster agent-per-cluster topology (inverse of hub+tunnels), no plain-server story, no app templates. **Biggest OSS threat.** ([kagent.dev](https://kagent.dev/), [CNCF](https://www.cncf.io/projects/kagent/))
- **HolmesGPT** (Robusta + Microsoft, CNCF Sandbox) — credibility leader of AI-SRE OSS; historically read-only, now shipping a K8s Remediation MCP toolset; 2.9k stars, ~71 contributors. Microsoft productization inside Azure is the tail risk. ([repo](https://github.com/HolmesGPT/holmesgpt), [CNCF blog](https://www.cncf.io/blog/2026/01/07/holmesgpt-agentic-troubleshooting-built-for-the-cloud-native-era/))
- **n8n** — 198k stars, $5.2B valuation (SAP investment 2026), first-class HITL approval gates + triggers + channels. Not an infra platform and its Sustainable Use License legally blocks assembly into one — but it absorbs "good-enough ops automation" budgets and commoditizes approvals/triggers/channels. ([SUL](https://docs.n8n.io/sustainable-use-license/), [SAP](https://www.trendingtopics.eu/sap-bets-big-on-ai-invests-in-n8n-at-a-5-2-billion-valuation/))
- **Aurora (Arvo AI)** — Apache 2.0, LangGraph incident RCA; **weak execution capacity** (378 stars, ~11 contributors, slowing cadence). Keep in the table, stop calling it the main OSS threat. ([repo](https://github.com/Arvo-AI/aurora))
- Notable churn: Keep acquired by Elastic (stagnating), Flowise acquired by Workday (slowing), Botkube dead, HumanLayer pivoted away from approvals, AutoGPT platform non-OSI. Free self-hosted **multi-tenancy is universally paywalled or license-prohibited** (n8n, Dify, Flowise, CrewAI AMP, LangGraph Platform, OpenHands Enterprise, Solo enterprise) — NAVA giving it away in OSS is a genuine wedge *and* a monetization design risk (see §5).
- **Agent runtimes commoditizing** (good for NAVA, consume don't fight): Linux Foundation AAIF (Dec 2025) now hosts MCP, AGENTS.md, goose; Claude Agent SDK is MIT; Codex CLI and Gemini CLI are Apache 2.0.

### 2.4 kcp health (the foundational bet)

Defensible, moderately de-risked: CNCF **incubation application in motion**
([cncf/toc#1909](https://github.com/cncf/toc/issues/1909)), contributor authorship
accelerating (~880 commits/31 authors Jan–Jul 2026), disciplined 2-month release
cadence, production adopters (Upbound; Kubermatic KDP GA Jan 2026; SAP ApeiroRA /
platform-mesh via NeoNephos with EU IPCEI funding). Risks: two-vendor concentration
(Kubermatic + SAP, bus factor ~3–4), pre-1.0 API churn (recurring migration tax),
ecosystem smaller than vCluster/Crossplane. Mitigations: internal abstraction,
budget one migration sprint per two minors, keep the maintainer seat. Tripwires:
the TOC incubation vote; Kubermatic's continued KDP investment.

### 2.5 IDP / golden-path convergence

- **Port**: $100M (Dec 2025, $800M valuation) explicitly to become an "agentic AI hub"; agents can execute self-service actions with per-action approval config. Engineers-only. ([SiliconANGLE](https://siliconangle.com/2025/12/11/port-nets-100m-turn-developer-portal-agentic-ai-hub/), [docs](https://docs.port.io/ai-interfaces/ai-agents/overview))
- **Qovery AI Builder Portal** (May 2026): the closest single overlap — blueprints + embedded coding agents (Claude Code/OpenCode) + governance proxy, explicitly for non-technical employees, on customer's K8s. Proprietary, single-vendor PaaS-shaped, dev-workspace-scoped rather than app-lifecycle golden paths. ([Qovery](https://www.qovery.com/blog/the-lovable-experience-enterprise-governance-your-infrastructure-we-built-it))
- **Humanitec / Syntasso Kratix / Cortex / Roadie-Backstage**: repositioned as governed *backends/context for agents others bring* (MCP servers); "golden paths as MCP tools" is now a recognized integration pattern, not a product. ([Humanitec](https://humanitec.com/products/platform-orchestrator), [Roadie MCP](https://roadie.io/docs/api/roadie-mcp/scaffolder/))
- **OpenChoreo/CNCF** (July 2026 blog): open-source architectural statement closest to NAVA's ("humans and agents as co-equal platform consumers") — developer-only, no business-user storefront. ([CNCF](https://www.cncf.io/blog/2026/07/21/platform-engineering-for-the-agentic-enterprise-managing-applications-resources-and-ai-agents/))
- Analyst frame: Gartner — 80% of large engineering orgs will have platform teams by 2026; 40% of enterprise apps embed task agents by end-2026; **counterweight: >40% of agentic AI projects canceled by end-2027** (cost/value/risk) — address proactively in the risk section. Platform-engineering market ≈$10.4B (2026) → $31.6B (2031). ([Gartner](https://www.gartner.com/en/newsroom/press-releases/2025-06-25-gartner-predicts-over-40-percent-of-agentic-ai-projects-will-be-canceled-by-end-of-2027), [Mordor](https://www.mordorintelligence.com/industry-reports/platform-engineering-and-internal-developer-platform-idp-market))

### 2.6 EU / Baltic landscape

- **Cast AI (Vilnius)** — unicorn Jan 2026 (>$1B; ~$272M raised); OpsPilot AI SRE agent + agentic runbooks + autonomous remediation with approvals. Closed SaaS, K8s-cost-optimization-centric, **Miami-incorporated** → adjacent-turning-competitor, not a sovereignty play. Name it in the grant. ([LRT](https://www.lrt.lt/en/news-in-english/19/2805099/lithuania-s-fifth-unicorn-vilnius-based-cast-ai-crosses-1bn-valuation), [OpsPilot](https://cast.ai/blog/meet-opspilot-your-ai-sre-agent-built-into-cast-ai/))
- **SUSE** — Rancher Prime "Agentic AI Ecosystem" (KubeCon EU 2026), marketed for digital sovereignty. Biggest EU incumbent threat. ([SUSE](https://www.suse.com/c/kubecon-eu-2026-first-agentic-ecosystem-platform/))
- **Qovery (FR)** — see §2.5. **Oxylabs (Vilnius)** — $3.6B unicorn July 2026, agent *data* infrastructure, not ops. **n8n (Berlin)** — see §2.3.
- **Sovereignty demand is quantified and citable**: European sovereign-cloud spend $6.9B (2025) → **$12.6B (2026), +83% YoY** (Gartner; worldwide $80B); EU Council Declaration on Digital Sovereignty (Dec 2025); Apply AI Strategy promotes **"buy European" public-sector procurement with a focus on open-source AI**; Cloud and AI Development Act promotes open source; €200B InvestAI; Lithuania won a €130M InvestAI AI-factory consortium. White space confirmed: **no EU-native managed agentic-AI platform** comparable to US hyperscalers. ([Gartner](https://www.gartner.com/en/newsroom/press-releases/2026-02-09-gartner-says-worldwide-sovereign-cloud-iaas-spending-will-total-us-dollars-80-billion-in-2026), [Apply AI](https://digital-strategy.ec.europa.eu/en/policies/apply-ai), [CADA](https://digital-strategy.ec.europa.eu/en/policies/cloud-and-ai-development-act))
- **AI Act timing**: GPAI/transparency enforcement from **Aug 2, 2026** (fines to €15M/3%); Digital Omnibus postponed Annex III high-risk deadline to **Dec 2, 2027**; no agent-specific regime — agents fall under existing rules; whether ops agents on critical infrastructure are Annex III high-risk is the live debate → compliance-driven buying window for auditable, self-hosted, human-overseen platforms. ([Gibson Dunn](https://www.gibsondunn.com/eu-ai-act-omnibus-agreement-postponed-high-risk-deadlines-and-other-key-changes/), [AI Act Service Desk](https://ai-act-service-desk.ec.europa.eu/en/faq))

---

## 3. The defensible wedge (verified unoccupied, July 2026)

Across ~40 commercial vendors and ~30 OSS projects, nobody combines all six of:
open source · self-hostable · multi-tenant SaaS · action execution ·
behind-firewall/NAT access · golden-path app templates.

| Vendor | OSS | Self-host | Multi-tenant SaaS | Executes | Behind-firewall | App templates |
|---|---|---|---|---|---|---|
| Robusta/HolmesGPT | ✅ core | ✅ ent. | ✅ own SaaS | ⚠️ K8s MCP remediation | ✅ in-cluster, K8s only | ❌ |
| Kubiya | ⚠️ SDK | ✅ | ✅ | ✅ | ✅ Local Runner | ⚠️ provisioning only |
| Hyground | ❌ | ✅ only mode | ❌ | ✅ + scheduled | n/a (in-perimeter) | ❌ |
| Harness | ❌ | ✅ (AI likely SaaS-only) | ✅ | ✅ pipeline-scoped | ✅ Delegate | ✅ IDP |
| Datadog | ❌ | ❌ | ✅ multi-org | ✅ preview | ✅ Private Action Runner | ❌ |
| PagerDuty | ❌ | ❌ | ✅ | ✅ approved automations | ✅ Rundeck runners | ✅❌ (runbooks ≠ templates) |
| Azure SRE Agent | ❌ | ❌ | ✅ Azure | ✅ | ⚠️ Arc reach | ❌ |
| kagent | ✅ | ✅ | ❌ (tenancy unsolved) | ✅ gated | ❌ in-cluster only | ❌ |

Uniquely verified absences across the entire field:
1. **Reverse-tunnel behind-firewall access in any OSS agent project: zero.**
2. **Plain Linux servers as a target: zero** — the whole field is K8s + cloud APIs.
3. **Free self-hosted multi-tenancy: zero** — paywalled or license-prohibited everywhere.
4. **Golden-path app templates fused with an ops agent: zero** (Harness has templates without the agent; Qovery has agents+blueprints for dev workspaces only).

## 4. Threats ranked (synthesis)

1. **Datadog** — behind-firewall runner + remediation preview + 75% price cut + $3.4B revenue distribution.
2. **Resolve AI** — capital ($190M) + marquee logos + own model lab; moving into execution.
3. **Microsoft** (Azure SRE Agent GA + HolmesGPT co-maintainership + Agent Framework) — squeezes from cloud and OSS sides simultaneously.
4. **kagent/Solo.io** — could ship multi-cluster tenancy and become the CNCF-default agent platform.
5. **Hyground** — narrative collision on "EU sovereign self-hosted"; tiny but Deutsche Bahn reference is potent.
6. **Cast AI** — regional narrative collision; expanding agentic scope quarterly.
7. **n8n / Port / Qovery** — budget absorption and golden-path encroachment from adjacent categories.

## 5. Sustainability assessment

**Top risks (ranked, evidenced):**
1. **Capital asymmetry in a crowded segment** — Resolve $190M/18mo; Traversal $48M; labs commoditizing generic orchestration from above (OpenAI Frontier + consulting alliances, Google GEAP, Anthropic Managed Agents). Survival depends on the parts the funded players aren't doing: MSP multi-tenancy, open core, EU sovereignty, servers/edge, templates.
2. **Open-core boundary erosion** — HashiCorp→OpenTofu and Redis→Valkey (83% enterprise adoption of the fork) prove relicensing destroys the asset; if "enterprise features" are things the community rebuilds, revenue thins. Reference patterns: Grafana (proprietary cloud, never relicense core), GitLab buyer-based tiering. NAVA-specific tension: free multi-tenancy is the wedge *and* the thing everyone else charges for — the commercial line must sit at managed-SaaS convenience + compliance features, not tenancy itself.
3. **Platform churn beneath the product** — model layer (Claude 3.5 retired with ~2 months notice; OpenAI killed AgentKit's Agent Builder in 13 months) multiplies adapter maintenance across BYO-LLM; CNCF layer has documented maintainer-burnout precedents (External Secrets near-shutdown) and kcp's bus factor ~3–4. Budget upstream maintenance as a real cost line.
4. (Grant-specific) **Gartner: >40% of agentic AI projects canceled by end-2027** — evaluators may cite it; pre-empt with the eval harness, quota governance, and human-approval design.

**Top strengths (ranked, evidenced):**
1. **Model-agnosticism is now a validated requirement**: 37% of enterprise CIOs run 5+ models (a16z 2026); 81% fear single-vendor dependency; ServiceNow bought OpenAI *and* Anthropic; Gartner: 70% of multi-LLM orgs on AI gateways by 2028. Labs cannot credibly be the neutral layer.
2. **Open-core comparables thriving + unit-economics tailwind**: n8n €300M→$5.2B in ~15 months on ~$40M ARR; LangChain $1.25B; Grafana 80–90% gross margins; GitLab $759M revenue. Inference prices falling ~10x/yr (Epoch: ~200x/yr median post-2024) → quota-based COGS shrinks continuously; BYO-LLM moves inference off NAVA's P&L entirely. Counterforce: agentic token volume grows.
3. **MSP channel demand documented with rails built**: 87% of MSPs raising AI investment by 2026; 93% use GenAI but only ~25% see operational impact (tooling gap); Pax8 Agent Store (Oct 2025) proves the marketplace motion; white-label multi-tenant beats resale on MSP margins; AIOps market to $36–42B by 2030.
4. **EU sovereignty demand quantified** (see §2.6) and regulatory timing favorable (Aug 2026 transparency enforcement; Dec 2027 high-risk deadline).

**DNSH/environmental**: criticism is real (IEA: AI-DC electricity +50% in 2025;
agents explicitly named as rising energy use) but the defensible story exists:
per-query footprint quantifiable (Mistral LCA: 1.14 g CO₂ / 45 mL water per
400-token query — usable as DNSH baseline), per-task efficiency improving at record
rates, quota metering doubles as consumption governance, BYO-LLM permits
EU-hosted efficient models, and infra-optimization agents can be net
energy-negative. EU AI Act currently imposes reporting/voluntary codes, not binding
limits; EED data-centre rules land on hosting providers. ([IEA](https://www.iea.org/reports/energy-and-ai/energy-demand-from-ai), [Mistral LCA](https://multilingual.com/mistral-ai-lifecycle-analysis/), [White & Case](https://www.whitecase.com/insight-alert/energy-efficiency-requirements-under-eu-ai-act))

---

## 6. Concrete edits implied for `nava-klausimynas-1-produktas.md`

1. **1.6 regional claim — MUST FIX (falsifiable in one search)**: replace "nerasta"
   with: region has adjacent players — Cast AI (Vilnius unicorn; closed SaaS AI SRE
   bolt-on, US-incorporated, K8s-cost-centric) and Oxylabs (agent data infra) — but
   no open-source, self-hostable, EU-jurisdiction agent *platform*; that narrower
   claim verified.
2. **1.6 table/prose — add rows/paragraphs**: Hyground (sovereign self-hosted DE,
   Deutsche Bahn), Azure SRE Agent (GA 2026-03), Datadog Bits AI (GA 2025-12,
   remediation preview + Private Action Runner), kagent (OSS platform, tenancy
   unsolved). Optionally split Aurora's row weight down.
3. **"Prieiga už ugniasienės" column — re-scope**: several vendors have outbound
   execution runners (Datadog, PagerDuty, Kubiya, Harness); NAVA's unoccupied claim
   is tunnels to arbitrary K8s **and plain servers** in an open-source multi-tenant
   platform. Reword the advantage bullet accordingly (currently claims "no
   competitor has this model" — too broad).
4. **1.5 — name Hyground** as proof the sovereign-self-hosted demand is real
   (Deutsche Bahn), while noting it is closed, single-org, K8s-only, template-less.
5. **1.6 advantage list — add honest encroachment sentence**: Qovery AI Builder and
   Port validate the agents+templates convergence; neither is open source,
   multi-tenant, or business-user + app-lifecycle scoped.
6. **1.17 risk table — add row**: "Agentinių DI projektų nusivylimo banga (Gartner:
   >40 % projektų nutraukiama iki 2027)" mitigated by eval harness, quotas,
   approval-gated design, pilots with measured value.
7. **Optional strengtheners**: cite sovereign-cloud growth (+83% YoY EU 2026) and
   "buy European open-source" procurement language in 1.4/1.6; cite Cleric/Datadog
   per-investigation pricing as evidence buyers pay per-operation (supports quota
   model); add DNSH paragraph with Mistral LCA figure to 1.13/1.14 area or the
   aprašymas DNSH section.
