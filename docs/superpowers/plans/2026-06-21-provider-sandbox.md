# Provider Sandbox Implementation Plan

> [!WARNING]
> **Obsolete.** This plan described the first standalone `provider-sandbox`
> direction. The branch now folds the backend/data-plane API into App Studio:
> App Studio owns project sync/restart/log/status/preview routes, while the
> infrastructure provider owns the KRO-composed `SandboxRunner` resource. See
> `docs/app-studio-sandbox-runtime.md` for the current architecture.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `provider-sandbox`, a dedicated development-runtime provider that gives App Studio a fast live preview path separate from artifact-based test/production deploys.

**Architecture:** App Studio remains the coordinator of Projects and LLM edits, but does not manage pods, PVCs, process supervisors, file sync internals, or preview networking. `provider-sandbox` owns `DevEnvironment` resources and exposes provider-owned operations for file sync, restart, logs, status, and preview URL. The first POC uses a Kubernetes runtime pod plus PVC and a small dev-runner HTTP service inside the pod; future versions can swap the backing runtime without changing App Studio's capability contract.

**Tech Stack:** Go, kcp APIExport/APIBinding, controller-runtime, Kubernetes core APIs, Vite/Vue provider portal patterns, hub provider proxy, App Studio provider bindings.

---

## Product Contract

The current production-style loop stays intact:

```text
LLM change -> GitHub commit -> GitHub Actions -> GHCR package -> Code Package -> App Studio artifact deploy -> Infrastructure/KRO runtime
```

`provider-sandbox` adds a fast development loop:

```text
LLM change -> App Studio workspace update -> provider-sandbox file sync -> dev process reload/restart -> preview refresh
```

The preview tab should prefer `development` when present. The test/production environments continue to use package/image deploys.

## Boundary Rules

App Studio may know:

```text
Project has a development environment
binding provider is sandbox
binding exposes sync/restart/logs/preview capabilities
changed files should be sent to the sandbox binding
```

App Studio must not know:

```text
pod names
PVC names
kubectl cp mechanics
KRO internals
process supervisor implementation
runtime namespace names
```

`provider-sandbox` owns those details.

## API Shape

Initial tenant-facing resource:

```yaml
apiVersion: sandbox.kedge.faros.sh/v1alpha1
kind: DevEnvironment
metadata:
  name: todo-dev
spec:
  projectRef: todo
  runtime:
    image: ghcr.io/faroshq/kedge-sandbox-runner:dev
    workingDir: /workspace
    startCommand: npm run dev -- --host 0.0.0.0
    port: 3000
  sync:
    mode: patch
status:
  phase: Running
  previewURL: /services/providers/sandbox/api/dev-environments/todo-dev/preview/
  lastSyncTime: "2026-06-21T18:00:00Z"
  observedGeneration: 1
  conditions:
    - type: Ready
      status: "True"
      reason: RunnerReady
```

Provider backend operations:

```text
POST /api/dev-environments/{name}/sync
POST /api/dev-environments/{name}/restart
GET  /api/dev-environments/{name}/logs
GET  /api/dev-environments/{name}/status
GET  /api/dev-environments/{name}/preview/*
```

Sync request body:

```json
{
  "files": [
    {"path":"public/style.css","content":"..."}
  ],
  "deletePaths": [],
  "restart": "auto"
}
```

Sync response body:

```json
{
  "phase": "Synced",
  "changed": ["public/style.css"],
  "restarted": false,
  "previewURL": "/services/providers/sandbox/api/dev-environments/todo-dev/preview/"
}
```

## File Map

Create provider module:

