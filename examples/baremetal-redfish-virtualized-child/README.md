# Baremetal Redfish With Virtualized Child Example

This example provisions one bare-metal single-node OpenShift cluster and then
uses that cluster as the KubeVirt substrate for a second single-node OpenShift
cluster.

The parent cluster is `metal-ocp`. It uses a real bare-metal server with
Redfish virtual media and receives OpenShift Virtualization through a
`ClusterExtension` that advertises `provides: kubevirt`.

The child cluster is `child-ocp`. It is a separate `ContainerCluster`; its
`InfraProvider` profile creates KubeVirt VMs in namespace
`bootwright-child-ocp` on the parent cluster.

Main path:

```text
bootwright check syntax -f examples/baremetal-redfish-virtualized-child
bootwright context init nested -f examples/baremetal-redfish-virtualized-child
bootwright secret set openshift-pull-secret --pull-secret ~/openshift-pull-secret.json
bootwright secret generate
bootwright apply all --yes
```

`apply all` installs `metal-ocp`, waits for the virtualization extension,
creates the child VM infrastructure, boots the child agent ISO with `virtctl`,
and waits for `child-ocp` installation.

Scoped child operations do not install the parent implicitly:

```text
bootwright apply infra --scope child-ocp --yes
bootwright apply cluster --scope child-ocp --yes
```

Use the scoped infra command only after `metal-ocp` is installed and its
OpenShift Virtualization extension is ready. The scoped cluster command keeps
install-only semantics and assumes child VM infrastructure already exists.
