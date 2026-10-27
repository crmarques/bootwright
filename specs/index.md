# Specification Index

Start with [project intent](project.md) when scope matters. Read the
[milestones](milestones.md) Status and Scope rules and the parts of the
authorized milestone page that [AGENTS.md](../AGENTS.md) names, then open only
the owning section below. A specified capability is not necessarily
implemented or authorized.

| Read when the task touches | Owner |
| --- | --- |
| Product purpose, scope and non-goals | [project](project.md) |
| What a term means: context, controller, BMC, substrate, binding, profile, capability, implementation, evidence, receipt | [glossary](glossary.md) |
| Boundaries, package shapes, Go/Ansible split, dependencies, fitness checks | [architecture](architecture.md) |
| Where a command's code lives | `internal/cli/catalog.go`, `cmd/bootwright/wiring_<domain>.go` and each package's `contracts.go` ([finding code](architecture.md#go-package-structure)) |
| Desired-state grammar, compilation, defaults | [api](api.md); input discovery and ceilings: [input](api/input.md) |
| One kind's fields | [environment](api/environment.md), [secrets](api/secrets.md), [machines](api/machines.md), [infrastructure services](api/infrastructure-services.md), [container clusters](api/openshift-clusters.md), [storage](api/ceph.md#kinds), [add-ons](api/addons.md), [custom playbooks](api/custom-playbooks.md) |
| Command behavior and journeys | [cli](cli.md); flags: [commands](cli/commands.md); streams, JSON, diagnostics: [output](cli/output.md) |
| Host prerequisites, setup, the controller stage | [controller](controller.md) |
| Contexts, frozen input, storage, locking, deletion | [contexts](contexts.md); controller record format: [controller record](contexts/controller-record.md) |
| Secret custody, bindings and reveal | [secrets](secrets.md) |
| Apply, destroy, continuation, ownership, mutation safety, GitOps readiness | [state reconciliation](state-reconciliation.md) |
| Managed services, host reservations, publication | [infrastructure services](infrastructure-services.md) |
| Provider hosts, machines, emulated BMCs, identity and power | [substrates](substrates.md) |
| Media store and operating-system installation | [managed OS](managed-os.md) |
| Container cluster installation | [container clusters](openshift-clusters.md) |
| OpenShift provider coverage and acceptance profiles | [cluster coverage](openshift-coverage.md) |
| Ceph lifecycle, native input, resources and release qualification | [storage](ceph.md), [native input](ceph-native-api.md), [resources](ceph-resources.md), [coverage](ceph-coverage.md) |
| Trust, sensitive material, processes, network, supply chain | [security](security.md) |
| The add-on boundary and lifecycle | [add-ons](add-ons.md) |
| Built-in add-on packages and acceptance profiles | [add-on coverage](add-on-coverage.md) |
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
