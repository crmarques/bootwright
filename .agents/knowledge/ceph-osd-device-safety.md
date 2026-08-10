# Ceph OSD device safety: markers, gates, reclaim, and the readiness poll

**Constraint:** Device safety has two selection contracts. Explicit block-device
paths — `devices`, per-host data/db/wal `paths`/`pathSpecs`, and covering fleet
drivegroup paths — are deduplicated and pass through the marker, emptiness,
reclaim, and destroy gates below. Managed dynamic data/db/wal selectors are not
statically enumerable, so `apply` renders their actual filter fields and runs a
read-only live-inventory gate before the persistent OSD service is applied.
Unknown inventory or probe state fails closed. Destroy still cannot infer an
arbitrary path from a filter; its filter-host cleanup is limited to devices
whose Ceph signatures establish the teardown target under the separate destroy
rules below.

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

**Constraint (dynamic DeviceSelection validation is part of the wipe boundary):**
The prior API accepted shapes cephadm rejects and the reclaim predicate read
only the `all` boolean. `all: true` plus a narrowing filter or `limit`, and even
an `unmanaged: true` service, could therefore be classified as permission to zap
every unavailable disk before cephadm rejected or ignored the service. Desired
validation now mirrors Ceph: `paths` and `pathSpecs` are mutually exclusive;
either explicit form cannot combine with `all`/`model`/`vendor`/`rotational`/
`size`; `all` is data-only and cannot combine with those filters; and `limit`
only caps another selector. Runtime classification remains defensive, but
ambiguous desired state never reaches it.

**Constraint (the dynamic filter gate consumes rendered intent, not the service
spec):** `storageHostsVars` renders `osdDeviceFilters` for every managed dynamic
data, DB, and WAL selector, each with the role, effective `filterLogic`, and the
authored `all`/`model`/`vendor`/`rotational`/`size`/`limit` fields. That value is
the Go-to-Ansible contract. `osd_filter_gate.yml` runs on the seed after host
admission, before optional auto-reclaim and before `/mnt/core-services.yaml` is
applied; it never re-parses the persistent service spec and runs no destructive
command.

The gate requires complete `ceph orch device ls --format json --refresh`
inventory for every dynamic host and readable `ceph osd metadata`. It excludes
this cluster's live OSDs through inventory `osd_ids` and metadata-derived paths,
then evaluates every `available: false` device against each role's filter.
Missing availability, path, model, vendor, rotational, or size facts are
`unknown`, never a non-match. The matcher uses Ceph's decimal size units,
inclusive ranges, substring model/vendor matching, and the authored AND/OR
logic. A candidate matched by more than one role is probed once and reports all
roles.

Every matching candidate is mount-probed on its owning host. A readable mounted
device is not consumable by ceph-volume and is excluded; an unreachable host or
unreadable mount verdict fails closed. An unmounted candidate is probed with
read-only `wipefs --no-act --noheadings`; a failed probe or any signature refuses
before the service apply unless an effectively unbounded selection is already
authorized for the auto-reclaim step. Neither `--mode rebuild` nor `--authorize
data-loss` bypasses the refusal for a narrowing selector. The in-product remedy
is to pin the named disk in `paths`/`pathSpecs`, then run the exact controller-
rendered reclaim invocation in the refusal. `bootwright_apply_reclaim_invocation`
preserves context, selection, mode, prior effect flags, authorizations, dry-run,
output, confirmation and SSH flags, unions `data-loss,unowned-devices`, and
contains one sentinel as the entire reclaim operand. The role validates and
comma-joins `bootwright_apply_reclaim_devices` with the runtime paths, quotes the
whole value, asserts one sentinel, and replaces only it. Commas, newlines, NULs,
relative paths, empty lists and sentinel collisions fail closed; shell metacharacters
remain data in one argv value. This converts a host-derived match into explicit,
reviewable wipe intent without letting a runtime path inject flags.

`limit` is deliberately ignored when deciding whether a dirty match is possible.
The cephadm service persists beyond the inventory snapshot and may select a
different matching disk when availability changes; gating an arbitrary current
first N would silently clear a future target.

**Constraint (effective-unbounded auto-reclaim):** Automatic reclaim is limited
to a managed data selector with `all: true` and no `limit`. Validation already
makes `all` data-only and mutually exclusive with the narrowing filters.
`unmanaged: true`, a limit, every model/vendor/rotational/size filter, every
static selection, and every unauthorized run are excluded.
`topology.OSDHostUsesAllDevices` is the
shared effective-selection predicate: the CLI uses the cluster consequence to
emit `bootwright_ceph_filter_reclaim_clusters`, the renderer mirrors it as
`osdReclaimAll`, and the gate, warning, and reclaim therefore cannot disagree.
It runs only under `apply --mode rebuild --authorize data-loss`, never under
rebuild alone, `--yes` alone, or dry-run.

`--reclaim-devices all` is not an alias for this dynamic behavior. When no
static path exists, converge returns a typed evidence-only error. The CLI changes
the exact invocation to rebuild and removes the incompatible reclaim flag only
when every selected cluster is effectively unbounded; for a narrowing or mixed
selection it requires a static desired path and repeats the original invocation
after the edit. Mixing `all` and paths is rejected without constructing a
command.

