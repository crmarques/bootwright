# Milestones

**M1h is the current milestone.** M1a–M1g are complete: the full command
catalog with help and completion; admission of all 26 API kinds; durable
contexts with immutable input; context-backed `validate` and public
`render effective`; the complete `secret` tree over `local-keyring`;
controller setup with `preflight controller` on RHEL 9 and Fedora for
Linux/amd64; the lifecycle engine with managed artifact serving, so `plan`,
`status`, `apply` and `destroy` are available; the complete `infra-components`
stage with stage selection; and the `controller` stage, which installs the
clients one context's own graph selects. M1h delivers a managed RHEL
installation on emulated bare metal. The Machine command family was delivered
out of sequence on explicit request, so `machine list`, `machine rsh`,
`machine exec`, `machine start`, `machine stop` and `machine restart` are also
available. Every other catalogued command, `machine trust` included, retains
the [unavailable result](cli.md#recognized-but-unavailable-commands).

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

## Out-of-sequence delivery

### Machine commands

**Owners:** Machine, with State reconciliation (the bounded runtime and the
durable evidence a Machine command reads) and Substrate (the power operations
it consumes). **Delivered on explicit request**, outside the milestone
sequence, so it adds no exit gate of its own.

`machine list` reports every selected Machine with the state its context's
durable evidence proves, filtered by cluster membership alone. `machine rsh`
and `machine exec` resolve one bounded SSH handoff, naming a Secret-backed
identity as an export the operator performs rather than materializing it.
`machine start`, `machine stop` and `machine restart` converge one Machine to a
power state through its own management controller, over the one Ansible
boundary, registering no operation and publishing no ownership.

Guarded by `internal/machine/inventory`, `internal/machine/access` and
`internal/machine/power` package tests, the `internal/cli` presentation tests
for the three result shapes, the `internal/reconciliation/lifecycle` runtime
and ownership tests, and the collection's own
`test_machine_power_protocol.py` and `test_redfish_boot.py` unit suites.

Constraints left behind: a power operation holds the context's shared lock for
its whole run, so it waits on a running lifecycle mutation and delays one that
starts while it runs; it reports no progress and retains no adapter output
while it runs, so a graceful stop polling a guest to off prints nothing until
it settles, for the reason C26 records for local `setup`; and `machine trust`
remains unavailable, because the context-managed trust store it maintains is
undefined (C27).

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
store with per-user selection, without migration from earlier formats) and
storage simplification (one authenticated metadata file, explicit v1 upgrade
and guarded cleanup). The M1b identity replacement then took the keyring to
`local-keyring-v3`, whose selector and authenticated data name the owning
context by name. Platform, entitlement and lifecycle effects remain deferred.

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

### M1d — controller setup

**Owners:** Controller (prerequisites, local adapters and verified host
evidence) and Workspace (shared setup state and context binding), using
Machine, Desired state and the invocation boundary. **Delivery:** completed;
end-to-end acceptance against a real controller is operator-run.

Delivered `controller setup`, its dry run and `preflight controller`
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
End-to-end acceptance on a real controller is operator-run and is not a gate of
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
remain C14; there is no host uninstall or automatic OS upgrade; resolution
verifies the Python archive and wheels in disposable staging and the bundle
step acquires them again, so a fresh setup transfers them twice; a retained
resolution serves only its exact target tool set, so a context whose tools
differ from every retained resolution resolves Python and Ansible again
instead of adding the missing tools to the verified closure; service images,
the pinned service Ansible closure, local service reservations, readiness and
their qualified inverse belong to M1e.

### M1e — lifecycle engine and managed artifact serving

**Owners:** State reconciliation and Infrastructure services, using Workspace,
Secrets, Machine, Controller and Trust. **Delivery:** completed; end-to-end
acceptance against a real controller is operator-run.

Delivered the durable lifecycle engine and its first domain capability, making
`plan`, `status`, `apply` and `destroy` available: plans, operation and block
state, leases, private logs, exact continuation, unknown-outcome resolution,
immutable secret binding and the Go-to-Ansible capability boundary, with the
managed `ArtifactServer` as one implementation behind the capability port.
Public destroy shipped with it, so the staged-availability exception was never
entered. Controller placement is the qualified arm; SSH placement is
implemented and operator-run.

Guarded by the reconciliation domain suite, the `reconciliation/lifecycle`
journey suite, the `operationstore` record and log tests, the `contextfs`
checkpoint tests, the capability and adapter suites under
`internal/infrastructureservices`, the CLI lifecycle goldens, and the example
acceptance in `cmd/bootwright/lab_artifacts_example_test.go`.

