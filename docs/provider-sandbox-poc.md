# Provider Sandbox POC

`provider-sandbox` is a dedicated live development runtime provider for App
Studio projects. App Studio owns the Project-level capability contract and file
workspace; the sandbox provider owns `DevEnvironment` resources, runtime pods,
PVCs, process supervision, sync, logs, restarts, and preview proxying.

The first runtime implementation is intentionally small:

- one tenant-facing `DevEnvironment` APIExport
- one Kubernetes namespace per logical tenant cluster
- one PVC, Deployment, and Service per development environment
- one runner HTTP service in the pod for sync, restart, logs, and preview

For local Tilt development, `sandbox-runner-image` builds and loads the default
runtime image with:

```bash
make load-sandbox-runner-image
```

## Current Security Caveats

This POC runs user-generated code in a Kubernetes pod and should be treated as a
development-only runtime. It does not yet provide a complete untrusted-code
sandbox.

Known limitations:

- no per-user network policy is enforced by the provider
- no seccomp/AppArmor/runtime-class hardening is configured
- no CPU, memory, storage, or process quotas are applied by default
- runner images are mutable dev images until a publishing workflow pins them
- file sync is text-file oriented and skips binary or oversized App Studio files
- workspace deletion and runtime garbage collection are not yet complete

Before promoting this beyond local/dev use, add explicit runtime isolation,
quota defaults, image provenance controls, network policy, and lifecycle
cleanup.

## Capability Boundary

App Studio should only depend on these provider capabilities:

- `sync`
- `restart`
- `logs`
- `status`
- `previewURL`

It should not depend on sandbox pod names, PVC names, runtime namespaces, kube
service proxy paths, or runner process details.
