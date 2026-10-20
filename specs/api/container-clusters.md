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

The [topology](#topology) rules for `release.version` are the general rule
unless a per-minor specialization names its major and minor. A version that
names no numeric minor takes the general rule, and the installer judges the
release (D162). A release pinned by image alone declares no minor, so only the
release-independent topology rules apply to it, and
[selection](../container-clusters.md#selection-and-refusal) refuses it.

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
| `vsphere` | Optional arm containing optional `vsphere.nodeNetworking.external.networkSubnetCidr[]` and `vsphere.nodeNetworking.internal.networkSubnetCidr[]`, and optional `vsphere.vcenterCredentials[]` of `{server, credentialsRef}`. The YAML spelling ends in `Cidr`. |
| `none` | No platform arm. |
| `external` | Optional `external` arbitrary map, preserved without interpretation by the read-only compiler. |

When the complete platform object is omitted, normalization derives it:

- every single-node cluster becomes `type: none`;
- multi-node libvirt or bare-metal machines whose three endpoint slots all take
  the `openshift` source become `type: baremetal` with `provisioningNetwork:
  disabled`, and those whose slots take no `openshift` source become
  `type: none`; a mix is refused at `spec.install.endpoints`, because the
  installer owns either all three addresses or none of them;
- multi-node vSphere machines become `type: vsphere`; and
- multi-node KubeVirt machines become `type: none`.

Derivation follows `nodes[].machineRef` to each `Machine` and then its
`substrate.providerRef`. Mixed provider types require an explicit platform. An
unresolved machine reference is reported at that reference and suppresses the
secondary cannot-derive error. An authored platform always wins.

`vsphere.vcenterCredentials` is the cluster's own vCenter credential, set keyed
by `server`: each `server` names a `vcenters[].server` of the nodes' provider,
each `credentialsRef` a `usernamePassword` Secret, and when present it names
every vCenter the nodes' failure domains use. It is never inferred from the
provider's provisioning `credentialsRef`; naming that same Secret is an
explicit choice. A cluster that declares it requires `topology.folder` on every
failure domain its nodes use and each such domain's `datacenter` among its
vCenter's `datacenters`. `nodeNetworking` is accepted and reaches no
agent-installer input.

A multi-node KubeVirt cluster may author `baremetal`; every slot's source is
then `openshift`, and every node's attachments are `Localnet`
ClusterUserDefinedNetworks or `bridge` NetworkAttachmentDefinitions.

Not yet met: the vSphere arm admits no `vcenterCredentials`, a KubeVirt
`baremetal` cluster's attachment rule is not enforced, and every multi-node
libvirt or bare-metal cluster derives `baremetal` whatever its slots' sources
are, with a mixed slot set admitted; tracked as
[B733](../milestones/m3.md#b733), [B88](../milestones/m3.md#b88) and
[B707](../milestones/m3.md#b707).

## Endpoints

`spec.install.endpoints` has only the `api`, `api-int`, and `ingress` slots.
`api` and `ingress` are authored. If `api-int` is omitted, normalization copies
only `api.address` and `api.source`; an authored `api-int` wins.

Every endpoint has this exact shape:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `address` | string | for `openshift` and `external` | — | IP literal owned by `openshift` or `external`; absent for load-balancer and node sources, which resolve it. Never in `::/96`; an IPv4-mapped address is admitted. |
| `dnsName` | string | no | — | DNS subdomain naming the endpoint. |
| `port` | integer | no | consumer default | `1..65535` when set. |
| `scheme` | string | no | consumer default | `http` or `https`. |
| `prefixLength` | integer | no | — | Valid only with `address`; `1..32` for IPv4 or `1..128` for IPv6. |
| `interfaceNetworks` | array of strings | no | `[]` | Valid CIDRs narrowing the interface that carries an owned address; effective state masks host bits. |
| `source.type` | string | no | `openshift` | `openshift`, `external`, `loadBalancer`, or `node`. |
| `source.loadBalancerRef` | string | conditional | — | Global LoadBalancer; valid only for `loadBalancer`. |
| `source.bindAddressRef` | string | conditional | sole bind address | LoadBalancer-local `bindAddresses[].name`; valid only for `loadBalancer`. |

`openshift` and `external` own an authored `address`, which every such slot
requires: [resolution before boot](../container-clusters.md#installation)
proves only a frozen address, so a `dnsName` satisfies no slot. An effective
address in the IPv4-compatible range `::/96` is refused whatever its source,
because glibc prints much of that range in another form than the frozen one;
write the IPv4 address instead. `loadBalancer` forbids an authored address: the
LoadBalancer and optional
[bind-address ref](infrastructure-services.md#loadbalancer) resolve it.

`node` is valid only for a one-node cluster and also forbids an authored
address. Effective normalization resolves that node Machine's
`network.installAddressRef` using the [Machine selection rules](machines.md#network-configuration)
and materializes the selected host IP without its prefix. Missing or ambiguous
installation candidates are errors. Single-node clusters reject the
default `openshift` source for all three slots; `source.type: node` is the
recommended form so one machine address is not repeated in three places, while
`external` with an `address` is also valid.

A multi-node cluster whose effective platform is `none`, authored or derived
for KubeVirt machines, also rejects the `openshift` source for every slot, at
its `source.type`: the installer receives no VIPs on that platform, so nothing
in the cluster would answer the endpoint. An `external` or `loadBalancer`
source is valid there.

On a multi-node `baremetal` or `vsphere` platform the installer owns all three
endpoint slots as virtual IPs its own VIP holders answer. Each takes the
`openshift` source with an authored `address`; an `external` or `loadBalancer`
source is refused at its `source.type`, because the 4.20 to 4.22 agent
installers pass no load-balancer type to the service that installs the cluster,
which deploys its VIP holders whatever the input says (the installer's
development branch does pass one), and on `vsphere` the agent installer ignores
`platform.vsphere.loadBalancer`; an external load balancer is declared with
platform `none`, Bootwright's modelling choice rather than a claim about every
release. `api-int` resolves the `api` address, and the `api` and
`ingress` addresses differ. API, internal API, ingress, and node IPs obey the
selected machine-network CIDRs. VIPs do not collide with node install IPs, and
endpoint/network address families are consistent.

Not yet met: such a platform admits `external` and `loadBalancer` endpoints, an
`api-int` address other than `api`'s and an `ingress` address equal to `api`'s;
tracked as [B707](../milestones/m3.md#b707).

**Endpoint addresses are the cluster's own.** An effective endpoint address
equals no effective endpoint address of another selected ContainerCluster, no
IP address a Machine declares other than the node a single-node cluster's
`node` source names, no host address of a managed libvirt network attachment,
no bind or endpoint address of a managed DNSServer, NTPServer, Proxy,
ArtifactServer or Registry, no emulated controller's bind address, and no
LoadBalancer bind address other than the one its own `loadBalancer` source
selects; two clusters never select one LoadBalancer bind address. A wildcard
bind claims no address here. Within one cluster `api-int` may equal `api`, and
on platform `none` slots may share one external or load-balancer address. Each
refusal is an `api.invariant` at the slot's address, or at its source when the
address is resolved, naming the other holder. A managed libvirt network's
prefix belongs to one context through its exclusive reservation, so no address
in it is shared across contexts; on any other network Bootwright coordinates no
address, as for node addresses.

Not yet met: only a collision with the cluster's own node install addresses is
refused; tracked as [B708](../milestones/m3.md#b708).

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

Not yet met: an opaque public-only `publicKeyRef` passes admission and planning
but the media apply reads only an `sshKeyPair` public part and fails; tracked
as [B306](../milestones/m3.md#b306).

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
| `ovnKubernetesConfig` | object | no | absent; `mtu`, `gatewayConfig.routingViaHost`, `gatewayConfig.ipForwarding` (`Restricted` or `Global`), each written into the install-time `Network` only when set. |
| `secondaryNetworks` | array | no | `[]`; localnet secondary networks below. |

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

### Secondary networks

Each `secondaryNetworks[]` entry is keyed by `name`, a DNS label that is
neither `physnet` nor `exgwphysnet`, and becomes the name of the network, of its
physical network and of the attachment definition workloads select.

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `topology` | string | yes | `Localnet`. |
| `uplink.bridge` | string | yes | `br-ex`, sharing the default bridge, or a new OVS bridge name of at most 15 characters. |
| `uplink.interface` | string | with a new bridge | An addressless `ethernet`, `bond` or `vlan` every node declares, other than its install interface. |
| `vlan` | integer | no | Access VLAN `1..4094`; distinct among networks sharing an uplink. |
| `mtu` | integer | no | `576..65536`, at least `1280` with an IPv6 subnet, at most the uplink interface's declared MTU. |
| `ipam.mode` | string | no | `Disabled` (default) or `Enabled`. |
| `ipam.subnets` | array of CIDRs | with `Enabled` | One or two, of different families. |
| `ipam.excludeSubnets` | array of CIDRs | no | At most 25, inside `subnets`. |
| `ipam.lifecycle` | string | no | `Persistent`, with `Enabled` only. |
| `namespaceSelector.matchLabels` | map | yes | Non-empty Kubernetes labels selecting the namespaces that receive the attachment definition. |

A cluster that declares a secondary network needs a binding of an add-on
providing `nmstate`.

Not yet met: `ovnKubernetesConfig` and `secondaryNetworks` are not admitted;
tracked as [B719](../milestones/m3.md#b719) and
[B726](../milestones/m3.md#b726).

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

On the agent path the roles resolve to the installer's `enableOn` value: every
represented role is `all`, `master` alone is `masters`, and `worker`, `infra` or
both is `workers`; the installer accepts no other selector, whatever its custom
resource's enumeration lists.

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
rule below accepts for the declared release. Agent installers refuse other
control-plane counts. Two install upstream only beside an arbiter pool or
fencing credentials, and no node role is an arbiter or carries fencing, so no
rule accepts two. Whatever the release, a cluster with one `master` declares
no `worker` or `infra` node, because the agent installer refuses compute
replicas beside a single control-plane replica. Each of these refusals is an
`api.invariant` at `spec.nodes`.

The general rule applies to every release minor without a specialization
(D162); the installer judges the release itself. A per-minor specialization
replaces it only where an installer accepts a different roster.

| Rule | Master counts | Read from `openshift/installer` |
| --- | --- | --- |
| general | 1, 3, 4, 5 | 4.21 [agent control plane](https://github.com/openshift/installer/blob/release-4.21/pkg/asset/agent/installconfig.go#L217-L232), [agent single node](https://github.com/openshift/installer/blob/release-4.21/pkg/asset/agent/installconfig.go#L234-L263), [install-config control plane](https://github.com/openshift/installer/blob/release-4.21/pkg/types/validation/installconfig.go#L758-L771); 4.22 [agent control plane](https://github.com/openshift/installer/blob/release-4.22/pkg/asset/agent/installconfig.go#L220-L305), [agent single node](https://github.com/openshift/installer/blob/release-4.22/pkg/asset/agent/installconfig.go#L307-L336), [install-config two nodes](https://github.com/openshift/installer/blob/release-4.22/pkg/types/validation/installconfig.go#L1731-L1753); 4.23 [agent control plane](https://github.com/openshift/installer/blob/release-4.23/pkg/asset/agent/installconfig.go#L220-L305), [agent single node](https://github.com/openshift/installer/blob/release-4.23/pkg/asset/agent/installconfig.go#L307-L336), [install-config two nodes](https://github.com/openshift/installer/blob/release-4.23/pkg/types/validation/installconfig.go#L1803-L1825) |

There is no per-minor specialization. The rule is `generalTopology` in
`internal/containercluster/topology.go`; a per-minor specialization, when an
installer first differs, is a record there with its own row in this table and a
row of the
[version-specialization catalog](../../.agents/knowledge/version-specializations.md).
`TestTopologyTableMatchesSpec` keeps this table and the code one, and
`TestDocsVersionSpecializationCatalogMatchesRegistries` keeps the code and the
catalog one.

## Cross-object invariants

- Every bound machine's provider, profile, network selection, attachment, NIC
  binding, root-device hints, BMC facts, and install IP satisfy the machine and
  provider rules in [machines.md](machines.md).
- A cluster's consumed machine networks and effective endpoint sources are
  resolved from the full object graph, never by file order or same-name
  guessing.
