# Architecture

Bootwright is organized as a desired-state loader, validator, renderer, and
idempotent apply pipeline.

## Layers

```text
YAML desired state
  -> load and strict decode
  -> normalize defaults
  -> validate ownership and references
  -> render effective state and tool inputs
  -> apply substrate, machine OS, storage, cluster, and add-on phases
```

`internal/README.md` maps these layers onto the Go package tree; the
per-package import matrix it describes is enforced structurally by
`internal/repo/checks`.

Apply and destroy execution record a durable run ledger under the context state
directory, including the CLI's exact resolved mutating invocation argv, and a
short-lived local lease for the process updating it. Cluster install
tasks also record per-cluster install state with a non-secret desired-input
fingerprint so repeated applies can skip completed installs and resume only
from known-safe phases. An `iso-created` record permits node boot only while its
publish time proves the agent ISO is less than 24 hours old; absent, future, or
expired evidence returns a typed cluster-scoped ISO-regeneration refusal before
any boot task can run. Ansible, `oc`, SSH, SCP, Ceph, and installer process
output produced by apply and destroy tasks is captured into root-managed run,
task, and cluster logs rather than streamed. The interactive passthrough verbs —
`container-cluster oc`, `container-cluster kubectl`, and the `rsh`/`exec` shells
under `cluster` and `machine` — stream the child process on the caller's stdio, the external process
passthrough exception in the `state-model.md` CLI Contract.

Context-backed bastion and OpenShift installer actions run on localhost.
Commands that need context data re-exec through `sudo` when necessary and store
runtime state under `/var/lib/bootwright`; only the caller's current context
selection remains in `~/.bootwright/contexts.yaml`.

OpenShift agent apply is scheduled as dependency stages: prepare provider and
machine infrastructure, create the cluster agent ISO with `openshift-install`,
boot each declared node through its rendered boot adapter as parallel node
tasks, then wait in two steps after every node boot task has completed:
`openshift-install agent wait-for bootstrap-complete`, which also publishes the
captured `kubeconfig` and `kubeadmin-password`, followed by
`openshift-install agent wait-for install-complete`. Post-install add-on apply
and node config apply are scheduled after the install wait, never after the
bootstrap wait ([ADR 0022](adr/0022-cluster-wait-bootstrap-boundary.md)).

Storage apply is a peer phase. For managed storage, Bootwright renders Ceph
tool inputs under `storage/<storageCluster>/`. The `machines` sub-phase (within
the `infra` family) prepares selected machines when needed. The `clusters` stage schedules an
Ansible storage task against a synthetic seed inventory entry, launches
`cephadm bootstrap` on the seed node, applies cephadm service specs, runs
topology and storage operations, and writes Data Foundation attachment records.
Imported storage clusters skip this storage task.

For KubeVirt children that reference a Bootwright-managed virtualization
cluster, the full graph and explicit parent+child `--clusters` selections add
graph edges from the child work to both the parent install wait and the parent
add-on wait task that provides `kubevirt`. VM infrastructure also waits for the
parent add-ons that declare its selected storage class and every network object
referenced by the machine-wide or per-interface KubeVirt attachments. Scoped
child applies do not expand the scope to install the parent; they fail before
mutation unless the parent is selected too or local runtime records prove the
parent install and KubeVirt add-on are ready.

Bootwright is the cross-cluster DAG orchestrator. The executor split is:
host and bastion mutations execute in Ansible; installed-cluster API
operations execute in Go through the `oc` command boundary
(`internal/addons/oc`); the `openshift-install` agent run stays in Ansible
because it is entangled with bastion machine state (controller DNS and
resolver stages). A new cluster-scoped executor follows this rule rather
than re-deciding the split. Provider, InfraComponent, machine-infrastructure, and
storage playbooks use Ansible-native parallelism, while Bootwright enforces
resource locks before launching concurrent playbooks: one mutating task per
provider or service machine until roles are classified more finely, and one
task per Redfish system or BMC target. KubeVirt-backed machine infrastructure,
managed-OS, and VM boot tasks lock
`kubevirt:<host-cluster-or-kubeconfig>:<namespace>:vm:<virtual-machine>`:
operations targeting the same VM serialize while independent VMs in one
namespace converge concurrently. Machine destroy uses one synthetic inventory
host per machine so independent VMs in the same child-before-host cluster pass
tear down concurrently; shared VIP preparation and ownership-record sweeps
remain serialized at the real provider-host boundary.

