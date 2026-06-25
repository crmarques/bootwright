---
title: Providers
description: InfraProvider capability shapes and cluster machine selection.
---

# Providers

`InfraProvider` declares what a substrate can provide. It does not decide
which cluster consumes the capability — selected machines and clusters bind
to a provider, and references always flow upward from cluster to provider to
host.

Current apply support covers libvirt machines with emulated Redfish BMCs,
bare-metal machines with Redfish virtual media, KubeVirt VMs hosted on an
OpenShift Virtualization cluster, and vCenter-managed vSphere VMs. (IPMI is
not apply-supported today.) Those substrates can back a complete cloud
platform graph or a selected `ContainerCluster` or `StorageCluster`
convergence.

## The provider spec at a glance

Every `InfraProvider` sets `spec.type` and the one matching arm:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: <provider-name>
spec:
  type: libvirt        # one of: libvirt | baremetal | vsphere | kubevirt
  libvirt: {}          # the matching arm; its fields are shown below
```

`spec.type` is required and must be one of `libvirt`, `baremetal`, `vsphere`,
or `kubevirt`; any other value (for example `ipmi`) fails validation. The arm
that matches `spec.type` is required, and every other arm must be empty.

!!! note "`artifactAccess` is not authored on `InfraProvider`"
    A `spec.artifactAccess` field exists on the struct but is **rejected** by
    validation on `InfraProvider`. Artifact publication is wired at the
    environment or cluster level instead — see
    [`Environment.spec.defaults.artifactAccess`](../api/environment.md) and
    [`ContainerCluster.spec.install.artifactAccess`](../api/container-cluster.md),
    and the [Artifact publication](#artifact-publication) section below.

## Libvirt

Libvirt is the primary apply-supported substrate and the one used by the
[Getting Started](../getting-started.md) walkthrough. The provider runs VMs on
a libvirt host machine and serves an emulated Redfish BMC so the agent ISO can
be attached as virtual media exactly as on real hardware.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: lab-libvirt-provider
spec:
  type: libvirt
  libvirt:
    machineRef: bastion
    uri: qemu:///system
    bmcEmulationDefaults:
      enabled: true
      auth:
        credentialsRef: bmc-credentials
    machineProfiles:
      - name: sno
        cpu: 8
        memoryMiB: 16384
        diskGiB: 120
  networkAttachments:
    - name: sno-bridge
      libvirt:
        bridge: vbr-cb-sno
```

| Field | Required | Default | Notes |
| --- | --- | --- | --- |
| `machineRef` | Yes | — | Names a `Machine` that must declare the `libvirt` capability. |
| `uri` | Yes | — | libvirt connection URI, e.g. `qemu:///system`. |
| `bmcEmulationDefaults` | Yes | — | Required for current libvirt apply support (see below). |
| `machineProfiles[]` | No | — | Shared VM shapes; machines select one by `profileRef`. |

### `bmcEmulationDefaults`

The emulated BMC block is **required** for current libvirt apply support. It
tunes the Redfish BMC emulation the provider runs for its machines:

| Field | Required | Default | Notes |
| --- | --- | --- | --- |
| `enabled` | No | `true` | `false` is rejected today; current apply requires an emulated Redfish BMC. |
| `protocol` | No | `redfish` | Only `redfish` is implemented; any other value is rejected. |
| `emulator` | No | `sushy-tools` | Emulator implementation. |
| `bindAddress` | No | `0.0.0.0` | Listen address for the emulated BMC. |
| `port` | No | derived | Redfish API port; must differ from `vMediaPort`. |
| `vMediaPort` | No | derived | Virtual-media port; must differ from `port`. |
| `auth.credentialsRef` | Yes (when enabled) | — | Names the BMC `user:password` secret. |
| `disableCertificateVerification` | No | — | Lab-only TLS opt-out; not a production default. |

`auth.credentialsRef` is required whenever emulation is enabled. `port` and
`vMediaPort` must resolve to different values in `1..65535`.

## Bare Metal

For bare metal, the `InfraProvider` carries the substrate-level boot method
and default BMC settings; the per-server hardware facts live on each
`Machine`. The `spec.baremetal` arm is **required** when `type: baremetal`.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: rack1-baremetal-provider
spec:
  type: baremetal
  baremetal:
    boot:
      method: external
    defaults:
      bmc:
        credentialsRef: bmc-credentials
        disableCertificateVerification: true
  networkAttachments:
    - name: rack1-vlan140-machine
      baremetal:
        vlan: 0
