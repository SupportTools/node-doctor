# GPG Signing Setup for Node Doctor Releases

This document provides quick reference for setting up GPG signing for Node Doctor releases.

## Overview

Node Doctor releases use **dual-layer signing** for defense-in-depth security:
1. **Cosign** (GitHub OIDC) - Automatic, proves CI/CD built it
2. **GPG** (Maintainer signature) - Manual setup required, proves maintainer approved it

## Security Model

### ⚠️ IMPORTANT: Use a Dedicated Signing Subkey

**NEVER upload your main GPP private key to GitHub Secrets!**

Instead, create a dedicated signing subkey:
- ✅ Main key stays on your local machine (safe)
- ✅ Only signing subkey goes to GitHub
- ✅ Subkey can be revoked independently if GitHub is compromised
- ✅ Follows GPG best practices for automated systems

### How Subkey Signing Works

```
┌─────────────────────────────────────────────────────────┐
│  Your Machine                                           │
│                                                          │
│  ┌──────────────────┐                                   │
│  │   Main GPG Key   │ (stays here, never leaves!)      │
│  │  1AF32A8B..F3    │                                   │
│  └────────┬─────────┘                                   │
│           │                                              │
│           │ creates                                      │
│           ▼                                              │
│  ┌──────────────────┐                                   │
│  │  Signing Subkey  │ (exported to GitHub)             │
│  │  ABC123...XYZ    │                                   │
│  └────────┬─────────┘                                   │
│           │                                              │
└───────────┼──────────────────────────────────────────────┘
            │
            │ export (without main key)
            ▼
┌─────────────────────────────────────────────────────────┐
│  GitHub Secrets                                         │
│                                                          │
│  GPG_PRIVATE_KEY = <signing subkey only>                │
│  GPG_PASSPHRASE  = <optional, if not removed>           │
└─────────────────────────────────────────────────────────┘
```

When verifying signatures:
- Users import your **main public key** (from keyserver or release notes)
- The subkey signature validates against the main public key
- Trust chain: Subkey → Main Key → User's trust decision

## Quick Setup (Automated)

```bash
# Run the setup script
./scripts/setup-gpg-signing-subkey.sh

# Follow the prompts to:
# 1. Create signing subkey
# 2. Export it (without main key)
# 3. Optionally remove passphrase
# 4. Get instructions for adding to GitHub Secrets
```

## Manual Setup

### Step 1: Create Signing Subkey

```bash
# Start key editing
gpg --expert --edit-key 1AF32A8B0481A7F3

# In GPG prompt:
gpg> addkey

# Choose:
# - Type: (4) RSA (sign only)
# - Size: 4096
# - Expiration: 2y (2 years recommended)
# - Confirm: y

gpg> save
```

### Step 2: Identify Your Subkey

```bash
# List keys with subkeys
gpg --list-secret-keys --with-subkey-fingerprints 1AF32A8B0481A7F3

# Look for the line with [S] (signing capability):
# ssb   rsa4096/ABC123XYZ 2025-01-17 [S] [expires: 2027-01-17]
#                           ↑
#                  This is your subkey ID
```

### Step 3: Export ONLY the Subkey

**CRITICAL**: The `!` suffix ensures only the subkey is exported, NOT the main key.

```bash
# Replace ABC123XYZ with your actual subkey ID
gpg --armor --export-secret-subkeys ABC123XYZ! > /tmp/gpg-signing-subkey.asc

# Verify it's only the subkey (not main key)
# Create temporary keyring to test
TEMP_KEYRING=$(mktemp -d)
gpg --homedir "$TEMP_KEYRING" --import /tmp/gpg-signing-subkey.asc

# Check output - should show "sec#" (# means main key is NOT included)
gpg --homedir "$TEMP_KEYRING" --list-secret-keys

# Should see:
# sec#  rsa4096/1AF32A8B0481A7F3 ...  (# = stub, main key NOT present) ✅
# ssb   rsa4096/ABC123XYZ ...        (subkey IS present) ✅

# Clean up test
rm -rf "$TEMP_KEYRING"
```

### Step 4: (Optional) Remove Passphrase from Subkey

For easier automation, you can remove the passphrase from the subkey while keeping your main key protected.

```bash
gpg --edit-key 1AF32A8B0481A7F3

# Select the subkey
gpg> key ABC123XYZ

# Change passphrase
gpg> passwd
# Enter current passphrase: <your main key passphrase>
# Enter new passphrase: <press Enter for blank>
# Repeat new passphrase: <press Enter for blank>

gpg> save

# Re-export the subkey (now without passphrase)
gpg --armor --export-secret-subkeys ABC123XYZ! > /tmp/gpg-signing-subkey.asc
```

### Step 5: Add to GitHub Secrets

#### Via GitHub CLI (Recommended)

```bash
# Add the subkey
gh secret set GPG_PRIVATE_KEY < /tmp/gpg-signing-subkey.asc

# If you kept the passphrase:
gh secret set GPG_PASSPHRASE
# Enter your passphrase when prompted

# Verify secrets are configured
gh secret list | grep GPG
```

#### Via GitHub UI

1. Go to: https://github.com/supporttools/node-doctor/settings/secrets/actions
2. Click "New repository secret"
3. **Secret 1:**
   - Name: `GPP_PRIVATE_KEY`
   - Value: Paste contents of `/tmp/gpg-signing-subkey.asc`
4. **Secret 2** (only if you kept passphrase):
   - Name: `GPG_PASSPHRASE`
   - Value: Your subkey passphrase

### Step 6: Secure Cleanup

```bash
# Securely delete the exported file
shred -u /tmp/gpg-signing-subkey.asc

# Or on macOS:
rm -P /tmp/gpg-signing-subkey.asc
```

