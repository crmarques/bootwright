# Milestones

This file owns delivery status and the rules every milestone page follows;
specs claim no availability. The catalog in
[`internal/cli`](../internal/cli/catalog.go) marks each available command;
every other command `bootwright --help` lists returns the
[unavailable result](cli.md#recognized-but-unavailable-commands).

- [M1](milestones/m1.md) to [M7](milestones/m7.md): one page per milestone,
  with its scope, planned slices and open items.
- [Delivered](milestones/delivered.md): completed work, guard tests and
  constraints.
- [Backlog](milestones/backlog.md): parked items, retired IDs and owner
  decisions.

## Status

| ID | Milestone | Requires | Delivery | Next |
| --- | --- | --- | --- | --- |
| [M1](milestones/m1.md) | Hardening: security, code and architecture improvement, bug fixes | none | in progress | X34; B49's run |
| [M2](milestones/m2.md) | Native input files for `openshift-install` and the cephadm and ceph CLIs | none | not started | define B51 |
| [M3](milestones/m3.md) | Provision and destroy OpenShift clusters on bare metal | M1 | in progress | B61's operator gate |
| [M4](milestones/m4.md) | Provision and destroy IBM Ceph clusters on bare metal | M1, M2, M3 | in progress | B72's operator gate; B73 waits for its host-key repair |
| [M5](milestones/m5.md) | First add-ons: MetalLB ingress and IBM Fusion Data Foundation | M3, M4 | not started | nothing until M3 and M4 |
| [M6](milestones/m6.md) | Provision and destroy OpenShift clusters over OpenShift Virtualization | M3 | not started | nothing until M3 |
| [M7](milestones/m7.md) | Add-ons ACM, Argo CD and GitLab | M5 | not started | nothing until M5 |

No slice is active; X34 is planned; M1 is frozen (D48).

- **Next for agents:** open and deliver X34.
- **Next for operator:** on a clean build that contains X21, first destroy
  every context applied before X21 with the build that applied it (X21 and X29
  to X32 move request and record versions, the keyring format and the
  automation digest), then run
  `setup`. Then run [lab-rhel](../examples/lab-rhel/README.md#run-it) for
  [B72](milestones/m4.md#b72) and [lab-sno](../examples/lab-sno/README.md) for
  [B61](milestones/m3.md#b61), and record each in the
  [acceptance ledger](../docs/acceptance.md) as the
  [operator guide](../docs/operator-guide.md) describes.
  [B73](milestones/m4.md#b73)'s rehearsal waits for its host-key repair.

## Scope rules

- Implement only the prompt-authorized outcome: an active slice's open work,
  or an explicitly requested slice or item, which authorizes only itself. A
  spec, milestone page, item or decision authorizes no implementation or
  effect.
- A slice is **active** only while its Delivery is `in progress`, and only
  active slices count toward the WIP limit of two: one product, safety or
  defect slice and one enabling slice, at most one of them out of sequence.
  Milestones are never active.
- **In sequence** means the next planned slice of the first milestone in
  Status order that holds open items; when it has none, planning its next
  slice is. Anything else is out of sequence.
- Requires governs promotion and implementation, not definition. An unmet
  Requires needs a waiver, which needs an explicit user request and is recorded
  in the item with its date.
- Removing or narrowing any safety refusal in [specs](index.md) needs a named
  slice.
- Record discovered work as an item on the page of the earliest milestone not
  done whose scope it blocks, or as a parked item, with owner, Kind, bounded
  outcome, deferral reason, Requires, Definition and exit evidence.
- `Specified` is ready for authorized implementation; `Needs definition` lists
  open decisions; `Candidate` is unpromoted; `Blocked` names its resumption
  condition. Promotion fixes the exact implementation and version, closes
  contract gaps and names executable exit evidence. Every item on a milestone
  page gates that milestone, whatever its Definition.
- Deferred commands keep the unavailable result with no placeholder or adjacent
  effect. Cross-cutting safety constraints apply from the start.

## Completion and verification

- Delivery status is `not started`, `in progress`,
  `awaiting operator acceptance`, `blocked` or `completed`; an item's Delivery
  cell holds one value, then its planned slice if it has one, and a deviation
  lives in its detail. An item completes when every exit gate passes; one
  whose in-tree gates pass awaits operator acceptance, or is `blocked` while a
  deviation stops its operator gate.
- An **in-tree gate** records its command and result in the delivering
  commit's body, and the slice summarizes it. Its tests are unitary and
  host-independent (no package manager, network, privilege, second OS or
  virtual machine) and qualify contracts, refusals and recovery, never an
  executed native installer.
- An **operator-run gate** records date, build commit and source stamp, command
  sequence, outcome and operation-log digest in the
  [acceptance ledger](../docs/acceptance.md), and completes its item only on
  an owner-accepted row that matches the item's acceptance baseline.
  [Development](../docs/development.md) lists the harnesses.
- Qualification tiers are in-tree, emulated rehearsal and real hardware; each
  destructive path names its tier, and each milestone page its exit tier.
- A gate passes only with the command and result that produced it; failed,
  skipped, flaky, unavailable and unrun gates are not passes.
- A milestone is **done** when its page holds no item and no planned slice,
  every milestone it requires is done, and owner-accepted ledger rows meet its
  exit tier where that tier is operator-run. The owner declares it in one docs
  commit that sets its Status Delivery to `done` and writes
  `Done <date> at <commit>` on its page. A done milestone takes no new item.
  Its Delivery is otherwise `in progress` once any of its items has started,
  and `not started` before. An item that completed has left its page, so the
  page's opening then links the record of a slice delivered toward it.

## Identifiers

- **M\<n\>, milestone.** A positive integer, never reused or suffixed; its
  number is its place in the delivery sequence. A new milestone is appended.
- **B\<n\>, item.** One bounded outcome on exactly one milestone page, or
  parked in the backlog. The number encodes nothing and never changes: a new
  item takes one above the largest B under `specs/milestones/`, and a branch
  that collides with another renumbers the item that has not landed. Only open
  work receives a B; delivered work keeps the ID its record names.
- **X\<n\>, slice.** The ordered items of one milestone delivered together,
  with the Kind its Planned slices row declares; each commit's `Refs:` line
  names the slice and the items it advances. A fix with no planned slice takes the next X when it opens.
- **D\<n\>, decision.** One dated log in [backlog](milestones/backlog.md#decisions),
  appended when the owner decides.
- **Alias.** Each item row keeps the IDs it had before 2026-09-28 and its dated
  move and split notes; an old ID found nowhere else is in the backlog's
  Retired table.
- A milestone page opens with its scope, Requires, Exit tier and Open
  decisions, then `Planned slices`, the `Items` table
  (`ID | Alias | Kind | Owner | Outcome | Requires | Definition | Delivery`)
  and one `### B<n>` detail section per item holding its bounded outcome,
  exit evidence and any deferral reason. Kind is `product`, `safety`, `defect` or
  `enabling`.
- A moved item keeps its ID, and its row and section move with a dated Alias
  note. A split keeps the ID on the part that stays and gives each part split
  out a new B whose Alias names its source and date (a split made by the
  2026-09-28 reorganization names its source only); no B carries a qualifier
  such as "(rest)". A partly delivered item keeps its ID and states what
  remains. A dropped item is retired with reason and date. A completed item
  leaves its page for the record of the slice that delivered it.
