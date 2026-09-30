# Deferred CLI access, rendering and preflight

The parked item [B101](../milestones/backlog.md#b101) revives this page's
cluster inspection, access and node-selection detail, the items
[B52](../milestones/m2.md#b52) and [B55](../milestones/m2.md#b55) its rendering
detail, and the first slice that implements an Environment preflight family its
preflight detail. None of it is a contract until that slice moves it back into
the [CLI](../cli.md), the [command catalog](../cli/commands.md) or the
[output contract](../cli/output.md), which keep what admission and the current
code enforce.

## Cluster inspection and explicit access

`cluster info` omits secret values by default and presents the exact
`secret show` or `cluster kubeconfig` command needed to retrieve them.
`cluster list` identifies each cluster's API kind. `cluster info` presents
each cluster's kind and the applicability and availability of its access
commands under the [discovery output contract](#cluster-discovery).

`cluster kubeconfig` resolves its target, applicability and readiness under the
[administrator access export](../cli.md#administrator-access-export). For an
available cluster access handoff, load and validate
the complete selected graph, resolve `--name` to one selected cluster, then check the
[applicability table](../cli/commands.md#cluster-command-applicability). An unknown
or excluded name fails `access.target`; it never selects another cluster or
searches outside the selected graph. An inapplicable command fails
`cluster.not-applicable` with exit `1`, empty standard output, and one diagnostic
on standard error naming the canonical command, selected cluster name and kind,
applicable targets, and `bootwright cluster info --context <context> --name
<cluster>` as the next discovery action. It reads no credential material,
produces no descriptor or sensitive output, and performs no write, process,
network, or remote access. This target check follows, and never bypasses, the
[unavailable-command gate](../cli.md#recognized-but-unavailable-commands).

On an applicable target, resolve the exact node when required, then establish
access readiness from local context-owned metadata and evidence. Missing access
metadata or a required credential artifact fails `access.unavailable` with exit
`1`, empty standard output, and a diagnostic identifying the missing prerequisite
and its safe next action. Missing target or ownership evidence remains
`access.target`; SSH identity and trust failures retain `trust.identity`, and
unsafe descriptor encoding remains `access.handoff`.
An access prerequisite failure never produces a partial descriptor or sensitive
result and never falls back to ambient configuration. Applicability and
prerequisite checks precede any permitted credential read.

`cluster rsh`, `cluster exec`, `cluster oc`, and `cluster kubectl` are explicit
access handoffs, not desired-state automation or lifecycle blocks. Bootwright
resolves one exact target and emits a bounded, deterministically escaped
descriptor for independent operator execution. It does not launch a client,
connect to the target, open an interactive stream, or treat later execution as
operation evidence. The descriptor names the pinned client identity, exact
target, minimal non-sensitive configuration, and requested argument vector as
data, never shell text; sensitive argument values have no supported transport.
[`machine rsh` and `machine exec`](../cli.md#machine-ssh-sessions) instead open the
session themselves.

`machine exec` and `cluster exec` preserve the command values as an argument
vector, never shell text. `rsh` accepts no command tail. `oc` and `kubectl`
preserve the payload argument vector but never inherit ambient kubeconfig,
plugins, credentials, proxy settings, cache, or executable lookup.

## Cluster node selection

`cluster rsh` and `cluster exec` resolve `--node` only within the selected
cluster's declared roster: `ContainerCluster.spec.nodes` or managed
`StorageCluster.spec.ceph.topology.nodes`. Canonical node order is ascending
bytewise order of node `name`, independent of declaration or map order.
Omission selects the first node in that order. An explicitly empty value is a
usage error under the scalar input rules.

For a supplied value, use the first matching tier:

1. exact declared node `name`;
2. exact effective node FQDN, including an authored override; then
3. `<role>-<ordinal>`, where the role is declared by the owning cluster schema
   and the ordinal is a zero-based decimal integer spelled `0` or `[1-9][0-9]*`.
   Filter nodes by that role, sort them in canonical node order, and select the
   indexed node. Container nodes use their authored role, including `infra`;
   a storage node participates once in each role listed in its `roles` set.

A literal node name wins even if it looks like a role selector. Multiple
matches in a tier, an unknown name or role, an absent role, an out-of-range
ordinal, or a malformed selector fails `access.target` with exit `1`; resolution
never falls through from an ambiguous tier or silently selects the default.
The diagnostic names the requested selector and gives the safe next action of
choosing one declared node name. Node selection changes no desired state,
canonical serialization, or lifecycle scope.

## Cluster discovery

`cluster list` returns one `clusters` array containing both cluster kinds in
ascending bytewise order of cluster name. Every entry identifies `name` and
`kind`, with `kind` exactly `ContainerCluster` or `StorageCluster`. Human output
shows the same names and kinds in the same order.

`cluster info` retains its `context`, `clusters`, and `storage` result fields.
`clusters` is the ContainerCluster array and `storage` is the StorageCluster
array; each sorts by cluster name and is empty when its kind is not selected.
Each cluster entry includes `name`, `kind`, and `accessCommands` alongside its
endpoints and artifact metadata. Human output shows container clusters then
storage clusters with the same membership and ordering as JSON. An explicitly
selected unknown or excluded cluster fails `access.target` with exit `1` under
the selected human or JSON failure mode; it never yields a successful empty
selection. Without `--name`, the catalog's default-all selection applies.

`accessCommands` contains exactly one entry for each canonical command, ordered
`cluster exec`, `cluster kubeconfig`, `cluster kubectl`, `cluster oc`, then
`cluster rsh`. Each entry orders these fields:

| Field | Contract |
| --- | --- |
| `command` | Canonical command path without the executable. |
| `status` | Exactly `not-applicable`, `not-implemented`, `unavailable`, or `ready`, selected by the precedence below. |
| `node` | Default node's declared name for applicable `cluster rsh` and `cluster exec`; omitted for other entries. |
| `reason` | Required non-empty safe explanation when status is not `ready`; omitted when ready. |

Within an available `cluster info` result, evaluate each entry as follows:

1. `not-applicable`: the cluster fails the catalog's
   [applicability rule](../cli/commands.md#cluster-command-applicability).
2. `not-implemented`: the command applies but this executable does not implement
   the requested access use case.
3. `unavailable`: the command applies and is implemented, but local access
   metadata, credential-artifact availability, identity, ownership, or context
   state does not establish the prerequisites. The reason identifies the
   missing or blocking evidence and the safe next action.
4. `ready`: the prerequisites are established by local context-owned metadata
   and evidence. This means ready to request the descriptor or explicit export;
   it does not assert live endpoint readiness or success of later execution.

SSH entries describe the default node selected under
[cluster node selection](#cluster-node-selection); readiness for that
node makes no claim about other nodes. Inspection opens no credential bytes by
default and performs no live probes to populate these fields. Secret declarations
alone do not prove that access material is available. Sensitive `cluster info
--secrets` retains its separate explicit disclosure boundary.

Human `cluster info` includes these statuses and reasons, identifies the default
SSH node, and gives context- and cluster-qualified invocation templates for
applicable commands, including `--node` for SSH and `-- <command>...` for a
required payload. Unavailable and unimplemented templates are visibly labeled;
inapplicable entries explain the restriction and offer no invocation template.
A template is guidance, never an executable handoff or a promise of readiness.

These inspection statuses do not alter invocation precedence: invoking an
unavailable command still follows the no-context-read
[unavailable-command gate](../cli.md#recognized-but-unavailable-commands).

## Rendering

Context-free render requires both path flags and writes placeholder-bearing
portable artifacts after loading and validating the desired-state file or
directory named by `--input-dir`. `render installer` and `render storage` write
to their context-owned artifact roots.

## Preflight

`--dry-run` limits the result to deterministic local validation,
renderability, dependency selection, and the checks that can be answered
without process or network access.

`--trust-on-first-use=true` permits bounded retrieval and presentation of an
unknown SSH host key from the exact authorized endpoint. It never accepts,
persists, or uses the key. The preflight fails `trust.identity` and names the
exact `machine trust` request needed for explicit enrollment.

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
| `preflight.unknown` | Required readiness could not be positively determined. |
