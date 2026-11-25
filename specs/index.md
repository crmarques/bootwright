# Specification Index

Start with [project intent](project.md) when scope matters. Read the
[milestones](milestones.md) Status and Scope rules and, on the authorized
milestone page, only the parts [AGENTS.md](../AGENTS.md) names, then open only
the owning section below.

| Read when the task touches | Owner |
| --- | --- |
| Product purpose, scope and non-goals | [project](project.md) |
| Terminology | [glossary](glossary.md) |
| Boundaries, package shapes, Go/Ansible split, dependencies, fitness checks | [architecture](architecture.md) |
| Where a command's code lives | `internal/cli/catalog.go`, `cmd/bootwright/wiring_<domain>.go` and each package's `contracts.go` ([finding code](architecture.md#go-package-structure)) |
| Desired-state grammar, compilation, defaults | [api](api.md); input discovery and ceilings: [input](api/input.md) |
| One kind's fields | [environment](api/environment.md), [secrets](api/secrets.md), [machines](api/machines.md), [networks](api/networks.md), [infrastructure services](api/infrastructure-services.md), [container clusters](api/openshift-clusters.md), [storage](api/ceph.md#kinds), [add-ons](api/addons.md), [custom playbooks](api/custom-playbooks.md) |
| Delegated SSH sessions | [session access policy](api/session-access-policy.md) |
| Machine file transfer | [machine file copies](cli/machine-copy.md) |
| Command behavior and journeys | [cli](cli.md); flags: [commands](cli/commands.md); streams, JSON, diagnostics: [output](cli/output.md), [stops](cli/stops.md); cluster access: [access](cli/cluster-access.md), [administrator credentials](cli/cluster-admin-credentials.md), [sign-on suspension](cli/cluster-sign-on.md) |
| Host prerequisites, install and uninstall, the controller stage | [controller](controller.md) |
| Contexts, frozen input, storage, locking, deletion | [contexts](contexts.md); [controller record](contexts/controller-record.md); [log root](contexts/log-root.md) |
| Secret custody, bindings and reveal | [secrets](secrets.md) |
| Apply, destroy, continuation, ownership, mutation safety, GitOps readiness | [state reconciliation](state-reconciliation.md) |
| Managed services, host reservations, publication | [infrastructure services](infrastructure-services.md) |
| Provider hosts, machines, emulated BMCs, identity and power | [substrates](substrates.md) |
| Media store and operating-system installation | [managed OS](managed-os.md); Satellite content: [Satellite](managed-os-satellite.md), [subscription source](managed-os-subscription-source.md) |
| Container cluster installation | [container clusters](openshift-clusters.md) |
| OpenShift provider coverage and acceptance profiles | [cluster coverage](openshift-coverage.md) |
| Ceph lifecycle, native input, resources and release qualification | [storage](ceph.md), [native input](ceph-native-api.md), [resources](ceph-resources.md), [coverage](ceph-coverage.md), [subscription dependencies](ceph-subscription-dependencies.md) |
| Trust, sensitive material, processes, network, supply chain | [security](security.md) |
| The add-on boundary and lifecycle | [add-ons](add-ons.md) |
| Built-in add-on packages and acceptance profiles | [add-on coverage](add-on-coverage.md) |
| A section inside a large page: tasks, then each page's sections | [index of sections](index-sections.md) |
| The M2a Ceph API review, as history | [review](storage-api-review.md) |
| Delivery state, open items, delivered history, parked work, decisions | [milestones](milestones.md) and its pages, [delivered](milestones/delivered.md), [backlog](milestones/backlog.md) |
| Design for an unpromoted feature, only when reviving it | [deferred](deferred/) |

## Conventions

Each requirement has one owning section; other pages link to it. Specs state target
invariants in the present tense, name each rule's actor and claim no
availability. An unmet rule has one paragraph, `Not yet met: ...;
tracked as B<n>`, ending at its open item's link on a milestone page or the
backlog; a B link states availability nowhere else, except in a refusal
table's lifting list. Moving the item to
`awaiting operator acceptance` deletes or re-scopes the paragraph.
Delivery state lives in milestones, lessons in
[knowledge](../.agents/knowledge/index.md), procedure in skills. Add detail
with its first requested feature or a demonstrated ambiguity; an unpromoted
feature's design lives under `deferred/`.