```text
providers/sandbox/go.mod
providers/sandbox/main.go
providers/sandbox/heartbeat.go
providers/sandbox/assets.go
providers/sandbox/manifest.yaml
providers/sandbox/Dockerfile
providers/sandbox/apis/v1alpha1/doc.go
providers/sandbox/apis/v1alpha1/groupversion_info.go
providers/sandbox/apis/v1alpha1/types_devenvironment.go
providers/sandbox/apis/v1alpha1/zz_generated.deepcopy.go
providers/sandbox/scheme/scheme.go
providers/sandbox/init_cmd.go
providers/sandbox/controller_manager.go
providers/sandbox/controller/devenvironment/controller.go
providers/sandbox/controller/devenvironment/controller_test.go
providers/sandbox/tenant/client.go
providers/sandbox/tenant/credentials.go
providers/sandbox/server/server.go
providers/sandbox/server/sync.go
providers/sandbox/server/preview.go
providers/sandbox/server/logs.go
providers/sandbox/runner/main.go
providers/sandbox/portal/package.json
providers/sandbox/portal/src/main.ts
providers/sandbox/portal/src/App.vue
providers/sandbox/portal/vite.config.ts
providers/sandbox/install/endpointslice.go
providers/sandbox/install/apiexport.go
providers/sandbox/config/crds/sandbox.kedge.faros.sh_devenvironments.yaml
providers/sandbox/config/kcp/apiexport-sandbox.kedge.faros.sh.yaml
providers/sandbox/deploy/chart/...
```

Modify existing repo wiring:

```text
go.work
Makefile
Tiltfile.cluster
portal/src/assets/main.css
providers/app-studio/apis/ai/v1alpha1/types_project.go
providers/app-studio/api/deployment_defaults.go
providers/app-studio/api/projects.go
providers/app-studio/api/preview.go
providers/app-studio/api/assistant_workflow.go
providers/app-studio/deployment/reconciler.go
providers/app-studio/portal/src/App.vue
providers/app-studio/portal/src/types.ts
providers/app-studio/portal/src/api.ts
```

---

## Task 1: Scaffold `provider-sandbox` Module

**Files:**
- Create: `providers/sandbox/go.mod`
- Create: `providers/sandbox/main.go`
- Create: `providers/sandbox/heartbeat.go`
- Create: `providers/sandbox/assets.go`
- Create: `providers/sandbox/manifest.yaml`
- Create: `providers/sandbox/portal/package.json`
- Create: `providers/sandbox/portal/src/main.ts`
- Create: `providers/sandbox/portal/src/App.vue`
- Create: `providers/sandbox/portal/vite.config.ts`
- Modify: `go.work`
- Modify: `Makefile`
- Modify: `Tiltfile.cluster`
- Modify: `portal/src/assets/main.css`

- [ ] **Step 1: Copy the provider skeleton pattern**

Use `providers/quickstart`, `providers/code`, and `providers/app-studio` as references. Do not copy quickstart API names. The provider name is exactly `sandbox` and the Go module is `github.com/faroshq/provider-sandbox`.

- [ ] **Step 2: Create `providers/sandbox/go.mod`**

```go
module github.com/faroshq/provider-sandbox

go 1.26

require (
    github.com/faroshq/faros-kedge v0.0.0
    github.com/gorilla/mux v1.8.1
    k8s.io/api v0.35.1
    k8s.io/apimachinery v0.35.1
    k8s.io/client-go v0.35.1
    sigs.k8s.io/controller-runtime v0.23.1
)
```

- [ ] **Step 3: Add `providers/sandbox` to `go.work`**

Add the module path to the existing `use (...)` block:

```text
./providers/sandbox
```

- [ ] **Step 4: Create `providers/sandbox/assets.go`**

```go
package main

import "embed"

//go:embed all:portal/dist
var portalAssets embed.FS
```

- [ ] **Step 5: Create minimal portal files**

`providers/sandbox/portal/package.json`:

```json
{"name":"kedge-provider-sandbox-portal","version":"0.1.0","private":true,"type":"module","scripts":{"build":"vite build && cp index.html dist/ && touch dist/.gitkeep"},"dependencies":{"@vitejs/plugin-vue":"latest","vite":"latest","vue":"latest","typescript":"latest"},"devDependencies":{}}
```

`providers/sandbox/portal/src/main.ts`:

```ts
import { createApp, defineCustomElement, h } from 'vue'
import App from './App.vue'

const TAG = 'kedge-provider-sandbox'

const Element = defineCustomElement({
  render: () => h(App),
})

if (!customElements.get(TAG)) {
  customElements.define(TAG, Element)
}

const mount = document.querySelector(TAG)
if (mount) createApp(App).mount(mount)
```

`providers/sandbox/portal/src/App.vue`:

