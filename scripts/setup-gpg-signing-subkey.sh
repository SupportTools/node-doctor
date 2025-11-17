#!/bin/bash
set -e

# setup-gpg-signing-subkey.sh
# Creates a dedicated signing subkey for GitHub Actions (safer than using main key)

echo "🔐 Setting up GPG Signing Subkey for Release Automation"
echo "================================================================"
echo ""
echo "WHY USE A SUBKEY?"
echo "- Protects your main GPG key from compromise"
echo "- Subkey can be revoked independently if GitHub is compromised"
echo "- Best practice for automated signing systems"
echo ""
echo "================================================================"
echo ""

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

MAIN_KEY_ID="1AF32A8B0481A7F3"

# Step 1: Verify main key exists
echo "1️⃣  Verifying main GPG key..."
if ! gpg --list-secret-keys "$MAIN_KEY_ID" > /dev/null 2>&1; then
    echo -e "${RED}❌ Main key $MAIN_KEY_ID not found${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Main key found${NC}"
echo ""

# Step 2: Check if signing subkey already exists
echo "2️⃣  Checking for existing signing subkey..."
EXISTING_SUBKEY=$(gpg --list-secret-keys --with-subkey-fingerprints "$MAIN_KEY_ID" | grep -A 1 "\[S\]" | tail -1 | awk '{print $1}' || echo "")

if [ -n "$EXISTING_SUBKEY" ]; then
    echo -e "${YELLOW}⚠️  Found existing signing subkey: $EXISTING_SUBKEY${NC}"
    echo ""
    read -p "Do you want to use this existing subkey? (y/n): " USE_EXISTING
    if [ "$USE_EXISTING" = "y" ]; then
        SUBKEY_ID="$EXISTING_SUBKEY"
        echo -e "${GREEN}✅ Using existing subkey${NC}"
        SKIP_CREATION=true
    else
        echo "Will create a new signing subkey..."
        SKIP_CREATION=false
    fi
else
    echo "No existing signing subkey found. Will create new one."
    SKIP_CREATION=false
fi
echo ""

# Step 3: Create signing subkey (if needed)
if [ "$SKIP_CREATION" != "true" ]; then
    echo "3️⃣  Creating dedicated signing subkey..."
    echo ""
    echo -e "${BLUE}ℹ️  You will be prompted to:${NC}"
    echo "   1. Enter your main key passphrase"
    echo "   2. Choose subkey type: Select '(4) RSA (sign only)'"
    echo "   3. Choose key size: Recommend 4096"
    echo "   4. Choose expiration: Recommend 2y (2 years)"
    echo ""
    read -p "Press Enter to continue..."
    echo ""

    # Interactive subkey creation
    gpg --expert --edit-key "$MAIN_KEY_ID" <<EOF
addkey
4
4096
2y
y
y
save
EOF

    # Get the new subkey ID
    SUBKEY_ID=$(gpg --list-secret-keys --with-subkey-fingerprints "$MAIN_KEY_ID" | grep -A 1 "\[S\]" | tail -1 | awk '{print $1}')

    if [ -z "$SUBKEY_ID" ]; then
        echo -e "${RED}❌ Failed to create subkey${NC}"
        exit 1
    fi

    echo ""
    echo -e "${GREEN}✅ Signing subkey created: $SUBKEY_ID${NC}"
else
    echo "3️⃣  Skipping subkey creation (using existing)"
fi
echo ""

# Step 4: Export ONLY the signing subkey
echo "4️⃣  Exporting signing subkey (NOT main key)..."
EXPORT_FILE="/tmp/gpg-signing-subkey-${SUBKEY_ID}.asc"

# Export subkey with '!' suffix to export only this specific subkey
if gpg --armor --export-secret-subkeys "${SUBKEY_ID}!" > "$EXPORT_FILE" 2>/dev/null; then
    echo -e "${GREEN}✅ Subkey exported to: $EXPORT_FILE${NC}"
    echo ""

    # Verify it's only the subkey (not main key)
    echo "5️⃣  Verifying export contains only subkey..."
    TEMP_KEYRING=$(mktemp -d)
    if gpg --homedir "$TEMP_KEYRING" --import "$EXPORT_FILE" 2>/dev/null; then
        # Check if main key secret is present
        if gpg --homedir "$TEMP_KEYRING" --list-secret-keys | grep -q "sec#"; then
            echo -e "${GREEN}✅ Export verified: Contains subkey only (main key stub)${NC}"
            echo "   The '#' after 'sec' means main key is NOT included (safe!)"
        else
            echo -e "${YELLOW}⚠️  Warning: Main key might be included${NC}"
        fi
    fi
    rm -rf "$TEMP_KEYRING"
else
    echo -e "${RED}❌ Failed to export subkey${NC}"
    exit 1
fi
echo ""

