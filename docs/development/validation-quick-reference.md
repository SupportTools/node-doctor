# Local CI/CD Validation - Quick Reference

Quick reference guide for using the local validation system to catch issues before pushing.

## ⚠️ CRITICAL: Validation Mode Selection

**ALWAYS use FULL validation before marking tasks complete or pushing to remote!**

- **Quick Mode** (`--quick`): Skips gosec security scans and tests
  - ✅ Use during development for fast feedback
  - ❌ **NEVER** use as final validation before commit/push

- **Full Mode** (default): Runs ALL checks including gosec and tests
  - ✅ **MANDATORY** before pushing to remote
  - ✅ **MANDATORY** before marking task complete
  - ✅ Mirrors CI/CD pipeline exactly

**Lesson Learned**: Using quick mode as final validation led to CI/CD failures that could have been caught locally. Always run full validation as the last step!

## Before You Push

### Full Validation (Recommended)

```bash
# Validate everything - mirrors CI/CD completely
make validate-pipeline-local

# Or directly
./scripts/validate-pipeline-local.sh
```

**When to use:** Before pushing to main/master, before creating PR

**Time:** ~2-5 minutes (depends on changed components)

### Quick Validation

```bash
# Skip tests and gosec - fast feedback
make validate-quick

# Or directly
./scripts/validate-pipeline-local.sh --quick
```

**When to use:** Before committing, during development, frequent checks

**Time:** ~30 seconds

### Component-Specific

```bash
# Validate only one component
make validate-component COMPONENT=api

# Or directly
./scripts/validate-pipeline-local.sh --component=ingestor
```

**Valid components:** api, org-management-controller, probe-controller, ingestor, customer-probe-agent, pkg

**When to use:** Working on specific component, debugging validation failures

**Time:** ~20 seconds per component

## Common Commands

### Makefile Targets

```bash
make validate-pipeline-local    # Full validation
make validate-quick             # Quick check (no tests)
make validate-pre-push          # Alias for full validation
make validate-component COMPONENT=api  # Single component
```

### Script Options

```bash
./scripts/validate-pipeline-local.sh --quick           # Skip tests
./scripts/validate-pipeline-local.sh --component=api   # One component
./scripts/validate-pipeline-local.sh --sequential      # Run one at a time
./scripts/validate-pipeline-local.sh --fail-fast       # Stop on first error
./scripts/validate-pipeline-local.sh --help            # Show all options
```

## Common Issues and Fixes

### Format Issues

**Error:**
```
✗ [component] format FAILED
pkg/foo/bar.go
```

**Fix:**
```bash
# Auto-fix formatting
gofmt -w .

# Or specific file
gofmt -w pkg/foo/bar.go
```

### Go Vet Errors

**Error:**
```
✗ [component] go-vet FAILED
composite literal uses unkeyed fields
```

**Fix:** Review the error, fix the code issue. These are real bugs that need fixing.

### Staticcheck Warnings

**Error:**
```
✗ [component] staticcheck FAILED
pkg/foo/bar.go:42:2: field token is unused (U1000)
```

**Fix Option 1 - Suppress (if intentional):**
```go
type Foo struct {
    token string //lint:ignore U1000 Reserved for future JWT auth
}
```

**Fix Option 2 - Remove unused code:**
```go
// Delete the unused field/function
```

### Gosec Security Warnings

**Error:**
```
✗ [component] gosec FAILED
[G115] integer overflow conversion (CWE-190)
```

**Fix Option 1 - Suppress (if validated):**
```go
//nolint:gosec // G115 - Validated: orgID range checked before conversion
orgID := uint64(unsafeValue)
```

**Fix Option 2 - Use safe conversion:**
```go
import "github.com/{{PROJECT_NAME}}/{{PROJECT_NAME}}/pkg/utils"

orgID := utils.SafeUintToUint64(value)  // Handles overflow
```

### Test Failures

**Error:**
```
✗ [component] tests FAILED
FAIL: TestFoo (0.00s)
```

**Fix:** Review test output, fix the failing test or code

**Run specific test:**
```bash
go test -v ./pkg/path/to/package -run TestName
```

### Race Detector Failures

**Error:**
```
WARNING: DATA RACE
Write at 0x... by goroutine X
```

**Fix:** This is a real concurrency bug - MUST be fixed

**Never suppress race detector warnings!**

## Component-Specific Commands

### API

```bash
cd /path/to/{{PROJECT_NAME}}
gofmt -l api/
cd api && go vet ./...
cd api && staticcheck ./...
cd api && gosec -quiet ./...
cd api && go test -v -race ./...
```

### pkg

```bash
gofmt -l pkg/
go vet ./pkg/...
staticcheck ./pkg/...
gosec -quiet ./pkg/...
go test -v -race ./pkg/...
```

### org-management-controller

```bash
gofmt -l pkg/controller/org-management/
go vet ./pkg/controller/org-management/...
staticcheck ./pkg/controller/org-management/...
gosec -quiet ./pkg/controller/org-management/...
go test -v -race ./pkg/controller/org-management/...
```

### probe-controller

```bash
gofmt -l pkg/probe/
go vet ./pkg/probe/...
staticcheck ./pkg/probe/...
gosec -quiet ./pkg/probe/...
go test -v -race ./pkg/probe/...
```

