# Providers — extending kedge with provider UIs, virtual workspaces, and APIs

**Status:** Design draft (ready for phase-1 implementation)
**Owner:** TBD
**Last updated:** 2026-05-22

---

## Restore-from-reboot summary

> This section exists so a fresh Claude Code session (or a returning human)
> can pick up the work without re-reading the conversation history.

**Where we are:** design phase complete. No code written yet. The branch is
`mcp.example` on a clean tree apart from this doc and a stray `bob` file.

**Goal in one sentence:** make kedge pluggable so third parties can ship
"providers" that bring an `APIExport`, optional UI, optional backend HTTP
service, and optional controllers — all installed via Helm, discovered and
wired up by hub controllers, surfaced in the portal under
`/providers/{name}`, proxied to avoid CORS.

**Decisions already pinned** (don't re-litigate; jump to §"Hub changes" for
the how):

| # | Decision | Rationale |
|---|---|---|
| 1 | Terminology = **provider** (not "addon") | `root:kedge:providers` already exists; first-party kedge `APIExport`s already live there |
| 2 | UI embedding = **iframe via hub proxy** | Same-origin → no CORS. Any frontend stack. Module Federation rejected (Vue lock-in + build coupling) |
| 3 | Provider workspace = `root:kedge:providers:{name}`, **auto-created by hub** on `ProviderCatalogEntry` admission | Chart needs no kcp credentials |
| 4 | Distribution = **one Helm chart per provider**, targets *host cluster only* | All kcp work owned by hub catalog controller |
| 5 | Registration = **hybrid**: chart creates `ProviderCatalogEntry` shell; provider pod heartbeats every 30s (`POST /api/providers/{name}/heartbeat`, TTL 90s) | Declarative install + runtime liveness |
| 6 | VW = **APIExport-only by default**; `spec.virtualWorkspace.url` is an opt-in escape hatch under `/services/providers/{name}/vw/*` | Most providers won't need a VW; lowers bar |
| 7 | Provider→kcp identity = SA `provider` in the provider's workspace; hub mints kubeconfig and writes it as Secret `kedge-provider-kubeconfig` in the provider's host namespace; **24h token rotation** | Reuses existing exec-credential pattern from `pkg/server/proxy/proxy.go` |
| 8 | Schema delivery = **inline** in `ProviderCatalogEntry.spec.apiExport.schemas[].body`; hub parses + applies | Solves chicken-and-egg of "chart can't apply to workspace that doesn't exist yet" |
| 9 | PermissionClaim acceptance = **auto-accept-all** at Enable time, but ONLY for claims marked `tenantScoped: true`. Non-tenant-scoped claims refused unless admin sets `kedge.faros.sh/accept-untrusted-claims=true` on the `ProviderCatalogEntry` | Simplest safe default; per-claim toggles deferred to v2 |
| 10 | Binding upgrade gate = `ProviderBinding.spec.acceptedClaimsHash`; chart upgrade adding new claims marks binding NotReady until user re-confirms | Prevents silent privilege escalation via chart upgrade |

**Deferred (do NOT block phase 1):**

- GraphQL discovery of provider CRs after `APIBinding` lands — **must work
  by end of phase 3**; gateway already does APIExport-based discovery for
  first-party CRs, so expected to "just work", but needs validation. If it
  doesn't, file follow-up; do not gate phase 1–2.
- Cross-provider dependencies — **explicitly out of scope** for v1. A
  provider's controller can error out if its prerequisite APIExport isn't
  bound.
- Heartbeat over kcp leases instead of HTTP — possible v2 simplification.
- Per-permission-claim UI toggles — v2.

**Next concrete step:** implement phase 1. See §"Phase 1 implementation
plan" near the bottom for the file-by-file checklist.

---

## Goal

Make kedge a pluggable platform. A *provider* is a self-contained extension
that brings:

1. An **`APIExport`** in kcp that user tenants bind to consume the
   provider — the *one required piece*.
2. A **UI** (micro-frontend, any stack) shown inside the kedge portal —
   optional.