# Step 5: Create passphrase for automated signing
echo "6️⃣  Subkey passphrase setup..."
echo ""
echo -e "${BLUE}ℹ️  The subkey currently uses your main key's passphrase.${NC}"
echo "For GitHub Actions, you have two options:"
echo ""
echo "Option A (RECOMMENDED): Remove passphrase from subkey"
echo "  - Safer because subkey can be revoked independently"
echo "  - No passphrase to manage in GitHub Secrets"
echo "  - Still requires passphrase for main key operations"
echo ""
echo "Option B: Use main key passphrase in GitHub Secrets"
echo "  - Keep subkey passphrase protection"
echo "  - Must store passphrase in GitHub Secrets"
echo ""
read -p "Remove passphrase from subkey? (recommended: y/n): " REMOVE_PASS

if [ "$REMOVE_PASS" = "y" ]; then
    echo ""
    echo "Removing passphrase from signing subkey..."
    echo -e "${YELLOW}You will be prompted for your CURRENT passphrase, then press Enter (blank) for new passphrase${NC}"

    gpg --edit-key "$MAIN_KEY_ID" <<EOF
key $SUBKEY_ID
passwd


save
EOF

    # Re-export without passphrase
    gpg --armor --export-secret-subkeys "${SUBKEY_ID}!" > "$EXPORT_FILE"
    NEEDS_PASSPHRASE=false
    echo -e "${GREEN}✅ Passphrase removed from subkey${NC}"
else
    NEEDS_PASSPHRASE=true
    echo -e "${YELLOW}⚠️  Subkey still requires passphrase${NC}"
fi
echo ""

# Step 6: GitHub configuration instructions
echo "================================================================"
echo "7️⃣  GitHub Secrets Configuration"
echo "================================================================"
echo ""
echo -e "${GREEN}✅ Subkey ready for GitHub Actions!${NC}"
echo ""
echo "Next steps:"
echo ""
echo "1. Add the subkey to GitHub Secrets:"
echo ""
echo -e "   ${BLUE}Via GitHub CLI:${NC}"
echo "   gh secret set GPG_PRIVATE_KEY < $EXPORT_FILE"
echo ""
echo -e "   ${BLUE}Via GitHub UI:${NC}"
echo "   - Go to: https://github.com/supporttools/node-doctor/settings/secrets/actions"
echo "   - Click 'New repository secret'"
echo "   - Name: GPG_PRIVATE_KEY"
echo "   - Value: <paste contents of $EXPORT_FILE>"
echo ""

if [ "$NEEDS_PASSPHRASE" = "true" ]; then
    echo "2. Add passphrase to GitHub Secrets:"
    echo ""
    echo -e "   ${BLUE}Via GitHub CLI:${NC}"
    echo "   gh secret set GPG_PASSPHRASE  # will prompt for value"
    echo ""
    echo -e "   ${BLUE}Via GitHub UI:${NC}"
    echo "   - Click 'New repository secret'"
    echo "   - Name: GPG_PASSPHRASE"
    echo "   - Value: <your main key passphrase>"
    echo ""
else
    echo "2. No passphrase needed (you removed it from the subkey)"
    echo ""
fi

echo "3. Publish public key for verification:"
echo ""
echo "   gpg --armor --export $MAIN_KEY_ID > node-doctor-signing-key.asc"
echo "   # Upload to: docs/signing-keys/node-doctor-signing-key.asc"
echo "   # Or publish to keyserver:"
echo "   gpg --send-keys $MAIN_KEY_ID"
echo ""

echo "4. Update release documentation with key ID:"
echo "   Main key ID: $MAIN_KEY_ID"
echo "   Signing subkey: $SUBKEY_ID"
echo ""

echo "================================================================"
echo "🔐 Security Notes"
echo "================================================================"
echo ""
echo -e "${GREEN}✅ Your main GPG key is safe!${NC}"
echo "   - Main key never leaves your machine"
echo "   - Only signing subkey is uploaded to GitHub"
echo "   - Subkey can be revoked independently if needed"
echo ""
echo -e "${BLUE}ℹ️  To revoke the subkey later (if GitHub is compromised):${NC}"
echo "   gpg --edit-key $MAIN_KEY_ID"
echo "   > key $SUBKEY_ID"
echo "   > revkey"
echo "   > save"
echo ""
echo -e "${YELLOW}⚠️  Remember to securely delete the exported file after uploading:${NC}"
echo "   shred -u $EXPORT_FILE"
echo ""

# Step 7: Verification
echo "================================================================"
echo "8️⃣  Verification"
echo "================================================================"
echo ""
echo "To verify the setup works:"
echo ""
echo "1. Run the verification script:"
echo "   ./scripts/verify-gpg-setup.sh"
echo ""
echo "2. Test with a pre-release tag:"
echo "   git tag v0.1.0-rc.1"
echo "   git push origin v0.1.0-rc.1"
echo ""
echo "3. Check GitHub Actions workflow for GPG signing steps"
echo ""
echo "4. Verify release artifacts have both signatures:"
echo "   - *.tar.gz.cosign.sig (Cosign/GitHub OIDC)"
echo "   - *.tar.gz.asc (GPG subkey signature)"
echo ""
