# Desired-State Model

Bootwright desired state uses `apiVersion: bootwright.io/v1alpha1` and
seventeen user-authored kinds. The schema intentionally tracks the inputs
consumed by `openshift-install` for agent installs, Bootwright-managed machine
OS installation, and cephadm for external Ceph storage.

There is no compatibility layer for abandoned kinds or fields. Retired resource
shapes must fail strict decode or validation instead of being translated.

## Kinds

The seventeen kinds and the fact each owns are listed in `domain.md` (Operating
Model). This document specifies each kind's fields, validation, and the CLI
contract.

## Environment

`Environment` is fleet-wide. It contains the fleet DNS base domain, defaults,
optional input resource selection, and secret references, never secret bytes.

Rules:

- `baseDomain` is required. It is the fleet DNS base domain rendered into each
  cluster's `install-config.yaml` `baseDomain`, and `Environment` is its single
  owner.
- `resources[]`, when set, is a YAML file or directory allow-list relative to
  the `Environment` file directory. The `Environment` file itself is always
  loaded.
- When `resources[]` is omitted, the current context's input directory loads
  every discovered YAML file.
- A listed file is loaded as a complete YAML file. A listed directory is walked
  deterministically for YAML files.
- Every referenced Bootwright resource must also be selected.
- `containerClusters[]` and `storageClusters[]`, when set, are the effective
  fleet selection lists for render, apply, status, destroy, and check flows.
  Loaded clusters outside the selection are excluded before validation runs;
  `bootwright validate` warns about each excluded cluster so an unselected
  cluster file never disappears silently.
- `safety.destroyProtection`, when set, must be `allow` or
  `requiredOverride`. Empty means `allow`. Bootwright never infers protection
  from environment names, context names, labels, or cluster names.
- `defaults.install.pullSecretRef` and `defaults.install.nodeSSH` fill omitted
  cluster install values only.
- `defaults.artifactAccess`, when set, is copied into selected
  `ContainerCluster.spec.install.artifactAccess` fields only for active
  artifact consumers. Its `serverRef` and `endpointRef` names are validated
  at the declaration site regardless of current consumers, so a typo fails
  when it is written; a per-cluster diagnostic on a normalize-injected value
  additionally names the Environment default it came from.
- `defaults.clientsMirror`, when set, must be an `http(s)` URL. It overrides the
  base URL Bootwright downloads the OpenShift clients (`oc`,
  `openshift-install`) from, for disconnected or mirrored labs.
- `infraComponents.*[]` entries are service access catalog entries. Each
  entry's `management` is either `external` with direct access configuration
  or `managed` with `componentRef` pointing at an `InfraComponent` arm of the
  matching kind. The word `type` is reserved API-wide for kind-of-thing
  discriminators (such as `InfraComponent.spec.type`), so the
  who-runs-it axis is spelled `management` here, matching
  `StorageCluster.spec.management`.
- `proxyFor.bootwright` and `proxyFor.containerClusterInstall` select proxy
  catalog entries by name. Omitted values default to `none`.
- `secretStorage.mode`, when set, must be `source` (default) or `context`.
  `context` requires `bootwright secret generate` to copy `file:`-sourced
  material into the encrypted context store before workflows read it; `source`
  reads operator file material in place.
- `registries.mirror`, when set, declares the disconnected mirror: optional
  `url` (external mirror) plus `trustBundleRef` and `credentialsRef` secret
  names. `registries.imageDigestSources[]` set `source`, `mirrors[]`, and an
  optional `sourcePolicy` of `NeverContactSource` or `AllowContactingSource`. A
  `ContainerCluster` with `install.mode: disconnected` requires
  `registries.mirror.trustBundleRef` plus either `registries.mirror.url` or a
  managed `infraComponents.registries[]` entry.
- `installTrust.caBundleRefs[]` are fleet-wide additional CA trust-bundle secret
  names rendered into cluster install trust.
- `componentImages` pins managed-service images as
  `componentImages.<componentType>.<implementation>`, keyed by the same
  component-type vocabulary as `InfraComponent.spec.type`. The accepted pairs
  are `loadBalancer.haproxy`, `registry.mirror-registry`, `proxy.squid`,
  `nameResolution.dnsmasq`, and `artifactServer.http`. Each entry sets at least
  one of `local` or `public`, and every reference must pin an explicit version
  tag or a `sha256:` digest; mutable references such as `:latest` or an omitted
  tag are rejected.
- `secrets[]` declares secret names, never bytes. Each item is exactly one of: a
  scalar secret name; or a single-key map whose value is `null`/omitted
  (context-local material), `{file: <path>}` (operator-owned local material,
  with an optional `keyFile: <path>` that requires `file`), or `{generated:
  ...}`. A `generated` value sets exactly one of `credentials` (optional
  `username`), `selfSignedCertificate` (required `commonName`; optional
  `dnsNames[]`, `ipAddresses[]`, `validityDays`), or `sshKeyPair` (optional
  `type: ed25519`, `comment`). `file` and `generated` are mutually exclusive.
  Any other shape is rejected naming `Environment.spec.secrets`.
- `entitlements[]` declares named subscription, registry entitlement, and
  license references for products that need vendor-controlled access. Each entry
  sets a `provider`/`product` pair from this compatibility matrix; other pairs
  are rejected:
  - `community`: `ceph`, `openshift`
  - `redhat`: `ceph`, `rhel`, `openshift`
  - `ibm`: `ibm-storage-ceph`

  A `rhel` entitlement and a `redhat`/`ceph` entitlement require `rhsm`
  (`organizationRef`, `activationKeyRef`); `redhat`/`ceph` also requires
  `registry.credentialsRef`. An `ibm-storage-ceph` entitlement requires
  `registry.credentialsRef`, `license.accept: true`, and `rhelEntitlementRef`
  naming a `redhat`/`rhel` entitlement for the RHEL subscription it runs on; it
  takes no inline `rhsm` arm. Referenced secret material still lives in
  `Environment.spec.secrets`.

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
    providerRef: rack-a
  hardware:
    nics:
      - name: primary
        macAddress: 52:54:00:21:11:10
    boot:
      nicRef: primary
    management:
      bmc:
        address: redfish-virtualmedia+https://bmc-0.example.test/redfish/v1/Systems/1
        credentialsRef: bmc-credentials
  os:
    provided: false
    install:
      rootDeviceHints:
        deviceName: /dev/sda
  network:
    config:
      networkConfigRef: ocp-machine-net
      interfaceAddresses:
        - interface: primary
          addressRef: ip
          prefixLength: 24
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
- `spec.os.installProfileRef` selects a `MachineInstallProfile` when Bootwright
  installs a managed OS.
