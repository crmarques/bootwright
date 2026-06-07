# Desired-State Model

Bootwright desired state uses `apiVersion: bootwright.io/v1alpha1` and
seventeen user-authored kinds. The schema intentionally tracks the inputs
consumed by `openshift-install` for agent installs, Bootwright-managed machine
OS installation, and cephadm for external Ceph storage.

There is no compatibility layer for abandoned kinds or fields. Retired resource
shapes must fail strict decode or validation instead of being translated.

## Kinds

- `Environment` owns fleet-wide defaults, context resource selection, selected
  clusters, secret sources, proxy defaults, registry mirrors, component image
  pins, and service access catalog entries.
- `Machine` owns machine desired state: capability tags, substrate binding,
  provided-vs-installed OS state, install profile reference, install network,
  named addresses, SSH, and hardware inventory.
- `MachineImage` owns bootable OS install media such as a base ISO URL,
  checksum, and trust references.
- `MachineInstallProfile` owns Bootwright-managed OS install behavior:
  installer type, image reference, repositories, host naming, SSH
  authorization, storage, packages, and services.
- `NetworkConfig` owns reusable `machineNetwork[]`, name-resolution service
  selections, and NMState templates rendered into installer inputs.
- `InfraProvider` owns substrate capabilities, machine profiles, provider
  connection facts, and network attachments.
- `InfraComponent` owns machine-bound shared services and their routable
  endpoints.
- `ContainerCluster` owns OpenShift or OKD install intent: distribution,
  release, install mode, platform render mode, endpoints, artifact access,
  trust, cluster networking, machine pools, and node-to-machine bindings.
- `StorageCluster` owns imported or managed Ceph intent. Managed clusters
  provision Ceph from selected machines.
- `StoragePlacementPolicy`, `StoragePool`, `StorageFilesystem`, and
  `StorageObjectGateway` own Ceph placement, pools, CephFS, RGW, and ingress
  endpoint intent.
- `StorageExport` owns the exported storage surface.
- `ClusterAddon`, `ClusterAddonProfile`, and `ClusterAddonBinding` own
  post-install component definitions, reusable add-on sets, and binding-scoped
  input values.

## Environment

`Environment` is fleet-wide. It contains defaults, optional input resource
selection, and secret references, never secret bytes.

Rules:

- `resources[]`, when set, is a YAML file or directory allow-list relative to
  the `Environment` file directory. The `Environment` file itself is always
  loaded.
- When `resources[]` is omitted, the current context input directory loads
  every discovered YAML file.
- A listed file is loaded as a complete YAML file. A listed directory is walked
  deterministically for YAML files.
- Every referenced Bootwright resource must also be selected.
- `containerClusters[]` and `storageClusters[]`, when set, are the effective
  fleet selection lists for render, apply, status, destroy, and check flows.
- `safety.destroyProtection`, when set, must be `allow` or
  `requiredOverride`. Empty means `allow`. Bootwright never infers protection
  from environment names, context names, labels, or cluster names.
- `defaults.install.pullSecretRef` and `defaults.install.nodeSSH` fill omitted
  cluster install values only.
- `defaults.artifactAccess`, when set, is copied into selected
  `ContainerCluster.spec.install.artifactAccess` fields only for active
  artifact consumers.
- `infraComponents.*[]` entries are service access catalog entries. They are
  either `external` with direct access configuration or `managed` with
  `componentRef.name` pointing at an `InfraComponent` arm of the matching kind.
- `proxyFor.bootwright` and `proxyFor.containerClusterInstall` select proxy
  catalog entries by name. Omitted values default to `none`.
- `secrets` declares names, not bytes.
- `entitlements[]` declares named subscription, registry entitlement, and
  license references for products that need vendor-controlled access. It
  accepts `provider: community | redhat | ibm` and
  `product: ceph | rhel | openshift | ibm-storage-ceph`; referenced secret
  material still lives in `Environment.spec.secrets`.

Authored desired-state YAML uses block-style collections. Do not use
flow-style mapping braces, inline lists, or empty inline maps in examples, e2e
inputs, fixtures, or scaffold output.

## Machine

