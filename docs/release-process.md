# Node Doctor Release Process

This document describes what the Node Doctor release pipeline **actually does**, how to cut a
release, and how to roll one back.

Everything below is derived from `.github/workflows/release.yml`, `.github/workflows/ci.yml`,
`Makefile`, and `helm/node-doctor/*.template`. If you change one of those, change this document
in the same PR.

## Table of Contents

- [Release Overview](#release-overview)
- [Which Helm chart files actually ship](#which-helm-chart-files-actually-ship)
- [Which image tags actually exist](#which-image-tags-actually-exist)
- [Version Numbering](#version-numbering)
- [Release Types](#release-types)
- [Creating a Release](#creating-a-release)
- [Release Checklist](#release-checklist)
- [Signing and Verification](#signing-and-verification)
- [Rollback Procedures](#rollback-procedures)
- [Local Makefile targets (and their caveats)](#local-makefile-targets-and-their-caveats)
- [Troubleshooting](#troubleshooting)
- [What this pipeline does NOT do](#what-this-pipeline-does-not-do)

## Release Overview

A release is triggered by pushing a Git tag matching `v*`. Nothing else triggers it — there is no
manual dispatch, no branch trigger, no scheduled release.

```
git push origin v1.9.0
     |
     +--> .github/workflows/release.yml  (on: push: tags: v*)
     |      docker                -> docker.io/supporttools/node-doctor            (amd64+arm64)
     |      docker-overlay-test   -> docker.io/supporttools/node-doctor-overlay-test (amd64+arm64)
     |      github-release        -> GitHub Release + deployment/{rbac,daemonset}.yaml
     |      helm-publish          -> SupportTools/helm-chart repo -> https://charts.support.tools
     |      release-complete      -> fails the run if any of the above failed
     |
     +--> .github/workflows/ci.yml       (also fires on tags: v*)
            lint / test / build / gosec / helm-chart
            docker (tag builds only)  -> docker.io/supporttools/node-doctor:v<TAG> and :latest
                                         **linux/amd64 only**
            grype-scan (informational)
```

The two workflows are **independent**. `release.yml` does not run tests and does not wait for
`ci.yml`. A tag whose test suite fails still produces a complete release.

> This is not hypothetical. On tag `v1.8.7` the `ci.yml` `Test` job failed, which skipped
> `Build` and `Docker Build & Push` — while `release.yml` succeeded and published images, the
> GitHub Release, and Helm chart `1.8.7`. See
> [Which image tags actually exist](#which-image-tags-actually-exist) for the consequence.

**Registry**: Docker Hub (`docker.io/supporttools`). Not Harbor. `release.yml:15`, `ci.yml:15`
and `Makefile:39` all agree on this.

**Timeline**: `release.yml` takes roughly 30 seconds (warm buildx cache) to ~20 minutes (cold
cache, QEMU arm64). The `helm-publish` job additionally polls `charts.support.tools` for up to
5 minutes waiting for the chart to appear.

### The four release jobs in detail

| Job | Needs | What it does |
|---|---|---|
| `docker` | — | Multi-arch (`linux/amd64,linux/arm64`) build of `Dockerfile`, pushed to `docker.io/supporttools/node-doctor`. Tags from `docker/metadata-action`: `{{version}}`, `{{major}}.{{minor}}`, and `latest` (the last suppressed for `rc`/`beta`/`alpha` tags). Then signs **one** tag with keyless cosign. |
| `docker-overlay-test` | — | Same, for `Dockerfile.overlay-test` -> `docker.io/supporttools/node-doctor-overlay-test`. **No cosign signing at all.** |
| `github-release` | `docker`, `docker-overlay-test` | Creates the GitHub Release from a heredoc template. Attaches exactly two files: `deployment/rbac.yaml` and `deployment/daemonset.yaml`. Marked prerelease for `rc`/`beta`/`alpha` tags. |
| `helm-publish` | `docker` | Regenerates `Chart.yaml`/`values.yaml` from templates, lints, packages, commits the `.tgz` into `SupportTools/helm-chart` (using `secrets.BOT_TOKEN`), reindexes, then polls `https://charts.support.tools/` until the version appears (30 tries x 10s). |
| `release-complete` | all of the above | `if: always()`; re-checks each result and fails the run if any job did not succeed. |

## Which Helm chart files actually ship

**`helm/node-doctor/values.yaml` and `helm/node-doctor/Chart.yaml` are generated artifacts.
Never hand-edit them.**

Before packaging, `.github/workflows/release.yml` deletes both and re-renders them from the
`*.template` files:

```bash
rm -f helm/node-doctor/Chart.yaml helm/node-doctor/values.yaml
envsubst < helm/node-doctor/Chart.yaml.template  > helm/node-doctor/Chart.yaml
envsubst < helm/node-doctor/values.yaml.template > helm/node-doctor/values.yaml
```

So an edit made directly to `values.yaml` **never reaches the published chart**. It is not
rejected and it produces no warning — it is simply discarded at package time. `.helmignore`
excludes the `*.template` files, so the resulting tarball looks entirely normal.

This is not theoretical. Commit `8d555e8` (2026-02-11) raised the overlay-test CPU limit from
10m to 100m to stop CFS throttling that was inflating probe latency to 300-500ms. It edited
`values.yaml`. The change shipped in name only and the cluster ran the throttled 10m limit for
six months, while the *other* half of the same commit — which touched
`templates/configmap.yaml` — went live immediately.

**The rule that follows:** everything under `templates/` is packaged verbatim and behaves as
you expect. Only `values.yaml` and `Chart.yaml` are regenerated. A chart change's fate depends
entirely on which of those two groups it lands in.

### Workflow

1. Edit `helm/node-doctor/values.yaml.template` (or `Chart.yaml.template`).
2. Run `make helm-generate` to re-render the committed copies.
3. Commit both the template and the generated file.

`make helm-verify-generated` re-renders and diffs; it runs in CI as the `Helm Chart` job and
fails the build on any drift. `make helm-lint` depends on it, so a local lint catches this too.

The three variables `envsubst` substitutes are `CHART_VERSION`, `APP_VERSION` and `IMAGE_TAG`.
In CI they come from the tag (`CHART_VERSION` is the tag with the `v` stripped; the other two
are the tag verbatim). Locally, `make helm-generate` substitutes fixed placeholders
(`1.0.0` / `v1.0.0` / `v1.0.0`, see `Makefile:405-407`) purely so the committed copies are
byte-reproducible and diffable — those placeholder values are never what ships.

### Verifying a chart change actually shipped

Checking the live object is not sufficient — it cannot distinguish "the chart is stale" from
"the chart is fine but the object drifted". Check what Helm rendered:

```bash
helm -n node-doctor get manifest node-doctor | grep -A6 'Minimal resources'
```

If the rendered manifest still shows the old value, the published chart is stale regardless of
what `kubectl get ds` reports.

## Which image tags actually exist

Two different workflows push to `docker.io/supporttools/node-doctor` on the same tag push, and
they use **different tag naming**. This matters, because the Helm chart points at the tag
produced by the *less* reliable of the two.

| Tag on Docker Hub | Pushed by | Platforms | Signed |
|---|---|---|---|
| `1.9.0` (no `v`) | `release.yml` `docker` job | amd64 + arm64 | yes, keyless cosign |
| `1.9` (no `v`) | `release.yml` `docker` job | amd64 + arm64 | no |
| `latest` | `release.yml` (multi-arch, non-prerelease only) **and** `ci.yml` (amd64 only, any `v*` tag) | whichever job finished last | no |
| `v1.9.0` (with `v`) | `ci.yml` `docker` job only | **amd64 only** | no |

`helm/node-doctor/values.yaml.template` sets both `image.tag` and `overlayTest.image.tag` to
`${IMAGE_TAG}`, and `release.yml` sets `IMAGE_TAG=${{ github.ref_name }}` — i.e. the **`v`-prefixed**
form. Two consequences follow:

1. **The chart's default `image.tag` resolves to the amd64-only image.** The multi-arch image
   published by `release.yml` lives under the un-prefixed tag, which the chart never references.
2. **`supporttools/node-doctor-overlay-test:v<X.Y.Z>` has never existed.** `ci.yml` only builds
   the main image, so nothing ever pushes a `v`-prefixed overlay-test tag. The chart's
   `overlayTest.image.tag` default has always pointed at a tag that is not in the registry.

And when `ci.yml` fails on a tag, the `v`-prefixed tag is never pushed at all. That is the state
of `v1.8.7` today: chart `1.8.7` is published and defaults to `image.tag: "v1.8.7"`, but
`supporttools/node-doctor:v1.8.7` does not exist on Docker Hub. A default `helm install` of that
chart version pulls nothing.

**Until this is fixed in the chart template, always pin the image tag explicitly when installing
or upgrading**, using the un-prefixed, multi-arch, cosign-signed tag:

```bash
helm upgrade --install node-doctor supporttools/node-doctor \
  --namespace node-doctor --create-namespace \
  --version 1.8.7 \
  --set image.tag=1.8.7 \
  --set overlayTest.image.tag=1.8.7
```

Verify before you install:

```bash
# The tag you are about to deploy must exist AND be multi-arch if you have arm64 nodes
docker buildx imagetools inspect docker.io/supporttools/node-doctor:1.8.7
docker buildx imagetools inspect docker.io/supporttools/node-doctor-overlay-test:1.8.7
```

## Version Numbering

Node Doctor follows [Semantic Versioning 2.0.0](https://semver.org/):

```
v{MAJOR}.{MINOR}.{PATCH}[-{PRERELEASE}]

Examples:
  v1.0.0         - Stable release
  v1.2.3         - Stable release with patches
  v2.0.0-rc.1    - Release candidate
  v1.5.0-beta.2  - Beta release
```

### Version Components

- **MAJOR**: Incompatible API changes, breaking changes
- **MINOR**: New features, backward-compatible
- **PATCH**: Bug fixes, backward-compatible
- **PRERELEASE**: `-rc.N`, `-beta.N`, `-alpha.N`

The Helm chart version is always the Git tag with the leading `v` removed
(`release.yml:236-238`). Chart version and app version are never independent.

### When to Increment

| Change Type | Version Increment | Example |
|------------|-------------------|---------|
| Breaking change to config format | MAJOR | v1.5.2 -> v2.0.0 |
| New monitor type added | MINOR | v1.5.2 -> v1.6.0 |
| Bug fix in existing monitor | PATCH | v1.5.2 -> v1.5.3 |
| Security vulnerability fix | PATCH | v1.5.2 -> v1.5.3 |
| Chart-only change (`values.yaml.template`, `templates/`) | PATCH | v1.5.2 -> v1.5.3 |
| Documentation update only | None | No release |

There is no way to publish a chart without cutting a version tag — `helm-publish` only runs on
tag push, and `make helm-publish` is a stub that does nothing (see
[Local Makefile targets](#local-makefile-targets-and-their-caveats)).

## Release Types

### Stable release

**Tag**: `v{MAJOR}.{MINOR}.{PATCH}`, e.g. `v1.9.0`

Gets `latest` on both images, gets `{{major}}.{{minor}}` tags, and the GitHub Release is not
marked prerelease.

### Prerelease (rc / beta / alpha)

**Tag**: `v{MAJOR}.{MINOR}.{PATCH}-rc.{N}` / `-beta.{N}` / `-alpha.{N}`

`release.yml` detects these by substring match on the tag name
(`contains(github.ref_name, 'rc')` etc.). For these tags:

- `latest` is **not** pushed by `release.yml`
- `docker/metadata-action` does not emit the `{{major}}.{{minor}}` tag
- the GitHub Release is marked `prerelease: true`
- the Helm chart **is still published** to `charts.support.tools` (there is no prerelease gate
  on `helm-publish`), as chart version e.g. `1.9.0-rc.1`

Two sharp edges in that substring match:

- `ci.yml` has no such gate. It pushes `:latest` (amd64-only) on **every** `v*` tag, including
  prereleases. Pushing an RC tag can therefore leave `latest` pointing at RC content.
- The match is a plain substring on the whole tag name. Any tag containing the letters `rc`
  anywhere — including in a suffix you did not intend as a prerelease marker — is treated as a
  prerelease.

## Creating a Release

### Prerequisites

```bash
# 1. Working tree clean, on main, up to date
git status
git branch --show-current   # main
git pull origin main

# 2. Tests pass locally. release.yml does NOT run tests, so this is your only real gate.
make test-ci

# 3. Chart generated files match their templates (CI 'Helm Chart' job enforces this)
make helm-verify-generated

# 4. CI is green on the commit you are about to tag
gh run list --branch main --limit 3
```

Step 4 is not optional. If `ci.yml` is red on the commit, `release.yml` will still publish a
release — and the `v`-prefixed image tag the chart defaults to will be missing.

### Cut the tag

```bash
NEW_VERSION="v1.9.0"

git tag -a "$NEW_VERSION" -m "Release $NEW_VERSION"
git push origin "$NEW_VERSION"

gh run watch
```

### Verify the release

```bash
VER=v1.9.0
CHART=${VER#v}

# 1. Both workflows
gh run list --workflow=release.yml --limit 3
gh run list --workflow=ci.yml --limit 3

# 2. GitHub Release exists and carries the two manifests
gh release view "$VER"
gh release view "$VER" --json assets --jq '.assets[].name'
#   expected: rbac.yaml, daemonset.yaml   (there are no binaries and no checksums.txt)

# 3. Images. The un-prefixed tag is the multi-arch one.
docker buildx imagetools inspect docker.io/supporttools/node-doctor:$CHART
docker buildx imagetools inspect docker.io/supporttools/node-doctor-overlay-test:$CHART

# 4. Cosign signature (only on the un-prefixed node-doctor tag)
cosign verify docker.io/supporttools/node-doctor:$CHART \
  --certificate-identity-regexp="https://github.com/supporttools/node-doctor" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"

# 5. Helm chart published
helm repo add supporttools https://charts.support.tools
helm repo update
helm search repo supporttools/node-doctor --version "$CHART"
```

## Release Checklist

### Pre-release

- [ ] All planned changes merged to `main`
- [ ] `make test-ci` passes locally
- [ ] `make helm-verify-generated` passes (chart files match templates)
- [ ] Chart changes were made in `*.template`, not in the generated `values.yaml`/`Chart.yaml`
- [ ] `ci.yml` green on the commit being tagged
- [ ] Docs updated (README, configuration, monitors)
- [ ] Breaking changes documented

### Release day

- [ ] Create and push the annotated tag
- [ ] `gh run watch` — watch **both** `release.yml` and `ci.yml`
- [ ] GitHub Release created, with `rbac.yaml` + `daemonset.yaml` attached
- [ ] `supporttools/node-doctor:<X.Y.Z>` exists and lists both amd64 and arm64
- [ ] `supporttools/node-doctor-overlay-test:<X.Y.Z>` exists and lists both amd64 and arm64
- [ ] `cosign verify` passes on `supporttools/node-doctor:<X.Y.Z>`
- [ ] Chart `<X.Y.Z>` visible via `helm search repo supporttools/node-doctor`
- [ ] If `ci.yml` failed: know that `supporttools/node-doctor:v<X.Y.Z>` is missing and pin
      `image.tag` explicitly on any install/upgrade

### Post-release

- [ ] Deploy and watch the DaemonSet roll (`kubectl -n node-doctor rollout status ds/node-doctor`)
- [ ] Monitor for 24h
- [ ] Update TaskForge / close related issues

## Signing and Verification

**What is signed:** exactly one thing — the `docker.io/supporttools/node-doctor:<X.Y.Z>` image
tag (`v` stripped), with keyless cosign via GitHub OIDC (`release.yml:65-80`).

**What is not signed:**

- the `{{major}}.{{minor}}` tag (e.g. `1.9`)
- the `latest` tag
- the `v`-prefixed tag pushed by `ci.yml`
- the `node-doctor-overlay-test` image, on any tag
- the GitHub Release assets (`rbac.yaml`, `daemonset.yaml`)
- the packaged Helm chart

There is **no GPG signing anywhere in the pipeline.** `release.yml` contains no `GPG_PRIVATE_KEY`
step, no `crazy-max/ghaction-import-gpg`, and no `.asc` output. If you previously set a
`GPG_PRIVATE_KEY` repository secret, nothing consumes it — it should be removed and the key
material revoked, because an unused secret is pure attack surface.

`scripts/setup-gpg-signing-subkey.sh` and `scripts/verify-gpg-setup.sh` are left over from a
signing scheme that was never wired into the workflow. Running them configures nothing.
Likewise `.goreleaser.yml` exists in the repo root but **is never invoked** — no workflow runs
GoReleaser, so it produces no binaries, no archives, and no `checksums.txt`.

### Verifying an image

```bash
cosign verify docker.io/supporttools/node-doctor:1.9.0 \
  --certificate-identity-regexp="https://github.com/supporttools/node-doctor" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"
```

Note this is `cosign verify` (image), not `cosign verify-blob` — there are no signed blobs.

## Rollback Procedures

There is **no rollback workflow.** `.github/workflows/` contains only `ci.yml`, `ci-ipv6.yml`
and `release.yml`. Roll back with Helm.

### Primary: `helm rollback`

```bash
# 1. Find the revision to go back to
helm -n node-doctor history node-doctor

# REVISION  UPDATED       STATUS      CHART              APP VERSION  DESCRIPTION
# 4         ...           superseded  node-doctor-1.7.2  v1.7.2       Upgrade complete
# 5         ...           deployed    node-doctor-1.8.7  v1.8.7       Upgrade complete

# 2. Roll back
helm rollback node-doctor -n node-doctor 4

# 3. Watch the DaemonSet re-roll
kubectl -n node-doctor rollout status daemonset/node-doctor --timeout=5m
```

**The non-obvious part that makes this reliable:** Helm v3 stores the *entire rendered chart*,
including its values, inside the release Secret for each revision
(`sh.helm.release.v1.node-doctor.v<REVISION>` in the release namespace). `helm rollback` replays
that stored copy. It does **not** re-fetch the chart from `charts.support.tools`.

That means rollback works even to a chart version that was never published to the chart
repository. This is directly relevant here: the `helm-publish` job failed on every tag from
`v1.8.0` through `v1.8.6`, so chart versions `1.8.0`-`1.8.6` do not exist at
`charts.support.tools` at all — `helm search repo` will not find them and
`helm upgrade --version 1.8.4` will fail. But if a cluster was ever upgraded to one of those
revisions (for example via a local `helm upgrade ./helm/node-doctor`), `helm rollback` to that
revision still works, because the chart came out of the cluster, not the repo.

Corollary: **do not delete the release history.** `helm history` depth is your rollback range.
Anything pruned by `--history-max` is only recoverable if the chart version is also published.

### Confirming a rollback landed

```bash
# What Helm believes is deployed
helm -n node-doctor list

# What is actually running
kubectl -n node-doctor get ds node-doctor \
  -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'

# What the rendered chart says (catches "chart is stale" vs "object drifted")
helm -n node-doctor get manifest node-doctor | grep -A6 'Minimal resources'
```

### If the cluster was deployed with raw manifests

Deployments made with `kubectl apply -f deployment/daemonset.yaml` (or `make deploy-prd-kubectl`)
are not Helm releases and have no `helm history`. Roll those back with:

```bash
kubectl -n node-doctor rollout undo daemonset/node-doctor
```

Note that `deployment/daemonset.yaml` in the repo pins a hardcoded image
(`supporttools/node-doctor:v1.5.4` as of writing) that is **not** bumped by the release
pipeline. The manifests attached to a GitHub Release therefore do not necessarily reference that
release's image. Treat them as an example, not as a release artifact, and set the image tag
yourself.

### Retiring a bad release

There is no automation for this. Do it by hand:

```bash
# Mark the GitHub Release as a prerelease and warn in the notes
gh release edit v1.9.0 --prerelease \
  --notes "**DO NOT USE** - <reason>. Use v1.8.7 instead."

# Or hide it entirely (artifacts preserved, can be undone)
gh release edit v1.9.0 --draft

# Or remove it (cannot be undone; the Git tag survives separately)
gh release delete v1.9.0 --yes
git push origin :refs/tags/v1.9.0
git tag -d v1.9.0
```

Deleting a tag does **not** remove the published Docker images or the Helm chart. If the bad
version must be unreachable, delete the tags from Docker Hub and revert the chart commit in
`SupportTools/helm-chart` as separate manual steps.

## Local Makefile targets (and their caveats)

`make help` lists the full set. These are the ones with behaviour that will surprise you.

### `VERSION` defaults to an epoch timestamp

`Makefile:42` sets `VERSION := $(shell date +%s)`. Every target that interpolates `$(VERSION)`
without an override gets an integer like `1786... `, which is never a published image tag.

```bash
make deploy-dev    # helm upgrade --install ... --set image.tag=1786132891
make deploy-stg    # same
make deploy-prd    # same, behind a y/n prompt
```

**These three targets are broken as written.** They deploy the local chart with an image tag that
does not exist in any registry, producing `ImagePullBackOff`. If you use them, always override:

```bash
make deploy-dev VERSION=1.8.7
```

They also pass `--set environment=<env>`, a value the chart templates do not consume.

`make build-*-image` / `push-*-image` have the same `VERSION` default; locally built images are
tagged with the epoch and with `latest`. Pushing those tags to `docker.io/supporttools`
overwrites the registry `latest` with a local build — avoid `make docker-push` unless that is
explicitly what you want.

### `make helm-publish` does nothing

`Makefile:443-446` packages the chart and then hits a `# CUSTOMIZE: Add your Helm chart publish
command` placeholder before printing "Helm chart published". It publishes nothing. The only path
to `charts.support.tools` is the `helm-publish` job in `release.yml`, i.e. a tag push.

### `make bump-rc` is a direct-to-production path

`Makefile:533-571` increments `.version-rc`, builds and pushes a multi-arch image, rewrites the
image line in `deployment/daemonset.yaml` with `sed`, applies it to the `a1-ops-prd` cluster,
then commits, tags and **pushes to `main` with `--tags`**. Pushing the tag re-triggers the full
`release.yml`. Its console output claims it pushes "to Harbor"; it pushes to Docker Hub
(`REGISTRY := docker.io/supporttools`). Understand all of that before running it.

### Useful and accurate

```bash
make test-ci               # unit (-short) + integration; the closest local mirror of CI's gate
make helm-generate         # re-render Chart.yaml/values.yaml from *.template
make helm-verify-generated # diff them; fails on drift (this is the CI 'Helm Chart' job)
make helm-lint             # depends on helm-verify-generated
make validate-pipeline-local
```

## Troubleshooting

### `release.yml` succeeded but the chart is not on charts.support.tools

The `helm-publish` job is the fragile one — it failed on every tag from `v1.8.0` to `v1.8.6`.
Check that job specifically:

```bash
gh run list --workflow=release.yml --limit 5
gh run view <run-id> --log --job "Publish Helm Chart"
```

Common causes:

- `secrets.BOT_TOKEN` expired or lacks write access to `SupportTools/helm-chart`
- `git push` in the "Update Helm repository" step raced another chart publish and was rejected
- the "Verify Chart Availability" step timed out (30 x 10s) because the static site had not
  rebuilt yet — in that case the chart *was* committed; re-check `helm repo update` before
  re-cutting a tag
- `helm lint` failed on a `values.yaml.template` change (this is the one CI's `Helm Chart` job
  would have caught first)

Note `release-complete` turns a `helm-publish` failure into a red run, but the `docker` and
`github-release` jobs have already published by then. A failed release is a **partial** release.

### `docker pull supporttools/node-doctor:v1.9.0` says not found

That `v`-prefixed tag comes from `ci.yml`, not `release.yml`. If `ci.yml` failed on the tag —
most often at the `Test` job, which skips `Build` and `Docker Build & Push` — the tag was never
pushed. The multi-arch image still exists under the un-prefixed `1.9.0`. See
[Which image tags actually exist](#which-image-tags-actually-exist).

```bash
gh run list --workflow=ci.yml --branch v1.9.0
gh run view <run-id> --json jobs --jq '.jobs[] | "\(.conclusion)\t\(.name)"'
```

### arm64 nodes stuck in `ImagePullBackOff`

Almost certainly the `v`-prefixed, amd64-only tag. Pin the un-prefixed tag:

```bash
helm upgrade node-doctor supporttools/node-doctor -n node-doctor \
  --reuse-values --set image.tag=1.9.0 --set overlayTest.image.tag=1.9.0
```

### A chart change did not take effect

You almost certainly edited `helm/node-doctor/values.yaml` instead of
`helm/node-doctor/values.yaml.template`. See
[Which Helm chart files actually ship](#which-helm-chart-files-actually-ship). `make
helm-verify-generated` catches the reverse case (template edited, generated file not regenerated);
it does not catch a hand-edit to `values.yaml` that you then propagated into the template — so
check `git diff` covers both files.

### Cosign verification fails

- Use `cosign verify` (image), not `cosign verify-blob` — no blobs are signed.
- Only the un-prefixed `<X.Y.Z>` tag on `node-doctor` is signed. `latest`, `{{major}}.{{minor}}`,
  the `v`-prefixed tag, and the entire overlay-test image are unsigned.
- The workflow signs with `cosign` v2.2.2 and `COSIGN_EXPERIMENTAL=1`; the signature lives in the
  registry as a `sha256-<digest>.sig` tag.

### Retagging an existing version

Force-moving a tag re-runs both workflows and republishes over the existing images. The
`helm-publish` job will then fail at `git commit` (nothing to commit) or push a duplicate chart
version, which `helm repo index` handles badly. Prefer cutting a new patch version over moving a
tag.

## What this pipeline does NOT do

Listed explicitly because previous versions of this document claimed otherwise:

- **No GoReleaser.** `.goreleaser.yml` exists in the repo root but no workflow invokes it.
- **No binary artifacts.** No `.tar.gz` archives, no per-platform binaries, no `checksums.txt`.
  The GitHub Release carries two YAML manifests and nothing else.
- **No GPG signing.** No `GPG_PRIVATE_KEY` usage, no `.asc` files, no dual-layer signing.
- **No Harbor.** The registry is Docker Hub throughout.
- **No `rollback-release.yml`.** No rollback workflow of any kind exists; every
  `gh workflow run rollback-release.yml` command will fail.
- **No changelog generation.** The release notes are a static heredoc in `release.yml:157-198`;
  they are identical for every release apart from the version string.
- **No tests.** `release.yml` runs none, and does not depend on `ci.yml`.

## Support

1. Check this document
2. `gh run list` / `gh run view --log`
3. Open an issue: https://github.com/supporttools/node-doctor/issues

## References

- [Semantic Versioning](https://semver.org/)
- [Cosign Documentation](https://docs.sigstore.dev/cosign/overview/)
- [Helm rollback](https://helm.sh/docs/helm/helm_rollback/)
- [GitHub Actions Workflows](../.github/workflows/)
- [Node Doctor Architecture](./architecture.md)
- [Deployment Guide](./deployment.md)
