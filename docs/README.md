# Node Doctor Documentation

Index of the docs in this repository. Pick the section for your role.

## Operators

Deploying, configuring, and running Node Doctor.

- [Quick Start](quick-start.md) - Install Node Doctor and import the dashboards.
- [Helm Chart](../helm/node-doctor/README.md) - Chart values and install options.
- [DaemonSet Deployment](../deployment/README.md) - Deploy with plain manifests.
- [Configuration Reference](configuration.md) - Every config option and its default.
- [Monitors](monitors.md) - The health check monitors and their settings.
- [DNS Monitor: Advanced Features](monitors/dns-advanced.md) - Extra DNS monitor options.
- [DNS Monitor: Troubleshooting](monitors/dns-troubleshooting.md) - Diagnose DNS monitor problems.
- [Remediation](remediation.md) - Auto-remediation and its safety controls.
- [Controller Deployment](controller-deployment.md) - Deploy the Node Doctor controller.
- [Troubleshooting](troubleshooting.md) - Common problems and how to debug them.
- [Grafana Dashboards](../dashboards/README.md) - Dashboard files and how to import them.
- [Example Configurations](../config/examples/README.md) - Sample config files.

## Developers

Building, testing, and releasing Node Doctor.

- [Architecture](architecture.md) - Components and how they fit together.
- [Developer Testing Guide](testing-guide.md) - Test patterns and how to run each suite.
- [Testing](testing.md) - Test organization and commands.
- [Release Process](release-process.md) - Versioning, tagging, and publishing a release.
- [Contributing](../CONTRIBUTING.md) - Workflow, coding standards, and PR process.
- [Security Policy](../SECURITY.md) - How to report a vulnerability.
- [E2E Tests](../test/e2e/README.md) - Run the end-to-end suite against a cluster.
- [Integration Tests](../test/integration/README.md) - Run the integration suite.
- [GitHub Actions Workflows](../.github/workflows/README.md) - What each CI workflow does.

Run `scripts/check-doc-links.sh` to check relative links in every markdown file.