Constraints left for later work: content publication into a served root is C20;
ISO construction is C12; neither may append to a frozen plan. SSH placement has
no cross-context conflict coordination, so two contexts targeting one SSH host
remain the operator's responsibility. Bounded parallel execution remains C7.
Executed service effects, the service adapter's process and cancellation
boundary, and SSH placement against a real host are operator-run.

### M1f — managed controller network services

**Owners:** Infrastructure services and State reconciliation, using Workspace,
Secrets, Machine and Controller. **Requires:** M1e. **Definition:** Specified.
**Delivery:** completed; end-to-end acceptance against a real controller is
operator-run.

Deliver the complete `infra-components` stage: managed `Proxy`, `DNSServer` and
`NTPServer` join the managed `ArtifactServer` behind the same capability port,
so a whole controller service set applies, replays and destroys as one unit. The
engine gains stage selection, which gates which blocks an invocation starts
without narrowing the plan.

**Supported shape.** One complete selected Environment whose lifecycle objects
are managed `Proxy`, `DNSServer`, `NTPServer` and persistent `ArtifactServer`
objects placed on OS-ready provided Machines. Every other selected object that
would require an effect refuses before registration, naming each unsupported
object and the reduced example. Controller placement is the qualified arm; SSH
placement uses the same request and evidence contract and is operator-run.

