---
title: Container clusters
description: ContainerCluster install intent — distribution, platform render mode, endpoints, networking, machine pools, and node bindings.
---

# Container clusters

A `ContainerCluster` owns OpenShift or OKD **install intent** and nothing else.
It declares the distribution and release, the install method and mode, the
installer platform render mode, cluster endpoints, artifact access, cluster
networking, machine-pool replica counts, and the node-to-machine bindings the
agent install consumes. It selects machines through `spec.nodes[].machineRef`;
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
  nodes:
    - name: master-0
      role: master
      machineRef: sno-libvirt-master-0
```

## Fields

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.distribution` | No | `type: openshift` | OpenShift or OKD release selection. Optional in shape only: an omitted block defaults `type: openshift`, which then requires `release.version` or `release.image`, so an absent or empty `distribution` fails validation. See [Distribution](#distribution). |
| `spec.install` | No | — | Install method, mode, platform, endpoints, artifact access, trust, serving certificates, and node SSH. |
| `spec.security` | No | — | Cluster security posture. Today: FIPS mode. See [Security](#security). |
| `spec.networking` | No | Defaulted networks (see [Networking](#networking)) | Cluster and service networks and the OpenShift network type. |
| `spec.nodes[]` | Yes | — | Node-to-machine bindings for the agent install. |

## Distribution

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `distribution.type` | No | `openshift` | `openshift` or `okd`. |
| `distribution.release.version` | No | — | Release version (the OpenShift release path). |
| `distribution.release.channel` | No | — | Optional release channel for the OpenShift release path. |
| `distribution.release.image` | No | — | Explicit release image (the OKD / pinned-install path). |

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
    resolution is a reinstall — `apply --mode rebuild` reinstalls the cluster, it does
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
| `install.agent.redfishVirtualMedia.artifactServerEndpoint` | No | `serverRef` may inherit the default `artifactServers[]` entry | Artifact server endpoint the Redfish BMC fetches the agent ISO from. |
| `install.agent.bootArtifacts.artifactServerEndpoint` | No | `serverRef` may inherit the default `artifactServers[]` entry | Artifact server endpoint that publishes disconnected agent boot artifacts. |
| `install.pullSecretRef` | No | Environment default, else `openshift-pull-secret` | Pull secret name. |
| `install.nodeSSH` | No | Generated `<cluster-name>-cluster-admin-ssh-key` | Node SSH material; see [Node SSH](#node-ssh). |
| `install.additionalTrustBundleRefs[]` | No | — | Cluster-scoped install CA bundle secret names. |
| `install.servingCertificates` | No | — | API and ingress serving certificates; see [Serving certificates](#serving-certificates). |

!!! note "Conditionally required install fields"
    `install.agent.redfishVirtualMedia.artifactServerEndpoint` is required when
    any node is bare-metal (the BMC fetches the agent ISO from it);
    `install.agent.bootArtifacts.artifactServerEndpoint` is required for
    `install.mode: disconnected`; `install.pullSecretRef` is required for an
    `openshift` distribution and is normally filled from the `Environment`
    default.

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
| `install.nodeSSH.keyPairRef` | No | Generated convention key | Secret holding both private and public material. |
| `install.nodeSSH.publicKeyRef` | No | — | Secret holding public key material. |
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
| `servingCertificates.apiServer.namedCertificates[]` | No | — | API server named certificates. |
| `servingCertificates.apiServer.namedCertificates[].names[]` | Yes (per entry) | — | DNS SAN names this certificate serves; must not target the internal `api-int` endpoint. |
| `servingCertificates.apiServer.namedCertificates[].secretRef` | Yes (per entry) | — | Secret holding the certificate and key. |
| `servingCertificates.ingress.defaultCertificateRef` | No | — | Secret holding the ingress default certificate. |

!!! note "Present-then-required leaves"
    `apiServer` and `ingress` are independently optional, but if you author
    `apiServer` you must supply at least one `namedCertificates` entry, and each
    entry needs both a `secretRef` and at least one `names` value. If you author
    `ingress` you must supply `defaultCertificateRef`. Named-certificate `names`
    must not name the internal
    `api-int.<cluster>.<domains.containerClusters>` endpoint.

## Security

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `security.fips.enabled` | No | `false` | Install and run the cluster in FIPS mode. |
| `security.diskEncryption` | No | — (unencrypted) | Presence installs the nodes onto a LUKS2 root volume sealed to each node's TPM 2.0. See [Disk encryption](#disk-encryption). |
| `security.diskEncryption.unlock.tpm2` | No | — | The only unlock arm. Takes no `pcrIds`/`pcrBank` — see below. |
| `security.diskEncryption.roles[]` | No | Every role in `nodes[]` | Which node roles to encrypt: `master`, `worker`, `infra`. `infra` folds into the `worker` machine config pool. |

When `security.diskEncryption` is present, `unlock.tpm2` is required — it is
the only unlock arm.

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

### Disk encryption

```yaml
spec:
  security:
    fips:
      enabled: true
    diskEncryption:
      unlock:
        tpm2: {}
```

Bootwright writes one `MachineConfig` per selected machine config pool into the
installer's `openshift/` extra-manifest directory —
`99-bootwright-master-disk-encryption` and `99-bootwright-worker-disk-encryption`
— each declaring an Ignition `storage.luks` entry on
`/dev/disk/by-partlabel/root` with `clevis.tpm2`. The bootstrap Machine Config
Operator folds them into the first rendered config, so Ignition re-provisions the
root filesystem onto the LUKS volume during each node's first boot. Nothing is
written to `install-config.yaml`; there is no disk-encryption field there.

No cipher option is rendered. OpenShift 4.18 dropped the
`aes-cbc-essiv:sha256` override that older FIPS guidance called for, and
`cryptsetup`'s default `aes-xts-plain64` is what current releases expect with or
without `security.fips.enabled`.

!!! warning "A node without a TPM installs, then strands itself"
    Nothing preflights the TPM on this path. A node whose firmware exposes none
    passes every check, has RHCOS written to its disk, and then fails in the
    initramfs on first boot — `clevis luks bind` errors, `ignition-disks.service`
    fails, and systemd drops to `emergency.target`. It never registers, so the
    installer only reports a host that never joined; the real message is on the
    serial or BMC console. Enable TPM 2.0 in firmware on every selected node
    first — on many vendors it is off by default — and clear stale TPM keys from
    a previous OS, which can wedge the deployment on their own.

!!! note "Day 1 only"
    `storage.luks` runs in the initramfs of the first boot after RHCOS is
    written; there is no second chance. The Machine Config Operator classifies
    `Storage.Luks` and `Storage.Filesystems` as irreconcilable and marks a node
    that receives them later `Degraded` rather than applying them. Adding the
    block to a running cluster therefore means reinstalling its nodes.

!!! note "No PCR policy on RHCOS"
    Unlike a [`MachineInstallProfile`](machines.md#sealing-to-the-boot-state),
    this block takes no `pcrIds`. Ignition seals with an empty TPM policy, and
    the agent-based installer exposes no way to pass one, so `pcrIds` and
    `pcrBank` are rejected here rather than silently ignored. The key is bound to
    the chip: disk theft is defeated, boot-chain tampering is not.

## Platform

`install.platform.type` is the installer **platform render mode**, not the
substrate type — substrate ownership stays with the selected machines and their
providers. The platform mode feeds the generated `install-config.yaml`.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `install.platform.type` | No | Derived from the bound machines' provider | `baremetal`, `vsphere`, `none`, or `external`. |
| `install.platform.baremetal.provisioningNetwork` | No | — | `disabled`, `managed`, or `unmanaged`. Authored lowercase; install-config spells the values `Disabled`/`Managed`/`Unmanaged`, and Bootwright renders the native casing for you. |
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
| `node` | The cluster's single node owns the endpoint; the address resolves from that node's `Machine`. Single-node clusters only, and `address` must be empty. |

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `endpoints.<slot>.address` | No | Node install address for the `node` source | Literal endpoint address. Required for the `openshift` and `external` sources, and must be empty for the `infraComponent` and `node` sources (see the source table above). |
| `endpoints.<slot>.dnsName` | No | — | Optional DNS name. |
| `endpoints.<slot>.port` | No | — | Optional port. |
| `endpoints.<slot>.scheme` | No | — | Optional scheme. |
| `endpoints.<slot>.prefixLength` | No | — | Prefix length for VIP-style endpoints. |
| `endpoints.<slot>.interfaceNetworks[]` | No | — | Interface networks for VIP placement. |
| `endpoints.<slot>.source.type` | No | `openshift` | `openshift`, `external`, `infraComponent`, or `node`. |
| `endpoints.<slot>.source.componentRef` | No | — | Load-balancer `InfraComponent` name. Required for the `infraComponent` source. |
| `endpoints.<slot>.source.bindAddressRef` | No | — | Names a `bindAddresses[]` entry on the referenced load balancer. |

!!! note "Single-node clusters answer at their node, not at a VIP"
    A single-node cluster cannot use `source.type: openshift` on the `api`,
    `api-int`, or `ingress` slot — pair it with the `platform.none` default
    above. Use `source.type: node`, which says the slot answers at the
    cluster's one node and resolves the address from that node's `Machine`:

    ```yaml
    endpoints:
      api:
        source:
          type: node
      api-int:
        source:
          type: node
      ingress:
        source:
          type: node
    ```

    The address comes from the `Machine.spec.addresses[]` entry that the node's
    `spec.network.config.interfaceAddresses[]` points at — the same install
    address the node boots with — so there is nothing to keep in agreement.
    `render effective` shows the resolved address. `node` is valid only on a
    cluster with exactly one node, and a node whose `interfaceAddresses[]`
    resolve to no address or to more than one is rejected naming what was
    found. `external` and `infraComponent` remain available if an operator-owned
    load balancer or DNS name fronts the node. For how VIPs and managed load
    balancers wire together, see
    [Networking](../advanced/networking.md).

!!! note "One cluster, one IP address family"
    Single-stack is the current scope. A `ContainerCluster` whose machine
    networks, `spec.networking.clusterNetwork`/`serviceNetwork`, and resolved
    endpoint addresses do not all share one address family is rejected naming
    the two conflicting values and their families. IPv6-only is fully
    supported — the rule refuses *mixing* families, not IPv6. Dual-stack is
    deferred: `endpoints.<slot>.address` is a single address where the native
    `apiVIPs`/`ingressVIPs` are lists precisely so a cluster can carry one VIP
    per family.

## Artifact Server Endpoints

Agent install consumers own the artifact server endpoint they use. The reusable
selector shape is:

```yaml
artifactServerEndpoint:
  serverRef: default   # optional when a default artifactServers[] entry exists
  endpointRef: bmc
```

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `install.agent.redfishVirtualMedia.artifactServerEndpoint.endpointRef` | No | — | Endpoint the Redfish BMC fetches the agent ISO from. |
| `install.agent.bootArtifacts.artifactServerEndpoint.endpointRef` | No | — | Endpoint that publishes agent boot artifacts and becomes `bootArtifactsBaseURL`. |
| `artifactServerEndpoint.serverRef` | No | Default `artifactServers[]` entry | Names an `Environment.spec.infraComponents.artifactServers[].name`. Empty inherits the entry marked `default: true`, or the sole entry. |

The Redfish endpoint is required when any node is bare-metal; the
boot-artifacts endpoint is required for disconnected installs.

`endpointRef` is never defaulted globally. A new consumer adds its own
`artifactServerEndpoint` field instead of adding a slot to `InfraComponent` or
`Environment`.

## Networking

`spec.networking` carries the OpenShift cluster and service networks plus the
network type. The whole block and any field within it may be omitted.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `networking.networkType` | No | — | OpenShift network type; renders the installer default when omitted. An authored value passes through verbatim in its native casing (e.g. `OVNKubernetes`). |
| `networking.clusterNetwork[].cidr` | Yes (per entry) | `10.128.0.0/14` (default entry) | Pod network CIDR. |
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

There are none to author. `spec.nodes[].role` is the whole roster: Bootwright
derives the control-plane and compute replica counts from it, and the agent
installer renders one default-architecture master pool and one worker pool (an
`infra` node installs in the worker pool). Strict decode rejects
`spec.controlPlane` and `spec.compute[]`, along with the other install-config
machine-pool fields (`replicas`, `architecture`, `hyperthreading`, `platform`,
`name`), naming the offending line.

## Nodes

`spec.nodes[]` binds each cluster node to a backing `Machine`. At least one
`master` node is required.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `nodes[].name` | Yes | — | The cluster's name for the node, independent of the machine name; unique within the cluster. Must be a DNS label (`[a-z0-9]([-a-z0-9]*[a-z0-9])?`) and composes to `<name>.<cluster>.<domains.containerClusters>` (the container-cluster zone; see [Environment → Domain model](environment.md#domain-model)). A dotted value is rejected — use `nodes[].fqdn` to pin a name outside that zone. The composed FQDN is the OpenShift node name and must equal the host's real OS hostname. It is also the node the day-2 node-config step targets when applying labels/taints. |
| `nodes[].fqdn` | No | composed from `name` | Explicit FQDN for a node whose real OS hostname lives outside the cluster zone (a pre-existing corporate host, say). Used verbatim as the node's cluster-visible identity. `name` is still required — it stays the node's identity inside the cluster and is what `placement`/bootstrap references resolve against. |
| `nodes[].role` | Yes | — | `master`, `worker`, or `infra`. |
| `nodes[].machineRef` | Yes | — | `Machine` that backs this node; no default is derived. |
| `nodes[].labels` | No | — | Extra node labels Bootwright applies day-2. |
| `nodes[].taints` | No | — | Extra node taints (`key`, optional `value`, `effect` ∈ {`NoSchedule`, `PreferNoSchedule`, `NoExecute`}) applied day-2. |

!!! note "Infra nodes are an authoring role"
    OpenShift has no install-time `infra` pool, so an `infra` node installs as a
    `worker` (it counts toward `compute[].replicas`) and Bootwright promotes it
    day-2 against the running cluster: it adds the
    `node-role.kubernetes.io/infra` label, a `node-role.kubernetes.io/infra:NoSchedule`
    taint, and the `infra` `MachineConfigPool`, plus any `nodes[].labels`/`taints`
    you author. Moving ingress, monitoring and other operands onto infra (the
    matching tolerations/nodeSelectors) is left to you. Authored `labels`/`taints`
    on a plain `master`/`worker` node are applied day-2 too, without the infra
    label/MCP.

!!! note "Where node binding rules are enforced"
    A referenced `Machine` must carry the `openshift-node` capability and may be
    node-bound by at most one cluster — across every `ContainerCluster` and
    `StorageCluster` — and at most one node entry. Those rules are enforced
    here. A node `Machine` is installed by Bootwright, so it is declared with
    `os.provided: false` on the `Machine` itself; that constraint is enforced by
    [Machine](machines.md) validation, not by `ContainerCluster`.

## Native mapping

Where the high-value `openshift-install` input keys live in Bootwright — the
rows an operator arriving with a working native config will look up. See
[conventions](index.md) for how to read the table.

### Native mapping — `install-config.yaml`

| Native key or flag | Bootwright path | Class | What the divergence buys |
| --- | --- | --- | --- |
| `baseDomain` | `Environment` `spec.domains.containerClusters` | relocated | fleet-level default — one zone for every cluster in the context |
| `metadata.name` | `metadata.name` | mirror | — |
| `controlPlane.replicas` / `compute[].replicas` | derived — counted from `spec.nodes[].role` (`infra` and `worker` count as compute) | derived | the node roster is the single source of truth |
| `networking.networkType` / `clusterNetwork[].cidr` / `clusterNetwork[].hostPrefix` / `serviceNetwork[]` | `spec.networking.networkType` / `spec.networking.clusterNetwork[]` / `spec.networking.serviceNetwork[]` | mirror | — |
| `networking.machineNetwork[].cidr` | `NetworkConfig` `spec.machineNetwork[]` | relocated | cross-document reference — machines and clusters share one network template |
| `platform.<type>` | `spec.install.platform.type` | mirror | — (discriminated union; the arm key equals the type) |
| `platform.baremetal.provisioningNetwork` | `spec.install.platform.baremetal.provisioningNetwork` | restructured | mirror key; only the value vocabulary is normalized — authored lowercase `disabled`/`managed`/`unmanaged`, rendered native `Disabled`/`Managed`/`Unmanaged` |
| `platform.*.apiVIPs` / `ingressVIPs` | `spec.install.endpoints.api.address` / `spec.install.endpoints.ingress.address` | restructured | cross-document reference plus a `source` union naming who owns the VIP. The native keys are lists so a dual-stack cluster can carry one VIP per address family; the Bootwright path is a single address because single-stack is the current scope, and a cluster mixing families is rejected rather than rendered single-stack |
| `platform.*.loadBalancer.type` | derived from each endpoint's `source.type` (`UserManaged` when any source is not `openshift`) | derived | the endpoint source already says who runs the balancer |
| `pullSecret` | `spec.install.pullSecretRef` | renamed | secret `…Ref` indirection — no secret bytes in desired state |
| `sshKey` | `spec.install.nodeSSH.keyPairRef` (or `publicKeyRef`) | restructured | secret `…Ref` indirection; the pair also serves `cluster rsh`/`exec` |
| `additionalTrustBundle` | `spec.install.additionalTrustBundleRefs[]` | renamed | secret `…Ref` indirection; a list of bundles concatenates |
| `proxy` | `Environment` proxies (`spec.infraComponents.proxies[]` + `spec.proxyFor.containerClusterInstall`) | relocated | fleet-level default with per-consumer override |
| `imageDigestSources` | `Environment` `spec.registries.imageDigestSources[]`, plus entries derived from the registry-mirror component | relocated | fleet-level default; release-image entries are derived from the mirror |
| `fips` | `spec.security.fips.enabled` | renamed | grouped into one cluster security posture |

### Native mapping — `agent-config.yaml`

| Native key or flag | Bootwright path | Class | What the divergence buys |
| --- | --- | --- | --- |
| `rendezvousIP` | derived — the first master's primary install address | derived | one less hand-copied IP; the roster already knows it |
| `hosts[].hostname` | `spec.nodes[].name` | renamed | composes `<name>.<cluster>.<zone>` under the fleet domain model |
| `hosts[].role` | `spec.nodes[].role` | mirror | authoring-only extra value `infra` — installs as `worker`, promoted day-2 |
| `hosts[].interfaces[].name` / `.macAddress` | `Machine` `spec.hardware.nics[]` (mirror keys `name`, `macAddress`) | relocated | cross-document reference — hardware facts live on the machine |
| `hosts[].rootDeviceHints.*` | `Machine` `spec.os.install.rootDeviceHints` (byte-for-byte Metal3 mirror) | relocated | cross-document reference — the hint survives a node moving between clusters |
| `hosts[].networkConfig` | `NetworkConfig` `spec.template.networkConfig` (nmstate passthrough) | relocated | cross-document reference — one template serves many hosts |
| `additionalNTPSources[]` | derived from the `Environment` NTP catalog entry / `InfraComponent` | derived | fleet-level default |
| `minimalISO` / `bootArtifactsBaseURL` | derived from `spec.install.mode: disconnected` + `spec.install.agent.bootArtifacts.artifactServerEndpoint` | derived | one mode switch drives the disconnected boot-artifact plumbing |

## Where to go next

- [Networking](../advanced/networking.md) — endpoints, VIPs, and managed load
  balancers in depth.
- [KubeVirt child clusters](../advanced/kubevirt.md) — nesting a
  `ContainerCluster` on a KubeVirt-backed parent.
- [Add-ons](add-ons.md) — post-install bootstrap components bound after install.
- [Conventions](index.md) — the object envelope, unions, references, and
  defaults that govern every kind.
