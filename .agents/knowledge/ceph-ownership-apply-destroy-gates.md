# Ceph cluster ownership: the 3-factor gate, apply modes, and destroy scoping

**Semantics:** Normal ownership of a cephadm cluster is 3-factor: the on-disk
`/etc/ceph/ceph.conf` carries an fsid AND a Bootwright storage-cluster
ownership record exists on the controller for this seed AND this host is the
declared `seedHost`; the host-local marker must be present and its fsid must
agree with the conf fsid. The record and marker are independent consistency
checks. A seed with no ceph.conf at all has nothing to protect and is treated
as owned only when the live fsid probe also finds no cluster.

**Semantics (a dead seed may delegate proof, never authorization):** the
declared seed remains the ownership authority whenever it answers. Under
`--authorize unreachable-nodes`, once that seed is positively classified
absent, the destroy play considers only reachable hosts rendered from the
declared `mon` topology. Before the device gates, it reads the controller owner
record plus the config and Bootwright marker on every reachable declared mon.
The controller JSON must be readable and identify Bootwright owner role, the
current context, `storage-cluster` kind/name, declared cluster and seed, and one
valid fsid. Each reachable mon's `/etc/ceph/ceph.conf` and
`/etc/ceph/.bootwright-owned` must be readable and identify that same
cluster/fsid. Only then does the first matching mon in authored topology order
become the ownership authority for the fsid-scoped orchestrator stop, per-host
removal, settle gate and record release. Missing, unreadable, contradictory or
ambiguous evidence aborts before teardown; a peer never reconstructs a missing
controller record and no token supplies a record-only escape. The absent seed
remains skipped, so its local state is retained and the cluster result remains
partial.

**Constraint (a seed that carries nothing is evidence about the seed, not about
its peers):** that "nothing to protect" branch resolves ownership WITHOUT
resolving an fsid, and every per-host cluster removal downstream is scoped to
the fsid the seed resolved. With none resolved, and with
`display_skipped_hosts = False`, the whole peer half of the teardown evaporates
in silence: `cephadm rm-cluster` never runs on a non-seed host, the
`cephadm_command.yml` include that carries the "no cephadm to remove it"
refusal is never reached either, and — the load-bearing part —
`bootwright_ceph_cleanup_owned_fsid` on a non-seed host is
`hostvars[seed].bootwright_ceph_destroy_fsid`, so it resolves to the empty
string and the host's OWN cluster is classified as a foreign co-resident
cluster and PRESERVED. The seed meanwhile finds no fsid directory of its own,
takes the full-removal branch, deletes its `/etc/ceph`, and removes the
cluster's controller ownership record. The run is green; the cluster is intact
on every peer; and because the seed's evidence is now gone, the next destroy
cannot prove ownership and the next apply refuses the disks as foreign data.
Observed on ceph-prd-01 2026-08-07 (seed srv4203), printed verbatim by the
teardown itself: `Preserving /etc/ceph, /var/lib/ceph and /var/log/ceph on
srv4204: foreign Ceph cluster fsid(s) 2744de24-… not owned by ceph-prd-01`
— where that fsid IS ceph-prd-01. Not the same bug as the 2026-08-04 seed
silent skip, which hardened ENTRY to this branch against probes that never ran;
this is what the branch does to the other five hosts once entered.
`cluster_gate.yml` now scans every non-seed host for `/var/lib/ceph/<fsid>`
directories whenever the seed resolved no fsid and fails closed on any, naming
both exits (`--recover-ceph-ownership <cluster>=<fsid>`, or a manual
fsid-scoped `cephadm rm-cluster`). No token relaxes it.

**Semantics:** `destroy --recover-ceph-ownership
<StorageCluster>=<fsid>[,...]` repairs both controller and host evidence. Go
requires a selected declared managed cluster and refuses an existing context
record that contradicts its declared seed; absence is recoverable. On the seed,
Ansible requires an exact supplied-fsid match against `/etc/ceph/ceph.conf`,
then writes the controller record on delegated localhost and the normal `0600`
host marker before re-reading both through the unchanged destroy ownership
decision. The supplied mapping is explicit operator attestation for that exact
identity. A reachable live `ceph fsid` must equal the configuration fsid; it is
used only to reject contradictory identity, never as authorization. Recovery
never overwrites contradictory evidence or relaxes OSD device gates.

**Root cause:** Storage-cluster resource-record includes originally inherited
the seed host's connection. They therefore wrote, read, and removed the
controller path on the remote seed instead of delegated localhost, leaving Go's
context ownership store empty even after successful apply. Storage-cluster
resource record writes, reads, and removal are controller-local; package
records remain host-local because they describe packages installed on each
storage node.

