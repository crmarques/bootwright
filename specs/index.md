# Specification Index

Start with [project intent](project.md) when scope matters. Read the
[milestones](milestones.md) Status and Scope rules and, on the authorized
milestone page, only its opening, the Planned slices row, the item's row and
its `### B<n>` section ([AGENTS.md](../AGENTS.md)), then open only the owning
section below. A specified capability is not necessarily
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

## Sections of the largest domains

| Inside a large domain | Owning sections |
| --- | --- |
| Managed OS: installing one Machine | [shape](managed-os.md#supported-shape), [Kickstart](managed-os.md#derived-installation), [boot](managed-os.md#boot), [completion](managed-os.md#completion), [removal](managed-os.md#inverse), [network](managed-os.md#network-convergence), [FIPS](managed-os.md#fips-mode), [encryption](managed-os.md#root-disk-encryption), [registration](managed-os.md#registration), [physical](managed-os.md#physical-installation), [refusals](managed-os.md#refusal-table) |
| State reconciliation: stages, recovery, removal | [stages](state-reconciliation.md#stages-and-the-pause-boundary), [attempts](state-reconciliation.md#attempts-and-unknown-outcomes), [continuation and removal](state-reconciliation.md#continuation-and-removal), [state machine](state-reconciliation.md#state-machine) |
| Storage: bootstrap, resources, removal | [bootstrap and continuation](ceph.md#bootstrap-identity-and-continuation), [resources and access](ceph.md#resources-readiness-and-access), [removal](ceph.md#removal-and-concurrency) |
| CLI: lifecycle, access, inspection | [lifecycle](cli.md#lifecycle-behavior), [inspection and access](cli.md#resource-inspection-and-explicit-access), [power](cli.md#machine-power-operations) |
| Contexts: layout, locking, restore, deletion | [storage and locking](contexts.md#storage-locking-and-publication), [restore](contexts.md#format-and-restore-boundary), [deletion](contexts.md#permanent-deletion) |
| Substrates: host, Machine, identity | [host](substrates.md#provider-host-realization), [Machine](substrates.md#machine-realization), [physical](substrates.md#physical-machine-realization), [identity and power](substrates.md#identity-and-power-operations) |
| Which substrates and controllers are qualified | [qualification](substrates.md#qualification) |
| The M2a Ceph API review, as history | [review](storage-api-review.md) |

## Conventions

Each requirement has one owner; other pages link to it. Specs state target
invariants in the present tense and claim no availability. A rule the code does
not yet meet carries one line, "Not yet met: ...; tracked as B<n>", linking the
item on its milestone page or among the parked items.
Delivery state lives in milestones, observed lessons in
[knowledge](../.agents/knowledge/index.md), and working procedure in skills. Add
detail with its first requested feature or a demonstrated ambiguity; design for
an unpromoted feature lives under `deferred/`.
