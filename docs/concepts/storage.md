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
    `apply --converge-drifted` does not prune undeclared objects either; it rebuilds only
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
    release: "20.2.2"
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
| `ceph.release` | No | `20.2.2` (`oss`); `9.1` (`redhat`); `9.9.1` (`ibm`) | Ceph release for the chosen distribution. `oss` accepts active Tentacle (`tentacle`/`20.2.x`) and Squid (`squid`/`19.2.x`) releases; exact versions pin the package repository and derive `quay.io/ceph/ceph:vX.Y.Z`. `redhat` accepts `9.0` or `9.1`; `ibm` accepts `9.9.1`. Bare `9` is a current-stream alias normalized to the distribution's exact default. Omitted and alias values track the catalog and can create override-gated structural drift when that default advances; pin an exact release to avoid implicit upgrades. |
| `ceph.image` | No | Derived from an `x.y.z` `oss` `ceph.release` when unset; otherwise none | Pins the exact cephadm daemon image as the default for every Ceph daemon. Must pin a version tag or a `sha256` digest (no mutable `:latest`). A `redhat` or `ibm` image must use that distribution and release's canonical repository. |
| `ceph.community.mirror` | No | `https://download.ceph.com` | HTTPS upstream package base URL for mirrored or disconnected environments. `oss` only. |
| `ceph.entitlementRef` | When `redhat` or `ibm` | — | Names an `Entitlement` object. Must resolve to a `redhat-ceph` (for `redhat`) or `ibm-storage-ceph` (for `ibm`) entitlement. Must be empty for `oss`. See [Secrets](secrets.md#entitlements). |
| `ceph.ibm.callHome` | When `ibm` | — | Explicit IBM Call Home outbound-communication intent: `enabled` or `disabled`. License acceptance enables Call Home by default, so omission is rejected. |
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

!!! warning "Release/image fields are install-time intent, not a day-2 upgrade"
    `ceph.release` and `ceph.image` select what cephadm bootstraps. Changing them
    on a live cluster is drift, and the only in-band resolution is a rebuild
    (`apply --converge-drifted` runs `cephadm rm-cluster --zap-osds` and re-bootstraps —
    data-destroying). Upgrade a running cluster out of band with `cephadm`/`ceph
    orch upgrade`; the desired state then names the old release, so `diff`
    reports drift until a future apply refreshes the record. Adopting an
    out-of-band upgrade into the recorded desired state is an open design item.

!!! note "Cross-field rules"
    - `ceph.config` **rejects** `public_network` and `cluster_network` keys in
      any section — they are owned by `ceph.networks` (`publicCIDRs` /
      `clusterCIDRs`). Config sections are `global`, `mon`, `mgr`, `osd`, `mds`,
      `client`, or a `<type>.<id>` daemon section, optionally suffixed with a
      single CRUSH mask — `<section>/class:<class>` or
      `<section>/<crush-bucket-type>:<value>` (e.g. `osd/class:ssd`,
      `osd/rack:r1`) for per-class or per-location tuning. Option values must
      not be empty.
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
| `redhat` | `entitlementRef` resolves to `redhat-ceph`. Release `9.0` supports RHEL 9.6, 9.7, 10, 10.0, or 10.1; release `9.1` supports RHEL 9.8 or 10.2. |
| `ibm` | `entitlementRef` resolves to `ibm-storage-ceph` with accepted license terms; the RHEL subscription is named by the nodes' `MachineInstallProfile.spec.subscription` or the cluster `osSubscriptionRef`. Release `9.9.1` supports RHEL 9.8 or 10.2. `ibm.callHome` is required. |

When `Entitlement.spec.registry.url` overrides the vendor namespace,
`ceph.image` must explicitly pin the canonical vendor repository below that
mirror root. For release stream `9`, examples are
`mirror.example.test/vendor/rhceph/rhceph-9-rhel9:<tag>` for Red Hat and
`mirror.example.test/vendor/ibm-ceph/ceph-9-rhel9:<tag>` for IBM. The registry
override controls credentials and trust; it does not permit an arbitrary image
repository below the namespace.

### Monitoring

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `monitoring.enabled` | No | `true` (when the `monitoring` block is present) | `false` renders the bootstrap `--skip-monitoring-stack` flag and no monitoring specs. |
| `monitoring.prometheus` | No | — | Per-service tuning; placement derives from the `prometheus` role. |
| `monitoring.grafana` | No | — | Per-service tuning; placement derives from the `grafana` role. |
| `monitoring.alertmanager` | No | — | Per-service tuning; placement derives from the `alertmanager` role. |
| `monitoring.nodeExporter` | No | every host (cephadm behavior) | node-exporter has no topology role; an authored block narrows by explicit placement only. |
| `monitoring.loki` | No | — | Centralized-logging aggregator (`service_type: loki`); role-less, placement authored explicitly. cephadm provisions Grafana's Loki datasource itself; no dashboard mgr command is emitted. `retentionTime`/`retentionSize` do not apply (cephadm's `MonitoringSpec` rejects them). |
| `monitoring.promtail` | No | — | Log shipper (`service_type: promtail`); role-less. Ships to loki, so it has no dashboard wiring. |

