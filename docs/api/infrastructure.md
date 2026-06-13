---
title: Infrastructure API
description: InfraProvider, InfraComponent, and NetworkConfig fields.
---

# Infrastructure

Infrastructure objects describe substrate capabilities, shared machine-bound
services, and reusable installer network templates. They share the common
object envelope and the **Required** / **Default** column convention documented
in [API Reference](index.md#object-envelope); this page documents only each
kind's `spec`.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: example
spec: {}
```

Every table below — including the sub-tables — reads its two leading columns
together: **Required: Yes** must be authored, while **Required: No** with a
stated default is optional and the normalize phase injects that default before
validators and renderers run. Cross-field rules that the schema enforces appear
as notes, because those are the silent authoring failures this reference exists
to catch.

## InfraProvider

`InfraProvider` declares what a substrate can provide. A provider is selected by
machines, not by clusters directly. `spec.type` is a discriminated union: the
populated arm key is byte-identical to the `type` value, and any other arm must
be empty.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.type` | Yes | — | `baremetal`, `libvirt`, `vsphere`, or `kubevirt`. |
| `spec.baremetal` | For `type: baremetal` | — | Bare-metal boot defaults and BMC defaults. |
| `spec.libvirt` | For `type: libvirt` | — | Libvirt host, URI, BMC emulation, and VM profiles. |
| `spec.vsphere` | For `type: vsphere` | — | vCenter, failure domains, staging, node networking, and VM profiles. |
| `spec.kubevirt` | For `type: kubevirt` | — | Host cluster or kubeconfig, namespace, storage class, and VM profiles. |
| `spec.networkAttachments[]` | No | — | Named substrate network attachment capabilities. |

!!! warning "`spec.artifactAccess` is rejected on InfraProvider"
    The struct still carries an `artifactAccess` field, but setting it on an
    `InfraProvider` fails validation:
    `spec.artifactAccess is not valid on InfraProvider; use
    Environment.spec.defaults.artifactAccess or
    ContainerCluster.spec.install.artifactAccess`. Author artifact access on
    [`Environment`](environment.md) or [`ContainerCluster`](container-cluster.md)
    instead.

!!! note "Arm matches `spec.type`"
    Exactly one provider arm is populated and it must match `spec.type`. Setting
    a different arm (for example `spec.vsphere` when `type: libvirt`) is rejected
    with `spec.<arm> must be empty when type=<type>`.

### Bare Metal

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `baremetal.boot.method` | No | — | Boot method. Free-form string today; `external` is the supported value. |
| `baremetal.defaults.bmc.credentialsRef` | No | — | Default BMC credentials secret, inherited by machines that omit their own. |
| `baremetal.defaults.bmc.disableCertificateVerification` | No | `false` | Lab-only BMC TLS verification opt-out for the control-node-to-BMC leg. |

### Libvirt

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
| `auth.credentialsRef` | Required when enabled | — | BMC emulation credentials secret. |
| `disableCertificateVerification` | No | `false` | TLS verification opt-out for the emulated BMC. |

!!! note "Port constraints"
    `port` and `vMediaPort` must differ and each must be in `1..65535`. Their
    effective values (after the `8000` / `port + 1` defaults) are also checked
    for collisions across all libvirt providers in the same context.

### vSphere

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
    node addressing. A single-network failure domain may omit it.

!!! note "`isoStaging` needs at least one field"
    If `vsphere.isoStaging` is present, it must set at least one of
    `{datastore, folder}`; an empty block is rejected. Absent, ISOs stage on the
    machine's failure-domain `topology.datastore` under the stock folder.

### KubeVirt

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `kubevirt.hostClusterRef` | Exactly one of these two | — | Bootwright-managed host `ContainerCluster` running OpenShift Virtualization. |
| `kubevirt.kubeconfigRef` | Exactly one of these two | — | Secret holding the kubeconfig of an external virtualization cluster. |
| `kubevirt.namespace` | Yes | — | Namespace for child VMs; must be a DNS label. |
| `kubevirt.storageClassRef` | No | — | Storage class used by VM disks. |
| `kubevirt.machineProfiles[]` | No | — | VM shape list ([Machine Profiles](#machine-profiles)). |

!!! note "Set exactly one of `hostClusterRef` or `kubeconfigRef`"
    Setting both, or neither, is rejected. `hostClusterRef` must resolve to a
    declared `ContainerCluster`. A KubeVirt-backed child cluster targeting a
    Bootwright-managed host applies only when that host is installed and bound to
    a `ClusterAddon` advertising `provides: [kubevirt]`; see
    [Providers](../advanced/providers.md).

### Machine Profiles

`machineProfiles[]` is the shared VM shape across libvirt, vSphere, and KubeVirt
providers. Fields a provider's adapter does not consume are rejected, so the
required/applies-to columns differ per arm.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `machineProfiles[].name` | Yes | — | Profile name selected by `Machine.spec.substrate.profileRef`; unique within the provider. |
| `machineProfiles[].cpu` | No | — | vCPU count; must be non-negative. |
| `machineProfiles[].memoryMiB` | No | — | Memory in MiB; must be non-negative. |
| `machineProfiles[].diskGiB` | No | — | Root disk size in GiB; must be non-negative. |
| `machineProfiles[].template` | No (vSphere only) | — | vSphere template to clone from; rejected on non-vSphere providers. |
| `machineProfiles[].failureDomainRef` | See note (vSphere only) | — | vSphere failure-domain name; rejected on non-vSphere providers. |
| `machineProfiles[].dataDisks[]` | No (libvirt and vSphere only) | — | Additional data disks; rejected on baremetal and kubevirt. |
| `machineProfiles[].dataDisks[].name` | Yes | — | Data disk name. |
| `machineProfiles[].dataDisks[].sizeGiB` | Yes | — | Data disk size; must be greater than zero. |

!!! note "`failureDomainRef` multiplicity"
    On a vSphere provider with **multiple** failure domains, each machine profile
    must set `failureDomainRef`, and it must resolve to a declared
    `failureDomains[].name`. With a single failure domain it may be omitted and
    resolves to that one domain implicitly.

### Network Attachments

`networkAttachments[]` is a presence union over the provider arms: each
attachment has a `name` (unique within the provider) and exactly one arm, which
must match the provider's `spec.type`.

| Arm | Field | Required | Default | Description |
| --- | --- | --- | --- | --- |
| `libvirt` | `bridge` | Yes | — | Host bridge name. |
| `baremetal` | `vlan` | No | — | VLAN id; must be `0..4094`. |
| `vsphere` | `portgroup` | Yes | — | Portgroup name. |
| `kubevirt` | `nadRef.name` | Yes | — | NetworkAttachmentDefinition name on the host cluster. |
| `kubevirt` | `nadRef.namespace` | Yes | — | NetworkAttachmentDefinition namespace; must be a DNS label. |

KubeVirt `nadRef` is the API's sole object-form reference because the NAD lives
on the host cluster, outside the loaded desired state. See
[References](index.md#references).

## InfraComponent

`InfraComponent` declares one machine-bound shared service. `spec.type` is a
discriminated union: exactly one arm is populated, the arm key equals the `type`
value, and `spec.type` must equal that arm key.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.type` | Yes | — | `artifactServer`, `loadBalancer`, `proxy`, `nameResolution`, `ntp`, or `registry`; equals the populated arm key. |
| `spec.artifactServer` | For `type: artifactServer` | — | Artifact publication service. |
| `spec.loadBalancer` | For `type: loadBalancer` | — | HAProxy-backed endpoint VIP service. |
| `spec.proxy` | For `type: proxy` | — | Forward proxy service. |
| `spec.nameResolution` | For `type: nameResolution` | — | DNS service. |
| `spec.ntp` | For `type: ntp` | — | NTP service. |
| `spec.registry` | For `type: registry` | — | Mirror registry service. |

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

    The artifact server has no `implementation` field. `machineRef` on every arm
    must resolve to a `Machine` and is checked for the service's required
    capability.

### Artifact Server

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `artifactServer.machineRef` | Yes | — | Machine that runs the service. |
| `artifactServer.bindAddress` | No | — | Bind address; must be a valid IP when set. |
| `artifactServer.listeners[]` | Yes | — | At least one listener. |
| `artifactServer.listeners[].name` | Yes | — | Listener name; DNS label, unique within the service. |
| `artifactServer.listeners[].protocol` | Yes | — | `http` or `https`. |
| `artifactServer.listeners[].port` | Yes | — | Listener port; `1..65535`, unique within the service. |
| `artifactServer.endpoints[]` | No | — | Named endpoint selectors. |
| `artifactServer.endpoints[].name` | Yes | — | Endpoint selector name; unique within the service. |
| `artifactServer.endpoints[].listenerRef` | Yes | — | Must name a declared `listeners[].name`. |
| `artifactServer.endpoints[].addressRef` | Yes | — | Must resolve to a `Machine.spec.addresses[].name` on the placement machine. |

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
| `bindAddress` | All | No | — | Bind address; must be a valid IP when set. |
| `port` | All | No | — | Service port; must be `0..65535`. |
| `endpoints[]` | All | No | — | Named endpoint selectors. |
| `endpoints[].name` | All | Yes | — | Endpoint selector name; unique within the service. |
| `endpoints[].addressRef` | All | Yes | — | Must resolve to a `Machine.spec.addresses[].name` on the placement machine. |
| `additionalIngressHosts[]` | `nameResolution` | No | — | Extra ingress hostnames to resolve before cluster DNS is ready. |
| `forwarders[]` | `nameResolution` | No | — | Upstream DNS forwarders; each must be a valid IP address. |
| `upstreamSources[]` | `ntp` | No | — | Upstream time sources. |

!!! note "Listening surface defaults live on the renderer"
    Providers and components declare *where* and *how* a service runs; the
    renderer owns the default listening surface (for example the squid `3128`,
    DNS `53`, NTP `123`, mirror-registry `5000`, and artifact HTTPS `8443`
    ports) when a `port` is omitted. These defaults are not authored fields here.

## NetworkConfig

`NetworkConfig` owns reusable installer network data: the installer
`machineNetwork[]` plus an NMState host template merged into each selected host.

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

`Machine.spec.network.config.interfaceAddresses[]` injects per-machine static
addresses into the selected template.
