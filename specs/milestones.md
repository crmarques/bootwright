# Milestones

**Current milestone: M1c — completed.** Desired-state
admission for all 21 API kinds, durable contexts, context-backed validation and
public `render effective` are implemented. M1a's catalog, help, version,
completion and unavailable-command guarantees remain in force. M1c is specified
in [Secrets](secrets.md).

This file owns delivery scope and deferred work. Product specs describe target
behavior; they do not claim availability. Keep detailed implementation plans
close to the task that will execute them, rather than pre-designing later work.

## Scope rules

- Implement the prompt-authorized outcome within the current milestone. An
  explicit out-of-sequence request authorizes only its named slice. A spec or
  backlog entry does not authorize implementation or effects.
- Keep exactly one milestone current. Completion requires all its exit evidence;
  moving to another milestone requires an explicit user-requested scope change.
- Record discovered work outside the authorized outcome under the earliest
  fitting future milestone. Give it an owner, bounded outcome, reason for
  deferral, dependencies, definition status and exit evidence. If none fits,
  add a bounded candidate rather than broadening existing work.
- `Specified` means ready for authorized implementation; `Needs definition`
  means listed decisions remain open; `Candidate` means unpromoted scope;
  `Blocked` names the evidence or external condition needed to proceed.
  Delivery status is `not started`, `in progress`, `blocked` or `completed`.
- Before promoting a slice, define its exact supported implementation/version,
  close contract gaps and name executable exit evidence. Required work belongs
  to the exit gate; candidates do not until promoted. A blocked milestone
  remains current and records its resumption condition.
