---
title: Container clusters
description: ContainerCluster install intent — distribution, platform render mode, endpoints, networking, machine pools, and host bindings.
---

# Container clusters

A `ContainerCluster` owns OpenShift or OKD **install intent** and nothing else.
It declares the distribution and release, the install method and mode, the
installer platform render mode, cluster endpoints, artifact access, cluster
networking, machine-pool replica counts, and the node-to-machine bindings the
agent install consumes. It selects machines through `spec.hosts[].machineRef`;
substrate ownership stays on [`Machine`](machines.md) and
[`InfraProvider`](infrastructure.md).

What a `ContainerCluster` deliberately does **not** own:

- **Node facts.** Hardware, OS mode, install network, and SSH access live on the
  bound [`Machine`](machines.md), not here.
- **Post-install add-ons.** Bootstrap components are not authored under
  `spec.install` — they are separate [add-on](add-ons.md) kinds the
  [`Environment`](environment.md) selects and binds after install.
- **External storage.** Ceph is a peer [`StorageCluster`](storage.md), never a
  `ContainerCluster` field.

The install scope is direct `openshift-install agent` runs against single-node
and multi-node machines. See [conventions](index.md) for the object envelope and
the Required/Default field-table convention every table below follows.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: sno-libvirt
spec:
  distribution:
    release:
      version: 4.21.15
  install:
    nodeSSH:
      keyPairRef: sno-libvirt-cluster-admin-ssh-key
    endpoints:
      api:
        address: 192.168.132.20
        source:
          type: external
      ingress:
        address: 192.168.132.20
        source:
          type: external
  hosts:
    - hostname: master-0
      role: master
      machineRef: sno-libvirt-master-0
