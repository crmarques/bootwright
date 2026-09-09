# Architecture

This file owns Bootwright's high-level structure, bounded-context map,
dependency direction, dependency selection, and Go-to-Ansible boundary. It
defines where behavior belongs; the owning domain specifications define that
behavior. A component or path named here is a placement contract, not a claim
that implementation already exists. Create it only with complete, authorized
behavior that needs it.

Bootwright uses domain-driven design with ports and adapters. Go is the product
control plane. Ansible and native tools are implementations at controlled
effect boundaries.

## Mandatory component boundaries

Every policy-bearing component is domain-specific. It is one bounded context
with a cohesive vocabulary, constraint set, change reason, and semantic owner.
Split behavior when its actors, invariants, volatility, security boundary,
state owner, or failure semantics differ.

Within a bounded context:

- domain types and services own invariants and deterministic decisions;
- application services sequence complete use cases without absorbing another
  context's policy;
- driving adapters translate external requests and present application
  results;
- driven adapters implement filesystem, process, persistence, native-tool, or
  remote effects behind consumer-defined ports; and
- the composition root selects and injects concrete implementations.

Presentation, composition, repository checks, and narrow policy-free technical
libraries are not domain components. They may exist only with a named consumer
and must not become alternate homes for business rules. Generic policy buckets
such as `common`, `utils`, `models`, `services`, `manager`, global `state`,
global `render`, or global `workflow` are forbidden when they lack one semantic
owner. Reuse follows demonstrated identical semantics; it is not a reason to
merge contexts or create an abstraction before its first consumer.

## Bounded contexts

| Context | Owns | Does not own |
| --- | --- | --- |
| Desired state | Document discovery and provenance, strict decoding, API-to-domain translation, normalized catalog assembly, deterministic diagnostics, and canonical effective-state encoding. | Domain invariants belonging to the referenced platform contexts or any platform effect. |
| Environment | Environment selection, shared defaults, complete graph closure, and cross-context reference semantics. | Policy internal to a selected machine, provider, cluster, storage service, or add-on. |
| Workspace | Named root-owned contexts, separate Context configuration, per-user selection, and persistence boundaries for local runtime data. | Lifecycle policy, secret meaning, or arbitrary user filesystem content. |
| Controller | Local controller prerequisite inspection, setup, invoking-account verification, and sudo process supervision. | Managed-machine provisioning, cluster readiness, or lifecycle ordering. |
| Secrets | Secret custody and materialization, immutable secret binding, and disclosure classification. | A consuming context's authorization or business decision. Entitlement semantics remain with the product context that consumes them. |
| Trust | SSH host identity, TLS trust, trust decisions, and durable trust evidence. | Secret custody, endpoint business policy, or ambient trust configuration. |
| Substrate | Provider capabilities, provider identity, network attachments, machine infrastructure realization, and normalized power and identity operations. | Machine OS policy, cluster installation, or lifecycle ordering. |
| Machine | Machine identity, addresses, access posture, declared capabilities, and the relationship to a substrate and operating-system intent. | Provider mechanics, OS installer mechanics, or cluster membership policy. |
| Managed OS | Machine images, install profiles, managed-OS entitlement use, installation intent, and OS completion evidence. | Substrate realization, storage-product entitlement policy, or higher-level cluster and storage orchestration. |
| Infrastructure services | Shared placement, identity, endpoint, and ownership rules for artifact servers, load balancers, proxies, name resolution, time synchronization, and registries. Each service capability owns its specific policy and adapter. | Consumer cluster policy or one generic service workflow. |
| Container cluster | OpenShift and OKD release, topology, installer intent, node binding, cluster access, and installation evidence. | Machine, substrate, shared-service, storage, or add-on ownership. |
| Storage | Ceph cluster topology, placement, pools, filesystems, gateways, NFS services, exports, and storage completion evidence. | Machine OS installation or downstream cluster-add-on policy. |
| Add-ons | The [add-on contract](add-ons.md): catalog packages, profiles, bindings, input expansion, package and driver resolution, ordering within the add-on boundary, cluster application, readiness, and add-on records. | Cluster installation, storage invariants, complete-environment state transitions, or a general workflow language. |
| Native artifacts | Typed artifact manifests, canonical bytes, destination policy, and safe publication. The platform context that understands a native format owns its projection into that format. | Starting, adopting, or simulating a managed operation when an operator executes an artifact independently. |
| State reconciliation | Immutable plans, operation state, scheduling, leases, continuation, ownership evidence, and transition authorization for the complete environment. | Domain capability mechanics or vendor-specific effects. |

