#!/bin/bash

# Script to validate that Go version is not downgraded
# This script should be run as part of CI/CD and pre-commit hooks

set -e

REQUIRED_GO_VERSION="1.21"
ERRORS=0

echo "Checking Go version in all go.mod files..."

# Find all go.mod files
while IFS= read -r mod_file; do
    # Extract Go version from go.mod
    go_version=$(grep "^go " "$mod_file" | awk '{print $2}' | sed 's/\.0$//')
    
    if [ -z "$go_version" ]; then
        echo "ERROR: No Go version found in $mod_file"
        ERRORS=$((ERRORS + 1))
        continue
    fi
    
    # Compare versions
    if [ "$go_version" != "$REQUIRED_GO_VERSION" ] && [ "$go_version" != "${REQUIRED_GO_VERSION}.0" ]; then
        echo "ERROR: $mod_file has Go version $go_version, expected $REQUIRED_GO_VERSION"
        ERRORS=$((ERRORS + 1))
    else
        echo "✓ $mod_file: Go $go_version"
    fi
done < <(find . -name "go.mod" -type f -not -path "./vendor/*" -not -path "./.git/*")

echo ""
echo "Checking Go version in all Dockerfiles..."

# Find all Dockerfiles
while IFS= read -r dockerfile; do
    # Check for golang base images
    go_images=$(grep -E "FROM\s+golang:" "$dockerfile" || true)
    
    if [ -n "$go_images" ]; then
        while IFS= read -r image_line; do
            # Extract version from golang:X.Y-alpine or similar
            version=$(echo "$image_line" | grep -oE "golang:[0-9]+\.[0-9]+" | cut -d: -f2)
            
            if [ -n "$version" ] && [ "$version" != "$REQUIRED_GO_VERSION" ]; then
                echo "ERROR: $dockerfile uses golang:$version, expected golang:$REQUIRED_GO_VERSION"
                ERRORS=$((ERRORS + 1))
            elif [ -n "$version" ]; then
                echo "✓ $dockerfile: golang:$version"
            fi
        done <<< "$go_images"
    fi
done < <(find . -name "Dockerfile*" -type f -not -path "./vendor/*" -not -path "./*/vendor/*" -not -path "./.git/*")

echo ""

if [ $ERRORS -gt 0 ]; then
    echo "❌ Found $ERRORS Go version errors!"
    echo ""
    echo "All Go code must use Go version $REQUIRED_GO_VERSION"
    echo "Please update:"
    echo "  - go.mod files: change 'go X.Y' to 'go $REQUIRED_GO_VERSION'"
    echo "  - Dockerfiles: change 'FROM golang:X.Y' to 'FROM golang:$REQUIRED_GO_VERSION'"
    exit 1
else
    echo "✅ All Go versions are correct!"
fi