```

## Fields

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.distribution` | No | `type: openshift` | OpenShift or OKD release selection. Optional in shape only: an omitted block defaults `type: openshift`, which then requires `release.version` or `release.image`, so an absent or empty `distribution` fails validation. See [Distribution](#distribution). |
| `spec.install` | No | — | Install method, mode, platform, endpoints, artifact access, trust, serving certificates, and node SSH. |
| `spec.security` | No | — | Cluster security posture. Today: FIPS mode. See [Security](#security). |
| `spec.controlPlane.replicas` | No | — | Control-plane replica count; when set, must equal the master host count. |
| `spec.compute[].replicas` | No | — | Worker replica count per pool; their sum must equal the worker+infra host count when any compute pool is declared (infra hosts install as workers). |
| `spec.networking` | No | Defaulted networks (see [Networking](#networking)) | Cluster and service networks and the OpenShift network type. |
| `spec.hosts[]` | Yes | — | Node-to-machine bindings for the agent install. |

## Distribution

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `distribution.type` | No | `openshift` | `openshift` or `okd`. |
| `distribution.release.version` | One of version/image | — | Release version (the OpenShift release path). |
| `distribution.release.channel` | No | — | Optional release channel for the OpenShift release path. |
| `distribution.release.image` | One of version/image | — | Explicit release image (the OKD / pinned-install path). |

!!! note "Cross-field rule: one of version/image is required"
    A release is pinned by `version` or `image`, and **at least one of the two
    must be set**: an `openshift` distribution requires `version` unless `image`
    is given, and an `okd` distribution requires `image` or `version`. `image`
    is the explicit-image path and the usual way to pin a release or install
    OKD. Because the whole `spec.distribution` block still defaults to
    `type: openshift`, omitting it fails validation for the missing
    `version`/`image` — the block is optional in shape only.

`channel` never selects a release on its own — it is recorded metadata layered on
top of a `version`. Authoring only `channel`, with no `version` and no `image`,
is **rejected by validation**, not merely ineffective: an `openshift` cluster
still fails for the missing `version`/`image`, and an `okd` cluster rejects
`channel` outright (`channel` is supported only for `openshift`).

!!! warning "Release fields are install-time intent, not a day-2 upgrade"
    `distribution.release.*` selects what bootwright installs. Editing it on an
    already-installed cluster is classified as drift, and its only in-band
    resolution is a reinstall — `apply --converge-drifted` reinstalls the cluster, it does
    not upgrade it. In-place cluster upgrades are a non-goal today: upgrade out of
    band with `oc adm upgrade`, then be aware that the desired state still names
    the old version, so `diff` reports the cluster as drifted until a
    future apply refreshes the record. Adopting an out-of-band upgrade into the
    recorded desired state is an open design item.

!!! note "Pull secret is required for OpenShift"
    An `openshift` distribution requires a pull secret. The normalize phase
    fills `install.pullSecretRef` from the `Environment` default, falling back
    to the `openshift-pull-secret` convention name, so you rarely author it. An
    `okd` distribution may omit a Red Hat pull secret unless a private release
    or mirror requires credentials. See [Secrets](secrets.md).

## Install

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `install.method` | No | `agent` | Install method. Only `agent` is accepted today. |
| `install.mode` | No | `connected` | `connected` or `disconnected`. |
| `install.platform` | No | Derived (see [Platform](#platform)) | Installer platform render mode. |
| `install.endpoints` | No | — | Closed map keyed by `api`, `api-int`, and `ingress`; see [Endpoints](#endpoints). |
| `install.agent.redfishVirtualMedia.artifactServerEndpoint` | Required for bare-metal nodes | `serverRef` may use `Environment.spec.defaults.artifactServerRef` | Artifact server endpoint the Redfish BMC fetches the agent ISO from. |
| `install.agent.bootArtifacts.artifactServerEndpoint` | Required for disconnected installs | `serverRef` may use `Environment.spec.defaults.artifactServerRef` | Artifact server endpoint that publishes disconnected agent boot artifacts. |
| `install.pullSecretRef` | Required for OpenShift | Environment default, else `openshift-pull-secret` | Pull secret name. |
| `install.nodeSSH` | No | Generated `<cluster-name>-cluster-admin-ssh-key` | Node SSH material; see [Node SSH](#node-ssh). |
| `install.additionalTrustBundleRefs[]` | No | — | Cluster-scoped install CA bundle secret names. |
| `install.servingCertificates` | No | — | API and ingress serving certificates; see [Serving certificates](#serving-certificates). |

!!! note "Disconnected installs need a mirror"
    `install.mode: disconnected` requires `Environment.spec.registries.mirror`
    with a `trustBundleRef` and either an external mirror `url` or a managed
    registry `InfraComponent`. It also requires
    `install.agent.bootArtifacts.artifactServerEndpoint.endpointRef` to resolve
    on a managed artifact server. See
    [Disconnected and proxied installs](../advanced/disconnected-proxy.md).

### Node SSH

Node SSH material is the cluster install/admin key pair the agent install uses.
Author either a combined key pair or split public/private references — not both.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `install.nodeSSH.keyPairRef` | One of `keyPairRef`/`publicKeyRef` | Generated convention key | Secret holding both private and public material. |
| `install.nodeSSH.publicKeyRef` | Required when `keyPairRef` is empty | — | Secret holding public key material. |
| `install.nodeSSH.privateKeyRef` | No | — | Secret holding private key material for local probes and `cluster rsh`/`exec`. |

!!! note "Key pair or split refs, not both"
    Setting `keyPairRef` together with `publicKeyRef` or `privateKeyRef` is
    rejected. When `keyPairRef` is empty, `publicKeyRef` is required. When the
    whole block is omitted, normalize injects the generated
    `<cluster-name>-cluster-admin-ssh-key` convention name. See
    [Secrets](secrets.md#node-ssh-keys) for how the secret name is keyed.

Container-node `cluster rsh` and `cluster exec` use this private key, the
`core` user, and the node's effective primary install IP. They do not require
the backing Machine to duplicate the cluster key under `spec.access.ssh`.
The first interactive connection asks before recording an unknown server key
in the context trust file. After verifying a changed node key out of band,
remove only that address's stale pin and reconnect interactively to confirm it:

```console
$ sudo ssh-keygen -R <effective-node-address> \
    -f /var/lib/bootwright/contexts/<context>/trust/ssh/known_hosts
$ bootwright cluster rsh --name <cluster> --node <node>
```

### Serving certificates

`install.servingCertificates` overrides the default cluster serving
certificates. Both arms are optional, but each arm validates its contents once
present. For the end-to-end corporate-PKI walkthrough — serving certificates
together with `additionalTrustBundleRefs` — see
[Corporate TLS](../advanced/corporate-certificates.md).

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `servingCertificates.apiServer.namedCertificates[]` | At least one when `apiServer` is present | — | API server named certificates. |
| `servingCertificates.apiServer.namedCertificates[].names[]` | At least one per entry | — | DNS SAN names this certificate serves; must not target the internal `api-int` endpoint. |
| `servingCertificates.apiServer.namedCertificates[].secretRef` | Yes (per entry) | — | Secret holding the certificate and key. |
| `servingCertificates.ingress.defaultCertificateRef` | Required when `ingress` is present | — | Secret holding the ingress default certificate. |

!!! note "Present-then-required leaves"
    `apiServer` and `ingress` are independently optional, but if you author
    `apiServer` you must supply at least one `namedCertificates` entry, and each
    entry needs both a `secretRef` and at least one `names` value. If you author
    `ingress` you must supply `defaultCertificateRef`. Named-certificate `names`
    must not name the internal `api-int.<cluster>.<baseDomain>` endpoint.

## Security

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `security.fips.enabled` | No | `false` | Install and run the cluster in FIPS mode. |

`security.fips.enabled: true` renders `fips: true` into the generated
`install-config.yaml`, so the agent installer lays down RHCOS in FIPS mode
across every control-plane and compute node. `false` and unset are equivalent —
only `enabled: true` renders FIPS.

Unlike the [managed Ceph FIPS gate](../advanced/ceph-topologies.md#fips), OCP
needs no matching flag on each node's OS: an OpenShift node's OS is RHCOS,
installed by this same `install-config.yaml`, so the one field is the whole
mechanism. There is no separate `MachineInstallProfile` to keep consistent.

!!! warning "FIPS requires OpenShift, not OKD"
    `security.fips.enabled` is rejected for `distribution.type: okd`: OKD ships
    community SCOS, which is not FIPS-validated. This mirrors the Ceph gate,
    which rejects the community `oss` distribution. FIPS validation is a Red Hat
    OpenShift feature.

!!! note "FIPS is install-time intent, not a day-2 toggle"
    Like `distribution.release.*`, `security.fips.enabled` selects what
    bootwright installs. Flipping it on an already-installed cluster is drift
    whose only in-band resolution is a reinstall — OpenShift does not support
    turning FIPS on or off after install.

## Platform

`install.platform.type` is the installer **platform render mode**, not the
substrate type — substrate ownership stays with the selected machines and their
providers. The platform mode feeds the generated `install-config.yaml`.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `install.platform.type` | No | Derived from the bound machines' provider | `baremetal`, `vsphere`, `none`, or `external`. |
| `install.platform.baremetal.provisioningNetwork` | No | — | `disabled`, `managed`, or `unmanaged`. |
| `install.platform.vsphere.nodeNetworking` | No | — | vSphere node networking subnets; see below. |
| `install.platform.external` | No | — | Free-form passthrough map for the `external` platform. |

Mapping the common topologies to a platform mode:

| Topology | Platform mode |
| --- | --- |
| Redfish virtual media on real bare metal | `baremetal` |
| Libvirt VM with emulated Redfish | `baremetal` |
| vSphere agent install | `vsphere` |
| KubeVirt-hosted child machines | `none` |
| Operator-owned external platform | `external` |

### vSphere node networking

`vsphere.nodeNetworking` maps 1:1 to the upstream install-config vSphere
`nodeNetworking` keys and renders unchanged. The lowercased `networkSubnetCidr`
spelling is the verbatim upstream key.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `vsphere.nodeNetworking.external.networkSubnetCidr[]` | No | — | External node subnet CIDRs. |
| `vsphere.nodeNetworking.internal.networkSubnetCidr[]` | No | — | Internal node subnet CIDRs. |

!!! note "Platform derivation and single-node clusters"
    When `platform` is omitted, the mode is derived from the single provider
    type behind the bound machines: libvirt and bare metal derive `baremetal`
    with `provisioningNetwork: disabled`, vSphere derives `vsphere`, and
    KubeVirt derives `none`. Machines spanning more than one provider type with
    `platform` omitted are rejected; an authored platform always wins.
    Single-node clusters render `platform.none` unless `platform.type: external`
    is explicit, because `openshift-install agent` rejects bare-metal and
    vSphere platform blocks for one control-plane node with zero compute.

## Endpoints

`install.endpoints` is a closed map; only the `api`, `api-int`, and `ingress`
keys are accepted. Omitting an endpoint's `source.type` normalizes it to
`openshift`. Each slot draws its address from one of three sources:

| Source | Meaning |
| --- | --- |
| `openshift` | The installer or cluster owns the endpoint; an explicit `address` is required. |
| `external` | Operator-owned load balancer or DNS; an explicit `address` is required. |
| `infraComponent` | Bootwright-managed load balancer selected by `componentRef`; the address resolves from the component's `bindAddressRef`. |

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `endpoints.<slot>.address` | Required for `openshift`/`external` sources | — | Literal endpoint address. |
| `endpoints.<slot>.dnsName` | No | — | Optional DNS name. |
| `endpoints.<slot>.port` | No | — | Optional port. |
| `endpoints.<slot>.scheme` | No | — | Optional scheme. |
| `endpoints.<slot>.prefixLength` | No | — | Prefix length for VIP-style endpoints. |
| `endpoints.<slot>.interfaceNetworks[]` | No | — | Interface networks for VIP placement. |
| `endpoints.<slot>.source.type` | No | `openshift` | `openshift`, `external`, or `infraComponent`. |
| `endpoints.<slot>.source.componentRef` | Required for `infraComponent` | — | Load-balancer `InfraComponent` name. |
| `endpoints.<slot>.source.bindAddressRef` | No | — | Names a `bindAddresses[]` entry on the referenced load balancer. |

!!! note "Single-node clusters reject `openshift` sources"
    A single-node cluster cannot use `source.type: openshift` on the `api`,
    `api-int`, or `ingress` slot — pair it with the `platform.none` default
    above. Use `external` or `infraComponent` instead. For how VIPs and managed
    load balancers wire together, see
    [Networking](../advanced/networking.md).

## Artifact Server Endpoints

Agent install consumers own the artifact server endpoint they use. The reusable
selector shape is:

```yaml
artifactServerEndpoint:
  serverRef: default   # optional when Environment.spec.defaults.artifactServerRef is set
  endpointRef: bmc
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `install.agent.redfishVirtualMedia.artifactServerEndpoint.endpointRef` | Required for bare-metal nodes | — | Endpoint the Redfish BMC fetches the agent ISO from. |
| `install.agent.bootArtifacts.artifactServerEndpoint.endpointRef` | Required for disconnected installs | — | Endpoint that publishes agent boot artifacts and becomes `bootArtifactsBaseURL`. |
| `artifactServerEndpoint.serverRef` | No | `Environment.spec.defaults.artifactServerRef` | Names an `Environment.spec.infraComponents.artifactServers[].name`. |

`endpointRef` is never defaulted globally. A new consumer adds its own
`artifactServerEndpoint` field instead of adding a slot to `InfraComponent` or
`Environment`.

## Networking

`spec.networking` carries the OpenShift cluster and service networks plus the
network type. The whole block and any field within it may be omitted.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `networking.networkType` | No | — | OpenShift network type; renders the installer default when omitted. |
| `networking.clusterNetwork[].cidr` | Required per entry | `10.128.0.0/14` (default entry) | Pod network CIDR. |
| `networking.clusterNetwork[].hostPrefix` | No | `23` (default entry) | Pod network host prefix. |
| `networking.serviceNetwork[]` | No | `172.30.0.0/16` | Service network CIDRs. |

!!! note "Both networks are defaulted when omitted"
    When `clusterNetwork` is omitted, normalize injects a single entry of
    `10.128.0.0/14` with `hostPrefix: 23`. When `serviceNetwork` is omitted, it
    injects `172.30.0.0/16`. `networkType` has no Bootwright default — leaving it
    unset renders the installer's own default. Run `render effective` to see the
    injected networks. The cluster and service networks are the only owned
    installer networking fields here.

## Machine pools

`spec.controlPlane` and `spec.compute[]` carry only replica counts. The agent
installer renders a single default-architecture master pool and worker pool, so
the other install-config machine-pool fields (`architecture`, `hyperthreading`,
`platform`, `name`) are not authorable — strict decode rejects them with the
offending line.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `controlPlane.replicas` | No | — | Control-plane replica count. |
| `compute[].replicas` | No | — | Worker replica count for this pool. |

!!! note "Replica counts cross-check the host roles"
    `controlPlane.replicas`, when set (non-zero), must equal the number of
    `master` hosts in `spec.hosts[]`; omitting it or setting `0` skips the
    check. The sum of `compute[].replicas` must equal the number of `worker`
    plus `infra` hosts when any compute pool is declared (infra hosts install in
    the worker pool). These fields restate the host roster rather than scaling
    it independently.

## Hosts

`spec.hosts[]` binds each cluster node to a backing `Machine`. At least one
`master` host is required.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `hosts[].hostname` | No | `node<NN>` (`node01`, `node02`, … in `hosts` list order) | Node hostname inside the cluster, independent of the machine name; unique within the cluster (by short label). A bare label composes to `<label>.<cluster>.<baseDomain>` — under the planned domain model ([ADR 0018](https://github.com/crmarques/bootwright/blob/main/specs/adr/0018-environment-domain-model.md)) the container-cluster zone `domains.containerClusters`; a dotted value is an explicit FQDN used verbatim. Also the node name the day-2 node-config step targets when applying labels/taints. |
| `hosts[].role` | Yes | — | `master`, `worker`, or `infra`. |
| `hosts[].machineRef` | Yes | — | `Machine` that backs this node; no default is derived. |
| `hosts[].labels` | No | — | Extra node labels Bootwright applies day-2. |
| `hosts[].taints` | No | — | Extra node taints (`key`, optional `value`, `effect` ∈ {`NoSchedule`, `PreferNoSchedule`, `NoExecute`}) applied day-2. |

!!! note "Infra nodes are an authoring role"
    OpenShift has no install-time `infra` pool, so an `infra` host installs as a
    `worker` (it counts toward `compute[].replicas`) and Bootwright promotes it
    day-2 against the running cluster: it adds the
    `node-role.kubernetes.io/infra` label, a `node-role.kubernetes.io/infra:NoSchedule`
    taint, and the `infra` `MachineConfigPool`, plus any `hosts[].labels`/`taints`
    you author. Moving ingress, monitoring and other operands onto infra (the
    matching tolerations/nodeSelectors) is left to you. Authored `labels`/`taints`
    on a plain `master`/`worker` host are applied day-2 too, without the infra
    label/MCP.

!!! note "Where host binding rules are enforced"
    A referenced `Machine` must carry the `openshift-node` capability and may be
    node-bound by at most one cluster — across every `ContainerCluster` and
    `StorageCluster` — and at most one host entry. Those rules are enforced
    here. A node `Machine` is installed by Bootwright, so it is declared with
    `os.provided: false` on the `Machine` itself; that constraint is enforced by
    [Machine](machines.md) validation, not by `ContainerCluster`.

## Where to go next

- [Networking](../advanced/networking.md) — endpoints, VIPs, and managed load
  balancers in depth.
- [KubeVirt child clusters](../advanced/kubevirt.md) — nesting a
  `ContainerCluster` on a KubeVirt-backed parent.
- [Add-ons](add-ons.md) — post-install bootstrap components bound after install.
- [Conventions](index.md) — the object envelope, unions, references, and
  defaults that govern every kind.
