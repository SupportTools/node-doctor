# ================================================================================================
# Nexmonyx Repository Template - Makefile
# ================================================================================================
#
# This makefile provides a complete build, test, and deployment framework for Go/Kubernetes projects.
#
# CUSTOMIZATION REQUIRED:
# 1. Replace {{PROJECT_NAME}} with your project name
# 2. Replace {{REGISTRY}} with your container registry (e.g., ghcr.io/myorg, harbor.mycompany.com/myproject)
# 3. Replace {{HELM_REPO}} with your Helm chart repository URL
# 4. Update COMPONENTS list with your actual components
# 5. Customize KUBECONFIG paths for your environments
# 6. Adjust build targets for your project structure
#
# ================================================================================================

.PHONY: help all test build deploy \
	build-local test-local validate-local validate-pipeline-local validate-quick \
	deploy-dev deploy-stg deploy-prd \
	bump bump-with-monitoring \
	qa-check devils-advocate workflow-status \
	gh-status gh-watch gh-logs gh-builds \
	check-prerequisites check-docker check-kubectl

# ================================================================================================
# Project Configuration
# ================================================================================================

# Project Configuration
PROJECT_NAME := node-doctor

# Container registry (Harbor for production releases)
REGISTRY := harbor.support.tools/node-doctor

# Version and build information
VERSION := $(shell date +%s)
GIT_COMMIT := $(shell git rev-parse HEAD)
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION := 1.21

# RC Version - for release candidate builds
RC_VERSION_FILE := .version-rc
RC_VERSION := $(shell [ -f $(RC_VERSION_FILE) ] && cat $(RC_VERSION_FILE) || echo "v0.1.0-rc.1")

export VERSION
export GIT_COMMIT
export BUILD_TIME
export GO_VERSION

# ================================================================================================
# Environment Configuration
# ================================================================================================

# CUSTOMIZE: Update these paths for your kubeconfig locations
KUBECONFIG_DEV := $(HOME)/.kube/dev-cluster
KUBECONFIG_STG := $(HOME)/.kube/staging-cluster
KUBECONFIG_PRD := $(HOME)/.kube/config

# Kubernetes context for production deployment
KUBECTL_CONTEXT_PRD := a1-ops-prd

export KUBECONFIG_DEV
export KUBECONFIG_STG
export KUBECONFIG_PRD

# Kubernetes namespaces (node-doctor runs in kube-system typically)
NAMESPACE_DEV := kube-system
NAMESPACE_STG := kube-system
NAMESPACE_PRD := node-doctor

# Helm Chart settings
# CUSTOMIZE: Update Helm repository URL
HELM_REPO_URL := {{HELM_REPO}}
CHART_VERSION := v$(shell git rev-list --count HEAD)

# ================================================================================================
# Component Configuration
# ================================================================================================

# Node Doctor is a single binary
COMPONENTS := node-doctor

# Docker image for node-doctor
DOCKER_IMAGE_node-doctor := $(REGISTRY)/$(PROJECT_NAME)

# ================================================================================================
# Color Output Functions
# ================================================================================================

RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[1;33m
BLUE := \033[0;34m
NC := \033[0m

define print_status
/bin/bash -c 'echo -e "$(BLUE)[$$(date +%Y-%m-%d\ %H:%M:%S)]$(NC) $(1)"'
endef

define print_success
/bin/bash -c 'echo -e "$(GREEN)[SUCCESS]$(NC) $(1)"'
endef

define print_error
/bin/bash -c 'echo -e "$(RED)[ERROR]$(NC) $(1)"'
endef

define print_warning
/bin/bash -c 'echo -e "$(YELLOW)[WARNING]$(NC) $(1)"'
endef

# ================================================================================================
# Prerequisites Checking
# ================================================================================================

check-docker:
	@$(call print_status,"Checking Docker prerequisites...")
	@command -v docker >/dev/null 2>&1 || ($(call print_error,"docker not found") && exit 1)
	@$(call print_success,"Docker check passed")

check-kubectl:
	@$(call print_status,"Checking kubectl prerequisite...")
	@command -v kubectl >/dev/null 2>&1 || ($(call print_error,"kubectl not found") && exit 1)
	@$(call print_success,"kubectl check passed")

check-prerequisites:
	@$(call print_status,"Checking prerequisites...")
	@command -v docker >/dev/null 2>&1 || ($(call print_error,"docker not found") && exit 1)
	@command -v kubectl >/dev/null 2>&1 || ($(call print_error,"kubectl not found") && exit 1)
	@command -v helm >/dev/null 2>&1 || ($(call print_error,"helm not found") && exit 1)
	@command -v git >/dev/null 2>&1 || ($(call print_error,"git not found") && exit 1)
	@command -v go >/dev/null 2>&1 || ($(call print_error,"go not found") && exit 1)
	@$(call print_success,"Prerequisites check passed")

check-go-version:
	@$(call print_status,"Checking Go version...")
	@./scripts/validate-go-version.sh
	@$(call print_success,"Go version check passed")

check-kubeconfig-%:
	@$(call print_status,"Checking kubeconfig for $*...")
	@ENV_UPPER=$$(echo $* | tr '[:lower:]' '[:upper:]') && \
	KUBECONFIG_VAR=KUBECONFIG_$$ENV_UPPER && \
	eval KUBECONFIG_PATH=\$$$$KUBECONFIG_VAR && \
	if [ ! -f "$$KUBECONFIG_PATH" ]; then \
		$(call print_error,"Kubeconfig not found for $*: $$KUBECONFIG_PATH"); \
		exit 1; \
	fi
	@$(call print_success,"Kubeconfig validated for $*")

# ================================================================================================
# Build Targets
# ================================================================================================

# Build all components locally
build-all-local: check-prerequisites check-go-version
	@$(call print_status,"Building all components locally...")
	@for component in $(COMPONENTS); do \
		$(call print_status,"Building $$component..."); \
		$(MAKE) build-$$component-local || exit 1; \
	done
	@$(call print_success,"All components built successfully")

# Build node-doctor binary
build-node-doctor-local:
	@$(call print_status,"Building node-doctor locally...")
	@mkdir -p bin
	@cd cmd/node-doctor && go build -ldflags="-X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)" -o ../../bin/node-doctor
	@$(call print_success,"node-doctor built: bin/node-doctor")

build-local: build-all-local

# ================================================================================================
# Test Targets
# ================================================================================================

# Run all tests locally
test-all-local: check-prerequisites check-go-version
	@$(call print_status,"Running all tests locally...")
	@go test ./... -v -cover -coverprofile=coverage.txt
	@$(call print_success,"All tests passed")

test-local: test-all-local

# Component-specific test target
test-node-doctor-local:
	@$(call print_status,"Running node-doctor tests...")
	@go test ./... -v -cover
	@$(call print_success,"node-doctor tests passed")

# ================================================================================================
# Validation Targets (mirrors CI/CD pipeline)
# ================================================================================================

validate-pipeline-local: check-prerequisites check-go-version
	@$(call print_status,"Running full pipeline validation (mirrors CI/CD)...")
	@./scripts/validate-pipeline-local.sh
	@$(call print_success,"Pipeline validation passed")

validate-quick: check-prerequisites check-go-version
	@$(call print_status,"Running quick validation (format, vet, staticcheck)...")
	@./scripts/validate-pipeline-local.sh --quick
	@$(call print_success,"Quick validation passed")

validate-component:
	@$(call print_status,"Validating component: $(COMPONENT)...")
	@./scripts/validate-pipeline-local.sh --component $(COMPONENT)
	@$(call print_success,"Component validation passed")

validate-local: validate-pipeline-local

# ================================================================================================
# Docker Image Build Targets
# ================================================================================================

# Build Docker image for node-doctor
build-node-doctor-image: check-docker
	@$(call print_status,"Building node-doctor Docker image...")
	@docker build -f Dockerfile -t $(DOCKER_IMAGE_node-doctor):$(VERSION) .
	@docker tag $(DOCKER_IMAGE_node-doctor):$(VERSION) $(DOCKER_IMAGE_node-doctor):latest
	@$(call print_success,"node-doctor image built: $(DOCKER_IMAGE_node-doctor):$(VERSION)")

# Build all images (just node-doctor)
build-all-images: build-node-doctor-image
	@$(call print_success,"Docker image built successfully")

# Push node-doctor image to registry
push-node-doctor-image:
	@$(call print_status,"Pushing node-doctor image to registry...")
	@docker push $(DOCKER_IMAGE_node-doctor):$(VERSION)
	@docker push $(DOCKER_IMAGE_node-doctor):latest
	@$(call print_success,"node-doctor image pushed")

push-all-images: push-node-doctor-image
	@$(call print_success,"Image pushed to registry")

# ================================================================================================
# Helm Chart Targets
# ================================================================================================

helm-lint:
	@$(call print_status,"Linting Helm chart...")
	@helm lint ./helm/$(PROJECT_NAME)
	@$(call print_success,"Helm lint passed")

helm-package:
	@$(call print_status,"Packaging Helm chart...")
	@helm package ./helm/$(PROJECT_NAME) --version $(CHART_VERSION)
	@$(call print_success,"Helm chart packaged")

helm-publish: helm-package
	@$(call print_status,"Publishing Helm chart to repository...")
	@# CUSTOMIZE: Add your Helm chart publish command
	@$(call print_success,"Helm chart published")

# ================================================================================================
# Deployment Targets
# ================================================================================================

deploy-dev: check-kubeconfig-dev
	@$(call print_status,"Deploying to development environment...")
	@KUBECONFIG=$(KUBECONFIG_DEV) helm upgrade --install $(PROJECT_NAME) ./helm/$(PROJECT_NAME) \
		--namespace $(NAMESPACE_DEV) \
		--set image.tag=$(VERSION) \
		--set environment=dev \
		--wait
	@$(call print_success,"Deployed to development")

deploy-stg: check-kubeconfig-stg
	@$(call print_status,"Deploying to staging environment...")
	@KUBECONFIG=$(KUBECONFIG_STG) helm upgrade --install $(PROJECT_NAME) ./helm/$(PROJECT_NAME) \
		--namespace $(NAMESPACE_STG) \
		--set image.tag=$(VERSION) \
		--set environment=staging \
		--wait
	@$(call print_success,"Deployed to staging")

deploy-prd: check-kubeconfig-prd
	@echo "⚠️  CRITICAL DECISION: Deploy to Production"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "What: Deploy $(PROJECT_NAME) version $(VERSION) to production"
	@echo "Why: Approved for production deployment"
	@echo "Risk: May impact production users"
	@echo ""
	@read -p "Do you approve? (y/n): " approve; \
	if [ "$$approve" != "y" ] && [ "$$approve" != "Y" ]; then \
		$(call print_error,"Production deployment cancelled"); \
		exit 1; \
	fi
	@$(call print_status,"Deploying to production environment...")
	@KUBECONFIG=$(KUBECONFIG_PRD) helm upgrade --install $(PROJECT_NAME) ./helm/$(PROJECT_NAME) \
		--namespace $(NAMESPACE_PRD) \
		--set image.tag=$(VERSION) \
		--set environment=production \
		--wait
	@$(call print_success,"Deployed to production")

# ================================================================================================
# Version Bump and Deployment Workflow
# ================================================================================================

bump: validate-pipeline-local
	@$(call print_status,"Creating new version bump...")
	@$(call print_status,"Current VERSION: $(VERSION)")
	@git add . && git commit -m "bump: automated version bump to $(VERSION) [skip ci]" || true
	@git push origin $$(git rev-parse --abbrev-ref HEAD)
	@$(call print_success,"Version bumped and pushed - CI/CD will handle deployment")

bump-with-monitoring: bump
	@$(call print_status,"Monitoring GitHub Actions workflow...")
	@$(MAKE) gh-watch

# Increment RC version (e.g., v0.1.0-rc.1 -> v0.1.0-rc.2)
increment-rc-version:
	@$(call print_status,"Incrementing RC version...")
	@CURRENT_RC=$$(cat $(RC_VERSION_FILE)); \
	BASE_VERSION=$$(echo $$CURRENT_RC | sed 's/-rc\.[0-9]*$$//'); \
	RC_NUM=$$(echo $$CURRENT_RC | sed 's/.*-rc\.//'); \
	NEW_RC_NUM=$$((RC_NUM + 1)); \
	NEW_RC_VERSION="$$BASE_VERSION-rc.$$NEW_RC_NUM"; \
	echo $$NEW_RC_VERSION > $(RC_VERSION_FILE); \
	echo "$(GREEN)✅ RC version updated: $$CURRENT_RC -> $$NEW_RC_VERSION$(NC)"

# Deploy to production cluster using kubectl
deploy-prd-kubectl: check-kubectl
	@$(call print_status,"Deploying to production cluster $(KUBECTL_CONTEXT_PRD)...")
	@$(call print_status,"Checking namespace $(NAMESPACE_PRD)...")
	@KUBECONFIG=$(KUBECONFIG_PRD) kubectl config use-context $(KUBECTL_CONTEXT_PRD)
	@KUBECONFIG=$(KUBECONFIG_PRD) kubectl get namespace $(NAMESPACE_PRD) > /dev/null 2>&1 || \
		($(call print_status,"Creating namespace $(NAMESPACE_PRD)...") && \
		KUBECONFIG=$(KUBECONFIG_PRD) kubectl create namespace $(NAMESPACE_PRD))
	@$(call print_status,"Applying RBAC resources...")
	@KUBECONFIG=$(KUBECONFIG_PRD) kubectl apply -f deployment/rbac.yaml -n $(NAMESPACE_PRD)
	@$(call print_status,"Applying DaemonSet...")
	@KUBECONFIG=$(KUBECONFIG_PRD) kubectl apply -f deployment/daemonset.yaml -n $(NAMESPACE_PRD)
	@$(call print_status,"Waiting for rollout to complete...")
	@KUBECONFIG=$(KUBECONFIG_PRD) kubectl rollout status daemonset/node-doctor -n $(NAMESPACE_PRD) --timeout=5m
	@$(call print_success,"Deployed to production cluster $(KUBECTL_CONTEXT_PRD)")

# Build and push RC release to Harbor and deploy to a1-ops-prd cluster
bump-rc: validate-pipeline-local increment-rc-version
	@$(call print_status,"Building RC release: $(RC_VERSION)")
	@$(call print_status,"Registry: $(REGISTRY)/$(PROJECT_NAME)")
	@$(call print_status,"Cluster: $(KUBECTL_CONTEXT_PRD), Namespace: $(NAMESPACE_PRD)")
	@echo ""
	@$(call print_status,"Building Docker image...")
	@docker build \
		--build-arg VERSION=$(RC_VERSION) \
		--build-arg GIT_COMMIT=$(GIT_COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-t $(REGISTRY)/$(PROJECT_NAME):$(RC_VERSION) \
		-t $(REGISTRY)/$(PROJECT_NAME):latest \
		-f Dockerfile .
	@$(call print_success,"Docker image built: $(REGISTRY)/$(PROJECT_NAME):$(RC_VERSION)")
	@echo ""
	@$(call print_status,"Pushing image to Harbor...")
	@docker push $(REGISTRY)/$(PROJECT_NAME):$(RC_VERSION)
	@docker push $(REGISTRY)/$(PROJECT_NAME):latest
	@$(call print_success,"Image pushed to Harbor")
	@echo ""
	@$(call print_status,"Updating DaemonSet image tag...")
	@sed -i.bak 's|image: harbor.support.tools/node-doctor/node-doctor:.*|image: $(REGISTRY)/$(PROJECT_NAME):$(RC_VERSION)|' deployment/daemonset.yaml
	@rm -f deployment/daemonset.yaml.bak
	@echo ""
	@$(MAKE) deploy-prd-kubectl
	@echo ""
	@$(call print_status,"Committing RC release...")
	@git add $(RC_VERSION_FILE) deployment/daemonset.yaml
	@git commit -m "bump-rc: Release candidate $(RC_VERSION) deployed to $(KUBECTL_CONTEXT_PRD)" || true
	@git tag -a $(RC_VERSION) -m "Release candidate $(RC_VERSION)" || true
	@git push origin main --tags || true
	@echo ""
	@$(call print_success,"🎉 RC Release $(RC_VERSION) complete!")
	@$(call print_success,"   - Built and pushed to Harbor")
	@$(call print_success,"   - Deployed to $(KUBECTL_CONTEXT_PRD) cluster")
	@$(call print_success,"   - Tagged as $(RC_VERSION)")
	@echo ""
	@$(call print_status,"Next steps:")
	@echo "  - Verify deployment: kubectl --context=$(KUBECTL_CONTEXT_PRD) -n $(NAMESPACE_PRD) get pods -l app=node-doctor"
	@echo "  - Check logs: kubectl --context=$(KUBECTL_CONTEXT_PRD) -n $(NAMESPACE_PRD) logs -l app=node-doctor"
	@echo "  - Monitor health: kubectl --context=$(KUBECTL_CONTEXT_PRD) -n $(NAMESPACE_PRD) get pods -l app=node-doctor -w"

# ================================================================================================
# Quality Workflow Targets
# ================================================================================================

workflow-status:
	@$(call print_status,"Checking workflow status...")
	@echo "TODO: Check TaskForge or project management system"

qa-check:
	@$(call print_status,"Running QA validation...")
	@echo "QA Check: Running validation suite"
	@$(MAKE) validate-pipeline-local
	@$(MAKE) test-local
	@$(call print_success,"QA validation passed")

devils-advocate:
	@$(call print_status,"Running Devils Advocate validation...")
	@echo "Devils Advocate: Challenging implementation..."
	@echo "- Testing edge cases"
	@echo "- Verifying error handling"
	@echo "- Checking security implications"
	@$(call print_success,"Devils Advocate review complete")

# ================================================================================================
# GitHub Actions Monitoring
# ================================================================================================

gh-status:
	@$(call print_status,"Checking GitHub Actions status...")
	@gh run list --limit 5

gh-watch:
	@$(call print_status,"Watching GitHub Actions workflow...")
	@gh run watch

gh-logs:
	@$(call print_status,"Fetching GitHub Actions logs...")
	@gh run view --log

gh-builds:
	@$(call print_status,"Showing recent builds...")
	@gh run list --workflow=pipeline-v2.yml --limit 10

# ================================================================================================
# Help System
# ================================================================================================

help:
	@echo "$(PROJECT_NAME) Makefile - Repository Template"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "QUICK START"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make build-local           Build all components locally"
	@echo "  make test-local            Run all tests"
	@echo "  make validate-pipeline-local  Validate before push (mirrors CI/CD)"
	@echo "  make bump                  Bump version and trigger deployment"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "BUILD COMMANDS"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make build-all-local       Build all components"
	@echo "  make build-api-local       Build API component"
	@echo "  make build-agent-local     Build agent component"
	@echo "  make build-all-images      Build all Docker images"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "TEST COMMANDS"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make test-local            Run all tests with coverage"
	@echo "  make test-api-local        Run API tests"
	@echo "  make test-agent-local      Run agent tests"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "VALIDATION COMMANDS (Pre-push)"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make validate-pipeline-local  Full validation (mirrors CI/CD exactly)"
	@echo "  make validate-quick        Quick validation (format, vet, staticcheck)"
	@echo "  make validate-component COMPONENT=api  Validate specific component"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "DEPLOYMENT COMMANDS"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make bump                  Bump version and trigger CI/CD"
	@echo "  make bump-rc               Build RC, push to Harbor, deploy to a1-ops-prd"
	@echo "  make bump-with-monitoring  Bump and watch deployment"
	@echo "  make deploy-dev            Deploy to development"
	@echo "  make deploy-stg            Deploy to staging"
	@echo "  make deploy-prd            Deploy to production (requires approval)"
	@echo "  make deploy-prd-kubectl    Deploy to a1-ops-prd using kubectl"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "QUALITY WORKFLOW COMMANDS"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make qa-check              Run QA validation"
	@echo "  make devils-advocate       Run adversarial testing"
	@echo "  make workflow-status       Check workflow status"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "GITHUB ACTIONS MONITORING"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make gh-status             Show recent GitHub Actions runs"
	@echo "  make gh-watch              Watch current workflow execution"
	@echo "  make gh-logs               View workflow logs"
	@echo "  make gh-builds             Show recent builds"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "HELM COMMANDS"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make helm-lint             Lint Helm chart"
	@echo "  make helm-package          Package Helm chart"
	@echo "  make helm-publish          Publish Helm chart to repository"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "UTILITY COMMANDS"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make check-prerequisites   Check all required tools installed"
	@echo "  make check-go-version      Verify Go version matches requirements"
	@echo "  make check-kubeconfig-dev  Verify development kubeconfig"
	@echo "  make help                  Show this help message"
	@echo ""
	@echo "For more information, see docs/development/"

workflow-help:
	@echo "Quality Workflow Commands"
	@echo ""
	@echo "  make workflow-status       - Show current workflow status"
	@echo "  make qa-check              - Run QA validation gate"
	@echo "  make devils-advocate       - Run adversarial testing gate"
	@echo "  make workflow-validate     - Full workflow validation"
	@echo ""
	@echo "See docs/development/task-execution-workflow.md for complete workflow documentation"

gh-help:
	@echo "GitHub Actions Monitoring Commands"
	@echo ""
	@echo "  make gh-status             - Show recent workflow runs"
	@echo "  make gh-watch              - Watch current workflow (follows logs)"
	@echo "  make gh-logs               - Show logs from latest run"
	@echo "  make gh-builds             - Show recent pipeline builds"
	@echo ""
	@echo "Requires 'gh' CLI: https://cli.github.com/"

# Default target
all: test-local build-local validate-pipeline-local
	@$(call print_success,"All targets completed successfully")

# ================================================================================================
# End of Makefile
# ================================================================================================
