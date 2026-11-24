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

The **committed portfolio** is what a milestone page schedules, and every item
on those pages gates its milestone ([scope rules](milestones.md#scope-rules)):

- bare metal through BMCs ([M1e](milestones/m1.md#m1e), [M3d](milestones/m3.md#m3d)),
  libvirt with sushy-tools ([M1c](milestones/m1.md#m1c),
  [M3b](milestones/m3.md#m3b)), VMware vSphere ([M1f](milestones/m1.md#m1f),
  [M3f](milestones/m3.md#m3f)) and KubeVirt, including OpenShift
  Virtualization ([M1g](milestones/m1.md#m1g), [M3e](milestones/m3.md#m3e));
- machine operating systems that Bootwright installs from installer media,
  with entitlement use, first RHEL through Anaconda
  ([M1c](milestones/m1.md#m1c), [M1e](milestones/m1.md#m1e));
- infrastructure services: artifact serving, proxying, DNS, time
  synchronization and registries ([M1d](milestones/m1.md#m1d));
- OpenShift container clusters ([M3](milestones/m3.md));
- IBM Ceph storage ([M2](milestones/m2.md)); and
- cluster-bound integrations through built-in add-ons, first MetalLB, IBM
  Fusion Data Foundation, Red Hat Advanced Cluster Management, Red Hat
  OpenShift GitOps (Argo CD), GitLab, the Kubernetes NMState Operator and Red Hat
  OpenShift Virtualization ([M4](milestones/m4.md)), then custom add-ons ([B90](milestones/m4.md#b90)).

The **extension envelope** is what the design must not preclude and no
milestone gates on: OKD clusters ([B748](milestones/backlog.md#b748)), the
open-source and Red Hat Ceph distributions beyond IBM Ceph, machine operating
systems other than RHEL, a managed load balancer
([B100](milestones/backlog.md#b100)) and a cluster spanning substrate arms
([B747](milestones/backlog.md#b747)). Only the owner promotes an envelope entry
into the portfolio; no session clause does.

These lists bound Bootwright's intended scope; [milestones](milestones.md) own
what is delivered.

Bootwright owns the provider-host virtual networks that a managed
[`Network`](api/networks.md#libvirt-arm) declares, among them the
NetworkManager trunk and VLAN leg of a
[tagged one](api/networks.md#tagged-managed-networks) with the nftables table
that routes, translates and isolates its provider's legs, the network configuration it installs on machines and
nodes, the secondary networks of a cluster, and the managed infrastructure
services. It only verifies, and never changes, the physical fabric (switches,
VLAN trunking and routing), an external bridge, the host firewall's
configuration beyond the trusted-zone membership of a managed network's bridge
and of its own NetworkManager connections and its own nftables table, except on
a Machine whose firewall policy it manages
([host firewall](managed-os.md#host-firewall-requirements)),
and an external DHCP or DNS service; a provider's
[segment](api/machines.md#machine-profiles-and-segments) still names its
existing bridge ([substrates](substrates.md#provider-host-realization)). DNS is
a managed service or an external endpoint, as the Environment selects; artifact
serving is always managed
([ArtifactServer](api/infrastructure-services.md#artifactserver)).

Bootwright provisions and sets up only what GitOps cannot do or would have to
undo, and GitOps configures the rest on the operators Bootwright installs
([D246](milestones/backlog.md#d246)): what only an installer writes, such as
a node's network configuration, its addresses, the virtual IPs, the cluster
and service networks and the install-time OVN-Kubernetes settings; what a
Machine Bootwright provisions needs, such as the host secondary networks a
hosted cluster's virtual machines attach to; what is disruptive to change
later; and the checks that keep declared facts consistent, so a network only
GitOps attaches workloads to is still declared as a `Network`. Workload
networks and their segregation, ingress shards, egress addresses and network
policy are GitOps configuration. A cluster's secondary networks and MetalLB
pools are provisioning features an estate may select, never a segregation
guarantee. GitOps takes control of the downstream day-2 resources at the
[GitOps handoff](#lifecycle-and-handoff).

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
a change to it is a destroy followed by a fresh apply
([D153](milestones/backlog.md#d153)); approving an upgrade of an owned
operator subscription stays the owner's, and the
[replacement chain rule](add-ons.md#observation-and-continuation) bounds when an
upgrade the owner approves reads complete. An optional
[GitOps bootstrap object](add-ons.md#the-gitops-bootstrap-object) may hand one
cluster to GitOps earlier, under the same ownership rule.

## Implementation and extension model

Go owns product policy, typed models, deterministic compilation, planning,
implementation selection, lifecycle control and user-facing results. Ansible
implements bounded remote observation and effects through driven adapters.
Official installers, APIs, modules and tools own their native mechanisms;
Bootwright coordinates and verifies them under the
[architecture contract](architecture.md).

Built-in [add-on](add-ons.md) packages are reviewed Go code in the executable
that renders a closed cluster object program, run by one qualified
Bootwright-owned adapter; authored (custom) packages of the executable OLM
shape compile into the same program under the same trust contract; other
authored shapes refuse ([trust boundary](add-ons.md#trust-boundary)). They
configure cluster-bound capabilities; underlying cluster, storage and
substrate invariants retain their domain owners.
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

Bootwright has no production deployment. Until the owner explicitly declares it
production-ready and authorizes a different policy, all Bootwright-owned
versions stay fixed at their development baseline: internal request, evidence,
protocol, marker and durable-format versions stay at 1; the embedded collection
and input charts stay at `0.1.0`; the public input API stays at
`bootwright.io/v1alpha1` ([D267](milestones/backlog.md#d267)). Do not bump these
versions for schema, effect or implementation changes. External dependency,
tool, operating-system and product release pins keep their own versions;
commit identities and content digests continue identifying actual content.

Support only the current development shape. Add no backward-compatibility code,
converters, migrations, aliases, older readers, reader-version negotiation,
fallbacks for retired fields or tailored refusals for historical shapes.
Change code, schemas, examples, charts, tests and contract goldens together.
Strict decoding, canonical encoding, identity and ownership proofs, content
checks and refusal of unsupported input remain required. Contract goldens pin
the current shape and are explicitly reviewed and regenerated without a
version bump; they promise no compatibility across builds. A breaking change
still names its impact and remedy in its commit. Existing development stores
and installations may require finishing or removing with the build that
created them and then recreating with the current build; do not discard
ownership evidence or operate on hosts merely to reset a version. Production
upgrade, migration and backward-compatibility support require the owner's
explicit authorization after production readiness.

Bootwright's lifecycle is the operation modes that
[state reconciliation](state-reconciliation.md#lifecycle-unit) lists: bootstrap,
the [additive apply](state-reconciliation.md#additive-apply) of add-on
instances, exact continuation and removal of recorded owned state. Other day-2
operations, including ongoing drift repair and publication of application or
fleet GitOps content, are non-goals; adding one needs the owner's request and
its own specification ([D211](milestones/backlog.md#d211)).

The commands of the
[command catalog](cli/commands.md#command-and-flag-catalog) outside apply and
destroy are the exception to that non-goal; the catalog states each one's
effect, and none repairs drift of a lifecycle-owned resource or publishes ownership of a realized resource
([power operations](cli.md#machine-power-operations)).

Bootwright is not a general workflow or configuration-management engine, a
runner of operator-authored code inside a lifecycle operation, or a
continuously reconciling control loop.
