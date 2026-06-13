---
title: Infrastructure API
description: InfraProvider, InfraComponent, and NetworkConfig fields.
---

# Infrastructure

Infrastructure objects describe substrate capabilities, shared machine-bound
services, and reusable installer network templates.

## InfraProvider

`InfraProvider` declares what a substrate can provide. A provider is selected by
machines, not by clusters directly.

| Field | Required | Description |
| --- | --- | --- |
| `spec.type` | Yes | `baremetal`, `libvirt`, `vsphere`, or `kubevirt`. |
| `spec.baremetal` | For `type: baremetal` | Bare-metal boot defaults and BMC defaults. |
| `spec.libvirt` | For `type: libvirt` | Libvirt host, URI, BMC emulation, and VM profiles. |
| `spec.vsphere` | For `type: vsphere` | vCenter, failure domains, staging, node networking, and VM profiles. |
| `spec.kubevirt` | For `type: kubevirt` | Host cluster or kubeconfig, namespace, storage class, and VM profiles. |
| `spec.artifactAccess` | No | Provider-level artifact server defaults. |
| `spec.networkAttachments[]` | No | Named substrate network attachment capabilities. |

### Bare Metal

| Field | Description |
| --- | --- |
| `baremetal.boot.method` | Boot method; current examples use `external`. |
| `baremetal.defaults.bmc.credentialsRef` | Default BMC credentials secret. |
| `baremetal.defaults.bmc.disableCertificateVerification` | Lab-only BMC TLS verification opt-out. |

### Libvirt

| Field | Description |
| --- | --- |
| `libvirt.machineRef` | Machine that hosts libvirt. |
| `libvirt.uri` | Libvirt URI, commonly `qemu:///system`. |
| `libvirt.bmcEmulationDefaults.enabled` | Defaults to true when the block is omitted. |
| `libvirt.bmcEmulationDefaults.protocol` | BMC emulation protocol. |
| `libvirt.bmcEmulationDefaults.emulator` | Emulator implementation. |
| `libvirt.bmcEmulationDefaults.bindAddress` | Bind address for BMC emulation. |
| `libvirt.bmcEmulationDefaults.port` | BMC emulation port. |
| `libvirt.bmcEmulationDefaults.vMediaPort` | Virtual media service port. |
| `libvirt.bmcEmulationDefaults.auth.credentialsRef` | BMC emulation credentials. |
| `libvirt.bmcEmulationDefaults.disableCertificateVerification` | TLS verification opt-out for emulation. |
| `libvirt.machineProfiles[]` | VM shape list. |

### vSphere

| Field | Description |
| --- | --- |
| `vsphere.vcenters[].server` | vCenter hostname or address. |
| `vsphere.vcenters[].port` | vCenter port; usually `443`. |
| `vsphere.vcenters[].datacenters[]` | Datacenters available on the vCenter. |
| `vsphere.vcenters[].credentialsRef` | vCenter credentials secret. |
| `vsphere.vcenters[].disableCertificateVerification` | Lab-only TLS verification opt-out. |
| `vsphere.failureDomains[].name` | Failure-domain name. |
| `vsphere.failureDomains[].region` | Optional region. |
| `vsphere.failureDomains[].zone` | Optional zone. |
| `vsphere.failureDomains[].server` | Must match a declared vCenter server. |
| `vsphere.failureDomains[].topology.datacenter` | Datacenter name. |
| `vsphere.failureDomains[].topology.computeCluster` | Compute cluster path or name. |
| `vsphere.failureDomains[].topology.datastore` | Datastore. |
| `vsphere.failureDomains[].topology.folder` | VM folder. |
| `vsphere.failureDomains[].topology.resourcePool` | Resource pool. |
| `vsphere.failureDomains[].topology.networks[]` | vSphere networks. |
| `vsphere.nodeNetworking.external.networkSubnetCidr[]` | External node-networking CIDRs. |
| `vsphere.nodeNetworking.internal.networkSubnetCidr[]` | Internal node-networking CIDRs. |
| `vsphere.isoStaging.datastore` | Datastore for uploaded ISOs. |
| `vsphere.isoStaging.folder` | Folder for uploaded ISOs. |
| `vsphere.machineProfiles[]` | VM shape list. |

### KubeVirt

| Field | Description |
| --- | --- |
| `kubevirt.hostClusterRef` | Bootwright-managed host `ContainerCluster`. |
| `kubevirt.kubeconfigRef` | Secret for external virtualization cluster kubeconfig. |
| `kubevirt.namespace` | Namespace for child VMs. |
| `kubevirt.storageClassRef` | Storage class used by VM disks. |
| `kubevirt.machineProfiles[]` | VM shape list. |

