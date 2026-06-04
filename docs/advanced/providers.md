---
title: Providers
description: InfraProvider capability shapes and cluster machine selection.
---

# Providers

`InfraProvider` declares what a substrate can provide. It does not decide
which cluster consumes the capability.

Current apply support covers libvirt machines with emulated Redfish BMCs,
bare-metal machines with Redfish virtual media, and KubeVirt VMs hosted on an
OpenShift Virtualization cluster. vSphere is a valid schema shape, but its
apply adapter is not converged yet.

## Bare Metal

Bare-metal inventory keeps physical server facts on the installing `Machine`:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: rack1-srv1
spec:
  capabilities:
    - openshift-node
  substrate:
    providerRef:
      name: rack1-baremetal
  hardware:
    nics:
      - name: nic1
        macAddress: 52:54:00:33:11:10
      - name: nic2
        macAddress: 52:54:00:33:11:20
    boot:
      nicRef:
        name: nic1
    management:
      bmc:
        address: redfish-virtualmedia+https://bmc.example.test/redfish/v1/Systems/1
        credentialsRef:
          name: bmc-credentials
        disableCertificateVerification: true
  os:
    provided: false
    install:
      rootDeviceHints:
        deviceName: /dev/sda
```

`disableCertificateVerification: true` is a lab posture for BMCs without
trusted TLS. Do not treat it as the production default.

The Machine selects its provider attachment and adds IP overrides:

```yaml
network:
  config:
    networkConfigRef:
      name: rack1-bonded-machine
    attachmentRef:
      name: rack1-machine-net
    overrides:
      interfaces:
        - name: bond0
          ipv4:
            address:
              - ip: 192.168.133.20
                prefix-length: 24
  interfaceBinding:
    - nicRef:
        name: nic1
      interfaceName: eno1
    - nicRef:
        name: nic2
      interfaceName: eno2
addresses:
  - name: ip
    address: 192.168.133.20
```

## vSphere

vSphere profiles keep vCenter, datacenter, failure-domain, and topology facts
inside the provider:

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
        credentialsRef:
          name: vcenter-credentials
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
    machineProfiles:
      - name: vsphere-control-plane
        cpu: 8
        memoryMiB: 22528
        diskGiB: 120
        template: rhcos
        failureDomainRef:
          name: dc1-zone-a
```

The vSphere desired-state shape is present so the schema can stabilize ahead
of the apply adapter. The shipped apply workflows do not converge vSphere
clusters yet.

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
    hostClusterRef:
      name: metal-ocp
    namespace: bootwright-child-ocp
    storageClassRef:
      name: lvms-vg1
    machineProfiles:
      - name: child-sno
        cpu: 8
        memoryMiB: 16384
        diskGiB: 120
```

Machines select one of those profiles through
`Machine.spec.substrate.profileRef`.

Use `hostClusterRef` when the virtualization host is another Bootwright
`ContainerCluster`. Bootwright uses the cluster secrets kubeconfig from that host
cluster; do not put kubeconfig bytes in desired state. Use `kubeconfigRef`
when the host cluster is external:

```yaml
kubevirt:
  kubeconfigRef:
    name: external-virt-cluster-kubeconfig
  namespace: bootwright-child-ocp
```

Exactly one of `hostClusterRef` or `kubeconfigRef` is required. The namespace is
required and the storage class is optional. KubeVirt machines must bind their
selected `NetworkConfig` to a provider `networkAttachments[].kubevirt.nadRef`,
and the full apply graph waits for the host cluster add-on that advertises
`provides: [kubevirt]` before creating child VMs. Focused applies must either
name both parent and child in `--clusters`, or run after the parent install and
KubeVirt add-on are ready.

```yaml
spec:
  networkAttachments:
    - name: child-machine-net
      kubevirt:
        nadRef:
          name: child-machine-net
          namespace: bootwright-child-ocp
```

## Services

Machine-bound shared services are declared as `InfraComponent` objects:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: proxy
spec:
  proxy:
    type: squid
    machineRef:
      name: services-host
    port: 3128
```

Artifact publication is different: generated ISO and boot-artifact publication
is derived from install requirements and uses an environment-bound
`InfraComponent`:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: artifact-server
spec:
  artifactServer:
    machineRef:
      name: services-host
    listeners:
      - name: https
        protocol: https
        port: 9443
    endpoints:
      - name: bmc
        listener: https
        machineAddress: lab-lan
      - name: cluster
        listener: https
        machineAddress: lab-lan
```

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Environment
spec:
  infraComponents:
    artifactServers:
      - name: default
        type: managed
        componentRef:
          name: artifact-server
```

```yaml
spec:
  defaults:
    artifactAccess:
      serverRef:
        name: default
      redfishVirtualMedia:
        endpointRef:
          name: bmc
```

Endpoint names are endpoint selectors; `machineAddress` values resolve against the
named addresses on the selected `machineRef`. For `redfishVirtualMedia`, use a
BMC-routable IP address entry in most environments; many BMCs do not reliably
resolve DNS aliases, and Bootwright uses the matched address value directly in
the ISO URL sent to Redfish. `ContainerCluster.spec.install.artifactAccess` may override
the default when one cluster needs a different artifact server or endpoint.
Bootwright serves HTTPS listeners with a self-signed certificate generated on
the host. Omit `listeners` to use the default HTTPS listener on port `8443`.

Supported authored `InfraComponent` arms are `artifactServer`,
`loadBalancer`, `proxy`, `nameResolution`, `ntp`, and `registry`.

When adding another managed service, keep the service path orthogonal: add a
typed `InfraComponent`/`Environment` arm, register its role/image/defaults in
`internal/infra/support`, add its consumer discovery to the service graph, project
that resolved graph into Ansible vars, and place the converging role under
`ansible/collections/ansible_collections/bootwright/core/roles/infra_component_*`.

For real BMCs, the artifact server endpoint selected by
`artifactAccess.redfishVirtualMedia.endpointRef.name` should usually resolve
to an IP address that the BMC network can reach. Controller reachability alone
is not enough for virtual-media ISO fetches.
