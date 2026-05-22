# Examples

Canonical examples are safe-to-commit desired-state input sets. They are meant
for reading, copying, and syntax validation; E2E cases under `test/e2e/` remain
the runnable host-specific test assets.

Current examples:

- `libvirt-redfish-fleet`: compact three-node OpenShift cluster on a libvirt
  provider with Redfish BMC emulation and Bootwright-provisioned HAProxy VIPs.
- `baremetal-redfish-fleet`: the same `Environment` and `ContainerCluster`
  intent mapped to explicit bare-metal inventory with operator-owned external
  VIPs.

The two examples intentionally keep `environment.yaml` and
`container-cluster.yaml` byte-identical. Provider swaps should normally change
substrate-owned files only: `hosts.yaml`, `networks.yaml`, `provider.yaml`, and
`cluster-infra.yaml`.

Copy an example to a working directory before editing it for a real
environment, then import that copy:

```text
bootwright context init ocp-nprd-01 -f <working-copy>
bootwright render installer --scope demo-ocp
bootwright render --output-dir ./rendered --scope demo-ocp --sensitive
```

The external CLI export writes OpenShift installer files under
`./rendered/openshift-install/demo-ocp/`.