Absent an explicit cluster-install cap, the selected dependency DAG, resource
locks, and the global, per-host, and Redfish budgets are the only ordering
constraints. Independent bare-metal ContainerCluster roots may therefore enter
their install chains together while peer Ceph machine and storage work runs;
neither cluster kind imposes an undeclared edge on the other.

Add-on playbooks use a narrower dynamic lock after their read-only requirement
wait. A playbook whose resolved target includes a `StorageCluster` acquires all
of those clusters as `storage:<name>` resources for the playbook, output
capture, bound-cluster manifests, secret-output reclamation, and step-record
write. This serializes external-Ceph credential exports that share one provider
without serializing either add-on's operator install or readiness wait. Target
resolution and lock acquisition fail closed before the playbook; unrelated
storage targets and manifest-only steps remain concurrent.

Mutation-control values that cross from Go into Ansible — intent, positive
authorizations, resolved scope, and task execution selectors — are a named
contract. Every such value is registered by the Go convergence layer,
documented in the collection vars contract, and consumed by Ansible under the
same spelling; the registry guard rejects an unregistered producer, a missing
consumer, or an undocumented channel. A missing value must under-authorize or
narrow the run, never expand what the playbook may mutate.

The Ansible source tree is authored under `/ansible`. `make sync-bundle` packs
that source and pinned external collections into the generated embedded archive
under `internal/converge/bundle/ansible_bundle.zip`; `make build` runs that
sync before compiling the CLI. The generated archive is not versioned. Source
checkouts without the generated archive must still compile and report an empty
embedded bundle for commands that need Ansible until the operator runs
`make build`.

A top-level `add-ons/` directory embeds a built-in catalog of ready-made
`ClusterAddon` directories into the binary (`internal/addons/nativecatalog`
parses its `catalog.yaml` index). `bootwright add-ons add` vends a catalog
release into a machine-local store under the Bootwright root
(`/var/lib/bootwright/add-ons/<name>/`). The desired-state loader resolves a
binding or profile `addonRef` from the input tree first and, when no authored
`ClusterAddon` matches, from that store; the embedded catalog is the source
`add-ons add` copies into the store, not a load-time fallback of its own.
`context init` snapshots each referenced store add-on into the context input
tree, so a converged context is self-contained.

The desired-state API is defined in `api/v1alpha1` and specified in
`specs/state-model.md`.

Shared parsing and resolution must live behind one reusable package or adapter
before provider-specific roles consume it. ISO references are resolved by the
Bootwright managed media resolver; providers, OS installers, and future
user-supplied ISO fields must not duplicate `local-media:`, `file://`, or
HTTP(S) parsing.

## Ownership Boundaries

Per-kind ownership is summarized in `domain.md` (Operating Model) and specified
field by field in `state-model.md`. Every fact has one owner, and those
boundaries drive rendering:

- `install-config.yaml` is rendered from `ContainerCluster`, `Environment`,
  selected machines, machine `NetworkConfig` references, selected providers,
  endpoints, and platform render mode.
- `agent-config.yaml` hosts are rendered from `ContainerCluster.spec.nodes`,
  referenced `Machine` objects, `NetworkConfig` templates, and provider or
  generated substrate MAC inventory.
- Machine and endpoint provider variables resolve substrate network
  attachments from `Machine.spec.network.config.attachmentRef`, or KubeVirt
  per-NIC `interfaceAttachments[]`, to
  `InfraProvider.spec.networkAttachments[]`; KubeVirt VM inventory carries the
  resolved attachment on each rendered interface.
- Global boot-artifact and time-source fields are rendered from disconnected
  install mode, `ContainerCluster.spec.install.agent.bootArtifacts`, and
  resolved environment NTP source entries.
