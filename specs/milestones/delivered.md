# Delivered

Completed milestones and delivered out-of-sequence slices. Each keeps its
owner, delivered outcome, the tests that guard it, the constraints it left
behind and links to the owning specs, which own the behavior. Further delivery
evidence lives in Git history. [Milestones](../milestones.md) owns status and
the completion rules.

The historical M1a to M1g below are distinct from the
[Foundations submilestones](m1.md#submilestones) introduced by D158.
M1a to M1g completed before the [acceptance ledger](../../docs/acceptance.md)
existed and named no operator gate; the operator-run acceptance some of them
name is unrecorded. They and X1 to X10 predate the seven milestones M1 to M7
and belong to none of them; X11 to X18 delivered toward M1. Out-of-sequence
slices carry X IDs, distinct from the audit plan's item prefixes. Entries
predate the Kind field and keep the fields and item IDs their delivery
recorded; from the reorganization of 2026-09-28, a record names each item it
delivers by its B ID with the old ID beside it.

## Completed milestones

### M1a — complete CLI skeleton

**Owner:** CLI; the composition root supplies process inputs and build metadata.

**Outcome:** `version`, the complete [command catalog](../cli/commands.md),
help and shell completion for Bash, Zsh, Fish and PowerShell. Every unavailable
command calls its typed injected stub and returns `cli.not-implemented` without
I/O.

**Guard tests:** the `internal/cli` package tests (catalog fixture, dispatch,
parsing, help precedence, output and cancellation), `test/architecture`
(dependency direction, effect boundaries and composition-only binding) and
`test/completion` (generated shell integrations; Bash in `make check`, the
other shells behind `BOOTWRIGHT_TEST_ALL_SHELLS=1`, see
[development](../../docs/development.md)).

### M1b — durable contexts and desired-state admission

**Owners:** Workspace, Desired state, Environment, Infrastructure services,
Machine, Managed OS and Container cluster.

**Outcome:** context-free `validate -f` for every API kind under the
[parser boundary](../api/input.md#parser-boundary); durable contexts (the `context`
tree), context-backed `validate` and public `render effective` under
[Contexts](../contexts.md); and admission of the six
[infrastructure service kinds](../api/infrastructure-services.md) and the
explicit controller Machine in place of the former shared-service union, which
took the catalog to 26 kinds with no alias, conversion command or silent
frozen-input rewrite.

**Guard tests:** [`cmd/bootwright/admission_acceptance_test.go`](../../cmd/bootwright/admission_acceptance_test.go)
and the `internal/desiredstate` package tests (all-kind and cross-kind
fixtures, determinism, non-mutation, bounded fuzzing); the isolated parser
qualification in
[`internal/desiredstate/yamlstream/qualification_linux_test.go`](../../internal/desiredstate/yamlstream/qualification_linux_test.go)
(1 GiB RSS and 120-second budgets); the context journeys in
`cmd/bootwright/contexts_test.go`; and the `internal/workspace/contextfs`
fault-injection tests (publication checkpoints, interrupted creation and
deletion, concurrent replacement, subprocess exits on both sides of the
registry commit).

**Constraints left behind:** local filesystems other than tmpfs and Btrfs,
power loss and remote filesystems are not qualified.

### M1c — context secret management

**Owner:** Secrets; Workspace owns the enclosing path boundary.

**Outcome:** the complete `secret` tree through the immutable implementation
catalog, with one production implementation, `local-keyring`, and a test-only
session-unlock implementation that qualifies the extension seam. Contexts use
direct root storage with per-user selection, and the keyring keeps one
authenticated metadata file with guarded cleanup. `local-keyring-v3` names the
owning context by name in its selector and authenticated data. No earlier
keyring format is converted: it refuses, and the remedy is a new context
([secrets](../secrets.md)). Platform, entitlement and lifecycle effects remain
deferred.

**Guard tests:** [`cmd/bootwright/secrets_conformance_test.go`](../../cmd/bootwright/secrets_conformance_test.go)
(the shared port suite over both implementations),
`cmd/bootwright/secrets_test.go`, `cmd/bootwright/secrets_disclosure_test.go`,
the real-PTY interrupt tests in
[`cmd/bootwright/interrupts_linux_amd64_test.go`](../../cmd/bootwright/interrupts_linux_amd64_test.go),
and the `internal/secrets` tamper, fault-injection and seal-reservation tests.

**Constraints left behind:** secure erasure, power loss and actual host sudo
password authentication are not qualified. Complete-store restore and bounded
lifetime ID allocation remain C14 and C15.

### M1d — controller setup

**Owners:** Controller (prerequisites, local adapters and verified host
evidence) and Workspace (shared setup state and context binding), using
Machine, Desired state and the invocation boundary.

**Outcome:** controller setup, its dry run and `preflight controller` for
RHEL 9 and Fedora on Linux/amd64 under [Controller](../controller.md) and the
[host-binding contract](../contexts.md#controller-relationship-and-host-binding):
dependency selection with version overrides, frozen resolution and retry
evidence, shared immutable bundles, host binding and the Go-orchestrated
Ansible setup entrypoint in the embedded `bootwright.core` collection. M1g
later made setup the context-free root `setup`, moved context-selected
dependencies to the controller stage and moved the binding to the first
`apply`. The verification model it introduced is now the milestones'
[completion and verification](../milestones.md#completion-and-verification)
rule.

**Guard tests:** the `internal/controller` package tests (both host matrices
through one request/result suite: clean install, no-op, overrides, frozen
retry, contention, cancellation, package failure, unknown outcome and exact
retry), `cmd/bootwright/controller_*_test.go`, the
`internal/workspace/contextfs/controller_*_test.go` checkpoint tests and
`make ansible-check` (pinned syntax, lint, sanity, unit and host-independent
integration targets).

**Constraints left behind:** authenticated or private-trust setup acquisition
needs its own Secrets consumer and recovery contract before promotion, with
exit evidence covering exact binding, reopen and release, certificate
validation, non-disclosure and interrupted acquisition with changed or
unavailable credentials; additional host families and architectures need
separate matrices; controller relocation and restore remain C14; there is no
host uninstall or automatic OS upgrade; resolution verifies the Python archive
and wheels in disposable staging and the bundle step acquires them again, so a
fresh setup transfers them twice. End-to-end acceptance against a real
controller is operator-run.

### M1e — lifecycle engine and managed artifact serving

**Owners:** State reconciliation and Infrastructure services, using Workspace,
Secrets, Machine, Controller and Trust.

**Outcome:** the durable lifecycle engine and its first domain capability,
making `plan`, `status`, `apply` and `destroy` available: plans, operation and
block state, leases, private logs, exact continuation, unknown-outcome
resolution, immutable secret binding and the Go-to-Ansible capability boundary,
with the managed `ArtifactServer` as one implementation behind the capability
port ([state reconciliation](../state-reconciliation.md),
[infrastructure services](../infrastructure-services.md)). Public destroy
shipped with it, so the staged-availability exception was never entered.
Controller placement is the qualified arm; SSH placement is implemented and
operator-run.

**Guard tests:** the reconciliation domain suite, the `reconciliation/lifecycle`
journey suite, the `operationstore` record and log tests, the `contextfs`
checkpoint tests, the capability and adapter suites under
`internal/infrastructureservices`, the CLI lifecycle goldens, and the example
acceptance in `cmd/bootwright/lab_rhel_example_test.go`.

**Constraints left behind:** content publication into a served root is C20 and
ISO construction C12; neither may append to a frozen plan. SSH placement has no
cross-context conflict coordination, so two contexts targeting one SSH host
remain the operator's responsibility. Executed service effects, the service
adapter's process and cancellation boundary, and SSH placement against a real
host are operator-run.

### M1f — managed controller network services

**Owners:** Infrastructure services and State reconciliation, using Workspace,
Secrets, Machine and Controller. **Requires:** M1e.

**Outcome:** the complete `infra-components` stage. Managed `Proxy`,
`DNSServer` and `NTPServer` join the managed `ArtifactServer` behind the same
capability port, so a controller service set applies, replays and destroys as
one unit, with configuration derived from the selected graph, never authored
daemon syntax ([managed network services](../infrastructure-services.md#managed-network-services)).
Its supported shape is one complete selected Environment whose lifecycle
objects are managed `Proxy`, `DNSServer`, `NTPServer` and persistent
`ArtifactServer` objects placed on OS-ready provided Machines; any other
selected object that would require an effect refuses before registration,
naming each unsupported object and the reduced example. Each kind runs one
container image, digest-pinned in its capability's catalog and qualified
against a real container runtime on the date recorded in
[development](../../docs/development.md). The engine gained
[stage selection](../state-reconciliation.md#stages-and-the-pause-boundary):
`plan` and `apply` accept `--stage`, every block carries a frozen stage, a
selection gates which blocks start without narrowing the plan, and an
operation that can start nothing further reports `paused` and is continued by
a later `apply` or removed by a `destroy` of the blocks it completed.

**Guard tests:** the stage domain suite (readiness, startability, deferral
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

**Constraints left behind:** controller placement is the qualified arm; SSH
placement uses the same request and evidence contract and is operator-run.
Executed service effects, the process and cancellation boundary of the service
adapter, and SSH placement against a real host are covered by no in-tree gate.
The operator-run acceptance it named, the journey of the since removed
managed-infra-components example (staged apply, replay, interrupt, continue and
destroy against a real container runtime), the single-service lab-artifacts
journey and SSH placement against a second OS-ready host, is unrecorded. Its consumer is now [`examples/machines/lab-rhel`](../../examples/machines/lab-rhel).

### M1g — controller prerequisites by selecting scope

**Owners:** Controller and CLI, using Workspace and State reconciliation.

**Outcome:** controller prerequisites split by what selects them, so one
prepared host serves every context created on it; [Controller](../controller.md)
owns the scopes and their commands. One CLI noun, `controller`, names the
domain, its host and its Machine. The root `setup` selects no context: it reads
no Environment, consumes no `--context`, prepares the host foundation, the
private Python and `ansible-core` bundle and the baseline native closure, and
publishes no binding. `preflight controller` keeps its context arm and reports
each check with the scope that owns it, so a negative report names either
`setup` or that context's controller stage. `Environment.spec.dependencyVersions`
lost `python`, `ansible`, `podman`, `openssh` and `nmstate`, which no single
context may move. The context-to-host binding is published by the first
`apply`, under its lock and lease, before any reservation or effect.

The `controller` stage is the first stage, with the engine-owned edge that
makes every other block wait for a controller block.
[Its capability](../controller.md#the-controller-stage) installs the target
client closure and the libvirt client a context selects on the controller
Machine through that Machine's proxy choice; a context selecting nothing beyond
the host baseline contributes no block. The clients go into a
[client area](../contexts/controller-record.md#tool-areas): a
shared host namespace content-addressed by the exact closure, reserved before
its directory exists, attributed to it and sealed once the tree is durable. The
identities a stage will acquire are retained before acquisition, the native
transaction publishes its before-state into the running attempt before it is
authorized, and two contexts selecting the same clients prove the same files; a
destroy retains them.

**Guard tests:** the `internal/controller/clients` suite (no block without a
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
block [`examples/machines/lab-rhel`](../../examples/machines/lab-rhel) plans; and `make check`.

**Constraints left behind:** an executed native client transaction, an executed
target-client publication and the RHEL libvirt refusal against a real entitled
source are covered by no in-tree gate. The operator-run acceptance it named,
`apply --stage controller` for a context selecting OpenShift clients and Helm
followed by a repeated apply that reports `unchanged` without publisher access,
and the same for a context declaring `libvirt` on Fedora, is unrecorded. An
operator upgrading a host runs `setup` again before the first `apply` of each
context, because no binding exists until then.

## Out-of-sequence slices

Each was delivered outside the milestone sequence and adds no exit gate to
another slice. Each except X10 was delivered on explicit request.

### X1 — Machine commands

**Owners:** Machine, with State reconciliation (the bounded runtime and the
durable evidence a Machine command reads) and Substrate (the power operations
it consumes).

**Outcome:** `machine list` reports every selected Machine as its context's
durable evidence proves it, filtered by cluster membership alone. `machine rsh`
and `machine exec` open one SSH session under X3's contract, which replaced the
printed descriptor first delivered here. `machine start`, `machine stop` and
`machine restart` converge one Machine to a power state through its own
management controller over the one Ansible boundary, registering no operation
and publishing no ownership. A power run retains what its adapter printed as a
[bounded run](../cli/output.md#bounded-run-output) and names that directory
before the adapter runs, so a refused run leaves something to read
([power operations](../substrates.md#identity-and-power-operations)).

**Guard tests:** `internal/machine/inventory`, `internal/machine/access` and
`internal/machine/power` package tests, the `internal/cli` presentation tests
for the three result shapes, the `internal/reconciliation/lifecycle` runtime and
ownership tests, and the collection's `test_machine_power_protocol.py` and
`test_redfish_boot.py` unit suites.

**Constraints left behind:** a power operation holds the context's shared lock
for its whole run, so it waits on a running lifecycle mutation and delays one
that starts while it runs; and it reports no progress beyond naming where it is
writing, so a graceful stop polling a guest to off prints nothing more until it
settles.

### X2 — Machine lifecycle position, addresses and power reading

**Owners:** Machine, with CLI (the listing's columns and its machine envelope).
**Requires:** X1.

**Outcome:** `machine list` separates
[what it reports](../cli.md#resource-inspection-and-explicit-access): the
lifecycle position this context's own evidence proves, named for the verb that
last acted on the Machine and whether it completed; every IP the Machine
declares beside the one contact its SSH access resolves; and, under
`--power-status`, the power its own management controller reports. The
lifecycle vocabulary replaced `owned`, `released`, `pending` and `unmanaged`
with no compatibility path; the envelope's `state` field is now `lifecycle`,
and `ips`, `power` and a result-level `powerRead` join it. A reading is a
second implementation of the Redfish power capability, `machine-power-read-v1`
over the `machine_power_read_redfish` role, bound without a lifecycle
capability of its own as day-2 power is. It freezes one survey per placement
host, so one bounded run answers for every Machine behind that host, and it
registers no operation, publishes no evidence and names no retained output: an
inspection answers or refuses, leaving nothing to resume or read.

**Guard tests:** `internal/machine` address tests, `internal/machine/inventory`
selection and service tests, the `internal/machine/power` reading survey and
evidence tests, the `internal/cli` listing presentation tests, and the
collection's `test_machine_power_protocol.py` reading cases.

**Constraints left behind:** `--power-status` opens the same bounded runtime a
power verb opens, so it takes the context's shared lock and waits on a running
lifecycle mutation (C7); a Machine whose management controller cannot be
resolved is left out of the survey and reports no reading rather than a
diagnostic, so a misconfigured controller reads there as one this context does
not reach and is diagnosed by a power verb instead; the reported addresses are
the declared ones, so a Machine addressed by DHCP shows none; and no reading
has run against a real host.

### X3 — Machine SSH sessions and host trust

**Owners:** Machine, with Trust (the host-key record format and the
`known_hosts` grammar), State reconciliation (the material one session binds and
the installation evidence it proves a key against) and Workspace (the context
trust area). **Requires:** X1, M1c and M1e.

**Outcome:** `machine rsh` and `machine exec` open the session under
[the session contract](../cli.md#machine-ssh-sessions): one resolved identity,
a host key proved from exactly one authorized source before any credential is
offered, session material passed as inherited descriptors, and the client's
exit status as the result. `machine trust` maintains the
[context trust store](../contexts.md#storage-locking-and-publication) those
sessions read, delivering C27: a context area of its own,
`state/trust/hosts.json`, holding public keys alone; an observed key enters it
only through `machine trust` or an explicitly confirmed interactive first use;
and `--replace` is the one path that supersedes a recorded key. An installed
Machine keeps proving its key against its own
[installation evidence](../substrates.md#identity-and-power-operations) and
consults no store.

**Guard tests:** `internal/trust` record and grammar tests, the
`internal/workspace/contextfs` trust-area tests, `internal/machine/access`
resolution and trust-precedence tests, `internal/machine/sshlocal` argument,
policy and descriptor-passing tests, `internal/trust/enrollment` plan tests,
the `internal/cli` session and trust presentation tests, and the
`internal/reconciliation/lifecycle` evidence and material tests.

**Constraints left behind:** the session's client runs with the elevated
invocation's privileges, so an `auth.operatorIdentity` Machine is reached with
the controller root account's own identity files and no agent, and an operator
offers their own key with `--ssh-id-file`; a `passwordRef` Machine has the
client prompt rather than answering for it; and proving an installed Machine's
key reads durable evidence under the context's shared lock, so a session to one
waits on a running lifecycle mutation exactly as a power operation does (C7).
See [SSH session knowledge](../../.agents/knowledge/ssh-session-boundary.md).

### X4 — Concurrent block execution

**Owner:** State reconciliation, with Managed OS (the first block that names a
resource it will not share) and CLI. It delivers the execution half of C7,
which keeps only the lease-only mutation boundary.

**Outcome:** an operation
[starts every startable block up to a bound](../state-reconciliation.md#stages-and-the-pause-boundary),
so blocks its graph never ordered against each other run together: after the
controller stage, one Environment's managed services and its provider host go
out at once, and every Machine and its installation follows its own chain. The
plan is frozen [wave by wave](../state-reconciliation.md#plan-and-execution),
so the numbered list an operator confirms is the order work starts, and a
definition may name host resources it will not share, which the first
installation of a shared package tree uses. An unproved effect is still observed
before anything else, a failed block is still retried alone and once, a failure
and a cancellation both admit nothing further and wait for what is in flight,
and the bound is the executable's own, never anything the plan or desired state
names. The wave-major order changed every plan digest, so a context an earlier
build applied is destroyed with that build first.

**Guard tests:** the `internal/reconciliation/lifecycle` scheduler suite
(blocks running together within the bound and never past it, a dependent that
waits while its siblings run, two blocks naming one resource that never
overlap, a failure that keeps every block in flight and names each failed block
in plan order, an interrupt that starts nothing more, concurrent observations
that admit nothing beside them, a retry that runs alone, and a stage boundary
that still pauses), the `reconciliation` plan suite (wave order, no block
before its dependency, and frozen exclusive sets through inverse and
narrowing), the `operationstore` concurrent-record test, the
`managedos/installation` shared tree cases, and the whole Go suite under the
race detector.

**Constraints left behind:** an apply and a destroy against a real host are
operator-run, and none has run under this delivery; the operation still holds
the exclusive root lock for its whole duration, so a concurrent reader waits
(C7); the removal's quiescence probes still run one block at a time, because
they settle as a single check row; the bound is one number for every kind
of block, so a host that cannot carry several image builds at once is served by
narrowing the bound rather than by a per-resource limit; and
[X11](#x11--audit-phase-0-and-spec-restructure)'s S4a binds
that bound to one block until S4b gives every role private scratch, so no
operation runs blocks together yet. See
[concurrency knowledge](../../.agents/knowledge/concurrent-block-execution.md).

### X5 — Sequential idempotence and the removal gate

**Owner:** State reconciliation, with every lifecycle capability (each defines
what its own assets being in use means) and CLI.

**Outcome:** repeating a verb is safe and a removal never destroys work that is
still running. A verb whose work durable state already proves
[settles](../state-reconciliation.md#lifecycle-unit) without an effect,
authorization or confirmation, rather than refusing. A resolution that proves a
target is this context's own and part way realized
[fails its block](../state-reconciliation.md#attempts-and-unknown-outcomes)
instead of leaving it unproved, so the next attempt converges it and a removal
is admitted; only a foreign or unreadable observation stays unknown. A fresh
removal
[proves every asset it would take back is out of use](../state-reconciliation.md#quiescence-before-removal)
before it registers, refusing `lifecycle.live` with no operation and naming the
command that stops each one, while every inverse revalidates its own target and
fails rather than forcing: the machine inverse no longer cuts the power to a
running domain.

**Guard tests:** the `internal/reconciliation/lifecycle` journey suite (a
settled apply and destroy that touch no record, a changed input that refuses, a
partial resolution converged by a retry and removable by a destroy, a refusal
that registers nothing and releases its binding, every block probed, an
unprovable probe treated as live, a superseding removal gated and a
continuation not), the `reconciliation` resolution table tests, the
per-capability partial classifiers, the Machine quiescence classifier and the
derived answers beside it, the collection's `test_removal_never_forces.py`,
`test_substrate_libvirt.py` and protocol suites, and the CLI settled-result
goldens.

**Constraints left behind:** the gate observes the Machines alone and derives
every other block from them, so nothing proves that a service, a published
artifact or a provider network is out of use, and a consumer outside the
context is never seen; those Machine probes retain no adapter output, because
the gate runs before any operation is registered, and report as one check row;
and a Machine whose state cannot be read is reported live, which refuses a
removal on a host whose hypervisor is down until it answers again.

### X6 — Removal of an apply that did not finish

**Owner:** State reconciliation, with CLI (the proof it reports and the next
step it offers).

**Outcome:** an operator who interrupts an apply, or whose executor dies under
one, can take the environment back. The record of an attempt's outcome is
[written under a boundary the interruption does not reach](../state-reconciliation.md#attempts-and-unknown-outcomes),
because every operation-store write refuses a cancelled context. A fresh
removal
[supersedes any apply that has not completed](../state-reconciliation.md#continuation-and-removal)
over every block it started, resolving each unproved effect first as a
read-only step that registers nothing, so the road out no longer runs through a
continuation frozen to the automation that failed. Ownership does not move as a
block resolves, which is what lets the plan be confirmed before the outcomes it
covers are proved. A removal re-proves, under the exclusive lock, that the
context still holds the operation it was planned from.

**Guard tests:** the `internal/reconciliation/lifecycle` journey suite (an
interrupted block and an interrupted resolution recorded `unknown`, every block
of a concurrent interrupt recorded, a removal that resolves then removes for
each of the three resolvable outcomes, a dead executor's running block
resolved, an apply with nothing unproved removed without observing, resolution
composed with the quiescence gate and not repeated by the removal that follows,
a removal refused when the operation it replaces moved, and an unprovable
effect refusing before registration), the `reconciliation` ownership tests (an
unproved block is owned, and the owned set does not move when it resolves), and
the CLI journeys that cover the interrupt exit code.

**Constraints left behind:** a removal performs one adapter observation per
unproved block before it registers, and those observations report as one check
row and write to the log tree of the operation they resolve rather than to one
of their own; an effect no observation can prove still refuses the removal, so
C23's remainder is exactly a foreign or unreadable target; and the resolution
runs under the build in hand, which makes the repaired-adapter road work but
means a frozen request this executable cannot read refuses there instead.

### X7 — Retiring a superseded execution bundle

**Owner:** Controller setup, with Workspace (the area it removes and the record
that accounts for it).

**Outcome:** `setup --purge-old-bundles` (X88 removed it)
retires the execution bundles this host no longer needs, after the setup it
runs beside completes and only then. Superseded areas are read from the
resolutions the host retains: every one the completed receipt does not name.
The store refuses the bundle that receipt names and every client closure,
whatever it is asked to retire, and an area it does not hold is already gone.
The resolution a retired bundle carries goes with it, which returns capacity:
the retained resolutions and bundle areas a host may hold are bounded at
sixteen, and reaching that bound refuses every later setup.

**Guard tests:** the `internal/controller/prerequisites` service tests (only
superseded bundles of a completed setup, nothing retired without one, a failed
retirement reported) and the `contextfs` store tests (the superseded area
removed and the current one kept and still readable, the receipt's own bundle
refused, an absent area completing, and an interrupted retirement left
unreadable and completed by repeating it).

**Constraints left behind:** retirement is an explicit operator journey rather
than something a setup does on its own, so a host that never asks still reaches
its bound; the client closures a context installed are never retired, because
nothing uninstalls them (M1d left no host uninstall); and an automation-only
revision still republishes a whole bundle rather than layering over a shared
foundation, which is what remains of C24.

### X8 — The context-free acquisition route

**Owner:** Controller setup, with CLI (the invocation that admits it), Managed
OS (the media import that shares it) and Security (the one environment value a
privileged re-execution carries).

**Outcome:** `setup`, `preflight controller` without a context and `media add
--from-url` acquire before any Environment exists, so they read their route
from the invoking environment under
[the context-free acquisition route](../controller.md#the-context-free-acquisition-route):
`HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` in either case, one grammar shared
with a declared external Proxy, credential-free by refusal, transport only, and
recorded in the receipt like any other route. Everything a context drives keeps
its Machine's proxy choice, which the selection type enforces structurally:
substituting an ambient route onto a selection that named a controller Machine
returns that selection unchanged.

**Guard tests:** `internal/controller` (the environment grammar, both
spellings, the conflict refusal, the HTTPS-only rule, bypass parsing and
matching, bounds, and that no route is read without an explicit lookup),
`internal/cli` (exactly which invocations consume it, and the route line in the
scope block), `internal/controller/privilege` (assignments forwarded only under
the fixed names and only before the option terminator),
`internal/controller/prerequisites` (context-free setup acquires over it, a
selected context ignores it, and an interrupted setup refuses a changed one)
and `internal/managedos/medialocal` (an import takes the supplied route and
fails closed without a usable one).

**Constraints left behind:** sudo forwards the route on its command line, so a
sudoers rule granting neither `ALL` nor `SETENV` refuses it and the operator
must run as root instead
([sudo knowledge](../../.agents/knowledge/sudo-command-line-environment.md)); an
authenticated or TLS-inspecting proxy is unsupported everywhere, so this route
carries no credential and needs no Secret consumer; and `ALL_PROXY` is ignored
rather than refused, because no SOCKS route is qualified.

### X9 — Removal under the build in hand

**Owner:** State reconciliation, with every lifecycle capability (each reads
its own frozen request) and CLI.

**Outcome:** a context is removable by the executable an operator holds,
without rebuilding the one that applied it. A fresh removal is
[planned from the plan its apply froze](../state-reconciliation.md#continuation-and-removal)
rather than re-derived and compared, so a capability whose content digest or
derivation moved no longer refuses every context that applied under the old
one. Each capability reads its own frozen request to state what removing that
block does, so a removal is planned in the words of the removal it performs and
consumes the authorization removing needs, while the block's identity, request
and digests stay exactly as frozen. Every capability reads exactly the request
version it writes, and a fresh removal reopens the binding its apply froze
instead of binding what current declarations name, so an edited declaration
does not strand a context. What cannot be read refuses before registration,
naming the block, its request version and the executable identity the
operation recorded; `status` reports that identity too, so the remedy is
available before the refusal is met.

**Guard tests:** the `internal/reconciliation/lifecycle` journey suite (a
removal across a moved content digest and across a superseded failed apply; a
refusal that names an unreadable version and registers nothing; a refusal that
names an implementation this executable no longer provides; a removal that
reopens its apply's binding rather than binding current declarations), the
per-capability removal and version-refusal decoder tests, and the CLI status
goldens.

**Constraints left behind:** a request of any other version refuses, so a
context whose shape has moved is removed by the executable its operation
records; `destroy` admits no stage or block scope, so the removal covers every
block the context owns; and the bundle half of the same collision is unchanged,
because a sealed bundle is retired by nothing (C24) and an operation continued
rather than superseded still runs the automation it froze (C25).

### X10 — Orphan-acknowledged context delete

**Owner:** Workspace, with State reconciliation (the
[context mutation evidence](../state-reconciliation.md#context-mutation-evidence)
guard) and CLI. Commit `5b4f08f` narrowed a deletion refusal without a slice;
this entry is its slice.

**Outcome:** `context delete --purge` refuses a context that still owns
realized objects and names `bootwright destroy`. `--allow-orphans` acknowledges
exactly what that refusal protects and deletes anyway, abandoning those objects
in place, unmanaged and unreferenced, so no later command can discover,
continue or remove them through Bootwright. It replaces neither `--purge` nor
the ordinary confirmation, whose prompt names the abandonment; missing, corrupt
or unsupported lifecycle evidence and a live lease refuse under it exactly as
without it. The deletion performs no remote effect, warns once on standard
error and reports the abandonment in its result
([permanent deletion](../contexts.md#permanent-deletion),
[`context delete`](../cli/commands.md)).

**Guard tests:** `TestOrphanAcknowledgementIsRequiredToDeleteAContextThatStillOwnsObjects`,
`TestOrphanAcknowledgementNeverBypassesUnreadableEvidenceOrALiveLease`,
`TestOrphanAcknowledgementClaimsNothingAndReplacesNoOtherSafeguard` and
`TestOrdinaryConfirmationNamesTheObjectsADeletionAbandons` in
`internal/workspace/contexts/service_test.go`, and
`TestAbandonedOrphansAreReportedOnceOnStandardError` in
`internal/cli/context_results_test.go`.

**Constraints left behind:** abandonment recovers nothing, so it is the only
local exit for C23's unresolvable block; and [secrets](../secrets.md) still
forbids abandoning a protected context while [security](../security.md) does not
name the waiver, which X11 reconciled.

### X11 — audit Phase 0 and spec restructure

**Owner:** Architecture, with every context the 2026-09 audit's Phase 0
touched. Landed on `main` at `8aa4494` through pull request 1 on 2026-09-26.

**Outcome:** the audit's Phase 0 items S1, S2a, S3a, S4a, S5, S6a, S7 to S9
(first steps), O1 to O8, Z1, G1 to G9 and A1 to A8, and the F3 decision.
Destructive paths the code cannot prove refuse: a physical installation
without a named root device or while its host key would be public, a physical
or install-profiled cluster node, and a device path outside `/dev/` or its safe
character set. Lifecycle concurrency is one, a failed block retries alone and
only inside the selection, registration re-proves the basis it decided on, and
contextfs publications are atomic and leave no stage. Verification is tiered
(`make quick`, `make docs-check`, `make race`, `make check-offline`) and CI runs
`make check`; guidance routes to one owner per fact; unpromoted designs live
under [deferred](../deferred/); and the spec tables that code must match are
tests.

**Guard tests:** `TestAPhysicalInstallationWithoutANamedRootDeviceRefuses`,
`TestAPhysicalInstallationRefusesWhileItsHostKeyWouldBePublic`,
`TestAPhysicalKickstartNeverClearsADiskItDidNotName`,
`TestUnsupportedNamesEveryClusterWithANodeThisContractCannotBoot`,
`TestDevicePathRejectsNewline`, `TestLifecycleConcurrencyBound`,
`TestFailedBlocksNamingOneResourceAreRetriedOneAtATime` (one shared resource
only since D144),
`TestContinuationRefusesWhenTheContextChangedBeforeMutation`,
`TestPublicationLeavesNoStageOnFailure`, `TestTransitionTablesMatchSpec`,
`TestDiagnosticCodesMatchOutputSpec`, `TestDocumentedBoundsMatchCode`,
`TestCommandCatalogMatchesSpec` and the docs gate in
`test/architecture/docs_test.go`.

**Constraints left behind:** M5a stays blocked on S3b and M4a's physical nodes
refuse until S2b; concurrency stays one until S4b and a real-host run;
`vulncheck` and the networked `ansible-test` suites run only in CI where a
session's egress refuses their hosts; architecture.md stays near 50 KB; and
every item not executed became an audit follow-up, now an item of a milestone
page, [parked](backlog.md#parked) or retired.

### X12 — audit Phase 1: context and guards

**Owner:** Architecture, with State reconciliation, Workspace, Controller and
CLI. Landed on `main` at `ecaef1a` through pull request 2 on 2026-09-26, on
explicit request.

**Outcome:** the audit's decision-free Phase 1 items. A required-log failure is
a durable, sticky operation fault that only a later invocation re-proving its
log clears; a destroy over an apply that started nothing completes; a
resolution that cannot start leaves the operation unchanged (S10, Phase 1
part). `plan` previews a fresh operation through the decision `apply` makes
(F1). The ambient proxy route follows the flags that decide acquisition (F2).
`status --watch`, `--watch-interval` and `--verbose` are withdrawn, and no
request field may go unread (F3). `media add` acquires with no root lock held
and publishes under a short exclusive hold that re-proves its stage (F4).
Fitness ratchets fail on stale entries, the collection's Python has a
function-length check and the diagnostic registry walks package variables
(R6). The developer and operator guides are separate and the lab-baremetal
journey is indexed (G8, rest).

**Guard tests:** `TestALogFaultLatchesCancellationAndItsRecord`,
`TestARestorationWhoseClearFailsStartsNothing`,
`TestADestroyOverAnApplyThatStartedNothingCompletes`,
`TestAResolutionThatCannotStartLeavesTheOperationUnchanged`,
`TestFreshPlanAndApplyShareOneDecision`,
`TestOnlyContextFreeAcquisitionForwardsTheInvokingRoute`,
`TestWithdrawnFlagsAreUsageErrors`, `TestEveryRequestFieldIsRead`,
`TestMediaAddHoldsNoRootLockWhileItAcquires`,
`TestASubstitutedStageNeverReplacesTheStoredImage` and
`TestDiagnosticCodesAreFoundInPackageVariables`.

**Constraints left behind:** backlog S10 (rest), F1 (rest), F4 (rest) and
R6 (rest); the pre-existing F12 and S12 it surfaced became X13; and
`ansible-check --suite units` and `vulncheck` ran only in CI.

### X13 — scheduler liveness and completed-removal proof

**Owner:** State reconciliation. Landed on `main` at `da960b1` through pull
request 3 on 2026-09-26, on explicit request.

**Outcome:** a block whose attempt cannot start stops admission instead of
being re-admitted without end under the root lock, keeps its state, and the
invocation reports the start failure; `cause()` keeps an error without a
diagnostic as `runtime.internal` (F12). A destroy after a completed apply
refuses before it registers when any block of that apply is not `done`, so a
lost block record can no longer leave an effect unremoved (S12).

**Guard tests:** the journeys and unit tests in
`internal/reconciliation/lifecycle/scheduler_liveness_test.go` and
`internal/reconciliation/lifecycle/completed_removal_test.go`, and
`TestTransitionTablesMatchSpec` for the narrowed `paused` row.

**Constraints left behind:** backlog S12 (rest) and the pre-existing S13 it
surfaced.

### X14 — adapters, serving and cluster identity

**Owner:** Architecture, with State reconciliation and Container cluster.
Landed on `main` at `732f99b` through pull request 4 on 2026-09-26, on
explicit request.

**Outcome:** a refused adapter record closes the acknowledgement channel and
ends a lifecycle adapter's process group at once, while the controller runner
releases a waiting adapter without killing an authorized native transaction
(S14). A lifecycle adapter dies with its invocation through a parent-death
signal and a subreaper walk in the supervisor, and a hangup cancels like a
terminate, relayed through the sudo supervisor (S15). A still-running earlier
adapter refuses the next run, and stale run directories are removed with the
secret files they hold, never through a link (S16). Private published files
are readable by the serving worker and nothing broader, and a published agent
image is fetched through the listener with its certificate verified (S17). The
install identity is the build's kubeconfig trust anchor instead of a
`metadata.json` the agent installer never writes (S18). A node off the artifact
server's host and a multi-node cluster on another platform refuse before
registration (F13).

**Guard tests:** `TestAProtocolRefusalEndsTheAdapterPromptly`,
`TestAProtocolRefusalReleasesAWaitingAdapter`,
`TestHangupCancelsTheOperationLikeTerminate`,
`TestHangupIsRelayedToThePrivilegedOperation`,
`TestAnAdapterStillRunningRefusesTheNextRun`,
`TestAStaleRunDirectoryIsRemovedWithItsSecretFiles`,
`TestTheSweepNeverFollowsALinkOrTouchesWhatIsNotItsOwn`,
`TestANodeOffTheArtifactServersHostRefusesBeforeRegistration`,
`TestAMultiNodeClusterOnAnotherPlatformRefusesBeforeRegistration`, and the
collection tests
`test_every_file_beneath_a_served_root_is_readable_by_the_worker_and_nothing_broader`,
`test_the_identity_is_the_anchor_the_image_build_wrote_and_needs_no_metadata`
and `test_a_lifecycle_playbook_dies_with_its_killed_supervisor`.

**Constraints left behind:** backlog S15 (rest), S16 (rest), S17 (rest), the
S14 edges recorded in C28 and the per-object reasons in F7; S26, which X15's
review found: the identity does not survive the installer's install-complete
rewrite of the kubeconfig; the M4a operator gate still waits for X16.

### X15 — installs that neither strand nor over-report

**Owner:** Container cluster, with State reconciliation and Controller. Landed
on `main` at `580de0a` through pull request 5 on 2026-09-27, on explicit
request.

**Outcome:** completion and the settled decision require ClusterVersion
`Available` and a completed newest history entry at the declared release, so an
install interrupted during its completion wait is waited for again rather than
recorded done (S19). Nodes presenting this block's own tokenized image prove a
partial install while the API is silent, and a retry neither re-inserts media
into nor re-arms a boot override on such a node (S20). The install marks its
work area before any node is handed the image, a media rebuild over a marked
area refuses before any effect, and a replay that skips the build still serves
and probes the image (S21). A start interrupted between its two writes is
adopted by the next (S13). Byte goldens pin the cluster requests and evidence
and the managed-OS request (T1). The automation digest stays over every
embedded file, pinned by a test, because documentation can leave it only with
the bundle attribution checks (Z2, digest).

**Guard tests:** `TestFrozenRequestsMatchTheirGoldens`,
`TestTheFrozenRequestsMatchTheirGoldens`, `TestInstallEvidenceMatchesItsGoldens`,
`TestMediaEvidenceMatchesItsGoldens`,
`TestAnInstallStoppedBeforeTheAPIAnswersIsPartialOnlyOnItsOwnImage`,
`TestAStartInterruptedBetweenItsWritesIsAdoptedByTheNext`,
`TestAStartAdoptsNothingButAnInterruptedStart`,
`TestAutomationDigestCoversEveryEmbeddedFile`, and the collection tests
`test_available_and_a_completed_history_at_the_declared_release_is_completed`,
`test_a_node_running_from_its_own_image_is_not_booted_again` and
`test_a_marked_area_that_would_be_rebuilt_refuses_naming_why_and_the_remedy`.

**Constraints left behind:** backlog Z2 (digest attribution) in X22, and S26,
which its review found, in X16.

### X16 — budgets that bound the run

**Owner:** Controller and Container cluster, with State reconciliation and
Substrate. Landed on `main` at `0e9e6d5` through pull request 6 on
2026-09-27, on explicit request.

**Outcome:** the install identity is a domain-separated digest of the build's
admin client certificate, so it survives the installer's install-complete
rewrite of the kubeconfig (S26, D8 as refined). Every adapter run is bounded
by a deadline its frozen budgets derive, clamped to a 6-hour ceiling; the
managed-OS request freezes its wait budgets (Z2, runner deadline, D16). Every
installer wait, image build and boot phase is bounded in wall-clock time by its
budget, counted in true seconds whatever the controller's time zone, every
`oc` read has a request timeout, and a cluster whose deadline would pass the
ceiling refuses at planning (Z2, cluster budgets). Resolution before boot
proves each frozen name answers its own address and nothing else (S22).
Topology admission accepts only the control-plane counts the release's agent
installer accepts (F14, D9). A silent hypervisor is never read as a stopped
machine (S6b, quiescence).

**Guard tests:** `TestARunIsBoundedByItsRequestsDeadlineUpToTheCeiling`,
`TestEveryCapabilityDeadlineCoversItsFrozenBudgets`,
`TestAClusterWhoseDeadlinePassesTheCeilingRefusesBeforeRegistration`,
`TestTopologyAdmitsOnlyTheControlPlaneCountsItsReleaseAccepts`,
`TestEveryExampleReleaseHasATopologyRow`,
`TestMachineEvidenceWithoutAnAnswerDecodesAsSilent`, and the collection tests
`test_after_the_install_complete_rewrite_a_completed_cluster_still_proves_this_build`,
`test_each_installer_wait_runs_every_attempt_under_what_its_budget_has_left`,
`test_every_budget_clock_counts_true_seconds_across_a_daylight_saving_change`,
`test_every_name_answering_with_its_frozen_address_alone_boots` and
`test_a_machine_removal_refuses_a_silent_hypervisor_before_it_stops_anything`.

**Constraints left behind:** backlog C6 (kubeconfig growth and a truncating
kill), F6 (`::/96` addresses and name-only slots), S6b (rest), F7 (refusal
reasons, including the deadline ceiling's) and Z2 (digest attribution) in X22.
Request versions changed (managed OS v4, cluster media and install v2), so a
machine or marked work area from an earlier build refuses until destroyed.

### X17 — goldens and checkpoint harnesses

**Owner:** every context. Landed on `main` at `ed0e5d5` through pull request 7
on 2026-09-27, on explicit request.

**Outcome:** byte goldens pin every lifecycle and workspace record, the frozen
plan, request, input and automation digests and the mutation evidence, each
read back through its own reader, and
`.agents/skills/code-implementation/references/go.md` states the golden
convention (T1, records). Exact goldens taken at the CLI Runner pin every
available command's JSON and text with its exit code and stderr, and each
example's effective output (T1, CLI). Every contextfs checkpoint is a catalogued
constant, and a harness refuses, cancels or kills every publication at each one;
a kill harness interrupts every lifecycle journey at every durable write; and a
Capability contract suite holds every production binding to its request,
outcome and evidence kinds (T2). What fails today is kept in exact ledgers that
only shrink.

**Guard tests:** `TestWorkspaceRecordsMatchTheirGoldens`,
`TestOperationRecordsMatchTheirGoldens`, `TestPlanDigestsMatchTheirGolden`,
`TestCommandOutputMatchesItsGoldens`, `TestEveryAvailableCommandHasAGolden`,
`TestEveryExampleEffectiveStateMatchesItsGolden`,
`TestAnInterruptedPublicationLeavesAUsableStoreAndItsRetryConverges`,
`TestEveryCheckpointIsACataloguedConstant`,
`TestAJourneyKilledAtAnyWriteLeavesAUsableStoreAndConvergesOnRetry`,
`TestEveryCapabilityHonoursTheCapabilityContract` and
`TestEveryBindingJoinsTheCapabilityContractSuite`.

**Constraints left behind:** the ledgers name backlog S8, S10 (rest), S12 (rest)
and S27, all in X18, and a keyring-initialization stage X18 settles; the goldens
pin F5's status omissions and secret sequence ordinals as they are; T2's flaky
reservation test; and three follow-ups recorded in F4 (rest), R4 and R5.

### X18 — durable records

**Owner:** State reconciliation and Workspace. Landed on `main` at `d2b94c6`
through pull request 8 on 2026-09-28, on explicit request.

**Outcome:** an unknown destroy block resolves by what its removal proves,
through a `Capability.ObserveRemoval` port every binding implements (S27). A
removal of an incomplete apply refuses the record contradictions it can prove,
and an unchanged apply over a completed apply settles only when every block is
done (S12, rest). An operation whose blocks are all done but whose record,
evidence, reservations or Secret bindings lag is finalized with no token or
confirmation: a completed one by the next apply or destroy, a running or
unknown one only by its own verb; a removal whose final release fails reports
an incomplete finalization, while the receipt of the invocation that
completed it reads done; a fresh apply claims its operation directory and
raises running evidence before it binds a Secret, and bindings no operation
names are collected (S10, rest). Every transition re-proves under the exclusive
lock the full fingerprint it was decided from, and every reader refuses a
frozen plan its operation record does not name (S9, full). One contextfs
publication primitive serves four protocols and removes its stage on any
failure, and a collector under the exclusive root lock removes the stages a
killed publication left (S8, collector and primitive, with R3). The lifecycle
kill ledger is empty. No record, request or plan format changed, but some
records earlier builds wrote now refuse where they used to proceed.

**Guard tests:** `TestAnUnknownDestroyBlockResolvesByWhatItsRemovalProves`,
`TestADestroyOverAnIncompleteApplyWithAContradictedBlockRefuses`,
`TestARepeatedApplyOverACompletedApplyWithABlockNotDoneRefuses`,
`TestARunningOperationWhoseBlocksAreAllDoneIsFinalized`,
`TestAnUnknownOperationWhoseBlocksAreAllDoneIsFinalizedByItsOwnVerb`,
`TestARemovalWhoseBindingReleaseFailsReportsIncompleteFinalization`,
`TestAFreshApplyClaimsItsOperationBeforeItBinds`,
`TestTheNextRegistrationReleasesBindingsNoOperationNames`,
`TestATransitionRefusesWhenItsFrozenPlanChangedBeforeMutation`,
`TestAFrozenPlanThatIsNotItsOperationsRefuses`,
`TestPublicationLeavesNoStageOnFailure`,
`TestTheNextLeaseCollectsWhatAKilledPublicationLeft`,
`TestReadsNeverCollectAStage`, and
`TestAJourneyKilledAtAnyWriteLeavesAUsableStoreAndConvergesOnRetry` with an
empty ledger.

**Constraints left behind:** a killed keyring initialization
(B26, was S28, delivered by [X20](delivered.md#x20--substrate-port-pre-boot-proof-and-observations)), lost and lagging records (B27, was
S29, delivered by [X26](#x26--follow-ups-from-x19-to-x25)), claims and bindings (B28, was S30, delivered by X26), resolution gaps that
need adapter changes (B29, was S31, delivered by [X29](#x29--defects-and-decided-items)) and the protocols the
primitive leaves (B35, was R3 (rest), delivered by X20), with clauses added to B8,
B11, B16, B21 and B30. Gates: `make check` in CI, `make race` and the kill
harness locally, `make docs-check`. No real-host run.

### X19 — Redfish and identity

**Owner:** Substrate, with Machine, Container cluster and Managed OS.
Integrated on local `main` on 2026-09-28 as one commit, on explicit request.
**Items:** B1 (was S23), B2 (was S24), B3 (was S11), B4 (was S25).

**Outcome:** every Redfish effect and every read a consumer decides from goes
through the vendor-neutral client, which discovers the media device, follows
tasks by their state, sends `If-Match`, reads each outcome back, speaks only to
the endpoint's own authority and fails closed on what it cannot read; the
fixed-path helpers are gone (B1). A proving bare-metal apply reports
`unchanged` (B2). A physical Machine's power operations compare the UUID and
serial its done bare-metal apply proved and refuse a mismatch before any
request; emulated Machines consult no pin (B3, D6). Every admitted root-device
hint reaches the agent configuration, which is written with every value's type
kept, a hint the installer cannot carry refuses, the media request is version
3, and a managed-OS installation refuses every hint but `deviceName` (B4).

**Guard tests:** `TestOnlyADoneBareMetalApplyProofPinsAMachine`,
`TestTheProvedIdentityIsReadOnlyFromAPresenceProof`,
`TestProvedEvidenceMatchesItsGolden`,
`TestAProvedMachineCarriesItsIdentityToTheAdapter`,
`TestAnEmulatedMachineConsultsNoPin`,
`TestAnUnreadableProofRefusesBeforeAnyPromptOrRun`,
`TestEveryDeclaredRootDeviceHintReachesTheAgentConfig`,
`TestARootDeviceTheAgentInstallerCannotCarryRefuses`,
`TestAnInstallationRefusesARootDeviceHintItCannotCarry`,
`TestLabSNOExampleCarriesEveryRootDeviceHintToItsAgentConfig`, and the
collection tests `test_every_operation_goes_through_the_client`,
`test_no_operation_builds_a_controller_path_itself`,
`test_a_reference_to_another_authority_is_refused_and_never_sent_the_credential`,
`test_an_unreadable_controller_is_a_failure_not_an_empty_answer`,
`test_a_boot_selection_is_read_back`, `test_the_inspection_contract_is_kept`,
`test_a_proving_apply_publishes_no_change`,
`test_the_identity_is_read_and_compared_before_any_power_request` and
`test_a_string_above_the_bmp_reaches_the_installer_as_a_yaml_escape`.

**Review:** five findings; two confirmed and fixed (a task reported
Interrupted no longer triggers a second insert; code points above U+FFFF
reach the installer as YAML escapes), three rejected.

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` and `./scripts/ansible-check --suite units`, `sanity`, `integration` and
`lint` pass on the integrated slice. No real-host run.

**Constraints left behind:** new items B108 to B113; clauses on B5, B6, B12,
B29, B32, B39, B67, B73 and B96; the emulated labs repeat their rows on a build
with X19, destroying lab-sno contexts applied before it with their own
executable. The upstream Redfish citations were not re-fetched.

### X22 — operator contract

**Owner:** CLI, with Desired state, Machine, Controller and State
reconciliation. Integrated on local `main` on 2026-09-29 as one commit, beside
X19, on explicit request. **Items:** B11 (was F5), B12 (was F6), B13 (was F15),
B14 (was Y2), B15 (was S7 (rest)), B16 (was Z2 (digest attribution)).

**Outcome:** every `--output json` result is a CLI-owned document, JSON mode
writes no progress, human `status` presents the JSON membership with every
next step and each block's attempts, secret JSON carries its sequence
ordinals, and a settled verb names a finalization or release it completed
(B11). Admission refuses what only a consumer refused: BMC addresses outside
one byte-exact http or https grammar (the metal3 schemes included), an emulated
BMC with no bind address or one that is not a nameable IPv4 unicast address,
non-positive libvirt sizes, IPv4-compatible endpoint addresses and name-only
endpoint slots (B12). OpenShift client downloads stream to disk under deadlines
scaled to their sizes within a derived client-stage ceiling, and `oc`'s
release stamp is checked (B13). ansible-core is the latest patch of the
qualified 2.21 minor, the controller Python the newest qualified minor (D20),
`requires_ansible` is bounded and a Python 3.9 floor test parses every
collection module (B14). A lifecycle placement host connects as root and never
escalates, and an escalation secret is never bound (B15, D7). The collection's
README and CHANGELOG leave the automation digest together with every
comparison of it (B16, D14).

**Guard tests:** `TestEveryJSONResultIsCLIOwned`,
`TestJSONModeProgressWritesNothing`, `TestStatusReportsEachBlocksAttempts`,
`TestStatusTextEscapesOnce`, `TestEveryAuthoredBMCAddressNamesOneExactSystem`,
`TestAnEmulatedBMCListensOnOneNameableUnicastAddress`,
`TestAnIPv4CompatibleEndpointAddressIsRefused`,
`TestAnAcquisitionDeadlineScalesWithItsSourceBytes`,
`TestAClientClosureBeyondTheCeilingRefusesBeforeAnsibleStarts`,
TestAnsibleLatestIsTheNewestQualifiedPatch,
TestPythonLatestIsTheNewestQualifiedMinor,
`TestALifecyclePlacementHostConnectsAsRoot`,
`TestAPlacementNeverBindsAnEscalationSecret`,
`TestDocumentationLeavesTheAutomationDigest`,
TestTheRunnerComparesOnlyTheDigestedAutomation (X88 replaced it with `TestARunExecutesTheStagedAutomationAndHoldsItsLock`), and the collection tests
`test_modules_parse_under_the_remote_python_floor` and
`test_each_tool_is_given_its_own_frozen_seconds`.

**Review:** five findings; one in scope, a new Not yet met line that
overstated a continuation's refusal, fixed; one real gap that predates X22
recorded as B115; three rejected.

**Gates:** on the integrated slice and again on the squashed commit over X19,
`make check-offline tidy-check modules-check vulncheck docs-check race` and
`./scripts/ansible-check --suite units`, `sanity`, `integration` and `lint`
pass. No real-host run.

**Constraints left behind:** new items B114 to B122 and clauses on B9, B37,
B45 and B47; B61's first run on this build confirms the client release stamp.
Operator inputs that use a metal3 BMC scheme or omit an emulated BMC's bind
address now fail `validate`. The automation digest moves, so finish or destroy
in-flight operations with the build that registered them, then run `setup`; a
retained 2.19 or 2.20 resolution is resolved fresh.

### X25 — ratchets, milestone status and the storage suite

**Owner:** Architecture, with Workspace, Environment and Desired state.
Integrated on local `main` on 2026-09-29 as one commit, beside X20, on
explicit request. **Items:** B37 (was R6 (rest)), B50 (new), B38 (was T2
(rest)).

**Outcome:** a docs byte budget fails with more than a tenth to spare, so
AGENTS.md, CLAUDE.md and the spec index budgets drop to 2516, 12 and 4656
bytes; an ignored guidance path fails once nothing outside it cites it; each
effect-boundary clause has a fixture proving it refuses what it guards and
that each grant exempts only its own clause (the reading of "per clause",
X25-B37-D2); the diagnostic registry follows a same-file package variable
through every write; and quick-test names a changed directory the default
build omits and tests the package a moved file left (B37). A docs test holds
each milestone's Status to its page, each item row to its section and each B ID
to one page or one delivered record (B50). One contract suite holds the
workspace's operation area and both in-memory doubles to its port; environment
selection and strict YAML each have a table, the environment API's closure
table now describes the code, the merge-key deviation is ledgered as B123, and
the reservation test is deterministic, settling X17's flaky test (B38).

**Guard tests:** `TestByteBudgetsFailWithMoreThanATenthToSpare`,
`TestIgnoredGuidancePathsFailOnceNothingOutsideThemCitesThem`,
`TestEveryEffectClauseRefusesWhatItGuards`,
`TestDiagnosticCodesFollowPackageDeclarationsOfTheirFile`,
`TestQuickTestSelectsChangedPackagesAndNamesWhatTheBuildOmits`,
`TestQuickTestTestsThePackageAMovedFileLeft`,
`TestDocsMilestonePagesAgreeWithTheirStatus`,
`TestDocsMilestoneCheckFindsEachDisagreement`,
`TestOperationAreaHonoursTheAreaContract`,
`TestMemoryAreaHonoursTheAreaContract`, `TestJourneyAreaHonoursTheAreaContract`,
`TestSelectionRetainsExactlyWhatTheClosureTableNames`,
`TestEveryStrictYAMLRuleRefusesWithItsCode` and
`TestStrictYAMLLedgerHoldsEachDeviationToItsOutcome`.

**Review:** three findings confirmed and fixed (a same-file package variable
reassigned elsewhere, a ledger that admitted any failure of the merge-key row,
and quick-test missing the package a moved file left); the older registry and
quick-test gaps are recorded as B124 and B125.

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` passes on the integrated slice and on the squashed commit; nothing
under `ansible/` changed, so the collection suites were not required.

**Constraints left behind:** new items B124 to B128; the `GOOS=darwin
GOARCH=arm64` vet failures in contextfs and the Ansible runner, which X20's B36
settles.

### X20 — substrate port, pre-boot proof and observations

**Owner:** Substrate, with Container cluster, Managed OS, State
reconciliation, Workspace, Secrets and Architecture. Integrated on local
`main` on 2026-09-29 as one commit, on explicit request. **Items:** B6 (was S2b
(pre-boot)), B5 (was A3 (port)), B7 (was Y1 (rest): substrate rules), B8 (was
S6b (rest)), and in parallel lanes B26 (was S28), B35 (was R3 (rest)), B31 (was
F4 (rest)), B30 (was F1 (rest)) and B36 (was R5).

**Outcome:** a physical cluster node's hardware is frozen in the install
request (now `cluster-install-agent-v3`), and every node passes its
substrate's pre-boot proof before its media is inserted; the bare-metal proof
compares the UUID and serial X19 pins, which an apply attempt receives from the
evidence its dependencies proved, and physical nodes and installations still
refuse (B6, D2, D6). Each machine role owns its port as validated entry points
(pre-boot, boot media, boot disk, identity read), a read-only Redfish module
reads power without discovering media, and read-only consumers left the
mutating boot module (B5, D1). Every substrate dispatch ends in a refusal
nothing catches, and an insert follows a pre-boot proof (B7). A libvirt
observation says whether the hypervisor, and each driver daemon, answered;
absence evidence carries observed values (the pool directory, a controller
socket listener, disks by path); a silent host proves nothing and its removal
refuses before any effect (B8). A keyring initialization a kill tore resumes
(B26). The registry and secret area replace through the publication
primitive, and the collector's gaps are closed (B35). Media acquisition keeps a
pinned, verified stage for its repeat, prompts with no lock held, bounds names
by their record and unlocks explicitly (B31). Every preview decides as its
verb does (B30). The privilege boundary moved into a tested package: a sudo
refusal is told from an elevated child that ran (D21, D22), and `make vet` also
vets darwin/arm64 (B36).

**Decided by the session, pending the owner's review:** the brief decisions
A3P-3, A3P-4 (B6 before B5), A3P-5, A3P-7, S2B-2, S2B-3, S2B-5, S2B-6, S6R-2 (a
silent libvirt host proves nothing, so a continuation waits until it answers),
S6R-5, S6R-6, X20-B26-TORN, B30-FINALIZATION, B30-FAILED-DESTROY,
B30-DESTROY-STAGE, B30-BOUNDARY, B31-RETAIN, B31-ADOPT, B31-DISPOSAL,
B31-CONFIRM and B31-NAME, and D21 and D22.

**Guard tests:** `TestAnApplyAttemptReceivesWhatItsDependenciesProved`,
`TestTheInstallationsReadTheBareMetalPinOfTheirOwnMachine`,
`TestAPhysicalNodeWhosePinCannotBeReadRefusesBeforeTheAdapter`,
`TestElevationOutcomes`, `TestElevationOutcomesAfterAnInterrupt`,
`TestStartFilterStripsOneAnnouncementAndPassesTheRestUnchanged`,
`TestEveryPreviewDecidesAsItsVerbDoes`, and the collection's
`test_substrate_port.py`, `test_containercluster_pre_boot.py`,
`test_baremetal_pre_boot.py`, `test_substrate_observation.py` and
`test_role_structure.py` rules; the checkpoint harness case
`secret-initialization/killed/write-file#1` converges with its ledger entry
removed.

**Review:** three findings confirmed and fixed (a host network and pool read
absent although their driver daemon never answered; a media retention lost to
a concurrent prune; the supervisor exiting 128 plus the signal instead of the
child's status), each checked independently. The check found that the last
fix let a child sudo killed at its deadline after Ctrl-C end with 137; such a
child now ends with the interrupt's 130, and three outcome rows guard it.

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` and `./scripts/ansible-check --suite units`, `sanity`, `integration` and
`lint` pass on the integrated slice, after its fixes and on the squashed commit
over X25. No real-host run.

**Constraints left behind:** new items B129 to B135 and clauses on B27, B32,
B49, B108, B109, B112 and B127. The install request is version 3 and the
automation digest moves: finish or destroy in-flight operations with the build
that registered them, then run `setup`. The emulated labs repeat their rows on
a build with X20.

### X26 — follow-ups from X19 to X25

**Owner:** State reconciliation, with Container cluster, CLI, Controller,
Substrate, Machine, Desired state, Infrastructure services and Workspace.
Integrated on local `main` on 2026-09-30 as one commit, beside X27, on
explicit request. **Items:** B27 (was S29), B28 (was S30), B109, B119, B108,
B129, B110, B112, B113, B114, B115, B121, B123, B132, B133, B33 (was F8).

**Outcome:** each lost or lagging record path refuses or converges, and
`status` names every record contradiction (B27); refused fresh applies reclaim
their empty claims, a bounded run's collected binding binds again, a stranded
binding is released and an interrupted done destroy finishes (B28); a block a
resolution completed reads back the evidence and outcome the resolution proved
(B109); the removal-plan test can fail (B119); the node and media margins
derive from the Redfish client's documented bounds (B108); an SSH-placed
install lists each material once (B129); the installer configuration keeps
every value's type (B110); the identity refusal bounds and strips what it
prints (B112); strict JSON refuses trailing closers (B113); human output
escapes each value once (B114); a failed JSON power run names its output in
every failure envelope (B115); the read-only tool inspection streams (B121);
a plain merge key refuses as `yaml.alias` and B123's ledger entry is gone
(B123); the libvirt host apply observes after enabling its driver daemons
(B132); elevated JSON keeps stderr empty and a relayed interrupt leaves the
child its own status (B133); and bridge edges and wildcard-aware socket
reservations order and refuse as the spec says (B33).

**Guard tests:** `TestAVerbOverALostIndexRefusesAndStatusNamesWhatNoIndexAccountsFor`,
`TestAFailedRemovalWhoseBlocksAreAllDoneIsFinalizedByTheDestroy`,
`TestRefusedFreshAppliesReclaimTheirClaims`,
`TestARegistrationRacingABoundedRunCollectsNothingOfIt` (X41 replaced the
binding retry test X26 added),
`TestEvidenceReportsWhatAResolutionProved`,
`TestResolutionOutcomeRecordsWhatTheCapabilityProved`,
`TestTheMarginAllowsEveryControllerCallAnApplyMakes`,
`TestAFrozenPlacementOffTheControllerRefusesEveryVerb` (X29 replaced the
SSH-placed material test X26 added),
`TestNoDecoderTakesMoreAsTheEndOfItsDocument`,
`TestOperationResultTextEscapesOnce`,
`TestAFailedPowerRunNamesItsOutputInEveryFailureEnvelope`,
TestReadOnlyToolInspectionStreamsTheSourceAndEachMember,
`TestARefreshWarningNeverReachesJSONStandardError`,
`TestARelayedInterruptLeavesTheChildItsOwnStatus`,
`TestAServiceBoundToAManagedBridgeRequiresItsProvider` and
`TestAWildcardSocketReservationRefusesEveryAddressAtItsPort`.

**Review:** one finding, rejected. Every item was researched from its own item
text; no separate brief was drafted.

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` and `./scripts/ansible-check --suite units`, `sanity`, `integration` and
`lint` pass on the integrated slice and on the squashed commit. No real-host
run.

**Constraints left behind:** new items B136 to B147 and clauses on B32, B49,
B67 and B73. The automation digest moves: finish or destroy in-flight
operations with the build that registered them, then run `setup`. Refused
record states have no recovery yet (B136).

### X27 — tooling and suites from X19 to X25

**Owner:** Architecture, with Controller, Workspace and CLI. Integrated on
local `main` on 2026-09-30 as one commit, beside X26, on explicit request.
**Items:** B117, B118, B124, B125, B126, B127, B128, B130, B134, B135.

**Outcome:** every collection module imports under a pinned Python 3.9 floor
interpreter in the sanity suite (B117); ansible-core's `latest` resolves from
PyPI's Index API (B118); the diagnostic registry follows the other paths a
code takes (B124); quick-test selects the packages whose tests read a changed
file, and the package a moved file left (B125); the docs tests read only
tracked Markdown, and a guard names every docs check that walks the working
tree (B126); contract suites hold the secret store's area, controller storage
and the lifecycle workspace (B127); the milestone checker refuses completed
items left on a page, planned slices naming absent items, done milestones with
open Requires, unknown Kinds and a stale Next column (B128); the plain
collection test loop loads the collection (B130); shell completion's path
bounds are tested (B134); and the composition root's leftovers are gone (B135).

**Guard tests:** the ones each item's commit names, among them the Python 3.9
import in `./scripts/ansible-check --suite sanity`, the diagnostic registry
fixture rows, the quick-test selection tests, the tracked-walk guard and the
milestone checker rules under `make docs-check`.

**Review:** four findings; three in scope and fixed (a constant declared in
two platform files, two docs tests still reading untracked Markdown, and the
architecture spec's package table), one older registry gap recorded in B150.
The walk guard's precision, which three check rounds narrowed, is recorded in
B148, and the architecture spec now says the guard matches by name.

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` and `./scripts/ansible-check --suite units`, `sanity`, `integration` and
`lint` pass on the integrated slice and on the squashed commit over X26. No
real-host run.

**Constraints left behind:** new items B148 to B157. Each ansible-check cache
needs `python3 scripts/tools/ansible_test_prepare.py` once, so it holds the
CPython 3.9 floor interpreter the sanity suite now pins.

### X28 — test tooling and the collection floor

**Owner:** Architecture, with Controller. Integrated on local `main` on
2026-09-30 as one commit, beside X21, on explicit request. **Items:** B148,
B150, B151, B154, B155, B157, B145, B149.

**Outcome:** the working-tree walk guard matches listings by import path and
knows in-memory filesystems, dot imports and writes through a qualifier (B148);
the diagnostic registry holds a package variable's every write and resolves
exported methods across packages (B150); quick-test selects the packages whose
embedded or test-data files changed and reads quoted paths (B151); the
milestone checker refuses a done page with a planned slice, a planned item whose
Delivery omits the slice, and an empty slice suffix (B154); a gate runs the
plain collection test loop (B155); the boundary test covers the composition
root's signal rule (B157); Go's read-only tool projection checks `oc`'s release
stamp (B145); and the collection's module unit tests also run on the Python 3.9
floor interpreter, whose lock minor is held to the qualified ansible-core, and
an exact ansible-core intent resolves through the Index API (B149).

**Review:** one finding, rejected. Follow-ups were triaged under the rule of
2026-09-30: three defects entered M1 (B158 to B160) and four tooling items
were parked (B161 to B164).

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` and `./scripts/ansible-check --suite units`, `sanity`, `integration` and
`lint` pass on the integrated slice and on the squashed commit. No real-host
run.

**Constraints left behind:** B158 needs the owner's choice between refusing and
warning on a newer Index API minor; until then a PyPI minor bump stops fresh
setups. X28 pins five more Ansible test artifacts, so each ansible-check cache
runs `python3 scripts/tools/ansible_test_prepare.py` again.

### X21 — BMC trust and administrator custody

**Owner:** Managed OS and Container cluster, with Secrets, Infrastructure
services and CLI. Integrated on local `main` on 2026-09-30 as one commit.
**Items:** B9, B25, B10. **Decisions:** D23 to D26.

**Outcome:** a physical management controller may name a CA bundle as
`tls.trustBundleRef`, the only anchor of every Redfish call to it, and every
Redfish task passes it; a certificate the declared trust refuses fails as
unverified instead of reading as an empty answer (D23). A physical Machine's
virtual media imports the media certificate by default and settles it at
eject, with bounded reads, `If-Match` and no fallback; `disable-verification`
is admitted only on one Machine and refused beside private delivery (D24). The
managed-OS, bare-metal and power requests moved version (B9). The cluster media
probe verifies the listener against the certificate part of the server's
Secret, which the media request now names, never the installed copy (B25). The
access a completed installation leaves moves into context custody: the keyring
becomes `local-keyring-v4` with produced material (D25), the install block's
kubeconfig is captured before its attempt completes, withdrawn when the
context's removal completes (D26), revealed by `cluster kubeconfig`, and named
by the orphan-acknowledged delete's confirmation (B10).

**Review:** three findings; one confirmed and fixed: a failed custody capture
recorded the attempt failed, so a later destroy could delete the only
kubeconfig; it now records it unknown, and a continuation or destroy observes
and recaptures first. Two were rejected as behavior the specs require.

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` and `./scripts/ansible-check --suite units`, `sanity`, `integration` and
`lint` pass on the integrated slice and on the squashed commit. No real-host
run.

**Constraints left behind:** a context holding a managed-OS installation, a
bare-metal claim, a power request or a cluster media block frozen by an earlier
build refuses both continuation and removal under this build, and a keyring
written before it refuses, so such contexts are destroyed with the build that
applied them before `setup` runs with this one. The import flow is proved only
in-tree ([B73](m1.md#b73)); the media probe names the controller's file because
media blocks run only on the controller; and every cluster read still uses the
installer's kubeconfig (B170 ([X31](#x31--recovery-setup-custody-refusals-and-adapter-defects))). New items B168 to B170 entered M1,
B171 was parked, and B67 records the refusal its physical nodes lift.

### X30 — setup at the bundle bound, storage suites and reclaim

**Owner:** Controller setup and Workspace, with State reconciliation.
Integrated on local `main` on 2026-09-30 as one commit, beside X29.
**Items:** B44, B153, B139. **Decisions:** D40.

**Outcome:** setup no longer strands a host at the 16-area bundle bound
(B44, D40). When a setup must publish a new execution bundle at the bound,
`setup --purge-old-bundles` first retires every superseded execution bundle and
every area an interrupted retirement left retiring, keeping the receipt's
bundle, the carry-forward source and the new bundle, then publishes and
completes; a failed or canceled receipt counts as settled, and a new receipt
whose bundle cannot be reserved is refused before it is published. Without the
flag the refusal names that command; when only client areas and the current
bundle hold the slots, the refusal says so. Every method of the operation area,
controller storage and lifecycle workspace ports has a storage-suite clause, the
memory doubles enforce the store's transition rules, and the two operation-area
doubles share one entry listing (B153). The kill harness kills a claim reclaim
part way, and the state-reconciliation spec tells a reclaim of ownership, a
non-goal, from removing an empty claim directory (B139).

**Review:** one blocking finding, confirmed and fixed: a failed setup receipt
at the bound still stranded the host. The first check found thin coverage and
narrow spec wording, both fixed; the second check found no gap.

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` passes on the integrated slice and on the squashed commit, and
`./scripts/ansible-check --suite units`, `sanity`, `integration` and `lint`
pass on the squashed commit. No real-host run.

**Constraints left behind:** a host a build before X30 left with a pending
receipt at the bound stays stuck until B175 ([X35](#x35--the-decision-of-2026-10-03)) decides its recovery;
repeating an interrupted retirement (B172 ([X31](#x31--recovery-setup-custody-refusals-and-adapter-defects))), a failed receipt
after a moved automation digest (B176 ([X31](#x31--recovery-setup-custody-refusals-and-adapter-defects))) and resolutions of a kept
bundle (B177 ([X31](#x31--recovery-setup-custody-refusals-and-adapter-defects))) can still stop setup. New items B172 to B178
entered M1, and B179 to B181 were parked.

### X29 — defects and decided items

**Owner:** State reconciliation, Container cluster, Substrate, Managed OS,
Infrastructure services, Desired state, Workspace, Controller and CLI.
Integrated on local `main` on 2026-09-30 as one commit, beside X30.
**Items:** B29, B140, B141, B142, B111, B143, B116, B147, B34, B146, B156, B137,
B144, B152, B131, B158, B160, B159, B169. **Decisions:** D27, D28, D31 to D35,
D44 to D47.

**Outcome:** removal observations run only what a removal needs; a package tree
a killed removal left reads partial, a stopped apply that published nothing
reads no effect, a foreign image on one of the cluster's Machines reads partial
(D27), and a managed service or artifact server with a silent listener reads
partial (B29). The libvirt host and machine replay change nothing and the
SSH-host install contract says its packages resolve at apply time (B34, D28).
The cluster boot budget is 300 seconds per node with a 900-second floor, a
cluster past the six-hour ceiling refuses, and a test holds the node margin to
the install role's calls (B140, D32). The runner refuses a material listed
twice and the agent-install decoders refuse a placement off the controller
(B141, D33). Additional trust bundles reach `install-config`, moving the media
request to `cluster-media-agent-v5` (B142). A libvirt Machine refuses wwn, hctl
and serial root-device hints (B111, D44); a proved UUID or serial refuses
anything unprintable (B143, D34); an emulated BMC admits a canonical IPv6
address with a bracketed endpoint (B116, D45); bridge-bound listeners are
ordered after their bridge and a context's own socket conflicts refuse on the
controller (B147). A YAML key is refused for the construct it carries (B146,
D35); completion bounds its reads and withholds `@` and `=` (B156); `status`
offers only the verbs its records allow (B137, D31); JSON logs and run
remediations name where output is (B144); a missing media callback refuses
(B152) and media publication proves the stage unchanged since it was measured
(B131, D46); a newer Index API minor warns and setup continues (B158, D47); an
unstamped `oc` names the release-stamp check (B160); a probe timeout reads alike
on every Python (B159); and `secret list` reports each version's sequence
(B169).

**Review:** lane reviews fixed their findings before integration; the slice
review found one finding, rejected as the behavior D31 decided. The
integration replaced two citations of the test B141 removed.

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` and `./scripts/ansible-check --suite units`, `sanity`, `integration` and
`lint` pass on the integrated slice and on the squashed commit; on the squashed
commit `sanity` first failed in validate-modules with status 3 and no
diagnostic, then passed on an unchanged rerun (B192 ([X31](#x31--recovery-setup-custody-refusals-and-adapter-defects))). No
real-host run.

**Constraints left behind:** a context holding a cluster media block frozen as
`cluster-media-agent-v4` refuses under this build, and a libvirt Machine
declaring a refused hint no longer plans; the automation digest moves. The
300-second budget is shorter than a libvirt node's worst-case boot under the
controller bounds, which fails as a spent budget an apply can resume. B160's
item text named the wrong remedy: the refusal printed the runner's exit remedy.
The media proof reads status, not bytes, so a same-size write within one coarse
clock tick is not caught (D46). A removal-scoped observation reports fields it
did not read as empty. New items B182 to B192 entered M1 (B188 needs an owner
decision), B193 to B202 were parked, and B17, B19, B39, B61 and B73 gained
clauses.

### X31 — recovery, setup, custody, refusals and adapter defects

**Owner:** State reconciliation, Controller setup, Workspace, Secrets,
Container cluster, Substrate, Managed OS, Infrastructure services, Architecture
and CLI. Integrated on local `main` on 2026-09-30 as one commit. **Items:**
B136, B43, B42, B178, B138, B41, B45, B46, B192, B176, B172, B191, B174, B173,
B177, B168, B170, B40, B39, B182, B189, B183, B190, B184, B186, B187, B185.
**Decisions:** D29, D36 to D39, D41, D42.

**Outcome:** the slice also advanced B32, which stays open. A failed apply
whose blocks are all done is finalized or replaced,
and the two record states that still refuse name a delete (B136, D39); an
unknown block's refusal and `status` say why it stayed unknown, and a context
delete releases its host reservations (B43, D38); a lost frozen binding refuses
before any effect and names its exits (B42, D37); a settling destroy reclaims
an empty claim (B178); the retained-operation bound is 1024, admitted only with
entry room for the removal (B138); the Secret file source is retired (B41,
D36); an operation record freezes its Python and Ansible closure and a
continuation refuses when it moves (B45, D41); setup keeps its Ansible output in
bounded runs, the newest eight (B46, D42); setup survives a failed receipt with
a moved automation digest, a repeated purge finishes an interrupted retirement,
and the store refuses to retire a client area, keeps a retired reserved area
readable and retires superseded resolutions of kept bundles at the bound
(B176, B172, B174, B173, B177); bundle failures name a remedy setup accepts
(B191); a run is lent only the Secret parts it writes (B168); cluster reads use
a kept kubeconfig copy (B170); refusals carry object, reason and remedy except
the parts B32 keeps; each capability spec has a refusal table and an
Environment's `spec.lifecycle.rescue` refuses (B40, D29); the collection's
structural rules hold with the fixes they guard (B39); a managed-OS tree's
remnants are taken back (B182); a managed service left on its earlier files
restarts (B189); the runner refuses unsafe or colliding material names (B183);
run areas keep only output a result names (B190); bare-metal refusals reach the
retained output (B184); the libvirt texts match the code (B186); a host removal
reads no effect from what it takes back (B187); a context's own socket conflicts
on one SSH host refuse (B185); and the example round trip no longer depends on
host load (B192).

**Review:** four findings, two confirmed and fixed: the entry bound was still
reached before the retained-operation bound by operations that ran blocks, and
B45's remedy for a version 1 record named a build that cannot read a host once
setup keeps runs. The checks found a flaky runner test and a reserve that
refused the last apply's removal, both fixed; the last check left one optional
test, for a partial record waiting at the drain, parked as
[B230](backlog.md#b230).

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` and `./scripts/ansible-check --suite units`, `sanity`, `integration` and
`lint` pass on the integrated slice and on the squashed commit. No real-host
run.

**Constraints left behind:** a context retains at most 1024 operations; a
Secret declaring `source.file` refuses, and a context imported with one loses
`status` until `context update`; continuing an operation an earlier build
registered refuses, and once this build's setup keeps runs no earlier build
reads the host, so contexts are destroyed with their build before `setup`; a
context whose install block an earlier build applied has no kept kubeconfig
copy and resolves unknown under this build; an Environment declaring
`spec.lifecycle.rescue` refuses; the automation digest moves. B32 keeps the
stall hint, lifecycle-role refusals and the JSON sudo reason. New items B203 to
B221 entered M1 (B206, B208 and B220 need owner decisions), B222 to B235 were
parked, and B17, B19, B73 and B180 gained clauses.

### X32 — defects X29 to X31 found

**Owner:** Controller, State reconciliation, Workspace, Substrate,
Infrastructure services, Managed OS, Container cluster and Architecture.
Integrated on local `main` on 2026-09-30 as one commit. **Items:** B210, B209,
B215, B207, B221, B205, B219, B204, B212, B211, B213, B214, B216, B217, B218,
B120, B203.

**Outcome:** the slice also advanced B32, which stays open for the stall hint.
The OpenShift install locates its clients from the closure the controller
stage proved, on a real store (B210); a new stage solve retires the resolutions
it supersedes (B209); a controller-hosted libvirt provider gets the libvirt
client (B215); controller remedies name the command that settles them (B207);
the controller runner judges records in any read order (B221); the operation
area keeps room for the last apply's removal at its directory and byte bounds
(B205) and removes a run file whose publication failed (B219); a refusal names
only a context deletion the guard admits (B204); every playbook validates its
request before its first task (B212) and the adapter output prints nothing a
`no_log` task raised (B211); a machine removal reads no effect from what it
takes back (B213); socket claims know when two Machines are one host (B214); a
managed service's observation compares its start with its files (B216); an
installation leaves no empty directory (B217); a node's target refusal names
its own reason (B218); pre-boot and JSON sudo refusals reach the operator
(B32); the unread sudo password fields are retired, moving the artifact
server, managed service, libvirt host and libvirt machine requests (B120); and
the ansible-check area name can no longer form a Python token (B203).

**Review:** two findings, both confirmed and fixed: the tool lookup worked the
closure out again from every source the host retains, so another context's
newer `latest` release hid the proved one; and the stage's proxy remedy named a
command that then refused. The check found no gap.

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` and `./scripts/ansible-check --suite units`, `sanity`, `integration` and
`lint` pass on the integrated slice and on the squashed commit. No real-host
run.

**Constraints left behind:** a context holding an artifact server, managed
proxy, DNS or NTP service, libvirt provider host or libvirt machine block an
earlier build froze refuses continuation and removal under this build; a
request its role does not admit fails at argument validation before the role
acts; a failed `no_log` completion shows only its name; a context whose
operation area holds more than 48 MiB refuses fresh applies (B206, delivered by [X34](#x34--the-decisions-of-2026-10-01));
the automation digest moves. Under D48 every follow-up was parked: B236 to
B252, with clauses on B94, B206 and B229.

### X33 — function splits and one reading of frozen JSON

**Owner:** Architecture, with Controller, State reconciliation, Secrets and
Workspace. Integrated on local `main` on 2026-10-01 as one commit, the last
slice before the M1 freeze (D48). **Items:** B47, B253, B254, B48.

**Outcome:** every production function over the 100-line limit is split into
named helpers in its own file, with code moved in its original order, so both
awaiting-split lists are empty and only shrink (B47, B253 and B254, split from
B47 by owner area so they ran as parallel lanes). Every frozen request is
frozen and read back through one `Freeze`, `Thaw` and `ProveCanonical`, every
adapter's evidence is decoded through one bounded, closed `DecodeEvidence`,
and a fitness test refuses a lifecycle consumer that reads JSON itself (B48).
No frozen byte, request version or golden moved. B48 fails closed in one more
place: a controller-stage proof over 64 KiB or with an undeclared member now
reads as no closure, where a lax reading accepted it; and four refusal
messages are reworded.

**Review:** no finding survived; the finders and the critic traced each split
and each decoder against the base.

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` passes on the integrated slice and on the squashed commit, and
`./scripts/ansible-check --suite units`, `sanity`, `integration` and `lint`
pass on the squashed commit. No real-host run.

**Constraints left behind:** none for operators beyond the reworded messages.
Under D48 every follow-up was parked: B255 to B257.

### X34 — the decisions of 2026-10-01

**Owner:** Container cluster, Controller setup, Substrate and State
reconciliation. Integrated on local `main` on 2026-10-01 as one commit.
**Items:** B220, B32, B208, B188, B206. **Decisions:** D49, D51 to D54.

**Outcome:** the slice also landed D50's guarded path for B175, which stays
open. Before each wait the install role restores a truncated installer
kubeconfig from the kept copy when that copy names the install's own cluster,
refuses a copy of another cluster before the installer runs, and records the
restore in the evidence (B220, D54). After a stall it reads the registered
hosts once from the rendezvous host's assisted-service API with the watcher
token, bounded and under `no_log`, reaching only an address the frozen request
names, and the give-up names each declared node that never registered; a
failed read keeps the earlier hint (B32, D49). A retained bundle that lost a
bootstrap source falls back to a fresh resolution with every verification
(B208, D53). A drifted owned network restarts when its Machines are stopped and
otherwise refuses before any effect (B188, D51). The retained-operation refusal
names destroy, then context deletion and a fresh init (B206, D52).

**Review:** three findings. One confirmed: D50's resume cannot run on a real
host, because every stranded receipt names another executable's automation;
it needs a new owner decision, so B175 stays open. Two were rejected: the
drifted-network refusal reaches the operator only through the retained output,
and its named remedy refuses for the orphaned domains it can name, which D51
required; both are parked ([B266](backlog.md#b266)).

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` and `./scripts/ansible-check --suite units`, `sanity`, `integration` and
`lint` pass on the integrated slice and on the squashed commit. No real-host
run.

**Constraints left behind:** the registered-hosts read sends the watcher token
in cleartext on the machine network and is proved only against the installer's
own generated files ([B260](m3.md#b260)); completion evidence gains a
`restored` member, which a build before X34 refuses; the automation digest
moves. Under D48 every follow-up was parked: B259 to B267. B264 is a
cross-context safety gap: another context's network passes the host's
ownership check.

### X35 — the decision of 2026-10-03

**Owner:** Controller setup. Integrated on local `main` on 2026-10-03 as one
commit. **Items:** B175. **Decisions:** D50, D55.

**Outcome:** at the bound, `setup --purge-old-bundles` cancels a pending setup
receipt stranded there that this executable cannot resume, whether by its
automation, its execution foundation, its route or the dependencies it froze,
when its setup never took effect: its first action, `execution-bundle`, holds
at most its intent and every later action is still planned, the shape every
build before X30 left. The cancellation observes that action as never started,
because the store holds no area for the bundle the receipt names, and is
durable before anything is retired; setup then retires the superseded areas,
sets the host up afresh under this executable and, once that completes,
retires the resolution the canceled receipt carried. One plan names the
cancellation, the retirement and the fresh setup. Without the flag setup and
preflight refuse with `controller.conflict` naming
`bootwright setup --purge-old-bundles`; a stranded receipt that may have taken
effect keeps its `controller.unknown` refusal, and D50's resume stays for one
this executable can prepare (B175, D55). The purge after a completed setup now
names and reports only the areas the store holds, so it neither reports nor
retires a controller stage's resolution, which settles
[B238](backlog.md#retired)'s purge-report clause. The setup help text and its
command row say what the flag cancels.

**Review:** seven findings, five confirmed and fixed: the purge reported the
canceled receipt's bundle, which never held an area, as retired and never
retired its resolution (three findings); the storage double removed a bundle
it held no area for; and a stranded receipt recorded over another ambient
route was never abandoned, which the fix extended to one frozen for another
platform. The first check found two untested conditions of the abandonment, an
imprecise refusal sentence and a guard pinned only against the double, all
fixed; the second check found no gap.

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` passes on the integrated slice, after each fix round and on the squashed
commit. `./scripts/ansible-check` was not required, since nothing under
`ansible/` changed. No real-host run.

**Constraints left behind:** no record, request or automation digest moves, so
no context is stranded. A never-started pending receipt below the bound still
refuses after the host's release moves, with a remedy an upgraded host cannot
follow ([B271](#x42--media-contexts-setup-service-and-machine-admission)). Under D48 every follow-up was parked: B268
to B271.

### X36 — the resolution test off the host's foundation

**Owner:** Controller setup. Delivered out of sequence on the owner's explicit
request after CI failed, and integrated on local `main` on 2026-10-03 as one
commit.

**Outcome:** X29's test of a resolution over a newer Index API version ran
the bootstrap resolution through its last step, which hashes the controller's
own glibc and libgcc against the catalog's Fedora 43 profile, so it passed only
on a host holding exactly those packages: CI's Ubuntu runner failed it on every
push from 35de8f40 on, and a glibc or libgcc update would have failed it
locally. That step is now a port of the resolver, like its publisher reads,
wired to the same qualification in production; the test qualifies without
reading the host and counts that the step ran. Production behavior is
unchanged.

**Gates:** `make quick` and `make check` pass. Built static and run in a private
user and mount namespace with a foreign glibc loader bound over the host's, the
test fails as CI did before the change and passes after it. No real-host run.

### X37 — Go and Ansible dependency versions

**Owner:** Architecture. Delivered out of sequence on the owner's explicit
request of 2026-10-03 ("Bump all dependency versions"), which the owner
narrowed on 2026-10-05 to the Go and Ansible development pins; CI's runner and
the product's own pins, such as its service images, are outside it.

**Outcome:** Go moves to go1.26.8, the newest 1.26 patch: `scripts/go`
selects it and both modules' `go` directives name it, so CI's setup-go
installs the same Go, and no GODEBUG default moves. The tools module moves
govulncheck from v1.4.0 to v1.8.0 with its x/tools, x/mod, x/sync, x/sys and
x/telemetry requirements, leaving its flags, output and exit statuses
unchanged. The Ansible check gate moves to CPython 3.13.16 from
python-build-standalone 20261003, and its lock to ansible-lint and
ansible-compat 26.9.0, cryptography 50.0.2, filelock 4.0.9, MarkupSafe 3.0.4,
platformdirs 4.12.2, pytest-mock 3.16.0 and urllib3 2.8.0, dropping
ruamel.yaml.clib, which nothing requires any more. The
pages that restate these versions move with them, and the
[build knowledge](../../.agents/knowledge/build-toolchain.md) now says that a
worktree nested inside another checkout gets that checkout's VCS stamp. The
[dependency rule](../architecture.md#dependency-selection-and-reuse) says
where each version lives.

**Deliberately not moved:** Go 1.27, whose 1.27.1 is the newest release,
because it changes product behavior ([B273](backlog.md#b273)); the root
module's requirements, each already at its newest release (only three modules
of its graph have newer ones, and tidy drops bumps of them); ansible-core
2.21.4, the newest stable patch of the qualified minor (D10); the gate's 3.13
minor, because `scripts/tools/ansible_check.py` runs ansible-test on 3.13 and
the artifact lock holds cp313 wheels for ansible-test's exact sanity pins, so
moving to 3.14, which fresh setups resolve, re-platforms the gate
([B274](backlog.md#b274)); the 3.9.25 floor interpreter, the last 3.9 build,
and the artifact lock; and the product fixtures
ansible/controller/requirements.txt,
internal/controller/bundlelocal/catalog.json and the import probe in
internal/controller/bundlelocal/probe_linux_amd64.go, which still name
3.13.15 and the earlier wheels, so no automation or bundle digest moves.

**Guard tests:** `scripts/tools/ansible_check.py` refuses any interpreter but
the pinned one and any package whose version differs from the lock;
TestQualifiedAnsibleMinorAgreesEverywhere holds the lock's ansible-core to
the qualified minor; and `make modules-check` and `make tidy-check` hold both
module files.

**Gates:** on go1.26.8, `make check-offline`, `make tidy-check`,
`make modules-check`, `make vulncheck`, `make race` and `make quick` pass; the
full `./scripts/ansible-check` passes on a cache bootstrapped from the new
locks; and on the slice's final tree `make check`, `make docs-check`,
`./scripts/check-commits` and `git diff --check` pass. No real-host run.

**Constraints left behind:** every shared check cache prepared before X37
holds CPython 3.13.15 and the earlier lock, which the gate now refuses: remove
the `ansible-check` directory under the directory `scripts/cache-dir` prints,
then run `python3 scripts/tools/ansible_check_bootstrap.py`. A cache serves one
lock at a time, so a worktree whose tree predates X37 refuses a rebuilt cache
and needs its own `BOOTWRIGHT_CACHE_DIR`. The first CI run after X37 starts
with cold caches: the `.cache` key hashes the tool locks X37 changed and has no
restore keys, and setup-go's key names the Go version. Under D48 every follow-up was parked: [B272](#x44--help-completion-contracts-and-ratchets), CI's race job,
which never runs although two pages say it runs nightly;
[B273](backlog.md#b273), Go 1.27; and [B274](backlog.md#b274), the gate on
CPython 3.14.

### X38 — the real-host run of 2026-10-05

**Owner:** State reconciliation; run by the operator, who ran
[lab-rhel](../../examples/machines/lab-rhel/README.md#run-it) end to end on a host and
accepted its [ledger row](../../docs/acceptance.md#ledger) the same day.
**Items:** B49, B72. **Decisions:** D58.

**Outcome:** the first owner-accepted real-host apply and destroy. On build
`35de8f40`, a clean build descending from X22's landing, Bootwright prepared
the controller, served the four managed services, realized the libvirt host
and a guest behind its emulated Redfish BMC, installed RHEL 9.8 twice, settled
a repeated apply, refused a removal while the guest ran, removed what it had
created while retaining the shared clients, and after a host restart powered
the guest on and off through its BMC. The owner accepted the same row for B72,
managed RHEL on emulated bare metal, on 2026-10-05 (D58). The JSON `status`
checks wrote one document and nothing to standard error each, ending with
`runtime.privilege` for a missing sudo password and for a sudoers rule denying
the executable.
Every domain inspection read `/proc/1/net` under the elevated worker. The
ledger row holds the commands as run, each operation's outcome, the refusals
met and the log digests.

**Constraints left behind:** the run does not show
`qemu-img info --force-share` succeeding against a running domain's disk,
which B49 also named ([B277](#x93--the-owner-run-of-2026-10-14)). It found [B275](#x40--machine-commands-status-secrets-and-validate-refusals) and
[B276](#x93--the-owner-run-of-2026-10-14) and a new sighting of [B270](#x93--the-owner-run-of-2026-10-14), all parked
under D48 until D56 attached them to M1. Lifecycle concurrency may now rise
above one through [B303](#x48--bounded-concurrent-execution), and the adapter protocol, roles and
codecs may collapse through [B19](#x93--the-owner-run-of-2026-10-14), [B20](#x93--the-owner-run-of-2026-10-14) and
[B22](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json), each with the real-host evidence its own item names.

### X39 — untrusted input, trust and privilege boundaries

**Owner:** Architecture, State reconciliation, Desired state, Controller,
Trust and Workspace, with Managed OS, Machine, Substrate and Secrets.
Integrated on local `main` on 2026-10-05 as one commit, the first slice of
D56's full bar. **Items:** B244, B278, B279, B280, B281, B282.
**Decisions:** D56, D61, D62, D63, D66, D109, D111.

**Outcome, B244:** both Ansible runners write every string of the document
they hand ansible-core, the frozen request, its digest and the material and
output paths alike, as an `__ansible_unsafe` object, so an authored value
holding `{{ 7*6 }}` or a `lookup('pipe', 'id')` expression reaches the module
verbatim instead of running as root on the controller, while the frozen
request bytes stay as they were. A fresh plan refuses before registration any
block whose request holds `{{`, `{%` or `{#`, or a key ansible-core decodes as
a typed value, with one `api.value` per block naming its object and first
field and a remedy that imports the repaired input with `context update`
before planning again (D66). Destroy, continuations and bounded runs are not
scanned.

**Outcome, B278:** a bounded run or consumer, such as a `machine` power
command, `rsh` or `exec`, whose runtime or material call is cancelled inside
the call or while reopening releases its transient Secret binding under a
context cancellation cannot refuse, so a Ctrl-C no longer keeps it.

**Outcome, B279:** authored strings that reach native configuration have a
grammar at validate. HTTP(S) URLs are written in RFC 3986's own characters
and the install profile's `baseURL` fields refuse a fragment or a quote;
services, packages, localization values and repository IDs are Kickstart
tokens; NMState interface names are Linux interface names and next hops are
IPs of their destination's family; and `spec.libvirt.uri` is the enumeration
`qemu:///system` (D61). The Kickstart renderer refuses a line break, a
Unicode line or paragraph separator or invalid UTF-8 in any value, and
whitespace, a quote, a backslash, `#` or a leading `%` in a single-token one,
so valid inputs render the same bytes. A bare-metal `hostKeyRef` that is also
the fleet key, another Machine's access `privateKeyRef`, a StorageCluster's
`clusterSSH` key or any of a ContainerCluster's three `nodeSSH` references
refuses naming both objects. One SSH comment grammar (no control character,
tab included, no Unicode line or paragraph separator, double quote or
backslash) holds at admission, at generation and over an imported public key
line, and the installation refuses `api.value` naming the Secret before the
adapter runs when the fleet key's public line is not one quoted Kickstart
value. A generated username or comment refuses at validate what generation
refuses, NUL included, and the machines spec says blank and comment lines of
`knownHostsRef` material are ignored.

**Outcome, B280:** `machine trust` shows every fingerprint it will record,
and the one `--replace` supersedes, on standard output before it asks,
records nothing on decline, and names where an unchanged key is trusted when
only its endpoint moved. The relay no longer kills the elevated child five
seconds after the operator's signal: the child finishes its bounded
cancellation and keeps its status, and a second SIGINT or SIGTERM the
supervisor receives kills sudo; a hangup never escalates, because one
terminal hangup delivers SIGHUP twice. Directory accounts resolve through the
pinned, root-owned `getent` under the single-entry and clean-home checks, and
a `sudo -i` root shell resolves as direct root (D62); an account that cannot
be verified names a local account or a clean root login. Held sudo lines map
to their own remedies: a required password to `sudo -v`, a policy denial to a
rule that permits the procfs re-execution, and `unable to execute` to a local
copy of the binary. The exit evidence's unreadable checkout path named with
the local-copy remedy is met by that mapping and by the reworded
`runtime.privilege` refusal the unprivileged supervisor gives, before sudo
runs, when the invoking account cannot resolve the executable's path; the
root child never resolves that path, because the selection and invoker-file
helpers re-execute themselves through procfs. The CLI and controller specs and
the operator guide state the sudo rule, where a rule naming the binary does
not match, contrary to the brief's reading of sudo 1.9.14; directory
accounts; a network home with root squash; a FIPS-mode controller's key
types; a host an earlier build manages; and what a second Ctrl-C does. The
real-hardware test runs on a separate RHEL 9.8 controller (D109).

**Outcome, B281:** a write, sync or create the kernel refuses for capacity
names only its errno with the free-space remedy, any other errno names the
filesystem to inspect, the secret area reports both as `secret.store`, never
`secret.store.corrupt`, and a media stage the filesystem refuses no longer
blames the source. One damaged context's refusal names the context, the
store-relative entry and the errno with the exit that runs for that damage
(D63): a lost directory is abandoned with
`bootwright context delete --name <name> --purge --allow-orphans`; a missing
or inconsistent configuration or revision entry is purged with `--purge`,
which runs over exactly that context; and an entry the store refuses to open,
a damaged reservation or a replaced directory names the whole-store restore.
The exit evidence's purge of a hand-removed directory is met with
`--allow-orphans`, keeping the orphan acknowledgement the contexts spec
requires when objects cannot be listed, as D89 does for unreadable evidence;
`--purge` alone refuses naming that exit. Listings read through the held
handle and refuse a substituted directory. A root an earlier build created,
holding no registry.json this build can read or one of format version 2, 3
or 4, refuses saying another build may manage live environments there and
never to move it aside while its services run; a root that is not
`root:root` 0700 names what it is. Controller bundle files are staged under a
reserved name, synced and renamed into place, and the next setup sweeps a
stage a killed write left, so no failed, cancelled or killed write wedges
setup.

**Outcome, B282:** the elevated child opens no operator-named path itself. A
bounded helper, the running binary under the invoking account's credentials,
opens the input directory and everything beneath it, a Context file, secret
files and a `media add --from-file` source without following a link at the
name and passes each descriptor to root, which re-proves its type, ownership
and stability (D111); a process already running as that account, direct root
included, opens in-process. A denial names the path and whose credentials
were refused, and root's own, under a root login on a root-squashed home,
gives the local-copy remedy. `--from-file` refuses a FIFO, a final link, a
device and a file the invoking account cannot read, and `validate -f` and
`context init` read one directory alike. `--ssh-id-file` stays root's until
[B283](#x40--machine-commands-status-secrets-and-validate-refusals).

**Operator-visible effects:** refusals only. A public key line of any
`sshKeyPair` Secret, whatever consumes it (the fleet key, Machine host keys,
cluster node keys), that holds a tab, a CRLF ending or another character a
generated comment refuses now refuses at import; a key already stored that
way refuses at its next binding, `secret check` names it, and it must be set
again. A live context whose imported input a new grammar refuses keeps its
destroy, but its next fresh apply and its `status` refuse until the input is
corrected and imported. A sudoers rule must permit the procfs re-execution, a
binary in a root-squashed home runs from a local copy, and under sudo's
`use_pty` a second Ctrl-C does not hurry the command, while `SIGTERM` sent
twice to `bootwright` kills it. **Digest effects:** none. No frozen request,
request or record version, plan digest or automation digest moves: the
runners mark strings only in the document they write at run time,
`ansible/variables.go` is outside the embedded automation, the collection's
new tests and goldens sit under its `tests` directory, and every golden is
byte-identical. One live context is stranded: a context an earlier build
registered whose frozen request holds a mapping key named exactly
`__ansible_type`, `__ansible_unsafe` or `__ansible_vault`, reachable only
through an open document such as NMState, is refused by this build's runners
at every run, its removal included, so destroy it with the earlier build
first; any other key, `__ansible_note` included, runs as before.

**Review:** ten findings, seven confirmed. The first fix round fixed five: a
fleet-key comment holding a Unicode line separator injected a root `%post`
section into the Kickstart; a host key named as a ContainerCluster's
`nodeSSH.publicKeyRef` was not refused; the damage refusal named a purge that
then refused for an entry the store could not open; a selection read on a
root-squashed home failed on a dead executable check with no remedy; and the
guide promised that a second Ctrl-C kills the command under sudo's `use_pty`,
a promise now narrowed to signals the supervisor receives. A fourth round
fixed the two confirmed outside the slice's scope: an earlier build's
registry of version 2, 3 or 4 refused with no guidance, and a killed bundle
write wedged setup. Three were not confirmed: two were rejected as wording
or as unchanged behavior the CLI spec requires, and parked, and one repeated
the registry finding. Four checks followed. The first three found, in turn,
a tab-separated key line refused as a comment, a CRLF or outer-tab line
accepted at import and refused at apply, and that the line rule reaches
every `sshKeyPair` consumer, with a wording slip; each was fixed in the next
round, so the secrets spec now states the type-wide rule. The fourth round
also reworded the unprivileged supervisor's unreadable-executable refusal,
and its check found no gap.

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` and `./scripts/ansible-check --suite units`, `sanity`, `integration`
and `lint` pass on the integrated slice, after one integration fix of a test
type two lanes both declared, after each of the first three fix rounds and
on the squashed commit; after the fourth round `make check-offline
tidy-check modules-check docs-check race` passed. `./scripts/check-commits`
and `git diff --check` pass. The tests that need euid 0, the opt-in
privileged fixtures and the native Ansible targets are unrun. No real-host
run.

**Constraints left behind:** the helper path as root, directory accounts and
a root-squashed home are proved only unprivileged
([B352](backlog.md#b352)), and a second Ctrl-C under a foreground `use_pty`
leaves the cancellation running ([B339](#x93--the-owner-run-of-2026-10-14)). A host an
earlier build already left with a partial bundle file under its final name
still refuses that replay ([B356](#x42--media-contexts-setup-service-and-machine-admission)); X39 prevents new
ones. Tracked elsewhere:
`--ssh-id-file` ([B283](#x40--machine-commands-status-secrets-and-validate-refusals)); `status` over a revision a new rule
refuses ([B226](backlog.md#b226)); a bind-time refusal naming its Secret
([B285](#x40--machine-commands-status-secrets-and-validate-refusals)); the emulator's URI rendered as data
([B295](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); the FIPS key types of the bare-metal README
([B73](m1.md#b73)); a cancellation inside a binding's publication, which
bounded runs lose with [B337](#x41--lifecycle-results-the-controller-stage-installation-shapes-and-bounded-runs); and a context whose reservation
is damaged or that owns live objects, now clauses of
[B325](backlog.md#b325). Under D48 every other follow-up was parked: B339
to B356.

### X40 — machine commands, status, secrets and validate refusals

**Owner:** Machine and Trust, State reconciliation and CLI, Secrets, and
Desired state with Environment. Integrated on local `main` on 2026-10-06 as
one commit. **Items:** B283, B275, B222, B284, B285, B286. **Decisions:**
D59, D64 to D73, D111 to D113.

**Outcome, B283:** `machine rsh` and `machine exec` reach the guests
Bootwright installs. An installed Machine that declares no `ssh` address now
dials its install address when it declares that address, else its FQDN
(D64): the address at which its installation proved the host key, so
lab-rhel's rhel-01 opens a session over the address its installation request
names, a session dialing any other address refuses `trust.identity` naming
the proved one, and an undeclared install address is refused once, at
`network.installAddressRef`. `machine start`, `stop` and `restart` and
`machine list --power-status` follow the provider's machine block instead of
the installation, so the `machine stop` a live-removal refusal prescribes runs
while the installation failed, is unknown or still runs, and a power run
shows one progress step with its sub-steps past the heartbeat.
`machine list --clusters` refuses a member naming no selected cluster, listing
the clusters it can select. A trust write that takes over the endpoint of a
Machine the context no longer declares removes that record in the same
confirmed write, shown as a `remove` row in the plan and the dry run, and a
first use names it on standard error before its prompt (D65). A plan that
would still pin one endpoint to two keys refuses before it is shown, naming
both Machines; when the other Machine no longer uses the context's SSH trust,
the refusal says so and names the input change that drops its record, what
that change needs and what it costs, and, for the controller Machine once an
apply has bound the context, that only a separate context drops it. Every
host-key remedy and the first-use prompt carry `--context` (D66).
`--ssh-id-file` is opened through B282's helper under the invoking account's
credentials, proved on the received handle and copied into anonymous session
material, so the client never reopens the operator's file (D111), and the
machines spec states what `auth.operatorIdentity` does today (D112). The
lab-rhel README runs `machine list`, `--power-status`, `exec`, `rsh` and
`restart`, and after a restart waits for the guest to boot before `exec`
(D59).

**Outcome, B275 and B222:** `status` reads every cluster and shared-service
row by the current operation's verb through one vocabulary. After a removal,
an object whose removal is done or released reads `pending`, a failed one
`failed` with `[FAIL]`, and a running or unknown one `unknown` (D67); an
object a replacing removal has not started, or one of a completed removal
whose block record no longer reads done, reads `unknown`, since no record
proves what the removal did to it. Beside unindexed records, and over a
completed destroy holding a block that is not done, status offers the
deletion their refusal names, and nothing over evidence that deletion cannot
read. It reads a keyring listing that fails as corrupt or undecryptable as the
reopen will, and over a lost binding it offers `bootwright apply` first where
that apply finalizes, then the orphan-acknowledged deletion (D69).

**Outcome, B284:** `Bound` becomes `Bindings`, the binding count, which reads
0 beside no operation and once a completed removal finalized (D68). A claimed
ContainerCluster reports pending, done, failed or unknown from its blocks
instead of unsupported. Until the host's controller setup completed, status
offers `bootwright setup` alone where a verb would re-prove it, and setup rows
read as labels, an unapplied context's binding `[PENDING]`, bound by the first
apply. A ready `preflight controller --context` names
`bootwright plan --context <ctx>` as its next step, and its golden is built
from real check summaries, the libvirt client labelled. The `machine list`
JSON row renames `address` to `contact` and `ips` to `addresses`, under the
CONTACT and ADDRESSES columns, and omits an absent contact, provider and power
(D70); a power result omits a `previous` the controller did not report. A
completed orphan-acknowledged deletion warns under its own `context.orphaned`,
the SSH password advisory's `access.credential` is registered, and the output
spec defines both severities and settles its three contradictions.

**Outcome, B285:** every Secret refusal names the Secret and the next command
with `--context`. Apply's binding refusal lists every missing or stale Secret
with its remedy by source; a `secret set` flag set that does not fit the
declared type is one `secret.input` usage failure naming the type's flags,
with exit 2; and an unconfirmed replacement or deletion names the Secret and
the command with `--yes`. A declaration's fingerprint covers type, source and
parameters only, so an identical re-import from another directory or with
reordered documents keeps Secrets available and `secret generate` no longer
re-mints them, while a version stored under the legacy fingerprint stays
current (D71). Certificate and public-key files follow a relaxed input rule,
and each input-file refusal names its condition and remedy (D72).
`--value-stdin` and `--password-stdin` at a terminal prompt on standard error
with echo off for tokens and passwords and refuse opaque and
`dockerConfigJson` values; standard input is read with no store lock held,
and the replacement decision is proved again under the lease (D73).
Generated certificates start 24 hours before generation, so a verifier whose
clock trails accepts them, earlier material stays current and serving keys
stay P-256 (D113), and an RSA serving key under 2048 bits refuses at
`secret set` and in the artifact server's check. `secret generate` lists
changed and unchanged names, a no-op delete says nothing was deleted,
rotation reports the retired keys and the re-encrypted counts, and
`secret encryption status` shows its keys as a table with a next step when
cleanup is required. The secrets and contexts specs and the lab READMEs
follow.

**Outcome, B286:** `validate` refusals name the object, decode failures
included, the expectation (permitted values bounded to 16, bounds, the
expected type, the reference kind) and a next step; an unknown key is
reported at its own path with a rename only within two edits, and no authored
scalar beyond an identifier is repeated. Invalid UTF-8 and YAML syntax errors
get separate fixed messages with their line, `input.not-found` its own
message, and a hard-linked candidate or marker `input.symlink`; resource and
cluster selection refusals sit at their indexed entry, and an empty
`resources` list is reported once. A target that fails decoding is its only
refusal at every reference field naming it. A multi-digit integer with a
leading zero refuses `api.type`; effective YAML double-quotes trailing
colons, Unicode line breaks and YAML 1.2 core-schema spellings; a test holds
normalization's kind order to the kind catalog; an `ibm-storage-ceph`
Entitlement no longer inherits `rhsm`, while the RHEL one does; and the
missing fleet key is one `api.required` naming an installed Machine.

**Operator-visible effects:** beyond the refusals and results above,
`render effective` shows lab-rhel's rhel-01 and lab-baremetal's installed
Machine with `access.ssh.addressRef: ip` where it showed `fqdn`, so their
sessions dial the install address. Output contracts change: the
`machine list` JSON row's `address` and `ips` become `contact` and
`addresses` and absent scalars are omitted (D70, with the `media list` row
left to [B300](#x44--help-completion-contracts-and-ratchets)); the status JSON's `secrets.bound` becomes
`secrets.bindings`, its setup check `dependency-bundle` becomes
`execution-bundle`, and its cluster and shared-service rows take the new
values; the fleet-key refusal moves from `api.invariant` to `api.required`;
and an orphan-acknowledged deletion warns under `context.orphaned`. A context
whose stored input holds a multi-digit leading-zero integer no longer
compiles, so its `status` and `apply` refuse `api.type` until
`context update` imports corrected input; its `destroy` plans from records,
and no example or template input holds such an integer. **Digest effects:**
none. No frozen request, request or record version, plan digest, automation
digest or keyring format moves, and nothing under `ansible/` changed. A probe
through the production planner gave lab-rhel's apply plan digest
`3db600387952ff65b2d3dfdc3618f6d50dfe5c3e060f90309dc31e23eb5b6477` and the
same block request digests at the base and on this commit, and the
render-effective, plan-digest and request goldens are byte-identical. Only
the fingerprint of a Secret version stored from this build on differs, and
earlier versions stay current. No context is stranded.

**Review:** thirteen findings, nine confirmed in scope, two of them
blocking, and the first fix round fixed all nine: a destroy that replaced a
failed one reported what the first attempt took back or failed on as done; a
unique candidate three edits away gave an empty rename; a target that failed
decoding was also reported undeclared, up to seven diagnostics for one
unknown lab-rhel key; a completed destroy that lost a block record read its
service done; an unconfirmed secret replacement or deletion named neither the
Secret nor `--yes`; the contexts spec's lock table still held the store lock
while `secret set` read standard input; an undeclared install address was
refused twice, once with a remedy an installed Machine cannot follow; the
lab-rhel journey ran `exec` right after `restart`, before the guest answered;
and the divergent-pin remedy named a re-trust that refuses when the other
Machine no longer uses the context's trust. Four were not confirmed: a spec
example whose contact is a DNS name, which a declared `ssh` address
produces; `status` refusing over a removal whose source apply lost its plan,
which the specs require; two `api.required` refusals at one omitted field,
which predate X40; and a first-use notice under the registered
`trust.identity`. Three checks followed. The first found that the new
remedy could still refuse under an incomplete operation, a referenced Machine
or a new input revision, and a stale comment; the second round made the
remedy state those limits. The second check found the controller Machine's
case a dead end once the context is bound; the third round made its refusal
say that only a separate context drops the record. The third check found
that the controller remedy no longer says that no other object may reference
the controller Machine, and that the CLI spec claims the input then compiles,
which is false for every tracked example; that wording stays with its
decision in [B357](#x46--the-safety-and-defect-sweep-of-m1).

**Gates:** `make check-offline tidy-check modules-check vulncheck docs-check
race` passes on the integrated slice, after each of the three fix rounds and
on the squashed commit, and each check round's `make check-offline
docs-check` passes. `./scripts/check-commits` and `git diff --check` pass.
`./scripts/ansible-check` was not required, since nothing under `ansible/`
changed, and is unrun. The adversarial review, by reviewers that did not
write the diff, is the independent review B283 and B285 name. `make race`
does not cover `internal/trust`, whose enrollment test double races under
`-race` ([B368](backlog.md#b368)). No real-host run.

**Constraints left behind:** a trust record held by a Machine the context
still declares but no longer trusts by SSH blocks another Machine at that
endpoint until the input drops it, and the controller Machine's record has no
exit inside a bound context; widening D65 is the owner's decision
([B357](#x46--the-safety-and-defect-sweep-of-m1)). Add-on and Machine checks still repeat a refusal for a
target that failed decoding ([B358](#x46--the-safety-and-defect-sweep-of-m1)); until a replacing removal
starts its first block, the object the replaced attempt failed on reads done
([B359](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json)); a YAML parser error can name the line before its own
([B360](#x46--the-safety-and-defect-sweep-of-m1)); the uninitialized-store refusals and the retired
file-source remedy name no `--context` ([B361](#x46--the-safety-and-defect-sweep-of-m1));
`machine list --power-status` reads controllers with no progress row
([B370](#x46--the-safety-and-defect-sweep-of-m1)); the context store's uncertain secret-state publication
names no inspection command ([B371](#x42--media-contexts-setup-service-and-machine-admission)); a file-input secret
replacement, deletion or rotation prompts under the host-wide lock and lease,
which the owner decides ([B372](#x46--the-safety-and-defect-sweep-of-m1)); and nothing reports a sudo
policy that logs a standard-input secret ([B373](#x46--the-safety-and-defect-sweep-of-m1)). Under D116 these
nine join M1 on X46. A Secret version stored before X40 stays current only
while its declaring path and document index are unchanged, so the first
re-import from another directory stales it once ([B362](backlog.md#b362)).
Tracked elsewhere: the confirmation prompts of `secret set`, `delete` and
rotation, which still call the Secret a context
([B287](#x41--lifecycle-results-the-controller-stage-installation-shapes-and-bounded-runs), D75);
the `media list` row ([B300](#x44--help-completion-contracts-and-ratchets)); the `secret set` help
([B95](#x44--help-completion-contracts-and-ratchets)); codes the registry scan cannot see
([B162](backlog.md#b162)); `auth.operatorIdentity` under the invoking account
([B332](backlog.md#b332)); `status` over input a new rule refuses
([B226](backlog.md#b226)); `--ssh-id-file` through the helper as root,
now part of [B352](backlog.md#b352); and the defaulted fleet-key duplicate on
an installed Machine ([B294](#x42--media-contexts-setup-service-and-machine-admission)). Under D48 (D116) the other
follow-ups were parked: B362 to B369 and B374 to B376.

### X41 — lifecycle results, the controller stage, installation shapes and bounded runs

**Owner:** State reconciliation and CLI, Controller, Managed OS with
Infrastructure services and Container cluster, Workspace and Secrets.
Integrated on local `main` on 2026-10-06 as one commit. **Items:** B201,
B287, B94, B240, B241, B257, B288, B249, B289, B24, B225, B337.
**Decisions:** D56, D57, D66, D74 to D85, D106.

**Outcome, B287:** lifecycle results name the command that comes next. A
paused, failed, unknown or running apply or destroy closes with a `Next` line,
such as `bootwright apply --context lab`, that carries `--authorize` for every
token the frozen plan consumes, and `status` offers the same commands. Every
command a lifecycle remedy or next step names carries `--context` (D66), as do
the libvirt stop command and the pre-boot refusals. An apply or destroy
interrupted after it registered its operation writes its result, its logs, its
`Next` line and its receipt, then `runtime.interrupted`, and exits 130. A
failed attempt's remedy names its block and its attempt output under the
operation's logs, which root reads. Status and the continuation and removal
refusals name the registering build, `devel (<commit>)` for a commit-only
record, and the executable now records its commit. Every plan presentation
marks the steps that consume `--authorize data-loss` and closes with a
`Requires` field naming them, except a finalization's preview, which needs no
token; a missing token refuses after the plan is shown and before the prompt,
naming the steps and the exact command that passes. The plan's width reads
"4 waves, widest 5 steps" and says when this build starts one block at a time,
and a failed plan write exits 1 instead of a fallback `runtime.internal`.
Under `--stage` the preview marks what the next apply works first (resolve,
retry or start), and the stage refusal names the exact apply that widens the
selection, with the plan's tokens. Confirmations name the object and context
they act on and keep each consumer's code: `controller.setup`, `media.store`,
`lifecycle.state`, `trust.identity`, `secret.store.conflict` for a Secret
replacement, deletion or rotation, and `machine.power` (D75). A declined or
non-interactive one gives its reason and, as its remedy, the operator's own
invocation repeated with `--yes`, every flag kept and `--context` first, with
a placeholder for a URL or username part, so following it never does more
than the reviewed plan. Destroy's lock-holding proofs follow the prompt, which
a test pins, and a fresh destroy's plan closes with `Stop first` naming the
Machines whose quiescence is observed on the host (D76). `machine exec` and
`rsh` own their exit status: Bootwright's refusals before a session opens, and
a non-interactive sudo refusal before it starts, exit 255, and a session's own
status wins over an interrupt (D74). A continuation of a context whose
controller binding is gone names an exit that works: the destroy that takes
back what an incomplete apply or a failed destroy owns, then the apply that
binds again, or a restore of the controller state; for a running, paused or
unknown removal, that restore or the context's deletion. `make build` sets
`CGO_ENABLED=0` and yields an executable with no dynamic dependencies.

**Outcome, B201:** a failed destroy's remedy names the destroy that replaces
it with a fresh removal of what it has not proved gone; `status` no longer
lists a failed destroy's lost block record as a contradiction, while a running
or unknown destroy's still counts; and the media lock refusal says a
repetition re-verifies the retained image, while a rewritten retained stage's
refusal says it was removed, so repeating acquires it again.

**Outcome, B257 and B241:** the controller clients' refusal helper sets its
remedy as the remediation instead of a source path, and every clients refusal
names one. Native resolution and controller Ansible failures reached from the
controller stage keep their codes and name
`bootwright apply --stage controller --context <name>`, while setup keeps its
own rerun; preflight scopes a target-tool selection failure and a libvirt
presence failure to the stage; an unbound context with no controller stage is
sent to its first `apply --context`, which binds it; and a not-ready preflight
prints the next command its service decided. Apply-time and stage refusals of
the controller record carry their own remedies, except the two
[B379](#x46--the-safety-and-defect-sweep-of-m1) records, the tool-location remedy names the context's
stage, and every controller selection refusal, the version and tool refusals
included, names a remedy.

**Outcome, B288, B94 and B240:** the controller stage and
`preflight controller --context` read a context's native closures through one
Controller-owned answer. The stage solves the container runtime beside exactly
the closures the frozen request selects, so an installer-media-only context
resolves no libvirt client, and reads only the latest retained resolution of
its own selection; preflight checks the libvirt client, hypervisor and
installer-media closures the context selects and reports not ready over another
selection's resolution. The controller and state-reconciliation specs state
when the libvirt client is selected, and a stage test runs the hypervisor
closure end to end (B240). A RHEL 9.8 controller accepts `lorax` and `xorriso`
the operator installed from the host's Red Hat repositories, proved by
presence from the RPM database snapshot, read as the unprivileged helper
account, where every installed instance must carry the RHEL 9 release key; a
missing package refuses before any acquisition naming
`dnf install lorax xorriso`, and one another key signed names its removal and
reinstallation (D106). Controller egress keeps one route grammar, the API's
proxy endpoint and bypass rules with a 1024-byte bound per bypass entry, at
admission on the Environment and at selection naming the field: an endpoint
with a path or a query, an out-of-range CIDR and an `httpProxy`-only Proxy
refuse, and the executable's own limits (another capability, a managed Proxy,
`proxyAuthRef`, `trustBundleRef`, an `httpProxy`-only Proxy, more than 128
bypass entries) refuse before registration as controller Unsupported rows with
remedies (D77). A Go and Python parity test holds the closure tables equal.
Dependency resolution stages under `/var/lib/bootwright-staging`, a root-owned
parent beside the store, each stage locked, swept once stale and removed on
release, and an `EACCES` or `EPERM` start on a noexec mount refuses naming the
mount. The dead setup-binding action, the legacy prerequisites layer, the host
runtime inspection and the static import probe are deleted, and the compiled
catalog keeps only each release's execution foundation (B94). The development
and operator guides mark RHEL 9.8 admitted but not yet run.

**Outcome, B289 and B249:** `validate` refuses what the only installation
cannot use. MachineImage `bootMedia` and hosted-tree `fromMedia` accept only
`local-media:<name>` for a valid media name, and an installed consumer's
profile must name its `redfishVirtualMedia` artifact endpoint on every
substrate (D81). A DHCP-only or IPv6 install network on a Bootwright-installed
Anaconda Machine refuses at `installAddressRef`, and one with no network
configuration is told to select one first (D80). A MachineInstallProfile whose
hosted tree publishes through the server that a same-named installed Machine's
image uses refuses at its `serverRef`, and the infrastructure-services spec
says who owns the consumer-level directories (B249, D83). Every managed-OS
admission refusal names its exact remedy, and the bootstrap-cycle refusals
state the refused shape. An installation through an external DNSServer or
NTPServer renders its declared address with no requirement edge (D78). Before
registration the installation refuses, in refusal-table rows that
`TestManagedOSRefusalTableMatchesUnsupported` holds, a non-ethernet install
interface; network content the install line cannot carry, read from the
composed network: other addresses, non-ethernet interfaces, an MTU other than
1500 and any route but the default route through the template's gateway (D79);
an image or tree server off the controller (D82); and a virtual Machine whose
provider host is not the image server's placement Machine, naming both. Three
deviations from the exit evidence: the CLI keeps its media-name copy, held to
the managed-OS rule by a parity test, because the layout test forbids the CLI
importing the managed-OS domain ([B393](backlog.md#b393)); the DHCP-only
refusal-table row is unreachable from validated input, since admission refuses
first; and "a physical target with an SSH-placed server stays valid" conflicts
with D82, so the tests prove instead that the provider-host row never fires on
a physical target, and the SSH-placed request golden keeps its bytes.

**Outcome, B24 and B337:** bounded runs (`machine start`, `stop` and
`restart`, the `machine list --power-status` read, `machine rsh` and `exec`)
read the current version of each Secret they need in one keyring session under
the store's shared lock and publish no binding or reservation, so a loop of
them no longer exhausts a context's keyring, with no keyring format change
(B337, D85). Each run request carries its context, block and description,
which the runner records beside a holder lock only the invocation holds, so
two contexts' bounded runs proceed together. A held job refuses only its own
context, naming its context, operation and Machine; it says to wait while its
invocation lives, and names the lock and the processes to end only once that
invocation is gone. A job an earlier build started, or one with no readable
record, still refuses every context, and the host-wide sweep still removes
every unheld job (B24, D57). A job record is published whole, and a sweep
takes a job's lock shared and removes a job or a scratch tree only under that
directory's own exclusive lock, so two contexts' sweeps never refuse each
other.

**Outcome, B225:** the lifecycle operation area and an SSH-trust mutation's
area measure their subtree once per transaction and then count their own
writes, measuring again after any failed write; the runs area, which every
bounded run of its context writes, measures every write. The runs area keeps
the newest 16 runs per context, retiring the oldest before a new run opens and
never one a live run holds (D84), so power commands keep working past the old
shared bound. The contexts spec's bounds table names the runs and SSH-trust
areas, and a capacity refusal names the storage it would overfill.

**Toward B95:** `bootwright destroy --help` says that destroy removes what the
apply owns whether or not it completed, that a failed destroy is replaced,
that Machines are stopped first and that a disk-deleting plan needs
`--authorize data-loss`. [B95](#x44--help-completion-contracts-and-ratchets) keeps every other command's help for
X44.

**Operator-visible effects:** `machine exec` and `rsh` exit 255 for a refusal
before the session, where they exited 1, except an interactive sudo refusal,
which still exits 1 as the CLI spec now states; an interrupted registered
apply or destroy prints its result before exiting 130. Results carry the
`Next` line, plans the `[data-loss]` step marker, the `Requires` and
`Stop first` fields and the new width wording, and remedies are exact commands
with `--context`. A declined confirmation reports its consumer's code with the
repeated invocation as its remedy, and preflight reports the `libvirt-client`,
`hypervisor` and `installer-media` checks. `validate` refuses more: the
controller's proxy grammar and its bounds, media sources, DHCP-only installs,
a same-named Machine and profile on one server, and a missing virtual-media
endpoint; plan and apply refuse the installation shapes and the controller's
Unsupported rows before registration. A live context whose stored input one of
these rules refuses keeps its destroy, while its next fresh apply and its
`status` refuse until `context update` imports corrected input. A RHEL
controller needs `lorax` and `xorriso` from its Red Hat repositories for
installer media, and `/var/lib/bootwright-staging` must be on an exec-capable
filesystem. A Fedora installer-media-only context that already applied
resolves again once on its next apply; the first bounded run after the upgrade
leaves only the newest 16 runs of its context; and a job a build before X41
left running refuses every context's runs until it ends. `make build` now
yields a static executable. **Digest effects:** none. No frozen request,
request or record version, setup or lifecycle plan digest, automation digest
or keyring format moves, and nothing under `ansible/` changed. The request,
plan-digest and effective goldens are byte-identical; the new goldens, the
site-services requests and the paused, running and unknown apply results, are
additions, and the changed ones are presentation text. `catalog.json` drops
its dependency pins and keeps `format`, `projection` and each release's `os`,
`release` and `execution` unchanged, while
TestTheCompiledExecutionFoundationIsUnchanged and
TestAResolvedBundleIdentityDoesNotReadTheCatalogPins pin the execution
foundation digests and a resolved bundle's identity; the installation's
content digest keeps the deleted tooling as a frozen literal under
`TestTheContentDigestIsPinned`. The job record under `/run` is per-boot
runtime state. No context is stranded.

**Review:** before it, completing the integration replaced X26's guard-test
citation of a test B337 deleted, tested the session status at the privilege
boundary, which a lane's own review had found untested, and gave the unbound
continuation an exit that works where its remedy had named an apply that
cannot bind. The review then raised eleven
findings, ten confirmed, nine of them in X41's scope and one blocking, and the
first fix round fixed all nine: the `--yes` remedy after a declined or
non-interactive confirmation dropped the operator's own flags, so following it
applied every stage or trusted every Machine beyond the reviewed plan
(blocking); the stage refusal's apply left out the plan's `--authorize`
tokens; a finalization's preview required a token no finalization needs; the
bare-metal pre-boot remedies named an apply that cannot pick up a corrected
declaration, and now name the destroy first; a bypass entry over 1024 bytes
passed admission and failed late in the stage; a foreign-signed `lorax` or
`xorriso` was offered an install that leaves it in place; the remedy for an
install Machine with no network configuration looped; another context's job
mid-claim or mid-removal refused a run; and the CLI spec promised that a
session status from 0 to 254 is always the remote command's, which now holds
except for an interactive sudo refusal, which the spec states. The tenth,
`status` offering an apply over an unbound incomplete apply, predates X41 and
became [B378](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json); the one not confirmed, a remedy that could also
name the declaring NetworkConfig, is wording in [B395](backlog.md#b395). Three
checks followed. The first found the job-race fix partial, since a sweep
removing another context's ended job held its lock as the job's own processes
do, and the second round made sweeps take it shared under a directory lock.
The second found that round's race test failing on one processor; the third
round made it skip there and closed a window, older than X41, in which two
sweeps emptied one orphaned scratch tree together. The third check found no
gap.

**Gates:** on the integrated lanes, `make check-offline tidy-check
modules-check vulncheck docs-check race` failed only on
`TestDocsCitedTestsExist`, over that stale citation, and the targets it
stopped before passed under `make -k`, except `docs-check` on the same line.
After the continuation's fixes the same command passed, as it did after each
of the three fix rounds and on the squashed commit, and each check round's
`make check-offline docs-check` passed. `./scripts/check-commits` and
`git diff --check` pass. `./scripts/ansible-check` was not required, since
nothing under `ansible/` changed, and is unrun on the slice; the lane that
changed the controller closures ran its units and lint suites, which passed.
The adversarial review, by reviewers that did not write the diff, is the
independent review the items name. The opt-in privileged and qualification
harnesses, a real noexec mount and RHEL 9.8's `rpm` are unrun. No real-host
run.

**Constraints left behind:** a fresh destroy, whether it supersedes an
incomplete apply or a failed destroy or removes a completed apply, verifies
neither the host identity nor the controller binding, which only a
continuation does, although the spec runs a controller-hosted service effect
only under a verified binding, and the unbound continuation's remedy relies on
it ([B377](#x46--the-safety-and-defect-sweep-of-m1)); `status` still offers a continuation that the
binding, automation-digest or other continuation proofs refuse, among them an
apply over an unbound incomplete apply and a destroy over an unbound running,
paused or unknown removal ([B378](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json)); an apply's or a deletion's
uncertain controller-record publication and an execution-foundation failure
the stage reaches still name setup's retry ([B379](#x46--the-safety-and-defect-sweep-of-m1)); lifecycle
resolution, controller-client, pre-boot, agent-install and one context-store
remedy still lack an exact command, its context or the plan's tokens
([B380](#x46--the-safety-and-defect-sweep-of-m1)); two Machine network admission refusals keep a slogan
remedy ([B381](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); managed-service shape refusals still come at
plan with no object or table row ([B382](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json)); the proxy grammar
holds only the controller's route ([B383](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json)); and the libvirt roles'
running-guest refusals and the substrates spec name `machine stop` without
`--context` ([B384](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)). Under D116 these eight join M1 on X46. An
interactive sudo refusal before a session still exits 1
([B390](backlog.md#b390)). A bounded run that a build before X41 started
holds no run lock, so retention could retire its directory if 16 newer runs
of its context opened beside it, which no later build can change. Tracked
elsewhere: the uninitialized-store refusal the bounded read shares with a
binding ([B361](#x46--the-safety-and-defect-sweep-of-m1)); container-cluster admission remedies
([B313](delivered.md#b313)); content the install line still drops, search domains
and IPv6 policy among it ([B338](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation), [B326](m1.md#b326)); RHEL 9.8's
`rpm` output and the snapshot read as the helper account
([B335](backlog.md#retired), and the RHEL 9.8 controller run D60 plans); the
glibc and libgcc builds a RHEL 9.8 controller must hold, and building on a
workstation ([B292](#x42--media-contexts-setup-service-and-machine-admission)); help beyond destroy ([B95](#x44--help-completion-contracts-and-ratchets)); an
invocation's own in-flight jobs ([B304](#x48--bounded-concurrent-execution)); and a bounded run
beside another context's exclusive apply lock ([B18](m1.md#b18)). Under D48
(D116) the other follow-ups were parked: B385 to B397, B385 and B393 split
from B94 and B289.

### X42 — Media, contexts, setup, service and machine admission

**Owner:** Managed OS with CLI, Workspace and State reconciliation, Controller
setup, Infrastructure services, and Substrate and Machine. Integrated on local
`main` on 2026-10-06 as one commit. **Items:** B290, B236, B291, B179, B202,
B228, B271, B292, B198, B293, B294, B349, B356, B350, B371. **Decisions:**
D55, D56, D66, D77, D86 to D95, D107, D108, D113, D114.

**Outcome, B290:** `media add --from-url` takes only an HTTPS URL (D86): a
plain `http://` URL, userinfo, a fragment, a control character in either
source and an origin over 512 bytes exit 2 before anything elevates or
downloads, and the acquirer refuses a non-HTTPS URL before any request. The
record carries a query-free origin, derived once before the confirmation and
the claim, while the request keeps its query. A redirect refuses naming its
status and its query-free target; DNS, certificate, timeout, HTTP status and
cancellation failures each name their own cause and the route, and so does
the new six-hour transfer deadline, beside the 30-second connection and
60-second response bounds the managed-OS spec states. No response body, query
or transport error text reaches a diagnostic, and a full disk is still never
blamed on the source. A reserved image's deletion, replacement and
revalidation name every reserving context and
`bootwright destroy --context <it>`, and a digest mismatch names the image,
its origin and both digests. `media add` reports an acquire or verify step and
a publish step, and `media list --checksums` one check per image, as progress
rows that JSON output never shows. `media list` lists a short image as a
mismatch instead of failing, names the contexts that reserve each image and,
with `--checksums`, shows the computed digest; its state reads stored,
verified or mismatch, and its JSON row gains `reservedBy` and `computed`. A
replacing add and a deletion present the stored record before the prompt, and
a replacement shows the source its record will carry, the retained one when
the claim adopts a retained stage. The media record format and its version
are unchanged, and records with `http://` or query-bearing origins stay
readable.

**Outcome, B291, B236, B350 and B371:** a sole empty occurrence of a
repeatable flag, as in `validate -f ""`, `--file=` or `apply --authorize=`,
exits 2 as a usage error before classification elevates. Context refusals
name the context and the exact next command, whether `context init`,
`context use`, `context update` or the deletion, use one code,
`context.input`, for missing input, and name the selection file or directory
and its repair; a Context file refusal names the field and its YAML line, and
a document of another kind the kind it declares. An interrupted
`context init` resumes from another directory and from no input. An identical
`context update` keeps the selected revision, publishes nothing and asks
nothing, so apply still settles (D87), and changed input over a completed
apply warns that apply refuses it until a destroy. `context update` and
`context delete` present what they change before the prompt: the source,
files, counts and warnings, or the revision, keyring, host reservations and
any abandonment. The orphan refusal and confirmation name
`bootwright status --context <name>` as the inventory (D88). Missing, empty,
corrupt or unsupported mutation evidence now has an exit: update and a default
deletion refuse naming status and
`bootwright context delete --name <name> --purge --allow-orphans`, which
abandons the context, saying that its objects cannot be listed and naming the
export of any custodied cluster kubeconfig first; status and the lifecycle
refusals offer that deletion instead of a whole-store restore, and absent or
empty evidence reads as such (B236, D89). `context update` reaches the
controller-input check through its transaction port, and over an incomplete
operation names its destroy outright beside the continuation status names.
Lifecycle operation refusals name store-relative entries, and listing the
registry leaves the held root handle's offset alone (B350). An uncertain
secret-state publication names `secret encryption status` and `secret check`
with `--context` (B371).

**Outcome, B179, B202, B228, B292, B349, B271 and B356:** setup's `Next`
line follows its refusal's remedy, and is absent when no command settles it;
a failed retirement and the retiring-area refusal name the purge (B179).
Resolution warnings print with the plan, before the prompt (B202, D91). A
setup run keeps 4 MiB of output, the bounded run's bound, while an earlier
build's 8 MiB run is still admitted; its output writer never fails the
runner; an Ansible failure's remedy names the run's output and that reading
it needs root; the checkpoint harness interrupts a setup run at every
checkpoint; and the operator guide describes setup runs (B228, D92). The
ambient `HTTPS_PROXY` host is lowercased as the receipt requires, and routes
read "direct (no HTTPS_PROXY)" or "direct (Machine <name>)". A bound full of
client areas refuses saying that no command of this build frees it, and the
stage's client-area and resolution bound refusals name the purge (D90; the
retirement stays parked as B322, which
[X90](#x90--controller-setup-v2-retention-and-offline-sources) closed). A purge dry run plans the
retirement, and a purge that removed nothing prints "Retired none". The
execution foundation gains a named check in setup and preflight: the catalog
attributes each release's glibc and libgcc files to their package and build
beside the execution profile; a drift refuses before the plan, naming the
file, package and build with a reinstall, versionlock and repeat remedy that
also serves apply and destroy; and scripts/foundation-catalog regenerates a
release's record, reproducing the Fedora 43 record exactly (D107). Setup and
preflight report the host's FIPS mode, with the statement that Bootwright's
runtime brings its own cryptography (D108). The operator guide lists the
destinations an allowlisting proxy must admit, which
`TestDocsProxiedNetworkDestinationsMatchTheCode` holds to the code under
`make docs-check`, says that a dry run only parses the proxy variables while
`preflight controller` confirms they crossed sudo, and describes holding the
foundation and building on a workstation (B292). `setup --dry-run` reports a
state-root check, inspecting the root without privilege and refusing, with
the store's own refusal, a wrong type, owner, mode or filesystem or an
earlier build's root (B349). `--purge-old-bundles` cancels a pending setup of
setup's own, at the bound or below it, that this executable cannot resume and
that never took effect: one never started, or one whose native transaction
holds only its intent while the host's package inventory, read under the rpm
lock, still equals the transaction's before-state. The cancellation is
decided again under the mutation and must match the plan, and without the
flag setup and preflight refuse naming the purge (B271, D55, D93); the setup
help and command row say so. A short file an earlier build left under its
final name in an unsealed bundle area is reported partial and discarded
through the area's write capability before the replay proceeds, while a
same-size file holding other bytes still refuses (B356).

**Outcome, B293 and B198:** validate refuses the service shapes their only
consumers cannot use: a DNSServer endpoint whose Machine address is a DNS
name; under a non-wildcard bind, an IP endpoint other than the bind; an
NTPServer port other than 123; a wildcard bind without an endpoint, or an
ArtifactServer without one per listener; a managed service on a Machine that
provides no OS; a tag image pin, defaults included; an empty listener list; a
download mirror that is not an HTTPS base URL on the default port without a
query, fragment or percent escape, a rule the controller stage's resolution
shares, while recovering a block an earlier build froze keeps the earlier
stage's tolerance; and an image repository holding an uppercase letter or a
`+`, while image digests are lowercased. A generated serving certificate that
misses an HTTPS endpoint's address refuses at that endpoint, naming the Secret
and the address, with a remedy that imports the corrected Secret with
`context update` before `secret generate --context`; every serving-certificate
refusal at execution names `Secret/<name>`, the field or command and
`--context`, and certificates stay P-256 (D113). Each service admission
refusal carries an exact remedy, and a mistyped `serverRef` is only a
reference problem. Readiness refuses a frozen request with no probe target or
with an unprobed listener, so such a service reads partial, never completed.
The artifact server reuses the managed-service derivations, and request
goldens written before that refactor pass unchanged after it. IPv6 listener
impacts and artifact endpoint URLs are bracketed (B198), while reservation
keys and probe addresses stay unbracketed and IPv4 and DNS-name bytes are
unchanged. The DNSServer keeps its wildcard default (D114).

**Outcome, B294:** validate refuses the libvirt shapes the host cannot
realize: an attachment or data-disk name that is not a DNS label; a data disk
named `root`, or more than seven; a CPU, memory or disk size past its
ceiling; one managed attachment name or bridge on two providers of one host,
naming both (D95); an InfraProvider name over 48 bytes or a Machine name over
52, so that their block identities fit 63 bytes; a non-provided libvirt
Machine that selects no network or whose composed network has no available
ethernet interface; an install address equal to its managed bridge's host,
network or broadcast address, and an IPv4 assignment that is its own prefix's
network or broadcast address; and `hardware.nics` MACs or `management.bmc` on
a libvirt provider's Machine. The emulated BMC port range counts only the
Machines allocation realizes. The libvirt provider's
`bmcEmulationDefaults.disableCertificateVerification`, the bare-metal
attachment's `vlan` (with [B74](m1.md#b74)) and `hardware.boot` leave
v1alpha1 and refuse as unknown fields, and bare metal no longer requires an
`attachmentRef` (D94). An invalid Machine name or an unresolved fleet key
yields only its own diagnostic, and the fqdn grammar is the API's DNS rule.
The machines and substrates specs state each rule, the kind tables match the
schema, and the lab-baremetal and multidc-platform examples drop the removed
fields.

**Toward B342 and B380:** the libvirt bridge and a Machine's
`interfaceAttachments[].interface` take the Linux interface-name grammar,
which the machines spec's tables state; [B342](#x46--the-safety-and-defect-sweep-of-m1) keeps the
templates' escaping of the bridge for X46's digest window. The context store's
refusal of a context with no revision carries its remedy as the remediation,
which [B380](#x46--the-safety-and-defect-sweep-of-m1) no longer lists.

**Operator-visible effects:** media URLs are HTTPS only (D86), so
`media add --from-url http://...` exits 2, and a redirect refuses. `media list`
gains a reserving-contexts column and, with `--checksums`, a computed-digest
column; its state tokens are stored, verified and mismatch where they were
corrupt and reserved; its JSON row gains `reservedBy` and `computed` and omits
`verified` when nothing was verified; and `media add` and
`media list --checksums` show progress. Context commands exit 2 on a sole
empty repeatable flag, refuse with new wording under one missing-input code,
present their plans before the prompt and keep an identical update silent.
Setup and preflight report the execution-foundation and FIPS-mode checks, and
a dry run the state-root check; a drifted glibc or libgcc file is named with
its package and build; setup runs keep 4 MiB of output; and `Next` lines
follow remedies. `validate` refuses more: the service, libvirt provider and
Machine shapes above, which earlier builds admitted, so an input that
validated before may now refuse; and the removed
`disableCertificateVerification`, bare-metal `vlan` and `hardware.boot`
fields refuse as unknown. IPv6 listener impacts and artifact URLs print
bracketed. **Digest effects:** no request or record version, setup or
controller record format, setup plan digest or automation digest moves, and
nothing under `ansible/` changed. The fields D94 removed were frozen in no
request: `TestExampleRequestsKeepTheirBytes` pins the libvirt host and
Machine, installation and bare-metal requests of lab-rhel, lab-sno and
lab-baremetal to goldens written at X42's base, and they pass unchanged, as do
the installation request goldens and the plan-digest golden, while the
effective goldens lose only the removed fields. The catalog's per-release
package attribution sits outside each release's execution profile, which
TestTheCompiledExecutionFoundationIsUnchanged pins, and the cancellation
evidence sits outside the setup plan digest. Only IPv6 moves bytes: an
IPv6-bound service's plan digest moves through its bracketed impacts, and an
IPv6 artifact endpoint's installation requests freeze bracketed URLs, with no
version bump, because the unbracketed form an earlier build froze was
unusable.

**Review:** the lanes integrated without a conflict or an integration fix.
The review raised seven findings, six confirmed, all in X42's scope and two
of them blocking; the seventh, a bridge grammar admitting a trailing `+` that
firewalld reads as a wildcard, was not confirmed as X42's, since it predates
X42, and became [B410](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation). The first fix round fixed five and part
of the sixth: abandoning a context over unreadable evidence deleted its
keyring without naming the custodied kubeconfig's export (blocking); a
missing evidence file in a present context directory had no exit and named
neither the context nor a remedy (blocking); a replacing add that adopts a
retained stage showed a source its record would not carry; the setup help and
command row said the purge cancels only at the bound; and the
certificate-coverage remedy named `secret generate` without the import before
it, so following it looped. Of the sixth, which found status named as the
inventory and next step although it refuses a live context whose stored input
the new rules refuse, the round made the update refusal name the destroy
outright; the rest is status over a revision that no longer compiles, which
stays parked as [B226](backlog.md#b226) with X42's case added. Three checks
followed. The first found three defects the fixes introduced, a misplaced
doc comment, a spec sentence giving `context update` a `--context` flag and
absent evidence worded as an unrecognized record, all fixed in the second
round, beside the residual, which it recorded; the second found only the
residual; the third round had nothing to fix, and the third check found no
gap.

**Gates:** on the integrated lanes, `make check-offline tidy-check
modules-check vulncheck docs-check race` passed, as it did after each of the
three fix rounds and on the squashed commit; each check round's
`make check-offline docs-check` passed; and `./scripts/check-commits` and
`git diff --check` pass. `./scripts/ansible-check` was not required, since
nothing under `ansible/` changed, and is unrun. The adversarial review, by
reviewers that did not write the diff, is the independent review the items
name. The RHEL 9.8 builds in the foundation record come from the brief, not
from a RHEL host, and no unprivileged test reaches a root-owned state root
the invoker cannot list. No real-host run.

**Constraints left behind:** a live context whose stored input a new
admission rule or a removed field refuses keeps its destroy, which plans from
the frozen plan, while its next fresh apply, its plan and its `status` refuse
until `context update` imports corrected input; status over such a revision is
[B226](backlog.md#b226). An IPv6 service operation an earlier build
registered keeps its frozen unbracketed URLs, so destroy it before applying
again, and a request an earlier build froze with no probe target no longer
proves presence, while its destroy is unaffected. Tracked elsewhere: the
templates' escaping of the bridge ([B342](#x46--the-safety-and-defect-sweep-of-m1)); the generic
execution-failure remedy that an rpm lock conflict, an unsafe root or an
unverified bundle location still gives when reached from apply or destroy
([B379](#x46--the-safety-and-defect-sweep-of-m1)); a receipt that this build leaves pending after a native
action refused before authorizing, which only the build that moves the
automation digest can cancel (D93, [B297](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); realization reading
the uncomposed template, so an override that adds the only ethernet interface
passes validate and refuses at plan ([B338](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); overlapping managed
prefixes across contexts ([B295](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); a managed service's egress
through a managed or authenticated Proxy, refused only at plan
([B382](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json)); the RHEL 9.8 foundation record, for the RHEL 9.8
controller run D60 plans to confirm ([B335](backlog.md#retired) beside it); and a
guard port that lists the owned blocks ([B324](backlog.md#b324)). Under D116
these join M1 on X46: an elevated selection refusal drops its remedy
([B398](#x46--the-safety-and-defect-sweep-of-m1)); the `context delete` row still promises a listing of
what an orphan-acknowledged deletion abandons, and a lost context's plan says
its keyring is removed ([B399](#x46--the-safety-and-defect-sweep-of-m1)); unsafe mutation evidence has no
named exit ([B400](#x46--the-safety-and-defect-sweep-of-m1)); stage-collection, bounded-run and setup-run
refusals name absolute paths, and setup's store backstops name no remedy
([B401](#x46--the-safety-and-defect-sweep-of-m1)); the dry run's state-root inspection follows a
symbolic-link ancestor the store refuses ([B402](#x46--the-safety-and-defect-sweep-of-m1)); a setup action
whose adapter failed before any native record stays unknown
([B403](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); a `contextStore` serving certificate is proved only
after registration, and missing material names no Secret
([B404](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json)); the `metadata.name` grammar refusal names no object or
remedy ([B405](#x46--the-safety-and-defect-sweep-of-m1)); a managed service's name can outgrow its block
identity ([B406](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json)); overlapping managed prefixes on one host in one
context are admitted ([B407](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); a mid-body media cancellation is
misnamed, and one unreadable media entry hides the listing
([B408](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); `media list --checksums` holds the root lock while it
hashes ([B409](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); and a bridge name may end in firewalld's
wildcard ([B410](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)). Under D48 (D116) the other follow-ups were
parked: B411 to B419.

### X43 — Libvirt roles, managed-OS installation, network composition and controller automation

**Owner:** Substrate, Managed OS, Machine, Controller setup and Workspace, with
State reconciliation. Integrated on local `main` on 2026-10-07 as one commit,
the first slice to move request versions and the automation digest.
**Items:** B200, B264, B234, B295, B17, B296, B338, B297, B343, B384, B407,
B410, B381, B403, B408, B409. **Decisions:** D56, D61, D66, D79, D93, D95 to
D98, D107, D116. **Partly delivered:** [B270](#x93--the-owner-run-of-2026-10-14), its emulated BMC
unit.

**Outcome, B295, B407 and B410:** a libvirt provider host now also claims its
virtual-media pool and every managed network's masked prefix, so a second
context whose pool name collides, or whose prefix is identical to, nested in
or encloses a held one, IPv4 or IPv6, refuses before any effect instead of
adopting the pool or routing the prefix to a second bridge. Every conflicting
held claim is its own `controller.conflict` diagnostic naming the holding
context, the held socket, bridge or prefix (only the class of a path, unit,
libvirt or BMC key), the holder's object, this context's object and the field
that chose the key, with `bootwright destroy --context <holder>` first or that
field as the remedy; a holder's completed continuation still refuses, and the
single class-only diagnostic and its remedy that could not work are gone.
Admission refuses two managed attachments on one host, of one provider or two,
whose prefixes overlap, naming both providers (B407, D95). A libvirt bridge
name takes a new `bridge` grammar, the interface-name grammar without `+`,
which firewalld reads as a wildcard while the managed network puts its bridge
in zone `trusted`; the NMState interface fields keep the interface-name
grammar (B410).

**Outcome, B264, B200, B234 and B384:** the libvirt host role owns exactly what
it defined. A same-named network is this context's only when its metadata names
this context and the attachment, and a same-named pool is this provider's only
when its definition targets the frozen directory, so another context's network
or pool is foreign and refuses, naming it, on apply and on destroy before any
network or pool is defined, stopped or removed (B264). An external attachment
is proved as a bridge device. Each network and the pool start before they are
set to autostart, so a failed start leaves no definition that starts with the
host; the observation reads autostart, and a replay that finds one off
switches it on again and reports the change (B200). Host evidence gains
`autostart`, `poolAutostart` and `poolOwned`, and presence refuses a managed
network or pool not set to autostart and a pool that does not target its
frozen directory. A destroy removes the provider's and the context's
directories beneath the libvirt image root with a plain `rmdir` once each is
empty, never recursively, and proves the pool path sits exactly three levels
below the retained prefix. The running-guest refusals name
`bootwright machine stop --context <context> --name <machine>` (B384, D66),
and the substrates spec quotes what the code emits. The unused pool template
is removed, a test holds that every role template is rendered by a task, and a
structure rule holds that every rescue ends in an unconditional failure
(B234).

**Outcome, B295 and B264 for the Machine role:** the libvirt machine request
freezes the provider host Machine's normalized egress, so the emulated BMC's
image pull takes exactly that route with every spelling of the proxy variables
and no ambient one, and a managed or authenticated proxy refuses with
`lifecycle.state`; the pull runs only when the pinned image is absent. A
same-named domain is this Machine's only when its metadata names this context
and Machine and its UUID is the frozen one, so another context's or Machine's
domain refuses before any effect on apply and on destroy. The emulator's
configuration renders every request string as JSON data (D61). A machine
destroy removes the context's empty directories with `rmdir`, never
recursively, and its refusal to remove a domain that is not shut off names
`machine stop --context`.

**Outcome, B270 in part:** the emulated BMC unit stops with SIGINT, because the
emulator runs as process 1 and has no SIGTERM handler, so podman waited out its
timeout and killed it and the unit stayed failed. Its template golden holds the
stop signal. The proxy unit's half is X24's ([B20](#x93--the-owner-run-of-2026-10-14)); B270 keeps its
row, narrowed to the proxy, and the proof that no unit stays failed after a
destroy needs a host.

**Outcome, B296, B17 and B343:** a plan freezes each image an installation
uses at the host media store's record, its SHA-256 and size, read once through
a port bound to the context store under the lifecycle's shared lock. Before
registration the plan refuses an image the store does not hold, one whose bytes
no longer have their recorded size and one the store lists as failed, each
naming the store's cause and a `bootwright media add` remedy that carries its
source flag; a declared MachineImage checksum is compared with the record,
never frozen in its place (D96). Each apply attempt proves every image's size
and digest before it extracts or builds from it. The published package tree
carries its DVD's digest, so a tree another DVD published is withdrawn and
extracted again. A fresh installation reports `changed` and a replay
`unchanged`. Every `ssh` the collection runs and the runner's SSH arm read a
generated configuration that holds only the host crypto-policy include, take
no ambient known hosts, proxy, control socket, agent or identity, and carry
`ServerAliveInterval=15` and `ServerAliveCountMax=3`. The work area moved under
a root-only 0700 parent beneath the Bootwright state area, proved before use
(B17, D98); per-invocation scratch stays B303. The apply fetches one byte of
the installer image and of the tree's `.treeinfo` through the listener before
the boot block and refuses naming the URL and status, and an HTTPS publication
freezes and binds its serving certificate. The Kickstart renders what it used
to validate and drop (D97): formats other than the language write the locale
file and add the langpack package; repositories are written in the `%post` as
repo files with name, base URL, enablement, `gpgcheck`, `gpgkey` and proxy; the
Machine's own proxy, else the profile's, reaches each repository by scheme
except the artifact endpoint and the `noProxy` matches, while a credentialed or
URL-less proxy refuses when a repository is configured;
`passwordAuthentication: true` refuses; and the architecture is `x86_64`. The
guard refuses an empty package source, an entry starting with `-` and a
repository ID that is empty or holds a slash, and admission refuses a mirror
repository ID, display name or key URL the guard would refuse (B343).

**Outcome, B338 and B381:** realization and installation compose
`spec.network.overrides` into the template, through one deterministic merge now
beneath the substrate and machine packages, so an override that adds, removes
or reroutes an interface is realized on the libvirt domain and in the
Kickstart, ethernet interfaces whose state is `absent` or `ignore` are
skipped, and the install gateway is the next hop of the first composed
non-absent route with a zero-prefix IPv4 destination and an IPv4 next hop. An
override that cannot merge yields one refusal that does not echo its keys. The
two Machine admission refusals that required a configured network name the
step, selecting `spec.network.configRef` or `spec.network.inline` or removing
the selection or interface, and an OS-ready Machine is told only to remove it
(B381).

**Outcome, B297 and B403:** every Python-side download works on Fedora 43 and
RHEL 9, whose system trust bundle carries UTF-8 labels: the loader reads only
the certificate blocks and refuses an empty bundle or a TLS failure as a
system-trust refusal. A refusal carries one closed class instead of a sentence
that blamed the host: the helper writes the class and one bounded detail line,
the adapters carry the class and its source in a refused record, Go maps each
class to a diagnostic and remedy naming the source's host, and raw lines reach
only the private setup run output; a native class after an authorized
transaction reports `controller.unknown`. Native packages each download under
their own acquisition deadline and together under a staging bound that scales
with their declared bytes under the two-hour ceiling, with one connection pool
per route. A setup run that fails after publishing its preparation but before
any native record was acknowledged is recorded failed with its intent, so the
next setup replaces it (B403). Setup qualifies a vendor-signed z-stream glibc
or libgcc of the qualified minor against the RPM database (D107): it reads each
installed instance with its signatures and file digests, qualifies one x86_64
instance per package of the compiled upstream version, derives the launch
requirement in the compiled shape and verifies it byte for byte, and records it
on the receipt, which gains an omitted-when-empty foundation member bound into
the plan digest. A host holding the compiled builds records nothing new, every
launch stays byte-exact, another upstream version or minor, an unsigned or
foreign-signed build and a file differing from its digest still refuse naming
the build, and a launch's drift remedy opens with the one `bootwright setup`
that qualifies a dnf update. A fresh bootstrap resolution qualifies against the
same qualified foundation. A storage refusal names the filesystem its source
was filling, the staging scratch for a package and the client bundle area for a
tool.

**Outcome, B408 and B409:** a media copy or download canceled mid-body reports
the cancellation. One unreadable media entry, a record that cannot be read
safely or decoded or an image file the store refuses as unsafe, no longer
refuses the whole `media list`: it is a row in the state `failed`, shown with
`-` for size, digest and added time, a Failed group naming each image and its
reason and one remedy line, and in JSON without size, digest, source or added
and with a `reason`; the listing stays `OK`. `media list --checksums` reads the
entries under the shared root lock and hashes each image through a descriptor
it holds after the lock is released, so every exclusive command proceeds while
it hashes, and an image whose status changed or that was unlinked meanwhile is
listed failed. The contexts spec's lock table says so.

**Review:** the lanes integrated without a conflict or an integration fix. The
review raised seven findings, six distinct, all confirmed and in scope, two of
them, one defect reported twice, blocking: a plan pinned an image the store
listed as failed with an empty digest and size 0 (B296 against B408); a fresh
bootstrap resolution on a z-stream host still qualified against the compiled
digests, so setup refused the host D107 qualifies (B297); and, as minor
findings, media remedies naming `media add` without the source flag the command
requires, a cross-context BMC conflict that printed an internal slug with no
object or field, a storage refusal that named the staging scratch for a tool
source, and the agent media request's content moving without its version. One
fix round fixed every finding but the version gap, each with a regression test that fails with the
fix reverted; the plan now refuses a failed image or a record with a malformed
digest or no size naming the store's cause, every media remedy comes from one
helper that names `--from-file` or `--from-url` with its digest, the resolution
qualifies against the launch requirement and records the compiled one, the
conflict maps the physical-server key to its Machine and the BMC address field,
and the storage refusal names the area. The version gap, that
`cluster-media-agent-v5` encodes different content since X43, was not fixed:
two lenses called it a version-discipline gap and one not real, since the
version only selects the decoder and a frozen request still decodes and
destroys what it froze; it is parked as [B434](backlog.md#b434). The check
after the round found no gap.

**Gates:** on the integrated lanes, `make check-offline tidy-check
modules-check vulncheck docs-check race` passed, and so did the four
`./scripts/ansible-check` suites, units (198 passed), sanity, integration and
lint (no failures or warnings under the production profile), as did
`./scripts/check-commits` and `git diff --check`; the fix round's `make
check-offline docs-check` and the seven touched packages passed, and on the
squashed commit, whose code is the fix round's, `make docs-check`,
`./scripts/check-commits` and `git diff --check` passed. The adversarial review, by reviewers that
did not write the diff, is the independent review the safety-class items name.
The native opt-in integration targets and every effect on a host are unrun:
the emulator's clean stop, `-F` with the crypto-policy include on RHEL 9's
OpenSSH, a 206 answer from the managed listener, root writing the tree identity
into an extracted tree, the cost of hashing a full DVD on every apply, the
errata qualification on a RHEL 9.8 or Fedora 43 host and a classified warning
reaching the retained run output are in-tree only
([B433](backlog.md#b433)). No real-host run.

**Digest effects:** the first slice to move them. The libvirt machine request
moves from `machine-libvirt-v2` to `machine-libvirt-v3` and freezes the
provider host's egress, so its content digest, its request goldens and its
template goldens move; the libvirt host request and its version are unchanged,
while host evidence gains three members. The installation request moves from
`os-install-anaconda-v5` to `os-install-anaconda-v6`: media carry a size and
always a SHA-256, an HTTPS publication freezes its certificate reference, the
Kickstart version moves to `kickstart-anaconda-v5`, and the installation
content digest and every plan digest of a context with an installed Machine
move. The automation digest moves through the libvirt host and machine roles
and plugins, the managed-OS role and plugins and the controller adapter roles
and plugins, so every plan digest moves with it; a running emulated controller
restarts once on the next apply. The controller capability request, never
persisted, moves from `controller-prerequisites-v4` to `controller-prerequisites-v5`
and strands nothing. A setup receipt written on an errata host may carry a
foundation member that an earlier build refuses through strict decoding. Two
changes move content without a version: the libvirt machine request's
interfaces and the installation request's gateway for a Machine whose overrides
change interfaces or routes, whose template declares an absent or ignored
ethernet interface or whose first default route is absent, and the OpenShift
agent media request's AgentConfig interfaces and `networkConfig` MAC addresses
for a node Machine with such overrides, whose version stayed
`cluster-media-agent-v5` and whose content digest re-plans its block as
changed ([B434](backlog.md#b434)). No setup record format, media record
format or example plan golden moves. Contexts applied before X43 hold no
prefix or pool reservation until their next apply. **Before a build that
contains X43 touches a host, destroy every live context with the build that
applied it, then run `setup` once, with `--purge-old-bundles` when the host is
at its bundle bound.**

**Operator-visible effects:** `validate` and `plan` refuse more: overlapping
managed prefixes on one host, a bridge name ending in `+`, a Machine whose
install profile asks for what the Kickstart now renders and refuses, an image
the store lacks, lists as failed or whose bytes changed, and a proxy that has
credentials while a repository is configured. A conflict between contexts names
both objects and the field. `media list` shows failed rows, and a checksum
listing no longer blocks other commands. Setup on a host whose glibc or libgcc
dnf updated needs one `bootwright setup`, and its refusals name a cause class
and a remedy instead of blaming the host.

**Constraints left behind:** [B270](#x93--the-owner-run-of-2026-10-14) keeps the proxy unit's stop
([B20](#x93--the-owner-run-of-2026-10-14), X24). A Machine and an InfraProvider of one name share a
directory, and the Machine's destroy can remove the provider's pool
([B420](#x46--the-safety-and-defect-sweep-of-m1)); the 64-key reservation bound meets an unbounded
attachment list ([B421](#x46--the-safety-and-defect-sweep-of-m1)); the install profile still admits fields
it ignores and repository IDs dnf refuses ([B422](#x46--the-safety-and-defect-sweep-of-m1)); two Machine
refusals keep the slogan remedy ([B423](#x46--the-safety-and-defect-sweep-of-m1)); a zero-prefix default
route spelled otherwise is refused ([B424](#x46--the-safety-and-defect-sweep-of-m1)); a controller-stage
install or a canceled setup that failed before any native record is still
`unknown` ([B425](#x46--the-safety-and-defect-sweep-of-m1)); and the failed media row of a directory has no
working remedy ([B426](#x46--the-safety-and-defect-sweep-of-m1)). The installation request's SSH placement arm,
whose material the role writes only on the controller, is unreachable since D82
and is [B387](backlog.md#b387)'s removal. Under D116 those seven join M1 on X46; the rest,
B427 to B434, were parked under D48.

### X24 — Adapter protocol, managed-service role, observation reasons and canonical JSON

**Owner:** Architecture, with State reconciliation, Controller, Infrastructure
services and Substrate. Integrated on local `main` on 2026-10-07 as one commit,
the second slice to move the automation digest and request versions, in the
window X43 opened. **Items:** B242, B298, B299, B21, B223, B246, B22, B252,
B382, B383, B404, B406, B359, B378. **Decisions:** D11, D56, D57, D66, D77,
D99, D114, D116. **In-tree gates passed, host gate awaiting operator
acceptance:** [B19](#x93--the-owner-run-of-2026-10-14), [B20](#x93--the-owner-run-of-2026-10-14) and [B270](#x93--the-owner-run-of-2026-10-14); their
rows stay on M1 with Delivery `awaiting operator acceptance`. **Partly
delivered:** [B233](#x44--help-completion-contracts-and-ratchets), its dead reporters.

**Outcome, B19, B242 and B298:** the lifecycle runner and the controller
runner decode the adapter result protocol with one decoder and supervise their
adapter with one runner core, in the technical package `internal/adapterprotocol`,
which imports nothing first-party; one table says which phases each runner
admits and each runner keeps its own requests, deadlines and diagnostics. The
decoder is strict for both: it refuses a non-ASCII byte, whitespace outside a
string, members out of order or equal under case folding at any depth, nesting
past 16, data after the object, a member outside the phase's exact set and a
completion whose evidence is not a non-empty object, so a lifecycle run fed such
a record ends `unknown` where it could end `failed`. A named refusal closes the
acknowledgement channel in the lifecycle run too. An acknowledgement whose write
fails with `EPIPE` proves no adapter holds the channel, so the adapter's exit
decides as if it had been read first and a failed exit stays failed in both
runners (B242). The controller's cancellation and deadline take the lifecycle's
stop path unless a native acknowledgement was delivered: the channel closes, the
supervisor is signaled first, and the group is killed once the adapter is
reaped or the drain passes, with the controller supervisor carrying the same
termination handler, so ansible-core 2.21's session-isolated workers end with
the run; both runners bound the wait for descendants that hold the output and
end `unknown` naming them (B298). On the Ansible side the nine capability
action plugins share one `adapter_protocol` module for phase dispatch, group
statuses, outcomes, the digest check and the publish rule, keeping their
evidence functions and documentation stubs; a goldens test recorded every
plugin's records before the change and passes after it. Progress emission
stays in roles and postcondition decisions in Go (D99, D11).

**Outcome, B20, B270, B299 and B252 for managed services:** one role,
`infra_managed_service`, realizes the DNS server, the NTP server and the proxy:
a fixed table keyed by the validated frozen kind selects only the daemon
configuration and unit templates, the nine playbooks keep their names, and every
rendered configuration and unit is byte-identical to its golden. The frozen
request version is `managed-service-v3` for all three. Its decoder reads the
version first and names an older one with the remedy to destroy the context
with the build that applied it, instead of calling a request with a
since-removed member malformed (B252). Squid sets a two-second shutdown
lifetime, so its stop ends within podman's timeout instead of ending in SIGKILL
and leaving the unit failed (B270, B20); the proxy also denies the cache
manager, loopback, unspecified and link-local destinations, IPv4 and IPv6,
before it admits a client (B299). A managed-service or artifact-server destroy
removes the kind's and the context's directories with `rmdir` once empty, never
recursively and never the prefix, and the roles prove the content root sits
exactly at its depth. Before any effect, while the unit is not active and no
container exists, the two roles read PID 1's TCP and UDP socket tables and
refuse when something else listens on a port at an address the service's bind
covers, through the protocol's refused record with the reason
`foreign-listener-<port>`; Go turns it into a `lifecycle.state` refusal on the
service that names the socket it binds, and for a wildcard bind, the DNSServer
default D114 keeps, the port and the bind that covers it and a pointer to the
retained run output, which lists every socket found, not the colliding address
([B438](backlog.md#b438)). The remedy is to stop what listens there or choose
another bindAddress, and another port only for a Proxy and an ArtifactServer,
because validation pins a DNSServer to 53 and an NTPServer to 123. The base
token `foreign-listener` is spelled identically in the roles' refused task, the
plugins' refusals and Go's constant, and the port-suffixed form is the wire
contract on both sides.

**Outcome, B21, B223 and B246:** an observation that cannot run is recorded once,
by the engine, on the attempt's resolution record, and `status` names it before
any capability reads its evidence; the five private recorders are gone and
capabilities return the run's failure. Every capability that can return evidence
and still leave a block unknown now names why, for the verb the operation
froze: the libvirt host and machine, the bare-metal machine, managed services,
the artifact server, managed-OS installation and the agent media and install
blocks, through one projection of the deciding refusal ("<subject> on <host> is
not what its <verb> froze: <refusal>"), and the resolution log records an
`unresolved` line. A drifted libvirt machine's apply resolution therefore names
its first difference. The remedy for a drifted subject is to restore it to what
its frozen request names; destroying cannot help, because a destroy over an
unknown apply resolves its blocks with the apply's own check ([B440](#x46--the-safety-and-defect-sweep-of-m1)).

**Outcome, B359 and B378:** a destroy that replaces a failed removal records the
removal it replaces in an optional `replaces` member, and `status` reads the
chain, so an object the replacement has not started reads what the replaced
removal did to it (unproved gives unknown, failed gives failed, gone gives
pending) and never the apply's done; a replacement an earlier build recorded
keeps its earlier reading. `status` offers a continuation that still runs a
block only when the recorded refusals pass, setup is complete, the controller
state records a binding and the frozen closure passes; where one refuses it
offers the destroy that supersedes an incomplete apply, or the context's
deletion when the refusal's remedy names it, and otherwise no step.

**Outcome, B22 and B252 for every decoder:** nine closed-decode and re-encode
proofs in seven packages share the technical package `internal/canonicaljson`
(encode with a bare or line ending, closed decode with one trailing-data rule,
prove, size). Each format keeps its own byte bound, pre-scan, line feed, error
codes and words, and every golden is byte-identical; a fitness test refuses a
marshal-and-compare outside the package against a closed, shrinking allowlist.
A plan encodes every unset list of its blocks as `[]`, not `null`, which moves
every plan digest, and a plan an earlier build froze with `null` refuses on read
with the remedy to destroy the context with the build that registered it. Every
frozen-request decoder, the libvirt host and machine, bare-metal, installation,
artifact-server, cluster media and install and controller prerequisites among
them, reads the request version first through one call, names it and gives that
remedy.

**Outcome, B382, B383, B404 and B406:** `validate` and `plan` refuse earlier and
name their object. A managed ArtifactServer, Proxy, DNSServer or NTPServer name
longer than 63 bytes minus its block prefix, 47, 57, 59 and 59, refuses at
`metadata.name` naming the limit, and the API page states them (B406). Managed
services refuse before registration, as refusal-table rows, a managed,
authenticated or private-trust Proxy on the placement Machine and, for a
placement Machine other than the controller, an `operatorIdentity`, a
`passwordRef` or a missing `knownHostsRef` (B382). Every Proxy consumer holds
the proxy grammar: an external Proxy's endpoint that is a URL but not a bare
endpoint and each `noProxy` entry outside the bypass grammar refuses at its own
field on a Machine, an install profile, a cluster installation or an
Environment default, and the remedy names no field path of another object
(B383). An ArtifactServer's `contextStore` serving certificate is proved for
coverage, expiry and usage when a fresh apply binds it, before registration,
and missing bound material refuses on its Secret with a remedy carrying
`--context` (B404, D66).

**Outcome, B233 in part:** the managed service's list of unsupported names and
the test-only reporters of the libvirt, managed-OS and agent-install
capabilities are removed. [B233](#x44--help-completion-contracts-and-ratchets) stays on M1 for the unreachable
agent-install checks, the refusal-table tests in `make docs-check` and the
engine's table, and for the artifact server's `Unsupported`, which now also
lists placement-row refusals.

**Review:** eleven sub-items in five lanes integrated with three textual
conflicts, resolved keeping both sides, and one integration fix: the observation
lane removed the managed-service package's edge to compilation from the
architecture table and the admission lane then used it again, which the
dependency-direction test caught. Each sub-item had its own two-lens
verification; six needed a fix folded into their commit. The slice review, by
reviewers that did not write the diff, raised four findings: two confirmed in
scope, none blocking, and fixed with a test that fails with the fix reverted
(a bypass remedy that named `spec.install.proxy` on an Environment default, and
a foreign-listener remedy that offered a DNSServer or an NTPServer a port
validation pins), and two not confirmed, which are kept as [B436](#x46--the-safety-and-defect-sweep-of-m1) and
[B443](backlog.md#b443): a destroy after a failed apply may leave an
unreachable unit listed failed, and the canonical-JSON fitness test misses a
marshal-and-compare split across two functions. The second round changed no
code; its check found that M1's B299 exit evidence quoted the old remedy, which
this record replaces with what the code does, and that the refusal names no
colliding address for a wildcard bind ([B438](backlog.md#b438)).

**Digest effects:** the second slice to move them, in X43's window. The
automation digest moves through the shared plugins, `module_utils`, the merged
role and the controller supervisor, so every plan digest moves with it. The
managed-service request versions `dns-server-dnsmasq-v2`, `ntp-server-chrony-v2`
and `proxy-squid-v2` become `managed-service-v3`, and the roles
`infra_dns_server_dnsmasq`, `infra_ntp_server_chrony` and `infra_proxy_squid`
become `infra_managed_service`, so every managed-service request digest, each
definition's content digest and every plan digest of a context holding a
managed service move; an old block still resolves to its capability and refuses
by naming its version. Every plan digest moves again through the canonical
encoding of unset lists as `[]`. The attempt record gains an omitted-when-unset
`failure` and the operation record an omitted-when-unset `replaces`, with no
record version moving; an earlier build cannot read a record that carries
either. The controller request stays `controller-prerequisites-v5`, the
artifact-server request, the evidence shapes, the setup record formats and every
other golden are unchanged. **Before a build that contains X43 or X24 touches a
host, destroy every live context with the build that applied it, then run
`setup` once, with `--purge-old-bundles` when the host is at its bundle bound;**
one such destroy and setup serve both slices, and the next window opens with
[X46](m1.md#planned-slices).

**Operator-visible effects:** an apply refuses a socket another daemon holds
before it pulls, publishes or starts anything, and names it. `status` reads a replaced removal's blocks, says why a block stayed
unknown and never offers a continuation its proofs refuse. A Squid stop ends cleanly, a destroy
leaves no empty service directory beneath the services prefix, and the lab-rhel
README ends its journey by listing failed units, which must be none. A
cancelled setup or controller stage no longer leaves session-isolated workers
running. `validate` refuses the shapes above before registration. The adapter's
real behavior on a host is the closing run's.

**Gates:** on the integrated lanes at their integration head, `make
check-offline tidy-check modules-check vulncheck docs-check race` passed after
one integration fix, and so did the four `./scripts/ansible-check` suites,
units, sanity, integration and lint (no failures or warnings under the
production profile), `./scripts/check-commits` and `git diff --check`; the fix
rounds' heads passed the same commands. On the squashed commit, whose code is
the fix rounds' and which differs from them only under `specs/`, the same
`make` targets passed, and so did the four `./scripts/ansible-check` suites
(units 227 passed, integration the controller supervisor test), `make
docs-check`, `./scripts/check-commits` and `git diff --check`. The adversarial
review, by reviewers that did not write the diff, is the independent review the
safety-class items name. `make race` ran over the reconciliation, privilege and
command packages as the Makefile defines it. Every effect on a host is unrun:
the adapter on a live ansible-core, Squid on its pinned image and the
cancellation of a live dnf tree are in-tree only ([B442](backlog.md#b442)). No
real-host run.

**Constraints left behind:** [B19](#x93--the-owner-run-of-2026-10-14), [B20](#x93--the-owner-run-of-2026-10-14) and
[B270](#x93--the-owner-run-of-2026-10-14) complete on an owner-accepted ledger row of D59's closing
run, which carries their real-host apply, destroy and no-failed-unit proofs; the
other host observations are [B442](backlog.md#b442). The binding-proof port
stays [B302](#x44--help-completion-contracts-and-ratchets)'s and the artifact server's `Unsupported`
[B233](#x44--help-completion-contracts-and-ratchets)'s. Under D116 three follow-ups join M1 on X46: a destroy that
refuses a content root with a `..` component ([B435](#x46--the-safety-and-defect-sweep-of-m1)), the reset of
a failed unit after a failed apply ([B436](#x46--the-safety-and-defect-sweep-of-m1)) and three continuation
edges ([B437](#x46--the-safety-and-defect-sweep-of-m1)). Under D48 eight were parked: B438 to B445.

### X45 — private installer delivery and physical Redfish variants

**Owner:** Substrate and Managed OS, with Infrastructure services. A slice of
M4 delivered out of sequence on the owner's request (D60), on 2026-10-08 as one
commit, the third slice to move request versions and the automation digest, in
the window X43 opened. **Decisions:** D60, D103, D104,
D110, D113, and, recorded after the run, D120, D121 and D122. **In-tree gates
passed, operator gates awaiting acceptance:** [B73](m1.md#b73), whose
acceptance baseline is this commit, and [B305](m1.md#b305); their rows stay on
M4 with Delivery `awaiting operator acceptance`.

**Outcome, B73:** a physical installation no longer refuses before
registration. A delivered-key Machine's installer image, whose Kickstart names
the tokenized host-key URL, is no longer served at the public
`os/<machine>/install.iso`: the apply renames it into
`private/os/<machine>/<token>/`, beside the key pair, hidden, owned by root and not world-readable,
after clearing whatever an earlier attempt left beneath the private
root. Before it inserts anything it fetches the first byte of the image, the
identity and the public key through the listener, with no proxy and no
redirect, verified against the bound serving certificate; each fetch is judged
alone and names the file and the cause with the token replaced. The insert
takes the private URL and its failure never prints the token. The private
material is withdrawn before the completion inspection runs, so the inspection
reports what is left, and the postcondition of a private installation requires
the image withdrawn as well as the key pair; a destroy removes the public image
directory only for the public arm. The installation request carries exactly one
publication, the public `image` of the guest-agent arm or the `private`
publication of the delivered-key arm, which needs https and a serving
certificate; the request version `os-install-anaconda-v6` becomes `v7` and the
Kickstart version `kickstart-anaconda-v5` becomes `v6`. The blanket refusal is
replaced by D103's: a delivered-key installation whose controller leg is not
https with its certificate verified, through `bmc.tls.verify` or a
`trustBundleRef`, refuses before registration (a refusal-table row), and an
apply whose frozen request holds both publications or neither, or a private one
over an unverified leg, refuses at execution naming the Machine and directing
the operator to destroy and plan again, while its destroy and observation still
run.

**Outcome, B73, the host key (F-035):** the request's target carries the
generated key type of the Secret its `hostKeyRef` names; a missing Secret refuses
as `api.reference`, a Secret with no generated source as `lifecycle.unsupported`
and a type outside the table as `api.value`, all before registration. The
Kickstart installs the pair at the path of its own type (Ed25519, RSA or ECDSA),
proves that the public half names the frozen algorithm and that the private
half derives the same key, and names it in a
`40-bootwright-host-key.conf` drop-in beneath `sshd_config.d`, all before
`ssh-keygen -A`; an apply whose bound key is of another type than the frozen one
refuses before any effect, and observation and destroy never refuse on it. A
new filter pins `HostKeyAlgorithms` to the delivered key's type on every
connection of the reachability wait and the identity read, with SHA-1 RSA left
out, so a controller under the FIPS policy can prove an RSA or ECDSA key. The
[lab-baremetal](../../examples/machines/lab-baremetal/README.md) example names a
`caBundle` Secret in `trustBundleRef` instead of opting out of verification,
and its README states what the run needs: the disk the media and hosted trees
take, the egress destinations, the listener hand check, the firewall ports by
source (D110), the rule for a host whose store an earlier build holds, the FIPS
key types, the RSA-2048 serving-certificate workaround (D113) with the
`openssl` command that makes a leaf Bootwright accepts, the artifact unit's
journal for a failed fetch, and why the emulated rehearsal cannot run yet. The
managed-OS, infrastructure-services, substrates and machines specs and the
operator guide state it.

**Outcome, B305:** the collection's one Redfish client drives the physical BMC
under test as the owner's captures show it, proved by a redacted fixture of that
captured shape running every parametrized operation. Physical qualification
remains a separate gate. An echo that names no port
matches the inserted image while a different explicit port still refuses; the
InsertMedia body is exactly `Image`, `Inserted` and the transfer protocol; a
task's one-object `Messages` yields its message id; a device presenting other
media is released and proved empty before the first attach, and one that cannot
be released is never attached. Under `established` trust an insert that carries
private material first reads, writing nothing, the device's `VerifyCertificate`
and the `HttpsTransferCertVerification` of every security service a manager
links, and refuses before any write unless both read true (D104); the insert
asks for that read-back exactly when the installer image is private, and
`redfish_boot` refuses private delivery beside `disable-verification`. A task
that ends unsuccessfully or never settles, or a device that never presents the
image, ends the insert naming the BMC-side causes and the artifact unit's
journal. That journal now records each completed fetch, without its URI: the
remote address, the time, the TLS protocol and cipher, the method, the status
and the bytes, with the error log kept at warn. The MAC proof stays strict and
its refusal names each declared NIC the inventory lacks and each member that
proved nothing by position, never a reported value. The knowledge page on
physical BMCs is rewritten from the captures without host names or serials, with
the RSA premise and its check, and each of [B74](m1.md#b74) to
[B77](m1.md#b77) points to the pre-rewrite page that holds its field lessons.
Each of them retains its own implementation and host-qualification gates.

**Review:** four sub-items in two lanes integrated without a conflict and with
one integration fix: the insert bound grew by 3 minutes (36m20s to 39m20s) for
the release and the verification reads, which moved the controller calls an apply
makes to 1h4m20s and lab-rhel's run deadline to 4h9m20s, within the 6 hour
ceiling, and a test and the managed-OS spec still stated the earlier figures.
The slice review, by reviewers that did not write the diff, raised six findings,
five confirmed in scope and one not confirmed, three distinct defects among the
five. The installation's insert never passed the private-delivery argument, so
the D104 read-back never ran for a real installation, a blocking finding
reported three times; the README's journal command named the block's prefix
instead of the artifact server's unit; and the README's RSA-2048 command made a
CA certificate, which Bootwright refuses. One fix round fixed all three, each
with a test that fails without it, the first mutation-checked against dropping
the argument and against a constant true and shown end to end through the
captured-shape emulator, where a private insert now refuses with no write. The check
found no gap. The finding not confirmed, a spec sentence that read against the
container cluster's plain-HTTP controller leg, which its spec already records as
an exception, is part of [B452](backlog.md#b452).

**Digest effects:** the third slice to move them, in X43's window. Every frozen
installation request and its content digest move with the request version
`os-install-anaconda-v7` and the Kickstart version `kickstart-anaconda-v6`; a
libvirt installation, lab-rhel's included, changes only in the version string,
and a delivered-key request omits `image` and carries `private` and the key
type. The automation digest moves through the managed-OS install role, the
bare-metal machine role, the new filter, the protocol and inspect plugins, the
Redfish client, discovery and module and the artifact-server nginx template, so
every plan digest moves with it. The lab-baremetal goldens move with the
example. The bare-metal machine request `machine-baremetal-v2`, the libvirt host
and machine requests, the controller request and the evidence shapes do not
move. An artifact server an earlier build applied renders its changed
configuration at its next apply and restarts its unit; observation does not
compare the rendered file, so it reports no drift before then. **Before a build
that contains X43, X24 or X45 touches a host, destroy every live context with
the build that applied it, then run `setup` once, with `--purge-old-bundles`
when the host is at its bundle bound;** one such destroy and setup serve the
three slices, and the next window opens with M4's [X47](m2.md#planned-slices) and M1's [X46](m1.md#planned-slices).

**Operator-visible effects:** a physical installation whose controller leg is
verified plans, applies and proves completion against its delivered key, and an
unverified leg refuses before registration, directing the operator to an
https address, the `tls.verify` opt-out's removal and `trustBundleRef`; the
physical BMC under test needs `established` trust, the media CA imported out of
band and its HTTPS transfer verification turned on, or the apply refuses with
`NO_VERIFIED_FETCH`. A failed fetch names its causes and the
unit's journal, which now shows each completed request. A refused MAC proof
names the declared NICs it lacks. An installation's run deadline grows by 3
minutes, lab-rhel's to 4h9m20s. A failed attempt leaves its private subtree
until the next apply or a destroy takes it (D122, stated in the
[managed-OS spec](../managed-os.md#physical-installation)).

**Gates:** at the integration head, after the one integration fix, `make
check-offline tidy-check modules-check vulncheck docs-check race` passed, and so
did the four `./scripts/ansible-check` suites, units, sanity, integration (the
controller supervisor test) and lint (no failures or warnings under the
production profile), `./scripts/check-commits` and `git diff --check`; the fix
round's head passed the same commands, and its collection run counted 1570
passed. On the squashed commit, whose code is the fix round's and which differs
from it only under `specs/` and in two sentences of the operator guide and the
lab-baremetal README, the same `make` targets passed (all modules verified, no
vulnerabilities found in called code), and so did the four suites (units 264,
238, 1068 and 1570 passed across its runs, sanity, integration and lint),
`make docs-check` after the last wording edit, `./scripts/check-commits` and
`git diff --check`. The Go test step reused cached results for packages
unchanged since the integration head's run. The adversarial review, by reviewers
that did not write the diff, is the independent review the safety-class items
name. No real-host run: the rehearsal, the physical BMC observation and every
effect on a host are unrun, and each depends on X47.

**Constraints left behind:** [B73](m1.md#b73) completes on an owner-accepted
ledger row of the lab-baremetal rehearsal on a build that descends from this
commit, which X47's [B446](#x47--verified-emulated-controller-trust-and-bounds) makes runnable (D120), and
[B305](m1.md#b305) on physical hardware, which D60 records as an
observation and which needs [B447](#x47--verified-emulated-controller-trust-and-bounds)'s rule (D121) first; real
hardware is [B78](m1.md#b78)'s (D17). A
[residual race](../state-reconciliation.md#mutation-safety) between the last
controller proof and the installer's first write, and physical destroy that
retains the system ([B70](m1.md#b70)), stay. None of the follow-ups joins M1:
each lies in the physical-installation or the Redfish journey, so four joined
M4, the emulated controller's TLS ([B446](#x47--verified-emulated-controller-trust-and-bounds)) and the read-back's rule
with the silent `disable-verification` ([B447](#x47--verified-emulated-controller-trust-and-bounds)), which D120 and
D121 put on the new slice X47, a key type the controller's policy excludes
([B448](#x47--pre-effect-ssh-key-policy)) and the insert bound ([B449](#x47--verified-emulated-controller-trust-and-bounds)); under D48 three
were parked: the private subtree on plain listeners ([B450](backlog.md#b450)),
the host observations ([B451](backlog.md#b451)) and the wording and tests
([B452](backlog.md#b452)). Already tracked: the physical BMC's privilege refusal, HTTP
400, joined [B319](m1.md#b319)'s definition; the libvirt identity path's FIPS
key is [B346](m1.md#b346); and the TLS half of [B334](backlog.md#b334)
moved to B446 (D120).

### X46 — the safety and defect sweep of M1

**Owner:** State reconciliation, Controller, Workspace, Secrets, Trust, Desired
state, Substrate, Managed OS and Infrastructure services. Integrated on local
`main` on 2026-10-08 as one commit, the sweep slice before the closing run (D116),
in the digest window X43, X24 and X45 opened, and its second pass, the follow-ups
D116 attached, as five lane commits on 2026-10-08 with the sibling template's
migration in its own repository. **Items:** B340, B342, B344, B347,
B351, B357, B358, B360, B361, B370, B372, B373, B377, B398, B399, B400, B401,
B402, B405, B420, B421, B422, B423, B424, B425, B426, B435, B436, B437, B440;
in the second pass B379, B380, B453, B454, B455, B456, B457, B458, B459 and
B465. **Decisions:** D56, D66, D73, D88, D89, D94, D116, D117, D118, D119, D123,
D124. **In-tree gates passed, operator gate awaiting acceptance:**
[B339](#x93--the-owner-run-of-2026-10-14), whose row stays on M1 with Delivery
`awaiting operator acceptance`.

**Outcome, B357:** a confirmed trust write that takes over an endpoint,
`machine trust` or a first use, also removes the record of a still-declared
Machine that no longer uses the context's SSH trust: one reached locally,
installed by Bootwright, declaring no SSH access or declaring a `knownHostsRef`,
the controller Machine's included (D117, widening D65). The removal is a
`remove` row in the plan the write presents, counted and shown before the
prompt, written only by the confirmed or `--yes` write and never by a dry run or
a declined prompt. Neither path reads a lifecycle record, so it holds in every
lifecycle state with no input edit. The input-edit and controller-binding
remedies and the CLI spec sentence that promised a compiling input are gone.

**Outcome, B347:** a `machine trust` plan that cannot be written refuses
`trust.identity`, naming the context and the repeat with `--context`, and an
unconfigured presenter is `runtime.internal`, in place of an error with no
diagnostic that ended as an unsupported result.

**Outcome, B344:** admission refuses a generated certificate's common name that
is empty, not UTF-8, holds NUL or exceeds the part byte limit, exactly as
generation does. The check is one helper, and generation now enforces the byte
limit too.

**Outcome, B361:** the uninitialized-store refusals of store access, lending,
binding reopen and produced material name `bootwright secret encryption init
--context <ctx>`, and the orphaned-store refusal names its context. The retired
file-source remedy names `--context <context>`, filled with the context
`validate` read (`--context`, or the selected one), while `validate -f` keeps
the placeholder (D66). The secret commands, `context init` and `context update`
still print the placeholder ([B453](#x46--the-safety-and-defect-sweep-of-m1)).

**Outcome, B372:** a file-input `secret set` replacement, `secret delete` and
`secret encryption rotate` confirm before the lease (D118, applying D73). A
shared read records the current version, or for a rotation the active key, the
prompt holds no store lock and no context lease, and under the lease the command
refuses with `secret.store.conflict` and writes nothing when that version or key
changed meanwhile. A delete of a name with no current version asks nothing and
takes no lease; `--yes` keeps the single-transaction path. An unanswered prompt
no longer makes every other Bootwright command on the host refuse as busy. The
concurrency test is rewritten and the secrets spec says so.

**Outcome, B373:** an elevated `secret set` with `--value-stdin` or
`--password-stdin` refuses `secret.input`, naming the Secret, before sudo starts
the child and before any input is read, when the listing the supervisor already
reads (`sudo -n -ll`) sets `log_input`, `log_stdin` or `log_ttyin` as an
explicit, non-negated option in the matching Defaults, in a Runas, host, user or
command-specific Defaults line with several members or a member that carries
arguments, or in a rule's Options. It is a refusal, not a warning, because a
warning cannot protect piped input and JSON keeps standard error empty; the
remedy, a file flag or a root login session that sudo did not start (a console
or SSH login, or `su -`), always works, and the refusal says that a command run
through `sudo`, even from a `sudo -i` shell, stays under sudo's I/O log. The
2026-10-11 review added `log_ttyin`, which logs what is typed at the terminal,
and the corrected root wording. A listing that cannot be
read without a password proves nothing and the command proceeds, which the
secrets spec and the operator guide state and [B458](#x46--the-safety-and-defect-sweep-of-m1) closes. The
sudo argument vector, the child's environment and D73's acquisition order do not
change.

**Outcome, B339 (in-tree):** the elevated child exits at once with status 130 on
its own second SIGINT or SIGTERM only where, at that signal, it proves it is the
foreground process group of a pseudo-terminal other than its supervisor's
terminal, as sudo's `use_pty` leaves an interactive invocation. Without that
proof it never escalates, and the supervisor's second signal kills sudo as
before. Unit tests prove each clause. The exit evidence, a real-sudo run on
Fedora 43 and RHEL 9.8 in which a second Ctrl-C ends the command at once and a
single one never does, is D59's closing run's, and the exit skips the child's
deferred cleanup, so the run also confirms the operator's terminal is left sane.

**Outcome, B377:** a fresh destroy proves its host under the shared lock before
its plan is presented and again under the exclusive lock before it resolves,
probes or registers: setup completed on this host, this host is the controller
state's own, and a binding recorded for the context names it. A context with no
recorded binding is admitted once the host is proved, so the unbound
continuation's remedy names a destroy that runs.

**Outcome, B437:** a continuation or a resolution re-proves its input,
automation, closure, setup and host binding before its plan is presented, so no
plan is printed for a continuation that then refuses. An apply over an
incomplete removal names that removal's continuation only where its proof admits
it, and otherwise the exit the proof's refusal names, followed by that refusal.
`status` offers `bootwright setup` in place of every step that runs a block or
registers while setup is incomplete or the receipt holds no execution definition.

**Outcome, B440:** a fresh destroy resolves each unproved block of the apply it
supersedes through the removal's own check, which proves the object is this
context's and can be taken back, so a machine that drifted from its frozen
request is removed with its ownership proved instead of waiting for a hand
restore (D119). The resolution first runs the block's apply observation (D123):
one that proves the operation completed records the block done and captures what
it produced into custody, as a completed apply's finalization does, before any
inverse runs, and anything less falls back to the removal's check. Before a
destroy runs the inverse of a block whose removal deletes the installer's work
area, and only when custody holds no entry for that cluster, the media
capability's read-only observation hands the installer's kept administrator
kubeconfig over, and the engine publishes it in one publication under the entry
a completed capture uses, marked unproved (D124). A failed read, hand-over or
publication records the removal attempt failed before its inverse runs, so the
copy stays in the work area, in custody or in both, whatever the nodes' power
state. `cluster kubeconfig` exports an unproved copy with one `access.unproved`
warning on standard error, and the entry leaves custody as any entry does. The
journeys drive the real agent-installation capability through an adapter double:
a completed installation whose publication failed is captured before its
removal, a part-way one captures nothing, and a completed one with its nodes off
is kept unproved before the work area goes, both when the destroy completes and
when it stops after the removal.

**Outcome, B340:** a bounded run never planned, `machine start`, `stop` and
`restart` and the power read of `machine list --power-status`, refuses a
template delimiter or a key ansible-core reserves before anything is confirmed,
lent or bound, naming the Machine and ending with the exact command to repeat.
The plan's refusal text is unchanged. The tests put the delimiter in
`controller.credentialsRef`, since a Machine's BMC endpoint and placement user
cannot hold one.

**Outcome, B370:** `machine list --power-status` reports one check per placement
host under Checks, running with the role's groups as sub-steps, then `ok` with
"N of M machines read" or its settlement.

**Outcome, B425:** an adapter run that published its preparation and fails or is
canceled before Go delivered an acknowledgement authorizing an effect is
recorded failed, for setup's own run and for a controller-stage client
installation alike; a recovery and an uncertain acknowledgement delivery stay
unknown, and a canceled setup's receipt records its failed action.

**Outcome, B379 (part):** an execution-foundation failure says "Restore the
qualified host execution foundation, then repeat this command", and names
`bootwright apply --stage controller --context <c>` for an apply's controller
stage only; a destroy keeps its own retry. The controller record's uncertain
publication, which an apply reaches while it publishes its client areas and a
context deletion reaches while it drops the context's claims, still names
setup's retry and stays on [B379](#x46--the-safety-and-defect-sweep-of-m1).

**Outcome, B380 (part):** the unknown-outcome remedies of the controller stage
and of the controller clients name the exact apply with every token its frozen
plan consumes. An unobserved block's `status` remedy is the command that
observes it before anything else starts, an unresolved block's diagnostic ends
with the exact repeat, and a removal resolving an apply names the destroy. The
pre-boot refusals, the agent installer's tool lookup and the sites listed on
[B380](#x46--the-safety-and-defect-sweep-of-m1) remain.

**Outcome, B398:** an elevated selection refusal keeps its remedy: the helper's
response carries the remediation when message and remediation are each one
bounded printable line, so the unsafe-marker and unsafe-directory repairs
survive sudo. The helper protocol is never persisted and has no version.

**Outcome, B399:** the `context delete` row names `status` as the inventory of
abandoned objects (D88), and a lost context's plan says its keyring is already
gone with its directory instead of promising to remove it.

**Outcome, B400:** an evidence file the store refuses as unsafe, for its mode,
owner, type, links or size, refuses `context update`, both deletions, `status`,
`apply` and `destroy` naming the context, the store-relative entry and the exit,
which is to remove that entry beneath the state root and run `context delete
--purge --allow-orphans`, or restore the store (D89). `--allow-orphans` does not
waive the refusal, and the deletion never removes an entry it cannot verify as
its own.

**Outcome, B401:** refusals of abandoned-stage collection, the bounded-run
listing and creation and a controller setup-run entry name the entry relative
to the state root. Setup's store backstops at the bundle bound, the retained
resolution bound and the retained dependency source bound refuse with
`controller.conflict`, name the bound and carry a remedy. No bound changed.

**Outcome, B402:** `setup --dry-run` judges the state root as the store opens
it, walking every ancestor without following links, so a root behind a
symbolic-link ancestor is reported not ready with the store's own refusal.

**Outcome, B426:** a failed `media list` row whose entry is a directory or
another non-regular file is removed by the `media delete` command its row names:
the store unlinks it without following, retries an empty directory with
`AT_REMOVEDIR`, and refuses a directory that still holds entries, naming it and
the remedy; nothing inside a directory is removed.

**Outcome, B351:** the input reader opens every ancestor, directory source and
descendant directory with `O_DIRECTORY`, falling back to a path handle only on
`ENOTDIR`, so a browse-mode autofs directory is mounted before a lookup beneath
it. Enumeration reads directory entries through `getdents` and never stats with
this process's credentials; an entry of unknown type is classified by opening it
through the session's opener, so a denial names who was denied, and any other
enumeration error names its errno and a remedy.

**Outcome, B358:** the compiler records which identities failed decoding. Add-on
profile and binding checks that name an undecodable profile, add-on or input
target add nothing, configuration and capability checks run only on a complete
expansion, and a Machine whose provider does not resolve gets no host-key or BMC
credential refusal. Every add-on refusal names a next step.

**Outcome, B360:** a parser-stage YAML error counts its line from 0 and a
scanner error from 1, and the syntax diagnostic adds one to the parser-stage
line, so a directive on line 1 names line 1 and the remedy says "at or below
that line". Scanner errors are unchanged.

**Outcome, B405:** an invalid `metadata.name` refusal names its kind and
document and states the DNS-label grammar to follow.

**Outcome, B423:** every Machine invariant, reference and type refusal requires
a remedy, and the slogan "make the Machine declaration consistent with its
referenced resources" is gone from all 75 sites. An Anaconda Machine with no
network that sets `installAddressRef` is refused once, by the static-install
rule.

**Outcome, B342:** the libvirt network and domain templates escape the bridge
in its XML attribute, so a value holding a quote, angle bracket or ampersand
stays one attribute value; the existing goldens are byte-identical, and a
collection test renders both templates with a hostile name and parses the
result. The request-template test's libvirt bridge case still skips by design,
because bridge admission refuses its value first; its evidence is
`TestAPlannedBridgeHoldingATemplateDelimiterRefusesBeforeRegistration` in the
libvirt substrate package, which refuses a bridge holding a template delimiter
at planning for the provider host and the Machine request before registration.

**Outcome, B420:** admission refuses a libvirt InfraProvider whose name a Machine
realized on a libvirt provider also takes, and selection keeps the refusal
before registration for state that bypassed admission, so the Machine's
recursive destroy can no longer delete the provider's pool directory. The layout
and request shapes are unchanged.

**Outcome, B421:** admission refuses a libvirt provider with more than 20 managed
attachments, naming the bound and `spec.networkAttachments`, and selection keeps
the refusal, in place of the opaque reservation error a 64-key reservation gave.

**Outcome, B422:** `customizations.hostname.source`,
`customizations.storage.rootDevice.source` and `customizations.packages.environment`
leave the install profile, because the Kickstart never read them (D94), so the
closed schema refuses each as an unknown field. A configured or mirror
repository ID is 1 to 239 ASCII letters, digits, `-`, `_`, `.` or `:` and neither
`.` nor `..`, what dnf accepts, at admission and in the renderer's guard;
subscription repository IDs keep the Kickstart-token rule. The sibling
environment template still emits the removed fields; its migration, which the
owner approved on 2026-10-07, is owed in the sibling repository ([B465](#x46--the-safety-and-defect-sweep-of-m1)) before D59's
closing run.

**Outcome, B424:** one reading of a zero-prefix IPv4 destination, any spelling,
serves the gateway and the installation's carried-route check, so a default
route spelled `192.0.2.0/0` is carried as `0.0.0.0/0` is.

**Outcome, B435:** the managed-service and artifact-server roles refuse a content
root that is not in normal form, exactly seven components with none empty, `.` or
`..`, in the frozen-request assert that runs before any effect, for the managed
apply and destroy and the artifact apply and destroy, so no removal can reach
the context's directory. The tests plant a root per entry point, among them a
context of `lab/nested` that only the depth condition refuses, and see no removal
run.

**Outcome, B436:** both destroys read the unit's state after the daemon reload and
reset one that systemd still lists failed, ignoring a unit that is not loaded and
counting the reset as a change, and the unit-state proof reports a definition-less
unit systemd still lists failed as failed, so a removal that leaves it listed is
an unfinished removal the next destroy converges. The lab-rhel README's claim that
a destroy leaves none holds. A service an earlier build destroyed that left its
unit listed failed is not revisited: reset it by hand.

**Review:** ten sub-items in six lanes integrated without a conflict. The first
gate run stopped on one citation, on M1's page, of a test the lanes renamed; a
repoint fixed it. The slice review, by reviewers that did not write the diff,
raised nine findings, six confirmed in scope and three not confirmed: a
scanner-stage YAML error on the first line that B360 leaves unchanged, now
[B456](#x46--the-safety-and-defect-sweep-of-m1); a commit-message wording; and a plan preview that the
state-reconciliation spec deliberately leaves unproved. Of the six, an input-logging
option hidden in a multi-member and then in an argument-carrying `Defaults`
line, and a controller-stage entry refusal that named no stage command, were
fixed over three rounds; the first fix of the second wrapped a destroy's refusal
too, which the check caught and the next round corrected, each with a test that
fails without it. The stdin refusal that never fires while the sudo listing needs
a password got its condition stated in the operator guide and became
[B458](#x46--the-safety-and-defect-sweep-of-m1); the sibling template and B342's skipping case were recorded; and
the loss of the only kubeconfig under D119 led the owner to D123. A review of D123
found that the capture needs the cluster API, which the destroy's own `Stop first`
path powers off, and the owner decided D124. A review of D124 found a log record
that claimed a copy no one had published and spec statements the unproved mark
falsified, both fixed; its check found no gap. In all, twelve findings, nine
confirmed in scope.

**Gates:** at the integration head after the last fix round, `make check-offline
tidy-check modules-check vulncheck docs-check race` passed, and so did the four
`./scripts/ansible-check` suites (units 240 passed, sanity, integration and
lint with no failures or warnings), `./scripts/check-commits` and `git diff
--check`. The review-fix and decision rounds re-ran them on each head. On the squashed commit the same `make` targets passed (all modules
verified, no vulnerabilities found, the race results of unchanged packages
cached from the integration head), and so did the four suites (units 264 and 240
passed across its runs, sanity, integration and lint with no failure or warning
in 314 files), `make docs-check` after the last wording edit,
`./scripts/check-commits` and `git diff --check`.
No real-host run: the real-sudo run of B339, the autofs and root-squash reads of
B351 and the keep task of D124 are unrun.

**Digest effects:** the fourth slice to move them, in X43's window. The automation
digest moves, and every plan digest with it, through B342's two templates, B435
and B436's roles and module utilities and the media role's keep task. No request version or
record version moves; the refusal texts this record names change, and so do the
goldens that show them. The keyring index's produced entries gain an optional
`unproved` member, written only when true, so a store holding none is
byte-identical; an executable built before X46 refuses a store holding an
unproved entry, so a context that holds one is destroyed with the build that
wrote it. **Before a build that contains X46 touches a host, destroy every live
context with the build that applied it, then run `setup` once, keeping the host
under its bundle bound with `--purge-old-bundles`;** one destroy and setup serve
X43, X24, X45 and X46. An input a context stores that holds a removed profile
field, a Machine and a libvirt provider of one name, more than 20 managed
attachments, a repository ID dnf refuses, a refused common name or an unsafe
bridge plans again once `context update` imports corrected input.

**Operator-visible effects:** the refusals this record names carry their context,
entry or exact command. A trust takeover shows and writes the removal of an
unread record, a prompt on a secret no longer blocks other commands, a stdin
secret under a logging sudo policy is refused before it is read, a destroy over
a drifted or unproved installation is removed instead of waiting, and a
completed installation's kubeconfig survives a destroy whose publication failed,
exported with an `access.unproved` warning. `machine list --power-status` shows
progress. Input over automounted and root-squashed mounts names its cause.

**Constraints left behind:** [B339](#x93--the-owner-run-of-2026-10-14) completes on an owner-accepted
ledger row of D59's closing run; B379 and B380 completed in the second pass
below. B342's evidence is the package test above, not the
named case. Under D116 eight follow-ups joined M1 on X46: the secret and
context commands that keep a placeholder ([B453](#x46--the-safety-and-defect-sweep-of-m1)), a stored trust file
pinning one endpoint to two keys ([B454](#x46--the-safety-and-defect-sweep-of-m1)), the deletion walk's and a
linked root's bare refusals ([B455](#x46--the-safety-and-defect-sweep-of-m1)), the first-line YAML scanner
error ([B456](#x46--the-safety-and-defect-sweep-of-m1)), the Machine and Environment refusals still without a step
([B457](#x46--the-safety-and-defect-sweep-of-m1)), the stdin secret under a sudo listing that needs a password
([B458](#x46--the-safety-and-defect-sweep-of-m1)) and a managed service's endpoint count
([B459](#x46--the-safety-and-defect-sweep-of-m1)) and the sibling template's removed fields
([B465](#x46--the-safety-and-defect-sweep-of-m1)). Under D48 five were parked: the resolution record's check
([B460](backlog.md#b460)), the infrastructure roles' shared helpers
([B461](backlog.md#b461)), the keep task's rehearsal ([B462](m3.md#b462)), the
host observations ([B463](backlog.md#b463)) and the wording, tests and dead code
([B464](backlog.md#b464)). Already tracked: the enrollment test double and the
status of a setup receipt with no execution definition are
[B368](backlog.md#b368) and [B375](backlog.md#b375).

**Second pass:** five file-disjoint lanes over the slice's opening commit, each
one commit, cherry-picked onto the task branch without a conflict.

**Outcome, B379:** the controller record's uncertain publication names the
retry of the command that met it: setup keeps its exact retry; an apply that
binds, reserves, reserves, attributes or seals a client area, or retains a
resolution names `bootwright apply --context <name>`; a destroy releasing its
reservations names `bootwright destroy --context <name>`; and a context
deletion dropping its claims names `bootwright context delete --name <name>
--purge`, each once storage is healthy. A refusal raised inside setup's own
transaction, its storage capability included, keeps setup's retry, because
only setup reaches it.

**Outcome, B380:** the pre-boot refusals continue the refusing apply with its
frozen plan's `--authorize` tokens; the agent installer's tool lookup names the
context's stage command; an attempt that cannot read a bound Secret names the
continuation that reopens its bindings, or `status`'s next command, in the
Ansible runner and both installations, through a run request `Repeat` that
reaches no adapter, variable or digest; the fresh destroy's removal summary
carries the removal plan's tokens; and the power evidence remedies name
`machine list --power-status` and the exact power command, adding `--force`
only to a graceful stop or restart.

**Outcome, B453:** the keyring's cleanup refusals name `bootwright secret
encryption init --context <ctx>`, and the retired file-source remedy carries
the context in the secret commands, `context init` and `context update`. The
2026-10-11 review made the remedy name the command to repeat first: from
`context init` or `context update` it says to declare `source: {contextStore:
{}}` in the input, rerun that command with `--input-dir`, then run `secret set`,
and from a secret command on a context whose stored input still holds the
retired source it names `context update --name <ctx> --input-dir <dir>` first.

**Outcome, B454:** `machine trust` alone reads a stored trust file that pins an
endpoint to two keys, and its plan removes, as shown `remove` rows, the records
at that endpoint of Machines that no longer use context trust; the refusal of
every other reader names that repair: `machine trust --machines` with the
Machines in use, so that a Machine whose key changed then refuses as a changed
key with its out-of-band remedy and nothing is written. Only a refused write
names `--replace`, after the operator verifies the new fingerprint out of band;
the 2026-10-11 review removed the earlier remedy that pre-authorized it.

**Outcome, B455:** the deletion walk names the context, the store-relative entry
and why, with the exit to remove the entry and repeat the purge; a state root
behind a link or a non-directory ancestor names that path, its type and the
repair, and a root that is itself a link is refused as a root of another type
with the ancestor's exit: replace it with a directory, such as the directory the
link names or a mount of that storage. An entry on another filesystem is told to
be unmounted and its contents never removed, since a removal could descend into
a mount; an entry whose device differs without the walk crossing a mount is
told to be unmounted if it is a mount point and otherwise removed. The 2026-10-11
review added both exits.

**Outcome, B456:** an unlocated scanner or parser reason reports line 1, and a
control character its line and column computed from the bytes; composer reasons
stay unlocated.

**Outcome, B457:** the fleet-key requirement counts only Machines Bootwright
installs, a reference at a field its object's invariant refuses is retracted,
the repeated remote rescue checksum refusal is removed, and the Environment's own
refusals carry remedies, each in the validate refusals golden.

**Outcome, B458:** an elevated standard-input secret is read only after sudo's
policy listing was read. An interactive invocation validates with `sudo -v`
and reads it again; a JSON or non-terminal one, or a listing still unreadable,
refuses `secret.input` before sudo starts the child. A readable listing that
sets `log_input`, `log_stdin` or `log_ttyin` keeps its refusal, whose text names
the Secret's input, whether typed at the terminal or piped. Commands without a
standard-input secret are unchanged.

**Outcome, B459:** the reproduction held: a wildcard Proxy with 62 endpoint
addresses claimed 65 keys. Admission bounds a managed service at 61 endpoint
addresses and 62 sockets, naming the bound and the field, and selection keeps
the refusal.

**Outcome, B465:** the sibling repository's `environment-ha` chart emits no
removed install-profile field (its commits 3dfba18 and 0e60505 on its local
`main`). On this branch the `example` and `example-lab` renders reproduce byte
for byte, and in-process admission reports only the twelve known
`fusion-data-foundation` references, none with the synthetic descriptor.

**Review:** the coordinator read each lane's diff against its item. Lane B380's
contract changes reached no spec, so its commit states them in the substrates,
contexts, state-reconciliation and CLI specs. No finding blocked. The lanes
were B379 with B458, B380 with B455 and B453 with B454, by other tools on
2026-10-08, beside B456 with B457 and B459 from the earlier session.

That read was not independent, though B380 is lifecycle safety and B458 is
privilege safety (F010). On 2026-10-11 an independent multi-reviewer review of
180b423ac and f6c540fd9, and of the remedy wording of bf9f6f6de, with three
lenses on each finding, found one blocking and several smaller defects, all
fixed on 2026-10-12:

- `log_ttyin` was not detected, so a stdin secret typed at a terminal under
  `Defaults log_ttyin` was read while sudo recorded the keystrokes (F001,
  blocking; B458 and B373), and the remedy's "run it as root, where sudo logs
  nothing" also covered `sudo bootwright` (F002). The commit "fix(privilege):
  refuse stdin secrets under log_ttyin; limit root remedy" (4c4a443a7) fixes both
  and "fix(privilege): the input-logging refusal names input, not only stdin"
  (113c6a88d) corrects the refusal's wording.
- A state root that is itself a link got "nothing repairs it" (F004) and a mount
  point in the deletion walk was told "remove" (F005; B455): "fix(contextfs):
  exits for a linked state root and a mounted entry" (5a405b927), and, after a
  second reviewer found that an entry on another device which is no mount point
  was forbidden every exit, "fix(contextfs): an entry on another device that is
  no mount is removable" (ef02ec8b2).
- The divergent-pin remedy pre-authorized `--replace` (F006; B454) and the
  file-source remedy named a command that cannot work before the input is
  corrected (F008; B453): "fix(trust): stop remedies pre-authorizing replace or
  skipping a rerun" (23d84adab). A secret command on a context whose stored
  input still holds the retired source had the same gap in custody: "fix(secrets):
  a stored retired file source names its re-import first" (5c6672073).
- The sibling chart rendered a BMC `trustBundleRef` that names a Secret it never
  declared, and its commit 3a93e6e declares it (F009; B465).

One fact stays unobserved: that a user- or group-bound `Defaults log_ttyin`
appears among the Defaults `sudo -ll` prints for the invoking user is read from
sudo's source comments, not from a recorded listing, and D59's closing run can
observe it.

**Gates:** at the task head, `make quick` passed on its second run; its first
failed once in `TestARecordWaitingAtTheDrainIsJudgedAsIfReadFirst`, which
passed 20 of 20 runs at this head and at `main`, the shared-CPU flake
[B464](backlog.md#b464) records. `make docs-check`, `make race`, the completion
test, `./scripts/ansible-check --suite units` (264, 240 and 1618 passed) and
`--suite lint`, `./scripts/check-commits` and `git diff --check` passed. `make
check` stopped at `make vulncheck`: ten reachable go1.26.8 standard-library
vulnerabilities, reported identically at `main`, which this slice did not
introduce and [B470](#x67--go-toolchain-vulnerability-gate) records. D139 (2026-10-11) leaves that failure
to B470 alone, so the second pass stays delivered rather than blocked on it;
the 2026-10-12 review fixes ran the full gates on a later head, which
[X48](#x48--bounded-concurrent-execution)'s record gives. The sibling chart passed `helm lint`
on both values files, its schema parsed and `git diff --check`. No real-host run.

**Digest effects:** none. No request, record, plan or automation digest moves;
only refusal texts and admission refusals change, so no context is stranded.

**Constraints left behind:** [B470](#x67--go-toolchain-vulnerability-gate) holds `make vulncheck` alone,
on X67, and under D139 no item waits on it. The 2026-10-11 audit's F030 asked to
put the second pass back on M1 as blocked; D139 settled it in favour of
completion.

### X44 — help, completion, contracts and ratchets

**Owner:** CLI, Architecture and Infrastructure services, with each spec's owner,
each test's owner and Substrate. Made by other tools on branch M1 over `main`
c85e406a6 on 2026-10-08 and 2026-10-09, and reviewed and fixed on 2026-10-11 and
2026-10-12 (D139). **Integrated:** on 2026-10-08, "docs(m1): X31's wording, CI's
race job and the lab-rhel checks" (8afcceee4), "fix(lifecycle): settle the
refusal tables X31 left" (90d33c01a), "test(architecture): a shrink-only ceiling
on each package's comment lines" (ec086b551), "fix(cli): version prints the
embedded automation digest as its bundle" (ee5bf4ed7) and "test: give each X33
mutant that still survived a failing test" (5cd382b9b); on 2026-10-09, "fix(m1):
finish CLI and contract hardening" (6f11e8fc2) and, outside any item, "docs(agents):
route OpenAI and Anthropic models by quality and cost" (46cca1f8c); and, as the
review's fixes on 2026-10-12, "fix(cli): complete files with a space and keep an
exec tail as payload" (2a8da99b1), "test(architecture): gate Not-yet-met
paragraphs, state the ratchet slack" (c4e938829), "fix(cli): root help marks a
group unavailable only if all of it is" (78d71adab), "docs(agents): restore the
owner's Anthropic routing policy" (789e0a926) and, for B272 and B302, "test(lifecycle):
make the lingering-adapter and reaping probes exact" (5ee8176d4, recorded with
[X48](#x48--bounded-concurrent-execution)). **Items:** B95, B300, B224, B272,
B336, B233, B301, B302, B255. **Decisions:** D59, D70, D100, D101, D102, D110,
D115, and, recorded after the work, D126, D139 and D140. **In-tree gates passed,
operator gates awaiting acceptance:** [B276](#x93--the-owner-run-of-2026-10-14) and
[B277](#x93--the-owner-run-of-2026-10-14), whose rows stay on M1 with Delivery
`awaiting operator acceptance`.

**Outcome, B95:** every visible command has complete help with examples, its enum
values are named in the help and in the scalar-enum refusal, the root listing and
each unavailable command say so, and a usage refusal tells an unknown flag from
one that belongs to another command. Goldens hold the root and every available
command's help. The review fixed two defects the goldens had frozen: the
help of an unavailable command ended in a doubled period and cluster access help
repeated its phrase "for independent operator execution" (F015, F041), and a flag
of another command was reported as "not accepted at this position", which now
names only a descendant's flag (F014). The root help marked `render` unavailable
although `render effective` works; a group is now marked only when neither it
nor any command under it is available, as
`TestAGroupWithAnAvailableSubcommandIsNotListedUnavailable` holds, while
`render installer` and `render storage` keep the marker.

**Outcome, B300:** `version` prints `Dependency bundle` as `sha256:` followed by
the embedded automation digest, the identity setup publishes and every
operation records, composed at startup (D100); the unused link variable is gone.
Every catalog payload command accepts `--` and operands with parsing derived
from the catalog, so the parser re-lists none. One writer encodes the JSON
envelope that eleven places encoded, with the goldens unchanged and the raw
secret and kubeconfig streams retained. Sorted names and the SSH host-token
rules have one owner each, the diagnostics sort order has a table test, the
stage enum equals the reconciliation stages, and dead and test-only code is
removed. The terminal probe is deferred, as its wiring comment says, without
losing a non-terminal run's progress heartbeats. Context results carry
guidance. Completion appended no
space after any path-flag candidate, so a completed regular file kept the cursor
glued to it; the private protocol now reports no-space only when at least one
candidate ends in `/` (F011, F040), as
`TestPathCompletionAppendsNoSpaceOnlyAfterADirectory` and the shell harness's
single-file case hold. Under D140, `machine exec` and `cluster exec` stop at the
payload, as `cluster oc` and `cluster kubectl` did: the first operand that is not
a flag starts the remote command and every later token is payload verbatim, so
`machine exec --name m1 kubectl --context prod get pods` no longer retargets the
session and `ls -la` is no longer an unknown flag (F012, F013;
`TestPayloadParsing`, `TestPayloadTailNeverRetargetsTheSession` and
`TestAFlagWrittenAfterThePayloadIsRefusedWithWhereFlagsGo`). A required
Bootwright flag written after the payload is refused with where flags go, never
echoing the arguments.

**Outcome, B224:** the destroy row of the command catalog names its housekeeping
of operation claims that hold nothing; the dependency-safety section says a
continuation of either verb uses its operation's frozen dependencies while a
fresh removal runs under the build and bundle in hand; the architecture spec
names the frozen Python and Ansible execution closure; and the bundle manager's
and install evidence's comments describe the retained setup bundle and the kept
copy of the installer's kubeconfig as they are.

**Outcome, B272:** CI's `race` job ran only on the `schedule` event, which GitHub
starts only from the default branch, `main-v1`, which holds no `check.yml`, so
the job had never run. It now runs on every push and pull request beside `make
check`, and the development guide and the Go reference say so. The first CI run
of it, on the work as the other tools left it, was red: the lingering-adapter
fixture of `TestAnAcknowledgementAfterAFailedExitKeepsThatExit` depended on the
shell running its TERM trap, failed at the 2 second drain under dash and
reproduced 2 times in 10 (F018, F038). The review replaced it with a Go helper
child and found no other trap-dependent shell fixture. `make race` passes
locally; the race job is separate from the `check` job, which stays red at
vulncheck until B470, and the first race run after the owner's next push is
expected green but has not run.

**Outcome, B336:** the clarity contract keeps short rationale and
safety-invariant comments (D101), and `TestProductionCommentLinesOnlyShrink`
holds each production Go package's comment lines, directives aside, to a
per-package ceiling set at 79 packages' counts of the day: a package above its
ceiling fails, one more than a tenth below it fails until the ceiling is lowered
(any shrink below ten lines), and a package with no ceiling or a ceiling naming
no package fails. `TestCommentCeilingsFailWithMoreThanATenthToSpare` drives each
violation. The review corrected the spec, which said narration that leaves a
package cannot return unnoticed: it can return up to that tenth, and the
ceilings bound the module's total (F017). Reducing what narration remains is
B98's, parked.

**Outcome, B233:** the two agent-install checks that admitted input cannot reach
are removed (a method other than agent, which the closed enum refuses, and the
import-certificate virtual-media trust, which only a physical node carries and a
physical node is refused first); the artifact server keeps its `Refusals`, which
its capability's `Unsupported` returns with the shared placement and reservation
rows, and loses the unused helper; the engine's refusal of kinds no capability
claims has its table in the state-reconciliation spec, held by
`TestUnclaimedObjectTableMatchesTheEngine`; and `make docs-check` runs the
capability refusal-table tests and that one.

**Outcome, B301:** the specs record only what is still unmet, and
`TestDocsNotYetMetNamesAnUndeliveredItem`, which `make docs-check` runs, now
enforces that every "Not yet met:" paragraph links a B item that has its
heading and row on a milestone page or the backlog and is not completed (F042;
its matcher test rejects a missing link, a delivered-record target, a wrong
anchor, an unresolved path, a wrong page, a completed row and a row without a
heading). The security spec has a listener-exposure table naming each listener's
clients, transport, authentication, privilege and client restriction; the
operator guide has a network-exposure section with ports by source and firewalld
commands (D110); the examples bind their emulated BMCs to loopback (D115), with
their effective-state goldens and example tests updated; the runbooks import a
copy of the example so a build is not stamped modified; the multidc-platform
README's counts match; and the lab-rhel README gains the checks D59's closing
run follows.

**Outcome, B302:** the packages that consume a selection port declare it in their
own contracts, and the boundary test refuses an exported function-typed port in
a domain root; owned-rule parity tests tie each remaining
copy to its owner, among them the context-name rule, the discovery rule and the
pristine evidence bytes; imports are grouped and unduplicated with `goimports`
pinned in the tools module and `scripts/goimports`; presentation-only request fields and
unused production helpers are removed; and the JSON and plan goldens are
unchanged. `make race` passes locally; as for B272, the first CI race run after
the owner's next push is expected green but has not run.

**Outcome, B255:** X44 re-ran X33's fourteen mutants at its base. Six were
already killed; the eight that survived each have a test in a `mutants_x44` file
of their package that fails with the mutant in place: a bootstrap whose wheels
lack ansible-core or urllib3, the ELF RPATH and RUNPATH search (proved through an
x86-64 shared object built in memory, so the network-tagged test is no longer the
only reach, and shown failing when the search is disabled), a recovered native
transaction whose evidence is not recorded, podman's request swapped for
libvirt's, the keyring's initialization-root predicate, two held files claiming
one part, a receipt listing a proxy bypass twice and a fresh reservation over a
directory that already holds one.

**Outcome, B276 and B277 (in-tree):** the lab-rhel README checks the managed time
service with `chronyd -Q -t 10`, since a managed chrony server takes about four
seconds to select its source and the timeout bounds the whole query, says a
printed offset is success, and lists chrony, bind-utils and qemu-img among the
prerequisites; its runbook reads the running guest's root disk with `qemu-img
info --force-share`, which must answer at the frozen 60 GiB, and runs `status`
after the last destroy. The real-host records are D59's closing run's.

**Review:** the other tools' work carried independent safety and correctness
reviews of its own, which found no blocker. The audit of 2026-10-11, an
independent multi-reviewer review with three lenses on each finding, found for
these items the red CI race job (F018, F038, blocking, above), completion's
no-space after files (F011, F040), the exec tail rejecting `ls -la` contrary to
the spec and the debate over a flag in the tail (F012, F013, now D140), the
misplaced-flag classification (F014), the doubled period (F015, F041), the
overstated ratchet (F017) and the unenforced Not-yet-met rule (F042); the fixes
are named under Integrated. It also found that "docs(agents): route OpenAI and
Anthropic models by quality and cost" had rewritten the agents' routing guidance
away from the owner's policy with no decision, allowing the exceptional tier as
a first line and inheritance of the session model, replacing many cheaper
workers with one strong reviewer by fewer agents, dropping the escalation and
vote rules and two measured lessons, and gating a cheaper model on a comparison
with nowhere to record it
(F036, F037). "docs(agents): restore the owner's Anthropic routing policy"
restores the owner's tier table and workflow rules, keeps the OpenAI mapping as
a separate section marked unqualified on this repository, and keeps the
runtime-model check and the total-cost accounting; the owner's review of that
session decision is pending.

**Gates:** the gate run of 2026-10-12 at 78d71adab, the integrated head before the
test-architecture guard fix [X48](#x48--bounded-concurrent-execution) records,
passed every step of `make check` except `make vulncheck`: the format, module
and tidy checks, `vet`, every Go test, `make completion-test` (the Bash cases;
the Zsh cases need `BOOTWRIGHT_TEST_ZSH` and Zsh is not installed on the host,
though the original commit ran them under Zsh 5.9.2), `make docs-check` and
`make race`. `make vulncheck` reported exactly B470's ten go1.26.8 standard-library
findings, GO-2026-6603, 6604, 6605, 6607, 6608, 6610, 6611, 6612, 6613 and 6617,
which D139 leaves to B470 alone. `./scripts/ansible-check` passed (syntax, lint,
sanity, units on the controller and on the Python 3.9 floor, and the safe
integration suites), and `git diff --check` was clean. The summary and per-step
logs of that run are kept outside Git in the review's scratch area, directory
`gates-followups-78d71adab`. The head's last commit adds a test-architecture
guard only, verified with its own tests and `make fmt-check vet docs-check`.

**Digest and version effects:** none. No request, record, plan or automation
digest moves; `version` now prints the embedded automation digest instead of
`none`, and the usage JSON golden and eight help goldens changed for the review's
wording.

**Operator-visible effects:** help names enum values and marks unavailable
commands; a completed file is followed by a space and a directory is not; the
remote command of `machine exec` and `cluster exec` is payload from its first
operand on, so Bootwright flags go before it; `version` shows the automation
digest; the examples' emulated BMCs listen on loopback, and the operator guide
lists each listener's ports by source; and CI starts the race job on each push.

**Constraints left behind:** [B276](#x93--the-owner-run-of-2026-10-14) and [B277](#x93--the-owner-run-of-2026-10-14) complete
on D59's owner-accepted closing run; the Zsh cases of the completion harness are
unrun on hosts without Zsh; D140 awaited the owner's review until D148
accepted it, and the routing guidance's restoration still awaits it; the narration that remains is [B98](backlog.md#b98)'s,
and the unproved listener-socket release is [B323](backlog.md#b323)'s. Whether
tracked text names the BMC firmware family in B305, B319 and B447 stays the
owner's question (audit F033).

### X51 — native RPM snapshot queries

**Owner:** Controller. Made by another tool on branch M1 on 2026-10-09 as one
commit, reviewed and fixed on 2026-10-11 and 2026-10-12 (D139). **Integrated:**
"fix(controller): query the platform's staged RPM database" (b2164e75d) and, as
the review's fix, "fix(nativelocal): refuse a snapshot that lost its rpm
database" (3a749e9ca). **Items:** B471. **Decisions:** D126, D139.

**Outcome, B471:** native RPM queries read the retained snapshot at the
admitted platform's database directory, `usr/lib/sysimage/rpm` for Fedora and
`var/lib/rpm` for RHEL, where the queries used RHEL's path for both, so Fedora's
foundation qualification and the operator queries read the evidence they staged.
Process isolation, output bounds and signature and digest verification are
unchanged, and an unsupported platform refuses without a live-database
fallback. Host-independent regressions cover Fedora and RHEL foundation
queries, RHEL operator queries and unsupported platforms. No foundation
qualification requirement changes.

**Review:** the audit (F020) found that the claim of a refusal for a missing
snapshot held only in a fake: rpm's SQLite backend retries a read-only open of a
missing file with creation and answers "not installed", so a snapshot that lost
its database read as a package Missing. The query now proves, before rpm runs,
that the snapshot holds a regular, non-empty `rpmdb.sqlite` under the platform's
directory (a link is refused too) and otherwise refuses with
`controller.setup`, "the native database snapshot no longer holds the package
database it was copied with". The test's fake answers as the observed rpm does
and TestRPMQueryReadsThePlatformSnapshot asserts, for a removed, empty, linked
and directory database, that the query refuses and rpm never ran. The
controller spec states the refusal.

**Gates:** as in [X44](#x44--help-completion-contracts-and-ratchets): the gate run
of 2026-10-12 at 78d71adab passed every step of `make check` except `make
vulncheck`, which reports B470's ten findings (D139); `make race`, `make
docs-check` and `./scripts/ansible-check` passed.

**Digest and version effects:** none. **Operator-visible effects:** a Fedora
controller's native queries read its own snapshot; a snapshot that lost its
package database is refused by name. **Constraints left behind:** none.

### X49 — installer image publication permissions

**Owner:** Managed OS. Made by another tool on branch M1 on 2026-10-09 as one
commit, reviewed on 2026-10-11 and 2026-10-12 (D139). **Integrated:** "fix(managed-os):
set installer image permissions before publication" (87cf9b15f). **Items:**
B468. **Decisions:** D126, D139.

**Outcome, B468:** a public installer image inherited the builder process's
umask and only a private image received explicit serving permissions, after
publication. Both staged forms are now set to root:root 0640 before labeling and
the atomic rename, so the selected nginx worker can read them without access
for other accounts and no mode changes after exposure. Token isolation and the
fetch-through-listener proof before media insertion are kept. The role's
public and private filesystem regressions start at 0600 under umask 077
(`test_managedos_install_private.py`), and the worker-permission contract
(`test_served_content_is_readable.py`) covers the staged rename sources.

**Review:** the audit (F021, F032) found that the lists of slices that move the
automation digest left X49 out and that the operator's retirement trigger still
keyed on X46, which `main` already contains. The trigger now keys on a build
that contains X48 ([M1](m1.md) and the milestones page), and X49 is among the
movers.

**Gates:** as in [X44](#x44--help-completion-contracts-and-ratchets): every step
of `make check` except `make vulncheck` (B470's ten findings, D139), `make race`,
`make docs-check` and `./scripts/ansible-check` at 78d71adab, on 2026-10-12.

**Digest and version effects:** the automation digest moves, and every plan
digest with it; no request or record version changes. An apply frozen under an
earlier build cannot continue across it. **Operator-visible effects:** an image
published under a restrictive umask is readable by the serving worker. **Before
a build that contains X48 touches a host,** destroy every live context with the
build that applied it, then run `setup` once with `--purge-old-bundles`; X49
shares that window with X48. **Constraints left behind:** none.

### X50 — direct SSH session material

**Owner:** Machine. Made by another tool on branch M1 on 2026-10-09 as one
commit, reviewed and fixed on 2026-10-11 and 2026-10-12 (D139). **Integrated:**
"fix(machine): keep SSH material readable through client startup" (18ed2d794)
and, as the review's fix, "fix(nativelocal): refuse a snapshot that lost its rpm
database" (3a749e9ca), which also holds the real-client tests. **Items:** B469.
**Decisions:** D126, D139.

**Outcome, B469:** OpenSSH closes inherited descriptors before it reads the
files its arguments select, so material passed as extra files at a path in the
child's own descriptor directory disappeared. The waited launcher now holds
anonymous mode-0600 files for configuration, credentials and pinned host keys
and names their actual descriptors under the launcher's process directory; the
invoking-account opener, the offered-key copy, the environment bounds,
cancellation and cleanup are kept, and host-key observation uses the same
correction. Host-independent regressions close the child's descriptors before
reading all material and prove lifetime and cleanup through cancellation.

**Review:** the audit (F022) found that only a stub client pinned the descriptor
paths. `TestTheRealClientReadsSessionMaterialThroughLauncherDescriptors` and
`TestTheRealClientReadsObservationPolicyThroughALauncherDescriptor` now run the
host's own `ssh -G` (skipped where none is executable) with an empty
environment over the real material and arguments, and prove that the client
opens the held configuration through the launcher's descriptors and resolves the
policy only that file sets; a negative control rewrites the configuration path
to the child's own descriptor directory and expects the client to fail. They
cover only the configuration file's readability, because `-G` exits before it
loads identities.

**Gates:** as in [X44](#x44--help-completion-contracts-and-ratchets): every step of
`make check` except `make vulncheck` (B470's ten findings, D139), `make race`,
`make docs-check` and `./scripts/ansible-check` at 78d71adab, on 2026-10-12.

**Digest and version effects:** none; no request, record, plan, toolchain or
automation digest changes. **Operator-visible effects:** a completed
installation keeps its ownership and evidence, and direct session checks resume
with the fixed build without setup or another installation. **Constraints left
behind:** none.

### X47 — pre-effect SSH key policy

**Owner:** Managed OS, with Machine and Trust. **Items:** B448.

**Outcome:** the installation placement host checks actual bound host and
fleet key types before publication or boot, naming the Secret and excluded
key type. Generated guest-agent keys use the same policy check. An empty
pinned host key names the missing identity proof. This reconciles the existing
implementation with its delivery record; it does not close X47's other items
or accept B346's FIPS controller journey.

**Guard tests:** `TestKeyPolicyRefusalsNameEachBoundSecretAndType` in
`internal/managedos/installation/key_policy_test.go` proves the Go diagnostics.
The collection's `test_ssh_key_policy.py` proves excluded host/fleet types and
generated-key policy without credentials or installation effects;
`test_actual_bound_key_policy_is_checked_before_publication_or_boot` proves
ordering for both identity channels, and
`test_a_missing_pinned_key_names_the_missing_identity_proof` proves the empty
key diagnostic. The owning implementation is
`internal/managedos/installation/key_policy.go`, `ssh_key_policy.py` and the
managed-OS installation role. Existing Go and 44 focused Python tests pass;
no live qualification or owner acceptance is claimed.

### X48 — bounded concurrent execution

**Owner:** State reconciliation, with Managed OS, Substrate, Infrastructure
services, CLI and each lifecycle role's owner. Made by another tool on branch M1
on 2026-10-09 as one commit over the work of X44 and X49 to X51, reviewed on
2026-10-11 and fixed on 2026-10-12 in a fix round of eighteen commits, eleven
lane commits, two review repairs kept and five follow-ups (D139). **Integrated:** "fix(lifecycle):
run independent provisioning blocks concurrently" (760802c6a) and the review's
fixes: "test(lifecycle): make the lingering-adapter and reaping probes exact"
(5ee8176d4), "fix(managedos): name tree refusals and take back empty work
directories" (b3776f349), "test(ansible): hold the no-fixed-scratch rule as a
function with self-tests" (3611fdabf), "test(lifecycle): qualify the composed
invocation scope with siblings" (ddfaa90a3), "fix(ansible): the no-fixed-scratch
rule also names /var/run" (5ceb86047), "test(architecture): production never uses
the adapter runner test hook" (a8b10f4f7) and "fix(architecture): the test-hook
guard refuses a dot-import of the runner" (dfb627dbf); the fix round also holds
the commits recorded with [X44](#x44--help-completion-contracts-and-ratchets),
[X46](#x46--the-safety-and-defect-sweep-of-m1), [X50](#x50--direct-ssh-session-material)
and [X51](#x51--native-rpm-snapshot-queries). **Items:** B303, B304, B467.
**Decisions:** D125, D127, D139. **Operator gate:** B466 initially awaited
D125's three-node closing run; [X98](#x98--m1-owner-acceptance) records its
acceptance and completion.

**Outcome, B303:** every lifecycle role receives the runner's private scratch
path as execution material, outside its frozen request and digest, proves its
own root-only child before writing and cleans only that child, on a remote
placement too; the new `lifecycle_scratch` module owns it. Both libvirt roles no
longer create and remove the fixed host directory they shared on replay. Durable
machine work and libvirt provider media occupy disjoint `machines` and
`providers` subtrees beneath the context's directory, so a provider and a
Machine may share a name and the former name-collision refusal is retired
(D127); a destroy removes the provider's, `providers` and context directories
only once empty, never recursively. A structural rule holds the no-fixed-scratch
promise across every non-controller role: no task, default, variable or template
names a literal path under `/tmp`, `/var/tmp`, `/run` or `/var/run`, apart from
two allowances each with its reason (`/run/libvirt`, the socket volume of a
Quadlet unit, and `/run/nginx.pid`), and the rule has pass and reject self-tests.
The review widened the earlier rule, which matched only paths named
`bootwright*` (F045), and a second reviewer then found that `/var/run`, the
alias of `/run` on every systemd host, escaped it.

**Outcome, B304:** the lifecycle opens a private invocation scope at its
execution boundary and closes it after every admitted adapter has joined. Runner
copies share its synchronized live-job registry, and an adapter's held job is
exempt from the running-adapter refusal only while that exact job's original
lock and holder files belong to the live scope; matching context, operation or
attribution bytes alone grant nothing, and another invocation's job, an
unattributed held job and a descendant that holds a job after its owner
released it still refuse. The runner's scope tests (among them
`TestAnInvocationsCopiedRunnersProceedBesideTheirOwnJobs`,
`TestAnEndedOwnedJobStillRefusesWhileItsDescendantHoldsIt` and
`TestOwnedJobsRequireTheirOriginalFilesAndReadableAttribution`) and
`TestInvocationScopeClosesOnlyAfterEveryCanceledAdapterSettles` prove it.

**Outcome, B467:** the two full-suite failures of
`TestACanceledControllerRunNeverAcknowledges` were diagnosed in the review, not
corrected in production. The original test run verbatim as a probe, capturing the
process state where its existence check failed, failed 34 of 1500 and 41 of
1500 iterations while the whole internal suite ran beside it. Every failing
descendant had started in that iteration, so no process identity was reused; its
parent was the user's systemd instance, the adopter of the orphaned `sleep`; its
state was running with the group SIGKILL pending in 43 cases, a zombie awaiting
the adopter's reap in 24, running and then zombie in 5, and gone between check
and read in 3, and every one disappeared within 35 ms. The cause is reaping
latency after the group SIGKILL, the death plus the adopter's reap, and not a
live descendant or a lasting unreaped zombie; no production defect was found.
The current test read a reaped task's record as the process vanished
and got ESRCH, which its loop took for a probe failure, once in 1500; `fixtureGone`
now accepts ENOENT or ESRCH, `TestAFixtureProcessReapedDuringItsReadIsGone`
holds it, and the test passed 1500 of 1500 under the same load. The same lane
replaced the dash-dependent lingering-adapter fixture of
`TestAnAcknowledgementAfterAFailedExitKeepsThatExit` (F018, F038, recorded with
[X44](#x44--help-completion-contracts-and-ratchets)'s B272); dash is not
installed on the host, so the old fixture's failure was not reproduced there,
and `-race -count=50 -cpu 1,2` passed 100 runs of the new one.

**Outcome, B466 (in-tree):** production uses the engine's bound of eight running
blocks, with no flag or desired-state field, and a hosted package tree is one
durable block per context and install profile in the `machines` stage, realizing
`MachineInstallProfile` under `os-package-tree-v1` with evidence
`os-package-tree-evidence-v1`. Each installation requires it and holds no
exclusive tree claim while it builds images, boots or waits; removal follows the
inverse order, consumers first and the tree before its artifact server. The
[Managed OS](../managed-os.md#shared-package-tree-publication) spec owns the
partial-effect, replay and old-plan contracts, and the
[three-node fixture](../../examples/machines/lab-rhel-3-nodes/README.md) (synthetic
documentation addresses, one shared install profile) exercises it.
[B466's section](#b466) states each step and the test that proves it. The
review closed three gaps: no test ran the production options against the real
runner, so wiring the invocation scope to nil left every test of `cmd/bootwright`
green while production would have refused each sibling after the first
(F023, F043); `TestTheComposedInvocationScopeAdmitsSiblingRunsOfItsRunner`
now holds it and fails when the scope is unwired or replaced by a plain
cancelable context. A DVD changed after planning, or a tree another image
published at the path, failed the tree block through a quiet assert and read as
"adapter operation did not complete", and every installation still hashed the
roughly 13 GB DVD it no longer extracts (F024, F046): the tree protocol now
admits the refused phase with `media-changed-tree` and `tree-source-drift`, the
tree role names them before its asserts, `TestATreeApplyNamesItsRefusalsWithTheirRemedy`
holds the remedies, and the installation role no longer requires the tree DVD.
A destroy left the empty per-Machine and per-context work directories that apply
created, so the operator gate's "no machine work remains" could not pass (F025,
F028): the destroy now takes them back with a non-recursive `rmdir`, never the
shared work root, and a sibling apply that finds the context directory gone
re-creates it. The tree role's asserts were never evaluated by its harness
(F026): the harness now evaluates them against a fixture carrying the validated
request shape and the real source file. `ansiblerunner.Isolated`, the constructor
the composition test needs, is exported from a non-test file, so an architecture
test (`TestProductionNeverReferencesTheAdapterRunnerTestHook`, with
`TestTestHookViolationsFindEveryReference` as its self-test) fails on any
production reference to it, and, after an independent review planted a
dot-import of the runner that passed the suite, on any dot-import of the runner.

**Review:** the other tool's work carried independent safety reviews of the
runner, the roles, the publication and the owning contracts. The audit of
2026-10-11 raised the findings above for X48, and the fixes ran as lanes
reviewed again by a reviewer that had not written them, which found a sibling
race in the empty-directory removal (an `rmdir` landing between a parallel
apply's existence check and its creation can only fail that attempt with a
retryable ENOENT, and the destroy tolerates a directory a sibling took) and the
`/var/run` hole above, each repaired, the first folded into its lane commit and
the second kept as its own. The reviewer approved every lane, the integration and
the follow-ups after those fixes. The shell-fixture audit of the follow-up found
no other fixture that depends on a shell's trap.

**Gates:** the gate run of 2026-10-12 at 78d71adab, the integrated head before
dfb627dbf, which adds only the dot-import guard and was verified with its own
tests and `make fmt-check vet docs-check`; at 78d71adab every step of
`make check` passed except
`make vulncheck`, which reported exactly B470's ten go1.26.8 standard-library
findings, GO-2026-6603, 6604, 6605, 6607, 6608, 6610, 6611, 6612, 6613 and 6617
(D139), and so did `make race` (115 s), `make docs-check`, `make completion-test`
and `./scripts/ansible-check` (syntax, lint, sanity, units on the controller
and the Python 3.9 floor, and the safe integration suites; the collection's unit
suite passed 1713 tests). The summary and per-step logs are kept outside Git,
directory `gates-followups-78d71adab`.

**Digest and version effects:** X48 moves request versions and the automation digest. The installation
request moves from `os-install-anaconda-v7` to `os-install-anaconda-v8`; the
libvirt host and Machine requests to `substrate-host-libvirt-v4` and
`machine-libvirt-v4`, whose frozen paths changed; the tree request and evidence
are new at `os-package-tree-v1` and `os-package-tree-evidence-v1`; and the
automation digest and every plan digest move, as they move again with the
review's tree-role and destroy edits, which add a refused phase to the tree
protocol and move no request or evidence version. **Before a build that contains
X48 touches a host,** destroy every live context with the build that applied
it, then run `setup` once, keeping the host under its bundle bound with
`--purge-old-bundles`; one destroy and setup serve every window since X43.

**Operator-visible effects:** independent managed services and Machines are
admitted together up to eight blocks and a fast chain advances while unrelated
work runs; the package tree is published once, ahead of the installations that
share it; a changed DVD or a foreign tree is refused by name with its remedy; a
destroy leaves no empty per-Machine or per-context work directory; a libvirt
provider and a Machine may share a name; and the three-node example shows the
shape.

**Constraints left behind:** B466's operator gate completed under
[X98](#x98--m1-owner-acceptance). The full lifecycle
composition over that fixture with the real runner is parked as
[B495](backlog.md#b495). The installation role's tooling list still names `xorriso`
beside a comment written before the tree moved to its own block, which no item
tracks. The structural scratch rule reads text and traces no variable.
No audit of interleaved native effects was recorded; the closing run is the
stated gate, and a conflict it shows needs a narrow resource boundary, never a
host-wide claim.

### X69 — continuation under another build

**Owner:** State reconciliation and CLI. An out-of-sequence enabling slice
implemented on the M2 branch at the owner's request (D142), as one commit.
**Items:** B504. **Decisions:** D142. **Integrated:** "feat(lifecycle):
continue under another build that freezes the same blocks".

**Outcome, B504:** another build continues an incomplete operation when, and
only when, it freezes the same blocks. A continuation that was refused because
the embedded automation differed now proves its blocks again. An apply plans the
operation's frozen input again and proceeds when every block matches on every
member the plan froze except the words it is shown in: its description, impacts
and the groups' descriptions. A destroy derives the removal again from the
operation's own frozen plan, which copies each block's request, implementation,
content digest, stage and dependencies, so it compares what the removal decides
(the authorizations it consumes and its groups) and whether every block's
capability resolves and reads its frozen request, as a fresh removal does. A
build counts as another build when its automation digest differs, when its
version or commit differs from the operation's record, or when its own stamp
says `Source: modified`, because a dirty build at the same commit is
indistinguishable from the one that registered the operation. The proof is taken
with the decision under the shared lock, since planning reads the media store
under that store's own shared lock, and the exclusive re-proof reuses it only
after finding every record unchanged; its zero value refuses. A refusal names
the first block that would be frozen differently, or why the operation could not
be planned again, and offers the removal that supersedes it where one may or the
registering build. The Python and Ansible closure check is unchanged, the record
keeps naming the build that registered the operation, and no record or request
version moved. `status` offers the continuation under the same proof. A
qualification still holds its original apply to the automation it registered
under.

**Guard tests:**
`TestAnotherBuildContinuesAnOperationWhoseBlocksItFreezesTheSame`,
`TestAnotherBuildRefusesToContinueABlockItWouldFreezeDifferently`,
`TestStatusOffersAnotherBuildTheContinuationItsBlocksAdmit`,
`TestDifferingBlockIgnoresOnlyPresentationWords` and
`TestAContinuationWithNoSameBlocksProofRefusesUnderAnotherBuild` in
`internal/reconciliation/lifecycle`; the refused-continuation golden and rows,
`TestARecordWithOnlyACommitNamesDevelAndTheCommit`,
TestAnEarlierVersionRecordNamesNoEarlierBuildOnceSetupKeepsRuns (X88 removed it with the version 1 operation record),
`TestContinuationRefusesDriftedInputExecutableOrHost` and
`TestARepairThatMovesABlockLeavesAFailedApplyDestroyable`, changed to move a
block with the automation; and
`TestAnOperationRecordsTheBuildTheVersionCommandReports` for the modified stamp.

**Constraints left behind:** an automation change that alters what an effect is
must move its capability's content digest; the reviewer checks that on every
`ansible/` change, and removal proves ownership and refuses a foreign object as
the backstop. The proof compares blocks only: a continuation keeps the host
reservations and the Secret binding its registration took. A destroy's proof
cannot compare content digests, because its removal copies them from the frozen
plan. What still needs the registering build: a frozen request version this
build does not read (`reconciliation.ThawVersion`), a frozen plan this build
cannot rebuild (`operationstore.ReadPlan`), and any store or record format move.
A completed apply stays settled under a fixed build, so re-running a fixed step
of it is still `destroy` then `apply`. M2's qualification engine binds its
journal to one build and is not converged here ([B498](backlog.md#retired)). Since X69,
an apply that another build continued keeps the automation it registered under,
so qualification over it refuses under the build that continued it; qualifying
needs an apply one build completed.

### X70 — Ceph through the ordinary staged apply

**Owner:** Storage and State reconciliation, with CLI, Environment, Secrets,
Workspace, Substrate and Architecture. A product slice of M2b, integrated on
the M2 branch as one commit at the owner's request (D143), which waived M1, X52
and X53 for its implementation, pending the owner's review.
**Items:** B513, B512, B518, B499, B517, B519, B515, B514, B516.
**Decisions:** D143, with D134, D135, D136, D138 and D142. **Integrated:**
"feat(storage): provision IBM Ceph through the ordinary staged apply".

**Outcome, B513:** `bootwright` is the only executable. The
`bootwright-ceph-qualification` entry point, its build tag, and its lifecycle
journal, admission, discovery, live, disposal and retirement engine are
deleted, with the operation-store records and goldens, the area-scoped secret
custody, the keyring reservation API, the contexts update fence and the
fixture ports of the Ceph capability. The fixture hooks leave the ordinary
Ansible modules; the fixture modules, roles, playbooks, action plugins and tests
are deleted, while the inherited crypto-policy proof the package transaction
needs stays as `ceph_crypto_policy.py`, now unconditional. The example's helper
scripts, the Makefile target, the vet tag and the `examples-test` target go
with them, and the three architecture allow-list edges the fixture needed
(B501's outcome) are removed. The operation index accepts only version 1: a
version 2 index, or one carrying `qualification`, refuses as unreadable
evidence with a remedy naming the build that prepared it, so such a context is
retired with that build or abandoned with `context delete --purge
--allow-orphans`; a damaged index takes the same path. A done apply still warns
on changed input, and the next apply refuses it.

**Outcome, B512:** the empty qualification registry, its two call sites, its
remedy and the duplicate Python gate are gone. `Capability.Unsupported` admits
one reviewed release per build (IBM Storage Ceph 9.9.1.2, packages
20.2.1-405.el9cp, cephadm-ansible 5.0.3-1, image
`cp.icr.io/cp/ibm-ceph/ceph-9-rhel9` at tag `v9.9.1-25074` or a `sha256:`
digest, transport `verified-tunnel`) and refuses anything else before
registration, credential reads or effects, naming the pinned values. A
controller-local block `ceph-image-<cluster>` (`storage-ceph-image-v1`)
resolves the tag through the Entitlement's registry Secret, manifest-only, to
the single linux/amd64 child digest of its image index; the cluster, pool and
client blocks depend on it and read the frozen digest from its evidence, so a
continuation never resolves again and an authored digest unequal to the
resolution refuses. A failed resolution keeps its adapter output in the
attempt. The core request moved to v4 and the content digest changed; on the
guest, `supported_transport` requires the tunnel, the pinned image and Ceph
20.2.1.

**Outcome, B518:** StorageCluster planning emits one
`ceph-host-<cluster>-<machine>` block per host (`storage-ceph-host-v1`, stage
clusters, exclusive per Machine), so hosts prepare concurrently and the cluster
block waits for all of them. Each installs the signed packages with the
inherited crypto-policy proof, pulls the frozen digest with a protected
authfile and a signature policy limited to the pinned repository (at most three
attempts of 1200 s, counted only once inspection proves the digest and
linux/amd64), runs a contained probe that yields the ceph UID and GID, crypto
policy, SELinux label and cephadm bytes equal to the authenticated RPM payload,
and probes the native SSH route with synthetic keys on loopback. The cluster
worker re-proves that runtime profile for the tunnel's manager identity, and a
`cache` phase publishes the exact cephadm bytes on every host before enrollment.
The host block fits 8365 s; the cluster block dropped its per-host package
term. Host-block removal makes no effect: packages and the image are retained
shared host state.

**Outcome, B499:** each host block derives every selected device's native ID
through the image's SDK from udev properties bound to the proved path, device
number, serial, size and nonrotational presentation (the vendor_model_serial
form for a SCSI SSD, the serial form for a virtio disk) and writes
`device-identities.json`; the OSD checks read that record instead of the bare
serial, and a missing or contradicting record refuses. Pool creation writes an
attempt record before `osd pool create`; a retry adopts the pool only from that
record and exactly one same-name pool with the frozen parameters, and refuses
`pool-identity` otherwise.

**Outcome, B517:** `cluster rsh` and `cluster exec` are available for a managed
Ceph StorageCluster. `--node` selects by exact node name, then FQDN, and
defaults to the first node in canonical order; a duplicate FQDN refuses. The
node's Machine opens the same session as `machine rsh` and `machine exec`,
with desired-state identity, the host-key proof, argv verbatim (D140) and the
client's exit status. Every refusal before the session (`access.target`,
`cluster.not-applicable` for external storage, `access.unavailable` for a
ContainerCluster) exits 255 and reads no credential bytes.

**Outcome, B519:** `cluster list` and `cluster info` are available for every
selected ContainerCluster and StorageCluster, in canonical order, reading only
validated desired state, durable block evidence, Machine host-key evidence and
custody presence, with no host contact. A managed Ceph entry reports
management, release, FSID, the frozen image reference, digest and platform,
monitors mapped to declared nodes, the admin node, the exact `cluster exec`
ceph prefix, and `ready` or `unavailable` with the `apply --stage clusters`
remedy. `--secrets` settles applicability before any custody read and refuses
`cluster.not-applicable` for a ContainerCluster or external storage.

**Outcome, B515:** a managed Ceph cluster whose hosts are all
Bootwright-owned virtual Machines removed by the same destroy is retired with
them. The engine derives the retired set from the frozen removal plan through
the `HostBound` and `HostRemoval` ports, lists the hosts under Stop first, and
flags the Ceph block's probe, destroy and removal observation, which then
answer from durable retirement evidence {action, cluster, fsid, retiredWith}
with no host-contacting call, so continuation never contacts stopped hosts.
While any host survives, the cluster keeps refusing with the B491 reason and
prints the capability's own remedy. Each Machine inverse still re-proves its
target stopped before deleting its domain and disks.

**Outcome, B514:** after readiness the bootstrap host enables the dashboard
module with TLS on 8443, a self-signed certificate only when none is stored,
one attempt-marked MGR redeploy (after which the tunnel's mount and unit
observation is re-proved), and an `admin` user whose 32-character password
passes a local copy of the 9.9.1 policy, travels only on stdin, and enters
produced custody as the cluster block's `dashboard` output. A lost response
recaptures the retained password and never regenerates; a user without this
operation's attempt marker refuses `dashboard-identity`; a hash mismatch
refuses `dashboard-drift` and is never reset; a missing verifier refuses
`dashboard-verify`. A ready observe recaptures through the read-only
`dashboard_read`. Evidence moved to `ceph-core-evidence-v2` with the dashboard
URL, certificate SHA-256, reachability and monitor addresses; `cluster info`
prints them, `unproved` with a reason when unreachable, and `--secrets --name`
reveals the password, warning on an unproved copy.

**Outcome, B516:** the
[three-node example](../../examples/storage-clusters/libvirt-3-nodes-ceph/README.md) pins
`image.version: v9.9.1-25074`, and its README is the staged-apply procedure
(context, registry Secret, the four stages, validation through `cluster info`'s
ceph prefix, the RBD client-data test, the dashboard login, the changed-intent
check, a one-node outage, `machine stop`, destroy and purge), every command
line checked against the catalog and host names checked through `machine exec
... getent hosts`. The specs dropped their "Not yet met" lines for these items,
and the knowledge pages record podman's pull-local `--signature-policy`
position and how inspection reads evidence without the frozen request.

**Guard tests:** `TestOneExecutable` (`test/architecture`);
`TestAnIndexTheRetiredQualificationFixtureWroteRefusesWithItsExit`,
`TestAContextHoldingTheRetiredFixturesIndexRefusesAndIsDeletedOnlyAsUnreadable`,
`TestADamagedOperationIndexStillLetsTheAcknowledgedDeletionRemoveTheContext`,
`TestEvidenceTheStoreProvesUnreadableRefusesWithItsOwnExit` and
`TestAnUpdateOverACompletedApplyWarnsAndTheNextApplyRefuses` for the retired
index and the done-apply update; in `internal/storage/ceph`,
`TestUnsupportedReleaseOrTransportRefusesBeforeRegistration`,
`TestPlanEmitsTheImageBlockEveryCephBlockDependsOn`,
`TestPrepareFillsTheImageFromTheOriginalResolutionWithoutResolvingAgain`,
`TestPrepareRefusesMissingAmbiguousOrForeignResolution`,
`TestAuthoredDigestMustEqualTheResolvedChildManifest`,
`TestImageResolutionRunsOnTheControllerWithOnlyTheRegistryLoan`,
`TestImageApplyRefusesUnprovedResolution`,
`TestAFailedResolutionKeepsItsAdapterOutputInTheAttempt`,
`TestPlanGoldenCarriesHostBlocksTheirDependenciesAndExclusivity`,
`TestDeadlinesFitTheRunCeilingForThreeAndTheLargestAdmittedCluster`,
`TestHostApplyLendsOnlyItsHostsMaterialAndTheRegistryAsAProtectedFile`,
`TestHostApplyRefusesWithoutItsDependenciesEvidence`,
`TestHostReleaseMakesNoEffectAndItsRemovalContactsNothing`,
`TestQuiescenceDerivesFromRetirementWithEveryHostOnly`,
`TestRetiredDestroyAndItsObservationRecordRetirementWithoutTheRunner`,
`TestRetirementEvidenceIsAcceptedOnlyForARetiredDestroyOfItsOwnScope`,
`TestEvidenceV2BoundsTheDashboardAndMonitorAddresses`,
`TestProducedDashboardPasswordIsBoundedToTheGeneratorsShape` and
`TestInspectReportsAnUnreachableOrUnservedDashboardUnproved`; the
host-preparation and device-identity tests in `test_ceph_host_runtime.py`,
`test_ceph_host_route_probe.py` and `test_ceph_host_execution.py`, the OSD
identity and pool-attempt tests in `test_ceph_core.py`, and the dashboard tests
in `test_ceph_dashboard.py`; the cluster session journeys
`TestClusterSessionsReachTheSelectedCephNode`,
`TestClusterSessionRefusalsExitTwoFiftyFiveBeforeAnyCredential`,
`TestClusterSessionsOpenTheSelectedNodesMachine` and
`TestClusterSessionRefusalsOpenNothing`; the inspection tests
`TestListClustersReportsEverySelectedClusterInCanonicalOrder`,
`TestClusterInfoReportsEachKindFromDesiredStateAndEvidence`,
`TestMissingEvidenceReportsUnavailableWithTheApplyRemedy`,
`TestSecretsRefusalsSettleTheTargetBeforeAnyEvidenceRead`,
`TestSecretsRevealTheCustodiedDashboardPasswordAfterTheTargetSettles` and
`TestClusterListAndInfoReadOnlyLocalStateForAnExplicitOrCurrentContext`, with
the `cli-cluster-list` and `cli-cluster-info` goldens in `internal/cli/testdata`;
the retirement tests `TestHostBoundEffectRetiresWithItsHostsWhichStopFirst`,
`TestHostBoundEffectStaysOnlineWhileAnyHostSurvives`,
`TestRetiredRemovalContinuesAfterAnUnknownInverseBeforeItsHost`,
`TestRetiredRemovalFlagsOnlyItsOwnDestroyAndRemovalObservation`,
`TestACephClusterRetiresWithEveryHostItRunsOnWithoutContactingThem`,
`TestACephClusterWhoseHostsSurviveNamesItsOwnRemedy` and
`TestAGuestRemovalDestroysItsHost`.

**Constraints left behind:** each build supports one reviewed, code-pinned
release, and the digest is frozen by the image block, not code-pinned, until
B472's lock adds it. The runtime profile and host preparation run on the apply
path. Their operator gate was unrun at delivery; B476's accepted run under
[X94](#x94--post-destroy-cluster-inspection) subsequently exercised them through
the ordinary staged apply, data and outage/recovery checks, dashboard checks,
retirement with all owned virtual hosts and purge. M2b's remaining
[core/prerequisite and B80 gates](m2.md#m2b) stay open; that accepted run is not
blanket qualification of every native failure or credential boundary.
The package decision journal and the operation index format moved,
so a context from an earlier build is destroyed with its applying build or
abandoned with `--purge --allow-orphans`. Removal while hosts survive stays
[B491](m2.md#b491). The dashboard keeps Ceph's self-signed certificate and
Bootwright never rotates it. Cluster sessions exist only for managed Ceph;
ContainerCluster sessions stay [B101](backlog.md#b101). D143's session clauses
await the owner's review, and `make vulncheck` stays B470. B499 has no
mutation checks, because the repository has no mutation-test pattern.
`ProvedHost` is exported with no production caller: the plan's dependencies
order the host blocks, and the guest re-proof fails closed. Inspection readiness
derives from the current operation's evidence, so a live cluster reads
`unavailable` during a later apply until its clusters stage runs again. The
dashboard's TLS proof connects to the active MGR's declared address, and a lost
redeploy response schedules the redeploy again. The Ceph modules reported a
refusal only as a failure reason, never a named `refused` record, so their
named remedies (a dashboard refusal on observe included, which fails the whole
cluster observe) reached the operator as a generic diagnostic until
[X72](#x72--ceph-refusals-reach-the-operator) delivered B520. The cluster block's call budget admits at most three hosts
under `lifecycle.MaxDeadline`, as before X70 ([B79](m2.md#b79)).

### X71 — concurrent retries

**Owner:** State reconciliation, with CLI. An out-of-sequence defect slice of
M2, integrated on the M2 branch as one commit at the owner's request (D144).
**Items:** B523, new on 2026-10-13 from the owner's M2b Ceph run, Kind
`defect`. **Decisions:** D144. **Integrated:** "fix(lifecycle): retry
independent failed blocks concurrently".

**Outcome, B523:** failed blocks are retried concurrently, as first attempts
run. The live M2b run of 2026-10-13 interrupted `apply --stage clusters` while
the three independent `ceph-host-ceph-lab-rhel-01`, `-02` and `-03` blocks ran
together, leaving all three `failed`; the continuation previewed only rhel-01
as `[retry]` (`Starts 1 of 20 blocks, 3 deferred`) and retried the three one
after another, although they share no dependency and each holds only its own
`ceph-host/<machine>` resource. The cause was the contract that a retry runs
alone, which dependencies already made unnecessary, since a dependent never
starts before its dependency is `done`. Admission now takes every failed block
the selection admits that the invocation has not worked, beside every
startable block, each starting once the bound and its exclusive resources
admit it; a dependent of a failed block waits for that block's retry. Once a
block the invocation worked fails, no further first attempt starts, while the
failed blocks still waiting are retried. Unproved effects are still resolved
first with nothing beside them, a failed block outside the selection is never
retried, and a selection that admits no failed block still refuses
`lifecycle.stage` before any effect; that rule holds only until the invocation
works a block, so later admission never depends on which block completes
first. A stage preview marks every failed block
it would retry `retry` and every ready step beside them `start`, and counts
them all as started. The operation is still recorded `running` once, before
any block starts, and takes the state its blocks give it at the end, so no
record or request version moved.

**Guard tests:** `TestIndependentFailedBlocksAreRetriedTogether`,
`TestAFailedBlockIsRetriedBeforeItsDependentsAndOnlyOncePerInvocation`,
`TestFailedBlocksNamingOneResourceAreRetriedOneAtATime`,
`TestAnUnprovedEffectIsResolvedBeforeAnyRetry`,
`TestAdmissionAfterARetryDoesNotDependOnCompletionOrder`,
`TestAFailedBlockOutsideTheSelectionIsNeverRetried` and
`TestAStagePreviewMarksWhatTheNextApplyAdmits` in
`internal/reconciliation/lifecycle`, and
`TestPlanResultMarksEveryBlockTheNextApplyRetries` and
`TestPlanResultMarksTheBlockTheNextApplyResolvesFirst` in `internal/cli`.

**Constraints left behind:** a failed block whose retry fails again still lets
the failed blocks waiting on a shared resource be retried in the same
invocation, as before; only first attempts stop. The `lifecycle.stage`
refusal still names only the first failed block's stage. The live
confirmation is the next continuation of [B476](delivered.md#x94--post-destroy-cluster-inspection)'s run.

### X72 — Ceph refusals reach the operator

**Owner:** Storage, with State reconciliation. An out-of-sequence defect slice
of M2, integrated on the M2 branch as one commit at the owner's request of
2026-10-13 to fix the live failure of B476's run. **Items:** B520.
**Integrated:** "fix(ceph): name every host and cluster refusal to the
operator".

**Outcome, B520:** a refused Ceph host or cluster operation names its reason.
The live M2b run of 2026-10-13 failed `ceph-host-ceph-lab-rhel-01` after 6m44s
with only the censored result of the `no_log` module task, so neither the
operator nor a developer could tell which reason the module raised. Each entry
point of `storage_ceph_host` and `storage_ceph_core` now keeps the module's
failure, publishes its reason through `ceph_cluster_protocol` as a `refused`
record and then fails; the module task keeps `no_log`, and the record carries
only a closed token the protocol checks against the union of the two blocks'
reasons and a detail of at most 160 letters, digits and `_.:@-`. A native
worker's refusal is named by the token its endpoint raised when the block names
it, any other refusal is `unknown` or `remote-refusal`, an exception is
`adapter-exception` with its type and code site, and a module that ends without
a reason is `adapter-exception` with `module-failure`. The package worker,
whose every failure used to reach the controller as `transport`, now returns
its closed `packages` or `package-*` token with the site the shipped helpers
attest, which the controller accepts only from that worker's closed set; a
failure of the package resolution, signature or transaction helpers is named
`package-resolution`, `package-signature` or `package-transaction`, and each
package token has a host entry naming what the operator checks, such as
`Entitlement.spec.license.accept` for `package-license`. `ceph_core.py` holds the
tokens each block may publish, equal to the block's `hostRefusals` or
`adapterRefusals` in `internal/storage/ceph`, so the runner reports that
entry's diagnostic and remedy with the detail after its message. A named
refusal ends the attempt with its entry's code, so every `lifecycle.unknown`
entry now leaves the block's attempt outcome unknown, where the censored
failure before was a failed exit: the next apply observes that block first and
a host block resolves through `hostEffect`. Only the `lifecycle.unsupported`
entries (`unsupported-release`, `package-license`, `package-platform`,
`dashboard-verify` and `client-quiescence-unqualified`) still fail it. A
refusal inside the package worker outside its closed set is published as
`packages` with the original token in the detail. The owning
contract is in [storage](../storage.md#bootstrap-identity-and-continuation).

**Guard tests:** `TestAHostRefusalReachesTheOperatorWithItsRemedy`,
`TestAPackageRefusalReachesTheOperatorWithWhatToCheck`,
`TestANamedHostRefusalsCodeDecidesTheAttemptOutcome`,
`TestAClusterRefusalReachesTheOperatorWithItsRemedy` and
`TestEachCephBlockNamesExactlyTheRefusalsItsModuleMayPublish` in
`internal/storage/ceph`,
`TestAnUnnamedRefusalTokenReachesTheOperatorInTheUnknownDetail` in
`internal/reconciliation/ansiblerunner`, and the collection's
`tests/unit/test_ceph_refusal_record.py`, which runs each module over a
refusal, renders each entry point's naming over what it registered and hands
it to the protocol plugin, and the package cases of
`tests/unit/plugins/module_utils/test_ceph_host_execution.py`, which carry a
package refusal from the worker through the controller transport to its
reason. Dropping the host entry point's refused publication, the remote token's
naming, the protocol's refused phase, the `image-pull` or `package-license`
entry, or the package worker's token (back to `transport` or `packages`) each
failed a guard test.

**Constraints left behind:** the tokens the workers raise outside a block's
table (the cluster's `registry-ownership`, `host-time`, `device-not-empty`,
`foreign-osd` and the other `*-unqualified` and `foreign-*` refusals among them)
reach the operator through the block's generic `unknown` or `remote-refusal`
diagnostic with the token in its detail, not a remedy of their own. The
package worker now ships `ceph_native_worker` in its closure to name a site. The
detail of a controller-local refusal is checked only against the protocol's
character filter, not attested against the shipped sources; that is safe
because every reason it names is a code literal. The automation digest moved. The live confirmation is the next continuation of
[B476](delivered.md#x94--post-destroy-cluster-inspection)'s run.

### X73 — Contained probes proved as Podman reports them

**Owner:** Storage. An out-of-sequence defect slice of M2, integrated on the M2
branch as one commit at the owner's request of 2026-10-13 to fix the live
failure of B476's run. **Items:** B524. **Integrated:** "fix(ceph): prove
contained probes as podman 5.8 reports them".

**Outcome, B524:** every Ceph host block of the M2b run of 2026-10-13 refused
at `image-profile@ceph_host_runtime.prove_container:501` once X72 named it:
the probe ran, and the proof before its removal read a `HOSTNAME` that Podman
adds to a started container that sets none, so the exact five-variable
environment check failed on every host. The probes now pin `--hostname` and
its `HOSTNAME`, and the proof also requires that hostname. Running the real
proofs against the frozen image under Podman 5.8 found two more refusals the
next run would have met: Podman records a bind as
`<source>:<target>:ro,z,rprivate,nosuid,nodev,rbind` and drops `z` once the
start relabelled it, which the label and route probes' exact `ro,z` match
refused; and its inspection has no `HostConfig.Sysctls`, which the route
probe's sysctl match refused. A bind now needs `ro`, and `z` until the
container has run, with only those recorded defaults beside them, and sysctls
are proved from the recorded create command, which must name exactly the
requested ones. The owning contract is in
[storage coverage](../storage-coverage.md).

**Guard tests:** the collection's
`tests/unit/plugins/module_utils/test_ceph_host_runtime.py`, whose engine
fixture now reports a container as Podman 5.8 does before and after its start:
`test_a_started_probe_with_mounts_and_a_sysctl_is_proved_as_podman_reports_it`,
`test_a_created_probe_with_mounts_or_sysctls_refuses_their_drift` and the
`hostname`, `hostname-environment`, `duplicate-environment` and
`requested-sysctl` drift cases. Against the previous module the fixture fails
16 tests, among them the facts, profile and device-identity tests.

**Constraints left behind:** a sysctl Podman adds from `containers.conf`
`default_sysctls` is not visible to the proof, because Podman reports none.
The route probe's exercise inside the image needs a root host to run as the
ceph account and was not reproduced off a guest; its first run is the next
continuation of [B476](delivered.md#x94--post-destroy-cluster-inspection)'s run. The automation digest moved.

### X74 — continuation under a rebuilt binary

**Owner:** State reconciliation, with CLI and Storage. An out-of-sequence
enabling slice of M2, integrated on the M2 branch as one commit at the owner's
request of 2026-10-13 (D145). **Items:** B525, new on 2026-10-13 from the
owner's M2b Ceph run, Kind `enabling`: it narrows no refusal and adds no
effect, so `safety` does not fit, and it adds a guard, an audit member and a
preview line around a rule D142 already settled. **Decisions:** D145.
**Integrated:** "feat(lifecycle): audit and guard continuation under a rebuilt
binary".

**Outcome, B525:** the question after the live M2b run was whether a fix to a
rebuilt binary must retire the operation it fixes. It need not: the 35-minute
destroy that followed X73's fix was advised on a false premise. Every content
digest is computed from the implementation's declared contract (its identity,
request and evidence versions and effect pins) and no capability package reads
the automation, so an `ansible/`-only fix moves the automation digest and no
content digest, request byte or version. `proveSameBlocks` plans the same
blocks, the closure check passes because the execution closure excludes the
automation, and `setup` carries the closure onto the new automation, so the
operation already continued under D142's rule. X74 keeps that rule (D145:
dropping the content digest from it would continue an operation across a pin
change) and adds what was missing around it. A contract guard pins, per
implementation, the content digest beside its Go request and evidence schemas
and the persisted format versions quoted in the collection's embedded plugins
and roles, and fails the build when any of them changes with the digest unmoved;
`-update` refuses to record it, and each digest is checked against the blocks
the real capabilities plan on the contract examples. Every attempt and
resolution record names the build it ran under (`build`: automation digest,
content digest, executable version and commit, `modified`), optional, so the
record version stays 1, and an adopted interrupted start names the build that
adopts it. A destroy continuation keeps X69's rule: its removal is derived from
the frozen plan and runs under the build in hand, as a fresh removal does, and
its proof compares what the removal decides, the authorizations it consumes, its
groups and that each block's capability reads its frozen request, never content
digests, which the removal copies from the frozen plan; an unknown removal block
is observed by the build in hand, whose closed evidence decoding refuses
evidence it cannot read, and every removal attempt and resolution names the
build that ran it. A continuation previewed under another build, by `plan` and
by the `apply` or `destroy` confirmation, closes with a `Build` field naming the
build that registered the operation and the steps this build runs, and `status`
reports `anotherBuild` in JSON and a `, another build than this one` suffix on
`Registered by`; the preview has no JSON form because `plan`, `apply` and
`destroy` have no JSON mode. No content digest, request version, record version
or plan encoding moved, so the owner's live operation, registered by 91855545e,
continues under the X74 build. `setup --purge-old-bundles` never retires what
such a continuation reads: it runs the current receipt's bundle, which
retirement never removes, and the closure is unchanged. The fix path is `make
build`, `bootwright setup`, then `bootwright apply` with the same `--stage`, as
the [continuation knowledge
page](../../.agents/knowledge/continuation-after-a-fix.md) records.

**Guard tests:** `TestEveryImplementationMovesItsContentDigestWithItsContract`
and `TestEveryAutomationVersionLiteralIsClassified` in `cmd/bootwright`, with
the fifteen goldens under `cmd/bootwright/testdata/contracts`, scanning every
`.py`, `.yml`, `.yaml` and `.j2` file of the collection's plugins and roles; the
Ceph core golden carries the runtime-profile, device-identity and host-cache
formats its workers read, and the Go request versions the roles check are
classified as mirrored;
`TestAnAttemptAndItsResolutionNameTheBuildTheyRanUnder`,
`TestAnAdoptedStartNamesTheBuildThatAdoptsIt`,
`TestAStartRefusesAnInvalidBuild` and
`TestAnAttemptRecordWithoutABuildStaysReadable` in
`internal/reconciliation/operationstore`, whose attempt and resolution goldens
(`TestOperationRecordsMatchTheirGoldens`) gained `build`;
`TestAnotherBuildRetriesAFailedBlockUnderItsOwnBuild`,
`TestAnotherBuildResolvesAnUnknownApplyThenRetriesIt`,
`TestAContinuationUnderAnotherBuildNamesTheStepsThisBuildRuns`,
`TestAPreviewNamesNoOtherBuildWhereNoContinuationRunsUnderOne`,
`TestAPreviewNamesNoOtherBuildWhenNoStepRunsUnderIt`,
`TestStatusSaysWhetherAnotherBuildRegisteredTheOperation`,
`TestARemovalUnderAnotherBuildNamesTheBuildThatRegisteredIt`,
`TestACephClusterLeftUnknownContinuesUnderARebuiltBinary`, which replays the
live case through the real Ceph capabilities, and
`TestACephClusterContinuesOverRecordsAnEarlierBuildWrote`, which continues it
over records that name no build, in
`internal/reconciliation/lifecycle`, with
`TestAnotherBuildRefusesToContinueABlockItWouldFreezeDifferently` gaining two
unknown-apply rows and
`TestAnotherBuildContinuesAnOperationWhoseBlocksItFreezesTheSame` asserting the
destroy resolution names the build; and `TestPlanNamesTheStepsAnotherBuildRuns`,
`TestConfirmationNamesTheStepsAnotherBuildRuns` and
`TestStatusSaysAnotherBuildRegisteredAnOperationWithoutExecutable` in
`internal/cli`, with the goldens `cli-plan-continuation-other-build`,
`cli-status-another-build` and `cli-status-another-build-json`; the other
status JSON goldens gained `"anotherBuild": false`.

**Constraints left behind:** the guard sees the Go request and evidence schemas
and quoted version literals in the collection's embedded plugins and roles, not
what a record or mark means; it fingerprints a raw-JSON member (the cluster
services the Ceph core and host requests carry, the agent media's install and
agent configurations) as opaque, and a version built dynamically, an unquoted
one and an unversioned mark are invisible to it, so the schema nested in such a
member and a change of meaning under an unmoved version stay the author's to
catch, with removal's ownership proof as the backstop. A pin or version change
still needs the destroy, since it moves the content digest. `build` is
compatible one way only: a build from before X74 decodes records closed and
refuses one that carries it as "lifecycle record is malformed or unsupported",
so once a build that writes `build` has worked an operation, no earlier build
can continue or remove it, and the removal under the build in hand is the way
out (D145). `plan` still previews a continuation its `apply` refuses on recorded
facts ([B526](backlog.md#b526)); no gate proves the approved bundle holds this
executable's automation before work starts ([B527](backlog.md#retired)); `setup
--purge-old-bundles` reads no operation record ([B528](backlog.md#retired)); and an
unknown block a resolution leaves failed is retried by the next apply, so
recovery can take two applies ([B529](#x82--resolution-and-retry-in-one-invocation)). Already present: an
attempt record holds at most `MaxAttemptBytes` (64 KiB), the evidence bound most
implementations use, so evidence at its maximum cannot fit one, and `build` adds
about 300 bytes to every record.

### X75 — Native sources behind root-owned links

**Owner:** Storage. An out-of-sequence defect slice of M2, integrated on the M2
branch as one commit at the owner's request of 2026-10-13 to fix the live
failure of B476's run. **Items:** B530. **Integrated:** "fix(ceph): follow a
root-owned link on a public root-only native source".

**Outcome, B530:** the cluster block of the M2b run of 2026-10-13 refused on
every host at `native-unknown:OSError@ceph_native.native_source:2029` once X72
named it: `authorize` proves the guest sshd include closure before any effect,
and RHEL 9's `50-redhat.conf` includes
`/etc/crypto-policies/back-ends/opensshserver.config`, a root-owned symbolic
link that `O_NOFOLLOW` refuses with ELOOP; its target
`/usr/share/crypto-policies/DEFAULT/opensshserver.txt` carries a second hard
link, which the one-name rule would have refused next. A public root-only
source now follows a root-owned link through the same ownership proof, under
a bound of eight, and may carry a package's hard links; a private source, an
account source and a link anyone else owns still refuse, and a private source
is still the only name of its inode. The owning contract is in
[storage native API](../storage-native-api.md).

**Guard tests:** the collection's
`tests/unit/plugins/module_utils/test_ceph_tunnel_native.py`:
`test_a_public_root_only_source_follows_the_root_owned_link_to_its_hard_linked_policy`
(an absolute and a relative link target),
`test_a_public_root_only_source_follows_links_only_within_the_bound` and
`test_every_other_source_refuses_a_link_and_a_second_name`, over a fixture
shaped like the RHEL 9 back-end; the existing drift cases keep refusing.

**Constraints left behind:** the walk proves what sshd reads by following the
same links sshd follows, but the whole chain is still never exercised against
a live host before the next apply; the first run of the phases after
`authorize` is the next continuation of [B476](delivered.md#x94--post-destroy-cluster-inspection)'s run. An
Ansible-only change moves the automation digest and no frozen block, so that
continuation needs a rebuild and `setup`, not a destroy.

### X76 — The live cluster phases as cephadm and ceph behave

**Owner:** Storage. An out-of-sequence defect slice of M2, integrated on the M2
branch as two commits at the owner's request of 2026-10-13 ("fix once for
all") after the third first-call refusal of B476's run. **Items:** B531 (the
bootstrap, configuration, readiness and timing phases), B532 (the observation
and custody proofs). **Integrated:** "fix(ceph): survive the live cluster
phases as cephadm and ceph behave" and "fix(ceph): observe the live host as
cephadm leaves it".

**Outcome:** an Opus audit of every phase after `authorize`, confirmed against
the three RHEL 9.8 guests and the IBM Storage Ceph 9.9.1.2 image, found the
defects the next live run would have met one by one, several of which would
have left the cluster in a state only a destroy exits. B531: cephadm writes
the bootstrap `ceph.conf` 0644 under sudo's umask, so capture now proves both
outputs root's own single-link files inside the private FSID area and makes
them 0600 through an O_NOFOLLOW descriptor before recording their identities,
while an observation before capture proves cephadm's own modes and changes
nothing; the `config-key` setter reads its value back through the text path,
because `--format json` prepends a blank line; the call-home acceptance flag
is read case-insensitively; an OSD the map holds before its first boot
(`[{"id":0}]`) is starting, not malformed; phases wait, bounded (300 s, 5 s
pause, inside a 480 s horizon under the 640 s worker budget), for the MON to
map an available manager whose orchestrator answers after the service spec,
`tunnel_mount` or the dashboard redeploy restart it, and manager-served reads
repeat only while it restarts; readiness is HEALTH_OK or only checks in the
closed tolerated set holding `CHA_ERROR_SENDING_REPORT`, and a readiness
timeout names the blocking checks and pending daemons in its `not-ready:`
detail; bootstrap runs under its own launcher admitting only that phase, an
1800 s worker budget under the 1920 s ceiling with cephadm bounded at 1620 s,
and the cluster block's deadline counts the bootstrap call and both readiness
waits (21540 s for three hosts). B532: the mount-source identity walk follows
a root-owned directory link such as `/var/run -> ../run`, bounded by
`SOURCE_LINKS`, and records the resolved directory; the `selinux` directory
cephadm makes for osd and ceph-volume containers is admitted root-owned with
no bit beyond 0755 and never read; a daemon directory of Bootwright's fsid
whose unit files are not yet written, whose systemd instance is not yet
installed or whose `--rm` container is gone is reported not ready, while a
container that exists and contradicts the request still refuses; and in the
read-only `observe`, `host_state` and `bootstrap_state` paths a manager or
seed daemon between containers is reported pending and named, where every
action-time proof still requires it running. No ContentDigest input or
request/evidence version moved, so the owner's live operation continued under
the rebuilt binary. The contracts are in [storage](../storage.md) and
[storage native API](../storage-native-api.md); the facts are in
[the live-host knowledge page](../../.agents/knowledge/ibm-ceph-live-host-facts.md).

**Guard tests:** in the collection's unit tests,
`test_bootstrap_capture_makes_cephadms_public_config_private_before_recording_it`,
`test_an_uncaptured_seed_is_proved_with_cephadms_own_modes_and_never_changed`,
`test_a_stored_setting_is_read_back_as_the_cli_prints_it`,
`test_the_call_home_acceptance_flag_reads_both_spellings_of_one_boolean`,
`test_an_osd_the_map_holds_before_its_first_boot_is_starting_not_malformed`,
`test_enrollment_waits_out_the_restarted_orchestrator_before_its_fence`,
`test_the_initial_manager_redeploy_waits_for_the_restarted_manager_before_any_proof`,
`test_an_orchestrator_read_while_the_manager_restarts_is_unfinished_not_refused`,
`test_call_home_egress_alone_never_blocks_readiness_and_every_other_check_is_named`,
`test_the_bootstrap_call_runs_alone_under_its_own_longer_budget`,
`test_the_bootstrap_launcher_admits_only_bootstrap_within_its_ceiling`,
`test_bind_source_under_a_root_owned_directory_link_is_the_resolved_directory`,
`test_bind_source_follows_directory_links_only_within_the_bound`,
`test_selinux_directory_cephadm_makes_for_osd_and_ceph_volume_is_metadata_only`,
`test_daemon_directory_before_its_unit_files_is_not_running`,
`test_daemon_whose_rm_container_is_gone_is_not_running`,
`test_transient_states_never_admit_a_contradiction`,
`test_observation_names_a_manager_cephadm_is_restarting_as_not_ready`,
`test_observed_manager_between_containers_is_pending_while_an_action_refuses`,
`test_seed_between_containers_is_observed_while_an_action_refuses` and
`test_observed_running_manager_that_contradicts_the_request_still_refuses`,
each run against the unmodified modules and failing there; in Go,
`TestTheClusterDeadlineCountsTheWorkersOwnBudgets`.

**Constraints left behind:** `READINESS_TIMEOUT` stays 900 s, because three
hosts sit 60 s under the six-hour block ceiling; a timeout is recovered by a
later apply's observation. `prove_seed`'s orchestrator reads after
`bootstrap-original-ready.json` still require the seed manager active, and
`tunnel_mount` keeps the 600 s `cephadm deploy` timeout inside its 640 s
budget. The bootstrap launcher overrides `ceph_host_worker` globals from
`ceph_core` rather than taking a phase and ceiling parameter. The `selinux`
directory's contents are never inspected. `podman ps --format json` shapes
were checked on the controller's 5.8.8, not the guests' 5.8.2. A manager whose
unit is starting or stopping while its container exists is pending in that
snapshot, and its mounts are proved by the next one. The independent review's
seven minor findings, each transient and recovered by a later apply, are
parked as [B533](backlog.md#b533). The first live run of every phase after
`authorize` is the next continuation of [B476](delivered.md#x94--post-destroy-cluster-inspection)'s run.

### X77 — Device reads on a host claimed before its authorization

**Owner:** Storage. An out-of-sequence defect slice of M2, integrated on the M2
branch as one commit at the owner's request of 2026-10-13 to fix the live
failure of B476's run. **Items:** B534. **Integrated:** "fix(ceph): read the
devices of a host claimed before its authorization".

**Outcome, B534:** the cluster block's retry under the X76 build refused at
`native-refusal:registry-ownership@ceph_native.prove_registry:1413`: the first
attempt had claimed every host and then failed inside `authorize` before the
registry authfile was prepared, and `apply_tunnel_cluster` runs `devices` on
every host before `claim` and `authorize`, where `devices` reproved the registry
of any owned host. A host claimed before its authorization holds no registry
yet, so every retry refused and any failure inside `authorize` wedged the
operation. `devices` now reproves the registry only once `registry-file.json`
records it; until then `authorize` prepares it with its full foreign-file
checks. The owning contract is unchanged.

**Guard tests:** the collection's `tests/unit/plugins/module_utils/test_ceph_registry.py`:
`test_a_claimed_host_whose_authorization_never_published_its_registry_still_reads_its_devices`
(a claimed host, and one whose authorization recorded only its intent), which
failed on the previous module, and
`test_a_published_registry_is_reproved_before_any_device_read`.

**Constraints left behind:** none beyond X76's; the next continuation of
[B476](delivered.md#x94--post-destroy-cluster-inspection)'s run is its live confirmation.

### X78 — Host trust compared in the form the transport bundle holds

**Owner:** Storage. An out-of-sequence defect slice of M2, integrated on the M2
branch as one commit at the owner's request of 2026-10-13 to fix the live
failure of B476's run. **Items:** B535. **Integrated:** "fix(ceph): compare
bootstrap host trust in the form the transport bundle holds".

**Outcome, B535:** the cluster retry under the X77 build refused at
`native-refusal:ownership@ceph_native.tunnel_bootstrap:1775`. `transport()`
retains `known_hosts` as `ceph_tunnel.known_hosts` renders it (entries in host
name order, one line each, no blank lines), while the controller sends each
entry with `HostKey.Line`'s own line end followed by one more
(`boundMaterial` in `internal/storage/ceph/materials.go`), and
`tunnel_bootstrap` compared those raw bytes with the retained form, so the
bootstrap could never start. It now renders the controller's material through
the same function before comparing; host trust naming another host set still
refuses. The unit fixture retained the raw bytes, which hid the mismatch.

**Guard tests:** the collection's `tests/unit/plugins/module_utils/test_ceph_tunnel_native.py`:
`test_bootstrap_accepts_the_controllers_known_hosts_in_the_form_the_bundle_holds`,
which failed on the previous module, and
`test_bootstrap_refuses_known_hosts_naming_fewer_hosts_than_the_bundle`; the
`prepared_bootstrap` fixture now retains the rendered form.

**Constraints left behind:** the controller still writes a blank line after
each known_hosts entry; every reader renders it, so it is left as is. The next
continuation of [B476](delivered.md#x94--post-destroy-cluster-inspection)'s run is its live confirmation.

### X79 — cephadm bootstrap reads its podman version

**Owner:** Storage. An out-of-sequence defect slice of M2, integrated on the M2
branch as one commit at the owner's request of 2026-10-13 to fix the live
failure of B476's run. **Items:** B536. **Integrated:** "fix(ceph): let cephadm
bootstrap read its podman version behind a no-change host proof".

**Outcome, B536:** the first live `cephadm bootstrap` (IBM Storage Ceph
9.9.1.2, cephadm 20.2.1-405) failed at "Creating mon..." with
`native-refusal:native-command@ceph_native.command`: cephadm's `main()` skips
its container-engine check for `bootstrap`, and the only bootstrap path that
reads the podman version is its host preparation, which Bootwright skipped with
`--skip-prepare-host`, so the first consumer of the version
(`should_log_to_journald` for the mon's log target) raised `RuntimeError`
before the mon store was made. Both bootstrap argv builders drop the flag, and
before `bootstrap-attempt.json` is written the worker proves that cephadm's
host preparation will change nothing: podman at or above cephadm's minimum,
`/usr/sbin/lvcreate` executable, and, walking cephadm's own time-unit list in
its order, an enabled and active unit reached before any installed unit cephadm
would enable. A host that would be changed refuses `host-prerequisite` or
`host-time` with no attempt record, so the bootstrap may be retried after the
operator fixes it.

**Guard tests:** the collection's `tests/unit/plugins/module_utils/test_ceph_tunnel_native.py`:
`test_a_rhel_guest_with_running_chronyd_lets_cephadm_prepare_the_host_without_a_change`,
`test_a_host_cephadm_would_change_while_preparing_it_refuses_before_any_bootstrap_record`
(a disabled or enabled-but-stopped unit ahead of the running one, no unit, an
old or unparsable podman, no lvcreate) and
`test_a_host_preparation_refusal_leaves_no_bootstrap_attempt_so_the_bootstrap_may_be_retried`;
the bootstrap argv tests assert the flag is absent.

**Constraints left behind:** a bootstrap that fails after its attempt record
leaves a partial seed the next apply never restarts; recovering one whose mon
store was never made is parked as [B537](backlog.md#b537). The live run of
[B476](delivered.md#x94--post-destroy-cluster-inspection) is its confirmation.

### X80 — Observation snapshots settle while cephadm converges

**Owner:** Storage. An out-of-sequence defect slice of M2, integrated on the M2
branch as one commit at the owner's request of 2026-10-13 to fix the live
failure of B476's run. **Items:** B538. **Integrated:** "fix(ceph): observe a
host's daemon layout again while cephadm converges".

**Outcome, B538:** after a successful bootstrap, tunnel phases, cache, enroll
and services (mon, mgr and osd.2 running on rhel-01, OSDs deploying on the
other hosts), the cluster block's readiness wait refused
`native-refusal:native-observation-unproved@ceph_native.daemons:1195`. The
guest's layout read afterwards was valid in every respect the observer proves,
and cephadm.log showed the manager deploying an OSD on that host in the second
the attempt ended. `ceph_native_observe.daemons()` re-proves its snapshot (the
listings at the end, each directory's and bound unit file's identity, the
ancestry), and cephadm creating a daemon directory or rewriting `unit.meta` or
`unit.image` during it failed that re-proof exactly as a foreign layout would,
which ended the wait. A re-proof that finds something moved now raises
`ObservationChangedRefusal`: a listing that differs at the end, an identity
that differs between its first read and its re-check, an entry gone after it
was listed, and a unit whose running state disagrees with its container's and
reads differently when read again. `daemons()` takes the snapshot again, at
most ten times 3 s apart within 60 s, and the worker refuses `not-ready` only
when it never settles. The readiness wait treats exactly that remote
`not-ready` from `observe` or `host_state` as not ready, polls again and names
`unsettled@<host>` in its timeout detail; any other remote refusal still ends
it at once. Owner, mode, link, name, FSID, content, image and unit
contradictions still refuse `native-observation-unproved` with no retry. No
version literal, content digest input or Go table changed; `not-ready` was
already in both reason sets.

**Guard tests:** the collection's
`tests/unit/plugins/module_utils/test_ceph_native_observe.py`:
`test_a_listing_changed_during_the_snapshot_settles_on_the_next`,
`test_unit_meta_rewritten_during_the_snapshot_is_taken_again` (replaced and
rewritten in place),
`test_a_daemon_directory_removed_after_its_listing_is_a_change`,
`test_a_unit_that_moved_between_its_read_and_its_container_is_a_change`,
`test_a_snapshot_that_never_settles_is_changed_and_bounded` (the attempt bound
and the horizon) and
`test_worker_maps_an_unsettled_snapshot_to_not_ready_and_a_contradiction_to_unproved`,
each of which failed on the previous modules, and
`test_a_contradiction_refuses_at_once_without_another_snapshot`;
`test_ceph_core.py`:
`test_an_unsettled_snapshot_is_not_ready_and_the_wait_polls_again` and
`test_a_host_that_never_settles_times_out_naming_it`, both of which failed on
the previous module, and
`test_a_contradiction_or_any_other_remote_refusal_still_ends_the_wait`.

**Constraints left behind:** a read-only `observe` operation outside the
readiness wait still refuses `not-ready` for a host that never settles rather
than returning partial evidence. A transient name cephadm stages in the FSID
directory (such as its `cephadm.<sha256>.new` upload) is still a foreign name.
The next continuation of [B476](delivered.md#x94--post-destroy-cluster-inspection)'s run is its live confirmation.

### X81 — The pool block reads the pool ID Ceph's listing names

**Owner:** Storage. An out-of-sequence defect slice of M2, integrated on the M2
branch as one commit at the owner's request of 2026-10-13 to fix the live
failure of B476's run. **Items:** B539. **Integrated:** "fix(ceph): read the
pool ID Ceph's pool listing names".

**Outcome, B539:** with the cluster block done, the pool block's
`osd pool create` succeeded and the worker then failed
`native-unknown:KeyError@ceph_native.pool:2432`, leaving the pool, its
ownership and attempt records, and no recorded pool ID. Every read of
`osd pool ls detail` took the ID from a `pool` key, which only `osd dump`
uses; the listing names it `pool_id` (read from a monitor of the qualified
image), and the test fixtures had copied the wrong shape. `Worker.pool_id`
now takes `pool_id` from a listing entry and refuses `native-output` for
anything but a non-negative integer, in the pool's create, resume and
identity checks, the client's pool binding and the observation. The
lost-reply resume accepted only the frozen PG count, which the PG
autoscaler, on by default, may already have moved: a moved count is accepted
only when the entry's `pg_autoscale_mode` is `on`. The observation refused
`pool-identity` for a same-name pool with no recorded ID even when this
operation's own attempt record named it, which would have kept the block
unknown: that pool is now unfinished work (not ready, ID -1, partial), which
the retry resumes without a second create; any other unrecorded or
different ID still refuses. No version literal, content digest input or Go
table changed; the Go evaluator already resolves that evidence as partial
(`TestAbsentNativeResourceWithOwnedResidueRemainsPartial`).

**Guard tests:** the collection's
`tests/unit/plugins/module_utils/test_ceph_core.py`:
`test_a_lost_creation_resumes_after_the_autoscaler_moved_pg_num_only_when_it_is_on`,
`test_a_pool_listing_without_pool_id_refuses_as_native_output`,
`test_a_pool_this_operation_created_without_a_recorded_id_observes_as_unfinished`
and `test_an_unproved_pool_id_still_refuses_observation`, with the pool and
client fixtures of `test_ceph_core.py` and `test_ceph_client_custody.py` now
in the listing's real shape; 90 of these tests failed on the previous module.

**Constraints left behind:** the resume still identifies its pool by name,
type, rule and attempt record only, which a pool deleted and recreated under
the same name between the create and the resume also satisfies; B540 tracks
a stronger fingerprint. The next continuation of [B476](delivered.md#x94--post-destroy-cluster-inspection)'s run is
its live confirmation.

### X82 — Resolution and retry in one invocation

**Owner:** State reconciliation. An out-of-sequence enabling slice of M2,
integrated on the M2 branch as one commit at the owner's request of
2026-10-13. **Items:** B529, parked on 2026-10-13 by X74 for an owner
decision and taken in by that request. **Decisions:** D146.
**Integrated:** "fix(lifecycle): retry a block its resolution leaves failed
in the same invocation".

**Outcome, B529:** on the M2b run, the `apply --stage clusters` that
continued after X81 resolved the unknown pool block as partly realized,
recorded it `failed`, printed `[FAIL]` and stopped; a second `apply` retried
the pool and ran the client. The scheduler marked a block worked when it
started either an attempt or a resolution, so a block its resolution left
`failed` counted as one this invocation had failed itself, and no first
attempt followed either. It now keeps the blocks it observed apart from the
blocks it attempted: an invocation observes a block at most once and never
one its own attempt left unproved, and once every unproved effect is
resolved, a block a resolution left `failed` is retried under the rules of
any retry (stage selected, dependencies `done`, exclusive resources free, the
bound), its dependents following in the same invocation. Only a failed
attempt stops further first attempts. A fresh removal's observe-only
resolutions retry nothing. The resolution of a block it will retry reports
its row running ("the effect is unfinished; retrying it") instead of failed,
so the retry continues that row; when the invocation stops before the retry
starts, or the retry cannot start, the row is settled with the state the
block keeps. A retry's own outcome replaces the resolution's diagnostic, so a
successful retry reports no failure. The stage preview is unchanged: it marks
the resolution, and the rest follows from what it proves. No record, request
or evidence version, content digest or plan encoding moved.

**Guard tests:** `internal/reconciliation/lifecycle`:
`TestABlockItsResolutionLeavesFailedIsRetriedByTheSameApply` (no effect and
partial), `TestARetryAfterAResolutionThatFailsAgainStopsFirstAttempts`,
`TestABlockAResolutionLeavesFailedOutsideTheSelectionIsNotRetried`,
`TestAnAnnouncedRetryThatNeverStartsIsSettledFailed` and
`TestAFailedResolutionIsRetriedButAFailedAttemptIsNot`; the resolution
journeys now converge in the invocation that resolves them
(`TestUnknownOutcomeIsResolvedFromLiveEvidence`,
`TestAPartlyRealizedBlockIsConvergedByTheOperationThatResolvesIt`,
`TestAPartlyRemovedBlockIsConvergedByTheRemovalThatResolvesIt`,
`TestAnotherBuildResolvesAnUnknownApplyThenRetriesIt`,
`TestAnUnknownDestroyBlockResolvesByWhatItsRemovalProves`,
`TestACephClusterLeftUnknownContinuesUnderARebuiltBinary`,
`TestAKeptKubeconfigStoppedBetweenTheMoveAndTheRemovalIsInBothPlaces`), whose
earlier versions, asserting the second invocation, fail on this scheduler; the kill harness's bound on the
invocations a killed journey needs fell from 2 to 1 for the fresh apply,
destroy, continuation, replacement and partial journeys and stays tight;
`TestAPartlyRealizedBlockWhoseRetryFailsIsConvergedByRepeatingTheOperation`,
`TestAPartlyRealizedBlockIsAlsoRemovable` and
`TestAResolutionRemedyNamesTheCommandThatTakesItOn` keep the remedies of a
resolution whose retry fails or never starts.

**Constraints left behind:** a retry that records its start but whose log path
cannot be formed or whose log cannot open leaves the resolution's row running
and, with no cause of its own, replaces the resolution's remedy by the log
fault the boundary reports; the operation is then `unknown`, which that
remedy no longer described. The next continuation of [B476](delivered.md#x94--post-destroy-cluster-inspection)'s run
is its live confirmation.

### X67 — Go toolchain vulnerability gate

**Owner:** Architecture. A defect slice of [M1](m1.md), planned by X46's
second pass and blocked at the owner's request (D126) until the owner
authorized go1.26.9 on 2026-10-14 (D147). **Items:** B470. **Decisions:**
D126, D139, D147.

**Outcome, B470:** Go moves from go1.26.8 to go1.26.9: `scripts/go` selects it
and both modules' `go` directives name it, so CI's setup-go installs the same
Go. go1.26.9 fixes the ten reachable standard-library vulnerabilities
govulncheck had reported since X46's second pass (GO-2026-6603, 6604, 6605,
6607, 6608, 6610, 6611, 6612, 6613 and 6617, in `net/http`, `net/textproto`,
`crypto/tls` and `os`), so `make vulncheck`, and with it `make check`, passes
again. The [version output](../cli/commands.md#version-output) example and the
[build knowledge](../../.agents/knowledge/build-toolchain.md) move with it.
Both modules' requirements are unchanged, and Go 1.27 stays parked
([B273](backlog.md#b273)).

**Gates:** on go1.26.9, every step of `make check` passes, `make vulncheck`
reporting no reachable vulnerability; `./scripts/ansible-check` ran through a
pipe, because the agent harness hands it non-blocking output. `make race`,
`make docs-check` and `./scripts/check-commits` pass. No real-host run.

**Digest and version effects:** none; no automation, content or plan digest
and no request or record version moves, so a live context continues under a
build that contains X67. **Constraints left behind:** setup-go's cache key
names the Go version, so the first CI run after X67 starts with a cold Go
cache. govulncheck still lists GO-2026-5932, the unmaintained
`golang.org/x/crypto/openpgp` package, as a module finding that no code calls
and that has no fix; it does not fail the gate.

### X83 — operation directories created concurrently

**Owner:** Workspace, with State reconciliation. A defect fix of [M1](m1.md)
(D116), made on the M2 branch on 2026-10-12 during the owner's M2b fixture
run, which reached `main` in the M2 squash (a3cf8aeae) and is recorded on M1
on 2026-10-14. **Integrated:** "fix(contextfs): reopen a directory a
concurrent creator made first" (7afbc159c, on the M2 branch). **Items:** B506.
**Decisions:** D116.

**Outcome, B506:** the operation store creates `<id>`, `<id>/blocks` and
`<id>/logs` up front; `logs/blocks`, each block's directory and, on a
context's first operation, the `operations` subtree are created by the first
write that needs them. Concurrent blocks (up to `MaxRunningBlocks`, 8) raced
on that creation, and the losers' `EEXIST` was refused as a foreign entry.
That surfaced as `runtime.log` "a required private operation log could not be
maintained", cancelled the invocation inside the siblings' record
publications and left their blocks unknown; the owner's 2026-10-12 destroy of
a 13-block context hit it in its first wave of four. The lifecycle, bounded-run
and SSH-trust operation areas now create each path component in one step that,
on `EEXIST`, reopens the directory through exactly the checks an existing
directory passes (no link, beneath the parent, same device, the parent's
owner, mode 0700), so no check is weakened, and the loser syncs the parent
itself before it continues. Deliberately exclusive creations (a run's
directory, a setup run, a snapshot revision, bundle and client areas) still
refuse `EEXIST`. The [concurrent block execution
knowledge](../../.agents/knowledge/concurrent-block-execution.md) records the
symptom.

**Guard tests:** `TestConcurrentWritersShareTheDirectoriesTheyCreate` in
`internal/workspace/contextfs` runs eight rounds of 16 writers behind one
barrier on the real filesystem. It failed 50 of 50 runs before the fix and
passed 200 race runs after it.

**Gates:** on the M2 branch with the fix, as its commit records: `make quick`,
`make docs-check` and `make race` passed, and `make check` stopped only at
`make vulncheck` (B470). On `main` at 524cb367d on 2026-10-14: the test passes,
and fails in every round with the fix reverted; `make quick`, `make race` and
`make docs-check` pass. No real-host run.

**Digest and version effects:** none; no record, request or plan digest
moves. **Constraints left behind:** [B510](backlog.md#b510), parked: a writer
whose plain open lands between a concurrent creator's creation and its parent
sync proceeds without syncing the parent, and `selectionfs` skips the parent
sync on `EEXIST`.

### X84 — Fedora 44 controllers

**Owner:** Controller setup. An enabling slice of [M1](m1.md), at the owner's
request of 2026-10-14 (D149), after the owner's controller moved to Fedora 44
and `setup` refused it with `controller.unsupported`. **Items:** B541, and
B547 as a follow-up of 2026-10-14. **Decisions:** D149.

**Outcome, B541:** `setup`, `preflight controller` and the controller stage
admit Fedora 44 on Linux/amd64 beside Fedora 43 and RHEL 9.8, and their
refusals name all three. The compiled catalog holds Fedora 44's execution
foundation as scripts/foundation-catalog printed it on the owner's Fedora 44
host: glibc 2.43-9.fc44 and libgcc 16.2.1-2.fc44, whose versioned library is
`libgcc_s-16-20260819.so.1`. A Fedora controller's native packages resolve
from its own release's `releases` and `updates` repositories, signed by that
release's primary key: for Fedora 44, fingerprint
`36F612DCF27F7D1A48A835E4DBFCF71C6D9F90A6`, which `fedora-gpg-keys-44-2`
installs and `rpm -V` verified, and whose key ID `dbfcf71c6d9f90a6` rpm 6.0.2
prints for the host's Fedora 44 packages. The collection reads
`RPM-GPG-KEY-fedora-<release>-primary` for a qualified release only, and still
imports only the key the plan's signer names. Fedora 43's and Fedora 44's
foundations share every path but the versioned libgcc file, so a requirement
setup qualified now matches the compiled record whose GCC major that file
keeps, which a qualification of the same upstream version always does, and no
record when two share that shape; a launch refusal therefore never names
another release to qualify within. [Development](../../docs/development.md#lock-refresh-and-tested-hosts)
records the builds and what admitting another Fedora release takes.

**Outcome, B547 (follow-up):** the first resolve against the Fedora 44
repositories, run unprivileged on the owner's host through
TestNativeCurrentOSResolution, refused with `controller.setup: the provided
native package solver refused its isolated dependency operation`. Every
baseline root was already installed there, and libdnf5 5.4.6.0 answers
`add_rpm_install` of an installed package with a transaction entry whose action
is `Reason Change`, which `solve5` refused as an unrequested action; the
apply-side re-solve from the plan's own RPMs reports the same entry for an
installed build. A reason change moves no file, so `solve5` now leaves it out
of the plan, neither inbound nor outbound, and still refuses every other
unrequested action. After the fix the same resolve passes (3 roots, 3 sources,
0 changes, 46 s including the metadata stage) and TestNativeCurrentOSInspection
finds the roots by name. The
[knowledge page](../../.agents/knowledge/native-root-reason-change.md) records
the symptom and how to read the helper's own refusal.

**Guard tests:** `Dnf5Transaction` in the collection's
`test_native_resolution.py` (a reason change is neither inbound nor outbound;
other unrequested actions still refuse);
TestTheCatalogAdmitsOnlyItsCompiledFoundations,
TestTheCompiledExecutionFoundationIsUnchanged (Fedora 44's digest as
admitted, Fedora 43's and RHEL 9.8's unchanged),
TestEveryCatalogFoundationFileHasOnePackage (each record's builds belong to
its own release), TestALaunchRefusalNamesSetupForAQualifiableUpdate (a
qualified requirement of each release names that release; it fails without
the GCC-major match), TestARequirementInTheShapeOfTwoReleasesNamesNeither,
TestEachFedoraReleaseResolvesFromItsOwnRepositories,
TestFoundationBuildsReadRPMFileDigests, TestAnotherMinorStillRefuses and
the collection's `test_each_qualified_fedora_release_reads_its_own_primary_key`.
TestNativeCurrentOSResolution now qualifies whichever admitted Fedora release
the host runs.

**Gates:** `make check` passes: fmt-check, modules-check, tidy-check, test,
vet, completion-test, `make vulncheck` (no reachable vulnerability) and
`./scripts/ansible-check` (ansible-lint production profile, sanity, and
297, 1856, 1286 and 3440 tests passed). `make quick` and `make docs-check`
pass, and the GCC-major test fails with that match removed. On the owner's
Fedora 44 host, unprivileged, TestTheResolverStagesTheQualifiedFoundation
runs and passes against every pinned file, and the collection's libdnf5 5.4.6
snapshot, setup and inventory ran under the host's Python 3.14. The B547
follow-up ran the resolve against the Fedora 44 repositories and the presence
check of its roots there, unprivileged (`BOOTWRIGHT_NATIVE_RESOLVE_QUALIFY=1`,
`BOOTWRIGHT_NATIVE_INSPECT_PLAN`), both passing after the fix, with
`./scripts/ansible-check --suite units` (1859 passed) and `--suite lint`,
`make quick` and `make docs-check`. Not run: `setup`, the controller stage and
`make race`.

**Digest and version effects:** Fedora 43's and RHEL 9.8's execution
foundations and bundle identities are unchanged. The collection change moves
the automation digest, so the next `setup` reprojects the bundle, and a live
context continues after it (D145). Moving a host from Fedora 43 to Fedora 44
changes its glibc and libgcc upstream versions, the update before which the
hold procedure (X88 removed it)
has the host's operations complete or its contexts destroyed, then sets it up
with a build that pins the new builds, as X84 does. **Constraints left behind:** Fedora 44
awaits its first [acceptance ledger](../../docs/acceptance.md#ledger) run of
setup and the controller stage; in-tree, its repository resolve and root
presence ran on Fedora 44 unprivileged (B547), not a package installation. [B542](backlog.md#retired), parked then and retired as moot by X89: a Fedora plan recorded
libdnf5's version without its first part.

### X85 — Machine commands across several machines

**Owner:** Machine, with CLI. An out-of-sequence product slice, integrated on
main as one commit at the owner's explicit request of 2026-10-14 (D150).
**Items:** B547. **Decisions:** D150. **Integrated:** "feat(machine): run
start, stop, restart and exec on several machines".

**Outcome, B547:** `--name` of `machine start`, `machine stop`, `machine restart`
and `machine exec` is a comma-separated list under the
[list rules](../cli.md#parsing-and-input-conventions), and the command applies
to every listed Machine at once. One member after normalization behaves as
before; a value that resolves to no member is `cli.usage`, and `machine rsh`
refuses a comma in its single `--name`. Every Machine resolves in the
context's effective graph before anything is confirmed, lent or run, and the
unknown or excluded ones fail `access.target` in one diagnostic naming each.
The power verbs ask one confirmation for the batch and run each Machine as the
single-Machine operation, concurrently and in canonical order, at most
`lifecycle.MaxRunningBlocks` at a time, inside one invocation scope that the
Ansible runner's `lifecycle.adapter-running` refusal admits as siblings. One
Machine's failure cancels no other; every admitted Machine has a result row,
the power JSON result is one shape for any count (`context`, `verb`,
`machines`), and a partial failure is one envelope with the rows and a
diagnostic for each failed Machine
([operations](../cli.md#machine-power-operations),
[results](../cli/output.md#machine-results)).
The exec proves every identity and, one Machine at a time in canonical order,
every host key before any session opens, lends the union of their credentials
once, and runs the sessions together with no standard input and a client told
never to prompt, each output line prefixed `<machine>: ` and written whole. A
nonzero session writes an `access.exit` warning, and the invocation exits with
the highest status ([sessions](../cli.md#machine-ssh-sessions)).

**Guard tests:** `internal/machine/power`:
`TestSeveralMachinesRunConcurrentlyInOneScope`,
`TestABatchRunsAtMostMaxRunningBlocksAtOnce`,
`TestABatchWithoutAScopeRunsOneMachineAtATime`,
`TestABatchAsksOneConfirmationNamingEveryMachine`,
`TestAListNamingNoMachineIsAUsageRefusal`,
`TestEveryUndeclaredMemberIsNamedInOneRefusal`,
`TestARefusingMemberRunsNothingOnAnyMachine`,
`TestAFailedMachineLeavesTheOthersSettled` (a run's own deadline included) and
`TestCancelingABatchStopsEveryRun`; `internal/machine/access`:
`TestOneMemberIsTheSingleSessionUnchanged`,
`TestNamesThatResolveToNoMemberAreAUsageRefusal`,
`TestSeveralSessionsRunConcurrentlyAndNonInteractively`,
`TestSeveralSessionsShareOneLendAndCarryTheirOwnCredentials`,
`TestEveryUnknownMemberIsNamedInOneRefusal`,
`TestARefusalForAnyMemberOpensNoSession`,
`TestHostKeysAreProvedOneAtATimeBeforeAnySessionOpens`,
`TestAnInterruptBeforeTheSessionsOpenStartsNone`,
`TestSessionStatusesAggregateWithWarningsInCanonicalOrder`,
`TestASessionThatCouldNotStartCountsAs255`,
`TestSeveralSessionsWritePrefixedLinesToTheirStreams`,
`TestConcurrentSessionLinesNeverInterleave`,
`TestAnUnterminatedLastLineIsWrittenWhenItsSessionEnds` and
`TestALineReaching64KiBIsWrittenAsItsOwnLine`; `internal/machine/sshlocal`:
`TestOnlyABatchSessionForbidsTheClientToPrompt`; `internal/cli`:
`TestOnlyTheCommandsThatActOnSeveralMachinesTakeANameList`,
`TestAMachineNameThatResolvesToNoMachineIsAUsageError`,
`TestSeveralMachineNamesReachTheServiceNormalized`,
`TestSeveralSessionsExitWithTheStatusTheServiceAggregated`,
`TestSeveralMachinesHeadlineTheirChange`,
`TestAFailedBatchWritesItsRowsToStandardOutputAndItsDiagnosticsToStandardError`,
`TestAFailedBatchIsOneEnvelopeWithItsResult`,
`TestAFailureWithNoSettledBatchWritesDiagnosticsOnly` and
`TestACanceledBatchNamesEveryRunsOutputInItsFailureEnvelope`, with the
`cli-machine-stop-several` goldens (text, JSON, and their failed forms).

**Constraints left behind:** power runs beyond `lifecycle.MaxRunningBlocks`
wait for a slot. A several-Machine exec has no standard input and cannot use
password authentication, so a Machine that signs in with a password is reached
alone, even with a key offered, because a password identity never tries an
offered key ([B544](backlog.md#b544)). The output lines of several sessions are
interleaved by line, never within one, and carry no ordering between Machines.
A batch of more than 16 Machines can retire the retained output of its own
earliest runs before its result names them ([B545](backlog.md#b545)).

### X86 — Cluster commands across several clusters and nodes

**Owner:** Environment access, with CLI. An out-of-sequence product slice,
integrated on main as one commit at the owner's explicit request of 2026-10-14
(D151), following [X85](#x85--machine-commands-across-several-machines).
**Items:** B546. **Decisions:** D151. **Integrated:** "feat(cluster): take cluster and node lists in exec and info".

**Outcome, B546:** `--name` and `--node` of `cluster exec` are
comma-separated lists under the
[list rules](../cli.md#parsing-and-input-conventions), and so is `--name` of
`cluster info`; `cluster rsh` stays single and refuses a comma in either flag.
A value that resolves to no member is `cli.usage`. `cluster exec` resolves the
listed clusters in canonical order through the existing checks, each applied
across every member before the next: every unknown name in one `access.target`,
then the first external StorageCluster, then the first OpenShift or OKD
ContainerCluster. Each cluster contributes its first node, or the `--node`
members it declares, matched by exact name and then by exact effective FQDN; a
member no listed cluster declares, and a cluster declaring none of the members,
each fail `access.target` in one diagnostic naming every offender. The resolved
`machineRef`s, deduplicated, reach the Machine session service as one list, so
one Machine is today's single session and several run the several-Machine
session, each output line prefixed by the Machine name
([cluster sessions](../cli.md#several-clusters-and-nodes)). `cluster info --name`
selects exactly the listed clusters and names every unknown member in one
`access.target`; `--secrets` still reveals one cluster and is `cli.usage` for a
list of several ([dashboard access](../cli.md#storage-dashboard-access)).

**Guard tests:** `internal/environment/access`:
`TestClusterExecHandsEveryResolvedMachineToOneSessionCall`,
`TestClusterExecRefusesOnceNamingEveryOffender`,
`TestClusterExecSingleRefusalsKeepTheirMessages` and
`TestClusterRshStaysOneSessionOnTheSelectedNode`;
`internal/environment/inspection`:
`TestClusterInfoSelectsExactlyTheListedClusters`,
`TestClusterInfoNamesEveryUnknownMemberInOneRefusal` and
`TestSecretsRevealsOneClusterNeverSeveral`; `internal/cli`:
`TestOnlyTheClusterCommandsThatSelectSeveralTakeLists`,
`TestAClusterListThatResolvesToNoMemberOrToSeveralWhereOneIsMeantIsAUsageError`
and `TestClusterListsReachTheServicesNormalized`; `cmd/bootwright`:
`TestClusterExecOnSeveralNodesRunsEachMachineOnceTogether`.

**Constraints left behind:** a node name or FQDN is matched in every listed
cluster, so a name two clusters share selects a node in each. A listed cluster
that declares none of the `--node` members refuses the invocation instead of
running nothing there. The Machine behind every resolved node is subject to the
several-Machine exec rules of X85, so a Machine that signs in with a password is
reached alone ([B544](backlog.md#b544)).

### X87 — Controller setup v2 foundations

**Owner:** Controller setup, with Controller (privilege) and Workspace. An
enabling slice of [M1](m1.md), the first of controller setup v2 (X87 to X90),
at the owner's request of 2026-10-14 (D152), integrated on local main as one
commit by the session ahead of the owner's review of D152, as its workflow
directed. **Items:** B548, B550, B551 and B553 (with parts of B549 and
B552, whose rest is planned for X90). **Decisions:** D152, pending the
owner's review. **Integrated:** "feat(controller): lay the setup v2
foundations".

**Outcome, B548:** before sudo, the supervisor refuses `runtime.privilege`
when the re-executed Bootwright file or any directory above it is owned by a
UID other than 0 or the invoking account's, or is writable by group or others.
A root-owned sticky directory such as `/tmp` is exempt, because the sticky bit
stops another account from replacing an entry it does not own; a sticky
directory another account owns still refuses. The report names only the
reason that applies, and the directory when that is the culprit, with the
remedy `sudo install -m 0755 <resolved path> /usr/local/bin/bootwright`
([privilege](../cli.md)). It is X87's only behavior change.

**Outcome, B549 (part):** the pure package `internal/controller/runtimelock`
parses and validates `bootwright.controller.runtime-lock-v1`, digests it
domain-separated over its canonical re-encoding, and embeds
`internal/controller/runtimelock/lock.json`. `scripts/runtime-lock`, a POSIX
sh wrapper over the stdlib-only `scripts/tools/runtime_lock.py`, offers
`generate`, `verify` and `outdated`, and `.github/workflows/runtime-lock.yml`
runs `verify` when the lock or the tool changes and weekly, and `outdated` as
an advisory summary. The first production lock, ansible-core 2.21.5 for the
Python ABIs 3.14, 3.13 and 3.12, was generated by the session rather than the
owner, so `scripts/tools/ansible-requirements.txt` moved its ansible-core pin
from 2.21.4 to 2.21.5 to keep one version everywhere. The rest of B549 (the
owner's `verify` run, `runtime-lock-v1`'s classification when B555 wires the
lock, and X90's offline tooling) stays on [M1](#x93--the-owner-run-of-2026-10-14).

**Outcome, B550:** one Go table in `internal/controller/prerequisites` names
the controller's host packages by name or RPM capability (baseline
`openssh-clients` and `podman`, `python(abi) = X.Y` only for an interpreter
to install, and the libvirt client, hypervisor and installer-media closures;
nmstate gone), behind a new `HostPackages` port that the new, unwired
`internal/controller/hostdnf` adapter implements: presence through
unprivileged rpm, a root `dnf install --assumeno
--setopt=install_weak_deps=False` preview kept as display text bounded to 64
KiB that refuses a no-match and a resolution failure naming the requirement
and notes core-package lines, and a root `dnf install --assumeyes` with the
same option followed by a presence recheck whose evidence names each
provider's NEVRA and repository. rpm and dnf are qualified system
executables, each call gets a clean environment and only the admitted proxy
route, and no call passes `--allowerasing`, `--nogpgcheck`,
`--repofrompath`, `--enablerepo` or `--releasever`. Two deviations from the
design's commands: rpm 6.0.2 reads a whole `name = EVR` string as a provide
name, so a versioned requirement queries the bare name with the providers'
`PROVIDENEVRS` and counts only a provider listing that exact provide, and the
grammar admits only `=`; rpm 6.0.2 rejects `%{FROM_REPO}`, so the repository
comes from `dnf repoquery --installed`.

**Outcome, B551:** the new, unwired `internal/controller/venvlocal` adapter
behind the new `prerequisites.PythonRuntime` port selects the receipt's
interpreter when it still qualifies, else the newest locked ABI at
`/usr/bin/python3.X`, walking ownership and write bits over the interpreter,
its target, the standard library and `lib-dynload`, probing it under `-I -B`
and checking the lock's glibc floor; fetches locked wheels into a
sha256-keyed cache, at most four at once, with no request on a hit; builds the
venv in a store stage, installs with pip `--no-index --no-deps
--require-hashes --only-binary` under a clean environment, compiles
unchecked-hash bytecode, refuses a `.pth`, `sitecustomize` or
`usercustomize`, probes the locked versions and writes `runtime.json` before
publication; and its launch guard holds the runtime shared for the whole block
and launches `bin/python -I -B` with a clean environment. A build that finds a
published runtime failing its probe retires and replaces it once.

**Outcome, B552 (part):** contextfs holds the setup subtree
`/var/lib/bootwright/setup/` beside the registry and outside its descriptor
([store layout](../contexts.md)): its lock, `state.json` in
`bootwright.setup-state-v1` (canonical, at most 256 KiB, pending intent
synchronized before effects, the receipt and the current and previous
runtimes), runtime stages published with one `syncfs`, a no-replace rename
and a synchronized parent, where a concurrent loser discards its stage, a
stage sweep, retirement under a non-blocking exclusive lock that skips a held
runtime, the newest 8 setup runs, and the refusal of another build's format,
which setup discards after confirmation once every runtime lock is claimed.
The store ports live in `prerequisites` and `setup-state-v1` is classified as
a self-healing store format. Retention, wheel pruning and the cache-only
rebuild went with
[X90](#x90--controller-setup-v2-retention-and-offline-sources).

**Outcome, B553:** the staging root `/var/lib/bootwright-staging` moved from
nativelocal to the new `internal/controller/stagelocal` package with unchanged
behavior, and composition takes it from there. Its new, unwired
`AutomationStager.Stage` extracts the embedded `ansible/` tree into
`automation-<32 hex>/tree` under the root 0700 staging root, compares the
read-back tree byte for byte, with its directories, count and modes, with
`ansible.Automation()`, the preimage of the automation digest, and holds an
exclusive lock a descendant inherits; the sweep removes only trees whose lock
it can take, and nothing staged is executable.

**Guard tests:** `internal/controller/privilege`:
`TestReexecutionRefusesAGroupWritableExecutable`,
`TestReexecutionRefusesAnExecutableOwnedByAnotherAccount`,
`TestReexecutionRefusesAWritableParentDirectory` and
`TestReexecutionAcceptsTheInvokersOwnBinary`;
`internal/controller/runtimelock`: `TestTheEmbeddedLockIsWellFormed`,
`TestTheLockCarriesOneAnsibleCoreEverywhere`,
`TestEveryLockedURLIsOnFilesPythonhosted`,
`TestEveryABICarriesItsOwnBinaryWheels`, `TestTheGlibcFloorAdmitsRHEL9`,
`TestTheLockDigestIgnoresFormatting`,
`TestParseRefusesWhatTheBuildCannotInstall`,
`TestTheRuntimeLockToolRoundTrips` and `TestTheRuntimeLockToolUnitsPass`;
`internal/controller/prerequisites`:
`TestTheBaselineNamesSshAndPodmanAndNoNmstate`,
`TestTheStageSelectsOnlyWhatTheCapabilitiesNeed` and
`TestTheRequirementGrammarAdmitsNamesAndCapabilitiesOnly`;
`internal/controller/hostdnf`: `TestPresenceIsRpmWhatprovidesWithoutVersions`,
`TestAVersionedCapabilityNeedsAProviderListingItExactly`,
`TestThePreviewIsBoundedDisplayText`, `TestAPreviewNoMatchNamesTheRequirement`,
`TestThePreviewNotesCorePackagesWithoutRefusing`,
`TestInstallNeverPassesAllowErasingOrNoGpgcheck`,
`TestInstallRechecksPresenceAndRecordsProviderAndRepository`,
`TestDnfGetsACleanEnvironmentAndOnlyTheAdmittedRoute` and
`TestAnUnsafeDnfExecutableIsRefused`; `internal/controller/venvlocal`:
`TestSelectionPrefersTheReceiptsInterpreterThenTheNewestABI`,
`TestAnInterpreterBelowAWritableDirectoryIsSkipped`,
`TestTheStdlibDirectoriesAreWalked`, `TestACacheHitMakesNoRequest`,
`TestAWheelWithAnotherHashIsRefused`, `TestTheVenvHoldsNoPthOrStartupHook`,
`TestBytecodeIsUncheckedHash`, `TestRuntimeJsonIsWrittenBeforePublication`,
`TestABuildReplacesAPublishedRuntimeThatFailsItsProbe` and
`TestTheGuardHoldsTheRuntimeSharedForTheBlock`;
`internal/workspace/contextfs`:
`TestTheSetupSubtreeIsOutsideTheRegistryDescriptor`,
`TestPendingIntentIsDurableBeforeEffects`,
`TestAnotherBuildsSetupFormatRefusesALaunch`,
`TestRetirementSkipsARuntimeALaunchHolds`,
`TestDiscardRefusesWhileALaunchHoldsARuntime`,
`TestConcurrentPublishersLeaveOneRuntime` and
`TestSetupKeepsTheNewestEightRuns`; `internal/controller/stagelocal`:
`TestStagedAutomationMatchesTheEmbeddedDigest`,
`TestNothingStagedIsExecutable`, `TestTheSweepSkipsALockedTree`,
`TestADescendantKeepsTheTreeLocked` and the moved staging tests;
`cmd/bootwright`: `TestEveryStoreFormatIsClassified`. Real-interpreter
venvlocal cases skip without a locked interpreter, and the mount-based sweep
tests skip without root.

**Gates:** in the x87 worktree after merging main 6e2a51d94, as its commit
records: `make check` passes (fmt-check, modules-check, tidy-check, test,
vet, completion-test, vulncheck with no reachable vulnerability,
ansible-check with lint 0 failures, sanity 24 of 24 and units 1859 passed and
1 skipped, and `git diff --check`); `make race` and `make docs-check` pass;
per lane, `make quick`, the package tests, `-race` on the new packages and
`scripts/runtime-lock verify` against PyPI passed. Not run: anything needing
root.

**Digest and version effects:** none for a live operation. The automation
digest, every implementation's contract and content digest, the execution
foundations and bundle identities are unchanged, since no file under
`ansible/` moved and nothing wires the lock, the venv, the setup subtree or
the automation stager. The only new persisted format, `setup-state-v1`, is
written by nothing yet. Running a build from a path another account can
write now refuses before sudo. **Constraints left behind:** setup and every
launch still use the bundle machinery until X88 (B554 to B556), which needs
D152's acceptance and its clause 9 choice. `venvlocal.Store` and the contextfs
setup transaction differ (`PublishRuntime` takes a stage path and returns an
error in one, a `RuntimeStage` and a published flag in the other, and
`RetireRuntime` returns a retired flag only in contextfs), so the wiring in
X88 reconciles them. ansible/controller/requirements.txt, the old bundle's
input, still pins ansible-core 2.21.4 until X89 deletes it; a shared
`.cache/ansible-check` venv built at 2.21.4 refuses until it is rebuilt.
specs/security.md does not yet state B548's rule or its sticky-directory
exemption; B555's security rewrite does. `controller/hostdnf` sits in the
architecture's port-test-support row only until B554 wires it. S0, the
owner's baseline measurement, is still pending.

### X88 — Controller setup v2 cut-over

**Owner:** Controller setup, with Controller (clients), State reconciliation,
Workspace and CLI. A product slice of [M1](m1.md), the second of controller
setup v2 (X87 to X90), integrated on local main as one commit by the session
on 2026-10-14 without a push. D152 still awaits the owner's review, and its
clause 9 sequencing choice was deferred by the owner on 2026-10-14
("implement whatever you can; I will test when I can run"), so the session
integrated X88 ahead of both. **Items:** B554 and B556 (with B555, whose
in-tree gates pass and which awaits the owner's fresh-install acceptance, and
a part of B549's remainder). **Decisions:** D152, pending the
owner's review. **Integrated:** "feat(controller): cut setup over to the OS
Python venv and host dnf".

**Outcome, B554:** the controller stage no longer builds a frozen native
transaction from setup's definition. At plan time it reads which of the
selected client closures' host package requirements are missing through
rpm alone, so a ready host runs no dnf; at effect time, inside the attempt's
existing intent record, it runs the root `dnf install --assumeno` preview and
the `--assumeyes` install with `install_weak_deps=False` through the
`hostdnf` adapter, now wired, on the stage's frozen proxy route, and then
records each provider's NEVRA and the repository `dnf repoquery --installed`
names. The role receives no native plan, the client request moves to
`controller-clients-v3` and its golden moves. A RHEL host installs `lorax` and
`xorriso` from whatever its entitled repositories provide, which supersedes
D106 and closes B330, an entitled-source adapter no longer needed.

**Outcome, B555 (in-tree):** setup inspects the host (family and floors, the
locked-ABI interpreter, host packages and the published runtime), reports a
ready host with no request and no write, and otherwise plans with dnf's own
preview, confirms, records pending intent in the setup subtree, installs host
packages, builds and publishes the venv runtime from the embedded lock and
writes the receipt; an interrupted setup is re-inspected after confirmation,
and another build's setup format is discarded after confirmation. The Python
execution guard is the venv launch with no release callback, behind one
prerequisites port that reconciles `venvlocal` with the contextfs setup
transaction. An operation locates this build's runtime once from the setup
receipt and the embedded lock: no receipt, a pending setup or an unpublished
runtime refuses `controller.state` "the controller runtime for this build
(lock …) is not prepared" with `run bootwright setup`. The closure an
operation freezes is `execution-closure-v2` over the lock digest and the ABI,
so a same-lock rebuild and a Python z-stream update continue a live operation,
and a lock change refuses naming the build whose runtime the host keeps. The
Ansible runner stages the embedded automation once per invocation into the
root 0700 staging root, passes its lock to the run and releases it at the end,
so an `ansible/`-only fix is build and apply. The controller prerequisites
role loses its native and inventory tasks and its request moves to
`controller-prerequisites-v6`; the supervisor asserts the venv. Setup's ready
path and `preflight controller` print one table of host, runtime and packages
with provider and origin, with the informational lines
`controller.untested-release` and `controller.loader-preload`; `setup` takes
`--dry-run`, `--yes` and `--output json` with `--dry-run`, which lists every
locked wheel's file, URL and SHA-256, and `--purge-old-bundles` is gone. The
merge of [X91](#x91--read-only-diff-and-the-delta-seam) moved `diff
--observe` onto the same located runtime. specs/controller.md,
specs/security.md (the attacker model, W1 to W4 and B548's rule with its
sticky-directory exemption), specs/cli/commands.md, specs/contexts.md,
specs/contexts/controller-record.md, specs/architecture.md and
specs/state-reconciliation.md state the cut-over, with the real rpm and dnf
commands X87 recorded as deviations. It closes B165: automation is no part of
any setup identity.

**Outcome, B556:** the controller record moves to version 3: it keeps host
identity, bindings, reservations and the tool sources and areas, each binding
names the tool closure its last stage published, and the receipt, retained sources and definitions and
execution bundles leave it. The registry moves to version 6; a version 5
store refuses before any preview with D152's clause 9 message and remedy. No
converter or reader of an old record is written.

**Outcome, B549 (part):** `runtime-lock-v1`, `runtime-v1` and
`execution-closure-v2` are classified in
`cmd/bootwright/contract_versions_test.go` as runtime-identity store formats
now that the receipt persists the lock digest. The owner's `verify` run,
`--wheel-mirror` and `setup --download-only` stay on [M1](#x93--the-owner-run-of-2026-10-14) for X90.

**Guard tests:** `internal/controller/prerequisites`:
`TestAReadyHostMakesNoRequestAndNoWrite`,
`TestThePlanShowsTheDnfPreviewVerbatim`,
`TestAnInterruptedSetupIsReinspectedAfterConfirmation`,
`TestAnUntestedReleaseIsInformational`,
`TestAnUnknownFamilyRefusesBeforeEffects`, `TestADerivativeIsAdmittedByIDLike`,
`TestNoLockedInterpreterAddsThePythonABIRequirement`,
`TestAGlibcBelowTheFloorRefusesBeforeEffects`,
`TestAnotherBuildsSetupStateIsDiscardedAfterConfirmation`,
`TestADeclinedPlanChangesNothing`, `TestAStoreTheSetupReadRefusesPreviewsNothing`,
`TestTheReadyPathRendersTheReceiptTable`, `TestPreflightIsReadOnlyAndNamesWhatIsMissing`
and `TestALoaderPreloadIsInformationalEverywhere`; `internal/controller/clients`:
`TestTheStageInstallsMissingClosuresThroughHostDnf`,
`TestApplyInstallsHostPackagesBeforeItPublishesTools`,
`TestAHostPackageFailureIsFailedBeforeTheInstallAndUnknownAfter`,
`TestALockedRereadSkipsWhatIsAlreadyInstalled`, `TestDnfReceivesTheFrozenRoute`,
`TestTheRoleCarriesNoNativePlan` and `TestTheStageRouteReachesDnfAsSetupsDoes`;
`internal/controller/hostdnf`: `TestInstalledReadsProvidersWithoutDnf` and
`TestPresentNamesOnlyThePresentProvidersAndTheirRepository`;
`internal/controller/venvlocal`:
`TestTheGuardReportsAnUnpublishedRuntimeAsUnavailable`;
`internal/reconciliation/ansiblerunner`:
`TestARunExecutesTheStagedAutomationAndHoldsItsLock`,
`TestAnInvocationStagesItsAutomationOnce` and
`TestAStagedTreeThisExecutableCannotRunIsRefused`;
`internal/reconciliation/lifecycle`:
`TestARegistrationFreezesTheRuntimesExecutionClosure`,
`TestASameLockRebuildContinuesALiveOperation`,
`TestAPythonZStreamUpdateContinues`,
`TestALockChangeRefusesContinuationNamingTheKeptRuntime`,
`TestAPendingSetupBlocksALaunch`, `TestALaunchWithoutAReceiptRefusesWithSetup`,
`TestAMissingRuntimeRefusesNamingTheBuildsLock`,
`TestARuntimeGoneAtLaunchRefusesWithSetup`, `TestAnOperationLocatesItsRuntimeOnce`
and `TestARemovalLocatesItsRuntimeOnce`; `internal/workspace/contextfs`:
`TestTheControllerRecordV3HoldsNoReceipt`,
`TestARegistryVersion5StoreNamesTheFreshInstallRemedy` and
`TestTheFirstBindingRefusesBeforeSetupCompletes`; `cmd/bootwright`:
`TestEveryStoreFormatIsClassified`,
`TestComposedBaselineDryRunReadsNoSetupState` and the moved
`TestEveryImplementationMovesItsContentDigestWithItsContract` goldens.

**Gates:** in the x88 worktree after merging main b81240b62 (X91), as its
commit records: `make check` passes (mod verify, tidy, `go test ./...`, vet,
the completion test, govulncheck with no vulnerabilities, ansible-check with
lint 0 failures in the production profile, sanity passing and units 1861
passed and 1 skipped, with 297, 1278 and 3437 further tests passed); `make
quick`, `make race`, `make docs-check` and `git diff --check` pass. Not run:
anything needing root.

**Digest and version effects:** the registry moves from 5 to 6 and the
controller record from 2 to 3; the closure an operation freezes moves to
`execution-closure-v2`; `controller-prerequisites-v6` and
`controller-clients-v3` move those implementations' content digests. A
fresh install is required: this build refuses a store an earlier build wrote,
so retire every live context with the build that applied or prepared it,
then, as root, remove `/var/lib/bootwright` and `/var/lib/bootwright-staging`
and run `setup` with this build. After that, an `ansible/`-only fix or any
rebuild with the same lock is build and apply with no setup.
**Constraints left behind:** X89 deletes the dead bundle machinery
(`internal/controller/bundlelocal`, the native solver code, the
controller-only Python native code, scripts/foundation-catalog and
ansible/controller/requirements.txt, which still pins ansible-core 2.21.4),
which makes B528 and B542 moot, and rewrites the operator guide, the
development guide and the knowledge pages that still describe bundles. X90
delivers retention, the cache-only rebuild, the wheel mirror, download-only
and B557's tool areas, which closes B322. Nothing was run as root: setup's
install path, the controller stage's dnf install and the mount-based tests
await the owner's fresh-install acceptance on Fedora 44 and a RHEL 9 VM
(B555), as do X87's S0 baseline and the owner's runtime-lock `verify` run.

### X89 — Controller setup v2 deletion and documentation

**Owner:** Controller setup, with Controller (clients), Workspace and
Architecture. A product slice of [M1](m1.md), the third of controller setup
v2 (X87 to X90), integrated on local main as one commit by the session on
2026-10-14 without a push, under the same deferral of D152's review and
clause 9 choice as [X88](#x88--controller-setup-v2-cut-over). **Items:**
B528 and B542, with B227, B231, B238, B263, B335, B429 and B527, each retired
as moot by the deletions (with the deletions and documentation that
[B555](#x93--the-owner-run-of-2026-10-14)'s constraints assign to X89, while B555 awaits the owner's
fresh-install acceptance, and B557's note on the deleted
harness). **Decisions:** D152,
pending the owner's review. **Integrated:** "refactor(controller): remove the
pre-v2 setup machinery and document setup v2".

**Outcome, deletions:** only what X88 left unreachable goes.
`internal/controller/bundlelocal` loses about 8,430 lines: the execution
foundation and its probe, the bootstrap, the compiled catalog and
`catalog.json`, the manager, metadata, the archive and tool projections,
`launch.go` and their tests; it keeps the tool areas' acquisition, transport
and trust, which X90's B557 moves, and its import grants narrow to them.
`internal/controller/prerequisites` loses 1,907 lines: the bootstrap, the
native plan, the qualified foundation, the definition, the resolution
contracts, effects, inspection and storage types and the tool projection.
`internal/workspace/contextfs` loses about 100 lines of execution-bundle
publication and its test, and `cmd/bootwright` the Go-to-Python closure parity
test of the deleted root table (about 100 lines). The collection loses about
1,060 lines net: `controller_native.py`, `NativeRefused` and its branches,
and the controller-only roots, dnf5 solve, apply and verify branches, Fedora
releases and key and entry points of `native_resolution`, `native_apply` and
`native_signatures`, which keep the dnf4 core the Ceph node worker loads; the
stale `controller_prerequisites` integration target goes and
`controller_native` becomes the RHEL-only `native_transaction` target.
scripts/foundation-catalog (112 lines) and ansible/controller/requirements.txt
(13 lines, still pinning ansible-core 2.21.4) go, the embed drops
`controller/` and `ansible.Documentation` goes. The `qualified_ansible` test
moves to `internal/controller/runtimelock` as the lock's statements test. In
all, about 12,600 lines are deleted and 830 added, most of the additions
documentation.

**Outcome, documentation:** the operator guide states setup v2's admission,
the untested-release line, what setup installs and from where, the dnf
preview, the venv, the fresh install across the registry version 5 refusal,
the dnf route and X90's offline preparation, and drops the execution
foundation hold and the Fedora 43 examples; its destinations list drops the
UBI and Fedora repositories, and the docs test reads the lock's wheel URLs.
docs/development.md drops the deleted harnesses; docs/acceptance.md gains
B555's [setup row template](../../docs/acceptance.md#setup-v2-fresh-install-row).
The knowledge pages controller-runtime-isolation.md and
controller-setup-resolution-cost.md are rewritten with the earlier design as
dated history, native-root-reason-change.md is marked history, and
concurrent-block-execution.md follows per-invocation automation. The setup
result's `execution-bundle` check keeps its ID and reads "Controller setup".
B227, B231, B238, B263, B335, B429, B527, B528 and B542 are
[retired](backlog.md#retired) as moot, B322 now names B557's tool-area
retirement, and D107 is marked superseded.

**Guard tests:** `test/architecture`'s boundaries (bundlelocal's grants
narrowed), comment ceilings (bundlelocal 316 to 30, prerequisites 524 to 288)
and `TestNoCodeReadsTheWholeEmbeddedTree`;
`TestDocsProxiedNetworkDestinationsMatchTheCode`;
`internal/controller/runtimelock`'s
`TestTheLintAndSanityConfigurationsNameTheLockedMinor`,
`TestTheDevelopmentGuideStatesTheLockedMinorAndABIs` and
`TestTheFloorLockPinsTheCollectionsRemotePythonFloor`;
`TestAProviderHostInstallsTheStagesHypervisorClosure`; and the collection's
`test_native_resolution`, `test_native_apply` and `test_native_signatures`
over the kept dnf4 core.

**Gates:** in the x89 worktree on main ace48daf1, as its commit records:
`make check` passes (mod verify, tidy, `go test ./...`, vet, the completion
test, govulncheck with no reachable vulnerability, ansible-check with lint 0
failures in the production profile, sanity passing and units 1852 passed and
1 skipped, with 297, 1278 and 3428 further tests passed); `make quick`, `make
race`, `make docs-check` and `git diff --check` pass. Not run: anything
needing root.

**Digest and version effects:** the collection changed, so the automation
digest moves; on a host set up by an X88 build that alone continues a live
context with build and `apply` (a `setup` changes nothing), since automation
is no part of any setup identity and every content digest hashes Go constants
only. A host an earlier build manages still needs X88's fresh install. No
contract version,
store format or closure moves, and no golden is deleted. **Constraints left
behind:** `internal/controller/bundlelocal` still holds the tool areas until
X90's B557 moves them under the controller's tools directory; X90 also adds an
operator-run harness for `playbooks/controller/setup.yml` in place of the
deleted target. X88's hostdnf and contextfs review nits stay for X90 where it
touches that code. Nothing was run as root, as for X88.

### X91 — Read-only diff and the delta seam

**Owner:** State reconciliation, with CLI, Workspace and Substrate. An
out-of-sequence enabling slice, integrated on main as one commit at the owner's
explicit request of 2026-10-14 (D153). **Items:** B564, B565, B566.
**Decisions:** D153. **Integrated:** "feat(reconciliation): add bootwright diff
and the delta seam".

**Outcome, B564 (the audit and the seam):** a reconciliation audit of `apply`
and `destroy` ([lessons](../../.agents/knowledge/reconciliation-audit-2026-10.md))
found two blocking defects outside the decision engine, parked as
[B567](backlog.md#b567) and [B568](backlog.md#b568), and the rest as
[B569](backlog.md#b569) to [B605](backlog.md#b605) beside the existing items it
names. The engine's frozen plan, closed records and re-proof under the
exclusive lock held. The slice adds `reconciliation.Compare` in
`internal/reconciliation/delta.go`, the pure comparison of two plans (added,
removed, changed and same blocks, in frozen then fresh order, with `First`,
`Objects` and `Count` over the result), and rebuilds the other-build proof on it
with its message and remedy unchanged. It reads a retained input revision under
the lock a lifecycle read already holds (`View.Revision`, see [storage, locking
and publication](../contexts.md#storage-locking-and-publication)), and makes the
changed-input refusal over a completed apply name what changed: the revision
repeating the frozen bytes, bytes that changed but declare the same objects, or
up to five objects with up to three field paths each, and the general message
when the frozen revision cannot be read or compiled
([lifecycle unit](../state-reconciliation.md#lifecycle-unit)). The decision
never changed: `inputDrift` is the one predicate that tells a revision change
from a bytes change. The slice adds no transition, record kind, record version
or content digest; the state-table rows are unchanged, six `apply done` cases
join `stateMachineRows`, and `TestApplyAdmitsOnlyTheThreeScenarios` pins where
each apply decision may appear. The design for one more scenario lives in
[reconciliation scenarios](../deferred/reconciliation-scenarios.md).

**Outcome, B565 (`bootwright diff`):** `bootwright diff [--context <name>]
[--observe]` compares the context's selected desired state with what its apply
realized, object by object, in text and JSON, with a unified line difference of
each object's canonical YAML (`applied/` or `real/` against `desired/`) under a
header that names the context, the operation compared against, the input, the
plan and the build
([comparison](../cli.md#desired-and-realized-state-comparison),
[results](../cli/output.md)). Without `--observe` it reads durable records only
and contacts no host
([comparing desired and real state](../state-reconciliation.md#comparing-desired-and-real-state)).
With it, the Secret binding the apply froze is reopened read-only, and each
realized block the apply started is read through its capability's `Observe`
alone, at most `Concurrency()` at once, under an execution that names no
operation, attempt or resolution, so the engine marks it read-only and
unrecorded; a controller-stage block gets a stage whose every mutator refuses.
A capability's `Projector` renders the evidence into spec fragments, the
declared side is pruned to the observed fields, and the fields no projector
claims are listed as not compared
([reading the real side live](../state-reconciliation.md#reading-the-real-side-live)).
A block this build plans differently from the build that froze it reads
unknown, an unknown is never read as absent, and a projection that carries
lent Secret material refuses the whole diff before any output. It exits 0
whenever it renders, refuses `lifecycle.lease` while an apply or destroy holds
the context ([B18](m1.md#b18)), and writes no record, run output or custody.
The first projector is the libvirt Machine.

**Outcome, B566 (the engine read-only guard):** `RunFor` derives a `ReadOnly`
and an `Unrecorded` mark on every adapter request from the execution, and the
Ansible runner refuses, before it resolves a playbook or creates a job, scratch
or material directory, a read-only request whose operation is not `observe` and
an unrecorded one that declares an output to take over
([attempts and unknown outcomes](../state-reconciliation.md#attempts-and-unknown-outcomes),
[lifecycle ports](../architecture.md#lifecycle-ports)). A keep marks the destroy
attempt's execution read-only, and the capability contract suite gains the
`observation-read-only` and `probe-read-only` properties with no known
deviation. A static test walks every `<implementation>/observe` playbook binding
and allows only tasks the read-only allowlist names, each with its reason. No
production call changed: every probe, resolution and keep already ran
`observe`, so the guard makes an inspection that cannot change a target a
property of the engine rather than of each call site.

**Guard tests:** `internal/reconciliation`: `TestCompareClassifiesEveryBlock`,
`TestCompareKeepsFrozenOrderThenFreshOrder`,
`TestFirstAgreesWithTheDifferingBlockItReplaces` and
`TestObjectsFoldsBlocksByObject`;
`internal/reconciliation/ansiblerunner`:
`TestAReadOnlyInvocationRunsOnlyObservation` and
`TestAnUnrecordedInvocationTakesOverNoMaterial`;
`internal/reconciliation/lifecycle`: `TestRunForDerivesReadOnlyAndUnrecorded`,
`TestEveryExecutionDeclaresItsReadOnlyness`, `TestAKeepRunsReadOnly`,
`TestDiffWithNoOperationReadsEveryObjectAbsent`,
`TestDiffOfACompletedApplyOverUnchangedInputShowsNoDifference`,
`TestDiffNamesAddedChangedAndRemovedObjects`,
`TestDiffOfAFailedApplyReadsItsFailedObjectUnknown`,
`TestDiffOfAnUnreadableFrozenRevisionReadsAppliedObjectsUnknown`,
`TestDiffUnderAnotherBuildListsTheBlocksItPlansDifferently`,
`TestDiffRefusesWhileAnOperationHoldsTheContext`, `TestDiffWritesNothing`,
`TestAnObservedDiffWritesNothingAndBindsNothing`,
`TestAnObservedDiffCallsOnlyObserve`,
`TestEveryInspectionIsReadOnlyAndUnrecorded`,
`TestAControllerStageInspectionCannotAuthorizeAnEffect`,
`TestAnUnreachableTargetRendersUnknownNeverAbsent`,
`TestMixedBlocksFoldUnknownBeforePartialBeforePresent`,
`TestABlockThisBuildPlansDifferentlyIsNotObserved`,
`TestAnObservedDiffRefusesWhenTheContextMovedWhileItRead`,
`TestAnObservedDiffRefusesAnUnreopenableBinding`,
`TestProducedMaterialIsClearedAndNeverRendered`,
`TestALentSecretInAProjectionRefusesBeforeOutput`,
`TestObservationsStayWithinTheConcurrencyBound`,
`TestCancellationStopsFurtherObservations`,
`TestAnInadmissibleProjectionReadsItsObjectUnknown`,
`TestAConflictingLeafIsOnlyANoteOnBothSides`,
`TestMergeUnionsAndExcludesConflicts`,
`TestPruneKeepsOnlyObservedLeavesAndNamesTheRest`,
`TestApplyAdmitsOnlyTheThreeScenarios`,
`TestEveryChangedInputOverACompletedApplyRefuses`,
`TestAChangedInputOverACompletedApplyNamesWhatChanged`,
`TestANamedChangeIsBoundedToFiveObjects`,
`TestANamedChangeNamesAtMostThreeFieldsOfAnObject`,
`TestABytesOnlyEditStillRefusesAndSaysSo`,
`TestAnUnreadableFrozenRevisionRefusesWithTheGeneralMessage` and
`TestAnUncompilableEditRefusesWithTheGeneralMessage`;
`internal/workspace/contextfs`: `TestARetainedRevisionReadsItsOwnBytes`,
`TestTheSelectedRevisionReadsAsTheViewsInputs`,
`TestARevisionReadRefusesAnInvalidOrMissingIdentity`,
`TestARevisionReadRefusesALinkedOrSubstitutedDirectory`,
`TestARevisionReadRefusesAManifestOfAnotherContextOrRevision`,
`TestARevisionReadRefusesATamperedBlob`,
`TestARevisionReadRefusesAfterItsCallbackReturned` and
`TestARevisionReadWritesNothing`; `cmd/bootwright`:
`TestEveryObservePlaybookRunsOnlyAllowedTasks`,
`TestEveryCapabilityHonoursTheCapabilityContract` (the two read-only
properties) and `TestAnUpdateOverACompletedApplyWarnsAndTheNextApplyRefuses`;
`internal/cli`: `TestDiffEscapesHostileNamesAndReasons`,
`TestDiffDecidesDifferenceFromEncodedBytes`,
`TestDiffNamesWhatAnObservedComparisonReadAndDidNotCompare`,
`TestUnifiedHunksOfMovedLinesAreMinimal` and
`TestUnifiedHunksBeyondTheCellBoundReplaceTheWholeChangedRegion`, with the
`cli-diff`, `cli-diff-json`, `cli-diff-none`, `cli-diff-unknown`,
`cli-diff-observe` and `cli-diff-observe-json` goldens and `help-diff`.

**Constraints left behind:** the audit's two blocking findings stay parked, and
a dedicated safety slice is recommended before any further `destroy` on a
shared libvirt host: [B567](backlog.md#b567), a libvirt provider-host removal
that stops a managed network other tenants' running domains use, and
[B568](backlog.md#b568), ownership by context name, which lets a recreated
same-name context adopt abandoned objects. Under `diff --observe` the Ceph
cluster and client blocks and the agent install and media blocks read unknown
until their observation runs without outputs
([B596](backlog.md#b596)). A read refuses `lifecycle.lease` while an apply
runs, and an `--observe` holds the shared lock so an `apply` refuses meanwhile
([B18](m1.md#b18)). A canceled `--observe` can leave a read-only Ceph worker
running to its own deadline ([B573](backlog.md#b573)). Only the libvirt Machine
has a projector: a Ceph pool, a bare-metal Machine, the managed services and
the artifact server, an OS tree and its install, the agent install, and the
Ceph image, host and core are the candidates [B600](backlog.md#b600) to
[B605](backlog.md#b605), and a present object no projector renders shows the
applied side with a note saying so. A libvirt disk resized by hand beside a
running controller reads unknown with the size refusal as its reason rather
than projecting the observed size ([B606](backlog.md#b606)). A newer build may not compile an old frozen
revision, and its applied objects then read unknown. A settled apply attests
only its own records ([D155](backlog.md#decisions) retains that policy and
retires B574).

### X90 — Controller setup v2 retention and offline sources

**Owner:** Controller setup, with Workspace, State reconciliation and CLI. A
product slice of [M1](m1.md), the last of controller setup v2 (X87 to X90),
integrated on local main as one commit by the session on 2026-10-14 without a
push, under the same deferral of D152's review and clause 9 choice as
[X88](#x88--controller-setup-v2-cut-over). **Items:** B552 and B557 (with the
remainder of B549 but the owner's `verify` run, and closing the parked B322).
**Decisions:** D152, pending the owner's review. **Integrated:**
"feat(controller): keep two runtimes, add offline wheel sources and retire
tool areas".

**Outcome, B552:** after each setup that writes its receipt, under
`setup/lock`, setup keeps the current runtime and the previous distinct one,
retires every other published runtime under a non-blocking exclusive lock on
its `.lock` (one a running command holds is kept and reported by the
informational `controller.runtime-retention` line; one whose `.lock` is gone
is retired), and prunes the wheel cache to the wheels every kept runtime's
`requirements.lock` names (which now also names the installer wheel its build
used), this build's locked wheels and its installer; setup runs are bounded to
the newest 8. A retention fault never changes the setup's outcome. A launch
(apply, destroy, the controller stage, a bounded run and `diff --observe`)
that finds only its runtime directory missing waits for `setup/lock` and
rebuilds it from the wheel cache with no network, confirmation or setup-record
change, adopting a runtime another launch published meanwhile; it refuses with
`run bootwright setup` when a wheel is missing, the interpreter no longer
qualifies or the receipt is incomplete, and a build that fails with every
wheel cached refuses naming the build's own cause. `status` never rebuilds. X88's
contextfs nits close where the code was touched: `HoldRuntime` checks the
digest `runtime.json` names, `RetireRuntime` retires a runtime whose `.lock` is
missing, the stage sweep admits any number of entries, and the concurrent
publishers test waits for the setup lock instead of spinning.

**Outcome, B549 (rest):** `setup --wheel-mirror` takes a `file:///` directory
or an `https://` base on the default port with no credentials, query or
fragment; the mirror is untrusted transport, read only for what the cache
lacks, opened without following a link or blocking on a pipe, and its bytes
are used only when the lock's size and SHA-256 match, so a missing, mismatched
or otherwise answered file refuses naming the file, the mirror and the remedy.
`setup --download-only --dest-dir DIR` runs unprivileged, reads no setup
record, runs no package manager and writes every locked wheel of every ABI,
the pip wheel and a byte copy of `lock.json` through verified partial files.
`setup --dry-run --output json` lists every locked file of the embedded lock;
with `--wheel-mirror`, the runtime action names the mirror and the listed URLs
stay the lock's. The operator guide documents the two-step offline
preparation. The owner's `verify` run stays on [M1](#x93--the-owner-run-of-2026-10-14).

**Outcome, B557:** tool areas move from `controller/bundles/` to
`controller/tools/<digest>/` under a bound of 16 of their own. Every
successful controller stage names on its context's binding the area it proved
(or none when it selects no tool), sealing an area an earlier attempt published
whole, then retires every sealed area no binding names except one the
operation holds open; `context delete` does the same once it drops the
binding, and a stage whose new area finds all 16 held retires the unnamed
areas before it refuses, with the remedy `bootwright context delete --name
<name> --purge`. Retirement publishes the entries as the new `retiring` mode,
removes each proven directory, syncs and publishes the record without them, so
an interrupted retirement is finished by the next. A retirement failure after
sealing leaves the stage unknown, and on a stage that changed nothing, failed.
It closes B322. The package `internal/controller/bundlelocal` keeps the tool
acquisition, transport and trust code it held. The opt-in
`controller_prerequisites` integration target returns as an operator-run
harness that sends one `controller-prerequisites-v6` request to the shipped
`playbooks/controller/setup.yml` and replays it
(`BOOTWRIGHT_ANSIBLE_CONTROLLER_TARGET=1` with `./scripts/ansible-check
--suite integration`, listed in docs/development.md); it needs no root or
network. The setup result's `execution-bundle` check keeps its ID.

**Guard tests:** `internal/controller/prerequisites`:
`TestRetentionKeepsTheCurrentAndThePreviousRuntime`,
`TestRetentionSkipsARuntimeInUse`, `TestAReadySetupRetiresNothing`,
`TestARebuildUsesOnlyTheWheelCache`,
`TestARebuildBuildsNothingWithoutEveryWheelCached`,
`TestARebuildIsRefusedWhereOnlySetupCanPrepareTheRuntime`,
`TestAFailedBuildIsARebuildFailure`,
`TestARebuildAdoptsARuntimePublishedMeanwhile`,
`TestAWheelMirrorIsAFileDirectoryOrAnHTTPSBase`,
`TestAMirrorEscapesTheFilenameItReads`,
`TestSetupReadsTheMirrorAndTheRuntimeActionNamesIt`,
`TestADryRunNamesTheMirrorAndStillListsTheLocksURLs`,
`TestAnInvalidMirrorRefusesBeforeAnyInspection`,
`TestDownloadOnlyTouchesNoHostPortAndElevatesNothing`,
`TestDownloadOnlyRefusesWhatItCannotDo` and
`TestDownloadOnlyWithoutTheLockIsUnavailable`;
`internal/controller/venvlocal`: `TestAFileMirrorWheelWithAnotherHashIsRefused`,
`TestAFileMirrorMissingAWheelOrHoldingTheWrongSizeIsRefusedWithARemedy`,
`TestAFileMirrorRefusesAPipeOrALinkNamedAsAWheel`,
`TestAnHTTPSMirrorWheelWithAnotherHashIsRefused`,
`TestAnHTTPSMirrorLackingAWheelIsRefusedNamingTheMirrorAndTheRemedy`,
`TestACachedWheelIsNeverReadFromTheMirror`,
`TestExportWritesExactlyTheLockedSetAndTheLock`,
`TestExportFromAnUnreadableCacheFetchesAndRefusesTamperedBytes`,
`TestARuntimeNamesTheWheelsItsBuildInstalled` and the live
`TestALiveBuildOnThisHost`; `internal/reconciliation/lifecycle`:
`TestAMissingRuntimeIsRebuiltFromCachedWheels`,
`TestAMissingWheelRefusesWithRunSetup`, `TestAFailedRebuildRefusesWithRunSetup`
and `TestStatusNeverRebuildsAMissingRuntime`; `internal/workspace/contextfs`:
`TestAStageRetiresTheAreaNoBindingNames`,
`TestAnAreaAnotherBindingNamesStaysUntilItsContextIsDeleted`,
`TestAStageWithoutToolsReleasesItsAreaAndKeepsUnfinishedOnes`,
`TestRetirementSealsTheUnsealedAreaItNames`, `TestAnAreaTheOperationHoldsOpenStays`,
`TestAnInterruptedRetirementIsFinishedByTheNext`,
`TestAClientAreaAtTheBoundRetiresUnnamedAreasFirst`,
`TestAwaitingSetupWaitsForTheHolderAndHonoursCancellation`,
`TestRetiringARuntimeWithoutItsLaunchLockRemovesIt`,
`TestAHoldRefusesARuntimeWhoseRecordNamesAnother`,
`TestTheSweepAdmitsManyRuntimeEntries` and `TestPruningKeepsOnlyTheNamedWheels`;
`internal/controller/clients`: `TestEverySuccessfulStageNamesItsAreaAndRetiresTheRest`
and `TestARetirementFailureKeepsTheStageOutcomeHonest`; `cmd/bootwright`:
`TestComposedDownloadOnlyStaysBelowThePrivilegeBoundaryAndReadsNoState` and
`TestTheDryRunJSONListsEveryLockedFileOfTheEmbeddedLock`; and the
`cli-setup-download` and `help-setup` goldens.

**Gates:** in the x90 worktree on main e01bf1de8, as its commit records:
`make check` passes (mod verify, tidy, `go test ./...`, vet, the completion
test, govulncheck with no reachable vulnerability, ansible-check with lint 0
failures in the production profile, sanity passing and units 1852 passed and
1 skipped, with 297, 1278 and 3428 further tests passed); `make quick`, `make
race`, `go test -race` over contextfs and `internal/controller/...`, `make
docs-check` and `git diff --check` pass; the live `TestALiveBuildOnThisHost`
passes on this Fedora 44 host (Python 3.14.8, 2.8 s); and the opt-in
`controller_prerequisites` harness passes. Not run: anything needing root.

**Digest and version effects:** no contract version, store format version or
closure moves and no content-digest golden changes; the collection change is
under `tests/`, which the automation digest excludes. The controller record
stays at version 3 with the added `retiring` mode, and the registry at 6.
**Constraints left behind:** tool areas moved from `controller/bundles/` to
`controller/tools/` without a version move, so a store an X88 or X89 build
populated with a tool area is not read by this build; nothing was run as root,
so no such store is known, and the fresh install covers one. Two X88 nits stay
open: hostdnf has no preview-timeout test, and `qualifiedSystemExecutable` is
duplicated in hostdnf and privilege. Nothing needing root was run: setup's
install path, retention and the rebuild on a real store and the owner's fresh-install acceptance on Fedora 44 and a RHEL 9 VM
([B555](#x93--the-owner-run-of-2026-10-14)) remain, with the owner's runtime-lock `verify` run
([B549](#x93--the-owner-run-of-2026-10-14)) and X87's S0 baseline.

### X92 — Setup v2 review findings

**Owner:** Controller setup, with Workspace, State reconciliation and CLI. A
defect slice of [M1](m1.md) under D152, integrated on local main as one
commit by the session on 2026-10-14 without a push: the adversarial review of
setup v2 (X87 to X90) confirmed twelve findings, two blocking; this slice
fixes them. **Items:** B607, created and completed in this slice for those
twelve findings. **Decisions:** D152, pending the owner's review.
**Integrated:** "fix(controller): settle the setup v2 review findings".

**Outcome, by finding** (severity; fix; guard test):

1. *Discarding a foreign setup subtree deleted the healthy runtime and the
   wheel cache, then the receipt named the deleted runtime* (blocking). While
   the subtree is another build's, inspection never adopts a runtime and
   counts every locked wheel as uncached, so the plan carries the `runtime`
   step and setup fetches, builds and publishes before the receipt names it.
   `TestDiscardingAnotherBuildsStateRebuildsAHealthyRuntime`
   (prerequisites) and `TestHealingASubtreeOverAHealthyRuntimeLeavesItLaunchable`
   (contextfs).
2. *On Fedora the missing-interpreter path asked dnf for `python(abi) =
   X.Y`, which Fedora's alternative interpreters never provide* (blocking).
   The baseline names the package `python3.X`, which every supported
   release's package of a locked ABI provides; hostdnf and the receipt read
   the ABI from that name. `TestAFedoraAlternativeInterpreterResolvesByName`
   (hostdnf, over a dnf5 preview fixture) and
   `TestTheBaselineNamesTheInterpreterPackageOnlyForAnInterpreterToInstall`.
3. *Setup readiness ignored the launch lock a launch requires* (minor), so a
   runtime without `.lock` looped between "run bootwright setup" and
   "Nothing to do". Runtime verification requires `.lock` as a regular file
   Bootwright owns, so setup retires and rebuilds such a runtime.
   `TestARuntimeWithoutItsLaunchLockIsNotReadyAndIsReplaced`.
4. *The setup store laundered cache-only rebuild refusals into a bare
   `context.state` storage error* (minor). The rebuilder keeps the refusal its
   transaction raised whatever the store reports.
   `TestARebuildOnTheRealStoreKeepsItsRefusal` (contextfs, on the real store).
5. *A continuation never attempted the cache-only rebuild* (minor) and
   refused with `run bootwright setup` first. The continuation's closure check
   locates the runtime as its run does, rebuilding a missing one, and an apply
   refused over an incomplete removal names that continuation; `status` still
   never rebuilds and offers setup. `TestAContinuationRebuildsAMissingRuntime`,
   `TestAnApplyOverARemovalWithAMissingRuntimeNamesItsContinuation` and
   `TestAContinuationMissingAWheelRefusesWithRunSetup` (lifecycle).
6. *The cut-over remedy and the operator guide named `context delete --name
   <name>` without `--purge`* (minor), which every build refuses. The remedy,
   specs/contexts.md and the guide add `--purge`.
   `TestARegistryVersion5StoreNamesTheFreshInstallRemedy`.
7. *The `controller.runtime-retention` line was never written* (minor): it is
   raised after the presented plan. The report counts the warnings the plan
   carried and the result writes the rest.
   `TestTheSetupPlanCarriesItsResolutionWarningsBeforeThePrompt` and
   `TestRetentionSkipsARuntimeInUse`.
8. *`setup --dry-run --output json` beside a state-root refusal wrote the
   human plan before the JSON envelope, which omitted the check* (minor). It
   writes one envelope with the plan's fields, `ok` false and the refusal in
   `diagnostics`. The `cli-setup-plan-state-root-json` golden.
9. *The output contract's result-fields table had no `setup` row* (minor).
   specs/cli/output.md gains it and controller.md links it; `make docs-check`.
10. *The lab-rhel README prescribed the pre-v2 setup-after-build and destroy
    road for an `ansible/`-only fix* (minor). It states rebuild and the same
    apply, setup only when the lock moves and removal only when a content
    digest moves; `make docs-check`.
11. *architecture.md assigned host-package installation to Ansible controller
    roles* (minor). Go owns host packages (hostdnf) and the runtime
    (venvlocal); Ansible owns target tool acquisition; `make docs-check`.
12. *`setup --download-only` could not run off the Fedora/RHEL family*
    (minor): the wheel client read only that trust bundle. Where the bundle is
    absent it falls back to Go's system pool, an unsafe bundle is still
    refused, and a host with no trust is refused naming `--wheel-mirror`;
    each trust refusal names its remedy.
    `TestAnAbsentFedoraBundleFallsBackToTheSystemPool`,
    `TestAnUnsafeBundleIsNeverReplacedByTheSystemPool` and
    `TestAHostWithNoTrustAtAllRefusesNamingTheBundleAndARemedy`.

**Gates:** in the x92 worktree on main 50aebb522, as its commit records:
`make check` passes (mod verify, tidy, `go test ./...`, vet, the completion
test, govulncheck with no reachable vulnerability, ansible-check with lint 0
failures in the production profile, sanity passing and units 1852 passed and
1 skipped, with 297, 1278 and 3428 further tests passed); `make quick`, `make
race`, `go test -race` over contextfs, `internal/controller/...` and
`internal/cli/...`, `make docs-check` and `git diff --check` pass; the live
`TestALiveBuildOnThisHost` passes on this Fedora 44 host (Python 3.14.8,
3.2 s). Not run: anything needing root.

**Digest and version effects:** none. No contract version, store or
setup-state format, closure, runtime lock or content digest moves, and the
collection is unchanged, so the automation digest stays. Behaviour changes
without a version: the baseline host requirement reads `python3.X` in the
receipts this build writes, within the same setup-state format, the
fresh-install remedy text adds `--purge`, and a refused `setup --dry-run
--output json` writes one envelope with its `result`, a new golden; no golden
is deleted. A host an X90 build set up continues with build alone.
**Constraints left behind:** nothing needing root was run; the owner's
fresh-install acceptance ([B555](#x93--the-owner-run-of-2026-10-14)) covers the foreign-subtree
discard and the `python3.X` install on a real host.

### X93 — the owner run of 2026-10-14

**Owner:** State reconciliation and Controller setup; run by the operator on
the qualified controller platforms from 2026-10-14 to 2026-10-15, on the clean
build `80f18c937`, which descends from
X44, X48 to X51 and X87 to X92. The owner accepted its rows on 2026-10-15.
**Items:** B19, B20, B270, B276, B277, B339, B549, B555. **Decisions:** D152,
D154. The three-node gate initially remained blocked by B608; X98 records
its subsequent acceptance. Environment details and exact evidence are retained
locally with the original ledger.

**The fresh install:** earlier effects and stores were removed before setup
v2. X87's S0 baseline could not be taken, as M1's
[plan](m1.md#controller-setup-v2) records.

**Outcome, B549:** runtime-lock verification and its CI workflow passed.

**Outcome, B555:** D154 accepted the fresh, ready and same-lock setup paths,
measured timing/cache budgets, controller readiness and applicable crypto-policy
checks. Exact measurements, package-preview exceptions and applicability notes
are retained locally.

**Outcome, B19, B20, B270, B276 and B277:** D154 accepted the
[lab-rhel journey](../../examples/machines/lab-rhel/README.md#run-it), including
service and machine access, clock and running-disk checks, restart, settled
replay, live-resource destroy refusal, stopped-resource removal, fresh apply,
host-restart recovery and absence checks. The JSON privilege cases returned
the required single documents and diagnostics without standard-error output.
Exact command history, results and log digests are retained locally.

**Outcome, B339:** interruption recorded the running block as `unknown`,
returned exit 130 with a usable terminal, and the next apply resolved it and
finished. The second-press path did not arise; D154 accepted the observation
as the gate. Platform-specific details remain local.

**Observed, B466:** the
[three-node journey](../../examples/machines/lab-rhel-3-nodes/README.md) proved
concurrent service and machine work, shared publication, overlapping
installations, settled replay and clean removal. Host-restart recovery failed
because the managed network remained inactive ([B608](#b608)); removal from
that state completed. That ledger row remains an observation. X98 combines
its accepted passing clauses with the subsequent restart journey.

**Constraints left behind:** the remaining B608/B466 gate and M1 completed
under [X98](#x98--m1-owner-acceptance). The run also found
[B609](backlog.md#b609), [B610](backlog.md#b610), [B611](backlog.md#b611) and
[B612](backlog.md#b612), parked under D48. Preview-capture evidence for
[B560](backlog.md#b560) and the original command history, outcomes and digests
are retained locally. The public ledger records the accepted qualifications.

### X94 — Post-destroy cluster inspection

**Owner:** CLI and Managed OS. A bounded defect slice of [M1](m1.md), the
first unfinished hardening milestone, authorized on 2026-10-16. **Items:**
B613, created and completed here; B476, acceptance of original X54 work.
**Decision:** [D155](backlog.md#decisions).

**Outcome:** `cluster info` after destroy reaches its existing unavailable and
custody handling. Host-key inspection checks that managed-OS evidence belongs
to a completed apply before decoding installation proof; removal evidence
supplies no host keys. Malformed or inconsistent proof from a completed apply
still refuses. The change neither mutates lifecycle state nor reveals
credentials, and changes no durable format or contract version.

**Guard tests:** `TestClusterInfoAfterDestroyReadsRemovalEvidenceAsUnavailable`
exercises the post-destroy inspection path;
`TestMachineHostKeysRequiresCompletedApplyEvidence` covers host-key selection
and malformed completed-apply evidence. **Gates:** `make quick`,
`make docs-check`, `make check` and `make race`; exact command results are
recorded in the delivering commit.

**Operator acceptance, B476 (original X54 work):** the owner accepted the
2026-10-16 clean-build `fcff2f58` ledger row under D155, explicitly confirming
the dashboard fingerprint comparison, successful login and expected host/OSD
inventory checks. The console evidence covers the required journey through
teardown and purge. Image/log digests and run details are retained locally.
B476's acceptance baseline is the commit whose `Refs:` line closes X70; this
row descends from it. B613's post-destroy inspection correction is qualified
in-tree; no live rerun of the correction is claimed.

**Constraints left behind:** X98 subsequently completed B608, B466 and M1.
B476 is complete; M2b's remaining core/prerequisite gates on
[M2](m2.md#m2b) stay open. B80's open definition and promotion
gates remain. D155's future reconciliation and shared-host safety directions
remain unimplemented; this correction authorizes none of them.

### X95 — Categorized examples and current runbooks

**Owner:** Documentation and CLI. An enabling slice of [M1](m1.md), explicitly
requested on 2026-10-16. **Item:** B614, created and completed here.

**Outcome:** the [examples index](../../examples/README.md) groups scenarios
under machines, infrastructure components, storage clusters, container clusters
and full platforms. A self-contained services-only graph fills the
infrastructure category. Every runbook covers its current prerequisites,
customization, supported commands, inspection, recovery and removal. The
multi-datacenter graph remains an expected-refusal authoring reference. Links,
diagnostic remediation and example discovery follow the categorized paths;
existing desired-state semantics are unchanged. The source-formatting reference
defines community YAML conventions and the repository's blank-line grouping;
all example YAML follows them, with a scoped yamllint configuration and pinned
lint instructions in the development guide.

**Guard tests:** `TestEveryExampleEffectiveStateMatchesItsGolden` compiles all
seven categorized scenarios, retaining the future-reference refusal assertions
and adding the services-only effective-state golden. `TestDocsExamplesAreIndexed`
requires each scenario's runbook and index entry. The command-catalog, README
certificate and artifact-journal checks continue to cover operator commands.
**Gates:** `make docs-check`, `make quick`, `make check` and `make race`; exact
results, pinned example YAML lint and parsed-equivalence checks are recorded
in the delivering commit.

**Constraints left behind:** no host lifecycle journey was run for this docs
change, and no operator acceptance or product capability is promoted. Existing
milestone gates and unsupported-feature refusals remain in force.

### X96 — Managed libvirt network startup

**Owner:** Substrate. A bounded defect slice of [M1](m1.md), authorized on
2026-10-16. **Item:** B608, delivered in-tree here; its operator gate and
B466 completed under [X98](#x98--m1-owner-acceptance).

**Outcome:** managed networks retain explicit `zone="trusted"` in libvirt
XML. Each provider with a managed network owns one bounded firewalld-readiness
oneshot and its exact `virtnetworkd.service.wants` link. It orders the actual
firewall readiness probe before network autostart, instead of treating
systemd's active state as readiness. A fresh probe also precedes current-apply
network realization. Firewalld must already be enabled and running; the role
does not enable it or change global daemon units. External-only providers
create no barrier. [Substrates](../substrates.md#provider-host-realization)
owns readiness, foreign-resource refusals and evidence requirements.

The original permanent-only bridge-binding proposal is corrected: libvirt
assigns its own default zone when XML omits one and reapplies that choice on
firewall reload. Retaining the explicit zone and waiting for readiness keeps
libvirt responsible for zone membership.

**Guard tests:** collection unit tests cover explicit trusted XML, bounded
readiness, fresh probing, already-enabled firewall prerequisites, external-only
providers, foreign unit/link/override refusals and interrupted cleanup. Go
request goldens freeze the provider's readiness-unit identity; host evidence
tests require owned active enabled completion and exact removal proof.
**Gates:** collection units and lint, `make quick`, `make docs-check`,
`make check` and `make race`; exact results are recorded in the delivering
commit.

**Upgrade boundary:** the libvirt host request advances from
`substrate-host-libvirt-v4` to `substrate-host-libvirt-v5`, with a new
automation digest and affected plan digests. Every live context must first be
retired with its applying build; it cannot continue across this change. This
change requires no setup v2 store reset.

**Constraints left behind:** this slice qualified the correction in-tree.
[X98](#x98--m1-owner-acceptance) subsequently records the completed restart
journey and owner acceptance, closing B608, B466 and M1. The earlier accepted
overlap, replay and cleanup evidence needed no repeat. M2b's M1 prerequisite
is now satisfied; its remaining gates stay open.

### X97 — Inherited service defaults

**Owner:** Substrate. A bounded B608 correction of [M1](m1.md), authorized by
the existing fix request. **Status:** delivered in-tree here; the remaining
B608/B466 operator gate completed under [X98](#x98--m1-owner-acceptance).

**Outcome:** readiness observation distinguishes the qualified operating
system's inherited vendor service defaults from foreign overrides of the owned
unit. The blanket `DropInPaths` refusal could reject completion after successful
native network and pool creation, and prevent cleanup of those partial effects.
The [firewall-readiness contract](../substrates.md#provider-host-realization)
owns the narrow vendor-directory trust boundary, effective unit invariants and
unchanged foreign-resource refusals. Shared defaults are read only and remain
outside Bootwright ownership.

**Guard tests:** collection units exercise completion, replay and removal with
inherited defaults, unchanged shared files, unsafe files and directory ancestry,
malformed paths, mixed vendor and unit-specific overrides, and incompatible
effective service settings. **Gates:** collection units and lint, `make quick`,
`make docs-check`, `make check` and `make race`; exact results are recorded in
the delivering commit.

**Recovery:** this automation-only correction changes no frozen request,
evidence, unit content, block content digest or runtime lock. A failed X96
operation can be recovered with its exact input and the corrected build; no
setup or store reset is needed. A changed desired state still requires taking
back the original operation's effects before importing the corrected input.
The actual restart run and owner acceptance subsequently completed B608,
B466 and M1 under [X98](#x98--m1-owner-acceptance).

### X98 — M1 owner acceptance

**Owner:** State reconciliation and Substrate. An acceptance record for
[M1](m1.md), authorized on 2026-10-16. **Items:** B466 and B608, completed.
**Decision:** [D156](backlog.md#decisions). **Tested build:**
`591af9616446596a77a599cd6303970a85131694`, source `clean`.

**Outcome:** the owner-accepted three-node scenario completed fresh apply,
controller host restart, automatic network and managed-service recovery,
explicit guest startup, access and service checks, safe stop/destroy, absence
checks, local evidence preservation and context purge. The earlier accepted
concurrency, shared publication, replay and cleanup evidence remains valid.
The owner accepted passing results in conversation and then requested the
records after the completed run; no separate hand-back form or approval is
required. The [acceptance ledger](../../docs/acceptance.md) records acceptance;
command history, runtime facts, operation-log digests, archive digest and raw
evidence are retained locally.

**Milestone completion:** M1 is done on 2026-10-16 at the tested build above.
It has no remaining item or planned slice. M2's M1 prerequisite is satisfied;
M2b retains its remaining core/prerequisite and B80 promotion gates. B476's
acceptance under D155 remains unchanged. This record completes no additional
M2 item or later milestone.

**Verification:** this slice changes delivery records only. Documentation and
repository gate commands and results belong to the delivering commit. The
functional implementation and guard tests remain in X48, X96 and X97.

#### B466

**Outcome:** independent services and same-profile installations run
concurrently. [X48](#x48--bounded-concurrent-execution) delivered the
implementation under D127. D125's operator gate is accepted under D156:
the earlier concurrency and replay evidence combines with X98's completed
restart journey. The following implementation and test map is retained from
the item.

The DAG already admitted independent infrastructure services and libvirt
machines, and the scheduler already implemented bounded concurrent execution.
Before X48, production nevertheless passed `Concurrency: 1` in
`cmd/bootwright/wiring_reconciliation.go`, and
`TestLifecycleConcurrencyBound` enforced it pending B303 and B304. A second,
independent limit was in `internal/managedos/installation/requests.go`:
`Request.ExclusiveKeys` froze the shared package-tree path on each entire
installation block, holding it through installer boot and completion.
Same-profile installations therefore serialized even with a higher production
bound. Wave width measures graph shape; it neither overrides the bound nor
accounts for exclusive resources.

Authorized on M1 by D127 after rebasing over main. The shared publication
realizes `MachineInstallProfile` under implementation/request
`os-package-tree-v1`, with evidence `os-package-tree-evidence-v1`;
installation requests advance to `os-install-anaconda-v8`.
[Managed OS](../managed-os.md#shared-package-tree-publication) owns
partial-effect, interrupted-removal, replay and old-plan contracts.

**What X48 delivered, and where each step is proved:**

1. B303 and B304 precede the bound: every role receives the runner's private
   scratch, durable machine work and libvirt provider media occupy disjoint
   `machines` and `providers` namespaces, and an invocation's own live jobs
   pass the running-adapter refusal while a foreign, unattributed or orphaned
   job still refuses. The runner's scope tests in
   `internal/reconciliation/ansiblerunner`, the role scratch and remnants unit
   tests, and the structural no-fixed-scratch rule with its self-tests prove it.
2. Managed OS owns one durable package-tree publication block per context and
   install profile with a hosted tree, in the `machines` stage; each
   installation requires it and holds no exclusive tree claim.
   `TestThreeInstallationsHaveOneSharedPublication` and the production-DAG test
   in step 5 prove one publication block with one exclusive key, which each
   installation requires and none holds.
3. Tree extraction, atomic publication, observation and removal moved to that
   owner, with its own evidence; inverse dependencies retain the tree until
   every consumer is removed. `TestTreeRequestAndEvidenceGoldens`,
   `TestTreeContinuationClassifiesPartialAndInterruptedRemoval`,
   `TestTreeOldRequestAndEvidenceVersionsRefuse`,
   `TestATreeApplyNamesItsRefusalsWithTheirRemedy` and the role harness in
   `test_managedos_package_tree.py` prove replay, continuation after partial
   publication, interrupted removal and the named refusals.
4. Production uses the engine's bound of eight with no public flag or
   desired-state field; `TestLifecycleConcurrencyBound` enforces it.
5. Production composition is qualified at three levels.
   `TestTheComposedInvocationScopeAdmitsSiblingRunsOfItsRunner` takes the
   composed options' invocation scope with a real runner and a controlled
   adapter, proves that two sibling runs are admitted while a run outside the
   scope refuses, and fails when the scope is unwired.
   `TestThreeNodeRHELExampleSharesOnePublicationAndAdmitsIndependentWork`
   compiles the three-node example through the production capabilities and
   checks which blocks are ready together, exclusive keys, selected stages and
   the inverse order.
   `TestThreeMachineWavesRunInOneInvocationWithPrivateScratch` runs each wave
   of a three-Machine apply and destroy through real adapter processes held at
   barriers. The cancellation case is
   `TestInvocationScopeClosesOnlyAfterEveryCanceledAdapterSettles`. The full
   lifecycle composition over the example with the real runner and controlled
   adapters is parked as [B495](backlog.md#b495), because D125's operator gate
   proves overlap on a host.

#### B608

**Outcome:** managed network startup waits for actual firewall readiness while
retaining the explicit trusted XML zone. [X96](#x96--managed-libvirt-network-startup)
delivered the bounded owned barrier, completion/absence evidence and frozen
request change; [X97](#x97--inherited-service-defaults) qualified inherited
vendor defaults and recovery without changing the frozen contract.
[Substrates](../substrates.md#provider-host-realization) owns the required
behavior. X98's completed restart journey and D156 close the operator gate.

**Constraints left behind:** B495's full composed lifecycle rehearsal remains
parked. M1 completion neither promotes parked safety work nor waives M2's
remaining gates. Runtime-specific observations and recovery details are
retained locally.

### X99 — Operator-selected Ceph artifacts and M2b completion

**Owner:** Storage and Desired state, with CLI. **Item:**
[B615](#b615), completed. **Decision:** [D157](backlog.md#decisions).
**Submilestone:** [M2b](m2.md#m2b), done on 2026-10-17 at
`7e3963d91851cc53bfaf54ed1962409a4051aca2`.

**Outcome:** authored release, package/tool and image coordinates reach native
consumers without a version allowlist or compatibility catalog. Native refusal
causes remain bounded and redacted. Independent artifact authenticity,
credential confinement, transport trust, ownership, authorization and frozen
continuation remain required. The [storage API](../api/storage.md) and
[storage lifecycle](../storage.md#bootstrap-identity-and-continuation) own the
contracts; this record adds no new admission rule or effect.

**Implementation and finding coverage:**

- `fa4b87d0f0fa028bb910c1e071d6cd08bc1dd5a2` delivered authored artifacts,
  aggregate local capacity/reserves, installed-host preflight, credential-confined
  acquisition, host/service continuation and owned partial-OSD proof for M2b.
- `6ba32fe7ecd149ac953560fabfa9f573a2acedef` keeps future guest prerequisites
  pending before installation; `ce97678499310d9adef008b6781a3d95e3811723`
  corrects the example's full native package release.
- Native TLS discovery, isolated error validation, manifest header handling,
  upstream refusal status and delegated blob delivery are corrected in
  `9ea35e12fd43aa48c0e254a817c03ad70d4f2203`,
  `6d9582453de7479f1c21001a143c220c743b4432`, `dd3bb18ae`, and
  `044688f38bdeb3fb0c2701d48c35e50b7511080c`.
  [Pull-boundary knowledge](../../.agents/knowledge/ceph-native-pull-boundaries.md)
  retains the generic causes and proofs.
- `082ae7cfa29c1d07e69bc338ace0dcd3a0b9219e` restores bootstrap-local
  container-engine initialization after read-only host admission.
  `288165004c6ed4cb6ca95031a744ea17708e768d` distinguishes owned inferred
  bootstrap services from foreign stored specifications; see the
  [service-projection proof](../../.agents/knowledge/ceph-bootstrap-service-projections.md).
- `7e3963d91851cc53bfaf54ed1962409a4051aca2` waits for dashboard publication,
  proves every manager's deployment port and retains a bounded exact-target
  repair plan with at-most-once attempts. Core request/content v6 owns the new
  persisted repair records; the original redeploy records remain unchanged.
  [Dashboard knowledge](../../.agents/knowledge/ceph-dashboard-port-publication.md)
  explains the ordering and completed-operation recovery boundary.

**Operator acceptance:** the [ledger](../../docs/acceptance.md) records the
separate negative-selection and successful authored-artifact journey. It also
identifies the explicit retrospective baseline designation for B615; advance
registration is not claimed, and future operator gates retain that rule. A
nonexistent image selection reached its native refusal before dependent host
or infrastructure effects. Successful native acquisition/execution, readiness,
RBD round trip, scoped client access, dashboard certificate/login acceptance,
safe stopped-host removal, owned absence, verified private archives, context
purge and retained-media verification passed. B476's earlier accepted
outage/recovery and continuation evidence remains valid. Exact environment,
coordinates, commands, operation identities, hashes and helper history remain
local and confidential.

The accepted journey used clean `aa7f4557efc582e134e47457d0aabb5e1369843a`
for the negative selection, clean
`082ae7cfa29c1d07e69bc338ace0dcd3a0b9219e` for the final apply, and clean
`288165004c6ed4cb6ca95031a744ea17708e768d` for continuation and retirement.
It includes explicit targeted dashboard recovery. This does not claim that
core-v6 automation was rerun as a fresh unattended deployment, or qualify every
version combination. Older registered operations retain their original build
for retirement when frozen contracts change.

**Verification:** the implementing commits record their exact gates. The final
product correction passed `make quick check race docs-check`, focused dashboard
regressions and independent safety/contract reviews. The original implementation
and earlier corrections also passed their recorded repository gates. Failed,
unrun and conditional skipped checks remain identified in their local records;
no synthetic check is presented as a live native run. This completion commit
changes records only; its own documentation and integration results are in the
commit body.

**Remaining scope:** M2c to M2e remain open; full M2 is not done. B315 to B317,
B494 and surviving-host removal retain their full-M2 gates. Parked
[B537](backlog.md#b537), [B616](backlog.md#b616) and [B617](backlog.md#b617)
remain registered follow-ups and do not reopen M2b; B618 was delivered later by
[X102](#x102--audit-defects-for-full-ibm-ceph-m2).
No later implementation or publication is authorized by this completion.

#### B615

**Outcome:** satisfy the [operator-selected version contract](../api/storage.md)
through native installation and execution. Added on 2026-10-16 at the owner's
request (D157); mandatory for [M2b](m2.md#m2b), delivered as
[X99](#x99--operator-selected-ceph-artifacts-and-m2b-completion).
**Owners:** Storage and Desired state, with CLI for native diagnostics. **Requires:** M2a's accepted
definition baseline and M1, satisfied under D156. **Implementation:** authorized by the owner on 2026-10-16. In-tree delivery
includes authored RPM roots (Ceph, cephadm-ansible and optional NFS-Ganesha),
repository selection, tag/direct-digest resolution, artifact-independent runtime
proofs and bounded redacted native diagnostics. The separate operator journey
below is accepted in the [ledger](../../docs/acceptance.md), closing B615 and
M2b. Earlier B476 acceptance remains a separate gate. **Acceptance baseline:**
`fa4b87d0f0fa028bb910c1e071d6cd08bc1dd5a2`, designated retrospectively under
the ledger's explicit B615 exception. Accepted clean builds and recovery
limits are recorded by X99.

Cover `ceph.release`, `ceph.packageVersion`,
`ceph.cephadm.ansible.packageVersion`, image repository/tag/digest selection,
derived tool versions and related service-version inputs, including
NFS-Ganesha. Version values are strings interpreted by native tools; remove
Bootwright's semantic version grammar as well as its version allowlists.
Each authored package version must select actual native package
acquisition, not function only as admission metadata. Define the missing
NFS-Ganesha selection surface here; NFS service/export orchestration remains
B478's work and is not implied by this package-selection requirement.

Remove the current release, tag, package, derived upstream version and source
hash allowlists throughout Go requests, Python admission, package acquisition,
image discovery, configuration and runtime preparation. A direct digest selects
its artifact independently of the old discovery tag. Neither a fixed host-OS
pin used as a release-compatibility rule nor equality between selected package
and image code may replace native compatibility handling. Authenticate each
selected artifact independently and freeze its acquired identity for continuation.
Native package, container and Ceph tools attempt the authored selection and
determine availability or compatibility; Bootwright surfaces their native
cause and selected coordinate without substituting a known version. Keep
structural validation, safe argument transport, secret redaction, publisher
and transport trust, ownership, authorization and continuation semantics.

The [storage API](../api/storage.md) defines the selection fields and native
consumers. Core v6, image v2 and package decision v2 freeze them; core v6 also
binds the dashboard's bounded port-repair records. Older
operations require their original applying executable for safe retirement. No IBM table
registry, version compatibility check, per-release profile or per-version
qualification gate is part of this work.

**Exit evidence:** boundary tests show unrelated, malformed, nonexistent and
deliberately incompatible version strings reach native commands unchanged, including Ceph
RPMs, cephadm-ansible, tags, direct digests and NFS-Ganesha package selection.
Tests use synthetic values rather than copying IBM's rows, verify that native
failures preserve their useful cause and redact secrets, and prove that retries
reuse resolved artifacts without refreshing tags or substituting versions.
An operator-run M2b journey demonstrates authored artifact acquisition and
execution plus an invalid selection's native error; it does not certify every
version or combination. Update the owning specs and examples with delivery.

### X101 — Version specializations and IBM Storage Ceph 9.9.0.x readiness

**Owner:** Storage, with Container clusters. An enabling slice of
[M2](m2.md), delivered on the owner's request of 2026-10-18 as one commit.
**Item:** [B626](m2.md#b626), whose in-tree gates pass; its operator gate is
the owner's three runs, so its row stays on M2 with Delivery `awaiting operator
acceptance`; the slice leaves the Planned slices table, as a delivered slice does. **Decision:** [D162](backlog.md#decisions), under D157.

**Outcome:** each component keeps one general flow, and a release difference in
native mechanics is a registered specialization selected from facts that pure
detectors read from the bytes of the operation's frozen, authenticated
artifacts. An unknown fact selects the general flow; a specialization never
approves, screens or substitutes a version, and never weakens a trust check.
[Architecture](../architecture.md#implementations-and-version-variation) owns
the rule. This slice delivers:

- The registry and its five records. Four describe IBM Storage Ceph
  `20.1.0-221`, which is not an IBM licensing build: the bootstrap without
  `--automatically-accept-license`, the MGR without the IBM license contract,
  opt-in Call Home, and `set-user` without `--skip-pre-steps`. The fifth is the
  `20.2.x` bootstrap license call. Committed per-build parser name lists back
  the detectors, and the registry ships in the guest bundle.
- The seams in the general flow: the bootstrap license flag, the license
  contract at enrollment and readiness, the Call Home model, the `set-user`
  pre-steps and the license-call refusal. The code asks a seam and never
  branches on a version.
- The [catalog](../../.agents/knowledge/version-specializations.md) of every
  record, its evidence, its seam and its test, the cases kept general, and the
  dependency rows that no code reads. `TestDocsVersionSpecializationCatalogMatchesRegistries`
  holds each registry and the catalog equal in both directions, and `make
  docs-check` runs it.
- Call Home checked at the cluster's claim against the selected build's model,
  and `ceph.ibm.callHome: disabled` admitted by planning (D162 clause (b)). The
  named `call-home-unqualified` and `native-option-undeclared` refusals are
  `lifecycle.unsupported`, and a refusal's coordinate may end with the ids of
  the specializations applied in that run.
- The bootstrap options checked against the anchored host parser before the
  attempt record is written, so a parser that lacks an option refuses before
  the native bootstrap runs; the host preparation, the cluster claim and the
  host authorizations remain for the operation's destroy to take back.
- The same-apply continuation of the `20.2.x` license-call refusal: the worker
  recognizes exactly that refusal in the private native output, proves the owned
  seed as the next apply would, and runs the general capture. Any other failure
  keeps its attempt record and the failed command.
- One general OpenShift control-plane roster of 1, 3, 4 and 5 masters for every
  release minor, so 4.22 and later plan and the installer judges the release
  (D162 clause (c)). No per-minor record exists yet.
- The [runbook](../../examples/storage-clusters/libvirt-3-nodes-ceph/README.md)
  for the owner's first runs of IBM Storage Ceph 9.9.0.3, 9.9.1.2 and 9.9.2.0.

**Decision D162 and its provenance:** the owner asked on 2026-10-18 for this
architecture and for readiness for IBM Storage Ceph 9.9.0.x to current,
OpenShift 4.21 to current, IBM Fusion 2.13 and Fusion Data Foundation 4.21 to
current. Clauses (b) to (d) are the design's recommendations taken under that
request, which the owner may revise. Clause (d) records provider OS and version
pairings, and Fusion, Data Foundation and OpenShift compatibility, as
dependency rows of the catalog that Bootwright does not gate on (D157).

Revised on 2026-10-19 under D163: the catalog's Dependencies table, the RHEL
pairing note and the runbook's release-to-OS table were removed; Bootwright
records no component version pairing or compatibility rule.

**Operator-visible effects:**

- `ceph.ibm.callHome` now follows the selected build. 9.9.0.3 (`20.1.0-221`, opt-in
  Call Home) takes `disabled`, its native default, and the IBM Call Home check
  expects `call_home_agent` not enabled; 9.9.1.2 and 9.9.2.0 (`20.2.x`) take
  `enabled`. The other value refuses `call-home-unqualified` at the claim, after
  host preparation, so a correction needs `bootwright destroy`, `context
  update` and a fresh apply. The refusal's remedy and the
  [storage API](../api/storage.md) say so. An earlier refusal is
  [B631](backlog.md#b631).
- A host parser that lacks a bootstrap option refuses `native-option-undeclared`
  before the attempt record, with the same destroy-first remedy.
- One apply is expected to complete 9.9.1.2 and 9.9.2.0. The byte shape of the
  `20.2.x` refusal is read from the signed packages and is unconfirmed on a
  guest; if it differs, the recognizer returns false and the next apply
  continues the seed as before.
- OpenShift 4.22 and later minors plan; 4.21 behaves as before.

**Digest effects:** no content digest, request version or contract golden moved.
The Ceph automation digest moved, and the Go changes (a request that admits one
more value, the topology rule) move no version or pin. A live operation therefore
continues after a rebuild and an apply, as
[a fix to a failed step](../../.agents/knowledge/continuation-after-a-fix.md)
states; a step that already wrote an attempt record and failed natively still
refuses `partial-cluster`. The specializations evidence member and its `cluster
info` line ride the next coordinated Ceph contract move ([B627](#x57--applied-specializations-in-ceph-evidence)).

**Review:** the nine lane items were each reviewed by two lenses, spec and
design conformance with test adequacy, and durability, continuation, digest
stability and fail-closed refusal. Sixteen verdicts approved; both lenses on
the Call Home admission blocked it, and its fix recorded a landing constraint
that the adapter lane then met. The adversarial review of the integrated diff
raised seven findings. Four were confirmed in scope, two of them blocking, all
on one false claim: that a corrected `callHome` continues the failed apply, when
the lifecycle refuses an input change over an incomplete operation. They are
fixed in the repair round by correcting the refusal remedies, the storage API,
the catalog note, the runbook and D162 clause (b), with a test for each remedy
that fails on the earlier text. Two findings had also proposed an earlier
refusal, which Go cannot decide without a planning-visible Call Home fact and
D157 forbids deciding from the release; it is [B631](backlog.md#b631). Of the
other three findings, the remedy of `native-option-undeclared` was a wording gap
that strands no operation and was corrected in the same round, the catalog
evidence cell listed the Call Home registrations beside the `orch accept-license`
line and is corrected, and the detector's compiled-module and package
forms are recorded in [B635](backlog.md#b635). A completeness round found no
gaps.

**Gates:** on the integrated lanes at their head, `make check-offline
tidy-check modules-check vulncheck docs-check race` passed with no
vulnerabilities, so did `./scripts/ansible-check` with all suites (2418 unit
tests passed, 1 skipped), `./scripts/check-commits` and `git diff --check`; the
review's repair head passed the same commands. The catalog parity test was
failure-injected twice, by renaming a record id and by adding a variant, and
both failed naming the differing lists. The results on the squashed commit are
in its body. No live host ran: the specializations on a real `20.1.0-221` or
`20.2.x` cluster are the owner's three runs.

**Constraints left behind:** the three runs accept [B626](m2.md#b626). First-run
watch items are the image holding the `20.1.0-221` MGR facts, which come from
the signed source archive and not an entitled image; the observation of a
configured seed now reading the image payload for the `set-user` seam; and the
image tags of the runbook, taken from the design and not confirmed against an
authenticated registry. Follow-ups: [B627](#x57--applied-specializations-in-ceph-evidence) the evidence member,
[B628](m2.md#b628) the CephX key type, [B629](m2.md#b629) the provider bootstrap
tail on a continued seed, [B630](backlog.md#b630) narrowing `partial-cluster`
for a proved-absent rejection, [B631](backlog.md#b631) an earlier Call Home
refusal, [B632](backlog.md#b632) the unused `installed_cephadm`,
[B633](backlog.md#b633) a flaky parallel test, [B634](backlog.md#b634) the
runbook per release and [B635](backlog.md#b635) the review's untested edges. The
multidc example still cannot plan because of `cephadm.workarounds`;
[B479](m2.md#b479) owns that migration. The comment ceiling of
`internal/containercluster` fell from 29 to 23, a one-line touchpoint for a peer
branch that rebases, and the line of module names in the Ceph native worker is
now 157 columns wide, which that branch re-wraps when it adds a module. Auxiliary
image selection is not a version specialization, and its contract stays with
B479.

### X102 — Audit defects for full IBM Ceph (M2)

**Owner:** Storage, with State reconciliation and CLI. A defect slice of
[M2](m2.md), delivered as one commit on the owner's approval of 2026-10-19 to
fix the general defects of the 2026-10-18 audit of main `fcff2f58f..ce190446a`.
**Decision:** [D163](backlog.md#decisions), which authorizes it and records that
Bootwright registers and enforces no component version rule. A slice is the
ordered items of one milestone, so D163's audit work is split: this slice
carries the M2 items and X103 the M1 items. **Items:** B636, B637, B618, B638,
B639, B640, B641 and B642, with B648 and B649 settled as below. Every confirmed
general finding of the audit is closed below, recorded as an owner question,
parked, already fixed or declined under D163.

- **B636, the guest registry broker.** Findings pull-broker-1 to pull-broker-6.
  An expired bearer answers an RFC 6750 `invalid_token` challenge, so the broker
  authenticates once more; a blob or config redirect with status 301, 302, 303,
  307 or 308 is followed for one delivery hop; the connect budget divides across
  the resolved addresses; teardown never replaces the native failure and always
  removes the broker's setup files; the native client receives an empty
  signature-lookaside configuration. No trust boundary widens: the redirect
  hop, the credentials and the lookaside stay confined as before.
- **B637, native diagnostics.** Findings adapter-diagnostics-2 to
  adapter-diagnostics-4. Redaction removes credential values only: the registry
  URL and username are not secrets at the broker, the discovery and the host
  pull sites, a sha256 digest and an absolute path survive opaque-material
  redaction, and the producer emits only characters the Go runner's tables
  accept, so a native failure keeps its named refusal.
- **B618**, moved from the backlog's parked table to M2 on 2026-10-19 under
  D163 and delivered here (finding milestones-docs-5). A bounded native cause
  keeps its head and the terminal tail of a long traceback inside the existing
  public ceiling. B615's clause that native failures preserve their useful
  cause is met by this item.
- **B638, capacity and its guard.** Findings adapter-diagnostics-1,
  storage-preflight-2, storage-preflight-3 and guards-contracts-1. Capacity
  admission runs only while a libvirt Machine block may still start (the
  settled B649 below); a Machine whose runtime proof fails keeps its proved disk
  credit, and a deficit that an unproved credit could explain is reported
  unknown, not failed; libvirt domain files are read under one 1 MiB bound and
  a failing file is named; the architecture suite confines `preflightlocal` to
  read-only members.
- **B639, Ceph finalization and the dashboard.** Findings
  ceph-bootstrap-licensing-2, ceph-bootstrap-licensing-3,
  gap1-cross-build-continuation-4-3, gap2-gap2-release-matrix-nati-2-4 and
  ceph-services-dashboard-1 to ceph-services-dashboard-4. An attempted
  `enable-call-home` is reissued once before acceptance, because the native
  command only sets state a repeat sets again; the dashboard port repair takes
  its holds before it writes an intent, fails the active MGR over only when a
  standby is registered and at most once per target, and resubmits once per
  apply; the redeploy wait tolerates MGR churn; the MGR container and plan
  refusals carry their own `dashboard-manager` token and remedy; the storage
  spec and knowledge state the repeat rules, the license readiness predicate,
  its limit on an enrolling continuation and the md5 source difference.
- **B640, coordinates and packages.** Findings ceph-coordinates-go-1 to
  ceph-coordinates-go-3 and gap1-test-weakening-2-4. Go admission refuses what
  the guest refuses: C1 controls, U+2028, U+2029 and invalid UTF-8 in a native
  version string; a `ceph.packageRepository` of more than 1798 bytes, or with a
  backslash or dot segment; and an image base that is only the registry host.
  The guest's staging bound for a `cephadm` package version equals the bound
  admission applies. The cluster worker checks the
  installed `cephadm` and `ceph-common` against the frozen package decision
  instead of the authored version string.
- **B641, tests, remedies and retained state.** Findings
  gap1-cross-build-continuation-4-1, ceph-ssh-runtime-3,
  gap1-test-weakening-2-2, gap1-test-weakening-2-3,
  gap2-gap2-package-signer-trus-1-4 and gap2-gap2-ceph-destroy-observ-3-1.
  Another build's refusal offers the take-back only when this build can plan the
  removal destroy would plan; the Ceph services and transport tests assert what
  production sends; the publisher-key tests refuse an extra key and a RHEL
  signer on a foreign platform; the host release impact names the retained
  host runtime and package records; the dead `installed_cephadm` is gone and its
  tests prove independence from the host package.
- **B642, specs, knowledge and the runbook.** Findings
  gap1-cross-build-continuation-4-4, ceph-coordinates-go-4,
  gap1-test-weakening-2-5, milestones-docs-7, ceph-ssh-runtime-2,
  examples-reorg-1, cli-output-contract-7, process-compliance-1, D163-A6 and
  gap1-package-resolution-matri-3-2. The storage specs, knowledge, the Ceph
  runbook and the acceptance ledger state core request v6, operator-selected
  images, the redirect, lookaside, capacity, retained-state and dashboard rules
  above, and no per-release screening or version-combination wording (D163).
  The audit's claim that IBM Storage Ceph 9.9.0.x requires a RHEL release is
  declined under D163 as a component version rule.

**Settled under D163's approval, for the owner to revise.** The orchestrating
session settled two owner questions of the audit, recorded here as delivered
and not as open items. **B648**, an exhausted image-pull journal that refused
every later apply: option (a), a per-invocation budget. Each host preparation
run makes up to three bounded attempts numbered 1 to 3 again, rewriting a
retained pull-attempt record is the same-content write, and the retained-budget
refusal is gone; the digest pin, platform proof, signature policy, broker
confinement and per-attempt cap are unchanged. A block that the same apply
retries after observation is a new run and gets three more attempts. The
image-pull remedy ("apply again; host preparation resumes from its journal") is
now true, and a pull fix continues after an automation-only rebuild; a build
from before this slice still refuses an exhausted journal. **B649**, capacity
admission on a Ceph-only continuation: option (b), the gate runs only while a
libvirt Machine block remains to start or resolve, because a domain's vCPU and
memory sizes are fixed when it is defined, the spec places the gate before a
claim, and the refusal's remedies cannot be followed while an operation is
incomplete. The earlier reading of B638 that also gated managed Ceph blocks is
withdrawn.

**Operator-visible effects:**

- A registry blob served through a 301, 302, 303, 307 or 308 redirect pulls,
  and an expired token re-authenticates once; a pull to a registry with one
  dead address no longer spends the whole connect budget on it.
- A failed native step keeps its coordinates, digests and absolute paths in
  the public cause, and its long traceback keeps the terminal exception.
- A continuation whose libvirt Machines are done and whose Ceph blocks remain
  no longer re-proves host capacity; an apply that starts only an artifact
  server does not either. A Machine whose runtime proof fails reports unknown
  capacity and the repeat remedy instead of "increase available capacity".
- A lost `enable-call-home` reply, a dashboard port repair interrupted after
  its intent, and an MGR that restarts during the redeploy recover by the next
  apply instead of refusing `partial-cluster` or timing out. A dashboard MGR
  refusal names `dashboard-manager`, not the dashboard admin user.
- A package repository, version string or image base that the guest would
  refuse is refused at planning; a plan with `cephadm.packageVersion` in a form
  DNF accepts no longer stops the cluster phase after it changed the host.
- A refusal from another build offers `bootwright destroy` as the take-back
  only when this build can plan it, and otherwise names installing the
  registering build and running `bootwright setup`.

**Digest effects:** no request, record or plan-digest version moved, and no
contract golden changed. The Ceph automation digest moved, and the Go changes
(stricter admission, the lifecycle gate, the preflight facts and a destroy
impact that plan comparison excludes) move no version or pin. A live operation
therefore continues after a rebuild, `setup` and an apply, as
[a fix to a failed step](../../.agents/knowledge/continuation-after-a-fix.md)
states; a step that already wrote an attempt record and failed natively still
refuses `partial-cluster`.

**Post-completion Ceph corrections.** Four commits landed after M2b's
acceptance of 2026-10-17 without a slice record, and this slice names them
(process-compliance-1): d9e67eb11, the RHEL 10 container repository selection;
125f31c03, the removal of the AsyncSSH exact-version refusal so the route proof
runs; 5dbf06288, licensing and Call Home finalization after protected
enrollment; and ce190446a, address reuse for the route probe's listeners. Their
bodies state that no live retry or apply ran, and that no real-image route
qualification ran; the pending release runs belong to [B626](m2.md#b626).
Whether M2b stays done after the dashboard repair and core request v6 is the
owner's question B653, which D165 answered: M2b stays done ([X104](#x104--owner-answers-of-2026-10-19)).

**Audit findings not fixed here.** Fixed earlier: ceph-bootstrap-licensing-1,
gap2-gap2-release-matrix-nati-2-1 and gap2-gap2-release-matrix-nati-2-2 by
fe3e4f715 (X101); gap1-package-resolution-matri-3-3 by fe3e4f715's runbook
forms and ce9767849's example; guards-contracts-3 by 082ae7cfa. Declined under
D163 as component version rules: gap1-package-resolution-matri-3-2 (a RHEL
release requirement), gap1-package-resolution-matri-3-4 (binding the license to
a Ceph release) and gap1-test-weakening-2-1 (an OS gate; its residual
diagnostic is parked). Owner questions B650 to B656, answered by
[D165](backlog.md#decisions) and closed in
[X104](#x104--owner-answers-of-2026-10-19). Parked: [B657](backlog.md#b657) to [B664](backlog.md#b664),
[B674](backlog.md#b674), [B675](backlog.md#b675), [B677](backlog.md#b677),
[B678](backlog.md#b678) and [B679](backlog.md#b679). The audit also noted that
the numbering of the peer branch `m2c` against the ids on main
(milestones-docs-1) is the peer's to reconcile above D164, B681 and X103; this
slice edits nothing of it.

**Review:** each of the 33 sub-items was reviewed by two lenses, brief
conformance with test adequacy, and its correctness against the production
path. Of 66 verdicts, 61 approved and 5 blocked. Four of the five found a guard
no test failed without (the broker's per-address handshake share, the path
exemption's padding and depth limits in diagnostics, the capacity gate's
unproved-block term and the production fallback that reads the package
decision), and each is fixed with a test that fails when the guard is reverted.
The fifth found a runbook sentence that read as if a Ceph-only apply re-proves
capacity, contrary to the settled B649, and the runbook is corrected. The
adversarial review of the integrated diff raised nine findings. Five were real,
in scope and non-blocking, and are fixed: a lone surrogate in a text cause
crashed redaction instead of keeping the named failure, the take-back judged
the whole frozen plan, the active-MGR failover had no cap, Go admitted package
repositories the guest cannot fetch, and one review-spec sentence contradicted
the secret-leaf refusal. Four were not real or out of scope. Three
repair-and-check rounds followed. The checks found redaction hardening for a
value and a secret that reach it in different undecodable-text forms (a lone
surrogate, an invalid byte, U+FFFD); no known path reaches any of them, and the
orchestrator's final fix compares every such run as one space. They also found
two `canTakeBack` branches with no test, the removal subset that mirrors
`RemainingSubset` and an empty owned set on a failed apply, both recorded in
B679. An integration fix
made a Unicode-table test conditional on the interpreter after the host-floor
Python 3.9 run failed it. No finding remains open.

**Gates:** the integrated lanes and each repair head passed `make check-offline
tidy-check modules-check vulncheck docs-check race`, `./scripts/ansible-check`
with all suites, `./scripts/check-commits` and `git diff --check`; the first
integrated `ansible-check` failed the Unicode-table test on the floor
interpreter, fixed as above. Each behaviour-changing sub-item names a mutation
that failed its test; the documentation-only sub-items (ceph-6, lex-3 and
spec-1 to spec-4) name none, and docs-check is their gate. The results on the
squashed commit are in its body. No live host ran: the
broker, the dashboard repair and the reissue are qualified in-tree only, and
their first real run is the owner's.

**Constraints left behind:** the 3-attempt pull budget is per host preparation
run, so a block retried in the same apply gets three more. Capacity admission
no longer guards a Ceph-only continuation, so a foreign domain defined mid
operation is the operator's to prevent. The first real-registry run confirms
the expired-token and redirect behaviour, which the tests model from RFC 6750
and the native client. Follow-ups: [B679](backlog.md#b679) holds the edges the
lanes left. The 2026-10-19 flake of
`test_vendor_defaults_allow_completion_replay_and_owned_removal_without_global_writes`
on Python 3.9, which this slice does not touch, is recorded in
[B633](backlog.md#b633).

### X103 — Audit defects and D163 screens for Foundations (M1)

**Owner:** CLI presentation, Controller, Managed OS and Substrate, with
Environment and Docs. A defect slice of [M1](m1.md), delivered as one commit
after [X102](#x102--audit-defects-for-full-ibm-ceph-m2) on the owner's approval
of 2026-10-19 to fix the general defects of the 2026-10-18 audit of main
`fcff2f58f..ce190446a`. **Decision:** [D163](backlog.md#decisions), which
authorizes the audit defect work, planned as X102, and records that Bootwright
registers and enforces no component version rule. A slice is the ordered items
of one milestone, so the work splits: X102 carries the M2 items and this slice
the M1 items. **Items:** B643, B644, B645, B646 and B647. Every confirmed
general finding of the audit that belongs to M1 is closed below, recorded as an
owner question or parked.

- **B643, the libvirt firewall readiness barrier.** Findings
  substrate-firewall-1 to substrate-firewall-3. Removing a barrier that had
  failed now runs `systemctl reset-failed`, so the failed unit no longer reads as
  foreign and no later destroy or apply refuses on it; a barrier that failed at
  boot while the networks, pool and directory were held is positive no removal
  effect ([substrates](../substrates.md)); and a readiness refusal, in Go and in
  the role's wait, carries a remedy.
- **B644, human output.** Findings cli-presentation-code-1, -3 and -4,
  cli-output-contract-2, -3, -4, -7 and -8, and trust-info-commands-1. A path, an
  identifier or a command is never split or truncated (the Logs path, operation
  ids, `Next`, the Ceph command, the access templates and a resource heading's
  name); `status` opens with `✓ Context <name>`; compact `cluster info` keeps the
  dashboard certificate fingerprint; every field group is one aligned call and a
  blank line separates resource records; progress headings are indented like
  section headings; the controller and CLI spec text match the rendered output.
- **B645, structured output.** Findings cli-presentation-code-2 and
  cli-output-contract-1. Every YAML string value is double-quoted and a mapping
  key YAML 1.1 would retype is quoted; a raw effective-state or diff document
  keeps every character of a JSON string; an export holding a C0 control other
  than tab, line feed or carriage return, DEL or a C1 control is base64-encoded
  in JSON and YAML alike ([output](../cli/output.md)); a successful `validate`
  prints exactly the one summary line the spec requires.
- **B646, the D163 screens.** Findings D163-A1 to D163-A3. An exact pin of
  `helm`, `govc`, `kubectl` or `virtctl`, and the exact `openshift-install` and
  `openshift-clients` of a cluster release, may name any publisher release tag,
  a release candidate included; the CLI version grammar is `latest` or an
  optional `v`, a leading digit and then `[A-Za-z0-9._+-]`, 95 characters at most
  after the `v`, and `latest` still resolves a stable release. The controller-stage
  module projects what admission accepts. The managed-OS `spec.os.version` is
  free-form and the refusal of a RHEL major below 9 is gone; the rescue
  declaration is no longer pinned to RHEL 9 (any rescue is still refused for want
  of a journey).
- **B647, the operator guide and the docs suite.** Findings examples-reorg-2,
  examples-reorg-3, guards-contracts-2 and milestones-docs-7 (operator-guide
  part). The [operator guide](../../docs/operator-guide.md) records the accepted
  M1 gates as a re-run recipe, the recovery text of the lab examples links the
  rebuild-and-continue path, and the docs suite indexes every tracked example
  scenario, including `examples/machines/libvirt-network-variants`.

**Operator-visible effects:**

- A libvirt host whose firewalld readiness barrier failed destroys and applies
  again; a readiness refusal names starting `firewalld.service`, confirming
  `firewall-cmd --state` and repeating the apply, and the wait states 31
  attempts two seconds apart.
- Human output never cuts a path, an identifier or a command, so a narrow
  terminal or a long name prints a line wider than the row; `status`,
  `cluster info`, `machine info`, `secret info`, the progress headings and
  `validate` follow [the output spec](../cli/output.md).
- `-o yaml` quotes every string value, so `on`, `yes` and `1:20` read back as
  strings; a secret export with a NEL, DEL or other control is base64 and no
  longer fails or folds to a space.
- A pin such as `virtctl v1.9.0-rc.0` or a `4.22.0-rc.1` cluster's installer is
  admitted, frozen and installed by `setup` and `apply --stage controller`.
  `distribution.release.version` of an OpenShift cluster is not part of this
  change ([B680](delivered.md#b680)).
- The stable-only pin screen, the managed-OS RHEL floor and the rescue RHEL 9
  pin are gone; a version the operator chooses fails natively on an older
  target.

**Digest effects:** no request, record or plan-digest version moved, and no
contract golden changed. The automation digest moved, by the libvirt host role
and the controller-stage module, and the Go changes move no pin. A live
operation therefore continues after a rebuild, `setup` and an apply, as
[a fix to a failed step](../../.agents/knowledge/continuation-after-a-fix.md)
states.

**Review:** each of the 17 sub-items had two lenses, brief conformance with test
adequacy, and its correctness against the production path. Of 34 verdicts, 33
approved and 1 blocked: no test failed when the verbatim call sites of the
unsplit-output change were reverted, and the renderer-level tests that answer it
were added. The adversarial review of the integrated diff raised 7 findings.
Six were real and in scope, one blocking and five minor, and are fixed with a
test that fails without the fix: the controller-stage module still refused every
non-stable exact pin (blocking), a continuation line opened with a space, a
resource heading still split a name, YAML altered or refused raw-document
strings and left keys such as `on` plain, `secrets.md` contradicted the output
spec, and admission accepted a 96-byte unprefixed tag that setup then refused
(the bound is now 95 characters after the optional `v`). One finding, about the
order of the operator guide's re-run recipe, was not real. An independent check
of the repaired head reproduced every scenario as fixed and found no further
gap. An integration fix inverted the two dependency-version test rows that
asserted the removed screen. No finding remains open.

**Gates:** the integrated head (after its integration fix) and the repair head
passed `make fmt-check modules-check tidy-check test vet completion-test
vulncheck docs-check race`, `./scripts/ansible-check` with all suites,
`./scripts/check-commits` and `git diff --check`; the first integrated `make
test` failed the dependency-version rows above, fixed as above. Each lane ran
its targeted tests and `make focus`, and the lex lanes' focus run failed the
dependency-version rows that the integration fix inverted. Each
behaviour-changing sub-item names a mutation that failed its test; the
documentation-only sub-items cli-6, docs1-1 and docs1-2 name none. The results
on the squashed commit are in its body. No live host ran: the
firewall removal path is qualified in-tree only, and its first real run is the
owner's.

**Constraints left behind:** `latest` still resolves a stable release only. A
rescue declaration is still refused. Follow-ups: owner questions
B651, B655 and B656, closed in [X104](#x104--owner-answers-of-2026-10-19); parked
[B665](backlog.md#b665) to [B673](backlog.md#b673), [B676](backlog.md#b676),
[B680](delivered.md#b680) and [B681](backlog.md#b681), which holds the edges the
lanes left.

### X104 — Owner answers of 2026-10-19

**Owner:** the owner, with CLI, Controller, Storage and Docs. An enabling slice
delivered as one commit under [D165](backlog.md#decisions), which records the
owner's answers of 2026-10-19 to the seven questions that
[X102](#x102--audit-defects-for-full-ibm-ceph-m2) and
[X103](#x103--audit-defects-and-d163-screens-for-foundations-m1) left open. It
spans [M1](m1.md) and [M2](m2.md), so it carries no planned slice. **Items:**
B650 to B656, all closed and removed from their pages.

- **B650, the example's build tag.** `libvirt-3-nodes-ceph` authors
  `ceph-9-rhel10:v9.9.2-26729`, which the owner confirmed exists in the
  entitlement. The cluster declaration, its runbook, the effective-state golden
  and the version-constraints knowledge page follow. The effective-state golden pins the
  tag.
- **B651, unknown names in `machine info` and `secret info`.** A name the
  context does not hold refuses `access.target` with exit status 1, naming every
  such member and the list command, like `cluster info`; a separator-only
  `--name` is a usage error and an omitted one still lists all. The CLI spec,
  the command table and the output spec follow. Tests: an unknown name refuses
  for each command with a known name beside it, a known name selects its row, and
  a separator-only list exits 2; each fails when its check is removed.
- **B652, public acceptance evidence.** The milestones rule, the ledger header
  and the operator guide agree: a summary row (date, build commit, command
  sequence, outcome) plus the operation-log digest of the private archive, and an
  agent may record an acceptance the owner states explicitly. Existing rows are
  not rewritten.
- **B653, B615 and M2b.** The owner confirms authorizing B615 on 2026-10-16 and
  accepting B615 and M2b on 2026-10-17. M2b stays done; the corrections named in
  X102 are post-completion corrections, and their pending release runs stay with
  [B626](m2.md#b626).
- **B654, the WIP limit.** X47 and X52 to X54, with the items they list, return to
  `not started`; D134's M2b-only exception is not extended, so X101 to X103 and
  later slices count against the limit of two.
- **B655, the CLI presentation migration (M1b).** Commit 2dc470bff, authored by
  the CLI tool and not rewritten, adopted the shared output presentation: semantic
  blocks with injected terminal capabilities, `machine info` and `secret info`,
  `-o yaml` and the inherited `-o`, and the `--color`, `--progress`, `--detail`,
  `--ascii` and `--width` flags. The owner authorized it, including the direct
  dependency `github.com/charmbracelet/x/ansi` (v0.11.7) and its indirect modules
  `clipperhouse/displaywidth`, `clipperhouse/uax29/v2`,
  `lucasb-eyer/go-colorful` and `mattn/go-runewidth`. The dependency is described
  in [development](../../docs/development.md) and the presentation in the
  [output spec](../cli/output.md).
- **B656, the untested-release warning.** `controller.untested-release` stays,
  with the table suffix and the lock's `tested` field, as an explicit exception
  to D163: it concerns Bootwright's own host and never refuses. The
  [controller spec](../controller.md#supported-hosts) records the exception.

**Constraints left behind:** none. No product behavior other than B651's
refusal changed.

### X59 — Registry trust bundle consumer

**Owner:** Storage and Secrets. A product slice of [M2c](m2.md#m2c) whose other
items are still open: this record covers B619, the one item of
X59 that completed, and X59's [B483](m2.md#b483) and [B493](m2.md#b493) stay on
the M2 page with what each has delivered in-tree. B619 was found while planning M2c: the
`Entitlement.registry.trustBundleRef` was admitted and documented as for
registry trust, but no storage code read it. **Items:** B619, delivered by the
lanes x59-d1-supply-admission, x59-d2-image-origin and int-x59, merged to main
with X59's other in-tree lanes under [D168](backlog.md#decisions).

**Outcome:** a declared bundle is the only TLS anchor of every registry
connection the Ceph supply makes. Image resolution, the guest pull broker and
each frozen auxiliary registry reference verify against it; host preparation
pulls the frozen Ceph image through that broker; and bootstrap skips cephadm's
own pull, so no guest pull reaches the registry outside the broker. Without a
bundle the host trust store stays the anchor, and a registry that verifies
under neither refuses at resolution before any host effect, naming the registry
and the anchor (the bundle Secret or the host trust store) and never a
location, verify message or credential. The bundle travels as a protected
request member and material, bounded in size and projected to its certificate
blocks. [Storage](../storage.md#bootstrap-identity-and-continuation) and the
[Secrets API](../api/secrets.md) own the behavior.

**Guard tests:** the Go plan and request tests
(`TestImageRequestBindsTheEntitlementTrustBundle`,
`TestHostRequestCarriesTheRegistryTrustBundle`,
`TestDeclaredRegistryTrustBundlePlansOnceConsumed` and the request goldens);
the collection's broker, resolution and host-runtime tests (a bundle that
verifies, one that does not and none: `test_broker_upstream_trusts_only_the_bound_bundle_when_declared`,
`test_broker_bundle_that_does_not_verify_names_registry_and_secret`,
`test_resolution_trusts_only_the_bound_bundle_when_declared`,
`test_resolution_without_a_bundle_keeps_the_host_trust_store`,
`test_bundle_that_does_not_verify_refuses`,
`test_every_frozen_auxiliary_reference_resolves_through_the_bound_bundle`,
`test_profile_passes_the_bound_bundle_to_every_broker`) and
`test_bootstrap_argv_skips_the_pull`, each with a mutation check recorded in
its commit body. The gates were `make quick`, `make focus`, `make docs-check`,
the storage, API and `cmd/bootwright` packages, the collection unit and lint
suites and the int-x59 checkpoint's full gates and adversarial review. They
are offline, as B619's exit evidence required; no live registry row is part of
it. The profile rows that exercise a private registry belong to
[B484](m2.md#b484) and stay unrun.

**Constraints left behind:** `ceph-image-v4`, `ceph-core-v22` and
`ceph-host-v5` moved with this work and `ceph-packages-v3` with B493's package
bundle; there is no older-version reader, so an incomplete operation or
context planned by an earlier build is finished or destroyed with that build.
No other behavior changed.

### X61 — Ceph transport crypto policy

**Owner:** Storage, with Machine and Trust. An enabling slice of
[M2c](m2.md#m2c) whose other work is still open: this record covers B625, the
one item of X61 that completed, and X61's [B484](m2.md#b484) stays on the M2
page with what it has delivered in-tree. B625 was found while planning M2c's
X60: the Ceph controller-to-host SSH passed `-F /dev/null` and no
`HostKeyAlgorithms`, so it ran on OpenSSH's compiled defaults instead of the
controller's crypto policy, and the contained tunnel's outer configuration
intersected no host-key algorithm with the pinned keys. **Items:** B625,
delivered by the lane x61-ceph-transport-policy and the fixes of the int-x61
checkpoint, merged to main under [D168](backlog.md#decisions).

**Outcome:** every Ceph controller-to-host session takes the controller's
crypto-policy back end, reduced to its crypto directives, and the managed-OS
guest session's option set, with `HostKeyAlgorithms` set to the pinned key's
algorithms that the policy admits, in pinned order; strict pinned known hosts
stay. The contained tunnel's client runs inside the selected Ceph image and
keeps that image's back end: its outer configuration pins, for each host, the
pinned algorithms that the back end's first `HostKeyAlgorithms` value admits.
Both paths share one builder. An empty intersection refuses before any host
is contacted: `ssh-policy-host-excluded` for the direct path and
`tunnel-host-key-excluded` for the tunnel, which the int-x61 review moved
before the cluster block claims or authorizes any host, naming the host and
the key type and the guest's FIPS switch as the remedy; a policy that cannot
be evaluated refuses `ssh-policy-unavailable`. The three refusals are
`lifecycle.unsupported`, so they leave their block failed. A destroy opens no
session and never evaluates the policy. [Security](../security.md#network-remote-systems-and-privilege)
and [storage](../storage.md#bootstrap-identity-and-continuation) own the
behavior.

**Guard tests:** `test_ceph_transport_uses_the_reduced_policy_config_not_dev_null`,
`test_ceph_transport_host_key_algorithms_are_the_intersection_with_the_policy`,
`test_ceph_transport_refuses_an_empty_intersection_naming_the_host`,
`test_ceph_transport_under_fips_policy_admits_ecdsa_and_refuses_ed25519_pins`,
`test_ceph_transport_keeps_strict_pinned_known_hosts` and
`test_ceph_transport_evaluates_the_controller_policy_once_and_never_on_destroy`
(unit tests of the direct path); `test_ceph_transport_option_set_matches_the_managed_os_guest_session`
(the parity test with the managed-OS guest session);
`test_tunnel_outer_configuration_host_key_algorithms_are_the_intersection_with_the_policy`,
`test_tunnel_outer_configuration_refuses_an_empty_intersection_naming_the_host`,
`test_tunnel_outer_configuration_and_direct_argv_share_one_builder` and
`test_restart_refuses_an_outer_configuration_without_its_host_blocks` (the
tunnel path); `test_an_excluded_tunnel_pin_refuses_before_any_host_is_claimed_or_authorized`
and `test_authorize_refuses_an_excluded_tunnel_pin_before_any_effect` (the
pre-claim refusal); `test_pinned_algorithms_match_the_host_key_algorithms_filter`;
and the Go `TestAnSSHPolicyRefusalReachesTheOperatorWithItsRemedy` and
`TestAnExcludedHostKeyRemedyNamesTheGuestKeyLeverAndDestroyFirst`, each with a
mutation check recorded in its commit body. The gates were `make focus`,
`make docs-check`, the storage, `cmd/bootwright` and architecture packages, the
collection unit, lint and sanity suites and the int-x61 checkpoint's full
gates and adversarial review. They are offline, as B625's exit evidence
required (unit tests of both paths, the parity test and the empty-intersection
refusal test); no live FIPS row is part of it. Keeping a policy is not FIPS
certification: the Ceph FIPS rows belong to [B484](m2.md#b484) and stay unrun.

**Not yet implemented:** X61's other work. The acceptance matrix lanes X61-L2
to X61-L9 are not implemented, nor are the lanes D166 released from their hold,
x61-ceph-fips (the ECDSA synthetic probe keys of [B660](backlog.md#b660) and
the FIPS host proofs), x61-host-variants-example and X61-L6. B484 names them.
The int-x61 review left no finding unfixed.

**Constraints left behind:** `ceph-core-v23` and `ceph-tunnel-v3` moved with
this work; there is no older-version reader, so an incomplete operation or
context holding a `ceph-core-v22` block or a `ceph-tunnel-v2` bundle from an
earlier build is finished or destroyed with that build. No other behavior
changed.

### X57 — Applied specializations in Ceph evidence

**Owner:** Storage, with CLI. A product slice of [M2c](m2.md#m2c) whose other
items are still open: this record covers B627, the one item of X57 that
completed, and X57's [B479](m2.md#b479), [B489](m2.md#b489) and
[B686](m2.md#b686) stay on the M2 page with what each has delivered in-tree.
B627 was a follow-up of [X101](#x101--version-specializations-and-ibm-storage-ceph-990x-readiness):
the registry, the seams and the refusal coordinate carried the applied
specialization ids, but the Ceph block evidence did not, so an operator could
read them only from a refusal. **Items:** B627, delivered by the lane X57-L7b
(its sub-items a and b and the review rounds that guard them) and merged to main
with X57's other in-tree lanes under [D168](backlog.md#decisions), carried in
M2c by [D169](backlog.md#decisions), riding the core move of the CephFS client
lane; only `ceph-host-evidence-v2` moved for the member, and it strands nothing
the same commit's core move did not.

**Outcome:** the Ceph core evidence and the ceph-host evidence carry an optional
`specializations` member: 1 to 32 ascending, unique lower-case ids of at most
64 bytes, the ids the native phases of the writing execution applied. A malformed
member refuses; nothing that decides an effect, ownership, identity, readiness,
drift or a digest reads it, and it is not cumulative across executions. The
collection's native dispatch records the ids the worker's selection applied,
the transport refuses an unknown, empty, unsorted or duplicated list, and the
member is absent when none applied, on the cluster, service-unit, gateway and
host blocks alike (a destroy's released host record and the SMB block record
none). `bootwright cluster info` shows them: the JSON gains `specializations`
after `image`, omitted when none, and the full text gains a `Specializations`
line after `Platform`, `none` when empty; the compact and unavailable entries
show nothing. The line is the sorted union of the cluster apply proof's ids and
those of every decodable ceph-host apply proof of the cluster.
[Storage](../storage.md#bootstrap-identity-and-continuation) and the
[output contract](../cli/output.md) own the behavior.

**Guard tests:** the Go `TestEvidenceSpecializationsMemberIsOptionalAndClosed`,
`TestHostEvidenceSpecializationsMemberIsOptionalAndClosed`,
`TestInspectNamesTheAppliedSpecializations`,
`TestClusterInfoShowsNoneWithoutSpecializations` and
`TestCephInspectionReportsAReasonWithoutProofsAndTheFSIDWithThem` (the wiring
guard), the cluster info goldens `cli-cluster-info-full`,
`cli-cluster-info-json` and `cli-cluster-info-secrets-json`, and the
collection's `test_core_and_host_evidence_name_the_applied_specializations`,
`test_unit_and_gateway_evidence_name_the_applied_specializations`,
`test_evidence_without_specializations_still_reads` (the older record without
the member, which is the continuation test),
`test_dispatch_records_the_ids_its_worker_applied`,
`test_transport_collects_applied_specializations_and_refuses_unknown_ids` and
`test_work_returns_applied_specializations_beside_the_result`, each with a
mutation check recorded in its commit body. The gates were `make quick`,
`make focus`, `make docs-check`, the storage, inspection, CLI,
`cmd/bootwright` and architecture packages, the collection unit, lint and
sanity suites, the regenerated contract goldens `storage-ceph-core-v3` and
`storage-ceph-host-v1`, and the int-ready-v4a checkpoint's full gates (all but
`make vulncheck`, which fails for GO-2026-6809 on main as well,
B690, delivered by [X156](#x156--go-toolchain-vulnerability-gate-go12610)) and adversarial review. They are offline, as B627's exit evidence required; no live row is part
of it.

**Digest and version effects:** the member rode the core contract move that the
CephFS client scope of [B489](m2.md#b489) made (`ceph-core-v24` to
`ceph-core-v25`), and `ceph-host-evidence-v1` moved to `ceph-host-evidence-v2`
because the host content digest hashes only its version constants;
`ceph-core-evidence-v8` stays, and a record without the member still reads. The
later move to `ceph-core-v26` is B479's. There is no older-version reader, so an
incomplete operation or context planned by an earlier build is finished or
destroyed with that build. The collection change moves the Ceph automation
digest, which alone needs a rebuild and an apply to continue.

### X111 — M3b controller and release recovery

**Owner:** Controller, with Container cluster. A defect slice of
[M3b](m3.md#m3b), delivered in-tree on `m3` at the explicit request of
2026-10-20 under [D172](backlog.md#decisions)'s M2 implementation waiver. The
delivering commit records the verification commands and results; no host
operation ran. [B706](m3.md#b706), the `nmstatectl` host-package closure of
the controller stage, is implemented and source-verified but stays on the M3
page, awaiting the owner-run lab-sno rehearsal with [B61](m3.md#b61), M3b's
operator gate, and [B462](m3.md#b462). **Items:** B307, B680, B309, B237,
B239, delivered in-tree.

**Outcome:** one exact publisher-release grammar governs ContainerCluster
admission, tool selection and tool requests; installation resolves tools in a
fixed order. A published but unsealed client area is incomplete, and every
completing path seals it before the block completes, the settled replay and a
replay that installs a missing host package alike. `latest` is selected from
the context binding the recovery or the preflight proved. The controller stage
installs `nmstate` for agent media whose cluster nodes reference a declared
Machine, and `preflight controller --context` reports an `nmstate` check. The
[controller spec](../controller.md) and the
[container cluster spec](../container-clusters.md) own the behavior.

**Guard tests:** `TestExactClusterReleaseGrammar`,
`TestReleaseVersionRequiresAPatchAndAcceptsPublisherPrereleases`,
`TestReleaseWithoutPatchIsRefusedByTheReleaseGrammar`,
`TestTargetToolsUseTheClusterReleaseGrammar` and
`TestOpenShiftToolRequestsRequireTheSharedExactReleaseGrammar` (B307 and
B680, whose stable-only mutant failed the admission test);
`TestMissingInstallationToolsAlwaysNameTheInstallerFirst` (B309);
`TestAnInterruptedPublicationNeedsAReplayToSeal`,
`TestAReplayThatInstallsAHostPackageSealsTheInterruptedPublication`,
`TestASealFailureAfterAReplayedHostPackageInstallIsUnknown` and
`TestInstalledToolLookupPreservesStoreFailures` (B237);
`TestContextLatestIgnoresANewerReleaseRetainedByAnotherContext`,
`TestContextPreflightSelectsLatestOnlyFromItsOwnBinding`,
`TestInterruptedStageUsesItsPreparedSourcesBeforeAnyContextBinding` and
`TestFreshStageKeepsTheReleaseInItsContextBinding` (B239); and for the
`nmstate` closure `TestAgentMediaSelectsNMStateFromItsClusterNodes`,
`TestARequestAndStageNativeOfAgree`, `TestTheFrozenRequestCarriesNMStateAndIPUtils`,
`TestContextPreflightReportsTheNMStateAndIPUtilsClosures` and
`TestAFrozenStageRequestOfAnyOtherVersionRefuses`. The audit fixes recorded a
mutation check for each test they added.

**Constraints left behind:** the frozen controller request moved from
`controller-clients-v3` to `controller-clients-v4` with the `nmstate` closure,
and its content digest moved with it
(`TestEveryImplementationMovesItsContentDigestWithItsContract`); X117 moves it
again. There is no older-version reader, so an incomplete operation or context
whose controller block an earlier build planned is finished or destroyed with
that build first, before this build touches it (D172, session clause 10). No
other behavior changed.

#### B307

One exact publisher-release grammar now governs ContainerCluster admission, tool selection and tool requests. A missing patch refuses at validate and plan; publisher pre-releases remain accepted.

#### B680

Admission of `4.22.0-rc.1` is regression-tested. Replacing the grammar with a stable-only screen made that test fail; the mutant was removed.

#### B309

Installation resolves tools in a fixed order, installer before `oc`, so two missing tools always name the installer.

#### B237

Recovery treats a published but unsealed client area as incomplete. Replay seals it before the block completes, on the settled path and after a host-package install alike, and tool lookup preserves store failures. A failure before the seal attempt on the settled path is failed; one from the seal attempt on, and any after a host install, is unknown.

#### B239

Recovery and context preflight select `latest` from the context binding they proved. An interrupted first publication reuses its prepared sources rather than adopting another context's newer client.

### X112 — M3b admission diagnostics

**Owner:** Container cluster, with Desired state and Add-ons. A slice of
[M3b](m3.md#m3b), delivered in-tree on `m3` at the explicit request of
2026-10-20. The delivering commit records the verification commands and
results; M3b's [B61](m3.md#b61) operator acceptance remains unrun.
**Items:** B411, B312, B313, delivered in-tree.

**Outcome:** release-image and catalog-image digests are normalized, a cluster
name beyond the longest lifecycle block prefix's budget refuses at admission,
the inert endpoint and channel fields are declared as such, and every
admission refusal names the field to correct and, where one exists, an exact
remedy. The [container cluster spec](../container-clusters.md) and its
[API](../api/container-clusters.md) own the behavior.

**Guard tests:** `TestCatalogImageNormalizationChangesOnlyDigest`,
`TestReleaseImageNormalizationChangesOnlyDigest`,
`TestClusterNameFitsInstallationBlock`, `TestClusterNameAdmissionBoundFitsEveryBlock`
and `TestClusterNameAdmissionDiagnosticNamesItsObject` (B411);
`TestAdmittedInertFieldsDoNotChangePlans` (B312);
`TestAdmissionRemediationNamesTheFieldToChange`,
`TestEveryAdmissionRuleRemediationNamesWhatToChange` and
`TestAdmissionRequiresTheEffectiveAPIEndpointAddress` (B313).

**Constraints left behind:** none. No frozen request version moved.

#### B411

Normalization lowercases ContainerCluster release-image and ClusterAddon catalog-image digests. ContainerCluster admission refuses names beyond the 47-byte budget of the longest lifecycle block prefix at `metadata.name`.

#### B312

Endpoint connection metadata and the derived release channel are declared inert or informational in the owning API contract, and so is `spec.install.endpoints.*.dnsName`, read only by the named-certificate collision check of `api-int`. The inert-fields test proves media and installation plans unchanged.

#### B313

ContainerCluster invariant diagnostics name the field to correct and provide an exact remedy when one exists. The generic remedy was removed. A table test pins the remedy of every admission rule, and admission requires an effective API endpoint address.

### X113 — M3b installer projection

**Owner:** Container cluster, with Secrets. A slice of [M3b](m3.md#m3b),
delivered in-tree on `m3` at the explicit request of 2026-10-20. The
delivering commit records the verification commands and results; M3b's
[B61](m3.md#b61) operator acceptance remains unrun. **Items:** B308, B306,
B310, B311, B622, delivered in-tree.

**Outcome:** the installer projection names the cluster and the field to
correct in every planning refusal; takes the administrator's authorized key
from a Secret through the media consumer's injected port, refusing key options
and unsafe comments; calls the owners of the cluster zone, endpoint names,
label grammar and substrate arms; reads normalized network defaults; and
refuses a composed node resolver list that conflicts with the selected name
servers. The [container cluster spec](../container-clusters.md) and the
[Secrets API](../api/secrets.md) own the behavior.

**Guard tests:** `TestPlanningRefusalsNameClusterAndSelectedField` and
`TestAnOffControllerServerRefusesBeforeRegistration` (B308);
`TestPublicAdministrationKeyFormsReachInstaller`,
`TestMalformedAdministrationKeyRefusesBeforeAdapter`,
`TestAuthorizedKeyRefusesOptionsAndUnsafeComments` and
`TestAuthorizedKeyAcceptsPublicPartAndOpaqueLine` (B306);
`TestPublishedEndpointNamesAgreeWithInstallerProbes` (B310);
`TestProjectionRefusesUnnormalizedNetworksWithClusterIdentity` (B311);
`TestNodeResolverDeclarationConflict` and
`TestAuthoredResolverListWithoutSelectionReachesInstaller` (B622).

**Constraints left behind:** none. No frozen request version moved.

#### B308

Agent-install planning diagnostics name the ContainerCluster and the field to correct. The off-controller boot-image server refusal joins the capability's Unsupported table.

#### B306

The Secrets material adapter returns one authorized-key line from an `sshKeyPair` public part or an opaque public key, through the media consumer's injected port. Malformed material, a key with options or a comment with a backslash, a control character or a quote names the Secret and discloses no bytes.

#### B310

Projection and admission call the owners of the cluster zone, controller Machine, endpoint names, label grammar and substrate arms. DNS publication and installation consume one endpoint-name owner, with a parity test.

#### B311

Projection reads normalized network defaults. An empty machine-network list refuses as an internal invariant; projection fixtures are normalized before use.

#### B622

Admission refuses composed node DNS server lists that conflict with selected DNS servers, naming both fields: an authored `dns-resolver.config.server` together with selected name servers is an `api.invariant` refusal at `nodes[i].machineRef`. Authored resolver lists without a selection reach the installer unchanged.

### X114 — Shared libvirt capacity admission

**Owner:** Substrate, with Storage and Container cluster. A shared-foundation
slice of [M1c](m1.md#m1c), delivered in-tree on `m3` with M3b at the explicit
request of 2026-10-20. The delivering commit records the verification
commands and results; M3b's [B61](m3.md#b61) operator acceptance remains
unrun. **Items:** B700, delivered in-tree.

**Outcome:** every apply whose selection may start or resolve a libvirt
Machine proves the aggregate host capacity before it registers, and the
composed apply refuses with the checker's reason; storage and container-cluster
preflight use the same budget. The [substrate spec](../substrates.md#capacity-admission)
owns the behavior.

**Guard tests:** `TestContainerOnlyApplyProvesAllDeclaredLibvirtGuestsBeforeAdmission`,
`TestStorageCapacityAdmissionSkipsTransitionsThatAllocateNoGuest`,
`TestComposedApplyRefusesAtTheLibvirtCapacityGateBeforeRegistration` (which
fails when the production wiring drops the gate),
`TestCapacityAggregatesSharedHostAndFilesystem` and
`TestCapacityNeverPassesUnprovedFactsOrReserve`.

**Constraints left behind:** none. No frozen request version moved.

#### B700

Every apply whose selection may start or resolve a libvirt Machine proves aggregate host CPU, memory and backing-filesystem disk capacity for all declared guests on that host plus the authored reserve. The substrate owns the shared budget and reserve policy; storage and container-cluster preflight reuse it. A continuation with all Machine blocks done performs no capacity observation. Every libvirt example declares its reserve, and the lab-sno acceptance-profile row states the reserve the example authors (1 CPU, 2 GiB, 10 GiB) with the gate's totals.

### X115 — M3b preflight and handoff

**Owner:** Container cluster, with CLI and Lifecycle. A product slice of
[M3b](m3.md#m3b), delivered in-tree on `m3` at the explicit request of
2026-10-20. The delivering commit records the verification commands and
results; M3b's [B61](m3.md#b61) operator acceptance remains unrun.
[B705](m3.md#b705), the container-cluster preflight, is implemented in-tree
and stays on the M3 page awaiting the same operator run. **Items:** B259, B71,
delivered in-tree.

**Outcome:** a kubeconfig the installer grew past 64 KiB is restored or left to
the installer by its last 8 KiB, and `status` derives GitOps handoff readiness
from durable records. `preflight container-cluster` resolves, through the
controller's own resolver, exactly the names the installation's resolution
before boot proves: `api`, `api-int`, the console name and the
`bootwright-<cluster>` wildcard label under `apps.<cluster>.<zone>`, never the
applications apex, which a `*.apps` wildcard does not answer. One owner in
`internal/containercluster` names them for preflight, the projection and the
install role, with a table shared with the role's Jinja rule. A settled apply
restores a latched required-log fault only after re-proving its records and
that the context's input is unchanged. The [container cluster spec](../container-clusters.md#preflight)
and [state reconciliation](../state-reconciliation.md) own the behavior.

**Guard tests:** `test_a_grown_installer_file_cut_in_each_section_is_restored`
and `test_a_failed_wait_names_the_kubeconfig_it_restored` (B259);
`TestStatusHandoffReadsRecordsWithoutWriting`,
`TestSettledApplyRestoresALatchedLogFault`,
`TestSettledLogRestorationReprovesItsRecords`,
`TestSettledLogRestorationReprovesItsInputInItsTransaction`,
`TestSettledLogRestorationRequiresADurableClear` and the handoff goldens of
`TestCommandOutputMatchesItsGoldens` (B71); and for the preflight names
`TestResolutionChecksTheNamesTheInstallationProvesAndNeverTheApex`,
`TestContainerPreflightUsesCompletedDNSAndApplyCapacityRows`,
`TestResolutionNamesAreTheNamesTheInstallationProves`,
`TestWildcardLabelMatchesTheInstallRoleTable`,
`test_the_role_renders_the_wildcard_label_of_the_shared_table` and
`test_the_shared_table_cuts_trailing_hyphens`.

**Constraints left behind:** none. No frozen request version moved; the
agentinstall projection derives the console name from the same owner and every
frozen request byte and golden is unchanged.

#### B259

Restore classifies a kubeconfig above 64 KiB from only its last 8 KiB: a complete final client-key-data line is left to the installer, and a cut rewrite is restored from the kept copy after identity validation. A failed wait names a restore it performed. Tests cover cuts in clusters and users, a whole grown file, and the failed wait.

#### B71

Status derives GitOps handoff readiness from durable operation and block records, with fixed-order reasons and no write or host probe. A settled apply restores a latched required-log fault under a mutating transaction after re-proving its records and its input. Human and JSON goldens cover readiness and every refusal reason.

### X116 — M3c endpoint ownership

**Owner:** Container cluster. A slice of [M3c](m3.md#m3c), delivered in-tree on
`m3` at the explicit request of 2026-10-20. The delivering commit records the
verification commands and results; the compact operator gate
[B66](m3.md#b66) remains unrun. **Items:** B707, B708, delivered in-tree.

**Outcome:** the platform of a multi-node cluster follows who owns its
endpoints, and an address a cluster declares for an endpoint belongs to no
other holder Bootwright knows. The [container cluster spec](../container-clusters.md)
owns the behavior.

**Guard tests:** `TestInstallerEndpointPlatformOwnership`,
`TestExternallyAnsweredEndpointsUsePlatformNone` and
`TestAuthoredBaremetalRefusesExternalEndpoints` (B707);
`TestEndpointOwnershipAcrossCatalog`, `TestEndpointOwnershipExceptions` and
`TestASelectedLoadBalancerAddressBelongsToOneCluster` (B708). The frozen
external-input goldens pin B707's rendering.

**Constraints left behind:** none. No frozen request version moved.

#### B707

Multi-node platform derivation follows endpoint ownership: installer VIPs use
`baremetal`; external and LoadBalancer endpoints use `none`. Mixed ownership,
an authored baremetal platform with external slots, differing API/internal API
VIPs and an API VIP equal to ingress refuse. Installer inputs carry no
load-balancer type. Admission and frozen external-input goldens pin this rule.

#### B708

Endpoint collision admission names the other cluster, Machine, managed network
host, managed service, emulated BMC or LoadBalancer holding an address. Tests
cover every holder class, wildcard binds and the own-node/own-LoadBalancer
exceptions.

### X117 — M3c boot and media safety

**Owner:** Container cluster, with Substrate. A safety slice of
[M3c](m3.md#m3c), delivered in-tree on `m3` at the explicit request of
2026-10-20. The delivering commit records the verification commands and
results. [B709](m3.md#b709) and [B710](m3.md#b710) are implemented and tested
in-tree and stay on the M3 page awaiting the compact operator run with
[B66](m3.md#b66). **Items:** B314, delivered in-tree; B796, a follow-up
delivered in-tree on 2026-10-20.

**Outcome:** no node is handed the media block's own publication. Each
fetching insert serves a fresh private hard link under its own token, and a
sweep retires every insert link no controller presents: when an apply has read
its nodes and before it boots any, when a release has ejected every node, and
in the block's inverse after its ejects, which keeps only the media of a node
whose eject failed. A failed insert retires its own link once its controller,
read without being changed, does not present it. The sweep removes a link only
when it is, by device and inode, the published image; any other insert entry
is left and refuses the block after every provable link is removed, and it
reports counts only. The token walk is anchored at the artifact server's
content root: the reserved root is `<content root>/public/private/<consumer>/<object>`,
every component from the content root down is opened without following a
link, and above it a symbolic link is resolved by the module, every directory
passed through owned by root and writable by neither group nor other, so a
controller that keeps its services root on a larger disk through a link still
applies, observes and destroys. One domain projection owns locally probeable
virtual IPs. Their IPv4 addresses on a controller-hosted managed network are
probed with ARP before a fresh boot, and only when no declared node is powered
on; an IPv6 address or one off a managed network is named in the completion
evidence as not probed. Boot TCP readiness shares the remaining boot budget and
gives every node one connection attempt, even after the budget is spent, so a
dead node never hides the nodes that answer; each unreachable node and its
address is named, with a hint that gives the two causes (the node did not boot
this cluster's image, or the image found no network configuration for the
hardware address it booted with) and says a repeated apply leaves such a node
as it is. Replays skip nodes already running this cluster's image and check
rendezvous port 8090 only when this attempt booted it. The
[container cluster spec](../container-clusters.md) and
[infrastructure services](../infrastructure-services.md#private-consumer-publication)
own the behavior.

**Guard tests:** the token module tests (`test_each_insert_is_unique_and_eject_removes_only_its_link`,
`test_another_inode_under_reserved_root_is_foreign`,
`test_symlinked_token_is_never_owned`,
`test_foreign_origin_and_traversal_are_never_owned`,
`test_ejected_token_answers_404_while_other_insert_still_serves`,
`test_a_symlink_from_the_content_root_down_refuses`,
`test_a_symlinked_ancestor_inspects_publishes_and_removes`,
`test_a_resolved_ancestor_others_can_write_refuses`,
`test_a_writable_directory_holding_the_link_refuses`,
`test_a_resolved_ancestor_root_does_not_own_refuses`,
`test_a_root_outside_the_reserved_shape_refuses`,
`test_sweep_keeps_presented_links_and_retires_every_other_insert`,
`test_sweep_refuses_and_keeps_an_insert_entry_it_cannot_prove` and
`test_the_sweep_task_fails_on_an_unprovable_entry_and_reports_only_counts`);
the role wiring tests (`test_boot_publishes_then_proves_the_listener_then_inserts`,
`test_the_insert_serves_the_per_insert_token_never_the_cluster_wide_publication`,
`test_release_retires_the_token_only_after_an_eject_that_reported_no_failure`,
`test_the_ownership_read_calls_an_insert_link_own_and_any_other_file_foreign`,
`test_apply_sweeps_right_after_reading_its_nodes_and_before_any_boot`,
`test_destroy_sweeps_after_its_ejects_keeping_only_unejected_media`,
`test_a_failed_insert_reads_its_controller_and_retires_its_link_when_not_presented`
and `test_observation_never_sweeps_or_removes_a_link`, with
`TestEveryObservePlaybookRunsOnlyAllowedTasks`); and for the proofs
`test_an_arp_reply_refuses_naming_address_slot_and_hardware`,
`test_no_arp_reply_proceeds`, `test_a_probe_that_could_not_run_refuses_as_failed_not_free`,
`test_any_powered_node_skips_every_probe`,
`test_unprobeable_vips_are_never_probed_and_reach_completion_evidence`,
`TestVirtualIPsOnlyProbeManagedIPv4NetworksOnController`,
`test_a_node_that_never_answers_is_named_and_the_later_ones_that_answer_are_not`,
`test_after_the_deadline_each_node_is_still_tried_once`,
`test_a_rendezvous_service_that_never_answers_is_named_with_the_reach_causes`,
`test_the_rendezvous_service_is_proved_only_when_its_node_was_booted_in_this_attempt`
and `test_a_reach_failure_names_its_causes_and_never_promises_another_boot`. The
contract guard is `TestEveryImplementationMovesItsContentDigestWithItsContract`
with the request goldens `TestFrozenRequestsMatchTheirGoldens`. Each carries a
mutation check recorded in its commit body.

**Constraints left behind:** the frozen controller request moved from
`controller-clients-v4` to `controller-clients-v5` for the selected `iputils`
closure, and the frozen install request moved from `cluster-install-agent-v4`
to `cluster-install-agent-v5` for the publication root, the serving
certificate, the virtual IPs and the rendezvous address. There is no
older-version reader, so an incomplete operation or context planned by an
earlier build is finished or destroyed with that build first, before this
build touches it. No other behavior changed.

#### B314

Every fetching insert publishes and verifies a fresh private hard link under
its own cryptographic token. Observation proves ownership through the source
image's device and inode; eject retires that node's token and proves absence.
Tests cover distinct tokens, post-eject HTTP 404, foreign inodes and paths,
verified publication and safe cleanup. Capability URLs enter no records.

#### B796

A follow-up found by the audit fixes of B314 and delivered on 2026-10-20. The
media block proves, before it creates, mints, publishes, relabels or removes
anything, the layout the install block's token walk accepts, through one rule
in the `containercluster_publication` module utility that both blocks use: no
symbolic link from the artifact server's content root down to the published
image, and every resolved ancestor above it owned by root and writable by
neither group nor other. Either block's refusal names the directory to fix,
never a URL or a token, so a layout the walk refuses is no longer published
first and then refused by every state read of the install block. No frozen
request version moved. **Guard tests:**
`test_a_symlink_from_the_content_root_down_refuses_naming_it`,
`test_a_symlink_beneath_the_reserved_root_refuses_naming_the_reserved_root`,
`test_a_resolved_ancestor_others_can_write_refuses_naming_it`,
`test_a_resolved_ancestor_root_does_not_own_refuses_naming_it`,
`test_a_symlinked_ancestor_is_accepted`,
`test_a_directory_missing_beneath_the_content_root_is_accepted` and
`test_a_missing_content_root_refuses_a_publication_and_is_nothing_to_remove`
(the shared rule); `test_an_accepted_layout_changes_nothing` and
`test_a_refusal_names_the_directory_and_no_url_or_token` (the media layout
module); `test_a_published_image_reached_through_a_link_refuses_naming_the_reserved_root`
and `test_the_module_names_the_directory_to_fix_and_no_url_or_token` (the token
module); `test_the_apply_proves_the_layout_before_it_includes_the_build` and
`test_the_destroy_proves_the_layout_before_it_removes_the_reserved_subtree`
(the media role's order). Each carries a mutation check recorded in its commit
body.

### X118 — M3c completion and bootstrap diagnosis

**Owner:** Container cluster. A safety slice of [M3c](m3.md#m3c), delivered
in-tree on `m3` at the explicit request of 2026-10-20. The delivering commit
records the verification commands and results. [B260](m3.md#b260) is
implemented at runtime and stays on the M3 page: it retains its
captured-response provenance and stalled-node operator gates, and modeled
responses are not live evidence. **Items:** B711, delivered in-tree.

**Outcome:** no media is released before a fresh authenticated read proves
completion, the release and every declared node. The release gate names a
stranded rendezvous host, and the destroy and fresh apply that follow, only
from a read in which every request was answered by the cluster with this
build's identity; otherwise it releases nothing and says the cluster could not
be read, that no media was released, and to repeat the apply. A failed
bootstrap stall, installer timeout or spent budget performs one bounded
assisted-host read, whose diagnostics lead with the nodes that never
registered, then the bad-state hosts with their messages and failing
validation IDs. The unregistered-node hint claims the cluster reached ready
only after a stall; after the installer's own timeout or a spent budget it
says only that installing starts once every declared node reports known, and a
read that names no node leaves the classification's own hint. The
[container cluster spec](../container-clusters.md) owns the behavior; the
[knowledge page](../../.agents/knowledge/openshift-agent-install-waits.md)
distinguishes recorded installer assets from modeled host responses.

**Guard tests:** `test_media_release_is_gated_by_a_fresh_read_after_the_installation_wait`,
`test_media_release_names_the_stranded_rendezvous_only_from_a_read_that_lacks_it`,
`test_a_replay_of_a_stranded_rendezvous_fails_the_same_way`,
`test_the_host_error_hint_names_a_destroy_and_a_fresh_apply_never_a_repeat`,
`test_a_failed_read_after_the_wait_releases_nothing_and_offers_no_destroy` and
`test_a_failed_node_read_cannot_prove_presence_from_partial_stdout` (B711);
`test_bootstrap_timeout_or_budget_spent_reads_hosts_once`,
`test_the_installers_own_timeout_reads_hosts_and_names_no_stall`,
`test_install_wait_timeout_never_reads_assisted_hosts`,
`test_bootstrap_diagnosis_leads_with_missing_then_bad_state_nodes` and
`test_success_host_errors_and_unrelated_failures_do_not_read_registered_hosts`
(the read and its hints); `test_registration_reports_bad_states_and_only_failing_validation_ids`
and `test_bad_validation_shape_keeps_the_host_status` (the read's parsing).
Each carries a mutation check recorded in its commit body.

**Constraints left behind:** none. No frozen request version moved.

#### B711

A fresh authenticated cluster read proves completion, release and every
declared node before any media release. Missing-node and replay tests prove
no eject occurs; a missing rendezvous host, in a read every request of which
was answered, names the stranded bootstrap control plane and destroy/fresh-apply
recovery, and a read that failed names no node. The shared API-only prerelease
read preserves the existing controller-call deadline margin.

### X119 — M3c serialization and compact journeys

**Owner:** Container cluster, with Lifecycle. An enabling slice of
[M3c](m3.md#m3c), delivered in-tree on `m3` at the explicit request of
2026-10-20. The delivering commit records the verification commands and
results. [B66](m3.md#b66) stays on the M3 page: it requires B61's accepted row
and a live compact rehearsal, and neither operator gate is claimed here.
**Items:** B712, B713, delivered in-tree.

**Outcome:** installations of one context run one at a time while media builds
overlap, and the compact example's three-master journey is qualified in-tree.
The [container cluster spec](../container-clusters.md) and the
[lab-compact example](../../examples/container-clusters/lab-compact/README.md)
own the behavior.

**Guard tests:** `TestTwoClusterPlanSerializesInstallationsOnlyGolden` and
`TestClusterInstallationsSerializeWhileMediaBuildsOverlap` (B712; the journey
holds the first installation at the gate and requires that no other block
arrives until it is released, failing every run when the exclusive key is
removed);
`TestLabCompactExamplePlansThreeMastersAndInstallerVIPsGolden`,
`TestCompactClusterContinuesAnInterruptedBootAfterResolvingMixedNodes`,
`TestCompactClusterUnknownWithForeignMediaRemainsUnresolved`,
`TestCompactRemovalNamesEveryMachineToStopBeforeRemovingAnyEffect` and
`test_compact_retry_boots_only_the_stopped_or_unbooted_nodes` (B713).

**Constraints left behind:** none. No frozen request version moved.

#### B712

Each cluster installation holds `container-cluster-install`; media builds
remain independent. A two-cluster plan golden and a scheduler journey prove
exclusivity and overlapping builds, following Q5's recommended serialization
policy.

#### B713

The [lab-compact example](../../examples/container-clusters/lab-compact/README.md)
declares three 6-vCPU, 16-GiB, 120-GiB masters, managed NAT networking,
installer-owned VIPs and a 4/8/40 capacity reserve. Its runbook carries B66's
staged, interrupted, killed-wait, stalled-node, settled and removal drills,
plus Q6's 25-hour image continuation. Effective-state and cluster-plan goldens
and continuation, mixed-state resolution, boot replay and three-Machine
`Stop first` journeys qualify the in-tree paths.



### X156 — Go toolchain vulnerability gate, go1.26.10

**Owner:** Architecture. A defect slice of [M1](m1.md), found at the
int-ready-v4a checkpoint of X57 on 2026-10-20 and delivered at the owner's
request of 2026-10-21, after CI failed on main at 88ddee07f. **Items:** B690.
**Decisions:** none.

**Outcome, B690:** Go moves from go1.26.9 to go1.26.10: `scripts/go` selects it
and both modules' `go` directives name it, so CI's setup-go installs the same
Go. go1.26.10 fixes GO-2026-6809, the distrust of compromised intermediate
certificates in `crypto/x509`, which the code reaches through
`internal/managedos/medialocal/acquirer.go`'s `download.Read`, so
`make vulncheck`, and with it `make check`, passes again. The
[version output](../cli/commands.md#version-output) example and the
[build knowledge](../../.agents/knowledge/build-toolchain.md) move with it.
Both modules' requirements are unchanged, and Go 1.27 stays parked
([B273](backlog.md#b273)). It repeats
[X67](#x67--go-toolchain-vulnerability-gate)'s move.

**Gates:** on go1.26.10, `make vulncheck` reports no vulnerabilities; `make
check-offline`, `make tidy-check`, `make modules-check`, `make docs-check` and
`make race` pass, and `./scripts/check-commits` accepts the commit. The
Ansible suites did not rerun, because no Ansible file changed. No real-host
run.

**Digest and version effects:** none; no automation, content or plan digest and
no request or record version moves, so a live context continues under a build
that contains X156. **Constraints left behind:** setup-go's cache key names the
Go version, so the first CI run after X156 starts with a cold Go cache.

### X135 — Accepted M4a definition

**Items:** B749. **Owner:** Add-ons, with Container cluster, Storage and Secrets.

### B749

The M4b to M4f contract, coverage and implementation baseline was defined in
commit `d5e7acb42` and accepted by the owner on 2026-10-21 (D174). Its
recorded `make docs-check` and `make focus` gates passed. Acceptance approves
the recommended baseline and authorizes no operator-run gate. The owner
separately requested M4b and M4c implementation and their named M3
prerequisites; M4d to M4f retain their explicit-request gate.

### X121 — Produced-input lending portion

**Item:** B64 (L5). **Owners:** State reconciliation and Secrets, with
Container cluster. Only B64 is delivered; B250 and B715 remain planned in
[M3](m3.md#planned-slices). The delivering commit records gate commands and
results; this delivery grants no cluster acceptance.

#### B64

Consumers freeze producer references and dependencies, then borrow several
produced entries through operation-scoped custody. Apply and continuation
withhold unproved material; removal can borrow retained entries. Missing,
undeclared and expired references refuse, and borrowed material is cleared.
The cluster-object adapter uses the lent kubeconfig with bounded authenticated
API access. [Produced material custody](../state-reconciliation.md#produced-material-custody)
owns the contract.

**Guard tests:** `internal/reconciliation/lifecycle/produced_inputs_test.go`
(multiple producers and kinds, continuation, unproved/missing/expired entries
and clearing), `internal/reconciliation/lifecycle/produced_test.go`
(non-disclosure and custody lifetime), and the cluster-object adapter tests.

### X137 — Typed built-in packages and diagnostics

**Items:** B321 and B752. **Owner:** Add-ons.
The [binding API](../api/addons.md) and [add-on lifecycle](../add-ons.md)
own the behavior. The delivering commit records in-tree gate results.

#### B321

Admission names each missing input, capability or add-on at its indexed field,
includes a remedy and identifies actual cycle members. Missing references
retain the schema diagnostic instead of duplicated domain errors.

**Guard tests:** `internal/addons/instances_test.go` and the add-on admission
and binding tests.

#### B752

Bindings select typed built-in packages through an immutable registry;
normalization and decoding retain defaults. Registry qualification rejects
duplicate OLM identities and unread capabilities, with explicit configuration
examples proving conditional readers. Actual instance configuration drives
capability dependencies, cycle checks, Last ordering and admission hooks over
cluster facts. Authored bindings remain on their existing path.

**Guard tests:** `internal/addons/instances_test.go`,
`internal/addons/packages/registry_test.go` and
`cmd/bootwright/package_admission_test.go` (production schema, hooks and typed
decoding). Package installation acceptance is separate.

### X136 — Cluster object program and adapter

**Items:** B750 and B751. **Owner:** Add-ons, with Container cluster.
The delivering commit records the exact in-tree gate commands and results.
Native operator acceptance remains with the consuming package's rehearsal.

#### B750

The immutable `cluster-objects-v1` program defines bounded objects, readiness
checks, Secret placement and exact/dependent approvals, canonical digests,
ownership receipts and observation proofs. Instance and network capabilities
share the driver. [Cluster object program](../add-ons.md#the-cluster-object-program)
owns the contract.

**Guard tests:** `internal/addons/clusterobjects/program_test.go`,
`internal/addons/clusterobjects/evidence_test.go` and the shared capability
contract suite.

#### B751

The native adapter executes frozen programs through `oc`, creates owned
objects without patching existing ones, compares continuations, checks
readiness and performs only the bounded InstallPlan approval mutations.
Ambiguous or foreign evidence stays unknown or refuses; native observations
exclude Secret values.

**Guard tests:**
`ansible/collections/ansible_collections/bootwright/core/tests/unit/test_cluster_objects.py`
and
`ansible/collections/ansible_collections/bootwright/core/tests/unit/test_cluster_objects_programs.py`.
No live Kubernetes API acceptance is claimed.

### X140 — Built-in package listing

**Item:** B754. **Owners:** Add-ons and CLI. The delivering commit records
in-tree gate commands and results.

#### B754

`add-ons list` presents built-in package metadata in human and JSON output
without a context or network access. `add-ons add` and `add-ons delete` are
removed from the command catalog, help and completion. The
[command catalog](../cli/commands.md) owns the surface.

**Guard tests:** `cmd/bootwright/main_test.go`, CLI catalog and completion tests,
and listing output goldens.

### X142 — Cluster address facts

**Item:** B756. **Owners:** Container cluster and Add-ons.
The delivering commit records in-tree gate commands and results.

#### B756

Package admission receives canonical machine, cluster and service networks,
node interfaces and routes, resolved endpoints and reserved address holders.
Derivation reuses endpoint-ownership claims and is stable under catalog
permutation. [Cluster facts](../api/addons.md) owns the package boundary.
Gateway and secondary-network extensions remain B758, whose prerequisites
and live proof are still open.

**Guard tests:** `internal/containercluster/addon_facts_test.go`
(endpoint sources, node composition, claim parity, determinism and network
settings), and package pool admission tests.

### X58 — iSCSI refusal

**Item:** B481. **Owner:** Storage and Desired state. D176 accepts the
implemented refusal as the closing outcome under OD27, resolving D161's
remaining owner decision. This closes B481 alone, with no iSCSI provisioning
or vendor support-status claim.

#### B481

An authored `spec.ceph.services` entry with `service_type: iscsi` refuses at
its `service_type` during full and partial validation before any effect. The
diagnostic directs block-access consumers toward NVMe-oF without echoing
authored service identifiers or credentials. The owning contract is
[native service admission](../storage-native-api.md).

**Guard tests:** `internal/storage/native_test.go` covers full admission,
partial admission and protected diagnostic output; `internal/storage/partial_test.go`
covers Environment-default admission. These are offline refusal gates.

### X47 — Verified emulated controller trust and bounds

**Items:** B446, B447, B449. **Owners:** Substrate and Managed OS. The closure
audit confirms the existing implementation meets these in-tree exits. B496
retains the separately split CA-issued physical-copy rehearsal; B73, B305 and
B78 retain their operator gates. No emulator or hardware acceptance is added.

#### B446

The emulated controller serves HTTPS using its declared certificate/key. Role
template goldens pin serving paths and mounts; substrate, machine-power and
installation request fixtures carry the same verified HTTPS endpoint and
declared certificate anchor. `test_redfish_control_trust.py` exercises real
loopback TLS with issued and self-signed certificates, and refuses an unrelated
anchor or hostname.
`internal/substrate/admission_certificate_test.go` rejects a generated serving
certificate that omits the provider bind address. D175's issuerRef additionally
supplies B496's CA-issued rehearsal input without changing the CA-bundle rule.

#### B447

D121's manager-switch precedence is implemented in the shared Redfish client
and stated in the substrate, Machine API and knowledge contracts. The four
`test_redfish_boot.py` cases cover manager enabled/device read-only false,
manager disabled before any write, device fallback with no manager switch,
and refusal of disable-verification under an authoritative manager. D104 is
explicitly amended by D121. Physical switch observation remains with B305.

#### B449

The controller insert allowance includes `(8 + 5 + 1) * 30` seconds for
certificate inventory, setup and one conditional retry. Substrate bound tests
pin native constants, documented steps and the 46m20s insert figure; the
installation margin test pins all controller calls and the resulting
4h50m50s reference installation deadline. Omitting the certificate allowance
fails those guards. The managed-OS contract records the additional seven
minutes. These tests qualify the bounded adapter contract without running an
installer.

### X62 — Controller listener firewall inspection

**Item:** B318. **Owners:** Controller and Infrastructure services. Preflight
and plan report per-socket runtime firewall admission under D110. Apply refuses
closed or unknown required listener and dependent-consumer paths before
registration. The controller stage remains available to establish prerequisites.

#### B318

The read-only firewalld adapter binds an exact local address to its interface
and runtime zone, qualifies explicit TCP/UDP port admission and structurally
bounded ICMPv6-only CONTINUE policies, and returns unknown for other policies,
source zones, rules, ambiguous interfaces or unavailable firewalld. Wildcard listeners retain
explicit unknown rows when no exact endpoint exists. Unknown never passes
preflight; the diagnostic supplies the operator-owned verification remedy.

**Guard tests:** `internal/controller/firewall/check_test.go` covers loopback
omission and unknown admission; `internal/controller/firewalllocal/inspect_test.go`
and `policies_test.go` cover bounded native queries, open/closed zones and
ambiguous rule shapes. `cmd/bootwright/wiring_firewall_test.go` exercises plan,
preflight, early dependent admission and wildcard projection. CLI goldens cover
human and JSON results. These are in-tree gates; B78's hardware qualification
and B490's cluster reachability retain their separate exits.

### X59 — Vendor package source admission

**Item:** B620. **Owner:** Storage and Desired state. Delivered under D169
as one bounded lane of X59; B493's disconnected delivery remains open.

#### B620

The storage schema accepts only `vendor` for the optional IBM package source.
Unused subscription entitlement and repository fields refuse as unknown
fields, and `subscription` refuses as an unsupported source naming `vendor`.
The example and admission fixture use the implemented vendor source. Machine
registration remains owned by the installation profile.

**Guard test:** `TestCephPackageSourceRefusesUnusedSubscriptionFields` in
`internal/desiredstate/compilation/storage_package_source_test.go` exercises
the production decoder's object, field, code and remediation. The delivering
commit records exact in-tree checks and their results; this item has no
operator-run gate.

### X52 — Shared storage admission and actionable diagnostics

**Items:** B315 and B317, moved from M4 on 2026-10-10 (D128).
**Owner:** Storage, with Desired state. Verified and completed on 2026-10-23
under the owner's request to finish M2 blockers needing no owner input.
The remaining X52 items stay on [M2](m2.md#planned-slices).

B315 shares intrinsic invariants between partial and complete admission;
`TestPartialValidationIssuesAlsoRefuseTheCompleteObject` in
`internal/storage/partial_test.go` guards parity. Its implementation was
already present; this delivery records the previously unrecorded exit.

B317 removes the generic invariant remedy. Each call now supplies its own
corrective action, and Ceph Entitlement diagnostics name the selected product
and exact field with registry or RHSM actions. Admission conditions, issue
ordering and frozen requests are unchanged. The regression tests
`TestStorageContradictionsNameCorrectiveActions` and
`TestCephEntitlementDiagnosticsNameProductAndField` in
`internal/storage/remediation_test.go` failed before the fix and pass after it;
existing exact-remedy assertions cover pool, image and cross-cluster cases.
The delivering commits record exact in-tree commands and results. Neither
item requires an operator-run acceptance row.

### X157 — Foreign client refusal

**Item:** B621 (found on 2026-10-18). **Owners:** Storage and CLI.
Delivered on 2026-10-23 under the owner's M2 completion request.

The coordinator preserves the native `foreign-client` refusal and the Go
adapter names the frozen `client.<name>` principal, explains that this
operation did not create it and supplies both safe external remediation and
the destroy-before-input-change path. It never adopts the foreign principal.
The request, evidence and mutation contracts are unchanged.

**Guard tests:** `test_a_foreign_client_refusal_keeps_its_named_reason` in
`test_ceph_core.py` and
`TestAForeignClientRefusalNamesTheClientAndFrozenInputRecovery` in
`internal/storage/ceph/refusal_test.go` failed before the fix. The reason-map
parity test covers the Go/Python boundary. The delivering commits record exact
in-tree checks and their results; this diagnostic item has no operator gate.

### X158 — Package metadata encoding barrier

**Item:** B511 (found on 2026-10-12). **Owner:** Storage.
Delivered on 2026-10-23 under the owner's M2 completion request.

One bounded streaming guard checks `repomd.xml` and every acquired metadata
member before native package processing. It rejects NUL bytes, non-UTF-8 byte
order marks and XML encoding declarations other than UTF-8 or US-ASCII,
including split headers and late NUL bytes. It retains the existing standard
comps-prolog exception, original-byte checksum verification and expansion,
download and deadline limits. No request version changes.

**Guard tests:** `test_ceph_package_sources.py` exercises repository and member
encodings, accepted UTF-8/US-ASCII, byte order marks and short reads. New
regressions failed before the guard. The delivering commits record the exact
focused and full checks, race gate and independent safety review; this
host-independent barrier has no separate operator-run gate.

### X159 — Runbook diagnostics and retained SSH policy

**Item:** B509 (opened on 2026-10-12, moved from M2 to M1c under D158).
**Kind:** enabling. **Owners:** Controller and Storage. Delivered on
2026-10-23 under the owner's answers D178 and D179.

D178 permits runbook-only diagnostic dependencies to be installed and removed
by that runbook. Bootwright's own package requirements remain limited to
what its execution needs. The
[lab-rhel procedure](../../examples/machines/lab-rhel/README.md) preserves its
independent DNS-answer and NTP-offset checks and adds operator installation
of only absent diagnostic roots. A private package record stays outside the
frozen input and is retained across interruptions; cleanup selects only those
recorded roots, disables dependency cleanup and requires review of the native
transaction so pre-existing packages and reverse dependents are preserved.
The procedure states partial-install recovery and removal limitations.

B509's revised documentation gate is complete; no package transaction or live
run was performed. The delivering commit records shell syntax and isolated
command-double checks, documentation checks and the full repository gate.
There is no separate operator-run acceptance gate for this procedure item.

D179 retains the implemented guest SSH policy, reload and include-closure
proof, resolving the pending D135 review question. It removes no transport
restriction and supplies no live qualification evidence. M2c's service and
matrix acceptance and M2d/M2e's physical/stretch gates remain open.
