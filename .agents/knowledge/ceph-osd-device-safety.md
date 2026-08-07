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
of a selected cluster and not mounted or in use — so the gate then sees
them empty and cephadm re-creates the OSDs. Without a token the reclaim acts
only on a controller-owned cluster; `--authorize unowned-devices` extends it to
a selected cluster the controller holds no ownership record for (ADR 0034
amendment 2026-08-07). That matters because a successful destroy RELEASES the
ownership record by design: before the amendment, every post-destroy reclaim
silently no-oped ("no device will be reclaimed"), and the device-empty gate
then named the exact `--reclaim-devices --authorize data-loss,unowned-devices`
command that had just no-oped — an unbreakable loop first hit on prd
(ceph-prd-01 seed srv4203, 2026-08-07). The CLI passes the eligible set as
`bootwright_ceph_reclaim_clusters` (renamed from
`bootwright_ceph_owned_clusters`), and `reclaimEligibleClusters` in
`internal/cli/scope_apply_destructive.go` is the one predicate the resolve
path, the destructive-set forecast, and the dry-run gate forecast all read.

**Symptom (`Refusing to reclaim <dev>: it could not be probed or is mounted/in
use ()` — empty parentheses):** the reclaim mount gate asserted `item.rc == 0`
AND an empty mountpoint list in one assertion, so `lsblk: <dev>: not a block
device` (rc 32) surfaced as the mount refusal with nothing between the parens.
The device is not mounted; it does not exist. The empty parenthesis is the
tell. Root cause is almost always a declared device list that does not match the
hardware — NVMe namespace numbering decides whether the data disks run
`nvme0n1`–`nvme3n1` or `nvme1n1`–`nvme4n1`, and whether the root disk is an NVMe
at all.

**Fix:** the reclaim path now classifies presence exactly as the destroy path
always has (`not a block device|No such file or directory` → absent), and the
three outcomes are separate tasks: absent is **skipped and reported** as a
declaration that does not match the hardware (nothing to wipe, and the OSD
readiness shortfall is the real diagnosis), any other probe failure is its own
fatal refusal, and the mount refusal keeps only the mount condition. The wipe,
zap, and gdisk tasks loop `bootwright_ceph_reclaim_present`, not the unfiltered
reclaim set, so an absent device is not a wipe failure either.

**Constraint (orphan vs live OSD — the holders gate cannot tell, so it must
say which case it is in):** an unmounted whole disk carrying LVM holders is the
bluestore signature, and a **live** OSD and an **orphan** left by a destroyed
cluster look identical to any device probe. The old refusal assumed live and
printed a drain-first remedy (`ceph osd tree`, `ceph orch osd rm`) that is
unactionable when `/var/lib/ceph` is gone — there is no cluster to drain from,
and no in-product path existed for a statically named selection. The gate now
stats `/var/lib/ceph` and branches the message on it, and ADR 0034's
`--authorize unowned-devices` (extra var
`bootwright_ceph_authorize_unowned_devices`, accepted by BOTH verbs) relaxes the
ownership half of device safety. The physical half stays closed to every token:
mounted/in-use and unprobeable still fail closed. On apply the gate is reachable
only under `--reclaim-devices`; the wipe itself still needs `data-loss`, so the
operator passes both.

**Constraint (an authorized reclaim must take the LVM stack down, not just
`wipefs` it):** `wipefs --all` clears the PV label but leaves active LVs mapped,
and ceph-volume rejects a device with holders regardless of its signatures — so
the token would have cleared the gate and still produced zero OSDs. The reclaim
runs `vgchange --activate n` → `vgremove --force --yes` → `pvremove --force
--yes` for exactly the devices whose holders it was authorized to destroy
(resolved from the holders probe via `selectattr('stdout', 'search', 'lvm')`,
VGs from `pvs --noheadings --readonly -o vg_name`), before the existing
`wipefs`/`sgdisk` pair. dm-crypt holders are named in the refusal but only the
LVM teardown is automated; `wipefs` handles the crypt signature itself.

