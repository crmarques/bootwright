# Milestones

**M1e is the current milestone.** M1a–M1d are complete: the full command
catalog with help and completion; admission of all 26 API kinds; durable
contexts with immutable input; context-backed `validate` and public
`render effective`; the complete `secret` tree over `local-keyring`; and
`bastion setup` with `preflight bastion` on RHEL 9 and Fedora for Linux/amd64.
M1e adds the lifecycle engine and its first capability, so `plan`, `status`,
`apply` and `destroy` become available for one bounded Environment shape. Every
other catalogued command retains the
[unavailable result](cli.md#recognized-but-unavailable-commands).

This file owns delivery scope and deferred work. Product specs describe target
behavior; they do not claim availability. A completed milestone keeps only its
owner, delivered outcome, the tests that guard it and the constraints it left
behind; the current milestone carries its bounded implementation plan; later
work retains only the definitions, dependencies and exit evidence needed to
preserve its scope. Further delivery evidence lives in Git history.

## Scope rules

- Implement the prompt-authorized outcome within the current milestone. An
  explicit out-of-sequence request authorizes only its named slice. A spec or
  backlog entry does not authorize implementation or effects.
- Keep at most one milestone current. Completion requires all its exit evidence;
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
- A gate is reported as passing only with the command and result that produced
  it. Failed, skipped, flaky, unavailable and unrun gates are not passes.

## Completed milestones

### M1a — complete CLI skeleton

**Owner:** CLI; the composition root supplies process inputs and build
metadata. **Delivery:** completed.

Delivered `version`, the complete [command catalog](cli/commands.md), help and
shell completion for Bash, Zsh, Fish and PowerShell. Every unavailable command
calls its typed injected stub and returns `cli.not-implemented` without I/O.

Guarded by the `internal/cli` package tests (catalog fixture, dispatch,
parsing, help precedence, output and cancellation), `test/architecture`
(dependency direction, effect boundaries and composition-only binding) and
`test/completion` (generated shell integrations; Bash in `make check`, the
other shells behind `BOOTWRIGHT_TEST_ALL_SHELLS=1`, see
[development](../docs/development.md)).

### M1b — durable contexts and desired-state admission

**Owners:** Workspace, Desired state, Environment, Infrastructure services,
Machine, Managed OS and Container cluster. **Delivery:** completed.

Delivered in three slices: context-free `validate -f` for every API kind
under the [parser boundary](api.md#parser-boundary); durable contexts (the
`context` tree), context-backed `validate` and public `render effective`
under [Contexts](contexts.md); and the admission update that replaced the
former shared-service union with the six
[infrastructure service kinds](api/infrastructure-services.md) and the
explicit controller Machine, taking the catalog to 26 kinds with no alias,
conversion command or silent frozen-input rewrite.

Guarded by [`cmd/bootwright/admission_acceptance_test.go`](../cmd/bootwright/admission_acceptance_test.go)
and the `internal/desiredstate` package tests (all-kind and cross-kind
fixtures, determinism, non-mutation, bounded fuzzing), the isolated parser
qualification in
[`internal/desiredstate/yamlstream/qualification_linux_test.go`](../internal/desiredstate/yamlstream/qualification_linux_test.go)
(1 GiB RSS and 120-second budgets), the context journeys in
`cmd/bootwright/contexts_test.go`, and the `internal/workspace/contextfs`
fault-injection tests (publication checkpoints, interrupted creation and
deletion, concurrent replacement, subprocess exits on both sides of the
registry commit).

Not qualified: local filesystems other than tmpfs and Btrfs, power loss and
remote filesystems.

### M1c — context secret management

**Owner:** Secrets; Workspace owns the enclosing path boundary. **Delivery:**
completed.

Delivered the complete `secret` tree through the immutable implementation
catalog with one production implementation, `local-keyring`, plus a test-only
session-unlock implementation that qualifies the extension seam. Two
authorized replacements followed: direct root context storage (the fixed root
store with per-user selection and registry v3 namespace/counter identities,
without migration from earlier formats) and storage simplification
(local-keyring v2 with one authenticated metadata file, explicit v1 upgrade and
guarded cleanup). Platform, entitlement and lifecycle effects remain deferred.

Guarded by [`cmd/bootwright/secrets_conformance_test.go`](../cmd/bootwright/secrets_conformance_test.go)
(the shared port suite over both implementations),
`cmd/bootwright/secrets_test.go`, `cmd/bootwright/secrets_disclosure_test.go`,
the real-PTY interrupt tests in
[`cmd/bootwright/interrupts_linux_amd64_test.go`](../cmd/bootwright/interrupts_linux_amd64_test.go),
and the `internal/secrets` tamper, fault-injection, seal-reservation and
upgrade tests.

Not qualified: real-store migration, secure erasure, power loss and actual
host sudo password authentication. Complete-store restore and bounded lifetime
ID allocation remain C14 and C15.

### M1d — bastion setup

**Owners:** Controller (prerequisites, local adapters and verified host
evidence) and Workspace (shared setup state and context binding), using
Machine, Desired state and the invocation boundary. **Delivery:** completed;
end-to-end acceptance against a real bastion is operator-run.

Delivered `bastion setup`, `bastion setup --dry-run` and `preflight bastion`
for RHEL 9 and Fedora on Linux/amd64 under [Controller](controller.md) and the
[host-binding contract](contexts.md#controller-relationship-and-host-binding):
desired-state dependency selection with version overrides, frozen resolution
and retry evidence, shared immutable bundles, host binding, and the
Go-orchestrated Ansible setup entrypoint in the embedded `bootwright.core`
collection. Setup installs every dependency the admitted desired state
selects, including target clients whose consuming lifecycle command is still
deferred.

**Verification model.** Every test carried in this repository is unitary and
host-independent: it runs without a package manager, network, privilege or a
second operating system, and creates no virtual machine. Such tests qualify
contracts, refusals and recovery semantics, never an executed native installer.
End-to-end acceptance on a real bastion is operator-run and is not a gate of
any milestone. The operator-run harnesses and their selectors are listed in
[development](../docs/development.md).

Guarded by the `internal/controller` package tests (both host matrices through
one request/result suite: clean install, no-op, overrides, frozen retry,
contention, cancellation, package failure, unknown outcome and exact retry),
`cmd/bootwright/controller_*_test.go`, the
`internal/workspace/contextfs/controller_*_test.go` checkpoint tests and
`make ansible-check` (pinned syntax, lint, sanity, unit and host-independent
integration targets).

Constraints left for later work: authenticated or private-trust setup
acquisition needs its own Secrets consumer and recovery contract before
promotion, with exit evidence covering exact binding, reopen and release,
certificate validation, non-disclosure and interrupted acquisition with
changed or unavailable credentials; additional host families and
architectures need separate matrices; controller relocation and restore
remain C14; there is no host uninstall or automatic OS upgrade; service images,
the pinned service Ansible closure, local service reservations, readiness and
their qualified inverse belong to M1e.

## M1e — lifecycle engine and managed artifact serving

**Owners:** State reconciliation and Infrastructure services, using Workspace,
Secrets, Machine, Controller and Trust. **Requires:** M1d. **Definition:**
Specified. **Delivery:** in progress.

Deliver the durable lifecycle engine and its first domain capability, making
`plan`, `status`, `apply` and `destroy` available for one bounded Environment
shape. The engine is generic: plans, operation and block state, leases,
private logs, exact continuation, unknown-outcome resolution, immutable secret
binding and the Go-to-Ansible capability boundary belong to State
reconciliation, and the artifact server is one implementation behind the
capability port. Later milestones add capabilities, not another engine.

**Supported shape.** One complete selected Environment whose only lifecycle
capabilities are persistent managed `ArtifactServer` objects placed on OS-ready
provided Machines. `Environment`, `Machine` and `Secret` are inputs, not plan
blocks. External services add no block. Every other selected object that would
require an effect refuses before registration, naming each unsupported object
and the reduced example. `install-only` retention is excluded.

**Placement.** Controller placement is the qualified arm: the placement Machine
is the Environment controller, so M1d's verified host binding, the shared root
lock and new service-specific reservations coordinate it. SSH placement is the
second supported arm, using authored `privateKeyRef` with required
`knownHostsRef` and any required `sudoPasswordRef`, binding the effective user
and all Secret versions. `passwordRef`, `operatorIdentity` and the global
borrowed-SSH flags are unsupported and refuse. No SSH configuration is inferred
for the controller, and no cross-context coordination exists for an SSH host.

**Public destroy is available for this capability set.** The staged-availability
exception in [state reconciliation](state-reconciliation.md#lifecycle-unit) and
the [CLI warnings](cli.md#staged-apply-without-destroy) remain the contract for
a build that lacks the inverse; M1e does not enter that condition because every
owned effect has a qualified inverse in the same executable.

[Infrastructure services](infrastructure-services.md) owns the implementation,
image selection, host layout, conflict identities, TLS rules, readiness,
absence evidence, replay and inverse. TLS validation covers bounded parsing,
certificate/key agreement, validity, server-auth usage and SAN coverage of
every serving endpoint without disclosure.

Qualify the [composition boundaries](architecture.md#dependency-direction-and-communication)
with interchangeable Go capability implementations and Ansible bindings using
the same request/result/failure/evidence contract. Verify fixed entrypoint,
role and plugin resolution and refusal of authored executable selection before
effects. Ansible service-adapter evidence extends M1d's bastion dependency
contracts and reuses its private runtime, execution guard and result channel.

**Verification model.** M1d's model continues: every test carried in this
repository is unitary and host-independent. Real-system acceptance is
operator-run and is not a gate of this milestone.

Exit evidence: the reconciliation domain suite (deterministic plan digests,
transition table, identity allocation and exhaustion); Workspace fault
injection at every publication checkpoint, each asserting the checkpoint fired,
and the proof that acquiring confidential material inside a lifecycle
transaction refuses rather than blocks; operation-store canonical-record,
exclusive-attempt and log-truncation tests; capability selection,
request-digest, TLS subject-alternative-name and evidence tests including
negative disclosure; adapter protocol, request-materialization and secret-file
tests; lifecycle journeys covering fresh apply, decline without effects,
interrupted registration, failed-block retry, all three unknown resolutions,
changed input, automation or host-binding refusal, cancellation, required-log
fault, destroy after a completed apply and its binding release; CLI goldens for
the plan, progress, receipt and status surfaces; the example's admission,
derived request and reservation keys, and the refusal naming every unsupported
object in the larger example; and `make check`.

Not covered by any in-tree gate, by the verification model above: executed
service effects, the process and cancellation boundary of the service adapter,
and SSH placement against a real host. These are operator-run.

Constraints left for later work: content publication into a served root is
C20; ISO construction is C12; neither may append to an M1e frozen plan. SSH
placement has no cross-context conflict coordination, so two contexts targeting
one SSH host remain the operator's responsibility. Bounded parallel execution
remains C7. Managed `Proxy`, `DNSServer` and `NTPServer` belong to M1f.

## Later ordered outcomes

These retain their intended order and scope, but all **need definition** before
implementation. Each must qualify exact releases and close its own contracts.

| Milestone | Owner and outcome | Requires | Definition and exit evidence |
| --- | --- | --- | --- |
| M1f — bastion network services | Infrastructure services: extend the M1e lifecycle to managed `Proxy`, `DNSServer` and `NTPServer` on OS-ready Machines, so a complete bastion service set applies and destroys as one unit. | M1e | Named consumer: [`examples/lab-ocp`](../examples/lab-ocp). Select one exact implementation and image per kind under the M1e capability port. Define each kind's conflict identities, readiness, absence evidence and inverse, including the host resolver and time-daemon collisions a wildcard bind causes. Reuse the M1e engine without extending its plan model. Same unitary verification model, plus the operator-run service journey. |
| M2a — OpenShift/OKD native files | Container cluster and Native artifacts: generate standalone `install-config.yaml` and `agent-config.yaml`. | M1e | Map identity, roles, networks, VIPs, hosts, interfaces, root hints and rendezvous. **N1:** validate NMState/installer schema parity. **N2:** validate/bind pull-secret and public SSH key (M1c). **N3:** define typed manifest, canonical bytes, destinations and overwrite rules (M1b). Prove API sufficiency, sensitive publication/cleanup, non-disclosure and release-specific native goldens. A FIPS slice also qualifies the matching installer and artifact parity. |
| M2b — Ceph native files | Storage and Native artifacts: render typed storage intent into one release-specific declarative file set. | M1e, N3 | Qualify release schemas and reject unprovable fields; revise the API deliberately if needed. **N5:** define each storage secret consumer's validation, immutable binding and sensitive publication (M1c, N3), or prove outputs secret-free. Native goldens and negative disclosure tests. |
| M3 — Ceph-pool script | Storage and Native artifacts: generate one deterministic native-CLI pool script. | M1e | **N4:** define the script manifest, bytes, fixed command structure, destination and generation journey (M1b). Prove argument encoding, replay semantics, diagnostics, sensitive classification, publication and goldens. No authored shell fragments, inline secrets or execution. |
| M4 — OCP bare-metal lifecycle | State reconciliation, Substrate and Container cluster: extend full-context apply and destroy to OCP effects. | M1e, M2a | **L2:** extend pure plans, impacts, dependencies and digests. **L5:** add consumer-owned OCP remote ports. **L4:** extend durable execution, readiness and removal, preserving the M1e inverse and safely refusing incompatible state. **L6:** qualify exact implementations with contract, crash/lease, identity/ownership, replay, cancellation and real-system tests. Destroy a completed M1e snapshot before a fresh expanded apply. |
| M5 — managed RHEL installation | Managed OS and Substrate: install one RHEL release using typed image, profile, entitlement, Secret and Machine intent. | M4, C17 | Extend L2/L4/L5; apply L6. Prove renderer/executor parity, identity, ownership, replay, secret custody and real-system acceptance. |
| M6 — managed Ceph bare metal | Storage, Managed OS, Substrate and State reconciliation: provision one Ceph cluster slice. | M2b and required M5 OS-readiness slice | Extend L2/L4/L5; apply L6 to each implementation. Prove storage identity, ownership, destructive authorization, replay, secret custody and real-system acceptance. |

Independent execution of M2a/M2b/M3 artifacts creates no Bootwright operation,
lease, ownership or continuation state. Generated artifacts remain disposable.

## Candidates

These are unpromoted outcomes, not additional exit gates. Unless otherwise
marked, their definition status is **Candidate**. Promote only the named slice;
fill its concrete version, journey and evidence gaps when requested.

| ID | Owner and bounded outcome | Deferred because / requires | Exit evidence |
| --- | --- | --- | --- |
| C1 | Substrate: one libvirt, vSphere or KubeVirt provisioning variant. | No variant/consumer selected; requires M1e and a named use case. | Exact release, adapter contract, failure/replay tests and real-system qualification. |
| C2 | Infrastructure services: one managed `Registry` or `LoadBalancer` lifecycle. | No named consumer; requires M1e. Managed `Proxy`, `DNSServer` and `NTPServer` were promoted to M1f. | Typed port, exact implementation, lifecycle evidence, failure and acceptance tests. |
| C3 | Storage: one Ceph pool, filesystem, gateway, NFS or export lifecycle. | Separate from operator-run scripts; requires M6 and a named service. | Ownership, replay, destroy and real-system qualification. |
| C4 | Add-ons: one built-in package and binding lifecycle. | No exact package/target/release selected; requires a supported cluster. | [Package/driver contract](add-ons.md), compatibility, trust/secrets, readiness, ordering/replay/destroy and acceptance. |
| C5 | Managed OS: one additional image/profile/entitlement variant. | No concrete consumer; requires M5. | Intent gap, deliberate API revision, renderer/executor parity and qualification. |
| C6 | UX: one additional view of available evidence or explicit access, or a dashboard/completion extension. | No journey selected; requires the underlying capability. | Complete human/machine journey, diagnostics, safety and end-to-end tests. |
| C7 | State reconciliation: bounded parallel block execution, and the lease-only mutation boundary it needs. | Sequential execution must be qualified first; requires L4. M1e holds the exclusive root lock for the whole operation, so a concurrent read waits; narrowing that to the context lease alone, which would let `status --watch` observe a running operation, belongs here. | Ordering/exclusion, deterministic scheduling, cancellation, persistence, partial-failure and replay tests, plus concurrent-reader evidence for the narrowed lock. |
| C8 | Custom automation: one typed, invertible executable playbook journey. **Needs definition.** | Reserved schema cannot prove effects/ownership/non-exfiltration; requires M1e, L2/L4/L5 and a named journey. | Same-change API replacement, immutable source/dependencies, exact targets, bounded secrets, authorization, continuation, failure injection and isolated-runner qualification. |
| C9 | Bare-metal safety: physical offline disk erase and managed-machine destroy. **Blocked.** | Exact disk identity is unproved during the controller-to-installer interval; needs new safety evidence that closes or explicitly bounds it. | Separate safety contract, immutable target proof at erase, failure injection and real-hardware qualification. |
| C10 | Add-ons, Workspace and CLI: custom-catalog acquisition, immutable publication, selection and removal, including the storage location and record format of `add-ons add` registrations and the meaning of the [`add-ons/_store` selection exception](api/environment.md#resource-and-cluster-selection). **Needs definition.** | No source/trust/storage/selection contract; requires M1b and C4. The three `add-ons` commands stay unavailable until promoted. | Closed schemas and formats, fixed bounds, authenticity, atomic/crash-safe storage, deterministic selection, retention through destroy and security/acceptance tests. |
| C11 | Add-ons: one declarative custom-package lifecycle. | No package/target/driver selected; requires C4, C10 and a supported cluster. | Exact identities, qualified driver, host-contract suite, code-content refusal and apply/readiness/replay/destroy acceptance. |
| C12 | Container cluster and Native artifacts: one local bootable installer ISO from M2a inputs and declared server endpoints. **Needs definition.** | No builder journey; requires M1e and M2a. No remote publication. | Exact builder/dependencies, bounded inputs, sensitive classification, typed manifest/digest, atomic publication, metadata goldens, negative effect tests and boot evidence. |
| C13 | Release engineering: one source/binary distribution with licensing and notices. **Needs definition.** | Buildability does not define redistribution; requires M1a and one release channel. | Project license, direct/transitive license review, exact release toolchain/platform/shell matrix, non-skipping completion tests, reproducible archives, notices, dependency inventory, checksums, provenance, SBOM and clean-room packaging verification. |
| C14 | Workspace and Secrets: explicit complete-store restore with logical identity preservation. **Needs definition.** | Copy restoration changes physical identities and may roll back allocation/seal reservations; storage simplification provides upgrades and safe refusal, not a backup/restore command. Requires M1b/M1c and a selected restore journey. | Coherent snapshot validation, authorized inode rebinding, fresh allocation epoch/key before writes after rollback, interruption/retry and wrong-store refusal tests; preserve lifecycle recovery evidence. |
| C15 | Secrets: replace per-ID reservations with bounded lifetime allocation. **Needs definition.** | Current opaque random version/binding IDs retain historical reservation files; a new allocation scheme must preserve issued-ID non-reuse across crashes and restore. Requires M1c and C14 restore semantics. | Bounded allocator state, reservation-before-use, counter/namespace exhaustion, migration of existing bindings and failed attempts, non-reuse and crash tests. |
| C16 | Controller setup and host binding: **delivered by M1d**; local service ownership and conflict refusal **delivered by M1e**. | Relocation requires C14 and a separately defined journey. | M1d owns setup/binding evidence; M1e owns local service qualification and host reservations. |
| C17 | Managed OS and Workspace: the media store for `media add`, `media list` and `media delete`: layout under the context root, record format, bounds, digest verification and retention while an operation freezes an image. **Needs definition.** | No storage contract exists behind the catalogued commands; requires M1b and M5's first media consumer. The three `media` commands stay unavailable until promoted. | Closed layout and record formats, fixed bounds, atomic publication, frozen-by-operation refusal, bounded download and negative effect tests. |
| C19 | Secrets: one additional secret-store implementation (passphrase-protected store, external broker or KDF-based custody). | `local-keyring` meets the current scope; requires M1c and a named operator need. | Shared conformance suite pass, session-material contract, rotation, tamper refusal and non-disclosure tests. |
| C20 | Infrastructure services and Native artifacts: publish generated content into a managed `ArtifactServer`'s served root as an owned lifecycle effect. **Needs definition.** | M1e serves an empty root; no content producer exists until C12 builds an ISO and M2a renders boot artifacts. Requires M1e and C12. | Typed content manifest and digests, destination and overwrite rules within the owned root, atomic publication, retention through destroy, bounded source reads, sensitive classification and negative effect tests. A frozen M1e plan cannot be appended; publication is a block of its own operation. |

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
