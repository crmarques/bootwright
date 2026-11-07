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

A `Network` name follows the [one name rule](../api.md#object-names). Every
[cluster selection](environment.md#cluster-selection) retains every `Network`.
`Network` is optional: a graph may declare none.

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
| `spec.vlan` | integer 1 to 4094 | none | The 802.1Q VLAN ID the network is tagged with. Absent: the network is only carried untagged. |
| `spec.mtu` | integer 576 to 65536 | `1500`, materialized | The largest frame the network allows. It is a capacity, never derived into an interface MTU and never emitted to a host. |
| `spec.sites` | non-empty set of site names | none: every site | Each name is a site of the selected Environment's `spec.sites`, refused `api.reference` otherwise. |
| `spec.ipv4`, `spec.ipv6` | `{cidr, gateway}` | none | At least one is present. See [subnets](#subnets-and-gateways). |
| `spec.libvirt` | presence arm | none | The one realization arm. See [the libvirt arm](#libvirt-arm). |

### Lifecycle

`spec.provided` mirrors a Machine's
[`os.provided`](machines.md#os-lifecycle-and-substrate-invariants) and sits
beside the arms as the [union rule](../api.md#unions) allows: the arms are a
presence union of at most one. A provided Network with an arm is refused at the
arm, because a provided network is realized by nothing; a Network that is not
provided and names no arm is refused at `$.spec`, naming the remedy `spec.libvirt`.
Both refusals are `api.invariant`.

A Network with `provided: false` is
[admitted but unrealized](../api.md#admitted-but-unrealized) until
[B1204](../milestones/m1.md#b1204): `validate` warns `api.unrealized` at
`$.spec.provided` and `plan` refuses it with `lifecycle.unsupported`, naming the
Network and the remedy of providing it outside Bootwright.

### Subnets and gateways

- `cidr` is an IP prefix of the block's family with no host bits set. A prefix
  with host bits set is refused at the field with its canonical spelling as the
  remedy; it is never masked. Normalization writes the canonical text of an
  admitted prefix and of the gateway.
- `gateway`, optional, is an IP of the block's family inside `cidr`. On IPv4
  below /31 it is neither the network nor the broadcast address. On IPv6 it is
  neither the Subnet-Router anycast address (the all-zero interface identifier,
  RFC 4291) below /127, nor a reserved subnet anycast address (RFC 2526): the
  highest 128 interface identifiers of a /64 in the modified EUI-64 format, or
  the highest 128 addresses of another prefix up to /121.

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
| `bridge` | bridge name | required | The bridge-name grammar of a provider's libvirt attachment. |
| `forward` | `nat` or `none` | `nat` | The libvirt forward mode. |
| `hostAddress` | IP | the IPv4 gateway, else the IPv6 gateway | The host's address on the bridge: a host address inside the CIDR of its family, under the rules a gateway follows. Required when the Network declares no gateway. |
| `taggedNetworkRefs` | set of `Network` references | none | The VLAN networks guests may stack on this bridge. Each declares `vlan` and is not the Network itself. |

Admission checks shape and references only; the realization, and the host-wide
rules between managed networks, arrive with
[B1204](../milestones/m1.md#b1204).

