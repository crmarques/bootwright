# Operator Guide

This guide is for running Bootwright on a real host: preparing the host,
running a lab journey and recording what happened in the
[acceptance ledger](acceptance.md). Changing Bootwright, its checks and its test
harnesses is [development](development.md). Each example's README owns its own
journey, and [milestones](../specs/milestones.md#status) owns which run is a
gate and which build it must match.

## Prepare a host

`setup` supports RHEL 9 and Fedora on Linux/amd64 at the exact releases it
[admits](development.md#qualified-hosts-and-images), Fedora 43 and RHEL 9.8,
and refuses any other. Fedora 43 has run; RHEL 9.8 is admitted but not yet run
until the [acceptance ledger](acceptance.md#ledger) records it. A controller
must hold exactly the glibc and libgcc builds
[development](development.md#qualified-hosts-and-images) names, as
[hold the execution foundation](#hold-the-execution-foundation) keeps them. Setup,
preflight and the controller stage stage their dependency resolution and
package inspection beneath /var/lib/bootwright-staging, which the first of them
creates, and run what they stage from there, so that directory must be on a
filesystem mounted with exec; /tmp may stay noexec. Each example's host
prerequisites add what its journey needs, such as `/dev/kvm` for a libvirt
guest. Run as root, or from an account that may run
`sudo`: Bootwright asks for authorization when a command needs it, and keeps its
state under `/var/lib/bootwright`
([contexts](../specs/contexts.md#storage-locking-and-publication)).
[The sudo rule](#the-sudo-rule) says what that account's policy must permit,
[directory accounts](#directory-accounts) how an SSSD, LDAP or AD account runs
it, [a network home with root squash](#a-network-home-with-root-squash) where
the executable must live,
[hold the execution foundation](#hold-the-execution-foundation) how to keep
the glibc and libgcc builds it runs on,
[a FIPS-mode controller](#a-fips-mode-controller) what its runtime claims and
which SSH keys it can use, and
[a host an earlier build manages](#a-host-an-earlier-bootwright-build-manages)
when a host is not yet a controller for this build.

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

A setup that runs its controller Ansible keeps what that Ansible printed in a
[setup run](../specs/cli/output.md#setup-run-output),
/var/lib/bootwright/controller/runs/setup-NNNNNN/run.output, which only root
can read. The host keeps the newest 8 runs, and the `Logs` field of setup's
result names the run; a failure that run explains names its `run.output` in
its remedy.

A RHEL controller that publishes installer media, because the artifact server
an Anaconda installation uses is placed on it, builds that media with `lorax`
and `xorriso`, which no public source Bootwright resolves from carries for
RHEL. Install both from the host's own enabled Red Hat repositories, as root,
before that context's first apply ([D106](../specs/milestones/backlog.md#decisions)):

```sh
dnf install lorax xorriso
```

Its controller stage accepts them by presence and the qualified Red Hat release
key, and otherwise refuses before it acquires anything, naming the package that
is missing or signed by another key; `preflight controller --context <name>`
reports the same check and names this step first. A package another key signed
is already installed, so the step for it removes it first, as
`dnf remove xorriso && dnf install lorax xorriso`. A Fedora controller's stage
installs them itself.

### On a proxied network

`setup`, `preflight controller` without `--context` and `media add --from-url`
run before any Environment exists, so they take their route from the invoking
environment. Export `HTTPS_PROXY`, and `NO_PROXY` when internal hosts must be
reached directly, before running them; the
[context-free route](../specs/controller.md#the-context-free-acquisition-route)
says which values refuse. Everything a context drives uses that context's
declared proxy instead, and exporting a variable does not change it.

`sudo` clears the environment, so an unprivileged invocation forwards the
variables to its elevated child on the sudo command line; a sudoers rule that
grants neither `ALL` nor `SETENV` refuses that, and running as root avoids it.
`bootwright setup --dry-run` never elevates, so the route it prints under
`Route` in its scope block shows only how the values parse.
`bootwright preflight controller` elevates and prints the same `Route` field,
which confirms that the variables crossed sudo.

#### Destinations to allow

A proxy that admits only listed destinations must admit HTTPS, on port 443, to
these hosts, which setup and the controller stage contact, directly or through
a redirect the download follows; `media add --from-url` contacts the URL it is
given. A proxy that inspects TLS re-signs every certificate, so its
certificate authority must be in the host's system trust store, which every
acquisition verifies against.

- `api.github.com`: release metadata of helm, govc, virtctl and the OKD
  clients.
- `cdn-ubi.redhat.com`: the UBI 9 BaseOS and AppStream repositories a RHEL 9.8
  controller's native packages resolve from.
- `cdn.dl.k8s.io`: where `dl.k8s.io` redirects a kubectl download.
- `dl.fedoraproject.org`: the Fedora 43 release and updates repositories a
  Fedora controller's native packages resolve from.
- `dl.k8s.io`: kubectl releases and their stable version.
- `files.pythonhosted.org`: the `ansible-core` wheel and its supporting wheels.
- `get.helm.sh`: helm releases.
- `github.com`: the python-build-standalone CPython release and the govc,
  virtctl and OKD client releases.
- `mirror.openshift.com`: OpenShift client and installer releases.
- `objects.githubusercontent.com`: where `github.com` redirects a release
  download.
- `pypi.org`: the `ansible-core` project page and wheel resolution.
- `raw.githubusercontent.com`: the CPython release metadata setup selects from.
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
applies. Where the policy sets `log_input` or `log_stdin`, a `secret set`
with `--value-stdin` or `--password-stdin` refuses with `secret.input` before
it reads anything, because sudo's I/O log would record the value. The check
reads the policy with `sudo -n -ll`, so it holds only when sudo lists the
policy without a password, through a cached credential (run `sudo -v` first)
or a rule that needs none; under a rule such as `%wheel ALL=(ALL) ALL` with no
cached credential it proves nothing, and sudo logs what you type. Pass the
value with `--value-file` or `--password-file`, or run the command as root
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

### Hold the execution foundation

Bootwright's private interpreter runs on the host's own loader, glibc and
libgcc, and each build pins their exact files, so it runs on no other build
of them. `setup` and `preflight controller` report the builds this build
requires as the `Execution foundation` check, for example
`glibc 2.42-16.fc43, libgcc 15.3.1-1.fc43` on Fedora 43. Hold both packages at
those builds on a controller; on RHEL 9 the `versionlock` command comes from
the `python3-dnf-plugin-versionlock` package, and the minor release is pinned
too, so updates come from the release Bootwright admits:

```sh
dnf versionlock add glibc libgcc
subscription-manager release --set=9.8
```

A z-stream errata of either package within the release Bootwright admits needs
one `setup`: release the hold, update, run `bootwright setup`, which qualifies
the vendor-signed builds from the RPM database and records them on its receipt
with no other host or bundle effect, then hold the new builds. Until that setup
runs, `preflight controller` reports the check not ready with next command
`bootwright setup`, and an `apply` or `destroy` refuses before its private
interpreter runs:

```sh
dnf versionlock delete glibc libgcc
dnf update glibc libgcc
bootwright setup
dnf versionlock add glibc libgcc
```

The hold still keeps a controller off another upstream version or minor
release. Before a host update that would move either package that way, destroy
the contexts this host runs, or let their operations complete, because every
`apply` and `destroy` runs the private interpreter, and set the host up
afterwards with a Bootwright build whose execution foundation pins the new
builds
([controller](../specs/controller.md#supported-host-and-dependency-selection)).

When a pinned file differs, `setup` and `preflight controller` refuse with
`controller.unsupported` before any plan, and an `apply` or `destroy` refuses
the same way before its private interpreter runs, naming the file, the
package build that provides it and what was found, for
example `the provided execution foundation differs at /usr/lib64/libc.so.6,
from glibc 2.42-16.fc43, which holds other content than this build pins`.
When `dnf` updated the package within the release, run `bootwright setup`
first, as above; setup refuses a build another key signed, another upstream
version, a second x86_64 instance or a file that differs from its RPM digest,
naming the build. Otherwise restore that build and hold it, then repeat the
command:

```sh
dnf install glibc-2.42-16.fc43
dnf versionlock add glibc-2.42-16.fc43
```

`dnf reinstall` restores a file of a build that is still installed. A
non-empty `/etc/ld.so.preload` refuses the same way; empty or remove it.

### A FIPS-mode controller

Bootwright's runtime brings its own cryptography: its executable and its
private CPython use their own cryptographic implementations, outside the
host's FIPS-validated modules, so running it on a FIPS-mode controller claims
no FIPS compliance ([D108](../specs/milestones/backlog.md#decisions)).
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
([contexts](../specs/contexts.md#storage-locking-and-publication)). Never move
that store aside while the services it manages run. The real-hardware test
runs this build on a separate RHEL 9.8 controller, in a subnet the BMC network
reaches on the artifact ports
([D109](../specs/milestones/backlog.md#decisions)); RHEL 9.8 is admitted but
not yet run, as [development](development.md#qualified-hosts-and-images)
records.

## Run a lab journey

Each lab makes the host the controller and the host of the managed services it
declares. Run one emulated lab per host at a time: lab-rhel and lab-sno bind the
same sockets and the same guest bridge.

| Journey | What it runs | Gate |
| --- | --- | --- |
| [lab-rhel](../examples/lab-rhel/README.md#run-it) | One RHEL guest installed through an emulated Redfish BMC, machine list and power reads, a session through exec and rsh, a restart, a settled replay, a removal refused while the guest runs, a fresh apply and a host restart | [B72](../specs/milestones/delivered.md#x38--the-real-host-run-of-2026-10-05) operator gate, accepted; next, M1's closing run ([D59](../specs/milestones/backlog.md#decisions)) |
| [lab-sno](../examples/lab-sno/README.md#run-it) | A single-node OpenShift cluster installed by the agent installer on one libvirt guest | [B61](../specs/milestones/m3.md#b61) operator gate |
| [lab-baremetal](../examples/lab-baremetal/README.md#run-it) | One physical Machine claimed, proved and installed through its own Redfish controller, its installer image and host key published privately; the emulated rehearsal needs an https emulated controller, [B446](../specs/milestones/m4.md#b446) | [B73](../specs/milestones/m4.md#b73) operator gate, not run |

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

SSH placement of a managed service on a second OS-ready host, with that host's
authored access and bound host key, is operator-run too and has no example.
No real-hardware row has been accepted yet: [B67](../specs/milestones/m3.md#b67)
and [B78](../specs/milestones/m4.md#b78) name one as exit evidence, and M3 and
M4 each need one besides their emulated rehearsal (decision D17). Today a physical
installation is proved in-tree only until [B73](../specs/milestones/m4.md#b73)'s
rehearsal runs, which waits on [B446](../specs/milestones/m4.md#b446), and a
physical cluster node refuses until [B67](../specs/milestones/m3.md#b67).

## Changing the build between runs

The automation is embedded in the executable and participates in the
dependency-bundle identity, so a build that changes `ansible/` changes the
bundle a context is bound to. Run `setup` again after such a build;
`preflight controller` reports the incompatible retained bundle and `apply`
refuses rather than executing automation the receipt does not cover.

An operation left incomplete by the previous build cannot be continued under
the new one, because a continuation runs the automation it froze. Remove it
instead: a fresh `destroy` supersedes any apply that did not complete and runs
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

Changing a frozen block's Go request or plan shape changes its digests, so an
operation registered by an earlier build can be neither continued nor removed
by this one. Destroy or purge any live context before switching builds.

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