```

| Field | Required | Default | Notes |
| --- | --- | --- | --- |
| `boot.method` | No | — | How declared nodes boot, e.g. `external` for Redfish virtual media. |
| `defaults.bmc.credentialsRef` | No | — | Default BMC `user:password` secret for machines on this provider. |
| `defaults.bmc.disableCertificateVerification` | No | — | Default lab-only TLS opt-out for BMCs without trusted TLS. |

`defaults.bmc` supplies provider-wide BMC defaults; an individual server can
override them through its own `Machine.spec.hardware.management.bmc`.

`disableCertificateVerification: true` is a lab posture for BMCs without
trusted TLS. Do not treat it as the production default.

### The Machine inventory companion

The provider is the substrate; the physical-server facts (NICs, BMC address,
boot device) live on each installing `Machine`, which selects the provider and
its network attachment:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: rack1-srv1
spec:
  capabilities:
    - openshift-node
  substrate:
    providerRef: rack1-baremetal-provider
  os:
    provided: false
    install:
      rootDeviceHints:
        deviceName: /dev/sda
  network:
    config:
      networkConfigRef: rack1-vlan140-machine
      interfaceAddresses:
        - interface: eno1
          addressRef: ip
          prefixLength: 24
    interfaceBinding:
      - nicRef: eno1
        interfaceName: eno1
  hardware:
    nics:
      - name: eno1
        macAddress: 00:25:90:5a:10:01
      - name: eno2
        macAddress: 00:25:90:5a:10:02
    boot:
      nicRef: eno1
    management:
      bmc:
        address: redfish-virtualmedia+https://bmc-rack1-srv1.bootwright.test/redfish/v1/Systems/1
        credentialsRef: bmc-credentials
        disableCertificateVerification: true
  addresses:
    - name: ip
      address: 192.168.140.20
```

The per-`Machine` `hardware.management.bmc` block overrides the provider's
`defaults.bmc` for that server. See [Machines and OS](../api/machines.md) for
the full `Machine` reference.

## vSphere

vSphere keeps vCenter, datacenter, failure-domain, and topology facts inside
the provider:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: vsphere-provider
spec:
  type: vsphere
  vsphere:
    vcenters:
      - server: vcenter.example.test
        port: 443
        datacenters:
          - dc1
        credentialsRef: vcenter-credentials
    failureDomains:
      - name: dc1-zone-a
        region: dc1
        zone: zone-a
        server: vcenter.example.test
        topology:
          datacenter: dc1
          computeCluster: /dc1/host/cluster1
          datastore: /dc1/datastore/datastore1
          folder: /dc1/vm/bootwright
          resourcePool: /dc1/host/cluster1/Resources/bootwright
          networks:
            - VM_Network_1
    isoStaging:
      folder: bootwright-vmedia
    machineProfiles:
      - name: vsphere-control-plane
        cpu: 8
        memoryMiB: 22528
        diskGiB: 120
        failureDomainRef: dc1-zone-a
```

| Field | Required | Default | Notes |
| --- | --- | --- | --- |
| `vcenters[]` | Yes | — | At least one vCenter. |
| `vcenters[].server` | Yes | — | vCenter hostname. |
| `vcenters[].port` | No | — | Range-checked `0..65535` when set. |
| `vcenters[].datacenters` | Yes | — | At least one datacenter. |
| `vcenters[].credentialsRef` | Yes | — | `user:password` secret name. |
| `vcenters[].disableCertificateVerification` | No | — | Lab-only TLS opt-out, per vCenter. |
| `failureDomains[]` | Yes | — | At least one failure domain. |
| `failureDomains[].name`/`.region`/`.zone`/`.server` | Yes | — | `server` must equal a declared `vcenters[].server`. |
| `topology.datacenter`/`.computeCluster`/`.datastore`/`.networks` | Yes | — | Placement facts. |
| `topology.folder`/`.resourcePool` | No | — | Optional placement overrides. |
| `nodeNetworking` | Conditional | — | Required when any failure domain declares more than one topology network. |
| `isoStaging` | No | — | When authored, must set at least one of `{datastore, folder}`. |
| `machineProfiles[]` | No | — | Shared VM shapes. |

`machineProfiles[].failureDomainRef` must name a `failureDomains[]` entry, and
every `failureDomains[].server` must equal a declared `vcenters[].server`.
When several failure domains are declared every profile must set
`failureDomainRef`; with exactly one, an empty ref resolves to it. `template`
and `failureDomainRef` are vSphere-only profile fields; profile `dataDisks`
are consumed by the libvirt and vSphere adapters only.

The vSphere adapter creates VMs through the vCenter API from the controller:
no provider host machine is involved. An empty `template` creates a blank VM
(the normal path — both the OpenShift agent ISO and managed RHEL installs
boot from virtual media); a set `template` clones from that vCenter
template. VMs are created with EFI firmware, thin-provisioned disks on a
paravirtual SCSI controller, vmxnet3 NICs with deterministic
manually-assigned MACs, and disk-first boot order so an attached install CD
cannot re-enter the installer once the disk is bootable.

### `nodeNetworking`

When a failure domain declares more than one entry in `topology.networks`,
`spec.vsphere.nodeNetworking` becomes **required** so the installer knows
which subnet backs node addresses:

```yaml
vsphere:
  nodeNetworking:
    external:
      networkSubnetCidr:
        - 192.168.140.0/24
    internal:
      networkSubnetCidr:
        - 192.168.140.0/24