**Semantics:** The fsid is always read from the on-disk conf, never live
`ceph fsid`, so a DOWN owned cluster stays classifiable, re-stampable, and
removable. Teardown resolves the live fsid when the cluster answers, else the
conf fsid — `cephadm rm-cluster` operates on local daemons/state by fsid, not
on the live cluster.

**Semantics (the live probe is bounded and conditional).** The destroy gate's
`ceph fsid` probe runs `timeout 60 cephadm shell -- ceph fsid` and only when
the seed still carries Ceph cluster state — `/etc/ceph/ceph.conf` exists, or
`/var/lib/ceph/<uuid>` does. Unbounded it parks the whole teardown: on a
SECOND destroy the first run removed the local state, so `cephadm shell` can
infer no image and falls back to pulling one (unbounded behind a proxy), and
where the conf survived with mons that are gone the `ceph` CLI retries the mon
connection forever. Neither is a connection failure, so the SSH keepalives in
`ansible.cfg` never fire. The state gate is not merely an optimization but
must not narrow to the conf alone: a seed with `/var/lib/ceph/<fsid>` and no
conf still answers `ceph fsid` (cephadm infers the cluster from the directory),
and skipping the probe there would let a seed running daemons take the
"no conf and no live cluster ⇒ nothing to protect" branch and fail the gate
OPEN. With no conf and no fsid directory the probe is structurally incapable
of finding a cluster — there is no config for the CLI to find a mon with — so
not running it changes no verdict. A timeout reads as "cluster unreachable",
the same as a down cluster, and unreachability alone never authorizes: the
conf/marker/record triple still has to agree.

**Semantics (the refusal names which factor is unproven).** The gate has three
independent ways to fail and the operator must be told which one fired.
`destroy_steps/cluster_gate.yml` resolves `bootwright_ceph_destroy_unproven`
— an ordered list of the specific factors that did not hold (no controller
record at `<path>`, marker missing or fsid-less, marker/conf disagreement, live
cluster/conf disagreement, conf without an fsid, conf absent but a live cluster
answering) — plus `bootwright_ceph_destroy_evidence`, the four readings
themselves. Both go into the refusal. The prior message listed all three causes
joined by "either … or …" and named none, so the reader could not tell whether
`--recover-ceph-ownership` was the right exit (it is, for a missing record or
marker) or would refuse anyway (it does, for a contradiction). Observed on
ceph-prd-01 2026-08-04: a repeat destroy refused on seed srv4203 for
`6a1388fa-9021-11f1-bf15-303ea72d7724` with no way to tell from the run which
evidence had gone missing.

**Semantics:** `cephadm rm-cluster` is host-local: run on the seed it stops and
removes only the SEED's daemons/state. Non-seed hosts keep their
mon/mgr/mds/osd systemd units and podman containers running, so wiping their
devices while daemons still hold them leaves restart-looping units and stale
device-mapper maps. Destroy runs rm-cluster per non-seed host too — keyed on
the same fsid and the seed's proven ownership — before any device wipe, and a
real rm-cluster failure fails closed (never fall through to wiping devices with
daemons still up). The destroy play runs `any_errors_fatal`, so a seed
ownership refusal aborts before any node wipes its OSD devices.

**Semantics (the seed-keyed rm-cluster has a hole the disks close):** both the
seed and non-seed removals are gated on a fsid the SEED resolves, so a seed a
previous run already cleaned resolves none and NO host removes anything — which
is exactly the state a teardown that skipped one node leaves. That node keeps
running its daemons and its OSD LVs stay open. `destroy_steps/lvm_teardown.yml`
closes it from the other end: the fsid comes from the `ceph.cluster_fsid` LV tag
of the bluestore VGs standing on the devices this teardown is authorized to wipe,
and when `/var/lib/ceph/<fsid>` still exists on that host it runs the same
fsid-scoped `cephadm rm-cluster --force --fsid <fsid>` there (no `--zap-osds`)
before taking the LVM stack down. The disks are the authorization: only VGs on
marker-recorded devices, or on an `osdReclaimAll` host's filter-selected disks,
may name an identity — never a device carried by `--authorize unowned-devices`
alone (ADR 0039).

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
bootstrap remains eligible for cleanup only through the dedicated
`bootwright_ceph_incomplete_bootstrap_authorized_clusters` list, never the
structural-drift list. Go adds the selected managed cluster only when its exact
owner-role record matches API, context, name, cluster, rendered host, and
recorded `seedHost`, and only for `apply --mode rebuild --authorize data-loss`.
The seed slurps and re-validates the same record, then combines it with config
present, marker absent, and unreachable health into one consequence predicate
read by the refusal, ownership gate, and cleanup. Any missing, unreadable, stale,
or mismatched record refuses before cleanup, bootstrap, or marker stamping.
Successful apply refreshes the fsid marker and enriched ownership record.

