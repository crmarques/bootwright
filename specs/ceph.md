# Managed storage lifecycle

This page owns the target runtime contract for managed Ceph, the storage
family's one implementation, which the CephCluster kind selects with
`management: managed`
([implementation selection](architecture.md#implementation-selection)). The
[storage API](api/ceph.md) owns admission and references;
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
[version selection](api/ceph.md). Each feature maps to
intent, projection, effects, readiness, access, continuation and removal gates.
Missing intent receives a deliberate API extension. Admission of other
distributions or external clusters does not select an M2 implementation.

The approved [native service target](ceph-native-api.md) uses complete
ServiceSpec documents under a closed allowlist of the service types Bootwright
realizes.
The [resource graph](ceph-resources.md) binds daemon owners and gives
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

## Selection and refusal

The selector follows the [domain template](architecture.md#implementation-selection).
A `CephCluster`'s `spec.management` selects: `managed` selects the
implementations whose block families [the plan](#plan-and-prerequisites)
freezes, and an `external` cluster selects none here. An effect-bearing Ceph
object that no bound implementation claims refuses by the
[unclaimed-object table](state-reconciliation.md#stages-and-the-pause-boundary),
and the combinations the release does not support refuse as the
[unsupported combinations](ceph-coverage.md#unsupported-combinations) list.
`TestEveryExampleObjectIsClaimedOncePerFamily` holds every example to the claim.

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
and a declared `packageRepositoryTrustBundleRef` is the only TLS anchor
of the authored package repository for package acquisition, and a supplied digest does not substitute for required
publisher trust. FIPS, CephX, encryption and call-home choices retain their
independent API, trust and authorization requirements. Native tools determine
whether the selected versions can realize those choices; Bootwright does not
screen their version compatibility. A FIPS Ceph request freezes its FIPS and
AES CephX policy, requires a completed FIPS installation proof for every
installed host, and checks each host's kernel and inherited crypto policy
before cluster claims: exactly `1` and exactly `FIPS`, as the
[managed-OS proof](managed-os.md#fips-mode) reads them. A provided host has no
installation proof, so its host block reads the same two files over the fleet
connection before host preparation and on every apply and observation, and
records them as its `fips` host evidence, never as a Machine installation
proof; a mismatch or an unreadable file refuses `fips-not-enabled`, naming the
Machine, and the cluster block requires that evidence. Its pinned host key
must be ECDSA or RSA of at least 2048 bits. Bootwright never switches a
host's FIPS mode; FIPS mode at installation, disk encryption and patching of
a provided host stay the operator's. Ed25519 cluster SSH keys and authored AES256K CephX policy refuse
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
The [storage API](api/ceph.md) owns version selection and the
[release qualification](ceph-coverage.md#release-qualification) owns the
digest resolution and the authored-digest rule.

Auxiliary images. A native service whose daemons run an image other than the
Ceph image needs an auxiliary image, named by cephadm's
`mgr/cephadm/container_image_<name>` setting. The operator authors each as
`spec.images.<name>` of the CephCluster, with `base` and `version`
under the rules of `image` ([admission](api/ceph.md)). The names form
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
image, a declared service derives a manager module or a CephSMBResource
names the cluster, is the single writer of these cluster-global settings: it
sets each frozen auxiliary digest with
`ceph config set mgr mgr/cephadm/container_image_<name> <image>` and enables
each derived manager module (`nfs` for a declared `nfs` service, `smb` when a
CephSMBResource names the cluster) with `ceph mgr module enable`. It writes
a setting only while the monitor configuration database holds no entry for it,
accepts an entry that holds
exactly the frozen digest in the `mgr` section, and refuses `foreign-image`
before any write for any other entry of that setting (another value, section,
mask or location), never overwriting it. Every other block only reads these
settings. The settings block follows the cluster block and, like every cluster
child, retires with the cluster. Bootwright keeps no default
auxiliary image and no table of values. A declared service or
CephSMBResource whose image is not authored, and an authored image that
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
The resolver installs no weak dependency, so a tool the host phases run that
the provider packages only recommend is a root of its own: the container
runtime `podman`, which cephadm only recommends, is resolved at its latest
version from the hosted DVD's BaseOS and AppStream repositories, as `openssl`
for the pull broker is, on every Ceph host, provided ones included, and
cluster removal retains it like the other roots.
Freeze repository metadata, the complete signed RPM closure, authenticated
package headers and the before/after RPM inventory in a separate durable
package decision before installation. Acquisition reads the authored
`packageRepository`, or its source default, over HTTPS without proxy or
redirect and never from another repository; it verifies that repository
against `packageRepositoryTrustBundleRef` alone when it is set and
against the host trust store otherwise, and a connection the anchor does not
verify fails the package phase with a native error that names the repository
and the anchor, never a credential; a publisher key connection that does not
verify names the key location and its selected anchor, never credential bytes.
An authored repository requires `packageRepositoryKey` at the same HTTPS
origin (host and effective port); that key uses the repository's sole TLS
anchor and the unchanged IBM publisher fingerprint. Omitted repository and
key select the public source defaults together. Neither metadata, RPM payloads
nor keys fall back to public sources. A connection lost in transport (closed
before a response, reset, truncated, refused, timed out, ended by TLS EOF, or
without a route or a name answer yet because the network is down) is fetched
again from its first byte from the same URL, at most three attempts within the
acquisition budget, and each attempt is verified like the first; an HTTP
status, a TLS verification or protocol failure, a name that does not exist and
a size, digest or signer refusal are never retried. This is the same
classification as [image resolution's](ceph-coverage.md#release-qualification). Continuation uses that decision and
refuses an unrelated inventory or changed installed payload. An explicitly
accepted IBM license authorizes only the provider's required `ACCEPT_EULA=yes`
transaction environment; missing acceptance refuses before package effects. The
provider's license record exceeds the MON's default config-key entry bound
while its store write reports success, so before acceptance the license call
raises the cluster's `mon_config_key_max_entry_size` to `LICENSE_STORE_FLOOR`
only while the persisted value is lower, never lowering it, and proves the
persisted and each quorum MON's running value within one manager wait
([D219](milestones/backlog.md#d219)); the value leaves with the cluster.
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
device. A missing or contradicting identity record refuses.

## Plan and prerequisites

The complete dependency DAG precedes effects. Storage declares dependencies
through API identities, following the
[lifecycle port contract](architecture.md#lifecycle-ports), and freezes bounded
blocks, native inputs, target identities, impacts, authorization and exclusive
resources. Readiness is a proved block postcondition, never an ordering hint. Admission
requires each host’s resolved enrollment address, including the bootstrap
address, to equal its fleet SSH address; separate native enrollment addresses
have no qualified implementation.

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
  CephCluster API identity that the cluster's image, host, settings and
  service-unit blocks share; a client still follows its pool through the
  CephPool it names.
- A service-unit block `ceph-service-<cluster>-<type>[-<id>]` (Kind
  CephCluster, claims per [removal and concurrency](#claims-and-throttle)) applies the native documents of
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
  port 9283 of every MGR host, for a declared prometheus service. The
  management gateway and every keepalive-only ingress whose
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
  an `incarnation` member, the monitor map's creation time that `ceph mon dump`
  reports, in the UTC form `YYYY-MM-DDTHH:MM:SS.ffffffZ`, or `""` while no
  cluster exists. Every ceph-core evidence carries
  an `sso` member, `""` or `"oauth2"`; only a management gateway unit with an
  oauth2-proxy may report `"oauth2"`, and its readiness needs it. Before a fresh apply registers, the bound certificate
  must name the gateway's `virtual_ip` (an IP address entry) or, without one,
  every host of its placement (a DNS name or a matching wildcard), or the
  apply refuses naming the Secret; no certificate byte enters the refusal.
  For RBD mirroring, a declared `rbd-mirror` service forms one
  `ceph-service-<cluster>-rbd-mirror` block whose one singleton document
  carries no `service_id` or `spec`; its daemons run the Ceph image, so it
  needs no auxiliary image, and its deadline is apply 2280 s, observe 720 s,
  destroy 1380 s.
  Every CephCluster plans one derived `ceph-service-<cluster>-crash` block,
  whether or not it declares a service: its one singleton `crash` document
  carries no `service_id`, `spec` or `networks` and places every cluster host
  in name order; it runs the Ceph image, needs no auxiliary image, depends on
  the cluster and image blocks (and the settings block when one is planned),
  and its deadline is apply 2280 s, observe 720 s, destroy 1380 s. It is ready when every placed host
  runs one crash daemon on the frozen Ceph image. It retires with its cluster;
  removing it alone is [B491](milestones/m2.md#b491)'s.
- A gateway block `ceph-gateway-<gateway>` (Kind CephObjectGateway,
  exclusive `ceph-rgw/<cluster>`) executes its gateway's realm, zone group, zone,
  zone placement and rgw service on the bootstrap host. It depends on the
  cluster and image blocks, on each of its derived zone pool blocks by ID and,
  when an ingress fronts its rgw service, on the settings block, and on each
  declared placement CephPool through that pool; its deadline
  is fixed (apply 4260 s, observe 720 s, destroy 1380 s) and does not grow with
  the host count.
- An object user block `ceph-object-user-<user>` (Kind CephObjectUser,
  [unclaimed](#claims-and-throttle)) realizes one CephObjectUser on the bootstrap
  host. It depends on the cluster and image blocks by ID and on its
  CephObjectGateway through that object; its deadline is fixed (apply
  2280 s, observe 1380 s, destroy 1380 s) and does not grow with the host
  count.
- An object bucket block `ceph-object-bucket-<bucket>` (Kind
  CephObjectBucket, [unclaimed](#claims-and-throttle)) realizes one
  CephObjectBucket on the bootstrap host. It depends on the cluster and
  image blocks by ID and on its owner CephObjectUser through that object;
  its deadline is fixed (apply 2280 s, observe 1380 s, destroy 1380 s) and
  does not grow with the host count.
- An NFS export block `ceph-nfs-export-<export>` (Kind CephNFSExport,
  exclusive `ceph-rgw/<cluster>` when RGW-backed) realizes one CephNFSExport on the bootstrap
  host. It depends on the cluster and image blocks and on its NFS service-unit
  block `ceph-service-<cluster>-nfs-<serviceID>` by ID, and on its backing
  CephFilesystem, CephSubvolume or CephObjectUser through that
  object; its deadline is
  fixed (apply 2280 s, observe 720 s, destroy 1380 s) and does not grow with
  the host count.
- An SMB block `ceph-smb-<object>` (Kind CephSMBResource, implementation
  `storage-ceph-smb-v1`, [unclaimed](#claims-and-throttle)) realizes one
  CephSMBResource. It depends on the cluster, image and settings blocks by
  ID; a cluster requires its `resourceRefs` members, a share its SMB cluster
  and its CephFilesystem or CephSubvolume, so auth resources precede
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
uses actual backing-filesystem availability plus unique proved allocations,
and counts what the apply writes beside its guests as
[capacity admission](substrates.md#capacity-admission) states.
A provider host on the SSH arm is observed as
[capacity admission](substrates.md#capacity-admission) states, without reuse
credit.

Storage preflight reports future Machines whose managed OS installation has
not started as pending without remote guest probes. Installed or provided OS
guests require actual prerequisite observations before readiness is proved.
A provided stretch tiebreaker is read as Ceph reaches it: as the cluster SSH
user with the fleet key against its pinned host key, never with its operator
`auth` arm or the operator's SSH options, and never when it pins no host key
([stretch topology](#stretch-topology)). A fleet connection that is not
established reports one `host.access` row naming the fleet key Secret to
authorize, its user and the pinned host key Secret. On any host whose key a
`knownHostsRef` pins, a `host.trust` refusal names that Secret rather than
`machine trust`.
A host identity, kernel, OS, time, privilege or DNS probe whose command runs
and exits nonzero fails its check; only a read that does not complete, the
SSH session included, leaves it unknown. So
`host.dns` fails when the host's resolver does not know its declared name.
cephadm writes some peers into a daemon's configuration by host name
([peers addressed by name](../.agents/knowledge/ibm-ceph-monitoring.md#peers-addressed-by-host-name)),
so on each host whose resolved placement runs `mgmt-gateway`,
`alertmanager`, `grafana`, `prometheus`, `loki`, `alloy` or `promtail`, and
on each MGR placement host of a cluster that declares one of them, a
`host.peer-names` check proves that every node's native hostname resolves
there; it fails naming the host, its services and each unresolved name.
A package that host preparation installs, the container runtime included,
is not a prerequisite, so preflight neither probes nor reports it.

Storage accepts any selected time source that answers
([time service readiness](infrastructure-services.md#managed-network-services))
and requires no proof of its upstream synchronization; Ceph's own `MON_CLOCK_SKEW` health check, which
the readiness set below does not tolerate, reports MON clocks that disagree.

B472 fixes provider-specific ordering and forbids cycles. Storage never installs
another domain's prerequisites itself. Preflight names unmet prerequisites and
their owners; [B490](milestones/m2.md#b490) qualifies host and capacity proofs.

Topology placement binds nodes to proved Machines, which are Ceph nodes because
the topology names them ([substrate arm](api/machines.md#substrate-arm),
[admission](api/ceph.md#complete-graph-invariants)), and to declared sites. Native cluster size and role counts are the native tools' to
judge ([release qualification](ceph-coverage.md#release-qualification));
acceptance-profile sizes impose no additional restriction. Native stretch mode
requires the complete two-data-site topology and an independent
third-site monitor-only tiebreaker, which [stretch topology](#stretch-topology)
owns. [B487](milestones/m2.md#b487) owns its service/endpoint restrictions and
[B488](milestones/m2.md#b488) its failure and recovery acceptance.

A compiled graph resolves every reference field, so the plan looks up only
derived native service names, and each lookup that misses refuses by name
before any effect rather than as an unsupported capability. Every request of a
cluster refuses with `api.reference` when one of its `ingress` services names a
`spec.backend_service` that is not one declared service of the cluster. A
service-unit request refuses with `api.invariant` when the
`nvmeof.<service_id>` name that an `nvmeof` service's `service_id` derives is
not one declared service, and a filesystem request when the
`mds.<filesystem>` name its `spec.mdsServiceRef` names is not.

Each [host preparation](#bootstrap-identity-and-continuation) block freezes
[presentation groups](cli/output.md#multi-machine-presentation) of its
Machine, one per step it runs in this order as its own native call resuming
from the journals: `install-packages`, `pull-ceph-image`, one group per pull
batch of the host's auxiliary images, `prove-runtime`, `prove-route` and
`identify-devices`. A host pre-pulls an auxiliary image when the placement
of a declared service needing it (per the
[auxiliary image table](#frozen-release-and-trust)) can schedule a daemon
there: the topology nodes its `hosts`, `label` or `host_pattern` selects,
whatever its `count`, so a daemon cephadm moves to another candidate after a
host failure still finds its image already pulled through the broker. When a
needing service's candidates cannot be derived exactly (its placement does not
resolve to declared nodes) or the image serves a CephSMBResource (`samba`,
`samba_metrics`), every host pre-pulls it. The host request freezes these
images as batches; a batch's pulls run at once, each with its own broker,
scratch and journal records, and its call returns once every pull has,
raising the first refusal in name order. The images worker's deadline signal
comes 40 s before its supervisor stops it, so a batch it interrupts cancels
every pull, which stops its process and leaves its broker, and returns the
timeout while the worker still runs; a call under 180 s, which can start no
pull, keeps its whole budget. A batch holds up to three of the host's auxiliary images
in name order, every batch but the last holds three, and its group is
`pull-` followed by their closed cephadm names (`_` written `-`), such as
`pull-alertmanager-grafana-node-exporter`, described as `pull
container_image_alertmanager, container_image_grafana and
container_image_node_exporter by digest together`, so the running row names
every image then downloading; at most five batches keep a block within its 32
groups. Only the pulls run under the longer host launcher, and together within
its 4285 s: each pull call receives what the previous ones left of that budget
and refuses `image-pull` once it is spent. The host block's apply deadline is
the packages call's 1980 s, that budget with the last pull call's 60 s
transport slack, the four later 660 s calls, and 10 s for each role step's own
start-up and reports, counted for the largest pull plan (eleven steps with
five batches), 9075 s in all. The runtime profile runs under the
ordinary native ceiling and refuses a digest of the host's pulls that they
did not record. A final read proves the package decision the first step
reported and publishes the evidence, and a step a resumed attempt already
proved settles as soon as its journal proves it again. A release freezes no
group.

### Refusal table

Each row is one refusal of the Ceph capability, and
`TestCephRefusalTableMatchesUnsupported` holds its `Unsupported` to it before
operation registration and secret custody. Its reason and remedy are the
refusal's reason and remediation. The capability refuses the CephCluster
and every CephPlacementPolicy, CephPool and CephClient whose
`clusterRef` names it, each with the same row. [Unsupported combinations](ceph-coverage.md#unsupported-combinations)
links each matrix combination that this table refuses.

| Refusal | Path | Reason | Remedy |
| --- | --- | --- | --- |
| Incomplete or unsafe Ceph coordinates or transport | a CephCluster, or a CephPlacementPolicy, CephPool or CephClient whose `clusterRef` names it, declaring an empty `release`, `packageVersion`, `cephadm.ansible.packageVersion` or `image.version`, an empty `image.base`, or a `cephadm.clusterSSH.transport` other than `verifiedTunnel` in an effective state that admission did not refuse (an omitted transport normalizes to `verifiedTunnel`, and `validate` refuses `native`) | `the selected Ceph coordinates or cluster SSH transport are incomplete or unsafe` | `declare bounded version strings, an image repository and verifiedTunnel transport; native transport is refused and host-key verification cannot be disabled` |
| A native service whose auxiliary image is not selected | a CephCluster, or a CephPlacementPolicy, CephPool or CephClient whose `clusterRef` names it, declaring in `services` a service whose type needs an auxiliary image (an `alertmanager`, `alloy`, `grafana`, `ingress`, `loki`, `mgmt-gateway`, `node-exporter`, `nvmeof`, `oauth2-proxy`, `prometheus`, `promtail`, `smb` or `snmp-gateway` service) that `images` does not set; `<service>` is the first such service's qualified name in name order, `<family>` its `service_type`, `<name>` the first image it needs in name order that `images` does not set (only `keepalived` for an `ingress` with `spec.keepalive_only`) and `<cluster>` the CephCluster | `the <family> service <service> needs cephadm's container_image_<name>, and spec.images.<name> is not set; Bootwright keeps no default` | `set spec.images.<name> of CephCluster <cluster> to an image beneath its Entitlement registry, or remove <service> from its spec.services, together with its spec.serviceBindings entries` |
| CephSMBResource objects whose samba images are not selected | a CephCluster, or a CephPlacementPolicy, CephPool or CephClient whose `clusterRef` names it, that a CephSMBResource of any resource type names in its `clusterRef` while `images` does not set both `samba` and `samba_metrics`, and that the native-service row does not refuse; `<smb>` is the first such CephSMBResource in name order, `<name>` is `samba` unless `images` sets it, else `samba_metrics`, and `<cluster>` the CephCluster | `CephSMBResource <smb> needs cephadm's container_image_<name>, and spec.images.<name> is not set; Bootwright keeps no default` | `set spec.images.<name> of CephCluster <cluster> to an image beneath its Entitlement registry, or remove every CephSMBResource whose spec.clusterRef is <cluster> from the selected Environment` |
| An auxiliary image that no declared service needs | a CephCluster, or a CephPlacementPolicy, CephPool or CephClient whose `clusterRef` names it, setting `images.<name>` for an image that no service in `services` and no CephSMBResource naming it needs (per the auxiliary image table, with `samba` and `samba_metrics` for a CephSMBResource) and that neither the native-service nor the CephSMBResource row refuses; `<name>` is the first such name in name order and `<cluster>` the CephCluster | `spec.images.<name> is set, but no declared service or CephSMBResource of CephCluster <cluster> needs cephadm's container_image_<name>` | `remove spec.images.<name> from CephCluster <cluster>` |
| More Ceph hosts than the cluster run deadline admits | a CephCluster without `topology.stretch`, or a CephPlacementPolicy, CephPool or CephClient naming it, whose topology declares more nodes than the [cluster execution budget](#cluster-execution-budget) fits under the run ceiling; `<deadline>` is that budget's apply deadline for `<hosts>` hosts and `<most>` the largest host count it admits | `CephCluster <cluster> declares <hosts> Ceph hosts, whose cluster run deadline of <deadline> is past the 6h0m0s every adapter run is held to` | `declare at most <most> entries in spec.topology.nodes of CephCluster <cluster>; B79 tracks larger clusters` |
| More hosts than a stretch cluster's run deadline admits | a CephCluster declaring `topology.stretch`, or a CephPlacementPolicy, CephPool or CephClient naming it, whose topology declares more nodes than the [cluster execution budget](#cluster-execution-budget) with its stretch activation fits under the run ceiling; `<deadline>` is that budget's apply deadline for `<hosts>` hosts and `<most>` the largest host count it admits | `CephCluster <cluster> declares <hosts> Ceph hosts, whose cluster run deadline of <deadline> is past the 6h0m0s every adapter run is held to` | `declare at most <most> entries in spec.topology.nodes of CephCluster <cluster>; B79 tracks larger clusters` |
| A distribution that core Ceph does not realize | a CephCluster, or a CephPlacementPolicy, CephPool or CephClient whose `clusterRef` names it, whose `distribution` selects the `oss` or `redhat` arm, an omitted `distribution` included, because omission selects `oss` ([distribution](api/ceph.md#cephcluster)) | `core Ceph realizes only the IBM Storage Ceph distribution` | `select spec.distribution.ibm` |

The [`clusterSSH.transport` field rule](api/ceph.md) states what omission
selects and that `validate` refuses `native`, so the transport part of this
row holds only as a guard behind admission.

## Stretch topology

A `topology.stretch` cluster is native
[stretch mode](https://docs.ceph.com/en/tentacle/rados/operations/stretch-mode/):
two data sites hold two replicas each and a third site holds only a
tiebreaker MON. [Admission](api/ceph.md#complete-graph-invariants) proves the
topology; this section owns what the plan derives and realizes (D196).

- **Failure domain and host location.** `failureDomain` is a native default
  CRUSH bucket type above `host`. Each data-site host is placed under its
  site's bucket of that type: a node's `location` names only that key, its
  value is the node's site, and an omitted `location` is derived from the
  site. The tiebreaker holds no OSD and has no host location. Outside stretch
  a host `location` refuses.
- **The tiebreaker.** It runs a MON alone: it is never the bootstrap node, and
  an authored service other than `mon` whose resolved placement includes it
  refuses; the derived per-host crash collector still runs there
  ([B686](milestones/m2.md#b686)). It may be a provided Machine
  (`os.provided: true`), the only place a provided Machine joins a Ceph
  cluster. A provided tiebreaker is reached over the fleet connection with
  the [fleet key](api/environment.md#remote-machine-access-and-install-defaults),
  which its Machine never names, because that key is effect material and a
  Machine access key an access credential (D204). Its `access.ssh` names the
  cluster SSH user and a `knownHostsRef` pinning the one host key every Ceph
  connection verifies; its `auth` arm serves operator sessions only. The
  operator authorizes the fleet key's public half (`bootwright secret show
  --part public-key`) for that user on the host. Storage preflight reads its
  prerequisites over the same fleet connection
  ([plan and prerequisites](#plan-and-prerequisites)), and the host block's
  first fleet call proves it again: a `transport` failure there names the
  Machine, the fleet key Secret, the user and the pinned host key beside the
  retained evidence to inspect. A tiebreaker without that user or pin, or with no fleet
  key declared, refuses at plan. It holds no device and no erase claim, needs
  no Machine block, and takes its OS dependency packages from the hosted DVD of the bootstrap
  host's install profile; a host whose release that tree cannot serve fails
  the native package resolution. Its FIPS proof is
  [observed](#frozen-release-and-trust).
- **Networks.** `networks.publicCIDRs` may list several CIDRs, rendered as
  one comma-separated `public_network`; every MON host's address lies inside
  one of them, because cephadm places a MON only on such a host
  ([MON service](https://docs.ceph.com/en/tentacle/cephadm/services/mon/)).
  The sites talk over routed IP, so their hosts need no shared subnet; only
  an ingress's hosts do
  ([complete-graph invariants](api/ceph.md#complete-graph-invariants)).
- **Pools and placement.** The cluster owns one CRUSH rule, `ruleName`, in
  IBM's single-`take` form: `take default`, `choose firstn 0 type
  <failureDomain>`, `chooseleaf firstn 2 type host`
  ([IBM stretch mode](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=storage-stretch-mode)).
  Every pool of the cluster, service-created pools included, uses that rule
  with `size: 4` and `minSize: 2`; an authored other pair refuses, and so do
  erasure-coded pools and device-class rules. A CephPlacementPolicy of a
  stretch cluster refuses at validation and at plan, and so does a CephPool
  `placementPolicyRef`, each remedy saying that the stretch rule applies.
- **Activation.** Declarative where native specs exist: each data host's
  enrollment spec carries the `location` `{<failureDomain>: <site>, root:
  default}`, as IBM's bootstrap specification places a site bucket under
  the root the rule takes, and the MON spec carries a `crush_locations`
  entry `<failureDomain>=<site>` for every MON host, the tiebreaker's at its
  own site, all derived from the topology; an authored MON `spec` other than
  exactly those entries refuses at plan. Activation starts once the cluster
  is ready in every other respect (every MON in quorum, every OSD up),
  because `mon enable_stretch_mode` needs every MON's location, and runs in
  two bootstrap-host calls, `stretch_locate` and `stretch_enable`, through
  five ordered stages. Each stage writes an attempt record before its first
  native command and a completion record only after its readback shows its
  postcondition:
  - `located`: `osd crush tree` shows each data host's bucket under its
    site's failure-domain bucket under `default`, no bucket for the
    tiebreaker or its site, and `mon dump` shows the MON count, the
    tiebreaker's MON, a MON at each data site and each MON's location. A
    host's bucket is the one its OSDs join, named after its short hostname,
    because an OSD's CRUSH location names the short name and starts directly
    under `default`, while the enrollment spec's `location` places only a
    bucket of the registered hostname, which holds no OSD when that name is
    fully qualified. A data host's bucket, when absent or directly under
    `default`, is created or moved with `osd crush add-bucket <short name>
    host root=default <failureDomain>=<site>` or `osd crush move <short name>
    root=default <failureDomain>=<site>`, and a MON without a location takes
    `mon set_location <mon> <failureDomain>=<site>`, because cephadm sets no
    location on a MON it already deployed
    ([MON service](https://docs.ceph.com/en/tentacle/cephadm/services/mon/));
    any other misplacement refuses `stretch-location`.
  - `rule-present`: IBM's procedure (`osd getcrushmap`, `crushtool -d`, the
    rule appended with the next free rule ID, `crushtool -c`) compiled offline
    in the frozen image, then `osd setcrushmap -i - <crush_version>` guarded
    by the CRUSH version `osd dump` reported before the map was read and
    reports again after the compile (else `not-ready` before the attempt),
    read back from `osd crush rule dump`
    ([IBM CRUSH map for stretch mode](https://www.ibm.com/docs/en/SSEG27_9.9.1/administration/config-crush-map-stretch-mode.html)).
    A rule of that name without this operation's attempt refuses
    `stretch-rule`.
  - `election-connectivity`: `mon set election_strategy connectivity`, read
    from `mon dump`; a strategy already set to connectivity is recorded
    without a command.
  - `stretch-enabled`: `mon enable_stretch_mode <tiebreaker MON>
    <ruleName> <failureDomain>`, read from the stretch fields of `mon dump`
    and `osd dump`; stretch mode already enabled without this operation's
    attempt refuses `stretch-foreign`. Before its attempt record the stage
    proves what the command needs: exactly the two data-site buckets of the
    failure domain (else `stretch-location`), equal and positive CRUSH
    weights of those buckets as `osd crush dump` holds them (else
    `stretch-weight`; Bootwright never reweights an OSD) and every pool
    replicated at the default protection (else `stretch-pools`).
  - `verified`: every pool `osd dump` lists is on the rule at size 4 and
    min_size 2, otherwise `stretch-pools`.

  A stage's readback after its commands polls at most five times, 2 s
  apart, since a command can return before every map it changes commits.
  A stage whose attempt record exists without its completion record is read
  back: its postcondition completes it, and its absence refuses
  `stretch-interrupted` without repeating the command; a completed stage
  that no longer holds refuses `stretch-drift`. Only the manager's `.mgr`
  pool, which bootstrap creates at the default protection, exists before
  activation, which `enable_stretch_mode` moves to the rule; every other
  pool, a CephPool's, a gateway's zone pool or a service's own, is created
  by a later block, after the cluster block's readiness. A CephPool and a
  zone pool of a stretch cluster are created on the stretch rule at 4/2, and
  the PG budget counts them on it.
- **Frozen intent.** Stretch is day-1 intent of the frozen cluster request:
  adding or removing `topology.stretch`, changing its tiebreaker, failure
  domain or rule, or a host's site refuses `stretch-intent` against the
  ownership and bootstrap-scope records the cluster was created with, before
  any other proof, its remedy retiring the whole cluster, and `mon
  disable_stretch_mode` is never run as an inverse.
- **Readiness and observation.** The cluster evidence carries a `stretch`
  member, the last journaled stage and whether stretch mode is enabled as
  this request enabled it. Readiness requires the `verified` stage and every
  stage's postcondition observed again; observation reports the member, and
  `bootwright diff --observe` reports a cluster whose observed stretch state
  lacks a stage or no longer holds one as partial realization. A pool off
  the rule or protection keeps the cluster partial, whoever created it; the
  [qualified services](#stretch-services-and-front-doors) create none.
  Every block's frozen cluster scope carries the stretch block, so host,
  SMB, consumer and replication blocks prove the same bootstrap scope.

The JSON keys the stages read that no cited page fixes are recorded as
unverified in the [stretch native facts](../.agents/knowledge/ceph-stretch-native-facts.md)
until B488's rehearsal reads them from the selected build.
Not yet met: an interrupted stretch stage has no forward exit short of
retiring the whole cluster; tracked as [B920](milestones/m2.md#b920).

### Stretch services and front doors

A stretch cluster realizes the services its storage consumers need (D203):
MON, MGR, OSD, the derived crash collector, MDS for CephFS, RGW with its
front doors, and the [CephConsumer](ceph-resources.md#cephconsumer) export.
Other services keep their ordinary admission and claim no site-loss
behaviour. NVMe-oF refuses, because IBM requires at least four Ceph nodes per
data site
([NVMe-oF in stretch](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=cluster-configuring-nvme-in-stretch)),
and so do an NFS service, a CephNFSExport and a CephSMBResource, whose
stretch behaviour has no qualified profile.

- **Placement.** Services keep native placement (`hosts`, `label`,
  `host_pattern`, `count`, `count_per_host`); a service's sites are those of
  its resolved hosts, so no site field exists. An `mgr`, `mds` or `rgw`
  service's resolved hosts exclude the tiebreaker and include a host at each
  data site, and a `count` set without `count_per_host` exceeds its hosts at
  either data site, so every choice cephadm can make places a daemon at
  each.
- **Pools.** A qualified service creates no pool off the stretch rule:
  `.mgr` moves at `stretch-enabled`, a filesystem's pools are CephPools, and
  a gateway's zone pools are created on the rule. `.rgw.root`, which any
  radosgw-admin call would create on the default rule, is created by the
  gateway that claims it, after its claim and before any radosgw-admin call,
  with `osd pool create .rgw.root 32 32 replicated <ruleName>
  --autoscale_mode on`, then size 4 and min_size 2 and the `rgw`
  application. A claimed `.rgw.root` on another
  rule refuses `stretch-pools`, and the gateway's readiness requires it on
  the rule at 4/2.
- **Front doors.** An `rgw` service is reached directly or through one
  ingress per data site. Directly, a daemon runs on every placed host and the
  endpoint FQDN resolves to every rgw host's public address at both sites,
  for an operator-owned load balancer or DNS or a declared DNSServer's
  records ([attached gateway DNS](ceph-resources.md#attached-gateway-dns)).
  Behind ingresses, each is placed only at its own data site with its own
  `virtual_ip`, both carry the same `frontend_port`, `ssl` and bound
  certificate Secret, and the endpoint resolves to both virtual IPs. An rgw
  ingress whose hosts span both data sites is a stretched VIP and refuses, as
  does an `rgw` service behind a single ingress, because a floating L2
  address does not survive the loss of its site.
- **Readiness.** The gateway is ready when its placement count of rgw
  daemons runs on the frozen image, at both data sites, each answering on its
  frontend port, and, behind ingresses, each ingress's haproxy and keepalived
  daemons run and each virtual IP answers, over TLS with the bound leaf.
  Client I/O through each site during a site loss is
  [B488](milestones/m2.md#b488)'s operator evidence.
- **Consumer export.** The export of a stretch cluster carries every MON,
  the tiebreaker's included, every manager host at both sites and the
  gateway's endpoint addresses. Data Foundation external mode against a
  stretched cluster is owner-run acceptance under B488; Bootwright records no
  compatibility rule for it (D163).

Not yet met: NVMe-oF has no stretch profile of at least four nodes per data
site; tracked as [B935](milestones/m2.md#b935).
Not yet met: NFS and SMB have no stretch profiles; tracked as
[B936](milestones/m2.md#b936).
Not yet met: observability and management services claim no site-loss
placement on a stretch cluster; tracked as [B937](milestones/m2.md#b937).

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
2. A pull of the frozen image digest and of each frozen auxiliary digest
   [it may run](#plan-and-prerequisites), each through its own
   invocation-local broker admitting only that repository and digest, with a
   protected authfile, a signature policy limited to that repository and at
   most three attempts per preparation run. A pull
   counts once local inspection proves the digest and the linux/amd64
   platform. Retained attempt records serve crash diagnosis and recovery.
   Scratch cleanup preserves validated durable records and refuses unvalidated
   names in their reserved namespace, unknown matching files, symlinks or
   foreign-owned directories. Incomplete scratch under the original owned
   temporary namespace remains recoverable. Those records do not consume a
   later apply's retry budget while the image is still absent: that apply
   makes its own three attempts. Bootstrap then skips cephadm's own pull
   (`--skip-pull`) and runs the image this phase proved, so no guest pull
   reaches the registry outside the broker.
3. A runtime profile from a contained probe of the image: the ceph account,
   crypto policy, SELinux label and cephadm bytes, bound to the authenticated
   frozen image. Host RPM payloads retain their independent authenticity and
   integrity proof without a package/image compatibility comparison.
4. A route probe of the provider's native SSH class inside the image.

The [coverage page](ceph-coverage.md#release-qualification) owns the probe
and route requirements.

The cluster block depends on every host block, re-proves the recorded profile
against the inspected image, and refuses a missing or contradicting one. Between
the tunnel mount and enrollment it publishes and proves the exact cephadm bytes
in the cache. Host preparation retains packages and the image as shared host
state. The owned per-FSID host runtime tree under
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
refuses before submission ([B1084](milestones/m2.md#b1084)). The
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
observation never changes them. Every admin client (`ceph`, `rbd`, `rados`,
`radosgw-admin`) runs on those retained outputs and names its monitors with
`-m`: the seed `monIP`, then the canonical public-network IP of each host the
MON service places, by name. It overrides the retained file's seed-only
`mon_host` without rewriting it, so one MON outage with quorum held leaves
administration available. Each call also passes
`--auth_client_required=cephx`, as the file sets no client authentication and
the client default admits `none`, so only a MON completing CephX with the
retained admin keyring answers. A phase that restarts the manager waits, bounded and inside its
worker budget, for the MON to report an available
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
phase it failed in: resolution, signature or transaction. A connection to a
selected artifact lost in transport after its bounded attempts is instead
`package-acquisition`, whose coordinate names only that artifact:
`RPM <name>-[<epoch>:]<version>-<release>.<arch>`, `IBM Ceph publisher key`,
`repository metadata <repository id>` or `installation tree metadata`, never a
source URL; TLS verification, HTTP status, a name that does not exist, size,
digest and signer failures keep their phase. Its detail names that
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
A helper failing before its diagnostic validator initializes keeps its
type and code site and omits native error metadata.

The coordinate may end with the ids of the
[version specializations](architecture.md#implementations-and-version-variation)
applied in that run; within its bound, that suffix is truncated before the
image and package part. The same ids appear in the block's evidence: the Ceph
core and ceph-host evidence carry an optional `specializations` member naming,
sorted, the specializations the native phases of the execution that wrote it
applied, absent when none applied. The member is informational: no effect,
ownership, readiness or identity decision reads it, and it enters no intent or
digest. [Named refusal outcomes](#named-refusal-outcomes) states how a refusal
ends the attempt.

### Cluster execution budget

The CephCluster's cluster block, the verifiedTunnel apply that bootstraps,
enrolls and deploys the cluster, covers every host count in
`spec.topology.nodes` that the selected native tools accept (D130), within
the deadline bound below. Fewer than three hosts keep their existing refusal.
The deadline below is the only host bound: no fixed host count is added to it.
A host count whose cluster deadline passes the run ceiling refuses before
operation registration and secret custody, as a row of the
[refusal table](#refusal-table) for the CephCluster and every
CephPlacementPolicy, CephPool and CephClient naming it, and request planning
and every block's deadline refuse it with the same reason and remedy.

Not yet met: clusters of more hosts than the run ceiling fits under this
budget refuse; tracked as [B79](milestones/m2.md#b79).

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
Physical hosts may therefore select their distinct devices in separate
OSD specifications without multiplying bounded remote calls.
A stretch cluster's deadline adds its two activation calls and one more
readiness wait, the one before activation: 2 × 660 s + 900 s. Seven stretch
hosts, the reference profile, need 21120 s, the most the ceiling fits, and
eight would need 21780 s; past seven, a stretch cluster refuses with the
[refusal table](#refusal-table)'s row for a stretch cluster's run deadline.
Three hosts need 19 calls and 16260 s; six hosts need 22 calls and 18240 s,
including P1's physical shape with twelve host-specific OSD specifications;
seven need 23 calls and 18900 s; eleven need 27 calls and 21540 s, the most
the 21600 s ceiling fits, and twelve would need 22200 s. The services call's
documents do not enter the formula, so a host-specific OSD specification per
selected disk keeps it. Neither does a host's device count: each per-host call
proves, binds or prepares all of its host's selected disks, so more disks lengthen
calls, not the call sequence. The six-hour adapter ceiling, per-call budgets, input
bounds and readiness proofs remain unchanged. A request whose deadline exceeds
the ceiling refuses at planning, coded `lifecycle.unsupported`, with the
[refusal table](#refusal-table)'s row for more Ceph hosts than the cluster run
deadline admits.

Both readiness waits stay 900 s. A readiness timeout after the services call
is recoverable: once cephadm has converged, the next apply observes a ready
cluster, every host's stage is complete, and the apply passes. The six-host
live runs of the [virtual provider matrix](milestones/m2.md#b484) record the
measured OSD convergence time; a shortfall there is its own item.

| An apply that stops | The next apply |
| --- | --- |
| before the bootstrap call | continues each host from its `unclaimed`, `claimed` or `prepared` stage |
| inside bootstrap, tunnel configuration or tunnel mount | continues from the seed stages of the [verified transport](ceph-native-api.md#verified-native-orchestration-transport) |
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
Machine's original erase policy to cover every selected
data, DB and WAL disk. Without that policy an interrupted physical deployment
has no forward or retirement exit today: only the orphan-acknowledged
[context deletion](contexts.md#permanent-deletion) remains, and it abandons
the hosts, their daemons, the claimed disks and their data.

The budget changes no `partial-cluster` refusal. Observation, destruction,
child blocks, host blocks and service units keep their deadlines; the ones that
grow with the host count n take n + 3 calls to observe the cluster, n + 2 to
destroy it and 2n + 3 to apply a block other than the cluster: 9, 8 and 15
calls at six hosts, and 14, 13 and 25 (9300 s, 8640 s and 16560 s) at eleven,
all under the ceiling. A CephFilesystem's apply adds one call per data pool and
per subvolume group to its 2n + 3 and one more readiness wait when it declares
a group, so at eleven hosts it fits six data pools without groups, or four
pools and groups together. One past that refuses at planning, coded
`lifecycle.unsupported`, with reason
`CephFilesystem <filesystem> needs a run deadline of <deadline>, past the 6h0m0s every adapter run is held to: two native calls for each of the <hosts> Ceph hosts of CephCluster <cluster> and one for each of its <pools> data pools and <groups> subvolume groups`
and remedy
`declare fewer spec.dataPoolRefs or spec.subvolumeGroups on CephFilesystem <filesystem>, or fewer entries in spec.topology.nodes of CephCluster <cluster>`.
Every enrollment call admits at least one host within its worker budget, so n
calls enroll n hosts at any count. The services call submits at most the 128
specifications the worker admits, and at eleven hosts P1's physical shape with
three host-specific OSD specifications per host submits 35. Past 128 MON, MGR,
OSD and MDS services, which one specification per disk passes from nine
physical hosts of fifteen disks, planning refuses, coded
`lifecycle.unsupported`, with reason
`CephCluster <cluster> declares <services> MON, MGR, OSD and MDS services, past the 128 one services call submits`
and remedy
`list a host's disks in one OSD service of CephCluster <cluster> instead of one service per disk`.
With one OSD service per host, eleven hosts of fifteen disks plan, and their
frozen request, ready evidence, host evidence and physical retirement policy
stay inside their bounds. At eleven hosts the
frozen request and the ready cluster evidence stay well inside the 256 KiB
request and 60 KiB evidence bounds, and the physical retirement policy admits
the same host count as the cluster.

The concurrent rounds are implemented under [B688](milestones/m2.md#b688), and
the deadline as the only host bound under [B890](milestones/m2.md#b890); the
live qualification of both remains outstanding.

## Named refusal outcomes

A named refusal of a Ceph block ends the attempt with its entry's code. An
entry coded `lifecycle.unknown` leaves the block's attempt outcome unknown, so
the next apply observes that block first and resolves it from the observed
evidence, a host block through its preparation evidence. Only the entries coded
`lifecycle.unsupported` (`unsupported-release`, `package-license`,
`package-platform`, `dashboard-verify`, `client-quiescence-unqualified`,
`call-home-unqualified`, `native-option-undeclared`,
`ssh-policy-host-excluded`, `ssh-policy-unavailable`,
`tunnel-host-key-excluded`, `device-not-empty`, `foreign-osd`,
`device-serial-absent`, `device-serial-ambiguous`, `device-serial-mismatch`,
`device-alias`, `device-root`) leave it failed. A censored failure that names
no refusal stays the generic adapter failure, failing the attempt.

## Devices and destructive ownership

When the original policy retains the root, the Ceph host block first proves
and removes only its original FSID's native daemon layout, own cluster-key
entry and registry anchor, using the verified package cephadm without disk zap.
A terminal retirement requires that native-absence receipt for every retained
root, including hosts with no OSDs, plus verified erase receipts for every
selected data, DB and WAL disk. Foreign state or an interrupted unproved
cleanup refuses or remains partial. Generic online removal remains closed by
the client-quiescence requirement.

Protected Bootwright recovery/custody and retry journals and its restrictive
SSH policy remain on the retained OS; native absence is not whole-filesystem
cleanup or sanitization. Reinstallation overwrites the independently selected
root, and explicit root erasure keeps its existing whole-disk semantics.

Physical retirement requires explicit original erase authority for every
selected data, DB and WAL disk, and for the root only when the policy names it
([root-target rule](api/machines.md#physical-erase-policy)); an omitted root is
retained. Machine removal erases only its frozen explicit targets; an
OS reinstall still overwrites its independently selected root volume.
The Machine's proved logical-volume NAA is excluded from OSD selection at the
live disk-identity boundary, even when the authored root uses firmware discovery.

An OSD declaration authorizes only a finite, proved device set on its bound
Machines. Before provisioning, dynamic selectors, including `all`, resolve to
concrete device identities; the operation durably binds that set before its
first destructive effect. A restart cannot discover additional disks and add
them to the same authorization. The capability re-proves identity and ownership
immediately before use, including data, DB and WAL devices and every intended
shared-device allocation.

The bounded core profile accepts explicit `data_devices.paths`, optional
`db_devices.paths` and `wal_devices.paths`, `encrypted` and `tpm2`, with one
OSD per data device. A `crush_device_class` at spec level, or on a
`{path, crush_device_class}` data path record, which takes precedence as in
ceph-volume, is the CRUSH class each such OSD takes: since cephadm's stored
spec keeps no path's class, the adapter submits one native OSD document per
host and class, with the class at spec level. Read-only readiness requires each
declared class in `ceph osd tree`; an OSD still starting without it stays
unready, and a booted OSD of another class refuses `osd-class-drift`
(`lifecycle.unknown`), because cephadm classes only the OSDs it creates. A
class is a native CRUSH name of at most 64 letters, digits, `-`, `_` and `.`;
a DB or WAL path record's class refuses at validation, since cephadm classes
only data OSDs, and data devices of more than one class beside DB or WAL
devices refuse at plan, since per-class documents cannot share them. All
selected paths are distinct within a drive group and across services on a
host; dynamic selectors, slots and sizes remain
unsupported. TPM2 requires encryption and a declared TPM profile. Omitted
options retain native BlueStore defaults. A nonempty DB/WAL device is reused
only when the retained intent and complete native OSD/LV relationships prove
its entire allocation belongs to this operation. Partial or foreign DB/WAL
allocations refuse before submission; [B1084](milestones/m2.md#b1084) owns
partial recovery. Read-only host readiness also proves completed data/DB/WAL
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
here to `/dev/disk/by-id/wwn-0x` plus 16 or 32 lowercase hexadecimal digits,
or `/dev/disk/by-serial/<serialNumber>` with 1 to 128 ASCII letters, digits
and `._:+-`. No such directory exists on a host: the second form names the
disk its Machine declares as a
[`hardware.eraseOnRemoval`](api/machines.md#physical-erase-policy) target
without `deviceName` and with that `serialNumber`, whose `sizeBytes` it takes,
because physical retirement needs that target anyway. A serial the Machine
declares no such target for, one its target names by `deviceName`, and the
root disk's serial refuse `api.invariant` at the selector. Preparation refuses
`device-root` a disk reporting a serial-declared root's serial.
It resolves the serial path on the live host, before the first destructive
effect, to the one whole disk, a hidden NVMe path node (`hidden` reads `1`)
excepted, whose sysfs serial number, read as the [physical
erase](state-reconciliation.md#opt-in-physical-logical-overwrite) worker reads
it, is that serial, a fixed, writable `sd` or `nvme` disk exactly `sizeBytes`
long. It records the disk's kernel path, its observed WWN when it has one, and
a stable alias: its `/dev/disk/by-id/wwn-0x` link when it has one, else the
first in byte order of its other `/dev/disk/by-id` links that contain the
serial and resolve to it. The alias, never the serial path, enters the native
service document; the binding is frozen with the host's device record, and
readback re-resolves the serial and refuses any change of kernel path, alias,
WWN or size. No such disk refuses `device-serial-absent`; more than one, such
as two paths of a multipath disk, namespaces of one NVMe controller or a
cloned serial, refuses `device-serial-ambiguous`; a disk of another size or
kind refuses `device-serial-mismatch`; one without such an alias refuses
`device-alias`. Each is coded `lifecycle.unsupported` and names the Machine
and the serials its host selects, and the first binding raises them before
any disk is written.
A physical host admits up to 15 distinct local whole disks its OSD services
select, data, DB and WAL paths together: the
[install-time clear](api/machines.md#install-time-data-disk-clear) bound, within
the 16 targets of its Machine's
[erase policy](state-reconciliation.md#opt-in-physical-logical-overwrite)
([D198](milestones/backlog.md#d198), confirmed by
[D252](milestones/backlog.md#d252)). No call, round or deadline grows with the
device count, since every per-host call proves all of its host's disks at once
(the [cluster execution budget](#cluster-execution-budget)).
One disk past it refuses at request planning, coded `lifecycle.unsupported`,
with reason
`CephCluster <cluster> selects <disks> disks on physical Ceph host <host>, past the 15 permitted by physical install-time clearing (erase policies allow 16 targets)`
and remedy
`select at most 15 distinct disks for host <host> across the data_devices, db_devices and wal_devices paths of the OSD services of CephCluster <cluster>`.
Shared WWN or serial selectors across hosts, partitions and multipath targets
refuse at admission at any count. Before OSD preparation or service submission, the
coordinator compares every host’s bound observed WWN and serial/size pair,
including disks selected through different forms. The native service worker
rechecks that aggregate before any service effect. The request records WWN intent without inventing a serial
or capacity, or a serial selection's serial and size from its Machine.
A fresh installation of a managed physical host can clear declared OSD disks
that a destroyed or abandoned cluster left, through its Machine's
[`hardware.clearOnInstall`](managed-os.md#physical-data-disk-clear); nothing
here clears a disk.

Not yet met: a host's device-identity, device-proof, OSD-preparation (its
owned clears included) and device-readback calls, and the cluster observation's per-OSD configuration
reads, run per-disk or per-OSD native commands whose ceilings are not admitted
against the call's 640 s worker budget, on virtual and physical hosts alike, so a slow host
ends the call ambiguous rather than refusing by name; tracked as
[B900](milestones/m2.md#b900). Original physical Machine presence evidence
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

Host preparation records each disk's observed WWN, serial, size, rotational status,
kernel path, stable alias for a serial selection, and native image SDK
identity. It compares lsblk and udev WWNs and
device numbers, rejects read-only/removable disks and ambiguous aliases, and
re-proves the same facts on read-only host readiness. A replay cannot extend
or replace the bound disk set. See the
[physical example](../examples/ceph-clusters/physical-3-nodes-ceph/README.md).
This code path has synthetic coverage; live physical acceptance remains unrun.

Physical retirement requires the original opt-in
[erase policy](state-reconciliation.md#opt-in-physical-logical-overwrite), with
either method on each disk as its Machine's owner chose, to cover every
selected data, DB and WAL disk, by its WWN or by its serial number and size,
and the root when the policy names it. A `metadataClear` OSD disk reads as
empty to native probes afterwards but is not sanitized. Ceph contributes
the retirement policy Managed OS installation and erasure honor through
[their retirement port](managed-os.md#physical-removal): each physical node's
Machine waits for its cluster's retirement verification, named as the
CephCluster in the storage physical-retirement family, and the verification
waits for each node's substrate claim the same way. Without it, a
realized cluster's removal refuses before registration. Storage first records a pending-erasure disposition
so consumer removal and managed-OS cleanup can precede the offline erase worker.
A separate terminal verification block then matches every complete original-policy
erase receipt to the original machine UUID, complete MAC set and observed Ceph
disk serial and capacity, and its WWN where the erase target declares one. Only that terminal proof permits claim release.
A stopped host, a released claim, or a pending disposition proves no erasure.

When the original core cluster block is positively unstarted, the terminal
verification may use its frozen WWN or serial-and-size intent without an
observed OSD binding;
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

A declared OSD disk that holds a partition table, filesystem, LVM or other
signature this operation did not write refuses with the named token
`device-not-empty`, and one that holds a Ceph OSD of another cluster or one
this operation did not create, on the disk or in the cluster map, refuses
`foreign-osd`; both are coded `lifecycle.unsupported`, because no retry clears
them. Their remedy names `wipefs --no-act <disk>` to see what the disk holds
and, for a disk of a Ceph cluster the operator destroyed or abandoned, either
declaring it in its Machine's `spec.hardware.clearOnInstall`
([physical installation](managed-os.md#physical-installation)) and installing the
Machine again, or clearing it on the host after confirming the disk. The
refusal's detail names the token and the code site that refused, never the
disk: a native worker's cause carries no value, so the host and device are
not reported.

An intent-marked data disk that holds LVM metadata without a complete OSD tag
set, the shape an interrupted `ceph-volume` creation leaves, refuses
`osd-partial`, coded `lifecycle.unknown`, because an interrupted creation and a
foreign write cannot be told apart, except in one bounded owned clear. OSD
preparation clears the disk only when every read-only proof holds: the host's
OSD device intent record is this operation's; no OSD identity record is
retained for the disk; it is a data device of exactly one declared OSD service
on the host, and that service declares no DB or WAL devices, since
`ceph-volume` creates their volumes before it tags the block volume; `wipefs`
reports exactly one LVM signature and nothing on the disk is mounted; `pvs`
shows exactly one PV on it, in a volume group named `ceph-<uuid>` holding no
other PV; `lvs` shows exactly one volume in that group, named
`osd-block-<osd fsid>`, whose tags all start with `ceph.`, with no
`ceph.osd_id`, a `ceph.cluster_fsid` absent or this cluster's and a
`ceph.type` absent or `block`, and whose OSD fsid names no OSD this host
recorded; that volume is active as the disk's only holder and is itself held
by nothing, so an open dm-crypt mapping refuses; and no process on the host
runs `ceph-volume`. A volume group without its block volume refuses, because
no OSD in the map can be attributed to it.

The clear records its intent, which names the disk by its declared stable
alias, serial and size, the PV's UUID and the OSD fsid; reads `pvs` and `lvs`
again and requires the same PVs and volumes, because `--destroy` removes
every PV of the disk's volume group; and runs
`cephadm --image <frozen image> ceph-volume --fsid <fsid> -- lvm zap --destroy <disk>`
through the verified host cephadm, bounded at 120 s. It proves every declared
disk again as the first proof did, so the cleared disk must read empty; when
`/etc/lvm/devices/system.devices` lists the PV and `pvs` shows no PV of that
UUID on any disk, it removes only that entry with
`lvmdevices --yes --delpvid <PV UUID>`; then it records completion. A failed
zap refuses `osd-clear-failed`; a disk that does not read empty, or a
devices-file entry that survives, refuses `osd-clear-unproved`. Both are coded
`lifecycle.unknown` and keep the intent: a replay admits the same disk's PV
with its volume group and no volume, or without a volume group, zaps it again,
and settles a disk already empty without a zap. A record whose serial or size
differs from the disk now behind its alias refuses `osd-partial`. Each disk is
cleared at most once per operation; a later partial shape on it refuses
`osd-partial`. DB and WAL disks have no clear.

`ceph-volume` allocates the OSD ID, with `ceph osd new`, before it creates the
volume, and a killed creation skips its rollback, so the cleared volume's OSD
fsid may still name an OSD in the map that no disk will ever start, while the
OSD service already submitted can create the disk's OSD again. Cluster
observation, including the bootstrap state an apply reads before any host's
OSD preparation, therefore counts an OSD whose metadata names no device and
whose map entry never booted against no declared disk instead of refusing
`foreign-osd`, and such an OSD keeps the cluster unready, named in the
not-ready detail as `unbooted@osd.<id>.<osd fsid>`, so the apply goes on
to OSD preparation and the services call; every other OSD beyond the declared
disks still refuses `foreign-osd`, except to an observation no operation
records, a [`diff --observe` read](state-reconciliation.md#reading-the-real-side-live),
which reads the cluster partial, names it as `unplaced@osd.<id>` and returns
it in its inventory, so the comparison can show it; that read authorizes
nothing. A clear whose completion was never
recorded completes in OSD preparation when the disk now holds a complete OSD
carrying neither the OSD fsid nor the PV UUID its intent proved, since the
zap evidently ran: it drops the PV's devices-file entry as above and records
completion with no zap. Each host's OSD preparation returns the
OSD fsids of its completed clears, also once the disk holds an OSD of another
fsid again, and the bootstrap host's services call,
before it submits any service, reads `ceph osd dump`: an fsid no OSD carries
is settled; an OSD of that fsid that never booted (down, `up_from` 0, its state
`exists` and `new`) is recorded and purged with
`ceph osd purge-new osd.<id> --yes-i-really-mean-it`, then the map must no
longer hold it, or the call refuses `osd-orphan-unproved`; an OSD of that fsid
that booted refuses `osd-orphan-booted` with no purge. Both are coded
`lifecycle.unknown`. Since every OSD preparation reports every completed
clear, a purge that an interrupted, lost or refused services call left
undone is finished by the next apply. The devices file's
interaction, the clear and the purge are unqualified on a live RHEL host.

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
The [API ownership rules](api/ceph.md) prevent one resource from implicitly
creating or deleting another resource's pools, services or credentials.

Resource evidence binds the logical object, cluster FSID, owning operation and
native immutable identity where the provider supplies one. Native-created
children have a named owner, creation boundary, dependency edges and qualified
absence/removal proof; they cannot become unrecorded side effects or acquire
ownership merely by matching a name. A service may not adopt another object's
pool or credential as its generated child. B472 and each resource item settle
native identity and child-resource gaps before promotion.

A placement policy and a pool each write their attempt record
(`ceph-policy-attempt-v1` before `osd crush rule create-replicated`, and
`ceph-pool-attempt-v1` for every pool, replicated or erasure-coded, before
`osd pool create`) before the native create, and record the native rule or pool
ID after it. An erasure-coded pool proves that no profile and no rule of its
name exist before it writes its attempt record, and sets its profile without
`--force`. A retry adopts a same-name rule,
profile or pool only with that record and the frozen shape: the rule's steps,
the stored profile as exactly the passed keys plus the plugin's frozen
defaults, and the pool's type, rule and PG count. Anything else refuses before
an effect: `foreign-rule` for a policy, `foreign-profile`, `foreign-rule` or
`native-output` for an erasure-coded pool, and `pool-identity` for a pool. This adoption, like each adoption on an attempt
record and a frozen shape below, trusts the one writer of that native name:
the one block of the cluster that creates it, which planning proves and
[claims](#claims-and-throttle) serialize where several blocks write one
cluster-wide state; a writer outside Bootwright on a managed
cluster is outside that guarantee
([mutation safety](state-reconciliation.md#mutation-safety)). Neither phase
ever removes a pool, profile or rule. A monitor's PG refusal of the create (`ERANGE`) refuses `pg-budget`. When fewer
hosts under root `default` hold an OSD of the class than the replica count or
the erasure-code row requires, the policy or erasure-coded pool refuses
`crush-capacity` before its rule or profile exists. Erasure-coded overwrites
are irreversible, so they are enabled only for rbd and cephfsData pools.
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
under their recorded IDs. It writes its attempt record
`ceph-rbd-image-attempt-v1` before `rbd create`, and records the native image
ID after it. A retry adopts a same-name image only
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
frozen metadata and default data pools; the
[refusal table](ceph-resources.md#cephfilesystem) names each other outcome.
Subvolume groups follow once every active rank is held. Readiness
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
and the frozen shape; the
[refusal table](ceph-resources.md#cephsubvolume) names each other outcome. Readiness compares the recorded path,
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
when keepalive-only, and the virtual IP holder proof below; a
readiness timeout names `<daemon type>@<service>`, `vip@ingress.nfs.<id>`,
`noholder@ingress.nfs.<id>` and `holders@ingress.nfs.<id>` beside the blocking
health checks. Observation never
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
These options have one owner: authored values in `config` refuse. Before
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

A managed [CephConsumer](ceph-resources.md#cephconsumer) is realized by
its consumer block, which mints the consumer's own principals and produces its
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
(each round calls every proxy host concurrently, so a round lasts one call of
at most 120 s whatever the host count; rounds 10 s apart, a 600 s window), that
every running oauth2-proxy container of this cluster there skips no identity
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

A CephObjectUser is realized by its user block `ceph-object-user-<user>`
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
ready recaptures it read-only with `object_user_read` when an attempt or a
resolution records it; an unrecorded observation, such as `diff --observe`,
declares no output and reads readiness alone. Evidence carries the
user ID only, never a key. The block never modifies, suspends or removes a
user, and adds no key, subuser or capability; it retires with the cluster's
hosts, and [B491](milestones/m2.md#b491) owns its online removal. S3 requests
with the pair are the operator's acceptance.

A CephObjectBucket is realized by its bucket block
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
and a non-empty `Statement` ([field rule](ceph-resources.md#cephobjectbucket-field-record)),
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

A CephNFSExport is realized by its export block `ceph-nfs-export-<export>`
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
[field record](ceph-resources.md#cephnfsexport) states this effect
restriction. On add the export JSON carries no key, and the NFS manager reads
the user's first key itself; an update must carry the stored pair, so the
worker reads it from `user info` into its memory, adds it to the apply's stdin
only and redacts each key from any refusal. Readiness also proves the stored
key pair, the gateway's names and both defaults; a moved default is not ready
(`default@realm.<realm>`) and the next apply sets it again, unless another
realm became the default.

A CephSMBResource is realized by its SMB block `ceph-smb-<object>`. Its
request freezes the admitted native document with only two changes, the
resolved placement of an SMB cluster and the `cephfs` leaves a share's
CephFilesystem or CephSubvolume reference supplies; the bound passwords
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
service `ceph orch ls` lists is declared by a CephSMBResource of the cluster
(`foreign-service`) and that the settings block holds both samba image settings
at the frozen digests (`foreign-image`); it never writes them. It records its
attempt, then runs `ceph smb apply -i - --password-filter=base64
--password-filter-out=hidden` with the resource as JSON on stdin, its bound
passwords base64-encoded in memory, and reads each one with
`ceph smb show <name> --password-filter=hidden`; no password reaches an
argument, a record, evidence or a diagnostic. An answer that is not one
successful result for the resource refuses `smb-result` without quoting the
manager, whose answer can echo its input. A resource without this operation's
attempt record, or one this operation applied that is gone or changed, refuses
`smb-identity`; Bootwright never adopts an SMB resource. The remedies of
these refusals name only corrections outside Bootwright followed by an apply
with the same input, or a destroy before a changed CephSMBResource, because
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

A CephObjectGateway's realm, zone group, zone, zone placement and rgw service
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
leaf's SHA-256 to equal the bound certificate's and an HTTP status line, and
each ingress passes the virtual IP holder proof below; a
timeout names `tls@`, `vip@`, `noholder@`, `holders@`, `haproxy@` or
`keepalived@` the service. An ingress plans once its auxiliary images are
authored ([refusal table](#refusal-table)).

Readiness reports bounded observations of FSID, quorum, orchestrator and host
state, expected daemons, OSD membership, placement/capacity, pool protection,
filesystem/MDS state and declared service endpoints as applicable. Waits bound
time, polling, response size, cancellation and any native asynchronous work.
Timeout reports unfinished or unknown according to evidence.
B472 fixes predicates, limits and explicitly tolerated warnings/transitions;
blanket warning acceptance cannot claim readiness. The core profile requires
`HEALTH_OK`, or a health whose every check is in a closed tolerated set or is a
[scoped check](#scoped-health-checks) naming nothing the block owns. The
tolerated set holds only `CHA_ERROR_SENDING_REPORT`: IBM Call Home's egress is
the operator's network, not the cluster's state. A readiness timeout names the
checks still blocking in its refusal detail. A read-only observation that is
not ready reports the same entries, at most 16, as `storage.not-ready`
warnings `not ready: <entry>` beside its evidence, which `diff --observe`
shows as notes; an apply or removal reports none. A manager daemon whose container
cephadm is restarting is not ready and is named beside those checks; an
observation never refuses it, while an action that needs the manager's mounted
bundle still does. A host's daemon layout that cephadm moves while it is
observed (a listing, directory or unit file whose identity changed between its
first read and its re-proof, an entry gone after it was listed, a running
unit whose container has exited, a container that runs while its unit does not
and whose unit reads otherwise when read again, a container that appeared after
its unit files' or unit's read and whose files or unit are there when read
again, a container whose inspection failed and that a second listing still
shows, a `podman` or `systemctl` read that exited non-zero, could not start or
timed out, or an FSID directory held by another user while a singleton listed
for the host has only an empty root-owned directory or none) is observed again
and never admitted. A new attempt starts at most ten times, 3 s apart, and none
starts 60 s after the first, though the attempt then running may still wait out
its command timeouts. One that never settles is not ready, never a
contradiction: the readiness wait polls again and its timeout names
`unsettled@<host>`, while an action refuses `not-ready` and the next apply
observes again; a foreign FSID holder beside such a singleton is therefore
refused late as not ready, never admitted. An owner, mode, link, name, FSID,
content, image or unit format contradiction, native output that does not parse
or exceeds its bound, a container that runs while its unit, read again
unchanged, does not, and a container whose unit files or unit are still absent
when read again still refuse at once. Either refusal's cause names the
observation site that refused.

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
[storage API](api/ceph.md) states. IBM license readiness applies only
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

## Scoped health checks

A *scoped check* is a Ceph health check whose detail names the pools or
filesystems it is about, from a closed table (`SCOPED_HEALTH` in the
collection's `ceph_native` module, after the Ceph tentacle health-check list
and the detail lines its monitor, OSD map, MDS map and `pg_autoscaler` module
write): `POOL_APP_NOT_ENABLED`, `POOL_FULL`, `POOL_NEAR_FULL`,
`POOL_NEARFULL`, `POOL_BACKFILLFULL`, `POOL_PG_NUM_NOT_POWER_OF_TWO`,
`POOL_NO_REDUNDANCY`, `POOL_TOO_FEW_PGS`, `POOL_TOO_MANY_PGS`,
`POOL_HAS_TARGET_SIZE_BYTES_AND_RATIO`, `SMALLER_PGP_NUM` and
`MANY_OBJECTS_PER_PG` name pools; `FS_DEGRADED`, `FS_WITH_FAILED_MDS`,
`FS_INLINE_DATA_DEPRECATED`, `MDS_ALL_DOWN`, `MDS_DAMAGE` and
`MDS_UP_LESS_THAN_MAX` name filesystems (D256). When one is raised, the
observation reads `ceph health detail` once and takes the names from each of
the check's detail lines by an exact pattern per check. The check keeps a block
from [ready](#resources-readiness-and-access) only when it names a pool or
filesystem that holds the block: one the block owns, by declaring or deriving
it, or one its frozen request names as what it depends on. A CephPool is held
by its pool; a CephObjectGateway by its zone and placement pools and
`.rgw.root`; a CephFilesystem by itself and its metadata and data pools; a
CephRBDImage by its pool and data pool; a CephSubvolume by its filesystem and
pool; a CephClient by the pool, data pools or filesystem its capabilities
scope; a CephFS-backed NFS export by its filesystem; an NVMe-oF subsystem by
its pool and its namespaces' image pools and data pools. A service unit, the
settings block, a placement policy, an RGW-backed NFS export, an object user
or bucket (whose requests name only their gateway, which is held) and the
cluster block are held by none, and a CephSMBResource's observation reads no
cluster health. A request names only declared objects, so a check naming only
pools or filesystems no applied object declares holds no block. A scoped check keeps every block
from ready, as any other check does, when the detail omits it, when one of its
lines matches none of its patterns, or when it names a system pool (a name
starting with `.`) other than `.rgw.root`. Every other check, `MON_DOWN` and
`MDS_INSUFFICIENT_STANDBY` included, keeps every block of the cluster from
ready.

A cluster block's read under `diff --observe` names each scoped check it
passed, one per pool or filesystem, as a `storage.health` warning
`health: <check> names pool <name>` (or `filesystem <name>`), which
`diff --observe` shows as a note, so a check on a pool no object declares
stays visible beside the reading's
[undeclared pools](milestones/m2.md#b1114).

## Ingress host firewall

Before the first effect of every apply of an NFS service unit, of the
management gateway unit with its ingress, or of a gateway whose request
carries an ingress, a read-only check runs on each host of the ingress's
placement. When `firewalld.service` is active there, the check finds
the interfaces cephadm puts the virtual IP and VRRP on: those with an address
whose network (the address and its prefix length, masked) contains the virtual
IP's address, else, when none does, those with an address whose network equals
one listed in `virtual_interface_networks`. It reads each one's runtime
firewalld zone, or the default zone for an interface bound to none, and refuses
`ingress-firewall`,
naming the host, the interface, the zone and what is missing, unless that zone
permits VRRP (IP protocol 112) and each port the ingress serves: the client
port, which is the ingress `frontend_port`, or the NFS `port` when the ingress
is keepalive-only, or the management gateway's `port` for its keepalive-only
ingress, and for the management gateway also its internal port 29443, which
Prometheus service discovery dials on the virtual IP. The internal port is one
constant of the cluster admission, the same on every release, that the
management gateway unit's request carries as `ingressPorts`; the worker never
names it itself. A zone
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
zone, after this check, and never opens VRRP, so the check assumes neither.
It changes no firewall ([D220](milestones/backlog.md#d220)) and runs on every
apply, since a later firewall change drops VRRP the same. Every
host's check runs in one concurrent round, each call bounded by 120 s, so the
check lasts one call whatever the placement's host count, within the one
native call of the block's deadline that covers it; every host answers before
the first refusal in host order is returned.

Storage publishes the same requirements as
[host firewall requirements](managed-os.md#host-firewall-requirements),
derived from the requests the plan freezes, so a published requirement equals
what this check verifies: for each ingress host, named by its Machine, the
publishing unit or gateway block, the networks (first the virtual IP itself as
a single-host prefix, `/32` or `/128`, then the ingress's
`virtual_interface_networks`), protocol `vrrp` and the TCP ports above. Every
consumer selects interfaces by the rule above: those whose address network
contains the first network's address, else those whose address network equals
a later one. A stretched gateway publishes each data site's ingress on that
site's hosts; a cluster without an ingress publishes none. SMB and CTDB public
addresses are not VRRP ingresses and publish nothing.

The clusters stage of `preflight` probes every ingress host live and read-only, with
the same interface selection and grant rules, which one shared fixture corpus
holds the Go and the worker's tests to. It reads `systemctl show
--property=ActiveState firewalld.service` (an inactive or failed firewalld
passes), `ip -json address show`, `firewall-cmd --get-active-zones` (an
interface it lists is in that zone, any other in `--get-default-zone`),
`--info-zone` and `--info-service`, and reports a `host.firewall` row naming
what each zone does not permit with both remedies. Every `firewall-cmd` read
runs as root, through `sudo -n` when the access user is not root and the
`host.privilege` probe proved `sudo -n` yields root, because firewalld on a
managed guest admits its reads only to an administrator. A read that fails or
prints an unexpected form leaves the row unknown, never passed.

The virtual IP holder proof: a service unit's or gateway's readiness wait
proves, once its own observation is ready and within the same wait window, that
each ingress's virtual IP has exactly one holder: each host the observation
lists running that ingress's keepalived, one ingress per data site on a stretch
cluster, reports read-only whether one of its interfaces carries the address
(`ip -json address show`), each call bounded by 60 s; an idle placement host is
not asked. No holder blocks readiness with `noholder@<ingress service>`;
several holders, the split brain that dropped VRRP leaves, block it with
`holders@<ingress service>`; a keepalived host that does not answer blocks it
with `unreachable@<host>`. The wait polls again until the window closes, and
the proof sits beside, never in place of, the endpoint's answer on its port.

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
An observation that no attempt or resolution records, such as
`bootwright diff --observe`, declares no output: the cluster, client and object
user blocks prove their postcondition without returning the dashboard
password, the client keyring or the S3 key pair, and the cluster's bootstrap
host checks the dashboard account against its retained password with
`dashboard_present`, which answers presence alone.
Confidential material kept beside a native effect follows
[native-side retention](secrets.md#native-side-retention). Partial removal
preserves custody for recovery; final withdrawal follows the engine
transaction. Downstream material is scoped to declared resources and does not
install an add-on.

Produced storage material is not an authored Secret and cannot be revealed
through Secret commands under the [custody contract](secrets.md#produced-material),
whose table lists each storage output's one revealing command: the dashboard
administrator password, one CephClient's Ceph keyring and a
CephObjectUser's S3 key pair. A client keyring is revealed in the native
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
the removal; the [CephConsumer](ceph-resources.md#cephconsumer) record
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

### A provided tiebreaker's removal

A stretch cluster whose [tiebreaker](#stretch-topology) is a provided
Machine retires with its installed hosts, as above: the provided Machine
leaves only the surviving-host binding, and still counts for the retirement
record and consumer hosts. The cluster's core blocks read it from the frozen
host's `provided` member; a
[CephConsumer](ceph-resources.md#cephconsumer)'s request and each
[CephReplication](ceph-resources.md#cephreplication) endpoint freeze it in
`providedMachines` beside the `machines` they name, and their run refuses a
cluster proof that names other provided Machines (D206). Its cluster block removes the tiebreaker's
footprint itself, after every read-only refusal of the retirement and before
it records the retirement, so a failure stops the destroy before any
data-site Machine is deleted (D203). The removal reaches that host over the
fleet connection the cluster already uses for it, with its pinned host key,
and needs neither the bootstrap host nor MON quorum.

- **Proof before effect.** Read-only: the owner marker under
  `/var/lib/bootwright/ceph/<fsid>` on the host equals this cluster's marker
  identity, for the FSID Bootwright recorded; the daemon inventory lists
  only `cephadm:v1` daemons, none of another FSID and, of this FSID, a
  subset of the host's `mon` and `crash`; `/var/lib/ceph` holds nothing but
  this FSID's directory; the cluster SSH user's `authorized_keys` names the
  recorded key in no line but the recorded one; and each registry
  credential name is absent or carries the descriptor metadata its record
  holds. A host the cluster's claim never reached, as when its apply
  refused at the host prerequisites, holds no owner marker: with no record
  in that marker directory but a marker write's `.ownership-` stage name (a
  claim writes the marker first), no `authorized_keys` line of the cluster
  SSH user tagged `bootwright:<owner>`, no sshd drop-in of this cluster, and
  none of the FSID's own layout, which is
  `/var/lib/ceph/<fsid>` (where cephadm keeps each of the FSID's daemons),
  `/var/log/ceph/<fsid>`, the FSID's logrotate file, a `ceph-<fsid>` unit
  file in `/etc/systemd/system` or its `ceph.target.wants`, and an
  `/etc/ceph/ceph.conf` naming the FSID, read without following links, its
  observation proves the footprint removed with no attempt, and its removal
  changes nothing, writes no record and proves the same; another FSID's
  layout there is no part of this cluster's footprint and stays. This
  cluster's registry credential anchor counts as a trace and refuses as
  below; a registry credential without its record or anchor is not
  attributed and stays. A
  claimed host whose marker was lost with every such trace is
  indistinguishable from an unclaimed one and is proved the same way, with
  no confirmation. Anything
  else, an FSID-scoped part on an unclaimed host among it, refuses
  `tiebreaker-foreign` with no effect. The
  inventory is the bounded read of cephadm's own layout every Ceph phase
  uses in place of `cephadm ls`, whose logging start writes host files
  ([cleanup facts](../.agents/knowledge/ibm-ceph-cleanup.md)).
- **Effect.** An attempt record in that marker directory, then, each only
  while its part remains: the cluster-key line the `authorize` phase added
  to the cluster SSH user's `authorized_keys`, exactly as recorded, removed
  by a staged rewrite that keeps every other byte, the file's owner and
  mode, and refuses a file changed since it was read; the registry
  credential `/etc/ceph/podman-auth.json`, removed under its recorded
  descriptor metadata; and, while any native part remains, the package
  cephadm binary's `rm-cluster --fsid <fsid> --force`, never `--zap-osds`,
  never the copy under `/var/lib/ceph/<fsid>`, the binary's package proof
  and its parser's declaration of every option passed both read before the
  attempt record. That command removes the FSID's units, data, log,
  run and temporary directories, logrotate and sysctl files; with no Ceph
  cluster left, cephadm's `ceph.target`, its logrotate file and its
  `cephadm.log` files; and, when `/etc/ceph/ceph.conf` names the FSID, that
  file, `ceph.pub` and the admin keyring. Its exact footprint on the selected
  build is unverified until [B488](milestones/m2.md#b488) reads it.
  Packages, images, firewall openings, users, disks, the OS and Bootwright's
  own records under `/var/lib/bootwright/ceph*/<fsid>` stay.
- **Absence.** The inventory lists no daemon of the FSID, no unit file
  named `ceph-<fsid>` remains in `/etc/systemd/system` or its
  `ceph.target.wants`, neither `/var/lib/ceph/<fsid>`,
  `/var/log/ceph/<fsid>` nor the FSID's logrotate file exists,
  `/etc/ceph/ceph.conf` does not name the FSID, and neither the recorded
  `authorized_keys` line nor the credential remains, read without following
  links; only then is the completion recorded beside the attempt and the
  cluster's retirement evidence names the tiebreaker as cleaned.
- **Recovery.** A lost reply leaves the effect unknown: the next attempt
  observes first, completes without a command when absence is proved, and
  otherwise repeats each removal under its attempt record. The removal
  observation reports a partial effect once an attempt record exists and
  no effect before one, so either retries the destroy. An unreachable
  tiebreaker is unknown, never absent, and refuses `tiebreaker-unreachable`
  before the retirement record.
- **Evidence.** The cluster block's retirement record carries version
  `ceph.RetirementVersion`, and its `tiebreaker` member names the host and
  Machine it cleaned; a record of another version refuses by name (D142).
  Every other core block's record carries the version and no tiebreaker.

Only the cluster block reaches the tiebreaker; every other core block of the
cluster, its consumer export and its replications retire as before, with no
native call. An export's principals and a replication's peer and bootstrap
credential live in the cluster's monitor store, whose copy on the tiebreaker
goes with the FSID's data under `rm-cluster`, and their custody is on the
bootstrap host, which is never the tiebreaker; no rbd-mirror daemon runs
there, since the tiebreaker [runs a MON alone](#stretch-topology). Not yet met: the sshd drop-in
the `authorize` phase writes stays on the tiebreaker after its removal;
tracked as [B940](milestones/m2.md#b940).

Not yet met: a tiebreaker the operator has decommissioned or reinstalled
refuses this removal with no exit; tracked as
[B938](milestones/m2.md#b938).

### Claims and throttle

The context lease admits one mutator. Within its DAG, blocks declare exclusive
resources for cluster-wide orchestrator/configuration changes, host preparation,
device allocation and any shared native/package state they touch. Independent
blocks run concurrently only when those claims and dependency proofs permit it.

Three claims serialize what several Ceph blocks write at once
(`exclusiveFor` in `internal/storage/ceph/capability.go` owns the table).
`ceph-mgr/<cluster>` is held by the two blocks that restart the active
manager, the settings block (`mgr module enable`) and the monitoring unit
(cephadm's derived `prometheus` module), and by every block that also holds
`ceph/<cluster>`: the cluster block, NVMe-oF subsystems and their unit, the
`rbd-mirror` unit, CephConsumers and CephReplications (on both of their
clusters), whose manager use is not yet qualified beside a restart or
another orchestrator writer. `ceph-rgw/<cluster>` is held by gateways and
RGW-backed NFS exports, which write the cluster's `.rgw.root` and default
realm. Every other block claims nothing: the other service units, CephFS-backed
NFS exports, CephSMBResources, placement policies, pools (declared and a
gateway's zone pools), clients, RBD images, filesystems, subvolumes, object
users and object buckets. Every Ceph block of a cluster except its image and host blocks
also names the [throttle](state-reconciliation.md#stages-and-the-pause-boundary)
`ceph/<cluster>` (a CephReplication names its primary cluster's, the one key a
block can hold), so at most four of them run at once whatever the bound,
which keeps the bootstrap host's load and the manager's command queue
bounded. An unclaimed block runs beside the others because admission gives
each native name it creates one owning block in the cluster (a pool's native
name, including a name a gateway derives for a zone pool or `.rgw.root`, a
policy's rule, an erasure-coded pool's derived rule and profile) and because
every manager-served command it reaches survives a restart: a read repeats,
bounded by the manager wait, while a restarting manager cannot answer; an
observation reads it as unfinished work; and a declaration (`orch apply`,
`nfs export apply`, `smb apply`, `dashboard sso enable`) is made again,
after reading back an NFS export or the dashboard's SSO option a lost reply
may have applied, until the manager wait ends; each of its commands ends by
that deadline. Each native SSH effect holds the published cephadm cache
file's exact inode and metadata and the FSID directory's device, inode and
mode, refusing `cache-unproved` when any of them moved; the directory's owner
is proved only when the hold starts, because cephadm, as root, reassigns it
to each daemon it deploys, a sibling block's included. A failure is the
command's own answer, and refuses as before,
only when the available manager it was sent to still holds one interval
later, its orchestrator answers with cephadm, and the declaring `nfs`, `smb`
or `dashboard` module answers a read-only command. A concurrent manager
restart therefore never fails an unclaimed block. Readiness stays cluster-wide
except for the [scoped checks](#scoped-health-checks), which keep from
ready only the blocks owning the pools or filesystems they name: a sibling's
transient health check delays, and never fails, a block before its readiness
deadline.
Per-invocation scratch is private. Reservations prevent conflicting local
contexts from claiming the same target; a native name, process-local mutex or
context lease alone cannot coordinate another controller. B79 qualifies the
supported coordination scope and refuses unproved exclusivity before effects.

## NVMe-oF subsystems

A `ceph-service-<cluster>-nvmeof-<service-token>` unit realizes one gateway group,
after its explicitly declared configuration pool and image/settings proofs.
The service token is the first sixteen hexadecimal characters of SHA-256 of
the native service ID, preserving dotted native IDs within safe block names;
a long cluster name
[hashes an identity over 63 bytes](state-reconciliation.md#durable-identities-and-private-paths)
like every block identity.
Its daemon count and pinned auxiliary image, native host/group and completed
gateway initialization are readiness requirements. A
`ceph-nvmeof-<subsystem>` block (Kind CephNVMeoFSubsystem, exclusive
`ceph/<cluster>` and `ceph-mgr/<cluster>`) waits on that unit and every declared RBD image provider.
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
These are fixed, non-secret refusals with recovery guidance.

## RBD replication

[CephReplication](ceph-resources.md#cephreplication) has its own
`storage-ceph-replication-v1` implementation, `ceph.ReplicationVersion` frozen
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
