# Fork Changelog

This changelog records changes maintained by
[BasBenIk/kubernetes-mcp-server](https://github.com/BasBenIk/kubernetes-mcp-server),
including how upstream changes are integrated. It is separate from the
[upstream release history](https://github.com/containers/kubernetes-mcp-server/releases).
Dates describe development checkpoints, not independently published releases.

## 2026-10-14 — Upstream synchronization

### Upstream baseline

- Integrate all 31 upstream `main` commits after the fork point
  `a6f6ef90e4644fb6c3dc6cb143fcdb319f8f695a`, through
  [`aecddcb7299972afa003b3fbd577f3008b96c8a1`](https://github.com/containers/kubernetes-mcp-server/commit/aecddcb7299972afa003b3fbd577f3008b96c8a1)
  (October 13, 2026), retaining the 25 existing fork commits and their history.
- Adopt upstream OAuth metadata fixes, explicit token-exchange configuration,
  permission metadata, aggregate cluster discovery, MCP Apps infrastructure,
  KubeVirt console screenshots, wrapper stderr fixes, and updated evaluations,
  dependencies, and CI tooling. These are upstream contributions, not new fork
  features. MCP Apps remain disabled by default; KubeVirt tools remain optional.
- Replace the temporary MCP SDK session-cleanup pin with upstream's stable
  `github.com/modelcontextprotocol/go-sdk v1.8.0`, which contains that fix.
  Retain the fork's stateful HTTP session-release regression test.

### Fork integration

- Port the fork's Argo CD and metrics toolsets to the new toolset and handler
  APIs, and migrate `redact_secrets` and `observability_metrics_contexts` to
  upstream's reloadable `Option[T]` configuration model.
- Pin `github.com/rhobs/obs-mcp` to
  `v0.8.2-0.20261007085909-6fd69c6ee57f`. Its latest tagged release, `v0.8.1`,
  still uses the removed server APIs. Import only its metrics package, not the
  aggregate toolsets or OpenShift backend discovery.
- Route metrics using the resolved Kubernetes handler target and request-scoped
  configuration overrides, without mutating shared configuration. Retain
  explicit endpoint configuration, query guardrails, kubeconfig authentication,
  HTTPS checks, and rejection of insecure TLS when `require_tls` is enabled.
  Mark the endpoint map sensitive in configuration dumps so URL credentials are
  not logged during startup or reload.
- Preserve mandatory multi-cluster target selection, lazy optional-context
  startup behavior, bounded cluster/namespace health checks, Secret redaction,
  resource pagination and status-only views, server-side dry runs, Helm timeout
  controls and history, Argo CD fleet status, and pod/node log error fixes.
- Enforce target selection in raw MCP tool handlers as well as advertising it in
  the schema. Missing, blank, or non-string targets on multi-cluster tool calls
  are rejected before selecting credentials or running confirmation prompts;
  the SDK's raw-handler path does not enforce schema requirements itself.
- Add permission metadata to fork tools and both bounded health prompts, matching
  their actual resource and namespace scopes rather than copying upstream's
  broader health-check requirements.
- Correct Secret apply-response redaction: redact a copy of the server response
  after apply, rather than a result that apply subsequently overwrites. Verify
  the response excludes base64-encoded credentials while persistence retains the
  original supplied value.
- Combine upstream read-only tests with the fork's independent non-mutation
  coverage. Keep `configuration_view` unavailable in read-only mode even when
  explicitly allowlisted, and exclude the credential-viewing evaluation from the
  read-only suite.
- Align Kubernetes modules at `v0.37.1` for the updated metrics dependency;
  retain the fork's newer controller-runtime/envtest `v0.25.0` tooling.
- Retain `github.com/prometheus/client_golang v1.24.1` as an explicit compatibility
  exception to upstream dependency commit `61dace4`. Version `v1.25.0` changes
  query API return signatures and does not compile with the current obs-mcp
  loader. Remove this pin when obs-mcp supports those signatures; all upstream
  commits remain in the fork's history.

### Upgrade notes

This synchronization includes upstream breaking changes. See
[Configuration Changes](configuration-changes.md) for the complete migration.

- Move runtime flags such as `--port`, `--read-only`, `--toolsets`, and
  `--kubeconfig` into TOML (`port`, `read_only`, `toolsets`, `kubeconfig`).
  Launch with `--config` and/or `--config-dir`.
- Replace `K8S_MCP_CONFIG_PATH` with `MCP_CONFIG_PATH`; explicitly supply
  `--config-dir` for drop-ins previously loaded from an implicit `conf.d/`.
- Replace legacy `sts_*` and `token_exchange_strategy` settings with the
  `[token_exchange]` and `[token_exchange.client_auth]` tables.
- Unknown configuration keys fail loading. Invalid SIGHUP reloads now terminate
  the process instead of retaining the previous configuration.
- Existing fork-specific TOML keys and tool parameters retain their names.
  No external MCP client configuration or running deployment is changed by the
  source synchronization itself.

### Validation

- `make build` passed, including module tidying, formatting, and linting
  (`golangci-lint`: zero issues).
- `make test` passed: the complete default Go test suite with the race detector.
  Regression coverage includes stateful HTTP session release, unreachable
  optional targets, read-only direct-call rejection, bounded health prompts,
  Secret response redaction, and concurrent metrics endpoint isolation.
- Regenerated tool/prompt snapshots and README/configuration tool documentation;
  the normal test run verifies the regenerated snapshots.
- Live mcpchecker evaluations, browser tests, and cluster-dependent E2E suites
  were not run. No running deployment or external client configuration was
  changed.

## 2026-09-08–09 — Initial fork additions

Baseline: upstream `a6f6ef90e4644fb6c3dc6cb143fcdb319f8f695a`.
Fork checkpoint: `6fb855ad81625b2d626893cc6ac2b6fe69aa3297`.

### Safety and bounded diagnostics

- Require an explicit target for multi-cluster tools (`95ce83a`).
- Add optional opaque/HMAC Secret response redaction (`ad8ac81`).
- Hide credential-bearing kubeconfig output in read-only mode and verify direct
  write calls cannot mutate an existing resource (`87de674`, `645373c`).
- Bound cluster health collection by deadlines, concurrency, list/event limits,
  stable ordering, partial-result reporting, and output size; add a separate
  namespace health prompt with no cluster-wide fallback (`c6f0f97`, `3447afb`).

### Operational features

- Discover API resources by kind and refresh resource mappings after CRD changes
  (`e661266`).
- Add server-side apply dry-run, resource-list pagination, and status-only
  retrieval (`fab2178`, `3902dbf`, `f6c0a97`).
- Configure Helm operation waits/timeouts and expose bounded, metadata-only
  release history (`ce3eb3e`, `2b14961`).
- Add read-only Argo CD Application fleet summaries (`0a1eb61`).
- Add guarded, explicitly configured Prometheus/Alertmanager tools with
  context-specific endpoint routing (`cd1aefd`, `7650144`).

### Reliability, tests, and documentation

- Preserve Kubernetes pod-log API errors and omit unsupported `tailLines` for
  node file-log queries (`01e45a5`, `a5bc47a`).
- Pin the SDK session-release fix and add stateful HTTP lifecycle tests
  (`a963186`).
- Add unreachable optional-kubeconfig-target startup coverage (`6544ede`).
- Add live read-only diagnostic/non-mutation evaluations and update Kubernetes
  and golang-x dependencies (`645373c`, `ab45cdd`).
- Document fork features and safety boundaries, prominently identify the
  independent fork, and remove README demos (`ead8d2f`, `616b929`, `8edc33c`,
  `6fb855a`); apply static-analysis fixes (`b2d889b`).

See [Additional Features](added-features.md) for usage and limitations.
