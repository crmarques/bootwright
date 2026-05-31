# Baremetal Redfish ODF External Ceph Example

This example provisions one three-node bare-metal OpenShift cluster with
Redfish virtual media, then applies post-install extensions for Red Hat
OpenShift Data Foundation external mode and OpenShift Virtualization.

The cluster is `demo-ocp`. The post-install extension order is:

1. Install the Red Hat OpenShift Data Foundation operator package
   `odf-operator` from `redhat-operators` in `openshift-storage`.
2. Install OpenShift Virtualization and wait for the `HyperConverged` resource.

OpenShift Data Foundation uses the ODF/OCS API surface in OpenShift: the OLM
package is `odf-operator`, external mode is reconciled by `ocs-operator`, and
the storage resources are `StorageCluster` and `StorageSystem`. This example
pins the OpenShift release to 4.21.15 and the Data Foundation subscription to
the Red Hat `stable-4.21` channel.

`StorageCluster/shared-ceph` is declared with `management: external`.
Bootwright does not run `cephadm`, create pools, or create Ceph users for this
example. `StorageExport/shared-ceph-data-foundation` reads the raw external
details JSON from `Environment.spec.secrets.shared-ceph-external-details`, and
`StorageClusterBinding/shared-ceph-data-foundation` renders and applies the
Data Foundation `rook-ceph-external-cluster-details`, `StorageCluster`, and
`StorageSystem` manifests to `demo-ocp` after the cluster and Data Foundation
operator are ready.

The Data Foundation exporter can produce the same values. Run it with RBD,
CephFS, and RGW inputs on the external Ceph side, then store the resulting raw
JSON array in the active Bootwright context:

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

The generated JSON includes the external Ceph monitor endpoints, FSID, CSI
keys, RBD pool, CephFS filesystem and pool, RGW endpoint, RGW pool prefix, and
RGW keys. Keep that file outside versioned content and load it with
`bootwright secret set shared-ceph-external-details --raw-file <external-details.json>`.

For a first install, copy this example to a working directory, prepare
`external-details.json`, then run the install and extension phases in one flow:

```text
bootwright check syntax -f ./my-odf-baremetal
bootwright context init odf-baremetal -f ./my-odf-baremetal
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

- Red Hat OpenShift Data Foundation external mode: https://docs.redhat.com/en/documentation/red_hat_openshift_data_foundation/4.21/html-single/deploying_openshift_data_foundation_in_external_mode/index
- Red Hat OpenShift Data Foundation architecture: https://docs.redhat.com/en/documentation/red_hat_openshift_data_foundation/4.21/html-single/red_hat_openshift_data_foundation_architecture/index
