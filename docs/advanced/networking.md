---
title: Networking
description: NetworkConfig templates, machine static-IP binding, endpoints, and load balancers.
---

# Networking and load balancing

`NetworkConfig` is the reusable network template for agent installs. It owns:

- `machineNetwork[]`, rendered into `install-config.yaml`
- `template.networkConfig`, the raw NMState host template rendered into
  `agent-config.yaml`

Substrate network surfaces live on `InfraProvider.spec.networkAttachments[]`.
`Machine.spec.network.config.attachmentRef` maps a logical `NetworkConfig` to
the selected provider attachment. When a provider-backed machine omits
`attachmentRef`, it defaults to the `networkConfigRef` name; the default is
accepted only while the provider declares a single attachment — with several,
validation requires an authored `attachmentRef` naming the one to bind.

This page owns the machine network-binding example and the endpoint, load
balancer, and RGW endpoint surfaces. See
[Providers](providers.md) for substrate arms and
[Ceph storage clusters](storage-ceph.md) for the rest of the gateway.

## Network template

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

The template is shared by every machine that references it. It carries no
per-node address — `bond0` declares `ipv4.enabled: true` with `dhcp: false`
but leaves the static address to each machine.

## Binding a node's static IP

A node's static install address is authored once as a named
`Machine.spec.addresses[]` entry, then bound to an NMState interface with
`spec.network.config.interfaceAddresses[]`. This is the idiomatic mechanism:
the address lives in exactly one place, and rendering injects it into the
referenced interface's `ipv4`/`ipv6` block in the rendered NMState.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: rack1-srv1
spec:
  network:
    config:
      networkConfigRef: rack1-bonded-machine
      attachmentRef: rack1-machine-net
      interfaceAddresses:
        - interface: bond0
          addressRef: ip
          prefixLength: 24
    interfaceBinding:
      - nicRef: eno1
        interfaceName: eno1
      - nicRef: eno2
        interfaceName: eno2

  hardware:
    nics:
      - name: eno1
        macAddress: 00:25:90:5a:10:01
      - name: eno2
        macAddress: 00:25:90:5a:10:02

  addresses:
    - name: ip
      address: 192.168.133.20
