# Milestones

**Current milestone: M1a — not started.** The repository contains specifications
and examples; there is no executable, Go module, build tooling or test suite.
No implementation exit evidence has been established.

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
  introduced by M1a. Add no successful placeholders, speculative domain packages
  or adjacent effects. Cross-cutting safety constraints apply from the start.

## M1a — complete CLI skeleton

**Owner:** CLI; composition root supplies process inputs and build metadata.
**Requires:** specification foundation. **Definition:** Specified.

Implement `version` plus the complete [command catalog](cli/commands.md), help
and shell completion. All unavailable application commands return
`cli.not-implemented` without application calls or other effects.

Implementation order:

1. Research and choose a CLI framework under the
   [dependency selection rule](architecture.md#dependency-selection-and-reuse),
   using its completion support or a suitable dependency when needed.
2. Add the Go module, reproducible locks and pinned development/check toolchain.
   Create `cmd/bootwright` and `internal/cli` under the
   [package contract](architecture.md#go-package-structure).
3. Define the command/flag/default/relationship catalog once; configure the
   framework and any completion dependency from it. Keep parsing and
   presentation in the CLI adapter. The unavailable handler has no application
   port or service.
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
- Effect sentinels prove these paths perform only authorized output: no
  application call, discovery, stdin or filesystem access, context lookup,
  secret access, randomness, prompt, privilege, process, network or remote work.
- No speculative domain, persistence or remote adapters are introduced.
- On the integrated commit: `gofmt`, `go mod verify`, `go test ./...`,
  `go vet ./...`, the pinned vulnerability check, and
  `git diff --check` pass; full diff review is complete.

## M1b — durable contexts and desired-state admission

**Owners:** Workspace (context state), Desired state and Environment (compiler
and immutable admitted input). **Requires:** M1a. **Definition:** Needs definition.

Implement all `context` commands, `validate` and `render effective` with all 21
[API kinds](api.md). Before implementation, define canonical context/current
selection and immutable-input formats, copied file set and provenance, locking,
crash/migration/refusal/archival behavior, and relative Secret paths without
copying or rebasing confidential bytes.

Exit: deterministic discovery, strict decoding, bounds, normalization,
references, graph validation, diagnostics and canonical YAML/JSON; an injected
CLI/application boundary; complete context journeys; and tests for concurrent
access, rooted atomic durable publication, corruption/crash recovery and safe
archival. Read-only commands must prove no payload/secret reads, writes,
processes or network access.

## M1c — context secret management

**Owner:** Secrets; Workspace owns the enclosing path boundary. **Requires:**
M1b. **Definition:** Needs definition.

Implement the complete `secret` tree without platform or entitlement effects.
First select the threat-modeled store and key mechanism; define encrypted format,
migration, recovery/rotation, declaration and overwrite rules, version retention,
CLI source/part mappings and machine output.

Exit: source/type validation, secure generation, immutable binding, owner-only
race-resistant storage, atomic publication, tamper/corruption refusal and
recoverable rotation. Test reveal boundaries, non-disclosure, traversal, links,
permissions, concurrency, cancellation and cryptographic failure.

## M1d — managed artifact-server apply

**Owners:** State reconciliation and Infrastructure services, using Workspace,
Secrets, Machine and Trust. **Requires:** M1c and one exact supported server and
host-runtime matrix. **Definition:** Needs definition.

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
