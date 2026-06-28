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
`Environment.spec.entitlements[]` entry that resolves a Red Hat or IBM Ceph
entitlement; `oss` takes no entitlement. An `ibm/ibm-storage-ceph` entitlement
carries only the IBM registry and license and names a separate `redhat/rhel`
entitlement (via `rhelEntitlementRef`) for the RHEL subscription cephadm
registers each node with. The entitlement model — provider/product pairs,
license acceptance, and the credential plumbing — lives in
[Secrets and entitlements](../concepts/secrets.md#entitlements).

`spec.ceph.release` selects which release to install for the chosen
distribution. For `oss` it is an upstream release name (`squid`, `reef`,
`quincy`) or a full `x.y.z` version, which pins the package repository
reproducibly and — when `image` is unset — derives the matching
`quay.io/ceph/ceph:vX.Y.Z` daemon image. For `redhat` and `ibm` it is the
product stream (for example `9`). It defaults to the community default release
for `oss` and to stream `9` for `redhat`/`ibm`. To mirror upstream packages for
a disconnected `oss` install, set `spec.ceph.community.mirror` (it must stay
empty for `redhat`/`ibm`).

```yaml
ceph:
  distribution: redhat
  release: "9"
  entitlementRef: rhcs           # Environment.spec.entitlements[] name
  cephadm:
    bootstrap:
      host: ceph-0               # topology host cephadm bootstraps on
```

The `ceph-distribution-oss`, `ceph-distribution-redhat`, and
`ceph-distribution-ibm` [reference examples](examples.md) show each source end
to end, including the entitlement and license-acceptance workflow.

!!! note "Pin the image on `redhat`/`ibm`"
    Set `spec.ceph.image` to a digest-pinned reference for production. Left
    unset, the install uses the distribution-packaged `cephadm`'s default image
    tag, which floats, so the running Ceph version is not reproducible across
    re-installs.

## The bootstrap seed host

`spec.ceph.cephadm.bootstrap.host` names the topology host cephadm bootstraps on
(the seed node). The rendered `cephadm bootstrap --mon-ip` is always an address
of that host: the address named by `cephadm.bootstrap.addressRef`, falling back
to `cephadm.addressRef`, and finally the host machine's SSH address.

`bootstrap.host` may name a node by its `Machine` name or its registered
hostname; both resolve, and Bootwright canonicalizes them to the registered
hostname cephadm expects. The same is true of the other authored host
references — `placement.hosts[]` and `topology.stretch.tiebreaker.host`.

## Host identity and FQDN node naming

cephadm registers every `spec.ceph.topology.hosts[]` entry under its
`hostname`, which defaults to the fully-qualified
`<machineRef>.<storageClusterName>.<baseDomain>` — the OpenShift node-naming
convention generalized to Ceph nodes (e.g. `ceph-3.ceph-ibm.bootwright.test`).
The name is rendered verbatim into the cephadm host spec, is the name the mgr
dashboard uses to reach monitoring services (Prometheus, Grafana,
Alertmanager), and must equal the host's real OS hostname **and resolve**
cluster-wide:

- For machines whose OS Bootwright installs, the contract holds by
  construction: the installer writes the same fully-qualified name as the OS
  hostname, and a `nameResolution` component the node's `NetworkConfig`
  references publishes an A record for it.
- For `os.provided: true` machines, the operator guarantees it. If a machine's
  real hostname differs, author `hostname:` explicitly — it is taken verbatim.
  A mismatch passes `validate` (which never reaches the host) but fails
  `bootwright preflight`, which compares each storage node's real hostname
  against the declared topology hostname before cephadm ever sees the host spec.

To opt a node out of fully-qualified naming and keep the bare `Machine` name as
the hostname (the pre-FQDN behavior), set `hostname.source: machineName` on its
`MachineInstallProfile`; this drives both the OS hostname and the cephadm host
identity. An explicit `hostname:` on the topology host always wins over either
default.

### Name resolution

The registered hostname must resolve from the mgr (and every node), or the
dashboard's monitoring integration logs errors such as *"Could not reach
Alertmanager's API on `http://<host>:9093` … Name or service not known"*. A
managed `nameResolution` (dnsmasq) component referenced by the nodes'
`NetworkConfig.nameResolutionRefs` publishes, for every machine it serves, an A
record for both the fully-qualified hostname and the bare `Machine` name, and
publishes each object gateway's `public.dnsName` at its ingress VIP. Point each
storage node's `NetworkConfig` `dns-resolver` at that component. See
[Networking](networking.md) for the resolver wiring.

## OSD device selection

Every `spec.ceph.topology.hosts[]` entry carrying the `osd` role must say which
disks it contributes: either the lean `devices:` list of literal paths or the
drivegroup-shaped `osd:` selection. `bootwright validate` rejects an osd-role
host that authors neither, and rejects either form on a host without the `osd`
role. There is no implicit all-devices default — handing every available
(blank) disk on a host to Ceph is the explicit opt-in
`osd: {dataDevices: {all: true}}`:

```yaml
hosts:
- machineRef: ceph-0
  roles: [mon, mgr, osd]
  devices:            # literal paths ...
  - /dev/vdb
- machineRef: ceph-1
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
hosts:
- machineRef: ceph-0
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
gate**: it asserts that the storage cluster runs in FIPS mode and is verified at
apply time. The **node profile** is set on each storage node's
`MachineInstallProfile` for machines whose OS Bootwright installs, so the RHEL
install comes up in FIPS mode. Both must agree — a cluster gated FIPS on hosts
that did not install with the FIPS profile fails closed. FIPS is an
install-time customization, so changing it on an installed machine is a
reinstall; see [managed-OS reinstall](operations.md#managed-os-reinstall-and-owned-ceph-rebuild).

## Stretch mode

`spec.ceph.topology.stretch` is enabled by presence and models a two-site
cluster with a tiebreaker monitor. Each `spec.ceph.topology.hosts[].site` is the
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
        host: ceph-arbiter        # the tiebreaker monitor host
    hosts:
    - machineRef: ceph-dc1-0
      site: dc1
      roles: [mon, mgr, osd]
      devices: [/dev/vdb]
    - machineRef: ceph-dc2-0
      site: dc2
      roles: [mon, mgr, osd]
      devices: [/dev/vdb]
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
up in `bootwright cluster access`, and a managed `nameResolution` component
should publish that name. The `ceph-ibm-libvirt-lab` and
`ceph-ibm-baremetal-redfish` [reference examples](examples.md) build the HA
dashboard end to end.

Two optional blocks secure the gateway:

- `spec.ceph.management.tls` (`certificateRef` + `keyRef`, both required
  together) supplies a real certificate for the gateway frontend (cephadm
  `ssl_certificate` / `ssl_certificate_key`). Without it the published `dnsName`
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

## Pools, filesystems, and placement

`StoragePool` owns one Ceph pool (`replicated` or `erasure`),
`StorageFilesystem` owns a CephFS filesystem and its MDS placement, and
`StoragePlacementPolicy` holds reusable CRUSH placement plus replicated-pool
defaults that pools select with `placementPolicyRef`. The pool's structural
identity is its `type` and erasure profile: changing it is the only desired-state
change that rebuilds a live pool (data-destroying, `--override` only); replicas,
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

## Convergence is additive-only

`apply` creates and converges what desired state declares and never removes a
live Ceph object whose declaration was deleted. The rule is storage-wide,
covering every surface: `spec.ceph.config` keys, `mgrModules[]`, `monitoring`
services, `services[]` passthrough entries, and the `StoragePool`,
`StorageFilesystem`, and `StorageObjectGateway` kinds. Deleting one from git and
running `bootwright apply` reconciles cleanly while the live pool, filesystem, or
service keeps running — remove it on the cluster with the `ceph`/`cephadm` CLI
when you mean it.

`--override` does not prune undeclared objects either: it rebuilds only
still-declared pools whose structural identity (pool `type` and erasure profile)
changed. See [Operations and recovery](operations.md#managed-os-reinstall-and-owned-ceph-rebuild)
for the owned-Ceph wipe-and-rebuild path.

!!! note "Storage sub-objects are not orphan-tracked"
    `bootwright state-check` correlates whole clusters, machines, and providers,
    so a deleted pool, filesystem, gateway, or passthrough service inside a
    still-declared `StorageCluster` does not appear under **"Owned but no longer
    declared"**.

## Accessing a managed cluster

`bootwright cluster access` prints everything needed to reach a managed Ceph
cluster, derived entirely from desired state — the seed node SSH line, the
monitor list, a health-check command, the dashboard URL, and the dashboard
credential file path. Run the **Health check** line; `HEALTH_OK` from `ceph -s`
confirms the cluster is reachable and healthy.

`cephadm bootstrap` enables the dashboard and generates a one-time random
`admin` password. Bootwright captures it **during the install** and saves it on
the controller (`clusters/<storage-cluster>/secrets/dashboard-password`, mode
`0600`), exactly like the kubeadmin password for OpenShift clusters.

!!! note "Dashboard password is captured at install only"
    The password is captured solely from the one-time `cephadm bootstrap`. It is
    never re-read or re-synced on later applies, and — like every secret —
    `cluster access` only ever shows its file path and a `sudo cat` command,
    never the bytes. The file persists after the cluster is destroyed; delete the
    cluster's `secrets/` directory by hand if you want the credential gone. If
    the file is lost or the in-cluster password was changed, see
    [Recovering the Ceph dashboard password](../troubleshooting.md#recovering-the-ceph-dashboard-password).

## Migrating an existing cluster

Two desired-state changes carry data-movement risk on an already-bootstrapped
cluster:

- **Adopting FQDN naming** renames the Ceph host identity of every host that
  left `hostname` unauthored. On a live cluster that makes the rendered topology
  name a host cephadm has never seen — cephadm would re-add it and move data.
  FQDN naming is safe for greenfield bootstraps; on an already-bootstrapped
  cluster either pin `hostname:` to the original name on each host, or set
  `hostname.source: machineName` on its `MachineInstallProfile`, before the next
  apply.
- **Authoring `stretch`** re-rules and resizes every policy-less pool, as
  described above. Confirm the validate notice naming the inheriting pools, and
  plan for the rebalance.

For the destructive rebuild paths (managed-OS reinstall, owned-Ceph
wipe-and-rebuild) and the `--override` authorization model, see
[Operations and recovery](operations.md).
