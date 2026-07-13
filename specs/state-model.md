# Desired-State Model

Bootwright desired state uses `apiVersion: bootwright.io/v1alpha1` and
twenty-one user-authored kinds. The schema intentionally tracks the inputs
consumed by `openshift-install` for agent installs, Bootwright-managed machine
OS installation, and cephadm for external Ceph storage.

There is no compatibility layer for abandoned kinds or fields. Retired resource
shapes must fail strict decode or validation instead of being translated.

## Kinds

The twenty-one kinds and the fact each owns are listed in `domain.md` (Operating
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
  cluster file never disappears silently. An omitted list selects every cluster.
  An authored but empty list is rejected naming the field: it reads as "select
  nothing" but would otherwise select the whole fleet, so an accidentally-emptied
  (for example templated) list cannot silently widen apply/destroy scope.
- `safety.destroyProtection`, when set, must be `allow` or
  `requiredOverride`. Empty means `allow`. Bootwright never infers protection
  from environment names, context names, labels, or cluster names.
- `safety.protectedKinds`, when set, lists object kinds — `ContainerCluster`,
  `StorageCluster`, or `Machine` — that require `--override` to destroy or to
  destructively rebuild via `apply --override` even when `destroyProtection` is
  `allow`. It is the granular tightening: protect the fleet's Ceph and machines
  without blanket friction on scratch container clusters. An unknown kind fails
  validation naming the object and the allowed set.
- `defaults.install.pullSecretRef` and `defaults.install.nodeSSH` fill omitted
  cluster install values only.
- `defaults.artifactServerRef`, when set, is the fleet-wide default artifact
  server selector. It supplies only `serverRef` for consumer-owned
  `artifactServerEndpoint` fields; every consumer still authors its own
  `endpointRef`.
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
- `proxyFor.bootwright`, `proxyFor.containerClusterInstall`, and
  `proxyFor.machineOSInstall` override, per consumer, which proxy catalog entry
  applies: a name selects that entry, `none` opts the consumer out, and an empty
  slot inherits the entry marked `default: true`. At most one catalog entry may
  be `default`, so one external default proxy routes every consumer with no
  `proxyFor` block. `proxyFor.machineOSInstall` routes the managed-OS (Anaconda)
  install fetch: a boot ISO carries no packages, so Anaconda reaches the install
  tree or the Red Hat CDN over the network during install. Only an external proxy
  entry applies here, because the node installs before any managed proxy component
  could exist — a managed value, or inheriting a managed default, is rejected.
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
- Secret material is no longer an `Environment` field. The former `secrets[]`
  list was removed and promoted to a first-class [`Secret`](#secret) kind; the
  only secret-related `Environment` field is `secretStorage.mode`.
- Entitlements are their own first-class `Entitlement` kind (one object per
  file), not an `Environment` field; the secret material they name is declared
  as [`Secret`](#secret) objects. Each `Entitlement` declares named subscription,
  registry entitlement, and license references for products that need
  vendor-controlled access. `spec.type` is the discriminator, from this set;
  other values are rejected:
  - `redhat-rhel`: a RHEL subscription (RHSM), for the RHEL BaseOS/AppStream
    repos.
  - `redhat-ceph`: a single Red Hat subscription covering both RHEL and the
    `rhceph` tools repo, plus `registry.redhat.io` access.
  - `ibm-storage-ceph`: IBM Storage Ceph product access (registry + license).

  A `redhat-rhel` and a `redhat-ceph` entitlement require `rhsm`
  (`organizationRef`, `activationKeyRef`); `redhat-ceph` also requires
  `registry.credentialsRef`. An `ibm-storage-ceph` entitlement requires
  `registry.credentialsRef`, `license.accept: true`, and `rhelEntitlementRef`
  naming a `redhat-rhel` entitlement for the RHEL subscription it runs on; it
  takes no inline `rhsm` arm. Referenced secret material is declared as
  [`Secret`](#secret) objects.

Authored desired-state YAML uses block-style collections. Do not use
flow-style mapping braces, inline lists, or empty inline maps in examples, e2e
inputs, fixtures, or scaffold output.

Authored input keeps one object per file. Object files are named for their
content — a cluster root as `cluster.yaml`, other objects by their
`metadata.name` — and grouped into directories per the layout in
`docs/advanced/fleets.md`. The loader still accepts multi-document files, but
examples, e2e inputs, fixtures, and `bootwright example init` output keep one
object per file so a name maps to exactly one file.

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
  and BMC access under `spec.hardware.management.bmc` (`address`, `protocol`,
  `credentialsRef`).
- `spec.hardware.management.bmc` governs two distinct TLS legs, each defaulting
  to verify:
  - `bmc.tls.verify` controls the connection Bootwright opens **to** the BMC
    (the Redfish API leg, controller → BMC). It is tri-state; omitted means
    verify. Set `false` only for a lab/self-signed BMC certificate.
  - `bmc.virtualMedia.tls` controls how the BMC handles the **artifact server's**
    certificate when it fetches the boot ISO (BMC → artifact server).
    `verify` (tri-state, default verify) asks the BMC to skip verification
    (best-effort; some firmware ignores it). `importServerCertificate: true`
    uploads the artifact server certificate into the BMC trust store before the
    fetch so a self-signed certificate is accepted, and
    `removeServerCertificateAfterBoot: true` (requires `importServerCertificate`)
    removes it once the ISO is mounted. This virtual-media leg is where
    Bootwright reconciles the artifact server's typically self-signed
    certificate; `security.md` describes the default fetch-window handling these
    fields override.
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

`MachineImage` describes bootable media used by managed OS installation. It
does not declare where Anaconda gets install-time packages; that behavior
belongs to the `MachineInstallProfile` that selects the image.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: MachineImage
metadata:
  name: rhel-94-dvd-iso
spec:
  bootMedia: local-media:rhel-9.4-x86_64-dvd.iso
  checksum: sha256:0000000000000000000000000000000000000000000000000000000000000000
  trustRefs:
    - image-ca
```

Rules:

- `spec.bootMedia` is required and locates the ISO the machine boots over BMC
  virtual media. It accepts `local-media:<filename.iso>`, `file://` absolute
  paths, `http://`, or `https://`.
- `local-media:<filename.iso>` resolves to the root-managed ISO media store
  under `/var/lib/bootwright/media/`. The media key is exactly the stored
  filename; it must be a basename ending in `.iso` and must not contain path
  traversal.
- Normal OpenShift agent ISOs generated by `openshift-install` are generated
  context artifacts, not managed media-store inputs. Future user-supplied
  RHCOS ISO fields may use `local-media:<key>`, but RHCOS rootfs, kernel,
  initramfs, and release image content must not use the ISO media store.
- `checksum`, `trustRefs[]`, and `headersRefs[]` are optional.
- Secret refs point to `Secret` objects by `metadata.name`.

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

- `spec.installer.anaconda` is the only installer backend; its presence is the
  discriminator (there is no `type` field).
- `spec.installer.anaconda.imageRef` references a `MachineImage`. Additional
  install-time package sources are owned by this Anaconda installer block.
- `spec.installer.anaconda.packageSource` is omitted for a full DVD image — the
  DVD carries its own packages, which install offline via `cdrom`. Set it when
  `imageRef` points at a small boot ISO, which carries no packages, to declare
  where Anaconda fetches them during installation. Exactly one arm selects the
  source: `mirror`, `redhatCDN`, or `hostedTree`.
- `packageSource.mirror` installs from an HTTP(S) install tree you host:
  `baseURL` is the primary Anaconda install tree (BaseOS) and `repositories[]`
  become additional Kickstart `repo` entries (e.g. AppStream). Every `baseURL`
  must be `http://` or `https://`.
- `packageSource.redhatCDN` sets `entitlementRef`, which must resolve to a
  `redhat-rhel` `Entitlement`. RHSM organization and activation key secret refs
  are owned by that entitlement.
- `packageSource.hostedTree` sets `fromMedia`, the full DVD Bootwright extracts
  once and serves from the selected managed artifact server. It must reference
  local media (`local-media:` or `file://`, not a URL), must differ from the
  referenced `MachineImage.spec.bootMedia`, and must declare an
  `artifactServerEndpoint.endpointRef` that resolves to an HTTP endpoint.
- `packageSource` affects only the Anaconda installation transaction.
  Bootwright does not render persistent repo files or `repo --install` from it;
  future updates use the installed system's normal Red Hat/RHSM repositories or
  later provisioning roles.
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
- `customizations.localization` owns the installed system's locale: `language`
  (system message locale, default `en_US.UTF-8`), `formats` (regional formatting
  locale for dates/numbers/currency; follows `language` when omitted), `keyboard`
  (default `us`), `timezone` (default `UTC`, hardware clock kept in UTC), and
  `additionalLocales[]`. `language`, `formats`, and `additionalLocales` are
  unioned into the Kickstart `%packages --inst-langs` list, which is
  authoritative over which locales survive on the installed system.
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
- `spec.baremetal.defaults.bmc` supplies fleet-wide BMC defaults inherited by
  every bound `Machine` that omits them. `tls` and `virtualMedia` inherit (a
  machine value wins over the default); `credentialsRef` stays per-machine and
  is not defaulted here, so each `Machine` sets its own BMC credential.
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
    agent:
      redfishVirtualMedia:
        artifactServerEndpoint:
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
  conflicting providers. When a `spec.hosts[].machineRef` does not resolve to a
  `Machine`, the derived-platform requirement is suppressed — the dangling
  `machineRef` is the root cause and is reported at the host, so validation does
  not additionally demand an authored `spec.install.platform.type`. An authored
  platform always wins.
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
- `spec.install.nodeSSH` (and the `Environment` `defaults.install.nodeSSH` that
  fills it when omitted) sets `keyPairRef`, or `publicKeyRef` with an optional
  `privateKeyRef`. `keyPairRef` is mutually exclusive with the other two, and
  an authored `nodeSSH` without `keyPairRef` requires `publicKeyRef`.
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
- `spec.hosts[].role` accepts `master`, `worker`, or `infra`; a cluster
  requires at least one `master` node. `infra` is an authoring-only role:
  OpenShift has no install-time infra role, so an infra host installs as a
  worker and is promoted day-2 with the `node-role.kubernetes.io/infra`
  label, a `NoSchedule` taint, and the infra MachineConfigPool.
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
  overrides the `download.ceph.com` base URL. `spec.ceph.community.checksum`
  optionally pins the fetched cephadm bootstrap binary as `sha256:<hex>`; the
  binary is downloaded and executed as root, so the pin adds a content check on
  top of the HTTPS transport, and when unset the binary is fetched with no
  content pin (the default). `spec.ceph.community` must be empty for `redhat`
  and `ibm`.
- `distribution: redhat` requires `entitlementRef` to resolve to a
  `redhat-ceph` `Entitlement`. Red Hat Ceph Storage repositories and registry
  access come from that entitlement and must not mix with upstream Ceph
  packages or images.
- `distribution: ibm` requires `entitlementRef` to resolve to an
  `ibm-storage-ceph` `Entitlement` with accepted license terms. IBM Storage Ceph
  registry access and license acceptance come from that entitlement; the RHEL
  BaseOS/AppStream repos cephadm needs come from the `redhat-rhel` entitlement
  it names via `rhelEntitlementRef`. Neither must mix with upstream Ceph packages
  or images.
- `cephadm.addressRef`, when set, selects a named
  `Machine.spec.addresses[]` entry for cephadm traffic.
- `cephadm.clusterSSHKeyRef`, when set, names the `sshKeyPair` `Secret` that
  becomes cephadm's own cluster management identity — the key Bootwright
  authorizes on, and cephadm distributes to and uses to reach, every host. It is
  independent of each `Machine`'s `access.ssh.keyRef` (how Bootwright connects to
  run the install phase) and must resolve to a declared `sshKeyPair` `Secret`.
  Omitted, the cluster SSH identity defaults to the first topology host's
  `access.ssh` key, which requires every node to share that one access key.
- `cephadm.clusterSSHUser` is the OS user cephadm manages every host as (`cephadm
  --ssh-user`) and must exist on every topology host. It defaults to `root` when
  `clusterSSHKeyRef` is set, and is ignored — the first host's `access.ssh` user
  is used — when `clusterSSHKeyRef` is omitted.
- `cephadm.bootstrap.host` names a storage topology host. The rendered
  cephadm `--mon-ip` is always an address of this host: the address named by
  `bootstrap.addressRef`, defaulting to `cephadm.addressRef` and finally the
  host machine's SSH address.
- `cephadm.bootstrap.singleHostDefaults: true` renders `cephadm bootstrap
  --single-host-defaults`, setting the CRUSH/replication defaults a single-node
  cluster needs to reach `active+clean`. It is valid only for a one-host,
  non-stretch topology, and is rejected when `spec.ceph.config[global]` also sets
  any of the three defaults the flag owns (`osd_pool_default_size`,
  `osd_pool_default_min_size`, `osd_crush_chooseleaf_type`).
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
  `dataAllocateFraction`, the top-level `unmanaged`, and a `serviceOverrides`
  escape hatch (`extraContainerArgs`/`extraEntrypointArgs`/`networks`/
  `customConfigs`)), mutually exclusive with `devices`.
  Within an `osd` object, `dataDevices` is required; `filterLogic` accepts
  `AND` or `OR`; `tpm2: true` requires `encrypted: true`; `osdsPerDevice`,
  `dbSlots`, and `walSlots` must be non-negative; and `dataAllocateFraction`,
  when set, must be in `(0, 1]`.
  Both require the `osd` role, and every osd-role host must author one of
  them — or be covered by a fleet `spec.ceph.topology.osdDrivegroups[]` entry:
  OSD device consumption is explicit opt-in, so consuming all available
  devices is the authored `osd: {dataDevices: {all: true}}`, never an
  omission default.
  `spec.ceph.topology.osdDrivegroups[]` are fleet OSD specs ({`serviceID`,
  `placement`, `osd`}): one cephadm OSD service spanning the resolved osd-role
  hosts instead of one spec per host. A host is owned by exactly one OSD spec —
  validation rejects a host claimed by both a fleet and a per-host
  `osd`/`devices`, or by two fleets.
  `hostname` is the rendered cephadm host-spec
  hostname; it defaults to the FQDN
  `<machineRef>.<cluster>.<baseDomain>` (falling back to the bare
  `machineRef` name when the `Environment` has no `baseDomain` or the node
  opts out of FQDN naming) and is authored only when the
  Ceph hostname genuinely differs from the default. It is rendered
  verbatim as the cephadm host identity and must equal the host's real OS
  hostname — self-fulfilling for Bootwright-installed machines (the installer
  sets the OS hostname to the same default), operator-guaranteed for
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
  `StoragePool` on the same `StorageCluster`. The metadata pool is part of the
  filesystem's structural identity — Ceph cannot move a live CephFS to a
  different metadata pool — so changing it is a data-destroying, `--override`-only
  recreate (`ceph fs rm` then recreate), never an in-place reconcile.
- `spec.cephfs.dataPoolRefs[]` is required; each entry is a plain pool name
  (a single entry becomes the default automatically) or `{name, default}` to
  elect the default data pool on multi-pool filesystems. Each must reference a
  `StoragePool` on the same `StorageCluster`, must differ from the metadata
  pool, and exactly one must be the default. The default data pool is also part
  of the filesystem's structural identity: changing which data pool is the
  default is the same data-destroying, `--override`-only recreate as the metadata
  pool (Ceph cannot move a live CephFS to a different default data pool in place).
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

- `spec.type` must be `dataFoundation`. For an export of a managed
  `StorageCluster` the populated `spec.dataFoundation` arm supplies it — normalize
  materializes `type: dataFoundation` when omitted. For an export of an external
  `StorageCluster` the `dataFoundation` arm must be empty, so there is no arm to
  derive from and `spec.type: dataFoundation` must be authored explicitly.
- `spec.storageClusterRef` is required.
- For managed `StorageCluster`s, `spec.dataFoundation` is required;
  `dataFoundation.rbdPoolRef` and `filesystemRef` are required and must reference
  resources on the same `StorageCluster`; `objectGatewayRef` is optional and
  same-cluster.
- For external `StorageCluster`s, `spec.dataFoundation` must be empty and
  `spec.externalDetails` is required.
- `spec.externalDetails`, when set, requires `fromSecretRef` (its only arm),
  which must resolve to a declared `Secret` holding the operator-supplied
  external-cluster-details JSON. A managed-`StorageCluster` export may omit
  `externalDetails` entirely: the consuming add-on then produces the payload
  itself — its hook runs the exporter on a Ceph node of the export's cluster
  and captures the JSON as a hook output.

## ClusterAddon

`ClusterAddon` owns one post-install bootstrap component.

Rules:

- `spec.type` is required and must be `olm` or `manifestSet`; the two
  arms are mutually exclusive.
- `olm` requires `spec.olm` and must not set `manifestSet`.
  `olm.namespace.name` is required; optional `olm.namespace.create` and
  `olm.namespace.labels` control namespace creation and labels. Optional
  `olm.operatorGroup` sets the OperatorGroup `name` and `targetNamespaces[]`.
  Optional `olm.catalogSource` ships an operator catalog with the add-on:
  `name` and `image` are required, `displayName`/`publisher`/`pollInterval` are
  optional (`pollInterval` is a Go duration), and `subscription.source` must
  match `catalogSource.name` (normalize defaults it when omitted).
  `olm.subscription` requires `name`, `package`, `channel`, and `source`;
  `sourceNamespace` defaults to `openshift-marketplace` and `installPlanApproval`
  defaults to `Automatic` (both normalize-materialized; `installPlanApproval`
  accepts `Automatic` or `Manual`), and optional `startingCSV` pins the initial
  CSV. Each `olm.customResources[]` entry requires `apiVersion`, `kind`, and
  `metadata.name`; `metadata.namespace` is optional (omitted for cluster-scoped
  resources). Apply installs the shipped CatalogSource first (waiting for its
  registry to report a READY connection), then the
  namespace/OperatorGroup/Subscription, waits for the operator's CSV to reach
  `Succeeded`, then applies the custom resources.
- `manifestSet` requires `spec.manifestSet.manifests[]` (at least one) and must
  not set `olm`. Each `manifests[].path` is relative to the `ClusterAddon` file,
  ends in `.yaml`/`.yml`, must stay within the file directory, must not be a
  symlink, and must exist.
- `spec.provides[]` accepts `kubevirt`, `dataFoundation`, or `nmstate`; declaring
  any `provides` value requires at least one `spec.readiness.checks[]` entry.
- `spec.requires[]` accepts the same vocabulary as `provides[]`. Each requirement
  must be provided by another add-on in the same binding (ordering is resolved per
  binding), and add-ons are applied after the add-ons providing their required
  capabilities (a per-binding stable topological order). Unsatisfied requirements
  and `requires`/`provides` cycles are rejected.
- `spec.readiness.timeout` is a Go duration. `spec.readiness.checks[].type`
  accepts `csvSucceeded` (requires `namespace`, `subscription`), `condition`
  (requires `apiVersion`, `kind`, `name`, `condition.{type,status}`), or
  `resourceExists` (requires `apiVersion`, `kind`, `name`).
- `spec.accepts.inputs[]` declare binding-scoped inputs. Each input has a `name`,
  an optional `required` marker (when `true`, every binding of the add-on must
  supply the input), an optional `schema` (`type`, `required[]`, and
  `properties` keyed by property name, each property setting exactly one of
  `refKind` — a known Bootwright kind — or `secret: true`), and optional
  `effects[]`. Each effect sets a `type`: `storageExportAttachment` or
  `globalPullSecretMerge`.
- A `storageExportAttachment` effect requires `provider: dataFoundation` and a
  schema declaring exactly one property, literally named `exportRef`, with
  `refKind: StorageExport` and listed in `required` — the scope machinery reads
  that exact value name to pull the referenced Ceph cluster into the add-on's
  task state. The attachment itself (the external-details payload and consumer
  manifests) is applied by the add-on's own hooks.
- A `globalPullSecretMerge` effect requires `registry` and `username` (and no
  `provider`), and a schema declaring exactly one property, `secret: true` and
  listed in `required`. Before the add-on's resources apply, the referenced
  secret's value is merged into the bound cluster's global pull secret as the
  `auths[<registry>]` credential.
- `spec.hooks[]` ship add-on-owned imperative integration run at a lifecycle
  point of the add-on apply — an Ansible playbook, templated Kubernetes
  manifests, or both — so the logic travels with the add-on instead of being
  compiled into Bootwright. `playbook`, `rolesPath`, `collectionsPath`, and each
  `manifests[].path` are relative paths resolved against the `ClusterAddon` file
  (the `manifestSet.path` rules); the loader skips the `playbooks/`, `roles/`,
  and `collections/` subtrees as Ansible content.
  - `hooks[].name` is required and unique within the add-on. `hooks[].lifecycle`
    is required: `preApply` (before the operator install), `postOperatorReady`
    (after the operator CSV reaches `Succeeded`, before `olm.customResources`;
    olm add-ons only), or `postReady` (after the readiness checks pass). Hooks
    run in that lifecycle order.
  - A hook ships a `playbook`, `manifests[]`, or both. A manifest-only hook (no
    `playbook`) applies templated manifests from values already available to the
    add-on and ignores `target`/`outputs`; a playbook hook additionally runs
    imperative work against resolved machines and captures outputs.
  - `hooks[].target` selects the machines a playbook runs against and is required
    for a playbook hook. It is a presence union — exactly one of `boundCluster`
    (the bound `ContainerCluster`'s nodes), `fromInput` (an accepted input's
    `refKind` property dereferenced to its object, then mapped to nodes:
    `StorageExport` → its `storageClusterRef` Ceph nodes, `StorageCluster` → its
    Ceph nodes, `ContainerCluster` → its agent nodes, `Machine` → the machine),
    or static `clusters`/`machines` lists. A hook carries no `hostGroups` and can
    never resolve to the controller/localhost. `target.limit` is `firstReachable`
    (default: run against the first machine that answers) or `all` (run against
    every resolved machine).
  - `hooks[].secretRefs[]` name `Secret`s materialized into the hook's scoped
    per-run secrets directory (`bootwright_hook_secrets_dir`) — only the declared
    secrets, never the whole store. `hooks[].extraVars` is a free-form map handed
    to the playbook as one JSON `-e` value.
  - `hooks[].timeout` bounds the playbook run (a Go duration; default `10m`).
    `hooks[].run` is `onChange` (default: skip a hook whose content and resolved
    inputs are unchanged since the last reconcile) or `always`.
    `hooks[].failureMode` is `fail` (default: a failing hook blocks the add-on
    apply) or `continue` (record the failure and proceed); a hook whose manifests
    consume its outputs must be `fail`.
  - `hooks[].outputs[]` declare files the playbook writes under
    `{{ bootwright_hook_outputs_dir }}`, captured after the run: `name` (the
    manifest token), `file` (relative to the outputs directory), optional
    `secret` (persisted `0600` under `clusters/<cluster>/secrets/addons/...` and
    reclaimed from run history; non-secret outputs persist under
    `runtime/addons/...`), and `format` `text` (default) or `json` (validates the
    captured bytes parse as JSON). A declared output the playbook did not write
    fails the hook; `outputs` require a `playbook`.
  - `hooks[].manifests[]` are templated manifests applied to the bound cluster
    (`oc apply --server-side`, in declared order) after the hook succeeds. Each
    entry requires `path`; `reclaimRendered` removes the rendered plaintext once
    applied (for manifests embedding secret outputs). Manifest tokens —
    `{{ cluster }}`, `{{ output <name> }}`, `{{ input <in>.<prop> }}`,
    `{{ secret <name> }}`, and `{{ exportDetails <in>.<prop> }}` (the
    operator-supplied external-cluster-details payload of a referenced
    `StorageExport`) — must each be a whole YAML scalar value.

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
  required values. Every accepted input the add-on marks `required: true` must
  be supplied.
- Input values for `refKind` schema properties must name a loaded object of
  that kind; values for `secret` properties must resolve to a declared `Secret`.

## Native add-on catalog store

Bootwright compiles a built-in catalog of ready-made `ClusterAddon` directories
into the binary. `bootwright add-ons add` registers a catalog release into a
machine-local store at `/var/lib/bootwright/add-ons/<name>/` — one registered
version per add-on name, sibling to `contexts/` and `media/` under the
Bootwright root — and `add-ons delete` removes it.

Rules:

- The store is a fallback resolution source, not a load path of its own. When a
  `ClusterAddonBinding` `spec.addons[].addonRef` or `ClusterAddonProfile`
  `spec.addonRefs[]` names no authored `ClusterAddon` in the loaded input, the
  loader resolves it from the store directory of that name (which loads like any
  authored add-on directory, its `SourcePath` anchoring the shipped
  playbooks/manifests). An authored add-on with the same name always wins.
- `context init`/`update` snapshot each referenced registered add-on into the
  context input tree, after which the in-tree copy resolves the reference and
  the store is not consulted; the context is self-contained, so deleting or
  re-registering a store add-on never changes an existing context.
- A store that is absent or unreadable (a rootless run cannot traverse the
  root-owned Bootwright directory) falls through to the normal
  unresolved-reference validation error, whose remedy names `add-ons add` when
  the built-in catalog ships the referenced name.

## ProvisioningPlaybook

`ProvisioningPlaybook` owns one operator-supplied Ansible playbook run against
machines at a chosen provisioning stage. It is the imperative escape hatch
sibling of `ClusterAddon`: where an add-on applies declarative Kubernetes objects
into an installed cluster, a `ProvisioningPlaybook` injects an operator playbook
(and optional vendored roles/collections) into the provisioning DAG at any of the
five sub-phases, before or after that phase's built-in work.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: ProvisioningPlaybook
metadata:
  name: harden-storage-nodes
spec:
  stage: machines          # fabric | machines | deps | base | add-ons
  timing: after            # before | after (default after)
  target:
    clusters: [nprd-ceph]
  playbook: playbooks/harden.yml
  rolesPath: roles
  collectionsPath: collections
  extraVars:
    tuned_profile: throughput-performance
  secretRefs: [vault-token]
  run: onChange            # onChange (default) | always
  failureMode: fail        # fail (default) | continue
```

Rules:

- `spec.stage` is one of the five sub-phase names (`fabric`, `machines`, `deps`,
  `base`, `add-ons`) — the same vocabulary as `--stage`. `spec.timing` is
  `before` or `after` (default `after`).
- `spec.playbook` is required, a `.yml`/`.yaml` file path relative to the
  `ProvisioningPlaybook` file, contained within its directory (no absolute paths,
  `..`, or symlinks) — the `ClusterAddon` `manifestSet.path` rules. `rolesPath`
  and `collectionsPath` are optional relative directories under the same rules,
  and must not be named `vendor` or `node_modules` (directories `context init`'s
  tree copy skips, so the vendored content would silently vanish).
- Operator Ansible content lives under `playbooks/`, `roles/`, and `collections/`
  directories; the loader skips those subtrees (they are Ansible content, not
  authored Bootwright objects) while `context init` still copies them so
  `ansible-playbook` resolves them at run time. Vendored collections are the
  air-gap-safe delivery; a Galaxy `requirements.yml` install is not supported.
- `spec.target` selects the inventory hosts and must set at least one of
  `clusters` (a `ContainerCluster` → its agent-node group, a `StorageCluster` →
  its storage group), `machines` (a `Machine` → its node inventory host(s)), or
  `hostGroups` (raw inventory group names). These are selection lists, not
  references. A target may not name the bootwright controller / localhost
  (`localhost`, `127.0.0.1`, `bootwright_ocp_hosts`,
  `bootwright_controller_hosts`): a controller-targeted playbook would run
  operator code as root over every context's secrets.
- `spec.provides`/`spec.requires` order playbooks within the same
  `(stage, timing)` bucket: every `requires` must be met by another enabled
  playbook's `provides` in the same bucket, `provides` are unique, and the graph
  is acyclic. `spec.order` tie-breaks within a bucket.
- `spec.secretRefs` must resolve to declared `Secret` objects; the playbook
  reads them from `{{ bootwright_secrets_dir }}/<name>` (never the argv).
- `spec.run` selects re-run behaviour: `onChange` (default) skips a run whose
  declared inputs — spec plus a content digest of the playbook and vendored trees
  — are unchanged since the last reconcile; `always` re-runs every apply.
- `spec.failureMode` is `fail` (default: a failed playbook blocks the anchor
  phase) or `continue` (the failure is recorded and the phase proceeds).
- `spec.enabled` defaults true; `enabled: false` keeps the object but skips it.

A playbook is planned only when its `stage` is in the run's phase set (the
`--stage` filter) and its target resolves to at least one in-scope host (the
`--clusters` filter). An `after` playbook waits for the anchor stage's core tasks
in scope; a `before` playbook gates every anchor-stage core task in scope and
itself lands after the previous stage. Playbooks flow through `apply`, `plan`,
`validate`, and `state-check` on the existing `--stage`/`--clusters` axes; there
is no dedicated CLI verb. Because a playbook is opaque, `state-check` reports it
as `match` (declared inputs unchanged) or `drift` (changed, will re-run) from the
input hash only — it never observes node reality.

## Secret

`Secret` declares one named piece of sensitive material and how Bootwright
obtains it. It is the promoted, first-class form of the removed
`Environment.spec.secrets[]` entries: every `SecretRef` in the fleet resolves to
a `Secret` by `metadata.name`. A `Secret` never carries material bytes in desired
state, so it is safe to commit.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Secret
metadata:
  name: proxy-credentials
spec:
  type: usernamePassword
  source:
    generated:
      username: proxy
```

Rules:

- `spec.type` is required and is one of `opaque`, `token`, `usernamePassword`,
  `dockerConfigJson`, `caBundle`, `tlsCertificate`, or `sshKeyPair`. There is no
  inference. The type fixes the material roles the secret carries, which source
  arms are legal, and the shape of any generated parameters.
- `spec.source` says how the material is obtained. Omitting it (or setting an
  empty block) selects `contextStore`; at most one of `contextStore`, `file`, or
  `generated` may be set.
  - `contextStore` keeps the material only in the per-context AES-256-GCM store,
    populated by `bootwright secret set`/`generate`. It carries no parameters.
  - `file` names operator-owned file(s), scoped by type: single-file types use
    `path`; `tlsCertificate` uses `cert`+`key`; `sshKeyPair` uses `privateKey`
    (+ optional `publicKey`). A file key the type does not consume is rejected.
    Paths resolve against the `Secret`'s own file, or are absolute or `~`-rooted.
    `Environment.spec.secretStorage.mode` governs whether file material is read
    in place (`source`) or copied into the context store (`context`).
  - `generated` has Bootwright mint the material and is legal only for `token`,
    `usernamePassword`, `tlsCertificate`, `caBundle`, and `sshKeyPair`. Its
    parameters are flat and scoped by type: `usernamePassword` takes `username`
    (default `admin`); `tlsCertificate`/`caBundle` take `commonName` (required),
    `dnsNames[]`, `ipAddresses[]`, `validityDays` (self-signed); `sshKeyPair`
    takes `keyType` (default `ed25519`, one of `ed25519`, `rsa`, `ecdsa-p256`,
    `ecdsa-p384`, `ecdsa-p521`) and `comment`; `token` takes `bytes`
    (default `32`). A parameter the type does not consume is rejected.
- Authored input keeps one `Secret` per file, named for its `metadata.name`, in a
  fleet-global `secrets/` grouping or beside `environment.yaml`.

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
- The name `artifact-server` is reserved: no `ContainerCluster` or
  `StorageCluster` may use it, because `destroy --stage infra --clusters
  artifact-server` accepts that literal to remove only the generated artifact
  publication service, and a cluster of that name would make the destructive
  selection ambiguous.
- Container cluster endpoints must resolve to valid addresses or valid
  InfraComponent bind addresses. When the cluster selects a machine network, a
  resolved endpoint address — the direct `address` or the `infraComponent`
  source's bind address — must fall inside a selected `NetworkConfig`
  `machineNetwork[].cidr`; an out-of-network endpoint fails validation naming the
  slot and value.
- A machine's `interfaceAddresses`-resolved install IP must fall inside a
  `machineNetwork[].cidr` of its selected `NetworkConfig`; an address outside
  every machine network fails validation naming the `Machine`, the
  `interfaceAddresses` entry, and the resolved IP.
- Bare-metal boot requires BMC details and artifact access suitable for the
  configured boot method.
- KubeVirt `hostClusterRef` dependencies must be acyclic. A cluster cannot
  host itself directly or indirectly.
- Secret references must resolve to a declared `Secret` object.

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
- A command that mutates the context's desired-state input — `context update`
  replacing the tree, `diff --adopt` editing objects — snapshots the current
  input to `input-history/<seq>-<reason>/` before writing, under a bounded
  retention. Every such mutation goes through one component so the
  snapshot-then-write guarantee holds uniformly and the pre-change input stays
  recoverable.
- Read-only verbs (`status`, `diff`, `render`, `plan`, `apply --dry-run`,
  `validate`, help, and discovery) must not write runtime records
  (convergence-safety, install, ownership, ledger) or acquire a mutating run
  lease, and must not mutate provider, BMC, cluster, or storage state. `status`,
  `render`, `plan`, `validate`, and `diff --recorded` must not contact provider
  hosts, BMCs, or clusters at all; the default live `diff` reads live cluster
  state read-only (managed Ceph seed reads and a container `ClusterVersion`
  check) and still writes no runtime records. `diff --adopt` additionally writes
  desired-state input YAML — folding discovered live state back into the context
  input tree, snapshotting the prior input to history first — but writes no
  runtime records and mutates no provider, BMC, cluster, or storage state.
  `preflight` may run an Ansible preflight but only
  with read-only or check-mode operations that converge nothing. As the one
  exception to that read-only posture, `preflight` (and `apply` before its host
  check) may record SSH server-key trust, but only for a host with no existing
  trust record and only after an explicit interactive per-host fingerprint
  confirmation; never under `--dry-run`, JSON output, or non-interactive
  execution (`--yes`). A changed key is never accepted interactively — it keeps
  failing closed until `bootwright machine trust --replace <machines>` records
  it deliberately.
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
- The two `--stage` families decompose into five ordered sub-phases, and
  `--stage`/`--through` accept a sub-phase name as well as a family name. The
  `infra` family is `fabric` then `machines`; the `clusters` family is `deps`,
  `base`, then `add-ons`:
  - `fabric` converges provider hosts (BMC services) and machine-bound shared
    services (proxy, registry, NTP, boot artifacts, DNS, load balancers).
  - `machines` makes machines exist with an OS: per-cluster substrate,
    instantiation, managed-OS install, machine networks, name resolution, VIPs.
  - `deps` installs per-cluster prerequisites: cephadm on storage nodes; build
    the `openshift-install` agent ISO.
  - `base` brings control planes up: bootstrap Ceph and apply OSDs; boot nodes
    and wait for `openshift-install`.
  - `add-ons` applies declarative cluster add-ons and attaches storage.

  `apply`, `plan`, and `state-check` accept sub-phase `--stage` values.
  `destroy --stage` accepts only the two families (`infra`, `clusters`);
  sub-phases are apply-only and rejected on `destroy`.
- `--through <stage>`, on `apply`, `plan`, and `state-check`, runs every stage
  from the beginning up to and including `<stage>`, cumulatively; `--stage` and
  `--through` are mutually exclusive.
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
  contains `Environment.spec.safety.destroyProtection: requiredOverride`, or when
  the scope-filtered teardown covers an object of a kind listed in
  `Environment.spec.safety.protectedKinds` (the granular gate — a protected kind
  absent from the scope does not require `--override`).
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
- `destroy --skip-unreachable` tolerates powered-off or unreachable nodes during
  teardown: it skips them — their devices are NOT wiped and their local state
  remains — and continues, leaving the cluster partially destroyed. It requires
  `--override`. Storage teardown still fails closed when a cluster's Ceph seed
  host is unreachable, so ownership stays proven before any device wipe.
- `apply` reconciles by default: it creates missing objects, skips objects
  whose recorded desired state matches the current desired state, converges
  drift that is reconcilable in place, and fails closed on structural
  (destructive-identity) drift or foreign ownership before any mutation.
  Reconcilable-in-place drift is drift that converges without a destructive
  rebuild: a StorageCluster OSD-device add; a storage sub-object edit confined
  to `ceph ... set`-reconcilable fields (pool replicas/quota/compression/crush,
  MDS placement, an added data pool) rather than an immutable identity (pool
  type / EC profile, CephFS metadata or default-data pool); a ContainerCluster
  edit confined to day-2-owned intent (node labels/taints, cluster add-ons)
  rather than install-config/agent-config identity; and any drift on a
  reconfigure-only kind (whose re-apply is idempotent and non-destructive).
  `apply --expect-new` additionally refuses to proceed when any selected object
  already exists. `--expect-new` and `--override` are mutually exclusive.
  Every selected object is classified independently against the recorded last
  apply by the same classification that powers `state-check`.
- `apply` is additive for every kind: it never removes, deprovisions, or
  unconfigures a live resource whose declaration was deleted from desired state.
  Deletion is not a plannable apply action; removal crosses the destroy
  authorization boundary (`destroy`) or is performed out of band. The storage
  additive-only rules above are the Ceph instance of this product-wide rule.
- Convergence-safety records are written per object when its apply task completes
  successfully; a failed or interrupted task writes no new record. So an object
  with no prior successful apply is `missing` — meaning "no completed apply is
  recorded", not "nothing exists on the substrate" (a half-provisioned VM whose
  install task failed still reports `missing`) — and a bare `apply` re-runs it,
  while an object with a prior successful record keeps classifying against that
  record.
- A bare `apply` resumes a partially-completed container install from its
  recorded phase: `creating-iso` (or no phase) restarts from the agent ISO;
  `iso-created` skips the ISO and resumes from node boot; `nodes-booted` and
  `waiting` skip the ISO and boot and resume the install wait; `complete` is a
  no-op. The `booting` phase fails closed — node-boot completion is uncertain, so
  Bootwright refuses to reboot without `--override` (which recreates the agent
  ISO and reboots the nodes; no completed cluster is destroyed). An unrecognized
  phase also fails closed.
- `apply --override` is command-scoped. It may continue past Bootwright-owned
  unsafe mismatch checks that have an explicit override path: it bypasses the
  skip-if-already-complete install check, reinstalls a managed-OS machine (the
  substrate VM is undefined and its disks wiped, then rebuilt), and cleanly
  rebuilds a managed Ceph cluster (`cephadm rm-cluster --zap-osds`), allowed only
  when a Bootwright ownership marker proves the live cluster is the one Bootwright
  created — a foreign or co-resident cluster fails closed. It must not bypass
  active-run leases, validation, secret checks, or foreign-resource ownership
  failures.
- `--override`'s consequence depends on the object kind, and this split gates
  destroy protection (below). For the reconfigure-only kinds — provider host
  services, infra-component services, node-config apply, per-host `virtctl`
  provisioning, cluster add-ons, provisioning-playbook re-runs, and
  storage-attachment apply — it is an idempotent, non-destructive re-apply that
  touches no data, OS, or VM. For every
  other kind — a managed-OS or substrate machine (reinstall; disks wiped) and a
  container or storage cluster (reinstall / `cephadm rm-cluster --zap-osds`) — it
  is a destructive rebuild. A kind is destructive unless it is on the
  reconfigure-only allowlist, so a newly added kind fails safe.
- When selected state contains `Environment.spec.safety.destroyProtection:
  requiredOverride`, `apply --override` fails closed before any mutation when the
  drift it would resolve is a destructive rebuild (a machine or cluster), rather
  than rebuilding protected resources: that destruction must cross the destroy
  authorization boundary, so the operator runs `destroy --override` for the
  affected scope and then re-applies. Drift confined to reconfigure-only kinds is
  an in-place re-apply and does not trip the protection gate. `protectedKinds`
  narrows this to specific kinds: on an `allow`-default environment, a destructive
  `apply --override` rebuild of a protected kind still fails closed the same way,
  while an unprotected kind rebuilds. Dry-run/plan still previews the override plan.
- Independent of `destroyProtection`, a destructive `apply --override` rebuild (a
  managed-OS or substrate machine reinstall with disks wiped, or a container/Ceph
  cluster wipe-and-rebuild) requires an explicit data-loss acknowledgment even on an
  unprotected environment, so a mis-scoped `--override` never silently destroys. An
  interactive run confirms it at a distinct data-loss prompt naming the objects; a
  non-interactive run must pass `--allow-destroy`. `--yes` skips the routine apply
  confirmation but never authorizes data loss (mirroring how `--yes` never implies
  `--override`). A reconfigure-only or reconcilable-in-place override touches nothing
  destructive and reaches neither gate.
- `bootwright machine trust` records SSH server-key trust for declared machines.
  It remains the scriptable pre-recording path for automation: non-interactive
  runs never record trust on first use, so pipelines record it with `machine
  trust` before `preflight`/`apply`, and only `machine trust --replace <machines>`
  may accept a changed key.
- `bootwright add-ons list|add|delete` manage the machine-local store of native
  add-ons vended from the binary's built-in catalog (see Native add-on catalog
  store). Registering an add-on only makes it available; binding it to a cluster
  with a `ClusterAddonBinding` is the separate, explicit step.
  - `add-ons list` prints each catalog entry with its available versions and
    default version, and — for a registered entry — the registered version and
    whether the on-disk content is locally modified. `--output json` emits the
    machine-readable report.
  - `add-ons add --name <name>[:<version>]` registers a catalog release into the
    store, replacing any prior version of the same add-on after a `--yes` or
    interactive confirm. `--version` is an alternative to the `<name>:<version>`
    shorthand (the two conflict) and defaults to the entry's default version.
  - `add-ons delete --name <name>[:<version>]` removes a registered add-on after
    a `--yes` or interactive confirm; an inline `<version>` is asserted against
    the registered one, not a selector. It refuses a directory the store did not
    register. Existing contexts keep their snapshotted copy.
