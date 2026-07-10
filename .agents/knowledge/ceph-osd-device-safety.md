# Ceph OSD device safety: markers, gates, reclaim, and the readiness poll

**Constraint:** Every device gate covers only explicitly named block-device
paths: the `devices` shorthand, the per-host drivegroup's explicit data/db/wal
paths, and a covering fleet `osdDrivegroups[]` entry's explicit paths —
deduplicated, shorthand first. Filter/`all: true` selections are resolved
on-host by ceph-volume and are not statically enumerable, so a host authored
purely by filter gets **no** install/destroy device gate; only ceph-volume's
own refusal to consume non-empty devices backs it.

**Constraint:** Each apply stamps an on-node OSD ownership marker
(`bootwright_ceph_osd_marker_path`) recording the exact devices Bootwright
claimed as OSDs, BEFORE bootstrap. At install, a signature on a declared device
normally means foreign data and fails closed; the one exception is a marker for
THIS cluster/node/device, which proves the signatures are a prior apply's
ceph-volume LVM — cephadm then reconciles those OSDs in place (how an owned,
half-converged cluster stays recoverable by re-apply). No marker, another
cluster's marker, or an unrecorded device still fails closed.

**Constraint:** Destroy wipes only devices in that recorded set — a declared
device missing from it was added out of band (never an OSD here) and may hold
foreign data. The marker is trusted only when it is a Bootwright marker for
THIS cluster and node; stale/foreign JSON at the path must not seed the
wipe-allowlist. With no valid marker, destroy falls back to the shape and
mount checks rather than failing a legitimate teardown.

**Constraint:** Destroy device gates, in order: (1) probe each declared device
for active mounts and fail closed — the device-name regex proves shape, not
that a kernel-reordered `/dev/sdX` is safe to zap; (2) a device lsblk reports
as "not a block device" is skipped (config drift must not block teardown), any
other probe failure stays fatal; (3) devices are re-probed immediately before
the irreversible wipe to shrink the mounted-after-first-check window.

**Constraint:** `sgdisk` lives in the `gdisk` package, which the Ceph install
never pulls in (the install-time device check uses `wipefs` only, and cephadm
runs ceph-volume's sgdisk inside its container). Without it the destroy zap
dies with `No such file or directory: sgdisk`. The role installs it on demand,
recorded via `ownership_record` so teardown uninstalls it again, gated on the
present-device set so mon-only nodes never install it. A failed zap fails
closed — a half-cleared partition table can corrupt the next provision.

**Constraint:** Removing a device from the declared list is a REMOVAL, not a
reconcile: cephadm never auto-removes an OSD, so dropping it from the
drivegroup spec would silently orphan a running OSD. Apply refuses when the
recorded device is still present AND still carries data, directing the operator
to drain it first; a removed device that is absent or already wiped passes and
simply stops being recorded.

**Constraint:** `apply --reclaim-devices` recovers owned OSD disks whose
on-node marker a managed-OS reinstall erased: the disks still carry this
cluster's ceph LVM but the empty-device gate would refuse them. It wipes
exactly the operator-named devices — gated on each being a declared OSD device
of a controller-owned cluster and not mounted or in use — so the gate then sees
them empty and cephadm re-creates the OSDs.

**Constraint:** Two validators protect the OS disk: a Ceph node must not list
its root disk (`rootDeviceHints.deviceName`) among OSD data/db/wal paths
(cephadm would ceph-volume it into an OSD, wiping the installed OS), and a
managed-OS OSD node must declare `rootDeviceHints` at all — with none resolved
the kickstart falls back to an unconditional `clearpart --all`, which also
wipes the data disks on the next (re)install. Any osd-role host counts: the
drivegroup and fleet shapes leave `devices` empty, so a lean
`len(devices) > 0` check missed them and their OSD disks were silently wiped.

**Symptom (zero-OSD green apply):** `ceph orch apply` registers the OSD
drivegroups with the mgr and returns immediately; cephadm creates OSDs
asynchronously through ceph-volume. Without a readiness poll, a cluster that
ends up with zero or fewer OSDs than declared — a busy, undersized, or
foreign-signed device, a ceph-volume refusal, an OSD image-pull or SELinux
failure — reports a green apply, and because the desired hash is unchanged it
reads `match` on every later re-apply.

**Fix:** The apply polls until the managed drivegroups' OSDs are `in`, then
fails closed with an actionable message. The expectation is rendered from the
declared topology (`osdReadiness`): `exact` (every managed selection names
explicit devices; count is exact, multiplied by `osdsPerDevice`), `atLeastOne`
(a filter/all selection makes the count host-resolved; only `> 0` is
assertable), or `skip` (no managed OSD service creates OSDs). It gates on
`num_in_osds`, not `num_up_osds`, so a transiently-down OSD on a benign
re-apply is not a false fail, and it runs before the topology operations so
pools/CRUSH/EC never land on a zero-OSD cluster. Observed counts and health
are persisted so the controller classifies a silently degraded cluster as
drift instead of a blind hash match.
