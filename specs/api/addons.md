# Cluster add-ons, profiles, and bindings

This page owns `ClusterAddon`, `ClusterAddonProfile`, and
`ClusterAddonBinding`. An authored add-on, a `ClusterAddon`, is one reusable
`olm` or `manifestSet` declaration ([shared definition](../add-ons.md)); a
[built-in package](#built-in-packages) is an add-on compiled into the executable
and selected by name. Profiles compose
authored add-ons and other profiles. Bindings attach the expanded set and any
built-in packages to one `OpenShiftCluster`, supplying scalar input values to
authored add-ons and typed configuration to packages.

Each of these kinds names its objects under the [one name rule](../api.md#object-names).

[The add-on boundary](../add-ons.md) owns what these declarations may never
grant, the [add-on lifecycle](../add-ons.md#built-in-packages-and-selection)
owns built-in packages, and the [add-ons design](../deferred/add-ons-design.md)
records what a custom-package lifecycle would add. Schema
acceptance implies no execution support; all declarations obey
[the compiler boundary](../api.md#compiler-boundary).

References are plain scalar names in fixed namespaces: `clusterRef` names a
`OpenShiftCluster`, `profileRefs` name `ClusterAddonProfile` objects,
`addonRefs` and `addonRef` name `ClusterAddon` objects, and `secretRefs` name
`Secret` objects, each effect material that a binding places in its cluster as
an [owned Secret](../add-ons.md#owned-secrets). An input's `resourceKind` or `secretType` constrains its
binding value as described below. [Environment kind defaults](environment.md#kind-defaults)
may supply only the fields permitted by that closed contract; this page owns
their kind-specific validation and built-in defaults.

## Kinds

| Kind | Declares |
| --- | --- |
| `ClusterAddon` | One authored add-on definition; a reserved shape that validates but never runs. |
| `ClusterAddonProfile` | One reusable selection of add-on configuration bound to clusters. |
| `ClusterAddonBinding` | The add-ons of one cluster: its built-in packages and authored add-on bindings. |

## ClusterAddon

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.provides` | array of strings | no | `[]` | Set of add-on capability tokens. |
| `spec.requires` | array of strings | no | `[]` | Set of add-on capability tokens. |
| `spec.inputs` | array of objects | no | `[]` | Accepted inputs, keyed by unique `name`. |
| `spec.olm` | object | conditional | — | Exactly one of `olm` and `manifestSet` is present. |
| `spec.manifestSet` | object | conditional | — | Exactly one of `olm` and `manifestSet` is present. |
| `spec.readiness` | object | no | — | Timeout and readiness checks. |
| `spec.steps` | array of objects | no | `[]` | Ordered step declarations, admitted and [inert](#steps). |

The arm selects the add-on. Missing, multiple, or
unknown arms are invalid; the selected arm must satisfy its required fields.
`spec.type` and `spec.accepts` are unknown fields, not compatibility aliases.

Add-on capability tokens match `^[A-Za-z0-9][A-Za-z0-9._-]*$` and are unique
in each list. They name what an add-on or package provides to a cluster, not a
lifecycle capability. `provides` and `requires` are not
closed enums. The names `kubevirt`, `dataFoundation`, `metallb` and `nmstate`
have defined downstream meanings, but all valid tokens participate in
dependency ordering. A built-in package
provides `metallb` when it answers `LoadBalancer` Service addresses
([package metallb](#package-metallb)); a package or add-on that needs such an
address requires it. An add-on with any `provides` value requires at least one
effective readiness check after the readiness defaults below apply.

### Accepted inputs and effects

`spec.inputs` is an array with set semantics keyed by author-defined `name`.
Each entry is:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Unique within the add-on. |
| `resourceKind` | string | conditional | — | Exactly one of `resourceKind` and `secretType`; names a registered kind other than `Secret`. |
| `secretType` | string | conditional | — | Exactly one of `resourceKind` and `secretType`; one of the [Secret types](secrets.md#secret). |
| `required` | boolean | no | `true` | Every binding must supply a value when true; explicit false makes the input optional. |
| `effects` | array | no | `[]` | Each entry is one effect union below. |

Names do not determine input types or effects. `resourceKind: Secret` is
invalid; Secret inputs use `secretType`, and a bound Secret input is effect
material its binding places. The former `resourceRef` and `secretRef` input declaration
fields are unknown.

Effects declare how a supplied input is consumed. Their names belong to a
closed vocabulary, unlike input names. Each effect entry sets exactly one
known arm:

| Arm | Shape | Rule |
| --- | --- | --- |
| `cephConsumerAttachment` | `{}` | Input is `resourceKind: CephConsumer` and the add-on provides `dataFoundation`; the bound consumer supplies storage integration for the binding's cluster. |
| `globalPullSecretMerge` | `{registry, username}` | Input is `secretType: token`; both strings are non-empty. The token supplies credentials for that registry and username in the binding's cluster global pull secret. |

Validation checks effect compatibility and resolves supplied object names.
The storage attachment also defines the relationship used by
[Environment cluster selection](environment.md#cluster-selection).
An empty effect list declares no such built-in relationship or operation.
Effects never grant execution authority. The
[add-on lifecycle](../add-ons.md#refusal-table) refuses every bound authored
add-on, and so every effect it declares; the
[closed execution vocabulary](../deferred/add-ons-design.md#closed-execution-vocabulary)
records what a custom-package lifecycle would add.

### OLM arm

`spec.olm` contains:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `namespace.name` | string | yes | — | Kubernetes namespace. |
| `namespace.management` | string | no | `managed` | `managed` requests namespace creation; `external` selects an existing externally managed namespace. |
| `namespace.labels` | map of string to string | no | `{}` | Kubernetes label keys and values. |
| `operatorGroup` | object | no | — | Optional OperatorGroup. |
| `operatorGroup.name` | string | no | `namespace.name` | Applies only when the OperatorGroup block is present. |
| `operatorGroup.targetNamespaces` | array of strings | no | `[namespace.name]` | Ordered non-empty namespace strings; an authored list replaces this default. |
| `catalogSource` | object | no | — | Optional shipped CatalogSource. |
| `catalogSource.name` | string | conditional | — | Required when the block is present. |
| `catalogSource.image` | string | conditional | — | Required when the block is present. |
| `catalogSource.displayName` | string | no | — | Optional display name. |
| `catalogSource.publisher` | string | no | — | Optional publisher. |
| `catalogSource.pollInterval` | string | no | — | Valid Go duration. |
| `catalogSource.grpcPodConfig.securityContextConfig` | string | conditional | — | `legacy` or `restricted` when the block is present. |
| `subscription.name` | string | no | `subscription.package` | Kubernetes Subscription name. |
| `subscription.package` | string | yes | — | OLM package. |
| `subscription.channel` | string | yes | — | OLM channel. |
| `subscription.startingCSV` | string | no | — | Optional starting CSV. |
| `subscription.source` | string | yes after normalization | `catalogSource.name` when shipped | CatalogSource name. |
| `subscription.sourceNamespace` | string | yes after normalization | `openshift-marketplace` | CatalogSource namespace. |
| `subscription.installPlanApproval` | string | yes after normalization | `Automatic` | `Automatic` or `Manual`. |
| `customResources` | array of objects | no | `[]` | Raw Kubernetes resources preserved as structured maps. |

The authored `subscription` defaults above (`installPlanApproval`
`Automatic`, an optional `startingCSV`) are not the rules of the package
[Subscription](#subscription), which requires `startingCSV` and approves only
that CSV. No authored add-on runs, because the
[lifecycle](../add-ons.md#refusal-table) refuses every binding that selects one.

Not yet met: the authored defaults are not aligned with the package
Subscription block, which the custom-package lifecycle of
[B90](../milestones/m4.md#b90) requires before any authored add-on runs;
tracked as B90. The full-platform nmstate example says so in a note.

Omitting `operatorGroup` requests no group. `operatorGroup: {}` requests the
namespace-named group targeting that namespace. An explicit `targetNamespaces`
list remains as authored, including an empty list. Empty authored names are
invalid, not a request for a default. `namespace.create` is unknown.

A shipped catalog requires both name and image, and subscription source must
match its name. When a shipped `catalogSource` is present and
`namespace.management` is `managed`, `subscription.sourceNamespace` must differ from
`namespace.name` because the CatalogSource namespace must already exist. Each
custom resource requires string `apiVersion`, string `kind`, and
`metadata.name`. A `kind: Secret` custom resource cannot carry `data` or
`stringData` because authored desired state never embeds secret bytes. Other
custom-resource keys remain an explicit open Kubernetes payload; the
containing Bootwright schema remains closed.

### Manifest-set arm

`spec.manifestSet.manifests` is a required non-empty ordered array of
`{path: <string>}`. A path is non-empty, has no leading or trailing
whitespace, is relative to the declaring add-on, remains below that directory,
has a `manifests` path segment, and has a case-insensitive `.yaml` or `.yml`
suffix.

Validation checks paths lexically. Materialization follows the add-on
contract's package, native-schema and content-safety gates.

### Readiness

`readiness.timeout` is a positive Go duration and normalizes to `30m`.
`readiness.checks` is an ordered array. If it is still omitted on an OLM add-on
after Environment defaults, built-in normalization supplies one `csvSucceeded`
check with `namespace` equal to `olm.namespace.name` and `subscription` equal
to the effective `olm.subscription.name`. This applies when `readiness` is omitted too.
Manifest-set add-ons default to no checks.

An authored check list replaces the default completely; no hidden check is
appended. An explicit `[]` selects no checks and therefore fails when
`provides` is non-empty. These defaults apply only to top-level
`readiness.checks`, not to a step's `requires` list. Every authored check must
supply its arm's required fields; no missing identity is inferred inside it.
Each check sets exactly one arm:

| Arm | Exact fields |
| --- | --- |
| `csvSucceeded` | required `namespace` and `subscription` |
| `condition` | required `apiVersion`, `kind`, `name`, and `condition: {type, status}`; optional `namespace` |
| `resourceExists` | required `apiVersion`, `kind`, and `name`; optional `namespace` |

### Steps

`spec.steps` is admitted and inert. Admission validates each step's closed
shape and its references as the [add-ons design](../deferred/add-ons-design.md#steps)
records it: an ordered set keyed by unique `name`, a playbook or manifest
declaration, a contained content path, a Bootwright-owned target selection,
and no `source.git`, reserved connection variable or controller target. No
lifecycle, renderer or command reads a step, so `gates`, `follows`, `outputs`,
`reclaimRendered` and manifest template tokens have no defined meaning and
their consumer defaults never materialize. A step grants no execution
authority under the [add-on boundary](../add-ons.md#trust-boundary).

## ClusterAddonProfile

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.profileRefs` | array of strings | no | `[]` | Ordered `ClusterAddonProfile` refs, expanded first. |
| `spec.addonRefs` | array of strings | no | `[]` | Ordered `ClusterAddon` refs, appended after nested profiles. |

At least one list is non-empty. References resolve in their fixed namespaces
and profile cycles are rejected. Expansion is depth-first in authored order;
an add-on reached more than once is retained at its first occurrence and later
occurrences are dropped.

## ClusterAddonBinding

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.clusterRef` | string | yes | — | One `OpenShiftCluster`. |
| `spec.profileRefs` | array of strings | no | `[]` | Ordered profiles expanded first. |
| `spec.addonRefs` | array of strings | no | `[]` | Ordered direct add-ons appended afterward. |
| `spec.addonConfigs` | array of objects | no | `[]` | Set keyed by `addonRef`; supplies inputs but does not select an add-on. |
| `spec.packages` | object | no | `{}` | Closed record whose optional members are [built-in package](#built-in-packages) names; a present member selects that package and holds its configuration. |
| `spec.afterRefs` | array of strings | no | `[]` | Set of other `ClusterAddonBinding` refs of the same cluster; everything this binding selects runs after everything they select. |

At least one profile, direct add-on or package is selected. Every config has:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `addonRef` | string | yes | — | A selected `ClusterAddon`. |
| `inputs` | array of objects | no | `[]` | Set keyed by input `name`. |
| `inputs[].name` | string | yes | — | Must be declared by the selected add-on. |
| `inputs[].value` | string | yes | — | Non-empty scalar resource or Secret name. |

For a `resourceKind` input, `value` resolves to the declared kind. For a
`secretType` input, it resolves to a `Secret` with that exact type. Every
required accepted input is supplied exactly once and undeclared inputs are
rejected. These remain generic named lists: input names and values are not
add-on-specific schema fields. Input order does not define installation order.

Only one binding may apply a given add-on to a given cluster after profile
expansion. A selected add-on's required capability tokens must be provided by
another selected add-on; the resulting `requires`/`provides` graph must be
acyclic. Config entries never select otherwise absent add-ons.

A package is bound to one cluster at most once across that cluster's
bindings, and packages take part in the capability graph with the add-ons
the cluster's bindings select.

`spec.afterRefs` authors ordering the capability graph does not derive: every
package and add-on the binding selects runs after every one each named binding
selects, and planning adds each such edge beside the derived ones, so an
instance requires the instances of the bindings it names. A named binding
binds the same cluster. The authored edges, the capability edges and a
[last package](../add-ons.md#built-in-packages-and-selection)'s edges together
form one acyclic order: naming the binding that selects a last package is a
cycle, because the last package already runs after everything else.

## Admission diagnostics

Add-on and package diagnostics follow the style of every other kind: a
lowercase message with no final period that names what it concerns, an
indexed field, and a remedy. Each fault is reported once, as its own
diagnostic, so several faults of one binding never merge. Indexes are the
authored positions in the document, as
[the refusal rule](../api.md#yaml-streams-and-decoding) states for every list.

| Fault | Code | Field | Message | Remedy |
| --- | --- | --- | --- | --- |
| A required input is missing | `api.required` | `$.spec.addonConfigs[<i>].inputs`, or `$.spec.addonConfigs` when the binding has no entry for the add-on | `required input <input> of ClusterAddon/<addon> is not supplied` | `add an input named <input> to the spec.addonConfigs entry of <addon>, naming a <kind>` (or `naming a Secret of type <secretType>`) |
| An input names no object of its kind | `api.reference` | `$.spec.addonConfigs[<i>].inputs[<j>].value` | `input <input> of ClusterAddon/<addon> names <value>, which is not a <kind> in the selected input` (or `which is not a Secret of type <secretType> in the selected input`) | `set the value to the name of a <kind> declared in the selected input` (or `of a Secret of type <secretType>`) |
| An input is not declared | `api.field` | `$.spec.addonConfigs[<i>].inputs[<j>].name` | `ClusterAddon/<addon> declares no input named <input>` | `remove the input, or use an input <addon> declares: <names>` (at most eight names, then `and <n> more`; `remove the input` when it declares none) |
| A profile reaches a missing add-on or profile | `api.reference` | `$.spec.profileRefs[<i>]` | `profile <profile> expands to <Kind>/<name>, which is not in the selected input` | `declare <Kind>/<name>, add its file to the selection, or remove it from ClusterAddonProfile/<owner>` |
| A required capability has no provider | `api.reference` | the selecting entry: `$.spec.addonRefs[<k>]`, `$.spec.profileRefs[<i>]` through which the binding reaches the add-on, or `$.spec.packages.<package>` | `<subject> requires capability <capability>, which nothing else selected for OpenShiftCluster/<cluster> provides` (`<subject>` is `ClusterAddon/<addon>` or `package <package>`) | `select a package or add-on that provides <capability> for OpenShiftCluster/<cluster>, or remove <subject> from the selection` |
| Capability requirements form a cycle | `api.invariant` | the selecting entry of the cycle's first member in canonical order | `capability requirements of the packages and add-ons selected for OpenShiftCluster/<cluster> form a cycle through <member list>` | `remove one requires entry among <member list> to break the cycle` |
| An add-on is bound twice to one cluster | `api.duplicate` | the selecting entry in the later binding in canonical order | `ClusterAddon/<addon> is already bound to OpenShiftCluster/<cluster> by ClusterAddonBinding/<binding>` | `bind <addon> to <cluster> through one ClusterAddonBinding only` |
| A package is bound twice to one cluster | `api.duplicate` | `$.spec.packages.<package>` of the later binding in canonical order | `package <package> is already bound to OpenShiftCluster/<cluster> by ClusterAddonBinding/<binding>` | `select package <package> for <cluster> in one ClusterAddonBinding only` |
| Two last packages on one cluster | `api.invariant` | `$.spec.packages.<package>` of the later binding in canonical order | `packages <first> and <second> both run last on OpenShiftCluster/<cluster>` | `select one of <first> and <second> for <cluster>` |
| A configuration names an unselected add-on | `api.reference` | `$.spec.addonConfigs[<i>].addonRef` | `ClusterAddon/<addon> is configured but not selected by this binding` | `select <addon> through spec.addonRefs or a profile, or remove this spec.addonConfigs entry` |
| A binding selects nothing | `api.invariant` | `$.spec` | `the binding selects no profile, add-on or package` | `add a package to spec.packages, a profile to spec.profileRefs or an add-on to spec.addonRefs` |
| Profile expansion returns to a profile | `api.invariant` | `$.spec.profileRefs[<i>]` | `profile expansion returns to ClusterAddonProfile/<profile>` | `remove the reference to <profile> from this list or from a profile it expands` |
| An ordering names a binding of another cluster | `api.invariant` | `$.spec.afterRefs[<i>]` | `spec.afterRefs names ClusterAddonBinding/<other>, which binds OpenShiftCluster/<other cluster>, not OpenShiftCluster/<cluster>` | `name a binding of OpenShiftCluster/<cluster>, or remove <other> from spec.afterRefs` |
| Bindings order each other in a cycle | `api.invariant` | `$.spec.afterRefs[<i>]` of the cycle's first binding in canonical order, at its first entry on the cycle | `spec.afterRefs of the bindings of OpenShiftCluster/<cluster> form a cycle through <binding list>` | `remove one spec.afterRefs entry among <binding list> to break the cycle` |
| Authored ordering closes a cycle with capability or last-package edges | `api.invariant` | `$.spec.afterRefs[<i>]` of the first cycle member in canonical order with an authored edge on the cycle | `ordering of the packages and add-ons selected for OpenShiftCluster/<cluster> forms a cycle through <member list> once spec.afterRefs applies` | `remove one spec.afterRefs or requires entry among <member list> to break the cycle` |

A `spec.afterRefs` entry naming no binding is reported by the reference rule
at its own indexed field. A binding cycle is reported instead of, never beside,
the member cycle it would also cause. A missing capability and a cycle are reported once for the cluster, on the
binding that selects the requiring member, never repeated on the cluster's
other bindings. A direct `addonRefs` or `profileRefs` entry naming a missing
object is reported by the reference rule at its own indexed field, with no
second copy. Each step-content refusal carries a remedy.

## Built-in packages

Each member of `spec.packages` is named after one
[built-in package](../add-ons.md#built-in-packages-and-selection) of the
executable; an unknown member is an unknown field. Its value is that
package's closed configuration, and diagnostics name fields under
`$.spec.packages.<name>`. Every package accepts `timeout`, a positive Go
duration of at most `5h50m` that defaults to the package's readiness
timeout. A package's own rules check its configuration against the bound
`OpenShiftCluster` and the effective catalog, never a live system.

Registry construction proves that every provided capability token has a reader or a
defined downstream meaning. `NewRegistryWithExamples` accepts explicit
configuration examples to prove conditional `RequiresFor` readers;
`NewRegistry` supplies none. These examples establish registry conformance,
while instance selection evaluates `RequiresFor` against each binding's
actual normalized configuration.

### Subscription

A package that installs an OLM operator has a required `subscription`
member:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `channel` | string | yes | — | The OLM channel the operator selects; no default exists. |
| `startingCSV` | string | yes | — | The exact CSV installed, and the only CSV Bootwright approves. |
| `source` | string | no | the package's documented catalog, or `catalogSource.name` when present | CatalogSource name; equals `catalogSource.name` when that block is present. |
| `sourceNamespace` | string | no | `openshift-marketplace` | CatalogSource namespace; `openshift-marketplace` when `catalogSource` is present. |
| `catalogSource` | object | no | — | A CatalogSource Bootwright creates in `openshift-marketplace`. |
| `catalogSource.name` | string | yes, in the block | — | DNS label other than `redhat-operators`, `certified-operators`, `community-operators` and `redhat-marketplace`. |
| `catalogSource.image` | string | yes, in the block | — | Index image reference, by tag or digest as the operator chooses. |
| `catalogSource.priority` | integer | yes, in the block | — | Stated explicitly, because a catalog without one outranks `redhat-operators`. The catalog serves dependency resolution for every Subscription on the cluster, so a priority above that of `redhat-operators` (-100) lets it supply their dependencies, plans that an operator-managed package approves included. |
| `catalogSource.pollInterval` | string | no | — | Positive Go duration. |
| `catalogSource.securityContextConfig` | string | no | `restricted` | `legacy` or `restricted`. |

### Package nmstate

The Kubernetes NMState Operator, providing `nmstate`
([coverage](../add-on-coverage.md#nmstate)). Its configuration
is `subscription` (`source` defaults to `redhat-operators`) and `timeout`
(default `30m`); it has no other field.

### Package kubevirt

Red Hat OpenShift Virtualization, providing `kubevirt`
([coverage](../add-on-coverage.md#kubevirt)). Its configuration is
`subscription` (`source` defaults to `redhat-operators`) and `timeout`
(default `45m`); it has no other field, and the `HyperConverged` object it
creates carries no configuration.

### Package metallb

The MetalLB Operator, providing `metallb`
([coverage](../add-on-coverage.md#metallb)), answers the addresses of
`LoadBalancer` Services from the layer 2 pools its configuration declares.
MetalLB never serves `api`, `api-int` or the default ingress. Its
configuration is `subscription` (`source` defaults to `redhat-operators`),
`timeout` (default `30m`) and `pools`:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `pools` | array | no | `[]` | At most 16 pools, keyed by `name`. An empty list installs the operator with no pool. |
| `pools[].name` | string | yes | — | DNS label, unique in the list; names the pool's `IPAddressPool` and its `L2Advertisement`. |
| `pools[].addresses` | array of strings | yes | — | 1 to 16 entries, each a CIDR with no host bits set or a `<first>-<last>` range, in canonical IP text. |
| `pools[].autoAssign` | boolean | no | `true` | `false` serves only Services that request the pool by name. |
| `pools[].l2.interface` | string | no | — | One node interface name. Omitted, the pool lies on a machine network and is announced on every interface. |

Each pool belongs to one network: either a consumed machine network of the
bound cluster or, with `l2.interface`, the one subnet that holds the static
address every node carries on that interface.

The named interface is never a node's default-route interface or `br-ex`,
because OVN-Kubernetes builds `br-ex` from the install interface. It is never
a secondary network's uplink interface or bridge either, because those carry
no host address.

A cluster whose pool names an interface declares
`ovnKubernetesConfig.gatewayConfig.routingViaHost: true` and
`ipForwarding: Global`, the host routing and forwarding MetalLB needs there.
Bootwright changes neither after installation.

A pool includes none of these addresses:
- an endpoint address;
- a node address;
- the next hop of a node's route;
- an address another declared object holds;
- the IPv4 network or broadcast address of its subnet.

A pool also overlaps no other pool, no cluster or service network, and no
secondary network's subnets. Pools change only when the cluster is applied
again after a destroy.

| Fault | Code | Field | Message | Remedy |
| --- | --- | --- | --- | --- |
| Non-canonical entry | `api.value` | `$.spec.packages.metallb.pools[<i>].addresses[<j>]` | `address <entry> of pool <pool> is not a canonical CIDR or <first>-<last> range` | `write it as <canonical>` |
| Reversed or mixed range | `api.value` | as above | `range <entry> of pool <pool> ends before it starts or mixes address families` | `write the range as <first>-<last> of one family, first not after last` |
| Wrong family | `api.invariant` | as above | `address <entry> of pool <pool> is not of the <family> family of OpenShiftCluster/<cluster>` | `use <family> addresses for the pools of <cluster>` |
| Outside every machine network | `api.invariant` | as above | `address <entry> of pool <pool> lies outside every machine network of OpenShiftCluster/<cluster>` | `use addresses inside <cidrs>, or name the interface whose subnet holds them under l2.interface` |
| Pool spans networks | `api.invariant` | `$.spec.packages.metallb.pools[<i>].addresses` | `pool <pool> spans <network> and <network>` | `split the pool so that each pool lies inside one network` |
| Interface missing on a node | `api.invariant` | `$.spec.packages.metallb.pools[<i>].l2.interface` | `interface <interface> of pool <pool> is not declared on node <machine> of OpenShiftCluster/<cluster>` | `declare <interface> on every node of <cluster>, or omit l2.interface` |
| Interface without an address | `api.invariant` | as above | `interface <interface> of pool <pool> carries no <family> address on node <machine>` | `assign <interface> an address in the pool's subnet on every node through network.addresses` |
| Interface on several subnets | `api.invariant` | as above | `interface <interface> of pool <pool> is on <subnet> on node <machine> and on <subnet> on node <machine>` | `assign every node's <interface> address in one subnet` |
| Outside the interface subnet | `api.invariant` | `$.spec.packages.metallb.pools[<i>].addresses[<j>]` | `address <entry> of pool <pool> lies outside <subnet>, the subnet of <interface>` | `use addresses inside <subnet>` |
| Default-route interface | `api.invariant` | `$.spec.packages.metallb.pools[<i>].l2.interface` | `interface <interface> of pool <pool> is the default-route interface of node <machine> or br-ex` | `omit l2.interface to announce a pool on the machine network` |
| Secondary-network uplink | `api.invariant` | as above | `interface <interface> of pool <pool> is the uplink of secondary network <network>, which carries no host address` | `name an interface that carries a node address, or omit l2.interface` |
| No host routing | `api.invariant` | as above | `pool <pool> announces on <interface>, which needs gatewayConfig.routingViaHost: true on OpenShiftCluster/<cluster>` | `set spec.networking.ovnKubernetesConfig.gatewayConfig.routingViaHost: true on <cluster>, or omit l2.interface` |
| No forwarding | `api.invariant` | as above | `pool <pool> announces on <interface>, which needs gatewayConfig.ipForwarding: Global on OpenShiftCluster/<cluster>` | `set spec.networking.ovnKubernetesConfig.gatewayConfig.ipForwarding: Global on <cluster>, or omit l2.interface` |
| Held address | `api.invariant` | `$.spec.packages.metallb.pools[<i>].addresses[<j>]` | `address <entry> of pool <pool> includes <address>, held by <holder>` | `exclude <address> from the pool` |
| Network or broadcast address | `api.invariant` | as above | `address <entry> of pool <pool> includes <address>, the network or broadcast address of <subnet>` | `exclude <address> from the pool` |
| Overlapping network | `api.invariant` | as above | `address <entry> of pool <pool> overlaps <cidr>, the <network kind> of OpenShiftCluster/<cluster>` | `use addresses outside <cidr>` |
| Overlapping pools | `api.duplicate` | `$.spec.packages.metallb.pools[<i>].addresses[<j>]` of the later pool | `pool <pool> overlaps pool <other> at <address>` | `make the addresses of <pool> and <other> disjoint` |

### Package fdf

IBM Fusion Data Foundation in external mode against a Ceph cluster the
context manages ([coverage](../add-on-coverage.md#fdf)). It installs the
operator package `odf-operator`, which IBM's catalog serves. Naming
`redhat-operators` as `subscription.source` is expected to install Red Hat
OpenShift Data Foundation; that `redhat-operators` serves the same package
name, namespace and objects is unverified and is re-verified by
[B761](../milestones/m4.md#b761) with `oc get packagemanifest odf-operator -n
openshift-marketplace` on a Red Hat catalog. It provides `dataFoundation`.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `subscription` | object | yes | — | The [shared block](#subscription); `source` is required, with no default, and names the owned `catalogSource` or a default catalog. |
| `cephConsumerRef` | string | yes | — | A managed `CephConsumer` this package instance consumes; no other instance names it. |
| `defaultStorageClass` | boolean | no | `false` | Makes the RBD storage class the cluster's default. |
| `timeout` | string | no | `1h` | As every package's `timeout`. |

Rules, each at its field under `$.spec.packages.fdf`:

| Fault | Code | Field | Message | Remedy |
| --- | --- | --- | --- | --- |
| The catalog is neither owned nor a default catalog | `api.invariant` | `subscription.source` | `package fdf subscribes from catalog <source>, which no cluster Bootwright installs serves` | `declare subscription.catalogSource named <source> with the IBM catalog image and an explicit priority, or name redhat-operators` |
| The consumer is consumed twice | `api.duplicate` | `cephConsumerRef` of the later binding in canonical order | `CephConsumer/<consumer> is already consumed by package fdf of ClusterAddonBinding/<binding>` | `give each consuming cluster its own CephConsumer` |
| A TLS object gateway reaches no cluster trust | `api.invariant` | `cephConsumerRef` | `CephConsumer/<consumer> attaches an object gateway over TLS, and OpenShiftCluster/<cluster> adds no trust bundle to its cluster-wide trusted CA` | `add the gateway certificate's issuing CA to spec.install.additionalTrustBundleRefs of <cluster> under additionalTrustBundlePolicy Always` |

The default catalogs are `redhat-operators`, `certified-operators`,
`community-operators` and `redhat-marketplace`. The trust rule holds when the
cluster selects at least one additional trust bundle under the effective
policy `Always`, which [B195](../milestones/delivered.md#b195) defines. It is a
conservative Bootwright choice, not a native fact: v1 observed NooBaa failing
with `x509` against a private RGW CA until the issuing CA was in the
cluster-wide trusted bundle, the trust source in 4.20 to 4.22 is not
established, and [B761](../milestones/m4.md#b761) re-verifies it. It also
refuses a gateway whose certificate already chains to a CA in cluster trust,
because admission cannot see that trust.


### Package acm

Red Hat Advanced Cluster Management for Kubernetes
([coverage](../add-on-coverage.md#acm)). Its configuration:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `subscription` | object | yes | — | The [Subscription](#subscription) of the hub operator; `source` defaults to `redhat-operators`. |
| `mce` | object | yes | — | The multicluster engine Subscription the hub operator creates. |
| `mce.channel` | string | yes | — | The engine's OLM channel; no default exists. |
| `mce.startingCSV` | string | yes | — | The exact engine CSV, and the only one Bootwright approves. |
| `mce.source` | string | no | `subscription.source` | CatalogSource name. |
| `mce.sourceNamespace` | string | no | `subscription.sourceNamespace` | CatalogSource namespace. |
| `hub` | object | no | `{}` | Fields of the `MultiClusterHub`. |
| `hub.availabilityConfig` | string | no | — | `Basic` or `High`; absent leaves the hub's own default. |
| `hub.disableHubSelfManagement` | boolean | no | `false` | `true` stops the hub managing itself as a managed cluster. |
| `hub.nodeSelector` | map of string to string | no | — | Label keys and values for the hub's components. |
| `hub.tolerations` | list | no | — | Entries of `key`, `operator` (`Exists` or `Equal`), `value`, `effect` (`NoSchedule`, `PreferNoSchedule` or `NoExecute`) and `tolerationSeconds`; a list replaces the hub's default infrastructure-node toleration. |
| `timeout` | string | no | `45m` | As every package's `timeout`. |

| Fault | Code | Field | Message | Remedy |
| --- | --- | --- | --- | --- |
| A single-node cluster without the basic hub | `api.invariant` | `$.spec.packages.acm.hub.availabilityConfig` | `package acm on single-node OpenShiftCluster/<cluster> needs hub availabilityConfig Basic` | `set hub.availabilityConfig to Basic` |

### Package gitops

Red Hat OpenShift GitOps with its default Argo CD instance, a
[last package](../add-ons.md#built-in-packages-and-selection)
([coverage](../add-on-coverage.md#gitops)). Its configuration:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `subscription` | object | yes | — | The [Subscription](#subscription) of the GitOps operator; `source` defaults to `redhat-operators`. |
| `bootstrap` | object | no | — | The [bootstrap object](../add-ons.md#the-gitops-bootstrap-object). |
| `bootstrap.repoURL` | string | yes, in the block | — | An `https` URL with a host and no user information, query or fragment. |
| `bootstrap.targetRevision` | string | yes, in the block | — | Branch, tag or commit; non-empty, no whitespace. |
| `bootstrap.path` | string | yes, in the block | — | Relative path in the repository, `.` for its root; no leading `/` and no `..` segment. |
| `bootstrap.destinationNamespace` | string | yes, in the block | — | DNS label; the namespace for manifests that name none. Naming `openshift-gitops` lets the repository's unscoped manifests write Argo CD's own namespace, including the placed repository Secret. |
| `bootstrap.credentialsRef` | string | no | — | Name of a Secret of type `usernamePassword` read by Argo CD for the repository. Effect material: the package places it in the cluster as an [owned Secret](../add-ons.md#owned-secrets). |
| `bootstrap.access.clusterRole` | string | no | — | Name of a ClusterRole granted to the default instance's application controller. |
| `bootstrap.sync.automated` | boolean | no | `true` | Argo CD synchronizes automatically. |
| `bootstrap.sync.prune` | boolean | no | `false` | Automated sync deletes what leaves the repository. |
| `bootstrap.sync.selfHeal` | boolean | no | `false` | Automated sync reverts live changes. |
| `timeout` | string | no | `20m` | As every package's `timeout`. |

| Fault | Code | Field | Message | Remedy |
| --- | --- | --- | --- | --- |
| The repository URL is not usable | `api.field` | `$.spec.packages.gitops.bootstrap.repoURL` | `bootstrap repoURL of package gitops is not an https URL with a host and no query or fragment` | `set the https URL of the repository` |
| The repository URL carries credentials | `api.field` | `$.spec.packages.gitops.bootstrap.repoURL` | `bootstrap repoURL of package gitops carries user information` | `remove the user information and name a usernamePassword Secret in bootstrap.credentialsRef` |
| The path leaves the repository | `api.field` | `$.spec.packages.gitops.bootstrap.path` | `bootstrap path <path> of package gitops leaves the repository` | `set a relative path without .. segments` |
| Prune or self-heal without automation | `api.invariant` | `$.spec.packages.gitops.bootstrap.sync` | `bootstrap sync prune and selfHeal of package gitops need sync.automated` | `set sync.automated to true, or remove prune and selfHeal` |

Diagnostics never echo a repository URL, which could hold credentials.

### Package certmgr

The cert-manager Operator for Red Hat OpenShift, providing `certManager`
([coverage](../add-on-coverage.md#certmgr)). Its configuration is
`subscription` and `timeout` (default `20m`); it has no other field.

Not yet met: the `certmgr` package does not exist; tracked as
[B767](../milestones/m4.md#b767).

### Package gitlab

GitLab through the GitLab Operator, requiring `certManager`
([coverage](../add-on-coverage.md#gitlab)). GitLab's host names are
`gitlab.`, `registry.` and `kas.` under the bound cluster's applications
domain, `apps.<cluster>.<zone>`, which the cluster's wildcard record already
resolves; no field changes the domain. Every referenced Secret is placed in
the cluster as an [owned Secret](../add-ons.md#owned-secrets); Bootwright never
parses, validates or repeats its contents.

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `subscription` | object | yes | — | The [shared block](#subscription); `source` is required, with no default. |
| `timeout` | string | no | `1h` | Positive Go duration of at most `5h50m`. |
| `chartVersion` | string | yes | — | Semantic version `<major>.<minor>.<patch>` of the GitLab chart the `GitLab` resource names; the operator chooses one the installed operator release lists. |
| `rootPasswordRef` | string | yes | — | `token` Secret holding the initial root password; GitLab reads it only when it first initializes its database. Effect material: the package places it in the cluster as an [owned Secret](../add-ons.md#owned-secrets). |
| `tlsCertificateRef` | string | yes | — | `tlsCertificate` Secret whose certificate covers the three host names, for example a wildcard for the applications domain. Effect material: the package places it in the cluster as an [owned Secret](../add-ons.md#owned-secrets). |
| `licenseRef` | string | no | — | `opaque` Secret holding an Enterprise Edition license. Effect material: the package places it in the cluster as an [owned Secret](../add-ons.md#owned-secrets). |
| `database.host` | string | yes | — | DNS name or IP address of a PostgreSQL server reachable from the cluster. |
| `database.port` | integer | no | `5432` | `1..65535`. |
| `database.name` | string | no | `gitlabhq_production` | Non-empty name of an existing empty database. |
| `database.username` | string | yes | — | Non-empty user name with full access to the database. |
| `database.passwordRef` | string | yes | — | `token` Secret holding that user's password. Effect material: the package places it in the cluster as an [owned Secret](../add-ons.md#owned-secrets). |
| `redis.host` | string | yes | — | DNS name or IP address of a standalone Redis or Valkey server reachable from the cluster. |
| `redis.port` | integer | no | `6379` | `1..65535`. |
| `redis.passwordRef` | string | no | — | `token` Secret; when absent, GitLab connects without authentication. Effect material: the package places it in the cluster as an [owned Secret](../add-ons.md#owned-secrets). |
| `objectStorage.connectionRef` | string | yes | — | `opaque` Secret whose value is the chart's consolidated object-storage connection document. Effect material: the package places it in the cluster as an [owned Secret](../add-ons.md#owned-secrets). |
| `objectStorage.registryRef` | string | yes | — | `opaque` Secret whose value is the registry's storage driver document. Effect material: the package places it in the cluster as an [owned Secret](../add-ons.md#owned-secrets). |
| `objectStorage.registryBucket` | string | no | `gitlab-registry-storage` | The bucket the registry document names. |
| `gitaly.storageClass` | string | yes | — | DNS subdomain naming the StorageClass of Gitaly's ReadWriteOnce volume. |
| `gitaly.size` | string | no | `50Gi` | Kubernetes quantity in `Gi` or `Ti`, at least `1Gi`. |

Admission refuses, at `$.spec.packages.gitlab`, a cluster whose applications
domain would make `registry.`, `gitlab.` or `kas.` before it longer than 253
bytes. Nothing relates the chart version, the CSV and the cluster release to
each other. The package requires `certManager`, and also `dataFoundation`
when `gitaly.storageClass` begins with `ocs-external-storagecluster-`, so
Gitaly's volume is claimed only after the cluster's `fdf` instance is ready;
with no instance providing it, the binding is refused as for any unprovided
requirement.

Not yet met: the `gitlab` package does not exist; tracked as
[B93](../milestones/m4.md#b93).

## Normalization and canonical state

After Environment kind defaults, normalization materializes input `required`,
readiness timeout, omitted OLM readiness checks, namespace management, present
OperatorGroup name and target namespaces, and subscription name, source,
source namespace and install-plan approval. These built-in defaults never
create an OperatorGroup still omitted after Environment defaults or append to
a supplied readiness-check list. For a selected package, normalization
materializes `timeout`, `subscription.source` (except for `fdf` and `gitlab`,
which require it), `subscription.sourceNamespace` and
`subscription.catalogSource.securityContextConfig`; for `metallb`, each
pool's `autoAssign`; for `fdf`, `defaultStorageClass`; for `acm`,
`mce.source`, `mce.sourceNamespace` and `hub.disableHubSelfManagement`; for
`gitops` with a `bootstrap`, the three `sync` members; and for `gitlab`, `database.port`, `database.name`, `redis.port`,
`objectStorage.registryBucket` and `gitaly.size`.

Explicit false remains false, including an optional input's `required`
value.
Ordered arrays retain order; set-valued arrays and map keys canonicalize under
the common API rules.