```vue
<template>
  <section class="rounded-2xl border border-border-subtle bg-surface-raised/80 p-6 text-text-primary">
    <p class="text-[10px] font-semibold uppercase tracking-[0.15em] text-text-muted">Sandbox provider</p>
    <h2 class="mt-2 text-[18px] font-bold">Development runtimes</h2>
    <p class="mt-2 text-[13px] leading-6 text-text-secondary">
      Provider Sandbox owns live development environments, file sync, logs, restarts, and preview URLs.
    </p>
  </section>
</template>
```

`providers/sandbox/portal/vite.config.ts`:

```ts
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [vue({ customElement: true })],
  build: {
    lib: {
      entry: 'src/main.ts',
      formats: ['es'],
      fileName: () => 'main.js',
    },
    rollupOptions: {
      output: { inlineDynamicImports: true },
    },
  },
})
```

- [ ] **Step 6: Add Makefile targets**

Add targets following the app-studio/code provider style:

```makefile
build-sandbox-provider-portal:
	cd providers/sandbox/portal && npm install --no-audit --no-fund && npm run build

build-sandbox-provider: build-sandbox-provider-portal
	cd providers/sandbox && go build $(GOFLAGS) -o $(CURDIR)/$(BINDIR)/sandbox-provider .

run-provider-sandbox: build-sandbox-provider
	PORT=$${PORT:-8086} \
	KEDGE_PROVIDER_NAME=sandbox \
	KEDGE_HUB_URL=$${KEDGE_HUB_URL:-https://localhost:9443} \
	KEDGE_HUB_TOKEN=$${KEDGE_HUB_TOKEN:-dev-token} \
	KEDGE_HUB_INSECURE=true \
	KEDGE_PROVIDER_KUBECONFIG=$${KEDGE_PROVIDER_KUBECONFIG:-$$( [ -f ".kcp/sandbox-runtime.kubeconfig" ] && echo ".kcp/sandbox-runtime.kubeconfig" )} \
	SANDBOX_RUNTIME_KUBECONFIG=$${SANDBOX_RUNTIME_KUBECONFIG:-.kedge-cluster.kubeconfig} \
		$(BINDIR)/sandbox-provider serve

init-provider-sandbox: build-sandbox-provider
	KEDGE_PROVIDER_KUBECONFIG=$${KEDGE_PROVIDER_KUBECONFIG:-.kcp/sandbox-runtime.kubeconfig} \
		$(BINDIR)/sandbox-provider init
```

- [ ] **Step 7: Add Tilt resource**

In `Tiltfile.cluster`, add a `sandbox` resource after `app-studio` and `infrastructure` are established. Use port `8086`. Watch `providers/sandbox` and resource-depend on `kedge-hub` plus provider registration/init.

- [ ] **Step 8: Update portal Tailwind source scanning**

Add this line in `portal/src/assets/main.css` near other provider `@source` directives:

```css
@source "../../../providers/sandbox/portal/src/**/*.{vue,ts}";
```

- [ ] **Step 9: Build provider portal**

Run:

```bash
make build-sandbox-provider-portal
```

Expected: Vite builds `providers/sandbox/portal/dist/main.js`.

- [ ] **Step 10: Commit scaffold**

```bash
git add go.work Makefile Tiltfile.cluster portal/src/assets/main.css providers/sandbox
git commit -m "feat(sandbox): scaffold provider"
```

---

## Task 2: Define `DevEnvironment` API and Provider Export

**Files:**
- Create: `providers/sandbox/apis/v1alpha1/doc.go`
- Create: `providers/sandbox/apis/v1alpha1/groupversion_info.go`
- Create: `providers/sandbox/apis/v1alpha1/types_devenvironment.go`
- Create: `providers/sandbox/scheme/scheme.go`
- Create: `providers/sandbox/install/apiexport.go`
- Create: `providers/sandbox/install/endpointslice.go`
- Create: `providers/sandbox/init_cmd.go`
- Modify generated: `providers/sandbox/apis/v1alpha1/zz_generated.deepcopy.go`
- Modify generated/config: `providers/sandbox/config/...`

- [ ] **Step 1: Create API package markers**

`providers/sandbox/apis/v1alpha1/doc.go`:

