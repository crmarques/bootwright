# Managed storage lifecycle

This page owns the target runtime contract for managed Ceph, the storage
family's one implementation, which a StorageCluster selects with `spec.type:
ceph` and `management: managed`
([implementation selection](architecture.md#implementation-selection)). The
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
ServiceSpec documents under a closed allowlist of the service types Bootwright
realizes.
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
weaken TLS verification, and a declared Entitlement `registry.trustBundleRef`
is the only TLS anchor of that registry for image resolution and guest pulls,
and a declared `ceph.packageRepositoryTrustBundleRef` is the only TLS anchor
of the authored package repository for package acquisition, and a supplied digest does not substitute for required
publisher trust. FIPS, CephX, encryption and call-home choices retain their
independent API, trust and authorization requirements. Native tools determine
whether the selected versions can realize those choices; Bootwright does not
screen their version compatibility. A FIPS Ceph request freezes its FIPS and
AES CephX policy, requires a completed FIPS installation proof for every host,
and checks each host's kernel and inherited crypto policy before cluster
claims. Ed25519 cluster SSH keys and authored AES256K CephX policy refuse
before host preparation. Synthetic route keys use ECDSA on an observed FIPS
kernel. Native auth keys must prove the declared AES type before readiness
or client-key publication; a mismatch retains ownership and reports its cause,
without changing native cipher policy. No FIPS service allowlist or vendor
qualification is inferred. Adapter
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

Auxiliary images. A native service whose daemons run an image other than the
Ceph image needs an auxiliary image, named by cephadm's
`mgr/cephadm/container_image_<name>` setting. The operator authors each as
`spec.ceph.images.<name>` of the StorageCluster, with `base` and `version`
under the rules of `ceph.image` ([admission](api/storage.md)). The names form
a closed set, and each service type needs these:

| Service type | Auxiliary images |
| --- | --- |
| `alertmanager` | `container_image_alertmanager` |
| `alloy` | `container_image_alloy` |
| `grafana` | `container_image_grafana` |
| `ingress` | `container_image_haproxy` and `container_image_keepalived`; only `container_image_keepalived` with `spec.keepalive_only` |
| `loki` | `container_image_loki` |
| `mgmt-gateway` | `container_image_nginx` |
| `node-exporter` | `container_image_node_exporter` |
| `nvmeof` | `container_image_nvmeof` |
| `oauth2-proxy` | `container_image_oauth2_proxy` |
| `prometheus` | `container_image_prometheus` |
| `promtail` | `container_image_promtail` |
| `smb` | `container_image_samba` and `container_image_samba_metrics` |
| `snmp-gateway` | `container_image_snmp_gateway` |

Each authored auxiliary image sits beneath the Entitlement registry namespace,
resolves in the image block to its own linux/amd64 manifest digest under the
Ceph image's rules, the authored-digest rule included, and is frozen in the
image request and evidence. The cluster and host blocks take frozen auxiliary
digests only from that original resolution proof. A settings block
`ceph-settings-<cluster>`, planned only when the cluster freezes an auxiliary
image, a declared service derives a manager module or a StorageSMBResource
names the cluster, is the single writer of these cluster-global settings: it
sets each frozen auxiliary digest with
`ceph config set mgr mgr/cephadm/container_image_<name> <image>` and enables
each derived manager module (`nfs` for a declared `nfs` service, `smb` when a
StorageSMBResource names the cluster) with `ceph mgr module enable`. It writes
a setting only while the monitor configuration database holds no entry for it,
accepts an entry that holds
exactly the frozen digest in the `mgr` section, and refuses `foreign-image`
before any write for any other entry of that setting (another value, section,
mask or location), never overwriting it. Every other block only reads these
settings. The settings block follows the cluster block and, like every cluster
child, retires with the cluster. Bootwright keeps no default
auxiliary image and no table of values. A declared service or
StorageSMBResource whose image is not authored, and an authored image that
nothing declared needs, refuse before operation registration (see the
[refusal table](#refusal-table)).
A native deploy that still pulls an image uses only the protected guest registry
record of the same registry authority.

IBM's public signed RPM repository does not require registry credentials.
Red Hat media/CDN access remains a separate entitlement and source boundary.

The minimal DVD installation lends Storage its proved original-apply media
identity and publication, together with the artifact server's public TLS
certificate. Storage uses the shared package resolver for its Ceph-owned roots;
it does not acquire RHSM registration or install another domain's prerequisites.
Freeze repository metadata, the complete signed RPM closure, authenticated
package headers and the before/after RPM inventory in a separate durable
package decision before installation. Acquisition reads the authored
`ceph.packageRepository`, or its source default, over HTTPS without proxy or
redirect and never from another repository; it verifies that repository
against `ceph.packageRepositoryTrustBundleRef` alone when it is set and
against the host trust store otherwise, and a connection the anchor does not
verify fails the package phase with a native error that names the repository
and the anchor, never a credential; a publisher key connection that does not
verify names the key location and its selected anchor, never credential bytes.
An authored repository requires `ceph.packageRepositoryKey` at the same HTTPS
origin (host and effective port); that key uses the repository's sole TLS
anchor and the unchanged IBM publisher fingerprint. Omitted repository and
key select the public source defaults together. Neither metadata, RPM payloads
nor keys fall back to public sources. Continuation uses that decision and
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
them; the [reference edges](state-reconciliation.md#reference-edges) catalog
lists the requirements each storage family emits:

- Verified Machine and managed-OS completion, declared host networks, name and
  time services, trusted remote access, package and registry access, and
  container/runtime and host prerequisites precede bootstrap.
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
- Every child block of a cluster (its settings block, its service-unit blocks,
  object gateways and their zone pool blocks, placement policies, pools,
  clients, RBD images, filesystems, subvolumes, NFS exports and SMB resources)
  depends on the cluster block by its block identity, never through the
  StorageCluster API identity that the cluster's image, host, settings and
  service-unit blocks share; a client still follows its pool through the
  StoragePool it names.
- A service-unit block `ceph-service-<cluster>-<type>[-<id>]` (Kind
  StorageCluster, exclusive `ceph/<cluster>`) applies the native documents of
  one unit type (for NFS, one `nfs` service with the ingress that fronts it)
  and depends on the cluster, image and settings blocks by ID plus any blocks
  its type names; its deadline is fixed per type and does not grow with the
  host count. For monitoring, every declared `prometheus`, `alertmanager`,
  `grafana`, `node-exporter`, `ceph-exporter`, `snmp-gateway`, `loki`,
  `alloy` and `promtail` service forms one `ceph-service-<cluster>-monitoring` block, whose deadline is apply
  2280 s, observe 720 s, destroy 1380 s; its singleton documents carry no
  `service_id`. The unit request carries each grafana and snmp-gateway
  credential binding only as a Secret reference (`service.bindings`), lent as
  a protected file on apply alone; the core request never carries one. The
  block lists as impacts, after its description, that Grafana grants anonymous
  viewer access unless `anonymous_access` is `false`, that monitoring
  endpoints are unauthenticated without a management gateway, and that
  cephadm enables the prometheus manager module, whose exporter listens on
  port 9283 of every MGR host, for a declared prometheus service. A cluster
  name longer than 39 characters refuses, because the block identity holds at
  most 63. The management gateway and every keepalive-only ingress whose
  `backend_service` is `mgmt-gateway` form one
  `ceph-service-<cluster>-mgmt-gateway` block: the ingress document comes
  first in native-name order, the singleton `mgmt-gateway` document carries no
  `service_id`, and both write `virtual_ip` in its canonical address form. A
  declared `oauth2-proxy` joins the block as a singleton document after
  `mgmt-gateway`; its spec carries only `allowlist_domains`, `client_id`,
  `https_address`, `oidc_issuer_url`, `provider_display_name` and
  `redirect_url`, so an authored `scope` or `email_domains` refuses at
  planning ("the native service oauth2-proxy carries spec.<member>, which
  Bootwright does not realize for this service"), and its one admitted
  `custom_configs` entry carries only its `mount_path`. The block also depends
  on `ceph-service-<cluster>-monitoring` when that block exists, and its
  deadline is apply 2940 s (4260 s with an oauth2-proxy, whose rendered
  configuration proof and single sign-on write each add one native call),
  observe 720 s, destroy 1380 s. Its bindings are Secret references
  (`service.bindings`) lent as protected files: the gateway's certificate to
  every operation and its private key to apply only, and the oauth2-proxy's
  client and cookie secrets, TLS pair and identity provider CA bundle to apply
  only. Without the gateway's TLS binding the block lists the impact "cephadm
  signs the management gateway's TLS certificate; the readiness proof reports
  its SHA-256". With an oauth2-proxy it also lists "the management gateway
  signs users in through oauth2-proxy and its identity provider, and
  Bootwright enables the Ceph dashboard's OAuth2 single sign-on", and without
  the proxy's TLS pair "cephadm signs oauth2-proxy's TLS certificate". Its
  evidence may carry an `endpoint` member whose lowercase SHA-256 is present
  exactly when the endpoint answered, and readiness needs that answer; every
  other unit's evidence carries no endpoint. Every ceph-core evidence carries
  an `sso` member, `""` or `"oauth2"`; only a management gateway unit with an
  oauth2-proxy may report `"oauth2"`, and its readiness needs it. A cluster name longer than 37
  characters refuses. Before a fresh apply registers, the bound certificate
  must name the gateway's `virtual_ip` (an IP address entry) or, without one,
  every host of its placement (a DNS name or a matching wildcard), or the
  apply refuses naming the Secret; no certificate byte enters the refusal.
  For RBD mirroring, a declared `rbd-mirror` service forms one
  `ceph-service-<cluster>-rbd-mirror` block whose one singleton document
  carries no `service_id` or `spec`; its daemons run the Ceph image, so it
  needs no auxiliary image, and its deadline is apply 2280 s, observe 720 s,
  destroy 1380 s. A cluster name longer than 39 characters refuses.
  Every StorageCluster plans one derived `ceph-service-<cluster>-crash` block,
  whether or not it declares a service: its one singleton `crash` document
  carries no `service_id`, `spec` or `networks` and places every cluster host
  in name order; it runs the Ceph image, needs no auxiliary image, depends on
  the cluster and image blocks (and the settings block when one is planned),
  and its deadline is apply 2280 s, observe 720 s, destroy 1380 s. A cluster
  name longer than 44 characters refuses. It is ready when every placed host
  runs one crash daemon on the frozen Ceph image. It retires with its cluster;
  removing it alone is [B491](milestones/m2.md#b491)'s.
- A gateway block `ceph-gateway-<gateway>` (Kind StorageObjectGateway,
  exclusive `ceph/<cluster>`) executes its gateway's realm, zone group, zone,
  zone placement and rgw service on the bootstrap host. It depends on the
  cluster and image blocks, on each of its derived zone pool blocks by ID and,
  when an ingress fronts its rgw service, on the settings block, and on each
  declared placement StoragePool through that pool; its deadline
  is fixed (apply 4260 s, observe 720 s, destroy 1380 s) and does not grow with
  the host count.
- An object user block `ceph-object-user-<user>` (Kind StorageObjectUser,
  exclusive `ceph/<cluster>`) realizes one StorageObjectUser on the bootstrap
  host. It depends on the cluster and image blocks by ID and on its
  StorageObjectGateway through that object; its deadline is fixed (apply
  2280 s, observe 1380 s, destroy 1380 s) and does not grow with the host
  count.
- An object bucket block `ceph-object-bucket-<bucket>` (Kind
  StorageObjectBucket, exclusive `ceph/<cluster>`) realizes one
  StorageObjectBucket on the bootstrap host. It depends on the cluster and
  image blocks by ID and on its owner StorageObjectUser through that object;
  its deadline is fixed (apply 2280 s, observe 1380 s, destroy 1380 s) and
  does not grow with the host count.
- An NFS export block `ceph-nfs-export-<export>` (Kind StorageNFSExport,
  exclusive `ceph/<cluster>`) realizes one StorageNFSExport on the bootstrap
  host. It depends on the cluster and image blocks and on its NFS service-unit
  block `ceph-service-<cluster>-nfs-<serviceID>` by ID, and on its backing
  StorageFilesystem, StorageSubvolume or StorageObjectUser through that
  object; its deadline is
  fixed (apply 2280 s, observe 720 s, destroy 1380 s) and does not grow with
  the host count.
- An SMB block `ceph-smb-<object>` (Kind StorageSMBResource, implementation
  `storage-ceph-smb-v1`, exclusive `ceph/<cluster>`) realizes one
  StorageSMBResource. It depends on the cluster, image and settings blocks by
  ID; a cluster requires its `resourceRefs` members, a share its SMB cluster
  and its StorageFilesystem or StorageSubvolume, so auth resources precede
  their cluster and a share follows its cluster and backing. Its deadline is
  fixed (apply 2040 s, a share 3600 s; observe 720 s, a share 1380 s).

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

Storage accepts any selected time source that answers
([time service readiness](infrastructure-services.md#managed-network-services))
and requires no proof of its upstream synchronization; Ceph's own `MON_CLOCK_SKEW` health check, which
the readiness set below does not tolerate, reports MON clocks that disagree.

B472 fixes provider-specific ordering and forbids cycles. Storage never installs
another domain's prerequisites itself. Preflight names unmet prerequisites and
their owners; [B490](milestones/m2.md#b490) qualifies host and capacity proofs.

Topology placement binds nodes to proved Machines that carry the `ceph-node`
capability ([admission](api/storage.md#complete-graph-invariants)) and to
declared sites. Native cluster size and role counts are the native tools' to
judge ([release qualification](storage-coverage.md#release-qualification));
acceptance-profile sizes impose no additional restriction. Native stretch mode
requires the complete two-data-site topology and an independent
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
| Incomplete or unsafe Ceph coordinates or transport | a StorageCluster, or a StoragePlacementPolicy, StoragePool or StorageClient whose `clusterRef` names it, declaring an empty `ceph.release`, `ceph.packageVersion`, `ceph.cephadm.ansible.packageVersion` or `ceph.image.version`, an empty `ceph.image.base`, or a `ceph.cephadm.clusterSSH.transport` other than `verified-tunnel` in an effective state that admission did not refuse (an omitted transport normalizes to `verified-tunnel`, and `validate` refuses `native`) | `the selected Ceph coordinates or cluster SSH transport are incomplete or unsafe` | `declare bounded version strings, an image repository and verified-tunnel transport; native transport is refused and host-key verification cannot be disabled` |
| A replication network that a Ceph host has no address inside | a StorageCluster, or a StoragePlacementPolicy, StoragePool or StorageClient whose `clusterRef` names it, declaring `ceph.networks.clusterCIDRs` while the Machine of a `ceph.topology.nodes[]` entry declares no IP address inside any of them, a Machine whose addresses are assigned at runtime included; `<host>` is the first such node's native hostname in declaration order, `<machine>` its Machine and `<cidrs>` the declared networks joined by a comma and a space | `Ceph host <host> (Machine <machine>) has no address inside the replication networks <cidrs>` | `add an address inside one of <cidrs> to the network of <machine>, or remove spec.ceph.networks.clusterCIDRs` |
| An ingress that resolves to no declared service | a StorageCluster, or a StoragePlacementPolicy, StoragePool or StorageClient whose `clusterRef` names it, declaring in `ceph.services` an `ingress` whose `spec.backend_service` resolves to no declared service; `<service>` is the first such ingress's qualified name in name order | `Bootwright does not deploy the native service <service> yet` | `remove <service> from spec.ceph.services, together with its spec.ceph.serviceBindings entries` |
| A native service whose auxiliary image is not selected | a StorageCluster, or a StoragePlacementPolicy, StoragePool or StorageClient whose `clusterRef` names it, declaring in `ceph.services` a service whose type needs an auxiliary image (an `alertmanager`, `alloy`, `grafana`, `ingress`, `loki`, `mgmt-gateway`, `node-exporter`, `nvmeof`, `oauth2-proxy`, `prometheus`, `promtail`, `smb` or `snmp-gateway` service) that `ceph.images` does not set, and that the previous row does not refuse; `<service>` is the first such service's qualified name in name order, `<family>` its `service_type`, `<name>` the first image it needs in name order that `ceph.images` does not set (only `keepalived` for an `ingress` with `spec.keepalive_only`) and `<cluster>` the StorageCluster | `the <family> service <service> needs cephadm's container_image_<name>, and spec.ceph.images.<name> is not set; Bootwright keeps no default` | `set spec.ceph.images.<name> of StorageCluster <cluster> to an image beneath its Entitlement registry, or remove <service> from its spec.ceph.services, together with its spec.ceph.serviceBindings entries` |
| StorageSMBResource objects whose samba images are not selected | a StorageCluster, or a StoragePlacementPolicy, StoragePool or StorageClient whose `clusterRef` names it, that a StorageSMBResource of any resource type names in its `clusterRef` while `ceph.images` does not set both `samba` and `samba_metrics`, and that neither of the two previous rows refuses; `<smb>` is the first such StorageSMBResource in name order, `<name>` is `samba` unless `ceph.images` sets it, else `samba_metrics`, and `<cluster>` the StorageCluster | `StorageSMBResource <smb> needs cephadm's container_image_<name>, and spec.ceph.images.<name> is not set; Bootwright keeps no default` | `set spec.ceph.images.<name> of StorageCluster <cluster> to an image beneath its Entitlement registry, or remove every StorageSMBResource whose spec.clusterRef is <cluster> from the selected Environment` |
| An auxiliary image that no declared service needs | a StorageCluster, or a StoragePlacementPolicy, StoragePool or StorageClient whose `clusterRef` names it, setting `ceph.images.<name>` for an image that no service in `ceph.services` and no StorageSMBResource naming it needs (per the auxiliary image table, with `samba` and `samba_metrics` for a StorageSMBResource) and that none of the three previous rows refuses; `<name>` is the first such name in name order and `<cluster>` the StorageCluster | `spec.ceph.images.<name> is set, but no declared service or StorageSMBResource of StorageCluster <cluster> needs cephadm's container_image_<name>` | `remove spec.ceph.images.<name> from StorageCluster <cluster>` |
| More than six Ceph hosts | a StorageCluster or a StoragePlacementPolicy, StoragePool or StorageClient naming it, whose topology declares more than six nodes | `StorageCluster <cluster> declares <hosts> Ceph hosts, and Bootwright's cluster execution budget covers at most six hosts per cluster` | `declare at most six entries in spec.ceph.topology.nodes of StorageCluster <cluster>; B79 tracks larger clusters` |

