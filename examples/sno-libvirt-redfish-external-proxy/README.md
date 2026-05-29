# SNO Libvirt Redfish External Proxy Example

This is a complete seven-kind single-node OpenShift example that uses an
operator-owned external proxy from
`Environment.spec.infraComponents.proxies[]`.

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
then rerun `bootwright context init <name> -f examples/sno-libvirt-redfish-external-proxy --yes`.
