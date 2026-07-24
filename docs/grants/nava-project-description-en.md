# Project Description

**Product working (code) name:** "NAVA" — an autonomous AI-agent process-automation platform
> **Note on the name:** "NAVA" is a **code (working) name**. The final product brand will be confirmed after SEO and brand analysis, so the publicly used name may differ.
**Applicant:** [COMPANY NAME], [reg. no.], [registration date]
**Call:** Financial incentives for startups and spin-off companies to create AI, blockchain, robotics and process-automation products and solutions
**Requested amount / intensity:** [e.g. 100,000] EUR, up to 90 % of eligible costs; applicant contribution — [e.g. 20] %
**Duration:** 12 months

---

## 1. Executive summary

The goal of the project is to build and bring to market **"NAVA" — an open-source, multi-tenant artificial-intelligence agent platform that automates enterprise operations and everyday work processes through a natural-language interface**. "NAVA" is **first and foremost an open-source project**, built in pursuit of **European digital sovereignty**: European organizations get a self-hostable AI-agent solution that does not depend on closed platforms operating outside EU jurisdiction, lets them freely choose the model provider (including European or local open models), and keeps control of data and decision logic within their own infrastructure.

The agents — driven by large language models (LLMs) — understand tasks in natural language, autonomously plan actions, use tools, keep long-term memory, run scheduled and event-driven jobs, and proactively notify humans through their usual channels (Slack, Telegram, email, portal). Instead of repetitive, manually performed operations, the AI agent carries them out itself, within clear human-approval boundaries.

The product's **core is the autonomous AI-agent platform** (natural-language processing + robotic process automation with human control). It is built on a **pluggable "provider" architecture**: agent capabilities are extended by independent modules, so the AI core runs standalone and integrations are added on demand. One such **optional** provider is secure connectivity to the customer's distributed infrastructure via outbound reverse tunnels (no VPN, no open ports), letting agents act in any environment with full audit. In the long-term vision, all infrastructure-connectivity components ("edges") become **optional providers** — the AI-agent platform is useful even with no connected infrastructure at all.

The product is available in two ways: as a **SaaS** (managed cloud service) and as a **self-hosted open-source deployment** in the customer's own infrastructure. The result is a market-ready (TRL 8–9) platform that reduces operational costs and human-error risk, targeting DevOps/platform teams, managed-service providers (MSPs) and mid-sized IT organizations in Lithuania and the EU.

---

## 2. Problem and market need

Modern organizations manage an increasingly **distributed and fragmented infrastructure**: multiple Kubernetes clusters, cloud accounts, edge devices, on-premises servers. Most day-to-day operations — deployments, incident investigation, configuration updates, compliance checks, reporting — are still performed **manually, repetitively and error-prone**, while skilled DevOps/SRE specialists are scarce and expensive.

Existing automation solutions have fundamental gaps:

- **Script-based automation** (scripts, CI/CD, runbooks) is brittle — it requires anticipating every case in advance and constant maintenance.
- **Traditional RPA** is oriented toward mimicking user interfaces, not infrastructure- and API-level operations.
- **AI chat assistants** lack secure, auditable operational access to real customer infrastructure and do not act autonomously on a schedule or on events.
- **Organizing access to distributed infrastructure** (VPN, open ports, kubeconfig distribution) is a security and operations burden.

**The core market gap — vendor lock-in and closedness.** Almost all of today's AI-agent and assistant products are **closed, cloud-only (SaaS-only) solutions with no self-hosted version**. The customer is forced to send their data and business logic to an external platform — usually one operating outside EU jurisdiction — cannot audit it, is tied to a single model provider, and is locked into one vendor's pricing and continued existence. For organizations that care about **data security, independence, regulatory compliance or sovereignty** (public sector, finance, healthcare, defence, critical infrastructure), such products are simply unusable.

**The market clearly lacks an open, AI-native platform for AI workloads** that any organization could run securely, independently and vendor-agnostically **in its own data centre or cloud**, freely choosing the model provider. This is exactly the gap "NAVA" fills: it combines LLM-based natural-language understanding with secure, auditable, self-hostable and vendor-neutral autonomous agent execution — within clear human-approval boundaries.

---

## 3. The solution — the "NAVA" product

"NAVA" is a platform in which each customer (tenant) builds and manages **their own AI agents** in an isolated workspace. The main product components:

