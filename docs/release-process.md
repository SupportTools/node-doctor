# Node Doctor Release Process

This document describes the release process for Node Doctor, including versioning, tagging, and deployment procedures.

## Table of Contents

- [Release Overview](#release-overview)
- [Version Numbering](#version-numbering)
- [Release Types](#release-types)
- [Creating a Release](#creating-a-release)
- [Release Checklist](#release-checklist)
- [Automated Release Pipeline](#automated-release-pipeline)
- [Rollback Procedures](#rollback-procedures)
- [Troubleshooting](#troubleshooting)

## Release Overview

Node Doctor uses automated releases triggered by Git tags. When a tag matching `v*` is pushed, the release pipeline:

1. ✅ Runs all tests
2. ✅ Builds multi-arch binaries (4 platforms)
3. ✅ Creates signed artifacts with cosign
4. ✅ Generates changelog from commits
5. ✅ Creates GitHub release with binaries
6. ✅ Builds and pushes Docker images (multi-arch)
7. ✅ Updates Harbor registry with new version

**Timeline**: Full release pipeline completes in ~10-15 minutes.

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

### When to Increment

| Change Type | Version Increment | Example |
|------------|-------------------|---------|
| Breaking change to config format | MAJOR | v1.5.2 → v2.0.0 |
| New monitor type added | MINOR | v1.5.2 → v1.6.0 |
| Bug fix in existing monitor | PATCH | v1.5.2 → v1.5.3 |
| Security vulnerability fix | PATCH | v1.5.2 → v1.5.3 |
| Documentation update only | None | No release |

## Release Types

### 1. Stable Release (Production)

**When**: Ready for production deployment
**Tag Format**: `v{MAJOR}.{MINOR}.{PATCH}`
**Example**: `v1.2.3`

**Criteria**:
- All tests passing ✅
- QA validation complete ✅
- No known critical bugs ✅
- Documentation updated ✅
- Breaking changes documented ✅

### 2. Release Candidate (RC)

**When**: Feature-complete, testing phase
**Tag Format**: `v{MAJOR}.{MINOR}.{PATCH}-rc.{N}`
**Example**: `v1.3.0-rc.1`

**Usage**:
```bash
# Build and deploy RC to test cluster
make bump-rc

# This increments RC version and deploys to a1-ops-prd cluster
```

**RC Workflow**:
1. Create RC tag: `v1.3.0-rc.1`
2. Deploy to staging/test cluster
3. Run integration tests
4. Fix bugs, increment RC: `v1.3.0-rc.2`
5. Repeat until stable
6. Create stable release: `v1.3.0`

### 3. Beta Release

**When**: Major features ready for wider testing
**Tag Format**: `v{MAJOR}.{MINOR}.{PATCH}-beta.{N}`
**Example**: `v2.0.0-beta.1`

**Characteristics**:
- API may still change
- Not production-ready
- For early adopters and testing

### 4. Alpha Release

**When**: Early development, experimental features
**Tag Format**: `v{MAJOR}.{MINOR}.{PATCH}-alpha.{N}`
**Example**: `v2.0.0-alpha.1`

**Characteristics**:
- Unstable, may have bugs
- For internal testing only
- API will likely change

## Creating a Release

### Prerequisites

Before creating a release, ensure:

```bash
# 1. All changes committed and pushed
git status
# Should show: "nothing to commit, working tree clean"

# 2. On main branch
git branch --show-current
# Should show: "main"

# 3. Pull latest changes
git pull origin main

# 4. All tests passing
make test-all

# 5. CI pipeline passing
gh run list --limit 1
```

### Step-by-Step Release Process

#### Option A: Automated Stable Release (Recommended)

```bash
# 1. Determine version number
CURRENT_VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
echo "Current version: $CURRENT_VERSION"

# 2. Decide new version (example: v1.2.3 → v1.3.0 for new feature)
NEW_VERSION="v1.3.0"

# 3. Create and push tag
git tag -a $NEW_VERSION -m "Release $NEW_VERSION"
git push origin $NEW_VERSION

# 4. Monitor release
gh run watch
```

**What happens next**:
- GitHub Actions triggers release workflow
- Builds binaries for 4 platforms
- Signs artifacts with cosign
- Creates GitHub release with changelog
- CI workflow builds Docker images
- Docker images pushed to Harbor

#### Option B: Release Candidate (RC)

```bash
# Use automated RC process
make bump-rc

# This will:
# - Validate pipeline (run tests)
# - Increment RC version (v0.1.0-rc.1 → v0.1.0-rc.2)
# - Build multi-arch Docker image
# - Push to Harbor registry
# - Deploy to a1-ops-prd cluster
# - Commit and tag the release
```

#### Option C: Manual Tag Creation

```bash
# 1. Create annotated tag
git tag -a v1.2.3 -m "Release v1.2.3

Features:
- Add Kubernetes 1.30 support
- Improve PLEG monitoring accuracy

Bug Fixes:
- Fix version parsing edge case

Breaking Changes:
- None
"

# 2. Push tag to trigger release
git push origin v1.2.3

# 3. Watch workflow
gh run watch
```

### Verification Steps

After tag push, verify release automation:

```bash
# 1. Check GitHub Actions status
gh run list --workflow=release.yml --limit 5

# 2. Watch release workflow
gh run watch

# 3. View release when complete
gh release view v1.2.3

# 4. Verify artifacts
gh release download v1.2.3 --dir /tmp/release-test
ls -lh /tmp/release-test

# 5. Verify Docker images
docker pull harbor.support.tools/node-doctor/node-doctor:v1.2.3

# 6. Test binary
cd /tmp/release-test
tar -xzf node-doctor_v1.2.3_linux_amd64.tar.gz
./node-doctor --version
```

## Release Checklist

Use this checklist for every stable release:

### Pre-Release (1-2 days before)

- [ ] All planned features merged to main
- [ ] All tests passing (`make test-all`)
- [ ] Integration tests passing
- [ ] No known critical bugs
- [ ] Documentation updated
  - [ ] README.md reflects new features
  - [ ] Configuration docs updated
  - [ ] Monitor docs updated
  - [ ] Changelog drafted
- [ ] Breaking changes documented
- [ ] Migration guide created (if needed)
- [ ] RC tested in staging environment

### Release Day

- [ ] Confirm main branch is stable
- [ ] Run full validation: `make validate-pipeline-local`
- [ ] Update version in documentation
- [ ] Create release tag: `git tag -a vX.Y.Z -m "Release vX.Y.Z"`
- [ ] Push tag: `git push origin vX.Y.Z`
- [ ] Monitor release workflow: `gh run watch`
- [ ] Verify GitHub release created
- [ ] Verify artifacts uploaded and signed
- [ ] Verify Docker images pushed to Harbor
- [ ] Test Docker image: `docker pull harbor.support.tools/node-doctor/node-doctor:vX.Y.Z`
- [ ] Test binary download and execution

### Post-Release

- [ ] Update deployment in production (if applicable)
- [ ] Monitor for issues in first 24 hours
- [ ] Update project status in TaskForge
- [ ] Announce release (Slack, email, etc.)
- [ ] Update any dependent projects
- [ ] Close related GitHub issues
- [ ] Update roadmap/project board

## Automated Release Pipeline

### Workflow Architecture

```
Tag Push (v*)
     ↓
┌─────────────────────────────────────┐
│  .github/workflows/release.yml      │
│  - Run tests (all + critical)       │
│  - Build binaries (GoReleaser)      │
│  - Smoke test binaries ✅           │
│  - Dual-sign artifacts (cosign+GPG) │
│  - Create GitHub release            │
└─────────────────────────────────────┘
     ↓
┌─────────────────────────────────────┐
│  .github/workflows/ci.yml           │
│  - Build Docker images (multi-arch) │
│  - Push to Harbor registry          │
│  - Run security scans               │
└─────────────────────────────────────┘
     ↓
GitHub Release Published
  - Changelog
  - Binaries: 2 platforms (linux/amd64, linux/arm64) - dual-signed
  - Docker images: linux/amd64, linux/arm64
  - Deployment manifests
  - Documentation
```

### Artifacts Produced

Each release creates:

1. **Binary Archives** (2 platforms):
   - `node-doctor_vX.Y.Z_linux_amd64.tar.gz`
   - `node-doctor_vX.Y.Z_linux_arm64.tar.gz`
   - Note: Darwin builds temporarily disabled due to cross-compilation issues

2. **Signatures** (dual-layer security):
   - `*.tar.gz.cosign.sig` - Cosign signature files (GitHub OIDC)
   - `*.tar.gz.cosign.crt` - Cosign certificate files
   - `*.tar.gz.asc` - GPG armored signatures (maintainer key)

3. **Checksums**:
   - `checksums.txt` - SHA256 checksums
   - `checksums.txt.cosign.sig` - Cosign-signed checksums
   - `checksums.txt.asc` - GPG-signed checksums

4. **Docker Images**:
   - `harbor.support.tools/node-doctor/node-doctor:vX.Y.Z`
   - `harbor.support.tools/node-doctor/node-doctor:latest` (stable only)

5. **Documentation**:
   - Complete docs/ directory
   - deployment/ manifests
   - Example configurations

### Artifact Verification

All artifacts are signed with **two independent layers** for defense-in-depth security:

1. **Cosign** (GitHub OIDC): Proves the artifact was built by the official GitHub Actions workflow
2. **GPG** (Maintainer Key): Proves a maintainer approved and signed the release

#### Layer 1: Cosign Verification (Keyless)

```bash
# Install cosign (one time)
brew install cosign  # macOS
# or
wget https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-amd64

# Verify binary signature (keyless)
cosign verify-blob \
  --signature node-doctor_v1.2.3_linux_amd64.tar.gz.cosign.sig \
  --certificate node-doctor_v1.2.3_linux_amd64.tar.gz.cosign.crt \
  --certificate-identity-regexp="https://github.com/supporttools/node-doctor" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  node-doctor_v1.2.3_linux_amd64.tar.gz

# Verify checksums
cosign verify-blob \
  --signature checksums.txt.cosign.sig \
  --certificate checksums.txt.cosign.crt \
  --certificate-identity-regexp="https://github.com/supporttools/node-doctor" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  checksums.txt
```

#### Layer 2: GPG Verification (Maintainer Signature)

```bash
# Install GPG (one time)
brew install gnupg  # macOS
# or
apt-get install gnupg  # Debian/Ubuntu

# Import maintainer public key (one time)
gpg --keyserver keyserver.ubuntu.com --recv-keys <MAINTAINER_KEY_ID>
# Key ID will be published in release notes

# Verify GPG signature
gpg --verify node-doctor_v1.2.3_linux_amd64.tar.gz.asc node-doctor_v1.2.3_linux_amd64.tar.gz

# Verify checksums GPG signature
gpg --verify checksums.txt.asc checksums.txt
```

#### Complete Verification (Recommended for Production)

```bash
# 1. Verify cosign signature (proves GitHub Actions built it)
cosign verify-blob \
  --signature node-doctor_v1.2.3_linux_amd64.tar.gz.cosign.sig \
  --certificate node-doctor_v1.2.3_linux_amd64.tar.gz.cosign.crt \
  --certificate-identity-regexp="https://github.com/supporttools/node-doctor" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com" \
  node-doctor_v1.2.3_linux_amd64.tar.gz

# 2. Verify GPG signature (proves maintainer approved it)
gpg --verify node-doctor_v1.2.3_linux_amd64.tar.gz.asc node-doctor_v1.2.3_linux_amd64.tar.gz

# 3. Verify file integrity
sha256sum -c checksums.txt

# All three checks must pass for production deployment ✅
```

**Why Two Layers?**

- **Cosign alone**: Vulnerable if GitHub account or OIDC is compromised
- **GPG alone**: Vulnerable if maintainer key is compromised
- **Both together**: Requires compromising BOTH GitHub infrastructure AND maintainer keys (defense-in-depth)

## Rollback Procedures

Node Doctor includes an automated rollback workflow for quickly responding to problematic releases.

### Automated Rollback Workflow (Recommended)

The rollback workflow provides three levels of response:

#### Option 1: Mark Release as Unsafe (Recommended for most cases)

Best when: Release has issues but you want to keep it visible with warnings

```bash
# Trigger via GitHub UI: Actions → Rollback Release → Run workflow
# Or via gh CLI:
gh workflow run rollback-release.yml \
  -f version=v1.2.3 \
  -f reason="Critical security vulnerability in authentication" \
  -f action=mark-unsafe \
  -f create_issue=true
```

**What this does**:
- ✅ Marks release as pre-release (less prominent)
- ✅ Adds prominent warning banner to release notes
- ✅ Sets release as not-latest
- ✅ Creates tracking issue
- ⚠️ Release remains visible (users can see the warning)

#### Option 2: Unpublish Release

Best when: Release should be hidden from users but preserved

```bash
gh workflow run rollback-release.yml \
  -f version=v1.2.3 \
  -f reason="Data corruption bug affecting all users" \
  -f action=unpublish \
  -f create_issue=true
```

**What this does**:
- ✅ Converts release to draft (invisible to users)
- ✅ Preserves all artifacts
- ✅ Creates tracking issue
- ✅ Can be republished if needed
- ⚠️ Users who already downloaded can still use it

#### Option 3: Delete Release

Best when: Release is critically broken and must be removed

```bash
gh workflow run rollback-release.yml \
  -f version=v1.2.3 \
  -f reason="Release contains malware/backdoor" \
  -f action=delete \
  -f create_issue=true
```

**What this does**:
- 🚨 Completely removes the release
- 🚨 Deletes all artifacts
- ✅ Creates tracking issue
- ⚠️ Git tag remains (delete separately if needed)
- ⚠️ This action CANNOT be undone

### Manual Rollback Procedures

If the automated workflow is not available, use these manual procedures:

#### Scenario 1: Bad Release Discovered Immediately

If a critical issue is found right after release:

**Recommended approach** - Use automated rollback workflow:
```bash
# Delete the release using automated workflow
gh workflow run rollback-release.yml \
  -f version=v1.2.3 \
  -f reason="Critical issue discovered immediately after release" \
  -f action=delete \
  -f create_issue=true
```

**Manual approach** (if automated workflow not available):
```bash
# 1. Delete the bad tag locally and remotely
git tag -d v1.2.3
git push origin :refs/tags/v1.2.3

# 2. Delete the GitHub release
gh release delete v1.2.3 --yes

# 3. Revert to previous stable version
kubectl set image daemonset/node-doctor \
  node-doctor=harbor.support.tools/node-doctor/node-doctor:v1.2.2 \
  -n kube-system

# 4. Fix the issue and create new release
# Fix code, then create v1.2.4 or v1.3.0
```

### Scenario 2: Issue Found in Production

If issue discovered after deployment:

```bash
# 1. Use automated rollback workflow (RECOMMENDED)
gh workflow run rollback-release.yml \
  -f version=v1.2.3 \
  -f reason="Critical bug causing node crashes" \
  -f action=mark-unsafe \
  -f create_issue=true

# 2. Rollback Kubernetes deployment to previous version
kubectl set image daemonset/node-doctor \
  node-doctor=harbor.support.tools/node-doctor/node-doctor:v1.2.2 \
  -n kube-system

# 3. Verify rollback
kubectl rollout status daemonset/node-doctor -n kube-system

# 4. Fix issue and create hotfix release
# Fix issue, test thoroughly
git tag -a v1.2.4 -m "Hotfix for v1.2.3"
git push origin v1.2.4

# Alternatively, if automated workflow not available:
# gh release edit v1.2.3 --notes "⚠️ **DO NOT USE** - Critical bug found. Use v1.2.2 instead."
```

### Scenario 3: Partial Rollback (Canary)

If only some nodes are affected:

```bash
# 1. Label nodes to rollback
kubectl label nodes node-1 node-doctor-version=v1.2.2

# 2. Update DaemonSet with nodeSelector override
# Use separate DaemonSet for rolled-back nodes

# 3. Monitor and decide next steps
kubectl get pods -n kube-system -l app=node-doctor -o wide
```

## Troubleshooting

### Release Workflow Failed

**Symptom**: GitHub Actions workflow fails

**Diagnosis**:
```bash
# View workflow logs
gh run view --log

# Check specific job
gh run view --job=<job-id> --log
```

**Common Issues**:

1. **Tests failing**:
   ```bash
   # Run tests locally
   make test-all

   # Fix failing tests, push fixes
   git commit -am "fix: failing tests"
   git push

   # Delete bad tag, recreate
   git tag -d v1.2.3
   git push origin :refs/tags/v1.2.3
   git tag -a v1.2.3 -m "Release v1.2.3"
   git push origin v1.2.3
   ```

2. **GoReleaser config error**:
   ```bash
   # Test GoReleaser locally
   goreleaser release --snapshot --clean

   # Fix .goreleaser.yml, commit, and recreate tag
   ```

3. **Cosign signing failed**:
   - Check GitHub Actions permissions (id-token: write)
   - Verify GITHUB_TOKEN has correct scopes
   - Check cosign version compatibility

### Docker Build Failed

**Symptom**: Docker images not available on Harbor

**Diagnosis**:
```bash
# Check CI workflow status
gh run list --workflow=ci.yml --limit 5

# View Docker build logs
gh run view <run-id> --job=docker
```

**Solutions**:
- Verify Harbor credentials in GitHub Secrets
- Check Dockerfile syntax
- Verify multi-arch buildx setup

### Artifacts Missing

**Symptom**: GitHub release created but artifacts missing

**Diagnosis**:
```bash
# Check release assets
gh release view v1.2.3 --json assets

# View GoReleaser logs
gh run view --log | grep -A 20 "goreleaser"
```

**Solutions**:
- Check .goreleaser.yml archive configuration
- Verify file paths in archives section
- Check GoReleaser version compatibility

## Support

For release process questions or issues:

1. Check this documentation first
2. Review GitHub Actions logs: `gh run list`
3. Open an issue: https://github.com/supporttools/node-doctor/issues
4. Contact maintainers

## References

- [Semantic Versioning](https://semver.org/)
- [GoReleaser Documentation](https://goreleaser.com/)
- [Cosign Documentation](https://docs.sigstore.dev/cosign/overview/)
- [GitHub Actions Workflows](../.github/workflows/)
- [Node Doctor Architecture](./architecture.md)
