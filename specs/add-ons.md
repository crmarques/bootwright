# Add-ons

An add-on is a declarative extension bound to a `ContainerCluster` during
bootstrap. This page owns packages, catalog resolution, driver contracts and
qualification. [The add-on API](api/addons.md) owns descriptors, profiles,
bindings, inputs and their ordering. Accepting a declaration claims no
lifecycle support.

## Responsibility boundary

Add-ons resolves packages and drivers, expands bindings, and contributes typed
local actions, readiness, ownership and inverse requirements. It consumes the
published capabilities of Container cluster, Storage, Machine and Secrets;
it cannot bypass their invariants or invoke another add-on as a workflow.

[Architecture](architecture.md) owns component placement. Workspace owns
verified catalog roots, atomic snapshot publication, crash recovery, retention
and cleanup. [State reconciliation](state-reconciliation.md) owns the global
plan, block IDs, cross-context edges and durable transitions, and retains frozen
package content through completed destroy. Add-ons owns semantic validation,
not a shared writable store.

## Declarative user journey

Authors select add-ons directly or through profiles, bind them to a cluster,
and supply declared resource or Secret inputs. Bootwright resolves and freezes
packages, drivers, dependencies and target constraints for the complete
selected environment. There is no standalone add-on execution, force,
adoption or reclaim journey.

Authors cannot select executable code, drivers, inventory, privileges, retry
policy or lifecycle order. The [compiler boundary](api.md#compiler-boundary)
applies to declarations and provenance markers. `CustomPlaybook` is a separate
[reserved, non-executable surface](api/custom-playbooks.md).

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

The Environment's `.bootwright-addon` marker grants only descriptor selection.
It proves no signature, package version, manifest, compatibility or execution
authority; catalog acquisition must establish those separately.

## Catalogs and dynamic discovery

Built-in catalogs are embedded, locked and released with Bootwright. Custom
catalogs require an explicitly specified acquisition, immutable publication
and selection journey, including Workspace persistence. Both use the same
package and host contracts; origin grants no exemption or extra authority.

Search only explicitly selected snapshots, offline. Never discover through
working directories, `PATH`, environment variables, user configuration,
Ansible paths, network registries or global plugin locations. Acquisition must
verify source, publisher/trust root, authenticity, integrity and immutable
identity before publication. Desired state and package fields cannot select a
hidden catalog; discovery never fetches, installs or updates content.

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

Attempt results separate typed success/failure from effect state `completed`,
`no-effect` or `unknown`, and include retry boundaries and bounded safe
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
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary): a
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
[state reconciliation](state-reconciliation.md#plan-and-execution). Replay and
continuation retain the exact request, targets, content, dependencies and
confidential binding.

Destroy uses the completed apply snapshot, processes dependents before
providers and removes only positively identified Bootwright-owned resources.
Every effect requires an idempotence/replay boundary, observation, safe inverse
or compensation, and positive absence proof. Without a safe complete inverse,
a package is ineligible for managed lifecycle.

## GitOps handoff

Instances contribute safe completion, readiness, access and ownership evidence;
[state reconciliation](state-reconciliation.md#bootstrap-completion-and-gitops-readiness)
owns the complete-environment handoff gate. Evidence names proven capabilities,
never Secrets or vendor payloads. Handoff transfers no ownership: GitOps may
control downstream resources, but cannot mutate Bootwright-owned resources.
Ownership transfer needs a separate acceptance, continuation and destroy
contract; add-ons confer no adoption, drift-repair or GitOps publication role.

## Security and isolation

Apply [security.md](security.md) to every descriptor, catalog, package, native
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
