#!/usr/bin/env bash
# Runs the same checks as .github/workflows/ci.yml. Tools are never auto-installed.
set -uo pipefail

QUICK=false
FAIL_FAST=false

usage() {
    cat <<EOF
Usage: $0 [--quick] [--fail-fast]

  --quick      Format, vet, lint and Helm checks only (skips tests)
  --fail-fast  Stop at the first failed check
EOF
}

for arg in "$@"; do
    case $arg in
        --quick) QUICK=true ;;
        --fail-fast) FAIL_FAST=true ;;
        -h|--help) usage; exit 0 ;;
        *) echo "Unknown option: $arg"; usage; exit 1 ;;
    esac
done

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

PASSED=0
FAILED=0
SKIPPED=0
LOG_DIR=$(mktemp -d)
trap 'rm -rf "$LOG_DIR"' EXIT

run_check() {
    local name=$1
    shift
    echo -e "${BLUE}→${NC} $name"
    if "$@" >"$LOG_DIR/$name.log" 2>&1; then
        echo -e "${GREEN}✓ $name${NC}"
        PASSED=$((PASSED + 1))
    else
        echo -e "${RED}✗ $name${NC}"
        cat "$LOG_DIR/$name.log"
        FAILED=$((FAILED + 1))
        if [ "$FAIL_FAST" = true ]; then
            exit 1
        fi
    fi
}

skip_check() {
    echo -e "${YELLOW}- $1 skipped: $2${NC}"
    SKIPPED=$((SKIPPED + 1))
}

gofmt_clean() {
    local unformatted
    unformatted=$(gofmt -s -l $(git ls-files '*.go'))
    [ -z "$unformatted" ] || { echo "$unformatted"; return 1; }
}

# CI pins golangci-lint v1 because .golangci.yml is a v1 config; prefer that build over a v2 on PATH.
golangci_lint_bin() {
    local gopath_bin
    gopath_bin="$(go env GOPATH)/bin/golangci-lint"
    if [ -x "$gopath_bin" ]; then
        echo "$gopath_bin"
    elif command -v golangci-lint >/dev/null 2>&1; then
        command -v golangci-lint
    fi
}

run_check gofmt gofmt_clean
run_check go-vet env GOOS=linux go vet ./...

GCL=$(golangci_lint_bin)
if [ -n "$GCL" ]; then
    run_check golangci-lint "$GCL" run --timeout=5m
else
    skip_check golangci-lint "not installed (CI uses v1.64.8)"
fi

if command -v helm >/dev/null 2>&1 && command -v envsubst >/dev/null 2>&1; then
    run_check helm-generated make -s helm-verify-generated
    run_check helm-lint helm lint ./helm/node-doctor
else
    skip_check helm "helm or envsubst not installed"
fi

if [ "$QUICK" = false ]; then
    if [ "$(uname -s)" = Linux ]; then
        run_check unit-tests go test ./pkg/... ./cmd/... -race -short
        run_check integration-tests go test ./test/integration/... -race
    else
        skip_check tests "the agent only builds on Linux"
    fi
fi

echo ""
echo -e "Passed: ${GREEN}$PASSED${NC}  Failed: ${RED}$FAILED${NC}  Skipped: ${YELLOW}$SKIPPED${NC}"

if [ "$FAILED" -gt 0 ]; then
    echo -e "${RED}Validation failed. Fix the issues above before pushing.${NC}"
    exit 1
fi
echo -e "${GREEN}All checks passed.${NC}"