```go
// Package v1alpha1 contains Provider Sandbox tenant APIs.
//
// +kubebuilder:object:generate=true
// +groupName=sandbox.kedge.faros.sh
package v1alpha1
```

`providers/sandbox/apis/v1alpha1/groupversion_info.go`:

```go
package v1alpha1

import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/runtime/schema"
)

const GroupName = "sandbox.kedge.faros.sh"

var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1alpha1"}

var (
    SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
    AddToScheme   = SchemeBuilder.AddToScheme
)

func Resource(resource string) schema.GroupResource {
    return SchemeGroupVersion.WithResource(resource).GroupResource()
}

func addKnownTypes(scheme *runtime.Scheme) error {
    scheme.AddKnownTypes(SchemeGroupVersion, &DevEnvironment{}, &DevEnvironmentList{})
    metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
    return nil
}
```

- [ ] **Step 2: Create `DevEnvironment` types**

`providers/sandbox/apis/v1alpha1/types_devenvironment.go`:

```go
package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
    ConditionReady     = "Ready"
    ConditionSynced    = "Synced"
    ConditionProcessUp = "ProcessUp"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,categories=kedge
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Preview",type=string,JSONPath=`.status.previewURL`
type DevEnvironment struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   DevEnvironmentSpec   `json:"spec,omitempty"`
    Status DevEnvironmentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type DevEnvironmentList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items           []DevEnvironment `json:"items"`
}

type DevEnvironmentSpec struct {
    ProjectRef string                `json:"projectRef,omitempty"`
    Runtime    DevEnvironmentRuntime `json:"runtime,omitempty"`
    Sync       DevEnvironmentSync    `json:"sync,omitempty"`
}

type DevEnvironmentRuntime struct {
    Image        string `json:"image,omitempty"`
    WorkingDir   string `json:"workingDir,omitempty"`
    StartCommand string `json:"startCommand,omitempty"`
    Port         int32  `json:"port,omitempty"`
}

type DevEnvironmentSync struct {
    Mode string `json:"mode,omitempty"`
}

type DevEnvironmentStatus struct {
    Phase              string             `json:"phase,omitempty"`
    PreviewURL         string             `json:"previewURL,omitempty"`
    LastSyncTime       *metav1.Time       `json:"lastSyncTime,omitempty"`
    ObservedGeneration int64              `json:"observedGeneration,omitempty"`
    Conditions         []metav1.Condition `json:"conditions,omitempty"`
}
```

- [ ] **Step 3: Add scheme package**

`providers/sandbox/scheme/scheme.go`:

```go
package scheme

import (
    "k8s.io/apimachinery/pkg/runtime"
    clientgoscheme "k8s.io/client-go/kubernetes/scheme"

    sandboxv1alpha1 "github.com/faroshq/provider-sandbox/apis/v1alpha1"
)

func New() *runtime.Scheme {
    s := runtime.NewScheme()
    _ = clientgoscheme.AddToScheme(s)
    _ = sandboxv1alpha1.AddToScheme(s)
    return s
}
```

- [ ] **Step 4: Add manifest**

`providers/sandbox/manifest.yaml`:

```yaml
apiVersion: providers.kedge.faros.sh/v1alpha1
kind: CatalogEntry
metadata:
  name: sandbox
spec:
  displayName: Sandbox
  description: Live development runtimes for App Studio projects.
  icon: terminal-square
  category: Developer Tools
  ui:
    path: /ui/providers/sandbox
    customElement: kedge-provider-sandbox
  backend:
    path: /services/providers/sandbox
    healthPath: /healthz
  apiExport:
    name: sandbox.kedge.faros.sh
    permissionClaims:
      - resource: secrets
        verbs: [get, list, watch, create, update, delete]
    schemas: []
```

- [ ] **Step 5: Implement init command**

Follow `providers/app-studio/init_cmd.go` and `providers/code/init_cmd.go`. It must:

```text
load KEDGE_PROVIDER_KUBECONFIG
create provider workspace APIExport sandbox.kedge.faros.sh
apply DevEnvironment APIResourceSchema
ensure APIExportEndpointSlice
write .kcp/sandbox-runtime.kubeconfig for local dev
```

- [ ] **Step 6: Generate code**

Add `codegen-sandbox-provider` target to `Makefile` using the app-studio/code target as a template.

