# Examples

Canonical examples are safe-to-commit desired-state input sets. They are meant
for reading, copying, and syntax validation; E2E cases under `test/e2e/` remain
the runnable host-specific test assets.

Current examples:

- `sno-libvirt-redfish`: minimal single-node OpenShift cluster on a libvirt
  provider with Redfish BMC emulation. Start here for the smallest safe
  input shape.
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
- `baremetal-redfish-fleet`: the same `Environment` and `ContainerCluster`
  intent mapped to explicit bare-metal inventory with operator-owned external
  VIPs.

The two examples intentionally keep `environment.yaml` and
`container-cluster.yaml` byte-identical. Provider swaps should normally change
substrate-owned files only: `hosts.yaml`, `networks.yaml`, `provider.yaml`, and
`cluster-infra.yaml`, plus `infra-component.yaml` when service placement or
routable service endpoints change.

Copy an example to a working directory before editing it for a real
environment, then import that copy:

```text
bootwright context init lab -f <working-copy>
bootwright secret list
bootwright render installer --scope <cluster-name>
bootwright render --output-dir ./rendered --scope <cluster-name> --sensitive
```

The external CLI export writes OpenShift installer files under
`./rendered/openshift-install/<cluster-name>/`.
