# Baremetal Redfish ODF External Ceph Example

This example provisions one three-node bare-metal OpenShift cluster with
Redfish virtual media, then applies post-install extensions for Red Hat
OpenShift Data Foundation external mode and OpenShift Virtualization.

The cluster is `demo-ocp`. The post-install extension order is:

1. Install the Red Hat OpenShift Data Foundation operator package
   `odf-operator` from `redhat-operators` in `openshift-storage`.
2. Apply fake external Ceph connection details, then create an external-mode
   `StorageCluster` and `StorageSystem` that connect through those details.
3. Install OpenShift Virtualization and wait for the `HyperConverged` resource.

OpenShift Data Foundation uses the ODF/OCS API surface in OpenShift: the OLM
package is `odf-operator`, external mode is reconciled by `ocs-operator`, and
the storage resources are `StorageCluster` and `StorageSystem`. This example
pins the OpenShift release to 4.21.15 and the Data Foundation subscription to
the Red Hat `stable-4.21` channel.

`ClusterExtension` does not connect to Ceph directly. Bootwright uses the
binding to apply Kubernetes resources to `demo-ocp` after the cluster is
installed. The Data Foundation operator consumes
`rook-ceph-external-cluster-details`, then reconciles
`ocs-external-storagecluster` into Ceph CSI and object storage resources.

This example is complete Bootwright input. For a real environment, update only
the `REPLACE_WITH_*` values in
`extensions/manifests/01-rook-ceph-external-cluster-details.yaml`. Those values
are the external Ceph monitor endpoints, FSID, CSI keys, RBD pool, CephFS
filesystem and pool, RGW endpoint, RGW pool prefix, and RGW keys. The
surrounding Secret, `StorageCluster`, `StorageSystem`, extension binding, and
extension order do not need structural changes.

The Data Foundation exporter can produce the same values. Run it with RBD,
CephFS, and RGW inputs on the external Ceph side, then copy the corresponding
values into the manifest:

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

The ODF external Ceph extension applies these manifests in order:

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
bootwright check syntax -f ./my-odf-baremetal
bootwright context init odf-baremetal -f ./my-odf-baremetal
bootwright secret set openshift-pull-secret --pull-secret ~/openshift-pull-secret.json
bootwright secret generate
bootwright apply cluster --yes
```

Use `ocs-external-storagecluster-ceph-rbd` as the KubeVirt profile
`storageClassRef.name` when adding a virtual OpenShift child cluster on top of
this host cluster; `examples/baremetal-redfish-virtualized-child` shows the
child-cluster shape.

Reference material:

- Red Hat OpenShift Data Foundation external mode: https://docs.redhat.com/en/documentation/red_hat_openshift_data_foundation/4.21/html-single/deploying_openshift_data_foundation_in_external_mode/index
- Red Hat OpenShift Data Foundation architecture: https://docs.redhat.com/en/documentation/red_hat_openshift_data_foundation/4.21/html-single/red_hat_openshift_data_foundation_architecture/index
