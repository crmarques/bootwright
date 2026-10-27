# Storage field detail

[B472](../milestones/m2.md#b472) revives this detail when it qualifies IBM Ceph
9.9.x lifecycle inputs; [B55](../milestones/m7.md#b55) later reuses that
qualification for standalone native files. Until then this page holds only the
shapes that no admitted kind owns: the shared lexical values of native service
input and the nested shapes of the retired embedded-service kinds. Each
admitted kind's field record lives on the one page its row in the
[kinds table](../api/ceph.md#kinds) links, and the closed schema in
`api/v1alpha1/storage.go` is authoritative for field names, types and shapes.
The [native input](../ceph-native-api.md) replaces the role-derived placement,
daemon helpers and combined NFS service and export ownership of those kinds,
and current admission refuses the families they described. Native defaults
still require B472 qualification.

## Shared shapes

### Storage lexical values

The following grammars match the complete value, without surrounding
whitespace, signs, fractional parts or additional suffixes. `N` denotes one or
more decimal digits with a positive integer value; leading zeros are preserved.
Units and tokens are case-sensitive. These checks qualify declarative syntax,
not compatibility with a particular native release.

| Fields | Accepted value |
| --- | --- |
| Prometheus `retentionTime` | `N` followed by one of `y`, `w`, `d`, `h`, `m`, `s`. |
| Prometheus `retentionSize` | `N` followed by one of `B`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB`. |
| OSD `blockDBSize`, `blockWALSize` | `N`, optionally followed by `B`, `K`, `M`, `G`, `T`, `P`, `E`, `KB`, `MB`, `GB`, `TB`, `PB`, `EB`. |
| Device selector `size` | One bound, `LOW:HIGH`, `LOW:`, or `:HIGH`; each bound is `N` followed by `M`, `G`, `T`, `MB`, `GB`, or `TB`. Both bounds cannot be empty. Compare bounds using powers of 1024 and require `LOW <= HIGH`. |

### StoragePlacement

`StoragePlacement` is used by monitoring, generic services, MDS, RGW, NFS, and
ingress declarations.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `hosts` | array of strings | no | role-derived placement where the owner defines a role | Ordered unique storage node names. Machine names are not accepted. |
| `sites` | array of strings | no | — | Ordered unique topology site names. |
| `countPerHost` | integer | no | native default | Non-negative native `count_per_host`. |

When both `hosts` and `sites` are present, every selected host must belong to a
selected site. A generic `services[]` entry and an NFS service must explicitly
set `hosts` or `sites`; role-owned first-class services may derive placement
from topology roles. Placement never accepts a label, host pattern, count, or
free-form expression.

An explicit host list remains fixed when topology roles change, and every
listed host must still satisfy the owning service's role constraints. Omitted
role-owned placement follows the eligible role membership.

### StorageIngress

`StorageIngress` is a shared nested shape for a service-owned VIP, not a
resource kind. It does not choose a backend or share service ownership. The
management gateway retains its own transport, TLS, authentication, and
lifecycle rules.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Unique native ingress service name. |
| `address` | string | yes | — | Canonical VIP address. |
| `prefixLength` | integer | yes | — | Valid for the VIP address family. |
| `virtualInterfaceNetworks` | array of strings | no | `[]` | Ordered unique valid CIDRs; effective state masks host bits. |
| `placement` | `StoragePlacement` | no | ingress-role hosts | Stretch placement covers both data sites unless site-narrowed. |
| `firstVirtualRouterID` | integer | no | native default | Zero or omission leaves the native default; a non-zero value is `1..255` and unique on overlapping ingress networks. |

VIPs, prefixes, potentially overlapping ports, placements, and overlapping
networks obey the
[complete-graph invariants](../api/ceph.md#complete-graph-invariants). An
ingress collection is a set keyed by `name`.