`Machine` is the single user-facing model for raw hardware, virtual machines,
machines whose OS is already installed, and machines whose OS Bootwright should
install.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: ocp-master-0
spec:
  capabilities:
    - openshift-node
  substrate:
    providerRef:
      name: rack-a
  hardware:
    nics:
      - name: primary
        macAddress: 52:54:00:21:11:10
    boot:
      nicRef:
        name: primary
    management:
      bmc:
        address: redfish-virtualmedia+https://bmc-0.example.test/redfish/v1/Systems/1
        credentialsRef:
          name: bmc-credentials
  os:
    provided: false
    install:
      rootDeviceHints:
        deviceName: /dev/sda
  network:
    config:
      networkConfigRef:
        name: ocp-machine-net
      attachmentRef:
        name: ocp-install
      overrides:
        interfaces:
          - name: primary
            ipv4:
              address:
                - ip: 192.0.2.20
                  prefix-length: 24
    interfaceBinding:
      - nicRef:
          name: primary
        interfaceName: primary
  addresses:
    - name: ip
      address: 192.0.2.20
```

Rules:

- `spec.os.provided` is required. `true` means the OS already exists;
  `false` means Bootwright or a downstream installer must provision the OS.
- `spec.os.provided: false` machines must declare `spec.substrate.providerRef`.
- Bare-metal install machines declare physical inventory under
  `spec.hardware.nics`, boot NIC selection under `spec.hardware.boot.nicRef`,
  and BMC access under `spec.hardware.management.bmc`.
- Libvirt, vSphere, and KubeVirt install machines select VM shape through
  `spec.substrate.profileRef`.
- `spec.os.profileRef` selects a `MachineInstallProfile` when Bootwright
  installs a managed OS.
- `spec.os.install.rootDeviceHints` is the Machine-owned root-device hint
  location.
- `spec.network.config.networkConfigRef` selects a `NetworkConfig`.
- `spec.network.config.attachmentRef` selects an
  `InfraProvider.spec.networkAttachments[]` entry on the machine provider.
- `spec.network.config.overrides` merges into the rendered NMState for that
  machine.
- `spec.network.interfaceBinding[]` maps hardware NIC names to effective
  NMState interface names for MAC injection.
- `spec.addresses[]` owns durable named addresses used by SSH and shared
  service endpoints.
- `spec.access.ssh` owns bastion-to-machine SSH identity and known-host
  material.
- `spec.capabilities[]` is a generic tag list such as `openshift-node`,
  `ceph-node`, `ceph-admin`, `container-runtime`, `artifact-server`,
  `load-balancer`, `proxy`, `name-resolution`, `ntp`, `registry`, `libvirt`.

## MachineImage

`MachineImage` describes bootable media used by managed OS installation.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-94-dvd-iso
spec:
  type: iso
  mediaType: dvd
  url: local-media:rhel-9.4-x86_64-dvd.iso
  checksum: sha256:0000000000000000000000000000000000000000000000000000000000000000
  trustRefs:
    - name: image-ca
```

Rules:

- `spec.type` currently accepts `iso`.
- `spec.mediaType` accepts `dvd` or `boot`. When omitted, Bootwright treats
  normal install media as `dvd` and filenames ending in `boot.iso` as `boot`.
- `spec.installSource` is required for `mediaType: boot`. It accepts
  `type: url` for a plain HTTP(S) install tree or `type: redhatCDN` for an
  RHSM-backed Red Hat CDN install.
- `installSource.type: url` can set `url` as the primary Anaconda install
  tree. Alternatively, `repositories[0].baseURL` becomes the primary install
  tree and subsequent repositories become additional Kickstart `repo` entries.
- `installSource.type: redhatCDN` sets `entitlementRef.name`, which must
  resolve to a Red Hat `rhel` entitlement. RHSM organization and activation
  key secret refs are owned by that Environment entitlement.
- `url` is required and accepts `local-media:<filename.iso>`, `file://`
  absolute paths, `http://`, or `https://`.
- `local-media:<filename.iso>` resolves to the root-managed ISO media store
  under `/var/lib/bootwright/media/`. The media key is exactly the stored
  filename; it must be a basename ending in `.iso` and must not contain path
  traversal.
