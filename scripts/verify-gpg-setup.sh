#!/bin/bash
set -e

# verify-gpg-setup.sh
# Verifies GPG signing configuration for GitHub Actions

echo "🔍 Verifying GPG Setup for Node Doctor Release Automation"
echo "================================================================"
echo ""

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

ERRORS=0
WARNINGS=0

# Check 1: Verify GPG key exists locally
echo "1️⃣  Checking local GPG key..."
if gpg --list-secret-keys 1AF32A8B0481A7F3 > /dev/null 2>&1; then
    echo -e "   ${GREEN}✅ GPG key 1AF32A8B0481A7F3 found locally${NC}"

    # Get key details
    KEY_CREATED=$(gpg --list-secret-keys --with-colons 1AF32A8B0481A7F3 | grep ^sec | cut -d: -f6)
    KEY_EMAIL=$(gpg --list-secret-keys 1AF32A8B0481A7F3 | grep uid | grep -oP '<.*>' | tr -d '<>')
    echo "   📧 Email: $KEY_EMAIL"
    echo "   📅 Created: $(date -d @$KEY_CREATED 2>/dev/null || date -r $KEY_CREATED 2>/dev/null || echo 'Unknown')"
else
    echo -e "   ${RED}❌ GPG key 1AF32A8B0481A7F3 NOT found locally${NC}"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# Check 2: Verify GitHub secrets are configured
echo "2️⃣  Checking GitHub repository secrets..."
if command -v gh &> /dev/null; then
    SECRET_LIST=$(gh secret list 2>&1)
    if echo "$SECRET_LIST" | grep -q "404"; then
        echo -e "   ${YELLOW}⚠️  Cannot verify secrets (permission issue)${NC}"
        echo "   Run manually: gh secret list"
        WARNINGS=$((WARNINGS + 1))
    else
        # Check for GPG_PRIVATE_KEY
        if echo "$SECRET_LIST" | grep -q "GPG_PRIVATE_KEY"; then
            echo -e "   ${GREEN}✅ GPG_PRIVATE_KEY secret is configured${NC}"
        else
            echo -e "   ${RED}❌ GPG_PRIVATE_KEY secret NOT configured${NC}"
            ERRORS=$((ERRORS + 1))
        fi

        # Check for GPG_PASSPHRASE
        if echo "$SECRET_LIST" | grep -q "GPG_PASSPHRASE"; then
            echo -e "   ${GREEN}✅ GPG_PASSPHRASE secret is configured${NC}"
        else
            echo -e "   ${RED}❌ GPG_PASSPHRASE secret NOT configured${NC}"
            ERRORS=$((ERRORS + 1))
        fi
    fi
else
    echo -e "   ${YELLOW}⚠️  GitHub CLI (gh) not installed - skipping secret check${NC}"
    WARNINGS=$((WARNINGS + 1))
fi
echo ""

# Check 3: Verify key can be exported (test without actually exporting)
echo "3️⃣  Checking if key can be exported..."
if gpg --list-secret-keys 1AF32A8B0481A7F3 > /dev/null 2>&1; then
    echo -e "   ${GREEN}✅ Key is accessible for export${NC}"
    echo "   ℹ️  To export: gpg --armor --export-secret-key 1AF32A8B0481A7F3 > /tmp/gpg-private-key.asc"
else
    echo -e "   ${RED}❌ Cannot access key for export${NC}"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# Check 4: Verify release workflow configuration
echo "4️⃣  Checking release workflow GPG configuration..."
RELEASE_WORKFLOW=".github/workflows/release.yml"
if [ -f "$RELEASE_WORKFLOW" ]; then
    if grep -q "GPG_PRIVATE_KEY" "$RELEASE_WORKFLOW"; then
        echo -e "   ${GREEN}✅ Release workflow references GPG_PRIVATE_KEY${NC}"
    else
        echo -e "   ${RED}❌ Release workflow missing GPG_PRIVATE_KEY reference${NC}"
        ERRORS=$((ERRORS + 1))
    fi

    if grep -q "GPG_PASSPHRASE" "$RELEASE_WORKFLOW"; then
        echo -e "   ${GREEN}✅ Release workflow references GPG_PASSPHRASE${NC}"
    else
        echo -e "   ${RED}❌ Release workflow missing GPG_PASSPHRASE reference${NC}"
        ERRORS=$((ERRORS + 1))
    fi

    # Check for GPG signing step
    if grep -q "gpg --batch --import" "$RELEASE_WORKFLOW"; then
        echo -e "   ${GREEN}✅ GPG key import step configured${NC}"
    else
        echo -e "   ${YELLOW}⚠️  GPG key import step not found${NC}"
        WARNINGS=$((WARNINGS + 1))
    fi

    # Check for GPG signing
    if grep -q "gpg.*--detach-sign" "$RELEASE_WORKFLOW"; then
        echo -e "   ${GREEN}✅ GPG artifact signing configured${NC}"
    else
        echo -e "   ${YELLOW}⚠️  GPG artifact signing not found${NC}"
        WARNINGS=$((WARNINGS + 1))
    fi
