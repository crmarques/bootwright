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
| Workspace (CLI noun `context`) | Named root-owned contexts, separate Context configuration, per-user selection, and persistence boundaries for local runtime data. | Lifecycle policy, secret meaning, or arbitrary user filesystem content. |
| Controller (CLI noun `controller`) | Local controller prerequisite inspection, setup, invoking-account verification, and sudo process supervision. | Managed-machine provisioning, cluster readiness, or lifecycle ordering. |
| Secrets | Secret custody and materialization, immutable secret binding, and disclosure classification. | A consuming context's authorization or business decision. Entitlement semantics remain with the product context that consumes them. |
| Trust | SSH host identity, TLS trust, trust decisions, and durable trust evidence. | Secret custody, endpoint business policy, or ambient trust configuration. |
| Substrate | Provider capabilities, provider identity, network attachments, provider-host and machine infrastructure realization with emulated management controllers, and normalized power and identity operations. | Machine OS policy, cluster installation, or lifecycle ordering. |
| Machine | Machine identity, addresses, access posture, declared capabilities, and the relationship to a substrate and operating-system intent. | Provider mechanics, OS installer mechanics, or cluster membership policy. |
| Managed OS | Installer media custody, machine images, install profiles, managed-OS entitlement use, installation intent, and OS completion evidence. | Substrate realization, storage-product entitlement policy, or higher-level cluster and storage orchestration. |
| Infrastructure services | Shared placement, identity, endpoint, and ownership rules for artifact servers, load balancers, proxies, name resolution, time synchronization, and registries. Each service capability owns its specific policy and adapter. | Consumer cluster policy or one generic service workflow. |
| Container cluster | OpenShift and OKD release, topology, installer intent, node binding, cluster access, and installation evidence. | Machine, substrate, shared-service, storage, or add-on ownership. |
| Storage | Ceph cluster topology, placement, pools, filesystems, gateways, NFS services, exports, and storage completion evidence. | Machine OS installation or downstream cluster-add-on policy. |
| Add-ons | The [add-on contract](add-ons.md): catalog packages, profiles, bindings, input expansion, package and driver resolution, ordering within the add-on boundary, cluster application, readiness, and add-on records. | Cluster installation, storage invariants, complete-environment state transitions, or a general workflow language. |
| Native artifacts | Typed artifact manifests, canonical bytes, destination policy, and safe publication. The platform context that understands a native format owns its projection into that format. | Starting, adopting, or simulating a managed operation when an operator executes an artifact independently. |
| State reconciliation (CLI verbs `plan`, `status`, `apply`, `destroy`) | Immutable plans, operation state, scheduling, leases, continuation, ownership evidence, and transition authorization for the complete environment. | Domain capability mechanics or vendor-specific effects. |

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
rules. An adapter never calls another adapter to bypass that service. The
[named exceptions](#named-exceptions) are the only departures from these rules.

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

### Named exceptions

Each is deliberate and bounded; another like it is a defect.

- **Adapter-to-adapter wiring.** No adapter imports another
  (`TestPackageDependencyDirection`), but within Controller composition injects
  one adapter's capability into another through a Controller-owned contract:
  `controller/ansiblelocal` receives the execution guard as a
  `prerequisites.PythonExecutionGuard`, and `controller/nativelocal` receives
  `bundlelocal.FetchMetadata` as its `MetadataReader`.
- **The execution guard.** Every process running the installed private Python
  runtime (setup's Ansible run, a bundle probe, and a lifecycle attempt,
  quiescence probe or bounded run) starts inside
  `controller/bundlelocal.ExecutionGuard`, which first verifies that foundation.
  Composition binds it to `prerequisites.PythonExecutionGuard` and directly to
  `lifecycle.ExecutionGuard`, not through a Controller service.
- **Machine's direct SSH sessions.** `machine/sshlocal`, the one pinned SSH
  client an explicit session and a host-key observation cross, is a Go-owned
  remote access path outside the
  [Go and Ansible boundary](#go-and-ansible-responsibility-boundary), bound only
  to `machine/access` and `trust/enrollment`.
  [Direct SSH sessions](security.md#direct-ssh-sessions) owns its security rules.
- **Composition translation.** Composition decodes what a package reads from
  another it may not import. In `cmd/bootwright/wiring_machine.go`,
  `machineOwnership` presents Reconciliation's evidence in Machine vocabulary;
  `machineHostKeys` decodes, through `installation.HostKeyEvidence`, the host
  key `managedos/installation` proved; and `machineIdentities`, for a power
  operation, and `provedIdentities`, for an installation's attempt, decode
  through `baremetal.PinnedIdentity` the hardware identity
  `substrate/baremetal` proved. `beginSignalOperation` in
  `cmd/bootwright/interrupts.go` reads an interrupt `privilege.Begin` received
  as the CLI's `ErrInterrupted`.
- **The schema registry.** `api/v1alpha1`'s `init` functions fill its
  package-global kind-to-schema map through `register`, which panics on a
  duplicate; nothing writes it afterwards, and `Schema` returns a fresh `Shape`.

## Dependency selection and reuse

Before adding or replacing a dependency, search for suitable options and choose
one that is well-known, trusted, and fits the task. Prefer the standard library
or dependencies already in use when suitable. Specs name a dependency's role,
never its version; exact versions live only in the lock files (`go.mod`,
`scripts/tools`, the Ansible locks) and in [development](../docs/development.md).

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
| `ansible/` | Controlled Ansible configuration, dependency locks, and embedded adapter collection. |
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

Packages are domain-first, so a reader can predict where a behavior lives. Each
has one kind, and its role in `packageRoles`
(`test/architecture/boundaries_test.go`) decides what it may import:

| Kind | Path | Owns | Role |
| --- | --- | --- | --- |
| Context root | `internal/<context>` | Pure domain values, invariants and derivations such as `substrate.TargetFor`, the kind admission rules (`Normalize`, `Validate`, `ValidateAuthored`, `ValidatePartial`) the compiler composes, and shared values such as `machine.Placement` and `secrets.Material`. No ports and no I/O. `desiredstate/customplaybooks` is of the same kind. | domain |
| Command service | `internal/<context>/<capability>` | One command family. `service.go` declares `Service`, its constructor and one exported method per command; `requests.go` the request and result types the CLI consumes; `contracts.go` every interface the package consumes. A recognized but unavailable family is a stub, `service.go` alone, listed in `stubCapabilities`. | application |
| Lifecycle capability | `internal/<context>/<capability>`, such as `managedos/installation`, `containercluster/agentinstall`, `infrastructureservices/managedservice` and `infrastructureservices/artifactserver` | The [`Capability` port](#lifecycle-ports) implementations of one substrate arm, installation method or service, or of the controller stage. `capability.go` declares a capability type and its constructor, `contracts.go` its ports, `requests.go` and `evidence.go` its frozen request and the evidence it proves, and, where it has them, `catalog.go` its kind and implementation identities and `selection.go` the objects it plans. | application |
| Capability definition | `infrastructureservices/dnsserver`, `ntpserver` and `proxy` | One `managedservice.Definition` the shared capability runs; it consumes no other application package. | application |
| Application store | `reconciliation/operationstore`, `secrets/secretstore` | A durable record format and its consistency over an `Area` a Workspace adapter supplies. `reconciliation/contextguard` likewise implements `workspace/contexts.ContextMutationGuard` for Reconciliation and consumes no port. | application |
| Port test support | `reconciliation/operationstore/areacontract`, `secrets/secretstore/areacontract`, `controller/prerequisites/storagecontract`, `reconciliation/lifecycle/workspacecontract`, `managedos/media/storecontract` and `reconciliation/operationstore/areadouble` | Each `contract` package, beside the port it exercises, is the shared contract suite `Verify` of one storage port a Workspace adapter implements: every implementation's tests run it, the in-memory doubles included, and it consumes only that port's package, the lifecycle workspace's also the operation areas and controller evidence its views carry and the secret context a transaction lends. The operation area's, controller storage's and lifecycle workspace's suites hold every method of their port to at least one clause. `areadouble.Admit` is what every in-memory operation store `Area` double refuses before it answers and `areadouble.Entries` what it lists; it imports nothing first-party. Only tests import these packages. | application |
| Driven adapter | `internal/<context>/<implementation>`, named by what it binds | One effect behind another package's contract, within what `TestAdmissionEffectBoundary` allows it. `secrets/material` and `controller/privilege` keep their consumed interfaces in `contracts.go`. | adapter |
| Diagnostics | `internal/diagnostics` | The diagnostic and typed-failure vocabulary every layer emits; it imports nothing first-party. | technical |
| Availability | `internal/availability` | The single unavailable-capability sentinel, `ErrNotImplemented`. | technical |

In an application package no file but `contracts.go` declares an exported
interface. `api/v1alpha1` is a domain package; `internal/cli`, `cmd/bootwright`
and the embedded `ansible` assets have roles of their own.
`applicationDependencies` names every application package another consumes, and
why. A consumer of a realized Machine reads `substrate.TargetFor` and imports no
substrate arm package.

A lifecycle capability, like every application package that consumes the
lifecycle port vocabulary, reads no JSON itself. It freezes its request through
`reconciliation.Freeze`, reads it back through `reconciliation.Thaw` and, once
the version it holds is its own, `reconciliation.ProveCanonical`, and reads an
adapter's evidence, or a proof another block left, through
`reconciliation.DecodeEvidence` at its own byte bound. These are functions over
one shape each, not a framework: they refuse a member the shape does not
declare and anything after the document, in the capability's own words, while
what a request selects and what its evidence proves stay the capability's
(`TestNoLifecycleConsumerReadsJSONItself`).

A command service exposes a concrete `Service`, a lifecycle capability concrete
capability types. Dependencies are private fields; add constructors when
there are actual dependencies to inject. Consumer-owned ports are repeated on
purpose: `Confirmer` and `Compiler` are declared by every service that needs
them, because each consumer owns the smallest interface it uses and composition
binds one implementation to all of them. Add domain files and packages with
their first authorized behavior. Do not create empty packages or repeat generic
`domain`, `application`, `ports`, and `adapters` layers inside every context.

`cmd/bootwright` is the only composition root. `main.go` is the process
entrypoint and holds the linker-injected build identity; `run.go` crosses the
local privilege boundary, then invokes the CLI with process streams and build
information. `wireServices` in `wiring.go` returns the statically typed
`cli.Services` bundle by calling one `wire<Domain>` function per domain, and
each `wiring_<domain>.go` constructs that domain's adapters and injects them,
naming every adapter bound to every port.

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
output-mode selection, and exit-code mapping. `runner.go` orders one invocation:
parse, validate, dispatch, render. `catalog.go` is the single list of commands;
each entry also records whether the command requires root and whether it is
implemented, so no second list of command paths exists. Each
`commands_<domain>.go` keeps its command declarations, consumer interface, and
request translation together and `output_<domain>.go` its rendering;
`dispatch.go` and `results.go` route a path and a result to them, and
`services.go` holds one interface field per domain service. CLI code never
constructs a driven adapter or sequences a cross-domain workflow.

Application services receive complete validated immutable requests and return
presentation-independent results. Domain decisions are pure. Filesystem,
process, persistence, network, clock, randomness, and remote access occur only
in injected adapters. Package-global mutable flags, streams, registries, clocks,
configuration, and service instances are forbidden, apart from the
[schema registry](#named-exceptions). An unavailable use case is a
typed stub whose every method returns `availability.ErrNotImplemented` after the
context check; it exposes no successful result schema, and the CLI alone renders
results and unavailable messages.

Repository-fitness packages under `test/architecture` inspect source and assets
but contain no production behavior. They enforce dependency direction, effect
boundaries, composition-only binding, the package kinds above, the CLI import
allowlist, and the stub set, whose commands the CLI catalog leaves unavailable.

### Finding code

1. Start from the command: grep its path string, such as `"secret set"`, in
   `internal/cli/commands_<domain>.go`.
2. That file's consumer interface names the request type's package,
   `internal/<context>/<capability>/`.
3. There, `service.go` is the use case, `contracts.go` lists what it needs, and
   `requests.go` is what the CLI sees.
4. `cmd/bootwright/wiring_<domain>.go` names the adapter package bound to each
   port.
5. The pure rules for an API kind are in `internal/<context>/admission.go`.
6. A lifecycle effect starts at its capability in `buildCapabilities`, and
   `operationPlaybook` binds its playbooks, both in
   `cmd/bootwright/wiring_reconciliation.go`.

Adding a command touches its spec in `commands_<domain>.go`, marked `available`
once implemented, the request type in the capability's `requests.go`, the
method on its `Service` and consumer interface, a case in `dispatch.go`, a
renderer in `output_<domain>.go` with its case in `results.go`, and the binding
in `wiring_<domain>.go`. `TestPublicCatalog` and
`TestCommandModesComeFromTheCatalog` hold its flags and modes,
`TestEveryCatalogCommandHasApplicationDispatch` fails until dispatch has the
case, and implementing a stub removes it from `stubCapabilities`.

### Adding a lifecycle capability

1. Create the package in the lifecycle-capability shape above, implementing
   every method of `Capability` in
   `internal/reconciliation/lifecycle/contracts.go`.
2. In `test/architecture/boundaries_test.go`, add it to the capability list in
   `packageRoles` and give it an `applicationDependencies` entry naming exactly
   the application packages it imports, normally `capabilityDependencies()`,
   with the reason for any other edge.
3. In `test/architecture/composition_test.go`, list in `applicationValues` each
   exported type with methods that an adapter constructs as a value rather than
   receives as a service.
4. In `cmd/bootwright/wiring_reconciliation.go`, add its `boundCapability` to
   `buildCapabilities` in API kind order and bind each operation's playbook in
   `operationPlaybook`; `TestBoundPlaybooksCoverExactlyTheOfferedCapabilities`
   and `TestEveryBoundPlaybookExistsInTheEmbeddedCollection` fail until those
   agree with each other and the collection.
5. Add its playbooks, role and protocol plugin to the
   [collection](#ansible-collection-structure).

### Context admission and compilation

Context management, immutable input reading, and compilation of an explicit
input universe are separate. `contexts.Service` owns context publication and
consumes its own `Compiler` and `Repository` interfaces; `compilation.Service`
consumes the narrow `ContextInputs` interface for existing named or current
context input, which `contexts.Inputs` implements through its own narrow
repository interface. Input values are immutable views, separate from writable
context records. Context setup is parsed independently of the Environment
graph; empty desired state is a valid initialized context.

`compilation.Compiler` accepts explicit input independently of context
selection and never calls context management; input lookup never calls the
compiler. Compilation translates decoded values into Environment-owned inputs
for pure selection and graph closure. Its `GraphSelector` coordinates injected
add-on attachment and Environment selection functions and translates their
results; the composition root only binds those functions. Environment's domain
rules never import the desired-state aggregate. Platform-owned invariants
remain with Machine, Container cluster, Storage, and the other referenced
domains; compilation phase and diagnostic semantics remain in
[the API contract](api.md).

The composition root binds `workspace/contextfs` to the fixed privileged store
and `workspace/selectionfs` to a lazily verified invoking account. The latter
accesses the user file with that account's credentials, including a bounded
credential-dropped subprocess when the caller is root. `controller/privilege`
owns local account lookup and invocation-scoped sudo process supervision, and
the elevation decisions: noninteractive mode, stream handoff, the elevated
child's start announcement, and what each outcome reports. Its `Begin` is the
one signal subscription, which the supervisor relays and every CLI operation's
cancellation derives from. `TestAdmissionEffectBoundary` holds every production
package to its effect grants, the composition root included, which holds no
`os/signal` (`TestTheEffectBoundaryHoldsTheCompositionRoot`). Only
validated, available commands needing stored context data acquire that
privilege boundary; informational and explicit-input validation paths remain
effect-free. [CLI invocation](cli.md#local-privilege-and-user-identity) owns
classification and refresh policy.

Context initialization receives a transaction-scoped secret-area capability
and initializer callback. It initializes the configured implementation before
readiness publication without reacquiring the Workspace lock. The immutable
implementation resolver validates configuration before any store mutation.
Encryption commands use that Context configuration independently of input.
Context update and deletion consume a Reconciliation-owned
`ContextMutationGuard` through an interface owned by the consuming Workspace
package; the guard participates in the mutation and locking boundary, and a
stale Boolean check cannot authorize a later mutation.

### Controller host and local services

The [controller Machine reference](api/environment.md#controller-machine) is
an admitted Environment relationship. It does not prove the invoking host's
identity, relocate execution or change Workspace storage. Machine owns the
selected host's proxy and capability intent; Environment owns selection and
retention. The admission and compiler boundary remains free of host probes and
runtime writes. [Controller](controller.md) defines prerequisite selection,
local setup, inspection and host-evidence requirements:

- Controller owns verified local-host evidence, prerequisite readiness and
  setup through bounded capability interfaces. Authored names, addresses,
  `access.local` and controller selection cannot substitute for that evidence.
- Workspace owns any durable binding between a context and its verified
  controller host, following the
  [binding](contexts.md#controller-relationship-and-host-binding) and
  [publication](contexts/controller-record.md#publication-and-recovery) contracts.
  Desired state never supplies a storage-root override or runtime identity token.
- [Infrastructure services](infrastructure-services.md) own local service
  effects, their host reservations and their readiness, replay and inverse
  evidence. A local adapter must preserve the same logical capability contract
  as its qualified remote counterpart; local execution does not bypass Go
  authorization, privileged-process or Ansible boundaries.
- Reconciliation freezes the required controller and implementation evidence
  and owns ordering and
  [controller-host protection](state-reconciliation.md#controller-host-protection).
  Shared host effects require coordination across contexts, not only the
  existing per-context mutation lease.

No inferred capability, public executable selector or generic local command
runner is introduced by the controller reference. Optional service co-location
must qualify its actual host and runtime implementation before becoming
executable.

### Environment inspection and access

Environment preflight consumes domain-owned prerequisite capabilities.
Inspection combines validated input and permitted local evidence. Environment
access selects the declared cluster and node before delegating platform access
decisions. Machine owns SSH session resolution and its host-key proof;
Container cluster owns `oc`, `kubectl`, and kubeconfig behavior. These calls
preserve the descriptor, session and export boundaries defined by
[the access contract](cli.md#resource-inspection-and-explicit-access).

### Lifecycle ports

`reconciliation/lifecycle.Service` owns the operation; every platform effect
reaches it through the `Capability` port that
`internal/reconciliation/lifecycle/contracts.go` declares beside every other
port the engine consumes. Three rules constrain how the set of capabilities
grows.

Composition injects an immutable resolver of the available capability
implementations, keyed by the API kind and the resolved implementation
identity, and binds the lifecycle runner's playbook entrypoints, so a
capability names no playbook. No global registry, ambient discovery or runtime
plugin path exists, and application policy never switches on a concrete
implementation.

Reconciliation owns cross-domain ordering, operation transitions and durable
records. A capability plans its own blocks and owns the meaning of their
completion, readiness and absence evidence, returning it through typed results;
it cannot schedule another domain's work, allocate an operation identity or
write lifecycle state. A cluster capability therefore cannot drive storage or
OS installation, and an adapter returns bounded results only to its own
application service. A capability observes a frozen apply and a frozen removal
through separate port methods, so a removal is resolved by what the removal
proves. A capability names what its blocks depend on as API
objects and the authorization they consume; Reconciliation resolves the former
into block dependencies and checks the latter at registration, so no capability
learns another's block identities.

Workspace owns the durable boundary: the lifecycle transaction, the operation
area and the reservation record are Workspace primitives, and Reconciliation
alone decides what they contain. Secret custody, controller host evidence and
the private execution runtime are consumed through their owning contexts'
published capabilities rather than reimplemented here. The lifecycle
transaction also lends its secret area, one caller at a time and only for the
length of that caller's callback, so the engine keeps
[produced material](secrets.md#produced-material) in Secrets custody inside
the transaction that records it, without taking the store lock the transaction
already holds; only Secrets interprets the area.

One attempt's `Execution` carries only what every capability consumes. Work
belonging to a single stage travels in its own value, present on the blocks the
plan froze into that stage and absent everywhere else, so one capability's
specifics never widen the contract the others receive. The controller stage's
publication boundary is such a value. Which blocks receive it follows the stage
the plan froze, which is declared domain vocabulary; no application policy reads
an implementation identity to decide.

`LocateTool` is the one question every attempt may ask another stage: where
the [controller stage](controller.md#the-controller-stage) installed one
executable. The engine hands it, with the controller stage block the plan
froze and that block's evidence from the apply the operation runs over, its own
or the one a removal takes back, to that block's capability through the
`ToolLocator` port, unread, because only that capability knows what its frozen
request selects and what its evidence names. A plan without that block has
installed nothing to run.

The one widening is an apply attempt's `Proved`: what each block it depends on
proved earlier in this operation, as the engine's block evidence, handed over
unread. A proof one capability relies on is another capability's to have made,
as the identity a physical Machine's block pins is what an installation's
pre-boot proof compares, so the consumer decodes that evidence only through the
evidence's owner, which composition wires to it. The engine never interprets
it, and the consumer never learns its shape.

A `Result` or an `Observation` may carry produced material beside its
evidence: named confidential bytes a proved completion left, such as an
installation's administrator kubeconfig. It is never evidence. Only the engine
places it in custody, and only for an apply's block it records `done`
([produced material custody](state-reconciliation.md#produced-material-custody));
everything else it discards and clears.

### Native projection and publication

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

Ansible owns controller dependency installation and Bootwright-controlled
interaction with managed remote components.
Remote observation, access, configuration, installation, verification, and
removal cross an Ansible adapter boundary, apart from
[Machine's direct SSH sessions](#named-exceptions). Ansible does not infer
desired state, choose product workflow, order cross-domain work, grant
authorization, own operation state, or format product output.

An Ansible adapter receives one frozen validated request and returns a bounded
structured result with required evidence. A local native tool may run
through a typed Go runner for a purely local transformation. Controller
prerequisite inspection uses bounded Go read-only ports. Go also materializes
the private Python/Ansible execution bootstrap. Host-package and native target
CLI installation cross the fixed Ansible controller roles/playbooks through
[Controller ports](controller.md#egress-and-local-effects). A tool
contacting a managed remote component belongs inside Ansible.

Operator-supplied automation and add-on package content are not internal
adapters. A schema declaration or package origin grants no execution authority.
[The custom-code rule](security.md#custom-code) permits a
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
| Collection `roles/<domain>_<capability>[_<implementation>]/` | One local or remote capability adapter with only applicable standard role directories. |
| Collection `plugins/` | Capability adapters, effect-free product translation/evidence normalization, or the adapter's output callback, which [prints nothing a `no_log` result raised](security.md#logs-output-and-diagnostics). |
| Collection `tests/unit/` | Collection-owned tests following `ansible-test` discovery. Their `conftest.py` installs the collection loader `ansible-test` would when no runner has, so plain pytest runs the same tests; the `units` suite of `scripts/ansible-check` runs them both ways. |

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

A playbook imports its role's entry point by the key the role's argument
specification declares, never by its task file's name, which ansible-core runs
unvalidated. ansible-core then validates what that entry point's specification
declares, the request and any material mapping beside it, before the entry
point's first task, and a request the role does not admit refuses there:
`test_the_first_task_of_every_playbook_validates_its_entry_points_arguments`
and
`test_a_request_the_role_does_not_admit_refuses_before_the_entry_points_first_task`
hold every playbook to this.

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

A consumer of a realized Machine dispatches on the substrate arm or identity
channel Go froze into its request, through that substrate role's entry points,
and each dispatch ends in a terminal fail for a value it has no entry point
for. The substrate owns the machine port as role entry points validated by
their argument specs (decision D1), and consumers compose only those.

### Ansible collection plugins and results

A Bootwright module or action validates a frozen request, invokes the required
API, and normalizes its result within the consumer's effect and security
boundary. Other plugins provide effect-free product translation or evidence
normalization. Plugins contain no workflow or authorization policy and are
tested with their consumer.

Playbook stdout is not a product API. Go consumes the structured adapter result
and presents product output under [cli.md](cli.md); external streams and
sensitive detail follow [security.md](security.md).

#### The adapter result protocol

Every adapter run reports on one channel its runner owns and the adapter
inherits: descriptor 3 carries the adapter's records and descriptor 4 the
runner's acknowledgements. Neither is a file the automation names, and nothing
the adapter prints on its own output is part of this protocol. A record is one
canonical JSON object (sorted keys, no insignificant whitespace, ASCII) on one
line of at most 64 KiB with its newline, carrying exactly its phase's fields;
an acknowledgement is the line `proceed`. The lifecycle runner
(`reconciliation/ansiblerunner`) and the controller runner
(`controller/ansiblelocal`, for `setup` and the controller stage) accept these
phases:

| Phase | Runner | Direction | Fields | Acknowledgement | Bounds | Failure effect |
| --- | --- | --- | --- | --- | --- | --- |
| `loaded` | both | adapter → runner | `phase` | before any effect | first, once | Acknowledgement EOF or another reply fails the adapter before its first effect. |
| `prepared` | controller | adapter → runner | `phase`, `preparation`: `inventorySHA256`, `addedSources`, and for a native plan `afterInventorySHA256`, `planDigest`, `transitionsSHA256` | after the runner matches this before-state to the frozen request and publishes it durably | once, after `loaded`, except on recovery; at most 512 sources | Unmatched or unpublished, it is never acknowledged, so no host-wide effect runs. |
| `native` | controller | adapter → runner | `phase` | before the native package transaction | once, after `prepared`, only for native actions | Once authorized, the transaction is not killed on a deadline or cancellation. |
| `continue` | controller | adapter → runner | `phase` | before each tool installation | one per frozen tool, in order, after `prepared`; at most 128 | Acknowledgement EOF fails the adapter before that tool. |
| `group` | lifecycle | adapter → runner | `phase`, `group`, `status` | none | after `loaded` | Progress only; a settled status advances completion only for a group the frozen block declares. |
| `completed` | both | adapter → runner | `phase`, `outcome`, non-empty `evidence` | none | last, once | Absent when the channel closes, the run has no result. |
| `refused` | controller | adapter → runner | `phase`, `reason`: `release-stamp` | none | last, once, in place of `completed`, while an `openshift-clients` tool is being installed | The run fails with the [release-stamp refusal](controller.md#selection-and-command-journeys) instead of the generic adapter failure, even when the adapter's failed exit is read first. |
| `refused` | lifecycle | adapter → runner | `phase`, `reason`: one the run's request names, `identity-mismatch` for a [pinned power run](cli.md#machine-power-operations) and each refusal of a [pre-boot proof](substrates.md#adapter-boundary) for an installation's apply | none | last, once, after `loaded`, in place of `completed` | The run fails with the diagnostic its caller gave that reason instead of the generic adapter failure, even when the adapter's failed exit is read first; the adapter is left to end on its own. |

`outcome` is `changed` or `unchanged`, and the controller runner refuses
`unchanged` after an authorized `native`. `status` is `running`, `ok`, `failed`
or `skipped`, and every status but `running` settles its group. Go alone derives
`failed`, `unknown` and `canceled`; they never cross the channel. `reason`
names an adapter refusal Go remedies by name; the outcome is still Go's, and
the diagnostic is the one Go gives that reason, carrying the object, what was
refused and the remedy.

Both runners refuse a malformed or oversized record, an unknown, repeated or
out-of-order phase, a record after `completed`, and more records than their
bound: 64 for the lifecycle runner, 132 for the controller runner (`loaded`,
`prepared`, `native`, `completed` or `refused`, and a `continue` per tool). The
controller runner also refuses a `refused` record with another reason or for
another tool kind, and the lifecycle runner a `refused` record whose reason the
run's request does not name. Both runners judge a record they read after the
adapter's failed exit as if they had read that record first, so a record they
refuse, or one they cannot read, fails the run as a protocol breach rather than
as the failed exit, whichever of the two they read first: a lifecycle attempt
is then `unknown`, and a controller run fails `controller.unknown`, its outcome
still set by `prepared` as below. A record they accept after the exit leaves
that failure and moves the protocol on, so a later record is judged in its
place, but nothing is released, published, authorized, acknowledged or
reported for it, since its adapter is gone. A read that the drain's close ends
is the runner's own and no record, so it leaves the failed exit. A record the
runner refuses, or a channel that cannot be read, ends the protocol at once:
the runner closes the acknowledgement channel, so an adapter waiting for one
fails instead of waiting out the deadline, and the lifecycle runner kills the
adapter's process group.
The controller runner kills nothing on a refusal: the adapter fails at its next
acknowledgement, and an authorized native transaction first runs to its end. A
deadline or a cancellation kills the adapter's process group, sparing only an
authorized native transaction. A controller run's deadline is 10 minutes; a
client installation adds each source's
[acquisition deadline](controller.md#the-controller-stage), held to a ceiling of
2 hours, and a client closure past that ceiling is refused before Ansible
starts. A lifecycle run's is the one its request states, which its capability
derives from the wait budgets that request froze plus a margin for the rest of
the run, held to a ceiling of 6 hours; a request that states none keeps 2
hours, and the
[bounds table](contexts.md#storage-locking-and-publication) names each
constant. A descendant still holding the channel after the adapter exits is
drained for 5 seconds, or 60 once a controller run is prepared or cancelled,
and the result is lost.

A lifecycle attempt is then `unknown` after a refusal, a deadline, a lost result
or a zero exit without `completed`; `failed` after a non-zero exit that broke no
protocol rule; and `canceled` when the operator cancels. The controller runner
reports `failed` for any failure before `prepared` is published, since no
host-wide effect was yet permitted, and `unknown` after it or on cancellation.

A lifecycle run on the controller may also declare output files: each is a
named path under a private `outputs/` directory of its job, passed to the
adapter as `<prefix>_output` only when the run declares one. After a completed
run the runner reads each back through the job's held handles, admitting only
a regular file of its owner, of mode `0600`, with one link, of one byte up to
the secret part bound, and fails the run on anything else; an absent file
offers nothing, and the job's removal removes every output. Output files are
neither records nor evidence, and a run elsewhere declares none.

A completion publishes evidence proving no postcondition only when the run is a
read-only observation, because the engine resolves a part-way effect from that
evidence while a mutation must reach its postcondition or fail. An adapter's
refusal names the fields and objects still unproved, never their values, so the
message stays safe where the evidence itself is censored.

The protocol, its phases and these rules are one shared implementation, and one
runner carries every adapter run. What the evidence contains, and what proves
it, belong to each capability. Go decides what an outcome and its evidence
mean; a plugin neither schedules work nor authorizes it.

Not yet met: `controller/ansiblelocal` and `reconciliation/ansiblerunner` each decode the protocol, with bounds of 132 and 64 records; the lifecycle decoder admits duplicate keys, a `group` on `completed` and any non-empty `status`; and each capability's action plugin copies the phase dispatch, enums and postcondition rule; tracked as [B19](milestones/m1.md#b19).

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
made immutable, each block freezes its selected implementation identity and
content digest, and the operation freezes the automation it runs;
continuation refuses another implementation or automation rather than
silently selecting one. Recovery follows
[state reconciliation](state-reconciliation.md#dependency-safety-during-recovery).

For an add-on, implementation selection also binds the exact package, catalog
snapshot, driver, and payload manifest defined by
[the add-ons design](deferred/add-ons-design.md#package-standard). Package discovery never replaces
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
- Build and vet the module on one platform besides linux/amd64, where each
  off-platform refusal satisfies the ports its Linux adapter is bound to.
- Verify implementation registry entries against actual adapter content,
  contract IDs, playbooks/roles, and immutable identities. Review support claims.
- Test pure decisions, ordering, diagnostics, cancellation, and deterministic
  results without real I/O. Run shared port suites against every implementation
  for failure, replay, ownership, evidence, and version boundaries.
- Verify the complete Ansible lock closure, fixed inventory/inputs, privilege,
  no unapproved shell/command/raw/script/role/plugin content, sensitive output,
  replay, interruption, and structured failure/evidence.
- The collection's structural tests hold every role to the rules a play cannot
  check: an entry point hands the runner `loaded` once, before anything but an
  assertion; a task that receives bound material, and every completion, runs
  under `no_log`, but for the exact list of completions that read no bound
  material and whose refusal the attempt's retained output carries, today the
  bare-metal proof and observation publications
  ([Physical machine realization](substrates.md#physical-machine-realization));
  material crosses an include only as a path; a command
  runs from `argv` an executable its exact allowlist names, `timeout`'s own
  command included, and never through a shell; a container unit names its
  entrypoint and nothing passes `--debug`; a poll reads its result only
  through a default, so a failed read spends an attempt; a task that reaches a
  management controller under `no_log` never fails itself, and the next task,
  with only the protocol's group records between them, either refuses outside
  `no_log` with the controller's own message, deciding on that task's result
  alone and stopping the play whenever the controller refused, or publishes
  the result as an unproved observation; every template
  renders the same bytes twice and its golden's; and every action plugin's
  `run()` is driven with what surrounds it stubbed. Go holds every request
  this build sends, request version included, to the argument specification of
  the role entry point that receives it: the capability contract suite for
  each lifecycle operation, with the material mapping lent beside it, and the
  power and controller tests for theirs;
  `TestEveryEntryPointAdmitsTheMaterialAnSSHPlacementLends` holds each entry
  point's material mapping to the identity and host key an SSH placement
  lends, and `TestRoleVersionAssertionsMatchTheirArgumentSpecs` each role's
  version assertion to that specification.
- Cover public serialization, artifacts, commands, streams, help, exit codes,
  and graph composition with applicable golden and end-to-end tests.
- Keep every allowlist and registry a fitness check reads exact: an entry that
  is no longer needed, or names what no longer exists, fails, so each list only
  shrinks. A known deviation in a table of refusals names its backlog item and
  the exact diagnostics its row reports until that item lands, which must
  refuse, so it excuses no other outcome. An ignored guidance path fails once
  no guidance outside the ignored paths cites it, and a guidance byte budget
  once it exceeds its file's size by more than a tenth of itself; each
  `.claude/rules` page has a budget of its own. The docs checks that list
  production Go source, `TestDocsPackageTableNamesEveryTestOnlyPackage` and
  `TestDiagnosticCodesMatchOutputSpec`, walk the working tree's sources,
  tracked or not, as a build reads them.
  `TestDocsNameEveryCheckThatWalksTheWorkingTree` keeps that list exact: it
  follows each docs check through the functions, methods and package variables
  of its package directory, sources and tests alike, to any reference, through
  a file's imports or dot imports, to a standard library function that lists a
  directory (`Glob`, `ReadDir` and `WalkDir` of `io/fs`, `ReadDir` of `os` and
  `io/ioutil`, and `Glob`, `Walk` and `WalkDir` of `path/filepath`), or to a
  method named `Glob`, `ReadDir`, `Readdir` or `Readdirnames`, whose receiver
  a syntax walk cannot type. A listing reached only through another package's
  function is beyond it, and another package's function of the same name,
  such as `ast.Walk`, is no listing. A listing of a filesystem whose syntax
  makes it an `fstest.MapFS` lists nothing: a literal, a conversion, or a
  parameter or variable declared as one, a package variable in any file of
  its package; any other filesystem, a field or a function's result among
  them, is the working tree's. It follows a package variable through every
  value an assignment statement gives it, directly or through an element,
  field or pointer, and from the external tests through the tested package's
  qualifier, and no order in which it reads the files decides its verdict.
  Every other docs check that lists files reads only the files Git tracks,
  never an untracked or ignored one: the guidance checks of
  `test/architecture/docs_test.go`, the milestone checker of
  `test/architecture/milestones_test.go` and the operator command lines of
  `internal/cli/docs_commands_test.go`. A link or cited path resolves only to
  a tracked file or a directory that holds one, a budgeted file Git does not
  track fails, and a tracked file a check reads that the working tree lacks
  fails until its removal is staged. Each clause of the effect boundary is
  proved by a fixture it refuses, and each kind of grant by the clauses it
  exempts and the ones it leaves. A production function, in Go or in the
  collection's plugins, stays within 100 lines; the lists of functions
  awaiting their split are empty and only shrink.
- `make quick` tests, besides the architecture suite, each package with a
  changed Go file and each package whose directory encloses, at any depth, a
  changed file that is not Go source, as a `go:embed` pattern or a read of
  `testdata` reaches it; each package that imports one, directly, through a
  dependency or only from its tests; and each package whose tests name, as a
  quoted path, the first path element of a changed file that is not Go source,
  as a test reading `specs/` or `examples/` does. Paths are matched as Git
  stores them, non-ASCII characters included. A change to the module files, or
  a changed file or reading test whose path Git quotes even with
  `core.quotePath` off, because it holds a double quote, a backslash or a
  control character, tests everything; a selected package the default build
  omits is named and left to its own gate.

Unit or fake-adapter success never qualifies a remote implementation. Every
supported substrate/component/product/version combination needs its named
real-system acceptance evidence before production use.