!!! note "Absent versus present"
    Omitting the `monitoring` block deploys cephadm's **default** monitoring
    stack with cephadm's own placement. Authoring the block switches to
    role-derived placement (like `mon`/`mgr`), with `enabled` defaulting to
    `true`; `enabled: false` skips the stack.

Each monitoring-service block (`prometheus`, `grafana`, `alertmanager`,
`nodeExporter`, `loki`, `promtail`) carries:

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `placement` | No | every host carrying the service's role | See [Shared placement](#shared-placement). |
| `port` | No | cephadm default | Service port. |
| `retentionTime` | No | cephadm default | Retention time (Prometheus only; `retention_time`/`retention_size` exist only on cephadm's `PrometheusSpec`, and every other monitoring service rejects the keys). |
| `retentionSize` | No | cephadm default | Retention size (Prometheus only). |
| `networks` | No | — | Bind the service to one or more CIDRs (cephadm `networks`), e.g. a dedicated management VLAN on multi-homed nodes. |

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
    monitors, managers, OSDs, MDS, RGW, NFS, ingress, or the monitoring
    services.

### Topology

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `topology.hosts` | Yes | — | At least one host. |
| `topology.hosts[].machineRef` | Yes | — | `Machine` with the `ceph-node` capability and declared SSH access. |
| `topology.hosts[].hostname` | No | `<machineRef>.<cluster>.<baseDomain>` (the bare `machineRef` name when `baseDomain` is unset, or when the node is provided-OS or its `hostname.source` is `machineName`) | cephadm host-spec hostname, rendered verbatim; must equal the host's actual hostname. |
| `topology.hosts[].site` | When stretch is enabled or any placement narrows by `sites` | — | Failure-domain bucket. Becomes the cephadm host-spec CRUSH location only in stretch mode; `placement.sites` selects against it. No effect otherwise. |
| `topology.hosts[].roles[]` | Yes | — | Ceph roles, such as `mon`, `mgr`, `osd`, `mds`, `rgw`, `prometheus`, `grafana`, `alertmanager`. Roles always become host labels. |
| `topology.hosts[].labels[]` | No | — | Additional free-form cephadm host labels (for example `_admin`). Must not duplicate a role. |
| `topology.hosts[].devices[]` | No | — | Literal OSD device paths; shorthand for `osd.dataDevices.paths`. Requires the `osd` role. Mutually exclusive with `osd`. |
| `topology.hosts[].osd` | No | — | Drivegroup-shaped OSD device selection; see [OSD device selection](#osd-device-selection). Requires the `osd` role. Mutually exclusive with `devices`. |
| `topology.osdDrivegroups[]` | No | — | Fleet OSD specs spanning many hosts; see [Fleet OSD drivegroups](#fleet-osd-drivegroups). |

!!! note "Cross-field rules"
    - An `osd`-role host **must** select devices via `devices[]`, `osd`, or a
      fleet `osdDrivegroups[]` entry that covers it. Consuming all available
      devices is the explicit opt-in `osd: {dataDevices: {all: true}}`, never the
      omission default.
    - `devices[]` and `osd` are mutually exclusive (`devices[]` is the shorthand
      for `osd.dataDevices.paths`).
    - A host is owned by **one** OSD spec: a host covered by a fleet
      `osdDrivegroups[]` entry must not also author per-host `devices[]`/`osd`,
      and no two fleet entries may claim the same host.

#### Fleet OSD drivegroups

For homogeneous racks, `topology.osdDrivegroups[]` renders **one** cephadm OSD
service spanning many hosts (the dominant declarative cephadm idiom) instead of
one spec per host. Per-host `hosts[].osd` remains the override for heterogeneous
nodes.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `serviceID` | Yes | — | cephadm OSD service ID; unique across drivegroups. |
| `placement` | No | every `osd`-role host | Narrow the span by `sites`/`hosts`; see [Shared placement](#shared-placement). |
| `osd` | Yes | — | The drivegroup-shaped selection (same fields as [OSD device selection](#osd-device-selection)). |

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
| `osd.serviceOverrides` | No | — | cephadm common service-spec escape hatch: `extraContainerArgs[]`, `extraEntrypointArgs[]`, `networks[]` (CIDRs), and `customConfigs[]` (`{mountPath, content}`, absolute mount path). Rendered as top-level service-spec keys. |

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

!!! warning "Stable device addressing"
    Kernel `/dev/sdX` / `/dev/vdX` names are **not stable** — they can reorder
    across boots, so a name recorded on one boot may point at a different disk on
    the next. Bootwright's OSD ownership marker and the empty-device / destroy
    gates key on the **literal** path string, so an unstable name can make them
    reason about the wrong disk. For explicitly-addressed OSDs prefer a stable
    path — `/dev/disk/by-id/...`, `/dev/disk/by-path/...`, or a `wwn-...` link —
    which is accepted anywhere a path is (`devices[]`, `dataDevices.paths`,
    `pathSpecs[].path`). For homogeneous fleets, a `model`/`vendor`/`size`/
    `rotational` filter avoids per-disk paths entirely.

    There is deliberately **no `wwn`/`serial` filter field** — the selector
    filters mirror cephadm exactly (`model`, `vendor`, `rotational`, `size`,
    `limit`). Per-disk stable selection is expressed as a `/dev/disk/by-id` or
    `wwn` **path** in `paths`/`pathSpecs`, not as a filter.

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
| `spec.ceph.crushDeviceClass` | No | — | Pin the replicated rule to one device class (`ssd`/`hdd`/`nvme`), the trailing argument of `crush rule create-replicated`. Fixed at rule creation; route to a different class with a new `ruleName`. |
| `spec.ceph.replicated.size` | No | Ceph default | Replica count. |
| `spec.ceph.replicated.minSize` | No | Ceph default | Minimum replicas to serve I/O. |

!!! note "Cross-field rule"
    A pool with `placementPolicyRef` set **must not** also set `ceph.replicated`;
    the referenced policy owns the pool's replication.

    For `failureDomain: host`, the effective replica size cannot exceed the
    number of topology hosts carrying the `osd` role. Effective `minSize`
    cannot exceed `size`.

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
| `spec.ceph.erasure.plugin` | No | Ceph default (`jerasure`) | EC plugin: `jerasure`, `isa`, `clay`, `lrc`, or `shec`. |
| `spec.ceph.erasure.technique` | No | — | Plugin-specific coding technique (`technique`, e.g. `reed_sol_van`). |
| `spec.ceph.erasure.crushDeviceClass` | No | — | Tier the EC pool onto a device class (`crush-device-class`). |
| `spec.ceph.erasure.crushRoot` | No | — | CRUSH subtree root for the profile (`crush-root`). |
| `spec.ceph.erasure.stripeUnit` | No | — | Per-chunk stripe size (`stripe_unit`, e.g. `4K`). |
| `spec.ceph.erasure.parameters` | No | — | Remaining `erasure-code-profile set` key=value pairs (`l`, `c`, `d`, `w`, `packetsize`, …), rendered verbatim. Keys owned by the fields above or the derived `crush-failure-domain` are rejected. |
| `spec.ceph.autoscale` | No | cephadm default | PG autoscaler intent; see [Pool tuning](#pool-tuning). |
| `spec.ceph.quota` | No | no limit | Pool quota; see [Pool tuning](#pool-tuning). |
| `spec.ceph.compression` | No | — | Inline compression; see [Pool tuning](#pool-tuning). |

!!! note "Cross-field rules"
    - `type: replicated` must not set `erasure`; `type: erasure` must not set
      `replicated` and requires `erasure`.
    - Erasure pools are not allowed on stretch-mode clusters; on a stretch
      cluster any authored `replicated.size`/`minSize` must be `4`/`2`.
    - Outside stretch mode, host-domain replica size and erasure `k+m` cannot
      exceed the number of OSD hosts. With `singleHostDefaults: true`, a
      policy-less pool uses cephadm's OSD-level `2/1` defaults and provisioning
      requires at least two in OSDs before pool creation.
    - Effective replicated `minSize` cannot exceed `size`.
    - The pool's structural identity is its `type` and erasure profile. Changing
      it is the only desired-state change that rebuilds a live pool
      (data-destroying, `apply --converge-drifted` only); replicas, CRUSH rule, and
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

### Pool tuning

The autoscaler, quota, and compression blocks render their fields as idempotent
`ceph osd pool set` / `set-quota` operations. Each reconciles in place
(last-write-wins) and none is part of the pool's structural identity, so they
never trigger a rebuild. `pg_num`/`pgp_num` are deliberately not modeled — the
autoscaler owns them.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `autoscale.mode` | No | cephadm default | `pg_autoscale_mode`: `on`, `off`, or `warn`. |
| `autoscale.targetSizeRatio` | No | — | `target_size_ratio` capacity hint. Mutually exclusive with `targetSizeBytes`. |
| `autoscale.targetSizeBytes` | No | — | `target_size_bytes` capacity hint (e.g. `10G`). |
| `autoscale.pgNumMin` | No | — | `pg_num_min` lower bound. |
| `autoscale.pgNumMax` | No | — | `pg_num_max` upper bound. |
| `autoscale.bulk` | No | — | `bulk`: start the pool large. |
| `quota.maxBytes` | No | no limit | `set-quota max_bytes`. An authored `0` is the native "no limit"; omit to leave a live quota untouched. |
| `quota.maxObjects` | No | no limit | `set-quota max_objects`. Same `0`/omit semantics. |
| `compression.mode` | No | — | `compression_mode`: `none`, `passive`, `aggressive`, or `force`. Required to set any other compression field. |
| `compression.algorithm` | No | — | `compression_algorithm`: `lz4`, `snappy`, `zlib`, or `zstd`. |
| `compression.requiredRatio` | No | — | `compression_required_ratio` `(0,1]`. |
| `compression.minBlobSize` | No | — | `compression_min_blob_size` (e.g. `8K`). |
| `compression.maxBlobSize` | No | — | `compression_max_blob_size`. |
| `mirroring.mode` | No | — | Enable RBD mirroring (`rbd mirror pool enable`): `image` or `pool`. Additive-only (never disabled). Deploy the `rbd-mirror` daemon via `spec.ceph.services[]`; peer setup is out of scope today. |

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
| `spec.cephfs.mds.activeCount` | No | Ceph default | Active MDS count (`max_mds`). |
| `spec.cephfs.mds.standbyReplay` | No | `false` | Enable hot standby-replay MDS (`allow_standby_replay`), the standard production HA posture. |
| `spec.cephfs.mds.standbyCountWanted` | No | Ceph default | Standby daemons the cluster wants (`standby_count_wanted`, non-negative). |
| `spec.cephfs.mds.placement` | No | every host carrying the `mds` role | MDS service placement; see [Shared placement](#shared-placement). |
| `spec.cephfs.mds.serviceSpec.unmanaged` | No | `false` | Freeze the MDS daemon set (`unmanaged`). |
| `spec.cephfs.mds.serviceSpec.extraContainerArgs[]` | No | — | `extra_container_args` for the daemon container. |
| `spec.cephfs.mds.serviceSpec.extraEntrypointArgs[]` | No | — | `extra_entrypoint_args` for the daemon. |
| `spec.cephfs.mds.serviceSpec.networks[]` | No | — | Bind the daemon to CIDRs (`networks`). |
| `spec.cephfs.subvolumeGroups[]` | No | — | Static subvolume groups; see [Subvolume groups](#subvolume-groups). |

!!! note "Cross-field rule"
    A single data pool becomes the default automatically. With multiple data
    pools you must mark **exactly one** as `default: true`.

!!! warning "Changing the metadata pool or default data pool recreates the filesystem"
    The CephFS metadata pool and its default data pool are part of the
    filesystem's structural identity — Ceph cannot move a live CephFS to a
    different metadata or default data pool — so changing
    `spec.cephfs.metadataPoolRef`, or which `dataPoolRefs[]` entry is the default,
    is a data-destroying, `apply --converge-drifted`-only recreate (`ceph fs rm` then
    recreate), not an in-place reconcile.

### Subvolume groups

`spec.cephfs.subvolumeGroups[]` declares the static `ceph fs subvolumegroup
create` boundaries that CSI and other tools provision subvolumes into.
Individual subvolumes are out of scope (apps/CSI own those). Additive-only: a
removed group keeps running.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `name` | Yes | — | Subvolume group name; unique within the filesystem. |
| `poolLayoutRef` | No | — | `StoragePool` (same cluster) for the group's data layout (`--pool_layout`). |
| `mode` | No | — | Directory mode in octal (`--mode`, e.g. `0755`). |
| `uid` | No | — | Owner UID (`--uid`, non-negative). |
| `gid` | No | — | Owner GID (`--gid`, non-negative). |
| `sizeBytes` | No | — | Group quota in bytes (`--size`, non-negative). |

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
| `spec.ceph.realm` | No | implicit default | Multisite realm (`rgw_realm`). Set with `zoneGroup` and `zone` (all-or-nothing). Bootwright creates the realm/zonegroup/zone and commits the period; even a single site benefits from a stable named zone. |
| `spec.ceph.zoneGroup` | No | — | Multisite zonegroup (`rgw_zonegroup`). |
| `spec.ceph.zone` | No | — | Multisite zone (`rgw_zone`). |
| `spec.ceph.config` | No | — | Per-RGW options applied as `ceph config set client.rgw.<serviceID>`. Values must not be empty; `rgw_frontend_port` is owned by `frontendPort`; a key must not also appear in the cluster `config[client.rgw.<serviceID>]` section. |
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

## StorageNFSExport

Owns one cephadm NFS-Ganesha service and its exports. Deleting the object from
desired state leaves the live service and exports running (additive-only). cephadm
auto-provisions the backing `.nfs` pool, so no pool/namespace is modeled.

| Field | Required | Default | Description |
| --- | --- | --- | --- |
| `spec.storageClusterRef` | Yes | — | Managed `StorageCluster`. |
| `spec.ceph.serviceID` | Yes | — | NFS service ID (`--cluster-id`). |
| `spec.ceph.placement` | Yes | — | Must set `hosts` or `sites` (there is no `nfs` topology role); see [Shared placement](#shared-placement). |
| `spec.ceph.ingresses[]` | No | — | Ingress VIPs fronting `nfs.<serviceID>` (same shape as the RGW gateway ingress). |
| `spec.exports[]` | No | — | NFS exports; each renders an idempotent `ceph nfs export create`. |
| `spec.exports[].pseudo` | Yes (per entry) | — | NFSv4 pseudo path (`--pseudo-path`); unique within the service. |
| `spec.exports[].filesystemRef` | One of two FSALs | — | CephFS export (`--fsname`); a `StorageFilesystem` in the same cluster. |
| `spec.exports[].path` | No | `/` | Directory within the filesystem (`--path`). |
| `spec.exports[].bucket` | One of two FSALs | — | RGW export (`--bucket`). |
| `spec.exports[].accessType` | No | — | `RW`, `RO` (renders `--readonly`), or `NONE`. |
| `spec.exports[].squash` | No | — | NFS squash mode (`--squash`, e.g. `no_root_squash`). |
| `spec.exports[].clients[]` | No | — | Restrict access by address/CIDR (`--client-addr`). |

!!! note "Cross-field rule"
    Each export sets **exactly one** of `filesystemRef` (CephFS, FSAL CEPH) or
    `bucket` (RGW, FSAL RGW).

## Shared placement

`StoragePlacement` selects where a Ceph service runs. It appears on monitoring
services, passthrough services, MDS, RGW, NFS, and ingress.

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
| `spec.externalDetails` | When `storageClusterRef` is external Ceph | — | Operator-supplied external-cluster details; see [External details](#external-details). |

!!! note "Cross-field rules"
    - For a **managed** `storageClusterRef`, `dataFoundation` is required and
      `externalDetails` may be omitted: the consuming add-on then produces the
      external-cluster details itself — its hook runs the exporter on a Ceph
      node of the export's cluster and captures the payload as a hook output.
    - For an **external** `storageClusterRef`, `externalDetails` is required and
      `dataFoundation` must be empty.

A managed export wiring the RBD pool and CephFS filesystem (the consuming
add-on's exporter hook reads these refs):

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
| `externalDetails.fromSecretRef` | Yes within `externalDetails` | — | Secret with the operator-supplied external-cluster-details JSON. |

!!! note "Who produces the details"
    The export itself never gathers credentials. Either the operator supplies
    the external-cluster-details JSON as a secret (`fromSecretRef` — the only
    option for external Ceph, where Bootwright manages no nodes), or the
    consuming [add-on](add-ons.md) ships a hook that fetches Rook's
    external-cluster-details exporter from the installed operator, runs it on a
    node of the managed Ceph cluster, and consumes the captured output. See the
    add-ons page for the hook shapes.

## Where to go next

- [Ceph topologies](../advanced/ceph-topologies.md) — stretch clusters, FIPS,
  the HA dashboard, and importing an existing cluster.
- [The Ceph lab](../getting-started/ceph.md) — a guided first managed-Ceph
  apply.
- [Add-ons](add-ons.md) — how a `StorageExport` feeds a Data Foundation
  external-mode attachment.
