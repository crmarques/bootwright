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
need adapter changes ([B29](m1.md#b29), was S31) and the protocols the
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
`TestABoundedRunWhoseBindingARegistrationCollectedBindsAgain`,
`TestEvidenceReportsWhatAResolutionProved`,
`TestResolutionOutcomeRecordsWhatTheCapabilityProved`,
`TestTheMarginAllowsEveryControllerCallAnApplyMakes`,
`TestAnSSHPlacedInstallationListsEachMaterialOnce`,
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