else
    echo -e "   ${RED}❌ Release workflow not found at $RELEASE_WORKFLOW${NC}"
    ERRORS=$((ERRORS + 1))
fi
echo ""

# Check 5: Test GPG signing capability (if key is available)
echo "5️⃣  Testing GPG signing capability..."
TEST_FILE="/tmp/gpg-test-$$"
echo "Test content for GPG signing verification" > "$TEST_FILE"

if gpg --list-secret-keys 1AF32A8B0481A7F3 > /dev/null 2>&1; then
    # Try to sign with --batch (will fail if passphrase needed)
    if echo "test-passphrase" | gpg --batch --yes --passphrase-fd 0 --armor --detach-sign --output "${TEST_FILE}.asc" "$TEST_FILE" 2>/dev/null; then
        echo -e "   ${YELLOW}⚠️  GPG signing works but test passphrase was accepted${NC}"
        echo "   ℹ️  Make sure to use your actual passphrase in GitHub secrets"
        WARNINGS=$((WARNINGS + 1))
        rm -f "${TEST_FILE}.asc"
    else
        echo -e "   ${GREEN}✅ GPG key requires passphrase (as expected)${NC}"
        echo "   ℹ️  GitHub Actions will use GPG_PASSPHRASE secret during release"
    fi
else
    echo -e "   ${YELLOW}⚠️  Cannot test signing - key not accessible${NC}"
    WARNINGS=$((WARNINGS + 1))
fi
rm -f "$TEST_FILE"
echo ""

# Summary
echo "================================================================"
echo "📊 Verification Summary"
echo "================================================================"
if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
    echo -e "${GREEN}✅ ALL CHECKS PASSED!${NC}"
    echo ""
    echo "GPG signing is properly configured. Next steps:"
    echo "1. Test with a pre-release: git tag v0.1.0-rc.1 && git push origin v0.1.0-rc.1"
    echo "2. Monitor the release workflow in GitHub Actions"
    echo "3. Verify dual signatures are created (.cosign.sig and .asc files)"
elif [ $ERRORS -eq 0 ]; then
    echo -e "${YELLOW}⚠️  PASSED WITH WARNINGS ($WARNINGS warnings)${NC}"
    echo ""
    echo "GPG configuration is mostly ready, but review warnings above."
else
    echo -e "${RED}❌ VERIFICATION FAILED ($ERRORS errors, $WARNINGS warnings)${NC}"
    echo ""
    echo "Please address the errors above before proceeding with releases."
    exit 1
fi

echo ""
echo "================================================================"
echo "📚 Quick Reference"
echo "================================================================"
echo ""
echo "Export GPG private key:"
echo "  gpg --armor --export-secret-key 1AF32A8B0481A7F3 > /tmp/gpg-private-key.asc"
echo ""
echo "Add GitHub secrets (manual via UI):"
echo "  1. Go to: https://github.com/supporttools/node-doctor/settings/secrets/actions"
echo "  2. Click 'New repository secret'"
echo "  3. Name: GPG_PRIVATE_KEY"
echo "     Value: <paste contents of /tmp/gpg-private-key.asc>"
echo "  4. Click 'New repository secret'"
echo "  5. Name: GPG_PASSPHRASE"
echo "     Value: <your GPG passphrase>"
echo ""
echo "Or add via CLI:"
echo "  gh secret set GPG_PRIVATE_KEY < /tmp/gpg-private-key.asc"
echo "  gh secret set GPG_PASSPHRASE  # will prompt for value"
echo ""
echo "Verify secrets:"
echo "  gh secret list"
echo ""
