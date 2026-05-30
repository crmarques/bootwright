# Bare-Metal Fleet With Stretched Ceph Data Foundation

This example declares two compact bare-metal OpenShift clusters and one
external Ceph storage cluster. The Ceph topology is a production-oriented
stretch layout with three storage nodes in `dc1`, three storage nodes in `dc2`,
and a monitor-only tiebreaker in `dc3`.

The Ceph machines are preinstalled RHEL nodes. Bootwright reaches them from the
bastion with `StorageCluster.spec.ceph.cephadm.nodeSSH`, bootstraps Ceph with
cephadm on `ceph-dc1-0`, applies monitor, manager, OSD, MDS, RGW, and ingress
service specs, then renders Data Foundation external-mode manifests for both
`dc1-ocp` and `dc2-ocp`.

The IBM Fusion Data Foundation operator extension advertises
`provides: data-foundation`; the `StorageClusterBinding` waits for that
operator readiness before applying generated external Ceph connection
manifests to each cluster.

The rendered Ceph operations create per-cluster Data Foundation client users.
The example does not version any generated external-cluster secret bytes.
