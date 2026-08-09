# Ceph RGW realm/zone ordering and NFS export semantics

**Constraint:** cephadm does not create the RGW realm topology, and the rgw
daemons need it before they start. The realm/zonegroup/zone creates (and the
per-RGW `ceph config set client.rgw.<id>` values) render in the storage phase,
BEFORE the rgw service spec applies.

**Constraint:** Realms, zonegroups, and zones are each created once, keyed by
name. A second gateway that shares a realm but declares its own zonegroup/zone
must still get them created — the first gateway's realm-create must not swallow
them. Only the FIRST zonegroup/zone of a realm is stamped
`--master --default`.

**Constraint:** A single-site realm only serves after its period is committed.
The period is committed ONCE per realm (`radosgw-admin period update --commit
--rgw-realm=<realm>`), AFTER every zonegroup/zone create for that realm, so a
zonegroup added by a later gateway is part of the committed period and its rgw
daemons reference a zone that exists.

**Constraint:** The standalone gateway's admin-user op
(`radosgw-admin user create --uid bootwright-<gw>-admin`) has no consumer for
the keys, so it captures nothing — but `user create`/`user info` output still
contains `keys[].access_key`/`secret_key`, so the op carries an explicit
`no_log` flag the role honors independently of capture, and the generated
native-CLI script discards its stdout (`bw_guarded_quiet`).

**Constraint:** For NFS, cephadm auto-provisions the backing `.nfs` pool
(Squid), so the service spec needs only placement — no pool/namespace. There
is no `nfs` topology role, so NFS placement is always authored explicitly
(resolved with role `""`, same as passthrough services).

**Constraint:** NFS-Ganesha binds every address on its host, so a service that
is fronted by an ingress cannot also hold the standard NFS port: haproxy needs
`2049` free on the VIP, and cephadm refuses to deploy a daemon whose port is
already taken (`TCP Port(s) '2049' required for nfs already in use`). The daemon
port is therefore modeled as `spec.ceph.port`, defaulted by normalize to `2049`
for a directly mounted service and `12049` when the object declares an ingress;
the ingress `frontend_port` stays `2049` so clients always mount the VIP on the
port they expect. Authoring `2049` alongside an ingress is a validation error,
not a silent misconfiguration. There is no escape hatch through the passthrough:
`nfs` is in the reserved service-type map, so `spec.ceph.services[]` cannot
re-declare the service with a different port.

**Constraint:** Each declared export renders as an idempotent
`ceph nfs export apply <serviceID> -i -` with the export JSON on stdin, in the
object-gateway phase; `apply` is a pure upsert and needs no probe. The Ansible
path applies the NFS service spec and proves its daemon count before this phase;
the generated native-CLI bundle likewise applies the late service spec before
the export documents. Exports are additive-only: a removed export, a renamed
pseudo, and a deleted `StorageNFSExport` all keep running until removed by hand.

**Constraint:** In the operations loop, the per-kind idempotency probe
register (`bootwright_ceph_rgw_user_info`) is registered only for `rgw-user`
ops and must be reset at each classify step, or it persists across loop
iterations and poisons later idempotency decisions.
