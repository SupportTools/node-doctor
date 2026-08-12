# Node Doctor Release Process

This document describes what the Node Doctor release pipeline **actually does**, how to cut a
release, and how to roll one back.

Everything below is derived from `.github/workflows/release.yml`, `.github/workflows/ci.yml`,
`Makefile`, and `helm/node-doctor/*.template`. If you change one of those, change this document
in the same PR.

## Table of Contents

- [Release Overview](#release-overview)
- [Release credentials](#release-credentials)
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
     +--> .github/workflows/release.yml  (on: push: tags: v*)   <-- publishes EVERYTHING
     |      docker                -> docker.io/supporttools/node-doctor            (amd64+arm64)
     |      docker-overlay-test   -> docker.io/supporttools/node-doctor-overlay-test (amd64+arm64)
     |      grype-scan            -> SARIF -> GitHub Security (informational)
     |      github-release        -> GitHub Release + deployment/{rbac,daemonset}.yaml
     |      helm-publish          -> SupportTools/helm-chart repo -> https://charts.support.tools
     |      release-complete      -> fails the run if any of the above failed
     |      notify-failure        -> Slack, if anything above failed
     |
     +--> .github/workflows/ci.yml       (also fires on tags: v*)
            lint / test / build / gosec / helm-chart / docker / ci-success
            **publishes nothing** — its docker job builds with push:false

daily 13:00 UTC
     +--> .github/workflows/release-audit.yml
            newest git tag vs newest chart on charts.support.tools -> Slack on mismatch
```

**`release.yml` is the sole publisher of container images.** `ci.yml` used to have a
tag-gated `docker` job that pushed the same repository on the same tag event, with no ordering
between the two — see [Which image tags actually exist](#which-image-tags-actually-exist) for
what that cost. Its `docker` job now builds with `push: false` on every PR and branch push, so
the Dockerfile is validated earlier than it used to be and can never race for a tag. Nothing
outside `release.yml` may move `:latest`.

The two workflows are still **independent**. `release.yml` does not run tests and does not wait
for `ci.yml`. A tag whose test suite fails still produces a complete release.

> This is not hypothetical. On tag `v1.8.7` the `ci.yml` `Test` job failed, which skipped
> `Build` and the then-existing `Docker Build & Push` job — while `release.yml` succeeded and
> published images, the GitHub Release, and Helm chart `1.8.7`. The *tag-naming* consequence
> that had is now fixed; the underlying "a release does not depend on tests passing" property
> is not. Cutting a tag on a red commit is still entirely your own foot.

**Registry**: Docker Hub (`docker.io/supporttools`). Not Harbor. `release.yml:15`, `ci.yml:15`
and `Makefile:39` all agree on this.

**Timeline**: `release.yml` takes roughly 30 seconds (warm buildx cache) to ~20 minutes (cold
cache, QEMU arm64). The `helm-publish` job additionally polls `charts.support.tools` for up to
5 minutes waiting for the chart to appear.

### The release jobs in detail

| Job | Needs | What it does |
|---|---|---|
| `docker` | — | Resolves the release version once — `github.ref_name` with the `v` stripped, pinned to a safe charset and exported as a job output, so no other job re-derives it from the raw ref. Multi-arch (`linux/amd64,linux/arm64`) build of `Dockerfile`, pushed to `docker.io/supporttools/node-doctor`. Tags from `docker/metadata-action`: `{{version}}`, `{{major}}.{{minor}}`, and `latest` (the last suppressed for `rc`/`beta`/`alpha` tags). Then signs **one** tag with keyless cosign. |
| `docker-overlay-test` | — | Same, for `Dockerfile.overlay-test` -> `docker.io/supporttools/node-doctor-overlay-test`. **No cosign signing at all.** |
| `grype-scan` | `docker` | Scans the published image with `anchore/scan-action` and uploads SARIF to GitHub Security. Informational: `fail-build: false`, never gates a release. Lives here rather than in `ci.yml` because it needs a *published* image. |
| `github-release` | `docker`, `docker-overlay-test` | Creates the GitHub Release from a heredoc template. Attaches exactly two files: `deployment/rbac.yaml` and `deployment/daemonset.yaml`. Marked prerelease for `rc`/`beta`/`alpha` tags. |
| `helm-publish` | `docker`, `docker-overlay-test` | Regenerates `Chart.yaml`/`values.yaml` from templates, lints, packages, **verifies every image the packaged chart references actually exists in the registry**, then commits the `.tgz` into `SupportTools/helm-chart` (using `secrets.BOT_TOKEN`), reindexes, and polls `https://charts.support.tools/` until the version appears (30 tries x 10s). It waits on `docker-overlay-test` so both images exist by the time that verification runs. |
| `release-complete` | `docker`, `docker-overlay-test`, `github-release`, `helm-publish` | `if: always()`; re-checks each result and fails the run if any job did not succeed. |
| `notify-failure` | all of the above | `if: failure()`; posts the per-job results and a run link to Slack. See [Release failure is a signal, not a colour](#release-failure-is-a-signal-not-a-colour). |

### Release failure is a signal, not a colour

A red release run is not, by itself, something anyone receives.

`helm-publish`'s `Verify Chart Availability` step polls `charts.support.tools` and hard-fails
when the chart never appears. It did exactly that, correctly, on every tag from `v1.8.0` to
`v1.8.6` — and across eight repositories for six months nobody noticed, because nothing pushed
the failure anywhere a human looks. The detection was never missing. The delivery was.

Two things now surface it:

1. **`notify-failure`** in `release.yml` — `if: failure()`, posts to Slack via the organization
   `SLACK_WEBHOOK_URL` webhook with the per-job results and a link to the run. It states the
   consequence explicitly ("charts.support.tools still serves the previous version") so the
   message is actionable without opening the run.
2. **`.github/workflows/release-audit.yml`** — a daily scheduled job (13:00 UTC, plus
   `workflow_dispatch`) comparing the newest `v*` git tag against the chart version actually
   served by `charts.support.tools`, with a one-hour grace period for an in-flight release.
   This catches the whole class rather than one cause: a release run that was never started,
   was cancelled, was re-run into a green state, or whose notification failed to deliver still
   shows up the next morning.

If you add a new failure mode to the release path, make sure it lands in one of those two.

## Release credentials

**`BOT_TOKEN` is the sole chart-publish credential.** It is an organization secret used at
exactly one place — the `Checkout helm-chart repository` step of `helm-publish` — to check out
and push to `SupportTools/helm-chart`. If charts stop publishing, this is the credential to
check first; an expired `BOT_TOKEN` presents as `helm-publish` failing at checkout or at
`git push`.

| Secret | Used by node-doctor | Purpose |
|---|---|---|
| `BOT_TOKEN` | `release.yml` (`helm-publish`) | push the packaged chart to `SupportTools/helm-chart` |
| `DOCKER_USERNAME` / `DOCKER_PASSWORD` | `release.yml`, `ci.yml` | Docker Hub push (release) and authenticated base-image pulls (CI) |
| `SLACK_WEBHOOK_URL` | `release.yml`, `release-audit.yml` | failure notifications |
| `CODECOV_TOKEN` | `ci.yml` | coverage upload (informational) |

### `HELM_CHART_PAT` is not this repo's credential

The organization secret `HELM_CHART_PAT` (created 2023-10-27, visibility: all) looks like it
ought to be the chart-publishing credential, and it is not — **no node-doctor workflow
references it.** Do not rotate it, do not extend its expiry, and do not investigate it when a
node-doctor chart fails to publish. That is a dead end; the credential in play is `BOT_TOKEN`.

It is *not* org-wide dead, though. An org-wide scan finds 13 repositories publishing to
`SupportTools/helm-chart`: 12 use `BOT_TOKEN`, and `SupportTools/powerdns-admin-proxy`
(`.github/workflows/build.yml`) still uses `HELM_CHART_PAT`. So it cannot simply be deleted —
that repository would break. Any cleanup means migrating `powerdns-admin-proxy` to `BOT_TOKEN`
first. Treat `HELM_CHART_PAT` as "in use by exactly one legacy consumer, out of scope for
node-doctor".

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
In CI they come from the tag: **`CHART_VERSION` and `IMAGE_TAG` are the tag with the `v`
stripped; only `APP_VERSION` is the tag verbatim.** Locally, `make helm-generate` substitutes
fixed placeholders (`1.0.0` / `v1.0.0` / `1.0.0`, see the `HELM_PLACEHOLDER_*` variables in the
`Makefile`) purely so the committed copies are byte-reproducible and diffable — those
placeholder values are never what ships. The placeholders mirror the same `v`-stripped/`v`-kept
asymmetry as CI on purpose; see
[Which image tags actually exist](#which-image-tags-actually-exist).

### Verifying a chart change actually shipped

Checking the live object is not sufficient — it cannot distinguish "the chart is stale" from
"the chart is fine but the object drifted". Check what Helm rendered:

```bash
helm -n node-doctor get manifest node-doctor | grep -A6 'Minimal resources'
```

If the rendered manifest still shows the old value, the published chart is stale regardless of
what `kubectl get ds` reports.

## Which image tags actually exist

**Image tags are `v`-stripped. `appVersion` keeps the `v`. That asymmetry is deliberate.**

`docker/metadata-action` publishes `type=semver,pattern={{version}}`, which strips the leading
`v`. The image on Docker Hub is `1.9.0`. **There is no `v1.9.0`, and nothing publishes one.**

| Tag on Docker Hub | Pushed by | Platforms | Signed |
|---|---|---|---|
| `1.9.0` (no `v`) | `release.yml` `docker` job | amd64 + arm64 | yes, keyless cosign |
| `1.9` (no `v`) | `release.yml` `docker` job | amd64 + arm64 | no |
| `latest` | `release.yml` only, non-prerelease tags only | amd64 + arm64 | no |
| `v1.9.0` (with `v`) | **nothing — this tag does not exist** | — | — |

`helm/node-doctor/values.yaml.template` sets both `image.tag` and `overlayTest.image.tag` to
`${IMAGE_TAG}`, and `release.yml` sets `IMAGE_TAG` to the `v`-stripped version, so the chart's
defaults resolve to the multi-arch, cosign-signed images. `APP_VERSION` — which becomes
`Chart.yaml` `appVersion` — keeps the `v`, because it is a human-facing release name and not a
registry reference. **Do not "fix" one to match the other.**

### How this used to be broken

`IMAGE_TAG` was previously `github.ref_name`, i.e. `v1.9.0`, so every published chart defaulted
to image tags that had never been pushed:

- **`supporttools/node-doctor-overlay-test:v<X.Y.Z>` has never existed for any version.**
  `release.yml` is its only publisher and has always pushed `v`-stripped, so the chart's
  `overlayTest.image.tag` default pointed at a tag that was not in the registry — always.
- The main image's `v`-prefixed tag existed only when `ci.yml`'s duplicate, amd64-only `docker`
  job happened to win a race against `release.yml`. When `ci.yml` failed on a tag, it was not
  pushed at all. That is the state of `v1.8.7`: chart `1.8.7` defaults to `image.tag: "v1.8.7"`,
  which does not exist on Docker Hub.

Removing `ci.yml` as a publisher would have removed the last thing producing the `v`-prefixed
main-image tag, so the two changes had to ship together.

**Consequence for older releases: chart versions up to and including `1.8.7` still carry the
broken `v`-prefixed defaults, and no fix can retroactively change an already-published chart.**
If you install one of those versions, pin the tags explicitly:

```bash
# Only needed for chart 1.8.7 and earlier
helm upgrade --install node-doctor supporttools/node-doctor \
  --namespace node-doctor --create-namespace \
  --version 1.8.7 \
  --set image.tag=1.8.7 \
  --set overlayTest.image.tag=1.8.7
```

From the first release cut after this change, the chart defaults are correct and no `--set` is
required.

### The guard that keeps it fixed

`helm-publish` will not publish a chart whose images do not exist. After `helm package`, a
`Verify packaged chart references images that exist` step reads `image.repository`/`image.tag`
and `overlayTest.image.*` back out of the **packaged tarball** — not the working tree, so it
checks exactly what ships — and runs `docker manifest inspect` on each. Any reference that does
not resolve fails the release before anything reaches `charts.support.tools`.

That guard is not specific to the `v` prefix: it catches any future drift between what the
`docker` jobs push and what the chart defaults to.

Verify by hand before you install:

```bash
# The tag you are about to deploy must exist AND be multi-arch if you have arm64 nodes
docker buildx imagetools inspect docker.io/supporttools/node-doctor:1.9.0
docker buildx imagetools inspect docker.io/supporttools/node-doctor-overlay-test:1.9.0

# What the published chart actually defaults to
helm show values supporttools/node-doctor --version 1.9.0 | grep -A3 '^image:'
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

The Helm chart version is always the Git tag with the leading `v` removed (resolved once in the
`docker` job and passed to `helm-publish` as a job output). Chart version and app version are
never independent.

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

One sharp edge remains in that substring match: it is a plain substring on the whole tag name.
Any tag containing the letters `rc` anywhere — including in a suffix you did not intend as a
prerelease marker — is treated as a prerelease.

`ci.yml` used to defeat this exclusion entirely: it had no prerelease gate and pushed `:latest`
(amd64-only) on **every** `v*` tag, so cutting an RC could leave `latest` pointing at RC
content, on one architecture. `ci.yml` no longer pushes anything, so `release.yml`'s exclusion
is now the only thing that decides `:latest`. `make bump-rc` likewise no longer tags `:latest`.

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

Step 4 is not optional. `release.yml` runs no tests and does not wait for `ci.yml`, so a red
commit still produces a complete, published release — images, GitHub Release and chart. CI being
green on the exact commit you tag is the only gate that exists.

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

# 3. Images. Tags are v-stripped; both must list amd64 AND arm64.
docker buildx imagetools inspect docker.io/supporttools/node-doctor:$CHART
docker buildx imagetools inspect docker.io/supporttools/node-doctor-overlay-test:$CHART

# 4. Cosign signature (only on the exact X.Y.Z node-doctor tag)
cosign verify docker.io/supporttools/node-doctor:$CHART \
  --certificate-identity-regexp="https://github.com/supporttools/node-doctor" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"

# 5. Helm chart published
helm repo add supporttools https://charts.support.tools
helm repo update
helm search repo supporttools/node-doctor --version "$CHART"

# 6. The published chart defaults to tags that exist (helm-publish enforces this,
#    but confirm once after any change to the image/tag plumbing)
helm show values supporttools/node-doctor --version "$CHART" | grep -E '^\s+tag:'
#   expected: the v-STRIPPED version, e.g. "1.9.0" — never "v1.9.0"
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
- [ ] Published chart's `image.tag` / `overlayTest.image.tag` default to the **v-stripped**
      version (`helm show values`)
- [ ] Release run is green — and if it is not, confirm the Slack failure notification arrived

### Post-release

- [ ] Deploy and watch the DaemonSet roll (`kubectl -n node-doctor rollout status ds/node-doctor`)
- [ ] Monitor for 24h
- [ ] Update TaskForge / close related issues

## Signing and Verification

**What is signed:** exactly one thing — the `docker.io/supporttools/node-doctor:<X.Y.Z>` image
tag (`v` stripped), with keyless cosign via GitHub OIDC (the `Sign Docker image` step of the
`docker` job).

**What is not signed:**

- the `{{major}}.{{minor}}` tag (e.g. `1.9`)
- the `latest` tag
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

There is **no rollback workflow.** `.github/workflows/` contains only `ci.yml`, `ci-ipv6.yml`,
`release.yml` and `release-audit.yml`. Roll back with Helm.

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

The `helm-publish` target packages the chart and then hits a `# CUSTOMIZE: Add your Helm chart publish
command` placeholder before printing "Helm chart published". It publishes nothing. The only path
to `charts.support.tools` is the `helm-publish` job in `release.yml`, i.e. a tag push.

### `make bump-rc` is a direct-to-production path

It increments `.version-rc`, builds and pushes a multi-arch image, rewrites the image line in
`deployment/daemonset.yaml` with `sed`, applies it to the `a1-ops-prd` cluster, then commits,
tags and **pushes to `main` with `--tags`**. Pushing the tag re-triggers the full `release.yml`.
Understand all of that before running it.

Two things about it were fixed alongside the tag-naming work, worth knowing if you remember the
old behaviour:

- It used to also tag `:latest`, so a workstation build of a *release candidate* could overwrite
  the `:latest` that `release.yml` publishes only for stable tags. It now pushes only the RC tag.
- Its `git commit` / `git tag` / `git push` steps used to end in `|| true`, silently swallowing
  failures — leaving an image pushed and a cluster deployed from a revision that was never
  tagged or pushed anywhere. They now fail loudly.

Its console output used to claim it pushes "to Harbor"; it pushes to Docker Hub
(`REGISTRY := docker.io/supporttools`), and the strings now say so.

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

- `secrets.BOT_TOKEN` expired or lacks write access to `SupportTools/helm-chart`. This is the
  sole chart-publish credential — see [Release credentials](#release-credentials)
- `git push` in the "Update Helm repository" step raced another chart publish and was rejected
- the "Verify Chart Availability" step timed out (30 x 10s) because the static site had not
  rebuilt yet — in that case the chart *was* committed; re-check `helm repo update` before
  re-cutting a tag
- `helm lint` failed on a `values.yaml.template` change (this is the one CI's `Helm Chart` job
  would have caught first)
- the "Verify packaged chart references images that exist" step rejected the chart because a
  default image tag does not resolve in the registry — see
  [Which image tags actually exist](#which-image-tags-actually-exist). This one fails *before*
  the chart is committed, so nothing was published and nothing needs cleaning up

Note `release-complete` turns a `helm-publish` failure into a red run, but the `docker` and
`github-release` jobs have already published by then. A failed release is a **partial** release:
the tag, the images and the GitHub Release exist while `charts.support.tools` still serves the
previous version. Nothing rolls that back automatically.

### `docker pull supporttools/node-doctor:v1.9.0` says not found

Working as intended — **no `v`-prefixed image tag exists, for any version.** Drop the `v`:

```bash
docker pull supporttools/node-doctor:1.9.0
```

Historically `ci.yml` pushed a `v`-prefixed, amd64-only tag; it no longer publishes anything.
See [Which image tags actually exist](#which-image-tags-actually-exist).

### `ImagePullBackOff` after installing the chart

If you installed chart **1.8.7 or earlier**, its defaults point at `v`-prefixed tags that do not
exist. Pin the real ones:

```bash
helm upgrade node-doctor supporttools/node-doctor -n node-doctor \
  --reuse-values --set image.tag=1.8.7 --set overlayTest.image.tag=1.8.7
```

On any chart cut after the tag-naming fix this should not happen — `helm-publish` refuses to
publish a chart whose images do not resolve. If it happens anyway on a *new* chart, the guard
has a hole; check the `Verify packaged chart references images that exist` step in that
release's `helm-publish` job.

### arm64 nodes specifically stuck in `ImagePullBackOff`

Check the manifest list actually has an arm64 entry:

```bash
docker buildx imagetools inspect docker.io/supporttools/node-doctor:1.9.0
```

Every tag `release.yml` publishes is multi-arch. A single-arch `:latest` was possible when
`ci.yml` also pushed it (amd64-only) and won the race; `release.yml` is now the only publisher,
so this should no longer occur.

### A chart change did not take effect

You almost certainly edited `helm/node-doctor/values.yaml` instead of
`helm/node-doctor/values.yaml.template`. See
[Which Helm chart files actually ship](#which-helm-chart-files-actually-ship). `make
helm-verify-generated` catches the reverse case (template edited, generated file not regenerated);
it does not catch a hand-edit to `values.yaml` that you then propagated into the template — so
check `git diff` covers both files.

### Cosign verification fails

- Use `cosign verify` (image), not `cosign verify-blob` — no blobs are signed.
- Only the `<X.Y.Z>` tag on `node-doctor` is signed. `latest`, `{{major}}.{{minor}}` and the
  entire overlay-test image are unsigned.
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
- **No changelog generation.** The release notes are a static heredoc in `release.yml`; they are
  identical for every release apart from the version string.
- **No tests.** `release.yml` runs none, and does not depend on `ci.yml`.
- **No image publishing from `ci.yml`.** It once pushed to the same repository on tag events;
  its `docker` job now builds with `push: false` and publishes nothing.
- **No automatic backfill.** Chart versions `1.8.0`-`1.8.6` were never published and nothing
  republishes them retroactively; the daily audit reports the gap, it does not close it.

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
