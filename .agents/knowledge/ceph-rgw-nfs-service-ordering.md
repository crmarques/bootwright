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

**Constraint:** Each declared export renders as an idempotent
`ceph nfs export create cephfs|rgw` in the object-gateway phase (after the nfs
service registers). The role probes `ceph nfs export ls <serviceID>` keyed by
`<serviceID>|<pseudo>`. Exports are additive-only: a removed export keeps
running until removed by hand.

**When it bites:** The `<serviceID>|<pseudo>` idempotency key contains `|`. In
the generated `apply.sh` it must be single-quoted or the shell parses it as a
pipe, which under `set -euo pipefail` aborts the whole script. Script
generation therefore quotes with an allowlist (`QuoteWords`), not a display
denylist (`Quote`) that misses shell-active characters.

**Constraint:** In the operations loop, the per-kind idempotency probe
register (`bootwright_ceph_rgw_user_info`) is registered only for `rgw-user`
ops and must be reset at each classify step, or it persists across loop
iterations and poisons later idempotency decisions.
