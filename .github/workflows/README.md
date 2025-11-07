# GitHub Actions CI Workflow

This directory contains the GitHub Actions workflows for Node Doctor continuous integration and deployment.

## Workflows

### CI Workflow (`ci.yml`)

Comprehensive CI pipeline that runs on:
- Pull requests to `main` branch
- Pushes to `main` branch
- Git tags matching `v*` pattern

#### Jobs

1. **Lint** - Code quality checks
   - Runs `golangci-lint` with comprehensive configuration
   - Uses `.golangci.yml` configuration
   - Timeout: 5 minutes

2. **Test** - Unit and integration tests
   - Matrix strategy: Go 1.21 and 1.22
   - Runs unit tests with race detection and coverage
   - Runs integration tests (if `test/integration/` exists)
   - Uploads coverage to Codecov (Go 1.22 only)

3. **Security Scan (gosec)** - Security vulnerability detection
   - Scans Go code for common security issues
   - Uploads results to GitHub Security tab (SARIF format)

4. **Build** - Binary compilation
   - Builds the `node-doctor` binary
   - Injects version metadata (VERSION, GIT_COMMIT, BUILD_TIME)
   - Tests binary execution
   - Uploads binary as artifact (7-day retention)

5. **Docker** - Multi-platform container build (only on tags)
   - Builds for `linux/amd64` and `linux/arm64`
   - Pushes to Harbor registry
   - Tags with version and `latest`
   - Runs Trivy security scan on image
   - Uploads Trivy results to GitHub Security

6. **CI Success** - Overall status check
   - Aggregates results from all jobs
   - Fails if any required job fails

#### Features

- **Dependency Caching**: Go modules and build cache are cached automatically
- **Code Coverage**: Generated and uploaded to Codecov
- **Security Scanning**: gosec (code) and Trivy (container images)
- **Multi-platform Builds**: Docker images for amd64 and arm64
- **Build Metadata**: Version, commit SHA, and build time injected into binaries

## Required GitHub Secrets

Configure these secrets in your GitHub repository settings:

### Required for Docker builds (triggered on tags)

- `HARBOR_USERNAME` - Harbor registry username
- `HARBOR_PASSWORD` - Harbor registry password or access token

### Optional for coverage reporting

- `CODECOV_TOKEN` - Codecov.io upload token (optional, public repos may not need it)

### How to add secrets

1. Go to repository **Settings** → **Secrets and variables** → **Actions**
2. Click **New repository secret**
3. Add each secret with the exact name shown above

## Codecov Integration

Coverage reports are uploaded to [Codecov](https://codecov.io) for tracking over time.

### Setup Codecov

1. Visit [codecov.io](https://codecov.io) and sign in with GitHub
2. Add your repository
3. Copy the upload token
4. Add as `CODECOV_TOKEN` secret (optional for public repos)

## Triggering Builds

### Pull Request
Create a PR to `main` branch - triggers lint, test, security scan, and build jobs.

### Main Branch Push
Push or merge to `main` branch - runs full CI pipeline.

### Release
Create and push a git tag:

```bash
git tag v1.0.0
git push origin v1.0.0
```

This triggers:
- Full CI pipeline
- Docker multi-platform build
- Push to Harbor registry
- Trivy security scan

## Local Testing

Before pushing, test locally:

```bash
# Run lint
make lint

# Run tests
make test

# Run all tests with coverage
make test-all

# Build binary
make build

# Build Docker image
docker build -t node-doctor:local .
```

## golangci-lint Configuration

Linter configuration is in `.golangci.yml` at repository root. It includes:

- **Enabled linters**: errcheck, gosimple, govet, ineffassign, staticcheck, unused, gofmt, goimports, misspell, revive, gosec, and more
- **Cyclomatic complexity**: Maximum 15
- **Code duplication**: Threshold 100 lines
- **Test exclusions**: Some strict linters disabled for test files

## Troubleshooting

### Lint failures
Run `make lint` locally to see issues. Fix with `make fmt`.

### Test failures
Run `make test` or `make test-all` locally. Check for race conditions with `-race` flag.

### Docker build failures
Ensure Dockerfile is valid. Test locally: `docker build -t test .`

### Secret errors
Verify secrets are set correctly in repository settings. Check secret names match exactly.

### Coverage upload failures
Codecov uploads are non-blocking (fail_ci_if_error: false). Check Codecov token if needed.

## Status Badge

Add to README.md:

```markdown
[![CI](https://github.com/supporttools/node-doctor/workflows/CI/badge.svg)](https://github.com/supporttools/node-doctor/actions)
```
