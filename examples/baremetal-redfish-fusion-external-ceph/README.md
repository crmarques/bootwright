# Baremetal Redfish Fusion External Ceph Example

This example provisions one three-node bare-metal OpenShift cluster with
Redfish virtual media, then applies post-install extensions for IBM Fusion Data
Foundation external mode and OpenShift Virtualization.

The cluster is `demo-ocp`. The post-install extension order is:

1. Create the IBM Fusion Data Foundation catalog source
   `isf-data-foundation-catalog`.
2. Install the IBM Fusion Data Foundation operator package `odf-operator` from
   that IBM catalog in `openshift-storage`.
3. Apply fake external Ceph connection details, then create an external-mode
   `StorageCluster` and `StorageSystem` that connect through those details.
4. Install OpenShift Virtualization and wait for the `HyperConverged` resource.

IBM Fusion Data Foundation still uses the ODF/OCS API surface in OpenShift:
the OLM package is `odf-operator`, external mode is reconciled by
`ocs-operator`, and the storage resources are `StorageCluster` and
`StorageSystem`. This example pins the OpenShift release to 4.20.10, the Data
Foundation subscription to `stable-4.20`, and the IBM catalog image to
`icr.io/cpopen/isf-data-foundation-catalog:v4.20`.

`ClusterExtension` does not connect to Ceph directly. Bootwright uses the
binding to apply Kubernetes resources to `demo-ocp` after the cluster is
installed. The Data Foundation operator consumes
`rook-ceph-external-cluster-details`, then reconciles
`ocs-external-storagecluster` into Ceph CSI and object storage resources.

This example is complete Bootwright input. For a real environment, update only
the `REPLACE_WITH_*` values in
`extensions/manifests/01-rook-ceph-external-cluster-details.yaml`. Those values
are the external IBM Storage Ceph monitor endpoints, FSID, CSI keys, RBD pool,
CephFS filesystem and pool, RGW endpoint, RGW pool prefix, and RGW keys. The
surrounding Secret, `StorageCluster`, `StorageSystem`, extension binding, and
extension order do not need structural changes.

The `openshift-pull-secret` used for the cluster install must also include the
registry credentials required by your IBM Fusion Data Foundation entitlement.

The Data Foundation exporter can produce the same values. Run it with RBD,
CephFS, and RGW inputs on the external IBM Storage Ceph side, then copy the
corresponding values into the manifest:

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

The Fusion extensions apply these manifests in order:

- `00-isf-data-foundation-catalog.yaml` supplies the IBM Fusion Data Foundation
  catalog source used by the `odf-operator` subscription.
- `01-rook-ceph-external-cluster-details.yaml` supplies the monitor,
  healthchecker, CSI, RBD, CephFS, RGW, and monitoring values.
- `02-ocs-external-storagecluster.yaml` enables Data Foundation external mode.
- `03-ocs-external-storagesystem.yaml` creates the Data Foundation storage
  system wrapper.

The storage extension waits for these classes to exist:

- `ocs-external-storagecluster-ceph-rbd` for block volumes
- `ocs-external-storagecluster-cephfs` for file volumes
- `ocs-external-storagecluster-ceph-rgw` for Ceph RGW S3
- `openshift-storage.noobaa.io` for Multicloud Object Gateway S3

For a first install, copy this example to a working directory, update the
`REPLACE_WITH_*` values, then run the install and extension phases in one flow:

```text
bootwright check syntax -f ./my-fusion-baremetal
bootwright context init fusion-baremetal -f ./my-fusion-baremetal
bootwright secret set openshift-pull-secret --pull-secret ~/openshift-pull-secret.json
bootwright secret generate
bootwright apply cluster --yes
```

Use `ocs-external-storagecluster-ceph-rbd` as the KubeVirt profile
`storageClassRef.name` when adding a virtual OpenShift child cluster on top of
this host cluster; `examples/baremetal-redfish-virtualized-child` shows the
child-cluster shape.

Reference material:

- IBM Fusion Data Foundation 4.20: https://www.ibm.com/docs/en/fusion-software/2.12.x?topic=services-fusion-data-foundation
- IBM Fusion Data Foundation external mode: https://www.ibm.com/docs/en/fusion-software/2.12.x?topic=foundation-deploying-data-in-external-mode
- IBM Fusion Data Foundation catalog source: https://www.ibm.com/docs/en/fusion-hci-systems/2.12.x?topic=dr-installing-multicluster-orchestrator-operator