```

`networkSubnetCidr` is the upstream openshift-install `nodeNetworking` key
verbatim — note the lowercase `Cidr`, which deviates from the house CIDR
casing — and renders into `install-config.yaml` unchanged.

### ISO staging

Boot and install ISOs upload to a datastore folder before attach.
`isoStaging` overrides the location: `datastore` defaults to the machine's
failure-domain `topology.datastore`, `folder` defaults to
`bootwright-vmedia`. When authored, `isoStaging` must set at least one of
the two fields. Cleanup removes the uploaded ISO files but the vSphere file
API cannot remove folders, so empty per-upload directories can accumulate
under the staging folder — delete the staging folder itself to reclaim
them.

Topology and staging values authored in openshift-install inventory-path
form (for example `/dc1/host/cluster1`) are reduced to their object names
for the vCenter operations; `folder` keeps its path form.

`vcenters[].credentialsRef` names a `user:password` secret (one line, like
BMC credentials). `vcenters[].disableCertificateVerification: true` opts a
self-signed lab vCenter out of TLS verification — a lab posture, not a
production default; verification stays on unless each vCenter opts out.

## KubeVirt

KubeVirt profiles create child cluster VMs on a host OpenShift Virtualization
cluster:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: child-kubevirt-provider
spec:
  type: kubevirt
  kubevirt:
    hostClusterRef: metal-ocp
    namespace: bootwright-child-ocp
    storageClassRef: lvms-vg1
    machineProfiles:
      - name: child-sno
        cpu: 8
        memoryMiB: 16384
        diskGiB: 120
```

Machines select one of those profiles through
`Machine.spec.substrate.profileRef`.

Use `hostClusterRef` when the virtualization host is another Bootwright
`ContainerCluster`. Bootwright uses the cluster secrets kubeconfig from that
host cluster; do not put kubeconfig bytes in desired state. Use
`kubeconfigRef` when the host cluster is external:

```yaml
kubevirt:
  kubeconfigRef: external-virt-cluster-kubeconfig
  namespace: bootwright-child-ocp
```

| Field | Required | Default | Notes |
| --- | --- | --- | --- |
| `hostClusterRef` | Conditional | — | Exactly one of `hostClusterRef`/`kubeconfigRef`; names a Bootwright `ContainerCluster`. |
| `kubeconfigRef` | Conditional | — | Exactly one of `hostClusterRef`/`kubeconfigRef`; names a kubeconfig secret for an external host. |
| `namespace` | Yes | — | Target namespace; must be a DNS label. |
| `storageClassRef` | No | — | Storage class for child VM disks. |
| `machineProfiles[]` | No | — | Shared VM shapes. |

Exactly one of `hostClusterRef` or `kubeconfigRef` is required. The namespace
is required and the storage class is optional. KubeVirt machines must bind
their selected `NetworkConfig` to a provider `networkAttachments[].kubevirt.networkRef`,
and the full apply graph waits for the host cluster add-on that advertises
`provides: [kubevirt]` before creating child VMs. Focused applies must either
name both parent and child in `--clusters`, or run after the parent install
and KubeVirt add-on are ready.

## Network attachments

`spec.networkAttachments[]` declares the named substrate networks that a
machine's `NetworkConfig` binds to (a `Machine` selects one with
`network.config.attachmentRef`). Each entry has a unique `name` and **exactly
one** arm, and that arm must match the provider's `spec.type` — the parent
type already fixes the kind, so there is no separate discriminator.

```yaml
spec:
  type: libvirt
  networkAttachments:
    - name: sno-bridge
      libvirt:
        bridge: vbr-cb-sno
```

| Arm | Authored under | Field | Notes |
| --- | --- | --- | --- |
| libvirt | `libvirt` | `bridge` | Required; host bridge name. |
| vSphere | `vsphere` | `portgroup` | Required; vCenter portgroup. |
| KubeVirt | `kubevirt` | `networkRef` | GVK-typed object-form reference (see below). |
| bare metal | `baremetal` | `vlan` | Optional integer `0..4094`. |

