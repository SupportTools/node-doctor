#!/bin/bash
# validate-pipeline-local.sh
# Mirrors CI/CD pipeline validation exactly for local testing
# This script validates node-doctor with the same checks as .github/workflows/ci.yml

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Counters for summary
TOTAL_CHECKS=0
PASSED_CHECKS=0
FAILED_CHECKS=0

# Components to validate (node-doctor is a single binary)
COMPONENTS=("node-doctor")

# Parse command line arguments
SPECIFIC_COMPONENT=""
QUICK_MODE=false
PARALLEL_MODE=true
FAIL_FAST=false

usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Validates node-doctor using the same checks as CI/CD pipeline.

OPTIONS:
    --component=NAME    Validate only specific component (node-doctor)
    --quick            Skip tests and gosec (format, vet, staticcheck only)
    --sequential       Run components sequentially instead of parallel
    --fail-fast        Stop on first failure
    -h, --help         Show this help message

EXAMPLES:
    $0                              # Validate node-doctor
    $0 --component=node-doctor      # Validate node-doctor (explicit)
    $0 --quick                      # Quick validation (no tests)
    $0 --fail-fast                  # Stop on first error

EOF
}

# Parse arguments
for arg in "$@"; do
    case $arg in
        --component=*)
            SPECIFIC_COMPONENT="${arg#*=}"
            ;;
        --quick)
            QUICK_MODE=true
            ;;
        --sequential)
            PARALLEL_MODE=false
            ;;
        --fail-fast)
            FAIL_FAST=true
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $arg${NC}"
            usage
            exit 1
            ;;
    esac
done

# Validate specific component if provided
if [ -n "$SPECIFIC_COMPONENT" ]; then
    VALID_COMPONENT=false
    for comp in "${COMPONENTS[@]}"; do
        if [ "$comp" = "$SPECIFIC_COMPONENT" ]; then
            VALID_COMPONENT=true
            break
        fi
    done

    if [ "$VALID_COMPONENT" = false ]; then
        echo -e "${RED}Error: Invalid component '$SPECIFIC_COMPONENT'${NC}"
        echo "Valid components: ${COMPONENTS[*]}"
        exit 1
    fi

    COMPONENTS=("$SPECIFIC_COMPONENT")
fi

# Print header
echo -e "${BLUE}╔════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║       Local CI/CD Pipeline Validation                         ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${BLUE}Components:${NC} ${COMPONENTS[*]}"
echo -e "${BLUE}Quick Mode:${NC} $QUICK_MODE"
echo -e "${BLUE}Parallel:${NC} $PARALLEL_MODE"
echo -e "${BLUE}Fail Fast:${NC} $FAIL_FAST"
echo ""

# Check for required tools
echo -e "${BLUE}Checking required tools...${NC}"
MISSING_TOOLS=()

if ! command -v gofmt &> /dev/null; then
    MISSING_TOOLS+=("gofmt")
fi

if ! command -v go &> /dev/null; then
    MISSING_TOOLS+=("go")
else
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    if [[ ! "$GO_VERSION" =~ ^1\.24 ]]; then
        echo -e "${YELLOW}Warning: Go version $GO_VERSION detected. CI/CD uses Go 1.24${NC}"
    fi
fi

if ! command -v staticcheck &> /dev/null; then
    echo -e "${YELLOW}Installing staticcheck...${NC}"
    go install honnef.co/go/tools/cmd/staticcheck@latest
fi

if [ "$QUICK_MODE" = false ] && ! command -v gosec &> /dev/null; then
    echo -e "${YELLOW}Installing gosec...${NC}"
    go install github.com/securego/gosec/v2/cmd/gosec@latest
fi

