#!/usr/bin/env bash
# Runs the checks from .github/workflows/ci.yml against the local tree.
set -uo pipefail

usage() {
    cat <<EOF
Usage: $0 [--quick] [--component=node-doctor]

  --quick                  Skip go test
  --component=node-doctor  Accepted for compatibility; node-doctor is the only component
  SKIP_LINT=1              Continue when golangci-lint is not installed
EOF
}

QUICK=false
for arg in "$@"; do
    case "$arg" in
        --quick) QUICK=true ;;
        --component=node-doctor) ;;
        --component=*) echo "Unknown component '${arg#*=}'; only node-doctor exists" >&2; exit 1 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "Unknown option: $arg" >&2; usage >&2; exit 1 ;;
    esac
done

cd "$(dirname "$0")/.."

PASSED=()
FAILED=()
SKIPPED=()

run() {
    local name=$1
    shift
    echo "==> $name"
    if "$@"; then
        PASSED+=("$name")
    else
        FAILED+=("$name")
        echo "FAILED: $name" >&2
    fi
}

skip() {
    SKIPPED+=("$1 ($2)")
    echo "==> $1 skipped: $2"
}

check_gofmt() {
    local unformatted
    unformatted=$(find . -name '*.go' -not -path './.*' -not -path './vendor/*' -print0 | xargs -0 gofmt -l)
    [ -z "$unformatted" ] && return 0
    echo "gofmt -l reports unformatted files:"
    echo "$unformatted"
    return 1
}

check_vet() {
    GOOS=linux go vet ./...
}

check_lint() {
    golangci-lint run --timeout=5m
}

check_gosec() {
    gosec -quiet ./...
}

check_tests() {
    go test -short -race ./pkg/... ./cmd/... ./config/...
}

check_helm_lint() {
    helm lint helm/node-doctor
}

check_helm_generated() {
    make -s helm-verify-generated
}

run gofmt check_gofmt
run "go vet (GOOS=linux)" check_vet

if command -v golangci-lint >/dev/null 2>&1; then
    run golangci-lint check_lint
elif [ "${SKIP_LINT:-0}" = "1" ]; then
    skip golangci-lint "not installed, SKIP_LINT=1"
else
    echo "==> golangci-lint"
    echo "golangci-lint is not installed. Install it (make install-deps) or set SKIP_LINT=1." >&2
    FAILED+=("golangci-lint")
fi

if command -v gosec >/dev/null 2>&1; then
    run gosec check_gosec
else
    skip gosec "not installed"
fi

if [ "$QUICK" = true ]; then
    skip "go test" "--quick"
else
    run "go test" check_tests
fi

run "helm lint" check_helm_lint
run helm-verify-generated check_helm_generated

echo
echo "Summary"
for name in "${PASSED[@]}"; do echo "  PASS  $name"; done
for name in "${SKIPPED[@]}"; do echo "  SKIP  $name"; done
for name in "${FAILED[@]}"; do echo "  FAIL  $name"; done

if [ ${#FAILED[@]} -gt 0 ]; then
    echo
    echo "${#FAILED[@]} check(s) failed." >&2
    exit 1
fi
echo
echo "All checks passed."
