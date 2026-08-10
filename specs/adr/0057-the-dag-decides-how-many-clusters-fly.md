# ADR 0057: The DAG Decides How Many Clusters Fly

## Status

Accepted

Extends [ADR 0053](0053-the-flight-plan-a-run-publishes.md), whose independence
contract makes undeclared cross-cluster ordering a failure. Follows the
value-carrying flag convention of
[ADR 0010](0010-cli-gate-and-flag-conventions.md).

## Context

The apply graph already gives independent ContainerCluster roots no edge and
runs Ceph as peer work. The scheduler nevertheless defaulted the separate
cluster-install budget to one. Two bare-metal OCP roots authored as independent
were therefore serialized even while Ceph work ran beside the first one. The
run plan said they could fly together, but a policy unrelated to the plan
prevented it.

The limit was introduced conservatively after concurrent release-payload pulls
starved a shared proxy. That is a real estate constraint, but it is not true of
every fleet. Making one the universal default turns an optional protection into
an implicit dependency. The only override was an environment variable, which
made a one-run throttle awkward to express and easy to lose from an exact retry.

## Decision

With no override, the cluster-install capacity equals the number of distinct
ContainerCluster install chains in the selected graph. The capacity is therefore
non-binding: dependency edges, resource locks, and the global, per-host, and
Redfish budgets alone decide which work can run. Managed-OS and StorageCluster
tasks, including Ceph work, remain outside the cluster-install budget.

`apply` and `plan` accept the positive value-carrying flag
`--cluster-install-parallelism`. Resolution is flag, then
`BOOTWRIGHT_APPLY_PARALLELISM_CLUSTERS`, then graph-derived capacity. A requested
value is clamped to the number of chains present. Invalid environment values are
ignored; invalid explicit flag values are usage errors. The resolved integer is
persisted in the run ledger, and exact apply retries retain an explicit flag.

The four task kinds in one install chain remain agent ISO, node boot, bootstrap
wait, and install wait. When a narrower cap is scarce, admission continues to
prefer an unparked chain over a hosted chain waiting on another cluster and
releases a held slot when a chain parks. When capacity fits every candidate,
admission lets every candidate compete and the DAG decides readiness.

## Consequences

- Independent OCP roots and peer Ceph work start together by default when the
  other budgets have capacity.
- Fleets sharing a constrained registry, mirror, proxy, or BMC path can set a
  standing environment limit or throttle one invocation with the flag.
- A run header may report more than one cluster install without configuration;
  the number is the selected graph's demand, not a machine-wide constant.
- Only an ordering edge serializes two install chains in the DAG; shared locks
  and the independently configured budgets remain separate constraints.
