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
| [B96](#b96) | Y3 | enabling | Architecture | Collection metadata and naming polish | Pruned from M1 on 2026-09-28 |
| [B97](#b97) | A8 (kind tables) | enabling | Desired state and Workspace | Kind field-table parity and a path checker | Pruned from M1 on 2026-09-28 |
| [B98](#b98) | C30; B336 split from it on 2026-10-05 (D101) | enabling | Architecture | Production comments reduced to what the clarity contract keeps | D101: M1 amends the rule, and a ratchet holds the count |
| [B99](#b99) | C1 (vSphere) | product | Substrate | One vSphere provisioning variant | No milestone names vSphere |
| [B100](#b100) | C2 (LoadBalancer) | product | Infrastructure services | One managed `LoadBalancer` lifecycle | No consumer; M5's MetalLB is an in-cluster add-on |
| [B101](#b101) | C6 (rest) | product | CLI | One more view of evidence or access, or a dashboard | No milestone names a view or dashboard |
| [B102](#b102) | C8 | product | Custom automation | One typed, invertible executable playbook journey | M7's packages are add-ons, not playbooks |
| [B103](#b103) | C14 | safety | Workspace and Secrets | Explicit complete-store restore | No milestone needs backup and restore |
| [B104](#b104) | C15; B337 split from it on 2026-10-05 (D85) | safety | Secrets | Bounded lifetime allocation for secret reservations | D85: after B337 only applies and new versions consume the budget; waits on B103 |
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
| [B180](#b180) | new, 2026-09-30 (X30) | enabling | Workspace, Controller setup and State reconciliation | Storage fixtures that can fail | Parked under the 2026-09-30 triage rule: test depth |
| [B181](#b181) | new, 2026-09-30 (X30) | safety | Secrets and State reconciliation | A bounded run's rebind names its consumer | Bounded at three bindings since X26; closing it changes the keyring format |
| [B193](#b193) | new, 2026-09-30 (X29) | enabling | Container cluster | A removal observation resolves only the tools it runs | Parked under the 2026-09-30 triage rule: efficiency |
| [B194](#b194) | new, 2026-09-30 (X29) | enabling | State reconciliation | A local placement asks for no SSH material | Parked under the 2026-09-30 triage rule: tidiness |
| [B196](#b196) | new, 2026-09-30 (X29) | enabling | Architecture | Two lessons for the knowledge pages | Parked under the 2026-09-30 triage rule: knowledge |
| [B197](#b197) | new, 2026-09-30 (X29) | enabling | Architecture, with each test's owner | Tests X29 left narrower than they read | Parked under the 2026-09-30 triage rule: test depth |
| [B199](#b199) | new, 2026-09-30 (X29) | enabling | CLI | Completion over word breaks and truncated sets | Needs an owner decision on what the operator sees |
| [B226](#b226) | new, 2026-09-30 (X31) | enabling | State reconciliation | Status over a revision that no longer compiles | Parked under the 2026-09-30 triage rule |
| [B227](#b227) | new, 2026-09-30 (X31) | enabling | State reconciliation | A continuation over a reprojected bundle refuses first | Parked under the 2026-09-30 triage rule: no effect runs |
| [B229](#b229) | new, 2026-09-30 (X31) | enabling | Architecture | Knowledge lessons X31 left | Parked under the 2026-09-30 triage rule: knowledge |
| [B230](#b230) | new, 2026-09-30 (X31) | enabling | Each test's owner | Tests X31 left narrower than they read | Parked under the 2026-09-30 triage rule: test depth |
| [B231](#b231) | new, 2026-09-30 (X31) | enabling | Controller setup | A completed purge also retires the kept bundle's superseded resolutions | Parked under the 2026-09-30 triage rule |
| [B232](#b232) | new, 2026-09-30 (X31) | safety | State reconciliation and Secrets | Each block's execution holds only its own parts | The key stays in the operation's memory while the server shares the operation; needs a port |
| [B235](#b235) | new, 2026-09-30 (X31) | enabling | CLI and Desired state | Refusals for objects that are not API objects | Parked under the 2026-09-30 triage rule |
| [B238](#b238) | new, 2026-09-30 (X32) | defect | Controller setup | Setup counts controller-stage resolutions | Found after the M1 freeze (D48) |
| [B243](#b243) | new, 2026-09-30 (X32) | safety | State reconciliation and Workspace | Room for a replacing removal and its records | Found after the M1 freeze (D48) |
| [B245](#b245) | new, 2026-09-30 (X32) | enabling | Architecture, with each role's owner | Censored completions that name only fields | Found after the M1 freeze (D48) |
| [B247](#b247) | new, 2026-09-30 (X32) | safety | Infrastructure services and Substrate | Socket claims of an SSH placement that is the controller | Found after the M1 freeze (D48); publishing SSH claims needs an owner decision |
| [B248](#b248) | new, 2026-09-30 (X32) | defect | Infrastructure services | The artifact server compares its start with its files | Found after the M1 freeze (D48) |
| [B256](#b256) | new, 2026-10-01 (X33) | enabling | Architecture | Line-limit texts for empty lists | Found after the M1 freeze (D48) |
| [B258](#b258) | split from B206 on 2026-10-01 (D52) | enabling | State reconciliation | Pruning retained operations | D52 names the existing exits; pruning needs a retention rule for evidence and audit |
| [B261](#b261) | new, 2026-10-01 (X34) | defect | Controller setup | A resumed carried-forward receipt reads its approved bytes | Found after the M1 freeze (D48) |
| [B262](#b262) | new, 2026-10-01 (X34) | safety | Controller setup | A pending receipt this executable cannot prepare refuses first | Found after the M1 freeze (D48) |
| [B263](#b263) | new, 2026-10-01 (X34) | enabling | Controller setup | The fresh-resolution fallback says why and reuses what it can | Found after the M1 freeze (D48); the display needs an owner decision |
| [B266](#b266) | new, 2026-10-01 (X34) | enabling | Substrate | The drifted-network refusal names an exit that works | Found after the M1 freeze (D48) |
| [B267](#b267) | new, 2026-10-01 (X34) | enabling | State reconciliation | Exits at the retained-operation bound | Found after the M1 freeze (D48); one part needs an owner confirmation |
| [B268](#b268) | new, 2026-10-03 (X35) | enabling | Controller setup | The storage contract holds doubles to the terminal-receipt rule | Found after the M1 freeze (D48) |
| [B269](#b269) | new, 2026-10-03 (X35) | defect | Controller setup | A setup at the bound that fails after an effect reports it | Found after the M1 freeze (D48) |
| [B273](#b273) | new, 2026-10-03 (X37) | enabling | Architecture | Build and gate on Go 1.27 | Found after the M1 freeze (D48); needs an owner decision |
| [B274](#b274) | new, 2026-10-03 (X37) | enabling | Architecture | The Ansible check gate on CPython 3.14 | Found after the M1 freeze (D48); needs an owner decision |
| [B322](#b322) | new, 2026-10-05 | enabling | Controller setup | `setup --purge-old-bundles` retires the client areas nothing names | D90: no M1 journey reaches 15 client closures, and M1 ships the remedies |
| [B323](#b323) | new, 2026-10-05 | safety | Infrastructure services | Removals prove a service's sockets free before releasing them | D102: no failure was observed, and the proof belongs in the evidence functions B19 creates |
| [B324](#b324) | new, 2026-10-05 | enabling | State reconciliation and Workspace | An orphan-acknowledged delete lists the blocks it abandons | D88: M1 names `status --context` as the inventory |
| [B325](#b325) | new, 2026-10-05 | safety | Workspace | A damaged context is isolated from the rest of the store | D63: M1 keeps store-wide verification and names the damaged entry |
| [B327](#b327) | new, 2026-10-05 | product | Managed OS | DHCP installation | D80: M1 refuses DHCP-only installations |
| [B328](#b328) | new, 2026-10-05 | enabling | State reconciliation | The lifecycle's execution file split by concern | An optional move no M1 lane needs; D56 leaves it out of M1 |
| [B329](#b329) | new, 2026-10-05 | enabling | Architecture | A declaration-spacing check across the tree | An optional check no M1 lane needs; D56 leaves it out of M1 |
| [B330](#b330) | new, 2026-10-05 | product | Controller | An entitled-source adapter for RHEL client closures | D106: M1 accepts operator-installed `lorax` and `xorriso` by presence |
| [B331](#b331) | new, 2026-10-05 | product | Controller setup | A FIPS-qualified controller runtime | D108: M1 documents the runtime's own cryptography on a FIPS host |
| [B332](#b332) | new, 2026-10-05 | product | Machine and Controller | `auth.operatorIdentity` runs under the invoking account | D112: M1 documents what the arm does today; this needs B282's helper |
| [B333](#b333) | new, 2026-10-05 | enabling | Infrastructure services | A DNSServer binds its endpoint address by default | D114: M1 keeps the wildcard default and names a colliding socket |
| [B334](#b334) | new, 2026-10-05 | safety | Substrate | The emulated BMC serves TLS, binds loopback and runs confined | D115: M1 binds the examples' emulated BMCs to loopback |
| [B335](#b335) | new, 2026-10-05 | enabling | Controller setup | An operator-run RHEL 9.8 resolution harness | Optional beside the RHEL 9.8 controller run D60 plans |
| [B341](#b341) | new, 2026-10-05 (X39) | enabling | State reconciliation | The template-delimiter refusal names the authored field and scopes native content | Found in X39 after D56; parked under D48; the scope needs an owner decision |
| [B345](#b345) | new, 2026-10-05 (X39) | enabling | Controller, with CLI | A sudoers denial in a human invocation names its remedy | Found in X39 after D56; parked under D48 |
| [B346](#b346) | new, 2026-10-05 (X39) | enabling | Machine, with Trust | A FIPS-mode controller verifies the host key types it pins | Found in X39 after D56; parked under D48 |
| [B348](#b348) | new, 2026-10-05 (X39) | enabling | Controller (privilege) | The invoking account resolves once per process, and directory data the passwd grammar refuses is named | Found in X39 after D56; parked under D48 |
| [B352](#b352) | new, 2026-10-05 (X39) | enabling | Workspace, with Controller | The invoking-account helper and real controller accounts proved as root | Found in X39 after D56; parked under D48 |
| [B353](#b353) | new, 2026-10-05 (X39) | enabling | Each test's owner | Tests X39 left narrower than they read | Found in X39 after D56; parked under D48 |
| [B354](#b354) | new, 2026-10-05 (X39) | enabling | Each spec's owner | Wording X39 left | Found in X39 after D56; parked under D48 |
| [B355](#b355) | new, 2026-10-05 (X39) | enabling | Architecture | Knowledge lessons X39 left | Found in X39 after D56; parked under D48 |
| [B362](#b362) | new, 2026-10-06 (X40) | enabling | Secrets | A Secret version stored before X40 stays current across a re-import from another directory | Found in X40; parked under D48 (D116); D71 accepted the residual |
| [B363](#b363) | new, 2026-10-06 (X40) | enabling | State reconciliation | Status diagnoses a removal whose source apply's records are unreadable | Found in X40; parked under D48 (D116); the specs require today's refusal |
| [B364](#b364) | new, 2026-10-06 (X40) | enabling | State reconciliation | A plan preview over a keyring listing that fails as corrupt names the lost binding | Found in X40; parked under D48 (D116) |
| [B365](#b365) | new, 2026-10-06 (X40) | defect | Desired state, with Add-ons | The add-on marker rule and the input reader agree | Found in X40; parked under D48 (D116); add-on input is in no M1 journey |
| [B366](#b366) | new, 2026-10-06 (X40) | enabling | Machine, with Desired state | Repeated reads X40 left | Found in X40; parked under D48 (D116) |
| [B367](#b367) | new, 2026-10-06 (X40) | enabling | Desired state | Validate refusals X40 left | Found in X40; parked under D48 (D116) |
| [B368](#b368) | new, 2026-10-06 (X40) | enabling | Each test's owner | Tests and gates X40 left | Found in X40; parked under D48 (D116) |
| [B369](#b369) | new, 2026-10-06 (X40) | enabling | Each spec's owner | Wording X40 left | Found in X40; parked under D48 (D116) |
| [B374](#b374) | new, 2026-10-06 (X40) | enabling | State reconciliation | Status names a keyring listing that fails for any reason | Found in X40; parked under D48 (D116) |
| [B375](#b375) | new, 2026-10-06 (X40) | enabling | State reconciliation, with Controller setup | Status over a failed setup receipt that has no execution definition | Found in X40; parked under D48 (D116) |
| [B376](#b376) | new, 2026-10-06 (X40) | enabling | Controller and CLI | A ready baseline readiness names its next step | Found in X40; parked under D48 (D116) |
| [B385](#b385) | split from B94 on 2026-10-06 (X41) | enabling | Workspace, with Controller setup | Setup's receipt and controller mutation drop the context they never use | Found in X41; parked under D48 (D116); dropping the receipt member moves the setup plan digest |
| [B386](#b386) | new, 2026-10-06 (X41) | enabling | Controller, with Environment and Container cluster | Dead code X41 left | Found in X41; parked under D48 (D116) |
| [B387](#b387) | new, 2026-10-06 (X41) | enabling | Managed OS | The installation request, its digest and its role drop what D82 retired | Found in X41; parked under D48 (D116); it moves every installation plan digest and the automation digest |
| [B388](#b388) | new, 2026-10-06 (X41) | enabling | CLI, with Secrets and Machine | One confirmer port carries `ConfirmIn`, and no unconfigured-confirmer refusal is left without its command | Found in X41; parked under D48 (D116) |
| [B389](#b389) | new, 2026-10-06 (X41) | enabling | Controller | The controller runner's job and scratch use the staging parent | Found in X41; parked under D48 (D116); B19's one runner may carry it |
| [B390](#b390) | new, 2026-10-06 (X41) | enabling | Controller (privilege), with CLI | An interactive session's sudo refusal exits 255 | Found in X41; parked under D48 (D116); needs a start signal sudo lets through |
| [B391](#b391) | new, 2026-10-06 (X41) | enabling | CLI, with State reconciliation | Plan previews and interrupts X41 left | Found in X41; parked under D48 (D116) |
| [B392](#b392) | new, 2026-10-06 (X41) | enabling | Managed OS and Secrets | Two decision readings X41 applied are confirmed | Found in X41; parked under D48 (D116); needs the owner's confirmation |
| [B393](#b393) | split from B289 on 2026-10-06 (X41) | enabling | CLI, with Managed OS | The CLI calls the managed-OS media-name rule | Found in X41; parked under D48 (D116); a parity test holds the copy |
| [B394](#b394) | new, 2026-10-06 (X41) | enabling | Each test's owner | Tests X41 left narrower than they read | Found in X41; parked under D48 (D116) |
| [B395](#b395) | new, 2026-10-06 (X41) | enabling | Each spec's owner | Wording X41 left | Found in X41; parked under D48 (D116) |
| [B396](#b396) | new, 2026-10-06 (X41) | enabling | Architecture | Knowledge lessons X41 left | Found in X41; parked under D48 (D116) |
| [B397](#b397) | new, 2026-10-06 (X41) | defect | Container cluster | An off-controller boot-image server refuses through the cluster's refusal table | Found in X41; parked under D48 (D116); container-cluster installs are in no M1 journey |
| [B411](#b411) | new, 2026-10-06 (X42) | defect | Container cluster and Add-ons | Cluster and add-on image digests are lowercased, and cluster names fit their block identity | Found in X42; parked under D48 (D116); container-cluster and add-on input is in no M1 journey |
| [B412](#b412) | new, 2026-10-06 (X42) | defect | Infrastructure services | A wildcard bind serves its IP endpoints' family | Found in X42; parked under D48 (D116); IPv6 service endpoints are in no M1 journey |
| [B413](#b413) | new, 2026-10-06 (X42) | enabling | Managed OS, with CLI | A media transfer shows its bytes and bounds its response headers | Found in X42; parked under D48 (D116); byte progress needs the owner to amend the output spec's rule 3 |
| [B414](#b414) | new, 2026-10-06 (X42) | enabling | Controller setup | An unreadable FIPS flag is reported, not a stop | Found in X42; parked under D48 (D116) |
| [B415](#b415) | new, 2026-10-06 (X42) | enabling | Managed OS, with Environment | One OS release rule for the rescue image and the install profile | Found in X42; parked under D48 (D116) |
| [B416](#b416) | new, 2026-10-06 (X42) | enabling | Architecture, with Workspace, Substrate and Infrastructure services | Copies and dead code X42 left | Found in X42; parked under D48 (D116) |
| [B417](#b417) | new, 2026-10-06 (X42) | enabling | Each test's owner | Tests X42 left narrower than they read | Found in X42; parked under D48 (D116) |
| [B418](#b418) | new, 2026-10-06 (X42) | enabling | Each spec's owner | Wording X42 left | Found in X42; parked under D48 (D116) |
| [B419](#b419) | new, 2026-10-06 (X42) | enabling | Desired state, with Machine and Infrastructure services | Validate diagnostics X42 left | Found in X42; parked under D48 (D116) |
| [B427](#b427) | new, 2026-10-07 (X43) | enabling | Managed OS | Configured repositories can serve the installation's own package set | Found in X43; parked under D48 (D116); D97 made them installed-system configuration, so install-time use needs its own decision |
| [B428](#b428) | new, 2026-10-07 (X43) | enabling | Managed OS | A replay that publishes nothing skips the tree image's digest | Found in X43; parked under D48 (D116); the cost is unmeasured on a host |
| [B429](#b429) | new, 2026-10-07 (X43) | enabling | Controller setup | The qualified foundation reaches every guard typed, and Go-run helper failures keep their raw line | Found in X43; parked under D48 (D116) |
| [B430](#b430) | new, 2026-10-07 (X43) | defect | Managed OS | Media trust by `import-certificate` serves a guest-agent Machine whose server declares a certificate | Found in X43; parked under D48 (D116); media trust by `import-certificate` is in no M1 journey (lab-rhel declares none); it fails safe |
| [B431](#b431) | new, 2026-10-07 (X43) | enabling | Each test's owner | Tests and dead code X43 left | Found in X43; parked under D48 (D116) |
| [B432](#b432) | new, 2026-10-07 (X43) | enabling | Each spec's owner | Wording X43 left | Found in X43; parked under D48 (D116) |
| [B433](#b433) | new, 2026-10-07 (X43) | enabling | State reconciliation, with Controller setup and Managed OS | Host observations of what X43 proved only in-tree | Found in X43; parked under D48 (D116); each needs a host run, which M1's closing run (D59) and the native opt-in targets provide |
| [B434](#b434) | new, 2026-10-07 (X43) | enabling | State reconciliation and Container cluster | One rule for a request version and the content derived within one shape | Found in X43; parked under D48 (D116); it needs a decision and moves a digest, so a digest window |
| [B438](#b438) | new, 2026-10-07 (X24) | enabling | Infrastructure services, with State reconciliation | A wildcard bind's foreign-listener refusal names the colliding address | Found in X24; parked under D48 (D116); the refused record carries no address, and D114 accepted a refusal that names the port and the bind |
| [B439](#b439) | new, 2026-10-07 (X24) | enabling | Infrastructure services | A realized Registry and the managed examples hold the foreign-listener rule | Found in X24; parked under D48 (D116); no Registry is realized and no example binds a managed service to a wildcard |
| [B441](#b441) | new, 2026-10-07 (X24) | enabling | Architecture | The last canonical-JSON proofs and wrappers | Found in X24; parked under D48 (D116); no behavior changes |
| [B442](#b442) | new, 2026-10-07 (X24) | enabling | State reconciliation, with Controller and Infrastructure services | Host observations of what X24 proved only in-tree | Found in X24; parked under D48 (D116); each needs a host run, which M1's closing run (D59) provides |
| [B443](#b443) | new, 2026-10-07 (X24) | enabling | Each test's owner | Tests X24 left narrower than they read | Found in X24; parked under D48 (D116) |
| [B444](#b444) | new, 2026-10-07 (X24) | enabling | Architecture, with each capability's owner | Copies and dead code X24 left | Found in X24; parked under D48 (D116) |
| [B445](#b445) | new, 2026-10-07 (X24) | enabling | Controller, with Architecture | The cancellation's group-kill order and its knowledge page | Found in X24; parked under D48 (D116); the order is bounded by the drain and was older than X24 |

### B96

Collection metadata and naming polish, including the collection's sanity
README, whose `missing-gplv3-license` note names only documentation stubs
although the ignore file also exempts the real modules with no action plugin
(found in X19). **Exit evidence:** `./scripts/ansible-check`.

### B97

Kind field-table to `Shape` parity and the `Value.Get` path checker. **Exit
evidence:** parity tests over every kind.

[B294](delivered.md#x42--media-contexts-setup-service-and-machine-admission) wrote the Machine kind tables' missing rows in M1 (found by the 2026-10-05 audit); the parity tests and the checker stay here.

### B98

Reduce production comments to what the
[code clarity contract](../architecture.md#self-explanatory-code-and-retained-knowledge)
retains. Most production comments state rationale the contract sends to the
knowledge catalog, and some state invariants that must survive as tests or
names rather than prose. Deciding each one is the work; a blanket strip would
lose the findings the catalog is meant to keep. Split on 2026-10-05 into
[B336](m1.md#b336), whose amended rule and ratchet hold the count meanwhile
(D101). **Exit evidence:** each retained comment justified by language,
tooling or a maintained contract; every durable finding moved into
`.agents/knowledge/` with its links and evidence; and B336's ratchet lowered
to the result.

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
opaque random version and binding IDs retain historical reservation files,
about 32,000 of which a context's keyring admits in its lifetime, and every
bounded run consumes one, so a loop of `machine` power, `rsh` or `exec`
commands exhausts a context (found by the 2026-10-05 audit, which corrected
this item's reason). Split on 2026-10-05 into [B337](delivered.md#x41--lifecycle-results-the-controller-stage-installation-shapes-and-bounded-runs), after which
bounded runs publish no durable binding and only applies and new versions
consume reservations (D85). A new allocation scheme must preserve issued-ID
non-reuse across crashes and restore, so it needs [B103](#b103)'s restore
semantics. **Exit evidence:** bounded allocator state, reservation before use,
counter and namespace exhaustion, migration of existing bindings and failed
attempts, non-reuse and crash tests.

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

### B180

The contextfs retirement fixture sets retained definitions that `Publish` ignores, so its kept-resolution assertion always passes; the controller storage memory double keeps no receipt's resolution append-only and refuses no replaced one, so the storage suite cannot require that retirement drops a retired bundle's resolution; and the kill harness's workspace clone does not copy the journey double's client files (found in X30). **Exit evidence:** fixtures and doubles that fail when the rule breaks.

Since X31 the contextfs retirement fixture holds real resolutions, so its kept-resolution check is meaningful; the memory double also retires an area no resolution names, which the store refuses.

### B181

A bounded run's rebind is limited to three bindings rather than closed, which a binding that names its consumer would close; that is a keyring format change (left by B139 in X30). **Exit evidence:** a binding format that names its consumer, and a rebind test that refuses any other.

### B193

The agent-install removal observation still resolves the openshift-install and oc paths although it runs no oc read; the destroy that follows needs them (found in X29). **Exit evidence:** an observation that resolves only what it runs.

[B309](m3.md#b309) makes the installation block's tool-resolution failure deterministic; this item keeps the removal observation's resolution.

### B194

The lifecycle's material list adds the SSH identity and host-key files whenever a placement names their Secrets, whatever its connection, so a local placement carrying SSH references still asks for SSH material; since X29 the agent-install decoders refuse such a placement, other decoders do not (found in X29). **Exit evidence:** a material-list test over a local placement.

### B196

ansible-core 2.21.4 keeps a Jinja string-literal escape such as `'\n'` as a backslash and an `n`, so a role that joins with it writes a literal backslash-n; and `renameat2` moves the renamed inode's change time on ext4, XFS, Btrfs and tmpfs, so a status proof across a rename must leave the change time out (both confirmed by probes in X29). **Exit evidence:** a knowledge page for each.

### B197

Two tests still put wwn hints on libvirt guests that admission now refuses; the media store contract suite has no missing-callback clause and its double would panic; the second-implementation secret store fake assigns no sequence; and two runner consume sites keep no test pinning their remediation (found in X29). **Exit evidence:** each test narrowed or added.

### B199

Bash's default word breaks also contain `:`, so a path with `:` breaks completion of the rest of the word as `@` and `=` did, and PowerShell's `,` may belong in the withheld set (not verified on a real shell); and when the candidate cap or the read bound truncates a listing, the shell still inserts the longest common prefix of the partial set (found in X29). **Exit evidence:** the chosen sets and truncation behavior, with completion tests.

### B226

Status compiles the imported revision and refuses when it no longer compiles, so a context imported with a retired field, since X31 a Secret file source, loses status until `context update`, although its destroy still works (found in X31). Since [X42](delivered.md#x42--media-contexts-setup-service-and-machine-admission) the orphan refusal, the orphan-acknowledged deletion's confirmation, the deletion plan and the `context update` refusal over an incomplete operation name `bootwright status --context <name>` as the inventory or the continuation, while X42's admission rules refuse revisions an earlier build imported, such as one holding the libvirt provider's removed certificate opt-out, MACs under a libvirt Machine's `hardware.nics`, an install address equal to its bridge's host address or a tag-pinned service image. For such a context the inventory those texts promise is unavailable, and the update refusal names only its destroy outright, so the apply continuation goes unnamed (found in X42). **Exit evidence:** status over such a revision reporting its records, and status or the update refusal naming the continuation, apply or destroy as the operation index records it rather than the mutation evidence, for a context whose stored revision does not compile.

### B227

A continuation run by its registering build over a bundle a later build reprojected passes the closure check and is refused only per attempt by the runner, after its log is restored and it is marked running; comparing the receipt's automation digest before the restore would refuse first (found in X31). **Exit evidence:** a continuation test that refuses before any record changes.

### B229

A knowledge page keyed on `SyntaxWarning: invalid decimal literal` for the ansible-check area name (B203), and the carry-forward page citing the test that carries a settled receipt forward (found in X31). **Exit evidence:** the pages.

X32 found the mechanism: ansible-core renders its home defaults through Jinja's native environment, whose concatenation parses the text, so any home path in which a number runs into a keyword warns in every process; CPython also warns for other number forms, such as `1_1if`, `1e1if` and `0x1fif`.

### B230

No test pins the pre-plan refusal of a pending receipt whose bundle is incompatible; the lifecycle test double's held area lets a removal skip its closed and read-only checks; and no runner test pins a partial record waiting at the drain after a failed exit, whose outcome X31 made unknown (found in X31). **Exit evidence:** each test.

### B231

After a completed setup, `--purge-old-bundles` retires superseded areas but leaves the superseded resolutions of the receipt's own bundle until a setup at the bound needs their room (found in X31). **Exit evidence:** a purge test that retires them.

### B232

Since X31 a run request lends a block only the Secret parts it writes, but each attempt's execution and quiescence probe still hand the capability's Go code the whole reopened operation map, the artifact server's key included (found in X31). **Exit evidence:** a port through which a capability declares its blocks' parts, with a test that another block's key is absent.

### B235

The lost-binding refusal names its operation and binding only in its message, because a diagnostic's object is an API object; and a refusal inherited from an Environment default gets the generic inherited-field remedy in place of its own, so a file source from a default names `secret set` only in its message (found in X31). **Exit evidence:** a diagnostic shape for such objects and inherited remedies kept.

### B238

Setup's bound decision never counts controller-stage resolutions as retirable, so a host holding ones an earlier build accumulated keeps them until the same selection solves again and setup at the bound blames client areas; after a completed setup, the purge reports stage resolutions as retired execution bundles although it removed nothing; and resolutions of a selection no context makes any more are never retired (found in X32). **Exit evidence:** bound, purge-report and retirement tests over stage resolutions.

**Partly settled in [X35](delivered.md#x35--the-decision-of-2026-10-03).** The purge after a completed setup names and reports only the areas the store holds, and retires a resolution whose bundle holds no area only when it belongs to setup's own closure, so it neither reports a controller stage's resolution as a retired execution bundle nor retires it; `TestSetupsPurgeLeavesAControllerStageResolution` pins this over the real store. The bound clause and the stale-selection clause stay parked.

### B243

An apply keeps room for one removal directory, so at the directory bound a failed removal's replacement refuses at registration; a removal's byte admission covers only its registration, so its records and logs can still stop it part way at the byte bound; and when the reserve cuts an attempt's output to nothing, the attempt log records nothing (found in X32). **Exit evidence:** bound tests for a replacing removal and for a removal's records, and a log entry for dropped output.

### B245

Since X32 the adapter output prints nothing a failed `no_log` task raised, so completion refusals that name only fields, never values, now print nowhere; role comments still say ansible-core keeps nothing of such a failure; and the censor overrides a private ansible-core method that a move past 2.21 must re-qualify (found in X32). **Exit evidence:** a decision per refusal, the comments, and the re-qualification note.

### B247

An SSH placement recognized as the controller has its socket, unit and path claims compared only within its context and never published, so another context on the controller is not refused over them; a controller whose sshd listens on a port other than 22 is not recognized; one host reached by a name and an address is not recognized; and bridge readiness pairs providers and services by Machine name (found in X32). **Exit evidence:** the decision, and planning tests for each.

### B248

The artifact server's observation never compares the container's start with its configuration, unit or serving material, so an unknown resolution of an apply stopped before its restart can record it done while nginx serves an earlier configuration; and since X32 a managed-service apply whose host clock stepped back restarts on every attempt until the clock passes the file's time (found in X32). **Exit evidence:** observation and apply tests for both.

### B256

Since X33 both awaiting-split lists are empty, but the collection's line-limit test still says their split is B47, and the Go test's failure message still offers adding a function to the list although the list only shrinks (found in X33). **Exit evidence:** both texts corrected.

### B258

No command prunes retained operations, so a context at the 1024-operation bound leaves only through destroy and deletion (D52). Pruning needs a rule for which completed operations no block's ownership and no audit depends on. **Exit evidence:** the retention rule, and a pruning journey that keeps every operation the index names.

### B261

A carried-forward pending receipt does not record the bundle it was carried from, so its resume acquires every source from the publisher again, and the controller spec's rule that a carried resolution consults no publisher does not survive an interruption (found in X34). **Exit evidence:** a resume test that reads the retained bytes.

### B262

Below the bound, a pending receipt whose bundle holds no area and that another executable recorded still refuses only at preparation, after setup presented its plan, published its intent and reserved an empty area; and the purge's help and the bound refusal do not name the stranded case (found in X34). **Exit evidence:** a refusal before any effect, and the texts.

### B263

Since X34 a retained bundle that lost a bootstrap source falls back to a fresh resolution, but its retained row ends as a failure with no warning naming the lost source, and the fresh resolution downloads every source again, including those the retained bundle still holds (found in X34). **Exit evidence:** the display, and a seeded resolution.

### B266

The host block runs before any Machine of its apply is realized, so the running Machines X34's drifted-network refusal names can only be domains a deleted context left behind, and `machine stop` refuses each of them as not realized; the refusal could name the domain and stopping it on the host. The network and the Machines also reach only the retained output, because an adapter refusal cannot carry names it read (found in X34). **Exit evidence:** a refusal whose exit runs.

### B267

Since X34 the retained-operation refusal names destroy, but a removal refused at its own registration names an orphan-acknowledged delete and a fresh init, since destroy would refuse again; a fresh apply's claim does not reclaim idle claims under pristine evidence before counting them; and status still offers `apply` over a context at the bound (found in X34). **Exit evidence:** the owner's exit, a reclaim before counting, and the status step.

### B268

The storage contract suite has no clause for the store's rule on settled receipts: a failed or canceled receipt keeps no intent or unknown effect, and every observed action carries evidence. Since X35 the prerequisites memory double enforces it, but no contract clause holds a future double to it (found in X35). **Exit evidence:** a contract clause both stores pass and a double without the rule fails.

### B269

When a setup at the bound fails after its first durable effect, the cancellation of a stranded receipt or a retirement, its result reports the outcome `planned`, which implies nothing changed, and does not name the cancellation (found in X35). **Exit evidence:** a failure after the cancellation and one after a retirement, each reported with what changed.

### B273

X37 moved Go only to the newest 1.26 patch, go1.26.8, while go1.27.1 is the newest release. Go 1.27 backs `encoding/json` with its v2 implementation, whose error texts may differ, drains an HTTP/1 response body on close and removes five TLS and X.509 GODEBUG settings, so the move changes product behavior and is the owner's decision (found in X37). **Exit evidence:** the owner's decision; for a move, `scripts/go` and both modules' `go` directives on 1.27 with every gate passing, and those changes reviewed against the product's JSON records, refusals and goldens and its HTTP acquisition.

### B274

The Ansible check gate runs on CPython 3.13, while fresh setups resolve the newest 3.14 patch, the newest minor ansible-core 2.21 supports as a controller. Moving the gate re-platforms it rather than bumping a pin: `scripts/tools/ansible_check.py` runs ansible-test with `--python 3.13` and `origin:python=3.13`, and `scripts/tools/ansible-test-artifacts.json` holds cp313 wheels of MarkupSafe and PyYAML for ansible-test 2.21.4's exact sanity pins (found in X37). **Exit evidence:** the owner's decision; for a move, the gate interpreter and its lock, the artifact lock and those arguments on 3.14, with the full gate passing.

### B322

Client areas count toward the 16-area bound, but nothing removes one, so a long-lived controller eventually wedges setup and the controller stage (found by the 2026-10-05 audit); [B292](delivered.md#x42--media-contexts-setup-service-and-machine-admission) gave the refusals their remedies and corrected the spec. `setup --purge-old-bundles` would retire a client area that no context binding or proved stage names, the controller record keeping each context's proved area in a field omitted when empty. **Exit evidence:** a purge that retires an unnamed client area and keeps every named one, and the record field.

### B323

Managed-service and artifact-server removals release their socket reservations without proving the sockets free (found by the 2026-10-05 audit); [B301](m1.md#b301) adds the Not-yet-met line. Each reserved socket would be probed after the stop, and its absence required in both absence checks. **Exit evidence:** absence tests in which a socket still held refuses the release.

### B324

An orphan-acknowledged context deletion names `bootwright status --context <name>` as its inventory since [B291](delivered.md#x42--media-contexts-setup-service-and-machine-admission). A Reconciliation guard port could list the owned blocks in the refusal, the prompt and the result, which serves real machines better (found by the 2026-10-05 audit). **Exit evidence:** the port, and refusal, prompt and result goldens that list the blocks.

### B325

Store-wide verification refuses every command for every context when one context is damaged; since [X39](delivered.md#x39--untrusted-input-trust-and-privilege-boundaries) the refusal names the damaged entry and admits that context's purge where the purge removes the damage. Isolating the damage per context, listing it with its reason and admitting its purge would keep the healthy contexts usable, at the cost of the store's fail-closed verification (found by the 2026-10-05 audit). X39 found two more cases it would serve: a ready context whose reservation is missing or unsafe cannot be purged, because the deletion lease verifies that reservation, and a damaged context that still owns live objects cannot be destroyed, because destroy verifies every context's mapping, so its only exits are `--allow-orphans` and a whole-store restore. **Exit evidence:** store tests in which a damaged context is listed and purged while the others keep working, a context whose reservation is missing purged, and a damaged context that owns live objects destroyed.

### B327

An Anaconda installation needs a static IPv4 install address, because completion dials and pins the frozen address, and [B289](delivered.md#x41--lifecycle-results-the-controller-stage-installation-shapes-and-bounded-runs) refuses a DHCP-only installation at admission. DHCP installation needs its own completion design (found by the 2026-10-05 audit). **Exit evidence:** that design, with completion and replay tests.

### B328

`internal/reconciliation/lifecycle/execution.go` holds the operation's decision, registration, removal gates and completion in about 1,600 lines, with registration and completion split across files (found by the 2026-10-05 audit). A pure move into one file per concern would make each easier to read. **Exit evidence:** the move with no behavior change, and `make check`.

### B329

Top-level declarations run together against the formatting rule in about 103 places, mostly in container-cluster admission and storage, and two projection names mislead (found by the 2026-10-05 audit). **Exit evidence:** an architecture-suite check with every case fixed, and the two renames.

### B330

A RHEL controller resolves client closures from UBI sources only, and UBI carries no `lorax` or `xorriso`; since [B288](delivered.md#x41--lifecycle-results-the-controller-stage-installation-shapes-and-bounded-runs) the operator installs both from the host's entitled repositories. An adapter over the host's enabled repositories or a Satellite could freeze RHEL client closures as UBI sources are frozen (found by the 2026-10-05 audit). **Exit evidence:** a frozen closure from an entitled source, with acquisition, signature and replay tests.

### B331

On a FIPS-mode controller host, Bootwright's Go binary and its private CPython use their own cryptography, outside the host's validated modules, which [B292](delivered.md#x42--media-contexts-setup-service-and-machine-admission) documents and preflight reports. Qualifying FIPS would use Go's FIPS 140-3 module and a private CPython linked to the system OpenSSL FIPS provider (found by the 2026-10-05 audit). **Exit evidence:** a qualified FIPS build and runtime, with setup and a lifecycle run on a FIPS-mode host.

### B332

`auth.operatorIdentity` promises the invoking operator's own SSH identity, but the session client runs as root with root's default identities, which [B283](delivered.md#x40--machine-commands-status-secrets-and-validate-refusals) documents. Running the arm's client under the invoking account, with its default identities and `-l` defaulting to that account, would build on [B282](delivered.md#x39--untrusted-input-trust-and-privilege-boundaries)'s helper (found by the 2026-10-05 audit). **Exit evidence:** argv and session tests under the invoking account.

### B333

A DNSServer's bind address defaults to the wildcard address, which collides with any other resolver on the host; since [B299](delivered.md#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json) the pre-start check names the colliding socket. Deriving the default from the single endpoint address moves the effective state and request digests of live contexts, so it waits for a later window (found by the 2026-10-05 audit). **Exit evidence:** a normalization test deriving the bind address, and the effective-state and request goldens.

### B334

The emulated BMC runs as root, unconfined, with the libvirt socket, and serves plain HTTP with a basic-auth credential; [B301](m1.md#b301) binds the examples' BMCs to loopback. TLS from the emulator with a generated certificate its client verifies, a refusal of a non-loopback bind address once an installation's provider host must be the artifact server's placement ([B289](delivered.md#x41--lifecycle-results-the-controller-stage-installation-shapes-and-bounded-runs)), and a confined SELinux type would shrink that surface (found by the 2026-10-05 audit). **Exit evidence:** the emulator's TLS with a verifying client, the refusal and the confined type, each with tests.

### B335

No harness exercises RHEL 9.8's dnf 4.14, rpm 4.16, Python 3.9 and keyring import, so setup's dnf4 path has never run on RHEL (found by the 2026-10-05 audit); [B288](delivered.md#x41--lifecycle-results-the-controller-stage-installation-shapes-and-bounded-runs) marks RHEL 9.8 admitted but not yet run, and a run on a disposable RHEL 9.8 controller qualifies it (D60). An operator-run harness in a ubi9 container could exercise resolution, inspection and signatures. **Exit evidence:** the harness and a recorded run of it.

### B341

The plan's template-delimiter refusal also scans native passthrough content, such as a composed NMState document, overrides and a StorageCluster's service specification, so legitimate native syntax holding `{#`, such as a shell's `${#var}`, cannot be planned; and the refusal names the block's object and request path, for example every service a controller `noProxy` entry reaches or a line of a Machine's Kickstart, rather than the authored object and field the operator edits (found in X39). B244 accepted the trade-off; a reviewed escape or a per-field scope for native content is the owner's decision. **Exit evidence:** that decision with its tests, and refusals naming the authored object and field.

### B345

In a human invocation sudo's sudoers denial, such as `Sorry, user ... is not allowed to execute`, carries no `sudo: ` prefix, so the holding filter forwards it and only a JSON invocation gets the remedy naming a rule for the procfs re-execution; at a terminal the operator sees sudo's own text, while the operator guide says each refusal names which case applies (found in X39). Holding sudo's untranslated policy lines in a human noninteractive invocation, and printing one remedy line after an interactive refusal, would close it; otherwise the guide and the CLI spec say which sudo text means the rule is missing. **Exit evidence:** elevation rows in which a human invocation's policy denial ends with the mapped remedy, or the reworded guide and spec.

### B346

The libvirt guest-agent identity proof and `machine rsh` and `exec` pin `ssh-ed25519` host keys, and it is unverified whether a FIPS-mode RHEL 9.8 controller's OpenSSH verifies an explicitly pinned Ed25519 key; if it refuses, lab-rhel cannot complete on such a controller and the identity path needs the key-type work [B73](m4.md#b73) does for physical installation (found in X39). **Exit evidence:** a recorded check on the D109 controller in FIPS mode before the real-hardware run, and, if it refuses, a pinned type the controller's policy admits on that path.

### B348

Each resolution of the invoking account runs `getent` three times, and the composition resolves lazily for selection, home, UID and each opener session, although the invoking identity cannot change within one invocation; and a directory account whose GECOS field holds `:` or a line break is refused as ambiguous with no remedy that names the field (found in X39). **Exit evidence:** one resolution per process, counted by a test, and a refusal naming the passwd field the name service returned.

### B352

The invoking-account helper's root path, that is a root process opening for another account, runs only as euid 0, so `TestRootHelperOpensWithTheInvokingCredentials`, `TestRootSelectionAdapterFixture` and the opt-in privileged account fixture skip in every gate; no gate exercises a directory account or a root-squashed network home, where root's reads of helper-issued descriptors run under a squashed or machine credential; and capacity refusals were proved with a file-size limit, never a full filesystem or a quota (found in X39). Since X40 the helper also opens the key `--ssh-id-file` offers, and the copy a root client reads instead of reopening it on a root-squashed home is likewise proved only unprivileged. **Exit evidence:** those tests passing as euid 0 on a disposable host or a root-capable runner, and a recorded operator check on a controller whose operator has a directory account and a root-squashed home: `context init`, `secret set` from files, `media add --from-file` and `machine exec --ssh-id-file` under sudo, and the root-login refusal.

### B353

The planning test of every proven request path skips its package and proxy-URL-path rows now that admission refuses those values first, and its `noProxy` and bridge rows will skip once the route and bridge grammars land, so the scan is no longer exercised through them; the name-service bounds, 10 s and 64 KiB per lookup, are not pinned by `TestDocumentedBoundsMatchCode`; `TestCancellationStopsAcquisitionBeforeItOpensAnything` passes no opener, so it cannot show that none was begun; the input and secret readers keep process-credential wrappers only for their older tests; and the controller-prerequisites integration target writes its request as plain JSON, unlike the runner (found in X39). **Exit evidence:** request-reaching rows or explicit admission assertions in place of the skips, the bounds pinned, an opener that fails if begun, the wrappers removed, and the target writing the runner's document.

### B354

The architecture spec says `validate -f`, `context init` and `context update` read one input directory under the same credentials, but an unelevated `validate -f` reads with the login session's groups and the elevated commands with the account database's, which differ until the operator logs in again; the operator guide says store commands refuse on a host an earlier build manages, which holds only for a store this build cannot read; the managed-OS spec's derived-installation paragraph does not say that every Kickstart value is one token the renderer refuses otherwise; the contexts spec does not say that context init and update read under the invoking account or list the `input.read` denials; and the security spec does not name the root-squash refusal beside the private-file rule (found in X39). **Exit evidence:** each page corrected.

### B355

Two lessons have no knowledge page: pykickstart splits a file as Python's `str.splitlines` does, so vertical tab, form feed, the file, group and record separators, NEL and U+2028 and U+2029 start lines, tokenizes command lines with `shlex` comments enabled and cuts `%packages` lines at `#`; and one terminal hangup delivers SIGHUP twice to a foreground job, once from the shell and once from the kernel (found in X39). **Exit evidence:** the pages, indexed by symptom.

### B362

A Secret version stored before X40 matches its declaration only through the legacy fingerprint, which covers the absolute import path and the document index, so it stays current only while both are unchanged: the first re-import from another directory, or with the declaring documents reordered, stales it once, and `secret generate` then mints a generated Secret again (found in X40). D71 accepted that residual, and X40 rewrites no stored version. **Exit evidence:** a legacy version recorded under the provenance-free fingerprint by the first command that writes its store, and a later re-import from another directory keeping it current.

### B363

Since X40 `status` reads the operation, plan and block records of the apply each current removal removes, so a removal whose source apply lost its plan fails `status` outright, as every other reader of damaged plan records already did, while `destroy` reads only the source operation record and still decides (found in X40). The state-reconciliation spec requires that refusal; a tolerant reading would report the affected rows `unknown` and name the damaged record with the exit its refusal names. **Exit evidence:** the owner's choice, and under a tolerant reading a status golden over such a removal.

### B364

Since X40 `status` reads a keyring listing that fails as corrupt or undecryptable as the reopen will and names the binding lost, but `plan`, whose preview never reopens a binding, still presents the continuation that `apply` then refuses (found in X40, older than it). **Exit evidence:** the lost-binding golden's failing-listing variant previewing the refusal and the exits `status` names.

### B365

The environment spec says a linked add-on marker grants no exception, but the input reader refuses the whole read when a marker is hard-linked, since X40 as `input.symlink` (found in X40, older than it). **Exit evidence:** the spec and the reader agreeing, with a reader test for a hard-linked marker.

### B366

`machine list --power-status` and the power commands read lifecycle evidence once per emulated Machine rather than once per invocation; a session offered `--ssh-id-file` asks the invoking account's opener twice, once to resolve the path and once to copy the key, which under root starts two helper processes; and resource selection scans the source files once per `resources` entry (found in X40). **Exit evidence:** one evidence read per invocation, one opening per session and one scan per selection, each pinned by a counting test.

### B367

The rename `validate` offers within two edits reads oddly for short keys, so `oc` suggests `govc`; a duplicate YAML key is reported at its parent mapping's path; retired-field refusals carry the generic remedy to remove the field, though each message names its replacement; and the infrastructure-services proxy check repeats, for `proxy: {}`, the schema's refusal of an empty choice as a second diagnostic (found in X40). **Exit evidence:** a rename offered only when the distance is below the key's length, the duplicate key at its own path and a remedy per retired field, each in the refusals golden, and `proxy: {}` yielding one diagnostic.

### B368

The enrollment test double records observations without a lock while enrollment observes up to eight endpoints at once, so `TestAnUnchangedKeyIsReusedAndNothingIsWritten` fails under `-race`, and `make race` does not cover `internal/trust`; the documented-command gate reads a `--` before a payload as an unknown flag, so the lab-rhel README writes `machine exec` without it; the status golden's retired-key fixture shows a keyring the local keyring cannot hold; `TestValidateRefusalsNameObjectFieldExpectationAndRemedy` lacks the `Golden` the Go rules ask of a golden test's name, and the rows of `TestSelectionRefusalsPointAtTheEntry` sit in a golden of their own; no session-path test pins the controller Machine's divergent-pin remedy, which only `TestADivergentPinHeldByTheControllerMachineNamesTheBindingThatKeepsIt` holds through enrollment; attributing a Secret refusal drops its usage mark, which custody avoids today only by never attributing a usage refusal; and the hand-built validate fixture `internal/cli/testdata/cli-validate-failed-json.golden` still shows the `api.field` message from before X40, `field is not permitted by this schema` (found in X40). **Exit evidence:** the double locked and `make race` covering `internal/trust`, the gate admitting `--`, a fixture the keyring can produce, the golden renamed and merged, the session-path test, attribution keeping the mark under a test, and the validate fixture rebuilt from a real compile.

### B369

The CLI spec's `--power-status` paragraph still calls an emulated controller unreachable when the context does not currently own it, where power now follows the machine block; its usage-failure rule names only `cli.usage`, though a service's usage refusal such as `secret.input` also exits 2 with concise help; and one line of its privilege paragraph runs past 80 columns. The command spec's `--ssh-id-file` row does not say the key is opened as the invoking account and copied; the controller spec asks only for the next safe command where a ready readiness for a context offers `bootwright plan --context`; the secrets spec does not say that the kernel's line buffer bounds a terminal line, so a long token must be piped, once that is confirmed; the input spec's plain-string rule would write plain some strings the YAML library reads as timestamps or numbers and double-quotes; the input ceiling table names the file-count resource differently from the parser's message; the lab-baremetal README's `--username <account>` placeholder reads as a redirection when pasted; and the CLI output spec's example diagnostic shows an `api.required` for a missing `metadata.name` on an object that has a name, which no compile can produce (found in X40). **Exit evidence:** each page corrected.

### B374

Since X40 `status` reads a keyring listing that fails as corrupt or undecryptable as the reopen will and names the binding lost, but a listing that fails for any other reason, such as `secret.store.key-unavailable`, names nothing, and `status` still offers `apply` and `destroy`, which then refuse with that cause (found in X40). **Exit evidence:** a status golden over a key-unavailable listing that names the failure and offers no verb that would refuse.

### B375

A fresh destroy over a failed setup receipt is proved to decide only in the lifecycle harness, whose receipt keeps an execution definition. The execution closure refuses a receipt that has none, naming `bootwright setup`, so the setup-gated steps `status` offers may have to name setup there in place of destroy; nobody checked what a failed receipt the context store writes holds (found in X40). **Exit evidence:** a failed receipt written through the context store, with `status` and `destroy` agreeing on the step.

### B376

A ready `preflight controller` run without `--context` names no next step, such as the `context init` that follows setup: X40 offers `bootwright plan --context` for a context's readiness and deliberately nothing for the baseline (found in X40). **Exit evidence:** the owner's choice, and under it the baseline golden.

### B385

Setup has run context-free since X32, and X41 deleted its controller-binding action, yet the setup receipt still encodes, and its plan digest still covers, an always-empty `context` member, and the controller mutation keeps a context scope no production caller uses: its lease, its controller-input check and its binding comparison, and the storage contract's scoped mutation (found in X41; the receipt member was tracked under B94, which X41 delivered without it). Dropping the member moves the plan digest of every receipt, so a pending receipt's compatibility is settled first. **Exit evidence:** a receipt without the member that a pending earlier receipt still settles, the controller mutation context-free under its contract tests, and the [controller record](../contexts/controller-record.md)'s Not-yet-met line removed.

### B386

Dead code X41 left: `RuntimeInspection.Conflict` has no producer since the host runtime inspection went; the execution-bundle inspection still checks target tools inside the bundle, which no resolved definition carries; the agent installer's resolver and time-source guards repeat the empty-name filter its sort already applies; and the environment admission's check of a remote rescue image's checksum, with the environment spec's "a remote image requires its checksum", can no longer fire since D81 narrowed boot media to `local-media:` (found in X41). **Exit evidence:** each removed, with every suite and golden unchanged.

### B387

After D82 no installation request can carry the SSH placement arm, yet the request shape keeps it and the `lab-rhel-ssh-placed` request golden pins it; the installation's content digest still hashes the deleted installation tooling as the frozen literal `lorax,xorriso`; and the Anaconda role's SSH-path tooling remedy and its media proof on the placement host are dead automation (found in X41). Removing them moves the installation request version, every installation plan digest and the automation digest, so they go in a digest window. **Exit evidence:** the arm, the literal and the dead tasks removed in one digest window, with the request and plan goldens regenerated.

### B388

The custody and power confirmation ports keep `Confirm` and gain an optional `ContextConfirmer` instead of requiring `ConfirmIn`, because the composition types the shared confirmer as the contexts port; and the refusals given when no confirmer is configured, in the lifecycle, media, trust enrollment and setup, name no exact command, although production always configures one (found in X41). **Exit evidence:** one confirmer type carrying both methods through the composition, the optional path gone, and each unconfigured-confirmer refusal naming its command or removed.

### B389

X41 moved dependency resolution under the staging parent `/var/lib/bootwright-staging`, with locked and swept stages and a refusal that names a noexec mount, while the controller runner's job and scratch parents stay under `/run` and `/var/tmp` (found in X41). [B19](m1.md#b19)'s one runner may carry the move. **Exit evidence:** the controller runner's job and scratch under the staging port, with its sweep and noexec refusal tested.

### B390

At an interactive terminal a sudo refusal before `machine exec` or `machine rsh` starts, such as a failed authentication, exits 1, which a caller cannot tell from the remote command's own 1; the [CLI spec](../cli.md#machine-ssh-sessions) states this exception, and a non-interactive refusal exits 255 since X41 (found in X41). Sudo hands the child the terminal and closes descriptors above 2, so lifting it needs a start signal the unprivileged supervisor can read through sudo. **Exit evidence:** an interactive session whose sudo refuses exiting 255, in a test, or the owner's acceptance of the exception.

### B391

`plan` without `--stage` over a failed or unknown multi-Machine apply marks nothing of what the continuation starts first, because the resolve and retry markers apply only under a selection; and an interrupt of a settled apply over a completed apply prints the settled result, then `runtime.interrupted` (found in X41). **Exit evidence:** an unselected preview marking what the next apply starts first, and a settled result left out of the interrupt branch, each in a golden.

### B392

X41 applied two readings the owner did not state: D79 lists MTUs among what refuses, while X41 refuses only an MTU other than 1500, the installed system's default, which lab-rhel declares; and D75's option text names `secret.store`, while X41 keeps `secret.store.conflict`, which X40 specified for an unconfirmed Secret replacement, deletion or rotation (found in X41). **Exit evidence:** the owner's confirmation, and under another reading the code, the specs and their tests changed.

### B393

The CLI keeps its own copy of the managed-OS media-name rule, held equal by `TestTheCLIMediaNameIsTheStoreRule`, because the layout test forbids the CLI importing the managed-OS domain package, although B289's exit evidence asked the CLI to call the rule (found in X41). Re-exporting the rule from the media service, beside its byte bound, lets the CLI call it. **Exit evidence:** the CLI calling the rule through the media service, its copy and the parity test deleted.

### B394

Tests X41 left narrower than they read: `TestAuthorizationTokenAndBorrowedCredentialsRefuseBeforeAnyRead` now presents its plan before refusing; the engine's rendering of a controller Unsupported row has no test, since a lifecycle test cannot import the clients; the noexec start refusal is exercised only with an injected noexec stage; a bounded run's re-check of its directory after taking its lock, and a run opened after release, have no test; neither the binding nor the bounded read tests the shared material bound; and the sweep's record re-check under the directory lock and its scratch-name re-check reach no deterministic test (found in X41). **Exit evidence:** each covered by a test that fails when its guard is removed, and the first test renamed.

### B395

Wording X41 left: the state-reconciliation spec's status contradictions still list each lost record of an incomplete removal, though a failed removal now lists none, and its authorization paragraph says a missing token's refusal names the blocks that consume it, where it names plan steps after presenting the plan; the output spec says a bounded run's output keeps the retention bounds of an attempt's output, though the runs area keeps the newest 16 runs; the contexts and managed-OS specs could add that repeating a busy media publication re-verifies the retained image; the development guide's `make build` row could say it yields a static executable; the capacity refusal of the operation, runs and SSH-trust areas names no remedy; two comments still say every write measures the whole operation subtree; and the uncarried-network remedy could also name the NetworkConfig that declares the content (found in X41). **Exit evidence:** each page, comment and remedy corrected.

### B396

Knowledge lessons X41 left: the concurrent-block-execution page quotes the old "up to 1 step at once" wording without marking it as history; the operation-store entry page says every write measures the whole subtree; no build page says that `make build` disables cgo while `scripts/go` keeps it, because `make race` needs it; and no page explains why a binding once refused during a bounded run (found in X41). **Exit evidence:** each page updated or written.

### B397

The agent installer refuses a boot-image server placed off the controller at plan, as an object-less `lifecycle.state` outside the container-cluster refusal table, which its projection test pins empty: the cluster counterpart of D82 (found in X41). **Exit evidence:** a refusal-table row held by a table test, the refusal naming its object and field before registration.

### B411

ContainerCluster `spec.distribution.release.image` and ClusterAddon `spec.olm.catalogSource.image` digests are not lowercased, against the [API's normalization rule](../api.md), whose Not-yet-met line names this item, while X42 lowercases service image digests; and the cluster's install and media block prefixes leave a ContainerCluster name 47 and 49 bytes of the 63-byte block identity, so a longer name the API admits fails at plan as an object-less `lifecycle.state` (found in X42). **Exit evidence:** both digests lowercased by normalization with the Not-yet-met line removed, and the cluster name limit refused at `metadata.name` naming it.

### B412

A wildcard bind of one family, such as `0.0.0.0` with an IPv6 endpoint, passes X42's endpoint rule but never answers on that endpoint (found in X42). **Exit evidence:** admission refusing an IP endpoint whose family the wildcard bind does not serve, naming the bind.

### B413

A media transfer proves no sub-steps, so the [output spec](../cli/output.md)'s rule 3 gives `media add` no completion share and a stalled multi-hour download shows only the heartbeat; reporting bytes received against the announced length needs the owner to amend that rule. The media transport also keeps the default 10 MB response-header limit, although its diagnostics already bound what they quote (found in X42). **Exit evidence:** the owner's decision and, under it, a byte-progress golden; and a tighter header bound with a test.

### B414

Setup and preflight stop with `controller.unsupported` when the kernel's FIPS flag cannot be read or holds an unexpected value, as the controller spec states, although the FIPS-mode check X42 added is otherwise informational (D108) (found in X42). **Exit evidence:** such a flag reported as not verified, outside readiness, in a test.

### B415

The Environment's rescue image `os` and a MachineInstallProfile's `os` constrain one OS release with different family, version and architecture rules (found in X42). **Exit evidence:** one rule both kinds call, with a parity test.

### B416

Copies and dead code X42 left: the lifecycle, custody and machine packages keep byte-identical copies of the context store's no-selection, absent-context and missing-input refusals, held by a cross-package test, because the layout forbids them importing the contexts package, where a Workspace vocabulary package they may import would hold one copy; the substrate keeps its own bracketing, port-formatting and name-segment helpers beside the managed-service ones, and the managed-service host-port helper sits beside the capability instead of the port formatter; the input decoder keeps an always-true clause on explicit string tags; and the architecture suite pins the setup run's output bound by a literal equal to the bounded run's constant (found in X42). **Exit evidence:** each copy replaced by its one owner and the clause removed, with every suite and golden unchanged.

### B417

Tests X42 left narrower than they read: `TestOrphanAcknowledgementNeverBypassesUnreadableEvidenceOrALiveLease` now proves only that a deletion without the acknowledgement refuses unreadable evidence and that a live lease refuses under it, since the acknowledgement abandons unreadable evidence, so it is renamed together with its citation in [X10's record](delivered.md#x10--orphan-acknowledged-context-delete); three test doubles still build diagnostics in the retired refusal wording; the deletion plan's reservations have no command-level test with real reservation keys; the media store contract holds no clause for reservations, so the double and the store are not held to one; of the two tests behind the incomplete-operation update refusal, only its constructor case pins the text; a placement test still places a managed service on a Machine without a provided OS, which service admission now refuses; and two schema-bypassing fixtures still author the removed `hardware.boot` (found in X42). **Exit evidence:** each covered or corrected by a test that fails when its guard is removed, and the renamed test cited.

### B418

Wording X42 left: the contexts spec's bounds table lacks the 512-byte media origin and the 30-second connection, 60-second response and six-hour transfer bounds the managed-OS spec states and `TestDocumentedBoundsMatchCode` pins there; the security spec's rule that redirects are disabled unless the port contract allows them could say that a media redirect always refuses; an interrupted init's remedy names "its original Context file" when it had none; the container-clusters spec's emulated BMC port refusal counts a node's position among all its provider's Machines, where allocation counts realized ones; AGENTS.md does not say that `make docs-check` now runs the controller destinations test; the bundle inspection's doc comment says a recoverable area holds only exact files, although partial ones are recoverable since X42; on standard output alone a media deletion's result follows its presentation with no blank line, as a power verb's follows its progress; the update plan shows the operator's absolute input directory, which the contexts spec's private-path rule should confirm or bar; and status and the lifecycle refusals call evidence the guard reads but that is spelled differently an unrecognized record (found in X42). **Exit evidence:** each page, comment and line corrected.

### B419

Validate diagnostics X42 left: a derived fqdn over 253 bytes, from a valid Machine name under a long Environment domain, refuses at an address the operator never wrote as an unassigned DNS contact, without naming the derivation; an unresolved `proxyRef`, `serverRef` or `registryRef` of a DNS, NTP or registry selection, and an absent artifact `serverRef`, each get two diagnostics on one field; other kinds' rules still add their own diagnostics when a Machine name is invalid; and a Machine's `network.attachmentRef` and `interfaceAttachments[].attachmentRef` take any non-empty value, although every attachment name is now a DNS label (found in X42). **Exit evidence:** each refusal once, naming what the operator wrote, in the validate refusals golden.

### B427

D97 renders configured repositories as installed-system configuration written
by the `%post`, so the Kickstart's `%packages` can no longer install from them
during the installation as the earlier install-time repository directive let it
(found in X43). **Exit evidence:** the owner's decision on install-time use of
a configured repository with GPG verification and, under it, the Kickstart
golden and the refusal or rendering it implies.

### B428

Every apply, an installed Machine's replay included, hashes the boot image and
the full DVD before it starts, although a replay that publishes nothing needs
neither the tree image's digest nor its extraction (found in X43). **Exit
evidence:** the replay either skipping the tree image's digest, with a test,
or the managed-OS spec recording the cost, once a host run measures it.

### B429

The setup guards still reach the qualified foundation through a context value
and an interface assertion: the native resolver's unsupported-platform build
has no `FoundationBuilds` stub, so the composition root assigns the port
through an assertion to keep the darwin build vetted, and a launch over a
receipt-recorded foundation names the package "as setup qualified it" and
remedies it with `dnf reinstall` without the exact build, because the guard
signature carries no builds. Native helper failures during plan-time
resolution, presence or inventory run from Go keep no run output, so only the
classified reason reaches the operator and the raw first line is discarded
(found in X43). **Exit evidence:** the stub and a typed port without the
assertion, a refusal that names the exact qualified build, and the raw line
retained in a run output, each with a test.

### B430

The managed-OS installation refuses `import-certificate` media trust unless a
delivered-key channel sets the serving certificate, so a guest-agent Machine
whose server declares a certificate refuses although the image's certificate
reference could anchor it (found in X43, older than it). **Exit evidence:**
the trust accepted over the certificate the server declares, or the refusal
naming why not, with a test.

### B431

Tests and dead code X43 left: the mutation that removes the media reader from a
plan test fails through a nil-interface panic instead of an assertion; the
media store contract holds no clause for a failed entry, so the in-memory
doubles are not held to the contextfs behaviour; the media shelf double keeps an
unused `Digest` method beside the held-image method; and the Kickstart renderer
treats a Proxy reference that does not resolve, or a managed Proxy, as no proxy
because admission refuses both first, so an admission that ever lets one
through would fetch repositories directly instead of refusing; and the controller
prerequisites argument spec types `nativeStaging` as raw because the role-spec
test does not know `int`, so the integer check lives only in the Python frozen
request and the role's tasks (found in X43). **Exit evidence:** each covered or
removed by a test that fails when its guard is removed, and `nativeStaging`
typed `int` with the role-spec test taught the type.

### B432

Wording X43 left: the libvirt evidence's refusal that a realized domain does
not carry this context's ownership, and the substrates spec's matching
sentence, do not name the case where the ownership holds but the UUID is not
the frozen one; the correction wording the controller refusals gained for the
status, redirect, integrity, storage, missing-candidate, signature, database,
transaction, postcondition, foundation and native-timeout classes was chosen by
the lane and is unreviewed under the CLI contract; and the
infrastructure-services spec's SSH-host paragraph says no SSH configuration is
consulted, while every ssh now reads the generated configuration the security
spec describes (found in X43). **Exit evidence:** each corrected or confirmed, with the golden or test that pins it.

### B433

X43 proved these only in-tree: the emulated BMC unit stopping cleanly on
SIGINT; the generated `-F` configuration with the crypto-policy include on
RHEL 9's OpenSSH and ansible-core's handling of an omitted `ca_path`; a 206
answer from the managed listener to the one-byte fetch; root writing the tree
identity into an extracted tree whose directories keep the ISO's read-only
modes; the cost of hashing a full DVD on every apply; the errata qualification
and the RPM database read on a RHEL 9.8 or Fedora 43 host, including preflight
without access to the staging parent; a classified warning from the action
plugin reaching the retained setup run output; and the native opt-in
integration targets that drive the host package manager (found in X43).
**Exit evidence:** each observed on a host, recorded in the
[acceptance ledger](../../docs/acceptance.md) with the build commit, or
withdrawn by the owner.

### B434

The OpenShift agent media request, `cluster-media-agent-v5`, encodes different
AgentConfig interfaces and `networkConfig` MAC addresses since X43 for a node
Machine whose `spec.network.overrides` add an ethernet interface or whose
template marks one `absent` or `ignore`. The request shape and decoder are
unchanged, so the version did not move, and the content digest re-plans such a
block as changed. The review's lenses split on it: the version selects the
decoder, so nothing is misread, and every such node is on a libvirt provider
whose machine request X43 moved anyway (found in X43). **Exit evidence:** a
decision in the state-reconciliation spec's request-versions rule on whether a
version must also move for derived-content changes within one shape and, if it
must, `cluster-media-agent-v6` with its golden in a digest-window slice, and a
test that pins an override-added interface in the AgentConfig.

### B438

A refused record carries a phase, a reason and one port, so for a wildcard
bind, the DNSServer default (D114), Go names the port and the bind that covers
it and points to the retained run output, which lists every socket found,
instead of the colliding address itself (found in X24's review). **Exit
evidence:** one bounded address member in the refused record, or one refusal
key per address and port, so the refusal names the socket without the run
output, with the infrastructure-services spec and the foreign-listener refusal
test updated.

### B439

The pre-start foreign-listener check covers a Proxy, a DNSServer, an NTPServer
and an ArtifactServer, but the API page's wildcard-default note also names a
Registry's `0.0.0.0` default, which no capability realizes yet, and the
multidc-platform example holds its DNS, NTP and proxy components external, so
none declares the explicit `bindAddress` D114 asks of a managed one (found in
X24). **Exit evidence:** the check in a realized Registry's role, or the note
narrowed to the kinds that have it, and a `bindAddress` in any example that
makes one of those components managed, each with a test or a doc check.

### B441

Two canonical-JSON proofs stay allowlisted in the controller's prerequisites
(the action match and the native preparation read), the local keyring still
sizes through its own `boundedSize` and the context store through its own
`fitsJSON` wrapper over the package's `Size` (found in X24). **Exit
evidence:** each moved onto the canonical-JSON package, the allowlist and both
wrappers deleted, the format goldens unchanged, and the fitness test's
allowlist empty of them.

### B442

X24 proved these only in-tree: Squid's `shutdown_lifetime` and its denial of
the manager and of loopback, unspecified and link-local destinations on the
pinned image; the controller supervisor's termination path through a live
`ansible-playbook` and `dnf` tree; the foreign-listener refusal from a live
ansible-core run through to the runner's named refusal, for each kind; and
every capability plugin's real evidence under the decoder's strict rules (no
case-variant members, nesting of at most 16) (found in X24). The adapter
protocol, the managed-service apply and destroy and the no-failed-unit
postcondition are [B19](m1.md#b19)'s, [B20](m1.md#b20)'s and
[B270](m1.md#b270)'s own host gates. **Exit evidence:** each observed on a
host, recorded in the [acceptance ledger](../../docs/acceptance.md) with the
build commit, or withdrawn by the owner.

### B443

Tests X24 left narrower than they read: nothing runs each capability plugin's
evidence goldens through the Go decoder's strict rules; the over-4096-byte proxy URL
case cannot isolate that bound, since a host is at most 253 bytes and a longer
URL also fails on its path; no `status` row covers
the closure-mismatch refusal; the observation-failure recorder's refusal of an
empty, over-long or non-UTF-8 diagnostic code has no test, and no capability
produces one; and the canonical-JSON fitness test refuses a marshal-and-compare
inside one function but not one split across an encode helper and a compare
(found in X24). **Exit evidence:** each covered by a test that fails when its
guard is removed, or the guard removed where nothing can reach it.

### B444

Copies and dead code X24 left: the managed-service decoder's check after its
version probe is reached only by a member spelled `Version`; the lifecycle
deletion drops a context's claims although the directory removal already does on
the path where the directory exists; the controller route selection's own
bypass-grammar branch is unreachable since the shared proxy choice runs first,
and that choice's refusal names no Machine in its remedy; and the binding-proof
port still sits in its own file instead of the lifecycle contracts, which
[B302](m1.md#b302) tracks (found in X24). **Exit evidence:** each removed or
made reachable, with a test that fails when its guard is removed; the port
stays B302's.

### B445

Both runners wait for descendants that hold the adapter's standard output
before the stop path kills the process group, so such a descendant delays the
group kill by the drain, five seconds for a lifecycle run and sixty for a
controller run, which `WaitDelay` bounds. The knowledge page on worker session
isolation still says only the lifecycle supervisor signals descendants on
cancellation, although the controller supervisor now carries the same
termination handler and is signaled before a native transaction is authorized
(found in X24). **Exit evidence:** the group kill no later than the supervisor's
exit, or the bound stated in the architecture spec, and the knowledge page
corrected.

## Retired

IDs no longer issued. Where an item took one over, its line here names that
item, whose Alias cell names the old ID too. Searching an old ID over
`specs/milestones/` finds its item's Alias cell, its record in
[delivered](delivered.md), or its line here.

| Old ID | Retired on | Reason and record |
| --- | --- | --- |
| V1 | 2026-09-28 | Answered by X14: the identity is the build's trust anchor (S18) and the image is fetched through the listener with its certificate verified (S17). |
| V2 | 2026-09-28 | Folded into [B72](delivered.md#x38--the-real-host-run-of-2026-10-05)'s operator gate. |
| V3, V4 | 2026-09-28 | Folded into [B61](m3.md#b61)'s operator gate. |
| V5 | 2026-09-28 | Folded into the operator gates of B61 and B72: a row accepted before X19 lands is repeated on a build that contains it. |
| S13 | 2026-09-28 | Delivered by [X15](delivered.md#x15--installs-that-neither-strand-nor-over-report); its row was left behind. |
| T1 | 2026-09-28 | Delivered by X15 (cluster goldens) and [X17](delivered.md#x17--goldens-and-checkpoint-harnesses) (records, commands and examples); no bounded remainder was named. |
| C13 | 2026-09-28 | No milestone needs a redistributable release. |
| O9 | 2026-09-28 | It measures the repository's guidance, not the product. |
| C16 | before 2026-09 | Delivered by [M1d](delivered.md#m1d--controller-setup) and [M1e](delivered.md#m1e--lifecycle-engine-and-managed-artifact-serving); controller relocation is [B103](#b103). |
| C17 | before 2026-09 | Promoted into M1h, now [B72](delivered.md#x38--the-real-host-run-of-2026-10-05), as its host-wide media store. |
| C18 | 2026-09-28 | Delivered by `d0980fc3` (refactor(architecture): align packages with the command and port map), which removed its row from the milestones page with no note. |
| C27 | before 2026-09 | Delivered by [X3](delivered.md#x3--machine-ssh-sessions-and-host-trust). |
| R1 | 2026-09-28 | Merged into [B19](m1.md#b19) with C28, whose outcome it was. |
| M2a, M2b, M3, M4, M5, M6 | 2026-09-28 | These rows became items whose Alias cells name them; the numbers M3 to M6 now name milestones. |
| M1h, M5a, M4a, M1i | 2026-09-28 | Became [B72](delivered.md#x38--the-real-host-run-of-2026-10-05), [B73](m4.md#b73), [B61](m3.md#b61) and [B71](m3.md#b71); M-letter IDs are no longer issued. |
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
- **D56** (2026-10-05, the owner): M1 gates the owner's full bar: well-defined specs; architecture and code that follow best practices; well-structured, domain-constrained APIs with input validation; good UX and CLI output; and a CLI ready for every controller function (context, secret, media, setup, preflight, validate, render effective, status, plan, apply, destroy and the machine commands) and for creating RHEL machines on libvirt and sushy-tools. This amends D48: M1 takes the parked B94, B95, B179, B198, B200 to B202, B222 to B225, B228, B233, B234, B236, B240 to B242, B244, B246, B249, B252, B255, B257, B264, B270 to B272 and B275 to B277, and the new B278 to B302 and B336 to B338.
- **D57** (2026-10-05, the owner): B17 to B24: X23 is dropped, and X24 keeps B19 to B22, re-planned with the role and request fixes of its lanes. B17 keeps its ID for the managed-OS work area's relocation, and B303 splits from it with per-invocation scratch, disjoint namespaces and the two-concurrent-installs test; B24 keeps its ID for cross-context scoping and an attributed refusal, and B304 splits from it with the exemption of an invocation's own in-flight jobs. B18, B23, B303 and B304 move to M3, B18 recording that context reads refuse during an apply and taking B265 into its exit evidence, and the parked B195, B237, B239, B250, B251, B259, B260 and B265 join them there.
- **D58** (2026-10-05, the owner): B72: the lab-rhel ledger row of 2026-10-05, which matches B72's acceptance baseline and gate, is accepted for B72 too, so B72 leaves M4 for X38's record; the defects the run found stay M1 items.
- **D59** (2026-10-05, the owner): M1's closing operator gate is the extended lab-rhel run on a clean build of X44's landing, on a host missing at least one native root so that setup or the controller stage downloads through the Python path: setup with the new bundle, media, context, secrets, a staged apply, preflight, plan, apply, `status` with its four JSON checks, `machine list` and `--power-status`, `machine exec` and `rsh`, restart, a destroy refused and then `machine stop`, destroy, a fresh apply, a host restart, `machine start` and `stop`, destroy, `status` after the destroy, the unit and time checks and an explicit `qemu-img info --force-share` read. One owner-accepted row completes the real-host evidence of B19, B20 and B277.
- **D60** (2026-10-05, the owner): the real xFusion test follows M1, which closes without physical installation, and B73 to B78 stay in M4, B74 to B77 outside any planned slice. On the owner's explicit request of 2026-10-05, X45, the slice the session's plan called XB, runs out of sequence right after X43 and X24, in the same digest window, and delivers B73's repair and B305, the Redfish client on the owner's xFusion iBMC. A run on a disposable RHEL 9.8 controller, the lab-baremetal rehearsal and one xFusion server with a reduced first-test profile, run from a separate RHEL 9.8 controller (D109) and recorded as an observation, follow it.
- **D61** (2026-10-05, accepted by the owner from the session's recommendations): B279 and B295: `spec.libvirt.uri` is an enumeration holding `qemu:///system`, which closes the command transports escaping cannot.
- **D62** (2026-10-05, accepted by the owner from the session's recommendations): B280 and B282: an operator whose account comes from a directory service resolves through NSS with a pinned, root-owned `getent` under the same single-entry and clean-home checks, `/etc/passwd` serving only where `getent` is absent, and a `sudo -i` root shell resolves as direct root.
- **D63** (2026-10-05, accepted by the owner from the session's recommendations): B281: store-wide verification stays, but a damaged context's refusal names the context, the store-relative entry and the errno, and `context delete --name <it> --purge` runs over exactly that context; per-context isolation is parked as B325.
- **D64** (2026-10-05, accepted by the owner from the session's recommendations): B283: an installed Machine's SSH session defaults `access.ssh.addressRef` to its `ssh` address, else its install address, else its FQDN, and the mismatch refusal gains a remedy.
- **D65** (2026-10-05, accepted by the owner from the session's recommendations): B283: a new or replaced trust record whose address collides with the record of a Machine the context no longer declares replaces it in the same confirmed write, shown as a remove row.
- **D66** (2026-10-05, accepted by the owner from the session's recommendations): B283 and B287: every remedy and next step that names a command carries `--context`, and every per-object prompt names the context.
- **D67** (2026-10-05, accepted by the owner from the session's recommendations): B275: after a removal, `status` reports a shared service whose destroy is done or released as `pending`, a failed one as `failed` with the token `[FAIL]`, and a running or unknown one as `unknown`.
- **D68** (2026-10-05, accepted by the owner from the session's recommendations): B284: `status` relabels `Bound` as the bindings count and reports 0 once a completed removal finalized, so that status still reads no keyring.
- **D69** (2026-10-05, accepted by the owner from the session's recommendations): B222: over a lost binding, `status` offers `bootwright apply` first, which finalizes without reopening the binding, then the orphan-acknowledged delete.
- **D70** (2026-10-05, accepted by the owner from the session's recommendations): B284 and B300: the `machine list` and `media list` JSON rows rename `address` to `contact` and `ips` to `addresses`, with the columns CONTACT and ADDRESSES, before any consumer exists.
- **D71** (2026-10-05, accepted by the owner from the session's recommendations): B285: a Secret declaration's fingerprint drops its provenance and covers type, source and parameters only, and a stored version whose fingerprint equals the legacy one stays current.
- **D72** (2026-10-05, accepted by the owner from the session's recommendations): B285: `secret set` keeps its strict input-file rule for files carrying a value, password, token or private key, and accepts a certificate or public-key file that is a regular single-link file, not writable by others and owned by the invoker or root.
- **D73** (2026-10-05, accepted by the owner from the session's recommendations): B285: `--value-stdin` and `--password-stdin` at a terminal prompt on standard error with echo off and read one line for tokens and passwords; opaque and `dockerConfigJson` values refuse a terminal with a pipe-or-file remedy.
- **D74** (2026-10-05, accepted by the owner from the session's recommendations): B287: Bootwright's own refusals before a `machine exec` or `rsh` session opens, `trust.identity` included, exit 255, and once the session opened its own status wins over an interrupt.
- **D75** (2026-10-05, accepted by the owner from the session's recommendations): B287: a declined or non-interactive confirmation reports its consumer's existing code, with the confirmer's reason and the `--yes` remedy.
- **D76** (2026-10-05, accepted by the owner from the session's recommendations): B287: the CLI and command specs say that destroy's proofs needing the exclusive lock or a remote observation follow the prompt and still refuse before registration, and the plan names the Machines that must be stopped.
- **D77** (2026-10-05, accepted by the owner from the session's recommendations): B288: controller egress keeps a pure route grammar for endpoint and bypass at admission and selection, and the executable's limits on a managed proxy, authentication, private trust and extra capabilities refuse as controller Unsupported rows with remedies.
- **D78** (2026-10-05, accepted by the owner from the session's recommendations): B289: an installation that selects an external DNSServer or NTPServer uses its declared address with no requirement edge.
- **D79** (2026-10-05, accepted by the owner from the session's recommendations): B289 and B338: network overrides are composed, and secondary addresses, MTUs, non-install bonds and VLANs and extra routes refuse before registration on a Bootwright-installed Machine; post-install NMState convergence is B326 on M4, before B80.
- **D80** (2026-10-05, accepted by the owner from the session's recommendations): B289: DHCP-only Anaconda installations refuse at admission, and both specs say so; DHCP installation is parked as B327.
- **D81** (2026-10-05, accepted by the owner from the session's recommendations): B289: MachineImage and hosted-tree media narrow to `local-media:<name>`, and installed consumers need the `redfishVirtualMedia` endpoint.
- **D82** (2026-10-05, accepted by the owner from the session's recommendations): B289: an installation whose image or tree server is not on the controller refuses before registration, the managed-OS spec is corrected and the unused install tooling deleted.
- **D83** (2026-10-05, accepted by the owner from the session's recommendations): B249: a Machine and a MachineInstallProfile of one name that publish through one server refuse at admission.
- **D84** (2026-10-05, accepted by the owner from the session's recommendations): B225: the runs area keeps the newest runs per context, removing the oldest before a new one, as D42 does for setup runs.
- **D85** (2026-10-05, accepted by the owner from the session's recommendations): B104 stays parked for bounded lifetime allocation, its reason corrected to state that every bounded run consumed a reservation; the narrow fix, in which `machine` power, `rsh` and `exec` read their material in one keyring session and publish no durable binding, splits out as B337 in M1.
- **D86** (2026-10-05, accepted by the owner from the session's recommendations): B290: media URLs are HTTPS only, in validation, the acquirer, help and the specs.
- **D87** (2026-10-05, accepted by the owner from the session's recommendations): B291: an identical `context update` over a completed apply keeps the selected revision, reports it unchanged and asks nothing, and changed input over a completed apply warns before the prompt.
- **D88** (2026-10-05, accepted by the owner from the session's recommendations): B291: an orphan-acknowledged deletion names `bootwright status --context <name>` as its inventory in the refusal and the prompt, and the specs narrow to that; a guard port that lists the owned blocks is parked as B324.
- **D89** (2026-10-05, accepted by the owner from the session's recommendations): B236: `--allow-orphans` may abandon a context whose evidence is unreadable or differently spelled, saying that its objects cannot be listed, and the refusal names that exit.
- **D90** (2026-10-05, accepted by the owner from the session's recommendations): B292: M1 ships the remedies and the corrected spec sentence for a 16-area bound held by client areas; their retirement by `setup --purge-old-bundles` is parked as B322.
- **D91** (2026-10-05, accepted by the owner from the session's recommendations): B202: setup prints resolution warnings with the plan, before the prompt.
- **D92** (2026-10-05, accepted by the owner from the session's recommendations): B228: setup runs and bounded runs share one 4 MiB output bound.
- **D93** (2026-10-05, accepted by the owner from the session's recommendations): B271: D55's cancellation extends to a pending setup receipt below the bound that this executable cannot resume, including one whose native action refused before authorizing any transaction.
- **D94** (2026-10-05, accepted by the owner from the session's recommendations): B294: the dead InfraProvider and Machine fields leave v1alpha1: the libvirt provider's `bmcEmulationDefaults.disableCertificateVerification` is removed; the bare-metal attachment's `vlan` is removed, coordinated with B74; bare metal no longer requires the `attachmentRef` nothing reads; `hardware.boot.nicRef` is removed; and `hardware.nics` MACs and `management.bmc` on a libvirt provider's Machines refuse at admission.
- **D95** (2026-10-05, accepted by the owner from the session's recommendations): B294: one managed attachment name or bridge on two libvirt providers of one host refuses at admission, naming both providers.
- **D96** (2026-10-05, accepted by the owner from the session's recommendations): B296: installation media is pinned as the managed-OS spec says: plan freezes the store record's size and digest, a declared checksum must match, and each attempt proves size and digest before first use.
- **D97** (2026-10-05, accepted by the owner from the session's recommendations): B296: the Kickstart renders formats with their glibc langpack, repositories as `%post` `.repo` files with `gpgcheck`, `gpgkey` and `name`, and a credential-free external proxy that exempts the artifact endpoint; `passwordAuthentication: true` refuses while `initialPassword` does, and the architecture enumerates `x86_64`.
- **D98** (2026-10-05, accepted by the owner from the session's recommendations): B17 is split: the managed-OS work area moves under a root-only 0700 parent in M1's digest window, and the rest of B17 is B303 in M3.
- **D99** (2026-10-05, accepted by the owner from the session's recommendations): B19 is not split: progress emission tasks stay in roles, one shared plugin base carries per-capability evidence functions, postcondition decisions stay in Go (D11), and B19's text and the architecture spec's Not-yet-met line are refreshed.
- **D100** (2026-10-05, accepted by the owner from the session's recommendations): B300: `version`'s `Dependency bundle` field is the embedded automation digest, computed at composition.
- **D101** (2026-10-05, accepted by the owner from the session's recommendations): B98 stays parked for pruning the narration; the amendment allowing short rationale and safety-invariant comments in the architecture spec, with a shrink-only per-package ratchet on the comment count, splits out as B336 in M1.
- **D102** (2026-10-05, accepted by the owner from the session's recommendations): B301 adds a Not-yet-met line for removals that release service socket reservations without proving the sockets free; the proof is parked as B323, for the per-capability evidence functions B19 creates.
- **D103** (2026-10-05, accepted by the owner from the session's recommendations): B73: the per-machine installer ISO is published only under the private token subtree and withdrawn after completion, the in-installer `curl --cacert` fetch stays, and private delivery requires a verified controller-to-BMC leg, because InsertMedia carries the token URL.
- **D104** (2026-10-05, accepted by the owner from the session's recommendations): B305 and B319: on the xFusion iBMC, under `established` trust, the client reads `VerifyCertificate` and `HttpsTransferCertVerification` before the insert and refuses private delivery when either reads false, the operator importing the CA out of band, within B73's repair; the manager SecurityService root-CA import follows as B319, before the first real-hardware row.
- **D105** (2026-10-05, accepted by the owner from the session's recommendations): the parked B96 to B107, B122, B161 to B167, B171, B180, B181, B193, B194, B196, B197, B199, B226, B227, B229 to B232, B235, B238, B243, B245, B247, B248, B256, B258, B261 to B263, B266 to B269, B273 and B274 stay parked.
- **D106** (2026-10-05, accepted by the owner from the session's recommendations): B288: a RHEL controller accepts `lorax` and `xorriso` the operator installed from the host's own entitled, vendor-signed repositories, proved by presence like any root; an entitled-source adapter is parked as B330.
- **D107** (2026-10-05, accepted by the owner from the session's recommendations): B292 and B297: the execution foundation keeps its byte-exact guard at every launch and gains a named check, a refusal naming package, build and file, a documented hold procedure and a regeneration tool in B292; in B297, within X43's window, setup qualifies vendor-signed glibc and libgcc within the qualified minor against the RPM database and records the proved digests as the receipt's requirement.
- **D108** (2026-10-05, accepted by the owner from the session's recommendations): B292: on a FIPS-mode controller host, Bootwright's runtime brings its own cryptography, which the docs state and preflight reports beside the host's FIPS mode; a FIPS-qualified runtime is parked as B331.
- **D109** (2026-10-05, accepted by the owner from the session's recommendations): the real-hardware test runs this build on a separate RHEL 9.8 controller in a subnet the BMC network reaches on the artifact ports, touching nothing on the owner's bastion; B280's host guidance says so.
- **D110** (2026-10-05, accepted by the owner from the session's recommendations): B301 and B73: the operator owns the controller host's firewall, and the docs list each listener's ports by source with firewalld commands; a per-socket readiness check in preflight and plan is B318, before B78.
- **D111** (2026-10-05, accepted by the owner from the session's recommendations): B282: operator-named paths are opened by a bounded helper under the invoking credentials, which passes the descriptors to root, and root keeps every proof.
- **D112** (2026-10-05, accepted by the owner from the session's recommendations): B283: the machines spec documents what `auth.operatorIdentity` does today; running its client under the invoking account is parked as B332, after B282.
- **D113** (2026-10-05, accepted by the owner from the session's recommendations): B305: generated serving certificates stay P-256; B305 records the iBMC's RSA premise with its check and, as the workaround, a `contextStore` RSA-2048 certificate set with `secret set`; a `keyType` for generation follows only if that check fails.
- **D114** (2026-10-05, accepted by the owner from the session's recommendations): B299: a DNSServer keeps its wildcard bind default, the new pre-start check names a colliding socket, and the examples and docs declare explicit binds; a derived default is parked as B333.
- **D115** (2026-10-05, accepted by the owner from the session's recommendations): B301: the examples bind their emulated BMCs to loopback, and the docs state that a privileged or cleartext-credential listener binds loopback or a host-only address unless its network is trusted; emulator TLS, a refusal of a non-loopback bind address and a confined SELinux type are parked as B334.
- **D116** (2026-10-06, the owner): every safety or defect follow-up a slice finds in M1's journeys joins M1, folded into the next fitting slice or a final sweep slice before the closing run (D59); enabling and wording follow-ups stay parked. This amends D56 and D48 for follow-ups found from X39 on: a safety or defect follow-up found in M1's journeys joins M1 on the planned sweep slice X46, or on an earlier planned slice whose lane already owns its files, with the Alias `new, <date> (<slice>); attached to M1 on <date> (D116)`; an enabling, wording, test-depth or knowledge follow-up stays parked under D48. M1 takes X39's parked safety and defect follow-ups B339, B340, B342 to B344, B347, B349 to B351 and B356 on X46.
- **D117** (2026-10-06, the owner accepted the session's recommendation): B357: D65 widens, so a confirmed trust write that takes over an endpoint also drops the record of a still-declared Machine that no longer uses the context's SSH trust, the controller Machine's included, as a `remove` row in every lifecycle state, with no input edit.
- **D118** (2026-10-06, the owner accepted the session's recommendation): B372: a file-input `secret set` replacement, `secret delete` and `secret encryption rotate` confirm before the lease and revalidate under it, as standard input does since D73, refusing with `secret.store.conflict` and writing nothing when the store changed meanwhile.
- **D119** (2026-10-07, the owner accepted the session's recommendation): B440 joins M1 on X46: a destroy over an unknown apply resolves each block with the removal's own check, which proves the object is this context's and can be taken back, so a drifted machine is removed with its ownership proved; the state-reconciliation spec records what that resolution proves.
