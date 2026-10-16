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

The target family remains IBM Storage Ceph 9.9.x. IBM's
[release/package mapping](https://www.ibm.com/support/pages/what-are-red-hat-and-ibm-storage-ceph-releases-and-corresponding-ceph-package-versions)
identifies this family with upstream Ceph Tentacle and lists 9.9.1.2, dated
6 September 2026. The planning references are IBM 9.9.1 documentation and
the matching Tentacle native formats. These sources establish the release
lineage, not a runnable package/image lock or a claim that any fix is the
latest supported one. IBM support and its exact packaged schemas take
precedence over upstream feature availability.

Before any Ceph implementation is promoted, B472 records one tested patch
release and its support lifecycle, host OS/architecture, kernel and container
runtime, cephadm and cephadm-ansible package coordinates and repository trust,
Ceph image digest, every service image digest, client-tool versions, native
service schemas, defaults and release-specific defects/workarounds. An
entitled registry lookup supplies actual immutable digests; this plan invents
none. A tag, documentation URL or successful schema validation alone does not
qualify a combination. IBM, OSS and Red Hat coordinates are not interchangeable.

Each build supports one reviewed, code-pinned release, which amends D134's
"latest provider release available at setup" (D143): 9.9.1.2 with discovery tag
`v9.9.1-25074`, resolved at apply to its immutable digest and frozen for the
run. No run resolves a newer release itself. A moving latest tag is not an
execution lock. The [storage API](api/storage.md) owns the admission refusal
of any other release. There is no runtime qualification registry:
acceptance is the owner-accepted ledger row of
[B476](milestones/delivered.md#x94--post-destroy-cluster-inspection); no other evidence promotes an artifact.
Native manager-to-host SSH must enforce pinned host keys, including a negative
wrong-key test through actual manager connection, reconnect and failover paths.
The [upstream Tentacle connection](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/ssh.py#L161)
disables that verification; controller-to-host verification
alone cannot qualify this independent native channel. No authored exception
can disable verification. `native` stays refused.
The approved [verified-tunnel target](storage-native-api.md#verified-native-orchestration-transport)
retains that requirement through its outer authenticated channel. Qualification
is specific to image and transport, and includes guest forwarding restrictions,
staged bootstrap, read-only MGR mounts, reconnection, restart/failover and owned
recovery/removal. Candidate image inventory and parser probes alone never
promote an artifact.

Apply resolves the pinned tag in a controller-local image block, using the one
declared licensed Entitlement and its protected registry Secret, before any
native effect. The block reads only manifests from the fixed registry origin:
the tag must resolve to an image index with exactly one linux/amd64 child
manifest matching its indexed digest and size, and a single-manifest tag is
refused. It fetches no blob, so no registry blob redirect takes part. Its
evidence is the release, base, tag, child digest and platform, and the cluster,
pool and client blocks depend on it. The result is frozen: continuation never
refreshes the tag, and an authored digest is admitted only when it equals the
child digest, never the index digest that registry tools print for the tag.
The guest re-proves the pulled image's platform and repository digest. The
operator supplies neither a Podman login nor a locally cached image.
Credential delivery and the original acquisition binding remain Secrets-owned.
Signed package acquisition and the pull belong to host preparation. RHEL
installation media are a separate prerequisite.
Not yet met: guest engine credential-destination qualification, including
Bearer realms and redirects; tracked as [B79](milestones/m2.md#b79).

Each host-preparation block derives account, code and crypto-policy facts from
the exact immutable image pulled with the original registry binding. Its
contained probes have no network, credential mounts, inherited image
environment, hooks, automatic health checks or image volumes. Public cephadm
bytes must match the independently authenticated provider RPM payload before
cache publication. Installed RPM metadata agreement alone is not publisher
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
frozen parameters, and otherwise refuses `pool-identity`.

For every matrix row, the release record names the primary vendor source and
revision/date, support status (supported, preview, deprecated or unsupported),
Bootwright input owner, native projection, prerequisites, readiness probe,
ownership/removal proof, tests and operator acceptance profiles. Supported
rows have an implementation owner below. Preview or deprecated rows receive an
explicit support decision before promotion. An unsupported row records its
source and pre-effect refusal; changing a supported row to deferred requires
an explicit scope decision. A newly discovered supported service gets a
bounded item before the release gate closes.

M2 supports every cluster size IBM supports for the qualified release and
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
| Hosts and native bootstrap | Managed RHEL, entitled package/registry access, time and DNS, non-root orchestration SSH login with qualified privilege escalation, public/replication networks, stable FSID, MON quorum and active/standby MGR, explicit IBM call-home choice, qualified config and manager modules | [B490](milestones/m2.md#b490), [B80](milestones/m2.md#b80) |
| Devices and OSDs | Explicit devices and bounded selectors, drive groups, BlueStore data/DB/WAL separation, device class, slots and allocation, encrypted OSDs and supported TPM2 variants; prove every destructive target and owned cleanup | [B475](milestones/m2.md#b475) |
| CRUSH and pools | Host/site failure domains, replicated and supported erasure profiles, application tags, autoscaling/PG bounds, quotas, compression, device classes where supported, RBD initialization and data-pool selection | [B81](milestones/m2.md#b81) |
| CephFS | Replicated metadata, replicated or supported EC data, multiple data pools and explicit default/layout, MDS placement and standby/replay, subvolume groups, subvolumes and service-consumer references | [B492](milestones/m2.md#b492) |
| RGW | Service placement, realm/zonegroup/zone bootstrap, pools, users/buckets/credentials needed to prove S3 access, backend ports, TLS public endpoints and HA ingress | [B477](milestones/m2.md#b477) |
| NFS | NFS-Ganesha placement, supported CephFS/RGW exports, client restrictions and squash, ingress transport and VIP failover; native release decides backend/TLS support | [B478](milestones/m2.md#b478) |
| Observability and management | Dashboard, Prometheus, Grafana, Alertmanager, node and Ceph exporters, supported log collection and SNMP gateway, management gateway, TLS, OAuth2/SSO and access roles, retention and health/alert checks | [B479](milestones/m2.md#b479) |
| NVMe-oF | Gateway groups and HA, listeners, subsystem/NQN identities, namespaces backed by owned RBD images, authorized clients and supported authentication/TLS, ANA and client failover | [B480](milestones/m2.md#b480) |
| iSCSI | Release support status, gateways and HA, targets/LUNs, RBD images, initiator groups, authentication and multipath; no assumption that an upstream service is IBM-supported | [B481](milestones/m2.md#b481) |
| SMB | Supported SMB cluster/domain/authentication modes, CephFS subvolumes and shares, HA/CTDB and client tests; AD is an explicitly declared external dependency | [B482](milestones/m2.md#b482) |
| Multi-cluster services | RGW multisite and supported RBD/CephFS mirroring setup, peer identity/credentials, initial synchronization and bounded failover evidence over separate managed clusters | [B483](milestones/m2.md#b483) |
| Host and security variants | Bond/VLAN install, final NMState network, OS registration, target FIPS and CephX policy, encrypted root disks, reboot access; distinguish these from encrypted OSDs and controller certification | [B74](milestones/m2.md#b74) to [B77](milestones/m2.md#b77), [B326](milestones/m2.md#b326), [B346](milestones/m2.md#b346) |
| Disconnected supply | Qualified private mirrors/registry, all bootstrap/service images and dependencies, CA and pull-secret custody, explicit destinations and no unintended public fallback | [B493](milestones/m2.md#b493) |
| Access and handoff | Redacted cluster status and endpoints, least-privilege client material, RBD/CephFS/S3 access evidence and managed StorageExport material; no Fusion installation | [B489](milestones/m2.md#b489) |
| Physical provisioning | Verified BMC/media TLS, identity, host install/network variants, firmware refusals, firewall diagnosis and independently proved physical erase | [B78](milestones/m2.md#b78), [B70](milestones/m2.md#b70), [B485](milestones/m2.md#b485) |
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
supported profile or more resources, never lower replica/quorum safety.

Full feature coverage is the union of separately declared, reproducible
profiles: core, pool variants, file/object services, gateway families,
observability/authentication, host/security variants, disconnected delivery
and multi-cluster replication. Mutually incompatible options run separately.
The full virtual suite need not fit all services simultaneously on three
guests, and a three-host lab cannot qualify EC profiles needing more distinct
failure domains. The full physical suite repeats the applicable matrix on
real hardware; emulated results do not substitute for it.

## Stretch constraints

IBM's [stretch-mode contract](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=storage-stretch-mode)
uses two data sites and a third tiebreaker site. The reference profile uses
IBM's recommended five-MON arrangement: two MONs per data site and one
tiebreaker. Other IBM-supported MON counts and placements follow the qualified
release's topology rules; the reference count is not an admission ceiling.
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

[Erasure-code overwrites](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=overview-erasure-coding-overwrites)
require BlueStore and explicit overwrite enablement for RBD/CephFS data, with
replicated metadata and explicit data-pool/layout selection. RGW keeps its
metadata/index pools replicated and uses EC only where the release permits.
The qualified patch decides supported EC plugins/profiles and workload limits;
an older support article and a newer native option are not interchangeable.

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

## Acceptance evidence

Each profile records its frozen build/release, redacted effective-intent and
plan digests, qualification tier, prerequisites, command sequence, per-feature
result, health/placement observations, client data checks, interruption and
exact continuation, failover/recovery where applicable, removal result and
operation-log digest. The [acceptance rules](milestones.md#completion-and-verification)
govern owner acceptance. Local machine inventories, addresses, credentials,
firmware captures and test-site mappings are not part of the public profile.

In-tree gates qualify contracts without hosts. The operator gates perform
native execution, inject failures externally, restore the lab and record
evidence; the plan authorizes no such execution. M2 completion requires
[M2a's definition baseline](milestones/m2.md#m2a), all supported matrix rows
and [M2b to M2e's delivery gates](milestones/m2.md#submilestones), not a single
healthy cluster or only a successful bootstrap command. Acceptance records
name the relevant submilestone as well as its B/X items and profile.