**Constraint (destroy takes the LVM stack down too, and releases the cluster the
disks name):** the destroy wipe went straight to `wipefs --all --force`, which
cannot open a device whose VG is still active (`probing initialization failed:
Device or resource busy`). It only ever worked because `cephadm rm-cluster
--zap-osds` usually removed the OSD VGs first — and that step is gated on an fsid
resolved from the SEED (`cluster_gate.yml`), so it is skipped exactly when it is
needed: a seed a previous run already cleaned resolves no fsid, and non-seed
removal is gated on the seed's. `destroy_steps/lvm_teardown.yml` is now included
by BOTH wipe paths (`wipe_and_cleanup.yml` for declared devices,
`filter_device_reclaim.yml` for `all: true` disks) and runs the same sequence the
apply-side reclaim always has: `vgchange --activate n` → assert → `vgremove` →
`pvremove`, before `wipefs`/`sgdisk`. A VG that refuses to deactivate has an open
LV = a live OSD; the assert names the VG, the `vgchange` rc, and the fsid-scoped
remedy, and no token relaxes it (ADR 0039). Before deactivating, the teardown
reads `ceph.cluster_fsid` from `lvs -o lv_tags` on the VGs standing on the
devices it is authorized to wipe and, when `/var/lib/ceph/<fsid>` still exists,
runs `cephadm rm-cluster --force --fsid <fsid>` there — no `--zap-osds` (the
teardown wipes the devices itself, and `--zap-osds` needs a pullable image). The
fsid may come ONLY from VGs on marker-recorded devices, or on the filter-selected
disks of an `osdReclaimAll` host: a device wiped under `--authorize
unowned-devices` alone vouches for no cluster identity and releases nothing.
The mount re-probe must stay immediately before the LVM teardown, which is now
the first destructive step (the repocheck pins that adjacency).

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
`ceph osd tree` — matched on the rendered `crushNames` (ceph shortens the
hostname for CRUSH; see
[ceph-host-identity-namespaces.md](ceph-host-identity-namespaces.md)), not on
the orchestrator FQDN; a transiently-down but still-in OSD on a benign re-apply
is not a false fail. `singleHostDefaults` raises either expectation to at least two in
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

**Constraint (dynamic-host CRUSH check is one cluster-wide poll, not per host):**
`ceph osd tree` is a whole-cluster snapshot, so the dynamic-host wait polls it
ONCE with an `until` that requires EVERY declared dynamic host bucket to hold an
in OSD (a filter chain — `selectattr('name','in', hosts) | map(attribute='children')
| map('intersect', <in-osd ids>) | map('length') | select('gt', 0) | list | length`
`>= hosts|length`), then a single looped assert re-reads that one snapshot per
host. Polling `ceph osd tree` once per host multiplied the attempt budget by the
host count for zero extra signal (worst case, N hosts x 30 x delay before the
assert fired). The per-host wait is gated on the global readiness fact so a
globally-failed cluster fails fast at the global assert instead of also burning
the per-host budget.

**Constraint (created-but-not-booted OSDs are not a rejected-device fault):** The
global gate deliberately keys on `num_in_osds`, so OSDs that are `in` but `down`
and `stray` (created by ceph-volume, daemons never booted into the CRUSH map)
PASS the global gate and fail only the per-host CRUSH check — the failure mode
behind a green-then-stray install. The rich diagnostics (`ceph orch device ls
--wide`, `ceph orch ps`, `ceph orch host ls`, `ceph orch ls osd`) therefore also
run when a dynamic host has no in OSD, not only on global-gate failure, and the
per-host assert reports the cluster-wide total/stray/down OSD counts and names
the two distinct remedies: investigate the OSD daemons (image pull / mon
connectivity) when OSDs exist but are stray/down, versus clean or exclude
rejected devices when no OSD exists for the host. The earlier per-host message
assumed rejected devices unconditionally, misdiagnosing the stray/down case.
After all late service specs and object operations, apply performs a final
health poll and refuses to record success while the cluster is unreachable or
`HEALTH_ERR`; `HEALTH_WARN` remains acceptable because expected operational
warnings do not make the desired topology unusable.

**Constraint (all-devices OSD auto-reclaim):** A host authored by filter renders
`devices: []`, so the Pass-1 static gate/reclaim/marker no-op for it and a
reinstall onto disks carrying a prior Ceph signature fails at the readiness poll
with zero OSDs. `apply --mode rebuild --authorize data-loss` closes that FOR `all: true`
hosts ONLY: the CLI emits `bootwright_ceph_filter_reclaim_clusters` for in-scope
clusters that have a host whose OSD selection is `data_devices.all=true`
(`topology.ClusterHasAllDevicesOSDHost`, mirrored per host by the rendered
`osdReclaimAll` flag) — never under `--mode rebuild` alone, `--yes` alone, or
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
under `all: true` + `--mode rebuild --authorize data-loss` their disks would be zapped.
`all: true` asserts every disk on the host belongs to this cluster; the CLI
warning states this explicitly (do not use `all: true` on a host shared with
another Ceph cluster or holding data to keep). The unreliable-diagnostic
counterpart: the readiness-failure device dump now runs `ceph orch device ls
--wide --refresh` with a short retry so reject reasons are populated, not empty.

