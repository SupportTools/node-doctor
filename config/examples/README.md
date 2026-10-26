# Example configurations

Start from the file closest to your setup, then check it with:

```bash
node-doctor -validate-config -config my-config.yaml
```

`-validate-config` loads the file, adds the default monitors, runs every monitor's validator and exits. `go test ./config/examples/` also builds every monitor in these files.

The Helm chart renders its own config from chart values and does not use these files.

| File | Use it for | Monitor types |
|---|---|---|
| [minimal.yaml](minimal.yaml) | Learning the format. Remediation off. | `system-disk` |
| [development.yaml](development.yaml) | Local runs with debug logging, short intervals and dry-run remediation | `system-*`, `network-dns-check`, `network-gateway-check`, `network-connectivity-check`, `kubernetes-*` |
| [production.yaml](production.yaml) | A full config with remediation and all exporters. Review thresholds and webhook URLs first. | Same as development |
| [testing.yaml](testing.yaml) | Exercising every monitor on a test node. Remediation off. | Same as development, plus `network-cni-check` and `custom-logpattern` |
| [custom-plugins.yaml](custom-plugins.yaml) | Plugins, log patterns, dependencies and script remediation | `custom-plugin`, `custom-logpattern`, `system-disk`, `network-connectivity-check`, `kubernetes-kubelet-check`, `kubernetes-apiserver-check` |
| [../node-doctor.yaml](../node-doctor.yaml) | The reference config, with the IPv6 monitors | `system-*`, `network-dns-check`, `network-gateway-check`, `network-ipv6-*`, `kubernetes-kubelet-check`, `kubernetes-runtime-check`, `kubernetes-capacity-check` |
| [../../examples/dns-monitoring/](../../examples/dns-monitoring/) | DNS monitor blocks, basic and with every advanced layer. Copy the monitor entry into a full config. | `network-dns-check` |

## Monitor types

All registered types. [docs/monitors.md](../../docs/monitors.md) has the keys, defaults and conditions for each one.

| Group | Types |
|---|---|
| System | `system-cpu`, `system-memory`, `system-disk` |
| Network | `network-dns-check`, `network-cluster-dns-pod`, `network-gateway-check`, `network-connectivity-check`, `network-cni-check`, `network-ip-forwarding` |
| IPv6 | `network-ipv6-sysctl`, `network-ipv6-route`, `network-ipv6-neighbor`, `network-ipv6-firewall` |
| Kubernetes | `kubernetes-kubelet-check`, `kubernetes-apiserver-check`, `kubernetes-runtime-check`, `kubernetes-capacity-check` |
| Custom | `custom-plugin`, `custom-logpattern` |
| Example | `noop` |

Top-level fields (`settings`, `exporters`, `remediation`, environment variable substitution) are in [docs/configuration.md](../../docs/configuration.md).