### ingestor

```bash
gofmt -l pkg/ingestor/
go vet ./pkg/ingestor/...
staticcheck ./pkg/ingestor/...
gosec -quiet ./pkg/ingestor/...
go test -v -race ./pkg/ingestor/...
```

### customer-probe-agent

**Note:** Has separate go.mod - must cd into directory

```bash
gofmt -l cmd/customer-probe-agent/
cd cmd/customer-probe-agent && go vet ./...
cd cmd/customer-probe-agent && staticcheck ./...
cd cmd/customer-probe-agent && gosec -quiet ./...
cd cmd/customer-probe-agent && go test -v -race ./...
```

## Enable Pre-Push Hook

### Automatic Validation on Push

```bash
# Run setup script
./.githooks/setup.sh

# Or manually configure
git config core.hooksPath .githooks
```

### Verify Hook Enabled

```bash
git config --get core.hooksPath
# Should output: .githooks
```

### Bypass Hook (Emergency Only)

```bash
git push --no-verify
```

**Warning:** Only use --no-verify for emergencies!

## Troubleshooting

### "directory prefix does not contain main module"

**Issue:** customer-probe-agent has separate go.mod

**Fix:** Always cd into directory:
```bash
cd cmd/customer-probe-agent && go vet ./...
```

### "couldn't parse format string"

**Issue:** Staticcheck false positive on `%.2f%%`

**Fix:** Add suppression:
```go
//lint:ignore SA5009 staticcheck false positive - format string valid
return fmt.Sprintf("%.2f%%", value)
```

### Race Detector Takes Too Long

**Solution:** Use quick mode during development:
```bash
make validate-quick  # Skips race detector
```

Run full validation before push:
```bash
make validate-pre-push  # Includes race detector
```

### Validation Hangs

**Solution:** Run sequential instead of parallel:
```bash
./scripts/validate-pipeline-local.sh --sequential
```

### Need to Validate Only Changed Code

**Solution:** Validate specific component:
```bash
# If you only changed ingestor code
make validate-component COMPONENT=ingestor
```

## Best Practices

### Development Workflow

1. **During development:** `make validate-quick` (frequent checks)
2. **Before commit:** `make validate-quick` (ensure no format/lint issues)
3. **Before push:** `make validate-pre-push` (full validation with tests - **MANDATORY**)
4. **Before marking task complete:** Full validation must pass - no exceptions!

**⚠️ WARNING**: Quick mode skips gosec security scans and tests. These WILL fail in CI/CD if you don't run full validation locally!

### Time-Saving Tips

1. **Use quick mode frequently** - catches 80% of issues in 30 seconds
2. **Validate specific components** - faster feedback when working on one area
3. **Enable pre-push hook** - prevents accidental pushes with issues
4. **Fix format issues first** - fastest to fix, unblocks other checks

### When to Bypass Validation

**Only bypass in these situations:**
- Emergency hotfix (non-code changes)
- Infrastructure-only changes (Helm charts, docs)
- You've already validated manually
- CI/CD pipeline will catch the issues anyway (not recommended!)

**Never bypass for:**
- Code changes
- "It works on my machine"
- "CI/CD is slow"
- Time pressure (fix the issues!)

## Examples

### Example 1: Quick Development Check

```bash
# Make some code changes
vim pkg/api/handlers/foo.go

# Quick check before committing
make validate-quick

# If passes, commit
git add .
git commit -m "feat: add new handler"
```

### Example 2: Full Pre-Push Validation

```bash
# Finished feature, ready to push
make validate-pre-push

# All checks pass
git push origin feature-branch
```

### Example 3: Fix Validation Failures

```bash
# Validation fails on format
make validate-quick
# ✗ [api] format FAILED

# Fix formatting
gofmt -w api/

# Re-validate
make validate-quick
# ✓ All checks pass

# Push
git push origin main
```

### Example 4: Component-Specific Development

```bash
# Working on ingestor only
vim pkg/ingestor/server/metrics.go

# Validate just ingestor
make validate-component COMPONENT=ingestor

# Passes, commit and continue
git commit -am "feat(ingestor): improve metrics collection"
```

## Validation Checklist

Before pushing code, ensure:

- [ ] Code is properly formatted (`gofmt`)
- [ ] No vet warnings (real bugs)
- [ ] No staticcheck issues (or properly suppressed)
- [ ] No security issues (gosec)
- [ ] All tests passing
- [ ] No race conditions detected
- [ ] Validation script returns exit code 0

## Need More Help?

- **Full documentation:** [local-cicd-validation-guide.md](local-cicd-validation-guide.md)
- **Git hooks:** [.githooks/README.md](../../.githooks/README.md)
- **Task workflow:** [task-execution-workflow.md](task-execution-workflow.md)
- **CI/CD pipeline:** [.github/workflows/pipeline-v2.yml](../../.github/workflows/pipeline-v2.yml)

## Quick Commands Summary

```bash
# Most common commands
make validate-quick                        # Fast check
make validate-pipeline-local               # Full check
make validate-component COMPONENT=api      # One component
./.githooks/setup.sh                       # Enable pre-push hook
git push --no-verify                       # Bypass hook (emergency)
```

---

**Remember:** Validation saves time by catching issues early. A few minutes of local validation prevents hours of CI/CD debugging!
