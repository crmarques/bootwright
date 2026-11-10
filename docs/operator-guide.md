# Operator Guide

This guide is for running Bootwright on a real host: preparing the host,
running a lab journey and recording what happened in the
[acceptance ledger](acceptance.md). Changing Bootwright, its checks and its test
harnesses is [development](development.md). Each example's README owns its own
journey, and [milestones](../specs/milestones.md#status) owns which run is a
gate and which build it must match.

## Prepare a host

`controller install` prepares what every context on the host shares: the host package
`openssh-clients` and the controller runtime that runs Ansible. A context's
controller stage adds what its graph places on this host, such as `podman` for a
managed service or the hypervisor for a hosted libvirt provider.
[What setup admits and installs](#what-setup-admits-and-installs) says which
hosts it accepts and what it changes on them. Every command that runs Ansible
unpacks the automation it embeds beneath /var/lib/bootwright-staging, which
the first of them creates. Each example's host
prerequisites add what its journey needs, such as `/dev/kvm` for a libvirt
guest. Run as root, or from an account that may run
`sudo`: Bootwright asks for authorization when a command needs it, and keeps its
state under `/var/lib/bootwright`
([contexts](../specs/contexts.md#storage-locking-and-publication)).
[The sudo rule](#the-sudo-rule) says what that account's policy must permit,
[directory accounts](#directory-accounts) how an SSSD, LDAP or AD account runs
it, [a network home with root squash](#a-network-home-with-root-squash) where
the executable must live,
[a FIPS-mode controller](#a-fips-mode-controller) what its runtime claims and
which SSH keys it can use.

Prepare the host once, from the repository root:

```sh
make build
./bin/bootwright version
./bin/bootwright controller install --dry-run
./bin/bootwright controller install
./bin/bootwright controller check
```

`make build` stamps `bin/bootwright` with the commit and source state of the
checkout, which `version` prints as its `Commit` and `Source` lines. A
controller needs no checkout or Go toolchain: run `make build` on a
Linux/amd64 workstation and copy `bin/bootwright`, a static executable that
embeds its automation, to the controller, for example to /usr/local/bin;
`bootwright version` prints the same `Commit` there. Copy
`bin/bootwright.cdx.json` beside it: the executable's bill of materials,
which `bootwright sbom --format cyclonedx-json` prints again, names every
component it carries and every client and image it pins, for the host's
security tooling. `controller install`
prepares the foundation every context on the host shares and reads no desired
state; the dry run previews it, and `controller check` reports what it
proved. Each context's first `apply` then binds the context to this host
([binding](../specs/contexts.md#controller-relationship-and-host-binding)), and
its `controller` stage installs the clients its graph selects
([controller](../specs/controller.md#the-controller-stage)). Media images live
in one host-wide store that every context shares, so `controller install` runs before the
first `media add`.

### What setup admits and installs

`controller install` admits a host by family and floors, never by release name, and refuses
anything else with `controller.unsupported` and its remedy before it changes
anything: Fedora or RHEL, or a derivative whose `/etc/os-release` `ID_LIKE`
names one, such as CentOS Stream, Rocky or Alma; amd64; a root-owned `rpm`
and `dnf` under /usr/bin; a Python the build's lock serves, installed or
offered by the host's dnf; and a glibc at or above the lock's floor
([supported hosts](../specs/controller.md#supported-hosts)). The releases a
build was tested on are listed in
[development](development.md#lock-refresh-and-tested-hosts). A newer release is
admitted with the informational `controller.untested-release` line, such as
"Fedora 45 is newer than the releases this build was tested on (43, 44);
setup checks its floors and continues", which changes neither the result nor
the exit status. A non-empty /etc/ld.so.preload is reported the same way, as
`controller.loader-preload`.

`controller install` installs only what is missing, and from two places:

- **Host packages,** `openssh-clients`, plus the `python3.X`
  package when no Python the lock serves is installed, come from the
  host's own dnf with its own repositories, signing keys, `proxy=`, mirrors,
  Satellite or RHSM registration, versionlock and excludes
  ([host packages](../specs/controller.md#host-packages)). Bootwright edits no
  repository and never passes `--allowerasing` or `--nogpgcheck`, and a host
  that already holds every package makes no dnf call. Register a RHEL host, or
  enable the repositories that carry these packages, AppStream for a RHEL 9
  Python, before setup.
- **The controller runtime** is one Python virtual environment, beneath
  /var/lib/bootwright/controller/install/runtimes, on the host's own `/usr/bin/python3.X`:
  Python 3.12 to 3.14, newest first, or the interpreter setup chose before
  while it still qualifies. Its ansible-core and supporting wheels are the
  exact files the lock compiled into this build names, carried inside the
  executable itself, copied into a cache beside it, checked for size and
  SHA-256, and installed by pip with no index and no other source, so
  installing them downloads nothing
  ([the controller runtime](../specs/controller.md#the-controller-runtime)).
  Nothing is installed into the host's own Python.

Before it asks for confirmation, `controller install` runs `dnf install --assumeno` as root
for the missing packages and shows dnf's output verbatim in its plan, beside
how many wheels the runtime copies from the executable and how many its cache
already holds. The preview
changes no package, though dnf may refresh its metadata cache as
`dnf-makecache` does. When the transaction also adds or updates core packages, such as
glibc, the kernel, systemd, openssl-libs or the selected Python, the plan
carries one informational note and no refusal: dnf applies them as for any
package install. Declining the prompt changes nothing. dnf may resolve
differently between the preview and the install when its metadata refreshes;
the install's output and the versions setup records are the truth.

A ready host is reported in under a second with the receipt table, which
names the host, the runtime and where each package came from, and
`Nothing to do.`, with no network request, confirmation or write;
`controller check` prints the same table
([results](../specs/controller.md#results-and-qualification)). A new
Bootwright build with the same lock, and an operating-system upgrade that keeps
the interpreter, need no setup at all; a build whose lock changed needs one
setup, which copies from the executable only the wheels its cache lacks.

A setup that installs host packages keeps what dnf printed in a
[setup run](../specs/cli/output.md#setup-run-output),
/var/log/bootwright/controller/install-NNNNNN.output, which only root
can read. The host keeps the newest 8 outputs, and the `Logs` field of its
result names the run; a failure that run explains names its output in its
remedy. `bootwright logs --install` prints the newest, and `--follow` shows
a running install's as it grows. A setup that was interrupted blocks every command that runs
Ansible with `controller.state`, "a controller setup (`install-000042`) was
interrupted before it finished"; run `bootwright controller install` again, which
re-inspects the host, names the interrupted run in its plan and continues
after confirmation.

A context's controller stage installs the host packages its graph selects,
such as `lorax` and `xorriso` for installer media or the libvirt client, with
the host's own dnf in the same way, on RHEL as on Fedora: register a RHEL
controller, or enable the repositories that carry them, before that context's
first apply.

A host with no route to the Internet prepares the same way: `controller install`
reaches only the repositories the host's dnf is configured to read, a local
`file://` repository, a DVD or a Satellite mirror included, because the
executable carries every Python wheel the runtime needs.
`bootwright controller install --dry-run --output json` lists each carried
file's SHA-256.

### On a proxied network

`controller install`, `controller check` and `media add --from-url`
run before any Environment exists, so they take their route from the invoking
environment. Export `HTTPS_PROXY`, and `NO_PROXY` when internal hosts must be
reached directly, before running them; the
[context-free route](../specs/controller.md#the-context-free-acquisition-route)
says which values refuse. Everything a context drives uses that context's
declared proxy instead, and exporting a variable does not change it.

When the admitted route is a proxy, `controller install` and the controller stage also hand
it to the host's dnf as `https_proxy` and `no_proxy`; a `proxy=` in the
host's dnf configuration wins over it. Nothing else from the environment
reaches dnf, and a proxy that needs credentials stays unsupported
([the dnf route](../specs/controller.md#egress-and-local-effects)).

`sudo` clears the environment, so an unprivileged invocation forwards the
variables to its elevated child on the sudo command line; a sudoers rule that
grants neither `ALL` nor `SETENV` refuses that, and running as root avoids it.
`bootwright controller install --dry-run` never elevates, so the route it prints under
`Route` in its scope block shows only how the values parse.
`bootwright controller check` elevates and prints the same `Route` field,
which confirms that the variables crossed sudo.

#### Destinations to allow

Host packages come from the repositories the host's own dnf is configured
with, through dnf's `proxy=` or the forwarded route, so a proxy must also
admit those: their mirrors, metalink or Satellite, whatever `dnf repolist -v`
names. Bootwright names no repository host of its own. A proxy that admits
only listed destinations must also admit HTTPS, on port 443, to these hosts,
which a context's controller stage contacts, directly or through a redirect
the download follows; `controller install` contacts none of them, and
`media add --from-url` contacts the URL it is given. A proxy that inspects TLS re-signs every certificate, so its certificate
authority must be in the host's system trust store, which every acquisition
verifies against.

- `api.github.com`: release metadata of a declared helm, govc or virtctl
  release other than the build's default, and of the OKD clients and
  installer.
- `cdn.dl.k8s.io`: where `dl.k8s.io` redirects a kubectl download.
- `dl.k8s.io`: kubectl releases.
- `get.helm.sh`: helm releases.
- `github.com`: the govc, virtctl and OKD client and installer releases.
- `mirror.openshift.com`: OpenShift client and installer releases.
- `objects.githubusercontent.com`: where `github.com` redirects a release
  download.
- `release-assets.githubusercontent.com`: where `github.com` redirects a
  release download.

### On a controlled network

A controlled network lets its machines reach the enterprise's own package
repositories and the vendors of the platforms they run, and nothing else on
the Internet. A machine an attacker reaches can then neither fetch code from a
site anyone publishes on nor send data to one.
[Acquisition origins](../specs/security.md#acquisition-origins) classes what a
context acquires as host packages, vendor origins (Red Hat and IBM) and public
origins. `bootwright render acquisitions --input-dir <dir>`, or
`--context <name>` for a context's input, lists each artifact your desired
state acquires, its origin and class, its digest and the Machines that read
it ([acquisition list](../specs/cli.md#acquisition-list)), which gives a
firewall's owner each Machine's destinations. This is what each step reaches,
and what moves a public origin to a registry or file server the enterprise
runs:

| Step | What connects | Reaches by default | Keeping it off a public origin |
| --- | --- | --- | --- |
| Host packages | Each host's dnf | The repositories dnf is configured with | Nothing to do: a Satellite or a local repository serves, as [setup](#what-setup-admits-and-installs) says |
| OpenShift clients and installer | The controller | `mirror.openshift.com`, a vendor origin | Nothing to do |
| Helm, which every OpenShift cluster selects, `virtctl` and `govc` | The controller | `get.helm.sh` and GitHub | `spec.mirror.artifacts` on the Environment: see the tools below |
| The OKD clients and installer | The controller | GitHub | Not possible yet: their release metadata is read from GitHub |
| A managed service's image | The Machine the service runs on | The registry its compiled default names | `spec.mirror.registry` on the Environment, or `spec.image.local` on the service: see the images below |
| The emulated BMC's image | The libvirt provider's host | `quay.io` | `spec.mirror.registry` on the Environment |
| Installation media | The controller | The file or URL you name | `media add --from-file`, or `--from-url` from your own server |
| An OpenShift release | The controller while it builds the image, then every node | `quay.io`, where the release payload is published | `spec.mirror.registry` on the Environment, or `spec.install.registries` on the cluster |
| A cluster's operators | The cluster's nodes | The registries of the catalog each add-on binding names | Nothing to do for a Red Hat catalog or IBM's Data Foundation on `icr.io`, vendor origins; another's repositories are mapped by the same `imageDigestSources` where the cluster pulls them by digest |
| IBM Ceph images and packages | The controller, which resolves each image's digest, then each Ceph host | `cp.icr.io` and `public.dhe.ibm.com`, vendor origins | Nothing to do |

**Tools.** Each build pins a default release of Helm, `virtctl`, `govc` and
`kubectl` with its size and SHA-256 ([the tool table](../specs/controller.md#the-tool-table)).
`spec.mirror.artifacts.url` on the Environment names one HTTPS file server
for all of them ([artifact mirror](../specs/api/environment.md#artifact-mirror)):
each is read from `<url>/<kind>/v<release>/<publisher file name>` by its pinned
checksum, so the controller reaches no client publisher, GitHub included. Fill it
with the files the `client` rows of `bootwright render acquisitions` name,
from a machine that reaches the publishers. With the mirror declared, a
release in `spec.dependencyVersions` other than the build's default is
refused at plan, because its checksum only its publisher holds. The
OpenShift clients and installer stay on `mirror.openshift.com`, a vendor
origin, or on `spec.downloads.openshiftClientsMirror`.

**Images.** Each compiled default is pinned by digest: `docker.io` for the
name resolver, the proxy and the time server, `registry.access.redhat.com`
for the artifact server and `quay.io` for the emulated BMC. The exact
references are the `catalog.go` of each kind under
`internal/infrastructureservices/` and `internal/substrate/libvirt/catalog.go`.
`spec.mirror.registry.registryRef` on the Environment selects an external
Registry as the image mirror ([image mirror](../specs/api/environment.md#image-mirror)):
every one of those images from `docker.io` or `quay.io`, the emulated BMC's
included, is then pulled from `<registry url>/<origin registry>/<repository>`
at the same digest, with the Registry's `credentialsRef` and `trustBundleRef`
lent to that one pull. The artifact server's image stays on
`registry.access.redhat.com`, a vendor origin. Copy each image the
`service image` and `emulated BMC image` rows of `bootwright render
acquisitions` name with its digest kept, for example with `skopeo copy --all
--preserve-digests docker://quay.io/metal3-io/sushy-tools@sha256:<digest>
docker://<registry url>/quay.io/metal3-io/sushy-tools:<any tag>`; the pull
reads it by digest. A Machine whose `spec.proxy` names a proxy reaches the
mirror through it unless the mirror's host is in its `noProxy`.
`spec.image.local` on a service instead names one copy in your registry,
pinned by digest ([image pins](../specs/api/infrastructure-services.md#management-and-placement));
that pull binds no login or trust anchor of Bootwright's, so it succeeds where
the registry admits anonymous pulls and the placement host trusts its
certificate authority.

**An OpenShift release.** With `spec.mirror.registry` on the Environment,
its Registry serves every cluster's release: Bootwright maps the release
repositories to `<registry url>/quay.io/openshift-release-dev/...` with
`NeverContactSource` and adds the Registry's credential and trust to the
install ([installation features](../specs/openshift-clusters.md#installation-features)).
Fill it with `oc adm release mirror --from=quay.io/openshift-release-dev/ocp-release:<version>-x86_64
--to=<registry url>/quay.io/openshift-release-dev/ocp-v4.0-art-dev
--to-release-image=<registry url>/quay.io/openshift-release-dev/ocp-release:<version>-x86_64`,
whose two repositories are the two the install maps. The registry must accept
repository paths that deep, as Docker Distribution, Harbor and Quay do, and
the controller's own trust store must hold its certificate authority while
the image build reads from it. Otherwise,
`spec.install.registries.mirror` selects a declared
Registry, and `imageDigestSources` maps the release's repositories to it; a
`sourcePolicy` of `NeverContactSource` keeps a node from falling back to the
source ([registry policy](../specs/api/openshift-clusters.md#registry-policy)).
The Registry's credential joins the install pull secret, and its trust bundle
is one the cluster selects
([installation features](../specs/openshift-clusters.md#installation-features)).
You fill that registry, for example with the release's own
`oc adm release mirror`, and map every repository the release names: a node
fetches a component whose repository is not mapped from its source. The live
acceptance of this route is still owed
([B724](../specs/milestones/m3.md#b724)).

**Time and names.** A managed DNSServer forwards only to its declared
`forwarders`, and a managed NTPServer follows only its declared
`upstreamSources`. A Machine or cluster with no NTP selection keeps its
operating system's default time sources, a public pool on RHEL and RHCOS
([DNS and NTP selections](../specs/api/infrastructure-services.md#dns-and-ntp-selection-lists)).
A Machine Bootwright installs is installed with its selected NTPServers as
its time sources. A cluster's nodes take theirs as additional sources, so
their default pool stays configured unless the cluster's own configuration
replaces it.

**The firewall.** In production the network's own firewall or proxy holds
each machine to its package repositories, the hosts its vendor documents for
the product and your mirror. For OpenShift that vendor list names `quay.io`
and its content hosts for the release payload; an installation from a
mirrored release under `NeverContactSource` needs none of them. A managed
libvirt Network declared `forward: none` gives its guests no outbound route at
all.

A lab, or a site without a filtering firewall, can hold the Machines that
route through a managed Proxy to a list instead: `spec.allowedDestinations`
on that Proxy admits only the names, `.domain` suffixes and addresses it
lists, each at an optional port, and refuses every other request
([the grammar](../specs/api/infrastructure-services.md#proxy-dns-ntp-and-registry-deployment)).
An address entry admits a request for that address alone, never a name that
resolves to it.
The hosts of `spec.mirror.artifacts.url` and of the image mirror Registry are
added for you. A list for a vendor-only lab starts from the vendor origins
Bootwright itself reads:

```yaml
spec:
  management: managed
  machineRef: bastion
  implementation: squid
  allowedDestinations:
    - mirror.openshift.com
    - registry.redhat.io
    - registry.access.redhat.com
    - registry.connect.redhat.com
    - cp.icr.io
    - icr.io
    - public.dhe.ibm.com
```

A registry's token service and the content host it redirects a pull to are
other names, which its vendor's firewall documentation lists and Bootwright
does not: add each one by its exact host name, and so do the hosts a
Machine's subscription and package repositories use when it reaches them
through this proxy. Do not write a `.domain` suffix of a shared hosting or
content-delivery domain, such as `.amazonaws.com`, `.cloudfront.net` or an
Akamai domain: it admits every tenant on that domain, and so a host anyone
can publish on or send data to. Each refusal is a `TCP_DENIED/403` line in
the proxy's access log, which `sudo podman exec <unit> cat
/var/log/squid/access.log` reads on the Proxy's Machine, the unit being the
`bootwright-<context>-proxy-<name>` container; a run that fails to fetch
names its host there. The log is held in 64 MiB of memory, drops lines once
full and starts again when the container restarts.

The list holds a Machine only when the network gives it no other route. A
Machine with `direct: {}` or a destination in its `noProxy` goes around the
proxy, and so does a hostile one, which ignores its proxy settings: it
connects directly wherever the network forwards it, or carries data out in
name lookups that a resolver forwards, a managed DNSServer's `forwarders`
included. On a network with no filtering firewall, give the Machines no
route but the proxy, for example a managed libvirt Network declared
`forward: none` with the Proxy on a Machine attached to both it and a
routed network, and give their DNSServer no `forwarders`, or forward only to
a resolver that answers just the names you admit. A Machine that reaches
everything through the proxy needs no public name lookup, because the proxy
resolves each name it admits
([managed network services](../specs/infrastructure-services.md#managed-network-services)).

One declaration that routes every public origin to a mirror host Bootwright
builds and fills, tools pinned by the build and a software bill of materials
are designed in [controlled network supply](../specs/deferred/controlled-network.md)
and parked under [D250](../specs/milestones/backlog.md#d250).

### The sudo rule

Bootwright re-executes itself as root through sudo
([local privilege](../specs/cli.md#local-privilege-and-user-identity)). An
executable root owns, below directories root owns, such as one installed with
`sudo install -m 0755 bootwright /usr/local/bin/bootwright`, is re-executed by
that absolute path, so a rule naming it admits exactly that binary:

```
%bootwright-operators ALL=(root) /usr/local/bin/bootwright
```

The elevated child confirms it is that file, so install it to a path without
spaces. Every root invocation sudo starts, including one you run with sudo by
hand, accepts the terminal hint `COLORTERM` and discards it before any
subprocess. It requires no sudo or PAM configuration change for this hint.
Bootwright also discards the admitted PAM session variables `KRB5CCNAME`,
`DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR`, `XDG_SESSION_*`, `XDG_SEAT` and
`XDG_VTNR`. A failure to discard one refuses `runtime.privilege` before a
process starts.

Variables outside what sudo sets, the acquisition route, those session
variables and `COLORTERM` still refuse before effects, including `GODEBUG` and
`ANSIBLE_CONFIG`. A host whose PAM stack, `/etc/environment` or pam_env
configuration exports another variable refuses naming it; to see what a root
command receives, run `env -i PATH=/usr/bin:/bin sudo -u '#0' -- /usr/bin/env`.
Any other executable, such as your own build output, is re-executed through
`sudo -u '#0' -- /proc/<pid>/exe`, which
`ALL` matches, as `%wheel ALL=(ALL) ALL` grants, and so does a
`/proc/[0-9]*/exe` rule. Every running program has such a path, so that rule is
`ALL` in effect unless a `sha256:<digest>` before the command pins it to one
build's executable, the `Digest_Spec` of sudoers(5), which every new build must
update. A policy a directory
service holds, such as SSSD or LDAP sudo rules, is changed by its
administrator. A forwarded proxy route also needs `SETENV`
([on a proxied network](#on-a-proxied-network)). A JSON invocation, or one
whose standard input is not a terminal, cannot prompt for a password, so run
`sudo -v` in the same terminal first. Each refusal names which of these
applies. A `secret set` with `--value-stdin` or `--password-stdin` reads
nothing until Bootwright has read the sudo policy with `sudo -n -ll`, and
refuses with `secret.input` before it reads anything where the policy sets
`log_input`, `log_ttyin` or `log_stdin`, because sudo's I/O log would record
the value, whether you type it at the terminal or pipe it in.
Under a rule such as `%wheel ALL=(ALL) ALL` with no cached credential, sudo
lists nothing without a password: an interactive run then validates with
`sudo -v`, which prompts on the terminal, and reads the policy again; a JSON
or non-terminal run, or a policy still unreadable, refuses with
`secret.input` rather than reading the value. Run `sudo -v` in the same
terminal first, pass the value with `--value-file` or `--password-file`, or
run the command in a root login session sudo did not start: a console or SSH
root login, or `su -`. A command run through sudo is still subject to its I/O
log, so the `sudo -i`, `sudo -s` and `sudo su -` shells that suit
[directory accounts](#directory-accounts) do not avoid it
([secret custody](../specs/secrets.md)).

### Directory accounts

An SSSD, LDAP or AD account resolves through the name service, as
`getent passwd` answers for it, and runs Bootwright like a local account. A
`sudo -i` or `sudo -s` shell runs it as root, with root's own context
selection, so name the context with `--context`. When the name service cannot
answer, the refusal says why, and a local account or a clean root login
(`su -`, `sudo su -` or a root SSH session) works
([local privilege](../specs/cli.md#local-privilege-and-user-identity)).

### A network home with root squash

Bootwright reads the input directory, the Context file, a media source, the
secret files and a key you offer with `--ssh-id-file` with your own
credentials, so they may stay in a network home that squashes root. Root must
still execute Bootwright itself, so copy `bin/bootwright` to a local directory
such as `/usr/local/bin` and run it from there. Where root cannot, sudo refuses
with `unable to execute` and the refusal names that local copy as the remedy.

### A FIPS-mode controller

The controller runtime's Python uses the host's OpenSSL, so its TLS follows
the host's crypto policy, and the runtime carries no OpenSSL of its own;
Bootwright's own executable stays outside the host's FIPS-validated modules,
so running it on a FIPS-mode controller claims no FIPS compliance
([security](../specs/security.md#the-controller-hosts-attacker-model)).
`controller check`, like `controller install`, reports the kernel's FIPS mode as its
`FIPS mode` check with that statement; the check never changes readiness,
except that a FIPS flag that cannot be read as 0 or 1 refuses `controller install` and
`controller check` with `controller.unsupported`.

Under the FIPS crypto policy (`update-crypto-policies --show` prints `FIPS`),
OpenSSH accepts no Ed25519 key. Declare `remoteMachinesAccessKey` and every SSH
access `privateKeyRef` with an `sshKeyPair` Secret of `keyType` `rsa`
(generated at 3072 bits), `ecdsaP256`, `ecdsaP384` or `ecdsaP521`, never the
default `ed25519` ([generated source](../specs/api/secrets.md#generated-source)).
A delivered `hostKeyRef` follows the same rule: declare it `rsa` or
`ecdsaP256` and the installation installs it at its type's path and pins its
type.

The real-hardware test runs this build on a separate RHEL controller, in a subnet
the BMC network reaches on the artifact ports
([D109](../specs/milestones/backlog.md#d109)).

### Changing the controller host's identity

Changing the controller's machine-id, DMI product UUID or root filesystem UUID
strands every context bound to it: the next command refuses with the host
mismatch. Destroy each context from this host before such a change. When the
host keeps the same `/var/lib/bootwright` under its new identity, as a
recreated VM with the same disk does, rebind the record once from your own
account; it asks for authorization as other commands do:

```sh
bootwright controller rebind
```

It names the recorded and the new identity before it asks, keeps every
binding and reservation, and refuses a store whose directories are not the
ones its registry attributed. It cannot tell a byte-for-byte clone of the
store, from a disk image, a snapshot or a cloned VM, from the original, so
confirm it only when the recorded host no longer runs this store; with `--yes`
that affirmation is `--recorded-host-retired`. Values named with
`--machine-id`, `--product-uuid` and `--root-uuid` are visible in the process
list and the shell history. The
[host binding](../specs/contexts.md#controller-relationship-and-host-binding)
names the other exits.

### Uninstalling Bootwright from this host

Once every environment is destroyed, every context deleted and every image
removed with `bootwright media delete`, remove what `controller install` added,
from your own account; it asks for authorization as other commands do:

```sh
bootwright controller uninstall
```

It refuses, naming the command that clears each, while a context or an image
remains, and names each libvirt domain, network or pool, systemd unit,
quadlet source or podman container Bootwright still owns. Its plan shows
`dnf remove --assumeno` for the packages the install receipt names as
install's own, then removes them with dnf and removes every root Bootwright
keeps on this host, whole: `/var/lib/bootwright`,
`/var/lib/bootwright-staging`, `/var/log/bootwright`,
`/var/lib/bootwright-hosted` and an empty `/var/lib/libvirt/images/bootwright`.
Packages present before Bootwright, packages a controller stage installed,
the executable and what a context realized on an SSH placement host stay.
`--keep-packages` removes only the state; it is also the way out when the
install record was interrupted or written by another build. A store this
build cannot read, such as one an earlier build left, and guest disks no
removal took back are removed by `bootwright controller uninstall
--discard-unreadable`, which removes no package. An interrupted uninstall is
finished by the next one
([uninstall flow](../specs/controller.md#the-uninstall-flow)).

### Where Bootwright writes

Each root is created by the first command that needs it, and `controller
uninstall` removes each one on this host; an SSH placement host's content stays. `status --context <name> --paths` lists, for one context,
which of these roots it holds on this host.

| Root | Contents | Created by | Host roles |
| --- | --- | --- | --- |
| `/var/lib/bootwright` | The store: contexts, registry, secrets, media; `controller/install` (runtimes, wheel cache, receipt) and `controller/record` | `controller install` and `context init` | Control only |
| `/var/lib/bootwright-staging` | The embedded automation unpacked for each run | The first command that runs Ansible | Control only |
| `/var/log/bootwright` | `controller/install-NNNNNN.output`; per context under `contexts/<context>/`, `operations/<op>/`, `runs/run-NNNNNN.output` and `current` | The first command that writes a log | Control only |
| `/var/lib/bootwright-hosted/<context>/services/<kind>/<service>` | A hosted service's content root, configuration and certificate | `apply` of an artifact server or managed service | Hosting services |
| `/var/lib/bootwright-hosted/<context>/machines/<machine>/install`, `.../erase/<operation>` and `clusters/<cluster>` | An installation image built before it is served; a physical erase's custody; a cluster installer's work area | `apply` of the installation, the erase or the cluster | Serving installations |
| `/var/lib/bootwright-hosted/<context>/machines/<machine>/bmc` and `/var/lib/libvirt/images/bootwright/<context>` | The emulated BMC's state; guest disks and the virtual-media pool | `apply` of the libvirt provider and its Machines | Libvirt provider host |
| `/var/lib/bootwright-hosted/<context>/...` on the placement host | The same hosted content, on the host the graph names | The same `apply`, over SSH | SSH placement host: not read by `status`, and left in place by `controller uninstall` |

Logs are troubleshooting output only: no decision reads them, and
[`bootwright logs`](#following-an-apply-or-destroy) is what presents them. The hosted and
image roots hold what an `apply` realized; `destroy` removes each emptied
directory but never a root, and `controller uninstall` refuses while a context
remains ([the contexts spec](../specs/contexts.md#roots-beside-the-store)).

### Following an apply or destroy

Start the operation in one terminal and follow it from a second. The log root
is private to root, so `logs` asks for authorization as other commands do.
Every `Logs` line an `apply`, `destroy`, `status`, machine power command or
`controller install` prints is followed by a `Follow` line holding the exact
command that reads it: the `--follow` form while it runs, and afterwards one
pinned to that operation, run or setup:

- `bootwright logs --context <name> --follow` prints the context's current
  operation so far and the last 10 lines of each running block's output, then
  streams new events and output, block by block, and ends when the operation
  does. `--block <block>` narrows it to one block.
- `bootwright status --context <name>` shows, in its Live section, each running
  block's newest output file and the bytes written so far; `--paths` lists the
  roots that hold the context's data on this host.
- `bootwright logs --context <name> --block <block> --raw` prints one block's
  whole newest adapter output; `--attempt <n>` picks an earlier attempt.
  `--raw` cannot be followed: use `--block <block> --follow`.
- `bootwright logs --context <name> --run run-NNNNNN` shows a bounded
  run, such as a machine power command, and `--follow` follows one still
  running.
- `bootwright logs --install` shows the newest `controller install` output,
  `--install=install-NNNNNN` a named one (write the `=`), and `--follow` follows
  one still running.
- `--operation <id>` reads an earlier operation than the current one.

Text output escapes every line, so a tab prints as `\t` and a terminal sequence
never runs, and `--detail full` prints each event's full UTC timestamp. For the
exact bytes use `-o json`, which cannot be followed. A follow ends with a
warning when the operation still reads `running` but no `apply` or `destroy`
holds the context; the warning names the command that continues it, and ends
quietly once a newer operation becomes current. A follow
holds no lease, so it does not keep the operation it watches from finishing or
the next one from starting; a command that writes the store waits out its
short read instead of failing. In the instant a `--install --follow` probes whether
the install still runs, a `controller install` that starts can be refused as
busy; repeat it. The full contract is
[reading and following logs](../specs/cli/output.md#reading-and-following-logs).

## Network exposure

Bootwright binds the listeners the graph declares and leaves the host firewall
to the operator. Read the
[listener contract](../specs/security.md#listener-exposure) before choosing a
bind address: wildcard addresses expose every matching interface, DNS has no
client authentication, and the emulated BMC has root access to libvirt and
requires basic authentication over verified HTTPS. The emulated lab BMC stays on
`127.0.0.1:8000`; it needs no LAN firewall opening.

Open only the listeners a declared client needs. The lab defaults below are
examples; use the graph's actual ports and source subnets. A physical BMC
fetches from the controller's artifact server, so its network must route to
that address and certificate.

| Source | Destination on the service host | Use |
| --- | --- | --- |
| Selected Machine networks and controller | `3128/tcp` | Managed HTTP proxy, when selected |
| Selected Machine/installer networks and controller | `53/udp`, `53/tcp` | Managed DNS |
| Selected Machine/installer networks and controller | `123/udp` | Managed NTP |
| BMC network | `8443/tcp`, or the declared HTTPS artifact port | Installer virtual media |
| Installer network | `8443/tcp` | Private host-key and image delivery, when declared |
| Installer network | `8080/tcp`, or the declared public artifact port | Public package tree, when declared |
| Controller on the provider host | Loopback `8000/tcp` | Emulated BMC; no remote access |

With firewalld, inspect the interface holding the declared bind address, its
runtime zone and existing grants first. The controller's
[bounded inspection](../specs/controller.md#results-and-qualification)
requires an accepting zone or an explicit port/range grant for each socket.
It reports `unknown` for source-selected zones, rich rules, unresolved service
expansion and unqualified policies. Adding a source-scoped rich rule therefore
does not make that configuration pass preflight. Preserve required source
restrictions; select a suitable isolated controller interface for a lab whose
policy the inspector can qualify.

A port grant admits every client that reaches that zone, so its interfaces
and reachable networks must fit the client sets above. For temporary E2E
runs, use the [listener preparation and cleanup](../e2e/README.md#prepare-listener-firewall)
procedure, which records only previously absent ports. A managed bridge's
`trusted` membership does not prove admission on the existing interface that
owns the controller's bind address.

For a runtime port grant on an appropriate interface zone, inspect before
adding, substituting the actual zone and port:

```sh
sudo firewall-cmd --get-active-zones
sudo firewall-cmd --zone=<zone> --list-all
sudo firewall-cmd --zone=<zone> --query-port=8443/tcp
sudo firewall-cmd --zone=<zone> --add-port=8443/tcp
```

Repeat only for missing ports and protocols in the graph; an existing port
range can already include a socket. Run context preflight and require every
listener to be `open`. Verify access from each actual client network before
persisting a grant that should survive this run:

```sh
sudo firewall-cmd --permanent --zone=<zone> --add-port=8443/tcp
sudo firewall-cmd --permanent --zone=<zone> --list-ports
```

Runtime changes act immediately; permanent changes survive reboot and become
runtime rules after a reload. The
[firewall-cmd manual](https://firewalld.org/documentation/man-pages/firewall-cmd.html)
owns those semantics. Remove only grants this run added, using `--remove-port`
from the runtime configuration and, if persisted, the permanent configuration
when that exposure is no longer needed. Keep pre-existing grants intact.

A local readiness answer proves that listener on the host, not its route or
firewall from another network. Check the declared artifact endpoint from a
BMC-network host and the DNS, NTP, proxy and artifact endpoints from a Machine
network. An empty artifact root answers `404`; a TLS probe used only for that
connectivity check does not qualify certificate trust.

## Run a lab journey

Each lab makes the host the controller and the host of the managed services it
declares. Run one emulated lab per host at a time: lab-rhel and lab-sno bind the
same sockets and the same guest bridge.

| Journey | What it runs | Gate |
| --- | --- | --- |
| [lab-rhel](../e2e/lab-rhel/runbook.md#run) | One RHEL guest installed through an emulated Redfish BMC, machine list and power reads, exec/rsh sessions, drift observation, replay, restart and safe removal; extended acceptance checks are below | [B72](../specs/milestones/delivered.md#x38--the-real-host-run-of-2026-10-05) operator gate, accepted; M1's extended checks follow [D59](../specs/milestones/backlog.md#d59) |
| [lab-sno](../e2e/lab-sno/runbook.md#2-render-and-initialize) | A single-node OpenShift cluster installed by the agent installer on one libvirt guest | [B61](../specs/milestones/m3.md#b61) operator gate |
| [lab-baremetal](../examples/machines/lab-baremetal/README.md) | One physical Machine claimed, proved and installed through its own Redfish BMC, its installer image and host key published privately | [B73](../specs/milestones/m1.md#b73) real-hardware operator gate, not run |
| [lab-kubevirt](../examples/openshift-clusters/lab-kubevirt/runbook.md) | A five-node OpenShift cluster on VirtualMachines of an existing OpenShift Virtualization host, its image pushed through the host's upload proxy | [B731](../specs/milestones/m3.md#b731) operator gate, not run |

Run each block from the repository root, one line at a time, and keep the
sequence exactly as run: the ledger records it. After each operation, `status`
reports its final state; that state and every refusal met on the way are the
run's outcome.

An apply that installs a cluster or an operating system can run for most of an
hour, and it lives only as long as the command that started it. Closing its
terminal or losing the SSH session it runs in interrupts it as Ctrl-C does, and
a command killed outright takes its running lifecycle adapter with it
([process boundary](../specs/security.md#process-boundary)). Ctrl-C asks the
elevated command to stop and waits while it releases what it holds, up to a
minute inside a package transaction. A second Ctrl-C ends the command at
once, whether sudo gives it a terminal of its own, as under `use_pty`, its
default since sudo 1.9.14, or not. Sending
`SIGTERM` twice, from another terminal, to the `bootwright` process you
started kills it either way. A killed command can leave its operation for the
next command to resolve
([local privilege](../specs/cli.md#local-privilege-and-user-identity)). Run a
long apply where it outlives your connection: inside a `tmux` session, or as a
transient systemd unit whose output `journalctl` follows. A unit runs as root
rather than through `sudo`, so it reads root's context selection, not yours;
name the context:

```sh
sudo systemd-run --unit=bootwright-apply --collect "$PWD/bin/bootwright" apply --context lab-sno --yes
sudo journalctl -f -u bootwright-apply
```

`machine start`, `machine stop`, `machine restart` and `machine exec` take a
comma-separated `--name` and act on every listed Machine at once. A stop asks
one confirmation for the batch, and an exec prefixes each output line with its
Machine and warns on standard error for each session that exits nonzero; its own
status is the highest of them. An exec on several Machines has no standard
input and no password prompt, so a Machine that signs in with a password is
reached alone
([sessions](../specs/cli.md#machine-ssh-sessions)):

```sh
bootwright machine stop --context lab-rhel --name rhel-01,rhel-02,rhel-03 --yes
bootwright machine exec --context lab-rhel --name rhel-01,rhel-02,rhel-03 cat /etc/os-release
```

`cluster exec` does the same across clusters and nodes: `--name` and `--node`
take comma-separated lists, each listed cluster runs the command on its
administrator node (the one `cluster info` names), or on the `--node` members
it declares, and every resolved Machine runs at
once with its output lines prefixed by the Machine name. `cluster info --name`
takes a list too, and `cluster rsh` stays on one node
([sessions](../specs/cli.md#several-clusters-and-nodes)):

```sh
bootwright cluster exec --context lab-rhel --name ceph-a,ceph-b hostname
bootwright cluster exec --context lab-rhel --name ceph-lab --node rhel-01,rhel-02 uptime
```

A Ceph ingress virtual IP returns after its host reboots only once cephadm's
manager starts that host's keepalived again: cephadm leaves keepalived units
disabled at boot and starts them after its next check of a host it marked
offline, every `mgr/cephadm/host_check_interval` (600 s by default). Until
then `ceph -s` can read `HEALTH_OK` while the virtual IP answers nothing, and
`diff --observe` notes the block `not ready: keepalived@<ingress>`
([readiness](../specs/ceph.md#resources-readiness-and-access)). Run
`ceph cephadm check-host <host>` through `cluster exec` to make the manager
reconnect at once, or bound the wait with
`ceph config set mgr mgr/cephadm/host_check_interval <seconds>`, which costs
one SSH check of each host per interval.

SSH placement of a managed service on a second OS-ready host, with that host's
authored access and bound host key, is operator-run too and has no example.
No real-hardware row has been accepted yet: [B67](../specs/milestones/m3.md#b67)
and [B78](../specs/milestones/m1.md#b78) name one as exit evidence, and M2 and
M3 each need one besides their emulated rehearsal (decision D17). Today a physical
installation is proved in-tree only until [B73](../specs/milestones/m1.md#b73)'s
real-hardware operator gate runs, and a
physical cluster node refuses until [B67](../specs/milestones/m3.md#b67).

### KubeVirt providers

A KubeVirt provider realizes Machines as VirtualMachines on an OpenShift
Virtualization host ([KubeVirt machine realization](../specs/substrates.md#kubevirt-machine-realization)).
The [lab-kubevirt runbook](../examples/openshift-clusters/lab-kubevirt/runbook.md)
lists what a host must offer; in brief:

- A storage class whose StorageProfile offers `ReadWriteMany` in its first
  claim property set, and a node schedulable for virtual machines with KVM.
  The provider proof refuses a host without them before any Machine exists.
- An attachment the installer's VIPs can live on: a bridge
  NetworkAttachmentDefinition with `macspoofchk` off, or a `Localnet`
  ClusterUserDefinedNetwork without IPAM. The VIPs are not probed before
  boot on a KubeVirt attachment ([B927](../specs/milestones/backlog.md#b927)),
  so prove them free.
- Capacity for the guests plus the virt-launcher overhead the coverage row
  gives, and about two cores per host node for OpenShift Virtualization.
- The build's default `virtctl`, or a release declared in
  `spec.dependencyVersions`, and a route from the controller to the host's
  upload proxy without an ambient proxy.

Stop every hosted Machine before a destroy: a removal refuses while a
VirtualMachine runs. A power command or `machine list --power-status` works
while a destroy is current: each KubeVirt Machine the destroy has not yet
removed is reached through its port and held to the identity (the host's
`kube-system` UID and the VirtualMachine's UID) that the apply the destroy
takes back recorded ([B951](../specs/milestones/m1.md#b951)). A Machine whose
VirtualMachine the destroy already removed reads absent: `machine stop` reports
it `off` without contacting the host, and `machine start` and `machine restart`
refuse until you finish the destroy and apply again. If the record of that
apply cannot be read, the power command refuses rather than sending an
unpinned request.

A VirtualMachine deleted by hand is not repaired: every later command names
it missing, and only a destroy of the operation clears it. After you approve
an upgrade of an owned OpenShift Virtualization Subscription, observation reads
the instance's installed CSV as other than the expected one, which is unknown
with the remedy to destroy and apply again; approving it is outside
Bootwright's scope ([observation](../specs/add-ons.md#observation-and-continuation),
[B106](../specs/milestones/backlog.md#b106)). Before destroying a context that
builds the host cluster, save the host's kubeconfig with
`bootwright cluster kubeconfig`: the destroy drops it from custody while
bare-metal hosts keep running.

## Add-ons

A `ClusterAddonBinding` installs operators on a cluster after it is up. This
section routes the journey; each linked section owns its rules.

- Choose a package: `bootwright add-ons list` shows the built-in packages and
  `bootwright add-ons info --name <package>` describes one and prints a binding
  skeleton ([the package listing](../specs/add-ons.md#the-package-listing)).
- Start from a worked binding in the `add-ons` category of the
  [examples](../examples/README.md). Their channels and CSVs are illustrative:
  read the pair your catalog serves with the lookup `add-ons info` prints.
- Install an operator Bootwright does not ship: author a `ClusterAddon` of the
  [executable OLM shape](../specs/api/addons.md#executable-olm-add-ons), select
  it under `spec.addonRefs` of a binding, and start from the
  [lab-sno-authored](../examples/add-ons/lab-sno-authored/README.md) example.
  Its catalog image and CSV are code you choose, under the
  [trust boundary](../specs/add-ons.md#trust-boundary).
- Try one in the lab: the [lab-sno runbook](../e2e/lab-sno/runbook.md#inputs)
  turns the NMState and MetalLB bindings on with its `--set addons.*` flags.
- Share a channel and CSV across bindings with Environment kind defaults: a
  default fills only the packages a binding declares and never selects one
  ([the rule](../specs/api/environment.md#kind-defaults)).
- Run it: `validate`, `plan` and `apply` as for any journey. `apply --until
  <cluster>.<package>` pauses after one instance
  ([stopping after an add-on instance](../specs/state-reconciliation.md#stopping-after-an-add-on-instance)),
  and an instance is done when its [readiness](../specs/add-ons.md#readiness)
  holds.
- Run it again: what a settled apply and a changed binding do is the
  [lifecycle unit](../specs/state-reconciliation.md#lifecycle-unit)'s rule.
- Add one later: put a package or a binding in the input of a completed
  context and run `apply`; the [additive apply](../specs/state-reconciliation.md#additive-apply)
  states what it admits and what refuses, and `destroy` takes back both applies
  ([continuation and removal](../specs/state-reconciliation.md#continuation-and-removal)).
- Remove it: `destroy` retires the instance with its cluster
  ([retirement](../specs/add-ons.md#retirement)).

## Changing the build between runs

The automation is embedded in the executable and is no part of any setup
identity: every command that runs Ansible unpacks the automation of the build
in hand. A build that changes only `ansible/` or Go code therefore needs no
`controller install`, and neither does any build with the same runtime lock. A build whose
lock changed needs one `controller install`, which builds its runtime; until then `apply`
refuses with `controller.state`, "the controller runtime for this build (<identity>) is not
prepared", and the remedy `run bootwright controller install`
([the controller runtime](../specs/controller.md#the-controller-runtime)).

Before installing a new build, take stock of every context:
`bootwright context list` names them and, under "Unfinished operations", each
operation a context has not finished, with its verb, state and the build that
registered it. Finish each one under that build, or note it, because the new
build continues an operation only as the paragraph on another build below says.

An operation left incomplete by the previous build that the new one cannot
continue, as the paragraph on another build below says, is removed instead: a
fresh `destroy` supersedes any apply that did not complete and runs
under the build in hand, which is the ordinary loop when the repair is to the
role that failed. An apply interrupted with Ctrl-C takes the same road, and the
removal resolves each effect whose outcome the interrupt lost before it plans
anything, so no separate `apply` is needed first. A removal also proves every
Machine it would take back is down, so stop any running one with
`bootwright machine stop` before it; the refusal names each Machine and the
command that stops it, and nothing is registered until they are.

When that resolution still proves nothing, the removal refuses having
registered nothing, and the refusal and `status` name, for each such effect,
why: a foreign object at its target, a host or endpoint that did not answer,
or a listener with nothing of the target behind it, with what to do about it.
Do that and repeat `destroy`, which observes the effect again and goes on. The
only other way out is `bootwright context delete --name <context> --purge
--allow-orphans`, which abandons what the context may own and releases the
host reservations it held.

An operation is continued and removed with the Secret versions it bound, so a
context whose keyring lost that binding, or can no longer read its material,
can do neither: `apply` and `destroy` refuse before any effect, naming
the operation, the binding and every object it owns, and `status` offers only
the deletion. Restore the context's keyring from a complete backup and repeat
the command, or run `bootwright context delete --name <context> --purge
--allow-orphans` and remove the objects the refusal names by hand. Nothing
re-binds the current Secrets in the lost binding's place.

Another build continues an incomplete apply only where it freezes the same
blocks: planned again from the operation's frozen input, every step keeps its
request, implementation, content digest, dependencies and claims, and only the
words a step is presented in may differ. An incomplete destroy continues when
the removal derived again from its own frozen plan consumes the same
authorizations and every step's capability reads its frozen request. A fix that
changes only automation, or only Go code that moves no version or pin, therefore
continues the failed or unknown step with no `controller install` between, as long as its
runtime lock and Python ABI are the operation's; a build with another lock
refuses the continuation and names the build that registered it, whose
runtime this host keeps.
An unknown step is observed first: when that leaves it failed, the same `apply`
retries it and goes on with the steps after it. A build that would freeze a step
differently refuses the continuation and names that step; `bootwright destroy --context <context>`
under that build takes back what the operation owns, and the next `apply` plans
afresh. A build that changes a frozen request's version or the plan's shape can
do neither, so destroy with the build that applied it before switching.

A build that reads another local keyring format refuses every access to the
keyring, `secret encryption init` included, with `secret.store.implementation`,
naming the persisted format and the remedy.

## M1 closing validation

Run every repository gate from the M1 worktree:

```sh
make -k check docs-check race
git diff --check
```

`-k` runs independent gates even when another fails; the command still fails
if any gate fails. A known reachable vulnerability on the pinned Go toolchain
is not a pass. This command installs no controller packages and runs no lab
effects.

M1's closing operator gate, D59's extended lab-rhel journey, was accepted
under D154 and D156 ([X98](../specs/milestones/delivered.md#x98--m1-owner-acceptance)).
To repeat it, run the e2e [`lab-rhel`](../e2e/lab-rhel/runbook.md#run) case
through its [destroy](../e2e/lab-rhel/runbook.md#destroy) on a clean committed
build, then the extended checks below. The
[milestone status](../specs/milestones.md#status) names any retirement a
build requires, which a fix confined to automation never does. Use a
disposable host missing at least one host package, so that setup or the
controller stage installs it through the host's dnf after showing its preview,
and with no runtime yet, so that setup builds one; an already-prepared host
does not qualify that part of the gate. Record setup's timings, its plan and
the receipt table in the [setup row](acceptance.md#setup-fresh-install-row).
Keep a local run record outside Git.

X48 adds the three-node RHEL journey of the
[`lab-rhel-3-nodes` example](../examples/machines/lab-rhel-3-nodes/README.md)
to that gate. Prove actual overlapping service work, machine realization and
three same-profile installation waits, with exactly one package-tree
publication. Keep timestamped progress and operation logs locally; graph
waves and total runtime alone do not qualify the overlap. Repeat apply and
safe destroy, proving all guests stopped and no sibling scratch, shared tree
or managed units remain.

The extended checks are a running-disk size read, a destroy refused while the
guest runs, then stop and destroy, a fresh apply, a host restart and guest
start/stop/removal checks. The accepted three-node
host-restart rerun is recorded under [B608](../specs/milestones/delivered.md#b608).
These checks go beyond the ordinary example journey.

Besides the journey's time, machine-access, replay, restart and final-unit
checks, run these JSON status cases after the
first complete apply as an unprivileged account under password-required sudo,
with no matching `NOPASSWD` grant. A root or passwordless invocation does not
qualify the missing-password refusal. Each case emits exactly one JSON
document, no normal stderr output, and the indicated result:

```sh
sudo -v
./bin/bootwright status --context lab-rhel --output json
./bin/bootwright status --context lab-rhel --output json </dev/null
sudo -k
./bin/bootwright status --context lab-rhel --output json </dev/null
```

The first two exit `0`. The third exits `1` with `runtime.privilege` naming the
missing password. Under a temporary sudoers policy on the disposable host
that denies both the executable and its verified descriptor path, cache sudo
credentials and repeat the redirected command: it exits `1` with
`runtime.privilege` carrying the denial. Restore the policy before proceeding.

During an interactive apply on Fedora with sudo's `use_pty`, and separately on
RHEL 9.8, send one Ctrl-C: the command waits for bounded cleanup. Send a second:
it exits at once with status `130`, leaving a usable terminal. Record both
observations for [B339](../specs/milestones/delivered.md#x93--the-owner-run-of-2026-10-14); an unavailable platform
leaves its observation unrun.

Record command results, build commit, clean source stamp and operation-log
digests following [record a run](#record-a-run); the human owner accepts the
[ledger row](acceptance.md#row-format). The owner accepted these host gates,
B466's among them, under D154 and D156; a repeat run is a re-qualification, not
an open M1 requirement.

## Record a run

The ledger owns the [row format](acceptance.md#row-format) and who may accept a
row. To record a run:

1. Build a commit that descends from the acceptance baseline the item's
   section on its [milestone page](../specs/milestones.md#status) records,
   from a checkout with no local changes, so `version` reports a `clean`
   source. Any other build, and any run before the item records a baseline, is
   an observation.
2. Before the journey, note the `Commit` and `Source` lines `version` prints and
   the host's distribution, release and architecture.
3. Run the example's block as written. Note every local edit beyond its lab
   conventions and every `bootwright` command in order.
4. After each operation, note the final state `status` reports and every
   refusal met.
5. For each operation, compute the log digest over the directory its human
   result names as `Logs`, with the command the row format gives.
6. Add the row to the [ledger](acceptance.md#ledger) as a summary: date, full
   build commit, command sequence, outcome and the log digest of the private
   evidence archive. Its last cell is `observation, not acceptance` unless the
   human owner accepts it there for an item it matches; an agent records an
   acceptance only when the owner states it explicitly.
