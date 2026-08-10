# ADR 0055: The Controller Resolves Before the Machine It Contacts

## Status

Accepted

Refines [ADR 0017](0017-machine-fqdn-and-node-identity.md),
[ADR 0018](0018-environment-domain-model.md),
[ADR 0023](0023-teardown-is-the-inverse-of-buildup.md), and
[ADR 0046](0046-the-hosts-preflight-cannot-reach-yet.md).

## Context

Bootwright connects to a machine that consumes managed name resolution through
that machine's FQDN. The managed dnsmasq publishes the record during `fabric`,
but controller split DNS was configured only inside the later OpenShift agent
install role. A storage-only environment, a first run before agent install, or
an explicit `--stage machines` could therefore reach its first SSH connection
while the controller still could not resolve the target.

The cluster-local implementation also made the resolver lifetime wrong. A
ContainerCluster destroy removed controller routing even when the managed DNS
service still served storage or another cluster, and the projected route knew
only the container base domain rather than every desired record the controller
may need.

## Decision

Controller resolution is a consequence of a consumed managed
`nameResolution` service, not of a ContainerCluster install. One context may
resolve consumers to at most one such service. Validation counts resolved
machine-service identities, not catalog rows: unused managed entries do not
count, and compatible entry aliases that select the same `InfraComponent`
remain one service. Consumers that resolve to distinct managed services are
refused before planning. Operator-owned external resolvers remain unrestricted.

Apply plans one controller activity for that service. With `fabric` in the
range it follows the remote service activity. With `machines` selected it may
establish or reconcile the route while treating an omitted `fabric` phase as
prior state. Every machines-phase mutation follows the activity, so a failed
controller probe stops before the first machine mutation or SSH connection.
The activity exists in storage-only input.

A range that starts at `deps`, `base`, or `add-ons` does not inherit an
unchecked assumption about controller DNS. It gets the same activity as a
live-proof task: the activity performs no resolver mutation, participates in no
apply-mode preflight or convergence record, and every selected later mutation
depends on it. A failed proof prints two controller-built commands. The first
reconciles only `fabric` for the service's complete unscoped consumer closure;
the second is the exact original invocation. Repair cannot run selected
`deps`/`base`/`add-ons` work, and resume does not widen that work.

Each service projects a sorted, deduplicated controller contract from desired
state:

- a concrete bind address, or the selected/sole declared service endpoint;
- the machine domain and each consuming ContainerCluster or StorageCluster
  zone;
- an exact route for any rendered record outside those domains; and
- probes for every rendered host, subtree, and CNAME record.

The execution state is narrowed to that exact service. Its convergence hash
uses the same service projection from the unscoped desired state, so
`--clusters` and `--machines` selection cannot manufacture drift, a second
service on the host cannot be recorded as converged without running, and a
controller-only policy value cannot enter the hash.

An existing controller resolver is accepted without mutation only when every
desired record already returns its exact address set, no Bootwright-owned route
for that service needs reconciliation, and no sibling Bootwright route exists.
An owned route is always reconciled to current desired state. Otherwise, a
systemd-resolved controller may be configured automatically only for the one
consumed managed service and when no sibling Bootwright resolver route exists.

The owned drop-in is exclusive owner of systemd-resolved's global `DNS=` and
`Domains=` settings. Before first mutation, any effective static assignment
outside that exact drop-in refuses. An unowned route also requires empty runtime
global settings. An owned route may reconcile its own stale runtime values, but
a per-link routing domain that equals, contains, or is contained by an owned
domain refuses; disjoint per-link routes may coexist. After mutation the global
server and route-domain sets must exactly equal desired state. This prevents
foreign global policy or an overlapping link route from silently capturing an
owned zone.

For the supported single-service case Bootwright records ownership before
writing a context-and-service-specific drop-in under the controller-global
shared-service mutation lease, then verifies effective resolver state and every
probe. It restarts systemd-resolved when the drop-in changed. It also restarts
an unchanged owned route when the initial exact probes failed, because a stale
daemon or cache may be the only drift; healthy unchanged state does not restart.
Both the drop-in and ownership-record identity use a filesystem-safe digest of
the exact context and service identity, while the record retains the readable
identity fields. The gate refuses a sibling Bootwright record or drop-in before
acceptance or mutation, and treats a symlink or non-regular record/drop-in as
foreign rather than following or recursively deleting it. When an owned route
needs reconciliation or an answer is missing, missing addresses,
unsupported or ambiguous resolver state, failed restart, and incomplete
answers fail closed. An unowned operator resolver whose complete exact probe
set already passes needs no managed-service address.

The controller retry accounts for partial mutation. A failed `create` becomes
the same selected invocation under `reconcile`, because ownership evidence may
already exist; `reconcile` and `rebuild` retain their mode. For a selected
machines range that omitted required fabric work, the same retry starts at
`fabric` while preserving its ending stage, context, object selection,
authorizations, effects, and connection options. The later-only live-proof path
uses its separate repair-then-resume pair instead.

The run ledger keeps the original invocation as audit evidence and persists
the CLI-resolved recovery at each task boundary. A stale or cancelled
controller mutation therefore reports the same mode-aware retry, a stale
later-only proof reports the exact original invocation, and a terminal proof
failure replaces that interruption recovery with the two-command repair and
resume sequence. Status never reconstructs these commands from task prose.

Changing the consumed managed service identity in place is unsupported. The
old service must remain declared and consumed while its owning infrastructure
destroy safely removes the old controller route and ownership evidence; only
after that destroy succeeds may desired state select and apply the replacement.
A direct identity switch encounters the old evidence as a sibling and refuses
rather than adopting, overwriting, or deleting it.

Destroy keeps controller routing through all machine teardown. A controller
preflight proves the exact route ownership before any infrastructure mutation;
cleanup waits for every bracketed task to settle and requires `ok` from each
task that names selected resource identity. A skipped/no-host task with
selected work is not completion proof and blocks cleanup while retaining the
route and its evidence; a truly empty no-op may settle as skipped. Only execution of
the owning name-resolution service's destroy role removes its drop-in; a
reference release does not. Resolver removal and any required restart precede
deletion of controller ownership evidence, and any genuine failure retains
that evidence with the exact destroy retry. Cleanup requires fresh absence of
the matching infra-component owner record, so a remote task tolerated under
`unreachable-nodes` cannot silently release the controller route. A destroy
that authorized stale input or unreadable ownership evidence disables
context-wide and record-only orphan sweeps, including the controller
preflight/cleanup bracket, so skipped resources remain standing.

## Consequences

- Storage-only and machines-only runs have the same DNS readiness barrier as
  OpenShift installs.
- Container-cluster records teardown no longer changes controller DNS.
- Existing infrastructure DNS remains a valid controller path when it already
  answers the complete desired probe set.
- Automatic split-DNS mutation remains limited to an unambiguous single-service
  systemd-resolved configuration. Other controller resolver implementations are
  configured out of band and accepted only after their answers prove readiness;
  multiple consumed managed services are invalid desired state.
- The controller play dispatches the registered name-resolution adapter rather
  than naming dnsmasq. A repository guard requires every future managed
  name-resolution adapter to implement both controller apply and owner-destroy
  lifecycle tasks.
- This decision does not change dnsmasq's controller-local listener ordering or
  its `bind-dynamic` race for port 53. B-007 remains open; single-service
  controller routing does not prove dnsmasq acquired its intended listener.