- `bootwright diff` compares the selected desired state against the live clusters
  and prints the differences as a git-style diff (`-` desired, `+` real). For each
  managed Ceph `StorageCluster` it discovers live state read-only on the seed —
  hosts, services and their placements, OSD device layout (which physical devices
  back OSDs on each host, from `ceph osd metadata`), CRUSH rules, pools and
  replication, ceph config, mgr modules, and health — and diffs it field by
  field. The `osd-devices` facet compares only hosts that pin explicit plain
  `/dev/<name>` devices, where a real mismatch is genuine drift; a host that
  selects devices by filter/`all` (or a stable `/dev/disk/by-*` alias) is not
  drift — the filter intent is satisfied — but is reported as a reconstruction
  advisory naming the devices it currently consumes, so the operator can pin
  `osd.dataDevices.paths` for a byte-exact rebuild. For each
  `ContainerCluster` it runs a shallow reachability/`ClusterVersion` Available
  check (a container cluster carries no declared quantitative expectation to diff
  deeper against, so this is the meaningful floor). Live discovery is read-only
  and best-effort: an unreachable seed or a never-applied cluster degrades to a
  note, never a fatal error. Under the storage additive-only rule an object on the
  cluster but not declared is reported as `real-only` (an `--adopt` candidate), not
  a deletion. `diff` accepts `--stage`, `--clusters`, and `--output` like the other
  selection commands and rejects `--override` (it neither mutates cluster state nor
  suppresses its report).