- `spec.os.install.rootDeviceHints` is the Machine-owned root-device hint
  location.
- `spec.network.config.networkConfigRef` selects a `NetworkConfig`.
- `spec.network.config.spec` is the inline alternative to `networkConfigRef`: it
  carries a full `NetworkConfig` `spec` (`machineNetwork[]`, `template`) for a
  one-off machine instead of a shared, reusable `NetworkConfig`. Set exactly one
  of `networkConfigRef` or `spec`. `overrides` is valid only with
  `networkConfigRef`; `interfaceAddresses` require one of `networkConfigRef` or
  `spec`.
- `spec.network.config.attachmentRef` selects an
  `InfraProvider.spec.networkAttachments[]` entry on the machine provider.
  When omitted on a provider-backed machine that sets `networkConfigRef`,
  `attachmentRef` defaults to the `networkConfigRef`; `render
  effective` materializes the default and validation resolves it against the
  provider's `networkAttachments[]`. An authored `attachmentRef` always wins.
  The default is accepted only while the provider declares a single
  attachment of its kind; with several, validation rejects the defaulted
  reference and lists the candidate names, so renaming a `NetworkConfig` can
  never silently re-bind a machine to a different attachment.
- `spec.network.config.interfaceAddresses[]` is the single owner of a node's
  static install IP. Each entry binds an NMState `interface` to a named
  `spec.addresses[]` entry through `addressRef` and sets `prefixLength` (and
  optional `family`, default `ipv4`); rendering injects the resolved address
  into that interface. Author the IP in `spec.addresses[]` once instead of
  duplicating it into an NMState override.
- `spec.network.config.overrides` merges arbitrary additional NMState (bonds,
  routes, extra interface attributes) into the rendered config for that machine;
  it must not carry the static install IP, which `interfaceAddresses` owns.
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
    - image-ca
```

Rules:

- `spec.type` currently accepts `iso`.
- `spec.mediaType` accepts `dvd` or `boot`. When omitted, normalize derives
  `boot` for `url` filenames ending in `boot.iso` and `dvd` otherwise;
  `render effective` shows the materialized value. A netinstall ISO whose
  filename does not end in `boot.iso` derives `dvd`, so author
  `mediaType: boot` explicitly for it.
- `spec.installSource` is required for `mediaType: boot`. It accepts
  `type: url` for a plain HTTP(S) install tree or `type: redhatCDN` for an
  RHSM-backed Red Hat CDN install. When `type` is omitted, normalize derives
  it from the fields present: `entitlementRef` means `redhatCDN`, `url` or
  `repositories` mean `url`.
- `installSource.type: url` can set `url` as the primary Anaconda install
  tree. Alternatively, `repositories[0].baseURL` becomes the primary install
  tree and subsequent repositories become additional Kickstart `repo`
  entries; normalize materializes the promotion, so the effective state
  shows the chosen install tree in `url`.
- `installSource.type: redhatCDN` sets `entitlementRef`, which must
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
      imageRef: rhel-94-dvd-iso
  customizations:
    hostname:
      source: machineName
    ssh:
      authorizeMachineSSHKey: true
    storage:
      rootDevice:
        source: machineRootDeviceHints
    packages:
      install:
        - cephadm
        - firewalld
    services:
      enabled:
        - sshd
    security:
      firewall:
        enabled: true
      fips:
        enabled: true
```

Rules:

- `spec.installer.type` currently accepts `anaconda`.
- `spec.installer.anaconda.imageRef` references a `MachineImage`.
- `spec.installer.anaconda.repositories[]` declares additional Anaconda
  repositories for the profile; each entry requires an `id` and an `http(s)`
  `baseURL`. The primary boot-ISO install source is owned by the referenced
  `MachineImage`.
- `customizations.hostname.source` accepts `machineName`.
- `customizations.storage.rootDevice.source` accepts
  `machineRootDeviceHints`.
- `customizations.packages.environment` currently accepts `minimal`, which
  renders the supported minimal Anaconda package environment.
- `customizations.packages.install[]` is the open allow-list of packages
  rendered into Kickstart; whatever is listed is installed. The validator
  enforces only the firewall dependency (a profile with
  `security.firewall.enabled: true` must list `firewalld`), not a closed set.
  `cephadm`, `podman`, `lvm2`, `chrony`, and `firewalld` are the recommended
  Ceph-node RHEL baseline, not a validated limit.
- `customizations.packages.excludeDocs: true` renders Kickstart package
  document exclusion.
- `customizations.packages.installWeakDeps: false` renders weak-dependency
  exclusion.
- `customizations.packages.languages[]` renders package language selection.
- `customizations.services.enabled[]` and
  `customizations.services.disabled[]` render Kickstart service state. A
  machine that references a managed OS install profile requires `sshd` in the
  enabled list so Bootwright can reconnect after installation.
- `customizations.security.selinux.mode` accepts `enforcing`, `permissive`, or
  `disabled`.
- `customizations.security.firewall.enabled` renders Kickstart firewall state.
  When true, the profile must install and enable `firewalld`.
- `customizations.security.fips.enabled: true` is supported only for RHEL
  Anaconda install profiles. It renders `fips=1` into the installer kernel
  command line through `mkksiso --cmdline`; changing this field on an installed
  machine is reinstall-only.
