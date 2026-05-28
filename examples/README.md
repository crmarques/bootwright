# Examples

Canonical examples are safe-to-commit desired-state input sets. They are meant
for reading, copying, and syntax validation; E2E cases under `test/e2e/` remain
the runnable host-specific test assets.

Current examples:

- `sno-libvirt-redfish`: minimal single-node OpenShift cluster on a libvirt
  provider with Redfish BMC emulation. Start here for the smallest safe
  input shape.
- `sno-libvirt-redfish-external-dns`: the minimal SNO shape with DNS provided
  by a name-resolution `InfraComponent` and selected from `NetworkConfig`.
- `sno-libvirt-redfish-external-proxy`: the minimal SNO shape with an
  operator-owned external proxy.
- `sno-libvirt-redfish-managed-proxy`: the minimal SNO shape with a
  Bootwright-managed Squid proxy.
- `sno-libvirt-redfish-disconnected-external-mirror`: disconnected install
  using an operator-owned external mirror registry.
- `sno-libvirt-redfish-managed-registry`: disconnected install using a
  Bootwright-managed mirror registry component.
- `libvirt-redfish-fleet`: compact three-node OpenShift cluster on a libvirt
  provider with Redfish BMC emulation and Bootwright-provisioned HAProxy VIPs.
- `baremetal-redfish`: the same `Environment` and `ContainerCluster`
  intent mapped to explicit bare-metal inventory with operator-owned external
  VIPs.
- `baremetal-redfish-fleet`: two bare-metal Redfish clusters sharing common
  services and provider inventory through a subdirectory layout.

The two examples intentionally keep `environment.yaml` and
`container-cluster.yaml` byte-identical. Provider swaps should normally change
substrate-owned files only: `hosts.yaml`, `networks.yaml`, `provider.yaml`, and
`cluster-infra.yaml`, plus `infra-component.yaml` when service placement or
routable service endpoints change.

## Variant Deltas

Provider swap invariant:

| Variant | Files that change | Files that should not change |
| --- | --- | --- |
| `libvirt-redfish-fleet` to `baremetal-redfish` | `hosts.yaml`, `networks.yaml`, `provider.yaml`, `cluster-infra.yaml`, `infra-component.yaml` | `environment.yaml`, `container-cluster.yaml` |

Single-node mode variants:

| Variant | Changed files and owning fields |
| --- | --- |
| `sno-libvirt-redfish-external-dns` | `environment.yaml` selects name resolution; `networks.yaml` uses `dnsRefs`; `infra-component.yaml` defines the name-resolution service |
| `sno-libvirt-redfish-external-proxy` | `environment.yaml` adds an external proxy connection and `proxyFor` audiences |
| `sno-libvirt-redfish-managed-proxy` | `environment.yaml` selects a managed proxy; `infra-component.yaml` places the Squid service |
| `sno-libvirt-redfish-disconnected-external-mirror` | `container-cluster.yaml` sets `install.mode: disconnected`; `environment.yaml` declares mirror registry URL, trust, and artifact cluster-install route |
| `sno-libvirt-redfish-managed-registry` | `container-cluster.yaml` sets `install.mode: disconnected`; `environment.yaml` selects managed registry and trust; `infra-component.yaml` places the registry |

Copy an example to a working directory before editing it for a real
environment, then import that copy:

```text
cp -a examples/sno-libvirt-redfish ./my-sno-lab
bootwright context init lab -f <working-copy>
bootwright secret list
bootwright render installer --scope <cluster-name>
bootwright render --output-dir ./rendered --scope <cluster-name> --sensitive
```

The external CLI export writes OpenShift installer files under
`./rendered/openshift-install/<cluster-name>/`.
