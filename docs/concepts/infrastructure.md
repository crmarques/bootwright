---
title: "Infrastructure: providers, components & networking"
description: InfraProvider substrate arms, InfraComponent shared services, and NetworkConfig templates.
---

# Infrastructure: providers, components & networking

Infrastructure objects describe what the substrate can do, the shared services
machines run, and the reusable network templates installers consume. Three kinds
cover this domain:

- **`InfraProvider`** declares a substrate capability — libvirt, bare metal,
  vSphere, or KubeVirt — including machine profiles, provider facts, and named
  network attachments. A provider is selected by machines, not by clusters
  directly: references flow upward from cluster to machine to provider to host.
- **`InfraComponent`** declares one machine-bound shared service: a load
  balancer, artifact server, DNS, NTP, proxy, or mirror registry.
- **`NetworkConfig`** owns reusable machine-network CIDRs, name-resolution
  selections, and the NMState host template merged into each selected machine.

Current apply support covers libvirt machines with emulated Redfish BMCs,
bare-metal machines with Redfish virtual media, vCenter-managed vSphere VMs, and
KubeVirt VMs hosted on an OpenShift Virtualization cluster. (IPMI is not
apply-supported today.) Those substrates can back a complete cloud-platform graph
or a single selected `ContainerCluster` / `StorageCluster` convergence.