Security constrains every context and effect boundary; it is not a component to
which other contexts delegate safety. Public API kinds are boundary contracts,
not a shared mutable domain model.

## Dependency direction and communication

Dependencies point toward stable domain and application policy:

```text
external request
  -> driving adapter
  -> application service
  -> domain policy
  -> consumer-owned port
  <- driven adapter
  <- external system
```

Domain code does not depend on CLI frameworks, Ansible, vendor SDKs,
persistence, filesystems, processes, networks, clocks, randomness, or
presentation. Adapters may depend on application request and result types and
domain vocabulary; domain and application packages never depend on a concrete
adapter. Bind implementations only at the composition root.

Across bounded contexts, identify the producer and consumer, dependency
direction, exchanged contract, semantic owner, data and state owner, and
translation location. An application service may coordinate contexts, but it
must call each through its published capability and must not reproduce its
rules. An adapter never calls another adapter to bypass that service.

Every replaceable service, repository, resolver, and effect implementation is
consumed through an injected contract, including dependencies between components
in the same package. Consumers neither construct another implementation nor
assert its concrete type. Constructors may return concrete implementations;
only composition binds them to their consumers. Renaming an implementation
from `Service` to `Compiler`, `Access`, or another name does not change this rule.

Each consuming package owns the smallest useful interface. In Go this is a
typed interface or typed function capability located with its consumer, not in
a provider or generic `ports` package. A function capability is appropriate for
one operation; cohesive operations may share an interface. Immutable values,
value constructors, and pure domain functions with shared semantics can remain
concrete; they do not discover or bind replaceable services. A result value must
be constructible by every conforming implementation without invoking the default
implementation, and must preserve its immutability when constructed that way.
The contract's immutable request and result use domain terms rather than
vendor, transport, inventory, or persistence shapes. Copy mutable collections
at boundaries and propagate `context.Context` through cancellable Go calls.
The contract defines applicable success, typed failure, cancellation, replay
or idempotence, ownership, and completion-evidence semantics. Reuse a port only
when every implementation preserves those semantics; otherwise split the
capability.

Languages without Go-style interfaces use an equivalent explicit contract.
For Ansible, that contract is a named capability with a versioned input, result,
failure, and evidence schema. Composition binds it to an allowlisted playbook
entrypoint and fixed role/plugin references. Shared contract tests qualify each
implementation; callers depend on the capability contract, not private tasks,
variables, or implementation-specific result shapes.

No context shares a writable model or store with another. One component owns
each mutable datum and consistency boundary. Crossing that boundary requires a
value snapshot or explicit operation, with partial failure, retry,
compensation, crash, and continuation behavior represented rather than hidden.

## Dependency selection and reuse

Before adding or replacing a dependency, search for suitable options and choose
one that is well-known, trusted, and fits the task. Prefer the standard library
or dependencies already in use when suitable.

## Product flow

