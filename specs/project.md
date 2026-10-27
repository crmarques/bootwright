# Project

Bootwright is a declarative orchestrator for provisioning and bootstrapping a
cloud platform from scratch until it is ready for GitOps tools to take over
ongoing platform management.

Users describe one complete environment: substrate layer, machines and their
operating systems, infrastructure services, container and storage clusters,
and bootstrap integrations. Bootwright validates and freezes that intent,
orders dependencies, drives qualified native tools through controlled adapters,
and verifies the selected outcomes to make provisioning repeatable and
auditable.

Users author desired facts and relationships, not an execution sequence.
Lifecycle initiation, confirmation and irreversible authorization are explicit
operator actions.

## Product scope

The target platform families are:

- bare metal through BMCs, libvirt, VMware vSphere and KubeVirt, including
  OpenShift Virtualization;
- machine operating systems that Bootwright installs from installer media,
  with entitlement use, first RHEL through Anaconda;
- infrastructure services: artifact serving, load balancing, proxying, DNS,
  time synchronization and registries;
- OpenShift and OKD container clusters;
- open-source, Red Hat and IBM Ceph storage distributions; and
- cluster-bound integrations through built-in and custom add-ons, first
  MetalLB, IBM Fusion Data Foundation, Red Hat Advanced Cluster Management,
  Argo CD and GitLab.

These platform families bound Bootwright's intended scope;
[milestones](milestones.md) own what is delivered.

Bootwright owns the provider-host virtual networks that a
[Machine's network attachment](api/machines.md#machine-profiles-and-network-attachments)
declares as managed, the network configuration it installs on machines and
nodes, the secondary networks of a cluster, and the managed infrastructure
services. It only verifies, and never changes, the physical fabric (switches,
VLAN trunking and routing), an external bridge, the host firewall's
configuration beyond the trusted-zone membership of a managed network's bridge,
and an external DHCP or DNS service; an external attachment still names its
existing bridge ([substrates](substrates.md#provider-host-realization)). DNS and
artifact serving are each a managed service or an external endpoint, as the
Environment selects.

## Lifecycle and handoff

Bootwright realizes the declared, owned scope of one frozen environment in a
bounded operation. Every selected outcome is positively verified or fails
closed. After durable registration, interruption or failure preserves the
evidence needed for exact continuation of that operation.

The [state contract](state-reconciliation.md#lifecycle-unit) owns
full-environment apply, destroy, continuation and ownership, including when a
completed apply must be destroyed before another. Edited intent is never
treated as continuation.

The [GitOps handoff gate](state-reconciliation.md#bootstrap-completion-and-gitops-readiness)
requires durable completion, readiness, access and ownership evidence for the
entire selected bootstrap scope. GitOps may then control downstream day-2
resources, without co-managing Bootwright-owned resources. Downstream day-2
resources are those that no owned-object evidence
([add-ons](add-ons.md#gitops-handoff-evidence)) lists. Handoff does not
transfer, adopt or reclaim ownership, so an owned object stays as applied and
a change to it is a destroy followed by a fresh apply; upgrades and approvals
of owned operator subscriptions stay outside Bootwright's scope
([B106](milestones/backlog.md#b106)). An optional
[GitOps bootstrap object](add-ons.md#the-gitops-bootstrap-object) hands one
cluster to GitOps earlier, once every other block of the `add-ons` stage that
requires that cluster's installation block, its other add-on instances and
its cluster network block included, is complete; a block of another stage
that reaches the cluster's API is not waited for
([last packages](add-ons.md#built-in-packages-and-selection)). It hands off
under the same ownership rule; this early handoff is
pending the owner's [Q60](milestones/m4.md#m4a-open-decisions) under D173.

## Implementation and extension model

Go owns product policy, typed models, deterministic compilation, planning,
implementation selection, lifecycle control and user-facing results. Ansible
implements bounded remote observation and effects through driven adapters.
Official installers, APIs, modules and tools own their native mechanisms;
Bootwright coordinates and verifies them under the
[architecture contract](architecture.md).

Built-in [add-on](add-ons.md) packages are reviewed Go code in the executable
that renders a closed cluster object program, run by one qualified
Bootwright-owned adapter; authored (custom) packages are declarative data
refused until [B90](milestones/m4.md#b90), which will compile them into the
same program under the same trust contract. They configure cluster-bound
capabilities; underlying cluster, storage and substrate invariants retain
their domain owners.
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

Never depend on an external location after admission: a command that admits
input copies it into the Bootwright store, later commands read only that copy,
and a recorded source location is provenance, never identity, uniqueness or a
read path.

Before 1.0, a durable format change converts no earlier format: a record in an
earlier format is refused, and the refusal names the remedy.

Day-2 operations are outside the current scope, including ongoing drift repair
and publication of application or fleet GitOps content. Bootwright's lifecycle
remains limited to bootstrap, exact continuation and removal of recorded owned
state. Selected day-2 operations may be added in the future when requested and
explicitly defined.

The admitted operator utilities are the exception: `machine start`, `stop` and
`restart`; `rsh` and `exec` sessions on Machines and clusters; `machine trust`;
`diff`; `status`; the `info` commands; `cluster oc` and `cluster kubectl`; and
the `cluster kubeconfig`, `cluster keyring` and `secret show` disclosures. Each
registers no lifecycle operation, publishes no ownership and never repairs
drift ([commands](cli/commands.md#command-and-flag-catalog),
[power operations](cli.md#machine-power-operations)).

Bootwright is not a general workflow or configuration-management engine, a
runner of operator-authored code inside a lifecycle operation, or a
continuously reconciling control loop.