**3.1. Autonomous AI agents (NLP core).** Each agent has a persona (system context), memory, a toolset and a behaviour policy. The user talks to the agent in natural language; through an LLM "tool loop" (plan → call a tool → observe the result → continue), the agent autonomously performs multi-step tasks.

**3.2. Autonomous execution — schedules, heartbeats and event triggers.** Agents act not only in reply to messages:
- **Schedules** (time-zone-aware cron) — recurring tasks ("every morning at 8, check…").
- **Periodic heartbeats** — the agent reviews its own checklist and escalates to the human only when action is needed (quietly, if nothing is new).
- **Event triggers** — external webhook / GitHub / channel events start the agent with the event payload.

**3.3. Tool ecosystem and integrations.** Agents use pluggable tools: web search and content fetching (SSRF-guarded), GitHub, arbitrary external tools via the Model Context Protocol (MCP), a file workspace, and — most importantly — **access to customer infrastructure** (Kubernetes clusters, servers) through the platform's control plane.

**3.4. Secure connectivity to customer infrastructure (optional provider).** When an agent needs to act in a customer environment, an optional infrastructure provider makes this safe: a component running in each environment **initiates an outbound reverse tunnel**, so systems behind NAT/firewalls become reachable through a single authenticated endpoint — no need to open ports or use a VPN. This is **optional**: the AI-agent platform works fully even with no connected infrastructure. Multi-tenant isolation is enforced at the workspace level (based on the open-source `kcp` control plane).

**3.5. Human control and security "by design".** Each agent's powers depend on context: in an interactive chat, risky actions are allowed only after approval, while unattended (scheduled) runs are read-only by default. An **approvals inbox** (in the portal and channels) lets a human approve or deny every risky action. All tool calls are recorded in an audit log. **Budgets** cap each agent's monthly LLM spend.

**3.6. Channels and portal.** The user talks to the agent not only through the web portal but also from their usual chat channels (Telegram, Slack) — two-way, including approvals.

**3.7. Operational "digital twin".** Through a graph query layer (GraphQL) the platform models the relationships of the customer's distributed infrastructure in real time — creating a **digital twin** of it, which agents' decisions rely on (e.g. "which workloads depend on this component").

**3.8. An open ecosystem — the provider model on the `kcp` framework.** The platform is built on the open-source **`kcp`** (Kubernetes-native control planes) framework, which makes it not a closed product but an **open platform-framework for extensions**. Any third party — a vendor, a systems integrator, or the customer organization itself — can build **their own provider** (its own API via `APIExport`, controllers, a UI micro-frontend, tool families), install it via Helm, and the platform auto-discovers and integrates it into the portal. This way agent and platform capabilities grow **beyond our sphere of influence**: the ecosystem is expanded by many independent participants, not a single vendor. The same `kcp` framework is already used by other projects (e.g. **platform-mesh.io**) and a growing number of vendors, so a shared, interoperable provider base is forming — a foundation for a future provider catalogue / marketplace. A standardized provider interface means that even infrastructure connectivity or future AI modules can be built by third parties and shared across the community.

---

## 4. Innovativeness and product novelty

"NAVA"'s novelty is not a single feature but a **unique architectural synthesis** that no competitor currently offers:

1. **Autonomous LLM agents + secure access to real distributed infrastructure.** Market AI assistants either lack operational access to customer systems or organize it insecurely. "NAVA" agents act through an auditable, multi-tenant control plane with reverse tunnels — enabling automation of real operations even behind a firewall, while preserving enterprise security requirements.

2. **Trigger-scoped trust model.** An agent's permitted actions differ based on who triggered it and whether a human is watching. This is an original solution to the core risk of autonomous AI agents — indirect prompt injection: unattended runs have no write capabilities by default.

3. **Persistent autonomy.** Unlike "one-shot" chatbots, "NAVA" agents have long-term memory, run on their own schedule, heartbeats and events, plan their next actions themselves and proactively notify the user.

4. **Pluggable "provider" architecture on the open `kcp` framework.** The platform is extended by independent modules, so agent capabilities (new integrations, tool families, even infrastructure connectivity) are extended without re-architecting the core system. Because it builds on the open-source `kcp` framework — also used by other vendors (e.g. platform-mesh.io) — the ecosystem can grow **beyond our sphere of influence**, forming the basis for an interoperable open-source provider ecosystem and commercial expansion (see §3.8).