**Semantics:** `--mode rebuild` does not always wipe. The controller classifies a
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
(`--authorize unreachable-nodes`), the cluster's controller-side ownership record is KEPT
and re-stamped as partially destroyed from the destroy-result file — the
cluster is only partially torn down and must not read as fully gone. A normal
teardown (no skipped nodes) removes the record.

**Root cause:** every ownership and apply-mode gate above reads the SEED host —
`/etc/ceph/ceph.conf`, the host marker and the controller record are all
resolved there, and `create` refusing a pre-existing cluster means a
pre-existing cluster *on the seed*. A non-seed node carries no such evidence, so
a node that still runs daemons of an unrelated fsid passed every gate and was
enrolled anyway. That is exactly what a teardown which skipped this node
(`--authorize unreachable-nodes`) or died partway through it leaves behind: the
other nodes are cleaned, this one keeps its systemd units and containers. The
leftover is silent until cephadm tries to place the same daemon type there —
every cephadm daemon binds its port on the host network, so the leftover keeps
it and the new deployment dies with `Cannot bind to IP 0.0.0.0 port <port>:
[Errno 98] Address already in use` / `TCP Port(s) '0.0.0.0:<port>' required for
<daemon> already in use`, retried every serve loop and never attributed to a
host. The apply then fails ~15 minutes later on the service readiness gate as a
service one daemon short of its declared count, naming neither the node nor the
cluster that owns the port. Observed on ceph-prd-01 2026-07-31: node-07 (the
stretch arbiter, whose earlier teardown failed) still ran
`ceph-9886b0ec-…@node-exporter.node-07.service` from the previous install, so
`node-exporter` stuck at 6/7 while every other service reached full count.

**Semantics:** `phases/foreign_cluster.yml` closes that gap on EVERY topology
host: it lists `ceph-<fsid>@*.service` units, subtracts the identities this
apply may legitimately find (the fsid in the node's own `/etc/ceph/ceph.conf`,
plus the seed's `bootwright_ceph_override_fsid` so an authorized rebuild is not
flagged for the cluster it is replacing), and refuses when anything remains. It
runs AFTER `phases/rebuild.yml` — an authorized rebuild has removed the override
fsid's units by then — and before non-seed hosts end, so it precedes bootstrap
and any device write. It keys on systemd units, not `/var/lib/ceph/<fsid>`
directories: an inert leftover directory holds no port, and destroy deliberately
preserves co-resident fsid directories, so directories are not evidence of a
running cluster. The refusal names each foreign fsid, the loaded units, and
`cephadm rm-cluster --force --fsid <fsid>` — fsid-scoped, so it removes only
that cluster's daemons and leaves the applied cluster's own untouched.

**The exit is in-product (ADR 0038).** `apply --authorize foreign-daemons` runs
that same fsid-scoped removal, once per foreign identity, on the node that
carries it — no `--zap-osds`, so the other cluster's OSD disks keep their data
and what is destroyed is its presence on this node. Two properties make the
authorized path still a gate rather than a bypass: the node is probed a SECOND
time and the foreign set re-resolved from that listing, so a leftover surviving
its own removal refuses exactly as before; and that probe carries no
`failed_when: false`, because a node whose units cannot be read is not a node
proven clean. The second probe must register its own variable — Ansible
overwrites a register when the task is skipped, so reusing the first probe's name
would erase, on every unauthorized run, the very listing the refusal prints.
Why the rebuild could not have done this: `phases/rebuild.yml` removes exactly
one identity, the seed's `bootwright_ceph_override_fsid`, on every topology host,
so an fsid a non-seed node carries that the seed no longer does is outside the
reach of every rebuild by construction. Observed on ceph-prd-01 2026-08-01: a
`--mode rebuild` apply refused on the arbiter, whose 07-31 15:00 UTC install
(`9886b0ec`) outlived the 17:08 UTC one (`6489f7ec`) the rebuild was replacing.
Both fsids are UUIDv1, so decoding their timestamps (and their shared node MAC,
the seed's) orders the installs and identifies which one a leftover belongs to
without a run log.
