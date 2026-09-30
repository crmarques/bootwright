# Environment and fleet defaults

`Environment` owns fleet domains, sites, selected state, access/install
defaults, controller selection and policy. Its name identifies the complete
effective graph and lifecycle unit. Shared services are independent objects in
[infrastructure-services.md](infrastructure-services.md). [The compiler boundary](../api.md#compiler-boundary)
applies to every declaration below.

## Environment

Environment fields emit in this order:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.domains` | object | yes | — | Fleet DNS zones; closed shape below. |
| `spec.sites` | array of objects | no | omitted | Estate site registry, keyed by `name`. |
| `spec.resources` | array of strings | no | all discovered YAML | Non-empty ordered file/directory allow-list relative to the Environment file. |
| `spec.containerClusters` | array of strings | no | all loaded container clusters | Non-empty unique `ContainerCluster` root selection. |
| `spec.storageClusters` | array of strings | no | all loaded storage clusters | Non-empty unique `StorageCluster` root selection. |
| `spec.remoteMachinesAccessKey` | object | conditional | — | Fleet key for the `bootwright` account installed on managed machines. |
| `spec.defaults` | kind-keyed partial specs | no | `{}` | Omitted object fields inherit the corresponding kind entry under the rules below. |
| `spec.downloads` | object | no | source-specific | Closed download-mirror policy below. |
| `spec.dependencyVersions` | object | no | `latest` at apply | Version intent for the prerequisites this Environment controller stage installs; closed shape below. |
| `spec.controller` | object | yes | — | Required controller Machine selection below. |
| `spec.lifecycle` | object | no | — | Offline-rescue input; a present `rescue` is refused until a rescue journey exists. |

Omitted optional arrays remain omitted unless their owning rule declares a
materialized default. Authored arrays reject duplicate entries by their
documented identity.

## Dependency versions

`spec.dependencyVersions` controls the versions this Environment's own
[controller stage](../state-reconciliation.md#stages-and-the-pause-boundary)
installs. Fields emit in the following order. Every field is optional and
accepts a string; omission means `latest` when the stage resolves the
dependency. Admission preserves authored values and does not materialize
release numbers or contact publishers.

| Field | Dependency | Exact override |
| --- | --- | --- |
| `libvirt` | Native libvirt client, including `virsh` | Distribution package version or `[EPOCH:]VERSION-RELEASE`. |
| `helm` | Helm | Stable `MAJOR.MINOR.PATCH`, optionally prefixed by `v`. |
| `govc` | vSphere client | Stable `MAJOR.MINOR.PATCH`, optionally prefixed by `v`. |
| `virtctl` | Upstream KubeVirt client | Stable `MAJOR.MINOR.PATCH`, optionally prefixed by `v`. |

The private interpreter, `ansible-core` and the baseline native packages have
no field here. They are [host prerequisites](../controller.md): one prepared
host serves every context, so no single Environment may move their versions.
`python`, `ansible`, `podman`, `openssh` and `nmstate` are therefore rejected
like any other unknown key.

`latest` selects the publisher's latest stable release for
the generic target clients. For native packages it selects the newest available
build from the approved repositories for the executing OS release and
architecture. A native version without a release selects the highest available
build of that version, using the package manager's version ordering. Version
ranges, wildcards, prereleases for generic clients, nulls and empty strings
are invalid. Native values are at most 96 ASCII characters, start with an
alphanumeric character, and otherwise contain only alphanumerics or `._+~^:-`.

The selected desired-state graph determines which dependencies are needed.
An override alone does not select an unused libvirt, Helm, govc or virtctl
client. Supporting Python wheels and native package dependencies are resolved
as a complete compatible closure; they are not individually configurable.
An incompatible or unavailable exact request fails with a dependency
diagnostic rather than silently substituting another root version.
The controller adapter runs the qualified `ansible-core` minor that
[development](../../docs/development.md#qualified-hosts-and-images) records, a
[host prerequisite](../controller.md#supported-host-and-dependency-selection)
no Environment versions.

OpenShift/OKD installer and client versions remain tied to the target cluster's
declared release. `openshift-install`, `oc` and `kubectl` are not override keys.
`virtctl` defaults to upstream latest; declare an explicit version when the
target virtualization installation requires a particular client. No version
is inferred from a running target cluster.

For example:

```yaml
spec:
  dependencyVersions:
    libvirt: latest
    helm: "4.3.0"
    virtctl: "1.9.0"
```

These fields follow the normal `defaults.Environment` inheritance rules.
Resolution, retention, retry and reuse follow
[the controller stage](../controller.md#the-controller-stage):
a serving retained resolution is reused as frozen, so moving a `latest`
dependency forward means declaring the newer release here.

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
  `<node.name>.<ContainerCluster.metadata.name>.<domains.containerClusters>`;
  and
- a storage node without an explicit `fqdn` is
  `<node.name>.<StorageCluster.metadata.name>.<domains.storageClusters>`.

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

## Resource and cluster selection

`spec.resources` refines the YAML universe acquired through the CLI and
discovered under [the API input rules](input.md#discovery):

- every value is non-empty, has no surrounding whitespace, is relative to the
  Environment file, and remains within that file's directory after cleaning;
- a value names a lowercase `.yaml`/`.yml` file or a directory containing at
  least one discoverable such file; skipped directories, reserved payload
  roots, and their descendants from [discovery](input.md#discovery) are
  outside this universe, and directory traversal and selected-file ordering
  are lexical;
- the Environment file is always selected;
- a listed YAML file is selected as a complete multi-document stream;
- a listed file or directory must not be a symlink, descendant directory
  symlinks are not followed, and a selected YAML symlink is an input error;
- duplicate cleaned paths are invalid; and
- omission selects every discovered YAML file, while an authored empty list is
  invalid.

A native add-on descriptor published by the add-on registration journey
([B89](../milestones/m7.md#b89) owns its storage and publication) is the sole selection exception.
`add-ons/_store/<name>/add-on.yaml` is selected when it contains exactly one
`ClusterAddon/<name>` and the same directory contains the exact sibling basename
`.bootwright-addon`. `<name>` is a DNS label of at most 63 ASCII bytes, and the
marker's complete UTF-8 content is exactly `<name>` followed by one LF. A
byte-order mark, carriage return, missing or additional LF, or any other
leading, trailing, or internal whitespace is invalid. The directory segment,
marker content, and `ClusterAddon.metadata.name` must be identical.

Markers obey [input ceilings and processing order](input.md#fixed-input-ceilings).
Open each relative to its held verified sibling directory with no-follow
semantics; the opened handle must prove a stable regular file with link count
one. A missing, linked, non-regular, unstable, oversized, malformed,
mismatched, duplicate or ambiguous marker grants no exception. It grants
selection only, never package authenticity or execution authority; see
[the add-on boundary](../add-ons.md#trust-boundary).

An excluded-file warning identifies its relative path, Bootwright object
identities and safe recovery guidance under the
[validation-report contract](../cli/output.md#json-output). Only a path within
the Environment directory can be suggested for `resources`; an outside file
requires relocating its declaration first. Non-Bootwright files produce no
such warning.

`spec.containerClusters` and `spec.storageClusters` select effective cluster
roots after resource decoding and reference-independent normalization. Each
authored list must be non-empty, contain unique names, and resolve to its
corresponding kind. Omission of either list selects every loaded root of that
class. Objects outside the selected cluster-owned graph are absent from
effective state, while resource-selected fleet-global objects and
resource-selected shared services remain available. Each excluded cluster
root produces a deterministic `validate` warning. These fields are selection
lists, not `Ref` fields, and no CLI flag can further narrow them.

The cluster-selection closure is authoritative and independent of file layout:

| Starting selection | Retained objects |
| --- | --- |
| Every selection | The selected `Environment`, its required controller Machine and that Machine's provider/provider-host closure, plus resource-selected `Entitlement`, `MachineImage`, `MachineInstallProfile`, `NetworkConfig`, `Proxy`, `DNSServer`, `NTPServer`, `ArtifactServer`, `Registry`, `LoadBalancer`, `CustomPlaybook`, and `Secret` objects; every retained managed service also retains its placement Machine and that Machine's `InfraProvider` and provider-host Machine closure. |
| One `ContainerCluster` | That root; its node Machines; the Machines' `InfraProvider` objects and any libvirt provider-host Machines; Machines hosting managed infrastructure services consumed by the root; bindings for the root plus their recursively expanded profiles and add-ons; and each `StorageExport` attached through a selected add-on's `storageExportAttachment` effect, its referenced storage cluster, policy/pool/filesystem/gateway chain, and every `StorageNFSExport` on that retained storage cluster. |
| One `StorageCluster` | That root and every `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`, `StorageObjectGateway`, `StorageNFSExport`, and `StorageExport` naming it; its node, provider, provider-host, and consumed-service Machines; and each `ContainerCluster` attached to one of those exports through a `storageExportAttachment` effect, together with that container root's bindings, recursively expanded profiles, and add-ons. |

Closures for all listed roots are unioned and then emitted in canonical API
order. The rules apply until nothing more is retained. A `ContainerCluster`
retained through an attachment brings everything its own row names, its node
Machines and its other attached exports and their chains included. A
`StorageCluster` retained only through a `StorageExport` keeps its node
Machines and their provider and provider-host closure, but not every object
naming it and no other attached `ContainerCluster`. A cluster root retained
this way is not excluded and produces no warning, even when an authored list
omits it. A `Machine` or `InfraProvider` not reached by the rules above is
excluded. A reference from an always-retained object to an excluded object is
not an implicit retention edge; it is an unresolved-reference error that tells
the author to make the selections consistent. Omission of both cluster lists
skips this filtering and retains every resource-selected object.

## Remote-machine access and install defaults

`spec.remoteMachinesAccessKey.keyRef` is a plain reference to an `sshKeyPair`
`Secret`. It is required as soon as a selected `Machine` sets
`os.installProfileRef`. A managed-OS install authorizes its public half for the
product-owned `bootwright` account and uses its private half for that account;
an already provided machine keeps its own `Machine.spec.access`.

The fleet key must differ from every Machine access key and every
`StorageCluster.spec.ceph.cephadm.clusterSSH.keyRef`. Each cluster SSH key must
also differ from every Machine-authored private key. Validation checks
reference identity and type, preventing Ceph key reuse for fleet access.

## Kind defaults

`spec.defaults` is a closed map keyed by exact, case-sensitive names from the
26-kind catalog. Each value is a partial copy of that kind's `spec`, without
an additional `spec` wrapper. For example:

```yaml
spec:
  defaults:
    ContainerCluster:
      install:
        pullSecretRef: openshift-pull-secret
```

Every loaded `ContainerCluster` missing `spec.install.pullSecretRef` receives
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
   other spec fields before resource and cluster selection. That entry cannot
   contain `defaults`. It cannot widen the acquired filesystem source universe
   or change the selected Environment identity. The authored defaults map is
   never itself defaulted or recursively reloaded.
3. Apply each other kind entry once to each resource-selected object before
   provider, conventional, built-in, or reference-derived fallbacks.
   Explicit object values win over Environment values; the owning kind's
   existing fallback order applies only to values still absent.
4. Fill absent attributes recursively inside nonempty schema-defined record
   objects. An explicit scalar, including `false`, `0`, or an empty string,
   remains explicit and must be valid. An explicit empty nested object is a
   whole authored value; it does not request Environment inheritance. The
   recipient's root `spec` is the container of attributes, not such a nested
   value. Explicit null is invalid and never requests fallback.
5. Lists, open/native maps, authentication and proxy choices, `install.nodeSSH`, and
   `Secret.source` are whole values. Copy them only when absent; never append
   records, patch native payloads, or combine different key/source material.
   A valid explicit empty collection or source block wins completely.
6. An authored discriminator or populated implementation arm selects the
   variant, using that union's own arm-population rules. An explicitly unset
   value-shape arm, such as an empty or all-zero
   [replicated pool block](storage.md#replicated-protection), does not select
   a variant.
   Defaults never introduce an alternative arm or change that selection.
   Compatible ordinary records within the selected arm may inherit missing
   fields. Without an authored selection, defaults may select one variant.
   Applied defaults must produce a valid complete union; inactive default arms
   are not applied to a differently selected variant.
   The same protection applies to schema-declared forbidden branches controlled
   by an explicit mode or feature choice: HTTP suppresses inherited TLS,
   disabled authentication suppresses inherited OAuth configuration, and
   external management suppresses inherited managed service and Ceph configuration.
   This does not remove authored forbidden fields or repair arbitrary failed
   prerequisites; those remain errors.
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

## Download mirrors

`spec.downloads` is a closed map with these fields in order:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `openshiftClientsMirror` | string | no | upstream source | Absolute HTTP(S) base URL for OpenShift client downloads. |
| `virtctlMirror` | string | no | upstream source | Absolute HTTP(S) base URL for the resolved `virtctl` release. |
| `helmMirror` | string | no | upstream source | Absolute HTTP(S) base URL for the resolved Helm release. |

URLs require scheme and host and reject embedded credentials. These fields
select download sources; they do not fill attributes on cluster objects. A
mirror is qualified only over HTTPS on the default port or port 443, without a
query string or fragment. Helm archives reside directly under the base URL;
OpenShift archives and `virtctl` binaries reside under `<base>/<exact-version>/`.
The `virtctl` directory and filename include the release's `v` prefix.
Publisher metadata determines versions and checksums even when a mirror supplies
the artifact bytes.

Secret source and custody declarations belong to
[each Secret](secrets.md#source-union). `Environment` has no `secretStorage`
setting. A default may supply an omitted Secret source, but cannot override an
authored source or copy material, and a retired `file` source it supplies is
[refused](secrets.md#file-source).

## Controller Machine

`spec.controller` is a required closed record containing only required
`machineRef`, a scalar reference to one Machine in the resource-selected
input. Kind defaults may supply the reference, but a conventional name,
sole Machine, hostname, capability or local-access declaration never selects
it implicitly. The former `controller.proxy` is unknown; controller egress
belongs to the selected Machine's [proxy choice](machines.md#machine-proxy).

The referenced Machine must have effective `os.provided: true`,
`access.local: true` and the declared `container-runtime` capability.
Selection never supplies these values or changes an SSH transport into local
execution. This is the sole local-access Machine in the
retained graph and cannot be a node of a selected ContainerCluster or
StorageCluster. Other retained Machines must not declare local access.

The controller is retained even when no service consumes it or cluster
selection excludes every other use. Its declaration must still be in the
selected resource universe; a reference does not load excluded files. The
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
  capabilities:
    - container-runtime

  os:
    provided: true

  proxy:
    direct: {}

  access:
    local: true
```

A controller may host a managed service when that service explicitly selects
its `machineRef` and the Machine has the service's required capabilities.
`container-runtime` is required for the controller even when no managed service
is selected. A capability declaration does not install or prove a runtime;
`setup` installs an absent qualified Podman and verifies its dependencies.
No service placement defaults to the controller.

Admission checks these declarations without inspecting the invoking host,
opening runtime state or moving execution. The
[Controller contract](../controller.md) defines setup and verified host
binding; [Architecture](../architecture.md#controller-host-and-local-services)
owns local-effect boundaries. The former Environment fields
`spec.proxy`, `spec.infraComponents`, `spec.registries`,
`spec.componentImages`, and `spec.trustedCAs` remain unknown. Registry policy,
image pins and service connection facts stay with their owning consumers and
service objects.

Additional installation trust belongs to each
[ContainerCluster](container-clusters.md#additional-installation-trust). Kind
defaults can share that consumer field using its normal replacement rules.
Each service declares any connection trust it requires separately.

## Lifecycle rescue declaration

`spec.lifecycle` contains only optional `rescue`. When present, `rescue`
contains exactly these required fields in order:

| Field | Type | Rule |
| --- | --- | --- |
| `imageRef` | string | `MachineImage` containing RHEL 9 Anaconda boot media; a remote image requires its checksum. |
| `os` | object | Exact `{family: rhel, version: 9.<minor>[.<patch>...], architecture}` tuple. |
| `artifactServerEndpoint` | object | Required `{serverRef, endpointRef}` selecting a persistent managed ArtifactServer directly; both references are required. |

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
state; the retired `safety` block remains unknown.