- Normal OpenShift agent ISOs generated by `openshift-install` are generated
  context artifacts, not managed media-store inputs. Future user-supplied
  RHCOS ISO fields may use `local-media:<key>`, but RHCOS rootfs, kernel,
  initramfs, and release image content must not use the ISO media store.
- `checksum`, `trustRefs[]`, and `headersRefs[]` are optional.
- Secret refs point to `Environment.spec.secrets` entries.

## MachineInstallProfile

`MachineInstallProfile` declares how Bootwright installs and customizes an OS.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineInstallProfile
metadata:
  name: rhel-94-ceph-node
spec:
  os:
    family: rhel
    version: "9.4"
    architecture: x86_64
  installer:
    type: anaconda
    anaconda:
      imageRef:
        name: rhel-94-dvd-iso
      repositories:
        - id: extras
          baseURL: https://repos.example.test/rhel/9.4/extras/x86_64/os/
  customizations:
    hostname:
      source: machineName
    ssh:
      authorizeMachineSSHKey: true
      passwordAuthentication: false
    storage:
      rootDevice:
        source: machineRootDeviceHints
      wipe: true
    packages:
      - cephadm
    services:
      enabled:
        - sshd
```

Rules:

- `spec.installer.type` currently accepts `anaconda`.
- `spec.installer.anaconda.imageRef.name` references a `MachineImage`.
- `spec.installer.anaconda.repositories[]` declares additional Anaconda
  repositories for the profile. The primary boot-ISO install source is owned by
  the referenced `MachineImage`.
- `customizations.hostname.source` accepts `machineName`.
- `customizations.storage.rootDevice.source` accepts
  `machineRootDeviceHints`.
- A `Machine` with `os.provided: false` and managed OS install must set
  `spec.os.profileRef.name`.

## InfraProvider

`InfraProvider` owns substrate capabilities and network attachments.

Rules:

- `spec.type` accepts `baremetal`, `libvirt`, `vsphere`, or `kubevirt`.
- Bare-metal providers declare boot behavior. Physical machine inventory lives
  on `Machine.spec.hardware`.
- Libvirt, vSphere, and KubeVirt providers declare `machineProfiles[]`.
  Machines select a profile through `Machine.spec.substrate.profileRef`.
- `networkAttachments[]` names provider-specific attachment targets. Machines
  bind to them through `spec.network.config.attachmentRef`.
- KubeVirt providers set exactly one of `hostClusterRef` or `kubeconfigRef`.
  `hostClusterRef` references a Bootwright `ContainerCluster`; `kubeconfigRef`
  references a secret containing an external virtualization kubeconfig.

## InfraComponent

`InfraComponent` owns shared services that run on selected machines.

Rules:

- Component arms use `machineRef.name` for placement.
- Artifact server, proxy, name-resolution, NTP, registry, and load-balancer
  arms require compatible machine capabilities.
- Endpoint entries use `machineAddress` to select a named
  `Machine.spec.addresses[]` value on the placement machine.

## NetworkConfig

`NetworkConfig` owns machine CIDRs, optional DNS catalog refs, and an NMState
template.

Rules:

- `spec.machineNetwork[].cidr` is required.
- `spec.template.networkConfig` is rendered into machine-level installer
  network config and merged deterministically with
  `Machine.spec.network.config.overrides`. The merge is the same for rendering
  and validation:
  - Maps deep-merge; the override value wins on conflict.
  - A list whose entries all carry a non-empty `name` merges by `name`: an
    override entry updates the matching base entry, and an override entry whose
    `name` is not present in the base is appended.
  - A list whose base and override entries are all maps but not all named merges
    positionally by index.
  - Any other list shape (for example scalars, or a mix of named and unnamed
    map entries) is rejected as an override error naming the owning field,
    rather than silently dropped.
- `spec.dnsRefs[]` selects name-resolution catalog entries by name.

## ContainerCluster

`ContainerCluster` owns cluster install intent.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: ocp-3node
spec:
  distribution:
    type: openshift
    release:
      version: 4.21.15
  install:
    method: agent
    platform:
      type: bareMetal
      baremetal:
        provisioningNetwork: disabled
    endpoints:
      api:
        address: 192.0.2.10
        source:
          type: external
      api-int:
        address: 192.0.2.10
        source:
          type: external
      ingress:
        address: 192.0.2.11
        source:
          type: external
    artifactAccess:
      serverRef:
        name: default
      redfishVirtualMedia:
        endpointRef:
          name: bmc
    nodeSSH:
      keyPairRef:
        name: ocp-3node-cluster-admin-ssh-key
  networking:
    clusterNetwork:
      - cidr: 10.128.0.0/14
        hostPrefix: 23
    serviceNetwork:
      - 172.30.0.0/16
  nodes:
    - hostname: master-0
      role: master
      machineRef:
        name: ocp-master-0
```

