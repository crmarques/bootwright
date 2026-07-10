# OVN child-network add-on wiring (CUDN + NNCP localnet)

Wiring rules for the nested-cluster child network shipped as a manifestSet
add-on in `examples/baremetal-redfish-multidc-virtualized-odf-ceph`
(`dc1-child-cudn.yaml`/`dc2-child-cudn.yaml`, `dc1-child-nncp.yaml`/
`dc2-child-nncp.yaml`).

**No subnets, no ipam on the CUDN:** the ClusterUserDefinedNetwork
deliberately declares no `subnets` and no `ipam` — child node IPs are
static, assigned by bootwright through NMState/agent-config
(`infra/networkconfigs/dc1-child-net.yaml`). OVN carries L2 only; enabling
IPAM on the CUDN would override those static IPs.

**Bridge-mapping name must match:** the NNCP's `ovn.bridge-mappings`
localnet name MUST equal the CUDN
`spec.network.localnet.physicalNetworkName`.

**VLAN plumbing:** the child VLAN (151 dc1 / 152 dc2) is peeled off the
single metal uplink as a subinterface and bridged into OVS for the localnet.
A dedicated NIC or bond works too — adapt base-iface/VLAN id to the real DC
fabric.

**Namespace selection:** the target namespace must carry the label selected
by the CUDN `namespaceSelector`.

**What VMs consume:** the OVN-derived NetworkAttachmentDefinition (readiness
check: `resourceExists` `k8s.cni.cncf.io/v1`) is what the child VMs actually
attach to, via multus networkName `<namespace>/<name>` — a (C)UDN's derived
NAD shares the object's name.
