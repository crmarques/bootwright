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
| [M1](milestones/m1.md) | Foundations: substrates, infrastructure services and Machines | none | in progress | operator gates; X132's definition |
| [M2](milestones/m2.md) | Full IBM Ceph 9.9.x: virtual, physical and stretch | D156; applicable M1c/M1d/M1e | in progress | M2c's X61 (B484); B483 blocked; operator gates |
| [M3](milestones/m3.md) | Full OpenShift on libvirt+sushy, BMCs, KubeVirt, vSphere | M2; applicable M1c to M1g; B18/B23/B265; B83, B735, B750, B751 before B726; B84, B727, B758 before B729 | in progress | M3b operator gate |
| [M4](milestones/m4.md) | Cluster add-ons | M2; M3c; B64, B719, B726 where a package uses them | in progress | M4a accepted; M4b–e in-tree; live gates pending |
| [M5](milestones/m5.md) | Retired: moved to M3e (D172) | none | not started | nothing |
| [M6](milestones/m6.md) | Retired: moved to M4 (D173) | none | not started | nothing |
| [M7](milestones/m7.md) | Standalone native files and commands | M4 | not started | after M4: B51 |

- **Next:** an agent takes only an [active slice](#active-slices)'s open work
  or an explicit request; the operator takes operator gates and
  the open session clauses.

## Active slices

Slices with an item `in progress`.

| Slice | Milestone | Authorized by | State |
| --- | --- | --- | --- |
| X61 | M2c | D159 | in progress |
| X118 | M3c | D172 | in progress |
| X123 | M3d | D174, D181 | in progress |
| X143 | M4c | D180 | in progress |
| X189 | M1 | D190 | in progress |
| X194 | M1 | D192 | in progress |

## Scope rules

- Implement only the prompt-authorized outcome: an active slice's open work,
  or an explicitly requested slice or item, which authorizes only itself. No
  spec, page, item or decision authorizes implementation or effect.
- A slice is **active** only while its [row](#active-slices) reads
  `in progress`; milestones are never active. The WIP limit is at most two
  active slices per program, the slices one owner decision or request
  authorizes.
- **In sequence** means the next planned slice of the first milestone in
  Status order that holds open items; when it has none, planning its next
  slice is. D158 schedules shared M1 work with its first consuming checkpoint;
  future arms block no unrelated consumer. Anything else is
  out of sequence.
- Requires governs promotion and implementation, not definition. An unmet
  Requires needs a waiver that quotes the owner's explicit request and is
  recorded in its decision. A clause a session takes pending the owner's
  review binds until overturned, and an overturned clause reopens its work as
  a new item ([open session clauses](milestones/backlog.md#open-session-clauses)).
  An [envelope](project.md#product-scope)-only arm needs the owner's
  promotion, never a clause.
- Removing or narrowing any safety refusal in [specs](index.md) needs a named
  slice; cross-cutting safety constraints apply from the start.
- Record discovered work as an item on the page of the earliest milestone not
  done whose scope it blocks, or as a parked item, with owner, Kind, bounded
  outcome, deferral reason, Requires, Definition and exit evidence.
- `Specified` is ready for authorized implementation; `Needs definition` lists
  open decisions; `Candidate` is unpromoted; `Blocked` names its resumption
  condition. Promotion fixes the exact implementation and version, closes
  contract gaps and names executable exit evidence. Every item on a milestone
  page gates that milestone, whatever its Definition.

## Completion and verification

- Delivery status is `not started`, `in progress`,
  `awaiting operator acceptance`, `blocked` or `completed`; an item's Delivery
  cell holds one value, then its planned slice if it has one, and a deviation
  lives in its detail. An item completes when every exit gate passes; one
  whose in-tree gates pass awaits operator acceptance, or is `blocked` while a
  deviation stops its operator gate.
- An **in-tree gate** records its command and result in the delivering
  commit's body. Its tests are host-independent (no package manager, network, privilege,
  second OS or virtual machine) and qualify contracts, refusals and recovery,
  never an executed native installer.
- An **operator-run gate** records a summary row (date, build commit, command
  sequence, outcome) and the private archive's operation-log digest in the
  [acceptance ledger](../docs/acceptance.md), and completes its item only on
  an owner-accepted row that matches the item's acceptance baseline. An agent
  may record an acceptance the owner states explicitly (D165).
- Qualification tiers are in-tree, emulated rehearsal and real hardware; each
  destructive path names its tier, and each milestone page its exit tier.
- A gate passes only with the command and result that produced it; failed,
  skipped, flaky, unavailable and unrun gates are not passes.
- A milestone is **done** when its page holds no item and no planned slice,
  every milestone it requires is done, and owner-accepted ledger rows meet its
  exit tier where that tier is operator-run. The owner declares it in one docs
  commit that sets its Status Delivery to `done` and writes
  `Done <date> at <commit>` on its page. A done milestone takes no new item.
  Otherwise its Delivery is `in progress` once any of its items has started
  and `not started` before; D158 reopens M1 with its accepted baseline. A
  completed item has left its page, whose opening links the record of a
  slice delivered toward it.

## Identifiers

- **M\<n\>, milestone.** Sequence position; new milestones append. Exceptions:
  D128's renumbering and the submilestones of D131 (M2), D158 (M1), D172 (M3)
  and D173 (M4). Other IDs and dated history stay fixed.
- **B\<n\>, item.** One bounded outcome on exactly one milestone page, or
  parked in the backlog. The number encodes nothing and never changes: a new
  item takes one above the largest B under `specs/milestones/`, and a branch
  that collides with another renumbers the item that has not landed. Only open
  work receives a B; delivered work keeps the ID its record names.
- **X\<n\>, slice.** The ordered items of one milestone delivered together,
  with the Kind its Planned slices row declares; each commit's `Refs:` line
  names the slice and the items it advances. A fix with no planned slice takes the next X when it opens.
- **D\<n\>, decision.** An entry of the [backlog](milestones/backlog.md#decisions)
  log in numeric order, anchored `#d<n>`, whose status token says whether the
  owner decided, delegated or has yet to review it.
- **Q\<n\>, OD\<n\>, owner question.** A row of an open-decision table
  (`ID | Question | Options | Recommendation | Blocks | Status`) whose Status
  is open or names the answering D; Q1 to Q118 and OD1 to OD31 stay fixed,
  and a new Q takes the next number. Other numberings, such as the
  2026-10-11 review's Q1 or an audit's finding IDs, are local to their record.
- **Alias.** Each item row keeps its pre-2026-09-28 IDs and its dated move and
  split notes; an old ID found nowhere else is in the backlog's
  Retired table.
- A milestone page opens with its scope, Requires, Exit tier and Open
  decisions, then `Planned slices`, the `Items` table
  (`ID | Alias | Kind | Owner | Outcome | Requires | Definition | Delivery`)
  and one `### B<n>` detail section per item holding its bounded outcome,
  exit evidence and any deferral reason. Kind is `product`, `safety`, `defect` or
  `enabling`. A byte budget on the opening plus row-and-section routing is the
  design that replaces per-page cards: page size sets no reading cost.
- A moved item keeps its ID, and its row and section move with a dated Alias
  note. A split keeps the ID on the part that stays and gives each part split
  out a new B whose Alias names its source and date (a split made by the
  2026-09-28 reorganization names its source only); no B carries a qualifier
  such as "(rest)". A partly delivered item keeps its ID and states what
  remains. A dropped item is retired with reason and date. A completed item
  leaves its page for the record of the slice that delivered it.
