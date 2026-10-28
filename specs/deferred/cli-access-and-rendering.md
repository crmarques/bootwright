# Deferred CLI access, rendering and preflight

The parked item [B101](../milestones/backlog.md#b101) revives this page's
OpenShiftCluster access detail, the items [B52](../milestones/m7.md#b52) and
[B55](../milestones/m7.md#b55) its rendering detail, and the first slice that
implements an Environment preflight family its preflight detail. None of it is
a contract until that slice moves it back into the [CLI](../cli.md), the
[command catalog](../cli/commands.md) or the [output contract](../cli/output.md),
which keep what admission and the current code enforce. Cluster inspection
and the sessions of a managed Ceph cluster are not deferred: the
[cluster discovery contract](../cli/output.md#cluster-discovery) and the
[cluster sessions](../cli.md#cluster-sessions) own them.

## OpenShiftCluster access handoffs and sessions

`cluster oc` and `cluster kubectl` resolve their target and applicability under
the [administrator access export](../cli.md#administrator-access-export), then
check the
[applicability table](../cli/commands.md#cluster-command-applicability). An
inapplicable command fails `cluster.not-applicable` with exit `1`, empty
standard output, and one diagnostic on standard error naming the canonical
command, selected cluster name and kind, applicable targets, and `bootwright
cluster info --context <context> --name <cluster>` as the next discovery action.
It reads no credential material, produces no descriptor or sensitive output, and
performs no write, process, network, or remote access. This target check
follows, and never bypasses, the
[unavailable-command gate](../cli.md#recognized-but-unavailable-commands).

On an applicable target, establish access readiness from local context-owned
metadata and evidence. Missing access metadata or a required credential
artifact fails `access.unavailable` with exit `1`, empty standard output, and a
diagnostic identifying the missing prerequisite and its safe next action.
Missing target or ownership evidence remains `access.target`; unsafe descriptor
encoding remains `access.handoff`. An access prerequisite failure never
produces a partial descriptor or sensitive result and never falls back to
ambient configuration. Applicability and prerequisite checks precede any
permitted credential read.

`cluster oc` and `cluster kubectl` are explicit access handoffs, not
desired-state automation or lifecycle blocks. Bootwright resolves one exact
target and emits a bounded, deterministically escaped descriptor for independent
operator execution. It does not launch a client, connect to the target, open an
interactive stream, or treat later execution as operation evidence. The
descriptor names the pinned client identity, exact target, minimal
non-sensitive configuration, and requested argument vector as data, never shell
text; sensitive argument values have no supported transport. They preserve the
payload argument vector but never inherit ambient kubeconfig, plugins,
credentials, proxy settings, cache, or executable lookup.

`cluster rsh` and `cluster exec` on an OpenShiftCluster open a
session under the same contract as those of a managed Ceph cluster, as
[cluster sessions](../cli.md#cluster-sessions) states, with this node
selection. They resolve `--node` only within `OpenShiftCluster.spec.nodes`,
whose canonical order is ascending bytewise order of node `name`. Omission
selects the first node in that order. For a supplied value, use the first
matching tier:

1. exact declared node `name`;
2. exact effective node FQDN, including an authored override; then
3. `<role>-<ordinal>`, where the role is the node's authored role, including
   `infra`, and the ordinal is a zero-based decimal integer spelled `0` or
   `[1-9][0-9]*`. Filter nodes by that role, sort them in canonical node order,
   and select the indexed node.

A literal node name wins even if it looks like a role selector. Multiple
matches in a tier, an unknown name or role, an absent role, an out-of-range
ordinal, or a malformed selector fails `access.target`; resolution never falls
through from an ambiguous tier or silently selects the default.

## Rendering

The whole `render` is [specified](../cli.md#validation-preflight-and-rendering).
`render installer` and `render storage` write to their context-owned artifact
roots.

## Preflight

`--dry-run` limits the result to deterministic local validation,
renderability, dependency selection, and the checks that can be answered
without process or network access.

## Multi-machine group results

No slice currently revives this detail. The current
[multi-machine presentation](../cli/output.md#multi-machine-presentation) shows
a group only as a progress sub-step, with no result row and no JSON `groups`.
The code keeps matching types that nothing populates: `GroupResult`,
`GroupCounts` and `MachineOutcome` in
`internal/reconciliation/lifecycle/requests.go`.

A multi-machine presentation group is a domain-owned step frozen into a plan
block. Its stable ID, safe description, non-empty Machine target set, order, and
outcome meanings come from the capability contract. Results are grouped as
follows:

1. blocks follow frozen plan order and groups follow their order within a block;
2. Machines sort by canonical `Machine/<metadata.name>` identity;
3. every targeted Machine receives exactly one terminal outcome; and
4. event arrival, parallelism, retry, and callback batching cannot alter order.

A human group begins with:

```text
[<STATUS>] <description>: <total> machines (<counts>)
```

Non-zero counts use this order: `changed`, `unchanged`, `skipped`, `failed`,
`unreachable`, `canceled`, `unknown`. Failed, unreachable, canceled, and unknown
Machine identities follow in canonical order:

```text
  [FAIL] Machine/<name>: failed
  [FAIL] Machine/<name>: unreachable
  [CANCELED] Machine/<name>: canceled
  [UNKNOWN] Machine/<name>: unknown
```

Successful per-Machine detail is omitted. Aggregate precedence is `unknown`,
then failed or unreachable, then canceled, then all-skipped, then success. A
missing terminal event, lost connection, indeterminate callback, or unproved
effect is unknown, never success or cancellation. Bootwright completes the
group with one terminal outcome per target before presenting it.

JSON-mode commands that report groups include a `groups` result array. Each
group orders `blockId`, `groupId`, `description`, `status`, `machines`,
`counts`, `exceptions`, and `log`. `status` is `ok`, `skipped`, `failed`,
`canceled`, or `unknown`; all seven count members are present and sum to
`machines`. Exceptions include exactly failed, unreachable, canceled, and
unknown Machines in canonical order. `log` is the safe relative attempt path on
the final group in its block and `null` otherwise.

## Reserved diagnostic codes

These codes join the [diagnostic taxonomy](../cli/output.md#diagnostic-taxonomy-and-order)
with their first emission:

| Code | Meaning |
| --- | --- |
| `render.publish` | A requested artifact could not be safely rendered or published. |
