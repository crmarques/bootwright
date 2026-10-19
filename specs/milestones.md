# Milestones

Milestones own delivery status; specs claim no availability.
The [command catalog](../internal/cli/catalog.go) marks availability; other
listed commands return the [unavailable result](cli.md#recognized-but-unavailable-commands).

- [M1](milestones/m1.md) to [M7](milestones/m7.md): one page per milestone.
- [Delivered](milestones/delivered.md): completed work and its evidence.
- [Backlog](milestones/backlog.md): parked work, retired IDs and decisions.

## Status

| ID | Milestone | Requires | Delivery | Next |
| --- | --- | --- | --- | --- |
| [M1](milestones/m1.md) | Foundations: substrates, infrastructure components and Machines | none | in progress | X55/X60 for M2c (D159); M1f/M1g future |
| [M2](milestones/m2.md) | Full IBM Ceph 9.9.x: virtual, physical and stretch | D156; applicable M1c/M1d/M1e; B509 | in progress | M2c under D159 on branch `m2c` |
| [M3](milestones/m3.md) | Full OpenShift on bare metal/BMC and libvirt+sushy | M2; applicable M1c/M1d/M1e; B18/B23/B265 | in progress | B61's gate; define B473 |
| [M4](milestones/m4.md) | MetalLB and IBM Fusion Data Foundation | M2, M3 | not started | nothing until M2 and M3 |
| [M5](milestones/m5.md) | OpenShift clusters over OpenShift Virtualization | M3, M1g | not started | nothing until M3 and M1g |
| [M6](milestones/m6.md) | Add-ons ACM, Argo CD and GitLab | M4 | not started | nothing until M4 |
| [M7](milestones/m7.md) | Standalone native files and commands | M6 | not started | after M6: B51 |

[D158](milestones/backlog.md#decisions) reopens M1 as Foundations with
[M1a to M1g](milestones/m1.md#submilestones). The accepted hardening baseline
under [X98](milestones/delivered.md#x98--m1-owner-acceptance) and M2b under
[X99](milestones/delivered.md#x99--operator-selected-ceph-artifacts-and-m2b-completion)
remain delivered.

- **Next for agents:** [M2c](milestones/m2.md#m2c)'s lanes under D159 on the
  local branch `m2c`, with M1c's X55/X60 as its foundation gaps, each offered
  to main at its checkpoint (D164).
- **Next for operator:** review D159 to D165. Other provider and cluster
  gates keep their acceptance.

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
  slice is. D158 schedules shared M1 work with its first consuming product
  checkpoint, currently M2c; future arms do not block unrelated consumers.
  Anything else is out of sequence.
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
- An **operator-run gate** records a summary row (date, build commit, command
  sequence, outcome) and the private archive's operation-log digest in the
  [acceptance ledger](../docs/acceptance.md), and completes its item only on
  an owner-accepted row that matches the item's acceptance baseline. An agent
  may record an acceptance the owner states explicitly (D165).
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
  D158 explicitly reopens M1 while preserving its accepted hardening baseline.
  Its Delivery is otherwise `in progress` once any of its items has started,
  and `not started` before. An item that completed has left its page, so the
  page's opening then links the record of a slice delivered toward it.

## Identifiers

- **M\<n\>, milestone.** Sequence position; new milestones append. Exceptions:
  D128's unfinished-scope renumbering, D131's M2 submilestones and D158's
  [Foundations redefinition](milestones/m1.md#submilestones).
  Other IDs and dated history stay fixed.
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
