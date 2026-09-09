# Additional Features

This deployment includes the following additions beyond the upstream baseline.
Unless noted otherwise, existing tools retain their previous behavior when new
parameters and configuration are omitted.

## Generic Resource Workflows

### Discover Custom Resource APIs

Use `api_resources_list` to find the preferred API version, plural resource
name, and scope for a Kubernetes kind. This is particularly useful before using
generic resource tools against a custom resource definition (CRD).

```text
api_resources_list(kind="Application")
```

Use the returned `apiVersion` and `kind` with `resources_get`,
`resources_list`, `resources_create_or_update`, `resources_delete`, or
`resources_scale`.

The generic resource resolver also refreshes its REST mapper and retries once
when a CRD was installed after the server started. A server restart is no
longer needed to access that newly installed CRD.

### Validate Applies Without Persisting

Set `dry_run` on `resources_create_or_update` to use Kubernetes server-side
apply validation without persisting the manifest.

```json
{
  "resource": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: example\n",
  "dry_run": true
}
```

The API server still authorizes the request and may run admission webhooks,
defaulting, and policy checks. Dry-run means the Kubernetes object is not
persisted; it does not guarantee that external admission webhooks have no side
effects. The tool remains a destructive operation for read-only, confirmation,
and RBAC policy purposes.

`resources_scale` is intentionally unchanged. Use `dry_run` only for generic
server-side apply through `resources_create_or_update`.

### Retrieve Only Resource Status

Set `status_only` on `resources_get` to return only the top-level `.status`
object. This reduces token usage when polling controllers, Jobs, Deployments,
StatefulSets, Argo CD Applications, and other custom resources.

```json
{
  "apiVersion": "apps/v1",
  "kind": "Deployment",
  "namespace": "payments",
  "name": "api",
  "status_only": true
}
```

The response is a status object rather than a partial Kubernetes resource. A
resource with no status field returns `{}`. The request still requires normal
Kubernetes `get` permission for the resource.

### Paginate Large Resource Lists

`resources_list` supports opt-in Kubernetes pagination:

```json
{
  "apiVersion": "v1",
  "kind": "Pod",
  "namespace": "default",
  "limit": 100
}
```

`limit` must be between 1 and 500. When the API server provides another page,
the response text includes an opaque `continue_token`. Pass that token with the
same API version, kind, namespace, and selectors to retrieve the next page.

```json
{
  "apiVersion": "v1",
  "kind": "Pod",
  "namespace": "default",
  "limit": 100,
  "continue_token": "TOKEN_FROM_PREVIOUS_RESPONSE"
}
```

Without `limit`, `resources_list` retains its previous unlimited behavior.

### Redact Secret Values

Set `redact_secrets` to prevent generic resource results from exposing Secret
contents to the MCP client or model.

```toml
redact_secrets = "opaque"
```

Available modes:

| Mode | Behavior |
|------|----------|
| `opaque` | Replaces every `data.*` and `stringData.*` value with `[REDACTED]`. |
| `hashed` | Replaces values with an HMAC marker that can correlate equal values during one server process. |
| unset | Disables Secret response redaction. |

Both modes remove the `kubectl.kubernetes.io/last-applied-configuration`
annotation, which can otherwise contain unredacted Secret values. They retain
Secret names, namespaces, key names, labels, owner references, type, and other
annotations.

Redaction applies to generic `resources_get`, YAML `resources_list`, and
`resources_create_or_update` responses. It does not change the manifest sent to
Kubernetes, so applying a Secret persists its supplied value rather than the
redaction marker. It does not redact values in logs, pod command output,
ConfigMaps, or arbitrary custom resources. Prefer `opaque` unless same-process
Secret-value correlation is specifically required.

## Helm

### Configure Blocking Behavior

Helm install and uninstall previously waited for five minutes with no
configuration. Configure the behavior under `[toolset_configs.helm]`:

```toml
[toolset_configs.helm]
timeout = "4m"
wait = false
uninstall_wait = true
```

| Setting | Default | Effect |
|---------|---------|--------|
| `timeout` | `5m` | Positive Go duration used by Helm install and uninstall actions. |
| `wait` | `true` | Wait for installed release resources to become ready. |
| `uninstall_wait` | `true` | Wait for release resources to be deleted. |

Set `timeout` below the MCP client's own tool-call timeout so that Helm returns
the useful failure message before the client disconnects. Set `wait = false`
when an agent should create resources and then investigate readiness with other
Kubernetes tools. `uninstall_wait` is separate so an immediate reinstall does
not race cleanup.

