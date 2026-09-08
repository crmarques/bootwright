# Custom playbooks

`CustomPlaybook` retains a reserved, non-executable declaration shape for
operator-supplied Ansible. [The compiler boundary](../api.md#compiler-boundary)
applies to enabled and disabled objects alike: both are strictly decoded,
normalized, reference-checked and rendered.

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
| `spec.secretRefs` | array of strings | no | `[]` | `Secret` references available only to a separately specified isolated runner. |
| `spec.timeout` | string | no | `10m` | Positive Go duration. |
| `spec.enabled` | boolean | no | `true` | Controls declaration ordering/no-work semantics; it does not expose execution. |

The retired `run` and `onFailure` fields are unknown and rejected.

### Anchors and ordering

`gates` and `follows` accept `fabric`, `machines`, `deps`, `base` or `add-ons`.

A `gates` declaration would complete before its anchor; `follows` would
complete after it. Enabled playbooks at the same anchor would order by `order`
and their `provides`/`requires` capability graph. Duplicate providers, missing
requirements, and cycles are invalid for enabled declarations. Disabled
declarations enter no ordering graph.

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
| `clusters` | array of strings | `ContainerCluster` or `StorageCluster` names. |
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
never accepted. `secretRefs` are scalar `Secret` names; desired-state
compilation resolves them and renders only the references.

## Read-only and lifecycle boundary

Normalization materializes `enabled: true` when omitted. Enablement affects
only declaration ordering at the compiler boundary; it grants no execution.

`plan`, `apply`, and `destroy` refuse an enabled `CustomPlaybook` before content
access or operation registration and offer no authorization bypass. A disabled
declaration plans no work.

Executable custom automation requires a separately authorized replacement
schema under the [custom-code boundary](../security.md#custom-code), with
immutable content, bounded extraction or checkout, exact Machine inventory,
bounded Secret materialization, isolated pinned execution, authorization,
failure/cancellation behavior, continuation, durable evidence, typed effects
and an ownership-aware inverse.
