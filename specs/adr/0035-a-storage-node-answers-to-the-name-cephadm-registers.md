# ADR 0035: A Storage Node Answers to the Name cephadm Registers

## Status

Accepted

Enforces the invariant [ADR 0017](0017-machine-fqdn-node-identity.md) states but
never checked, and repairs the `nodes[].fqdn` escape hatch
[ADR 0025](0025-composed-names-are-labels-plus-explicit-overrides.md) introduced.

## Context

cephadm identifies a host by the hostname its kernel reports. `ceph orch apply -i`
hands each `service_type: host` document to the mgr, which opens an SSH
connection to the document's `addr` and runs `cephadm check-host
--expect-hostname <hostname>` on the far side. That comparison is a literal
string match against the remote's own hostname. No name resolution participates
in it: a CNAME from the node name to the machine, an A record, a search domain,
an entry in `/etc/hosts` — none of them make a host answer to a name its kernel
does not hold.

ADR 0017 states the consequence as a requirement: a cluster-bound machine's OS
hostname must equal its node FQDN, "or cephadm host matching breaks". Bootwright
satisfied that requirement in exactly one place — the Anaconda kickstart, whose
`--hostname=` flag is fed the node FQDN. That covers every machine whose OS
Bootwright installs and no others. For an `os.provided: true` machine the
requirement was documentation addressed to the operator, and nothing on the
`apply` path ever read the machine's hostname to see whether they had met it.

The failure this produces is expensive out of proportion to its cause. cephadm
rejects the offending host document but exits zero, so the run continues:
Bootwright has by then installed packages on every node, configured
repositories, installed cephadm, bootstrapped a live cluster and stamped
ownership records for it. The rejected node is absent from the orchestrator
inventory, the mon placement that named it is rejected with it, and the
bootstrap default `count:` placement stays in force — so the mgr's own scheduler
picks mon hosts. The operator sees this much later, as a monmap readiness
failure that names neither the node nor the hostname.

A check for precisely this condition already existed and asserted the right
thing, with a failure message that named the remedy. It lived in
`check_storage_preflight`, which only the `preflight` command runs. `apply`
dispatches the storage playbook directly and never imports it. The check was
correct, well-written, and unreachable from the command that needed it.

The one Go check that does run during `apply` and looks at node naming — the
"Name resolution" group — cannot cover this and never could. It is gated on the
machine referencing a `nameResolution` component through
`spec.network.config`, and a provided machine is *required* to leave that field
empty. For exactly the machines whose DNS the operator owns, the lookup is never
constructed.

ADR 0025 offered `nodes[].fqdn` as the escape hatch for a host whose real name
lives outside the composed zone, and promised that "an operator may keep
authoring the label" because every surface referencing a node accepts the FQDN
or its leftmost label. That promise held only while the override was a name
whose leftmost label matched the authored one. `Normalize` resolves the FQDN
into `node.Name`, and the token matchers compare against `node.Name` or its
short form — so pinning an unrelated corporate hostname detached every
cluster-level token from the node it named. Setting `fqdn:` on a stretch
tiebreaker took a valid state to seven validation failures, and the only way to
restore it was to write the corporate hostname into `tiebreaker.node` as well.
The escape hatch for a foreign hostname did not survive being used for one.

## Decision

**The name a node answers to is checked before Bootwright touches the cluster.**
A new `node_identity` phase runs at the head of the cephadm role, on every
storage host, in every apply mode. It gathers the `platform` fact subset,
resolves the host's declared entry, and refuses any node whose
`ansible_facts['nodename']` is not the name cephadm will register. The phase is
ungated: `--stage base` sets `skip_prereqs` and skips the context phase, and the
gate must not be skippable with it. It is reached only through the role's
`main.yml`, so `destroy` — which enters through `tasks_from` — is unaffected and
a mismatched hostname can never strand a teardown.

The comparison is exact against the kernel nodename. It does not fall back to
the short hostname, and it does not case-fold. Both weakenings admit
configurations cephadm rejects, which is the failure this gate exists to
prevent; the preflight assert, which carried both, is corrected to match.

**Bootwright does not write the hostname of a machine it did not install.**
Establishing the name stays where it is: the installer writes it for a managed
OS, and the operator owns it for `os.provided: true`. This ADR moves the
contract from unverified prose to an enforced precondition; it does not move
ownership. Writing an OS hostname onto a provided machine is a separate
decision, with a Satellite consumer-identity hazard behind it, and is not taken
here.

**`nodes[].fqdn` resolves the tokens that name the node.** When `Normalize`
rewrites a node's `name` to its resolved FQDN, it rewrites in the same pass
every cluster-level token that named that node by its authored label —
`bootstrap.node`, the stretch tiebreaker, and `placement.hosts[]` wherever it
appears, including on the `StorageFilesystem`, `StorageObjectGateway` and
`StorageNFSExport` objects bound to the cluster. ADR 0025's guarantee becomes
true by construction rather than by the accident of a matching leftmost label.

## Consequences

- The failure surfaces as a named refusal on the node that has the wrong
  hostname, before any package is installed, instead of as a monmap timeout
  after a cluster exists. The message states what cephadm compares, why DNS does
  not satisfy it, and both remedies.
- A cluster that converges today keeps converging: cephadm applies the same
  predicate at `add_host`, so the gate cannot refuse a host cephadm accepts.
- `apply` now fails on a condition it previously carried into bootstrap. That is
  the intent, and it is not a policy change — the run failed either way.
- Pinning `nodes[].fqdn` becomes usable for its stated purpose. Post-normalize,
  cluster tokens hold resolved names, so validation errors and rendered
  artifacts quote the name cephadm uses rather than the authored label. Renaming
  a node through `fqdn` changes the derived operation keys that embed it
  (`set-mon-location-<node>`), which re-runs those operations once.
- The `--allow-fqdn-hostname` flag Bootwright passes to `cephadm bootstrap`
  keeps its narrow meaning: it permits the seed's own hostname to be an FQDN. It
  has never relaxed the per-host check, and nothing here changes that.