- Deferred commands retain the [unavailable result](cli.md#recognized-but-unavailable-commands)
  introduced by M1a. Their explicitly scoped application stubs add no successful
  placeholders or adjacent effects. Cross-cutting safety constraints apply
  from the start.

## M1a — complete CLI skeleton

**Owner:** CLI; composition root supplies process inputs and build metadata.
**Requires:** specification foundation. **Definition:** Specified.

Implement `version` plus the complete [command catalog](cli/commands.md), help
and shell completion. All unavailable application commands call their typed,
injected stub and return `cli.not-implemented` without I/O or other effects.

Implementation order:

1. Qualify the requested Cobra CLI framework under the
   [dependency selection rule](architecture.md#dependency-selection-and-reuse),
   using its completion support or a suitable dependency when needed.
2. Add the Go module, reproducible locks and pinned development/check toolchain.
   Create `cmd/bootwright` and `internal/cli` under the
   [package contract](architecture.md#go-package-structure).
3. Define the command/flag/default/relationship catalog once; configure the
   framework and any completion dependency from it. Keep parsing and
   presentation in the CLI adapter. Define the consuming CLI interfaces and
   context-owned request types and stubs; wire every command at composition.
4. Implement help, version, output and usage contracts; test the composed
   command and the provider boundaries before exposing any domain capability.

Exit evidence:

- Every public path, flag, shorthand, operand, enum, default and relationship
  follows the catalog. Framework defaults add no public surface.
- Root and explicit help, bare render/completion, version, malformed usage and
  unavailable human/JSON results match the [CLI](cli.md) and
  [output contract](cli/output.md). Source and exercise generated completion
  scripts under every supported shell runtime.
- Removed root groups fail resolution, including explicit help, and appear in
  neither help nor completion. The consolidated cluster paths and payload
  parsing, including flag-shaped payloads after `--`, follow the catalog.
  Access help explains descriptor-only success and static applicability.
  `machine list --silent=true --output json` produces the JSON usage envelope;
  cover reversed flag order, repeated scalar flags, `--silent=false`, and help
  precedence. Cluster commands remain unavailable without resolving targets.
- Effect sentinels prove help, completion, version, and invalid requests call no
  application service, while valid application requests call only their injected
  stub. All skeleton paths perform only authorized output: no
  discovery, stdin or filesystem access, context lookup,
  secret access, randomness, prompt, privilege, process, network or remote work.
- Only the command-consumed interfaces, requests, and stubs are introduced;
  no domain policy, persistence or remote adapters are implemented.
- On the integrated commit: `gofmt`, `go mod verify`, `go test ./...`,
  `go vet ./...`, the pinned vulnerability check, and
  `git diff --check` pass; full diff review is complete.

Delivered evidence:

- `make check` covers formatting, both module locks, package tests, vet,
  generated shell integrations, the pinned vulnerability scan, and diff
  whitespace. `make build` produces `bin/bootwright`.
- The independent catalog fixture checks every public command, local flag,
  shorthand, and output enum. All 49 composed application routes and all stub
  cancellation/deadline paths are covered, alongside parsing, normalization,
  help precedence, output failure, and payload preservation regressions.
- First-party import checks forbid ambient effect capabilities in the skeleton;
  injected call sentinels and completion environment/file sentinels verify the
  no-effect boundary. `go test -race ./...` passes, and a 10-second
  `FuzzInvocation` run completed more than 87,000 executions without failure.
- Both module graphs pass `go mod tidy -diff`. The pinned `govulncheck` reports
  no known reachable vulnerabilities. Full source, dependency, prospective
  tree, and Git-diff reviews are complete.
- Bash, Zsh, Fish, and PowerShell runtime tests pass with and without
  descriptions; see [development](../docs/development.md) for the exact tested
  versions and the required PowerShell preview release.

## M1b — durable contexts and desired-state admission

**Owners:** Workspace (context state), Desired state and Environment (compiler
and immutable admitted input). **Requires:** M1a.

### First delivery: context-free desired-state admission

**Definition:** Specified. **Delivery:** Completed.

Implement `validate` only when at least one `-f/--file` source is supplied,
with all 21 [API kinds](api.md), on Linux/amd64 with the existing Go 1.26.7
toolchain. Use unmodified `go.yaml.in/yaml/v3` v3.0.5 under the
[parser boundary](api.md#parser-boundary). The CLI returns a typed validation
report through the existing text/JSON modes. Without `-f`, `validate` retains
the unavailable result before any state-root or context lookup. Every other
unavailable use case retains its typed stub and no-effect boundary.

The compiler owns immutable authored input, provenance, normalization and an
internal effective inspection representation. This delivery adds neither
context persistence nor public effective rendering, payload access, native
rendering, process or network work.

Admission coverage must include kind-default precedence, empty/atomic values,
variant/conditional-arm inheritance, internal effective-state serialization
stability for empty and zero values, self-default rejection, expansion bounds,
recipient path bases and dependency closure; unified Machine IP/prefix assignments,
installation eligibility and exact NIC binding; native NMState composition;
storage child ownership and endpoint TLS; and typed add-on inputs and OLM
readiness. The specifications and examples do not constitute this compiler.

Exit evidence, required before claiming this delivery complete:

- Deterministic rooted discovery, strict decoding, all fixed bounds, complete
  normalization, reference and graph validation, and ordered text/JSON reports.
- Immutable authored and normalized representations, retained safe provenance,
  and canonical encoder fixtures without an authored-input trust bypass.
- Parser qualification against malformed, oversized, deeply nested and
  high-cardinality fixtures in an isolated helper with a 1 GiB RSS ceiling and
  120-second watchdog, including the documented composition lookahead and
  cooperative cancellation limits.
- All-kind and cross-kind positive/negative fixtures, determinism and
  non-mutation tests, bounded fuzzing, injected CLI/application tests, and
  proof of no state lookup, payload/secret reads, writes, processes or network
  access. Help, completion, version, invalid usage and unavailable commands
  retain their stricter no-effect tests.
- Repository verification and dependency review on the integrated commit;
  passed checks are recorded only after they have run.

Delivered evidence:

- `make check`, `make build`, and `./scripts/go test -race ./...` pass with
  Go 1.26.7 on Linux/amd64. Both module graphs pass `go mod tidy -diff` and
  verification. The complete official vulnerability database was mirrored
  locally for the pinned scanner; no known reachable vulnerabilities were found.
  All four [qualified shell integrations](../docs/development.md) pass.
- The composed CLI validates the unchanged synthetic example as 93 files and
  93 objects. The expanded acceptance fixture admits 113 objects spanning all
  21 kinds, including StorageNFSExport and reserved CustomPlaybook declarations.
  [Acceptance tests](../cmd/bootwright/admission_acceptance_test.go) cover
  precedence, order independence, derived values and authored restrictions.
- [Parser qualification](../internal/desiredstate/yamlstream/qualification_linux_test.go)
  passed all seven isolated adversarial cases within the 1 GiB/120-second
  budgets. The measured maximum RSS was 423,936,000 bytes; the suite took
  2.848 seconds. These measurements do not change the cooperative parser
  cancellation contract.
- A 20-second `FuzzAdmissionCompiler` run with two workers completed 112,854
  executions without failure. Parser and reserved-content fuzzing, strict
  decoding, graph/default/cancellation/limit regressions, safe acquisition,
  immutable provenance, effective codec round trips and CLI output-failure
  checks also pass. Independent effect, dependency, full-tree and diff reviews
  are complete.

### Second delivery: durable contexts and admitted input

**Definition:** Complete in [Contexts](contexts.md). **Delivery:** Completed.

All `context` commands, context-backed `validate`, and public `render effective`
use the versioned registry, frozen acquisition manifests, original provenance,
mutation leases and atomic publication. Authored input remains separate from
effective inspection output.

Exit: complete context journeys; canonical public YAML/JSON; concurrent access,
rooted atomic durable publication, interrupted creation/deletion and guarded
permanent removal tests. Input inspection proves no payload/secret acquisition
or state writes; privileged store and invoking-user access follow the CLI boundary.

Delivered evidence:

- Complete init/update/use/list/current/delete journeys, identity-preserving
  input replacement, interrupted initialization retry, per-user selection
  clearing, ordinary confirmation and guarded permanent disposal pass.
- The synthetic 93-object example imports, survives removal of its original
  tree, validates equivalently and renders complete canonical YAML/JSON.
  Frozen excluded streams, markers and original Secret path bases are retained.
  FIFO payload and lifecycle fixtures prove inspection does not open them.
- Closed metadata, duplicate/null/unknown-field refusal, preallocation bounds,
  immutable blob digests, unsafe paths/types/modes/links, input/state overlap,
  concurrent replacement and complete old/new reader observations pass.
  Every publication checkpoint admits injected interruption; subprocess exits
  on both sides of the registry commit prove safe selection and lock release.
- A real PTY confirmation interrupted by an OS signal returns exit `130`, keeps
  selection unchanged and releases leases. Input descriptor flags, cancellation,
  output failures and informational/unavailable effect boundaries pass.
- Final `make check` (including four-shell completion and the complete local
  vulnerability database), `make build`, `./scripts/go test -race ./...`, both
  modules' `go mod tidy -diff`, and `git diff --check` pass. The ordinary suite
  includes parser qualification under its 1 GiB/120-second acceptance budgets.
- Final 20-second, two-worker fuzz runs pass: `FuzzAdmissionCompiler` completes
  51,507 executions and `FuzzPersistedRecords` completes 364,043 executions.
  Storage qualification passes on available tmpfs and Btrfs filesystems. Other
  allowlisted local filesystem types were not individually exercised; no
  power-loss or remote-filesystem qualification is claimed.

## M1c — context secret management

**Owner:** Secrets; Workspace owns the enclosing path boundary. **Requires:**
M1b. **Definition:** Specified in [Secrets](secrets.md). **Delivery:** Completed.

Implement the complete `secret` tree through the immutable implementation
catalog and semantic store sessions in [Secrets](secrets.md), with one production
implementation, `local-keyring`. Qualify a test-only session-unlock implementation
without changing core command or binding logic. No platform, entitlement or
lifecycle effects; M1d owns the future lifecycle consumer port.

Exit evidence:

- Shared semantic conformance for selected/persisted implementations, typed
  acquisition and generation, immutable bindings and protected-context policy.
- Complete CLI/help/completion and machine-result journeys, exact raw reveal,
  lazy input, confirmation leases and canary non-disclosure checks.
- Owner-only race-resistant storage, inclusive bounds, authenticated records,
  tamper refusal without fallback, reservation ceilings, atomic publication,
  process-death/retry/uncertainty and rotation-failure tests.
- Integrated `make check`, `make build`, `go test -race ./...`, both module
  tidy/verification checks, the pinned vulnerability scan, all four completion
  runtimes, bounded record/material fuzz runs and `git diff --check`. The real
  PTY tests must pass without skips on a PTY-capable runner.

Delivered evidence:

- All nine secret routes are composed through the immutable implementation
  catalog. The [shared conformance journey](../cmd/bootwright/secrets_conformance_test.go)
  exercises local-keyring and a test-only session-unlock implementation through
  the same core services. Desired-state Secret schemas remain unchanged; M1d
  and every platform, entitlement and lifecycle effect remain unavailable.
- [Command journeys](../cmd/bootwright/secrets_test.go) and
  [disclosure checks](../cmd/bootwright/secrets_disclosure_test.go) cover current
  source/part access, stale/orphan handling, protected-context refusal, fresh IDs
  after context-name reuse, immutable bindings, rotation and non-disclosure.
  Confirmation and expected-snapshot tests cover the context mutation boundary.
- [Publication tests](../internal/workspace/contextfs/secrets_publication_linux_amd64_test.go)
  inject every mutation/rotation checkpoint and kill subprocesses before/after
  selector publication. Held-reader, inode-substitution, mount-containment,
  unsafe-file, canonical-bound, seal-ceiling, and
  [AAD/envelope tamper tests](../internal/secrets/localstore/tamper_linux_amd64_test.go)
  pass without older-generation fallback. These are not power-loss or secure
  erasure qualification.
- Complete `make check` and `./scripts/go test -race ./... -count=1` pass
  without test exclusions.
  `make build`, `make fmt-check`, `make vet`, both module `go mod tidy -diff`
  checks and both `go mod verify` checks pass using `./scripts/go`.
  `make completion-test` passes all 104 cases across Bash 5.3.0, Zsh 5.9,
  Fish 4.2.0 and PowerShell 7.7.0-preview.2, without skips. The pinned
  scanner reports no reachable or imported-package vulnerabilities; it reports
  the unimported x/crypto OpenPGP advisory GO-2026-5932 at module level. The final
  scan uses a complete official database copy freshly acquired and validated
  on 2026-09-09, modified `2026-09-02T19:12:04Z`. All 4,378 referenced advisory
  identities, modification times and affected modules match the indexes;
  both official indexes remain unchanged across acquisition. Direct scanner
  DNS lookup fails, so the final gate uses this verified local copy.
- Independent bounded-count fuzz runs pass on their first attempts for canonical
  selectors, private store records, canonical-size preflight, material JSON and
  strict PEM parsers, each with `-fuzztime=100000x -parallel=2 -timeout=3m`.
  Initial 20-second size-preflight and strict-PEM runs exited with
  `context deadline exceeded`; unchanged timed retries passed, but those initial failures
  are not counted as acceptance passes. The independent execution-count runs
  provide the final bounded-fuzz evidence. Focused storage, material, CLI and
  conformance tests and race checks pass; `git diff --check` passes.

Real-terminal qualification is complete: the
[test helper](../cmd/bootwright/interrupts_linux_amd64_test.go) allocates through
the documented Linux PTY multiplexer, retaining all interruption, input,
cancellation, unchanged-selection and descriptor-flag assertions. Both
`TestInterruptDuringRealConfirmationPreservesSelection` and
`TestTerminalFlagsAreRestoredAfterReadyAndEmptyReads` pass in the complete normal
and race gates. Neither real-terminal test was skipped; no device permissions
were changed.
M1c remains current and completed; M1d is not promoted and stays unavailable.

### Direct root context storage

The authorized replacement of M1b/M1c context persistence uses the fixed root
store and independent user selection defined in [Contexts](contexts.md).
It removes prior formats and archival behavior without migration. Context
configuration and desired-state import are separate; plain initialization
creates an eager keyring and may remain without Environment input.

Verification covers no-file creation, immutable imports, configuration-only
no-ops, fresh identities after name reuse, stale/independent selections,
protected deletion and explicit pending-mutation retry. Filesystem tests inject
publication/synchronization failures and substitutions, including resuming
only after registry intent and reused initialization files are durable.
Sudo tests cover pre-effect classification, argument/stdin preservation,
timeout policies, signal forwarding and refresh cleanup. Privileged fixtures
run in isolated user/mount namespaces with synthetic account data and homes;
they exercise actual credential drops and the production default store path.
Normal unprivileged test runs skip those explicitly privileged fixtures; the
isolated acceptance runs execute them separately.

Final `make check`, `make build` and `./scripts/go test -race ./... -count=1`
pass. Completion executes all four shell runtimes without skips. The scanner
uses the complete official database validated on 2026-09-09, with all 4,380
file hashes rechecked; direct online scanning fails on DNS lookup. It reports
no reachable or imported-package vulnerabilities. The production-default
plain-init/current/encryption-status/delete journey passes in a synthetic
root filesystem, including every UID/GID and private-mode assertion. Actual
host sudo password authentication and power-loss behavior were not qualified.

### Storage simplification

Registry v3 uses a fixed-size namespace/counter allocator, preserving existing
IDs through an authorized v2 upgrade. Pristine input updates collect verified
unselected revisions after durable publication. Local-keyring v2 uses one
authenticated metadata file, compact declaration summaries and three artifact
directories. Cleanup retains current/bound material and identity reservations,
removes retired custody, and resumes explicitly after interruption. Supported
v1 conversion preserves logical material and preflights peak storage capacity.

Final `make check`, `make build` and
`./scripts/go test -race ./... -count=1` pass. Completion uses all four qualified
shell runtimes. The pinned vulnerability scan uses the official database copy
validated on 2026-09-09 with all 4,380 hashes rechecked; it finds no reachable
or imported-package vulnerabilities. The existing unimported module advisory
remains outside reachable code. Bounded 100,000-execution fuzz runs pass for
the shared store record, private store records and canonical size preflight;
the registry record run passes 100,027 executions.

Fault tests cover original and newly written file substitution at metadata
commit, recovered-key synchronization, guarded cleanup at physical ceilings,
commit uncertainty, interrupted conversion and cleanup, seal reservations,
missing live identity reservations, and repeated rotations/input updates.
Real-store migration and power-loss qualification were not performed. A source
without conversion headroom safely refuses; complete-store restore and bounded
lifetime secret-ID allocation remain C14/C15 outcomes below.

## M1d — managed artifact-server apply

**Owners:** State reconciliation and Infrastructure services, using Workspace,
Secrets, Machine and Trust. **Requires:** M1c and one exact supported server and
host-runtime matrix. **Definition:** Needs definition.

Extend the context mutation guard to enforce the staged-availability update
restriction after apply registration while destroy remains unavailable, as
required by [state reconciliation](state-reconciliation.md#lifecycle-unit).

Enable full-context apply and exact continuation for an Environment whose only
lifecycle capabilities are persistent managed `artifactServer` components on
OS-ready provided Machines. Refuse any unsupported required capability before
registration or effects. `Environment` and `Secret` are inputs, not plan blocks;
`install-only` retention is excluded.

Use authored `knownHostsRef`, `privateKeyRef` or `passwordRef`, and any required
`sudoPasswordRef`, binding the effective user and all Secret versions.
`operatorIdentity` and the global borrowed-SSH flags are unsupported here.

First define the capability port, HTTP implementation/image, content ownership,
endpoints/TLS, host-key limits and algorithms, readiness, replay, cancellation,
inverse removal and pinned Go-to-Ansible closure. TLS validation must cover
bounded parsing, certificate/key agreement, validity, server-auth usage and SAN
coverage without disclosure.

Qualify the [composition boundaries](architecture.md#dependency-direction-and-communication)
with interchangeable Go capability implementations and Ansible bindings using
the same request/result/failure/evidence contract. Verify fixed entrypoint,
role and plugin resolution and refusal of authored executable selection before
effects. Ansible contract evidence begins with this first implemented adapter.

Exit: deterministic complete plans/digests; immutable input and credentials;
versioned snapshots, leases, logs, ownership, sequential execution and exact
continuation; qualified create/configure/readiness/inverse behavior with
failure injection and real-system evidence. Test crash and unknown outcomes,
warning/confirmation order, locked contexts, cancellation and refusal without
effects. Changed, missing or corrupt bound SSH/trust material must refuse
before connection; observed key mismatch must refuse before remote commands.

Public destroy remains unavailable, so all [staged-availability safeguards](state-reconciliation.md#lifecycle-unit)
and [CLI warnings](cli.md#staged-apply-without-destroy) are required. A compatible
future executable must remove the exact frozen effects. ISO construction (C12)
and remote upload are outside this slice and cannot append to its frozen plan.

## Later ordered outcomes

These retain their intended order and scope, but all **need definition** before
implementation. Each must qualify exact releases and close its own contracts.

| Milestone | Owner and outcome | Requires | Definition and exit evidence |
| --- | --- | --- | --- |
| M2a — OpenShift/OKD native files | Container cluster and Native artifacts: generate standalone `install-config.yaml` and `agent-config.yaml`. | M1d | Map identity, roles, networks, VIPs, hosts, interfaces, root hints and rendezvous. **N1:** validate NMState/installer schema parity. **N2:** validate/bind pull-secret and public SSH key (M1c). **N3:** define typed manifest, canonical bytes, destinations and overwrite rules (M1b). Prove API sufficiency, sensitive publication/cleanup, non-disclosure and release-specific native goldens. A FIPS slice also qualifies the matching installer and artifact parity. |
| M2b — Ceph native files | Storage and Native artifacts: render typed storage intent into one release-specific declarative file set. | M1d, N3 | Qualify release schemas and reject unprovable fields; revise the API deliberately if needed. **N5:** define each storage secret consumer's validation, immutable binding and sensitive publication (M1c, N3), or prove outputs secret-free. Native goldens and negative disclosure tests. |
| M3 — Ceph-pool script | Storage and Native artifacts: generate one deterministic native-CLI pool script. | M1d | **N4:** define the script manifest, bytes, fixed command structure, destination and generation journey (M1b). Prove argument encoding, replay semantics, diagnostics, sensitive classification, publication and goldens. No authored shell fragments, inline secrets or execution. |
| M4 — OCP bare-metal lifecycle | State reconciliation, Substrate and Container cluster: extend full-context apply and enable destroy for artifact-server and OCP effects. | M1d, M2a | **L2:** extend pure plans, impacts, dependencies and digests. **L5:** add consumer-owned OCP remote ports. **L4:** extend durable execution, readiness and removal, preserving the M1d inverse and safely refusing incompatible state. **L6:** qualify exact implementations with contract, crash/lease, identity/ownership, replay, cancellation and real-system tests. Destroy a completed M1d snapshot before a fresh expanded apply. |
| M5 — managed RHEL installation | Managed OS and Substrate: install one RHEL release using typed image, profile, entitlement, Secret and Machine intent. | M4 | Extend L2/L4/L5; apply L6. Prove renderer/executor parity, identity, ownership, replay, secret custody and real-system acceptance. |
| M6 — managed Ceph bare metal | Storage, Managed OS, Substrate and State reconciliation: provision one Ceph cluster slice. | M2b and required M5 OS-readiness slice | Extend L2/L4/L5; apply L6 to each implementation. Prove storage identity, ownership, destructive authorization, replay, secret custody and real-system acceptance. |

Independent execution of M2a/M2b/M3 artifacts creates no Bootwright operation,
lease, ownership or continuation state. Generated artifacts remain disposable.

## Candidates

These are unpromoted outcomes, not additional exit gates. Unless otherwise
marked, their definition status is **Candidate**. Promote only the named slice;
fill its concrete version, journey and evidence gaps when requested.

| ID | Owner and bounded outcome | Deferred because / requires | Exit evidence |
| --- | --- | --- | --- |
| C1 | Substrate: one libvirt, vSphere or KubeVirt provisioning variant. | No variant/consumer selected; requires M1d and a named use case. | Exact release, adapter contract, failure/replay tests and real-system qualification. |
| C2 | Infrastructure services: one managed service arm beyond artifact serving. | No named consumer; requires M1d. | Typed port, exact implementation, lifecycle evidence, failure and acceptance tests. |
| C3 | Storage: one Ceph pool, filesystem, gateway, NFS or export lifecycle. | Separate from operator-run scripts; requires M6 and a named service. | Ownership, replay, destroy and real-system qualification. |
| C4 | Add-ons: one built-in package and binding lifecycle. | No exact package/target/release selected; requires a supported cluster. | [Package/driver contract](add-ons.md), compatibility, trust/secrets, readiness, ordering/replay/destroy and acceptance. |
| C5 | Managed OS: one additional image/profile/entitlement variant. | No concrete consumer; requires M5. | Intent gap, deliberate API revision, renderer/executor parity and qualification. |
| C6 | UX: one additional view of available evidence or explicit access, or a dashboard/completion extension. | No journey selected; requires the underlying capability. | Complete human/machine journey, diagnostics, safety and end-to-end tests. |
| C7 | State reconciliation: bounded parallel block execution. | Sequential execution must be qualified first; requires L4. | Ordering/exclusion, deterministic scheduling, cancellation, persistence, partial-failure and replay tests. |
| C8 | Custom automation: one typed, invertible executable playbook journey. **Needs definition.** | Reserved schema cannot prove effects/ownership/non-exfiltration; requires M1d, L2/L4/L5 and a named journey. | Same-change API replacement, immutable source/dependencies, exact targets, bounded secrets, authorization, continuation, failure injection and isolated-runner qualification. |
| C9 | Bare-metal safety: physical offline disk erase and managed-machine destroy. **Blocked.** | Exact disk identity is unproved during the controller-to-installer interval; needs new safety evidence that closes or explicitly bounds it. | Separate safety contract, immutable target proof at erase, failure injection and real-hardware qualification. |
| C10 | Add-ons, Workspace and CLI: custom-catalog acquisition, immutable publication, selection and removal. **Needs definition.** | No source/trust/storage/selection contract; requires M1b and C4. | Closed schemas and formats, fixed bounds, authenticity, atomic/crash-safe storage, deterministic selection, retention through destroy and security/acceptance tests. |
| C11 | Add-ons: one declarative custom-package lifecycle. | No package/target/driver selected; requires C4, C10 and a supported cluster. | Exact identities, qualified driver, host-contract suite, code-content refusal and apply/readiness/replay/destroy acceptance. |
| C12 | Container cluster and Native artifacts: one local bootable installer ISO from M2a inputs and declared server endpoints. **Needs definition.** | No builder journey; requires M1d and M2a. No remote publication. | Exact builder/dependencies, bounded inputs, sensitive classification, typed manifest/digest, atomic publication, metadata goldens, negative effect tests and boot evidence. |
| C13 | Release engineering: one source/binary distribution with licensing and notices. **Needs definition.** | Buildability does not define redistribution; requires M1a and one release channel. | Project license, direct/transitive license review, exact release toolchain/platform/shell matrix, non-skipping completion tests, reproducible archives, notices, dependency inventory, checksums, provenance, SBOM and clean-room packaging verification. |
| C14 | Workspace and Secrets: explicit complete-store restore with logical identity preservation. **Needs definition.** | Copy restoration changes physical identities and may roll back allocation/seal reservations; storage simplification provides upgrades and safe refusal, not a backup/restore command. Requires M1b/M1c and a selected restore journey. | Coherent snapshot validation, authorized inode rebinding, fresh allocation epoch/key before writes after rollback, interruption/retry and wrong-store refusal tests; preserve lifecycle recovery evidence. |
| C15 | Secrets: replace per-ID reservations with bounded lifetime allocation. **Needs definition.** | Current opaque random version/binding IDs retain historical reservation files; a new allocation scheme must preserve issued-ID non-reuse across crashes and restore. Requires M1c and C14 restore semantics. | Bounded allocator state, reservation-before-use, counter/namespace exhaustion, migration of existing bindings and failed attempts, non-reuse and crash tests. |

When a cluster inspection or access slice under C6 is promoted, its exit evidence
must exercise the [cluster discovery](cli/output.md#cluster-discovery) and
[applicability](cli/commands.md#cluster-command-applicability) contracts across
OpenShift, OKD, managed Ceph, and external Ceph. Cover explicit and current
contexts, selected and excluded names, each applicable/inapplicable command,
missing access metadata or artifacts, unavailable implementations, context-state
and identity refusals, and stable diagnostics/streams. Verify node-name/FQDN/
role-ordinal precedence, declaration-order independence, multi-role Ceph nodes,
`infra` preservation, duplicate-FQDN refusal, and invalid or out-of-range
selectors. Prove inspection uses only permitted metadata, inapplicable access
reads no credential bytes, failed access emits no descriptor or sensitive
result, and successful handoff/export preserves its payload and output boundary.
These tests depend on the promoted use case and do not expand M1a availability.

[Product non-goals](project.md#design-priorities-and-non-goals) remain excluded.
Partial lifecycle selection, reconciliation, adoption, force behavior and day-2
mutation require an explicit product/state-contract change before candidacy.