- Infra component variables are rendered from `InfraComponent` services
  referenced by cluster endpoints, environment catalog entries, and
  `NetworkConfig.spec.nameResolutionRefs[]`.
- Storage tool inputs render to cephadm host, core service, and late service
  specs; explicit operation metadata; Ansible storage contracts; and generated
  Data Foundation manifests for managed storage.
- Managed machine OS inputs render from `Machine`, `MachineImage`, and
  `MachineInstallProfile`, then reuse the same machine component, provider,
  BMC, virtual-media boot, and SSH trust contracts used by cluster node flows.
- Extension apply plans are rendered from `ClusterAddonBinding` expansion,
  `ClusterAddonProfile` order, and `ClusterAddon` generated resources or
  manifest paths. They do not mutate installer input.
- Rendering is a second enforcement line behind validation for name
  resolution: every render entry point fails before writing anything when an
  endpoint load-balancer bind or a managed Ceph topology node address does not
  resolve, instead of degrading to output with empty values.

Convergence is resumable by default. Every mutating workflow task runs under its
existing resource lock, derives a non-secret desired hash and Bootwright owner
identity, and writes a durable convergence-safety record. The records classifier
compares that recorded evidence against current desired state into the four
outcomes `diff --recorded` reports (`missing`/`foreign`/`match`/`drift`; defined
in the `state-model.md` CLI Contract).

A records-based apply-mode preflight uses that classification to fail closed on
structural (destructive-identity) drift or `foreign` before any mutation, for
every kind. Drift that is reconcilable in place converges on a bare `apply`, so
plain `apply` never destructively rebuilds and never touches foreign state. The
classification is not itself a per-task execution-time skip gate.

Once a run proceeds — a clean run, reconcilable drift, or `--mode rebuild` —
most provider-service and infra-component config tasks have no reliable external
probe: they re-run and rely on idempotent execution, and their record is marked
`unknown` (recorded but not classified) as durable evidence rather than an
apply-time skip.

Execution-time skip-vs-fail decisions live at the concrete-probe sites: cluster
install records, add-on records, managed OS markers, provider metadata, and
storage comparison results. Cluster install reconcile reads per-cluster install
records and probes live cluster availability, skips completed installs, resumes
only from known-safe phases, treats only a provably fresh `iso-created` record
as safe to boot, and fails closed when install state exists for
missing or different inputs after node boot unless a command-scoped
`--mode rebuild` is given. Destroy requires `--authorize protected` when selected state
sets `Environment.spec.safety.destroyProtection: protected`.

Convergence refusals never carry executable argv. They return a registered typed
remedy action with typed targets, and retain the observation that caused the
refusal on the owning error type. The CLI is the only layer that maps the action
onto a resolved invocation and therefore the only layer that may render an exact
`bootwright` retry; formatter coverage over the action registry and the
apply/destroy safety matrix keep that boundary closed over new actions and flags.
Protection remedies name only a validated unique set of machine-layer and
cluster-layer roles; the CLI projects fixed `infra`/`clusters` destroy stages
onto the original resolved object selection and never consumes backend names as
selection argv. The destructive apply-kind registry makes the layer choice
explicit and rejects an unclassified or retired task kind in guard tests.

Ownership evidence is a named cross-boundary contract: executing collection
roles write ownership through `bootwright.core.ownership_record` at mutation
time; Go reads those records for destroy scoping, host package removal gating,
orphan reporting, and `diff --recorded`, and stamps destroy-status attributes. The one
Go write is the partial-destroy path: after `--authorize unreachable-nodes` leaves a storage
cluster only partly torn down, Go stamps `destroyStatus=partial` onto that
StorageCluster's ownership record so it is not treated as fully gone. Run,
install, and convergence-safety ledgers remain Go-written.

