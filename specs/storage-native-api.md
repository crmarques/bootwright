# Native Ceph service input

This page owns the M2 target for the native service model approved in D133.
[Storage admission](api/storage.md) and the closed schemas own current accepted
input. M2b implements the common native path and strict MON/MGR/OSD admission;
[B492](milestones/m2.md#b492) adds the MDS family.
[B477](milestones/m2.md#b477) and [B478](milestones/m2.md#b478) add the RGW,
ingress and NFS records, which planning refuses until their execution lanes
land.
Release-qualified projection, additional families and protected leaf registries
remain gated by [B472](milestones/m2.md#b472), [B474](milestones/m2.md#b474)
and each [service item](storage-coverage.md#feature-matrix).
Examples covering later families remain target documents. The [review](storage-api-review.md)
records the approved before/after decision.

## Cluster and service ownership

Keep `StorageCluster`'s type, management, distribution, exact release/package/
image coordinates, entitlement, bootstrap/SSH, networks, security, native
cluster config and manager-module fields. Keep topology's Machine bindings,
hostnames and sites. One native service path replaces daemon-specific helpers:
`spec.ceph.services` contains complete native ServiceSpec mappings, including
DriveGroupSpec for OSDs.

The [Tentacle ServiceSpec](https://docs.ceph.com/en/tentacle/cephadm/services/)
is the comparison format; the selected IBM release's schema and support policy
are authoritative. Native names and nesting are retained, including
`service_type`, `service_id`, `placement`, `networks`, `config`, `spec`,
`extra_container_args`, `extra_entrypoint_args` and `custom_configs` where
qualified. Bootwright fields remain camel case outside native payloads.
Keeping a field's format does not authorize every value or side effect.

`services` is required and nonempty for the target managed Ceph arm. The final
MON and MGR services are explicit. A data-bearing cluster also declares OSD
services and the resources that use them. Provider-supported service counts
and topology restrictions apply; no fixed lab size becomes an admission limit.
Service-list order does not order effects: resource/service dependencies do.

An optional daemon family absent from the list is not requested. Bootstrap
suppresses automatic optional service stacks, then realizes declared services.
The bootstrap seed MON/MGR are temporary bootstrap-owned state that converges
to the declared final services; they are not separate permanent owners.
Any unavoidable native-created helper has a named derived owner and planned
effects. B80/B479 qualify this control over native bootstrap behavior.

### Verified native orchestration transport

D135 approves optional `ceph.cephadm.clusterSSH.transport: verified-tunnel`.
The outer OpenSSH connection verifies the original installation host key at
the frozen address and port before offering the cluster key. It forwards only
to that authenticated host's loopback SSH endpoint. The inner native connection
has no direct fallback, ambient agent, alternate credentials or arbitrary
proxy command. Unknown destinations and missing, writable or changed bundles
refuse. A transport qualification belongs to one immutable provider image;
it cannot promote `native`, the default transport, for that image.

Bootwright owns the protected key/config/proxy bundle and read-only mounts on
every eligible MGR. The cluster key belongs to the fleet user, which holds
`NOPASSWD` sudo, so it is root-equivalent on each host and no guest sshd policy
confines it (`sshd_config(5)` `AllowTcpForwarding` limits a session, not a user
who can edit the policy). The confinement is the client's: the pinned
installation host key, the fixed `-W 127.0.0.1:22` forward and no direct
fallback. The exact guest sshd policy Bootwright installs, which restricts TCP
forwarding to local loopback SSH and forbids remote listeners, Unix-socket
forwarding and tunnels, is hygiene, not a boundary. Foreign conditional sshd
configuration or an unproved include/alias closure still refuses before
publication, and no general sshd configuration editor is authorized. The
include closure is proved file by file, each include opened without following
links, every directory on its path root-owned and closed to others, and the
file regular, root-owned and closed to others; a root-owned symbolic link on
such a public root-only path, as RHEL's
`/etc/crypto-policies/back-ends/opensshserver.config` is, is followed through
the same proof under a bound of eight links, and such a file may carry a
package's hard links, as that back-end's target does. Every other native
source refuses a link and is the only name of its inode. Removing
the policy, its reload and its include parser is code work that waits for the
owner's answer on amending D135 (session question Q1 of the 2026-10-11 M2
review). Current candidate alias resolution is deliberately
closed pending the actual RHEL configuration qualification.

The bundle manifest binds the validated cluster public key and hashes only
nonsecret configuration and code. Original private material remains in
root-only custody outside the manager mount. Prove original descriptor identity
before reading it and compare materialized copies privately; matching bytes
cannot establish ownership of an unbound file. Read-only manager mounts must
have no alternate alias exposing those originals: each bind source is
identified as the directory or file it resolves to, a root-owned symbolic link
as a directory on its path, as RHEL's `/var/run` under cephadm's
`/var/run/ceph/<fsid>` is, followed under a bound of eight, and any other link
refuses. An observation reports a manager or initial seed daemon whose
container is absent or not running as pending rather than refused; tunnel
mount, enrollment and cache publication still require it running, and a
running container that contradicts the request refuses either way. The proxy
proves that its
private key derives the bound public identity with the provider's qualified
parser before opening SSH. Manager keyring/configuration retention follows
the same original-material rule before redeployment. A qualified SELinux
relabel is an explicit owned transition; it does not permit accepting
arbitrary metadata drift on replay.

Bootstrap first creates its frozen MON/MGR pair with zero native host inventory
and SSH setup skipped. It imports key/config/user while that inventory is empty,
then redeploys the initial MGR locally over Bootwright's verified fleet channel
with the protected read-only mount, preserving its exact native metadata and
ports. Enrollment follows current bundle, native config and mount proofs.
Subsequent native MGR specifications retain the generated mount. Public
[`cephadm deploy`](https://docs.ceph.com/en/tentacle/man/8/cephadm/#deploy)
is documented but marked deprecated in the inspected IBM source; the initial
redeploy route still requires provider artifact qualification. Private native
orchestrator interfaces are not substituted.

The pre-enrollment candidate recovery protocol distinguishes unclaimed,
prepared, exact initial MON/MGR seed, configured and mounted states. Its
Bootwright custody proof never creates directories, records custody or settles
relabels during observation.
Attempt fences precede native effects; original bootstrap configuration and
administrator keyring enter protected custody before consumption. Lost replies
allow only exact-result observation and completion of missing receipts. A
missing result after an attempt, legacy incomplete ownership, changed scope or
foreign daemon refuses, except Call Home initialization: its
`orch enable-call-home --raise-automation-warning` only sets values and enables
a module, so when its attempt matches the current intent, no Call Home
acceptance or configured-tunnel record exists and the needs-acceptance flag is
absent, or set with `call_home_agent` disabled, a mutating apply reissues it
once and then requires the flag and module as the first issue does;
observation never issues it, and a cleared flag or any acceptance record still
refuses. One version specialization adopts a native bootstrap
failure: when the verified host cephadm's bootstrap makes the IBM license call,
an exit whose private native output is exactly that call's zero-host refusal
continues the attempted seed in the same apply, after the exact owned-seed proof
the next apply would run
([catalog](../.agents/knowledge/version-specializations.md#ceph-bootstrap-license-call-continues));
every other bootstrap failure keeps its attempt record and remains a native
failure. Tunnel configuration writes a Call Home initialization fence
(`tunnel-config-attempt-call_home_initialize.json`) before its
`orch enable-call-home` and a separate acceptance fence before the Call Home
acceptance. Enrollment starts a separate durable fence and, once every host is
enrolled and labelled, runs `orch accept-license --image` for the selected
image at the end of the enroll step when the image carries the IBM license
contract and the provider's MON acceptance entry is absent, then requires that
entry. The enrolling stage continues enrollment and services while every other
host has no daemons (`ceph_core.py` `apply_tunnel_cluster`; `ceph_native.py`
`preparation_state`); once a non-bootstrap host carries a daemon, a cluster
that is not ready refuses `partial-cluster` (parked B659).
Candidate daemon inventory uses bounded direct filesystem, systemd and Podman
inspection instead of `cephadm ls`, whose inspected logging initialization can
create shared native files. It holds and re-proves protected directory and
metadata bindings, checks the exact original FSID, unit and immutable image,
and projects only existing public daemon fields. The admitted daemon
directory types are those of the daemon-type table below: MON, MGR, OSD, MDS,
crash and ceph-exporter. Beside the daemon
directories, the cluster directory may hold only cephadm's cache binary, its
`config`, `crash` and `removed` directories and the root-owned `selinux`
directory it makes for OSD and ceph-volume containers on an SELinux host. A
daemon of the exact FSID that cephadm has not yet written unit files for, whose
systemd instance is not yet installed, or whose removed-on-exit container does
not exist is reported not running rather than refused, and while its forking
unit is still starting or stopping its container may run or not. Foreign layouts,
replacement paths, contradictory state, a container without proved unit files
and failed reads refuse. RPM verification disables
verification scripts. These source and boundary proofs do not establish the
complete native touch closure or qualify actual guest/runtime observations;
that evidence remains [B79](milestones/m2.md#b79).

These are approved target requirements. Admission uses the
verified-tunnel path and authored artifact selection of the [storage API](api/storage.md): the image digest is
frozen before any native effect, and each host proves the native SSH class
route at host preparation, with correct-key forwarding and zero direct contact
for changed keys, unknown destinations, unavailable proxies and missing native
configuration. That probe does not prove the complete MGR lifecycle or the
guest sshd policy. Correct/wrong-key behavior through actual manager
connections, actual MGR UID/permissions/SELinux, reconnection,
restart/failover, interrupted-state recovery and removal remain
[B472/B79/B80](milestones/m2.md#m2b) promotion evidence. B476's accepted
minimal run is recorded in
[X94](milestones/delivered.md#x94--post-destroy-cluster-inspection); remaining
core/promotion gates stay on M2.

Host preparation supplies registry access through the invocation-local
credential broker defined by [storage](storage.md). Podman receives an empty
authfile and ephemeral mutual-TLS identity. Native bootstrap and provider
registry-login arguments never receive entitlement credentials. Core
authorization retains its separately owned protected guest registry record;
publication binds a reserved inode to a
durable frozen-scope intent before the native file becomes visible. Recovery
and removal require that binding and the exact descriptor identity; matching
bytes or an empty native inventory do not supply ownership. Ownership records
contain neither secret values nor secret-derived digests. A foreign authfile
or stored native registry credentials refuse. Actual guest/runtime use and
owned cleanup remain qualification requirements.

For example, this partial cluster declares core services using labels attached
to its already-declared hosts. It omits provider coordinates and topology, so
it is not a complete cluster document:

```yaml
spec:
  ceph:
    services:
      - service_type: mon
        placement:
          label: monitors
          count: 3

      - service_type: mgr
        placement:
          label: managers
          count: 2

      - service_type: osd
        service_id: data
        placement:
          label: storage
        spec:
          data_devices:
            paths: [/dev/disk/by-id/example-data]
```

Every selected host in this OSD example must prove that path's distinct,
eligible device identity. The label is not permission to discover new hosts
or disks after the authorization is frozen.

### Service identity and references

The service registry keys each entry by its qualified native canonical service
name: the native service type plus its service ID where that type uses one,
or the native singleton name. Thus `rgw.objects`, `nfs.files`,
`ingress.files`, `mon`, `mgr` and `grafana` have separate identities.
Do not impose global uniqueness on the bare `service_id`. Any authored native
`service_name` must agree with the identity computed by the provider schema.

`service_id` follows one table keyed by service type, with three states.
Forbidden: `mon` and `mgr`, the native singletons that cephadm refuses with an
ID. Native-required: `mds`, `rgw`, `ingress` and `nfs`; every row in this state is a type in cephadm's
`REQUIRES_SERVICE_ID` (container, ingress, iscsi, mds, nfs, nvmeof, rgw, smb,
[upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/python-common/ceph/deployment/service_spec.py#L804-L813)),
and later family slices add their rows. Bootwright-required: `osd`, which
cephadm admits with or without an ID; Bootwright requires one for a stable
qualified OSD service identity. Every ID uses `[A-Za-z0-9][A-Za-z0-9_.-]*`, and
an `mds` ID never starts with a digit, which cephadm's
[MDS specification](https://github.com/ceph/ceph/blob/v20.2.1/src/python-common/ceph/deployment/service_spec.py#L2833-L2859)
refuses. An `mds` service binds exactly one StorageFilesystem of the same
cluster whose name is its `service_id`, because cephadm sets `mds_join_fs` to
the service ID
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/cephadmservice.py#L950-L960));
an MDS service that no filesystem of its cluster binds refuses at admission.

The cluster block applies MON, MGR, OSD and MDS services in its one services
batch. Core observation admits a daemon only through its type's row in one
daemon-type table, which names the executor, the host rule, whether the daemon
counts toward readiness and the image rule. MON and MGR daemons map to their
hosts through their native identity; OSD, crash and ceph-exporter daemons run
on any frozen request host; an MDS daemon is admitted only on a host of the one
declared MDS service its native daemon ID
`<service_id>.<host short name>.<six lowercase letters>` names
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/module.py#L922-L966)).
An MDS service is ready when its placement count of daemons runs with the
frozen image; no active rank is required before its filesystem exists.

`serviceRef` is a scalar exact reference to this registry in the already
selected cluster. It is not a Bootwright object name, may contain native dots,
and is resolved through the registry rather than guessed by splitting an
arbitrary string. An absent, ambiguous or wrong-type target is a field error.
Native backend-service names similarly resolve declared owners and create
dependency edges; they cannot target a foreign service merely by name.

An explicitly declared service has one owner. Resource objects bind it rather
than copy its placement, TLS, ports or daemon configuration. For example, a
filesystem binds its MDS service; an object gateway binds RGW and optional
ingress. Native manager-generated SMB services remain derived children of the
SMB resource owner and cannot also appear as independently managed entries.
The [resource contract](storage-resources.md) owns those relationships.

### RGW, ingress and NFS records

Admission accepts `rgw`, `ingress` and `nfs` documents under closed records
whose members and spelling follow IBM's python3-ceph-common 20.2.1-405 ServiceSpec schema (the package the build pins); planning
still refuses each of them `lifecycle.unsupported` until its execution lane.

| Type | Admitted `spec` members |
| --- | --- |
| `rgw` | `rgw_realm`, `rgw_zonegroup`, `rgw_zone`, `rgw_frontend_port` (1-65535), `rgw_frontend_type`, `ssl`, `certificate_source`, `only_bind_port_on_networks`, `rgw_exit_timeout_secs` (at least 1) |
| `ingress` | required `backend_service` and `virtual_ip`; `frontend_port`, `monitor_port`, `first_virtual_router_id` (1-255), `virtual_interface_networks`, `health_check_interval`, `keepalive_only`, `enable_haproxy_protocol`, `haproxy_peer_communication_port`, `ssl`, `certificate_source` |
| `nfs` | `port`, `monitoring_port`, `virtual_ip`, `enable_haproxy_protocol` |

`rgw_exit_timeout_secs` starts at 1 because the schema stores only truthy
members and cephadm applies the option only when it is set
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/cephadmservice.py#L1323-L1324)).
Every other member of the schema refuses at its own field with an owner:

- Inline TLS material (`ssl_cert`, `ssl_key`) never appears in a native
  document; the RGW legacy `rgw_frontend_ssl_certificate` refuses because the
  schema migrates it into `ssl_cert` and `ssl_key`, so the stored service
  would not match the document. Both bind through
  [secret bindings](#secret-bindings).
- Parked by D160 (OD26): cephadm-signed certificates (`generate_cert`,
  `custom_sans`, `certificate_source: cephadm-signed`), RGW virtual-hosted
  style (`wildcard_enabled`, `zonegroup_hostnames`), NFSv3 (`enable_nfsv3`,
  `enable_nlm`), Kerberos and ID mapping (`idmap_conf`), NFS over TLS (`ssl`,
  `ssl_*`, `certificate_source`, `custom_sans`, `tls_*`), kmip (`kmip_*`) and
  NFS cluster QoS (`cluster_qos_*`).
- NFS RDMA (`enable_rdma`, `rdma_port`) refuses: Bootwright does not support
  it yet (D160, OD27).
- RGW multisite (`zone_endpoints`, `rgw_realm_token`, `update_endpoints`,
  `disable_multisite_sync_traffic`) belongs to
  [B483](milestones/m2.md#b483); `data_pool_attributes` belongs to
  StoragePool objects with role `rgw`.
- Ingress credentials (`monitor_user`, `monitor_password`,
  `keepalived_password`) stay derived confidential children cephadm
  generates; Bootwright never authors or reads them. One ingress serves one
  `virtual_ip`, so `virtual_ips_list` refuses.
- Members with no qualified effect contract refuse: RGW
  `rgw_frontend_extra_args`, `subcluster`, the counters caches, `d3n_cache`,
  `qat` and `concentrator*`; ingress keepalived transport, HAProxy statistics
  endpoint, backend TLS and cipher options; NFS `enable_virtual_server`,
  `colocation_ports`, `monitoring_networks`, `monitoring_ip_addrs` and
  `ip_addrs`.

RGW value rules: `certificate_source` admits only `inline` (`reference` is not
qualified) and only with `ssl: true`; `rgw_frontend_type` admits only `beast`;
`rgw_realm` and `rgw_zone` appear together
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/python-common/ceph/deployment/service_spec.py#L1338-L1342));
`placement.count_per_host` is at most 1; `only_bind_port_on_networks: true`
needs service `networks`; `ssl: true` needs the `/spec/ssl_cert` and
`/spec/ssl_key` bindings of one `tlsCertificate` Secret unless an ingress
fronts the service.

NFS value rules from the same schema: `placement.count_per_host` refuses, and
`virtual_ip` refuses together with service `networks`. `virtual_ip` is a bare
address without a prefix length, zone or IPv4-mapped form, because cephadm
binds the text verbatim
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/nfs.py#L105-L107)).

Ingress value rules: `health_check_interval` is digits followed by `s`, `m`
or `h`, as IBM's `IngressSpec.validate` requires; `virtual_ip` is
`address/prefix` without a zone or IPv4-mapped form; each
`virtual_interface_networks` entry is a network prefix without host bits,
because cephadm compares it with the host's network keys as text
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/ingress.py#L322-L325)),
and lies inside the declared public CIDRs; `keepalive_only` and
`enable_haproxy_protocol` exclude each other. Unless `keepalive_only: true`,
`frontend_port` and `monitor_port` are required, and the frontend, monitor
and peer ports differ (an NFS ingress reserves 1024 for HAProxy peers unless
`haproxy_peer_communication_port` names another). The ingress placement
selects at least two eligible hosts.

The ingress record is a per-backend arm table (program C8). `backend_service`
resolves by exact qualified name, never by splitting on `.`, to one declared
service of the same cluster whose type has an arm; one ingress serves each
backend service in this build. An arm-only member that the resolved arm does
not list refuses at its field.

| Arm | Arm-only members | Meaning |
| --- | --- | --- |
| `rgw` | `ssl`, `certificate_source` | TLS terminates at the ingress through the `/spec/ssl_cert` and `/spec/ssl_key` [bindings](#secret-bindings), with the RGW `certificate_source` rules |
| `nfs` | `keepalive_only`, `enable_haproxy_protocol`, `haproxy_peer_communication_port` | the NFS mode below |

Other backends wait for their owning item (B479 adds the management
gateway's arm). An RGW service behind an ingress selects at least two
eligible hosts, its `placement.count` is absent or at least 2, and it keeps
`ssl` false: TLS terminates at the ingress (IBM's high-availability guide for
the Ceph Object Gateway).

An NFS service's mode follows the ingress that fronts it:

| Mode | Ingress | NFS service |
| --- | --- | --- |
| direct | none | no `virtual_ip`, no `enable_haproxy_protocol` |
| keepalive-only | `keepalive_only: true` | `placement.count: 1` over at least two eligible hosts; `virtual_ip` required and equal to the ingress virtual IP address; no `enable_haproxy_protocol` |
| haproxy-protocol | `enable_haproxy_protocol: true` | `enable_haproxy_protocol: true`; `placement.count` present and below the eligible host count; `virtual_ip` optional and, when present, equal to the ingress virtual IP address |
| haproxy-standard | neither flag | refuses at the ingress `enable_haproxy_protocol` |

HAProxy without the PROXY protocol hides client addresses, so IP level
restrictions on NFS exports would not function
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/doc/mgr/nfs.rst#L111-L128)).
The native NFS module sets the NFS `virtual_ip` in both ingress modes
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/nfs/cluster.py#L77-L97)),
and cephadm adds it to Ganesha's HAProxy hosts
([upstream](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/nfs.py#L133-L136)).

Every virtual IP comparison uses parsed addresses, and later renderings use
the canonical form. An ingress `virtual_ip` lies inside a declared public CIDR
and carries that CIDR's prefix length; it is neither the CIDR's network
address nor, for IPv4 below `/31`, its broadcast address; and its address
equals no declared Machine address and no other ingress `virtual_ip` of any
StorageCluster. Diagnostics name the field, never the authored address.

The `nfs` manager module is derived from the declared NFS services (D159
clause 9): `spec.ceph.mgrModules` refuses it at its entry, and Bootwright
enables it for a declared `nfs` service.

## Hosts, placement and CRUSH

Topology nodes retain `name`, `machineRef`, optional `fqdn`, `site` and
`labels`, plus a native `location` map for qualified CRUSH bucket types and
names. `location` retains the native HostSpec spelling and string values.
Host identity and address are derived from the proved Machine/address binding;
the registered hostname must agree with the installed host's native hostname.
The generated native HostSpec is visible as non-secret plan intent. A raw
`service_type: host` entry in `services` refuses because it would create a
second infrastructure owner.

Native [HostSpec location](https://docs.ceph.com/en/tentacle/cephadm/host-management/#setting-the-initial-crush-location-of-host)
describes initial hierarchy placement, not a live hierarchy-edit API. Native
`host` identity is generated, not overridden through `location`. Each
declared site resolves through Environment and the Machine. When a site maps
to the configured CRUSH failure-domain bucket, `location` must agree with that
mapping; it cannot put the host in a second site. B81/B486 qualify hierarchy,
capacity and removal of owned buckets without removing foreign parents.

`topology.roles`, per-node `devices`/`osd` and `topology.osdDrivegroups` are
retired by this migration. Native service placement is the authority for
daemon membership. Node labels retain their native meaning; a label named
after a daemon is not a second role declaration.
Reserved labels with orchestration effects require explicit qualification;
a label cannot silently disable owned scheduling or export administrative keys.

Placement preserves the provider's qualified native grammar, including
`hosts`, `label`, `host_pattern`, `count` and `count_per_host`. Explicit host
tokens resolve to declared registered hostnames. When a native host token
also carries a network/address or daemon identifier, that data must agree
with the proved host/network and the selected service. Pattern matching uses
the registered native hostname, not an unrelated Machine display name.
As in cephadm, `count_per_host` requires `hosts`, `label` or `host_pattern`,
and cannot combine with host tokens that carry a network or daemon name.

Freeze the eligible host set, provider placement constraints and cardinality,
not the instantaneous daemon-to-host assignment. Native placement may move
or restart daemons within that owned set as its qualified HA mechanism needs.
It may not enroll additional hosts, consume additional disks, or escape a
site restriction. The runtime projection bounds selectors to eligible hosts
while retaining native count and placement semantics. If a native mechanism
cannot express or enforce that boundary, refuse that combination with the
unmet constraint; do not silently change its scheduling meaning.

Empty resolved placement, impossible counts, unsupported combinations of
selectors, or missing failure-domain capacity refuse before effects. A host
outage changes observed availability, not desired membership or authorization.
Stretch rules apply to resolved placement, including MON-only tiebreaker
constraints and service-specific data-site counts.

## Secret bindings

Native documents contain no secret bytes. `ceph.serviceBindings` is an
optional set keyed by `serviceRef`; every entry binds exactly one declared
service and contains a nonempty `secrets` collection. Each `secrets` entry is
the same closed pair used by native resource wrappers:

| Field | Contract |
| --- | --- |
| `path` | Required nonempty JSON Pointer to one release-qualified confidential leaf, relative to the native document root. Exact member names, `~0`/`~1` escapes and qualified array indices only; no wildcards, append token, expressions or parent/root replacement. |
| `secretRef` | Required scalar reference to a declared `Secret` with a type accepted by that target leaf. No literal value, file source, arbitrary part selector, template or transformation expression. |

The collection is a set keyed by path. Duplicate paths, parent/child overlap,
an unsupported path, wrong Secret type or an authored value at the same leaf
refuse. Binding locations are not a generic patch facility. A path may name
a leaf missing from the public payload; the qualified schema determines its
required parents, native type and confidential serialization. Arrays must
already have the addressed structural element; bindings cannot grow arrays or
choose resource identities. Whole-secret documents are not injected into
arbitrary maps.

For a native resource wrapper, `spec.secrets` uses this same pair and resolves
paths relative to that resource's `native` payload. No `serviceRef` is needed
there because the resource envelope already identifies the owner.

This partial cluster binds Grafana's initial password without changing the
native placement or authentication field names:

```yaml
spec:
  ceph:
    services:
      - service_type: grafana
        placement:
          hosts: [host-a]
        spec:
          anonymous_access: false

    serviceBindings:
      - serviceRef: grafana
        secrets:
          - path: /spec/initial_admin_password
            secretRef: grafana-admin
```

The [native Grafana leaf](https://docs.ceph.com/en/tentacle/cephadm/services/monitoring/#setting-the-initial-admin-password)
receives the validated UTF-8 value of an `opaque` or `token` Secret. Invalid
encoding or provider restrictions refuse before invocation without echoing
the material. The cluster's core services and the Secret declaration are
omitted from this partial example.

The binding registry is fixed per qualified service/resource schema. It names
accepted Secret types, required parts, native encoding, byte bounds and
related-field requirements. For example, the qualified RGW leaves
`/spec/ssl_cert` (the certificate part, chain allowed) and `/spec/ssl_key`
(the private-key part) consume one `tlsCertificate` Secret together and take
effect only with `ssl: true`; an ingress whose backend is an RGW service
takes the same two leaves under the same rules. The legacy
[`/spec/rgw_frontend_ssl_certificate`](https://docs.ceph.com/en/tentacle/cephadm/services/rgw/#setting-up-https)
leaf refuses because IBM's python3-ceph-common 20.2.1-405 ServiceSpec schema migrates it into those two leaves. NFS services
and the ingress that fronts one take no bindings: NFS over TLS and kmip are
parked by D160 (OD26). The adapter
never guesses from a field's suffix or permits an operator
to splice arbitrary secrets into command arguments or custom config.

Admission validates descriptor/reference types and the native payload with
the declared binding slots. It does not open the secret store. Required native
secret fields are satisfied only by compatible bindings, not by skipping
native validation. Material is bound to one version during authorized
execution and injected only into private, bounded runtime input; serialized
effective YAML, ordinary projection, logs and evidence contain descriptors,
never secret bytes or their hashes. Native tools' secret-bearing responses
are filtered at their output boundary and captured through produced custody
where applicable.

Native generated credentials/certificates are derived confidential outputs,
not an excuse to invent an authored Secret value or a fresh identity on retry.
Each consuming item qualifies their custody and removal. A generated option
conflicting with an authored Secret binding refuses rather than choosing one.
Public access/export remains [B489](milestones/m2.md#b489)'s explicit boundary.

For a generated pool-scoped client, retain the original confidential key and
capabilities before native authorization. Bind its protected file to the
original operation, FSID, bootstrap host and native pool ID with
secret-independent ownership and descriptor metadata. Replays and protected
recapture prove that original file before reading it, then compare the native
principal with the retained material privately. A lost authorization response
reuses the original identity; a matching name, replacement file or key digest
does not supply ownership. Native authorization must create or verify that
exact identity, without overwriting a foreign principal.

## Native preservation, defaults and diagnostics

The common [input grammar](api.md#yaml-streams-and-decoding) still applies,
including the null, alias and duplicate-key prohibitions. No implicit file
include, environment expansion, shell evaluation or template execution is
introduced. A native field that deliberately accepts structured values keeps
those values and types under the pinned native schema.

Compilation preserves the authored native document and outputs deterministic
effective desired state with binding descriptors. The lifecycle separately
freezes its execution projection: resolved eligible hosts, proved devices,
reference-derived names, immutable images, qualified defaults and protected
material slots. It records why any field differs; it never silently drops or
overrides authored data. The effective service list keeps authored order, so
a diagnostic names the authored service; the execution projection orders
services MON, MGR, OSD, then by native service name, so authored order never
changes a frozen request. Binding sets sort by their canonical key; native
ordered lists retain order. Complete-graph validation is independent of
file/document order.

Resolve defaults that affect topology, protection, ports, VRRP ranges,
security, native-generated resources or lifecycle before graph conflicts and
capacity checks. The native schema owns ordinary native defaults; the release
record freezes the effects they imply. Omission, `false`, explicit zero and
an empty collection retain their field-specific meanings; there is no global
"zero means absent" rule. B494 closes the review's effective-value defects.

For every qualified field, B474's ledger identifies one of native preservation,
reference expansion, frozen default, explicit effect restriction or unsupported
capability. A binding-derived native name may be omitted by the operator; if
copied from an existing native file, it must agree with the reference. Resource
bindings supply identity rather than adding an arbitrary override layer.

Unknown native fields, unsupported service types, inline credentials,
conflicting ownership, `unmanaged` owned services and unsafe container/mount
options refuse before registration. Native-valid but unsupported input gets
a different explanation from a schema error. Diagnostics name the cluster or
resource, field/binding path, unmet requirement and correction without echoing
untrusted scalar values. `validate` remains pure; lifecycle support refusal
does not masquerade as a successful deployment.

## Migration and implementation sequence

The migration stays in `bootwright.io/v1alpha1`; no alias version or dual
authoring mode is introduced. Existing input is not silently reinterpreted.
The B474 delivering change updates closed schema, aggregate admission,
effective rendering, diagnostics, runnable examples and tests together, and
rejects retired fields with their replacement path. Operator files are never
rewritten automatically. Frozen non-storage operations keep their existing
compatibility contract; this design promises no migration of runtime records.

| Current authoring | Target migration |
| --- | --- |
| `services[].serviceType`, `serviceID`, custom placement and inner `spec` | Full native ServiceSpec; `service_type`, `service_id` and native placement. |
| `topology.roles` and role-derived placement | Explicit MON/MGR and other native services; retain host labels, Machine/site binding and verified topology. |
| Node `devices`/`osd`, `osdDrivegroups` | Native OSD ServiceSpec/DriveGroupSpec, retaining proved data/DB/WAL allocations and encryption intent. |
| `monitoring` helpers | Explicit native monitoring services; selected retention, networks and ports stay in native fields, passwords use bindings. Both old `enabled: true` and old omission that selected the native stack expand into visible qualified service entries; old `enabled: false` creates none. |
| `mgmtGateway` and nested OAuth/ingress | Native management/OAuth/ingress services plus required endpoint/resource and Secret bindings. Ingress is optional where the provider permits direct service access. |
| Filesystem MDS placement/service helpers | Filesystem rank/standby policy stays resource-owned; one native MDS service carries daemon placement and options. |
| RGW service/ingress helpers | Logical object gateway/resource ownership remains; native RGW/ingress services own daemon config and placement. |
| NFS service plus `exports[]` | Native NFS/ingress services and individually named StorageNFSExport resources with native JSON projection. |
| Global bare service-ID uniqueness | Uniqueness by qualified native service name and checked reference type. |

B474 delivers the common migration and shared binding/admission machinery in
X52. It does not wait for later service lifecycles to exist. B80 is the first
core consumer; B81/B477 to B483/B489/B492 add their qualified resource/service
fields, projections and effects without implementing another native-input
path. An admitted family whose lifecycle is not delivered still refuses
execution before registration. New resource kinds are registered with their
owning schema change, not accepted as arbitrary generic objects in advance.

Before each consumer's promotion, freeze the applicable native schema,
binding registry, default ledger, immutable dependencies, ownership and
readiness/removal evidence. In-tree gates include native projection fixtures,
equivalent-intent determinism, old-field remedies, wrong-cluster/service/Secret
references, inline-secret refusal, field collision and no-effect tests.
OSD tests additionally prove finite device authorization; HA acceptance proves
rescheduling inside the owned candidate set. Operator qualification follows
the existing M2b to M2e profiles.