**Stage selection.** `plan` and `apply` accept `--stage` over the five stages
[state reconciliation](state-reconciliation.md#stages-and-the-pause-boundary)
defines. Every block carries a frozen stage; a selection gates which blocks
start; an operation that can start nothing further reports `paused` and is
continued by a later `apply` or removed by a `destroy` of the blocks it
completed. Only `infra-components` has capabilities in this executable, so the
other four stages are defined and exercised by tests rather than by effects.

**Implementations.** One container image per kind, digest-pinned in each
capability's catalog and qualified against a real container runtime on the date
recorded in [development](../docs/development.md). Derived configuration comes
from the selected graph, never authored daemon syntax;
[infrastructure services](infrastructure-services.md#managed-network-services)
owns the contract.

Exit evidence: the stage domain suite (readiness, startability, deferral
reasons, done subsets and the nested-cluster graph in which a substrate block
waits on an add-on block); the lifecycle journey suite covering a pause at the
boundary, a wider continuation, destroy from a pause, a refused selection, a
retry excluded by a selection, unknown resolution regardless of selection, and
the preview markers; the shared capability suite (frozen request round trip,
placement arms, digest-pinned images, reservation keys, presence and absence
evidence); adapter protocol and material tests across all four kinds; the
collection gates over the three new roles, playbooks and plugins; CLI goldens
for the stage flag, the plan markers and the paused receipt; the example's
admission, derived requests and reservation keys in
`cmd/bootwright/lab_rhel_example_test.go`; and `make check`.

**Verification model.** M1d's model continues: every test carried in this
repository is unitary and host-independent. Real-system acceptance is
operator-run.

Not covered by any in-tree gate: executed service effects, the process and
cancellation boundary of the service adapter, and SSH placement against a real
host. The named consumer was `examples/managed-infra-components`, which M1h replaced
with [`examples/lab-rhel`](../examples/lab-rhel); the larger `examples/lab-ocp`
fixture it carried was retired with it.

### M1g — controller prerequisites by selecting scope

**Owner:** Controller and CLI, using Workspace and State reconciliation.
**Delivery:** completed.

Split controller prerequisites by what selects them, so one prepared host
serves every context created on it. [Controller](controller.md) owns the
contract; the scopes and their owning commands are its opening table.

One CLI noun names the domain, its host and its Machine: `controller`. Setup
became the root command `setup`, which selects no context: it reads no
Environment, consumes no `--context`, prepares the host foundation, the private
Python and `ansible-core` bundle and the baseline native closure, and publishes
no binding. `preflight controller` keeps its context arm and reports each check
with the scope that owns it, so a negative report names either `setup` or that
context's own controller stage. `Environment.spec.dependencyVersions` lost
`python`, `ansible`, `podman`, `openssh` and `nmstate`, which no single context
may move. The context-to-host binding moved to the first `apply`, published
under the lock and lease that operation already holds, before any reservation
or effect.

The `controller` stage is the first stage, selectable by `--stage` and defined
by [state reconciliation](state-reconciliation.md#stages-and-the-pause-boundary),
with the engine-owned edge that makes every other block wait for a controller
block. [Its capability](controller.md#the-controller-stage) fills it: one block
that resolves and installs the target client closure and the libvirt client a
context selects, on the controller Machine, through that Machine's proxy
choice. A context selecting nothing beyond the host baseline contributes no
block.

The clients are published into a
[client area](contexts.md#controller-relationship-and-host-binding): a shared
host namespace content-addressed by the exact closure, reserved before its
directory exists, attributed to that directory, and sealed once the complete
tree is durable, under the same rules the setup bundle has. The identities a
stage will acquire are retained before acquisition, the native transaction
publishes its before-state into the running attempt before it is authorized,
and the sealed closure is shared: two contexts selecting the same clients prove
the same files and a destroy retains them.

Exit evidence: the `internal/controller/clients` suite (no block without a
selection, a deterministic frozen request, a prepared host proved without
publisher access, intent retained before publication, an unproved native
postcondition left unknown, presence-only observation that publishes nothing,
and a removal that retains the shared closure); the
`internal/workspace/contextfs` client-area suite (publication under its own
reservation, refusal of the setup bundle's identity, recovery from an
interrupted attribution, refusal of an unattributable directory, append-only
retained dependencies, and every client-area checkpoint leaving the store
readable); the `internal/reconciliation/operationstore` before-state record
published once while its attempt runs; the `internal/controller/prerequisites`
suite (setup ignores the tools and libvirt a context selects, retains no tool
source, publishes no binding, and reports both scopes through preflight); the
`internal/controller` selection and version tests; the lifecycle journey suite
(a first apply binds, a repeated apply revalidates, a rebind refuses, an
unprepared host refuses before any effect, a controller block precedes every
other block and receives the publication boundary, an observation cannot
authorize a host effect, and a plan without a controller block gains no
dependency); the CLI catalog, dispatch, confirmation and output tests;
`cmd/bootwright` admission and example acceptance, including the one controller
block [`examples/lab-rhel`](../examples/lab-rhel) plans; and `make check`.

Not covered by any in-tree gate: an executed native client transaction, an
executed target-client publication, and the RHEL libvirt refusal against a real
entitled source. Real-system acceptance is operator-run, and an operator
upgrading an existing host runs `setup` again before the first `apply` of each
context, because no binding exists until then.

## M1h — managed RHEL on emulated bare metal

**Owners:** Substrate and Managed OS, with Infrastructure services (consumer
publication), Workspace (host-wide media store), Controller (the hypervisor
and installer-tooling closures its stage installs) and State reconciliation
(cross-capability requirements, consumed authorization and the one shared
adapter runner); using Secrets and Machine. **Requires:** M1g.
**Definition:** Specified. **Delivery:** in progress.

Deliver one Bootwright-installed RHEL 9.8 Machine on a libvirt guest that boots
its installer through an emulated Redfish BMC, so the bare-metal installation
path is rehearsed end to end without hardware. The `substrates` and `machines`
stages gain their first capabilities; `media add`, `media list` and
`media delete` become available over one host-wide media store; and a managed
`ArtifactServer` serves the per-machine installer ISO and the DVD package tree
derived from that store, for installation and for package updates afterwards.
The named consumer is `examples/lab-rhel`, which replaces
`examples/managed-infra-components`; `examples/lab-artifacts` and
`examples/lab-ocp` are removed with it, and the controller-selection and
context-journey tests they carried move to the new example and to
`examples/multidc-platform`.

**Supported shape.** One Environment whose lifecycle objects are the M1f managed
service set, one libvirt `InfraProvider` whose host Machine is the controller or
an SSH-reachable OS-ready Machine and whose emulated BMCs bind a unicast
address, and Bootwright-installed Machines on that provider whose Anaconda
profile selects `hostedTree` or no package source. A profile selecting
`initialPassword`, `diskEncryption`, `fromSubscription`, `mirror` or
`templateClone`, and every bare-metal, vSphere or KubeVirt provider, refuses
before registration. The served ISO is therefore secret-free; a later
private-content path must extend the frozen request without changing its shape.

**Capabilities.**

- Substrate realizes the provider host: the hypervisor closure (libvirt, QEMU,
  swtpm and the ISO tooling) installed and managed by Bootwright, on a remote
  host through the Ansible boundary over SSH and on the controller through the
  controller stage; the libvirt networks its attachments declare; and its
  virtual-media pool. It realizes each Machine as a libvirt domain with disks,
  deterministic UUID and MAC, ownership metadata and no autostart, together with
  that Machine's own emulated BMC: one digest-pinned sushy-tools container
  serving exactly that domain, so every guest has its own BMC endpoint as a
  physical server does.
- Managed OS installs the OS: it derives the kickstart from effective state,
  builds the per-machine ISO from the frozen boot media, publishes it and the
  DVD tree beneath the selected artifact server's served root, boots the guest
  through its BMC over Redfish virtual media, proves completion through the
  guest agent (install marker and host key), ejects the media and leaves the
  guest running from disk.
- State reconciliation resolves cross-capability requirements named by API
  object into block dependencies, freezes the authorization a block consumes so
  a destroy that deletes a machine's disks requires `data-loss`, keys the
  capability resolver by kind and implementation, and owns the one Ansible
  runner every capability crosses.

**Definitions.** D1, the network a managed libvirt attachment declares, is the
[attachment schema](api/machines.md#machine-profiles-and-network-attachments)
and its realization in [substrates](substrates.md#provider-host-realization).
D2, one emulated BMC per Machine allocated from `bmcEmulationDefaults.port`, is
the [libvirt arm](api/machines.md#libvirt-arm) allocation rule and
[machine realization](substrates.md#machine-realization); `vMediaPort` is
retired. D3, the host-wide media store and its shared `media:` reservations, is
the [media store](managed-os.md#media-store) over the
[Workspace layout](contexts.md#storage-locking-and-publication) and the
[reservation classes](infrastructure-services.md#host-reservations). D4,
consumer content beneath a served root, is
[consumer publication](infrastructure-services.md#consumer-publication). D5,
the guest-agent identity proof, is the Substrate
[identity operation](substrates.md#identity-and-power-operations). D6, the
sushy-tools image, is pinned in the substrate catalog and qualified by hand
before its role is written, with its identity and date recorded in
[development](../docs/development.md); `mkksiso` is qualified against the
RHEL 9.8 boot image the same way.

**Task 0 progress.** The image is resolved, pinned and its container runtime
recorded in [emulated-BMC knowledge](../.agents/knowledge/sushy-tools-emulated-bmc.md),
including the stock command this contract must not use. Its Redfish surface —
the `Systems` collection, virtual media, boot override, power and basic
authentication — and `mkksiso` against RHEL 9.8 boot media remain unqualified.
The capabilities and their roles are written against the contract rather than
against an observed run, so the first real-system acceptance is where those two
are proved and anything they contradict is corrected.

**Deviations to close.** Two parts of this contract are not yet as specified.
The hypervisor closure is installed by the provider host block on either
placement arm, rather than by the controller stage on the controller, because
the controller stage's native closure is versioned per root and the daemon,
emulator and TPM helper have no declared version intent yet; the installer-media
tooling is likewise installed by the installation block. And managed OS composes
the substrate's power and identity operations through its modules rather than
through fixed task files of its roles. Both belong to M1h and neither changes a
frozen request shape.

**Operator output and retained adapter logs.** The first real `lab-rhel` apply
showed both gaps. A block occupied one row per presentation group, so eight
blocks filled the screen with settled work and the step actually running was
hard to find; every step now settles as exactly one row that names the sub-step
in flight and how much of the block is complete. And an adapter run retained
what it printed only when it ended without a complete result, while setup and
the controller stage discarded it entirely, so a run that completed left no
record of the tasks it performed. Every lifecycle adapter run now retains its
own output, the controller stage included. Local `setup` still retains none,
because it allocates no operation identity and so has nowhere to put it; giving
it one is a durable surface of its own and is C26.

**Implementation order.** Three coherent changes: the `media` commands with
their store; the engine's requirements, consumed authorization,
kind-and-implementation resolver and Reconciliation-owned Ansible runner,
together with the `lab-rhel` example that replaces the three it retires; then
the substrate and managed-OS capabilities with their roles, plugins and the
completed example. The hypervisor closure and installer-media tooling join the
controller stage's selection in the same delivery, and the API admission of
managed attachments and BMC port ranges precedes all of it.

Exit evidence: the `internal/substrate` admission tests for managed attachments
and BMC port ranges; capability planning goldens over `examples/lab-rhel`; request
round-trip, evidence-validation and refusal tests for both capabilities; the
engine suite for requirements, consumed authorization, binding resolution, the
inverted removal graph and the fresh removal that supersedes a failed
operation;
media store bounds, fault injection and cross-context freeze refusal; adapter
protocol and module tests with fake HTTP and virsh runners; the collection gates
over the new roles, playbooks and plugins; CLI goldens for the media commands
and the authorization refusal; the progress-row goldens over a step with
sub-steps and the retained-adapter-log suites for a completed run; the example
acceptance in `cmd/bootwright/lab_rhel_example_test.go`; and `make check`.

**Verification model.** M1d's model continues; real-system acceptance on a
prepared libvirt host is operator-run, preceded by a by-hand qualification of
the sushy-tools image and of `mkksiso` against RHEL 9.8 boot media, recorded in
[development](../docs/development.md).

## Later ordered outcomes

These retain their intended order and scope, but all **need definition** before
implementation. Each must qualify exact releases and close its own contracts.

| Milestone | Owner and outcome | Requires | Definition and exit evidence |
| --- | --- | --- | --- |
| M2a — OpenShift/OKD native files | Container cluster and Native artifacts: generate standalone `install-config.yaml` and `agent-config.yaml`. | M1e | Map identity, roles, networks, VIPs, hosts, interfaces, root hints and rendezvous. **N1:** validate NMState/installer schema parity. **N2:** validate/bind pull-secret and public SSH key (M1c). **N3:** define typed manifest, canonical bytes, destinations and overwrite rules (M1b). Prove API sufficiency, sensitive publication/cleanup, non-disclosure and release-specific native goldens. A FIPS slice also qualifies the matching installer and artifact parity. |
| M2b — Ceph native files | Storage and Native artifacts: render typed storage intent into one release-specific declarative file set. | M1e, N3 | Qualify release schemas and reject unprovable fields; revise the API deliberately if needed. **N5:** define each storage secret consumer's validation, immutable binding and sensitive publication (M1c, N3), or prove outputs secret-free. Native goldens and negative disclosure tests. |
| M3 — Ceph-pool script | Storage and Native artifacts: generate one deterministic native-CLI pool script. | M1e | **N4:** define the script manifest, bytes, fixed command structure, destination and generation journey (M1b). Prove argument encoding, replay semantics, diagnostics, sensitive classification, publication and goldens. No authored shell fragments, inline secrets or execution. |
| M4 — OCP bare-metal lifecycle | State reconciliation, Substrate and Container cluster: extend full-context apply and destroy to OCP effects. | M1e, M2a | **L2:** extend pure plans, impacts, dependencies and digests. **L5:** add consumer-owned OCP remote ports. **L4:** extend durable execution, readiness and removal, preserving the M1e inverse and safely refusing incompatible state. **L6:** qualify exact implementations with contract, crash/lease, identity/ownership, replay, cancellation and real-system tests. Destroy a completed M1e snapshot before a fresh expanded apply. |
| M5 — managed RHEL on bare metal | Managed OS and Substrate: extend M1h's Anaconda installation to physical machines and to the secret-bearing profile arms, using typed image, profile, entitlement, Secret and Machine intent. | M1h, M4, C9 | Extend L2/L4/L5; apply L6. Prove renderer/executor parity, exact Redfish system and disk identity, ownership, replay, secret custody and real-hardware acceptance. |
| M6 — managed Ceph bare metal | Storage, Managed OS, Substrate and State reconciliation: provision one Ceph cluster slice. | M2b and required M5 OS-readiness slice | Extend L2/L4/L5; apply L6 to each implementation. Prove storage identity, ownership, destructive authorization, replay, secret custody and real-system acceptance. |

Independent execution of M2a/M2b/M3 artifacts creates no Bootwright operation,
lease, ownership or continuation state. Generated artifacts remain disposable.

## Candidates

These are unpromoted outcomes, not additional exit gates. Unless otherwise
marked, their definition status is **Candidate**. Promote only the named slice;
fill its concrete version, journey and evidence gaps when requested.

| ID | Owner and bounded outcome | Deferred because / requires | Exit evidence |
| --- | --- | --- | --- |
| C1 | Substrate: one vSphere or KubeVirt provisioning variant; the libvirt variant is **promoted into M1h**. | No further variant/consumer selected; requires M1h's substrate contract and a named use case. | Exact release, adapter contract, failure/replay tests and real-system qualification. |
| C2 | Infrastructure services: one managed `Registry` or `LoadBalancer` lifecycle. | No named consumer; requires M1f, whose shared managed-service capability both kinds would extend. | Typed port, exact implementation, lifecycle evidence, failure and acceptance tests. |
| C3 | Storage: one Ceph pool, filesystem, gateway, NFS or export lifecycle. | Separate from operator-run scripts; requires M6 and a named service. | Ownership, replay, destroy and real-system qualification. |
| C4 | Add-ons: one built-in package and binding lifecycle. | No exact package/target/release selected; requires a supported cluster. | [Package/driver contract](add-ons.md), compatibility, trust/secrets, readiness, ordering/replay/destroy and acceptance. |
| C5 | Managed OS: one additional image/profile/entitlement variant. | No concrete consumer; requires M5. | Intent gap, deliberate API revision, renderer/executor parity and qualification. |
| C6 | UX: one additional view of available evidence or explicit access, or a dashboard/completion extension. | No journey selected; requires the underlying capability. | Complete human/machine journey, diagnostics, safety and end-to-end tests. |
| C7 | State reconciliation: bounded parallel block execution, and the lease-only mutation boundary it needs. | Sequential execution must be qualified first; requires L4. A lifecycle operation holds the exclusive root lock for its whole duration, so a concurrent read waits; narrowing that to the context lease alone, which would let `status --watch` observe a running operation, belongs here. | Ordering/exclusion, deterministic scheduling, cancellation, persistence, partial-failure and replay tests, plus concurrent-reader evidence for the narrowed lock. |
| C8 | Custom automation: one typed, invertible executable playbook journey. **Needs definition.** | Reserved schema cannot prove effects/ownership/non-exfiltration; requires M1e, L2/L4/L5 and a named journey. | Same-change API replacement, immutable source/dependencies, exact targets, bounded secrets, authorization, continuation, failure injection and isolated-runner qualification. |
| C9 | Bare-metal safety: physical offline disk erase and managed-machine destroy. **Blocked.** | Exact disk identity is unproved during the controller-to-installer interval; needs new safety evidence that closes or explicitly bounds it. | Separate safety contract, immutable target proof at erase, failure injection and real-hardware qualification. |
| C10 | Add-ons, Workspace and CLI: custom-catalog acquisition, immutable publication, selection and removal, including the storage location and record format of `add-ons add` registrations and the meaning of the [`add-ons/_store` selection exception](api/environment.md#resource-and-cluster-selection). **Needs definition.** | No source/trust/storage/selection contract; requires M1b and C4. The three `add-ons` commands stay unavailable until promoted. | Closed schemas and formats, fixed bounds, authenticity, atomic/crash-safe storage, deterministic selection, retention through destroy and security/acceptance tests. |
| C11 | Add-ons: one declarative custom-package lifecycle. | No package/target/driver selected; requires C4, C10 and a supported cluster. | Exact identities, qualified driver, host-contract suite, code-content refusal and apply/readiness/replay/destroy acceptance. |
| C12 | Container cluster and Native artifacts: one local bootable installer ISO from M2a inputs and declared server endpoints. **Needs definition.** | No builder journey; requires M1e and M2a. No remote publication. | Exact builder/dependencies, bounded inputs, sensitive classification, typed manifest/digest, atomic publication, metadata goldens, negative effect tests and boot evidence. |
| C13 | Release engineering: one source/binary distribution with licensing and notices. **Needs definition.** | Buildability does not define redistribution; requires M1a and one release channel. | Project license, direct/transitive license review, exact release toolchain/platform/shell matrix, non-skipping completion tests, reproducible archives, notices, dependency inventory, checksums, provenance, SBOM and clean-room packaging verification. |
| C14 | Workspace and Secrets: explicit complete-store restore with logical identity preservation. **Needs definition.** | Copy restoration changes physical identities and may roll back seal reservations; storage simplification provides upgrades and safe refusal, not a backup/restore command. Requires M1b/M1c and a selected restore journey. | Coherent snapshot validation, authorized inode rebinding, fresh key before writes after rollback, interruption/retry and wrong-store refusal tests; preserve lifecycle recovery evidence. |
| C15 | Secrets: replace per-ID reservations with bounded lifetime allocation. **Needs definition.** | Current opaque random version/binding IDs retain historical reservation files; a new allocation scheme must preserve issued-ID non-reuse across crashes and restore. Requires M1c and C14 restore semantics. | Bounded allocator state, reservation-before-use, counter/namespace exhaustion, migration of existing bindings and failed attempts, non-reuse and crash tests. |
| C16 | Controller setup and host binding: **delivered by M1d**; local service ownership and conflict refusal **delivered by M1e**. | Relocation requires C14 and a separately defined journey. | M1d owns setup/binding evidence; M1e owns local service qualification and host reservations. |
| C17 | Managed OS and Workspace: the media store behind `media add`, `media list` and `media delete`, **promoted into M1h** as one host-wide store shared by every context (M1h D3). | The store is host-wide rather than per context because context-scoped artifact servers, not the store, publish media to consumers. The three `media` commands stay unavailable until M1h delivers them. | Owned by M1h: closed layout and record formats, fixed bounds, atomic publication, frozen-by-operation refusal across contexts, bounded download and negative effect tests. |
| C19 | Secrets: one additional secret-store implementation (passphrase-protected store, external broker or KDF-based custody). | `local-keyring` meets the current scope; requires M1c and a named operator need. | Shared conformance suite pass, session-material contract, rotation, tamper refusal and non-disclosure tests. |
| C20 | Infrastructure services and Native artifacts: publish generated content into a managed `ArtifactServer`'s served root as an owned lifecycle effect. Consumer publication beneath the served root is **promoted into M1h** (D4); typed manifests for rendered native artifacts remain here. **Needs definition.** | M1e serves an empty root; the first content producer is M1h's installer ISO and package tree, and rendered boot artifacts arrive with C12 and M2a. Requires M1h and C12. | Typed content manifest and digests, destination and overwrite rules within the owned root, atomic publication, retention through destroy, bounded source reads, sensitive classification and negative effect tests. A frozen M1e plan cannot be appended; publication is a block of its own operation. |
| C21 | Secrets and Workspace: bring `file`-sourced Secret material under the [copied-input rule](project.md#design-priorities-and-non-goals) so materialization reads only context custody. **Needs definition.** | The [file source](api/secrets.md#file-source) names operator-owned paths that a lifecycle operation reads at materialization, after admission; whether import copies them into the keyring, the `secret` tree does, or the arm is retired changes the Secrets API and custody contract. Requires M1c and M1e. | Closed import journey and record format, binding of imported bytes to the declaring Secret, refusal of a changed or missing source after import, non-disclosure and negative effect tests. |
| C22 | State reconciliation and Secrets: one recovery for a registered operation whose frozen binding can no longer be reopened. **Needs definition.** | A continuation reopens the exact binding the operation froze, and an incomplete apply refuses every other verb, so a binding lost to an earlier defect or to operator action leaves the context with no continue, no destroy and no delete. Whether recovery re-binds under a proved-equivalent declaration, admits a destroy of a failed apply, or releases ownership explicitly changes both contracts. Requires M1e and M1c. | Exact recovery journey and its authorization, proof that re-acquired material is the material the operation froze or an explicit refusal, ownership and reservation release, and crash/replay tests over a lost binding. |
| C23 | State reconciliation: one recovery for an operation whose unknown block no observation can resolve. **Needs definition.** | [Resolution](state-reconciliation.md#attempts-and-unknown-outcomes) admits only positive completion or positive no effect, and an unknown block starts no retry, dependent block, destroy effect or replacement while [deletion](contexts.md#permanent-deletion) refuses a context that owns one. A partially realized target proves neither state — a libvirt domain defined and owned whose management controller never started is the worked example — so the context has no continue, no destroy and no delete. M1h narrowed what reaches this three times over: a diagnosed adapter failure is reported as `failed`, leaving only a lost or cancelled result; a fresh destroy supersedes a failed apply or destroy, so repairing the adapter that failed an operation no longer strands the context; and a resolution that proves a target is this context's own and part way realized now fails its block, which the next attempt converges. What remains is an observation that can prove neither: a foreign target of the same name, or one that cannot be read at all. That block admits no removal, because no evidence says what it owns. Whether a capability may prove a third resolution, an operator may abandon an operation under its own authorization, or destroy is admitted over an unknown block changes the safety contract. Requires M1e. | Exact recovery journey and its authorization; proof that no unproved effect is repeated outside the capability's safe retry contract; ownership and reservation release; crash and replay tests over a partially realized target; and the refusal that still holds when an effect is genuinely unprovable. |
| C24 | Controller setup and Workspace: retirement of a superseded execution bundle. **Needs definition.** | An automation-only revision no longer re-resolves or re-acquires anything: that resolution is [carried forward](controller.md#supported-host-and-dependency-selection) from the bundle the host already holds. It still names a new bundle area, and [no uninstall or garbage collection](controller.md#supported-host-and-dependency-selection) retires the area it replaced. Retained resolutions and bundle areas are bounded at sixteen, so the sixteenth distinct automation revision on one host refuses every later setup with no operator recovery. Whether the automation projection becomes its own layer over a shared foundation, or retirement stays an explicit operator journey, changes the sealed-bundle and attribution contracts. Requires M1d and M1e. | Closed layer identities and their attribution; proof that a foundation is reused only when its resolved closure is unchanged and that sealed-bundle immutability survives an automation-only revision; authorized retirement that refuses a bundle any retained receipt or frozen lifecycle still needs; bounded reacquisition; and crash/replay tests over a partially retired area and over the exhausted retention limit. |
| C25 | State reconciliation and Controller setup: one upgrade journey for a context whose completed apply a newer build can no longer destroy, **reachable from the installed executable alone**. **Needs definition.** | A removal proves it describes exactly the effects its apply created by comparing each block's request digest, so any change to a capability's content digest refuses `destroy` on every context that applied under the old one. The refusal's own instruction — restore the executable that registered the apply — then collides with [bundle identity](controller.md#selection-and-command-journeys), because that older executable's embedded automation no longer matches the installed bundle. The two digests move independently and each has its own refusal, so an operator is bounced between them with no command that reconciles the pair. M1h is the worked example: a Kickstart version bump refused the destroy, the pre-change executable refused the sealed bundle, and the paths that worked were a build carrying the old renderer beside the new automation, or resealing the bundle to an older executable — four destroy attempts across one afternoon. `destroy` also admits no stage or block scope, so the recovery removes every block the context owns rather than the one whose digest moved. **Building and running a previous revision of the executable is not an acceptable recovery**: it requires the source tree, a toolchain and knowledge of which commit registered the apply, none of which an operator of a released binary has. | The exact upgrade journey and its authorization, offered by the installed executable with no rebuild of any prior revision; whether a removal may be planned from the frozen plan rather than re-derived from current code; how a context names the executable and bundle identity its frozen operation needs, so the refusal can state the remedy instead of the obstacle; scoped removal if one is admitted; and proof that no effect outside the completed apply is ever removed. |
| C26 | Controller setup and Workspace: a durable home for what local `setup` prints. **Needs definition.** | Setup allocates [no lifecycle identity and no operation log](cli/output.md#private-operation-logs), so the Ansible that installs the container runtime and publishes the execution bundle discards its own output, and a failure it does not diagnose leaves nothing to read. The controller stage has no such gap, because it is a lifecycle block whose attempt already owns a retained output file. Whether setup gains a bounded log tree beside its [private recovery receipt](controller.md#publication-and-interrupted-setup), reuses the receipt itself, or keeps discarding changes what a local command is permitted to leave behind. Requires M1d. | Closed path grammar, ownership and retention bound beneath the root-owned controller area; exclusive creation that follows no link and overwrites no unrelated content; proof that a retention fault never changes a setup outcome or its receipt; the `Logs` reference in the human result; and negative tests over a full, unwritable and pre-existing destination. |
| C27 | Trust and Workspace: the context-managed host-key trust `machine trust` maintains. **Needs definition.** | The [Machine access schema](api/machines.md#addresses-and-access) defaults `knownHostsRef` to context-managed trust, but no contract says where that trust is stored, how an observed key enters it, or how `--replace` supersedes one; the installed-machine path instead captures a guest's host key into its own [installation evidence](substrates.md#identity-and-power-operations). Whether the store is a context area of its own, an entry of the existing secret custody, or a projection of installation evidence changes both the Secrets and Workspace contracts, and the observation itself needs an adapter, because a key is read from a remote endpoint. Requires M1c and M1e. The command stays unavailable until promoted. | Closed storage layout and record format; bounded observation of exactly the authorized endpoint; proof that an unknown key is never accepted without `machine trust` and that a changed key refuses without `--replace`; dry-run purity; and the JSON confirmation rule the [catalog](cli/commands.md) already fixes. |

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
Reconciliation, adoption, force behavior and day-2 mutation require an explicit
product/state-contract change before candidacy. The
[stage selection](state-reconciliation.md#stages-and-the-pause-boundary) is not
one of them: it gates which blocks an invocation starts and leaves the plan,
the lifecycle unit and ownership complete.
