---
title: Networking
description: NetworkConfig templates for agent installs, binding machine networks with NMState, authoring static IPs once and referencing them, route/bond/VLAN overrides, endpoint sources and the single-node rule, and load-balancer VIP placement.
---

# Networking and load balancing

This page is the task-oriented guide to wiring machine networks and cluster
endpoints. The object model — `NetworkConfig`, `InfraProvider` attachments, and
`InfraComponent` services — lives in
[Infrastructure: providers, components & networking](../concepts/infrastructure.md);
cluster endpoint fields live in
[Container clusters](../concepts/container-clusters.md#endpoints). Link there for
the field tables; this page shows how to assemble them.

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
      routes:
        config:
          - destination: 0.0.0.0/0
            next-hop-address: 192.168.133.1
            next-hop-interface: bond0
            table-id: 254
```

The resolver is not repeated in the template: `nameResolutionRefs: [default]`
already injects the managed resolver's address into the rendered
`dns-resolver.config.server` list.

The template is shared by every machine that references it. It carries no
per-node address — `bond0` declares `ipv4.enabled: true` with `dhcp: false`
but leaves the static address to each machine.

## Binding a node's static IP

A node's static install address is authored **once** as a named
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
    NMState for settings the template does not cover — extra routes, MTU, a
    secondary interface, a VLAN, an additional bond. Do **not** set a static
    install IP through `overrides`: validation rejects an interface whose install
    IP is already owned by `interfaceAddresses[]`. `overrides` is valid only
    alongside `networkConfigRef`.

!!! note "Inline NetworkConfig spec"
    A machine may inline a full `NetworkConfig` spec under
    `spec.network.config.spec` instead of referencing one with
    `networkConfigRef`. The two are **mutually exclusive** — setting both is
    rejected (`config must set only one of networkConfigRef or spec`).
    `interfaceAddresses[]` works with either form.

## Routes, bonds, and VLANs

Bonds and routes can be authored directly in the shared template (as in the
example above). For per-machine deviations — a host that needs an extra route, a
larger MTU, a VLAN, or a secondary interface the template does not declare —
patch the template through `spec.network.config.overrides` rather than forking
the `NetworkConfig`. Keep the static install address in `interfaceAddresses[]`
even when overriding other attributes on the same interface.

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
`spec.network.config.attachmentRef`, as shown in the machine above. The full
attachment model, including KubeVirt secondary networks
(`networkAttachments[].kubevirt.networkRef`), is in
[Infrastructure](../concepts/infrastructure.md#network-attachments).

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

`source.type` is one of:

- `openshift` (default) — the cluster's own VIP, managed by the installer.
- `external` — an external load balancer / DNS outside Bootwright.
- `infraComponent` — a Bootwright-provisioned load balancer; set
  `source.componentRef` (the `InfraComponent` name) and, when the load balancer
  declares more than one bind address, `source.bindAddressRef`.

The full `Endpoint` field table is in
[Container clusters](../concepts/container-clusters.md#endpoints).

!!! note "Single-node clusters reject `openshift` sources"
    A single-node cluster cannot use `source.type: openshift` on the `api`,
    `api-int`, or `ingress` slot — the agent installer rejects bare-metal and
    vSphere platform blocks for one-control-plane clusters, so these clusters
    render `platform.none`. Use `external` or `infraComponent` instead.

## Load balancers and VIP placement

For a Bootwright-provisioned load balancer, declare the target `InfraComponent`
and its bind addresses, then point an endpoint slot at it:

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

`source.bindAddressRef` is the bind-address **name**, not the IP itself; the
effective IP comes from the matching `bindAddresses[].address`. If a load
balancer has exactly one bind address, the endpoint may omit
`source.bindAddressRef`; a non-empty `source.bindAddressRef` must always match a
declared `bindAddresses[].name`, even on a single-bind load balancer. Listener
ports for OpenShift roles are derived from the consumer role, not from arbitrary
endpoint names. Effective VIPs are validated against the machine networks
selected by cluster machines.

Bootwright renders and converges the HAProxy provider service for
`source.type=infraComponent` endpoints. Today, automatic VIP attachment is
implemented only for libvirt-backed cluster infrastructure when the
load-balancer host is also the infra host that can attach the address to the
libvirt network. Other placements require the external network fabric to route
the VIP to the load-balancer host.

## Storage RGW endpoints

Storage RGW endpoints are owned by the `StorageObjectGateway`, not by a cluster.
`spec.public` is the public S3 endpoint, and each `spec.ceph.ingresses[]` entry
is a concrete ingress VIP. `prefixLength` provides the `/24`-style suffix
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
          hosts: [node01, node02, node03]   # node hostnames, not machine names
```

See [Ceph storage topologies](ceph-topologies.md#rgw-and-ingress) for how RGW
fits the broader storage spine.

## Name resolution

`NetworkConfig.spec.nameResolutionRefs[]` selects entries from
`Environment.spec.infraComponents.nameResolution[]`; the resolved addresses are
injected into the rendered `dns-resolver.config.server` list. A `NetworkConfig`
that selects a resolver must therefore not also hardcode that resolver's address
in its `template.networkConfig.dns-resolver` — reserve static template resolver
servers for resolvers not declared in the environment (operator-external ones).
Keep `nameResolutionRefs` outside `template.networkConfig`; that map is raw
NMState.

### Machine and node records

A managed name-resolution (dnsmasq) component publishes, for every machine it
serves, a `host-record` for the machine's
[`fqdn` name](../concepts/machines.md#the-dnsentry-address)
(`<machineName>.<baseDomain>` unless overridden) targeting the machine's
`access.ssh.addressRef` IP, and a `cname` from each cluster node FQDN to the
bound machine's `fqdn`. When an operator-declared `fqdn` lives in a
zone the managed resolver does not own, the node record degrades to a direct
`host-record` on the same IP. The bare machine-label record (`<machineName>`
without the domain) is not published. Bootwright itself connects to
name-resolution-wired machines through the `fqdn` name, so these records
are load-bearing, not cosmetic.

On provided (external) name resolution the operator owns the records: for each
machine create `A <fqdn> → <ip>`, and for each cluster node
`CNAME <nodeFQDN> → <fqdn>` (or an equivalent A record). The preflight
group **Name resolution** resolves each machine's `fqdn` and each node
FQDN before apply and fails naming the exact record to create when one is
missing or points at the wrong address; under managed resolution the same
checks point at the apply command that converges the resolver instead.

### Cluster records

Managed name-resolution services render records for `api`, `api-int`, and the
cluster `*.apps.<cluster>.<baseDomain>` wildcard for each consuming cluster.
Console, OAuth, and other `*.apps` routes are already covered by that wildcard —
do not re-author them. Use `additionalIngressHosts[]` (on the environment entry
or the managed `InfraComponent.spec.nameResolution`) only for hostnames the
wildcard cannot cover. Entries render verbatim as host records pointing at the
consuming cluster's ingress VIP, so author fully-qualified names; values from the
environment entry and component merge for shared services.

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
    # Only for names the *.apps wildcard cannot cover; rendered verbatim as
    # host records at the cluster ingress VIP, so use fully-qualified names.
    additionalIngressHosts:
      - vanity.example.test
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

## See also

- [Infrastructure](../concepts/infrastructure.md) — the `NetworkConfig`,
  attachment, and `InfraComponent` object model.
- [Container clusters](../concepts/container-clusters.md#endpoints) — the
  `Endpoint` slot vocabulary and field table.
- [Operations and recovery](operations.md) — VIP and recovery interactions
  during apply and destroy.
