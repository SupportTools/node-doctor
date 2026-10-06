# Docs fix plan

The work is split into 9 PRs: 3 that fix code and 6 that fix docs. Code fixes land before the docs that describe them, so each doc only needs one rewrite. Findings and line references are in [DOCS_AUDIT.md](DOCS_AUDIT.md).

## Defaults I picked (change before starting if you disagree)

1. **Code and docs ship in separate PRs.** Example YAMLs go with the code fix because tests load them.
2. **Dead config fields are removed, not documented.** Config parsing is not strict (no `KnownFields`), so old configs that still set these fields keep loading. Removing them changes nothing for users. The one exception is `circuitBreaker`: it is wired at startup, not removed.
3. **Controller docs get a "no published image yet" banner.** Building a controller image is a separate decision (see Out of scope).
4. **SECURITY.md stays.** Cosign keyless signing is real (`release.yml:104`).

## Order

```
PR1 (cleanup) ──────────────────────────────────────────┐
PR2 (monitor config bugs) ─► PR6 (monitors docs)        │
PR3 (build/release bugs) ──► PR8 (install/ops docs)     ├─► done
PR4 (remediation config) ──► PR5 (config ref) ─► PR7 (remediation + architecture)
                                                PR9 (dev docs) ─┘
```

PR1, PR2, PR3, PR4 and PR9 can all start in parallel.

---

## PR1 `docs: remove template leftovers and rebuild the docs index`
- Delete `docs/development/` (all 4 files) and `docs/deployment.md`.
- Delete `.githooks/` (README, `setup.sh`, and the `pre-push` hook that just exits). If you want to keep the hook, re-enable it instead and rename it from Nexmonyx.
- Delete the dead scripts `init-from-template.sh`, `substitute-variables.sh`, `validate-api.sh` and the GPG scripts, plus the dead component branches in `validate-pipeline-local.sh:182-198`.
- Delete the Makefile targets that only call the deleted scripts. Check `qa-check` and `devils-advocate` too.
- Rewrite `docs/README.md` as a short index of the real docs, grouped by reader (operator, developer).
- **Done when** the link check passes (see Checks).

## PR2 `fix(monitors): honor documented config keys in defaults and examples`
- `disk.go:32,51`: change the built-in default to `mountPoints` and `checkReadonly`.
- `dns.go:607-609`: change the default to the key the parser reads (`nameserverCheckEnabled`).
- Example YAMLs (`config/node-doctor.yaml`, `config/examples/*.yaml`):
  - `paths` → `mountPoints`
  - `loadAverageThresholds` → `warningLoadFactor`/`criticalLoadFactor`
  - `pattern:` → `name` + `regex`
- Extend `config/examples/validation_test.go` so it builds every monitor in every example through the registry factory and validator, not just the top-level config. `custom-plugins.yaml` failing to start would then have been caught. Add `testing.yaml` to the test.
- **Done when** `go test ./config/... ./pkg/monitors/...` passes and the new test fails on the old YAML.

## PR3 `fix(build): e2e tag, image port and v-stripped tags`
- `Makefile:243`: add `-tags=e2e` to `test-e2e`.
- `Dockerfile:56`: `EXPOSE 9101`.
- `release.yml:197`: print the pull command without the `v`.
- `deployment/daemonset.yaml:132`: pin the current release with the v-stripped tag. Better: have the release job rewrite the pin, the same way it already attaches the file.

## PR4 `fix(remediation): apply circuit breaker config at startup, drop unused fields`
- `main.go`: apply `remediation.circuitBreaker` when the agent starts, not only on reload. Either honor `enabled`, or remove it and document the breaker as always on. I'd honor it.
- Remove these fields from `pkg/types/config.go`, including their validation and defaulting:
  - per-monitor `cooldown`, `maxAttempts`, `priority`, `gracefulStop`, `waitTimeout`
  - global `cooldownPeriod`, `maxAttemptsGlobal`, `overrides`
  - `features.*`
  - `settings.enableRemediation`
  - First check that the controller does not need any of them; the audit says the controller reads some of them.
- Remove the `exporters.http.bindAddress` Helm value, or add the field to the code. I'd remove it.
- Remove the unregistered `DiskRemediator`/`RuntimeRemediator`, or register them. I'd remove them; validation already rejects their strategy names.
- **Done when** a test proves a non-default `circuitBreaker.timeout` takes effect without a reload.

## PR5 `docs(config): rebuild the configuration reference from the code`
- Rewrite `docs/configuration.md` (target about 500 lines, down from 1,556) from `pkg/types/config.go` after PR4. Include:
  - every field with its real default
  - `reload`, `remediation.coordination`, `exporters.http.controller`, `conditionStaleTTL`, `exporters.prometheus.bindAddress`
  - all 8 strategies
  - env substitution (whole-file `os.ExpandEnv`)
  - the config search path
- Add a **CLI flags** table (`main.go:112-125`) and a **health endpoints** table: `/healthz`, `/ready`, `/status`, `/remediation/history`, the unix socket, and the liveness/readiness split.
- Add `docs/metrics.md`, built from `pkg/exporters/prometheus/metrics.go` plus the controller metrics. Every other doc links here instead of listing metric names.
- Drop the production example, best practices, troubleshooting and the duplicated field summary; link `config/examples/` instead.
- Drop all `file.go:NNN` line citations, since they go stale.

