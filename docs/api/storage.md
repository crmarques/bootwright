---
title: Storage API
description: StorageCluster, Ceph sub-objects, and StorageExport fields.
---

# Storage

Storage objects model imported Ceph or Bootwright-managed Ceph. Managed storage
is additive-only: `apply` creates and converges declared Ceph objects but does
not prune live pools, filesystems, gateways, services, modules, or config keys
when their declarations are removed.

## StorageCluster

| Field | Required | Description |
| --- | --- | --- |
| `spec.type` | Yes | Currently `ceph`. |
| `spec.management` | No | `managed` or `external`; empty means `managed`. |
| `spec.ceph` | For managed Ceph | Ceph distribution, cephadm, networks, topology, config, monitoring, and services. |

External storage omits `spec.ceph` and is consumed through `StorageExport`.

### Ceph

| Field | Description |
| --- | --- |
| `ceph.distribution` | `oss`, `redhat`, or `ibm`; empty means `oss`. |
| `ceph.release` | Upstream release name or version for `oss`; product stream for `redhat` and `ibm`. |
| `ceph.image` | Exact cephadm daemon image, pinned by version tag or digest. |
| `ceph.community.mirror` | Upstream package mirror for `oss`. |
| `ceph.entitlementRef` | Environment entitlement for `redhat` or `ibm`. |
| `ceph.cephadm.addressRef` | Default address name for cephadm operations. |
| `ceph.cephadm.bootstrap.host` | Topology host where cephadm bootstrap runs. |
| `ceph.cephadm.bootstrap.addressRef` | Bootstrap host address override. |
| `ceph.networks.publicCIDRs[]` | Public network CIDRs. |
| `ceph.networks.clusterCIDRs[]` | Cluster network CIDRs for replication and recovery traffic. |
| `ceph.config` | Ceph config database options by section and key. |
| `ceph.mgrModules[]` | mgr modules to enable. |
| `ceph.monitoring` | cephadm monitoring stack controls. |
| `ceph.services[]` | Raw cephadm service-spec passthrough for unmodeled services. |
| `ceph.topology` | Hosts, roles, OSD devices, sites, and stretch mode. |

Distribution rules:

| Distribution | Requirements |
| --- | --- |
| `oss` | Community package and image sources; `community.mirror` may override `download.ceph.com`. |
| `redhat` | `entitlementRef` must resolve to a Red Hat Ceph entitlement. |
| `ibm` | `entitlementRef` must resolve to an IBM Storage Ceph entitlement with accepted license terms. |

### Monitoring

| Field | Description |
| --- | --- |
| `monitoring.enabled` | Defaults to true when block is present; false skips monitoring stack. |
| `monitoring.prometheus` | Placement, port, retention time, and retention size. |
| `monitoring.grafana` | Placement and port. |
| `monitoring.alertmanager` | Placement and port. |
| `monitoring.nodeExporter` | Explicit placement narrowing; no topology role is required. |

Monitoring service fields are `placement`, `port`, `retentionTime`, and
`retentionSize`. Retention fields apply to Prometheus.

### Passthrough Services

| Field | Description |
| --- | --- |
| `services[].serviceType` | cephadm service type. |
| `services[].serviceID` | Optional service ID. |
| `services[].placement` | Hosts, sites, and count per host. |
| `services[].spec` | Raw service spec map rendered to cephadm. |

Do not declare service types already owned by first-class Bootwright surfaces,
such as monitors, managers, OSDs, MDS, RGW, ingress, or monitoring services.

### Topology

| Field | Description |
| --- | --- |
| `topology.hosts[].machineRef` | `Machine` with `ceph-node` capability. |
| `topology.hosts[].hostname` | cephadm host name; defaults to `machineRef`. |
| `topology.hosts[].site` | Site/failure-domain label used by stretch and site placement. |
| `topology.hosts[].roles[]` | Ceph roles and labels, such as `mon`, `mgr`, `osd`, `mds`, `rgw`, `prometheus`, `grafana`, `alertmanager`. |
| `topology.hosts[].labels[]` | Additional cephadm host labels. |
| `topology.hosts[].devices[]` | Literal OSD device paths. |
| `topology.hosts[].osd` | Drivegroup-shaped OSD device selection. |

