# Add-ons

An add-on is a package installed on a `ContainerCluster` that the context
builds. [The add-on API](api/addons.md) owns admission of `ClusterAddon`,
`ClusterAddonProfile` and `ClusterAddonBinding`; this page owns the boundary
those declarations cross and the lifecycle of the built-in packages a binding
selects. [Add-on coverage](add-on-coverage.md) lists each built-in package
with its objects, configuration, readiness and acceptance.

Not yet met: no capability realizes an add-on, so a selection containing one
refuses before registration under the
[unrealizable-kind rule](state-reconciliation.md#stages-and-the-pause-boundary);
tracked as [B83](milestones/m4.md#b83).

## Trust boundary

An authored add-on package is declarative data under the
[custom-code rule](security.md#custom-code). A built-in package's hooks are
pure Go reviewed with the executable, and its only remote effects are the
[cluster object adapter](#the-cluster-object-adapter)'s. Authors cannot
select executable code, drivers, inventory, privileges, retry policy or
lifecycle order. A step
that names a playbook, roles or collections is admitted as inert data and
makes its package lifecycle-ineligible regardless of origin. The
[compiler boundary](api.md#compiler-boundary) applies to declarations and
provenance markers.

Nothing is discovered ambiently. Working directories, `PATH`, environment
variables, user configuration, Ansible paths, network registries and global
plugin locations contribute no package, and desired state and package fields
cannot select a hidden catalog. The Environment's `.bootwright-addon` marker
grants only descriptor selection under
[Environment selection](api/environment.md#resource-and-cluster-selection); it
proves no signature, package version, manifest, compatibility or execution
authority.

Add-ons consume the published capabilities of Container cluster, Storage,
Machine and Secrets; they cannot bypass those invariants or invoke another
add-on as a workflow. `CustomPlaybook` is a separate
[reserved, non-executable surface](api/custom-playbooks.md).

## Built-in packages and selection

A built-in package is reviewed Go code released in the executable: a name of
at most nine bytes, the capability tokens it provides and requires (some
required only by a configuration, through a pure function), a default
readiness timeout, its OLM operator when it installs one, a closed
configuration schema, admission rules over that configuration and the bound
cluster's admitted facts, and a pure renderer that contributes operands,
Secret placements and readiness checks to a
[cluster object program](#the-cluster-object-program). The executable is its
only copy: no catalog file, registration or download exists, and a package
never names or carries code the adapter runs. No package registers a default
channel, CSV or catalog image, a version pairing or a compatibility
statement; the operator selects them (D163). A package's default catalog
source is the documented OLM catalog name only, never an image, tag or
channel, and its operator gate re-verifies the name; `fdf` and `gitlab` have
none and require `subscription.source`. Release differences in
mechanics are
[version specializations](architecture.md#implementations-and-version-variation).
A package provides a capability only when a rule in the tree reads it or the
[add-on API](api/addons.md#clusteraddon) defines its meaning.

The bound cluster's admitted facts are derived purely from the whole
effective catalog, one derivation per owning domain, so a package's rules can
check its configuration against addresses, exports and trust that other
declarations hold, never against a live system.

A binding selects a built-in package by naming it under
[`spec.packages`](api/addons.md#built-in-packages); the member's value is the
package's configuration. Package names and authored `ClusterAddon` names are
separate namespaces and nothing resolves one through the other, so neither
shadows the other. A package is bound to one cluster at most once across that
cluster's bindings. The capability graph spans the packages and authored
add-ons that every binding of the cluster selects. Each package owns its OLM
namespace, which no other package or Subscription shares.

A package may be a last package: its instance runs after every other block
that reaches its cluster's API, provides no capability, and is selected at
most once per cluster. The GitOps package is one, so Argo CD starts on a
cluster only after Bootwright's own objects there are complete.

A package's configuration may name a `StorageExport` it consumes. The
export, its storage cluster and their chain join the consuming cluster's
[selection closure](api/environment.md#resource-and-cluster-selection), the
instance requires the export's block, and one export is consumed by at
most one package instance across the input. The export's external-cluster
details reach the cluster only as an [owned Secret](#owned-secrets) placed
from the export block's produced material.

An authored `ClusterAddon` is admitted declaration data. A binding that
selects one, directly or through a profile, is refused at `plan` and `apply`
under the [refusal table](#refusal-table): authored packages need the
custom-package lifecycle of [B90](milestones/m4.md#b90). `ClusterAddon` and
`ClusterAddonProfile` are definitions that perform nothing by themselves; a
binding's selection is what has effects.

The [cluster network block](container-clusters.md#post-installation-network-configuration)
requires the instance that provides `nmstate`
([B735](milestones/m4.md#b735)), and the MetalLB package carries its own
address pools and advertisements as its configuration
([B727](milestones/m4.md#b727)), never the cluster's.

Not yet met: no binding selects a built-in package, the executable holds no
package, no package runs last, and `ClusterAddon` and `ClusterAddonProfile`
are still effect-bearing kinds; tracked as
[B752](milestones/m4.md#b752) and [B735](milestones/m4.md#b735).

Not yet met: no package configuration names a `StorageExport`; tracked as
[B760](milestones/m4.md#b760).

### Package qualification

Every built-in package passes, before it is offered: registry conformance
(name, namespace and OLM package unique; a provided token implies a readiness
check; its schema and its Go configuration type agree); purity and
determinism of its hooks; content safety of its rendered program at its
largest admitted configuration (no `Secret` object, reserved key, `status`,
managed fields or template delimiter, every Secret part used once, within the
bounds); an admission test per rule; render and plan goldens; a fake-cluster
journey (fresh apply, continuation `unchanged`, unknown resolved by
observation, a foreign object refused, a plan of another CSV never approved,
retirement contacting nothing); and parity with its coverage row. Its
submilestone's operator gate qualifies it on a real cluster.

Not yet met: no package is qualified; tracked as
[B752](milestones/m4.md#b752).

## The package listing

`bootwright add-ons list` reports each built-in package of the executable:
its name, provided and required capabilities, OLM package, default catalog
source and configuration members. It reads no context, file or network. No
command registers, replaces or deletes a package.

Not yet met: `add-ons list` is unavailable, and `add-ons add` and
`add-ons delete` are recognized; tracked as
[B754](milestones/m4.md#b754).

## Instances and blocks

Each package a binding selects is one instance, and each instance is one
block in the `add-ons` stage:

| Member | Value |
| --- | --- |
| Identity | `addon-<package>-<cluster>`; a package name has at most nine bytes, so every cluster whose installation block identity is valid yields a valid instance identity |
| Kind and object | `ClusterAddonBinding`, `<cluster>.<package>` |
| Implementation | `addon-instance-v1`, for every package |
| Requirements | the cluster's blocks; every instance on the same cluster that provides a capability the package requires; the installation block's `kubeconfig` as a [produced input](state-reconciliation.md#produced-material-custody); the objects the package's configuration references that another block realizes, with their produced material when a Secret placement takes it; and, for a [last package](#built-in-packages-and-selection), every other block of the `add-ons` stage that requires the same cluster's installation block: every other instance on that cluster and, when the cluster has one, its [cluster network block](container-clusters.md#post-installation-network-configuration) |
| Apply description and impact | `install add-on <package> on <cluster>`; `install-addon <package> <cluster>` |
| Removal description and impact | `retire add-on <package> with <cluster>`, saying its objects leave with the cluster's disks or stay on a cluster that keeps running; no impact |

Dependency edges order instances; the order of the instance function
(dependency order, then binding name, then package name) only breaks ties. The
request, version `addon-instance-request-v1`, freezes the cluster, binding and
package, the controller placement, the `oc` of the cluster's exact release, one
readiness budget in seconds, the rendered program, the names of the Secrets it
places and its produced inputs by producer kind, object and output, never
their bytes. The content digest covers the implementation, request, evidence
and program versions and the ownership and approval rule tags; a change to what
a package renders changes the request, so another build continues an operation
only when re-planning yields the same request.

An attempt runs under one absolute deadline, its start plus the budget, which
mutation consumes; the run deadline is the budget plus ten minutes, and
admission bounds a budget at `5h50m`. An instance is never added to or removed
from a completed apply, and repeating `apply` over a completed apply contacts no
cluster ([D153](state-reconciliation.md#lifecycle-unit)).

Not yet met: no capability plans an instance block; tracked as
[B83](milestones/m4.md#b83).

## The cluster object program

A program is ordered steps from a closed vocabulary: create an owned object,
place an [owned Secret](#owned-secrets), wait for an owned CatalogSource to
report `READY`, subscribe with an [approval policy](#olm-installation),
approve a dependent Subscription's first plan, and wait for
[checks](#readiness); readiness checks follow the last step. A step starts
only after the previous one proved its postcondition. A phase's gate checks
hold before any of its Secrets or objects is created, and a gate or check may
name an object the program never creates, such as a CustomResourceDefinition
or an operator's own resource; while waiting, such an object's absence is not
yet a result. An object entry holds its `apiVersion`, `kind`, namespace and
name, whether its kind is namespaced, and its canonical body. A program holds
at most 64 owned objects, 32 checks and 96 steps, and its request fits the
engine's request bound; planning refuses a program past a bound, naming it.
Bodies are canonical JSON and never hold a `Secret` object, Secret bytes,
`status`, managed fields, the owner annotation or a template delimiter.

The program is the one Kubernetes object port. A block that is not an add-on
instance, such as the
[cluster network block](container-clusters.md#post-installation-network-configuration),
freezes the same program section, version `cluster-objects-v1`, in its own
request and runs it through the same adapter. The section carries every
approval policy and Secret placement source the built-in packages use, so a
later package moves no program version.

Not yet met: no program type exists; tracked as
[B750](milestones/m4.md#b750).

## Ownership receipts

Bootwright creates an object only when it is absent and never patches,
applies over, adopts or deletes an existing one; the exceptions are the
[approvals](#olm-installation) of InstallPlans. Each created object carries
the owner annotation, its mark: the SHA-256 of the domain
`bootwright.cluster-objects.owner-v1`, NUL, the apply operation's identity,
NUL, the block's identity, NUL and the object's group, kind, namespace and
name, so the mark read from one object never matches another. Creation names
the field manager `bootwright-` followed by the first 32 hexadecimal digits of
the per-block value, the same SHA-256 without the object's identity. Both are
unique to the operation and stable across continuation, resolution and
context rename.

The frozen request names every object and its desired digest, the SHA-256 of
its canonical body, before any attempt runs. Completion evidence holds one
receipt per owned object: its identity, the `uid` its creating response
returned, its desired digest and whether it was created or found unchanged. A
present object is this block's own when its `uid` equals a receipt of the
block, or, when the block holds no receipt for it, its owner annotation equals
that object's mark and its server-set `metadata.creationTimestamp` is not
earlier than the operation's registration, less the clock-skew allowance the
program version fixes; any other present object is foreign and refuses,
naming its kind, namespace and name. Only a recorded `uid` proves ownership
outright; the mark decides only for an object whose receipt was lost with an
interrupted or failed attempt.

Not yet met: no receipt is recorded and nothing creates a cluster object;
tracked as [B750](milestones/m4.md#b750) and
[B751](milestones/m4.md#b751).

## The cluster object adapter

The adapter runs on the controller with the `oc` the controller stage
installed for the cluster's exact release and the kubeconfig
[custody](state-reconciliation.md#produced-material-custody) lends as a
material file, and bounds every request at 30 seconds. Two Bootwright modules
start only that `oc`, by argument list: a read module that only reads and a
write module that only creates objects and approves an InstallPlan. Every read
and create names the resource by kind, version and group, never by a bare
kind, plural or short name. For each owned object in program order it:

1. reads it by name; only the API's not-found answer is absence, and every
   other failed read is unknown, retried until the deadline before any
   mutation;
2. creates an absent object, carrying the owner annotation and the field
   manager, and keeps the response's `uid` as its receipt; a failed create is
   read again: own is its receipt, foreign refuses, and absent fails the
   attempt naming the object, except that a refusal the API classified, by its
   status reason and never its message, as `InternalError` or
   `ServiceUnavailable`, as when an admission webhook is not yet serving, is
   retried at the next poll until the deadline;
3. compares an own present object with its body: every declared field must be
   present with an equal value; a list must have the declared length and equal
   elements in order; a map may hold members the API or an operator added,
   except that an owned Secret's data key set and an RBAC object's `subjects`
   and `rules` must equal the declared ones exactly, and a Subscription's
   whole `spec` must equal the rendered spec, with no undeclared member, before
   any plan of it is approved; equal is `unchanged`, and a difference fails the attempt
   naming the object and the first differing field.

Every read and create projects only the fields the evaluators need: identity,
`uid`, `creationTimestamp`, the owner annotation, the declared fields and the
status members a check names. A Secret is created with output that carries
only its name and read through a projection of metadata, type and data key
names; the value comparison of a continuation runs inside the write module's
place action under `no_log` against the lent material and returns only
`equal` or the differing key, so no module result ever holds a Secret `data`
value. An InstallPlan is read through a projection of its `spec`, owner
references and `status.bundleLookups`, never `status.plan`.

Results name objects by kind, namespace and name only; no request body, API
response or material reaches evidence, logs or output. The adapter has no
removal entry point: an instance is [retired](#retirement).

Not yet met: no adapter reaches a cluster API; tracked as
[B751](milestones/m4.md#b751).

## OLM installation

A package that installs an operator creates its owned namespace, its
OperatorGroup, its owned CatalogSource in `openshift-marketplace` when the
configuration declares one (with the configuration's explicit priority, then a
wait for `READY`), and one Subscription with `installPlanApproval: Manual` and
the `channel` and `startingCSV` the configuration declares. Default
CatalogSources and `OperatorHub` are never changed.

The subscription step waits for the Subscription's InstallPlan and approves it
only when its owner references name the Subscription's `uid`, it asks for
manual approval, its CSV list is exactly the `startingCSV`, and every
`status.bundleLookups[].catalogSourceRef` names the Subscription's `source`
and `sourceNamespace`; the approval is
one JSON patch that first tests the plan's `uid`, CSV list and approval mode,
so the plan checked is the plan approved. A plan already approved that passes
the same tests is accepted on continuation; any other plan fails the attempt
naming its CSVs, and Bootwright never approves an upgrade plan. A Subscription
reporting `ResolutionFailed` fails the attempt at once, naming its message; that
the condition is never transient on a Subscription of a ready catalog is an
assumption, re-verified by [B755](milestones/m4.md#b755).

The operator is installed when the Subscription's installed CSV is the
`startingCSV` and that CSV reports `Succeeded`; the current CSV is reported,
never compared, and a pending upgrade waits for the operator. The evidence
records the approved plan, the installed CSV and its version, the catalog
image and the bundle image the plan resolved, and any maximum OpenShift
version the CSV declares, as evidence and never as a rule. An
operator-managed package also records the catalog of each later plan it
approves, from the plan's `bundleLookups`. Every native string recorded or
diagnosed follows the bound of [Readiness](#readiness).

A package whose operator approves the Manual InstallPlans of its own
namespace states the operator-managed approval policy and its reason in
its [coverage row](add-on-coverage.md#built-in-packages). Until the
Subscription's installed CSV is the `startingCSV`, Bootwright approves
only the plan the Subscription owns whose CSV list is exactly the
`startingCSV`, by the same tests, and fails a plan of its Subscription that
lists any other CSV beside it. Once that CSV is installed, Bootwright
approves nothing and fails on no plan: every later plan, including those of
the Subscriptions the operator creates, is the operator's. The package's
own checks then prove its operands. The evidence lists each InstallPlan
and CSV of the namespace, as evidence and never as a rule.

Some operators create another operator's Subscription themselves. A
package whose configuration fixes that Subscription's spec (the ACM hub's
multicluster engine) names it as a dependent: the adapter waits for it and
requires its `spec.name`, `channel`, `source`, `sourceNamespace` and
`startingCSV` to equal the values the configuration renders for it, with
`installPlanApproval: Manual`, and its server-set `creationTimestamp` to be
no earlier than that of the owned object whose operator creates it;
otherwise it fails, naming the Subscription and field. It approves the
dependent's first plan by the same tested patch, only when the plan belongs
to that Subscription, lists exactly that CSV and resolved its bundle from
that `source` and `sourceNamespace`. A
dependent Subscription is neither owned nor foreign; its installed CSV is
checked like the package's own, and its absence after a completed apply is
a partial realization. These approvals are the only writes to objects
Bootwright did not create.

Not yet met: no operator is installed; tracked as
[B751](milestones/m4.md#b751) and [B83](milestones/m4.md#b83).

Not yet met: every package uses exact approval; tracked as
[B759](milestones/m4.md#b759).

## Readiness

| Check | Holds when |
| --- | --- |
| `csvSucceeded` | the Subscription's installed CSV is the expected one (the declared `startingCSV`, or the CSV the check names for a dependent Subscription) and reports `Succeeded` |
| `condition` | the object's condition of that type has that status and, when the condition or the object reports an observed generation, it is the object's current generation; a declared failure condition fails the check at once, naming the object, condition and reason |
| `resourceExists` | the object is present; it need not be owned |
| `statusPhase` | a named status field holds one of the declared values; any other value, an unknown one included, is not ready |

All waits and checks of an attempt share its one absolute deadline. Checks
are read every ten seconds; a failed read while waiting is not yet a result,
and in an observation it is unknown. A condition unchanged long after its
object's other conditions is reported as stale and never decides. At the
deadline the attempt fails, naming each unmet check and its last observation,
and the next invocation retries. The authored `ClusterAddon` readiness API
keeps its three arms; `statusPhase` and failure conditions are program checks.

Every native string that reaches evidence or a diagnostic (a condition reason
or message, a last observation, an OLM resolution message, an image
reference, a CSV or plan name, a maximum OpenShift version) is bounded to 256
bytes, stripped of control characters, and recorded only after validation of
its grammar: an image reference by the reference grammar, a CSV or plan name
as a DNS subdomain, `olm.maxOpenShiftVersion` as `<major>.<minor>`. An invalid
value is recorded as `invalid` and never decides.

Not yet met: nothing evaluates add-on readiness; tracked as
[B751](milestones/m4.md#b751).

## Owned Secrets

A package places a Secret in its cluster only through a Secret placement: the
target namespace, name and Kubernetes type, and the mapping of the bound
Secret's parts to data keys, with optional labels and non-secret keys. The
apply binds the Secret, the attempt lends exactly the mapped parts, and the
adapter builds the object in memory and creates it as any owned object. A
placed part's bytes are copied exactly, multi-line documents included, and
never trimmed, re-encoded or parsed; a `tlsCertificate` Secret's certificate
and private key become a `kubernetes.io/tls` Secret's `tls.crt` and `tls.key`.
Its desired digest covers its metadata, type, key names and the values of its
non-secret keys, which are part of the canonical body, and never a lent value;
an apply continuation compares the lent values in memory and the non-secret
values, and reports only `unchanged` or the differing key, and an observation
compares type, key names and non-secret values, never a lent value. A
placement's namespace is one the program owns or one the package's registry
entry names as its operator's namespace (for `gitops`, `openshift-gitops`),
and its type is `Opaque` or `kubernetes.io/tls`; planning refuses any other
namespace or type naming the placement.
[Material placed in a cluster](secrets.md#material-placed-in-a-cluster)
states what remains after retirement. A global pull-secret merge is refused.

A placement may instead take a whole produced material entry that another
block proves, named by that block's kind, object and output, as one data
key. The instance names it as a
[produced input](state-reconciliation.md#produced-material-custody), the
attempt is lent the entry, and the digest, comparison and observation
rules above apply unchanged; an unproved entry is never placed. A produced
placement source is one of a closed set the program version names, today
only a `StorageExport`'s `external-details`. Planning refuses any other
producer kind or output, in particular every `kubeconfig` and `dashboard`
output, naming the placement.

Not yet met: no package places a Secret; tracked as
[B753](milestones/m4.md#b753).

Not yet met: no placement takes produced material; tracked as
[B760](milestones/m4.md#b760).

## Observation and continuation

An observation is read-only and reads every program object, the OLM state and
every check once: every object own and equal with every check holding is
completion; every object absent is no effect; some own objects with the rest
absent or not ready is a partial realization; a foreign object, a failed read
or an installed CSV other than the expected one is unknown, whose remedy is to
destroy and apply again. A continuation never re-renders: it runs the frozen
program, whose comparisons report what is done `unchanged`.
`bootwright diff --observe` reads the same observation and reports
differences, never repairs them.

Not yet met: no instance is observed; tracked as
[B83](milestones/m4.md#b83).

## Retirement

An instance is retired with its cluster and never removed online. Every
instance requires its cluster's installation block, and a removal holds every
block its apply started, so every removal that holds an instance also removes
that cluster from management, after the instance. The instance's removal runs
nothing and contacts no cluster: its removal completes, and its removal check
is answered, from its frozen request. A virtual cluster's objects leave with
its nodes' disks; a physical cluster keeps running after its context is
destroyed, with its add-on objects and placed Secrets, unmanaged, which the
removal's description states and its impacts omit. An effect outside the
cluster is never an add-on effect but a block of the domain that owns it,
with its own inverse. Removing one instance from a running cluster is parked
as [B769](milestones/backlog.md#b769).

Not yet met: no instance is retired; tracked as
[B83](milestones/m4.md#b83).

## GitOps handoff evidence

An instance's completion evidence names its package, cluster and provided
capabilities, every owned object by kind, namespace, name and `uid`, the
installed CSV and each readiness result; it names no Secret value and no
vendor payload. Handoff transfers no ownership: a GitOps source must exclude
what the evidence lists. The GitOps package may create one
[bootstrap object](#the-gitops-bootstrap-object).
[State reconciliation](state-reconciliation.md#bootstrap-completion-and-gitops-readiness)
owns the complete-environment predicate.

## The GitOps bootstrap object

The GitOps package's optional bootstrap creates, once, one Argo CD
`Application` named `bootwright-bootstrap` in the default instance's
namespace `openshift-gitops`, pointing at the repository, revision and path
the operator declares, with the cluster itself as its only destination, the
`default` project, the sync policy the configuration states and no
finalizer, so deleting it never deletes what it synchronized. A private
repository's credential reaches the cluster only as an
[owned Secret](#owned-secrets) carrying Argo CD's repository label; the
URL never carries credentials. When the configuration names a ClusterRole,
one owned ClusterRoleBinding grants it to the default instance's
application controller; otherwise Argo CD keeps the instance's built-in
permissions, which the upstream usage guide documents as not cluster
administration (the product CSV is re-verified by
[B766](milestones/m4.md#b766)). Granting a ClusterRole
gives every writer of the repository that role on the cluster; the
operator's declaration is that trust decision.

Bootwright never reads the repository, waits for or reports a sync result,
writes to a repository, enables Applications outside the instance's
namespace, or patches, adopts or deletes an Argo CD object it did not
create. Argo CD applies every manifest in its path, even one another tool
created: the operator's source must exclude every object the
[handoff evidence](#gitops-handoff-evidence) lists, the bootstrap object
included. A declared field Argo CD changes is reported by
`bootwright diff --observe` and never repaired. Because the GitOps package
is a last package, the object is created only after every other block that
reaches its cluster is complete; Argo CD may then act on that cluster
before the [handoff predicate](state-reconciliation.md#bootstrap-completion-and-gitops-readiness)
holds for the whole environment, and still owns nothing Bootwright
created. The object is retired with its cluster and keeps working on a
physical cluster that outlives its context. Bootwright publishes no
application or fleet GitOps content: it creates one pointer to content the
operator owns.

Not yet met: no package creates a bootstrap object; tracked as
[B765](milestones/m4.md#b765).

## Refusals

### Refusal table

Each row is one refusal of the add-on instance capability, and a refusal-table
test of that capability's package holds its `Unsupported` to it. Its reason
and remedy are the diagnostic's message and remediation, in which `<addon>` is
the refused ClusterAddon, `<binding>` the binding, `<package>` the package,
`<cluster>` the bound cluster and `<bound>` the bound passed.

| Refusal | Path | Reason | Remedy |
| --- | --- | --- | --- |
| An authored add-on | a `ClusterAddon` a binding selects, directly or through a profile | `<addon> is an authored add-on package, which needs the custom-package lifecycle this executable does not offer` | `select a built-in package under spec.packages of <binding>, or remove <addon> from its selection` |
| An oversized instance | a package whose rendered program for `<cluster>` passes a bound | `the program of package <package> for <cluster> passes its <bound>` | `reduce the configuration of package <package> in <binding>` |

Not yet met: nothing reports these refusals; tracked as
[B83](milestones/m4.md#b83).

## Design

Authored (custom) packages, custom catalogs, a versioned host interface for
drivers outside the executable, and step semantics remain in the
[add-ons design](deferred/add-ons-design.md), which
[B90](milestones/m4.md#b90) revives.
