# SNO Libvirt Redfish Disconnected External Mirror Example

This is a complete seven-kind single-node OpenShift example for disconnected
install using an operator-owned external mirror registry.

Authored files:

```text
environment.yaml       fleet defaults, selected resources, and secret names
hosts.yaml             provider host reachability
provider.yaml          libvirt and Redfish emulator capabilities
networks.yaml          machine network and NMState template
cluster-infra.yaml     selected machine, endpoints, and managed infra components
container-cluster.yaml OpenShift install intent and node binding
```

Generated context state, installer files, runtime output, and external
`render --output-dir` exports are not edit points. Change desired state here,
then rerun `bootwright context init <name> -f examples/sno-libvirt-redfish-disconnected-external-mirror --yes`.