```

Each `interfaceAddresses[]` entry has these fields:

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `interface` | string | Yes | — | NMState interface name to receive the address (e.g. `bond0`). |
| `addressRef` | string | Yes | — | Names a `Machine.spec.addresses[]` entry; its `address` is injected at render time. |
| `prefixLength` | int | Yes | — | CIDR prefix. 1–128; must be ≤ 32 for `ipv4`. |
| `family` | string | No | `ipv4` | One of `ipv4` or `ipv6`. The same interface may carry one entry per family. |

`interfaceBinding[]` is separate: it maps a declared
`spec.hardware.nics[]` entry (`nicRef`) to an NMState interface name
(`interfaceName`) so the rendered host config matches each NIC by MAC. The
`nicRef` values above (`eno1`, `eno2`) resolve to the `hardware.nics[]` entries
declared on the same machine.

!!! note "Raw NMState overrides are for non-address tweaks only"
    `spec.network.config.overrides` patches the referenced template's raw
    NMState for settings the template does not cover (extra routes, MTU, a
    secondary interface). Do **not** set a static install IP through
    `overrides` — validation rejects an interface whose install IP is already
    owned by `interfaceAddresses[]`. `overrides` is valid only alongside
    `networkConfigRef`.

!!! note "Inline NetworkConfig spec"
    A machine may inline a full `NetworkConfig` spec under
    `spec.network.config.spec` instead of referencing one with
    `networkConfigRef`. The two are **mutually exclusive** — setting both is
    rejected (`config must set only one of networkConfigRef or spec`).
    `interfaceAddresses[]` works with either form.

## Provider attachments

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

Bind that attachment from each installing machine via
`spec.network.config.attachmentRef`, as shown in the machine above.

## Endpoints

Cluster endpoints live in `ContainerCluster.spec.install.endpoints` under the
closed slot vocabulary `api`, `api-int`, and `ingress`; any other key is
rejected. Storage gateways do not borrow cluster endpoints; they own their RGW
public and ingress endpoints directly (below).

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

Each endpoint slot is an `Endpoint`:

| Field | Type | Required | Default | Notes |
| --- | --- | --- | --- | --- |
| `address` | string | No | — | The VIP for this endpoint. |
| `dnsName` | string | No | — | DNS name for this endpoint. |
| `port` | int | No | — | Listen port; the consumer role usually derives the OpenShift port. |
| `scheme` | string | No | — | URL scheme (e.g. `https`). |
| `prefixLength` | int | No | — | CIDR suffix for the VIP, used when Bootwright attaches the address. |
| `interfaceNetworks` | list | No | — | Subnets that may host the VIP, mirroring the RGW ingress fields below. |
| `source` | object | No | `openshift` | Where the endpoint comes from — see `source.type` below. |

`source.type` is one of:

- `openshift` (default) — the cluster's own VIP, managed by the installer.
- `external` — an external load balancer / DNS outside Bootwright.
- `infraComponent` — a Bootwright-provisioned load balancer; set
  `source.componentRef` (the `InfraComponent` name) and, when the load balancer
  declares more than one bind address, `source.bindAddressRef`.

For Bootwright-provisioned load balancers, declare the target component and its
bind addresses. `source.bindAddressRef` is the bind-address **name**, not the
IP itself; the effective IP comes from the matching `bindAddresses[].address`.

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
shortcut never applies to an authored name. Listener ports for OpenShift roles
are derived from the consumer role, not from arbitrary endpoint names.
Effective VIPs are validated against the machine networks selected by cluster
machines.

Bootwright renders and converges the HAProxy provider service for
`source.type=infraComponent` endpoints. Today, automatic VIP attachment is
implemented only for libvirt-backed cluster infrastructure when the
load-balancer host is also the infra host that can attach the address to the
libvirt network. Other placements require the external network fabric to route
the VIP to the load-balancer host.

## Storage RGW endpoints

Storage RGW endpoints are owned by the `StorageObjectGateway`, not by a cluster.
`spec.public` is the public S3 endpoint, and each `spec.ceph.ingresses[]` entry
is a concrete ingress VIP. `prefixLength` provides the `/24` style suffix
cephadm expects for the keepalived virtual IP, and `virtualInterfaceNetworks[]`
tells cephadm which site-local subnet can host that VIP. `spec.ceph.frontendPort`
sets the RGW daemon's frontend listen port behind the ingress (distinct from the
public `spec.public.port`):

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageObjectGateway
metadata:
  name: odf-rgw
spec:
  public:
    dnsName: rgw-ceph.bootwright.test
    scheme: https
    port: 443
  ceph:
    serviceID: odf
    frontendPort: 8080
    ingresses:
      - name: dc1
        address: 192.168.141.80
        prefixLength: 24
        virtualInterfaceNetworks:
          - 192.168.141.0/24
        placement:
          hosts: [ceph-dc1-0, ceph-dc1-1, ceph-dc1-2]
```

## Name resolution

`NetworkConfig.spec.nameResolutionRefs[]` selects entries from
`Environment.spec.infraComponents.nameResolution[]`. Static resolver servers
may still come from the `NetworkConfig` NMState template; resolved DNS refs are
appended to the rendered `dns-resolver.config.server` list. Keep
`nameResolutionRefs` outside `template.networkConfig`; that map is raw NMState.

Managed name-resolution services render records for `api`, `api-int`, and the
cluster apps wildcard for each consuming cluster. Use `additionalIngressHosts[]`
on the environment entry or the managed `InfraComponent.spec.nameResolution`
when specific ingress hostnames, such as console or OAuth routes, must resolve
before the cluster DNS operator is ready. Those hostnames point at the consuming
cluster's ingress VIP, and values from the environment entry and component merge
for shared services.

A managed `nameResolution` `InfraComponent` also accepts `forwarders[]`: the
list of upstream DNS servers the managed resolver forwards queries it cannot
answer to. Set it when the lab subnet has no default upstream the resolver can
reach on its own.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: InfraComponent
metadata:
  name: dns
spec:
  type: nameResolution
  nameResolution:
    implementation: dnsmasq
    machineRef: bastion
    forwarders:
      - 192.168.133.1
    additionalIngressHosts:
      - console-openshift-console
      - oauth-openshift
```

## NTP sources

`Environment.spec.infraComponents.ntp[]` declares fleet-wide time sources for
agent installs. External entries provide an `address` directly. Managed entries
reference an `InfraComponent` with `spec.ntp`; Bootwright converges chrony on
the selected host and renders the managed endpoint, or a concrete bind address,
into installer `additionalNTPSources`.

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
