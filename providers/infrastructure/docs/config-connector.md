# Config Connector for local Cloud Run templates

The `gcp-cloud-run-service` Template generates a kro ResourceGraphDefinition
that creates Config Connector resources on the kro runtime cluster. Install
Config Connector before provisioning the template, otherwise the generated
RunService and IAMPolicyMember resources have no controller.

## Local Tilt setup

The Tilt resources `config-connector-up`, `config-connector-status`, and
`config-connector-down` wrap the Make targets below. They are manual because
`config-connector-up` imports Google Cloud credentials into the local runtime
cluster.

```sh
export CONFIG_CONNECTOR_KEY_FILE=/absolute/path/to/key.json
tilt trigger config-connector-up
```

`CONFIG_CONNECTOR_KEY_FILE` must point to an existing service-account JSON key.
Keep that file outside the repository, or place it under `.secrets/`, which is
ignored. The Make target imports it as a Kubernetes Secret in `cnrm-system` with
the required key name `key.json`; it does not print the key or write it to disk.

Optional settings:

```sh
export CONFIG_CONNECTOR_KUBECONFIG=/absolute/path/to/runtime.kubeconfig
export CONFIG_CONNECTOR_SECRET_NAME=kcc-gcp-key
```

If `CONFIG_CONNECTOR_KUBECONFIG` is unset, the Make target uses the active kro
runtime kubeconfig: `KRO_TARGET_KUBECONFIG` when set by `Tiltfile.cluster`, or
`.kedge-kro.kubeconfig` in the embedded-kcp Tiltfile.

## Google Cloud prerequisites

Create the service account, grant the minimum roles needed for the resources you
plan to reconcile, enable the required Google APIs, and create the key yourself.
For the first Cloud Run template, enable `run.googleapis.com` and grant access
to manage Cloud Run plus the public invoker IAM binding.

Service-account keys are a local-dev compromise. Prefer Workload Identity or
another keyless identity mechanism for production clusters.
