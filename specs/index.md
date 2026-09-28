# Specification Index

Start with [project intent](project.md) when scope matters. Read the
[milestones](milestones.md) status and the milestone page of the authorized
slice or item, then open only the owning section below. A specified capability
is not necessarily implemented or authorized.

| Read when the task touches | Owner |
| --- | --- |
| Product purpose, scope and non-goals | [project](project.md) |
| Boundaries, package shapes, Go/Ansible split, dependencies, fitness checks | [architecture](architecture.md) |
| Where a command's code lives | `internal/cli/catalog.go`, `cmd/bootwright/wiring_<domain>.go` and each package's `contracts.go` ([finding code](architecture.md#go-package-structure)) |
| Desired-state grammar, compilation, defaults | [api](api.md); input discovery and ceilings: [input](api/input.md) |
| One kind's fields | [environment](api/environment.md), [secrets](api/secrets.md), [machines](api/machines.md), [infrastructure services](api/infrastructure-services.md), [container clusters](api/container-clusters.md), [storage](api/storage.md), [add-ons](api/addons.md), [custom playbooks](api/custom-playbooks.md) |
| Command behavior and journeys | [cli](cli.md); flags: [commands](cli/commands.md); streams, JSON, diagnostics: [output](cli/output.md) |
| Host prerequisites, setup, the controller stage | [controller](controller.md) |
| Contexts, frozen input, storage, locking, deletion | [contexts](contexts.md); controller record format: [controller record](contexts/controller-record.md) |
| Secret custody, bindings and reveal | [secrets](secrets.md) |
| Apply, destroy, continuation, ownership, mutation safety, GitOps readiness | [state reconciliation](state-reconciliation.md) |
| Managed services, host reservations, publication | [infrastructure services](infrastructure-services.md) |
| Provider hosts, machines, emulated BMCs, identity and power | [substrates](substrates.md) |
| Media store and operating-system installation | [managed OS](managed-os.md) |
| Container cluster installation | [container clusters](container-clusters.md) |
| Trust, sensitive material, processes, network, supply chain | [security](security.md) |
| The add-on boundary | [add-ons](add-ons.md) |
| Delivery state, open items, delivered history, parked work, decisions | [milestones](milestones.md) and its pages, [delivered](milestones/delivered.md), [backlog](milestones/backlog.md) |
| Design for an unpromoted feature, only when reviving it | [deferred](deferred/) |

## Conventions

Each requirement has one owner; other pages link to it. Specs state target
invariants in the present tense and claim no availability. A rule the code does
not yet meet carries one line, "Not yet met: ...; tracked as B<n>", linking the
item on its milestone page or among the parked items.
Delivery state lives in milestones, observed lessons in
[knowledge](../.agents/knowledge/index.md), and working procedure in skills. Add
detail with its first requested feature or a demonstrated ambiguity; design for
an unpromoted feature lives under `deferred/`.

## Glossary

- **Context**: a named, durable Bootwright workspace holding frozen input,
  secrets and lifecycle state ([contexts](contexts.md)). It is not a Go
  `context.Context` and not a bounded context of the architecture.
- **Controller**: the host that runs Bootwright. A **management controller** is
  a server's out-of-band Redfish endpoint, physical or emulated.
- **Realized target**: what a substrate derives for a Machine: the management
  controller it boots through, its identity channel, whether it is physical and
  the block that realizes it ([substrates](substrates.md#selection-and-refusal)).
- **Reservation**: a host-wide claim, such as a socket, BMC, media or machine,
  that refuses a conflicting context
  ([host reservations](infrastructure-services.md#host-reservations)).
- **Bundle** and **binding**: setup publishes a sealed execution bundle; a
  context binds the controller and bundle it uses; an operation binds the exact
  secret material it froze ([secrets](secrets.md)).
- **Acceptance**: an operator-run gate recorded in the
  [ledger](../docs/acceptance.md); in-tree gates are tests.
