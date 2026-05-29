# Baremetal Redfish Postinstall Example

This example extends `examples/baremetal-redfish` with declarative post-install
extensions. Cluster provisioning remains in `ContainerCluster`, while
OpenShift Virtualization is declared as a `ClusterExtension`, grouped by a
`ClusterExtensionSet`, and attached to `demo-ocp` by a
`ClusterExtensionBinding`.

The extension phase runs after the cluster install is complete:

```text
bootwright apply cluster --yes
bootwright check extensions
bootwright apply extensions --dry-run
bootwright apply extensions --yes
```

`bootwright apply all --yes` includes the extension phase after cluster
installation. `bootwright apply cluster --yes` remains provisioning-only.
