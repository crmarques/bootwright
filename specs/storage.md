# Managed storage lifecycle

This page owns the target runtime contract for managed Ceph. The
[storage API](api/storage.md) owns admission and references;
[state reconciliation](state-reconciliation.md) owns operation state,
authorization, leases, continuation and removal. [M2](milestones/m2.md)
owns delivery and qualification. An accepted storage declaration does not
claim lifecycle support.

Candidate core plans and runtime ports are implemented. Not yet met:
complete resource lifecycles, removal while hosts survive and release
qualification; tracked as
[B79](milestones/m2.md#b79), [B80](milestones/m2.md#b80),
[B81](milestones/m2.md#b81), [B472](milestones/m2.md#b472) and
[B491](milestones/m2.md#b491).

## Ownership and supported inputs

Storage owns Ceph topology, native projection, cluster identity, placement,
resources and their completion evidence. Go validates and freezes typed
provider intent, resolves the implementation, produces native inputs and
plans effects. Reconciliation orders capability blocks; Managed OS owns host
installation; Substrate owns Machine realization and physical identity;
Secrets owns confidential material. Each remote observation or effect crosses
one bounded Ansible adapter under the
[Go and Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary).
Ansible invokes qualified provider mechanisms, including cephadm, without
choosing desired intent, dependencies, authorization or operation state.

The managed lifecycle consumes Storage's native projection directly without
M7's standalone renderer or operator scripts. Native inputs are frozen data;
native orchestration deploys daemons within the authorized request, while
Bootwright owns the plan, target proof and interpretation of its result.

B472 records tested artifact/schema coordinates, the complete provider feature
matrix and new typed API fields before their consuming implementation is
promoted. Tested coordinates do not restrict the operator's
[version selection](api/storage.md). Each feature maps to
intent, projection, effects, readiness, access, continuation and removal gates.
Missing intent receives a deliberate API extension. Admission of other
distributions or external clusters does not select an M2 implementation.

The approved [native service target](storage-native-api.md) uses complete
ServiceSpec documents under a closed, release-qualified service-type allowlist.
The [resource graph](storage-resources.md) binds daemon owners and gives
imperative-only resources declarative identities. Explicit services and
native-generated children cannot duplicate ownership. Unknown service types,
incompatible fields and unsafe options refuse before registration. Native
maps do not override proved identity, versions, credentials, dependency edges
or safety policy. Arguments and custom configuration remain data, never shell
programs or Ansible templates. B474 owns the common authoring migration;
consuming items qualify their native fields and resource lifecycles.

Projection preserves authored native values unless an explicit API rule
normalizes them; it never silently drops a field or replaces it with a default.
Defaults affecting topology, durability, security or effects are frozen from
the selected artifacts or verified against the frozen expected value. Final
core services are explicit; omitted optional service families are absent and
bootstrap suppresses their automatic deployment. Unavoidable generated helpers
still have planned effects, images, dependencies and a derived owner.

## Frozen release and trust

A plan binds exact package, cephadm and tool coordinates, schema identity,
automation content and all daemon/auxiliary images by immutable digest.
Authored tags alone cannot identify execution. Resolution follows the
[supply-chain contract](security.md#cryptography-and-supply-chain); continuation
uses the frozen result and never refreshes a tag, upgrades a host implicitly or
substitutes an ambient tool. An unavailable or mismatching dependency refuses
with the required coordinate and recovery action.

Registry and repository entitlement, transport trust, cluster SSH trust and
required publisher authenticity are explicit inputs. Registry login cannot
weaken TLS verification, and a supplied digest does not substitute for required
publisher trust. FIPS, CephX, encryption and call-home choices retain their
independent API, trust and authorization requirements. Native tools determine
whether the selected versions can realize those choices; Bootwright does not
screen their version compatibility. Adapter
[version specializations](architecture.md#implementations-and-version-variation)
adapt native mechanics to capabilities read from the frozen, authenticated
artifacts; they neither screen nor approve a selection.

Bootwright acquires provider artifacts using the selected Entitlement's bound
Secret and the cluster's release/source intent. Operators import credentials;
they need not perform a separate registry login or image pre-pull. Resolution
freezes the authenticated immutable image reference before native execution,
and guest delivery uses that exact reference and original credential binding.
Ambient auth files, a cached tag and operator reports cannot replace those
proofs. The authored version and image coordinates select the artifacts;
native tools determine whether that selection can be installed and run.
Acquisition freezes the selected artifacts before their native effects, and
guest delivery uses the original binding. Bootwright does not substitute a
reviewed release or compare the selected versions for compatibility.
The [storage API](api/storage.md) owns version selection and the
[release qualification](storage-coverage.md#release-qualification) owns the
digest resolution and the authored-digest rule.

IBM's public signed RPM repository does not require registry credentials.
Red Hat media/CDN access remains a separate entitlement and source boundary.

The minimal DVD installation lends Storage its proved original-apply media
identity and publication, together with the artifact server's public TLS
certificate. Storage uses the shared package resolver for its Ceph-owned roots;
it does not acquire RHSM registration or install another domain's prerequisites.
Freeze repository metadata, the complete signed RPM closure, authenticated
package headers and the before/after RPM inventory in a separate durable
package decision before installation. Continuation uses that decision and
refuses an unrelated inventory or changed installed payload. An explicitly
accepted IBM license authorizes only the provider's required `ACCEPT_EULA=yes`
transaction environment; missing acceptance refuses before package effects.
Before provider host preparation, read-only checks prove it will neither
install packages through ambient repositories outside that frozen decision nor
enable or start a time service. Only then may the selected native host check
run. A refusal leaves no bootstrap attempt. Bootstrap retains its own
container-engine initialization; a separate prerequisite-check process cannot
initialize that invocation's state.
After the closure verifies in a scratch keyring, the host's RPM database gains
the pinned key of each signer the decision names, Red Hat's from the OS's own
key file and IBM's from the acquired one, before the transaction checks them.
Cluster removal retains shared host packages, publisher keys and the pulled
image. Actual installation and interruption qualification remains
[B79](milestones/m2.md#b79) work.

## Plan and prerequisites

The complete dependency DAG precedes effects. Storage declares dependencies
through API identities, following the
[lifecycle port contract](architecture.md#lifecycle-ports), and freezes bounded
blocks, native inputs, target identities, impacts, authorization and exclusive
resources. Readiness is a proved block postcondition, never an ordering hint.

The graph includes the following dependencies where the desired graph needs
them:

- Verified Machine and managed-OS completion, declared host networks, name and
  time services, trusted remote access, package and registry access, and
  provider-qualified container/runtime and host prerequisites precede bootstrap.
- Bootstrap identity and seed readiness precede cluster host enrollment;
  per-host preparation precedes its service and OSD effects. Enrollment and
  required MON/MGR placement precede final membership, quorum and topology
  readiness. Seed readiness alone never proves the completed cluster ready.
- OSD inventory, CRUSH topology and any stretch activation precede the placement
  policies and pools that rely on them. Placement and pool protection are
  checked against proved live capacity as well as declared capacity.
- Pools precede their filesystems; filesystem/MDS or object storage prerequisites
  precede their NFS exports; gateways precede their ingress endpoints. An export
  for a downstream consumer waits for all resources and material it describes.

Before an apply that selects managed Ceph claims an operation or binds secrets,
it observes aggregate declared libvirt CPU, memory and backing-filesystem disk
capacity, including guests outside the selected cluster. Pure planning performs
no capacity probes. Apply re-proves the capacity gate under its exclusive
transaction before registration or native effects. [Storage preflight](cli/commands.md) reports the same
capacity checks; unknown facts cannot authorize effects. The gate runs only for an apply
transition that may still start or resolve a libvirt Machine block in its
selection; a continuation whose Machine blocks are all done is not gated. The provider authors
its reserve explicitly through `libvirt.capacityReserve`; there is no inferred
vendor sizing profile or default reserve. Existing owned allocations receive
credit only after exact protected domain, process and backing-file proofs.
Foreign configured domains, including inactive domains, reserve their vCPUs.
Memory uses observed available memory plus proved owned resident memory; disk
uses actual backing-filesystem availability plus unique proved allocations.
Remote provider facts remain unknown until that observer is qualified.

Storage preflight reports future Machines whose managed OS installation has
not started as pending without remote guest probes. Installed or provided OS
guests require actual prerequisite observations before readiness is proved.

B472 fixes provider-specific ordering and forbids cycles. Storage never installs
another domain's prerequisites itself. Preflight names unmet prerequisites and
their owners; [B490](milestones/m2.md#b490) qualifies host and capacity proofs.

Topology placement binds nodes to proved Machines and declared sites. Native
cluster size and role counts follow the
[provider-qualified requirements](storage-coverage.md#release-qualification);
acceptance-profile sizes impose no additional restriction. Native
stretch mode requires the complete two-data-site topology and an independent
third-site monitor-only tiebreaker; an incomplete tiebreaker declaration retains
its [admission meaning](api/storage.md#complete-graph-invariants) and cannot
claim stretch readiness. [B486](milestones/m2.md#b486) qualifies native stretch,
[B487](milestones/m2.md#b487) its service/endpoint restrictions, and
[B488](milestones/m2.md#b488) its failure and recovery acceptance.

### Refusal table

Each row is one refusal of the Ceph capability, and
`TestCephRefusalTableMatchesUnsupported` holds its `Unsupported` to it before
operation registration and secret custody. Its reason and remedy are the
refusal's reason and remediation. The capability refuses the StorageCluster
and every StoragePlacementPolicy, StoragePool and StorageClient whose
`clusterRef` names it, each with the same row. [Unsupported combinations](storage-coverage.md#unsupported-combinations)
links each matrix combination that this table refuses.

| Refusal | Path | Reason | Remedy |
| --- | --- | --- | --- |
| Incomplete or unsafe Ceph coordinates or transport | a StorageCluster, or a StoragePlacementPolicy, StoragePool or StorageClient whose `clusterRef` names it, declaring an empty `ceph.release`, `ceph.packageVersion`, `ceph.cephadm.ansible.packageVersion` or `ceph.image.version`, an empty `ceph.image.base`, or a `ceph.cephadm.clusterSSH.transport` other than `verified-tunnel`, an omitted transport included | `the selected Ceph coordinates or cluster SSH transport are incomplete or unsafe` | `declare bounded version strings, an image repository and verified-tunnel transport; native transport is refused and host-key verification cannot be disabled` |
| A replication network that a Ceph host has no address inside | a StorageCluster, or a StoragePlacementPolicy, StoragePool or StorageClient whose `clusterRef` names it, declaring `ceph.networks.clusterCIDRs` while the Machine of a `ceph.topology.nodes[]` entry declares no IP address inside any of them, a Machine whose addresses are assigned at runtime included; `<host>` is the first such node's native hostname in declaration order, `<machine>` its Machine and `<cidrs>` the declared networks joined by a comma and a space | `Ceph host <host> (Machine <machine>) has no address inside the replication networks <cidrs>` | `add an address inside one of <cidrs> to the network of <machine>, or remove spec.ceph.networks.clusterCIDRs` |

## Bootstrap identity and continuation

A cluster receives one stable FSID UUID, durably bound to its context, owning
operation incarnation and frozen bootstrap request before the first remote
effect. Its identity never derives from the context or object name alone:
recreating a name cannot address a previous cluster. Continuation, observation
and removal use the recorded FSID and the same proved Machines. B79 fixes UUID
allocation, publication and request-binding semantics while preserving pure,
deterministic plan previews. A lost response never allocates a replacement;
unknown or contradictory identity records refuse before another bootstrap.

Before bootstrap, positive absence is proved on the exact target. Where a
cluster already exists, FSID, immutable provider identity, context ownership,
bootstrap target and relevant frozen configuration must agree. A matching name
or FSID alone does not prove ownership. An existing foreign cluster, replacement
host or ambiguous partial bootstrap refuses without adoption or cleanup.

Per-host preparation precedes bootstrap and enrollment. One block per host,
exclusive to that host and concurrent across hosts, journals four phases under
owned markers scoped by owner, FSID and host:

1. Package installation from the frozen package decision, keeping the
   inherited crypto-policy proof.
2. A pull of the frozen image digest with a protected authfile, a signature
   policy limited to the selected repository and at most three bounded attempts
   per preparation run. A pull counts only after local inspection proves the
   digest and the linux/amd64 platform. The retained attempt records serve
   crash diagnosis and recovery; they never refuse a later apply while the image
   is still absent, so that apply makes its own three attempts.
3. A runtime profile from a contained probe of the image: the ceph account,
   crypto policy, SELinux label and cephadm bytes, bound to the authenticated
   frozen image. Host RPM payloads retain their independent authenticity and
   integrity proof without a package/image compatibility comparison.
4. A route probe of the provider's native SSH class inside the image.

The [coverage page](storage-coverage.md#release-qualification) owns the probe
and route requirements.

The cluster block depends on every host block, re-proves the recorded profile
against the inspected image, and refuses a missing or contradicting one. Between
the tunnel mount and enrollment it publishes and proves the exact cephadm bytes
in the cache. A host block removes nothing; packages and the image are retained
shared host state. The owned per-FSID host runtime tree under
`/var/lib/bootwright/ceph-host/<fsid>` (profile, route, payload, device
identities, pull journals, and the ephemeral broker material a killed pull can
leave) and the `ceph-packages/<fsid>` package journal are retained too; they
hold no credential. Guest pull credentials enter only the invocation-local
registry broker over protected stdin. Podman receives an empty authfile and
an ephemeral mutual-TLS identity; upstream entitlement and bearer credentials
remain broker-owned. Authentication, token exchanges and manifest reads connect
only to the frozen registry authority with ordinary host TLS verification;
foreign token realms and manifest redirects refuse. The broker admits only
bounded reads within the frozen repository. Local clients without the invocation
certificate cannot use it, and a proxy bypass carries no entitlement or bearer
credential. The native client receives an empty signature-lookaside
configuration; trust stays the frozen digest and the signature policy.

For an admitted blob GET or HEAD, the verified registry may delegate delivery
through one redirect response (301, 302, 303, 307 or 308). The broker follows that one hop internally;
it never sends the Location to the native client. The destination must be
HTTPS on port 443, without userinfo or a fragment. Every resolved address must
be public unicast; a private, reserved, mixed, scoped, IPv4-mapped or IPv6
tunneling answer refuses the complete result. Connections use only those validated
numeric addresses and verify TLS against the original delivery hostname.
Redirect URLs, including signed queries, remain invocation-local private data.
Delivery requests contain no registry authorization, cookie, proxy credential
or Referer. A delivery authentication challenge or further redirect refuses
without another authentication exchange or hop. Delivery response metadata is
normalized and error bodies are not published, so it cannot echo a private URL
into the native client's output. The native client still verifies blobs against
the frozen manifest's digests; a redirect never substitutes an artifact.
Registry resolution follows the same single delivery hop, under the same rules,
when it reads a configuration blob.

Delivery shares the existing concurrency, byte and operation deadlines. DNS
runs in a bounded, isolated subprocess of the existing host interpreter,
receiving only the validated hostname through stdin and returning bounded
numeric addresses. Each lookup has at most five seconds plus the existing
one-second process cleanup allowance. Shutdown refuses admission after DNS and
at connection registration; already admitted connection attempts remain bounded
by the existing join deadline.
Listener, upstream sockets, resolver children and workers finish before the
private invocation directory is removed.

Each bootstrap, enrollment, service and resource attempt observes first and
defines partial-completion evidence and safe retry. Complete owned state is
unchanged; other owned differences converge only through qualified transitions.
Command success alone proves neither ownership nor readiness.

Bootstrap runs as the cluster's one longer native call, under its own launcher
and worker budget, because cephadm restarts the manager several times before it
returns; the cluster block's deadline counts that call and both readiness
waits. cephadm writes its configuration file under the caller's umask, so
capture proves both bootstrap outputs root's own single-link files closed to
writes by others and makes them private before recording their identities;
observation never changes them. A phase that restarts the manager waits,
bounded and inside its worker budget, for the MON to report an available
manager whose orchestrator answers. Orchestrator reads, and the idempotent IBM enable-call-home and accept-license declarations, repeat while the active manager restarts. Per-host enrollment
and label attempts and per-service apply attempts retain exact intent before
effects and exact completion after native readback. A lost reply settles from
the same host/service identity; drift refuses. Native ServiceSpec defaults come
from the selected image's SDK and are frozen before effects.
An orchestrator service description is not by itself a stored specification:
native discovery can describe a bootstrap daemon through an inferred unmanaged
service. Only the exact owned bootstrap MON/MGR projection may be treated as
the absence of a stored specification, after full native descriptions prove
that distinction and current bootstrap, daemon and transport proofs agree.
A stored unmanaged specification receives the ordinary ownership checks.
Every observed service is checked before a new service effect. The inferred
projection never supplies desired placement or completion: normal frozen
service intent precedes apply, and exact readback precedes completion. Unknown
description shapes, foreign specifications and completed-service drift refuse.
OSD services use deterministic per-host children with the proved virtio or
QEMU SCSI serial aliases. Immediately before submission each host proves empty devices or exact
owned partial OSD/LV UUID and membership; raw path reuse cannot widen the write
target. Read-only device observation publishes no custody records. Pool setters
retain setting intent/completion and settle from the same numeric pool ID.
An incomplete RBD init may repeat only the bounded native idempotent init of
that exact owned pool; an application flag alone proves no completed init.

Cancellation, lost response, unavailable evidence and contradictory native
state follow [unknown-effect recovery](state-reconciliation.md#converging-an-effect).
Read-only resolution proves the exact frozen apply or removal outcome before
any new effect. Unknown results never permit blind retries or force. Unsupported
changes refuse with the object, observed difference and safe next action.

A host or cluster block that refuses publishes a
[`refused` record](architecture.md#the-adapter-result-protocol) in place of
completion. Its reason is only a closed token the block's caller gives a
diagnostic and remedy; a native or package worker's refusal is named by the
token its endpoint raised, and a package helper's own failure by the package
phase it failed in: resolution, signature or transaction. Its detail names that
token and the code site that refused, or an exception type and site, never a
value. A token the block gives no remedy is published as `unknown`, or
`remote-refusal` from a native worker, with the token in the detail, so no
refusal reaches the operator without its reason. A refusal inside the package
worker whose token is outside that worker's closed set is published as
`packages`, with the original token in the detail. Selected native acquisition
and execution failures additionally carry the bounded redacted `nativeError`
projection defined by the adapter protocol, so the diagnostic retains the
requested coordinate and useful native cause without reflecting raw output.
The coordinate may end with the ids of the
[version specializations](architecture.md#implementations-and-version-variation)
applied in that run; within its bound, that suffix is truncated before the
image and package part.

A named refusal ends the attempt with its entry's code. An entry coded
`lifecycle.unknown` leaves the block's attempt outcome unknown rather than
failed, so the next apply observes that block first and resolves it from the
observed evidence, a host block through its preparation evidence. Only the
entries coded `lifecycle.unsupported` (`unsupported-release`,
`package-license`, `package-platform`, `dashboard-verify`,
`client-quiescence-unqualified`, `call-home-unqualified` and
`native-option-undeclared`) leave it failed. A censored failure that names
no refusal stays the generic adapter failure, which fails the attempt.

## Devices and destructive ownership

An OSD declaration authorizes only a finite, proved device set on its bound
Machines. Before provisioning, dynamic selectors, including `all`, resolve to
concrete device identities; the operation durably binds that set before its
first destructive effect. A restart cannot discover additional disks and add
them to the same authorization. The capability re-proves identity and ownership
immediately before use, including data, DB and WAL devices and every intended
shared-device allocation.

Live checks exclude the OS root device and its backing devices, boot media,
mounted filesystems, swap, foreign signatures, foreign OSDs and devices owned by
another operation. Path spelling and native “available” status alone are
insufficient. Aliases, multipath views, partitions, overlapping selections and
replacement devices must resolve without ambiguity; unsupported layouts or
unreadable inventory refuse before mutation. Native drivegroups cannot
authorize later-attached devices.

Provisioning records the relation between cluster FSID, OSD identity, Machine,
physical device identity and DB/WAL allocation. Existing owned OSDs are reused
only on exact proof. Erase, zap, signature clearing and destructive replacement
are separate effects with explicit impacts, authorization and proof; an OSD
selector or permission to bootstrap does not authorize them.

Physical erase remains governed by
[physical disk safety](state-reconciliation.md#physical-disk-safety) and
[B70](milestones/m1.md#b70). Storage planning cannot remove that refusal.
B79 and B80 define which device preparation can be proved within apply; any
path requiring physical erase waits for its safety contract and qualification.
[B475](milestones/m2.md#b475) qualifies OSD identity, ownership and write/erase
safety; virtual-disk ownership never establishes physical-disk safety.

## Resources, readiness and access

Each resource family has a release-qualified request and native identity,
creation/update boundary, owned-state observation, readiness predicate and
inverse. B81 covers placement and replicated/erasure-coded pools; other families
include [CephFS/MDS](milestones/m2.md#b492), [RGW](milestones/m2.md#b477),
[NFS](milestones/m2.md#b478), [observability/management](milestones/m2.md#b479),
[NVMe](milestones/m2.md#b480), [iSCSI](milestones/m2.md#b481),
[SMB](milestones/m2.md#b482), [multi-cluster/mirroring](milestones/m2.md#b483)
and [client handoff](milestones/m2.md#b489). B472 qualifies exact provider support
and typed gaps. Observation proves usability, not just object existence.
The [API ownership rules](api/storage.md) prevent one resource from implicitly
creating or deleting another resource's pools, services or credentials.

Resource evidence binds the logical object, cluster FSID, owning operation and
native immutable identity where the provider supplies one. Native-created
children have a named owner, creation boundary, dependency edges and qualified
absence/removal proof; they cannot become unrecorded side effects or acquire
ownership merely by matching a name. A service may not adopt another object's
pool or credential as its generated child. B472 and each resource item settle
native identity and child-resource gaps before promotion.

A placement policy and a pool each write their attempt record before the native
create, and record the native rule or pool ID after it. A retry adopts a
same-name rule, profile or pool only with that record and the frozen shape:
the rule's steps, the stored profile as exactly the passed keys plus the
plugin's frozen defaults, and the pool's type, rule and PG count; anything else
refuses before an effect. Neither phase ever removes a pool, profile or rule. A
monitor's PG refusal of the create (`ERANGE`) refuses `pg-budget`. When fewer
hosts under root `default` hold an OSD of the class than the replica count or
the erasure-code row requires, the policy or erasure-coded pool refuses
`crush-capacity` before its rule or profile exists. Erasure-coded overwrites
are irreversible, so they are enabled only for rbd and cephfs-data pools.
Every pool setting after the create (size and minimum size, erasure-coded
overwrites, both quotas, each declared compression option and the application
of a pool that is not a replicated rbd pool) keeps the pool setter
intent/completion records: it is sent only while its completion is absent and
its observed value differs, then read back, and a completed setting is never
sent again. A completed size or minimum size that drifted refuses
`pool-identity`; another completed setting that drifted reads as not ready. A
settled pool apply makes no native effect and reports no change. Pool
readiness compares every frozen attribute (type, rule, size, minimum size,
profile and stripe width, overwrites, bulk, autoscale mode, the PG count when
the autoscaler is off, quota, application and each owned pool option), and an
unset value must be absent; a mismatch is not ready, and only an identity
contradiction refuses.

An RBD image is created only in pools this operation created and still holds
under their recorded IDs. It writes its attempt record before `rbd create`, and
records the native image ID after it. A retry adopts a same-name image only
with that record and an image of exactly the frozen size, format, data pool and
features with no snapshot; an image without the record refuses
`foreign-rbd-image`. A declared size, data pool or feature set that differs
from the recorded one, a native image ID that differs from the recorded one,
or a recorded image that is gone refuses `rbd-image-identity`. Image readiness
compares the size, format 2, data pool and the frozen features plus the
implicit `data-pool`, and a clone is not ready; a native image that drifted
from them is not ready rather than refused, and a consumer's snapshot does not
make it unready. No phase removes, resizes, moves or re-features an image.

A CephFS filesystem is created only over pools this operation created and
still holds under their recorded IDs, and only when no filesystem of its name
exists. It writes its attempt record before the one `ceph fs new` that also
sets `max_mds`, `allow_standby_replay` and `standby_count_wanted`, records the
native filesystem ID after it, and attaches each further data pool not yet
attached. A retry adopts a same-name filesystem only with that record and the
frozen metadata and default data pools; otherwise it refuses
`foreign-filesystem`, as it does for a pool another filesystem holds. A
declared filesystem that differs from the recorded one, a native ID that
differs, or a recorded filesystem that is gone refuses `filesystem-identity`.
Before `fs new` it refuses `filesystem-multiple-disabled` when another
filesystem exists and the FSMap disables multiple filesystems, and
`mds-coverage` when the bound MDS service has fewer standby daemons than the
filesystem needs. Subvolume groups follow once every active rank is held; a
group created by someone else refuses `foreign-subvolume-group`. Readiness
compares the pools, the MDS settings, the active ranks, standby-replay
followers and standbys of the bound service, and each declared group's owner,
mode, layout and quota; a drift is not ready rather than refused. No phase
removes a filesystem or group, fails a rank, or forces an operation.

Readiness reports bounded observations of FSID, quorum, orchestrator and host
state, expected daemons, OSD membership, placement/capacity, pool protection,
filesystem/MDS state and declared service endpoints as applicable. Waits bound
time, polling, response size, cancellation and any native asynchronous work.
Timeout reports unfinished or unknown according to evidence.
B472 fixes predicates, limits and explicitly tolerated warnings/transitions;
blanket warning acceptance cannot claim readiness. The core profile requires
`HEALTH_OK`, or a health whose every check is in a closed tolerated set. That
set holds only `CHA_ERROR_SENDING_REPORT`: IBM Call Home's egress is the
operator's network, not the cluster's state. A readiness timeout names the
checks still blocking in its refusal detail. A manager daemon whose container
cephadm is restarting is not ready and is named beside those checks; an
observation never refuses it, while an action that needs the manager's mounted
bundle still does. A host's daemon layout that cephadm moves while it is
observed (a listing, directory or unit file whose identity changed between its
first read and its re-proof, an entry gone after it was listed, or a unit that
moved between its read and its container's) is observed again, at most ten
times 3 s apart within 60 s. One that never settles is not ready, never a
contradiction: the readiness wait polls again and its timeout names
`unsettled@<host>`, while an action refuses `not-ready` and the next apply
observes again. An owner, mode, link, name, FSID, content, image or unit
contradiction still refuses at once.

The core profile proves a complete placement-group count and `active+clean`
states before claiming readiness. Zero placement groups are allowed only for
initial cluster readiness before user pools; pool and client readiness require
a positive count. Native MON IDs and generated MGR IDs must map uniquely to
the declared host's short name, then agree with that daemon row's full owned
hostname and pinned image. Short-name collisions refuse before effects;
quorum alone does not replace the running MON daemon proof. OSD metadata and
daemon rows must agree on the same numeric OSD identity, owned host and exact
declared disk identity. Declared enabled IBM Call Home requires current native module
state as well as frozen consent; declared disabled Call Home is realized as
[storage API](api/storage.md) states. IBM license readiness applies only
where the selected image carries the IBM license contract. There the MON must
hold the `mgr/cephadm/<version>_license_<digest>` record for the selected build
whose image ID equals the runtime profile's image ID; otherwise readiness reports
`IBM_LICENSE_NOT_ACCEPTED` as blocking. Expected manager-cache lag and OSDs still
starting yield partial evidence, including an OSD the map holds that has not
booted and so reports no device identity; effective daemon configuration is
checked only after the selected OSD is running, up and in. Apply may repeat
these read-only observations within one monotonic deadline, with each remote
timeout reduced to the remaining budget. Malformed evidence, foreign identity,
configuration drift and failed reads stop immediately, except a read the
manager serves, such as the orchestrator inventory or a daemon's running
configuration, which is unfinished while it fails as the active manager
restarts.
Waiting never repeats bootstrap or service effects and never authorizes
adoption of an unowned partial cluster. An owned
partial bootstrap may continue only through its separately journaled recovery
protocol: exact frozen scope, original private output custody and proved native
postconditions select the next transition. An attempted effect with no proved
result is unknown; absence does not permit repeating bootstrap or a setter.
[B80](milestones/m2.md#b80) owns qualification and operator acceptance.

Status and health use domain-owned read-only inspection. They distinguish
proved completion, current readiness, degradation, unreachable targets and
unknown evidence. They neither repair the cluster nor resolve lifecycle state
implicitly. Remote probes receive only needed bound credentials through
custody. Access resolves the selected owned cluster, native tool and trust under
[the access contract](cli.md#resource-inspection-and-explicit-access). Health
inspection never mutates or reveals keys.
Unavailable commands remain unavailable until their slice delivers them.

After readiness, a dashboard phase on the bootstrap host enables the Ceph
dashboard; bootstrap keeps skipping the dashboard and the monitoring stack, so
the staged MGR port proof is unchanged. The phase enables the module with TLS,
creates Ceph's self-signed certificate only when absent, waits for publication
of the dashboard endpoint before an attempt-marked MGR redeploy, and proves
the dashboard port in every intended running MGR's refreshed deployment
metadata. If publication changes during failover, one scoped repair plan
redeploys each affected owned standby MGR after publication, failing over first
when the target is active; its per-daemon intent is written inside the transport
and cache holds just before the effect, and a target still on its recorded
container on a later apply is requested again once per apply, because a pending
redeploy coalesces.
Missing port proof never completes that phase. The phase then creates user
`admin` with a generated password. The password lives in produced
custody, reaches the native tool on standard input only and never appears in
arguments, logs, evidence or plans. A lost response recaptures from retained
custody and never regenerates. A user with no attempt marker, or a password
changed outside Bootwright, refuses as drift naming the custodied password and
is never reset. The proof is a controller-side TLS connect to the active MGR
URL, comparing the certificate SHA-256 with the stored certificate; an
unreachable port reports the dashboard unproved. Certificates, roles, SSO and
the monitoring stack remain [B479](milestones/m2.md#b479).

The client-data test is operator-run through `cluster exec` with the
bootstrap node's administrator keyring. It writes, flushes, reads and compares
at most 1 MiB on a retained 16 MiB RBD image. Teardown removes the image; the
test never deletes it by name or repeats a write whose completion is
ambiguous. Bootwright creates no test data, and read-only readiness,
preflight, status, health and unknown-effect resolution never do. Passing
native readiness does not claim the client-data acceptance gate passed; that
gate is the owner-accepted run of [B476](milestones/delivered.md#x94--post-destroy-cluster-inspection).

## Secret custody

Entitlement and registry credentials, SSH private keys, CephX keys, administrator
keyrings, TLS private keys and downstream access material follow
[Secrets](secrets.md) and the
[sensitive-material boundary](security.md#sensitive-material). Native inputs
refer to bound material; ordinary plans, evidence, logs and errors hold neither
secret bytes nor their digests. Public endpoints carry no embedded credentials.

Generated cluster access material is offered only with proved completion and
enters [produced-material custody](state-reconciliation.md#produced-material-custody)
before its block is done. Capture failure leaves an unknown outcome and safe
recapture. Retries keep identity and material without incidental rotation.
Private files use bounded protected storage and cleanup. Partial removal
preserves custody for recovery; final withdrawal follows the engine
transaction. Downstream material is scoped to declared resources and does not
install an add-on.

Produced storage material is not an authored Secret and cannot be revealed
through Secret commands under the [custody contract](secrets.md#produced-material).
The dashboard administrator password is revealed only as
[storage dashboard access](cli.md#storage-dashboard-access) defines. Client and
export access need a Storage-owned
boundary with selected cluster and resource identity, permitted material,
sensitive-output behavior and removal semantics. Its public interface remains
pending owner approval under [B489](milestones/m2.md#b489); this contract adds
no other command or implicit reveal.

## Removal and concurrency

Removal uses the frozen applied DAG and takes dependents before dependencies.
Exports, ingress and dependent services are settled before backing resources;
filesystems and pools are settled before the topology and cluster they use.
Cluster teardown cannot indiscriminately purge foreign services, pools, OSDs,
credentials or host packages. Every inverse re-proves identity, ownership,
authorization, dependency integrity and required absence, and has a qualified
partial-removal and retry contract. Data-bearing deletion explicitly consumes
its destructive authorization. A foreign dependent or inability to establish
safe removal refuses without cascading deletion.

A provider cluster-removal command supplies an effect, not the removal proof.
Its qualified wrapper must account for shared host configuration and runtime
files it can touch, and prove each owned or absent before invocation. Disk
removal re-proves the original allocation and live device/FSID/OSD identity;
it cannot broaden the target to a current inventory path. Ignored native errors
or a mixed-volume warning leave unresolved work until independent postcondition
probes establish complete cleanup.

The target removal has two phases: prove storage clients quiescent and remove
owned services/resources while administrative hosts remain reachable; then
require explicit host shutdown and prove Machine quiescence before any Machine
or disk destruction. Dependencies preserve administrative access until online
teardown completes. Neither phase authorizes powering off hosts implicitly.
Read-only unknown-effect resolution precedes either phase, and a partial
teardown keeps the bindings needed to resume.

A managed cluster whose hosts are all Bootwright-owned virtual Machines that
the same destroy removes is retired with them, with no native call: destroy
requires data-loss authorization and the operator's prior `machine stop`, and
each Machine inverse proves its target stopped and absent before deleting its
domain and disks, including every OSD disk. A surviving host, or a bare-metal
one, keeps the refusal below. Retirement is recorded as durable evidence, so
continuation never contacts the stopped hosts.

Not yet met: removal while hosts survive, including storage-native consumer
quiescence and complete owned cleanup; tracked as
[B491](milestones/m2.md#b491). The engine supports
the [online ordering](state-reconciliation.md#quiescence-before-removal), but
candidate Ceph capabilities refuse removal until their native proofs and
inverses are qualified. Native principal or pool absence alone cannot prove
withdrawal of retained confidential material or sessions. B70 owns offline
physical erase; removing services or releasing claims proves no erasure.

The context lease admits one mutator. Within its DAG, blocks declare exclusive
resources for cluster-wide orchestrator/configuration changes, host preparation,
device allocation and any shared native/package state they touch. Independent
blocks run concurrently only when those claims and dependency proofs permit it.
Per-invocation scratch is private. Reservations prevent conflicting local
contexts from claiming the same target; a native name, process-local mutex or
context lease alone cannot coordinate another controller. B79 qualifies the
supported coordination scope and refuses unproved exclusivity before effects.
