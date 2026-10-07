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
| Context | The target client closure the selected graph needs; the libvirt client closure a declared `libvirt` capability, or any Machine a libvirt provider hosts, selects; the hypervisor closure and the libvirt client a libvirt provider hosted on this Machine selects; the installer-media tooling an Anaconda installation published through an artifact server on this Machine selects; the binding between this context and this host. | [`apply --stage controller`](#the-controller-stage) |

A host prepared once therefore serves every context later created on it, and a
context that selects nothing beyond the baseline needs no controller stage.
The [Environment-selected controller Machine](api/environment.md#controller-machine)
names the host a context expects to run on; `setup` never reads it.

## Supported host and dependency selection

Setup supports **RHEL 9 and Fedora on Linux/amd64**. Each supported
combination identifies an exact OS release, tested kernel/filesystem and
privilege primitives, package repositories, native package-manager version,
package builds, and immutable execution bundle. The exact admitted releases
are compiled into the executable with each one's provided execution foundation
(`internal/controller/bundlelocal/catalog.go`) and recorded, with whether each
has run, in [development](../docs/development.md), never in this contract.
Every dependency release is resolved by setup; the compiled catalog pins none.
Each setup freezes source identities, byte counts and SHA-256 values. A family
name alone does not qualify all minor releases, Fedora releases or future
updates. Unsupported or unprovable combinations refuse before installation. The
exact matrix and dependency locks must remain consistent with that exit
evidence.

The baseline selects CPython from python-build-standalone, `ansible-core`,
their supporting wheels and urllib3 for bounded Ansible-owned downloads.
Host dependencies resolve to latest stable, with two bounded roots:
`ansible-core` resolves to the latest stable patch of the one qualified minor,
and CPython to the latest stable patch of the newest minor that `ansible-core`
minor supports as a controller;
[development](../docs/development.md#qualified-hosts-and-images) records both.
`latest` ignores a newer minor of either: it is never selected and never
refused. A yanked `ansible-core` release, or one without a pure-Python wheel,
is not a candidate, and an exact `ansible-core` intent selects its release only
when it is one. `latest` and an exact intent alike read the candidates from the
JSON form of the publisher's [Index API](https://peps.python.org/pep-0691/)
project page, never from the project JSON, and refuse a page of any major API
version but 1. A page of a minor newer than the one the build was checked
against only adds what the build does not read, so, as
[PEP 629](https://peps.python.org/pep-0629/) asks of a client, setup reads it
as the minor it knows, continues and reports a warning, before the plan when
it presents one and otherwise with its result
([owner decision D47](milestones/backlog.md#decisions),
[D91](milestones/backlog.md#decisions)). The collection's
`requires_ansible` names the same `ansible-core` minor, and the embedded
configuration makes a mismatch an error. No
Environment declares their versions, because setup reads none. The
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

Every private Python launch, whether setup's, a controller stage's or a
lifecycle block's in apply or destroy, verifies that foundation byte for byte
under the native package read lock, and setup and `preflight controller`
report the same verification as the `execution-foundation` check, after the
installed host, requiring the exact `glibc` and `libgcc` builds the compiled
catalog attributes the foundation files to
([D107](milestones/backlog.md#decisions)). The catalog names, beside each
admitted release's execution requirement and outside it, the package build
that provides each pinned file, so the attribution moves no bundle or
resolution identity. A foundation that differs settles that check not-ready,
observed at the first path that differs, and refuses with
`controller.unsupported` before any plan, naming the path, the package build
that provides it (for a pinned link, the build that provides the file it
reaches; for a path no package provides, the path alone), and what was found:
missing, other content, an unsafe owner, mode, type or link count, a link that
points elsewhere, or a non-empty `/etc/ld.so.preload`. A launch that meets it
refuses with the same diagnostic, and its remedy holds from every command: if
`dnf` updated `glibc` or `libgcc` within the qualified OS release, run
`bootwright setup` to qualify the new builds; otherwise install exactly that
build again with `dnf`, hold it with `dnf versionlock`, then repeat the
command; a host that must take an upstream or minor update needs a Bootwright
build whose foundation pins it. No next command is offered. The operator
guide's [hold procedure](../docs/operator-guide.md#hold-the-execution-foundation)
keeps a controller on those builds, and
[development](../docs/development.md#qualified-hosts-and-images) names the
tool that regenerates a release's record from a qualified host.

A z-stream errata needs one setup ([D107](milestones/backlog.md#decisions)).
When the compiled foundation differs at a file a pinned build provides, setup
and `preflight controller` read that package's installed instances from a
snapshot of the host's RPM database, under the same read lock, and qualify the
host's builds only when each package has exactly one x86_64 instance (an i686
instance is ignored), of the compiled build's upstream version, signed by the
platform's vendor key (the low 64 bits of the RHEL 9 or Fedora 43 profile's
signer, every present signature naming it), with SHA-256 file digests. The
qualified requirement is the compiled one with each pinned file's digest
replaced by the RPM database's digest for that path, and the versioned
`libgcc_s` file, the `libgcc_s.so.1` link and their preload entry taken from
the build's file list and link targets; it is then verified byte for byte
exactly as a launch verifies. Another OS minor stays an unadmitted release.
Another upstream version, an unsigned or foreign-signed build, a missing or
second x86_64 instance, or a file whose bytes differ from its RPM digest
refuses with `controller.unsupported` naming the package build, what
disqualified it and the reinstall remedy. A
qualified foundation settles the check ready, observed as the qualified builds
(for example `glibc 2.34-276.el9_8, libgcc 11.5.0-15.el9 (vendor-signed,
qualified within rhel 9.8)`). While a complete receipt records another
foundation, setup plans `Re-qualify the execution foundation`, with no host or
bundle effect, and publishes a new complete receipt that records the qualified
requirement as its `foundation`, bound into its plan digest; the next setup is
unchanged. `preflight controller` over such a receipt reports the check
not-ready with next command `bootwright setup`. A host holding the compiled
builds records nothing, so every digest and receipt stays as it was. Every
launch stays byte-exact against the receipt's requirement, the recorded
foundation when there is one and the definition's otherwise, and never reads
the RPM database; setup's own launches verify the foundation its inspection
qualified. Neither the catalog, a resolution's execution requirement nor any
bundle or resolution identity moves.

Bootwright's runtime brings its own cryptography: the Go executable and the
private CPython use their own cryptographic implementations, outside the host's
FIPS-validated modules, on a FIPS-mode controller as on any other
([D108](milestones/backlog.md#decisions)). Setup and `preflight controller`
report the kernel's FIPS mode as the informational `fips-mode` check, with that
statement when it is enabled; the check never changes readiness, the plan or
the next command while the kernel's flag reads 0 or 1 or is absent; a flag that
cannot be read as 0 or 1 refuses with `controller.unsupported`, which
[B414](milestones/backlog.md#b414) would report as not verified instead.
Bootwright claims no FIPS compliance; a FIPS-qualified runtime is parked as
[B331](milestones/backlog.md#b331).

The publisher and platform policies are compiled into the executable; exact
releases are resolved by explicit setup. Dependency selection follows
[dependency selection](architecture.md#dependency-selection-and-reuse) and
[supply-chain integrity](security.md). It distinguishes:

| Prerequisite | Selection and allowed setup |
| --- | --- |
| Host foundation | Verify the provided OS, architecture, local identity, account/sudo boundary, filesystem containment/durability, free-space limits and trusted package sources. No OS installation, release upgrade, repository enrollment, entitlement registration or reboot. |
| Baseline execution bundle | Publish the resolved exact Python and `ansible-core` closure in an isolated Bootwright-owned location. Do not use system/user Python imports or ambient Ansible configuration. |
| Container runtime | Setup selects Podman for every prepared host, so a controller that declares `container-runtime` finds it ready. Install or update the approved dependency set and verify an existing exact runtime without taking ownership of its containers or configuration. Do not start a service, pull a managed-service image or create a container. |
| Native target clients | Selected by the admitted desired-state graph, so [the controller stage](#the-controller-stage) owns them. OpenShift/OKD clients (`oc`, `kubectl`) and installer match the selected release; Kubernetes consumers select Helm; referenced vSphere providers select `govc`; virtualization selects upstream `virtctl`. A declared `libvirt` capability, or any Machine a libvirt provider hosts, selects `virsh` and its native client dependencies; a libvirt provider hosted on the controller Machine selects that client with the [hypervisor closure](substrates.md#provider-host-realization), because its host block proves both and the daemon packages need not install the client; and an Anaconda installation published through an artifact server on the controller Machine selects the [installer-media tooling](managed-os.md#installation). Setup selects none of them; the SSH and NMState clients that support the baseline flows are host prerequisites. Install these with the fixed Ansible controller role. |
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
additional libvirt requirement. On Fedora the native solver resolves the
libvirt client closure without the virtualization daemons. The public RHEL
baseline source does not include that client; a selected RHEL libvirt requirement
refuses before acquisition until an approved entitled source is defined.

Setup resolves Python and Ansible to their latest qualified releases during
explicit setup, before confirmation, and only when no retained resolution
serves the selected platform and native requirements under the running
executable. Native packages
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
or whose `latest` Python or Ansible release lies outside the qualified set,
older or newer, is superseded by a fresh resolution, never carried forward; a
resolution an earlier build recorded stays readable. An incomplete setup
reuses its exact frozen resolution without metadata refresh. A setup that
failed or was canceled is settled like a completed one: the next setup
publishes a new receipt rather than resuming it, so the resolution it recorded
is superseded or carried forward exactly as a completed setup's is.

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
that resolution's own approved sources from the retained bundle holding them,
projects them again under the embedded automation, and publishes the result as
the new bundle that identity names. Every release, byte count, signer and
publisher origin is preserved, no publisher or repository is consulted, and the
native transaction is reused unless a selected root is missing. A retained
bundle that cannot serve one of those sources as its approved bytes, because it
lost or changed one or is gone, has nothing to carry: setup resolves afresh, as
it does for a resolution it cannot use, and that resolution's publishers supply
every source it names, each verified as any fresh resolution's is. A publisher
that fails, or that now serves other bytes under a retained source's identity,
refuses that setup exactly as it would any fresh resolution. A source the
bundle loses after the reprojection is acquired from its publisher when the new
bundle is published. A resolution needing a different provided execution
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
completed: a setup that refuses, fails or previews retires nothing then,
because what may be retired is decided by what the new bundle now holds. The
one earlier retirement is a setup at the bound, below. A bundle is retired
only when it is positively identified as a superseded execution bundle, which
is one a retained resolution names and that the receipt does not, or when an
interrupted retirement already marked its area as retiring. That receipt
is a settled one, whose setup completed, failed or was canceled; a pending
receipt admits no retirement, except one stranded at the bound, below.
Everything else is left untouched, including
every [client area](#the-controller-stage), which is shared host state no
context uninstalls, and any area this record does not account for; the
[store refuses](contexts/controller-record.md#bundles-and-client-areas) to
retire either, whatever setup names.

Retirement is safe because a completed setup leaves nothing behind that needed
the bundles it replaces. Publication writes every source into the bundle it
publishes, whether that source was acquired or recovered from the bundle a
[carried-forward resolution](#supported-host-and-dependency-selection) read, so
the next carry-forward reads the bundle this setup just sealed. A lifecycle
operation always runs the bundle the current receipt names, so no frozen
operation reaches a retired one. At the bound, retirement also runs beside a
receipt whose setup failed or was canceled, and that is safe too: that setup is
over, nothing resumes it, and the bundle it names is kept.

The resolution a retired bundle carries is retired with it, because a retained
resolution whose sources are gone can be carried forward from nothing. That is
what returns capacity: a host holds at most 16 bundle areas, execution bundles
and client areas alike, and at most 16 retained resolutions
([bounds](contexts/controller-record.md#bounds)). A superseded execution
bundle that holds no area has only its resolution to give back: once a later
receipt replaces a canceled one whose bundle never held an area, the
retirement after completion retires that receipt's resolution alone. The
resolution a [controller stage](#the-controller-stage) retains for the native
clients a context selects names no area either, but only that stage judges
which of those are superseded, so setup never retires one. The result counts
the areas the command removed, and nothing else, and a completed setup whose
purge removed none says `none`. A `--purge-old-bundles` dry run plans that
retirement after the setup it previews completes, and a retirement that fails
after its setup completed names the purge again, which completes it, as its
remedy and next command.

**A setup at the bound.** When a setup must publish a new execution bundle and
the host already holds 16 areas or 16 retained resolutions, it decides before
it presents its plan, and decides again under the mutation that publishes. It
has an exit in every case but one: a bound that client areas and the current
execution bundle fill, which this build cannot free
([B322](milestones/backlog.md#b322)):

- With `--purge-old-bundles` it first retires every superseded execution
  bundle, and every area an interrupted retirement left marked as retiring,
  except the bundle the receipt names and the one its
  [carried-forward resolution](#supported-host-and-dependency-selection) reads.
  It also retires every superseded resolution of the bundles it keeps, which
  are those two and the new one: a retry after a failed setup, or a solve
  against a changed package inventory, names the same bundle under a new
  resolution, so one kept bundle can come to hold every resolution the host
  may retain. Of those only the receipt's own, the one this setup publishes
  and the latest naming each kept bundle stay, so every kept bundle stays
  named by a resolution, and no area is removed for them; the
  [store refuses](contexts/controller-record.md#bundles-and-client-areas) to
  drop the receipt's own or the last resolution naming a bundle it holds.
  The plan names that retirement. It then publishes and completes, and the
  retirement after completion follows as usual. Retirement is never undone,
  so a setup that fails after it still leaves that room.
- Without the flag it refuses with `controller.conflict` before any effect, and
  names `bootwright setup --purge-old-bundles` as its remedy and next command.
- When retiring would free no room, because only client areas and the current
  execution bundle hold the bound, it refuses with `controller.conflict` and
  says so, with or without the flag. This build retires no client area, so no
  command of it frees that room and the result names no next command; the
  remedy is to keep using the build whose execution bundle the host holds, or
  to set this build up on another controller host. A controller stage whose
  client area or retained resolution meets the bound refuses the same way,
  naming the purge that retires superseded execution bundles
  ([bounds](contexts/controller-record.md#bounds)).

The receipt may be one whose setup completed, or one whose setup failed or was
canceled: the next setup replaces either with a new receipt, so both are
treated alike at the bound. A pending receipt is resumed exactly rather than
replaced, so it admits no retirement unless it is stranded: an earlier build
published it at the bound, and the bundle it names holds no area while the
host holds 16, so resuming it could never reserve one. The store refuses a new
receipt that names a bundle it cannot reserve, so no new receipt is stranded.
Retirement is never undone, so for one an earlier build left, setup first
proves that this executable can prepare the bundle that receipt names. When it
can, setup decides as above with the bundle that receipt names as the new
one. With `--purge-old-bundles` it retires every superseded execution bundle
and every area left marked as retiring, keeping
that receipt's bundle, its resolution, which the store retained when it
published the receipt, and every client area, then resumes the receipt
exactly. A resumption carries nothing forward, so no other bundle is kept, and
it retires no resolution, because only an area is missing. Without the flag it
refuses with `controller.conflict` before any effect and names
`bootwright setup --purge-old-bundles`. This is safe too: nothing the
resumption reads is in an area it retires, and no lifecycle operation runs
while a receipt is pending. A pending receipt whose bundle holds an area needs
no room and admits no retirement.

A pending receipt that is [setup's own](#publication-and-interrupted-setup),
at the bound or below it and over whatever ambient route it recorded, cannot
be resumed when its automation or provided execution foundation is not one
this executable embeds, or when it froze dependencies other than the ones this
host now selects, such as another platform's after a release upgrade, and one
whose setup never took effect is canceled instead
([D55](milestones/backlog.md#decisions),
[D93](milestones/backlog.md#decisions)): a route binds only what a setup
acquires, and nothing that one acquired took effect. Two shapes prove it. In
the first, its first action, `execution-bundle`, holds at most its intent and
every later action is still planned, as an earlier build leaves it that
recorded the intent and then met the bound reserving that bundle's area, or
lost the publication that followed; a bundle area holds private files that
nothing reads before the receipt seals it. In the second, `execution-bundle`
was observed `changed` or `unchanged` and the native transaction holds its
intent with the preparation the receipt's own resolution admits, for a plan of
at least one action, while the host's installed package inventory, read now
from a snapshot taken under the native package read lock, still equals that
preparation's before-state and not its after-state: the installer records the
preparation before it stages the payloads and is authorized only after them,
so a refusal in between, such as every Python-side download before
[B297](milestones/delivered.md#x43--libvirt-roles-managed-os-installation-network-composition-and-controller-automation), leaves this shape. Setup reads that inventory
through the native inspector, and a composition that cannot read it never
cancels the second shape. With `--purge-old-bundles` setup records that
receipt `canceled` under its own ID and plan, observing what it found
([controller record](contexts/controller-record.md#record-and-receipt)); at
the bound it then retires every superseded execution bundle and every area
left marked as retiring; and it sets this host up afresh under this executable
from its observed state, as the next setup after any canceled receipt does.
Once that setup completes, the retirement after completion retires what the
canceled receipt left: its bundle's area when it held one, and otherwise its
resolution. The one plan it presents names the cancellation, its shape and why
this executable cannot resume it, the retirement and the fresh setup. Under the
mutation that records it, setup decides again, reading the inventory again,
and refuses as controller state that changed after plan confirmation when the
receipt, its plan, its shape or its inventory moved; the cancellation is
durable before anything is retired. Without the flag setup refuses with
`controller.conflict` before any effect, names
`bootwright setup --purge-old-bundles` as its next command and says the
receipt is at this host's bound only when it is, and preflight refuses the
same way. Any other pending receipt this executable cannot resume, such as one
with another action holding its intent, a native transaction whose inventory
differs from its before-state, equals its after-state or cannot be read, or any
action observed `unknown`, may have taken effect, so it is never canceled: it
refuses with `controller.unknown` before any effect, with or without the flag.
When it froze dependencies other than the ones this host now selects, the
refusal says so and names the original input to restore; otherwise it refuses
as [another pending attempt](#publication-and-interrupted-setup) does.

Retirement records its intent before it removes anything, so an interruption
leaves an area marked as retiring rather than an area the record still presents
as usable. Recording that intent already retires the resolutions those areas
carry, so no resolution names them afterwards: `setup --purge-old-bundles`
also retires every area marked as retiring, except the bundle the receipt
names, and repeating the command once completes an interrupted retirement. A
stranded receipt stays pending with its resolution retained however its
retirement or resumption is interrupted, and repeating the command goes on
from what the interruption left. One being canceled stays pending until its
cancellation is durable; an interruption after that, during the retirement or
the fresh setup, leaves it canceled, at the bound still there until the
retirement completes, or already replaced by the fresh setup's own receipt,
and repeating the command completes it from there. A partially
removed area is never readable, published into, or counted as retained.

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
and that context's own `apply --stage controller --context <name>` for anything
its graph selects. A context whose graph selects no target client, libvirt
client, hypervisor or installer-media tooling has no controller stage, so its
only context check, the binding, names `apply --context <name>`, whose first
apply publishes it. The report's next command and the diagnostic's remedy are
that one decision. An unmet host prerequisite always wins, because the
prerequisites a context adds cannot be prepared on an unprepared host. A controller
requirement outside the supported setup matrix blocks setup before effects.
Global SSH flags remain unconsumed.

| Invocation | Required behavior |
| --- | --- |
| `bootwright setup --dry-run` | Produce deterministic dependency intent and actions from policy and bounded local file metadata; with `--purge-old-bundles`, plan the retirement that follows the setup's completion. No controller record is read, so this stays below the privilege boundary. No dependency subprocess, network, Secret read, privilege escalation or write. Versions requiring live resolution and readiness facts requiring effects are explicitly unverified. The `state-root` check reports, from an unprivileged look at the state root, an absent root, which setup creates, or a `root:root` `0700` directory on a local filesystem, whose contents setup verifies. A root of another type, owner or mode, one on an unqualified filesystem, and, when the root's owner runs the dry run, one holding state this build cannot read, such as an earlier build's, are not ready: the dry run reports the check beside the store's own refusal and remedy, and exits 1. |
| `bootwright setup` | Inspect, present the complete bounded local plan, confirm when it contains changes, prepare the host dependencies and verify every required postcondition. Publish no binding and record no context on the receipt. |
| `bootwright preflight controller` | Read and verify the host prerequisites with bounded local probes, the [execution foundation](#supported-host-and-dependency-selection) among them, and report the host's FIPS mode. May use the verified privilege boundary for private metadata and disposable local probe scratch. |
| `bootwright preflight controller --context <name>` | Additionally report that context's own target tools, libvirt client, hypervisor and installer-media closures, and host binding as context-scoped checks, by presence only, exactly as [its controller stage](#the-controller-stage) reads them. Contact no publisher and read no repository metadata for them. |

Neither command installs, downloads, refreshes repository metadata, contacts a
managed endpoint, creates or repairs shared state, or publishes a binding on
behalf of the other's scope. Preflight publishes nothing at all.

Dry-run does not execute a native package resolver. It lists required version
intent and identifies any transaction feasibility, live identity or readiness
evidence still needed by real setup. A valid dry-run exits successfully with
unverified checks visibly labeled; it never reports completed setup. Preflight
requires positive current evidence for all selected checks; an unbound context
is a failure that names its controller stage when it has one, and otherwise
`apply --context <name>`, whose first apply publishes the binding, distinct
from host mismatch, which names the restored host. Its binding check observes
that it is not yet bound and that its first apply binds it, and stays not
ready rather than pending.

Real setup first resolves dependencies in disposable unprivileged staging.
This phase may download verified public bootstrap payloads, run wheel-only pip
resolution and use the provided OS's DNF4/DNF5 foundation to inspect a private
inventory snapshot and solve the exact native transaction. Every repository
member the solver may open is staged, because which ones it opens is its own
decision and one it cannot find fails the whole repository. The inventory
snapshot is taken once and reused by every later inspection of the same
invocation while the installed database still has the identity it was copied
from; the invocation that took it releases it. All metadata, caches, logs and resolver
outputs stay in bounded scratch storage. Scratch lives beneath
/var/lib/bootwright-staging, the private Bootwright-owned scratch parent: a
sibling of the verified store rather than part of it, because setup resolves
before confirmation, when the store may not exist, and the store admits no
entry it does not own. It is never the shared ambient temporary directory,
which hardened hosts mount noexec. Any command that stages creates the parent on
first use, owned by the invoking identity with mode 0711 so the unprivileged
staging identity can reach its own stage but list none, and refuses, naming the
parent and its correction, a parent that is a link, not a directory, owned by
another identity or writable by any other. The parent holds nothing between
invocations: each stage is a directory of its own that one invocation creates
exclusively and locks for the stage's lifetime, and removes when it ends. The
next invocation that stages removes every stage whose lock is free, following no
link and crossing no filesystem, leaves one still held or still being created,
and refuses, naming the parent, when the parent holds more than its entry bound
or a stale stage cannot be removed safely, so an interrupted run leaves no
unrecognized state behind. The staged resolver executes from the parent's
filesystem, so that filesystem must permit execution: an execution the kernel
denies there while it is mounted noexec refuses naming that mount, with the
remount as its correction. Setup refuses before any effect when that
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

The `controller-prerequisites-v5` Ansible adapter receives one frozen request
with platform, exact package/tool sources, an acquisition deadline in seconds
for each native package and then each tool source, in that order, the
`nativeStaging` bound in seconds that every native package is staged under
together, two scoped bundle identities, declared egress and optional retained
preparation. The first names the approved
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
files are verified by their streamed digest, never overwritten. A target
source streams to disk under its own
[acquisition deadline](#the-controller-stage), each chunk written and
digested before the next is read, and its members stream into place the same
way, so neither a source nor a member is ever held whole in memory; an
interrupted transfer leaves only an unlinked file. Only naming a complete,
proved file creates the directories it is named in, so a refused source or
member leaves no directory behind. Before any member of
`openshift-clients` is published, its `oc` must name the frozen release, for
both compatibilities. A released `oc` names its release in its own bytes: the
version, NUL-terminated, overwrites the head of a fixed 93-byte marker. The
bytes must hold exactly one marker stamped with the frozen version and no
unstamped one, or neither `oc` nor `kubectl` is published, and the retained
source is all the refusal leaves. The adapter names this refusal to Go in a
[`refused` record](architecture.md#the-adapter-result-protocol) before it
fails, so its diagnostic's remedy names the release-stamp check and asks for a
`release.version` or `downloads.openshiftClientsMirror` whose `oc` passes it:
the same release and mirror reuse the retained source and refuse again, so
restoring sources cannot help. No downloaded tool
is ever executed, so the stamp is read, never asked of the tool. Inventory,
request, downloads, expanded members and callback frames have fixed bounds. A
native package streams into its own file in disposable scratch the same way,
chunk by chunk, under its own acquisition deadline, and every package of the
run under the `nativeStaging` bound, the acquisition deadline of their
declared bytes together, held to the controller stage's 2-hour ceiling less the
run's 10-minute base; each is proved by its digest before the transaction may
name it. One connection pool serves each route, direct or through the proxy,
for every package of a run and for one tool's source.

An adapter that refuses names one closed class to Go in a
[`refused` record](architecture.md#the-adapter-result-protocol) before it
fails, and never the exception's text. An acquisition names `dns`,
`certificate`, `timeout`, `unreachable`, `proxy`, `status`, `redirect`,
`integrity`, `trust` or `storage`, with the `source` it was acquiring. The
native helper writes exactly two lines to its standard error, `refused
<class>` and the exception's type and first line of text, bounded at 200
bytes, and exits 1; its classes are `solver-conflict`, `missing-candidate`,
`signature`, `database`, `transaction`, `postcondition`, `foundation`,
`timeout` and `internal`, and the inventory and package adapters carry the
class in the record. Go maps each class to its diagnostic and remedy, naming
the source's host for an acquisition, and for `storage` the directory that
source was being written into: the run's scratch for a native package, the
publication bundle for a tool; `internal` keeps the generic adapter
failure, and a refusal after Go authorized the native transaction is
`controller.unknown`. The raw first line reaches only the private setup run
or controller-stage attempt output, never a record or a diagnostic.

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
It streams each target source once and each published target member it proves,
holding only their digests, so neither is ever held whole in memory; a bundle
area that cannot stream a file is refused rather than read whole. When it
projects an `openshift-clients` source, it also reads `oc`'s release stamp as
the member streams, as the adapter does, and refuses a source whose `oc` does
not hold exactly one marker stamped with the frozen version and no unstamped
one with the same release-stamp diagnostic. A client area's readiness stays
presence only and reads no bytes, so the stamp of a client already in a sealed
area is not read again.

The automation content digest covers every embedded automation file except
documentation, the collection's root `README` and `CHANGELOG`, which stay in
the bundle ([owner decision D14](milestones/backlog.md#decisions)).
That exclusion has its own digest domain version, so no digest under one rule
equals a digest under the other. A check of an approved bundle's automation
compares only what the digest covers. The bootstrap projection identity, file
count and byte total leave documentation out too, under a projection domain
version of their own. Documentation must still be present in every bundle, as
a regular non-executable file within the member bound, and no check compares
its size or bytes, so a documentation-only build keeps the digest, and a
bundle holding an earlier release's documentation stays attributable and is
completed rather than refused.

`--yes` suppresses ordinary confirmation only. Use the
[ordinary confirmation](cli.md#ordinary-confirmation), with the plan before
the prompt; decline, noninteractive input without `--yes`, or cancellation
starts no setup mutation.
A verified no-op needs no prompt or installed-host/shared-state writes.
Disposable resolution and local-probe scratch beneath
/var/lib/bootwright-staging is removed after use, and a stage an interrupted
invocation left is removed by the next one that stages. Setup
changes neither current-context selection nor Environment input, and claims no
context.

## The controller stage

`apply --stage controller` installs the prerequisites one context's own graph
selects. It is one block in that context's lifecycle plan, planned by its
capability from effective state alone and frozen before any effect: the target
client requirements the graph names, the libvirt requirement a declared
`libvirt` capability adds, the [hypervisor closure](substrates.md#provider-host-realization)
and the libvirt requirement a libvirt provider hosted on the controller Machine adds, the
[installer-media tooling](managed-os.md#installation) an Anaconda installation
published through an artifact server on the controller Machine adds, and the
controller Machine's normalized proxy choice. The libvirt client is selected by
a declared `libvirt` capability, a libvirt provider hosted on the controller
Machine, or any Machine a libvirt provider hosts, and by nothing else: the
stage resolves the client only when it is selected, so a context that selects
only the installer-media tooling resolves no libvirt client, and no hypervisor
or installer-media selection forces one into the transaction. Exact releases
are not frozen with the block, because a `latest` intent is resolved by the
attempt that installs it and retained from then on. A context that selects nothing beyond the host
baseline contributes no block at all.

The block extends the resolution setup froze; it never resolves the host
foundation again. It first recovers the closure from retained identities alone,
which reads no publisher metadata: a host that already carries every selected
client reports `unchanged` without acquisition, repository access or
publication. Only what is missing is resolved, and native packages are solved
again whenever a selected root is absent, because a frozen native transaction
binds the host's exact before-inventory.

Before acquiring a single byte it publishes the exact source identities it will
fetch, and the native resolution a selected closure needs, into the shared
controller record. The stage reads only the latest retained resolution of its
selection, never setup's own: the same platform, the same libvirt client,
hypervisor and installer-media closures a transaction solves there, and the
same libvirt intent. A resolution of another selection proves nothing about
this one, even when it installed some of the same roots, so a new one
supersedes every earlier one of that selection, and the publication that
retains it [retires them](contexts/controller-record.md#bundles-and-client-areas).
Solving again whenever a root is missing therefore never accumulates
resolutions toward the host's bound of 16. Target clients are published into a
[client area](contexts/controller-record.md#bundles-and-client-areas) named by
the digest of that exact closure, under the reservation, attribution and
sealing rules the setup bundle has; the native transaction publishes its
before-state into the running attempt before it is authorized. The block
completes only after every selected client is proved present by presence alone
and the area is sealed.

Every other block of the plan runs the clients this stage installed, and
locates each executable through this block alone, from what the block proved
in the apply whose effects the asking operation runs over: that apply, or the
apply a removal takes back, because the stage's own removal retains and proves
no closure. That proof names the client area and the exact bytes of each
client. No retained resolution names a client closure, so the stage recovers
the closure from the retained identities of exactly those clients and answers
with the file the proved area holds. The host's retained sources are shared,
so recovering the closure from all of them would select the newest release
another context retained under the same `latest` intent, an area this context
never published. An apply that proved no completed stage, a closure only partly
retained or naming another area, an area that does not exist, or one without
the file answers that the executable is not installed and names
`apply --stage controller`, and no search path is ever consulted instead.

Each client source and each native package has an acquisition deadline of 2
minutes plus its declared bytes at 512 KiB/s, rounded up to a whole second.
Native staging is bounded by the acquisition deadline of every native package's
declared bytes together, held to the 2-hour ceiling less the 10-minute run
base. A run's deadline is the controller run's 10 minutes plus its native
staging bound plus the acquisition deadline of every tool source it selects,
held to the controller stage's 2-hour ceiling; a closure with tools whose
deadline would pass that ceiling is refused before Ansible starts, so the
ceiling never shortens a run it admits. The
[bounds table](contexts.md#storage-locking-and-publication) names both
constants.

Because the stage is a lifecycle block, what its Ansible prints is retained as
that block's [attempt output](cli/output.md#private-operation-logs), exactly as
every other adapter run's is. Local setup allocates no operation identity, so
its own Ansible keeps what it prints in a
[setup run](cli/output.md#setup-run-output) beneath the controller directory
instead.

The closure is shared host state. Two contexts selecting the same clients prove
the same sealed files, a different closure gets its own area, and removing a
context retains both: destroying a context uninstalls no native package and
deletes no client area, so resolving the stage's removal runs nothing and is
always its completion. Recovery is idempotent rather than compensating —
publication verifies existing bytes instead of overwriting them, so an
interrupted stage is completed by repeating it. `preflight controller --context
<name>` reports the same closures by presence, one check each for the libvirt
client, the hypervisor and the installer-media tooling the context selects,
read through the same matcher from the same latest resolution, and requires
every one of them; it names this command when one is not yet installed.

On RHEL, a selected libvirt requirement, client or hypervisor, refuses before
any retained resolution, target client or host package is read, naming a
Fedora controller, until an entitled source exists
([B330](milestones/backlog.md#b330)), and preflight reports that refusal with
no next command. A RHEL controller's installer-media tooling is
the operator's own ([D106](milestones/backlog.md#decisions)): `lorax` and
`xorriso` installed from the host's own enabled Red Hat repositories are
accepted by presence, each installed instance proved by its identity and a
signature of the qualified Red Hat release key, never by file integrity, as
any root is. No public source this executable resolves from carries them, so
the stage never solves them on RHEL, and an installer-media-only context runs
no native resolution there at all. When either is missing or carries another
signer, the stage refuses before any publisher contact, naming the package and
the remedy, then this command: `dnf install lorax xorriso`, and for a package
another key signed `dnf remove <package> && dnf install lorax xorriso`, since
installing a name already installed changes nothing; preflight's report and
remedy lead with that same step. The qualified Fedora profile carries both
libvirt closures and the installer-media tooling. Setup remains the owner of
the host foundation, and this stage never installs it, publishes a binding, or
claims another context's resources.

A failure this stage meets in an adapter it shares with setup (the dependency
bundle adapter's tool catalog, projection, publisher metadata, transport,
acquisition or trust, the native resolution, or the controller Ansible run)
keeps its own diagnostic code and names this stage's own `bootwright apply
--stage controller --context <name>` as what settles it, never `setup`, which
installs nothing a context selects. `preflight controller --context <name>`
names the same command for the same failures it meets while it recovers that
context's clients by presence. That remedy offers the correction alone, even
for a publisher the host could not reach or resolve: the block froze the
controller Machine's proxy choice it acquires over, and the failed apply
holding it can only be
[continued over its exact input](state-reconciliation.md#state-machine), so
another route is nothing that command can settle. A refusal of the controller
record that an apply or this stage meets names its own remedy: a host without
completed setup names `bootwright setup` and then the refused command, another
host names the host the state belongs to, and a context bound elsewhere names
its bound Machine input or a new context. Setup's own refusals name setup's
exact retry, and so does a controller record publication whose outcome is
unknown, which setup, an apply and context deletion share.

## Egress and local effects

With a context, the controller Machine's normalized
[proxy choice](api/machines.md#machine-proxy) is the sole route selection: the
controller stage and context preflight support direct access and qualified
unauthenticated external proxies using the qualified system trust store, whose
`tls-ca-bundle.pem` the adapter reads as its certificate blocks only, ignoring
the `# <label>` lines `update-ca-trust` writes, which may hold UTF-8. That
route follows the [context-free route's grammar](#the-context-free-acquisition-route):
admission refuses an endpoint or bypass entry outside it, and controller
selection proves it again, naming the Proxy or Machine field that breaks it
with a remedy. A managed Proxy, `proxyAuthRef`, `trustBundleRef`, a Proxy with
`httpProxy` but no `httpsProxy`, and every other limit of this executable the
[controller refusal table](api/environment.md#refusal-table) lists refuse
before registration with their remedies, and selection refuses the first of
them naming its field; an `httpProxy` alone names `connection.httpsProxy`,
because every dependency source is HTTPS. Never fall back to direct access or
read Secret material to probe an unsupported route; authenticated or
private-trust acquisition needs its own Secrets consumer and recovery
definition.

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
needs no Secret consumer. The endpoint's host is recorded in lowercase, as the
setup receipt records it, so every endpoint the route admits is one the
receipt accepts.

`NO_PROXY` is a comma-separated list of the same bypass entries a declared
[proxy choice](api/infrastructure-services.md#proxy-choice) carries: a host
name, a domain suffix with or without a leading dot, an IP address, a CIDR
block, a `host:port` pair, or `*` for every destination. Entries are trimmed,
deduplicated and bounded by what a receipt and the controller stage's
acquisition may carry, each entry at most 1024 bytes, matching consults no
resolver, and a bypass list without a selected proxy is nothing. An unqualified
value refuses before any privilege escalation, acquisition or local effect, and
names the variable to correct.

The route selects transport only. It never moves a source identity, digest,
version or the approved closure, and it reconfigures nothing on the host. It is
recorded in the setup receipt exactly as a declared route is, so an interrupted
setup resumed over a different route refuses rather than completing a plan that
was approved for another one.

A publisher setup could not reach or resolve is therefore offered a proxy
through `HTTPS_PROXY`, kept out of `NO_PROXY`, and never an external Proxy on
the controller Machine, which setup does not read; a failure setup meets names
`bootwright setup` as what settles it. The same adapter failure met by a
context's controller stage names [that stage](#the-controller-stage) instead.

Local privilege is where this route is qualified. The unprivileged invocation
reads and admits it before `sudo` can prompt, then forwards exactly the
canonical variables to the elevated child, which `env_reset` would otherwise
drop. A sudoers rule that permits neither `SETENV` nor `ALL` refuses that
forwarding, and the refusal, human or JSON, names the `SETENV` tag as its
remedy ([local privilege](cli.md#local-privilege-and-user-identity)); running as
root reads the environment directly. Child processes
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
relocation waits on [B103](milestones/backlog.md#b103).

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
What its controller Ansible prints, for a preparation or a recovery, is kept
beside that record in a [setup run](cli/output.md#setup-run-output), which is
troubleshooting material only: the receipt never names a run, and neither
keeping one nor failing to changes a setup outcome or its receipt.

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
with a presumed no-effect outcome. Setup's own action whose adapter failed after
publishing its preparation but before Go acknowledged a native record is
recorded failed, because the adapter cannot start its transaction before that
acknowledgement, and the next setup replaces it. Changed input, host identity or dependency
closure cannot replace an incomplete setup; restore the exact compatible
executable/dependencies and resolve that receipt first. The one exception is a
pending receipt of setup's own, at the bound or below it, that this executable
cannot resume and whose setup never took effect, which
`setup --purge-old-bundles` [cancels](#supported-host-and-dependency-selection)
on what it observes: that the receipt's bundle was never published, or that
the host's package inventory still equals the before-state its native
transaction recorded.

Whether a pending receipt is this setup's own is decided without a context,
because setup records none: a receipt that names one is never setup's, and a
context's preflight compares neither its own context, nor its controller
Machine's route with the ambient route the receipt recorded, nor a binding
action setup never plans. Setup and a context-free preflight read that ambient
route and still compare it, so setup resumed over another route refuses. A
pending receipt setup [cancels](#supported-host-and-dependency-selection) is
never resumed, so its route is not compared.
Another pending attempt refuses, naming the executable that recorded it and the
`HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` values it ran with, then
`bootwright setup`.

Cancellation stops authorization of new effects. Before native installation
starts, the runner signals Ansible's supervisor, which ends every descendant,
including Ansible workers in sessions of their own, then kills the process
group. Once an authorized
package transaction is running, it waits for native package hooks to finish
and retains coordination until the child has exited; a timeout never grants
lock takeover. The unprivileged sudo supervisor relays the operator's interrupt
and waits for the elevated command with no deadline of its own, so a second
operator signal is the only kill
([local privilege](cli.md#local-privilege-and-user-identity)). Abrupt process
death can leave package effects or an incomplete bundle. An explicit retry must prove either the exact before-inventory for
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
an incomplete receipt still requires its original compatible executable, unless
it is one setup cancels. An
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
versions, planned changes, readiness and next safe command. A direct route
names what chose it too: `direct (no HTTPS_PROXY)` for an invoking environment
that names no proxy, and `direct (Machine <name>)` for a context's controller
Machine. A refused or failed setup's next command is the `bootwright` command
its remediation names, and it names none when the remediation names none, as
beside a confirmation refusal, whose remedy repeats the operator's own
invocation with `--yes`. Both commands stream
[long-running progress](cli/output.md#long-running-progress): the scope, then
one `Checks` row per host check as it is verified. Real setup adds one
`Resolving` step per dependency family it resolves before the plan, and one
`Progress` step per receipt action after confirmation, with the source, native
transaction or target tool in flight as its detail and, while it acquires them,
the share of its sources already published. Host fingerprints, private paths,
credentials, environment dumps and raw native-tool output are not public
results. The one private path setup names is the directory of the
[setup run](cli/output.md#setup-run-output) keeping its controller Ansible's
output, as a `Logs` field before that Ansible starts and again with the
result; what that Ansible printed stays in the run, and a failure of that
Ansible leads its remedy with the run's `run.output` and that reading it needs
root. Neither command has JSON output, a lifecycle receipt or a private
operation log.
What setup's dependency resolution read but did not refuse, such as a
publisher page of a newer Index API minor, is a `[WARN]` diagnostic on standard
error, written once: just before the plan when setup presents one, so before
its prompt, and otherwise with the result, whether that setup completes or
stops ([D91](milestones/backlog.md#decisions)). It never changes the outcome or
exit status, and a setup that reuses a retained resolution reads no publisher
and reports none.

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
local integration checks qualify the shipped Ansible entrypoints as content;
sanity also imports every module and module utility under a pinned interpreter
of the oldest Python a managed host may run.
[Milestones](milestones.md#completion-and-verification) own the verification model and
which acceptance remains operator-run.