- `bootwright diff --recorded` skips all cluster contact and instead produces the
  fast offline desired-vs-recorded report for automation. It compares desired
  state against the convergence-safety evidence recorded by the last apply only;
  it does not probe live hosts, BMCs, or clusters, so a change made out of band
  after a matching apply (a wiped disk, an undefined VM, a deleted namespace) is
  invisible to `--recorded` but is exactly what the default live `diff` surfaces.
  - **Classification vocabulary.** It classifies each selected resource against
    the durable convergence-safety evidence recorded by the last apply: `missing`
    (no completed apply recorded), `match` (applied with the current desired
    state), `drift` (desired state changed since it was applied), or `foreign` (a
    non-Bootwright owner). It additionally reports `undeclared` ("Owned but no
    longer declared") resources: Bootwright ownership records — at `Machine`,
    cluster, `InfraProvider`, and `InfraComponent` granularity — that correlate to
    no object in the FULL desired state (never the `--clusters`-scoped subset),
    i.e. objects deleted from desired state without being destroyed. `undeclared`
    is report-only: it does not affect the exit code, which gates on
    selected-state sync only; reclaim an orphan with `destroy` (see the removal
    lifecycle in `docs/advanced/operations.md`).
  - **Evidence granularity.** A root whose resources are all `missing` is reported
    as one absence; a present root reports only the resources that are not in sync.
    Drift is reported per apply task: each selected apply task is one reported
    resource. Classification granularity is per task, and how finely it isolates a
    single desired-state edit depends on the task's recorded hash scope. Two scopes
    classify at true object granularity: a managed `StorageCluster`'s pools,
    filesystems, object gateways, NFS exports, and exports are each classified
    against their own recorded apply (the report names the individual pool or
    export that drifted or is not yet applied — the same object granularity
    `apply` acts on), and each `infrastructure` host task hashes a host-scoped
    projection so an unrelated edit does not flip it. The ContainerCluster install
    tasks (agent ISO, node boot, install wait, per-machine provisioning) hash the
    cluster-filtered desired state for their full-drift signal but also carry a
    structural projection that excludes day-2-owned intent (cluster add-ons, node
    labels/taints), so an edit confined to that intent is reconcilable-in-place
    drift on the install object rather than a destructive reinstall; a change to
    install-config/agent-config identity still moves the structural hash and stays
    a rebuild. A present `StorageCluster` lists its out-of-sync sub-objects under
    the cluster root, while a never-applied cluster still collapses to one absence.
    The `infrastructure` root aggregates the provider and infra-component host
    tasks.
  - **Display classes.** Any change within a present cluster still reports every
    one of that cluster's install tasks as drift (the display class), not only the
    task that consumes the changed object. The `--recorded` text report summarizes
    a present root's out-of-sync resources by class (drifted, foreign-owned,
    not-yet-applied) so opposite remediations are never conflated. It is distinct
    from `status` (context setup checks, local readiness, and next-step spine),
    `preflight` (Ansible preflight), and `plan`/`apply --dry-run` (the intended
    task graph).
  - **Remediation hints.** The report names which resource drifted, not which
    field, and annotates each drifted resource with whether its drift reconciles
    in place (a day-2 reconfigure, a storage `set-*` edit, an OSD-device add) or
    needs a destructive rebuild, so a safe reconcile is never mistaken for a wipe;
    use `render effective` and diff, or `plan`, to see which object changed, and
    the reconcilable/structural split to tell a day-2 reconcile from a reinstall.
  - **Exit codes.** They let automation gate without parsing the report, for both
    modes: `0` when the selected state is in sync, `3` when it is out of sync (a
    live difference, drift, foreign, degraded, or never-applied), `1` on load
    error, and `2` on usage error.
- `bootwright diff --adopt` folds the discovered live state back into desired-state
  YAML so a subsequent `apply` reproduces the cluster as it actually runs. It is the
  one mutating form of `diff`, requires live discovery (rejected with `--recorded`),
  and writes only inside the context input tree — snapshotting the prior input to
  history first, so the pre-adopt state is recoverable. It edits declared objects in
  place (preserving comments) and creates a one-object file for a pool that exists
  only on the cluster; a difference it cannot safely fold in (a structural pool
  change, host/service/CRUSH topology) is reported as detected-but-not-adopted for a
  deliberate manual edit rather than silently dropped.
  Once the context has a recorded apply, the `status` next-step spine surfaces
  `diff` ahead of `plan`/`apply`. When the last run failed, the spine's
  next step is instead the exact scoped retry command (`apply` with the failed
  run's `--stage`/`--clusters` selection) alongside the failed tasks' log paths
  under `runs/history/<run-id>/`, so an interrupted apply resumes without an
  operator reaching for `--override` or `destroy`.
- Rendered effective state must not include secret bytes.
