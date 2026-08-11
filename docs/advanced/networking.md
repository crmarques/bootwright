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
[Container clusters](../concepts/container-clusters.md#endpoints). Those pages
own the kinds and their field tables; this page owns assembly.

`NetworkConfig` is the reusable network template for agent installs. It owns:

- `machineNetwork[]`, rendered into `install-config.yaml`
- `template.networkConfig`, the raw NMState host template rendered into
  `agent-config.yaml`

Substrate network surfaces live on `InfraProvider.spec.networkAttachments[]`.
`Machine.spec.network.config.attachmentRef` maps a logical `NetworkConfig` to
one selected provider attachment for all physical NICs. A KubeVirt machine can
instead map each NIC through `interfaceAttachments[]`. When a provider-backed
machine omits both, `attachmentRef` defaults to the `networkConfigRef` name; the
default is accepted only while the provider declares a single attachment — with
several, validation requires an explicit whole-machine or per-interface choice.

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

The `interfaceAddresses[]` field table is on
[Machines → Network](../concepts/machines.md#network).

The sibling `interfaceBinding[]` is *not* in the example because this machine
does not need it: it maps a declared `spec.hardware.nics[]` entry (`nicRef`) to a
differently-named NMState interface (`interfaceName`). Omit it when the NMState
interface names already equal the `hardware.nics[]` names; the render then uses
the declared NICs as-is. Author it only to remap — for example
`nicRef: eno1` → `interfaceName: bond0-port0`.

Use `spec.network.config.overrides` for non-address template tweaks the template
does not cover (extra routes, MTU, a VLAN); it never carries a static install IP
— see [Machines → Network](../concepts/machines.md#network).

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
(`networkAttachments[].kubevirt.networkRef`) and per-interface binding, is in
[Infrastructure](../concepts/infrastructure.md#network-attachments).

### KubeVirt child networks

A KubeVirt child cluster's machines attach to one or more
`ClusterUserDefinedNetwork` objects, usually `topology: Localnet` networks
bridged to physical VLANs by a `NodeNetworkConfigurationPolicy` on the parent.
Bootwright **references** those objects and never renders them: deliver the
CUDNs and NNCP as a `manifestSet` add-on on the parent, then name each CUDN from
`networkAttachments[].kubevirt.networkRef`.

One attachment can still serve every VM NIC. When guest NICs must land on
different networks, use `Machine.spec.network.config.interfaceAttachments[]`.
The list maps each physical interface in the effective `NetworkConfig` to its
own provider attachment; it is not a VLAN-trunk shortcut. The following child
network keeps the OpenShift machine network on a 1500-byte primary NIC and adds
a 9000-byte routed client network for external Ceph:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata:
  name: dc1-child-net
spec:
  machineNetwork:
    - cidr: 192.168.151.0/24
  template:
    networkConfig:
      interfaces:
        - name: primary
          type: ethernet
          state: up
          mtu: 1500
          ipv4:
            enabled: true
            dhcp: false
          ipv6:
            enabled: false
        - name: ceph-public
          type: ethernet
          state: up
          mtu: 9000
          ipv4:
            enabled: true
            dhcp: false
          ipv6:
            enabled: false
      routes:
        config:
          - destination: 0.0.0.0/0
            next-hop-address: 192.168.151.1
            next-hop-interface: primary
            table-id: 254
          - destination: 192.168.141.0/24
            next-hop-address: 192.168.171.1
            next-hop-interface: ceph-public
            table-id: 254
```

Each machine binds both NICs, authors only the primary install IP through
`interfaceAddresses[]`, and adds its secondary client address through the
per-machine NMState override:

```yaml
spec:
  network:
    config:
      networkConfigRef: dc1-child-net
      interfaceAttachments:
        - interface: primary
          attachmentRef: dc1-child-net
        - interface: ceph-public
          attachmentRef: dc1-ceph-public
      interfaceAddresses:
        - interface: primary
          addressRef: ip
          prefixLength: 24
      overrides:
        interfaces:
          - name: ceph-public
            ipv4:
              address:
                - ip: 192.168.171.60
                  prefix-length: 24
  addresses:
    - name: ip
      address: 192.168.151.30
    - name: ceph-public
      address: 192.168.171.60
```

The primary `192.168.151.0/24` CIDR remains the child `ContainerCluster`'s only
machine network. `192.168.171.0/24` is a routed node/client subnet, not a Ceph
daemon bind network, so it does **not** belong in
`StorageCluster.spec.ceph.networks.publicCIDRs`; that list contains the networks
where Ceph hosts actually stand. Data Foundation external-mode CSI uses node
networking to reach Ceph monitor endpoints, so every schedulable child node
needs the secondary route. The fabric must route both directions between the
node/client subnet and every Ceph public CIDR, permit the required Ceph ports,
and carry MTU 9000 end to end. A localnet CUDN with disabled IPAM leaves both
addresses under Bootwright desired state. See
[KubeVirt nested clusters](kubevirt.md) for the provider and parent-ordering
model.

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
- `node` — the cluster's single node; the address resolves from that node's
  `Machine`. Single-node clusters only, and `address` must be empty.

The full `Endpoint` field table is in
[Container clusters](../concepts/container-clusters.md#endpoints).

!!! note "Single-node clusters answer at their node, not at a VIP"
    A single-node cluster cannot use `source.type: openshift` on the `api`,
    `api-int`, or `ingress` slot — the agent installer rejects bare-metal and
    vSphere platform blocks for one-control-plane clusters, so these clusters
    render `platform.none`.

    A single-node cluster has no VIP: all three slots answer at the node's own
    install address. Say that with `source.type: node` instead of repeating the
    address:

    ```yaml
    endpoints:
      api:
        source:
          type: node
      api-int:
        source:
          type: node
      ingress:
        source:
          type: node
    ```

    Bootwright resolves the address from the `Machine.spec.addresses[]` entry
    the node's `spec.network.config.interfaceAddresses[]` points at, and
    `render effective` materializes it, so the endpoints cannot drift from the
    machine. A node that resolves to no install address, or to more than one,
    is rejected naming what was found. `external` and `infraComponent` stay
    available when an operator-owned load balancer or DNS name fronts the node.

## One cluster, one IP address family

Single-stack is the current scope. A `ContainerCluster`'s effective networking —
the `machineNetwork[]` CIDRs of the `NetworkConfig`s its nodes consume, its
`spec.networking.clusterNetwork` and `serviceNetwork`, and the resolved `api`,
`api-int`, and `ingress` endpoint addresses — must be one address family.
Mixing families is refused naming the cluster, both conflicting values with
their families, and the scope.

IPv6-only is fully supported: an all-IPv6 cluster validates, and the
`clusterNetwork`/`serviceNetwork` defaults follow the machine-network family.
The rule refuses *mixing*, not IPv6.

Dual-stack is deferred rather than partially wired.
`endpoints.<slot>.address` is a single address, where the native
`apiVIPs`/`ingressVIPs` are lists precisely so a cluster can carry one VIP per
family — so a fleet authored with both families used to render a single-stack
install-config nobody wrote. Failing closed at validation replaces that silence.

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
    dnsLabel: rgw-ceph
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
          hosts: [node-01, node-02, node-03]   # node names, not machine names
```

`public.dnsLabel` is the leftmost label only; Bootwright composes the published
name as `<dnsLabel>.<storageClusterRef name>.<domains.storageClusters>`, the same
composition the Ceph mgmt-gateway and node FQDNs use. It defaults to the
gateway's own `metadata.name`, so gateways on one cluster never collide. Under
managed name resolution that composed name gets one `host-record` per declared
ingress VIP — one gateway with several site-scoped ingresses resolves to all of
their VIPs, not just the first. For a stable, distinct FQDN per data site
instead (the Metro-DR pattern), author one `StorageObjectGateway` per site
rather than stacking ingresses under one name.

See [Ceph storage topologies](ceph-topologies.md#rgw-and-ingress) for how RGW
fits the broader storage spine.

## Name resolution

`NetworkConfig.spec.nameResolutionRefs[]` selects entries from
`Environment.spec.infraComponents.nameResolution[]`; the resolved addresses are
injected into the rendered `dns-resolver.config.server` list, unioned with
whatever the template already carries and deduplicated. Repeating a selected
resolver's address in `template.networkConfig.dns-resolver` is therefore
redundant rather than wrong — the ref already injects it. Reserve static template
resolver servers for resolvers **not** declared in the environment
(operator-external ones). Keep `nameResolutionRefs` outside
`template.networkConfig`; that map is raw NMState.

One context may consume one managed name-resolution service. Bootwright counts
the services that loaded `NetworkConfig.nameResolutionRefs` actually resolve,
not every managed row in the Environment catalog. Unused catalog entries are
allowed, and two compatible catalog names that point at the same
`InfraComponent` remain one service. Consumers that resolve to two distinct
managed components fail validation; use one shared component or external DNS.

Bootwright establishes controller resolution after that remote DNS service is
ready and before any machines-phase SSH or FQDN access. This applies to
storage-only environments and to a machines-only reapply; the latter assumes
the omitted `fabric` work already exists but may reconcile its controller route.
A range that starts at `deps`, `base`, or `add-ons` instead performs a live
read-only proof before its first selected mutation. It never changes resolver
state merely because an earlier phase was omitted.

On systemd-resolved Bootwright installs one context-and-service-owned split-DNS
drop-in. The route includes the machine domain, every container- and
storage-cluster zone that consumes that service, and any exact rendered name
outside those zones. It verifies every rendered host, subtree, and CNAME answer
against the exact desired address set.

If the controller already returns every desired answer, Bootwright leaves its
resolver untouched only when no Bootwright-owned route needs reconciliation and
no sibling Bootwright route exists. An owned route is always reconciled to
current desired state. Before changing it, Bootwright also proves that its
drop-in can exclusively own global `DNS=` and `Domains=` policy: effective
static assignments outside the owned file refuse, a first install requires
empty runtime global settings, and an overlapping per-link route domain
refuses. Disjoint per-link routes may coexist. After a write, the effective
global servers and route domains must exactly match desired state.

The systemd-resolved daemon restarts when the drop-in changes. It also restarts
an unchanged owned route when the initial exact probe fails, which recovers a
stale daemon or cache before failing the final proof. A healthy unchanged route
does not restart.

A concrete bind address is used when declared; a wildcard bind resolves through
the environment entry's selected service endpoint (or the component's sole
endpoint). If neither yields a concrete controller-reachable address,
Bootwright cannot install or reconcile an owned route: it refuses before
machine work and prints an exact controller-built command to retry. An unowned
operator resolver needs no such address when every exact answer already works.
When selected work may already have changed the controller route, the retry
preserves context, selection, authorization, effects, connection options, and
ending stage. It changes `--mode create` to `--mode reconcile`, because the
failed attempt may already have written ownership; `reconcile` and `rebuild`
stay unchanged. A machines-only retry starts at `fabric` when service repair is
required.

A later-only proof failure prints two commands instead: first an exact
`--mode reconcile --stage fabric` repair for every cluster that consumes the
shared service, then a byte-equivalent retry of the original selection. The
first command cannot run later-phase work; the second cannot widen it. Run them
in that order.

During infrastructure destroy, a read-only controller ownership preflight runs
before any teardown and controller cleanup runs last. Cleanup requires every
task naming selected resource identity to report success; skipped or no-host
selected work retains the resolver route and its ownership evidence for the
exact destroy retry. Empty no-op tasks may settle as skipped.

Do not replace the consumed managed component identity in place. Leave the old
component declared and referenced, complete its owning infrastructure destroy
so Bootwright safely removes the old route and ownership evidence, then update
the reference and apply the replacement. A direct switch refuses the old route
as sibling evidence. On a controller without systemd-resolved the operator must
provide the required route; Bootwright accepts it only when every exact probe
already passes. External name-resolution entries remain entirely
operator-owned.

### Machine and node records

A managed name-resolution (dnsmasq) component publishes, for every machine it
serves, a `host-record` for the machine's
[`fqdn` name](../concepts/machines.md#the-fqdn-address)
(`<machineName>.<machine domain>` unless overridden — the machine domain is
`domains.machines`, which defaults to `domains.base`)
targeting the machine's
`access.ssh.addressRef` IP, and a `cname` from each cluster node FQDN to the
bound machine's `fqdn`. The `fqdn` host-record is published even when an
operator-declared `fqdn` lives in a foreign zone: the managed resolver
answers authoritatively for the machines it serves, so the cname target is
always locally known and environments resolve corporate machine names without
reaching corporate DNS. Note that a multi-homed machine whose foreign-zone
record points at a different interface resolves environment-locally to the
`access.ssh.addressRef` IP. The bare machine-label record (`<machineName>`
without the domain) is not published. Bootwright itself connects to
name-resolution-wired machines through the `fqdn` name, so these records
are load-bearing, not cosmetic.

On provided (external) name resolution the operator owns the records: for each
machine create `A <fqdn> → <ip>`, and for each cluster node
`CNAME <nodeFQDN> → <fqdn>` (or an equivalent A record). The preflight
group **Name resolution** resolves each machine's `fqdn` and each node
FQDN before apply and fails naming the exact record to create when one is
missing or points at the wrong address; under managed resolution the same
checks warn instead of failing — converging the resolver is exactly what
apply does — and point at the apply command. The apply graph's controller-DNS
barrier then performs the authoritative probe and refuses before machines work
if the warning was not repaired by managed convergence.

A machine with `os.provided: true` cannot declare `spec.network.config` at all,
so it can never reference a name-resolution entry. When such a machine is a
cluster node — a provided stretch arbiter, say — and the environment declares
any name-resolution entry, both of its records are checked against the
environment's declaration and fail when missing. They fail rather than warn
even under managed resolution: the managed resolver's record set is built from
the machines that reference it, so no apply produces a record for a machine
that references none. Placing that one is the operator's, wherever their
resolver lives.

### Cluster records

Managed name-resolution services render records for `api`, `api-int`, and the
cluster `*.apps.<cluster>.<domains.containerClusters>` wildcard for each
consuming cluster (the container-cluster zone under
[ADR 0018](https://github.com/crmarques/bootwright/blob/main/specs/adr/0018-environment-domain-model.md)).
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
