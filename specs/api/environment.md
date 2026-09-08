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
| `spec.defaults` | kind-keyed partial specs | no | `{}` | Omitted object fields inherit the corresponding kind entry under the rules below. |
| `spec.downloads` | object | no | source-specific | Closed download-mirror policy below. |
| `spec.proxy` | object | no | direct access | Default and per-consumer proxy selection below. |
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

## Kind defaults

`spec.defaults` is a closed map keyed by exact, case-sensitive names from the
21-kind catalog. Each value is a partial copy of that kind's `spec`, without
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
   arms may be omitted in a fragment. Conflicting present arms and locally
   provable type violations remain errors. Recipient-dependent requirements
   and references are checked after application. An unused entry must still be
   a valid partial spec; it creates no object or retention edge by itself.
2. Apply `defaults.Environment`, if present, once to the selected Environment's
   other spec fields before resource and cluster selection. That entry cannot
   contain `defaults`. It cannot widen the acquired filesystem source universe
   or change the selected Environment identity. The authored defaults map is
   never itself defaulted or recursively reloaded.
3. Apply each other kind entry once to each resource-selected object before
   provider, catalog, conventional, built-in, or reference-derived fallbacks.
   Explicit object values win over Environment values; the owning kind's
   existing fallback order applies only to values still absent.
4. Fill absent attributes recursively inside nonempty schema-defined record
   objects. An explicit scalar, including `false`, `0`, or an empty string,
   remains explicit and must be valid. An explicit empty nested object is a
   whole authored value; it does not request Environment inheritance. The
   recipient's root `spec` is the container of attributes, not such a nested
   value. Explicit null is invalid and never requests fallback.
5. Lists, open/native maps, authentication choices, `install.nodeSSH`, and
   `Secret.source` are whole values. Copy them only when absent; never append
   records, patch native payloads, or combine different key/source material.
   A valid explicit empty collection or source block wins completely.
6. An authored discriminator or populated implementation arm selects the
   variant, using that union's own arm-population rules. An explicitly unset
   value-shape arm, such as an empty/all-zero replicated pool block, does not
   select a variant.
   Defaults never introduce an alternative arm or change that selection.
   Compatible ordinary records within the selected arm may inherit missing
   fields. Without an authored selection, defaults may select one variant.
   Applied defaults must produce a valid complete union; inactive default arms
   are not applied to a differently selected variant.
   The same protection applies to schema-declared forbidden branches controlled
   by an explicit mode or feature choice: HTTP suppresses inherited TLS,
   disabled authentication suppresses inherited OAuth configuration, and
   external storage management suppresses inherited managed Ceph configuration.
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
| `virtctlMirror` | string | no | host-cluster source | Absolute HTTP(S) base URL for the matching `virtctl`. |
| `helmMirror` | string | no | upstream source | Absolute HTTP(S) base URL for Helm's `latest` channel. |

URLs require scheme and host and reject embedded credentials. These fields
select download sources; they do not fill attributes on cluster objects.

Secret source and custody declarations belong to
[each Secret](secrets.md#source-union). `Environment` has no `secretStorage`
setting. A default may supply an omitted Secret source, but cannot override an
authored source or copy material. Moving operator-owned file material into
context storage requires an explicit source migration and authorized import.

## Proxy selection and service catalogs

`spec.proxy` is a closed object with fields in this order:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `defaultRef` | string | no | direct access | Nonempty name of one `infraComponents.proxies[]` row. |
| `bootwright` | choice object | no | `defaultRef`, otherwise direct | Controller-side proxy choice. |
| `containerClusterInstall` | choice object | no | `defaultRef`, otherwise direct | Container installation proxy choice. |
| `machineOSInstall` | choice object | no | `defaultRef`, otherwise direct | Machine OS installation proxy choice; a selected proxy must be external. |

Each authored consumer choice contains exactly one `proxyRef: <catalog-name>`
or `direct: {}`. `proxyRef` is a nonempty string resolving to a proxy row;
`direct` accepts no parameters and opts out even if `defaultRef` exists.
An empty consumer object, null, unknown key/reference, or conflicting arm is
invalid. Empty strings and the former string `none` selector are not choices.

Resolve choices after applying kind defaults. An omitted consumer inherits
`defaultRef`; with no default it uses direct access. If no `proxy` remains, or
it is explicitly `{}`, all consumers use direct access. Proxy catalog presence,
row order, singleton status, and ambient proxy variables never select a route.
`defaultRef` is the only proxy default selector; proxy rows have no default flag.
A row named `default` is an ordinary row, not a keyword. Existing catalog name
constraints remain in force.

Managed proxy dependencies must be ready before their consumers run. Failure
or unavailability never silently falls back to direct access. Machine OS
installation may select only an external proxy because a managed proxy does
not exist before its own host's OS is installed.

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
state which management mode they permit. No catalog or built-in fallback
supplies `endpointRef`; an applicable kind default may supply it explicitly.

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
