# Git Hooks for Nexmonyx

This directory contains Git hooks that help maintain code quality by running validations before certain Git operations.

## Available Hooks

### pre-push

Runs comprehensive validation before pushing to remote:
- Code formatting (gofmt)
- Static analysis (go vet)
- Advanced linting (staticcheck)
- Security scanning (gosec)
- Unit tests with race detection

This prevents CI/CD failures by catching issues locally before they reach the pipeline.

## Setup

### Quick Setup (Recommended)

Run the setup script:

```bash
./.githooks/setup.sh
```

This will:
1. Configure Git to use `.githooks` directory
2. Enable all available hooks
3. Show current configuration

### Manual Setup

Configure Git to use this hooks directory:

```bash
git config core.hooksPath .githooks
```

Verify configuration:

```bash
git config --get core.hooksPath
# Should output: .githooks
```

## Usage

Once enabled, hooks run automatically:

```bash
# Pre-push hook runs automatically
git push origin main
```

### Bypassing Hooks

**Warning:** Only bypass hooks when absolutely necessary (e.g., emergency hotfix).

```bash
# Bypass pre-push validation
git push --no-verify

# Or bypass all hooks for one commit
git commit --no-verify -m "Emergency fix"
```

## Disabling Hooks

Temporarily disable:

```bash
git config --unset core.hooksPath
```

Re-enable:

```bash
git config core.hooksPath .githooks
```

## Hook Details

### pre-push Hook

**What it validates:**
- All 6 components: api, org-management-controller, probe-controller, ingestor, customer-probe-agent, pkg
- 5 checks per component: format, vet, staticcheck, gosec, tests

**When it runs:**
- Before every `git push`
- After `git push` command, before sending to remote

**What happens on failure:**
- Push is blocked
- Detailed error output shown
- Instructions provided to fix or bypass

**Performance:**
- Quick mode available: Only runs format, vet, staticcheck (skip tests)
- Full validation: ~2-5 minutes depending on changes
- Parallel execution: All components validated simultaneously

## Customization

### Run Quick Validation Only

Edit `.githooks/pre-push` and change:

```bash
./scripts/validate-pipeline-local.sh
```

To:

```bash
./scripts/validate-pipeline-local.sh --quick
```

### Validate Specific Components

Edit `.githooks/pre-push` to add component filtering:

```bash
# Only validate changed components
CHANGED_COMPONENTS=$(git diff --name-only HEAD @{u} | grep -E '^(api|pkg)/' | cut -d'/' -f1 | sort -u)
for component in $CHANGED_COMPONENTS; do
    ./scripts/validate-pipeline-local.sh --component=$component
done
```

## Troubleshooting

### Hook Not Running

Check configuration:

```bash
git config --get core.hooksPath
```

Verify hook is executable:

```bash
ls -la .githooks/pre-push
# Should show: -rwxr-xr-x
```

Make executable if needed:

```bash
chmod +x .githooks/pre-push
```

### Hook Failing Unexpectedly

Test hook manually:

```bash
./.githooks/pre-push
```

Run validation script directly:

```bash
./scripts/validate-pipeline-local.sh --quick
```

### Performance Issues

Use quick mode or component-specific validation:

```bash
# Quick validation (no tests)
./scripts/validate-pipeline-local.sh --quick

# Specific component
./scripts/validate-pipeline-local.sh --component=api
```

## Best Practices

1. **Enable hooks for all developers**
   - Prevents CI/CD failures
   - Faster feedback loop
   - Consistent code quality

2. **Run quick validation frequently**
   ```bash
   make validate-quick
   ```

3. **Run full validation before push**
   ```bash
   make validate-pre-push
   ```

4. **Only bypass when necessary**
   - Emergency hotfixes
   - Infrastructure-only changes
   - Non-code changes

5. **Keep hooks updated**
   - Pull latest `.githooks` directory
   - Re-run setup if hooks change

## Related Documentation

- [Local CI/CD Validation Guide](../docs/development/local-cicd-validation-guide.md)
- [Validation Quick Reference](../docs/development/validation-quick-reference.md) *(coming soon)*
- [Task Execution Workflow](../docs/development/task-execution-workflow.md)

## Support

For issues or questions:
- Check troubleshooting section above
- Review validation guide documentation
- Test validation script manually
- Check CI/CD pipeline for comparison
