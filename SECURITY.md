# Security Policy

Node Doctor is a Kubernetes node health monitoring and auto-remediation tool that requires privileged access to function. This document describes our security model, vulnerability reporting process, and security best practices.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

### Preferred Method

Use [GitHub Security Advisories](https://github.com/supporttools/node-doctor/security/advisories/new) to report vulnerabilities privately. This allows us to assess and address the issue before public disclosure.

### Alternative Method

Email security concerns to: **security@supporttools.com**

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact assessment
- Any suggested fixes (optional)

### Response Timeline

| Stage | Timeline |
|-------|----------|
| Initial acknowledgment | 48 hours |
| Severity assessment | 5 business days |
| Fix for critical issues | 30 days |
| Coordinated disclosure | 90 days |

We follow responsible disclosure practices and will credit researchers (if desired) when vulnerabilities are fixed and disclosed.

## Supported Versions

| Version | Security Support |
|---------|------------------|
| Latest release | Full support |
| Previous minor release | Security fixes only |
| Older versions | No support |

We recommend always running the latest release to receive all security updates.

## Security Model

Node Doctor requires elevated privileges by design to monitor and remediate node-level issues. This section documents the required access and why it's necessary.

### Kubernetes RBAC Requirements

| Resource | Verbs | Purpose |
|----------|-------|---------|
| nodes | get, list, watch | Monitor node status |
| nodes/status | get, patch, update | Update node conditions |
| pods | get, list, watch | Monitor pod health |
| pods/status | get | Check pod status |
| events | create, patch, update | Record remediation actions |
| leases | get, list, watch, create, update, patch, delete | Leader election for HA |
| configmaps | get, list, watch | Configuration loading |
| services | get, list, watch | Service health checks |
| endpoints | get, list, watch | Endpoint monitoring |
| namespaces | get, list, watch | Namespace discovery |
| componentstatuses | get, list | Cluster component health |
| nonResourceURLs (/healthz, /livez, /readyz, /version) | get | API server health |

### Container Privileges

Node Doctor runs as a **privileged container** with the following security context. This level of access is required for comprehensive node monitoring and remediation.

> **Important**: Running as privileged means the container has full access to the host system. The read-only mount flags provide defense-in-depth but can be bypassed by a privileged process.

| Privilege | Purpose |
|-----------|---------|
| `privileged: true` | Full host access for monitoring and remediation |
| `hostPID: true` | Process monitoring and inspection |
| `hostNetwork: true` | Network health monitoring |
| `allowPrivilegeEscalation: true` | Required for remediation actions |
| `runAsUser: 0` | Root access for system operations |

**Linux Capabilities Added:**

| Capability | Purpose |
|------------|---------|
| `SYS_ADMIN` | Filesystem and mount operations |
| `SYS_PTRACE` | Process inspection |
| `NET_ADMIN` | Network diagnostics |
| `SYS_TIME` | Time-related operations |
| `SETUID` | Process permissions management |
| `SETGID` | Group permissions management |

**Additional Security Settings:**
- `readOnlyRootFilesystem: false` - Container filesystem is writable
- `apparmor.security.beta.kubernetes.io: unconfined` - AppArmor is disabled

### Host Volume Access

The following host paths are mounted into the container:

| Mount | Access | Purpose |
|-------|--------|---------|
| `/` (host root) | Read-only | Host filesystem access via `/host` |
| `/proc` | Read-only | Process and system metrics |
| `/sys` | Read-only | Kernel parameters and hardware info |
| `/dev/kmsg` | Read-only | Kernel message monitoring (OOM detection) |
| `/var/log` | Read-only | System log analysis |
| `/var/log/journal` | Read-only | Systemd journal access |
| `/etc/kubernetes` | Read-only | Kubernetes configuration checks |
| `/etc/machine-id` | Read-only | Machine identification |
| `/etc/os-release` | Read-only | OS information |
| Container runtime sockets | Read-only | Container health monitoring |

> **Note**: While mounts are configured as read-only, the privileged container mode means these restrictions can be bypassed. The read-only flags provide defense-in-depth, not absolute protection.

## Remediation Safety Controls

When remediation is enabled, multiple safety mechanisms prevent runaway operations:

### Rate Limiting
- `maxRemediationsPerHour`: Limits total remediations per hour
- `maxRemediationsPerMinute`: Limits burst remediation rate

### Cooldown Periods
- Minimum time between remediation attempts for the same issue
- Prevents rapid repeated remediation cycles

### Circuit Breaker
- Automatically disables remediation after repeated failures
- Auto-resets after configurable timeout (manual reset also available)
- Configurable failure thresholds

### Script Validation
For custom remediation scripts:
- **Path normalization** - relative paths are converted to absolute paths
- **Path traversal blocked** - `..` sequences rejected during config validation
- **Executable check** - verifies script is executable
- **Timeout protection** - default 5 minutes, configurable

## Security Best Practices

### For Operators

1. **Start with dry-run mode**: Enable `dryRunMode: true` before production deployment to validate behavior without taking action.

2. **Use conservative rate limits**: Start with low values and increase based on observed behavior.

3. **Review custom scripts carefully**: Any custom remediation scripts run with root privileges on the node.

4. **Monitor the audit trail**: All remediation actions create Kubernetes events for auditing.

5. **Restrict metrics access**: Use NetworkPolicies to limit access to the metrics endpoint (port 9101) and health endpoint (port 8080).

6. **Consider TLS for HTTP exporter**: Enable TLS if exposing webhooks externally.

7. **Keep node-doctor updated**: Security fixes are only backported to the previous minor release.

### For Contributors

See [CONTRIBUTING.md](CONTRIBUTING.md) for security guidelines when contributing code, including:
- Input validation requirements
- Safety-first principles for remediators
- Security review process for changes

## Artifact Verification

Releases are signed with Cosign using GitHub OIDC (keyless signing).

### Verify with Cosign

```bash
cosign verify docker.io/supporttools/node-doctor:<version> \
  --certificate-identity-regexp="https://github.com/supporttools/node-doctor" \
  --certificate-oidc-issuer="https://token.actions.githubusercontent.com"
```

Replace `<version>` with the specific release tag (e.g., `v1.0.0`).

## Scope

This security policy covers:
- The Node Doctor application code
- Official container images (`docker.io/supporttools/node-doctor`)
- Helm charts in this repository
- Documentation in this repository

### Out of Scope

- Third-party dependencies (report to upstream maintainers)
- User-provided configuration errors
- Custom remediation scripts written by users
- Infrastructure where Node Doctor is deployed
- Vulnerabilities in container runtimes (Docker, containerd, CRI-O)

## Contact

- **Security issues**: security@supporttools.com or [GitHub Security Advisory](https://github.com/supporttools/node-doctor/security/advisories/new)
- **General questions**: [GitHub Discussions](https://github.com/supporttools/node-doctor/discussions)
- **Bug reports**: [GitHub Issues](https://github.com/supporttools/node-doctor/issues)
