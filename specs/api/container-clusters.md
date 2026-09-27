# Container clusters

`ContainerCluster` owns OpenShift/OKD install intent: references, endpoint and
artifact selections, security and network defaults. Native installer files are
derived outputs, and [container clusters](../container-clusters.md) owns what
installing one does. [The compiler boundary](../api.md#compiler-boundary)
applies; `platform.external` remains inert until validated by a
release-specific consumer.

## Shape

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: edge

spec:
  distribution:
    type: openshift
    release:
      version: 4.21.15

  install:
    method: agent
    mode: connected

    platform:
      type: baremetal
      baremetal:
        provisioningNetwork: disabled

    endpoints:
      api:
        address: 192.0.2.10
        source:
          type: external

      ingress:
        address: 192.0.2.11
        source:
          type: external

  nodes:
    - name: master-0
      role: master
      machineRef: master-0
```

## Distribution and release

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.distribution.type` | string | no | `openshift` | `openshift` or `okd`. |
| `spec.distribution.release.version` | string | conditional | — | Release version; required unless a pinned `image` is present. |
| `spec.distribution.release.channel` | string | no | derived for OpenShift | OpenShift-only. With OpenShift `version` and no `image`, omission defaults to `stable-<major>.<minor>`. |
| `spec.distribution.release.image` | string | conditional | — | Release-image pull spec pinned by a version tag or SHA-256 digest; untagged and `latest` selections are rejected. |

An authored image wins as the exact release payload. Declaration validity
claims no renderer support for that release.

The major and minor of `release.version` must have a row in the
[topology table](#topology); a version whose minor has none, or that names no
numeric minor, refuses at `spec.distribution.release.version`. A release pinned
by image alone declares no minor, so only the release-independent topology
rules apply to it, and [selection](../container-clusters.md#selection-and-refusal)
refuses it.

## Install selection

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.install.method` | string | no | `agent` | `agent` is the only accepted method. |
| `spec.install.mode` | string | no | `connected` | `connected` or `disconnected`. |
| `spec.install.platform` | object | no | derived | Closed union described below. |
| `spec.install.endpoints` | map | yes | — | Closed keys `api`, `api-int`, and `ingress`; endpoint shape below. |
| `spec.install.agent.redfishVirtualMedia.artifactServerEndpoint` | object | conditional | — | Required when any bound machine uses bare metal. |
| `spec.install.agent.bootArtifacts.artifactServerEndpoint` | object | conditional | — | Required in disconnected mode. |
| `spec.install.proxy` | choice object | no | direct access | Independent Proxy choice for installation and cluster policy. |
| `spec.install.ntp` | array of selections | no | native OS default | NTPServer selections; an empty list clears inherited selections. |
| `spec.install.registries` | object | no | — | Cluster-owned mirror selection and image-source policy below. |
| `spec.install.pullSecretRef` | string | OpenShift | environment/convention | `dockerConfigJson` `Secret`; not required for OKD. |
| `spec.install.nodeSSH` | object | normalized | environment/convention | Cluster administration public/private SSH material. |
| `spec.install.additionalTrustBundleRefs` | array of strings | no | `[]` | Ordered unique `caBundle` Secret references for this cluster's native installation trust. |
| `spec.install.servingCertificates` | object | no | — | Typed API and ingress serving-certificate refs below. |

Disconnected mode requires `spec.install.registries.mirror`, whose Registry
declares `trustBundleRef`, and a managed agent boot-artifacts endpoint.
Connected mode obtains boot artifacts from the release payload and does not
use an authored boot-artifacts selection.

Both consumers use the shared
[artifact endpoint selection](infrastructure-services.md#artifactserver), and
require a managed artifact server.

The Redfish endpoint serves virtual media for bare-metal nodes; the boot
artifacts endpoint serves rootfs, kernel, and initramfs content. They are
separate selections even when one component provides both.

### Installation network services

`spec.install.proxy` uses the shared
[proxy choice](infrastructure-services.md#proxy-choice), independently of
Machine choices, including the selected controller's proxy. `spec.install.ntp`
uses the shared
[NTP selection list](infrastructure-services.md#dns-and-ntp-selection-lists),
and its omission leaves the native OS default. These fields describe the
downstream installation and do not mutate or inherit Machine OS installation
policy.

### Registry policy

`spec.install.registries` contains optional `mirror` then
`imageDigestSources`. `mirror` selects a declared Registry; connection details,
credentials and trust belong to that object.

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `mirror.registryRef` | string | with mirror | Global Registry name; never inferred. |
| `mirror.endpointRef` | string | conditional | [Registry endpoint selection](infrastructure-services.md#registry-selection). |
| `imageDigestSources[].source` | string | yes | Source image registry. |
| `imageDigestSources[].mirrors` | array of strings | yes | Non-empty ordered mirror registries. |
| `imageDigestSources[].sourcePolicy` | string | no | `NeverContactSource` or `AllowContactingSource`. |

Source entries are unique by `source`. Registry locations contain no inline
credentials. Defaults can share complete cluster mirror policy; another cluster's
choice never changes this cluster's route. A mirror selection and its image
source mapping describe different facts and do not implicitly create each
other.

### Platform union and derivation

`spec.install.platform.type` is `baremetal`, `vsphere`, `none`, or `external`.
The matching configuration arm is optional for `baremetal`, `vsphere`, and
`external`, while every nonmatching arm is forbidden. `none` has no arm.

| Platform | Exact arm |
| --- | --- |
| `baremetal` | Optional arm containing optional `baremetal.provisioningNetwork`: `disabled`, `managed`, or `unmanaged`; omission leaves the release/native default. |
| `vsphere` | Optional arm containing optional `vsphere.nodeNetworking.external.networkSubnetCidr[]` and `vsphere.nodeNetworking.internal.networkSubnetCidr[]`. The YAML spelling ends in `Cidr`. |
| `none` | No platform arm. |
| `external` | Optional `external` arbitrary map, preserved without interpretation by the read-only compiler. |

When the complete platform object is omitted, normalization derives it:

- every single-node cluster becomes `type: none`;
- multi-node libvirt or bare-metal machines become `type: baremetal` with
  `provisioningNetwork: disabled`;
- multi-node vSphere machines become `type: vsphere`; and
- multi-node KubeVirt machines become `type: none`.

Derivation follows `nodes[].machineRef` to each `Machine` and then its
`substrate.providerRef`. Mixed provider types require an explicit platform. An
unresolved machine reference is reported at that reference and suppresses the
secondary cannot-derive error. An authored platform always wins.

## Endpoints

`spec.install.endpoints` has only the `api`, `api-int`, and `ingress` slots.
`api` and `ingress` are authored. If `api-int` is omitted, normalization copies
only `api.address` and `api.source`; an authored `api-int` wins.

Every endpoint has this exact shape:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `address` | string | source- and topology-dependent | — | Optional IP literal owned by `openshift` or `external`; absent for load-balancer and node sources and required when that direct source must supply a VIP. |
| `dnsName` | string | no | — | DNS subdomain naming the endpoint. |
| `port` | integer | no | consumer default | `1..65535` when set. |
| `scheme` | string | no | consumer default | `http` or `https`. |
| `prefixLength` | integer | no | — | Valid only with `address`; `1..32` for IPv4 or `1..128` for IPv6. |
| `interfaceNetworks` | array of strings | no | `[]` | Valid CIDRs narrowing the interface that carries an owned address; effective state masks host bits. |
| `source.type` | string | no | `openshift` | `openshift`, `external`, `loadBalancer`, or `node`. |
| `source.loadBalancerRef` | string | conditional | — | Global LoadBalancer; valid only for `loadBalancer`. |
| `source.bindAddressRef` | string | conditional | sole bind address | LoadBalancer-local `bindAddresses[].name`; valid only for `loadBalancer`. |

`openshift` and `external` may own an authored `address`; otherwise `dnsName`
can satisfy a non-VIP slot. `loadBalancer` forbids an authored address: the
LoadBalancer and optional
[bind-address ref](infrastructure-services.md#loadbalancer) resolve it.

`node` is valid only for a one-node cluster and also forbids an authored
address. Effective normalization resolves that node Machine's
`network.installAddressRef` using the [Machine selection rules](machines.md#network-configuration)
and materializes the selected host IP without its prefix. Missing or ambiguous
installation candidates are errors. Single-node clusters reject the
default `openshift` source for all three slots; `source.type: node` is the
recommended form so one machine address is not repeated in three places, while
`external` with a sufficient `dnsName` is also valid.

A multi-node cluster whose effective platform is `none`, authored or derived
for KubeVirt machines, also rejects the `openshift` source for every slot, at
its `source.type`: the installer receives no VIPs on that platform, so nothing
in the cluster would answer the endpoint. An `external` or `loadBalancer`
source is valid there. Multi-node libvirt and bare-metal machines derive
`baremetal`, which carries the VIPs.

On a multi-node `baremetal` or `vsphere` platform, all three endpoint slots are
VIP-bearing. Each therefore resolves an address directly or through an
`loadBalancer`; `dnsName` alone is insufficient. API, internal API, ingress,
and node IPs obey the selected machine-network CIDRs. VIPs do not collide with
node install IPs, and endpoint/network address families are consistent.

## Credentials and certificates

[Environment kind defaults](environment.md#kind-defaults) apply before the
following conventional fallbacks. An omitted OpenShift `pullSecretRef` may
receive `Environment.spec.defaults.ContainerCluster.install.pullSecretRef`;
if absent there too, normalization injects `openshift-pull-secret`. The
resulting name must still resolve to a declared `dockerConfigJson` `Secret`.

`install.nodeSSH` is a presence union:

- `keyPairRef` names an `sshKeyPair` `Secret` and is mutually exclusive with
  the split fields; or
- `publicKeyRef` names public SSH material and is required when the split form
  is authored; optional `privateKeyRef` names the matching private material.

When the whole block is omitted, it defaults from
`Environment.spec.defaults.ContainerCluster.install.nodeSSH`; if that is
absent, normalization injects
`keyPairRef: <cluster-name>-cluster-admin-ssh-key`. An authored block wins as a
whole; key-pair and split-key choices are never combined through defaults.
The reference must still resolve. Public-only material is sufficient to
declare installation but cannot authorize a cluster SSH command.

`servingCertificates` admits only:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `apiServer.namedCertificates` | array | when `apiServer` is set | Non-empty entries of `{names, secretRef}`. |
| `apiServer.namedCertificates[].names` | array of strings | yes | Non-empty unique DNS names; the internal `api-int` name is forbidden. |
| `apiServer.namedCertificates[].secretRef` | string | yes | `tlsCertificate` `Secret`. |
| `ingress.defaultCertificateRef` | string | when `ingress` is set | `tlsCertificate` `Secret`. |

### Additional installation trust

`spec.install.additionalTrustBundleRefs` selects additional CA bundles for this
cluster's native install rendering. Each scalar reference resolves directly to
a `caBundle` Secret, and authored order is retained. The complete list is the
cluster's additional trust selection; there is no Environment trust list to
append to it.

Kind defaults may share the choice:

```yaml
spec:
  defaults:
    ContainerCluster:
      install:
        additionalTrustBundleRefs:
          - installation-ca
```

An omitted cluster field receives this list under the ordinary
[kind-default rules](environment.md#kind-defaults), then defaults to `[]` if
still absent. An authored list replaces the complete default, and an explicit
`additionalTrustBundleRefs: []` clears inherited additional trust. Defaults do
not create Secret declarations or read certificate material.

This selection does not add controller, Machine OS, SSH, service or ambient
process trust. Proxy and Registry connection trust, entitlement trust and
other consumer-specific trust remain on their owning objects. The former
`Environment.spec.trustedCAs` is rejected; there is no compatibility merge or
implicit trust inherited from another consumer.

## Networking

`spec.networking` has the exact fields:

| Field | Type | Required | Default |
| --- | --- | --- | --- |
| `networkType` | string | no | Installer default when absent. |
| `clusterNetwork` | array of `{cidr, hostPrefix}` | normalized | Family-dependent default below. |
| `serviceNetwork` | array of CIDR strings | normalized | Family-dependent default below. |

The two lists default independently. An authored non-empty list retains its
membership and order while effective output canonicalizes CIDR text:

| Consumed machine networks | `clusterNetwork` default | `serviceNetwork` default |
| --- | --- | --- |
| IPv4 or unresolved/mixed evidence | `[{cidr: 10.128.0.0/14, hostPrefix: 23}]` | `[172.30.0.0/16]` |
| IPv6-only | `[{cidr: fd01::/48, hostPrefix: 64}]` | `[fd02::/112]` |

Every cluster-network `cidr` is valid and its required `hostPrefix` is larger
than the CIDR prefix and no larger than `32` or `128`. Service CIDRs are valid.
The consumed `NetworkConfig.spec.machineNetwork` values, cluster networks, service
networks, node install addresses, and endpoint addresses form one supported
address family; dual-stack or mixed-family input is rejected by this contract.

## Security

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.security.fips.enabled` | boolean | no | `false` | `true` is OpenShift-only. |
| `spec.security.diskEncryption` | object | no | — | Cluster-node TPM2 disk-encryption intent. |
| `spec.security.diskEncryption.unlock.tpm2` | empty object | with encryption | — | The only unlock arm; `pcrBank` and `pcrIds` are forbidden here. |
| `spec.security.diskEncryption.roles` | array of strings | no | all represented pools | Unique values from `master`, `worker`, and `infra`. |

Roles select the native machine-config pools receiving this one encryption
configuration; they do not assign node roles or access permissions. When
encryption is present, omission selects every represented node role. An
authored role selection resolves to at least one declared node. `infra` folds
into the worker machine-config pool: selecting `worker` or `infra` targets that
pool, and selecting both produces one configuration. This cannot express an
independent infra-only encryption boundary or contradictory per-node
overrides. TPM2 inventory and install-time projection are fail-closed
effect-time checks; the desired-state declaration never triggers a hardware
read or disk operation. General destructive and secret boundaries are defined
in [security.md](../security.md) and
[state-reconciliation.md](../state-reconciliation.md).

## Nodes

`spec.nodes` is non-empty. There are no authorable control-plane, compute, or
machine-pool blocks; the node roster and roles are the single source from which
a native renderer derives pools and replica counts.

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `spec.nodes[].name` | string | yes | Cluster-local DNS label, unique by short name. It is not defaulted from `machineRef`. |
| `spec.nodes[].fqdn` | string | no | Explicit DNS-subdomain override for the node identity. |
| `spec.nodes[].role` | string | yes | `master`, `worker`, or `infra`. |
| `spec.nodes[].machineRef` | string | yes | Global `Machine` reference. |
| `spec.nodes[].labels` | map of string to string | no | Kubernetes label keys and values. |
| `spec.nodes[].taints` | array | no | Entries have required `key` and `effect`, optional `value`; effects are `NoSchedule`, `PreferNoSchedule`, or `NoExecute`. |

Normalization composes the node FQDN under
[Environment domains](environment.md#domains); an authored `fqdn` wins verbatim.

An `infra` node installs through the native worker pool but retains its
authoring role for post-install placement intent.
Desired-state compilation accepts and preserves repeated authored taints. A
native projection deduplicates by `key` plus `effect`; explicitly authoring the
standard infra taint is therefore a no-op rather than an error.

Every `machineRef` requires capability `openshift-node` and `os.provided: false`.
Network and root-device input comes only from that Machine. The common
[graph invariants](../api.md#graph-validation) enforce unique node binding.

### Topology

At least one node is a `master`, and the number of `master` nodes is one the
declared release's row below accepts. The release's install-config validation
refuses only zero control-plane replicas; its agent installer narrows that to
the listed counts. Two install upstream only beside an arbiter pool, which
[needs a feature gate](https://github.com/openshift/installer/blob/release-4.21/pkg/types/validation/installconfig.go#L138-L144),
and no node role is an arbiter, so no row accepts two. Whatever the release, a
cluster with one `master` declares no `worker` or `infra` node, because every
row's agent installer refuses compute replicas beside a single control-plane
replica. Each of these refusals is an `api.invariant` at `spec.nodes`. A
release minor with no row refuses at `spec.distribution.release.version`, as
[distribution and release](#distribution-and-release) states, with a
remediation naming the qualified minors.

| Release minor | Master counts | Read from `openshift/installer` |
| --- | --- | --- |
| `4.21` | 1, 3, 4, 5 | [agent control plane](https://github.com/openshift/installer/blob/release-4.21/pkg/asset/agent/installconfig.go#L217-L232), [agent single node](https://github.com/openshift/installer/blob/release-4.21/pkg/asset/agent/installconfig.go#L234-L263), [install-config control plane](https://github.com/openshift/installer/blob/release-4.21/pkg/types/validation/installconfig.go#L758-L771) |

The rows are `releaseTopologies` in `internal/containercluster/topology.go`.
Qualifying a minor means reading its installer branch and adding its row to
both tables: `TestTopologyTableMatchesSpec` keeps them one, and
`TestEveryExampleReleaseHasATopologyRow` holds every example to a qualified
minor, so an example's release bump reviews the table.

## Cross-object invariants

- Every bound machine's provider, profile, network selection, attachment, NIC
  binding, root-device hints, BMC facts, and install IP satisfy the machine and
  provider rules in [machines.md](machines.md).
- A cluster's consumed machine networks and effective endpoint sources are
  resolved from the full object graph, never by file order or same-name
  guessing.