if [ ${#MISSING_TOOLS[@]} -gt 0 ]; then
    echo -e "${RED}Error: Missing required tools: ${MISSING_TOOLS[*]}${NC}"
    exit 1
fi

echo -e "${GREEN}✓ All required tools available${NC}"
echo ""

# Function to run a validation check
run_check() {
    local component=$1
    local check_name=$2
    local command=$3

    TOTAL_CHECKS=$((TOTAL_CHECKS + 1))

    echo -e "${BLUE}[$component]${NC} Running $check_name..."

    if eval "$command" > /tmp/validate_${component}_${check_name}.log 2>&1; then
        echo -e "${GREEN}✓ [$component] $check_name passed${NC}"
        PASSED_CHECKS=$((PASSED_CHECKS + 1))
        return 0
    else
        echo -e "${RED}✗ [$component] $check_name FAILED${NC}"
        cat /tmp/validate_${component}_${check_name}.log
        FAILED_CHECKS=$((FAILED_CHECKS + 1))

        if [ "$FAIL_FAST" = true ]; then
            echo -e "${RED}Fail-fast enabled. Stopping validation.${NC}"
            exit 1
        fi

        return 1
    fi
}

# Function to validate a single component
validate_component() {
    local component=$1
    local component_failed=false

    echo -e "${BLUE}════════════════════════════════════════════════════════════════${NC}"
    echo -e "${BLUE}Validating: $component${NC}"
    echo -e "${BLUE}════════════════════════════════════════════════════════════════${NC}"

    # Step 1: Format check (gofmt)
    case "$component" in
        api)
            run_check "$component" "format" "test -z \"\$(find api -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)\"" || component_failed=true
            ;;
        pkg)
            run_check "$component" "format" "test -z \"\$(find pkg -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)\"" || component_failed=true
            ;;
        org-management-controller)
            run_check "$component" "format" "test -z \"\$(find pkg/controller/org-management -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)\"" || component_failed=true
            ;;
        probe-controller)
            run_check "$component" "format" "test -z \"\$(find pkg/probe -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)\"" || component_failed=true
            ;;
        ingestor)
            run_check "$component" "format" "test -z \"\$(find pkg/ingestor -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)\"" || component_failed=true
            ;;
        customer-probe-agent)
            run_check "$component" "format" "test -z \"\$(find cmd/customer-probe-agent -name '*.go' -not -path '*/vendor/*' -exec gofmt -l {} +)\"" || component_failed=true
            ;;
    esac

    # Step 2: Go vet
    case "$component" in
        api)
            run_check "$component" "go-vet" "(cd api && go vet ./...)" || component_failed=true
            ;;
        pkg)
            run_check "$component" "go-vet" "go vet ./pkg/..." || component_failed=true
            ;;
        org-management-controller)
            run_check "$component" "go-vet" "go vet ./pkg/controller/org-management/..." || component_failed=true
            ;;
        probe-controller)
            run_check "$component" "go-vet" "go vet ./pkg/probe/..." || component_failed=true
            ;;
        ingestor)
            run_check "$component" "go-vet" "go vet ./pkg/ingestor/..." || component_failed=true
            ;;
        customer-probe-agent)
            # NOTE: This is the CORRECT way - CI/CD workflow has a bug here
            run_check "$component" "go-vet" "(cd cmd/customer-probe-agent && go vet ./...)" || component_failed=true
            ;;
    esac

    # Step 3: Staticcheck
    case "$component" in
        api)
            run_check "$component" "staticcheck" "(cd api && staticcheck ./...)" || component_failed=true
            ;;
        pkg)
            run_check "$component" "staticcheck" "staticcheck ./pkg/..." || component_failed=true
            ;;
        org-management-controller)
            run_check "$component" "staticcheck" "staticcheck ./pkg/controller/org-management/..." || component_failed=true
            ;;
        probe-controller)
            run_check "$component" "staticcheck" "staticcheck ./pkg/probe/..." || component_failed=true
            ;;
        ingestor)
            run_check "$component" "staticcheck" "staticcheck ./pkg/ingestor/..." || component_failed=true
            ;;
        customer-probe-agent)
            # NOTE: This is the CORRECT way - CI/CD workflow has a bug here
            run_check "$component" "staticcheck" "(cd cmd/customer-probe-agent && staticcheck ./...)" || component_failed=true
            ;;
    esac

    # Step 4: Gosec (skip in quick mode)
    if [ "$QUICK_MODE" = false ]; then
        case "$component" in
            api)
                run_check "$component" "gosec" "(cd api && gosec -quiet ./...)" || component_failed=true
                ;;
            pkg)
                run_check "$component" "gosec" "gosec -quiet ./pkg/..." || component_failed=true
                ;;
            org-management-controller)
                run_check "$component" "gosec" "gosec -quiet ./pkg/controller/org-management/..." || component_failed=true
                ;;
            probe-controller)
                run_check "$component" "gosec" "gosec -quiet ./pkg/probe/..." || component_failed=true
                ;;
            ingestor)
                run_check "$component" "gosec" "gosec -quiet ./pkg/ingestor/..." || component_failed=true
                ;;
            customer-probe-agent)
                # NOTE: This is the CORRECT way - CI/CD workflow has a bug here
                run_check "$component" "gosec" "(cd cmd/customer-probe-agent && gosec -quiet ./...)" || component_failed=true
                ;;
        esac
    fi

    # Step 5: Unit tests with race detection (skip in quick mode)
    if [ "$QUICK_MODE" = false ]; then
        case "$component" in
            api)
                run_check "$component" "tests" "(cd api && go test -v -race -coverprofile=coverage.out -covermode=atomic ./...)" || component_failed=true
                ;;
            org-management-controller)
                run_check "$component" "tests" "go test -v -race -coverprofile=coverage.out -covermode=atomic ./pkg/controller/org-management/..." || component_failed=true
                ;;
            probe-controller)
                run_check "$component" "tests" "go test -v -race -coverprofile=coverage.out -covermode=atomic ./pkg/probe/..." || component_failed=true
                ;;
            ingestor)
                run_check "$component" "tests" "go test -v -race -coverprofile=coverage.out -covermode=atomic ./pkg/ingestor/..." || component_failed=true
                ;;
            customer-probe-agent)
                run_check "$component" "tests" "(cd cmd/customer-probe-agent && go test -v -race -coverprofile=coverage.out -covermode=atomic ./...)" || component_failed=true
                ;;
            pkg)
                run_check "$component" "tests" "go test -v -race -coverprofile=coverage.out -covermode=atomic ./pkg/..." || component_failed=true
                ;;
        esac
    fi

    echo ""

    if [ "$component_failed" = true ]; then
        return 1
    fi
    return 0
}

