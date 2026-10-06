# Docs audit (2026-10-06)

The repo has 19.4k lines of markdown in 31 files. About a third is template leftover or invented content. Most of the rest has wrong type names, ports, endpoints or metric names. Some of the problems are code bugs, so they are listed separately below.

## Facts the docs get wrong most often

| Topic | Truth | Source |
|---|---|---|
| Monitor types | `system-cpu`, `system-memory`, `system-disk`, `custom-plugin`, `custom-logpattern`, `network-*`, `kubernetes-*`, `network-ip-forwarding`. No `-check` suffix on system/custom types | `pkg/monitors/**` |
| Metrics port | 9101 when deployed (code default 9100) | `helm/node-doctor/values.yaml:107`, `deployment/daemonset.yaml:216` |
| Health | TCP 8080 + `/var/run/node-doctor/health.sock`; `/healthz`, `/ready`, `/status`, `/remediation/history`; exec probes `-healthcheck`/`-healthcheck-ready` | `pkg/health/server.go:230` |
| Namespace | `node-doctor`, not `kube-system` | `deployment/daemonset.yaml:5` |
| Image | `docker.io/supporttools/node-doctor`, tags without `v`, debian-slim (no curl/wget/nslookup) | `release.yml:67`, `Dockerfile` |
| Metrics | `node_doctor_*` names in `pkg/exporters/prometheus/metrics.go:117-607`. Most names in README/architecture/troubleshooting are invented | |
| Strategies | `systemd-restart`, `custom-script`, `flush-dns`, `restart-interface`, `reset-routing`, `flush-ipv6-route`, `node-reboot`, `pod-delete`. Disk/Runtime remediators are not registered | `pkg/remediators/builtin.go` |
| CLI | `-validate-config` (not `--validate`), `-dump-config`, `-list-monitors`, `-health-socket`, `-enable-profiling`, `-profiling-port` | `cmd/node-doctor/main.go:112-125` |
| Go | 1.25 (docs say 1.21) | `go.mod` |
| Coverage gate | 70% in CI (docs say 80/85) | `.github/workflows/ci.yml` |

## Plan per file

### Delete (template leftovers or mostly invented)
- `docs/development/master-task-management.md`: TaskForge/Nexmonyx template
- `docs/development/local-cicd-validation-guide.md`: Nexmonyx components, `pipeline-v2.yml`
- `docs/development/validation-quick-reference.md`: same; keep about 5 lines in testing-guide
- `docs/development/task-execution-workflow.md`: template process; move node-doctor rules (line 695+) to CONTRIBUTING if wanted
- `.githooks/README.md`: "Git Hooks for Nexmonyx", and the hook it describes is disabled (`exit 0`)
- `docs/deployment.md`: stale (Alpine, v0.1.0, kube-system, 9100, personal paths) and duplicates the other install docs

### Rewrite
- `docs/README.md`: template hub with 15+ dead links; replace with an index of the real docs
- `docs/troubleshooting.md` (1765 lines): about 80% invented (metrics, config keys, `/debug/config`, top-level `remediators:`). Rewrite to about 150 lines from real material
- `docs/architecture.md` (978 lines): wrong interfaces, package tree, endpoints, metrics; "future" features already shipped. Rewrite to about 250 lines
- `scripts/README.md`: list the real scripts

### Update and trim
- `README.md`: one Helm install block plus links; fix 9101, `/healthz`, namespace, tags; delete the binaries/signing section and invented metrics
- `docs/configuration.md` (1556 lines, target about 500): one schema reference rebuilt from `pkg/types/config.go`. Add `reload`, `coordination`, `exporters.http.controller`, `conditionStaleTTL`, 8 strategies and a CLI flags table. Mark dead fields. Drop examples, best practices and the duplicated field summary
- `docs/remediation.md` (1765 lines, target about 400): add `node-reboot`/`pod-delete`. Drop Disk/Runtime, invented fields and generic best practices. Fix the `reset-routing` description
- `docs/monitors.md` (2403 lines): fix type names and kubelet/logpattern/disk keys, add `network-ip-forwarding`, drop stale line citations and the troubleshooting section, move DNS best practices into the DNS docs
- `docs/controller-deployment.md`: lead with Helm `controller.enabled`; fix metrics; blocked on the missing controller image
- `docs/quick-start.md`: fix metrics, add the IPv6 dashboard, link dashboards/README for import steps
- `docs/testing-guide.md`: remove 5 fake make targets; fix the e2e claim and the coverage table
- `CONTRIBUTING.md`: Go 1.25, real make targets, golangci-lint, 70% coverage; cut the duplicated test commands
- `helm/node-doctor/README.md`: fix the image repo; add probes, `publishNotReadyAddresses`, `controller.*`, `overlayTest.*`, `reload`, `prometheusRule`. Make this the single values reference
- `deployment/README.md`: 500m CPU, 9101, namespace
- `config/examples/README.md`: trim to an index plus the correct type list; fix the flag name
- `dashboards/README.md`: add `node-doctor-ipv6.json`
- `test/e2e/README.md`, `test/integration/README.md`: Go 1.25, trim to directory specifics
- `.github/workflows/README.md`: note self-hosted runners
- `SECURITY.md`: check the Cosign signing claim against `release.yml`

### Merge
- `docs/monitors/dns-troubleshooting.md` into `dns-advanced.md` (or into troubleshooting.md); drop the `logLevel` advice
- `docs/monitors/dns-advanced.md`: keep one field listing (it has YAML and tables), link from monitors.md
- `examples/dns-monitoring/`: link it or move it under `config/examples/`

### Keep as is
- `docs/release-process.md`, `docs/testing.md`

### Create
- A CLI flags and health endpoints reference (inside configuration.md is fine)
- A metrics reference generated from `metrics.go` (could replace all the invented lists)

## Code bugs found (not doc work)

| Bug | Where |
|---|---|
| Disk built-in default uses `paths`/`checkReadOnly`; parser reads `mountPoints`/`checkReadonly`. Defaults silently dropped | `pkg/monitors/system/disk.go:32,51` vs `:671,719` |
| Example YAMLs use `paths`, `loadAverageThresholds`, and `pattern:` (logpattern needs `name`+`regex`, so that monitor fails to start) | `config/*.yaml`, `config/examples/*.yaml` |
| DNS default sets `checkNameservers`/`enableNameserverChecks`, which the parser never reads | `pkg/monitors/network/dns.go:607-609` |
| `circuitBreaker` config ignored at startup (registry starts with defaults, only reload applies it); `enabled` never checked | `pkg/remediators/registry.go:302,437` |
| Per-monitor `cooldown`/`maxAttempts`/`priority`, global `cooldownPeriod`/`maxAttemptsGlobal`/`overrides`, `features.*`, `settings.enableRemediation` are parsed but unused | `pkg/types/config.go` |
| `exporters.http.bindAddress` Helm value does nothing (health server hard-codes `::`:8080) | `cmd/node-doctor/main.go:522` |
| `make test-e2e` runs zero tests (missing `-tags=e2e`) | `Makefile:243` |
| No controller image is built (no Dockerfile, workflow or make target) | `.github/`, `Makefile` |
| `release.yml:197` prints `v`-prefixed pull command; `Dockerfile` `EXPOSE 9100`; `deployment/daemonset.yaml` pins `v1.5.4` | |
| Dead template scripts: `init-from-template.sh`, `substitute-variables.sh`, `validate-api.sh`, GPG scripts; dead branches in `validate-pipeline-local.sh:182-198` | `scripts/` |
