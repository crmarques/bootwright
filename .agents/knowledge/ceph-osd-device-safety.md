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

**Symptom (zero-or-partial-OSD green apply):** `ceph orch apply` registers the OSD
drivegroups with the mgr and returns immediately; cephadm creates OSDs
asynchronously through ceph-volume. Without a readiness poll, a cluster that
ends up with zero or fewer OSDs than declared — a busy, undersized, or
foreign-signed device, a ceph-volume refusal, an OSD image-pull or SELinux
failure — reports a green apply, and because the desired hash is unchanged it
reads `match` on every later re-apply. A cluster-wide `num_in_osds > 0` check
also masks a dynamically selected host with no OSD when another host succeeds.

**Fix:** The apply polls until the managed drivegroups' OSDs are `in`, then
fails closed with an actionable message. The expectation is rendered from the
declared topology (`osdReadiness`): `exact` (every managed selection names
explicit devices; count is exact, multiplied by `osdsPerDevice`), `atLeastOne`
(a filter/all selection makes the count host-resolved, so every declared
dynamic host must have at least one OSD with positive CRUSH reweight), or
`skip` (no managed OSD service creates OSDs). The exact path gates on
`num_in_osds`, not `num_up_osds`, while the dynamic path requires the static
count plus at least one OSD per dynamic host and checks those host buckets in
`ceph osd tree`; a transiently-down but still-in OSD on a benign re-apply is not
a false fail. `singleHostDefaults` raises either expectation to at least two in
OSDs because cephadm's single-host pool size is two; a statically countable
selection below two is rejected before apply, while a dynamic or unmanaged
selection must prove the minimum at runtime. The check runs before topology
operations so pools/CRUSH/EC never land on a zero- or partial-OSD cluster.
Observed counts and health are persisted in the on-host ownership record for
diagnosis.
The readiness retry tasks terminate at their configured attempt budget and
defer failure to explicit assertions. Letting Ansible's `until` exhaust marks
the task failed before the device, service, and CRUSH diagnostics can run even
when the command task has `failed_when: false`.
After all late service specs and object operations, apply performs a final
health poll and refuses to record success while the cluster is unreachable or
`HEALTH_ERR`; `HEALTH_WARN` remains acceptable because expected operational
warnings do not make the desired topology unusable.

**Constraint (all-devices OSD auto-reclaim):** A host authored by filter renders
`devices: []`, so the Pass-1 static gate/reclaim/marker no-op for it and a
reinstall onto disks carrying a prior Ceph signature fails at the readiness poll
with zero OSDs. `apply --override --allow-destroy` closes that FOR `all: true`
hosts ONLY: the CLI emits `bootwright_ceph_filter_reclaim_clusters` for in-scope
clusters that have a host whose OSD selection is `data_devices.all=true`
(`topology.ClusterHasAllDevicesOSDHost`, mirrored per host by the rendered
`osdReclaimAll` flag) — never under `--override` alone, `--yes` alone, or
dry-run. Narrowing filters (`model`/`size`/`rotational`/`vendor`/`limit`) are
DELIBERATELY excluded: the ansible layer only knows the boolean flag, not the
predicate (which lives solely in `core-services.yaml`), so auto-zapping every
unavailable disk on a narrowing-filter host would wipe disks the filter never
targets; those hosts fall back to the readiness diagnostic + manual clean. On
`all: true` every non-OS disk genuinely is a target, so wiping is in-scope.
`osd_reclaim.yml` runs on the SEED in Pass 2, inserted in `service_specs.yml`
BETWEEN the host-spec apply (which adds every host to the orchestrator) and the
OSD (core-services) apply, so ceph resolves each host's devices for us (`ceph
orch device ls --format json --refresh`, retried until every declared host
reports inventory) and the zap lands before the OSD spec, on clean disks cephadm
then consumes within the readiness wait.

**Constraint (auto-reclaim safety, all fail-closed):** The reclaim is skipped
entirely unless BOTH `ceph orch device ls` and `ceph osd metadata` return rc 0 —
without a trustworthy live-OSD list a raw (unmounted) bluestore OSD is
indistinguishable from a dirty disk, so an unreadable input wipes nothing. A
candidate is a device the host reports `available: false` whose path is excluded
by NEITHER of two independent live-OSD gates: (1) the device's OWN `osd_ids`
field from `ceph orch device ls` is non-empty (authoritative and
path-format-agnostic — this is what protects a live OSD on a `/dev/mapper`,
by-id, or by-path device, where basename reconstruction would silently fail
open); (2) its kernel path is in the `/dev/<basename>` set rebuilt from `ceph osd
metadata` (a secondary guard for any device the inventory does not mark). Each
survivor is then mount-probed with `lsblk` DELEGATED to its owning host
(`ignore_unreachable: true` so a transiently-unreachable node fails the probe and
is skipped rather than aborting the `any_errors_fatal` play); a non-empty
mountpoint/swap OR any probe failure excludes it (protects the OS/root disk,
in-use data disks, and unreachable/failed probes — never a false zap). Survivors
are wiped with `ceph orch device zap <host> <path> --force`. Static-path hosts,
non-`all:true` clusters, and unauthorized runs are never touched. KNOWN
LIMITATION: a co-resident FOREIGN live Ceph cluster's OSDs are absent from THIS
cluster's `osd_ids`/`ceph osd metadata` and, being raw bluestore, unmounted — so
under `all: true` + `--override --allow-destroy` their disks would be zapped.
`all: true` asserts every disk on the host belongs to this cluster; the CLI
warning states this explicitly (do not use `all: true` on a host shared with
another Ceph cluster or holding data to keep). The unreliable-diagnostic
counterpart: the readiness-failure device dump now runs `ceph orch device ls
--wide --refresh` with a short retry so reject reasons are populated, not empty.
