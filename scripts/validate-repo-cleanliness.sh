#!/bin/bash

# Script to validate repository cleanliness
# Checks for files that shouldn't exist in certain locations

set -e

ERRORS=0

echo "Checking repository cleanliness..."

# Files that should NEVER be in the root directory
ROOT_BLACKLIST=(
    "*.go"           # Go files belong in proper directories
    "test-*"         # Test files
    "*test*"         # Any test files (except .gitignore, etc)
    "*.tmp"          # Temporary files
    "*.log"          # Log files
    "*.bak"          # Backup files
    "*~"             # Editor backup files
    "*.swp"          # Vim swap files
    "*.swo"          # Vim swap files
    "Dockerfile.*"   # Test Dockerfiles
    "*.out"          # Output files
    "*.exe"          # Executables
)

# Allowed files in root (whitelist)
ROOT_WHITELIST=(
    ".gitignore"
    ".gitattributes"
    ".validation.json"
    "README.md"
    "LICENSE"
    "CLAUDE.md"
    "Makefile"
    "makefile"
    "makefile.windows"
)

echo ""
echo "Checking root directory..."

# Check for blacklisted patterns
for pattern in "${ROOT_BLACKLIST[@]}"; do
    files=$(find . -maxdepth 1 -name "$pattern" -type f 2>/dev/null | grep -v "test-results" || true)
    if [ -n "$files" ]; then
        echo "❌ ERROR: Found prohibited files in root directory matching '$pattern':"
        echo "$files" | sed 's/^/   /'
        ERRORS=$((ERRORS + 1))
    fi
done

# Check for any Go files in root
go_files=$(find . -maxdepth 1 -name "*.go" -type f 2>/dev/null || true)
if [ -n "$go_files" ]; then
    echo "❌ ERROR: Go files should not be in the root directory:"
    echo "$go_files" | sed 's/^/   /'
    echo "   Move these files to appropriate packages"
    ERRORS=$((ERRORS + 1))
fi

# Check for compiled binaries
echo ""
echo "Checking for stray binaries..."
binaries=$(find . -type f -executable \
    -not -path "./.git/*" \
    -not -path "./bin/*" \
    -not -path "./scripts/*" \
    -not -path "./node_modules/*" \
    -not -path "./ui/node_modules/*" \
    -not -path "./vendor/*" \
    -not -name "*.sh" \
    -not -name "Makefile" \
    -not -name "makefile" \
    -exec file {} \; 2>/dev/null | grep -E "ELF|Mach-O|PE32" | cut -d: -f1 || true)

if [ -n "$binaries" ]; then
    echo "❌ ERROR: Found compiled binaries outside of bin/ directory:"
    echo "$binaries" | sed 's/^/   /'
    ERRORS=$((ERRORS + 1))
fi

# Check for temporary files
echo ""
echo "Checking for temporary files..."
temp_files=$(find . -type f \( \
    -name "*.tmp" -o \
    -name "*.temp" -o \
    -name "*.bak" -o \
    -name "*~" -o \
    -name "*.swp" -o \
    -name "*.swo" -o \
    -name ".DS_Store" -o \
    -name "Thumbs.db" \
    \) -not -path "./.git/*" 2>/dev/null || true)

if [ -n "$temp_files" ]; then
    echo "❌ ERROR: Found temporary files:"
    echo "$temp_files" | sed 's/^/   /'
    ERRORS=$((ERRORS + 1))
fi

# Check for test files in wrong locations
echo ""
echo "Checking test file locations..."
misplaced_tests=$(find . -name "*_test.go" \
    -not -path "./.git/*" \
    -not -path "./vendor/*" \
    -not -path "./*/vendor/*" | while read -r test_file; do
    dir=$(dirname "$test_file")
    # Test files should be next to the code they test
    base_file="${test_file%_test.go}.go"
    if [ ! -f "$base_file" ]; then
        # Check if it's in a test directory
        if [[ ! "$dir" =~ /test$ ]] && [[ ! "$dir" =~ /tests$ ]]; then
            echo "$test_file"
        fi
    fi
done)

if [ -n "$misplaced_tests" ]; then
    echo "⚠️  WARNING: Test files may be in wrong locations:"
    echo "$misplaced_tests" | sed 's/^/   /'
    echo "   Test files should be next to the code they test or in test/ directories"
fi

echo ""

if [ $ERRORS -gt 0 ]; then
    echo "❌ Repository cleanliness check FAILED with $ERRORS errors!"
    echo ""
    echo "To fix these issues, run: ./scripts/cleanup-repo.sh"
    echo ""
    echo "Rules for keeping the repository clean:"
    echo "1. No Go files in the root directory - use proper package structure"
    echo "2. No test files or temporary files in the root"
    echo "3. No compiled binaries outside of bin/ directory"
    echo "4. No temporary/backup files anywhere in the repo"
    echo "5. Test files should be next to the code they test"
    exit 1
else
    echo "✅ Repository is clean!"
fi