# Controller prerequisites and setup

Controller owns inspection and preparation of the local host on which
Bootwright runs. This contract defines `setup`, `preflight controller` and the
controller stage. Setup never provisions an operating system or executes a
managed service's lifecycle.

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
| Context | The target client closure the selected graph needs; the libvirt client closure a declared `libvirt` capability selects; the hypervisor closure a libvirt provider hosted on this Machine selects; the installer-media tooling an Anaconda installation published through an artifact server on this Machine selects; the binding between this context and this host. | [`apply --stage controller`](#the-controller-stage) |

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
| Native target clients | Selected by the admitted desired-state graph, so [the controller stage](#the-controller-stage) owns them. OpenShift/OKD clients (`oc`, `kubectl`) and installer match the selected release; Kubernetes consumers select Helm; referenced vSphere providers select `govc`; virtualization selects upstream `virtctl`. A declared `libvirt` capability selects `virsh` and its native client dependencies; a libvirt provider hosted on the controller Machine selects the [hypervisor closure](substrates.md#provider-host-realization) instead, and an Anaconda installation published through an artifact server on the controller Machine selects the [installer-media tooling](managed-os.md#installation). Setup selects none of them; the SSH and NMState clients that support the baseline flows are host prerequisites. Install these with the fixed Ansible controller role. |
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
installed nonconfiguration files of what it installs. A path the package
declares but does not install, such as a runtime directory its own daemon
creates, is owned by the host and is never read as a dependency defect.
Readiness afterwards confirms only that each selected root package is installed
by name, whatever release the host carries. Workload access under that policy is
verified by its flow consumer.
A list of package names alone is not a complete plan.

Each native selection has its own content identity, including the host platform
and required client capabilities. A completed baseline bundle cannot prove an
additional libvirt requirement. The Fedora catalog includes a separate libvirt
client closure without installing virtualization daemons. The public RHEL
baseline source does not include that client; a selected RHEL libvirt requirement
refuses before acquisition until an approved entitled source is defined.

Setup resolves Python and Ansible to latest stable during explicit setup,
before confirmation, and only when no retained resolution serves the selected
platform and native requirements under the running executable. Native packages
use the latest available build for the selected OS release and approved
repository set. Freeze source URL, version, byte count and publisher SHA-256 in
the plan and receipt. Mirrors change acquisition only. Dry-run and preflight do
not discover versions. Setup never checks for newer releases: a serving
retained resolution is reused as frozen, a ready controller reports unchanged
without publisher or repository access, and a controller missing part of that
closure installs only what is missing. Native packages are solved again only
when a selected native root is not installed, because the frozen transaction
binds the host's exact before-inventory; the retained Python and Ansible
resolution is kept. A retained resolution the running executable cannot use,
or whose `latest` Ansible release falls below the collection minimum, is
superseded by a fresh resolution. An incomplete setup reuses its exact frozen
resolution without metadata refresh.

[The controller stage](#the-controller-stage) resolves target clients under the
version intent the Environment's
[`dependencyVersions`](api/environment.md#dependency-versions) declares, and
retains them the same way. OpenShift/OKD installer and clients remain tied to
the target release. A cluster pinned to a release image still selects its
clients from the declared `release.version`; without that version the stage
refuses rather than inferring a client release from the image. `virtctl` uses
upstream latest unless explicitly overridden; no remote target discovery or
automatic compatibility inference is required. Moving a retained `latest`
client to a newer release requires declaring that release in
`dependencyVersions`.

A retained resolution that differs from the running executable only by the
automation it carries is not superseded: it is carried forward. Setup reads
that resolution's own approved sources from the sealed bundle holding them,
projects them again under the embedded automation, and publishes the result as
the new bundle that identity names. Every release, byte count, signer and
publisher origin is preserved, no publisher or repository is consulted, and the
native transaction is reused unless a selected root is missing. A source the
retained bundle cannot serve as its approved bytes is acquired from its
publisher as usual, and a resolution needing a different provided execution
foundation is refused rather than carried, because no local projection can
establish one. Preflight reports the incompatibility and carries nothing.

A dependency is checked for identity, version and integrity when setup
acquires, publishes or installs it. Publication reads the complete closure back
and qualifies the private interpreter once, and that verification is the
postcondition its caller decides on; the same closure is never read back and
probed twice for one effect. Afterwards readiness is presence only, with
no version comparison: the sealed bundle's published files by count and size,
its retained sources by size, the private interpreter, each target tool's
source and files, and each selected native root package by name. The report
shows the frozen release as required and the installed release as observed.
Process exit status alone does not prove readiness. An older bundle needed for
retry or a future frozen lifecycle is retained, and setup removes nothing of its
own accord.

**Retiring superseded bundles.** `setup --purge-old-bundles` retires the
execution bundles this host no longer needs, after the setup it runs beside has
completed and only then: a setup that refuses, fails or previews retires
nothing, because what may be retired is decided by what the new bundle now
holds. A bundle is retired only when it is positively identified as a
superseded execution bundle, which is one a retained resolution names and that
the completed receipt does not. Everything else is left untouched, including
every [client area](#the-controller-stage), which is shared host state no
context uninstalls, and any area this record does not account for.

Retirement is safe because a completed setup leaves nothing behind that needed
the bundles it replaces. Publication writes every source into the bundle it
publishes, whether that source was acquired or recovered from the bundle a
[carried-forward resolution](#supported-host-and-dependency-selection) read, so
the next carry-forward reads the bundle this setup just sealed. A lifecycle
operation always runs the bundle the current receipt names, so no frozen
operation reaches a retired one.

The resolution a retired bundle carries is retired with it, because a retained
resolution whose sources are gone can be carried forward from nothing. That is
what returns capacity: the retained resolutions and bundle areas a host may
hold are bounded, and without retirement the bound is reached and every later
setup refuses.

Retirement records its intent before it removes anything, so an interruption
leaves an area marked as retiring rather than an area the record still presents
as usable. Repeating the command completes it. A partially removed area is
never readable, published into, or counted as retained.

## Selection and command journeys

`setup` selects no context. It ignores the invoking user's current selection,
resolves no desired state, and consumes no explicit `--context` value. This
allows preparation before context creation or Environment import, and makes one
prepared host serve every context created on it. No current directory or user
profile selects inputs. Version intent for the host dependencies comes
from the compiled default alone, because no Environment can move it; the
[Environment version policy](api/environment.md#dependency-versions) declares
only the versions a controller stage installs.

Its acquisition route is the one exception, because no Environment exists to
carry one: setup takes the [invoking environment's route](#the-context-free-acquisition-route),
and direct access when that environment names none.

`preflight controller` uses a named context **only when `--context` is explicit
and nonempty**. An explicitly empty value is omission. Nonempty names follow
the existing context-name grammar. An explicit context must be ready and have
an admitted input revision; an empty one returns `context.input` with the
import command. Every check carries the scope that owns it, so a negative
report names the one command that settles it: `setup` for a host prerequisite,
and that context's own `apply --stage controller` for anything its graph
selects. An unmet host prerequisite always wins, because the prerequisites a
context adds cannot be prepared on an unprepared host. A controller
requirement outside the supported setup matrix blocks setup before effects.
Global SSH flags remain unconsumed.

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
inventory snapshot and solve the exact native transaction. Every repository
member the solver may open is staged, because which ones it opens is its own
decision and one it cannot find fails the whole repository. The inventory
snapshot is taken once and reused by every later inspection of the same
invocation while the installed database still has the identity it was copied
from; the invocation that took it releases it. All metadata, caches, logs and resolver
outputs stay in bounded scratch storage. Scratch is
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

The `controller-prerequisites-v3` Ansible adapter receives one frozen request
with platform, exact package/tool sources, two scoped bundle identities,
declared egress and optional retained preparation. The first names the approved
execution bundle the fixed automation is read from; the second is the only area
the request may publish into, and its writability is the adapter's authority to
change anything at all. Setup passes the same area for both, because it
publishes into the bundle it executes; a controller stage passes a separate
[client area](contexts/controller-record.md#bundles-and-client-areas), because
its execution bundle is sealed. The fixed controller setup playbook
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

The automation content digest covers every embedded automation file except
documentation, the collection's root `README` and `CHANGELOG`, which stay in
the bundle ([owner decision D14](milestones/backlog.md#pre-openshift-readiness-program-2026-09)).
That exclusion has its own digest domain version, so no digest under one rule
equals a digest under the other. A check of an approved bundle's automation
compares only what the digest covers.

Not yet met: the digest still covers documentation, because the lifecycle runner, the controller adapter and bundle inspection still compare every embedded file; dropping documentation from the digest before them would let a documentation-only change keep the digest while setup refuses the retained bundle as unattributable instead of carrying it forward; tracked as [backlog Z2](milestones/backlog.md#audit-follow-ups-2026-09).

`--yes` suppresses ordinary confirmation only. Use the
[ordinary confirmation](cli.md#ordinary-confirmation), with the plan before
the prompt; decline, noninteractive input without `--yes`, or cancellation
starts no setup mutation.
A verified no-op needs no prompt or installed-host/shared-state writes.
Disposable resolution and local-probe scratch is removed after use. Setup
changes neither current-context selection nor Environment input, and claims no
context.

## The controller stage

`apply --stage controller` installs the prerequisites one context's own graph
selects. It is one block in that context's lifecycle plan, planned by its
capability from effective state alone and frozen before any effect: the target
client requirements the graph names, the libvirt requirement a declared
`libvirt` capability adds, the [hypervisor closure](substrates.md#provider-host-realization)
a libvirt provider hosted on the controller Machine adds, the
[installer-media tooling](managed-os.md#installation) an Anaconda installation
published through an artifact server on the controller Machine adds, and the
controller Machine's normalized proxy choice. Exact releases are not frozen with the
block, because a `latest` intent is resolved by the attempt that installs it
and retained from then on. A context that selects nothing beyond the host
baseline contributes no block at all.

The block extends the resolution setup froze; it never resolves the host
foundation again. It first recovers the closure from retained identities alone,
which reads no publisher metadata: a host that already carries every selected
client reports `unchanged` without acquisition, repository access or
publication. Only what is missing is resolved, and native packages are solved
again whenever a selected root is absent, because a frozen native transaction
binds the host's exact before-inventory.

Before acquiring a single byte it publishes the exact source identities it will
fetch, and the native resolution a libvirt requirement needs, into the shared
controller record. Target clients are published into a
[client area](contexts/controller-record.md#bundles-and-client-areas) named by
the digest of that exact closure, under the reservation, attribution and
sealing rules the setup bundle has; the native transaction publishes its
before-state into the running attempt before it is authorized. The block
completes only after every selected client is proved present by presence alone
and the area is sealed.

Because the stage is a lifecycle block, what its Ansible prints is retained as
that block's [attempt output](cli/output.md#private-operation-logs), exactly as
every other adapter run's is. Local setup retains nothing: it allocates no
operation identity, so it has nowhere of its own to put it.

The closure is shared host state. Two contexts selecting the same clients prove
the same sealed files, a different closure gets its own area, and removing a
context retains both: destroying a context uninstalls no native package and
deletes no client area. Recovery is idempotent rather than compensating —
publication verifies existing bytes instead of overwriting them, so an
interrupted stage is completed by repeating it. `preflight controller --context
<name>` reports the same closure by presence, and names this command when it is
not yet installed.

A selected libvirt requirement on RHEL, client or hypervisor, refuses before
acquisition until an approved entitled source is defined; the qualified Fedora
profile carries both closures and the installer-media tooling. Setup remains the owner of the host foundation, and this stage
never installs it, publishes a binding, or claims another context's resources.

## Egress and local effects

With a context, the controller Machine's normalized
[proxy choice](api/machines.md#machine-proxy) is the sole route selection: the
controller stage and context preflight support direct access and qualified
unauthenticated external proxies using the qualified system trust store. A
managed Proxy, `proxyAuthRef` or `trustBundleRef` refuses before acquisition.
Never fall back to direct access or read Secret material to probe an
unsupported route; authenticated or private-trust acquisition needs its own
Secrets consumer and recovery definition.

### The context-free acquisition route

A command that acquires before any context exists has no Environment to select
a route from, so it reads one from the environment that invoked it. An admitted
invocation's parsed flags decide this, never its command path alone, and
exactly three shapes read it: `setup`, whose `--dry-run` preview reports the
route setup would take; `preflight controller` while its final `--context` is
omitted or empty, which reports the host's route; and `media add --from-url`.
Every context-backed shape, including `apply --stage controller`,
`preflight controller --context <name>` and every lifecycle block, continues to
take its Machine's normalized proxy choice alone, so one context never acquires
over two routes. A local shape, such as `media add --from-file`, and an
invocation that is not admitted read no route, so neither refuses over nor
forwards a route it never uses.

The route is read from `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY`, in either
case. An unset environment is direct access. The two spellings of one name set
to different nonempty values refuse rather than resolve, because either choice
discards an explicit operator value. `ALL_PROXY` and every other variable are
ignored, and no proxy configuration file, user profile or package-manager
setting is consulted.

Every approved source is HTTPS, so `HTTPS_PROXY` alone selects the route and
`HTTP_PROXY` without it refuses rather than acquiring directly. The endpoint
follows the same grammar as a declared external Proxy: an absolute HTTP or
HTTPS URL, bounded and ASCII, with no `userinfo`, path, query or fragment. A
credential-bearing endpoint refuses, so this route never carries a secret and
needs no Secret consumer.

`NO_PROXY` is a comma-separated list of the same bypass entries a declared
[proxy choice](api/infrastructure-services.md#proxy-choice) carries: a host
name, a domain suffix with or without a leading dot, an IP address, a CIDR
block, a `host:port` pair, or `*` for every destination. Entries are trimmed,
deduplicated and bounded by what a receipt may carry, matching consults no
resolver, and a bypass list without a selected proxy is nothing. An unqualified
value refuses before any privilege escalation, acquisition or local effect, and
names the variable to correct.

The route selects transport only. It never moves a source identity, digest,
version or the approved closure, and it reconfigures nothing on the host. It is
recorded in the setup receipt exactly as a declared route is, so an interrupted
setup resumed over a different route refuses rather than completing a plan that
was approved for another one.

Local privilege is where this route is qualified. The unprivileged invocation
reads and admits it before `sudo` can prompt, then forwards exactly the
canonical variables to the elevated child, which `env_reset` would otherwise
drop. A sudoers rule that permits neither `SETENV` nor `ALL` refuses that
forwarding; running as root reads the environment directly. Child processes
still receive no proxy variable of their own: the route reaches the download
client, package resolution and automation as request data, exactly as a
declared route does.

Reject a bootstrap dependency on a proxy or other service whose readiness
requires this setup or a subsequent apply. Admission of a reference is not
readiness evidence. Proxy routing applies only to setup's child processes and
download client; it does not reconfigure the provided OS. TLS verification,
trusted publisher metadata, digest checks, bounded redirects and acquisition
limits follow [Security](security.md). Package/source URLs contain no
credentials; Python, Ansible, package-manager and executable-path
configuration cannot alter the approved closure, and neither can the
[context-free route](#the-context-free-acquisition-route), which selects
transport alone.

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
resistance. Full-store restore follows the
[restore boundary](contexts.md#format-and-restore-boundary); controller
relocation waits on backlog [C14](milestones/backlog.md#candidates).

[Workspace](contexts.md#controller-relationship-and-host-binding) persists the
verified identity and context relationship. Setup prepares the host without
claiming any context; the relationship is published by the first apply that
uses the host, under the same verified identity, in the
[controller record](contexts/controller-record.md). Later operations and context
preflight verify that exact binding under
[controller-host protection](state-reconciliation.md#controller-host-protection);
they never silently rebind.

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
plan in the [controller record](contexts/controller-record.md#record-and-receipt).

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
an incomplete receipt still requires its original compatible executable. An
input update may change a context's controller prerequisite intent, which its
next controller stage installs, but cannot silently change an established
controller Machine/host binding. A host prerequisite that loses readiness is
prepared again by `setup`. What deleting a context removes from shared host
state follows the
[host-binding rules](contexts.md#controller-relationship-and-host-binding).

## Results and qualification

Both commands are text-only. Order checks and actions by catalog dependency
order, then stable prerequisite identity. Show the baseline or explicit context
scope, the resolved acquisition route with what selected it, required/observed
versions, planned changes, readiness and next safe command. Both commands stream
[long-running progress](cli/output.md#long-running-progress): the scope, then
one `Checks` row per host check as it is verified. Real setup adds one
`Resolving` step per dependency family it resolves before the plan, and one
`Progress` step per receipt action after confirmation, with the source, native
transaction or target tool in flight as its detail and, while it acquires them,
the share of its sources already published. Host fingerprints, private paths,
credentials, environment dumps and raw native-tool output are not public
results. Neither has JSON output, a lifecycle receipt or a private operation log.

Each check reports `ready` or `not-ready`, or `unverified` where a dry run
cannot verify it.

The [diagnostic taxonomy](cli/output.md#diagnostic-taxonomy-and-order) owns
`preflight.*` and `controller.*` failure codes; existing context, privilege
and cancellation codes retain their meanings. Failures include a safe
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
preparation, read-only preflight and dry-run, unsupported
combinations, dependency/identity substitution, multi-context contention,
protected dependencies, cancellation, package failures, uncertain publication
and exact retry. A fake adapter proves this contract and never proves an
executed native installer. Collection syntax, pinned lint, sanity, unit and
local integration checks qualify the shipped Ansible entrypoints as content.
[Milestones](milestones.md#completion-and-verification) own the verification model and
which acceptance remains operator-run.