3. Optional **controllers** reconciling the provider's resources.
4. Optional **custom HTTP backend** (REST/GraphQL/WebSocket) for the UI
   to talk to, proxied through the hub.
5. Optional **virtual workspace** (advanced) for non-CRD verbs.

A user opens the portal, browses the "Providers" view (catalog), clicks
**Enable** on a provider, and:

- The provider's APIs become available in their tenant workspace via an
  `APIBinding`.
- The provider's UI (if any) appears under `/providers/{name}` in the
  portal — proxied through the hub, so it is same-origin and there are no
  CORS concerns.

## Why "provider" (terminology)

The kcp workspace `root:kedge:providers` already exists and is where
kedge's own `APIExport`s live (`kedge.faros.sh`, `tenancy.kedge.faros.sh`,
`core.faros.sh`). See
[config/kcp/workspace-providers.yaml](../config/kcp/workspace-providers.yaml)
and [config/kcp/embed.go](../config/kcp/embed.go).

A third-party provider therefore lives at `root:kedge:providers:{name}` —
sibling to the first-party providers, with identical mechanics. No new
top-level workspace, no new vocabulary.

## Non-goals (v1)

- Hot-reloading provider controllers inside the hub process (providers run
  as separate Deployments).
- Cross-provider dependency resolution / version compatibility matrices.
- A public provider marketplace / registry. Distribution is Helm chart +
  `kubectl apply`.
- Per-provider auth policies (single OIDC at the hub).
- Per-permission-claim consent UI (v1: accept-all on Enable; per-claim
  toggles deferred to v2).

---

