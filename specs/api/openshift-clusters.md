# OpenShift clusters

`OpenShiftCluster` owns OpenShift and OKD install intent: references, endpoint and
artifact selections, security and network defaults. OKD is a distribution
variant of this kind (`spec.distribution.type: okd`), not a kind of its own, so
B748's OKD arm is a value of this field. The kind is the `containerCluster`
category's member of the [cluster family registry](../api.md#cluster-family-registry):
the Environment selects it through `spec.containerClusters`, and the general
flow reads its node binding through the membership the cluster domain publishes
(`spec.nodes[].machineRef`), never by naming the kind. Native installer files are
derived outputs, and [OpenShift clusters](../openshift-clusters.md) owns what
installing one does. [The compiler boundary](../api.md#compiler-boundary)
applies; `platform.external` remains inert until validated by a
release-specific consumer.

OpenShiftCluster `metadata.name` follows the
[one name rule](../api.md#object-names).

## Kinds

| Kind | Declares |
| --- | --- |
| `OpenShiftCluster` | One OpenShift or OKD cluster: its distribution, install selection, nodes, endpoints, credentials, networking and security. |

## Shape

```yaml
apiVersion: bootwright.io/v1alpha1
kind: OpenShiftCluster
metadata:
  name: edge

spec:
  distribution:
    type: openshift
    release:
      version: 4.21.15

  install:
    mode: connected

    platform:
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
      role: controlPlane
      machineRef: master-0
```

## Distribution and release

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.distribution.type` | string | no | `openshift` | `openshift` or `okd`. |
| `spec.distribution.release.version` | string | conditional | — | Exact `major.minor.patch` publisher release, including prereleases such as `4.22.0-rc.1`; required unless a pinned `image` is present. |
| `spec.distribution.release.channel` | string | no | derived for OpenShift | OpenShift-only. With OpenShift `version` and no `image`, omission defaults to `stable-<major>.<minor>`. |
| `spec.distribution.release.image` | string | conditional | — | Release-image pull spec pinned by a version tag or SHA-256 digest; untagged and `latest` selections are rejected. |

An authored image wins as the exact release payload. Declaration validity
claims no renderer support for that release.

The [topology](#topology) rules are the general rule, and the installer judges
the release (D162). `release.version` keys no specialization. A release pinned
by image alone declares no minor, so only the release-independent topology
rules apply to it, and
[selection](../openshift-clusters.md#selection-and-refusal) refuses it.

## Install selection

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.install.agent` | object | no | `{}` | The agent installer's arm, the only installation method; its presence selects it. |
| `spec.install.mode` | string | no | `connected` | `connected` or `disconnected`. |
| `spec.install.platform` | object | no | derived | Presence union described below. |
| `spec.install.endpoints` | map | yes | — | Closed keys `api`, `api-int`, and `ingress`; endpoint shape below. |
| `spec.install.agent.redfishVirtualMedia.artifactServerEndpoint` | object | conditional | — | Required when any bound machine uses bare metal. |
| `spec.install.agent.bootArtifacts.artifactServerEndpoint` | object | conditional | — | Required in disconnected mode. |
| `spec.install.proxy` | choice object | no | direct access | Independent Proxy choice for installation and cluster policy. |
| `spec.install.ntp` | array of selections | no | native OS default | NTPServer selections; an empty list clears inherited selections. |
| `spec.install.registries` | object | no | — | Cluster-owned mirror selection and image-source policy below. |
| `spec.install.pullSecretRef` | string | OpenShift | environment/convention | `dockerConfigJson` `Secret`; not required for OKD. Effect material: the installation writes it into the cluster. |
| `spec.install.nodeSSH` | object | normalized | environment/convention | Cluster administration public/private SSH material. |
| `spec.install.additionalTrustBundleRefs` | array of strings | no | `[]` | Ordered unique `caBundle` Secret references for this cluster's native installation trust. Effect material: the installation adds them to the cluster's trust. |
| `spec.install.additionalTrustBundlePolicy` | string | no | — | `Always` or `Proxyonly`; requires non-empty `additionalTrustBundleRefs`. Written to the installer only when selected. |
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

`spec.install.platform` is a presence union: exactly one of the arms
`baremetal`, `vsphere`, `none`, or `external` is present, and the present arm is
the platform. Two arms, or a present `platform` with no arm, refuse
`api.invariant` at `spec.install.platform`.

| Platform | Exact arm |
| --- | --- |
| `baremetal` | Object containing optional `provisioningNetwork`: `disabled`, `managed`, or `unmanaged`; omission leaves the release/native default. |
| `vsphere` | Object containing optional `nodeNetworking.external.networkSubnetCidr[]` and `nodeNetworking.internal.networkSubnetCidr[]`, and optional `vcenterCredentials[]` of `{server, credentialsRef}`. The YAML spelling ends in `Cidr`. |
| `none` | Empty object `{}`. |
| `external` | Arbitrary map, preserved without interpretation by the read-only compiler. |

When the complete platform object is omitted, normalization derives it:

- every single-node cluster becomes `none: {}`;
- multi-node libvirt or bare-metal machines whose three endpoint slots all take
  the `installer` source become `baremetal` with `provisioningNetwork:
  disabled`, and those whose slots take no `installer` source become
  `none: {}`; a mix is refused at `spec.install.endpoints`, because the
  installer owns either all three addresses or none of them;
- multi-node vSphere machines become `vsphere: {}`; and
- multi-node KubeVirt machines become `none: {}`.

Derivation follows `nodes[].machineRef` to each `Machine` and then its
`substrate.providerRef`. Mixed provider types require an explicit platform. An
unresolved machine reference is reported at that reference and suppresses the
secondary cannot-derive error. An authored platform always wins.

`vsphere.vcenterCredentials` is the cluster's own vCenter credential, set keyed
by `server`: each `server` names a `vcenters[].server` of the nodes' provider,
each `credentialsRef` a `usernamePassword` Secret that is effect material, since
the installation places it in the cluster, and when present it names
every vCenter the nodes' failure domains use. It is never inferred from the
provider's provisioning `credentialsRef`; naming that same Secret is an
explicit choice. A cluster that declares it requires `topology.folder` on every
failure domain its nodes use and each such domain's `datacenter` among its
vCenter's `datacenters`. `nodeNetworking` is accepted and reaches no
agent-installer input.

A multi-node KubeVirt cluster may author `baremetal`; every slot's source is
then `installer`, and every node's attachments are `Localnet`
ClusterUserDefinedNetworks or `bridge` NetworkAttachmentDefinitions.

Not yet met: the vSphere arm admits no `vcenterCredentials`; tracked as
[B733](../milestones/m3.md#b733). KubeVirt attachment kinds are checked at
admission; proving the referenced external object is `Localnet` or `bridge`
requires the host-side reads of [B87](../milestones/m1.md#b87).

## Endpoints

`spec.install.endpoints` has only the `api`, `api-int`, and `ingress` slots.
`api` and `ingress` are authored. If `api-int` is omitted, normalization copies
only `api.address` and `api.source`; an authored `api-int` wins.

Every endpoint has this exact shape:

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `address` | string | for `installer` and `external` | — | IP literal owned by `installer` or `external`; absent for load-balancer and node sources, which resolve it. Never in `::/96`; an IPv4-mapped address is admitted. |
| `dnsName` | string | no | — | DNS subdomain naming the endpoint; inert for the agent installer apart from `api-int`'s named-certificate collision check, see [the inert fields](../openshift-clusters.md#selection-and-refusal). |
| `port` | integer | no | consumer default | `1..65535` when set. The agent installer uses its native endpoint ports, so an authored value other than the native port refuses at plan on the agent path, see [the inert fields](../openshift-clusters.md#selection-and-refusal). |
| `scheme` | string | no | consumer default | `http` or `https`. The agent installer uses its native protocols, so an authored value other than the native one refuses at plan on the agent path, see [the inert fields](../openshift-clusters.md#selection-and-refusal). |
| `prefixLength` | integer | no | — | Valid only with `address`; `1..32` for IPv4 or `1..128` for IPv6. No agent-installer input consumes it, so an authored value refuses at plan on the agent path. |
| `interfaceNetworks` | array of strings | no | `[]` | Valid CIDRs narrowing the interface that carries an owned address; effective state masks host bits. No agent-installer input consumes it, so an authored non-empty list refuses at plan on the agent path. |
| `source.type` | string | no | `installer` | `installer`, `external`, `loadBalancer`, or `node`; `installer` means the installer owns the address as its virtual IP. |
| `source.loadBalancerRef` | string | conditional | — | Global LoadBalancer; valid only for `loadBalancer`. |
| `source.bindAddressRef` | string | conditional | sole bind address | LoadBalancer-local `bindAddresses[].name`; valid only for `loadBalancer`. |

`installer` and `external` own an authored `address`, which every such slot
requires: [resolution before boot](../openshift-clusters.md#installation)
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
default `installer` source for all three slots; `source.type: node` is the
recommended form so one machine address is not repeated in three places, while
`external` with an `address` is also valid.

A multi-node cluster whose effective platform is `none`, authored or derived
for KubeVirt machines, also rejects the `installer` source for every slot, at
its `source.type`: the installer receives no VIPs on that platform, so nothing
in the cluster would answer the endpoint. An `external` or `loadBalancer`
source is valid there.

On a multi-node `baremetal` or `vsphere` platform the installer owns all three
endpoint slots as virtual IPs its own VIP holders answer. Each takes the
`installer` source with an authored `address`; an `external` or `loadBalancer`
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


**Endpoint addresses are the cluster's own.** An effective endpoint address
equals no effective endpoint address of another selected OpenShiftCluster, no
IP address a Machine declares other than the node a single-node cluster's
`node` source names, no host address of a managed libvirt network attachment,
no bind or endpoint address of a managed DNSServer, NTPServer, Proxy,
ArtifactServer or Registry, no emulated BMC's bind address, and no
LoadBalancer bind address other than the one its own `loadBalancer` source
selects; two clusters never select one LoadBalancer bind address. A wildcard
bind claims no address here. Within one cluster `api-int` may equal `api`, and
on platform `none` slots may share one external or load-balancer address. Each
refusal is an `api.invariant` at the slot's address, or at its source when the
address is resolved, naming the other holder. A managed libvirt network's
prefix belongs to one context through its exclusive reservation, so no other
context's managed attachment shares an address in it; an external attachment
claims no prefix, and on any other network Bootwright coordinates no address,
as for node addresses.


## Credentials and certificates

[Environment kind defaults](environment.md#kind-defaults) apply before the
following conventional fallbacks. An omitted OpenShift `pullSecretRef` may
receive `Environment.spec.defaults.OpenShiftCluster.install.pullSecretRef`;
if absent there too, normalization injects `openshift-pull-secret`. The
resulting name must still resolve to a declared `dockerConfigJson` `Secret`,
which is effect material.

`install.nodeSSH` is a presence union:

- `keyPairRef` names an `sshKeyPair` `Secret`, effect material whose public
  half the installation authorizes on every node, and is mutually exclusive with
  the split fields; or
- `publicKeyRef` names public SSH material and is required when the split form
  is authored; optional `privateKeyRef` names the matching private material.
  Both are effect material, as `keyPairRef` is.

When the whole block is omitted, it defaults from
`Environment.spec.defaults.OpenShiftCluster.install.nodeSSH`; if that is
absent, normalization injects
`keyPairRef: <cluster-name>-cluster-admin-ssh-key`. An authored block wins as a
whole; key-pair and split-key choices are never combined through defaults.
The reference must still resolve. Public-only material is sufficient to
declare installation but cannot authorize a cluster SSH command.

Media projection reads one authorized-key line from the `sshKeyPair` public
part or the opaque `publicKeyRef` value. A malformed value refuses before the
adapter runs and names the Secret, effect material, to correct.

`servingCertificates` admits only:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `apiServer.namedCertificates` | array | when `apiServer` is set | Non-empty entries of `{names, secretRef}`. |
| `apiServer.namedCertificates[].names` | array of strings | yes | Non-empty unique DNS names; the internal `api-int` name is forbidden. |
| `apiServer.namedCertificates[].secretRef` | string | yes | `tlsCertificate` `Secret`. Effect material: the cluster serves it. |
| `ingress.defaultCertificateRef` | string | when `ingress` is set | `tlsCertificate` `Secret`. Effect material: the cluster serves it. |

### Additional installation trust

`spec.install.additionalTrustBundleRefs` selects additional CA bundles for this
cluster's native install rendering. Each scalar reference resolves directly to
a `caBundle` Secret, effect material the installation adds to the cluster's
trust, and authored order is retained. The complete list is the
cluster's additional trust selection; there is no Environment trust list to
append to it.

The optional `spec.install.additionalTrustBundlePolicy` is `Always` or
`Proxyonly`. It has no default and is projected unchanged only when selected;
without it, the installer applies its own default. A selected policy without
additional bundle references refuses. `Always` also makes those bundles
available through the cluster-wide trusted CA, as TLS gateway consumers require.

Kind defaults may share the choice:

```yaml
spec:
  defaults:
    OpenShiftCluster:
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

A cluster whose [MetalLB pool](addons.md#package-metallb) names an interface
declares `gatewayConfig.routingViaHost: true` and `gatewayConfig.ipForwarding:
Global`: forwarding on that interface is the cluster's install-time
declaration, never an add-on's change.

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
| `uplink.bridge` | string | yes | `br-ex`, sharing the default bridge, or a new OVS bridge name: 1 to 15 bytes of letters, digits, `_`, `.`, `+` or `-`. |
| `uplink.interface` | string | with a new bridge | An `ethernet`, `bond` or `vlan` every node declares, other than its install interface: addressless, unless `uplink.hostAddresses` is `bridge`. |
| `uplink.hostAddresses` | string | no | `none` (default): the uplink carries no node address. `bridge`: the static addresses each node declares on the uplink move onto the bridge, below. Only with a new bridge; the same on every network sharing that bridge. |
| `vlan` | integer | no | Access VLAN `1..4094`; distinct among networks sharing an uplink. |
| `mtu` | integer | no | `576..65536`, at least `1280` with an IPv6 subnet, at most the uplink interface's declared MTU. |
| `ipam.mode` | string | no | `Disabled` (default) or `Enabled`. |
| `ipam.subnets` | array of CIDRs | with `Enabled` | One or two, of different families. |
| `ipam.excludeSubnets` | array of CIDRs | no | At most 25, inside `subnets`. |
| `ipam.lifecycle` | string | no | `Persistent`, with `Enabled` only. |
| `namespaceSelector.matchLabels` | map | yes | Non-empty Kubernetes labels selecting the namespaces that receive the attachment definition. |

A cluster that declares a secondary network needs a binding that
selects a package or add-on providing `nmstate`.

With `uplink.hostAddresses: bridge`, one VLAN carries both the node and the
network's workloads, the way the machine network carries both through `br-ex`.
Each node's Machine declares its address on the uplink, as on any other
interface; the installer configures it there, and after installation the
[cluster network block](../openshift-clusters.md#post-installation-network-configuration)
moves it onto the bridge's own interface, named after the bridge. From then on
the bridge, not the uplink, is the node's interface on that network, so a
[MetalLB pool](addons.md#package-metallb) announcing there names the bridge in
`l2.interface`, and an egress IP in its subnet leaves through it. Because the
move leaves the uplink addressless, admission refuses at `uplink.interface` an
uplink a node addresses by DHCP or autoconf, and one that a route of the node
names as its `next-hop-interface`; with `none`, an uplink a node addresses
refuses there too, naming `bridge` in its remedy.

Two uplinks refuse because their frames would not reach the network: a `vlan`
on `br-ex` where a node's install interface is itself a `vlan` interface,
because OVN-Kubernetes builds `br-ex` on that interface and the access tag
would ride inside its tag, refuses at `vlan`; and an `uplink.interface` that is
the `base-iface` of a `vlan` interface or a port of a bond the node declares,
its install interface's included, because the bridge would take that interface
away from the interface built on it, refuses at `uplink.interface`.

## Security

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.security.fips.enabled` | boolean | no | `false` | `true` is OpenShift-only. |
| `spec.security.diskEncryption` | object | no | — | Cluster-node TPM2 disk-encryption intent. |
| `spec.security.diskEncryption.unlock.tpm2` | empty object | with encryption | — | The only unlock arm; `pcrBank` and `pcrIds` are forbidden here. |
| `spec.security.diskEncryption.roles` | array of strings | no | all represented pools | Unique values from `controlPlane`, `worker`, and `infra`. |

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

A KubeVirt node's TPM is the VirtualMachine's vTPM, and a non-persistent one
is discarded whenever the host stops the instance, which a TPM2-sealed root
cannot survive. Admission therefore refuses `api.invariant` at the node's
`machineRef` when the node's role is encrypted and its KubeVirt profile
declares no `tpm`, or sets `tpm.persistent: false`
([KubeVirt machine realization](../substrates.md#kubevirt-machine-realization)).

On the agent path the roles resolve to the installer's `enableOn` value: every
represented role is `all`, `controlPlane` alone is `masters`, and `worker`, `infra` or
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
| `spec.nodes[].role` | string | yes | `controlPlane`, `worker`, or `infra`. A `controlPlane` node is rendered as the installer's native `master`. |
| `spec.nodes[].machineRef` | string | yes | Global `Machine` reference. |
| `spec.nodes[].labels` | map of string to string | no | Kubernetes label keys and values the node carries once its installation completes. |
| `spec.nodes[].taints` | array | no | Entries have required `key` and `effect`, optional `value`; effects are `NoSchedule`, `PreferNoSchedule`, or `NoExecute`. The node carries them once its installation completes. |

Normalization composes the node FQDN `<node>.<cluster>.<domain>` under
[Environment domains](environment.md#domains); an authored `fqdn` wins verbatim.
A derived node FQDN, and the derived endpoint names `api-int.<cluster>.<domain>`
and `*.apps.<cluster>.<domain>`, must fit a DNS name (253 bytes, 63 per label);
otherwise validate refuses with `api.invariant` at `spec.nodes[].name` or at the
cluster's `metadata.name`, naming the composed name.

An `infra` node installs through the native worker pool and, once the
installation completes, also carries `node-role.kubernetes.io/infra` with an
empty value unless its `labels` author that key; no taint is implied by the
role ([node placement](../openshift-clusters.md#node-placement)).
Desired-state compilation accepts and preserves repeated authored taints. A
native projection deduplicates by `key` plus `effect`; explicitly authoring the
standard infra taint is therefore a no-op rather than an error.

Every `machineRef` requires `os.provided: false`; naming it makes the Machine an OpenShift node, with no role declared on the Machine ([substrate arm](machines.md#substrate-arm)).
The cluster domain publishes these bindings as the cluster's membership
(`containercluster.MemberMachines`), which Environment selection, node binding
uniqueness and machine targeting read without naming this kind.
Network and root-device input comes only from that Machine. The common
[graph invariants](../api.md#graph-validation) enforce unique node binding.

### Topology

At least one node is a `controlPlane` node, and the number of `controlPlane` nodes is one the
rule below accepts for the declared release. Agent installers refuse other
control-plane counts. Two install upstream only beside an arbiter pool or
fencing credentials, and no node role is an arbiter or carries fencing, so no
rule accepts two. Whatever the release, a cluster with one `controlPlane` node declares
no `worker` or `infra` node, because the agent installer refuses compute
replicas beside a single control-plane replica. Each of these refusals is an
`api.invariant` at `spec.nodes`.

The general rule applies to every release, and the installer judges the release
itself (D162). Where an installer first accepts a different roster, the
difference is a version specialization whose condition reads the frozen
installer artifact, never `release.version`
([implementations and version variation](../architecture.md#implementations-and-version-variation)),
so the refusal it changes fires at plan, not at admission.

| Rule | Master counts | Read from `openshift/installer` |
| --- | --- | --- |
| general | 1, 3, 4, 5 | 4.21 [agent control plane](https://github.com/openshift/installer/blob/release-4.21/pkg/asset/agent/installconfig.go#L217-L232), [agent single node](https://github.com/openshift/installer/blob/release-4.21/pkg/asset/agent/installconfig.go#L234-L263), [install-config control plane](https://github.com/openshift/installer/blob/release-4.21/pkg/types/validation/installconfig.go#L758-L771); 4.22 [agent control plane](https://github.com/openshift/installer/blob/release-4.22/pkg/asset/agent/installconfig.go#L220-L305), [agent single node](https://github.com/openshift/installer/blob/release-4.22/pkg/asset/agent/installconfig.go#L307-L336), [install-config two nodes](https://github.com/openshift/installer/blob/release-4.22/pkg/types/validation/installconfig.go#L1731-L1753); 4.23 [agent control plane](https://github.com/openshift/installer/blob/release-4.23/pkg/asset/agent/installconfig.go#L220-L305), [agent single node](https://github.com/openshift/installer/blob/release-4.23/pkg/asset/agent/installconfig.go#L307-L336), [install-config two nodes](https://github.com/openshift/installer/blob/release-4.23/pkg/types/validation/installconfig.go#L1803-L1825) |

There is no specialization. The rule is `generalTopology` in
`internal/containercluster/openshift/topology.go`; a specialization, when an installer
first differs, is a record with its own row in this table and a row of the
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
