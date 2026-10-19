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
| [B102](#b102) | C8 | product | Custom automation | One typed, invertible executable playbook journey | M6's packages are add-ons, not playbooks |
| [B103](#b103) | C14 | safety | Workspace and Secrets | Explicit complete-store restore | No milestone needs backup and restore |
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
| [B247](#b247) | new, 2026-09-30 (X32) | safety | Infrastructure services and Substrate | Socket claims of an SSH placement that is the controller | Found after the M1 freeze (D48); publishing SSH claims needs an owner decision |
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
| [B333](#b333) | new, 2026-10-05 | enabling | Infrastructure services | A DNSServer binds its endpoint address by default | D114: M1 keeps the wildcard default and names a colliding socket |
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
| [B462](#b462) | new, 2026-10-08 (X46) | enabling | Container cluster | The media role's keep task is qualified in an agent-install rehearsal | Found in X46; parked under D48 (D116); needs an agent-install run on a host, which B61's operator gate provides |
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
| [B572](#b572) | new, 2026-10-14 (X91 audit) | safety | State reconciliation and Workspace | Operation-area admission reserves every record a running block's completion and resolution can need | Found by the X91 audit; no milestone depends on it: it takes roughly 340 retried attempts to reach the bound; extends B243 and B267 |
| [B573](#b573) | new, 2026-10-14 (X91 audit) | safety | Storage | A Ceph remote worker ends on cancellation or disconnect, or the engine waits out its budget | Found by the X91 audit; no milestone depends on it: the bootstrap marker fences the one destructive race |
| [B575](#b575) | new, 2026-10-14 (X91 audit) | enabling | State reconciliation | One pure classifier replaces the five parallel transition derivations | Found by the X91 audit; no consumer until the first scenario slice after X91, whose first step it is |
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
| [B592](#b592) | new, 2026-10-14 (X91 audit) | defect | CLI | The CLI spec's stage-selection text matches the retry rule | Found by the X91 audit; no milestone depends on it: stale contract text |
| [B593](#b593) | new, 2026-10-14 (X91 audit) | enabling | Desired state | `RenderEffective` can keep the authored catalog and origins for a consumer that needs them | Found by the X91 audit; no consumer: `diff` takes the compile port |
| [B594](#b594) | new, 2026-10-14 (X91 audit) | enabling | Desired state | One envelope shape definition serves the decoder and the encoder | Found by the X91 audit; no milestone depends on it: drift risk only |
| [B595](#b595) | new, 2026-10-14 (X91 audit) | enabling | Storage | A Ceph refusal table test and a docs-check entry | Found by the X91 audit; no consumer until the first Ceph delta scenario |
| [B596](#b596) | new, 2026-10-14 (X91 audit) | enabling | Storage and Container cluster | Ceph and agent-install observations run without outputs when no operation records them | Found by the X91 audit; no milestone depends on it: until then `diff --observe` reads these blocks as unknown |
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
| [B622](#b622) | new, 2026-10-18 (D159; found while planning M2c) | defect | Container clusters | The agent-install projection keeps an authored `dns-resolver.config.server` | M3's agent-install journey is its first consumer and no milestone exit names it yet |
| [B624](#b624) | new, 2026-10-18 (D159; found by the X55 checkpoint) | enabling | CLI and Managed OS | `diff --observe` shows a Machine's bounded network differences | Display only; the runbook reads the drifted value from the guest, and a projector is promoted with B576 |
| [B630](#b630) | new, 2026-10-19 (X101) | safety | Storage | A proved-absent native rejection leaves a bootstrap or `set-user` attempt recoverable instead of `partial-cluster` | Found in X101; narrowing a safety refusal needs its own named slice and decision; X101's argv check before the attempt record already covers the bootstrap options |
| [B631](#b631) | new, 2026-10-19 (X101) | enabling | Storage | A wrong `ceph.ibm.callHome` for the selected build is refused before any host block runs | Found in X101; Go has no planning-visible Call Home model fact, and D157 forbids gating on the release |
| [B632](#b632) | new, 2026-10-19 (X101) | enabling | Storage | The unused `installed_cephadm` and the tests that patch it are removed | Found in X101; dead code with no effect |
| [B633](#b633) | new, 2026-10-19 (X101); widened 2026-10-19 (X102) | defect | Architecture and developer tooling | Two unit tests stop failing under load: the native-timeout test's xdist worker and the libvirt firewall readiness replay case | Found in X101 and X102; each file passes alone and the whole tree passed on rerun |
| [B634](#b634) | new, 2026-10-19 (X101) | enabling | Storage | The libvirt three-node Ceph runbook validates the operator's selected coordinates, not only the tracked ones | Found in X101; documentation for the owner's runs, no gate |
| [B635](#b635) | new, 2026-10-19 (X101) | enabling | Storage, with Container clusters | The edges X101's reviews left untested or unreconciled are tested or recorded | Found in X101; each is hardening of a path no signed build reaches today |
| [B657](#b657) | new, 2026-10-19 (X102) | safety | Storage package resolution, with Security | Plan packages from the `broker` root must be RHEL-signed and sourced from the host's DVD tree | No trust break today: every package is verified under its repository's pinned signer, and no IBM repository ships `openssl` |
| [B658](#b658) | new, 2026-10-19 (X102) | enabling | Storage preflight and State reconciliation | Cross-context capacity admission states its limit or holds a host-scoped reservation | No spec violation, a short window, and a reservation would be new durable cross-context state |
| [B659](#b659) | new, 2026-10-19 (X102) | product | Ceph core lifecycle | An apply that stopped after enrollment continues from per-host receipts instead of refusing `partial-cluster` | Needs a new durable recovery protocol, and the native API spec declares it out of scope |
| [B660](#b660) | new, 2026-10-19 (X102); assigned to M2c lane x61-ceph-fips on 2026-10-19 (D166) | safety | Ceph host runtime route probe, with the future FIPS core profile | The route probe's synthetic keys follow the cluster key's type under FIPS | Declared FIPS is refused at planning; D166 assigns the probe key contract to M2c lane x61-ceph-fips |
| [B661](#b661) | new, 2026-10-19 (X102) | enabling | Ceph host runtime image profile | The cephadm payload bound is raised or derived and an oversized payload refuses with its measured size | The largest current payload is 1,676,391 bytes; a future refusal, not a defect |
| [B662](#b662) | new, 2026-10-19 (X102) | safety | Ceph lifecycle, Devices and destructive ownership | An intent-marked OSD device with partial LVM metadata refuses with a named token and a supported recovery | Nothing is adopted or zapped today; a real remedy needs an owned-zap design |
| [B663](#b663) | new, 2026-10-19 (X102) | enabling | Storage adapter | Authored package candidates mark only the chosen root, keeping DNF's own no-match text | No wrong plan was observed; refusing glob characters would be a Bootwright version grammar |
| [B664](#b664) | new, 2026-10-19 (X102); assigned to M2c lane x59-d4-host-pull-route on 2026-10-19 (D166) | product | the owner for scope, then Storage adapter | Mirror or disconnected IBM package sources are decided, with the publisher key location authored beside them | Fail-closed today; D166 decided the scope and assigns the fix to M2c lane x59-d4-host-pull-route |
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
| [B675](#b675) | new, 2026-10-19 (X102) | product | Storage, Ceph Call Home | `ceph.ibm.callHome: disabled` passes the opt-out flag when the selected build declares it | Keyed on artifact facts, not a version label; a feature gap outside a defect slice |
| [B676](#b676) | new, 2026-10-19 (X103) | product | Controller | `spec.dependencyVersions.libvirt` honours a NEVRA pin through host `dnf`, or the field leaves the grammar | Implementation scope, not a compatibility rule |
| [B677](#b677) | new, 2026-10-19 (X102) | enabling | Storage | The misnamed `unsupported-release` Ceph refusal token is renamed `unsupported-coordinates` | Cosmetic, and it collides with the closed reason lists the m2c lanes edit |
| [B678](#b678) | new, 2026-10-19 (X102) | defect | Storage package resolution | A non-rhel host refuses `package-platform` naming the os-release ID, not a generic `package-resolution` | Fails closed before any effect; low value |
| [B679](#b679) | new, 2026-10-19 (X102) | enabling | Storage and State reconciliation | The edges X102's lanes and reviews left untested, unmeasured or unreconciled are tested or recorded | Found in X102; each is hardening of a path no known input reaches |
| [B680](#b680) | new, 2026-10-19 (X103) | defect | Container clusters | OpenShift admission of `distribution.release.version` accepts an exact publisher release such as a release candidate, and any stable-only screen found is removed | Found by X103; unverified, because X103 changed only the CLI tool pins and the installer download |
| [B681](#b681) | new, 2026-10-19 (X103) | enabling | CLI, Substrate and Docs | The edges X103's lanes and reviews left untested, unmeasured or unreconciled are tested or recorded | Found in X103; each is hardening of output or removal paths no known input breaks |
| [B682](#b682) | new, 2026-10-20 (X57; parked by D160, OD7, kept by D161) | product | Storage and Secrets | A StorageCluster binds the direct MGR dashboard's certificate | The management gateway fronts the dashboard with a bound certificate; X70's self-signed dashboard certificate stays |
| [B683](#b683) | new, 2026-10-20 (X57; parked by D160, OD7, kept by D161) | product | Storage and Secrets | Declared dashboard roles and local users with protected passwords | No approved API; SSO grants only system roles, and X70's administrator stays |
| [B684](#b684) | new, 2026-10-20 (X57) | product | Storage | The manager prometheus exporter serves external scrapers without a Prometheus service | No consumer; without a declared service the module would have no owner, so `spec.ceph.mgrModules` refuses it |
| [B685](#b685) | new, 2026-10-20 (X57; parked by D160, OD26) | product | Storage | Monitoring and gateway certificates that cephadm signs or that a certificate source selects, with the cephadm root CA reported | Bound certificates cover the gateway; cephadm-signed sources and root-CA retrieval have no documented native contract |

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

### B333

A DNSServer's bind address defaults to the wildcard address, which collides with any other resolver on the host; since [B299](delivered.md#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json) the pre-start check names the colliding socket. Deriving the default from the single endpoint address moves the effective state and request digests of live contexts, so it waits for a later window (found by the 2026-10-05 audit). **Exit evidence:** a normalization test deriving the bind address, and the effective-state and request goldens.

### B334

The emulated BMC runs as root, unconfined, with the libvirt socket, and requires a basic-auth credential; its TLS half, a generated serving certificate its client verifies, landed in-tree under [B446](m1.md#b446) (in progress, X47; D120), and the endpoint no longer serves plain HTTP. [B301](delivered.md#x44--help-completion-contracts-and-ratchets) binds the examples' BMCs to loopback. A refusal of a non-loopback bind address once an installation's provider host must be the artifact server's placement ([B289](delivered.md#x41--lifecycle-results-the-controller-stage-installation-shapes-and-bounded-runs)), and a confined SELinux type would shrink that surface (found by the 2026-10-05 audit). D120 split the TLS out as B446, now in M2 (D128). The loopback bind and the confinement remainder stay parked. **Exit evidence:** the refusal and the confined type, each with tests.

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

### B365

The environment spec says a linked add-on marker grants no exception, but the input reader refuses the whole read when a marker is hard-linked, since X40 as `input.symlink` (found in X40, older than it). **Exit evidence:** the spec and the reader agreeing, with a reader test for a hard-linked marker.

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

### B389

X41 moved dependency resolution under the staging parent `/var/lib/bootwright-staging`, with locked and swept stages and a refusal that names a noexec mount, while the controller runner's job and scratch parents stay under `/run` and `/var/tmp` (found in X41). [B19](delivered.md#x93--the-owner-run-of-2026-10-14)'s one runner may carry the move. **Exit evidence:** the controller runner's job and scratch under the staging port, with its sweep and noexec refusal tested.

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
The device's `VerifyCertificate` is [B447](m1.md#b447)'s. **Exit evidence:**
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

### B462

X46's D124 keep task, the media role's observation that copies the installer's
kept administrator kubeconfig into the output the engine publishes to custody
before the work area goes, is proved in-tree only: the Go journeys, the role's
unit tests and their mutants (found in X46). **Exit evidence:** an agent-install
rehearsal in which a completed installation whose custody publication failed is
destroyed, the kubeconfig held in custody marked unproved when the work area is
removed and exported by `cluster kubeconfig` with its `access.unproved` warning,
recorded in the [acceptance ledger](../../docs/acceptance.md) with the build
commit, or withdrawn by the owner.

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

[D155](#decisions) requires definition and delivery of this guard with B568's
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
destroy. A host-wide `bridge-use:` reservation, so that an external attachment
conflicts with another context's managed bridge, is a separate item to record
when this one is promoted. **Exit evidence:** a collection unit test that
`destroy.yml` refuses on an unanswered listing and on a plugged-in domain that is
not shut off, a capability test that `Quiescent` returns `lifecycle.live` naming
the attached domains, a lifecycle test that a continuation refuses the same way,
and `make docs-check` over the amended spec sentences.

### B568

[D155](#decisions) accepts persistent random incarnation identity as the
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
(`specs/storage.md:155-158`). Managed services and the artifact server show the
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

### B572

`capacity()` checks `entries+1 > maxOperationEntries` on every write, including
a Replace of an existing record and an Append to an existing log, which add no
entry (`internal/workspace/contextfs/operations_linux_amd64.go:739-750`, with
`:623`, `:648`, `:668` and `:697` settling zero entries). Admission reserves room
once at registration (`2*FirstPassEntries + 1024`,
`internal/reconciliation/operationstore/records.go:52-63`), and a continuation
re-admits nothing, so an operation's own retries and resolutions draw the
reserve down unchecked
(`internal/reconciliation/operationstore/store.go:168-191`, `:231-255`).
When an append fails at the bound, `logBoundary.fail` latches the fault and
cancels the shared work context, interrupting every sibling effect
(`internal/reconciliation/lifecycle/logging.go:31-48`, `:99-104`);
`CompleteAttempt` is refused, so the block stays durably `running`
(`internal/reconciliation/lifecycle/attempts.go:102-103`, `:153-156`); and every
exit is refused: resume restores with an Append, resolve needs a
`WriteExclusive`, and destroy restores before it resolves. Only `context delete
--purge --allow-orphans` remains, which leads into the same-name adoption of
B568. The `runtime.log` remedy also names the wrong cause. **Impact:** reaching
the entry bound takes roughly 340 retried attempts or tens of continuations with
several persistently failing blocks. The interruption equals a crash and adapters
tolerate it, but the promise to resume until equal is lost. This is a stronger
form of B243 and B267. **Smallest fix:** `capacity` counts only the entries a
write adds, so exhaustion stops at a creating write (`StartAttempt` or
`OpenLog`) before any effect runs, with a spec edit at
`specs/contexts.md:531`; optionally admit an attempt's whole write set before its
effect starts; give the bound its own diagnostic; derive `FirstPassEntries` from
one declared list of record paths. Schedule it before any scenario adds a
per-block record. **Exit evidence:** a test double that enforces the entry bound,
a test that hits the bound mid-attempt and leaves the block resolvable, and the
amended contexts spec.

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
(`specs/security.md:165-177` or `specs/storage.md:110`); then a remote-ceiling
wait, or a worker that ends on channel EOF, before any delta re-runs a Ceph
block. SSH qualification is already open in M2. **Exit evidence:** the spec
clause and comment now; a test that a canceled remote worker ends, or that the
engine waits its budget before it resolves.

### B575

The transition rule is restated in about five derivations. `decideOver`
(`internal/reconciliation/lifecycle/execution.go:367-423`) alone gates effects;
`previewed` (`internal/reconciliation/lifecycle/service.go:178-198`),
`supersedable` (`internal/reconciliation/lifecycle/execution.go:670-675`),
`nextAction` (`internal/reconciliation/lifecycle/attempts.go:572-582`, consumed
by `continuationCommand` and `status`) and `frozenReopenOf`
(`internal/reconciliation/lifecycle/frozenbinding.go:168-207`) re-derive parts of
the same facts. Only `TestTransitionTablesMatchSpec` drives `decide`;
`previewed`, `nextAction` and `frozenReopenOf` have no row-level test. **Impact:**
drift can only mislead `plan` or `status`, because effects pass through
`decideOver`; every new scenario would be threaded through five places by hand.
**Smallest fix:** `decide` reports the verb it chose, so `previewed` goes; one
pure `classify()` over the operation's verb and state and its pending and unknown
blocks returns a kind (fresh, continue, resolve, finalize, replace-removal,
settle or refuse); `decideOver`, `nextAction` and `frozenReopenOf` switch on it;
the `stateMachineRows` cases also assert the previewed verb and `nextAction`. It
is the first step of the first scenario slice after X91. **Exit evidence:** the
refactor with no behavior change, `TestTransitionTablesMatchSpec` green, and a
row-level test of each derived view.

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
needs a field. [D155](#decisions) accepts this correction for a future named
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
and `apply` refuses. **Impact:** misleading output, no harm. `diff` already shows
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
check, writing drift back unreported. [D155](#decisions) accepts refusal without
writes until a named delta admits drift.
[X99](delivered.md#x99--operator-selected-ceph-artifacts-and-m2b-completion)
delivers that policy for the minimal pool phase: each setter retains its intent
before the effect and its completion after readback, keyed by the numeric pool
ID, and a completed setting whose live value differs refuses `pool-identity`
without writing
(`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py:2874-2889`);
[storage](../storage.md) requires it for every pool setter. **Remaining:** no
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

### B592

`specs/cli.md:531-535` still says "the one failed block" is retried and other
ready blocks wait; D144 and D146 replaced that rule, and the code and
`specs/state-reconciliation.md:865-874` agree with each other
(`internal/reconciliation/lifecycle/scheduler.go:165-167`). **Impact:** stale
contract text. **Smallest fix:** reword the CLI spec to the retry rule. **Exit
evidence:** the reworded text and `make docs-check`.

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
blocks read unknown. **Impact:** safe, but it lowers the usefulness of `diff` for
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
apply currently refuses even when all original bytes are restored. [D155](#decisions)
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

### B622

The agent-install projection overwrites an authored
`dns-resolver.config.server` when DNS servers are selected
(`internal/containercluster/agentinstall/projection.go:308-323`), so an
authored resolver list is dropped without a refusal. Found while planning M2c;
parked because no M2 slice reads it. **Outcome:** an authored resolver list is
kept, or the conflicting declaration refuses naming both sources and the next
step. **Exit evidence:** a projection test over an authored `server` list with
and without selected DNS servers.

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

A wrong `ceph.ibm.callHome` for the selected build is refused at the cluster's
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
the audit (`adapter-diagnostics-5`). **Outcome:** [storage](../storage.md)
states the limit (admission is per context, and serializing concurrent applies
to one host is the operator's job), or a host-scoped reservation is held from
admission until the Machine blocks settle. **Why parked:** no spec requires more
today, the window is short, and a reservation is new durable cross-context
state. It joins M2 if pulled in. **Requires:** none. **Definition:**
`Needs definition`. **Exit evidence:** the spec sentence, or a two-context
admission test that refuses the second apply.

### B659

An apply that stopped after enrollment, with some host carrying daemons and
the cluster not ready, refuses `partial-cluster` on every later apply. Found by
the audit (`ceph-bootstrap-licensing-5`); its license-specific trigger is
refuted. **Outcome:** such an apply continues from per-host enrollment receipts
and service attempt and complete markers in `ceph_core.apply_tunnel_cluster`
and the recovery stages. **Why parked:** it needs a new durable recovery
protocol, and [the native API](../storage-native-api.md) declares it out of
scope today. **Requires:** an owner decision on the protocol. **Definition:**
`Needs definition`. **Exit evidence:** a continuation test from each recorded
enrollment state, and a test that an unrecorded state still refuses.

### B660

When the FIPS core profile is implemented, the route probe's synthetic keys
must follow the cluster key's type (RSA or ECDSA), and a FIPS-mode host with an
ed25519 cluster key must refuse with a named key-type reason before
preparation. Found by the audit (`ceph-ssh-runtime-1`). **Outcome:** the probe
follows the cluster key's type, and the native fact (the 20.2.2 cephadm SSH
module's key handling) is recorded in the
[native SSH knowledge](../../.agents/knowledge/ibm-ceph-native-ssh.md).
**Why parked:** declared FIPS is refused at planning, and changing the probe's
key contract belongs to that profile. **Assigned:** M2c lane x61-ceph-fips on
2026-10-19 ([D166](#decisions)), which generates ECDSA synthetic probe keys on
FIPS hosts. **Requires:** the FIPS core profile.
**Definition:** `Candidate`. **Exit evidence:** a probe test per key type under
a FIPS-mode fixture, and the named refusal for the ed25519 case.

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

### B662

An intent-marked OSD device that holds LVM metadata without a complete tag
set for this operation's cluster is neither adopted nor cleared. Found by the
audit (`ceph-services-dashboard-5`). **Outcome:** it refuses with a named
`lifecycle.unknown` token `osd-partial`, whose diagnostic says an interrupted
OSD creation and a foreign write cannot be told apart and whose remedy names
the supported recovery; optionally a bounded owned clear for a device that
carries this operation's intent and only an untagged `ceph-volume` volume group
and logical volume; the RHEL 9 LVM devices-file risk is checked live. **Why
parked:** a real remedy needs an owned-zap design. **Requires:** an owner
decision on the destructive path. **Definition:** `Needs definition`. **Exit
evidence:** refusal tests per partial-metadata shape and, if the clear is
chosen, its removal proof.

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
([D166](#decisions)), which takes the key from the declared mirror under its
trust bundle. **Requires:** none; D166 answered the owner decision. **Definition:**
`Needs definition`. **Exit evidence:** a package-source test with an authored
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
`examples/infra-components/lab-services` asserts the apply plan's block set with
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
`ceph.ibm.callHome: disabled` today refuses `call-home-unqualified` instead of
passing the flag. Found by the D163 sweep. **Outcome:** the flag is passed,
and the [storage API](../api/storage.md) states it. **Why parked:** the fact
comes from the artifacts and not from a version label, so it is not a D163
removal, and it is a feature gap outside a defect slice. **Requires:** none.
**Definition:** `Specified`. **Exit evidence:** a seam test on a synthetic build
that declares the option, and the existing refusal kept for a build that does
not.

### B676

`spec.dependencyVersions.libvirt` admits a NEVRA pin
(`api/v1alpha1/environment.go`), while the controller stage refuses any pin
(`internal/controller/clients/capability.go`). Found by the D163 sweep
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

### B680

[D163](#decisions) removed the stable-only screen on exact CLI tool pins, so the
OpenShift installer and clients of a release candidate download
(`internal/controller/tools.go`). Whether the OpenShift container-cluster
admission of `distribution.release.version` accepts such an exact publisher
release is unchecked: X103 reached only the tool pins and the download. Found
by X103 (D163 follow-up). **Outcome:** admission accepts an exact publisher
release such as a release candidate, and any stable-only screen found in
`internal/controller/tools.go` or the container-cluster admission is removed.
**Why parked:** an unconfirmed edge outside the five X103 items. **Requires:**
none. **Definition:** `Specified`. **Exit evidence:** an admission test with a
`4.22.0-rc.1` release, mutation-checked, and `make docs-check`.

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
prometheus service (`internal/storage/observability.go`); cephadm enables the
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
(`internal/storage/observability.go`); the
[native records](../../specs/storage-native-api.md#observability-and-management-gateway-records)
say "Parked by D160 (OD26)". Without a binding, the gateway reports only the
cephadm-signed leaf's digest, and upstream certmgr documents no root CA
retrieval and never renews user-provided certificates. See the
[knowledge](../../.agents/knowledge/ibm-ceph-monitoring.md).

**Outcome:** these fields are realized, or each is recorded as refused, and a
cephadm-signed endpoint names its root CA. **Why parked:** D160 (OD26 (a)); no
documented native contract. **Requires:** none. **Definition:** `Needs
definition`. **Exit evidence:** admission tests per field and a live CA-chain
check under B484.

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
| M1h, M5a, M4a, M1i | 2026-09-28 | Became [B72](delivered.md#x38--the-real-host-run-of-2026-10-05), [B73](m1.md#b73), [B61](m3.md#b61) and [B71](m3.md#b71); these M-letter IDs are no longer issued. |
| N1 to N5, L2, L4, L5, L6 | 2026-09-28 | Became items whose Alias cells name them; L1 and L3 never existed. |
| K01 to K73 | 2026-09-28 | The audit plan's cluster tokens, defined nowhere in the repository; the grouping stays in Git history. |
| Phase 0 to Phase 4 | 2026-09-28 | The audit plan's ordering; slices carry order now. |
| M1h's (D1) to (D6) | 2026-09-28 | Capability labels that collided with the decision log; they became prose. |
| D13, D15 | never issued | Recorded so nobody looks for them. |
| B497 | 2026-10-12 | Answered by [D143](#decisions): M2b's acceptance is an ordinary staged apply recorded as an owner-accepted ledger row ([B476](delivered.md#x94--post-destroy-cluster-inspection)), and there is no runtime registry to promote. |
| B498 | 2026-10-12 | Answered by [D143](#decisions): converge. [X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply) retires the second executable and its engine (B513, B512, B518, B515). |
| B501 | 2026-10-12 | Folded into B513 ([X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply)) under [D143](#decisions), which removes the three allow-list edges. |
| B502 | 2026-10-12 | Moot under [D143](#decisions): no fixture state exists, and a leftover anchor refuses at index read with a named remedy (B513, [X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply)). |
| B505 | 2026-10-12 | Moot under [D143](#decisions): B512 ([X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply)) keeps a failed resolution's adapter output in its attempt file. |
| B507 | 2026-10-12 | Moot under [D143](#decisions): the example's `acceptance.py` and `test_acceptance.py` are deleted by B513 ([X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply)). |
| B227 | 2026-10-14 | Moot under [D152](#decisions): X89 deleted the execution bundle and its reprojection; every command stages the automation it runs. |
| B231 | 2026-10-14 | Moot under [D152](#decisions): X89 deleted `setup --purge-old-bundles` and the kept bundle's resolutions. |
| B238 | 2026-10-14 | Moot under [D152](#decisions): X89 deleted the setup bound over resolutions and the purge that reported them. |
| B263 | 2026-10-14 | Moot under [D152](#decisions): X89 deleted the retained bundle and its fresh-resolution fallback. |
| B335 | 2026-10-14 | Moot under [D152](#decisions): X89 deleted the setup resolution it would have exercised; the host's dnf path on RHEL awaits the owner's run in X90's acceptance ([B560](#b560)). |
| B429 | 2026-10-14 | Moot under [D152](#decisions): X89 deleted the foundation guards, the native resolver and its Go-run helpers. |
| B527 | 2026-10-14 | Moot under [D152](#decisions): X89 deleted the approved bundle; each command stages its own automation, so no setup projects it. |
| B528 | 2026-10-14 | Moot under [D152](#decisions): X89 deleted the execution bundles, retained resolutions and `setup --purge-old-bundles` it concerned. |
| B542 | 2026-10-14 | Moot under [D152](#decisions): X89 deleted the Fedora native plan and its libdnf5 solver; the host's dnf installs controller packages. |
| B574 | 2026-10-16 | [D155](#decisions) retains records-only settlement; comparing the current build's plan is declined for now. No implementation remains. |
| B500 | 2026-10-16 | Superseded by [D157](#decisions) and [B615](delivered.md#b615): remove reviewed release pins instead of consolidating them into per-language release profiles. |

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
- **D40** (2026-09-30, accepted by the owner from the session's recommendations): B44: only the remaining bundle-area limit is fixed; the layer is parked (B165), which [X88](delivered.md#x88--controller-setup-v2-cut-over) closed under D152's clause 7.
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
- **D60** (2026-10-05, the owner): the real physical-BMC test follows M1, which closes without physical installation, and B73 to B78 stay in M4, B74 to B77 outside any planned slice. On the owner's explicit request of 2026-10-05, X45, the slice the session's plan called XB, runs out of sequence right after X43 and X24, in the same digest window, and delivers B73's repair and B305, the Redfish client on the owner's physical BMC. A run on a disposable RHEL 9.8 controller, the lab-baremetal rehearsal and one physical server with a reduced first-test profile, run from a separate RHEL 9.8 controller (D109) and recorded as an observation, follow it.
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
- **D90** (2026-10-05, accepted by the owner from the session's recommendations): B292: M1 ships the remedies and the corrected spec sentence for a 16-area bound held by client areas; their retirement by `setup --purge-old-bundles` is parked as B322. D152's clause 8, pending the owner's review, supersedes it where it bounds execution areas: [X90](delivered.md#x90--controller-setup-v2-retention-and-offline-sources) retires a tool area no binding names and closed B322.
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
- **D104** (2026-10-05, accepted by the owner from the session's recommendations): B305 and B319: on the physical BMC under test, under `established` trust, the client reads `VerifyCertificate` and `HttpsTransferCertVerification` before the insert and refuses private delivery when either reads false, the operator importing the CA out of band, within B73's repair; the manager SecurityService root-CA import follows as B319, before the first real-hardware row. D121 (2026-10-07) amends which flag the read-back counts.
- **D105** (2026-10-05, accepted by the owner from the session's recommendations): the parked B96 to B107, B122, B161 to B167, B171, B180, B181, B193, B194, B196, B197, B199, B226, B227, B229 to B232, B235, B238, B243, B245, B247, B248, B256, B258, B261 to B263, B266 to B269, B273 and B274 stay parked.
- **D106** (2026-10-05, accepted by the owner from the session's recommendations): B288: a RHEL controller accepts `lorax` and `xorriso` the operator installed from the host's own entitled, vendor-signed repositories, proved by presence like any root; an entitled-source adapter is parked as B330. D152's clause 6, pending the owner's review, supersedes it: [X88](delivered.md#x88--controller-setup-v2-cut-over) installs both through the host's dnf and closed B330.
- **D107** (2026-10-05, accepted by the owner from the session's recommendations): B292 and B297: the execution foundation keeps its byte-exact guard at every launch and gains a named check, a refusal naming package, build and file, a documented hold procedure and a regeneration tool in B292; in B297, within X43's window, setup qualifies vendor-signed glibc and libgcc within the qualified minor against the RPM database and records the proved digests as the receipt's requirement. D152's clause 1, pending the owner's review, supersedes it: [X88](delivered.md#x88--controller-setup-v2-cut-over) dropped the byte guard from every launch, and X89 deletes its code.
- **D108** (2026-10-05, accepted by the owner from the session's recommendations): B292: on a FIPS-mode controller host, Bootwright's runtime brings its own cryptography, which the docs state and preflight reports beside the host's FIPS mode; a FIPS-qualified runtime is parked as B331.
- **D109** (2026-10-05, accepted by the owner from the session's recommendations): the real-hardware test runs this build on a separate RHEL 9.8 controller in a subnet the BMC network reaches on the artifact ports, touching nothing on the owner's bastion; B280's host guidance says so. A test profile grants no authority over any other host.
- **D110** (2026-10-05, accepted by the owner from the session's recommendations): B301 and B73: the operator owns the controller host's firewall, and the docs list each listener's ports by source with firewalld commands; a per-socket readiness check in preflight and plan is B318, before B78.
- **D111** (2026-10-05, accepted by the owner from the session's recommendations): B282: operator-named paths are opened by a bounded helper under the invoking credentials, which passes the descriptors to root, and root keeps every proof.
- **D112** (2026-10-05, accepted by the owner from the session's recommendations): B283: the machines spec documents what `auth.operatorIdentity` does today; running its client under the invoking account is parked as B332, after B282.
- **D113** (2026-10-05, accepted by the owner from the session's recommendations): B305: generated serving certificates stay P-256; B305 records the physical BMC under test's RSA premise (it has fetched only from an RSA-2048 self-signed server, so whether it accepts a P-256 serving certificate is unproved) with its check and, as the workaround, a `contextStore` RSA-2048 certificate set with `secret set`; a `keyType` for generation follows only if that check fails. No hardware certificate-compatibility result is recorded here.
- **D114** (2026-10-05, accepted by the owner from the session's recommendations): B299: a DNSServer keeps its wildcard bind default, the new pre-start check names a colliding socket, and the examples and docs declare explicit binds; a derived default is parked as B333.
- **D115** (2026-10-05, accepted by the owner from the session's recommendations): B301: the examples bind their emulated BMCs to loopback, and the docs state that a privileged or cleartext-credential listener binds loopback or a host-only address unless its network is trusted; emulator TLS, a refusal of a non-loopback bind address and a confined SELinux type are parked as B334.
- **D116** (2026-10-06, the owner): every safety or defect follow-up a slice finds in M1's journeys joins M1, folded into the next fitting slice or a final sweep slice before the closing run (D59); enabling and wording follow-ups stay parked. This amends D56 and D48 for follow-ups found from X39 on: a safety or defect follow-up found in M1's journeys joins M1 on the planned sweep slice X46, or on an earlier planned slice whose lane already owns its files, with the Alias `new, <date> (<slice>); attached to M1 on <date> (D116)`; an enabling, wording, test-depth or knowledge follow-up stays parked under D48. M1 takes X39's parked safety and defect follow-ups B339, B340, B342 to B344, B347, B349 to B351 and B356 on X46.
- **D117** (2026-10-06, the owner accepted the session's recommendation): B357: D65 widens, so a confirmed trust write that takes over an endpoint also drops the record of a still-declared Machine that no longer uses the context's SSH trust, the controller Machine's included, as a `remove` row in every lifecycle state, with no input edit.
- **D118** (2026-10-06, the owner accepted the session's recommendation): B372: a file-input `secret set` replacement, `secret delete` and `secret encryption rotate` confirm before the lease and revalidate under it, as standard input does since D73, refusing with `secret.store.conflict` and writing nothing when the store changed meanwhile.
- **D119** (2026-10-07, the owner accepted the session's recommendation): B440 joins M1 on X46: a destroy over an unknown apply resolves each block with the removal's own check, which proves the object is this context's and can be taken back, so a drifted machine is removed with its ownership proved; the state-reconciliation spec records what that resolution proves.
- **D120** (2026-10-07, the owner): the emulated controller serves TLS: the libvirt emulator unit serves HTTPS with a generated serving certificate the InfraProvider's emulated-BMC defaults name, the lab-baremetal physical copy names its authority in `trustBundleRef` and verifies the leg, so D103's verified controller leg holds for the rehearsal; B334 splits: its TLS half joins M4 as B446 on a new planned slice X47, the loopback bind stays with X44 (D115) and confinement stays parked.
- **D121** (2026-10-07, accepted by the owner from the session's recommendation): D104 is amended: where the manager publishes `HttpsTransferCertVerification` that switch alone decides, and the device's `VerifyCertificate` counts only on firmware whose manager publishes no such switch; a hardware capture of the device flag stays an operator item; this is B447 on X47 together with the follow-up that `disable-verification` is a silent no-op on the iBMC.
- **D122** (2026-10-07, accepted by the owner from the session's recommendation): after a failed physical apply the private subtree stays published until the next apply or a destroy clears it; the managed-OS spec says so.
- **D123** (2026-10-08, the owner accepted the session's recommendation): B440: D119's destroy resolution must not lose produced material: a destroy that resolves an unknown apply block first runs that block's apply observation (the capability's read-only Observe) and, when it proves the operation completed, captures the produced material into custody exactly as a completed apply's finalization does (for example the agent-install admin kubeconfig) before any inverse runs; a drifted machine whose observation does not prove completion is still resolved by the removal's ownership proof as D119 says; the state-reconciliation spec states the capture.
- **D124** (2026-10-08, the owner accepted the session's recommendation): B440: an installer's kept administrator kubeconfig is never deleted while custody holds none for that cluster: when a removal (the agent-install media block's inverse, or any inverse that deletes the installer's work area) would delete the kept kubeconfig and custody holds no kubeconfig for that ContainerCluster, it first moves the kept copy into custody as produced material marked unproved access, its completion never proved, under the custody path and bounds a completed apply's capture uses, and only then removes the work area, so a crash between the two leaves the copy in one durable place or both and never in neither; `bootwright cluster kubeconfig` exports it with a warning that its access was not proved; deleting the context removes it as it removes any custody entry; this holds whatever the nodes' power state, so the destroy's `Stop first` path loses nothing.
- **D125** (2026-10-09, the owner): bounded concurrent execution is required for M1 to be implemented. B303 and B304 return from M3 to M1 with their IDs, and B466 adds separate shared package-tree publication and production concurrency, with contract definition required before its implementation. X48 is planned after X46 and before X44; D59's closing gate must prove independent service and machine work, including three same-profile installations, actually overlapping, with replay, cleanup and safe destroy. Prior serial acceptance does not discharge this gate. This request authorizes review and backlog planning; implementation remains subject to the scope rules.
- **D126** (2026-10-09, the owner): finish X44's remaining implementation while deferring the Go toolchain upgrade B470 requires. B470 and the failing vulnerability gate remain unmet; the deferral changes no exit gate or security requirement and declares neither X44 nor M1 complete.
- **D127** (2026-10-09, the owner): rebase M1 on current main and implement X48 there, so the three-node RHEL fixture and future examples use all dependency-ready parallelism within the existing bound. B466 realizes shared profile publication under implementation/request `os-package-tree-v1` and evidence `os-package-tree-evidence-v1`, advances installation requests to `os-install-anaconda-v8`, and records effect, replay, inverse and compatibility boundaries in Managed OS. This also names X48 as the slice retiring libvirt's provider/Machine name collision refusal through separate durable namespaces. B470's existing upgrade deferral and every M1 exit gate remain unchanged.
- **D128** (2026-10-10, the owner): keep M1's scope, sequence and completion gates unchanged. M2 is full managed IBM Ceph 9.9.x provisioning on libvirt+sushy and physical bare metal/BMC, with every provider provisioning feature qualified through B472, B80 and B81; M3 is full OpenShift cluster provisioning on those two provider paths, with complete coverage defined by B473. Move the former M4 Ceph scope to M2, former M5 first add-ons to M4, former M6 OpenShift Virtualization to M5, former M7 ACM/Argo CD/GitLab to M6, and former M2 standalone native rendering to the final M7. Move B70's shared physical erase gate from M3 to M2; managed storage projection and lifecycle qualification no longer wait for standalone rendering or OpenShift. B, X and prior D identifiers remain fixed, and dated history retains its original milestone numbers. Each scope transferred as a whole keeps its items and delivery evidence.

- **D129** (2026-10-11, the owner): define M2 before implementation and deliver
  its Ceph journey in order: minimal three-node libvirt+sushy, full virtual
  provider coverage, full physical BMC coverage, then two-data-site stretch
  with a third-site monitor-only tiebreaker. Split B80/B81's combined outcomes
  into independently qualified feature families and acceptance checkpoints;
  B472 owns the release/coverage definition and B474 its executable projection.
  Plan X52 to X66 alongside retained X47, with the [M2 page](m2.md) owning
  their dependencies. B82 is parked without a concrete consumer; B346 joins
  M2 for its FIPS access profile. The [provider matrix](../storage-coverage.md)
  records native restrictions and alternate profiles; full coverage never
  claims unsupported combinations. B491 must settle the online-cleanup versus
  machine-quiescence contract, and B70 remains blocked on physical erase
  evidence. This is a specs-only definition request: M1 gates remain, no
  implementation slice is activated and no host operation is authorized.

- **D130** (2026-10-11, the owner): M2 supports every cluster size IBM supports
  for the qualified release and deployment mode. Features follow the provider's
  requirements, including per-site/per-role capacity, topology and service
  compatibility. Acceptance profiles are examples, not product limits; include
  larger qualifying profiles for supported features that require them.
  B472 records the support ranges and requirements, B474/B79/B490 qualify
  admission and internal bounds, and B487/B488 cover the service-specific
  stretch profiles.
  The definition remains planning-only and changes no implementation authority.

- **D131** (2026-10-11, the owner): name the M2 submilestones for future
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

- **D132** (2026-10-11, the owner): include the complete Ceph API and operator
  authoring review in [M2a](m2.md#m2a). Preserve useful existing abstractions
  and native declarative formats where they reduce operator learning; expose
  imperative-only storage resources as declarative intent. Make small,
  justified corrections, but obtain owner approval with current/proposed
  examples before adopting substantial API changes. The
  [review](../storage-api-review.md) records findings and pending proposals;
  this decision does not approve those proposals, authorize implementation,
  or declare M2a complete. B494 records the review's bounded admission defects.

- **D133** (2026-10-11, the owner): approve the two substantial M2a API
  proposals reviewed under D132. P1 consolidates daemon configuration and
  placement into complete native Ceph service documents, with Bootwright
  retaining proved Machine/site ownership, release pins and protected secret
  bindings. P2 keeps pools/filesystems and adds separately owned declarative
  resources for imperative-only objects; NFS exports and SMB resources retain
  their native formats. The [native service](../storage-native-api.md) and
  [resource](../storage-resources.md) specs own the target and migration;
  B474 delivers the common schema path and each family item its qualified
  extensions. This approves the breaking v1alpha1 authoring target, not a
  version change, runtime implementation, sensitive-output command or host
  operation. M1 and implementation promotion gates remain unchanged.

- **D134** (2026-10-11, the owner): accept M2a's definition baseline and
  explicitly authorize M2b implementation, its generic three-node example,
  a locally ignored machine-specific copy, static execution review and an
  operator setup/provision/test/cleanup runbook. M1's outstanding checks and
  owner acceptance remain promotion gates. No privileged operator test or
  push is authorized. Test runs resolve the latest provider release available
  at setup and pin its exact artifacts for the whole run. Keep verified native
  Ceph SSH mandatory: P3's proposed provider-default exception is rejected;
  bootstrap remains gated until the entitled IBM artifact proves verification.

- **D135** (2026-10-11, the owner): approve P4's verified-tunnel design and
  qualification. Add `clusterSSH.transport: verified-tunnel` while retaining
  required verification: an outer pinned OpenSSH connection forwards only to
  the same authenticated host's loopback SSH endpoint, with no direct fallback.
  Protected MGR mounts, staged bootstrap, server forwarding restrictions and
  restart/failover/recovery are part of qualification. Approval changes the
  target API and trust boundary; it grants no provider-default exception,
  operator-evidence bypass or automatic artifact promotion. B472/B79/B80 own
  the remaining exact-artifact and runtime evidence.

- **D136** (2026-10-11, the owner): approve P5's protected RBD canary retention
  until explicit cluster teardown. The fixed fixture owns a 16 MiB image,
  retains its ID before writing/reading at most 1 MiB by ID, closes the client
  and reports `cleanup: cluster-teardown-required`. No immediate deletion by
  name is permitted; exact disposable-cluster removal owns the retained data.
  This changes the test retention contract, not public input schemas or
  ordinary cluster qualification gates.

- **D137** (2026-10-11, the owner): approve P6's optional libvirt
  `dataDisks[].media: ssd`. Explicit SSD data disks use virtio-SCSI with
  `rotation_rate="1"`, starting at `/dev/sda`; root remains `/dev/vda` and
  omission preserves the existing virtio-blk layout. Mixed profiles use
  independent target sequences. Frozen requests, domain inspection, guest
  device observation and native Ceph class must agree. This declares virtual
  guest media behavior rather than physical host performance; existing
  realizations keep their original requests and require teardown before a
  changed layout is installed. Actual guest/runtime acceptance remains open.

- **D138** (2026-10-11, the owner): clarify M2b's entitlement-driven artifact
  acquisition. After an operator imports the registry credential into the
  Entitlement's selected Secret, Bootwright resolves the provider image and
  downloads required IBM images/packages itself. Remove the manual image
  pre-pull and digest-discovery prerequisite from the example and runbook.
  Preserve immutable resolution, original credential custody, verified TLS,
  current qualification gates and the separate Red Hat media/source boundary.
  This corrects the authorized M2b flow without changing public Storage schemas
  or authorizing an agent-run privileged or entitled test.

- **D139** (2026-10-11, the owner): "every backlog to milestone M1 done, except the ones that i need to run myself the test in real bare metal and the one related to a go version that is not available in my machine". Recorded meaning: `make vulncheck`'s go1.26.8 standard-library findings (B470) no longer hold any other M1 item. An item completes when every other exit gate passes (`make check`'s other steps, `make race`, `make docs-check`, `./scripts/ansible-check` and its own tests), or awaits operator acceptance when it also has a host gate; B470 alone carries the vulnerability gate, on its own slice X67; M1 completes only after B470 and the owner's host gates. It amends D126's sentence that the deferral "declares neither X44 nor M1 complete" for X44's items and X48 to X51 (M1 itself stays incomplete), and it settles the 2026-10-11 audit's finding that X46's second pass completed under a different standard in favour of completion, so X46's second pass stays delivered. D129 to D138, recorded on the M2 branch, precede it there.
- **D140** (2026-10-11, taken by the session, pending the owner's review): `machine exec` and `cluster exec` stop at the payload, as `cluster oc` and `cluster kubectl` already did: the first operand that is not a flag starts the remote command and every later token is payload verbatim, flag-shaped or not, so a remote command's own `--context` or `--name` can never retarget the session. Bootwright flags go before the command; `--` is needed only when the payload's first token is flag-shaped, and a required Bootwright flag written after the payload is refused with where flags go. This replaces the earlier rule under which a recognized flag after the first operand still took effect; `TestPayloadParsing`, `TestPayloadTailNeverRetargetsTheSession` and `TestAFlagWrittenAfterThePayloadIsRefusedWithWhereFlagsGo` prove it. The owner accepted it on 2026-10-14 (D148).
- **D141** (2026-10-12, taken by the session, pending the owner's review): vendor and device names were removed from D60, D104, D109 and D113 and the X45 record on 2026-10-11 for confidentiality; their operative content, restored here, is unchanged by the redaction. The constraints (D109's bastion), the observed firmware behaviors, the check procedures and the RSA-2048 workaround (D113) that the redaction dropped are written back in neutral wording ("the physical BMC under test"), in those decisions, in the X45 record and in the [Redfish knowledge page](../../.agents/knowledge/redfish-physical-bmc.md), and no vendor, model, firmware, serial or host name returns.
- **D142** (2026-10-12, the owner): before 1.x no backward compatibility is wanted. An incomplete operation is bound to the blocks it froze, not to the build that froze them: another build, meaning other automation, another version or commit, or any build stamped `modified`, continues it exactly when it freezes the same blocks, every frozen member except the words they are presented in, and the execution closure check stays. No converter or older reader is added for any record or request version. B504 joins M1 as an exception to D48 and is delivered by X69, an out-of-sequence enabling slice implemented on the M2 branch at the owner's request.
- **D143** (2026-10-12, the owner; the clauses after the owner's words are taken by the session, pending the owner's review): "The only binary that should implement all features of bootwright is bootwright itself; the operator declares desired state and runs bootwright apply, with --stage for phases; validation through machine exec / cluster exec; a bootwright command prints the dashboard access info", and, on the M2 branch, "Yes. Work and apply changes on branch M2". This settles [B498](#retired): converge, so the `bootwright-ceph-qualification` executable and its fixture engine are retired. It settles [B497](#retired): M2b's acceptance is an ordinary staged `bootwright apply` over the declared StorageCluster, StoragePool and StorageClient, recorded as an owner-accepted ledger row ([B476](delivered.md#x94--post-destroy-cluster-inspection)); no fixture evidence closes an item, and there is no runtime registry to promote. [X70](delivered.md#x70--ceph-through-the-ordinary-staged-apply) delivers it. Taken by the session, pending the owner's review: (1) each build supports one reviewed, code-pinned release: IBM Storage Ceph 9.9.1.2, image `cp.icr.io/cp/ibm-ceph/ceph-9-rhel9` tag `v9.9.1-25074`, packages `20.2.1-405.el9cp`, cephadm-ansible `5.0.3-1`; a different declared release, repository or tag refuses at admission naming the pinned one, and a newer release needs a reviewed pin bump. This amends D134's "latest provider release available at setup". (2) Apply resolves the pinned tag through the Entitlement's registry Secret to its linux/amd64 child manifest digest in a block that completes before any native effect and is frozen for continuation; an authored `sha256:` digest is admitted only when it equals that resolution; the digest is not code-pinned until B472's lock adds it. (3) `verified-tunnel` is the only supported transport; `native` stays refused (D134, D135). (4) The empty qualification registry and its refusal are removed by B512. Pinned-release admission, verified host keys, data-loss authorization and the frozen plan still protect apply. (5) Every guest proof the fixture made moves into ordinary per-host blocks (B518, B499) and is not dropped. (6) A managed cluster whose hosts are all Bootwright-owned virtual Machines that the same destroy removes is retired with them, with no native call (B515); removal while hosts survive stays [B491](m2.md#b491). (7) The dashboard is enabled after readiness, with user `admin`, a generated password in produced custody revealed only by `bootwright cluster info --secrets --name`, and Ceph's self-signed certificate whose SHA-256 `cluster info` prints. Bootwright never rotates it, and a password changed outside Bootwright refuses as drift. This is the public interface B489 left for owner approval, narrowed to that credential; client export stays B489, and certificates, roles, SSO and monitoring stay B479. (8) B476's 1 MiB RBD test is run by the operator through `bootwright cluster exec` with the bootstrap node's administrator keyring, keeping D136's retention (a retained 16 MiB image, no deletion by name, removed by teardown); Bootwright's canary and StorageClient-material harness retire. (9) `cluster list` and `cluster info` become available for every kind; `cluster rsh` and `cluster exec` connect for managed Ceph (node by name or FQDN, default first in canonical order, argv verbatim, exit 255 for every refusal before the session); ContainerCluster sessions stay [B101](#b101). (10) The owner's request is the dated waiver of M1, X52 and X53 for X70's implementation, and X70 joins D134's exception to the WIP limit; promotion still requires them. (11) A context holding a fixture anchor is retired with the build that prepared it; a later build refuses it, and its only fallback is `bootwright context delete --name <ctx> --purge --allow-orphans`, which abandons that context's Machines, disks and reserved bindings.
- **D144** (2026-10-13, the owner): "check why those steps for vms were not performed in parallel. remember that any step that could be ran in parallel must be done as that, to optimize provision time". Failed blocks are retried concurrently under the admission rules of a first attempt (dependencies `done`, stage selected, once per invocation, exclusive resources free, the engine bound), beside every startable block; unproved effects are still resolved first with nothing beside them, and a selection that admits no failed block still refuses `lifecycle.stage`. This replaces the rule that a retry runs alone. [X71](delivered.md#x71--concurrent-retries) delivers it as B523.
- **D145** (2026-10-13, taken by the session, pending the owner's review): the owner proposed to continue an operation under a rebuilt binary by comparing declared versions in place of the content digest, on the premise that the content digest hashes Ansible code. It does not: it is the implementation's contract identity, computed from its identity, its request and evidence versions and its effect pins, and no capability package reads the automation. A fix confined to automation, or to Go code that moves no version or pin, such as X72, X73, X75 and X76, therefore moves the automation digest and no content digest, request byte or version, so D142's rule already continues it once `setup` has projected the fix; the 35-minute destroy of the M2b run was advised, not required. Dropping the content digest from the rule would let a pin change (a Ceph image, the cephadm binary, a package set, a host path) continue an operation half done under the old pin. So: (1) D142's rule stands: a block continues under another build exactly when it freezes the same blocks, content digest included, and nothing here narrows a refusal. (2) A contract guard replaces the human rule that an automation change that alters what an effect is must move its content digest, where a tool can check it: a golden per implementation pins its content digest beside its Go request and evidence schemas and the persisted format versions quoted in the collection's embedded plugins and roles, and changing any of them while the content digest stays fails the build and cannot be recorded (`TestEveryImplementationMovesItsContentDigestWithItsContract`). It sees versions and Go schemas, not meaning: a raw-JSON member, such as the cluster services the Ceph core and host requests carry, is fingerprinted as opaque, and an unquoted, dynamically built or unversioned format is not seen. (3) Every attempt and resolution record names the build it ran under, an optional `build` member that moves no record version. A consequence for the owner to review: the member is compatible one way only, since a build from before X74 decodes records closed and refuses one that carries `build` as "lifecycle record is malformed or unsupported", so once a build that writes `build` has worked an operation, no earlier build can continue or remove it, and the removal under the build in hand is the way out. The alternative, moving the attempt record version, would have stranded every live operation at once. (4) A destroy continuation keeps X69's rule: its removal is derived from the frozen plan and runs under the build in hand, as a fresh removal does, and its proof compares what the removal decides, the authorizations it consumes, its groups and that each block's capability reads its frozen request; it cannot compare content digests, because the removal copies them from the frozen plan. An unknown removal block is observed by the build in hand, whose closed evidence decoding refuses evidence it cannot read, and every removal attempt and resolution names the build that ran it. (5) A continuation previewed under another build names the build that registered it and the steps this build runs, and `status` says whether the build in hand is another build. The preview has no JSON form because `plan`, `apply` and `destroy` have no JSON mode, so `status --output json` carries `anotherBuild` instead. (6) No content digest, request version, record version or plan encoding moves, so the owner's live operation, registered by 91855545e, continues under a build that contains X74. [X74](delivered.md#x74--continuation-under-a-rebuilt-binary) delivers it as B525.
- **D146** (2026-10-13, the owner): "i had to run twice apply to make it work. it shouldn't be needed. check if you can fix it", after an `apply` resolved an unknown Ceph pool block as partly realized and stopped, and a second `apply` retried it and the client after it. This settles B529: a resolution is not an attempt. An invocation still observes a block at most once and never observes one its own attempt left unproved, but a block its resolution leaves `failed` is retried by that same invocation once every unproved effect is resolved, under the admission rules of any retry (stage selected, dependencies `done`, exclusive resources free, the engine bound), so its dependents start there too. A failed resolution admits further first attempts as a proved outcome does; a failed attempt, including that retry, still admits none. A fresh `destroy`'s resolutions of the apply it supersedes retry nothing. The resolution's progress row stays running into the retry and is settled `failed` when the invocation stops before the retry starts. It amends D144's "once per invocation" only in that a resolution no longer counts as the block's turn. [X82](delivered.md#x82--resolution-and-retry-in-one-invocation) delivers it as B529.
- **D147** (2026-10-14, the owner): "you can upgrade go to 1.26.9 and mark this backlog as done". This ends D126's deferral of the Go upgrade: [X67](delivered.md#x67--go-toolchain-vulnerability-gate) moves the pin from go1.26.8 to go1.26.9 and delivers B470, so `make vulncheck`, and with it `make check`, passes again and D139's separation of the vulnerability gate from M1's other items holds nothing more. It moves no other dependency; Go 1.27 stays parked as [B273](#b273).
- **D148** (2026-10-14, the owner accepted the session's recommendation): D140 stands as taken. `machine exec`, `cluster exec`, `cluster oc` and `cluster kubectl` keep the one payload rule of [the CLI contract](../cli.md#parsing-and-input-conventions): Bootwright flags go before the remote command, and every token from the first operand on is payload verbatim, so a remote command's own `--context` or `--name` never retargets the session and `ls -la` needs no `--`. The alternatives the owner declined were the earlier rule, under which a recognized flag after the first operand still took effect, and a mandatory `--` before every payload. No code or contract changes; M1 holds no open decision.
- **D149** (2026-10-14, the owner): "i upgraded my fedora to 44. bootwright should support it", after `setup` refused the upgraded controller with `controller.unsupported`. This joins B541 to M1 as an exception to D48, because M1's closing run (D59, D125) runs on that controller, and [X84](delivered.md#x84--fedora-44-controllers) delivers it: `setup`, `preflight controller` and the controller stage admit Fedora 44 beside Fedora 43, which stays admitted, and RHEL 9.8.
- **D150** (2026-10-14, the owner): "update commands bootwright machine start|stop|restart|exec to accept a comma-separated list of machines that the command will be applied in parallel to all". `--name` of those four commands is a comma-separated list under the existing list rules; one member behaves exactly as before and several run concurrently, each power run as today's single-Machine run (at most the engine's bound at once, inside one invocation scope, behind one confirmation) and each exec session non-interactive with its output lines prefixed by their Machine. `machine rsh` stays single because it opens one interactive session, and `cluster rsh` and `cluster exec` are unchanged. Taken by the session, pending the owner's review: (1) a Machine whose resolved identity is a password refuses a several-Machine `exec`, because a password prompt cannot be shared by concurrent sessions; (2) a partial power failure exits `1` with one diagnostic for each failed Machine and still presents every admitted Machine's row; (3) a several-Machine `exec` exits with the highest status any session returned and warns `access.exit` for each nonzero one. [X85](delivered.md#x85--machine-commands-across-several-machines) delivers it as B543.
- **D151** (2026-10-14, the owner): "when you finish, do the same for bootwright cluster commands", following the request that made `machine start|stop|restart|exec --name` a comma-separated list applied in parallel (D150). The owner answered two questions. For `cluster exec`: "BOTH `--name` and `--node` take comma lists. `--name a,b` runs on every listed cluster and `--node n1,n2` on several nodes. Each listed cluster runs on its default first node, or on the `--node` members it declares. A `--node` member no listed cluster declares is refused. Every resolved node Machine runs in parallel, exactly like `machine exec` with several names." For `cluster info`: "`--name` takes a comma list too (a selection convenience; local read, nothing to parallelize)." `cluster rsh` stays single because it opens one interactive session, and `cluster info --secrets` still reveals exactly one cluster. Taken by the session, pending the owner's review: (1) a listed cluster that declares none of the `--node` members refuses `access.target` instead of running nothing there; (2) the session prefix is the Machine name, and Machines two nodes share run once; (3) a FQDN shared by several nodes of one cluster refuses as it does for a single cluster. [X86](delivered.md#x86--cluster-commands-across-several-clusters-and-nodes) delivers it as B546.
- **D152** (2026-10-14, the owner's request; the clauses are taken by the session, pending the owner's review): "make a full review and audit of how bootwright install/setup works. i think this is too restrict and not optimal about dependencies. for instance, i had my fedora updated from 43 to 44 and it stopped working. you actually fixed this, but i think we could review this process in a broader way. check how dependencies are defined and used. i want bootwright to require those dependencies in a general but safe way. this should work in a similar way other linux packages that requires dependencies work. other thing that is bothering me is how long it takes to run bootwright setup, to install ansible and python dependencies, even after a previous bootwright setup, with older bootwright's version. this make setup take too long (more than 3 minutes). bootwright seems to be using dedicate installation for those package. evaluate if it is really necessary or would make more sense or would be justified to used SO's package for that. how other similar community package handle that? if it makes sense to use a dedicate python installation, for instance, couldn't the bootwright setup from a newer bootwright's version reuse the previous one? ... check alternatives and impact in code and propose a plan for us to refactor this installation/setup use case, using the best alternative, considering estability and ux and community pattern/best practices. no backward compatibility needed/wanted. you can consider a new fresh installation for this new bootwright version." It carries forward the owner's rules of 2026-09-13: "bootwright should just check if dependency packages are available and only if not install; it shouldn't check for updates unless a specific min version becomes necessary", and "versioning logic for packages is over-engineering; only check the required packages exist; add version logic only when a specific version issue appears". Until the owner accepts it, D152 is a proposal and authorizes nothing; acceptance authorizes [X87 to X90](m1.md#planned-slices) on M1 as an out-of-sequence exception to D48, in the form of D149, and must carry the owner's dated sequencing choice for X88 (clause 9). The clauses: (1) The glibc and libgcc byte pin is not a security boundary and is dropped: the per-file digests, link pins, the single-link and exact-upstream rules, D107's requalification, the receipt's `foundation`, the operator guide's hold procedure, the compiled catalog and scripts/foundation-catalog. Root ownership with no group or other write stays on every executed or loaded path, extended to the interpreter's standard library and `lib-dynload` and to the re-executed binary (B548), and specs/security.md states the attacker model and the widenings W1 to W4 once. Declined: digests as a reproducibility check, the cause of the Fedora 44 refusal, since no attacker model was found in which digests of root-owned files stop an attacker who cannot already write them. Supersedes D107. (2) Admission is by family and floors and fails closed: `ID` is fedora or rhel or `ID_LIKE` names one, amd64, a qualified `/usr/bin/rpm` and `/usr/bin/dnf`, a locked-ABI interpreter installed or offered, and glibc at or above the lock's floor (2.34). A release outside the lock's tested list gets the informational line `controller.untested-release` and is never refused; a docs test ties that list to the development guide. Declined: exact (OS, release) rows, the cause of the Fedora 43 to 44 break, refusing untested releases or derivatives, and silent admission. (3) The interpreter is the OS CPython 3.12 to 3.14 at its versioned path `/usr/bin/python3.X`, with one venv per lock, ABI and interpreter path and no python-build-standalone fallback (parked as B561). Python TLS follows the host's OpenSSL and crypto-policy, since `OPENSSL_CONF=/dev/null` goes; D108 is amended: the Go binary and cryptography's bundled OpenSSL stay outside any FIPS claim, and B331 stays parked. Declined: a standalone CPython with a lock, wheels embedded in the binary, distribution ansible-core (no distribution ships 2.21) and an Execution Environment image. (4) Versions are chosen at build time in a lock the binary embeds (internal/controller/runtimelock/lock.json, format `bootwright.controller.runtime-lock-v1`), which `scripts/runtime-lock generate` writes with network, run by the owner or by CI, and which is committed; `verify` runs in a network CI workflow and `outdated` is advisory. Setup never discovers versions or checks for updates, the upgrade verb is a new build, and D10's and D20's "latest patch" means the latest at lock generation, which also meets the rules of 2026-09-13. (5) ansible-core is exactly the 2.21 minor the collection's `requires_ansible` admits, its patch pinned in the lock; a guard test requires the lock, `scripts/tools/ansible-requirements.txt` and the collection's `meta/runtime.yml` to agree. cryptography stays because ansible-core declares it; dropping it is parked as B559. (6) Native packages come from the host's dnf. Requirements are names or capabilities in one Go table, presence is `rpm -q --whatprovides` with no version logic, and before confirmation a root `dnf install --assumeno --setopt=install_weak_deps=False` preview is shown verbatim as display text bounded to 64 KiB; lines naming core packages (glibc, libgcc, kernel, systemd, openssl-libs, the selected python3.X) get an informational note, never a refusal. Install is `dnf install --assumeyes` with the same option, presence is checked again, and each provider's NEVRA and repository are recorded. The host's dnf configuration owns signers, versionlock, excludes, protected packages, mirrors, Satellite and `proxy=`; Bootwright never passes `--allowerasing`, `--nogpgcheck`, `--repofrompath`, `--enablerepo` or `--releasever`. The Ceph node's dnf4 core keeps its files and only the controller-only branches go. Declined: the frozen plan with a cache, a host-configured solver with vendor-only signers, and a protected-set refusal or `--exclude`. Supersedes D106; closes B330 and B542. (7) Automation leaves every setup identity: the embedded `ansible/` tree is extracted for each invocation into root 0700 staging, held by an inherited lock while descendants run; the automation digest and D145's re-proof are unchanged; the runtime digest hashes `runtime-v1`, the lock digest, the ABI and the interpreter path; the closure digest hashes `execution-closure-v2`, the lock digest and the ABI. An `ansible/`-only fix is build and apply. Closes B165, parked by D40, and supersedes D14's per-build area where it describes the runtime. Declined: a persistent automation directory per digest. (8) Retention runs at the end of each successful setup under the setup lock: the current runtime and the previous distinct one and their wheels stay, every other runtime is retired under a non-blocking exclusive lock that skips one in use, and the newest 8 run logs stay. apply, destroy and the controller stage rebuild this build's runtime without network when its directory is missing and every locked wheel is cached, and refuse with "run bootwright setup" only when a wheel is missing. Tool areas move under the controller's tools directory with their own bound of 16 and are retired when no binding names them (B557, closing B322). The bound of 16 on execution areas, retained sources and definitions, and `--purge-old-bundles` go; this supersedes D90 where it bounds execution areas. Declined: keeping only the current runtime, which would strand live operations offline after a lock change, and unbounded rebuild without retention. (9) The cut-over is a fresh install. A setup-owned subtree `/var/lib/bootwright/setup/` stays root 0700, carries the format `setup-state-v1`, lies outside the registry descriptor and heals itself: setup discards another build's format after confirmation, and launches refuse with "run bootwright setup". The controller record moves from version 2 to 3 and the registry from 5 to 6, and a version 5 store refuses with "/var/lib/bootwright holds a store an earlier Bootwright build wrote (registry version 5), which this build does not read or convert" and the remedy "with the build that wrote it, destroy each environment (bootwright destroy --context <name>) and delete each context (bootwright context delete --name <name>); then, as root, remove /var/lib/bootwright and /var/lib/bootwright-staging and run bootwright setup with this build. Never remove /var/lib/bootwright while services it manages run." The owner retires m2b-ceph and the lab contexts with the old build before the first X88 build runs on the owner's host. X87 integrates on acceptance; X88 integrates only after the owner records, dated, either (a) M1's closing run (D59, D125) and B476's run complete on the current build, which the session recommends, or (b) a waiver that runs the closing validation on the X88 build. **Open: the owner's choice.** Declined: a separate world-readable setup root, which contradicts the root 0700 store rule (a readable index copy is parked as owner decision B558), and moving bindings out of the controller record. (10) `--wheel-mirror` (a `file://` directory or an `https://` base) is untrusted transport bound by the lock's sha256; `setup --download-only --dest-dir DIR` runs unprivileged and writes the wheels, the installer and the lock; `--dry-run --output json` lists every locked file's URL and sha256. The default origin is only `files.pythonhosted.org`, native packages use dnf's own configuration, and the admitted context-free `HTTPS_PROXY` and `NO_PROXY` route is forwarded to dnf as `https_proxy` and `no_proxy`, where dnf.conf's `proxy=` wins; credentials and `ALL_PROXY` stay refused. (11) nmstate leaves the baseline, since nothing consumes it; podman stays, because the container-runtime capability is mandatory. (12) The rpm read lock is not held: each launch holds a shared lock on its runtime directory for the whole Ansible block, so retirement never removes a runtime in use and the operator's dnf is never blocked; a same-minor standard-library update during a run is an accepted risk that setup's import probe repairs. (13) Pre-existing defects: the re-executed binary has no owner or mode check, which X87 fixes as B548 in its own commit; controller-record refusals name no remedy, and the registry version 6 and setup-subtree refusals that replace them each name one; the spec's missing native re-solve, the staged `other` metadata, the stale python3.13 defaults and B542 are moot with the code X89 deletes; the operator guide's Fedora 43 examples are rewritten in X89. Parked by D152: B558 to B563. The session integrated [X87](delivered.md#x87--controller-setup-v2-foundations) on 2026-10-14 ahead of this acceptance, as its workflow directed: it is additive and unwired except B548. On 2026-10-14 the owner deferred clause 9's sequencing choice ("implement whatever you can; I will test when I can run"), so the session integrated [X88](delivered.md#x88--controller-setup-v2-cut-over) on local main without a push; the choice and the owner's review of D152 stay open, and the owner's fresh-install acceptance of B555 waits until the owner can run it.
- **D153** (2026-10-14, the owner): "today bootwright must accept only this scenario: non-existent real state ... half-provisioned/setup ... real state already equals desired state ... bootwright could have an command: bootwright diff ... do not implement this support yet, just make code, logic and architecture ready for this in the future ... reconciliation must be predictable and safe. target environment can never be broken with bootwright apply". Taken by the session, pending the owner's review: (1) `apply` admits exactly three scenarios: a fresh apply where the context's records show no operation or a completed destroy, a continuation of an incomplete apply over byte-identical input, and an idempotent settle of a completed apply over unchanged input. Changed input over a completed apply keeps refusing before any effect and names what changed, with destroy then apply as the remedy; `TestApplyAdmitsOnlyTheThreeScenarios` pins the placement. (2) `bootwright diff` is read-only: it compares the context's selected desired state with what its apply realized, object by object, reads durable records only, or with `--observe` each realized block's target through that block's read-only observation, writes no record, takes no lock beyond the shared read lock and exits 0 whenever it renders. (3) Every further scenario is added one per named slice by the recipe in the design: a decision, a spec row and a `stateMachineRows` case, the prerequisite items, and one branch in the changed-input path, all in one commit; none is implemented here, and this slice builds only seams with a present consumer (the comparison, the read-only guard, the retained-revision reader). (4) The settled scenario means "the records prove the frozen plan done over unchanged input": a settled apply attests its own records and reaches no host. Drift that `diff --observe` shows is reported, never repaired. [X91](delivered.md#x91--read-only-diff-and-the-delta-seam) delivers it; the audit's blocking findings [B567](#b567) and [B568](#b568) stay parked and recommended as a dedicated safety slice before any further `destroy` on a shared libvirt host. Needs the owner's decision: whether a settle also requires the current build to freeze the same plan ([B574](#retired); the recommendation is not now). Needs the owner's decision: whether input restored byte for byte after an edit settles a completed apply ([B598](#b598)). Needs the owner's decision: the libvirt redefine semantics and what machine evidence proves ([B576](#b576)). Needs the owner's decision: whether Ceph pool `size` and `min_size` drift is refused or reported ([B587](#b587)). Needs the owner's decision: the context incarnation identity design ([B568](#b568)).
- **D154** (2026-10-15, the owner): "mark all backlogs that we validated so far
  as done; save this fix as a new backlog; we will handle it and §9 in near
  future", and, asked, accepted D152 with clause 9's option (b) and the
  closing run's B339 observation as met. The owner accepts the ledger rows of
  [X93](delivered.md#x93--the-owner-run-of-2026-10-14) for B19, B20, B270, B276, B277,
  B339, B549 and B555, which leave M1; accepts D152 as written, whose clause 9
  is (b): the closing validation ran on the X88 build; and records the
  reboot defect as [B608](delivered.md#b608) on M1, where it blocks B466. M1 stays
  in progress, and B476's run (M2b) follows later. D156 subsequently closes
  the remaining M1 gates.
- **D155** (2026-10-16, the owner accepts the recommendations): D153 clauses
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
- **D156** (2026-10-16, the owner): the standing acceptance of passing criteria
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
- **D157** (2026-10-16, the owner): require [B615](delivered.md#b615) before M2b
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
  [storage admission](../api/storage.md). X99 was planned and not started at
  this decision; its later authorized implementation and acceptance are recorded
  in [X99](delivered.md#x99--operator-selected-ceph-artifacts-and-m2b-completion).
  This initial request authorized backlog/specification work, not implementation.
- **D158** (2026-10-17, the owner): redefine M1 as Foundations: substrates,
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

- **D159** (2026-10-18, the owner's requests; the clauses after the owner's
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
  crypto-policy defect to [B625](m2.md#b625), both in X61; X60 keeps the OS arms
  B75, B76 and B77 and controller access B346. Each overturned clause reopens
  the work it shaped as a new item.
- **D160** (2026-10-15, the owner; recorded on 2026-10-18 under D159): after
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
- **D161** (2026-10-15, a session finding and the owner's answers; recorded on
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

- **D162** (2026-10-18, the owner requests this architecture and readiness
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
  (b) `ceph.ibm.callHome: disabled` is admitted by planning, and the selected
  build decides how it is realized, as [storage API](../api/storage.md) states.
  Every refusal of a Call Home intent comes before the cluster is claimed but
  after host preparation, so correcting it is an input change that needs the
  operation's destroy and a fresh apply; planning no longer refuses it first.
  (c) D157's non-screening extends to OpenShift release minors. The agent
  installer's accepted control-plane counts are a general rule for every
  minor, with per-minor specializations only where an installer differs. A
  minor with no specialization takes the general rule, and the installer
  judges the release. This narrows the per-minor refusal of
  [container clusters](../api/container-clusters.md#topology).
  (d) Provider OS and version pairings, such as IBM Storage Ceph 9.9.0.x on
  RHEL 9.7, and Fusion, Data Foundation and OpenShift compatibility are
  recorded as dependency rows of the catalog; Bootwright does not gate on them
  (D157), and native tools judge installability.
  This request authorizes the specification and the version-specialization
  slice, with its 9.9.0.x first wave. It authorizes no M3 node-role work, no M4
  add-on work and no evidence-format move. The evidence member rides the next
  coordinated Ceph contract move.
  D163 supersedes clause (d).
- **D163** (2026-10-19, the owner): Bootwright registers and enforces no
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

- **D164** (2026-10-18, the owner; recorded on 2026-10-19): the owner answered
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
  ([B619](m2.md#b619)), a CA-bundle binding for the package repository (one new
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
- **D167** (2026-10-19, the owner): the owner answered the session's question
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

- **D165** (2026-10-19, the owner): the owner answered the seven questions that
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

- **D166** (2026-10-19, the owner): the owner answered the session's question
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

- **D168** (2026-10-20, the owner): the owner instructed that "finished lanes
  that are complete and would make main functional with the feature it
  implemented can be merged to main as soon as they are done". This supersedes
  the owner-merge step of [D164](#decisions) (OD31 (b)), D160's OD1 and D159
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
  [B481](m2.md#b481) stays in progress for the vendor support statement the
  owner left open (D161, OD27); and [B477](m2.md#b477), [B478](m2.md#b478) and
  [B479](m2.md#b479) plan their families once the images are authored.