## Architecture overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                            kedge-hub                                  │
│                                                                       │
│  /ui/*                       → embedded SPA (Vue portal)              │
│  /ui/providers/{p}/*         → reverse proxy → catalog.spec.ui.url    │
│  /services/providers/{p}/*   → reverse proxy → catalog.spec.backend.url│
│  /clusters/*, /services/agent-proxy, /services/mcp ... (unchanged)    │
│  /api/providers/{p}/heartbeat (POST, provider-SA-authed)              │
│                                                                       │
│  Catalog controller: watches ProviderCatalogEntry                     │
│    - auto-creates root:kedge:providers:{p} sub-workspace              │
│    - creates `provider` ServiceAccount in that workspace              │
│    - writes kedge-provider-kubeconfig Secret to provider's namespace  │
│    - applies inline bootstrap (APIResourceSchema, APIExport)          │
│    - rebuilds proxy routing table; tracks heartbeats                  │
│                                                                       │
│  Binding controller: watches ProviderBinding in tenant workspaces     │
│    - creates APIBinding pointing at provider's APIExport              │
│    - auto-accepts permissionClaims (tenant-scoped only)               │
└──────────────────────────────────────────────────────────────────────┘
        │ kcp                              │ HTTP (in-cluster Service)
        ▼                                  ▼
┌────────────────────────────┐   ┌────────────────────────────────────┐
│ root:kedge:providers:cost  │   │ Provider pod (e.g. cost)           │
│   APIExport cost.faros.sh  │◄──│   - mounts kedge-provider-kubeconfig│
│   APIResourceSchema(s)     │   │   - runs controllers against kcp    │
│   SA: provider             │   │   - serves UI on :3000 (optional)   │
└────────────────────────────┘   │   - serves backend HTTP on :8080    │
        ▲                        │   - heartbeats hub every 30s        │
        │ APIBinding (kcp        └────────────────────────────────────┘
        │ serves natively)
┌────────────────────────────┐
│ root:kedge:tenants:alice   │
│   (user workspace — sees   │
│    cost CRs natively)      │
└────────────────────────────┘
```

Single origin from the browser's perspective: every request goes to
`kedge.example.com`. The hub fans out to providers internally.

**Key clarification on traffic flow:** provider CRs are served by kcp via
the normal `/clusters/...` path on the hub — the same flow as kedge's own
CRDs today. The `/services/providers/{name}` proxy is *only* for the
provider's own custom HTTP backend (REST/GraphQL/WS), not for CR traffic.

---

## CRDs

Two new CRDs, both in the kedge API group, both first-party (added to the
existing `kedge.faros.sh` `APIExport`).

### `ProviderCatalogEntry` (cluster-scoped, in `root:kedge:providers`)

Installed by an administrator via the provider's Helm chart, which targets
the host Kubernetes cluster API. The hub's catalog controller projects it
into kcp.

```yaml
apiVersion: kedge.faros.sh/v1alpha1
kind: ProviderCatalogEntry
metadata:
  name: cost-insights
spec:
  displayName: "Cost Insights"
  description: "Per-edge cost attribution and forecasting."
  vendor: "Acme Cloud"
  version: "1.2.0"
  iconURL: "/ui/providers/cost-insights/icon.svg"  # served via UI proxy

  # Host-cluster namespace where the provider Deployment runs. Hub writes
  # the kedge-provider-kubeconfig Secret here.
  serviceAccountNamespace: "cost-insights"

  # OPTIONAL: micro-frontend. Omit if provider has no UI.
  ui:
    url: "http://cost-insights-ui.cost-insights.svc.cluster.local"
    indexPath: "/"

  # OPTIONAL: custom HTTP backend (NOT for CR traffic — CRs go via kcp).
  # Omit if provider only exposes CRs.
  backend:
    url: "http://cost-insights.cost-insights.svc.cluster.local:8080"
    healthPath: "/healthz"

  # OPTIONAL: opt-in to serving a kcp virtual workspace for non-CRD verbs.
  # Omit for v1 — only needed if provider needs custom resource verbs.
  virtualWorkspace:
    url: "http://cost-insights.cost-insights.svc.cluster.local:6443"

  # REQUIRED: the APIExport the provider owns. Hub creates the workspace,
  # applies the inline schema(s), then creates the APIExport.
  apiExport:
    name: "cost.faros.sh"
    # Inline APIResourceSchema docs the hub applies on first reconcile.
    # Multiple schemas allowed; one APIExport references them all.
    schemas:
      - groupResource: "greetings.cost.faros.sh"
        # The full v1alpha1 APIResourceSchema body as a string. Hub parses
        # and applies. Kept inline so the chart needs no kcp access.
        body: |
          apiVersion: apis.kcp.io/v1alpha1
          kind: APIResourceSchema
          metadata:
            name: v260522-abc.greetings.cost.faros.sh
          spec: { ... }
    # PermissionClaims declared on the APIExport itself (kcp-enforced).
    # Mirrored here as informational for the Enable dialog.
    permissionClaims:
      - resource: configmaps
        verbs: [get, list, watch]
        # Tenant-scoped flag tells the binding controller this is safe to
        # auto-accept. Out-of-tenant claims are refused.
        tenantScoped: true

status:
  # Filled by catalog controller
  workspace: "root:kedge:providers:cost-insights"
  apiExportRef:
    workspace: "root:kedge:providers:cost-insights"
    name: "cost.faros.sh"
  endpoints:
    ui: "http://cost-insights-ui.cost-insights.svc.cluster.local"
    backend: "http://cost-insights.cost-insights.svc.cluster.local:8080"

  # Filled by heartbeat. provider.Ready = true iff heartbeat within TTL
  # AND (no backend declared OR backend healthz is 200).
  lastHeartbeat: "2026-05-22T10:15:00Z"
  reportedVersion: "1.2.0"
  ready: true

  conditions:
    - type: WorkspaceReady
    - type: APIExportReady
    - type: BackendHealthy   # only present if .spec.backend set
    - type: Ready
```

### `ProviderBinding` (namespaced, in a user's tenant workspace)

Created when a user clicks **Enable** in the portal.

```yaml
apiVersion: kedge.faros.sh/v1alpha1
kind: ProviderBinding
metadata:
  name: cost-insights
  namespace: default
spec:
  providerRef:
    name: cost-insights        # → ProviderCatalogEntry name
  # User has implicitly accepted all tenantScoped permissionClaims by
  # creating this resource. If a future chart upgrade adds new claims,
  # the binding becomes NotReady until the user re-confirms via the UI
  # (which bumps spec.acceptedClaimsHash).
  acceptedClaimsHash: "sha256:..."
status:
  apiBindingRef:
    name: cost.faros.sh
  ready: true
  conditions:
    - type: APIBindingReady
    - type: ClaimsAccepted
```

`ProviderBinding` owns its `APIBinding` via `ownerReferences`; deleting
the `ProviderBinding` removes the binding.

---

## Hub changes

### 1. Catalog controller (`pkg/hub/controllers/providercatalog/`)

Watches `ProviderCatalogEntry` in `root:kedge:providers`. On each
reconcile:

1. **Sub-workspace**: ensure `root:kedge:providers:{name}` exists. Use
   the existing kcp tenancy client. Created with type `universal`,
   `bootstrap.kcp.io/create-only: "true"`.
2. **Provider ServiceAccount**: ensure a `ServiceAccount` named
   `provider` exists in that workspace, bound to `cluster-admin` on the
   workspace (admin within its own sandbox, nothing outside).
3. **Kubeconfig Secret**: mint a token for the SA, build an exec-credential
   kubeconfig pointing at the hub URL with cluster
   `root:kedge:providers:{name}`, write it as Secret
   `kedge-provider-kubeconfig` in `spec.serviceAccountNamespace` of the
   *host* cluster. Idempotent. Rotate token every 24h (set
   `kubernetes.io/service-account-token` style annotation).
4. **Schema + APIExport apply**: parse `spec.apiExport.schemas[].body`,
   apply each as an `APIResourceSchema` in the workspace, then
   apply/update the `APIExport` referencing them.
5. **Registry upsert**: push (Name, UIURL, BackendURL, VWURL, Ready) into
   the in-process `Registry` (below).

The controller runs in the hub. It uses the hub's existing controller
manager and the kcp admin client.

### 2. In-memory routing registry (`pkg/hub/providers/`)

```go
type Registry struct {
    mu     sync.RWMutex
    byName map[string]*Provider
}

type Provider struct {
    Name       string
    UIURL      *url.URL  // may be nil
    BackendURL *url.URL  // may be nil
    Ready      bool
    Version    string
}

func (r *Registry) Get(name string) (*Provider, bool)
func (r *Registry) List() []*Provider
func (r *Registry) Upsert(p *Provider)
func (r *Registry) Delete(name string)
```

Pure in-memory; rebuilt on hub restart from the `ProviderCatalogEntry`
list. No external store.

### 3. Heartbeat endpoint

```
POST /api/providers/{name}/heartbeat
Authorization: Bearer <provider-SA-token>
Content-Type: application/json

{ "version": "1.2.0", "buildTime": "...", "status": "healthy" }
```

- Authenticates the bearer token against the SA in
  `root:kedge:providers:{name}`. Rejects any other identity.
- Updates `ProviderCatalogEntry.status.lastHeartbeat` and
  `reportedVersion`.
- TTL: 90 seconds. Catalog controller flips `Ready=false` if no heartbeat
  within TTL.
- Cheap: providers heartbeat every 30s; tiny payload.

### 4. Generic provider proxy

Two route prefixes registered in [pkg/hub/server.go](../pkg/hub/server.go):

```go
// New paths in pkg/api/url/paths.go
const (
    PathPrefixProvidersUI      = "/ui/providers"
    PathPrefixProvidersBackend = "/services/providers"
)

router.PathPrefix(apiurl.PathPrefixProvidersUI + "/").Handler(
    providers.NewUIProxy(registry, logger))
router.PathPrefix(apiurl.PathPrefixProvidersBackend + "/").Handler(
    providers.NewBackendProxy(registry, authMiddleware, logger))
```

Proxy behavior:

- Parse `{name}` from path: `/ui/providers/cost-insights/foo` → name=`cost-insights`, rest=`/foo`.
- Look up in registry; **404** if unknown, **503** if not Ready.
- Backend proxy: requires standard kedge auth middleware; forwards the
  user's `Authorization` header and adds `X-Kedge-User`, `X-Kedge-Tenant`.
- UI proxy: no auth requirement on static assets; injects
  `X-Kedge-Base-Path: /ui/providers/{name}` so the provider can rewrite
  absolute links.
- Standard `httputil.ReverseProxy` with header sanitization.

Note: if `spec.virtualWorkspace.url` is set, the backend proxy also
recognizes a `/services/providers/{name}/vw/*` sub-path and routes it to
the VW URL instead. This is the opt-in advanced path.

### 5. Binding controller (`pkg/hub/controllers/providerbinding/`)

Watches `ProviderBinding` across all tenant workspaces. On reconcile:

1. Look up `ProviderCatalogEntry` by `spec.providerRef.name`. If missing
   or not Ready → set `ClaimsAccepted=False`, requeue.
2. **Claim safety check**: iterate `permissionClaims`. If any is **not**
   `tenantScoped`, refuse and emit a `PermissionClaimRejected` event with
   the offending claim. The user cannot complete enable until either the
   provider chart is fixed or an admin overrides via annotation
   `kedge.faros.sh/accept-untrusted-claims=true` on the
   `ProviderCatalogEntry`.
3. Create/update `APIBinding` in the binding's workspace pointing at
   `status.apiExportRef`, with `permissionClaims` auto-accepted.
4. Set `ownerReference` on the `APIBinding` → `ProviderBinding`.
5. Watch the `APIBinding` and mirror its `Bound` condition to
   `ProviderBinding.status.conditions`.

On delete: cascade via owner references.

### 6. Bootstrap

The kcp bootstrap in [pkg/hub/bootstrap](../pkg/hub/bootstrap) already
creates `root:kedge:providers`. We add:

- `APIResourceSchema` for `ProviderCatalogEntry` and `ProviderBinding`,
  added to the existing `kedge.faros.sh` `APIExport` (first-party).
- New embed paths for these schemas in
  [config/kcp/embed.go](../config/kcp/embed.go).

No new workspaces in bootstrap; provider sub-workspaces are created
lazily on `ProviderCatalogEntry` admission.

---

## Portal changes

### 1. Dynamic route registration

[portal/src/router/index.ts](../portal/src/router/index.ts) currently has a
static array. After auth resolves:

```ts
const { data } = await urql.query(LIST_PROVIDER_BINDINGS).toPromise()
for (const binding of data.providerBindings) {
  router.addRoute({
    path: `/providers/${binding.name}/:rest(.*)*`,
    name: `provider-${binding.name}`,
    component: () => import('@/pages/ProviderFrame.vue'),
    props: route => ({
      providerName: binding.name,
      subPath: route.params.rest ?? '',
    }),
  })
}
```

### 2. `ProviderFrame.vue`

```vue
<template>
  <iframe
    :src="`/ui/providers/${providerName}/${subPath}?v=${version}`"
    class="w-full h-full border-0"
    sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
  />
</template>
```

`version` comes from `ProviderCatalogEntry.status.reportedVersion` — used
as a cache-bust query param when providers upgrade.

### 3. Provider SDK (`@kedge/provider-sdk`)

Tiny TypeScript package (published from `portal/sdk/`) provider authors
import:

```ts
import { useKedge } from '@kedge/provider-sdk'

const { token, user, tenant, onNavigate } = useKedge()
```

`postMessage` handshake with the portal shell. Optional — provider UIs
work without it; they just won't share state with the shell.

### 4. Providers page

New route `/providers` showing the catalog:

- Lists all `ProviderCatalogEntry` (read via GraphQL).
- For each, shows whether the current user has a `ProviderBinding`.
- **Enable** opens a dialog listing `permissionClaims`; confirm →
  creates `ProviderBinding` with `acceptedClaimsHash` set.
- **Disable** deletes the binding.
- Once `status.ready`, the provider's UI route appears in the side nav.

---

## Provider author experience

A provider ships as one Helm chart. **Chart only targets the host
cluster — never kcp directly.** All kcp interactions are owned by the
hub's catalog controller.

```
provider-cost-insights/
├── Chart.yaml
├── values.yaml
└── templates/
    ├── namespace.yaml
    ├── serviceaccount.yaml
    ├── deployment.yaml          # provider pod (controllers + UI + backend)
    ├── service.yaml             # ClusterIP services for UI and backend
    └── catalogentry.yaml        # ProviderCatalogEntry (with inline schemas)
```

`helm install cost-insights ./chart` →

1. Provider Deployment starts. Reads
   `/var/run/secrets/kedge/kedge-provider-kubeconfig` (mounted from the
   Secret the hub will write).
2. `ProviderCatalogEntry` is applied to the host cluster API.
3. Hub catalog controller picks it up:
   a. Creates `root:kedge:providers:cost-insights` workspace.
   b. Creates `provider` SA in that workspace.
   c. Mints token, writes `kedge-provider-kubeconfig` Secret to
      `cost-insights` namespace.
   d. Applies `APIResourceSchema` + `APIExport` to the workspace.
4. Provider pod's controller-runtime manager sees the kubeconfig file
   appear (or retries until it does), starts reconciling its own CRs.
5. Provider starts heartbeating; `status.ready=true`.
6. Users see it in `/providers`, click Enable, get an `APIBinding`.

### Minimal provider backend contract

A provider's backend (if it declares one) MUST:

- Heartbeat: `POST /api/providers/{name}/heartbeat` to the hub every 30s
  (helper in the SDK).
- `GET /healthz` → 200 when ready (used by hub for `BackendHealthy`).

A provider's controller (the kcp-talking part) MUST:

- Wait for `kedge-provider-kubeconfig` Secret to appear before starting.
- Use the kubeconfig's `provider` SA identity. The SA only has rights in
  the provider's own workspace; cross-workspace access is via the
  `APIExport`'s VirtualWorkspace endpoint (kcp serves this natively
  using the APIExport's identity).

A provider's UI MUST:

- Serve static assets such that internal links are relative or rooted at
  `/ui/providers/{name}/`. Use `X-Kedge-Base-Path` from the proxy if a
  build-time base is needed.

---

## Security considerations

- **Auth token forwarding** (backend proxy): the user's bearer token is
  forwarded to the provider backend. Operators MUST trust the providers
  they install. Same trust model as installing any cluster operator.
- **Provider→kcp isolation**: provider SAs are scoped to their own
  workspace. Cross-tenant access only via the APIExport mechanism, which
  kcp gates by `permissionClaims`.
- **Permission claim gate**: the binding controller refuses any claim not
  marked `tenantScoped`. An override exists
  (`kedge.faros.sh/accept-untrusted-claims=true`) but is admin-only
  (host-cluster RBAC on the `ProviderCatalogEntry` resource).
- **iframe sandboxing**: `sandbox` attribute set; no
  `allow-top-navigation`.
- **CSP**: hub portal CSP allows `frame-src 'self'` only.
- **Internal-only services**: providers should be `ClusterIP`. Hub is the
  only public ingress. Network policies recommended.
- **Heartbeat token**: provider SA token is short-lived (24h); rotation
  handled by the catalog controller.

---

## Phased delivery

| Phase | Scope | Verifiable outcome |
|---|---|---|
| 1 | `ProviderCatalogEntry` CRD + catalog controller (workspace + SA + Secret + schema apply) + registry + heartbeat endpoint + backend proxy | An example provider's chart installs, hub provisions everything, provider pod heartbeats, `/services/providers/example/*` reaches the backend |
| 2 | UI proxy + `ProviderFrame.vue` + dynamic routes | Static "hello" provider UI loads inside the portal |
| 3 | `ProviderBinding` CRD + binding controller (with claim-safety check) + Providers page (Enable/Disable + claim dialog) | Users can enable/disable; `APIBinding` created; provider CRs visible in tenant workspace |
| 4 | Provider SDK + example chart in `examples/provider-hello/` | Third party can copy the example and ship a working provider end-to-end |
| 5 | Hardening: CSP, RBAC fuzz, cache-bust, e2e tests, optional `virtualWorkspace` opt-in | Ready to declare stable |

## Deferred items

1. **GraphQL discovery of provider CRs** — REQUIRED by end of phase 3, not
   optional. Once a tenant workspace has an `APIBinding` to a provider's
   `APIExport`, the embedded GraphQL gateway MUST expose the bound CRs in
   that workspace's schema. The gateway already discovers schemas via
   APIExport for first-party kedge resources (see
   [pkg/hub/graphql.go](../pkg/hub/graphql.go) and
   [cmd/graphql/main.go](../cmd/graphql/main.go) — points at
   `root:kedge:providers`). Expected to work transparently, but validate in
   phase 3 with the example provider's `Greeting` CR appearing in GraphQL.
   If discovery is not automatic, the binding controller will need to
   trigger a gateway refresh — file as a follow-up task, do NOT block phase
   1 or 2.
2. **Cross-provider dependencies** — out of scope for v1.
3. **Heartbeat over kcp leases** — possible v2 simplification.
4. **Per-permission-claim UI toggles** — v2.

---

## Phase 1 implementation plan

Phase 1 = the full backend skeleton, no portal changes yet. Verifiable by
installing a stub provider chart and curling
`/services/providers/example/healthz` through the hub.

### Files to create

| Path | Purpose |
|---|---|
| `apis/kedge/v1alpha1/types_providercatalogentry.go` | Go types for `ProviderCatalogEntry` |
| `apis/kedge/v1alpha1/types_providerbinding.go` | Go types for `ProviderBinding` (used in phase 3 but ship the type now) |
| `apis/kedge/v1alpha1/zz_generated.deepcopy.go` | Regenerated via `make codegen` |
| `config/crds/kedge.faros.sh_providercatalogentries.yaml` | CRD manifest (used outside kcp / dev) |
| `config/crds/kedge.faros.sh_providerbindings.yaml` | CRD manifest |
| `config/kcp/apiresourceschema-providercatalogentries.kedge.faros.sh.yaml` | kcp APIResourceSchema |
| `config/kcp/apiresourceschema-providerbindings.kedge.faros.sh.yaml` | kcp APIResourceSchema |
| `pkg/hub/providers/registry.go` | In-memory `Registry` + `Provider` types |
| `pkg/hub/providers/proxy.go` | `NewUIProxy`, `NewBackendProxy` reverse proxies |
| `pkg/hub/providers/proxy_test.go` | Table-driven test for path parsing + lookup |
| `pkg/hub/providers/heartbeat.go` | `POST /api/providers/{name}/heartbeat` HTTP handler |
| `pkg/hub/providers/kubeconfig.go` | Mint exec-credential kubeconfig for the provider SA, idempotent Secret writer |
| `pkg/hub/controllers/providercatalog/controller.go` | Reconciler: workspace + SA + Secret + schema/APIExport apply + registry upsert + TTL eviction |
| `pkg/hub/controllers/providercatalog/controller_test.go` | Reconcile-loop unit tests with fake kcp client |

### Files to edit

| Path | Edit |
|---|---|
| [pkg/api/url/paths.go](../pkg/api/url/paths.go) (or wherever existing path constants live — `apiurl` package) | Add `PathPrefixProvidersUI = "/ui/providers"`, `PathPrefixProvidersBackend = "/services/providers"`, `PathProviderHeartbeat = "/api/providers/{name}/heartbeat"` |
| [pkg/hub/server.go](../pkg/hub/server.go) — currently registers routes at lines 307-359 | Construct `Registry`, mount UI/backend proxies and heartbeat handler, start catalog controller |
| [pkg/hub/scheme.go](../pkg/hub/scheme.go) | Register new types with the scheme |
| [apis/kedge/v1alpha1/groupversion_info.go](../apis/kedge/v1alpha1/groupversion_info.go) | Register new types via `SchemeBuilder.Register(...)` |
| [config/kcp/apiexport-kedge.faros.sh.yaml](../config/kcp/apiexport-kedge.faros.sh.yaml) | Add `providercatalogentries` and `providerbindings` resources |
| [config/kcp/embed.go](../config/kcp/embed.go) | Update `ProvidersFS` embed glob (already matches `apiresourceschema-*.yaml` and `apiexport-*.yaml`, should pick up automatically) |
| [pkg/hub/bootstrap](../pkg/hub/bootstrap) (whichever file applies the APIExport) | No structural change expected; verify the new schemas are applied |
| `Makefile` | Confirm `make codegen` regenerates deepcopy for new types |

### Key code anchors (from current tree)

- Route registration block: [pkg/hub/server.go:307-359](../pkg/hub/server.go#L307-L359)
- Exec-credential kubeconfig pattern (model for provider kubeconfig
  minting): [pkg/server/proxy/proxy.go](../pkg/server/proxy/proxy.go)
- Existing APIExport YAML (template for the new one's permissionClaims):
  [config/kcp/apiexport-kedge.faros.sh.yaml](../config/kcp/apiexport-kedge.faros.sh.yaml)
- Bootstrap entry point: `pkg/hub/bootstrap` + invocation around
  [pkg/hub/server.go:280-301](../pkg/hub/server.go#L280-L301)
- kcp embedded FS: [config/kcp/embed.go](../config/kcp/embed.go)
- Static path constants live in `pkg/api/url/` (referenced as `apiurl` in
  `pkg/hub/server.go`)
- Workspace YAML pattern:
  [config/kcp/workspace-providers.yaml](../config/kcp/workspace-providers.yaml)

### Phase 1 verification recipe

1. `make codegen && make build` — clean build.
2. Start the hub against an embedded kcp:
   `./bin/kedge-hub --embedded-kcp --static-auth-tokens=test:user-default`.
3. Apply a stub `ProviderCatalogEntry`:
   ```yaml
   apiVersion: kedge.faros.sh/v1alpha1
   kind: ProviderCatalogEntry
   metadata: { name: hello }
   spec:
     displayName: Hello
     vendor: kedge
     version: 0.0.1
     serviceAccountNamespace: default
     backend:
       url: http://localhost:8081  # any local HTTP responder
       healthPath: /healthz
     apiExport:
       name: hello.example.com
       schemas:
         - groupResource: greetings.hello.example.com
           body: |
             apiVersion: apis.kcp.io/v1alpha1
             kind: APIResourceSchema
             metadata: { name: v260522-stub.greetings.hello.example.com }
             spec: { ... minimal valid schema ... }
   ```
4. Observe in hub logs:
   - workspace `root:kedge:providers:hello` created
   - SA `provider` created
   - Secret `kedge-provider-kubeconfig` written to `default` namespace
   - APIResourceSchema + APIExport applied
   - registry shows `hello` once stub backend returns 200 on `/healthz`
5. `curl -H "Authorization: Bearer test" \
   http://localhost:9443/services/providers/hello/healthz` → reaches the
   stub backend (matches the body it serves).
6. POST a heartbeat with the SA token from the Secret →
   `status.lastHeartbeat` updates.
7. Delete the `ProviderCatalogEntry` → registry entry removed, Secret
   cleaned up. (Workspace deletion is a v2 concern — leave it for now,
   note in code as TODO.)

### What phase 1 deliberately does NOT do

- No portal changes.
- No `ProviderBinding` reconciliation (the CRD ships but the controller
  arrives in phase 3).
- No GraphQL validation.
- No Helm example chart yet (phase 4).
- No `virtualWorkspace` opt-in path (phase 5).

---

## Example: a minimal provider

Tracked under `examples/provider-hello/` once phase 1 lands. Structure: one
Go binary serving `/healthz` + `/api/hello` + a static `index.html`; one
controller using `kedge-provider-kubeconfig` to manage a `Greeting` CR;
Helm chart from §"Provider author experience".