The [API contract](api.md#yaml-streams-and-decoding) owns compilation phase
order: safe discovery and selection, strict decoding, validation,
normalization, reference resolution, and a completed immutable environment
model. That model supplies validation, canonical effective state, domain-owned
native projections, and transition planning. Stages do not mutate prior
representations. Independent valid input continues far enough to collect
non-cascading diagnostics; errors prevent a final model and dependent output.
Native-artifact generation never starts or simulates a managed operation.

## Repository layout

Always organize code and scripts into packages, directories and files by
cohesive responsibility. Use descriptive domain, capability or task names so
newcomers can find a behavior's implementation from its path and filename and
follow its entrypoint to the owning logic. Keep related logic together,
separate unrelated responsibilities, use consistent layouts for comparable
capabilities, and avoid unnecessary nesting.

The following paths have stable ownership. A directory is added when its first
owned behavior or asset is added.

| Path | Responsibility |
| --- | --- |
| `specs/` | Authoritative product behavior, public contracts, architecture, and safety invariants. |
| `.agents/` | Agent workflow, engineering skills, and indexed observed knowledge; it does not define product behavior. |
| `docs/` | Human-oriented explanation and workflows that link to, but do not replace, specifications. |
| `examples/` | Synthetic, safe-to-commit desired-state examples conforming to the public API. |
| `api/<version>/` | Public desired-state wire types and schema vocabulary for one API version. |
| `cmd/bootwright/` | Executable composition root and process boundary. |
| `internal/<context>/` | Private Go domain, application, and adapter packages grouped by bounded context. |
| `ansible/` | Controlled Ansible configuration, dependency locks, and embedded remote adapter collection. |
| `add-ons/` | Built-in packages conforming to [the add-on contract](add-ons.md), owned by the Add-ons context and embedded through a narrow package. |
| `scripts/` | Build, generation, packaging, and verification tooling; never product policy. |
| `test/` | Cross-component and end-to-end fixtures and harnesses; package tests remain beside their Go packages. |

Versioned content, examples, fixtures, Git metadata, and documentation contain
no private environment, identity, topology, credential, or secret material.

## Self-explanatory code and retained knowledge

Make code explain its behavior through precise domain names, explicit types,
small cohesive functions, straightforward control flow, and meaningful test
names. Apply this to production code, tests, scripts, configuration, and code
examples. When code needs an explanation, first improve its names or structure;
do not compensate for unclear code with prose or introduce unnecessary
abstractions solely to eliminate a comment.

Minimize comments. Omit narration of what code does, redundant declaration or
package summaries, section labels, commented-out code, and change diaries.
An exported Go name alone is not a reason to add a comment. Retain only comments
required by language, tooling, a maintained documentation contract, or legal
obligations, such as build/embed directives, shebangs, generator markers, and
license notices. Keep those comments minimal and correctly attached; preserve
comments that are intentional fixture data. Change generated or vendored
content through its owning source or dependency workflow.

Preserve a finding only when it can change a future implementation, review, or
diagnosis. Record observed constraints, non-obvious implementation rationale,
and dependency workarounds in the searchable
[knowledge catalog](../.agents/knowledge/index.md), with links to the affected
code and verification evidence. Required behavior and its rationale remain in
the owning spec; deferred work remains in [milestones](milestones.md). Knowledge
links to those owners instead of duplicating their contracts. Do not move
obvious code narration into knowledge merely to save every removed comment.

During review, examine existing comments in the affected scope. Remove
redundancy, improve unclear code without changing its behavior, and preserve
useful findings in the catalog before removing their explanation from source.
Keep the catalog's paths, symbols, evidence, and applicability current when
changing the associated implementation.

## Go package structure

`api/v1alpha1` owns public wire types and vocabulary. It is I/O-free and does
not contain orchestration, persistence, vendor logic, or CLI presentation.
Internal contexts translate boundary values when their invariants require a
domain model; they do not mechanically duplicate the API or pass API objects
around as shared mutable state.

Use domain-first capability packages:

| Package | Owns |
| --- | --- |
| `internal/<context>` | Pure domain values, invariants, and published value contracts. |
| `internal/<context>/<capability>` | Application services, command requests, and interfaces consumed by the capability. |
| Named implementation packages such as `localstore`, `inputfs`, and `ansible` | Effects and translation behind the consuming capability's interface. |

Each application capability exposes a concrete `Service`. Dependencies are
private fields; add constructors when there are actual dependencies to inject.
Keep requests with their capability and shared domain values at the context
root, including `machine.SSHOptions` and `secrets.Source`. Add domain files and
implementation packages with their first authorized behavior. Do not create
empty packages or repeat generic `domain`, `application`, `ports`, and
`adapters` layers inside every context.

`cmd/bootwright` is the only composition root:

| File | Responsibility |
| --- | --- |
| `main.go` | Process entrypoint and exit; linker-injected `version`, `commit`, and `dependencyBundle` variables. |
| `run.go` | Assemble the process context, arguments, streams, and build information into the CLI invocation. |
| `wiring.go` | Construct concrete services and return the statically typed `cli.Services` bundle from `wireServices()`. |

Wiring constructs adapters and injects them into their consumers as capabilities
are implemented. It performs no context discovery, application work, domain
decision, or presentation. The CLI collects explicit input; `cmd/bootwright`
constructs the complete graph of available implementations before dispatch.
Loading here means explicit construction and injection, not runtime code loading.
When selection depends on validated context configuration, desired state, or
persisted identity, composition injects an immutable resolver containing the
available implementations. Application policy supplies semantic selection
criteria through that resolver's consumer-owned port; it never imports,
constructs, or switches on a concrete implementation. Context lookup still
occurs only in the authorized use case, preserving effect-free help, completion,
version, and invalid usage.

`internal/cli` is the driving adapter backed by the selected framework. It owns
the public command catalog, framework configuration, contract-specific
validation, help content and templates, prompts, presentation, diagnostics,
output-mode selection, and exit-code mapping. It owns the consumer interface
through which a command invokes an application use case. Each
`commands_<context>.go` keeps its command declarations, consumer interfaces,
and request translation together. `catalog.go` assembles those fragments into
the public tree and order, including shared groups such as `cluster`,
`preflight`, and `render`; every command has one declaration. `services.go`
holds the typed dependency bundle. Parsing, help, completion, output, and static
dispatch remain shared. CLI code never constructs a driven adapter or
sequences a cross-domain workflow.

Application services receive complete validated immutable requests and return
presentation-independent results. Domain decisions are pure. Filesystem,
process, persistence, network, clock, randomness, and remote access occur only
in injected adapters. Package-global mutable flags, streams, registries,
clocks, configuration, and service instances are forbidden.

The CLI has consumer-owned interfaces and typed requests for all 49
application commands. M1b's explicit-input validation service returns a typed
report or admission failure. Its composition binds the safe input reader,
YAML parser and pure component-owned rules. Other capability-owned stub
methods return `internal/availability.ErrNotImplemented`, or the caller's
context cancellation error, without I/O. They expose no successful result
schema until the owning use case is implemented. The CLI alone renders
results and unavailable messages; package structure does not advance
[delivery scope](milestones.md).

Repository-fitness packages may inspect source and assets but contain no
production behavior. They enforce dependency direction and mapping rules
rather than becoming a runtime dependency.

### Application service ownership

Every package below is under `internal/` and exposes `Service`. The table
assigns application ownership; implementations remain limited to the skeleton
until their [milestone](milestones.md) is authorized. Detailed domain behavior
remains with the owning specifications.

| Capability package | `cli.Services` field | Responsibility |
| --- | --- | --- |
| `workspace/contexts` | `Contexts` | Context configuration, initialization, input update, user selection, inspection, permanent deletion, identity, and immutable input publication. |
| `desiredstate/compilation` | `DesiredState` | Coordinate validation and effective-state rendering around deterministic compilation. |
| `environment/preflight` | `EnvironmentPreflight` | Infrastructure, selected-cluster, and whole-environment prerequisite checks. |
| `environment/inspection` | `EnvironmentInspection` | Cluster listing and information across platform contexts. |
| `environment/access` | `EnvironmentAccess` | Cluster/node selection for shell and command access. |
| `controller/prerequisites` | `Controller` | Controller prerequisite inspection and setup. |
| `secrets/custody` | `Secrets` | Secret management, immutable bindings, materialization, and disclosure. |
| `secrets/encryption` | `Encryption` | Encryption initialization, status, and rotation. |
| `trust/enrollment` | `MachineTrust` | Machine identity trust enrollment and its evidence. |
| `machine/inventory` | `MachineInventory` | Machine inventory presentation data. |
| `machine/access` | `MachineAccess` | Explicit machine access descriptors. |
| `managedos/media` | `Media` | Installer media management. |
| `containercluster/preflight` | `ContainerPreflight` | Container-cluster prerequisites. |
| `containercluster/installation` | `Installer` | Installer intent and native projection. |
| `containercluster/access` | `ClusterAccess` | `oc`, `kubectl`, and kubeconfig access. |
| `storage/preflight` | `StoragePreflight` | Storage prerequisites. |
| `storage/rendering` | `StorageArtifacts` | Ceph-native projection. |
| `addons/catalog` | `AddOnCatalog` | Add-on catalog registrations. |
| `addons/preflight` | `AddOnPreflight` | Add-on prerequisites. |
| `nativeartifacts/rendering` | `Artifacts` | Whole-render coordination and safe publication. |
| `reconciliation/lifecycle` | `Lifecycle` | Plans, transitions, scheduling, authorization, continuation, and complete-environment readiness. |

Environment's three preflight methods belong to `preflight`, its two inspection
methods to `inspection`, and its two access methods to `access`. Platform rules
remain with their owning contexts. Substrate realization and normalized
identity/power operations, managed-OS installation, and infrastructure-service
provisioning and readiness gain separate capabilities when implemented.

### Service interactions

M1b implements `compilation.Compiler`, immutable values and reports,
`contexts.Inputs`, the Workspace repository and the local Reconciliation
mutation guard. Lifecycle execution and native artifact publication retain
their later milestone scope. Durable context publication follows
[the context contract](contexts.md).

#### Context admission and compilation

Separate context management, immutable input reading, and compilation of an
explicit input universe:

```text
context init/update -> contexts.Service -> configuration reader/validator
                                       -> compilation.Compiler (explicit input only)
                                       -> Workspace repository
                                       -> transaction-scoped secret initializer
                                       -> invoking-user selection store

validate/render effective -> compilation.Service -> contexts.Inputs
                                                 -> compilation.Compiler
contexts.Inputs -> Workspace repository (read only)
```

`contexts.Service` owns context publication and consumes its own `Compiler`
interface and `Repository` interface. `compilation.Service` consumes a narrow
`ContextInputs` interface for existing named/current context input.
`contexts.Inputs` implements that reading capability through a narrow
repository interface of its own. Input values are immutable views, separate
from writable context records. Context setup is parsed independently of the
Environment graph; empty desired state is a valid initialized context.

The composition root binds `workspace/contextfs` to the fixed privileged store
and `workspace/selectionfs` to a lazy verified invoking account. The latter
accesses the user file with that account's credentials, including a bounded
credential-dropped subprocess when the caller is root. `controller/invocation`
owns local account lookup and invocation-scoped sudo process supervision.
Only validated, available commands needing stored context data acquire that
privilege boundary; informational and explicit-input validation paths remain
effect-free. [CLI invocation](cli.md#local-privilege-and-user-identity) owns
classification and refresh policy.

Context initialization receives a transaction-scoped secret-area capability
and initializer callback. It initializes the configured implementation before
readiness publication without reacquiring the Workspace lock. The immutable
implementation resolver validates configuration before any store mutation.
Encryption commands use that Context configuration independently of input.

`compilation.Compiler` accepts explicit input independently of context
selection; it never calls context management. Input lookup never calls the
compiler. This separates context admission's compilation dependency from
command-facing compilation's input lookup without a package or service cycle.
Compilation translates decoded values into Environment-owned inputs for pure
selection and graph closure. Its `GraphSelector` coordinates injected add-on
attachment and Environment selection functions and translates their results;
the composition root only binds those functions. Environment's domain rules
never import the desired-state aggregate. Platform-owned invariants remain with Machine,
Container cluster, Storage, and the other referenced domains; compilation phase
and diagnostic semantics remain in [the API contract](api.md).

#### Environment inspection and access

Environment preflight consumes domain-owned prerequisite capabilities.
Inspection combines validated input and permitted local evidence. Environment
access selects the declared cluster and node before delegating platform access
decisions. Machine owns SSH descriptor construction; Container cluster owns
`oc`, `kubectl`, and kubeconfig behavior. These calls preserve the descriptor
and export boundaries defined by
[the access contract](cli.md#resource-inspection-and-explicit-access).

#### Lifecycle and state ownership

`lifecycle.Service` consumes narrow interfaces for Workspace context identity
and immutable input, Desired state compilation, Secrets bindings and controlled
materialization, domain-owned platform capabilities, and Reconciliation-owned
operation persistence and leases. Reconciliation owns cross-domain ordering;
a cluster service cannot schedule storage or OS installation independently.

Platform services own the meaning of completion evidence and return it through
typed results. Adapters return bounded results to their application service.
Reconciliation owns operation transitions and their durable records under
[the lifecycle contract](state-reconciliation.md).

Context update and deletion consume a separate Reconciliation-owned
`ContextMutationGuard` capability through an interface owned by the consuming
Workspace package. The guard participates in the mutation and locking
boundary; a stale Boolean check cannot authorize a later mutation. Lifecycle
reads Workspace inputs through the independent input capability, avoiding a
dependency cycle with context management. The owning lifecycle and context
contracts determine which mutations are permitted.

#### Native projection and publication

```text
whole-render service -> platform-owned projections
                     -> immutable artifact manifests
                     -> publication capability -> filesystem adapter
```

Container cluster and Storage own their native formats and projections.
The `nativeartifacts` root owns shared artifact-manifest values; a separate
publication capability owns destination and publication behavior. Platform
render commands may consume publication directly without calling the
whole-render coordinator. Artifact generation creates no lifecycle operation.

Define successful result schemas and concrete effect interfaces with the first
implemented use case, following the
[communication contract](#dependency-direction-and-communication).

## Go and Ansible responsibility boundary

Go owns:

- public-request translation and product diagnostics;
- desired-state semantics, normalization, validation, and graph closure;
- domain services and application-use-case orchestration;
- deterministic native projection and implementation selection;
- immutable planning, ordering, authorization, operation state,
  continuation, and user-facing results; and
- translation of one authorized capability request into one bounded adapter
  call.

Ansible owns Bootwright-controlled interaction with managed remote components.
Remote observation, access, configuration, installation, verification, and
removal cross an Ansible adapter boundary. Ansible does not infer desired
state, choose product workflow, order cross-domain work, grant authorization,
own operation state, or format product output.

An Ansible adapter receives one frozen validated request and returns a bounded
structured result with required evidence. A local native tool may run
through a typed Go runner for a purely local transformation; a tool contacting
a managed remote component belongs inside Ansible.

Operator-supplied automation and add-on package content are not internal
adapters. A schema declaration or package origin grants no execution authority.
[The add-on host contract](add-ons.md#versioned-host-interface) permits a
qualified Bootwright-owned driver to enter only a pinned embedded adapter;
package-carried executable content remains ineligible.

## Ansible collection structure

Bootwright-owned content lives in one embedded `bootwright.core` collection
under `ansible/collections/ansible_collections/bootwright/core/`. Add only
needed paths:

| Path | Responsibility |
| --- | --- |
| `ansible/ansible.cfg` | Controlled configuration. |
| `ansible/controller/` and `ansible/collections/` | Selected dependency inputs and exact locks for the complete runtime closure. |
| Collection `playbooks/<domain>/<operation>.yml` | One fixed application-port entrypoint; private fragments under its `tasks/`. |
| Collection `roles/<domain>_<capability>[_<implementation>]/` | One remote capability adapter with only applicable standard role directories. |
| Collection `plugins/` | Capability adapters or effect-free product translation/evidence normalization. |
| Collection `tests/unit/` | Collection-owned tests. |

The first consumer selects package-native or standard lock formats; Bootwright
does not invent a dependency resolver or lock format. Locks cover
`ansible-core`, collection/role artifacts, controller Python and SDKs,
execution-environment images, and native tools. Every FQCN resolves to that
exact closure. A standalone role also requires a canonical immutable source,
complete lock, supported resolver, and fixed non-ambient reference. External
content remains a dependency, not Bootwright-owned code. A second Bootwright
collection needs a distinct ownership, dependency, and release boundary.

### Playbooks

Each thin entrypoint validates one port request, targets an explicit host
group, composes allowlisted FQCNs or locked canonical role references, and
normalizes one result/evidence set. The domain directory identifies its owner;
the operation matches the Go capability. Playbooks do not choose workflows or
providers from authored data or ambient facts, dynamically select another
playbook, or coordinate independent domain operations.

### Roles

A role validates and translates its frozen request, composes selected
dependencies, constrains effects, and normalizes results. It declares
namespaced inputs/defaults/outputs, privilege, effects, replay, and sensitive
values. It accepts no authored role names, arbitrary task lists, ambient
inventory, or uncontrolled variables. Split private task files by concern;
keep configuration bodies in files/templates. Native invocations have explicit
arguments, change/failure semantics, bounds, and secret handling.

Implementation variants may share a port only when success, failure, replay,
ownership, and evidence semantics agree. A role never calls an unrelated domain
role to hide orchestration.

The selected playbook is the Ansible composition boundary. It binds capability
dependencies with explicit `ansible.builtin.import_role` or
`ansible.builtin.include_role` calls using fixed qualified role names, and fixed
module/action FQCNs. A role's private task includes may split its own
implementation; they are not public capability interfaces. A replaceable role
or plugin dependency must have its own declared request/result contract and be
bound by the selected entrypoint, rather than discovered by a consumer role.
Implementation changes affect these bindings and their locks, not consumers.
Any required variant dispatch is an explicit allowlisted binding frozen by Go;
authored variables, gathered facts, and arbitrary role/task/plugin names cannot
select executable code. Do not use implicit role dependencies or shared mutable
facts to communicate across capability boundaries.

### Ansible collection plugins and results

A Bootwright module or action validates a frozen request, invokes the required
API, and normalizes its result within the consumer's effect and security
boundary. Other plugins provide effect-free product translation or evidence
normalization. Plugins contain no workflow or authorization policy and are
tested with their consumer.

Playbook stdout is not a product API. Go consumes the structured adapter result
and presents product output under [cli.md](cli.md); external streams and
sensitive detail follow [security.md](security.md).

## Implementations and version variation

[Secrets](secrets.md#implementation-selection) owns its immutable implementation
catalog and session ports. Workspace supplies the held context persistence
capability; core Secret services do not depend on a concrete store type.

A variation that changes authored intent or a business invariant is an
explicit typed variant in its owning domain. Vendor, substrate, component,
transport, and release mechanics remain in a concrete adapter or renderer
behind the capability port.

Each implementation declares an immutable identity, capability, supported
target and version constraints, content identity, and execution dependencies.
An allowlisted registry resolves from explicit validated state:

- resolution is deterministic and produces exactly one implementation;
- no match or more than one match fails before output publication or effects;
- there is no implicit latest, nearest-version fallback, or selection from
  ambient or gathered facts;
- accepting desired-state syntax does not imply implementation support; and
- application and domain policy never branch on concrete implementation
  identity to reproduce adapter mechanics.

Supported matrices belong to the owning domain contract. When an operation is
made immutable, it freezes the selected implementation identity, content
digest, and execution dependencies; continuation refuses drift rather than
silently selecting another implementation. Recovery follows
[state reconciliation](state-reconciliation.md#dependency-safety-during-recovery).

For an add-on, implementation selection also binds the exact package, catalog
snapshot, driver, and payload manifest defined by
[add-ons.md](add-ons.md#package-standard). Package discovery never replaces
the allowlisted registry or introduces executable authority at runtime.

## Data, state, and effects

Parsing preserves file, document, line, and column provenance outside public
API objects. Normalization, graph construction, validation, and planning do not
mutate their inputs. Deterministic output consumes only completed valid values.

Each mutable store has one context owner, canonical form, consistency and crash
boundary, and compatibility policy. An operation that can partially succeed
records enough state to distinguish no effect, unknown effect, completed
effect, and safe continuation. Names and successful process exits are not
ownership or completion evidence.

Side effects occur only at narrow adapters after pure validation, selection,
planning, and authorization. Requests are complete and immutable before they
cross the boundary. Adapter results normalize vendor detail without erasing
failure, identity, ownership, cancellation, or replay evidence. State and
effect rules must agree with [state-reconciliation.md](state-reconciliation.md)
and [security.md](security.md).

## Architecture verification

Introduce executable fitness checks with the boundary's first implementation;
reviewers retain semantic judgments that source checks cannot prove.

- Check every production Go package for inward dependencies, declared context
  ownership, and no direct adapter-to-adapter calls or domain vendor/I/O leaks.
- Reject concrete service types and their constructors outside composition,
  regardless of their names, and reject concrete component dependencies in
  same-package service fields. Test replacement through consumer interfaces and
  typed function capabilities, including independently constructed results and
  failure propagation; import direction alone does not prove substitutability.
- Verify that builds and runtime adapters use their declared versions and
  locks, with no ambient or silently substituted dependencies.
- Verify implementation registry entries against actual adapter content,
  contract IDs, playbooks/roles, and immutable identities. Review support claims.
- Test pure decisions, ordering, diagnostics, cancellation, and deterministic
  results without real I/O. Run shared port suites against every implementation
  for failure, replay, ownership, evidence, and version boundaries.
- Verify the complete Ansible lock closure, fixed inventory/inputs, privilege,
  no unapproved shell/command/raw/script/role/plugin content, sensitive output,
  replay, interruption, and structured failure/evidence.
- Cover public serialization, artifacts, commands, streams, help, exit codes,
  and graph composition with applicable golden and end-to-end tests.

Unit or fake-adapter success never qualifies a remote implementation. Every
supported substrate/component/product/version combination needs its named
real-system acceptance evidence before production use.
