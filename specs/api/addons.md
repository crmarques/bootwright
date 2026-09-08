# Cluster add-ons, profiles, and bindings

This page owns `ClusterAddon`, `ClusterAddonProfile`, and
`ClusterAddonBinding`. An add-on is one reusable `olm` or `manifestSet`
declaration. Profiles compose add-ons and other profiles. Bindings attach the
expanded set to one `ContainerCluster` and supply scalar input values.

[The add-on contract](../add-ons.md) owns executable packages, catalogs,
drivers and lifecycle qualification. Schema acceptance implies no execution
support; all declarations obey [the compiler boundary](../api.md#compiler-boundary).

References are plain scalar names in fixed namespaces: `clusterRef` names a
`ContainerCluster`, `profileRefs` name `ClusterAddonProfile` objects,
`addonRefs` and `addonRef` name `ClusterAddon` objects, and `secretRefs` name
`Secret` objects. An accepted `resourceRef.kind` supplies the namespace for its
binding value.

## ClusterAddon

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.type` | string | yes | — | `olm` or `manifestSet`. |
| `spec.provides` | array of strings | no | `[]` | Set of capability tokens. |
| `spec.requires` | array of strings | no | `[]` | Set of capability tokens. |
| `spec.accepts` | object | no | — | Contains only `inputs`. |
| `spec.olm` | object | conditional | — | Required exactly for `type: olm`. |
| `spec.manifestSet` | object | conditional | — | Required exactly for `type: manifestSet`. |
| `spec.readiness` | object | no | — | Timeout and readiness checks. |
| `spec.steps` | array of objects | no | `[]` | Ordered lifecycle-step declarations. |

Capability tokens match `^[A-Za-z0-9][A-Za-z0-9._-]*$` and are unique in each
list. `provides` and `requires` are not closed enums. The names `kubevirt`,
`dataFoundation`, and `nmstate` have defined downstream meanings, but all
valid tokens participate in dependency ordering. An add-on with any
`provides` value declares at least one readiness check.

### Accepted inputs and effects

`spec.accepts.inputs` is a set keyed by `name`. Each entry is:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Unique within the add-on. |
| `required` | boolean | no | `false` | Every binding must supply a value when true. |
| `resourceRef` | object | conditional | — | Exactly one of `resourceRef` and `secretRef`. |
| `resourceRef.kind` | string | conditional | — | A registered Bootwright kind; the scalar binding value names that kind. |
| `secretRef` | empty object | conditional | — | Presence arm; the scalar binding value names a `Secret`. |
| `effects` | array | no | `[]` | Each entry is one effect union below. |

Each effect entry sets exactly one arm:

| Arm | Shape | Rule |
| --- | --- | --- |
| `storageExportAttachment` | `{}` | Input is `resourceRef: {kind: StorageExport}` and the add-on provides `dataFoundation`. |
| `globalPullSecretMerge` | `{registry?, username?}` | Input uses `secretRef: {}`; both strings are required by semantic validation. |

Validation checks effect compatibility and resolves supplied object names.

### OLM arm

`spec.olm` contains:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `namespace.name` | string | yes | — | Kubernetes namespace. |
| `namespace.create` | boolean | no | `false` | Whether the lifecycle creates it. |
| `namespace.labels` | map of string to string | no | `{}` | Kubernetes label keys and values. |
| `operatorGroup` | object | no | — | Optional OperatorGroup. |
| `operatorGroup.name` | string | conditional | — | Required when the block is present. |
| `operatorGroup.targetNamespaces` | array of strings | no | `[]` | Ordered non-empty namespaces. |
| `catalogSource` | object | no | — | Optional shipped CatalogSource. |
| `catalogSource.name` | string | conditional | — | Required when the block is present. |
| `catalogSource.image` | string | conditional | — | Required when the block is present. |
| `catalogSource.displayName` | string | no | — | Optional display name. |
| `catalogSource.publisher` | string | no | — | Optional publisher. |
| `catalogSource.pollInterval` | string | no | — | Valid Go duration. |
| `catalogSource.grpcPodConfig.securityContextConfig` | string | conditional | — | `legacy` or `restricted` when the block is present. |
| `subscription.name` | string | yes | — | Kubernetes Subscription name. |
| `subscription.package` | string | yes | — | OLM package. |
| `subscription.channel` | string | yes | — | OLM channel. |
| `subscription.startingCSV` | string | no | — | Optional starting CSV. |
| `subscription.source` | string | yes after normalization | `catalogSource.name` when shipped | CatalogSource name. |
| `subscription.sourceNamespace` | string | yes after normalization | `openshift-marketplace` | CatalogSource namespace. |
| `subscription.installPlanApproval` | string | yes after normalization | `Automatic` | `Automatic` or `Manual`. |
| `customResources` | array of objects | no | `[]` | Raw Kubernetes resources preserved as structured maps. |

A shipped catalog requires both name and image, and subscription source must
match its name. When a shipped `catalogSource` is present and
`namespace.create: true`, `subscription.sourceNamespace` must differ from
`namespace.name` because the CatalogSource namespace must already exist. Each
custom resource requires string `apiVersion`, string `kind`, and
`metadata.name`. A `kind: Secret` custom resource cannot carry `data` or
`stringData` because authored desired state never embeds secret bytes. Other
custom-resource keys remain an explicit open Kubernetes payload; the
containing Bootwright schema remains closed.

### Manifest-set arm

`spec.manifestSet.manifests` is a required non-empty ordered array of
`{path: <string>}`. A path is non-empty, has no leading or trailing
whitespace, is relative to the declaring add-on, remains below that directory,
has a `manifests` path segment, and has a case-insensitive `.yaml` or `.yml`
suffix.

Validation checks paths lexically. Materialization follows the add-on
contract's package, native-schema and content-safety gates.

### Readiness

`readiness.timeout` is a positive Go duration and normalizes to `30m`.
`readiness.checks` is an ordered array. Each check sets exactly one arm:

| Arm | Exact fields |
| --- | --- |
| `csvSucceeded` | required `namespace` and `subscription` |
| `condition` | required `apiVersion`, `kind`, `name`, and `condition: {type, status}`; optional `namespace` |
| `resourceExists` | required `apiVersion`, `kind`, and `name`; optional `namespace` |

### Steps

`spec.steps` is an ordered array keyed by unique `name`. The exact step shape
is:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Unique provisioning token. |
| `gates` | string | conditional | — | Exactly one of `gates` and `follows`; only `apply`. |
| `follows` | string | conditional | — | `operatorReady` or `ready`; `operatorReady` is OLM-only. |
| `requires` | readiness-check array | no | `[]` | Same three-arm union as add-on readiness. |
| `source` | object | no | — | Shared playbook source shape below. |
| `playbook` | string | conditional | — | Relative entrypoint with a case-insensitive `.yaml` or `.yml` suffix; co-located content has a `playbooks` path segment. |
| `rolesPath` | string | no | — | Contained relative roles directory. |
| `collectionsPath` | string | no | — | Contained relative collections directory. |
| `target` | object | conditional | — | Required with a playbook; union below. |
| `extraVars` | object | no | `{}` | Arbitrary non-connection extra-variable values. |
| `secretRefs` | array of strings | no | `[]` | Set of `Secret` refs. |
| `timeout` | string | no | `10m` | Positive Go duration. |
| `outputs` | array of objects | no | `[]` | Requires a playbook. |
| `manifests` | array of objects | no | `[]` | Ordered manifest templates. |

A step declares `playbook`, `manifests`, or both; `run` and `onFailure` are
unknown fields. Reject reserved connection, inventory and privilege names in
`extraVars`. Co-located playbooks, roles and collections use their corresponding
reserved path segment. External content is relative to its source root without
that segment requirement; all paths remain contained.

The shared `source` union is:

- `path`: one absolute external content directory outside the input tree; or
- `git: {url, ref, subdir?, secretRef?}`.

A present source requires exactly one arm; empty blocks are invalid. Add-on
steps reject `source.git` because content belongs to the package. Validation
checks source and contained path spelling only.

A playbook target sets exactly one selection arm and an optional limit:

| Field | Shape | Rule |
| --- | --- | --- |
| `boundCluster` | `{}` | Machines of the binding's `ContainerCluster`. |
| `fromInput` | `{input: <name>}` | A declared `resourceRef` input of kind `StorageExport`, `StorageCluster`, `ContainerCluster`, or `Machine`. A `StorageExport` input also declares `storageExportAttachment`. |
| `static` | `{clusters?: [names], machines?: [names]}` | At least one list is non-empty. Cluster names resolve to `ContainerCluster` or `StorageCluster`; machine names resolve to SSH-accessible `Machine` objects. |
| `limit` | string | `firstReachable` by default, or `all`. |

The target is forbidden on a manifest-only step. No target selects the
controller or an ambient inventory group.

Each output is:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Unique provisioning token. |
| `file` | string | yes | — | Clean contained relative path. |
| `secret` | boolean | no | `false` | Marks sensitive output for a private consumer. |
| `format` | string | no | `text` | `text`, `json`, or `sha256`; `sha256` cannot be secret. |

Each step manifest is `{path, reclaimRendered?}`. Its `path` follows the
manifest-set path rules; `reclaimRendered` defaults false. Validation checks
only authored declaration relationships, without inspecting template tokens.

## ClusterAddonProfile

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.profileRefs` | array of strings | no | `[]` | Ordered `ClusterAddonProfile` refs, expanded first. |
| `spec.addonRefs` | array of strings | no | `[]` | Ordered `ClusterAddon` refs, appended after nested profiles. |

At least one list is non-empty. References resolve in their fixed namespaces
and profile cycles are rejected. Expansion is depth-first in authored order;
an add-on reached more than once is retained at its first occurrence and later
occurrences are dropped.

## ClusterAddonBinding

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.clusterRef` | string | yes | — | One `ContainerCluster`. |
| `spec.profileRefs` | array of strings | no | `[]` | Ordered profiles expanded first. |
| `spec.addonRefs` | array of strings | no | `[]` | Ordered direct add-ons appended afterward. |
| `spec.addonConfigs` | array of objects | no | `[]` | Set keyed by `addonRef`; supplies inputs but does not select an add-on. |

At least one profile or direct add-on is selected. Every config has:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `addonRef` | string | yes | — | A selected `ClusterAddon`. |
| `inputs` | array of objects | no | `[]` | Set keyed by input `name`. |
| `inputs[].name` | string | yes | — | Must be declared by the selected add-on. |
| `inputs[].value` | string | yes | — | Non-empty scalar resource or Secret name. |

For a `resourceRef` input, `value` resolves to the declared kind. For a
`secretRef` input, it resolves to `Secret`. Every required accepted input is
supplied exactly once and undeclared inputs are rejected.

Only one binding may apply a given add-on to a given cluster after profile
expansion. A selected add-on's required capabilities must be provided by
another selected add-on; the resulting `requires`/`provides` graph must be
acyclic. Config entries never select otherwise absent add-ons.

## Normalization and canonical state

Normalization materializes the table defaults for readiness timeout and OLM
subscription source, source namespace and install-plan approval.

The step timeout, target limit, and output format have effective defaults
`10m`, `firstReachable`, and `text` for their effectful consumers. They remain
absent in effective state when unauthored. Boolean zero values stay false.
Ordered arrays retain order; set-valued arrays and map keys canonicalize under
the common API rules.
