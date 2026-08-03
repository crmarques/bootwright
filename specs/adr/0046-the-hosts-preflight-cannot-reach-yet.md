# ADR 0046: The Hosts Preflight Cannot Reach Yet

## Status

Accepted

Extends [ADR 0017](0017-machine-fqdn-node-identity.md): the "Name resolution"
group it defines now also covers a cluster-bound machine whose `os.provided:
true` forbids it from referencing a resolver. Closes the coverage gap
[ADR 0035](0035-a-storage-node-answers-to-the-name-cephadm-registers.md)
recorded in passing for that same group.

## Context

`preflight` runs before `apply`. Both getting-started guides publish it in that
order, and the pre-apply ritual in `docs/getting-started/installation.md` lists
it between `bastion setup` and `plan` — before anything has provisioned a
machine.

Its Ansible half did not hold that order. The "Check host prerequisites" play
targets `bootwright_storage_hosts` along with the provider, infra-component and
boot host groups, and gathered facts from all of them. A storage node whose
`os.provided` is `false` has no OS until the machines phase installs one, so on
a day-1 estate every one of those hosts is unreachable. Ansible marked them
`dark`, exited `4`, and `preflight all` failed with no way to pass: the run
demanded a login on machines the same run exists to create. B-070 had already
noticed the consequence from the other end — the shipped lab profile is under
the storage preflight's own disk floor, "so either the shipped lab fails its own
preflight, or the preflight is not reached on that path". It was not reached.

The failure the operator saw named the wrong thing twice. `summarizeFailure`
attributed it to the **last** `TASK [` banner in the log — the final task that
ran on the hosts that *were* reachable — rather than the task the failure
happened in. And it reported the first `fatal:` line with a parseable message,
which on a play with several unreachable hosts is whichever host lost the race,
not the one worth acting on.

The same day-1 estate exposed a second gap, in the Go half. The "Name
resolution" group builds its lookups only for a machine that references a
name-resolution entry through `spec.network.config`. Validation *forbids* that
field on an `os.provided: true` machine, so a provided machine can never
reference one — the absence carries no information about DNS, only about how
the machine was authored. ADR 0017 carved out "machines whose network
configuration references no name-resolution entry at all" from connecting *by
name*, which is right and stays; nothing followed from it about whether the
machine's records must exist. A stretch cluster's provided arbiter is a
topology node like any other: it holds a node FQDN that ADR 0017 makes the
cluster-visible identity, and in an environment whose resolver the operator owns
that record is theirs to create. It was the one node preflight never asked
about.

## Decision

**Preflight probes the connection first, then classifies the host it could not
open.** The play gathers facts through its own task with `ignore_unreachable`
rather than the play's implicit gather, and reads the result:

- A machine whose OS this run installs (`bootwright_os_provided` is `false`) is
  **deferred**: the play reports that Bootwright installs it in the machines
  phase, which runs before the phase that logs in to it, and ends the host.
  Preflight stays green on it.
- Every other host is **refused**: the play fails, naming the host, the address
  and user it tried, and the transport error. Nothing this run does makes such a
  host reachable, so demanding it before apply is the point.
- A host with no declared OS provenance defaults to must-be-reachable. Only a
  machine the inventory positively marks as installed-this-run may be deferred.

`ignore_unreachable` stays on the probe task alone. A host that answers the
probe and drops later still fails the run, because a check that silently did not
run is worse than one that failed.

**Name resolution follows the environment's declaration when the machine cannot
carry one.** A machine that is `os.provided: true`, that a cluster names as a
node, and that lives in an environment declaring name-resolution entries gets
the same two lookups as any other node: its `fqdn`, and its node FQDN. They fail
rather than warn, because Bootwright renders no record for a machine that
references no resolver — the managed resolver's `host-record`/`cname` set is
built from the machines that reference it, so no apply will produce this one.
The remediation names the exact record to create, as ADR 0017 specified.

An environment that declares no name resolution at all still gets no lookups.
There is nothing to ask, and inventing a requirement for an estate that
deliberately runs on addresses would be a regression, not a check.

**A failure summary names the task the failure happened in.** The scan tracks
the banner in force when each `fatal:` is printed and reports that one. Between
candidates it prefers a real failure over a tolerated unreachable, and an
enriched `[ERROR]:` banner over both, so the line an operator reads is the
refusal the run made rather than the noise it decided to accept.

## Consequences

- `preflight all` passes on a day-1 estate, and the storage host checks —
  hostname, systemd, repositories, root-filesystem budget, OSD device
  signatures — become reachable on the next preflight, once the machines phase
  has installed the nodes. B-070's "or the preflight is not reached on that
  path" is answered: it is now reached, on the second run.
- A provided host that refuses Bootwright's declared login fails preflight
  alone, by name, instead of being buried in a play-wide `exit status 4`.
- A managed resolver still publishes nothing for a provided cluster node, so the
  new lookups fail in a managed-DNS environment with no record to point at. That
  is honest — apply would not create it either — but it means a provided node in
  such an environment needs a record the operator places by hand, or the
  resolver's record set has to grow to cover machines it serves without being
  referenced by them. The second is the larger decision and is not made here.
- The deferral is reported in the Ansible log, not in the CLI check list. The Go
  checks run before the play and cannot observe reachability; claiming a host was
  skipped when it was in fact checked would be the worse error.