Machine-hosted shared services add a cross-context control boundary. The
controller resolves the exact infra-component Kind+Name+Host consequence and
holds one root-global lease around sibling ownership inspection, host mutation,
and evidence cleanup, in addition to the context-local command lease. Executing
roles write a context claim before their first package/config/service mutation;
container roles stamp the same identity as a live label. A reference-only
destroy deletes only the role-qualified reference record, while owner teardown
fails closed on sibling or unreadable evidence unless destroy explicitly accepts
`shared-infra`.

Classification is additive-first: `diff --recorded` classifies only the resources
the loaded desired state currently declares, so an object removed from desired
state produces no `missing`/`match`/`drift`/`foreign` entry at all. It is not
invisible. A complementary orphan pass walks the ownership records and reports
every Bootwright-owned record whose machine, cluster, infra component, or
provider is no longer declared as `undeclared`. Kinds below that granularity —
pools, filesystems, gateways, exports, add-ons — carry no ownership record and
do fall out of enumeration entirely. The `undeclared` report is advisory: it
does not make the report out of sync and does not change the exit code, because
`apply` never deletes. Removing an object from desired state asks Bootwright to
stop managing it; reclaiming what it left behind is an explicit `destroy`. A
later same-name re-declaration can classify `match` against the stale
pre-destroy evidence until it is re-applied.

The default `diff` compares desired state against live reality rather than the
recorded evidence (`diff --recorded` is the offline classifier above). A
read-only discovery playbook runs a battery of `ceph … --format json` reads on
each managed Ceph seed; a leaf observation model decodes those blobs, and a
comparison engine diffs them field by field against the desired side derived
through the same storage-topology resolver the renderer uses, so an expected
value (a pool's effective replication, a rule's failure domain) matches exactly.
Container clusters get a shallow in-process `ClusterVersion` reachability check
against the stored kubeconfig — no deeper probe, since a container cluster
carries no declared quantitative expectation. `diff --adopt` folds the live
observation back into authored desired-state YAML through the single
input-mutation component that snapshots history first (also used by `context
update`); it edits declared objects in place and synthesizes files for
cluster-only pools, reporting anything it cannot safely represent rather than
dropping it.

Shared machine services are resolved through one service graph before
validation, rendering, status, or scoped apply checks make decisions about
them. The graph owns service identity, consuming clusters, machine placement,
conflict fields, and mergeable overlay fields.

## Providers

Provider adapters consume capability arms instead of inferring behavior from
names. Current provider capability arms include:

- machine profiles: `libvirt`, `vsphere`, `kubevirt`
- explicit machines: `baremetal`

All four arms are apply-supported today: each has an in-tree apply adapter and
substrate role. (`example init` prints the per-provider `apply support` line as
the runtime source of truth.) Adding a substrate means adding a capability arm,
validation, renderer support, and an apply adapter. It must not move physical
facts into cluster intent.

Adapters should use official CLI capabilities from the tools Bootwright drives
before adding custom orchestration around the same operation. For example,
OpenShift install completion remains delegated to
`openshift-install agent wait-for install-complete`.

Provider and BMC behavior must be handled by capability discovery and
advertised metadata before supplier-specific branching. Supplier-specific
workarounds are allowed only when discovery cannot express the behavior; keep
them isolated, minimal, tested, and documented in the knowledge base.

## Platform Rendering

`ContainerCluster.spec.install.platform.type` drives installer platform output.
It is the installer platform render mode, not the substrate type; substrate
ownership remains with selected machines and providers.

- `baremetal` renders bare-metal agent install platform data.
- `vsphere` renders vSphere platform data from selected profiles and optional
  node networking hints.
- `none` renders platform none for substrates where Bootwright only prepares
  machines.
- `external` is reserved for explicit external platform rendering.

Single-node cluster topologies render installer `platform.none` unless
`platform.type: external` is explicitly selected, because `openshift-install
agent` rejects bare-metal and vSphere installer platform blocks for one
control-plane and zero compute nodes.

## Testing

Schema changes require:

- focused validator tests for new and rejected fields
- fixture updates for generated desired-state examples
- renderer tests for install-config and agent-config output
- fixture-generation tests proving generated input sets load and validate
- stale-definition checks over docs, specs, examples, tests, and agent
  knowledge when terms move between kinds
