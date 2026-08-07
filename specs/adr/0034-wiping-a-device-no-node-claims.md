# ADR 0034: Wiping a Device No Node Claims

## Status

Accepted

Extends [ADR 0030](0030-one-intent-flag-and-named-authorizations.md) with one
token and narrows the blanket "no token relaxes device data-safety" clause in
`specs/state-model.md` to the half of it that has a self-service remedy.

## Context

Bootwright records the disks it provisioned as OSDs in an on-node marker at
`/etc/bootwright/ceph-osd-devices.json`. Three gates read it, and each refuses a
declared device the marker does not name:

- the `--reclaim-devices` holders gate on `apply`, which refuses a device
  carrying LVM or dm-crypt holders as a probable live bluestore OSD;
- the device-empty gate on `apply`, which refuses a device carrying any wipefs
  signature and names `--reclaim-devices` as the remedy;
- the declared-device wipe gate on `destroy`, which refuses to wipe signatures
  it has no ownership record for.

Each refusal assumed the same thing: that an unclaimed signature belongs to
something still alive, so the operator can go and drain it. That assumption
holds for a co-resident cluster and fails completely for an **orphan** — the
LVM stack a Ceph cluster leaves on its data disks when the cluster itself is
gone. A managed-OS reinstall wipes the OS disk and the marker with it while the
separate data disks keep their `ceph-<uuid>` volume groups. A teardown that
never had the device in its declaration leaves it untouched. In both cases
`/var/lib/ceph` is absent, no daemon is running, and the remedy every refusal
printed — `ceph osd tree`, then `ceph orch osd rm <id>` — addresses a cluster
that does not exist.

The operator was then out of options inside the product. `--reclaim-devices`
refused the device. `destroy` refused the same device through a different gate.
`--mode rebuild --authorize data-loss` reaches only `dataDevices.all: true`
hosts, so a statically named selection had no path at all. What remained was
`vgremove`/`pvremove`/`wipefs` by hand on every node — the operation Bootwright
exists to own, performed out of band, unaudited, with no gate distinguishing the
data disk from the root disk.

The distinction the gates could not express is not live-versus-dead — that is
not reliably knowable from a device probe. It is *who claims it*: a device this
node holds no ownership record for, when nothing on the node is running Ceph, is
an orphan, and an orphan has no owner to ask.

## Decision

**Split device data-safety into an ownership half and a physical half, and make
only the ownership half authorizable.**

`--authorize unowned-devices` relaxes exactly one refusal: that a declared OSD
device carries signatures or LVM/dm-crypt holders while the node holds no
Bootwright OSD ownership record for it. It is accepted by `apply` and by
`destroy` — the same refusal stands in both, so a token that cleared only one
would leave the other as a wall. On `apply` its gate runs only under
`--reclaim-devices`, which already names the exact devices; the token never
widens the set, it only lifts the ownership objection to the devices already
named. It does not authorize the wipe itself: that remains `data-loss`, so
clearing an orphan is `--authorize data-loss,unowned-devices` and neither token
alone suffices.

The physical half stays closed to every token:

- a **mounted or in-use** device is refused, always — this is what keeps a root
  disk out of a reclaim, and it is the one check that cannot be argued with;
- a device whose probe **failed** for any reason other than absence is refused,
  always — an unreadable probe is not evidence of anything;
- a device that is **absent** is neither refused nor wiped. It is skipped with a
  report that the declaration does not match the hardware, and the run continues
  to OSD readiness, where a count short of the declaration is the real
  diagnosis.

That last case was previously fatal, and fatal with the wrong message: the
holders probe and the mount probe shared one assertion, so `lsblk: not a block
device` surfaced as "mounted or in use ()" — an empty parenthesis where the
mountpoint would go, and an operator sent looking for a mount that never
existed. Absence is now classified exactly as the destroy path has always
classified it.

Where the refusals remain, they must name which case they are in. The holders
gate stats `/var/lib/ceph` and branches: with a daemon tree present it keeps the
drain-first remedy and names the token only as a last resort; with no daemon
tree it states that the holders are an orphan, that `ceph orch osd rm` cannot
reach them, and that the token is the remedy — plus the `pvs`/`lvs` commands
that confirm the volume group is prior-install residue before anything is
wiped.

An authorized reclaim also has to leave a disk that ceph-volume will accept.
`wipefs` clears the PV label but leaves active logical volumes mapped, and
ceph-volume rejects a device with holders regardless of its signatures — the
token would have cleared the gate and still produced zero OSDs. The reclaim
path therefore takes the LVM stack down first (`vgchange --activate n`,
`vgremove`, `pvremove`) for exactly the devices whose holders it was authorized
to destroy, before the existing `wipefs`/`sgdisk` pair.

## Amendment (2026-08-07): the ownership objection has a cluster level

The original implementation left one ownership objection the token could not
reach: the `apply` reclaim acted only on a cluster the controller recorded as
Bootwright-owned. A successful destroy releases exactly that record, so after
every destroy the reclaim silently no-oped ("no device will be reclaimed") and
the device-empty gate then named the very `--reclaim-devices --authorize
data-loss,unowned-devices` command that had just no-oped — recreating the
out-of-options loop this ADR exists to close. The token now lifts the ownership
objection at both levels: the node's missing OSD marker entry, and the missing
controller ownership record for the selected cluster. Selection still bounds
the blast radius — the reclaim acts only on clusters the run selected and only
on their declared OSD devices — and the physical half stays closed to every
token, unchanged.

## Consequences

`unowned-devices` is the second token an `apply` gate can consume, so "every
token except `data-loss` is destroy-only" is no longer true and is restated
wherever it was published. The token-vocabulary contract test keeps the three
published tables and the code in agreement, and the scenario matrix exercises
the token on both verbs.

The blast radius is real and is the point: on a node whose Ceph daemons *are*
running, the token wipes a live OSD and its data. The gate can no longer
pretend to distinguish the two cases for the operator, so it makes the
distinction visible — the daemon-tree branch in the refusal, the `pvs`/`lvs`
confirmation step, and a CLI warning stating both readings — and then defers to
the operator, which is what an authorization token is for.

`dataDevices.all: true` hosts are unaffected: their auto-reclaim already wipes
every unavailable, unmounted, non-OSD disk under `--mode rebuild --authorize
data-loss`, and it keys on availability rather than ownership. The new token
closes the equivalent gap for statically named selections (`paths`/`pathSpecs`),
which had no in-product path at all.

## Alternatives Rejected

**Widen `data-loss` to cover unowned devices.** One token would then mean both
"destroy data" and "destroy data whose owner is unknown", and every existing
`--authorize data-loss` run would silently gain the second meaning — including
`--mode rebuild` runs that never asked for it. ADR 0030's rule that a token
unblocks exactly one refusal exists to prevent that.

**Detect liveness and auto-clear a dead OSD.** A probe cannot distinguish a
dead OSD from a live one belonging to a cluster whose daemons are merely
stopped, and guessing wrong destroys data with no operator in the loop. The
daemon-tree stat is used to *phrase the refusal*, never to clear it.

**Leave it to manual `vgremove`.** This is the status quo, and it is the
weakest option available: it moves the most destructive operation in the
product outside every gate the product has, onto a shell where nothing checks
that the disk is not the root disk.