Rules:

- `spec.install.method` defaults to `agent`; only `agent` is currently
  accepted.
- `spec.install.platform.type` accepts `bareMetal`, `vsphere`, `none`, or
  `external`.
- OpenShift standard endpoint slots are `api`, `api-int`, and `ingress`.
- `endpoints.<slot>.source.type` accepts `openshift`, `external`,
  `infraComponent`, or `cephadm`; an omitted `source.type` defaults to
  `openshift`. The accepted set and required companion fields are
  consumer-specific:
  - **Container `api`, `api-int`, `ingress`** accept only `openshift` (default),
    `external`, or `infraComponent`; `cephadm` is rejected. `openshift` and
    `external` require `address`. `infraComponent` requires
    `source.componentRef.name` pointing at a `loadBalancer` `InfraComponent` and
    `source.bindAddress`, and must not set `address`.
  - **Single-node clusters** additionally reject `source.type: openshift` on the
    `api`, `api-int`, and `ingress` slots.
  - **`StorageObjectGateway.spec.publicEndpointRef`** requires an endpoint with
    `source.type: external` and a `dnsName`.
  - **`StorageObjectGateway` ingress `endpointRef`** requires an endpoint with
    `source.type: cephadm`, an `address`, and a `prefixLength`.
  - `source.componentRef` and `source.bindAddress` are valid only when
    `source.type: infraComponent`. Every endpoint must set `address`, `dnsName`,
    or `source.type: infraComponent`.
- `spec.nodes[].machineRef.name` references a `Machine` with
  `openshift-node` capability and `os.provided: false`.
- Each node hostname must be unique inside the cluster.
- Node network input is owned by the referenced `Machine`, not by the cluster
  node entry.
- `spec.distribution.type` accepts `openshift` (default) or `okd`; `openshift`
  clusters require a pull secret via `spec.install.pullSecretRef` or the
  `Environment` default.
- `spec.install.mode` accepts `connected` (default) or `disconnected`;
  `spec.install.method` accepts `agent` (default).
- `spec.nodes[].role` accepts `master` or `worker`; a cluster requires at least
  one `master` node.
- `spec.controlPlane.replicas`, when set, must equal the number of `master`
  nodes. `spec.compute[]` declare worker machine pools; the summed `replicas` of
  the default worker pool (name omitted or `worker`) must equal the number of
  `worker` nodes. A single all-`master` topology omits both.
- Single-node topologies render installer platform `none` unless
  `platform.type: external` is explicit.

## StorageCluster

`StorageCluster` owns imported or managed external storage intent.

Rules:

- `spec.management` accepts `managed` or `external`; omitted means `managed`.
- Imported storage declares external details and does not run cephadm.
- Managed storage nodes reference `Machine` objects with `ceph-node`
  capability.
- Managed storage seed/admin operations use `Machine.spec.access.ssh`.
- Managed Ceph `spec.ceph.distribution` accepts `oss`, `redhat`, or `ibm`;
  omitted means `oss`.
- `distribution: oss` uses upstream/community Ceph package and image sources
  and must not set `entitlementRef`.
- `distribution: redhat` requires `entitlementRef.name` to resolve to a Red
  Hat `ceph` entitlement. Red Hat Ceph Storage repositories and registry
  access come from that entitlement and must not mix with upstream Ceph
  packages or images.
