# Milestones

This file owns delivery scope, status and the next outcomes; specs claim no
availability. The catalog in [`internal/cli`](../internal/cli/catalog.go) marks
each available command; every other command `bootwright --help` lists returns
the [unavailable result](cli.md#recognized-but-unavailable-commands).

- [Delivered](milestones/delivered.md): completed work, guard tests and constraints.
- [Backlog](milestones/backlog.md): candidates and audit follow-ups.

## Status

| ID | Slice | Owner | Kind | Definition | Delivery |
| --- | --- | --- | --- | --- | --- |
| [M1h](#m1h--managed-rhel-on-emulated-bare-metal) | Managed RHEL on emulated bare metal | Substrate, Managed OS | product | Specified | awaiting operator acceptance |
| [M5a](#m5a--managed-rhel-on-physical-bare-metal) | Managed RHEL on physical bare metal | Substrate, Managed OS | product | Specified | blocked |
| [M4a](#m4a--single-node-openshift-through-the-agent-installer) | Single-node OpenShift through the agent installer | Container cluster, Substrate | product | Specified | awaiting operator acceptance |
| [X12](#x12--audit-phase-1-context-and-guards) | Audit Phase 1: context and guards | Architecture | safety | Specified | in progress |

- **Next for agents:** land X12; promote [audit follow-ups](milestones/backlog.md#audit-follow-ups-2026-09) only on request.
- **Next for operator:** on a clean build descending from `8aa4494`, run [lab-rhel](../examples/lab-rhel/README.md#run-it) and record it in the [acceptance ledger](../docs/acceptance.md) as the [operator guide](../docs/operator-guide.md) describes (M1h), then [lab-sno](../examples/lab-sno/README.md) (M4a); M5a's rehearsal waits for S3b.
- **Next to define:** M1i, the [GitOps handoff gate](#next-ordered-outcomes).

## Scope rules

- Implement only the prompt-authorized outcome: an active slice's open work, or
  an explicitly requested out-of-sequence slice, which authorizes only itself.
  A spec, backlog row or audit item authorizes no implementation or effect.
- A slice is **active** only while its Delivery is `in progress`, and only
  active slices count toward the WIP limit of two: one product or safety and
  one enabling, at most one of them out of sequence.
- **In sequence** means the first uncompleted ordered outcome: M1h, then the
  [next ordered outcomes](#next-ordered-outcomes) in row order. Anything else
  is out of sequence.
- Every slice carries an ID, a Kind (`product`, `safety` or `enabling`),
  Definition and Delivery status, Requires and exit gates.
- Requires governs promotion and implementation, not definition. An unmet
  Requires needs a waiver, which needs an explicit user request and is recorded
  in the slice with its date.
- Removing or narrowing any safety refusal in [specs](index.md) needs a named
  slice.
- Record discovered work under the earliest fitting milestone or as a backlog
  candidate with owner, bounded outcome, deferral reason, Requires, definition
  status and exit evidence. Accepted work, audit items included, carries its
  plan item ID.
- `Specified` is ready for authorized implementation; `Needs definition` lists
  open decisions; `Candidate` is unpromoted; `Blocked` names its resumption
  condition. Promotion fixes the exact implementation and version, closes
  contract gaps and names executable exit evidence; candidates add no gate.
- Deferred commands keep the unavailable result with no placeholder or adjacent
  effect. Cross-cutting safety constraints apply from the start.

## Completion and verification

- Delivery status is `not started`, `in progress`,
  `awaiting operator acceptance`, `blocked` or `completed`; a Delivery cell
  holds one value alone and a deviation lives in the slice. A slice completes
  when every exit gate passes; one whose in-tree gates pass awaits operator
  acceptance, or is `blocked` while a deviation stops its operator gate.
- An **in-tree gate** records its command and result in the delivering
  commit's body, and the slice summarizes it. Its tests are unitary and
  host-independent (no package manager, network, privilege, second OS or
  virtual machine) and qualify contracts, refusals and recovery, never an
  executed native installer.
- An **operator-run gate** records date, build commit and source stamp, command
  sequence, outcome and operation-log digest in the
  [acceptance ledger](../docs/acceptance.md), and completes its slice only on
  an owner-accepted row that matches the slice's acceptance baseline.
  [Development](../docs/development.md) lists the harnesses.
- Qualification tiers are in-tree, emulated rehearsal and real hardware; each
  destructive path names its tier.
- A gate passes only with the command and result that produced it; failed,
  skipped, flaky, unavailable and unrun gates are not passes.

## Open slices

### M1h — managed RHEL on emulated bare metal

**Owners:** Substrate and Managed OS, with Infrastructure services, Workspace
(media store), Controller (stage closures) and State reconciliation.
**Kind:** product. **Requires:** M1g. **Definition:** Specified.
**Delivery:** awaiting operator acceptance. **Acceptance baseline:** `8aa4494`,
where X11 landed.

One Bootwright-installed RHEL 9.8 Machine on a libvirt guest that boots its
installer through an emulated Redfish BMC. Consumer:
[`examples/lab-rhel`](../examples/lab-rhel/README.md).

**Supported shape.** The M1f service set, one libvirt `InfraProvider` whose host
is the controller or an SSH-reachable OS-ready Machine and whose BMCs bind a
unicast address, and installed Machines whose Anaconda profile selects
`hostedTree` or no package source. `initialPassword`, `diskEncryption`, `fips`,
`fromSubscription`, `mirror`, `templateClone` and vSphere or KubeVirt providers
refuse before registration, so the served ISO is secret-free; a private-content
path must extend the frozen request without changing its shape. Bare metal is
M5a's.

**Capabilities.** [Provider host](substrates.md#provider-host-realization) with
managed [attachments](api/machines.md#machine-profiles-and-network-attachments)
(D1) and [machine realization](substrates.md#machine-realization) with a pinned
sushy-tools BMC per domain (D2, D6); the host-wide
[media store](managed-os.md#media-store) (D3);
[installation](managed-os.md#installation) from a per-machine ISO and DVD tree
under [consumer publication](infrastructure-services.md#consumer-publication)
(D4), proved by the guest agent (D5); a disk-deleting destroy consuming
`data-loss`; one progress row per step and retained output for every adapter
run.

**Exit evidence:** the `internal/substrate` admission tests for managed
attachments and BMC port ranges; capability planning goldens over
`examples/lab-rhel`; request round-trip, evidence-validation and refusal tests
for both capabilities; the engine suite for requirements, consumed
authorization, binding resolution, the inverted removal graph and the fresh
removal that supersedes a failed operation; media store bounds, fault injection
and cross-context freeze refusal; adapter protocol and module tests with fake
HTTP and virsh runners; the collection gates over the new roles, playbooks and
plugins; CLI goldens for the media commands and the authorization refusal; the
progress-row goldens over a step with sub-steps and the retained-adapter-log
suites for a completed run; `cmd/bootwright/lab_rhel_example_test.go`; and
`make check`.

**Operator gate** (emulated rehearsal): lab-rhel under a matching build
(staged apply, a settled replay, a removal refused while the guest runs,
`machine stop`, destroy, a fresh apply), then a host restart and `machine
start`. The 2026-09-17 run predates `c42ea10`, so it is an
[observation](../.agents/knowledge/installation-completion-proof.md).

**Constraints left behind:** local `setup` retains no adapter output (C26).

### M5a — managed RHEL on physical bare metal

**Owners:** Substrate and Managed OS, with Infrastructure services, Machine and
State reconciliation. **Kind:** product. **Requires:** M1h, waived on explicit
request by 2026-09-16. **Definition:** Specified. **Delivery:** blocked. The
physical half of M5 on M1h's Anaconda path, without M4 or C9.

**Deviation.** Physical managed-OS installation refuses before registration
until private host-key delivery is repaired. Its in-tree gates pass; delivery
resumes on backlog S3b, which sets the acceptance baseline.

**Supported shape.** A bare-metal `InfraProvider` whose Machines declare NICs,
boot NIC, management controller and root device, install through
`redfishVirtualMedia` and name the delivered `sshKeyPair`; `substrates` may be
empty and virtual and physical Machines may mix. M1h's refusals stand;
`import-certificate` trust is unimplemented and refuses.

**Capabilities.** One [target derivation](substrates.md#selection-and-refusal)
every consumer reads, so `machine start` honours `bmc.tls.verify`;
[physical realization](substrates.md#physical-machine-realization), which proves
identity and MAC set and retains everything on removal; and
[physical installation](managed-os.md#physical-installation), which consumes
`data-loss` on apply, re-proves the target before and inside the installer and
proves completion over SSH pinned to the key its
[private publication](infrastructure-services.md#private-consumer-publication)
delivered.

**Exit evidence:** the `substrate` target-derivation tests (each arm's
controller, channel and requirement, and a consumer that reads only the derived
answer); the `substrate/baremetal` capability suite (identity and MAC proof, an
incomplete inventory left unknown, the claim key, a removal that retains and
consumes nothing, always-quiescent); the `managedos/installation` suite for the
physical arm (authorization on apply, private publication removed at
completion, delivered-key completion, the Kickstart's in-installer proof);
`internal/machine/power` honouring declared controller trust; the collection's
Redfish client suite against three firmware shapes, its system-inspection and
protocol suites; the `examples/lab-baremetal` acceptance; and `make check`.

**Operator gate** (emulated rehearsal): the
[lab-baremetal](../examples/lab-baremetal/README.md) rehearsal, first proof of
the emulator's `EthernetInterfaces`. Real hardware is not a gate; its tier is
an open owner decision ([knowledge](../.agents/knowledge/redfish-physical-bmc.md)).

**Constraints left behind:** a
[residual race](state-reconciliation.md#mutation-safety) between the last
controller proof and the installer's first write; physical destroy and erase
remain C9, so removal retains the system; bonded or VLAN install interfaces and
FIPS profiles refuse; and two contexts claiming one controller from different
hosts are not coordinated.

### M4a — single-node OpenShift through the agent installer

**Owners:** Container cluster and Substrate, with Infrastructure services,
Controller, Secrets and State reconciliation; using Machine. **Kind:**
product. **Requires:** M1h, waived on explicit request on 2026-09-16.
**Definition:** Specified. **Delivery:** awaiting operator acceptance.
**Acceptance baseline:** `8aa4494`, where X11 landed. The agent-installer half of
M4, without M2a's `render installer`, C12 or C9.

**Deviation.** A physical cluster node refuses before registration until the
pre-boot target proof is repaired (backlog S2b).

**Descoped:** the administrator-access custody contract moves to C6.

**Supported shape.** Nodes that are virtual Machines on a realized substrate;
single-node is the rehearsed topology and multi-node libvirt stays admitted
pending the owner decision (plan Part VI). The rest is
[selection and refusal](container-clusters.md#selection-and-refusal): a release
declared by version, the `agent` method, `connected` mode. OKD, disconnected
mode, FIPS, disk encryption, serving certificates, registry policy and a
release pinned by image alone refuse. Consumer:
[`examples/lab-sno`](../examples/lab-sno/README.md).

**Capabilities.** [Boot media](container-clusters.md#boot-media) proves the
installer against the release and builds the agent image from the
[projected inputs](container-clusters.md#installer-inputs), published privately
because it carries the pull secret. [Installation](container-clusters.md#installation)
boots each node through its
[substrate's boot operation](substrates.md#identity-and-power-operations) and
reads the cluster back before releasing the media.

**Exit evidence:** the `containercluster` projection suite
(install-config and agent-config goldens for a single-node libvirt cluster, a
multi-node libvirt cluster and a multi-node physical cluster, the derived
platform and rendezvous address, and the refusals above); the capability suite
for both blocks (the installer-version refusal, private publication removed by
the inverse, the resumable and terminal wait classifications, completion proved
against the cluster's own identity, replay without a rebuild, an inverse that
retains the cluster, quiescence); the substrate boot-operation tests for both
arms; the managed resolver's cluster records; the `examples/lab-sno`
acceptance; and `make check`.

**Operator gate** (emulated rehearsal): lab-sno on a libvirt host with a pull
secret; physical hardware is not a gate
([knowledge](../.agents/knowledge/openshift-agent-disk-safety.md)).

**Constraints left behind:** administrator access stays in the installer's
root-owned work area, so no command reveals it and a destroy of the context
takes it with the area; an artifact server on another Machine refuses;
`render installer` stays unavailable (M2a); the controller's resolver is proved,
not configured, so the operator routes the managed zone; disconnected
installation waits for a managed `Registry` (C2); and a destroyed cluster's
physical nodes keep running (C9).

### X12 — audit Phase 1: context and guards

**Owner:** Architecture, with State reconciliation, Workspace, Controller and
CLI. **Kind:** safety, out of sequence, on explicit request on 2026-09-26.
**Requires:** [X11](milestones/delivered.md#x11--audit-phase-0-and-spec-restructure).
**Definition:** Specified. **Delivery:** in progress. Items, each with its
bounded outcome and exit evidence in the
[audit follow-ups](milestones/backlog.md#audit-follow-ups-2026-09): S10 (its
Phase 1 part), F1, F2, F3 (the withdrawal), F4, R6 and G8 (rest). Exit gates:
each item's exit evidence, `make check`, `make docs-check` and CI. Every item is
implemented; what they left is backlog S10 (rest), F1 (rest), F4 (rest) and
R6 (rest), and they surfaced the pre-existing S12 and F12. Remaining: landing
through its pull request.

## Next ordered outcomes

Each needs definition; the
[later-outcome detail](milestones/backlog.md#later-outcome-detail) keeps their
N and L items.

| Outcome | Owner and outcome | Requires | Definition and exit evidence |
| --- | --- | --- | --- |
| M1i — GitOps handoff gate (audit G4) | State reconciliation and CLI: a derived, effect-free `status` handoff section whose [predicate](state-reconciliation.md#bootstrap-completion-and-gitops-readiness) uses facts the code has: the apply is done, every planned block is done, no unresolved `unknown`, no durable fault. | M1h, M5a, M4a | **Needs definition:** whether block completion discharges frozen readiness and access requirements, and how a completed apply whose last write latched the log fault restores it short of a destroy. Status goldens for ready and each unready reason. |
| M2a — OpenShift/OKD native files | Container cluster and Native artifacts: `render installer` writes M4a's installer inputs as a standalone artifact. | M1e, M4a | N1 to N3; non-disclosure and release goldens. |
| M2b — Ceph native files | Storage and Native artifacts: one release-specific file set. | M1e, N3 | N5; qualified schemas and goldens. |
| M3 — Ceph-pool script | Storage and Native artifacts: one deterministic pool script. | M1e | N4; replay and goldens, no execution. |
| M4 — OCP bare-metal lifecycle | State reconciliation, Substrate and Container cluster: multi-node, physical nodes, disconnected, day-2. | M1e, M4a | L2, L4, L5, L6. |
| M5 — managed RHEL on bare metal | Managed OS and Substrate: secret-bearing arms and the cluster-facing remainder. | M5a, M4, C9 | L2, L4, L5, L6; real hardware. |
| M6 — managed Ceph bare metal | Storage, Managed OS, Substrate and State reconciliation: one Ceph cluster. | M2b, M5a | **Needs definition:** the M5 arms a Ceph host needs; L2, L4, L5, L6. |
