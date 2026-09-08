# Project

Bootwright is a declarative orchestrator for provisioning and bootstrapping a
cloud platform from scratch until it is ready for GitOps tools to take over
ongoing platform management.

Users describe one complete environment: substrate layer, machines,
infrastructure services, container and storage clusters, and bootstrap
integrations. Bootwright validates and freezes that intent, orders dependencies,
drives qualified native tools through controlled adapters, and verifies the
selected outcomes to make provisioning repeatable and auditable.

Users author desired facts and relationships, not an execution sequence.
Lifecycle initiation, confirmation and irreversible authorization are explicit
operator actions.

## Product scope

The target platform families are:

- bare metal through BMCs, libvirt, VMware vSphere and KubeVirt, including
  OpenShift Virtualization;
- infrastructure services: artifact serving, load balancing, proxying, DNS,
  time synchronization and registries;
- OpenShift and OKD container clusters;
- open-source, Red Hat and IBM Ceph storage distributions; and
- cluster-bound integrations, including IBM Fusion, IBM Fusion Data Foundation
  and Red Hat OpenShift Data Foundation, plus built-in and custom add-ons.

These platform families describe Bootwright's intended scope. Individual
capabilities become available as they are implemented and tested.

## Lifecycle and handoff

Bootwright realizes the declared, owned scope of one frozen environment in a
bounded operation. Every selected outcome is positively verified or fails
closed. After durable registration, interruption or failure preserves the
evidence needed for exact continuation of that operation.

The [state contract](state-reconciliation.md) owns full-environment apply,
destroy, continuation and ownership. A completed apply must be destroyed before
another apply. Edited intent is never treated as continuation.

The [GitOps handoff gate](state-reconciliation.md#bootstrap-completion-and-gitops-readiness)
requires durable completion, readiness, access and ownership evidence for the
entire selected bootstrap scope. GitOps may then control downstream day-2
resources, without co-managing Bootwright-owned resources. Handoff does not
transfer, adopt or reclaim ownership.

## Implementation and extension model

Go owns product policy, typed models, deterministic compilation, planning,
implementation selection, lifecycle control and user-facing results. Ansible
implements bounded remote observation and effects through driven adapters.
Official installers, APIs, modules and tools own their native mechanisms;
Bootwright coordinates and verifies them under the
[architecture contract](architecture.md).

[Add-ons](add-ons.md) are immutable declarative data packages executed by
qualified Bootwright-owned drivers. Built-in and custom packages share the
same trust and lifecycle contract. They configure cluster-bound capabilities;
underlying cluster, storage and substrate invariants retain their domain owners.
There is no ambient plugin path or dynamic native-code loading.

`CustomPlaybook` is a reserved authoring shape, with no execution authority.
Executable custom automation requires a separately specified typed, invertible
replacement contract.

Authored desired state is the source of truth. Effective state, native files,
scripts, inventories, plans and evidence are generated outputs. Independently
running a generated artifact creates no Bootwright operation or ownership.

## Design priorities and non-goals

Keep intent typed, declarative and provider-neutral; outputs deterministic;
domain ownership explicit; effects least-privileged and fail-closed; and
continuation evidence durable. Reuse trusted dependencies and native
interfaces under the [dependency selection rule](architecture.md#dependency-selection-and-reuse).
Keep operator output concise and actionable, with protected troubleshooting
detail. Introduce abstractions and extension points only for real consumers.

Day-2 operations are outside the current scope, including ongoing drift repair
and publication of application or fleet GitOps content. Bootwright's lifecycle
remains limited to bootstrap, exact continuation and removal of recorded owned
state. Selected day-2 operations may be added in the future when requested and
explicitly defined.

Bootwright is not a general workflow or configuration-management engine, an
arbitrary-code runner, or a continuously reconciling controller.