**Constraint (every `until:` in this role needs an attempt escape, even with
`failed_when: false`):** ansible-core applies `failed_when` inside the retry
loop body and then, in the loop's `else` branch, sets `result['failed'] = True`
unconditionally when the budget is exhausted (`task_executor.py`, "we ran out of
attempts"). `failed_when: false` therefore does NOT survive retry exhaustion.
Without the `or (… .attempts >= … retries)` term, `osd_reclaim.yml`'s device
inventory poll aborted the `any_errors_fatal` play BEFORE the OSD spec was
applied and before `osd_readiness.yml` could produce any diagnostic — so
authorizing the reclaim could make a fresh bootstrap strictly worse than not
authorizing it, because cephadm scans host inventories asynchronously and six
just-registered hosts routinely need longer than the old 12x10s budget. Both
that poll and the coverage report's twin now carry the escape; the reclaim
budget matches the readiness poll (30 attempts), and hosts whose inventory never
arrived are named in a debug rather than silently contributing no candidates.

**Constraint (the readiness failure names the remedy that matches the declared
selection shape):** the only text that named `apply --mode rebuild --authorize
data-loss` used to live in `osd_coverage_report.yml`, which `bootstrap.yml`
includes AFTER `osd_readiness.yml` — correctly, because it reports the
pass-but-short case — so the total-shortfall failure never reached it and told
the operator to hand-clean disks instead. `osd_readiness.yml` now composes the
remedy itself from the rendered `osdReclaimAll` host flag: `all: true` hosts get
the auto-reclaim invocation (with its irreversibility, the identity-drift
re-bootstrap caveat, and the `protectedKinds` fail-closed caveat); everything
else gets `--reclaim-devices` for a static `paths`/`pathSpecs` selection and
manual cleaning for a narrowing filter. The two must not be crossed: an
`all: true` cluster declares no static path, so `--reclaim-devices` exits 2
against it, and narrowing filters are deliberately outside the auto-reclaim.
The failure also leads with an N-of-M availability rollup computed from a
`ceph orch device ls --format json` sibling probe, because "every declared
device on every host was refused" is a different diagnosis from one stray disk
and the `--wide` table alone left the prose reading as the latter.

**Constraint (the CLI's declared-device set must equal the ansible gate's):**
`converge.DeclaredOwnedOSDDevices` — which decides whether a `--reclaim-devices`
path is legitimate — read only `host.Devices`, while the ansible side gates on
the rendered `devices` list, i.e. `topology.OSDHostAllStaticDevices` (host
devices UNION osd data/db/wal `paths` and `pathSpecs` UNION a covering
drivegroup's static paths). A cluster declaring OSDs through `osd.dataDevices.paths`
or a drivegroup therefore got `install.yml`'s gate telling it to run
`--reclaim-devices <path>` on a path the CLI then rejected as undeclared. Both
sides now resolve through `OSDHostAllStaticDevices`.

**Constraint (a host-by-host teardown must stop the orchestrator before the
first host, or the cluster reprovisions the disks behind it):** `cephadm
rm-cluster` is a LOCAL command — it does not remove the host from `ceph orch
host ls`, and the manager keeps the SSH access it was enrolled with. So a
teardown that purges the seed first, then each non-seed, leaves every purged host
registered with managers still running on the hosts it has not reached, and the
cephadm module reconciles them straight back: it redeploys the daemons the purge
removed and, because the purge freed their OSD devices, runs ceph-volume over
them again. The seed is purged first and is therefore the node that comes back
signed — matching the observed shape where every peer ends clean and the seed
alone carries fresh `LVM2_member` PVs (new PV UUIDs, no valid marker, because the
same teardown removed it). The run is green throughout: every task exited 0. A
second destroy cleans the node because no manager is left to reprovision it.
`cluster_gate.yml` now runs `cephadm shell -- ceph mgr module disable cephadm`
(bounded by `timeout`) on the seed once ownership is proven and the live cluster
answered, and fails closed when it cannot — this is step one of the upstream
cephadm purge procedure for exactly this reason. `phases/rebuild.yml` carries the
same step for `--mode rebuild`, which removes the old cluster from every topology
host before bootstrapping onto the same disks. A teardown that fails after the
disable leaves the module off; `ceph mgr module enable cephadm` restores it.

**Constraint (the wipe is verified before the node's ownership evidence is
released):** every device gate before the wipe reads what the disk held BEFORE
it; nothing re-read the disk after. A wipe that reached the device and was undone
afterwards therefore passed unnoticed while the same file went on to remove the
OSD ownership marker and the cluster ownership record — leaving a node holding
data no run claims and the next apply refuses as foreign. Both wipe paths
(`wipe_and_cleanup.yml` for declared devices, `filter_device_reclaim.yml` for
`all: true` disks) now re-read every wiped device with `wipefs --no-act` after
the `sgdisk` zap and fail closed on a surviving signature — with the
probe-completeness assert its siblings carry, since an unreachable-ignored probe
returns results with no `rc`. Placement is load-bearing: the refusal sits BEFORE
`Remove managed Ceph local state` and `Remove storage cluster ownership record`,
so a failed verification keeps the evidence that lets a re-run wipe the devices
as Bootwright's own. No `--authorize` token relaxes it: it reports what the disk
holds now, not who owned it.

**Constraint (the per-host wipe verification is not the release proof — the
settle gate is):** the two constraints above each closed one way a green teardown
could leave the seed signed, and the shape returned anyway. Both share a
structural weakness: they verify a *precondition* (the module answered a disable)
or verify the disk *at the moment that host finished*, while other hosts are
still removing their cluster. Three paths reopen the race, and none of them
prints anything — `ansible.cfg` sets `display_skipped_hosts = False`, so a task
whose `when` fails leaves no line at all:

1. **The disable was gated on the probe that fails first.** Both the disable and
   its fail-closed assert keyed on `bootwright_ceph_fsid.stdout` being non-empty.
   `Check Ceph fsid on seed host` is `timeout 60 cephadm shell -- ceph fsid` with
   `failed_when: false`, and it routinely returns nothing while the cluster is
   very much alive — pulling its container image, or retrying mons that are slow
   rather than dead. Ownership still resolves (the decision accepts an empty live
   fsid), `bootwright_ceph_destroy_fsid` falls back to the `ceph.conf` fsid, and
   every host runs `rm-cluster --zap-osds` with the module still enabled. The
   disable is now gated on `bootwright_ceph_destroy_fsid` — the same condition
   that gates `rm-cluster`, so it covers every removal — while the fail-closed
   assert keeps the live-answer gate so a genuinely dead cluster still tears down;
   a disable that failed on an unanswering cluster is reported, not silent.
2. **A whole cluster removal can be skipped.** `rm-cluster` on every host is
   gated on the SEED resolving an fsid, and per host on that host resolving a
   `cephadm` command. A seed carrying neither `/etc/ceph/ceph.conf` nor a
   `/var/lib/ceph/<fsid>` dir resolves no fsid, so NO host removes anything while
   peers may still run the full cluster; and a host whose `/var/lib/ceph/<fsid>`
   a previous partial run already deleted resolves no cephadm and is reported as
   "already done" while its units keep running.
3. **Ordering between the two wipe paths.** `wipe_and_cleanup.yml` removed the
   OSD marker and the ownership record before `filter_device_reclaim.yml` had
   wiped an `osdReclaimAll` host's disks at all.

The fix is to stop proving preconditions per host and prove the outcome once, for
the whole play. `destroy.yml` is now
`… → wipe_and_cleanup → filter_device_reclaim → settle_gate → release_node`:
every ownership-releasing step moved out of `wipe_and_cleanup.yml` into
`release_node.yml`, and `settle_gate.yml` sits between them. Because the play is
`linear` + `any_errors_fatal`, a task boundary is a cluster-wide barrier: when
the settle gate runs, every host has finished both wipes, and a refusal on any
host stops all of them before any host releases anything. The gate asserts two
things per host: no Ceph daemon outlived the teardown
(`systemctl list-units 'ceph-*@*.service'`, fsids parsed from the unit names,
tolerating only an fsid this host still holds `/var/lib/ceph` state for and this
teardown does not own — which is exactly the co-resident cluster
`release_node.yml` preserves), and every device either wipe path touched still
reads clean (`wipefs --no-act` over `bootwright_ceph_present_devices` +
`bootwright_ceph_filter_wiped_disks`, with the probe-completeness assert its
siblings carry). Whatever re-signs a disk — a manager the disable missed, a host
whose removal was skipped, a daemon whose state is gone while its unit runs — is
named here instead of being discovered by the next apply.
