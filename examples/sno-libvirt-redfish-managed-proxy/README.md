# SNO Libvirt Redfish Managed Proxy Example

This is a complete seven-kind single-node OpenShift example that uses a
Bootwright-managed Squid proxy selected by `Environment.spec.infraComponents.proxies[]`.
The managed proxy is created during infrastructure convergence, so it is not
available to bootstrap `apply bastion`; use an external proxy instead when the
bastion phase itself needs proxy access.

Authored files:

```text
environment.yaml       fleet defaults, selected resources, and secret names
shared/hosts.yaml             provider host reachability
shared/provider.yaml          libvirt and Redfish emulator capabilities
shared/infra-component.yaml   shared infra services
shared/networks.yaml          machine network and NMState template
clusters/<cluster>/cluster-infra.yaml     selected machine and endpoints
clusters/<cluster>/container-cluster.yaml OpenShift install intent and node binding
```

Generated context state, installer files, runtime output, and external
`render --output-dir` exports are not edit points. Change desired state here,
then rerun `bootwright context init <name> -f examples/sno-libvirt-redfish-managed-proxy --yes`.