# Main validation loop
echo -e "${BLUE}Starting validation...${NC}"
echo ""

COMPONENT_RESULTS=()

if [ "$PARALLEL_MODE" = true ] && [ ${#COMPONENTS[@]} -gt 1 ]; then
    echo -e "${YELLOW}Running components in parallel (max 6 workers)...${NC}"
    echo ""

    # Run components in parallel using background jobs
    PIDS=()
    for component in "${COMPONENTS[@]}"; do
        validate_component "$component" &
        PIDS+=($!)

        # Limit to 6 parallel workers
        if [ ${#PIDS[@]} -ge 6 ]; then
            wait -n
            PIDS=($(jobs -p))
        fi
    done

    # Wait for all remaining jobs
    for pid in "${PIDS[@]}"; do
        wait "$pid"
        COMPONENT_RESULTS+=($?)
    done
else
    # Sequential execution
    for component in "${COMPONENTS[@]}"; do
        validate_component "$component"
        COMPONENT_RESULTS+=($?)
    done
fi

# Cleanup temp files
rm -f /tmp/validate_*.log

# Print summary
echo -e "${BLUE}════════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}Validation Summary${NC}"
echo -e "${BLUE}════════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "Total checks:  $TOTAL_CHECKS"
echo -e "${GREEN}Passed:        $PASSED_CHECKS${NC}"
echo -e "${RED}Failed:        $FAILED_CHECKS${NC}"
echo ""

# Component-level results
echo -e "${BLUE}Component Results:${NC}"
for i in "${!COMPONENTS[@]}"; do
    component="${COMPONENTS[$i]}"
    result="${COMPONENT_RESULTS[$i]}"

    if [ "$result" -eq 0 ]; then
        echo -e "  ${GREEN}✓ $component${NC}"
    else
        echo -e "  ${RED}✗ $component${NC}"
    fi
done
echo ""

# Exit with appropriate code
if [ $FAILED_CHECKS -eq 0 ]; then
    echo -e "${GREEN}╔════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║  ✓ All validations passed! Safe to push.                      ║${NC}"
    echo -e "${GREEN}╚════════════════════════════════════════════════════════════════╝${NC}"
    exit 0
else
    echo -e "${RED}╔════════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${RED}║  ✗ Validation failed! Fix issues before pushing.              ║${NC}"
    echo -e "${RED}╚════════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${YELLOW}See logs above for details.${NC}"
    echo -e "${YELLOW}Run with --component=NAME to validate specific component.${NC}"
    echo -e "${YELLOW}Run with --quick for faster feedback (skips tests).${NC}"
    exit 1
fi
