# Deferred CLI access, rendering and preflight

The items [B52](../milestones/m7.md#b52) and
[B55](../milestones/m7.md#b55) revive this page's rendering detail; the first
slice implementing an Environment preflight family revives its preflight detail. None of it is
a contract until that slice moves it back into the [CLI](../cli.md), the
[command catalog](../cli/commands.md) or the [output contract](../cli/output.md),
which keep what admission and the current code enforce. Cluster inspection
and cluster access are not deferred: the
[cluster discovery contract](../cli/output.md#cluster-discovery) and the
[cluster sessions](../cli.md#cluster-sessions) and
[client handoffs](../cli.md#cluster-client-handoffs) own them.

## Rendering

The whole `render` is [specified](../cli.md#validation-preflight-and-rendering).
`render installer` and `render storage` write to their context-owned artifact
roots.

## Preflight

A deferred preflight family's own checks need no process or network access;
live observation follows
[validation, preflight and rendering](../cli.md#validation-preflight-and-rendering).

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
