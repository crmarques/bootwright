# Add-ons design

C4 revives this design with the first built-in package and binding lifecycle,
C10 with custom-catalog acquisition and C11 with a declarative custom-package
lifecycle, each listed in the [backlog](../milestones/backlog.md#candidates).
Until then the [add-on boundary](../add-ons.md) and the
[add-on API](../api/addons.md) own what admission and the current code
enforce, and nothing here is implemented except the step shape recorded
under [Steps](#steps), which admission enforces.

## Responsibility boundary

Add-ons resolves packages and drivers, expands bindings, and contributes typed
local actions, readiness, ownership and inverse requirements.

[Architecture](../architecture.md) owns component placement. Workspace owns
verified catalog roots, atomic snapshot publication, crash recovery, retention
and cleanup. [State reconciliation](../state-reconciliation.md) owns the global
plan, block IDs, cross-context edges and durable transitions, and retains frozen
package content through completed destroy. Add-ons owns semantic validation,
not a shared writable store.

## Declarative user journey

Authors select add-ons directly or through profiles, bind them to a cluster,
and supply declared resource or Secret inputs. Bootwright resolves and freezes
packages, drivers, dependencies and target constraints for the complete
selected environment. There is no standalone add-on execution, force,
adoption or reclaim journey.

## Package standard

| Term | Meaning |
| --- | --- |
| Definition | One normalized `ClusterAddon` object. |
| Package | Its definition and every referenced asset and metadata file. |
| Catalog record | Trusted host metadata binding package identity, origin, compatibility, content and eligible driver; never desired state. |
| Driver | Bootwright-owned implementation for one closed capability and target range. |
| Resolved add-on | One package and driver bound to a cluster, normalized inputs and immutable referenced-resource snapshots. |
| Instance | Lifecycle identity scoped by target cluster and add-on name. |

A package root contains exactly one `add-on.yaml`, holding one `ClusterAddon`
whose name matches the catalog record. Referenced assets remain beneath that
root in the API's `manifests`, `playbooks`, `roles` or `collections` path
classes. Unused classes may be absent; additional descriptors, hooks, hidden
dependency roots and alternate entrypoints are forbidden. Code-bearing assets
may remain declaration data but make a package lifecycle-ineligible.

Package formats and catalog records name exact revisions. Package format,
desired-state API and host interface are independent compatibility identities;
none has an implicit revision or nearest-version fallback. Before exposing a
catalog journey, define its closed record schema, canonical encoding and digest
algorithm, fixed bounds, selection and collision rules, diagnostics, and
conformance vectors.

An executable package freezes:

- immutable package ID and exact version;
- descriptor API version, kind, name and canonical normalized digest;
- canonical manifest of every selected regular payload file and content digest;
- catalog origin and immutable snapshot identity;
- driver capability, implementation identity, version and digest;
- exact target-product/version constraints; and
- every consumed native tool, collection, image, chart and other dependency pin.

One package ID/version must always identify the same descriptor and payload
bytes; reuse for different content is corruption or tampering. Secret values
and secret-derived digests never enter package identity; confidential bindings
follow state reconciliation.

Asset paths are clean and relative. Open them beneath a held package-root
handle and verify the frozen manifest against the opened content. Assets must
be bounded, stable regular files on the permitted device: reject symbolic
links, hard-linked aliases, special
files, mount crossings and concurrent replacement. Freeze content before
planning effects. External step `source.path` remains valid declaration data
but lifecycle-ineligible; live external directories are never lifecycle input.
The API rejects add-on `source.git`.

## Catalogs and dynamic discovery

Built-in catalogs are embedded, locked and released with Bootwright. Custom
catalogs require an explicitly specified acquisition, immutable publication
and selection journey, including Workspace persistence. Both use the same
package and host contracts; origin grants no exemption or extra authority.

Search only explicitly selected snapshots, offline. Acquisition must
verify source, publisher/trust root, authenticity, integrity and immutable
identity before publication. Discovery never fetches, installs or updates content.

Enumeration and composition are canonical and bounded. Package ID/version
pairs are unique; records for an add-on may coexist only with non-overlapping
target/version constraints. Duplicate definitions, aliases, case collisions,
shadowing and contradictory metadata fail before payload access or operation
registration. There is no precedence or last-writer rule, and unselected
packages are not opened.

For each API-selected normalized descriptor, resolution requires exactly one
matching package and one driver. API version, kind, name and canonical
descriptor digest must match the package exactly. No match or ambiguity fails
before planning. Freeze package, catalog, driver, dependencies and target
constraints in the operation; apply, observation, continuation and destroy
never reload or re-resolve them. Drift causes safe refusal.

## Versioned host interface

Each catalog record and driver names one exact host-interface revision and
supported package/API/driver/target combinations. The first executable journey
must define closed request, result, failure and evidence schemas and
conformance vectors before assigning a public revision. Changes to meaning,
effects, evidence or compatibility require a deliberate owning-contract version
and migration decision; compatibility is never inferred.

The host resolver selects the package and driver from trusted metadata before
invocation. Drivers do not receive the registry or select themselves. They
implement these logical conversations, independent of implementation language:

| Operation | Request and result | Allowed effect |
| --- | --- | --- |
| `CompileApply` | Resolved add-on, frozen non-secret content, declared target facts and prerequisite evidence references → deterministic local action set. | None; no Secret, process, network, live target or persistent-state access. |
| `Observe` | Frozen action, observation purpose, intended identity and positive live identity/access evidence when the prerequisite exists → ready/complete, absent/no-effect, owned incomplete, foreign/conflicting or unknown, with positive evidence where claimed. | Only the purpose's read-only observation. |
| `ApplyAction` | Authorized frozen action, exact prerequisite-proven live targets and bounded confidential handles → typed attempt result. | Only the allowlisted action. |
| `CompileDestroy` | Completed apply snapshot, ownership/completion evidence and identical frozen package/driver → deterministic reverse actions and absence requirements. | None; pure and read-only. |
| `DestroyAction` | Authorized frozen reverse action and exact owned live targets → typed removal/absence result. | Only the allowlisted inverse. |

Effect and observation requests carry host-owned attempt/resolution identity,
logging boundary, deadline and cancellation. A resolved add-on carries instance
identity, normalized non-secret bindings and safe provenance, declared cluster,
storage and Machine identities, prerequisite identity/readiness references,
capability dependencies, package/driver/dependency identities and compatibility
decision. It contains only Secret references or confidential binding IDs.

Local action sets define stable keys/descriptions, order, requirements,
impacts, effect/observation kinds, intended targets, timeouts and bounds, replay
keys, completion/readiness conditions, ownership and inverses. State
reconciliation supplies global IDs and cross-context edges. Actions carry no
executable names, shell text, unreviewed arguments, Secret values or
package-selected privilege.

Attempt results separate typed success/failure from the effect state their
[attempt outcome](../state-reconciliation.md#attempt-outcomes) records, and include retry boundaries and bounded safe
structured evidence/diagnostics. Direct `no-effect` is a failure; an
already-ready apply or already-absent destroy is `completed` only with positive
evidence. Exit status, prose, presence or elapsed time alone proves no
completion. State reconciliation owns the resulting durable transition.

## Closed execution vocabulary

Drivers may project only qualified API-declared capabilities:

- OLM namespace, OperatorGroup, CatalogSource, Subscription and typed resources;
- package-owned Kubernetes/OpenShift manifest sets and manifest-only steps;
- registered typed domain effects, such as `StorageExport` attachment; and
- readiness observations of named resources and conditions.

Packages cannot supply playbooks, roles, collection plugins, Go plugins,
shared libraries, executables, shell fragments or new effect kinds for
execution. A `playbook`, `rolesPath` or `collectionsPath` declaration makes a
package lifecycle-ineligible regardless of origin. Remote effects use the
[Go/Ansible boundary](../architecture.md#go-and-ansible-responsibility-boundary): a
qualified driver selects a pinned Bootwright-owned `bootwright.core`
entrypoint and its vetted, locked dependencies by allowlisted FQCN or fixed
canonical reference. Package data cannot supply or select that code.

A consumer-owned typed port binds the qualified native API, module, installer
or allowlisted CLI for the target/release. Package data cannot expand arguments,
override configuration, redirect endpoints, introduce a shell, or request
ambient credentials or privilege. Native output remains untrusted until the
driver validates and normalizes it.

Catalog acquisition or built-in release assembly must resolve, authenticate
and pin every consumed artifact before lifecycle resolution. A mutable OLM
channel, image tag, Git ref, chart version or collection name alone is
insufficient. Planning and execution never discover, fetch, install or update
dependencies.
Unsupported declarations, including global pull-secret merges or resources
without a proven ownership-aware inverse, fail before planning; no selected
step is silently dropped or reported as partially supported.

## Lifecycle and ordering

Preserve the API's resolved profile/binding order as the stable tie-break;
dependency edges take precedence. Missing requirements, cycles, unsupported
target versions, conflicting effects or ambiguous ownership fail before
operation registration.

Each instance requires positive target-cluster readiness and every consumed
domain capability. Storage integration additionally requires positive storage
readiness and the exact export identity. These are global plan prerequisites;
an add-on cannot probe ahead or hide them in automation.

Execute one state-authorized frozen action at a time. Completion requires all
effects, readiness and ownership evidence to be positive and durable. Failure,
cancellation, lost response and malformed or contradictory evidence follow
[state reconciliation](../state-reconciliation.md#plan-and-execution). Replay and
continuation retain the exact request, targets, content, dependencies and
confidential binding.

Destroy uses the completed apply snapshot, processes dependents before
providers and removes only positively identified Bootwright-owned resources.
Every effect requires an idempotence/replay boundary, observation, safe inverse
or compensation, and positive absence proof. Without a safe complete inverse,
a package is ineligible for managed lifecycle.

## GitOps handoff

Instances contribute safe completion, readiness, access and ownership evidence;
[state reconciliation](../state-reconciliation.md#bootstrap-completion-and-gitops-readiness)
owns the complete-environment handoff gate. Evidence names proven capabilities,
never Secrets or vendor payloads. Handoff transfers no ownership: GitOps may
control downstream resources, but cannot mutate Bootwright-owned resources.
Ownership transfer needs a separate acceptance, continuation and destroy
contract; add-ons confer no adoption, drift-repair or GitOps publication role.

## Security and isolation

Apply [security.md](../security.md) to every descriptor, catalog, package, native
output and live observation, including built-in content. Before an effect:

- verify authenticity/integrity against separately trusted metadata and the
  frozen manifest;
- enforce filesystem, parser, bytes/depth/files/targets, runtime, retry,
  concurrency and output bounds;
- validate payload documents against the pinned target schema and reject
  secret-bearing package content;
- resolve each object, endpoint, host, Secret, privilege and effect to its exact
  authorized target; and
- use the least-capable qualified driver and prove state-owned authorization,
  identity, ownership, logging and replay prerequisites.

Secrets reach only declared typed driver fields through confidential bindings;
a package cannot enumerate or request undeclared Secrets. The isolated Ansible
adapter cannot target the controller, widen inventory or privilege, access the
catalog, install dependencies or perform unregistered effects.

## Compatibility, support, and conformance

Support names exact package/catalog origins and identities, host-contract and
driver identities/versions, target products/releases/architectures, operations,
and pinned execution dependencies. Schema acceptance alone claims no support.

Every built-in and custom package passes the same qualification suite:

- bounded deterministic discovery, collision refusal, exact identity freezing
  and continuation drift refusal;
- strict descriptor, payload, input, target, dependency and effect validation;
- byte-stable planning across equivalent inputs, with no writes, processes,
  network/target access or Secret reads in resolution and planning;
- apply/readiness, state-owned handoff evidence, reverse-order destroy and
  positive absence;
- failure, cancellation, timeout, partial progress, lost response, retry,
  observation, unknown outcomes and crash continuation at every effect edge;
- traversal/replacement, dependency/code/argument/inventory/endpoint/privilege
  substitution, disclosure and resource-exhaustion attacks, including zero
  process, target or network effects for package-carried executable content; and
- real-system acceptance for every supported product/release, alongside
  hermetic driver and host-contract tests.

Only passing packages enter the supported matrix. Unqualified packages may
remain valid API declarations, but lifecycle operations refuse them before
payload execution or platform mutation.

## Frozen add-on blocks

Add-on blocks also freeze the package, catalog snapshot, payload manifest,
driver, and compatibility decision under the package standard above. They obey
the same state machine and never re-resolve on continuation or destroy.
Retain the selected non-secret package bytes in the operation snapshot through
completed destroy, independent of source catalog availability.

## Steps

This section is the exception to the page's status: admission enforces the
closed step shape and references it records, while no lifecycle, renderer or
command reads a step, as the [add-on API](../api/addons.md#steps) states, so
its defaults and step semantics are unimplemented.

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
| `fromInput` | `{input: <name>}` | A declared `resourceKind` input of kind `StorageExport`, `StorageCluster`, `ContainerCluster`, or `Machine`. A `StorageExport` input also declares `storageExportAttachment`. |
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

The step timeout, target limit, and output format have effective defaults
`10m`, `firstReachable`, and `text` for their effectful consumers. They remain
absent in effective state when unauthored.

## Reserved diagnostic code

`addon.catalog`, for an invalid add-on name, version, registration or catalog
identity, joins the
[diagnostic taxonomy](../cli/output.md#diagnostic-taxonomy-and-order) with its
first emission.
