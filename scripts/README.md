# Validation Scripts

This directory contains validation scripts that mirror CI/CD pipeline checks, allowing local validation before pushing code.

## Core Validation Scripts

### validate-pipeline-local.sh ⭐ PRIMARY SCRIPT

**Purpose**: Mirrors the complete CI/CD pipeline validation locally

**Usage**:
```bash
# Full validation (mirrors CI/CD exactly)
./scripts/validate-pipeline-local.sh

# Quick validation (format, vet, staticcheck only - no tests)
./scripts/validate-pipeline-local.sh --quick

# Validate specific component
./scripts/validate-pipeline-local.sh --component api
```

**What it validates**:
- Go version compliance
- Repository cleanliness
- Code formatting (gofmt)
- Go vet checks
- Static analysis (staticcheck)
- Security scanning (gosec)
- Unit tests with coverage
- Component-specific validations

**Exit codes**:
- `0` - All validations passed
- `1` - One or more validations failed

---

### validate-go-version.sh

**Purpose**: Ensures Go version consistency across project

**Usage**:
```bash
./scripts/validate-go-version.sh
```

**Checks**:
- Installed Go version matches go.mod requirement
- go.mod specifies correct Go version
- Dockerfiles use correct Go version

**Customization**:
Edit the script to change the required Go version (currently 1.24)

---

### validate-repo-cleanliness.sh

**Purpose**: Enforces repository structure rules

**Usage**:
```bash
./scripts/validate-repo-cleanliness.sh
```

**Checks**:
- No Go files in repository root
- No build artifacts in repository
- No temporary files committed
- No secrets or credentials
- Proper directory organization

**Violations result in**:
- Detailed error messages
- Suggested fixes
- Non-zero exit code

---

### validate-api.sh (Example Component Validation)

**Purpose**: Component-specific validation for API

**Usage**:
```bash
./scripts/validate-api.sh
```

**What it does**:
- Runs API-specific tests
- Validates API configuration
- Checks OpenAPI/Swagger documentation
- Verifies handler structure

**Customization**: Copy this pattern for other components

---

## Integration with Workflow

### Pre-Push Validation

Enable automatic pre-push validation:

```bash
# Setup git hooks (in .githooks/)
git config core.hooksPath .githooks
```

### Makefile Integration

These scripts are integrated into the makefile:

```bash
# Full validation
make validate-pipeline-local

# Quick validation
make validate-quick

# Component validation
make validate-component COMPONENT=api
```

### CI/CD Integration

The CI/CD pipeline uses these same scripts:

```yaml
# .github/workflows/pipeline-v2.yml
validation:
  - run: ./scripts/validate-go-version.sh
  - run: ./scripts/validate-repo-cleanliness.sh
  - run: ./scripts/validate-pipeline-local.sh
```

## Creating Component Validation Scripts

To add validation for a new component:

### 1. Create Component Script

```bash
cp scripts/validate-api.sh scripts/validate-mycomponent.sh
```

### 2. Customize for Component

```bash
#!/bin/bash
# validate-mycomponent.sh

set -e

echo "Validating my component..."

# Component-specific checks
cd cmd/mycomponent
go test ./... -v
go vet ./...

# Additional validation
# ...

echo "✅ My component validation passed"
```

### 3. Make Executable

```bash
chmod +x scripts/validate-mycomponent.sh
```

### 4. Add to Pipeline Script

Edit `validate-pipeline-local.sh` to include your component:

```bash
COMPONENTS=("api" "agent" "mycomponent")
```

### 5. Add Makefile Target

```makefile
validate-mycomponent-local:
	@./scripts/validate-mycomponent.sh
```

## Validation Configuration

Configuration is stored in `.validation.json`:

```json
{
  "validation": {
    "enabled": true,
    "parallel_jobs": 4,
    "quick_mode": {
      "skip_tests": true,
      "skip_gosec": true
    }
  },
  "components": {
    "api": {
      "enabled": true,
      "test_timeout": "10m",
      "coverage_threshold": 80
    }
  },
  "tools": {
    "staticcheck": {
      "enabled": true,
      "checks": ["all", "-ST1000"]
    },
    "gosec": {
      "enabled": true,
      "severity": "medium"
    }
  }
}
```

## Exit Codes

All validation scripts follow these exit code conventions:

- `0` - Success, all checks passed
- `1` - Failure, one or more checks failed
- `2` - Configuration error or missing dependencies

## Best Practices

### 1. Always Validate Locally Before Pushing

```bash
make validate-pipeline-local
```

This prevents CI/CD failures and saves time.

### 2. Use Quick Mode During Development

```bash
make validate-quick
```

Run quick validation frequently, full validation before push.

### 3. Fix Issues Systematically

When validation fails:
1. Read the error message carefully
2. Fix the reported issue
3. Re-run validation
4. Don't commit until validation passes

### 4. Keep Scripts Updated

When changing:
- Go version → Update validate-go-version.sh
- Repository structure → Update validate-repo-cleanliness.sh
- Component structure → Update component validation scripts

### 5. Test Scripts Locally

Before committing script changes:
```bash
# Test success case
./scripts/validate-pipeline-local.sh

# Test failure case (introduce an error)
echo "bad code" > test.go
./scripts/validate-pipeline-local.sh
git checkout test.go
```

## Troubleshooting

### Script Permission Denied

```bash
chmod +x scripts/*.sh
```

### Go Version Mismatch

```bash
# Check installed version
go version

# Check required version
grep "^go" go.mod

# Update Go if needed
```

### Validation Takes Too Long

Use quick mode during development:
```bash
./scripts/validate-pipeline-local.sh --quick
```

### Component Not Found

Add component to validate-pipeline-local.sh:
```bash
COMPONENTS=("api" "agent" "your-component")
```

## Integration Testing

Test that validation correctly catches issues:

```bash
# Test 1: Should pass
make validate-pipeline-local

# Test 2: Should fail on formatting
echo "package main;func main(){}" > cmd/test/bad.go
make validate-pipeline-local
git checkout cmd/test/bad.go

# Test 3: Should fail on cleanliness
touch root-file.go
make validate-pipeline-local
rm root-file.go
```

## Performance

### Validation Times (Typical)

- **Quick mode**: 30-60 seconds
- **Full validation**: 2-5 minutes
- **Component validation**: 30-120 seconds per component

### Optimization Tips

1. **Use parallel execution** (already configured)
2. **Skip tests in quick mode** for rapid iteration
3. **Cache Go modules** to speed up builds
4. **Use component validation** when working on specific areas

## Documentation

For complete validation workflow documentation, see:
- [Local CI/CD Validation Guide](../docs/development/local-cicd-validation-guide.md)
- [Task Execution Workflow](../docs/development/task-execution-workflow.md)

## Contributing

When adding new validation:
1. Create a focused validation script
2. Add to validate-pipeline-local.sh
3. Document in this README
4. Test both success and failure cases
5. Update .validation.json if needed
