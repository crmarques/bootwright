# Container clusters

`ContainerCluster` owns OpenShift/OKD install intent: references, endpoint and
artifact selections, security and network defaults. Native installer files are
derived outputs. [The compiler boundary](../api.md#compiler-boundary) applies;
`platform.external` remains inert until validated by a release-specific consumer.

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

## Install selection

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.install.method` | string | no | `agent` | `agent` is the only accepted method. |
| `spec.install.mode` | string | no | `connected` | `connected` or `disconnected`. |
| `spec.install.platform` | object | no | derived | Closed union described below. |
| `spec.install.endpoints` | map | yes | — | Closed keys `api`, `api-int`, and `ingress`; endpoint shape below. |
| `spec.install.agent.redfishVirtualMedia.artifactServerEndpoint` | object | conditional | — | Required when any bound machine uses bare metal. |
| `spec.install.agent.bootArtifacts.artifactServerEndpoint` | object | conditional | — | Required in disconnected mode. |
| `spec.install.pullSecretRef` | string | OpenShift | environment/convention | `dockerConfigJson` `Secret`; not required for OKD. |
| `spec.install.nodeSSH` | object | normalized | environment/convention | Cluster administration public/private SSH material. |
| `spec.install.additionalTrustBundleRefs` | array of strings | no | `[]` | Unique `caBundle` `Secret` references. |
| `spec.install.servingCertificates` | object | no | — | Typed API and ingress serving-certificate refs below. |

Disconnected mode requires `Environment.spec.registries.mirror` and a managed
agent boot-artifacts endpoint. Connected mode obtains boot artifacts from the
release payload and does not use an authored boot-artifacts selection.

Both consumers use the shared
[artifact endpoint selection](environment.md#artifact-server-catalog), and
require a managed artifact server.

The Redfish endpoint serves virtual media for bare-metal nodes; the boot
artifacts endpoint serves rootfs, kernel, and initramfs content. They are
separate selections even when one component provides both.

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
| `address` | string | source- and topology-dependent | — | Optional IP literal owned by `openshift` or `external`; absent for managed component and node sources and required when that direct source must supply a VIP. |
| `dnsName` | string | no | — | DNS subdomain naming the endpoint. |
| `port` | integer | no | consumer default | `1..65535` when set. |
| `scheme` | string | no | consumer default | `http` or `https`. |
| `prefixLength` | integer | no | — | Valid only with `address`; `1..32` for IPv4 or `1..128` for IPv6. |
| `interfaceNetworks` | array of strings | no | `[]` | Valid CIDRs narrowing the interface that carries an owned address; effective state masks host bits. |
| `source.type` | string | no | `openshift` | `openshift`, `external`, `infraComponent`, or `node`. |
| `source.componentRef` | string | conditional | — | Global `InfraComponent` whose type is `loadBalancer`; valid only for `infraComponent`. |
| `source.bindAddressRef` | string | conditional | sole bind address | Component-local `bindAddresses[].name`; valid only for `infraComponent`. |

`openshift` and `external` may own an authored `address`; otherwise `dnsName`
can satisfy a non-VIP slot. `infraComponent` forbids an authored address: the
component and optional bind-address ref resolve it. `bindAddressRef` may be
omitted only when the selected load balancer has one bind address.

`node` is valid only for a one-node cluster and also forbids an authored
address. Effective normalization resolves the unique install address selected
by that node machine's `network.config.interfaceAddresses[]` and materializes
it. Zero or multiple candidates are errors. Single-node clusters reject the
default `openshift` source for all three slots; `source.type: node` is the
recommended form so one machine address is not repeated in three places, while
`external` with a sufficient `dnsName` is also valid.

On a multi-node `baremetal` or `vsphere` platform, all three endpoint slots are
VIP-bearing. Each therefore resolves an address directly or through an
`infraComponent`; `dnsName` alone is insufficient. API, internal API, ingress,
and node IPs obey the selected machine-network CIDRs. VIPs do not collide with
node install IPs, and endpoint/network address families are consistent.

## Credentials and certificates

For OpenShift, `pullSecretRef` first defaults from
`Environment.spec.defaults.install.pullSecretRef`; if absent there too,
normalization injects `openshift-pull-secret`. The resulting name must still
resolve to a declared `dockerConfigJson` `Secret`.

`install.nodeSSH` is a presence union:

- `keyPairRef` names an `sshKeyPair` `Secret` and is mutually exclusive with
  the split fields; or
- `publicKeyRef` names public SSH material and is required when the split form
  is authored; optional `privateKeyRef` names the matching private material.

When the whole block is omitted, it defaults from
`Environment.spec.defaults.install.nodeSSH`; if that is absent, normalization
injects `keyPairRef: <cluster-name>-cluster-admin-ssh-key`. The reference must
still resolve. Public-only material is sufficient to declare installation but
cannot authorize a cluster SSH command.

`servingCertificates` admits only:

| Field | Type | Required | Rule |
| --- | --- | --- | --- |
| `apiServer.namedCertificates` | array | when `apiServer` is set | Non-empty entries of `{names, secretRef}`. |
| `apiServer.namedCertificates[].names` | array of strings | yes | Non-empty unique DNS names; the internal `api-int` name is forbidden. |
| `apiServer.namedCertificates[].secretRef` | string | yes | `tlsCertificate` `Secret`. |
| `ingress.defaultCertificateRef` | string | when `ingress` is set | `tlsCertificate` `Secret`. |

A native consumer combines additional trust-bundle refs with
`Environment.spec.trustedCAs.caBundleRefs`.

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
The consumed `NetworkConfig.machineNetwork` values, cluster networks, service
networks, node install addresses, and endpoint addresses form one supported
address family; dual-stack or mixed-family input is rejected by this contract.

## Security

| Field | Type | Required | Default | Rule |
| --- | --- | --- | --- | --- |
| `spec.security.fips.enabled` | boolean | no | `false` | `true` is OpenShift-only. |
| `spec.security.diskEncryption` | object | no | — | Cluster-node TPM2 disk-encryption intent. |
| `spec.security.diskEncryption.unlock.tpm2` | empty object | with encryption | — | The only unlock arm; `pcrBank` and `pcrIds` are forbidden here. |
| `spec.security.diskEncryption.roles` | array of strings | no | all represented pools | Unique values from `master`, `worker`, and `infra`. |

An authored role selection resolves to at least one declared node. `infra`
folds into the worker machine-config pool. TPM2 inventory and install-time
projection are fail-closed effect-time checks; the desired-state declaration
never triggers a hardware read or disk operation. General destructive and
secret boundaries are defined in [security.md](../security.md) and
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

At least one node is a `master`. An `infra` node installs through the native
worker pool but retains its authoring role for post-install placement intent.
Desired-state compilation accepts and preserves repeated authored taints. A
native projection deduplicates by `key` plus `effect`; explicitly authoring the
standard infra taint is therefore a no-op rather than an error.

Every `machineRef` requires capability `openshift-node` and `os.provided: false`.
Network and root-device input comes only from that Machine. The common
[graph invariants](../api.md#graph-validation) enforce unique node binding.

## Cross-object invariants

- Every bound machine's provider, profile, network selection, attachment, NIC
  binding, root-device hints, BMC facts, and install IP satisfy the machine and
  provider rules in [machines.md](machines.md).
- A cluster's consumed machine networks and effective endpoint sources are
  resolved from the full object graph, never by file order or same-name
  guessing.
