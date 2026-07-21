# Backlog

Deferred and open engineering work. This is the in-repo catalog of activities
that are known, consciously postponed, and must not be lost between sessions or
agents. It is the companion to the lessons log in
[`KNOWLEDGE.md`](KNOWLEDGE.md): the index records what the codebase already
learned; this file records what it still owes.

## How to use this file

- **Before proposing new work** in an area, scan for an existing entry so a
  known-deferred item is not rediscovered from scratch.
- **Before deferring a review finding**, file an entry here in the same change
  that merges the review's accepted fixes, so the deferral is recorded where the
  next contributor will find it.
- **Entry lifecycle.** An entry is deleted in the commit that lands its fix (the
  commit subject references the ID); any durable lesson from the fix moves to the
  owning `.agents/knowledge/` file. A rejected item is deleted and replaced by a
  "evaluated and rejected, do not re-propose" note in the relevant knowledge
  file — the house pattern already used by
  [substrate-destroy-gate-no-shared-helper.md](substrate-destroy-gate-no-shared-helper.md)
  and [openshift-kubevirt-agent-boot.md](openshift-kubevirt-agent-boot.md). The
  catalog is live-only; git history is the archive.
- **IDs** are `B-NNN`, assigned in order and never reused, even after deletion.
- **Status** is `open` or `in-progress`.

## Entries

## B-001 — `customizations.storage.wipe` is a dead field
- Status: open
- Area: managed-os / api
- Origin: code audit 2026-07-12, finding M3
- Problem: `MachineInstallProfile` `spec.customizations.storage.wipe` passes
  strict decode but is never read by any validator, renderer, or playbook; the
  kickstart `clearpart` is unconditional. Docs that describe `wipe` as a control
  therefore promise behavior that does not exist.
- Exit: either wire the field into the kickstart render (guard test
  `internal/render/inventory/managed_os_test.go` must move with it) or delete it
  so strict decode rejects it, per the spec's no-dead-shapes rule. Fix the
  `docs/advanced/managed-os.md` wipe rows in the same change.
- Related: [managed-os-install-gates.md](managed-os-install-gates.md)

## B-002 — Narrowing-filter OSD reclaim bypasses the emptiness guard
- Status: open
- Area: ceph / osd-safety
- Origin: OSD-install hardening (`--reclaim-devices`), narrowing-filter case
- Problem: `all: true` OSD device sets auto-reclaim dirty disks under
  `--confirm-data-loss`, but a *narrowing* device filter (paths/model/size
  selectors) still bypasses the disk-emptiness guard, so a filtered OSD apply can
  proceed against non-empty disks without the same fail-closed check.
- Exit: extend the emptiness/reclaim guard to cover narrowing filters, or
  document a deliberate boundary; add a regression test alongside the existing
  OSD device-safety pins.
- Related: [ceph-osd-device-safety.md](ceph-osd-device-safety.md)

## B-003 — `--converge-drifted` vs the OSD-device-empty install gate
- Status: open
- Area: ceph / apply-modes
- Origin: Ceph apply-modes design (open OSD-device gate)
- Problem: the interaction between `apply --converge-drifted` and the
  OSD-device-empty install gate is unresolved — whether a drift-rebuild should
  be allowed to proceed against a device the install-time gate would refuse is
  still open.
- Exit: decide and document the precedence of the drift-rebuild path over the
  install-time device gate; encode it as a validator/preflight rule with a test.
- Related: [ceph-override-structural-rebuild.md](ceph-override-structural-rebuild.md),
  [ceph-osd-device-safety.md](ceph-osd-device-safety.md)

## B-004 — Standalone managed-OS machine teardown is fail-closed only
- Status: open
- Area: cli / machine-scope
- Origin: `--machines` apply/destroy selection landing
- Problem: `destroy --machines` fails closed for a standalone managed-OS machine
  (one not owned by a cluster substrate); real per-machine teardown of that class
  was deferred rather than implemented.
- Exit: implement (or consciously decline) teardown of standalone managed-OS
  machines and lift the fail-closed refusal.
- Related: [substrate-ownership-markers.md](substrate-ownership-markers.md)

## B-005 — Kickstart merges bond and VLAN into one stanza, capping bond MTU
- Status: open
- Area: managed-os / networking / render
- Origin: managed-OS post-install network review
- Problem: the minimal kickstart emits a single merged bond+VLAN network stanza,
  which cannot set the bond-layer MTU independently; jumbo-frame bonds are capped
  because the MTU can only be expressed on the merged line.
- Exit: de-merge the bond and VLAN stanzas in the kickstart render so each layer
  carries its own MTU (render-only change; add a golden fixture).
- Related: [managed-os-post-install-network.md](managed-os-post-install-network.md)

## B-006 — Redfish Ansible de-noise + `cleanup_media` assert bug
- Status: open
- Area: ansible / redfish
- Origin: Redfish Ansible de-noise review 2026-07-02 (not implemented)
- Problem: the boot_redfish role has accumulated redundant tasks the review
  proposed removing, and carries a latent `cleanup_media` assert bug identified
  but not yet fixed.
- Exit: apply the reviewed de-noise cleanups and fix the `cleanup_media` assert;
  keep the boot-flow guard tests green.
- Related: [redfish-boot-flow-quirks.md](redfish-boot-flow-quirks.md)

