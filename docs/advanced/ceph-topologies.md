---
title: Ceph Storage Topologies
description: Managed vs imported Ceph, distribution and release pinning, entitlements, the bootstrap seed host, FQDN host identity, stretch mode, FIPS, the HA dashboard management gateway, RGW ingress, exports to Data Foundation, and migrating an existing cluster.
---

# Ceph storage topologies

This page is the task-oriented guide to standing up and evolving Ceph storage
clusters. A `StorageCluster` of `type: ceph` with `management: managed` is
bootstrapped by Bootwright with `cephadm`. Ceph keeps no kubeconfig-style admin
file on the controller — the admin keyring and `ceph.conf` live on the seed node
— so day-to-day access is by SSH to the seed node plus `cephadm shell`.

For the full `StorageCluster` object model and every field of the storage kinds,
see the [storage reference](../concepts/storage.md). For a guided first
managed-Ceph apply, see [Provisioning a Ceph cluster](../getting-started/ceph.md).
The complete reference trees that exercise each topology are catalogued under
[Reference examples](examples.md).

!!! note "Scope: managed Ceph vs imported Ceph"
    This page covers `management: managed` clusters — the ones Bootwright stands
    up and converges with `cephadm`. A `StorageCluster` can also be imported with
    `management: external`, in which case Bootwright runs no storage task at all
    and only references the cluster (typically consumed through a `StorageExport`
    and Data Foundation external mode). For the imported path, see the
    [`StorageCluster` reference](../concepts/storage.md#storagecluster) and the
    `baremetal-redfish-imported-ceph-odf` [reference example](examples.md). The
    behavior below applies only when `management` is `managed`.

## Choosing a distribution and pinning a release

`spec.ceph.distribution` selects where Ceph comes from: `oss` (the default, the
upstream community packages and images), `redhat` (Red Hat Ceph Storage), or
`ibm` (IBM Storage Ceph). The subscription distributions install from
entitlement-backed repositories, so `spec.ceph.entitlementRef` must name an
`Entitlement` that resolves to a `redhat-ceph` or `ibm-storage-ceph`
entitlement; `oss` takes no entitlement. An `ibm-storage-ceph` entitlement
carries only the IBM registry and license; the storage nodes register RHEL
through a separate `redhat-rhel` subscription named by their
`MachineInstallProfile.spec.subscription` (or the cluster `osSubscriptionRef`),
which the machines-phase registration task registers each node with — after the OS is in
place and before the Ceph deps work, not inside the cephadm flow. An
entitlement with `rhsm.management: external` delegates that registration to an
operator-supplied `Playbook` instead. The entitlement model — the
`spec.type` values, license acceptance, the `rhsm.management` axis, and the
credential plumbing — lives in
[Secrets and entitlements](../concepts/secrets.md#entitlements).

`spec.ceph.release` selects which release to install for the chosen
distribution, and Bootwright derives the artifact coordinates from it instead of
looking them up — the leading component is the product stream, and the stream
plus each node's RHEL major build the tools repo, the vendor `.repo` URL, and the
daemon image repository. `oss` takes an upstream release name (`tentacle`) or an
exact `x.y.z` version; the default `20.2.2` pins both the package repository and
`quay.io/ceph/ceph:v20.2.2`. Red Hat and IBM take a dot-separated numeric product
version of any length, defaulting to `9.1` and `9.9.1.0`.

Bootwright keeps no list of releases and no vendor support matrix, so it never
judges the release you declare, nor the RHEL version you run it on. A release
published today installs today. Neither the vendor image build tag nor the Ceph
package build is derivable from a product release, so name them yourself with
`spec.ceph.image.version` and `spec.ceph.packageVersion` to lock the exact build
on both axes. To mirror upstream packages for a
disconnected `oss` install, set an HTTPS `spec.ceph.community.mirror`.
Check what a release supports against the vendor's own sources — the upstream
[Ceph releases](https://docs.ceph.com/en/latest/releases/) page, the
[Red Hat Ceph Storage compatibility guide](https://docs.redhat.com/en/documentation/red_hat_ceph_storage/9/pdf/compatibility_guide/Red_Hat_Ceph_Storage-9-Compatibility_Guide-en-US.pdf),
and IBM's [node prerequisites](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=installation-registering-storage-ceph-nodes).

```yaml
ceph:
  distribution: redhat
  release: "9.1"
  packageVersion: "19.2.1-245.el9cp"   # the cephadm RPM build from the vendor's table
  image:
    version: "9-1234"                  # the daemon image build; base is derived
  entitlementRef: rhcs                 # names a redhat-ceph Entitlement
  cephadm:
    bootstrap:
      node: node-01                    # topology node cephadm bootstraps on (node name)
```

The `ceph-distribution-oss`, `ceph-distribution-redhat`, and
`ceph-distribution-ibm` [reference examples](examples.md) show each source end
to end, including the entitlement and license-acceptance workflow.

!!! note "Pin the build on `redhat`/`ibm`"
    Set `spec.ceph.image.version` for production — a vendor build tag, or a
    `sha256:` digest for a fully immutable pin. Left unset, the install uses the
    distribution-packaged `cephadm`'s default image tag, which floats, so the
    running Ceph version is not reproducible across re-installs. You normally
    only write the version: `spec.ceph.image.base` defaults to the vendor
    repository Bootwright derives from the distribution, release and entitlement
    registry, so the namespace and stream cannot drift from the release. Author
    `base` only to mirror the image or to name a build base Bootwright has not
    recorded.

    Pin `spec.ceph.packageVersion` alongside it to fix the `cephadm` RPM on the
    hosts. It is the one version field that is *not* install-time-only: it names
    the host CLI, not the daemons, so changing it reconciles in place instead of
    proposing a rebuild.

!!! warning "IBM Call Home is an explicit choice"
    IBM Storage Ceph 9.9.1.0 enables Call Home when the license is accepted.
    Author `spec.ceph.ibm.callHome: enabled` to retain it or `disabled` to turn
    it off after bootstrap. Validation rejects an IBM cluster that leaves the
    outbound-communication choice implicit.

## The bootstrap seed host

`spec.ceph.cephadm.bootstrap.node` names the topology node cephadm bootstraps on
(the seed node). The rendered `cephadm bootstrap --mon-ip` is always an address
of that host: the address named by `cephadm.bootstrap.addressRef`, falling back
to `cephadm.addressRef`, and finally the host machine's SSH address.

`bootstrap.node` names a node by its name — the FQDN or its short label
(`node-01`); both resolve, and Bootwright canonicalizes them to the registered
hostname cephadm expects. A `Machine` name is rejected with guidance naming
the node. The same is true of the other authored node references —
`placement.hosts[]` and `topology.stretch.tiebreaker.node`.

## Host identity and FQDN node naming

cephadm registers every `spec.ceph.topology.nodes[]` entry under its
`name` — the cluster's name for the node, independent of the bound machine's
name and **required** on every entry. A bare label composes to the
fully-qualified `<name>.<cluster>.<baseDomain>` (e.g.
`node-03.ceph-ibm.bootwright.test`); a dotted value is an explicit FQDN taken
verbatim. The composed FQDN is rendered verbatim into the cephadm host spec, is
the name the mgr dashboard uses to reach monitoring services (Prometheus,
Grafana, Alertmanager), and must equal the host's real OS hostname **and
resolve** cluster-wide:

- For machines whose OS Bootwright installs, the contract holds by
  construction: the installer writes the node FQDN as the OS hostname, and a
  `nameResolution` component the node's `NetworkConfig` references publishes a
  record for it.
- For `os.provided: true` machines, the operator guarantees it. If a machine's
  real hostname differs, author `name:` explicitly — it is taken verbatim.
  A mismatch passes `validate` (which never reaches the host) but fails
  `bootwright preflight`, which compares each storage node's real hostname
  against the declared node FQDN before cephadm ever sees the host spec.

The machine's own DNS name is separate: every `Machine` carries a `fqdn`
address (`<machineName>.<baseDomain>` by default) that Bootwright connects to
and that the node FQDN resolves through — see
[Machines](../concepts/machines.md#the-fqdn-address). A cluster-bound
node's OS hostname must equal its node FQDN, so
`hostname.source: machineName` on a `MachineInstallProfile` is valid only for
machines not bound to any cluster.

### Name resolution

The registered hostname must resolve from the mgr (and every node), or the
dashboard's monitoring integration logs errors such as *"Could not reach
Alertmanager's API on `http://<host>:9093` … Name or service not known"*. A
managed `nameResolution` (dnsmasq) component referenced by the nodes'
`NetworkConfig.nameResolutionRefs` publishes, for every machine it serves, a
`host-record` for the machine's `fqdn` name at its IP and a `cname` from
each node FQDN to the bound machine's `fqdn` (the bare machine label is not
published), and publishes each object gateway's composed public name at its
ingress VIP. Point each storage node's `NetworkConfig` `dns-resolver` at that
component. On provided (external) DNS the operator creates the records, and
the "Name resolution" preflight group names any missing one. See
[Networking](networking.md#name-resolution) for the resolver wiring and record
model.

## OSD device selection

Every `spec.ceph.topology.nodes[]` entry carrying the `osd` role must say which
disks it contributes: either the lean `devices:` list of literal paths or the
drivegroup-shaped `osd:` selection. `bootwright validate` rejects an osd-role
host that authors neither, and rejects either form on a host without the `osd`
role. There is no implicit all-devices default — handing every available
(blank) disk on a host to Ceph is the explicit opt-in
`osd: {dataDevices: {all: true}}`:

```yaml
nodes:
- machineRef: ceph-0
  name: node-01
  roles: [mon, mgr, osd]
  devices:            # literal paths ...
  - /dev/vdb
- machineRef: ceph-1
  name: node-02
  roles: [osd]
  osd:                # ... or a drivegroup-shaped selection
    dataDevices:
      all: true       # explicit opt-in: every available device becomes an OSD
```

The drivegroup form mirrors the cephadm OSD service spec field for field; the
[storage reference](../concepts/storage.md#osd-device-selection) carries the
exact field table.

A production drivegroup typically selects data disks by stable `model`/`vendor`
(not `/dev` paths, which reorder across boots) and carves the BlueStore DB onto a
shared NVMe, one slot per OSD:

```yaml
nodes:
- machineRef: ceph-0
  name: node-01
  roles: [osd]
  osd:
    dataDevices:
      model: MZ7LH3T8         # all SAS data disks of this model
      rotational: false
    dbDevices:
      paths: [/dev/nvme0n1]   # one fast device shared by every data OSD
    dbSlots: 8                # carve it into 8 equal DB slices
    encrypted: true
    tpm2: true                # seal the LUKS key in the host TPM
    unmanaged: false          # set true to freeze the drivegroup (no new claims)
```

On a mixed-disk host that needs distinct CRUSH classes per device, use the
expanded `pathSpecs` form so pool placement can tier onto a class:

```yaml
osd:
  dataDevices:
    pathSpecs:
    - {path: /dev/sdb, crushDeviceClass: ssd}
    - {path: /dev/sdc, crushDeviceClass: hdd}
```

## Production layout

Bootwright accepts small and single-node Ceph clusters for labs, but
`bootwright validate` emits **warnings** (it never blocks apply) when a cluster
departs from the layout Ceph recommends for production:

- **Monitors** — give the `mon` role to at least **3 hosts**, and keep the count
  **odd** (3, 5, 7) so the cluster holds quorum through a monitor failure.
- **Managers** — give the `mgr` role to at least **2 hosts** for an
  active/standby pair; a single manager is a single point of failure for
  orchestration, the dashboard, and metrics.

### Networks

`cephadm bootstrap` runs all Ceph traffic on the **public** network by default.
For production, IBM recommends a dedicated **cluster** network so OSD
replication, recovery, and heartbeat traffic does not contend with client I/O.
Declare both under `spec.ceph.networks`: the public CIDRs seed `public_network`
before the first monitor binds, and the cluster CIDRs render
`cephadm bootstrap --cluster-network`.

```yaml
ceph:
  networks:
    publicCIDRs:  [10.0.10.0/24]   # client + monitor traffic
    clusterCIDRs: [10.0.20.0/24]   # OSD replication/recovery (production)
```

Omit `clusterCIDRs` to keep IBM's default of one network carrying everything.

## FIPS

FIPS has two independent gates. `spec.ceph.security.fips` is the **cluster
gate**: it asserts that the storage cluster runs in FIPS mode. The **node
profile** is set on each storage node's `MachineInstallProfile` for machines
whose OS Bootwright installs, so the RHEL install comes up in FIPS mode.

The gate is a **one-way desired-state check**, run at validate time (before
apply), not a runtime probe of the hosts: when the cluster gate is on, every
managed-OS storage node's install profile must also enable FIPS, or validation
fails closed. It does not check the reverse direction — FIPS-enabled node
profiles under a non-FIPS cluster gate pass — and nodes whose OS Bootwright does
not install (`os.provided: true`) carry no install profile and are out of scope
for the check. There is no `fips-mode-setup` verification in the Ansible roles;
correctness of the running mode is the operator's responsibility on
provided-OS hosts.

FIPS is an install-time customization, so changing it on an installed machine is
a reinstall; see [managed-OS reinstall](operations.md#managed-os-reinstall-and-owned-ceph-rebuild).

## Stretch mode

`spec.ceph.topology.stretch` is enabled by presence and models a two-site
cluster with a tiebreaker monitor. Each `spec.ceph.topology.nodes[].site` is the
host's failure-domain bucket: outside stretch mode the failure domain is `host`
and `site` renders nothing; under stretch it becomes the host's cephadm CRUSH
location, and `placement.sites` (on monitoring, passthrough services, and the
gateway/filesystem surfaces) selects against it. Because it is inert otherwise,
`site` is required *exactly where it has effect* — when `stretch` is set or any
`placement` narrows by `sites` — and may be omitted everywhere else.

Two-site stretch comes with **fixed pool replication**: Ceph requires `size: 4`
/ `minSize: 2`. Every `StoragePool` without a `placementPolicyRef` inherits the
stretch CRUSH rule and that replication — including pools that pre-date the
stretch block. Author a `placementPolicyRef` only for genuinely divergent
placement, and note that erasure pools are not allowed on stretch-mode clusters.

The tiebreaker monitor lives at a third site:

```yaml
ceph:
  topology:
    stretch:
      tiebreaker:
        node: node-03             # the tiebreaker monitor node
    nodes:
    - machineRef: ceph-dc1-0
      name: node-01
      site: dc1
      roles: [mon, mgr, osd]
      devices: [/dev/vdb]
    - machineRef: ceph-dc2-0
      name: node-02
      site: dc2
      roles: [mon, mgr, osd]
      devices: [/dev/vdb]
    - machineRef: ceph-arbiter    # tiebreaker node -> node-03
      name: node-03
      site: dc3
      roles: [mon]
```

!!! warning "Authoring `stretch` on an existing cluster re-rules its pools"
    Adding `stretch` re-rules and resizes every policy-less pool on the next
    apply, with no change to any `StoragePool` file, and Ceph rebalances the data
    accordingly. `bootwright validate` prints a one-line notice naming the
    inheriting pools, and the resulting `ceph osd pool set` commands are visible
    in the rendered `ceph/operations.yaml`.

## The management gateway and HA dashboard

`spec.ceph.management` renders a native cephadm **management gateway**
(`mgmt-gateway`) fronted by a `keepalive_only` ingress, giving the Ceph
dashboard a single highly-available VIP that floats across the mgr hosts instead
of pinning operators to the active mgr's address. The VIP and its DNS name show
up in `bootwright cluster info`, and a managed `nameResolution` component
should publish that name.

You do not spell that name out. `spec.ceph.management.dnsLabel` is the leftmost
label only — Bootwright composes the published name as
`<dnsLabel>.<StorageCluster name>.<domains.storageClusters>`, the same
composition that gives Ceph nodes their FQDNs. `dnsLabel` defaults to `mgr`, so
a `ceph-ibm` cluster on `example.com` publishes `mgr.ceph-ibm.example.com`
untouched, and `dnsLabel: dashboard` publishes
`dashboard.ceph-ibm.example.com`. A dotted value is rejected: the cluster and
domain arms are not overridable per cluster.

The `ceph-ibm-libvirt-lab` and
`ceph-ibm-baremetal-redfish` [reference examples](examples.md) build the HA
dashboard end to end.

Two optional blocks secure the gateway:

- `spec.ceph.management.tls` (`certificateRef` + `keyRef`, both required
  together) supplies a real certificate for the gateway frontend (cephadm
  `ssl_certificate` / `ssl_certificate_key`). Without it the composed name
  serves a self-signed cert that browsers reject.
- `spec.ceph.management.enableAuth: true` puts the dashboard behind SSO and
  **requires** `spec.ceph.management.oauth2Proxy` — Bootwright deploys the
  cephadm `oauth2-proxy` daemon (`providerDisplayName`, `clientId`,
  `clientSecretRef`, `oidcIssuerUrl`, optional `redirectUrl`, `httpsAddress`,
  `allowlistDomains`, `cookieSecretRef`). `enableAuth` without `oauth2Proxy`, or
  an `oauth2Proxy` block without `enableAuth`, is rejected by `validate`.

The cert and OIDC secrets are read from their secrets and inlined only into a
`0600` spec on the seed host (applied separately from the static service spec),
so they never land in a locally-rendered file.

## RGW and ingress

`StorageObjectGateway` owns the RGW service, its storage-owned public S3
endpoint, and its ingress VIPs. `spec.public` is the public endpoint;
`spec.ceph.ingresses[]` are the concrete keepalived ingress VIPs, each with a
`prefixLength` and `virtualInterfaceNetworks[]` telling cephadm which site-local
subnet can host the VIP. The endpoint and ingress fields are detailed under
[Networking](networking.md#storage-rgw-endpoints); the gateway object itself is
in the [storage reference](../concepts/storage.md#storageobjectgateway).

For a stretch cluster, the site-local pattern is one `StorageObjectGateway` per
data site, each with `spec.ceph.placement.sites` and its ingress `placement.sites`
narrowed to that site, its own VIP, its own `firstVirtualRouterID`, and its own
`public.dnsLabel` — every RGW daemon, ingress VRRP group, and HAProxy backend set
then stays inside one data center, and each ODF cluster is pointed at the one
gateway local to it. Siblings may share a `realm`/`zoneGroup`/`zone` (or all
leave it unset) without that being RGW multisite — Bootwright never configures
cross-zone replication. A single cluster-wide gateway with one unnarrowed
ingress (or several site-scoped ingresses under one gateway) is also accepted
for a simpler, non-stretch-local topology, such as an L2 network extended
across both data centers; that shape does not give per-site backend locality.

## Pools, filesystems, and placement

`StoragePool` owns one Ceph pool (`replicated` or `erasure`),
`StorageFilesystem` owns a CephFS filesystem and its MDS placement, and
`StoragePlacementPolicy` holds reusable CRUSH placement plus replicated-pool
defaults that pools select with `placementPolicyRef`. The pool's structural
identity is its `type` and erasure profile: changing it is the only desired-state
change that rebuilds a live pool (data-destroying, `--converge-drifted` only); replicas,
CRUSH rule, and application reconcile in place. The full field tables are in the
[storage reference](../concepts/storage.md#storagepool).

## Exporting storage to Data Foundation

`StorageExport` exposes storage services for downstream consumers. The common
consumer is OpenShift Data Foundation in **external** mode: a Data Foundation
add-on declares a `storageExportAttachment` input effect, and a
`ClusterAddonBinding` supplies the `StorageExport` name for one installed
cluster. The `baremetal-redfish-imported-ceph-odf` and
`baremetal-redfish-multidc-virtualized-odf-ceph` [reference examples](examples.md)
wire export-to-ODF for imported and managed Ceph respectively. The export object
model is in the [storage reference](../concepts/storage.md#storageexport).

`spec.dataFoundation.objectGatewayRef` names the `StorageObjectGateway` ODF's
object storage should use; set it and the exporter hook passes that gateway's
public endpoint as `--rgw-endpoint`, so external mode also gets S3 alongside
RBD and CephFS. Leave it unset for a block/file-only export. Each ODF cluster
should get its own `StorageExport` naming its own site-local gateway, so
distinct OCP clusters never share one S3 endpoint.

## Convergence is additive-only

`apply` creates and converges what desired state declares and never removes a
live Ceph object whose declaration was deleted. The rule is storage-wide,
covering every surface: `spec.ceph.config` keys, `mgrModules[]`, `monitoring`
services, `services[]` passthrough entries, and the `StoragePool`,
`StorageFilesystem`, and `StorageObjectGateway` kinds. Deleting one from git and
running `bootwright apply` reconciles cleanly while the live pool, filesystem, or
service keeps running — remove it on the cluster with the `ceph`/`cephadm` CLI
when you mean it.

`--converge-drifted` does not prune undeclared objects either: it rebuilds only
still-declared objects whose structural identity changed: a pool's `type` and
erasure profile, or a CephFS metadata pool — changing the metadata pool is a
data-destroying `ceph fs rm` recreate, not an in-place reconcile. See
[Operations and recovery](operations.md#managed-os-reinstall-and-owned-ceph-rebuild)
for the owned-Ceph wipe-and-rebuild path.

!!! note "Storage sub-objects are not orphan-tracked"
    `bootwright diff` correlates whole clusters, machines, and providers,
    so a deleted pool, filesystem, gateway, or passthrough service inside a
    still-declared `StorageCluster` does not appear under **"Owned but no longer
    declared"**.

## Accessing a managed cluster

`bootwright cluster info` prints everything needed to reach a managed Ceph
cluster, derived entirely from desired state — the seed node SSH line, the
monitor list, a health-check command, the dashboard URL, and the dashboard
credential file path. Run the **Health check** line; `HEALTH_OK` from `ceph -s`
confirms the cluster is reachable and healthy.

When `spec.domains.storageClusters` is set, the dashboard URL is
`https://mgr.<cluster>.<domains.storageClusters>` instead of the seed node's
bare address, even without an explicit `spec.ceph.management` block — the same
`mgr.` alias convention the HA gateway uses below. Bootwright does not publish
that DNS record itself outside the HA gateway case; without a `nameResolution`
component (or another record) pointing it at the active mgr, resolve it
manually or fall back to the seed node's address.

`cephadm bootstrap` enables the dashboard and generates a one-time random
`admin` password. Bootwright captures it **during the install** and saves it on
the controller (`clusters/<storage-cluster>/secrets/dashboard-password`, mode
`0600`), exactly like the kubeadmin password for OpenShift clusters.

!!! note "Dashboard password is captured at install only"
    The password is captured solely from the one-time `cephadm bootstrap`. It is
    never re-read or re-synced on later applies, and — like every secret —
    `cluster info` shows its file path by default, revealing the bytes only
    when you pass `--secrets`. The file persists after the cluster is destroyed; delete the
    cluster's `secrets/` directory by hand if you want the credential gone. If
    the file is lost or the in-cluster password was changed, see
    [Recovering the Ceph dashboard password](../troubleshooting.md#recovering-the-ceph-dashboard-password).

## Migrating an existing cluster

Two desired-state changes carry data-movement risk on an already-bootstrapped
cluster:

- **Renaming a node's Ceph identity** — `topology.nodes[].name` is required and
  declared explicitly on every node. Setting it to a value cephadm has never
  registered makes the rendered topology name a host cephadm has never seen —
  cephadm would re-add it and move data. On an already-bootstrapped cluster set
  each `topology.nodes[].name` to the identity cephadm already registered
  before the next apply.
- **Authoring `stretch`** re-rules and resizes every policy-less pool, as
  described above. Confirm the validate notice naming the inheriting pools, and
  plan for the rebalance.

For the destructive rebuild paths (managed-OS reinstall, owned-Ceph
wipe-and-rebuild) and the `--converge-drifted` authorization model, see
[Operations and recovery](operations.md).