- `distribution: ibm` requires `entitlementRef.name` to resolve to an IBM
  `ibm-storage-ceph` entitlement with accepted license terms. IBM Storage Ceph
  repositories, registry access, and license acceptance come from that
  entitlement and must not mix with upstream Ceph packages or images.
- `cephadm.addressRef.name`, when set, selects a named
  `Machine.spec.addresses[]` entry for cephadm traffic.
- `cephadm.bootstrap.seedNode` names a storage topology node.
- `spec.type` is required and must be `ceph`.
- Managed clusters require `spec.ceph`; external clusters must not set
  `spec.ceph`.
- `spec.ceph.cephadm.bootstrap.seedNode` and
  `spec.ceph.cephadm.bootstrap.monIP.nodeRef.name` must name
  `spec.ceph.topology.nodes[]` entries.
- `spec.ceph.cephadm.registry.url` is required, must not contain whitespace, and
  must not embed credentials; `registry.credentialsRef.name` is required.
- `spec.ceph.networks.publicCIDRs[]` and `clusterCIDRs[]` must be valid CIDRs.
- `spec.ceph.topology.nodes[]` require a unique `name`, a `machineRef` to a
  `ceph-node` `Machine`, a `site`, and at least one `roles[]` value from
  `mon`, `mgr`, `osd`, `mds`, `rgw`, `ingress`. All node `Machine`s in one
  `StorageCluster` must share one SSH user and `keyRef`.
- Storage placement policies, pools, filesystems, gateways, and exports must
  reference the owning `StorageCluster`.
- When `spec.ceph.topology.stretch.enabled` is true: `dataSites` must contain
  exactly two sites; `tiebreaker.site` must be distinct from the data sites;
  `tiebreaker.node` and `ruleName` are required; `replicatedPoolDefaults` must
  be `size: 4` and `minSize: 2`; each data site must hold exactly two `mon`
  nodes and the tiebreaker site exactly one; the tiebreaker node must be
  mon-only with no OSD `devices`; erasure-coded pools are rejected; and MDS,
  RGW, and ingress placement must include at least two role-capable hosts per
  data site.

## StoragePlacementPolicy

`StoragePlacementPolicy` owns reusable Ceph placement and replicated-pool
defaults for the pools that select it.

Rules:

- `spec.storageClusterRef.name` is required and must reference a managed
  (non-`external`) `StorageCluster`.
- `spec.ceph.ruleName` is required.
- `spec.ceph.failureDomain` and `spec.ceph.replicated.{size,minSize}` are the
  defaults applied to pools that reference the policy.

## StoragePool

`StoragePool` owns one Ceph pool.

Rules:

- `spec.storageClusterRef.name` is required and must reference a managed
  `StorageCluster`.
- `spec.placementPolicyRef.name`, when set, must reference a
  `StoragePlacementPolicy` on the same `StorageCluster`. The referenced policy
  owns the pool's replication; `spec.ceph.replicated` must not also be set on
  the pool.
- `spec.ceph.type` accepts `replicated` (default) or `erasure-coded`.
  `replicated` must not set `ceph.erasureCoded`. `erasure-coded` requires
  `ceph.erasureCoded.{dataChunks,codingChunks}`, must not set `ceph.replicated`,
  and is not allowed on stretch-mode clusters.
- `spec.ceph.role`, when set, accepts `rbd`, `cephfs-metadata`, `cephfs-data`,
  or `rgw`. `spec.ceph.application` is the cephadm application override.
- On stretch-mode clusters, `ceph.replicated.size` must be `4` and `minSize`
  must be `2` when set.

## StorageFilesystem

`StorageFilesystem` owns one CephFS filesystem and the pools that back it.

Rules:

- `spec.storageClusterRef.name` is required and must reference a managed
  `StorageCluster`.
- `spec.cephfs.metadataPoolRef.name` is required and must reference a
  `StoragePool` on the same `StorageCluster`.
- `spec.cephfs.dataPoolRefs[]` is required; each must reference a `StoragePool`
  on the same `StorageCluster`, must differ from the metadata pool, and exactly
  one must set `default: true`.
