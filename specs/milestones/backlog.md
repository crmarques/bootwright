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
| [B161](#b161) | new, 2026-09-30 (X28) | enabling | Architecture | The walk guard follows every write | Parked under the 2026-09-30 triage rule: test tooling |
| [B162](#b162) | new, 2026-09-30 (X28) | enabling | Architecture | The diagnostic registry follows every call shape | Parked under the 2026-09-30 triage rule: test tooling |
| [B163](#b163) | new, 2026-09-30 (X28) | enabling | Architecture | Test-loop wording and counts | Parked under the 2026-09-30 triage rule: tooling polish |
| [B164](#b164) | new, 2026-09-30 (X28) | enabling | Architecture | Only the privilege supervisor subscribes to signals | Parked under the 2026-09-30 triage rule: test tooling |
| [B122](#b122) | new, 2026-09-29 (X22); parked 2026-09-30 by D43 | enabling | CLI | JSON results for plan, apply and destroy | No milestone names a consumer yet (D43) |
| [B165](#b165) | split from B44 on 2026-09-30 (D40) | enabling | Controller setup and Workspace | The automation projection as a layer over a shared foundation | B44 fixes only the remaining limit (D40) |
| [B166](#b166) | split from B45 on 2026-09-30 (D41) | safety | State reconciliation | Scoped removal | A state-contract candidate (D41) |
| [B167](#b167) | split from B34 on 2026-09-30 (D28) | safety | Substrate and Controller | Frozen remote package transactions | The SSH-host install contract was narrowed instead (D28) |
| [B171](#b171) | new, 2026-09-30 (X21) | enabling | State reconciliation and Secrets | Custody checks each held by its own test | Parked under the 2026-09-30 triage rule: test depth |
| [B179](#b179) | new, 2026-09-30 (X30) | enabling | CLI and Controller setup | A refused setup names the remedy its refusal gives | Parked under the 2026-09-30 triage rule: wording |
| [B180](#b180) | new, 2026-09-30 (X30) | enabling | Workspace, Controller setup and State reconciliation | Storage fixtures that can fail | Parked under the 2026-09-30 triage rule: test depth |
| [B181](#b181) | new, 2026-09-30 (X30) | safety | Secrets and State reconciliation | A bounded run's rebind names its consumer | Bounded at three bindings since X26; closing it changes the keyring format |
| [B193](#b193) | new, 2026-09-30 (X29) | enabling | Container cluster | A removal observation resolves only the tools it runs | Parked under the 2026-09-30 triage rule: efficiency |
| [B194](#b194) | new, 2026-09-30 (X29) | enabling | State reconciliation | A local placement asks for no SSH material | Parked under the 2026-09-30 triage rule: tidiness |
| [B195](#b195) | new, 2026-09-30 (X29) | enabling | Container cluster | Additional trust for the cluster-wide trusted CA | Needs an owner decision; no milestone asks for it |
| [B196](#b196) | new, 2026-09-30 (X29) | enabling | Architecture | Two lessons for the knowledge pages | Parked under the 2026-09-30 triage rule: knowledge |
| [B197](#b197) | new, 2026-09-30 (X29) | enabling | Architecture, with each test's owner | Tests X29 left narrower than they read | Parked under the 2026-09-30 triage rule: test depth |
| [B198](#b198) | new, 2026-09-30 (X29) | enabling | Infrastructure services | IPv6 listener impacts print bracketed | Parked under the 2026-09-30 triage rule: display |
| [B199](#b199) | new, 2026-09-30 (X29) | enabling | CLI | Completion over word breaks and truncated sets | Needs an owner decision on what the operator sees |
| [B200](#b200) | new, 2026-09-30 (X29) | enabling | Substrate | Autostart replays report what they change | Parked under the 2026-09-30 triage rule: replay fidelity |
| [B201](#b201) | new, 2026-09-30 (X29) | enabling | State reconciliation and Workspace | Remediations that match the next command | Parked under the 2026-09-30 triage rule: wording |
| [B202](#b202) | new, 2026-09-30 (X29) | enabling | Controller setup | Resolution warnings in the plan and on failure | Needs an owner decision on the plan |
| [B222](#b222) | new, 2026-09-30 (X31) | enabling | State reconciliation and CLI | Status next steps over delete exits and lost bindings | Parked under the 2026-09-30 triage rule; one part needs an owner decision |
| [B223](#b223) | new, 2026-09-30 (X31) | enabling | State reconciliation, with each capability's owner | Unknown-block reasons for every capability | Parked under the 2026-09-30 triage rule |
| [B224](#b224) | new, 2026-09-30 (X31) | enabling | Each spec's owner | Wording X31 left | Parked under the 2026-09-30 triage rule: wording |
| [B225](#b225) | new, 2026-09-30 (X31) | enabling | Workspace | Operation-area capacity scans and shared bounds | Parked under the 2026-09-30 triage rule: performance |
| [B226](#b226) | new, 2026-09-30 (X31) | enabling | State reconciliation | Status over a revision that no longer compiles | Parked under the 2026-09-30 triage rule |
| [B227](#b227) | new, 2026-09-30 (X31) | enabling | State reconciliation | A continuation over a reprojected bundle refuses first | Parked under the 2026-09-30 triage rule: no effect runs |
| [B228](#b228) | new, 2026-09-30 (X31) | enabling | Controller setup | Setup runs: harness, remedy, guide and size | Parked under the 2026-09-30 triage rule; the size needs an owner decision |
| [B229](#b229) | new, 2026-09-30 (X31) | enabling | Architecture | Knowledge lessons X31 left | Parked under the 2026-09-30 triage rule: knowledge |
| [B230](#b230) | new, 2026-09-30 (X31) | enabling | Each test's owner | Tests X31 left narrower than they read | Parked under the 2026-09-30 triage rule: test depth |
| [B231](#b231) | new, 2026-09-30 (X31) | enabling | Controller setup | A completed purge also retires the kept bundle's superseded resolutions | Parked under the 2026-09-30 triage rule |
| [B232](#b232) | new, 2026-09-30 (X31) | safety | State reconciliation and Secrets | Each block's execution holds only its own parts | The key stays in the operation's memory while the server shares the operation; needs a port |
| [B233](#b233) | new, 2026-09-30 (X31) | enabling | Each capability spec's owner | Refusal tables X31 left | Parked under the 2026-09-30 triage rule |
| [B234](#b234) | new, 2026-09-30 (X31) | enabling | Architecture, with each role's owner | Collection rules X31 left | Parked under the 2026-09-30 triage rule |
| [B235](#b235) | new, 2026-09-30 (X31) | enabling | CLI and Desired state | Refusals for objects that are not API objects | Parked under the 2026-09-30 triage rule |
| [B236](#b236) | new, 2026-09-30 (X32) | safety | State reconciliation and Workspace | Exits over unreadable or differently spelled evidence | Found after the M1 freeze (D48); one part needs an owner decision |
| [B237](#b237) | new, 2026-09-30 (X32) | defect | Controller | The controller stage completes only over a sealed client area | Found after the M1 freeze (D48) |
| [B238](#b238) | new, 2026-09-30 (X32) | defect | Controller setup | Setup counts controller-stage resolutions | Found after the M1 freeze (D48) |
| [B239](#b239) | new, 2026-09-30 (X32) | defect | Controller | A context keeps the `latest` client it proved | Found after the M1 freeze (D48) |
| [B240](#b240) | new, 2026-09-30 (X32) | enabling | Controller | The libvirt client rule in the specs | Found after the M1 freeze (D48) |
| [B241](#b241) | new, 2026-09-30 (X32) | defect | Controller | Stage and preflight failures name their own remedies | Found after the M1 freeze (D48) |
| [B242](#b242) | new, 2026-09-30 (X32) | defect | Controller and State reconciliation | An acknowledgement after a failed exit keeps that exit | Found after the M1 freeze (D48) |
| [B243](#b243) | new, 2026-09-30 (X32) | safety | State reconciliation and Workspace | Room for a replacing removal and its records | Found after the M1 freeze (D48) |
| [B244](#b244) | new, 2026-09-30 (X32) | safety | Architecture and State reconciliation | Request strings are never rendered as templates | Found after the M1 freeze (D48) |
| [B245](#b245) | new, 2026-09-30 (X32) | enabling | Architecture, with each role's owner | Censored completions that name only fields | Found after the M1 freeze (D48) |
| [B246](#b246) | new, 2026-09-30 (X32) | defect | Substrate | A drifted machine's apply resolution names its reason | Found after the M1 freeze (D48) |
| [B247](#b247) | new, 2026-09-30 (X32) | safety | Infrastructure services and Substrate | Socket claims of an SSH placement that is the controller | Found after the M1 freeze (D48); publishing SSH claims needs an owner decision |
| [B248](#b248) | new, 2026-09-30 (X32) | defect | Infrastructure services | The artifact server compares its start with its files | Found after the M1 freeze (D48) |
| [B249](#b249) | new, 2026-09-30 (X32) | defect | Managed OS and Infrastructure services | A Machine and a profile of one name share no served directory | Found after the M1 freeze (D48); the consumer directories need an owner decision |
| [B250](#b250) | new, 2026-09-30 (X32) | enabling | Container cluster and Substrate | Substrate refusals in the cluster's own words | Found after the M1 freeze (D48) |
| [B251](#b251) | new, 2026-09-30 (X32) | enabling | State reconciliation, with each role's owner | Every lifecycle refusal reaches the operator | Found after the M1 freeze (D48) |
| [B252](#b252) | new, 2026-09-30 (X32) | defect | State reconciliation, with each capability's owner | An old request refuses by naming its version | Found after the M1 freeze (D48) |
| [B255](#b255) | new, 2026-10-01 (X33) | enabling | Each test's owner | Mutants the X33 splits found surviving | Found after the M1 freeze (D48) |
| [B256](#b256) | new, 2026-10-01 (X33) | enabling | Architecture | Line-limit texts for empty lists | Found after the M1 freeze (D48) |
| [B257](#b257) | new, 2026-10-01 (X33) | defect | Controller | Controller-client refusals carry their remedy | Found after the M1 freeze (D48) |
| [B258](#b258) | split from B206 on 2026-10-01 (D52) | enabling | State reconciliation | Pruning retained operations | D52 names the existing exits; pruning needs a retention rule for evidence and audit |
| [B259](#b259) | new, 2026-10-01 (X34) | safety | Container cluster | A grown installer kubeconfig is restored too | Found after the M1 freeze (D48) |
| [B260](#b260) | new, 2026-10-01 (X34) | enabling | Container cluster | Qualify the registered-hosts read on a real install | Found after the M1 freeze (D48) |
| [B261](#b261) | new, 2026-10-01 (X34) | defect | Controller setup | A resumed carried-forward receipt reads its approved bytes | Found after the M1 freeze (D48) |
| [B262](#b262) | new, 2026-10-01 (X34) | safety | Controller setup | A pending receipt this executable cannot prepare refuses first | Found after the M1 freeze (D48) |
| [B263](#b263) | new, 2026-10-01 (X34) | enabling | Controller setup | The fresh-resolution fallback says why and reuses what it can | Found after the M1 freeze (D48); the display needs an owner decision |
| [B264](#b264) | new, 2026-10-01 (X34) | safety | Substrate | A network is this context's only when its metadata names this context | Found after the M1 freeze (D48) |
| [B265](#b265) | new, 2026-10-01 (X34) | safety | Substrate and Machine | No Machine starts while its network restarts | Found after the M1 freeze (D48) |
| [B266](#b266) | new, 2026-10-01 (X34) | enabling | Substrate | The drifted-network refusal names an exit that works | Found after the M1 freeze (D48) |
| [B267](#b267) | new, 2026-10-01 (X34) | enabling | State reconciliation | Exits at the retained-operation bound | Found after the M1 freeze (D48); one part needs an owner confirmation |

### B94

Delete the dead setup-binding and legacy prerequisites layer, and share one
retained-native matcher between preflight and the controller stage. **Exit
evidence:** preflight and stage tests over one closure.

Since X32 setup still plans a controller-binding action and appends a binding when a context is named, although its scope is always empty (found in X32).

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
completion extension. Its custody half is B10, which [X21](delivered.md#x21--bmc-trust-and-administrator-custody) delivered. **Exit evidence:**
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

### B161

The working-tree walk guard follows only assignment statements into package variables, so a range clause assigning with `=` and a write through a pointer are missed; and it still reports a walk over an in-memory filesystem reached through a field, a function result, `fs.Sub` or another package's variable (found in X28). **Exit evidence:** guard fixtures for each shape.

### B162

The diagnostic registry keeps only the last literal prefix of a parameter that flows into two stores, misses a code passed through a method expression, a code-carrying field keyed by another package, and a dot-imported function (found in X27 and X28). **Exit evidence:** registry fixture rows for each.

### B163

The Makefile's `quick` comment and the Go reference still describe `make quick` as the changed packages and their dependents; the plain collection loop's gate checks pytest's status but not that it collects the tests ansible-test runs; and a manual plain run leaves ignored `__pycache__` directories under the collection's tests. **Exit evidence:** the wording, and a collection-count check.

### B164

Nothing checks that `os/signal` is granted to the privilege supervisor alone, although the spec calls its `Begin` the process's one signal subscription; and the composition-root signal test X28 kept repeats what the boundary tests refuse. **Exit evidence:** a grant check and one test.

### B122

JSON results for `plan`, `apply` and `destroy`, which X22 left out (decision
X22-D1), carrying each block's attempts, which the operation result does not
fill today. **Exit evidence:** JSON goldens for each command.

### B165

An automation-only revision still names a whole new bundle area and republishes the same closure into it, because the projection carrying the automation is part of the bundle's identity rather than a layer over a shared foundation. **Exit evidence:** closed layer identities, a foundation reused only when its closure is unchanged, and bounded reacquisition.

### B166

`destroy` admits no stage or block scope, so a recovery removes every block the context owns rather than the one whose digest moved. **Exit evidence:** the authorization and journey of a scoped removal, proof that a scope never leaves a dependent behind, and crash and replay tests.

### B167

An SSH-host provider's hypervisor install frozen as one exact transaction under the controller stage's before-state rules. **Exit evidence:** a frozen-transaction test for the SSH arm.

### B171

Three of X21's custody mutations are caught only by defence in depth: removing the lent area's close, dropping the closed-transaction check, and withdrawing before the removal's record reads done. **Exit evidence:** one test that fails for each mutation alone.

### B179

After a refused setup, the result's Next line always says `bootwright setup`, while the bound refusal's diagnostic names `bootwright setup --purge-old-bundles` (found in X30). **Exit evidence:** a result golden whose Next line follows the refusal's remediation.

### B180

The contextfs retirement fixture sets retained definitions that `Publish` ignores, so its kept-resolution assertion always passes; the controller storage memory double keeps no receipt's resolution append-only and refuses no replaced one, so the storage suite cannot require that retirement drops a retired bundle's resolution; and the kill harness's workspace clone does not copy the journey double's client files (found in X30). **Exit evidence:** fixtures and doubles that fail when the rule breaks.

Since X31 the contextfs retirement fixture holds real resolutions, so its kept-resolution check is meaningful; the memory double also retires an area no resolution names, which the store refuses.

### B181

A bounded run's rebind is limited to three bindings rather than closed, which a binding that names its consumer would close; that is a keyring format change (left by B139 in X30). **Exit evidence:** a binding format that names its consumer, and a rebind test that refuses any other.

### B193

The agent-install removal observation still resolves the openshift-install and oc paths although it runs no oc read; the destroy that follows needs them (found in X29). **Exit evidence:** an observation that resolves only what it runs.

### B194

The lifecycle's material list adds the SSH identity and host-key files whenever a placement names their Secrets, whatever its connection, so a local placement carrying SSH references still asks for SSH material; since X29 the agent-install decoders refuse such a placement, other decoders do not (found in X29). **Exit evidence:** a material-list test over a local placement.

### B195

Since X29 additional trust bundles reach `install-config`, but `additionalTrustBundlePolicy` stays at the installer default `Proxyonly`, and Bootwright refuses an installation proxy, so the bundles never feed the cluster-wide trusted CA. Whether they should (`Always`) is undecided (found in X29). **Exit evidence:** the decision, with a projection golden.

### B196

ansible-core 2.21.4 keeps a Jinja string-literal escape such as `'\n'` as a backslash and an `n`, so a role that joins with it writes a literal backslash-n; and `renameat2` moves the renamed inode's change time on ext4, XFS, Btrfs and tmpfs, so a status proof across a rename must leave the change time out (both confirmed by probes in X29). **Exit evidence:** a knowledge page for each.

### B197

Two tests still put wwn hints on libvirt guests that admission now refuses; the media store contract suite has no missing-callback clause and its double would panic; the second-implementation secret store fake assigns no sequence; and two runner consume sites keep no test pinning their remediation (found in X29). **Exit evidence:** each test narrowed or added.

### B198

The managed service and artifact server plan impacts print an IPv6 listener unbracketed, as `open-listener fd00::1:3128`, where the emulated BMC's impact prints `[fd00::1]:8000` since X29. **Exit evidence:** impact goldens over IPv6.

### B199

Bash's default word breaks also contain `:`, so a path with `:` breaks completion of the rest of the word as `@` and `=` did, and PowerShell's `,` may belong in the withheld set (not verified on a real shell); and when the candidate cap or the read bound truncates a listing, the shell still inserts the longest common prefix of the partial set (found in X29). **Exit evidence:** the chosen sets and truncation behavior, with completion tests.

### B200

The libvirt host apply runs network and pool autostart on every replay reporting no change, and the observation never reads autostart, so an autostart switched off is re-enabled silently (found in X29). **Exit evidence:** a replay test that reports the change and an observation that reads autostart.

### B201

A failed destroy's remediation still says to repeat the operation to continue it, where the next destroy replaces it; status lists a failed destroy's lost block record as a contradiction beside an offered destroy that no record refuses; and a repeated media add whose adopted stage was rewritten promises a publication its next repeat refuses (found in X29). **Exit evidence:** remediation and status goldens for each.

### B202

Since X29 a newer Index API minor warns in setup's result, but the plan shown before confirmation does not show it, and a failure after a warning-bearing selection drops the warning from its report (found in X29). **Exit evidence:** plan and failure goldens that carry the warning.

### B222

Status offers no next step over the two states whose refusal names a delete; it cannot see a keyring whose listing a missing part file breaks, so it still offers apply and destroy there; and over a failed apply whose blocks are all done with a lost binding it offers only the delete, though `apply` would finalize it, which D37's wording chose (found in X31). **Exit evidence:** status goldens for each state.

### B223

Since X31 only the libvirt machine explains why a block stayed unknown; the libvirt host, bare-metal machine, managed services, artifact server, managed-OS installation, cluster install and controller clients give the general reason. Status also shows a null evidence's reason rather than the error an observation that could not run reported, and no command test deletes a context whose controller record holds reservations (found in X31). **Exit evidence:** a reason per capability and the command test.

### B224

The destroy row of the command catalog lists no housekeeping; the dependency-safety section still says destroy uses the exact frozen dependencies although a fresh destroy runs under the build in hand; the architecture spec could name the frozen execution closure; and comments still call a carry source a sealed area and describe cluster reads through the installer's kubeconfig (found in X31). **Exit evidence:** the texts corrected.

### B225

The operation area re-measures its whole subtree, re-verifying each directory's ancestors, before every record write and log append, about a third of a second per write at its entry bound on this host; and the runs and SSH-trust areas share its entry and byte bounds while the bounds table names only the operation area (found in X31). **Exit evidence:** an incremental measure with a bound test, and the table rows.

### B226

Status compiles the imported revision and refuses when it no longer compiles, so a context imported with a retired field, since X31 a Secret file source, loses status until `context update`, although its destroy still works (found in X31). **Exit evidence:** status over such a revision reporting its records.

### B227

A continuation run by its registering build over a bundle a later build reprojected passes the closure check and is refused only per attempt by the runner, after its log is restored and it is marked running; comparing the receipt's automation digest before the restore would refuse first (found in X31). **Exit evidence:** a continuation test that refuses before any record changes.

### B228

Setup runs are not driven by the contextfs checkpoint harness; a failure their output explains does not point its remedy at `run.output`; the operator guide does not mention them or that reading them needs root; their output keeps 8 MiB where a bounded run truncates at 4 MiB, although D42 names one limit; and the runner relies on each output writer swallowing its own errors (found in X31). **Exit evidence:** a harness scenario, the remedy, the guide text, one size, and a swallowing wrapper.

### B229

A knowledge page keyed on `SyntaxWarning: invalid decimal literal` for the ansible-check area name (B203), and the carry-forward page citing the test that carries a settled receipt forward (found in X31). **Exit evidence:** the pages.

X32 found the mechanism: ansible-core renders its home defaults through Jinja's native environment, whose concatenation parses the text, so any home path in which a number runs into a keyword warns in every process; CPython also warns for other number forms, such as `1_1if`, `1e1if` and `0x1fif`.

### B230

No test pins the pre-plan refusal of a pending receipt whose bundle is incompatible; the lifecycle test double's held area lets a removal skip its closed and read-only checks; and no runner test pins a partial record waiting at the drain after a failed exit, whose outcome X31 made unknown (found in X31). **Exit evidence:** each test.

### B231

After a completed setup, `--purge-old-bundles` retires superseded areas but leaves the superseded resolutions of the receipt's own bundle until a setup at the bound needs their room (found in X31). **Exit evidence:** a purge test that retires them.

### B232

Since X31 a run request lends a block only the Secret parts it writes, but each attempt's execution and quiescence probe still hand the capability's Go code the whole reopened operation map, the artifact server's key included (found in X31). **Exit evidence:** a port through which a capability declares its blocks' parts, with a test that another block's key is absent.

### B233

The managed service's list of unsupported names no longer satisfies the port and is dead; two agent-install checks cannot fire through admitted input; `make docs-check` does not run the refusal-table tests; and the engine's refusal of kinds no capability claims has no table (found in X31). **Exit evidence:** each settled.

### B234

No structural rule checks that a rescue ends in an unconditional fail, so one could swallow a refusal (the only rescue today does end so); and the libvirt host's pool template is rendered by no task but stays in the automation digest (found in X31). **Exit evidence:** the rule, and the template removed or rendered.

### B235

The lost-binding refusal names its operation and binding only in its message, because a diagnostic's object is an API object; and a refusal inherited from an Environment default gets the generic inherited-field remedy in place of its own, so a file source from a default names `secret set` only in its message (found in X31). **Exit evidence:** a diagnostic shape for such objects and inherited remedies kept.

### B236

A context whose evidence is corrupt or unsupported still has no runnable exit: deletion refuses it even with `--allow-orphans`, and the only remedy named is a whole-store restore no procedure provides; whether `--allow-orphans` may abandon such a context is an owner decision. The lost-binding refusal and its status step still name the orphan-acknowledged delete whatever the evidence reads; the context guard's evidence refusal names no remedy; and the lifecycle compares evidence by exact bytes where the guard accepts any spelling (found in X32). **Exit evidence:** the chosen exit, and each refusal naming a delete the guard admits.

### B237

An attempt interrupted after it publishes the clients but before it seals their area is later proved present and complete without sealing it, so the area stays writable although the specs say the stage completes only once it is sealed; and locating an installed tool reports any store error as not installed, pointing at the controller stage when store state is the cause (found in X32). **Exit evidence:** a resolution and replay that seal the area, and a lookup that names a store failure.

### B238

Setup's bound decision never counts controller-stage resolutions as retirable, so a host holding ones an earlier build accumulated keeps them until the same selection solves again and setup at the bound blames client areas; after a completed setup, the purge reports stage resolutions as retired execution bundles although it removed nothing; and resolutions of a selection no context makes any more are never retired (found in X32). **Exit evidence:** bound, purge-report and retirement tests over stage resolutions.

### B239

The controller stage's recovery and `preflight controller --context` select the newest retained release under a `latest` prefix from the host-wide sources, so once another context retains a newer client, a context's next fresh apply republishes under it undeclared, and its preflight reports its clients missing (found in X32, older than it). **Exit evidence:** stage and preflight tests in which another context's newer release is retained.

### B240

The controller selects the libvirt client for any Machine hosted on a libvirt provider, which the controller and state-reconciliation specs do not state; and no stage test runs a plan that selects the hypervisor closure end to end (found in X32). **Exit evidence:** the rule stated or dropped, and the test.

### B241

Native resolution and Ansible runner failures reached from the controller stage still name setup in their remedies; `preflight controller --context` compares a pending setup receipt's route with the context's Machine proxy, which setup never reads; and it reports a tool selection failure over retained sources with setup's remedy (found in X32). **Exit evidence:** remedy tests for each path.

### B242

In both runners, a record that needs an acknowledgement, read before the adapter's failed exit while the adapter is gone, fails its write with `EPIPE` and reports authorization delivery as uncertain, where reading the exit first keeps the failed exit; for a lifecycle attempt that is unknown against failed (found in X32). **Exit evidence:** runner tests that force each order.

### B243

An apply keeps room for one removal directory, so at the directory bound a failed removal's replacement refuses at registration; a removal's byte admission covers only its registration, so its records and logs can still stop it part way at the byte bound; and when the reserve cuts an attempt's output to nothing, the attempt log records nothing (found in X32). **Exit evidence:** bound tests for a replacing removal and for a removal's records, and a log entry for dropped output.

### B244

ansible-core 2.21.4 loads a runner's `--extra-vars @request.json` as trusted templates, so a request string holding Jinja delimiters is rendered on the controller, and since X32 argument validation renders every declared request string before the first task; no Go check refuses such delimiters, and whether an authored value can reach one is unverified. The install request would also carry `endpoints: null` if no endpoint address resolved, which validation now refuses (found in X32). **Exit evidence:** requests passed as unsafe data or delimiters refused, with a test per path.

### B245

Since X32 the adapter output prints nothing a failed `no_log` task raised, so completion refusals that name only fields, never values, now print nowhere; role comments still say ansible-core keeps nothing of such a failure; and the censor overrides a private ansible-core method that a move past 2.21 must re-qualify (found in X32). **Exit evidence:** a decision per refusal, the comments, and the re-qualification note.

### B246

The libvirt machine's apply resolution reads a controller on another image, a domain with another UUID or a resized disk as unknown with no reason, because the adapter proves its postcondition and Go refuses it on the comparison (found in X32). B213's text was also imprecise: before X32 only a silent BMC read partial on removal. **Exit evidence:** observation rows with named reasons.

### B247

An SSH placement recognized as the controller has its socket, unit and path claims compared only within its context and never published, so another context on the controller is not refused over them; a controller whose sshd listens on a port other than 22 is not recognized; one host reached by a name and an address is not recognized; and bridge readiness pairs providers and services by Machine name (found in X32). **Exit evidence:** the decision, and planning tests for each.

### B248

The artifact server's observation never compares the container's start with its configuration, unit or serving material, so an unknown resolution of an apply stopped before its restart can record it done while nginx serves an earlier configuration; and since X32 a managed-service apply whose host clock stepped back restarts on every attempt until the clock passes the file's time (found in X32). **Exit evidence:** observation and apply tests for both.

### B249

A Machine and a MachineInstallProfile with the same name publish into the same `os/<name>/` directory, and the Machine's removal deletes it recursively, taking the profile's tree; nothing refuses the collision (found in X32 by reading the code). The consumer-level `os/`, `private/` and `private/os/` directories stay until the artifact server's own removal, and no block owns them. **Exit evidence:** a refusal or distinct paths, with a test, and the owner's rule for consumer directories.

### B250

A node's port and credential refusals show the substrate's Machine-worded reason on a ContainerCluster diagnostic that does not name the node's Machine; and whether a provider's substrate is realized is decided by hand in three places (found in X32). **Exit evidence:** the wording, and one owner for the rule.

### B251

Lifecycle refusals other than the pre-boot proof, such as the managed-OS check that a machine holds another installation and the cluster boot's missing image, still reach the operator only through the adapter output; and the physical pre-boot refusals should be exercised end to end once B73 and B67 lift the physical refusals (found in X32). **Exit evidence:** a diagnostic per refusal, and an end-to-end run.

### B252

Every frozen-request decoder decodes strictly before it checks the version, so a request an earlier version froze with a since-removed field refuses as malformed instead of naming its version, as the state-reconciliation spec requires; and four decoders' unsupported-version messages name no version (found in X32). **Exit evidence:** decoder tests over an old request with a removed field.

### B255

Disabling these left every suite passing, before the splits as after: the desired-state issue for a resource path that selects no acquired file; the Machine BMC issue for credentials required after provider inheritance; the schema enum issue; a bootstrap whose wheels lack ansible-core or urllib3; a native recovery whose evidence is not recorded in the receipt; the compiler returning no state and no error; podman's native request intent swapped for libvirt's; the ELF RPATH and RUNPATH search, reached only by a network-tagged test; and in the keyring, secret material and workspace, the initialization-root refusal, the selector and part rotation, the publication preflight, the duplicate-part refusal, the duplicate `NoProxy` refusal and the refusal to adopt an existing reservation directory (found in X33). **Exit evidence:** a test that fails for each mutant.

### B256

Since X33 both awaiting-split lists are empty, but the collection's line-limit test still says their split is B47, and the Go test's failure message still offers adding a function to the list although the list only shrinks (found in X33). **Exit evidence:** both texts corrected.

### B257

The controller clients' refusal helper passes its remedy where the diagnostic takes a source path, so every such refusal that gives a remedy, the frozen-request version refusal, "run bootwright setup" and "repeat the operation" among them, carries it as a path and shows no remediation (found in X33, older than it). **Exit evidence:** diagnostic tests whose remediation holds the remedy and whose path is empty.

### B258

No command prunes retained operations, so a context at the 1024-operation bound leaves only through destroy and deletion (D52). Pruning needs a rule for which completed operations no block's ownership and no audit depends on. **Exit evidence:** the retention rule, and a pruning journey that keeps every operation the index names.

### B259

Since X34 a wait restores a truncated installer kubeconfig from the kept copy, but one the install-complete rewrites grew past the 64 KiB read bound and a kill cut to a prefix still over that bound is never read, so it is not restored and the wait fails as before; and a restore is recorded only in the completion evidence, so a wait that restored and then failed leaves no durable note (found in X34). **Exit evidence:** a cut proven without a full read, and the note.

### B260

X34's read of the registered hosts after a stall is proved against fakes taken from `openshift-install` 4.21.10's own `agent create config-image` output; a stalled multi-node install on a real host should name the missing node, and the watcher token should list the hosts. A knowledge page could record what that command writes (state members, `rendezvous-host.env`, `common.sh`, `set-hostname.sh`, `agent.service`) for later fakes (found in X34). **Exit evidence:** an acceptance-ledger row, and the page.

### B261

A carried-forward pending receipt does not record the bundle it was carried from, so its resume acquires every source from the publisher again, and the controller spec's rule that a carried resolution consults no publisher does not survive an interruption (found in X34). **Exit evidence:** a resume test that reads the retained bytes.

### B262

Below the bound, a pending receipt whose bundle holds no area and that another executable recorded still refuses only at preparation, after setup presented its plan, published its intent and reserved an empty area; and the purge's help and the bound refusal do not name the stranded case (found in X34). **Exit evidence:** a refusal before any effect, and the texts.

### B263

Since X34 a retained bundle that lost a bootstrap source falls back to a fresh resolution, but its retained row ends as a failure with no warning naming the lost source, and the fresh resolution downloads every source again, including those the retained bundle still holds (found in X34). **Exit evidence:** the display, and a seeded resolution.

### B264

The libvirt host's ownership check counts any Bootwright ownership element as owned, not only this context's, so a network carrying another context's metadata passes the foreign guard, is redefined and, since X34, restarted when idle, although the substrates spec says a network without this context's ownership is foreign and refuses (found in X34, older than it). **Exit evidence:** a host test in which another context's network refuses before any effect.

### B265

Nothing stops a Machine starting, for example through `machine start` in another invocation, between the host apply's guest read and its network restart; whether machine power commands are excluded while an apply runs was not checked (found in X34). **Exit evidence:** the exclusion shown, with a test.

### B266

The host block runs before any Machine of its apply is realized, so the running Machines X34's drifted-network refusal names can only be domains a deleted context left behind, and `machine stop` refuses each of them as not realized; the refusal could name the domain and stopping it on the host. The network and the Machines also reach only the retained output, because an adapter refusal cannot carry names it read (found in X34). **Exit evidence:** a refusal whose exit runs.

### B267

Since X34 the retained-operation refusal names destroy, but a removal refused at its own registration names an orphan-acknowledged delete and a fresh init, since destroy would refuse again; a fresh apply's claim does not reclaim idle claims under pristine evidence before counting them; and status still offers `apply` over a context at the bound (found in X34). **Exit evidence:** the owner's exit, a reclaim before counting, and the status step.

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
- **D21** (2026-09-29, taken by the session under the owner's instruction to
  proceed with recommendations; pending review): an elevated child announces
  that it started, and the held `sudo:` lines decide the outcome only when that
  announcement cannot be proved (X20).
- **D22** (2026-09-29, taken likewise; pending review): an elevated command
  that exits without a result reports `runtime.internal` with exit status 1;
  `runtime.interrupted` still wins after an interrupt (X20).
- **D23** (2026-09-30, taken by the session under the owner's instruction to proceed with recommendations): B9: a controller's CA bundle is the only anchor of its Redfish calls; without one, the system trust store.
- **D24** (2026-09-30, taken by the session under the owner's instruction to proceed with recommendations): B9: a physical Machine's virtual media imports the media certificate by default; `disable-verification` is admitted only on one Machine and refused beside private delivery.
- **D25** (2026-09-30, taken by the session under the owner's instruction to proceed with recommendations): B10: the local keyring becomes `local-keyring-v4`, which holds produced material; an earlier keyring refuses and names the build that reads it.
- **D26** (2026-09-30, taken by the session under the owner's instruction to proceed with recommendations): B10: the custodied kubeconfig is withdrawn when the context's removal of its cluster completes.
- **D27** (2026-09-30, accepted by the owner from the session's recommendations): B29: a foreign image on one of the cluster's own Machines reads as partial during an agent-install removal.
- **D28** (2026-09-30, accepted by the owner from the session's recommendations): B34: an SSH-host provider's hypervisor packages resolve at apply time; the frozen transaction is parked (B167).
- **D29** (2026-09-30, accepted by the owner from the session's recommendations): B40: admission refuses `spec.lifecycle.rescue` until a rescue journey exists.
- **D30** (2026-09-30, accepted by the owner from the session's recommendations): Follow-ups a slice finds enter M1 only when they are safety or defect items; enabling and polish follow-ups are parked unless they block an M1 item.
- **D31** (2026-09-30, accepted by the owner from the session's recommendations): B137: `status` offers as next steps only the verbs the records allow.
- **D32** (2026-09-30, accepted by the owner from the session's recommendations): B140: the cluster boot budget is 300 seconds per node with a 900-second floor; polled reads still count as answered at once.
- **D33** (2026-09-30, accepted by the owner from the session's recommendations): B141: the agent-install decoders refuse, for every verb, a frozen placement off the controller; the runner refuses a material listed twice.
- **D34** (2026-09-30, accepted by the owner from the session's recommendations): B143: a proved UUID or serial refuses anything that is not printable, by the CLI's printable rule.
- **D35** (2026-09-30, accepted by the owner from the session's recommendations): B146: a YAML key is refused for the construct it carries (`yaml.alias`, `yaml.tag`), as a value is.
- **D36** (2026-09-30, accepted by the owner from the session's recommendations): B41: the Secret `file` source is retired; operators load files with `secret set`.
- **D37** (2026-09-30, accepted by the owner from the session's recommendations): B42: a lost frozen binding refuses before any effect and names the exits: restore the keyring, or an orphan-acknowledged delete.
- **D38** (2026-09-30, accepted by the owner from the session's recommendations): B43: an unresolvable unknown block keeps its refusal, which names why and how to fix it; the orphan-acknowledged delete also releases host reservations.
- **D39** (2026-09-30, accepted by the owner from the session's recommendations): B136: a failed apply whose blocks are all done is recovered; the other two refused states name their exit.
- **D40** (2026-09-30, accepted by the owner from the session's recommendations): B44: only the remaining bundle-area limit is fixed; the layer is parked (B165).
- **D41** (2026-09-30, accepted by the owner from the session's recommendations): B45: a continuation freezes its Python and Ansible closure and refuses when it moves; scoped removal is parked (B166).
- **D42** (2026-09-30, accepted by the owner from the session's recommendations): B46: local setup keeps its Ansible output in its own bounded run area, the newest eight runs.
- **D43** (2026-09-30, accepted by the owner from the session's recommendations): B122: lifecycle JSON results are parked until a milestone names a consumer.
- **D44** (2026-09-30, accepted by the owner from the session's recommendations): B111: validation refuses wwn, hctl and serial root-device hints on a libvirt Machine.
- **D45** (2026-09-30, accepted by the owner from the session's recommendations): B116: the emulated BMC admits canonical IPv6 unicast bind addresses, with a bracketed endpoint and the shared socket key.
- **D46** (2026-09-30, accepted by the owner from the session's recommendations): B131: media publication proves nothing wrote the stage after it was measured, and the spec says so; the pre-X20 long-name leftover is removed by hand.
- **D47** (2026-09-30, accepted by the owner from the session's recommendations): B158: a newer Index API minor version makes setup warn and continue; a newer major version refuses.
- **D48** (2026-09-30, the owner): M1 is frozen after X33. Follow-ups X32, X33 and later slices find are parked here for a future milestone, whatever their kind, instead of entering M1; D30 no longer admits items to M1. M1 keeps its open items: X32, X33, the owner decisions and the chain behind B49.
- **D49** (2026-10-01, the owner accepted the session's recommendation A): B32: after a stall the install role reads the registered hosts from the rendezvous host's assisted-service API with the watcher token, bounded and under `no_log`, and the give-up names the node.
- **D50** (2026-10-01, the owner accepted the session's recommendation A): B175: `setup --purge-old-bundles` retires superseded areas under a pending receipt whose bundle holds no area, then resumes it.
- **D51** (2026-10-01, the owner accepted the session's recommendation A): B188: a drifted running network restarts when its Machines are stopped and otherwise refuses before any effect, naming them.
- **D52** (2026-10-01, the owner accepted the session's recommendation A): B206: the retained-operation refusal names destroy, then context deletion and a fresh init; pruning is parked (B258).
- **D53** (2026-10-01, the owner accepted the session's recommendation A): B208: a retained bundle that lost a bootstrap source falls back to a fresh resolution.
- **D54** (2026-10-01, the owner accepted the session's recommendation A): B220: before a wait, the install role restores a truncated installer kubeconfig from the kept copy when it names the same cluster.
- **D55** (2026-10-03, the owner accepted the session's recommendation A): B175: under `setup --purge-old-bundles`, a receipt stranded at the bound that this executable cannot resume is recorded as canceled, its never-started bundle publication observed as such, and a fresh setup follows; D50's resume stays for one it can prepare.
