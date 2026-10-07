# node-doctor build, test, and local validation targets.
# Images and charts are published only by .github/workflows/release.yml.

.PHONY: help all test build \
	build-local test-local validate-local validate-pipeline-local validate-quick validate-component \
	workflow-status workflow-help gh-help \
	gh-status gh-watch gh-logs gh-builds \
	check-prerequisites check-docker check-kubectl check-go-version require-version \
	build test test-integration test-e2e test-all test-ci \
	test-net-icmp-integration \
	lint fmt clean install-deps \
	docker-build docker-push \
	helm-lint helm-package helm-generate helm-verify-generated \
	coverage-check coverage-threshold

# ================================================================================================
# Project Configuration
# ================================================================================================

# Project Configuration
PROJECT_NAME := node-doctor

# Container registry (DockerHub for production releases)
REGISTRY := docker.io/supporttools

# Version and build information
VERSION := $(shell date +%s)
GIT_COMMIT := $(shell git rev-parse HEAD)
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION := 1.25

export VERSION
export GIT_COMMIT
export BUILD_TIME
export GO_VERSION

CHART_VERSION := v$(shell git rev-list --count HEAD)

# ================================================================================================
# Component Configuration
# ================================================================================================

# Node Doctor binaries
COMPONENTS := node-doctor overlay-test-server node-doctor-controller

# Docker images
DOCKER_IMAGE_node-doctor := $(REGISTRY)/$(PROJECT_NAME)
DOCKER_IMAGE_overlay-test-server := $(REGISTRY)/$(PROJECT_NAME)-overlay-test
DOCKER_IMAGE_node-doctor-controller := $(REGISTRY)/$(PROJECT_NAME)-controller

# ================================================================================================
# Color Output Functions
# ================================================================================================

RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[1;33m
BLUE := \033[0;34m
NC := \033[0m

define print_status
printf '\033[0;34m[%s]\033[0m %s\n' "$$(date +'%Y-%m-%d %H:%M:%S')" $(1)
endef

define print_success
printf '\033[0;32m[SUCCESS]\033[0m %s\n' $(1)
endef

define print_error
printf '\033[0;31m[ERROR]\033[0m %s\n' $(1)
endef

define print_warning
printf '\033[1;33m[WARNING]\033[0m %s\n' $(1)
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

# Pushing images needs a real tag; the epoch default would litter the registry.
require-version:
ifeq ($(origin VERSION),file)
	@$(call print_error,"Set VERSION explicitly: make docker-push VERSION=1.2.3"); exit 1
endif

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

# Build overlay-test-server binary
build-overlay-test-server-local:
	@$(call print_status,"Building overlay-test-server locally...")
	@mkdir -p bin
	@cd cmd/overlay-test-server && go build -o ../../bin/overlay-test-server
	@$(call print_success,"overlay-test-server built: bin/overlay-test-server")

build-node-doctor-controller-local:
	@$(call print_status,"Building node-doctor-controller locally...")
	@mkdir -p bin
	@cd cmd/node-doctor-controller && go build -ldflags="-X main.Version=$(VERSION) -X main.GitCommit=$(GIT_COMMIT) -X main.BuildTime=$(BUILD_TIME)" -o ../../bin/node-doctor-controller
	@$(call print_success,"node-doctor-controller built: bin/node-doctor-controller")

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
# Standard Build and Test Targets (Task #3137)
# ================================================================================================

# Standard build target - compile binary
build: build-local

# Standard test target - unit tests only
test:
	@$(call print_status,"Running unit tests...")
	@go test ./pkg/... ./cmd/... -v -cover -short
	@$(call print_success,"Unit tests passed")

# Integration tests
test-integration:
	@$(call print_status,"Running integration tests...")
	@if [ -d "test/integration" ]; then \
		go test ./test/integration/... -v -cover; \
	else \
		$(call print_warning,"Integration tests not yet implemented (test/integration/ does not exist)"); \
	fi
	@$(call print_success,"Integration tests completed")

# CI gate: fast unit tests (-short) PLUS integration tests (no -short), so
# coverage gaps like TestLeaseCoordinationFlow / TestCorrelationDetectionFlow
# (in test/integration/controller/) are exercised. Build-tagged integration
# tests (e.g. the kind dual-stack test) still require -tags=integration and are
# not run here. Use this target in CI in place of `test` alone.
test-ci:
	@$(call print_status,"Running CI test gate (unit + integration)...")
	@go test ./pkg/... ./cmd/... -v -cover -short
	@if [ -d "test/integration" ]; then \
		go test ./test/integration/... -v -cover -timeout 10m; \
	else \
		$(call print_warning,"Integration tests not yet implemented (test/integration/ does not exist)"); \
	fi
	@$(call print_success,"CI test gate passed")

# E2E tests create a kind cluster; they need docker, kind, and kubectl.
# Env: E2E_KEEP_CLUSTER=1, E2E_EXPORT_LOGS=1, E2E_DEBUG=1 (see test/e2e/README.md).
test-e2e:
	@$(call print_status,"Running E2E tests (kind cluster)...")
	@command -v kind >/dev/null 2>&1 || ($(call print_error,"kind not found") && exit 1)
	@go test -tags=e2e ./test/e2e/... -v -timeout 30m
	@$(call print_success,"E2E tests completed")

# Run the real ICMP pinger integration test under privilege.
#
# The default pinger opens RAW ICMP sockets (CAP_NET_RAW), so this must run as
# root. We compile the test binary as the normal user first (preserving the Go
# environment / module cache) and then run ONLY this test under sudo with the
# integration env var set, so socket/permission failures are HARD failures
# instead of silent skips.
test-net-icmp-integration:
	@$(call print_status,"Compiling network test binary...")
	@go test -c -o /tmp/nd-network.test ./pkg/monitors/network/
	@$(call print_status,"Running ICMP integration test as root (CAP_NET_RAW)...")
	@sudo NODE_DOCTOR_ICMP_INTEGRATION=1 /tmp/nd-network.test -test.run '^TestDefaultPinger_Integration$$' -test.v
	@$(call print_success,"ICMP integration test passed")

# Run all tests with coverage
test-all:
	@$(call print_status,"Running all tests with coverage...")
	@mkdir -p coverage
	@go test ./... -v -cover -covermode=atomic -coverprofile=coverage/coverage.out
	@go tool cover -html=coverage/coverage.out -o coverage/coverage.html
	@go tool cover -func=coverage/coverage.out | grep total | awk '{print "Total coverage: " $$3}'
	@$(call print_success,"All tests passed - coverage report: coverage/coverage.html")

# Single source for the coverage gate; ci.yml calls coverage-threshold with its own profile.
COVERAGE_THRESHOLD := 70
COVERAGE_PROFILE ?= coverage/coverage.out

coverage-check:
	@$(call print_status,"Generating unit test coverage profile...")
	@mkdir -p $(dir $(COVERAGE_PROFILE))
	@go test ./pkg/... ./cmd/... -short -covermode=atomic -coverprofile=$(COVERAGE_PROFILE) > /dev/null
	@$(MAKE) coverage-threshold COVERAGE_PROFILE=$(COVERAGE_PROFILE)

coverage-threshold:
	@$(call print_status,"Checking $(COVERAGE_PROFILE) against minimum $(COVERAGE_THRESHOLD)%...")
	@[ -f $(COVERAGE_PROFILE) ] || { $(call print_error,"$(COVERAGE_PROFILE) not found"); exit 1; }
	@COVERAGE=$$(go tool cover -func=$(COVERAGE_PROFILE) | grep total | awk '{print $$3}' | sed 's/%//'); \
	[ -n "$$COVERAGE" ] || { $(call print_error,"Failed to extract coverage percentage"); exit 1; }; \
	echo "Current coverage: $${COVERAGE}%"; \
	if [ $$(echo "$${COVERAGE} < $(COVERAGE_THRESHOLD)" | bc -l) -eq 1 ]; then \
		$(call print_error,"Coverage $${COVERAGE}% is below minimum threshold of $(COVERAGE_THRESHOLD)%"); \
		exit 1; \
	fi
	@$(call print_success,"Coverage check passed (>= $(COVERAGE_THRESHOLD)%)")

# Lint code with golangci-lint
lint:
	@$(call print_status,"Running golangci-lint...")
	@command -v golangci-lint >/dev/null 2>&1 || ($(call print_error,"golangci-lint not found - run 'make install-deps'") && exit 1)
	@golangci-lint run ./...
	@$(call print_success,"Linting passed")

# Format code with gofmt and goimports
fmt:
	@$(call print_status,"Formatting Go code...")
	@command -v goimports >/dev/null 2>&1 || ($(call print_error,"goimports not found - run 'make install-deps'") && exit 1)
	@gofmt -s -w .
	@goimports -w .
	@$(call print_success,"Code formatted")

# Clean build artifacts
clean:
	@$(call print_status,"Cleaning build artifacts...")
	@rm -rf bin/
	@rm -rf coverage/
	@rm -f coverage.txt coverage.out
	@go clean -cache -testcache -modcache -fuzzcache
	@$(call print_success,"Build artifacts cleaned")

# Install development dependencies
install-deps:
	@$(call print_status,"Installing development dependencies...")
	@command -v golangci-lint >/dev/null 2>&1 || \
		($(call print_status,"Installing golangci-lint...") && \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	@command -v goimports >/dev/null 2>&1 || \
		($(call print_status,"Installing goimports...") && \
		go install golang.org/x/tools/cmd/goimports@latest)
	@$(call print_status,"Downloading Go module dependencies...")
	@go mod download
	@go mod tidy
	@$(call print_success,"Dependencies installed")

# Docker build shorthand
docker-build: build-all-images

# Docker push shorthand; never tags :latest, release.yml owns that
docker-push: require-version push-all-images

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
	@./scripts/validate-pipeline-local.sh --component=$(COMPONENT)
	@$(call print_success,"Component validation passed")

validate-local: validate-pipeline-local

# ================================================================================================
# Docker Image Build Targets
# ================================================================================================

# Build Docker image for node-doctor
build-node-doctor-image: check-docker
	@$(call print_status,"Building node-doctor Docker image...")
	@docker build -f Dockerfile -t $(DOCKER_IMAGE_node-doctor):$(VERSION) .
	@$(call print_success,"node-doctor image built: $(DOCKER_IMAGE_node-doctor):$(VERSION)")

# Build Docker image for overlay-test-server
build-overlay-test-server-image: check-docker
	@$(call print_status,"Building overlay-test-server Docker image...")
	@docker build -f Dockerfile.overlay-test -t $(DOCKER_IMAGE_overlay-test-server):$(VERSION) .
	@$(call print_success,"overlay-test-server image built: $(DOCKER_IMAGE_overlay-test-server):$(VERSION)")

build-node-doctor-controller-image: check-docker
	@$(call print_status,"Building node-doctor-controller Docker image...")
	@docker build -f Dockerfile.controller \
		--build-arg VERSION=$(VERSION) --build-arg GIT_COMMIT=$(GIT_COMMIT) --build-arg BUILD_TIME=$(BUILD_TIME) \
		-t $(DOCKER_IMAGE_node-doctor-controller):$(VERSION) .
	@docker tag $(DOCKER_IMAGE_node-doctor-controller):$(VERSION) $(DOCKER_IMAGE_node-doctor-controller):latest
	@$(call print_success,"node-doctor-controller image built: $(DOCKER_IMAGE_node-doctor-controller):$(VERSION)")

# Build all images
build-all-images: build-node-doctor-image build-overlay-test-server-image build-node-doctor-controller-image
	@$(call print_success,"Docker images built successfully")

# Push node-doctor image to registry
push-node-doctor-image: require-version
	@$(call print_status,"Pushing node-doctor image to registry...")
	@docker push $(DOCKER_IMAGE_node-doctor):$(VERSION)
	@$(call print_success,"node-doctor image pushed")

# Push overlay-test-server image to registry
push-overlay-test-server-image: require-version
	@$(call print_status,"Pushing overlay-test-server image to registry...")
	@docker push $(DOCKER_IMAGE_overlay-test-server):$(VERSION)
	@$(call print_success,"overlay-test-server image pushed")

push-node-doctor-controller-image:
	@$(call print_status,"Pushing node-doctor-controller image to registry...")
	@docker push $(DOCKER_IMAGE_node-doctor-controller):$(VERSION)
	@docker push $(DOCKER_IMAGE_node-doctor-controller):latest
	@$(call print_success,"node-doctor-controller image pushed")

push-all-images: push-node-doctor-image push-overlay-test-server-image push-node-doctor-controller-image
	@$(call print_success,"Images pushed to registry")

# ================================================================================================
# Helm Chart Targets
# ================================================================================================

# Chart.yaml and values.yaml are GENERATED from *.template — the release workflow
# (.github/workflows/release.yml) deletes both and re-renders them with envsubst before
# packaging, so the published chart NEVER contains hand-edits made directly to them.
# Edit the .template files, then run `make helm-generate`. CI enforces this via
# `make helm-verify-generated`.
#
# These placeholders stand in for the tag-derived values CI injects; they exist only so
# the committed copies are byte-reproducible and diffable.
#
# IMAGE_TAG is v-STRIPPED and APP_VERSION is not. That asymmetry is deliberate and mirrors
# release.yml: docker/metadata-action publishes `type=semver,pattern={{version}}`, so images
# land on Docker Hub as `1.8.7`, never `v1.8.7`, while Chart.yaml appVersion keeps the v as a
# human-facing release name. Keep these placeholders matching release.yml or the committed
# values.yaml stops being an accurate model of the shipped chart.
HELM_PLACEHOLDER_CHART_VERSION := 1.0.0
HELM_PLACEHOLDER_APP_VERSION   := v1.0.0
HELM_PLACEHOLDER_IMAGE_TAG     := 1.0.0

# Renders the templates the same way release.yml does (bare envsubst).
define helm_render
	CHART_VERSION="$(HELM_PLACEHOLDER_CHART_VERSION)" \
	APP_VERSION="$(HELM_PLACEHOLDER_APP_VERSION)" \
	IMAGE_TAG="$(HELM_PLACEHOLDER_IMAGE_TAG)" \
	envsubst < helm/$(PROJECT_NAME)/$(1).template > $(2)
endef

helm-generate:
	@$(call print_status,"Regenerating Chart.yaml and values.yaml from templates...")
	@$(call helm_render,Chart.yaml,helm/$(PROJECT_NAME)/Chart.yaml)
	@$(call helm_render,values.yaml,helm/$(PROJECT_NAME)/values.yaml)
	@$(call print_success,"Chart files regenerated - commit them")

helm-verify-generated:
	@$(call print_status,"Checking Chart.yaml/values.yaml match their templates...")
	@$(call helm_render,Chart.yaml,/tmp/node-doctor-Chart.rendered.yaml)
	@$(call helm_render,values.yaml,/tmp/node-doctor-values.rendered.yaml)
	@diff -u helm/$(PROJECT_NAME)/Chart.yaml /tmp/node-doctor-Chart.rendered.yaml || \
		{ $(call print_error,"Chart.yaml drifted from Chart.yaml.template - run 'make helm-generate'"); exit 1; }
	@diff -u helm/$(PROJECT_NAME)/values.yaml /tmp/node-doctor-values.rendered.yaml || \
		{ $(call print_error,"values.yaml drifted from values.yaml.template - run 'make helm-generate'"); exit 1; }
	@$(call print_success,"Chart files match their templates")

helm-lint: helm-verify-generated
	@$(call print_status,"Linting Helm chart...")
	@helm lint ./helm/$(PROJECT_NAME)
	@$(call print_success,"Helm lint passed")

helm-package:
	@$(call print_status,"Packaging Helm chart...")
	@helm package ./helm/$(PROJECT_NAME) --version $(CHART_VERSION)
	@$(call print_success,"Helm chart packaged")

# ================================================================================================
# Quality Workflow Targets
# ================================================================================================

workflow-status:
	@$(call print_status,"Checking workflow status...")
	@echo "TODO: Check TaskForge or project management system"

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
	@gh run list --workflow=ci.yml --limit 10

# ================================================================================================
# Help System
# ================================================================================================

help:
	@echo "$(PROJECT_NAME) Makefile"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "QUICK START"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make install-deps          Install development dependencies"
	@echo "  make build                 Compile node-doctor binary"
	@echo "  make test                  Run unit tests"
	@echo "  make test-all              Run all tests with coverage"
	@echo "  make lint                  Run linter (golangci-lint)"
	@echo "  make fmt                   Format code (gofmt + goimports)"
	@echo "  make clean                 Clean build artifacts"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "BUILD COMMANDS"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make build                 Compile binary (shorthand for build-local)"
	@echo "  make build-local           Build node-doctor binary"
	@echo "  make docker-build          Build Docker image"
	@echo "  make docker-push VERSION=x.y.z  Push Docker image (VERSION required, never :latest)"
	@echo "  make clean                 Remove build artifacts and caches"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "TEST COMMANDS"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make test                  Run unit tests (fast)"
	@echo "  make test-integration      Run integration tests"
	@echo "  make test-e2e              Run end-to-end tests (creates a kind cluster)"
	@echo "  make test-all              Run all tests with coverage report"
	@echo "  make coverage-check        Verify unit coverage >= $(COVERAGE_THRESHOLD)% threshold"
	@echo "  make lint                  Run golangci-lint"
	@echo "  make fmt                   Format code with gofmt and goimports"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "DEVELOPMENT COMMANDS"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make install-deps          Install golangci-lint, goimports, etc."
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "VALIDATION COMMANDS (Pre-push)"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make validate-pipeline-local  gofmt, vet, golangci-lint, gosec, tests, helm (mirrors CI)"
	@echo "  make validate-quick        Same without tests"
	@echo "  make validate-component COMPONENT=node-doctor  Same, explicit component"
	@echo "  SKIP_LINT=1                Allow a missing golangci-lint"
	@echo ""
	@echo "  Deployment is done by .github/workflows/release.yml on a v* tag; see docs/release-process.md"
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
	@echo "  make helm-generate         Render Chart.yaml/values.yaml from their templates"
	@echo "  make helm-verify-generated Fail if Chart.yaml/values.yaml drifted from templates"
	@echo "  make helm-lint             Lint Helm chart"
	@echo "  make helm-package          Package Helm chart"
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "UTILITY COMMANDS"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo ""
	@echo "  make check-prerequisites   Check all required tools installed"
	@echo "  make check-go-version      Verify Go version matches requirements"
	@echo "  make workflow-status       Check workflow status"
	@echo "  make help                  Show this help message"
	@echo ""
	@echo "For more information, see CONTRIBUTING.md"

workflow-help:
	@echo "Workflow Commands"
	@echo ""
	@echo "  make workflow-status       - Show current workflow status"

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
