# GitHub Actions Workflows

This directory contains every GitHub Actions workflow for Node Doctor. There are four:

| File | Trigger | Purpose |
|---|---|---|
| `ci.yml` | PR to `main`, push to `main`, push of a `v*` tag | Lint, test, chart verification, build, gosec, and a Docker build that **never pushes** |
| `ci-ipv6.yml` | Same, but path-filtered to network/exporter/integration code | IPv6 and dual-stack tests needing a kind cluster and `CAP_NET_RAW`. Deliberately separate so its flakes do not block `ci-success` |
| `release.yml` | Push of a `v*` tag **only** | The actual release: multi-arch images, GitHub Release, Helm chart publish. **The only workflow that publishes anything** |
| `release-audit.yml` | Daily at 13:00 UTC, plus `workflow_dispatch` | Compares the newest `v*` tag against the chart version served by charts.support.tools; Slack on mismatch |

For the full release story — what gets published where, what is signed, how to roll back — see
[docs/release-process.md](../../docs/release-process.md). That document is the source of truth;
this one covers only what lives in this directory.

## `ci.yml`

### Jobs

1. **Lint** — `golangci-lint` (pinned `v1.64.8`), config in `.golangci.yml` at repo root,
   5-minute timeout.
2. **Helm Chart** — runs `make helm-verify-generated`, then `helm lint ./helm/node-doctor`.
   This job exists to catch the generated-vs-template trap: `release.yml` re-renders
   `Chart.yaml`/`values.yaml` from `*.template` before packaging, so hand-edits to the committed
   copies never reach the published chart. See
   [docs/release-process.md](../../docs/release-process.md#which-helm-chart-files-actually-ship).
3. **Test** — single Go version (`env.GO_VERSION`, currently 1.25 — there is no version matrix).
   Unit tests with `-race -short`, then integration tests from `test/integration/` if present.
   Enforces a **70% unit coverage threshold inline**; that inline check is the hard gate.
   Codecov upload is `continue-on-error` and purely informational.
4. **Pinger ICMP Integration** — compiles the network test binary as the runner user, then runs
   only `TestDefaultPinger_Integration` under `sudo` with `NODE_DOCTOR_ICMP_INTEGRATION=1`, so a
   missing `CAP_NET_RAW` is a hard failure instead of a silent skip. Separate job so a
   privileged-socket flake does not block the coverage job.
5. **Security Scan (gosec)** — `securego/gosec` with `-no-fail`; uploads SARIF to the Security
   tab. Never gates the build.
6. **Build** — compiles `node-doctor` and `overlay-test-server`, runs `--version`, uploads the
   binary as a 7-day artifact. Needs `lint` + `test`.
7. **Docker Build (no push)** — runs on every PR and branch push, needs `lint`, `test`, `build`,
   `security-gosec`. Builds `linux/amd64` with **`push: false`**. It exists to keep the
   Dockerfile under test; it publishes nothing and cannot race `release.yml` for a tag.
8. **CI Success** — aggregate status check over `lint`, `test`, `build`, `security-gosec`,
   `helm-chart`, `docker`.

New gating jobs are wired into `ci-success`'s `needs:` rather than added as new required status
checks — a brand-new required context leaves every already-open PR stuck on
"Expected — waiting for status".

### `ci.yml` publishes nothing

It used to. A tag-gated `Docker Build & Push` job pushed
`docker.io/supporttools/node-doctor:v<TAG>` (amd64 only, unsigned) plus an unconditional
`:latest`, at the same time `release.yml` pushed the same repository multi-arch. With no
ordering between the two, whichever finished last won `:latest` — so `:latest` could silently
become amd64-only against a fleet with arm64 nodes, and an RC tag moved `:latest` regardless of
`release.yml`'s prerelease exclusion.

`release.yml` is now the sole publisher, and the Grype scan moved there with it (it scans a
*published* image). Full story in
[docs/release-process.md](../../docs/release-process.md#which-image-tags-actually-exist).

## `release.yml`

Triggered only by pushing a tag matching `v*`. It runs **no tests** and does not depend on
`ci.yml`; a tag whose tests fail still produces a complete release.

Jobs: `docker`, `docker-overlay-test`, `grype-scan`, `github-release`, `helm-publish`,
`release-complete`, `notify-failure`.
Full breakdown in [docs/release-process.md](../../docs/release-process.md#release-overview).

Two things worth knowing before you touch it:

- `helm-publish` refuses to publish a chart whose default image tags do not resolve in the
  registry (`Verify packaged chart references images that exist`). Image tags are **`v`-stripped**;
  only `Chart.yaml` `appVersion` keeps the `v`.
- `notify-failure` (`if: failure()`) posts to Slack via `SLACK_WEBHOOK_URL`. Without it a broken
  release is just a red square nobody is subscribed to — which is exactly how chart publishing
  stayed broken from `v1.8.0` to `v1.8.6`.

## `release-audit.yml`

Daily standing check that the newest `v*` tag has a matching chart on charts.support.tools,
with a one-hour grace period for an in-flight release. Catches releases that were never started,
were cancelled, or whose failure notification did not arrive. Notifies Slack on mismatch.

## Required GitHub Secrets

| Secret | Used by | Required |
|---|---|---|
| `DOCKER_USERNAME` | `release.yml` docker + docker-overlay-test + helm-publish jobs; `ci.yml` docker job (base-image pulls only) | Yes for releases; in CI only to dodge anonymous pull rate limits |
| `DOCKER_PASSWORD` | same | same |
| `BOT_TOKEN` | `release.yml` `helm-publish` — checkout of and push to `SupportTools/helm-chart` | Yes, or the chart is never published |
| `SLACK_WEBHOOK_URL` | `release.yml` `notify-failure`, `release-audit.yml` | Yes, or release failures go unnoticed |
| `CODECOV_TOKEN` | `ci.yml` Test job | Optional; upload is non-blocking |
| `GITHUB_TOKEN` | `release.yml` `github-release`, `release-audit.yml` | Provided automatically |

`HELM_CHART_PAT` is an org secret that looks like it belongs here and **does not** — no
node-doctor workflow references it. See
[docs/release-process.md](../../docs/release-process.md#helm_chart_pat-is-not-this-repos-credential).

The registry is **Docker Hub** (`docker.io/supporttools`), not Harbor. There are no
`HARBOR_USERNAME` / `HARBOR_PASSWORD` secrets in use; if they exist in repository settings they
are dead and should be removed.

Likewise there is **no `GPG_PRIVATE_KEY` step** in any workflow. Signing is keyless cosign only,
on a single image tag.

### How to add secrets

1. Repository **Settings** -> **Secrets and variables** -> **Actions**
2. **New repository secret**
3. Use the exact names in the table above

## Triggering builds

### Pull request
Opens lint, helm-chart, test, pinger-icmp, gosec, build and docker jobs. No images are pushed —
the docker job builds with `push: false`.

### Push to `main`
Same set. Still no images.

### Release
```bash
git tag -a v1.9.0 -m "Release v1.9.0"
git push origin v1.9.0
```
Runs `ci.yml` (full pipeline, still publishing nothing) **and** `release.yml` (multi-arch
images, Grype, GitHub Release, Helm chart) in parallel. `release.yml` is the one that matters
for artifacts; `ci.yml` is the one that tells you whether the commit was any good. Watch both:

```bash
gh run list --workflow=ci.yml --branch v1.9.0
gh run list --workflow=release.yml --branch v1.9.0
```

## Local equivalents

```bash
make lint                   # golangci-lint
make helm-verify-generated  # the Helm Chart job's hard check
make helm-lint              # helm lint (depends on the above)
make test-ci                # closest mirror of the Test job (unit -short + integration)
make build                  # both binaries
docker build -t node-doctor:local .
```

## Troubleshooting

**Lint failures** — `make lint` locally; `make fmt` fixes formatting.

**Test failures** — `make test-ci`. The CI job uses `-race`; reproduce with
`go test ./pkg/... ./cmd/... -race -short`.

**Coverage below threshold** — the gate is 70% on unit tests only (`-short`). `make
coverage-check` uses a stricter local threshold of 80%, so passing locally implies passing CI.

**Helm Chart job failure** — you edited `helm/node-doctor/values.yaml` or `Chart.yaml` directly,
or edited a `*.template` without regenerating. Run `make helm-generate` and commit both files.

**Docker job failed** — the Dockerfile does not build. It publishes nothing either way, so this
never affects the registry; it does block `ci-success`, and it should, because the same
Dockerfile is what `release.yml` builds from.

**`docker pull ...:v1.9.0` says not found** — expected. Image tags are `v`-stripped; pull
`1.9.0`. See
[docs/release-process.md](../../docs/release-process.md#docker-pull-supporttoolsnode-doctorv190-says-not-found).

**A release published images but no chart** — check `helm-publish` in that `release.yml` run,
and check Slack for the `notify-failure` message. See
[docs/release-process.md](../../docs/release-process.md#releaseyml-succeeded-but-the-chart-is-not-on-chartssupporttools).

**Secret errors** — check names against the table above; the most common cause is a workflow
looking for `DOCKER_*` while only `HARBOR_*` are configured.

## Status badge

```markdown
[![CI](https://github.com/supporttools/node-doctor/workflows/CI/badge.svg)](https://github.com/supporttools/node-doctor/actions)
```
