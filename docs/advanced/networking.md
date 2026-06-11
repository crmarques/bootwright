---
title: Networking
description: NetworkConfig templates, machine overrides, endpoints, and load balancers.
---

# Networking

`NetworkConfig` is the reusable network template for agent installs. It owns:

- `machineNetwork[]`, rendered into `install-config.yaml`
- `template.networkConfig`, rendered into `agent-config.yaml`

Substrate network surfaces live on
`InfraProvider.spec.networkAttachments[]`. `Machine.spec.network.config.attachmentRef`
maps a logical `NetworkConfig` to the selected provider attachment. When a
provider-backed machine omits `attachmentRef`, it defaults to the
`networkConfigRef` name; the default is accepted only while the provider
declares a single attachment — with several, validation requires an authored
`attachmentRef` naming the one to bind.

## Bonded Bare-Metal Template

```yaml
apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata:
  name: rack1-bonded-machine
spec:
  machineNetwork:
    - cidr: 192.168.133.0/24

  nameResolutionRefs:
    - default

  template:
    networkConfig:
      interfaces:
        - name: eno1
          type: ethernet
          state: up
        - name: eno2
          type: ethernet
          state: up
        - name: bond0
          type: bond
          state: up
          link-aggregation:
            mode: active-backup
            options:
              miimon: "100"
            port:
              - eno1
              - eno2
          ipv4:
            enabled: true
            dhcp: false
          ipv6:
            enabled: false
      dns-resolver:
        config:
          server:
            - 192.168.133.1
      routes:
        config:
          - destination: 0.0.0.0/0
            next-hop-address: 192.168.133.1
            next-hop-interface: bond0
            table-id: 254
```

Each cluster machine references the template and overrides only its address:

```yaml
network:
  config:
    networkConfigRef: rack1-bonded-machine
    attachmentRef: rack1-machine-net
    overrides:
      interfaces:
        - name: bond0
          ipv4:
            address:
              - ip: 192.168.133.20
                prefix-length: 24
  interfaceBinding:
    - nicRef: nic1
      interfaceName: eno1
    - nicRef: nic2
      interfaceName: eno2
addresses:
  - name: ip
    address: 192.168.133.20
```

## Provider Attachments

Provider attachments describe how a substrate exposes a logical machine
network:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: lab-libvirt
spec:
  networkAttachments:
    - name: rack1-machine-net
      libvirt:
        bridge: vbr-rack1
```

Bind that attachment from each installing Machine:

```yaml
network:
  config:
    networkConfigRef: rack1-bonded-machine
    attachmentRef: rack1-machine-net
```

## Endpoints

Cluster endpoints live in `ContainerCluster.spec.install.endpoints` under the
closed slot vocabulary `api`, `api-int`, and `ingress`; any other key is
rejected. Storage gateways do not borrow cluster endpoints; they own their
RGW public and ingress endpoints directly (below).

```yaml
endpoints:
  api:
    address: 192.168.133.10      # source.type defaults to openshift
  api-int:
    address: 192.168.133.10
    source:
      type: external             # external LB/DNS, outside Bootwright
  ingress:
    source:
      type: infraComponent       # Bootwright-provisioned load balancer
      componentRef: apps
      bindAddressRef: apps-ip
```

For Bootwright-provisioned load balancers, declare the target component and its
bind addresses. `source.bindAddressRef` is the bind-address name, not the IP
itself; the effective IP comes from the matching `bindAddresses[].address`.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: apps
spec:
  type: loadBalancer
  loadBalancer:
    implementation: haproxy
    machineRef: bastion
    bindAddresses:
      - name: apps-ip
        address: 192.168.133.11
```

If a load balancer has exactly one bind address, the endpoint may omit
`source.bindAddressRef`. A non-empty `source.bindAddressRef` must always match
a declared `bindAddresses[].name`, even on a single-bind load balancer; the
shortcut never applies to an authored name. Listener ports for OpenShift
roles are derived from the consumer role, not from arbitrary endpoint names.
Effective VIPs are validated against the machine networks selected by cluster
machines.

Bootwright renders and converges the HAProxy provider service for
`source.type=infraComponent` endpoints. Today, automatic VIP attachment is
implemented only for libvirt-backed cluster infrastructure when the
load-balancer host is also the infra host that can attach the address to the
libvirt network. Other placements require the external network fabric to route
the VIP to the load-balancer host.

Storage RGW endpoints are owned by the `StorageObjectGateway`, not by a cluster.
`spec.public` is the public S3 endpoint, and each `spec.ceph.ingresses[]` entry
is a concrete ingress VIP. `prefixLength` provides the `/24` style suffix cephadm
expects for the keepalived virtual IP, and `virtualInterfaceNetworks[]` tells
cephadm which site-local subnet can host that VIP:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageObjectGateway
spec:
  public:
    dnsName: rgw-ceph.bootwright.test
    scheme: https
    port: 443
  ceph:
    serviceID: odf
    ingresses:
      - name: dc1
        address: 192.168.141.80
        prefixLength: 24
        virtualInterfaceNetworks:
          - 192.168.141.0/24
        placement:
          hosts: [ceph-dc1-0, ceph-dc1-1, ceph-dc1-2]
```

## Name Resolution

`NetworkConfig.spec.nameResolutionRefs[]` selects entries from
`Environment.spec.infraComponents.nameResolution[]`. Static resolver servers
may still come from the `NetworkConfig` NMState template; resolved DNS refs are
appended to the rendered `dns-resolver.config.server` list. Keep `nameResolutionRefs`
outside `template.networkConfig`; that map is raw NMState.

Managed name-resolution services render records for `api`, `api-int`, and the
cluster apps wildcard for each consuming cluster. Use
`additionalIngressHosts[]` on the environment entry or the managed
`InfraComponent.spec.nameResolution` when specific ingress hostnames, such as
console or OAuth routes, must resolve before the cluster DNS operator is ready.
Those hostnames point at the consuming cluster's ingress VIP, and values from
the environment entry and component merge for shared services.

## NTP Sources

`Environment.spec.infraComponents.ntp[]` declares fleet-wide time
sources for agent installs. External entries provide an `address` directly.
Managed entries reference an `InfraComponent` with `spec.ntp`; Bootwright
converges chrony on the selected host and renders the managed endpoint, or a
concrete bind address, into installer `additionalNTPSources`.

```yaml
infraComponents:
  ntp:
    - name: external-01
      management: external
      address: ntp.example.test
    - name: lab-ntp
      management: managed
      componentRef: ntp-server
      endpointRef: cluster
```