## B-007 — dnsmasq bind-order follow-up for controller DNS auto-wiring
- Status: open
- Area: networking / dns
- Origin: controller-DNS auto-wiring landing (latent follow-up)
- Problem: the managed controller resolver is wired via a systemd-resolved
  drop-in, but a latent dnsmasq bind-order issue remains where dnsmasq can race
  to grab `:53` ahead of the intended listener.
- Exit: pin the dnsmasq bind order (or bind address) so the controller resolver
  wins deterministically.
- Related: [external-dns-bootstrap.md](external-dns-bootstrap.md)

## B-008 — Multi-context shared-service degrading clobber
- Status: open
- Area: converge / shared-services / ownership
- Origin: multi-context shared bastion services landing (open item)
- Problem: a reference-aware destroy releases shared services correctly, but a
  *degrading* apply (one context reducing a shared service another context still
  references) can still clobber the shared component.
- Exit: gate degrading edits to shared services on the reference set, mirroring
  the reference-aware destroy release.
- Related: [ownership-records-store.md](ownership-records-store.md)

## B-009 — Remaining drift-detection gaps: NIC defaults and Ceph FIPS
- Status: open
- Area: converge / drift engine
- Origin: recorded-drift (diff --recorded) coverage thrust, remaining items
- Problem: the recorded-state drift classifier does not yet cover NIC default
  drift or Ceph FIPS drift, so those changes are invisible to `diff --recorded`.
- Exit: extend the recorded-state comparison to include NIC defaults and the Ceph
  FIPS gate; add classifier tests.
- Related: [scoped-runs-render-vs-work-set.md](scoped-runs-render-vs-work-set.md)

## B-010 — Secret first-class kind: Phase E
- Status: open
- Area: secrets / api
- Origin: Secret first-class kind landing (Phase E deferred)
- Problem: the Secret-kind migration landed Phases A–D; Phase E work was scoped
  and deferred.
- Exit: complete the deferred Phase E scope (recover its definition from the
  landing change) or close it explicitly.
- Related: [secret-index-resolution.md](secret-index-resolution.md), ADR 0016

## B-011 — Scoped audit 2026-07-12: NFS and fabric-hash findings (H6)
- Status: open
- Area: converge / storage
- Origin: scoped audit 2026-07-12 (finding H6 and the fabric-hash finding)
- Problem: the H6 NFS finding and the fabric-hash finding were consciously
  deferred at merge time.
- Exit: resolve or explicitly close both findings; capture any resulting rule in
  the owning knowledge file.
- Related: [converge-hash-drift-model.md](converge-hash-drift-model.md)

## B-012 — Lifecycle review 2026-07-12: deferred H1/H4/H5
- Status: open
- Area: converge / lifecycle
- Origin: lifecycle review 2026-07-12 (findings H1, H4, H5)
- Problem: three high findings from the lifecycle review were deferred rather
  than fixed in the merged batch.
- Exit: re-examine H1/H4/H5 against current code and resolve or close each.
- Related: [apply-task-graph-planning.md](apply-task-graph-planning.md)

## B-013 — Lifecycle fixes 2026-07-05: deferred scenarios W8–W17
- Status: open
- Area: converge / lifecycle
- Origin: lifecycle review + fixes 2026-07-05 (scenarios W8–W17)
- Problem: scenarios W8 through W17 from the 140-scenario lifecycle review were
  deferred.
- Exit: work through W8–W17, landing fixes or recording deliberate no-ops.
- Related: [destroy-scoping-and-sweeps.md](destroy-scoping-and-sweeps.md)

## B-014 — Flow review 2026-07-09: 6 deferred findings
- Status: open
- Area: converge / flow
- Origin: 23-lane flow review 2026-07-09 (6 findings deferred of 49)
- Problem: six flow-review findings were deferred after ~43 landed.
- Exit: revisit the six deferred findings and resolve or close them.
- Related: [apply-task-graph-planning.md](apply-task-graph-planning.md)

## B-015 — Architecture recommendations 2026-07-06: Rec1 and Rec2
- Status: open
- Area: architecture
- Origin: arch-recs branch 2026-07-06 (Rec1, Rec2 deferred)
- Problem: two architecture recommendations were deferred after the others
  landed (cephadopt package, plan-builder split).
- Exit: evaluate Rec1 and Rec2 for landing now or record them as declined.
- Related: [package-boundary-contracts.md](package-boundary-contracts.md)

## B-016 — No reference example for the recommended boot-ISO install path
- Status: open
- Area: examples / managed-os
- Origin: managed-OS docs recommend the boot-ISO path
- Problem: `docs/advanced/managed-os.md` recommends the boot-ISO + profile
  `packageSource` path (~1 GB) over the full DVD (~10 GB), but no shipped example
  demonstrates it, so the recommended path has no runnable reference.
- Exit: ship a boot-ISO / `hostedTree` variant example (or a boot-ISO variant of
  a ceph-ibm lab) and reference it from the managed-OS docs.
- Related: [managed-os-media-staging.md](managed-os-media-staging.md)

## B-017 — No vSphere reference example
- Status: open
- Area: examples / vsphere
- Origin: examples substrate-coverage gap
- Problem: vSphere is a fully documented substrate but has no example tree; users
  have only `example init --provider vsphere` scaffolding.
- Exit: add a vSphere reference example, or point vSphere users at the scaffold
  from `docs/advanced/examples.md`.
- Related: [openshift-vsphere-agent-boot.md](openshift-vsphere-agent-boot.md)