- A `Machine` with `os.provided: false` and managed OS install must set
  `spec.os.installProfileRef`.

## InfraProvider

`InfraProvider` owns substrate capabilities and network attachments.

Rules:

- `spec.type` accepts `baremetal`, `libvirt`, `vsphere`, or `kubevirt`.
- Bare-metal providers declare boot behavior. Physical machine inventory lives
  on `Machine.spec.hardware`.
- Libvirt, vSphere, and KubeVirt providers declare `machineProfiles[]`.
  Machines select a profile through `Machine.spec.substrate.profileRef`.
- Profile fields the selected provider's adapter does not consume are
  rejected: `template` and `failureDomainRef` are vSphere-only, and
  `dataDisks` are consumed by the libvirt and vSphere adapters only.
- vSphere `machineProfiles[].failureDomainRef` must name a
  `spec.vsphere.failureDomains[]` entry, and every
  `failureDomains[].server` must equal a declared `vcenters[].server`.
  With several declared failure domains every profile must set
  `failureDomainRef`; with exactly one, an empty ref resolves to it.
- vSphere `spec.vsphere.isoStaging` overrides where boot and install ISOs
  are uploaded; when authored it must set at least one of
  `{datastore, folder}`. Absent fields default to the machine's
  failure-domain `topology.datastore` and the stock vmedia folder.
- `networkAttachments[]` names provider-specific attachment targets. Machines
  bind to them through `spec.network.config.attachmentRef`.
- KubeVirt providers set exactly one of `hostClusterRef` or `kubeconfigRef`.
  `hostClusterRef` references a Bootwright `ContainerCluster`; `kubeconfigRef`
  references a secret containing an external virtualization kubeconfig.

## InfraComponent

`InfraComponent` owns shared services that run on selected machines.

Rules:

- `spec.type` is required and selects which kind of component this is:
  `artifactServer`, `loadBalancer`, `proxy`, `nameResolution`, `ntp`, or
  `registry`. The populated arm key is byte-identical to the type value.
- Each arm except `artifactServer` declares `implementation`: which software
  realises the component. Accepted implementations are `haproxy`
  (loadBalancer), `squid` (proxy), `dnsmasq` (nameResolution), `chrony` (ntp),
  and `mirror-registry` (registry) — the same spelling set
  `Environment.spec.componentImages` is keyed by.
- Component arms use `machineRef` for placement.
- Artifact server, proxy, name-resolution, NTP, registry, and load-balancer
  arms require compatible machine capabilities.
- Endpoint entries use `addressRef` to select a named
  `Machine.spec.addresses[]` value on the placement machine.
- A `nameResolution` arm authoritatively answers its rendered records and
  forwards every other query to `forwarders[]` (IP resolvers); with no
  `forwarders` it answers only local records.

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
- `spec.nameResolutionRefs[]` selects name-resolution catalog entries by name.

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
    artifactAccess:
      serverRef: default
      redfishVirtualMedia:
        endpointRef: bmc
    nodeSSH:
      keyPairRef: ocp-3node-cluster-admin-ssh-key
  hosts:
    - hostname: master-0
      role: master
      machineRef: ocp-master-0
