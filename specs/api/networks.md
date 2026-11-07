# Networks

`Network` declares one layer-2 network: at most one VLAN, at most one subnet per
address family with an optional gateway, an MTU capacity and the sites that
reach it ([D240](../milestones/backlog.md#d240) (1), (7), (9), (12)). The
schema in `api/v1alpha1/networks.go` is authoritative for its shape and
`internal/network` owns its admission; the realization arm is checked by the
substrate that realizes it. `NetworkConfig` keeps its name beside it
([D240](../milestones/backlog.md#d240) (14)): a `NetworkConfig` is one host's
operating-system network configuration, a `Network` is the network those hosts
attach to. The examples keep one `Network` per file under `infra/networks/`.

A `Network` name follows the [one name rule](../api.md#object-names);
[cluster selection](environment.md#cluster-selection) says what retains it.
`Network` is optional: a graph with contacts only needs none, except as the
[cross-kind checks](#cross-kind-checks) say.

## Kinds

| Kind | Declares |
| --- | --- |
| `Network` | One layer-2 network with its VLAN, subnets, gateways, MTU capacity and sites, provided outside Bootwright or realized by one arm. |

## Network

Effective output emits the fields in this order, which is the schema's.

| Path | Type | Default | Rule |
| --- | --- | --- | --- |
| `spec.provided` | boolean | required | `true`: the network exists outside Bootwright, which names and checks it and never changes it. `false`: Bootwright realizes it through exactly one arm. |
| `spec.description` | string | none | Inert. |
| `spec.vlan` | integer 1 to 4094 | none | The 802.1Q VLAN ID the network is tagged with. Absent: carried only untagged. |
| `spec.mtu` | integer 576 to 65536 | `1500`, materialized | The largest frame the network allows: a capacity, never derived into an interface MTU, and emitted only as a [managed network's](#libvirt-arm) MTU. With `spec.ipv6`, below 1280 (RFC 8200) is refused. |
| `spec.sites` | non-empty set of site names | none: every site | Each name is a site of the selected Environment's `spec.sites`, refused `api.reference` otherwise. |
| `spec.ipv4`, `spec.ipv6` | `{cidr, gateway}` | none | At least one is present. See [subnets](#subnets-and-gateways). |
| `spec.libvirt` | presence arm | none | The one realization arm. See [the libvirt arm](#libvirt-arm). |

### Lifecycle

`spec.provided` mirrors a Machine's
[`os.provided`](machines.md#os-lifecycle-and-substrate-invariants) and sits
beside the arms as the [union rule](../api.md#unions) allows: the arms are a
presence union of at most one. A provided Network with an arm is refused at the
arm, since nothing realizes it; one not provided that names no arm is refused
at `$.spec`, naming every arm. Both refusals are `api.invariant`.

A Network with `provided: false` is realized by its arm; there is no block per
Network ([D240](../milestones/backlog.md#d240) (13)).

### Subnets and gateways

- `cidr` is an IP prefix of the block's family with no host bits set, else
  refused at the field naming its canonical spelling, never masked.
  A zero-length, loopback (`127.0.0.0/8`, `::1/128`) or multicast
  (`224.0.0.0/4`, `ff00::/8`) prefix is refused there (`api.value`).
  Normalization writes the canonical text of an admitted prefix and gateway.
- `gateway`, optional, is an IP of the block's family inside `cidr`. On IPv4
  below /31 it is neither the network nor the broadcast address. On IPv6 it is
  neither the Subnet-Router anycast address (the all-zero interface identifier,
  RFC 4291) below /127, nor a reserved subnet anycast address (RFC 2526): the
  highest 128 interface identifiers of a /64 in the modified EUI-64 format, or
  the highest 128 addresses of another prefix up to /120.

### One 802.1Q domain per site

Two Networks whose site sets overlap, where an absent set overlaps every set,
are refused on each of them, naming both, when they declare the same `vlan`, or
when their CIDRs of one family overlap. This assumes one 802.1Q domain per site
([D240](../milestones/backlog.md#d240) (9)).

### Libvirt arm

`spec.libvirt` is admitted only with `provided: false` and declares a libvirt
network defined on a provider's host. Its fields, in emitted order:

| Path | Type | Default | Rule |
| --- | --- | --- | --- |
| `providerRef` | `InfraProvider` reference | required | An `InfraProvider` with a `libvirt` arm. |
| `bridge` | bridge name | required | The bridge-name grammar of a provider's libvirt segment. |
| `forward` | `nat` or `none` | `nat` | The libvirt forward mode. |
| `hostAddress` | IP | the IPv4 gateway, else the IPv6 gateway | The host's address on the bridge: a host address inside the CIDR of its family, under the rules a gateway follows. Required when the Network declares no gateway. |
| `taggedNetworkRefs` | set of `Network` references | none | The VLAN networks guests may stack on this bridge. Each declares `vlan` and is not the Network itself. |

The host block of the provider `providerRef` names defines the network
([provider host realization](../substrates.md#provider-host-realization)) as
`bootwright-<context>-<name>` with `hostAddress` and its CIDR's prefix; its
`mtu` is emitted only when it is not 1500. The Network is a
[segment](machines.md#machine-profiles-and-segments) of that provider under its
own name, carrying itself untagged and `taggedNetworkRefs` tagged: a Machine
NIC's `segmentRef` names it, and a provider segment of the same name refuses at
`providerRef`. `taggedNetworkRefs` realizes nothing: the bridge carries guest
VLAN tags.

Between the managed Networks of one host's providers, each refused
`api.invariant` at the Network, naming both Networks and their providers: no
other managed Network or provider segment on that host names its `bridge`,
and no other managed Network's prefix overlaps its own, IPv4 or IPv6, unless
[one 802.1Q domain per site](#one-8021q-domain-per-site) already refuses the
pair. One host defines at most 20 managed Networks, refused once (`api.value`)
at the first by name past the bound, naming each Network over it. A managed
Network's `cidr` is shorter than /32 or /128, leaving guests an address.

## Cross-kind checks

Each kind below refuses, with `api.invariant` (fabric carriage `api.required`)
naming the object, reason and next step; a contacts-only graph still meets
fabric carriage and Ceph networks:

- **Interface assignments** (Machine, at `spec.network.addresses[].address`):
  every [interface assignment](machines.md#addresses-and-access) lies in
  exactly one Network of its family, scoped to the Machine's `placement.site`
  when it has one, and takes that Network's prefix: the composed NMState
  `prefix-length`, the Kickstart netmask, the install-address checks and the
  node subnets read it.
- **VLAN agreement** (Machine, at `spec.network.addresses[].interface`): when
  the assigned interface's composed NMState stack carries a VLAN, itself or
  along a bridge or bond port path (from an `ovs-interface` through its
  bridge's other ports), every such `vlan.id` equals the Network's `vlan`, and
  a Network without `vlan` refuses a tagged stack.
- **Carriage** (Machine, at the same field): each NIC under the assigned
  interface sits on a [segment](machines.md#machine-profiles-and-segments)
  that carries the Network tagged when a VLAN lies on the path to it, else
  untagged, each segment of a bridge alike; a bond across segments is the bond
  rule's. Every other available VLAN on a NIC tags the `vlan` of a Network
  its segment carries tagged (at the network configuration). Each
  refusal names the Machine, interface, NIC, segment and Network; the remedy
  adds the Network to the segment's carriage, never tagged on KubeVirt or
  vSphere, or moves the NIC.
- **Capacity** (Machine, at `spec.network`): an interface's effective MTU does
  not exceed the `mtu` of a Network it holds an assignment on.
- **Segment selection** (Machine, at its bare-metal or vSphere `nics` entry): on
  a provider with two or more segments, a NIC an available ethernet interface
  matches names its `segmentRef`.
- **Fabric carriage** (InfraProvider, at `spec.segments[].untaggedNetworkRef`):
  a KubeVirt or vSphere segment carries one Network untagged.
- **Install gateway** (Machine, at `spec.network.installAddressRef`): when the
  install address's Network declares a gateway of that family, the next hop
  of the install default route equals it, beside the rule that it lies inside
  the install prefix. A link-local next hop is not compared.
- **Cluster overlays** (OpenShiftCluster, at
  `spec.networking.clusterNetwork[].cidr` and `serviceNetwork[]`): no overlay
  overlaps a Network CIDR, beside the
  [node subnet overlaps](openshift-clusters.md#networking).
- **Ceph networks** (CephCluster, at `spec.networks.publicCIDRs[]` and
  `clusterCIDRs[]`): every entry equals the CIDR of a declared Network
  ([D240](../milestones/backlog.md#d240) (11)).