The [`clusterSSH.transport` field rule](api/storage.md) states what omission
selects and that `validate` refuses `native`, so the transport part of this
row holds only as a guard behind admission.

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
2. A pull of the frozen image digest and of each frozen auxiliary digest, each
   through its own invocation-local broker admitting only that repository and
   digest, with a protected authfile, a signature policy limited to that
   repository and at most three bounded attempts per preparation run. A pull
   counts only after local inspection proves the digest and the linux/amd64
   platform. The retained attempt records serve crash diagnosis and recovery;
   they never refuse a later apply while the image is still absent, so that
   apply makes its own three attempts. Bootstrap then skips cephadm's own pull
   (`--skip-pull`) and runs the image this phase proved, so no guest pull
   reaches the registry outside the broker.
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
only to the frozen registry authority. Every upstream connection, the delivery
hop included, verifies TLS against the Entitlement's `registry.trustBundleRef`
alone when it is set (no host trust store, default path or environment
override), and against the host trust store otherwise; one anchor serves the
Ceph image and every auxiliary image. A connection that does not verify fails
the pull, and its native error names the registry authority and the anchor,
never a credential, token or delivery location;
foreign token realms and manifest redirects refuse. The broker admits only
bounded reads within its one frozen repository; one broker serves each member
of the frozen image set, and a reference outside the set refuses before any
connection. Local clients without the invocation
certificate cannot use it, and a proxy bypass carries no entitlement or bearer
credential. The native client receives an empty signature-lookaside
configuration; trust stays the frozen digest and the signature policy.

