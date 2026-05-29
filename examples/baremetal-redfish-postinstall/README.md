# Baremetal Redfish Postinstall Example

This example extends `examples/baremetal-redfish` with declarative post-install
extensions. Cluster provisioning remains in `ContainerCluster`, while
OpenShift Virtualization is declared as a `ClusterExtension`, grouped by a
`ClusterExtensionSet`, and attached to `demo-ocp` by a
`ClusterExtensionBinding`.

The extension phase runs after the cluster install is complete:

```text
bootwright apply cluster --yes
```

To converge extensions again after the cluster is already installed:

```text
bootwright check extensions
bootwright apply extensions --dry-run
bootwright apply extensions --yes
```

`bootwright apply cluster --yes` and `bootwright apply all --yes` include the
extension phase after cluster installation.