5. **Open source and European digital sovereignty.** Unlike the dominant closed, non-EU-controlled AI-agent platforms, "NAVA" is open source and can be deployed on an organization's own infrastructure with a freely chosen (including European or local) model provider. This lets European organizations use autonomous AI without handing data, decision logic and dependency control to third parties — a direct contribution to EU technological sovereignty and transparency (anyone can audit it themselves).

Level of novelty: the product being built is **new to the market** (not merely at the company level). The project **does not finance scientific research or experimental development** — all activity is applied development of a **product** for the commercial market, from existing technological components to production readiness.

---

## 5. Alignment with AI domains and technological basis

The product directly matches **three** of the AI domains named in the call:

| AI domain | How it manifests in the product |
|---|---|
| **Natural-language processing (NLP)** | The primary interface and the agents' "mind" — the LLM understands tasks, plans, and generates responses and reports in natural language (LT/EN). |
| **Smart robotics and automation** | Robotic process automation: agents autonomously execute sequences of operations through tools and infrastructure APIs under policy-based control. |
| **Digital twins** | A real-time graph model of the customer's distributed infrastructure that agents' decisions rely on. |

**High-performance computing (HPC) and big data.** The project plans to use HPC / GPU infrastructure for LLM model serving and performance optimization (e.g. [specify — national HPC facility / LUMI / GPU cloud]), while **big-data** processing pipelines feed the agents' memory, the operational digital twin and audit analytics (operational telemetry and event streams from many customer environments). *(Meets the selection criterion "use of HPC / big data".)*

**Technological basis (applied, mature components):** LLM provider APIs (with self-hosted model serving as needed), an agent execution engine with a tool loop and checkpoint mechanism, a reverse-tunnel network layer (HTTP/1.1 + WebSocket, compatible with any reverse proxy), a multi-tenant control plane (`kcp`), a durable data layer (PostgreSQL), a GraphQL query layer.

---

## 6. Project activities and work plan (12 months)

All activity is **applied product development** to commercial readiness (no scientific research).

**Phase 1 (months 1–3) — Product core and architecture.**
- Production hardening of the agent execution engine and multi-tenant isolation.
- Bringing the secure control plane (reverse tunnels, OIDC authentication) to production.
- Implementing durable data storage (transcripts, memory, audit).

**Phase 2 (months 3–6) — Autonomy and tools.**
- Schedules, heartbeats and event-trigger subsystem.
- Tool families: web, GitHub, MCP integrations, file workspace, infrastructure operations.
- Trigger-scoped trust model and approvals inbox.

**Phase 3 (months 6–9) — HPC/data layer and digital twin.**
- HPC/GPU model-serving integration and performance optimization.
- Big-data pipelines for memory, RAG and the operational digital twin.
- Graph (GraphQL) model and portal visualizations.

**Phase 4 (months 9–12) — Commercial readiness, security and pilots.**
- Hardening of budgets, audit, data encryption and security.
- Completion of channel (Slack/Telegram) and OAuth integrations.
- Pilot deployments with [1–3] customers, documentation, deployment packages (Helm), commercial launch.

**Risk management:** model-provider independence (BYO / swappable models), cost control via budgets, security-risk management via the trigger-scoped trust model and audit.

---

## 7. Results and product readiness

By the end of the project the following will be delivered:

- A **market-ready "NAVA" product** (TRL 8–9): a SaaS version and a self-hostable package.
- At least **[1–3] pilot / paying deployments** with real customers.
- Deployment, security and user documentation; a commercial licensing and pricing scheme.
- A foundation for further ecosystem and export expansion (pluggable provider architecture).

---

## 8. Market, customers and commercialization

**Target market:** DevOps/platform teams, managed-service providers (MSPs), mid-sized IT organizations, technology companies with distributed infrastructure — in Lithuania and the EU. Broader context — the fast-growing AI-agent and IT-process-automation (RPA/AIOps) markets.

**Value proposition:** reducing operational costs by automating repetitive tasks, faster incident resolution, lower human-error risk, secure access to distributed infrastructure without a VPN.

**Commercialization model (open core):** the product core is open source, freely deployable in the customer's infrastructure; revenue comes from a **managed SaaS service** (subscription based on active agents/usage) and from **enterprise features and support** for self-hosted customers. Open source lowers the barrier to entry, builds community and trust, while self-hosting is decisive for sovereignty-sensitive customers (public sector, regulated industries) who require deployment without vendor dependency. Sales — direct and through a partner (MSP) channel.

