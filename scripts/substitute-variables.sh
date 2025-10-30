#!/bin/bash
# Variable Substitution Script
#
# This script replaces template variables with your project values.
# It searches for {{VARIABLE}} patterns and replaces them with your values.

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}   Nexmonyx Template Variable Substitution${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

# Check if running in repository root
if [ ! -f "CLAUDE.md" ] || [ ! -d ".claude" ]; then
    echo -e "${RED}Error: Must run from repository root${NC}"
    exit 1
fi

# Function to prompt for variable
prompt_variable() {
    local var_name=$1
    local description=$2
    local default_value=$3
    local value

    echo -e "${YELLOW}${var_name}${NC}"
    echo -e "  ${description}"
    if [ -n "$default_value" ]; then
        read -p "  Enter value (default: ${default_value}): " value
        value=${value:-$default_value}
    else
        read -p "  Enter value: " value
    fi

    echo "$value"
}

# Collect variables from user
echo -e "${BLUE}Please provide values for template variables:${NC}"
echo ""

PROJECT_NAME=$(prompt_variable "PROJECT_NAME" "Your project name (lowercase, no spaces)" "my-project")
GITHUB_ORG=$(prompt_variable "GITHUB_ORG" "Your GitHub organization or username" "")
REGISTRY=$(prompt_variable "REGISTRY" "Container registry (e.g., ghcr.io/myorg, harbor.example.com/myproject)" "ghcr.io/${GITHUB_ORG}")
HELM_REPO=$(prompt_variable "HELM_REPO" "Helm chart repository URL" "https://charts.example.com")
PRIMARY_DOMAIN=$(prompt_variable "PRIMARY_DOMAIN" "Primary domain for your project" "example.com")
GO_VERSION=$(prompt_variable "GO_VERSION" "Go version" "1.24")

echo ""
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}   Summary of Values${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo "PROJECT_NAME: $PROJECT_NAME"
echo "GITHUB_ORG: $GITHUB_ORG"
echo "REGISTRY: $REGISTRY"
echo "HELM_REPO: $HELM_REPO"
echo "PRIMARY_DOMAIN: $PRIMARY_DOMAIN"
echo "GO_VERSION: $GO_VERSION"
echo ""

read -p "Proceed with substitution? (y/n): " confirm
if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
    echo -e "${YELLOW}Substitution cancelled${NC}"
    exit 0
fi

echo ""
echo -e "${BLUE}Performing substitutions...${NC}"

# Find all files that might contain variables (excluding binary files, .git, and node_modules)
files=$(find . -type f \
    -not -path "*/\.git/*" \
    -not -path "*/node_modules/*" \
    -not -path "*/bin/*" \
    -not -path "*/.claude/settings.local.json" \
    -not -name "*.png" \
    -not -name "*.jpg" \
    -not -name "*.jpeg" \
    -not -name "*.gif" \
    -not -name "*.ico" \
    -not -name "*.pdf" \
    -not -name "*.woff*" \
    -not -name "*.ttf" \
    -not -name "*.eot")

count=0

for file in $files; do
    # Check if file contains any template variables
    if grep -q "{{.*}}" "$file" 2>/dev/null; then
        echo -e "  ${GREEN}✓${NC} $file"

        # Perform substitutions (macOS and Linux compatible)
        if [[ "$OSTYPE" == "darwin"* ]]; then
            # macOS
            sed -i '' "s|{{PROJECT_NAME}}|$PROJECT_NAME|g" "$file"
            sed -i '' "s|{{GITHUB_ORG}}|$GITHUB_ORG|g" "$file"
            sed -i '' "s|{{REGISTRY}}|$REGISTRY|g" "$file"
            sed -i '' "s|{{HELM_REPO}}|$HELM_REPO|g" "$file"
            sed -i '' "s|{{PRIMARY_DOMAIN}}|$PRIMARY_DOMAIN|g" "$file"
            sed -i '' "s|{{GO_VERSION}}|$GO_VERSION|g" "$file"
        else
            # Linux
            sed -i "s|{{PROJECT_NAME}}|$PROJECT_NAME|g" "$file"
            sed -i "s|{{GITHUB_ORG}}|$GITHUB_ORG|g" "$file"
            sed -i "s|{{REGISTRY}}|$REGISTRY|g" "$file"
            sed -i "s|{{HELM_REPO}}|$HELM_REPO|g" "$file"
            sed -i "s|{{PRIMARY_DOMAIN}}|$PRIMARY_DOMAIN|g" "$file"
            sed -i "s|{{GO_VERSION}}|$GO_VERSION|g" "$file"
        fi

        ((count++))
    fi
done

echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}   Substitution Complete!${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}   Updated $count files${NC}"
echo ""
echo "Next steps:"
echo "1. Review the changes: git diff"
echo "2. Update additional project-specific files as needed"
echo "3. Initialize git: git init (if not already initialized)"
echo "4. Commit the changes: git add . && git commit -m \"Initialize from template\""
echo ""