```

Rules:

- `spec.install.method` defaults to `agent`; only `agent` is currently
  accepted.
- `spec.install.platform.type` accepts `baremetal`, `vsphere`, `none`, or
  `external`.
- An omitted `spec.install.platform` derives from the single `InfraProvider`
  type behind `spec.hosts[].machineRef` →
  `Machine.spec.substrate.providerRef`: `libvirt` and `baremetal` providers
  derive `type: baremetal` with `baremetal.provisioningNetwork: disabled`;
  `vsphere` providers derive `type: vsphere`; `kubevirt` providers derive
  `type: none`. `render effective` materializes the derived platform. When the bound machines span multiple provider types
  and the platform is omitted, validation rejects the cluster naming the
  conflicting providers. An authored platform always wins.
- `spec.install.endpoints` keys are the closed slot vocabulary `api`,
  `api-int`, and `ingress`; any other key is rejected naming the accepted
  set. An omitted `api-int` slot defaults to a copy of the authored `api`
  endpoint (its `address` and `source`); `render effective` materializes the
  copy and an authored `api-int` always wins.
- `endpoints.<slot>.source.type` accepts `openshift` (default), `external`,
  or `infraComponent`. `openshift` and `external` require `address`.
  `infraComponent` requires `source.componentRef` pointing at a
  `loadBalancer` `InfraComponent` and must not set `address`.
  `source.bindAddressRef` names the selected `bindAddresses[]` entry; it may
  be omitted only when the load balancer declares exactly one bind address,
  and a non-empty `source.bindAddressRef` must match a `bindAddresses[].name`
  regardless of bind count.
  - **Single-node clusters** additionally reject `source.type: openshift` on the
    `api`, `api-int`, and `ingress` slots.
  - `source.componentRef` and `source.bindAddressRef` are valid only when
    `source.type: infraComponent`. Every endpoint must set `address`, `dnsName`,
    or `source.type: infraComponent`.
- `spec.hosts[].machineRef` is required and references a `Machine` with
  `openshift-node` capability and `os.provided: false`. No default is derived
  from the `hostname`: hostnames are cluster-local while `Machine` names are
  global, so an implicit same-name binding could silently capture a foreign
  `Machine`.
- Each node hostname must be unique inside the cluster.
- A `Machine` is node-bound by at most one cluster (and at most one host
  entry): `machineRef` entries must be disjoint across every
  `ContainerCluster` and `StorageCluster`.
- Node network input is owned by the referenced `Machine`, not by the cluster
  node entry.
- `spec.distribution.type` accepts `openshift` (default, materialized by
  `render effective`) or `okd`; `openshift` clusters require a pull secret via
  `spec.install.pullSecretRef` or the `Environment` default.
- `spec.networking.clusterNetwork` defaults to one entry
  `{cidr: 10.128.0.0/14, hostPrefix: 23}` and `spec.networking.serviceNetwork`
  defaults to `[172.30.0.0/16]` (the stock openshift-install networks) when
  omitted; `render effective` materializes the defaults. Each list is
  defaulted independently, and an authored list — even a partial one — is
  left untouched.
- `spec.install.mode` accepts `connected` (default) or `disconnected`;
  `spec.install.method` accepts `agent` (default). `disconnected` requires
  `Environment.spec.registries.mirror` (trust bundle plus an external mirror URL
  or a managed registry component).
- `spec.install.additionalTrustBundleRefs[]` are cluster-scoped additional CA
  trust-bundle secret names, merged with fleet-wide
  `Environment.spec.installTrust.caBundleRefs[]`.
- `spec.install.servingCertificates`, when set, supplies cluster serving
  certificates: `apiServer.namedCertificates[]` (each with `names[]` and a
  `secretRef`) and `ingress.defaultCertificateRef`.
- `spec.hosts[].role` accepts `master` or `worker`; a cluster requires at least
  one `master` node.
- `spec.controlPlane.replicas`, when set, must equal the number of `master`
  nodes. `spec.compute[]` declare worker machine pools; their summed `replicas`
  must equal the number of `worker` nodes. A single all-`master` topology omits
  both. `replicas` is the only machine-pool field: the agent installer renders
  a single default-architecture `master`/`worker` pool, so the other
  install-config machine-pool fields (`architecture`, `hyperthreading`,
  `platform`, `name`) are not authorable.
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
- Storage convergence is additive-only across `spec.ceph.config`, `mgrModules[]`,
  `monitoring`, the `services[]` passthrough, and the `StoragePool`/
  `StorageFilesystem`/`StorageObjectGateway` kinds: `apply` creates and converges
  declared objects and never removes a live Ceph object whose declaration was
  deleted; `--override` rebuilds only still-declared pools whose structural
  identity changed, never prunes. Removal is out of band, pending the open
  override/reconcile design.
- Managed Ceph `spec.ceph.distribution` accepts `oss`, `redhat`, or `ibm`;
  omitted means `oss`.
- `spec.ceph.release` selects which Ceph release to install for the chosen
  distribution. For `oss` it is an upstream release name (`squid`, default) or a
  full upstream `x.y.z` version (for example `19.2.1`); a version pins the
  package repository to `rpm-x.y.z` reproducibly. For `redhat` and `ibm` it is
  the product stream (for example `9`, the default), selecting the
  `rhceph-<N>-tools` and `ibm-storage-ceph-<N>` repositories.
- `spec.ceph.image` optionally pins the exact cephadm container image, which
  `cephadm bootstrap` applies as the default image for every Ceph daemon, making
  the running cluster version reproducible. It must pin a version tag or a
  `sha256` digest and must not use a mutable `:latest` tag. For `oss` an `x.y.z`
  `release` derives `quay.io/ceph/ceph:vX.Y.Z` automatically when `image` is
  unset; a release name leaves the daemon image unpinned. `redhat` and `ibm`
  registry tags are not `x.y.z`, so reproducible pins are supplied here
  explicitly.
- `distribution: oss` uses upstream/community Ceph package and image sources
  and must not set `entitlementRef`. Bootwright configures the upstream
  community repository on each node with cephadm. `spec.ceph.community.mirror`
  overrides the `download.ceph.com` base URL. `spec.ceph.community` must be
  empty for `redhat` and `ibm`.
- `distribution: redhat` requires `entitlementRef` to resolve to a Red
  Hat `ceph` entitlement. Red Hat Ceph Storage repositories and registry
  access come from that entitlement and must not mix with upstream Ceph
  packages or images.
- `distribution: ibm` requires `entitlementRef` to resolve to an IBM
  `ibm-storage-ceph` entitlement with accepted license terms. IBM Storage Ceph
  registry access and license acceptance come from that entitlement; the RHEL
  BaseOS/AppStream repos cephadm needs come from the `redhat`/`rhel` entitlement
  it names via `rhelEntitlementRef`. Neither must mix with upstream Ceph packages
  or images.
- `cephadm.addressRef`, when set, selects a named
  `Machine.spec.addresses[]` entry for cephadm traffic.
- `cephadm.bootstrap.host` names a storage topology host. The rendered
  cephadm `--mon-ip` is always an address of this host: the address named by
  `bootstrap.addressRef`, defaulting to `cephadm.addressRef` and finally the
  host machine's SSH address.
- `spec.type` is required and must be `ceph`.
- Managed clusters require `spec.ceph`; external clusters must not set
  `spec.ceph`.
- `spec.ceph.cephadm.bootstrap.host` must name a
  `spec.ceph.topology.hosts[]` entry.
- `spec.ceph.networks.publicCIDRs[]` and `clusterCIDRs[]` must be valid CIDRs.
  They render to the Ceph `public_network`/`cluster_network` configuration
  (seeded at bootstrap, reconciled by `ceph config set` on later applies).
- `spec.ceph.config` declares Ceph configuration database options as
  `config.<section>.<key>: <value>`, rendered as idempotent `ceph config set`
  operations. Sections are `global`, `mon`, `mgr`, `osd`, `mds`, `client`, or
  `<type>.<id>`. Keys removed from the spec are not unset on the cluster (the
  storage-wide additive-only rule above). `public_network` and
  `cluster_network` are owned by `spec.ceph.networks` and rejected here.
- `spec.ceph.mgrModules[]` declares mgr modules, rendered as idempotent
  `ceph mgr module enable` operations. Modules removed from the spec are not
  disabled (additive-only); module settings are declared under `config.mgr`
  (`mgr/<module>/<key>`).
- `spec.ceph.monitoring` declares the cephadm monitoring stack. Absent means
  the cephadm default stack with cephadm's own placement; `enabled: false`
  renders `cephadm bootstrap --skip-monitoring-stack` and forbids per-service
  blocks. The `prometheus`, `grafana`, and `alertmanager` services place by
  the topology roles of the same names exactly like `mon`/`mgr` (narrow with
  `placement.sites`/`hosts`); `nodeExporter` keeps the cephadm all-hosts
  default unless authored. Service knobs render 1:1 into the cephadm service
  spec: `port`, and (prometheus only) `retentionTime`/`retentionSize` as
  `retention_time`/`retention_size`. An authored service must resolve to at
  least one host.
- `spec.ceph.services[]` is the cephadm service-spec passthrough for service
  types Bootwright does not model first-class (`nfs`, `loki`, ...):
  `serviceType`, `serviceID`, `placement`, and `spec` render field for field
  into a `ceph orch apply` document. Types owned by a first-class surface
  (topology roles, monitoring, gateways) are rejected; `placement` requires
  explicit `hosts` or `sites`.
- `spec.ceph.topology.hosts[]` require a `machineRef` to a `ceph-node`
  `Machine` and at least one `roles[]` value from `mon`, `mgr`,
  `osd`, `mds`, `rgw`, `ingress`, `prometheus`, `grafana`, `alertmanager`.
  `site` is required exactly where it has effect — when
  `spec.ceph.topology.stretch` is set (it becomes the cephadm CRUSH location)
  or when any placement narrows by `sites` — and optional otherwise (no
  location is rendered without stretch).
  Optional `labels[]` pass additional free-form cephadm host labels (for
  example `_admin`) through verbatim; roles always become labels.
  `devices[]` is the lean OSD shorthand (literal paths ==
  `osd.dataDevices.paths`); the optional `osd` object is the drivegroup-shaped
  selection mirroring the cephadm OSD spec (`dataDevices`/`dbDevices`/
  `walDevices` with `paths|pathSpecs|all|model|vendor|rotational|size|limit`,
  plus spec-level `filterLogic`, `encrypted`, `tpm2`, `osdsPerDevice`,
  `crushDeviceClass`, `blockDBSize`/`blockWALSize`/`dbSlots`/`walSlots`,
  `dataAllocateFraction`, and the top-level `unmanaged`), mutually exclusive
  with `devices`.
  Both require the `osd` role, and every osd-role host must author one of
  them: OSD device consumption is explicit opt-in, so consuming all available
  devices is the authored `osd: {dataDevices: {all: true}}`, never an
  omission default.
  `hostname` is the rendered cephadm host-spec
  hostname; it defaults to the `machineRef` name and is authored only when the
  Ceph hostname genuinely differs from the Machine name. It is rendered
  verbatim as the cephadm host identity and must equal the host's real OS
  hostname — self-fulfilling for Bootwright-installed machines (the installer
  sets the OS hostname to the `Machine` name), operator-guaranteed for
  `os.provided` machines; a mismatch passes `validate` but fails the storage
  node preflight, which asserts each node's real hostname against the declared
  topology hostname. Hostnames must be
  unique. All host `Machine`s in one `StorageCluster` must share one SSH user
  and `keyRef`. A host `Machine` is node-bound by at most one cluster (and at
  most one host entry) across every `ContainerCluster` and `StorageCluster`.
- Storage placement policies, pools, filesystems, gateways, and exports must
  reference the owning `StorageCluster`.
- `spec.ceph.topology.stretch` enables stretch mode by presence (no `enabled`
  flag):
  - **Required:** `failureDomain` (CRUSH failure domain for the stretch rule)
    and `tiebreaker.host`.
  - **Normalized:** `dataSites` from the topology's non-tiebreaker sites,
    `tiebreaker.site` from the tiebreaker host's `site`, `ruleName` to
    `stretch-rule`. Author `dataSites` only to exclude OSD-only sites the
    derivation would wrongly include.
  - **Replication:** not authorable; policy-less replicated pools always render
    `size: 4` / `minSize: 2` (the two-site stretch requirement); non-4/2 is
    unsupported. Authoring `stretch` on an existing cluster re-rules and resizes
    every policy-less pool on the next apply with no `StoragePool` change;
    `bootwright validate` prints a one-line notice naming the inheriting pools.
  - **Validation (post-normalize):** `dataSites` holds exactly two sites;
    `tiebreaker.site` is distinct from them; each data site holds exactly two
    `mon` hosts and the tiebreaker exactly one; the tiebreaker host is mon-only
    with no OSD `devices`; erasure-coded pools are rejected; MDS, RGW, and
    ingress placement include at least two role-capable hosts per data site.

## StoragePlacementPolicy

`StoragePlacementPolicy` owns reusable Ceph placement and replicated-pool
defaults for the pools that select it.

Rules:

- `spec.storageClusterRef` is required and must reference a managed
  (non-`external`) `StorageCluster`.
- `spec.ceph.ruleName` is required.
- `spec.ceph.failureDomain` and `spec.ceph.replicated.{size,minSize}` are the
  defaults applied to pools that reference the policy.

## StoragePool

`StoragePool` owns one Ceph pool.

Rules:

- `spec.storageClusterRef` is required and must reference a managed
  `StorageCluster`.
- `spec.placementPolicyRef`, when set, must reference a
  `StoragePlacementPolicy` on the same `StorageCluster`. The referenced policy
  owns the pool's replication; `spec.ceph.replicated` must not also be set on
  the pool.
- `spec.ceph.type` accepts `replicated` (default) or `erasure` (the upstream
  `ceph osd pool create` words; the populated arm key equals the type value).
  `replicated` must not set `ceph.erasure`. `erasure` requires
  `ceph.erasure.{dataChunks,codingChunks}` (rendered as the erasure-code
  profile `k=`/`m=`), must not set `ceph.replicated`, and is not allowed on
  stretch-mode clusters. The profile also accepts `plugin`, `technique`,
  `crushDeviceClass`, `crushRoot`, `stripeUnit`, and an opaque `parameters`
  map (rendered with their `erasure-code-profile set` spellings); the whole
  profile is immutable, so every field is part of the pool's structural
  identity. Per-pool steady-state intents — `ceph.autoscale`, `ceph.quota`,
  `ceph.compression` — render as idempotent `ceph osd pool set`/`set-quota`
  ops and are explicitly NOT structural (they reconcile in place).
- `spec.ceph.role`, when set, accepts `rbd`, `cephfs-metadata`, `cephfs-data`,
  or `rgw`. It drives `StorageExport` wiring and infers the pool application
  (`rbd` → `rbd`, `cephfs-*` → `cephfs`, `rgw` → `rgw`);
  `spec.ceph.application` overrides the inference.
- The pool's structural identity is its `type` (and erasure profile): the only
  desired-state change that rebuilds a live pool (data-destroying, `--override`
  only). Replicas, crush rule, and application reconcile in place.
- On stretch-mode clusters, pools inherit the stretch CRUSH rule and the
  fixed stretch replication (`size: 4` / `minSize: 2`) without a placement
  policy; a `placementPolicyRef` is needed only for genuinely divergent
  placement.
- On stretch-mode clusters, `ceph.replicated.size` must be `4` and `minSize`
  must be `2` when set.

## StorageFilesystem

`StorageFilesystem` owns one CephFS filesystem and the pools that back it.

Rules:

- `spec.storageClusterRef` is required and must reference a managed
  `StorageCluster`.
- `spec.cephfs.metadataPoolRef` is required and must reference a
  `StoragePool` on the same `StorageCluster`.
- `spec.cephfs.dataPoolRefs[]` is required; each entry is a plain pool name
  (a single entry becomes the default automatically) or `{name, default}` to
  elect the default data pool on multi-pool filesystems. Each must reference a
  `StoragePool` on the same `StorageCluster`, must differ from the metadata
  pool, and exactly one must be the default.
- `spec.cephfs.mds.placement` defaults to every topology host with the `mds`
  role; `sites`/`hosts` narrow the selection and must resolve to at least one
  `mds`-capable host. On stretch-mode clusters the resolved placement must
  cover at least two MDS-capable hosts per data site.

## StorageObjectGateway

`StorageObjectGateway` owns one RGW service and its ingress endpoints.

Rules:

- `spec.storageClusterRef` is required and must reference a managed
  `StorageCluster`.
- `spec.ceph.serviceID` is required; `spec.ceph.frontendPort` must be in
  `0`–`65535`.
- `spec.public.dnsName` is required and is the storage-owned public S3 endpoint;
  optional `spec.public.scheme` and `spec.public.port` refine it. The gateway
  owns this fact, so a storage-only object store needs no `ContainerCluster`.
- `spec.ceph.placement` defaults to every topology host with the `rgw` role;
  `sites`/`hosts` narrow the selection. On stretch-mode clusters the resolved
  placement must cover at least two per data site.
- `spec.ceph.ingresses[]` require a unique `name`, a storage-owned `address` and
  `prefixLength` for the ingress VIP (optional `virtualInterfaceNetworks[]`,
  rendered verbatim to the cephadm ingress `virtual_interface_networks`), and
  a `placement` that defaults to every `ingress`-role host, narrowed by
  `sites`/`hosts` (per-site VIPs author `placement.sites`).
- Optional `spec.ceph.realm`/`zoneGroup`/`zone` bind the RGW to a named
  multisite realm (rendered as `rgw_realm`/`rgw_zonegroup`/`rgw_zone`); all three
  are set together, and Bootwright creates them and commits the period before the
  service applies. Optional `spec.ceph.config` is a per-RGW
  `ceph config set client.rgw.<serviceID>` map (one owner: a key must not also
  appear in the cluster config map, and `rgw_frontend_port` is reserved).

## StorageNFSExport

`StorageNFSExport` owns one cephadm NFS-Ganesha service and its exports.

Rules:

- `spec.storageClusterRef` is required and must reference a managed
  `StorageCluster`. `spec.ceph.serviceID` is required.
- `spec.ceph.placement` must set `hosts` or `sites` (there is no `nfs` topology
  role). `spec.ceph.ingresses[]` mirror the RGW ingress shape and front
  `nfs.<serviceID>`.
- Each `spec.exports[]` sets a unique `pseudo` path and exactly one FSAL —
  `filesystemRef` (CephFS, same cluster) or `bucket` (RGW). Optional `path`,
  `accessType` (`RW`/`RO`/`NONE`), `squash`, and `clients[]` spell the
  `ceph nfs export create` flags. cephadm auto-provisions the backing `.nfs`
  pool, so no pool/namespace is modeled. Additive-only: a removed export keeps
  running.

## StorageExport

`StorageExport` owns the exported storage surface consumed by a downstream
platform.

Rules:

- `spec.type` is required and must be `dataFoundation` (the type value equals
  the populated arm key).
- `spec.storageClusterRef` is required.
- For managed `StorageCluster`s, `spec.dataFoundation` is required;
  `dataFoundation.rbdPoolRef` and `filesystemRef` are required and must reference
  resources on the same `StorageCluster`; `objectGatewayRef` is optional and
  same-cluster.
- For external `StorageCluster`s, `spec.dataFoundation` must be empty and
  `spec.externalDetails` is required.
- `spec.externalDetails` must set exactly one of `fromSecretRef`, `generated`,
  or `sshExecution`. `fromSecretRef` must be declared in
  `Environment.spec.secrets`.
  `generated` is rejected for external clusters. `sshExecution` requires
  `machineRefs[]` to `ceph-admin` `Machine`s with SSH (for external clusters),
  `exporter.source: boundDataFoundationAddon`, and `config.rbdDataPoolName`;
  `config.format`, when set, must be `json`.

## ClusterAddon

`ClusterAddon` owns one post-install bootstrap component.

Rules:

- `spec.type` is required and must be `olm` or `manifestSet`; the two
  arms are mutually exclusive.
- `olm` requires `spec.olm` and must not set `manifestSet`.
  `olm.namespace.name` is required; `olm.subscription` requires `name`,
  `package`, `channel`, `source`, `sourceNamespace`, and `installPlanApproval`;
  `installPlanApproval` accepts `Automatic` or `Manual`.
- `manifestSet` requires `spec.manifestSet.manifests[]` (at least one) and must
  not set `olm`. Each `manifests[].path` is relative to the `ClusterAddon` file,
  ends in `.yaml`/`.yml`, must stay within the file directory, must not be a
  symlink, and must exist.
- `spec.provides[]` accepts `kubevirt` or `dataFoundation`; declaring any
  `provides` value requires at least one `spec.readiness.checks[]` entry.
- `spec.readiness.timeout` is a Go duration. `spec.readiness.checks[].type`
  accepts `csvSucceeded` (requires `namespace`, `subscription`), `condition`
  (requires `apiVersion`, `kind`, `name`, `condition.{type,status}`), or
  `resourceExists` (requires `apiVersion`, `kind`, `name`).
- `spec.accepts.inputs[]` declare binding-scoped inputs; each schema property
  sets exactly one of `refKind` (a known Bootwright kind) or `secret`. A
  `dataFoundation` `storageExportAttachment` effect requires the schema to declare
  exactly one property, literally named `exportRef`, with
  `refKind: StorageExport` and listed in `required` — the attachment machinery
  reads that exact value name.

## ClusterAddonProfile

`ClusterAddonProfile` owns a reusable, ordered group of add-ons.

Rules:

- A profile must include at least one of `spec.profileRefs[]` or
  `spec.addonRefs[]`.
- `spec.profileRefs[]` must reference `ClusterAddonProfile`s; nesting must be
  acyclic.
- `spec.addonRefs[]` must reference `ClusterAddon`s.

## ClusterAddonBinding

`ClusterAddonBinding` owns the per-cluster binding of add-ons and binding-scoped
input values.

Rules:

- `spec.clusterRef` is required and references a `ContainerCluster`.
- A binding must include at least one of `spec.addonProfileRefs[]` or
  `spec.addons[]`.
- `spec.addonProfileRefs[]` must reference `ClusterAddonProfile`s;
  `spec.addons[].addonRef` must reference a `ClusterAddon`.
- A given `ClusterAddon` may be applied to one `ContainerCluster` only once
  across all bindings.
- `spec.addons[].inputs[]` must be declared by the add-on's
  `spec.accepts.inputs`, have unique names, and satisfy the input schema's
  required values.
- Input values for `refKind` schema properties must name a loaded object of
  that kind; values for `secret` properties must be declared in
  `Environment` `spec.secrets`.

## Rendering Contract

- `install-config.yaml` is rendered from `ContainerCluster`, `Environment`,
  selected machines, selected providers, endpoints, and platform render mode.
- `agent-config.yaml` hosts are rendered from `ContainerCluster.spec.hosts`,
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
- A default consumed by more than one pipeline stage is materialized by the
  normalize phase (for example, an omitted standard container endpoint
  `source.type` becomes `openshift`); validators and renderers read the
  normalized value instead of recomputing the default. A diagnostic on a
  normalize-injected reference the author never wrote (Environment-defaults
  copies, the `openshift-pull-secret` convention,
  `<cluster-name>-cluster-admin-ssh-key`) says the value was defaulted and
  how to override it.
- References must resolve to loaded resources selected by `Environment`.
- Machines must declare `spec.os.provided`.
- Machines with `os.provided: false` must have `spec.substrate.providerRef`.
- Machines that are used over SSH must declare
  `spec.access.ssh.addressRef`, `keyRef`, and a matching address.
  `access.ssh.addressRef` defaults to the address named `ssh` when one exists
  (documented convention; there is no only-address fallback).
- Provider network attachment refs must exist and match the provider arm used
  by the machine.
- A `Machine` is node-bound by at most one cluster across `ContainerCluster`
  `spec.hosts[].machineRef` and `StorageCluster`
  `spec.ceph.topology.hosts[].machineRef`, and by at most one host entry
  within that cluster.
- `ContainerCluster` and `StorageCluster` names share one cluster selection
  namespace: `--clusters` and the `Environment` cluster lists resolve bare
  names against both kinds, so a name is declared by at most one cluster root
  across the two kinds (in addition to the per-kind duplicate rules).
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
- `context init --name <name> -f <dir>` (`--name` and `-f` required, exactly one directory) creates
  a context by copying the whole source directory tree into the context's input
  directory at `/var/lib/bootwright/contexts/<name>/input/`. The context is
  self-contained: every command reads the copy, so the context keeps working
  even if the source is later moved or deleted, and editing the source has no
  effect until the next `context update`. `init` fails if the context already
  exists; `--yes` drops the existing context entirely and recreates it from the
  source. The whole tree is copied (not only YAML) so `file:`-sourced secrets and
  SSH keys, resolved relative to the loaded YAML, remain available.
- `context update --name <name> -f <dir>` (`--name` and `-f` required, exactly one
  directory) replaces the named context's input directory with a fresh copy of the
  source and preserves all other context state (secrets, runs, rendered output,
  clusters, ownership, provider state). It does not change the current-context
  selection.
- An input directory that is missing, unreadable, or not a directory is a named
  failure at context resolution/readiness time that names the context and the
  input directory and points at `context update --name <name> -f` (or `context init
  --name <name> … --yes`) to repopulate it; there is no silent degradation.
- A mutating `apply` records the loaded input YAML files as a forensic output
  under the run's history directory (`runs/history/<run-id>/input/`); a
  mutating `destroy` records them under `runs/last-destroy-input/`. The
  snapshot is what was applied, written at the start of the mutating run;
  nothing reads it back, and plan/`--dry-run` never write it.
- Read-only verbs (`status`, `state-check`, `render`, `plan`, `apply --dry-run`,
  `validate`, help, and discovery) must not write runtime records
  (convergence-safety, install, ownership, ledger) or acquire a mutating run
  lease, and must not mutate provider, BMC, cluster, or storage state. `status`,
  `state-check`, `render`, `plan`, and `validate` must not contact provider
  hosts, BMCs, or clusters at all; `preflight` may run an Ansible preflight but only
  with read-only or check-mode operations that converge nothing. As the one
  exception to that read-only posture, `preflight` (and `apply` before its host
  check) may record SSH server-key trust, but only for a host with no existing
  trust record and only after an explicit interactive per-host fingerprint
  confirmation; never under `--dry-run`, JSON output, or non-interactive
  execution (`--yes`). A changed key is never accepted interactively — it keeps
  failing closed until `bootwright host trust --replace` records it
  deliberately.
- Cluster selection uses one flag name, `--clusters`, on every command that
  narrows to specific roots: `apply`/`plan`/`destroy --stage`, the `preflight`
  scope subcommands, and `render`. It accepts a comma-separated list
  of `ContainerCluster` and `StorageCluster` names; validation keeps the
  names unique across both kinds, so a name selects exactly one cluster
  root. `destroy --stage infra
  --clusters` additionally accepts the literal `artifact-server` to remove only
  the generated artifact publication service.
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
- `destroy` with `--stage` omitted tears down the whole context: it runs the
  clusters teardown then the infra teardown as one ordered task graph (the
  reverse of the apply order), and sweeps context-owned VM artifacts and orphan
  ownership records exactly as unscoped `destroy --stage infra` does. `--clusters`
  with no `--stage` still narrows to `destroy --stage clusters`; the full
  teardown is the no-selector form.
- Destroy must remove host packages only when ownership records prove
  Bootwright installed them and no remaining ownership record on that host
  still requires the package.
- `destroy --stage infra|clusters --override` is required when selected state
  contains `Environment.spec.safety.destroyProtection: requiredOverride`.
  `--yes` only skips the confirmation prompt and never implies `--override`.
- `destroy --force-unowned` relaxes the machine-substrate teardown ownership
  gates only: it tears down a libvirt domain, KubeVirt VirtualMachine, or vSphere
  VM that matches the Bootwright `<cluster>-<machine>` naming but carries a
  missing or mismatched ownership marker — the recovery path when the
  desired-state names changed after the resources were applied. It is orthogonal
  to `--override` (it neither authorizes protected-environment teardown nor is
  authorized by it), does not relax the Ceph cluster or OSD-device ownership
  gates, never relaxes the device data-safety checks (a mounted, in-use, or
  unprobeable device still fails closed), and does not imply `--yes`.
- `apply` reconciles by default: it creates missing objects, skips objects
  whose recorded desired state matches the current desired state, and fails
  closed on drift or foreign ownership before any mutation. `apply
  --expect-new` additionally refuses to proceed when any selected object
  already exists. `--expect-new` and `--override` are mutually exclusive.
  Every selected object is classified independently against the recorded last
  apply by the same classification that powers `state-check`.
- `apply --override` is command-scoped. It may continue past Bootwright-owned
  unsafe mismatch checks that have an explicit override path: it bypasses the
  skip-if-already-complete install check, reinstalls a managed-OS machine (the
  substrate VM is undefined and its disks wiped, then rebuilt), and cleanly
  rebuilds a managed Ceph cluster (`cephadm rm-cluster --zap-osds`), allowed only
  when a Bootwright ownership marker proves the live cluster is the one Bootwright
  created — a foreign or co-resident cluster fails closed. It must not bypass
  active-run leases, validation, secret checks, or foreign-resource ownership
  failures.
- When selected state contains `Environment.spec.safety.destroyProtection:
  requiredOverride`, `apply --override` fails closed before any mutation rather
  than rebuilding protected resources: that destruction must cross the destroy
  authorization boundary, so the operator runs `destroy --override` for the
  affected scope and then re-applies. Dry-run/plan still previews the override
  plan.
- `bootwright host trust` records SSH server-key trust for declared machines.
  It remains the scriptable pre-recording path for automation: non-interactive
  runs never record trust on first use, so pipelines record it with `host
  trust` before `preflight`/`apply`, and only `host trust --replace` may accept
  a changed key.
- `bootwright state-check` is a read-only desired-vs-recorded report. It never
  mutates state, writes convergence records, or runs playbooks, and it accepts
  `--stage`, `--clusters`, and `--output` like the other selection commands. It
  classifies each selected resource against the durable convergence-safety
  evidence recorded by the last apply: `missing` (never applied), `match`
  (applied with the current desired state), `drift` (desired state changed since
  it was applied), or `foreign` (a non-Bootwright owner). It compares desired
  state against that recorded evidence only; it does not probe live hosts, BMCs,
  or clusters, so a change made out of band after a matching apply (a wiped disk,
  an undefined VM, a deleted namespace) is not detected until the next apply
  refreshes the record. A root whose resources are all `missing` is reported as
  one absence; a present root reports only the resources that are not in sync.
  Drift is reported per object: each selected apply task is one reported
  resource, and a managed `StorageCluster`'s pools, filesystems, object
  gateways, and exports are each classified independently against their own
  recorded apply, so the report names the individual pool or export that
  drifted or is not yet applied — the same object granularity `apply` acts on.
  A present `StorageCluster` lists its out-of-sync sub-objects under the cluster
  root, while a never-applied cluster still collapses to one absence. The
  `infrastructure` root aggregates the provider and infra-component host tasks.
  The report names which resource drifted, not which field; run `render
  effective` and diff, or `plan`, to see the exact change. It
  is distinct from `status` (context setup checks, local readiness,
  and next-step spine), `preflight` (Ansible preflight), and `plan`/`apply
  --dry-run` (the intended task graph). `--override` is rejected because
  state-check neither mutates nor suppresses its report. The text report
  summarizes a present root's out-of-sync resources by class (drifted,
  foreign-owned, not-yet-applied) so opposite remediations are never conflated.
  Exit codes let automation gate on drift without parsing the report: `0` when
  the selected state is in sync, `3` when any selected resource is out of sync
  (drift, foreign, or never-applied), `1` on load error, and `2` on usage error.
  Once the context has a recorded apply, the `status` next-step spine surfaces
  `state-check` ahead of `plan`/`apply`.
- Rendered effective state must not include secret bytes.