### Inspect Release History

Use read-only `helm_history` to inspect recent revisions of a release:

```json
{
  "name": "payments-api",
  "namespace": "payments",
  "max": 10
}
```

The tool returns the newest requested revisions in chronological order. It
defaults to 10 entries and accepts at most 256. Results contain release
metadata, chart/app versions, revision, status, timestamps, and Helm's status
description. They intentionally exclude release manifests, supplied values, and
hooks because those can contain credentials.

## Argo CD Application Fleet Status

Enable the optional `argocd` toolset to inspect a fleet of standard Argo CD
`Application` CRs without retrieving verbose reconciliation histories:

```toml
toolsets = ["core", "config", "argocd"]
```

`argocd_applications_status` is read-only. It accepts an optional namespace and
Kubernetes label selector and returns structured summaries containing:

- Application name and namespace
- AppProject, source repository, revision, and path
- Destination server and namespace
- Sync and health status
- Last operation phase and message

Use generic core resource tools for Argo CD manifest CRUD. This toolset does
not perform syncs, rollbacks, Git diffs, or Argo CD Operator management.

## Multi-Cluster Metrics and Alerts

Enable the optional `observability/metrics` toolset to query Prometheus or
Thanos and Alertmanager. It uses the guarded tools from `rhobs/obs-mcp` and
does not use OpenShift Route discovery.

```toml
toolsets = ["core", "config", "observability/metrics"]

[toolset_configs."observability/metrics"]
# Fallback for contexts without an override.
prometheus_url = "http://127.0.0.1:9090"
alertmanager_url = "http://127.0.0.1:9093"
auth_mode = "kubeconfig"
guardrails = "all"
```

The toolset includes PromQL instant/range queries, metric and label discovery,
series inspection, Alertmanager alerts, and silences. Its query and cardinality
guardrails are enabled by default. `prometheus_url` is required; configure
`alertmanager_url` when using alert or silence tools.

### Select Endpoints by Kubernetes Context

Associate separate observability endpoints with kubeconfig context names. The
standard MCP `context` parameter selects both the Kubernetes target and its
corresponding metrics endpoints.

```toml
[observability_metrics_contexts.cpt-hybrid-production]
prometheus_url = "http://127.0.0.1:19090"
alertmanager_url = "http://127.0.0.1:19093"

[observability_metrics_contexts.cpt-hybrid-staging]
prometheus_url = "http://127.0.0.1:29090"
alertmanager_url = "http://127.0.0.1:29093"
```

Context entries override only the URLs they specify. A missing context entry
uses the default URLs from `[toolset_configs."observability/metrics"]`.
Endpoint URLs cannot be supplied in tool calls, preventing MCP callers from
redirecting requests to arbitrary hosts.

For local interactive use, persistent port forwards are appropriate:

```shell
kubectl --context cpt-hybrid-production -n <monitoring-namespace> \
  port-forward svc/<prometheus-service> 19090:9090
kubectl --context cpt-hybrid-production -n <monitoring-namespace> \
  port-forward svc/<alertmanager-service> 19093:9093
```

Run equivalent port forwards with different local ports for each context. Keep
them running for the MCP server session. For remotely deployed or long-running
MCP servers, prefer stable, authenticated HTTPS endpoints behind an internal
gateway or private load balancer.

When `require_tls = true`, all configured metrics and Alertmanager endpoints
must use HTTPS and `insecure = true` is rejected. Local HTTP port forwarding is
therefore intended for development or trusted local operation without
`require_tls`.

## Multi-Cluster Target Selection

When more than one Kubernetes target is available, every cluster-aware tool
requires the injected target parameter, normally `context`. This prevents an
omitted target from silently running against the default cluster.

```json
{
  "context": "cpt-hybrid-production",
  "apiVersion": "apps/v1",
  "kind": "Deployment",
  "namespace": "payments",
  "name": "api"
}
```

Use the target-list tool, normally `contexts_list`, to discover valid context
names.

## Reliability Fixes

Two operational fixes apply automatically and need no configuration:

- `pods_log` now preserves meaningful Kubernetes API error details when logs
  for a terminated or removed container cannot be retrieved.
- `nodes_log` does not send `tailLines` for file queries, which kubelets reject.
  File paths are relative to `/var/log`, such as `/kubelet.log`; `tailLines`
  remains available for service queries such as `kubelet`.
