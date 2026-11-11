# Environment and fleet defaults

`Environment` owns fleet domains, sites, selected state, access/install
defaults, controller selection and policy. Its name labels the complete
effective graph and is not an identity; a
[context](../contexts.md#identity-and-selection) owns the lifecycle identity.
Shared services are independent objects in
[infrastructure-services.md](infrastructure-services.md). [The compiler boundary](../api.md#compiler-boundary)
applies to every declaration below.

Every kind on this page names its objects under the [one name rule](../api.md#object-names).

## Kinds

| Kind | Declares |
| --- | --- |
| `Environment` | The one root of a context's desired state: its domains, sites, controller Machine, cluster selection and kind defaults. |

## Environment

Environment fields emit in this order:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.domains` | object | yes | — | Fleet DNS zones; closed shape below. |
| `spec.sites` | array of objects | no | omitted | Estate site registry, keyed by `name`. |
| `spec.containerClusters` | array of strings | no | all loaded container clusters | Non-empty unique root selection of the `containerCluster` category's kinds ([family registry](../api.md#cluster-family-registry)): `OpenShiftCluster`. |
| `spec.storageClusters` | array of strings | no | all loaded storage clusters | Non-empty unique root selection of the `storageCluster` category's kinds: `CephCluster`. |
| `spec.remoteMachinesAccessKey` | object | conditional | — | Fleet key for the `bootwright` account installed on managed machines. |
| `spec.defaults` | kind-keyed partial specs | no | `{}` | Omitted object fields inherit the corresponding kind entry under the rules below. |
| `spec.dependencyVersions` | object | no | this build's defaults | Version intent for the prerequisites this Environment controller stage installs; closed shape below. |
| `spec.mirror` | object | no | — | The [artifact mirror](#artifact-mirror) that serves every public-origin client, an operator's `url` or a managed `serverRef` and `endpointRef`, with its optional trust anchor, and the [image mirror](#image-mirror) that serves every public-origin image a Machine runs as a service. |
| `spec.controller` | object | yes | — | Required controller Machine selection below. |
| `spec.lifecycle` | object | no | — | Offline-rescue input; a present `rescue` is refused until a rescue journey exists. |

Omitted optional arrays remain omitted unless their owning rule declares a
materialized default. Authored arrays reject duplicate entries by their
documented identity.

## Dependency versions

`spec.dependencyVersions` controls the versions this Environment's own
[controller stage](../state-reconciliation.md#stages-and-the-pause-boundary)
installs. Fields emit in the following order. Every field is optional and
accepts a string; omission means the default release the build's
[tool table](../controller.md#the-tool-table) names for a client, and `latest`
for libvirt. Admission preserves authored values and does not materialize
release numbers or contact publishers.

| Field | Dependency | Exact override |
| --- | --- | --- |
| `libvirt` | Native libvirt client, including `virsh` | `latest`; the grammar admits a distribution package version or `[EPOCH:]VERSION-RELEASE`, which a plan refuses ([refusal table](#refusal-table)). |
| `helm` | Helm | An exact publisher release tag, optionally prefixed by `v`, at or above the build's minimum. |
| `govc` | vSphere client | An exact publisher release tag, optionally prefixed by `v`, at or above the build's minimum. |
| `virtctl` | Upstream KubeVirt client | An exact publisher release tag, optionally prefixed by `v`, at or above the build's minimum. |

The private interpreter, `ansible-core` and the baseline native packages have
no field here. They are [host prerequisites](../controller.md): one prepared
host serves every context, so no single Environment may move their versions.
`python`, `ansible`, `podman`, `openssh` and `nmstate` are therefore rejected
like any other unknown key.

A client takes no `latest`: the build's default replaces it, and a newer
release is a new build or a declared one. A declared release below the
build's minimum, or one without a leading `MAJOR.MINOR.PATCH`, refuses at plan
while its client is selected ([refusal table](#refusal-table)). For the
libvirt client the controller stage
installs whatever the host's own repositories provide, through the host's dnf,
so a selected libvirt closure with any other `libvirt` value refuses at plan,
naming the Environment and the remedy of removing the field, and the stage
refuses a frozen request that carries one
([the controller stage](../controller.md#the-controller-stage)); the pin is
tracked as [B676](../milestones/backlog.md#b676). Version
ranges, wildcards, nulls and empty strings are invalid. A client release tag,
after its optional `v`, is at most 95 ASCII characters, starts with a digit,
and otherwise contains only alphanumerics or `._+-`; any tag the publisher
publishes, prereleases included, is a valid exact pin. Native values are at
most 96 ASCII characters, start with an alphanumeric character, and otherwise
contain only alphanumerics or `._+~^:-`.

The selected desired-state graph determines which dependencies are needed.
An override alone does not select an unused libvirt, Helm, govc or virtctl
client. Native package dependencies are whatever the host's dnf resolves; they are
not individually configurable. An incompatible or unavailable exact request
fails with a dependency diagnostic rather than silently substituting another
root version. The controller adapter runs the `ansible-core` release the
[runtime lock](../controller.md#the-runtime-lock) of the running build pins, a
host prerequisite no Environment versions.

OpenShift/OKD installer and client versions remain tied to the target cluster's
declared release. `openshift-install`, `oc` and `kubectl` are not override keys.
`virtctl` defaults to the build's default upstream release; declare an
explicit version when the target virtualization installation requires a
particular client. No version
is inferred from a running target cluster.

For example:

```yaml
spec:
  dependencyVersions:
    libvirt: latest
    helm: "4.3.1"
    virtctl: "1.9.1"
```

These fields follow the normal `defaults.Environment` inheritance rules.
Resolution, retention, retry and reuse follow
[the controller stage](../controller.md#the-controller-stage):
a retained client release is reused as frozen, and a later build's new
default reaches a completed environment only through a destroy and a fresh
apply ([the tool table](../controller.md#the-tool-table)).

## Domains

`spec.domains` contains exactly these fields and defaults in dependency order:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `base` | string | yes | — | Fleet base DNS zone. |
| `machines` | string | no | `base` | Zone for implicit Machine `fqdn` addresses. |
| `clusters` | string | no | `base` | Umbrella default for both cluster classes. |
| `containerClusters` | string | no | `clusters` | Zone used by container-cluster node names and install-config `baseDomain`. |
| `storageClusters` | string | no | `clusters` | Zone used by storage-cluster node and service names. |

Values are lowercase DNS names without trailing dots. A single-zone
environment needs only `base`.

Composition uses one rule throughout the API:

- a Machine's implicit `fqdn` address is
  `<Machine.metadata.name>.<domains.machines>`;
- a container node without an explicit `fqdn` is
  `<node.name>.<OpenShiftCluster.metadata.name>.<domains.containerClusters>`;
  and
- a storage node without an explicit `fqdn` takes its Machine's effective
  `fqdn`, and is
  `<node.name>.<CephCluster.metadata.name>.<domains.storageClusters>` only when
  that Machine has none ([CephCluster normalization](ceph.md#normalization)).

Authored node `name` values are DNS labels. A separate `fqdn` field is the only
way to pin a cluster-visible host name outside the composed zone.

## Sites

Each `spec.sites[]` entry contains:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `name` | string | yes | DNS label, unique in the Environment; rendered as the CRUSH site bucket name. |
| `description` | string | no | Operator-facing description. |

Every site named by Machine placement, storage topology, stretch or service
placement must resolve here. Without such uses the registry may be omitted;
unused declared sites are valid and silent.

## Cluster selection

The Environment's directory is the scope: every YAML file discovery acquires
under [the API input rules](input.md#discovery) is selected, and the cluster
lists below are the only narrowing. For example, an Environment that installs
`ocp-01` and its storage while a second declared cluster waits authors:

```yaml
spec:
  containerClusters:
    - ocp-01
```

The input reader still reads, bounds and records the
`add-ons/_store/<name>/.bootwright-addon` markers, although with every
discovered file selected they grant nothing; their removal is parked as
[B771](../milestones/backlog.md#b771).

Not yet met: a hard-linked marker refuses the whole input read as
`input.symlink`; tracked as [B771](../milestones/backlog.md#b771).

`spec.containerClusters` and `spec.storageClusters` select effective cluster
roots after decoding and reference-independent normalization. Each
authored list must be non-empty, contain unique names, and resolve to its
corresponding kind. Omission of either list selects every loaded root of that
class. Objects outside the selected cluster-owned graph are absent from
effective state, while fleet-global objects and shared services remain
available. Each excluded cluster
root produces a deterministic `validate` warning. These fields are selection
lists, not `Ref` fields, and no CLI flag can further narrow them.

The cluster-selection closure is authoritative and independent of file layout:

| Starting selection | Retained objects |
| --- | --- |
| Every selection | The selected `Environment`, its required controller Machine and that Machine's provider/provider-host closure, plus every `Entitlement`, `MachineImage`, `MachineInstallProfile`, `Network`, `NetworkConfig`, `Proxy`, `DNSServer`, `NTPServer`, `ArtifactServer`, `Registry`, `LoadBalancer`, `CustomPlaybook`, and `Secret` objects; every `Network` with a `libvirt` arm also retains the `InfraProvider` it names and that provider's host closure; every retained managed service also retains its placement Machine and that Machine's `InfraProvider` and provider-host Machine closure; and every Bootwright-installed Machine (`os.provided: false` with `installProfileRef`) that is neither a node nor a node's provider host of any declared cluster, as a root with its provider and provider-host closure. |
| One `OpenShiftCluster` | That root; its node Machines; the Machines' `InfraProvider` objects and any libvirt provider-host Machines; each KubeVirt provider's `hostClusterRef` `OpenShiftCluster`, retained as a root with its own row; Machines hosting managed infrastructure services consumed by the root; bindings for the root plus their recursively expanded profiles and add-ons; and each `CephConsumer` named by a selected package's configuration, its referenced storage cluster, policy/pool/filesystem/gateway chain, and every `CephNFSExport` on that retained storage cluster. |
| One `CephCluster` | That root and every Ceph child kind whose `clusterRef` names it, `CephConsumer` included; its node, provider, provider-host, and consumed-service Machines; and each `OpenShiftCluster` attached to one of those exports through a package's configuration, together with that container root's bindings, recursively expanded profiles, and add-ons. |

Closures for all listed roots are unioned and then emitted in canonical API
order. The rules apply until nothing more is retained. An `OpenShiftCluster`
retained through an attachment brings everything its own row names, its node
Machines and its other attached exports and their chains included. A
`CephCluster` retained only through a `CephConsumer` keeps its node
Machines and their provider and provider-host closure, but not every object
naming it and no other attached `OpenShiftCluster`. When an attachment reaches
a cluster root that its authored list omits, `validate` refuses with an error
on that list naming the root and the export that reached it, because the
selection of a completed apply cannot change
([lifecycle unit](../state-reconciliation.md#lifecycle-unit),
[D153](../milestones/backlog.md#d153)); a cluster retained only
through an attachment is therefore always refused by that omitted-root rule.
A retained
`CephReplication` retains both its clusters as full roots, and selecting
either end of the relationship retains it; this widens nothing else, so a
`CephCluster` retained only through a `CephConsumer` still keeps its node
closure alone. A `Machine` or `InfraProvider` not reached by the rules above is
excluded; when no excluded cluster root accounts for it as a node, a node's
provider or a provider host, `validate` warns, naming it and stating that no
selection field retains it. A reference from any retained object to an excluded
object is not an implicit retention edge unless a row above lists it; it is an
unresolved-reference error naming the selection field that would retain the
target: an excluded cluster root names its own list, and any other excluded
object names the authored lists. Omission of both cluster lists skips this filtering and retains
every selected object.

## Remote-machine access and install defaults

`spec.remoteMachinesAccessKey.keyRef` is a plain reference to an `sshKeyPair`
`Secret`. It is required as soon as a selected `Machine` with
`os.provided: false` sets `os.installProfileRef`. A managed-OS install authorizes its public half for the
product-owned `bootwright` account and uses its private half for that account;
an already provided machine keeps its own `Machine.spec.access`. The Secret is
[effect material](../secrets.md#immutable-binding-and-contexts), because the
install places its public half in that account.

The fleet key must differ from every Machine access key and every
`CephCluster.spec.cephadm.clusterSSH.keyRef`. Each cluster SSH key must
also differ from every Machine access key, every `os.install.hostKeyRef`,
and every other cluster SSH key. Validation checks
reference identity and type, preventing Ceph key reuse for fleet access. The
fleet key must also differ from every Machine's `os.install.hostKeyRef`; the
Machine [refuses the collision](machines.md#os-lifecycle-and-substrate-invariants).
A provided Ceph stretch tiebreaker is no exception: Ceph dials it with the
fleet key while its Machine names only its own operator access
([stretch topology](../ceph.md#stretch-topology)), and the refusal of a
provided Machine that names the fleet key says so.

## Kind defaults

`spec.defaults` is a closed map keyed by exact, case-sensitive names from the
[kind catalog](../api.md#kind-catalog). Each value is a partial copy of that
kind's `spec`, without an additional `spec` wrapper. For example:

```yaml
spec:
  defaults:
    OpenShiftCluster:
      install:
        pullSecretRef: openshift-pull-secret
```

Every loaded `OpenShiftCluster` missing `spec.install.pullSecretRef` receives
that reference. The mechanism applies to every registered kind, not only
installation material. It never creates objects or supplies `apiVersion`,
`kind`, or `metadata`. Unknown kinds or fields are errors.

Defaults obey these rules in order:

1. Read the authored defaults map from the selected Environment. Validate each
   entry against its kind's partial schema: field names, YAML types, scalar
   grammar/ranges still apply, but required record fields, discriminators, and
   arms may be omitted in a fragment, except where a kind requires a
   discriminating field in its fragment, as
   [ArtifactServer](infrastructure-services.md#artifactserver) does for
   `endpoints`. Conflicting present arms and locally
   provable type violations remain errors. Recipient-dependent requirements
   and references are checked after application. An unused entry must still be
   a valid partial spec; it creates no object or retention edge by itself.
2. Apply `defaults.Environment`, if present, once to the selected Environment's
   other spec fields before cluster selection. That entry cannot
   contain `defaults`. It cannot widen the acquired filesystem source universe
   or change the selected Environment identity. The authored defaults map is
   never itself defaulted or recursively reloaded.
3. Apply each other kind entry once to each selected object before
   provider, conventional, built-in, or reference-derived fallbacks.
   Explicit object values win over Environment values; the owning kind's
   existing fallback order applies only to values still absent.
4. Fill absent attributes recursively inside nonempty schema-defined record
   objects. An explicit scalar, including `false`, `0`, or an empty string,
   remains explicit and must be valid. An explicit empty nested object is a
   whole authored value; it does not request Environment inheritance. The
   recipient's root `spec` is the container of attributes, not such a nested
   value. Explicit null is invalid and never requests fallback. A record whose
   members select, `ClusterAddonBinding.spec.packages` alone, is the exception:
   a default fills only the members the recipient declares and never adds one,
   so an absent or empty record inherits no member, and a declared member,
   `nmstate: {}` included, inherits the fields it omits.
5. Lists, open/native maps, authentication and proxy choices, `install.nodeSSH`, and
   `Secret.source` are whole values. Copy them only when absent; never append
   records, patch native payloads, or combine different key/source material.
   A valid explicit empty collection or source block wins completely.
6. An authored discriminator or present arm selects the variant under the
   [union rules](../api.md#public-authoring-grammar). An empty or all-zero
   [replicated pool block](../ceph-resources.md#replicated-protection) selects
   replicated protection; it does not inherit an erasure arm.
   Defaults never introduce an alternative arm or change that selection.
   Compatible ordinary records within the selected arm may inherit missing
   fields. Without an authored selection, defaults may select one variant.
   Applied defaults must produce a valid complete union; inactive default arms
   are not applied to a differently selected variant.
   The same protection applies to schema-declared forbidden branches controlled
   by an explicit mode or feature choice: an HTTP-only ArtifactServer listener set suppresses inherited TLS,
   disabled authentication suppresses inherited OAuth configuration, and
   external management suppresses inherited managed service and Ceph configuration.
   This does not remove authored forbidden fields or repair arbitrary failed
   prerequisites; those remain errors. An Entitlement default never introduces
   an arm that the authored `type` does not permit.
7. Check required fields and every cross-field, reference, domain, and graph
   constraint on the resulting objects. Invalid explicit or inherited values
   fail; they never trigger another fallback or silently weaken constraints.
   Default-introduced references participate in the ordinary dependency closure.

Inherited fields retain their authored origin for intent checks. Recheck
forbidden-input and authored-intent constraints after applying defaults and
before normalization can replace or erase a value. For example, a Machine
default cannot add authored access to a Bootwright-installed Machine whose
access must be derived. An explicitly declared default is authored intent;
an intrinsic built-in fallback is not.

Omitted `defaults`, `defaults: {}`, and an omitted or empty kind entry add no
Environment fallback for that scope. Schema-defined omission defaults remain
in force after Environment defaults; required means required in the resulting
object unless the field is explicitly an authored-intent requirement.

Relative paths copied from defaults retain the recipient kind's declaring-file
or source-root rules. They are not rebased against the Environment directory.
Diagnostics identify the default's source path, recipient object/field, and
recipient resolution base without opening a payload. Defaulting performs no
file access, Secret read, generation, or materialization. The common API
[normalization rules](../api.md#defaults-normalization-and-effective-state)
own expansion limits, canonical output, and immutable source provenance.

Secret source and custody declarations belong to
[each Secret](secrets.md#source-union). `Environment` has no `secretStorage`
setting. A default may supply an omitted Secret source, but cannot override an
authored source or copy material.

## Artifact mirror

`spec.mirror.artifacts.url` names one HTTPS file server that serves the
clients of a [public origin](../security.md#acquisition-origins) the controller
stage installs: Helm, `govc`, `kubectl` and `virtctl`. Its absolute HTTPS base
URL is at most 3994 bytes, leaving room for the longest client path, and uses
the default port or port 443. Validate refuses `http`, another port,
userinfo, a query, a fragment, a bare `?` or `#`, and a percent-escaped path;
the stage applies the same rule before acquisition. The OKD clients, also of a
public origin, are refused beside it, because no build pins them. A client of
a vendor origin, the OpenShift clients and installer, stays on its vendor.
Each mirrored client is read from
`<url>/<kind>/<release>/<file>`, its release with the `v` prefix and its file
the publisher's own file name, for example
`<url>/helm/v4.3.1/helm-v4.3.1-linux-amd64.tar.gz` and
`<url>/virtctl/v1.9.1/virtctl-v1.9.1-linux-amd64`.

Because a client behind the mirror is acquired only by the size and SHA-256
the build's [tool table](../controller.md#the-tool-table) pins, the stage
reaches no publisher for any client behind it. A selected client release the table does
not pin, a declared `spec.dependencyVersions` override or the OKD clients,
refuses at plan ([refusal table](#refusal-table)). `bootwright render acquisitions`
names each file the mirror must hold.

`spec.mirror.artifacts.trustBundleRef`, optional, names a `caBundle` or `trustBundle`
[Secret](secrets.md), a [trust anchor](../secrets.md#immutable-binding-and-contexts) whose certificates verify the mirror beside the system
trust store, as the image mirror Registry's `trustBundleRef` adds to it, so a
mirror whose certificate an enterprise authority issued needs no change to
the controller's trust store. The anchor verifies only the mirror's own host:
a proxy and any other host keep the system trust store alone. Only the
Secret's name is frozen into the controller stage's request; its certificate
is lent to the attempt that downloads, as a private file the run removes
after it, and to the preflight probe, and no record or evidence holds it.
Without it the mirror verifies against the system trust store alone.

Mirror artifact `tlsVerification` is an optional sibling of `trustBundleRef`,
defaulting to `full`. Other modes refuse under
[client TLS trust](../secrets.md#client-tls-trust).

Before the stage runs, [`preflight`](../cli/output.md#preflight) reads the
size of each file the stage would read from the mirror, with no body: an
HTTPS `HEAD`, or a one-byte ranged `GET` where the server refuses `HEAD`,
through the controller Machine's [proxy choice](machines.md#machine-proxy)
and the same trust, following a redirect only to HTTPS on the mirror's own
host, within two minutes for all of them. The stage's download keeps to a
like rule: a redirect leaves a source's host only from one publisher download
host to another, so the artifact mirror cannot redirect the stage to a
publisher. It fails a file the mirror lacks or serves at a size other than the one the build pins, naming the file's URL and
the publisher file to copy there, and reports one it could not read, or whose
size the mirror did not name, as unknown. Without the declaration preflight
probes nothing.

In place of `url`, `spec.mirror.artifacts.serverRef` names a managed
[ArtifactServer](infrastructure-services.md) that Bootwright fills on its
[mirror host](../infrastructure-services.md#mirror-host), with a required
`endpointRef` naming one of its endpoints; `url` and `serverRef` exclude each
other, and `endpointRef` beside `url` refuses. The mirror's URL is then
`https://<endpoint address>/mirror/<environment>`, under the same layout, so
the endpoint's listener serves HTTPS on port 443, where the controller stage
reads a mirror, and admission refuses another protocol or port at
`$.spec.mirror.artifacts.endpointRef`. Without `trustBundleRef` the anchor is
the generated `caBundle` that issues the server's serving certificate, and
admission refuses at `$.spec.mirror.artifacts.serverRef` a certificate with
no issuer. `preflight` reports a file such a mirror answers 404 or 410 for as
pending, since the apply fills it before the controller stage reads it; a file
served at another size still fails, and a transport or TLS failure stays
unknown. The server may be placed on the controller Machine, around which the
[controller stage](../controller.md#the-controller-stage) splits, or on a
Machine Bootwright installs ([placement](infrastructure-services.md#management-and-placement)).

## Image mirror

`spec.mirror.registry.registryRef` selects one
[Registry](infrastructure-services.md) as the image mirror. Each managed
service's image, the artifact server's and the emulated BMC's whose origin is
a [public origin](../security.md#acquisition-origins) is then pulled and run as
`<registry location>/<origin registry>/<repository>@<digest>`, the digest the build
pins, so a short Docker Hub name takes `docker.io` as its origin registry: for
example `mirror.example.test/images/quay.io/metal3-io/sushy-tools@sha256:…`. An
image of a vendor origin, such as the artifact server's from
`registry.access.redhat.com`, stays on its vendor, and a service's own
`spec.image.local` stays its image, as does every image pulled on the mirror
host and, when Bootwright installs it, the image of each emulated BMC and
managed server that installation waits for. An external Registry's
location is its `url`.

A managed Registry is filled on its
[mirror host](../infrastructure-services.md#mirror-host) with every image its
consumers pull through it. The selection takes the registry-selection shape:
`endpointRef` names one of the Registry's endpoints, required unless it
declares exactly one, and refused beside an external Registry. The location is
`<endpoint address>:<port>`, the Registry's own port, so a consumer reads
`203.0.113.20:5000/quay.io/metal3-io/sushy-tools@sha256:…`. Without the
Registry's `trustBundleRef` the anchor is the generated `caBundle` that issues
its serving certificate, and admission refuses at
`$.spec.mirror.registry.registryRef` a certificate with no issuer. Every pull
binds the Registry's `credentialsRef`, the credential the fill pushes with,
because Distribution's password file grants each user every action; a pull-only
credential is [B1319](../milestones/backlog.md#b1319). An image pulled on the
mirror host itself, the Registry's own and the artifact server's there, stays
its origin's. Off that host, only the emulated BMCs and managed servers its
installation waits for keep their origins, following the
[mirror host ordering](../infrastructure-services.md#mirror-host); every other
mirrored consumer waits for the Registry and its fill.
The Registry may be placed on the controller Machine, whose controller stage
then installs `skopeo`, or on a Machine Bootwright installs, whose profile
installs it.

The pull goes over HTTPS with TLS verification on, through the placement
Machine's frozen egress route, and binds the Registry's `credentialsRef` and
trust anchor: the operation lends them for that one pull, in a private
directory the pull removes after it, never through an insecure-registry
setting or the host's registry configuration. A host already holding the
mirrored reference pulls nothing; one holding only the origin's copy pulls the
same digest once from the mirror. A pull the mirror cannot serve fails naming
the reference. A cluster's release repositories map to the mirror, and a managed mirror's
host copies each such cluster's release into it
([registry mirrors](../openshift-clusters.md#installation-features)). A
managed image mirror's placement refuses at plan as its
[refusal table](#refusal-table) rows say. `bootwright render acquisitions`
names each mirrored reference and, for a managed mirror, each image the mirror
host copies from its origin.

## Controller Machine

`spec.controller` is a required closed record containing only required
`machineRef`, a scalar reference to one Machine in the selected input. Kind defaults may supply the reference, but a conventional name,
sole Machine, hostname, capability or local-access declaration never selects
it implicitly. Controller egress belongs to the selected Machine's
[proxy choice](machines.md#machine-proxy).

The referenced Machine must have effective `os.provided: true`
and `access.local: true`; it receives the container runtime only when it
runs a container, which no Machine declares
([host packages](../controller.md#host-packages)).
Selection never supplies these values or changes an SSH transport into local
execution. This is the sole local-access Machine in the
retained graph and cannot be a node of a selected OpenShiftCluster or
CephCluster. Other retained Machines must not declare local access.

Each controller violation is refused with `api.invariant` at
`spec.controller.machineRef` and names its remedy on the Machine it concerns:
set `os.provided` to true, give the controller `access.local: true` and no
`access.ssh`, remove
`access.local` from the other Machine, or remove the controller from the
cluster's nodes; or select another controller Machine.

The controller is retained even when no service consumes it or cluster
selection excludes every other use. Its declaration must still be in the
selected input; a reference does not load a file outside it. The
controller Machine's provider and provider-host closure is retained normally.
These are Environment relationships, not a new Machine role, type, capability
or API kind. A Machine may represent the controller in more than one context;
this alone grants no shared-service ownership or mutation coordination.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: example

spec:
  domains:
    base: example.test

  controller:
    machineRef: controller

---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: controller

spec:
  os:
    provided: true

  proxy:
    direct: {}

  access:
    local: true
```

A controller may host a managed service when that service explicitly selects
its `machineRef`, which makes the controller a container-runtime host: its
[controller stage](../controller.md#the-controller-stage) then installs an
absent Podman.
No service placement defaults to the controller.

Admission checks these declarations without inspecting the invoking host,
opening runtime state or moving execution. The
[Controller contract](../controller.md) defines setup and verified host
binding; [Architecture](../architecture.md#controller-host-and-local-services)
owns local-effect boundaries. Registry policy, image pins and service
connection facts stay with their owning consumers and service objects.

Additional installation trust belongs to each
[OpenShiftCluster](openshift-clusters.md#additional-installation-trust). Kind
defaults can share that consumer field using its normal replacement rules.
Each service declares any connection trust it requires separately.

The controller Machine's route follows the grammar every acquisition route
shares, the one the [context-free route](../controller.md#the-context-free-acquisition-route)
reads from the environment. That grammar is held where each value is declared,
for every consumer: a Proxy's endpoints by
[its own admission](infrastructure-services.md#proxy-dns-ntp-and-registry-deployment),
and each bypass entry by the [proxy choice](infrastructure-services.md#proxy-choice)
that declares it, each naming its field and never the value. This Environment
therefore adds no refusal of its own. A shape only this executable cannot
realize is refused where the [compiler boundary](../api.md#compiler-boundary)
states, and the table below lists the controller's.

### Refusal table

Each row is one controller shape this executable cannot realize, and
`TestControllerRefusalTableMatchesUnsupported` holds the controller stage
capability's `Unsupported` to it. A plan or apply refuses each one before
registration with `lifecycle.unsupported` naming this Environment, and
controller selection refuses the first one it meets with
`controller.unsupported` on the declaring object and field. Its reason and
remedy are the diagnostic's message and remediation, in which `<machine>` is
the controller Machine, `<proxy>` the Proxy it selects and `<environment>` this
Environment.

| Refusal | Path | Reason | Remedy |
| --- | --- | --- | --- |
| Too many bypass entries | more than 128 `spec.proxy.noProxy` entries on the controller `Machine` | `this executable's controller route carries at most 128 bypass entries, and <machine> declares more in spec.proxy.noProxy` | `keep at most 128 entries in spec.proxy.noProxy on <machine>` |
| Managed Proxy | `spec.proxy.proxyRef` of the controller `Machine` names a managed `Proxy` | `this executable's controller stage acquires only directly or through an external Proxy that is already ready, and <machine> selects the managed <proxy>` | `select direct: {} or an external Proxy in spec.proxy on <machine>` |
| Proxy authentication | `spec.connection.auth.proxyAuthRef` on the selected external `Proxy` | `this executable's controller stage acquires through no authenticated proxy, and <proxy> sets spec.connection.auth.proxyAuthRef` | `select direct: {} or an external Proxy that needs no authentication in spec.proxy on <machine>` |
| Private trust | `spec.connection.trustBundleRef` on the selected external `Proxy` | `this executable's controller stage trusts only the host's system trust store, and <proxy> sets spec.connection.trustBundleRef` | `select direct: {} or an external Proxy without spec.connection.trustBundleRef that the host's system trust store verifies in spec.proxy on <machine>` |
| HTTP proxy alone | `spec.connection.httpProxy` without `spec.connection.httpsProxy` on the selected external `Proxy` | `every dependency source is HTTPS, so the spec.connection.httpProxy of <proxy> alone selects no controller acquisition route` | `set connection.httpsProxy on <proxy> to the proxy endpoint; every dependency source is HTTPS` |
| Libvirt release pin | `spec.dependencyVersions.libvirt` other than `latest`, with the libvirt client selected | `this executable's controller stage installs the libvirt release the host's repositories provide, and <environment> pins spec.dependencyVersions.libvirt` | `remove spec.dependencyVersions.libvirt from <environment>` |
| Client release below the minimum | `spec.dependencyVersions.<tool>` older than the minimum this executable's tool table holds, with that client selected | `this executable accepts <tool> <minimum> or newer, and <environment> declares <release> in spec.dependencyVersions.<tool>` | `declare <tool> <minimum> or newer in spec.dependencyVersions.<tool> of <environment>, or remove it for this build's default` |
| Client release without MAJOR.MINOR.PATCH | `spec.dependencyVersions.<tool>` with no leading `MAJOR.MINOR.PATCH`, with that client selected | `this executable holds <tool> to <minimum> or newer by its MAJOR.MINOR.PATCH, and <environment> declares <release> in spec.dependencyVersions.<tool>, which names none` | `declare <tool> <minimum> or newer in spec.dependencyVersions.<tool> of <environment>, or remove it for this build's default` |
| Unpinned client behind the artifact mirror | a selected public-origin client at a release the tool table does not pin, with `spec.mirror.artifacts` | `with spec.mirror.artifacts, this executable acquires a public-origin client only at a release its tool table pins, and <environment> selects <tool> <release>, whose checksum only its publisher holds` | `declare no <tool> release in spec.dependencyVersions of <environment> so it takes this build's pinned default, or remove spec.mirror.artifacts` |
| OKD clients behind the artifact mirror | an OKD cluster's clients, with `spec.mirror.artifacts` | `with spec.mirror.artifacts, this executable acquires a public-origin client only at a release its tool table pins, and <environment> selects <tool> <release>, whose checksum only its publisher holds` | `select an openshift distribution, whose clients come from their vendor, or remove spec.mirror.artifacts from <environment>` |
| Managed artifact mirror on another name for the controller host | a managed `spec.mirror.artifacts` on a Machine other than the controller `Machine` with `access.local`, or reached on SSH port 22 at a controller address | `this executable places a managed artifact mirror on the controller host only through its controller Machine, and <server> is placed on <host>, another name for that host` | `place <server> on <machine> or on another Machine reached over SSH` |
| Managed artifact mirror on a provider host | its `Machine`, other than the controller `Machine`, hosting or bridged by a libvirt provider | `the artifact mirror is filled before the controller stage, and <server> on <host> waits for <provider>, which that Machine hosts or whose bridge carries its address` | `place <server> on a Machine that hosts no InfraProvider and whose address no provider bridges, or select an artifact mirror the operator fills in spec.mirror.artifacts.url of <environment>` |
| Managed artifact mirror behind a managed Proxy | its `Machine` selecting a managed `Proxy` | `the artifact mirror on <host> is filled before any managed service runs, and <host> reaches the publishers through the managed <proxy>` | `select direct: {} or an external Proxy in spec.proxy on <host>` |
| Managed artifact mirror without an HTTPS URL | `spec.mirror.artifacts.endpointRef` resolving to no HTTPS URL a client path fits beneath | `the artifact mirror is read over HTTPS beneath <server>'s selected endpoint, and that endpoint names no HTTPS URL a client path fits beneath` | `select an https endpoint of <server> in spec.mirror.artifacts.endpointRef of <environment>` |
| Mirror services on two Machines | a managed image and artifact mirror on two Machines | `the image and artifact mirrors share one mirror host, and <registry> is on <host> while <server> is on <other>` | `place <registry> and <server> on one Machine` |
| Managed image mirror on another name for the controller host | a managed `spec.mirror.registry` on such a Machine, as for the artifact mirror | `this executable places a managed image mirror on the controller host only through its controller Machine, and <registry> is placed on <host>, another name for that host` | `place <registry> on <machine> or on another Machine reached over SSH` |
| Managed image mirror without an address | `spec.mirror.registry.endpointRef` resolving to no address | `the image mirror is read at <registry>'s selected endpoint, and that endpoint names no address on <host>` | `select an endpoint of <registry> with an address of <host> in spec.mirror.registry.endpointRef` |
| Managed image mirror behind a managed Proxy | its `Machine` selecting a managed `Proxy` | `the image mirror on <host> is filled before the services that pull from it, and <host> reaches the origins through the managed <proxy>` | `select direct: {} or an external Proxy in spec.proxy on <host>` |
| Managed image mirror on a provider host | its `Machine` hosting a libvirt provider, other than the controller Machine, or bridged by one | `the image mirror is filled before the emulated BMCs that pull from it, and <registry> on <host> waits for <provider>, which that Machine hosts or whose bridge carries its address` | `place <registry> on a Machine no InfraProvider hosts or bridges, or select an external Registry in spec.mirror.registry.registryRef of <environment>` |

## Lifecycle rescue declaration

`spec.lifecycle` contains only optional `rescue`. When present, `rescue`
contains exactly these required fields in order:

| Field | Type | Rule |
| --- | --- | --- |
| `imageRef` | string | `MachineImage` containing RHEL Anaconda boot media. |
| `os` | object | Exact `{family: rhel, version: <non-empty>, architecture}` tuple. |
| `artifactServerEndpoint` | object | Required `{serverRef, endpointRef}` selecting a managed ArtifactServer directly; both references are required. |

The selected artifact server must run on a Machine with `os.provided: true` so
it remains reachable after managed machines shut down. Validation checks the
declaration and its references. No rescue journey exists yet, so admission
refuses a present `rescue` with `api.invariant` at `spec.lifecycle.rescue`,
naming that no rescue journey exists yet, with the remedy to remove it
(`TestAdmissionRefusesARescueDeclarationUntilARescueJourneyExists`). The schema
keeps the field, and the rules above, for a later journey; no lifecycle
requires or consumes it.

## Aggregate invariants and read-only boundary

Proxy, mirror, Satellite and endpoint URLs verify TLS by default and contain no
userinfo. Defaults never create Secret objects; Secrets and Entitlements are
first-class kinds. Lifecycle authorization is command intent, never desired
state.