**Ecosystem growth engine (network effect):** because the platform builds on a shared open-source `kcp` framework — also used by other vendors (e.g. platform-mesh.io) — the more third parties build providers, the more valuable the platform becomes for everyone. We can offer managed hosting (SaaS), provider certification and support for the growing ecosystem — a revenue stream that grows faster than the features we build ourselves.

**Go-to-market strategy:** pilots during the project → paid subscriptions → export within the EU market.

---

## 9. Team and competencies

We are a company that has worked in technology and open source for years, with deep competence in distributed systems, cloud infrastructure, Kubernetes and AI deployment. All of our products are open source and built on the principles of openness.

**Leadership in open-source communities.** We are active in the CNCF (Cloud Native Computing Foundation, Linux Foundation) ecosystem and are the originators and maintainers of projects we **lead ourselves**:
- `kcp.io` — a framework for multi-tenant Kubernetes-native control planes (the foundation of this project);
- `kbind.dev` — an API service-binding solution;
- `multicluster-runtime` (github.com/multicluster-runtime/multicluster-runtime) — a multi-cluster controller framework.

Part of Europe's — and many large companies' — **independence from the US technology stack** is built on these projects. We are also active participants in other projects (e.g. Kubernetes).

**Participation in EU-scale projects.** We actively contribute to large European initiatives:
- APEIRORA — https://apeirora.eu/ (also https://documentation.apeirora.eu/blog/2025-03-25-kcp-multi-tenant-control-planes);
- platform-mesh.io — https://platform-mesh.io/release-0.4/ (built on the same `kcp` framework — direct proof of the emerging provider ecosystem).

**Own commercial products (Edge/AI).** We are the **owners and builders** of Edge/AI deployment SaaS platforms:
- Synpse — https://synpse.com/;
- Faros — https://faros.sh/,

with which we have, for several years, helped customers manage infrastructure in complex, distributed environments, and where users can spin up self-hosted AI models at the click of a button. This directly demonstrates the team's ability to bring exactly this kind of product — open, vendor-neutral, self-hostable AI — to market.

**Public international talks on emerging technologies:**
https://www.youtube.com/watch?v=zBs2LG-Oi4w · https://www.youtube.com/watch?v=R9YUOo0MwqY · https://www.youtube.com/watch?v=y0JgZ-hQ-Bo · https://www.youtube.com/watch?v=43X0_U3cc-Y · https://www.youtube.com/watch?v=7op_r9R0fCo

**Customers and prior commercial projects.** For several years we have co-developed AI and infrastructure solutions with international customers — among them **Cast AI, Upbound, Clyso**. Invoices confirming sales revenue are attached to the application. *(Contracts cannot be provided due to NDA (confidentiality) clauses they contain.)* *(Meets the selection criterion "prior innovation projects" — ≥1 project in the last 2 years and ≥30,000 EUR in sales revenue; see the attached invoices.)*

---

## 10. Alignment with the call's selection (scoring) criteria

| Criterion (weight) | How the project meets it |
|---|---|
| **Use of HPC / big data** | HPC/GPU model serving + big-data pipelines for memory, RAG and the operational digital twin (§5). |
| **Prior innovation projects** | [Name prior projects and ≥30,000 EUR revenue — §9]. |
| **Product novelty** | A market-new architectural synthesis (autonomous agents + secure distributed control plane + trigger-scoped trust model) — §4. |
| **Co-financing above 10 %** | Company contribution — [e.g. 20–30] %, exceeding the minimum 10 % requirement. |
| **AI-domain focus** | Matches three domains: NLP, smart robotics and automation, digital twins (§5). |

---

## 11. Impact and horizontal principles

**Economic impact:** raising the productivity and competitiveness of Lithuania's IT sector, creating an exportable AI product and high-skill jobs.

**Strategic impact (digital sovereignty):** an open-source, European, vendor-neutral AI-agent solution reduces European organizations' dependence on closed, non-EU-controlled platforms and gives the public sector and regulated industries the ability to use autonomous AI on their own infrastructure with full audit and data control.

**Horizontal principles / "do no significant harm" (DNSH):** the product is a software platform with no significant direct environmental impact; energy efficiency is improved by the budget mechanism and efficient model use. Security, privacy and data protection are implemented "by design" (isolation, audit, encryption, human control). Equal opportunities and accessibility are ensured in the product interface.

---

*This document is a draft project description. The `[…]` fields and the working name "NAVA" are to be filled in / changed according to the applicant's data before submitting the application in the DMS system.*