Run:

```bash
make codegen-sandbox-provider
```

Expected:

```text
providers/sandbox/apis/v1alpha1/zz_generated.deepcopy.go updated
providers/sandbox/config/crds/sandbox.kedge.faros.sh_devenvironments.yaml created
providers/sandbox/config/kcp/apiexport-sandbox.kedge.faros.sh.yaml created
```

- [ ] **Step 7: Commit API**

```bash
git add providers/sandbox Makefile
git commit -m "feat(sandbox): add dev environment API"
```

---

## Task 3: Implement Runtime Controller

**Files:**
- Create: `providers/sandbox/controller_manager.go`
- Create: `providers/sandbox/controller/devenvironment/controller.go`
- Create: `providers/sandbox/controller/devenvironment/controller_test.go`
- Modify: `providers/sandbox/main.go`

- [ ] **Step 1: Write controller test for desired runtime objects**

Test behavior:

```text
Given DevEnvironment{name: todo-dev, port: 3000, startCommand: npm run dev}
When reconciled
Then runtime namespace contains PVC, Deployment, Service
And status.previewURL is /services/providers/sandbox/api/dev-environments/todo-dev/preview/
```

Use fake client for tenant object status and Kubernetes fake client for runtime objects.

- [ ] **Step 2: Implement controller object naming**

Use deterministic names:

```go
func runtimeNamespace(clusterName string) string {
    return "sandbox-" + clusterName
}

func pvcName(name string) string { return name + "-workspace" }
func deploymentName(name string) string { return name + "-runner" }
func serviceName(name string) string { return name + "-preview" }
```

- [ ] **Step 3: Implement Deployment shape**

The first POC runner pod has one container:

```yaml
containers:
  - name: runner
    image: ghcr.io/faroshq/kedge-sandbox-runner:dev
    env:
      - name: SANDBOX_WORKDIR
        value: /workspace
      - name: SANDBOX_START_COMMAND
        value: npm run dev -- --host 0.0.0.0
      - name: SANDBOX_PORT
        value: "3000"
    ports:
      - name: preview
        containerPort: 3000
      - name: control
        containerPort: 7070
    volumeMounts:
      - name: workspace
        mountPath: /workspace
volumes:
  - name: workspace
    persistentVolumeClaim:
      claimName: todo-dev-workspace
```

- [ ] **Step 4: Implement Service shape**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: todo-dev-preview
spec:
  selector:
    sandbox.kedge.faros.sh/dev-environment: todo-dev
  ports:
    - name: preview
      port: 3000
      targetPort: preview
    - name: control
      port: 7070
      targetPort: control
