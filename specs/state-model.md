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
- `installSource.type: redhatCDN` sets `rhsm.organizationRef.name` and
  `rhsm.activationKeyRef.name`. Both secret refs point to
  `Environment.spec.secrets` entries; secret bytes are read only from runtime
  secret material when Bootwright renders the Kickstart.
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
  network config and merged with per-machine overrides.
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
- Endpoint sources accept `external`, `infraComponent`, `openshift`, or
  `cephadm`.
- `spec.nodes[].machineRef.name` references a `Machine` with
  `openshift-node` capability and `os.provided: false`.
- Each node hostname must be unique inside the cluster.
- Node network input is owned by the referenced `Machine`, not by the cluster
  node entry.
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
- `cephadm.addressRef.name`, when set, selects a named
  `Machine.spec.addresses[]` entry for cephadm traffic.
- `cephadm.bootstrap.seedNode` names a storage topology node.
- Storage placement policies, pools, filesystems, gateways, and exports must
  reference the owning `StorageCluster`.

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
- `apply --stage infra` includes provider, infra-components, machine-infra, and
  storage-infra work.
- `apply --stage clusters` includes storage-cluster, container-cluster, and
  add-ons work.
- `destroy --stage infra` tears down infrastructure for the current context.
  Without `--clusters`, it must also remove all context-owned VMs that provider
  adapters can identify. With `--clusters`, it is limited to the selected
  `ContainerCluster` names and must not run context-wide VM cleanup.
- `destroy --stage clusters` removes OpenShift cluster install runtime state
  for selected or all `ContainerCluster` names.
- `bootwright host trust` records SSH server-key trust for declared machines.
- Rendered effective state must not include secret bytes.
