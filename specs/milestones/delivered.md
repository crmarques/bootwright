# Delivered

Completed milestones and delivered out-of-sequence slices. Each keeps its
owner, delivered outcome, the tests that guard it, the constraints it left
behind and links to the owning specs, which own the behavior. Further delivery
evidence lives in Git history. [Milestones](../milestones.md) owns status and
the completion rules.

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
journey and SSH placement against a second OS-ready host, is unrecorded. Its consumer is now [`examples/lab-rhel`](../../examples/lab-rhel).

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
[client area](../contexts/controller-record.md#bundles-and-client-areas): a
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
block [`examples/lab-rhel`](../../examples/lab-rhel) plans; and `make check`.

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

**Outcome:** [`setup --purge-old-bundles`](../controller.md#supported-host-and-dependency-selection)
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
`TestFailedBlocksAreRetriedOneAtATime`,
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
`TestAnsibleLatestIsTheNewestQualifiedPatch`,
`TestPythonLatestIsTheNewestQualifiedMinor`,
`TestALifecyclePlacementHostConnectsAsRoot`,
`TestAPlacementNeverBindsAnEscalationSecret`,
`TestDocumentationLeavesTheAutomationDigest`,
`TestTheRunnerComparesOnlyTheDigestedAutomation`, and the collection tests
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
`TestReadOnlyToolInspectionStreamsTheSourceAndEachMember`,
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
in-tree ([B73](m4.md#b73)); the media probe names the controller's file because
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
[B238](backlog.md#b238)'s purge-report clause. The setup help text and its
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
`ansible/controller/requirements.txt`,
`internal/controller/bundlelocal/catalog.json` and the import probe in
`internal/controller/bundlelocal/probe_linux_amd64.go`, which still name
3.13.15 and the earlier wheels, so no automation or bundle digest moves.

**Guard tests:** `scripts/tools/ansible_check.py` refuses any interpreter but
the pinned one and any package whose version differs from the lock;
`TestQualifiedAnsibleMinorAgreesEverywhere` holds the lock's ansible-core to
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
restore keys, and setup-go's key names the Go version. Under D48 every follow-up was parked: [B272](m1.md#b272), CI's race job,
which never runs although two pages say it runs nightly;
[B273](backlog.md#b273), Go 1.27; and [B274](backlog.md#b274), the gate on
CPython 3.14.

### X38 — the real-host run of 2026-10-05

**Owner:** State reconciliation; run by the operator, who ran
[lab-rhel](../../examples/lab-rhel/README.md#run-it) end to end on a host and
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
which B49 also named ([B277](m1.md#b277)). It found [B275](#x40--machine-commands-status-secrets-and-validate-refusals) and
[B276](m1.md#b276) and a new sighting of [B270](m1.md#b270), all parked
under D48 until D56 attached them to M1. Lifecycle concurrency may now rise
above one through [B303](m3.md#b303), and the adapter protocol, roles and
codecs may collapse through [B19](m1.md#b19), [B20](m1.md#b20) and
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
leaves the cancellation running ([B339](m1.md#b339)). A host an
earlier build already left with a partial bundle file under its final name
still refuses that replay ([B356](#x42--media-contexts-setup-service-and-machine-admission)); X39 prevents new
ones. Tracked elsewhere:
`--ssh-id-file` ([B283](#x40--machine-commands-status-secrets-and-validate-refusals)); `status` over a revision a new rule
refuses ([B226](backlog.md#b226)); a bind-time refusal naming its Secret
([B285](#x40--machine-commands-status-secrets-and-validate-refusals)); the emulator's URI rendered as data
([B295](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); the FIPS key types of the bare-metal README
([B73](m4.md#b73)); a cancellation inside a binding's publication, which
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
left to [B300](m1.md#b300)); the status JSON's `secrets.bound` becomes
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
decision in [B357](m1.md#b357).

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
([B357](m1.md#b357)). Add-on and Machine checks still repeat a refusal for a
target that failed decoding ([B358](m1.md#b358)); until a replacing removal
starts its first block, the object the replaced attempt failed on reads done
([B359](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json)); a YAML parser error can name the line before its own
([B360](m1.md#b360)); the uninitialized-store refusals and the retired
file-source remedy name no `--context` ([B361](m1.md#b361));
`machine list --power-status` reads controllers with no progress row
([B370](m1.md#b370)); the context store's uncertain secret-state publication
names no inspection command ([B371](#x42--media-contexts-setup-service-and-machine-admission)); a file-input secret
replacement, deletion or rotation prompts under the host-wide lock and lease,
which the owner decides ([B372](m1.md#b372)); and nothing reports a sudo
policy that logs a standard-input secret ([B373](m1.md#b373)). Under D116 these
nine join M1 on X46. A Secret version stored before X40 stays current only
while its declaring path and document index are unchanged, so the first
re-import from another directory stales it once ([B362](backlog.md#b362)).
Tracked elsewhere: the confirmation prompts of `secret set`, `delete` and
rotation, which still call the Secret a context
([B287](#x41--lifecycle-results-the-controller-stage-installation-shapes-and-bounded-runs), D75);
the `media list` row ([B300](m1.md#b300)); the `secret set` help
([B95](m1.md#b95)); codes the registry scan cannot see
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
[B379](m1.md#b379) records, the tool-location remedy names the context's
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
`--authorize data-loss`. [B95](m1.md#b95) keeps every other command's help for
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
`TestTheCompiledExecutionFoundationIsUnchanged` and
`TestAResolvedBundleIdentityDoesNotReadTheCatalogPins` pin the execution
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
it ([B377](m1.md#b377)); `status` still offers a continuation that the
binding, automation-digest or other continuation proofs refuse, among them an
apply over an unbound incomplete apply and a destroy over an unbound running,
paused or unknown removal ([B378](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json)); an apply's or a deletion's
uncertain controller-record publication and an execution-foundation failure
the stage reaches still name setup's retry ([B379](m1.md#b379)); lifecycle
resolution, controller-client, pre-boot, agent-install and one context-store
remedy still lack an exact command, its context or the plan's tokens
([B380](m1.md#b380)); two Machine network admission refusals keep a slogan
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
binding ([B361](m1.md#b361)); container-cluster admission remedies
([B313](m3.md#b313)); content the install line still drops, search domains
and IPv6 policy among it ([B338](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation), [B326](m4.md#b326)); RHEL 9.8's
`rpm` output and the snapshot read as the helper account
([B335](backlog.md#b335), and the RHEL 9.8 controller run D60 plans); the
glibc and libgcc builds a RHEL 9.8 controller must hold, and building on a
workstation ([B292](#x42--media-contexts-setup-service-and-machine-admission)); help beyond destroy ([B95](m1.md#b95)); an
invocation's own in-flight jobs ([B304](m3.md#b304)); and a bounded run
beside another context's exclusive apply lock ([B18](m3.md#b18)). Under D48
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
retirement stays parked as [B322](backlog.md#b322)). A purge dry run plans the
retirement, and a purge that removed nothing prints "Retired none". The
execution foundation gains a named check in setup and preflight: the catalog
attributes each release's glibc and libgcc files to their package and build
beside the execution profile; a drift refuses before the plan, naming the
file, package and build with a reinstall, versionlock and repeat remedy that
also serves apply and destroy; and `scripts/foundation-catalog` regenerates a
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
attachment's `vlan` (with [B74](m4.md#b74)) and `hardware.boot` leave
v1alpha1 and refuse as unknown fields, and bare metal no longer requires an
`attachmentRef` (D94). An invalid Machine name or an unresolved fleet key
yields only its own diagnostic, and the fqdn grammar is the API's DNS rule.
The machines and substrates specs state each rule, the kind tables match the
schema, and the lab-baremetal and multidc-platform examples drop the removed
fields.

**Toward B342 and B380:** the libvirt bridge and a Machine's
`interfaceAttachments[].interface` take the Linux interface-name grammar,
which the machines spec's tables state; [B342](m1.md#b342) keeps the
templates' escaping of the bridge for X46's digest window. The context store's
refusal of a context with no revision carries its remedy as the remediation,
which [B380](m1.md#b380) no longer lists.

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
`TestTheCompiledExecutionFoundationIsUnchanged` pins, and the cancellation
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
templates' escaping of the bridge ([B342](m1.md#b342)); the generic
execution-failure remedy that an rpm lock conflict, an unsafe root or an
unverified bundle location still gives when reached from apply or destroy
([B379](m1.md#b379)); a receipt that this build leaves pending after a native
action refused before authorizing, which only the build that moves the
automation digest can cancel (D93, [B297](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); realization reading
the uncomposed template, so an override that adds the only ethernet interface
passes validate and refuses at plan ([B338](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); overlapping managed
prefixes across contexts ([B295](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); a managed service's egress
through a managed or authenticated Proxy, refused only at plan
([B382](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json)); the RHEL 9.8 foundation record, for the RHEL 9.8
controller run D60 plans to confirm ([B335](backlog.md#b335) beside it); and a
guard port that lists the owned blocks ([B324](backlog.md#b324)). Under D116
these join M1 on X46: an elevated selection refusal drops its remedy
([B398](m1.md#b398)); the `context delete` row still promises a listing of
what an orphan-acknowledged deletion abandons, and a lost context's plan says
its keyring is removed ([B399](m1.md#b399)); unsafe mutation evidence has no
named exit ([B400](m1.md#b400)); stage-collection, bounded-run and setup-run
refusals name absolute paths, and setup's store backstops name no remedy
([B401](m1.md#b401)); the dry run's state-root inspection follows a
symbolic-link ancestor the store refuses ([B402](m1.md#b402)); a setup action
whose adapter failed before any native record stays unknown
([B403](#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation)); a `contextStore` serving certificate is proved only
after registration, and missing material names no Secret
([B404](#x24--adapter-protocol-managed-service-role-observation-reasons-and-canonical-json)); the `metadata.name` grammar refusal names no object or
remedy ([B405](m1.md#b405)); a managed service's name can outgrow its block
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
D98, D107, D116. **Partly delivered:** [B270](m1.md#b270), its emulated BMC
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
stop signal. The proxy unit's half is X24's ([B20](m1.md#b20)); B270 keeps its
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

**Constraints left behind:** [B270](m1.md#b270) keeps the proxy unit's stop
([B20](m1.md#b20), X24). A Machine and an InfraProvider of one name share a
directory, and the Machine's destroy can remove the provider's pool
([B420](m1.md#b420)); the 64-key reservation bound meets an unbounded
attachment list ([B421](m1.md#b421)); the install profile still admits fields
it ignores and repository IDs dnf refuses ([B422](m1.md#b422)); two Machine
refusals keep the slogan remedy ([B423](m1.md#b423)); a zero-prefix default
route spelled otherwise is refused ([B424](m1.md#b424)); a controller-stage
install or a canceled setup that failed before any native record is still
`unknown` ([B425](m1.md#b425)); and the failed media row of a directory has no
working remedy ([B426](m1.md#b426)). The installation request's SSH placement arm,
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
acceptance:** [B19](m1.md#b19), [B20](m1.md#b20) and [B270](m1.md#b270); their
rows stay on M1 with Delivery `awaiting operator acceptance`. **Partly
delivered:** [B233](m1.md#b233), its dead reporters.

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
unknown apply resolves its blocks with the apply's own check ([B440](backlog.md#b440)).

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
capabilities are removed. [B233](m1.md#b233) stays on M1 for the unreachable
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
validation pins), and two not confirmed, which are kept as [B436](m1.md#b436) and
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

**Constraints left behind:** [B19](m1.md#b19), [B20](m1.md#b20) and
[B270](m1.md#b270) complete on an owner-accepted ledger row of D59's closing
run, which carries their real-host apply, destroy and no-failed-unit proofs; the
other host observations are [B442](backlog.md#b442). The binding-proof port
stays [B302](m1.md#b302)'s and the artifact server's `Unsupported`
[B233](m1.md#b233)'s. Under D116 three follow-ups join M1 on X46: a destroy that
refuses a content root with a `..` component ([B435](m1.md#b435)), the reset of
a failed unit after a failed apply ([B436](m1.md#b436)) and three continuation
edges ([B437](m1.md#b437)). Under D48 eight were parked: B438 to B445.