For KubeVirt, the attachment references a network object on the host cluster by
GVK + identity. It is UDN/CUDN-first: `kind`/`apiGroup` default to
`ClusterUserDefinedNetwork` / `k8s.ovn.org`, the OCP 4.21-preferred secondary
network for OpenShift Virtualization. `UserDefinedNetwork` and
`NetworkAttachmentDefinition` (legacy/foreign) are also accepted.

```yaml
spec:
  networkAttachments:
    - name: child-machine-net
      kubevirt:
        networkRef:
          apiGroup: k8s.ovn.org              # optional; defaults with kind
          kind: ClusterUserDefinedNetwork    # default; may be omitted
          name: child-machine-net
          namespace: bootwright-child-ocp    # defaults to spec.kubevirt.namespace
```

`networkRef` is the API's sole object-form reference: the network object lives
on the host cluster, outside the loaded state, so it is identified by an
external GVK + `{name, namespace}` identity. Every other reference in the API is
a plain name string. Bootwright *references* the object; it does not render or
own it (author the CUDN/UDN/NAD and any OVS bridge-mapping
`NodeNetworkConfigurationPolicy` out of band, e.g. as a `manifestSet` add-on —
see the `baremetal-redfish-multidc-virtualized-odf-ceph` example). In every
case the VM attaches via `multus.networkName: <namespace>/<name>`, because a
(C)UDN's OVN-derived NAD shares the object's name. See
[Networking](networking.md) for the localnet topology and the static-IP rule.

## Machine profiles

`machineProfiles[]` (on the libvirt, vSphere, and KubeVirt arms) are the
shared VM shapes that virtual machines select by name. Each profile sets
`name` and optional `cpu`, `memoryMiB`, and `diskGiB` (all non-negative).
Fields a provider's adapter does not consume are rejected:

- `template` and `failureDomainRef` are **vSphere-only**; a set `template`
  clones from a vCenter template, and `failureDomainRef` must resolve against
  `spec.vsphere.failureDomains[].name`.
- `dataDisks` are provisioned only by the **libvirt and vSphere** adapters and
  are rejected on KubeVirt and bare-metal profiles.

```yaml
machineProfiles:
  - name: storage-node
    cpu: 16
    memoryMiB: 32768
    diskGiB: 120
    dataDisks:
      - name: osd-0
        sizeGiB: 200
      - name: osd-1
        sizeGiB: 200
```

Each `dataDisks[]` entry requires `name`, and `sizeGiB` must be greater than
zero.

## Services

Machine-bound shared services are declared as `InfraComponent` objects:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: proxy
spec:
  type: proxy
  proxy:
    implementation: squid
    machineRef: services-host
    port: 3128
```

Supported authored `InfraComponent` arms are `artifactServer`,
`loadBalancer`, `proxy`, `nameResolution`, `ntp`, and `registry`. See
[Infrastructure](../api/infrastructure.md) for the full `InfraComponent`
reference and [Networking and Load Balancing](networking.md) for endpoint and
load-balancer wiring.

### Artifact publication

Artifact publication is different: generated ISO and boot-artifact publication
is derived from install requirements and uses an environment-bound
`InfraComponent`:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: artifact-server
spec:
  type: artifactServer
  artifactServer:
    machineRef: services-host
    listeners:
      - name: https
        protocol: https
        port: 9443
    endpoints:
      - name: bmc
        listenerRef: https
        addressRef: lab-lan
      - name: cluster
        listenerRef: https
        addressRef: lab-lan
```

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: lab
spec:
  infraComponents:
    artifactServers:
      - name: default
        management: managed
        componentRef: artifact-server
```

```yaml
spec:
  defaults:
    artifactAccess:
      serverRef: default
      redfishVirtualMedia:
        endpointRef: bmc
```

Endpoint names are endpoint selectors; `addressRef` values resolve against the
named addresses on the selected `machineRef`. For `redfishVirtualMedia`, use a
BMC-routable IP address entry in most environments; many BMCs do not reliably
resolve DNS aliases, and Bootwright uses the matched address value directly in
the ISO URL sent to Redfish. `ContainerCluster.spec.install.artifactAccess`
may override the default when one cluster needs a different artifact server or
endpoint. Bootwright serves HTTPS listeners with a self-signed certificate
generated on the host. Omit `listeners` to use the default HTTPS listener on
port `8443`.

!!! warning "BMC reachability for virtual media"
    For real BMCs, the artifact server endpoint selected by
    `artifactAccess.redfishVirtualMedia.endpointRef` should usually resolve to
    an IP address that the BMC network can reach. Controller reachability
    alone is not enough for virtual-media ISO fetches.

!!! note "Extending Bootwright with a new substrate or service"
    Adding a new substrate adapter or shared-service type is a contributor
    task that touches the Go registry, `internal/roles`, and the Ansible role
    families. See [Contributing](../contributing/extending.md) for the
    extension contract; it is not part of operator authoring.
