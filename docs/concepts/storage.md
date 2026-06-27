---
title: Storage
description: StorageCluster and the Ceph sub-objects — managed cephadm or imported clusters, pools, filesystems, gateways, and exports.
---

# Storage

Storage is a separate domain from [`ContainerCluster`](container-clusters.md):
Ceph is never a cluster field, it is its own set of kinds the
[`Environment`](environment.md) selects and binds. Two operating modes share the
same `StorageCluster` kind:

- **Managed Ceph** (`management: managed`) — Bootwright bootstraps and converges
  Ceph on selected machines via **cephadm**, then applies pools, filesystems,
  RGW, and prepares downstream export data.
- **Imported / external Ceph** (`management: external`) — Bootwright consumes an
  operator-supplied cluster through a [`StorageExport`](#storageexport) and skips
  the cephadm storage task entirely.

A `StorageCluster` references machines **by node**: each topology host names a
[`Machine`](machines.md) with the `ceph-node` capability. A storage export can
then feed an [add-on](add-ons.md) input — for example an OpenShift Data
Foundation external-mode attachment that names a `StorageExport` for one
installed cluster.

See [conventions](index.md) for the object envelope and the Required/Default
field-table convention every table below follows.

!!! warning "Storage convergence is additive-only"
    Across the whole storage domain, `apply` creates and converges what desired
    state declares and **never** removes a live Ceph object whose declaration was
    deleted — pools, filesystems, gateways, passthrough services, mgr modules,
    and config keys keep running until removed on the cluster out of band.
    `apply --override` does not prune undeclared objects either; it rebuilds only
    still-declared pools whose structural identity changed. See
    [Operations and recovery](../advanced/operations.md) for removal patterns.

A minimal managed cluster and its OSS distribution:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ceph-oss
spec:
  type: ceph
  ceph:
    distribution: oss
    release: squid
    cephadm:
      bootstrap:
        host: ceph-0
    topology:
      hosts:
        - machineRef: ceph-0
          roles:
            - mon
```

An imported cluster carries no `ceph` block:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: imported-ceph
spec:
  type: ceph
  management: external
```

## StorageCluster

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.type` | Yes | — | Currently `ceph`. |
| `spec.management` | No | `managed` | `managed` or `external`. External (imported) clusters are consumed through `StorageExport` and skip the cephadm storage task entirely. |
| `spec.ceph` | When `management: managed` | — | Ceph distribution, cephadm bootstrap, networks, topology, config, monitoring, and passthrough services. |

!!! note "Cross-field rules"
    - `spec.ceph` is **required when** the cluster is managed and **must be
      empty** for external clusters.
    - A `Machine` is node-bound by at most one cluster (and at most one host
      entry) across every `ContainerCluster` and `StorageCluster`.

### Ceph

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `ceph.distribution` | No | `oss` | One of `oss`, `redhat`, or `ibm`. |
| `ceph.release` | No | `squid` (`oss`); `9` (`redhat`, `ibm`) | Ceph release for the chosen distribution. For `oss`, an upstream release name (`squid`, `reef`, `quincy`) or a full `x.y.z` version (for example `19.2.1`); a version pins the package repository and, when `ceph.image` is unset, derives the matching `quay.io/ceph/ceph:vX.Y.Z` image. For `redhat`/`ibm`, the product stream (for example `9`), selecting the `rhceph-<N>-tools` / `ibm-storage-ceph-<N>` repositories. |
| `ceph.image` | No | Derived from an `x.y.z` `oss` `ceph.release` when unset; otherwise none | Pins the exact cephadm daemon image as the default for every Ceph daemon. Must pin a version tag or a `sha256` digest (no mutable `:latest`). `redhat`/`ibm` tags are not `x.y.z`, so they pin here explicitly. |
| `ceph.community.mirror` | No | `https://download.ceph.com` | Upstream package base URL for mirrored or disconnected environments. `oss` only. |
| `ceph.entitlementRef` | When `redhat` or `ibm` | — | Names an `Environment.spec.entitlements[]` entry. Must resolve to a Red Hat Ceph (for `redhat`) or IBM Storage Ceph (for `ibm`) entitlement. Must be empty for `oss`. See [Secrets](secrets.md#entitlements). |
| `ceph.cephadm.addressRef` | No | — | Default address name used to resolve cephadm host addresses. |
| `ceph.cephadm.clusterSSHKeyRef` | No | the first topology host's `access.ssh` key | Names the `sshKeyPair` secret cephadm uses as its cluster identity — the key Bootwright authorizes on, and cephadm reaches, every host. Set it to decouple the cluster identity from how Bootwright connects to each node. |
| `ceph.cephadm.clusterSSHUser` | No | `root` when `clusterSSHKeyRef` is set; otherwise the first host's `access.ssh.user` | OS user cephadm manages every host as (`--ssh-user`); must exist on every host. |
| `ceph.cephadm.bootstrap.host` | Yes | — | Topology host that cephadm bootstraps on. |
| `ceph.cephadm.bootstrap.addressRef` | No | `ceph.cephadm.addressRef`, then the host machine's SSH address | Address used for the rendered cephadm `--mon-ip`, resolved in that fallback order. |
| `ceph.networks.publicCIDRs[]` | No | — | Public-network CIDRs (renders `public_network`). |
| `ceph.networks.clusterCIDRs[]` | No | — | Cluster-network CIDRs for replication and recovery traffic (renders `cluster_network`). |
| `ceph.security.fips.enabled` | No | `false` | `true` requires a `redhat` or `ibm` distribution and that **every** Ceph node's `MachineInstallProfile` sets `customizations.security.fips.enabled: true`. Ceph runs FIPS by running on FIPS-installed RHEL nodes — there is no cephadm FIPS flag. |
| `ceph.config` | No | — | Ceph config database options as `section -> key -> value`, rendered as idempotent `ceph config set` after bootstrap. |
| `ceph.mgrModules[]` | No | — | mgr modules to enable (`ceph mgr module enable`). |
| `ceph.monitoring` | No | cephadm default stack (block absent) | cephadm monitoring stack controls; see [Monitoring](#monitoring). |
| `ceph.services[]` | No | — | Raw cephadm service-spec passthrough for unmodeled service types; see [Passthrough services](#passthrough-services). |
| `ceph.topology` | Yes | — | Hosts, roles, OSD devices, sites, and stretch mode; see [Topology](#topology). |

!!! note "Cross-field rules"
    - `ceph.config` **rejects** `public_network` and `cluster_network` keys in
      any section — they are owned by `ceph.networks` (`publicCIDRs` /
      `clusterCIDRs`). Config sections are `global`, `mon`, `mgr`, `osd`, `mds`,
      `client`, or a `<type>.<id>` daemon section; option values must not be
      empty.
    - mgr module settings are declared in `ceph.config` under the `mgr` section
      (`mgr/<module>/<key>`), not on `mgrModules[]`.
    - Removed `config` keys, `mgrModules[]`, and `services[]` are **not** undone
      (additive-only).
    - `ceph.security.fips.enabled: true` requires distribution `redhat` or `ibm`
      (the community `oss` images are not FIPS-validated) and that every Ceph
      node Bootwright installs uses a `MachineInstallProfile` with
      `customizations.security.fips.enabled: true`. Provided-OS nodes (no
      install profile) are not checked and must be FIPS-installed out of band.

Distribution requirements:

| Distribution | Requirements |
| --- | --- |
| `oss` | Community package and image sources; `entitlementRef` must be empty; `community.mirror` may override `download.ceph.com`. |
| `redhat` | `entitlementRef` must resolve to a Red Hat Ceph entitlement; node OS must be RHEL 9.6, 9.7, 10, or 10.1. |
| `ibm` | `entitlementRef` must resolve to an IBM Storage Ceph entitlement with accepted license terms, which names a `redhat/rhel` entitlement via `rhelEntitlementRef` for the RHEL subscription; node OS must be RHEL 9.6, 9.7, 10, or 10.1. |

### Monitoring

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `monitoring.enabled` | No | `true` (when the `monitoring` block is present) | `false` renders the bootstrap `--skip-monitoring-stack` flag and no monitoring specs. |
| `monitoring.prometheus` | No | — | Per-service tuning; placement derives from the `prometheus` role. |
| `monitoring.grafana` | No | — | Per-service tuning; placement derives from the `grafana` role. |
| `monitoring.alertmanager` | No | — | Per-service tuning; placement derives from the `alertmanager` role. |
| `monitoring.nodeExporter` | No | every host (cephadm behavior) | node-exporter has no topology role; an authored block narrows by explicit placement only. |

!!! note "Absent versus present"
    Omitting the `monitoring` block deploys cephadm's **default** monitoring
    stack with cephadm's own placement. Authoring the block switches to
    role-derived placement (like `mon`/`mgr`), with `enabled` defaulting to
    `true`; `enabled: false` skips the stack.

Each monitoring-service block (`prometheus`, `grafana`, `alertmanager`,
`nodeExporter`) carries:

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `placement` | No | every host carrying the service's role | See [Shared placement](#shared-placement). |
| `port` | No | cephadm default | Service port. |
| `retentionTime` | No | cephadm default | Retention time (applies to Prometheus). |
| `retentionSize` | No | cephadm default | Retention size (applies to Prometheus). |

### Passthrough services

For cephadm service types Bootwright does not model first-class (for example
`nfs`, `loki`), `ceph.services[]` renders field-for-field into a `ceph orch
apply` document.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `services[].serviceType` | Yes | — | cephadm service type. |
| `services[].serviceID` | No | — | cephadm service ID. The `serviceType/serviceID` pair must be unique. |
| `services[].placement` | Yes | — | Must set `hosts` or `sites`; see [Shared placement](#shared-placement). |
| `services[].spec` | No | — | Raw service spec map, rendered 1:1 into cephadm. |

!!! warning "Do not duplicate first-class surfaces"
    Do not declare service types already owned by Bootwright surfaces —
    monitors, managers, OSDs, MDS, RGW, ingress, or the monitoring services.

### Topology

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `topology.hosts` | Yes | — | At least one host. |
| `topology.hosts[].machineRef` | Yes | — | `Machine` with the `ceph-node` capability and declared SSH access. |
| `topology.hosts[].hostname` | No | the `machineRef` name | cephadm host-spec hostname, rendered verbatim; must equal the host's actual hostname. |
| `topology.hosts[].site` | When stretch is enabled or any placement narrows by `sites` | — | Failure-domain bucket. Becomes the cephadm host-spec CRUSH location only in stretch mode; `placement.sites` selects against it. No effect otherwise. |
| `topology.hosts[].roles[]` | Yes | — | Ceph roles, such as `mon`, `mgr`, `osd`, `mds`, `rgw`, `prometheus`, `grafana`, `alertmanager`. Roles always become host labels. |
| `topology.hosts[].labels[]` | No | — | Additional free-form cephadm host labels (for example `_admin`). Must not duplicate a role. |
| `topology.hosts[].devices[]` | No | — | Literal OSD device paths; shorthand for `osd.dataDevices.paths`. Requires the `osd` role. Mutually exclusive with `osd`. |
| `topology.hosts[].osd` | No | — | Drivegroup-shaped OSD device selection; see [OSD device selection](#osd-device-selection). Requires the `osd` role. Mutually exclusive with `devices`. |

!!! note "Cross-field rules"
    - An `osd`-role host **must** select devices via `devices[]` or `osd`.
      Consuming all available devices is the explicit opt-in
      `osd: {dataDevices: {all: true}}`, never the omission default.
    - `devices[]` and `osd` are mutually exclusive (`devices[]` is the shorthand
      for `osd.dataDevices.paths`).

#### OSD device selection

All field names render 1:1 into the cephadm OSD drivegroup spec.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `osd.dataDevices` | Yes (when `osd` is set) | — | Data device selector. |
| `osd.dbDevices` | No | — | DB device selector. |
| `osd.walDevices` | No | — | WAL device selector. |
| `osd.filterLogic` | No | `AND` | How cephadm combines the device filters across selectors: `AND` (intersect) or `OR` (union). Spec-level (`filter_logic`). |
| `osd.encrypted` | No | `false` | Enable encrypted (LUKS) OSDs. |
| `osd.tpm2` | No | `false` | Seal the OSD LUKS key in the host TPM (`tpm2`). Requires `encrypted: true`. |
| `osd.osdsPerDevice` | No | cephadm default | OSDs per selected device (non-negative). |
| `osd.crushDeviceClass` | No | — | CRUSH device class for the whole drivegroup. |
| `osd.blockDBSize` | No | — | Per-OSD DB slice size carved from `dbDevices` (`block_db_size`, e.g. `60G`). |
| `osd.blockWALSize` | No | — | Per-OSD WAL slice size carved from `walDevices` (`block_wal_size`). |
| `osd.dbSlots` | No | — | Number of equal DB slices per shared `dbDevices` device (`db_slots`, non-negative). |
| `osd.walSlots` | No | — | Number of equal WAL slices per shared `walDevices` device (`wal_slots`, non-negative). |
| `osd.dataAllocateFraction` | No | `1` | Fraction `(0,1]` of each data device to allocate (`data_allocate_fraction`), reserving headroom. |
| `osd.unmanaged` | No | `false` | Freeze this OSD service so cephadm stops claiming new devices (`unmanaged`, a top-level service-spec key). |

Each device selector (`dataDevices`, `dbDevices`, `walDevices`) mirrors the
cephadm drivegroup device filter:

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `*.paths[]` | No | — | Literal device paths. |
| `*.pathSpecs[]` | No | — | Expanded path form `{path, crushDeviceClass}` pinning a per-device CRUSH class; renders to the cephadm `paths: [{path, crush_device_class}]` mapping. |
| `*.all` | No | `false` | Select all matching devices. |
| `*.model` | No | — | Device model filter (`model`). |
| `*.vendor` | No | — | Device vendor filter (`vendor`). |
| `*.rotational` | No | — | Rotational filter. |
| `*.size` | No | — | Size filter: a single size (`10G`) or a range (`10G:40G`, `:40G`, `10G:`). |
| `*.limit` | No | — | Cap on matching devices (non-negative). |

!!! note "Cross-field rules"
    - `paths`, `pathSpecs`, and `all` are **mutually exclusive** within one
      selector; set at most one. `pathSpecs[].path` is required.
    - A selector must select something: set at least one of `paths`, `pathSpecs`,
      `all`, `model`, `vendor`, `rotational`, `size`, or `limit`. `model`,
      `vendor`, `rotational`, `size`, and `limit` only narrow the match.
    - Address fleet disks by `model`/`vendor` rather than `/dev` paths, which can
      reorder across boots.

#### Stretch mode

Authoring the `stretch` block is the enablement signal — its presence turns on
stretch mode. Only `failureDomain` and the tiebreaker host are facts the
operator alone knows; normalize derives the rest from the topology.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `topology.stretch.failureDomain` | Yes (when stretch is enabled) | — | CRUSH failure domain mapping sites to real buckets. |
| `topology.stretch.dataSites[]` | No | the topology's non-tiebreaker sites | Must resolve to exactly the two mon-bearing data sites. Author only when extra OSD-only sites would be wrongly derived. |
| `topology.stretch.tiebreaker.host` | Yes (when stretch is enabled) | — | Mon-only host with no OSD devices, in the tiebreaker site. |
| `topology.stretch.tiebreaker.site` | No | the tiebreaker host's site | Must be distinct from `dataSites`. |
| `topology.stretch.ruleName` | No | `stretch-rule` | Stretch CRUSH rule inherited by policy-less stretch pools. |

!!! note "Fixed stretch replication"
    Stretch is supported only as two data sites plus one mon-only tiebreaker
    site. Policy-less replicated pools get `size: 4` / `minSize: 2` as a
    render-time constant — non-4/2 stretch is unsupported and the replication is
    not authorable. Erasure pools are not allowed on stretch-mode clusters. See
    [Ceph topologies](../advanced/ceph-topologies.md) for the full stretch
    walkthrough.

## StoragePlacementPolicy

Reusable placement and replicated-pool defaults for pools that select it via
`placementPolicyRef`.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.storageClusterRef` | Yes | — | Managed `StorageCluster`. |
| `spec.ceph.ruleName` | Yes | — | CRUSH rule name. |
| `spec.ceph.failureDomain` | No | — | CRUSH failure domain. |
| `spec.ceph.replicated.size` | No | Ceph default | Replica count. |
| `spec.ceph.replicated.minSize` | No | Ceph default | Minimum replicas to serve I/O. |

!!! note "Cross-field rule"
    A pool with `placementPolicyRef` set **must not** also set `ceph.replicated`;
    the referenced policy owns the pool's replication.

## StoragePool

Owns one Ceph pool. Deleting the object from desired state leaves the live pool
running (additive-only).

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.storageClusterRef` | Yes | — | Managed `StorageCluster`. |
| `spec.placementPolicyRef` | No | — | `StoragePlacementPolicy` (same cluster) supplying replicated defaults. |
| `spec.ceph.type` | No | `replicated` | Immutable data-protection strategy: `replicated` or `erasure`. |
| `spec.ceph.role` | No | — | `rbd`, `cephfs-metadata`, `cephfs-data`, or `rgw`. Drives `StorageExport` wiring and the inferred application (`rbd`→`rbd`, `cephfs-*`→`cephfs`, `rgw`→`rgw`). |
| `spec.ceph.application` | No | inferred from `role` | Overrides the inferred `ceph osd pool application enable` value. |
| `spec.ceph.replicated.size` | No | Ceph default | Replica count (`replicated` only). |
| `spec.ceph.replicated.minSize` | No | Ceph default | Minimum replicas (`replicated` only). |
| `spec.ceph.erasure.dataChunks` | Yes (when `type: erasure`) | — | Erasure `k`; must be positive. |
| `spec.ceph.erasure.codingChunks` | Yes (when `type: erasure`) | — | Erasure `m`; must be positive. |

!!! note "Cross-field rules"
    - `type: replicated` must not set `erasure`; `type: erasure` must not set
      `replicated` and requires `erasure`.
    - Erasure pools are not allowed on stretch-mode clusters; on a stretch
      cluster any authored `replicated.size`/`minSize` must be `4`/`2`.
    - The pool's structural identity is its `type` and erasure profile. Changing
      it is the only desired-state change that rebuilds a live pool
      (data-destroying, `apply --override` only); replicas, CRUSH rule, and
      application reconcile in place.

A role-typed RBD pool selecting its cluster:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StoragePool
metadata:
  name: odf-rbd
spec:
  storageClusterRef: ceph-storage
  ceph:
    role: rbd
```

## StorageFilesystem

Owns one CephFS filesystem and its MDS placement. Deleting the object from
desired state leaves the live filesystem running (additive-only).

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.storageClusterRef` | Yes | — | Managed `StorageCluster`. |
| `spec.cephfs.metadataPoolRef` | Yes | — | Metadata `StoragePool`. |
| `spec.cephfs.dataPoolRefs[]` | Yes | — | One or more data pools, each authored as a plain pool name (the `{name, default}` object form is only for electing the default). |
| `spec.cephfs.dataPoolRefs[].name` | Yes (object form) | — | Data pool name. |
| `spec.cephfs.dataPoolRefs[].default` | No | `true` for a single entry; otherwise `false` | Marks the default data pool. |
| `spec.cephfs.mds.activeCount` | No | Ceph default | Active MDS count. |
| `spec.cephfs.mds.placement` | No | every host carrying the `mds` role | MDS service placement; see [Shared placement](#shared-placement). |

!!! note "Cross-field rule"
    A single data pool becomes the default automatically. With multiple data
    pools you must mark **exactly one** as `default: true`.

## StorageObjectGateway

Owns one RGW service and its ingress endpoints. Deleting the object from desired
state leaves the live service running (additive-only).

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.storageClusterRef` | Yes | — | Managed `StorageCluster` (external clusters reject Bootwright-managed gateways). |
| `spec.public.dnsName` | Yes | — | Public S3 endpoint DNS name. |
| `spec.public.scheme` | No | — | Public endpoint scheme. |
| `spec.public.port` | No | — | Public endpoint port. |
| `spec.ceph.serviceID` | Yes | — | RGW service ID. |
| `spec.ceph.placement` | No | every host carrying the `rgw` role | RGW placement; see [Shared placement](#shared-placement). |
| `spec.ceph.frontendPort` | No | cephadm default | RGW frontend port (0–65535). |
| `spec.ceph.ingresses[]` | No | — | cephadm ingress VIPs. |
| `spec.ceph.ingresses[].name` | Yes (per entry) | — | Ingress name; unique within the gateway. |
| `spec.ceph.ingresses[].address` | Yes (per entry) | — | VIP address. |
| `spec.ceph.ingresses[].prefixLength` | Yes (per entry) | — | VIP prefix length. |
| `spec.ceph.ingresses[].virtualInterfaceNetworks[]` | No | — | Renders verbatim to the cephadm ingress `virtual_interface_networks`. |
| `spec.ceph.ingresses[].placement` | No | every host carrying the `ingress` role | Ingress placement; see [Shared placement](#shared-placement). |

!!! note "Storage owns the endpoint"
    RGW public endpoints and ingress VIPs are owned by the storage gateway, not
    by `ContainerCluster`. Downstream consumers reference the gateway. See
    [Networking](../advanced/networking.md).

## Shared placement

`StoragePlacement` selects where a Ceph service runs. It appears on monitoring
services, passthrough services, MDS, RGW, and ingress.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `placement.hosts[]` | No | — | Explicit topology hostnames; narrows below site granularity. |
| `placement.sites[]` | No | — | Topology sites; narrows to hosts in the named sites. |
| `placement.countPerHost` | No | — | Renders to the cephadm `count_per_host` (non-negative). |

When `placement` is omitted, a service defaults to every topology host carrying
that service's role. Passthrough services are the exception: their placement
must set `hosts` or `sites`.

## StorageExport

`StorageExport` owns storage surfaces prepared for downstream consumers, such as
OpenShift Data Foundation external mode. The export name is what an
[add-on](add-ons.md) binding supplies to a `storageExportAttachment` input.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.type` | Yes | — | Currently `dataFoundation`. |
| `spec.storageClusterRef` | Yes | — | Imported or managed `StorageCluster`. |
| `spec.dataFoundation` | When `storageClusterRef` is managed Ceph | — | References managed storage services to export; see [Data Foundation](#data-foundation). |
| `spec.externalDetails` | When `storageClusterRef` is external Ceph | — | How external-cluster details are supplied; see [External details](#external-details). |

!!! note "Cross-field rules"
    - For a **managed** `storageClusterRef`, `dataFoundation` is required.
    - For an **external** `storageClusterRef`, `externalDetails` is required and
      `dataFoundation` must be empty.

A managed export wiring the RBD pool, CephFS filesystem, and RGW gateway:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageExport
metadata:
  name: odf-external-ceph
spec:
  type: dataFoundation
  storageClusterRef: ceph-storage
  dataFoundation:
    rbdPoolRef: odf-rbd
    filesystemRef: odf-cephfs
    objectGatewayRef: odf-rgw
```

An external export taking operator-supplied details from a secret:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: StorageExport
metadata:
  name: imported-ceph-odf
spec:
  type: dataFoundation
  storageClusterRef: imported-ceph
  externalDetails:
    fromSecretRef: imported-ceph-odf-details
```

### Data Foundation

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `dataFoundation.rbdPoolRef` | Yes | — | RBD `StoragePool` (same cluster). |
| `dataFoundation.filesystemRef` | Yes | — | CephFS `StorageFilesystem` (same cluster). |
| `dataFoundation.objectGatewayRef` | No | — | RGW `StorageObjectGateway` (same cluster). |

### External details

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `externalDetails.fromSecretRef` | One of three arms | — | Secret with operator-supplied external-cluster details. |
| `externalDetails.generated` | One of three arms | — | Generate details for managed storage (empty object; must be empty for external Ceph). |
| `externalDetails.sshExecution` | One of three arms | — | Gather external details over SSH; see below. |

!!! note "Exactly one arm"
    `externalDetails` must set **exactly one** of `fromSecretRef`, `generated`,
    or `sshExecution`. For managed clusters, generated details are produced
    during the storage apply.

`sshExecution` gathers external-cluster details by running an exporter over SSH:

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `sshExecution.machineRefs[]` | When `storageClusterRef` is external Ceph | — | `ceph-admin` machines (with SSH access) used to gather details. |
| `sshExecution.timeout` | No | — | Go duration such as `10m`, `30m`, or `1h`. |
| `sshExecution.exporter.source` | Yes | — | Exporter source (the bound Data Foundation add-on). |
| `sshExecution.config.format` | No | — | Currently `json` when set. |
| `sshExecution.config.rbdDataPoolName` | Yes | — | RBD data pool name. |
| `sshExecution.config.radosNamespace` | No | — | RADOS namespace. |
| `sshExecution.config.rbdMetadataECPoolName` | No | — | RBD metadata EC pool name. |
| `sshExecution.config.cephfsFilesystemName` | No | — | CephFS filesystem name. |
| `sshExecution.config.cephfsDataPoolName` | No | — | CephFS data pool name. |
| `sshExecution.config.cephfsMetadataPoolName` | No | — | CephFS metadata pool name. |
| `sshExecution.config.rgwEndpoint` | No | — | RGW endpoint. |
| `sshExecution.config.rgwPoolPrefix` | No | — | RGW pool prefix. |
| `sshExecution.config.monitoringEndpoint[]` | No | — | Monitoring endpoints (entries must not be empty). |
| `sshExecution.config.monitoringEndpointPort` | No | — | Monitoring port (0–65535). |
| `sshExecution.config.clusterName` | When `restrictedAuthPermission` is true | — | Storage cluster name for the exported details. |
| `sshExecution.config.k8sClusterName` | No | — | Kubernetes cluster name for the exported details. |
| `sshExecution.config.restrictedAuthPermission` | No | `false` | Restrict the exported auth permission. |

## Where to go next

- [Ceph topologies](../advanced/ceph-topologies.md) — stretch clusters, FIPS,
  the HA dashboard, and importing an existing cluster.
- [The Ceph lab](../getting-started/ceph.md) — a guided first managed-Ceph
  apply.
- [Add-ons](add-ons.md) — how a `StorageExport` feeds a Data Foundation
  external-mode attachment.
