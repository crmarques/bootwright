# Ceph cluster ownership: the 3-factor gate, apply modes, and destroy scoping

**Semantics:** Ownership of a cephadm cluster is 3-factor: the on-disk
`/etc/ceph/ceph.conf` carries an fsid AND a Bootwright storage-cluster
ownership record exists for this seed AND this host is the declared
`seedHost`; the host-local marker must be present and its fsid must agree with
the conf fsid. The record is the load-bearing proof Bootwright created the
cluster (a foreign or co-resident cluster has none); the marker fsid is an
independent consistency check. A seed with no ceph.conf at all has nothing to
protect and is treated as owned.

**Semantics:** `destroy --recover-ceph-ownership
<StorageCluster>=<fsid>[,...]` repairs only the host-local marker. Go first
requires a selected declared managed cluster and its context owner record for
the declared seed. On the seed, Ansible independently requires the same record
path and an exact supplied-fsid match against `/etc/ceph/ceph.conf`, writes the
normal `0600` marker, then re-reads it through the unchanged destroy ownership
decision. It never creates a missing controller record, uses live `ceph fsid`
as authorization, or relaxes OSD device gates.

**Semantics:** The fsid is always read from the on-disk conf, never live
`ceph fsid`, so a DOWN owned cluster stays classifiable, re-stampable, and
removable. Teardown resolves the live fsid when the cluster answers, else the
conf fsid — `cephadm rm-cluster` operates on local daemons/state by fsid, not
on the live cluster.

**Semantics:** `cephadm rm-cluster` is host-local: run on the seed it stops and
removes only the SEED's daemons/state. Non-seed hosts keep their
mon/mgr/mds/osd systemd units and podman containers running, so wiping their
devices while daemons still hold them leaves restart-looping units and stale
device-mapper maps. Destroy runs rm-cluster per non-seed host too — keyed on
the same fsid and the seed's proven ownership — before any device wipe, and a
real rm-cluster failure fails closed (never fall through to wiping devices with
daemons still up). The destroy play runs `any_errors_fatal`, so a seed
ownership refusal aborts before any node wipes its OSD devices.

**Semantics:** Apply-mode contract (host-side backstop to the Go preflight):
`create` refuses any pre-existing cluster on the seed (greenfield only);
`continue` and `override` act only on a Bootwright-owned cluster and refuse a
foreign/co-resident one. An owned cluster proceeds — override to the
zap-and-rebuild, continue to the idempotent bootstrap skip and re-stamp.

**Semantics:** Ownership is pre-recorded BEFORE the non-idempotent
`cephadm bootstrap` runs. A crash that writes `/etc/ceph/ceph.conf` but aborts
before the post-bootstrap marker still leaves the controller-side record.
Reachable markerless clusters fail closed on normal apply; destroy recovery
requires the operator-confirmed fsid path above. An unreachable incomplete
bootstrap remains eligible for the separately authorized
`apply --converge-drifted` rebuild. Without the controller record, neither
recovery path adopts the cluster; restore the record or tear the cluster down
manually. Successful apply refreshes the fsid marker and enriched ownership
record.

**Semantics:** `--converge-drifted` does not always wipe. The controller classifies a
cluster whose only drift is an OSD-device add as reconcilable in place and
names it in `bootwright_ceph_reconcilable_only_clusters`; for such a cluster
override must NOT zap — `ceph orch apply` adds the new OSD additively. An
absent/empty var keeps the zap-and-rebuild behavior for structural drift.

**Semantics:** The destructive rm-cluster additionally requires a positive
rebuild-authorization token: the controller names ONLY structurally drifted
clusters in `bootwright_ceph_rebuild_authorized_clusters`, and the wipe runs
only for clusters in that list. A no-drift MATCH and a reconcilable-only
device add are absent from it. The var is fail-safe: an absent or empty token
authorizes NO wipe, so a stale bundle can only under-authorize (skip a
rebuild), never over-authorize. This gate is independent of, and additional
to, the ownership and reconcilable-only checks.

**Semantics:** When destroy skipped a node as unreachable
(`--skip-unreachable`), the cluster's controller-side ownership record is KEPT
and re-stamped as partially destroyed from the destroy-result file — the
cluster is only partially torn down and must not read as fully gone. A normal
teardown (no skipped nodes) removes the record.