```

- [ ] **Step 5: Status update**

Status rules:

```text
Ready=True when Deployment available replicas >= 1
PreviewURL always set once Service is applied
Phase=Provisioning until deployment is available
Phase=Running when Ready=True
Phase=Failed only on reconcile errors that prevent object apply
```

- [ ] **Step 6: Wire controller manager**

`providers/sandbox/controller_manager.go` should mirror `providers/app-studio/controller_manager.go`, but also load `SANDBOX_RUNTIME_KUBECONFIG` for runtime Kubernetes writes.

- [ ] **Step 7: Commit controller**

```bash
git add providers/sandbox/controller_manager.go providers/sandbox/controller providers/sandbox/main.go
git commit -m "feat(sandbox): reconcile development runtime objects"
```

---

## Task 4: Build Sandbox Runner

**Files:**
- Create: `providers/sandbox/runner/main.go`
- Create: `providers/sandbox/runner/go.mod` or keep as package under provider module
- Modify: `providers/sandbox/Dockerfile`

- [ ] **Step 1: Implement runner endpoints**

Runner listens on `:7070` for control operations and starts the dev command in `/workspace`.

Endpoints:

```text
GET  /healthz
POST /sync
POST /restart
GET  /logs
```

- [ ] **Step 2: Implement sync request handling**

`POST /sync` accepts:

```go
type syncRequest struct {
    Files []struct {
        Path    string `json:"path"`
        Content string `json:"content"`
    } `json:"files"`
    DeletePaths []string `json:"deletePaths"`
    Restart string `json:"restart"`
}
```

Rules:

```text
reject absolute paths
reject paths containing .. after path.Clean
create parent directories with 0755
write files with 0644
remove deletePaths only under /workspace
restart if restart == "always"
```

- [ ] **Step 3: Implement process supervisor**

Start command from env:

```go
cmd := exec.CommandContext(ctx, "/bin/sh", "-lc", os.Getenv("SANDBOX_START_COMMAND"))
cmd.Dir = os.Getenv("SANDBOX_WORKDIR")
```

Keep a ring buffer of the last 500 log lines. Restart kills the current process group and starts a new one.

- [ ] **Step 4: Dockerfile**

First POC image can be Node-oriented:

```dockerfile
FROM node:22-bookworm
RUN apt-get update && apt-get install -y ca-certificates git && rm -rf /var/lib/apt/lists/*
COPY sandbox-runner /usr/local/bin/sandbox-runner
WORKDIR /workspace
EXPOSE 3000 7070
ENTRYPOINT ["/usr/local/bin/sandbox-runner"]
```

- [ ] **Step 5: Commit runner**

```bash
git add providers/sandbox/runner providers/sandbox/Dockerfile
git commit -m "feat(sandbox): add development runner"
```

---

## Task 5: Implement Provider Backend Operations

**Files:**
- Create: `providers/sandbox/server/server.go`
- Create: `providers/sandbox/server/sync.go`
- Create: `providers/sandbox/server/preview.go`
- Create: `providers/sandbox/server/logs.go`
- Create: `providers/sandbox/tenant/client.go`
- Create: `providers/sandbox/tenant/credentials.go`
- Modify: `providers/sandbox/main.go`

- [ ] **Step 1: Implement tenant client factory**

Copy the canonical pattern from `providers/code/tenant/client.go` and `providers/app-studio/tenant/client.go`.

- [ ] **Step 2: Implement route registration**

`providers/sandbox/server/server.go`:

```go
func New(runtimeConfig *rest.Config, tenantFactory *tenant.ClientFactory) http.Handler {
    r := mux.NewRouter()
    r.HandleFunc("/healthz", healthz).Methods(http.MethodGet)
    r.HandleFunc("/api/dev-environments/{name}/sync", syncDevEnvironment(runtimeConfig, tenantFactory)).Methods(http.MethodPost)
    r.HandleFunc("/api/dev-environments/{name}/restart", restartDevEnvironment(runtimeConfig, tenantFactory)).Methods(http.MethodPost)
    r.HandleFunc("/api/dev-environments/{name}/logs", logsDevEnvironment(runtimeConfig, tenantFactory)).Methods(http.MethodGet)
    r.PathPrefix("/api/dev-environments/{name}/preview/").Handler(previewDevEnvironment(runtimeConfig, tenantFactory)).Methods(http.MethodGet, http.MethodHead)
    return r
}
```

- [ ] **Step 3: Sync endpoint behavior**

Provider server receives user request through hub. It must:

```text
resolve tenant from X-Kedge-Tenant
read DevEnvironment from tenant workspace
locate runtime Service control port for that environment
POST sync payload to runner control endpoint through Kubernetes service proxy
update DevEnvironment status.lastSyncTime
return runner response
```

- [ ] **Step 4: Preview endpoint behavior**

Proxy:

```text
/api/dev-environments/{name}/preview/*
```

To Kubernetes service proxy:

```text
/api/v1/namespaces/{runtimeNamespace}/services/{name}-preview:preview/proxy/*
```

Use `rest.TransportFor(runtimeConfig)` like the infrastructure preview proxy.

- [ ] **Step 5: Logs endpoint behavior**

Proxy runner logs:

```text
/api/v1/namespaces/{runtimeNamespace}/services/{name}-preview:control/proxy/logs
```

- [ ] **Step 6: Commit backend**

```bash
git add providers/sandbox/server providers/sandbox/tenant providers/sandbox/main.go
git commit -m "feat(sandbox): expose sync preview and logs APIs"
```

---

## Task 6: Integrate App Studio Development Environment

**Files:**
- Modify: `providers/app-studio/apis/ai/v1alpha1/types_project.go`
- Modify: `providers/app-studio/api/deployment_defaults.go`
- Modify: `providers/app-studio/api/projects.go`
- Modify: `providers/app-studio/deployment/reconciler.go`
- Modify: `providers/app-studio/api/assistant_workflow.go`
- Modify: `providers/app-studio/portal/src/types.ts`
- Modify: `providers/app-studio/portal/src/api.ts`
- Modify: `providers/app-studio/portal/src/App.vue`

- [ ] **Step 1: Add environment mode**

Extend `ProjectEnvironmentSpec` with:

```go
type ProjectEnvironmentMode string

const (
    ProjectEnvironmentModeArtifact ProjectEnvironmentMode = "artifact"
    ProjectEnvironmentModeLive     ProjectEnvironmentMode = "live"
)

Mode ProjectEnvironmentMode `json:"mode,omitempty"`
```

Default empty mode to `artifact` for existing test/production behavior.

- [ ] **Step 2: Add default development environment**

Add helper:

```go
func defaultProjectDevelopmentEnvironment(projectName string) aiv1alpha1.ProjectEnvironmentSpec {
    instanceName := projectName
    if instanceName == "" { instanceName = "app" }
    return aiv1alpha1.ProjectEnvironmentSpec{
        Name: "development",
        Mode: aiv1alpha1.ProjectEnvironmentModeLive,
        AutoDeploy: false,
        Promotion: aiv1alpha1.ProjectPromotionManual,
        Bindings: []aiv1alpha1.ProjectProviderBindingSpec{{
            Name: "dev",
            Provider: "sandbox",
            Kind: aiv1alpha1.ProjectBindingKindProviderResource,
            ResourceRef: &aiv1alpha1.ProjectProviderResourceReference{
                APIVersion: "sandbox.kedge.faros.sh/v1alpha1",
                Kind: "DevEnvironment",
                Resource: "devenvironments",
            },
            Values: projectDeploymentJSONValues(map[string]any{
                "name": instanceName + "-dev",
                "projectRef": projectName,
                "runtime": map[string]any{
                    "image": "ghcr.io/faroshq/kedge-sandbox-runner:dev",
                    "workingDir": "/workspace",
                    "startCommand": "npm run dev -- --host 0.0.0.0",
                    "port": int64(3000),
                },
                "sync": map[string]any{"mode": "patch"},
            }),
        }},
    }
}
```

- [ ] **Step 3: Reconcile live binding without artifact injection**

In App Studio deployment reconciler:

```text
if env.Mode == live:
  reconcile bindings even when no Code Package artifact exists
  do not require artifact.valuePath
  write status from provider resource status.previewURL
```

- [ ] **Step 4: Prefer development preview URL**

Update `projectAssistantRuntimePreviewURL`:

```text
prefer environment name development with binding dev output url/previewURL
fallback to test/web
fallback to legacy runtime url
```

- [ ] **Step 5: Add provider sync client**

Add App Studio backend method:

```text
POST /api/projects/{project}/sync-development
```

It should:

```text
find development/dev binding
build changed file list from LLM write/apply_patch result or current request
POST to /services/providers/sandbox/api/dev-environments/{name}/sync through hub base URL
return sync response
```

For first POC, sync all project workspace files after each successful LLM file mutation. Optimize to changed files later.

- [ ] **Step 6: Call sync after LLM edits**

After successful `write_file`, `apply_patch`, or generated file update in App Studio:

```text
persist workspace changes
commit to GitHub as today
start sandbox sync asynchronously
show user: "Preview syncing..."
```

Do not block GitHub commit or CI path on sandbox sync. If sync fails, show a warning and keep the production flow running.

- [ ] **Step 7: Update portal UX**

Preview tab should display:

```text
Development preview: Live
Test deploy: Building image / Running / Failed
```

Refresh button refreshes development preview by default when available.

- [ ] **Step 8: Commit App Studio integration**

```bash
git add providers/app-studio
git commit -m "feat(app-studio): add live development environment integration"
```

---

## Task 7: Local E2E POC

**Files:**
- Modify: `Tiltfile.cluster`
- Modify: `Makefile`
- Optional create: `docs/provider-sandbox-poc.md`

- [ ] **Step 1: Start Tilt cluster**

Run:

```bash
tilt up
```

Expected:

```text
sandbox Ready=True
app-studio Ready=True
code Ready=True
infrastructure Ready=True
```

- [ ] **Step 2: Enable providers in Console**

Enable:

```text
code
app-studio
sandbox
infrastructure
```

Expected: all APIBindings bound in tenant workspace.

- [ ] **Step 3: Create App Studio project**

Use existing Console flow. The Project should now have:

```text
development/dev provider=sandbox mode=live
test/web provider=infrastructure mode=artifact
```

- [ ] **Step 4: Wait for DevEnvironment**

Run:

```bash
kubectl --kubeconfig=<tenant-kubeconfig> get devenvironments.sandbox.kedge.faros.sh
```

Expected:

```text
NAME              PHASE     PREVIEW
todo-dev          Running   /services/providers/sandbox/api/dev-environments/todo-dev/preview/
```

- [ ] **Step 5: Make a small LLM change**

Prompt:

```text
Change the Add Task button to red.
```

Expected:

```text
App Studio writes file changes
App Studio syncs changed files to provider-sandbox
preview updates without waiting for GitHub Actions package deploy
GitHub commit/CI/package still happens in background
```

- [ ] **Step 6: Verify latency**

Record rough timings:

```text
LLM file write complete -> preview updated: target under 5 seconds
GitHub package deploy path: can remain minutes-scale
```

- [ ] **Step 7: Commit docs**

```bash
git add docs/provider-sandbox-poc.md
git commit -m "docs: document provider sandbox POC flow"
```

---

## Task 8: Guardrails and Follow-Up Decisions

**Files:**
- Create: `docs/provider-sandbox-poc.md`

- [ ] **Step 1: Document security caveat**

Add this exact section:

```markdown
## Security caveat

The POC serves project previews through the hub origin for convenience. Before production, previews must move to an isolated origin or subdomain so project JavaScript is not same-origin with Console. The preview iframe may use `allow-same-origin` only when the preview origin is isolated from the Console origin.
```

- [ ] **Step 2: Document unsupported cases**

```markdown
## POC limitations

- Node/Railpack-style projects only.
- One development environment per Project.
- Sync is whole-file patch sync, not rsync.
- No terminal in the first slice.
- No multi-user collaborative editing.
- No resource quota enforcement beyond Kubernetes namespace defaults.
- No production-grade sandbox escape hardening yet.
```

- [ ] **Step 3: Document next capabilities**

```markdown
## Next capabilities

- Isolated preview domains.
- Framework detection for start commands and ports.
- Incremental changed-file sync.
- Terminal and log streaming.
- Per-tenant quotas.
- Runtime idle/suspend/resume.
- Provider capability discovery so App Studio can target sandbox, VM, or other dev runtime providers interchangeably.
```

- [ ] **Step 4: Commit docs**

```bash
git add docs/provider-sandbox-poc.md
git commit -m "docs(sandbox): capture POC guardrails"
```

---

## Acceptance Criteria

- `provider-sandbox` appears in the provider catalog.
- Tenant can enable `sandbox` provider.
- Tenant workspace can create `DevEnvironment` resources.
- Controller creates runtime PVC, Deployment, and Service.
- Provider backend exposes sync, restart, logs, status, and preview endpoints.
- App Studio creates a `development` live environment for projects.
- App Studio syncs LLM-authored file changes to the sandbox runtime.
- Preview tab prefers development preview when available.
- A CSS-only change appears in preview without waiting for GitHub Actions, GHCR, Code package crawl, or KRO artifact deploy.
- Existing test/production artifact deploy path remains intact.

## Non-Goals for First POC

- General VM runtime support.
- Firecracker isolation.
- Browser IDE terminal.
- Production security hardening.
- Multi-language framework matrix.
- Replacing Infrastructure/KRO artifact deploys.
- Making App Studio own Kubernetes runtime internals.

## Self-Review Notes

- The plan keeps App Studio at the capability level and puts runtime mechanics in `provider-sandbox`.
- The plan preserves the artifact-based deployment path for test/production.
- The first POC intentionally optimizes Node/Railpack projects because that matches the current App Studio sample flow.
- The biggest open design risk is preview origin isolation. The plan calls this out as a required production follow-up rather than burying it.
