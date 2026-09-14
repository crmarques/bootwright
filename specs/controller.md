# Controller prerequisites and setup

Controller owns inspection and preparation of the local host on which
Bootwright runs. This contract defines `setup` and `preflight controller`;
[milestones](milestones.md#m1d--controller-setup) own delivery and qualification
status. Setup never provisions an operating system or executes a managed
service's lifecycle.

Prerequisites divide by what selects them. **Host prerequisites** are
context-independent: every context on this host needs exactly the same ones, so
`setup` owns them and reads no desired state at all. **Context prerequisites**
are selected by one Environment's own graph, so the
[controller stage](state-reconciliation.md#stages-and-the-pause-boundary) of
that context's apply owns them. Each prerequisite belongs to exactly one side,
and neither command performs the other's work.

| Scope | Prerequisites | Owner |
| --- | --- | --- |
| Host | Provided OS, architecture and verified installed-host identity; the fixed root and its controller record; the private Python and `ansible-core` execution bundle with the embedded automation; the baseline native closure of container runtime, SSH and NMState clients. | `setup` |
| Context | The target client closure the selected graph needs; the libvirt client closure a declared `libvirt` capability or referenced libvirt provider selects; the binding between this context and this host. | `apply --stage controller` |

A host prepared once therefore serves every context later created on it, and a
context that selects nothing beyond the baseline needs no controller stage.
The [Environment-selected controller Machine](api/environment.md#controller-machine)
names the host a context expects to run on; `setup` never reads it.

## Supported host and dependency selection

Setup supports **RHEL 9 and Fedora on Linux/amd64**. Each supported
combination identifies an exact OS release, tested kernel/filesystem and
privilege primitives, package repositories, native package-manager version,
package builds, and immutable execution bundle. The exact qualified releases
are recorded with the compiled dependency catalog
(`internal/controller/bundlelocal/catalog.go`) and in
[development](../docs/development.md), never in this contract. Each setup
freezes source identities, byte counts and SHA-256 values. A family name alone does not
qualify all minor releases, Fedora releases or future updates. Unsupported or
unprovable combinations refuse before installation. The exact matrix and
dependency locks must remain consistent with that exit evidence.

The baseline selects CPython from python-build-standalone, `ansible-core`,
their supporting wheels and urllib3 for bounded Ansible-owned downloads.
Every host dependency resolves to latest stable: no Environment declares their
versions, because setup reads none. The
[Environment version policy](api/environment.md#dependency-versions) declares
only the versions a controller stage installs. Resolve Python and Ansible
independently, then use the
selected interpreter's maintained pip resolver with exact roots and wheel-only
downloads. Incompatibility is a refusal, not permission to choose an older root.
Freeze the complete wheel closure, metadata and resolver identities. Production
publishes the verified fixed file projection; unsupported scheme relocations,
`.pth` files and startup hooks refuse. No package resolution occurs during
installation or retry, and entrypoints are repository-owned.
The exact Python archive's internal file links become regular private files;
directory links and escape/cycle attempts refuse. Retained source bytes derive
the complete expected file inventory, including executable modes, so
publication rejects additions and substitutions rather than trusting a version
string. Once the bundle is sealed, readiness confirms only that its published
files are still present.

The private interpreter runs isolated from ambient Python, loader and Ansible
configuration: no system or user Python, pip configuration, Ansible
configuration, environment search path, loader cache, library search variable
or preload configuration can alter the closure or select executable code, and
publication rejects additions and substitutions. The provided host supplies the
qualified glibc and libgcc foundation; setup never replaces those shared
libraries, freezes their package ownership with the execution profile, and
refuses any native transaction that would change them before presenting the
plan. A dependency release that requires a different foundation needs a
separately qualified profile. The mechanisms that achieve this are recorded in
[controller runtime isolation](../.agents/knowledge/controller-runtime-isolation.md).

The publisher and platform policies are compiled into the executable; exact
releases are resolved by explicit setup. Dependency selection follows
[dependency selection](architecture.md#dependency-selection-and-reuse) and
[supply-chain integrity](security.md). It distinguishes:

| Prerequisite | Selection and allowed setup |
| --- | --- |
| Host foundation | Verify the provided OS, architecture, local identity, account/sudo boundary, filesystem containment/durability, free-space limits and trusted package sources. No OS installation, release upgrade, repository enrollment, entitlement registration or reboot. |
| Baseline execution bundle | Publish the resolved exact Python and `ansible-core` closure in an isolated Bootwright-owned location. Do not use system/user Python imports or ambient Ansible configuration. |
| Container runtime | Setup selects Podman for every prepared host, so a controller that declares `container-runtime` finds it ready. Install or update the approved dependency set and verify an existing exact runtime without taking ownership of its containers or configuration. Do not start a service, pull a managed-service image or create a container. |
| Native target clients | Selected by the admitted desired-state graph, so the controller stage owns them. OpenShift/OKD clients (`oc`, `kubectl`) and installer match the selected release; Kubernetes consumers select Helm; referenced vSphere providers select `govc`; virtualization selects upstream `virtctl`. A declared `libvirt` capability or referenced libvirt provider selects `virsh` and its native client dependencies. Setup selects none of them; the SSH and NMState clients that support the baseline flows are host prerequisites. Install these with the fixed Ansible controller role. |
| Service execution | Service images, containers and lifecycle configuration remain with their service consumer. Installing a client grants no authority to contact or change a target. |

Setup installs missing dependencies, updates selected dependencies to their
resolved versions and publishes immutable private bundles. An explicit native
root version may require a reviewed downgrade of that root. Supporting package
upgrades must be required by the solved closure. Refuse unrelated removals,
dependency downgrades, vendor changes, protected-foundation replacement and OS
release upgrades. Setup does not replace the running Bootwright executable,
modify shell profiles or global tool search paths, run a general package update,
or repair unrelated files. Present each install, upgrade or explicit downgrade
with exact before and after identities before confirmation.
Approved publisher package hooks are part of the dependency transaction: the
Ansible package role must run required hooks and verify their postconditions.
It must not suppress required hooks and then claim a usable runtime. Existing
operator SELinux policy remains operator-owned. The native package manager
performs required labeling and policy hooks; setup does not toggle enforcement
or replace local policy. The native transaction verifies the identities and
installed nonconfiguration files of what it installs; readiness afterwards
confirms only that each selected root package is installed by name, whatever
release the host carries. Workload access under that policy is verified by its
flow consumer.
A list of package names alone is not a complete plan.

Each native selection has its own content identity, including the host platform
and required client capabilities. A completed baseline bundle cannot prove an
additional libvirt requirement. The Fedora catalog includes a separate libvirt
client closure without installing virtualization daemons. The public RHEL
baseline source does not include that client; a selected RHEL libvirt requirement
refuses before acquisition until an approved entitled source is defined.

Python, Ansible and generic clients without exact desired-state versions resolve
latest stable during explicit setup, before confirmation, and only when no
retained resolution serves the selected platform, version intent, native
requirements and target tools under the running executable. Native packages use
the latest available build for the selected OS release and approved repository
set. OpenShift/OKD installer and clients remain tied to the target release.
A cluster pinned to a release image still selects its clients from the declared
`release.version`; without that version setup refuses rather than inferring a
client release from the image.
`virtctl` uses upstream latest unless explicitly overridden; no remote target
discovery or automatic compatibility inference is required. Freeze source URL,
version, byte count and publisher SHA-256 in the plan and receipt. Mirrors
change acquisition only. Dry-run and preflight do not discover versions. Setup
never checks for newer releases: a serving retained resolution is reused as
frozen, a ready controller reports unchanged without publisher or repository
access, and a controller missing part of that closure installs only what is
missing. Native packages are solved again only when a selected native root is
not installed, because the frozen transaction binds the host's exact
before-inventory; the retained Python, Ansible and target-tool resolution is
kept. A retained resolution the running executable cannot use, or whose
`latest` Ansible release falls below the collection minimum, is superseded by a
fresh resolution; a declared release below that minimum refuses. An incomplete
setup reuses its exact frozen resolution without metadata refresh. Moving a
`latest` dependency to a newer release requires declaring that release in
`dependencyVersions`.

A dependency is checked for identity, version and integrity when setup
acquires, publishes or installs it. Afterwards readiness is presence only, with
no version comparison: the sealed bundle's published files by count and size,
its retained sources by size, the private interpreter, each target tool's
source and files, and each selected native root package by name. The report
shows the frozen release as required and the installed release as observed.
Process exit status alone does not prove readiness. An older bundle needed for
retry or a future frozen lifecycle is retained; setup supplies no uninstall or
garbage-collection command.

## Selection and command journeys

`setup` selects no context. It ignores the invoking user's current selection,
resolves no desired state, consumes no explicit `--context` value, and selects
the host dependencies with direct download routing. This allows preparation
before context creation or Environment import, and makes one prepared host
serve every context created on it. No current directory, user profile or
ambient proxy selects inputs. Version intent for the host dependencies comes
from the compiled default alone, because no Environment can move it; the
[Environment version policy](api/environment.md#dependency-versions) declares
only the versions a controller stage installs.

`preflight controller` uses a named context **only when `--context` is explicit
and nonempty**. An explicitly empty value is omission. Nonempty names follow
the existing context-name grammar. An explicit context must be ready and have
an admitted input revision; an empty one returns `context.input` with the
import command. Every check carries the scope that owns it, so a negative
report names the one command that settles it: `setup` for a host prerequisite,
and that context's own `apply --stage controller` for anything its graph
selects. An unmet host prerequisite always wins, because the prerequisites a
context adds cannot be prepared on an unprepared host. Unavailable cluster or
service lifecycle commands remain unavailable. A controller requirement outside
the supported setup matrix does block setup before effects. Global SSH flags
remain unconsumed.

| Invocation | Required behavior |
| --- | --- |
| `bootwright setup --dry-run` | Produce deterministic dependency intent and actions from policy and bounded local file metadata. No stored evidence is read, so this stays below the privilege boundary. No dependency subprocess, network, Secret read, privilege escalation or write. Versions requiring live resolution and readiness facts requiring effects are explicitly unverified. |
| `bootwright setup` | Inspect, present the complete bounded local plan, confirm when it contains changes, prepare the host dependencies and verify every required postcondition. Publish no binding and record no context on the receipt. |
| `bootwright preflight controller` | Read and verify the host prerequisites with bounded local probes. May use the verified privilege boundary for private metadata and disposable local probe scratch. |
| `bootwright preflight controller --context <name>` | Additionally report that context's own target tools, libvirt client and host binding as context-scoped checks, by presence only. Contact no publisher and read no repository metadata for them. |

Neither command installs, downloads, refreshes repository metadata, contacts a
managed endpoint, creates or repairs shared state, or publishes a binding on
behalf of the other's scope. Preflight publishes nothing at all.

Dry-run does not execute a native package resolver. It lists required version
intent and identifies any transaction feasibility, live identity or readiness
evidence still needed by real setup. A valid dry-run exits successfully with
unverified checks visibly labeled; it never reports completed setup. Preflight
requires positive current evidence for all selected checks; an unbound context
is a failure that names its controller stage, distinct from host mismatch,
which names the restored host.

Real setup first resolves dependencies in disposable unprivileged staging.
This phase may download verified public bootstrap payloads, run wheel-only pip
resolution and use the provided OS's DNF4/DNF5 foundation to inspect a private
inventory snapshot and solve the exact native transaction. All metadata,
caches, logs and resolver outputs stay in bounded scratch storage. Scratch is
private Bootwright-owned durable temporary storage, never the shared ambient
temporary directory and never the verified context store, so an interrupted run
leaves no unrecognized state behind. Setup refuses before any effect when that
filesystem cannot hold the approved payload closure with headroom; exhausting it
mid-transaction is an unknown outcome. Isolate
downloaded resolver code from installed-host writes and ambient configuration;
root invocation drops to an unprivileged staging identity. This phase grants no
shared-state, host-package, Secret or managed-target mutation authority.

Present the frozen complete package closure and permitted effects before
confirmation. Only after confirmation and durable intent may setup publish a
private execution bundle or install host dependencies. DNF4 on RHEL 9 and DNF5
on Fedora own native solving and installation. Apply verified local RPMs with
repositories disabled, compare all resulting actions with the frozen plan and
refuse extra actions. Do not implement a separate dependency solver.
Revalidate the transaction, host and input under the held coordination boundary
before the first mutation. A changed transaction requires a fresh plan and
confirmation, never extra work silently appended to the approved plan.
Hold native package-manager coordination through apply. Direct concurrent RPM
or other operator mutation outside that coordination is unsupported
interference; native transaction checks still apply, and a differing final
inventory is an unknown outcome requiring recovery, never success.

The `controller-prerequisites-v2` Ansible adapter receives one frozen request
with platform, exact package/tool sources, a scoped bundle identity, declared
egress and optional retained preparation. The fixed controller setup playbook
composes `bootwright.core.controller_prerequisites`. It validates the request,
observes the before-inventory, and waits for Go to durably record preparation
before installation. Package effects use the native package manager under its
transaction boundary; target archives are verified before safe extraction of
only the named regular members into private versioned tool locations. Existing
files are verified, never overwritten. Inventory, request, downloads, expanded
members and callback frames have fixed bounds.

Go publishes only the pinned private Python/Ansible runtime and embedded
repository automation needed to start Ansible. All host-package and target-CLI
installation runs in the roles. Final bootstrap publication projects frozen
artifacts without a fresh pip resolution. The platform's Python/DNF runtime is
a separate provided foundation; it does not supply private Ansible imports.
Neither phase uses ambient Ansible or executable discovery. The automation content
digest participates in bundle and receipt identity. Go owns selection,
authorization, host/context coordination, durable state and product output;
Ansible returns bounded structured evidence through the runner protocol.
Task output is not an application API. Read-only Go verification reconstructs
installed bootstrap and target file inventories from approved retained sources.

`--yes` suppresses ordinary confirmation only. Use the existing bounded
yes/no confirmation semantics, with the plan before the prompt; decline,
noninteractive input without `--yes`, or cancellation starts no setup mutation.
A verified no-op needs no prompt or installed-host/shared-state writes.
Disposable resolution and local-probe scratch is removed after use. Setup
changes neither current-context selection nor Environment input, and claims no
context.

## Egress and local effects

With an explicit context, the controller Machine's normalized
[proxy choice](api/machines.md#machine-proxy) is the sole route selection.
Setup supports direct access and qualified unauthenticated external proxies using
the qualified system trust store. A managed Proxy, `proxyAuthRef` or
`trustBundleRef` is unsupported for setup and refuses before acquisition.
Never fall back to direct access or read Secret material to probe an
unsupported route. Authenticated/private-trust setup acquisition needs its
own Secrets consumer and recovery definition before promotion.

Reject a bootstrap dependency on a proxy or other service whose readiness
requires this setup or a subsequent apply. Admission of a reference is not
readiness evidence. Proxy routing applies only to setup's child processes and
download client; it does not reconfigure the provided OS. TLS verification,
trusted publisher metadata, digest checks, bounded redirects and acquisition
limits follow [Security](security.md). Package/source URLs contain no
credentials; ambient proxy, Python, Ansible, package-manager and executable-path
configuration cannot alter the approved closure.

Controller consumes typed local inspection, acquisition, package-transaction
and bundle-publication ports. Requests carry exact target evidence, dependency
identities, authorized actions, route, limits and cancellation. Results report
per-action postconditions and `unchanged`, `changed`, `failed`, `canceled` or
`unknown` outcomes; they contain no raw subprocess transcript. Pure policy
selects and orders actions; composition binds fixed local adapters. Local dependency installation crosses the same controlled
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary)
as managed automation. Playbook and role selection is fixed by Bootwright,
never supplied by desired-state input.

## Host identity and shared prerequisites

Controller verifies the executing installed host; a Machine name, address,
DNS answer or `access.local` cannot prove it. `linux-installed-v1` combines the
canonical 32-digit lowercase machine-id, canonical DMI product UUID and root
filesystem UUID. Missing, all-zero or all-ones identity evidence refuses. The
private host digest is SHA-256 over `bootwright.controller.installed-host`, the
provider and those three values, separated by NUL without a trailing separator.
Boot IDs, hostnames, kernel versions and mount namespace IDs are not durable
identity. This preserves an ordinary reboot while detecting relocation that
changes any required evidence.

Acquisition uses bounded trusted file handles, checks procfs/sysfs identity,
compares the executing process and PID 1 mount namespaces, and refuses known
container markers. The root mount's block source must match its trusted
`/dev/disk/by-uuid` entry. Btrfs uses its real block source rather than treating
an anonymous subvolume device number as the filesystem identity. XFS/ext4 also
cross-check the root mount device. Mutable ancestry or identity files refuse;
setup does not repair their permissions. These checks detect observable
substitution and namespace contradictions, not hardware attestation. A complete
clone preserving all three identity values and the relevant namespace view is
indistinguishable; copied identity bytes alone are never a claim of clone
resistance. Controller relocation and full-store restore remain separately
defined work.

[Workspace](contexts.md#controller-relationship-and-host-binding) persists the
verified identity and context relationship. Setup prepares the host without
claiming any context; the relationship is published by the first apply that
uses the host, under the same verified identity. Later operations and context
preflight verify that exact binding; they never silently rebind.
follows [controller-host protection](state-reconciliation.md#controller-host-protection).

Workspace serializes shared prerequisite mutation across every context in the
fixed root using its exclusive root lock, acquired before a context lease.
Preflight holds a shared root lock when the root exists, for coherent stored
evidence. An absent root reports missing setup with `setup` guidance
and creates nothing. A busy lock refuses; there is no timeout-based takeover. Before installation, reject any
package/runtime transaction that could alter dependencies in use or retained
by another setup or frozen lifecycle. The OS package-manager lock is an
additional requirement, not a replacement for Bootwright coordination.

No service port or container name is reserved by setup.
[Infrastructure services](infrastructure-services.md#host-reservations) defines
those conflict identities and joins this host coordination through the shared
root lock and the stored reservation record. Dependency readiness never
establishes service ownership or authorizes another context's resources.

## Publication and interrupted setup

Setup has a private durable receipt, separate from lifecycle operation state
and logs. Before the first installation or bundle-publication effect, record
the verified host, selected implementation and complete immutable local action
plan. For a context-bound attempt, retain its context name, controller identity
and input revision. Keep required input/binding evidence protected from update
and purge until the attempt has a definitive terminal outcome.

Each action has attributable before-state, fixed request, postcondition and
outcome. Record intent durably before starting it; record success only after
both postcondition verification and durable evidence publication. Interrupted
or failed setup may leave approved packages installed. Do not claim rollback
or remove shared OS dependencies automatically. A receipt is recovery evidence,
not a lifecycle operation ID, ownership of the host, or GitOps readiness.

An explicit repeat of the same setup command first resolves the exact pending
receipt. If no effect occurred, it may retry the same action. Positive proof
of the requested postcondition permits completion without repeating an install.
Contradictory or incomplete proof leaves the action unknown and blocks further
mutation with recovery guidance. Never turn a lost child result into failure
with a presumed no-effect outcome. Changed input, host identity or dependency
closure cannot replace an incomplete setup; restore the exact compatible
executable/dependencies and resolve that receipt first.

Cancellation stops authorization of new effects. Before native installation
starts, the runner terminates and reaps its process group. Once an authorized
package transaction is running, it waits for native package hooks to finish
and retains coordination until the child has exited; a timeout never grants
lock takeover. Abrupt process death can leave package effects or an incomplete
bundle. An explicit retry must prove either the exact before-inventory for
safe replay, or the complete expected after-inventory and every required tool
before recording success. Missing
or contradictory evidence remains unknown and requires operator recovery.
After the native transaction is proved complete, an attributable subset of
exact tool files may be completed by Ansible under the original source closure
and route. Publish each source and selected member atomically; never adopt a
partial download, overwrite a file or repeat a native transaction to repair
missing tool files. Each additional tool effect requires a live Go authorization.
Publication/sync failure authorizes neither cleanup nor an unrecorded retry.

Completed setup retains enough bounded host and bundle evidence for later
inspection; no-op repetition revalidates without republishing. A later input
or Bootwright automation revision may select a new bundle through a fresh setup.
Preflight reports an incompatible retained bundle without resolving a replacement;
an incomplete receipt still requires its original compatible executable.
An input
update may change controller prerequisite intent after setup recovery is
complete, but cannot silently change an established controller Machine/host
binding. Changes in readiness require another explicit setup. Deleting a
disposable context removes its binding/receipt references, never shared host
packages or bundles. Complete-store restore and relocation remain separately
defined work.

## Results and qualification

Both commands retain their existing text-only CLI surface. Order checks and
actions by catalog dependency order, then stable prerequisite identity. Show
the baseline or explicit context scope, required/observed versions, planned
changes, readiness and next safe command. Both commands stream
[long-running progress](cli/output.md#long-running-progress): the scope, then
one `Checks` row per host check as it is verified. Real setup adds one
`Resolving` step per dependency family it resolves before the plan, and one
`Progress` step per receipt action after confirmation, with the source, native
transaction or target tool in flight as its detail. Host fingerprints, private paths,
credentials, environment dumps and raw native-tool output are not public
results. No new JSON flag, lifecycle receipt or private operation log is added.

The [diagnostic taxonomy](cli/output.md#diagnostic-taxonomy-and-order) owns
`preflight.*` and `controller.*` failure codes; existing context, privilege,
output and cancellation codes retain their meanings. Failures include a safe
actionable next step. A failed acquisition names the publisher host it could
not use and distinguishes unresolvable names, an untrusted certificate, an
expired acquisition timeout and an unreachable endpoint, because each needs a
different operator action and none of them is a corrupt retained dependency.
Request paths, transport text and operating-system error strings stay private.
The [output contract](cli/output.md) owns streams and exit
status, including complete negative readiness reports and partial setup
outcomes. No setup failure may imply that already verified effects rolled back.

RHEL 9 and Fedora share one set of host-independent request/result tests
covering clean dependency installation, already-ready no-op, context-free
preparation, explicit binding, read-only preflight and dry-run, unsupported
combinations, dependency/identity substitution, multi-context contention,
protected dependencies, cancellation, package failures, uncertain publication
and exact retry. A fake adapter proves this contract and never proves an
executed native installer. Collection syntax, pinned lint, sanity, unit and
local integration checks qualify the shipped Ansible entrypoints as content.
[Milestones](milestones.md#m1d--controller-setup) own the verification model and
which acceptance remains operator-run.
