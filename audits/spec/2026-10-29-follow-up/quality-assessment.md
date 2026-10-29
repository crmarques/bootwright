# Quality of the addressed audit work

Assessed 2026-10-29 against `4510d73b6`, which preserves the original
[disposition review](review.md). The product code is unchanged from that
review's `78d982988` baseline. This supplement evaluates the quality of work
behind the 185 addressed findings and 29 explicit decisions; it does not
change their dispositions or close any of the 18 incomplete findings.

## Executive assessment

**The sampled remediation is substantial and technically strong. Confidence
is highest in deterministic contracts and failure handling, and lower in
operator usability and live behavior.** The work includes actual production
changes, negative regression tests and shared enforcement. It is more than a
documentation exercise. However, the closure count combines implemented
corrections, accepted design choices and warnings about deferred work; those
deliver different kinds of assurance.

- **Implementation quality:** strong in the inspected API and lifecycle paths.
  Tests frequently check forbidden effects, conflicting inputs, durable
  continuation and observed postconditions, rather than just successful output.
- **Architecture and maintainability:** improved through registries, shared
  contracts, typed boundaries and tests that detect stale exceptions. Synthetic
  implementations demonstrate selected extension seams, not full production
  portability.
- **Verification quality:** good in-tree regression protection, with tests of
  the enforcement tools themselves. Some guarantees still depend on an author's
  judgment, and passing structural checks does not establish semantic correctness.
- **Documentation and decisions:** generally sound at their accepted scope.
  Limitations and ownership are clearer, but existing exceptions and the
  original partial findings prevent a claim of complete consistency.
- **Operational evidence:** incomplete. B857's authoring tasks and B867's Ceph
  identity experiment remain unrun; several safety protections remain deferred.
  These gaps limit confidence without implying that every delivered fix is poor.

## Method and confidence

Each conclusion considers five questions: does the remedy fit the accepted
contract; does production code implement it; do tests exercise the relevant
failure boundary; does the change reduce duplicated ownership and future drift;
and what evidence exists outside synthetic tests?

The judgments are qualitative, not a numeric score:

| Judgment | Meaning |
| --- | --- |
| Strong in-tree evidence | The inspected implementation addresses the stated defect and meaningful regression tests cover its boundary. This says nothing by itself about live acceptance. |
| Sound at accepted scope | The requested documentation, process or design correction is supported and proportionate. No missing runtime implementation is credited. |
| Qualified | Useful work exists, but a consequential limitation, deferred protection or unvalidated assumption bounds the conclusion. |
| Not yet demonstrated | The required operator or live evidence is absent. This is an evidence gap, not a finding that the implementation fails. |

All 214 addressed/decision entries were considered at catalog level. Deeper
inspection was selected for destructive effects, durable state, cross-cutting
API/architecture changes and checks intended to prevent recurrence. The tables
below identify those samples and their evidence. Other entries retain the
finding-level assessment in [findings.json](findings.json); they have not each
received a fresh independent code audit or a quality grade.

| Catalog sections | Addressed | Explicit decisions | Review scope |
| --- | ---: | ---: | --- |
| S3–S5: scope, vocabulary, API and CLI | 37 | 21 | Catalog review plus selected schema, admission, output and placement implementation/tests. |
| S6–S7, S10: architecture, adapters and agent guidance | 52 | 1 | Catalog review plus selected registry, capability, selection and tooling implementation/tests. |
| S8: lifecycle and safety | 81 | 5 | Catalog review plus selected destructive-operation, recovery, identity and credential boundaries. |
| S9: documentation and delivery organization | 15 | 2 | Catalog review plus owning documentation and enforcement tests, including seeded violations. |
| Total | 185 | 29 | 214 entries; no extrapolated per-finding quality count. |

## API, CLI and design decisions

