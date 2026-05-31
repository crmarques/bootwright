# Baremetal Redfish Fusion External Ceph Example

This example provisions one three-node bare-metal OpenShift cluster with
Redfish virtual media, then applies post-install extensions for IBM Fusion Data
Foundation external mode and OpenShift Virtualization.

The cluster is `demo-ocp`. The post-install extension order is:

1. Create the IBM Fusion Data Foundation catalog source
   `isf-data-foundation-catalog`.
2. Install the IBM Fusion Data Foundation operator package `odf-operator` from
   that IBM catalog in `openshift-storage`.
3. Install OpenShift Virtualization and wait for the `HyperConverged` resource.

IBM Fusion Data Foundation still uses the ODF/OCS API surface in OpenShift:
the OLM package is `odf-operator`, external mode is reconciled by
`ocs-operator`, and the storage resources are `StorageCluster` and
`StorageSystem`. This example pins the OpenShift release to 4.20.10, the Data
Foundation subscription to `stable-4.20`, and the IBM catalog image to
`icr.io/cpopen/isf-data-foundation-catalog:v4.20`.

`StorageCluster/shared-ceph` is declared with `management: external`.
Bootwright does not run `cephadm`, create pools, or create Ceph users for this
example. `StorageExport/shared-ceph-data-foundation` reads the raw external
details JSON from `Environment.spec.secrets.shared-ceph-external-details`, and
`StorageClusterBinding/shared-ceph-data-foundation` renders and applies the
Data Foundation `rook-ceph-external-cluster-details`, `StorageCluster`, and
`StorageSystem` manifests to `demo-ocp` after the cluster and Data Foundation
operator are ready.

The `openshift-pull-secret` used for the cluster install must also include the
registry credentials required by your IBM Fusion Data Foundation entitlement.

The Data Foundation exporter can produce the same values. Run it with RBD,
CephFS, and RGW inputs on the external IBM Storage Ceph side, then store the
resulting raw JSON array in the active Bootwright context:

```text
python3 ceph-external-cluster-details-exporter.py \
  --rbd-data-pool-name <rbd-block-pool> \
  --cephfs-filesystem-name <cephfs-name> \
  --cephfs-data-pool-name <cephfs-data-pool> \
  --cephfs-metadata-pool-name <cephfs-metadata-pool> \
  --rgw-endpoint <rgw-host-or-ip>:<rgw-port> \
  --rgw-pool-prefix <rgw-pool-prefix> \
  --run-as-user <client-name>
```

The generated JSON includes the external IBM Storage Ceph monitor endpoints,
FSID, CSI keys, RBD pool, CephFS filesystem and pool, RGW endpoint, RGW pool
prefix, and RGW keys. Keep that file outside versioned content and load it
with `bootwright secret set shared-ceph-external-details --raw-file <external-details.json>`.

The Fusion extension set still applies `00-isf-data-foundation-catalog.yaml`
before the operator subscription so the `odf-operator` package resolves from
the IBM Fusion Data Foundation catalog.

For a first install, copy this example to a working directory, prepare
`external-details.json`, then run the install and extension phases in one flow:

```text
bootwright check syntax -f ./my-fusion-baremetal
bootwright context init fusion-baremetal -f ./my-fusion-baremetal
bootwright secret set openshift-pull-secret --pull-secret ~/openshift-pull-secret.json
bootwright secret set shared-ceph-external-details --raw-file ./external-details.json
bootwright secret generate
bootwright apply all --yes
```

Use `ocs-external-storagecluster-ceph-rbd` as the KubeVirt profile
`storageClassRef.name` when adding a virtual OpenShift child cluster on top of
this host cluster; `examples/baremetal-redfish-virtualized-child` shows the
child-cluster shape.

Reference material:

- IBM Fusion Data Foundation 4.20: https://www.ibm.com/docs/en/fusion-software/2.12.x?topic=services-fusion-data-foundation
- IBM Fusion Data Foundation external mode: https://www.ibm.com/docs/en/fusion-software/2.12.x?topic=foundation-deploying-data-in-external-mode
- IBM Fusion Data Foundation catalog source: https://www.ibm.com/docs/en/fusion-hci-systems/2.12.x?topic=dr-installing-multicluster-orchestrator-operator
