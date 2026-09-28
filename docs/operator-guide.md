# Operator Guide

This guide is for running Bootwright on a real host: preparing the host,
running a lab journey and recording what happened in the
[acceptance ledger](acceptance.md). Changing Bootwright, its checks and its test
harnesses is [development](development.md). Each example's README owns its own
journey, and [milestones](../specs/milestones.md#status) owns which run is a
gate and which build it must match.

## Prepare a host

`setup` supports RHEL 9 and Fedora on Linux/amd64 at the exact releases its
catalog [qualifies](development.md#qualified-hosts-and-images) and refuses any
other. Each example's host prerequisites add what its journey needs, such as
`/dev/kvm` for a libvirt guest. Run as root, or from an account that may run
`sudo`: Bootwright asks for authorization when a command needs it, and keeps its
state under `/var/lib/bootwright`
([contexts](../specs/contexts.md#storage-locking-and-publication)).

Prepare the host once, from the repository root:

```sh
make build
./bin/bootwright version
./bin/bootwright setup --dry-run
./bin/bootwright setup
./bin/bootwright preflight controller
```

`make build` stamps `bin/bootwright` with the commit and source state of the
checkout, which `version` prints as its `Commit` and `Source` lines. `setup`
prepares the foundation every context on the host shares and reads no desired
state; the dry run previews it, and `preflight controller` reports what it
proved. Each context's first `apply` then binds the context to this host
([binding](../specs/contexts.md#controller-relationship-and-host-binding)), and
its `controller` stage installs the clients its graph selects
([controller](../specs/controller.md#the-controller-stage)). Media images live
in one host-wide store that every context shares, so `setup` runs before the
first `media add`.

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
`bootwright setup --dry-run` prints the resolved route under `Route` in its
scope block, which is the cheapest way to confirm the variables took effect.

## Run a lab journey

Each lab makes the host the controller and the host of the managed services it
declares. Run one emulated lab per host at a time: lab-rhel and lab-sno bind the
same sockets and the same guest bridge.

| Journey | What it runs | Gate |
| --- | --- | --- |
| [lab-rhel](../examples/lab-rhel/README.md#run-it) | One RHEL guest installed through an emulated Redfish BMC, a settled replay, a removal refused while the guest runs, a fresh apply and a host restart | [B72](../specs/milestones/m4.md#b72) operator gate |
| [lab-sno](../examples/lab-sno/README.md#run-it) | A single-node OpenShift cluster installed by the agent installer on one libvirt guest | [B61](../specs/milestones/m3.md#b61) operator gate |
| [lab-baremetal](../examples/lab-baremetal/README.md#run-it-today) | Admission and import of one physical Machine, then the refusal of its installation; the emulated rehearsal once B73 resumes | [B73](../specs/milestones/m4.md#b73) operator gate, blocked |

Run each block from the repository root, one line at a time, and keep the
sequence exactly as run: the ledger records it. After each operation, `status`
reports its final state; that state and every refusal met on the way are the
run's outcome.

An apply that installs a cluster or an operating system can run for most of an
hour, and it lives only as long as the command that started it. Closing its
terminal or losing the SSH session it runs in interrupts it as Ctrl-C does, and
a command killed outright takes its running lifecycle adapter with it
([process boundary](../specs/security.md#process-boundary)). Run a long apply
where it outlives your connection: inside a `tmux` session, or as a transient
systemd unit whose output `journalctl` follows. A unit runs as root rather than
through `sudo`, so it reads root's context selection, not yours; name the
context:

```sh
sudo systemd-run --unit=bootwright-apply --collect "$PWD/bin/bootwright" apply --context lab-sno --yes
sudo journalctl -f -u bootwright-apply
```

SSH placement of a managed service on a second OS-ready host, with that host's
authored access and bound host key, is operator-run too and has no example.
Real hardware is no item's gate yet, but M3 and M4 each need a real-hardware
row besides their emulated rehearsal (decision D17). Today a physical
installation refuses until [B73](../specs/milestones/m4.md#b73) resumes, and a
physical cluster node until [B67](../specs/milestones/m3.md#b67).

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

Changing a frozen block's Go request or plan shape changes its digests, so an
operation registered by an earlier build can be neither continued nor removed
by this one. Destroy or purge any live context before switching builds.

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
