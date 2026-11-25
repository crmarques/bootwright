# Custom playbooks

`CustomPlaybook` retains a reserved, non-executable declaration shape for
operator-supplied Ansible. The closed schema in
`api/v1alpha1/customplaybooks.go` is authoritative for its fields, and the
[custom playbook detail](../deferred/custom-playbooks.md) records the reserved
ordering graph that [B102](../milestones/backlog.md#b102) revives.
[The compiler boundary](../api.md#compiler-boundary) applies to enabled and
disabled objects alike: both are strictly decoded, normalized,
reference-checked and rendered.

A `CustomPlaybook` name follows the [one name rule](../api.md#object-names).

## Kinds

| Kind | Declares |
| --- | --- |
| `CustomPlaybook` | One reserved operator playbook declaration; it validates and never runs. |

## Reserved-shape contract

Admission checks source, path, reference, tag and Secret-type spelling
lexically; it never resolves a source, fetches Git content, reads a playbook or
reads credential bytes. Cluster and Machine targets resolve in the complete
graph, and the controller is never a target: its inventory identities are
forbidden host groups. `extraVars` is the kind's one open value map and rejects
connection, inventory, interpreter, SSH, privilege and Bootwright-reserved
names; inline secret bytes are never accepted.

Normalization materializes `enabled: true` when omitted. Enablement affects
only declaration ordering at the compiler boundary: enabled declarations at one
anchor form a valid `order` and `provides`/`requires` graph, and disabled
declarations enter none. It grants no execution.

## Schema

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.gates` | string | conditional | — | Exactly one of `gates` and `follows`. |
| `spec.follows` | string | conditional | — | Exactly one of `gates` and `follows`. |
| `spec.source` | object | no | — | Exactly one `path` or `git` arm when present. |
| `spec.playbook` | string | yes | — | Clean relative entrypoint with a case-insensitive `.yaml` or `.yml` suffix. |
| `spec.rolesPath` | string | no | — | Clean relative roles directory inside the selected content root. |
| `spec.collectionsPath` | string | no | — | Clean relative collections directory inside the selected content root. |
| `spec.tags` | array of strings | no | `[]` | Ordered unique Ansible tag tokens. |
| `spec.skipTags` | array of strings | no | `[]` | Ordered unique tag tokens, disjoint from `tags`. |
| `spec.target` | object | yes | — | At least one target list below. |
| `spec.order` | integer | no | `0` | Reserved ordering tie-break within one anchor. |
| `spec.provides` | array of strings | no | `[]` | Reserved capability names. |
| `spec.requires` | array of strings | no | `[]` | Reserved dependencies on same-anchor providers. |
| `spec.extraVars` | object | no | `{}` | Reserved arbitrary non-connection extra-variable values. |
| `spec.secretRefs` | array of strings | no | `[]` | `Secret` references, effect material available only to a separately specified isolated runner. |
| `spec.timeout` | string | no | `10m` | Positive Go duration. |
| `spec.enabled` | boolean | no | `true` | Controls declaration ordering/no-work semantics; it does not expose execution. |

`gates` and `follows` accept `fabric`, `machines`, `deps`, `base` or `addOns`.
They are legacy tokens: only `machines` and `addOns` name a current
[stage](../state-reconciliation.md#stages-and-the-pause-boundary) (whose CLI
token is `add-ons`), and a block depends on other blocks, never on a stage.

### Source

`spec.source` sets exactly one arm when present:

| Arm | Fields | Rule |
| --- | --- | --- |
| `path` | scalar string | Absolute external content directory outside the environment input tree. |
| `git` | `url`, `ref`, optional `subdir`, optional `secretRef` | Reserved Git source. `url` accepts HTTPS, SSH, `file://`, or an absolute local repository location outside the environment input tree; `ref` is required; `subdir` stays below the repository root. |

An HTTPS Git credential names a `token` or `usernamePassword` Secret. An SSH
credential names an `sshKeyPair` Secret. A local repository rejects a
credential. Desired-state validation checks URL, path, subdirectory, ref,
reference, and Secret-type spelling without resolving the source or reading
credential bytes.

With no source, `playbook`, `rolesPath`, and `collectionsPath` are relative to
the declaring object. With a source, they are relative to its content root.
They cannot be absolute, empty, or escape through `..`. The playbook has a
case-insensitive `.yaml` or `.yml` suffix and, for co-located content, has a
`playbooks` path segment. For co-located content, `rolesPath` has an exact
`roles` segment and `collectionsPath` has an exact `collections` segment;
externally sourced content does not need those segment names. Discovery
excludes those exact payload roots before reading their contents. Roles and
collections directories cannot be named `vendor` or `node_modules`.

These are lexical desired-state checks. File existence, file type, symlink
rejection, content digesting, Git resolution, archive or repository safety,
and Ansible syntax require a separate immutable execution-content contract
before an enabled declaration can execute.

### Target

`spec.target` has exactly these list fields:

| Field | Type | Namespace and rule |
| --- | --- | --- |
| `clusters` | array of strings | `OpenShiftCluster` or `CephCluster` names. |
| `machines` | array of strings | `Machine` names. |
| `hostGroups` | array of strings | Inventory-group tokens. |

At least one list is non-empty. Cluster and Machine names resolve in the
complete desired-state graph. Host groups are provisioning tokens; the
controller identities `localhost`, `127.0.0.1`,
`bootwright_ocp_hosts`, and `bootwright_controller_hosts` are forbidden. A
qualified executor must expand the declaration to an exact non-empty Machine
set and derive access only from those Machines; the controller is never a
target.

### Tags, variables, and Secrets

Tags and skipped tags match `^[A-Za-z0-9][A-Za-z0-9._-]*$`, contain no leading
or trailing whitespace, are unique in each list, and do not overlap.

`extraVars` is the one explicit open value map in this kind. Connection,
inventory, interpreter, SSH, privilege, and Bootwright-reserved variable names
are rejected because they could repoint an executor. Inline secret bytes are
never accepted. `secretRefs` are scalar `Secret` names of effect material; desired-state
compilation resolves them and renders only the references.

## Lifecycle refusal

An enabled `CustomPlaybook` is an object no capability realizes, so a fresh
`apply` and its `--dry-run` preview refuse it under the
[unrealizable-kind rule](../state-reconciliation.md#unrealizable-objects)
before content access or operation registration, with no authorization bypass,
and `validate` warns it `api.unrealized`
([admitted but unrealized](../api.md#admitted-but-unrealized)). A disabled
declaration plans no work and is warned `api.deferred`.

Executable custom automation requires a separately authorized replacement
schema that meets every prerequisite of the
[custom-code boundary](../security.md#custom-code).
