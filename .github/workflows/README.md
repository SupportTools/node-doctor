# GitHub Actions Workflows

This directory contains every GitHub Actions workflow for Node Doctor. There are three:

| File | Trigger | Purpose |
|---|---|---|
| `ci.yml` | PR to `main`, push to `main`, push of a `v*` tag | Lint, test, chart verification, build, gosec, and (on tags only) an amd64 Docker push + Grype scan |
| `ci-ipv6.yml` | Same, but path-filtered to network/exporter/integration code | IPv6 and dual-stack tests needing a kind cluster and `CAP_NET_RAW`. Deliberately separate so its flakes do not block `ci-success` |
| `release.yml` | Push of a `v*` tag **only** | The actual release: multi-arch images, GitHub Release, Helm chart publish |

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
7. **Docker Build & Push** — *tags only* (`if: startsWith(github.ref, 'refs/tags/v')`), needs
   `lint`, `test`, `build`, `security-gosec`. Builds **`linux/amd64` only** and pushes
   `docker.io/supporttools/node-doctor:<TAG>` (the tag verbatim, i.e. **with** the `v` prefix)
   plus `:latest`.
8. **Grype Scan (informational)** — tag builds only, `fail-build: false`, SARIF to the Security
   tab. Runs `anchore/scan-action` in-line rather than the org reusable workflow (see the
   comment in `ci.yml` for why).
9. **CI Success** — aggregate status check over `lint`, `test`, `build`, `security-gosec`,
   `helm-chart`.

### The tag-naming overlap with `release.yml`

On a `v*` tag push both workflows run and both push to `docker.io/supporttools/node-doctor`:

- `ci.yml` pushes `v1.9.0` — **amd64 only**, unsigned
- `release.yml` pushes `1.9.0` and `1.9` — **amd64 + arm64**, and cosign-signs `1.9.0`
- both push `latest`; whichever finishes last wins, and `ci.yml` has no prerelease gate, so an
  RC tag can leave `latest` pointing at amd64-only RC content

The Helm chart defaults `image.tag` to the `v`-prefixed form, which is the `ci.yml` one. Details
and the workaround are in
[docs/release-process.md](../../docs/release-process.md#which-image-tags-actually-exist).

## `release.yml`

Triggered only by pushing a tag matching `v*`. It runs **no tests** and does not depend on
`ci.yml`; a tag whose tests fail still produces a complete release.

Jobs: `docker`, `docker-overlay-test`, `github-release`, `helm-publish`, `release-complete`.
Full breakdown in [docs/release-process.md](../../docs/release-process.md#release-overview).

## Required GitHub Secrets

| Secret | Used by | Required |
|---|---|---|
| `DOCKER_USERNAME` | `ci.yml` docker job, `release.yml` docker + docker-overlay-test jobs | Yes, for any tag build |
| `DOCKER_PASSWORD` | same | Yes, for any tag build |
| `BOT_TOKEN` | `release.yml` `helm-publish` — checkout of and push to `SupportTools/helm-chart` | Yes, or the chart is never published |
| `CODECOV_TOKEN` | `ci.yml` Test job | Optional; upload is non-blocking |
| `GITHUB_TOKEN` | `release.yml` `github-release` | Provided automatically |

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
Opens lint, helm-chart, test, pinger-icmp, gosec and build jobs. No images are pushed.

### Push to `main`
Same set. Still no images — the docker job is gated on `refs/tags/v`.

### Release
```bash
git tag -a v1.9.0 -m "Release v1.9.0"
git push origin v1.9.0
```
Runs `ci.yml` (full pipeline + amd64 image + Grype) **and** `release.yml` (multi-arch images,
GitHub Release, Helm chart) in parallel. Watch both:

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

**Docker job skipped** — expected on PRs and `main` pushes; it only runs on `v*` tags.

**Docker job failed on a tag** — the `v`-prefixed image tag was not pushed. The multi-arch image
from `release.yml` still exists under the un-prefixed tag. See
[docs/release-process.md](../../docs/release-process.md#docker-pull-supporttoolsnode-doctorv190-says-not-found).

**Secret errors** — check names against the table above; the most common cause is a workflow
looking for `DOCKER_*` while only `HARBOR_*` are configured.

## Status badge

```markdown
[![CI](https://github.com/supporttools/node-doctor/workflows/CI/badge.svg)](https://github.com/supporttools/node-doctor/actions)
```
