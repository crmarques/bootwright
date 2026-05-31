# Bare-Metal Multi-DC Virtualized ODF Ceph

This reference example shows the full canonical layout with:

- two three-node bare-metal OpenShift parent clusters in separate data centers;
- one three-master plus three-worker KubeVirt-hosted OpenShift child cluster on each parent;
- one managed stretched Ceph cluster spanning two data sites plus a tiebreaker;
- OpenShift Data Foundation bound to all four container clusters for block, file, and object storage;
- OpenShift Virtualization bound only to the two parent clusters;
- child cluster namespace and NAD manifests delivered as a cluster add-on.

Defaulted fields are intentionally present with short comments so authors can see the available surface and omit those values in smaller input sets.
