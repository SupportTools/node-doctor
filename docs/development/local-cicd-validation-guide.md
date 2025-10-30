# Local CI/CD Validation Guide

## Table of Contents
1. [Problem Statement](#problem-statement)
2. [CI/CD Pipeline Architecture](#cicd-pipeline-architecture)
3. [Local Validation Script](#local-validation-script)
4. [Known Issues & Fixes](#known-issues--fixes)
5. [Usage Guide](#usage-guide)
6. [Integration with Git Hooks](#integration-with-git-hooks)
7. [Troubleshooting](#troubleshooting)

---

## Problem Statement

### The Gap

Our current local validation script (`scripts/validate-all.sh`) was designed for the original repository structure and checks these components:
- `api/`
- `agent/`
- `monitoring-agent/`
- `ui/`

However, our **CI/CD pipeline** (`.github/workflows/pipeline-v2.yml`) now tests a completely different set of 6 components:
- `api` (via `cd api && ...`)
- `org-management-controller` (via `./pkg/controller/org-management/...`)
- `probe-controller` (via `./pkg/probe/...`)
- `ingestor` (via `./pkg/ingestor/...`)
- `customer-probe-agent` (via `./cmd/customer-probe-agent/...`)
- `pkg` (via `./pkg/...`)

### The Cost

This mismatch means:
- ❌ Issues that will fail CI/CD **cannot be detected locally**
- ❌ Developers push code thinking it's validated
- ❌ CI/CD fails 15-30 minutes later
- ❌ Developer context-switches back to fix the issue
- ❌ Another push, another 15-30 minute wait
- ❌ **Total time wasted per issue: 30-60 minutes**

### The Solution

Create a new validation script (`validate-pipeline-local.sh`) that:
- ✅ **Mirrors the exact CI/CD pipeline steps**
- ✅ Tests all 6 components the CI/CD tests
- ✅ Runs the same commands in the same order
- ✅ Provides immediate feedback (5-10 minutes vs 15-30 minutes)
- ✅ Detects 100% of CI/CD issues before pushing

---

## CI/CD Pipeline Architecture

### Pipeline File
**Location**: `.github/workflows/pipeline-v2.yml`

### Test Matrix

The CI/CD pipeline uses GitHub Actions matrix strategy to test 6 components in parallel:

```yaml
strategy:
  matrix:
    component:
      - api
      - org-management-controller
      - probe-controller
      - ingestor
      - customer-probe-agent
      - pkg
  fail-fast: false
  max-parallel: 6
```

### Test Steps for Each Component

Every component goes through these 5 validation steps:

#### 1. Go Format Check (gofmt)
Ensures all Go code is properly formatted using `gofmt`.

**Per-component commands:**
```bash
# api
test -z "$(find api -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)"

# pkg
test -z "$(find pkg -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)"

# org-management-controller
test -z "$(find pkg/controller/org-management -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)"

# probe-controller
test -z "$(find pkg/probe -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)"

# ingestor
test -z "$(find pkg/ingestor -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)"

# customer-probe-agent
test -z "$(find cmd/customer-probe-agent -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)"
```

**Exit code**: `0` = pass, `1` = files need formatting

#### 2. Go Vet (static analysis)
Examines Go source code and reports suspicious constructs.

**Per-component commands:**
```bash
# api
cd api && go vet ./...

# pkg
go vet ./pkg/...

# org-management-controller
go vet ./pkg/controller/org-management/...

# probe-controller
go vet ./pkg/probe/...

# ingestor
go vet ./pkg/ingestor/...

# customer-probe-agent (HAS BUG - see Known Issues)
go vet ./cmd/customer-probe-agent/...  # ❌ BROKEN - should cd first
```

#### 3. Staticcheck (advanced linting)
Staticcheck is a state-of-the-art linter for Go. It uses static analysis to find bugs and performance issues.

**Installation** (done in CI/CD):
```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
```

**Per-component commands:**
```bash
# api
cd api && staticcheck ./...

# pkg
staticcheck ./pkg/...

# org-management-controller
staticcheck ./pkg/controller/org-management/...

# probe-controller
staticcheck ./pkg/probe/...

# ingestor
staticcheck ./pkg/ingestor/...

# customer-probe-agent (HAS BUG - see Known Issues)
staticcheck ./cmd/customer-probe-agent/...  # ❌ BROKEN - should cd first
```

**Suppression syntax**:
```go
//lint:ignore SA1019 Reason for ignoring this specific check
return deprecatedFunction()

//lint:file-ignore SA1019 AWS SDK deprecations - planned migration
package services
```

#### 4. Gosec (security scanning)
Inspects source code for security problems.

**Installation** (done in CI/CD):
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

**Per-component commands:**
```bash
# api
cd api && gosec -quiet ./...

# pkg
gosec -quiet ./pkg/...

# org-management-controller
gosec -quiet ./pkg/controller/org-management/...

# probe-controller
gosec -quiet ./pkg/probe/...

# ingestor
gosec -quiet ./pkg/ingestor/...

# customer-probe-agent (HAS BUG - see Known Issues)
gosec -quiet ./cmd/customer-probe-agent/...  # ❌ BROKEN - should cd first
```

**Suppression syntax**:
```go
// #nosec G115 - queueSize is always positive, validated at creation
queueCapacity := uint64(pq.queueSize)
```

#### 5. Unit Tests with Race Detection
Runs all unit tests with race detector and generates coverage reports.

**Per-component commands:**
```bash
# api
cd api && go test -v -race -coverprofile=coverage.out -covermode=atomic ./...

# org-management-controller
go test -v -race -coverprofile=coverage.out -covermode=atomic ./pkg/controller/org-management/...

# probe-controller
go test -v -race -coverprofile=coverage.out -covermode=atomic ./pkg/probe/...

# ingestor
go test -v -race -coverprofile=coverage.out -covermode=atomic ./pkg/ingestor/...

# customer-probe-agent
cd cmd/customer-probe-agent && go test -v -race -coverprofile=coverage.out -covermode=atomic ./...

# pkg
go test -v -race -coverprofile=coverage.out -covermode=atomic ./pkg/...
```

**Flags explained**:
- `-v`: Verbose output
- `-race`: Enable race detector (catches data races)
- `-coverprofile=coverage.out`: Generate coverage report
- `-covermode=atomic`: Coverage mode for race detector compatibility

---

## Local Validation Script

### Script Location
**File**: `scripts/validate-pipeline-local.sh`

### Design Principles

1. **Exact Parity with CI/CD**
   - Uses identical commands
   - Same execution order
   - Same exit codes

2. **Clear Output**
   - Component-by-component progress
   - ✓/✗ indicators for each step
   - Color-coded results

3. **Detailed Logging**
   - Saves full output to `.validation-logs/`
   - Timestamped log files
   - Easy debugging

4. **Fast Failure**
   - Stops on first error per component (default)
   - `--continue` flag to validate all components

5. **Component Isolation**
   - Each component tested independently
   - Can validate single component with `--component`

### Script Structure

```bash
#!/bin/bash
# Validates all 6 CI/CD components locally before pushing

set -e

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Components matching CI/CD pipeline
COMPONENTS=(
  "api"
  "org-management-controller"
  "probe-controller"
  "ingestor"
  "customer-probe-agent"
  "pkg"
)

# Validation functions
validate_go_fmt() { ... }
validate_go_vet() { ... }
validate_staticcheck() { ... }
validate_gosec() { ... }
validate_tests() { ... }

# Main validation loop
for component in "${COMPONENTS[@]}"; do
  echo "Validating $component..."
  validate_go_fmt "$component"
  validate_go_vet "$component"
  validate_staticcheck "$component"
  validate_gosec "$component"
  validate_tests "$component"
done
```

### Output Format

```
🔍 Local CI/CD Validation
═══════════════════════════════════════════════════════════════

[1/6] Validating api...
  ✓ go fmt check
  ✓ go vet
  ✓ staticcheck
  ✓ gosec
  ✓ tests (47 passed)

[2/6] Validating org-management-controller...
  ✓ go fmt check
  ✓ go vet
  ✓ staticcheck
  ✓ gosec
  ✓ tests (23 passed)

...

═══════════════════════════════════════════════════════════════
✅ All 6 components passed validation!

Logs saved to: .validation-logs/20251011_123045/
```

---

## Known Issues & Fixes

### Issue 1: customer-probe-agent Module Confusion

**Problem**: `customer-probe-agent` has its own `go.mod` file (separate Go module), but the CI/CD workflow runs validation commands from the repository root.

**Symptom**:
```bash
$ go vet ./cmd/customer-probe-agent/...
pattern ./cmd/customer-probe-agent/...: directory prefix cmd/customer-probe-agent
does not contain main module or its selected dependencies
```

**Root Cause**: When a directory has its own `go.mod`, it becomes a separate module. Commands like `go vet ./cmd/customer-probe-agent/...` from the root try to vet it as part of the root module, which fails.

**Fix in Workflow** (`.github/workflows/pipeline-v2.yml`):

```yaml
# BEFORE (BROKEN):
- name: Run go vet
  run: |
    case "${{ matrix.component }}" in
      customer-probe-agent)
        go vet ./cmd/customer-probe-agent/...  # ❌ Fails
        ;;
    esac

# AFTER (FIXED):
- name: Run go vet
  run: |
    case "${{ matrix.component }}" in
      customer-probe-agent)
        cd cmd/customer-probe-agent && go vet ./...  # ✅ Works
        ;;
    esac
```

**Apply same fix to**:
- `staticcheck` step (lines 148-150)
- `gosec` step (lines 172-174)

**Tests are already correct**: They already `cd` into the directory before running.

### Issue 2: Type Safety - uint vs uint64

**Problem**: Some fields use `uint` for organization IDs but NATS functions expect `uint64`.

**Symptom**:
```go
// pkg/ingestor/server/server.go:331
if uint64(conn.OrganizationID) != organizationID
```

The fact that we need `uint64()` cast suggests a type mismatch.

**Investigation**:
```bash
# Find all OrganizationID field definitions
grep -r "OrganizationID.*uint" pkg/ingestor/server/

# Check if it should be uint64 everywhere
grep -r "func.*organizationID uint64" pkg/
```

**Fix**: Ensure OrganizationID is consistently `uint64` throughout ingestor package.

### Issue 3: Staticcheck False Positives

**Problem**: Staticcheck sometimes reports false positives for format strings.

**Symptom**:
```
pkg/notification/analytics/reports.go:392:21: couldn't parse format string (SA5009)
```

**Fix**: Use `//lint:ignore SA5009` to suppress:
```go
//lint:ignore SA5009 staticcheck false positive - format string is valid with %% for literal %
return fmt.Sprintf(html, ...)
```

**File-level suppression**:
```go
//lint:file-ignore SA1019 AWS SDK deprecations - planned migration

package services
```

---

## Usage Guide

### Basic Usage

#### Validate All Components
```bash
./scripts/validate-pipeline-local.sh
```

This is the **recommended command** before every push. It validates all 6 components exactly as CI/CD will.

**Time**: ~5-10 minutes (depends on test count)

#### Validate Single Component
```bash
./scripts/validate-pipeline-local.sh --component ingestor
```

Use this when you've only changed code in one component and want quick feedback.

**Time**: ~1-2 minutes per component

#### Quick Mode (Skip Heavy Checks)
```bash
./scripts/validate-pipeline-local.sh --quick
```

Skips expensive operations like full test suites. Only runs fmt, vet, staticcheck, gosec.

**Time**: ~2-3 minutes

**When to use**: Pre-commit checks, quick verification

#### Continue on Failure
```bash
./scripts/validate-pipeline-local.sh --continue
```

Validates all components even if some fail. Useful for getting complete picture of issues.

**Default behavior**: Stops at first failure per component

### Makefile Integration

#### Add to Makefile

```makefile
# =============================================================================
# Local CI/CD Validation
# =============================================================================

.PHONY: validate-pipeline-local validate-component

# Validate all components (mirrors CI/CD pipeline)
validate-pipeline-local:
	@echo "Running full pipeline validation locally..."
	@./scripts/validate-pipeline-local.sh

# Validate single component
validate-component:
	@echo "Validating component: $(COMPONENT)..."
	@./scripts/validate-pipeline-local.sh --component $(COMPONENT)

# Quick validation (skip expensive tests)
validate-quick:
	@echo "Running quick validation..."
	@./scripts/validate-pipeline-local.sh --quick
```

#### Usage via Make

```bash
# Full validation
make validate-pipeline-local

# Single component
make validate-component COMPONENT=ingestor

# Quick mode
make validate-quick
```

### Reading Validation Logs

Logs are saved to `.validation-logs/TIMESTAMP/` with this structure:

```
.validation-logs/
└── 20251011_123045/
    ├── api_fmt.log
    ├── api_vet.log
    ├── api_staticcheck.log
    ├── api_gosec.log
    ├── api_tests.log
    ├── ingestor_fmt.log
    ├── ingestor_vet.log
    └── ...
```

**To view a specific failure**:
```bash
# List recent validation runs
ls -lt .validation-logs/

# View specific log
cat .validation-logs/20251011_123045/ingestor_staticcheck.log

# Search for errors
grep -r "error:" .validation-logs/20251011_123045/
```

**Log cleanup**: Script automatically keeps last 10 runs, deletes older logs.

---

## Integration with Git Hooks

### Pre-Push Hook

**File**: `.githooks/pre-push`

```bash
#!/bin/bash
# Validate code before pushing to remote

echo "🔍 Running local CI/CD validation before push..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Run quick validation (skip expensive full test suites)
./scripts/validate-pipeline-local.sh --quick

# Check exit code
if [ $? -ne 0 ]; then
    echo ""
    echo "❌ Validation failed! Push aborted."
    echo ""
    echo "To fix:"
    echo "  1. Review errors above"
    echo "  2. Fix issues and commit"
    echo "  3. Push again"
    echo ""
    echo "To skip validation (NOT recommended):"
    echo "  git push --no-verify"
    exit 1
fi

echo ""
echo "✅ Validation passed! Proceeding with push..."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
exit 0
```

### Enable Git Hooks

```bash
# Configure git to use .githooks/ directory
git config core.hooksPath .githooks

# Make hook executable
chmod +x .githooks/pre-push
```

### Override Hook (Emergency)

If you **absolutely must** push without validation (production emergency, etc.):

```bash
git push --no-verify
```

⚠️ **Warning**: Use sparingly! This bypasses all validation and CI/CD will likely fail.

---

## Troubleshooting

### Issue: "staticcheck: command not found"

**Cause**: staticcheck not installed

**Fix**:
```bash
go install honnef.co/go/tools/cmd/staticcheck@latest
```

### Issue: "gosec: command not found"

**Cause**: gosec not installed

**Fix**:
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

### Issue: Tests timeout

**Cause**: Some tests take longer than default timeout

**Fix**: Increase timeout in test command:
```bash
go test -v -race -timeout=10m -coverprofile=coverage.out ./...
```

### Issue: "permission denied" when running script

**Cause**: Script not executable

**Fix**:
```bash
chmod +x scripts/validate-pipeline-local.sh
```

### Issue: customer-probe-agent validation fails

**Cause**: Workflow bug - see [Known Issues](#known-issues--fixes)

**Temporary Workaround**: Skip customer-probe-agent until workflow fixed:
```bash
./scripts/validate-pipeline-local.sh --skip customer-probe-agent
```

### Issue: Log directory fills up disk space

**Cause**: Many validation runs, logs not cleaned up

**Fix**:
```bash
# Manual cleanup (keeps last 10 runs)
ls -t .validation-logs | tail -n +11 | xargs -I {} rm -rf .validation-logs/{}

# Or delete all logs
rm -rf .validation-logs
```

### Issue: Different results locally vs CI/CD

**Possible causes**:
1. **Go version mismatch**: CI/CD uses Go 1.24 - check yours:
   ```bash
   go version  # Should be 1.24.x
   ```

2. **Stale dependencies**: Update go.mod:
   ```bash
   go mod tidy
   go mod download
   ```

3. **Uncommitted changes**: CI/CD tests committed code:
   ```bash
   git status  # Check for uncommitted changes
   ```

4. **Cache issues**: Clear Go cache:
   ```bash
   go clean -cache -testcache -modcache
   ```

### Issue: Validation takes too long

**Solutions**:

1. **Use quick mode** for pre-commit:
   ```bash
   ./scripts/validate-pipeline-local.sh --quick
   ```

2. **Validate only changed components**:
   ```bash
   # If you only changed ingestor
   ./scripts/validate-pipeline-local.sh --component ingestor
   ```

3. **Run in parallel** (future enhancement):
   ```bash
   ./scripts/validate-pipeline-local.sh --parallel
   ```

---

## Appendix: Component Coverage Matrix

| Component | fmt | vet | staticcheck | gosec | tests | Path |
|-----------|-----|-----|-------------|-------|-------|------|
| api | ✓ | ✓ | ✓ | ✓ | ✓ | `api/` |
| org-management-controller | ✓ | ✓ | ✓ | ✓ | ✓ | `pkg/controller/org-management/` |
| probe-controller | ✓ | ✓ | ✓ | ✓ | ✓ | `pkg/probe/` |
| ingestor | ✓ | ✓ | ✓ | ✓ | ✓ | `pkg/ingestor/` |
| customer-probe-agent | ✓ | ⚠️ | ⚠️ | ⚠️ | ✓ | `cmd/customer-probe-agent/` |
| pkg | ✓ | ✓ | ✓ | ✓ | ✓ | `pkg/` |

⚠️ = Has workflow bug, needs `cd` before command

---

## Related Documentation

- [CI/CD Standardization Summary](./ci-cd-standardization-summary.md)
- [Makefile Pipeline Quick Reference](./makefile-pipeline-quick-reference.md)
- [Task Execution Workflow](./task-execution-workflow.md)
- [Error Response Standard](./error-response-standard.md)

---

**Last Updated**: 2025-10-11
**Maintained By**: Platform Team
**Questions**: See #engineering-platform on Slack
