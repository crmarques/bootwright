# Ceph API review for M2a

This page records the [M2a](milestones/m2.md#m2a) API and operator-journey
review. It records the M2a baseline and approved target. M2b now implements
common native service admission and minimal `StorageClient`;
[storage admission](api/storage.md) states the current subset and gates.
[Storage admission](api/storage.md) and `api/v1alpha1/storage.go` still own
accepted input. [Storage](storage.md) owns target lifecycle behavior;
[coverage](storage-coverage.md) owns provider qualification.

**Review state:** current schemas and their validation/normalization paths
reviewed; the owner approved P1 and P2 in D133. The
[native service contract](storage-native-api.md) and
[resource contract](storage-resources.md) own the reconciled target.
The admitted subset is stated where it lives: P1's core native-service
subset, the `StorageClient` kind, `clusterSSH.transport` and `dataDisks` media
are admitted ([storage](api/storage.md), [machines](api/machines.md)); every
other field, kind or command here is target only. The remaining implementation
stays in M2b to M2e. Examples are synthetic, partial where marked, and deliberately
outside the runnable `examples/` tree.

## Design criteria

An operator should declare the intended cluster, its services and its storage
resources without translating a known native specification into a second
service vocabulary or writing an imperative script. Preserve a native
declarative format when it expresses that intent well. Add Bootwright
declarations for imperative-only resources and for the infrastructure,
dependency, trust and ownership facts native inputs do not express.

Keep the existing kind/metadata envelope, explicit `clusterRef`, Machine and
Secret references, bootstrap/release selection, pool and filesystem intent.
No evidence justifies renaming these merely for uniformity. Native fidelity
does not mean accepting unqualified features, secret bytes in public input,
foreign hosts, unsafe container options or unbounded disk discovery.

This review uses IBM 9.9.x and matching upstream Tentacle descriptions.
The [release record](storage-coverage.md#release-qualification) distinguishes
documentation evidence from exact entitled binary/schema qualification.

## Existing API review

The [field detail](deferred/storage-api.md) remains the current field inventory.
The table records decisions and gaps rather than duplicating every field.

| Surface reviewed | Keep or improve, with operator impact | Work owner |
| --- | --- | --- |
| Cluster type, management and distribution | Keep explicit ownership and distribution. External-cluster admission does not promise an M2 managed lifecycle; do not infer a distribution from a version string. | [B472](milestones/m2.md#b472), [B80](milestones/m2.md#b80) |
| Release, package, image, repository and entitlement | Keep exact coordinates and separate entitlement/trust. Explain which coordinate belongs to which component and freeze auxiliary images as well as Ceph. A reusable release-profile abstraction is not justified by this review. | B472, [B493](milestones/m2.md#b493) |
| Bootstrap host, SSH and networks | Keep Machine/address/Secret binding and distinct public/replication networks. Diagnose absent addresses and network mismatches at their authored field; do not invent a second host inventory. | [B317](milestones/m2.md#b317), [B490](milestones/m2.md#b490) |
| FIPS, CephX, encryption and call-home | Keep explicit policy. Validate combinations against host and release prerequisites, with no silently weakened choice. | [B75](milestones/m1.md#b75), [B346](milestones/m1.md#b346), B472 |
| Cluster configuration, manager modules and workarounds | Keep native config keys; reject collisions with generated ownership. Freeze native defaults and qualify module/workaround necessity per patch. Do not turn config into a way to bypass the resource graph. | [B474](milestones/m2.md#b474) |
| Generic services and placement | Current `serviceType`, `serviceID`, custom placement and inner `spec` preserve only part of ServiceSpec. Missing native `count`, `label` and `host_pattern` prevent useful native placements; MON/MGR cannot use the generic path. P1 proposes full native documents. | B472, B474 |
| Topology names, FQDNs, sites, roles and labels | Keep proved Machine/site identity and deterministic hostnames. Site references must resolve. Roles and service placement currently duplicate daemon intent; P1 proposes service-owned placement. CRUSH location needs qualified hierarchy support, not merely an unconstrained failure-domain string. | [B316](milestones/m2.md#b316), B474, [B81](milestones/m2.md#b81) |
| OSD selectors, data/DB/WAL, drivegroups and encryption | Current renamed DriveGroup fields add learning cost and whole-host exclusivity excludes native multi-spec layouts. Preserve native syntax under P1; own concrete devices and allocation extents. Explicit `all: false` and cross-selector alias/overlap need distinct validation fixes. | B474, [B475](milestones/m2.md#b475), [B494](milestones/m2.md#b494) |
| Monitoring, management gateway, OAuth and ingress | Current helper shapes omit native fields and force gateway ingress. OAuth allowlist grammar excludes a documented host-and-port form. Consolidate daemon declarations under P1; keep protected credential bindings and explicit endpoint dependencies. | [B479](milestones/m2.md#b479), B474 |
| Stretch | Keep explicit topology and provider constraints. Incomplete tiebreaker input currently warns and does not enable stretch or prove stretch readiness. B486 must settle whether managed execution refuses deferred activation; changing current admission requires an explicit API decision. | [B486](milestones/m2.md#b486), [B487](milestones/m2.md#b487) |
| StoragePlacementPolicy | Keep a reusable CRUSH/pool policy and one owner of protection. Validate actual failure-domain/class capacity, including omitted native defaults. Model supported hierarchy/rule capabilities deliberately rather than offering arbitrary native commands. | B81, B494 |
| StoragePool | Keep declarative replicated/EC protection, application, PG/autoscaler, quota, compression and mirroring intent. Freeze effective protection; distinguish omitted values from explicit native unlimited zero. Add explicit RBD image/data-pool resources under P2. | B81, B494 |
| StorageFilesystem | Keep pool references, default data pool and filesystem/MDS resource policy. Daemon placement/configuration moves under approved P1. Preserve groups and add subvolumes under P2. A group layout must refer to a data pool attached to that filesystem. | [B492](milestones/m2.md#b492), B494 |
| StorageObjectGateway | Current kind combines an RGW daemon and public endpoint, with realm/zone names but no object resources. P1 separates daemon/ingress specifications; P2 defines realm/zone and user/bucket ownership. Avoid silently creating default pools or credentials. | [B477](milestones/m2.md#b477) |
| StorageNFSExport | Current name actually owns one NFS service and an array of exports, with reduced client/FSAL fields. P1 owns daemons; P2 makes individual exports addressable and preserves native export JSON semantics. | [B478](milestones/m2.md#b478) |
| StorageExport | Keep the existing Data Foundation handoff purpose. Do not turn it into a miscellaneous command bag. Its generated material needs a deliberate sensitive-output boundary; Secret commands cannot reveal produced material. | [B489](milestones/m2.md#b489) |
| Missing service/resource families | RBD images, subvolumes, object users/buckets, client principals, gateway mappings, SMB manager resources and replication peers need explicit ownership. P2 supplies the model; each feature item supplies its exact release-qualified fields and mechanisms. | B81, B477, [B480](milestones/m2.md#b480) to [B483](milestones/m2.md#b483), B489, B492 |

### Native evidence and deliberate exceptions

- The [Tentacle ServiceSpec](https://docs.ceph.com/en/tentacle/cephadm/services/)
  includes top-level configuration, networks, container arguments and custom
  configuration as well as `spec`. [IBM placement](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=services-placement-specification-ceph-orchestrator)
  includes native selection forms absent from current Bootwright placement.
  Preserving only the inner map does not preserve the whole native contract.
- [Native OSD specifications](https://docs.ceph.com/en/tentacle/cephadm/services/osd/)
  allow multiple specifications on one host and can keep consuming matching
  devices. Multiple disjoint strategies are useful; continued discovery is
  incompatible with Bootwright's frozen destructive authorization. Keep the
  authored selector, but freeze execution to proved devices and allocations.
- [Native NFS modes](https://docs.ceph.com/en/tentacle/cephadm/services/nfs/)
  distinguish HAProxy from keepalive-only ingress. Backend-port rules and
  client-IP forwarding depend on the mode. The current unconditional ingress
  port rule cannot describe every supported mode.
- [IBM management gateway examples](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=gateway-example-configurations-mgmt-high-availability)
  use OAuth allowlist entries with ports. The current DNS-only validator is
  too narrow; arbitrary URLs are not the remedy. Use the qualified native
  grammar and explicit protected secret fields.
- [NFS export JSON](https://docs.ceph.com/en/tentacle/mgr/nfs/#create-or-update-export-via-json-specification)
  and [IBM SMB resources](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=method-smb-service-resource-specifications)
  already express desired resources. Retain their field names and structured
  values, with bound references and secrets. JSON may be represented as a
  structured YAML mapping in Bootwright, but the native export artifact is
  JSON; do not call it native NFS YAML.

Native container arguments, mounts and configuration are capability-bearing
input even without shell evaluation. B474 classifies allowed effects and
rejects unsafe options. Native `unmanaged` flags and removal intents cannot
override Bootwright ownership or destructive authorization. SMB manager-owned
services and NFS manager-owned authentication objects have one derived owner;
they must not also be independently declared as managed services/clients.

## P1: native service documents

**Approved, D133; core subset admitted.** The core native-service subset is
admitted ([storage](api/storage.md)); the family services stay target only.
Replace the collection of daemon-specific helper
shapes with complete native ServiceSpec documents in the existing
`StorageCluster.spec.ceph.services` list. Preserve native snake case, nesting,
types and PlacementSpec, including OSD DriveGroupSpec. Keep Bootwright's
cluster/bootstrap envelope and proved Machine inventory.

Current partial input for two Prometheus instances:

```yaml
spec:
  ceph:
    monitoring:
      enabled: true
      prometheus:
        placement:
          hosts: [host-a, host-b]
        retentionTime: 14d
        retentionSize: 20GB
```

Proposed partial input, using native placement and retention vocabulary:

```yaml
spec:
  ceph:
    services:
      - service_type: prometheus
        placement:
          count: 2
          label: monitoring
        spec:
          retention_time: 14d
          retention_size: 20GB
```

The label must resolve against declared, proved hosts. Its eligible host set
and service cardinality become frozen planning facts; continuation cannot
silently target new hosts. Native selections retain their authored form in
input, while frozen execution reflects the approved finite target set. Native
HA may reschedule daemons inside that set; exact initial assignments are not frozen.

Current partial OSD drivegroup:

```yaml
spec:
  ceph:
    topology:
      osdDrivegroups:
        - serviceID: data
          placement:
            hosts: [host-a]
          osd:
            dataDevices:
              paths: [/dev/disk/by-id/example-data-a]
            encrypted: true
```

Proposed native service entry:

```yaml
spec:
  ceph:
    services:
      - service_type: osd
        service_id: data
        placement:
          hosts: [host-a]
        spec:
          data_devices:
            paths: [/dev/disk/by-id/example-data-a]
          encrypted: true
```

The second form allows multiple non-overlapping native strategies on a host;
it grants no authority to use an unproved disk. Native selector scalar types,
including rotational filters, come from the selected provider schema.

Under the approved target service placement replaces `topology.roles`. Topology
continues binding native hostnames to Machines/sites and qualified labels/CRUSH
location. It does not redeclare daemon placement. Explicit MON/MGR services
and qualified bootstrap defaults must cover the minimal cluster without a
second role-inference system.

The migration covers `monitoring`, `mgmtGateway`, ingress helper objects,
OSD helper objects, and the daemon portions of filesystem/RGW/NFS resources.
It does not remove resource kinds merely because their daemon configuration
has moved. In particular, an object gateway's logical endpoint and object
resource ownership still need an explicit owner and binding to its native
RGW service; public DNS/trust facts remain Bootwright declarations.
Resource properties such as CephFS active ranks remain owned by their
resource, not duplicated in a daemon spec. RGW realm/zone references,
filesystem-to-MDS identity and ingress backend names create checked graph
edges. A native name locates a declared owner; it never authorizes adoption.

Secret leaves are omitted from the public native payload and supplied through
the [shared binding grammar](storage-native-api.md#secret-bindings).
B474 qualifies its closed per-release leaf registry, Secret types and native
serialization. Unsupported paths and duplicate authored leaves refuse. Native
file inclusion is not introduced; an eventual file-loading API needs its own
path/trust contract.

Native service identity uses the qualified service type plus service ID,
not a globally unique bare ID. For example, IBM's
[NFS ingress example](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=servers-configure-multi-active-nfs-gateway)
uses the same ID for an NFS service and its ingress. Backend references must
resolve the full native identity. The existing null/alias/duplicate-key
prohibitions in the [input grammar](api.md) remain unless separately approved;
native fidelity here describes the qualified subset, not arbitrary YAML.

This is an approved breaking v1alpha1 authoring change. B474 updates schema,
admission, effective state, diagnostics, examples and migration guidance
together. It explicitly rejects retired helpers rather than silently ignoring
them, translating both competing shapes, or rewriting operator files.
Existing realized storage operations do not exist, but existing authored
declarations still need a documented migration.

Alternative considered: keep all helper shapes and add a native escape hatch
to each. This preserves old syntax but creates multiple default/placement
owners and repeats provider schema maintenance. The review recommends P1
because one native service path exposes concrete missing capabilities.

## P2: declarative resource graph

**Approved, D133; `StorageClient` admitted.** The `StorageClient` kind is
admitted ([storage](api/storage.md)); the other resources stay target only.
Keep pools and filesystems as useful declarative
abstractions. Add independently referenceable resources where the native
provider offers only imperative creation. Preserve native resource documents
where the provider already offers a suitable declarative interface. Do not
offer arbitrary native command lists as desired state.

Today `StoragePool` can declare an RBD pool but cannot declare an owned image;
`StorageFilesystem` has groups but no subvolume resource. These proposed
documents show the added ownership and references:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageRBDImage
metadata:
  name: application-disk

spec:
  clusterRef: ceph-example
  poolRef: block-data
  sizeBytes: 10737418240

---
apiVersion: bootwright.io/v1alpha1
kind: StorageSubvolume
metadata:
  name: documents

spec:
  clusterRef: ceph-example
  filesystemRef: shared
  group: applications
  sizeBytes: 10737418240
```

The pool, filesystem and group are separately declared dependencies. An image
may additionally reference a qualified EC data pool and explicit features;
its metadata remains in a suitable replicated pool. A subvolume's group and
layout resolve within its selected filesystem. Size is an integer number of
bytes; the [resource target](storage-resources.md) specifies required image
size, optional subvolume quota and explicit-zero behavior. These examples
illustrate approved shapes; they are not admitted today.

Current partial NFS input combines daemon placement and multiple exports:

```yaml
spec:
  clusterRef: ceph-example
  serviceID: files

  placement:
    hosts: [host-a, host-b]

  exports:
    - pseudo: /documents
      filesystemRef: shared
      path: /
      accessType: RW
      squash: root_squash
      clients: [192.0.2.0/24]
```

Proposed individual export, with the NFS daemon declared through P1:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageNFSExport
metadata:
  name: documents

spec:
  clusterRef: ceph-example
  serviceID: files
  filesystemRef: shared

  native:
    path: /
    pseudo: /documents
    access_type: RW
    squash: root_squash
    protocols: [4]
    transports: [TCP]
    fsal:
      name: CEPH
      cmount_path: /
    clients:
      - addresses: [192.0.2.0/24]
        access_type: RO
        squash: root_squash
```

The protected runtime artifact is native JSON. `filesystemRef` binds the
Bootwright owner and supplies native `fs_name`. If an operator retains
`fs_name` when copying an existing native export, it must identify that same
filesystem; no repeated value is required just to satisfy the wrapper. The
binding must also support qualified subvolume and RGW bucket references
without accepting an ambiguous bare bucket name. Native-generated export
IDs, CephFS principals and credentials are captured, not chosen afresh on
retry. The implementation must document the small reference-binding wrapper;
it must not rename the native export fields.

The corresponding SMB share payload stays native, inside an owned SMB
resource declaration rather than a separately managed cephadm SMB daemon:

```yaml
resource_type: ceph.smb.share
cluster_id: files
share_id: documents
cephfs:
  volume: shared
  subvolumegroup: applications
  subvolume: documents
  path: /
readonly: false
```

The enclosing resource binds the declared filesystem/subvolume and SMB
cluster. Native names must agree with those bindings; authentication uses
Secret references instead of native inline passwords. The SMB manager owns
the daemon services it creates. P2 approves this separation. Native removal
intents and external adoption remain subject to their explicit ownership and
authorization boundaries.

### Required resource relationships

| Family | Declarative ownership and dependency path | Exact-field owner |
| --- | --- | --- |
| Block | Placement/pool → image → gateway namespace or LUN → authorized initiator/client → access material. Metadata/data-pool references and native image identity are explicit. | B81, B480, B481, B489 |
| File | Pools → filesystem → group/subvolume → NFS export or SMB share → authenticated/restricted client. No overlapping protocol exposure that the provider forbids. | B492, B478, B482, B489 |
| Object | Realm/zonegroup/zone and owned pool layout → RGW service → account/user → bucket and access policy → scoped access material. Native pool creation must have a named owner. | B477, B489 |
| NVMe-oF and iSCSI | Native daemon spec plus typed gateway/group, listener, subsystem/target, image-backed namespace/LUN and initiator permissions. Do not synthesize untracked images or credentials. iSCSI remains conditional on IBM support. | B480, B481 |
| SMB | Native cluster/share/join-auth/usersgroups resources with declared filesystem dependencies and bound secrets; generated daemon ownership stays with the native manager. | B482 |
| Replication peers | Directed relationships between declared clusters/resources, one owner of shared bootstrap material, explicit trust, credentials and initial synchronization. Any cross-context/external-peer arm needs a separate ownership contract before promotion. | B483 |
| Access and handoff | Typed CephX, S3 and gateway principals with scoped resource access; generated material enters produced custody. Keep Data Foundation export distinct from general client access. | B489 |

Every resource needs create, read-only observe, exact continuation and removal
semantics, including native identity and automatically generated children.
Native names alone never establish ownership. A change to a completed
declaration retains the existing destroy/fresh-apply boundary; this proposal
does not introduce a day-2 reconciliation loop.

Separate resources were selected because consumers can reference one
owned object, errors can name it, and its dependency/removal boundary is
visible. Nesting all objects inside pools/filesystems reduces document count
but makes cross-service references and individual ownership less clear.

## Admission and UX corrections

[B494](milestones/m2.md#b494) owns confirmed current validation gaps; it must
apply them to the approved target rather than repair helpers that a chosen
migration removes. They are recorded here as findings, not implemented fixes:

| Finding and evidence | Required correction/evidence |
| --- | --- |
| `internal/storage/devices.go` counts presence of `all: false` as a selector mode. | False selects no devices and must not conflict with an actual filter; preserve rejection of genuinely empty or conflicting selections. |
| `internal/storage/pools.go` checks replicated capacity only for explicitly authored `failureDomain: host`. | Resolve the effective qualified failure domain first, then check declared/live capacity; omitted host defaults must not bypass protection checks. |
| The same file checks subvolume-group layout only for same-cluster pool identity. | Require an eligible data pool attached to that filesystem, including default/layout and EC compatibility. |
| The same file skips MDS placement validation when the filesystem's `mds` block is omitted. | Check effective MDS coverage whether authored or derived; under P1, bind the selected native MDS service. |
| `internal/storage/endpoints.go` ignores omitted monitoring ports and compares only the first VRRP ID. | Resolve release defaults and all consumed listener/VRRP ranges before conflict checks; qualify overlap by interface/network and placement. |

Cross-selector data/DB/WAL overlap, aliases and shared-device allocation are
B475's device-proof work, including legitimate qualified shared DB/WAL cases.
Its capacity proof also distinguishes a path count from resolved device and
OSD counts, including `osdsPerDevice` and dynamic selectors.
Native placement/ingress/OAuth expressiveness gaps belong to B474 and their
service items. Existing partial/full validation duplication, missing site
resolution and misleading remedies remain B315, B316 and B317.

Two safe documentation corrections retain current API behavior: external
clusters cannot set `ceph`, so the documented external `clusterSSH` default is
unreachable; a sole filesystem data pool normalizes to `default: true`, even
when false was authored, so the general authored-value preservation statement
must acknowledge that existing exception. A future strict refusal instead
of that normalization is a separate API decision.

## Completion and promotion checklist

P1/P2 have a recorded disposition, each retained/new family has an owner and
bounded contract, and the reconciled definition baseline has no unassigned
decision. The [M2a readiness table](milestones/m2.md#m2a-readiness) owns the
completion state. Further substantial public API choices still need owner
approval; assigning them to an item does not approve their eventual design.

Before each implementation item is promoted, its exact field record covers
grammar and bounds; omission/null/empty/zero; default ownership; ordered/set
semantics; references and graph edges; native projection and restrictions;
Secret/produced-material handling; identity; create/observe/retry/remove;
destructive authorization; readiness and limits; diagnostics; compatibility;
and planned executable in-tree/operator exit evidence. Passing results gate
delivery, not definition. B474 maintains a field ledger classifying
direct native preservation, reference expansion, frozen default, deliberate
policy restriction and unsupported capability. No silent drop or override.

Exact entitled artifacts, provider-schema fixtures, measured host budgets,
native removal/fencing proof, executable tests and operator runs remain
explicit promotion/acceptance gates. Their absence is not filled with guessed
values and does not require implementing M2b to finish M2a. B489 must settle
its explicit sensitive-output command before delivery; this review neither
invents a command nor authorizes disclosure through existing Secret commands.
