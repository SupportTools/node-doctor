# Command Argument Validation

## Overview

This document describes the command argument validation security controls implemented in the Node Doctor remediators to prevent command injection, path traversal, and other security vulnerabilities.

## Threat Model

### Identified Threats

1. **Command Injection**: Malicious input in configuration that could execute arbitrary commands
2. **Path Traversal**: Attempts to access files outside allowed directories
3. **Resource Exhaustion**: Malformed parameters causing unintended system behavior

### Attack Vectors

- Configuration files provided by users
- Environment variables passed to the application
- Custom remediation scripts

## Security Controls

### 1. Path Whitelisting (Disk Remediator)

**Locations**: `pkg/remediators/validation.go:11-14`, `disk.go:185-187`

The disk remediator implements a strict whitelist approach for cleanup operations:

```go
var allowedCleanupPaths = []string{
    "/tmp",
    "/var/tmp",
}
```

**Validation**:
- Converts all paths to absolute paths using `filepath.Abs()`
- Cleans paths with `filepath.Clean()` to remove `..`, `.`, and redundant separators
- Checks against whitelist before allowing cleanup
- **Blocks**: `../etc/passwd`, `/home`, `/var/log`, any path containing `..`

### 2. Interface Name Validation (Network Remediator)

**Locations**: `pkg/remediators/validation.go:16-20`, `network.go:170-172`

Network interface names must match strict format requirements:

```go
// Pattern: ^[a-zA-Z0-9_-]{1,15}$
// Examples: eth0, ens3, wlan0, docker0, br-abc123, veth1234
```

**Validation**:
- Maximum length: 15 characters (Linux IFNAMSIZ - 1)
- Allowed characters: alphanumeric, underscore, hyphen
- **Blocks**: Shell metacharacters (`;`, `|`, `&`, `$`, `` ` ``, `"`, `'`, `<`, `>`, etc.)
- **Blocks**: Path separators (`/`, `\`)
- **Blocks**: Spaces and control characters

### 3. Vacuum Size Format Validation

**Locations**: `pkg/remediators/validation.go:23-26`

Journal vacuum sizes must match a specific format:

```go
// Pattern: ^[1-9][0-9]*[KMG]$
// Examples: 500M, 1G, 100K
```

**Validation**:
- Must start with non-zero digit
- Must end with K, M, or G (uppercase)
- No decimals, no lowercase, no spaces
- **Blocks**: `0M`, `1.5G`, `500m`, `500 M`, `500M; rm -rf /`

### 4. File Age Validation

**Locations**: `pkg/remediators/validation.go:99-107`

Temporary file age parameters are validated for sanity:

```go
func validateTmpFileAge(age int) error {
    if age < 0 {
        return fmt.Errorf("tmp file age must be non-negative, got: %d", age)
    }
    return nil
}
```

**Validation**:
- Must be non-negative integer
- **Blocks**: Negative values that could cause unexpected `find` command behavior

## Defense-in-Depth

### Layer 1: Configuration-Time Validation

All parameters are validated when the remediator is created:

- `disk.go:185-187`: Calls `validateDiskOperation()` during config validation
- `network.go:170-172`: Calls `validateNetworkOperation()` during config validation

This **fail-fast** approach prevents invalid configurations from ever being used.

### Layer 2: Command Execution Safety

Even with validation, commands are executed safely:

1. **Use of exec.CommandContext**: Prevents shell expansion
   ```go
   cmd := exec.CommandContext(ctx, "ip", "link", "set", interfaceName, "down")
   ```
   - Arguments passed as separate parameters, not shell-interpolated strings
   - No shell metacharacters are interpreted

2. **Context-based Timeouts**: All commands have timeout protection
   - Disk operations: default timeout based on operation type
   - Network operations: configurable `VerifyTimeout`
   - Custom scripts: configurable timeout (max 30 minutes)

3. **No Dynamic Command Construction**: Commands are not built from strings
   - Safe: `exec.CommandContext(ctx, "ip", "link", "set", name, "down")`
   - Unsafe (not used): `exec.Command("sh", "-c", fmt.Sprintf("ip link set %s down", name))`

## Testing

### Security Test Coverage

The validation logic includes comprehensive test cases for attack scenarios:

**Path Traversal Tests** (`validation_test.go:TestValidateDiskCleanupPath`):
- `../../etc/passwd` → Rejected
- `/tmp/../etc` → Rejected
- Relative paths → Rejected

**Command Injection Tests** (`validation_test.go:TestValidateInterfaceName`):
- `eth0; rm -rf /` → Rejected
- `eth0 | cat /etc/passwd` → Rejected
- `eth0 && echo hacked` → Rejected
- ``eth0`whoami``` → Rejected
- `eth0$(whoami)` → Rejected

**Format Validation Tests** (`validation_test.go:TestValidateVacuumSize`):
- `500M; rm -rf /` → Rejected
- `1.5G` → Rejected (prevents parsing issues)
- `0M` → Rejected (invalid vacuum size)

## Operational Guidance

### Recommended Configuration

1. **Use Default Values**: Defaults are security-reviewed
   ```yaml
   disk:
     operation: clean-tmp
     # Uses default path /tmp (whitelisted)
     # Uses default age 7 days
   ```

2. **Avoid Custom Paths**: Stick to whitelisted paths only
   ```yaml
   disk:
     operation: clean-tmp
     # ✓ Safe: /tmp (whitelisted)
     # ✗ Unsafe: /custom/path (not whitelisted)
   ```

3. **Validate Interface Names**: Use standard naming conventions
   ```yaml
   network:
     operation: restart-interface
     interface: eth0  # ✓ Safe
     # interface: "eth0; whoami"  # ✗ Blocked by validation
   ```

### Monitoring

Watch for validation failures in logs:

```
level=error msg="operation validation failed: path not allowed for cleanup: /etc"
level=error msg="operation validation failed: invalid interface name format: eth0; rm -rf /"
```

These indicate potential attack attempts or misconfigurations.

## Future Improvements

Potential enhancements for future releases:

1. **Extended Path Whitelist**: Support for configurable additional safe paths
2. **SELinux/AppArmor Integration**: Additional mandatory access control
3. **Audit Logging**: Track all validation failures for security monitoring
4. **Rate Limiting**: Prevent brute-force validation bypass attempts

## References

- [CWE-78: OS Command Injection](https://cwe.mitre.org/data/definitions/78.html)
- [CWE-22: Path Traversal](https://cwe.mitre.org/data/definitions/22.html)
- [OWASP Command Injection](https://owasp.org/www-community/attacks/Command_Injection)

## Security Contact

For security vulnerabilities, please report to the project maintainers through GitHub Security Advisories.