These kinds share the common object envelope and the **Required** / **Default**
column convention documented on
[The desired-state model](index.md#object-envelope); this page documents only
each kind's `spec`. This page owns the three kinds and their field tables;
[Networking & load balancing](../advanced/networking.md) owns how they are
assembled into a working network.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: example
spec:
  type: baremetal
  baremetal:
    boot:
      method: external
```

## InfraProvider

`InfraProvider` declares what a substrate can provide. `spec.type` is a
discriminated union: the populated arm key is byte-identical to the `type` value,
and any other arm must be empty.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.type` | Yes | — | `baremetal`, `libvirt`, `vsphere`, or `kubevirt`. |
| `spec.baremetal` | No | — | Bare-metal boot defaults and BMC defaults. Required for `type: baremetal`. |
| `spec.libvirt` | No | — | Libvirt host, URI, BMC emulation, and VM profiles. Required for `type: libvirt`. |
| `spec.vsphere` | No | — | vCenter, failure domains, staging, node networking, and VM profiles. Required for `type: vsphere`. |
| `spec.kubevirt` | No | — | Host cluster or kubeconfig, namespace, storage class, and VM profiles. Required for `type: kubevirt`. |
| `spec.networkAttachments[]` | No | — | Named substrate network attachment capabilities. |

!!! note "Artifact use belongs to consumers"
    `InfraProvider` does not declare artifact access. Artifact servers are
    cataloged on [`Environment`](environment.md#artifact-server-default), and
    consumers such as cluster agent install or Anaconda hosted-tree sources
    declare their own `artifactServerEndpoint`.

!!! note "Arm matches `spec.type`"
    Exactly one provider arm is populated and it must match `spec.type`. Setting
    a different arm (for example `spec.vsphere` when `type: libvirt`) is rejected
    with `spec.<arm> must be empty when type=<type>`.

### Bare Metal

For bare metal the provider carries the substrate-level boot method and default
BMC settings; the per-server hardware facts (NICs, BMC address, boot device) live
on each [`Machine`](machines.md), which selects the provider and its network
attachment.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `baremetal.boot.method` | No | — | Boot method. Free-form string today; `external` is the supported value for Redfish virtual media. |
| `baremetal.defaults.bmc.credentialsRef` | No | — | Default BMC credentials secret. Not inherited by machines — credentials stay per-machine. |
| `baremetal.defaults.bmc.tls.verify` | No | `true` | Default for the bootwright→BMC TLS leg; `false` is a lab opt-out. Inherited by machines that omit `bmc.tls`. |
| `baremetal.defaults.bmc.virtualMedia.tls.{trust,restoreVerificationAfterBoot,removeCertificateAfterBoot}` | No | — | Default for the BMC→artifact-server virtual-media TLS trust strategy. Inherited whole by machines that omit `bmc.virtualMedia`. |

`defaults.bmc` supplies provider-wide BMC defaults: `tls` and `virtualMedia` are
inherited by every Machine that omits them (credentialsRef stays per-machine). An
individual server overrides them through its own
`Machine.spec.hardware.management.bmc`. `tls.verify: false` is a lab posture for
BMCs without trusted TLS — do not treat it as the production default.

A provider plus its physical-server `Machine` companion:

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
        tls:
          verify: false
        virtualMedia:
          tls:
            trust: import-certificate
            removeCertificateAfterBoot: true
  networkAttachments:
    - name: rack1-vlan140-machine
      baremetal:
        vlan: 0
```

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
        # tls + virtualMedia inherited from the provider's defaults.bmc
  addresses:
    - name: ip
      address: 192.168.140.20
```

### Libvirt

Libvirt is the primary apply-supported substrate and the one used by the
[Getting Started](../getting-started/openshift.md) walkthrough. The provider runs
VMs on a libvirt host machine and serves an emulated Redfish BMC so the agent ISO
can be attached as virtual media exactly as on real hardware.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `libvirt.machineRef` | Yes | — | Machine that hosts libvirt; must carry the `libvirt` capability. |
| `libvirt.uri` | Yes | — | Libvirt URI, commonly `qemu:///system`. |
| `libvirt.bmcEmulationDefaults` | Yes | — | Emulated Redfish BMC block; required for libvirt apply (see below). |
| `libvirt.machineProfiles[]` | No | — | VM shape list ([Machine Profiles](#machine-profiles)). |

!!! warning "`bmcEmulationDefaults` is required for libvirt"
    libvirt apply boots machines through an emulated Redfish BMC, so the
    `bmcEmulationDefaults` block is **required** — omitting it fails with
    `.bmcEmulationDefaults is required for current libvirt apply support`.
    Setting `enabled: false` is also rejected (`current libvirt apply requires
    emulated Redfish BMC`), and `auth.credentialsRef` is required whenever
    emulation is enabled.

#### `bmcEmulationDefaults`

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `enabled` | No | `true` | Tri-state pointer. `false` is rejected today (emulation is mandatory). |
| `protocol` | No | `redfish` | Only `redfish` is supported; any other value is rejected. |
| `emulator` | No | `sushy-tools` | Emulator implementation. |
| `bindAddress` | No | `0.0.0.0` | Bind address for the emulated BMC. |
| `port` | No | `8000` | BMC emulation port; must be `1..65535`. |
| `vMediaPort` | No | `port + 1` (i.e. `8001`) | Virtual media service port; must be `1..65535`. |
| `auth.credentialsRef` | No | — | BMC emulation credentials secret. Required whenever emulation is enabled (see the warning above). |
| `disableCertificateVerification` | No | `false` | TLS verification opt-out for the emulated BMC. |

!!! note "Port constraints"
    `port` and `vMediaPort` must differ and each must be in `1..65535`. Their
    effective values (after the `8000` / `port + 1` defaults) are also checked
    for collisions across all libvirt providers in the same context.

!!! warning "Emulated BMC is a lab endpoint on an all-interfaces listener"
    The `sushy-tools` endpoint serves Redfish over cleartext HTTP with mandatory
    basic auth, binds `0.0.0.0` by default (`bindAddress`), and opens its Redfish
    port in the host firewall. Credentials are required but transit in cleartext
    on every interface, so keep the emulated BMC on a trusted management segment
    and do not expose it beyond the lab.

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

### vSphere

vSphere keeps vCenter, datacenter, failure-domain, and topology facts inside the
provider. The vSphere adapter creates VMs through the vCenter API from the
controller — no provider host machine is involved.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `vsphere.vcenters[]` | Yes | — | At least one vCenter. |
| `vsphere.vcenters[].server` | Yes | — | vCenter hostname or address. |
| `vsphere.vcenters[].port` | No | — | vCenter port; must be `0..65535`. |
| `vsphere.vcenters[].datacenters[]` | Yes | — | At least one datacenter on the vCenter. |
| `vsphere.vcenters[].credentialsRef` | Yes | — | vCenter credentials secret. |
| `vsphere.vcenters[].disableCertificateVerification` | No | `false` | Lab-only TLS verification opt-out. |
| `vsphere.failureDomains[]` | Yes | — | At least one failure domain. |
| `vsphere.failureDomains[].name` | Yes | — | Failure-domain name, unique within the provider. |
| `vsphere.failureDomains[].region` | Yes | — | Region label. |
| `vsphere.failureDomains[].zone` | Yes | — | Zone label. |
| `vsphere.failureDomains[].server` | Yes | — | Must match a declared `vcenters[].server`. |
| `vsphere.failureDomains[].topology.datacenter` | Yes | — | Datacenter name. |
| `vsphere.failureDomains[].topology.computeCluster` | Yes | — | Compute cluster path or name. |
| `vsphere.failureDomains[].topology.datastore` | Yes | — | Datastore. |
| `vsphere.failureDomains[].topology.networks[]` | Yes | — | At least one vSphere network. |
| `vsphere.failureDomains[].topology.folder` | No | — | VM folder. |
| `vsphere.failureDomains[].topology.resourcePool` | No | — | Resource pool. |
| `vsphere.nodeNetworking.external.networkSubnetCidr[]` | No | — | External node-networking CIDRs (renders verbatim into install-config). |
| `vsphere.nodeNetworking.internal.networkSubnetCidr[]` | No | — | Internal node-networking CIDRs (renders verbatim into install-config). |
| `vsphere.isoStaging.datastore` | No | failure-domain `topology.datastore` | Datastore for uploaded ISOs. |
| `vsphere.isoStaging.folder` | No | stock folder | Folder for uploaded ISOs. |
| `vsphere.machineProfiles[]` | No | — | VM shape list ([Machine Profiles](#machine-profiles)). |

!!! note "Multi-network failure domains require nodeNetworking"
    When a failure domain declares **more than one** `topology.networks[]` entry,
    `spec.vsphere.nodeNetworking` is required so the installer can disambiguate
    node addressing. A single-network failure domain may omit it. The
    `networkSubnetCidr` key is the upstream openshift-install spelling (note the
    lowercase `Cidr`) and renders into `install-config.yaml` unchanged.

!!! note "`isoStaging` needs at least one field"
    If `vsphere.isoStaging` is present, it must set at least one of
    `{datastore, folder}`; an empty block is rejected. Absent, ISOs stage on the
    machine's failure-domain `topology.datastore` under the stock folder. Cleanup
    removes the uploaded ISO files but cannot remove folders, so empty per-upload
    directories can accumulate — delete the staging folder itself to reclaim them.

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

### KubeVirt

KubeVirt provider profiles create child-cluster VMs on a host OpenShift
Virtualization cluster. Use `hostClusterRef` when the virtualization host is
another Bootwright `ContainerCluster` (Bootwright reads that host's cluster
secrets kubeconfig — do not put kubeconfig bytes in desired state); use
`kubeconfigRef` when the host cluster is external.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `kubevirt.hostClusterRef` | No | — | Bootwright-managed host `ContainerCluster` running OpenShift Virtualization. |
| `kubevirt.kubeconfigRef` | No | — | Secret holding the kubeconfig of an external virtualization cluster. |
| `kubevirt.namespace` | Yes | — | Namespace for child VMs; must be a DNS label. |
| `kubevirt.storageClassRef` | No | — | Storage class used by VM disks. |
| `kubevirt.machineProfiles[]` | No | — | VM shape list ([Machine Profiles](#machine-profiles)). |

!!! note "Set exactly one of `hostClusterRef` or `kubeconfigRef`"
    Setting both, or neither, is rejected. `hostClusterRef` must resolve to a
    declared `ContainerCluster`. A KubeVirt-backed child cluster targeting a
    Bootwright-managed host applies only when that host is installed and bound to
    a `ClusterAddon` advertising `provides: [kubevirt]`; a focused apply must name
    both parent and child in `--clusters`, or run after the parent install and
    KubeVirt add-on are ready. See
    [KubeVirt nested clusters](../advanced/kubevirt.md).

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

### Machine Profiles

`machineProfiles[]` is the shared VM shape across libvirt, vSphere, and KubeVirt
providers — virtual machines select one by name through
`Machine.spec.substrate.profileRef`. Fields a provider's adapter does not consume
are rejected, so the required/applies-to columns differ per arm.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `machineProfiles[].name` | Yes | — | Profile name selected by `Machine.spec.substrate.profileRef`; unique within the provider. |
| `machineProfiles[].cpu` | No | — | vCPU count; must be non-negative, and greater than zero on vSphere. |
| `machineProfiles[].memoryMiB` | No | — | Memory in MiB; must be non-negative, and greater than zero on vSphere. |
| `machineProfiles[].diskGiB` | No | — | Root disk size in GiB; must be non-negative, and greater than zero on vSphere. |
| `machineProfiles[].template` | No | — | vSphere template to clone from; rejected on non-vSphere providers. |
| `machineProfiles[].failureDomainRef` | No | — | vSphere failure-domain name; rejected on non-vSphere providers. See the multiplicity note below. |
| `machineProfiles[].dataDisks[]` | No | — | Additional data disks (libvirt and vSphere); rejected on baremetal and kubevirt. |
| `machineProfiles[].dataDisks[].name` | Yes (per entry) | — | Data disk name. |
| `machineProfiles[].dataDisks[].sizeGiB` | Yes (per entry) | — | Data disk size; must be greater than zero. |
| `machineProfiles[].tpm` | No | — (no TPM) | Presence attaches an emulated TPM 2.0 (libvirt and kubevirt only). See [Emulated TPM](#emulated-tpm). |
| `machineProfiles[].tpm.persistent` | No | `true` | Whether the vTPM keeps its state across VM restarts. KubeVirt only; rejected on libvirt. |

!!! note "vSphere clones from a sized template"
    A vSphere profile whose `cpu`, `memoryMiB` or `diskGiB` is zero or omitted is
    rejected: the clone has no shape to apply. The other providers accept a
    partial profile and fall back to their own defaults.

!!! note "`failureDomainRef` multiplicity"
    On a vSphere provider with **multiple** failure domains, each machine profile
    must set `failureDomainRef`, and it must resolve to a declared
    `failureDomains[].name`. With a single failure domain it may be omitted and
    resolves to that one domain implicitly.

### Emulated TPM

A VM has no TPM unless the hypervisor gives it one, so
[machine disk encryption](machines.md#disk-encryption) and
[TPM-sealed OSDs](storage.md) fail on a virtual substrate whose profile is
silent. `tpm: {}` on the profile fixes that:

```yaml
machineProfiles:
  - name: ceph-node
    cpu: 8
    memoryMiB: 16384
    diskGiB: 120
    tpm: {}
```

- **libvirt** attaches a `tpm-crb` device backed by `swtpm`, which Bootwright
  installs on the hypervisor. State lives in `/var/lib/libvirt/swtpm/<domain-uuid>/`
  and survives a power cycle, but `virsh undefine` deletes it — a VM torn down
  and rebuilt gets a **new** TPM and cannot unseal the old volume. That is
  harmless under Bootwright, whose `destroy` + `apply` reinstalls the OS anyway.
  `tpm.persistent` is rejected here: libvirt has no ephemeral mode to opt out of.
- **kubevirt** sets `devices.tpm.persistent: true`, which needs the
  `VMPersistentState` feature gate and a `vmStateStorageClass` on the host
  cluster; without them the VM will not start. `persistent: false` drops the
  state with the `virt-launcher` pod, so anything sealed to that vTPM is lost on
  the first restart — set it only for a machine that is reinstalled each boot.
- **vsphere** rejects `tpm`. A vTPM there needs a vCenter key provider, EFI
  firmware and a powered-off snapshot-free VM, none of which Bootwright
  configures. Encrypt those nodes out of band, or run them on hardware.
- **baremetal** ignores it — the TPM is a firmware fact. Enable it in the BIOS
  before the install; Bootwright cannot see it until the machine boots.

### Network Attachments

`networkAttachments[]` declares the named substrate networks a machine's
`NetworkConfig` binds to (a `Machine` selects one with
`network.config.attachmentRef`). It is a presence union over the provider arms:
each attachment has a `name` (unique within the provider) and exactly one arm,
which must match the provider's `spec.type` — the parent type already fixes the
kind, so there is no separate discriminator.

| Arm | Field | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `libvirt` | `bridge` | Yes | — | Host bridge name. |
| `baremetal` | `vlan` | No | — | VLAN id; must be `0..4094`. |
| `vsphere` | `portgroup` | Yes | — | Portgroup name. |
| `kubevirt` | `networkRef.apiGroup` | No | `k8s.ovn.org` (with kind) | API group of the network object; pairs with `kind`. |
| `kubevirt` | `networkRef.kind` | No | `ClusterUserDefinedNetwork` | `ClusterUserDefinedNetwork`, `UserDefinedNetwork`, `NetworkAttachmentDefinition`, or any kind. |
| `kubevirt` | `networkRef.name` | Yes | — | Network object name on the host cluster. |
| `kubevirt` | `networkRef.namespace` | No | `spec.kubevirt.namespace` | Selected VM namespace (CUDN) / object namespace (UDN, NAD); must be a DNS label. |

KubeVirt `networkRef` is the API's sole object-form reference because the network
object lives on the host cluster, outside the loaded desired state. It is
UDN/CUDN-first and GVK-typed (mirroring the Kubernetes `TypedObjectReference`
idiom) so it references any network kind without Bootwright encoding that kind's
schema. Bootwright *references* the object; it does not render or own it — author
the CUDN/UDN/NAD and any OVS bridge-mapping policy out of band. See
[References](index.md#references), the
[`baremetal-redfish-multidc-virtualized-odf-ceph`](../advanced/examples.md)
example, [KubeVirt nested clusters](../advanced/kubevirt.md), and
[KubeVirt child networks](../advanced/networking.md#kubevirt-child-networks) for
the localnet topology and the static-IP rule.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: child-kubevirt-network-provider
spec:
  type: kubevirt
  kubevirt:
    hostClusterRef: metal-ocp
    namespace: bootwright-child-ocp
  networkAttachments:
    - name: child-machine-net
      kubevirt:
        networkRef:
          apiGroup: k8s.ovn.org              # optional; defaults with kind
          kind: ClusterUserDefinedNetwork    # default; may be omitted
          name: child-machine-net
          namespace: bootwright-child-ocp    # defaults to spec.kubevirt.namespace
```

## InfraComponent

`InfraComponent` declares one machine-bound shared service. `spec.type` is a
discriminated union: exactly one arm is populated, the arm key equals the `type`
value, and `spec.type` must equal that arm key. Each placed `machineRef` must
resolve to a `Machine` that carries `container-runtime` — the service runs as a
container, so the host needs the runtime, **not** a capability named after the
service. The `ntp`/`chrony` arm is the exception and requires no capability.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.type` | Yes | — | `artifactServer`, `loadBalancer`, `proxy`, `nameResolution`, `ntp`, or `registry`; equals the populated arm key. |
| `spec.artifactServer` | No | — | Artifact publication service. Required for `type: artifactServer`. |
| `spec.loadBalancer` | No | — | HAProxy-backed endpoint VIP service. Required for `type: loadBalancer`. |
| `spec.proxy` | No | — | Forward proxy service. Required for `type: proxy`. |
| `spec.nameResolution` | No | — | DNS service. Required for `type: nameResolution`. |
| `spec.ntp` | No | — | NTP service. Required for `type: ntp`. |
| `spec.registry` | No | — | Mirror registry service. Required for `type: registry`. |

!!! note "`implementation` is a fixed single value per arm"
    Each service arm carries a required `implementation` field that must equal
    the one supported value for that arm — it is a closed value, not an example
    list. Any other value (or empty) is rejected.

    | Arm | Required `implementation` |
    | --- | --- |
    | `loadBalancer` | `haproxy` |
    | `proxy` | `squid` |
    | `nameResolution` | `dnsmasq` |
    | `ntp` | `chrony` |
    | `registry` | `mirror-registry` |

    The artifact server has no `implementation` field.

A managed `InfraComponent` is published to consumers through the
[`Environment.spec.infraComponents`](environment.md#infra-component-catalog)
catalog (a `componentRef` on a `management: managed` entry). Endpoint and
load-balancer wiring is covered in
[Networking & load balancing](../advanced/networking.md).

### Artifact Server

Generated ISO and boot-artifact publication is derived from install requirements
and uses an artifact-server `InfraComponent`. Bootwright serves HTTPS listeners
with a self-signed certificate generated on the host; omit `listeners` to use the
default HTTPS listener on port `8443`.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `artifactServer.machineRef` | Yes | — | Machine that runs the service. |
| `artifactServer.bindAddress` | No | `0.0.0.0` | Bind address; must be a valid IP when set. Normalize injects `0.0.0.0` when omitted. |
| `artifactServer.retention` | No | `persistent` | `persistent` keeps the service until an explicit destroy; `install-only` reclaims it automatically at the end of the apply that finishes installing every cluster it serves, and re-provisions it on demand when a later apply must install or rebuild a node it serves. |
| `artifactServer.tls.minVersion` | No | server default | Lowest TLS version the HTTPS listeners accept (`TLSv1`, `TLSv1.1`, `TLSv1.2`, `TLSv1.3`); renders `ssl_protocols` from it up to `TLSv1.3`. Lower it for a legacy BMC HTTPS client. |
| `artifactServer.tls.ciphers` | No | server default | OpenSSL cipher string rendered verbatim as `ssl_ciphers` (e.g. `DEFAULT:@SECLEVEL=0`). Requires an `https` listener. |
| `artifactServer.listeners[]` | No | one `https` listener on port `8443` | Listener list. When omitted, the normalize phase injects a single `https` listener named `https` on port `8443`; author `listeners[]` to change or add listeners. |
| `artifactServer.listeners[].name` | Yes | — | Listener name; DNS label, unique within the service. |
| `artifactServer.listeners[].protocol` | Yes | — | `http` or `https`. |
| `artifactServer.listeners[].port` | Yes | — | Listener port; `1..65535`, unique within the service. |
| `artifactServer.endpoints[]` | No | — | Named endpoint selectors. |
| `artifactServer.endpoints[].name` | Yes | — | Endpoint selector name; unique within the service. |
| `artifactServer.endpoints[].listenerRef` | Yes | — | Must name a declared `listeners[].name`. |
| `artifactServer.endpoints[].addressRef` | Yes | — | Must resolve to a `Machine.spec.addresses[].name` on the placement machine. |

!!! note "`install-only` retention"
    Three things the row above does not say: a reclaimed server is **not**
    re-provisioned by subsequent no-op applies, so the host stays in sync without
    churn; reclaim is reference-aware, so a server another context still
    references is left in place; and the manual equivalent is
    `bootwright destroy --stage infra --clusters artifact-server`.

!!! warning "BMC reachability for virtual media"
    The artifact server endpoint selected by an
    `redfishVirtualMedia.artifactServerEndpoint.endpointRef` should usually
    resolve to an IP address the BMC network can reach. Many BMCs do not
    reliably resolve DNS aliases, and Bootwright uses the matched address value
    directly in the ISO URL sent to Redfish — controller reachability alone is
    not enough for virtual-media ISO fetches.

!!! note "Legacy BMC HTTPS virtual media"
    `tls.minVersion` and `tls.ciphers` exist for a legacy BMC HTTPS client that
    cannot negotiate the modern TLS the bundled nginx build offers and aborts the
    InsertMedia task. Start broad (`minVersion: TLSv1` with
    `ciphers: "DEFAULT:@SECLEVEL=0"`) and tighten to the weakest profile the BMC
    accepts — this weakens TLS on the management network the BMCs reach. The
    symptom and the surrounding trust decision are in
    [SSH or artifact fetch failures](../troubleshooting.md#ssh-or-artifact-fetch-failures).

### Load Balancer

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `loadBalancer.implementation` | Yes | — | Must equal `haproxy`. |
| `loadBalancer.machineRef` | Yes | — | Machine that runs the service. |
| `loadBalancer.bindAddresses[]` | No | — | VIP bind addresses. |
| `loadBalancer.bindAddresses[].name` | Yes | — | Address selector name. |
| `loadBalancer.bindAddresses[].address` | Yes | — | VIP address. |

### Machine-Bound Services

The `proxy`, `nameResolution`, `ntp`, and `registry` arms share this placement
shape. Each requires its own `implementation` value (see the table above).

| Field | Applies to | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `implementation` | All | Yes | — | Closed value per arm: `squid` / `dnsmasq` / `chrony` / `mirror-registry`. |
| `machineRef` | All | Yes | — | Machine that runs the service. |
| `bindAddress` | All | No | `0.0.0.0` | Bind address; must be a valid IP when set. |
| `port` | All | No | `3128`/`53`/`123`/`5000` (squid/dnsmasq/chrony/mirror-registry) | Service port; must be `0..65535`. |
| `endpoints[]` | All | No | — | Named endpoint selectors. |
| `endpoints[].name` | All | Yes | — | Endpoint selector name; unique within the service. |
| `endpoints[].addressRef` | All | Yes | — | Must resolve to a `Machine.spec.addresses[].name` on the placement machine. |
| `additionalIngressHosts[]` | `nameResolution` | No | — | Extra ingress hostnames to resolve before cluster DNS is ready. |
| `forwarders[]` | `nameResolution` | No | — | Upstream DNS forwarders; each must be a valid IP address. |
| `upstreamSources[]` | `ntp` | No | — | Upstream time sources. |

!!! note "Port and bind-address defaults are normalize-injected"
    `port` and `bindAddress` are authorable fields with defaults the normalize
    phase injects before renderers and validators read them. An omitted `port`
    becomes the per-implementation default — squid `3128`, dnsmasq `53`, chrony
    `123`, mirror-registry `5000` (and the artifact server's HTTPS listener
    `8443`) — and an omitted `bindAddress` becomes `0.0.0.0`. Run
    `render effective` to see the injected values.

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

## NetworkConfig

`NetworkConfig` owns reusable installer network data: the installer
`machineNetwork[]` plus an NMState host template merged into each selected host. A
machine selects a template with `Machine.spec.network.config.networkConfigRef`,
and `Machine.spec.network.config.interfaceAddresses[]` injects per-machine static
addresses into it.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.machineNetwork[]` | Yes | — | At least one entry. |
| `spec.machineNetwork[].cidr` | Yes | — | Machine network CIDR; must parse and be unique within the NetworkConfig. |
| `spec.template.networkConfig` | Yes | — | NMState template merged into each selected host. |
| `spec.nameResolutionRefs[]` | No | — | Names `Environment.spec.infraComponents.nameResolution[]` entries; each ref must match a declared entry and be unique. |

!!! note "Name resolution belongs in `nameResolutionRefs`, not the template"
    `spec.template.networkConfig` must not contain a `nameResolutionRefs` key —
    that is rejected as invalid NMState. Use the dedicated
    `spec.nameResolutionRefs[]` field; its resolved addresses feed the NMState
    `dns-resolver` server list and installer DNS.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata:
  name: sno-bridge
spec:
  machineNetwork:
    - cidr: 192.168.132.0/24
  nameResolutionRefs:
    - default
  template:
    networkConfig:
      interfaces:
        - name: primary
          type: ethernet
          state: up
          ipv4:
            enabled: true
            dhcp: false
          ipv6:
            enabled: false
      routes:
        config:
          - destination: 0.0.0.0/0
            next-hop-address: 192.168.132.1
            next-hop-interface: primary
            table-id: 254
```

## Native mapping

Where the native constructs an operator already knows live on these kinds. See
[conventions](index.md) for how to read the tables.

### Native mapping — nmstate

`NetworkConfig` fronts nmstate directly: the template **is** an nmstate
document, passed through verbatim into the agent installer's
`hosts[].networkConfig`. A managed-OS install lowers only a subset of it into
the kickstart `network` directive — see
[Kickstart network subset](../advanced/managed-os.md#kickstart-network-subset).

| Native key or flag | Bootwright path | Class | What the divergence buys |
| --- | --- | --- | --- |
| the nmstate document (`interfaces`, `routes`, `dns-resolver`, …) | `spec.template.networkConfig` | mirror | — |
| `dns-resolver.config.server` | `spec.nameResolutionRefs[]` | restructured | cross-document reference into the `Environment` name-resolution catalog |
| per-host nmstate additions | `Machine` `spec.network.config.overrides` (deep-merged over the selected template) | mirror | — |
| per-host static addresses | `Machine` `spec.network.config.interfaceAddresses[]` referencing `spec.addresses[]` | restructured | the IP is authored once and shared with SSH and endpoint resolution |

### Native mapping — `install-config.yaml` (vSphere platform)

The vSphere platform keys are install-config's own spellings, relocated onto
the provider so one document serves every cluster on that vCenter.

| Native key or flag | Bootwright path | Class | What the divergence buys |
| --- | --- | --- | --- |
| `platform.vsphere.vcenters[]` (`server`, `port`, `datacenters`) | `spec.vsphere.vcenters[]` (verbatim keys) | relocated | cross-document reference — the provider owns vCenter facts once |
| `platform.vsphere.vcenters[].user` / `.password` | `spec.vsphere.vcenters[].credentialsRef` | renamed | secret `…Ref` indirection |
| `platform.vsphere.failureDomains[]` (`name`, `region`, `zone`, `server`) | `spec.vsphere.failureDomains[]` (verbatim keys) | relocated | cross-document reference |
| `…failureDomains[].topology` (`datacenter`, `computeCluster`, `datastore`, `folder`, `resourcePool`, `networks`) | `spec.vsphere.failureDomains[].topology` (verbatim keys) | relocated | cross-document reference |
| `platform.vsphere.nodeNetworking.{external,internal}.networkSubnetCidr` | `spec.vsphere.nodeNetworking` (verbatim keys, including the lowercase `Cidr`) | relocated | cross-document reference |

The provider's `spec.baremetal.defaults.bmc` block only supplies fleet defaults
for the per-machine BMC fields; the Metal3/Redfish mapping table lives on
[Machines](machines.md#native-mapping).

## Where to go next

- [Networking & load balancing](../advanced/networking.md) for the deep
  networking, endpoint, and load-balancer how-to, including the static-IP rule.
- [KubeVirt nested clusters](../advanced/kubevirt.md) for hosting child clusters
  on OpenShift Virtualization.
- [Disconnected & proxied installs](../advanced/disconnected-proxy.md) for mirror
  registries and proxies.
- [The desired-state model](index.md) for the conventions every field table
  shares.
