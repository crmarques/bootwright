# Code audit against the specifications — Codex

Assessment date: **2026-11-05**. Reviewer: Codex, with delegated domain
reviews and independent checks. Original product baseline:
`f8b15669f8ece8183d9cada8191f2f3b56669ba9`. Completion baseline:
`88fbf7be19e41ddc756ea74591f2248973287298`. Between them, the repository
added the other audit report and container-build support; the application and
Ansible sources cited below did not change. The build delta is assessed
separately below. Line references describe these baselines.

Scope: the desired-state API and compiler, CLI and composition, controller,
contexts, reconciliation, secrets and trust, infrastructure services,
substrates, machine operations, managed OS, OpenShift, Ceph, add-ons, native
artifacts, and their host-independent validation. This is a repository-wide,
risk-directed source and test audit. It does not claim statement-by-statement
verification of every file, native dependency, or specification clause.

This report records findings and correction proposals. It changes no product
behavior, contract, specification, milestone status, or operator acceptance.
It authorizes no implementation or live operation.

Reading order: [assessment](#assessment), [validation subtasks](#validation-subtasks),
[coverage](#coverage-and-positive-evidence), [findings](#findings),
[correction sequence](#correction-sequence), [verification](#verification-and-limits),
and [retained reproductions](#retained-reproduction-evidence).

## Assessment

The implementation has substantial protection at its dangerous boundaries:
strict admission, immutable requests, ownership receipts, lifecycle reproof,
confidential material types, descriptor-based file access and bounded adapter
protocols. Those strengths do not establish full conformance. The audit found
reachable cases where a completed effect is insufficiently proved, valid
input cannot complete, or an execution boundary does not meet its owning
specification.

**18 confirmed discrepancies: 15 medium-priority (P2) and 3 low-priority
(P3).** C15 partly overlaps existing backlog item B122. Counts include both
findings produced by the initial independent reviews and candidates from the
other report that were subsequently checked against source and specifications.
They are not a claim of 18 previously unknown defects. No P0/P1 conclusion is
established by this evidence. That is not a safety certification.

The first work should address physical disk selection, private publication
permissions, and KubeVirt completion proof. Next, repair the root/invoker
publication boundary and process qualification, then the add-on and Ceph
paths that admit work they cannot complete. Keep the narrower severity
qualifications below: a missing qualification check is not proof of an
exploited host, and a conditional permission window is not proof that every
installation disclosed a secret.

| ID | Priority | Confirmed discrepancy | Evidence |
| --- | --- | --- | --- |
| [C01](#c01-physical-agent-installation-admits-an-unstable-root-disk-selector) | P2 | Physical agent installation admits an unstable root selector | Admission probe; conflicting spec clauses |
| [C02](#c02-private-publication-temporarily-relaxes-file-permissions) | P2 | Private publication temporarily relaxes file permissions | Pinned Ansible primitive probe |
| [C03](#c03-kubevirt-completion-does-not-prove-readiness-and-frozen-shape) | P2 | KubeVirt completion omits readiness and recovery shape proof | Three failing synthetic regressions |
| [C04](#c04-render-publishes-as-the-effective-account) | P2 | Render publishes as the effective account | Invocation probe and production wiring |
| [C05](#c05-some-native-executables-bypass-required-qualification) | P2 | Native executables bypass required qualification | Unsafe-file qualifier probe and source |
| [C06](#c06-the-adapter-retains-system-plugin-search-paths) | P2 | Adapter retains ambient system plugin search paths | Pinned Ansible configuration inspection |
| [C07](#c07-controller-ssh-policy-rejects-a-disabling-directive) | P2 | Controller SSH policy rejects a disabling directive | Pure parser probe |
| [C08](#c08-a-pending-upgrade-plan-invalidates-a-healthy-pinned-add-on) | P2 | Pending upgrade plan invalidates healthy pinned add-on | Synthetic object-engine execution |
| [C09](#c09-cluster-object-projection-is-not-json-safe) | P2 | Cluster-object projection is not JSON-safe | Admission plus exact template evaluation |
| [C10](#c10-bucket-names-fail-only-at-the-native-worker) | P2 | Long bucket names pass earlier phases but fail in worker | Admission/planning versus worker bounds |
| [C11](#c11-authenticated-nvme-of-bindings-are-refused-by-planning) | P2 | Required NVMe-oF bindings are refused by planning | Schema/planner contradiction |
| [C12](#c12-transient-placed-secret-observation-is-not-retried) | P2 | Transient placed-Secret observation is not retried | Engine control flow |
| [C13](#c13-image-admission-accepts-unqualified-references) | P2 | Image grammar admits references without repositories | Failing lexical regression |
| [C14](#c14-http-listeners-inherit-forbidden-tls-defaults) | P2 | HTTP listeners inherit forbidden TLS | Failing inheritance/admission regression |
| [C15](#c15-operation-results-omit-required-block-facts) | P2 | Operation results omit required block facts | Result construction and existing golden |
| [C16](#c16-secret-version-ordinals-can-restart) | P3 | Secret version ordinal restarts after deletion | Real keyring/contextfs synthetic regression |
| [C17](#c17-canceled-cluster-info-leaves-borrowed-material-uncleared) | P3 | Canceled cluster info omits material cleanup | Failing CLI cancellation regression |
| [C18](#c18-malformed-sudo-default-lists-lose-the-unknown-result) | P3 | Malformed sudo default lists lose parse-failure state | Source and existing malformed-input test |

P2 means an actionable contract, safety-boundary, or supported-journey defect
whose demonstrated scope is bounded below. P3 means a narrower reporting,
cleanup, or defensive parsing defect. Severity reflects the established
trigger and consequence, not the worst hypothetical deployment.

## Validation subtasks

The task was split before reviewing implementation. Complex work used
**GPT-6 Astra, medium effort**, its default effort: it involves safety
refusals, cross-package contracts, durable state and Ansible adapters. Routine
inventory and report checking used **GPT-6.1 Sol, low effort**, its default.
No price, latency or benchmark superiority is claimed; the routing follows
the repository's complexity rule and the models available in this session.

| Subtask | Model / effort | Acceptance criteria |
| --- | --- | --- |
| State, context filesystem, secrets, trust, controller/process safety | GPT-6 Astra / medium | Trace required invariants through effects and recovery; inspect existing guards; reproduce concrete violations |
| Substrates, machines, infrastructure, OS and OpenShift installation | GPT-6 Astra / medium | Check identity, destructive selection, completion proof, private artifacts and capability claims |
| Ceph, add-ons, cluster objects and custom playbooks | GPT-6 Astra / medium | Check admitted versus executable input, ownership, credential boundaries and uncertain outcomes |
| Independent safety and integration cross-checks | GPT-6 Astra / medium | Challenge consequential candidates and severity; check the container-build delta |
| Final storage/add-on validation | GPT-6 Astra / medium | Resolve incomplete candidate handoffs against exact current source and owning clauses |
| Gate inventory, retained evidence and report consistency | GPT-6.1 Sol / low | Count tracked sources; separate executed, reused, failed and unrun checks; verify artifact consistency |
| API/CLI/compiler, aggregation and gate execution | Coordinating Codex session | Independently trace input/output contracts, run synthetic probes and repository gates, reconcile evidence |

The coordinator reviewed the domain findings rather than accepting model
rank as evidence. KubeVirt completion received a second domain review.
Sensitive-publication, disk-selection and render findings were cross-checked
again at completion. The object-user cleanup probe was narrowed to the
explicit object-user contract after the first dashboard-only reproduction.

The [Claude report](report-claude.md) appeared during this audit. Its candidate
list helped target a second pass. Its counts, clause-completeness claim and
all unexamined findings are **not adopted** here. In particular, C04, C07 and
C08 are assessed as P2 here; the higher severity and broader prevalence
claims of that report were not independently established. C08 also corrects
one detail: observation does set `upgradePending`, despite ending unknown.

## Coverage and positive evidence

At completion, the tracked source inventory contains 784 production Go
files, 1,138 Go test files, 177 non-test Python files, 225 Python test files,
27 roles with 198 role YAML files, and 69 playbook YAML files. Python counts
use test filename conventions. There are 35 specification Markdown files
outside the milestone index/tree and deferred tree; that count includes the
spec index, glossary and historical review, not just normative owner pages.
These are scope measurements, not claims that every line was inspected.

| Area and owning specs | Source/test paths reviewed | Assessment and limit |
| --- | --- | --- |
| [API](../../specs/api.md), [input](../../specs/api/input.md), [Environment](../../specs/api/environment.md) | `api/v1alpha1`, `internal/desiredstate`, `internal/environment` | Strict decoding, bounded acquisition, selection, inheritance and compiler purity inspected; C13/C14. No exhaustive combination test of all kind defaults |
| [CLI](../../specs/cli.md), [commands](../../specs/cli/commands.md), [output](../../specs/cli/output.md) | `internal/cli`, `cmd/bootwright` | Catalog/flag dispatch, structured-output ownership, cancellation and sensitive rendering inspected; C04/C15/C17. Gate covers catalog and presentation fixtures |
| [Architecture](../../specs/architecture.md) | Composition, ports, capability claims, architecture fitness suite | Boundaries and refusal tables have executable guards; passing those guards does not prove every semantic field path respects ownership |
| [Contexts](../../specs/contexts.md), [reconciliation](../../specs/state-reconciliation.md) | `internal/workspace`, `internal/reconciliation` | Shared proof/exclusive reproof, frozen plans, unknown outcomes and held-directory publication traced; no new store-corruption finding confirmed in this sample |
| [Secrets](../../specs/secrets.md), [security](../../specs/security.md) | `internal/secrets`, `internal/trust`, privilege and process adapters | Immutable IDs, authenticated custody and exact trust publication remain material safeguards; C05/C06/C16/C17/C18 |
| [Controller](../../specs/controller.md) | Controller runtime, privilege, Ansible launcher, request transport | Isolation, subprocess lifetime and request material boundaries inspected; C07. Not every downloader/install/uninstall branch independently reviewed |
| [Infrastructure services](../../specs/infrastructure-services.md) | `internal/infrastructureservices`, relevant roles/modules | Service ownership/removal and private publication sampled; C02/C14. No live listener, DNS, NTP or firewall qualification |
| [Substrates](../../specs/substrates.md) | libvirt, physical and KubeVirt adapters; machine power/trust | Identity, frozen shape, receipts and preboot proof sampled; C01/C03. Qualified BMC/native API behavior remains outside this audit |
| [Managed OS](../../specs/managed-os.md) | Media, installation, erase/evidence and publication roles | Disk guards, private material flow and installer sequencing inspected; C02. No native installer or real disk was exercised |
| [OpenShift](../../specs/openshift-clusters.md), [coverage](../../specs/openshift-coverage.md) | Agent selection, controller/media/completion boundaries, networking | C01 exposes a physical safety/domain-spec disagreement. Native release behavior and real cluster readiness were not proved |
| [Ceph](../../specs/ceph.md), [native API](../../specs/ceph-native-api.md), [resources](../../specs/ceph-resources.md) | Request construction, resources, RGW, ownership, retirement, native workers | Leaf pin before S3 transmission and distinct retirement paths inspected; C10/C11. Not exhaustive across every worker or release specialization |
| [Add-ons](../../specs/add-ons.md), [coverage](../../specs/add-on-coverage.md) | Admission/rendering, cluster-object engine, projection, OLM, placement | C08/C09/C12. UID/mark receipt checks and narrow secret reads counter several apparent unsafe patterns |
| [Custom playbooks](../../specs/api/custom-playbooks.md), native artifacts | Admission, unrealizable-kind handling, rendering/comparison | Enabled custom-playbook effects remain refused; disabled declarations are inert. C04 concerns publication credentials |
| Build, scripts, tests and e2e qualification | Makefile, workflows, pinned wrappers, new Docker build, acceptance boundaries | Static/in-tree verification only; no e2e case or operator acceptance was executed |

Examples of counterevidence that prevented unsupported findings:

- Adapter protocol decoding checks EOF, member/depth/record bounds and phase;
  acknowledgment distinguishes a broken pipe from uncertain delivery.
- Lifecycle execution uses stop-on-breach behavior, parent-death protection,
  inherited locks and process-tree supervision. Exclusive mutation rechecks
  the shared-read basis.
- Context file access uses held handles and bounded path components.
  Keyring publication revalidates expectations and distinguishes uncertain
  commits. Historical random-ID reservations are real, although they do not
  preserve the public ordinal in C16.
- The RGW client path that disables ordinary certificate-chain verification
  still compares the expected leaf fingerprint before transmitting S3 bytes;
  the inspected path is not a demonstrated TLS-authentication bypass.
- Mark-based cluster-object recovery has an explicit specification. It was
  not mislabeled as an accidental failure to demand a previously stored UID.
- Ansible private scratch, unsafe-tagged variables, pipelining and disabled
  SSH multiplexing are present. C06 identifies a remaining plugin path, not
  an absence of all runtime isolation.

Explicit deferrals are not counted as new defects: surviving-host Ceph
removal (B491), unimplemented package families such as cert-manager/GitLab
(B767/B93), recognized unavailable commands, and the recorded physical,
KubeVirt and vSphere qualification limits. Existing operator-acceptance gaps
remain open regardless of any in-tree pass.

## Findings

### C01 Physical agent installation admits an unstable root-disk selector

**P2; physical safety and domain-spec conflict.**
[Physical disk safety](../../specs/state-reconciliation.md#physical-disk-safety)
(lines 2414–2422) forbids selecting an operator-owned bare-metal disk merely
by its kernel name. The agent installer admits a physical node whose sole
root hint is `/dev/sda`:
[selection.go](../../internal/containercluster/agentinstall/selection.go)
(lines 171–178, 429–460). The physical test catalog constructs that shape in
[fixtures_test.go](../../internal/containercluster/agentinstall/fixtures_test.go)
(lines 131–145, 343 onward).

A pure Go probe of `Refusals(physicalCatalog())` found no refusal. If disk
enumeration changes, machine UUID/MAC proof still identifies the machine but
not the disk the installer will erase. No real disk was used in this audit.

**Counterevidence and decision:** the OpenShift domain spec explicitly
admits `/dev/<name>` and by-path syntax. The managed-OS API's by-id rule is
not the violated clause, and it cannot simply be copied into this native
projection, which refuses that syntax. The residual-risk exception for the
agent's missing pre-wipe hook does not explicitly waive durable disk
selection. Resolve this disagreement in the owning contracts, then require
supported stable identity or refuse the physical shape. Add a physical
admission/selection regression and preserve the separate operator gate.
Source-report cross-reference: [RCS-02](report-claude.md#rcs-02).

### C02 Private publication temporarily relaxes file permissions

**P2; conditional local confidentiality exposure.** Private consumers require
files to remain `0640 root:root`, with no access granted to other accounts
([private publication contract](../../specs/infrastructure-services.md),
lines 508–521). The installation role uses `copy: remote_src` directly into
its token directory for the recovery value and host identity:
[private_recovery.yml](../../ansible/collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/tasks/private_recovery.yml)
(lines 9–16) and
[private.yml](../../ansible/collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/tasks/private.yml)
(lines 28–39).

The pinned ansible-core 2.21.5 copy path calls `atomic_move` before applying
requested attributes. For a new destination, that primitive applies
`0666 & ~umask`; a deterministic synthetic-file probe under umask `022`
observed `0644` before the later mode setter. Production code supplies no
corresponding restrictive umask guarantee. Token directories are traversable
at `0711`. The image move's earlier argument vector can reveal the recovery
path to a local account where process visibility permits it.

**Limits:** final mode, random token, `no_log` and cleanup are present.
Actual concurrent access also depends on process visibility and SELinux.
The observed primitive window is proved; a universal exploit or a live
secret disclosure is not. The identified argument-vector sequence supports
recovery-path discovery, not discovery before the earlier host-key copy.
Stage with final permissions/ownership/label in private work and publish
without an intermediate permission relaxation. Test the publication boundary,
not just its final mode. Cross-reference: [MOI-04](report-claude.md#moi-04).

### C03 KubeVirt completion does not prove readiness and frozen shape

**P2; false completion and recovery.**
[KubeVirt realization](../../specs/substrates.md#kubevirt-machine-realization)
requires waiting for root DataVolume success and refusing reread differences
in frozen VM fields. In
[kubevirt_machine.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/kubevirt_machine.py),
`Host.wait` returns its last reading on expiry (130–137); apply ignores that
result (486–488); `settled` considers a present root and owned VM sufficient
(444–446). Observe reaches this settlement without `frozen_difference`
(450–451), although ordinary apply performs that check (482).

Three synthetic regressions failed: ordinary apply with a permanently
Pending root, observation with a Pending root, and recovery after changing
CPU cores from 4 to 99. Each still returned `postcondition=True`; the 26
existing tests in the narrowed file passed. Go then maps this evidence to
`EffectCompleted` in
[capability.go](../../internal/substrate/kubevirt/capability.go)
(lines 528–531). Dependent installation may proceed without a ready root,
and continuation may accept frozen-shape drift.

Cluster/object UID checks still prevent unrelated-object adoption; they do
not prove readiness or shape. Share a completion proof between apply and
observe, inspect the wait result, and retain pending work as partial/unknown.
Keep removal inventory separate so drift need not prevent safe removal.
Regression coverage should include Pending/Failed roots and each relevant
frozen-shape family. This finding received independent source review.

### C04 Render publishes as the effective account

**P2; incorrect invoking-account boundary.** The
[render contract](../../specs/cli.md#validation-preflight-and-rendering)
requires sensitive output ownership by the invoking account (500–504).
`render` is privileged through
[command registration](../../internal/cli/commands_nativeartifacts.go)
and [catalog.go](../../internal/cli/catalog.go). Both context-free and
context-backed render invocations were confirmed to require root.

Production [wiring.go](../../cmd/bootwright/wiring.go) supplies
`renderlocal.Files{}` directly. Its
[files.go](../../internal/nativeartifacts/renderlocal/files.go)
creates output under current credentials (58) and checks `os.Geteuid()`
(129–135), with no invoking-account helper. Under elevation, a caller-owned
private directory is rejected while new private output belongs to root,
preventing ordinary caller access.

Fixed filenames, held-root/atomic publication and regular-file checks narrow
the impact; no arbitrary code execution was demonstrated. Direct-root use
also does not exhibit the identity mismatch. Route publication through the
invoking-account boundary while retaining those path checks, and test both
caller-owned and root-owned targets. Cross-reference:
[CLI-01](report-claude.md#cli-01).

### C05 Some native executables bypass required qualification

**P2; conditional executable-integrity boundary.** The
[attacker model](../../specs/security.md#the-controller-hosts-attacker-model)
and [process boundary](../../specs/security.md#process-boundary) require
verified executable ownership/ancestry and a fixed safe working directory.
[getent resolution](../../internal/containercluster/preflightlocal/resolver.go)
(lines 26–28) and
[firewall execution](../../internal/controller/firewalllocal/native.go)
(lines 24–27) lack the qualification used by other native tools and leave
working directory inheritance. The SSH qualifier in
[launcher_linux_amd64.go](../../internal/machine/sshlocal/launcher_linux_amd64.go)
(lines 395–409) checks an absolute path, regular file and executable bits,
but not writable ownership/ancestry.

A pure qualifier probe accepted a synthetic mode-0777 executable; it did not
execute the file. Standard root-owned installation mitigates the trigger,
but does not implement refusal when installation is unsafe. Reuse a shared
qualification boundary, fix child working directories, and test unsafe file
and ancestor ownership/modes before any launch. No compromised installed
binary or privilege escalation was observed. Cross-reference:
[SEC-01](report-claude.md#sec-01).

### C06 The adapter retains system plugin search paths

**P2; ambient configuration/code boundary.** The
[process contract](../../specs/security.md#process-boundary) says ambient
plugins are not authority. Inspection of pinned ansible-core 2.21.5 with
shipped [ansible.cfg](../../ansible/ansible.cfg), isolated Python and a
synthetic home still returned `/usr/share/ansible/plugins/vars`, `action`
and `lookup` in the corresponding effective search paths. The
[adapter process environment](../../internal/adapterprotocol/process_linux_amd64.go)
(lines 104–110) does not remove those defaults.

The pinned vars-plugin loader enumerates and imports these plugins.
Therefore the automation closure can acquire installed system plugin code
outside the embedded collection and locked Python packages. No system plugin
was planted and no live adapter was started. The usual root-controlled
system directory limits the threat; this is not demonstrated unprivileged
escalation. Explicitly constrain plugin paths and test the effective loader
configuration using the pinned runtime. Cross-reference:
[PRJ-03](report-claude.md#prj-03).

### C07 Controller SSH policy rejects a disabling directive

**P2; conditional supported-policy incompatibility.** The controller must
retain the reduced host policy under the
[remote-process security contract](../../specs/security.md#network-remote-systems-and-privilege)
(lines 595–601). The allowlist in
[ssh_key_policy.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ssh_key_policy.py)
(lines 22–27, 61–75) rejects `GSSAPIKeyExchange no`;
[ceph_ssh_policy.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_ssh_policy.py)
turns that into `ssh-policy-unavailable`.

An import-only probe accepted `Ciphers aes128-ctr`, then refused the same
policy with the disabling line added. The tunnel's reducer explicitly accepts
that fixed `no` form in
[ceph_tunnel.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_tunnel.py)
(lines 83–84). This inconsistency blocks consumers of the controller parser
when the host policy contains that line. It does not establish that every
qualified FIPS controller produces it.

Admit only the fixed disabling form, retain the reduced policy, and refuse
enabling/unknown variants. Test both controller and placement consumers;
keep actual host-policy prevalence a qualification question. Cross-reference:
[SEC-07](report-claude.md#sec-07).

### C08 A pending upgrade plan invalidates a healthy pinned add-on

**P2; fail-closed readiness/continuation defect.** Under
[OLM installation](../../specs/add-ons.md#olm-installation), a pinned healthy
installation and a newer unapproved upgrade need distinct treatment
(lines 340–343). This audit reads the nearby rule that any other plan fails as proof of the
requested installation, alongside the explicit healthy-installed rule. That
interaction should be clarified rather than treating either clause in isolation.
The engine's plan gate in
[cluster_objects_engine.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_engine.py)
(265–282, 352–375, 454–458) handles a pending upgrade as a refusal for
exact policy before establishing
that the pinned installed CSV already succeeded.

A synthetic real-engine execution supplied owned/equal Subscription state,
a Succeeded pinned CSV and a newer pending InstallPlan. Observe returned
unknown; apply refused; neither wrote anything. Observation **did** set
`upgradePending=True`. Thus the installed pinned version can be healthy while
resolution or observed readiness fails. This is not proof of a failed initial
installation or of an unauthorized upgrade.

Separate initial-install approval, installed-CSV readiness and newer-upgrade
reporting. Keep the refusal to approve or follow an unrequested version.
Test exact policy with a healthy pinned CSV plus newer pending plan for
observe, continued apply and first-install control cases. Cross-reference:
[ADD-01](report-claude.md#add-01).

### C09 Cluster-object projection is not JSON-safe

**P2; admitted objects cannot be read back or recovered.** The
[cluster-object program](../../specs/add-ons.md#the-cluster-object-program)
admits canonical JSON bodies, and the
[adapter](../../specs/add-ons.md#the-cluster-object-adapter) must compare
declared fields and recover interrupted creates. The `JSON_TEMPLATE` in
[cluster_objects_native.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_native.py)
uses Go `printf "%q"` for keys and strings (22, 25).

That is Go string quoting, not JSON quoting: BEL, VT and ESC produce `\a`,
`\v` and `\x1b`, which Python JSON decoding rejects. Separate overlay probes
proved that an authored custom resource with ESC passes admission, rendering
and program compilation, and that the exact production template emits
invalid JSON for all three characters.

A create can persist successfully while both its response and subsequent
reads are undecodable, preventing a receipt and ordinary continuation.
Bounded decoding correctly fails closed, and UID/mark guards remain present;
they cannot repair invalid serialization. Use JSON-compatible encoding of
the selected subtree without broadening reads to whole objects. Add full
control-character round trips and lost-create-response recovery coverage.

### C10 Bucket names fail only at the native worker

**P2; admission/planning versus execution bound.** The resource contract
permits 3–63 characters, but the native bucket worker accepts only 44.
[object.go](../../internal/storage/cephcluster/object.go) (188–192) and its
[63-character acceptance test](../../internal/storage/cephcluster/object_bucket_test.go)
(35–40) preserve the wider range.
[object_bucket.go](../../internal/storage/ceph/object_bucket.go) (28–51)
hashes the block identity but leaves the bucket name unchanged;
[ceph_native_object.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_object.py)
(98, 286) enforces the narrower maximum. A 45–63-character
`CephObjectBucket` is therefore accepted and planned before an opaque request
refusal at execution. This violates the truthful admission/realization
boundary in the [API contract](../../specs/api.md#compiler-boundary) and the
[bucket resource contract](../../specs/ceph-resources.md).

The [kind page](../../specs/api/ceph.md) (428) still says 3–44, while
[the resource owner](../../specs/ceph-resources.md) (1626) says 3–63.
Resolve that contract inconsistency as well as the code mismatch. The worker's
bound is a genuine guard, not evidence that the admitted name is executable.
Align the public or planned bound with the complete
native naming scheme, provide a field-specific remedy, and test 44, 45 and
63 characters through admission, planning and worker decoding. This finding
is fail-closed; no wrong bucket mutation was demonstrated. This final
cross-check traced the predicates and existing tests; it did not run a native
worker or an end-to-end bucket plan.
Cross-reference: [CRB-07](report-claude.md#crb-07).

### C11 Authenticated NVMe-oF bindings are refused by planning

**P2; mutually incompatible validation and planner requirements.** The
NVMe-oF auth shape requires five mTLS bindings when `enable_auth` is true,
but the planner's service-binding allowlist excludes `nvmeof`.
[native_bindings.go](../../internal/storage/cephcluster/native_bindings.go)
(32–36) requires the five bindings;
[requests.go](../../internal/storage/ceph/requests.go) (374–378) requires
`gatewayExecuted` or `unitBinding`.
[services.go](../../internal/storage/ceph/services.go) (198–214) and
[service_unit.go](../../internal/storage/ceph/service_unit.go) (445–457)
exclude `nvmeof` from both branches, despite the preceding `nvmeofLeaves`
helper defining its intended paths (426–442). A declaration
that supplies the bindings demanded by admission consequently refuses as an
unrealized service binding. See the [native API](../../specs/ceph-native-api.md)
and [resource contracts](../../specs/ceph-resources.md).

This is not a reason to remove TLS checks or silently ignore bindings. Make
the admitted auth mode reach the intended frozen service/worker inputs, or
explicitly describe and diagnose its non-realization at the required earlier
boundary. Add a complete authenticated service fixture proving either
execution request construction or the documented early refusal. The older
restrictive realization list in
[the kind page](../../specs/api/ceph.md) (119–123) also needs reconciliation
with the native mTLS contract (982–990). Auth-disabled/managed-certificate
mode is unaffected. The final check proved the contradictory predicates
from source; it did not run a complete plan or contact a native service.
Cross-reference: [CEA-08](report-claude.md#cea-08).

### C12 Transient placed-Secret observation is not retried

**P2; premature failure after uncertain placement.** The add-on
[owned-Secret contract](../../specs/add-ons.md#owned-secrets) makes placement
subject to the generic adapter convergence rules: the unknown-read and
transient-create retry clause (286–295) applies through the statement that
placement creates as any owned object (407–413).
The placement path in
[cluster_objects_engine.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_engine.py)
(127–156) returns an unknown private reading directly to an unconditional
refusal, without the bounded pause/retry used by ordinary object convergence.
[cluster_objects_secrets.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/cluster_objects_secrets.py)
(67–94) reports initial private-read failure or an unproved post-create read
as not accepted. The initial public probe does retry; this finding concerns
the later module-private path only.

A transient create/post-create read failure therefore ends the attempt with
an unproved-placement difference rather than retrying to its deadline.
Confidential bytes still stay out of returned evidence; the finding is about
failure classification and recovery, not disclosure. Preserve the distinction
between unknown and proved unequal material, retry only the transient class
within the existing deadline, and test eventual success, deadline expiry and
proved mismatch independently. Cross-reference:
[ADD-11](report-claude.md#add-11).

### C13 Image admission accepts unqualified references

**P2; public grammar mismatch.** The
[image grammar](../../specs/api.md#graph-validation) (596–603) requires a
registry and repository before a tag or digest.
[lexical.go](../../api/v1alpha1/lexical.go) (332–350, 361–371) reuses
`validRegistry`, which permits an empty path. Consequently `squid:6.10`,
`registry.example.test:5000` and `squid@sha256:<64 hex>` pass the image rule.
A failing regression reproduced all three; a qualified reference remained
accepted.

This can defer failure to a native pull or let native short-name policy
supply an unrequested resolution. Those downstream outcomes depend on the
consumer configuration and were not executed here. Existing digest/tag and
`latest` checks do not supply the missing repository requirement. Require a
repository in both branches while preserving bare-host registry inputs, and
exercise every image-shaped API field through its shared lexical rule.
Cross-reference: [API-01](report-claude.md#api-01).

### C14 HTTP listeners inherit forbidden TLS defaults

**P2; valid explicit mode becomes a false refusal.**
[Kind defaults](../../specs/api/environment.md#kind-defaults) (305–309)
require HTTP to suppress inherited TLS while retaining errors for authored
forbidden fields. ArtifactServer's
[schema suppressions](../../api/v1alpha1/infrastructure_services.go)
(lines 52–60) account for management mode but not an HTTP-only listener set.
[inherit](../../internal/desiredstate/compilation/defaults.go) (44–55)
therefore copies the TLS default, and
[validateTransport](../../internal/infrastructureservices/admission.go)
(490–491) rejects it.

A pure regression constructed an authored HTTP-only listener and a TLS kind
default. It both retained `tls` and emitted the downstream forbidden-TLS
issue. The operator cannot override it with an authored TLS value; the
workaround is to remove the kind default and repeat it on HTTPS servers.
Suppress only inherited TLS for an effective HTTP-only set, preserving
omitted-listener HTTPS defaults and refusal of explicitly authored TLS on
HTTP-only recipients. Cross-reference: [ENV-06](report-claude.md#env-06).

### C15 Operation results omit required block facts

**P2; partially tracked output-contract defect.** The
[structured operation contract](../../specs/cli/output.md#json-output)
(lines 301–303) requires `outcome`, `attempts` and unresolved-block details.
[blockResults](../../internal/reconciliation/lifecycle/attempts.go)
(lines 724–733) supplies only ID, description, stage and state. The
[existing apply golden](../../internal/cli/testdata/cli-apply-warned-json.golden)
contains done blocks with empty outcome and zero attempts; the structured
writer serializes those defaults. Human result rows also omit the required
outcome. `status` separately populates attempts and unresolved details.

[B122](../../specs/milestones/backlog.md#b122) already records missing attempt
population, although its description also says the JSON commands were left
out. It does not settle the remaining outcome/unresolved discrepancies.
Do not count the entire problem as newly discovered. Populate operation
results from the same attempt/evidence model as status, reconcile B122 and
its specification qualification, and test retried and unknown blocks in
actual result construction as well as output goldens. Cross-reference:
[OUT-01](report-claude.md#out-01).

### C16 Secret version ordinals can restart

**P3; public version-history error.** The
[secret-store contract](../../specs/secrets.md) (517–527) requires the next
ordinal to exceed the highest one the Secret has ever reached.
[nextSequence](../../internal/secrets/localkeyring/mutations.go)
(lines 755–762) scans retained versions only. Deletion (125–136) and
collection (643–663) can discard every version or the highest one, with no
persistent per-Secret high-water mark.

A real contextfs/localkeyring test using synthetic material published v1 and
v2, deleted the unbound Secret, then recreated it: the result was v1, not
v3. The new durable random ID was distinct. Thus bindings and cryptographic
identity were not substituted; the public ordinal/history contract failed.
The existing sequence test keeps the maximum and misses this trigger.
Persist an authenticated bounded high-water mark independently of retained
versions, respecting store-version compatibility. Test both deleting all
versions and deleting the maximum while a lower bound version survives.

### C17 Canceled cluster info leaves borrowed material uncleared

**P3; best-effort memory cleanup omitted.** The
[object-user export contract](../../specs/cli.md#storage-dashboard-access)
(lines 1398–1401) explicitly requires clearing borrowed credentials on
failure and after rendering.
[commandResult.clearSensitive](../../internal/cli/dispatch.go) (73–83)
omits `clusterInfo`; its cleanup is only in
[writeClusterResult](../../internal/cli/output_cluster.go) (370–371).
[runner.execute](../../internal/cli/runner.go) can detect cancellation and
return before entering that renderer.

The synthetic CLI probe canceled in `FinishProgress`, after the service
returned a valid object-user result and before presentation. Exit status was
1, neither stream disclosed the value, but seven synthetic secret bytes
remained uncleared. An earlier dashboard-only probe showed the same path;
the object-user probe supplies the direct contract match. This is not an
output leak or proof of practical memory extraction. Clear `clusterInfo`
through unconditional result cleanup and test cancellation, service failure,
invalid presentation and successful output.

### C18 Malformed sudo default lists lose the unknown result

**P3; fail-open interpretation of malformed policy.** The
[secret input boundary](../../specs/secrets.md#acquisition-and-commands)
requires proving stdin logging absent before lending a secret through sudo.
[sudo.go](../../internal/controller/privilege/sudo.go) skips a defaults list
when `defaultFields` rejects its quoting/escaping (297–304, 340–368). The
caller then interprets an empty option result as no logging (148–153).
The existing malformed-list case in
[input_logging_test.go](../../internal/controller/privilege/input_logging_test.go)
expects that behavior even when the malformed input contains `log_input`.

The parser therefore loses the difference between proved absence and an
uninterpretable answer. This is source-confirmed; the audit did **not** prove
that a qualified sudo emits that malformed form while logging is enabled,
or that any secret was logged. Propagate validity, refuse secret-stdin
elevation with `InputUnproven` on malformed policy, and test invalid versus
valid-negated lists separately. Cross-reference:
[SCR-05](report-claude.md#scr-05).

## Correction sequence

These are bounded proposals, not newly activated slices. Milestones remain
the delivery authority; use existing carriers where they cover the work and
record new bounded items in the earliest owning milestone when scheduling
implementation. C15 should update B122 rather than duplicate its attempt
population work.

| Work package | Findings | Owner / suggested carrier | Exit evidence |
| --- | --- | --- | --- |
| Physical and private-publication safety | C01, C02 | Physical/container installation and managed-OS owners; M1/M3 | Resolve selector contract conflict; refuse unstable physical hints; prove private modes throughout publication |
| Correct completion proof | C03 | KubeVirt substrate; M1/B87 follow-on | Pending/Failed roots never complete; observe/apply share frozen-shape proof; removal still works safely |
| Invocation and process trust | C04–C07, C18 | Controller, CLI and adapter owners; M1 | Invoker-owned outputs, executable/ancestor checks, closed effective plugin paths, supported disabling policy, malformed sudo refusal |
| Add-on convergence | C08, C09, C12 | Add-ons and cluster objects; M4 | Pinned-CSV upgrade case, exact JSON round trips, unknown placed-Secret retry and deadline proofs |
| Ceph truthful execution | C10, C11 | Storage; M2 | Boundary name lengths and a complete authenticated NVMe-oF request journey |
| Admission and output consistency | C13–C15 | API/compiler/CLI; M1, existing B122 where applicable | Qualified images, mode-aware defaults, populated attempt/outcome/unresolved results |
| Custody bookkeeping and cleanup | C16, C17 | Secret store and inspection; M1 | Durable ordinal high-water compatibility, unconditional sensitive-result cleanup |

Every safety or durable-format correction needs the repository's independent
review, contract/digest decision where applicable, focused regressions and
required race/full gates. A test that reproduces these failures should fail
before the fix. Native qualification remains separate from a passing fake.

## Verification and limits

### Baselines and interrupted work

The initial three domain reviews and synthetic probes assessed `f8b15669f`.
A separate documentation integration advanced the checkout to `998191ad2`
without changing product sources. The final worktree was advanced to
`88fbf7be1` before completion. The build-only delta was separately reviewed.
No unrelated checkout, branch or user content was modified by this audit.

An initial `make check` hit read-only default Go cache/temp paths. Its retry
used writable temporary paths, but the initial task worktree was removed by
external activity before a final result was obtained. Those attempts were
stopped and are **not passes**. A further gate was in flight when the session
was interrupted; its scratch log was unavailable on resume, so no result is
claimed for it. The completion gate below is a fresh run on the completion
baseline. These environment interruptions are not product defects.

A domain reviewer did complete the broad reconciliation/workspace/secrets/
trust/adapter suite successfully during the first phase. A later redundant
run with temporary build paths failed the executable-qualification tests
`TestReexecutionPathPinsRunningExecutable` and
`TestReexecutionAcceptsTheInvokersOwnBinary`; the cause was not conclusively
diagnosed. Neither that failure nor the initial pass is substituted for the
completion gate. The report retains the distinction.

### Container-build delta

The completion review inspected [Dockerfile](../../Dockerfile),
[Makefile](../../Makefile), [.dockerignore](../../.dockerignore) and the
[container-build tests](../../test/architecture/container_build_test.go)
added by `0811da0e1` and corrected by `88fbf7be1`. The latter replaces the
historical proxy/mirror build arguments with a temporary secret mount,
removes that local secret on exit/signals, and reads it as quoted exported
values rather than sourcing shell code. The final output stage contains the
executable. No additional current defect was established in that change.

Both existing container-build architecture tests passed with module downloads
disabled and writable temporary/cache paths:

```sh
GOPROXY=off GOTMPDIR="$AUDIT_TMP" GOCACHE="$AUDIT_CACHE" \
  ./scripts/go test ./test/architecture -run '^TestContainerBuild' -count=1
```

An earlier attempt with `GOSUMDB=off` failed toolchain verification, and an
attempt using the default temporary location hit sandbox permissions; neither
was counted as a pass. These tests are structural checks. No Docker/Podman
build or engine-history proof was run in this audit. `.dockerignore` excludes
specific conventional paths; it is not a general interpreter of Git ignore
rules, and a secret mount does not protect arbitrary files included in a
build context. No particular leaked context file was demonstrated here.

### Completion gate record

All completion commands used the pinned wrappers on `88fbf7be1` plus this
audit-only documentation diff. `make check` completed with exit 0. The Go
suite includes reused package-test results, as Go reports; the Ansible gate
executed all six default lanes afresh, rather than reusing a gate pass.

| Command / check | Result | Qualification |
| --- | --- | --- |
| `make check` | PASS, exit 0 | Formatting, both module checks/tidy checks, ordinary Go tests, both vet targets, completion, vulnerability analysis, Ansible lanes and diff check |
| `make docs-check` | PASS, exit 0 | Guidance links, paths, command/spec tables; final report receives an additional link/count check |
| `make race` | PASS, exit 0 | Scope is exactly the Makefile target; contextfs and secret-store packages are not separately race-tested by that target |
| `./scripts/govulncheck -show verbose -db https://vuln.go.dev ./...` | PASS, exit 0 | No vulnerable imported package/called symbol; one module-only advisory below |
| Container-build architecture tests | PASS | Both existing tests; offline module route, no container build |
| Independent report checks | PASS | Finding counts, source paths and Markdown links/anchors; substantive review of consequential findings |
| Live/e2e/operator acceptance | NOT RUN | No authority inferred from an audit request |

The Ansible plain lane reported **8,725 passed, 1 skipped**. The recorded
skip is `test_capacity_publishes_where_a_held_listing_is_stale` in
[test_controller_files.py](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_controller_files.py)
(line 289): the test's temporary filesystem did not exhibit stale held-directory
listing. Its separate synthetic held-listing test remains in the suite; the
skipped filesystem-specific case is not a pass. Other unit
phases cover overlapping controller/floor subsets; their counts must not be
added as unique tests. Syntax, lint, sanity, units, integration and plain
lanes all passed. The log contains host-pattern warnings during syntax
checking; those are not live target contact. The successful nonverbose Go
package log does not establish how many individual Go tests skipped.

Verbose vulnerability output identified module-only
[GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) in
`golang.org/x/crypto@v0.58.0`, concerning `openpgp`. The scan reported zero
affected imported packages and zero affected called symbols. This is a
retained dependency observation, not a demonstrated reachable Bootwright
vulnerability or a complete security clearance. No dependency was changed.

Retained completion-gate excerpt:

```text
================= 8725 passed, 1 skipped in 386.91s (0:06:26) ==================
===== end of lane plain =====
lane syntax: passed in 22 s
lane lint: passed in 3 s
lane sanity: passed in 234 s
lane units: passed in 204 s
lane integration: passed in 15 s
lane plain: passed in 390 s
git diff --check
```

### What the gates do and do not establish

`make check` includes Go formatting, module verification and tidy checks for
both modules, all ordinary Go packages, native and darwin/arm64 vet, fresh
completion tests, vulnerability analysis, the Ansible gate and a final diff
check. The Ansible gate covers syntax, lint, sanity, unit/harness tests,
Python-floor checks, synthetic integration and a separate serial plain test
loop. A printed reuse of its unchanged-input cache is a reused pass, not a
fresh execution. `make race` is separate.

None of those results establishes real-machine installation, native SDK/API
behavior, real-host privilege configuration, destructive-disk correctness or
operator acceptance. The default Ansible gate excludes the operator native
transaction and controller prerequisite targets. Fresh controller setup,
network runtime-lock verification, non-Bash completion where not opted in,
container-engine builds, and live release/provider matrices require separate
evidence. Nonverbose Go package success does not disclose the count of
skipped tests. Source mentions of skips are not actual skip counts.

### Other observations, not additional confirmed defect counts

The role-owned scratch layout is not uniform:
`containercluster_install_agent/tasks/boot.yml` writes a private URL in the
runner scratch root, and `ceph_host_runtime.py` creates private temporary
probe storage outside the role child. These differ from the stated location
contract. Per-invocation directories, private modes and cleanup remain, so
this audit established neither exposure nor deletion across invocations.
Treat location reconciliation as a separate conformance review, not a proved
security incident. Compare [RCP-11](report-claude.md#rcp-11).

The cluster-object native retry code recognizes structured JSON Status
errors. Whether the exact selected `oc` emits those structures for all
intended retryable errors was not proved; a real pinned-client/mock-server
qualification is needed. That candidate is not promoted to a finding.
The hundreds of other entries in the Claude report were not individually
adjudicated by this review. No conclusion about their validity follows from
the smaller count here.

## Retained reproduction evidence

The probes were temporary overlays, isolated copies or pure imported helper
calls. None was installed as a production test or run against real resources.
The following excerpts preserve observed results; commands using an overlay
require recreating that probe from the described fixture/trigger. An
intentional regression failure below proves the discrepancy; it is not a
failure of the unmodified repository gate.

### KubeVirt proof probe

In an isolated copy, append tests to the existing
`tests/unit/plugins/module_utils/test_kubevirt_machine.py` fixture: keep the
root Pending during creation; set an existing root Pending for observation;
change the existing VM's CPU cores from 4 to 99 for observation. Use the
fixture clock so expiry is synthetic, and assert `postcondition` is false.
Run the pinned wrapper on that file:

```sh
./scripts/ansible-check --suite units \
  ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_kubevirt_machine.py
```

Observed result on the product baseline: **3 failed, 26 passed**. The failures
were the new readiness/shape assertions, not the existing suite.

### API, defaults and canceled object-user probes

The image probe calls `ValidLexical("image", value)` for the three unqualified
forms and expects refusal, with a qualified-reference positive control.
The inheritance probe calls `inherit` with ArtifactServer's actual schema,
an authored HTTP-only listener and a TLS fallback, then calls
`infrastructureservices.ValidateAuthored`. The CLI probe uses the existing
valid Ceph/object-user fixture, supplies `FinishProgress: cancel`, runs
`cluster info --name storage --object-user documents --output json` through
an in-memory service double, and checks both empty confidential output and
zeroed returned material.

```sh
./scripts/go test -overlay="$PROBE_OVERLAY" \
  ./api/v1alpha1 ./internal/desiredstate/compilation ./internal/cli \
  -run '^TestAudit003' -count=1
```

Retained failure excerpts:

```text
unqualified image admitted: squid:6.10
unqualified image admitted: registry.example.test:5000
unqualified image admitted: squid@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
HTTP-only recipient inherited forbidden TLS
downstream refusal: HTTP-only artifact listeners forbid TLS configuration
canceled object-user inspection retains 7 non-cleared confidential bytes
```

The CLI probe additionally asserted exit 1 and that neither stream contained
the synthetic secret; those assertions passed.

### Secret ordinal and executable qualification probes

Use the existing real contextfs/keyring harness to put v1, replace with v2,
delete with no bindings and put again. Assert sequence 3 and distinct ID.
Use the SSH package's qualifier on a synthetic regular mode-0777 executable,
without executing it, and assert refusal.

```sh
./scripts/go test -overlay="$PROBE_OVERLAY" ./internal/secrets/localkeyring \
  -run '^TestAudit003OrdinalAfterDelete$' -count=1
./scripts/go test -overlay="$PROBE_OVERLAY" ./internal/machine/sshlocal \
  -run '^TestAudit003WritableSSHExecutable$' -count=1
```

Observed: ordinal **1 instead of 3**, with a new durable ID; unsafe SSH
executable **accepted**. Each contract assertion failed.

### Projection, policy, plugin and private-file probes

- Authored custom-resource admission, rendering and `CompilePhase` accepted
  ESC. Running the exact production Go template over BEL/VT/ESC values
  produced invalid JSON. Both evidence-checking overlay tests passed because
  they asserted these current behaviors, not the desired repaired behavior.
- Importing the controller policy parser accepted `Ciphers aes128-ctr` and
  refused it with `GSSAPIKeyExchange no` appended. No client was launched.
- Effective pinned Ansible paths included the system `vars`, `action` and
  `lookup` plugin directories under isolated configuration. No plugin was
  planted.
- A pinned Ansible `atomic_move` over synthetic bytes with umask `022`
  yielded mode `0644` before requested attributes were applied; the
  no-other-permission assertion failed. It was a deterministic primitive
  test, not a competing-user exploit.
- A pure physical-catalog `Refusals` test expecting rejection of sole
  `/dev/sda` failed. A pure invocation classification test confirmed both
  render forms require root. Neither ran installation or publication.

### Broad initial package suite

The domain reviewer recorded exit 0 for:

```sh
./scripts/go test ./internal/adapterprotocol/... \
  ./internal/controller/ansiblelocal/... ./internal/controller/privilege/... \
  ./internal/trust/... ./internal/reconciliation/... \
  ./internal/workspace/... ./internal/secrets/...
```

That is an initial-baseline observation, not a replacement for the completion
run or the explicit limitations above.
