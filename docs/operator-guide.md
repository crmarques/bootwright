# Operator Guide

This guide is for running Bootwright on a real host: preparing the host,
running a lab journey and recording what happened in the
[acceptance ledger](acceptance.md). Changing Bootwright, its checks and its test
harnesses is [development](development.md). Each example's README owns its own
journey, and [milestones](../specs/milestones.md#status) owns which run is a
gate and which build it must match.

## Prepare a host

`setup` prepares what every context on the host shares: the host packages
`openssh-clients` and `podman`, and the controller runtime that runs Ansible.
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
which SSH keys it can use, and
[a host an earlier build manages](#a-host-an-earlier-bootwright-build-manages)
how a host an earlier build prepared becomes a controller for this one.

Prepare the host once, from the repository root:

```sh
make build
./bin/bootwright version
./bin/bootwright setup --dry-run
./bin/bootwright setup
./bin/bootwright preflight controller
```

`make build` stamps `bin/bootwright` with the commit and source state of the
checkout, which `version` prints as its `Commit` and `Source` lines. A
controller needs no checkout or Go toolchain: run `make build` on a
Linux/amd64 workstation and copy `bin/bootwright`, a static executable that
embeds its automation, to the controller, for example to /usr/local/bin;
`bootwright version` prints the same `Commit` there. `setup`
prepares the foundation every context on the host shares and reads no desired
state; the dry run previews it, and `preflight controller` reports what it
proved. Each context's first `apply` then binds the context to this host
([binding](../specs/contexts.md#controller-relationship-and-host-binding)), and
its `controller` stage installs the clients its graph selects
([controller](../specs/controller.md#the-controller-stage)). Media images live
in one host-wide store that every context shares, so `setup` runs before the
first `media add`.

### What setup admits and installs

Setup admits a host by family and floors, never by release name, and refuses
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

Setup installs only what is missing, and from two places:

- **Host packages,** `openssh-clients` and `podman`, plus a `python(abi)`
  capability when no Python the lock serves is installed, come from the
  host's own dnf with its own repositories, signing keys, `proxy=`, mirrors,
  Satellite or RHSM registration, versionlock and excludes
  ([host packages](../specs/controller.md#host-packages)). Bootwright edits no
  repository and never passes `--allowerasing` or `--nogpgcheck`, and a host
  that already holds every package makes no dnf call. Register a RHEL host, or
  enable the repositories that carry these packages, AppStream for a RHEL 9
  Python, before setup.
- **The controller runtime** is one Python virtual environment, beneath
  /var/lib/bootwright/setup/runtimes, on the host's own `/usr/bin/python3.X`:
  Python 3.12 to 3.14, newest first, or the interpreter setup chose before
  while it still qualifies. Its ansible-core and supporting wheels are the
  exact files the lock compiled into this build names, fetched from
  `files.pythonhosted.org` into a cache beside it, checked for size and
  SHA-256, and installed by pip with no index and no other source
  ([the controller runtime](../specs/controller.md#the-controller-runtime)).
  Nothing is installed into the host's own Python.

Before it asks for confirmation, setup runs `dnf install --assumeno` as root
for the missing packages and shows dnf's output verbatim in its plan, beside
how many wheels the runtime downloads and how many it already has. The preview
changes no package, though dnf may refresh its metadata cache as
`dnf-makecache` does. When the transaction also updates core packages, such as
glibc, the kernel, systemd, openssl-libs or the selected Python, the plan
carries one informational note and no refusal: dnf applies them as for any
package install. Declining the prompt changes nothing. dnf may resolve
differently between the preview and the install when its metadata refreshes;
the install's output and the versions setup records are the truth.

A ready host is reported in under a second with the receipt table, which
names the host, the runtime and where each package came from, and
`Nothing to do.`, with no network request, confirmation or write;
`preflight controller` prints the same table
([results](../specs/controller.md#results-and-qualification)). A new
Bootwright build with the same lock, and an operating-system upgrade that keeps
the interpreter, need no setup at all; a build whose lock changed needs one
setup, which downloads only the wheels it has not cached.

A setup that installs host packages keeps what dnf printed in a
[setup run](../specs/cli/output.md#setup-run-output),
/var/lib/bootwright/setup/runs/setup-NNNNNN/run.output, which only root
can read. The host keeps the newest 8 runs, and the `Logs` field of setup's
result names the run; a failure that run explains names its `run.output` in
its remedy. A setup that was interrupted blocks every command that runs
Ansible with `controller.state`, "a controller setup (`setup-000042`) was
interrupted before it finished"; run `bootwright setup` again, which
re-inspects the host, names the interrupted run in its plan and continues
after confirmation.

A context's controller stage installs the host packages its graph selects,
such as `lorax` and `xorriso` for installer media or the libvirt client, with
the host's own dnf in the same way, on RHEL as on Fedora: register a RHEL
controller, or enable the repositories that carry them, before that context's
first apply.

Preparing a host with no route to `files.pythonhosted.org` is not available
yet: `setup --wheel-mirror` and an unprivileged `setup --download-only
--dest-dir` that copies the locked wheels to such a host are to be delivered
in X90 ([B549](../specs/milestones/m1.md#b549)). Host packages already come
from wherever the host's dnf is configured to read them, a local `file://`
repository, a DVD or a Satellite mirror included.

### On a proxied network

`setup`, `preflight controller` without `--context` and `media add --from-url`
run before any Environment exists, so they take their route from the invoking
environment. Export `HTTPS_PROXY`, and `NO_PROXY` when internal hosts must be
reached directly, before running them; the
[context-free route](../specs/controller.md#the-context-free-acquisition-route)
says which values refuse. Everything a context drives uses that context's
declared proxy instead, and exporting a variable does not change it.

When the admitted route is a proxy, setup and the controller stage also hand
it to the host's dnf as `https_proxy` and `no_proxy`; a `proxy=` in the
host's dnf configuration wins over it. Nothing else from the environment
reaches dnf, and a proxy that needs credentials stays unsupported
([the dnf route](../specs/controller.md#egress-and-local-effects)).

`sudo` clears the environment, so an unprivileged invocation forwards the
variables to its elevated child on the sudo command line; a sudoers rule that
grants neither `ALL` nor `SETENV` refuses that, and running as root avoids it.
`bootwright setup --dry-run` never elevates, so the route it prints under
`Route` in its scope block shows only how the values parse.
`bootwright preflight controller` elevates and prints the same `Route` field,
which confirms that the variables crossed sudo.

#### Destinations to allow

Host packages come from the repositories the host's own dnf is configured
with, through dnf's `proxy=` or the forwarded route, so a proxy must also
admit those: their mirrors, metalink or Satellite, whatever `dnf repolist -v`
names. Bootwright names no repository host of its own. A proxy that admits
only listed destinations must also admit HTTPS, on port 443, to these hosts,
which setup and the controller stage contact, directly or through a redirect
the download follows; `media add --from-url` contacts the URL it is given. A
proxy that inspects TLS re-signs every certificate, so its certificate
authority must be in the host's system trust store, which every acquisition
verifies against.

- `api.github.com`: release metadata of helm, govc, virtctl and the OKD
  clients.
- `cdn.dl.k8s.io`: where `dl.k8s.io` redirects a kubectl download.
- `dl.k8s.io`: kubectl releases and their stable version.
- `files.pythonhosted.org`: the locked `ansible-core` wheel, its supporting
  wheels and the pip wheel that installs them.
- `get.helm.sh`: helm releases.
- `github.com`: the govc, virtctl and OKD client releases.
- `mirror.openshift.com`: OpenShift client and installer releases.
- `objects.githubusercontent.com`: where `github.com` redirects a release
  download.
- `release-assets.githubusercontent.com`: where `github.com` redirects a
  release download.

### The sudo rule

Bootwright re-executes itself as root through
`sudo -u '#0' -- /proc/<pid>/exe`, so the sudoers rule that admits the account
must match that path
([local privilege](../specs/cli.md#local-privilege-and-user-identity)). `ALL`
matches it, as `%wheel ALL=(ALL) ALL` grants, and so does a `/proc/[0-9]*/exe`
rule. Every running program has such a path, so that rule is `ALL` in effect
unless a `sha256:<digest>` before the command pins it to one build's
executable, the `Digest_Spec` of sudoers(5), which every new build must update.
A rule naming the Bootwright binary does not match the re-execution: sudo
compares a command's base name, `exe`, before its path. A policy a directory
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
the host's crypto policy; Bootwright's own executable and the `cryptography`
wheel's bundled OpenSSL stay outside the host's FIPS-validated modules, so
running it on a FIPS-mode controller claims no FIPS compliance
([security](../specs/security.md#the-controller-hosts-attacker-model)).
`preflight controller`, like `setup`, reports the kernel's FIPS mode as its
`FIPS mode` check with that statement; the check never changes readiness,
except that a FIPS flag that cannot be read as 0 or 1 refuses setup and
preflight with `controller.unsupported`.

Under the FIPS crypto policy (`update-crypto-policies --show` prints `FIPS`),
OpenSSH accepts no Ed25519 key. Declare `remoteMachinesAccessKey` and every SSH
access `privateKeyRef` with an `sshKeyPair` Secret of `keyType` `rsa`
(generated at 3072 bits), `ecdsa-p256`, `ecdsa-p384` or `ecdsa-p521`, never the
default `ed25519` ([generated source](../specs/api/secrets.md#generated-source)).
A delivered `hostKeyRef` follows the same rule: declare it `rsa` or
`ecdsa-p256` and the installation installs it at its type's path and pins its
type.

### A host an earlier Bootwright build manages

A host whose `/var/lib/bootwright` holds a store an earlier Bootwright build
created is not a supported controller for this build until that build's
environments are retired and its store removed. Store commands refuse there
([contexts](../specs/contexts.md#storage-locking-and-publication)). Every build
from before the setup v2 cut-over (X88) wrote registry version 5, which this
build neither reads nor converts, so its first command, `setup` included,
refuses before any plan with `context.state`, "/var/lib/bootwright holds a
store an earlier Bootwright build wrote (registry version 5), which this
build does not read or convert". The way across is a fresh install, in this
order:

1. With the build that wrote the store, the one that applied or prepared each
   context, destroy every environment and delete every context. Keep that
   build until this is done: this one cannot read its records.

   ```sh
   bootwright context list
   bootwright destroy --context <name>
   bootwright context delete --name <name>
   ```

2. Once nothing it manages runs any more, as root, remove both store roots:

   ```sh
   sudo rm -r /var/lib/bootwright /var/lib/bootwright-staging
   ```

3. With this build, prepare the host as [above](#prepare-a-host), from
   `setup --dry-run` on.

Never remove or move that store aside while the services it manages run: a
context deleted without its destroy leaves its guests, units and listeners
behind with nothing that owns them. The
real-hardware test runs this build on a separate RHEL controller, in a subnet
the BMC network reaches on the artifact ports
([D109](../specs/milestones/backlog.md#decisions)).

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

With firewalld, inspect the active zone and its existing grants first. Adding a
source-scoped rule does not narrow an existing broad port/service grant or a
trusted zone. Use a zone whose existing grants fit the intended client set.
For one IPv4 source and listener, substitute the zone, source CIDR and port:

```sh
sudo firewall-cmd --get-active-zones
sudo firewall-cmd --zone=<zone> --list-all
sudo firewall-cmd --zone=<zone> --add-rich-rule='rule family="ipv4" source address="<source-cidr>" port port="8443" protocol="tcp" accept'
```

Repeat per permitted source, port and protocol, using `family="ipv6"` for an
IPv6 source. Verify access from each actual client network before persisting
the same rule:

```sh
sudo firewall-cmd --permanent --zone=<zone> --add-rich-rule='rule family="ipv4" source address="<source-cidr>" port port="8443" protocol="tcp" accept'
sudo firewall-cmd --permanent --zone=<zone> --list-rich-rules
```

Runtime changes act immediately; permanent changes survive reboot and become
runtime rules after a reload. The
[firewall-cmd manual](https://firewalld.org/documentation/man-pages/firewall-cmd.html)
owns those semantics. Remove the same rule with `--remove-rich-rule` from both
configurations when its exposure is no longer needed.

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
| [lab-rhel](../examples/lab-rhel/README.md#run-it) | One RHEL guest installed through an emulated Redfish BMC, machine list and power reads, a session through exec and rsh, a restart, a settled replay, a removal refused while the guest runs, a fresh apply and a host restart | [B72](../specs/milestones/delivered.md#x38--the-real-host-run-of-2026-10-05) operator gate, accepted; next, M1's closing run ([D59](../specs/milestones/backlog.md#decisions)) |
| [lab-sno](../examples/lab-sno/README.md#run-it) | A single-node OpenShift cluster installed by the agent installer on one libvirt guest | [B61](../specs/milestones/m3.md#b61) operator gate |
| [lab-baremetal](../examples/lab-baremetal/README.md#run-it) | One physical Machine claimed, proved and installed through its own Redfish controller, its installer image and host key published privately; the HTTPS emulated rehearsal still needs a CA trust chain accepted by the physical BMC contract, [B446](../specs/milestones/m2.md#b446) | [B73](../specs/milestones/m2.md#b73) operator gate, not run |

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
take comma-separated lists, each listed cluster runs the command on its first
node, or on the `--node` members it declares, and every resolved Machine runs at
once with its output lines prefixed by the Machine name. `cluster info --name`
takes a list too, and `cluster rsh` stays on one node
([sessions](../specs/cli.md#several-clusters-and-nodes)):

```sh
bootwright cluster exec --context lab-rhel --name ceph-a,ceph-b hostname
bootwright cluster exec --context lab-rhel --name ceph-lab --node rhel-01,rhel-02 uptime
```

SSH placement of a managed service on a second OS-ready host, with that host's
authored access and bound host key, is operator-run too and has no example.
No real-hardware row has been accepted yet: [B67](../specs/milestones/m3.md#b67)
and [B78](../specs/milestones/m2.md#b78) name one as exit evidence, and M2 and
M3 each need one besides their emulated rehearsal (decision D17). Today a physical
installation is proved in-tree only until [B73](../specs/milestones/m2.md#b73)'s
rehearsal runs, which waits on [B446](../specs/milestones/m2.md#b446), and a
physical cluster node refuses until [B67](../specs/milestones/m3.md#b67).

## Changing the build between runs

The automation is embedded in the executable and is no part of any setup
identity: every command that runs Ansible unpacks the automation of the build
in hand. A build that changes only `ansible/` or Go code therefore needs no
`setup`, and neither does any build with the same runtime lock. A build whose
lock changed needs one `setup`, which builds its runtime; until then `apply`
refuses with `controller.state`, "the controller runtime for this build (<identity>) is not
prepared", and the remedy `run bootwright setup`
([the controller runtime](../specs/controller.md#the-controller-runtime)).
A build from before the setup v2 cut-over cannot read this build's store, and
this build cannot read its store: crossing it is the
[fresh install](#a-host-an-earlier-bootwright-build-manages).

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
continues the failed or unknown step with no `setup` between, as long as its
runtime lock and Python ABI are the operation's; a build with another lock
refuses the continuation and names the build that registered it, whose
runtime this host keeps.
An unknown step is observed first: when that leaves it failed, the same `apply`
retries it and goes on with the steps after it. A build that would freeze a step
differently refuses the continuation and names that step; `bootwright destroy --context <context>`
under that build takes back what the operation owns, and the next `apply` plans
afresh. A build that changes a frozen request's version or the plan's shape can
do neither, so destroy with the build that applied it before switching. Do not
switch back either: a build from before X74 cannot read the attempt and
resolution records a later build wrote, which name the build that ran them, so
once such a build has worked an operation, the removal under the build in hand
is the way out.

A build that changes the local keyring's format cannot open a keyring the
earlier format wrote: every access to it, `secret encryption init` included,
refuses with `secret.store.implementation`, naming the persisted format and
the way out. A context created before X21 keeps a `local-keyring-v3` keyring,
so destroy it with the build that applied it, before running `setup` with
this one, and then delete it with `bootwright context delete --name <context>
--purge` and create it again. An apply or destroy that binds no Secret meets
the refusal only when it reaches the keyring, which can be after its effects.

A build before X20 accepted an installer media name of 251 to 255 bytes, whose
record name, the image name followed by `.json`, is longer than a file name
may be. Its `media add` moved the image into place and then could not write
the record. Every media command of a later build refuses such a name, so
`media list` never shows the image and `media delete` cannot remove it. List
any such image in the host's media directory:

```sh
sudo find /var/lib/bootwright/media -maxdepth 1 -type f -name '*.iso' -regextype posix-extended -regex '.*/[^/]{251,255}' -printf '%f\n'
```

Then remove each one it lists by hand, holding the store's lock, which
refuses while another Bootwright command holds the store:

```sh
sudo flock --nonblock /var/lib/bootwright rm -- /var/lib/bootwright/media/<name>
```

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

M1 also needs D59's extended [lab-rhel journey](../examples/lab-rhel/README.md#run-it)
on a clean committed build. A build at or after the setup v2 cut-over (X88)
first needs the [fresh install](#a-host-an-earlier-bootwright-build-manages) on
a host an earlier build prepared; the
[milestone status](../specs/milestones.md#status) names any other retirement a
build requires, which a fix confined to automation never does. Use a
disposable host missing at least one host package, so that setup or the
controller stage installs it through the host's dnf after showing its preview,
and with no runtime yet, so that setup builds one; an already-prepared host
does not qualify that part of the gate. Record setup's timings, its plan and
the receipt table in the [setup row](acceptance.md#setup-v2-fresh-install-row).
Keep a local run record outside Git.

X48 adds the [three-node RHEL journey](../examples/lab-rhel-3-nodes/README.md)
to that gate. Prove actual overlapping service work, machine realization and
three same-profile installation waits, with exactly one package-tree
publication. Keep timestamped progress and operation logs locally; graph
waves and total runtime alone do not qualify the overlap. Repeat apply and
safe destroy, proving all guests stopped and no sibling scratch, shared tree
or managed units remain.

Besides the runbook's time, running-disk, machine-access, replay, restart,
removal-refusal and final-unit checks, run these JSON status cases after the
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
observations for [B339](../specs/milestones/m1.md#b339); an unavailable platform
leaves its observation unrun.

Record command results, build commit, clean source stamp and operation-log
digests following [record a run](#record-a-run); the human owner accepts the
[ledger row](acceptance.md#row-format). These host gates, B466's among them,
remain requirements before the owner can declare M1 done.

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
6. Add the row to the [ledger](acceptance.md#ledger). Its last cell is
   `observation, not acceptance` unless the human owner accepts it there for a
   item it matches; an agent records only observations.
