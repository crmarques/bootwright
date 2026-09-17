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
available, and so is `machine trust`. Every other catalogued command retains
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

### M5a — managed RHEL on physical bare metal

**Owners:** Substrate and Managed OS, with Infrastructure services (private
consumer publication), Machine (the host key a physical installation delivers)
and State reconciliation (the authorization an installation consumes).
**Requires:** M1h. **Definition:** Specified. **Delivery:** in progress.
**Delivered on explicit request**, outside the milestone sequence: it is the
physical half of M5, bounded to the Anaconda path M1h already proves on
emulated hardware, and it neither requires nor delivers M4 or C9.

Every capability, adapter and test below is implemented and its exit evidence
passes. What remains is the by-hand rehearsal this delivery's verification
model requires: the physical path driven end to end against an emulated
controller, which is also where the pinned emulator's `EthernetInterfaces`
collection is first proved.

Install RHEL on an operator-owned physical server through its own Redfish
management controller, over the same contract that installs a libvirt guest.
The `machines` stage gains a second Machine implementation, `substrates` may
be empty, and one Environment may mix virtual and physical Machines.

**The general seam.** Which substrate realizes a Machine is derived once, in
the Substrate context root, into the management controller a consumer boots
through, the [identity channel](substrates.md#identity-and-power-operations) it
proves completion with, whether the machine is physical, and the block that
realizes it. [Managed OS](managed-os.md#installation) and the
[power commands](substrates.md#identity-and-power-operations) consume that one
answer and name no substrate, so the vSphere, KubeVirt and cloud arms that
follow add a capability package and one derivation case rather than a branch in
every consumer. This retires the second, duplicated controller dispatch the
machine commands introduced, and with it the reason `machine start` ignored
`bmc.tls.verify`.

**Supported shape.** A bare-metal `InfraProvider` whose Machines declare their
NICs, boot NIC, management controller and root device, install through
`redfishVirtualMedia` and name the `sshKeyPair` their installation delivers.
Every M1h refusal stands, and `import-certificate` virtual-media trust refuses
before registration until it is qualified against real firmware.

**Capabilities.** [Physical machine realization](substrates.md#physical-machine-realization)
claims one server by its normalized controller endpoint, proves its exact
ComputerSystem identity and complete MAC set, realizes nothing and retains
everything on removal. [Physical installation](managed-os.md#physical-installation)
consumes `data-loss` on apply, repeats the target proof immediately before it
inserts media and again inside the installer, delivers the host key through
[private publication](infrastructure-services.md#private-consumer-publication)
and proves completion over SSH pinned to that key.

Exit evidence: the `substrate` target-derivation tests (each arm's controller,
channel and requirement, and a consumer that reads only the derived answer);
the `substrate/baremetal` capability suite (identity and MAC proof, an
incomplete inventory left unknown, the claim key, a removal that retains and
consumes nothing, always-quiescent); the `managedos/installation` suite for the
physical arm (authorization on apply, private publication removed at
completion, delivered-key completion, the Kickstart's in-installer proof);
`internal/machine/power` honouring declared controller trust; the collection's
Redfish client suite against three firmware shapes, its system-inspection and
protocol suites; the `examples/lab-baremetal` acceptance; and `make check`.

**Verification model.** M1d's model continues. The physical path is rehearsed
in-tree and by hand against an emulated controller, which exercises every
contract above without hardware; acceptance against a real server is
operator-run and is not a gate of this delivery.

Constraints left behind: the interval between the controller's last proof and
the installer's first write remains a
[residual race](state-reconciliation.md#mutation-safety); physical destroy and
offline erase remain C9, so a removal retains the installed system; a bonded or
VLAN installation interface is not yet derived, so an install address on one
refuses; a FIPS-enabled profile refuses with every other arm that carries
effects this contract does not prove; `import-certificate` trust is refused
rather than implemented; and two contexts claiming one controller from different
controller hosts are not coordinated, exactly as the SSH placement arm is not.

### Machine commands

**Owners:** Machine, with State reconciliation (the bounded runtime and the
durable evidence a Machine command reads) and Substrate (the power operations
it consumes). **Delivered on explicit request**, outside the milestone
sequence, so it adds no exit gate of its own.

`machine list` reports every selected Machine with the state its context's
durable evidence proves, filtered by cluster membership alone. `machine rsh`
and `machine exec` open one SSH session as the identity the Machine's desired
state authorizes, over a host key proved before any credential is offered; the
[session slice](#machine-ssh-sessions-and-host-trust) below owns that contract
and replaced the printed descriptor this milestone first delivered.
`machine start`, `machine stop` and `machine restart` converge one Machine to a
power state through its own management controller, over the one Ansible
boundary, registering no operation and publishing no ownership.

Guarded by `internal/machine/inventory`, `internal/machine/access` and
`internal/machine/power` package tests, the `internal/cli` presentation tests
for the three result shapes, the `internal/reconciliation/lifecycle` runtime
and ownership tests, and the collection's own
`test_machine_power_protocol.py` and `test_redfish_boot.py` unit suites.

A power run retains what its adapter printed as a
[bounded run](cli/output.md#bounded-run-output) and names that directory before
the adapter runs, so a refused run leaves something to read.

Constraints left behind: a power operation holds the context's shared lock for
its whole run, so it waits on a running lifecycle mutation and delays one that
starts while it runs; and it reports no progress beyond naming where it is
writing, so a graceful stop polling a guest to off prints nothing more until it
settles.

### Machine SSH sessions and host trust

**Owners:** Machine, with Trust (the host-key record format and the
`known_hosts` grammar), State reconciliation (the material one session binds
and the installation evidence it proves a key against) and Workspace (the
context trust area). **Requires:** the Machine commands above, M1c and M1e.
**Delivered on explicit request**, outside the milestone sequence, so it adds
no exit gate of its own.

`machine rsh` and `machine exec` open the session rather than printing a
descriptor, under [the session contract](cli.md#machine-ssh-sessions): one
resolved identity, a host key proved from exactly one authorized source before
any credential is offered, session material passed as inherited descriptors,
and the client's exit status as the result. `machine trust` maintains the
[context trust store](contexts.md#storage-locking-and-publication) those
sessions read, so C27 is delivered here.

Guarded by `internal/trust` record and grammar tests, the
`internal/workspace/contextfs` trust-area tests, `internal/machine/access`
resolution and trust-precedence tests, `internal/machine/sshlocal` argument,
policy and descriptor-passing tests, `internal/trust/enrollment` plan tests,
the `internal/cli` session and trust presentation tests, and the
`internal/reconciliation/lifecycle` evidence and material tests.

Constraints left behind: the session's client runs with the elevated
invocation's privileges, so an `auth.operatorIdentity` Machine is reached with
the controller root account's own identity files and no agent — an operator
offers their own key with `--ssh-id-file`; a `passwordRef` Machine has the
client prompt rather than answering for it; and proving an installed Machine's
key reads durable evidence under the context's shared lock, so a session to one
waits on a running lifecycle mutation exactly as a power operation does (C7).

### Concurrent block execution

**Owner:** State reconciliation, with Managed OS (the first block that names a
resource it will not share) and CLI. **Delivered on explicit request**, outside
the milestone sequence, so it adds no exit gate of its own. It delivers the
execution half of C7, which keeps only the lease-only mutation boundary.

An operation
[starts every startable block up to a bound](state-reconciliation.md#stages-and-the-pause-boundary)
instead of one at a time, so the blocks its graph never ordered against each
other run together: after the controller stage, one Environment's managed
services and its provider host go out at once, and every Machine and its
installation follows its own chain. The plan is now frozen
[wave by wave](state-reconciliation.md#plan-and-execution), so the numbered list
an operator confirms is the order the work is started in, and a definition may
name [host resources it will not share](state-reconciliation.md#plan-and-execution),
which the first installation of a shared package tree uses. Every safety rule
is unchanged: an unproved effect is still observed before anything else, a
failed block is still retried alone and once, a failure and a cancellation both
admit nothing further and wait for what is in flight, and the bound is the
executable's own rather than anything the plan or desired state names.

Guarded by the `internal/reconciliation/lifecycle` scheduler suite (blocks
running together within the bound and never past it, a dependent that waits
while its siblings run, two blocks naming one resource that never overlap, a
failure that keeps every block in flight and names each failed block in plan
order, an interrupt that starts nothing more, concurrent observations that
admit nothing beside them, a retry that runs alone, and a stage boundary that
still pauses), the `reconciliation` plan suite (wave order, no block before its
dependency, and frozen exclusive sets through inverse and narrowing), the
`operationstore` concurrent-record test, the `managedos/installation` shared
tree cases, and the whole Go suite under the race detector.

Constraints left behind: the operation still holds the exclusive root lock for
its whole duration, so a concurrent reader waits (C7); the removal's quiescence
probes still run one block at a time, because they settle as a single check
row; and the bound is one number for every kind of block, so a host that cannot
carry several image builds at once is served by narrowing the bound rather than
by a per-resource limit.

### Sequential idempotence and the removal gate

**Owner:** State reconciliation, with every lifecycle capability (each defines
what its own assets being in use means) and CLI. **Delivered on explicit
request**, outside the milestone sequence, so it adds no exit gate of its own.

Three rules, so that repeating a verb is always safe and a removal never
destroys work that is still running. A verb whose work durable state already
proves [settles](state-reconciliation.md#lifecycle-unit) without an effect,
authorization or confirmation, rather than refusing. A resolution that proves
a target is this context's own and part way realized
[fails its block](state-reconciliation.md#attempts-and-unknown-outcomes)
instead of leaving it unproved, so the next attempt converges it and a removal
is admitted; only a foreign or unreadable observation still stays unknown. And
a fresh removal [proves every asset it would take back is out of
use](state-reconciliation.md#quiescence-before-removal) before it registers,
refusing `lifecycle.live` with no operation and naming the command that stops
each one, while every inverse revalidates its own target and fails rather than
forcing — the machine inverse no longer cuts the power to a running domain.

Guarded by the `internal/reconciliation/lifecycle` journey suite (a settled
apply and destroy that touch no record, a changed input that refuses, a partial
resolution converged by a retry and removable by a destroy, a refusal that
registers nothing and releases its binding, every block probed, an unprovable
probe treated as live, a superseding removal gated and a continuation not),
the `reconciliation` resolution table tests, the per-capability partial
classifiers, the Machine quiescence classifier and the derived answers beside
it, the collection's `test_removal_never_forces.py`,
`test_substrate_libvirt.py` and protocol suites, and the CLI settled-result
goldens.

Constraints left behind: the gate observes the Machines alone and derives every
other block from them, so nothing proves that a service, a published artifact
or a provider network is out of use, and a consumer outside the context is
never seen; those Machine probes retain no adapter output, because the gate
runs before any operation is registered and so has no attempt for its output to
sit beside, and they report no progress beyond a row per block; and a Machine
whose state cannot be read is reported live, which refuses a removal on a host
whose hypervisor is down until it answers again.

### Retiring a superseded execution bundle

**Owner:** Controller setup, with Workspace (the area it removes and the record
that accounts for it). **Delivered on explicit request**, outside the milestone
sequence, so it adds no exit gate of its own.

`setup --purge-old-bundles` retires the execution bundles this host no longer
needs, after the setup it runs beside completes and only then. Which areas are
superseded is read from the resolutions the host retains: every one the
completed receipt does not name. The store refuses the bundle that receipt does
name and every client closure whatever it is asked to retire, and an area it
does not hold is already gone. The resolution a retired bundle carries goes with
it, which is what returns capacity, because the retained resolutions and bundle
areas a host may hold are bounded at sixteen and reaching that bound refused
every later setup.

Guarded by the `internal/controller/prerequisites` service tests (only
superseded bundles of a completed setup, nothing retired without one, a failed
retirement reported), and the `contextfs` store tests (the superseded area
removed and the current one kept and still readable, the receipt's own bundle
refused, an absent area completing, and an interrupted retirement left
unreadable and completed by repeating it).

Constraints left behind: retirement is an explicit operator journey rather than
something a setup does on its own, so a host that never asks still reaches its
bound; the client closures a context installed are never retired, because
nothing uninstalls them (C16); and an automation-only revision still republishes
a whole bundle rather than layering over a shared foundation, which is what
remains of C24.

### Removal under the build in hand

**Owner:** State reconciliation, with every lifecycle capability (each reads
its own frozen request) and CLI. **Delivered on explicit request**, outside the
milestone sequence, so it adds no exit gate of its own.

A context is removable by the executable an operator holds, without rebuilding
the one that applied it. A fresh removal is
[planned from the plan its apply froze](state-reconciliation.md#continuation-and-removal)
rather than re-derived and then compared, so a capability whose content digest
or derivation moved no longer refuses every context that applied under the old
one. Each capability reads its own frozen request to state what removing that
block does, so a removal is planned in the words of the removal it performs and
consumes the authorization removing needs, while the block's identity, request
and digests stay exactly as they were frozen. Every capability reads exactly the
request version it writes, and a fresh removal reopens the binding its apply
froze instead of binding what current declarations name, so an edited
declaration does not strand a context. What cannot be read refuses before
registration, naming the block, its request version and the executable identity
the operation recorded; `status` reports that identity too, so the remedy is
available before the refusal is met.

Guarded by the `internal/reconciliation/lifecycle` journey suite (a removal
across a moved content digest and across a superseded failed apply; a refusal
that names an unreadable version and registers nothing; a refusal that names an
implementation this executable no longer provides; a removal that reopens its
apply's binding rather than binding current declarations), the per-capability
removal and version-refusal decoder tests, and the CLI status goldens.

Constraints left behind: a request of any other version refuses, so a context
whose shape has moved is removed by the executable its operation records;
`destroy` still admits no stage or block scope, so the removal covers every
block the context owns; and the bundle half of the same collision is unchanged,
because a sealed bundle is retired by nothing (C24) and an operation continued
rather than superseded still runs the automation it froze.

### M4a — single-node OpenShift through the agent installer

**Owners:** Container cluster and Substrate, with Infrastructure services
(private consumer publication and the names a cluster answers at), Controller
(the installer the stage publishes), Secrets (the material an installation
consumes and the access it captures) and State reconciliation (the `clusters`
stage and the authorization an installation consumes); using Machine.
**Requires:** M1h. **Definition:** Specified. **Delivery:** in progress.
**Delivered on explicit request**, outside the milestone sequence: it is the
agent-installer half of M4 bounded to one cluster topology, and it neither
requires nor delivers M2a's standalone `render installer`, C12's operator-run
ISO builder or C9.

Every capability, adapter and test below is implemented and its exit evidence
passes. What remains is the by-hand rehearsal this delivery's verification
model requires, on a libvirt host with a pull secret, and the contract that
moves the administrator access a completed installation produces out of the
installer's own work area into context custody, which `cluster kubeconfig`
would then reveal.

Install one single-node OpenShift cluster on a Machine the libvirt substrate
realizes, booting the agent image through that Machine's own emulated Redfish
controller, so the bare-metal cluster path is rehearsed end to end without
hardware. The `clusters` stage gains its first capability. The named consumer
is `examples/lab-sno`, which carries the same managed service set as
`examples/lab-rhel` and installs no operating system of its own.

**The general seam.** Nothing here is specific to one substrate or one
topology. The installer inputs are
[projected from effective state](container-clusters.md#installer-inputs) and the
nodes are booted through
[the boot operation their own substrate publishes](substrates.md#identity-and-power-operations),
so a physical cluster, a multi-node cluster and a later substrate arm are each
a case of the same two blocks rather than a second installation path. What a
physical node adds is the authorization its erasure consumes and the one-time
boot override its controller honours; what a multi-node cluster adds is more
nodes to boot and a platform with virtual addresses.

**Supported shape.** [Selection and refusal](container-clusters.md#selection-and-refusal)
owns it: an OpenShift release declared by version, the `agent` method,
`connected` mode, nodes on a realized substrate. OKD, disconnected mode, FIPS,
disk encryption, serving certificates and registry policy refuse before
registration, as does a release pinned by image alone, which names no version
for the installer the controller stage would have to match.

**Capabilities.** [Boot media](container-clusters.md#boot-media) proves the
exact installer executable against the declared release, builds the agent image
from frozen inputs in one owned work area, and publishes it through
[private consumer publication](infrastructure-services.md#private-consumer-publication),
because the image carries the pull secret in its own ignition.
[Installation](container-clusters.md#installation) proves the controller
resolves the cluster's names, boots each node through the
[boot operation its own substrate publishes](substrates.md#identity-and-power-operations),
waits through the installer's own give-ups under one wall-clock budget, proves
the cluster it installed by reading that cluster back, and releases the media
only once the installation has completed.

Exit evidence: the `containercluster` projection suite (install-config and
agent-config goldens for a single-node libvirt cluster, a multi-node libvirt
cluster and a multi-node physical cluster, the derived platform and rendezvous
address, and the refusals above); the capability suite for both blocks (the
installer-version refusal, private publication removed by the inverse, the
resumable and terminal wait classifications, completion proved against the
cluster's own identity, replay without a rebuild, an inverse that retains the
cluster, quiescence); the substrate boot-operation tests for both arms; the
managed resolver's cluster records; the `examples/lab-sno` acceptance; and
`make check`.

**Verification model.** M1d's model continues: every in-tree gate is unitary
and host-independent. The cluster install itself is rehearsed by hand on a
libvirt host, and acceptance against physical hardware is operator-run and is
not a gate of this delivery.

Constraints left behind: the administrator access a completed installation
produces stays in the installer's own root-owned work area, so no command
reveals it and a destroy of the context takes it with the area; the image is
built by the installer the controller stage published there, so a cluster whose
artifact server is placed on another Machine refuses; `render installer` stays unavailable, so the projected
inputs are produced only by an operation (M2a); the cluster's captured access is
revealed by no command until `cluster kubeconfig` is promoted, so the first
delivery leaves it readable only through the context store; the controller's own
resolver is proved rather than configured, so an operator supplies the route to
the managed zone; disconnected installation waits for a managed `Registry` (C2);
and a node whose cluster is destroyed is left as its Machine block leaves it, so
a physical cluster keeps running after its context is removed (C9).

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
remain the operator's responsibility.
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
`initialPassword`, `diskEncryption`, `fips`, `fromSubscription`, `mirror` or
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
including the stock command this contract must not use. The by-hand
qualification this milestone scheduled before its acceptance was deliberately
deferred, and the first real `lab-rhel` install performed it instead: that run
drove the `Systems` collection, virtual media insert and eject, boot override,
power and basic authentication, and built its per-machine image with `mkksiso`
against RHEL 9.8 boot media. The guest it produced installed, booted and served
SSH, so both are qualified by an observed run rather than by contract. What that
run contradicted is corrected and recorded in
[development](../docs/development.md).

**Deviations closed.** Both parts of this contract that were not yet as
specified now are. The hypervisor closure and the installer-media tooling are
installed by the controller stage on the controller, which proves them by
presence and refuses naming that stage when they are absent; a requirement may
now name more than one root package, and the hypervisor shares the version
intent of the libvirt client it runs beside rather than carrying one of its own.
A provider host reached over SSH still installs its own closure. And managed OS
composes each substrate's proof and identity read through the two fixed task
files every substrate machine role exposes, which is the composition M5a's
general seam required.

**What remains.** Every in-tree gate this milestone names passes. Its delivery
is the operator-run journey of `examples/lab-rhel` under the build in hand,
which no run has yet covered since the removal gate, the settled verbs and the
closures above changed what an apply and a removal do.

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
| M2a — OpenShift/OKD native files | Container cluster and Native artifacts: make the [installer inputs](container-clusters.md#installer-inputs) an operator-run standalone artifact through `render installer`, with the secret placeholders and optional sensitive render that command owns. The projection itself is **delivered by M4a**, which freezes it in a plan instead of writing it to a chosen directory. | M1e, M4a | Map identity, roles, networks, VIPs, hosts, interfaces, root hints and rendezvous. **N1:** validate NMState/installer schema parity. **N2:** validate/bind pull-secret and public SSH key (M1c). **N3:** define typed manifest, canonical bytes, destinations and overwrite rules (M1b). Prove API sufficiency, sensitive publication/cleanup, non-disclosure and release-specific native goldens. A FIPS slice also qualifies the matching installer and artifact parity. |
| M2b — Ceph native files | Storage and Native artifacts: render typed storage intent into one release-specific declarative file set. | M1e, N3 | Qualify release schemas and reject unprovable fields; revise the API deliberately if needed. **N5:** define each storage secret consumer's validation, immutable binding and sensitive publication (M1c, N3), or prove outputs secret-free. Native goldens and negative disclosure tests. |
| M3 — Ceph-pool script | Storage and Native artifacts: generate one deterministic native-CLI pool script. | M1e | **N4:** define the script manifest, bytes, fixed command structure, destination and generation journey (M1b). Prove argument encoding, replay semantics, diagnostics, sensitive classification, publication and goldens. No authored shell fragments, inline secrets or execution. |
| M4 — OCP bare-metal lifecycle | State reconciliation, Substrate and Container cluster: the cluster-facing remainder of OCP effects, beyond the one topology **M4a delivers** — a multi-node cluster with its virtual addresses, physical nodes, disconnected installation and the day-2 surface a completed cluster exposes. | M1e, M4a | **L2:** extend pure plans, impacts, dependencies and digests. **L5:** add consumer-owned OCP remote ports. **L4:** extend durable execution, readiness and removal, preserving the M1e inverse and safely refusing incompatible state. **L6:** qualify exact implementations with contract, crash/lease, identity/ownership, replay, cancellation and real-system tests. Destroy a completed M1e snapshot before a fresh expanded apply. |
| M5 — managed RHEL on bare metal | Managed OS and Substrate: the secret-bearing profile arms and the cluster-facing remainder of physical installation, using typed image, profile, entitlement, Secret and Machine intent. The Anaconda path itself is **delivered by M5a**, which also delivers the private publication those arms need. The install arms alone — a bonded or VLAN installation interface, FIPS, registration and disk encryption — depend on M5a and on nothing else here, so they are separable as a bounded out-of-sequence slice when a consumer needs them; the cluster-facing remainder is what requires M4 and C9. | M5a, M4, C9 | Extend L2/L4/L5; apply L6. Prove renderer/executor parity, ownership, replay, secret custody over the private path, a bonded or VLAN installation interface, and real-hardware acceptance. |
| M6 — managed Ceph bare metal | Storage, Managed OS, Substrate and State reconciliation: provision one Ceph cluster slice. | M2b and required M5 OS-readiness slice | Extend L2/L4/L5; apply L6 to each implementation. Prove storage identity, ownership, destructive authorization, replay, secret custody and real-system acceptance. |

Independent execution of M2a/M2b/M3 artifacts creates no Bootwright operation,
lease, ownership or continuation state. Generated artifacts remain disposable.

## Candidates

These are unpromoted outcomes, not additional exit gates. Unless otherwise
marked, their definition status is **Candidate**. Promote only the named slice;
fill its concrete version, journey and evidence gaps when requested.

| ID | Owner and bounded outcome | Deferred because / requires | Exit evidence |
| --- | --- | --- | --- |
| C1 | Substrate: one vSphere or KubeVirt provisioning variant; the libvirt variant is **promoted into M1h** and the bare-metal variant into M5a. | No further variant/consumer selected; requires a named use case. M5a made the shape of this work clear: an arm is a capability package plus one case of the [target derivation](substrates.md#selection-and-refusal), and no consumer of a realized Machine changes. | Exact release, adapter contract, its two fixed identity and pre-boot task files, failure/replay tests and real-system qualification. |
| C2 | Infrastructure services: one managed `Registry` or `LoadBalancer` lifecycle. | No named consumer; requires M1f, whose shared managed-service capability both kinds would extend. | Typed port, exact implementation, lifecycle evidence, failure and acceptance tests. |
| C3 | Storage: one Ceph pool, filesystem, gateway, NFS or export lifecycle. | Separate from operator-run scripts; requires M6 and a named service. | Ownership, replay, destroy and real-system qualification. |
| C4 | Add-ons: one built-in package and binding lifecycle. | No exact package/target/release selected; requires a supported cluster. | [Package/driver contract](add-ons.md), compatibility, trust/secrets, readiness, ordering/replay/destroy and acceptance. |
| C5 | Managed OS: one additional image/profile/entitlement variant. | No concrete consumer; requires M5. | Intent gap, deliberate API revision, renderer/executor parity and qualification. |
| C6 | UX: one additional view of available evidence or explicit access, or a dashboard/completion extension. | No journey selected; requires the underlying capability. | Complete human/machine journey, diagnostics, safety and end-to-end tests. |
| C7 | State reconciliation: the lease-only mutation boundary. | Bounded parallel block execution is **delivered** out of sequence; what remains is the lock it does not need but a reader does. A lifecycle operation holds the exclusive root lock for its whole duration, so a concurrent read waits; narrowing that to the context lease alone, which would let `status --watch` observe a running operation, belongs here. | Concurrent-reader evidence for the narrowed lock, and proof that an operation's own blocks still see one coherent record set. |
| C8 | Custom automation: one typed, invertible executable playbook journey. **Needs definition.** | Reserved schema cannot prove effects/ownership/non-exfiltration; requires M1e, L2/L4/L5 and a named journey. | Same-change API replacement, immutable source/dependencies, exact targets, bounded secrets, authorization, continuation, failure injection and isolated-runner qualification. |
| C9 | Bare-metal safety: physical offline disk erase and managed-machine destroy. **Blocked.** | Exact disk identity is unproved during the controller-to-installer interval; needs new safety evidence that closes or explicitly bounds it. M5a narrowed that interval with an in-installer identity check but did not close it, and deliberately kept removal retaining: a physical removal releases its claim and erases nothing. | Separate safety contract, immutable target proof at erase, failure injection and real-hardware qualification. |
| C10 | Add-ons, Workspace and CLI: custom-catalog acquisition, immutable publication, selection and removal, including the storage location and record format of `add-ons add` registrations and the meaning of the [`add-ons/_store` selection exception](api/environment.md#resource-and-cluster-selection). **Needs definition.** | No source/trust/storage/selection contract; requires M1b and C4. The three `add-ons` commands stay unavailable until promoted. | Closed schemas and formats, fixed bounds, authenticity, atomic/crash-safe storage, deterministic selection, retention through destroy and security/acceptance tests. |
| C11 | Add-ons: one declarative custom-package lifecycle. | No package/target/driver selected; requires C4, C10 and a supported cluster. | Exact identities, qualified driver, host-contract suite, code-content refusal and apply/readiness/replay/destroy acceptance. |
| C12 | Container cluster and Native artifacts: one local bootable installer ISO an operator builds and keeps, outside any operation. **Needs definition.** | No builder journey; requires M1e and M2a. The image a cluster installs from is **delivered by M4a** as an owned lifecycle effect, published privately and removed by its inverse; what remains here is a disposable artifact with no operation, no ownership and no remote publication. | Exact builder/dependencies, bounded inputs, sensitive classification, typed manifest/digest, atomic publication, metadata goldens, negative effect tests and boot evidence. |
| C13 | Release engineering: one source/binary distribution with licensing and notices. **Needs definition.** | Buildability does not define redistribution; requires M1a and one release channel. | Project license, direct/transitive license review, exact release toolchain/platform/shell matrix, non-skipping completion tests, reproducible archives, notices, dependency inventory, checksums, provenance, SBOM and clean-room packaging verification. |
| C14 | Workspace and Secrets: explicit complete-store restore with logical identity preservation. **Needs definition.** | Copy restoration changes physical identities and may roll back seal reservations; storage simplification provides upgrades and safe refusal, not a backup/restore command. Requires M1b/M1c and a selected restore journey. | Coherent snapshot validation, authorized inode rebinding, fresh key before writes after rollback, interruption/retry and wrong-store refusal tests; preserve lifecycle recovery evidence. |
| C15 | Secrets: replace per-ID reservations with bounded lifetime allocation. **Needs definition.** | Current opaque random version/binding IDs retain historical reservation files; a new allocation scheme must preserve issued-ID non-reuse across crashes and restore. Requires M1c and C14 restore semantics. | Bounded allocator state, reservation-before-use, counter/namespace exhaustion, migration of existing bindings and failed attempts, non-reuse and crash tests. |
| C16 | Controller setup and host binding: **delivered by M1d**; local service ownership and conflict refusal **delivered by M1e**. | Relocation requires C14 and a separately defined journey. | M1d owns setup/binding evidence; M1e owns local service qualification and host reservations. |
| C17 | Managed OS and Workspace: the media store behind `media add`, `media list` and `media delete`, **promoted into M1h** as one host-wide store shared by every context (M1h D3). | The store is host-wide rather than per context because context-scoped artifact servers, not the store, publish media to consumers. The three `media` commands stay unavailable until M1h delivers them. | Owned by M1h: closed layout and record formats, fixed bounds, atomic publication, frozen-by-operation refusal across contexts, bounded download and negative effect tests. |
| C19 | Secrets: one additional secret-store implementation (passphrase-protected store, external broker or KDF-based custody). | `local-keyring` meets the current scope; requires M1c and a named operator need. | Shared conformance suite pass, session-material contract, rotation, tamper refusal and non-disclosure tests. |
| C20 | Infrastructure services and Native artifacts: publish generated content into a managed `ArtifactServer`'s served root as an owned lifecycle effect. Consumer publication beneath the served root is **promoted into M1h** (D4); typed manifests for rendered native artifacts remain here. **Needs definition.** | M1e serves an empty root; the first content producer is M1h's installer ISO and package tree, and rendered boot artifacts arrive with C12 and M2a. Requires M1h and C12. | Typed content manifest and digests, destination and overwrite rules within the owned root, atomic publication, retention through destroy, bounded source reads, sensitive classification and negative effect tests. A frozen M1e plan cannot be appended; publication is a block of its own operation. |
| C21 | Secrets and Workspace: bring `file`-sourced Secret material under the [copied-input rule](project.md#design-priorities-and-non-goals) so materialization reads only context custody. **Needs definition.** | The [file source](api/secrets.md#file-source) names operator-owned paths that a lifecycle operation reads at materialization, after admission; whether import copies them into the keyring, the `secret` tree does, or the arm is retired changes the Secrets API and custody contract. Requires M1c and M1e. | Closed import journey and record format, binding of imported bytes to the declaring Secret, refusal of a changed or missing source after import, non-disclosure and negative effect tests. |
| C22 | State reconciliation and Secrets: one recovery for a registered operation whose frozen binding can no longer be reopened. **Needs definition.** | A continuation reopens the exact binding the operation froze, and an incomplete apply refuses every other verb, so a binding lost to an earlier defect or to operator action leaves the context with no continue, no destroy and no delete. Whether recovery re-binds under a proved-equivalent declaration, admits a destroy of a failed apply, or releases ownership explicitly changes both contracts. Requires M1e and M1c. | Exact recovery journey and its authorization, proof that re-acquired material is the material the operation froze or an explicit refusal, ownership and reservation release, and crash/replay tests over a lost binding. |
| C23 | State reconciliation: one recovery for an operation whose unknown block no observation can resolve. **Needs definition.** | [Resolution](state-reconciliation.md#attempts-and-unknown-outcomes) admits only positive completion or positive no effect, and an unknown block starts no retry, dependent block, destroy effect or replacement while [deletion](contexts.md#permanent-deletion) refuses a context that owns one until the operator abandons it. A partially realized target proves neither state — a libvirt domain defined and owned whose management controller never started is the worked example — so the context has no continue and no destroy, and its only delete abandons what that block may own. M1h narrowed what reaches this three times over: a diagnosed adapter failure is reported as `failed`, leaving only a lost or cancelled result; a fresh destroy supersedes a failed apply or destroy, so repairing the adapter that failed an operation no longer strands the context; and a resolution that proves a target is this context's own and part way realized now fails its block, which the next attempt converges. What remains is an observation that can prove neither: a foreign target of the same name, or one that cannot be read at all. That block admits no removal, because no evidence says what it owns. Whether a capability may prove a third resolution or destroy is admitted over an unknown block changes the safety contract; abandoning the context locally is already answered and recovers nothing. Requires M1e. | Exact recovery journey and its authorization; proof that no unproved effect is repeated outside the capability's safe retry contract; ownership and reservation release; crash and replay tests over a partially realized target; and the refusal that still holds when an effect is genuinely unprovable. |
| C24 | Controller setup and Workspace: the automation projection as its own layer over a shared foundation. **Needs definition.** | Retiring a superseded execution bundle is **delivered** by [`setup --purge-old-bundles`](controller.md#supported-host-and-dependency-selection), so the retention bound is no longer reached with no operator recovery. What remains is the other half of the original question: an automation-only revision still names a whole new bundle area and republishes the same closure into it, because the projection that carries the automation is part of the bundle's identity rather than a layer over a shared foundation. Requires M1d and M1e. | Closed layer identities and their attribution; proof that a foundation is reused only when its resolved closure is unchanged and that sealed-bundle immutability survives an automation-only revision; and bounded reacquisition.
| C25 | State reconciliation and Controller setup: the remainder of the upgrade journey — scoped removal, and the bundle half of the same collision. **Needs definition.** | Planning a removal from the plan its apply froze is **delivered** by [removal under the build in hand](#removal-under-the-build-in-hand), so a moved content digest, a request shape one version old and an edited declaration no longer strand a context. Two parts remain. `destroy` admits no stage or block scope, so a recovery removes every block the context owns rather than the one whose digest moved. And a sealed bundle is retired by nothing (C24), so an operation that must be *continued* rather than superseded still runs the automation it froze and still refuses a host whose bundle has moved on; only a fresh verb runs under the build in hand. Requires M1d and M1e. | The authorization and journey of a scoped removal, and proof that a scope never leaves a dependent behind; how a continuation names the bundle identity its frozen operation needs so the refusal states the remedy; and crash and replay tests over both.
| C26 | Controller setup and Workspace: a durable home for what local `setup` prints. **Needs definition.** | Setup allocates [no lifecycle identity and no operation log](cli/output.md#private-operation-logs), so the Ansible that installs the container runtime and publishes the execution bundle discards its own output, and a failure it does not diagnose leaves nothing to read. The controller stage has no such gap, because it is a lifecycle block whose attempt already owns a retained output file, and neither does a bounded run, which retains its own under [`state/runs/<run-id>`](cli/output.md#bounded-run-output). Setup is what is left: it selects no context, so it has no context state to keep a run beside. Whether it gains a bounded log tree beside its [private recovery receipt](controller.md#publication-and-interrupted-setup), reuses the receipt itself, or keeps discarding changes what a context-free local command is permitted to leave behind. Requires M1d. | Closed path grammar, ownership and retention bound beneath the root-owned controller area; exclusive creation that follows no link and overwrites no unrelated content; proof that a retention fault never changes a setup outcome or its receipt; the `Logs` reference in the human result; and negative tests over a full, unwritable and pre-existing destination. |
| C27 | Trust and Workspace: the context-managed host-key trust `machine trust` maintains. **Delivered by [Machine SSH sessions and host trust](#machine-ssh-sessions-and-host-trust).** | The three open questions are closed there: the store is a context area of its own, `state/trust/hosts.json`, holding public keys alone; an observed key enters it only through `machine trust` or an explicitly confirmed interactive first use; and `--replace` is the one path that supersedes a recorded key. An installed Machine keeps proving its key against its own [installation evidence](substrates.md#identity-and-power-operations) and consults no store. Requires M1c and M1e. | Closed storage layout and record format; bounded observation of exactly the authorized endpoint; proof that an unknown key is never accepted without `machine trust` and that a changed key refuses without `--replace`; dry-run purity; and the JSON confirmation rule the [catalog](cli/commands.md) already fixes. |
| C29 | Architecture: split the production functions still over the line limit, and retire the three copied managed-service roles. **Needs definition.** | `test/architecture` now fails a production function longer than 100 lines unless it is listed as awaiting a split, and 38 are. Separately, `infra_dns_server_dnsmasq`, `infra_ntp_server_chrony` and `infra_proxy_squid` differ only by a variable prefix and a template name, while Go already runs all three through one capability over one definition; collapsing them to one role changes each frozen request's shape and every role identity, which no in-tree gate proves against a host. Requires M1h. | Each split proved by the suite that already covers the function, with the entry removed from the list; one parameterized managed-service role with its argument spec, the collection gates over it, and a real-host apply and destroy of one managed service before the copies are removed. |
| C30 | Architecture: reduce production comments to what the [code clarity contract](architecture.md#self-explanatory-code-and-retained-knowledge) retains. **Needs definition.** | Production Go carries about 5,200 comment lines, role and playbook YAML about 210, and collection plugins about 370 beyond the documentation blocks `ansible-doc` requires. Most state rationale the contract sends to the knowledge catalog, and some state invariants that must survive as tests or names rather than prose. Deciding each one is the work; a blanket strip would lose the findings the catalog is meant to keep. Requires no other slice. | Each retained comment justified by language, tooling or a maintained contract; every durable finding moved into `.agents/knowledge/` with its links and evidence; and a fitness gate that holds the result. |
| C28 | Architecture: one implementation of [the adapter result protocol](architecture.md#the-adapter-result-protocol), and one Ansible runner. **Needs definition.** | The protocol is now specified, but ten per-capability action plugins each carry their own copy of its phase dispatch, acknowledgement, group emission and postcondition rule, and each is paired with a documentation-only module. Beside them, `controller/ansiblelocal` is a second runner with a second protocol reader and a `prepared` phase the lifecycle runner does not have, so `setup` and the controller stage cross a different boundary from every other block. Collapsing both is mechanical but changes what every role emits and what fails a run, and no in-tree gate proves an adapter's real host behavior. Whether the postcondition decision moves to Go, and whether progress groups come from task tags through a callback plugin instead of 194 emission tasks, changes what a role contains. Requires M1h. | One shared protocol implementation with its own unit tests; one runner whose request, phases and failure semantics both consumers share; per-capability evidence proved by its own tests; the collection sanity, lint and integration gates; and a real-host run of one apply and one destroy before the old paths are removed. |

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
