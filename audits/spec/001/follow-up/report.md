# Specification audit 001 — follow-up report

Assessment date: 2026-10-29. Original audit: [final report](../report-final.md)
at `17acb63d9`. Dispositions reviewed at
`78d982988a3de0d78bbbab91e4edeb34a1a4adb8`; quality reviewed at `4510d73b6`.

This report combines the status and quality assessments. Its reorganization
on 2026-10-29 changes presentation and links; it does not reassess product
changes after those baselines. Dates and commits identify the evidence even
though this follow-up directory is undated.

[Executive summary](#executive-summary) ·
[Remaining work](#remaining-work) ·
[Quality assessment](#quality-assessment) ·
[Implementation plan](implementation-plan.md) ·
[Verification](#verification-and-limits) ·
[Complete findings](#complete-findings)

## Executive summary

**Not all audit work is complete.** The [remediation ledger](../../../../specs/milestones/backlog.md) marks all 31 lanes merged, but closure of a lane sometimes meant moving unfinished work to another item. All 232 catalog findings have a disposition below: no missing or duplicate IDs.

| Disposition | Findings |
| --- | ---: |
| Addressed at the audit-requested scope | 185 |
| Settled through an explicit owner decision | 29 |
| Partially addressed | 17 |
| Open | 1 |
| Total | 232 |

These are finding-level judgments, not a count of fully implemented capabilities. **Addressed** includes findings whose accepted correction was to state a limitation accurately and carry its implementation in an open item. **Explicit decision** means a different design or a deliberate retention/deferral was recorded; it is not a missed fix. **Partial** includes unfinished implementation, remaining contradictory wording and uncompleted structural recommendations; those have different operational importance. **Open** means the requested deliverable is still absent.

The most consequential remaining work is:

| Work | Current evidence and consequence |
| --- | --- |
| Shared-host teardown — B567 | [specs/substrates.md:297](../../../../specs/substrates.md) still states that removal does not enumerate host domains. [internal/substrate/libvirt/capability.go:575](../../../../internal/substrate/libvirt/capability.go) still derives quiescence from this context's Machines. Shared bridge reservations were added, but live foreign-domain checks remain unfinished. |
| Context incarnation ownership — B568 | [specs/state-reconciliation.md:2150](../../../../specs/state-reconciliation.md) retains name-derived ownership for libvirt, emulated BMCs, services and artifact paths. The interim warning is implemented; persistent incarnation identity is not. |
| Ceph recovery and online removal — B659, B662, B491 | [specs/milestones/m2.md:504](../../../../specs/milestones/m2.md) and [specs/milestones/m2.md:514](../../../../specs/milestones/m2.md) mark these Needs definition / not started. Partial enrollment and partial DB/WAL refuse; removal while hosts survive remains unsupported. Physical retirement with a covering erase policy already exists, so the original blanket claim of no backward exit was too broad. |
| Restore seal-counter safety — B103 | [specs/contexts.md:862](../../../../specs/contexts.md) explicitly says rolled-back seal counters are not detected and a fresh encryption key is not established. The restore-boundary documentation is corrected; the protection is not implemented. |
| Operator authoring-task validation — B857 | [specs/milestones/backlog.md:2934](../../../../specs/milestones/backlog.md) explicitly records that the eight section-5.7 tasks were not run before the API reshape landed. This is a roadmap exit requirement beyond the 232-ID catalog count. |
| Acceptance templates — F17-07 / B61, B755 | [specs/milestones/m3.md:589](../../../../specs/milestones/m3.md) and [specs/milestones/m4.md:837](../../../../specs/milestones/m4.md) defer the missing cluster and add-on ledger templates until their first acceptance runs. |
| Adapter schema cleanup — V2-15 / B431 | [specs/architecture.md:849](../../../../specs/architecture.md) and [specs/milestones/backlog.md:792](../../../../specs/milestones/backlog.md) retain bare nested mappings and an allowance for 29 undeclared entry-point reads. Rules and tests landed; the legacy cleanup did not. |
| Ceph recapture identity live proof — B867 | [specs/milestones/backlog.md:2951](../../../../specs/milestones/backlog.md) requires a live run to validate the chosen monitor-map creation-time invariant. This is follow-up debt discovered during remediation, not another original catalog finding. |

Two concrete spec contradictions remain: [specs/architecture.md:1159](../../../../specs/architecture.md) still names removed `CephCluster.spec.type` as a selector (F06-01); and [specs/state-reconciliation.md:2106](../../../../specs/state-reconciliation.md) promises GitOps bootstrap after every block reaching the cluster, while [specs/add-ons.md:74](../../../../specs/add-ons.md) and [internal/reconciliation/lifecycle/stage_tails.go:31](../../../../internal/reconciliation/lifecycle/stage_tails.go) limit the dependency to the same stage (F12-04).

### Quality at a glance

**Overall judgment: the sampled remediation is technically strong, but operational confidence is uneven.** The work includes substantive implementation and useful failure-path tests. The strongest evidence is in lifecycle refusals and replay, API consistency, shared capability contracts and automated documentation checks. The weakest evidence is user-task validation and live qualification. This is an assessment of the delivered scope, not a production-readiness certification.

The [quality assessment](#quality-assessment) adds evidence and qualifications for the **185 addressed findings and 29 explicit decisions**, reviewed against `4510d73b6` on 2026-10-29. All 214 entries received a catalog-level review; selected implementation and test paths received deeper inspection. It does not assign an unsubstantiated score to every finding. The original 232-finding dispositions and 18 incomplete findings remain unchanged.

| Area | Quality judgment | Main qualification |
| --- | --- | --- |
| Lifecycle and destructive-operation guards | Strong in-tree evidence: meaningful negative tests check refusal, replay and effect ordering. | Shared-host discovery, incarnation ownership, restore protection and some Ceph recovery remain deferred. |
| API and CLI changes | Strong contract consistency and regression coverage; explicit choices are traceable. | The eight operator authoring tasks remain unrun under B857, so improved usability is not demonstrated. |
| Architecture and extensibility | Stronger ownership and shared contracts, with useful synthetic second-implementation tests. | Synthetic examples and structural guards do not prove another production implementation works. |
| Verification tooling | Useful enforcement with tests of the checks themselves. | Author assertions, advisory hooks and file classification still need judgment; passing checks are not semantic proof. |
| Documentation and delivery records | Good navigation, explicit limitations and enforceable drift controls. | Existing exceptions and remaining contradictory prose show that editorial cleanup is incomplete. A merged lane is not evidence of complete runtime delivery. |
| Decisions and interim documentation fixes | Adequate where they satisfy the accepted correction and expose the tradeoff. | Recording a decision or warning earns no implementation or live-acceptance credit. |

The most important caution applies to F10-03, CX-R4 and CX-API04: their closure documents a safety or recovery limitation; it does not remove that limitation. R2-06-01 adds a real shared-claim guard but still lacks foreign-domain discovery. R2-04-08 implements and tests an identity rule whose live invariant remains unverified. The detailed assessment explains the evidence and what would increase confidence in each case.

## Remaining work

The following 18 findings are not fully closed against the original recommendation. Several are low-severity structural preferences; the table does not elevate them to runtime defects.

| Finding | Original severity | Status | What remains | Current evidence |
| --- | --- | --- | --- | --- |
| F02-01 | Medium | Partial | BMC wording sweep mostly landed, but machines.md still says an installer-provisioned or OS-ready bare-metal Machine declares no controller, meaning its BMC. This violates the reserved bare-controller rule. No dedicated BMC wording regression guard found; optional by verifier. | specs/glossary.md:12-16; specs/api/machines.md:320; specs/milestones/delivered.md:9165 |
| F06-01 | Medium | Partial | Per-object claim/gap/overlap enforcement exists and tests pass. The selector contract still names CephCluster.spec.type at architecture.md:1160, although the dedicated CephCluster schema has no type field; correct the stale selector and complete domain selection records. | internal/reconciliation/lifecycle/unclaimed_object_test.go:25; cmd/bootwright/claims_linux_amd64_test.go:34; specs/architecture.md:1157; api/v1alpha1/storage.go:91 |
| R2-04-01 | High | Partial | vCenter credentialsRef still lacks the access/effect class in specs/deferred/machine-arms.md:29; B99 explicitly retains classification and rotated-login destroy test at specs/milestones/m1.md:595-601. No current vSphere runtime exists. | specs/secrets.md:320-331; state-reconciliation.md:1838 onward; internal/reconciliation/lifecycle/rebind_test.go. KubeVirt access credential now part of B87 implementation. |
| F02-03 | Low | Partial | Undefined JSON members are now explained, but a plan node is still called a step in presentation and a block in lifecycle; the recommended single block terminology was not adopted. B824 narrowed its implementation work to the groups member. | specs/cli/output.md:241-242,281-288 |
| F02-06 | Low | Partial | Authored-add-on refusal wording changed, but add-on is still defined narrowly as ClusterAddon in one owner page and includes built-in packages in the API page. No shared Add-on/Package/Instance glossary definitions as recommended. | specs/add-ons.md:3-5; specs/api/addons.md:4-7; specs/glossary.md:7-78 |
| F03-06 | Low | Partial | Enablement/default semantics clarified, but no closed omission-class taxonomy shared by field tables or Field attribute as recommended. Q111 settles boolean form only; no explicit decision found declining the omitted class model. | specs/api.md:336-352,588-619; api/v1alpha1/schema.go:8-13 |
| F03-09 | Low | Partial | Retired-field/value/kind registry and tailored remedies landed. Recommended authoring-schema fingerprint in version/frozen context/refusal did not; only unrelated internal contract-test schemaFingerprint exists. | specs/api.md:73-79,373-470; api/v1alpha1/retired.go; internal/cli/version.go:8-16,37-44 |
| F04-05 | Low | Partial | rootDevice overreach fixed by hardware.rootDevice, but OS lifecycle still changes from Bootwright-installed to downstream-installer when installProfileRef is omitted. Explicit lifecycle union recommendation remains unimplemented/untracked separately. | specs/api/machines.md:217-220,227-242; specs/milestones/delivered.md:8750-8756 |
| R2-05-08 | Low | Partial | Stale prose corrected and README qualification limits/ledger links added. Recommended per-row admission/plan/accepted Status column and guard are absent; current index deliberately states outcomes only. No explicit decline of status-column recommendation found. | examples/README.md:11-52; specs/milestones/delivered.md:8878-8882 |
| F06-12 | Low | Partial | The Selection/Implementations/Refusal table template is required on a page's next edit, but these pages and ceph.md were edited again Oct 28-29 and still omit the Implementations heading/record. The instruction was added before those edits (e2fa5bb3c). | .agents/skills/architecture-best-practices/SKILL.md:32; specs/substrates.md:20; specs/openshift-clusters.md:25; specs/infrastructure-services.md:13 |
| F07-07 | Low | Partial | Allocation projection moved to the libvirt owner, but both composition bindings are still libvirt-only and container-cluster preflight still imports Storage's Report, Graph, Ownership and Projector. The neutral/shared preflight boundary remains unfinished. | internal/substrate/libvirt/capacity.go:15; cmd/bootwright/wiring_preflight.go:23; internal/containercluster/preflight/contracts.go:7; internal/containercluster/preflight/service.go:15 |
| V2-15 | Low | Partial | Argument depth and undeclared-variable rules/tests exist, but B431 still retains bare nested mappings, raw nativeStaging, three cited incomplete schemas, and 29 existing undeclared reads. This is a completed spec correction with unfinished adapter cleanup. | specs/architecture.md:836; specs/architecture.md:849; specs/milestones/backlog.md:791; ansible/collections/ansible_collections/bootwright/core/tests/unit/test_entry_point_reads.py:129 |
| F12-04 | Low | Partial | Scope contradiction survives. Code StageTail uses same-stage dependencies. B884 tracks adjacent stale bootstrap Not yet met line, but does not close the ordering mismatch. | specs/add-ons.md:74-80 explicitly limits last package to add-ons-stage blocks and excludes other-stage API users; state-reconciliation.md:2106-2108 still promises every other block that reaches the cluster; m4.md:1491 repeats broader claim. Code: internal/reconciliation/lifecycle/stage_tails.go:31; TestLastInstanceWaitsForEverySameClusterStageConsumer passed and deliberately omits the other-stage block. |
| F13-09 | Low | Partial | The cut-over and publication duplication is reduced, but the requested single home for setup-subtree refusal/retention rules is not complete. This is documentation duplication, not a proved runtime defect. | specs/controller.md:822-826 links layout ownership; :928-932 delegates cut-over refusal to contexts.md:237. But controller.md:857-893 and contexts/controller-record.md:246-261 both describe pending/format refusal and healing. |
| F15-04 | Low | Partial | Remove acceptance-only status from Not yet met paragraphs and leave it in milestone/ledger owners. Docs-check verifies the linked item is open, not whether code is actually missing. | specs/index.md:49-54 reserves Not yet met for code-unmet rules, but specs/openshift-coverage.md:141-143 still marks every row whose item is open, including B61/B66 whose in-tree code is delivered; specs/ceph-coverage.md:11 also includes tested-journey records. |
| F17-05 | Low | Partial | Adoption duplication was removed. The proposed relocation of host-preparation safety mechanics out of the coverage page remains incomplete; this is a structural recommendation. | specs/ceph-coverage.md:130-133 now delegates adoption rules to ceph.md, but :75-128 still owns detailed host-preparation/probe safety mechanics, and ceph.md:642 explicitly delegates them back to coverage. |
| F17-07 | Low | Open | Add the two docs/acceptance.md templates and coverage links under B61/B755. Moving their ownership did not deliver them. | specs/milestones/m3.md:589-593 and m4.md:837-841 explicitly defer the missing Container cluster row and Add-on row templates until first acceptance runs; delivered.md:9177 records their transfer from B820. |
| F17-08 | Low | Partial | Complete the proposed migration of observed release facts to indexed knowledge; the governing release-selection rules are already corrected. | The release-evidence openings now link knowledge (openshift-coverage.md:14-25; add-on-coverage.md:15-27). However native release-specific statements remain, e.g. openshift-coverage.md:126 (4.22 Localnet behavior) and ceph-coverage.md:464-468 (v20.2.1 firewall behavior). |

The [implementation plan](implementation-plan.md) maps these findings and
the carried safety/validation work to dependencies and exit evidence.

## Quality assessment

### Method and confidence

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

### API, CLI and design decisions

| Findings sampled | Quality assessment and evidence | What the evidence does not establish |
| --- | --- | --- |
| F02-04, F03-01, F02-09, F03-07 | **Strong in-tree evidence.** [Catalog tests](../../../../api/v1alpha1/catalog_docs_test.go) compare registered kinds with documentation and output order. `TestEveryUnionFollowsTheOneRule` in [union tests](../../../../test/architecture/unions_test.go) rejects synthetic rule violations. [Enum](../../../../test/architecture/enum_casing_test.go), [reference](../../../../test/architecture/reference_suffix_test.go) and [scope](../../../../api/v1alpha1/scope_test.go) guards reduce independent inventories and detect stale exceptions. | Structural consistency does not establish that the chosen model is easy to author. |
| R2-05-01, F05-04, F05-09, R2-02-06, F16-01 | **Strong implementation of an honest availability boundary.** [Unrealized declaration tests](../../../../cmd/bootwright/unrealized_linux_amd64_test.go) compare the table, warning identity/severity and actual plan refusal, with coverage of every listed declaration. [Unavailable-command tests](../../../../cmd/bootwright/unavailable_commands_test.go) check stub wiring, failure status and diagnostics. | Clear warnings and refusals do not implement vSphere, reserved add-ons or custom playbooks. |
| F03-02, F03-03, F04-01, R2-05-05, V2-02 | **Strong in-tree evidence.** [Compiler tests](../../../../internal/desiredstate/compilation/compiler_test.go) check skipped-secret warnings and remedies. [Machine admission tests](../../../../cmd/bootwright/machine_admission_test.go) check arm conflicts, derived defaults and authored overrides. [Entitlement-default tests](../../../../internal/desiredstate/compilation/entitlement_defaults_test.go) verify that defaults do not introduce arms forbidden by the selected type. [Ceph normalization](../../../../internal/storage/cephcluster/normalization.go) and [topology admission](../../../../internal/storage/cephcluster/topology.go) implement the safe access default and native-mode refusal. | Synthetic declarations and catalogs do not qualify native providers. |
| V2-19, F04-11 | **Strong in-tree evidence.** [Block identity tests](../../../../internal/reconciliation/block_identity_test.go) exercise deterministic bounded identities and collision refusal naming both objects. One shared rule is more maintainable than service-specific name limits. | These tests do not prove every external service accepts every otherwise valid authored name. |
| F05-06 | **Strong behavioral correction.** [Placement tests](../../../../internal/containercluster/agentinstall/placement_test.go) freeze labels/taints, reject missing or changed completion evidence, preserve unrelated manager taints and ensure read-only diff reports a missing label without restoring it. [Evidence validation](../../../../internal/containercluster/agentinstall/evidence.go) enforces the postcondition. | Synthetic evidence/runners do not establish native installation or live cluster acceptance. |
| F03-05 | **Sound alternative, with qualified usability evidence.** [Origin tests](../../../../cmd/bootwright/render_origins_linux_amd64_test.go) check text and JSON provenance; [schema-export tests](../../../../cmd/bootwright/json_schema_linux_amd64_test.go) check examples, malformed input classes, defaults and bounded export size/depth. | The schema is deliberately advisory; it omits semantic constraints and uses an in-repository subset checker in its tests. General editor interoperability and operator task success are not demonstrated. |
| F01-01, F03-01, F04-01, F05-03, F18-02 and the other explicit decisions | **Sound at accepted scope.** The [decision record](../../../../specs/milestones/backlog.md#d183) makes retained service implementation fields, narrow substrate arms, family-specific kinds and other choices traceable. Existing guards enforce several of those choices. | Choosing a defensible alternative does not prove it is easier to use. B857 remains the relevant empirical exit, and retained/deferred functionality gains no implementation credit. |

The main weakness is evidence of the user benefit: [B857](../../../../specs/milestones/backlog.md#b857)
explicitly records that the eight human/agent authoring tasks were not run
before the API reshape landed. Contract consistency is demonstrated more
strongly than usability. A quality-focused follow-up should execute those
tasks and record completion, corrections, time and wrong assumptions before
claiming the API is simpler in practice.

### Lifecycle, safety and recovery

| Findings sampled | Quality assessment and evidence | Remaining limit |
| --- | --- | --- |
| F10-01, F10-02, F10-05 | **Strong in-tree evidence.** [Installer target-proof tests](../../../../internal/managedos/installation/target_proof_test.go) execute the rendered Bash against synthetic sysfs and mocked device reads: wrong/missing disks, partitions, MAC mismatches and unreadable identity must stop before storage changes. [Power tests](../../../../internal/machine/power/service_test.go) check refusal before runtime entry for unreadable proof, missing identity and the declared controller Machine, plus propagation of the adapter's controller-host refusal. | Executing the rendered proof is stronger than a text snapshot, but still uses synthetic hardware. The Anaconda proof does not establish the same inside-installer protection for OpenShift Agent; its [disk-safety limitation](../../../../.agents/knowledge/openshift-agent-disk-safety.md) remains relevant. |
| CX-R5, CX-X1, F09-02 | **Strong failure-oriented corrections.** [Inconclusive-removal tests](../../../../internal/reconciliation/lifecycle/inconclusive_removal_test.go) retain unknown observations, prevent replacement registration and constrain reread/replay. [Disk-boot intent tests](../../../../internal/managedos/installation/disk_boot_intent_test.go) model recovery before and after installation proof, material withdrawal, power-off and disk boot. [Registration tests](../../../../internal/reconciliation/lifecycle/registration_test.go) exhaust apply budgets while preserving a working removal reserve. | [Adapter ordering tests](../../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/test_managedos_disk_boot_intent.py) inspect task structure; they do not execute an installer. In-tree recovery evidence is not a live failure drill. |
| R2-02-01, F10-04, R2-06-01, R2-06-04, R2-06-06 | **Strong guards within their coordination boundary.** [Consumer-survival tests](../../../../internal/reconciliation/lifecycle/consumer_survival_test.go) refuse before registration, probing or destruction. [Shared-claim tests](../../../../internal/substrate/libvirt/shared_claims_test.go) and [system-claim tests](../../../../internal/workspace/contextfs/system_claim_linux_amd64_test.go) cover bridge/BMC claims, interrupted removal and host aliases sharing a pinned key. | Claims coordinate the same store. [Libvirt host quiescence](../../../../internal/substrate/libvirt/capability.go) still relies on this context's Machines; B567's foreign-domain discovery remains absent. This is a real guard, not complete shared-host safety. |
| F13-07, F13-02, F13-06 | **Strong observable-state tests.** [Root filesystem tests](../../../../internal/workspace/contextfs/root_filesystem_linux_amd64_test.go) refuse volatile filesystems before publication. [Context service tests](../../../../internal/workspace/contexts/service_test.go) change evidence during confirmation and assert no write. [Controller-rebind tests](../../../../internal/workspace/contextfs/controller_rebind_linux_amd64_test.go) check preserved bindings, copied-directory refusal and interruptions around durable rename. | Filesystem types are injected; reboot durability is not tested. Supported rebind is not general restore or arbitrary filesystem-identity recovery. |
| F11-03, F11-08, F10-06 | **Precise trust and confidential-input boundaries.** [Enrollment tests](../../../../internal/trust/enrollment/service_test.go) ensure missing unattended fingerprints and mismatched supplied fingerprints cannot be recorded. [Private URL tests](../../../../internal/managedos/medialocal/url_file_test.go) bound input and prevent disclosure in errors. [Elevation tests](../../../../internal/controller/privilege/elevation_test.go) refuse stdin-logging policy. [Authorization tests](../../../../internal/reconciliation/lifecycle/authorization_test.go) prevent prompts, mutations and effects for stale/unbound data-loss tokens. | Deployed sudo/PAM policies are not exhaustively qualified. The documented distinction between keyring custody and material retained on a native host does not erase that material on abandonment. |
| R2-04-08 | **Good implementation safeguards; native identity assumption not yet demonstrated.** [Produced-identity tests](../../../../internal/secrets/localkeyring/produced_identity_linux_amd64_test.go) check retention, invalid/unproved identity refusal and unchanged storage. [Ceph module tests](../../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_ceph_core.py) validate timestamps, FSID matching and evidence shape. | The tests fabricate monitor-map timestamps. They do not prove stability through membership changes/upgrades or a change on same-FSID rebootstrap. B867 explicitly owns that live experiment. |

Three addressed findings especially need a different quality judgment from
the implemented guards above:

| Finding | Quality of the delivered work | Protection still absent and consequence |
| --- | --- | --- |
| F10-03 | **Sound interim documentation.** [Ownership wording](../../../../specs/state-reconciliation.md) states the name-derived identity boundary and carries B568. | Persistent incarnation identity is absent; recreating a context with the same name does not gain the stronger ownership separation the original safety concern sought. |
| CX-R4 | **Sound restore-boundary clarification.** [Context restore documentation](../../../../specs/contexts.md) identifies the matching-whole-store boundary, no supported restore command, and B103. | Rolled-back seal counters are not detected and a fresh encryption key is not established. Honest disclosure does not supply rollback protection. |
| CX-API04 | **Sound correction of an overbroad claim and explicit recovery limits.** [Ceph lifecycle](../../../../specs/ceph.md) distinguishes erase-backed physical retirement from the unsupported paths. | B659/B662 still own partial enrollment and DB/WAL recovery; B491 owns removal while hosts survive. These are material recovery limitations, despite closure of the documentation finding. |

The five explicit safety decisions (F09-01, F13-05, R2-03-01, R2-06-02 and
V2-05) are traceable in the [owner decision](../../../../specs/milestones/backlog.md#d183).
Their quality is decision clarity: whole-removal refusal, bounded retained
runtime recovery, no removal content-digest comparison and claim release on
acknowledged abandonment remain deliberate limits. They are not additional
implemented protections or proof of indefinite offline recovery.

### Architecture, adapters and verification tooling

| Findings sampled | Quality assessment and evidence | Remaining limit |
| --- | --- | --- |
| F06-03, F06-07, F18-04 | **Strong reduction of coupling.** [Retirement contracts](../../../../internal/managedos/installation/contracts.go) and [composition](../../../../cmd/bootwright/wiring_reconciliation.go) support multiple policies; [erase tests](../../../../internal/managedos/installation/erase_test.go) require both installation and erase to wait for two injected policies. [Identity-owner checks](../../../../test/architecture/implementation_identity_test.go) discover constants, reject forbidden comparisons and stale allowances; the migration allowlist is empty. | The AST check recognizes particular references and exempts legitimate owners/composition. It cannot prove all business policy resides in the right component. |
| F06-06 | **Strong shared-contract evidence.** [Capability tests](../../../../cmd/bootwright/capability_contract_test.go) cover every offered binding plus a synthetic double, reject missing suite coverage, and check deterministic effect-free planning, frozen removal requests, malformed/foreign evidence, failure outcomes and replay. The known-deviation map is empty. [Second-storage tests](../../../../internal/environment/second_storage_implementation_test.go) demonstrate membership-based admission with a different layout. | The second storage implementation is synthetic. Scripted-runner replay checks identical requests, not native idempotence; cancellation uses an already-cancelled context rather than every interruption timing. |
| R2-01-01 through R2-01-06 | **Strong targeted graph corrections.** [Edge-catalog tests](../../../../cmd/bootwright/edge_catalog_linux_amd64_test.go) compare requirements in both directions. [Selection tests](../../../../internal/environment/selection_closure_test.go) cover both replication ends, exclusion of unrelated scope, omitted roots and nested hosts. [KubeVirt nested tests](../../../../internal/substrate/kubevirt/nested_test.go) check hosted prerequisites and their absence for external hosts. | The catalog test plans examples and skips individual capabilities whose plan errors. Fixtures do not enumerate every admissible graph or demonstrate a live nested deployment. |
| F08-01, F08-06, F08-07, V2-14 | **Good responsibility boundaries with explicit exceptions.** [Python-site tests](../../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/test_python_sites.py) detect copied process/serialization owners, aliases and stale exceptions. [Entry-point tests](../../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/test_playbook_entry_points.py) use Ansible's loader to check validation is first and reject undeclared imports. | Standalone-worker exceptions and eight later-play task allowances remain. [Language-row tests](../../../../test/architecture/language_rows_test.go) classify files/extensions/shebangs; they do not determine whether business logic belongs in that language. V2-15's adapter schema debt remains partial in the original review. |
| F14-01, F14-02, F14-12, V2-11 | **Strong tests of enforcement failure behavior.** [Focus-tool tests](../../../../scripts/tools/focus_test.py) preserve flaky outcomes, distinguish other exit-75 failures and exercise termination, interruption and deadlines. [Guidance tests](../../../../test/architecture/docs_test.go) reject missing/orphan skill wrappers and compare effectful-command ask rules with the catalog. | Repository rules cannot guarantee agent compliance. The stop hook is intentionally advisory and runtime-specific. |
| R2-03-03 | **Useful, bounded change discipline.** [Commit checks](../../../../scripts/check-commits) require a contract decision and corresponding changed goldens when a content digest moves. | The tool cannot determine whether an author's `Contract: unchanged` judgment is semantically true. Independent review remains necessary. |
| F06-04, F06-05, F06-09, F06-10, F06-11 | **Sound design guidance.** [Architecture](../../../../specs/architecture.md) distinguishes typed variants, implementations and specializations and names the extension touch set. [Specialization tests](../../../../test/architecture/version_specializations_test.go) discover unlisted registries and test discovery failures. | Choosing the correct mechanism still requires design judgment; a catalog guard does not establish the suitability of every specialization. |

### Documentation, ownership and delivery records

| Findings sampled | Quality assessment and evidence | Remaining limit |
| --- | --- | --- |
| F15-01, F15-07, F15-08 | **Strong enforcement of bounded navigation.** [Documentation gates](../../../../test/architecture/docs_gates_test.go) exercise too-large sections, overly generous exceptions, missing pages, unreachable specs and added delivery-history references using seeded violations. [Entry budgets](../../../../test/architecture/docs_test.go) bound the pages and openings read routinely. | Existing large sections and history references remain under explicit ratchets. The quality review did not reproduce the historical task-token experiment; these gates do not measure comprehension. |
| F17-03, F15-06, V2-12, F15-10 | **Sound and testable organization.** [Milestone checks](../../../../test/architecture/milestones_test.go) reject inconsistent active slices, missing authorizing decisions and WIP violations. [Decision checks](../../../../test/architecture/milestones_decisions_test.go) detect missing status tokens and inconsistent pending-clause lists; decision-link tests require the individual anchor. | Structural consistency cannot verify that a delivery claim reflects real acceptance. Keeping the decision log in the backlog is an explicit choice, not a failure to perform the rejected split. |
| F05-08, F15-03, F15-09, F16-03, V2-08, V2-16 | **Sound at accepted scope.** The [Ceph API](../../../../specs/api/ceph.md) routes fields to their owner; the [historical review](../../../../specs/storage-api-review.md) is labeled; [CLI help](../../../../specs/cli/commands.md#help-content) has a normative owner; [substrate contracts](../../../../specs/substrates.md) refer to code-owned version symbols instead of stale literals. | These corrections reduce drift but do not prove complete deduplication. The original remaining findings still identify stale selector wording, GitOps ordering contradictions and incomplete page conventions. |
| R2-02-05, F01-02, F01-04 | **Sound clarification of qualification and ownership.** [Substrate qualification](../../../../specs/substrates.md#qualification) distinguishes accepted evidence from unrun gates. [Project scope](../../../../specs/project.md) clarifies retained ownership and future lifecycle work. | A qualification row is not a new acceptance run; clear retained-ownership wording is not an implemented ownership-transfer mechanism. |

The original audit's interpretation of an earlier scope decision was also
corrected in X160. Checking such counterevidence is good review practice:
retaining an authorized active slice or recording an explicit alternative is
not itself a remediation defect. Conversely, marking all remediation lanes
merged overstates completion if their carried work is omitted from the view.
The original report's separate disposition and remaining-work tables should
remain part of any executive presentation.

### What would increase confidence

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

## Verification and limits

### Original disposition review

The review compared the original catalog with current owning specs, decision and delivery records, relevant code and test guards. It checked counterevidence, including explicit decisions that rejected or narrowed recommendations, and kept those separate from unfinished work. The same IDs are counted only once even when a remaining concern overlaps another finding.

`make docs-check` passed across its 12 selected packages. The successful run used writable Go temporary/cache directories; an initial environment-only failure is not counted as a test failure or pass. The successful output is in [original docs-check output](evidence/docs-check.txt).

Focused host-independent tests also passed for API/schema/default/reference guards; JSON-schema, origin, unrealized-field and example-name checks; implementation boundaries, claims and reference edges; shared capability contracts; inconclusive removal, log-fault restoration, consumer retirement, lost-record destroy and removal reserves; physical identity/MAC proofs; shared/releasing claims and controller rebind; and same-stage GitOps ordering. Existing Python/Ansible structural tests were inspected rather than rerun. No live machine operations, native installer runs, full `make check`, or fresh production qualification were performed. Resolved status does not imply live acceptance.

The initial read-only review changed no tracked files or Git metadata. Its dated assessment, its machine-readable [findings.json](findings.json), and the subsequent [implementation plan](implementation-plan.md) are preserved together. Evidence citations refer to the reviewed commit, not to later edits.

### Quality enhancement

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

### Format migration

The 2026-10-29 format migration preserves the original 232 dispositions, all
18 proposed work packages and the retained check output. It consolidates the
human assessment and repairs navigation; it supplies no new product-quality
judgment. Its delivering commit records migration checks and the integration
gate separately from the earlier evidence above.

## Complete findings

### S3 — 8 findings

| Finding | Original severity | Status | Assessment and qualification | Evidence |
| --- | --- | --- | --- | --- |
| F01-01 | Medium | Explicit decision | Two scope lists now exist. Owner chose to keep milestone pages/completion unchanged, including vSphere in the committed portfolio. | specs/project.md:18-45; specs/milestones/backlog.md:3184 (Q89), D183 |
| F01-02 | Medium | Addressed | Downstream resources, frozen ownership and destroy/fresh-apply path are explicit. The broader day-2/relinquish feature remains B106, as the audit acceptance allowed. | specs/project.md:74-91; specs/state-reconciliation.md:27-46 |
| F17-02 | Medium | Explicit decision | Submilestone Requires cells and cross-milestone prerequisites added. Owner retained all-items completion; broad completion restructuring was not selected. | specs/milestones/m1.md:37; specs/milestones/m2.md:106; specs/milestones/m4.md:73; specs/milestones/backlog.md:3185 (Q90) |
| F01-03 | Low | Addressed | Explicit admitted-utility exception distinguishes bootstrap scope from sessions, power, trust and inspection. | specs/project.md:136-152 |
| F01-04 | Low | Addressed | Early per-cluster handoff explicitly remains pending Q60; no longer presented as unconditionally decided. | specs/project.md:84-91 |
| F01-05 | Low | Addressed | Managed OS installation and entitlement use now appear in committed scope. | specs/project.md:28-30 |
| F01-06 | Low | Addressed | Owned network configuration and provided physical fabric/external services are named. | specs/project.md:50-60 |
| F01-11 | Low | Addressed | Purpose routes to the owning project scope; milestone and implementation links updated. | README.md:3; specs/project.md:18; specs/milestones/m7.md:5 |

### S4 — 14 findings

| Finding | Original severity | Status | Assessment and qualification | Evidence |
| --- | --- | --- | --- | --- |
| F02-01 | Medium | Partial | BMC wording sweep mostly landed, but machines.md still says an installer-provisioned or OS-ready bare-metal Machine declares no controller, meaning its BMC. This violates the reserved bare-controller rule. No dedicated BMC wording regression guard found; optional by verifier. | specs/glossary.md:12-16; specs/api/machines.md:320; specs/milestones/delivered.md:9165 |
| F02-04 | Medium | Addressed | Literal catalog counts removed; registered kinds, canonical order and owner-page rows are checked. Both selected tests passed. | specs/api.md:474-491; api/v1alpha1/catalog_docs_test.go:69; api/v1alpha1/children_test.go:29 |
| F01-10 | Low | Explicit decision | D183 Q104(b) explicitly keeps State reconciliation; its row includes diff and its contract excludes changed-state reconciliation. | specs/milestones/delivered.md:8853-8858; specs/architecture.md:63 |
| F02-02 | Low | Explicit decision | Arm/variant/implementation distinguished. Owner explicitly retained managed-service implementation enum. | specs/glossary.md:58-69; specs/api.md:303-312; specs/milestones/backlog.md:3176 (Q81) |
| F02-03 | Low | Partial | Undefined JSON members are now explained, but a plan node is still called a step in presentation and a block in lifecycle; the recommended single block terminology was not adopted. B824 narrowed its implementation work to the groups member. | specs/cli/output.md:241-242,281-288 |
| F02-05 | Low | Addressed | Infrastructure service and substrate meanings are explicit; stage is infrastructure-services, with retired-token refusal. | specs/glossary.md:20-26; internal/cli/catalog_test.go:21; specs/milestones/delivered.md:8785 |
| F02-06 | Low | Partial | Authored-add-on refusal wording changed, but add-on is still defined narrowly as ClusterAddon in one owner page and includes built-in packages in the API page. No shared Add-on/Package/Instance glossary definitions as recommended. | specs/add-ons.md:3-5; specs/api/addons.md:4-7; specs/glossary.md:7-78 |
| F02-07 | Low | Addressed | Overloaded qualified forms now have owner links in a standalone glossary. Audit narrative permits keeping the full ~40-term table in the audit; not all forty terms need copying. | specs/glossary.md:35-78; audits/spec/001/report-final.md:409 |
| F02-09 | Low | Explicit decision | Naming/Ref rules exist and tests passed; D183 Q105 chose lowerCamel instead of recommended kebab-case. | specs/api.md:354-371; test/architecture/enum_casing_test.go:71; test/architecture/reference_suffix_test.go:66 |
| F02-12 | Low | Explicit decision | Roles removed; lifecycle and add-on capability senses qualified. D183 Q105 chose lowerCamel tokens. | specs/glossary.md:51-57; specs/api/machines.md:326-332; specs/api.md:360-363 |
| F05-05 | Low | Explicit decision | D183 Q107 renamed export to CephConsumer, retaining storage principal / package consumer decomposition. | specs/api/ceph.md:82-83; specs/milestones/delivered.md:8730-8734 |
| F12-05 | Low | Addressed | Unchanged/settled/compared/drift semantics are centralized; settle message points to diff --observe. | specs/state-reconciliation.md:41-46,1940-1955; specs/cli.md:820-850 |
| F16-07 | Low | Addressed | Selectors use --name, related nouns plural, names-only --short, with a catalog guard. | specs/cli/commands.md:76-82; specs/milestones/delivered.md:8868-8875 |
| F16-08 | Low | Addressed | Receipt token-to-real-command mapping is explicit and status repeats the operation verb. | specs/cli.md:659-671,694-713 |

### S5 — 43 findings

| Finding | Original severity | Status | Assessment and qualification | Evidence |
| --- | --- | --- | --- | --- |
| F03-01 | Medium | Explicit decision | One testable union rule, flattened CephCluster and presence arms landed. Owner retained service implementation enum. Selected union test passed. | specs/api.md:298-334; test/architecture/unions_test.go:66 |
| F03-02 | Medium | Addressed | Skipped secrets YAML warns and unresolved-reference remedy names directory; selected regression passed. | specs/api/input.md:76-80; internal/desiredstate/compilation/compiler_test.go:151 |
| F03-03 | Medium | Explicit decision | Single derivations normalize; node FQDN override retained by explicit owner choice. Derivation test checked in this review. | specs/api.md:641-649; cmd/bootwright/machine_admission_test.go:132; specs/milestones/backlog.md:3177 (Q82) |
| F04-01 | Medium | Explicit decision | Narrow per-substrate arms landed, preserving neutral NIC names/root hints outside arms per owner decision; mismatched arm refuses. | specs/api/machines.md:292-324; specs/milestones/backlog.md:3174 (Q79) |
| F04-02 | Medium | Addressed | Role tokens removed and prerequisites derived from cluster membership/placements; spec.capabilities retired. | specs/api/machines.md:326-332; specs/controller.md:220 |
| F05-02 | Medium | Addressed | CephReplication, native names/reference classes and conditional mirroring documented; registered Ceph kind table guard passed. | specs/api/ceph.md:31-70,111-128; api/v1alpha1/children_test.go:29 |
| F05-03 | Medium | Explicit decision | Owner chose CephCluster and all Ceph child kind names. Neutral extension is a new family registry entry. | specs/api/ceph.md:3-12; specs/api.md:489; specs/milestones/backlog.md:3173 (Q78) |
| F05-06 | Medium | Addressed | Endpoint mismatches refuse; informational channel distinguished and inert dnsName warns api.deferred. Labels/taints now applied after installation. | specs/openshift-clusters.md:51-73; internal/containercluster/agentinstall/refusal_table_test.go:128 |
| F05-07 | Medium | Explicit decision | mdsServiceRef, SMB share cluster_id and resourceRefs derive as owner chose; native refs remain authored home. | specs/api.md:645-649; specs/milestones/backlog.md:3178 (Q83); examples/ceph-clusters/libvirt-6-nodes-ceph-services/storage |
| F18-02 | Medium | Explicit decision | Explicit one-kind-per-family choice, OpenShiftCluster name, OKD variant and registry-derived node membership satisfy alternative acceptance. | specs/api/openshift-clusters.md:3-12; specs/api.md:493-530; specs/milestones/backlog.md:3172 (Q77) |
| R2-05-01 | Medium | Addressed | Default refusal point/exception criterion and checked unsupported table exist; api.unrealized warns. Selected table test checked in review. | specs/api.md:21-61; cmd/bootwright/unrealized_linux_amd64_test.go:70 |
| R2-05-04 | Medium | Addressed | External profile now has exact directory/coverage identity; additional realized baremetal-VIP and nested examples are separate profiles. | specs/openshift-coverage.md:161-163; examples/openshift-clusters/lab-kubevirt-external/README.md:1; examples/README.md:38-40 |
| V2-01 | Medium | Addressed | NVMe-oF realization is documented. CephConsumer scope still uses its reference-only declaration, now with that rationale rather than falsely claiming no realizing capability; allowed by the audit alternative. | specs/api/ceph.md:97-101; specs/api.md:489; specs/cli/output.md:640 |
| V2-02 | Medium | Addressed | Omission selects verifiedTunnel; native refuses at validate; refusal table agrees. | specs/api/ceph.md:260-265; specs/ceph.md:404 |
| V2-19 | Medium | Addressed | One public 63-byte object-name rule and hashing of long block identities replaces service-dependent caps; subsequent Q2 closes long-name followups. | specs/api.md:238-246; specs/milestones/delivered.md:8773-8780 |
| F03-04 | Low | Explicit decision | Redundant libvirt/boot/install method fields removed; libvirt URI defaults. Owner explicitly kept managed-service implementation required. | specs/api.md:370-371,373-415; specs/api/machines.md:91; specs/milestones/backlog.md:3207 (Q112) |
| F03-05 | Low | Explicit decision | D183 Q110 chose origins and advisory JSON Schema, not explain/Go field documentation. Selected origins/schema tests checked in review. | specs/cli.md:524-550; internal/cli/catalog_test.go:21,24; cmd/bootwright/render_origins_linux_amd64_test.go:15; cmd/bootwright/json_schema_linux_amd64_test.go:198 |
| F03-06 | Low | Partial | Enablement/default semantics clarified, but no closed omission-class taxonomy shared by field tables or Field attribute as recommended. Q111 settles boolean form only; no explicit decision found declining the omitted class model. | specs/api.md:336-352,588-619; api/v1alpha1/schema.go:8-13 |
| F03-07 | Low | Addressed | Reference classes typed, resolution scopes declared, shared diagnostic implemented in X179; reference/scope guards passed. | specs/api.md:250-296; api/v1alpha1/schema.go:30-34; api/v1alpha1/scope_test.go:75 |
| F03-08 | Low | Explicit decision | Owner removed resources; directory scope plus cluster lists is now explicit and shown with an example. | specs/api/environment.md:147-178; specs/milestones/backlog.md:3167 (Q72) |
| F03-09 | Low | Partial | Retired-field/value/kind registry and tailored remedies landed. Recommended authoring-schema fingerprint in version/frozen context/refusal did not; only unrelated internal contract-test schemaFingerprint exists. | specs/api.md:73-79,373-470; api/v1alpha1/retired.go; internal/cli/version.go:8-16,37-44 |
| F04-04 | Low | Explicit decision | Libvirt/KubeVirt MACs derive and interfaceBinding refuses. vSphere remains authored-or-derived in its own arm; owner narrow-arm decision retained vSphere MAC facts, rather than universal derivation. | specs/api/machines.md:279-290,309; specs/deferred/machine-arms.md:91-104; specs/milestones/backlog.md:3174 (Q79) |
| F04-05 | Low | Partial | rootDevice overreach fixed by hardware.rootDevice, but OS lifecycle still changes from Bootwright-installed to downstream-installer when installProfileRef is omitted. Explicit lifecycle union recommendation remains unimplemented/untracked separately. | specs/api/machines.md:217-220,227-242; specs/milestones/delivered.md:8750-8756 |
| F04-06 | Low | Explicit decision | bmcEmulation rename, removed one-value mechanics and URI default landed; explicit required service implementation retained by owner. | specs/api/machines.md:87-107; specs/milestones/backlog.md:3207 (Q112) |
| F04-07 | Low | Addressed | External ArtifactServer refuses. LoadBalancer bind addresses now have declared consumers; managed unsupported modes warn/refuse. | specs/api/infrastructure-services.md:168-179,184-203 |
| F04-08 | Low | Addressed | Narrowed documentation remedy explicitly distinguishes authored Machine bypass from OpenShift-derived bypass, removing ambiguity. Common derived bypass recommendation was not adopted; this is a retained design choice, not implemented common derivation. | specs/api/machines.md:481-486; specs/openshift-clusters.md:363-368 |
| F04-09 | Low | Addressed | Bare-metal attachment explicitly selects/configures nothing and examples omit redundant selections; stronger removal was optional. | specs/api/machines.md:342,360-365; specs/milestones/delivered.md:8788-8790 |
| F04-10 | Low | Explicit decision | Owner kept both image references; precedence local, else public, else compiled default is now stated. | specs/api/infrastructure-services.md:37-49; specs/milestones/backlog.md:3208 (Q113) |
| F04-11 | Low | Addressed | All object names use one 63-byte rule; overflow block identity hashes. V2-19 is jointly fixed. | specs/api.md:238-246; specs/milestones/delivered.md:8773-8780 |
| F05-04 | Low | Explicit decision | Reserved authored add-ons clearly cannot run. Owner kept validating shapes; B90 owns future Subscription alignment. | specs/api/addons.md:34-36,130-139; specs/api.md:59-60; specs/milestones/backlog.md:3210 (Q115) |
| F05-09 | Low | Explicit decision | Owner expressly declined admission-only refusal and kept validating CustomPlaybook; unrealized warning/plan refusal are explicit. | specs/api/custom-playbooks.md:1-41; specs/api.md:61; specs/milestones/backlog.md:3210 (Q115) |
| F05-10 | Low | Addressed | Default pull-secret/nodeSSH values omitted; explicit topology FQDN overrides remain documented choices, placement-only CRUSH rule notes added. | examples/openshift-clusters/lab-compact/clusters/compact/cluster.yaml:35; specs/milestones/delivered.md:8878-8882 |
| F16-01 | Low | Addressed | Checked unavailable-command list and help labels replace implicit availability. Some commands remain stubs, truthfully identified. | specs/cli/commands.md:136-140,204-215 |
| F18-06 | Low | Addressed | Provider nodeNetworking removed/retired and cluster owns it; vSphere realization itself remains B99 as intended. | specs/api.md:394; specs/deferred/machine-arms.md:17-41 |
| R2-02-06 | Low | Addressed | Both managed unsupported services warn and refuse with external remedy and owning B68/B100 tracking. | specs/api/infrastructure-services.md:199-204; specs/api.md:54-55 |
| R2-02-07 | Low | Addressed | Local-stratum host-clock fallback and readiness-vs-sync distinction stated. | specs/infrastructure-services.md:630-634,664; specs/api/infrastructure-services.md:118-121 |
| R2-05-03 | Low | Addressed | Runnable authored scenario demonstrates external proxy/DNS/NTP and kind defaults; example schema equivalence test checked in review. | examples/README.md:14; examples/machines/lab-rhel-external-services/environment.yaml; specs/milestones/delivered.md:8878-8882 |
| R2-05-05 | Low | Addressed | Closed entitlement arm sets enforced and forbidden defaults omitted; selected regression passed. | specs/api/secrets.md:163-173; internal/desiredstate/compilation/entitlement_defaults_test.go:18 |
| R2-05-07 | Low | Addressed | Existing type/input table now also gives stored parts; generation support stays with owning API. Consolidation location differs from recommendation, but exact set flags and parts are documented. | specs/secrets.md:61-70; specs/api/secrets.md:26,114-126 |
| R2-05-08 | Low | Partial | Stale prose corrected and README qualification limits/ledger links added. Recommended per-row admission/plan/accepted Status column and guard are absent; current index deliberately states outcomes only. No explicit decline of status-column recommendation found. | examples/README.md:11-52; specs/milestones/delivered.md:8878-8882 |
| R2-05-09 | Low | Addressed | Diagnostics now use authored positions including set-valued add-on lists. | specs/api.md:165-166; specs/milestones/delivered.md:8788 |
| R2-05-10 | Low | Addressed | Example Environment names distinct with guard; name is a label, not execution identity. Selected test checked in review. | cmd/bootwright/example_environment_names_test.go:19; specs/api/environment.md:3-7 |
| R2-05-11 | Low | Addressed | License true explicitly labeled placeholder/operator acceptance in scenario READMEs and index; keeping value was audit recommendation. | examples/README.md:67-73; examples/ceph-clusters/libvirt-6-nodes-ceph-services/README.md:33 |

### S6 — 31 findings

| Finding | Original severity | Status | Assessment and qualification | Evidence |
| --- | --- | --- | --- | --- |
| F17-01 | High | Addressed | The support-catalog contradiction was removed and guarded. U03 is explicitly retained as a native replica-safety rule by D183/Q64, subsequently made distribution-neutral in X175. | specs/ceph-coverage.md; test/architecture/docs_test.go:TestDocsSpecsStateNoVendorSupportStatus; specs/milestones/delivered.md:8362 |
| F06-01 | Medium | Partial | Per-object claim/gap/overlap enforcement exists and tests pass. The selector contract still names CephCluster.spec.type at architecture.md:1160, although the dedicated CephCluster schema has no type field; correct the stale selector and complete domain selection records. | internal/reconciliation/lifecycle/unclaimed_object_test.go:25; cmd/bootwright/claims_linux_amd64_test.go:34; specs/architecture.md:1157; api/v1alpha1/storage.go:91 |
| F06-03 | Medium | Addressed | A source-level identity/arm fitness rule and negative fixture enforce the boundary; targeted tests pass. | test/architecture/implementation_identity_test.go:172; specs/architecture.md:1176 |
| F06-04 | Medium | Addressed | The documented minimum touch set, missing-seam rule and shared contract-suite procedure now exist. | specs/architecture.md:450; .agents/skills/extend-implementation/SKILL.md:20 |
| F06-07 | Medium | Addressed | Managed OS owns a retirement fan-in port; composition gathers every implementation policy. Proof requirements pass through the dependency evidence seam rather than a hard-coded bare-metal block ID. | internal/managedos/installation/contracts.go:45; cmd/bootwright/wiring_reconciliation.go:295; internal/managedos/installation/erase_test.go:262 |
| F06-09 | Medium | Addressed | Admission cannot specialize by release label; facts come from frozen artifacts and catalog/registry tests enforce the form. | specs/architecture.md:1109; test/architecture/version_specializations_test.go:219 |
| F06-10 | Medium | Addressed | The normative contract now explicitly permits a pre-effect refusal seam for positively absent native mechanics while preserving unknown/general behavior. | specs/architecture.md:1119 |
| F07-03 | Medium | Addressed | Product policy moved behind family packages, membership functions and neutral StorageEvidence. Family roots retain only registry/fact surfaces. | specs/architecture.md:66; internal/storage/cephcluster/; internal/containercluster/openshift/; internal/environment/inspection/contracts.go; cmd/bootwright/wiring_desiredstate.go:52 |
| F18-03 | Medium | Addressed | One substrate dispatcher now serves installation and power consumers; Go consumers use target facts. Adding an arm has the explicitly bounded additional schema choice edits in machine_power and machine_power_read. | specs/architecture.md:885; ansible/collections/ansible_collections/bootwright/core/roles/substrate_machine/; specs/substrates.md:39 |
| F18-04 | Medium | Addressed | Kind facts, membership and family accessors replace cross-domain field-path reads; planned descriptions supply CLI labels, with exhaustiveness and boundary guards. | specs/architecture.md:73; cmd/bootwright/wiring_members_test.go:13; internal/containercluster/facts_test.go:11; test/architecture/implementation_identity_test.go:172 |
| R2-01-01 | Medium | Addressed | A normative reference-edge catalog now covers capability requirements; the example-corpus edge test passed. | specs/state-reconciliation.md:932; cmd/bootwright/edge_catalog_linux_amd64_test.go:91 |
| R2-01-02 | Medium | Addressed | Hosted KubeVirt proof requires its host installation, add-on prerequisites and cluster network. The implementation and nested-host ordering regression test exist. | specs/substrates.md:903; internal/substrate/kubevirt/nested_test.go:102 |
| R2-01-04 | Medium | Addressed | Closure retains cluster children and both CephReplication endpoints as roots rather than a stale four-kind enumeration. | internal/environment/selection_closure_test.go; specs/api/environment.md |
| R2-01-05 | Medium | Addressed | Authored cluster lists retain standalone Bootwright-installed Machines and their provider closure; the targeted regression is present. | internal/environment/selection_closure_test.go:75 |
| F06-05 | Low | Addressed | The context table and following rules now name container/storage families and their current product implementations. | specs/architecture.md:59; specs/architecture.md:66 |
| F06-06 | Low | Addressed | The first-consumer seam is explicit and shared capability behavior is exercised against real bindings and a synthetic double; capability suite passed. | specs/architecture.md:450; cmd/bootwright/capability_contract_test.go:494; internal/environment/second_storage_implementation_test.go:33 |
| F06-08 | Low | Addressed | Effect-bearing knowledge lives in owning domain predicates; the engine's missing-claim check is generic and its regression passed. | internal/storage/effect.go; internal/containercluster/effect.go; internal/reconciliation/lifecycle/unclaimed_object_test.go:25 |
| F06-11 | Low | Addressed | A change to request/evidence/plan/intent/order requires a new implementation identity; registry discovery uses a marker and its seeded-registry test passes. | specs/architecture.md:1130; test/architecture/version_specializations_test.go:358 |
| F06-12 | Low | Partial | The Selection/Implementations/Refusal table template is required on a page's next edit, but these pages and ceph.md were edited again Oct 28-29 and still omit the Implementations heading/record. The instruction was added before those edits (e2fa5bb3c). | .agents/skills/architecture-best-practices/SKILL.md:32; specs/substrates.md:20; specs/openshift-clusters.md:25; specs/infrastructure-services.md:13 |
| F07-07 | Low | Partial | Allocation projection moved to the libvirt owner, but both composition bindings are still libvirt-only and container-cluster preflight still imports Storage's Report, Graph, Ownership and Projector. The neutral/shared preflight boundary remains unfinished. | internal/substrate/libvirt/capacity.go:15; cmd/bootwright/wiring_preflight.go:23; internal/containercluster/preflight/contracts.go:7; internal/containercluster/preflight/service.go:15 |
| F07-08 | Low | Addressed | A role/plugin/playbook naming scheme and exact shrinking legacy allowances replace three unstated schemes; X162 records the renamed plugin/playbooks. | specs/architecture.md:450; specs/architecture.md:814; ansible/collections/ansible_collections/bootwright/core/tests/unit/test_collection_names.py |
| F17-06 | Low | Addressed | Coverage pages now name their actual product families and the index routes them explicitly; the proposed directory layout was optional. | specs/ceph-coverage.md; specs/openshift-coverage.md; specs/index.md:29 |
| F18-07 | Low | Addressed | Power and state reading use the substrate dispatcher and admit kubevirt. HEAD includes both externally supplied and context-built KubeVirt host access. | specs/substrates.md:1158; ansible/collections/ansible_collections/bootwright/core/roles/machine_power/meta/argument_specs.yml:12; ansible/collections/ansible_collections/bootwright/core/roles/machine_power_read/meta/argument_specs.yml:24 |
| F18-09 | Low | Addressed | Managed-service selection now filters the declared implementation and freezes the managed-service-v4 identity. | internal/infrastructureservices/managedservice/selection.go:130; specs/milestones/delivered.md:8659 |
| F18-10 | Low | Addressed | Storage owns its attached-gateway DNS contribution, bound in composition; generic managed service code does not read Ceph field paths. | cmd/bootwright/wiring_reconciliation.go:435; internal/storage/cephcluster/; specs/architecture.md:68 |
| R2-01-03 | Low | Addressed | A nested cluster retains its host cluster as a complete root; hosted KubeVirt is now implemented. | internal/environment/selection_closure_test.go:119; specs/substrates.md:903 |
| R2-01-06 | Low | Addressed | An attachment reaching a cluster excluded by an authored list is a validation error under D183/Q75 rather than silent partial retention. | internal/environment/selection_closure_test.go; specs/milestones/delivered.md:8668 |
| R2-01-07 | Low | Explicit decision | D183/Q115 explicitly keeps validating both reserved CustomPlaybook and add-on-step shapes; legacy ordering fields remain deliberately reserved until B102/B90. | specs/milestones/backlog.md:3210; specs/milestones/delivered.md:8672 |
| R2-02-04 | Low | Addressed | BMC variation is normative capability discovery through Redfish resources, not a vendor/model specialization or a trust-layer decision. | specs/architecture.md:1092; specs/substrates.md:1392 |
| V2-10 | Low | Addressed | Implementation constraints are predicates over artifact facts or authored arms, never version ranges, matrices or component pairing rules. | specs/architecture.md:1140 |
| V2-18 | Low | Addressed | The path rule now matches capability-plus-operation playbook names, with binding and collection-name tests. | specs/architecture.md:814; ansible/collections/ansible_collections/bootwright/core/tests/unit/test_collection_names.py |

### S7 — 12 findings

| Finding | Original severity | Status | Assessment and qualification | Evidence |
| --- | --- | --- | --- | --- |
| F08-01 | Medium | Addressed | The fixed-worker exception specifies embedded source, stdin data, interpreter/privilege boundaries and one quoting owner, guarded by AST tests. | specs/security.md:256; ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/remote_command.py; ansible/collections/ansible_collections/bootwright/core/tests/unit/test_python_sites.py:255 |
| F08-04 | Medium | Addressed | Python has a language matrix row, execution-site/reference owner, layout rules and file-extension routing from code-implementation. | specs/architecture.md:756; .agents/skills/code-implementation/references/python.md; .agents/skills/code-implementation/references/formatting.md:104 |
| F08-06 | Medium | Addressed | Canonical JSON and process launching have shared owners, bundled into workers; exact exceptions and alias-aware AST checks bound remaining independent sites. Cross-language vectors explicitly pin the intentional ASCII-encoding difference. | specs/architecture.md:772; ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/canonical_json.py; ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/bounded_process.py |
| F08-07 | Medium | Addressed | The placement matrix and tracked-source mapping check exist; positive and seeded unmapped-source tests passed. | specs/architecture.md:756; test/architecture/language_rows_test.go:74 |
| F08-02 | Low | Addressed | Adapters own evidence projection and defensive field checks; Go alone judges whether it proves the postcondition, explicitly citing D11/D99. | specs/architecture.md:1072 |
| F08-03 | Low | Addressed | The workflow ban is scoped to product workflow and cross-block ordering; fixed native procedures inside one frozen request are allowed. | specs/architecture.md:901 |
| F08-05 | Low | Addressed | Execution sites and their interpreter rules are now explicit; managed-host floor references the code constant/locked floor configuration instead of duplicating a literal. | specs/architecture.md:768; .agents/skills/code-implementation/references/python.md:24 |
| F08-08 | Low | Addressed | One network-placement rule distinguishes managed component contact from bounded controller egress, and names the direct-SSH exception. | specs/architecture.md:739 |
| F08-09 | Low | Addressed | The controller Ansible runner is scoped to the controller stage; setup's own host packages/runtime remain Go-owned. | specs/architecture.md:929 |
| F08-10 | Low | Addressed | Tooling language/interpreter rules now exist, legacy shell engines are named exceptions, and docs-check selection is held to a test inventory. | .agents/skills/code-implementation/references/python.md:83; .agents/skills/code-implementation/references/formatting.md:114; test/architecture/testdata/docs-checks.txt |
| V2-14 | Low | Addressed | The multi-play exception is explicit and permits only the eight listed internal task files after first-play request validation. | specs/architecture.md:841; ansible/collections/ansible_collections/bootwright/core/tests/unit/test_playbook_entry_points.py |
| V2-15 | Low | Partial | Argument depth and undeclared-variable rules/tests exist, but B431 still retains bare nested mappings, raw nativeStaging, three cited incomplete schemas, and 29 existing undeclared reads. This is a completed spec correction with unfinished adapter cleanup. | specs/architecture.md:836; specs/architecture.md:849; specs/milestones/backlog.md:791; ansible/collections/ansible_collections/bootwright/core/tests/unit/test_entry_point_reads.py:129 |

### S8 — 88 findings

| Finding | Original severity | Status | Assessment and qualification | Evidence |
| --- | --- | --- | --- | --- |
| CX-API04 | High | Addressed | Written-contract fix complete. B659/B662 forward recovery and B491 online retirement remain undefined/unimplemented; physical erase-policy retirement already exists, correcting the audit's blanket no-backward-exit claim. | specs/ceph.md:700-707,900-909; specs/milestones/m2.md:1934-1961; D183 Q69 explicitly makes B659/B662/B491 prerequisites of B485. |
| CX-R5 | High | Addressed | Targeted test passed; no actual host effects exercised. | specs/state-reconciliation.md:585-605; internal/reconciliation/lifecycle/inconclusive_removal_test.go:110. Failed reads stay unknown; typed inconclusive permits one owned replay-safe retry. |
| F10-01 | High | Addressed | Root identity/MAC tests passed; operator physical qualification is separate. | specs/managed-os.md Physical installation; internal/managedos/installation/target_proof_test.go:103,189; selection_test.go:506; security-safety skill uses WWN by-id paths. |
| F10-03 | High | Addressed | Only the audit's requested interim correction is complete. Persistent incarnation ownership B568 remains open, including libvirt, emulated BMC, service and artifact objects. | specs/state-reconciliation.md:2149-2160; specs/contexts.md:947-956; B568 links and same-name orphan warnings now explicit. |
| F13-01 | High | Addressed | Historical registry-version mentions remain intentionally about refused stores, not this build's current format. | specs/contexts.md Storage locking and publication; specs/contexts/controller-record.md; test/architecture docs checks compare record bounds/versions to code; X160/X164. |
| R2-02-01 | High | Addressed | Surviving-consumer refusal tested and passed; online cleanup remains B491. | specs/state-reconciliation.md:2179 onward; internal/reconciliation/lifecycle/consumer_survival_test.go:124; X167. |
| R2-04-01 | High | Partial | vCenter credentialsRef still lacks the access/effect class in specs/deferred/machine-arms.md:29; B99 explicitly retains classification and rotated-login destroy test at specs/milestones/m1.md:595-601. No current vSphere runtime exists. | specs/secrets.md:320-331; state-reconciliation.md:1838 onward; internal/reconciliation/lifecycle/rebind_test.go. KubeVirt access credential now part of B87 implementation. |
| R2-06-01 | High | Addressed | Shared bridge-use claims/releasing guard tests passed. Independent live guest enumeration before deleting a managed network remains B567 (substrates.md:297-299). internal/substrate/libvirt/capability.go:575-579 still returns Quiescent based only on this context's Machines. | specs/substrates.md:315-327; infrastructure-services.md:130; internal/substrate/libvirt/shared_claims_test.go:48; contextfs/system_claim_linux_amd64_test.go:152. |
| CX-R4 | Medium | Addressed | B103 remains open: rollback detection/fresh seal key not implemented. The audit's requested truthful restore boundary is now documented. | specs/contexts.md:835-864 defines matching whole-store image, retained device/inode identity and no supported restore command; other remedies link this boundary. |
| CX-R6 | Medium | Addressed | No new command-effect taxonomy; Observe may rebuild cache-only runtime as specified. | specs/cli/commands.md Effects classification; specs/security.md Required security proof; specs/controller.md The controller runtime; X160. |
| CX-R9 | Medium | Addressed | Separate existing run-retention issue documented at cli.md:1494; not the audited fan-out defect. | specs/cli.md Machine SSH sessions; internal/machine/access/several_test.go; X166 bounds machine/cluster exec and cancellation. |
| CX-RULEplanbytes | Medium | Addressed | None found. | specs/contexts.md bounds table names MaxPlanBytes; state-reconciliation.md Operation records defers to those bounds. |
| CX-X1 | Medium | Addressed | Recovery no longer labels installed-but-off no-effect or reruns installer after recorded install proof. | specs/managed-os.md:640-654,823-824; X165 records durable disk-boot intent before power-off. |
| CX-X6 | Medium | Addressed | None found. | specs/openshift-clusters.md:666 own-image definition and :917 reference; pushed transport definition at :216 is a distinct transport case. |
| F09-01 | Medium | Explicit decision | One unresolvable block can still prevent whole removal; orphan-acknowledged context deletion remains the accepted exit. | D183 Q92 at specs/milestones/backlog.md:3187; X182 explicitly declines scoped abandonment under D38. |
| F09-02 | Medium | Addressed | Targeted entry and byte reserve tests passed. | specs/contexts.md:640-681; internal/reconciliation/lifecycle/registration_test.go:1122,1184; X176 completes entry/byte accounting. |
| F09-03 | Medium | Addressed | Whole-frozen-plan destroy over lost/non-done block tests passed. | specs/state-reconciliation.md:506,1586; internal/reconciliation/lifecycle/completed_removal_test.go:21; X170. |
| F09-04 | Medium | Addressed | Superseding removal uses its own ownership check after apply observation; drift alone does not prohibit removal. | specs/state-reconciliation.md:1465-1476; internal/reconciliation/lifecycle removal-resolution tests; X160/X170. |
| F10-02 | Medium | Addressed | Targeted physical proof tests passed; controller/self-target refusal included in implementation. | specs/substrates.md Identity and power operations; state-reconciliation.md:2401; internal/machine/power/service_test.go:678,720,879. |
| F10-04 | Medium | Addressed | Surviving physical/other consumers block retirement before registration; online removal B491 remains. | specs/state-reconciliation.md:2179-2190; specs/ceph.md Removal and concurrency; X167. |
| F10-05 | Medium | Addressed | Exact MAC-set test passed; management-controller host interface exception is explicit. | specs/state-reconciliation.md Physical disk safety; internal/managedos/installation/target_proof_test.go:132; X165. |
| F11-01 | Medium | Addressed | Compromised remote principals remain an explicit trust-model limitation. | specs/security.md:84-103 names remote principals and trusted assertions; state-reconciliation.md:2139 links it. |
| F11-02 | Medium | Addressed | Publisher HTTPS metadata is the declared authority; publisher signatures deliberately not verified, disclosed as widening. | specs/controller.md:393-408; specs/security.md:716-722; target-tool verification tests and X160. |
| F11-03 | Medium | Addressed | Unattended enrollment/replacement requires the supplied SHA256 fingerprint. | specs/cli.md machine trust --expect; X166 and machine trust tests. |
| F11-07 | Medium | Addressed | No real controller/remote scratch cleanup executed in this review. | specs/milestones/delivered.md X164/X176; runner scratch now under root-owned /var/lib/bootwright-staging/runs with per-owned-directory counting. |
| F11-08 | Medium | Addressed | Usernames explicitly nonsecret identity metadata; signed URLs enter privately through file input. | specs/security.md:110,121-122; secrets.md:165-169; media add --from-url-file; X166. |
| F12-01 | Medium | Addressed | Selected input revision is now part of handoff readiness. | specs/state-reconciliation.md:2086-2094,2116-2127; lifecycle handoff tests; X170. |
| F12-02 | Medium | Addressed | Partial removal reports unknown rather than absent. | specs/state-reconciliation.md Comparison states; X170/X180; lifecycle completed_removal and diff tests. |
| F12-03 | Medium | Addressed | Only implementations listed in observable-fields table claim field projection. | specs/add-ons.md:381-394 names own-but-different partial realization and first differing field; state-reconciliation.md observable-fields table. |
| F13-02 | Medium | Addressed | Prompts hold no exclusive root lock; reproof occurs after confirmation. | specs/contexts.md:429-441,1031-1036; X164 prompt-race and no-wait tests. |
| F13-03 | Medium | Addressed | Earlier builds may label new keyring index corrupt, explicitly documented; current reader separates newer format. | specs/state-reconciliation.md:455-465; specs/secrets.md keyring minReader refusal; contexts.md format/version remedies; X163/X181. |
| F13-04 | Medium | Addressed | A failure before a dnf transaction clears intent; interrupted native transaction still requires setup. | specs/controller.md:846-871; X170 setup failure clearing pending intent. |
| F13-05 | Medium | Explicit decision | Runtime/wheel pinning for all unfinished operations deliberately declined; current-plus-previous retention and network/mirror recovery remain. | D183 Q95; specs/controller.md:835-836,885-891; state-reconciliation.md:1255-1260; X170. |
| F13-07 | Medium | Addressed | Production tmpfs/overlayfs refusal implemented; isolated test roots remain permitted. | specs/contexts.md production/test filesystem boundary; contextfs filesystem tests; X164. |
| R2-02-03 | Medium | Addressed | In-tree ownership/UID pins, lost-reply adoption, namespace/VM claim implemented; real-system acceptance of B87 still pending. | specs/substrates.md KubeVirt Machine realization, especially :1054-1065 and :1105-1115; B87; internal/substrate/kubevirt tests. |
| R2-03-01 | Medium | Explicit decision | Owner explicitly retained D145(4), no removal digest split. Frozen-request identity and classified fixed literals guard the chosen alternative. | D183 Q76; X163; specs/state-reconciliation.md Dependency safety during recovery. |
| R2-03-02 | Medium | Addressed | Semantic proof equivalence retains human judgment; mechanically catalogued identity literals are checked. | X163; contract goldens now request-version/mark-formats/proof-ids/fixed-places/effect-pins/request-identity; architecture contract/literal guards. |
| R2-04-02 | Medium | Addressed | Owner authorized S3 reveal via D183 Q100/Q116; per-user selector prevents blanket key dump. | specs/secrets.md Produced material reveal table; cli.md cluster info --object-user; X166. |
| R2-04-03 | Medium | Addressed | Native retention remains outside keyring/context purge by design and is disclosed on abandonment. | specs/secrets.md:418-446 Native-side retention; contexts.md:958-967 deletion inventory; X181. |
| R2-04-04 | Medium | Addressed | Dashboard reachability status distinct from custody unproved mark. | specs/secrets.md:367-371; access.unproved at cli/output.md:1016; X166. |
| R2-06-03 | Medium | Addressed | Canonical endpoint and bmc-system UUID claims implemented; coordination deliberately one store and UUID-less systems cannot add UUID key. | specs/substrates.md:683-697; infrastructure-services.md:136-137; X165. |
| R2-06-05 | Medium | Addressed | D188 permits broader main-table gateway routes with warning; overlapping/on-link unsafe shapes refuse before net-define. | specs/substrates.md Managed networks; X170/X178; libvirt route tests. |
| R2-06-06 | Medium | Addressed | Address plus pinned SSH host-key qualification coordinates contexts of one store; separate stores remain uncoordinated. | specs/infrastructure-services.md:210-227; X170/X180/X188; contextfs host-key claim tests. |
| V2-03 | Medium | Addressed | Status-only undeclared-object visibility B578 remains a separate older item; deletion no longer relies on status alone. | specs/contexts.md:932-943 names status plus diff/lifecycle inventory; X165/X181 deletion inventory golden. |
| V2-20 | Medium | Addressed | Settled-apply log restoration test passed; status never clears the fault. | specs/state-reconciliation.md:1515-1525; internal/reconciliation/lifecycle/handoff_test.go:14. |
| CX-X3 | Low | Addressed | Time quality is explicitly not a synchronization guarantee. | specs/infrastructure-services.md:629-633,659-667 states local stratum and responding-source readiness. |
| F09-05 | Low | Addressed | Unknown continuation resolves first, then retries in the same invocation after all unknowns clear. | specs/state-reconciliation.md state-machine table and :607-613; X170. |
| F09-06 | Low | Addressed | Durable record recovery is implemented; no in-memory-only completion authority. | specs/state-reconciliation.md Operation state precedence; X170 scheduler rereads after failed publication. |
| F09-07 | Low | Addressed | No new work admitted after unknown; in-flight work is joined. | specs/state-reconciliation.md Converging an effect; unknown admission stop rule. |
| F09-08 | Low | Addressed | Historical commit-specific explanations removed from normative rule. | specs/state-reconciliation.md:82-97 generic finalization rule; X160. |
| F10-06 | Low | Addressed | Noninteractive data-loss token binds to plan digest; interactive bare token remains accepted by decision. | specs/state-reconciliation.md Authorizations; X170; bound data-loss authorization tests. |
| F10-07 | Low | Addressed | Contract gap fixed; vSphere remains unimplemented under B99. | specs/substrates.md:1134-1142; B99 ownership/unknown clauses. |
| F10-08 | Low | Addressed | Mark fallback explicitly recovery heuristic, not outright proof; reduced-trust basis preserved in evidence. | specs/add-ons.md:201-216 receipt ownership basis created/uid-matched/mark-recovered; X170. |
| F11-04 | Low | Addressed | Real sudo/PAM acceptance was not performed by the delivering lane or this review. | specs/security.md:430-446; X166 absolute-path sudo reexecution plus same-file proof. |
| F11-05 | Low | Addressed | Per-context access control is explicitly outside the model. | specs/security.md:140-144 says sudo-admitted accounts fully trusted across contexts. |
| F11-09 | Low | Addressed | Encryption protects material-only copies, not root compromise or whole-store theft including keys. | specs/secrets.md local-keyring encryption threat explanation near :600; security.md attacker model. |
| F11-10 | Low | Addressed | None found. | specs/security.md:226-230 served-private-tree 0640 exception and group restriction. |
| F11-11 | Low | Addressed | Backup trust remains bounded by restore source and identity rules. | specs/security.md:11-17 root-only held-handle state trust; keyring MAC separately named. |
| F11-12 | Low | Addressed | No contradictory universal proxy forwarding rule found. | specs/security.md process/network environment rules project one admitted route; X160. |
| F12-04 | Low | Partial | Scope contradiction survives. Code StageTail uses same-stage dependencies. B884 tracks adjacent stale bootstrap Not yet met line, but does not close the ordering mismatch. | specs/add-ons.md:74-80 explicitly limits last package to add-ons-stage blocks and excludes other-stage API users; state-reconciliation.md:2106-2108 still promises every other block that reaches the cluster; m4.md:1491 repeats broader claim. Code: internal/reconciliation/lifecycle/stage_tails.go:31; TestLastInstanceWaitsForEverySameClusterStageConsumer passed and deliberately omits the other-stage block. |
| F12-06 | Low | Addressed | Restoring original bytes still cannot settle (B598); warning now explains this accepted restriction and destroy exit. | specs/state-reconciliation.md:34-40; contexts.md context-update warning; internal/workspace/contexts/service_test.go:1521. |
| F12-07 | Low | Addressed | None found beyond separate F12-04 early-handoff scope mismatch. | specs/state-reconciliation.md:2086-2127 defines predicate and ordered status reasons, explaining done evidence. |
| F12-09 | Low | Addressed | Input line points to status rather than promising continuation/settlement. | specs/cli.md diff Input line; X172. |
| F13-06 | Low | Addressed | Rebind tests passed. A filesystem-device-number change remains outside the supported rebind. | specs/contexts.md:100-116 controller rebind and ordered exits; X164; controller_rebind tests. |
| F13-08 | Low | Addressed | Append-only source identities retained deliberately; fresh-store/other-host remedy meets report section 8.B3 smallest correction. Optional retirement not adopted. | specs/contexts/controller-record.md:169-176; TestToolSourcesPastTheBoundNameTheFreshStoreExit; X164. |
| F16-04 | Low | Addressed | None found. | specs/cli/output.md:823; X172 TestStatusStepsAreExactlyWhatTheVerbsDecisionsDerive. |
| F16-05 | Low | Addressed | Preflight add-ons remains B770; requested local readiness rows implemented. | specs/cli/output.md:816,822-823; cli.md:803-817; X172/X177/X191 status and implemented preflight. |
| F16-06 | Low | Addressed | Implemented via existing remediation member instead of duplicate optional next field; same observable remedy requirement. | specs/cli/output.md:425-428 requires remediation on errors except cli.usage and exact command where appropriate; X172 diagnostic guards. |
| F16-09 | Low | Addressed | Runbooks now own journeys; example README intentionally concise. | examples/README.md:75-85; e2e/runbooks/lab-rhel.md:127-138 validate then full plan before staged apply. |
| F16-10 | Low | Addressed | Dashboard reveal retained; S3 reveal selects one user. | cli.md cluster info --object-user; X166 and D183 Q116. |
| F16-11 | Low | Addressed | Opt-in exit 3 on changed/added/removed/unknown; default remains 0. | specs/cli.md:842-848; cli/output.md:13; X172 diff exit-code tests. |
| R2-03-05 | Low | Addressed | Unknown fields still refuse; record writing build used where available. | specs/state-reconciliation.md:455-465 one-way optional-member evolution rule. |
| R2-03-06 | Low | Addressed | None found. | specs/state-reconciliation.md:1283-1289 defines effect pin and digest ownership; X163 goldens. |
| R2-04-05 | Low | Addressed | Nonsecret parts may be material values; secret parts remain files/no_log module inputs. | specs/security.md:147-159 material channel and secret-file handling; secrets.md part classification. |
| R2-04-06 | Low | Addressed | Native-side plaintext retention explicitly owned and disclosed; no same-name adoption of installer work area. | specs/secrets.md:418-446; contexts.md:958-967; X166/X181 installer incarnation and private file checks. |
| R2-04-07 | Low | Addressed | No actual remote host exercise here. | specs/security.md:151-159 pipelining; X181 local connection pipelining/private_file/structural transfer checks. |
| R2-04-08 | Low | Addressed | New remediation-dependent live qualification B867 remains (state-reconciliation.md:753-756; backlog.md:2951-2962). Original missing contract/in-tree guard addressed, assumption not live-proved. | specs/state-reconciliation.md:728-735 recapture authority/principal/FSID creation identity; X181 custody identity tests. |
| R2-06-02 | Low | Explicit decision | Tombstone scheme explicitly declined; acknowledged orphan deletion frees claims by design. | D183 Q67 preserves D38; contexts.md:971-978 warns exclusive keys released while objects remain. |
| R2-06-04 | Low | Addressed | Shared emulated-BMC key and release exclusion tests passed. | specs/substrates.md:499; infrastructure-services.md:136; shared_claims_test.go:64 and X165/X180. |
| R2-06-07 | Low | Addressed | None found. | specs/infrastructure-services.md Release table :176-183; contexts/controller-record links that owner. |
| R2-06-08 | Low | Addressed | Pre-publication controller.conflict names bound and cleanup remedy. | specs/contexts/controller-record.md:144-156; X164 reservation-bound test. |
| V2-04 | Low | Addressed | No stale one-failed-block retry guarantee found. | specs/cli.md lifecycle behavior links stage/retry owner; state-reconciliation.md:607-613; X160/X172. |
| V2-05 | Low | Explicit decision | No indefinite offline retention promise; network/mirror setup recovery deliberately accepted. | D183 Q95; specs/controller.md:885-891 format healing and recovery wording. |
| V2-06 | Low | Addressed | None found. | specs/cli/output.md:264-278; X164 context list unfinished operation/build; operator-guide upgrade procedure. |
| V2-07 | Low | Addressed | Heuristic explicitly scoped; external concurrent native writers remain outside guarantee. | specs/state-reconciliation.md:2162-2170 lost-reply adoption and single-writer condition; ceph-resources.md references it. |
| V2-09 | Low | Addressed | Plan impacts and resource scope rows disclose realm/filesystem-wide grant. | specs/cli/output.md:656-665 grant clauses; internal/storage/ceph/output_spec_test.go:107-124; X167. |
| V2-13 | Low | Addressed | No post-digest material-value dispatch arm remains. | internal/machine/power request carries substrate in frozen request; X169 and architecture_harness agent cross-check. |
| V2-17 | Low | Addressed | Documentation widened to all opted-in Ceph calls. | specs/architecture.md:945 nativeError whole opt-in set; X160. |

### S9 — 22 findings

| Finding | Original severity | Status | Assessment and qualification | Evidence |
| --- | --- | --- | --- | --- |
| F17-03 | High | Addressed | The chosen single Active slices table supersedes duplicating Delivery in every Planned slices table. | specs/milestones.md:27-53 centralizes active slices, authorizing decisions and per-program WIP; test/architecture/milestones_test.go:553 checks consistency. X160 records that the audit misread D168 as pausing M2c (delivered.md:8286). |
| F05-08 | Medium | Addressed | Storage contract spans five to eight pages; deferred page contradicts itself | specs/api/ceph.md:11-36 routes each kind to one field record; :179-184 states distribution union, oss default and IBM-only realization. The stale deferred/storage-api.md page is gone. |
| F15-01 | Medium | Addressed | Current verification reran the budget gates, not the historical task-token experiment. | AGENTS.md:5-10 and specs/index.md:3-6 require narrow row/section reads; test/architecture/docs_test.go:28-42 budgets milestone openings; delivered.md:8907-8914 records moved narratives and 16.6k-token measurement. |
| F15-08 | Medium | Addressed | Docs gates bound entry files only, not the specs and milestones agents read | test/architecture/docs_gates_test.go:13,205,302,408 tests milestone opening budgets, spec-section budgets, reachability and normative-history ratchets, including seeded violations; docs-check passes. |
| F17-04 | Medium | Explicit decision | Owner chose binding pending clauses with a review queue, rather than making them inert. | specs/milestones.md:56-62 defines binding pending clauses and reopen-on-overturn; backlog.md:3186 records D183 Q91(a); backlog.md:3281 lists open session clauses. |
| F07-10 | Low | Addressed | Spec navigation cites a test-only function as the capability registry | specs/architecture.md:413,1146 names production buildCapabilitiesWith and its allowlisted lifecycle registry. |
| F13-09 | Low | Partial | The cut-over and publication duplication is reduced, but the requested single home for setup-subtree refusal/retention rules is not complete. This is documentation duplication, not a proved runtime defect. | specs/controller.md:822-826 links layout ownership; :928-932 delegates cut-over refusal to contexts.md:237. But controller.md:857-893 and contexts/controller-record.md:246-261 both describe pending/format refusal and healing. |
| F15-02 | Low | Addressed | Six-host Ceph cap is written as a target rule, contradicting D130 | specs/ceph.md:801 now defers only clusters beyond the run deadline; specs/milestones/m2.md:518 records implemented larger clusters under B890/X197; ceph-coverage.md:186-200 makes profiles samples, not product limits. |
| F15-03 | Low | Addressed | Stale, unindexed M2a storage API review page sits among normative specs | specs/storage-api-review.md:3-17 explicitly labels history and redirects accepted behavior; specs/index.md:44 indexes it as history; TestDocsEverySpecPageIsReachableFromTheIndex passes. |
| F15-04 | Low | Partial | Remove acceptance-only status from Not yet met paragraphs and leave it in milestone/ledger owners. Docs-check verifies the linked item is open, not whether code is actually missing. | specs/index.md:49-54 reserves Not yet met for code-unmet rules, but specs/openshift-coverage.md:141-143 still marks every row whose item is open, including B61/B66 whose in-tree code is delivered; specs/ceph-coverage.md:11 also includes tested-journey records. |
| F15-06 | Low | Addressed | Other program-local numberings remain local to their record, rather than becoming global IDs. | specs/milestones.md:109-125 defines identifier ownership and Q/OD allocation; milestone question tables use the common Status header; test/architecture/milestones_decisions_test.go:189 tests violations. |
| F15-07 | Low | Addressed | Budgets permit named existing exceptions; this is an incremental ratchet, not a claim that every section is already under the proposed target size. | specs/index.md:33-44 routes large domains by section; test/architecture/docs_gates_test.go:205 enforces section-size ratchets; docs-check passes. |
| F15-09 | Low | Addressed | Milestone item details restate spec rules, creating a second owner | specs/milestones/m2.md:1863-1897 reduces B688 to outcome, dependency and exit evidence; backlog.md:4619 retains the former definition narrative in D170. |
| F15-10 | Low | Explicit decision | The decision log is a 132 KB tail of backlog.md mixing pending clauses | backlog.md:3197 records D183 Q102(a): keep decisions in backlog with individual anchors/status tokens; backlog.md:3270-3288 and test/architecture/docs_gates_test.go:56 implement that choice. |
| F16-03 | Low | Addressed | The CLI help content contract exists only in delivered history, not in the CLI spec | specs/cli/commands.md:204-216 now owns Help content and names its golden/enum tests. |
| F17-05 | Low | Partial | Adoption duplication was removed. The proposed relocation of host-preparation safety mechanics out of the coverage page remains incomplete; this is a structural recommendation. | specs/ceph-coverage.md:130-133 now delegates adoption rules to ceph.md, but :75-128 still owns detailed host-preparation/probe safety mechanics, and ceph.md:642 explicitly delegates them back to coverage. |
| F17-07 | Low | Open | Add the two docs/acceptance.md templates and coverage links under B61/B755. Moving their ownership did not deliver them. | specs/milestones/m3.md:589-593 and m4.md:837-841 explicitly defer the missing Container cluster row and Add-on row templates until first acceptance runs; delivered.md:9177 records their transfer from B820. |
| F17-08 | Low | Partial | Complete the proposed migration of observed release facts to indexed knowledge; the governing release-selection rules are already corrected. | The release-evidence openings now link knowledge (openshift-coverage.md:14-25; add-on-coverage.md:15-27). However native release-specific statements remain, e.g. openshift-coverage.md:126 (4.22 Localnet behavior) and ceph-coverage.md:464-468 (v20.2.1 firewall behavior). |
| R2-02-05 | Low | Addressed | This satisfies the catalog recommendation for a substrate qualification section; no separate service-qualification catalog was added. | specs/substrates.md:111-123 adds qualification rows per realized arm with operator gates and accepted/unrun evidence, linked from specs/index.md:28. |
| V2-08 | Low | Addressed | secrets.md still gives unknown secret info names an empty selection, contradicting B651 | specs/secrets.md:135-139 delegates selection and unknown-name refusal to CLI; specs/cli/commands.md:99 confirms unknown names refuse. |
| V2-12 | Low | Addressed | Decision links have no per-decision anchor; each D<n> link opens the whole 132 KB log | backlog.md:3275 defines per-decision headings; individual ### D<n> anchors and TestDocsDecisionLinksTargetTheirOwnEntry at docs_gates_test.go:56 enforce targeted links. |
| V2-16 | Low | Addressed | substrates.md quotes libvirt request versions v4/v7 that the code and roles refuse | specs/substrates.md:412-413 cites hostRequestVersion and machineRequestVersion in code, without stale v4/v7 literals. |

### S10 — 14 findings

| Finding | Original severity | Status | Assessment and qualification | Evidence |
| --- | --- | --- | --- | --- |
| F14-02 | High | Addressed | The guarded hook skips primary checkout/re-entry, keys unchanged content, remembers failed keys and runs bounded advisory focus in the task worktree. | scripts/hooks/stop-check:18; .agents/skills/code-implementation/references/verification.md:64; .codex/hooks.json |
| F14-01 | Medium | Addressed | The entrypoint effect prohibition and path-independent ask rules exist; a catalog-based check covers every effectful command. | AGENTS.md:30; .claude/settings.json; test/architecture/docs_test.go:1056 |
| F14-03 | Medium | Addressed | Local fast-forward is the documented D168 default; PR/push waits for explicit owner/harness scope. | .agents/skills/code-implementation/SKILL.md:54; .agents/skills/code-implementation/references/git-integration.md:45 |
| F14-05 | Medium | Addressed | One capability-neutral safety class covers destructive effects, privilege, custody, ownership proofs and durable transitions, adding review/race coverage. | .agents/skills/code-implementation/SKILL.md:25; .agents/skills/code-implementation/references/verification.md:12 |
| R2-03-03 | Medium | Addressed | Go/Ansible digest checklists and the enforced Contract line exist, including unchanged-digest contradictions and coverage of implementation paths. | .agents/skills/code-implementation/references/go.md:116; .agents/skills/code-implementation/references/ansible.md:17; scripts/check-commits:40 |
| CX-G4 | Low | Addressed | Only inventories/process copies move into tests; required behavior stays in its owning spec and names the test. | .agents/skills/architecture-best-practices/SKILL.md:21 |
| CX-G5 | Low | Addressed | A blocking pre-existing defect can be fixed in its own commit after recording an item; D183/Q103 explicitly accepts this path. | AGENTS.md:21; specs/milestones/backlog.md:3198 |
| F14-04 | Low | Addressed | Tracked guidance directs local overrides to user-level configuration or requires that they defer to AGENTS.md first. | .agents/skills/index.md:28 |
| F14-06 | Low | Addressed | The gate policy is explicit and make focus/check/race are in the allowlist; unrun/flaky gates cannot be recorded as passes. | .agents/skills/code-implementation/references/verification.md:8; .claude/settings.json:10 |
| F14-07 | Low | Addressed | Contract/API/commit work has checklists in existing owners, matching the audit arbitration that rejected another change-class file. | .agents/skills/code-implementation/references/go.md:116; .agents/skills/ux-and-apis/SKILL.md:40; .agents/skills/code-implementation/SKILL.md:49 |
| F14-09 | Low | Addressed | Tracked guidance sends model selection and staffing preferences to user-level configuration. | .agents/skills/code-implementation/references/agent-operation.md:3 |
| F14-11 | Low | Addressed | Verification.md owns tier names/use; other guidance routes to it and lists commands for discovery rather than incompatible gate classes. | .agents/skills/code-implementation/references/verification.md:3; docs/development.md; specs/architecture.md:1277 |
| F14-12 | Low | Addressed | Bidirectional canonical/wrapper parity now rejects missing wrappers, orphan wrappers and mismatched frontmatter, with negative fixtures. | test/architecture/docs_test.go:463; test/architecture/docs_test.go:481; test/architecture/docs_test.go:513 |
| V2-11 | Low | Addressed | Parallel-only failure is FLAKY/75, the entire lane has a 900-second deadline, and termination kills child checks; regression tests cover each. | scripts/tools/focus.py:376; scripts/tools/focus.py:442; scripts/tools/focus_test.py:321; .agents/skills/code-implementation/references/verification.md:56 |
