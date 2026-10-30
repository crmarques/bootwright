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
| [B82](#b82) | C5; moved from M4 on 2026-10-10 (D128); parked on 2026-10-11 (D129) | product | Managed OS | One more image, profile or entitlement variant | No concrete additional OS variant beyond the qualified M2 matrix |
| [B96](#b96) | Y3 | enabling | Architecture | Collection metadata and naming polish | Pruned from M1 on 2026-09-28 |
| [B97](#b97) | A8 (kind tables) | enabling | Desired state and Workspace | Kind field-table parity and a path checker | Pruned from M1 on 2026-09-28 |
| [B98](#b98) | C30; B336 split from it on 2026-10-05 (D101) | enabling | Architecture | Production comments reduced to what the clarity contract keeps | D101: M1 amends the rule, and a ratchet holds the count |
| [B100](#b100) | C2 (LoadBalancer) | product | Infrastructure services | One managed `LoadBalancer` lifecycle | No consumer; M4's MetalLB is an in-cluster add-on |
| [B101](#b101) | C6 (rest) | product | CLI | One more view of evidence or access, or a dashboard | No milestone names a view or dashboard |
| [B102](#b102) | C8 | product | Custom automation | One typed, invertible executable playbook journey | M4's packages are add-ons, not playbooks |
| [B103](#b103) | C14; B836 split from it on 2026-10-26 | safety | Workspace and Secrets | Explicit complete-store restore | No milestone needs backup and restore |
| [B104](#b104) | C15; B337 split from it on 2026-10-05 (D85) | safety | Secrets | Bounded lifetime allocation for secret reservations | D85: after B337 only applies and new versions consume the budget; waits on B103 |
| [B105](#b105) | C19 | product | Secrets | One more secret-store implementation | `local-keyring` meets the current scope |
| [B106](#b106) | M4 (day-2 surface) | product | Container cluster | The day-2 surface a completed cluster exposes | A product non-goal |
| [B107](#b107) | new, 2026-09-28 | product | Add-ons and Storage | Data Foundation over an external Ceph cluster | M4 integrates a managed Ceph cluster (D19, D128) |
| [B161](#b161) | new, 2026-09-30 (X28) | enabling | Architecture | The walk guard follows every write | Parked under the 2026-09-30 triage rule: test tooling |
| [B162](#b162) | new, 2026-09-30 (X28) | enabling | Architecture | The diagnostic registry follows every call shape | Parked under the 2026-09-30 triage rule: test tooling |
| [B163](#b163) | new, 2026-09-30 (X28) | enabling | Architecture | Test-loop wording and counts | Parked under the 2026-09-30 triage rule: tooling polish |
| [B164](#b164) | new, 2026-09-30 (X28) | enabling | Architecture | Only the privilege supervisor subscribes to signals | Parked under the 2026-09-30 triage rule: test tooling |
| [B122](#b122) | new, 2026-09-29 (X22); parked 2026-09-30 by D43 | enabling | CLI | JSON results for plan, apply and destroy | No milestone names a consumer yet (D43) |
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
| [B229](#b229) | new, 2026-09-30 (X31) | enabling | Architecture | Knowledge lessons X31 left | Parked under the 2026-09-30 triage rule: knowledge |
| [B230](#b230) | new, 2026-09-30 (X31) | enabling | Each test's owner | Tests X31 left narrower than they read | Parked under the 2026-09-30 triage rule: test depth |
| [B232](#b232) | new, 2026-09-30 (X31) | safety | State reconciliation and Secrets | Each block's execution holds only its own parts | The key stays in the operation's memory while the server shares the operation; needs a port |
| [B235](#b235) | new, 2026-09-30 (X31) | enabling | CLI and Desired state | Refusals for objects that are not API objects | Parked under the 2026-09-30 triage rule |
| [B243](#b243) | new, 2026-09-30 (X32) | safety | State reconciliation and Workspace | Room for a replacing removal and its records | Found after the M1 freeze (D48) |
| [B245](#b245) | new, 2026-09-30 (X32) | enabling | Architecture, with each role's owner | Censored completions that name only fields | Found after the M1 freeze (D48) |
| [B247](#b247) | new, 2026-09-30 (X32) | safety | Infrastructure services and Substrate | SSH host recognition and bridge readiness pairing | Found after the M1 freeze (D48); publishing SSH claims decided by D183 (Q96 a) and done with B816 |
| [B248](#b248) | new, 2026-09-30 (X32) | defect | Infrastructure services | The artifact server compares its start with its files | Found after the M1 freeze (D48) |
| [B256](#b256) | new, 2026-10-01 (X33) | enabling | Architecture | Line-limit texts for empty lists | Found after the M1 freeze (D48) |
| [B258](#b258) | split from B206 on 2026-10-01 (D52) | enabling | State reconciliation | Pruning retained operations | D52 names the existing exits; pruning needs a retention rule for evidence and audit |
| [B261](#b261) | new, 2026-10-01 (X34) | defect | Controller setup | A resumed carried-forward receipt reads its approved bytes | Found after the M1 freeze (D48) |
| [B262](#b262) | new, 2026-10-01 (X34) | safety | Controller setup | A pending receipt this executable cannot prepare refuses first | Found after the M1 freeze (D48) |
| [B266](#b266) | new, 2026-10-01 (X34) | enabling | Substrate | The drifted-network refusal names an exit that works | Found after the M1 freeze (D48) |
| [B267](#b267) | new, 2026-10-01 (X34) | enabling | State reconciliation | Exits at the retained-operation bound | Found after the M1 freeze (D48); one part needs an owner confirmation |
| [B268](#b268) | new, 2026-10-03 (X35) | enabling | Controller setup | The storage contract holds doubles to the terminal-receipt rule | Found after the M1 freeze (D48) |
| [B269](#b269) | new, 2026-10-03 (X35) | defect | Controller setup | A setup at the bound that fails after an effect reports it | Found after the M1 freeze (D48) |
| [B273](#b273) | new, 2026-10-03 (X37) | enabling | Architecture | Build and gate on Go 1.27 | Found after the M1 freeze (D48); needs an owner decision |
| [B274](#b274) | new, 2026-10-03 (X37) | enabling | Architecture | The Ansible check gate on CPython 3.14 | Found after the M1 freeze (D48); needs an owner decision |
| [B323](#b323) | new, 2026-10-05 | safety | Infrastructure services | Removals prove a service's sockets free before releasing them | D102: no failure was observed, and the proof belongs in the evidence functions B19 creates |
| [B324](#b324) | new, 2026-10-05 | enabling | State reconciliation and Workspace | An orphan-acknowledged delete lists the blocks it abandons | D88: M1 names `status --context` as the inventory |
| [B325](#b325) | new, 2026-10-05 | safety | Workspace | A damaged context is isolated from the rest of the store | D63: M1 keeps store-wide verification and names the damaged entry |
| [B327](#b327) | new, 2026-10-05 | product | Managed OS | DHCP installation | D80: M1 refuses DHCP-only installations |
| [B328](#b328) | new, 2026-10-05 | enabling | State reconciliation | The lifecycle's execution file split by concern | An optional move no M1 lane needs; D56 leaves it out of M1 |
| [B329](#b329) | new, 2026-10-05 | enabling | Architecture | A declaration-spacing check across the tree | An optional check no M1 lane needs; D56 leaves it out of M1 |
| [B331](#b331) | new, 2026-10-05 | product | Controller setup | A FIPS-qualified controller runtime | D108: M1 documents the runtime's own cryptography on a FIPS host |
| [B332](#b332) | new, 2026-10-05 | product | Machine and Controller | `auth.operatorIdentity` runs under the invoking account | D112: M1 documents what the arm does today; this needs B282's helper |
| [B334](#b334) | new, 2026-10-05; its TLS half split to B446 on 2026-10-07 (D120) | safety | Substrate | The emulated BMC refuses a non-loopback bind address and runs confined | D115: M1 binds the examples' emulated BMCs to loopback; D120 moved the TLS to B446, now in M2 |
| [B341](#b341) | new, 2026-10-05 (X39) | enabling | State reconciliation | The template-delimiter refusal names the authored field and scopes native content | Found in X39 after D56; parked under D48; the scope needs an owner decision |
| [B345](#b345) | new, 2026-10-05 (X39) | enabling | Controller, with CLI | A sudoers denial in a human invocation names its remedy | Found in X39 after D56; parked under D48 |
| [B348](#b348) | new, 2026-10-05 (X39) | enabling | Controller (privilege) | The invoking account resolves once per process, and directory data the passwd grammar refuses is named | Found in X39 after D56; parked under D48 |
| [B352](#b352) | new, 2026-10-05 (X39) | enabling | Workspace, with Controller | The invoking-account helper and real controller accounts proved as root | Found in X39 after D56; parked under D48 |
| [B353](#b353) | new, 2026-10-05 (X39) | enabling | Each test's owner | Tests X39 left narrower than they read | Found in X39 after D56; parked under D48 |
| [B354](#b354) | new, 2026-10-05 (X39) | enabling | Each spec's owner | Wording X39 left | Found in X39 after D56; parked under D48 |
| [B355](#b355) | new, 2026-10-05 (X39) | enabling | Architecture | Knowledge lessons X39 left | Found in X39 after D56; parked under D48 |
| [B362](#b362) | new, 2026-10-06 (X40) | enabling | Secrets | A Secret version stored before X40 stays current across a re-import from another directory | Found in X40; parked under D48 (D116); D71 accepted the residual |
| [B363](#b363) | new, 2026-10-06 (X40) | enabling | State reconciliation | Status diagnoses a removal whose source apply's records are unreadable | Found in X40; parked under D48 (D116); the specs require today's refusal |
| [B364](#b364) | new, 2026-10-06 (X40) | enabling | State reconciliation | A plan preview over a keyring listing that fails as corrupt names the lost binding | Found in X40; parked under D48 (D116) |
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
| [B390](#b390) | new, 2026-10-06 (X41) | enabling | Controller (privilege), with CLI | An interactive session's sudo refusal exits 255 | Found in X41; parked under D48 (D116); needs a start signal sudo lets through |
| [B391](#b391) | new, 2026-10-06 (X41) | enabling | CLI, with State reconciliation | Plan previews and interrupts X41 left | Found in X41; parked under D48 (D116) |
| [B392](#b392) | new, 2026-10-06 (X41) | enabling | Managed OS and Secrets | Two decision readings X41 applied are confirmed | Found in X41; parked under D48 (D116); needs the owner's confirmation |
| [B393](#b393) | split from B289 on 2026-10-06 (X41) | enabling | CLI, with Managed OS | The CLI calls the managed-OS media-name rule | Found in X41; parked under D48 (D116); a parity test holds the copy |
| [B394](#b394) | new, 2026-10-06 (X41) | enabling | Each test's owner | Tests X41 left narrower than they read | Found in X41; parked under D48 (D116) |
| [B395](#b395) | new, 2026-10-06 (X41) | enabling | Each spec's owner | Wording X41 left | Found in X41; parked under D48 (D116) |
| [B396](#b396) | new, 2026-10-06 (X41) | enabling | Architecture | Knowledge lessons X41 left | Found in X41; parked under D48 (D116) |
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
| [B450](#b450) | new, 2026-10-08 (X45) | enabling | Infrastructure services | The artifact server serves its private subtree only over https listeners | Found in X45; parked under D48 (D116); defense in depth, since fetching a token in clear needs a plain listener the operator declared, and it moves the automation digest |
| [B451](#b451) | new, 2026-10-08 (X45) | enabling | Managed OS, Substrate and Infrastructure services | Host observations of what X45 proved only in-tree | Found in X45; parked under D48 (D116); each needs the lab-baremetal rehearsal or real hardware, which B73's gate and the first physical firmware qualification provide |
| [B452](#b452) | new, 2026-10-08 (X45) | enabling | Each spec's and test's owner | Wording and tests X45 left narrower than they read | Found in X45; parked under D48 (D116) |
| [B460](#b460) | new, 2026-10-08 (X46) | enabling | State reconciliation | A resolution record says which check proved its evidence | Found in X46; parked under D48 (D116); it adds an optional field to the attempt record |
| [B461](#b461) | new, 2026-10-08 (X46) | enabling | Infrastructure services | The managed-service and artifact-server roles share their helpers, and Go checks the content root it decodes | Found in X46; parked under D48 (D116); defense in depth, since no input reaches either, and the role half moves the automation digest |
| [B463](#b463) | new, 2026-10-08 (X46) | enabling | Controller (privilege), Desired state and Infrastructure services | Host observations of what X46 proved only in-tree | Found in X46; parked under D48 (D116); each needs root on a host, which D59's closing run provides |
| [B464](#b464) | new, 2026-10-08 (X46) | enabling | Each spec's and test's owner | Wording, tests and dead code X46 left | Found in X46; parked under D48 (D116) |
| [B495](#b495) | new, 2026-10-12 (X48 review) | enabling | State reconciliation, with Managed OS, Substrate and Infrastructure services | The three-node lifecycle composition runs end to end with the real runner and controlled adapters | Found in X48's review; parked under D48 (D116): D125's operator gate already proves overlap on a host, and X48's tests qualify each part |
| [B503](#b503) | new, 2026-10-12 (the 2026-10-11 M2 review) | defect | Substrate and Secrets | Applying a generated BMC serving certificate proves its expiry and that its key matches | Found in the M2 review; no milestone depends on it: admission now proves the names and a fresh apply binds the stored certificate |
| [B508](#b508) | new, 2026-10-12 (the owner's M2b fixture run) | defect | Secrets, with CLI | After a refused standard-input `secret set`, `secret check` names a remedy that works | Found in the M2b run; no milestone depends on it: since b3e48bcb6 the standard-input route is refused only under a policy that logs input |
| [B510](#b510) | new, 2026-10-12 (the owner's M2b fixture run) | defect | Workspace | Every on-demand directory creation is durable for each writer that proceeds | Found reviewing B506's fix; no milestone depends on it: each window is microseconds wide and the winner's failed sync fails its own operation |
| [B521](#b521) | new, 2026-10-13 (the owner's M2b Ceph run) | product | CLI and Lifecycle | A stage-selection plan says how many blocks the invocation will run and how wide that selection is | Found in the M2b Ceph run; no milestone depends on it: the step markers are correct, only the closing summary misleads |
| [B522](#b522) | new, 2026-10-13 (the owner's M2b Ceph run) | product | Machine inventory, with CLI | A provided Machine whose blocks are done does not read as untouched in `machine list` | Found in the M2b Ceph run; no milestone depends on it: the column follows the spec, which ties the position to a Machine block that a provided Machine never has |
| [B526](#b526) | new, 2026-10-13 (X74) | defect | State reconciliation, with CLI | `plan` refuses where `apply` refuses on recorded facts | Found in X74; no milestone depends on it: X74's `Build` field is shown only for a continuation `apply` would admit, and `apply` still refuses before any effect |
| [B533](#b533) | new, 2026-10-13 (the X76 review) | defect | Storage | The Ceph observation and manager waits hold their contract at every transient edge | Found by the X76 review; no milestone depends on it: each case is transient, leaves no effect unproved and is recovered by a later apply |
| [B537](#b537) | new, 2026-10-13 (the owner's M2b Ceph run) | safety | Storage | A bootstrap that failed before making any mon store is taken back and retried instead of forcing a destroy | Found in the M2b run; no milestone depends on it: destroy recovers it, and removing cluster files needs its own safety decision |
| [B540](#b540) | new, 2026-10-13 (the X81 review) | safety | Storage | The pool block proves the pool it resumes or creates is newer than its attempt | Found by the X81 review; no milestone depends on it: both cases need an outside actor to recreate the pool during day-0 |
| [B544](#b544) | new, 2026-10-14 (X85) | defect | Machine | A key offered with `--ssh-id-file` is tried ahead of a Machine's declared password | Found in X85; no milestone depends on it: the password still opens the session, and a borrowed `--ssh-user` account takes the key |
| [B545](#b545) | new, 2026-10-14 (the X85 review) | defect | Machine, with Workspace | A power batch keeps the retained output of every run its result names | Found by the X85 review; no milestone depends on it: it needs a batch of more than 16 Machines, and every run's outcome is still reported |
| [B558](#b558) | new, 2026-10-14 (D152) | product | Controller setup | `setup --dry-run` renders the setup receipt without sudo from a readable index outside the store | Owner decision under D152's clause 9: the store stays root 0700, and the dry run lists requirement names without the receipt |
| [B559](#b559) | new, 2026-10-14 (D152) | enabling | Controller setup | The runtime lock carries no cryptography, cffi or pycparser | D152's clause 5: ansible-core declares cryptography, and no playbook smoke test has yet proved that nothing imports it |
| [B560](#b560) | new, 2026-10-14 (D152) | enabling | Controller setup | The dnf4 and dnf5 preview fixtures are real captures from RHEL 9 and Fedora 44 | D152's clause 6: the in-tree fixtures follow dnf's documented output, and the captures need the owner's hosts in X90's acceptance |
| [B561](#b561) | new, 2026-10-14 (D152) | product | Controller setup | A standalone CPython for a host whose repositories offer no locked Python ABI | D152's clause 3: every supported host offers a locked ABI |
| [B562](#b562) | new, 2026-10-14 (D152) | enabling | Controller setup | CI proposes a lock refresh when a newer ansible-core 2.21 patch or dependency appears | D152's clause 4: `outdated` is advisory and the owner regenerates the lock |
| [B563](#b563) | new, 2026-10-14 (D152) | product | Controller setup, with State reconciliation | apply rebuilds the runtime for another Python ABI after an OS upgrade without setup | D152's clause 8: a new ABI may need dnf, which only setup and the controller stage run |
| [B567](#b567) | new, 2026-10-14 (X91 audit) | safety | Substrate | Provider-host removal proves no domain is attached to a managed network before it stops the network | D155 requires the guard and B568 interim warning before further shared-host destroy; named safety slice required |
| [B568](#b568) | new, 2026-10-14 (X91 audit) | safety | Substrate, Infrastructure services and Managed OS | A context incarnation identity enters every ownership marker, so a same-name context never adopts an abandoned object | D155 accepts the persistent random incarnation design; define exact formats and migration in a named slice before implementation |
| [B569](#b569) | new, 2026-10-14 (X91 audit) | safety | State reconciliation | The operation store refuses a transition the block table does not admit | Found by the X91 audit; no path reaches it today: the scheduler is the only caller |
| [B570](#b570) | new, 2026-10-14 (X91 audit) | defect | State reconciliation | A fresh destroy records a host-free removal answer on the apply's block only after quiescence is proved | Found by the X91 audit; no milestone depends on it: it changes the D119 table and rides with the check-naming item B460 |
| [B571](#b571) | new, 2026-10-14 (X91 audit) | safety | Infrastructure services and Substrate | A retained, running physical system blocks the removal of the services it uses | Found by the X91 audit; no milestone depends on it: the operator's destroy is explicit and the contract states the quiescence rule (B70) |
| [B573](#b573) | new, 2026-10-14 (X91 audit) | safety | Storage | A Ceph remote worker ends on cancellation or disconnect, or the engine waits out its budget | Found by the X91 audit; no milestone depends on it: the bootstrap marker fences the one destructive race |
| [B576](#b576) | new, 2026-10-14 (X91 audit) | defect | Substrate | Libvirt machine evidence proves the frozen definition the spec claims, and a redefine reports changed | D155 accepts the evidence and redefine correction for a future named slice; `diff` does not compare unproved fields meanwhile |
| [B577](#b577) | new, 2026-10-14 (X91 audit) | enabling | Storage | Ceph pool and client requests stop embedding the cluster request | Found by the X91 audit; no milestone depends on it: adding one pool already moves no sibling's digest; a prerequisite of the first Ceph delta scenario |
| [B578](#b578) | new, 2026-10-14 (X91 audit) | defect | State reconciliation and CLI | `status` lists realized objects that are no longer declared | Found by the X91 audit; no milestone depends on it: `diff` already lists them and `apply` refuses before any effect |
| [B579](#b579) | new, 2026-10-14 (X91 audit) | defect | State reconciliation | A stage refusal for an unselected failed block is decided the same whether or not a resolution ran | Found by the X91 audit; no milestone depends on it: nothing outside the selection starts either way |
| [B580](#b580) | new, 2026-10-14 (X91 audit) | enabling | State reconciliation | One owned-bindings function replaces the three binding computations | Found by the X91 audit; no milestone depends on it: they agree while an operation holds one binding |
| [B581](#b581) | new, 2026-10-14 (X91 audit) | enabling | State reconciliation | A lost block record reads pending through one normalization | Found by the X91 audit; no milestone depends on it: the store already normalizes, so only hand-built maps reach the copies |
| [B582](#b582) | new, 2026-10-14 (X91 audit) | enabling | State reconciliation | `StartResolution` proves the attempt and parses entry names as `LastResolution` does | Found by the X91 audit; no milestone depends on it: the two parsers agree on every name the store writes |
| [B583](#b583) | new, 2026-10-14 (X91 audit) | enabling | State reconciliation | Record version literals come from one declaration per record | Found by the X91 audit; no milestone depends on it: a missed site fails closed |
| [B584](#b584) | new, 2026-10-14 (X91 audit) | enabling | State reconciliation | Goldens pin a resolution with an observation failure, an adopted attempt and `replaces` | Found by the X91 audit; no milestone depends on it: test depth |
| [B585](#b585) | new, 2026-10-14 (X91 audit) | defect | Substrate | The spec and the role agree on which libvirt disk sizes refuse | Found by the X91 audit; no milestone depends on it: the role is the safe side |
| [B586](#b586) | new, 2026-10-14 (X91 audit) | enabling | Controller setup | The controller-clients content digest comment says only what it hashes | Found by the X91 audit; no milestone depends on it: a comment |
| [B587](#b587) | new, 2026-10-14 (X91 audit) | defect | Storage | Detected Ceph pool `size` and `min_size` drift refuses without writing until a named delta admits it | D155 selects refusal without writes; X99's pool setters refuse drift of a completed `size` or `min_size` without writing; a drift test remains, and M2c's B81 extends the same records to every pool setter |
| [B588](#b588) | new, 2026-10-14 (X91 audit) | enabling | Storage and Managed OS | Image and tree observations surface decode errors as unknown with a reason | Found by the X91 audit; no milestone depends on it: the effect is the same, only the diagnostic is weaker |
| [B589](#b589) | new, 2026-10-14 (X91 audit) | enabling | Storage | Ceph core and host blocks gain an unresolved reason | Found by the X91 audit; no milestone depends on it: diagnostics only |
| [B590](#b590) | new, 2026-10-14 (X91 audit) | enabling | Storage | Ceph wires its image dependency one way | Found by the X91 audit; no milestone depends on it: the edges are correct today |
| [B591](#b591) | new, 2026-10-14 (X91 audit) | enabling | State reconciliation | `status` and `Ownership` share one per-object derivation | Found by the X91 audit; no milestone depends on it: the two views disagree only for a released object |
| [B593](#b593) | new, 2026-10-14 (X91 audit) | enabling | Desired state | `RenderEffective` can keep the authored catalog and origins for a consumer that needs them | Found by the X91 audit; no consumer: `diff` takes the compile port |
| [B594](#b594) | new, 2026-10-14 (X91 audit) | enabling | Desired state | One envelope shape definition serves the decoder and the encoder | Found by the X91 audit; no milestone depends on it: drift risk only |
| [B595](#b595) | new, 2026-10-14 (X91 audit) | enabling | Storage | A Ceph refusal table test and a docs-check entry | Found by the X91 audit; no consumer until the first Ceph delta scenario |
| [B596](#b596) | new, 2026-10-14 (X91 audit); agent-install half delivered by X126 (D199) | enabling | Storage | Ceph observations run without outputs when no operation records them | Found by the X91 audit; no milestone depends on it: until then `diff --observe` reads these blocks as unknown |
| [B597](#b597) | new, 2026-10-14 (X91 audit) | defect | State reconciliation | The state table names the paused destroy row | Found by the X91 audit; no milestone depends on it: the decision (continue) does not change |
| [B598](#b598) | new, 2026-10-14 (X91 audit) | safety | State reconciliation and Workspace | Input restored byte for byte after an edit settles a completed apply | D155 accepts exact-byte restoration for a future named slice; current refusal stays |
| [B599](#b599) | new, 2026-10-14 (X91 audit) | safety | Substrate and Managed OS | Every observe task writes only inside the runner's private directories | Found by the X91 audit; its extent is what the allowlist test finds, and the audit names two candidates |
| [B600](#b600) | new, 2026-10-14 (X91 audit) | product | Storage | `diff --observe` proves a Ceph pool's size, minimum size and role | Candidate; found by the X91 audit; one projector, promoted when an operator asks for it |
| [B601](#b601) | new, 2026-10-14 (X91 audit) | product | Substrate | `diff --observe` names a bare-metal Machine's hardware NICs and the BMC address that answered | Candidate; found by the X91 audit; one projector, promoted when an operator asks for it |
| [B602](#b602) | new, 2026-10-14 (X91 audit) | product | Infrastructure services | `diff --observe` names a managed service's and the artifact server's bind address, ports and listeners | Candidate; found by the X91 audit; one projector, promoted when an operator asks for it |
| [B603](#b603) | new, 2026-10-14 (X91 audit) | product | Managed OS | `diff --observe` names an OS tree's and an install's address | Candidate; found by the X91 audit; one projector, promoted when an operator asks for it |
| [B604](#b604) | new, 2026-10-14 (X91 audit) | product | Container cluster | `diff --observe` names the agent install's release version and declared nodes | Candidate; found by the X91 audit; one projector, promoted when an operator asks for it |
| [B605](#b605) | new, 2026-10-14 (X91 audit) | product | Storage | `diff --observe` names a Ceph image's, host's and core's image, topology nodes and labels | Candidate; found by the X91 audit; one projector, promoted when an operator asks for it |
| [B606](#b606) | new, 2026-10-14 (X91 review) | enabling | Substrate | A libvirt machine whose only difference is a disk size reads partial under observation, so a hand-resized disk beside a running controller projects | Found in X91's review; no milestone depends on it: the resize reads unknown with the size refusal as its reason, and `diff --observe` shows no hunk for it |
| [B609](#b609) | new, 2026-10-15 (X93) | safety | CLI | A confirmation is never asked before the plan it confirms has been written | Found in the owner run; `--yes` avoids it; parked under D48 |
| [B610](#b610) | new, 2026-10-15 (X93) | product | Substrate | `diff --observe` shows a managed libvirt network that is not active | Found in the owner run; a projector for the provider host, promoted with B576 |
| [B611](#b611) | new, 2026-10-15 (X93) | enabling | CLI | A check list written to a non-terminal names each row once | Found in the owner run; display only |
| [B612](#b612) | new, 2026-10-15 (X93) | defect | Controller setup | The core-package note says install, not update, for a package the host lacks | Found in the owner run; wording only |
| [B616](#b616) | new, 2026-10-16 | defect | Architecture and developer tooling | Source checks exclude unreadable ignored non-source directories | Found during integration verification; test tooling, no milestone gate |
| [B617](#b617) | new, 2026-10-17 | defect | Storage | Registry socket publication serialized with close | Pre-existing connector race; separate cancellation follow-up, no new milestone gate |
| [B624](#b624) | new, 2026-10-18 (D159; found by the X55 checkpoint) | enabling | CLI and Managed OS | `diff --observe` shows a Machine's bounded network differences | Display only; the runbook reads the drifted value from the guest, and a projector is promoted with B576 |
| [B630](#b630) | new, 2026-10-19 (X101) | safety | Storage | A proved-absent native rejection leaves a bootstrap or `set-user` attempt recoverable instead of `partial-cluster` | Found in X101; narrowing a safety refusal needs its own named slice and decision; X101's argv check before the attempt record already covers the bootstrap options |
| [B631](#b631) | new, 2026-10-19 (X101) | enabling | Storage | A wrong `spec.distribution.ibm.callHome` for the selected build is refused before any host block runs | Found in X101; Go has no planning-visible Call Home model fact, and D157 forbids gating on the release |
| [B632](#b632) | new, 2026-10-19 (X101) | enabling | Storage | The unused `installed_cephadm` and the tests that patch it are removed | Found in X101; dead code with no effect |
| [B633](#b633) | new, 2026-10-19 (X101); widened 2026-10-19 (X102) | defect | Architecture and developer tooling | Two unit tests stop failing under load: the native-timeout test's xdist worker and the libvirt firewall readiness replay case | Found in X101 and X102; each file passes alone and the whole tree passed on rerun |
| [B634](#b634) | new, 2026-10-19 (X101) | enabling | Storage | The libvirt three-node Ceph runbook validates the operator's selected coordinates, not only the tracked ones | Found in X101; documentation for the owner's runs, no gate |
| [B635](#b635) | new, 2026-10-19 (X101) | enabling | Storage, with Container clusters | The edges X101's reviews left untested or unreconciled are tested or recorded | Found in X101; each is hardening of a path no signed build reaches today |
| [B657](#b657) | new, 2026-10-19 (X102) | safety | Storage package resolution, with Security | Plan packages from the `broker` root must be RHEL-signed and sourced from the host's DVD tree | No trust break today: every package is verified under its repository's pinned signer, and no IBM repository ships `openssl` |
| [B658](#b658) | new, 2026-10-19 (X102) | enabling | Storage preflight and State reconciliation | Cross-context capacity admission states its limit or holds a host-scoped reservation | No spec violation, a short window, and a reservation would be new durable cross-context state |
| [B660](#b660) | new, 2026-10-19 (X102); assigned to M2c lane x61-ceph-fips on 2026-10-19 (D166) | safety | Ceph host runtime route probe, with the future FIPS core profile | The route probe's synthetic keys follow the cluster key's type under FIPS | In-tree key selection and refusals implemented; operator FIPS acceptance stays with B484 |
| [B661](#b661) | new, 2026-10-19 (X102) | enabling | Ceph host runtime image profile | The cephadm payload bound is raised or derived and an oversized payload refuses with its measured size | The largest current payload is 1,676,391 bytes; a future refusal, not a defect |
| [B663](#b663) | new, 2026-10-19 (X102) | enabling | Storage adapter | Authored package candidates mark only the chosen root, keeping DNF's own no-match text | No wrong plan was observed; refusing glob characters would be a Bootwright version grammar |
| [B664](#b664) | new, 2026-10-19 (X102); assigned to M2c lane x59-d4-host-pull-route on 2026-10-19 (D166); field decided on 2026-10-20 (D169) | product | the owner for scope, then Storage adapter | Mirror or disconnected IBM package sources are decided, with the publisher key location authored beside them | Fail-closed today; D166 decided the scope and assigns the fix to M2c lane x59-d4-host-pull-route, and D169 decided the field `ceph.packageRepositoryKey` that lane adds |
| [B665](#b665) | new, 2026-10-19 (X103) | enabling | Developer tooling, with the code-implementation skill | Commits that change a trust anchor say so; `scripts/check-commits` resolves `Refs:` tokens, the subject length and attribution trailers; policy changes land apart from reformats | Process tooling; history stays as it is |
| [B666](#b666) | new, 2026-10-19 (X103) | enabling | Infrastructure, substrate libvirt | The firewall readiness barrier reloads systemd only when something changed, and destroy removes its stale staging files | Each leftover is inert and the reload is harmless; fixing it adds durable creation state for no behavioural gain |
| [B667](#b667) | new, 2026-10-19 (X103) | enabling | CLI presentation and help | Compact `cluster info` explains not-applicable access rows once, and `session` and `exec` help list the SSH flags | The output matches the spec today and the "More detail" pointer keeps the flags discoverable |
| [B668](#b668) | new, 2026-10-19 (X103) | enabling | Privilege boundary and CLI | A refusal remedy repeats only what the operator typed, not the `--color=never` the privilege boundary adds | Cosmetic (the repeated command is valid), and the fix changes the privilege invocation contract |
| [B669](#b669) | new, 2026-10-19 (X103) | enabling | Build and CLI | The CLI depends only on a width library, measured before styling | Documented and free of known vulnerabilities; no defect |
| [B670](#b670) | new, 2026-10-19 (X103) | safety | CLI and Secrets | Structured exports are encoded into an owned buffer cleared after the write, or documented as transient copies | No spec requires clearing output copies, and Go strings make complete clearing impossible without a custom encoder |
| [B671](#b671) | new, 2026-10-19 (X103) | enabling | Infrastructure components and `cmd/bootwright` tests | A plan test pins the block set of the `lab-services` apply and destroy plans | Test hardening; no defect shown |
| [B672](#b672) | new, 2026-10-19 (X103) | enabling | Developer tooling | `make docs-check` runs the pinned yamllint over tracked example YAML, or the formatting reference records the omission | Gate hardening; no example fails today |
| [B673](#b673) | new, 2026-10-19 (X103) | defect | Workspace contextfs tests | `TestAnInterruptedPublicationLeavesAUsableStoreAndItsRetryConverges` is deterministic under concurrent gate runs, or its cause is recorded | It does not reproduce cheaply |
| [B674](#b674) | new, 2026-10-19 (X102) | safety | Ceph host lifecycle, with B491 | A release whose cluster block never started removes the owned host runtime tree and any broker scratch | A new destructive host effect needing its own removal proof |
| [B675](#b675) | new, 2026-10-19 (X102) | product | Storage, Ceph Call Home | `spec.distribution.ibm.callHome: disabled` passes the opt-out flag when the selected build declares it | Keyed on artifact facts, not a version label; a feature gap outside a defect slice |
| [B676](#b676) | new, 2026-10-19 (X103) | product | Controller | `spec.dependencyVersions.libvirt` honours a NEVRA pin through host `dnf`, or the field leaves the grammar | Implementation scope, not a compatibility rule |
| [B677](#b677) | new, 2026-10-19 (X102) | enabling | Storage | The misnamed `unsupported-release` Ceph refusal token is renamed `unsupported-coordinates` | Cosmetic, and it collides with the closed reason lists the m2c lanes edit |
| [B678](#b678) | new, 2026-10-19 (X102) | defect | Storage package resolution | A non-rhel host refuses `package-platform` naming the os-release ID, not a generic `package-resolution` | Fails closed before any effect; low value |
| [B679](#b679) | new, 2026-10-19 (X102) | enabling | Storage and State reconciliation | The edges X102's lanes and reviews left untested, unmeasured or unreconciled are tested or recorded | Found in X102; each is hardening of a path no known input reaches |
| [B681](#b681) | new, 2026-10-19 (X103) | enabling | CLI, Substrate and Docs | The edges X103's lanes and reviews left untested, unmeasured or unreconciled are tested or recorded | Found in X103; each is hardening of output or removal paths no known input breaks |
| [B682](#b682) | new, 2026-10-20 (X57; parked by D160, OD7, kept by D161) | product | Storage and Secrets | A StorageCluster binds the direct MGR dashboard's certificate | The management gateway fronts the dashboard with a bound certificate; X70's self-signed dashboard certificate stays |
| [B683](#b683) | new, 2026-10-20 (X57; parked by D160, OD7, kept by D161) | product | Storage and Secrets | Declared dashboard roles and local users with protected passwords | No approved API; SSO grants only system roles, and X70's administrator stays |
| [B684](#b684) | new, 2026-10-20 (X57) | product | Storage | The manager prometheus exporter serves external scrapers without a Prometheus service | No consumer; without a declared service the module would have no owner, so `spec.ceph.mgrModules` refuses it |
| [B685](#b685) | new, 2026-10-20 (X57; parked by D160, OD26) | product | Storage | Monitoring and gateway certificates that cephadm signs or that a certificate source selects, with the cephadm root CA reported | Bound certificates cover the gateway; cephadm-signed sources and root-CA retrieval have no documented native contract |
| [B736](#b736) | new, 2026-10-20 (D172) | product | Infrastructure services and Controller | Bootwright routes the controller's resolver to the managed zones it serves | [Q3](m3.md#m3a-open-decisions) keeps the route an operator prerequisite proved by preflight; a host-wide resolver mutation needs an ownership, lease and cross-context design no M3 profile requires |
| [B737](#b737) | new, 2026-10-20 (D172) | product | Container cluster | Two-node clusters with fencing | The 4.20 and 4.21 agent installers refused it and 4.22 accepts it; fencing credentials enter the image; no consumer |
| [B738](#b738) | new, 2026-10-20 (D172) | product | Container cluster, with Substrate | Dual-stack clusters | One address family per cluster is the contract; per-family VIP lists, endpoints and managed networks are unspecified |
| [B739](#b739) | new, 2026-10-20 (D172) | product | Container cluster | Workers added to an installed cluster by `oc adm node-image` | A day-2 change over a completed apply (D153); the any-size block covers sizes the ceiling admits |
| [B740](#b740) | new, 2026-10-20 (D172) | product | Container cluster | Install-time OVS or Linux bridges and a customized `br-ex` on cluster nodes | Unproven in agent `networkConfig`; needs a live test |
| [B741](#b741) | new, 2026-10-20 (D172) | product | Container cluster | Linux-bridge NADs and overlay UDNs as secondary networks | M3d's external networks are localnet; neither is needed by its profiles, pending the owner's [Q24](m3.md#m3a-open-decisions) |
| [B742](#b742) | new, 2026-10-20 (D172) | product | Container cluster | Tang disk encryption | The API admits TPM2 only; Tang needs reachable servers and thumbprints |
| [B743](#b743) | new, 2026-10-20 (D172) | product | Container cluster and Substrate | One virtual cluster across several libvirt hosts | An emulated controller fetches the private image without verifying its server |
| [B744](#b744) | new, 2026-10-20 (D172) | product | Add-ons | MetalLB BGP advertisements | M3d's pools are L2; BGP needs a router and one ASN per cluster |
| [B745](#b745) | new, 2026-10-20 (D172) | product | Substrate | A Redfish facade realizes KubeVirt Machines | The 2026-06-28 owner decision keeps `virtctl image-upload`; vSphere needs the port anyway; the facade needs host-wide feature gates |
| [B746](#b746) | new, 2026-10-20 (D172) | enabling | Substrate | One staged image per cluster on push arms | Per-node pushes keep ownership per Machine; it needs evidence from M3e or M3f runs that per-node pushes are the bottleneck |
| [B747](#b747) | new, 2026-10-20 (D172) | product | Container cluster and Substrate | A cluster whose nodes span KubeVirt or vSphere and another arm | No run qualifies it, and the media block would both publish and push; it needs B88 or B733 and a qualifying run |
| [B748](#b748) | new, 2026-10-20 (D172) | product | Container cluster | OKD clusters through the agent installer | M3's coverage is OpenShift; OKD's release, client and image sources need their own definition, and its refusal row stays |
| [B769](#b769) | new, 2026-10-20 (D173) | product | Add-ons | One add-on instance removed online from a cluster that stays | M4 retires instances with their cluster; [D153](#d153) refuses changing a completed apply's selection |
| [B770](#b770) | new, 2026-10-20 (D173) | product | Add-ons | `preflight add-ons` reports add-on prerequisites before an apply | No check is provable before the cluster exists; apply-time gates name their failures |
| [B988](#b988) | new, 2026-10-30 (D210; audit 002 F11) | product | Add-ons, with API | A `ClusterAddonProfile` carries built-in packages as a named preset | No repeated consumer; Q26 (a) chose profiles without packages |
| [B990](#b990) | new, 2026-10-30 (D211; B90 first slice) | product | Add-ons | An authored add-on creates a cluster-scoped custom resource once a program check proves the kind's scope | No consumer; the first slice admits only namespaced resources in the add-on's own namespace |
| [B771](#b771) | new, 2026-10-20 (D173); folds B365 | defect | Add-ons, with Workspace | The `add-ons/_store` selection exception, its marker and its reader rules leave the input reader | Dead surface now that B89 is retired; removing input-reader rules is not needed by any M4 package |
| [B772](#b772) | new, 2026-10-20 (D173) | product | Add-ons | A package installed through OLM v1 (`ClusterExtension`) | ODF/FDF, ACM, MCE and GitLab do not qualify; own-namespace installs are Technology Preview |
| [B773](#b773) | new, 2026-10-20 (D173) | product | Add-ons | A registry credential merged into a cluster's global pull secret with an exact-entry receipt | Its inverse never runs under retirement; the install pull secret carries entitlements from first boot |
| [B774](#b774) | new, 2026-10-20 (D173) | safety | Container cluster, with Add-ons | Add-on work uses a scoped cluster identity instead of the 10-year administrator certificate | Needs an identity minted and revoked per operation; [B64](delivered.md#b64) lends the installer's kubeconfig today |
| [B775](#b775) | new, 2026-10-20 (D173) | product | Add-ons | NMState's probe host on a disconnected cluster, as package configuration | M3 refuses disconnected clusters |
| [B776](#b776) | new, 2026-10-20 (D173) | product | Add-ons | A MetalLB pool on a secondary interface forwarded by a per-interface NNCP instead of the cluster's `ipForwarding: Global` | The cluster-wide setting serves every documented release; per-interface NNCP forwarding is documented only from 4.22 and needs the `nmstate` package |
| [B777](#b777) | new, 2026-10-20 (D173) | safety | Add-ons | MetalLB pools of two clusters on one layer 2 network refused when they overlap | Needs admission across add-on instances, which the package interface does not offer; no profile shares a network between two clusters' pools |
| [B778](#b778) | new, 2026-10-20 (D173) | product | Add-ons, with Container cluster | Data Foundation on a disconnected cluster, its mirror set derived from the selected bundles' related images | M3 refuses disconnected clusters |
| [B779](#b779) | new, 2026-10-20 (D173) | product | Add-ons and Storage | In-transit encryption between a Data Foundation cluster and its Ceph cluster | The Ceph-side secure messenger modes and daemon restarts are unverified and no M4 profile needs them |
| [B780](#b780) | new, 2026-10-20 (D173) | product | Add-ons, with Container cluster | The Data Foundation console plugin enabled on the cluster console | Enabling it patches the cluster-owned console operator configuration, which no package may touch; readiness does not need it |
| [B781](#b781) | new, 2026-10-20 (D173) | product | Add-ons, with Container cluster | An `acm` hub imports other Bootwright-managed clusters of the context as managed clusters | Import places one cluster's administrator credential in another cluster, which the Secrets contract refuses; managed clusters block the hub's deletion; fleet management is day-2 |
| [B782](#b782) | new, 2026-10-20 (D173) | product | Add-ons | Managed clusters registered with the hub's Argo CD through `GitOpsCluster`, `Placement` and a cluster set binding | Needs [B781](#b781)'s managed clusters; it is fleet GitOps wiring, outside bootstrap |
| [B783](#b783) | new, 2026-10-20 (D173) | product | Add-ons | GitOps installed without its default instance, or with a Bootwright-owned Argo CD instance whose source namespaces, trust and placement Bootwright declares | No M4 journey needs it; disabling the default later deletes it, so the choice is creation-time only |
| [B784](#b784) | new, 2026-10-20 (D173) | product | Add-ons | A bootstrap repository reached over SSH or behind a private certificate authority, and an `ApplicationSet` root | Known hosts and repository CAs live in operator-managed ConfigMaps Bootwright never patches; needs [B783](#b783)'s owned instance or an operator-supported creation-time field |
| [B785](#b785) | new, 2026-10-20 (D173) | product | Add-ons | ACM component toggles (`spec.overrides.components`) as `acm` configuration | The hub operator may rewrite the list it is given, which create-only comparison would read as a difference on every continuation; needs a keyed list comparison or proof it does not |
| [B786](#b786) | new, 2026-10-20 (D173) | product | Add-ons | GitLab installed from the operator's OpenShift release manifest supplied by the operator | Needs [B90](m4.md#b90)'s frozen manifest-set payloads; embedding a manifest in the executable would register an operator version (D163) |
| [B787](#b787) | new, 2026-10-20 (D173) | product | Storage, with Add-ons and Secrets | GitLab's object-storage documents composed by Bootwright from a StorageObjectUser's produced keys | Needs a closed document format in the Secret placement beside [B760](m4.md#b760)'s produced placement; the operator-supplied documents serve M4f |
| [B788](#b788) | new, 2026-10-20 (D173) | product | Add-ons | Git over SSH to GitLab through a Gateway API TCPRoute with an explicit controller and a LoadBalancer address | Routes cannot carry SSH; needs a Gateway controller package and MetalLB on the cluster |
| [B789](#b789) | new, 2026-10-20 (D173) | product | Add-ons | PostgreSQL and Redis or Valkey for GitLab from in-cluster operator packages | GitLab calls in-cluster stateful services non-production; no verified package research |
| [B790](#b790) | new, 2026-10-20 (D173) | product | Add-ons | GitLab Community Edition through the operator | CE through the operator is undocumented and unverified |
| [B791](#b791) | new, 2026-10-20 (D173) | product | Add-ons | GitLab on a disconnected cluster | M3 refuses disconnected clusters; the bundle declares itself not disconnected-capable |
| [B792](#b792) | new, 2026-10-20 (D173) | product | Add-ons | GitLab Runner through the GitLab Runner Operator as its own package | The GitLab Operator installs no Runner; no M4 journey needs CI execution |
| [B793](#b793) | new, 2026-10-20 (D173) | product | Add-ons, with Secrets | GitLab backups to object storage and a declared Rails secret, so external data survives a cluster rebuild | Chart keys unverified; backup content is day-2 data custody |
| [B794](#b794) | new, 2026-10-20 (D173) | product | Add-ons | A program check arm that probes an HTTP endpoint, such as GitLab's readiness path through its Route | A new check arm moves the program version; no M4 package needs it to exit |
| [B795](#b795) | new, 2026-10-20 (D173) | product | Add-ons, with Secrets | GitLab trusts a declared CA bundle toward its object storage, PostgreSQL and Redis | The chart's custom-CA and TLS keys are unverified; the profile reaches its services without a private CA |
| [B797](#b797) | new, 2026-10-21 (X56) | product | Storage | A StorageObjectBucket's declared policy changes after completion | A completed bucket refuses `object-bucket-identity` on a changed policy; a day-2 update is a state-contract change no milestone needs |
| [B798](#b798) | new, 2026-10-23 (M3 verification) | defect | Verification tooling | The full Ansible unit gate selects its intended worker mode explicitly | Pre-existing tooling behavior outside the requested product change |
| [B799](#b799) | new, 2026-10-23 (M3 verification) | defect | Workspace | Setup interruption retries identify any remaining setup-lock holder | Pre-existing intermittent refusal; its cause is unproved |
| [B857](#b857) | new, 2026-10-27 (lane I) | enabling | Desired state | The audit 5.7 operator tasks run on the reshaped API | B814's paper check was not run before X171 |
| [B867](#b867) | new, 2026-10-27 (lane R review) | safety | Storage | The Ceph incarnation rule is confirmed on a live cluster | The monitor map's creation time was chosen without a live run |
| [B881](#b881) | new, 2026-10-28 (X190 review) | safety | State reconciliation, with Substrate | A cancelled or deadline-ended firmware power cycle still forces its machine off | The adapter supervisor kills the worker with no grace; a grace changes every adapter's termination |
| [B882](#b882) | new, 2026-10-28 (owner request: the e2e stretch case); narrowed 2026-10-29 | product | Storage, with Machine and Managed OS | A provided-OS Machine as a Ceph data, manager or gateway host | Planning admits a provided Machine only as the stretch tiebreaker (D204) |
| [B905](#b905) | new, 2026-10-28 (D197) | product | Substrate and Managed OS | A Bootwright-installed operating system on a KubeVirt Machine | B87 realizes installer-provisioned Machines; an installation needs an identity channel and seeds the arm does not define |
| [B906](#b906) | new, 2026-10-28 (D197) | product | Substrate, with Add-ons | A KubeVirt profile's persistent vTPM state is proved migratable on a declared state class | The current target declares no vTPM; the state class is host-wide and set on HyperConverged |
| [B907](#b907) | new, 2026-10-28 (D197) | product | Substrate | KubeVirt Machines boot with EFI Secure Boot | Whether the agent image boots under KubeVirt Secure Boot is unverified |
| [B909](#b909) | new, 2026-10-28 (D197) | product | Substrate | KubeVirt interfaces on a namespaced UserDefinedNetwork | The arm realizes Localnet ClusterUserDefinedNetwork and bridge NetworkAttachmentDefinition attachments; no profile needs a UDN |
| [B925](#b925) | new, 2026-10-28 (D201) | safety | Substrate and Container cluster | A released KubeVirt node's image claim is ejected live where the host enables declarative hotplug | The eject leaves the claim until the node next stops, which security records; live removal is an optional specialization no profile needs yet |
| [B927](#b927) | new, 2026-10-28 (D201) | product | Container cluster and Substrate | An installer VIP on a KubeVirt attachment is proved free before a fresh boot | The pre-boot ARP probe reaches only a managed libvirt bridge on the controller; a KubeVirt attachment is reported unprobed and the operator proves its VIPs free |
| [B965](#b965) | new, 2026-10-29 (X220) | enabling | Specification maintenance | The ten section-budget exceptions and the leaf-budget exceptions of the spec pages are split into leaves within budget | The split is large documentation work with no consumer; the docs gate holds each ceiling falling in the meantime |
| [B966](#b966) | new, 2026-10-29 (X220) | enabling | Ansible collection | The role and plugin naming misfits are renamed to the pattern | The rename moves the automation digest, so it rides a build and setup; the allowance in `test_collection_names.py` only shrinks in the meantime |
| [B967](#b967) | new, 2026-10-29 (X220) | enabling | Verification tooling | The selection logic of `scripts/quick-test` and `scripts/check-commits` moves behind `scripts/tools` launchers | Both scripts are named shell exceptions that work; no gate fails until the move |
| [B968](#b968) | new, 2026-10-29 (X216) | enabling | CLI | CLI help and output name cluster and storage kinds from the family registry, and the family-neutral check covers `internal/cli` | About 24 help and output strings name kinds directly and the help goldens pin them; the check exempts `internal/cli` meanwhile |
| [B969](#b969) | new, 2026-10-29 (X216) | enabling | Storage | The six storage requirements no example plans become reference-edge rows | `CephClient`, `CephNFSExport` and `CephSMBResource` `filesystemRef` and the three `CephObjectGateway` `spec.pools` references order blocks in code, but the catalog test needs a planned example per row, so they carry a reason meanwhile |

### B82

One additional managed-OS image, profile or entitlement variant beyond the
install arms [B74](m1.md#b74) to [B78](m1.md#b78) carry. No concrete consumer is named
yet; [B80](m2.md#b80)'s definition may supply one. **Exit evidence:** the intent gap, a deliberate API revision, renderer
and executor parity and qualification.

**Requires:** a concrete consumer beyond M2's qualified host arms.
**Definition:** Candidate. Parked on 2026-10-11 (D129); no extra OS variant is
required merely to close M2.

### B96

Collection metadata and naming polish, including the collection's sanity
README, whose `missing-gplv3-license` note names only documentation stubs
although the ignore file also exempts the real modules with no action plugin
(found in X19). **Exit evidence:** `./scripts/ansible-check`.

The 2026-10-11 M2b acquisition check also exposes pre-existing collection-wide
sanity failures in `no-assert`, Python formatting/lint, shebang and module
metadata checks. Correct them without changing strict input semantics or
inventing a GPL grant; narrowly documented licensing exceptions preserve the
existing licensing status. Targeted units/lint do not count as a full sanity
pass.

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
[B336](delivered.md#x44--help-completion-contracts-and-ratchets), whose amended rule and ratchet hold the count meanwhile
(D101). **Exit evidence:** each retained comment justified by language,
tooling or a maintained contract; every durable finding moved into
`.agents/knowledge/` with its links and evidence; and B336's ratchet lowered
to the result.

### B100

One managed `LoadBalancer` lifecycle over the shared managed-service capability
[legacy network services](delivered.md#m1f--managed-controller-network-services) delivered; its `Registry` half is [B68](m1.md#b68). **Exit evidence:** a
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
not expand [legacy CLI availability](delivered.md#m1a--complete-cli-skeleton).

Narrowed on 2026-10-12 (D143 clause 9, pending the owner's review): `cluster
list` and `cluster info` (B519,
[X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply)) and
managed-Ceph `cluster rsh` and `cluster exec` (B517,
[X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply)) move to M2.
This item keeps ContainerCluster sessions, the `oc` and `kubectl` handoffs and
the remaining matrix.

### B102

One typed, invertible executable custom-playbook journey. The reserved schema
cannot prove effects, ownership or non-exfiltration; it needs [the legacy lifecycle baseline](delivered.md#m1e--lifecycle-engine-and-managed-artifact-serving), the plan and
execution journeys of [B713](delivered.md#b713) and the produced input of
[B64](delivered.md#b64) and a named journey. **Exit evidence:** a same-change API replacement of the
[reserved shape](../deferred/custom-playbooks.md), immutable source and
dependencies, exact targets, bounded secrets, authorization, continuation,
failure injection and isolated-runner qualification, and playbooks ordered by
their dependencies on blocks or API objects, never on stage anchors.

### B103

Explicit complete-store restore with logical identity preservation. Copy
restoration changes physical identities and may roll back seal reservations;
[M1c](delivered.md#m1c--context-secret-management) converts no earlier keyring
format and refuses it instead, and offers no backup or restore command.
Controller relocation, left over from C16, was split out as [B836](delivered.md#x164--audit-lane-e-store-durability-and-controller-records) on
2026-10-26. **Exit evidence:**
coherent snapshot validation, authorized inode rebinding, a fresh key before
writes after rollback, interruption and retry and wrong-store refusal tests;
lifecycle recovery evidence preserved.

**Work moved here from B816 (2026-10-27):**

- The restored seal counters of the
  [format and restore boundary](../contexts.md#format-and-restore-boundary):
  restore establishes the fresh encryption key the rule requires and detects
  rolled-back seal counters, so a keyring restored with older seal counters
  never keeps sealing under its old key with its usage counted from the older
  value; a test restores a keyring with older counters and proves a new seal
  uses a fresh key (the "fresh key before writes after rollback" test above).

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
and removal. It stays parked until a product-scope change admits it. Owned
objects are frozen after handoff ([project](../project.md#lifecycle-and-handoff)),
so the two options the 2026-10-26 audit raised (F01-02) wait here: an explicit
relinquish verb that records an ownership release for GitOps to adopt, which
amends the handoff sentence that ownership is never transferred, or a named
post-handoff owner for a class of owned object, such as an approved InstallPlan.
Either needs an owner decision. **Exit evidence:** set by that change.

### B107

OpenShift Data Foundation, through IBM Fusion Data Foundation, integrating an
OpenShift cluster with a Ceph cluster Bootwright does not manage. M4 integrates
the Ceph cluster M2 manages instead (D19, D128). **Exit evidence:** set when it is
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

[B309](delivered.md#b309) makes the installation block's tool-resolution failure deterministic; this item keeps the removal observation's resolution.

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

### B229

A knowledge page keyed on `SyntaxWarning: invalid decimal literal` for the ansible-check area name (B203), and the carry-forward page citing the test that carries a settled receipt forward (found in X31). **Exit evidence:** the pages.

X32 found the mechanism: ansible-core renders its home defaults through Jinja's native environment, whose concatenation parses the text, so any home path in which a number runs into a keyword warns in every process; CPython also warns for other number forms, such as `1_1if`, `1e1if` and `0x1fif`.

### B230

No test pins the pre-plan refusal of a pending receipt whose bundle is incompatible; the lifecycle test double's held area lets a removal skip its closed and read-only checks; and no runner test pins a partial record waiting at the drain after a failed exit, whose outcome X31 made unknown (found in X31). **Exit evidence:** each test.

### B232

Since X31 a run request lends a block only the Secret parts it writes, but each attempt's execution and quiescence probe still hand the capability's Go code the whole reopened operation map, the artifact server's key included (found in X31). **Exit evidence:** a port through which a capability declares its blocks' parts, with a test that another block's key is absent.

### B235

The lost-binding refusal names its operation and binding only in its message, because a diagnostic's object is an API object; and a refusal inherited from an Environment default gets the generic inherited-field remedy in place of its own, so a file source from a default names `secret set` only in its message (found in X31). **Exit evidence:** a diagnostic shape for such objects and inherited remedies kept.

### B243

An apply keeps room for one removal directory, so at the directory bound a failed removal's replacement refuses at registration; a removal's byte admission covers only its registration, so its records and logs can still stop it part way at the byte bound; and when the reserve cuts an attempt's output to nothing, the attempt log records nothing (found in X32). **Exit evidence:** bound tests for a replacing removal and for a removal's records, and a log entry for dropped output.

### B245

Since X32 the adapter output prints nothing a failed `no_log` task raised, so completion refusals that name only fields, never values, now print nowhere; role comments still say ansible-core keeps nothing of such a failure; and the censor overrides a private ansible-core method that a move past 2.21 must re-qualify (found in X32). **Exit evidence:** a decision per refusal, the comments, and the re-qualification note.

### B247

Widened by D183's answer to Q96 (a): every SSH-placed claim is published in the controller record, one whose placement is the controller as the controller's own and any other with every key qualified by `ssh/<address>:<port>/`, under the controller's conflict and release rules (an additive key form under controller record version 3, no version move; implemented with [B816](delivered.md#x182--audit-lane-n0-remaining-parts-to-their-owners-and-bmc-wording)). Still open: a controller whose sshd listens on a port other than 22 is not recognized; one host reached by a name and an address, or by two addresses, is not recognized; and bridge readiness pairs providers and services by Machine name (found in X32). **Exit evidence:** planning tests for each remaining part.

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

### B323

Managed-service and artifact-server removals release their socket reservations without proving the sockets free (found by the 2026-10-05 audit); [B301](delivered.md#x44--help-completion-contracts-and-ratchets) adds the Not-yet-met line. Each reserved socket would be probed after the stop, and its absence required in both absence checks. **Exit evidence:** absence tests in which a socket still held refuses the release.

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

### B331

On a FIPS-mode controller host, Bootwright's Go binary and its private CPython use their own cryptography, outside the host's validated modules, which [B292](delivered.md#x42--media-contexts-setup-service-and-machine-admission) documents and preflight reports. Qualifying FIPS would use Go's FIPS 140-3 module and a private CPython linked to the system OpenSSL FIPS provider (found by the 2026-10-05 audit). **Exit evidence:** a qualified FIPS build and runtime, with setup and a lifecycle run on a FIPS-mode host.

### B332

`auth.operatorIdentity` promises the invoking operator's own SSH identity, but the session client runs as root with root's default identities, which [B283](delivered.md#x40--machine-commands-status-secrets-and-validate-refusals) documents. Running the arm's client under the invoking account, with its default identities and `-l` defaulting to that account, would build on [B282](delivered.md#x39--untrusted-input-trust-and-privilege-boundaries)'s helper (found by the 2026-10-05 audit). **Exit evidence:** argv and session tests under the invoking account.

### B334

The emulated BMC runs as root, unconfined, with the libvirt socket, and requires a basic-auth credential; its TLS half, a generated serving certificate its client verifies, landed in-tree under [B446](delivered.md#x47--verified-emulated-controller-trust-and-bounds) (delivered in-tree, X47; D120), and the endpoint no longer serves plain HTTP. [B301](delivered.md#x44--help-completion-contracts-and-ratchets) binds the examples' BMCs to loopback. A refusal of a non-loopback bind address once an installation's provider host must be the artifact server's placement ([B289](delivered.md#x41--lifecycle-results-the-controller-stage-installation-shapes-and-bounded-runs)), and a confined SELinux type would shrink that surface (found by the 2026-10-05 audit). D120 split the TLS out as B446, now in M2 (D128). The loopback bind and the confinement remainder stay parked. **Exit evidence:** the refusal and the confined type, each with tests.

### B341

The plan's template-delimiter refusal also scans native passthrough content, such as a composed NMState document, overrides and a StorageCluster's service specification, so legitimate native syntax holding `{#`, such as a shell's `${#var}`, cannot be planned; and the refusal names the block's object and request path, for example every service a controller `noProxy` entry reaches or a line of a Machine's Kickstart, rather than the authored object and field the operator edits (found in X39). B244 accepted the trade-off; a reviewed escape or a per-field scope for native content is the owner's decision. **Exit evidence:** that decision with its tests, and refusals naming the authored object and field.

### B345

In a human invocation sudo's sudoers denial, such as `Sorry, user ... is not allowed to execute`, carries no `sudo: ` prefix, so the holding filter forwards it and only a JSON invocation gets the remedy naming a rule for the procfs re-execution; at a terminal the operator sees sudo's own text, while the operator guide says each refusal names which case applies (found in X39). Holding sudo's untranslated policy lines in a human noninteractive invocation, and printing one remedy line after an interactive refusal, would close it; otherwise the guide and the CLI spec say which sudo text means the rule is missing. **Exit evidence:** elevation rows in which a human invocation's policy denial ends with the mapped remedy, or the reworded guide and spec.

### FIPS access moved to M2

[B346](m1.md#b346) moved to M2 on 2026-10-11 (D129) for its security-profile
consumer.

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

### B366

`machine list --power-status` and the power commands read lifecycle evidence once per emulated Machine rather than once per invocation; a session offered `--ssh-id-file` asks the invoking account's opener twice, once to resolve the path and once to copy the key, which under root starts two helper processes; and resource selection scans the source files once per `resources` entry (found in X40). **Exit evidence:** one evidence read per invocation, one opening per session and one scan per selection, each pinned by a counting test.

### B367

The rename `validate` offers within two edits reads oddly for short keys, so `oc` suggests `govc`; a duplicate YAML key is reported at its parent mapping's path; retired-field refusals carry the generic remedy to remove the field, though each message names its replacement; and the infrastructure-services proxy check repeats, for `proxy: {}`, the schema's refusal of an empty choice as a second diagnostic (found in X40). **Exit evidence:** a rename offered only when the distance is below the key's length, the duplicate key at its own path and a remedy per retired field, each in the refusals golden, and `proxy: {}` yielding one diagnostic.

### B368

The enrollment test double records observations without a lock while enrollment observes up to eight endpoints at once, so `TestAnUnchangedKeyIsReusedAndNothingIsWritten` fails under `-race`, and `make race` does not cover `internal/trust`; the documented-command gate reads a `--` before a payload as an unknown flag, so the lab-rhel README writes `machine exec` without it; the status golden's retired-key fixture shows a keyring the local keyring cannot hold; `TestValidateRefusalsNameObjectFieldExpectationAndRemedy` lacks the `Golden` the Go rules ask of a golden test's name, and the rows of `TestSelectionRefusalsPointAtTheEntry` sit in a golden of their own; attributing a Secret refusal drops its usage mark, which custody avoids today only by never attributing a usage refusal; and the hand-built validate fixture `internal/cli/testdata/cli-validate-failed-json.golden` still shows the `api.field` message from before X40, `field is not permitted by this schema` (found in X40). **Exit evidence:** the double locked and `make race` covering `internal/trust`, the gate admitting `--`, a fixture the keyring can produce, the golden renamed and merged, attribution keeping the mark under a test, and the validate fixture rebuilt from a real compile.

### B369

The CLI spec's `--power-status` paragraph still calls an emulated controller unreachable when the context does not currently own it, where power now follows the machine block; its usage-failure rule names only `cli.usage`, though a service's usage refusal such as `secret.input` also exits 2 with concise help; and one line of its privilege paragraph runs past 80 columns. The command spec's `--ssh-id-file` row does not say the key is opened as the invoking account and copied; the controller spec asks only for the next safe command where a ready readiness for a context offers `bootwright plan --context`; the secrets spec does not say that the kernel's line buffer bounds a terminal line, so a long token must be piped, once that is confirmed; the input spec's plain-string rule would write plain some strings the YAML library reads as timestamps or numbers and double-quotes; the input ceiling table names the file-count resource differently from the parser's message; the lab-baremetal README's `--username <account>` placeholder reads as a redirection when pasted; and the CLI output spec's example diagnostic shows an `api.required` for a missing `metadata.name` on an object that has a name, which no compile can produce (found in X40). **Exit evidence:** each page corrected.

### B374

Since X40 `status` reads a keyring listing that fails as corrupt or undecryptable as the reopen will and names the binding lost, but a listing that fails for any other reason, such as `secret.store.key-unavailable`, names nothing, and `status` still offers `apply` and `destroy`, which then refuse with that cause (found in X40). **Exit evidence:** a status golden over a key-unavailable listing that names the failure and offers no verb that would refuse.

### B375

A fresh destroy over a failed setup receipt is proved to decide only in the lifecycle harness, whose receipt keeps an execution definition. The execution closure refuses a receipt that has none, naming `bootwright setup`, so the setup-gated steps `status` offers may have to name setup there in place of destroy; nobody checked what a failed receipt the context store writes holds (found in X40). X46 saw `status` report its `execution-bundle` setup check as ready over a receipt that holds no execution definition (found in X46). **Exit evidence:** a failed receipt written through the context store, with `status` and `destroy` agreeing on the step, and that check not ready.

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
through would fetch repositories directly instead of refusing (found in X43).
**Partly delivered** by
[X221](delivered.md#x221--declared-adapter-arguments): the digest and scratch
variables, `storage_ceph_core`'s nested request members and the
`physical_erase` targets; `nativeStaging` was already typed `int`. **Remains:** the bare nested mappings of
`managedos_install_anaconda` (`identity`, `placement`, `target`), the
`physical_erase` `install` mapping and the other Ceph roles' nested mappings.
**Exit evidence:** each defect covered or removed by a test that fails when its
guard is removed, and the remaining mappings declared and held to their Go
requests by a struct walk like `storage_ceph_core`'s.

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
postcondition are [B19](delivered.md#x93--the-owner-run-of-2026-10-14)'s, [B20](delivered.md#x93--the-owner-run-of-2026-10-14)'s and
[B270](delivered.md#x93--the-owner-run-of-2026-10-14)'s own host gates. **Exit evidence:** each observed on a
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
[B302](delivered.md#x44--help-completion-contracts-and-ratchets) tracks (found in X24). **Exit evidence:** each removed or
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

### B450

Every listener of a managed artifact server serves the whole served root, so
the private subtree is also reachable on a plain-HTTP listener: a tokenized URL
learned once could be fetched in clear (found in X45). Serving `private/` only
on https listeners is defense in depth for D103 and for the container cluster's
private agent image, and it changes the nginx template, so the automation
digest, and the golden. **Exit evidence:** the nginx golden denying the private
path on an http listener, and a test that a private publication on a server
with both listeners is fetched only over https.

### B451

X45 proved these only in-tree, each for the first host that runs the
lab-baremetal rehearsal or the physical BMC observation (found in X45): the SELinux
label of the delivered key files and of the `sshd_config.d` drop-in the
Kickstart's `%post` writes in the chroot, so that sshd loads the key, and
whether the drop-in's `HostKey` line limits sshd to the delivered key given the
`sshd_config` the release ships; whether the installer image and the served
tree carry the same label now that `chcon` references the served root for the
public arm too; the physical BMC's release time after an `EjectMedia` that answers 202,
which the client polls for up to 24 times 5 seconds and no capture measures;
whether the physical BMC's virtual-media fetch accepts the generated P-256 serving
certificate (U-11), read from its ClientHello; the physical BMC's `EthernetInterfaces`
collection and members on a powered-off server; the artifact unit's journal
volume during a streamed ISO fetch against journald's rate limit, since one line
is logged per completed range request; and the disk figures and the firewall
zone opening the lab-baremetal README gives, written as the brief stated them.
The device's `VerifyCertificate` is [B447](delivered.md#x47--verified-emulated-controller-trust-and-bounds)'s. **Exit evidence:**
each observed on a host and recorded in the
[acceptance ledger](../../docs/acceptance.md) or the knowledge page with the
build commit, or withdrawn by the owner.

### B452

Wording and tests X45 left narrower than they read (found in X45): the
protocol-record goldens for the managed-OS installation hold no
`privateDelivery` case, so its postcondition cases live only in the role's own
test; the substrates spec's delivered-key identity paragraph does not say the
pinned connection also pins `HostKeyAlgorithms` to the delivered key's type,
which the managed-OS spec does; the infrastructure-services spec does not state
that the managed artifact server logs each completed request without its URI to
its unit's journal and keeps its error log at warn, which the substrates spec
does; the sentence that a consumer sends a private URL only over a verified
channel, in the infrastructure-services spec and in the doc comment of the
private publication path, reads against the container cluster's agent image
posted to a plain-HTTP emulated controller, an exception the container-clusters
spec already records, so it needs a pointer to it; the docstring of the
security-service discovery says the physical BMC under test has a null
top-level property where the capture shows it absent, so it and its fixtures
need to distinguish absent and null top-level properties; the relaxed echo rule treats a device showing
`https://host/x.iso` as already holding `https://host:8443/x.iso`, so a device
serving the same path on another port of the same host is taken as inserted,
which is a recorded limit and a stronger proof is the owner's call; and the
lab-baremetal example still declares the default `import-certificate`
virtual-media trust, where the owner's physical BMC under test needs
`established`. **Exit
evidence:** each stated in its owner's file or covered by a test that fails when
its guard is removed, or withdrawn by the owner.

### B460

A resolution record does not say whether the apply's check or the removal's
(D119) produced its evidence, so `status` explains a removal-check resolution of
an apply block through the apply's reading, with a fallback; recording the check
needs a new optional field in the attempt record. The removal's check also reads
an agent installation as removed when no node presents media while the cluster
keeps running, and a capability may report no effect for a target it cannot
read, which the resolution maps to "partial, this context's own": stronger than
what was proved, though the more conservative of the two failed outcomes (found
in X46). **Exit evidence:** the record naming the check, `status` reading it, and
the state-reconciliation spec stating the weaker proofs, with tests; or withdrawn
by the owner.

### B461

The managed-service and artifact-server module utilities still duplicate the
unit-state, invoke, container-image, container-present and probe-failure helpers
after X24, so a fix like B436's is made twice. Go decodes a frozen managed-service
or artifact-server request without checking that its content root is clean and
under the prefix, where a controller-side mirror of B435's role refusal would
refuse a corrupt frozen block before any adapter run; no input reaches it today
(found in X46). **Exit evidence:** one helper set the two utilities import, with
the collection's units and lint suites passing, and the decoders refusing a root
with a `.` or `..` component or a repeated slash, in a test per kind; and
`make quick`, `make docs-check`, `make check` and `make race`.

### B463

X46 proved these only in-tree, each for D59's closing run or the first host that
runs the journey (found in X46): the layout of the `sudo -ll` listing the
input-logging check parses, from sudo's display code and not a recorded listing,
on Fedora 43 and RHEL 9.8 under a policy that sets `log_input`; that the
operator's terminal is left sane after the elevated child's exit on a second
signal under `use_pty`, which skips its deferred cleanup ([B339](delivered.md#x93--the-owner-run-of-2026-10-14)'s
run); a browse-mode autofs path and an NFSv3 export without READDIRPLUS under
root squash, for the input reader, and the kernel's `ENOTDIR` for
`O_PATH|O_NOFOLLOW|O_DIRECTORY` on a symbolic link or a FIFO; and systemd's
documented handling of a failed unit, that stopping it leaves it failed, that
failed units are not collected, that `reset-failed` on an unloaded unit fails and
that stopping a failed unit with no definition succeeds. **Exit evidence:** each
observed on a host and recorded in the [acceptance ledger](../../docs/acceptance.md)
or the knowledge page with the build commit, or withdrawn by the owner.

### B464

Wording, tests and dead code X46 left (found in X46). Wording: `specs/cli.md`'s
bullet for a destroy over an apply that has not completed still says the destroy
is offered unless a contradiction is named, beside the setup bullet that
overrides it; the lab-baremetal README does not point to the new `secret.input`
refusal under `log_input`; the security spec's row on authored strings reaching
Ansible does not cite the power-verb refusal tests; refusals other than the
`metadata.name` grammar from an object whose name is not a DNS label carry
neither the object nor its kind; and the interactive confirmation of a context
deletion has not been compared with the lost-context wording. Tests: the host
proof of a fresh destroy is shown between its shared and exclusive reads only
through presentation counts; the `--allow-orphans` exit for unsafe evidence of a
context still initializing is untested; the prerequisites service tests fake
the store's old bare limit texts; no test reaches the bundle backstop of the
client area or the dependency source backstop; and
`TestADestroyAfterAFailedPublicationCapturesBeforeAnyInverse` reads the result's
receipt when the destroy returned no result, so it fails through a nil-pointer
panic instead of its assertion. Dead code: the unlink helper builds its name pointer twice, the repository
grammar keeps an empty rule field, the context store keeps a private
reservation-key constant beside the prerequisites package's exported one, and
the template test's host fixture names a pool path the layout does not.
**Exit evidence:** each stated in its owner's file or covered by a test that
fails when its guard is removed, or withdrawn by the owner.

### B495

X48 qualified production concurrency in parts: the composed invocation scope
admits sibling runs of its real runner
(`TestTheComposedInvocationScopeAdmitsSiblingRunsOfItsRunner`), the production
DAG over the [three-node fixture](../../examples/machines/lab-rhel-3-nodes/README.md)
holds the right blocks ready together (`TestThreeNodeRHELExampleSharesOnePublicationAndAdmitsIndependentWork`),
and real adapter processes held at barriers overlap wave by wave
(`TestThreeMachineWavesRunInOneInvocationWithPrivateScratch`). No test runs the
whole lifecycle service over the fixture with the real runner and controlled
adapters (found in X48's review by the 2026-10-11 audit). **Exit evidence:** a
test that applies the fixture through the lifecycle service and the real runner
with controlled adapters, proving overlap of the services, of the machines and
of the three installations with one tree publication first, then one failure
and one cancellation each leaving no sibling's scratch or effect removed, and
its destroy in inverse order, with a mutation check that unwires the
production bound or the invocation scope; `make quick`, `make docs-check`,
`make race` and `make check`; or withdrawn by the owner.

### B503

A generated BMC serving certificate is proved only for the names it covers, at
admission, and a stored certificate when a fresh apply binds it. Its expiry and
the match between its private key and public certificate are not proved before
the emulator starts (found by the 2026-10-11 M2 review). **Exit evidence:**
bind-time tests that an expired certificate and a mismatched key refuse before
any effect, and that the remedy names the Secret.

### B508

After `secret set --password-stdin` refused with `secret.input` because sudo did
not list its policy, `secret check` still named
`bootwright secret set ... --password-stdin` as the remedy for the missing
material: the route just refused. Since b3e48bcb6 the policy listing works on
Fedora's sudo, so the refusal now happens only under a policy that logs input,
and there the remedy should name the file flags. **Exit evidence:** a test that,
under an input-logging policy, the missing-material remedy names
`--password-file`/`--value-file` and not the standard-input flag.

### B510

Two durability windows remain around on-demand directory creation. Both were
found while reviewing [B506](delivered.md#x83--operation-directories-created-concurrently)'s fix:

- A writer whose plain open lands between a concurrent creator's `Mkdirat`
  and its parent sync proceeds without syncing the parent itself.
- `selectionfs` (`files_linux_amd64.go`, the `.bootwright` creation) accepts
  `EEXIST` but skips the parent sync on that path.

**Exit evidence:** each writer that proceeds on a created directory has either
synced its parent or observed the creator's completed sync, with a test for
each path, or the owner accepts the window.

### B521

A stage-selection plan closes with `Starts N of M blocks, K deferred` and
`Concurrency`, as the [plan presentation](../cli/output.md#lifecycle-and-status-results)
requires. The deferred count does not separate two things: selected blocks that
wait on a block this invocation starts, which run in the same invocation, and
blocks outside the selection, which do not. `Concurrency` reports the width of
the whole plan, not the selection. In the owner's M2b Ceph run (628b35f58),
`apply --stage infra-components,substrates` previewed `Starts 1 of 20 blocks,
18 deferred` and then ran 5 blocks. `apply --stage machines` previewed `Starts 4
of 20 blocks, 10 deferred` and ran 7. Every selection previewed `9 waves, widest
7 steps`. **Exit evidence:** an updated contract and a golden for a stage
selection whose closing fields count blocks that start now, blocks that start
later in this invocation, and blocks not selected, and that report the
selection's own width, or the owner keeps the current fields.

### B522

`machine list` reports the `controller` Machine (OS `provided`) as LIFECYCLE
`not-applied` after the controller stage completed blocks placed on it. Under
the [lifecycle position rule](../cli.md#resource-inspection-and-explicit-access),
`not-applied` means no frozen plan names the Machine. The position is derived
only from a Machine's own realization block, which a provided Machine never
has, so the column reads as if nothing touched it (found in the owner's M2b Ceph
run, 628b35f58). **Exit evidence:** a decided position for a provided Machine,
such as `applied` once every block placed on it is done or a position of its
own, in the spec and a `machine list` golden.

### B526

`Service.Plan` computes the same-blocks proof of a continuation under another
build and applies neither it nor the closure and binding checks, contrary to
the [preview contract](../state-reconciliation.md#dependency-safety-during-recovery)
that a plan is presented only for a continuation that does not refuse, so
`plan` can preview a continuation its `apply` refuses with `lifecycle.state`.
X74 shows its `Build` field only where the proof passed and leaves the
difference alone. **Exit evidence:** a test that `plan` refuses, naming the same
block and remedy as `apply`, wherever `apply` refuses on recorded facts, and a
spec sentence that says so.

### B533

The independent review of
[X76](delivered.md#x76--the-live-cluster-phases-as-cephadm-and-ceph-behave)
left seven minor edges, each transient, each without an unproved effect and
each recovered by a later apply: `bootstrap_state` still refuses
`native-command` while the manager restarts, because `prove_seed`'s
orchestrator reads after `bootstrap-original-ready.json` and
`prove_mounted_seed`'s `verify_tunnel_configuration` are not wrapped by the
manager-served handling, against the storage spec's "an observation never
refuses it"; in observation a manager whose unit is activating or
deactivating while its container runs is pending without its mounts,
privileged flag and RW flag being inspected in that snapshot, so the
"contradicts the request refuses either way" sentence overstates; `tunnel_mount`
proves the redeployed manager's mounts before it waits for the manager, so a
container not yet running when cephadm returns refuses
`native-tunnel-unproved`; `enroll` can reach its fence with no retry budget
left, because the retries before it may use the whole 480 s horizon; a wait's
last iteration can overrun the horizon by up to 120 s, leaving little of the
640 s budget for the proofs after it; `readiness_detail` joins entries with
`.`, which daemon names also contain; and the storage spec says every phase
that restarts the manager waits for it, where only `tunnel_mount` does, and
"repeats only while that manager restarts", where any `native-command`,
`native-timeout` or `native-output` failure repeats for up to 300 s.
`Transport.call(phase="bootstrap")` also ignores a `timeout` argument, which
no caller passes. **Exit evidence:** each edge closed with a test, or the spec
narrowed to the implemented rule, and the detail separator made unambiguous.

### B537

A `cephadm bootstrap` that fails after `bootstrap-attempt.json` is written
leaves a partial seed, and by design no later apply starts a second bootstrap
over it, so the only exit is destroy. The 2026-10-13 run left exactly such a
seed on rhel-01 (an empty `mon.rhel-01` directory and a stopped, unit-less mon
in `cephadm ls`, from the failure X79 fixes) and had to be destroyed with its
machines. A bootstrap that provably made no mon store, started no daemon and
captured no output could instead be taken back for its own fsid only
(`cephadm rm-cluster --force --fsid <fsid>` without `--zap-osds`, the attempt
record cleared in the same journal) and retried. **Exit evidence:** the
recovery decision recorded, and, if adopted, a test that a seed with any mon
store, unit, container or captured output still refuses, and one that the
empty seed is removed and re-bootstrapped.

### B540

The pool block's lost-reply resume identifies the pool it created by name,
replicated type, CRUSH rule and this operation's attempt record; X81 dropped
the PG count from that list when the autoscaler is on. A pool deleted and
recreated under the same name between the create and the resume satisfies
all of them, and the resume would then record its ID, set its size and
initialize it for RBD. Separately, a same-name pool created by someone else
between the empty listing and `osd pool create` makes the create succeed and
is recorded as this operation's. Both need an outside actor during day-0.
Recording `osd dump`'s `pool_max` in the attempt record before the create and
requiring the resumed pool's `pool_id` to exceed it (Ceph never reuses pool
IDs) would rule out every pool that existed before the create; a
`create_time` bound would narrow the rest. **Exit evidence:** the
fingerprint decision recorded, and, if adopted, tests that a resume or a
create whose pool's ID does not exceed the recorded `pool_max` refuses
`pool-identity`.

### B544

[Machine SSH sessions](../cli.md#machine-ssh-sessions) offer `--ssh-id-file`
ahead of the declared credential, which remains the fallback. For a Machine
whose `access.ssh.auth` is a `passwordRef` arm, the session resolves the
password identity, and `sshlocal.authentication` then passes
`PubkeyAuthentication=no` with `PreferredAuthentications=password,keyboard-interactive`,
so the client names the offered key with `-i` but never tries it. X85 found
this while deciding the several-machine `machine exec` refusal of a password
identity, which therefore stays a refusal even when a key is offered. The
outcome is an authentication arm for a password identity with an offered key
that tries the key first and the password after it, with batch sessions
restricted to the key. **Exit evidence:** an argument-vector test that a
password identity with an offered key enables public-key authentication
ahead of the password methods, and a several-machine `machine exec` test that
such a Machine opens its session with the offered key instead of refusing.

### B545

A [power batch](../cli.md#machine-power-operations) runs each Machine in its
own bounded runtime, and a context's runs area keeps the 16 newest runs no live
run holds (`maxBoundedRuns` in `internal/workspace/contextfs`). A runtime holds
its run only until its own call returns, so in a batch of more than 16
Machines, with at most the engine's bound running at once, a later run's
admission can retire an earlier finished run of the same batch, and the result
row, the `LOGS` column and the envelope's `logs` then name a `run.output` that
no longer exists. The outcome is that every run a batch's result names is kept
until that result is presented: the batch holds its runs until it returns, or
retention spares the runs of a live invocation. **Exit evidence:** a test that
a batch of more than 16 Machines leaves every `run.output` its result names in
place.

### B558

`setup --dry-run` never elevates, and under D152 the setup receipt lives in
`/var/lib/bootwright/setup/state.json`, inside the root 0700 store, so the dry
run lists requirement names without the host, runtime and package table the
ready path prints. A 0644 copy of the receipt's index outside the store would
let the dry run render it unprivileged, at the cost of a second, world-readable
record of the host's packages and paths. **Exit evidence:** the owner's
decision; if adopted, a test that the dry run renders the table from the index
without elevating and a test that the index holds no secret or private path.

### B559

The runtime lock keeps cryptography, cffi and pycparser because ansible-core
declares cryptography, although no controller playbook is known to import it
(D152's clause 5). Dropping them shrinks the lock and removes the abi3 and
per-ABI wheels, and is a manifest-only change once proved. **Exit evidence:** a
playbook smoke test over every controller role that passes without the three
packages, and the lock regenerated without them.

### B560

X87's hostdnf adapter classifies dnf's `--assumeno` preview from fixtures the
agents wrote from dnf4's and dnf5's documented output (D152's clause 6). Real
captures from a RHEL 9 host and a Fedora 44 host, of a preview that installs,
one that finds no match and one that fails to resolve, would replace them.
**Exit evidence:** the captured fixtures committed, from the owner's runs in
X90's acceptance, with the adapter's tests passing on them.

### B561

Under D152's clause 3 the runtime uses the OS CPython at a versioned path, so
a host whose repositories offer no locked Python ABI refuses until a lock
covers it. A python-build-standalone interpreter, pinned by the lock, would
admit such a host at the cost of a second publisher and about 35 MB. No
supported host needs it. **Exit evidence:** the owner's decision; if adopted,
the interpreter in the lock and tests that it is used only when no locked ABI
is installed or offered.

### B562

`scripts/runtime-lock outdated` reports newer ansible-core 2.21 patches and
dependency releases as an advisory job summary (D152's clause 4), and the owner
regenerates the lock by hand. CI could open a pull request with the
regenerated lock when `outdated` finds one. **Exit evidence:** the workflow, and
a run that proposes a regenerated lock whose `verify` passes.

### B563

Under D152's clause 8, apply, destroy and the controller stage rebuild this
build's runtime from cached wheels, but only for the ABI of the receipt's
interpreter; after an OS upgrade that changes the Python ABI the operator runs
setup, because the new interpreter may need dnf. A rebuild for another ABI
whose interpreter is already installed and whose wheels are cached needs no
dnf. **Exit evidence:** a lifecycle test in which a changed ABI with its
interpreter present and its wheels cached rebuilds without setup, and one in
which a missing interpreter still refuses with "run bootwright setup".

### B567

[D155](#d155) requires definition and delivery of this guard with B568's
interim orphan warning before further `destroy` on a shared libvirt host.
Implementation still needs its named safety slice. Destroying a libvirt
provider host runs `virsh net-destroy` and `net-undefine` on every owned managed network
(`ansible/collections/ansible_collections/bootwright/core/roles/substrate_libvirt_host/tasks/destroy.yml:101-134`)
and never asks libvirt which running domains are plugged into the network or its
bridge. `HostCapability.Quiescent` answers quiescent unconditionally, because its
machines are probed in this removal (`internal/substrate/libvirt/capability.go:558-563`),
which assumes that only this context's Machines use the network. Nothing outside
one catalog enforces it: admission's shared-bridge check covers one context
(`internal/substrate/admission.go:428-466`), host reservations claim `bridge:`,
`libvirt-network:` and `prefix:` keys only for managed attachments
(`internal/substrate/libvirt/requests.go:167-182`), and an `external` attachment,
the default, claims nothing. Another context's Machines, or the operator's own
domains, can therefore share a managed bridge, and `destroy` removes it from
under them. The apply path guards the same hazard for a drifted network
(`ansible/collections/ansible_collections/bootwright/core/roles/substrate_libvirt_host/tasks/apply.yml:177-210`
and `network_guests` in
`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/substrate_libvirt.py:311-336`);
destroy has no equivalent, and the engine gate runs only for a fresh destroy
(`internal/reconciliation/lifecycle/execution.go:1078-1093`), so a continuation
skips it. The code matches `specs/substrates.md:167-170`, which says quiescence
is derived rather than observed, and that sentence contradicts
`specs/substrates.md:147-153` (stopping a network disconnects every domain
plugged into it) and `specs/state-reconciliation.md:1787` (removal takes only
what is out of use). **Impact:** running workloads outside the destroying context
lose their uplink and cannot restart until the bridge exists again; the other
context's settled apply still reports `done`, so nothing reports the damage. The
shipped examples all attach to `virbr-lab` on the controller host. **Smallest
fix:** the removal observation lists guests for every owned active network, not
only a drifted one; `destroy.yml` gains, before the first `net-destroy`, the two
refusals `apply.yml` has (the listing did not answer, or a domain that is not
shut off is plugged in), which also covers a continuation;
`HostCapability.Quiescent` probes instead of asserting and refuses
`lifecycle.live` naming the attached domains, so the refusal comes before
registration; the two spec sentences and the comment at `capability.go:558` say
so. Under D142 and D145 the role guard continues a live operation and needs no
destroy. The host-wide `bridge-use:` reservation, so that an external
attachment conflicts with another context's managed bridge, is tracked as
[B803](delivered.md#x165--audit-lane-b-physical-targets-and-shared-hosts). **Open:** whether a shut-off domain defined on the
network or bridge also refuses, since it cannot start once the bridge is gone.
**Exit evidence:** a collection unit test that
`destroy.yml` refuses on an unanswered listing and on a plugged-in domain that is
not shut off, a destroy test with a shut-off foreign domain defined on the
bridge that asserts the answer to the open question, a capability test that
`Quiescent` returns `lifecycle.live` naming
the attached domains, a lifecycle test that a continuation refuses the same way,
and `make docs-check` over the amended spec sentences.

### B568

[D155](#d155) accepts persistent random incarnation identity as the
design direction. Exact formats and migration belong to its named slice;
implementation is not authorized by this decision. B567 and the interim orphan
warning come first, before further shared-host destroy; full identity precedes
later delta scenarios. Libvirt object identity is a pure function of the
context name and the Machine name: `DomainName`, `DiskDirectory`,
`ControllerUnitName`, `DomainUUID`, the MAC address and the disk serial
(`internal/substrate/naming.go:55-80`). "Owned" means the domain metadata names
this context and Machine and the UUID equals the derived one; nothing ties an
object to a context incarnation, and the marker and the request digest carry no
operation identity (`internal/managedos/installation/requests.go:203-212`,
`internal/reconciliation/plan.go:196-212`). A fresh apply registers with no prior
host observation and the adoption guard refuses only an unowned same-name
domain. Two paths reach the gap: the documented recovery, `context delete
--purge --allow-orphans` followed by "the same init"
(`specs/contexts.md:590-596`), which omits the "remove those objects by hand"
step of `specs/state-reconciliation.md:1479-1481`; and two controllers whose
contexts share a name and target one libvirt host, which
`specs/infrastructure-services.md:89-91` already calls not coordinated. The new
apply then keeps the old disks (`creates:`), runs `virsh define` over the old
domain, overwrites the emulator's htpasswd and TLS and restarts it
(`ansible/collections/ansible_collections/bootwright/core/roles/substrate_libvirt_machine/tasks/apply.yml:33-44`).
A powered-off abandoned guest gives no identity answer, the anaconda guard passes
when nothing answered
(`ansible/collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/tasks/apply.yml:427-440`),
`pre_boot.yml` requires only power Off, and the Kickstart runs `clearpart --all`
(`internal/managedos/installation/kickstart.go:367-372`), so the disk is erased.
A running guest with the deterministic marker is silently accepted as installed.
Ceph is not affected, because its FSID is bound to the operation
(`specs/ceph.md:155-158`). Managed services and the artifact server show the
same pattern, because `infra_service.foreign()` skips the socket check when a
unit of that name is active
(`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/infra_service.py:127-139`);
that part is not fully traced. This is not a code and spec mismatch: name-derived
ownership is the specified contract, and the promise that scenario 1 means real
state is absent is an unproved assumption. **Impact:** silent data loss and
takeover of BMC credentials on objects the operator abandoned or another
controller runs; neither the plan nor the confirmation names any pre-existing
object. **Smallest fix:** first, a spec-only interim wording at
`specs/contexts.md:590-596` and in the orphan confirmation text: remove the
abandoned objects by hand before initializing the name again. Then a random
context incarnation id minted at `context init` and kept in the registry,
written into the libvirt ownership metadata, into the derivation inputs of the
UUID, MAC and serial, into `MarkerFor` and into the ownership markers of service
and artifact-server units, so a leftover object reads as foreign and the
existing "remove or rename it" refusals fire. It moves content digests across
five capabilities; exact formats and migration remain for the named slice.
Merge it with
the Ceph lineage id of B79, because both are the identity seam any scenario
beyond the three needs. **Exit evidence:** exact format and migration
definition under D155; a test per capability that an object carrying another
incarnation reads as foreign and its apply and destroy refuse; the contract
guard goldens and `cmd/bootwright/contract_versions_test.go` classifying every
new literal; the interim wording in the contexts spec.

### B569

`Store.StartAttempt`, `StartResolution` and `CompleteResolution` write without
checking the block record's state: `StartAttempt` sets `Attempts+1` and
`running` on whatever it reads, `StartResolution` allocates the next resolution,
and `completeRecord` sets the state for any effect and state pair
(`internal/reconciliation/operationstore/store.go:717-776`, `:889-932`,
`:1009-1046`). The only guards are in memory: `firstAdmission` and `unproved`
(`internal/reconciliation/lifecycle/scheduler.go:205-240`), the re-read in
`verifyBasis`, and `ResolutionTransition`
(`internal/reconciliation/state.go:104-116`) in the single caller. No path
reaches a bad transition today. A second caller, such as a reconcile of only the
new pool or a wrongly built diff, could re-run an effect over an unproved block,
demote a `done` block or replace the completion evidence dependents read
(`internal/reconciliation/lifecycle/evidence.go:159-168`). **Impact:** defense in
depth; the durable record would lie silently if a future path misused the store.
**Smallest fix:** `StartAttempt` refuses `lifecycle.state` unless the record
reads pending (an absent record counts) or failed under a retry, leaving the
adopt branch alone; `StartResolution` refuses unless the record reads unknown or
running with `Attempts == attempt`; `completeRecord` refuses unless the pair
equals `ResolutionTransition(effect)`; the tests that start after a done block
(`internal/reconciliation/operationstore/store_test.go:519`, `:587-601`) are
rewritten; one spec sentence under Block transitions. **Exit evidence:** a store
test per refusal, the rewritten tests, and `TestTransitionTablesMatchSpec`
unchanged. The parsing half is [B582](#b582).

### B570

A fresh destroy over an unproved apply block falls back to the removal check
after any apply observation that does not prove completion. Four removal checks
do not observe and synthesize `Completed`: controller clients, Ceph host, Ceph
image, and Ceph core when `RetiredWithHosts`
(`internal/controller/clients/capability.go:182-201`,
`internal/storage/ceph/host.go:437-448`, `internal/storage/ceph/image.go:269-282`,
`internal/storage/ceph/retirement.go:75-81`). That answer is recorded as
no-effect or failed on the apply's block
(`internal/reconciliation/lifecycle/attempts.go:182-201`, `:263-279`) before
`proveQuiescent` runs (`internal/reconciliation/lifecycle/execution.go:1085-1094`,
`:1187-1205`); if quiescence then refuses, the record stays. It is the specified
D119 behavior (`specs/state-reconciliation.md` removal-check rules), but the
record claims an absence no host observed. **Impact:** `status` and a diff read
"never performed", and the next apply retries instead of resolving first; no
damage, because the Apply paths converge under the FSID ownership checks.
**Smallest fix:** mark the checks that do not observe (`Observing=false`), record
those blocks failed with the effect unknown, and have a continuation resolve
them before it retries. Do not leave them unknown, because that breaks destroy of
retired Ceph. It changes the D119 table, so it rides with B460. **Exit
evidence:** a lifecycle test that a refused quiescence leaves the apply block
unrecorded by a removal check that observed nothing, a test that a retired Ceph
destroy still completes, and the amended table in the state-reconciliation spec.

### B571

Managed services, the artifact server and the installation each derive
quiescence from "their Machines are probed", and a retained bare-metal Machine
always answers quiescent
(`internal/infrastructureservices/managedservice/capability.go:405-411`,
`internal/infrastructureservices/artifactserver/capability.go:323-328`,
`internal/managedos/installation/capability.go:572-577`,
`internal/substrate/baremetal/capability.go:212-217`). Destroy therefore removes
DNS, NTP and the artifact server from under a running, retained physical host
(the lab-baremetal example's NTP `lab-ntp` and DNS `lab-dns`). Each link is in
the spec (`specs/infrastructure-services.md:362-365`,
`specs/substrates.md:519-535`); the consequence is not, and "removes only what is
out of use" (`specs/state-reconciliation.md:1787`) is overstated. **Impact:** a
retained host loses name resolution and time sync; the operator's destroy was
explicit and no data is lost. **Smallest fix:** state in the spec that derived
quiescence holds only through consumers that prove their stop; the destroy plan
names each retained consumer that still references a removed service; the three
code comments say so. The `Quiescent` answers do not change. It sits beside B70.
**Exit evidence:** the spec sentence, a plan test that a retained physical
consumer is named, and `make docs-check`.

### B573

The runner's "no adapter effect outlives cancellation" holds only for the local
process tree
(`internal/reconciliation/ansiblerunner/process_linux_amd64.go:267-273`). A Ceph
remote worker (setsid, `RequestTTY no`, no stdin or channel watch) runs on for
up to 640 s, or 1800 to 1920 s for a bootstrap, after cancellation or a dropped
link
(`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_worker.py:1-9`,
`:186-188`, `:230-340`;
`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_core.py:28-31`).
The engine treats unknown or running as "the executor died" and resolves or
retries at once
(`internal/reconciliation/lifecycle/execution.go:1504-1506`,
`internal/reconciliation/lifecycle/scheduler.go:276-289`, D146). A concurrent
bootstrap is fenced by `bootstrap-attempt.json` and the daemons check
(`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py:1767`,
`:1773`, `:1788`, `:1873`); the later phases rely on idempotence, which was not
audited for concurrency. **Impact:** a misleading partial-cluster refusal or a
failed block during the window; no confirmed damage. A canceled `diff --observe`
can keep a read-only worker alive for the same window. **Smallest fix:** reword
the runner comment and add a spec clause stating the remote window
(`specs/security.md:165-177` or `specs/ceph.md:110`); then a remote-ceiling
wait, or a worker that ends on channel EOF, before any delta re-runs a Ceph
block. SSH qualification is already open in M2. **Exit evidence:** the spec
clause and comment now; a test that a canceled remote worker ends, or that the
engine waits its budget before it resolves.

### B576

The spec says completion "requires the domain defined with the frozen
definition" (`specs/substrates.md:336`), but `ValidateMachinePresence` proves
the domain name and ownership, the active unit, the controller image, the system
UUID, a non-empty power state and the disks
(`internal/substrate/libvirt/evidence.go:70-105`, `:382-409`). It never reads
back vCPU, memory, interfaces and MACs, TPM or boot order. Every apply re-runs
`virsh define`, so apply converges the definition by construction, but
`changed_when` is true only when the domain was absent and the outcome rollup
ignores the define, so a redefinition is reported `unchanged`
(`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/substrate_libvirt.py:386-416`,
`:684-694`). On the resolution path, `Observe` can resolve a block `done` with no
redefine, after a cross-build template change or an operator `virsh edit`.
**Impact:** a false claim of completeness and a mislabelled outcome; the target
is not damaged, and `diff --observe` cannot compare the unproved fields. **Smallest
fix:** narrow `specs/substrates.md:336` to what the evidence proves and say that
the definition is rewritten on every attempt; report `changed` when
the dumpxml taken before the define differs from the rendered XML; widen
`libvirt_domain_inspect` and `MachineEvidence` as the Machine projector item
needs a field. [D155](#d155) accepts this correction for a future named
slice. **Exit evidence:** the narrowed sentence, a role test that an actual
redefine reports `changed`, and read-back tests for each newly supported field.

### B577

Ceph pool and client requests embed the whole cluster request
(`internal/storage/ceph/requests.go:426-477`,
`internal/storage/ceph/capability.go:46-56`, `:90`,
`internal/reconciliation/plan.go:196-212`). Adding one pool moves no sibling's
digest, because a pool embeds the cluster and not its sibling pools; the debt
applies only to a scenario that edits the cluster and reconciles the delta. Part
of the embedding is load-bearing: the OSD-host count gates the pool size
(`internal/storage/ceph/requests.go:447-458`) and the native module requires the
hosts
(`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py:352-356`).
**Impact:** none today; a prerequisite of the recipe for one more `StoragePool`
over a completed apply. **Smallest fix:** a pool and a client request carry only
the cluster facts they use, so a comparison shows exactly one added block and
every other block unchanged. It rides with the Ceph lineage id of B79. **Exit
evidence:** a request-shape test that a pool's digest does not move with an
unrelated host edit, and the contract guard goldens.

### B578

`status` builds its rows from the compiled catalog only, and
`realizedClusters` and `realizedServices` decorate existing rows
(`internal/reconciliation/lifecycle/status.go:194-199`, `:254-260`,
`:515-548`, `:673-689`). After `context update --input-dir` over a completed
apply, which only warns (`specs/contexts.md:157-160`), an object the input no
longer declares is not listed although `lifecycle.blocks` still lists its blocks
and `apply` refuses. **Impact:** a safety visibility gap: `status` is the
inventory the [permanent deletion](../contexts.md#permanent-deletion) names
for what an orphan-acknowledged deletion abandons, so that deletion can
abandon a realized object the operator never saw listed. `diff` already shows
the realized-only side. **Smallest fix:** build the rows from the union of
declared and realized objects, mark the undeclared ones, and update
`specs/cli/output.md:632`. **Exit evidence:** a `status` golden over a context
whose edited input removed a realized object.

### B579

After an observation, `firstAdmission` falls through to `Startable` when the only
failed block is outside the selection
(`internal/reconciliation/lifecycle/scheduler.go:196-240`), while the same
selection with nothing unproved refuses `lifecycle.stage`
(`internal/reconciliation/lifecycle/execution.go:597-607`,
`internal/reconciliation/stage.go:62-83`). The doc comment intends this; the
spec says "worked" (`specs/state-reconciliation.md:876-879`) and the code does
not count an observation as worked. **Impact:** less predictable; nothing
outside the selection, and nothing downstream of the failed block, starts.
**Smallest fix:** decide whether "worked or observed" applies, say so in the
spec and add a unit test. Needs a small owner decision. **Exit evidence:** the
decision, the spec sentence and a scheduler test for both orders.

### B580

The release set is computed in four places and the reopen binding in three
(`internal/reconciliation/lifecycle/execution.go:385-391`, `:403-404`,
`:701-702`, `:712-727`; `internal/reconciliation/lifecycle/finalization.go:106-116`;
`internal/reconciliation/lifecycle/frozenbinding.go:186-197`). They agree only
because `Operation.Bindings` holds at most one entry
(`internal/reconciliation/lifecycle/execution.go:1572-1576`) while the reader
accepts up to 64. **Impact:** drift risk when a second binding exists. **Smallest
fix:** one `ownedBindings(store, op) (reopen, release)`, or pin `len(Bindings) <=
1` in `validateOperation`. **Exit evidence:** one function with the three callers
and a test that the four release sets agree for an operation of each verb.

### B581

"A missing state reads pending" is open-coded at about nine sites
(`internal/reconciliation/lifecycle/execution.go:503-505`, `:1323-1330`;
`internal/reconciliation/lifecycle/contradictions.go:92-97`;
`internal/reconciliation/lifecycle/service.go:208-213`;
`internal/reconciliation/lifecycle/status.go:90-93`, `:621-625`;
`internal/reconciliation/lifecycle/attempts.go:586-591`;
`internal/reconciliation/lifecycle/finalization.go:292-298`;
`internal/reconciliation/stage.go:164-169`). The store already normalizes
(`internal/reconciliation/operationstore/store.go:559-569`, `:672-673`), so the
guards fire only for hand-built maps. **Impact:** drift risk for new readers such
as a diff. **Smallest fix:** route every site through `blockState` in
`internal/reconciliation/stage.go`, or guarantee a non-empty state at the
producers. **Exit evidence:** the copies gone and one test of the shared
function.

### B582

`StartResolution` and `LastResolution` parse resolution entry names differently
(`internal/reconciliation/operationstore/store.go:903-914` against `:968-981`),
and `StartResolution` never reads `attempt-N.json`. **Impact:** none on the names
the store writes; part of the same gap as B569. **Smallest fix:** one shared
parser with a strict `.json` check and a canonical number, and `StartResolution`
proving the attempt it resolves. **Exit evidence:** a store test that a
non-canonical entry name and a resolution of a missing attempt both refuse.

### B583

The index, block and attempt record versions are bare literals; only
`OperationVersion` has a name
(`internal/reconciliation/operationstore/records.go:147`, `:261`, `:271`,
`:350`, `:363`; `internal/reconciliation/operationstore/store.go:82`, `:673`,
`:758`, `:923`). **Impact:** a version bump can miss a site; it fails closed.
**Smallest fix:** one named constant per record, classified in the contract
guard (`cmd/bootwright/contract_versions_test.go`). **Exit evidence:** no bare
version literal remains and the guard classifies each constant.

### B584

No golden covers a resolution carrying an `ObservationFailure`; its tags are
unpinned at byte level. `replaces` is pinned only by a substring, and an adopted
attempt has the same shape as `attempt-running`
(`internal/reconciliation/operationstore/golden_test.go:266-273`,
`internal/reconciliation/operationstore/records.go:210`, `:218-222`,
`internal/reconciliation/operationstore/store_test.go:268-273`, `:1051-1054`).
**Impact:** a tag rename passes silently. **Smallest fix:** a
`resolution-unknown-failure.golden` and goldens for an adopted attempt and for
`replaces`. **Exit evidence:** the three goldens.

### B585

`specs/substrates.md:292-293` and `:347-348` say the root disk size refuses; the
role refuses a size mismatch on any present disk
(`ansible/collections/ansible_collections/bootwright/core/roles/substrate_libvirt_machine/tasks/apply.yml:46-54`,
`internal/substrate/libvirt/requests.go:140`). The role is the safe side.
**Impact:** the spec understates the refusal. **Smallest fix:** both sentences
say "an owned disk, root or data". **Exit evidence:** the two sentences and `make
docs-check`.

### B586

The comment on the controller-clients `ContentDigest` says shape and automation
changes invalidate a plan, but the function hashes only the label,
Implementation and Version
(`internal/controller/clients/requests.go:115-122`). The contract guard test is
the real protection, and only for Go schemas (D145,
`cmd/bootwright/contract_versions_test.go:43`). **Impact:** the comment
over-claims. **Smallest fix:** reword it to what the function hashes, or drop it
under the comment rule. **Exit evidence:** the reworded comment under `make
quick`.

### B587

When the X91 audit ran, the Ceph pool phase ran `osd pool set size` and
`min_size` blindly on every pass that was not ready and passed the ownership
check, writing drift back unreported. [D155](#d155) accepts refusal without
writes until a named delta admits drift.
[X99](delivered.md#x99--operator-selected-ceph-artifacts-and-m2b-completion)
delivers that policy for the minimal pool phase: each setter retains its intent
before the effect and its completion after readback, keyed by the numeric pool
ID, and a completed setting whose live value differs refuses `pool-identity`
without writing
(`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py:2874-2889`);
[storage](../ceph.md) requires it for every pool setter. **Remaining:** no
role test shows detected drift of a completed setting refusing without writing,
and M2c's [B81](m2.md#b81) must route its quota, compression and EC-overwrite
setters through the same records. **Exit evidence:** that role test. The
projector that proves them is [B600](#b600).

### B588

`Observe` on the Ceph image and on the installation tree drops decode errors and
returns Unknown without an error
(`internal/storage/ceph/image.go:262-265`,
`internal/managedos/installation/tree.go:248-251`, against
`internal/storage/ceph/host.go:405-408`;
`internal/reconciliation/lifecycle/attempts.go:190`, `:301-308`;
`internal/reconciliation/lifecycle/unresolved.go:116-148`). The effect is the
same, but no observation-failed event or recorded failure is produced.
**Impact:** weaker diagnostics. **Smallest fix:** return the error. **Exit
evidence:** a test per capability that a malformed evidence reads Unknown with a
recorded reason.

### B589

The Ceph core and host capabilities implement no `Unresolved`, so the generic
"proved neither" reason applies; the image capability is the only one
(`internal/storage/ceph/image.go:291`,
`cmd/bootwright/wiring_reconciliation.go:203`, `:206`,
`internal/reconciliation/lifecycle/unresolved.go:16-52`). **Impact:**
diagnostics only. **Smallest fix:** `Unresolved` methods using
`lifecycle.Drifted`. **Exit evidence:** a test of the reason each returns.

### B590

Ceph wires the image dependency two ways: a pool gets it through `Requires`,
which also pulls in every host block, and cluster and client blocks use explicit
edges (`internal/storage/ceph/capability.go:59-89`,
`internal/storage/ceph/image.go:151-152`, `internal/storage/ceph/host.go:225-227`,
`internal/reconciliation/plan.go:226-239`). The edges are correct today
(`internal/storage/ceph/image_test.go:97-101`). **Impact:** maintainability.
**Smallest fix:** one mechanism. **Exit evidence:** the existing edge test green
over the single mechanism.

### B591

`Ownership()` and `realizations()` are two independent folds of the same
records and disagree: a released object reads Destroy and done in one and
pending or unknown in the other, one keeps running apart from unknown and the
other merges them, and the rankings differ
(`internal/reconciliation/lifecycle/status.go:59-169` against `:513-689`;
consumers in `internal/machine/access/hostkey.go:59` and
`internal/machine/inventory/service.go:45`). `diff` reads `realizations` and adds
no third fold. **Impact:** consistency risk between `status` and the Machine
commands. **Smallest fix:** one per-object fold with a test that ties the two
views together. **Exit evidence:** one fold and that test.

### B593

`RenderEffective` returns only the counts and the effective catalog, while the
state from the compile port still exposes `Authored` and `Origin`
(`internal/desiredstate/compilation/service.go:33-41`,
`internal/desiredstate/compilation/requests.go:17-20`,
`internal/reconciliation/lifecycle/contracts.go:105`). **Impact:** none, because
`diff` uses the compile port. **Smallest fix:** `RenderEffective` can keep the
authored catalog and origins when a consumer needs them; until one does, leave
it. **Exit evidence:** the consumer that needs it, with a test.

### B594

The envelope shape is defined twice, once typed for decoding and once untyped for
encoding (`internal/desiredstate/compilation/decoding.go:59-64`,
`internal/desiredstate/encoding/encoding.go:72-74`). **Impact:** drift risk for
a renderer. **Smallest fix:** one shared constructor in the API package. **Exit
evidence:** one definition and a round-trip test.

### B595

Ceph has no refusal table in the spec and no
`Test*RefusalTableMatchesUnsupported`, and the docs-check package list, kept by
hand, omits it (`internal/storage/ceph/capability.go:104-122`,
`internal/reconciliation/lifecycle/contracts.go:295-297`, `Makefile:54`). Ceph is
the first scenario consumer. **Impact:** a refusal can change without the spec.
**Smallest fix:** the table, a test over `Unsupported()`, and the package in the
docs-check list. **Exit evidence:** that test and a docs-check entry.

### B596

The Ceph cluster and client observations, and the agent install and media
observations, hand over material (dashboard, client keyring, admin kubeconfig)
and require it
(`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_core.py:508-511`,
`internal/containercluster/agentinstall/install.go:292-296`). The runner refuses
`Outputs` on an invocation no operation records, so under `diff --observe` these
blocks read unknown. X126 ([D199](#d199)) delivered the agent install block's
half: its observation declares no output outside an operation; the Ceph
cluster and client blocks remain. **Impact:** safe, but it lowers the usefulness of `diff` for
Ceph contexts. **Smallest fix:** their observation runs without outputs when no
operation records it. It touches the Ansible plugin and the capability Go code.
**Exit evidence:** a test that `diff --observe` reads the Ceph cluster, Ceph
client and agent blocks present or absent instead of unknown, with no material in
the run directory.

### B597

`destroy paused` is reachable
(`internal/reconciliation/lifecycle/online_removal.go:165-168` sets the online
boundary; `internal/reconciliation/lifecycle/scheduler.go:142`) but has no row in
`specs/state-reconciliation.md:449-466`. **Impact:** the state table is
incomplete; the decision (continue) is unchanged. **Smallest fix:** a row and a
`stateMachineRows` case. **Exit evidence:** `TestTransitionTablesMatchSpec` over
the new row.

### B598

Input restored byte for byte after an edit gets a new revision, so a completed
apply currently refuses even when all original bytes are restored. [D155](#d155)
accepts settlement over exact-original-byte restoration for a future named
slice. Until that slice changes the content comparison and spec row, the
current code and refusal remain. **Exit evidence:** a lifecycle test and spec
row admitting exact-original-byte restoration while every other changed input
still refuses before effects.

### B599

Whatever the allowlist test over the observe entry points finds: any observe task
that writes outside the runner's job directory or its scratch directory. The
candidates are `work.yml` and `reachable.yml` of
`ansible/collections/ansible_collections/bootwright/core/roles/managedos_install_anaconda/tasks/`.
**Impact:** an observation, which the read-only guard assumes changes nothing,
would leave files elsewhere. **Smallest fix:** observe writes nothing outside the
runner's private directories. **Exit evidence:** the allowlist test with no entry
that writes outside them.

### B600

Candidate. A projector for the Ceph StoragePool: `Ready` proves
`spec.replicated.size`, `spec.replicated.minSize` and `spec.role` equal to the
frozen request
(`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py:2992-2993`),
and `Exists && !Ready` renders a note. It carries the evidence widening of the
design's per-capability item for the pool. **Requires:** the projector port that
B565 delivers. **Definition:** Candidate. **Exit evidence:** a projection golden
and the observable-field table row in the diff spec.

### B601

Candidate. A projector for the bare-metal Machine: the MAC set becomes the named
`hardware.nics`, and the BMC address that answered is shown. It carries the
evidence widening for that capability. **Requires:** the projector port that
B565 delivers. **Definition:** Candidate. **Exit evidence:** a projection golden
and the observable-field table row.

### B602

Candidate. A projector for managed services and the artifact server: bind
address, ports and listeners. It carries the evidence widening for those
capabilities. **Requires:** the projector port that B565 delivers.
**Definition:** Candidate. **Exit evidence:** a projection golden per capability
and the observable-field table rows.

### B603

Candidate. A projector for the OS tree and the install: the install address. It
carries the evidence widening for those capabilities. **Requires:** the projector
port that B565 delivers. **Definition:** Candidate. **Exit evidence:** a
projection golden and the observable-field table row.

### B604

Candidate. A projector for the agent install: release version and declared
nodes. It carries the evidence widening for that capability and needs
[B596](#b596) to read the block at all. **Requires:** the projector port that
B565 delivers. **Definition:** Candidate. **Exit evidence:** a projection golden
and the observable-field table row.

### B605

Candidate. A projector for the Ceph image, host and core: image, topology nodes
and labels. It carries the evidence widening for those capabilities and needs
[B596](#b596) for the core. **Requires:** the projector port that B565
delivers. **Definition:** Candidate. **Exit evidence:** a projection golden per
capability and the observable-field table rows.

### B606

A libvirt machine whose owned, answered domain differs from its frozen request
only in a disk size reads unknown under observation while its controller runs:
the adapter's postcondition stays true, `ValidateMachinePresence` refuses the
size, and `machineEffect` (`internal/substrate/libvirt/capability.go`) accepts
neither the presence nor the partial reading, so the engine never calls the
projector and `diff --observe` shows the size refusal as the reason and no
hunk. The journey `TestALibvirtDiskResizedByHandShowsAsAHunk` reaches the hunk
only beside a stopped controller, which reads partial. The projector also emits
one note per missing frozen disk, so a machine missing more than eight disks
reads unknown under the eight-note bound instead of projecting. **Outcome:** an
owned, answered domain whose only difference is a disk size reads partial for
the apply observation, so a running resized machine projects its observed size
and the missing-disk notes fold into one bounded note. **Requires:** none.
**Definition:** Specified. **Exit evidence:** a journey case with the
controller running that asserts the InfraProvider reads changed with the
observed size, and a projection test over nine missing disks that still
projects.

### B609

In the owner run of 2026-10-15 ([X93](delivered.md#x93--the-owner-run-of-2026-10-14)), with standard
output piped (`apply | tee`, `setup | tee`), the terminal showed `Confirm apply
for context …? [y/N]` and `Confirm controller setup on this host? [y/N]`
before the plan and dnf preview they confirm: the prompt reaches the terminal
while the plan is still buffered on the pipe. On the RHEL 9.8 guest the owner
answered a setup prompt before its preview appeared, which declined it.
**Outcome:** every confirmation follows the complete plan on every output
stream, whether standard output is a terminal, a pipe or a file. **Exit
evidence:** a CLI test over a piped standard output that the plan bytes precede
the prompt.

### B610

After a host restart left a context's managed network inactive ([B608](delivered.md#b608)),
`diff --observe` reported "No differences" and read the provider host as
"part of this effect is realized as its frozen request states" with
`spec.networkAttachments` not compared. **Outcome:** observation of a libvirt
host reads a managed network that is defined but not active as a difference,
which the provider's hunk shows. **Exit evidence:** a projector test over an
inactive owned network.

### B611

`diff --observe` with standard output piped wrote the row "read what this
context's apply realized, changing nothing" once per block, nine and then
thirteen times, without naming the block. **Outcome:** a non-terminal check
list writes one row per block, naming it, or one summary row. **Exit
evidence:** a golden of the piped check output.

### B612

On the RHEL 9.8 guest of the owner run, setup's preview note read "this install
also updates core packages (python3.14, python3.14-libs,
python3.14-pip-wheel)" for packages the host did not hold
(`internal/controller/hostdnf/packages_linux_amd64.go`). D152's clause 6 makes
the selected `python3.X` a core package, so the note is right to appear.
**Outcome:** the note says the install adds or updates core packages as dnf's
transaction does. **Exit evidence:** a hostdnf preview test with a fresh
install.

### B616

Repository-wide Go package patterns and `TestEveryRequestFieldIsRead` can walk
ignored non-source directories and fail on an unreadable entry before checking
tracked source. The same committed source passes in a clean worktree. Make the
source boundary explicit without changing permissions or deleting ignored data,
and retain complete tracked-package coverage. **Exit evidence:** an ignored,
unreadable directory regression plus the complete source checks. Parked as
developer tooling; this adds no product or M2b acceptance gate.

### B617

`RegistryConnection.connect` in
`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_pull_broker.py`
checks cancellation before allocating and publishing its socket, without
serializing that publication against `close`. A network-free synthetic schedule
can let close see no socket, then let connect allocate one afterward. Concurrent
close can also clear `self.sock` during connection setup. Existing socket and
worker deadlines still bound the attempt; immediate cancellation is not proved.
This predates blob delivery and is left unchanged by that correction.
**Outcome:** socket publication and close share a tested ownership transition,
without holding a mutex across blocking connect or TLS. **Exit evidence:**
injected cancellation at allocation, connect and TLS boundaries proves no late
publication, lost socket or unhandled setup exception. Parked for separate
cancellation work; it adds no M2b acceptance gate.

### B624

Network convergence ([B326](m1.md#b326)) stores a drifted installed Machine's
bounded `networkDifferences` in its installation evidence, and observation
reads the Machine as a positive partial realization, but `diff --observe`
attaches only the generic partial note: no command renders the differences
(found by the X55 checkpoint, which changed the libvirt network-variants
runbook to read the drifted value from the guest). **Outcome:** the observed
diff of an installed Machine names each bounded network difference without
echoing a secret. **Exit evidence:** a projector and diff golden over a drifted
bond MTU.

### B630

A bootstrap or MGR `set-user` that the native tool rejects at argument level,
with an exit 2 or an `EINVAL` and a complete proof that nothing changed, still
leaves an attempt record, so every later apply refuses `partial-cluster` and
the operation's destroy is the only way out. X101 checks the bootstrap options
against the anchored host parser before the record, which covers that one
case; the `set-user` case has no static parser to check against. **Outcome:**
a named slice narrows `partial-cluster` for a proved-absent native rejection
only, with the proof, the refusal that stays and the destroy-only cases
specified. **Requires:** a named safety slice and an owner decision, as the
[scope rules](../milestones.md#scope-rules) require for narrowing a safety
refusal. **Definition:** `Needs definition`. **Exit evidence:** a test that a
proved-absent rejection continues and one that every other attempted state
still refuses.

### B631

A wrong `spec.distribution.ibm.callHome` for the selected build is refused at the cluster's
claim, after the ceph-host blocks ran from the frozen input, so correcting it
costs the operation's destroy and a fresh apply (D162 clause (b)). The refusal
cannot come earlier while Go cannot see the build's Call Home model, and D157
forbids deciding it from the release label. **Outcome:** once the frozen,
authenticated artifacts expose that model to planning or to the first host
block, the wrong value is refused before any host effect. **Requires:** a
planning-visible Call Home model fact. **Definition:** `Needs definition`.
**Exit evidence:** a refusal test with no host block run, and the existing claim
refusal kept as the backstop.

### B632

`installed_cephadm` in the Ceph host runtime compares the host and image
cephadm and has no caller. Three tests in the host runtime unit file patch it.
**Outcome:** the function and its patches are deleted, or a caller is added
with a stated reason. **Requires:** none. **Definition:** `Specified`.
**Exit evidence:** the collection unit and sanity suites
pass without it.

### B633

An xdist worker crashed on `test_native_timeout_is_bounded_and_sanitized` in the
SSH key policy unit file during the first whole-tree unit run of X101, under
load; the file passes alone and the whole tree passed on rerun. **Outcome:** the
test is deterministic under parallel workers or its cause is recorded. **Requires:** none. **Definition:** `Specified`.
**Exit evidence:** repeated narrowed parallel runs of the file with no crash.

X102's second review gate found a second case: the libvirt firewall readiness
unit test `test_vendor_defaults_allow_completion_replay_and_owned_removal_without_global_writes`
(parametrized case `names0`) failed once on Python 3.9 and passed on an identical
rerun and on every later run; no X102 commit touches it or its module. The test
compares each vendor file's `stat()` before and after the reads it proves
harmless, so an access-time update under load is the suspected cause,
unconfirmed; ce9767849 reported the same test earlier, with only `st_atime`
differing. **Outcome:** that test too is deterministic under concurrent gate
runs or its cause is recorded. **Exit evidence:** repeated concurrent gate runs
of the file with no mismatch.

M3b's full gate on 2026-10-20 observed the same Python 3.9 `names0`
failure. File bytes, inode, mode, ownership, size, modification time and change
time matched; only access time differed after the read. Neither the test nor
its production module is changed by M3b. This remains B633's follow-up rather
than a change to the M3b implementation.

### B634

The runbook's validation text and its Dashboard and IBM Call Home rows assume
the tracked `callHome: enabled` and `20.2.2-349.el9cp`, and the guide for guests
of another RHEL minor does not say to rename `metadata.name` in the two copied
profile files. **Outcome:** the validation steps compare against the operator's
own selected coordinates and the Call Home intent the selected build realizes
(`call_home_agent` enabled, or not enabled for `disabled`), with no per-release
table (D163), and the guide names every field to change in the copied profile
files, including `metadata.name`. **Requires:** none. **Definition:** `Specified`.
**Exit evidence:** a docs-check pass and the owner's three runs reading it.

### B635

X101's reviews left these edges, none reached by a signed build today:

- the `ibm_build` detector reads `IBM_BUILD` only from `override_vars.py`, so a
  zipapp that ships it as a compiled module or a package reads as absent where
  unknown would keep the strict flow;
- the seam-call test credits a call to the innermost function only, and the
  license-call refusal's tie to the image has a surviving mutation;
- `verify_tool` runs more than once per worker run, as before X101;
- the image-only guard in the OpenShift topology rule and the version parameter
  that rule ignores have no test that fails without them;
- the constraints page does not reconcile the 2026-10-13 run of 20.2.1, where
  the bootstrap apparently completed, with the 20.2.1 source, which predicts the
  license-call refusal.

**Outcome:** each edge has a test that fails without its guard, or a recorded
reason it needs none. **Requires:** none. **Definition:** `Specified`.
**Exit evidence:** the named tests, mutation-checked, and
the reconciled page.

### B657

Package planning installs the `broker` root's packages (`openssl`) without
binding them to the RHEL signer or to the host's DVD tree, as the crypto-policy
package is bound. Found by the audit (`gap2-gap2-package-signer-trus-1-1`). No
trust boundary is broken today: every package is verified under its
repository's pinned signer, the IBM key is already trusted on the host after the
key import, and no IBM repository ships `openssl`. **Outcome:** plan packages
from the `broker` root must be RHEL-signed and sourced from the host's DVD tree,
or the package phase refuses `package-signature`. **Why parked:** the check adds
a new refusal without closing a demonstrated gap. **Requires:** none.
**Definition:** `Specified`. **Exit evidence:** a package-resolution test that
refuses an `openssl` candidate from a non-DVD repository.

### B658

Two contexts that share one libvirt host can both pass capacity admission
before either defines its guests, and together overcommit the host. Found by
the audit (`adapter-diagnostics-5`). **Outcome:** [storage](../ceph.md)
states the limit (admission is per context, and serializing concurrent applies
to one host is the operator's job), or a host-scoped reservation is held from
admission until the Machine blocks settle. **Why parked:** no spec requires more
today, the window is short, and a reservation is new durable cross-context
state. It joins M2 if pulled in. **Requires:** none. **Definition:**
`Needs definition`. **Exit evidence:** the spec sentence, or a two-context
admission test that refuses the second apply.

### B660

Assigned to M2c lane x61-ceph-fips on 2026-10-19 ([D166](#d166)), under
[B484](m2.md#b484). The in-tree route probe selects ECDSA synthetic keys from
the observed FIPS kernel state and keeps inherited crypto policy. Declared
and bound Ed25519 cluster keys refuse before host preparation. Host-independent
fixtures cover key selection, native policy mismatches and no-effect refusals.
Operator FIPS acceptance remains open with B484; this claims no provider
qualification. D166's ECDSA selection supersedes following an RSA cluster key.

### B661

The cephadm payload bound `MAX_PAYLOAD` (2 MiB) in the host runtime sits
close to the largest current payload (1,676,391 bytes). Found by the audit
(`gap2-gap2-release-matrix-nati-2-3`). **Outcome:** the bound is raised or
derived (for example 8 MiB, below the 16 MiB host read bound), and an oversized
payload refuses with a named size reason that states the measured size. It is a
structural bound, not a version rule under D163. **Why parked:** a future
refusal, not a defect today. **Requires:** none. **Definition:** `Specified`.
**Exit evidence:** a host-runtime test of a payload over the old bound that
passes and of one over the new bound that refuses with its size.

### B663

`authored_candidates` in `native_resolution.py` can leave extra goal jobs
for a glob subject. Found by the audit (`gap1-package-resolution-matri-3-5`).
**Outcome:** it marks only the chosen root (`package_install`) while still
surfacing DNF's own no-match text, so a glob subject leaves no extra goal jobs.
**Why parked:** the current outcomes are native DNF semantics or fail-closed
refusals, and refusing glob characters would be a Bootwright version grammar,
which D163 excludes. **Requires:** none. **Definition:** `Specified`. **Exit
evidence:** a resolution test with a glob subject that leaves one goal job.

### B664

The publisher key is always fetched from IBM's public site, so a mirror or
disconnected package source cannot supply it. Found by the audit
(`gap2-gap2-package-signer-trus-1-2`). **Outcome:** decide whether mirror or
disconnected IBM package sources are supported; if so, the publisher key
location is authored beside `ceph.packageRepository` while the code-pinned IBM
fingerprint stays the trust anchor, and the `el10cp` signer is recorded in
[knowledge](../../.agents/knowledge/ibm-ceph-version-constraints.md) once an
`el10cp` package is inspected. **Why parked:** it fails closed today and needed
a scope decision. **Assigned:** M2c lane x59-d4-host-pull-route on 2026-10-19
([D166](#d166)), which takes the key from the declared mirror under its
trust bundle. **Decided outcome:** D166 took the "if so" branch, and
[D169](#d169) (2026-10-20, the owner) chose "Add the field": a new
required field `ceph.packageRepositoryKey` whenever `ceph.packageRepository` is
authored, using that repository's scheme, host and port and its trust bundle,
with the key's fingerprint matching the code pin. Lane x59-d4-host-pull-route
adds it and stays the item's carrier; the item keeps its state until that lane
lands. **Implementation:** B493's x59-d4 consumes the authored key at the
repository origin and TLS anchor, preserves the pinned IBM fingerprint, and
freezes the key URL in core and host requests. Host-independent tests cover
foreign origins, missing/untrusted keys, no public fallback and mismatched
publisher fingerprints. The `el10cp` signer observation remains pending a
separate inspected package; this change qualifies no new package signer.
**Requires:** none; D166 and D169 answered the owner decision. **Definition:**
`Specified`. **Exit evidence:** a package-source test with an authored
key location whose fingerprint differs from the pin and refuses.

### B665

A trust-anchor change and a policy change both reach main without a mark in the
commit that carries them. Found by the audit (`gap2-gap2-package-signer-trus-1-3`,
`process-compliance-5` and `process-compliance-7`). **Outcome:** (1) a commit
that removes or changes a trust anchor (pinned digests, release pins, a fixed
source) names that change in its body; (2) `scripts/check-commits` requires a
`Refs:` line whose X, B and D tokens resolve under `specs/milestones` (or a
user-request form the [code-implementation skill](../../.agents/skills/code-implementation/SKILL.md)
defines), enforces the subject length and rejects attribution trailers, and a
local make target runs it over the integration range; (3) the skill states that
a policy or test-rule change lands in its own commit, apart from a mechanical
reformat. **Why parked:** process tooling, and history stays as it is.
**Requires:** none. **Definition:** `Specified`. **Exit evidence:** a
`check-commits` test per rejected form and a passing run over the range.

### B666

The libvirt firewall readiness barrier's apply reloads systemd on every run, and
destroy leaves some inert files behind. Found by the audit
(`substrate-firewall-4`). **Outcome:** apply reloads systemd only when a file or
link changed or the loaded unit is not the owned barrier; destroy removes stale
`.bootwright-firewall-*` staging files in `/etc/systemd/system` and syncs the
directory after a replace and a symlink; optionally destroy removes an empty
`virtnetworkd.service.wants` directory that Bootwright recorded creating. **Why
parked:** each leftover is inert and the reload is harmless, and fixing them adds
durable creation state for no behavioural gain. **Requires:** none.
**Definition:** `Specified`. **Exit evidence:** role unit tests for the
unchanged-apply no-reload case and for the staging cleanup.

### B667

Compact `cluster info` shows rows that do not apply to a cluster without
explaining them, and the `session` and `exec` help lists neither SSH flag. Found
by the audit (`cli-presentation-code-5`, which also carries
`cli-output-contract-6`). **Outcome:** compact `cluster info` shows one line,
once per cluster, that explains the not-applicable access rows; `session` and
`exec` compact help lists `--ssh-user` and `--ssh-id-file` and shows only the
accepted `-o` values. **Why parked:** the output matches [the output
spec](../cli/output.md) today, which has no help-placement clause, and the "More
detail" pointer keeps the flags discoverable. **Requires:** none.
**Definition:** `Specified`. **Exit evidence:** help and `cluster info` goldens.

### B668

`internal/cli/presentation_resolution.go` adds `--color=never` to the argv that a privilege handoff
repeats, so a refusal remedy can show a flag the operator did not type. Found by
the audit (`cli-presentation-code-6`). **Outcome:** the no-color preference
travels outside argv (for example a `privilege.Invocation` field) and a remedy
repeats only what the operator typed. **Why parked:** it is cosmetic, because the
repeated command is valid, and the fix changes the privilege invocation
contract. **Requires:** none. **Definition:** `Specified`. **Exit evidence:** a
remedy test run under `NO_COLOR`.

### B669

The CLI depends on `charmbracelet/x/ansi` and `go-colorful` besides a width
library. Found by the audit (`cli-presentation-code-7`). **Outcome:** consider
measuring before styling and depending only on `go-runewidth`, dropping the other
two. X103 still uses `ansi.StringWidth` and `ansi.Truncate`. **Why parked:** the
modules are documented and free of known vulnerabilities, and nothing is
defective. **Requires:** none. **Definition:** `Candidate`. **Exit
evidence:** the module list after the change and the `internal/cli` tests.

### B670

A structured export holds transient copies of a secret in Go strings. Found by
the audit (`cli-output-contract-9`). **Outcome:** structured exports are encoded
into an owned byte buffer that is cleared after the write, with no string copies,
or the [output spec](../cli/output.md) states that they hold transient
GC-managed copies. **Why parked:** no spec requires clearing output copies
([secrets](../secrets.md) governs confidential capabilities), and Go strings make
complete clearing impossible without a custom encoder. **Requires:** none.
**Definition:** `Needs definition`. **Exit evidence:** an export test that
inspects the buffer after the write, or the spec sentence and `make docs-check`.

### B671

Nothing asserts the block set of the `lab-services` plans. Found by the audit
(`examples-reorg-4`). **Outcome:** a plan test for
`examples/infrastructure-services/lab-services` asserts the apply plan's block set with
no consumed authorization, and that the destroy plan consumes no data-loss
authorization and plans only `remove-` and `close-` impacts. **Why parked:** test
hardening, with no defect shown. **Requires:** none. **Definition:**
`Specified`. **Exit evidence:** the plan test, mutation-checked.

### B672

The gate does not lint example YAML. Found by the audit (`examples-reorg-5`).
**Outcome:** `make docs-check` (or `make check`) runs the pinned yamllint with
`examples/.yamllint` over the tracked example YAML, or the
[formatting reference](../../.agents/skills/code-implementation/references/formatting.md)
records the deliberate omission. **Why parked:** gate hardening, and no example
fails today. **Requires:** none. **Definition:** `Specified`. **Exit
evidence:** the gate failing on a deliberately malformed example, then passing.

### B673

A test fails under concurrent gate runs: the controller-setup lease conflict in
`TestAnInterruptedPublicationLeavesAUsableStoreAndItsRetryConverges` (reported
in d9e67eb11, 2dc470bff, 7e3963d91 and 77e0be76c). The firewall access-time
case (ce9767849) is [B633](#b633)'s. Found by the audit
(`process-compliance-4`). **Outcome:** the test is deterministic under
concurrent gate runs, or its cause is recorded. **Why parked:** it does not
reproduce cheaply. **Requires:** none. **Definition:** `Needs definition`.
**Exit evidence:** repeated concurrent gate runs (`make -j check race quick
docs-check`) with no lease conflict.

**2026-10-21 observation:** the default parallel `make check` and an isolated
rerun of the unchanged baseline reproduced a different setup-checkpoint lease
failure. A serial full-package run with `GOMAXPROCS=1` passed. This is evidence
of recurrence, not a diagnosis or proof that serialization fixes the cause.
The full integration gate after temporary-directory cleanup remains unrun.

**2026-10-30 observation:** X225's integration gate reproduced the lease
conflict at `setup/refused/007-after-setup-state-rename` while `make check`
overlapped `make race`, with no workspace implementation changes. After the
race suite finished, the whole checkpoint test passed with `-count=1` and its
default parallelism. The integration commits record the complete gate rerun.
This recurrence and isolated pass do not diagnose or close the flake.

### B674

Releasing a Ceph host whose cluster block never started retains the owned
Bootwright host runtime tree for the cluster's fsid and any broker scratch a
killed pull left. Found by the audit (`gap2-gap2-ceph-destroy-observ-3-1`);
X102 fixes the impact text and the storage spec sentence, which now name what is
retained. **Outcome:** such a release removes the owned tree and the scratch.
**Why parked:** a new destructive host effect that needs its own removal proof.
**Requires:** [B491](m2.md#b491)'s teardown contract. **Definition:**
`Needs definition`. **Exit evidence:** a destroy test that removes only the
owned tree, and one that refuses a tree it does not own.

### B675

When the selected build declares `--disable-ibm-call-home` (the
`host-cephadm.call-home-opt-out` fact in `native_specializations.py`),
`spec.distribution.ibm.callHome: disabled` today refuses `call-home-unqualified` instead of
passing the flag. Found by the D163 sweep. **Outcome:** the flag is passed,
and the [storage API](../api/ceph.md) states it. **Why parked:** the fact
comes from the artifacts and not from a version label, so it is not a D163
removal, and it is a feature gap outside a defect slice. **Requires:** none.
**Definition:** `Specified`. **Exit evidence:** a seam test on a synthetic build
that declares the option, and the existing refusal kept for a build that does
not.

### B676

`spec.dependencyVersions.libvirt` admits a NEVRA pin
(`api/v1alpha1/environment.go`), while plan and the controller stage refuse
any pin (`internal/controller/selection.go`,
`internal/controller/clients/capability.go`). Found by the D163 sweep
(`D163-B13`). **Outcome:** the field either honours a NEVRA pin through host
`dnf` or leaves the grammar. **Why parked:** it is implementation scope, not a
compatibility rule. **Requires:** none. **Definition:** `Needs definition`.
**Exit evidence:** a controller test that installs the pinned package, or the
field's removal from the grammar and its spec row.

### B677

The `unsupported-release` refusal checks the verified-tunnel transport, a
sha256 digest image and bounded control-free strings; it checks no version.
Found by the D163 sweep. **Outcome:** the token is renamed
`unsupported-coordinates` across `ceph_core.py`, `ceph_cluster_protocol.py`,
`internal/storage/ceph/host.go` and `internal/storage/ceph/materials.go`. **Why
parked:** cosmetic, and it collides with the closed reason lists that the m2c
lanes edit; do it after m2c lands. **Requires:** m2c landed. **Definition:**
`Specified`. **Exit evidence:** the refusal tests under the new token and a
contract guard update if a content digest moves.

### B678

On a host whose os-release ID is not `rhel`, `native_resolution.db_path`
raises a bare `ValueError` that surfaces as `package-resolution`. Found by the
audit (`gap1-test-weakening-2-1`, residual); the OS gate itself is declined
under D163. **Outcome:** it refuses `package-platform` and names the
os-release ID. **Why parked:** it fails closed before any effect, and the value
is low. **Requires:** none. **Definition:** `Specified`. **Exit evidence:** a
resolution test on a non-rhel os-release that expects the named refusal.

### B679

X102's lanes and reviews left these edges, none reached by a known input:

- the pull broker's challenge parser refuses quoted-pair escapes inside a
  quoted string, so an `error_description` that a registry escapes would still
  refuse; the live answer of IBM's registry for an expired bearer, and of
  quay.io's redirect, are modelled from the RFC and the audit notes, not
  observed;
- native redaction now runs over the whole unbounded native output, and its
  lazy quoted-value pattern may be quadratic on one long line with many
  unclosed `password="` openers, where it ran on at most 64 KiB before; a
  secret ending in a lone surrogate still redacts the same text followed by a
  space, which over-redacts and fails safe;
- capacity reports unknown whenever any Machine is unproved and the total
  exceeds the available amount, even when the deficit is larger than the
  unproved Machine's whole declared quantity; and a permanent definition-proof
  failure (a foreign UUID, `nlink` other than 1, aliased disks, a disk on
  another filesystem) reports unknown with the repeat remedy, which may never
  resolve;
- the continuation test for a Ceph-only apply calls the admission directly and
  does not drive a whole lifecycle continuation through the real capabilities,
  and the take-back remedy test fakes an unreadable frozen request instead of a
  real older core request; `canTakeBack` still skips the contradiction check
  that `supersede` runs, as it did before X102; the removal branch
  (`RemainingSubset`) and the empty-owned-set branch of `canTakeBack` have no
  test;
- cephadm cannot redeploy the only MGR, so a repair with one declared MGR
  refuses `dashboard-manager`, and the image check of `supported_transport` is
  redundant after request validation on every dispatch path;
- the effect of the unflagged `md5` call in the license source on a FIPS-mode
  host is not documented, and the phrase "release-qualified" still names
  mechanics qualification in several spec sentences and in the Go refusal text
  of the native check, which D163 may want reworded.

**Outcome:** each edge has a test that fails without its guard, a measurement,
or a recorded reason it needs none. **Requires:** none. **Definition:**
`Specified`. **Exit evidence:** the named tests, mutation-checked, and the
recorded measurements.

### B681

X103's lanes and reviews left these edges, none reached by a known input:

- compact managed Ceph `cluster info` emits its Access group in several calls,
  so values start at different columns inside one visual group, and the 95-cell
  Certificate SHA-256 token overflows a 96-column row on a line of its own, which
  the never-split rule accepts;
- the progress presenter's `field()` never wraps, `internal/cli/output_controller.go`
  builds `field` with unkeyed literals, and `wrapDisplayText` yields a trailing
  empty line for a value that ends in spaces after an over-wide token, as it did
  before X103;
- the status headline test reads goldens instead of rendering each fixture, and
  the structured-output test decodes JSON output with a YAML decoder, which folds
  NEL; a JSON-only regression is better checked with `encoding/json`;
- the destroy guard cannot see whether the readiness unit file exists, so its
  remedy shows when the barrier is failed and not enabled; the fake's `Unit X not
  loaded.` text is taken from systemd's D-Bus manager source from memory, and no
  real host ran the removal path;
- `ansible-lint` warns `jinja[spacing]` for the multi-line `fail_msg` in
  `roles/substrate_libvirt_host/tasks/destroy.yml`, and the readiness wait's
  attempt count (31 at two seconds) follows ansible-core 2.21;
- the [operator guide](../../docs/operator-guide.md) section "M1 closing
  validation" still carries a closing framing and a present-tense X48 sentence
  although M1 is accepted.

**Outcome:** each edge has a test that fails without its guard, a measurement,
or a recorded reason it needs none. **Requires:** none. **Definition:**
`Specified`. **Exit evidence:** the named tests, mutation-checked, and the
recorded measurements.

### B682

Today the direct MGR dashboard serves the self-signed certificate that
[X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply) creates. The
native surface is `ceph dashboard set-ssl-certificate -i -` and
`set-ssl-certificate-key -i -`, followed by a MGR redeploy
([upstream dashboard module](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/dashboard/module.py)).

**Outcome:** a `tlsCertificate` binding for the dashboard, applied on stdin,
with the served leaf's SHA-256 proved. **Why parked:** D160 (OD7 (a)), kept by
D161; the management gateway already fronts the dashboard with a bound
certificate. **Requires:** an owner request. **Definition:** `Needs
definition`. **Exit evidence:** a plan test that the material never leaves the
binding, an apply test of the stdin-only path, and a live served-leaf check
under B484.

### B683

Dashboard roles reached through SSO map only onto system roles (upstream
[`map_to_system_roles`](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/dashboard/services/access_control.py)).
Custom roles therefore need local users (`ac-role-create`, and `ac-user-create`,
which reads the password from `-i`).

**Outcome:** typed roles and local users with passwords bound to Secrets. **Why
parked:** D160 (OD7 (a)), kept by D161; no API is approved. **Requires:** an
owner decision on the API. **Definition:** `Needs definition`. **Exit
evidence:** admission and binding tests, plus a live denied-access and role
check under B484.

### B684

`spec.ceph.mgrModules` refuses `prometheus` with or without a declared
prometheus service (`internal/storage/cephcluster/observability.go`); cephadm enables the
module only for a declared service. See the
[knowledge](../../.agents/knowledge/ibm-ceph-monitoring.md).

**Outcome:** a declared owner for the manager exporter without a Prometheus
service, or a recorded reason none is needed. **Why parked:** no consumer; an
ownerless module would break one owner per fact. **Requires:** none.
**Definition:** `Candidate`. **Exit evidence:** an admission test of the chosen
owner and a readiness test that reads `mgr services`.

### B685

`custom_sans` and `certificate_source` on `mgmt-gateway` refuse today, as do
`ssl` and `certificate_source` on the monitoring services
(`internal/storage/cephcluster/observability.go`); the
[native records](../ceph-native-api.md#observability-and-management-gateway-records)
say "Parked by D160 (OD26)". Without a binding, the gateway reports only the
cephadm-signed leaf's digest, and upstream certmgr documents no root CA
retrieval and never renews user-provided certificates. See the
[knowledge](../../.agents/knowledge/ibm-ceph-monitoring.md).

**Outcome:** these fields are realized, or each is recorded as refused, and a
cephadm-signed endpoint names its root CA. **Why parked:** D160 (OD26 (a)); no
documented native contract. **Requires:** none. **Definition:** `Needs
definition`. **Exit evidence:** admission tests per field and a live CA-chain
check under B484.

### B736

Bootwright installs and removes a controller resolver route for each managed
zone a context's DNSServer serves. Parked on 2026-10-20 (D172) because
[Q3](m3.md#m3a-open-decisions) keeps the route an operator prerequisite that
preflight proves. **Requires:** an owner decision. **Definition:** `Candidate`.
**Exit evidence:** a contract for host resolver ownership (a systemd-resolved
drop-in or none), a cross-context reservation class for a route, a
controller-global mutation lease, removal order after every dependent, and
tests. The v1 implementation's design is a lesson, not the target.

### B737

Two-node clusters with fencing: `controlPlane.fencing.credentials[]` with
`hostName`, `username`, `password`, `address` (containing `redfish`) and
`certificateVerification`. The 4.20 and 4.21 agent installers refused it; 4.22
accepts it with `DualReplica` in the Default feature set (the installer judges
the release), and the credentials are embedded in the image. Parked on 2026-10-20 (D172): no profile needs it.

### B738

Dual-stack clusters: two VIPs per slot of different families, the primary
family first in every network list, two service networks, and dual-family
managed libvirt networks. Parked on 2026-10-20 (D172): the one-family contract
of `specs/api/openshift-clusters.md` stands.

### B739

Workers added to an installed cluster by `oc adm node-image create` and
`monitor`: worker-only, on `baremetal`, `vsphere`, `nutanix` and `none`. The
installer's 48-hour image token is refreshed within its last 24 hours, which
invalidates earlier images, and CSRs need approval outside the initial
install. Parked on 2026-10-20 (D172): a day-2 change over a completed apply
(D153).

### B740

Install-time `ovs-bridge`, `ovs-interface` and `linux-bridge` interfaces in a
node's `networkConfig`, and a customized `br-ex` through the documented
MachineConfig writing `/etc/nmstate/openshift/<host>.yml`. Parked on
2026-10-20 (D172). **Definition needs:** a live test of `nmstatectl gc` and the
live image with each. On 2026-10-28 (D194) [B889](m3.md#b889) took the
post-installation half, a node address moved onto a localnet's bridge by the
cluster network block; the install-time bridges stay parked here.

### B741

Linux-bridge NADs (`type: bridge` on an NNCP `linux-bridge`, trunk access) and
overlay UDNs (`Layer2` or `Layer3`, primary or secondary). Parked on 2026-10-20
(D172): M3d's external networks are localnet ClusterUserDefinedNetworks,
pending the owner's [Q24](m3.md#m3a-open-decisions).

### B742

Tang disk encryption: `mode: tang` with `tangServers` as a JSON string of
`{url, thumbprint}`, every server reachable from the nodes before the install.
Parked on 2026-10-20 (D172): the API admits TPM2 alone.

### B743

One virtual cluster whose nodes sit on several libvirt hosts. The emulated
controller fetches the private image without verifying its server, so every
provider host must be the artifact server's (the container-cluster refusal "A
node off the artifact server's host"). Parked on 2026-10-20 (D172).
**Definition needs:** a verified emulator fetch or a per-host publication.

### B744

MetalLB BGP: `BGPPeer` (`metallb.io/v1beta2`) and `BGPAdvertisement`, one ASN
and router ID per cluster. Parked on 2026-10-20 (D172): the pools of
[B727](m4.md#b727) are L2. Revived as configuration of the MetalLB package
([B84](m4.md#b84)): a pool's `bgp` advertisement beside `l2`, peers in
`metallb.io/v1beta2 BGPPeer` with one `myASN` and router ID per cluster, and a
profile with a BGP router reachable on TCP 179. The `bgpBackend` choice (`frr`
or `frr-k8s`) is the operator's configuration and never a Bootwright default
(D173).

### B745

KubeVirt Machines reached through a Redfish endpoint (OpenShift 4.22's
KubeVirt Redfish, Technology Preview, control-plane VMs only; or KubeVirtBMC),
so the libvirt-style fetch path applies. Parked on 2026-10-20 (D172) because
the 2026-06-28 owner decision keeps `virtctl image-upload` (a DataVolume pull
needs host-to-controller egress, the token URL in the DataVolume spec and a CA
ConfigMap), because vSphere needs the port anyway, and because the facade
needs host-wide HyperConverged feature gates. **Exit evidence if revived:** a
facade arm behind the same port, its trust contract for the importer's fetch,
and a run.

### B746

Upload the agent image once per cluster and share it (a vSphere datastore file
attached read-only by every node; a KubeVirt source DataVolume cloned per
node). Parked on 2026-10-20 (D172) because per-node pushes keep ownership per
Machine, and the v1 implementation's shared source brought a stale-generation
race and an unbounded clone wait. **Revival needs:** evidence from the
[B731](m3.md#b731) or [B734](m3.md#b734) runs that per-node pushes are the
bottleneck. **Exit evidence:** measured push durations, a cluster-scoped owner
block, a bounded clone escape.

### B747

A cluster mixing push-arm nodes with fetch-arm nodes, such as the vendor's
control plane on KubeVirt with physical workers. The port dispatches per node
already. Parked on 2026-10-20 (D172) because no run qualifies it and the media
block would both publish and push. **Revival needs:** [B88](m3.md#b88) or
[B733](m3.md#b733) and a qualifying run. **Exit evidence:** projection and
capability tests of the mixed shape and a run.

### B748

An OKD cluster installed by the agent installer on the provider paths M3
covers. Parked on 2026-10-20 (D172) when M3's coverage was defined for
OpenShift only. **Revival needs:** OKD's release, installer, client and
base-image sources and their trust, a distribution arm of the installer inputs,
and an acceptance run; the refusal-table row lifts with that code.

### B769

One add-on instance removed online from a cluster that stays: operands before
their operator as each product requires, deletes by a raw DELETE whose
`DeleteOptions` pin the recorded `uid` (`oc delete` has no precondition flag),
positive absence of Subscription, CSV and namespace, a terminating namespace
reported from its conditions, and a report of the OLM footprint it cannot
invert (CSVs, InstallPlans, CRDs, dependency Subscriptions). Parked on
2026-10-20 (D173): M4 retires instances with their cluster, and
[D153](#d153) refuses changing a completed apply's selection. **Revive
when** the owner asks for day-2 removal; it needs a desired-state journey D153
does not admit today.

Package facts for that removal. ACM: remove `MultiClusterObservability`,
`DiscoveryConfig`, `AgentServiceConfig` and non-local `ManagedCluster` objects
first, because the hub's validating webhook refuses deletion while they exist;
deletion takes up to 20 minutes and its finalizer removes the multicluster
engine it created; CRDs, cluster roles and webhook configurations remain until
the documented cleanup script runs, and a later install of an earlier minor
over them can fail. GitOps: delete the bootstrap object first (it carries no
cascade finalizer), then `GitopsService/cluster` (which the documentation says deletes the
instances; garbage collection of the default instance is unverified), then the Subscription and CSV; the operator's instances are not
removed by removing the operator.

### B770

`bootwright preflight add-ons` reporting, with no effect, the cluster API
answering through the lent kubeconfig, a named CatalogSource `READY` and the
declared `startingCSV` in its channel. Parked on 2026-10-20 (D173): no check
is provable before the cluster exists, and apply-time gates name their
failures. **Revive when** an operator asks for the first pre-apply check.

### B771

Remove the `add-ons/_store/<name>/add-on.yaml` selection exception, its
marker, its reader bounds and its context-manifest rule now that no
registration journey exists; folds B365, whose hard-linked-marker defect
disappears with it. Touches the input reader and context records
(`specs/api/environment.md`, `specs/api/input.md`). Parked on 2026-10-20
(D173): the exception is dead surface now that B89 is retired, and no M4
package needs the input-reader rules removed. **Revive when** an input-reader
change is already open, or the exception causes a defect.

### B772

OLM v1 `ClusterExtension` as a second install shape for packages that meet its
criteria (MetalLB, GitOps and NMState on paper). No Manual-approval equivalent
was found (uncertain). Parked on 2026-10-20 (D173): ODF/FDF, ACM, MCE and
GitLab do not qualify, and own-namespace installs are Technology Preview.
**Revive when** OLM v1 installs a package this milestone covers, with a
documented approval step.

### B773

`globalPullSecretMerge` realized: read and rewrite `openshift-config/pull-secret`
under `no_log` with a `resourceVersion` precondition, refuse a different
existing entry for the registry, record registry, username and disposition,
remove only the exact recorded entry. Parked on 2026-10-20 (D173): its inverse
runs only with [B769](#b769), and the install pull secret carries entitlements
from first boot. **Revive when** an entitlement must be added after
installation.

### B774

A least-privilege identity for add-on work (for example a ServiceAccount token
minted per operation and revoked after it), keeping the installer's 10-year
`system:admin` certificate (no rotation entry was found; Kubernetes has no
client-certificate revocation) for installation only.
Parked on 2026-10-20 (D173): it needs an identity minted and revoked per
operation, and [B64](delivered.md#b64) lends the installer's kubeconfig today.
**Revive when** the owner asks for a scoped identity or a package needs
permissions the lent kubeconfig must not carry.

### B775

On a disconnected cluster the NMState package sets
`spec.probeConfiguration.dns.host` from a `probeDNSHost` configuration field
naming a host the managed resolver answers (the former B735 open question).
Parked on 2026-10-20 (D173): M3 refuses disconnected clusters. **Revive when**
M3 admits `spec.install.mode: disconnected`; it is one of three parked
disconnected-cluster items with [B778](#b778) and [B791](#b791).

### B776

The MetalLB package could forward for a pool on a secondary interface through
one `nmstate.io/v1 NodeNetworkConfigurationPolicy` per node setting
`ipv4.forwarding: true` on the named interface:
- the 4.22 documentation does it this way and its release notes list
  per-interface forwarding as new;
- the package would then require `nmstate`;
- a run must prove that the policy's partial interface edit keeps the
  interface's static address;
- it limits forwarding to one interface instead of every interface.

Parked on 2026-10-20 (D173): the cluster-wide `ipForwarding: Global` setting
serves every documented release. **Revive when** an operator refuses
cluster-wide forwarding, with a run on each release minor that operator uses.

### B777

Two clusters of one context whose MetalLB pools overlap on a shared layer 2
network would both answer ARP for one address. Refusing this needs a rule over
every `metallb` instance of the effective catalog. The package interface sees
one instance and its cluster's facts, so the rule would be an add-on admission
rule of [B752](delivered.md#b752)'s instance function. Parked on 2026-10-20 (D173):
no profile shares a network between two clusters' pools. **Revive when** a
profile shares one network between two clusters' pools.

### B778

Data Foundation on a disconnected cluster: the mirror set and pull-secret
checks derive from the selected bundles' `relatedImages`, never from a fixed
per-product list, because the bundles pull from IBM and several Red Hat
registry paths that differ per bundle; the operator mirrors the catalog and
names it through an owned `catalogSource`. Parked on 2026-10-20 (D173): M3
refuses disconnected clusters. **Revive when** M3 admits
`spec.install.mode: disconnected`.

### B779

`spec.network.connections.encryption.enabled` on the StorageCluster with the
Ceph-side `ms_client_mode`, `ms_cluster_mode`, `ms_service_mode secure` and
`rbd_default_map_options ms_mode=secure` and their daemon restarts (uncertain).
Parked on 2026-10-20 (D173): the Ceph-side secure messenger modes and daemon
restarts are unverified, and no M4 profile needs them. **Revive when** the
owner asks for it and a Ceph-side procedure is verified.

### B780

The `odf-console` plugin named by the CSV enabled in
`consoles.operator.openshift.io/cluster` `spec.plugins`. Parked on 2026-10-20
(D173): enabling it patches the cluster-owned console operator configuration,
which no package may touch, and readiness does not need it. **Revive when** a
cluster-owned object patch with an exact-entry receipt exists, as
[B773](#b773) would need.

### B781

An `acm` configuration listing other `ContainerCluster`s of the context that
the hub imports: per cluster, a namespace, a `ManagedCluster`
(`cluster.open-cluster-management.io/v1`) with `spec.hubAcceptsClient: true`, a
`KlusterletAddonConfig` and an import credential (uncertain: not re-verified).
The documented auto-import route places a kubeconfig or token of the imported
cluster in the hub (Secret `auto-import-secret`), which "A cluster's
administrator kubeconfig is never placed in a cluster"
([secrets](../secrets.md), [B753](m4.md#b753)) refuses; a least-privilege
import identity (beside [B774](#b774)) or the manual import-manifest route
would be needed. Cross-cluster edges (the imported cluster's install block), a
hub that "cannot manage a different hub" and the deletion webhook all follow.
Parked on 2026-10-20 (D173). **Revive when** the owner asks for fleet
registration at bootstrap.

### B782

`ManagedClusterSet`, a `ManagedClusterSetBinding` in `openshift-gitops` whose
name equals the set, a `Placement` and a `GitOpsCluster`
(`apps.open-cluster-management.io/v1beta1`, `spec.argoServer.cluster`,
`spec.argoServer.argoNamespace: openshift-gitops`, `spec.placementRef`) on a
cluster bound to both `acm` and `gitops`. The controller writes cluster
Secrets into `openshift-gitops` beside Bootwright's repository Secret. Parked
on 2026-10-20 (D173): it is fleet GitOps wiring, outside bootstrap. **Revive
when** [B781](#b781) is revived.

### B783

Either `DISABLE_DEFAULT_ARGOCD_INSTANCE: "true"` in the Subscription
`config.env` at creation (the operator then deletes any default instance), or a
Bootwright-created `ArgoCD` in an owned namespace with declared
`sourceNamespaces`, `spec.tls.initialCerts`, `initialSSHKnownHosts`, placement
and RBAC. The second is the route to [B784](#b784) and to a self-signed GitLab
(M4f). Parked on 2026-10-20 (D173): no M4 journey needs it, and disabling the
default later deletes it, so the choice is creation-time only. **Revive when**
an operator needs a GitOps instance other than the default.

### B784

SSH repository URLs (known hosts beyond the operator's defaults), HTTPS behind
a private certificate authority (entries in `argocd-tls-certs-cm`, an
operator-managed ConfigMap), and an `ApplicationSet` root (generators are
content; the ApplicationSet controller is outside the instance's `Available`
computation). Parked on 2026-10-20 (D173): Bootwright never patches
operator-managed ConfigMaps. **Revive when** [B783](#b783) lands an owned
instance, or the operator supports a creation-time field for them.

### B785

`spec.overrides.components` as a map of component name to enabled; names are
not validated by Bootwright because the hub webhook does and the set changes
per release. Parked on 2026-10-20 (D173): the operator is reported to complete
the list it is given (inference, unverified), which create-only comparison
would read as a difference on every continuation. **Revive when** a keyed list
comparison exists in the cluster object program or a run proves the hub
operator leaves the list as given.

### B786

GitLab through the operator's OpenShift release manifest (a 31-document file:
the CRD, service accounts, cluster roles, the webhook with cert-manager CA
injection, the Issuer and Certificate, an SCC, an IngressClass, and everything
in `gitlab-system`), which GitLab recommends over OLM and which places no
`olm.maxOpenShiftVersion` gate on cluster updates. The operator supplies the
file, frozen with the context as a [B90](m4.md#b90) manifest-set payload;
Bootwright embeds no version (D163). Parked on 2026-10-20 (D173): it needs
B90's payloads. **Revive when** B90 is revived.

### B787

Bootwright composes the chart's consolidated `connection` document (provider
`AWS`, region, the RGW endpoint, the keys, `path_style: true`) and the registry
storage document from a StorageObjectUser's produced keys and a
StorageObjectGateway's endpoint, placed through the produced-input source
[B760](m4.md#b760) delivers and a closed document format added to the Secret
placement; one key copy instead of the operator's two. Parked on 2026-10-20
(D173): the operator-supplied documents serve M4f. **Revive when** an operator
asks for RGW object storage without handling keys.

### B788

Git over SSH: Gateway API with an explicit controller (the operator pins Envoy
Gateway's controller name unless overridden), a `TCPRoute` for `gitlab-shell`
on port 22, and a LoadBalancer address (MetalLB, M4c). The bundled NGINX route
is deprecated and leaves Routes; it stays refused. Parked on 2026-10-20
(D173): Routes cannot carry SSH, and the path needs a Gateway controller
package and MetalLB on the cluster. **Revive when** an operator needs SSH to
GitLab.

### B789

CloudNativePG (which GitLab's end-to-end suite uses) and a Valkey operator as
built-in packages providing tokens `gitlab` would require. Parked on
2026-10-20 (D173): GitLab positions in-cluster stateful services as
non-production, retirement with a virtual cluster would destroy the data, and
no package research is verified. **Revive when** an operator asks for
in-cluster services and a package research pass qualifies them.

### B790

`global.edition: ce`: the chart supports it, the operator documents no edition
and the certified image list holds only `-ee` images. Parked on 2026-10-20
(D173): the combination is undocumented and unverified. **Revive when** a live
check shows the operator installs the Community Edition.

### B791

The bundle's `relatedImages` lists only the operator image and declares
disconnected `"false"`; operand images need per-subchart repository overrides
or cluster-level tag mirroring. Parked on 2026-10-20 (D173): M3 refuses
disconnected clusters. **Revive when** M3 admits disconnected clusters.

### B792

`gitlab-runner-operator` from the certified and community catalogs as its own
package, with registration tokens as placed Secrets. Parked on 2026-10-20
(D173): the GitLab Operator installs no Runner, and no M4 journey needs CI
execution. **Revive when** an operator asks for CI execution on the cluster.

### B793

Toolbox backups to object storage (an s3cmd configuration Secret) and the
Rails secret, which is not in a backup (uncertain), declared in custody so
that a new cluster can reuse a database and buckets that outlive a retired
one. Parked on 2026-10-20 (D173): the chart keys are unverified, and backup
content is day-2 data custody. **Revive when** an operator asks for data to
survive a cluster rebuild.

### B794

A `httpEndpoint` check arm probing a URL through the cluster's ingress (for
GitLab, the webservice readiness path) under the attempt's deadline, read from
the controller. Parked on 2026-10-20 (D173): a new check arm moves the program
version, and no M4 package needs it to exit. **Revive when** a package's
readiness cannot be proved from cluster objects.

### B795

A `caBundle` Secret placed and named in the chart's custom certificate
authorities, and TLS settings toward PostgreSQL (`global.psql.ssl`) and Redis
(`rediss`), for services behind a private CA (the custom-CA key is
unverified). Parked on 2026-10-20 (D173): the profile reaches its services
without a private CA. **Revive when** an operator's PostgreSQL, Redis or
object storage sits behind a private CA.

### B797

A StorageObjectBucket whose declared `policy` changes after its block
completed refuses `object-bucket-identity` today, and the operator puts the
new policy by hand outside Bootwright. Outcome: a day-2 policy update applied
through PutBucketPolicy with the same pinning and custody as the first put, the
owner's key pair read into the worker's memory only and a daemon sent nothing
unless it serves the bound leaf. The same treatment is considered for a
StorageObjectUser display-name change, which refuses `object-user-identity`
today. Parked on 2026-10-21 (X56): a day-2 change to a completed resource is a
state-contract change, and no milestone needs one. **Revive when** an operator
asks to change a bucket policy or a user display name without retiring the
context.

### B798

**Owner:** Verification tooling. **Kind:** defect. **Requires:** none.
**Definition:** Specified. The full gate passes `--num-workers 0` to the
locked ansible-test, whose implementation translates zero to pytest `-n auto`.
Its unit run therefore starts workers despite the documented serial intent.
Choose and test an explicit serial invocation without changing selected tests
or their pass/fail behavior. **Exit evidence:** a harness regression over the
actual generated pytest command and the full Ansible gate. **Why parked:**
this pre-existing verification behavior is recorded separately from M3's
product implementation; no failed test is reclassified as passed.

### B799

Investigate transient setup-lock refusals in the parallel publication
checkpoint harness. During B18 verification on 2026-10-23, two default
concurrency package runs reported `lifecycle.lease` (controller setup held)
when retrying interrupted setup publications; a full race-enabled run passed.
The failures included refused creation and killed/cancelled
`before-setup-state-rename` checkpoints. `lockSetup` currently releases its
flock by closing the descriptor, whereas root and context locks explicitly
unlock first. An unrelated checkpoint helper could briefly inherit the
close-on-exec descriptor between fork and exec; this is a hypothesis, not a
proved cause. Parked as a pre-existing defect outside B18. **Revival needs:**
a focused reproduction that identifies the remaining lock holder. **Exit
evidence:** parallel interruption/retry probes and a justified release fix,
considering an explicit unlock before close without weakening exclusion or
adding a retry that conceals a live owner.

### B857

**Owner:** Desired state. **Kind:** enabling. **Requires:** none.
**Definition:** Specified. B814's Work 5 named the operator-task check of
section 5.7 of the
[2026-10-26 spec audit](../../audits/001-spec/report-final.md); X171 landed the
reshaped API without it. **Outcome:** the eight tasks of that section (declare
an environment from an empty directory, fix a wrong-class reference, predict an
omitted field's effective value, select an unsupported implementation, narrow
scope to one cluster, continue after a transient failure, learn what remains
after destroy, recover from a credential or host-key failure) run once by one
human operator and once by an agent that reads only `validate` and
`render effective` JSON, each recording completion, corrections, time,
questions and wrong assumptions. **Exit evidence:** the results in a dated
record under `audits/`, and an item for each shape or remedy the run shows
misleads.

### B867

**Owner:** Storage. **Kind:** safety. **Requires:** a live Ceph cluster run.
**Definition:** Specified. Found in the lane R review of B829 on 2026-10-27.
A Ceph output's recapture identity detects a cluster bootstrapped again under
the same fsid by the monitor map's creation time
([produced material custody](../state-reconciliation.md#produced-material-custody)),
chosen without a live run. **Outcome:** a live run shows that the time `ceph
mon dump` reports stays the same across a monitor added and removed and a Ceph
upgrade, and changes when the cluster is bootstrapped again under the same
fsid; otherwise the rule moves to a value that does. **Exit evidence:** a
ledger row from that live run.

### B881

**Owner:** State reconciliation, with Substrate. **Kind:** safety.
**Requires:** [B880](m1.md#b880). **Definition:** Needs definition: whether
the adapter supervisor signals its descendants and grants a bounded grace
before its kill, and how that grace enters every deadline. Today a run
deadline or cancellation during the [firmware](../substrates.md#physical-firmware)
power cycle kills the worker before its ForceOff, leaving the machine
unproved, possibly on in firmware setup; the refusal's remedy stops it.
**Outcome:** the cycle's ForceOff is sent and polled after a cancellation or
deadline. **Exit evidence:** a supervisor test that cancels during the cycle
and observes the ForceOff.

### B882

**Owner:** Storage, with Machine and Managed OS. **Kind:** product.
**Requires:** none. **Definition:** Needs definition: how a
[provided-OS](../api/machines.md#machine) Machine joins a managed CephCluster
as an OSD, manager or gateway host. A provided Machine joins only as the
[stretch tiebreaker](../ceph.md#stretch-topology), which the e2e
[lab-5-nodes-ceph-stretch](../../e2e/lab-5-nodes-ceph-stretch/runbook.md)
case runs; any other provided node refuses at plan. Open: device and erase
claims on a host Bootwright did not install, its package prerequisites, and
removal that leaves the operating system. **Outcome:** a provided-OS Machine
carries OSDs in a managed cluster. **Exit evidence:** planning and host-trust
tests and an owner-run apply and destroy.

### B905

**Owner:** Substrate and Managed OS. **Kind:** product. **Requires:**
[B87](m1.md#b87). **Definition:** Needs definition: the identity channel a
Bootwright-installed Machine on KubeVirt proves itself through (a delivered
host key or the guest agent), its install seeds and the managed-OS boot path
over the pushed port. Today a KubeVirt Machine naming an install profile
refuses before registration ([selection](../substrates.md#selection-and-refusal)).
**Outcome:** a RHEL Machine installed on a KubeVirt provider. **Exit
evidence:** request goldens, refusal tests and an owner-run installation.

### B906

**Owner:** Substrate, with Add-ons. **Kind:** product. **Requires:**
[B87](m1.md#b87). **Definition:** Needs definition: how the provider proof
reads the effective VM state storage class (HyperConverged
`vmStateStorageClass`, then the default virtualization class, then the cluster
default) and refuses one that cannot migrate a persistent vTPM VM, and where
the class is declared (a `kubevirt` package field, [B732](m3.md#b732)'s
question 3b). Today a profile's `tpm` renders a persistent vTPM whose state
claim's migratability is not proved
([KubeVirt machine realization](../substrates.md#kubevirt-machine-realization)).
**Outcome:** a vTPM Machine is placed only where its host can drain it.
**Exit evidence:** proof tests over a fake API and a live drain of a vTPM VM.

### B907

**Owner:** Substrate. **Kind:** product. **Requires:** [B87](m1.md#b87).
**Definition:** Needs definition: the firmware specialization, keyed on an
observed fact and never on a release label (D163), that sets
`secureBoot: true` with SMM once a live run boots the agent image under it.
**Outcome:** KubeVirt Machines boot with Secure Boot. **Exit evidence:** a
live boot and request goldens.

### B909

**Owner:** Substrate. **Kind:** product. **Requires:** [B87](m1.md#b87).
**Definition:** Needs definition: whether a namespaced secondary
UserDefinedNetwork attaches through the NetworkAttachmentDefinition it
generates, which binding it takes and how the provider proof reads it. Today
the arm refuses an attachment of any kind but a ClusterUserDefinedNetwork or a
NetworkAttachmentDefinition before registration. **Outcome:** a KubeVirt
interface on a UDN. **Exit evidence:** selection and proof tests and a live
attachment.

### B925

**Owner:** Substrate and Container cluster. **Kind:** safety. **Requires:**
[B88](m3.md#b88). **Definition:** Needs definition: [Q20](m3.md#m3a-open-decisions)
option (b). Where the host's HyperConverged enables `declarativeHotplugVolumes`,
as the provider proof would read it, the eject also unplugs the media volume
from the running instance, so the claim holding the image, which embeds the
pull secret, is released without waiting for the next stop; a specialization
keyed on that observed gate, never on a release label (D163). Today the claim
stays until the instance next stops ([security](../security.md#network-remote-systems-and-privilege)).
**Outcome:** a released node holds no image claim. **Exit evidence:** proof and
eject tests over a fake host with and without the gate, and a live eject.

### B927

**Owner:** Container cluster and Substrate. **Kind:** product. **Requires:**
[B88](m3.md#b88). **Definition:** Needs definition: how the installation
proves an installer VIP of a KubeVirt cluster free before a fresh boot, from
the controller's route to the attachment's layer 2 or from the host, and what
refuses when it cannot. Today the VIP evidence names those addresses as not
probed ([container clusters](../openshift-clusters.md#installation)).
**Outcome:** a VIP another host answers refuses before boot. **Exit evidence:**
probe tests and a live run.

### B965

**Owner:** Specification maintenance. **Kind:** enabling. **Requires:** none.
**Definition:** Specified. `docsSectionExceptions` in
`test/architecture/docs_gates_test.go` grants ten level-2 sections a ceiling
above the section budget, up to 88812 bytes, and `docsLeafExceptions` grants
the leaves above the leaf budget; the first split targets are
[Installation](../managed-os.md#installation) and
[Plan and execution](../state-reconciliation.md#plan-and-execution).
**Outcome:** each excepted section and leaf is split into named leaves within
its budget, the index routes to them, and both exception maps are empty.
**Exit evidence:** `make docs-check` with both maps empty, and a link check
that no anchor or citation of a split section broke.

### B966

**Owner:** Ansible collection. **Kind:** enabling. **Requires:** none.
**Definition:** Specified. `ROLE_MISFITS` (5 roles) and `PLUGIN_MISFITS` (14
plugins) in `test_collection_names.py` are the exact allowance that
[playbooks](../architecture.md#playbooks) states only shrinks. **Outcome:**
the 5 roles and 14 plugins are renamed to the role and plugin patterns,
including playbook imports, role FQCNs, Go bindings and plugin references, and
both lists are empty. The rename moves the automation digest only, so it is
delivered by a build and setup, with no destroy. **Exit evidence:** the
collection unit tests with both lists empty, the architecture suite and the
contract goldens.

### B967

**Owner:** Verification tooling. **Kind:** enabling. **Requires:** none.
**Definition:** Specified. The
[Python reference](../../.agents/skills/code-implementation/references/python.md#repository-tools)
names `scripts/quick-test` and `scripts/check-commits` as shell exceptions
until their logic moves behind a tested launcher; `scripts/runtime-lock`'s host
`python3` stays as documented. **Outcome:** both scripts only pin and execute a
launcher under `scripts/tools/` with its tests beside it, and the exceptions
leave the reference. **Exit evidence:** the launchers' `*_test.py` suites,
`TestCheckCommitsRequiresAContractLineWhereAContractCanMove` and `make quick`.

### B968

**Owner:** CLI. **Kind:** enabling. **Requires:** none. **Definition:**
Specified. `kindProseExempt` in `test/architecture/family_neutral_test.go`
exempts `internal/cli`, whose help and output strings name kinds directly.
**Outcome:** those strings come from the family registry, so a second family
changes no CLI literal, and the exemption is gone. **Exit evidence:** the
family-neutral check over `internal/cli` and regenerated help goldens, plus a
test that adds a synthetic second storage family kind and sees it named.

### B969

**Owner:** Storage. **Kind:** enabling. **Requires:** none. **Definition:**
Specified. `TestEveryReferenceFieldHasAnEdgeRowOrAReason` gives six storage
references that emit requirements a reason instead of a
[reference edges](../state-reconciliation.md#reference-edges) row because no
example plans them. **Outcome:** examples or fixtures plan each edge, the rows
exist and the reasons are gone. **Exit evidence:** the edge-catalog test with
the six rows.

### B988

A `ClusterAddonProfile` that carries `spec.packages` as a named preset of
built-in packages: a binding selecting the profile inherits its packages, a
binding's own member overrides the profile's, two profiles supplying one
package refuse, and `render effective --origins` names the profile. Parked
on 2026-10-30 ([D210](#d210)): Environment kind defaults and input templates
serve today's estates, and [Q26](m4.md#m4a-open-decisions) (a) excluded
packages from profiles. **Revive when** an estate declares one package set on
three or more cluster shapes; the decision is [Q137](m4.md#m4a-open-decisions),
and the expansion, cycle and last-package tests of
[admission](../api/addons.md#admission-diagnostics) extend with it.

### B990

An authored add-on ([B90](m4.md#b90)) whose custom resource is
cluster-scoped: the cluster object program first proves, read-only, that the
kind is cluster-scoped and that its group is outside the reserved groups,
then creates the resource create-only under ownership receipts. Parked on
2026-10-30 ([D211](#d211)): B90's first slice admits only namespaced
resources in the add-on's own namespace, because the program cannot prove a
kind's scope today. **Revive when** an operator needs a cluster-scoped custom
resource from an authored add-on.

## Audit remediation (2026-10-26)

The [2026-10-26 spec audit](../../audits/001-spec/report-final.md) ordered its
recommendations into a roadmap (its section 14). Slice X160 corrected the
specs and skills where no owner decision or code was needed; every remaining
recommendation is parked above as [B800](delivered.md#x161--audit-lane-a-harness-and-guards) to [B836](delivered.md#x164--audit-lane-e-store-durability-and-controller-records), one item per
roadmap step or batch, and the Not yet met lines in the specs point to them.
Like every parked item, these authorize nothing: a lane, an item or a set of
lanes runs on an explicit request, which attaches its items to a slice.

### Audit remediation lanes

A lane is an ordered sequence of items that touch the same code; lanes marked
as not concurrent edit shared packages. An item's parts that wait for an owner
decision stay open while its other parts land.

| Lane | Items in order | Starts after | Not concurrently with | Owner decisions |
| --- | --- | --- | --- | --- |
| A Harness and guards | B800 (X161), B801 (X161) | none | none | Q62, Q63, Q64 |
| B Physical targets and shared hosts | B802 (X165), B803 (X165) | B801 | D, E, G, I | Q65, Q66, Q67 |
| C Storage retirement | B804 (X167), B835 (X167) | none | F, G | Q68 |
| D Credentials and trust | [B805](delivered.md#x182--audit-lane-n0-remaining-parts-to-their-owners-and-bmc-wording), B817 (X166), [B829](delivered.md#x181--audit-lane-r-private-installer-material-and-recapture-identity), B831 (X166) | none | B | Q70, Q97, Q98, Q99, Q100, Q101, Q116 |
| E Store durability and controller records | B806 (X164), B836 (X164), B832 (X164) | B801 | B, J | none |
| F Truthful admission | B807 (X168) | none | C, I | Q71 |
| G Extension architecture | [B808](delivered.md#x179--audit-lane-q1-per-object-claims-and-nested-references), B811 (X169), B809 (X169), B810 (X169), B812 (X169) | B800 to B807 | B, C, I, J, K | Q72, Q73, Q74, Q75 |
| H Cross-build continuation | B813 (X163) | B801 | none | Q76 |
| I API shape and grammar | B814 (X171), [B826](delivered.md#x179--audit-lane-q1-per-object-claims-and-nested-references), B827 (X171), B828 (X171), B824 (X171), B821 (X171), B837 (X171) | B808 | B, F, G | Q77, Q78, Q79, Q80, Q81, Q82, Q83, Q84, Q85, Q86, Q87, Q88, Q105, Q106, Q107, Q110, Q111, Q112, Q113, Q114, Q115, Q118 |
| J Reconciliation rules | [B816](delivered.md#x182--audit-lane-n0-remaining-parts-to-their-owners-and-bmc-wording), B830 (X170) | B800 to B807 | E, G | Q92, Q93, Q94, Q95, Q96, Q117, Q118 |
| K Language and adapter contracts | B818 (X162), B834 (X162) | none | G | none |
| L Corpus structure and skills | B815 (X173), B819 (X173), [B820](delivered.md#x182--audit-lane-n0-remaining-parts-to-their-owners-and-bmc-wording), B822 (X173) | B801; B822 after B813 and B818 | none | Q89, Q90, Q91, Q102, Q103 |
| M CLI and examples | [B825](delivered.md#x177--audit-lane-t-inherited-flags-and-cluster-preflight), B833 (X172), B823 (X172), B842 (X172) | none | none | Q104, Q108, Q109 |

### Lane order and progress

The owner's 2026-10-27 request to implement the remaining items, with
[D188](#d188)'s answers, adds lanes 14 to 24. [D182](#d182) runs the lanes in this order, from the harness and the
low-level contracts to the operator-facing surface, one at a time. Each lane
is one branch, `lane-<letter>`, in the worktree
`../bootwright-worktrees/lane-<letter>`, and lands on main as one commit that
also updates this table.

| Order | Lane | State |
| --- | --- | --- |
| 1 | A Harness and guards | merged (X161) |
| 2 | K Language and adapter contracts | merged (X162) |
| 3 | H Cross-build continuation | merged (X163) |
| 4 | E Store durability and controller records | merged (X164) |
| 5 | B Physical targets and shared hosts | merged (X165) |
| 6 | D Credentials and trust | merged (X166) |
| 7 | C Storage retirement | merged (X167) |
| 8 | F Truthful admission | merged (X168) |
| 9 | G Extension architecture | merged (X169) |
| 10 | J Reconciliation rules | merged (X170) |
| 11 | I API shape and grammar | merged (X171) |
| 12 | M CLI and examples | merged (X172) |
| 13 | L Corpus structure and skills | merged (X173) |
| 14 | N Race coverage (B841) | merged (X174) |
| 15 | O Store bounds and run scratch (B572, B389) | merged (X176) |
| 16 | Q3 Ceph object and replica refusals (B849, B838) | merged (X175) |
| 17 | S Libvirt host evidence and routes (B845, B843, B840) | merged (X178) |
| 18 | P Removal views and host claims (B846, B839, B844) | merged (X180) |
| 19 | Q1 Unclaimed objects and references (B808, B826) | merged (X179) |
| 20 | Q2 Family accessors and name bounds (B848, B850, B847) | merged (X183) |
| 21 | R Installer material (B829) | merged (X181) |
| 22 | T Context-free commands and preflight (B852, B825) | merged (X177) |
| 23 | U Unrealized fields (B853 to B856) | merged (X184) |
| 24 | N0 Remaining text to owning items (B805, B816, B820, B851) | merged (X182) |
| 25 | V1 Ceph replica defaults and derived names (B859, B860, B861) | merged (X185) |
| 26 | V2 Test and CLI defects (B862, B863, B865, B866) | merged (X186) |
| 27 | V3 Known-hosts and claim kinds (B869, B870, B871, B868) | merged (X188) |
| 28 | W1 Preflight infra and all, render to a path (B858) | merged (X191) |
| 29 | W2 Adapter warnings (B864) | merged (X187) |
| 30 | Y1 NVMe-oF pool and replica wording (B872, B873) | merged (X193) |
| 31 | Y2 Parallel test flakes (B874, B875) | merged (X192) |

To continue, take the first row not `merged`. If its branch exists, resume it:
each finished item is a `wip` commit whose body names what remains, and the
branch log is the progress record. Otherwise create the branch from main.
Implement the lane's items in order under
[code-implementation](../../.agents/skills/code-implementation/SKILL.md),
leaving every part that waits for an owner question open. Before landing: run
`make check` (and, for the safety class, `make race` and an independent
review), rebase onto main, squash the branch into one commit whose body lists
each item and gate, move each completed item to a delivered record of the
lane's slice, trim a partly delivered item to what remains, set the row here
to `merged (X<n>)`, and fast-forward main. Pushing waits for the owner.

### Audit remediation decisions

Questions for the owner, numbered after M4a's Q61. The owner answered Q62 to
Q118 on 2026-10-26 ([D183](#d183)); each answer is applied by the item it
blocks. Q119 to Q134 come from the
[consolidated follow-up 001](../../audits/001-spec/follow-up/001/report-final.md)
and are open; an answer that adopts new work opens its item.

| ID | Question | Options | Recommendation | Blocks | Status |
| --- | --- | --- | --- | --- | --- |
| Q62 | Should a Codex project hook call scripts/hooks/stop-check, which needs a narrow .codex/hooks.json un-ignored in the tracked tree? | (a: add it) Codex sessions get the same advisory end-of-turn focus check; .gitignore gains one exception.; (b: leave .codex/ untracked) Only Claude Code runs the hook; Codex users configure their own. | **Answered by D183: a.** Recommended: The report (10.4) leaves it to the owner without a recommendation; non-blocking for B800's docs test. | [B800](delivered.md#x161--audit-lane-a-harness-and-guards) | answered by [D183](#d183) |
| Q63 | How should the WIP limit of two active slices (D158) be reconciled with the five slices now in progress (X61, X118, X123, X130, X143), where X130 (M3e, D181) has no recorded exception? | (a: keep the limit and per-decision exceptions) Record an exception for X130 or pause one slice; the scope rule stays as written.; (b: a limit per program or milestone) Rewrite the scope rule to count active slices per program; the standing exceptions in D134, D143, D159, D172 and D173 retire.; (c: retire the WIP limit) Delete the limit and its exceptions; the Active slices table alone governs authorization. | **Answered by D183: b.** Recommended: The audit leaves the WIP limit to the owner (P0-B exit); it suggests a rule current practice satisfies, such as a per-program limit, but does not choose. | [B801](delivered.md#x161--audit-lane-a-harness-and-guards) | answered by [D183](#d183) |
| Q64 | Does matrix row U03 (replicated size equal to the OSD-host count refused, citing IBM's colocation page) stay as an admission refusal under D159 clause 15? | (a: keep) U03 stays tested, recorded as a native replica-safety rule (three replicas need four hosts) rather than a support statement.; (b: remove) Admission stops refusing size equal to host count; the U03 row, fixture and admission check are removed by a code change. | **Answered by D183: a.** Recommended: The report routes this to the owner without a recommendation; its basis may be a replica-safety rule rather than a vendor table. | [B801](delivered.md#x161--audit-lane-a-harness-and-guards) | answered by [D183](#d183) |
| Q65 | How does a removal observation that cannot tell its postcondition from an unreadable target resolve (state-reconciliation.md Resolution outcomes, against security.md's unknown-stays-unknown rule)? | (a: delete the clause) An unreadable removal observation stays unknown and only an operator resolution moves it; smallest edit, more manual resolutions, teardown can strand.; (b: typed inconclusive result) A new effect state distinct from no-effect permits one retry only for a capability declaring the removal idempotent, with ownership proved from durable records and a re-read before any effect; one table row, one capability field, one test, and one sentence in security.md. | **Answered by D183: b.** Recommended: The report gives both without choosing (14.8); section 8.B3 calls B the smallest correction that keeps replay-safe teardown. Either way the negative test is the same. | [B802](delivered.md#x165--audit-lane-b-physical-targets-and-shared-hosts) | answered by [D183](#d183) |
| Q66 | How is a private Machine that the network restart powered off before (or after) its disk boot read on recovery (managed-os.md 'the one window', currently 'positive no effect')? | (a: relabel) Read the window as a positive partial realization of this operation's own installation, retried by the same frozen request behind the boot proof, and name the cases it covers including power-off after disk boot.; (b: durable intent) Record durably before power-off that installation was proved and disk boot is intended; recovery takes the disk-boot identity path or stays unknown, never re-running the installer. | **Answered by D183: b.** Recommended: The report records no choice; it calls the relabel the smallest fix, while durable intent removes the reinstall. | [B802](delivered.md#x165--audit-lane-b-physical-targets-and-shared-hosts) | answered by [D183](#d183) |
| Q67 | Should an orphan-acknowledged context deletion keep its exclusive host keys as an abandoned/<name> tombstone instead of releasing them at once (amending D38)? | (a: keep D38) Keys release at the deletion; the orphan confirmation names the keys it frees and the reinstall hazard (spec applied, code in B803).; (b: tombstone) Exclusive entries become an abandoned/<name> holder that refuses conflicting claims until a new confirmed 'bootwright controller release --abandoned <name>'; shared media entries still drop at once. | **Answered by D183: a.** Recommended: Arbiter and lanes: A's warning now; B only if the owner reopens D38. | [B803](delivered.md#x165--audit-lane-b-physical-targets-and-shared-hosts) | answered by [D183](#d183) |
| Q68 | Confirm the D143 clause 6 reading: does retiring a Ceph cluster (or StorageExport) with its virtual hosts require that no consumer of its exports survives the removal? | (a: refuse) Removal refuses before registration naming the surviving consumer and export until B491; a mixed context (virtual Ceph, physical OpenShift) has no full destroy until then (the specs state this refusal as the interim safe reading).; (b: named impact plus acknowledgement) Removal proceeds after an acknowledgement naming the surviving consumer, whose PVs and Secrets then point at deleted storage. | **Answered by D183: a.** Recommended: Roadmap P0-E and R2-02-01 recommend A with a plan-time topology flag; F10-04's refuter and calibrator prefer B as smaller. | [B804](delivered.md#x167--audit-lane-c-storage-retirement) | answered by [D183](#d183) |
| Q69 | Should B659, B662 and B491 become prerequisites of M2d's B485 exit, or does B485 close with the physical recovery limitation recorded? | (a: prerequisites) B485 Requires gains B659, B662 and B491; M2d cannot complete until interrupted physical deployments have exits, which first needs their protocol decisions.; (b: record the limitation) B485 may close with the physical profile documented as an incomplete recovery profile (text applied now); the exits stay parked. | **Answered by D183: a.** Recommended: The audit (14.8) offers both and keeps the recovery protocols as owner decisions; it does not choose. | [B485](m2.md#b485) | answered by [D183](#d183) |
| Q70 | Should continuation and removal be able to present the current version of a rotated access credential (BMC/Redfish, KubeVirt kubeconfigRef, vCenter, registry login) instead of the bound one, and through which recorded gate? | (a: explicit recorded rebind exit) A third exit beside D37 for access credentials only, gated by a flag or the --authorize token model and recorded as a new binding version; ownership proofs still run and effect-material Secrets refuse it.; (b: keep today's rule) Removal always presents the bound version; a rotated credential strands destroy, leaving restore-keyring or --purge --allow-orphans. | **Answered by D183: a.** Recommended: The report recommends A, gated by the --authorize model and named apart from B181's bounded-run rebind, keeping D37 and D85; the owner decides the flag. | [B805](delivered.md#x182--audit-lane-n0-remaining-parts-to-their-owners-and-bmc-wording) | answered by [D183](#d183) |
| Q71 | Refuse the four inert ContainerCluster endpoint fields (port, scheme, prefixLength, interfaceNetworks) at plan on the agent path, or remove them from the schema until a consumer exists? | (a: refuse at plan) Fields stay admitted; an authored non-native value refuses at its path with a refusal row per field (spec states this).; (b: remove the fields) The endpoint shape loses the four fields; no refusal rows; examples and goldens change. | **Answered by D183: a.** Recommended: Refuse at plan now (roadmap P0-H); removal stays an owner option. Non-blocking: B807 implements A unless the owner chooses B first. | [B807](delivered.md#x168--audit-lane-f-truthful-admission) | answered by [D183](#d183) |
| Q72 | Which mechanism scopes an Environment: the source directory, spec.resources and cluster lists all narrow scope today? | (a) Remove spec.resources; the Environment directory is the scope and cluster lists are the only narrowing, documented with an example.; (b) Keep spec.resources and drop cluster lists, which makes the hostClusterRef retention edge moot.; (c) Keep both, with one tracked example using spec.resources and a one-sentence rationale in environment.md. | **Answered by D183: a.** Recommended: Decide together with hostclusterref-retention-edge; the report's first option is (a), and the lanes expect cluster lists to stay. Either way the excluded-reference refusal names the selection field. | [B812](delivered.md#x169--audit-lane-g-extension-architecture) | answered by [D183](#d183) |
| Q73 | If cluster lists stay, does selecting a nested cluster retain its KubeVirt provider's hostClusterRef ContainerCluster as a root with its own closure? | (a) Add the closure row, effective when B732 lifts the KubeVirt refusal, with a multidc selection test as B732 exit evidence.; (b) Moot because cluster lists are dropped (environment-scope-mechanism b). | **Answered by D183: a.** Recommended: a if cluster lists stay; record the selection test as B732 exit evidence either way. | [B812](delivered.md#x169--audit-lane-g-extension-architecture) | answered by [D183](#d183) |
| Q74 | Does an authored cluster list retain Bootwright-installed Machines (and their provider closure) that are nodes or hosts of no declared cluster, instead of only warning? | (a) Retain them as roots; the validate warning covers only what stays excluded.; (b) Warn only, as the spec states today. | **Answered by D183: a.** Recommended: a (calibrator), decided with environment-scope-mechanism; the warning stands until then. | [B812](delivered.md#x169--audit-lane-g-extension-architecture) | answered by [D183](#d183) |
| Q75 | When an attachment reaches a cluster root that an authored list omits, does validate warn, refuse, or retain the full closure? | (a) Deterministic validate warning naming the list to extend (spec today).; (b) Validate error naming the list to extend, since a completed apply's selection cannot change (D153).; (c) Retain the full closure of the omitted root. | **Answered by D183: b.** Recommended: b (calibrator); the warning is the interim minimum. | [B812](delivered.md#x169--audit-lane-g-extension-architecture) | answered by [D183](#d183) |
| Q76 | Does a removal under another build compare an effect-identity part of the content digest with the build in hand (amending D145(4))? | (a) Keep D145(4): removals compare no digest and rely on identity values travelling in the frozen request (stated in the spec now).; (b) Split the digest into effect-identity and mechanism parts; a removal refuses lifecycle.state before resolution on an effect-identity mismatch, naming the registering build and the deletion exit. | **Answered by D183: a.** Recommended: a now (calibrator, smallest); b presented to the owner as an amendment of D145(4). | [B813](delivered.md#x163--audit-lane-h-cross-build-continuation-guards) | answered by [D183](#d183) |
| Q77 | How does a non-OpenShift distribution enter ContainerCluster, and does B748's OKD arm follow the same path? | (a) A distribution-neutral envelope (nodes with neutral roles, endpoints, networks, proxy/NTP/registry, trust) plus a presence union of distribution arms starting with spec.openshift; node binding reads a distribution-declared node capability; role master becomes control-plane and endpoint source.type openshift becomes installer (section 4.3).; (b) One kind per distribution family, with the generic touch points each new kind implies listed.; (c) Confirm OpenShift and OKD only in project.md and defer the question. | **Answered by D183: b, as OpenShiftCluster.** Recommended: Record one decision now, linked from api/openshift-clusters.md, stating node binding and B748 alignment; the report's scenario analysis favors a, which is cheapest before 1.x. M3/M4 keep the flat shape until decided. | [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q78 | How are the Ceph-only Storage* child kinds bound to Ceph? | (a) Rename the children to Ceph* (CephPool, CephClient, CephRBDImage and the rest), keeping StorageCluster neutral; revisits D133's naming baseline and touches examples and goldens.; (b) Keep the names; api/ceph.md states they are the Ceph arm's kinds, admission refuses a child whose cluster's type is not ceph, a new storage type brings its own kinds, and StorageExport is classified. | **Answered by D183: a, with CephCluster.** Recommended: b (verifiers and calibrator); a only as an explicit owner decision revisiting D133, which the report prefers while renames are cheap. | [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q79 | Does Machine carry substrate facts in a per-substrate arm mirroring the InfraProvider arm? | (a) Full arm: spec.substrate {providerRef, baremetal/libvirt/kubevirt/vsphere} with admission requiring the arm to match the provider.; (b) Narrow arm: NIC names and neutral root hints stay top level; only bmc, MACs, wwn/hctl/serial admissibility, profileRef, failureDomainRef, template and interfaceAttachments move into arms.; (c) Keep the flat shape and derive per-substrate admissibility from one substrate-arm registry table. | **Answered by D183: b.** Recommended: b, decided before B87/B99 revive the arms; D44 and D94 hold under every option. | [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q80 | Are the Machine.capabilities role tokens (openshift-node, ceph-node, ceph-arbiter) dropped and container-runtime and libvirt derived from placements? | (a) Drop the three roles and derive the prerequisites at once; remove the field or keep the remainder as hostPrerequisites (section 4.3).; (b) Staged: first state the ceph-node rule in the storage API spec (or drop it) and drop ceph-arbiter; then derive container-runtime and libvirt and remove the roles, keeping an authored path for the controller Machine.; (c) Keep the field and its role tokens. | **Answered by D183: a.** Recommended: b; step one needs no shape change beyond removing a value nothing reads; update controller.md and the environment.md controller refusal table. | [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q81 | Which single testable rule decides between a presence arm and a type, method or implementation enum, and which current idioms change? | (a) State the rule, drop StorageCluster.spec.type, fold install.method into install.agent presence and make the service implementation an arm; an architecture test enforces it.; (b) Same rule, but StorageCluster gets an engine arm usable for external management before type goes, and the service implementation stays an enum.; (c) Keep the four idioms and only state the criterion. | **Answered by D183: one rule with its architecture test; services keep the implementation enum.** Recommended: b, recorded with current and proposed examples; the rule then goes into api.md#unions with its architecture test. | [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q82 | Does api.md state that a value with exactly one valid derivation is derived, not authored and compared (bindAddress, generated SANs, node fqdn)? | (a) State the principle; fold bindAddress into B333; derive generated-certificate SANs from consumer endpoints; derive node fqdn.; (b) As a, but node fqdn keeps its explicit optional override and only examples stop authoring derived values.; (c) Keep authored-and-compared copies and only document the collision trap. | **Answered by D183: b.** Recommended: b; pre-1.x no-compatibility removes B333's digest-window reason; reread ADR 0035/0036 before touching fqdn. | [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q83 | Which single-valued storage references become derived: mdsServiceRef, the SMB share cluster_id, and SMB resourceRefs versus the native refs? | (a) mdsServiceRef dropped or optional (mds.<name>), cluster_id optional from smbClusterRef, one of resourceRefs or native refs derived from the other; a disagreeing authored value still refuses at its field.; (b) mdsServiceRef optional, not dropped; native refs stay the authored home (D133) and resourceRefs become optional, derived by native ID.; (c) Keep every value authored and agreement-checked. | **Answered by D183: b.** Recommended: b; the 6-node services example must validate with the leaves removed. | [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q84 | Where does an admitted but unrealized declaration refuse by default, and what single criterion allows an admission-time refusal? | (a) api.md#compiler-boundary names plan-time lifecycle.unsupported as the default and one criterion for admission-time api.invariant (no build has any consumer, as rescue under D29); kind pages link it.; (b) Leave the refusal point per field. | **Answered by D183: a.** Recommended: a. | [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q85 | Does validate warn, with a stable diagnostic code in text and -o json output, for every admitted but unrealized declaration, backed by one api.md table held to the lifecycle.unsupported set by a test? | (a) Add the warning code and the code-checked table (a new public diagnostic).; (b) No warning; the plan-time refusal is the only signal. | **Answered by D183: a.** Recommended: a as revision 2 of the report asks, taken as a separate UX decision from unrealized-refusal-point. | [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q86 | Which single rule replaces the StorageCluster name limits that vary with declared services (44, 39, 37, 26)? | (a) One unconditional metadata.name limit (26 or less) on the API page; the per-service caps link to it.; (b) One block-identity rule: an identity over 63 characters replaces its overflowing segment with a fixed-length hash token, so public limits stop depending on internal prefixes. | **Answered by D183: b.** Recommended: b (report); either way no name limit depends on declared services, and each kind gets one name rule. | [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q87 | Do the section 4.3 before-1.0 renames that hang on these shape decisions (Ceph*, hostPrerequisites, control-plane, installer) land in one coordinated slice with the reshapes and the L2 renames? | (a) One batched slice per surface with retired-path refusals naming each replacement, landed while few live contexts exist.; (b) Each rename lands with its own shape change, as separate slices.; (c) Keep the current names. | **Answered by D183: a.** Recommended: a (report 5.6); every moved request or block identity stops D142 continuation, so affected live operations complete or are destroyed first. | [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar), [B824](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q88 | Is the tracked lab-kubevirt example the external-endpoint profile (lab-kubevirt-external) or the first-ranked baremetal-VIP profile of Q19? | (a) Rename the example, golden and README to lab-kubevirt-external, reconcile its sizing with the coverage row, and fix the B731 note.; (b) Change the coverage row to match the tracked example. | **Answered by D183: a.** Recommended: a; it matches Q19 (a) and pre-empts no answer. | [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q89 | Is project.md Product scope split into a committed portfolio and an extension envelope that gates no milestone until promoted? | (a) Two labelled lists, each committed entry linking its milestone; no milestone page changes.; (b) a plus moving family-only arms (M1f vSphere, M3f) out of their gating milestones; amends D158 and the D172 Q1 reading.; (c) Keep one list; every family item keeps gating its milestone. | **Answered by D183: a.** Recommended: a now; b as a separate owner option; an envelope-only arm needs owner promotion, never a session clause. | [B815](delivered.md#x173--audit-lane-l-corpus-structure-and-skills) | answered by [D183](#d183) |
| Q90 | Do milestone Requires state only real dependencies per submilestone, and may portfolio submilestones complete without family arms? | (a) Per-submilestone Requires cells on m1 to m4 with true prerequisites, cross-milestone items included; completion rules unchanged.; (b) a plus a portfolio or family tag so a milestone can be done on its portfolio submilestones; supersedes D158's all-items clause.; (c) a plus moving M1f, M3f (and optionally M4e, M4f) to a later milestone; amends D158, D172, D173. | **Answered by D183: a.** Recommended: a, correcting the waiver citation to D174; b or c only as an owner decision superseding D158. | [B815](delivered.md#x173--audit-lane-l-corpus-structure-and-skills) | answered by [D183](#d183) |
| Q91 | How do session-taken clauses pending the owner's review bind, and where are they tracked? | (a) One scope rule: a waiver quotes the owner's explicit request, pending clauses bind until overturned and an overturned clause reopens its work; one open-clause list.; (b) Pending clauses authorize nothing until the owner confirms them. | **Answered by D183: a.** Recommended: a; milestones.md is at its byte budget, so the rule needs equal trimming. | [B815](delivered.md#x173--audit-lane-l-corpus-structure-and-skills) | answered by [D183](#d183) |
| Q92 | Does a fresh destroy accept a scoped, acknowledged abandonment of one block that stays unknown, so one unreachable target no longer blocks the whole removal (reopening D38)? | (a) Keep D38: the removal stays refused and the orphan-acknowledged deletion is the only exit.; (b) Record that block abandoned, keep it, its plan ancestors and host reservations, and remove the rest in inverted order; needs B568 so its markers stay guarded. | **Answered by D183: a.** Recommended: No recommendation in the report: it is a request to reopen D38, and roadmap 14.6 holds the work until the owner does. | [B816](delivered.md#x182--audit-lane-n0-remaining-parts-to-their-owners-and-bmc-wording) | answered by [D183](#d183) |
| Q93 | Which exit does a completed apply holding a lost block record get? | (a) The existing purge-with-orphans deletion, named in the refusal and status; abandons the apply's host effects (spec today).; (b) A destroy over the completed apply covers its whole frozen plan, resolving non-done blocks by observation first. | **Answered by D183: b.** Recommended: a; b only as an owner item. | [B816](delivered.md#x182--audit-lane-n0-remaining-parts-to-their-owners-and-bmc-wording) | answered by [D183](#d183) |
| Q94 | Does handoff readiness cover the selected input revision or the revision the apply froze? | (a) Add a predicate condition and status reason: the selected revision is not the one the apply froze, and diff shows what changed.; (b) Readiness covers the frozen scope; project.md says so and status prints the frozen revision beside ready. | **Answered by D183: a.** Recommended: a (calibrator), mirroring the settlement scope. | [B816](delivered.md#x182--audit-lane-n0-remaining-parts-to-their-owners-and-bmc-wording) | answered by [D183](#d183) |
| Q95 | Does retention pin the runtime and wheels of every non-terminal operation so its continuation survives more than one lock change or a setup-format discard offline? | (a) Wording only: current-plus-previous retention stays and refusals name the registering build's setup with network or --wheel-mirror (spec today).; (b) A host-wide pin list under setup/lock written at registration, honoured by retention and format healing, dropped at finalization or deletion. | **Answered by D183: a.** Recommended: a now; b parked as an owner option under D152 clauses 8 and 9. | [B816](delivered.md#x182--audit-lane-n0-remaining-parts-to-their-owners-and-bmc-wording) | answered by [D183](#d183) |
| Q96 | Are SSH-placed host claims published in the controller record, qualified by the SSH host token, so two contexts of one store targeting one SSH host conflict? | (a) Widen B247: every SSH-placed key gains an ssh/<address>:<port>/ qualifier with the controller conflict and release rules, best effort per declared address; controller-record schema moves pre-1.x.; (b) Keep B247 narrow and carry cross-context SSH coordination in a new parked item.; (c) Keep the limitation documented; contexts on one SSH host stay uncoordinated. | **Answered by D183: a.** Recommended: a, decided with B247; do not edit delivered history. | [B816](delivered.md#x182--audit-lane-n0-remaining-parts-to-their-owners-and-bmc-wording) | answered by [D183](#d183) |
| Q97 | How does machine trust under --yes or structured output reconcile with the explicit host-key confirmation rule? | (a) Add --expect <machine>=SHA256:<fp> and make --replace take one; an add or replace whose fingerprint is not named refuses (new public flag syntax).; (b) security.md names --yes enrollment as an operator-accepted trust-on-first-use exception; no code change. | **Answered by D183: a.** Recommended: The roadmap P1-J exit chooses a; the calibrator preferred b with a as hardening. a adds a public flag, so the owner decides. | [B817](delivered.md#x166--audit-lane-d-credentials-and-trust) | answered by [D183](#d183) |
| Q98 | Is a usernamePassword Secret's username secret? | (a) Declare it non-secret identity metadata in security.md and secrets.md; cli.md stops treating --username as hidden.; (b) Add a file or stdin input for the username (new public flag). | **Answered by D183: a.** Recommended: a (calibrator); D72/D73 keep usernames on the flag without deciding they are non-secret. | [B817](delivered.md#x166--audit-lane-d-credentials-and-trust) | answered by [D183](#d183) |
| Q99 | May a query-signed media URL travel on argv for media add --from-url? | (a) Take the URL from a file or stdin (for example --from-url-file) and state that a signed query is a credential (new public flag).; (b) Record a named exception in security.md for a query-signed URL on argv. | **Answered by D183: a.** Recommended: a or b (calibrator); refusing queries is rejected because managed OS intends signed URLs. | [B817](delivered.md#x166--audit-lane-d-credentials-and-trust) | answered by [D183](#d183) |
| Q100 | Is the cluster info --secrets reveal of StorageObjectUser S3 keys (objectUsers) a current, authorized reveal? | (a) Current: secrets.md lists it, and ceph-resources.md and ceph.md link the reveal instead of saying no command reveals the pair.; (b) Not authorized: cli.md marks it Not yet met under B489 and the code stops revealing it. | **Answered by D183: a.** Recommended: a (calibrator): delivered under B489's in-tree handoff, but D143 clause 7 authorizes only the dashboard password, so the owner confirms. | [B817](delivered.md#x166--audit-lane-d-credentials-and-trust) | answered by [D183](#d183) |
| Q101 | What does cluster info --secrets emit for a dashboard password whose apply never proved the dashboard reachable? | (a) No warning: dashboard.status unproved and its reason convey it.; (b) A distinct registered warning code such as access.unreachable. | **Answered by D183: a.** Recommended: a, the smaller change; either way access.unproved keeps only the D124 custody meaning. | [B817](delivered.md#x166--audit-lane-d-credentials-and-trust) | answered by [D183](#d183) |
| Q102 | Does the decision log stay in backlog.md (with per-decision anchors and status tokens) or move to its own page? | (a) Keep the log in backlog.md#decisions and add #d<n> anchors, status tokens and an order check.; (b) Move the log to specs/milestones/decisions.md, retarget every backlog.md#decisions link and amend milestones.md's D<n> bullet. | **Answered by D183: a.** Recommended: a: the smallest change; anchors and status tokens need no move. | [B819](delivered.md#x173--audit-lane-l-corpus-structure-and-skills) | answered by [D183](#d183) |
| Q103 | May a pre-existing defect that blocks the authorized outcome or its gates be fixed inside the slice? | (a) AGENTS.md admits the fix in its own commit after the defect is recorded as an item the Refs line names; other defects are reported.; (b) Keep the unqualified ban; a blocking defect stops the slice until the owner authorizes its item. | **Answered by D183: a.** Recommended: a, as the report drafts it; its only precedent (D152 clause 13) was accepted with D152 by [D154](#d154) ([D188](#d188)). | [B822](delivered.md#x173--audit-lane-l-corpus-structure-and-skills) | answered by [D183](#d183) |
| Q104 | Does the State reconciliation context keep its display name, given its contract disclaims reconciliation? | (a) Rename the display name (architecture row, H1, index link text), keeping the file path and retargeting inbound anchors.; (b) Keep the name; the disclaimer now applied in state-reconciliation.md is the whole change. | **Answered by D183: b.** Recommended: b is smallest; a if the owner wants the name fixed before 1.0. | [B823](delivered.md#x172--audit-lane-m-cli-and-examples) | answered by [D183](#d183) |
| Q105 | Which single casing do Bootwright-owned enum values use, and are Secret type names an exception? | (a) lowerCamel: renames the kebab outliers (container-runtime, import-certificate, install-only, read-only, verified-tunnel).; (b) kebab-case: renames redfishVirtualMedia, loadBalancer, logicalOverwrite, dataFoundation, certManager and the Secret types.; (c) State the rule per page and leave existing values unchanged. | **Answered by D183: a.** Recommended: The convention with the smaller rename count, likely lowerCamel; native spellings exempt; decide Secret types as an explicit exception. | [B824](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q106 | Is the --stage token infra-components renamed to match 'infrastructure services'? | (a) Rename it (for example infrastructure-services) in the CLI, plan goldens, completion, examples and specs.; (b) Keep the token; the glossary entry defines it. | **Answered by D183: a.** Recommended: a, cheap before 1.0, together with stating what preflight infra checks. | [B824](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q107 | Is StorageExport (a per-consumer Data Foundation attachment) renamed, and does its shape change? | (a) Rename to StorageAttachment or StorageConsumer with per-protocol arms, drop spec.type, move external details to StorageCluster's external arm (B107).; (b) Rename the kind and fdf.storageExportRef only; keep spec.type unless api.md's union list is amended.; (c) Keep the name with the clarification now in api/ceph.md. | **Answered by D183: b.** Recommended: b at least, keeping the D173 decomposition; ask with B814's storage naming questions. | [B824](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q108 | Should status add Secret and media readiness hints to nextSteps? | (a) Add read-only rows for missing or stale Secrets and missing media, with remedies before plan or apply, as a separate item.; (b) Keep status as now narrowed; operators use secret check and media list. | **Answered by D183: a.** Recommended: b now; a only as its own UX item. | [B825](delivered.md#x177--audit-lane-t-inherited-flags-and-cluster-preflight) | answered by [D183](#d183) |
| Q109 | Accept an opt-in bootwright diff --exit-code (exit 3 on any differing or unknown object, default unchanged)? | (a) Add the flag, its commands.md row, an exit-status row and an amended ok/exitCode rule.; (b) No flag; document a stable JSON summary field as the drift signal. | **Answered by D183: a.** Recommended: b costs nothing; a is the common convention and needs acceptance as a new public flag under D153 clause 2. | [B825](delivered.md#x177--audit-lane-t-inherited-flags-and-cluster-preflight) | answered by [D183](#d183) |
| Q110 | Which schema-discovery commands or flags does the CLI gain? | (a) render effective --origins marks each leaf authored, defaults.<Kind>, built-in or derived, with a golden.; (b) a plus bootwright schema --format json-schema as an advisory export.; (c) a and b plus bootwright explain with per-field docs in Go, a second owner of field meaning.; (d) Defer all. | **Answered by D183: a and b.** Recommended: a first; b optional; c only if the owner wants Go as the single source of field docs. | [B826](delivered.md#x179--audit-lane-q1-per-object-claims-and-nested-references) | answered by [D183](#d183) |
| Q111 | Is a plain enabled boolean allowed for a featureless switch, or do presence blocks replace them? | (a) api.md allows a plain boolean when the feature has no settings and omission equals the stated default (fips, firewall); no schema change.; (b) Presence blocks only: fips: {} and firewall: {} replace the booleans in schema, examples and goldens. | **Answered by D183: a.** Recommended: a. | [B826](delivered.md#x179--audit-lane-q1-per-object-claims-and-nested-references) | answered by [D183](#d183) |
| Q112 | Which one-value mechanics of the libvirt arm and managed services stop being authored, and is managed-service implementation optional? | (a) libvirt uri becomes optional with a materialized default and bmcEmulationDefaults is renamed; implementation stays required.; (b) As a, and implementation becomes optional with a materialized default while a kind has one implementation. | **Answered by D183: a.** Recommended: a: explicit implementation selection stays, unless the owner prefers b. | [B827](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q113 | Do managed services keep image.local and image.public with the stated precedence, or collapse to one image reference? | (a) Keep both; the precedence now stated is the contract, no schema change.; (b) One image reference; schema, admission, kind defaults, examples and goldens change, mirroring left to registry policy (B69). | **Answered by D183: a.** Recommended: a now; b only together with the registry-policy design. | [B827](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q114 | Does the API adopt one public name limit for all kinds, or keep per-kind limits derived from block prefixes? | (a) One 47-byte limit stated once, new block prefixes at most 16 bytes with a test; tightens Machine, InfraProvider, Proxy, DNS, NTP and export names.; (b) Hash long names into block identities; one rule without shortening names, less readable run trees.; (c) Keep per-kind limits in one api.md table linked from each kind. | **Answered by D183: b.** Recommended: a with an architecture test on prefix length; ask with B814's V2-19 name-limit question. | [B827](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q115 | Are the reserved CustomPlaybook shape and ClusterAddon.spec.steps refused at admission instead of validated? | (a) Refuse an authored CustomPlaybook naming B102; lexical rules stay in the deferred page; normalization code removed.; (b) a plus refuse ClusterAddon.spec.steps naming B90, letting B90 redefine steps.; (c) Keep validating both reserved shapes. | **Answered by D183: c.** Recommended: a; b only if the owner agrees B90 may redefine steps. | [B828](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q116 | Do object-user S3 keys leave cluster info --secrets for a per-user selector? | (a) Add --object-user <u> emitting only that user's keys; --secrets alone reveals only the dashboard password; B489 amended.; (b) A new raw single-credential export command for object users.; (c) Keep the current reveal and document the handoff risk. | **Answered by D183: a.** Recommended: a, a new public flag closing B489's sensitive-output gate; ask with B817's R2-04-02. | [B829](delivered.md#x181--audit-lane-r-private-installer-material-and-recapture-identity) | answered by [D183](#d183) |
| Q117 | Is --authorize data-loss bound to the reviewed consuming steps when --yes or non-interactive input suppresses review? | (a) Require --authorize data-loss=<digest of consuming blocks and targets> under --yes; a stale digest refuses before registration.; (b) Keep the bare token; the cli.md wording fix already applied is the whole change. | **Answered by D183: a.** Recommended: b unless the owner wants hardening; a changes a public flag form under D153 clause 2. | [B830](delivered.md#x170--audit-lane-j-reconciliation-rules) | answered by [D183](#d183) |
| Q118 | Does the last GitOps package wait for every block that reaches its cluster's API, or only the add-ons-stage blocks requiring the installation? | (a) Widen to every block reaching the cluster, whatever its stage; planning edges change and need a cycle check.; (b) Keep the narrowed set now stated, naming the excluded cross-stage blocks. | **Answered by D183: new item B837.** Recommended: b now; settle with the pending Q48 and Q60 answers. | [B830](delivered.md#x170--audit-lane-j-reconciliation-rules), [B837](delivered.md#x171--audit-lane-i-api-shape-and-grammar) | answered by [D183](#d183) |
| Q119 | Is M2c paused, as D168's last paragraph says, or does it continue, as m2.md's M2c state and the X160 record say (F17-03)? | (a) Paused: B484 becomes blocked until the owner resumes M2c, X61 leaves the Active slices table, and m2.md and the X160 record are corrected.; (b) Continues: a decision resumes X61 and amends D168's last paragraph. | (b) if the 2026-10-28 estate request that delivered M2c's X197 was meant to resume M2c, otherwise (a); either way the two texts must agree. | [B484](m2.md#b484) | open |
| Q120 | Does a Machine select its operating-system lifecycle through an explicit presence union instead of `os.provided` plus an optional `installProfileRef` (F04-05)? | (a) A presence union of provided, Bootwright-installed and cluster-installer modes, with retired-field remedies and every example migrated.; (b) Keep the current fields and state the omitted-profile reading as the design. | (a): the boolean breaks api.md's own union rule, and pre-1.x the cut-over needs no converter. | — | open |
| Q121 | Is one derived proxy bypass set applied to every proxy consumer (F04-08)? | (a) Derive one bypass set from Environment domains, consumed machine networks and selected services for every consumer.; (b) Record that Machine and install-profile bypass is authored only, beside the derived OpenShift bypass. | (b): the derived OpenShift bypass is a native requirement; Machines keep the authored list. | — | open |
| Q122 | Does a controller tool source identity retire once no tool area names it (F13-08)? | (a) Retire it in the root transaction that retires the last tool area naming it.; (b) Keep sources append-only, with the refusal past 4096 sources and its fresh-store exit. | (b): the bound is far above any realistic host. | — | open |
| Q123 | Does api.md state a closed set of omission classes with per-field metadata (F03-06)? | (a) A closed set (absent is default, absent inherits, empty clears, empty is invalid, presence enables) carried as field metadata with an exhaustiveness check.; (b) Decline: each field's table owns its omission semantics. | (b), unless the B857 authoring tasks show operators mispredict omitted fields. | [B857](#b857) | open |
| Q124 | Do `version`, schema output and frozen context input carry an authoring-schema fingerprint (F03-09)? | (a) A digest over a canonical structural manifest, reported by `version` and stored with frozen input, named by admission refusals of frozen input.; (b) Decline under D142: the retired-field registry and build identity suffice pre-1.x. | (b) | — | open |
| Q125 | Is a presented plan node called a block in human and structured output (F02-03)? | (a) Rename `steps` and the human headings to block, keeping step for in-block progress and authored add-on steps.; (b) Keep `steps` and define a step as a block as output presents it. | (b): the glossary entries land under either answer. | — | open |
| Q126 | Does the examples index carry a status projection (R2-05-08)? | (a) One generated projection (admission golden, plan-tested, accepted) from existing evidence, with a consistency check.; (b) Decline: rows state outcomes and link the acceptance ledger. | (b): a status column repeats state the milestone pages and the ledger own. | — | open |
| Q127 | What does an omitted CephCluster `distribution` select, now that its default always refuses at plan? | (a) Make `distribution` required.; (b) Default to an implementation that plans.; (c) Keep the refusing default and its refusal row. | (a): V2-02 removed the same trap for the SSH transport. | — | open |
| Q128 | Is the stretch NVMe-oF refusal at ceph.md's stretch services stated on a vendor requirement? | (a) Restate it on a Bootwright-owned basis, such as gateway placement per data site, and widen the vendor-support lint to vendor-attributed requirements.; (b) Keep the vendor-cited reason pending D203's review. | (a), reviewed with D203. | [D203](#d203) | open |
| Q129 | Does a stopped foreign guest that references a retiring libvirt network or bridge refuse its removal? | (a) Refuse when any foreign domain, shut off included, references it.; (b) Refuse only for running guests, accepting that a stopped one loses its start path. An unreadable inventory refuses under either answer. | (a) | [B567](#b567) | open |
| Q130 | How does a context prove ownership across a recreation under the same name? | (a) Mint a random incarnation, persist it atomically, bind every ownership proof to it, and refuse old stores and marks with a named retirement path.; (b) Keep name-derived ownership with the D155 warning. | (a), with the cut-over and whole-store restore defined before code. | [B568](#b568) | open |
| Q131 | Does an interrupted Ceph enrollment continue forward? | (a) Continue only from a matching frozen request, durable per-host receipts and live FSID, host and daemon proofs; unrecorded native state stays unknown.; (b) Keep refusing; the abandoning purge stays the only exit. | (a), with its journal states and commit points defined first. | [B659](m2.md#b659) | open |
| Q132 | How does a host with partial OSD DB/WAL metadata recover? | (a) Classify first, then a journaled clear of narrowly enumerated owned shapes whose device identity is proved.; (b) A documented operator repair procedure only; Bootwright keeps refusing. | (a), narrowly; foreign or unknown metadata keeps refusing. | [B662](m2.md#b662) | open |
| Q133 | Is owned storage removed while its hosts survive? | (a) Per family, with client quiescence or fencing, exact native inverses and data-loss authorization, one family per slice.; (b) Keep refusing removal while hosts survive. | (a), incrementally, after primary-source research. | [B491](m2.md#b491) | open |
| Q134 | What does a supported store restore guarantee about rolled-back seal counters? | (a) An explicit complete-store restore that validates a coherent snapshot and activates a fresh sealing key before any write.; (b) (a) plus an external monotonic anchor that detects out-of-band rollback.; (c) Keep no supported restore. | (a) | [B103](#b103) | open |

## Retired

Historical IDs no longer issued in their original meanings. D131 reuses
M2a/M2b only for the [new M2 submilestones](m2.md#submilestones); their dated
aliases below retain the earlier meanings. Where an item took one over, its line here names that
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
| R1 | 2026-09-28 | Merged into [B19](delivered.md#x93--the-owner-run-of-2026-10-14) with C28, whose outcome it was. |
| M2a, M2b, M3, M4, M5, M6 | 2026-09-28 | These rows became items whose Alias cells name them; the numbers M3 to M6 now name milestones. |
| M1h, M5a, M4a, M1i | 2026-09-28 | Became [B72](delivered.md#x38--the-real-host-run-of-2026-10-05), [B73](m1.md#b73), [B61](m3.md#b61) and [B71](delivered.md#b71); these M-letter IDs are no longer issued. |
| N1 to N5, L2, L4, L5, L6 | 2026-09-28 | Became items whose Alias cells name them; L1 and L3 never existed. |
| K01 to K73 | 2026-09-28 | The audit plan's cluster tokens, defined nowhere in the repository; the grouping stays in Git history. |
| Phase 0 to Phase 4 | 2026-09-28 | The audit plan's ordering; slices carry order now. |
| M1h's (D1) to (D6) | 2026-09-28 | Capability labels that collided with the decision log; they became prose. |
| D13, D15 | never issued | Recorded so nobody looks for them. |
| B497 | 2026-10-12 | Answered by [D143](#d143): M2b's acceptance is an ordinary staged apply recorded as an owner-accepted ledger row ([B476](delivered.md#x94--post-destroy-cluster-inspection)), and there is no runtime registry to promote. |
| B498 | 2026-10-12 | Answered by [D143](#d143): converge. [X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply) retires the second executable and its engine (B513, B512, B518, B515). |
| B501 | 2026-10-12 | Folded into B513 ([X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply)) under [D143](#d143), which removes the three allow-list edges. |
| B502 | 2026-10-12 | Moot under [D143](#d143): no fixture state exists, and a leftover anchor refuses at index read with a named remedy (B513, [X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply)). |
| B505 | 2026-10-12 | Moot under [D143](#d143): B512 ([X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply)) keeps a failed resolution's adapter output in its attempt file. |
| B507 | 2026-10-12 | Moot under [D143](#d143): the example's `acceptance.py` and `test_acceptance.py` are deleted by B513 ([X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply)). |
| B227 | 2026-10-14 | Moot under [D152](#d152): X89 deleted the execution bundle and its reprojection; every command stages the automation it runs. |
| B231 | 2026-10-14 | Moot under [D152](#d152): X89 deleted `setup --purge-old-bundles` and the kept bundle's resolutions. |
| B238 | 2026-10-14 | Moot under [D152](#d152): X89 deleted the setup bound over resolutions and the purge that reported them. |
| B263 | 2026-10-14 | Moot under [D152](#d152): X89 deleted the retained bundle and its fresh-resolution fallback. |
| B335 | 2026-10-14 | Moot under [D152](#d152): X89 deleted the setup resolution it would have exercised; the host's dnf path on RHEL awaits the owner's run in X90's acceptance ([B560](#b560)). |
| B429 | 2026-10-14 | Moot under [D152](#d152): X89 deleted the foundation guards, the native resolver and its Go-run helpers. |
| B527 | 2026-10-14 | Moot under [D152](#d152): X89 deleted the approved bundle; each command stages its own automation, so no setup projects it. |
| B528 | 2026-10-14 | Moot under [D152](#d152): X89 deleted the execution bundles, retained resolutions and `setup --purge-old-bundles` it concerned. |
| B542 | 2026-10-14 | Moot under [D152](#d152): X89 deleted the Fedora native plan and its libdnf5 solver; the host's dnf installs controller packages. |
| B574 | 2026-10-16 | [D155](#d155) retains records-only settlement; comparing the current build's plan is declined for now. No implementation remains. |
| B500 | 2026-10-16 | Superseded by [D157](#d157) and [B615](delivered.md#b615): remove reviewed release pins instead of consolidating them into per-language release profiles. |
| B62 | 2026-10-20 | Alias L2. Folded under [D172](#d172) into [B713](delivered.md#b713) and each M3 item's own planning goldens; it named no bounded shape. |
| B63 | 2026-10-20 | Alias L4. Folded under [D172](#d172) into [B713](delivered.md#b713); each later shape carries its own lifecycle journeys. |
| B65 | 2026-10-20 | Alias L6. Retired under [D172](#d172): every M3 item carries the contract, crash, replay, cancellation and real-system evidence of its own binding. |
| B397 | 2026-10-20 | Folded under [D172](#d172) into [B308](delivered.md#b308), whose refusal-table rows include the off-controller boot-image server. |
| B89 | 2026-10-20 | Alias C10. Moved from M6 and folded under [D173](#d173) into [B752](delivered.md#b752) (built-in packages need no acquisition; authored packages are input-tree data frozen with the context) and [B754](delivered.md#b754) (the registration commands are removed); its `add-ons/_store` exception is parked as [B771](#b771). |
| B365 | 2026-10-20 | Folded under [D173](#d173) into [B771](#b771), which removes the `add-ons/_store` exception and its marker, so the hard-linked-marker disagreement leaves with them. |

## Decisions

Decisions, dated. An item waiting on one not yet taken names it in its
Requires cell as `owner decision: <question>`.

Each decision is a `### D<n>` entry in numeric order, so `#d<n>` anchors it, and
opens with its status token: `owner`, `delegated` (decided by a session under
the owner's explicit instruction to proceed; binding and not in the review
queue), `session-pending` (a clause a session took, listed below),
`accepted-by D<n>` or `superseded-by D<n>`.

### Open session clauses

A clause a session took pending the owner's review binds until the owner
overturns it, and an overturned clause reopens the work it shaped as a new item
([scope rules](../milestones.md#scope-rules)). This list names each decision
holding such a clause, the clauses pending and the work they enable.

- [D7](#d7): the 2026-09-29 reading that non-root SSH accounts refuse on
  lifecycle placement hosts (X22).
- [D134](#d134): the 2026-10-11 readings that D134 waives M2's Requires for
  X47, X52, X53 and X54 and excepts M2b from the WIP limit; moot since D165
  returned those slices and D183 counts the WIP limit per program.
- [D141](#d141): the neutral wording restored into D60, D104, D109 and D113
  and the X45 record.
- [D143](#d143): clauses (1) to (11), Ceph through the ordinary staged
  apply (X70); D157 supersedes the release admission of (1) and (2).
- [D145](#d145): clauses (1) to (3), (5) and (6), continuation under a
  rebuilt binary and its contract guard (X74); D183 (Q76 (a)) confirms (4).
- [D150](#d150), [D151](#d151): the clauses after the owner's words,
  the several-Machine and several-cluster `exec` and power rules (X85, X86).
- [D159](#d159): clauses (1) to (16), the M2c program, X55 and X60 (X56 to
  X61).
- [D161](#d161): the session finding (1) and the clauses after the owner's
  answers, the Ceph auxiliary-image and SSO lanes (X57).
- [D172](#d172): clauses (1) to (10), the M3 submilestones, M3f read as
  vSphere (Q1) and M2's waiver for M3b to M3d (Q2).
- [D173](#d173): clauses (1) to (13), the M4 submilestones and M4b to M4f's
  waiver (D174 applied it for M4b and M4c, D180 for M4d's and M4e's
  blockers); Q26 to Q61 stay open, D174 having accepted their recommended
  baseline; (13), the per-cluster early handoff, waits for Q60.
- [D192](#d192): clauses (1) to (7), registration with a declared Satellite
  or Capsule and its release pin (X194).
- [D193](#d193): clauses (1) to (5), the built-in `kubevirt` package (X195)
  and the estate review's records.
- [D194](#d194): clauses (1) to (7), addressed localnet bridges and the
  uplink rules (X196).
- [D195](#d195): clauses (1) to (6), the Ceph cluster deadline as the only
  host bound (X197).
- [D196](#d196): clauses (1) to (12), the stretch reference profile, its
  topology inputs and the provided-host FIPS proof (X198, X199).
- [D197](#d197): clauses (1) to (13), the KubeVirt Machine arm over an
  externally supplied host (X129).
- [D198](#d198): clauses (1) to (7), the physical Ceph host's disk bound
  derived from its Machine's erase policy (X201).
- [D199](#d199): clauses (1) to (9), node placement once the installation
  completes (X126).
- [D200](#d200): clauses (1) to (12), how stretch activation is realized,
  read back and refused (X199).
- [D201](#d201): clauses (1) to (11), OpenShift on KubeVirt Machines over an
  external host (X130).
- [D202](#d202): clauses (1) to (11), KubeVirt Machines on a host cluster the
  context builds and their spread (X131).
- [D203](#d203): clauses (1) to (14), stretch service placement and front
  doors (X200) and the removal of a provided tiebreaker (X203).
- [D204](#d204): clauses (1) to (7), how a provided stretch tiebreaker is
  dialed with the fleet key its Machine never names (X204).
- [D205](#d205): clauses (1) to (9), how a provided tiebreaker's removal is
  realized (X203).
- [D206](#d206): clauses (1) to (6), the consumer and replication bindings
  of a stretch cluster with a provided tiebreaker (X205).
- [D207](#d207): clauses (1) to (10), power and the live reading of KubeVirt
  Machines on a host the context builds and the reading's pin (X206).

### D1

**owner** (2026-09-26): the substrate owns the machine port as role entry points.

### D2

**owner** (2026-09-26): lifting the physical refusal needs in-tree tests and an
emulated rehearsal, with real-hardware acceptance before support is claimed.

### D3

**owner** (2026-09-26): the BMC certificate is imported where the BMC supports
it, otherwise an explicit per-machine disable-verification exception applies,
never a default, and controller-to-BMC TLS gains a CA bundle.

### D4

**owner** (2026-09-26): administrator access is kept in context custody with
`cluster kubeconfig`.

### D5

**owner** (2026-09-26): every node presenting this block's own tokenized image
proves a partial install.

### D6

**owner** (2026-09-26): the first proved UUID and serial are pinned and a
mismatch refuses.

### D7

**session-pending** (2026-09-26): non-root SSH accounts refuse at admission for now.
Read on 2026-09-29 by the session, pending the owner's review, as applying
to lifecycle placement hosts: the machines managed services and libvirt
providers name (X22).

### D8

**owner** (2026-09-26): the cluster identity is the build's kubeconfig trust
anchor, refined on 2026-09-27 (S26) to its admin client certificate, which
the installer's install-complete rewrite keeps.

### D9

**owner** (2026-09-26): multi-node libvirt stays admitted under F14.

### D10

**owner** (2026-09-26): ansible-core is the latest patch of the qualified 2.21
minor.

### D11

**owner** (2026-09-26): postcondition decisions stay in Go.

### D12

**owner** (2026-09-26): media completion claims the inputs digest and installer
version, not an image digest.

### D14

**owner** (2026-09-26): documentation stays in the bundle but leaves the
automation digest.

### D16

**owner** (2026-09-26): managed-OS waits are frozen as request budgets.

### D17

**owner** (2026-09-28): M3 and M4 are done only with two owner-accepted
acceptance-ledger rows each: an emulated rehearsal on libvirt guests behind
sushy-tools Redfish BMCs, and a run on real hardware. Each item keeps the
gate its own detail names.

### D18

**owner** (2026-09-28): the product scope names MetalLB, IBM Fusion Data
Foundation, Red Hat Advanced Cluster Management, Argo CD and GitLab as the
first add-on packages.

### D19

**owner** (2026-09-28): M5 integrates OpenShift with a Ceph cluster M4 manages;
an external Ceph cluster is parked as [B107](#b107).

### D20

**superseded-by D152** (2026-09-29, taken by the session under the owner's instruction to
proceed with recommendations; pending review): the controller CPython is
the latest patch of the newest minor the qualified ansible-core supports as
a controller, and `latest` ignores a newer minor (X22).

### D21

**delegated** (2026-09-29, taken by the session under the owner's instruction to
proceed with recommendations; pending review): an elevated child announces
that it started, and the held `sudo:` lines decide the outcome only when that
announcement cannot be proved (X20).

### D22

**delegated** (2026-09-29, taken likewise; pending review): an elevated command
that exits without a result reports `runtime.internal` with exit status 1;
`runtime.interrupted` still wins after an interrupt (X20).

### D23

**delegated** (2026-09-30, taken by the session under the owner's instruction to proceed with recommendations): B9: a controller's CA bundle is the only anchor of its Redfish calls; without one, the system trust store.

### D24

**delegated** (2026-09-30, taken by the session under the owner's instruction to proceed with recommendations): B9: a physical Machine's virtual media imports the media certificate by default; `disable-verification` is admitted only on one Machine and refused beside private delivery.

### D25

**delegated** (2026-09-30, taken by the session under the owner's instruction to proceed with recommendations): B10: the local keyring becomes `local-keyring-v4`, which holds produced material; an earlier keyring refuses and names the build that reads it.

### D26

**delegated** (2026-09-30, taken by the session under the owner's instruction to proceed with recommendations): B10: the custodied kubeconfig is withdrawn when the context's removal of its cluster completes.

### D27

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B29: a foreign image on one of the cluster's own Machines reads as partial during an agent-install removal.

### D28

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B34: an SSH-host provider's hypervisor packages resolve at apply time; the frozen transaction is parked (B167).

### D29

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B40: admission refuses `spec.lifecycle.rescue` until a rescue journey exists.

### D30

**owner** (2026-09-30, accepted by the owner from the session's recommendations): Follow-ups a slice finds enter M1 only when they are safety or defect items; enabling and polish follow-ups are parked unless they block an M1 item.

### D31

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B137: `status` offers as next steps only the verbs the records allow.

### D32

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B140: the cluster boot budget is 300 seconds per node with a 900-second floor; polled reads still count as answered at once.

### D33

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B141: the agent-install decoders refuse, for every verb, a frozen placement off the controller; the runner refuses a material listed twice.

### D34

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B143: a proved UUID or serial refuses anything that is not printable, by the CLI's printable rule.

### D35

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B146: a YAML key is refused for the construct it carries (`yaml.alias`, `yaml.tag`), as a value is.

### D36

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B41: the Secret `file` source is retired; operators load files with `secret set`.

### D37

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B42: a lost frozen binding refuses before any effect and names the exits: restore the keyring, or an orphan-acknowledged delete.

### D38

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B43: an unresolvable unknown block keeps its refusal, which names why and how to fix it; the orphan-acknowledged delete also releases host reservations.

### D39

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B136: a failed apply whose blocks are all done is recovered; the other two refused states name their exit.

### D40

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B44: only the remaining bundle-area limit is fixed; the layer is parked (B165), which [X88](delivered.md#x88--controller-setup-v2-cut-over) closed under D152's clause 7.

### D41

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B45: a continuation freezes its Python and Ansible closure and refuses when it moves; scoped removal is parked (B166).

### D42

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B46: local setup keeps its Ansible output in its own bounded run area, the newest eight runs.

### D43

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B122: lifecycle JSON results are parked until a milestone names a consumer.

### D44

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B111: validation refuses wwn, hctl and serial root-device hints on a libvirt Machine.

### D45

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B116: the emulated BMC admits canonical IPv6 unicast bind addresses, with a bracketed endpoint and the shared socket key.

### D46

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B131: media publication proves nothing wrote the stage after it was measured, and the spec says so; the pre-X20 long-name leftover is removed by hand.

### D47

**owner** (2026-09-30, accepted by the owner from the session's recommendations): B158: a newer Index API minor version makes setup warn and continue; a newer major version refuses.

### D48

**owner** (2026-09-30, the owner): M1 is frozen after X33. Follow-ups X32, X33 and later slices find are parked here for a future milestone, whatever their kind, instead of entering M1; D30 no longer admits items to M1. M1 keeps its open items: X32, X33, the owner decisions and the chain behind B49.

### D49

**owner** (2026-10-01, the owner accepted the session's recommendation A): B32: after a stall the install role reads the registered hosts from the rendezvous host's assisted-service API with the watcher token, bounded and under `no_log`, and the give-up names the node.

### D50

**owner** (2026-10-01, the owner accepted the session's recommendation A): B175: `setup --purge-old-bundles` retires superseded areas under a pending receipt whose bundle holds no area, then resumes it.

### D51

**owner** (2026-10-01, the owner accepted the session's recommendation A): B188: a drifted running network restarts when its Machines are stopped and otherwise refuses before any effect, naming them.

### D52

**owner** (2026-10-01, the owner accepted the session's recommendation A): B206: the retained-operation refusal names destroy, then context deletion and a fresh init; pruning is parked (B258).

### D53

**owner** (2026-10-01, the owner accepted the session's recommendation A): B208: a retained bundle that lost a bootstrap source falls back to a fresh resolution.

### D54

**owner** (2026-10-01, the owner accepted the session's recommendation A): B220: before a wait, the install role restores a truncated installer kubeconfig from the kept copy when it names the same cluster.

### D55

**owner** (2026-10-03, the owner accepted the session's recommendation A): B175: under `setup --purge-old-bundles`, a receipt stranded at the bound that this executable cannot resume is recorded as canceled, its never-started bundle publication observed as such, and a fresh setup follows; D50's resume stays for one it can prepare.

### D56

**owner** (2026-10-05, the owner): M1 gates the owner's full bar: well-defined specs; architecture and code that follow best practices; well-structured, domain-constrained APIs with input validation; good UX and CLI output; and a CLI ready for every controller function (context, secret, media, setup, preflight, validate, render effective, status, plan, apply, destroy and the machine commands) and for creating RHEL machines on libvirt and sushy-tools. This amends D48: M1 takes the parked B94, B95, B179, B198, B200 to B202, B222 to B225, B228, B233, B234, B236, B240 to B242, B244, B246, B249, B252, B255, B257, B264, B270 to B272 and B275 to B277, and the new B278 to B302 and B336 to B338.

### D57

**owner** (2026-10-05, the owner): B17 to B24: X23 is dropped, and X24 keeps B19 to B22, re-planned with the role and request fixes of its lanes. B17 keeps its ID for the managed-OS work area's relocation, and B303 splits from it with per-invocation scratch, disjoint namespaces and the two-concurrent-installs test; B24 keeps its ID for cross-context scoping and an attributed refusal, and B304 splits from it with the exemption of an invocation's own in-flight jobs. B18, B23, B303 and B304 move to M3, B18 recording that context reads refuse during an apply and taking B265 into its exit evidence, and the parked B195, B237, B239, B250, B251, B259, B260 and B265 join them there.

### D58

**owner** (2026-10-05, the owner): B72: the lab-rhel ledger row of 2026-10-05, which matches B72's acceptance baseline and gate, is accepted for B72 too, so B72 leaves M4 for X38's record; the defects the run found stay M1 items.

### D59

**owner** (2026-10-05, the owner): M1's closing operator gate is the extended lab-rhel run on a clean build of X44's landing, on a host missing at least one native root so that setup or the controller stage downloads through the Python path: setup with the new bundle, media, context, secrets, a staged apply, preflight, plan, apply, `status` with its four JSON checks, `machine list` and `--power-status`, `machine exec` and `rsh`, restart, a destroy refused and then `machine stop`, destroy, a fresh apply, a host restart, `machine start` and `stop`, destroy, `status` after the destroy, the unit and time checks and an explicit `qemu-img info --force-share` read. One owner-accepted row completes the real-host evidence of B19, B20 and B277.

### D60

**owner** (2026-10-05, the owner): the real physical-BMC test follows M1, which closes without physical installation, and B73 to B78 stay in M4, B74 to B77 outside any planned slice. On the owner's explicit request of 2026-10-05, X45, the slice the session's plan called XB, runs out of sequence right after X43 and X24, in the same digest window, and delivers B73's repair and B305, the Redfish client on the owner's physical BMC. A run on a disposable RHEL 9.8 controller, the lab-baremetal rehearsal and one physical server with a reduced first-test profile, run from a separate RHEL 9.8 controller (D109) and recorded as an observation, follow it.

### D61

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B279 and B295: `spec.libvirt.uri` is an enumeration holding `qemu:///system`, which closes the command transports escaping cannot.

### D62

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B280 and B282: an operator whose account comes from a directory service resolves through NSS with a pinned, root-owned `getent` under the same single-entry and clean-home checks, `/etc/passwd` serving only where `getent` is absent, and a `sudo -i` root shell resolves as direct root.

### D63

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B281: store-wide verification stays, but a damaged context's refusal names the context, the store-relative entry and the errno, and `context delete --name <it> --purge` runs over exactly that context; per-context isolation is parked as B325.

### D64

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B283: an installed Machine's SSH session defaults `access.ssh.addressRef` to its `ssh` address, else its install address, else its FQDN, and the mismatch refusal gains a remedy.

### D65

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B283: a new or replaced trust record whose address collides with the record of a Machine the context no longer declares replaces it in the same confirmed write, shown as a remove row.

### D66

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B283 and B287: every remedy and next step that names a command carries `--context`, and every per-object prompt names the context.

### D67

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B275: after a removal, `status` reports a shared service whose destroy is done or released as `pending`, a failed one as `failed` with the token `[FAIL]`, and a running or unknown one as `unknown`.

### D68

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B284: `status` relabels `Bound` as the bindings count and reports 0 once a completed removal finalized, so that status still reads no keyring.

### D69

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B222: over a lost binding, `status` offers `bootwright apply` first, which finalizes without reopening the binding, then the orphan-acknowledged delete.

### D70

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B284 and B300: the `machine list` and `media list` JSON rows rename `address` to `contact` and `ips` to `addresses`, with the columns CONTACT and ADDRESSES, before any consumer exists.

### D71

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B285: a Secret declaration's fingerprint drops its provenance and covers type, source and parameters only, and a stored version whose fingerprint equals the legacy one stays current.

### D72

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B285: `secret set` keeps its strict input-file rule for files carrying a value, password, token or private key, and accepts a certificate or public-key file that is a regular single-link file, not writable by others and owned by the invoker or root.

### D73

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B285: `--value-stdin` and `--password-stdin` at a terminal prompt on standard error with echo off and read one line for tokens and passwords; opaque and `dockerConfigJson` values refuse a terminal with a pipe-or-file remedy.

### D74

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B287: Bootwright's own refusals before a `machine exec` or `rsh` session opens, `trust.identity` included, exit 255, and once the session opened its own status wins over an interrupt.

### D75

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B287: a declined or non-interactive confirmation reports its consumer's existing code, with the confirmer's reason and the `--yes` remedy.

### D76

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B287: the CLI and command specs say that destroy's proofs needing the exclusive lock or a remote observation follow the prompt and still refuse before registration, and the plan names the Machines that must be stopped.

### D77

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B288: controller egress keeps a pure route grammar for endpoint and bypass at admission and selection, and the executable's limits on a managed proxy, authentication, private trust and extra capabilities refuse as controller Unsupported rows with remedies.

### D78

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B289: an installation that selects an external DNSServer or NTPServer uses its declared address with no requirement edge.

### D79

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B289 and B338: network overrides are composed, and secondary addresses, MTUs, non-install bonds and VLANs and extra routes refuse before registration on a Bootwright-installed Machine; post-install NMState convergence is B326 on M4, before B80.

### D80

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B289: DHCP-only Anaconda installations refuse at admission, and both specs say so; DHCP installation is parked as B327.

### D81

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B289: MachineImage and hosted-tree media narrow to `local-media:<name>`, and installed consumers need the `redfishVirtualMedia` endpoint.

### D82

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B289: an installation whose image or tree server is not on the controller refuses before registration, the managed-OS spec is corrected and the unused install tooling deleted.

### D83

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B249: a Machine and a MachineInstallProfile of one name that publish through one server refuse at admission.

### D84

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B225: the runs area keeps the newest runs per context, removing the oldest before a new one, as D42 does for setup runs.

### D85

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B104 stays parked for bounded lifetime allocation, its reason corrected to state that every bounded run consumed a reservation; the narrow fix, in which `machine` power, `rsh` and `exec` read their material in one keyring session and publish no durable binding, splits out as B337 in M1.

### D86

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B290: media URLs are HTTPS only, in validation, the acquirer, help and the specs.

### D87

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B291: an identical `context update` over a completed apply keeps the selected revision, reports it unchanged and asks nothing, and changed input over a completed apply warns before the prompt.

### D88

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B291: an orphan-acknowledged deletion names `bootwright status --context <name>` as its inventory in the refusal and the prompt, and the specs narrow to that; a guard port that lists the owned blocks is parked as B324.

### D89

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B236: `--allow-orphans` may abandon a context whose evidence is unreadable or differently spelled, saying that its objects cannot be listed, and the refusal names that exit.

### D90

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B292: M1 ships the remedies and the corrected spec sentence for a 16-area bound held by client areas; their retirement by `setup --purge-old-bundles` is parked as B322. D152's clause 8, pending the owner's review, supersedes it where it bounds execution areas: [X90](delivered.md#x90--controller-setup-v2-retention-and-offline-sources) retires a tool area no binding names and closed B322.

### D91

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B202: setup prints resolution warnings with the plan, before the prompt.

### D92

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B228: setup runs and bounded runs share one 4 MiB output bound.

### D93

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B271: D55's cancellation extends to a pending setup receipt below the bound that this executable cannot resume, including one whose native action refused before authorizing any transaction.

### D94

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B294: the dead InfraProvider and Machine fields leave v1alpha1: the libvirt provider's `bmcEmulationDefaults.disableCertificateVerification` is removed; the bare-metal attachment's `vlan` is removed, coordinated with B74; bare metal no longer requires the `attachmentRef` nothing reads; `hardware.boot.nicRef` is removed; and `hardware.nics` MACs and `management.bmc` on a libvirt provider's Machines refuse at admission.

### D95

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B294: one managed attachment name or bridge on two libvirt providers of one host refuses at admission, naming both providers.

### D96

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B296: installation media is pinned as the managed-OS spec says: plan freezes the store record's size and digest, a declared checksum must match, and each attempt proves size and digest before first use.

### D97

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B296: the Kickstart renders formats with their glibc langpack, repositories as `%post` `.repo` files with `gpgcheck`, `gpgkey` and `name`, and a credential-free external proxy that exempts the artifact endpoint; `passwordAuthentication: true` refuses while `initialPassword` does, and the architecture enumerates `x86_64`.

### D98

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B17 is split: the managed-OS work area moves under a root-only 0700 parent in M1's digest window, and the rest of B17 is B303 in M3.

### D99

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B19 is not split: progress emission tasks stay in roles, one shared plugin base carries per-capability evidence functions, postcondition decisions stay in Go (D11), and B19's text and the architecture spec's Not-yet-met line are refreshed.

### D100

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B300: `version`'s `Dependency bundle` field is the embedded automation digest, computed at composition.

### D101

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B98 stays parked for pruning the narration; the amendment allowing short rationale and safety-invariant comments in the architecture spec, with a shrink-only per-package ratchet on the comment count, splits out as B336 in M1.

### D102

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B301 adds a Not-yet-met line for removals that release service socket reservations without proving the sockets free; the proof is parked as B323, for the per-capability evidence functions B19 creates.

### D103

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B73: the per-machine installer ISO is published only under the private token subtree and withdrawn after completion, the in-installer `curl --cacert` fetch stays, and private delivery requires a verified controller-to-BMC leg, because InsertMedia carries the token URL.

### D104

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B305 and B319: on the physical BMC under test, under `established` trust, the client reads `VerifyCertificate` and `HttpsTransferCertVerification` before the insert and refuses private delivery when either reads false, the operator importing the CA out of band, within B73's repair; the manager SecurityService root-CA import follows as B319, before the first real-hardware row. D121 (2026-10-07) amends which flag the read-back counts.

### D105

**owner** (2026-10-05, accepted by the owner from the session's recommendations): the parked B96 to B107, B122, B161 to B167, B171, B180, B181, B193, B194, B196, B197, B199, B226, B227, B229 to B232, B235, B238, B243, B245, B247, B248, B256, B258, B261 to B263, B266 to B269, B273 and B274 stay parked.

### D106

**superseded-by D152** (2026-10-05, accepted by the owner from the session's recommendations): B288: a RHEL controller accepts `lorax` and `xorriso` the operator installed from the host's own entitled, vendor-signed repositories, proved by presence like any root; an entitled-source adapter is parked as B330. D152's clause 6, pending the owner's review, supersedes it: [X88](delivered.md#x88--controller-setup-v2-cut-over) installs both through the host's dnf and closed B330.

### D107

**superseded-by D152** (2026-10-05, accepted by the owner from the session's recommendations): B292 and B297: the execution foundation keeps its byte-exact guard at every launch and gains a named check, a refusal naming package, build and file, a documented hold procedure and a regeneration tool in B292; in B297, within X43's window, setup qualifies vendor-signed glibc and libgcc within the qualified minor against the RPM database and records the proved digests as the receipt's requirement. D152's clause 1, pending the owner's review, supersedes it: [X88](delivered.md#x88--controller-setup-v2-cut-over) dropped the byte guard from every launch, and X89 deletes its code.

### D108

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B292: on a FIPS-mode controller host, Bootwright's runtime brings its own cryptography, which the docs state and preflight reports beside the host's FIPS mode; a FIPS-qualified runtime is parked as B331.

### D109

**owner** (2026-10-05, accepted by the owner from the session's recommendations): the real-hardware test runs this build on a separate RHEL 9.8 controller in a subnet the BMC network reaches on the artifact ports, touching nothing on the owner's bastion; B280's host guidance says so. A test profile grants no authority over any other host.

### D110

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B301 and B73: the operator owns the controller host's firewall, and the docs list each listener's ports by source with firewalld commands; a per-socket readiness check in preflight and plan is [B318](delivered.md#x62--controller-listener-firewall-inspection), before B78.

### D111

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B282: operator-named paths are opened by a bounded helper under the invoking credentials, which passes the descriptors to root, and root keeps every proof.

### D112

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B283: the machines spec documents what `auth.operatorIdentity` does today; running its client under the invoking account is parked as B332, after B282.

### D113

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B305: generated serving certificates stay P-256; B305 records the physical BMC under test's RSA premise (it has fetched only from an RSA-2048 self-signed server, so whether it accepts a P-256 serving certificate is unproved) with its check and, as the workaround, a `contextStore` RSA-2048 certificate set with `secret set`; a `keyType` for generation follows only if that check fails. No hardware certificate-compatibility result is recorded here.

### D114

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B299: a DNSServer keeps its wildcard bind default, the new pre-start check names a colliding socket, and the examples and docs declare explicit binds; a derived default is parked as B333.

### D115

**owner** (2026-10-05, accepted by the owner from the session's recommendations): B301: the examples bind their emulated BMCs to loopback, and the docs state that a privileged or cleartext-credential listener binds loopback or a host-only address unless its network is trusted; emulator TLS, a refusal of a non-loopback bind address and a confined SELinux type are parked as B334.

### D116

**owner** (2026-10-06, the owner): every safety or defect follow-up a slice finds in M1's journeys joins M1, folded into the next fitting slice or a final sweep slice before the closing run (D59); enabling and wording follow-ups stay parked. This amends D56 and D48 for follow-ups found from X39 on: a safety or defect follow-up found in M1's journeys joins M1 on the planned sweep slice X46, or on an earlier planned slice whose lane already owns its files, with the Alias `new, <date> (<slice>); attached to M1 on <date> (D116)`; an enabling, wording, test-depth or knowledge follow-up stays parked under D48. M1 takes X39's parked safety and defect follow-ups B339, B340, B342 to B344, B347, B349 to B351 and B356 on X46.

### D117

**owner** (2026-10-06, the owner accepted the session's recommendation): B357: D65 widens, so a confirmed trust write that takes over an endpoint also drops the record of a still-declared Machine that no longer uses the context's SSH trust, the controller Machine's included, as a `remove` row in every lifecycle state, with no input edit.

### D118

**owner** (2026-10-06, the owner accepted the session's recommendation): B372: a file-input `secret set` replacement, `secret delete` and `secret encryption rotate` confirm before the lease and revalidate under it, as standard input does since D73, refusing with `secret.store.conflict` and writing nothing when the store changed meanwhile.

### D119

**owner** (2026-10-07, the owner accepted the session's recommendation): B440 joins M1 on X46: a destroy over an unknown apply resolves each block with the removal's own check, which proves the object is this context's and can be taken back, so a drifted machine is removed with its ownership proved; the state-reconciliation spec records what that resolution proves.

### D120

**owner** (2026-10-07, the owner): the emulated controller serves TLS: the libvirt emulator unit serves HTTPS with a generated serving certificate the InfraProvider's emulated-BMC defaults name, the lab-baremetal physical copy names its authority in `trustBundleRef` and verifies the leg, so D103's verified controller leg holds for the rehearsal; B334 splits: its TLS half joins M4 as B446 on a new planned slice X47, the loopback bind stays with X44 (D115) and confinement stays parked.

### D121

**owner** (2026-10-07, accepted by the owner from the session's recommendation): D104 is amended: where the manager publishes `HttpsTransferCertVerification` that switch alone decides, and the device's `VerifyCertificate` counts only on firmware whose manager publishes no such switch; a hardware capture of the device flag stays an operator item; this is B447 on X47 together with the follow-up that `disable-verification` is a silent no-op on the iBMC.

### D122

**owner** (2026-10-07, accepted by the owner from the session's recommendation): after a failed physical apply the private subtree stays published until the next apply or a destroy clears it; the managed-OS spec says so.

### D123

**owner** (2026-10-08, the owner accepted the session's recommendation): B440: D119's destroy resolution must not lose produced material: a destroy that resolves an unknown apply block first runs that block's apply observation (the capability's read-only Observe) and, when it proves the operation completed, captures the produced material into custody exactly as a completed apply's finalization does (for example the agent-install admin kubeconfig) before any inverse runs; a drifted machine whose observation does not prove completion is still resolved by the removal's ownership proof as D119 says; the state-reconciliation spec states the capture.

### D124

**owner** (2026-10-08, the owner accepted the session's recommendation): B440: an installer's kept administrator kubeconfig is never deleted while custody holds none for that cluster: when a removal (the agent-install media block's inverse, or any inverse that deletes the installer's work area) would delete the kept kubeconfig and custody holds no kubeconfig for that ContainerCluster, it first moves the kept copy into custody as produced material marked unproved access, its completion never proved, under the custody path and bounds a completed apply's capture uses, and only then removes the work area, so a crash between the two leaves the copy in one durable place or both and never in neither; `bootwright cluster kubeconfig` exports it with a warning that its access was not proved; deleting the context removes it as it removes any custody entry; this holds whatever the nodes' power state, so the destroy's `Stop first` path loses nothing.

### D125

**owner** (2026-10-09, the owner): bounded concurrent execution is required for M1 to be implemented. B303 and B304 return from M3 to M1 with their IDs, and B466 adds separate shared package-tree publication and production concurrency, with contract definition required before its implementation. X48 is planned after X46 and before X44; D59's closing gate must prove independent service and machine work, including three same-profile installations, actually overlapping, with replay, cleanup and safe destroy. Prior serial acceptance does not discharge this gate. This request authorizes review and backlog planning; implementation remains subject to the scope rules.

### D126

**owner** (2026-10-09, the owner): finish X44's remaining implementation while deferring the Go toolchain upgrade B470 requires. B470 and the failing vulnerability gate remain unmet; the deferral changes no exit gate or security requirement and declares neither X44 nor M1 complete.

### D127

**owner** (2026-10-09, the owner): rebase M1 on current main and implement X48 there, so the three-node RHEL fixture and future examples use all dependency-ready parallelism within the existing bound. B466 realizes shared profile publication under implementation/request `os-package-tree-v1` and evidence `os-package-tree-evidence-v1`, advances installation requests to `os-install-anaconda-v8`, and records effect, replay, inverse and compatibility boundaries in Managed OS. This also names X48 as the slice retiring libvirt's provider/Machine name collision refusal through separate durable namespaces. B470's existing upgrade deferral and every M1 exit gate remain unchanged.

### D128

**owner** (2026-10-10, the owner): keep M1's scope, sequence and completion gates unchanged. M2 is full managed IBM Ceph 9.9.x provisioning on libvirt+sushy and physical bare metal/BMC, with every provider provisioning feature qualified through B472, B80 and B81; M3 is full OpenShift cluster provisioning on those two provider paths, with complete coverage defined by B473. Move the former M4 Ceph scope to M2, former M5 first add-ons to M4, former M6 OpenShift Virtualization to M5, former M7 ACM/Argo CD/GitLab to M6, and former M2 standalone native rendering to the final M7. Move B70's shared physical erase gate from M3 to M2; managed storage projection and lifecycle qualification no longer wait for standalone rendering or OpenShift. B, X and prior D identifiers remain fixed, and dated history retains its original milestone numbers. Each scope transferred as a whole keeps its items and delivery evidence.

### D129

**owner** (2026-10-11, the owner): define M2 before implementation and deliver
its Ceph journey in order: minimal three-node libvirt+sushy, full virtual
provider coverage, full physical BMC coverage, then two-data-site stretch
with a third-site monitor-only tiebreaker. Split B80/B81's combined outcomes
into independently qualified feature families and acceptance checkpoints;
B472 owns the release/coverage definition and B474 its executable projection.
Plan X52 to X66 alongside retained X47, with the [M2 page](m2.md) owning
their dependencies. B82 is parked without a concrete consumer; B346 joins
M2 for its FIPS access profile. The [provider matrix](../ceph-coverage.md)
records native restrictions and alternate profiles; full coverage never
claims unsupported combinations. B491 must settle the online-cleanup versus
machine-quiescence contract, and B70 remains blocked on physical erase
evidence. This is a specs-only definition request: M1 gates remain, no
implementation slice is activated and no host operation is authorized.

### D130

**owner** (2026-10-11, the owner): M2 supports every cluster size IBM supports
for the qualified release and deployment mode. Features follow the provider's
requirements, including per-site/per-role capacity, topology and service
compatibility. Acceptance profiles are examples, not product limits; include
larger qualifying profiles for supported features that require them.
B472 records the support ranges and requirements, B474/B79/B490 qualify
admission and internal bounds, and B487/B488 cover the service-specific
stretch profiles.
The definition remains planning-only and changes no implementation authority.

### D131

**owner** (2026-10-11, the owner): name the M2 submilestones for future
reference: M2a detailed scope/task definition, M2b simple three-node
libvirt+sushy Ceph, M2c full-featured virtual Ceph, M2d full-featured physical
bare-metal/BMC Ceph, and M2e full-featured stretch for all provider-supported
desired-state configurations and sizes. The [M2 page](m2.md#submilestones)
owns their mapping and exit gates. M2a is definition only; executable work
remains in the existing slices mapped to M2b through M2e. This expressly
permits the suffixed submilestone IDs while retaining M2's single status and
all B/X/D identifiers. Pre-2026-09-28 M2a/M2b aliases keep their historical
meanings. Squash the M2 planning commits into one commit over the M1 base;
no implementation or host operation is authorized.

### D132

**owner** (2026-10-11, the owner): include the complete Ceph API and operator
authoring review in [M2a](m2.md#m2a). Preserve useful existing abstractions
and native declarative formats where they reduce operator learning; expose
imperative-only storage resources as declarative intent. Make small,
justified corrections, but obtain owner approval with current/proposed
examples before adopting substantial API changes. The
[review](../storage-api-review.md) records findings and pending proposals;
this decision does not approve those proposals, authorize implementation,
or declare M2a complete. B494 records the review's bounded admission defects.

### D133

**owner** (2026-10-11, the owner): approve the two substantial M2a API
proposals reviewed under D132. P1 consolidates daemon configuration and
placement into complete native Ceph service documents, with Bootwright
retaining proved Machine/site ownership, release pins and protected secret
bindings. P2 keeps pools/filesystems and adds separately owned declarative
resources for imperative-only objects; NFS exports and SMB resources retain
their native formats. The [native service](../ceph-native-api.md) and
[resource](../ceph-resources.md) specs own the target and migration;
B474 delivers the common schema path and each family item its qualified
extensions. This approves the breaking v1alpha1 authoring target, not a
version change, runtime implementation, sensitive-output command or host
operation. M1 and implementation promotion gates remain unchanged.

### D134

**session-pending** (2026-10-11, the owner): accept M2a's definition baseline and
explicitly authorize M2b implementation, its generic three-node example,
a locally ignored machine-specific copy, static execution review and an
operator setup/provision/test/cleanup runbook. M1's outstanding checks and
owner acceptance remain promotion gates. No privileged operator test or
push is authorized. Test runs resolve the latest provider release available
at setup and pin its exact artifacts for the whole run. Keep verified native
Ceph SSH mandatory: P3's proposed provider-default exception is rejected;
bootstrap remains gated until the entitled IBM artifact proves verification.

Taken by the session on 2026-10-11, pending the owner's review: D134 is the
dated waiver of M2's Requires for the implementation of X47, X52, X53 and X54
only, and it excepts M2b from the two-active-slice WIP limit, so X47, X52, X53
and X54 could be active together; remaining verification, acceptance and
release/operator gates are not waived. Both readings were taken by the
session, pending the owner's review, and [D165](#d165) (2026-10-19, the owner)
returns those slices to `not started` without extending the exception. Both
readings are moot: D165 returned the slices they cover, and since
[D183](#d183) the WIP limit counts per program.

### D135

**owner** (2026-10-11, the owner): approve P4's verified-tunnel design and
qualification. Add `clusterSSH.transport: verified-tunnel` while retaining
required verification: an outer pinned OpenSSH connection forwards only to
the same authenticated host's loopback SSH endpoint, with no direct fallback.
Protected MGR mounts, staged bootstrap, server forwarding restrictions and
restart/failover/recovery are part of qualification. Approval changes the
target API and trust boundary; it grants no provider-default exception,
operator-evidence bypass or automatic artifact promotion. B472/B79/B80 own
the remaining exact-artifact and runtime evidence.

### D136

**owner** (2026-10-11, the owner): approve P5's protected RBD canary retention
until explicit cluster teardown. The fixed fixture owns a 16 MiB image,
retains its ID before writing/reading at most 1 MiB by ID, closes the client
and reports `cleanup: cluster-teardown-required`. No immediate deletion by
name is permitted; exact disposable-cluster removal owns the retained data.
This changes the test retention contract, not public input schemas or
ordinary cluster qualification gates.

### D137

**owner** (2026-10-11, the owner): approve P6's optional libvirt
`dataDisks[].media: ssd`. Explicit SSD data disks use virtio-SCSI with
`rotation_rate="1"`, starting at `/dev/sda`; root remains `/dev/vda` and
omission preserves the existing virtio-blk layout. Mixed profiles use
independent target sequences. Frozen requests, domain inspection, guest
device observation and native Ceph class must agree. This declares virtual
guest media behavior rather than physical host performance; existing
realizations keep their original requests and require teardown before a
changed layout is installed. Actual guest/runtime acceptance remains open.

### D138

**owner** (2026-10-11, the owner): clarify M2b's entitlement-driven artifact
acquisition. After an operator imports the registry credential into the
Entitlement's selected Secret, Bootwright resolves the provider image and
downloads required IBM images/packages itself. Remove the manual image
pre-pull and digest-discovery prerequisite from the example and runbook.
Preserve immutable resolution, original credential custody, verified TLS,
current qualification gates and the separate Red Hat media/source boundary.
This corrects the authorized M2b flow without changing public Storage schemas
or authorizing an agent-run privileged or entitled test.

### D139

**owner** (2026-10-11, the owner): "every backlog to milestone M1 done, except the ones that i need to run myself the test in real bare metal and the one related to a go version that is not available in my machine". Recorded meaning: `make vulncheck`'s go1.26.8 standard-library findings (B470) no longer hold any other M1 item. An item completes when every other exit gate passes (`make check`'s other steps, `make race`, `make docs-check`, `./scripts/ansible-check` and its own tests), or awaits operator acceptance when it also has a host gate; B470 alone carries the vulnerability gate, on its own slice X67; M1 completes only after B470 and the owner's host gates. It amends D126's sentence that the deferral "declares neither X44 nor M1 complete" for X44's items and X48 to X51 (M1 itself stays incomplete), and it settles the 2026-10-11 audit's finding that X46's second pass completed under a different standard in favour of completion, so X46's second pass stays delivered. D129 to D138, recorded on the M2 branch, precede it there.

### D140

**accepted-by D148** (2026-10-11, taken by the session, pending the owner's review): `machine exec` and `cluster exec` stop at the payload, as `cluster oc` and `cluster kubectl` already did: the first operand that is not a flag starts the remote command and every later token is payload verbatim, flag-shaped or not, so a remote command's own `--context` or `--name` can never retarget the session. Bootwright flags go before the command; `--` is needed only when the payload's first token is flag-shaped, and a required Bootwright flag written after the payload is refused with where flags go. This replaces the earlier rule under which a recognized flag after the first operand still took effect; `TestPayloadParsing`, `TestPayloadTailNeverRetargetsTheSession` and `TestAFlagWrittenAfterThePayloadIsRefusedWithWhereFlagsGo` prove it. The owner accepted it on 2026-10-14 (D148).

### D141

**session-pending** (2026-10-12, taken by the session, pending the owner's review): vendor and device names were removed from D60, D104, D109 and D113 and the X45 record on 2026-10-11 for confidentiality; their operative content, restored here, is unchanged by the redaction. The constraints (D109's bastion), the observed firmware behaviors, the check procedures and the RSA-2048 workaround (D113) that the redaction dropped are written back in neutral wording ("the physical BMC under test"), in those decisions, in the X45 record and in the [Redfish knowledge page](../../.agents/knowledge/redfish-physical-bmc.md), and no vendor, model, firmware, serial or host name returns.

### D142

**owner** (2026-10-12, the owner): before 1.x no backward compatibility is wanted. An incomplete operation is bound to the blocks it froze, not to the build that froze them: another build, meaning other automation, another version or commit, or any build stamped `modified`, continues it exactly when it freezes the same blocks, every frozen member except the words they are presented in, and the execution closure check stays. No converter or older reader is added for any record or request version. B504 joins M1 as an exception to D48 and is delivered by X69, an out-of-sequence enabling slice implemented on the M2 branch at the owner's request.

### D143

**session-pending** (2026-10-12, the owner; the clauses after the owner's words are taken by the session, pending the owner's review): "The only binary that should implement all features of bootwright is bootwright itself; the operator declares desired state and runs bootwright apply, with --stage for phases; validation through machine exec / cluster exec; a bootwright command prints the dashboard access info", and, on the M2 branch, "Yes. Work and apply changes on branch M2". This settles [B498](#retired): converge, so the `bootwright-ceph-qualification` executable and its fixture engine are retired. It settles [B497](#retired): M2b's acceptance is an ordinary staged `bootwright apply` over the declared StorageCluster, StoragePool and StorageClient, recorded as an owner-accepted ledger row ([B476](delivered.md#x94--post-destroy-cluster-inspection)); no fixture evidence closes an item, and there is no runtime registry to promote. [X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply) delivers it. Taken by the session, pending the owner's review: (1) each build supports one reviewed, code-pinned release: IBM Storage Ceph 9.9.1.2, image `cp.icr.io/cp/ibm-ceph/ceph-9-rhel9` tag `v9.9.1-25074`, packages `20.2.1-405.el9cp`, cephadm-ansible `5.0.3-1`; a different declared release, repository or tag refuses at admission naming the pinned one, and a newer release needs a reviewed pin bump. This amends D134's "latest provider release available at setup". (2) Apply resolves the pinned tag through the Entitlement's registry Secret to its linux/amd64 child manifest digest in a block that completes before any native effect and is frozen for continuation; an authored `sha256:` digest is admitted only when it equals that resolution; the digest is not code-pinned until B472's lock adds it. (3) `verified-tunnel` is the only supported transport; `native` stays refused (D134, D135). (4) The empty qualification registry and its refusal are removed by B512. Pinned-release admission, verified host keys, data-loss authorization and the frozen plan still protect apply. (5) Every guest proof the fixture made moves into ordinary per-host blocks (B518, B499) and is not dropped. (6) A managed cluster whose hosts are all Bootwright-owned virtual Machines that the same destroy removes is retired with them, with no native call (B515); removal while hosts survive stays [B491](m2.md#b491). (7) The dashboard is enabled after readiness, with user `admin`, a generated password in produced custody revealed only by `bootwright cluster info --secrets --name`, and Ceph's self-signed certificate whose SHA-256 `cluster info` prints. Bootwright never rotates it, and a password changed outside Bootwright refuses as drift. This is the public interface B489 left for owner approval, narrowed to that credential; client export stays B489, and certificates, roles, SSO and monitoring stay B479. (8) B476's 1 MiB RBD test is run by the operator through `bootwright cluster exec` with the bootstrap node's administrator keyring, keeping D136's retention (a retained 16 MiB image, no deletion by name, removed by teardown); Bootwright's canary and StorageClient-material harness retire. (9) `cluster list` and `cluster info` become available for every kind; `cluster rsh` and `cluster exec` connect for managed Ceph (node by name or FQDN, default first in canonical order, argv verbatim, exit 255 for every refusal before the session); ContainerCluster sessions stay [B101](#b101). (10) The owner's request is the dated waiver of M1, X52 and X53 for X70's implementation, and X70 joins D134's exception to the WIP limit; promotion still requires them. (11) A context holding a fixture anchor is retired with the build that prepared it; a later build refuses it, and its only fallback is `bootwright context delete --name <ctx> --purge --allow-orphans`, which abandons that context's Machines, disks and reserved bindings.

### D144

**owner** (2026-10-13, the owner): "check why those steps for vms were not performed in parallel. remember that any step that could be ran in parallel must be done as that, to optimize provision time". Failed blocks are retried concurrently under the admission rules of a first attempt (dependencies `done`, stage selected, once per invocation, exclusive resources free, the engine bound), beside every startable block; unproved effects are still resolved first with nothing beside them, and a selection that admits no failed block still refuses `lifecycle.stage`. This replaces the rule that a retry runs alone. [X71](delivered.md#x71--concurrent-retries) delivers it as B523.

### D145

**session-pending** (2026-10-13, taken by the session, pending the owner's review): the owner proposed to continue an operation under a rebuilt binary by comparing declared versions in place of the content digest, on the premise that the content digest hashes Ansible code. It does not: it is the implementation's contract identity, computed from its identity, its request and evidence versions and its effect pins, and no capability package reads the automation. A fix confined to automation, or to Go code that moves no version or pin, such as X72, X73, X75 and X76, therefore moves the automation digest and no content digest, request byte or version, so D142's rule already continues it once `setup` has projected the fix; the 35-minute destroy of the M2b run was advised, not required. Dropping the content digest from the rule would let a pin change (a Ceph image, the cephadm binary, a package set, a host path) continue an operation half done under the old pin. So: (1) D142's rule stands: a block continues under another build exactly when it freezes the same blocks, content digest included, and nothing here narrows a refusal. (2) A contract guard replaces the human rule that an automation change that alters what an effect is must move its content digest, where a tool can check it: a golden per implementation pins its content digest beside its Go request and evidence schemas and the persisted format versions quoted in the collection's embedded plugins and roles, and changing any of them while the content digest stays fails the build and cannot be recorded (`TestEveryImplementationMovesItsContentDigestWithItsContract`). It sees versions and Go schemas, not meaning: a raw-JSON member, such as the cluster services the Ceph core and host requests carry, is fingerprinted as opaque, and an unquoted, dynamically built or unversioned format is not seen. (3) Every attempt and resolution record names the build it ran under, an optional `build` member that moves no record version. A consequence for the owner to review: the member is compatible one way only, since a build from before X74 decodes records closed and refuses one that carries `build` as "lifecycle record is malformed or unsupported", so once a build that writes `build` has worked an operation, no earlier build can continue or remove it, and the removal under the build in hand is the way out. The alternative, moving the attempt record version, would have stranded every live operation at once. (4) A destroy continuation keeps X69's rule: its removal is derived from the frozen plan and runs under the build in hand, as a fresh removal does, and its proof compares what the removal decides, the authorizations it consumes, its groups and that each block's capability reads its frozen request; it cannot compare content digests, because the removal copies them from the frozen plan. An unknown removal block is observed by the build in hand, whose closed evidence decoding refuses evidence it cannot read, and every removal attempt and resolution names the build that ran it. (5) A continuation previewed under another build names the build that registered it and the steps this build runs, and `status` says whether the build in hand is another build. The preview has no JSON form because `plan`, `apply` and `destroy` have no JSON mode, so `status --output json` carries `anotherBuild` instead. (6) No content digest, request version, record version or plan encoding moves, so the owner's live operation, registered by 91855545e, continues under a build that contains X74. [X74](delivered.md#x74--continuation-under-a-rebuilt-binary) delivers it as B525.

### D146

**owner** (2026-10-13, the owner): "i had to run twice apply to make it work. it shouldn't be needed. check if you can fix it", after an `apply` resolved an unknown Ceph pool block as partly realized and stopped, and a second `apply` retried it and the client after it. This settles B529: a resolution is not an attempt. An invocation still observes a block at most once and never observes one its own attempt left unproved, but a block its resolution leaves `failed` is retried by that same invocation once every unproved effect is resolved, under the admission rules of any retry (stage selected, dependencies `done`, exclusive resources free, the engine bound), so its dependents start there too. A failed resolution admits further first attempts as a proved outcome does; a failed attempt, including that retry, still admits none. A fresh `destroy`'s resolutions of the apply it supersedes retry nothing. The resolution's progress row stays running into the retry and is settled `failed` when the invocation stops before the retry starts. It amends D144's "once per invocation" only in that a resolution no longer counts as the block's turn. [X82](delivered.md#x82--resolution-and-retry-in-one-invocation) delivers it as B529.

### D147

**owner** (2026-10-14, the owner): "you can upgrade go to 1.26.9 and mark this backlog as done". This ends D126's deferral of the Go upgrade: [X67](delivered.md#x67--go-toolchain-vulnerability-gate) moves the pin from go1.26.8 to go1.26.9 and delivers B470, so `make vulncheck`, and with it `make check`, passes again and D139's separation of the vulnerability gate from M1's other items holds nothing more. It moves no other dependency; Go 1.27 stays parked as [B273](#b273).

### D148

**owner** (2026-10-14, the owner accepted the session's recommendation): D140 stands as taken. `machine exec`, `cluster exec`, `cluster oc` and `cluster kubectl` keep the one payload rule of [the CLI contract](../cli.md#parsing-and-input-conventions): Bootwright flags go before the remote command, and every token from the first operand on is payload verbatim, so a remote command's own `--context` or `--name` never retargets the session and `ls -la` needs no `--`. The alternatives the owner declined were the earlier rule, under which a recognized flag after the first operand still took effect, and a mandatory `--` before every payload. No code or contract changes; M1 holds no open decision.

### D149

**owner** (2026-10-14, the owner): "i upgraded my fedora to 44. bootwright should support it", after `setup` refused the upgraded controller with `controller.unsupported`. This joins B541 to M1 as an exception to D48, because M1's closing run (D59, D125) runs on that controller, and [X84](delivered.md#x84--fedora-44-controllers) delivers it: `setup`, `preflight controller` and the controller stage admit Fedora 44 beside Fedora 43, which stays admitted, and RHEL 9.8.

### D150

**session-pending** (2026-10-14, the owner): "update commands bootwright machine start|stop|restart|exec to accept a comma-separated list of machines that the command will be applied in parallel to all". `--name` of those four commands is a comma-separated list under the existing list rules; one member behaves exactly as before and several run concurrently, each power run as today's single-Machine run (at most the engine's bound at once, inside one invocation scope, behind one confirmation) and each exec session non-interactive with its output lines prefixed by their Machine. `machine rsh` stays single because it opens one interactive session, and `cluster rsh` and `cluster exec` are unchanged. Taken by the session, pending the owner's review: (1) a Machine whose resolved identity is a password refuses a several-Machine `exec`, because a password prompt cannot be shared by concurrent sessions; (2) a partial power failure exits `1` with one diagnostic for each failed Machine and still presents every admitted Machine's row; (3) a several-Machine `exec` exits with the highest status any session returned and warns `access.exit` for each nonzero one. [X85](delivered.md#x85--machine-commands-across-several-machines) delivers it as B543.

### D151

**session-pending** (2026-10-14, the owner): "when you finish, do the same for bootwright cluster commands", following the request that made `machine start|stop|restart|exec --name` a comma-separated list applied in parallel (D150). The owner answered two questions. For `cluster exec`: "BOTH `--name` and `--node` take comma lists. `--name a,b` runs on every listed cluster and `--node n1,n2` on several nodes. Each listed cluster runs on its default first node, or on the `--node` members it declares. A `--node` member no listed cluster declares is refused. Every resolved node Machine runs in parallel, exactly like `machine exec` with several names." For `cluster info`: "`--name` takes a comma list too (a selection convenience; local read, nothing to parallelize)." `cluster rsh` stays single because it opens one interactive session, and `cluster info --secrets` still reveals exactly one cluster. Taken by the session, pending the owner's review: (1) a listed cluster that declares none of the `--node` members refuses `access.target` instead of running nothing there; (2) the session prefix is the Machine name, and Machines two nodes share run once; (3) a FQDN shared by several nodes of one cluster refuses as it does for a single cluster. [X86](delivered.md#x86--cluster-commands-across-several-clusters-and-nodes) delivers it as B546.

### D152

**accepted-by D154** (2026-10-14, the owner's request; the clauses are taken by the session, pending the owner's review): "make a full review and audit of how bootwright install/setup works. i think this is too restrict and not optimal about dependencies. for instance, i had my fedora updated from 43 to 44 and it stopped working. you actually fixed this, but i think we could review this process in a broader way. check how dependencies are defined and used. i want bootwright to require those dependencies in a general but safe way. this should work in a similar way other linux packages that requires dependencies work. other thing that is bothering me is how long it takes to run bootwright setup, to install ansible and python dependencies, even after a previous bootwright setup, with older bootwright's version. this make setup take too long (more than 3 minutes). bootwright seems to be using dedicate installation for those package. evaluate if it is really necessary or would make more sense or would be justified to used SO's package for that. how other similar community package handle that? if it makes sense to use a dedicate python installation, for instance, couldn't the bootwright setup from a newer bootwright's version reuse the previous one? ... check alternatives and impact in code and propose a plan for us to refactor this installation/setup use case, using the best alternative, considering estability and ux and community pattern/best practices. no backward compatibility needed/wanted. you can consider a new fresh installation for this new bootwright version." It carries forward the owner's rules of 2026-09-13: "bootwright should just check if dependency packages are available and only if not install; it shouldn't check for updates unless a specific min version becomes necessary", and "versioning logic for packages is over-engineering; only check the required packages exist; add version logic only when a specific version issue appears". Until the owner accepts it, D152 is a proposal and authorizes nothing; acceptance authorizes [X87 to X90](m1.md#planned-slices) on M1 as an out-of-sequence exception to D48, in the form of D149, and must carry the owner's dated sequencing choice for X88 (clause 9). The clauses: (1) The glibc and libgcc byte pin is not a security boundary and is dropped: the per-file digests, link pins, the single-link and exact-upstream rules, D107's requalification, the receipt's `foundation`, the operator guide's hold procedure, the compiled catalog and scripts/foundation-catalog. Root ownership with no group or other write stays on every executed or loaded path, extended to the interpreter's standard library and `lib-dynload` and to the re-executed binary (B548), and specs/security.md states the attacker model and the widenings W1 to W4 once. Declined: digests as a reproducibility check, the cause of the Fedora 44 refusal, since no attacker model was found in which digests of root-owned files stop an attacker who cannot already write them. Supersedes D107. (2) Admission is by family and floors and fails closed: `ID` is fedora or rhel or `ID_LIKE` names one, amd64, a qualified `/usr/bin/rpm` and `/usr/bin/dnf`, a locked-ABI interpreter installed or offered, and glibc at or above the lock's floor (2.34). A release outside the lock's tested list gets the informational line `controller.untested-release` and is never refused; a docs test ties that list to the development guide. Declined: exact (OS, release) rows, the cause of the Fedora 43 to 44 break, refusing untested releases or derivatives, and silent admission. (3) The interpreter is the OS CPython 3.12 to 3.14 at its versioned path `/usr/bin/python3.X`, with one venv per lock, ABI and interpreter path and no python-build-standalone fallback (parked as B561). Python TLS follows the host's OpenSSL and crypto-policy, since `OPENSSL_CONF=/dev/null` goes; D108 is amended: the Go binary and cryptography's bundled OpenSSL stay outside any FIPS claim, and B331 stays parked. Declined: a standalone CPython with a lock, wheels embedded in the binary, distribution ansible-core (no distribution ships 2.21) and an Execution Environment image. (4) Versions are chosen at build time in a lock the binary embeds (internal/controller/runtimelock/lock.json, format `bootwright.controller.runtime-lock-v1`), which `scripts/runtime-lock generate` writes with network, run by the owner or by CI, and which is committed; `verify` runs in a network CI workflow and `outdated` is advisory. Setup never discovers versions or checks for updates, the upgrade verb is a new build, and D10's and D20's "latest patch" means the latest at lock generation, which also meets the rules of 2026-09-13. (5) ansible-core is exactly the 2.21 minor the collection's `requires_ansible` admits, its patch pinned in the lock; a guard test requires the lock, `scripts/tools/ansible-requirements.txt` and the collection's `meta/runtime.yml` to agree. cryptography stays because ansible-core declares it; dropping it is parked as B559. (6) Native packages come from the host's dnf. Requirements are names or capabilities in one Go table, presence is `rpm -q --whatprovides` with no version logic, and before confirmation a root `dnf install --assumeno --setopt=install_weak_deps=False` preview is shown verbatim as display text bounded to 64 KiB; lines naming core packages (glibc, libgcc, kernel, systemd, openssl-libs, the selected python3.X) get an informational note, never a refusal. Install is `dnf install --assumeyes` with the same option, presence is checked again, and each provider's NEVRA and repository are recorded. The host's dnf configuration owns signers, versionlock, excludes, protected packages, mirrors, Satellite and `proxy=`; Bootwright never passes `--allowerasing`, `--nogpgcheck`, `--repofrompath`, `--enablerepo` or `--releasever`. The Ceph node's dnf4 core keeps its files and only the controller-only branches go. Declined: the frozen plan with a cache, a host-configured solver with vendor-only signers, and a protected-set refusal or `--exclude`. Supersedes D106; closes B330 and B542. (7) Automation leaves every setup identity: the embedded `ansible/` tree is extracted for each invocation into root 0700 staging, held by an inherited lock while descendants run; the automation digest and D145's re-proof are unchanged; the runtime digest hashes `runtime-v1`, the lock digest, the ABI and the interpreter path; the closure digest hashes `execution-closure-v2`, the lock digest and the ABI. An `ansible/`-only fix is build and apply. Closes B165, parked by D40, and supersedes D14's per-build area where it describes the runtime. Declined: a persistent automation directory per digest. (8) Retention runs at the end of each successful setup under the setup lock: the current runtime and the previous distinct one and their wheels stay, every other runtime is retired under a non-blocking exclusive lock that skips one in use, and the newest 8 run logs stay. apply, destroy and the controller stage rebuild this build's runtime without network when its directory is missing and every locked wheel is cached, and refuse with "run bootwright setup" only when a wheel is missing. Tool areas move under the controller's tools directory with their own bound of 16 and are retired when no binding names them (B557, closing B322). The bound of 16 on execution areas, retained sources and definitions, and `--purge-old-bundles` go; this supersedes D90 where it bounds execution areas. Declined: keeping only the current runtime, which would strand live operations offline after a lock change, and unbounded rebuild without retention. (9) The cut-over is a fresh install. A setup-owned subtree `/var/lib/bootwright/setup/` stays root 0700, carries the format `setup-state-v1`, lies outside the registry descriptor and heals itself: setup discards another build's format after confirmation, and launches refuse with "run bootwright setup". The controller record moves from version 2 to 3 and the registry from 5 to 6, and a version 5 store refuses with "/var/lib/bootwright holds a store an earlier Bootwright build wrote (registry version 5), which this build does not read or convert" and the remedy "with the build that wrote it, destroy each environment (bootwright destroy --context <name>) and delete each context (bootwright context delete --name <name>); then, as root, remove /var/lib/bootwright and /var/lib/bootwright-staging and run bootwright setup with this build. Never remove /var/lib/bootwright while services it manages run." The owner retires m2b-ceph and the lab contexts with the old build before the first X88 build runs on the owner's host. X87 integrates on acceptance; X88 integrates only after the owner records, dated, either (a) M1's closing run (D59, D125) and B476's run complete on the current build, which the session recommends, or (b) a waiver that runs the closing validation on the X88 build. **Open: the owner's choice.** Declined: a separate world-readable setup root, which contradicts the root 0700 store rule (a readable index copy is parked as owner decision B558), and moving bindings out of the controller record. (10) `--wheel-mirror` (a `file://` directory or an `https://` base) is untrusted transport bound by the lock's sha256; `setup --download-only --dest-dir DIR` runs unprivileged and writes the wheels, the installer and the lock; `--dry-run --output json` lists every locked file's URL and sha256. The default origin is only `files.pythonhosted.org`, native packages use dnf's own configuration, and the admitted context-free `HTTPS_PROXY` and `NO_PROXY` route is forwarded to dnf as `https_proxy` and `no_proxy`, where dnf.conf's `proxy=` wins; credentials and `ALL_PROXY` stay refused. (11) nmstate leaves the baseline, since nothing consumes it; podman stays, because the container-runtime capability is mandatory. (12) The rpm read lock is not held: each launch holds a shared lock on its runtime directory for the whole Ansible block, so retirement never removes a runtime in use and the operator's dnf is never blocked; a same-minor standard-library update during a run is an accepted risk that setup's import probe repairs. (13) Pre-existing defects: the re-executed binary has no owner or mode check, which X87 fixes as B548 in its own commit; controller-record refusals name no remedy, and the registry version 6 and setup-subtree refusals that replace them each name one; the spec's missing native re-solve, the staged `other` metadata, the stale python3.13 defaults and B542 are moot with the code X89 deletes; the operator guide's Fedora 43 examples are rewritten in X89. Parked by D152: B558 to B563. The session integrated [X87](delivered.md#x87--controller-setup-v2-foundations) on 2026-10-14 ahead of this acceptance, as its workflow directed: it is additive and unwired except B548. On 2026-10-14 the owner deferred clause 9's sequencing choice ("implement whatever you can; I will test when I can run"), so the session integrated [X88](delivered.md#x88--controller-setup-v2-cut-over) on local main without a push; the choice and the owner's review of D152 stay open, and the owner's fresh-install acceptance of B555 waits until the owner can run it.

### D153

**accepted-by D155** (2026-10-14, the owner): "today bootwright must accept only this scenario: non-existent real state ... half-provisioned/setup ... real state already equals desired state ... bootwright could have an command: bootwright diff ... do not implement this support yet, just make code, logic and architecture ready for this in the future ... reconciliation must be predictable and safe. target environment can never be broken with bootwright apply". Taken by the session, pending the owner's review: (1) `apply` admits exactly three scenarios: a fresh apply where the context's records show no operation or a completed destroy, a continuation of an incomplete apply over byte-identical input, and an idempotent settle of a completed apply over unchanged input. Changed input over a completed apply keeps refusing before any effect and names what changed, with destroy then apply as the remedy; `TestApplyAdmitsOnlyTheThreeScenarios` pins the placement. (2) `bootwright diff` is read-only: it compares the context's selected desired state with what its apply realized, object by object, reads durable records only, or with `--observe` each realized block's target through that block's read-only observation, writes no record, takes no lock beyond the shared read lock and exits 0 whenever it renders. (3) Every further scenario is added one per named slice by the recipe in the design: a decision, a spec row and a `stateMachineRows` case, the prerequisite items, and one branch in the changed-input path, all in one commit; none is implemented here, and this slice builds only seams with a present consumer (the comparison, the read-only guard, the retained-revision reader). (4) The settled scenario means "the records prove the frozen plan done over unchanged input": a settled apply attests its own records and reaches no host. Drift that `diff --observe` shows is reported, never repaired. [X91](delivered.md#x91--read-only-diff-and-the-delta-seam) delivers it; the audit's blocking findings [B567](#b567) and [B568](#b568) stay parked and recommended as a dedicated safety slice before any further `destroy` on a shared libvirt host. Needs the owner's decision: whether a settle also requires the current build to freeze the same plan ([B574](#retired); the recommendation is not now). Needs the owner's decision: whether input restored byte for byte after an edit settles a completed apply ([B598](#b598)). Needs the owner's decision: the libvirt redefine semantics and what machine evidence proves ([B576](#b576)). Needs the owner's decision: whether Ceph pool `size` and `min_size` drift is refused or reported ([B587](#b587)). Needs the owner's decision: the context incarnation identity design ([B568](#b568)).

### D154

**owner** (2026-10-15, the owner): "mark all backlogs that we validated so far
as done; save this fix as a new backlog; we will handle it and §9 in near
future", and, asked, accepted D152 with clause 9's option (b) and the
closing run's B339 observation as met. The owner accepts the ledger rows of
[X93](delivered.md#x93--the-owner-run-of-2026-10-14) for B19, B20, B270, B276, B277,
B339, B549 and B555, which leave M1; accepts D152 as written, whose clause 9
is (b): the closing validation ran on the X88 build; and records the
reboot defect as [B608](delivered.md#b608) on M1, where it blocks B466. M1 stays
in progress, and B476's run (M2b) follows later. D156 subsequently closes
the remaining M1 gates.

### D155

**owner** (2026-10-16, the owner accepts the recommendations): D153 clauses
1 to 4 stand as written. B574 is retired: settlement continues to attest only
the completed frozen records over unchanged input, without re-planning or a
host check. B598 adopts exact-original-byte restoration as a future scenario;
until its named slice, the current code and changed-revision refusal remain.
B576 will narrow claims to observed evidence, report an actual redefine as
changed, and widen read-back with each supported field. B587 will refuse
detected pool `size` or `min_size` drift without writing until a named delta
admits it. B568 adopts a persistent random context incarnation design;
exact formats and migration are defined in its named slice. Define and
schedule B567 and B568 before shared-host destroy: the B567 guard and B568
interim orphan warning come first, full incarnation identity before later
deltas. These are accepted directions for future work, not authorization to
implement those items now. The owner authorizes the bounded post-destroy
inspection correction as X94/B613. Conditional agreement to finish M1 does
not satisfy its gates: B608 is unimplemented and B466's host-restart rerun is
missing, so M1 remains in progress. The owner accepts B476's
[ledger row](../../docs/acceptance.md) and explicitly attests that dashboard
fingerprint comparison, successful login and expected host/OSD inventory
checks passed. This closes B476's operator gate on 2026-10-16; M2b promotion
still requires M1 and its remaining core/prerequisite gates. D156
subsequently satisfies M1; the other M2b gates remain open.

### D156

**owner** (2026-10-16, the owner): the standing acceptance of passing criteria
in conversation applies to the completed B466/B608 journey. The owner
requested the records after the actual fresh apply, controller host restart,
automatic network and managed-service recovery, guest startup/access checks,
safe removal, evidence preservation and context purge passed. Accept B466
and B608 on clean tested build
`591af9616446596a77a599cd6303970a85131694`, recorded by
[X98](delivered.md#x98--m1-owner-acceptance) and the existing acceptance
ledger. No separate hand-back form or further approval is required.
M1 is done on 2026-10-16 at that tested build, with no open item or planned
slice. M2's M1 prerequisite is satisfied; M2b's remaining core/prerequisite
and B80 promotion gates remain open. B476's acceptance under D155 stands.
This decision neither completes M2 nor authorizes unrelated implementation.
Runtime facts, exact commands, digests and raw evidence are retained locally.

### D157

**owner** (2026-10-16, the owner): require [B615](delivered.md#b615) before M2b
completes. Operators may supply any IBM Ceph release, package/tool version,
container tag/digest and related version parameter, even nonexistent or
incompatible selections. Bootwright attempts those selections through native
package, container and Ceph tools and reports their native errors. It does
not validate version compatibility, register IBM's release table, maintain
approved-version catalogs or require per-release profiles or approval.
This supersedes D143's single-release admission and pinned-tag selection
policy, the corresponding B472 restriction and B500's proposed release
profiles. Independent authenticity, transport trust, authorization and frozen
artifact/continuation requirements remain. The owning contract is
[storage admission](../api/ceph.md). X99 was planned and not started at
this decision; its later authorized implementation and acceptance are recorded
in [X99](delivered.md#x99--operator-selected-ceph-artifacts-and-m2b-completion).
This initial request authorized backlog/specification work, not implementation.

### D158

**owner** (2026-10-17, the owner): redefine M1 as Foundations: substrates,
infrastructure components and Machines. Group it as M1a API review, M1b
repository/code structuring, M1c libvirt+sushy, M1d infrastructure components,
M1e bare metal, and future M1f vSphere and M1g OpenShift Virtualization.
Move unfinished shared foundation work from M2/M3 into M1: X47, X55, X60,
X62 and X63 retain their IDs and item states; B78 moves out of X64 into
M1e's X100; B68 moves from M3 to M1d. Shared lifecycle safety B18/B23
moves to M1b, machine/network exclusion B265 to M1c, and the generic
controller/check-tool procedure B509 from M2 to M1c. Move parked B99 to M1f and B87 from
M5 to M1g, retaining IDs and requiring definition before implementation.
B100 and other parked work stay unpromoted. M1a/M1b map the previously
accepted hardening work; this declares no new audit or restructuring done.
Reopen M1 for its remaining foundation scope as an explicit exception to
the done-milestone rule. Preserve D156/X98's historical completion, every
accepted gate and M2b's completion under X99. B/X IDs and dated decisions
stay fixed; the historical M1a to M1g delivery labels retain their meanings
and distinct links. This changes planning history, not Git history.
M2 retains Ceph effects and service/security compatibility, including
CephX/FIPS and daemon compatibility in B472/B484/B485. M3 retains OpenShift
cluster delivery and M2 as a prerequisite. Moved B509 still gates full M2;
moved B18/B23/B265 still gate M3 completion, without reopening accepted
checkpoints or becoming prerequisites of every implementation slice.
M5 retains B88's OpenShift
cluster lifecycle and requires M3 plus B87. M1g consumes an existing
qualified host cluster and does not require Bootwright to provision it.
Consumers require the foundation capabilities they use, not all of M1;
vSphere and OpenShift Virtualization do not gate libvirt/bare-metal delivery.
Schedule shared foundation work with its first consuming product checkpoint;
M2c remains next with its required foundation gaps. M1f/M1g are future work
and do not take scheduling priority merely because they belong to M1.
All M1 items still gate M1's eventual completion. The WIP limit, promotion
gates, safety refusals and operator acceptance requirements remain. This
request authorizes roadmap and ownership changes only, with no new active
slice or substrate implementation.

### D159

**session-pending** (2026-10-18, the owner's requests; the clauses after the owner's
words are taken by the session, pending the owner's review): on 2026-10-15
the owner asked to "start planning and then implement M2c, using best fitting
model/effort for each subtask", and on 2026-10-18, after D158 and M2b's
completion, said "resume". The session reads them as authorizing
[M2c](m2.md#m2c)'s slices X56 to X59 and X61 and, as the foundation gaps M2c
requires under D158, M1c's slices [X55 and X60](m1.md#planned-slices). It
replaces the session's earlier activation records on the unmerged branch
`m2c`, whose decision and item numbers collided with main's and are not used.
Taken by the session, pending the owner's review: (1) M2b is done, so nothing
of it is waived; M2's Requires on M1's X55 and X60 is waived for the
implementation of X56 and X61 only, and X55's Requires on X47's trust
contracts and X60's and B346's Requires on B448 are waived for implementation
only; promotion of B74, B326, B75 to B77 and B346 still requires them.
Promotion of each M2c item still requires the M1 features its profile uses,
and M2c's exit stays [B484](m2.md#b484)'s owner-accepted matrix on the commit
closing X61. (2) X55 to X61 are excepted from the two-active-slice WIP limit
and may run out of sequence. (3) The M2c program's dependency graph fixes the
landing order of its lanes; the Requires between these slices name the work a
lane builds on and do not force whole-slice integration order. (4) The work
lands on the local branch `m2c`, never pushed, rebased onto main `a38a6d183`:
this docs commit opens it, the code commits of the lanes that had landed
follow it, and each later reviewed lane is rebased onto the branch tip and
fast-forwarded by one serialized landing step; slice checkpoints run the full
gates and an adversarial review of the slice's commits. Merging `m2c` waits
for the owner. A version-specifics slice on main and this branch integrate in
landing order: whoever lands second rebases, and no M2c lane adds
release-conditional behavior outside that slice's architecture. (5) In the
replay, each replayed commit that moved a frozen version moves it one step
from its value on the replay tip, commit by commit; after that, each lane
that changes a frozen type moves that version exactly one step from its value
on the branch tip, lanes holding the same version are ordered by the graph,
and no reader of an older version is added.
A context made by an earlier build, including every build of `m2c` before this
rebase, is finished or destroyed with that build. (6) X56's "Requires D143"
means D143's transport and retirement baseline as D157 amends it, and waives
nothing else. (7) One execution mechanism for services the cluster block does
not own: the cluster block applies mon, mgr, osd and mds in its one services
batch with its deadline formula unchanged; RGW and its ingress run in the
gateway block; NFS, monitoring, the management gateway, NVMe-oF gateways and
rbd-mirror run in one generic service-unit block type with per-type hooks;
core observation tolerates a delegated daemon only when its service is listed
in the request and it runs on that service's hosts; per-host and per-service
intent records extend the formats M2b delivered. (8) Families tied to one
cluster are actions of the core Ceph request; SMB, the NVMe-oF subsystem and
cross-cluster replication are their own implementations; the host request
carries only host-relevant facts. (9) A settings block is the single writer
of every `mgr/cephadm/container_image_*` key a declared family needs, each
from its frozen digest, and of every manager module a declared family derives
(nfs, smb); cephadm enables the prometheus module itself for a declared
prometheus service; `spec.ceph.mgrModules` admits none of them; a key holding
a foreign value refuses without overwrite. Bootwright keeps no auxiliary
image default; their source is M2c's question OD28. (10) Every cluster child
block depends on its core cluster block by block ID, not through Requires
StorageCluster. (11) Post-install network convergence runs on every installed
anaconda Machine ([B326](m1.md#b326)), which amends the B326 sentence that
B80's minimal profile uses the delivered install path (D129). A network
declaration error found by convergence is recovered by `bootwright destroy`
and a fresh apply; destroy keeps a physical Machine's installed system, so
its operator also powers it off from its management controller before that
apply ([B623](m1.md#b623)). (12) [B76](m1.md#b76) never publishes
registration secrets: its exit evidence reads "secrets reach the guest only
on a fixed worker's stdin and are never published" instead of "over the
private path". (13) The defaults listed under M2c's question OD25 are
implemented as recorded, except B81's erasure-profile and jerasure rows, which
D157 removes because Bootwright checks no vendor support, and the FIPS service
list, which OD29 asks about. (14) M2c's questions OD1 to OD31 (OD8 merged into
OD7) are listed on [its page](m2.md#m2c-open-decisions); until answered, the
lanes a question gates do not start, every gated lane starts only after the
decision-free program has landed, and for each non-gating open question the
option named under Running by default is implemented. (15) No product string
claims or cites a vendor support status or a release: a refusal of a feature
Bootwright does not realize names the object, that Bootwright does not realize
it and the next step, and may name its parked item; a restriction whose only
basis is a vendor support table is removed (D157). Decision and question IDs
never appear in operator-visible strings. (16) Under D158, Ceph on FIPS-mode
hosts belongs to [B484](m2.md#b484) and the Ceph controller-to-host
crypto-policy defect to [B625](delivered.md#x61--ceph-transport-crypto-policy), both in X61; X60 keeps the OS arms
B75, B76 and B77 and controller access B346. Each overturned clause reopens
the work it shaped as a new item.

### D160

**owner** (2026-10-15, the owner; recorded on 2026-10-18 under D159): after
reading the M2c decisions brief (questions OD1 to OD27, OD8 merged into OD7),
the owner replied: "I accept recommendations". The owner accepts the
recommendation of every question OD1 to OD27 of
[M2c](m2.md#m2c-open-decisions), which answers them as follows; a bracket
says where a later owner decision changes an answer's premise. OD1 (a): M2c
lands on the local branch `m2c` [M2b is done (X99), so "main stays B476's
build" no longer applies; OD31 asks when M1c's X55 and X60 reach main]. OD2
(a): per-resource online removal and B489's single-client revocation move to
[B491](m2.md#b491); the family items keep whole-cluster retirement. OD3 (a):
StorageObjectUser and StorageObjectBucket, with the example records of the
X56b slice plan, 1.1, approved. OD4 (a): `objectUserRef` user-level RGW NFS
exports. OD5 (a): the owner supplies the then-pinned image's auxiliary tags
and refusing pin tables guard until then [superseded by D157: Bootwright
keeps no pin table or image catalog; OD28 asks how auxiliary images are
selected]. OD6 (d1) if the image facts show loki and its agent, else (d2)
[D161 settles (d1); under D157 the selected release's cephadm refuses a
service type it does not know]. OD7 (a): park SSO, the direct-dashboard
certificate and local roles, and revisit (b) only if the image facts show
that the IBM oauth2-proxy template verifies the IdP [reopened and answered by
D161]. OD9 (a). OD10 (a), a cluster keyring command, with the output shape
(o1) [the shared output presentation carries (o1): compact identity and
health, full scope and endpoints]. OD11 (a): StorageExport material moves to
M4's [B86](m4.md#b86). OD12 (a): one StorageNVMeoFSubsystem kind. OD13 (B):
host-NQN authorization plus gateway mTLS now, and (C) DH-HMAC-CHAP after the
live probe. OD14 (B): refuse `linked_to_cluster`. OD15 (A). OD16 (A) and OD17
(a): IBM's physical image names through `Entitlement.registry` plus two
package-mirror fields, and the pinned tag resolved on the declared mirror
[D157's authored image base and `ceph.packageRepository` change both
premises; OD30 re-asks them]. OD18 (A). OD19 (a). OD20 (c): a pre-stop
removal phase. OD21 (b): IBM per-guest sums plus 2 GiB [M2b's capacity
admission also refuses an apply that does not fit the provider host]. OD22
(a): a bounded item split from B79 and B475 into M2c. OD23 (a): a larger
single lab host, which the owner provides. OD24 (A). OD25: every session
default is accepted as recorded [D157 removes B81's erasure-profile and
jerasure rows, which came from a vendor table; isa stays the default; OD29
asks whether the FIPS service list stays]. OD26 (a): refuse and park [the
refusals name no release or decision, D159 clause 15]. OD27 (a) for NFS over
RDMA and CephFS snapshot mirroring from IBM's public pages, and (b) for iSCSI
and the RHEL FIPS row [under D157 Bootwright records no vendor status: NFS
over RDMA, CephFS snapshot mirroring and iSCSI refuse as features Bootwright
does not realize, and the RHEL FIPS row becomes OD29's service-list question].
The gated lanes start only after the decision-free program has landed (D159
clause 14).

### D161

**session-pending** (2026-10-15, a session finding and the owner's answers; recorded on
2026-10-18 under D159; the clauses after the owner's words are taken by the
session, pending the owner's review): (1) Session finding, not an owner
answer: the session read the then-pinned image
`cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-25074` (manifest digest
`sha256:c5b181f15d4ebff833a1c0de84398d2d55ff416d9dd5ed3816a9baa817eb4bde`;
`ceph-common` and `cephadm` `20.2.1-405.el9cp`) read-only with the network
disabled and recorded its auxiliary image defaults, known service types and
oauth2-proxy template as
[research](../../.agents/knowledge/ibm-ceph-auxiliary-images.md). Under D157
they describe that release only and are not Bootwright defaults; this
supersedes the session's earlier reading that the image answered OD5. OD6
resolves to (d1): `alloy`, `loki` and `promtail` are known service types
there. `iscsi` is still a known type, which establishes no vendor support.
The oauth2-proxy template writes `ssl_insecure_skip_verify=true` only when
the spec field (default false) is true, so D160's condition for revisiting
OD7 held. (2) The owner's answers, to the session's questions: OD7: "Also
private-CA IdPs". oauth2-proxy SSO is admitted for an IdP whose certificate
chains to a CA in the selected image's trust store and, for a private-CA IdP,
through a Bootwright-bound CA bundle mounted into the oauth2-proxy container
through the native `custom_configs`; `ssl_insecure_skip_verify=true` always
refuses. Because the release is the operator's (D157), verification is proved
from the deployed daemon's rendered configuration, never from one release's
template. This supersedes D160's OD7 (a) for SSO and adds a gated SSO lane to
X57; the direct-dashboard certificate and local roles stay parked. OD15: "No
AD available". Both SMB authentication modes are implemented and
offline-tested; the active-directory live row stays unrun and is recorded as
such. OD23: "Bigger host later". The session builds the larger profiles and
their runbooks, and the owner runs their rows when the host exists. OD27:
"Leave them open"; D157 later settled them: Bootwright records no vendor
status, and only the FIPS service list remains a question (OD29).

### D162

**delegated** (2026-10-18, the owner requests this architecture and readiness
for the listed versions; clauses (b) to (d) are the design's recommendations
taken under that request, which the owner may revise): adopt version
specializations as defined in
[architecture](../architecture.md#implementations-and-version-variation),
after the owner asked to "design and implement a general architecture and
code structure" in which code has one general flow, the default for any
component version, and a small specialized flow for a given version handles a
known issue or dependency and then returns to the general flow, with the
specifics in code and in repository knowledge with references, and to be
ready for the provider-supported versions from OpenShift 4.21, IBM Storage
Ceph 9.9.0.x, IBM Fusion 2.13 and Fusion Data Foundation 4.21 to current. It
refines D157 and does not reverse it.
(a) Each component keeps one general flow. Within one implementation, an
adapter or driver may deviate from it only through a registered
specialization: a record with an id, a declared seam of that flow, a declared
variant, and a condition over facts that pure detectors derive from the bytes
of the operation's frozen, authenticated artifacts. Only when no artifact
feature exposes the difference may the condition use a proved coordinate,
ordered by the component's own comparator. An unknown fact or an unparseable
version selects the general flow and is never by itself a refusal. A
specialization adapts how a step reaches its declared outcome, and the flow
then returns to the general flow. It never approves, screens, refuses,
selects or substitutes a version or an artifact. It never changes a block's
intent, ordering or authorization. It never weakens trust, host-key
verification, publisher signatures, redaction, an ownership proof or a trust
refusal. Each specialization has a row with evidence in the
[version-specialization catalog](../../.agents/knowledge/version-specializations.md),
and a test keeps each registry and the catalog equal. This shape applies to
every registry, whatever its language, including later add-on drivers.
Specializations are not per-release profiles or a release catalog. They key
on observed capability, exist only where the general flow is known to fail,
and gate no selection.
(b) `spec.distribution.ibm.callHome: disabled` is admitted by planning, and the selected
build decides how it is realized, as [storage API](../api/ceph.md) states.
Every refusal of a Call Home intent comes before the cluster is claimed but
after host preparation, so correcting it is an input change that needs the
operation's destroy and a fresh apply; planning no longer refuses it first.
(c) D157's non-screening extends to OpenShift release minors. The agent
installer's accepted control-plane counts are a general rule for every
minor, with per-minor specializations only where an installer differs. A
minor with no specialization takes the general rule, and the installer
judges the release. This narrows the per-minor refusal of
[container clusters](../api/openshift-clusters.md#topology).
(d) Provider OS and version pairings, such as IBM Storage Ceph 9.9.0.x on
RHEL 9.7, and Fusion, Data Foundation and OpenShift compatibility are
recorded as dependency rows of the catalog; Bootwright does not gate on them
(D157), and native tools judge installability.
This request authorizes the specification and the version-specialization
slice, with its 9.9.0.x first wave. It authorizes no M3 node-role work, no M4
add-on work and no evidence-format move. The evidence member rides the next
coordinated Ceph contract move.
D163 supersedes clause (d).

### D163

**owner** (2026-10-19, the owner): Bootwright registers and enforces no
component version rule: no pairing or compatibility rule between component
versions, such as an IBM Storage Ceph release requiring a RHEL release, and
no Fusion, Data Foundation, OpenShift or Ceph compatibility rule. Neither
code nor repository knowledge records such rules; choosing versions is the
operator's responsibility. This extends D157 to every component and
supersedes D162 clause (d). Version specializations stay: they adapt native
mechanics to capabilities of the selected artifacts and never approve,
refuse or recommend a version. The owner also authorizes the defect work for
the confirmed general findings of the 2026-10-18 audit of main
fcff2f58f..ce190446a, planned as X102.

### D164

**owner** (2026-10-18, the owner; recorded on 2026-10-19): the owner answered
the four questions OD28 to OD31 that D157 and the rebase onto main created for
[M2c](m2.md#m2c-open-decisions), choosing the recommended option each time.
Their selections: OD28 "Authored per family (Recommended)"; OD29 "Drop the
list (Recommended)"; OD30 "Trust + closure (Recommended)"; OD31 "M1c at
checkpoints (Recommended)". They settle the following. (1) OD28 (a): a
StorageCluster field `ceph.images.<name>` with `base` and `version`, like
`ceph.image`, keyed by cephadm's `container_image_<name>` names, required
only when a declared family needs that image. Each is resolved and frozen by
digest, and the settings block writes the frozen digests. Bootwright keeps no
default and no table; examples author the values and the owner confirms them
for the authored release, because the research on one release's images is not
a source for another. This replaces OD5 (a) of D160 and the OD5 part of D161
(the image facts stay research). The pull broker's scope grows from one frozen
repository@digest to the frozen set (lane X56b-E1), and the lanes implement this answer in place of the
running default (a family that needs an auxiliary image refuses at plan),
which no lane has built.
(2) OD29 (b): every declared service is admitted on FIPS-mode hosts. A service
that cannot run under the OS's FIPS policy fails natively and the operator
sees its cause through nativeError, then recovers by destroy and apply.
Bootwright keeps the host FIPS proof and the Ed25519 and AES256K key
refusals, which are mechanism refusals. This replaces OD25's service list and
OD27's RHEL FIPS row; x61-ceph-fips and od29-fips-service-list implement
no service allow-list (none was built), and [B484](m2.md#b484)'s FIPS rows record the
tested services as evidence, never as an allowlist. (3) OD30 (a): D157's
naming stays, so the authored registry, image base and `ceph.packageRepository`
are the disconnected naming, and no physical-name rule or second pair of
package-mirror fields is added; OD16 (A) and OD17 (a) of D160 are superseded.
[B493](m2.md#b493) adds the registry trust-bundle consumer
([B619](delivered.md#x59--registry-trust-bundle-consumer)), a CA-bundle binding for the package repository (one new
field), auxiliary-image closure and an egress-denied proof with a disconnected
example. The lanes x59-d1 to x59-d4 are re-planned to that scope before their
briefs; x59-d5 and X61-L7 keep their purpose. (4) OD31 (b) changes the
integration cadence of D160's OD1 and D159: when int-x55 or int-x60 passes
(int-x55 re-runs on the replayed tip), the session offers that M1c slice's
commits to main for the owner to merge, then rebases `m2c` onto main. M1c's
X55 and X60 are therefore offered to main at their checkpoints; M2c's Ceph
slices stay on `m2c` until the owner merges them. OD1's branch answer stands,
and the owner still decides each merge: nothing merges or is pushed without
that decision. The four answers gate no lane any longer; the gated lanes
still start only after the decision-free program has landed (D159 clause 14).
The brackets in D159, D160 and D161 that name these questions are history.

### D165

**owner** (2026-10-19, the owner): the owner answered the seven questions that
X102 and X103 left open (B650 to B656). (1) B650: the libvirt-3-nodes-ceph
example authors the build tag `v9.9.2-26729`, which the owner confirmed
exists in the entitlement, instead of the alias `v9.9.2`. (2) B651:
`machine info --name` and `secret info --name` refuse an explicitly named
unknown resource with `access.target`, like `cluster info`, instead of
selecting no rows; an omitted `--name` still lists all, and a separator-only
list is a usage error. (3) B652: public acceptance evidence in the
[acceptance ledger](../../docs/acceptance.md) is a summary row (date, build
commit, command sequence, outcome) plus the operation-log digest of the
private evidence archive, and an agent may record an acceptance the owner
states explicitly. (4) B653: the owner confirms authorizing B615's
implementation on 2026-10-16 and accepting B615 and M2b on 2026-10-17; M2b
stays done, and the fixes after its acceptance (named in X102) are
post-completion corrections. (5) B654: slices X47 and X52 to X54 return to
`not started`, carrying their full-M2 remainder; D134's M2b-only WIP
exception is not extended. (6) B655: the owner authorized commit 2dc470bff,
the shared CLI output presentation, including the
`github.com/charmbracelet/x/ansi` dependency; it is recorded as delivered
under M1 (M1b) in [X104](delivered.md#x104--owner-answers-of-2026-10-19).
(7) B656: the controller's `controller.untested-release` warning (the runtime
lock's `tested` list, warning only) is an explicit exception to D163, because
it concerns Bootwright's own host and never refuses; the
[controller spec](../controller.md#supported-hosts) records it.
*Correction (2026-10-20):* the tag in clause (1) does not exist in the
registry; `v9.9.2-26947` replaces it under [D171](#d171).

### D166

**owner** (2026-10-19, the owner): the owner answered the session's question
about the two audit defects that X102 parked and that held eight M2c lanes,
choosing "Fix both in M2c (Recommended)". (1) [B660](#b660): the Ceph route
probe always generates ssh-ed25519 synthetic keys, so a FIPS-mode Ceph host
cannot pass host preparation and its refusal reads as a route failure; M2c
lane x61-ceph-fips fixes it with ECDSA synthetic probe keys on FIPS hosts.
(2) [B664](#b664): the IBM package publisher key is always fetched from the
public IBM site even when `ceph.packageRepository` names a mirror; M2c lane
x59-d4-host-pull-route fixes it, and the key comes from the declared mirror
under its trust bundle. The answer releases the program's two holds: the
lanes x61-ceph-fips, od29-fips-service-list, x61-host-variants-example, X61-L6
and int-x61-held, and the lanes x59-d4-host-pull-route, x59-d5-disconnected-profile
and int-x59-held. It changes no product spec and no delivery state; the
items stay parked and name their lanes.

### D167

**owner** (2026-10-19, the owner): the owner answered the session's question
about how destroy tells an installation that never registered from one whose
registration it cannot read, choosing "Install evidence (Recommended)". The
X60 checkpoint found that `spec.subscription` stays refused after destroy's
pre-stop unregistration was built, because no rule decided that difference
([B76](m1.md#b76)). The rule: at destroy, the installation block's own apply
evidence decides. Registration runs only after the install marker is proved,
so an installation whose marker was never proved never registered, and the
pre-stop removal skips unregistration for it. A proved marker with an
unreadable registration refuses before any effect, naming the Machine, the
reason and a manual next step. M1c lane x60-subscription-lift (slice X60,
item B76) implements the rule and lifts the `spec.subscription` refusal. The
answer changes no product spec by itself and no delivery state: B76 stays in
progress until that lane lands, and the lane updates the owning spec.

### D168

**owner** (2026-10-20, the owner): the owner instructed that "finished lanes
that are complete and would make main functional with the feature it
implemented can be merged to main as soon as they are done". This supersedes
the owner-merge step of [D164](#d164) (OD31 (b)), D160's OD1 and D159
clause (4), "merging waits for the owner" / "Merging `m2c` waits for the
owner": the session merges a checkpoint-passed,
feature-complete slice to local main as soon as it is done, without asking,
and pushing still waits for the owner. A lane that is a dependency of such a
slice and is itself complete goes with it and is reported (X61's first lane,
the matrix catalog and refusal table, went with X56); a lane of a slice whose
checkpoint has not passed, or whose gated lanes are open, stays on the local
branch `m2c`. The instruction changes no product spec. It records X56 and X57
in-tree: [B81](m2.md#b81) and [B492](m2.md#b492) await operator acceptance,
and [B477](m2.md#b477), [B478](m2.md#b478), [B479](m2.md#b479) and
[B489](m2.md#b489) stay in progress for their gated lanes. It also records
[B686](m2.md#b686) and [B687](m1.md#b687), found at the int-x57 and
int-x55-r checkpoints.
The same rule merged X58's in-tree work and the lane od28-aux-image-source
(D164's OD28 (a)) after the int-x58 checkpoint: [B482](m2.md#b482) awaits
operator acceptance, with the active-directory live row unrun (D161, OD15);
[B481](delivered.md#x58--iscsi-refusal) stays in progress for the vendor support statement the
owner left open (D161, OD27); and [B477](m2.md#b477), [B478](m2.md#b478) and
[B479](m2.md#b479) plan their families once the images are authored.
It then merged X59's in-tree lanes after the int-x59 checkpoint, with the
docs commit of D166 that assigns [B664](#b664) to the lane
x59-d4-host-pull-route: [B619](delivered.md#x59--registry-trust-bundle-consumer)
is delivered; [B483](m2.md#b483) stays in progress for the replication lanes
(D160, OD18 (A) and OD19 (a)) and [B493](m2.md#b493) for the host pull route
and the disconnected profile.
It then merged X61's lane x61-ceph-transport-policy after the int-x61
checkpoint: [B625](delivered.md#x61--ceph-transport-crypto-policy) is
delivered; [B484](m2.md#b484) stays in progress for the acceptance matrix
lanes X61-L2 to X61-L9 and the lanes D166 released, x61-ceph-fips,
x61-host-variants-example and X61-L6 (none implemented).
Finally, by the owner's order of 2026-10-20 to merge everything already
implemented and ready, it merged the remaining checkpoint-passed lanes of
X57 and the definition of X61's cluster execution budget (the int-ready-v4a
checkpoint) together with D169 to D171: [B686](m2.md#b686) (crash collection
as a derived Ceph service unit) awaits operator acceptance;
[B627](delivered.md#x57--applied-specializations-in-ceph-evidence) is
delivered; [B479](m2.md#b479) (log collection) and [B489](m2.md#b489) (a
StorageClient scoped to a CephFS filesystem or subvolume, the `cluster
keyring` export, and managed StorageExport material named as
[B86](m4.md#b86)'s) stay in progress; and [B688](m2.md#b688), split from B79
and B475, is Specified by the owner's acceptance (D170) with no code. It
records [B689](m2.md#b689) (Ceph file logging for a declared log agent) and
B690 (the Go toolchain's move to go1.26.10, which clears
GO-2026-6809 in `make vulncheck`, delivered by [X156](delivered.md#x156--go-toolchain-vulnerability-gate-go12610)). Under that order M2c is paused: X57's SSO,
all X56b and X58 lanes, X59's host pull route, disconnected profile,
replication and multisite lanes, B620's field removal and X61's other lanes
are not implemented, and no M2c item has operator evidence.

### D169

**owner** (2026-10-20, the owner): the owner answered the session's three
questions for M2c's follow-up program, choosing the recommended option each
time. (1) [B620](delivered.md#b620) (`ceph.ibm.packages.source: subscription` and
`ceph.osSubscriptionRef` are admitted but never read): "Remove the fields
(Recommended)". Both fields leave the API (pre-1.x, no compatibility) and may
be re-added when a subscription package source is implemented; an M2c lane
removes them with admission, spec and example updates. (2) [B627](delivered.md#x57--applied-specializations-in-ceph-evidence)
(the core and host evidence `specializations` member and its `cluster info`
line): "Carry it in M2c (Recommended)". The next M2c lane that moves the Ceph
core contract adds the member and the line with B627's exit evidence, so no
version moves for B627 alone. (3) [B664](#b664)'s fix in lane
x59-d4-host-pull-route: "Add the field (Recommended)". A new required field
`ceph.packageRepositoryKey` whenever `ceph.packageRepository` is authored,
using the repository's scheme, host and port and its trust bundle, with the
key's fingerprint matching the code pin, as B664's recorded outcome requires.
The answers change no product spec by themselves and no delivery state:
B620, B627 and B664 stay in their current state until their lanes land, and
each lane updates the owning spec.

### D170

**owner** (2026-10-20, the owner): the owner accepted the definition of
[B688](m2.md#b688) (Ceph clusters of three to six hosts: the deadline formula,
concurrent per-host rounds and batched enrollment) that commits 16116f4b1 and
6d140d248 landed on `m2c`, and answered the session's question about its OSD
variants with "Accept, variants in B688 (Recommended)". DB/WAL, encrypted and
TPM2 OSDs on explicit devices stay in B688 with one added lane after the
enrollment lane, m2c-osd-variants, which moves the core request version one step
from its value at that lane's base; the alternative of leaving them with
[B475](m2.md#b475) (X53) is not taken. B688 becomes Specified with the lanes
m2c-cluster-budget-plan, m2c-cluster-budget-enrollment and m2c-osd-variants,
and the amendment of D159 clause (7) in it is accepted: the services and
enrollment calls count toward the cluster deadline. B659's continuation after
a partial enrollment and clusters above six hosts stay out of B688. The
decision changes no product spec by itself and no delivery state: B688 stays
not started until its first lane lands, and each lane updates the owning spec.

**Definition record moved from B688 (2026-10-27, F15-09).** The original findings, design point, alternatives, lanes and in-tree corrections follow, as B688 recorded them.

**Original findings.** The former formula (7n+6 calls, internal/storage/ceph/materials.go)
under-counts its own sequence: the per-host tunnel preflight, OSD preparation
and per-service calls were added without the formula growing. At the three-node
example the run can make 35 calls of 660 s against 27 counted, so the deadline
is 5280 s short of the per-call bounds, and the 2026-10-13 note in B79 ("covers
two hosts or more") does not hold. No serial formula at 660 s per call fits four
or more hosts under the six-hour ceiling, so the per-host loops of the cluster
apply change; its `partial-cluster` refusals do not.

**Original design point changed from the recommendation.** The plan recommended one
enrollment call whose host documents go to cephadm in one apply, for a formula
independent of the host count. Enrollment's fixed proofs include a manager wait
bounded by a 480 s horizon inside its 640 s worker budget, which leaves 160 s:
less than six 60 s host documents plus a 60 s readback (420 s), and the time
cephadm takes per host document is not measured offline. Enrollment therefore
takes the bounded-batch branch, which adds up to n calls: at six hosts the
largest S is six (21 + S ≤ 27 calls), and P1's four services
fit. The closing correction below replaces that per-service call count with
one batch. The OSD services call keeps a host-independent command count (one readback,
one apply, one readback), because it already leaves daemon creation to the
readiness wait.

**Part 2, B475's OSD variants.** `db_devices` and `wal_devices` with explicit
paths, `encrypted` and `tpm2` now have frozen request members, host device
proofs for DB and WAL devices, and native OSD submission.
`internal/storage/ceph/osd_variants_test.go` and the native device tests cover
the in-tree path; their live P1 results remain unrun. Admission already validates the native spellings,
DB/WAL overlap with data and TPM2's need for encryption and a TPM profile. The
`tpm2` field is cephadm's own spelling: Ceph's drive group specification lists
it among its supported features
([drive_group.py](https://github.com/ceph/ceph/blob/tentacle/src/python-common/ceph/deployment/drive_group.py),
with the
[OSD service specification](https://docs.ceph.com/en/tentacle/cephadm/services/osd/#advanced-osd-service-specifications)).
P1's HDD-data/SSD-DB lane also requires the bounded libvirt HDD prerequisite:
`dataDisks[].media: hdd` realizes a known rotational SCSI presentation, and
frozen media identity is checked by original Machine custody and live host
proofs. Explicit SSD and omitted-media behavior remain stable. This prerequisite
belongs to B688 and makes no physical qualification claim.
Dynamic selectors (`all` and the filters), `osds_per_device` other than 1 and
slot or size options stay refused. Two OSD services on one host with distinct
explicit data paths need no additional variant code: admission accepts them
and each retains its own specification and attempt in the services batch.
Recommended (i), accepted by the owner on 2026-10-20 (D170): the variants stay
in this item with one added lane, m2c-osd-variants, after the enrollment lane, which moves the core request version
one step from its value at that lane's base (and the host request's if it gains
DB/WAL identities). That lane refuses, before any effect, every partial state on
a DB or WAL device instead of recovering it, because owned partial LVM recovery
is parked [B662](m2.md#b662); dm-crypt keys stay in the cluster's own store
and Bootwright never reads or publishes them. Alternative (ii): the variants stay
with B475 (X53), and P1's DB/WAL, encryption and TPM2 cells stay unrun, so
B484's exit waits on X53.

**Alternatives considered.** Concurrent rounds were chosen. Per-phase call
ceilings below 660 s were rejected: no measured per-phase worst case exists
offline, so a slow healthy host would fail `native-timeout`. Per-host enrollment
blocks were rejected: a new block type and durable plan shape close to B659's
protocol, more than the planned lanes. Raising `lifecycle.MaxDeadline` is
excluded: every adapter shares it.

**Lanes** (program names, never product strings): m2c-cluster-budget-plan, in
Go: the host bound and its refusal row, the formula and the deadline refusal
naming the cluster; it adds no request member and moves no version, and it
updates the three-node deadline pin (21540 s to 17580 s) and the premise of the
MDS deadline test, since services now move the deadline.
m2c-cluster-budget-enrollment, in collection Python: the rounds, the OSD
multi-document apply and enrollment's bounded batches. Its file list changes
from the cluster apply read-only to the per-host call loops of
apply_tunnel_cluster and execute's prerequisites loop in ceph_core.py, with the
five `partial-cluster` refusal lines and the stage checks unchanged, plus the
enroll and services phases of ceph_native.py. Part 2 adds the lane m2c-osd-variants
after it (D170). The generator re-plans the enrollment lane's files and adds that lane.

**Original amendment to D159 clause (7)**, accepted with this definition (D170): the services calls
and enrollment calls are counted, so the cluster deadline grows with the
request's mon, mgr, osd and mds services and with its hosts.

**Implementation correction:** license verification reads a variable number of
provider records and license acceptance may retry under the manager horizon.
It cannot fit a proved fixed tail of the final enrollment batch. A dedicated
license call preserves its native budgets and adds 660 s to the initial
14+n+S formula, giving 15+n+S and the limits above. Every host completion is
re-proved before this call may accept a license.

**Delivered in-tree:** the plan and enrollment lanes enforce the six-host bound
and request-derived deadline, join each host round before the next phase,
batch enrollment within the remaining native worker budget and submit each OSD
service's children together. Host-order refusals, enrollment interruption and
replay, no-progress budget refusal and service call counts have offline tests.
The five partial-cluster fences and native per-call budgets remain in place.
The explicit-device OSD variant path is implemented too; this is not live
timing, device or unlock evidence.

**Closing implementation correction (2026-10-23):** physical P1 needs twelve
host-specific OSD specifications, and physical P2 needs six; their unique WWN
selectors cannot share the virtual profiles' cross-host drive groups. Calling
the services worker once per specification exceeded the six-hour block budget.
The worker's existing bounded batch validates each specification and original
attempt, submits pending documents together and proves readback. The cluster
adapter now invokes that batch once, and its deadline counts that one call.
This changes neither the frozen request nor its version; the embedded
automation changes. Six-host and native command-count regressions cover the
expanded physical shape, interruption and replay. Operator timing and every
physical acceptance row remain unrun.

The linked P1 [six-host pools fixture](../../examples/ceph-clusters/libvirt-6-nodes-ceph-pools/README.md)
plans five MONs, two MGRs and twelve OSDs, including HDD data with SSD DB,
encryption and a TPM2 variant. Its declared budget, production capability plan
and runbook structure are tested. The runbook records the fail-closed partial
DB/WAL boundary and leaves all operator timing, I/O, media, unlock and failure
acceptance unrun.

### D171

**owner** (2026-10-20, the owner): the owner confirmed from the entitlement
registry the coordinates of the Ceph image the examples author, and the
auxiliary images of that build. (1) The tag `v9.9.2-26729` that
[D165](#d165) recorded does not exist: a pull gives "manifest unknown".
The build tag that exists is `v9.9.2-26947`, `cp.icr.io/cp/ibm-ceph/ceph-9-rhel10`
with ceph-common and cephadm 20.2.2-349.el10cp, manifest
`sha256:6cb6f09bae7b1da7e7d1e9a594dec7d90b76829af94683f11ac5cc37b402f5ca`;
the floating alias `v9.9.2` also exists. The examples now author
`v9.9.2-26947` in place of both the nonexistent build tag and the alias.
(2) The auxiliary images of `v9.9.2-26947`: applying OD28 (a) of
[D164](#d164) (1), the owner chose "I'll pin digests". Each example that
declares one of these families authors the image as `ceph.images.<name>`, with
`base` set to the listed repository and `version` set to the digest. The
defaults were read from the image with `ceph.cephadm.images.DefaultImages` and
the digests resolved by the owner with `skopeo inspect`:

| cephadm name | base | version (digest) |
| --- | --- | --- |
| haproxy | cp.icr.io/cp/ibm-ceph/haproxy-rhel10 | sha256:59d020dd7a02ad25248ae3766914f5bc5a241de77c2257fcad7124c13eea82f1 |
| keepalived | cp.icr.io/cp/ibm-ceph/keepalived-rhel10 | sha256:4ade24fea1bf11cc0c2e88af631597f21bc586562da3001ea15b8d10e0860694 |
| prometheus | cp.icr.io/cp/ibm-ceph/prometheus | sha256:f4d613a79bebcb4deeb0b5f9b1f231ba8046572978207abe242bc7c9cf589789 |
| alertmanager | cp.icr.io/cp/ibm-ceph/prometheus-alertmanager | sha256:7331dd1e3aabe689ae45470e560472dc39aeeea611c3c479e266bae9140243ed |
| node_exporter | cp.icr.io/cp/ibm-ceph/prometheus-node-exporter | sha256:4d4e3433d212042b07534c042d512bf1f8c65a43823b20e6f1b05da5cd88f0ba |
| grafana | cp.icr.io/cp/ibm-ceph/grafana-rhel10 | sha256:12006a7f211b2bf24893e39f7e5ea208b8d641d1dd9c9b562a0c502663484141 |
| loki | cp.icr.io/cp/ibm-ceph/logging-loki-rhel8 | sha256:e7e4c7c372a02aa2fbab9a0f3af48475f0df860164551e8562565983576dc61c |
| alloy | cp.icr.io/cp/ibm-ceph/alloy-rhel10 | sha256:4f645e6ee02545ceb29cd01fb42e2d9de03e1e48e509281898a325cc72d14b85 |
| nvmeof | cp.icr.io/cp/ibm-ceph/nvmeof-rhel10 | sha256:4727ed1dd465b8d0064d8da6064b32f39068fca558fea9a78c3d65f41923b226 |
| snmp_gateway | cp.icr.io/cp/ibm-ceph/snmp-notifier-rhel10 | sha256:ecf89943abe681278df906508ef1ff011661654170488be27e2ab3eeaaa339ba |
| samba | cp.icr.io/cp/ibm-ceph/samba-server-rhel10 | sha256:3b8f9164c478d267427e51e0b387c9e32ae3566e3a2078471e16d44ac6807cf1 |
| samba_metrics | cp.icr.io/cp/ibm-ceph/samba-metrics-rhel10 | sha256:090f5d43981a51923b593da36b9010d43d67264691a625ff2b92005676574f29 |
| nginx | cp.icr.io/cp/ibm-ceph/nginx-126-rhel10 | sha256:c7ba454e04aab177e3d2e283e679412dc90477d77848715ebabd3db9af4b45af |
| oauth2_proxy | cp.icr.io/cp/ibm-ceph/oauth2-proxy-rhel10 | sha256:bdeafeb29fdd9d46a29b148a99d9df200aa7cd1867037911ffcfc1b270f9a2a9 |

promtail is not used: its default lives on registry.redhat.io, outside the
entitlement registry, so the examples use alloy. The decision changes no
product spec and no delivery state; a lane that authors an auxiliary image in
an example uses this table.

### D172

**session-pending** (2026-10-20, the owner's requests; the clauses after the owner's
words are taken by the session, pending the owner's review): the owner asked
"let's start planning m3 / think of basic requisite and structure the
high-level design / we will divide m3 in submilestones: / m3a: requisite
details and spec update, so implementation will be straigthforward / m3b:
support to provision a sno over libvirt+sushy / m3c: support to provision a
3-node ocp over libvirt+sushy / m3d: support to any size ocp cluster over
libvirt+sushy and real bmc+bare metal, with advanced networking as NAD and
CUDN/UDN for providing external network for internal containers/vms, and
usage of metallb (to be installed and configured as add-on, after initial
implementation of M4 / m3e: support to any size ocp cluster over openshift
virtualization / m3f: support to any size ocp cluster over openshift
virtualization / detail the high-level spec and implement m3a, so i could use
other AIs to implement the following m3 submilestones", then "create a new
branch call m3 to save the work from this session, and later we will
incorporate to main", then "reference branch already implemented those
features in the past and have a lot of lession / i don't want you to blindly
copy things from there, but you could be inspired and take some notes of the
problems that have been overcome", and then "optimize token consumption by
dividing tasks and using best fitting model/effort for any of them". The
[M3 page](m3.md#submilestones) owns the submilestones' mapping and gates and
the [coverage page](../openshift-coverage.md) the provider matrix.
This expressly permits the suffixed submilestone IDs M3a to M3f while
retaining M3's single status and all B/X/D identifiers, as D131 did for M2.
Taken by the session, pending the owner's review: (1) m3f repeats m3e's
words; the session reads M3f as OpenShift over VMware vSphere, which mirrors
M1f and is the one product-scope substrate no other submilestone covers
([Q1](m3.md#m3a-open-decisions)). (2) M3a is definition only, delivered as
enabling slice X110 whose one item, [B473](m3.md#b473), is redefined as M3's
coverage and contract definition and completes when the owner accepts the
baseline, as D134 accepted M2a; it grants no implementation authority. B473's
"exact supported releases" gives way to recorded evidence under D157, D162
and D163. (3) [B88](m3.md#b88) moves from M5 to M3e; M5 is retired, its page a
stub holding no item, its Status row kept `not started`, and its number never
reused. M4 requires M2 and M3c instead of M3, so M3d consumes M4's B83 and B84
after their first delivery, and B83 requires [B64](delivered.md#b64). (4) B62, B63,
B65 and the parked B397 retire into named items; the parked B411, B462, B622
and B680 move into M3b; B64, B66, B67, B69, B71, B195, B250, B251, B259, B260
and B314 are redefined, B71 requiring only B61; B87 and B99 gain the
cluster-facing requirements of M3e and M3f. B688 to B699 and X105 to X109 are
left unused so this session cannot collide with M2c's concurrent allocations;
the numbers carry no meaning, and integration re-checks main for B688 or
later and X105 or later and renumbers the unlanded items on a collision. The
integration on 2026-10-20 found M2c had taken B688 to B690 and no collision.
New
items B700 to B748: shared
foundation items B700 to B704 sit on M1's page under M1c and M1g, scheduled
with their first M3 consumer (D158); B727 and B735 sit on M4's page; B736 to
B748 are parked. (5) The words "so i could use other AIs to implement the
following m3 submilestones" are read as waiving M2 for the implementation of
M3b to M3d's slices (X111 to X127) only, excepting those slices from the
two-active-slice WIP limit beside M2c's (D159 clause 2), and letting M3c's
and M3d's in-tree slices start on the preceding submilestone's in-tree
slices; promotion, every operator gate and M3 completion keep their Requires
([Q2](m3.md#m3a-open-decisions)). The waiver takes effect only for a slice
the owner authorizes. (6) Native facts are evidence, never rules: the agent
installer's 48-hour authentication token belongs to the day-2
`oc adm node-image` flow and an install image's tokens do not expire, so no
image-age rule is added ([Q6](m3.md#m3a-open-decisions)); the agent installer
in the 4.20 to 4.22 branches does not forward
`platform.baremetal.loadBalancer` (its development branch does), so today's
user-managed rendering is a pre-existing defect recorded as
[B707](delivered.md#b707), and modelling an external load balancer only as `none` is
Bootwright's choice. (7) The
reference branch is read for lessons only: a native fact it observed enters
knowledge marked "observed by the v1 implementation (main-v1); re-verify on
this tree" unless research confirmed it, and none of its architecture becomes
a requirement. (8) The token instruction governs how the session divides and
routes its work; it changes no product spec. (9) The definition lands as one
squashed docs commit on the local branch `m3`, created from main `9d2d8cf5c`
and never pushed; incorporating `m3` into main waits for the owner, and D168's
merge on checkpoint applies to M3's slices only once `m3` is on main. (10)
Each slice that changes a frozen request moves its version exactly one step
from its value on the branch tip, the second of two slices holding one
version rebases it, no reader of an older version is added, and a context an
earlier build applied is destroyed with that build first. No implementation
slice is activated and no host operation is authorized.

### D173

**session-pending** (2026-10-20, the owner's request; the clauses after the owner's
words are taken by the session, pending the owner's review): the owner asked
"let's start planning m4 - addons / think of basic requisites and structure
the high-level design / we will divide m4 in submilestones: / m4a: requisite
details and spec update, so implementation will be straigthforward / m4b:
code skelleton to handle add-ons fully / m4c: network addons / m4d: ibm data
foundation add-ons / m4e: acm/gitops addon / m4f: Gitlab addon / detail the
high-level spec and implement m4a, so i could use other AIs to implement the
following m4 submilestones / create a new branch call m4 to save the work
from this session, and later we will incorporate to main / reference branch
already implemented some of those features in the past and have a lot of
learned lessions / i don't want you to blindly copy things from there, but
you could be inspired and take some notes of the problems that have been
overcome / optimize token usage but dividing work in tasks that will be
performed by the best fitting model/effort". The
[M4 page](m4.md#submilestones) owns the submilestones' mapping and gates,
the [add-on lifecycle](../add-ons.md) the shared contract, and the
[coverage page](../add-on-coverage.md) the package matrix. This expressly
permits the suffixed submilestone IDs M4a to M4f while retaining M4's single
status and all B/X/D identifiers, as D131 did for M2 and D172 for M3; M4's
title becomes "Cluster add-ons". Taken by the session, pending the owner's
review: (1) M4a is definition only, delivered as enabling slice X135 whose
one item, [B749](delivered.md#b749), is M4's coverage and contract definition and
completes when the owner accepts the baseline, as D134 accepted M2a; it
grants no implementation authority. (2) "code skelleton to handle add-ons
fully" is read as the one built-in lifecycle every M4c to M4f package uses:
packages compiled into the executable and selected under a binding's
`spec.packages` with typed configuration ([Q26](m4.md#m4a-open-decisions)),
one cluster object program and adapter over `oc`, create-only with
operation-specific ownership ([Q27](m4.md#m4a-open-decisions)), Manual OLM
approval of a declared `startingCSV` ([Q28](m4.md#m4a-open-decisions)), one
instance block per package and cluster retired with its cluster
([Q32](m4.md#m4a-open-decisions)), owned cluster Secrets and the package
listing; a built-in package is therefore reviewed Go code rather than
declarative data, and the [project's extension model](../project.md#implementation-and-extension-model)
and the [custom-code rule](../security.md#custom-code) are amended to say so,
authored packages staying declarative data refused until B90
([Q26](m4.md#m4a-open-decisions)); the program carries from its first delivery every approval policy
and Secret placement source M4c to M4f use, so those submilestones move no
existing contract version (M4d's B86 adds the new `ceph-export` identities).
The Kubernetes NMState Operator, [B735](m4.md#b735),
moves from M4c into M4b as its reference package
([Q29](m4.md#m4a-open-decisions)). Authored (custom) packages stay
[B90](m4.md#b90), scheduled after M4f ([Q31](m4.md#m4a-open-decisions));
online removal, add-on preflight, OLM v1, the global pull-secret merge and a
least-privilege cluster identity are parked. (3) M6 folds into M4 because
the owner placed ACM and GitOps in M4e and GitLab in M4f: [B91](m4.md#b91)
and [B92](m4.md#b92), "acm/gitops" read as Red Hat Advanced Cluster
Management and Red Hat OpenShift GitOps, move to M4e, [B93](m4.md#b93) to
M4f and B90 to M4, each with a dated alias; B89 retires, folded into
[B752](delivered.md#b752) and [B754](delivered.md#b754), with `add-ons add` and
`add-ons delete` removed ([Q30](m4.md#m4a-open-decisions)); M6 is retired as
M5 was, its page a stub holding no item, its Status row kept
`not started`, and its number never reused; M7 requires M4 instead of M6.
(4) "network addons" is read as MetalLB with its layer 2 pools
([Q36](m4.md#m4a-open-decisions)); X134 keeps its ID as M4c's
network-package slice, which B83 and B735 leave for X138, and M3d's
cluster-network block narrows its dependency on B83 to the program and
adapter it consumes ([B750](delivered.md#b750), [B751](delivered.md#b751)). (5) "ibm data
foundation add-ons" is read as one package, `fdf`, installing the operator
package `odf-operator` from IBM's catalog or, expected but unverified until
[B761](m4.md#b761), the same package from Red Hat's, as the binding names
([Q38](m4.md#m4a-open-decisions)), attached in external mode to the Ceph
cluster M2 manages; the Ceph side stays Storage's
([B86](m4.md#b86), [B762](delivered.md#b762), [B763](delivered.md#b763)), and the IBM
entitlement travels in the cluster's install pull secret
([Q33](m4.md#m4a-open-decisions)). (6) The words "so i could use other AIs
to implement the following m4 submilestones" are read, as D172 (5) read
them, as waiving M2 and M3c for the implementation of M4b to M4f's slices
only, each slice keeping the M2 and M3 slices its row names, excepting those
slices from the two-active-slice WIP limit beside M2c's and M3's, and
letting M4c to M4f's in-tree slices start once the M4b in-tree slices they
name have landed; promotion, every operator gate and M4 completion keep
their Requires ([Q59](m4.md#m4a-open-decisions)), and
[Q25](m3.md#m3a-open-decisions) keeps M3d's scope. The waiver takes effect
only for a slice the owner authorizes. (7) Each submilestone exits on an
emulated rehearsal recorded as an owner-accepted ledger row; D17's "M4"
names the milestone that D128 renumbered to M2, so no M4 row on real
hardware is required, and no package needs one
([Q61](m4.md#m4a-open-decisions)). (8) Native facts are
evidence, never rules: the operator declares every channel, CSV and catalog
image; a bundle's `olm.maxOpenShiftVersion` is recorded, never enforced; a
difference between release minors becomes a registered version
specialization only when a run shows the general mechanism failing (D157,
D162, D163). The reference branch is read for lessons only, as D172 (7)
states, and none of its architecture becomes a requirement. (9) The token
instruction governs how the session divides and routes its work; it changes
no product spec. (10) New items B749 to B795: B749 is M4a's; B750 to B768 sit
on M4's page, B762 to B764 with Owner Storage; B769 to B795 are parked. New
slices X135 to X155, and X134 is re-planned. Owner questions continue M3's
series as Q26 to Q61. The parked B365 retires, folded into
[B771](#b771). B688 to B699 and X105 to X109 stay unused (D172 (4)); the
numbers carry no meaning, and integration re-checks main, `m2c` and `m3` for
B749 or later, X135 or later and D173 or later and renumbers the unlanded
items on a collision, and re-checks `m2c` for conflicts with m2.md's B491
addition (revoking one export's principals, [Q39](m4.md#m4a-open-decisions))
and OD25 text and with Q43's change to M2c's default of no published storage
FQDN. (11) The definition lands as one squashed docs commit
on the local branch `m4`, created from branch `m3` at `310284528` (the
unmerged M3 definition) and never pushed; incorporating `m4` into main waits
for the owner and follows `m3`'s, and D168's merge on checkpoint applies to
M4's slices only once `m4` is on main. (12) Each slice that changes a frozen
request moves its version as D172 (10) states. (13) The GitOps bootstrap
object hands its cluster to GitOps once every other block that reaches that
cluster is complete, before the environment-wide handoff predicate holds,
and the [project's handoff paragraph](../project.md#lifecycle-and-handoff)
is amended to allow that per-cluster early handoff; this clause stays
pending until the owner answers [Q60](m4.md#m4a-open-decisions). No
implementation slice is activated and no host operation is authorized.

### D174

**owner** (2026-10-21, the owner's requests): the owner accepted M4a's
recommended definition baseline and requested implementation of M4b and
M4c on `m4`. The owner also authorized their missing M3 prerequisites:
B64's produced-input lending, B719's typed OVN gateway manifests and
B726's secondary-network block. D173's implementation waiver applies;
operator rehearsals and their acceptance remain separate gates. No M4d
to M4f implementation or host operation is authorized by these requests.

### D175

**owner** (2026-10-22, accepted by the owner): B496 adds optional
`Secret.spec.source.generated.issuerRef` to a generated `tlsCertificate`, naming
a generated `caBundle`. Generation signs the leaf with that authority while
retaining the signing key in private custody. Existing self-signed
generation remains available when the reference is omitted. This resolves
the API choice; implementation and rehearsal evidence remain B496's gates.

### D176

**owner** (2026-10-22, accepted by the owner): B481 closes with the
implemented field-specific refusal of iSCSI declarations, with guidance
toward NVMe-oF and no vendor support-status claim. This supplies the closing
decision left open under D161, OD27; it authorizes no iSCSI lifecycle.

### D177

**owner** (2026-10-22, accepted by the owner): B70 uses opt-in
`Machine.spec.hardware.eraseOnRemoval` with `method: logicalOverwrite` and
explicit targets containing `deviceName`, `wwn`, `serialNumber` and
`sizeBytes`. Omission retains the installed system. An offline RAM worker
must prove the machine and every target, overwrite and read back all
addressable bytes, and deliver a durable receipt before shutdown. This
contract claims neither remapped-sector coverage nor hardware sanitization;
implementation and hardware qualification remain required.

### D178

**owner** (2026-10-23, the owner): tools used only by a runbook may be
installed by that runbook and removed at its end. Bootwright installs and
manages only packages its own execution needs. This narrows B509's
2026-10-12 rule: the lab-rhel diagnostic tools remain in its independent
checks, with operator installation and cleanup preserving pre-existing
packages, rather than becoming controller prerequisites. B509 closes when
that procedure is documented and verified; no host operation is performed
by recording this decision.

### D179

**owner** (2026-10-23, the owner): keep the implemented guest SSH forwarding
restrictions under D135 and complete their qualification. The policy is
additional hygiene for the root-equivalent cluster account; pinned host-key
verification, fixed loopback forwarding and no direct fallback remain the
transport boundary. This answers the 2026-10-11 review's Q1 without removing
policy management, reloads or the include-closure proof. Live qualification
remains a separate gate.

### D180

**owner** (2026-10-23, the owner's request): implement every independent
blocker of M4a to M4e, including their specified code prerequisites, while
leaving operator inputs, live rehearsals and owner acceptance open. B195
follows Q13(a): an authored-only typed additional trust policy, without a
default, required by M4a's accepted Data Foundation trust contract. This
authorizes no host operation and claims no passed operator gate.

### D181

**owner** (2026-10-23, the owner's request): "finish implementing all items
that are blocking M3a-e to be declared done that doesn't requires anything
from me". Implement the independent specified code, tests and acceptance
preparation, including shared prerequisites, while leaving owner decisions,
baseline acceptance, live environments, captured native responses and
operator-run acceptance open. This records no host operation or passed live
gate. The resulting in-tree work is recorded under
[M3 independent completion](delivered.md#m3-independent-in-tree-completion).

### D182

**owner** (2026-10-26, the owner's request): "order all lanes from most
basic, fundamentals, architectural and low level one to the high level
ones, and start implementing all of them in sequence. One branch for each
lane that will be merged to main branch as a single commit", proceeding so
another coding agent can continue. This authorizes the
[audit remediation lanes](#lane-order-and-progress) in the recorded order,
one lane at a time, each merged to local main as one commit once its gates
pass. Parts that wait for an owner question (Q62 to Q118) stay open, no host
operation or live gate is authorized, and pushing waits for the owner.

### D183

**owner** (2026-10-26, the owner's answers to the
[audit remediation questions](#audit-remediation-decisions)): Q62 (a) a
tracked `.codex/hooks.json` runs the same advisory end-of-turn check; Q63 (b)
the WIP limit is at most two active slices per program, the slices one owner
decision or request authorizes, so the standing per-decision exceptions are
no longer needed and the one-product-or-defect-plus-one-enabling composition
clause retires with them (composition clause retired; orchestrator's reading);
Q64 (a) U03 stays as a native replica-safety rule; Q65 (b) an unreadable
removal observation becomes a typed inconclusive result, never no-effect;
Q66 (b) durable install-then-disk-boot intent; Q67 (a) D38 stands with the
named-keys warning; Q68 (a) retirement refuses while a consumer survives;
Q69 (a) B659, B662 and B491 become prerequisites of [B485](m2.md#b485), so
B659 and B662 move to M2; Q70 (a) a recorded access-credential rebind exit
under the `--authorize` model; Q71 (a) refuse inert endpoint fields at plan;
Q72 (a) `spec.resources` is removed and cluster lists are the only
narrowing; Q73 (a), Q74 (a), Q75 (b) validate error; Q76 (a) D145(4) stands.
API shape: Q77 one kind per cluster distribution, `ContainerCluster`
becoming `OpenShiftCluster` (OKD as its distribution variant) with a
cluster-family registry so a new family is additive; Q78 (a) the storage
children become `Ceph*` and `StorageCluster` becomes `CephCluster`, revisiting
D133's naming; Q79 (b) a narrow per-substrate Machine arm; Q80 (a) the
capability role tokens are dropped and prerequisites derived in one change;
Q81 one testable union rule with its architecture test, the service
implementation staying an enum; Q82 (b); Q83 (b); Q84 (a); Q85 (a); Q86 and
Q114 (b) block identities hash an overflowing segment, so one public name
rule serves every kind; Q87 (a) the renames land in one batched slice per
surface with retired-path refusals; Q88 (a); Q105 (a) lowerCamel enum values,
native spellings exempt; Q106 (a) the stage token is renamed; Q107 (b) the
export kind and `fdf.storageExportRef` are renamed in the `Ceph*` family with
their shape kept; Q110 (a and b) `render effective --origins` and an
advisory JSON Schema export; Q111 (a); Q112 (a); Q113 (a); Q115 (c) both
reserved shapes keep validating. Recovery and trust: Q92 (a) D38 stands; Q93
(b) a destroy over a completed apply covers its whole frozen plan; Q94 (a);
Q95 (a); Q96 (a) B247 widens to SSH-placed keys; Q97 (a) `--expect`
fingerprints; Q98 (a); Q99 (a) `--from-url-file`; Q100 (a) the object-user
reveal is authorized; Q101 (a); Q116 (a) `--object-user <name>` selects one
user's keys and `--secrets` alone reveals only the dashboard password; Q117
(a) `--authorize data-loss=<digest>` under `--yes`. Corpus: Q89 (a); Q90 (a);
Q91 (a) pending clauses bind until overturned; Q102 (a); Q103 (a) a blocking
pre-existing defect may be fixed in its own commit once recorded as an item;
Q104 (b) the name stays; Q108 (a) and Q109 (a) status readiness hints and
`diff --exit-code`. Q118: authored add-on ordering and an apply that stops
after a named add-on become [B837](delivered.md#x171--audit-lane-i-api-shape-and-grammar) in lane I. Each answer authorizes
only the item that applies it, within D182.

### D184

**owner** (2026-10-26, the owner's answer about [B836](delivered.md#x164--audit-lane-e-store-durability-and-controller-records)): a
root-confirmed `bootwright controller rebind` moves every context to a
controller host whose machine-id, DMI product UUID or root filesystem UUID
changed. It confirms interactively, naming the recorded and the new
identity, or under `--yes` only with the new identity named; it proves the
same store under the exclusive root lock, rewrites the recorded host
identity once, and keeps every binding and reservation. This adds a public
command under D153 clause 2.

### D185

**owner** (2026-10-26, the owner's confirmation of [B814](delivered.md#x171--audit-lane-i-api-shape-and-grammar)'s names):
`StorageExport` becomes `CephConsumer` (`fdf.cephConsumerRef`, block prefix
`ceph-consumer-`); `CephCluster` settings flatten from `spec.ceph.*` to
`spec.*`; the node role `master` becomes `controlPlane`, still rendered as
`master` for the installer; the domain spec pages are renamed with the
kinds (OpenShift and Ceph pages, coverage pages included). The rest of
B814's rename table stands as defined.

### D186

**owner** (2026-10-27, the owner's answer about Q96's qualifier): the
private Controller record stays version 3. Its reservation keys may carry
the `ssh/<address>:<port>/` qualifier as an additive key form, so a record an
earlier build wrote stays current and no controller needs a fresh install;
each context publishes its SSH-qualified claims on its next apply. Builds
before X170 read a qualified key as opaque, keep other contexts' entries and
find no fewer conflicts than before; such a build's apply of a context drops
that context's qualified entries until a later build applies it again.

### D187

**owner** (2026-10-27, the owner's answers during lane L's review): D174
accepted M4a's recommended definition baseline for implementation and
answered no open question, so [Q26 to Q61](m4.md#m4a-open-decisions) stay
open and D173's clauses stay pending. A decision a session took under the
owner's explicit instruction to proceed carries the status token `delegated`:
it binds and is not in the review queue; D23 to D26 and D162 carry it.

### D188

**owner** (2026-10-27, the owner's answers while planning the rest of the
audit remediation): D154 accepted D152 as written, clause 13 included; D20 is
superseded by D152's clauses 3 and 4, and D21 and D22 are `delegated`. The
replica-safety refusal (a pool of size N needs N+1 hosts) applies to every
Ceph distribution ([B838](delivered.md#x175--audit-lane-q3-ceph-object-and-replica-refusals)). A broader host route through a gateway that
covers a libvirt managed prefix is admitted with a warning naming the route
and the in-prefix hosts it shadows; identical, nested-narrower and on-link
broader routes refuse ([B843](delivered.md#x178--audit-lane-s-libvirt-host-evidence-routes-and-root-disk-identity)). SSH-placed claims are also keyed by the
host's pinned SSH host-key fingerprint (`sshkey/<SHA256>/`), an additive key
form under controller record version 3 ([B844](delivered.md#x180--audit-lane-p-removal-views-releasing-claims-and-host-key-claims)). `schema` and `version`
drop `--context`, and structured help gains no `examples` or `available`
([B852](delivered.md#x177--audit-lane-t-inherited-flags-and-cluster-preflight), [B820](delivered.md#x182--audit-lane-n0-remaining-parts-to-their-owners-and-bmc-wording)). `installer.templateClone` folds into
[B99](m1.md#b99); `packageSource.mirror`, `packageSource.fromSubscription`,
`ssh.initialPassword` and ArtifactServer retention `installOnly` retire with
their replacements (B853 to B856). `preflight clusters` runs the container-cluster and
storage-cluster checks together, and `preflight infra` and `preflight all`
park in one definition item ([B825](delivered.md#x177--audit-lane-t-inherited-flags-and-cluster-preflight)). The lifecycle runner moves only
its scratch under root-owned `/var/lib/bootwright-staging/runs` on the
controller and SSH hosts, and the job directory and bound Secret files stay
under `/run` ([B389](delivered.md#x176--audit-lane-o-store-bounds-and-run-scratch)).

### D189

**owner** (2026-10-28, the owner's answers while implementing the follow-up
items): `preflight infra` runs the controller check and the
infrastructure-services checks, `preflight all` runs controller,
infrastructure and clusters as one report, and `render` with a path flag
writes its output there ([B858](delivered.md#x191--audit-lane-w1-preflight-infra-and-all-render-to-a-path)). An adapter reports warnings in an
optional `warnings` member of its lifecycle result, carried into the run's
diagnostics with registered codes ([B864](delivered.md#x187--audit-lane-w2-adapter-warnings)). A placement whose pinned
host key is one of the controller's own sshd host keys, read from `/etc/ssh`,
must use an address the controller Machine declares ([B868](delivered.md#x188--audit-lane-v3-known-hosts-entries-claim-kinds-and-the-controller-host-key)). No
operation registered by an earlier build is live, so the one-entry
known-hosts rule holds for every run with no compatibility path.

### D190

**owner** (2026-10-28, the owner's request and answers about updates within
the declared release). The request: "when bootwright provision a rhel
machine, does it update the package for that target rhel version? for
instance, if user declare it want a machine with rhel 9.8, bootwright must
install that OS version with the media/iso/qcow provided that must run a
update command with distro package manage to update all package version, but
keeping machine in the specificed version (9.8) ... handle all variation
scenarios, with subscriptions, all kind of image medias, internal satellite,
etc...". After the session's plan the owner answered "Proceed with this
session's recommendations", accepting: (1) updates are on by default
(`spec.updates.policy: withinRelease`) and refuse when the profile declares no
update source; (2) `spec.os.version` is always an exact `<major>.<minor>`,
never major-only; (3) `withinRelease` beside TPM2 disk-encryption `pcrIds`
holding 4, 8 or 9 refuses at admission. This authorizes
[B876](m1.md#b876) as slice X189 on M1 alone, out of sequence; the contract
is [update within release](../managed-os.md#update-within-release). It closes
the pre-existing defect that no code reads `spec.os.version`. Satellite
registration is not authorized and is parked as [B877](m1.md#b877). The operator
chooses repository IDs and update streams; no rule pairs a minor with a
stream ([D163](#d163)).

### D191

**owner** (2026-10-28, the owner's request about BMC firmware settings). The
request: "Does it already support communicating with bmc to setup necessary
flags in machine before initiating installation in fips mode? That question
can be expanded for any rhel machine too", then, after the session's plan, "Ok.
Let's implement all recommendations in this session." It accepts: a
bare-metal Machine declares Secure Boot, a UEFI-only boot mode and verbatim
BIOS attributes in `spec.substrate.baremetal.firmware`; the managed-OS and
cluster installations apply or prove them in the bare-metal pre-boot step
before media insert, under their `data-loss`, set-only and never reverted. The
session's refinements under that instruction are `delegated`: `mode: prove`
replaces the planned `proveOnly` boolean, and the block is never a kind
default. This authorizes [B880](m1.md#b880) as slice X190 on M1 alone, out of
sequence; the contract is [physical firmware](../substrates.md#physical-firmware).
The operator chooses attribute names and values; no rule pairs them with a
vendor or firmware version ([D163](#d163)).

### D192

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The request:
"Plan and implement the support for operator declare his internal satellite
instance and how bootwright will use it to configure machines with rhel SO".
It authorizes [B877](m1.md#b877) as slice X194 on M1, out of sequence, and is
the dated waiver of B877's Requires on [B876](m1.md#b876), delivered in-tree
and awaiting operator acceptance, for implementation only. The owner then
asked how the feature affects repository configuration and package updates
after provisioning under the declared release, which clauses (6) and (7)
answer. Taken by the session, pending the owner's review: (1) the API reuses
`Entitlement.spec.rhsm.satellite`; `hostname` names a Satellite or a Capsule;
on a `redhatRhel` Entitlement `trustBundleRef` is required whenever
`satellite` is present, and `contentBaseURL`, the root rhsm appends every
repository path to, is HTTPS with no query, fragment or userinfo
([RHSM](../api/secrets.md#rhsm)); the `redhatCeph` Entitlement is unchanged.
(2) The plan admits a Satellite; Insights and a proxy stay refused, the route
being direct to the hosted service or the declared Satellite. (3) The frozen
registration carries an optional `satellite` `{hostname, contentBaseURL,
trustBundleRef}`, omitted for the hosted service, which moves the request
version. (4) The CA bundle is lent like the organization and activation key
and is validated before the adapter runs: 1 to 32 X.509 `CERTIFICATE` PEM
blocks, nothing else, at most 64 KiB. (5) The registration worker writes the
bundle to `/etc/rhsm/ca/bootwright-satellite.pem` and runs `subscription-manager
config` naming the server, the content root and that CA on every apply before
registering over the unchanged D-Bus call; it never installs
`katello-ca-consumer`, fetches nothing over HTTP and never disables TLS
verification; a certificate the bundle does not verify refuses
`registration-trust`. (6) With a Satellite, a release the server does not
offer is unset rather than refused, so an activation key's own release cannot
outlive the profile's, and the content view is the pin; the Kickstart writes
no `/etc/dnf/vars/releasever`; the evidence records the pin in `update.pin`
(`release`, `contentView` or `variable`). (7) Repositories and updates after
provisioning keep the pin in place for every later dnf run, as
[update within release](../managed-os.md#update-within-release) states; destroy
unregisters the consumer and leaves the Satellite's own host-record setting
alone ([registration](../managed-os.md#registration)).

### D193

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). After describing
a target estate, kept out of this public repository at the owner's
instruction, the owner asked: "check what is already implemented and what is
missing for realize this desired environment ... plan the implementation of
is missing then implement it". It authorizes [B883](m4.md#b883) as slice X195
on M4, out of sequence, and records the review's other findings. Taken by the
session, pending the owner's review: (1) [B732](m3.md#b732)'s question 4 is
answered by the built-in OLM package `kubevirt`, providing `kubevirt`, rather
than an authored package behind [B90](m4.md#b90). (2) The package's
HyperConverged carries no spec field: persistent vTPM backend storage stays
with [B87](m1.md#b87)'s question 7 and the KubeVirt provider namespace with
B732's question 3. (3) Its operator gate is the nested host of B732's run, so
it adds no acceptance profile. (4) The review's other findings are recorded,
not implemented: [B884](delivered.md#x227--add-on-discovery-and-operator-journey), [B885](m3.md#b885), [B886](m4.md#b886),
[B887](m2.md#b887) and [B888](m2.md#b888). (5) The existing items the review
found blocking keep
their definitions, and this request closes none of their questions:
[B79](m2.md#b79) for more than six Ceph hosts, [B486](m2.md#b486) and
[B487](m2.md#b487) for stretch mode, [B87](m1.md#b87), [B88](m3.md#b88),
[B731](m3.md#b731) and B732 for KubeVirt nodes on a host the context builds,
[B740](#b740) for a host address on a localnet bridge and [B785](#b785) for
the ACM cluster-backup component.

### D194

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). Answering D193's
review, the owner stated that each of their networks is one VLAN, that a
second VLAN per network "would break my environment setup (outside bootwright
control)", and asked: "plan the implementation what is missing support and
then implement ... the solution must be as good as possible in terms of
well-designed architecture and ux". It authorizes [B889](m3.md#b889) and
[B885](m3.md#b885) as slice X196 on M3, out of sequence. Taken by the
session, pending the owner's review: (1) a node address on a localnet's VLAN is
declared, never inferred: `uplink.hostAddresses: bridge`, default `none`, and
an addressed uplink without it keeps refusing, its remedy naming `bridge`. (2)
The address stays a Machine fact on the uplink interface: the installer
configures it there, because the agent installer refuses a bridge, and the
cluster network block moves it onto the bridge; no address kind is added to
the cluster. (3) The bridge's interface takes the bridge's name, the uplink's
declared MTU and `copy-mac-from` the uplink, OpenShift's documented `br-ex`
shape. (4) DHCP or autoconf on such an uplink, and a route naming it as its
next-hop interface, refuse; routes are never rewritten. (5) A MetalLB pool
names the bridge, and naming its uplink refuses with the bridge in the remedy.
(6) [B885](m3.md#b885)'s uplink rule covers every interface built on the
uplink, a VLAN or a bond, not only the install interface. (7) This revises
D193 (4) for B885, now implemented, and takes [B740](#b740)'s
post-installation half; its install-time bridges stay parked.

### D195

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "plan the implementation what is missing support and then
implement" and said "the solution must be as good as possible in terms of
well-designed architecture and ux". It splits [B890](m2.md#b890) from
[B79](m2.md#b79), as [D170](#d170) split [B688](m2.md#b688), and authorizes it
as slice X197 on M2, out of sequence. Taken by the session, pending the
owner's review: (1) The fixed six-host count is removed: the
[cluster execution budget](../ceph.md#cluster-execution-budget)'s apply
deadline, (16 + n) × 660 s + 3720 s against the 21600 s run ceiling, is the
only host bound. It admits eleven hosts (21540 s) and refuses twelve
(22200 s); the build derives the count from the formula's constants, so the
count is never restated. A raised fixed count (seven) was not taken, since it
would invent a ceiling the deadline does not need, nor restructured or
multi-run cluster blocks, which stay B79's. (2) One refusal: the refusal
table's row names the host count, the deadline and the largest admitted count,
before operation registration and secret custody for the CephCluster and its
policies, pools and clients, and request planning and every block deadline
refuse with the same text; it replaces both the six-host row and the separate
deadline refusal. (3) Every other deadline or size that grows with the host
count was checked at eleven hosts and none is smaller than this bound:
observation (n + 3 calls, 9300 s), destruction (n + 2, 8640 s), a block other
than the cluster (2n + 3, 16560 s), fixed host and service-unit deadlines,
enrollment (each call admits at least one host within its worker budget, so n
calls), the core-specification batch (35 of the worker's 128 documents in
P1's physical shape with three disks per host), the frozen request (about
22 KiB of 256 KiB) and the ready cluster evidence (about 24 KiB of 60 KiB). The review found the fixed 600 s ingress and
single sign-on proof windows of a service unit serial over its placed hosts,
so they can end a run `not-ready` from six placed hosts; that is recorded as
[B891](m2.md#b891), not taken here.
The physical retirement policy's decoding bound, six before, follows the
cluster bound. (4) A CephFilesystem whose data pools and subvolume groups put
its own apply past the ceiling, which at eleven hosts is more than six pools,
or more than four pools and groups together, refuses at planning by its own
name and fields instead of the cluster's text. (5) Evidence: in-tree planning
goldens and the recording fake transport at seven hosts and at the bound; a
live seven-guest libvirt run is the operator gate. (6) The 900 s readiness
waits are unchanged; a shortfall the live run measures is its own item.

### D196

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "Plan and implement what is still missing" and said "the
solution must be as good as possible in terms of well-designed architecture
and ux". It splits [B893](m2.md#b893), the stretch topology inputs, from
[B486](m2.md#b486), and authorizes B893 with [B887](m2.md#b887) as slice X198
and B486, activation and readiness, as X199 on M2, out of sequence, and is the dated waiver of B486's Requires on B485, B316 and B472,
for implementation only. Taken by the session, pending the owner's review:
(1) One reference stretch profile is admitted ahead of the full M2c/M2d
matrix (two data sites of three OSD hosts, a MON-only tiebreaker at a third
site, `4/2` on a `datacenter` stretch rule); its exit evidence is B488's
subset for that profile, and the matrix rows stay open. (2) Activation is
declarative where native specs exist (host `location` at enrollment, MON
`crush_locations` derived from the topology), and imperative only for
`election_strategy connectivity`, the stretch rule and `enable_stretch_mode`.
(3) Those run as stages `located`, `rule-present`, `election-connectivity`,
`stretch-enabled` and `verified`, each with an attempt and a completion record
and read back before the next; an ambiguous stage is observed, never
repeated; activation precedes every pool. (4) The cluster owns its stretch
rule, `ruleName`, in IBM's single-`take` form; every pool uses it at `4/2`; a
CephPlacementPolicy of a stretch cluster, and a CephPool
`placementPolicyRef` naming one, refuse at validation and at plan with a
remedy saying the stretch rule applies. (5) `failureDomain` is any native
default CRUSH bucket type above `host`; a host `location` names only that
key, equal to the node's site, derived when omitted; the tiebreaker has none;
outside stretch a `location` refuses at validation. (6) A provided Machine
joins a Ceph cluster only as its stretch tiebreaker: MON-only, never the
bootstrap node, with the fleet key ([D204](#d204): dialed with it, never
named by the Machine), the cluster SSH user and a
`knownHostsRef`-pinned host key, no device or erase claim and no Machine
block requirement; its OS dependency packages come from the bootstrap host's
hosted DVD; any other authored service placing on it refuses, while the
derived crash collector still runs there. (7) `publicCIDRs` may span several
CIDRs; admission proves each MON host address inside one. (8) Stretch is
frozen day-1 intent: adding or removing it refuses with whole-cluster
retirement as the remedy, and `disable_stretch_mode` is never an inverse,
which settles the IBM pages' differing exit guidance for Bootwright. (9) A
provided host proves FIPS mode by its host block reading
`/proc/sys/crypto/fips_enabled` and `/etc/crypto-policies/state/current` over
the fleet connection before host preparation and on every apply and
observation, exactly `1` and `FIPS`; the reading is the host block's `fips`
evidence, never a Machine installation proof, and the cluster block requires
it; a mismatch or unreadable file refuses `fips-not-enabled` naming the
Machine; the pinned host key must be ECDSA or RSA of at least 2048 bits;
`/proc/cmdline` is not read, since the two files are what `fips-mode-setup
--check` reads; the run-time FIPS check of installed hosts now also requires
exactly `FIPS`, as the managed-OS proof does. (10) [B316](m2.md#b316) needs
no new field for stretch: X198 adds only that every stretch node sits at a
data site or the tiebreaker's; its service-placement remainder stays with
[B487](m2.md#b487). (11) X198 admits the stretch topology at validation and
keeps planning refused (`lifecycle.unsupported`) until X199 realizes
activation. (12) Destroy of a cluster with a provided tiebreaker keeps
refusing by the surviving-host rule until [B892](m2.md#b892) removes that
host's MON daemon and cluster data.

### D197

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "Plan and implement what is still missing" and said "the
solution must be as good as possible in terms of well-designed architecture
and ux". It answers [Q18](m3.md#m3a-open-decisions) and the open questions of
[B87](m1.md#b87), promotes B87 to `Specified` and authorizes it as the planned
slice X129 on M1; B87's Requires on a qualified host is waived for
implementation only, the host qualification staying the operator gate
([B731](m3.md#b731) under X130). The owner also directed that the target
machines need FIPS, but neither a vTPM nor disk encryption, which clause (8)
follows. Taken by the session, pending the owner's review:
(1) Q18 (a): the arm speaks the host's native API, VirtualMachine and
DataVolume through the Kubernetes API and `virtctl image-upload --no-create`
through the CDI upload proxy, behind the substrate port; the Redfish facade
stays parked as [B745](#b745).
(2) Question 1: installer-provisioned Machines only; a Bootwright-installed
operating system on KubeVirt is [B905](#b905) and keeps refusing before
registration.
(3) Question 2: the VirtualMachine is `<machine>-<8 hex digits>`, the Machine
name cut to 48 bytes and the digits the head of the SHA-256 of the context and
Machine names, with DataVolumes `<vm>-root` and `<vm>-media`, each at most 63
bytes; the brief's 52-byte cut would put `<vm>-media` at 67 bytes. The labels
`bootwright.io/context` and `bootwright.io/machine` derive the name, so a port
entry point refuses a same-named object whose labels derive another. The
operation-and-block mark is the annotation `bootwright.io/owner`, because a
label value holds 63 bytes and the mark 64. The namespace is the operator's and
is never created or written.
(4) Question 3: each interface's MAC is libvirt's derivation, QEMU's locally
administered prefix `52:54:00`, pinned on the VirtualMachine; the provider
proof reads KubeMacPool's range where it may and records the pins `inside`,
`outside` or `unknown` of it, refusing nothing; the operator gate confirms an
out-of-range pin is accepted.
(5) Question 4: a read-only provider block, `substrate-host-kubevirt-v1`, in
the `substrates` stage, which every machine block of the provider requires;
it changes nothing and its removal reads nothing. Its checks are the ones
[KubeVirt machine realization](../substrates.md#kubevirt-machine-realization)
lists; a check it cannot read is unknown and fails the block, so no machine
effect follows.
(6) Question 5: the accepted kubeconfig forms are as listed: one cluster, one
user and one context, an embedded certificate authority, and a client
certificate and key or a token. The adapter refuses any other form before its
first request, in every block and port entry point, since no material is read
before registration; the security contract's "before registration" is
corrected to that. Both blocks read the kubeconfig as an access credential, so
a rotated or expired one takes the
[access-credential rebind](../state-reconciliation.md#access-credential-rebind)
(B805's remainder).
(7) Question 6: EFI with `secureBoot: false`, frozen at creation; Secure Boot
is [B907](#b907).
(8) Question 7: a vTPM is rendered only when the profile declares `tpm`, with
`persistent`, `true` by default, frozen at creation and never toggled.
Admission refuses `security.diskEncryption` on a cluster whose encrypted
KubeVirt node's profile lacks `tpm` or sets `persistent: false`. Removal proves
every claim the VirtualMachine owned absent, a persistent state claim found by
its owner reference to the VirtualMachine's UID. The state class's
migratability proof and its `vmStateStorageClass` setting are [B906](#b906).
(9) Question 8: the arm sets `evictionStrategy: LiveMigrate`, and the provider
proof refuses a root class whose StorageProfile offers no `ReadWriteMany`,
naming the class; the StorageProfile decides the volume mode, so a Block class
gives a Block root.
(10) Question 9: the conservative call bounds and boot allowance
[KubeVirt machine realization](../substrates.md#kubevirt-machine-realization)
publishes; [B731](m3.md#b731)'s phase durations correct them.
(11) Question 10: `machine stop` asks the `stop` subresource and polls until
the VirtualMachineInstance is absent within the published power bound, then
reports the machine still running; `--force` stops it with a zero grace
period. The power service lends a controller credential by the parts the arm
names, a substrate fact this slice adds, so it names no arm.
(12) Question 11: the provider proof records the server and the `kube-system`
namespace UID; every machine block compares the live UID with that apply
record and with its own, and a mismatch refuses. Each VirtualMachine claims
`kubevirt-vm:<server>/<namespace>/<name>` once its block proves it (B816's
remainder).
(13) New items: [B905](#b905), [B906](#b906), [B907](#b907),
[B908](m3.md#b908) (peer VM placement), [B909](#b909) (UserDefinedNetwork
attachments, refused by the arm until then) and [B910](m3.md#b910), the live power read's
pin; the port's own cluster and UID pin, which the slice's review found
missing, is delivered with it.

### D198

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "Plan and implement what is still missing" and said "the
solution must be as good as possible in terms of well-designed architecture
and ux". It defines [B888](m2.md#b888) as slice X201 on M2 and waives, for
implementation only, its Requires of B688's live timing and an owner
decision. Taken by the session, pending the owner's review: (1) Why three:
X64 fixed the count as an input bound for one data, DB and WAL allocation; no
budget, deadline or proof derives it, and no call, round or deadline counts
devices: every per-host round makes one call per host that proves, binds or
prepares all of its disks, and the services call takes every host's device
identities at once. The cluster deadline therefore cannot bound the device
count, and neither more calls nor a per-device term is added to it. (2) The
binding bound is retirement: a physical host's every selected data, DB and WAL
disk must be in its Machine's original `eraseOnRemoval` targets beside the
root disk, and that policy admits 16 targets, so a host selecting more than 15
could never be retired. The build derives the count as the Machine erase
target bound less one (`machine.MaxEraseTargets`, now one constant for the
Machine admission, the erase request and Ceph), so it is never restated.
(3) One disk past it refuses at request planning, coded
`lifecycle.unsupported`, naming the cluster, host and count, the bound and the
erase policy, with a remedy to select at most 15 per host; the physical
retirement policy's decoder follows the same bound and the erase-target bound.
(4) Every destructive guarantee stays as before at any count: canonical WWN
paths only, shared WWNs across hosts, partitions and multipath refused, WWN
intent frozen without serial or capacity, binding and re-proof of every disk,
erase receipts matched per disk, and the original erase policy covering each.
The worker's request admission drops its physical three-disk check and keeps
its 128-device per-host bound; Go owns the policy. (5) The services call
submits at most the worker's 128 specifications; one OSD specification per
disk passes that from nine hosts of fifteen disks, so planning past it
refuses by name with a remedy to list a host's disks in one OSD service. With
one service per host, eleven hosts of fifteen disks fit the frozen request
(about 38 KiB of 256 KiB), the ready evidence, the 16000-byte host evidence at
the longest admitted fields and the retirement policy. (6) Found and not
taken: at their per-command ceilings, the device-identity call's per-disk
commands need about 290 s a disk, the device proof about 120 to 180 s and
the device readback 60 s, and the cluster observation reads each OSD's
declared configuration keys at 60 s each, past the 640 s worker budget
already at three disks and for virtual hosts alike; a
slow host ends such a call ambiguous, its records intact for the next apply,
rather than refusing by name. That is recorded as [B900](m2.md#b900), not
taken, because it predates this bound and needs per-disk admission or batched
probes in the worker. A build from before X201 refuses a frozen retirement
policy of more than three disks per host as inconsistent, which is safe; no
version moves under the pre-1.x rule. The Machine API has no parallel three-disk limit:
libvirt's data-disk list and the 16-target erase policy are its only disk
bounds. (7) Evidence: planning and binding tests at 3, 5 and 15 disks and the
refusal at 16, retirement with five-disk hosts, the recording fake
transport's OSD-preparation round at 3 to 15 disks and eleven hosts, and the
worker's device identity at 3, 5 and 15 disks; a physical run with five or
more WWN disks per host is the operator gate.

### D199

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "Plan and implement what is still missing" and said "the
solution must be as good as possible in terms of well-designed architecture
and ux". It authorizes [B716](m3.md#b716) as slice X126 on M3, out of
sequence, waiving for its implementation only the Q11 owner decision its
Requires named; [B717](m3.md#b717) stays in X126 behind its own definition
gate (Q17). Taken by the session, pending the owner's review: (1) Q11 is
answered (a): the installation block places each node once, in the apply
attempt that first proves the installation complete, after its media is
released and before it offers the administrator access to custody. An `infra`
node carries `node-role.kubernetes.io/infra` with an empty value unless its
own labels author that key, and every node carries its authored `labels` and
`taints`. A settled apply attests its records and reaches no host (D153), and
`bootwright diff --observe` reports a removed or changed label or taint as a
partial realization, repairing nothing. (2) No taint is synthesized unless
authored: infra nodes install through the worker pool and keep its worker
label, and with compute nodes present the default IngressController's routers
run only on worker-labelled nodes without a toleration, so an all-infra
compute pool tainted `NoSchedule` would leave ingress unschedulable. Moving
routers or other workloads onto infra nodes is GitOps' work. (3) The completion
proof requires placement as evidence: the evidence records, for each node that
declares any, the observed values of its declared label keys and its taints of
a declared key, and presence holds only when they equal the frozen request's.
(4) Labels in `node-role.kubernetes.io/` are written through the API with the
administrator kubeconfig the installation keeps, never by kubelet
self-labelling, which the NodeRestriction admission plugin forbids for that
prefix and for every taint change. (5) Placement is not written by server-side
apply: an apply to a Node that disappeared between read and write would create
one, and `spec.taints` is an atomic list an apply would own whole. It is one
JSON patch per node under the field manager `bootwright-cluster-install`,
pinned by `test` operations on the node's uid, its whole label map and its
whole spec as read, that adds only the declared labels and taints the node
lacks. It never removes, takes over or overwrites another manager's label or
taint: a declared key present with another value, or a declared taint key and
effect present with another value, refuses naming the node and key, and a
node changed between read and write refuses with a repeat of the apply as the
remedy. (6) A continuation re-observes before it writes: the step reads every
node first and writes only what is absent, so a lost reply after labelling is
read back and written again never; an observation that finds the placement is
positive completion, and one that finds it missing on an otherwise complete
cluster is a partial realization the next attempt converges. (7) The
installation's observation outside an operation, as `diff --observe` runs it,
declares no administrator-access output, so the runner admits it (the
agent-install half of [B596](#b596)), and its
projection renders the observed `spec.nodes[].labels` and
`spec.nodes[].taints`. (8) The install request moves to
`cluster-install-agent-v10`, freezing each node's placement; a v9 record
refuses by name (D142). (9) The `api.deferred` warning for node labels and
taints is removed. Exit evidence: completion-evidence goldens with labels and
taints, an infra node without authored labels, a continuation after a lost
reply, an observation that `diff --observe` reports as partial without
restoring the label, a settled apply reaching no host; the operator gate is
an owner-accepted infra row of lab-anysize on X126's build.

### D200

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "Plan and implement what is still missing" and said "the
solution must be as good as possible in terms of well-designed architecture
and ux". Under [D196](#d196)'s authorization of X199 it fixes how
[B486](m2.md#b486)'s activation is realized; D196 is unchanged. Taken by the
session, pending the owner's review: (1) A data host's enrollment `location`
is `{<failureDomain>: <site>, root: default}`, after IBM's bootstrap example,
so its site bucket hangs under the root the rule takes; the authored
`location` still names only the failure domain, and the tiebreaker has none.
(2) The MON spec's `crush_locations` is derived for every MON host, the
tiebreaker's at its own site; an authored MON `spec` other than exactly
those entries refuses at plan. (3) Activation starts once the cluster is
ready in every other respect, because `enable_stretch_mode` needs every MON
located; it runs in two bootstrap-host calls, `stretch_locate` (`located`,
`rule-present`) and `stretch_enable` (`election-connectivity`,
`stretch-enabled`, `verified`), before the final readiness wait and the
dashboard, and a stretch cluster whose other recovery stages are complete
continues from activation. D196 (3)'s stages are unchanged; the brief's
separate `tiebreaker-located` stage is part of `located`. (4) `located`
corrects only the bootstrap host's bucket, absent or directly under
`default`, with `osd crush add-bucket` or `osd crush move`, and a MON
without a location with `mon set_location`, because cephadm sets no
location on a MON it already deployed; any other misplacement refuses
`stretch-location`. (5) The rule is injected by IBM's
get/decompile/edit/compile/set procedure, compiled offline in the frozen
image's `crushtool` with the next free rule ID, and set with `osd
setcrushmap -i - <crush_version>`, the version read before the map, so a
concurrent map change refuses rather than being overwritten. (6) A stage
whose attempt record has no completion is read back: its postcondition
completes it, and its absence refuses `stretch-interrupted` and never
repeats the command, even when the readback suggests the command never ran;
the forward exit is [B920](m2.md#b920). A completed stage that no longer
holds refuses `stretch-drift`. (7) A rule of the stretch rule's name, or
stretch mode, found without this operation's attempt refuses
(`stretch-rule`, `stretch-foreign`); an election strategy already
`connectivity` is recorded without a command, since it is a setting and not
an object; before the `stretch-enabled` attempt the stage also proves the
two data-site buckets and every pool replicated at the default protection,
so a refusal `enable_stretch_mode` would make leaves no attempt behind. (8) `verified` reads every pool on the stretch rule at 4/2, else
`stretch-pools`; every observation reads all pools again, so a service-created
pool off the rule keeps the cluster partial until [B487](m2.md#b487)
qualifies that service. A CephPool and a gateway's zone pool of a stretch
cluster are created on the stretch rule at 4/2, admitted with exactly four
OSD hosts, the native two per site. (9) A change of `topology.stretch`, its
tiebreaker, failure domain, rule or a host's site refuses `stretch-intent`
by name in host and cluster blocks, before the generic ownership proof. (10)
The cluster evidence carries a `stretch` member, the last journaled stage and
whether stretch mode is enabled as this request enabled it; readiness also
requires every stage's postcondition observed again; the core request moves
to `ceph-core-v35` and its evidence to `ceph-core-evidence-v13`; every
block's cluster scope carries the stretch block, so the host request moves to
`ceph-host-v10`; older records refuse by name (D142). (11) A stretch cluster's deadline adds its
two calls and one readiness wait, so seven hosts, the reference profile, are
the most a stretch cluster admits (21120 s), a refusal-table row of its own.
(12) The JSON keys of `mon dump`, `osd dump` and `osd crush tree` that no
cited page fixes are recorded as unverified in
[the stretch knowledge page](../../.agents/knowledge/ceph-stretch-native-facts.md)
and read from the selected build by [B488](m2.md#b488)'s emulated rehearsal.
Exit evidence: the request and evidence goldens, the stage, interruption,
drift, foreign-state and frozen-intent tests over the scripted monitor, the
controller order test and the refusal-table row; the operator gate is B488's
emulated rehearsal of the reference profile.

### D201

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "Plan and implement what is still missing" and said "the
solution must be as good as possible in terms of well-designed architecture
and ux". It answers [Q19](m3.md#m3a-open-decisions) and
[Q20](m3.md#m3a-open-decisions) and closes the questions left in
[B88](m3.md#b88)'s `Specified` definition, and implements B88 and
[B731](m3.md#b731)'s in-tree part as the planned slice X130; B88's Requires on
[B66](m3.md#b66)'s accepted row is waived for implementation only, the
acceptance staying B66's operator gate. Taken by the session, pending the
owner's review:
(1) Q19 (a): B731 is one operator run, of lab-kubevirt on authored
`baremetal` with installer VIPs, which OpenShift's own keepalived and haproxy
serve on the nodes' shared layer 2; this slice adds that example. The
derived-`none` lab-kubevirt-external variant stays in projection goldens and
validation and gets no row.
(2) Q20 (a): a KubeVirt release removes the media volume and deletes the
upload DataVolume by UID; the claim that held the image stays bound until the
node's VirtualMachine next stops, no node is restarted to release it, and
[security](../security.md#network-remote-systems-and-privilege) records the
exposure. Option (b), a live eject keyed on the observed declarative hotplug
gate, is [B925](#b925).
(3) The installation lends each node's controller credential by the parts the
arm's `ControllerCredential` fact names: for KubeVirt the kubeconfig's value
alone as the port's `user` file, with no password or trust-bundle material,
and a pushed cluster lends no artifact certificate.
(4) A node on a pinned port carries the identity its Machine's block recorded
in the original apply (`ApplyProved`, else the attempt's `Proved`) to every
entry point of every operation, apply, removal and observation; an apply that
finds no record refuses before the adapter runs, and a removal or observation
passes what it has, the port refusing an unpinned node with nothing changed.
The frozen node says so (`portPinned`), so the role names no arm.
(5) The install request freezes the clients a pushing arm's port runs
(`portTools`): `virtctl` with the Environment's controller-stage intent, which
an apply's attempt locates in the proved closure, since only its push runs it; a `latest` intent resolves to the
one release that closure holds, and to nothing when it holds none or several.
(6) A pushed node is handed the image file in the work area, `virtctl` and the
media identity in place of a link, so no link is minted, proved, retired or
swept; it presents this cluster's own image when its port reports the volume
its own, carrying that identity. The boot requires the image a regular file at
the frozen path.
(7) The node ceiling is [B714](m3.md#b714)'s derived bound over the KubeVirt
arm's published bounds: eight nodes; a larger cluster refuses naming it. A
nine-node hub waits for B731's measured bounds, [B926](m3.md#b926).
(8) Node placement ([D199](#d199)) composes unchanged: it is read and written
through the installed cluster's API, whatever arm realized the nodes.
(9) The cluster's removal composes the arm's: the install inverse ejects
through the pinned port, and each Machine's inverse, which the plan orders
after it and gates on the instance being absent, proves the VirtualMachine,
both DataVolumes and every claim absent.
(10) The pre-boot VIP probe reaches no KubeVirt attachment; the evidence names
those VIPs unprobed and the runbook asks the operator to prove them free;
[B927](#b927) records a probe.
(11) The install request moves to `cluster-install-agent-v11`, adding
`portTools` and `nodes[].portPinned` and admitting the controller kind `api`;
a v10 record refuses by name (D142), and the media request is unchanged.

### D202

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "Plan and implement what is still missing" and said "the
solution must be as good as possible in terms of well-designed architecture
and ux". It answers [Q21](m3.md#m3a-open-decisions) and
[B732](m3.md#b732)'s questions 1, 2, 3, 5 and 6 (question 4 is
[D193](#d193)'s), specifies [B908](m3.md#b908), and implements both as the
planned slice X131; B732's Requires on the operator acceptance of B88, B83
and B726 is waived for implementation only, each acceptance staying its own
operator gate. Taken by the session, pending the owner's review:
(1) Q21 (a): `hostClusterRef` is M3e's, so M3 completes with a host the
context builds.
(2) The proof block of a provider naming `hostClusterRef` requires the host's
`OpenShiftCluster`, every add-on instance on it but a last package's, and its
cluster network block when it declares secondary networks; each Machine block
requires the proof, the child installation its Machines, and a removal
inverts every edge. A plan cycle refuses naming one cycle's blocks in order.
A host on which no instance provides `kubevirt` refuses before registration.
(3) No arm run, port entry point or child installation is lent the host
installation's administrator kubeconfig. Before each run, the arm's `lend`
(operation `lend` of `substrate-host-kubevirt-v1`, request
`substrate-access-kubevirt-v1`) borrows it as the host installation's produced
input and posts one `TokenRequest` for ServiceAccount `bootwright-kubevirt`
of the provider's namespace, lifetime the consuming run's deadline plus 600 s
(at least 600 s, at most the run ceiling plus 600 s), and leaves as its one
output a kubeconfig of the same server and authority with that token alone,
lent to that run and cleared; a shorter granted lifetime refuses. No token
Secret exists, so there is no inverse. Every attempt, observation, quiescence
probe and removal mints again; the child installation mints once per host
namespace for its whole deadline.
(4) A lend creates no object, so the runner admits it in a read-only or
unrecorded execution with its one output, which no other operation may take
there, and a quiescence probe borrows produced entries as its removal does.
(5) The `kubevirt` package owns, from admitted facts alone, one Namespace per
hosted provider namespace labelled with the union of the
`namespaceSelector.matchLabels` of the host secondary networks the providers'
attachments name, the ServiceAccount and RoleBinding `bootwright-kubevirt` to
ClusterRole `bootwright-kubevirt-machines` (the arm's verbs and the
attachment-definition read), and ClusterRole and ClusterRoleBinding
`bootwright-kubevirt-proof` for the proof's cluster reads, narrowed by name
where the proof names its object, with a Role and RoleBinding of that name in
`openshift-config-managed` and `openshift-cnv` for the one ConfigMap each
holds. The arm's verbs gain claim `get`, which `virtctl image-upload` uses.
The token is bound to no object, so it stays valid on the host until it
expires; the lifetime bound is its only revocation. Two networks giving one key two values
refuse at admission, naming both; a host holds at most 16 hosted namespaces;
an attachment of a hosted provider that names no `ClusterUserDefinedNetwork`
among the host's secondary networks refuses at plan. External hosts keep the
operator's namespace.
(6) Readiness is the CSV `Succeeded` and the HyperConverged `Available`
`True` and `Degraded` `False`; an unready host leaves the child pending
through the edges, and a host the lend or the proof cannot read is unknown,
never absent.
(7) No edge orders a child's image build after the host's installation.
(8) Question 3b's `vmStateStorageClass` is not added: the owner's target VMs
need no vTPM, and the state class stays [B906](#b906)'s.
(9) B908: each KubeVirt Machine that is a cluster node freezes `spread`, the
first 16 hexadecimal digits of the SHA-256 of
`bootwright.substrate.kubevirt.spread-v1`, context, cluster and role; its
VirtualMachine carries the template label `bootwright.io/spread` and a
preferred pod anti-affinity of weight 100 on `kubernetes.io/hostname`, never
a required one, which would block a host drain; no host count warns.
(10) The requests move to `substrate-host-kubevirt-v2`, `machine-kubevirt-v2`
(adding `hostCluster`, `serviceAccount` and `spread`) and
`cluster-install-agent-v12` (adding `nodes[].controller.hostCluster`); older
records refuse by name (D142).
(11) `bootwright machine` power commands refuse, and the power reading omits,
a Machine on a hosted provider until [B930](m3.md#b930); the
lab-kubevirt-nested example and its README name the operator's stop through
the host's API before a destroy.
Exit evidence: the nested lifecycle journey, the lend, namespace, edge,
cycle and power tests, the package and contract goldens and the example; the
operator gate is the lab-kubevirt-nested run.

### D203

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "Plan and implement what is still missing" and said "the
solution must be as good as possible in terms of well-designed architecture
and ux". It authorizes [B487](m2.md#b487) as slice X200 and
[B892](m2.md#b892) as slice X203 on M2, out of sequence, and is the dated
waiver of their Requires on B486's acceptance and B487's qualified service
items, for implementation only. It answers the stretch services brief's
B487-Q1 to Q3 and closes [B316](m2.md#b316)'s remainder. Taken by the
session, pending the owner's review, for B487
([stretch services](../ceph.md#stretch-services-and-front-doors)): (1) The
consumers' set qualifies first: MON, MGR, OSD and the derived crash
collector, MDS for CephFS, RGW with its front doors and the CephConsumer
export Data Foundation's external mode reads (Q1 (a)). NVMe-oF refuses on a
stretch cluster, because IBM requires at least four Ceph nodes per data site
and the reference profile has three; its profile is [B935](m2.md#b935). An
NFS service, a CephNFSExport and a CephSMBResource of a stretch cluster
refuse until their own profiles ([B936](m2.md#b936)). Observability and
management services keep their ordinary admission and claim no site-loss
behaviour ([B937](m2.md#b937)). (2) Placement keeps native labels and
selectors and adds no field (Q2 (a)+(c)): a service's sites are those of its
resolved hosts, which resolve to the Environment's sites, so B316 needs no
service-placement field and closes. An `mgr`, `mds` or `rgw` service's
resolved hosts exclude the tiebreaker and include a host at each data site,
and a `count` without `count_per_host` exceeds its hosts at either data
site, so every placement cephadm may choose holds a daemon at each; a direct
`rgw` service places a daemon on every host (the attached-gateway rule). (3)
A qualified service creates no pool off the stretch rule. `.rgw.root`, which
radosgw-admin creates on the default rule when absent, is created by the
gateway holding its claim, after the claim and before any radosgw-admin call,
on the stretch rule at 32 PGs with the autoscaler on, then set to 4/2 with
the `rgw` application; a claimed `.rgw.root` on another rule refuses
`stretch-pools` and is never moved; the gateway's readiness requires it on
the rule at 4/2. No Bootwright effect moves a pool another tool created.
(4) RGW front doors (Q3 (a), admitting (b)): direct, every rgw host's public
address behind an operator-owned load balancer or DNS, or a declared
DNSServer's records; or exactly one ingress per data site, each placed only at
its site with its own virtual IP and the same `frontend_port`, `ssl` and bound
Secret, the endpoint resolving to both virtual IPs. An rgw ingress whose hosts
span both data sites is a stretched VIP and refuses, as does an rgw backend
behind one ingress. (5) The gateway's readiness requires its rgw daemons at
both data sites, each answering on its frontend port, and behind ingresses
every ingress's daemons and every virtual IP answering, over TLS with the
bound leaf; client I/O during a site loss is B488's operator evidence. (6) The
consumer export carries every MON, the tiebreaker's included, every manager
host and the gateway's endpoint addresses; Data Foundation external mode
against a stretched cluster is owner-run acceptance under
[B488](m2.md#b488) only, and no compatibility rule is recorded (D163). (7) The
gateway's request admits several ingress documents and the `.rgw.root`
effect, so the core request moves to `ceph-core-v36`; older records refuse by
name (D142). Exit evidence: admission tests per rule and their neighbours,
gateway-worker fakes for `.rgw.root` placement, per-site ingress readiness
and site coverage, and a consumer export planned over a stretch cluster; the
operator gate is B488's emulated rehearsal of the reference profile.
Taken by the session, pending the owner's review, for B892
([provided tiebreaker removal](../ceph.md#a-provided-tiebreakers-removal)):
(8) A stretch cluster whose tiebreaker is a provided Machine retires with its
installed hosts: that Machine leaves only the surviving-host binding, and
the cluster block's destroy removes the tiebreaker's footprint after every
read-only refusal of the retirement and before its retirement record, so a
failure stops the destroy before any data-site Machine is deleted; every
other surviving host keeps the refusal. (9) The removal reaches the
tiebreaker over the fleet connection the cluster already uses for that host,
with its pinned host key, and needs neither the bootstrap host nor MON
quorum. (10) Before any effect it proves, read-only, the owner marker under
`/var/lib/bootwright/ceph/<fsid>` equal to the cluster's marker identity,
`cephadm ls` listing only `cephadm:v1` daemons, none of another FSID and of
this FSID a subset of the host's `mon` and `crash`, and no other FSID's
directory under `/var/lib/ceph`; anything else refuses `tiebreaker-foreign`.
(11) Under one attempt record it removes the recorded cluster-key line from
the cluster SSH user's `authorized_keys`, the registry credential
`/etc/ceph/podman-auth.json` under its recorded metadata, and runs the
package cephadm's `rm-cluster --fsid <fsid> --force`, never `--zap-osds`,
whose footprint (the FSID's units, directories and files, with no cluster
left cephadm's shared target, logrotate file and logs, and `/etc/ceph`'s
conf, public key and admin keyring when the conf names the FSID) is
unverified until B488 reads it; packages, images, firewall openings, users,
disks, the OS and Bootwright's records stay. (12) Absence is proved by no
daemon, unit or target of the FSID, none of its data, log or logrotate
paths, no FSID in `/etc/ceph/ceph.conf`, and neither the key line nor the
credential, read without following links; only then is completion recorded
and the retirement evidence names the tiebreaker as cleaned. (13) A lost
reply leaves the effect unknown: the next attempt observes first, completes
without a command when absence is proved, and otherwise repeats each removal
under its attempt record; an unreachable tiebreaker reads as unknown, never
absent, refusing `tiebreaker-unreachable` before the retirement record; a
decommissioned or reinstalled tiebreaker has no exit yet ([B938](m2.md#b938)).
(14) The cluster's retirement evidence gains the cleaned tiebreaker and its
version moves; older records refuse by name (D142). Exit evidence: destroy
tests over the fake transport for a lost reply, an unreachable host, a
foreign daemon on the tiebreaker and the complete journey refusing nothing;
the operator gate is the removal in B488's emulated rehearsal.

### D204

**session-pending** (2026-10-28, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "Plan and implement what is still missing" and said "the
solution must be as good as possible in terms of well-designed architecture
and ux". It defines and authorizes [B894](m2.md#b894), the defect of X198
that no stretch cluster with a provided tiebreaker both validates and plans,
as slice X204 on M2. Taken by the session, pending the owner's review: (1)
The [fleet key](../api/environment.md#remote-machine-access-and-install-defaults)
is effect material: an installation places its public half on every
installed host, so only its bound version proves those hosts' access, and
it is never rebound. A Machine's `access.ssh.auth.privateKeyRef` is an
access credential the operator owns and rotates freely, since only operator
sessions present it and no operation binds it; were it bound, only the
[access-credential rebind](../state-reconciliation.md#access-credential-rebind)
could move it, which refuses effect material. One Secret cannot be both:
rotating what the operator reads as one host's login would strand every
installed host, and its class would depend on which blocks name it. So the Environment invariant that the
fleet key differs from every authored Machine access key stays, with no
exemption for a tiebreaker (option (b) rejected). (2) A provided stretch
tiebreaker's Machine declares `access.ssh` with the cluster SSH user and a
`knownHostsRef` host-key pin; its `auth` arm (an operator identity, a key of
its own or a password) serves operator sessions only, and no Ceph request
names it. (3) Ceph dials the tiebreaker with the Environment's fleet key, as
it dials every Ceph host; the frozen host entry's `sshKeyRef` already names
that key, so no request, evidence or contract version moves. (4) The
operator authorizes the fleet key's public half for the cluster SSH user on
the tiebreaker; Bootwright never places it there, having installed nothing.
The host block's first fleet call proves it, and a `transport` failure on a
provided host names the Machine, its endpoint, the fleet key Secret and how
to show its public half, the user and the pinned host key Secret, beside the
retained evidence to inspect, since that reason also covers a worker's
malformed output; no separate preflight probe is added. (5) Planning refuses a tiebreaker whose SSH user
is not the cluster SSH user, that pins no host key, that names the fleet or
cluster key as its access key, or whose Environment declares no fleet key;
validation's refusal of a provided Machine naming the fleet key offers an
operator identity or a key of its own and says that a host Bootwright dials
with the fleet key, as a stretch tiebreaker, never names it but has its
public half authorized; the Environment names no storage kind for this. (6) [D196](#d196) (6)'s "with the fleet key" reads as dialed with it;
D196 is otherwise unchanged. (7) Exit evidence: an end-to-end validation and
plan over a full catalog with a provided tiebreaker, the refusals of (5),
the named `transport` refusal and the multidc-platform reference's
tiebreaker Machine in this shape; the operator gate is B488's emulated
rehearsal reaching a provided tiebreaker authorized only with the fleet key.

### D205

**session-pending** (2026-10-29, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "Plan and implement what is still missing". It fixes how X203
realizes [B892](m2.md#b892) under [D203](#d203) (8) to (14), as
[a provided tiebreaker's removal](../ceph.md#a-provided-tiebreakers-removal)
states. Taken by the session, pending the owner's review: (1) The daemon
inventory D203 (10) and (12) call `cephadm ls` is the bounded read of
cephadm's own layout every Ceph phase uses, because `cephadm ls` starts
cephadm's logging, which writes host files
([cleanup facts](../../.agents/knowledge/ibm-ceph-cleanup.md)); with
`/var/lib/ceph` holding nothing but the FSID's directory it proves only
`cephadm:v1` daemons and none of another FSID. A MON's or crash collector's
ID is the tiebreaker's short or full name. (2) The proof before effect also
reads the cluster-key line (no line names the recorded key but the recorded
one), each registry credential name (absent, or carrying its record's
descriptor metadata; a name without a record is foreign) and the fixed
layout (a link or non-directory where it keeps a directory); each refuses
`tiebreaker-foreign`. (3) Each removal runs only while its part remains, and
`rm-cluster` only while a native part does, the package cephadm's proof and
its parser's declaration of every option passed both read before the attempt
record; the key line is removed by
a staged rewrite that keeps every other byte, the file's owner and mode, and
refuses a file changed since it was read. (4) Unit absence reads the unit
files in `/etc/systemd/system` and its `ceph.target.wants`, not systemd's
loaded state. The removal observation reports a partial effect once an
attempt record exists and no effect before one; a failed fleet connection to
that host refuses `tiebreaker-unreachable`. A `retirement: tiebreaker` value
selects the run within the cluster block's ordinary destroy and observe, so
the observation stays the runner's read-only operation; the run takes no
produced output and lends only the fleet key and the pinned host keys. (5)
The sshd drop-in the `authorize` phase writes stays, since D203 names no
such removal and the removal touches nothing it does not name;
[B940](m2.md#b940) records it. (6) A CephConsumer's and a CephReplication's
frozen requests name the provided Machine among the hosts they retire with,
so their destroy still refuses by the surviving-host rule;
[B941](m2.md#b941) records it rather than moving their versions here. (7)
Every core block's retirement record carries `ceph-core-retirement-v2` (the
unversioned record being the first), and the cluster block's adds its
`tiebreaker` member; that version and the tiebreaker evidence's
`ceph-tiebreaker-evidence-v1` enter `storage-ceph-core-v3`'s content digest,
the request version staying, since no request member changes; the SMB,
consumer and host-release records are unchanged. (8) Exit evidence: the
fake-transport destroy and observation tests and the worker tests over a
temporary root that B892 names; the operator gate is the removal in B488's
emulated rehearsal, which reads the `rm-cluster` footprint the
[stretch native facts](../../.agents/knowledge/ceph-stretch-native-facts.md)
record as unverified. (9) A tiebreaker the apply never claimed has no owner
marker and refuses `tiebreaker-foreign` even with no footprint;
[B942](m2.md#b942) records it, as treating such a host as absent needs the
owner's decision.

### D206

**session-pending** (2026-10-29, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "Plan and implement what is still missing". It defines and
authorizes [B941](m2.md#b941) as slice X205 on M2 and waives B941's unmet
Requires, B892 awaiting operator acceptance, for implementation only. Taken
by the session, pending the owner's review: (1) A
[CephConsumer](../ceph-resources.md#cephconsumer)'s frozen request and each
[CephReplication](../ceph-resources.md#cephreplication) endpoint freeze
`providedMachines`, the sorted provided Machines of their cluster's frozen
hosts, beside `machines`, which still names every host Machine. Their
surviving-host binding is `machines` without `providedMachines`, so the
provided tiebreaker leaves it exactly as it leaves the core blocks' (D205);
a request whose `providedMachines` is not a sorted subset of `machines`
that leaves an installed Machine refuses at decode, and a surviving
Bootwright-installed host still refuses by the
[surviving-host rule](../ceph.md#removal-and-concurrency). (2) Their
retirement records keep their form and still name every host Machine in
`retiredWith`, the tiebreaker included, as the cluster's own record does;
no record or evidence version moves. (3) Their runs, in Go and in the
workers, refuse a cluster proof whose provided Machines differ from the
frozen ones, as they refuse another host set. (4) Neither block reaches the
tiebreaker at destroy, so no fleet connection is added for them: an export's
principals and a replication's peer and bootstrap credential are entries of
the cluster's monitor store, whose copy on the tiebreaker is FSID data the
cluster block's `rm-cluster` removes; the export keys' and the replication
token's custody is on the bootstrap host, never the tiebreaker; and no
rbd-mirror daemon runs there, since an authored service placed on the
tiebreaker refuses. Those blocks record their retirement before the cluster
block's removal runs; a refused removal stops the destroy before any
data-site Machine is deleted, and the next destroy continues at the cluster
block, as for every other core block. (5) The consumer request moves to
`ceph-export-request-v3` and the replication request to
`ceph-replication-v2`, moving `ceph-export-v1`'s and
`storage-ceph-replication-v2`'s content digests, because a frozen request of
the earlier version names a provided tiebreaker as a host it retires with; a
request of the earlier version refuses by name (D142), so an operation an
earlier build applied continues or is removed only under that build. (6)
Exit evidence: the engine destroy journeys of a stretch cluster with a
provided tiebreaker and a CephConsumer, or a CephReplication, that refuse
nothing, record each retirement and record the cluster's only once the
tiebreaker reports its footprint absent, with the binding, decode and
surviving-host refusal tests; the operator gate is B488's emulated rehearsal
destroying such a cluster with its consumer export.

### D207

**session-pending** (2026-10-29, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). The owner asked
the session to "Plan and implement what is still missing". It answers
[B930](m3.md#b930)'s open questions and specifies it, and fixes how
[B910](m3.md#b910) is met; X206 implements both, and X207 fixes
[B950](m1.md#b950), which blocked both. Taken by the session, pending the
owner's review:
(1) A power command (`machine start`, `stop`, `restart`, `stop --force`) or
the power reading on a KubeVirt Machine whose provider names
`hostClusterRef` registers no operation; inside its bounded runtime, after
every resolution and the confirmation and before its own run, it runs the
arm's `lend` (operation `lend` of `substrate-host-kubevirt-v1`, request
`substrate-access-kubevirt-v1`, unchanged) as an unrecorded, read-only
execution bound to that run's request digest, and lends the minted
token-only kubeconfig to that one run, clearing it on every return. No
record, evidence, log, result or frozen request carries the token.
(2) The lend run alone is handed the host installation's administrator
kubeconfig, read from the context's custody entry `cluster-install-<cluster>`,
`kubeconfig`, under custody's shared lock, the read `diff --observe` makes;
only that entry of the provider's host cluster is lent, and only proved: a
missing or unproved entry refuses a power command `access.unavailable`
naming the apply that completes the host, and leaves a reading `unknown`.
(3) A destroy part way through leaves the entry readable: custody withdraws it
only when the host installation's removal completes, which the inverted edges
order after every hosted Machine's removal, so it is held while any hosted
VirtualMachine may exist.
(4) The power run states a deadline, the arm's power bound plus two state
reads and 300 s for the run itself (1260 s), and a reading one state read per
Machine and the same 300 s; each lend's
lifetime is that deadline plus 600 s, as [D202](#d202) (3) sets.
(5) A reading groups hosted Machines by placement host, host cluster and
namespace, so one lend serves one reading run; a lend that fails, or a run
that ends unanswered, leaves that group's Machines `unknown`, never absent, and every other
group still reads; a cancellation still ends the reading.
(6) Every hosted verb is held to the Machine's pin as an external one is, so a
swapped cluster or another VirtualMachine refuses `identity-mismatch` before
any power request, and the refusal's remedy names the provider's
`kubeconfigRef` or `hostClusterRef`, not a BMC address.
(7) B910: each reading target of a pinned port names its own material values
(`pinnedUUIDVariable`, `pinnedSerialVariable`) carrying its Machine's pin,
which the read passes to the port; a Machine whose block recorded none, or
whose host refuses the pin, reads `unknown`.
(8) The requests move to `machine-power-v4` (adding `controller.hostCluster`)
and `machine-power-read-v4` (adding it and the pin variables); power is no
plan block and retains nothing, so no record refuses.
(9) A hosted Machine's quiescence refusal names `bootwright machine stop`, as
an external one's does.
(10) A KubeVirt pin is read from the current apply alone, so while a destroy
is the context's current operation a KubeVirt power command refuses
`identity-mismatch`; [B951](m1.md#b951) records it. Exit evidence: the
hosted power, reading, lend and pin tests and goldens; the operator gates are
the lab-kubevirt and lab-kubevirt-nested runs.

### D208

**delegated** (2026-10-29, the owner's E2E fix instruction): the owner
delegated fixes needed during their E2E testing to one temporary task branch,
with only short, targeted checks during testing. Long suites and integration
wait until testing finishes and the owner explicitly authorizes merging to
main. Under this delegation, X225 implements [B973](delivered.md#x225--e2e-execution-and-runbook-corrections), the bounded
controller forwarding-policy inspection correction. This authorizes the fix,
not an acceptance claim or skipped integration gates: independent safety
review and the full required gates remain necessary before delivery.

The owner's follow-up asks that prerequisites be explicit in the runbook,
or implemented where Bootwright owns them. X225 adds [B974](delivered.md#x225--e2e-execution-and-runbook-corrections) to
restore preparation order, operator-owned firewall admission and the D178
diagnostic package procedure after runbook migration. B509's historical
delivery remains intact; the new item records the regression without moving
runbook-only tools into Bootwright's controller prerequisites.

The owner further requires simple runbooks executed from beginning to end,
with no alternate resume flow. B974 applies that sequence to all E2E cases:
shared preparation, installation, verification, required drills and cleanup.
A selected variant succeeds only as a complete run; planned negative tests
retain their explicit expected outcomes.

The 2026-10-30 E2E follow-up adds [B978](m1.md#b978) to X225: shorten
invocation scratch names so Ansible's local RPC socket fits, preserving
private custody and cleanup of earlier names. This is a runner correction,
with no change to a frozen request or host effect identity.

The same delegation covers [B979](m1.md#b979): an encrypted install brings
up its declared network before its pre-script fetches recovery material.
The correction retains the supported install topologies, TPM proof and
verified TLS; a frozen installation is removed and planned again rather than
rewritten to adopt a changed Kickstart.

The owner's progress follow-up adds [B980](delivered.md#x225--e2e-execution-and-runbook-corrections): installation output
names the post-network work actually running, including subscription
registration and package updates. This changes presentation on new plans;
existing frozen plans and running invocations retain their definitions.

On 2026-10-30, the owner explicitly authorizes integration into local main
after the required gates, squashed into two commits: code fixes and
documentation updates. The temporary task branch and worktree are removed
after integration. This authorization changes no operator-acceptance status
and authorizes no push; the
[X225 record](delivered.md#x225--e2e-execution-and-runbook-corrections) separates
in-tree delivery from the remaining operator gates.

### D209

**delegated** (2026-10-29, the owner). The owner asked to "rename bootwright
setup to bootwright install" and to "add a new command: bootwright
uninstall", then told the session to "proceed with recommendations". It
authorizes B975 and [B976](m1.md#b976) as slice [X226](delivered.md#x226--controller-install-and-uninstall) on M1, out
of sequence by the owner's request. The recommendations it accepts:
(1) the command is `bootwright controller install`, beside `controller
rebind`, because `install` alone reads as installing the platform, which
`apply` does; there is no alias.
(2) Its refusal code becomes `controller.install`; the setup subtree, its
record format, its lock and its run IDs keep their names, so no prepared host
needs another install.
(3) `bootwright controller uninstall` refuses while the store holds any
context, naming `destroy` and `context delete --purge`.
(4) It removes only the host packages the install receipt records as
installed by an install run, never one present before Bootwright, refuses when
another installed package requires one, shows dnf's own removal preview before
confirmation, and `--keep-packages` leaves every package.
Decided by the session under the same instruction:
(5) packages a context's controller stage installed stay, because no record
names them;
(6) a media store that holds an image refuses, naming `media delete`, because
uninstall never deletes data the operator supplied;
(7) an interrupted install, or an install record this build cannot read,
refuses package removal unless `--keep-packages`, because what it installed is
not known;
(8) packages are removed before the state, and the state is renamed away
before it is deleted, so running uninstall again finishes an interrupted one.
The [uninstall flow](../controller.md#the-uninstall-flow) owns the contract.

### D210

**accepted-by D211** (2026-10-30, the owner; the clauses after the owner's
words are taken by the session, pending the owner's review). After
[audit 002](../../audits/002-addons/report-final.md) the owner asked:
"prepare a plan to implement using best practices and sound architecture the
gaps ... do not implement yet. when the plan is done i will use other lower
cost model". Taken by the session, pending the owner's review:
(1) The audit's work packages are M4 items [B981](delivered.md#x227--add-on-discovery-and-operator-journey) to
[B987](m4.md#b987) and the parked [B988](#b988), with slices X227 to X231 in
M4's Planned slices; none is active, nothing is implemented, and an
implementer takes a slice only on the owner's explicit request, in the order
X227 (B981, B982), X228 (B983), X229 ([B984](delivered.md#x229--placement-namespaces-in-the-frozen-add-on-request)), X230 ([B985](delivered.md#x230--add-on-packages-without-name-keyed-generic-code)), then X231 (B986)
once Q135 is answered.
(2) The decisions the plan needs are open questions Q135 to Q138 on M4's
page, each with a recommendation: Q135 kind defaults never select a package;
Q136 an additive apply only if the owner wants day-2 additions, after X227;
Q137 presets only with a repeated consumer; Q138 B90's first slice as an
authored OLM definition compiled by the existing capability, after X229.
(3) Routing follows the owner's model table: X229 and the definitions of
B987 and B90 are complex; the other slices are routine, X230 with a
complex-tier review of its registry rules.
(4) [B770](#b770) and [B769](#b769) stay parked; [B884](delivered.md#x227--add-on-discovery-and-operator-journey) and
[B886](m4.md#b886) keep their definitions, and B884 may land as X227's first
commit.
(5) The audit reports stay as written; each item names the audit finding it
closes in its Alias cell, and the consolidated report's delivery sequence is
superseded by this record.

### D211

**owner** (2026-10-30): "proceed implementation. i accept your recommendation
for question for my decision". It accepts [D210](#d210), answers its open
questions with their recommendations and authorizes the plan's
implementation: (1) [Q135](m4.md#m4a-open-decisions) (b): Environment kind
defaults fill only the package members a binding declares and never select
one, which specifies [B986](delivered.md#x231--kind-defaults-never-select-a-package). (2) Q136 (a): a package added to a
cluster after its apply completed is installed by one additive operation
linked to the completed one; the owner's audit request asked for optional
add-ons "added as needed by operator", so day-2 addition is wanted, and
[B987](m4.md#b987) is defined and implemented after X227. (3) Q137 (b): no
named presets until a repeated consumer; [B988](#b988) stays parked. (4) Q138
(a): [B90](m4.md#b90)'s first slice is an authored OLM `ClusterAddon` compiled
by the existing capability, defined and implemented after X229. (5) Slices
X227 to X231 are authorized in their planned order, at most two active at a
time; B987's and B90's slices are planned when their definitions close and
are covered by this request.

### D212

**delegated** (2026-10-30, the owner). The owner asked to "implement all
backlogs you created in this session". The session created B973, B974, B977,
B978, B979 and B980. [X225](delivered.md#x225--e2e-execution-and-runbook-corrections)
delivered their in-tree work except [B977](delivered.md#x232--sudo-terminal-hint-admission); B978 and B979 retain
operator acceptance on M1. This request promotes B977 from parked Candidate
to Specified on M1 as named safety slice X232, out of sequence by the owner's
request. It authorizes accepting exactly `COLORTERM` at direct and supervised
sudo entry and discarding it before any subprocess, with the owning security
contract, sudo guidance and executable safety evidence. The other inherited
variable refusals and all existing operator-acceptance gates remain in force.

X232's integration gate exposed [B989](delivered.md#x233--permission-fixture-modes),
a pre-existing permission fixture defect under an inherited restrictive umask.
The session applies the repository's [pre-existing-defect rule](../../AGENTS.md) as X233 to correct
that necessary gate blocker in its own commit. This adds no owner decision
or product scope; [B673](#b673) remains separately open.