Set exactly one of `hostClusterRef` or `kubeconfigRef`.

### Machine Profiles

| Field | Description |
| --- | --- |
| `machineProfiles[].name` | Profile name selected by `Machine.spec.substrate.profileRef`. |
| `machineProfiles[].cpu` | vCPU count. |
| `machineProfiles[].memoryMiB` | Memory in MiB. |
| `machineProfiles[].diskGiB` | Root disk size in GiB. |
| `machineProfiles[].template` | vSphere template name; vSphere only. |
| `machineProfiles[].failureDomainRef` | vSphere failure-domain name; vSphere only. |
| `machineProfiles[].dataDisks[].name` | Additional data disk name. |
| `machineProfiles[].dataDisks[].sizeGiB` | Additional data disk size. |

### Network Attachments

Each attachment has `name` and exactly one arm matching the provider type:

| Arm | Fields |
| --- | --- |
| `libvirt` | `bridge` |
| `baremetal` | `vlan` |
| `vsphere` | `portgroup` |
| `kubevirt` | `nadRef.name`, `nadRef.namespace` |

KubeVirt `nadRef` is the API's object-form reference because the NAD lives on
the host cluster, outside the loaded desired state.

## InfraComponent

`InfraComponent` declares one machine-bound shared service.

| Field | Required | Description |
| --- | --- | --- |
| `spec.type` | Yes | `artifactServer`, `loadBalancer`, `proxy`, `nameResolution`, `ntp`, or `registry`. |
| `spec.artifactServer` | For `type: artifactServer` | Artifact publication service. |
| `spec.loadBalancer` | For `type: loadBalancer` | HAProxy-backed endpoint VIP service. |
| `spec.proxy` | For `type: proxy` | Proxy service. |
| `spec.nameResolution` | For `type: nameResolution` | DNS service. |
| `spec.ntp` | For `type: ntp` | NTP service. |
| `spec.registry` | For `type: registry` | Mirror registry service. |

### Artifact Server

| Field | Description |
| --- | --- |
| `artifactServer.machineRef` | Machine that runs the service. |
| `artifactServer.bindAddress` | Optional bind address. |
| `artifactServer.listeners[].name` | Listener name. |
| `artifactServer.listeners[].protocol` | Listener protocol, usually `https`. |
| `artifactServer.listeners[].port` | Listener port. |
| `artifactServer.endpoints[].name` | Endpoint selector name. |
| `artifactServer.endpoints[].listenerRef` | Listener name. |
| `artifactServer.endpoints[].addressRef` | Address on the placement machine. |

### Load Balancer

| Field | Description |
| --- | --- |
| `loadBalancer.implementation` | Current implementation is `haproxy`. |
| `loadBalancer.machineRef` | Machine that runs the service. |
| `loadBalancer.bindAddresses[].name` | Address selector name. |
| `loadBalancer.bindAddresses[].address` | VIP address. |

### Machine-Bound Services

The `proxy`, `nameResolution`, `ntp`, and `registry` arms share this placement
shape:

| Field | Applies to | Description |
| --- | --- | --- |
| `implementation` | All | Implementation name, such as `squid`, `dnsmasq`, `chrony`, or `mirror-registry`. |
| `machineRef` | All | Machine that runs the service. |
| `bindAddress` | All | Optional bind address. |
| `port` | All | Optional service port. |
| `endpoints[].name` | All | Endpoint selector name. |
| `endpoints[].addressRef` | All | Address on the placement machine. |
| `additionalIngressHosts[]` | `nameResolution` | Additional ingress hostnames to resolve before cluster DNS is ready. |
| `forwarders[]` | `nameResolution` | Upstream DNS forwarders. |
| `upstreamSources[]` | `ntp` | Upstream time sources. |

## NetworkConfig

`NetworkConfig` owns reusable installer network data.

| Field | Required | Description |
| --- | --- | --- |
| `spec.machineNetwork[].cidr` | Usually | Machine network CIDR rendered into installer input. |
| `spec.nameResolutionRefs[]` | No | Names `Environment.spec.infraComponents.nameResolution[]` entries. |
| `spec.template.networkConfig` | No | NMState template merged into each selected host. |

`Machine.spec.network.config.interfaceAddresses[]` injects per-machine static
addresses into the selected template.
