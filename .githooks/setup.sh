#!/bin/bash
# Setup script to enable git hooks for validation

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo ""
echo -e "${BLUE}╔════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║  Git Hooks Setup for Nexmonyx                                 ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Check if already configured
CURRENT_HOOKS_PATH=$(git config --get core.hooksPath)

if [ "$CURRENT_HOOKS_PATH" = ".githooks" ]; then
    echo -e "${GREEN}✓ Git hooks are already configured!${NC}"
    echo ""
    echo "Current configuration:"
    echo "  core.hooksPath = .githooks"
    echo ""
    exit 0
fi

echo -e "${BLUE}This will configure Git to use local validation hooks:${NC}"
echo ""
echo "  • Pre-push hook: Validates all components before pushing"
echo "  • Prevents CI/CD failures by catching issues locally"
echo "  • Can be bypassed with: git push --no-verify"
echo ""

read -p "Enable git hooks? (y/n): " -n 1 -r
echo ""

if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo -e "${YELLOW}Setup cancelled.${NC}"
    echo ""
    exit 0
fi

# Configure git to use .githooks directory
git config core.hooksPath .githooks

echo ""
echo -e "${GREEN}╔════════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║  ✓ Git hooks configured successfully!                         ║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo "Configuration:"
echo "  core.hooksPath = .githooks"
echo ""
echo "Active hooks:"
echo "  • pre-push: Full validation before push"
echo ""
echo -e "${BLUE}To disable hooks:${NC}"
echo "  git config --unset core.hooksPath"
echo ""
echo -e "${BLUE}To bypass on specific push:${NC}"
echo "  git push --no-verify"
echo ""