The auto-reclaim still fails closed around live state. Both device inventory and
OSD metadata must be readable before any zap. A candidate is
`available: false`, has no inventory `osd_ids`, is absent from metadata-derived
live-OSD paths, and has a successful delegated mount probe with no mountpoint or
swap. Only then does `ceph orch device zap <host> <path> --force` run. The
earlier read-only gate has already proved that candidate in scope. The result of
every zap is asserted before the persistent OSD service: a non-zero result
refuses with its rc/stdout/stderr and the exact controller-rendered retry. That
retry preserves the operator's resolved context, selection, authorization,
dry-run, and SSH flags while changing only the rebuild intent required to
continue, instead of falling through to an impossible readiness wait.

The known blast radius remains explicit: a co-resident foreign Ceph cluster's
raw, unmounted OSD may be absent from this cluster's inventory and metadata. An
effective unbounded `all` selection asserts every such disk belongs to this
service, so the CLI warning forbids using it on a host that carries another Ceph
cluster or data to keep. The readiness-failure device dump uses `ceph orch
device ls --wide --refresh` with a short retry so rejection reasons are
populated rather than empty.

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
budget stays at 30 attempts while the readiness poll scales to
`max(90, expectedCount * 6)` under a wall-clock deadline, and hosts whose
inventory never arrived are named in a debug rather than silently contributing no
candidates. Two polls in this role were still missing the escape after the
readiness rework, and the first one mattered most in exactly the failure the
rework exists to diagnose: `osd_readiness.yml`'s DIAGNOSTIC
`ceph orch device ls --wide --refresh` gates on `stdout | trim | length > 0`, a
predicate a cluster cephadm never scanned can never satisfy, so its 6 attempts
exhausted and aborted the play with "Ran out of attempts" BEFORE the
uninventoried-host summary, the `remedy_orchestrator` text and the assert could
render — the run lost the whole diagnosis at the one moment it was worth having.
`result_and_ownership.yml`'s final health poll had the same hole, reporting "Ran
out of attempts" instead of the crafted refusal on the very next line. A
`timeout --kill-after` wrapper does NOT substitute for the escape: it bounds one
attempt, not the budget. `TestStorageCephRetryPollsCarryAnAttemptEscape` now
walks every task file in the role and requires one
`.attempts | default(1) | int)` per `retries:`, so the invariant is enforced
rather than remembered.

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

**Constraint (the release proof reads the node, not the device list the run
resolved — `destroy_steps/lvm_sweep.yml`):** every gate above shares one blind
spot. They all iterate `bootwright_ceph_present_devices` (or the filter set),
which is resolved from the declared paths BEFORE the cluster comes down, so each
one is vacuously green on a node whose devices that resolution missed. Three
paths reach it, and `display_skipped_hosts = False` prints nothing for any of
them: a declared device that `lsblk` reported as "not a block device" is
classified absent and drops out of the present set (a deliberate config-drift
tolerance — NVMe namespace numbering alone decides whether the data disks are
`nvme0n1`–`nvme3n1` or `nvme1n1`–`nvme4n1`); an OSD ceph-volume created on a
path no declaration names is never in it; and a disk re-signed after its own
verification read clean is *clean* in it. In all three the node ends the run
carrying `ceph-<uuid>` volume groups with `osd-block-<uuid>` LVs while the run
reports the cluster destroyed and releases its ownership evidence — the shape
that leaves `pvs -o pv_name,vg_name,lv_name` naming five OSD volume groups on
the seed while every peer is bare, and that leaves the next destroy unable to
prove ownership of the cluster the residue still names (the operator exit is
`--recover-ceph-ownership <cluster>=<fsid>`; observed on ceph-prd-01/srv4203 on
2026-08-07 and again 2026-08-09).

The first sweep implementation still reproduced the incident because its final
scan classified only rows whose PV appeared in the *initial* selected-device
list. If that list was empty and cephadm recreated the five seed VGs between the
two scans, the final `pvs` output contained them but the survivor filter selected
nothing. The assert passed, `release_node.yml` erased the host and controller
evidence, and Go treated the absence of skipped-node entries as completion. The
source-shape test checked task ordering and variable names, so it pinned the
broken predicate without executing the race.

`lvm_sweep.yml` still runs after the settle barrier and before release, but its
scanner now takes bounded, batched whole-node `pvs` and `lvs` snapshots rather
than one `lvs` process per VG. Each sample reads both tables twice between
ceph-volume process probes; the terminal proof needs three identical writer-free
samples spanning at least two seconds. A writer or changed row set resets the
window, and every subprocess shares the 30-second deadline. Both the initial
sweep and terminal proof classify the rows they just read: **taken down** when
the VG carries this teardown's fsid or stands on a declared device;
**preserved** when it belongs to resolved co-resident state; and **left named but
untouched** when neither fact proves ownership. The terminal survivor set is
therefore independent of the initial device list and catches a VG first created
during teardown. The exact five-NVMe seed fixture injects that late creation and
must fail before ownership release.

After every reachable node proves zero owned survivors, Ansible writes one
versioned per-node attestation before releasing host evidence. Go requires an
exact match to the selected topology and binds skipped outcomes to the consumed
`unreachable-nodes` authorization before the scheduler may persist task success,
release the controller owner, reset convergence state, purge history, or allow
machine-registration, access, and substrate teardown to proceed. Missing or
incomplete evidence is a failed task, never success inferred from silence. No
`--authorize` token relaxes the scan or the attestation.
