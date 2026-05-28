# Baremetal Redfish Fleet Example

This example maps the same three-node OpenShift control-plane intent from
`examples/libvirt-redfish-fleet` to explicit bare-metal inventory with Redfish
virtual media.

It is paired with `examples/libvirt-redfish-fleet` to show the provider swap
contract. `environment.yaml` and `container-cluster.yaml` should remain
byte-identical between the two examples when cluster intent is unchanged.

Provider-owned files in this variant:

| File | Why it differs |
| --- | --- |
| `hosts.yaml` | Declares provider, BMC, and service reachability for real hosts |
| `networks.yaml` | Uses the physical machine network and host NMState template |
| `provider.yaml` | Uses explicit bare-metal machines, MAC addresses, and Redfish BMC endpoints |
| `infra-component.yaml` | Publishes artifact routes reachable by physical BMCs |
| `cluster-infra.yaml` | Selects bare-metal machines and operator-owned external VIPs |
