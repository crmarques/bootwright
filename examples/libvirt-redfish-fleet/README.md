# Libvirt Redfish Fleet Example

This example maps one three-node OpenShift control plane to libvirt virtual
machines with emulated Redfish BMC access.

It is paired with `examples/baremetal-redfish` to show the provider swap
contract. `environment.yaml` and `container-cluster.yaml` should remain
byte-identical between the two examples when cluster intent is unchanged.

Provider-owned files in this variant:

| File | Why it differs |
| --- | --- |
| `hosts.yaml` | Declares the libvirt provider/service host addresses |
| `networks.yaml` | Uses the libvirt bridge and lab machine network |
| `provider.yaml` | Uses libvirt machine profiles and emulated BMC metadata |
| `infra-component.yaml` | Places managed HAProxy and artifact services for the libvirt lab |
| `cluster-infra.yaml` | Selects libvirt profiles, generated machines, and managed VIP endpoints |
