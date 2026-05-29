# SNO Libvirt Redfish External DNS Example

This is a complete seven-kind single-node OpenShift example that uses a DNS
service outside cluster intent. `InfraComponent/external-dns` provides the
name-resolution service, `Environment.spec.infraComponents.nameResolution[]`
publishes it as `external-dns`, and `NetworkConfig.spec.dnsRefs[]` selects it
for installer host networking.

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
then rerun `bootwright context init <name> -f examples/sno-libvirt-redfish-external-dns --yes`.