| Findings sampled | Quality assessment and evidence | What the evidence does not establish |
| --- | --- | --- |
| F02-04, F03-01, F02-09, F03-07 | **Strong in-tree evidence.** [Catalog tests](../../../api/v1alpha1/catalog_docs_test.go) compare registered kinds with documentation and output order. `TestEveryUnionFollowsTheOneRule` in [union tests](../../../test/architecture/unions_test.go) rejects synthetic rule violations. [Enum](../../../test/architecture/enum_casing_test.go), [reference](../../../test/architecture/reference_suffix_test.go) and [scope](../../../api/v1alpha1/scope_test.go) guards reduce independent inventories and detect stale exceptions. | Structural consistency does not establish that the chosen model is easy to author. |
| R2-05-01, F05-04, F05-09, R2-02-06, F16-01 | **Strong implementation of an honest availability boundary.** [Unrealized declaration tests](../../../cmd/bootwright/unrealized_linux_amd64_test.go) compare the table, warning identity/severity and actual plan refusal, with coverage of every listed declaration. [Unavailable-command tests](../../../cmd/bootwright/unavailable_commands_test.go) check stub wiring, failure status and diagnostics. | Clear warnings and refusals do not implement vSphere, reserved add-ons or custom playbooks. |
| F03-02, F03-03, F04-01, R2-05-05, V2-02 | **Strong in-tree evidence.** [Compiler tests](../../../internal/desiredstate/compilation/compiler_test.go) check skipped-secret warnings and remedies. [Machine admission tests](../../../cmd/bootwright/machine_admission_test.go) check arm conflicts, derived defaults and authored overrides. [Entitlement-default tests](../../../internal/desiredstate/compilation/entitlement_defaults_test.go) verify that defaults do not introduce arms forbidden by the selected type. [Ceph normalization](../../../internal/storage/cephcluster/normalization.go) and [topology admission](../../../internal/storage/cephcluster/topology.go) implement the safe access default and native-mode refusal. | Synthetic declarations and catalogs do not qualify native providers. |
| V2-19, F04-11 | **Strong in-tree evidence.** [Block identity tests](../../../internal/reconciliation/block_identity_test.go) exercise deterministic bounded identities and collision refusal naming both objects. One shared rule is more maintainable than service-specific name limits. | These tests do not prove every external service accepts every otherwise valid authored name. |
| F05-06 | **Strong behavioral correction.** [Placement tests](../../../internal/containercluster/agentinstall/placement_test.go) freeze labels/taints, reject missing or changed completion evidence, preserve unrelated manager taints and ensure read-only diff reports a missing label without restoring it. [Evidence validation](../../../internal/containercluster/agentinstall/evidence.go) enforces the postcondition. | Synthetic evidence/runners do not establish native installation or live cluster acceptance. |
| F03-05 | **Sound alternative, with qualified usability evidence.** [Origin tests](../../../cmd/bootwright/render_origins_linux_amd64_test.go) check text and JSON provenance; [schema-export tests](../../../cmd/bootwright/json_schema_linux_amd64_test.go) check examples, malformed input classes, defaults and bounded export size/depth. | The schema is deliberately advisory; it omits semantic constraints and uses an in-repository subset checker in its tests. General editor interoperability and operator task success are not demonstrated. |
| F01-01, F03-01, F04-01, F05-03, F18-02 and the other explicit decisions | **Sound at accepted scope.** The [decision record](../../../specs/milestones/backlog.md#d183) makes retained service implementation fields, narrow substrate arms, family-specific kinds and other choices traceable. Existing guards enforce several of those choices. | Choosing a defensible alternative does not prove it is easier to use. B857 remains the relevant empirical exit, and retained/deferred functionality gains no implementation credit. |

The main weakness is evidence of the user benefit: [B857](../../../specs/milestones/backlog.md#b857)
explicitly records that the eight human/agent authoring tasks were not run
before the API reshape landed. Contract consistency is demonstrated more
strongly than usability. A quality-focused follow-up should execute those
tasks and record completion, corrections, time and wrong assumptions before
claiming the API is simpler in practice.

## Lifecycle, safety and recovery

| Findings sampled | Quality assessment and evidence | Remaining limit |
| --- | --- | --- |
| F10-01, F10-02, F10-05 | **Strong in-tree evidence.** [Installer target-proof tests](../../../internal/managedos/installation/target_proof_test.go) execute the rendered Bash against synthetic sysfs and mocked device reads: wrong/missing disks, partitions, MAC mismatches and unreadable identity must stop before storage changes. [Power tests](../../../internal/machine/power/service_test.go) check refusal before runtime entry for unreadable proof, missing identity and the declared controller Machine, plus propagation of the adapter's controller-host refusal. | Executing the rendered proof is stronger than a text snapshot, but still uses synthetic hardware. The Anaconda proof does not establish the same inside-installer protection for OpenShift Agent; its [disk-safety limitation](../../../.agents/knowledge/openshift-agent-disk-safety.md) remains relevant. |
| CX-R5, CX-X1, F09-02 | **Strong failure-oriented corrections.** [Inconclusive-removal tests](../../../internal/reconciliation/lifecycle/inconclusive_removal_test.go) retain unknown observations, prevent replacement registration and constrain reread/replay. [Disk-boot intent tests](../../../internal/managedos/installation/disk_boot_intent_test.go) model recovery before and after installation proof, material withdrawal, power-off and disk boot. [Registration tests](../../../internal/reconciliation/lifecycle/registration_test.go) exhaust apply budgets while preserving a working removal reserve. | [Adapter ordering tests](../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/test_managedos_disk_boot_intent.py) inspect task structure; they do not execute an installer. In-tree recovery evidence is not a live failure drill. |
| R2-02-01, F10-04, R2-06-01, R2-06-04, R2-06-06 | **Strong guards within their coordination boundary.** [Consumer-survival tests](../../../internal/reconciliation/lifecycle/consumer_survival_test.go) refuse before registration, probing or destruction. [Shared-claim tests](../../../internal/substrate/libvirt/shared_claims_test.go) and [system-claim tests](../../../internal/workspace/contextfs/system_claim_linux_amd64_test.go) cover bridge/BMC claims, interrupted removal and host aliases sharing a pinned key. | Claims coordinate the same store. [Libvirt host quiescence](../../../internal/substrate/libvirt/capability.go) still relies on this context's Machines; B567's foreign-domain discovery remains absent. This is a real guard, not complete shared-host safety. |
| F13-07, F13-02, F13-06 | **Strong observable-state tests.** [Root filesystem tests](../../../internal/workspace/contextfs/root_filesystem_linux_amd64_test.go) refuse volatile filesystems before publication. [Context service tests](../../../internal/workspace/contexts/service_test.go) change evidence during confirmation and assert no write. [Controller-rebind tests](../../../internal/workspace/contextfs/controller_rebind_linux_amd64_test.go) check preserved bindings, copied-directory refusal and interruptions around durable rename. | Filesystem types are injected; reboot durability is not tested. Supported rebind is not general restore or arbitrary filesystem-identity recovery. |
| F11-03, F11-08, F10-06 | **Precise trust and confidential-input boundaries.** [Enrollment tests](../../../internal/trust/enrollment/service_test.go) ensure missing unattended fingerprints and mismatched supplied fingerprints cannot be recorded. [Private URL tests](../../../internal/managedos/medialocal/url_file_test.go) bound input and prevent disclosure in errors. [Elevation tests](../../../internal/controller/privilege/elevation_test.go) refuse stdin-logging policy. [Authorization tests](../../../internal/reconciliation/lifecycle/authorization_test.go) prevent prompts, mutations and effects for stale/unbound data-loss tokens. | Deployed sudo/PAM policies are not exhaustively qualified. The documented distinction between keyring custody and material retained on a native host does not erase that material on abandonment. |
| R2-04-08 | **Good implementation safeguards; native identity assumption not yet demonstrated.** [Produced-identity tests](../../../internal/secrets/localkeyring/produced_identity_linux_amd64_test.go) check retention, invalid/unproved identity refusal and unchanged storage. [Ceph module tests](../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_ceph_core.py) validate timestamps, FSID matching and evidence shape. | The tests fabricate monitor-map timestamps. They do not prove stability through membership changes/upgrades or a change on same-FSID rebootstrap. B867 explicitly owns that live experiment. |

Three addressed findings especially need a different quality judgment from
the implemented guards above:

| Finding | Quality of the delivered work | Protection still absent and consequence |
| --- | --- | --- |
| F10-03 | **Sound interim documentation.** [Ownership wording](../../../specs/state-reconciliation.md) states the name-derived identity boundary and carries B568. | Persistent incarnation identity is absent; recreating a context with the same name does not gain the stronger ownership separation the original safety concern sought. |
| CX-R4 | **Sound restore-boundary clarification.** [Context restore documentation](../../../specs/contexts.md) identifies the matching-whole-store boundary, no supported restore command, and B103. | Rolled-back seal counters are not detected and a fresh encryption key is not established. Honest disclosure does not supply rollback protection. |
| CX-API04 | **Sound correction of an overbroad claim and explicit recovery limits.** [Ceph lifecycle](../../../specs/ceph.md) distinguishes erase-backed physical retirement from the unsupported paths. | B659/B662 still own partial enrollment and DB/WAL recovery; B491 owns removal while hosts survive. These are material recovery limitations, despite closure of the documentation finding. |

The five explicit safety decisions (F09-01, F13-05, R2-03-01, R2-06-02 and
V2-05) are traceable in the [owner decision](../../../specs/milestones/backlog.md#d183).
Their quality is decision clarity: whole-removal refusal, bounded retained
runtime recovery, no removal content-digest comparison and claim release on
acknowledged abandonment remain deliberate limits. They are not additional
implemented protections or proof of indefinite offline recovery.

## Architecture, adapters and verification tooling

| Findings sampled | Quality assessment and evidence | Remaining limit |
| --- | --- | --- |
| F06-03, F06-07, F18-04 | **Strong reduction of coupling.** [Retirement contracts](../../../internal/managedos/installation/contracts.go) and [composition](../../../cmd/bootwright/wiring_reconciliation.go) support multiple policies; [erase tests](../../../internal/managedos/installation/erase_test.go) require both installation and erase to wait for two injected policies. [Identity-owner checks](../../../test/architecture/implementation_identity_test.go) discover constants, reject forbidden comparisons and stale allowances; the migration allowlist is empty. | The AST check recognizes particular references and exempts legitimate owners/composition. It cannot prove all business policy resides in the right component. |
| F06-06 | **Strong shared-contract evidence.** [Capability tests](../../../cmd/bootwright/capability_contract_test.go) cover every offered binding plus a synthetic double, reject missing suite coverage, and check deterministic effect-free planning, frozen removal requests, malformed/foreign evidence, failure outcomes and replay. The known-deviation map is empty. [Second-storage tests](../../../internal/environment/second_storage_implementation_test.go) demonstrate membership-based admission with a different layout. | The second storage implementation is synthetic. Scripted-runner replay checks identical requests, not native idempotence; cancellation uses an already-cancelled context rather than every interruption timing. |
| R2-01-01 through R2-01-06 | **Strong targeted graph corrections.** [Edge-catalog tests](../../../cmd/bootwright/edge_catalog_linux_amd64_test.go) compare requirements in both directions. [Selection tests](../../../internal/environment/selection_closure_test.go) cover both replication ends, exclusion of unrelated scope, omitted roots and nested hosts. [KubeVirt nested tests](../../../internal/substrate/kubevirt/nested_test.go) check hosted prerequisites and their absence for external hosts. | The catalog test plans examples and skips individual capabilities whose plan errors. Fixtures do not enumerate every admissible graph or demonstrate a live nested deployment. |
| F08-01, F08-06, F08-07, V2-14 | **Good responsibility boundaries with explicit exceptions.** [Python-site tests](../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/test_python_sites.py) detect copied process/serialization owners, aliases and stale exceptions. [Entry-point tests](../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/test_playbook_entry_points.py) use Ansible's loader to check validation is first and reject undeclared imports. | Standalone-worker exceptions and eight later-play task allowances remain. [Language-row tests](../../../test/architecture/language_rows_test.go) classify files/extensions/shebangs; they do not determine whether business logic belongs in that language. V2-15's adapter schema debt remains partial in the original review. |
| F14-01, F14-02, F14-12, V2-11 | **Strong tests of enforcement failure behavior.** [Focus-tool tests](../../../scripts/tools/focus_test.py) preserve flaky outcomes, distinguish other exit-75 failures and exercise termination, interruption and deadlines. [Guidance tests](../../../test/architecture/docs_test.go) reject missing/orphan skill wrappers and compare effectful-command ask rules with the catalog. | Repository rules cannot guarantee agent compliance. The stop hook is intentionally advisory and runtime-specific. |
| R2-03-03 | **Useful, bounded change discipline.** [Commit checks](../../../scripts/check-commits) require a contract decision and corresponding changed goldens when a content digest moves. | The tool cannot determine whether an author's `Contract: unchanged` judgment is semantically true. Independent review remains necessary. |
| F06-04, F06-05, F06-09, F06-10, F06-11 | **Sound design guidance.** [Architecture](../../../specs/architecture.md) distinguishes typed variants, implementations and specializations and names the extension touch set. [Specialization tests](../../../test/architecture/version_specializations_test.go) discover unlisted registries and test discovery failures. | Choosing the correct mechanism still requires design judgment; a catalog guard does not establish the suitability of every specialization. |

## Documentation, ownership and delivery records

| Findings sampled | Quality assessment and evidence | Remaining limit |
| --- | --- | --- |
| F15-01, F15-07, F15-08 | **Strong enforcement of bounded navigation.** [Documentation gates](../../../test/architecture/docs_gates_test.go) exercise too-large sections, overly generous exceptions, missing pages, unreachable specs and added delivery-history references using seeded violations. [Entry budgets](../../../test/architecture/docs_test.go) bound the pages and openings read routinely. | Existing large sections and history references remain under explicit ratchets. The quality review did not reproduce the historical task-token experiment; these gates do not measure comprehension. |
| F17-03, F15-06, V2-12, F15-10 | **Sound and testable organization.** [Milestone checks](../../../test/architecture/milestones_test.go) reject inconsistent active slices, missing authorizing decisions and WIP violations. [Decision checks](../../../test/architecture/milestones_decisions_test.go) detect missing status tokens and inconsistent pending-clause lists; decision-link tests require the individual anchor. | Structural consistency cannot verify that a delivery claim reflects real acceptance. Keeping the decision log in the backlog is an explicit choice, not a failure to perform the rejected split. |
| F05-08, F15-03, F15-09, F16-03, V2-08, V2-16 | **Sound at accepted scope.** The [Ceph API](../../../specs/api/ceph.md) routes fields to their owner; the [historical review](../../../specs/storage-api-review.md) is labeled; [CLI help](../../../specs/cli/commands.md#help-content) has a normative owner; [substrate contracts](../../../specs/substrates.md) refer to code-owned version symbols instead of stale literals. | These corrections reduce drift but do not prove complete deduplication. The original remaining findings still identify stale selector wording, GitOps ordering contradictions and incomplete page conventions. |
| R2-02-05, F01-02, F01-04 | **Sound clarification of qualification and ownership.** [Substrate qualification](../../../specs/substrates.md#qualification) distinguishes accepted evidence from unrun gates. [Project scope](../../../specs/project.md) clarifies retained ownership and future lifecycle work. | A qualification row is not a new acceptance run; clear retained-ownership wording is not an implemented ownership-transfer mechanism. |

The original audit's interpretation of an earlier scope decision was also
corrected in X160. Checking such counterevidence is good review practice:
retaining an authorized active slice or recording an explicit alternative is
not itself a remediation defect. Conversely, marking all remediation lanes
merged overstates completion if their carried work is omitted from the view.
The original report's separate disposition and remaining-work tables should
remain part of any executive presentation.

## What would increase confidence

The [implementation plan](implementation-plan.md) already sequences the open
work; this assessment adds the quality criteria for judging its outcome:

1. Preserve the strongest pattern: test the prohibited effect, relevant
   interruption and recovery path, not just the successful result. For B567,
   B568 and B103, a new warning or happy-path test is insufficient.
2. Run B857's operator tasks and B867's native identity experiment, recording
   unexpected behavior as well as success. Do not substitute schema checks or
   fabricated timestamps for the missing evidence.
3. Carry the existing limits forward when evaluating acceptance: synthetic
   extension tests, scripted capability replay and injected filesystem tests
   do not qualify another backend, native idempotence or reboot durability.
4. Judge each closure against its actual deliverable. Keep documentation,
   explicit decisions, implemented behavior and live acceptance distinguishable
   in delivery reporting, and retain links to carried work.

## Verification of this enhancement

The domain assessments inspected code, tests, owning specs and decisions;
inspection alone is not recorded as a test pass. Independent API, architecture
and safety reviews corrected finding-to-evidence mappings and overbroad test
wording before integration.

| Check | Result |
| --- | --- |
| Report artifact checks | Passed: relative links/anchors, whitespace, unchanged original catalog data, 232 unique findings and 214 addressed/decision entries. |
| `make docs-check` | Passed across 12 packages. |
| `make check` | Passed: formatting, modules/tidy, Go tests, both vet platforms, completion, vulnerability check and Ansible gates. |
| Ansible qualification limits | The filesystem-dependent `test_controller_files.py:289` case was skipped: module-utils runs each reported 4,735 passed / 1 skipped under Python 3.13 and 3.9; the full collection run reported 7,617 passed / 1 skipped. These are skipped cases, not passes. |

The integration gate used short writable Go temporary/cache directories and
ran outside the restricted execution sandbox so local socket/process tests
could execute. Its success verifies the repository's automated gate at this
snapshot, not every native or operational claim discussed above.

No live machine operations, native installer run, operator authoring exercise
or fresh production acceptance was performed. A separate race run was not
performed for this documentation-only change. The delivering commit records
these results persistently alongside the assessment.
