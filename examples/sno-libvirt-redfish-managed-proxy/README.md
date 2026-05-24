# SNO Libvirt Redfish Managed Proxy Example

This is a complete six-kind single-node OpenShift example that uses a
Bootwright-managed Squid proxy selected by `ClusterInfra.spec.components.proxy`.

Authored files:

```text
environment.yaml       fleet defaults, selected resources, and secret names
hosts.yaml             provider host reachability
provider.yaml          libvirt, Redfish emulator, and managed-service capabilities
networks.yaml          machine network and NMState template
cluster-infra.yaml     selected machine, endpoints, and managed infra components
container-cluster.yaml OpenShift install intent and node binding
```

Generated context state, installer files, runtime output, and external
`render --output-dir` exports are not edit points. Change desired state here,
then rerun `bootwright context init <name> -f examples/sno-libvirt-redfish-managed-proxy --yes`.
