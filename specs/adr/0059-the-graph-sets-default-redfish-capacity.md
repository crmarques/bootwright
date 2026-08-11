# ADR 0059: The Graph Sets Default Redfish Capacity

## Status

Accepted

Extends [ADR 0057](0057-the-dag-decides-how-many-clusters-fly.md), applying
the same graph-full default to the independent Redfish budget.

## Context

The apply graph may make bare-metal cluster roots and managed storage peers
independent while every machine has its own BMC resource lock. A fixed default
Redfish budget of eight nevertheless imposed ordering that the graph did not.
In a production graph with a six-node managed-OS install and two three-node OCP
boots, first-fit partial admission granted `6 + 2 + 0`: one OCP root remained
pending until unrelated work completed even though the cluster-install budget,
dependencies, and resource locks all allowed it to start.

Partial Redfish grants keep a task dispatchable under an intentionally narrow
budget, but they cannot make a universal fixed budget describe every estate.
Distinct BMC resource keys already serialize operations against the same target.
The global task limit protects controller capacity, and the per-host limit
protects shared hypervisors and service machines. A narrower shared BMC or
management-path limit is estate policy, not an undeclared dependency every run
should inherit.

The scheduler also recorded a denied admission only in memory. It saved and
reported task starts and completions, so the last visible frame could show a
bare pending phase until another task event happened instead of naming the
budget that withheld it.

## Decision

With no override, Redfish capacity equals the sum of the Redfish slots declared
by tasks in the selected apply or destroy graph. It is therefore non-binding:
graph edges, per-target resource locks, and the global and per-host budgets
decide which BMC work can enter. A graph with no Redfish work retains a trivial
capacity of one for a stable ledger shape.

`BOOTWRIGHT_APPLY_PARALLELISM_REDFISH` remains the standing positive override.
A valid value is clamped to graph demand, while an absent, invalid, zero, or
negative value selects graph-derived capacity. The resolved value and graph
demand remain persisted in the run ledger, so a run in flight keeps the policy
under which it started.

An explicit narrow limit retains partial admission: a task receives
`min(declared, free)` slots when any are free, uses that grant as its Ansible
fork count, and holds the grant until the task returns.

Whenever dependency-ready work is denied by any scheduler budget, lock, or
admission rule, the scheduler saves the updated `ReadyAt` and `BlockedOn` state
and publishes a run-tree snapshot before waiting for the next task event.

## Consequences

- Independent managed-OS and cluster-boot cohorts such as `6 + 3 + 3` can all
  start by default when their dependencies and the remaining budgets allow it.
- Estates with a constrained BMC gateway or management path opt into a standing
  Redfish throttle with the existing environment variable.
- Global controller and per-host safety limits are unchanged.
- An intentional cap may still queue work, but both the durable ledger and live
  tree name the active blocker immediately.