## Verification

### Verify Setup

```bash
# Run verification script
./scripts/verify-gpg-setup.sh

# Expected output:
# ✅ GPG key found locally
# ✅ GPG_PRIVATE_KEY secret is configured
# ✅ GPG_PASSPHRASE secret is configured (or N/A if removed)
# ✅ Release workflow references GPG secrets
# ✅ GPG signing configured
```

### Test with Pre-Release

```bash
# Create a test pre-release
git tag v0.1.0-rc.test
git push origin v0.1.0-rc.test

# Monitor GitHub Actions
gh run watch

# Check release artifacts
gh release view v0.1.0-rc.test --json assets -q '.assets[].name'

# Should see both signature types:
# - node-doctor_linux_amd64.tar.gz.cosign.sig  (Cosign signature)
# - node-doctor_linux_amd64.tar.gz.asc         (GPG signature) ✅

# Clean up test release
gh release delete v0.1.0-rc.test --yes
git push origin --delete v0.1.0-rc.test
```

## Publishing Your Public Key

Users need your public key to verify signatures. Publish it in multiple ways:

### Method 1: Keyserver (Recommended)

```bash
# Send to Ubuntu keyserver
gpg --keyserver keyserver.ubuntu.com --send-keys 1AF32A8B0481A7F3

# Send to OpenPGP keyserver
gpg --keyserver keys.openpgp.org --send-keys 1AF32A8B0481A7F3
```

### Method 2: In Repository

```bash
# Export public key
gpg --armor --export 1AF32A8B0481A7F3 > docs/signing-keys/node-doctor-signing-key.asc

# Commit it
git add docs/signing-keys/node-doctor-signing-key.asc
git commit -m "docs: add GPG signing public key"
git push
```

### Method 3: In Release Notes

Add to each release notes:

```markdown
## Signature Verification

All artifacts are signed with:
- **Cosign** (GitHub OIDC): Automatic CI/CD signature
- **GPG Key**: 1AF32A8B0481A7F3

Import GPG key:
```bash
gpg --keyserver keyserver.ubuntu.com --recv-keys 1AF32A8B0481A7F3
```

Verify signatures:
```bash
cosign verify-blob --signature <file>.tar.gz.cosign.sig ...
gpg --verify <file>.tar.gz.asc <file>.tar.gz
```
```

## Revoking the Subkey

If GitHub Secrets are compromised or you suspect the subkey is exposed:

```bash
# Edit main key
gpg --edit-key 1AF32A8B0481A7F3

# Select the subkey
gpg> key ABC123XYZ

# Revoke it
gpg> revkey
# Reason: 1 = Key has been compromised
# Description: "GitHub Actions subkey - suspected compromise"

gpg> save

# Publish revocation to keyservers
gpg --keyserver keyserver.ubuntu.com --send-keys 1AF32A8B0481A7F3

# Create new subkey
gpg> addkey
# ... follow subkey creation steps again ...
```

### What Happens After Revocation?

- Old signatures remain valid (they were signed before revocation)
- New signatures will use the new subkey
- Users updating your public key will see the revocation
- Automated systems should reject new signatures from revoked subkey

## Troubleshooting

### "Permission denied" when adding secrets

```bash
# Check your GitHub CLI authentication
gh auth status

# Re-authenticate if needed
gh auth login
```

### "No secret key" when signing locally

Your main key is locked or not available:

```bash
# Unlock your GPG agent
echo "test" | gpg --clearsign > /dev/null

# Or restart GPG agent
gpgconf --kill gpg-agent
```

### GitHub Actions: "gpg: signing failed: Inappropriate ioctl for device"

Missing TTY for passphrase. Solution:
1. Remove passphrase from subkey (recommended for automation)
2. Or ensure GPG_PASSPHRASE secret is configured

### Signature verification fails

```bash
# Make sure you're verifying with the main public key
gpg --list-keys 1AF32A8B0481A7F3

# If not present, import it
gpg --keyserver keyserver.ubuntu.com --recv-keys 1AF32A8B0481A7F3

# Verify again
gpg --verify file.tar.gz.asc file.tar.gz
```

## FAQ

**Q: Why not just use Cosign alone?**
A: Defense in depth. Cosign proves GitHub Actions built it. GPG proves a maintainer approved it. If GitHub's OIDC is compromised, you still have GPG. If your GPG subkey is compromised, you still have Cosign.

**Q: Can I use my existing signing subkey?**
A: Yes! If you already have a signing subkey, use it. The setup script will detect it.

**Q: What if my subkey expires?**
A: Extend it before expiration or create a new one. Update GitHub Secrets with the new subkey.

**Q: Do users need the subkey to verify?**
A: No. Users import your main public key. The subkey signature validates against the main public key automatically.

**Q: What if I lose my main key?**
A: The subkey won't work without the main key for creation/revocation. Keep backups of your main key in a secure location (encrypted USB drive, password manager, etc.).

## Security Best Practices

1. ✅ **Use a subkey** - Never upload main key
2. ✅ **Set expiration** - 1-2 years recommended
3. ✅ **Keep main key offline** - On encrypted storage
4. ✅ **Regular renewal** - Extend or create new subkey before expiration
5. ✅ **Monitor usage** - Review GitHub Actions logs for signing activity
6. ✅ **Revoke if compromised** - Act immediately if you suspect exposure
7. ✅ **Backup main key** - Encrypted backups in multiple secure locations

## References

- [GPG Subkey Best Practices](https://wiki.debian.org/Subkeys)
- [GitHub Actions Secrets](https://docs.github.com/en/actions/security-guides/encrypted-secrets)
- [Cosign Documentation](https://docs.sigstore.dev/cosign/overview/)
- [Node Doctor Release Process](release-process.md)