## PR6 `docs(monitors): fix type names and keys, merge the DNS docs`
- `docs/monitors.md`:
  - fix the type names
  - fix the kubelet `auth`/`circuitBreaker` keys, the logpattern `name`/`regex` keys, and the disk keys
  - fix the CPU validation claim, the DNS `clusterDomains` default and AAAA support
  - add `network-ip-forwarding`
  - drop the "Source File" lines and the troubleshooting section
  - move the DNS best practices into the DNS doc
- Merge `dns-troubleshooting.md` into `dns-advanced.md`, keeping one field listing and dropping the `logLevel` advice. Link it from monitors.md.
- `config/examples/README.md`:
  - trim it to an index of the YAML files and the full type list (including IPv6 and ip-forwarding)
  - fix `--validate-config`
- Link `examples/dns-monitoring/` from the DNS doc, or move it under `config/examples/`.

## PR7 `docs: rewrite remediation and architecture to match the code`
- `docs/remediation.md` (target about 400 lines, down from 1,765):
  - keep the safety pipeline, the registered strategies (including `node-reboot` and `pod-delete`, with their danger called out) and dry-run
  - correct the `reset-routing` description
  - drop the invented fields, generic best practices and the custom-remediator dev guide
- `docs/architecture.md` (target about 250 lines, down from 978): cover these, with no YAML and no package tree:
  - the components
  - the monitor → detector → exporter flow
  - the real `Exporter` interface
  - the reload lifecycle (`ClassifyReload`, `ConfigReloadRestartRequired`)
  - the health/probe design
  - peer pruning

## PR8 `docs: consolidate install docs and rewrite troubleshooting`
- Each install method gets one home:
  - Helm → `helm/node-doctor/README.md`. Make it the full values reference: fix the image repo; add probes, `publishNotReadyAddresses`, `controller.*`, `overlayTest.*`, `reload`, `prometheusRule`.
  - Raw manifests → `deployment/README.md`: 500m CPU, port 9101, namespace `node-doctor`, exec vs httpGet probes.
  - First run → `docs/quick-start.md`: fix the metrics, link dashboards/README instead of copying the import steps.
- `README.md`:
  - keep the overview, one Helm install block and links
  - delete the binaries/signing section, the invented metrics and the `configmap.yaml` step
  - fix `/healthz`, the namespace and the tags
- `docs/troubleshooting.md`: rewrite to about 150 lines using only real material:
  - endpoints over the socket
  - real metrics
  - `ConfigReloadRestartRequired`
  - the stuck-rollout steps from the helm README
  - NotReady scraping
  - which tools the image lacks, and to use `kubectl debug` instead of `exec curl`
- `docs/controller-deployment.md`: lead with Helm `controller.enabled`, add the "no published image" banner, and fix the metrics port and names.
- `dashboards/README.md`: add `node-doctor-ipv6.json`.

## PR9 `docs(dev): fix contributor and testing docs`
- `CONTRIBUTING.md`:
  - Go 1.25
  - real make targets (`build-node-doctor-image`, etc.)
  - golangci-lint instead of staticcheck
  - 70% coverage
  - replace the command list with a pointer to `make help`
- Make the coverage number agree everywhere: `Makefile:276` (80) must match CI (70).
- `docs/testing-guide.md`:
  - remove the 5 fake targets
  - fix the e2e claim (correct after PR3)
  - fix the coverage table
  - add about 5 lines on `validate-pipeline-local.sh`
- `test/e2e/README.md`, `test/integration/README.md`: Go 1.25, trim to directory specifics, link testing-guide.
- `scripts/README.md`: list the real scripts (after PR1).
- `.github/workflows/README.md`: add a line on self-hosted runners and the baked-in tools. Correct the Grype claim: it runs inline in `release.yml`, not via the reusable workflow.

---

## Checks to run on every docs PR

Add these to a `make docs-check` target, and to CI once PR1 lands so the problems can't come back:
- **Relative link check:** the `find`/`grep` loop from the audit, or `lychee --offline`.
- **Banned strings** in `*.md` must return nothing:
  - `system-(cpu|memory|disk)-check`
  - `custom-(plugin|logpattern)-check`
  - `kube-system`
  - `:9100/metrics`
  - `GET /health\b`
  - `pipeline-v2`
  - `Nexmonyx`
  - `TaskForge`
  - `{{PROJECT_NAME}}`
- **Every YAML block in docs with a top-level `monitors:`** passes `node-doctor -validate-config`. This is optional; the extended example test from PR2 covers most of it.

## Out of scope (separate decisions)
- **Controller image:** nothing builds it today. Either add a Dockerfile and a release step, or mark the controller as experimental. Until then, the PR8 banner covers it.
- **Strict unknown-key warnings in monitor config parsers:** this would catch future key drift like `paths`. It's worth doing, but it's a behavior change.

## Size
- **PR1:** small
- **PR2, PR3:** small
- **PR4:** medium (removing fields touches config, validation, the controller and tests)
- **PR5–PR8:** medium to large writing, mostly deletion
- **PR9:** small

Net result: the markdown shrinks from about 19.4k lines to about 7k.
