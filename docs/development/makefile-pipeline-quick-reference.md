# Makefile CI/CD Pipeline - Quick Reference Guide

**Quick Start**: `make help` shows all available commands

## Common Workflows

### Local Development Deployment
```bash
# Complete dev deployment with custom version
make pipeline-dev VERSION=v$(date +%s)

# Or use the workflow integration
make bump
```

### Testing Individual Stages
```bash
# Check prerequisites
make check-prerequisites

# Build images only
make build-all-images VERSION=v99999

# Build and push images
make build-all-images push-all-images VERSION=v99999

# Generate and publish Helm chart
make helm-release VERSION=v99999

# Run security scans
make security-scan VERSION=v99999

# Verify deployment health
make verify-deployment-dev
```

### Troubleshooting

#### Check if prerequisites are installed
```bash
make check-prerequisites
```
**Checks**: docker, kubectl, helm, envsubst, git

#### Verify kubeconfig exists
```bash
make check-kubeconfig-dev
make check-kubeconfig-stg
make check-kubeconfig-prd
```

#### See what images would be built
```bash
# Builds but doesn't push (no network access needed except for base images)
make build-all-images VERSION=test
docker images | grep {{PROJECT_NAME}}
```

#### Test Helm chart generation
```bash
make helm-generate VERSION=test
ls -la helm/server/Chart.yaml helm/server/values.yaml
cat helm/server/Chart.yaml
```

## Pipeline Stages Explained

### 1. Prerequisites Check
```bash
make check-prerequisites
```
- Verifies required tools installed
- Checks: docker, kubectl, helm, envsubst, git
- **Exit code 0**: All tools found
- **Exit code 1**: Missing tools (shows which ones)

### 2. Build Images
```bash
make build-all-images VERSION=v123
```
**Builds**:
- API server (`api:v123`)
- Migration service (`migrations:v123`)
- Org provisioner (`org-provisioner:v123`)
- Agent ingestion (`agent-ingestion:v123`)
- Health controller (`health-controller:v123`)
- Probe controller (`probe-controller:v123`)
- Customer probe agent binaries (linux-amd64, linux-arm64)

**Tags**: Both `VERSION` and `latest`

### 3. Push Images
```bash
make push-all-images VERSION=v123
```
- Pushes all images to Harbor registry
- Pushes both version-specific and `latest` tags
- **Requires**: Docker login to Docker Hub

### 4. Helm Chart Release
```bash
make helm-release VERSION=v123
```
**Steps**:
1. Generate `Chart.yaml` and `values.yaml` from templates
2. Lint chart
3. Package as `{{PROJECT_NAME}}-server-v123.tgz`
4. Clone/update `helm-chart-private` repo (SSH)
5. Copy package and update repo index
6. Push to GitHub
7. Poll for chart availability (30 tries, 10 sec intervals)

**Requires**: SSH key for `SupportTools/helm-chart-private`

### 5. Database Migrations
```bash
make run-migrations-dev-ci VERSION=v123
make run-migrations-stg-ci VERSION=v123
make run-migrations-prd-ci VERSION=v123
```
- Uses pre-built migration image
- Runs Kubernetes Job
- Waits for completion (timeout: 10 minutes)
- **Note**: These are called by pipeline targets

### 6. ArgoCD Deployment
```bash
make argocd-deploy-dev VERSION=v123
make argocd-deploy-stg VERSION=v123
make argocd-deploy-prd VERSION=v123
```
**Steps**:
1. Apply ArgoCD project
2. Patch or create ArgoCD Application
3. Set `targetRevision` to new version
4. Wait for `Synced` + `Healthy` status (timeout: 7.5 minutes)

### 7. Deployment Verification
```bash
make verify-deployment-dev
make verify-deployment-stg
make verify-deployment-prd
```
**Checks**:
- Pod readiness count
- Health endpoint (`/health`)
- WebSocket connectivity

### 8. Security Scanning
```bash
make security-scan VERSION=v123
```
- Installs Trivy if not present
- Scans all images for CRITICAL+HIGH vulnerabilities
- Outputs to `security-scan-*.txt` files
- **Exit code 0**: Always succeeds (for CI/CD)

## Complete Pipelines

### Development Pipeline
```bash
make pipeline-dev VERSION=v123
```
**Executes**:
1. `check-prerequisites`
2. `build-all-images`
3. `push-all-images`
4. `helm-release`
5. `run-migrations-dev-ci`
6. `argocd-deploy-dev`
7. `verify-deployment-dev`

**Duration**: ~10-15 minutes

### Staging Pipeline
```bash
make pipeline-stg VERSION=v123
```
Same as dev, but for staging environment.

### Production Pipeline
```bash
make pipeline-prd VERSION=v123
```
Same as dev, but for production environment.

## Environment Variables

### Required
```bash
VERSION                 # Build version (default: timestamp)
```

### Optional (defaults provided)
```bash
REGISTRY                # Docker registry (default: docker.io/supporttools)
CHART_REPO_PATH         # Helm chart repo path (default: ../helm-chart-private)
CHART_REPO_URL          # Chart URL (default: https://charts-private.support.tools)
```

