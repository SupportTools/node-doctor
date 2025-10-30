#!/bin/bash
# Template Initialization Script
#
# This script initializes a new project from the Nexmonyx repository template.
# It performs variable substitution, sets up git hooks, and validates the setup.

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}   Nexmonyx Template Initialization${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "This script will:"
echo "  1. Substitute template variables with your project values"
echo "  2. Set up git hooks for validation"
echo "  3. Validate the setup"
echo "  4. Provide next steps"
echo ""

# Check prerequisites
echo -e "${BLUE}[1/5] Checking prerequisites...${NC}"

command -v git >/dev/null 2>&1 || { echo -e "${RED}Error: git not found${NC}"; exit 1; }
command -v go >/dev/null 2>&1 || { echo -e "${YELLOW}Warning: go not found (required for Go projects)${NC}"; }

echo -e "${GREEN}✓ Prerequisites checked${NC}"
echo ""

# Run variable substitution
echo -e "${BLUE}[2/5] Variable substitution...${NC}"

if [ ! -f "scripts/substitute-variables.sh" ]; then
    echo -e "${RED}Error: substitute-variables.sh not found${NC}"
    exit 1
fi

chmod +x scripts/substitute-variables.sh
./scripts/substitute-variables.sh

echo ""

# Set up git hooks
echo -e "${BLUE}[3/5] Setting up git hooks...${NC}"

if [ -f ".githooks/setup.sh" ]; then
    chmod +x .githooks/setup.sh
    cd .githooks && ./setup.sh && cd ..
    echo -e "${GREEN}✓ Git hooks configured${NC}"
else
    echo -e "${YELLOW}Warning: Git hooks not found, skipping${NC}"
fi

echo ""

# Validate setup
echo -e "${BLUE}[4/5] Validating setup...${NC}"

# Check for remaining template variables
remaining=$(grep -r "{{.*}}" . \
    --exclude-dir=".git" \
    --exclude-dir="node_modules" \
    --exclude-dir="bin" \
    --exclude="*.png" --exclude="*.jpg" --exclude="*.jpeg" \
    2>/dev/null | wc -l | tr -d ' ')

if [ "$remaining" -gt 0 ]; then
    echo -e "${YELLOW}Warning: $remaining files still contain template variables${NC}"
    echo "  Run: grep -r '{{.*}}' . to find them"
else
    echo -e "${GREEN}✓ No remaining template variables found${NC}"
fi

echo ""

# Provide next steps
echo -e "${BLUE}[5/5] Next steps...${NC}"
echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}   Initialization Complete!${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "Your project has been initialized. Next steps:"
echo ""
echo "1. Review the changes:"
echo "   git diff"
echo ""
echo "2. Customize for your project:"
echo "   - Update CLAUDE.md with project-specific instructions"
echo "   - Add your specialized agents to .claude/agents/specialized/"
echo "   - Update makefile with your components"
echo "   - Configure .validation.json for your components"
echo ""
echo "3. Initialize or update git repository:"
echo "   git init                          # If new repository"
echo "   git add ."
echo "   git commit -m \"Initialize from Nexmonyx template\""
echo ""
echo "4. Set up remote repository (optional):"
echo "   git remote add origin https://github.com/YOUR_ORG/YOUR_REPO.git"
echo "   git push -u origin main"
echo ""
echo "5. Read the documentation:"
echo "   - docs/README.md                  # Documentation hub"
echo "   - docs/development/task-execution-workflow.md  # Development process"
echo "   - .claude/AGENT_GUIDE.md          # How to use agents"
echo ""
echo "6. Start development:"
echo "   make help                         # See available commands"
echo "   make build-local                  # Build your project"
echo "   make test-local                   # Run tests"
echo "   make validate-pipeline-local      # Validate before pushing"
echo ""
echo -e "${BLUE}For complete setup instructions, see: TEMPLATE_USAGE.md${NC}"
echo ""