OSD hosts must set either `devices[]` or `osd`. There is no implicit
all-devices default.

### OSD Device Selection

| Field | Description |
| --- | --- |
| `osd.dataDevices` | Data device selector. |
| `osd.dbDevices` | DB device selector. |
| `osd.walDevices` | WAL device selector. |
| `osd.encrypted` | Enable encrypted OSDs. |
| `osd.osdsPerDevice` | Number of OSDs per selected device. |
| `osd.crushDeviceClass` | CRUSH device class. |
| `*.paths[]` | Literal device paths. |
| `*.all` | Explicitly select all matching devices. |
| `*.rotational` | Rotational filter. |
| `*.size` | Size filter. |
| `*.limit` | Limit matching devices. |

### Stretch Mode

| Field | Description |
| --- | --- |
| `topology.stretch.failureDomain` | CRUSH failure domain. |
| `topology.stretch.dataSites[]` | Exactly two data sites when authored. |
| `topology.stretch.tiebreaker.site` | Tiebreaker site. |
| `topology.stretch.tiebreaker.host` | Tiebreaker host. |
| `topology.stretch.ruleName` | Stretch CRUSH rule name; defaults to `stretch-rule`. |

Stretch mode is enabled by the presence of the `stretch` block. Policy-less
replicated pools inherit stretch placement and `size: 4`, `minSize: 2`.

## StoragePlacementPolicy

| Field | Required | Description |
| --- | --- | --- |
| `spec.storageClusterRef` | Yes | Managed `StorageCluster`. |
| `spec.ceph.failureDomain` | No | CRUSH failure domain. |
| `spec.ceph.ruleName` | No | CRUSH rule name. |
| `spec.ceph.replicated.size` | No | Replica count. |
| `spec.ceph.replicated.minSize` | No | Minimum replicas. |

Pools using `placementPolicyRef` must not also set `ceph.replicated`.

## StoragePool

| Field | Required | Description |
| --- | --- | --- |
| `spec.storageClusterRef` | Yes | Managed `StorageCluster`. |
| `spec.placementPolicyRef` | No | Placement policy for replicated defaults. |
| `spec.ceph.type` | No | `replicated` or `erasure`; empty means `replicated`. |
| `spec.ceph.role` | No | `rbd`, `cephfs-metadata`, `cephfs-data`, or `rgw`. |
| `spec.ceph.application` | No | Overrides inferred pool application. |
| `spec.ceph.replicated.size` | No | Replica count. |
| `spec.ceph.replicated.minSize` | No | Minimum replicas. |
| `spec.ceph.erasure.dataChunks` | For erasure | Erasure `k`. |
| `spec.ceph.erasure.codingChunks` | For erasure | Erasure `m`. |

Changing pool type or erasure profile is structural and requires break-glass
rebuild behavior. Erasure pools are not allowed on stretch-mode clusters.

## StorageFilesystem

| Field | Required | Description |
| --- | --- | --- |
| `spec.storageClusterRef` | Yes | Managed `StorageCluster`. |
| `spec.cephfs.metadataPoolRef` | Yes | Metadata `StoragePool`. |
| `spec.cephfs.dataPoolRefs[]` | Yes | Data pool names or objects. |
| `spec.cephfs.dataPoolRefs[].name` | Yes in object form | Data pool name. |
| `spec.cephfs.dataPoolRefs[].default` | No | Marks the default data pool. |
| `spec.cephfs.mds.activeCount` | No | Active MDS count. |
| `spec.cephfs.mds.placement` | No | MDS service placement. |

A single data pool defaults automatically. Multiple data pools require exactly
one default.

## Shared Placement

Several storage surfaces use `placement`:

