# Environment and fleet defaults

`Environment` owns fleet domains, sites, selected state, access/install
defaults, shared-service catalogs and policy. Its name identifies the complete
effective graph and lifecycle unit. Catalog `InfraComponent` objects live in
[machines.md](machines.md). [The compiler boundary](../api.md#compiler-boundary)
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
| `spec.defaults` | object | no | — | Cluster-install material and client mirrors. |
| `spec.secretStorage` | object | no | `mode: source` | Custody mode for file-sourced Secrets. |
| `spec.proxyFor` | object | no | each consumer inherits the default proxy | Per-consumer proxy catalog selection. |
| `spec.infraComponents` | object | no | — | External/managed shared-service access catalogs. |
| `spec.registries` | object | no | — | Disconnected registry mirror intent. |
| `spec.trustedCAs` | object | no | — | Fleet-wide additional CA trust for native install rendering. |
| `spec.lifecycle` | object | no | — | Offline-rescue input; the declaration exposes no lifecycle command. |
| `spec.componentImages` | object | no | — | Closed managed-component image-pin map. |

Omitted optional arrays remain omitted unless their owning rule declares a
materialized default. Authored arrays reject duplicate entries by their
documented identity.

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
discovered under [the API input rules](../api.md#environment-directory-and-selected-state):

- every value is non-empty, has no surrounding whitespace, is relative to the
  Environment file, and remains within that file's directory after cleaning;
- a value names a lowercase `.yaml`/`.yml` file or a directory containing at
  least one discoverable such file; skipped directories, reserved payload
  roots, and their descendants from `api.md` are outside this universe, and
  directory traversal and selected-file ordering are lexical;
- the Environment file is always selected;
- a listed YAML file is selected as a complete multi-document stream;
- a listed file or directory must not be a symlink, descendant directory
  symlinks are not followed, and a selected YAML symlink is an input error;
- duplicate cleaned paths are invalid; and
- omission selects every discovered YAML file, while an authored empty list is
  invalid.

A context-generated native add-on descriptor is the sole selection exception.
`add-ons/_store/<name>/add-on.yaml` is selected when it contains exactly one
`ClusterAddon/<name>` and the same directory contains the exact sibling basename
`.bootwright-addon`. `<name>` is a DNS label of at most 63 ASCII bytes, and the
marker's complete UTF-8 content is exactly `<name>` followed by one LF. A
byte-order mark, carriage return, missing or additional LF, or any other
leading, trailing, or internal whitespace is invalid. The directory segment,
marker content, and `ClusterAddon.metadata.name` must be identical.

Markers obey [input ceilings and processing order](../api.md#fixed-input-ceilings).
Open each relative to its held verified sibling directory with no-follow
semantics; the opened handle must prove a stable regular file with link count
one. A missing, linked, non-regular, unstable, oversized, malformed,
mismatched, duplicate or ambiguous marker grants no exception. It grants
selection only, never package authenticity or execution authority; see
[the add-on package contract](../add-ons.md#package-standard).

An excluded-file warning identifies its relative path, Bootwright object
identities and a path that can be added to `resources`. Non-Bootwright files
produce no such warning.

`spec.containerClusters` and `spec.storageClusters` select effective cluster
roots after resource decoding and reference-independent normalization. Each
authored list must be non-empty, contain unique names, and resolve to its
corresponding kind. Omission of either list selects every loaded root of that
class. Objects outside the selected cluster-owned graph are absent from
effective state, while resource-selected fleet-global objects and
Environment-selected shared services remain available. Each excluded cluster
root produces a deterministic `validate` warning. These fields are selection
lists, not `Ref` fields, and no CLI flag can further narrow them.

The cluster-selection closure is authoritative and independent of file layout:

| Starting selection | Retained objects |
| --- | --- |
| Every selection | The selected `Environment` plus resource-selected `Entitlement`, `MachineImage`, `MachineInstallProfile`, `NetworkConfig`, `InfraComponent`, `CustomPlaybook`, and `Secret` objects; every retained `InfraComponent` also retains its placement Machine and that Machine's `InfraProvider` and provider-host Machine closure. |
| One `ContainerCluster` | That root; its node Machines; the Machines' `InfraProvider` objects and any libvirt provider-host Machines; Machines hosting managed infrastructure services consumed by the root; bindings for the root plus their recursively expanded profiles and add-ons; and each `StorageExport` attached through a selected add-on's `storageExportAttachment` effect, its referenced storage cluster, policy/pool/filesystem/gateway chain, and every `StorageNFSExport` on that retained storage cluster. |
| One `StorageCluster` | That root and every `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`, `StorageObjectGateway`, `StorageNFSExport`, and `StorageExport` naming it; its node, provider, provider-host, and consumed-service Machines; and each `ContainerCluster` attached to one of those exports through a `storageExportAttachment` effect, together with that container root's bindings, recursively expanded profiles, and add-ons. |

Closures for all listed roots are unioned and then emitted in canonical API
order. A `Machine` or `InfraProvider` not reached by the rules above is
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

`spec.defaults` emits `install`, `clientsMirror`, `virtctlMirror`, then
`helmMirror`:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `install.pullSecretRef` | string | no | cluster convention | Default `dockerConfigJson` Secret name copied only to a `ContainerCluster` that omits its install pull secret. |
| `install.nodeSSH` | object | no | cluster convention | Same closed shape as `ContainerCluster.spec.install.nodeSSH`; copied only when that cluster omits it. |
| `clientsMirror` | string | no | upstream source | Absolute HTTP(S) base URL for OpenShift client downloads. |
| `virtctlMirror` | string | no | host-cluster source | Absolute HTTP(S) base URL for the matching `virtctl`. |
| `helmMirror` | string | no | upstream source | Absolute HTTP(S) base URL for Helm's `latest` channel. |

URLs must include scheme and host and must not embed credentials. A diagnostic
against a copied pull-secret or node-SSH reference identifies the value as
defaulted and names the cluster field that overrides it.

## Secret storage

`spec.secretStorage.mode` accepts `source` or `context` and defaults to
`source`:

- `source` leaves `Secret.spec.source.file` material at its declared
  operator-owned paths; and
- `context` requires an explicit materialization command to copy that
  material into confidential context storage before a secret-consuming
  operation.

This field selects custody policy only; materialization is an explicit effect.

## Proxy selection and service catalogs

`spec.proxyFor` contains only `bootwright`, `containerClusterInstall`, and
`machineOSInstall`. Each value is empty, the literal `none`, or the name of one
`spec.infraComponents.proxies[]` entry. Empty inherits the one default proxy,
or the sole proxy when exactly one exists; `none` opts out. The machine-OS
install consumer may resolve only to an external proxy because a managed proxy
does not exist before that OS is installed.

`spec.infraComponents` contains catalogs in this field order: `proxies`,
`nameResolution`, `artifactServers`, `registries`, then `ntp`. Every entry has
a unique DNS-label `name` other than the reserved `none`, and a required
`management` of `external` or `managed`. A managed row requires a
`componentRef` to the matching `InfraComponent` arm and forbids external
connection facts. An external row forbids `componentRef` and supplies the
facts named below.

### Proxy catalog

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Catalog identity. |
| `default` | boolean | no | `false` | At most one proxy row is marked default. |
| `management` | string | yes | — | `external` or `managed`. |
| `componentRef` | string | conditional | — | Required for managed; selects `InfraComponent.spec.proxy`. |
| `endpointRef` | string | no | — | Managed endpoint name on the selected component. |
| `connection` | object | conditional | — | Required for external and forbidden for managed. |
| `connection.httpProxy` | string | no | — | Absolute HTTP(S) proxy URL without userinfo. |
| `connection.httpsProxy` | string | no | — | Absolute HTTP(S) proxy URL without userinfo. |
| `connection.noProxy` | array of strings | no | — | Ordered native no-proxy entries. |
| `connection.auth.proxyAuthRef` | string | no | — | `usernamePassword` Secret. |
| `connection.trustBundleRef` | string | no | — | `caBundle` Secret for TLS inspection. |

An external connection sets at least one of `httpProxy`, `httpsProxy`, or
`noProxy`. Credentials are always separate Secret references.

### Name-resolution catalog

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `name` | string | yes | Catalog identity. |
| `management` | string | yes | `external` or `managed`. |
| `componentRef` | string | conditional | Required for managed; selects `InfraComponent.spec.nameResolution`. |
| `endpointRef` | string | no | Managed endpoint name on the selected component. |
| `address` | string | conditional | Required valid IP for external and canonicalized in effective state; forbidden for managed. |
| `additionalIngressHosts` | array of strings | no | Additional ingress hostnames. |

Selected `NetworkConfig.spec.nameResolutionRefs` may resolve to at most one
distinct managed name-resolution component. Unused catalog rows and aliases of
that same component do not count; external rows do not participate in this
managed-service limit.

### Artifact-server catalog

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Catalog identity. |
| `default` | boolean | no | `false` | At most one artifact-server row is marked default. |
| `management` | string | yes | — | `external` or `managed`. |
| `componentRef` | string | conditional | — | Required for managed; selects `InfraComponent.spec.artifactServer`. |
| `endpoints` | array of objects | conditional | — | Required non-empty for external; forbidden for managed. |
| `endpoints[].name` | string | yes | — | Unique endpoint identity. |
| `endpoints[].url` | string | yes | — | Absolute HTTP(S) endpoint URL. |

A consumer's `artifactServerEndpoint` is a closed object with string fields
`serverRef` then `endpointRef`. The optional `serverRef` names a catalog row;
omission selects its default or sole row. The required `endpointRef` names an
endpoint on that row's managed component or external endpoint list. Consumers
state which management mode they permit. No default supplies `endpointRef`.

### Registry catalog

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `name` | string | yes | — | Catalog identity. |
| `default` | boolean | no | `false` | At most one registry row is marked default. |
| `management` | string | yes | — | `external` or `managed`. |
| `componentRef` | string | conditional | — | Required for managed; selects `InfraComponent.spec.registry`. |
| `endpointRef` | string | no | — | Managed endpoint name on the selected component. |
| `url` | string | conditional | — | Required external registry URL; forbidden for managed. |

### NTP catalog

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `name` | string | yes | Catalog identity. |
| `management` | string | yes | `external` or `managed`. |
| `componentRef` | string | conditional | Required for managed; selects `InfraComponent.spec.ntp`. |
| `endpointRef` | string | no | Managed endpoint name on the selected component. |
| `address` | string | conditional | Required IP or DNS hostname for external; forbidden for managed. |

Load balancers intentionally have no Environment catalog. A container-cluster
endpoint directly selects a managed load-balancer component or declares an
external endpoint.

## Registries and trusted CAs

`spec.registries` emits `mirror` then `imageDigestSources`:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `mirror.url` | string | no | External mirror root. |
| `mirror.credentialsRef` | string | no | Registry credential Secret. |
| `mirror.trustBundleRef` | string | no | `caBundle` Secret. |
| `imageDigestSources[].source` | string | yes | Source image registry. |
| `imageDigestSources[].mirrors` | array of strings | yes | Non-empty ordered mirror registries. |
| `imageDigestSources[].sourcePolicy` | string | no | `NeverContactSource` or `AllowContactingSource`. |

Source entries are unique by `source`. Registry locations contain no inline
credentials. A disconnected container-cluster install requires mirror trust
and either an external mirror URL or a managed registry catalog entry.

`spec.trustedCAs.caBundleRefs` is an ordered unique array of `caBundle` Secret
refs. It adds trust only to native install rendering, never controller, SSH,
OS, service or ambient process trust.

## Lifecycle rescue declaration

`spec.lifecycle` contains only optional `rescue`. When present, `rescue`
contains exactly these required fields in order:

| Field | Type | Rule |
| --- | --- | --- |
| `imageRef` | string | `MachineImage` containing RHEL 9 Anaconda boot media; a remote image requires its checksum. |
| `os` | object | Exact `{family: rhel, version: 9.<minor>[.<patch>...], architecture}` tuple. |
| `artifactServerEndpoint` | object | Required `{serverRef?, endpointRef}` selecting a persistent managed artifact server; `serverRef` may use the catalog default. |

The selected artifact server must run on a Machine with `os.provided: true` so
it remains reachable after managed machines shut down. A selected graph with a
bare-metal Machine whose OS is not provided requires this complete rescue
declaration before a fresh apply. Validation checks the declaration and refs.

## Component image pins

`spec.componentImages` is a closed two-level map. The only accepted paths are:

| Path | Implementation |
| --- | --- |
| `loadBalancer.haproxy` | Managed HAProxy load balancer. |
| `registry.mirror-registry` | Managed mirror registry. |
| `proxy.squid` | Managed Squid proxy. |
| `nameResolution.dnsmasq` | Managed dnsmasq resolver. |
| `artifactServer.http` | Managed HTTP artifact server. |

Each leaf contains optional `local` then `public` image references and must set
at least one. Every reference has an explicit non-`latest` version tag or
content digest. Unknown component or implementation keys are rejected.

## Aggregate invariants and read-only boundary

Proxy, mirror, Satellite and endpoint URLs verify TLS by default and contain no
userinfo. Defaults never create Secret objects; Secrets and Entitlements are
first-class kinds. Lifecycle authorization is command intent, never desired
state; the retired `safety` block remains unknown.
