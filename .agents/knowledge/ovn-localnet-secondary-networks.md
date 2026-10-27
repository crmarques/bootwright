# OVN-Kubernetes localnet secondary networks

How a localnet ClusterUserDefinedNetwork (CUDN) reaches an external network
and what fails when it does not. [Container clusters](../../specs/openshift-clusters.md)
owns the required behavior; this page records native facts for
[B726](../../specs/milestones/m3.md#b726), the secondary networks of a
OpenShiftCluster, and
[B719](../../specs/milestones/m3.md#b719), which renders the install-time
settings they depend on.

Provenance: `R<n>:C<m>` is a claim id of the M3 research, `RV` marks a claim the
review re-verified, and "observed by the v1 implementation (main-v1)" marks a
fact to re-verify on this tree. Release labels name where a fact was read; they
are not supported or required versions (D163). The ids name the M3 definition's
research and review notes of 2026-10-20, which this repository does not track;
each statement here carries its fact, and an id only records where it was read.
The v1 lesson ids name review notes of the main-v1 branch, likewise untracked.

## The objects

- A CUDN with topology `Localnet` sets `spec.network.localnet`: `role`
  (required, `Secondary` only); `physicalNetworkName` (required, 1 to 253
  characters, no `,` or `:`, equal to an NNCP bridge mapping's `localnet`);
  optional `subnets` (one IPv4 and one IPv6 CIDR at most); `excludeSubnets` (at
  most 25); `ipam` (`mode` `Enabled` or `Disabled`, and `lifecycle:
  Persistent`); `mtu` (576 to 65,536, at least 1,280 with an IPv6 subnet; 1,500
  is rendered when omitted); and `vlan` (`mode: Access` with `access.id` 1 to
  4094; omitted means the underlay's default VLAN). Its CEL rules: `subnets`
  is required when `ipam.mode` is `Enabled` or unset and forbidden otherwise;
  `lifecycle: Persistent` needs `ipam.mode: Enabled`; `excludeSubnets` must be
  unset when `subnets` is (R3:C15). Its network definition is immutable after
  creation, so a change is a delete and a re-create (R3:C18, RV).
- For each namespace its `namespaceSelector` matches, the CUDN controller
  renders a NetworkAttachmentDefinition (NAD) named as the CUDN, with an owner
  reference to it, the label `k8s.ovn.org/user-defined-network` and the
  finalizer `k8s.ovn.org/user-defined-network-protection`; its CNI network name
  is `cluster_udn_<cudn name>` (R3:C19). A hand-made NAD with the CUDN's name
  blocks it: the CUDN reports `SyncError` with "foreign NetworkAttachmentDefinition
  with the desired name already exist". A healthy CUDN reports `NetworkCreated`
  `True`.
- NodeNetworkConfigurationPolicy (NNCP) readiness reads its conditions
  (R3:C29, read from upstream main, not pinned to shipped operator builds;
  medium): reason `SuccessfullyConfigured` is `Available` `True`,
  `FailedToConfigure` is `Degraded` `True`, and `NoMatchingNode` is `Ignored`
  `True` with `Available` `False` because no node matched `nodeSelector`, a
  plain map without `matchExpressions`. Per-node results are
  NodeNetworkConfigurationEnactment objects.
- Two NADs on one physical network that reference the same VLAN ID break
  OVN-Kubernetes networking, so each needs a unique VLAN (R3:C10).
- The NMState handler's connectivity probe resolves `root-servers.net` by
  default. On a disconnected cluster the `NMState` resource's
  `spec.probeConfiguration.dns.host` names a host the cluster resolves, or the
  DNS server answers an NS query for `root-servers.net`; otherwise NNCP
  enactments can fail the probe and roll back (R3:C28).
- The shipped NMState CSV supports own, single and all namespaces and declares
  disconnected support (`M4 RV-R1`, the M4 definition's untracked review of OLM
  and catalogs, corrections 1 and 2). An earlier reading took the upstream
  `bundle/` template for the shipped CSV and found an install-mode
  contradiction that does not exist.

## Bridge mappings and the primary interface

- A single-NIC node maps `br-ex`, which no NNCP may change after installation
  (R3:C23, RV). A secondary network on the primary interface therefore reuses
  the existing mapping.
- Bridge mappings that exist beside the `physnet` mapping are preserved when a
  policy adds its own (R2:C23, RV).
- A virtual machine attached to a localnet network needs `ipam.mode: Disabled`
  (R3:C37).

## Wiring lessons from v1

Observed by the v1 implementation (main-v1); re-verify on this tree (v1 lesson
ids L1 NW-7 and L2-N01 to N09):

- Omitting `ipam` defaults its mode to Enabled and then requires `subnets`;
  enabling IPAM overrides Bootwright-owned static addresses, so the network sets
  `ipam.mode: Disabled` and no subnets.
- Each NNCP `ovn.bridge-mappings[].localnet` equals its CUDN
  `physicalNetworkName`.
- A dedicated OVS bridge used for a localnet mapping needs
  `bridge.allow-extra-patch-ports: true`, or NMState can reject or remove the
  patch ports OVN owns. VLAN subinterfaces are peeled off the uplink into a
  bridge of their own. Mapping to `br-ex` versus a dedicated bridge is a design
  choice.
- The namespace a CUDN selects must carry the label its `namespaceSelector`
  names.
- The NAD a (C)UDN derives shares the object's name, and a VM attaches by
  `<namespace>/<name>`.
- The NNCP applies asynchronously. The CUDN and NAD can exist while the bridge
  is not live, so readiness is the NNCP `Available`, then the CUDN, then the
  derived NADs.
- The NNCP fails before the NMState operator's CRDs and webhook exist.
- MTU is a property of each layer: a 9000-byte storage client path needs the
  CUDN, VLAN interface, bridge, guest NIC and physical parent to carry it, and
  a routed client network needs a return path through the site gateway.
