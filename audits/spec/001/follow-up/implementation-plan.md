# Implementation plan for the specification-audit remainder

Date: 2026-10-29. Baseline: `78d982988a3de0d78bbbab91e4edeb34a1a4adb8`.
The [review](report.md) and [232-finding ledger](findings.json) supply the
assessment. This plan covers all 17 partial findings, the one open finding,
and the deferred safety and validation work named in the executive summary.

The requested deliverable is this plan and a persistent review. The work
packages below are proposed implementation units, not active slices or
permission to run effects. Their W and A labels are local to this plan.
[Milestones](../../../../specs/milestones.md#scope-rules) remain the authority
for delivery state and authorization. The 29 explicit owner dispositions in
the review remain settled unless the owner changes them.

## Outcomes and completion standard

The work is complete when the 18 incomplete findings each have evidence of
implementation or a new explicit decision disposing of their remaining
recommendation; the carried safety items have their own exit evidence; and
operator gates have actual accepted results. A merged documentation change,
a moved item, a synthetic test and a live acceptance are recorded separately.

Use existing B items rather than reopening completed remediation lanes.
Before implementing a package without an existing carrier, create a bounded B
item in the earliest fitting milestone, or park it, and allocate its X slice.
For a partial carrier such as B431 or B99, split out only the authorized
outcome if needed and retain the parent item's unrelated work. Do not mark
B61 or B755 complete merely because their missing templates have been added.
The delivery record for preparing these documents does not activate any W.

## Recommended order

| Wave | Work | Dependency and purpose |
| --- | --- | --- |
| 0 | W07 contract corrections; W15 acceptance templates; W16 baseline authoring exercise; define W01/W02/W03/W04/W05/W06 | Correct misleading contracts and collect API evidence while safety designs are settled. The human half of W16 may remain pending without blocking independent work. |
| 1 | W01 shared-host guard, then W02 persistent ownership; W06 restore once identity/restore formats are agreed | Close shared-host hazards first. W02 and W06 share the identity design and must not edit or publish incompatible store formats concurrently. |
| 2 | W03 enrollment recovery; W04 OSD recovery; W05 online removal in bounded resource-family slices | Recovery and removal each use positively proved ownership. W05 consumes the agreed identity and device-proof contracts, not an assumption that all recovery paths completed. |
| 3 | W08 adapter schemas; W09 preflight boundary; W14 corpus cleanup | Can proceed alongside disjoint safety packages. Coordinate W08 with safety work touching the same roles; W14 follows the owning spec edits it would otherwise duplicate. |
| 4 | W10 omission metadata; W12 OS lifecycle; W13 plan vocabulary; then W11 final schema identity | Decide public changes together. Settle fingerprint semantics early; freeze final fingerprint fixtures after the approved schema changes. |
| 5 | W16 final authoring exercise and W18 Ceph live qualification; applicable operator gates of W01–W06 | Record observed results and remaining deviations. Run W18 earlier if a suitable explicitly authorized live window is available. |
| Consumer-gated | W17 vSphere credentials, within B99 | Definition can proceed now. Runtime integration and its live test wait for a real vSphere implementation; this plan does not create a second vSphere program. |

Work may run concurrently only across disjoint mutable code and state. In
particular, serialize ownership/store changes, serialize modifications to the
Ceph core worker, and coordinate edits to common API normalization and output
goldens. Program WIP follows the milestone scope rule.

## Decisions to resolve before dependent implementation

These are recommendations for review, not accepted decisions. Recording a
choice should name the relevant B/X items and amend the owning spec.

| Decision | Recommendation and alternative | Blocks |
| --- | --- | --- |
| A1: stopped foreign guests on a retiring network | Refuse removal if any foreign domain, including a shut-off one, still references the network or bridge; it otherwise loses its future start path. An alternative limited to running guests must explicitly accept that consequence. Refuse unreadable inventory under either choice. | W01 |
| A2: persistent identity and old stores | Mint a random logical context incarnation, persist it atomically and bind ownership proofs to it. Define a safe refusal/retirement path for old stores and old target marks; do not silently infer an incarnation from names or rewrite live ownership. Decide exact format/cut-over and how legitimate whole-store restore preserves identity. | W02, W06, W17 ownership integration |
| A3: partial Ceph enrollment | Continue only from matching frozen requests, durable per-host receipts and positive live FSID/host/daemon proofs. Unrecorded or contradictory native state remains unknown. Decide journal states, commit points and exact allowable transitions before changing the refusal. | W03 |
| A4: partial OSD metadata | Deliver actionable classification first; choose a supported repair procedure or a bounded journaled owned-clear path. Recommend automatic clearing only for narrowly enumerated shapes whose ownership and device identities can be proved; unknown/foreign metadata keeps refusing. A warning alone does not close the recovery outcome. | W04 |
| A5: online storage teardown | Specify per-family client quiescence/fencing, exact native inverses, data-loss authorization and the durable operator-stop boundary. Prefer incremental family slices with unimplemented families still refused. Native inverse behavior requires primary-source research and operator evidence before its safety claim. | W05 |
| A6: restore and counter rollback | Start with an explicit complete-store restore that validates a coherent snapshot and activates a fresh sealing key before writes. Decide whether arbitrary out-of-band rollback detection is in scope; counters inside the same restored snapshot cannot be their own independent rollback witness. An external monotonic anchor is a separate design choice, not silently assumed. | W06 |
| A7: API changes | Approve W10's omission categories, W11's public fingerprint fields and semantics, W12's explicit OS lifecycle arms, and W13's human/JSON/YAML output names together. Recommend a pre-1.0 coordinated cut-over with retired-field remedies, rather than introducing compatibility aliases without a requirement. Explicitly declining an optional recommendation is also a valid disposition. | W10–W13 |
| A8: example-status representation | Recommend one generated/tested qualification projection from existing evidence rather than another independently maintained status list. Settle the representation within the authorized W15 definition; no extra owner approval is required for this routine implementation choice. W07 and W14 follow the already accepted selection-template and ownership rules without reopening them. Declining an original recommendation instead of implementing it needs an explicit recorded disposition. | W15 representation only |

## Safety and recovery packages

### W01 — Prove shared-host network quiescence

**Carrier:** [B567](../../../../specs/milestones/backlog.md#b567).
**Owner:** Substrate, with State reconciliation. **Class:** safety.
**Ready after:** A1 and a named safety slice.

Replace the unconditional libvirt host quiescence result with a bounded,
read-only inventory of domains referencing each owned network or bridge. Keep
the existing cross-context claims; they do not replace live enumeration. Repeat
the guard immediately before destructive adapter calls, including resumed
removals. Reuse the apply-side inventory/proof mechanics where their contracts
match. Define the remaining external-writer race explicitly: local reservations
cannot lock an operator's independent libvirt client.

Primary touch points: `internal/substrate/libvirt/capability.go`, the
`substrate_libvirt_host` role and shared substrate modules, the owning
[removal contract](../../../../specs/substrates.md#provider-host-realization).
**Exit:** tests prove an unreadable inventory, running foreign guest, stopped
foreign guest under A1, and continuation all refuse without a destructive
call; an unused owned network removes successfully. A deliberate guard
removal must make the negative test fail. Qualify the network-use scenarios
on an explicitly authorized emulated host before recording live acceptance.

### W02 — Give owned resources a persistent context incarnation

**Carrier:** [B568](../../../../specs/milestones/backlog.md#b568).
**Owners:** Workspace, Substrate, Infrastructure services and Managed OS.
**Class:** safety. **Ready after:** W01 and A2; co-design with W06.

Distinguish the existing filesystem-derived `ContextIdentity.Incarnation`
from the persistent random logical identifier required for target ownership.
Freeze the logical identity in requests and bind it into libvirt identifiers,
emulated BMC and service markers, artifact ownership and installation proofs.
Inventory actual consumers before choosing affected contract digests. Coordinate
Ceph lineage only where it consumes the same identity; do not replace its FSID
proof by a context label.

Define initialization publication, restart behavior, corrupt/missing identity,
old-store handling and the boundary between identity-preserving restore and a
new context. Update all consumers as one coherent format transition.
**Exit:** a deleted/recreated same-name context cannot adopt old resources;
two controllers with matching names cannot claim each other's objects; foreign
marks refuse apply and destroy; crashes around initialization never expose a
partly assigned identity; continuation and intentional restore retain the
right identity. Contract-version and per-capability proof tests identify every
changed effect. Keep the existing orphan warning until the full path qualifies.

### W03 — Continue partially enrolled Ceph clusters

**Carrier:** [B659](../../../../specs/milestones/m2.md#b659).
**Owner:** Storage. **Class:** safety because recovery changes effect proofs.
**Ready after:** A3 and the applicable identity contract from W02.

Specify the enrollment journal before changing `partial-cluster`: request and
FSID binding, per-host intent/completion, daemon proof, lost replies and replay
boundaries. Resume only the next proved incomplete transition; retain unknown
for unexpected daemons, foreign FSIDs and missing receipts. Keep the native
worker's bounded execution and frozen-input guarantees.

**Exit:** interruption at every journal/native-effect boundary, after one or
several enrolled hosts, and before/after service completion resumes correctly;
missing or conflicting proof makes no write. Tests assert no duplicated
bootstrap/enrollment effect and unchanged finished-host evidence. An operator
rehearsal validates the supported interruption windows on the selected native
artifacts; the item stays awaiting acceptance until that evidence is accepted.

### W04 — Provide a supported partial-OSD recovery exit

**Carrier:** [B662](../../../../specs/milestones/m2.md#b662).
**Owners:** Storage and device ownership. **Class:** safety.
**Ready after:** A4 and the agreed receipt/device identity contracts.

Classify partial metadata shapes and return a specific `osd-partial` outcome
with a remedy that actually exists. If A4 selects a native clear, constrain it
to exact proved devices and this operation's durable intent, journal it, and
re-read postconditions before continuation. Foreign tags, ambiguous extents,
changed identities and unexpected metadata always refuse. W03 receipts may be
reused where they prove the same fact; enrollment success alone is no erase
authority.

**Exit:** each admitted/refused metadata shape has a test; unknown state runs
no destructive command; interruption on either side of clear is resumable;
unrelated devices and metadata remain unchanged. The LVM devices-file behavior
and selected native clear need an authorized live device rehearsal before the
supported recovery claim closes.

### W05 — Remove owned storage while hosts survive

**Carrier:** [B491](../../../../specs/milestones/m2.md#b491).
**Owners:** Storage and State reconciliation. **Class:** safety.
**Ready after:** A5 and applicable W02/W04 proofs; use existing online ordering.

Split implementation by concrete resource family and its consumers. For each,
prove/fence use, authorize the named data loss, remove by the recorded native
identity, prove confidential withdrawal and persist completion before the
operator-stop boundary. Preserve foreign resources and shared files. Destroy
never powers hosts off on the operator's behalf. Keep the current safe refusal
for each family until its inverse and proof are delivered.

**Exit:** active/unknown clients, unreachable hosts, replaced same-name objects,
lost-reply adoption and foreign files all exercise refusal or safe continuation.
Crash tests cover both sides of online cleanup and the stop boundary; no
Machine removal precedes native cleanup. Native ignored errors cannot become
success without independent absence proof. Include live-client, mixed-host and
consumer-retirement rehearsals; each family records its own acceptance scope.

### W06 — Restore complete stores without reusing rolled-back seal state

**Carrier:** [B103](../../../../specs/milestones/backlog.md#b103).
**Owners:** Workspace and Secrets. **Class:** safety.
**Ready after:** A2/A6 and a named restore definition; W02 defines logical identity.

Design snapshot coherence, whole-store validation, authorized physical
identity rebinding, provenance and exclusive publication. Rewrap or migrate
secret custody into a fresh active sealing key before restored state can write;
retain needed old decryption material without reusing it for new seals. Define
crash states and rollback of a failed restore transaction. Do not claim that
restored counters detect an arbitrary out-of-band rollback of themselves.

**Exit:** restore an older counter snapshot and demonstrate that a new seal
uses a fresh key; reject mixed/incomplete snapshots and wrong-store data;
preserve operation evidence, bindings and logical ownership; inject failures
before/after key publication and store activation. Prove retry cannot expose
mixed key/store state. The public restore workflow needs a separate accepted
contract and its operator gate; improving a warning alone cannot close B103.

## Contract, architecture and API packages

### W07 — Correct contracts and vocabulary without changing runtime behavior

**Findings:** F02-01, F02-06, F06-01, F06-12, F12-04.
**Owners:** Architecture, Machine, Add-ons and State reconciliation.
**Carrier:** new bounded documentation item; coordinate the adjacent
[B884](../../../../specs/milestones/m4.md#b884) corrections without marking it done
unless both of its stated defects are fixed.

Replace the obsolete Ceph selector, finish the BMC wording, define add-on,
package and instance once, and apply Selection/Implementations/Refusal table
sections to the affected domain pages. Describe GitOps stage-tail dependencies
as the existing same-stage rule; retain Q60 as pending. Widening dependency
scope would be a separate behavior decision and is not a wording fix.

**Exit:** owning pages agree with current schema and stage-tail tests;
`make docs-check` passes; review finds no contrary duplicate rule. Add an
executable check only for an invariant with a meaningful stable representation,
such as a selector schema path; do not add string-matching tests for prose edits.

### W08 — Complete the adapter schemas covered by the audit

**Finding:** V2-15. **Carrier:** the argument-schema portion of
[B431](../../../../specs/milestones/backlog.md#b431).
**Owners:** each affected adapter. **Class:** enabling unless behavior changes.

Declare the request members and auxiliary digest/scratch variables entry points
consume, type `nativeStaging` as an integer, and teach the schema checker the
supported type. Eliminate the corresponding exact allowances. Mirror choices
and bounds where the role owns admission; otherwise identify the owning frozen
request validator. Coordinate with W01–W05 roles and keep task behavior fixed.

**Exit:** undeclared-variable and nested-schema negative fixtures fail when a
required declaration is removed; the relevant allowance is empty; collection
unit, syntax and sanity suites pass. Remaining unrelated B431 defects stay open.

### W09 — Move shared preflight contracts below product domains

**Finding:** F07-07. **Owners:** preflight consumers and Substrate.
**Carrier:** new architecture item with a concrete second consumer.

Remove the Container cluster report alias and shared service dependencies on
Storage preflight. Place the common report/summary at the smallest neutral
owner; use consumer-owned structural interfaces for graph and ownership reads.
Keep allocation projection and provider observation behind the existing
Substrate capacity seam and composition registrations.

**Exit:** Storage and OpenShift preflight preserve their results without a
cross-domain import; a second provider test proves dispatch requires no
consumer edit. A provider without a real capacity observer reports unknown or
unsupported explicitly. Do not report KubeVirt capacity using libvirt
assumptions; a real KubeVirt observer is a separately bounded feature if chosen.
Architecture boundary checks and report/diagnostic tests hold the new ownership.

### W10 — Make omission semantics explicit and machine-readable

**Finding:** F03-06. **Owner:** Desired state. **Carrier:** new API item.
**Ready after:** A7, informed by the W16 baseline.

Inventory actual missing/null/empty/zero semantics. Recommend a small set of
field metadata categories, derived where possible from existing schema rules,
covering required, defaulted, inherited/derived, presence-selected and
unrealized input. Do not create a second defaulting engine. Resolve conflicts
with Secret input rules and explicit false/zero before coding.

**Exit:** representative fields exercise missing/null/empty/zero/false and
precedence through admission, normalization, schema and effective output. One
owner generates the explanatory schema fields; adding a new category requires
an exhaustiveness check. Existing valid behavior stays unchanged unless the
approved contract explicitly changes it.

### W11 — Publish a deterministic authoring-schema identity

**Finding:** F03-09. **Owner:** Desired state, with CLI and Workspace.
**Carrier:** new API/store item. **Ready after:** A7; finalize fixtures after W10/W12.

Define exactly what the fingerprint covers: registered public kinds, field
shapes, defaults, retired spellings and any manually versioned semantic rules
that cannot be derived safely. Recommend a digest over a canonical structural
manifest with an explicit treatment of validation/normalization semantics;
do not claim a structural hash captures arbitrary Go behavior. Expose the
chosen identity through version/schema output and retained input metadata,
and use it in actionable mismatch diagnostics.

**Exit:** ordering and formatting alone preserve identity, a semantic schema
change moves it, and a retained context reports the identity it froze rather
than the running build's. Test diagnostics and output goldens. The schema
fingerprint is descriptive until a separately specified compatibility rule
uses it; it must not replace lifecycle request digests or introduce a new
continuation refusal accidentally. Durable record changes take safety gates.

### W12 — Select Machine OS lifecycle through explicit intent

**Finding:** F04-05. **Owner:** Machine desired state and Managed OS.
**Carrier:** new product/API item. **Ready after:** A7 and the W16 baseline.

Specify explicit modes for an already installed OS, Bootwright installation,
and installation by a downstream cluster installer. Recommend presence arms
consistent with the existing union rule; choose exact field names during
contract definition. Keep `hardware.rootDevice` at its current owner. State
where `installProfileRef` is required or forbidden so its accidental omission
cannot silently choose another lifecycle.

**Exit:** every mode has a complete input and a contradictory/missing-field
refusal; unchanged intended modes produce equivalent plans, and an ambiguous
legacy shape receives a retired-field remedy. Update examples, effective
output and input-schema contracts together. Review frozen-request changes and
downstream input-chart impact; another repository changes only in its own task.

### W13 — Use block terminology consistently in plan output

**Finding:** F02-03. **Owner:** CLI, with State reconciliation.
**Carrier:** new output-contract item. **Ready after:** A7.

Recommend `block` for plan nodes in human output and structured field names,
keeping `step` only for genuine within-block progress and authored add-on
steps. Define whether each structured key changes and document the cut-over;
renaming human headings alone does not close a promise of uniform JSON naming.

**Exit:** plan text/JSON/YAML goldens agree with the chosen names, the API
add-on step shape remains correctly distinct, and help/examples follow them.
No lifecycle plan intent or content digest changes solely because a CLI label
changes. Inspect downstream machine consumers before approving field renames.

## Documentation and qualification packages

### W14 — Finish moving facts to their owners

**Findings:** F13-09, F15-04, F17-05, F17-08.
**Owners:** Controller, Workspace, Storage and specification maintenance.
**Carrier:** new documentation item. **Ready after:** relevant W07 and safety spec edits.

Choose one home for setup-subtree refusal/publication/retention rules and replace
duplicates with links. Move Ceph host-preparation safety mechanics into its
lifecycle owner and observed release-specific facts into indexed knowledge.
Keep acceptance state in milestones/ledger; use Not yet met only for missing
code. Preserve dated source evidence and IDs when moving text. The host-probe
relocation is structural; the review did not establish a runtime defect there.

**Exit:** no obligation is lost or weakened in the move; references and section
budgets pass; each moved fact has one owner; acceptance-only paragraphs no
longer pretend to describe missing implementation. Use existing documentation
gates and a focused before/after review, not new tests that mirror sentences.

### W15 — Supply acceptance templates and truthful example status

**Findings:** F17-07 and R2-05-08.
**Carriers:** template clauses of [B61](../../../../specs/milestones/m3.md#b61)
and [B755](../../../../specs/milestones/m4.md#b755); a new example-index item if needed.
**Owners:** Verification documentation, Container cluster and Add-ons.

Add the two blank row templates from the existing gate requirements now, and
link them from coverage. Include build, profile, version coordinates, command
sequence, drill outcomes, evidence digest and acceptance status; keep detailed
runtime evidence in the existing private archive boundary. No template is an
accepted run. Add the chosen per-example admission/plan/acceptance projection
under A8, linked to existing evidence and with explicit unrun states.

**Exit:** both templates can represent their milestone gates without invented
results. Example status agrees with current admission/plan checks and actual
accepted rows; a seeded false acceptance or missing example is rejected by the
projection's consistency check. B61/B755 remain awaiting their operator gates.

### W16 — Run the eight authoring tasks and act on the results

**Carrier:** [B857](../../../../specs/milestones/backlog.md#b857).
**Owner:** Desired state and operator experience.
**Ready after:** baseline preparation; repeat after approved W10–W13 changes.

Prepare synthetic scenarios for the audit's eight tasks: start an environment,
repair a wrong-class reference, predict omission defaults, choose an unsupported
implementation, narrow cluster scope, continue after a transient failure,
understand what destroy leaves, and recover from credential/host-key failure.
Run once with a human and once with an agent using only the specified validate
and render-effective JSON interface. Use simulated states for unauthorised
effects; a walkthrough is labelled as such and never reported as a live retry.

**Exit:** a dated results record reports completion, corrections, time,
questions and wrong assumptions separately for each participant and task.
Convert demonstrated design/remedy failures into bounded items; do not infer
success from unit tests or fabricate the human run. Use baseline evidence to
decide A7, and rerun affected tasks against the final implemented shape.

### W17 — Complete access-credential semantics with the vSphere arm

**Finding:** R2-04-01 remainder.
**Carrier:** [B99](../../../../specs/milestones/m1.md#b99).
**Owners:** Substrate and Secrets. **Class:** safety for binding/rebind behavior.

Specify `credentialsRef` as access material in the deferred arm now; preserve
its refusal until realization. During B99 implementation, expose it through
capability Secret references and reuse the recorded access-rebind exit. Resolve
B99's native credential transport, TLS, inventory and identity questions before
launching an adapter. Do not invent an unavailable provider solely to exercise
audit closure.

**Exit:** a rotated bound login permits destroy only through the explicit
rebind gate; effect material remains ineligible; the live target's vCenter/VM
identities are re-proved, and unverified transport or foreign identity refuses.
A fake-port contract test precedes B99's real-system credential-rotation gate.
The audit finding remains partial until those runtime conditions are delivered.

### W18 — Qualify Ceph recapture identity on native artifacts

**Carrier:** [B867](../../../../specs/milestones/backlog.md#b867).
**Owner:** Storage. **Ready after:** an explicitly authorized disposable live
cluster and an agreed evidence procedure.

Measure monitor-map creation identity across monitor addition/removal and
upgrade, then a new bootstrap under the same FSID, exactly as B867 requires.
Record artifact coordinates and observed identities in the private evidence
archive; publish only the accepted summary and digest. If the proposed value
fails the invariant, keep the finding open and define a different provable
identity before changing custody logic.

**Exit:** an accepted ledger row proves stability within one incarnation and
change across the deliberately recreated incarnation. A code change resulting
from the run requires its own regression and safety review; a plausible native
field name or a mocked response is not live qualification.

## Coverage matrix

| Incomplete finding | Work package | Existing carrier or placement on promotion |
| --- | --- | --- |
| F02-01 | W07 | new Machine/architecture documentation item |
| F02-03 | W13 | new CLI output-contract item |
| F02-06 | W07 | new Add-ons/glossary documentation item |
| F03-06 | W10 | new Desired-state API item |
| F03-09 | W11 | new Desired-state/Workspace item |
| F04-05 | W12 | new Machine lifecycle API item |
| F06-01 | W07 | new selector-documentation item; engine claim checks already delivered |
| F06-12 | W07 | same domain-selection documentation item |
| F07-07 | W09 | new preflight architecture item |
| R2-04-01 | W17 | B99 |
| V2-15 | W08 | B431 argument-schema portion |
| F12-04 | W07 | new ordering-wording item, coordinated with B884 |
| F13-09 | W14 | new setup-documentation consolidation item |
| F15-04 | W14 | same corpus item |
| F17-05 | W14 | same corpus item; structural relocation |
| F17-07 | W15 | B61 and B755 template portions |
| F17-08 | W14 | same corpus/knowledge item |
| R2-05-08 | W15 | new example-index item |

Carried implementation debt is W01/B567, W02/B568, W03/B659, W04/B662,
W05/B491 and W06/B103. Additional validation is W16/B857 and W18/B867.
These do not change the review's 232-finding count.

## Execution and closure protocol

For each promoted package, record the outcome and any owner decisions in its
owning milestone before implementation. Use a task worktree and coherent
commits under [code implementation](../../../../.agents/skills/code-implementation/SKILL.md).
Read the owning spec and symptom-indexed knowledge; verify current paths and
line references against the implementation baseline rather than copying this
snapshot's line numbers blindly.

Run a meaningful regression/contract probe, then `make focus`; integration
requires `make check`. Safety-class changes add independent review and
`make race`, plus the changed concurrent package if the target omits it.
Changes to effect identity move the applicable content digest and fixtures;
format or output changes declare their compatibility and refusal behavior.
A documentation-only package uses document checks and review without adding
runtime tests for prose.

Maintain a disposition table linked from each delivering record: original
finding, actual change or explicit decision, commits, exact commands/results,
remaining carrier and operator gate. Keep the review's assessment date and
commit explicit. Subsequent reassessments update this undated follow-up in a
new commit, preserving earlier versions in Git; delivery evidence remains in
the owning milestone. Finish by rechecking all 18 incomplete IDs and all eight
carried safety/validation items. Completion requires no silent deferrals and
no unrun gate represented as passed.
