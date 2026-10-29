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

Bootwright installs and manages only packages its own execution needs.
Tools used solely for an operator runbook's independent diagnostics belong
to that runbook, which documents installation and removal while preserving
packages already present ([D178](milestones/backlog.md#d178)).

| Scope | Prerequisites | Owner |
| --- | --- | --- |
| Host | A [supported](#supported-hosts) OS family and architecture and verified installed-host identity; the fixed root and its [setup subtree](contexts/controller-record.md#the-setup-subtree); the [controller runtime](#the-controller-runtime), a virtual environment on the host's own CPython holding the [locked](#the-runtime-lock) `ansible-core` closure; the baseline [host packages](#host-packages) `openssh-clients` and `podman`. | `setup` |
| Context | The target client closure the selected graph needs; the libvirt client closure any Machine a libvirt provider hosts selects, derived from placements with no Machine role declared; the hypervisor closure and the libvirt client a libvirt provider hosted on this Machine selects; the installer-media tooling an Anaconda installation published through an artifact server on this Machine selects; the `nmstate` package an agent-installed OpenShiftCluster node that references a declared Machine selects; the `iputils` package an agent-installed multi-node baremetal OpenShiftCluster selects when one of its OpenShift-sourced IPv4 virtual IPs lies in a managed libvirt network of a provider hosted on this Machine; the binding between this context and this host. | [`apply --stage controller`](#the-controller-stage) |

A host prepared once therefore serves every context later created on it, and a
context that selects nothing beyond the baseline needs no controller stage.
The [Environment-selected controller Machine](api/environment.md#controller-machine)
names the host a context expects to run on; `setup` never reads it.

Setup installs only what is missing and never checks for updates: a host
package is asked of the host's own dnf by name or RPM capability, and the
runtime's wheels come from the lock the running build embeds. A ready host is
reported in under a second with no network request, confirmation or write.
The upgrade verb is a new Bootwright build ([D152](milestones/backlog.md#d152)).

## Supported hosts

Admission fails closed before any effect. A host is admitted when all of these
hold, each refusing `controller.unsupported` with its remedy otherwise:

- `/etc/os-release` `ID` is `fedora` or `rhel`, or its `ID_LIKE` names one, so
  CentOS Stream, Rocky and Alma are admitted; another names the remedy of a
  Fedora, RHEL or derivative host;
- the architecture is amd64, matching the Go build;
- `/usr/bin/rpm` and `/usr/bin/dnf` are qualified system executables: each
  resolves to a regular file owned by root, and neither it nor any directory
  above it is writable by group or others; another names restoring the host's
  tool under `/usr/bin` with root ownership;
- an interpreter of a locked Python ABI is installed, or the host's dnf offers
  one ([the controller runtime](#the-controller-runtime));
- glibc is at or above the lock's `glibcFloor`, which the lock generator
  computes from the highest manylinux tag among the locked wheels; another
  names a host whose glibc is that floor or newer.

No release name appears in the admission. The lock's `tested` list names the
releases a build was tested on, and [development](../docs/development.md#lock-refresh-and-tested-hosts)
records it. A release outside it is admitted with the informational
`controller.untested-release` line, for example "Fedora 45 is newer than the
releases this build was tested on (43, 44); setup checks its floors and
continues", which never changes readiness, the plan or the next command. This
warning is an explicit exception to [D163](milestones/backlog.md#d163)
under [D165](milestones/backlog.md#d165): it concerns Bootwright's own
host, never refuses and is not a component version rule.

A non-empty `/etc/ld.so.preload` is reported as the informational
`controller.loader-preload` line, for example "/etc/ld.so.preload is not empty,
so it loads libraries into every process, root dnf runs included; setup
continues", and is no refusal anywhere: that file is root-owned and already
applies to every root dnf run. Setup, its dry run and `preflight controller`
read it without following a link another account owns; a file they cannot
read as an ordinary empty or absent one is reported the same way.

Bootwright's own executable uses Go's cryptography, and the `cryptography`
wheel bundles its own OpenSSL, which Bootwright never uses for TLS; both stay
outside the host's FIPS-validated modules, while the runtime's Python TLS
follows the host's OpenSSL and crypto-policy
([W4](security.md#the-controller-hosts-attacker-model)). Setup and
`preflight controller` report the kernel's FIPS mode as the informational
`fips-mode` check, with that statement when it is enabled; the check never
changes readiness, the plan or the next command while the kernel's flag reads
0 or 1 or is absent; a flag that cannot be read as 0 or 1 refuses with
`controller.unsupported`, which [B414](milestones/backlog.md#b414) would report
as not verified instead. Bootwright claims no FIPS compliance; a
FIPS-qualified runtime is parked as [B331](milestones/backlog.md#b331).

## The runtime lock

Each build embeds one lock, `internal/controller/runtimelock/lock.json`, in the
format `bootwright.controller.runtime-lock-v1`. It names one `ansible-core`
release of the qualified minor the collection's `requires_ansible` admits, the
Python ABIs it serves, newest first, the glibc floor, the tested releases, the
pip wheel that installs it, and, for each ABI, the exact binary wheel files of
`ansible-core`'s declared closure and of urllib3 for the one controller
downloader, each with its URL on `files.pythonhosted.org`, size and SHA-256.
Its digest is SHA-256 under the domain `bootwright.controller.runtime-lock-v1`
over its canonical JSON re-encoding, so whitespace in the file never moves it.

Versions are chosen when the lock is generated, never at setup: D10's and
D20's "latest patch" is the latest at lock generation, and setup reads no
publisher metadata. `scripts/runtime-lock` generates and verifies the lock,
and [development](../docs/development.md#lock-refresh-and-tested-hosts) owns
refreshing it. Offline `make check` holds the lock to its strict parse, its
one URL host, its digests, one `ansible-core` release across every ABI, each
ABI's wheel tags, its floor and that `cryptography` is present.

Not yet met: dropping `cryptography`, `cffi` and `pycparser` from the lock is
parked as [B559](milestones/backlog.md#b559).

## The controller runtime

**Interpreter.** The runtime runs on the host's own CPython at its versioned
path, `/usr/bin/python<abi>` for an ABI the lock serves, never
`/usr/bin/python3`, so the path fixes the ABI. Setup prefers the interpreter
its receipt names while it still qualifies, so a newer interpreter installed
later never forces a rebuild, and otherwise takes the newest locked ABI. A
candidate qualifies when it, its resolved target and the interpreter's
standard library and `lib-dynload` directories are owned by root with no
group or other write on them or any directory above, when `-I -B` runs a probe
that imports `ssl`, `ctypes`, `zlib`, `bz2` and `venv` and reports its version,
cache tag and glibc, and when that glibc is at or above the floor. With no
qualified interpreter, setup adds the host package requirement `python3.X`
for the first locked ABI X.Y whose dnf preview resolves, newest first
([W1](security.md#the-controller-hosts-attacker-model)). It names the package
rather than the `python(abi) = X.Y` capability because Fedora's alternative
interpreters omit that provide, while every supported release's package of a
locked ABI, the OS's own `python3` included, provides `python3.X`: RHEL 9
AppStream's `python3.12`, Fedora's `python3.13` and `python3.12`.

Not yet met: a standalone CPython for a host whose repositories offer no
locked ABI is parked as [B561](milestones/backlog.md#b561).

**Build.** The runtime digest is SHA-256 over
`bootwright.controller.runtime-v1`, the lock digest, the ABI and the
interpreter path, NUL-separated. Setup builds it in a stage the
[setup subtree](contexts/controller-record.md#the-setup-subtree) hands out:

1. `python<abi> -I -m venv --without-pip <stage>`, so the OS pip wheel is no
   dependency;
2. each locked wheel of the ABI, and the pip wheel, is fetched into the wheel
   cache over HTTPS with bounded redirects and at most 4 concurrent fetches,
   verified for size and SHA-256 before it is renamed into place; a cached
   wheel makes no request;
3. the stage's own Python runs the cached pip wheel as
   `pip install --isolated --no-index --find-links <only the locked files>
   --no-deps --require-hashes --only-binary=:all: --no-compile --no-input
   --disable-pip-version-check` over one `name==version --hash=sha256:...`
   line per locked file, under `env -i` with `LC_ALL=C.UTF-8`,
   `PATH=/usr/sbin:/usr/bin`, `HOME` the stage and `PIP_CONFIG_FILE=/dev/null`;
4. `compileall` writes unchecked-hash bytecode, which `-B` launches still read;
5. the stage must hold no `*.pth`, `sitecustomize.py` or `usercustomize.py`,
   its `pyvenv.cfg` must exclude the system site-packages, and an import probe
   of `ansible`, `jinja2`, `yaml`, `urllib3` and the standard modules must
   report the locked versions;
6. a `bootwright-python` shim runs the venv's Python with `-I -B`, the one
   interpreter path Ansible uses for a local module;
7. `runtime.json` records the lock digest, ABI, interpreter, Python release
   and `ansible-core` release, and the stage is
   [published](contexts/controller-record.md#the-setup-subtree) whole.

A failure in any step refuses `controller.setup` naming the step, with the
remedy `run bootwright setup` again, and leaves no runtime under its digest.

**Launch.** Every private Python launch, a lifecycle block's, a bounded run's
and the controller stage's, locates the runtime of this build's lock on the
interpreter the setup receipt names. It refuses `controller.state`, "the
controller runtime for this build (lock `<12 hex>`, Python `<abi>`) is not
prepared", with the remedy `run bootwright setup`, when there is no receipt,
a setup is pending, another build's setup record stands or the runtime
directory is missing. The launch guard holds `flock(LOCK_SH)` on the
runtime's `.lock` for the whole Ansible block, so retirement never removes a
runtime in use, then repeats the ownership walk over the runtime directory,
`bin/python`, its target and the interpreter's standard library, and requires
`runtime.json` to name that runtime. It holds no RPM database lock: an OS
Python update during a run is an accepted risk that setup's import probe
detects. The launch is `bin/python -I -B` with only `LC_ALL`, `LANG`, `PATH`,
`HOME` and `TMPDIR` set; no loader invocation and no `OPENSSL_CONF` override,
so the host crypto-policy applies, and no `-S`, because the venv's own `site`
supplies its packages. The supervisor refuses an interpreter that is not
isolated, writes bytecode or is not a venv. Setup holds a runtime to what a
launch requires: one whose `.lock` is not a regular file Bootwright owns is not
ready, and setup retires and rebuilds it rather than adopt it.

**Cache-only rebuild.** A launch that finds only the runtime directory missing
rebuilds it before it refuses: apply, destroy, the controller stage, a bounded
run and diff, once per operation, before anything registers or runs. A
continuation of either verb rebuilds it before it presents its plan, and an
apply refused over an incomplete removal names that removal's continuation,
which rebuilds it, rather than setup. When the
receipt is complete, its interpreter still qualifies at the same path and the
wheel cache holds every locked wheel of its ABI and the installer, the launch
waits for `setup/lock` (it never refuses a held lock as busy), repeats those
checks under it, and builds and publishes the runtime exactly as setup does
from the cache alone: no network, no confirmation and no change to the setup
record, so it is a store-internal effect rather than a setup. A runtime another
launch published while it waited is adopted. It refuses with the message above
and `run bootwright setup` when a locked wheel is missing, the interpreter no
longer qualifies or the receipt is incomplete, and a build that fails once
every wheel is cached refuses `controller.state`, "the controller runtime for
this build (…) could not be rebuilt from the wheel cache: `<cause>`", with the
same remedy, `<cause>` being the build's own first message rather than its
setup remedy. Each refusal keeps that code and remedy whatever the setup store
reports for the transaction it ran in. `status` changes nothing: it never rebuilds, and offers
`bootwright setup` while the directory is missing.

**Automation.** The automation is no part of any setup identity. Each
invocation that launches Ansible extracts the embedded tree once into
root-owned staging under `/var/lib/bootwright-staging`, reads it back against
the embedded files and the automation digest, and runs every Ansible of that
invocation from it; the lifecycle runner shares one extraction per invocation,
the controller stage's installer extracts its own. Every adapter process
inherits the extraction's exclusive lock, so the staging sweep never removes a
tree a descendant still runs from, and the invocation releases it at its end.
The digest covers every embedded automation file except documentation, the
collection's root `README` and `CHANGELOG`
([D14](milestones/backlog.md#d14)), under a domain version of its own,
and an extraction holds exactly what it covers. An `ansible/`-only fix
therefore needs a build and an apply, never a setup.

## Host packages

One Go table names the host packages by name or RPM capability:

| Scope | Selected when | Requirements |
| --- | --- | --- |
| Baseline (setup) | always | `openssh-clients`, `podman` |
| Baseline (setup) | no locked interpreter installed | `python3.X` |
| Stage | libvirt client | `libvirt-client` |
| Stage | hypervisor | `libvirt-daemon`, `libvirt-daemon-driver-network`, `libvirt-daemon-driver-qemu`, `libvirt-daemon-driver-storage-core`, `qemu-img`, `qemu-kvm`, `swtpm`, `swtpm-tools` |
| Stage | installer media | `lorax`, `xorriso` |
| Stage | agent-installed OpenShiftCluster with a node that references a declared Machine | `nmstate` |
| Stage | agent-installed multi-node baremetal OpenShiftCluster with an OpenShift-sourced IPv4 virtual IP in a managed libvirt network of a provider hosted on the controller Machine | `iputils` |

`podman` is baseline because the controller Machine always receives the
container runtime; no Machine declares it ([substrate arm](api/machines.md#substrate-arm)).
An entry names no version; the grammar admits `name = EVR` and no entry uses
it until a specific version issue appears.

**Presence** is `rpm -q --whatprovides` run as the invoking user with no
version logic and no repository access. A versioned capability is queried by
its bare name and matches an installed provider's exact `PROVIDENEVRS` entry,
because rpm reads a whole `name = EVR` string as one provide name.

**Preview** runs as root,
`dnf install --assumeno --setopt=install_weak_deps=False -- <missing>`, before
confirmation. Its standard output is display text, bounded to 64 KiB and shown
verbatim in the plan, never parsed for a decision, because dnf4 and dnf5 render
it differently; its expected exit status 1 is no error. A "no match" for a
requirement and a dependency-resolution failure each refuse `controller.setup`
naming the requirement and dnf's last error line, with the remedy of fixing
the host's dnf configuration. A line naming `glibc*`, `libgcc`, `kernel*`,
`systemd*`, `openssl-libs` or the selected `python3.X` appends one
informational note, "this install also updates core packages (glibc); dnf
applies it as for any package install", and never refuses
([W3](security.md#the-controller-hosts-attacker-model)).

**Install** runs as root,
`dnf install --assumeyes --setopt=install_weak_deps=False -- <missing>`, its
output streamed into the run's log. Presence is then read again and must
report nothing missing, or the install refuses `controller.setup`, "dnf
finished but nothing provides `<requirement>`", with the remedy of checking
the host's dnf configuration and running setup again. The evidence records
each requirement's provider NEVRA and the repository `dnf repoquery
--installed` reports for it, because rpm reads no `%{FROM_REPO}`. dnf may
resolve differently between the preview and the install when its metadata
refreshes; the install's output and the recorded NEVRAs are the truth.

`rpm` and `dnf` get a clean environment, `LC_ALL=C` and
`PATH=/usr/sbin:/usr/bin`, stdin from `/dev/null`, a bounded run time and
bounded output, and only the admitted [route](#egress-and-local-effects). They
are never passed `--allowerasing`, `--nogpgcheck`, `--repofrompath`,
`--enablerepo` or `--releasever`. The host's dnf configuration owns
repositories, mirrors and metalink, Satellite or RHSM, `gpgcheck` and its
keys, `proxy=`, versionlock, excludes, protected packages and plugins
([W2](security.md#the-controller-hosts-attacker-model)); Bootwright never
edits a repository and installs whatever the host's enabled repositories
provide, on RHEL as on Fedora. A host that already holds every package makes
no dnf call. The Ceph node's own package path is unchanged.

## The setup flow

`setup` selects no context. It ignores the invoking user's current selection,
resolves no desired state, and refuses an explicit `--context` as a flag it does not accept. This
allows preparation before context creation or Environment import, and makes one
prepared host serve every context created on it. No current directory or user
profile selects inputs. Its acquisition route is the
[invoking environment's](#the-context-free-acquisition-route), and direct
access when that environment names none.

`preflight controller` uses a named context **only when `--context` is explicit
and nonempty**. An explicitly empty value is omission. Nonempty names follow
the existing context-name grammar. An explicit context must be ready and have
an admitted input revision; an empty one returns `context.input` with the
import command. Every check carries the scope that owns it, so a negative
report names the one command that settles it: `setup` for a host prerequisite,
and that context's own `apply --stage controller --context <name>` for anything
its graph selects. A context whose graph selects no target client, libvirt
client, hypervisor, installer-media tooling, `nmstate` or `iputils` has no controller stage, so its
only context check, the binding, names `apply --context <name>`, whose first
apply publishes it. The report's next command and the diagnostic's remedy are
that one decision. An unmet host prerequisite always wins, because the
prerequisites a context adds cannot be prepared on an unprepared host. Global
SSH flags remain unconsumed.

| Invocation | Required behavior |
| --- | --- |
| `bootwright setup --dry-run` | Admit the host as setup does, select the interpreter and list by name the host package requirements setup would install, through unprivileged `rpm` presence queries and without a dnf preview. No setup record is read, so this stays below the privilege boundary. No network, Secret read, privilege escalation or write. Runtime readiness and the setup state are explicitly unverified. With `--output json`, which setup accepts only with `--dry-run`, the result also lists every locked wheel's ABI, file, URL and SHA-256. The `state-root` check reports, from an unprivileged look at the state root, an absent root, which setup creates, or a `root:root` `0700` directory on a local filesystem, whose contents setup verifies. A root of another type, owner or mode, one on an unqualified filesystem, and, when the root's owner runs the dry run, one holding state this build cannot read, such as an earlier build's, are not ready: the dry run reports the check beside the store's own refusal and remedy, and exits 1. The check reaches the root as the store opens it, every ancestor without following a link and on a qualified local filesystem. A root behind a symbolic link in its path is not ready, and the dry run reports the store's own refusal beside the check (`TestTheStateRootInspectionReachesTheRootAsTheStoreOpensIt`). |
| `bootwright setup --download-only --dest-dir <dir>` | Write the lock's files into `<dir>` as [Offline and mirrored wheels](#offline-and-mirrored-wheels) defines. It never elevates and admits no host: it reads no setup record, runs no `rpm` or `dnf` and changes nothing outside `<dir>`, so any connected host can run it. |
| `bootwright setup` | Elevate, then inspect read-only: admission, the installed host, FIPS mode, the untested release and loader preload (informational), the setup state, the interpreter and its runtime and the baseline package presence. A ready host prints the receipt table and `Nothing to do.`, with no network request, confirmation or write. Otherwise run the preview and present the plan, confirm, take `setup/lock`, inspect again and refuse a host that moved, record the pending intent, install with dnf, fetch, build, verify and publish the runtime, and write the receipt, which clears the pending intent. Publish no binding and touch no controller record. |
| `bootwright preflight controller` | Read and verify the same host checks as setup with bounded local probes and no dnf preview, and report the host's FIPS mode and the missing package names. May use the verified privilege boundary for private metadata. |
| `bootwright preflight controller --context <name>` | Additionally report that context's own target tools, its libvirt client, hypervisor, installer-media, `nmstate` and `iputils` requirements by `rpm` presence, and its host binding, as context-scoped checks. Contact no publisher and read no repository metadata for them. |

Neither command contacts a managed endpoint, creates or repairs shared state
on behalf of the other's scope, or publishes a binding. Preflight publishes
nothing at all, and neither preflight nor a dry run runs dnf, downloads a
wheel or refreshes repository metadata.

The plan names each action setup takes, in order:

- `discard-setup-state`, when the setup record cannot be read or carries
  another format: "Discard setup state another Bootwright build wrote
  (`<format>`)", which removes the subtree after confirmation;
- `resume`, when a pending intent is present: "An earlier setup
  (`setup-000042`) was interrupted during `<step>`; this run re-inspects the
  host and continues";
- `native-packages`, with the verbatim dnf preview and its core-package note;
- `runtime`, for example "Python 3.14 at /usr/bin/python3.14, ansible-core
  2.21.5: 3 of 10 wheels to download (2.1 MB), 7 cached".

A dry run, preflight and an interrupted setup never discover versions: the
lock decides every wheel and the host's dnf every package. A valid dry run
exits successfully with unverified checks visibly labeled; it never reports
completed setup. Preflight requires positive current evidence for all
selected checks; an unbound context is a failure that names its controller
stage when it has one, and otherwise `apply --context <name>`, whose first
apply publishes the binding, distinct from host mismatch, which names the
restored host. Its binding check observes that it is not yet bound and that
its first apply binds it, and stays not ready rather than pending.

`--yes` suppresses ordinary confirmation only. Use the
[ordinary confirmation](cli.md#ordinary-confirmation), with the plan before
the prompt; decline, noninteractive input without `--yes`, or cancellation
starts no setup mutation. Setup changes neither current-context selection nor
Environment input, and claims no context.

`--wheel-mirror` and `--download-only --dest-dir` serve a host that cannot
reach `files.pythonhosted.org`; [Offline and mirrored wheels](#offline-and-mirrored-wheels)
owns them.

## The controller stage

`apply --stage controller` installs the prerequisites one context's own graph
selects. It is one block in that context's lifecycle plan, planned by its
capability from effective state alone and frozen before any effect: the target
client requirements the graph names, the closures it selects with the
[host package](#host-packages) requirements each one names, the libvirt
intent, and the controller Machine's normalized proxy choice. The libvirt
client closure is selected by a libvirt
provider hosted on the controller Machine, or any Machine a libvirt provider
hosts, and by nothing else; the [hypervisor closure](substrates.md#provider-host-realization)
by a libvirt provider hosted on the controller Machine; the
[installer-media tooling](managed-os.md#installation) by an Anaconda
installation published through an artifact server on the controller Machine;
the `nmstate` package by an agent-installed OpenShiftCluster with a node whose
`machineRef` names a declared Machine; and the `iputils` package by an
agent-installed OpenShiftCluster with at least two nodes on the `baremetal`
platform that has an OpenShift-sourced IPv4 endpoint address inside a managed
libvirt network attachment of a provider hosted on the controller Machine.
No closure forces another into the request. The plan presents each selected
requirement by name, `install-host-package <requirement>`; planning reads no
host, so it names what the closures require, and a requirement the host
already provides is left as it is.

Target clients are selected by the admitted graph: OpenShift/OKD clients
(`oc`, `kubectl`) and installer match the selected release; Kubernetes
consumers select Helm; referenced vSphere providers select `govc`;
virtualization selects upstream `virtctl`. They resolve under the version
intent the Environment's [`dependencyVersions`](api/environment.md#dependency-versions)
declares. OpenShift/OKD installer and clients remain tied to the target
release. A cluster pinned to a release image still selects its clients from
the declared `release.version`; without that version the stage refuses rather
than inferring a client release from the image. `virtctl` uses upstream
latest unless explicitly overridden; no remote target discovery or automatic
compatibility inference is required. A tool's `latest` intent is resolved by
the attempt that installs it and retained from then on; moving a retained
`latest` client to a newer release requires declaring that release in
`dependencyVersions`. A host package is whatever the host's repositories
provide. An agent-installed OpenShiftCluster node that references a declared Machine
selects the host's `nmstate` package for the installer's `nmstatectl gc`
invocation; the `iputils` package serves the virtual-IP duplicate-address
probe.

Each target tool's SHA-256 comes from its publisher's own metadata, read over
TLS to that publisher and never from a mirror
([dependency versions](api/environment.md#dependency-versions)), and the
adapter verifies the source against it before extraction. No publisher
signature is verified for any tool, so that TLS and the publisher's metadata
are the whole authenticity a source has, a named widening of
[publisher authenticity](security.md#cryptography-and-supply-chain):

| Tool | Release resolved from | SHA-256 from |
| --- | --- | --- |
| OpenShift `oc`, `kubectl` and installer | the declared release | `sha256sum.txt` beside the archive on `mirror.openshift.com` |
| OKD `oc`, `kubectl` and installer | the declared release's GitHub release | the release asset's digest, else `sha256sum.txt` beside the archive |
| standalone `kubectl` | the declared release, or `dl.k8s.io/release/stable.txt` for `latest` | `<archive>.sha256` on `dl.k8s.io` |
| Helm | the `helm/helm` GitHub release | `<archive>.sha256sum` on `get.helm.sh` |
| `govc` | the `vmware/govmomi` GitHub release | the release asset's digest, else the release's `checksums.txt` |
| `virtctl` | the `kubevirt/kubevirt` GitHub release | the release asset's digest; a release without one refuses |

A context that selects nothing beyond the host
baseline contributes no block at all.

The block first reads what is missing. Presence reads each selected host
package requirement, and the target clients are recovered from retained
identities alone, which reads no publisher metadata. A host that already
provides every selected requirement and carries every selected client reports
`unchanged` without dnf, acquisition, repository access or publication. Only
missing tools are resolved.

Before any effect the attempt records its intent, the
[preparation](state-reconciliation.md#state-machine) of its running attempt:
the requirements it is about to ask dnf for and the exact tool source
identities it is about to add, which are first retained in the shared
controller record. A declared `spec.dependencyVersions.libvirt` other than
`latest` refuses before anything is read, naming the Environment and the
remedy of removing it: the host's repositories decide the libvirt release.

Missing host packages are installed as root under `setup/lock`, so setup and
another context's stage never run dnf beside it; a busy lock refuses rather
than waits. Under the lock the stage refuses an interrupted or foreign setup
record as a launch does, reads presence again and asks only for what is still
missing, previews and installs as [host packages](#host-packages) does,
writing the preview's display text and the install's output into the
attempt's output; cancelling the install sends `SIGTERM` to dnf's process
group. A preview that finds no match or cannot resolve the install refuses,
and the attempt is failed with nothing changed. A failed install, which may
have changed the host, leaves the attempt unknown, and its resolution reads
presence again. When the controller Machine selects a proxy, dnf receives that
route as `https_proxy` and `no_proxy`.

Target clients are published into a
[tool area](contexts/controller-record.md#tool-areas) named by the digest of
that exact closure, under its reservation, attribution and sealing rules.
Every successful stage, one that published nothing included, ends by naming
on the context's binding the area it proved, or none when it selects no tool,
and then retires every sealed area no binding names, under the
[retirement rules](contexts/controller-record.md#tool-areas); a retirement that
fails after the area was sealed leaves the attempt unknown, and one that fails
on a stage that changed nothing leaves it failed
(`TestEverySuccessfulStageNamesItsAreaAndRetiresTheRest`,
`TestARetirementFailureKeepsTheStageOutcomeHonest`). The block completes only
after every selected requirement is provided again and every tool it published
is proved present by presence alone and its area sealed. An area an earlier
attempt published but never sealed is sealed by every path that completes the
stage, one that installs a missing host package included; a failure before
the seal attempt of a stage that changed nothing leaves the attempt failed,
and one from the seal attempt on leaves it unknown
(`TestAnInterruptedPublicationNeedsAReplayToSeal`,
`TestAReplayThatInstallsAHostPackageSealsTheInterruptedPublication`,
`TestASealFailureAfterAReplayedHostPackageInstallIsUnknown`). Its evidence names
each requirement's installed providers by NEVRA, and each published tool by
kind, release and source digest. Presence is read through `rpm` alone, so a
host that already holds every package runs no dnf; only the reading after an
install also asks `dnf repoquery --installed` for each provider's repository.

The `controller-prerequisites-v6` Ansible adapter acquires target tools and
nothing else. It receives one frozen request with the block's request digest
as its identity, at least one exact tool source, an acquisition deadline in
seconds for each tool source in order, the tool area it may publish into and
the declared egress. That area's writability is the adapter's authority to
change anything at all; the fixed automation runs from the stage's own staged
extraction ([automation](#the-controller-runtime)). The fixed controller
playbook composes `bootwright.core.controller_prerequisites`. It validates the
request, announces the exact sources it is about to add, which must equal the
sources the stage already recorded as the attempt's intent, and waits for
each tool's acknowledgement before installing it. Target archives are
verified before safe extraction of only the named regular members into
private versioned tool locations. Existing files are verified by their
streamed digest, never overwritten. A target source streams to disk under its
own acquisition deadline, each chunk written and digested before the next is
read, and its members stream into place the same way, so neither a source nor
a member is ever held whole in memory; an interrupted transfer leaves only an
unlinked file. Only naming a complete, proved file creates the directories it
is named in, so a refused source or member leaves no directory behind. Before
any member of `openshift-clients` is published, its `oc` must name the frozen
release, for both compatibilities. A released `oc` names its release in its
own bytes: the version, NUL-terminated, overwrites the head of a fixed 93-byte
marker. The bytes must hold exactly one marker stamped with the frozen version
and no unstamped one, or neither `oc` nor `kubectl` is published, and the
retained source is all the refusal leaves. The adapter names this refusal to
Go in a [`refused` record](architecture.md#the-adapter-result-protocol) before
it fails, so its diagnostic's remedy names the release-stamp check and asks
for a `release.version` or `downloads.openshiftClientsMirror` whose `oc`
passes it: the same release and mirror reuse the retained source and refuse
again, so restoring sources cannot help. No downloaded tool is ever executed,
so the stamp is read, never asked of the tool. Request, downloads, expanded
members and callback frames have fixed bounds. One connection pool serves each
route, direct or through the proxy, for one tool's source.

An adapter that refuses names one closed class to Go in a
[`refused` record](architecture.md#the-adapter-result-protocol) before it
fails, and never the exception's text: an acquisition names `dns`,
`certificate`, `timeout`, `unreachable`, `proxy`, `status`, `redirect`,
`integrity`, `trust` or `storage`, with the `source` of the tool it was
acquiring, and any other refusal is `internal`. Go maps each class to its
diagnostic and remedy, naming the source's host, and for `storage` the tool
area that source was being written into; `internal`, a class without the
source being installed, and a source the run is not installing keep the
generic adapter failure or break the protocol. A run that failed after
announcing its sources but before Go acknowledged any tool installed nothing
and is failed; once a tool's acknowledgement may have been delivered, it is
unknown.

Read-only Go verification reconstructs installed target file inventories from
approved retained sources. It streams each target source once and each
published target member it proves, holding only their digests, so neither is
ever held whole in memory; an area that cannot stream a file is refused rather
than read whole. When it projects an `openshift-clients` source, it also reads
`oc`'s release stamp as the member streams, as the adapter does, and refuses a
source whose `oc` does not hold exactly one marker stamped with the frozen
version and no unstamped one with the same release-stamp diagnostic. A tool
area's readiness stays presence only and reads no bytes, so the stamp of a
client already in a sealed area is not read again.

Every other block of the plan runs the clients this stage installed, and
locates each executable through this block alone, from what the block proved
in the apply whose effects the asking operation runs over: that apply, or the
apply a removal takes back, because the stage's own removal retains and proves
no closure. That proof names the tool area and the exact bytes of each client,
so the stage recovers the closure from the retained identities of exactly
those clients and answers with the file the proved area holds. The host's
retained sources are shared, so recovering the closure from all of them would
select the newest release another context retained under the same `latest`
intent, an area this context never published. An apply that proved no
completed stage, a closure only partly retained or naming another area, an
area that does not exist, or one without the file answers that the executable
is not installed and names `apply --stage controller`, and no search path is
ever consulted instead.

Each client source has an acquisition deadline of 2 minutes plus its declared
bytes at 512 KiB/s, rounded up to a whole second. A tool publication's
deadline is the controller run's 10 minutes plus the acquisition deadline of
every tool source it selects, held to the controller stage's 2-hour ceiling; a
closure whose deadline would pass that ceiling is refused before Ansible
starts, so the ceiling never shortens a run it admits. The
[bounds table](contexts.md#storage-locking-and-publication) names both
constants.

Because the stage is a lifecycle block, what dnf and its Ansible print is
retained as that block's [attempt output](cli/output.md#private-operation-logs),
exactly as every other adapter run's is.

The closure is shared host state. Two contexts selecting the same clients prove
the same sealed files, a different closure gets its own area, and removing a
context retains both: destroying a context uninstalls no host package and
deletes no tool area, so resolving the stage's removal runs nothing and is
always its completion. Only deleting the context drops its binding, and the
area no remaining binding names is then retired. Recovery is idempotent rather than compensating:
dnf installs only what is missing and publication verifies existing bytes
instead of overwriting them, so an interrupted stage is completed by repeating
it. `preflight controller --context <name>` reports the same closures by
presence, one check each for the libvirt client, the hypervisor, the
installer-media tooling, `nmstate` and `iputils` the context selects, read through the same
requirements, and requires every one of them; it names this command when one
is not yet installed. Setup remains the owner of the host baseline, and this
stage never installs it, publishes a binding, or claims another context's
resources.

A failure this stage meets in an adapter it shares with setup (the tool
catalog, its publisher metadata, transport, acquisition or trust, the host's
dnf, or the controller Ansible run) keeps its own diagnostic code and names
this stage's own `bootwright apply --stage controller --context <name>` as
what settles it, never `setup`, which installs nothing a context selects.
`preflight controller --context <name>` names the same command for the same
failures it meets while it recovers that context's clients by presence. That
remedy offers the correction alone, even for a publisher the host could not
reach or resolve: the block froze the controller Machine's proxy choice it
acquires over, and the failed apply holding it can only be
[continued over its exact input](state-reconciliation.md#state-machine), so
another route is nothing that command can settle. A refusal of the controller
record that an apply or this stage meets names its own remedy: a host without
completed setup names `bootwright setup` and then the refused command, another
host names the host the state belongs to, and a context bound elsewhere names
its bound Machine input or a new context. A publication of the controller
record whose outcome is unknown names, once storage is healthy,
`bootwright apply --context <name>` while an apply binds the context, reserves
host resources, reserves, attributes, seals or retires a tool area, or retains tool
sources; `bootwright destroy --context <name>` while a destroy releases its
reservations; and `bootwright context delete --name <name> --purge` while a
context deletion, which the registry already records, drops the context's
binding and reservations and retires the tool area only that binding named
(`TestAnUnknownControllerPublicationNamesTheApplyThatMetIt`,
`TestAnUnknownControllerPublicationNamesTheDestroyThatMetIt`,
`TestAnUnknownControllerPublicationNamesTheDeletionThatMetIt`).

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

**Trust off the Fedora/RHEL family.** The `tls-ca-bundle.pem` read above is the
only trust the wheel fetch reads while that file exists, and an unsafe one is
refused. Where the file is absent, as on a Debian, SUSE or Alpine workstation
that runs `setup --download-only`, the fetch uses Go's system certificate pool,
which reads the host's own bundle locations; a host with no trust at all is
refused naming the absent bundle and `--wheel-mirror`. A wheel is used only
when its size and SHA-256 match the lock, so the TLS trust never decides what
is installed (`TestAnAbsentFedoraBundleFallsBackToTheSystemPool`,
`TestAnUnsafeBundleIsNeverReplacedByTheSystemPool`).

**Wheel origins.** Setup fetches only the URLs the lock names, on
`files.pythonhosted.org`, over the route it admitted, or the files of a
`--wheel-mirror` in their place; pypi.org, uv metadata and GitHub are not
contacted. A cached wheel is never fetched again.

### Offline and mirrored wheels

**`--wheel-mirror <mirror>`** moves where setup, and `setup --download-only`,
read each locked wheel from: the lock's URL becomes `<mirror>/<filename>`.
The mirror is untrusted transport. The lock's size and SHA-256 alone decide
whether its bytes are used, through the same wheel cache and the same
verification as a fetch from the lock's URL, so a mirror cannot change what is
installed, only where the bytes come from. A mirror is one of:

- `file:///<absolute directory>`, with no host. Setup opens
  `<directory>/<filename>` without following a link or waiting on a pipe,
  requires the opened file to be a regular file of the locked size, never a
  link, a device or a pipe, and copies it into the cache from that handle.
- `https://<host>[/<base>]` on the default port, with no credentials, query or
  fragment, fetched over the same client as a lock URL: the system trust
  store, the admitted [route](#the-context-free-acquisition-route), HTTPS-only
  redirects and a bounded chain.

Any other form, including `http`, a credential, a query or a relative
directory, is a usage error naming the accepted forms. The mirror's base path
is kept percent-encoded, so an encoded `?` or `/` stays part of one path
segment. A wheel the mirror lacks, answers with another status or holds with
other bytes fails setup before that file enters the cache and before the
runtime is built, naming the file, the mirror and the remedy: place the locked
wheels there, or drop `--wheel-mirror`. An HTTPS mirror host this host cannot
reach is refused as any unreachable publisher is. A cached wheel is still never
fetched again, so a mirror is read only for what the cache lacks. The mirror
only replaces wheel origins: it changes no dnf configuration, and the plan's
runtime action names it.

**`setup --download-only --dest-dir <dir>`** is the counterpart of
`dnf --downloadonly --destdir`. It runs unprivileged on any connected host and
never elevates: the invocation classification treats it as `setup --dry-run`
is, and the command reads no setup record, takes no lock, admits no OS family
and runs no package manager. It writes into `<dir>`, which it creates when
absent:

- every locked wheel of every Python ABI, because the offline host's
  interpreter is not known here, and the pip wheel, each as `<filename>`, so
  that `<dir>` serves as a `file:///<dir>` mirror unchanged;
- `lock.json`, a copy of the lock this build embeds, byte for byte.

Nothing else is written. A wheel is copied from the wheel cache when this
account can read it, which is the case when it runs as root, and is otherwise
fetched from the lock's URL, or from `--wheel-mirror`, over the admitted
route. Each file passes the same size and SHA-256 verification before it
takes its name, through a partial file that a refused or interrupted transfer
leaves nothing of. A file already in `<dir>` with its locked size and SHA-256
is kept and not fetched again. Files are `0644` and the directory `0755`.
`--download-only` requires `--dest-dir`, conflicts with `--dry-run`, and takes
no `--output json`; a missing or unwritable `<dir>` is a failure naming it,
and a failed transfer names the file and the host.

**`setup --dry-run --output json`** lists every locked file under `files`: its
Python ABI (absent for the pip wheel), filename, URL and SHA-256, so an
operator can fetch them by other means. With `--wheel-mirror`, the plan's
runtime action also names the mirror; the listed URLs stay the lock's.
The fields are the `setup` row of the [result fields](cli/output.md#json-output).

**The dnf route.** When the admitted route is a proxy, dnf receives it as
`https_proxy` and `no_proxy`, and a `proxy=` in the host's dnf configuration
wins over it. Nothing else ambient is forwarded, and credentials and
`ALL_PROXY` stay unsupported. A local `file://` repository, a DVD, Satellite
or a mirror in dnf's own configuration all serve, and a host that already
holds the packages makes no dnf call.

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
needs no Secret consumer. The endpoint's host is recorded in lowercase.

`NO_PROXY` is a comma-separated list of the same bypass entries a declared
[proxy choice](api/infrastructure-services.md#proxy-choice) carries: a host
name, a domain suffix with or without a leading dot, an IP address, a CIDR
block, a `host:port` pair, or `*` for every destination. Entries are trimmed,
deduplicated and bounded by what the controller stage's acquisition may carry,
each entry at most 1024 bytes, matching consults no resolver, and a bypass list
without a selected proxy is nothing. An unqualified value refuses before any
privilege escalation, acquisition or local effect, and names the variable to
correct.

The route selects transport only. It never moves a lock digest, wheel digest,
version or approved closure, and it reconfigures nothing on the host. A setup
interrupted and repeated over another route re-inspects and continues, because
nothing it recorded depends on the route.

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
still receive no proxy variable of their own: the route reaches the wheel
fetcher, the automation and dnf as request data, exactly as a declared route
does.

Reject a bootstrap dependency on a proxy or other service whose readiness
requires this setup or a subsequent apply. Admission of a reference is not
readiness evidence. TLS verification against system trust, digest checks,
bounded redirects and acquisition limits follow [Security](security.md).
Source URLs contain no credentials; Python, pip, Ansible and executable-path
configuration cannot alter the approved closure.

Controller consumes typed local inspection, host-package, runtime, staging and
tool-publication ports. Requests carry exact target evidence, dependency
identities, authorized actions, route, limits and cancellation. Results report
per-action postconditions and `unchanged`, `changed`, `failed`, `canceled` or
`unknown` outcomes; they contain no raw subprocess transcript. Pure policy
selects and orders actions; composition binds fixed local adapters. Local tool
installation crosses the same controlled
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
claiming any context and writes no controller record; the first apply that
uses a prepared host creates the [controller record](contexts/controller-record.md)
with this host's identity and publishes its binding. Later operations and
context preflight verify that exact binding under
[controller-host protection](state-reconciliation.md#controller-host-protection);
they never silently rebind.

Workspace serializes shared prerequisite mutation across every context in the
fixed root using its exclusive root lock, acquired before a context lease.
Preflight holds a shared root lock when the root exists, for coherent stored
evidence. An absent root reports missing setup with `setup` guidance
and creates nothing. A busy lock refuses; there is no timeout-based takeover.
Setup's own subtree has its own lock, `setup/lock`, which setup, runtime
publication and the controller stage's dnf take; a launch takes none and holds
its runtime's `.lock` shared instead. The OS package-manager lock is an
additional requirement, not a replacement for Bootwright coordination.

**Setup subtree.** Setup keeps its pending intent, receipt, runtimes, wheel
cache and runs in `/var/lib/bootwright/setup/`, independently versioned as
`bootwright.setup-state-v1` and outside the registry; the
[controller record](contexts/controller-record.md#the-setup-subtree) owns its
layout and bounds.

**Execution closure.** An operation freezes
`bootwright.controller.execution-closure-v2`: SHA-256 over that name, the lock
digest and the ABI, with the ABI and `ansible-core` release a refusal names
([dependency safety](state-reconciliation.md#dependency-safety-during-recovery)).
An OS Python z-stream update, an OS release upgrade that keeps the ABI, and any
rebuild with the same lock continue a live operation; a new lock or another
ABI refuses its continuation, naming the build that registered it, whose setup
restores its runtime, fetching any wheel the cache no longer holds over the
network or with `--wheel-mirror`, because [retention](#publication-and-interrupted-setup)
keeps only the current and previous runtimes and a setup-format discard keeps
none, or the destroy that takes back what it owns.

No service port or container name is reserved by setup.
[Infrastructure services](infrastructure-services.md#host-reservations) defines
those conflict identities and joins this host coordination through the shared
root lock and the stored reservation record. Dependency readiness never
establishes service ownership or authorizes another context's resources.

## Publication and interrupted setup

Setup's receipt is private durable evidence, separate from lifecycle operation
state and logs, in the [setup subtree](contexts/controller-record.md#the-setup-subtree).
What its dnf prints is kept beside it in a
[setup run](cli/output.md#setup-run-output), which is troubleshooting material
only: neither keeping one nor failing to changes a setup outcome or its
receipt. A receipt is no lifecycle operation ID, ownership of the host or
GitOps readiness.

**Intent before effect.** Before dnf or the runtime build runs, setup writes
and synchronizes the pending intent: its run ID, plan digest, remaining
actions and start time. Each finished action shortens that list, and the
receipt written last clears it. A setup that fails before any dnf transaction
began, as a failed wheel fetch does, also clears it, because a runtime
publishes only whole and nothing else changed, so the runtime a kept receipt
names still launches. Interrupted or failed setup may leave approved
packages installed; nothing is rolled back, and shared OS packages are never
removed.

**An unknown outcome blocks every consumer.** A launch that finds the pending
intent refuses `controller.state`, "a controller setup (`setup-000042`) was
interrupted before it finished", with the remedy `run bootwright setup`, adding
`--wheel-mirror <mirror>` on a host that cannot reach the wheel origins, or,
when it keeps a receipt, the setup of the build whose lock that receipt names,
which keeps its runtime. Setup itself
re-inspects: its plan names the interrupted run and the step it stopped in and
asks for confirmation, then installs only what is still missing and publishes
a runtime only whole, so both effects are idempotent. No stranded, abandoned
or canceled receipt exists, and no setup refuses an earlier one's route or
executable.

**Format healing.** A setup record that cannot be read or carries another
format, or a setup subtree holding an entry this build does not list, is
another build's; setup's read and its locked transaction judge it alike. Setup plans "Discard setup state another
Bootwright build wrote (`<format>`)", removes the subtree after confirmation,
taking every runtime's lock first so it never removes one in use, and
proceeds. Because the discard removes every runtime and the wheel cache, the
plan treats them as absent: its `runtime` row counts every locked wheel as one
to download, and setup fetches, builds and publishes the runtime before the
receipt names it. A launch refuses as the
[setup subtree](contexts/controller-record.md#the-setup-subtree) states. A
future setup-format change therefore never leaves setup
unable to heal its own state, but the discard also removes the previous
runtime: an operation registered under the discarded closure continues only
through its registering build's setup, which fetches its wheels again and so
needs network or `--wheel-mirror`, and which discards this build's subtree in
turn, or through the removal that supersedes it.

**Publication.** A runtime is published only whole, as the
[setup subtree](contexts/controller-record.md#the-setup-subtree) defines.

**Retention.** Setup's runs are bounded to the newest 8: opening a run removes
the oldest beyond 7 first. The receipt names the current runtime and the
record's `runtimes` the current and the immediately previous distinct one. After
each setup that writes its receipt, under the same `setup/lock` and before it
reports:

- every published runtime but those two is retired, each under
  `flock(LOCK_EX|LOCK_NB)` on its `.lock`; one a running command holds is
  skipped and kept, and setup reports the informational
  `controller.runtime-retention` line "kept: `<N>` runtimes (`<M>` in use)";
  a runtime whose `.lock` is gone is retired, since no launch can hold it;
- the wheel cache is pruned to the wheels every runtime still published names
  (read from the `requirements.lock` its build left, which also names the
  installer wheel that build used), this build's locked wheels of the
  receipt's ABI and this build's installer wheel; every other cache
  entry is removed.

The previous runtime keeps a live operation registered with the earlier lock
continuable offline until a later lock change or a setup-format discard
retires it. A fault in retention never changes the setup's outcome or
receipt: setup reports it as a `controller.runtime-retention` line and the next
setup retries. A setup with nothing to do writes nothing and retires nothing.

Cancellation stops authorization of new effects. Once dnf is running, the
install is ended by `SIGTERM` to its process group, and dnf's own transaction
handling decides what it leaves; the next setup reads presence again. The
unprivileged sudo supervisor relays the operator's interrupt and waits for the
elevated command with no deadline of its own, so a second operator signal is
the only kill ([local privilege](cli.md#local-privilege-and-user-identity)).

**The cut-over refusal.** A store a build before the setup v2 cut-over wrote
refuses every access with the fresh-install refusal
[contexts](contexts.md#storage-locking-and-publication) owns; setup's first
read refuses so, before any dnf preview, plan or confirmation.

An input update may change a context's controller prerequisite intent, which
its next controller stage installs, but cannot silently change an established
controller Machine/host binding. A host prerequisite that loses readiness is
prepared again by `setup`. What deleting a context removes from shared host
state follows the
[host-binding rules](contexts.md#controller-relationship-and-host-binding).

## Results and qualification

Both commands support human, JSON and YAML results through the
[shared output contract](cli/output.md#json-output). A dry run preserves its
existing JSON result schema; other structured results project the returned
checks, actions, preview, receipt and next command through CLI-owned tagged
DTOs, suppressing streamed progress. Order checks and actions by dependency
order, then stable prerequisite identity. Show the
baseline or explicit context scope, the resolved acquisition route with what
selected it, required/observed versions, planned changes, readiness and next
safe command. A direct route names what chose it too: `direct (no
HTTPS_PROXY)` for an invoking environment that names no proxy, and `direct
(Machine <name>)` for a context's controller Machine. A refused or failed
setup's next command is the `bootwright` command its remediation names, and it
names none when the remediation names none, as beside a confirmation refusal,
whose remedy repeats the operator's own invocation with `--yes`. Both commands
stream [long-running progress](cli/output.md#long-running-progress): the
scope, then one `Checks` row per host check as it is verified, and, after
confirmation, one `Progress` step per action. The host checks are `host`,
`installed-host`, `fips-mode`, `state-root`, `setup-state`, `python-runtime`
and `host-packages`; the context checks are `target-tools`, `libvirt-client`,
`hypervisor`, `installer-media`, `nmstate`, `iputils`, `controller-binding`
and one `listener-firewall` row per declared controller socket.

Context preflight and public `plan` inspect the controller's managed DNS
(TCP and UDP), NTP (UDP), proxy (TCP), artifact listeners (TCP), and locally
hosted emulated BMC listeners. Loopback listeners require no ingress admission.
Exact bind addresses, or every declared endpoint of a wildcard listener, must
belong to one local interface. Inspection reads that interface's runtime
firewalld zone and reports the socket, interface, zone and `open`, `closed` or
`unknown` result. It never changes firewall configuration.

Only an explicit runtime port/range or an accepting zone proves admission in
the bounded interface-zone shape. An active policy with target `CONTINUE`,
no transport rules, no redirect or masquerade, and only ICMPv6 accept rules
is harmless to the inspected TCP/UDP sockets regardless of its name; this
includes the [shipped host IPv6 policy](https://github.com/firewalld/firewalld/blob/main/config/policies/allow-host-ipv6.xml). Source-bound zones, other policies, direct or
passthrough rules, rich rules, admission depending on unresolved service
expansion, and unsupported or
ambiguous inspection report `unknown`. A stopped, missing or inaccessible
firewalld also reports `unknown`; another firewall's presence or absence is
never inferred safe. The operator must verify the declared port from every
BMC, installer and client, configure the operator-owned firewall, and repeat
preflight. These rows inspect the local zone, not end-to-end reachability.

Unknown and closed required sockets cannot pass preflight. Public `plan`
retains its preview and includes the observations and manual remedy. Before
claims or registration, `apply` checks every starting listener and the
listeners its starting consumers depend on, including listeners already
proved ready. It repeats inspection under the exclusive transaction. A
controller-only stage with no listener dependency has no listener admission
requirement. The native queries use the runtime semantics documented by
[firewall-cmd](https://firewalld.org/documentation/man-pages/firewall-cmd.html)
and [zone targets](https://firewalld.org/documentation/man-pages/firewalld.zone.html).

Setup's ready path and `preflight controller` render the same short receipt
table, for example:

```text
Host      Fedora Linux 44 (amd64), glibc 2.43 — tested
Runtime   Python 3.14.8 (/usr/bin/python3.14), ansible-core 2.21.5 — ready
Packages  podman 5:5.8.1-1.fc44 (updates), openssh-clients 10.2p1-1.fc44 (present before bootwright)
Nothing to do.
```

`Host` names the operating system and release, the architecture, the glibc of
the selected interpreter and whether the release is in the lock's `tested`
list. `Runtime` names the interpreter, its path and the ansible-core version,
then `ready` or `not prepared`. `Packages` names each receipt package as its
provider's name and version with the repository it came from, or `present
before bootwright` for one setup did not install, and names what is missing
instead when a package is. `preflight controller` renders the table in every
result that reached it, ready or not, in place of the host's ready check
rows; a host check that is not ready, such as an interrupted setup, and the
context's check rows remain. Both end a ready result with `Nothing to
do.`. Setup renders the table only on its ready path, and never in its dry run,
which reads no setup record.
`preflight controller` stays read-only and runs no dnf preview.

Host fingerprints, credentials, environment dumps and raw native-tool output
are not public results. The one private path setup names is the directory of
the [setup run](cli/output.md#setup-run-output) keeping dnf's output, as a
`Logs` field before dnf starts and again with the result; a failure of dnf
leads its remedy with the run's `run.output` and that reading it needs root.
Neither command has a lifecycle receipt or a private operation log.

Each check reports `ready` or `not-ready`, or `unverified` where a dry run
cannot verify it. An informational line, `controller.untested-release` or
`controller.loader-preload`, is a `warning` diagnostic on standard error,
written once with the result; it never changes the outcome or exit status.
Setup writes the lines its plan carried before that plan and its prompt, and
those raised after it, such as `controller.runtime-retention`, with the result.

The [diagnostic taxonomy](cli/output.md#diagnostic-taxonomy-and-order) owns
`preflight.*` and `controller.*` failure codes; existing context, privilege
and cancellation codes retain their meanings. Every refusal names its remedy.
A failed acquisition names the publisher host it could not use and
distinguishes unresolvable names, an untrusted certificate, an expired
acquisition timeout and an unreachable endpoint, because each needs a
different operator action and none of them is a corrupt retained dependency.
Request paths, transport text and operating-system error strings stay private.
The [output contract](cli/output.md) owns streams and exit
status, including complete negative readiness reports and partial setup
outcomes. No setup failure may imply that already verified effects rolled back.

Fedora and RHEL share one set of host-independent request/result tests
covering clean installation, the ready no-op, context-free preparation,
read-only preflight and dry-run, unsupported hosts, substituted executables,
an interrupted setup, another build's setup format, cancellation and package
failures. A fake adapter proves this contract and never proves an executed
dnf install. Collection syntax, pinned lint, sanity, unit and local
integration checks qualify the shipped Ansible entrypoints as content; sanity
also imports every module and module utility under a pinned interpreter of the
oldest Python a managed host may run, which
[development](../docs/development.md#lock-refresh-and-tested-hosts) records. A
fixed worker the automation sends to a managed host as source, and an
entrypoint it runs inside a container image, imports only the target
interpreter's standard library. Each release a build runs on is an
[acceptance ledger](../docs/acceptance.md#ledger) row, in place of a list of
qualified releases.
[Milestones](milestones.md#completion-and-verification) own the verification
model and which acceptance remains operator-run.
