# Managed Ceph coverage and qualification

This page owns the M2 provider coverage requirements. [Storage](storage.md)
owns lifecycle behavior, [storage admission](api/storage.md) owns public
intent, and [M2](milestones/m2.md) owns delivery and sequencing. Each row below
is required when supported by the frozen IBM release; an unqualified feature
is refused before effects, never silently omitted.

Not yet met: complete release qualification and native projection; tracked as
[B472](milestones/m2.md#b472) and [B474](milestones/m2.md#b474). B476's accepted
minimal run is recorded in
[X94](milestones/delivered.md#x94--post-destroy-cluster-inspection).

## Release qualification

IBM's
[release/package mapping](https://www.ibm.com/support/pages/what-are-red-hat-and-ibm-storage-ceph-releases-and-corresponding-ceph-package-versions)
is a reference for operators, not a Bootwright catalog. The
[storage API](api/storage.md) owns operator-selected version coordinates and
delegation of compatibility and installation failures to native tools (D157).
Bootwright does not register this table or maintain per-release profiles.

B472 records the artifacts, host/runtime, native schemas, defaults and observed
constraints of tested adapter and feature journeys. These records establish
the scope of that evidence; they do not approve version combinations or gate
an authored selection. The
[version-specialization catalog](../.agents/knowledge/version-specializations.md)
records the mechanics each tested build needed and the evidence for them; its
rows are tested scope, not approved versions. B476's accepted minimal journey is recorded in its
owner-accepted ledger row; it certifies no other version or feature.

Authored versions now select native acquisition without release/artifact pins.
[B615](milestones/delivered.md#b615)'s separate operator journey is accepted;
[X99](milestones/delivered.md#x99--operator-selected-ceph-artifacts-and-m2b-completion)
closes M2b's exit.

Native manager-to-host SSH must enforce pinned host keys, including a negative
wrong-key test through actual manager connection, reconnect and failover paths.
The [upstream Tentacle connection](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/ssh.py#L161)
disables that verification; controller-to-host verification
alone cannot qualify this independent native channel. No authored exception
can disable verification. `native` stays refused.
The approved [verified-tunnel target](storage-native-api.md#verified-native-orchestration-transport)
retains that requirement through its outer authenticated channel. Transport
proof applies to the selected artifacts and includes guest forwarding
restrictions, staged bootstrap, read-only MGR mounts, reconnection,
restart/failover and owned recovery/removal; prior per-version approval is not
an admission requirement.

Apply resolves the authored tag or digest in a controller-local image block,
using the declared licensed Entitlement and its protected registry Secret, before any
native effect. Resolution authenticates the selected manifest and platform
from the declared registry origin. Its TLS verifies that origin against the
Entitlement's `registry.trustBundleRef` alone when it is set, and against the
host trust store otherwise, for the Ceph image and each auxiliary image; an
origin the anchor does not verify refuses resolution before any effect, and
the native error names the registry and the anchor. A direct digest selects that artifact
without resolving or comparing it to a build-pinned tag. Resolution evidence
identifies the authored reference, selected digest and platform, and the cluster,
pool and client blocks depend on it. The result is frozen: continuation never
refreshes a tag or substitutes another release. Native acquisition failures
retain their cause and the requested coordinate through Bootwright's diagnostics.
The guest re-proves the pulled image's platform and repository digest. The
operator supplies neither a Podman login nor a locally cached image.
Credential delivery and the original acquisition binding remain Secrets-owned.
Signed package acquisition and the pull belong to host preparation. RHEL
installation media are a separate prerequisite.
Guest pull credentials are confined by the invocation-local broker described
in [storage](storage.md); the native client receives no real
registry credential or token. Synthetic boundary tests include foreign realms,
redirects, uncredentialed bypass, local mutual-TLS access, a registry trust
bundle that verifies, one that does not, and none, and a package repository
trust bundle that verifies, one that does not, and none.

Each host-preparation block derives account, code and crypto-policy facts from
the exact immutable image pulled with the original registry binding. Its
contained probes have no network, credential mounts, inherited image
environment, hooks, automatic health checks or image volumes. Image code and
host RPM payloads are independently authenticated and bound to their frozen
artifacts; equality between different selected artifacts is not a version
compatibility gate. Installed RPM metadata agreement alone is not publisher
authentication, and an operator report cannot create a runtime authority.
The same contained image's SDK normalizes only the frozen core ServiceSpec
documents, including the code-owned protected MGR mount. These native defaults
are retained for exact stored-service comparison; Bootwright does not invent
or copy the provider's defaults. A held empty mount manifest disables Podman's
implicit host subscription copies before any probe starts.
Each probe's inspected configuration must prove the original image/user/code,
closed environment, read-only root, exact mounts, private namespaces, empty
effective/bounding capabilities and bounded resources before starting or
removing its original container. Creation flags alone supply no such proof.
The closed environment pins the hostname and its `HOSTNAME`, because Podman
adds one to a started container that sets none, so the proof before the start
and the one before removal read the same five variables. Exact mounts are the
requested read-only relabelled binds with only the propagation and `nosuid`,
`nodev`, `rbind` options Podman records beside them, and without `z` once the
start has relabelled the source. Podman's inspection reports no sysctls, so
they are the one setting proved from Podman's recorded create command, which
must name exactly the requested ones.
Image facts do not qualify the actual native SSH route or running MGR mounts.
The route probe exercises the authenticated provider SSH class inside the same
immutable image, under the host's inherited crypto policy, using only fresh
synthetic keys and loopback listeners in a private network namespace.
Correct-key forwarding must succeed; changed-key, unknown-target,
unavailable-proxy and missing-configuration cases must refuse with no direct
contact or remote effect. The host block records the result with its runtime
profile after exact container absence; an operator report cannot substitute
for it. These bounded cases do not qualify real guest forwarding policy or
running MGR restart/failover.
Probe cleanup proves each returned original container ID absent; a lost
creation reply retains uncertainty, without adopting a matching label or name.
Implementation is `ceph_host_runtime.py`, a session-free module the worker
ships; the complete live consumer and qualification remain
[B79](milestones/m2.md#b79) and [B80](milestones/m2.md#b80).

The package phase authenticates inherited crypto-policies roots from the
frozen RHEL DVD and lends completed original generator/profile facts to a
descriptor-bound predefined OpenSSH-server policy alias resolver. Image
provenance never substitutes for that host-OS proof.

The host block's contained probe derives each selected SCSI SSD's complete
native device identifier through the exact image's SDK, using the host's udev
properties bound to the proved path, device number, serial, size and
nonrotational presentation; a virtio disk yields its serial form. It records
the identities before OSD realization. Later health checks compare the
complete identifiers and original OSD identities, and prove the SSD class; a
guessed serial projection or prefix-stripping comparison never proves the
device. A missing or contradicting identity record refuses. Pool creation
writes an attempt record before `osd pool create`; a retry adopts the pool
only when that record exists and exactly one pool of that name exists with the
frozen parameters, and otherwise refuses `pool-identity`. A placement policy
writes `ceph-policy-attempt-v1` before `osd crush rule create-replicated`, and
adopts a same-name rule only with that record and the frozen
take/chooseleaf/emit steps; otherwise it refuses `foreign-rule`. An
erasure-coded pool proves that no profile and no rule of its name exist before
writing `ceph-pool-attempt-v2`, sets its profile without `--force`, and accepts
the stored profile only as the passed keys plus the frozen per-plugin defaults;
otherwise it refuses `foreign-profile`, `foreign-rule` or `native-output`.
An RBD image writes `ceph-rbd-image-attempt-v1` before `rbd create` and adopts
a same-name image only with that record and the frozen shape; otherwise it
refuses `foreign-rbd-image` or `rbd-image-identity`. A CephFS filesystem writes
`ceph-filesystem-attempt-v1` before `ceph fs new` and a subvolume group
`ceph-subvolume-group-attempt-v1` before `fs subvolumegroup create`; each adopts
only with its record, and otherwise refuses `foreign-filesystem`,
`filesystem-identity`, `filesystem-multiple-disabled`, `mds-coverage` or
`foreign-subvolume-group`. A CephFS subvolume writes
`ceph-subvolume-attempt-v1` before `fs subvolume create` and adopts only with
its record; otherwise it refuses `foreign-subvolume` or `subvolume-identity`.

For every matrix row, the release record names the primary vendor source and
revision/date, support status (supported, preview, deprecated or unsupported),
Bootwright input owner, native projection, prerequisites, readiness probe,
ownership/removal proof, tests and operator acceptance profiles. Supported
rows have an implementation owner below. Preview or deprecated rows receive an
explicit support decision before promotion. An unsupported row records its
source and pre-effect refusal; changing a supported row to deferred requires
an explicit scope decision. A newly discovered supported service gets a
bounded item before the release gate closes.

M2 supports every cluster size the selected native tools accept for the
deployment mode (D130). B472 records provider minima, documented maxima,
per-site and per-role counts, failure-domain rules and each feature's resource
requirements. Lab profiles do not define an allowed-size list or a product
ceiling. A provider recommendation is distinguished from a support requirement.
Feature eligibility is evaluated against the declared cluster and the
provider's requirements; an ineligible combination names the unmet requirement
and the required change before effects.

## Feature matrix

Rows describe bootstrap capabilities, not an ongoing day-2 control plane.
Replication/failover mechanisms may be configured at bootstrap and exercised
by the operator for acceptance without adding ongoing Bootwright management.
The last column names the item that owns the implementation and its evidence.

| Family | Required coverage and qualification boundary | Work |
| --- | --- | --- |
| Hosts and native bootstrap | Managed RHEL, entitled package/registry access, time and DNS, non-root orchestration SSH login with qualified privilege escalation, public/replication networks (a declared replication network is refused before any effect unless every Ceph host's Machine declares an address inside it, [refusal table](storage.md#refusal-table)), stable FSID, MON quorum and active/standby MGR, explicit IBM call-home choice, qualified config and manager modules | [B490](milestones/m2.md#b490), [B80](milestones/m2.md#b80) |
| Devices and OSDs | Explicit devices and bounded selectors, drive groups, BlueStore data/DB/WAL separation, device class, slots and allocation, encrypted OSDs and supported TPM2 variants; prove every destructive target and owned cleanup | [B475](milestones/m2.md#b475) |
| CRUSH and pools | Host/site failure domains, replicated and erasure profiles, application tags, autoscaling/PG bounds, quotas, compression, device classes where supported, RBD initialization and data-pool selection | [B81](milestones/m2.md#b81) |
| CephFS | Replicated metadata, replicated or supported EC data, multiple data pools and explicit default/layout, MDS placement and standby/replay, subvolume groups, subvolumes and service-consumer references | [B492](milestones/m2.md#b492) |
| RGW | Service placement, realm/zonegroup/zone bootstrap, pools, users/buckets/credentials needed to prove S3 access, backend ports, TLS public endpoints and HA ingress | [B477](milestones/m2.md#b477) |
| NFS | NFS-Ganesha placement, supported CephFS/RGW exports, client restrictions and squash, ingress transport and VIP failover; native release decides backend/TLS support | [B478](milestones/m2.md#b478) |
| Observability and management | Dashboard, Prometheus, Grafana, Alertmanager, node and Ceph exporters, log collection (Loki with Alloy or Promtail, each image authored under `ceph.images`) and SNMP gateway, management gateway, TLS, OAuth2/SSO and access roles, retention and health/alert checks; the prometheus manager module and `crash` are derived children with named owners (cephadm enables the module for a declared prometheus service; Bootwright's derived crash unit applies `crash` on every host) and are never authored; the direct dashboard certificate ([B682](milestones/backlog.md#b682)), local dashboard roles and users ([B683](milestones/backlog.md#b683)), the manager exporter without a Prometheus service ([B684](milestones/backlog.md#b684)) and cephadm-signed or selected certificate sources ([B685](milestones/backlog.md#b685)) are parked | [B479](milestones/m2.md#b479) |
| NVMe-oF | Gateway groups and HA, listeners, subsystem/NQN identities, namespaces backed by owned RBD images, authorized clients and supported authentication/TLS, ANA and client failover | [B480](milestones/m2.md#b480) |
| iSCSI | Refusal only: admission refuses `service_type: iscsi` before any effect as a gateway Bootwright does not realize; Bootwright records no vendor support status for it | [B481](milestones/delivered.md#x58--iscsi-refusal) |
| SMB | Supported SMB cluster/domain/authentication modes, CephFS subvolumes and shares, HA/CTDB and client tests; AD is an explicitly declared external dependency | [B482](milestones/m2.md#b482) |
| Multi-cluster services | RGW multisite and supported RBD/CephFS mirroring setup, peer identity/credentials, initial synchronization and bounded failover evidence over separate managed clusters | [B483](milestones/m2.md#b483) |
| Host and security variants | Bond/VLAN install, final NMState network, OS registration, target FIPS and CephX policy, encrypted root disks, reboot access; distinguish these from encrypted OSDs and controller certification | [B74](milestones/m1.md#b74) to [B77](milestones/m1.md#b77), [B326](milestones/m1.md#b326), [B346](milestones/m1.md#b346), [B472](milestones/m2.md#b472), [B484](milestones/m2.md#b484), [B485](milestones/m2.md#b485) |
| Disconnected supply | Qualified private mirrors/registry, all bootstrap/service images and dependencies, CA and pull-secret custody, explicit destinations and no unintended public fallback | [B493](milestones/m2.md#b493) |
| Access and handoff | Redacted cluster status and endpoints, least-privilege client material and RBD/CephFS/S3 access evidence; no Fusion installation in this milestone, the `fdf` package being M4d's [B85](milestones/m4.md#b85). Managed StorageExport material belongs to [B86](milestones/m4.md#b86), which reuses this custody and handoff boundary | [B489](milestones/m2.md#b489) |
| Physical provisioning | Verified BMC/media TLS, identity, host install/network variants, firmware refusals, firewall diagnosis and independently proved physical erase | [B78](milestones/m1.md#b78), [B70](milestones/m1.md#b70), [B485](milestones/m2.md#b485) |
| Stretch | Native stretch quorum and CRUSH, supported pool/service subset, per-site endpoints and fault behavior, monitor-only third-site tiebreaker | [B486](milestones/m2.md#b486) to [B488](milestones/m2.md#b488) |

All service rows cover create, observe, exact continuation, failed/ambiguous
effects, readiness, access custody and reverse-dependency removal. Declarative
changes to a completed cluster remain subject to the existing destroy/fresh
apply rule. HA coverage records both native daemon health and client behavior
under the declared failure, within a recorded timeout. The qualified service
mode defines whether clients continue automatically or require an explicit
recovery action; a daemon-health pass alone never proves client continuity.

## Profiles and capacity

Cluster size and role placement are parameterized within the provider's
supported range. The profiles below sample that range for acceptance; they
do not restrict supported initial cluster sizes. Coverage includes minimum
supported shapes, representative larger shapes, feature-specific thresholds
and documented upper boundaries. In-tree tests exercise boundaries and
size-independent planning; operator rows identify the sizes actually run.
B474/B79/B490 review input, request, evidence and inventory bounds, batching,
concurrency and deadlines against the supported range. A conflicting
Bootwright limit needs a bounded backlog item and qualification before M2
completion; lab capacity does not justify a narrower product limit.
I/O and concurrency stay bounded; qualified batching or pagination preserves
snapshot identity and complete inventory, with no silent truncation.

A minimal three-node virtual profile uses three MONs, redundant MGRs, one
dedicated data disk per OSD host and an explicitly declared 2/2 replicated RBD
pool, following IBM's
[three-node all-flash colocation requirements](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=recommendations-colocation).
Three replicas require at least four OSD-bearing hosts; the ordinary 3/2
default remains unchanged. It uses a single public network; it is a functional lab
profile, not a production sizing promise. MON/MGR roles may share those hosts.
Monitoring and gateways are enabled only in their later profiles. B490 reports
the aggregate RAM/vCPU/storage budget, controller reserve and host deficits
before provisioning; insufficient capacity requires an explicit smaller
declared allocation or more resources, never lower replica/quorum safety.

Full feature coverage is the union of separately declared, reproducible
profiles: core, pool variants, file/object services, gateway families,
observability/authentication, host/security variants, disconnected delivery
and multi-cluster replication. Mutually incompatible options run separately.
The full virtual suite need not fit all services simultaneously on three
guests, and a three-host lab cannot qualify EC profiles needing more distinct
failure domains. The full physical suite repeats the applicable matrix on
real hardware; emulated results do not substitute for it.

### Profile catalog

Each profile is a public example directory `libvirt-<n>-nodes-ceph-<profile>/`
under `examples/`, where `<n>` counts its Ceph hosts (`2x4` for two clusters
of four), with one README runbook. The catalog follows these rules:

- Every Ceph daemon placement follows IBM's
  [colocation page](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=recommendations-colocation)
  for the daemons it names. For a daemon the page does not name, the delivering
  family item records the placement it qualified. Whether admission enforces
  the page's list stays with [B472](milestones/m2.md#b472).
- Declared guest memory follows [OD21](milestones/m2.md#m2c-open-decisions).
  Under its recommended option each guest declares at least the per-guest sum
  of IBM's
  [minimum hardware figures](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=recommendations-minimum-hardware-considerations)
  for its daemons plus 2 GiB of operating-system headroom, plus the family
  item's recorded figure for a daemon that page does not list. Until OD21 is
  answered, both RAM totals are shown. Counts, roles, colocation, replica and
  quorum rules and CPU prerequisites are never relaxed under either option.
- An NVMe-oF gateway host keeps IBM's figures: 4 vCPUs and 8 GB per gateway in
  addition to its colocated OSD's 5 GB. Its provider host CPU is Intel 64, 6th
  generation or later, with AVX-512, exposed under host-passthrough. An AMD
  CPU with AVX-512 is not accepted until [B480](milestones/m2.md#b480) records
  IBM's position.
- Each profile reserves 4 GiB of controller memory, as the
  [three-node example](../examples/storage-clusters/libvirt-3-nodes-ceph/README.md) does.
- A profile with more than three Ceph hosts in one cluster plans only after
  [OD22](milestones/m2.md#m2c-open-decisions)'s execution-budget item.
- P0 is [B476](milestones/delivered.md#x94--post-destroy-cluster-inspection)'s profile and is shown for reference only:
  it claims no B484 coverage. P6 claims the core cells (single public network,
  one explicit data disk, explicit 2/2 replicated pool, minimal pool client);
  P5 also covers the data disk and the 2/2 pool.

The Coverage cell lists entries separated by `; `. Each entry is a Family name
of the [feature matrix](#feature-matrix), optionally followed by details in
one pair of parentheses. Until a profile's directory lands, its Example cell
reads `planned:` and names the directory. Once it lands, the cell links the
directory, and the Guests, vCPU, RAM and Root+data cells must be exact
numbers. `TestCephProfileCatalogCoversEveryVirtualMatrixRow` requires every
feature-matrix family except Physical provisioning and Stretch, and each core
cell, in some P1 to P7 Coverage cell. `TestCephProfileBudgetsMatchExamples`
holds each linked example's guests to its row: the sum of `cpu`, `memoryMiB`,
`diskGiB` and `dataDisks[].sizeGiB` of each authored substrate-realized Machine's
provider profile, including clients managed in separate contexts. The budget
counts every declared guest even when the storage root's effective closure
excludes that client. Its RAM must equal one of the two RAM cells.
`TestCephProfilesPlanWithoutRefusal` plans each linked example through the
production capabilities with no `Unsupported` refusal and one Ceph host block
per topology node.

| Profile | Example | Coverage | Ceph hosts and daemons | IBM per guest (GB) | Other guests | Networks | Lab per guest under OD21 (b) | Guests | vCPU | RAM GiB under OD21 (b) | RAM GiB under OD21 (a) | Root+data GiB | External dependencies | Delivered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| P0 core (B476 only, not counted) | [libvirt-3-nodes-ceph/](../examples/storage-clusters/libvirt-3-nodes-ceph/) | none: B476's profile, shown for reference | 3: MON 3, MGR 2, OSD 3 | 11, 11, 8 (sum 30) | none | public | 8 GiB (B476's values, fixed OSD memory target) | 3 | 12 | 24 | 24 | 180+240 | IBM entitlement | [B476](milestones/delivered.md#x94--post-destroy-cluster-inspection) |
| P1 pools (representative larger) | [libvirt-6-nodes-ceph-pools/](../examples/storage-clusters/libvirt-6-nodes-ceph-pools/) | Hosts and native bootstrap (public and cluster networks); Devices and OSDs (two drive groups, HDD data with DB on SSD, device classes, encrypted OSDs, TPM2 variant if B475 qualifies it); CRUSH and pools (replicated 3/2, EC 4+2 RBD data pool with overwrites and replicated metadata on six hosts, EC 2+2, device-class rule, quota, compression, autoscale bounds, RBD images) | 6: MON 5 on h1-h5 (3 if B472 records a narrower MON range), MGR 2 on h1 and h2, OSD 12 (2 per host) | h1, h2 16; h3-h5 13; h6 10 (sum 81) | none | public, cluster | h1, h2 18; h3-h5 15; h6 12 GiB; 4 vCPU; 60 root, 40 SSD, 40 HDD and 10 SSD (DB) GiB | 6 | 24 | 93 | 60 | 360+540 | IBM entitlement | [B81](milestones/m2.md#b81), [B475](milestones/m2.md#b475), [B326](milestones/m1.md#b326); OD22's budget item |
| P2 services | [libvirt-6-nodes-ceph-services/](../examples/storage-clusters/libvirt-6-nodes-ceph-services/) | CephFS (replicated metadata, replicated and EC 2+2 data, layouts, MDS active and standby-replay, subvolume groups and subvolumes); RGW (realm, zonegroup and zone, two RGW daemons, TLS, ingress VIP, users and buckets); NFS (multi-active count 2 over 3 candidate hosts, ingress, CephFS export, RGW user export); SMB (user mode, clustered count 2 if B482 supports it, two shares on distinct subvolumes); Access and handoff (CephFS, S3, NFS and SMB client evidence) | 6: h1, h2 MON, MGR, OSD and MDS; h3 MON, OSD and RGW; h4, h5 OSD and NFS (candidates h4-h6); h6 OSD and RGW; SMB and ingress placement as B482 and B477 qualify | h1, h2 14; h3 9; h4, h5 5+?; h6 6 (sum 53+) | 1 client (RHEL 9.8, DVD packages per family) | public (VIPs in it) | h1, h2 16; h3 11; h4, h5, h6 12 GiB; client 4 GiB; Ceph 4 vCPU, client 2; Ceph 60 root and 60 SSD GiB, client 40 root GiB | 7 | 26 | 83 | 83 | 400+360 | AD only if B482 qualifies AD mode (operator-declared) | [B492](milestones/m2.md#b492), [B477](milestones/m2.md#b477), [B478](milestones/m2.md#b478), [B482](milestones/m2.md#b482), [B489](milestones/m2.md#b489); OD22's budget item |
| P3 observability | [libvirt-4-nodes-ceph-observability/](../examples/storage-clusters/libvirt-4-nodes-ceph-observability/) | Observability and management (Prometheus, Grafana, Alertmanager, node and Ceph exporters, retention, alerts, SNMP gateway, management gateway with a keepalive-only ingress VIP, OAuth2 SSO through oauth2-proxy with roles, TLS, log collection with Loki and Alloy) | 4: h1, h2 MON, MGR and OSD; h3 MON and OSD; h4 OSD and the observability stack; management gateway and oauth2-proxy placement as B479 qualifies | h1, h2 11; h3 8; h4 5 (base sum 35; extra service allowances in runbook) | none | public (VIP in it) | h1, h2 16; h3 10; h4 20 GiB; 4 vCPU; 60 root (h4 80) and 80 data GiB | 4 | 16 | 62 | 62 | 260+320 | an OIDC IdP of a kind IBM names and an SNMP receiver, both operator-provided | [B479](milestones/m2.md#b479); OD22's budget item |
| P4 NVMe-oF | [libvirt-5-nodes-ceph-nvmeof/](../examples/storage-clusters/libvirt-5-nodes-ceph-nvmeof/) | NVMe-oF (one gateway group, 2 gateways for HA, listeners, subsystems and NQNs, namespaces on owned RBD images, host NQN authorization, DH-HMAC-CHAP only if OD13 admits it, initiator failover); iSCSI (refusal only: Bootwright does not realize the iSCSI gateway) | 5: h1, h2 MON, MGR and OSD; h3 MON and OSD; h4, h5 OSD and NVMe-oF gateway | h1, h2 11; h3 8; h4, h5 13 (sum 56) | 1 initiator client (RHEL 9.8, nvme-cli) | public | h1, h2 13; h3 10; h4, h5 15 GiB and 8 vCPU; others 4 vCPU; client 4 GiB, 2 vCPU; 60 root and 60 data GiB, client 40 root GiB | 6 | 30 | 70 | 60 | 340+300 | provider host CPU: Intel 64, 6th generation or later, AVX-512 | [B480](milestones/m2.md#b480), [B481](milestones/delivered.md#x58--iscsi-refusal); OD22's budget item |
| P5 host variants | [libvirt-3-nodes-ceph-host-variants/](../examples/storage-clusters/libvirt-3-nodes-ceph-host-variants/) | Host and security variants (active-backup bond over 2 NICs, VLAN carrying the cluster network over the bond, final NMState, RHSM registration, FIPS without OSPP, CephX key type, LUKS root with TPM2 unlock and recovery, reboot access); Devices and OSDs (one explicit data disk); CRUSH and pools (explicit 2/2 replicated pool) | 3: MON 3, MGR 2, OSD 3 | 11, 11, 8 (sum 30) | none | public (bond), cluster (VLAN on the bond) | 13, 13, 10 GiB; 4 vCPU; 60 root and 80 data GiB; 2 NICs | 3 | 12 | 36 | 24 | 180+240 | Red Hat registration credentials (operator Secret) | [B326](milestones/m1.md#b326), [B74](milestones/m1.md#b74), [B75](milestones/m1.md#b75), [B76](milestones/m1.md#b76), [B77](milestones/m1.md#b77), [B346](milestones/m1.md#b346) |
| P6 disconnected (also the B484 core) | [libvirt-3-nodes-ceph-disconnected/](../examples/storage-clusters/libvirt-3-nodes-ceph-disconnected/) | Disconnected supply (controller-local mirror registry, CA and pull-secret custody, every image the profile deploys, no public route); Hosts and native bootstrap (single public network); Devices and OSDs (one explicit data disk); CRUSH and pools (explicit 2/2 replicated pool); Access and handoff (minimal pool client) | 3: MON 3, MGR 2, OSD 3, and the auxiliary services B493 qualifies (at least the monitoring set and ingress) | 11, 11, 8 plus auxiliaries (sum 30+) | none | public with `forward: none` | 20 GiB each (2 GiB OS and provisional auxiliary headroom); 4 vCPU; 60 root and 80 data GiB | 3 | 12 | 60 | 24 | 180+240 | externally prepared mirror with all images, signed RPM closure and publisher key; local locked wheels via setup --download-only/--wheel-mirror | [B493](milestones/m2.md#b493) |
| P7 multi-cluster | [libvirt-2x4-nodes-ceph-multisite/](../examples/storage-clusters/libvirt-2x4-nodes-ceph-multisite/) | Multi-cluster services (RBD mirroring, RGW multisite unresolved, CephFS snapshot mirroring refused as a feature Bootwright does not realize, initial sync, peer unavailable, operator failover and removal) | 2 clusters of 4: h1, h2 MON, MGR, OSD and MDS; h3 MON, OSD and RGW; h4 OSD and rbd-mirror | h1, h2 14; h3 9; h4 5 plus mirror headroom (42 per cluster before headroom) | 1 client | one public network per cluster, routed through the provider host | h1, h2 16; h3 11; h4 8 GiB; client 4 GiB; Ceph 4 vCPU, client 2; Ceph 60 root and 60 data GiB, client 40 root GiB | 9 | 34 | 106 | 106 | 520+480 | none beyond the IBM entitlement | [B483](milestones/m2.md#b483), [B477](milestones/m2.md#b477), [B492](milestones/m2.md#b492), [B489](milestones/m2.md#b489); OD22's budget item |

The iSCSI family is refusal-only: [B481](milestones/delivered.md#x58--iscsi-refusal) refuses it as
a gateway Bootwright does not realize. Physical provisioning and Stretch are M2d and M2e rows outside
this catalog.

The [three-node block and file example](../examples/storage-clusters/libvirt-3-nodes-ceph-block-file/README.md)
is outside this catalog and claims no B484 row. On three OSD hosts it declares
the [B81](milestones/m2.md#b81) and [B492](milestones/m2.md#b492) resources
that fit them: a placement policy, an RBD pool placed by it, an RBD image,
replicated CephFS metadata and data pools, an `mds` service of two daemons
bound to a filesystem with one active rank, one standby and a subvolume group,
a subvolume in that group and the pool client. Its runbook follows the profile
runbook sections and records RBD and CephFS data checks with the administrator
keyring, MDS failover, interruption and continuation, replay, changed-intent
refusal and retirement with the hosts. Erasure-coded pools and the larger
CephFS shapes stay with P1 and P2.

The [three-node object example](../examples/storage-clusters/libvirt-3-nodes-ceph-object/README.md)
and the [three-node NFS example](../examples/storage-clusters/libvirt-3-nodes-ceph-nfs/README.md)
are outside this catalog and claim no B484 row; their runbooks are inputs to
P2's. The object example binds a StorageObjectGateway to an rgw service of two
daemons that terminate TLS themselves with a generated `tlsCertificate` Secret
naming the gateway's endpoint, over the eight derived zone pools. Its runbook
records an HTTPS status line from each daemon through the endpoint name with
the bound certificate as trust anchor, a failed name check through a host
address, and one RGW daemon stopped and started while the other answers. The
NFS example serves a subvolume through a direct `nfs` service and one export
whose own access is `NONE`, with a denied client address listed before the
allowed client network. Its runbook records an NFSv4 mount, write and read
from an allowed guest and a refused mount from the denied guest.

P2 authors synthetic auxiliary image coordinates for offline planning. Its
runbook requires operator-selected authenticated images of the chosen release
and records their resolved digests before live qualification. All P2 operator
runs and ingress acceptance remain unrun.

`TestIngressFixturesPlanWithSelectedImages` applies an RGW ingress on two
hosts, an NFS keepalive-only ingress and an NFS haproxy-protocol ingress
([fixtures](../cmd/bootwright/testdata/ingress/)) to these examples with
synthetic `spec.ceph.images` coordinates, asserts that every production binding
plans and that the image and cluster blocks carry exactly those images, and pins
each fixture's effective StorageCluster.
`TestIngressFixturesWithoutTheirImagesRefuseNamingTheField` applies the same
variants without the images and asserts the [refusal](storage.md#refusal-table)
of a native service whose auxiliary image is not selected. P2 keeps TLS
ingress, VIP failover, multi-active NFS and the client guest.

### Profile runbooks

Each P1 to P7 README has these `##` headings, each once and in this order.
Other headings may sit between them:

1. `## Budget and prerequisites`: the catalog row's numbers (IBM per guest and
   declared), external dependencies, the 4 GiB controller reserve, and for an
   NVMe-oF profile the provider CPU checks `grep -c avx512f /proc/cpuinfo` and
   `grep -m1 'model name' /proc/cpuinfo`.
2. `## Initial setup`
3. `## IBM entitlement`: both as the
   [three-node example](../examples/storage-clusters/libvirt-3-nodes-ceph/README.md) does.
4. `## Create the context`: `validate` with its exact files-seen and
   objects-decoded counts, `context init`, `context update`, `context use`,
   `secret generate`, `secret set` and `secret check`.
5. `## Apply by stage`: controller, infra-components and substrates, machines,
   then clusters, with the per-guest read-only checks before clusters. The memory
   sampler below runs during `apply --stage clusters`.
6. `## Validate the cluster`: `cluster info` and read-only native checks (`-s`,
   `health detail`, `orch ls`, `orch ps`, `osd tree`, `osd pool ls detail`, and the
   family's own list commands with its redaction filters), never a
   secret-bearing export.
7. `## Client access`: the family item's own client evidence, run from the
   client guest or through `cluster exec`.
8. `## Failover`: stop the declared gateway or daemon host with
   `bootwright machine stop`, then record native health and the client result
   within a stated timeout, and the declared recovery action. A profile without
   a gateway runs the one-node outage of the three-node example here.
9. `## Interruption and continuation`: one Ctrl-C of `apply --stage clusters`
   after the first Ceph block starts, `status`, the same command again, and
   the record that it continued the same operation.
10. `## Replay`: `apply` on the done context settles without effect.
11. `## Check that changed intent is refused`
12. `## Measured budget`: the commands below and every measured budget field
    of the [Ceph matrix row](../docs/acceptance.md#ceph-matrix-row), by name.
13. `## Cleanup`: `destroy --authorize data-loss` refused while hosts run,
    `machine stop` of the whole group, destroy again, and the absence checks.

Measured budget commands are operator-run and read-only. Run them before the
first apply (a), after `apply --stage clusters` (b), and after the client and
failover checks (c):

```text
nproc
grep MemAvailable /proc/meminfo
sudo du -sb --apparent-size /var/lib/libvirt/images/bootwright/<context>
sudo du -sb /var/lib/libvirt/images/bootwright/<context>
virsh --connect qemu:///system domstats --balloon --vcpu
<cephCommand> orch ps --format json-pretty
<cephCommand> df
```

The memory sampler starts in a second terminal just before
`apply --stage clusters` and stops with Ctrl-C when that command returns:

```text
while sleep 10; do date +%s; grep MemAvailable /proc/meminfo; done > "$ev/memavailable-clusters.txt"
```

`TestCephProfileRunbooksHaveRequiredSections` checks each linked P1 to P7
README for these headings and for every measured budget field name in its
`## Measured budget` section.

## Stretch constraints

IBM's [stretch-mode contract](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=storage-stretch-mode)
uses two data sites and a third tiebreaker site. The reference profile uses
IBM's recommended five-MON arrangement: two MONs per data site and one
tiebreaker. Other MON counts and placements are the native tools' to judge; the reference count is not an admission ceiling.
Data-site CRUSH weights are equal; the stretch rule places four replicas, two
per data site, with healthy `size=4,min_size=2`. Native degraded stretch may
temporarily lower `min_size` to 1; recovery must restore 2. This qualified
transition does not authorize weakening an authored profile to fit a lab.
Bootwright's arbiter profile carries no OSD, MGR, MDS, gateway, ingress or
monitoring workload. The release-specific flash, failure-domain,
network/latency and capacity prerequisites are admission and live-preflight
gates. The single-site lab policy is not reused as a stretch policy.

The same IBM page excludes EC pools and device-class rules in stretch mode.
EC coverage belongs to the non-stretch profiles. A healthy MON quorum alone
does not prove OSD write availability: acceptance records native degraded and
recovery transitions and the actual client read/write result.

[NVMe-oF in stretch](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=cluster-configuring-nvme-in-stretch)
requires at least four Ceph nodes per data site. The NVMe-oF stretch acceptance
profile meets that minimum and every other provider prerequisite; a smaller
core-stretch profile cannot claim that row. M2 includes qualifying larger
profiles for supported features that need them. Applying an undersized authored
cluster refuses the incompatible feature and names the required change;
Bootwright never silently resizes declared intent. Other gateways also need
explicit release/topology qualification.

[RGW multisite](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=gateway-multi-site-considerations)
requires at least two independent Ceph clusters and is a different profile
from one stretched cluster. Site-local VIPs require an explicitly qualified
front-door/DNS or load-balancer arrangement: a routed inter-site link does not
make a floating L2 VIP work across sites. External traffic management remains
operator-owned unless a separately specified Bootwright capability owns it.

IBM's stretch overview and [changing stretch mode](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=mode-changing-stretch)
give different reversibility guidance. B472 resolves the exact patch's native
behavior; continuation never assumes that disabling stretch is a safe inverse
of a partially completed activation. Owned-cluster removal has its own proof.

## Other native restrictions to freeze

[Native OSD specifications](https://docs.ceph.com/en/tentacle/cephadm/services/osd/#multiple-osd-specs-for-a-single-host)
allow multiple drive groups on one host. B472/B475 qualify each supported
layout through resolved device identities, non-conflicting allocations and
owned cleanup; whole-host exclusivity is not a native requirement. Dynamic
selectors also need a qualified boundary for later matching devices, because
cephadm retains their declarative intent after the initial apply.

[IBM's multi-active NFS mode](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=servers-configure-multi-active-nfs-gateway)
requires explicit placement counts; client-IP restrictions need compatible
forwarding through ingress. It warns that backend failure can leave client I/O
hung and require unmount/remount. B478 qualifies ingress and session behavior
per mode, including the required recovery action; it does not infer seamless
failover from a VIP.

Guest firewalls: when `firewalld.service` is enabled and running, upstream
cephadm opens each deployed daemon's TCP endpoint ports with
`firewall-cmd --permanent --add-port` in firewalld's default zone and then
reloads ([cephadm.py](https://github.com/ceph/ceph/blob/v20.2.1/src/cephadm/cephadm.py#L976-L983),
[firewalld.py](https://github.com/ceph/ceph/blob/v20.2.1/src/cephadm/cephadmlib/firewalld.py#L84-L119),
[serve.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/serve.py#L1371-L1383)),
and it never opens VRRP for keepalived, which has no firewalld service form
([ingress.py](https://github.com/ceph/ceph/blob/v20.2.1/src/cephadm/cephadmlib/daemons/ingress.py#L150-L160)).
Whether a managed guest runs firewalld depends on its install profile.
Bootwright owns no host firewall: an ingress refuses `ingress-firewall` before
effects when an ingress host's active firewalld would drop VRRP or the client
port ([Resources, readiness and access](storage.md#resources-readiness-and-access)).
Other families' listeners (NVMe-oF, SMB) are B484's live observation.

[Erasure-code overwrites](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=overview-erasure-coding-overwrites)
require BlueStore and explicit overwrite enablement for RBD/CephFS data, with
replicated metadata and explicit data-pool/layout selection. RGW keeps its
metadata/index pools replicated and uses EC only where the release permits.
The selected release's native tools decide which EC plugins, profiles and
workload limits they accept and report a refusal through `nativeError`.

B81 records the EC decision (D160, OD25, as D157 amends it). An
erasure-coded pool needs `k+m` OSD hosts of its device class under failure
domain host, one per chunk; admission checks the declared hosts and apply
proves the count from `ceph osd tree`. No `k`/`m` pair is otherwise
restricted, and a profile the selected release's monitor refuses is reported
through `nativeError`. The plugin defaults to `isa`; `isa` and `jerasure` with
technique `reed_sol_van` are what Bootwright realizes, and `clay`, `lrc` and
`shec` refuse as not realized. FastEC (`allow_ec_optimizations`) is not
exposed; its native default is false (upstream `global.yaml.in` at v20.2.1,
unverified for the IBM image until B472). Site and rack failure domains and CRUSH roots other than `default` are
parked (D160, OD25). The [admission contract](api/storage.md#pool-protection-and-erasure-coding)
owns the rules.

[SMB management](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=services-smb-cluster-share-management)
requires compatible subvolume bindings: NFS and SMB cannot expose the same
subvolume. SMB support is assessed by subfeature, not rejected wholesale as a
preview. The [native SMB manager](https://docs.ceph.com/en/tentacle/mgr/smb/#smb-cluster-management)
generates its cephadm services from cluster/share resources. B482 qualifies
that ownership boundary: generated services are observed, not independently
authored under a second owner.

[Dashboard OAuth2](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=dashboard-enabling-oauth2-single-sign)
requires the management gateway, OAuth2 proxy, a qualified identity provider
and user-role mapping; configuring a redirect alone is not SSO acceptance.
Bootwright realizes SSO through the management gateway unit for an identity
provider whose certificate chains to the selected image's trust store or to a
CA bundle bound through the oauth2-proxy's one `custom_configs` entry, and
enables the dashboard's OAuth2 single sign-on only after each deployed proxy's
rendered configuration proves that it verifies the identity provider's
certificate. Local dashboard roles and users stay parked
([B683](milestones/backlog.md#b683)).

Monitoring and the management gateway ([B479](milestones/m2.md#b479)) follow the
[native records](storage-native-api.md#observability-and-management-gateway-records).
Three upstream v20.2.1 mechanics shape them.
[`PrometheusService.config`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/monitoring.py#L480-L489)
enables the prometheus manager module and the monitor respawns the active
manager, so the module is the declared service's derived effect. The
[gateway](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/mgmt_gateway.py#L54-L78)
keeps the first certificate pair it stores, and a generated certificate's IP
SAN is the bare `virtual_ip`, so one gateway owns one pair, and a bound pair
travels in the first apply and is proved from the served leaf.
[certmgr](https://github.com/ceph/ceph/blob/v20.2.1/doc/cephadm/certmgr.rst)
does not renew user-provided certificates. The `cephadm.workarounds` list stays
retired: the management gateway needed no provider-defect specialization.
Acceptance rows record the tested coordinates as evidence, never as an
allowlist. See [the lessons](../.agents/knowledge/ibm-ceph-monitoring.md).

## Unsupported combinations

Each row is a combination that a primary source calls unsupported. It names
the item that owns the refusal and the diagnostic that refuses it before any
effect. The diagnostic owns its text, so this table restates no reason or
remedy. An admission refusal's code, field, message and remediation are pinned
by the row's fixture. An `Unsupported` refusal's reason and remedy are a row of
the [Ceph refusal table](storage.md#refusal-table), which the fixture names.
`TestCephMatrixCombinationFixturesRefuse` applies each `tested` row's fixture
to its base example and asserts exactly the diagnostics it lists.
`TestEveryCephMatrixFixtureHasATestedRow` refuses a fixture without a `tested`
row. A `gap` row is a combination that the code still admits. It has no
fixture, names the item that must deliver the refusal, and blocks
[B484](milestones/m2.md#b484)'s exit. The row turns `tested` when that item
refuses the combination. It is removed when the item records the combination
as supported, with a source, and a profile claims it. Every refusal names the
object, the reason and a concrete next step. A remediation that still reads
`correct <field> to satisfy this requirement` is replaced with a concrete one
before B484's exit.

| ID | Combination | Source | Owner | Owning diagnostic | Status | Fixture |
| --- | --- | --- | --- | --- | --- | --- |
| U01 | Stretch topology in a non-stretch milestone | [Stretch constraints](#stretch-constraints) (M2e) | [B486](milestones/m2.md#b486) | admission `api.invariant` on StorageCluster `$.spec.ceph.topology.stretch`, beside `api.invariant` on each StoragePool `$.spec.replicated` that the stretch default makes invalid | tested | [U01.yaml](../cmd/bootwright/testdata/matrix/U01.yaml) |
| U02 | Host-domain replicas above the declared OSD hosts | [IBM colocation](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=recommendations-colocation) | [B81](milestones/m2.md#b81) | admission `api.invariant` on StoragePool `$.spec.replicated.size` | tested | [U02.yaml](../cmd/bootwright/testdata/matrix/U02.yaml) |
| U03 | Replicated size equal to the OSD-host count on IBM | [IBM colocation](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=recommendations-colocation) ("three replicas is four nodes") | [B81](milestones/m2.md#b81) | admission `api.invariant` on StoragePool `$.spec.replicated.size` (IBM's extra-host rule) | tested | [U03.yaml](../cmd/bootwright/testdata/matrix/U03.yaml) |
| U04a | EC `k+m` above the declared OSD hosts of the pool's device class | [upstream erasure code](https://docs.ceph.com/en/tentacle/rados/operations/erasure-code/) and [upstream CRUSH device classes](https://docs.ceph.com/en/tentacle/rados/operations/crush-map/#device-classes) (one chunk per host under failure domain host; rule semantics, not a vendor statement) | [B81](milestones/m2.md#b81) | admission `api.invariant` on StoragePool `$.spec.erasure` | tested | [U04a.yaml](../cmd/bootwright/testdata/matrix/U04a.yaml) |
| U05 | EC on a pool role that does not permit it (today only `cephfs-metadata`) | [IBM EC overwrites](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=overview-erasure-coding-overwrites) | [B81](milestones/m2.md#b81) | admission `api.invariant` on StoragePool `$.spec.role` | tested | [U05.yaml](../cmd/bootwright/testdata/matrix/U05.yaml) |
| U06 | EC CephFS metadata pool | [IBM EC overwrites](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=overview-erasure-coding-overwrites) | [B81](milestones/m2.md#b81), [B492](milestones/m2.md#b492) | admission `api.invariant` on StoragePool `$.spec.type` | tested | [U06.yaml](../cmd/bootwright/testdata/matrix/U06.yaml) |
| U07 | `clusterSSH.transport: native`, or the transport omitted | [Release qualification](#release-qualification); [upstream ssh.py v20.2.1](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/ssh.py#L161) | [B79](milestones/m2.md#b79) (refusal), B595 (table) | [Ceph refusal table](storage.md#refusal-table): Incomplete or unsafe Ceph coordinates or transport | tested | [U07.yaml](../cmd/bootwright/testdata/matrix/U07.yaml) |
| U09 | Replicated size above the OSD hosts that could carry the pool rule's declared device class | [upstream CRUSH device classes](https://docs.ceph.com/en/tentacle/rados/operations/crush-map/#device-classes) (rule semantics, not an IBM statement) | [B81](milestones/m2.md#b81) | admission `api.invariant` on StoragePlacementPolicy and StoragePool `$.spec.replicated.size` | tested | [U09.yaml](../cmd/bootwright/testdata/matrix/U09.yaml) |

## Physical qualification

[B485](milestones/m2.md#b485) repeats the applicable P1 to P7 feature and
combination rows using BMC-installed hosts and real disks, on the
[B78 host foundation](managed-os.md#physical-host-qualification) with B70 erase
evidence. The [three-host physical example](../examples/storage-clusters/physical-3-nodes-ceph/README.md)
supplies the base installation and storage trajectory. Expand its authored
hosts, devices and native services to each catalog profile's required shape.

| Physical profile | Required coverage beyond the common host foundation |
| --- | --- |
| P1 | Representative larger host count, explicit WWN data/DB/WAL disks and observed serial/capacity/native identities, device classes, replicated and EC pools, OSD encryption and its TPM2 arm. |
| P2 | CephFS, RGW, NFS and SMB service placement, their clients, ingress and failure/recovery on physical networks. |
| P3 | Observability, management/authentication, TLS, alerts and logs with the declared auxiliary images and external dependencies. |
| P4 | Physical gateway CPU prerequisites, separately managed initiator, NVMe-oF access and gateway/host/network failover. |
| P5 | Bond/VLAN networking, managed registration, FIPS and CephX policy, encrypted root with TPM2/recovery and reboot access; host evidence and Ceph service evidence are both required. |
| P6 | Private registry/package trust and complete image/package closure with external egress denied. |
| P7 | Separate physical clusters, client replication, interrupted setup, peer failure, exercised failover and ordered whole-cluster retirement. The RGW multisite credential-interface blocker remains applicable. |

Each profile includes disk/NIC/host substitution refusals, interruption and exact
continuation, client data checks and B70-authorized retirement. The final
Storage retirement proof must cover all originally observed OSD/DB/WAL disks;
an early pending-erase record cannot complete storage removal or release its
machine claims. Record actual measured capacity separately from synthetic
scale-boundary tests. All physical rows remain unrun until the owner supplies
and accepts their evidence using the
[physical ledger template](../docs/acceptance.md#physical-qualification-row).

## Acceptance evidence

Each profile records its frozen build/release, redacted effective-intent and
plan digests, qualification tier, prerequisites, command sequence, per-feature
result, health/placement observations, client data checks, interruption and
exact continuation, failover/recovery where applicable, removal result and
operation-log digest. The [acceptance rules](milestones.md#completion-and-verification)
govern owner acceptance. Local machine inventories, addresses, credentials,
firmware captures and test-site mappings are not part of the public profile.

For [B484](milestones/m2.md#b484), each catalog profile P1 to P7 needs its own
owner-accepted ledger row on B484's acceptance baseline. The row uses the
[Ceph matrix row](../docs/acceptance.md#ceph-matrix-row) template; its locally
retained evidence holds the measured budget fields. P0 belongs to B476 alone.

In-tree gates qualify contracts without hosts. The operator gates perform
native execution, inject failures externally, restore the lab and record
evidence; the plan authorizes no such execution. M2 completion requires
[M2a's definition baseline](milestones/m2.md#m2a), all supported matrix rows
and [M2b to M2e's delivery gates](milestones/m2.md#submilestones), not a single
healthy cluster or only a successful bootstrap command. Acceptance records
name the relevant submilestone as well as its B/X items and profile.

The [P4 implementation fixture](../examples/storage-clusters/libvirt-5-nodes-ceph-nvmeof/README.md)
is tracked and its five-storage-host plan is tested. The aggregate budget includes its separately managed
initiator; the storage-root closure deliberately excludes an unattached client
Machine. No initiator, hardware or failover acceptance has been run.

P7 currently supplies the RBD relationship and local MDS/RGW resources only.
RGW multisite has a [native credential-interface blocker](storage.md#rgw-multisite-blocker);
full P7/B484 qualification is not claimed. Its two clusters each declare
51 GiB (16+16+11+8); the separate client adds 4 GiB. The authored total is
106 GiB under OD21 (b); no reduced-memory variant is qualified. The 4 GiB
controller reserve is additional. The client is realized in a separate
machine-only context, as the runbook describes.