For an admitted blob GET or HEAD, the verified registry may delegate delivery
through one redirect response (301, 302, 303, 307 or 308). The broker follows that one hop internally;
it never sends the Location to the native client. The destination must be
HTTPS on port 443, without userinfo or a fragment. Every resolved address must
be public unicast; a private, reserved, mixed, scoped, IPv4-mapped or IPv6
tunneling answer refuses the complete result. Connections use only those validated
numeric addresses and verify TLS against the original delivery hostname with
the same anchor.
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
defines partial-completion evidence and safe retry, except two states that
refuse until their recovery is defined: a cluster that is not ready after a
non-bootstrap host carries a daemon refuses `partial-cluster`
([B659](milestones/m2.md#b659)), and a nonempty or partial DB/WAL device
refuses before submission ([B662](milestones/m2.md#b662)). The
[interruption table](#cluster-execution-budget) states their exits. Complete
owned state is unchanged; other owned differences converge only through
qualified transitions.
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
and label attempts and per-service apply attempts, including each document of
a service-unit block, retain exact intent before effects and exact completion
after native readback. A lost reply settles from
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
retain setting intent/completion, settle from the same numeric pool ID and
refuse `pool-drift` without writing when a completed setting
[drifted](state-reconciliation.md#comparing-desired-and-real-state).
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
value; an SSH crypto-policy refusal's detail is instead the host name and the
key type, both public. A token the block gives no remedy is published as `unknown`, or
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
image and package part. The same ids appear in the block's evidence: the Ceph
core and ceph-host evidence carry an optional `specializations` member naming,
sorted, the specializations the native phases of the execution that wrote it
applied, absent when none applied. The member is informational: no effect,
ownership, readiness or identity decision reads it, and it enters no intent or
digest.

A named refusal ends the attempt with its entry's code. An entry coded
`lifecycle.unknown` leaves the block's attempt outcome unknown rather than
failed, so the next apply observes that block first and resolves it from the
observed evidence, a host block through its preparation evidence. Only the
entries coded `lifecycle.unsupported` (`unsupported-release`,
`package-license`, `package-platform`, `dashboard-verify`,
`client-quiescence-unqualified`, `call-home-unqualified`,
`native-option-undeclared`, `ssh-policy-host-excluded`,
`ssh-policy-unavailable` and `tunnel-host-key-excluded`) leave it failed. A censored failure that names
no refusal stays the generic adapter failure, which fails the attempt.

### Cluster execution budget

The StorageCluster's cluster block, the verified-tunnel apply that bootstraps,
enrolls and deploys the cluster, covers every host count in
`spec.ceph.topology.nodes` that the selected native tools accept (D130), within
the deadline bound below. Fewer than three hosts keep their existing refusal.
Today more than six refuse before operation registration and secret custody,
as a row of the [refusal table](#refusal-table) for the StorageCluster and every
StoragePlacementPolicy, StoragePool and StorageClient naming it, with reason
`StorageCluster <cluster> declares <hosts> Ceph hosts, and Bootwright's cluster execution budget covers at most six hosts per cluster`
and remedy
`declare at most six entries in spec.ceph.topology.nodes of StorageCluster <cluster>; B79 tracks larger clusters`.

Not yet met: clusters of more than six hosts refuse; tracked as
[B79](milestones/m2.md#b79).

The cluster block's deadline is at least the sum of the ceilings of the longest
call sequence its run can make: every 660 s native call, the 1860 s bootstrap
call and both 900 s readiness waits, plus 60 s. The run ceiling therefore never
ends a run that every per-call bound admits, and in particular never after the
first services call, the point from which an interrupted apply refuses
`partial-cluster`. The worst case is computed from the frozen request alone:
its hosts, its manager placement, its OSD services and their hosts, and its
mon, mgr, osd and mds services. No request member is added for it.

The per-host calls of one step run as one round: prerequisites, devices, tunnel
preflight, claim, authorization, cache on the hosts other than the bootstrap
host, devices after enrollment, host device identities, and OSD preparation.
Every host's call of a round is in flight together, each with its own private
scratch directory; the transport proves its SSH crypto policy and known-hosts
preflight once, before the first round. A round ends when every call of it has
returned. A refusal in any call ends the run after its round with the first
refusal in host order, and no later round starts. The bootstrap host's cache
call stays a round of its own before the other hosts' cache round. OSD
preparation is one round over every host that carries an OSD service, because
its intent record covers every device of the host. Each call keeps its own
records exactly as the serial order does, so an interruption leaves the same
per-host stages.

Enrollment and an OSD service's per-host children are the two phases whose
native work grows with the host count inside one call. An OSD service submits
its per-host children to cephadm as one multi-document `ceph orch apply -i`
after writing every child's attempt record, then reads them back once and
writes the completion records; its call then issues one readback, one apply and
one readback whatever the host count, as a single-host service does, and leaves
daemon creation to the readiness wait. Enrollment cannot show the same bound:
its fixed proofs include a manager wait that may run to its 480 s horizon,
leaving 160 s of the 640 s worker budget, less than one 60 s command per host
document at six hosts plus a readback. Each enrollment call therefore admits
hosts in host order only while its remaining worker budget covers 60 s per
admitted host document plus the batch's readback. It writes the admitted hosts'
attempt records, submits their documents in one multi-document
`ceph orch apply -i` whose ceiling is 60 s per document, reads them back once
and writes their completion records. This post-apply readback makes one 60 s
command; a failed readback continues through the next apply, without repeating
a proved enrollment. The controller calls enrollment again
while hosts remain, at most once per host. A call that finds at least one
request host not yet enrolled, and whose remaining worker budget admits none of
them, refuses before any host attempt or native effect with the named token
`enrollment-budget`, coded `lifecycle.unknown`;
the bootstrap host stays `enrolling` and every other host `prepared`, which the
next apply continues. A call that finds every request host enrolled admits
none and completes after readback; on a
continuing apply that call is its first enrollment call, so it does not raise
the fresh apply's count of n enrollment calls. A separate license call then
re-proves every host's enrollment completion before checking or accepting the
provider license. Its native records and manager retries have their own 640 s
worker budget and 660 s transport bound: their variable command count cannot
be reserved as a fixed tail of a host enrollment batch. The services phase
never stops between its first daemon-deploying
apply and readiness except on a native failure.

The cluster block's apply deadline is
(10 rounds + 5 single calls + n enrollment calls + 1 services call) × 660 s +
1860 s + 2 × 900 s + 60 s, where n is the host count. The ten rounds are the
nine above with the bootstrap host's cache round; the five single calls are
the bootstrap state, tunnel configuration, tunnel mount, license and dashboard
calls. The services call validates every mon, mgr, osd and mds specification
and its original attempt, submits the pending documents together through the
native multi-document apply, and proves their readback. A lost response retains
each service's attempt identity; continuation never adopts a foreign service.
Physical hosts may therefore select their distinct WWN devices in separate
OSD specifications without multiplying bounded remote calls.
Three hosts need 19 calls and 16260 s; six hosts need 22 calls and 18240 s,
including P1's physical shape with twelve host-specific OSD specifications.
The six-hour adapter ceiling, per-call budgets, input bounds and readiness
proofs remain unchanged. A request whose deadline exceeds the ceiling refuses at
planning, coded `lifecycle.unsupported`, with reason
`StorageCluster <cluster> needs a cluster run deadline of <deadline>, past the 6h0m0s every adapter run is held to`
and remedy
`declare fewer hosts in spec.ceph.topology.nodes of StorageCluster <cluster>`.

Both readiness waits stay 900 s. A readiness timeout after the services call
is recoverable: once cephadm has converged, the next apply observes a ready
cluster, every host's stage is complete, and the apply passes. The six-host
live runs of the [virtual provider matrix](milestones/m2.md#b484) record the
measured OSD convergence time; a shortfall there is its own item.

| An apply that stops | The next apply |
| --- | --- |
| before the bootstrap call | continues each host from its `unclaimed`, `claimed` or `prepared` stage |
| inside bootstrap, tunnel configuration or tunnel mount | continues from the seed stages of the [verified transport](storage-native-api.md#verified-native-orchestration-transport) |
| inside enrollment, devices, host device identities, OSD preparation or the services calls, before any non-bootstrap host carries a daemon | continues from `enrolling`, every other host `prepared` |
| after a non-bootstrap host carries a daemon, the cluster not ready | refuses `partial-cluster` until [B659](milestones/m2.md#b659) defines its recovery |
| in a readiness wait | continues once cephadm has converged, as above |

Neither `partial-cluster` nor the partial DB/WAL refusal of
[devices](#devices-and-destructive-ownership) has a forward exit until its item
defines recovery. On Bootwright-owned virtual hosts, `machine stop` and a
destroy that retires the cluster with its hosts
([removal](#removal-and-concurrency)), then a fresh apply, leave either
refusal. On physical hosts the only retirement is the erase-backed
[physical retirement](#devices-and-destructive-ownership), which needs each
Machine's original erase policy to cover its root disk and every selected
data, DB and WAL disk. Without that policy an interrupted physical deployment
has no forward or retirement exit today: only the orphan-acknowledged
[context deletion](contexts.md#permanent-deletion) remains, and it abandons
the hosts, their daemons, the claimed disks and their data.

The budget changes no `partial-cluster` refusal. Observation, destruction,
child blocks, host blocks and service units keep their deadlines; at six hosts
the ones that grow with the hosts count 9 calls to observe the cluster, 8 to
destroy it and 15 to apply a block other than the cluster.

The host bound and concurrent rounds are implemented under
[B688](milestones/m2.md#b688); its live qualification remains outstanding.

## Devices and destructive ownership

An OSD declaration authorizes only a finite, proved device set on its bound
Machines. Before provisioning, dynamic selectors, including `all`, resolve to
concrete device identities; the operation durably binds that set before its
first destructive effect. A restart cannot discover additional disks and add
them to the same authorization. The capability re-proves identity and ownership
immediately before use, including data, DB and WAL devices and every intended
shared-device allocation.

The bounded core profile accepts explicit `data_devices.paths`, optional
`db_devices.paths` and `wal_devices.paths`, `encrypted` and `tpm2`, with one
OSD per data device. All selected paths are distinct within a drive group and
across services on a host; dynamic selectors, slots and sizes remain
unsupported. TPM2 requires encryption and a declared TPM profile. Omitted
options retain native BlueStore defaults. Any nonempty or partial DB/WAL
device refuses before submission; [B662](milestones/m2.md#b662) owns
recovery. Read-only host readiness separately proves completed data/DB/WAL
allocations by OSD identity and LV UUID relationships. Encryption requires
matching native `ceph.encrypted` and `ceph.with_tpm` tags and an active LUKS
mapper for the exact LV; TPM2 also requires a `systemd-tpm2` token in the
non-key `cryptsetup luksDump` text metadata. A submitted ServiceSpec alone
does not establish readiness. Encryption keys remain in Ceph's store and
are never read or exported by Bootwright.

P1's HDD-data/SSD-DB prerequisite is bounded to declared libvirt `media: hdd`
and `media: ssd` disks. The frozen request binds rotational identity as well
as path, serial and size; the original Machine proof and host runtime must
agree. This proves virtual presentation, with no physical qualification claim.

Managed physical hosts use the same native explicit path selectors, restricted
here to `/dev/disk/by-id/wwn-0x` plus 16 or 32 lowercase hexadecimal digits.
At most three local whole disks per host are admitted; shared WWNs across hosts,
partitions and multipath targets refuse. The request records WWN intent without
inventing a serial or capacity. Original physical Machine presence evidence
supplies the system UUID and complete MAC inventory; installed-host SSH proof
remains mandatory. Before device binding, the host re-proves those hardware
facts from bounded kernel inventory. An unreadable or changed inventory refuses.
Bond slaves use the kernel's saved pre-enslavement address only when reciprocal
master/slave links and the complete physical bond membership agree. Missing
permanent-address metadata refuses; no current-address fallback replaces it.
Virtual bond and VLAN interfaces do not add hardware identities. The bounded
reader is shared with the offline erase worker. See the
[Linux bond-slave attribute](https://github.com/torvalds/linux/blob/v6.12/drivers/net/bonding/bond_sysfs_slave.c)
and [its original-address capture](https://github.com/torvalds/linux/blob/v6.12/drivers/net/bonding/bond_main.c).

Host preparation records each disk's observed serial, size, rotational status,
kernel path and native image SDK identity. It compares lsblk and udev WWNs and
device numbers, rejects read-only/removable disks and ambiguous aliases, and
re-proves the same facts on read-only host readiness. A replay cannot extend
or replace the bound disk set. See the
[physical example](../examples/storage-clusters/physical-3-nodes-ceph/README.md).
This code path has synthetic coverage; live physical acceptance remains unrun.

Physical retirement requires the original opt-in logical-overwrite policy to
cover the root disk and every selected data, DB and WAL WWN. Ceph contributes
the retirement policy Managed OS installation and erasure honor through
[their retirement port](managed-os.md#physical-removal): each physical node's
Machine waits for its cluster's retirement verification, named as the
StorageCluster in the storage physical-retirement family, and the verification
waits for each node's substrate claim the same way. Without it, a
realized cluster's removal refuses before registration. Storage first records a pending-erasure disposition
so consumer removal and managed-OS cleanup can precede the offline erase worker.
A separate terminal verification block then matches every complete original-policy
erase receipt to the original machine UUID, complete MAC set and observed Ceph
disk WWN, serial and capacity. Only that terminal proof permits claim release.
A stopped host, a released claim, or a pending disposition proves no erasure.

When the original core cluster block is positively unstarted, the terminal
verification may use its frozen WWN intent without an observed OSD binding;
the original sealed dependency scope must prove that unstarted state. Missing
evidence cannot substitute for it. Every started core block requires its
completed original host binding. A partial or lost erase receipt leaves the
removal incomplete and resumes through B70's original receipt protocol.

If both the original core cluster and every physical OS installation are
positively unstarted, terminal verification may instead prove retaining cleanup
without installation. The original installation owner must validate the frozen
erase-policy history, including an unstarted policy or its completed retaining
cleanup. This releases unused claims and records `retainedWithoutInstall`,
without claiming that any disk was erased. Unknown or missing history refuses.

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
[B70](milestones/m1.md#b70). Storage planning cannot authorize erase implicitly.
B79 and B80 define which device preparation can be proved within apply; any
path requiring physical erase remains subject to that contract and qualification.
[B475](milestones/m2.md#b475) qualifies OSD identity, ownership and write/erase
safety; virtual-disk ownership never establishes physical-disk safety.

## Resources, readiness and access

Each resource family has a closed request and native identity,
creation/update boundary, owned-state observation, readiness predicate and
inverse. B81 covers placement and replicated/erasure-coded pools; other families
include [CephFS/MDS](milestones/m2.md#b492), [RGW](milestones/m2.md#b477),
[NFS](milestones/m2.md#b478), [observability/management](milestones/m2.md#b479),
[NVMe](milestones/m2.md#b480),
[SMB](milestones/m2.md#b482), [multi-cluster/mirroring](milestones/m2.md#b483)
and [client handoff](milestones/m2.md#b489). The iSCSI gateway is not a family: admission refuses
`service_type: iscsi` ([B481](milestones/delivered.md#x58--iscsi-refusal)). B472 records the tested
native behavior and typed gaps. Observation proves usability, not just object existence.
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
refuses before an effect. This adoption, like each adoption on an attempt
record and a frozen shape below, trusts the one writer that the exclusive
`ceph/<cluster>` reservation admits; a writer outside Bootwright on a managed
cluster is outside that guarantee
([mutation safety](state-reconciliation.md#mutation-safety)). Neither phase
ever removes a pool, profile or rule. A monitor's PG refusal of the create (`ERANGE`) refuses `pg-budget`. When fewer
hosts under root `default` hold an OSD of the class than the replica count or
the erasure-code row requires, the policy or erasure-coded pool refuses
`crush-capacity` before its rule or profile exists. Erasure-coded overwrites
are irreversible, so they are enabled only for rbd and cephfs-data pools.
Every pool setting after the create (size and minimum size, erasure-coded
overwrites, both quotas, each declared compression option and the application
of a pool that is not a replicated rbd pool) keeps the pool setter
intent/completion records: it is sent only while its completion is absent and
its observed value differs, then read back, and a completed setting is never
sent again. A completed setting whose live value differs, or a completed RBD
init whose pool no longer carries the rbd application, refuses `pool-drift`
before any write: Bootwright never rewrites it, and the operator restores the
value outside Bootwright, or destroys and applies again. A setting or init
record that names another numeric pool ID refuses `pool-identity`. A
settled pool apply makes no native effect and reports no change. Pool
readiness compares every frozen attribute (type, rule, size, minimum size,
profile and stripe width, overwrites, bulk, autoscale mode, the PG count when
the autoscaler is off, quota, application and each owned pool option), and an
unset value must be absent; observation reports a mismatch as not ready and
refuses only an identity contradiction.

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

A CephFS subvolume is created only after the worker proves the filesystem,
the layout pool and the named group this operation created, under their
recorded IDs and marks, and proves with `fs subvolume ls` that no subvolume of
its name exists in that filesystem and group. It writes its attempt record
before `fs subvolume create` with every attribute explicit, creates once more
when only the mode differs because a fresh create applies the manager's umask,
records the reported path, and sets the owner metadata `bootwright-owner` to
the operation ID. A retry adopts a same-name subvolume only with that record
and the frozen shape; otherwise it refuses `foreign-subvolume`. A declared
subvolume that differs from the recorded one, a recorded path that differs or
is gone, a created shape that differs, or owner metadata naming another
operation refuses `subvolume-identity`. Readiness compares the recorded path,
type, state, layout pool, namespace, quota, owner, mode and owner metadata; a
drift is not ready rather than refused. No phase removes, resizes, snapshots,
clones or re-creates a recorded subvolume; it retires with its cluster's
hosts, and [B491](milestones/m2.md#b491) owns its online removal.

A declared `nfs` service, with the `ingress` that fronts it when one is
declared, is realized by its service-unit block
`ceph-service-<cluster>-nfs-<id>`, which depends on the cluster, image and
settings blocks and runs its apply phases only on the bootstrap host; with an
ingress, the firewall check below first runs on each ingress host. Its apply
phase proves,
before any write: the cluster's ownership record and FSID; the `nfs` manager
module the settings block enabled (`nfs-module-disabled` when it is off; the
unit never enables a module); the shared `.nfs` pool, which may exist only
under the claim this operation's NFS units recorded and, once recorded, only
under its recorded ID (`nfs-pool-foreign`; it is never adopted, rewritten or
deleted); and, with an ingress, the frozen `container_image_keepalived`
setting and, unless the ingress is keepalive-only, `container_image_haproxy`
(`foreign-image` names a setting the settings block no longer holds; the unit
never writes one). It then retains the unit's normalized inputs record under
its own name, claims `.nfs` before cephadm creates it, writes each missing
document's attempt record and applies every missing document in one
`ceph orch apply -i -` stream, the ingress first so the virtual IP exists for
the NFS daemons. A readback of every stored specification precedes the
completion records; a stored specification without this operation's attempt,
or one that differs from a completed document, refuses `foreign-service`.
Last it waits, bounded by 120 s, for cephadm's asynchronous creation of `.nfs`
with its `nfs` application and records the pool's native ID. Readiness needs
every document completed, each daemon type's count running on its frozen image
(the Ceph image for `nfs`, the frozen auxiliary image for `haproxy` and
`keepalived`), `.nfs` with application `nfs` under its recorded ID, the unit's
ID in `nfs cluster ls` and, with an ingress, a TCP connect from the bootstrap
host to the virtual IP at the ingress `frontend_port`, or at the NFS `port`
when keepalive-only; a readiness timeout names `<daemon type>@<service>` and
`vip@ingress.nfs.<id>` beside the blocking health checks. Observation never
writes, and a stored specification, password or keyring never reaches
evidence, which lists only each daemon's host, name and service. The block's
deadline is fixed for its type (apply 2940 s, observe 720 s, destroy 1380 s).

The declared `prometheus`, `alertmanager`, `grafana`, `node-exporter`,
`ceph-exporter`, `snmp-gateway`, `loki`, `alloy` and `promtail` services are
realized by the one
service-unit block `ceph-service-<cluster>-monitoring`, whose apply phase runs
only on the bootstrap host. Before any write it proves the cluster's ownership
record and FSID and reads, never writes, the frozen `container_image_*`
setting of every auxiliary image its daemons run (`foreign-image` names one the
settings block no longer holds); ceph-exporter runs the Ceph image. The grafana
`initial_admin_password` and snmp-gateway `credentials` leaves that its
bindings name are normalized as fixed placeholders, so the normalization
input, the unit's inputs record and every attempt and completion record hold
placeholders only. The bound values, lent from the runner's private material
to that apply phase alone, are spliced in memory into the one
`ceph orch apply -i -` stream and never reach arguments, records, evidence or
refusals. The credential comparison covers only a service whose attempt record
this block holds; any other stored specification of a unit service refuses
`foreign-service` whatever credential it stores. For a covered service, a
stored specification whose bound leaves equal the bound values, or which omits
them all, is the applied document; a stored leaf of another value, an empty
one, or some leaves without the others refuses `service-credential-foreign`,
because Bootwright never replaces a credential of a deployed service. A lost reply continues with the same material and never
sends a stored document again. Readiness needs every document completed, each
type's daemon count running on its frozen image (the Ceph image for
ceph-exporter) and, with a prometheus service, an available MGR and a
non-empty `prometheus` endpoint in `ceph mgr services`: cephadm enables the
prometheus manager module itself for that service, and Bootwright never
enables a module. The stored specifications and `ceph orch ps` are read as one
manager-served observation, so the MGR restart that enable causes leaves the
observation unfinished rather than refused. A readiness timeout names
`<type>@<service>` and `module@prometheus` beside the blocking health checks.
Observation never writes, and no credential or stored specification reaches
evidence, which lists only each daemon's host, name and service. The block's
deadline is fixed for its type (apply 2280 s, observe 720 s, destroy 1380 s).

When `alloy` or `promtail` is declared, the monitoring unit's frozen `config`
contains exactly `global.log_to_file: "true"` and
`global.mon_cluster_log_to_file: "true"`. No agent means no derived config.
These options have one owner: authored values in `ceph.config` refuse. Before
any unit write, an existing native entry without this operation's configuration
attempt refuses `config-drift`, even when its value agrees. The attempt freezes
the configuration and recovery scope before native writes. Each absent option
is set once and read back; a lost reply continues from the same attempt without
repeating a proved write. Completion is retained only after both options agree.
Another section, mask, location, value, duplicate entry or completed option
that disappeared refuses without a repair write. Observation checks those
records and values without mutation. The options retire with the owned cluster;
removing an agent changes frozen intent and requires whole-cluster retirement
under the existing lifecycle contract, rather than an online configuration
transition.

A managed [StorageExport](storage-resources.md#storageexport) is realized by
its export block, which mints the consumer's own principals and produces its
external-cluster details in custody; it installs nothing on the consuming
cluster.

The management gateway unit `ceph-service-<cluster>-mgmt-gateway` runs its
apply phase only on the bootstrap host, after the firewall check below on each
host of its keepalive-only ingress when it declares a `virtual_ip`. Before any
write it proves the cluster's ownership record and FSID and reads, never
writes, the frozen `container_image_nginx` setting and, with its ingress,
`container_image_keepalived` (`foreign-image` names one the settings block no
longer holds). The gateway's bound `ssl_cert` and `ssl_key` are placeholders in
the normalization input, the inputs record and every attempt and completion
record; the leaves are spliced in memory into the one `ceph orch apply -i -`
stream, the ingress document first, so the gateway document is never applied
without its bound leaves. The private key is lent to that apply phase alone,
the certificate also to every observation and removal. Stored leaves follow
the monitoring credential rule above (`service-credential-foreign`), and
without a binding cephadm signs the certificate. Readiness needs both
documents completed, the `mgmt-gateway` and `keepalived` daemon counts running
on their frozen images, and an endpoint proof from the bootstrap host: the
virtual IP at the gateway `port` (TLS name the address, the `Host` header
bracketing an IPv6 one), else every running gateway daemon at that port with
TLS name its host. Each endpoint must serve, with a binding, a leaf whose
SHA-256 equals the bound certificate's, and then an HTTP status line.
Evidence's `endpoint` holds that SHA-256 once every endpoint passed, or without
a binding the SHA-256 of the cephadm-signed leaf the first endpoint served, and
the coordinator refuses `evidence` for any other digest of a bound unit. A
readiness timeout names `mgmt-gateway@mgmt-gateway`, `keepalived@ingress.<id>`,
`tls@mgmt-gateway` and `http@mgmt-gateway` beside the blocking health checks.
Without an oauth2-proxy, Bootwright writes no dashboard setting and runs no
`dashboard` or `mgr module` command for the gateway; cephadm sets the
dashboard's standby behaviour itself, and the cluster's dashboard phase keeps
its own publication and readback.

With an oauth2-proxy, the unit also reads `container_image_oauth2_proxy`, and
the proxy's five bound leaves (`client_secret`, `cookie_secret`, `ssl_cert`,
`ssl_key` and `/custom_configs/0/content`) are lent to its apply phase only
and spliced, like the gateway's, into the one apply stream after the
`mgmt-gateway` document. Their placeholders pass cephadm's spec validation,
so the `cookie_secret` one holds 32 bytes; a bound `cookie_secret` that
validation refuses, neither 16, 24 nor 32 bytes after URL-safe base64
decoding when it decodes, else as text, refuses `material` before any effect.
The identity provider's CA bundle reaches the
container only through that document's `custom_configs` entry, which cephadm
mounts at `/etc/pki/tls/certs/oauth2-proxy-idp-ca.pem`; no plan, record,
argument, evidence or diagnostic carries a bundle or secret byte or a hash of
one. After the apply, and before any dashboard write, each host of the proxy's
placement proves read-only, within one native call of the block's deadline
(calls of at most 120 s, rounds 10 s apart, a 600 s window), that every
running oauth2-proxy container of this cluster there skips no identity
provider certificate verification: neither its arguments
(`--ssl-insecure-skip-verify`) nor its environment
(`OAUTH2_PROXY_SSL_INSECURE_SKIP_VERIFY`) nor the configuration file its
`--config` argument names, read through its mount beneath
`/var/lib/ceph/<fsid>/` without following a link, sets the skip (a line
naming the key other than as its bare assignment leaves the daemon unproved),
and, with a bound bundle, its CA mount holds exactly that bundle. The deployed
container's rendered configuration is the postcondition, never one release's
template. A skip refuses `sso-unverified`, naming the host, the daemon and
whether the argument, environment or configuration skips; this is the unit's
one refusal after its apply's effect, and the dashboard's single sign-on is
never enabled through an unproved proxy. Until the placed daemon count runs
proved, the window's exhaustion refuses `not-ready`. Then the bootstrap host
runs the unit's one dashboard write, `ceph dashboard sso enable oauth2`, only
when the dashboard module option `mgr/dashboard/sso_oauth2` is not already
true and once `ceph orch ps --daemon_type oauth2-proxy` lists a running
daemon, which the enable requires; until then it writes nothing and asks
again 10 s apart, each call begun within the proof's window, whose
exhaustion refuses `not-ready`. Then it reads the option back. cephadm's only
dashboard single sign-on write disables it when it removes the last running
oauth2-proxy daemon, so readiness turns not-ready until the next apply enables
it again. Readiness also needs the `oauth2-proxy` daemon count on its frozen image and
that option true, which it reads and never writes (`dashboard sso status` is
never run, because it answers EPERM without python3-saml); evidence's `sso`
is then `"oauth2"`. A readiness timeout also names `oauth2-proxy@oauth2-proxy`
and `sso@oauth2-proxy`. Destroy leaves the dashboard's single sign-on setting
to whole-cluster retirement; per-resource removal is
[B491](milestones/m2.md#b491)'s.

A StorageObjectUser is realized by its user block `ceph-object-user-<user>`
on the bootstrap host, in one `object_user` phase. Before any write it proves
the cluster's ownership record and FSID, the gateway's own records with its
realm, zone group and zone still listed under their recorded IDs
(`object-realm-foreign`), the gateway's committed period (`not-ready`) and
`radosgw-admin` itself against the digest the gateway recorded (`artifact`).
It runs `radosgw-admin` only with `--rgw-realm`, `--rgw-zonegroup` and
`--rgw-zone` of its gateway. A user ID that `user list` reports without this
operation's attempt record, or a user this operation created that is gone,
refuses `object-user-foreign` before the user's record is written. The phase
then records the user's intent and attempt and runs
`user create --uid <user> --display-name <name>` once, with no key argument,
so Ceph generates the S3 key pair. The pair crosses only that command's
output, which stays in the worker's memory: a failed `radosgw-admin` user
command reports its standard error only. The pair `user info` reports is
retained on the bootstrap host
([native-side retention](secrets.md#native-side-retention)) before the phase
answers; a lost reply is recovered under
the attempt record from `user info`, and a retained pair is never read again
from Ceph, regenerated or rotated. Readiness and every later phase prove with
`user info` that the user has the frozen display name, is not suspended, has
no subuser, Swift key, capability, system, admin or account role, and holds
exactly one active S3 key equal to the retained pair; anything else refuses
`object-user-identity`. The phase answers the retained pair as the block's
produced material `object-user`, and an observation that proves the user
ready recaptures it read-only with `object_user_read`. Evidence carries the
user ID only, never a key. The block never modifies, suspends or removes a
user, and adds no key, subuser or capability; it retires with the cluster's
hosts, and [B491](milestones/m2.md#b491) owns its online removal. S3 requests
with the pair are the operator's acceptance.

A StorageObjectBucket is realized by its bucket block
`ceph-object-bucket-<bucket>` on the bootstrap host, in one `object_bucket`
phase. Before any write it proves what the user block proves of the gateway
(`object-realm-foreign`, `not-ready`, `artifact`), that its owner's completion
record names this operation, gateway and zone (`ownership`), and reads the
owner's one active S3 key pair with `user info` into the worker's memory
(`object-user-identity` otherwise). A bucket that `bucket list` reports without
this operation's attempt record, or one this operation created that is gone,
refuses `object-bucket-foreign` before the bucket's record is written. The
phase then records the bucket's intent and attempt and sends S3 CreateBucket,
signed with AWS Signature Version 4 under the owner's pair, to the first
running daemon of the gateway's rgw service, in daemon name order, whose
connection succeeds at its frozen frontend port. A request moves to the next
daemon only when the connection itself fails (the connect, the TLS handshake or
the certificate pin) before any byte of it is sent; once any byte was sent, a
read timeout, a closed connection or a status of 500 or more refuses
`object-bucket-s3`, the request goes to no other daemon, and the next apply
recovers from `bucket stats`. When that service terminates TLS, the block holds
the bound certificate only (never its key) and sends a request only to a daemon
whose served leaf has the bound leaf's SHA-256, with SNI set to the endpoint
FQDN; every other daemon is sent nothing. A plain-HTTP rgw backend sits behind
an ingress that terminates TLS, so its signed requests cross the cluster
network in clear: the access key ID and the signed, idempotent CreateBucket and
PutBucketPolicy are visible and replayable within RGW's clock-skew window,
and the secret key never crosses the wire. `bucket stats` then proves the bucket
under its name with no tenant, owned by its owner and in the gateway's zone
group: a bucket owned by anyone else that this operation never proved refuses
`object-bucket-foreign`, and the bucket ID is recorded. A lost reply is
recovered under the attempt record from `bucket stats`, never by a second
create. A declared policy is admitted only as a mapping with a `Version` string
and a non-empty `Statement` ([field rule](storage-resources.md#storageobjectbucket-field-record)),
and the worker's request validator refuses any other text, a null or a
non-integer number, and a text that is not the planner's canonical JSON.
With a declared policy, the phase reads the stored one with
GetBucketPolicy and, before the bucket's completion record, puts the declared
canonical JSON with PutBucketPolicy when they differ. RGW's error answer to any
S3 request refuses `object-bucket-s3`, naming the request, HTTP status, error
code and message, never a key; no answering daemon is `not-ready` with
`http@<service>` or `tls@<service>`. Readiness and every later phase prove with
`bucket stats` the owner, the recorded bucket ID and the zone group, and with
the owner's GetBucketPolicy that the stored policy is exactly the declared one,
or absent when none is declared; that answer also proves the bucket usable by
its owner, because RGW refuses a suspended bucket's owner `UserSuspended`.
Another owner, ID, zone group or policy refuses `object-bucket-identity`, and
the block never removes or replaces a policy it did not put. Evidence carries
the bucket name only. The block never deletes a bucket or its objects; it
retires with the cluster's hosts, and [B491](milestones/m2.md#b491) owns its
online removal.

A StorageNFSExport is realized by its export block `ceph-nfs-export-<export>`
on the bootstrap host. A subvolume binding binds the path the subvolume's
applied creation proof reports and never constructs one. Before any write the
apply proves the cluster's ownership record and FSID, the `nfs` manager module
(`nfs-module-disabled`), the NFS service in `nfs cluster ls`
(`nfs-export-apply`), for a subvolume binding that `fs subvolume getpath`
still reports the proved path (`subvolume-identity`), that an authored `path`
lies within that path and that `fsal.cmount_path` is `/`, the export path or a
directory above it (`nfs-export-path-escape`), and with `nfs export info` that
no export holds the pseudo path without this operation's attempt record
(`nfs-export-foreign`; the NFS manager upserts by pseudo path). It then writes
the attempt record, which freezes the export JSON with its resolved path, and
applies that JSON with `ceph nfs export apply <service> -i -`, never with an
export ID, cluster ID or CephX principal. A failed apply, a warning, an answer
other than `added` for a new export or `updated` for its own recorded one, or a
readback that differs refuses `nfs-export-apply`. A
[lost reply](state-reconciliation.md#mutation-safety) is adopted only
under the attempt record and only when the stored export equals the frozen
JSON. The generated export ID and the CephFS principal the NFS manager derives
are recorded once; an export that is gone or holds another ID refuses
`nfs-export-foreign`. Each readback drops `cephx_key`, `access_key_id` and
`secret_access_key` on the host before any comparison, so none reaches a
record, refusal or evidence. Readiness compares the stored export, apart from
its export ID, cluster ID and principal, with the frozen JSON; a drifted export
is not ready and the next apply re-applies it. Evidence carries the export ID
and pseudo path only. The block never removes an export, creates or removes an
NFS service, or changes a client key; it retires with the cluster's hosts, and
[B491](milestones/m2.md#b491) owns its online removal. Mounting from a client
is the operator's acceptance.

An `objectUserRef` binding freezes `fsal: {name: RGW, user_id: <user>}`, path
`/` and its gateway's realm, zone group and zone. Before any write the apply
also proves, as the user block does, the gateway's records, its committed
period and recorded `radosgw-admin`; the user's completion record of this
operation (`ownership`); the user's one active S3 key in `user info`
(`object-user-identity`); for an existing export, that its stored key pair
equals the user's (`nfs-export-foreign`); and with `radosgw-admin realm list`
that the default realm is unset or the gateway's realm
(`nfs-export-realm-default`). The NFS manager and NFS-Ganesha find the user
only through the default realm and its default zone, so the block then runs
`radosgw-admin zone default` for the gateway's zone, the realm's own pointer,
and, when no default realm is set, `radosgw-admin realm default` for the
gateway's realm, and proves both with `realm list` and `zone list`; the
[field record](storage-resources.md#storagenfsexport) states this effect
restriction. On add the export JSON carries no key, and the NFS manager reads
the user's first key itself; an update must carry the stored pair, so the
worker reads it from `user info` into its memory, adds it to the apply's stdin
only and redacts each key from any refusal. Readiness also proves the stored
key pair, the gateway's names and both defaults; a moved default is not ready
(`default@realm.<realm>`) and the next apply sets it again, unless another
realm became the default.

A StorageSMBResource is realized by its SMB block `ceph-smb-<object>`. Its
request freezes the admitted native document with only two changes, the
resolved placement of an SMB cluster and the `cephfs` leaves a share's
StorageFilesystem or StorageSubvolume reference supplies; the bound passwords
only as Secret references, lent as protected files on apply alone; the full
Machine set of its cluster; for an SMB cluster or share, the SMB cluster's
service name, hosts and effective clustering, the declared SMB cluster IDs of
the cluster and the samba image references. Execution takes the owner, FSID,
bootstrap host, cluster scope and the samba image digests only from the core
cluster and image proofs of the original apply. Evidence carries its flags,
owner, FSID, the native resource name and, for a share, the generated daemons'
host, name and service only. The block retires with every Machine of its
cluster and refuses removal while any survives
([B491](milestones/m2.md#b491)). The block runs its native calls on the
bootstrap host. Before any effect it proves the cluster's ownership, that the
`smb` manager module is enabled (`smb-module-disabled`), and for an SMB cluster
or share that every SMB cluster `ceph smb cluster ls` lists and every `smb`
service `ceph orch ls` lists is declared by a StorageSMBResource of the cluster
(`foreign-service`) and that the settings block holds both samba image settings
at the frozen digests (`foreign-image`); it never writes them. It records its
attempt, then runs `ceph smb apply -i - --password-filter=none
--password-filter-out=hidden` with the one resource as JSON on stdin and the
bound passwords spliced in memory, and reads every resource with
`ceph smb show <name> --password-filter=hidden`; no password reaches an
argument, a record, evidence or a diagnostic. An answer that is not one
successful result for the resource refuses `smb-result` without quoting the
manager, whose answer can echo its input. A resource without this operation's
attempt record, or one this operation applied that is gone or changed, refuses
`smb-identity`; Bootwright never adopts an SMB resource. The remedies of
these refusals name only corrections outside Bootwright followed by an apply
with the same input, or a destroy before a changed StorageSMBResource, because
a failed apply continues only with the input it froze
([state machine](state-reconciliation.md#state-machine)). The readback compares
every admitted leaf after the manager's defaults (a share's name defaults to
its ID, `readonly` false, `browseable` true, `restrict_access` false,
`cephfs.path` `/`, `cephfs.provider` `samba-vfs`, an omitted `subvolumegroup`
or `subvolume` empty, a source's `source_type` `resource`, a login entry's
`category` `user` and `access` `read`), requires each leaf admission refuses to
be unset and ignores leaves Bootwright does not author. An SMB cluster, join
auth or users-and-groups resource is ready once it reads back equal; the SMB
manager generates the `smb.<cluster_id>` service only when the cluster has a
share, so a share is ready only when, in addition, that service runs its
daemons, as many as the placement count or else one per placement host, on the
SMB cluster's hosts with the frozen samba image, and each `public_addrs`
address of a clustered SMB cluster accepts TCP port 445 from the bootstrap
host. The apply proof reads no daemon, since the daemons follow the effect;
while it waits for a share's daemons, and when it observes a share, a daemon of
its `smb.<cluster_id>` service on a host outside the frozen hosts, or a daemon
of an undeclared `smb` service, refuses `foreign-service`. A share not ready
within 900 seconds refuses `smb-directory` for an
active-directory cluster, whose daemons join the directory before they start,
and `smb-unavailable` otherwise. The core request lists each declared SMB
cluster's `smb.<cluster_id>` service with its hosts among the services other
blocks execute, so core observation tolerates exactly those daemons.

A StorageObjectGateway's realm, zone group, zone, zone placement and rgw service
are realized by its gateway block `ceph-gateway-<gateway>`, which runs its
three phases only on the bootstrap host, after the firewall check below when an
ingress fronts its rgw service. Every `radosgw-admin` call that reaches the
configuration store creates the shared `.rgw.root` pool when it is absent, so
`gateway_zone` first proves the cluster's ownership record and FSID and
`radosgw-admin` itself (the verified `ceph-common` package and the hash its
record holds, `artifact` when it changes), then proves `.rgw.root` absent or
under the claim this operation's gateways share through monitor commands only,
and writes that claim before any `radosgw-admin` call; while `.rgw.root` is
absent no `radosgw-admin` call runs before every name proof passes. A realm,
zone group or zone of the gateway's names without its creation record, a
recorded one that is gone or now names another ID or realm, or a `.rgw.root`
without the claim refuses `object-realm-foreign` before any create and before
the gateway's own record is written, so renaming a refused name applies; the
refusal sends a recorded one that is gone or changed to `bootwright destroy`.
A name is adopted only under the attempt record written before its create. The phase creates the realm, then the zone group and zone with
`--master` and never with `--default`, records each native ID, and adds the
zone to its zone group once when a lost response left it outside.
`gateway_placement` binds the declared pools to the zone's `default-placement`
with one `zone placement modify` naming only the differing pools, refuses
`object-zone-pools` when the zone names a pool outside the gateway's derived
and declared set or its placement still differs, records the placement, and
only then commits the period with `period update --commit`; a derived pool the
zone does not name is accepted. `gateway_service` runs only after that commit
and applies the rgw document in one `ceph orch apply -i -` stream, with the
same inputs, attempt, readback and completion records as a service unit.
Readiness needs the committed period holding the zone, every record, the
placement count of `rgw` daemons running on the frozen Ceph image and an HTTP
status line from each daemon's frontend port, probed from the bootstrap host; a
readiness timeout names `rgw@rgw.<id>` and `http@rgw.<id>`. S3 requests are the
operator's acceptance. Observation never writes and never calls `radosgw-admin`
while `.rgw.root` is absent; evidence carries the zone ID, the endpoint FQDN,
each daemon's host, name and service, and an `endpoint` member holding whether
the terminating service answered and, with TLS, the SHA-256 of the leaf it
served, never zone keys, a stored specification, TLS material or user data. The block never reads or creates an object user
(cephadm's own `dashboard` user is tolerated), and the block retires only with
the cluster's hosts.

The gateway request carries the TLS bindings of its rgw service or of the
ingress that fronts it as Secret references only, and the ingress document
after the rgw document. Before an apply registers, the bound certificate must
name the endpoint FQDN in its subject alternative names (a DNS name or a
matching wildcard), or the apply refuses naming the Secret. The certificate is
bound to every operation of the block and the private key to apply only; the
worker normalizes the documents with placeholder leaves, splices the leaves
into the one `ceph orch apply -i -` stream in memory, and keeps them out of
every record, argument and result. A stored service whose leaves differ from
the bound Secret's refuses `object-endpoint-certificate`; one whose exported
specification omits them is accepted. With an ingress, the frozen
`container_image_haproxy` and `container_image_keepalived` settings must hold
before any record is written (`foreign-image`), haproxy and keepalived run on
those images, and the endpoint is the virtual IP at `frontend_port`. Endpoint
readiness opens TLS with SNI equal to the endpoint FQDN, requires the served
leaf's SHA-256 to equal the bound certificate's and an HTTP status line; a
timeout names `tls@`, `vip@`, `haproxy@` or `keepalived@` the service. An
ingress plans once its auxiliary images are authored
([refusal table](#refusal-table)).

Before the first effect of every apply of an NFS service unit, of the
management gateway unit with its ingress, or of a gateway whose request
carries an ingress, a read-only check runs on each host of the ingress's
placement. When `firewalld.service` is active there, the check finds
the interfaces cephadm puts the virtual IP and VRRP on: those whose network
contains the virtual IP, else those on a network listed in
`virtual_interface_networks`. It reads each one's runtime firewalld zone, or the
default zone for an interface bound to none, and refuses `ingress-firewall`,
naming the host, the interface, the zone and what is missing, unless that zone
permits both VRRP (IP protocol 112) and the client port: the ingress
`frontend_port`, or the NFS `port` when the ingress is keepalive-only, or the
management gateway's `port` for its keepalive-only ingress. A zone
permits them through target `ACCEPT`, a listed protocol (`vrrp` or `112`) or
TCP port or port range, a service without a destination that grants them
(followed through its includes), or an unrestricted rich rule of the form
`rule [family="ipv4"|"ipv6"] protocol value="vrrp" accept` or
`rule [family=...] port port="<port>" protocol="tcp" accept`; rich rules of
other forms, source-bound zones, policies and direct rules are not evaluated,
so a permit given only that way refuses. An inactive or failed
`firewalld.service` passes, and an unsettled one refuses `ingress-firewall`
naming its state; a host with no such interface passes, because cephadm reports
that itself. cephadm opens a deployed daemon's TCP ports only in the default
zone, after this check, and never opens VRRP, so the check reads the current
state and assumes neither. It never changes a firewall, and it runs again on
every apply because a firewall changed later drops VRRP just the same. Each
host's check is bounded by 120 s, all of them within a 600 s window that one
native call of the block's deadline covers.

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
unreachable port reports the dashboard unproved. The dashboard's direct
certificate and roles remain [B479](milestones/m2.md#b479); its OAuth2 single
sign-on is enabled by the management gateway unit above, and the monitoring
stack is realized by its service-unit block above.

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
Confidential material kept beside a native effect follows
[native-side retention](secrets.md#native-side-retention). Partial removal
preserves custody for recovery; final withdrawal follows the engine
transaction. Downstream material is scoped to declared resources and does not
install an add-on.

Produced storage material is not an authored Secret and cannot be revealed
through Secret commands under the [custody contract](secrets.md#produced-material),
whose table lists each storage output's one revealing command: the dashboard
administrator password, one StorageClient's Ceph keyring and a
StorageObjectUser's S3 key pair. A client keyring is revealed in the native
keyring form, for one selected managed cluster and one of its declared clients
settled before any custody read, until the destroy that completes the removal
withdraws it. A bucket block reads its owner's S3 key pair only into its
worker's memory to sign the owner's S3 requests and never retains, answers or
records it. This contract adds no other command or implicit reveal.

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
domain and disks, including every OSD disk. The hosts' stop proof stands in
for client quiescence only when no declared consumer of the cluster survives
the removal; the [StorageExport](storage-resources.md#storageexport) record
states that condition and its refusal for a consuming container cluster. A
surviving host keeps the refusal below, and a bare-metal one retires only
through the erase-backed
[physical retirement](#devices-and-destructive-ownership). Retirement is
recorded as durable evidence, so continuation never contacts the stopped hosts.

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

## NVMe-oF subsystems

A `ceph-service-<cluster>-nvmeof-<service-token>` unit realizes one gateway group,
after its explicitly declared configuration pool and image/settings proofs.
The service token is the first sixteen hexadecimal characters of SHA-256 of
the native service ID, preserving dotted native IDs within safe block names.
Its daemon count and pinned auxiliary image, native host/group and completed
gateway initialization are readiness requirements. A
`ceph-nvmeof-<subsystem>` block (Kind StorageNVMeoFSubsystem, exclusive
`ceph/<cluster>`) waits on that unit and every declared RBD image provider.
The request freezes group, pool, mTLS mode, gateway/listener endpoints,
authorized NQNs and each backing image's full identity and shape.

Before the first resource effect the bounded native worker proves the cluster
FSID and original owner, completed owned gateway service, exact dashboard
gateway registry and native gateway identities, configuration pool, and each
RBD creation attempt, image ID and shape. It refuses a pre-existing subsystem
without this attempt's intent. It records the exact request and stable serial
and namespace UUIDs before creation, then creates subsystem, namespaces,
listeners and authorized hosts. Exact readback is required before completion.
A lost reply resumes only an exact subset of the same original attempt; extra
hosts, listeners, namespaces, a foreign serial or UUID, or a changed group,
image, pool or owner refuses. Completed allocations are never silently
recreated. No provider key values are returned in evidence or refusals.

Observation is read-only and repeats identity, explicit authorization and HA
proofs. Every declared listener must be active and reachable by TCP from the
bootstrap host. Each namespace load-balancing group must have an optimized
path and only optimized/non-optimized advertised paths across its gateways.
Evidence carries the stable subsystem serial and no key material. Native
readiness is not an initiator login, I/O, failover or physical qualification
claim. The request-derived deadline includes preparation, bounded resource
creation, separate cluster and resource observation, and the readiness retry
window. B491 continues to refuse online individual removal; whole-owned-cluster
retirement remains supported.

`nvmeof-native` names unavailable/refused qualified native commands;
`nvmeof-identity` names a changed backing, gateway, authorization or native
identity; `nvmeof-foreign` names a subsystem without this attempt's intent.
These are fixed, non-secret refusals with recovery guidance. Request formats
move to `ceph-core-v32` and `ceph-host-v7`; old incomplete operations
must be finished or destroyed with their original build.

## RBD replication

[StorageReplication](storage-resources.md#storagereplication) has its own
`storage-ceph-replication-v2` implementation, `ceph-replication-v1` frozen
request and `ceph-replication-evidence-v1` observation. Its two runtime scopes
come only from both clusters' original apply proofs. Scoped SSH keys and
proved host keys reach the controller worker as private files; no ambient SSH
configuration or context is consulted. Each endpoint selects the SSH key of
its proved bootstrap host, independent of the host list order. The existing
bounded ResourceWorker, registered native worker and private per-cluster
custody directory own effects.

The fixed native interface is `rbd mirror pool enable`, `mirror pool info`,
`mirror pool peer bootstrap create`, `mirror pool peer bootstrap import
--direction rx-only <pool> -`, `mirror image enable <pool>/<image> journal`,
`info`, `ls` and `mirror image status`. Ordinary owned-pool and cluster proof
reads precede these commands. Import consumes the token on stdin. Pool info
never uses `--all`, which exposes peer credentials. The source permits no
peer; the receiver permits exactly its recorded source UUID, site FSID,
client and `rx-only` direction. Custom site names and preexisting bootstrap
credentials refuse. Each effect is preceded by a durable original-attempt
marker; immutable pool/image/peer IDs and constant-time comparison with the
retained original token detect replacement. Tokens are capped at 1023 bytes,
matching the native importer's fixed input buffer. The source retains the
ASCII token on its host under the `ceph-replication-custody-v1` reservation
([native-side retention](secrets.md#native-side-retention)). Continuation re-proves native output against the retained token and never
replaces completed custody. Raw tokens and CephX keys otherwise stay in
worker/controller memory and stdin, never observations, diagnostics, argv or
public produced files.

Native bootstrap internals have debug logging of decoded tokens, so every RBD
invocation explicitly disables `debug-rbd`, `debug-rados`, and `debug-ms`
(`0/0`) and native log sinks. Bootstrap/import failures are opaque named
refusals, never native stdout/stderr. This boundary follows Ceph's
[bootstrap implementation](https://github.com/ceph/ceph/blob/v20.2.2/src/librbd/api/Mirror.cc)
and [stdin token import](https://github.com/ceph/ceph/blob/v20.2.2/src/tools/rbd/action/MirrorPool.cc).
Journal readiness follows native
[image mirroring status](https://github.com/ceph/ceph/blob/v20.2.2/src/tools/rbd/action/MirrorImage.cc).
Live qualification of the selected IBM build remains unrun.

For `i` images (1–3), apply makes `8+3*i` bounded calls: prepare both pools,
create/import token, enable each source image, observe both pools and each
image on both sides, then commit both endpoints. Each call reserves 660
seconds around a 640-second worker with 60-second native commands. The
three-image commit reads at most nine commands (540 seconds). Initial sync
adds one 1800-second retry window, a 60-second interruption allowance and
60 seconds of local margin: `(8+3*i)*660+1860+60` seconds (13,140 at three).
Observe makes `2+2*i` calls and reserves `(2+2*i)*660+60` seconds. No calls run
concurrently; retry polling clips its call timeout to the remaining window.
The ceiling is independent of core cluster enrollment's budget.

Closed refusal names are `request`, `artifact`, `ownership`, `evidence`,
`material`, `transport`, `remote-refusal`, `adapter-exception`,
`native-command`, `native-timeout`, `native-output`, `unknown`,
`foreign-cluster`, `foreign-pool`, `pool-identity`, `foreign-image`,
`image-identity`, `replication-identity`, `replication-unavailable` and
`replication-token`, plus existing SSH-policy refusals. Each preserves unknown
or partial effect unless both endpoint proofs establish completion. Evidence
is bounded to 16 KiB and contains only owner/request identity, both FSIDs,
owned/existing/ready flags and the ordered image IDs and synchronization flags.

## RGW multisite blocker

B483's required native `ceph rgw zone create` route cannot currently satisfy
the no-argv/no-log credential boundary. Ceph's
[RGW manager](https://github.com/ceph/ceph/blob/v20.2.2/src/pybind/mgr/rgw/module.py)
accepts a token on stdin, but its
[RGWAM implementation](https://github.com/ceph/ceph/blob/v20.2.2/src/python-common/ceph/rgw/rgwam_core.py)
passes decoded access and secret keys as `radosgw-admin realm pull` argv and
logs native invocations, output and failures. The
[native CLI](https://github.com/ceph/ceph/blob/v20.2.2/src/rgw/radosgw-admin/radosgw-admin.cc)
assigns those pull credentials from CLI options, with no qualified stdin or
config credential route. JSON realm/zone imports do not provide a period
import: realm creation resets current-period identity, so they cannot safely
bootstrap authenticated pulls from stored zone credentials. Bootwright does
not clone upstream behavior with internal RADOS writes or accept an input
that can only refuse at execution. Existing multisite refusals remain;
B483 and P7 qualification remain open pending a native secret-safe interface
and empirical initial-sync/failover evidence.