### Kubeconfig Paths (auto-configured)
```bash
KUBECONFIG_DEV          # Dev cluster config
KUBECONFIG_STG          # Staging cluster config
KUBECONFIG_PRD          # Production cluster config
```

## Common Errors and Solutions

### Error: "docker not found"
```bash
# Install Docker
sudo apt-get install docker.io

# Or check if it's in PATH
which docker
```

### Error: "envsubst not found"
```bash
# Install gettext-base package
sudo apt-get install gettext-base
```

### Error: "Kubeconfig not found"
```bash
# Check kubeconfig path
ls -la ~/.kube/mattox/

# Verify path in makefile matches your setup
grep KUBECONFIG makefile
```

### Error: "Chart not available after timeout"
```bash
# Check if chart was actually published
cd ../helm-chart-private
git log -1

# Manually verify chart in repository
helm repo update
helm search repo private-charts/{{PROJECT_NAME}}-server --version v123
```

### Error: "Permission denied (publickey)" for helm-chart repo
```bash
# Check SSH key is added to ssh-agent
ssh-add -l

# Test SSH connection
ssh -T git@github.com

# Add SSH key if needed
ssh-add ~/.ssh/id_rsa
```

### Error: "ArgoCD deployment timeout"
```bash
# Check ArgoCD application status manually
kubectl --kubeconfig $KUBECONFIG_DEV -n argocd get application {{PROJECT_NAME}}-dev

# View ArgoCD application details
kubectl --kubeconfig $KUBECONFIG_DEV -n argocd describe application {{PROJECT_NAME}}-dev

# Check ArgoCD logs
kubectl --kubeconfig $KUBECONFIG_DEV -n argocd logs deployment/argocd-application-controller
```

## Tips and Best Practices

### 1. Use Specific Versions
```bash
# Good - specific version for tracking
make pipeline-dev VERSION=v12345

# Avoid - timestamp makes it hard to track
make pipeline-dev  # Uses timestamp by default
```

### 2. Test Locally First
```bash
# Always test individual stages before full pipeline
make build-all-images VERSION=test
make helm-generate VERSION=test
```

### 3. Check Prerequisites Regularly
```bash
# Run after system updates or fresh installs
make check-prerequisites
```

### 4. Review Security Scans
```bash
# After running security-scan, review results
cat security-scan-api.txt
cat security-scan-migrations.txt
```

### 5. Monitor Deployments
```bash
# After deployment, check logs
make logs-api-dev

# Check pod status
kubectl --kubeconfig $KUBECONFIG_DEV -n {{PROJECT_NAME}}-dev get pods

# Health check
make health-check-dev
```

## Comparison: Old vs New Workflow

### Old Workflow (Shell Scripts)
```bash
# API deployment
cd api
./scripts/deploy-dev.sh

# Org management deployment
cd ../controllers/org-management
./scripts/deploy-dev.sh

# Different commands in CI/CD (GitHub Actions inline)
```

### New Workflow (Makefile)
```bash
# Same command locally and in CI/CD
make pipeline-dev VERSION=v123

# Or individual components
make build-all-images VERSION=v123
make helm-release VERSION=v123
make argocd-deploy-dev VERSION=v123
```

## Getting Help

### Show all available commands
```bash
make help
```

### Show workflow commands
```bash
make workflow-help
```

### Show GitHub Actions monitoring
```bash
make gh-help
```

### Check current task status
```bash
make workflow-status
```

## Quick Debugging

### What's running in dev?
```bash
kubectl --kubeconfig $KUBECONFIG_DEV -n {{PROJECT_NAME}}-dev get pods
kubectl --kubeconfig $KUBECONFIG_DEV -n {{PROJECT_NAME}}-dev get deployments
```

### What version is deployed?
```bash
kubectl --kubeconfig $KUBECONFIG_DEV -n {{PROJECT_NAME}}-dev get deployment {{PROJECT_NAME}} -o jsonpath='{.spec.template.spec.containers[0].image}'
```

### Is the API healthy?
```bash
make health-check-dev
# Or directly
curl -f https://api-dev.{{PROJECT_NAME}}.com/v1/healthz
```

### What's in the Helm chart repo?
```bash
helm repo update
helm search repo private-charts/{{PROJECT_NAME}}-server --versions | head -20
```

## Emergency Procedures

### Rollback Deployment
```bash
# Patch ArgoCD to previous version
kubectl --kubeconfig $KUBECONFIG_DEV -n argocd patch application {{PROJECT_NAME}}-dev \
  --type merge \
  -p '{"spec":{"source":{"targetRevision":"v12344"}}}'

# Wait for sync
make argocd-wait-dev
```

### Force Pod Restart
```bash
make restart-api-dev
# Or manually
kubectl --kubeconfig $KUBECONFIG_DEV -n {{PROJECT_NAME}}-dev rollout restart deployment/{{PROJECT_NAME}}
```

### Check Recent Changes
```bash
# GitHub Actions runs
make gh-status

# Git commits
git log --oneline -10

# Helm chart versions
cd ../helm-chart-private && git log --oneline -10
```