| Field | Description |
| --- | --- |
| `placement.hosts[]` | Explicit topology hostnames. |
| `placement.sites[]` | Topology sites. |
| `placement.countPerHost` | cephadm `count_per_host`. |

## StorageObjectGateway

| Field | Required | Description |
| --- | --- | --- |
| `spec.storageClusterRef` | Yes | Managed `StorageCluster`. |
| `spec.public.dnsName` | Yes | Public S3 endpoint DNS name. |
| `spec.public.scheme` | No | Endpoint scheme. |
| `spec.public.port` | No | Endpoint port. |
| `spec.ceph.serviceID` | Yes | RGW service ID. |
| `spec.ceph.placement` | Yes | RGW placement. |
| `spec.ceph.frontendPort` | No | RGW frontend port. |
| `spec.ceph.ingresses[]` | No | cephadm ingress VIPs. |
| `spec.ceph.ingresses[].name` | Yes | Ingress name. |
| `spec.ceph.ingresses[].address` | Yes | VIP address. |
| `spec.ceph.ingresses[].prefixLength` | Yes | VIP prefix length. |
| `spec.ceph.ingresses[].virtualInterfaceNetworks[]` | No | cephadm virtual interface networks. |
| `spec.ceph.ingresses[].placement` | No | Ingress placement. |

RGW endpoints are owned by the storage gateway, not by `ContainerCluster`.

## StorageExport

`StorageExport` owns storage services prepared for downstream consumers.

| Field | Required | Description |
| --- | --- | --- |
| `spec.type` | Yes | Currently `dataFoundation`. |
| `spec.storageClusterRef` | Yes | Imported or managed `StorageCluster`. |
| `spec.dataFoundation` | Managed storage | References managed storage services to export. |
| `spec.externalDetails` | External storage | Describes how external-cluster details are supplied. |

### Data Foundation

| Field | Description |
| --- | --- |
| `dataFoundation.rbdPoolRef` | RBD pool. |
| `dataFoundation.filesystemRef` | CephFS filesystem. |
| `dataFoundation.objectGatewayRef` | Optional RGW service. |

### External Details

| Field | Description |
| --- | --- |
| `externalDetails.fromSecretRef` | Secret with operator-supplied external-cluster details. |
| `externalDetails.generated` | Generated details for managed storage. |
| `externalDetails.sshExecution.machineRefs[]` | `ceph-admin` machines used to gather external details. |
| `externalDetails.sshExecution.timeout` | SSH execution timeout. |
| `externalDetails.sshExecution.exporter.source` | Exporter source. |
| `externalDetails.sshExecution.config.format` | Currently `json` when set. |
| `externalDetails.sshExecution.config.rbdDataPoolName` | RBD data pool name. |
| `externalDetails.sshExecution.config.radosNamespace` | RADOS namespace. |
| `externalDetails.sshExecution.config.rbdMetadataECPoolName` | RBD metadata EC pool name. |
| `externalDetails.sshExecution.config.cephfsFilesystemName` | CephFS filesystem name. |
| `externalDetails.sshExecution.config.cephfsDataPoolName` | CephFS data pool name. |
| `externalDetails.sshExecution.config.cephfsMetadataPoolName` | CephFS metadata pool name. |
| `externalDetails.sshExecution.config.rgwEndpoint` | RGW endpoint. |
| `externalDetails.sshExecution.config.rgwPoolPrefix` | RGW pool prefix. |
| `externalDetails.sshExecution.config.monitoringEndpoint[]` | Monitoring endpoints. |
| `externalDetails.sshExecution.config.monitoringEndpointPort` | Monitoring port. |
| `externalDetails.sshExecution.config.clusterName` | Storage cluster name for exported details. |
| `externalDetails.sshExecution.config.k8sClusterName` | Kubernetes cluster name for exported details. |
| `externalDetails.sshExecution.config.restrictedAuthPermission` | Restricted auth permission flag. |

For external clusters, set exactly one of `fromSecretRef` or `sshExecution`.
For managed clusters, generated details are produced during storage apply.
