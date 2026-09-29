# Backlog

Items attached to no milestone, the IDs the 2026-09-28 reorganization retired,
and the owner decisions the milestone pages cite. Open work lives on the
milestone pages [M1](m1.md) to [M7](m7.md); [milestones](../milestones.md)
owns the rules. [Product non-goals](../project.md#design-priorities-and-non-goals)
stay excluded: reconciliation, adoption, force behavior and day-2 mutation need
an explicit product or state-contract change before candidacy. Stage selection
is not one of them, because it gates which blocks an invocation starts and
leaves the plan, the lifecycle unit and ownership complete.

## Parked

A parked item gates no milestone and authorizes nothing. Attaching one to a
milestone page is an explicit request, recorded with its date in the item's
Alias cell.

| ID | Alias | Kind | Owner | Outcome | Why parked |
| --- | --- | --- | --- | --- | --- |
| [B94](#b94) | F10 | enabling | Controller and Workspace | Delete the dead setup-binding and legacy prerequisites layer | Pruned from M1 on 2026-09-28 so M1 can finish |
| [B95](#b95) | F11 | enabling | CLI | Help and usage polish | Pruned from M1 on 2026-09-28 |
| [B96](#b96) | Y3 | enabling | Architecture | Collection metadata and naming polish | Pruned from M1 on 2026-09-28 |
| [B97](#b97) | A8 (kind tables) | enabling | Desired state and Workspace | Kind field-table parity and a path checker | Pruned from M1 on 2026-09-28 |
| [B98](#b98) | C30 | enabling | Architecture | Production comments reduced to what the clarity contract keeps | Pruned from M1 on 2026-09-28 |
| [B99](#b99) | C1 (vSphere) | product | Substrate | One vSphere provisioning variant | No milestone names vSphere |
| [B100](#b100) | C2 (LoadBalancer) | product | Infrastructure services | One managed `LoadBalancer` lifecycle | No consumer; M5's MetalLB is an in-cluster add-on |
| [B101](#b101) | C6 (rest) | product | CLI | One more view of evidence or access, or a dashboard | No milestone names a view or dashboard |
| [B102](#b102) | C8 | product | Custom automation | One typed, invertible executable playbook journey | M7's packages are add-ons, not playbooks |
| [B103](#b103) | C14 | safety | Workspace and Secrets | Explicit complete-store restore | No milestone needs backup and restore |
| [B104](#b104) | C15 | safety | Secrets | Bounded lifetime allocation for secret reservations | Waits on B103; no defect drives it |
| [B105](#b105) | C19 | product | Secrets | One more secret-store implementation | `local-keyring` meets the current scope |
| [B106](#b106) | M4 (day-2 surface) | product | Container cluster | The day-2 surface a completed cluster exposes | A product non-goal |
| [B107](#b107) | new, 2026-09-28 | product | Add-ons and Storage | Data Foundation over an external Ceph cluster | M5 integrates a managed Ceph cluster (D19) |

### B94

Delete the dead setup-binding and legacy prerequisites layer, and share one
retained-native matcher between preflight and the controller stage. **Exit
evidence:** preflight and stage tests over one closure.

### B95

Help and usage polish, and the request fields `inventory.ListRequest.Silent`
and `contexts.CurrentRequest.Short` that only the CLI's result writer reads
through their flags, which `TestEveryRequestFieldIsRead` allowlists. **Exit
evidence:** CLI help goldens.

### B96

Collection metadata and naming polish, including the collection's sanity
README, whose `missing-gplv3-license` note names only documentation stubs
although the ignore file also exempts the real modules with no action plugin
(found in X19). **Exit evidence:** `./scripts/ansible-check`.

### B97

Kind field-table to `Shape` parity and the `Value.Get` path checker. **Exit
evidence:** parity tests over every kind.

### B98

Reduce production comments to what the
[code clarity contract](../architecture.md#self-explanatory-code-and-retained-knowledge)
retains. Most production comments state rationale the contract sends to the
knowledge catalog, and some state invariants that must survive as tests or
names rather than prose. Deciding each one is the work; a blanket strip would
lose the findings the catalog is meant to keep. **Exit evidence:** each
retained comment justified by language, tooling or a maintained contract;
every durable finding moved into `.agents/knowledge/` with its links and
evidence; and a fitness gate that holds the result.

### B99

One vSphere machine-provisioning variant. An arm is a capability package plus
one case of the [target derivation](../substrates.md#selection-and-refusal),
and no consumer of a realized Machine changes; its KubeVirt counterpart is
[B87](m6.md#b87). **Exit evidence:** the exact release, adapter contract, its
two fixed identity and pre-boot task files, failure and replay tests and
real-system qualification; [refused machine arms](../deferred/machine-arms.md)
records the admitted arm.

### B100

One managed `LoadBalancer` lifecycle over the shared managed-service capability
M1f delivered; its `Registry` half is [B68](m3.md#b68). **Exit evidence:** a
typed port, the exact implementation, lifecycle evidence, and failure and
acceptance tests.

### B101

One additional view of available evidence or explicit access, or a dashboard or
completion extension. Its custody half is [B10](m1.md#b10). **Exit evidence:**
a complete human and machine journey, diagnostics, safety and end-to-end
tests; a cluster inspection or access item also meets the cluster evidence
below.

When a cluster inspection or access item is promoted, its exit evidence
exercises the [cluster discovery](../cli/output.md#cluster-discovery) and
[applicability](../cli/commands.md#cluster-command-applicability) contracts
across OpenShift, OKD, managed Ceph and external Ceph. Cover explicit and
current contexts, selected and excluded names, each applicable and inapplicable
command, missing access metadata or artifacts, unavailable implementations,
context-state and identity refusals, and stable diagnostics and streams. Verify
node-name, FQDN and role-ordinal precedence, declaration-order independence,
multi-role Ceph nodes, `infra` preservation, duplicate-FQDN refusal, and
invalid or out-of-range selectors. Prove inspection uses only permitted
metadata, inapplicable access reads no credential bytes, failed access emits no
descriptor or sensitive result, and successful handoff or export preserves its
payload and output boundary. These tests depend on the promoted use case and do
not expand M1a availability.

### B102

One typed, invertible executable custom-playbook journey. The reserved schema
cannot prove effects, ownership or non-exfiltration; it needs M1e, the plan and
execution extensions of [B62](m3.md#b62) to [B64](m3.md#b64) and a named
journey. **Exit evidence:** a same-change API replacement of the
[reserved shape](../deferred/custom-playbooks.md), immutable source and
dependencies, exact targets, bounded secrets, authorization, continuation,
failure injection and isolated-runner qualification.

### B103

Explicit complete-store restore with logical identity preservation. Copy
restoration changes physical identities and may roll back seal reservations;
[M1c](delivered.md#m1c--context-secret-management) converts no earlier keyring
format and refuses it instead, and offers no backup or restore command.
Controller relocation, left over from C16, waits here too. **Exit evidence:**
coherent snapshot validation, authorized inode rebinding, a fresh key before
writes after rollback, interruption and retry and wrong-store refusal tests;
lifecycle recovery evidence preserved.

### B104

Replace per-ID secret reservations with bounded lifetime allocation. Current
opaque random version and binding IDs retain historical reservation files; a
new allocation scheme must preserve issued-ID non-reuse across crashes and
restore, so it needs [B103](#b103)'s restore semantics. **Exit evidence:**
bounded allocator state, reservation before use, counter and namespace
exhaustion, migration of existing bindings and failed attempts, non-reuse and
crash tests.

### B105

One additional secret-store implementation: a passphrase-protected store, an
external broker or KDF-based custody. **Exit evidence:** the shared
conformance suite, the session-material contract, rotation, tamper refusal and
non-disclosure tests.

### B106

The day-2 surface a completed cluster exposes, beyond bootstrap, continuation
and removal. It stays parked until a product-scope change admits it. **Exit
evidence:** set by that change.

### B107

OpenShift Data Foundation, through IBM Fusion Data Foundation, integrating an
OpenShift cluster with a Ceph cluster Bootwright does not manage. M5 integrates
the Ceph cluster M4 manages instead (D19). **Exit evidence:** set when it is
attached to a milestone.

## Retired

IDs no longer issued. Where an item took one over, its line here names that
item, whose Alias cell names the old ID too. Searching an old ID over
`specs/milestones/` finds its item's Alias cell, its record in
[delivered](delivered.md), or its line here.

| Old ID | Retired on | Reason and record |
| --- | --- | --- |
| V1 | 2026-09-28 | Answered by X14: the identity is the build's trust anchor (S18) and the image is fetched through the listener with its certificate verified (S17). |
| V2 | 2026-09-28 | Folded into [B72](m4.md#b72)'s operator gate. |
| V3, V4 | 2026-09-28 | Folded into [B61](m3.md#b61)'s operator gate. |
| V5 | 2026-09-28 | Folded into the operator gates of B61 and B72: a row accepted before X19 lands is repeated on a build that contains it. |
| S13 | 2026-09-28 | Delivered by [X15](delivered.md#x15--installs-that-neither-strand-nor-over-report); its row was left behind. |
| T1 | 2026-09-28 | Delivered by X15 (cluster goldens) and [X17](delivered.md#x17--goldens-and-checkpoint-harnesses) (records, commands and examples); no bounded remainder was named. |
| C13 | 2026-09-28 | No milestone needs a redistributable release. |
| O9 | 2026-09-28 | It measures the repository's guidance, not the product. |
| C16 | before 2026-09 | Delivered by [M1d](delivered.md#m1d--controller-setup) and [M1e](delivered.md#m1e--lifecycle-engine-and-managed-artifact-serving); controller relocation is [B103](#b103). |
| C17 | before 2026-09 | Promoted into M1h, now [B72](m4.md#b72), as its host-wide media store. |
| C18 | 2026-09-28 | Delivered by `d0980fc3` (refactor(architecture): align packages with the command and port map), which removed its row from the milestones page with no note. |
| C27 | before 2026-09 | Delivered by [X3](delivered.md#x3--machine-ssh-sessions-and-host-trust). |
| R1 | 2026-09-28 | Merged into [B19](m1.md#b19) with C28, whose outcome it was. |
| M2a, M2b, M3, M4, M5, M6 | 2026-09-28 | These rows became items whose Alias cells name them; the numbers M3 to M6 now name milestones. |
| M1h, M5a, M4a, M1i | 2026-09-28 | Became [B72](m4.md#b72), [B73](m4.md#b73), [B61](m3.md#b61) and [B71](m3.md#b71); M-letter IDs are no longer issued. |
| N1 to N5, L2, L4, L5, L6 | 2026-09-28 | Became items whose Alias cells name them; L1 and L3 never existed. |
| K01 to K73 | 2026-09-28 | The audit plan's cluster tokens, defined nowhere in the repository; the grouping stays in Git history. |
| Phase 0 to Phase 4 | 2026-09-28 | The audit plan's ordering; slices carry order now. |
| M1h's (D1) to (D6) | 2026-09-28 | Capability labels that collided with the decision log; they became prose. |
| D13, D15 | never issued | Recorded so nobody looks for them. |

## Decisions

Owner decisions, dated. An item waiting on one not yet taken names it in its
Requires cell as `owner decision: <question>`.

- **D1** (2026-09-26): the substrate owns the machine port as role entry points.
- **D2** (2026-09-26): lifting the physical refusal needs in-tree tests and an
  emulated rehearsal, with real-hardware acceptance before support is claimed.
- **D3** (2026-09-26): the BMC certificate is imported where the BMC supports
  it, otherwise an explicit per-machine disable-verification exception applies,
  never a default, and controller-to-BMC TLS gains a CA bundle.
- **D4** (2026-09-26): administrator access is kept in context custody with
  `cluster kubeconfig`.
- **D5** (2026-09-26): every node presenting this block's own tokenized image
  proves a partial install.
- **D6** (2026-09-26): the first proved UUID and serial are pinned and a
  mismatch refuses.
- **D7** (2026-09-26): non-root SSH accounts refuse at admission for now.
  Read on 2026-09-29 by the session, pending the owner's review, as applying
  to lifecycle placement hosts: the machines managed services and libvirt
  providers name (X22).
- **D8** (2026-09-26): the cluster identity is the build's kubeconfig trust
  anchor, refined on 2026-09-27 (S26) to its admin client certificate, which
  the installer's install-complete rewrite keeps.
- **D9** (2026-09-26): multi-node libvirt stays admitted under F14.
- **D10** (2026-09-26): ansible-core is the latest patch of the qualified 2.21
  minor.
- **D11** (2026-09-26): postcondition decisions stay in Go.
- **D12** (2026-09-26): media completion claims the inputs digest and installer
  version, not an image digest.
- **D14** (2026-09-26): documentation stays in the bundle but leaves the
  automation digest.
- **D16** (2026-09-26): managed-OS waits are frozen as request budgets.
- **D17** (2026-09-28): M3 and M4 are done only with two owner-accepted
  acceptance-ledger rows each: an emulated rehearsal on libvirt guests behind
  sushy-tools Redfish BMCs, and a run on real hardware. Each item keeps the
  gate its own detail names.
- **D18** (2026-09-28): the product scope names MetalLB, IBM Fusion Data
  Foundation, Red Hat Advanced Cluster Management, Argo CD and GitLab as the
  first add-on packages.
- **D19** (2026-09-28): M5 integrates OpenShift with a Ceph cluster M4 manages;
  an external Ceph cluster is parked as [B107](#b107).
- **D20** (2026-09-29, taken by the session under the owner's instruction to
  proceed with recommendations; pending review): the controller CPython is
  the latest patch of the newest minor the qualified ansible-core supports as
  a controller, and `latest` ignores a newer minor (X22).
