# OVN child-network add-on wiring (CUDN + NNCP localnet)

Wiring rules for the nested-cluster child networks shipped as a manifestSet
add-on in `examples/baremetal-redfish-multidc-virtualized-odf-ceph`
(`dc1-child-cudn.yaml`/`dc2-child-cudn.yaml`, the corresponding
`*-ceph-public-cudn.yaml` files, and `dc1-child-nncp.yaml`/
`dc2-child-nncp.yaml`).

**No subnets, explicitly disabled IPAM on the CUDN:** child node IPs are
static, assigned by bootwright through NMState/agent-config
(`infra/networkconfigs/dc1-child-net.yaml`). The ClusterUserDefinedNetwork
therefore omits `subnets` and sets `spec.network.localnet.ipam.mode: Disabled`.
Omitting `ipam` defaults its mode to `Enabled` and makes `subnets` required;
enabling IPAM would override the static IP ownership.

**Bridge-mapping name must match:** every NNCP `ovn.bridge-mappings[].localnet`
name MUST equal its CUDN `spec.network.localnet.physicalNetworkName`.
Every dedicated OVS bridge used by an OVN localnet mapping must set
`bridge.allow-extra-patch-ports: true`; otherwise NMState can reject or remove
the patch ports OVN owns.

**VLAN plumbing:** the child machine VLAN (151 dc1 / 152 dc2) and routed Ceph
client VLAN (171 dc1 / 172 dc2) are peeled off the single metal uplink as
subinterfaces and bridged separately into OVS. A dedicated NIC or bond works
too — adapt base-iface/VLAN id to the real DC fabric.

**MTU and routing:** the primary child CUDN stays at MTU 1500. The Ceph client
CUDN, VLAN interface, OVS bridge, child guest NIC, and physical parent uplink
use MTU 9000. Child NetworkConfigs route the Ceph daemon public networks through
the site client gateway. Parent NetworkConfigs route both client subnets through
their site gateway, while the external fabric owns forwarding in both directions
and the tiebreaker host's reverse path. Those client subnets do not belong in
`StorageCluster.spec.ceph.networks.publicCIDRs`, which declares Ceph daemon bind
networks rather than client allowlists.

**Namespace selection:** the target namespace must carry the label selected
by the CUDN `namespaceSelector`.

**Readiness barrier:** list the NNCP before the CUDNs in the manifest set, then
require its `Available=True` condition as well as both CUDN-derived NADs to
exist. Readiness checks are conjunctive, so the add-on cannot publish its ready
record — and the dependency planner cannot release child VM creation — while
the bridge policy is still converging. Object existence alone is not evidence
that the VLAN subinterfaces and OVS bridge mappings are active. The manifest
add-on also requires the `nmstate` capability, advertised by this example's
dedicated Kubernetes NMState Operator add-on after its CSV, `NMState` object,
and webhook readiness gates pass, so Bootwright does not try to apply the NNCP
before the operator and CRD exist.

**What VMs consume:** each OVN-derived NetworkAttachmentDefinition (readiness
check: `resourceExists` `k8s.cni.cncf.io/v1`) is what the child VMs actually
attach to, via Multus networkName `<namespace>/<name>` — a (C)UDN's derived NAD
shares the object's name. `Machine.spec.network.config.interfaceAttachments[]`
maps `primary` and `ceph-public` to their distinct NADs; no trunk is assumed.