- `spec.cephfs.mds.placement.hosts[]` select topology nodes; on stretch-mode
  clusters they must cover at least two MDS-capable hosts per data site.

## StorageObjectGateway

`StorageObjectGateway` owns one RGW service and its ingress endpoints.

Rules:

- `spec.storageClusterRef.name` is required and must reference a managed
  `StorageCluster`.
- `spec.ceph.serviceID` is required; `spec.ceph.frontendPort` must be in
  `0`–`65535`.
- `spec.publicEndpointRef` requires a `ContainerCluster` endpoint with
  `source.type: external` and a `dnsName`.
- `spec.ceph.placement.hosts[]` select topology nodes that hold the `rgw` role;
  on stretch-mode clusters at least two per data site.
- `spec.ceph.ingresses[]` require a unique `name`, an `endpointRef` to an
  endpoint with `source.type: cephadm`, an `address`, and a `prefixLength`, and
  a `placement` over `ingress`-role nodes.

## StorageExport

`StorageExport` owns the exported storage surface consumed by a downstream
platform.

Rules:

- `spec.type` is required and must be `data-foundation`.
- `spec.storageClusterRef.name` is required.
- For managed `StorageCluster`s, `spec.dataFoundation` is required;
  `dataFoundation.rbdPoolRef` and `cephFSRef` are required and must reference
  resources on the same `StorageCluster`; `objectGatewayRef` is optional and
  same-cluster.
- For external `StorageCluster`s, `spec.dataFoundation` must be empty and
  `spec.externalDetails` is required.
- `spec.externalDetails` must set exactly one of `fromSecret`, `generated`, or
  `sshExecution`. `fromSecret` must be declared in `Environment.spec.secrets`.
  `generated` is rejected for external clusters. `sshExecution` requires
  `machineRefs[]` to `ceph-admin` `Machine`s with SSH (for external clusters),
  `exporter.source: boundDataFoundationAddon`, and `config.rbdDataPoolName`;
  `config.format`, when set, must be `json`.

## ClusterAddon

`ClusterAddon` owns one post-install bootstrap component.

Rules:

- `spec.type` is required and must be `olm-operator` or `manifest-set`; the two
  arms are mutually exclusive.
- `olm-operator` requires `spec.olm` and must not set `manifestSet`.
  `olm.namespace.name` is required; `olm.subscription` requires `name`,
  `package`, `channel`, `source`, `sourceNamespace`, and `installPlanApproval`;
  `installPlanApproval` accepts `Automatic` or `Manual`.
- `manifest-set` requires `spec.manifestSet.manifests[]` (at least one) and must
  not set `olm`. Each `manifests[].path` is relative to the `ClusterAddon` file,
  ends in `.yaml`/`.yml`, must stay within the file directory, must not be a
  symlink, and must exist.
- `spec.provides[]` accepts `kubevirt` or `data-foundation`; declaring any
  `provides` value requires at least one `spec.readiness.checks[]` entry.
- `spec.readiness.timeout` is a Go duration. `spec.readiness.checks[].type`
  accepts `csvSucceeded` (requires `namespace`, `subscription`), `condition`
  (requires `apiVersion`, `kind`, `name`, `condition.{type,status}`), or
  `resourceExists` (requires `apiVersion`, `kind`, `name`).
- `spec.accepts.inputs[]` declare binding-scoped inputs; each schema property
  sets exactly one of `refKind` (a known Bootwright kind) or `secretRef`. A
  data-foundation storage-attachment effect requires an `exportRef` property.

## ClusterAddonProfile

`ClusterAddonProfile` owns a reusable, ordered group of add-ons.

Rules:

- A profile must include at least one of `spec.profiles[]` or `spec.addons[]`.
- `spec.profiles[].name` must reference a `ClusterAddonProfile`; nesting must be
  acyclic.
- `spec.addons[].name` must reference a `ClusterAddon`.

## ClusterAddonBinding

`ClusterAddonBinding` owns the per-cluster binding of add-ons and binding-scoped
input values.

Rules:

- `spec.clusterRef.name` is required and references a `ContainerCluster`.
- A binding must include at least one of `spec.addonProfiles[]` or
  `spec.addons[]`.
- A given `ClusterAddon` may be applied to one `ContainerCluster` only once
  across all bindings.
- `spec.addons[].inputs[]` must be declared by the add-on's
  `spec.accepts.inputs`, have unique names, and satisfy the input schema's
  required values.

## Rendering Contract

- `install-config.yaml` is rendered from `ContainerCluster`, `Environment`,
  selected machines, selected providers, endpoints, and platform render mode.
- `agent-config.yaml` hosts are rendered from `ContainerCluster.spec.nodes`,
  each referenced `Machine`, selected `NetworkConfig` templates, per-machine
  network overrides, and substrate MAC inventory.
- Machine boot variables are rendered from `Machine` substrate facts,
  `InfraProvider` capabilities, and cluster artifact access.
- Shared service variables are rendered from the machine-service graph built
  from `InfraComponent`, environment catalog selections, network DNS refs, and
  cluster endpoint sources.
- Storage tool inputs are rendered from `StorageCluster`, selected storage
  machines, placement resources, and storage exports.

## Validation Rules

- Unknown kinds and unknown fields are rejected at load time.
- Retired kinds and fields are not migrated.
- References must resolve to loaded resources selected by `Environment`.
- Machines must declare `spec.os.provided`.
- Machines with `os.provided: false` must have `spec.substrate.providerRef`.
- Machines that are used over SSH must declare
  `spec.access.ssh.addressRef.name`, `keyRef.name`, and a matching address.
- Provider network attachment refs must exist and match the provider arm used
  by the machine.
- Container cluster endpoints must resolve to valid addresses or valid
  InfraComponent bind addresses.
- Bare-metal boot requires BMC details and artifact access suitable for the
  configured boot method.
- KubeVirt `hostClusterRef` dependencies must be acyclic. A cluster cannot
  host itself directly or indirectly.
- Secret references must resolve to `Environment.spec.secrets`.

## CLI Contract

- Human CLI output goes through `internal/cli/output` except JSON output, shell
  exports, Cobra help, prompts, and external process passthrough.
- Cluster selection uses one flag name, `--clusters`, on every command that
  narrows to specific roots: `apply`/`plan`/`destroy --stage`, the `check` and
  `destroy` scope subcommands, and `render`. It accepts a comma-separated list
  of `ContainerCluster` and `StorageCluster` names. `destroy infra --clusters`
  additionally accepts the literal `artifact-server` to remove only the
  generated artifact publication service; that targeted infra cleanup is the
  reason `destroy` keeps the `infra` subcommand alongside the `--stage`
  selector.
- `apply --stage infra` includes provider, infra-components, machine-infra, and
  storage-infra work.
- `apply --stage clusters` includes storage-cluster, container-cluster, and
  add-ons work.
- `destroy --stage infra` tears down infrastructure for the current context.
  It uses current desired state plus root-managed ownership records. Without
  `--clusters`, it must also remove all context-owned VMs that provider
  adapters can identify. With `--clusters`, it is limited to selected
  `ContainerCluster` or `StorageCluster` roots and must not run context-wide VM
  cleanup. Managed machine disk cleanup is limited to provider-owned disks or
  declared Bootwright-managed devices; Bootwright must not wipe arbitrary
  visible disks.
- `destroy --stage clusters` removes cluster-stage runtime for selected or all
  `ContainerCluster` and `StorageCluster` names: OpenShift install runtime,
  add-on records, generated storage attachment records, and managed storage
  cluster services/runtime. It does not destroy provider infrastructure.
- Destroy must remove host packages only when ownership records prove
  Bootwright installed them and no remaining ownership record on that host
  still requires the package.
- `destroy --stage infra|clusters --override` is required when selected state
  contains `Environment.spec.safety.destroyProtection: requiredOverride`.
  `--yes` only skips the confirmation prompt and never implies `--override`.
- `apply --override` is command-scoped. It may continue past
  Bootwright-owned unsafe mismatch checks that have an explicit override path,
  but it must not bypass active-run leases, validation, secret checks, or
  foreign-resource ownership failures.
- `bootwright host trust` records SSH server-key trust for declared machines.
- Rendered effective state must not include secret bytes.
