# Data Foundation over managed Ceph and compact OpenShift

[lab-compact](../../openshift-clusters/lab-compact/) plus a three-node managed
IBM Storage Ceph cluster and a `ClusterAddonBinding` for the `fdf` package,
which consumes the Ceph cluster through a `CephConsumer`. The compact
installation is the [B66 rehearsal](../../../specs/milestones/m3.md#b66); the
Data Foundation gate is [B761](../../../specs/milestones/m4.md#b761), which
requires an accepted compact cluster and qualified Ceph baseline. Operator
acceptance remains open.

## Worth noting

- Three compact masters (6 vCPU, 16 GiB, 120 GiB root each, `.21` to `.23`)
  and three Ceph guests `rhel-01` to `rhel-03` (4 vCPU, 13 GiB, 60 GiB root and
  an 80 GiB data disk each, `.31` to `.33`). Host capacity: at least 34 logical
  CPUs, 95 GiB available memory and 820 GiB free pool disk, plus about 7 GiB
  for five image copies, including the provider's 4 CPU, 8 GiB, 40 GiB reserve.
  Reserve six emulated BMC ports from 8000.
- The consumer uses an RBD pool, CephFS filesystem and an object gateway. One
  RGW daemon is placed on `rhel-01` with no failover, and the managed DNS
  service publishes its address under `s3.lab.example.test`. The gateway
  certificate must cover that name and its issuer must match `rgw-issuer`,
  which the cluster selects under `additionalTrustBundlePolicy: Always`.
- Accept the IBM license yourself: `storage/entitlement.yaml` sets
  `license.accept: true` only as a placeholder. Include the IBM entitlement
  credentials in the OpenShift pull secret before initializing the context.
  Bind `openshift-pull-secret`, `ibm-registry`, `rgw-issuer` and
  `rgw-objects-tls` yourself.
- Replace the synthetic Prometheus image digest and verify the IBM Ceph
  coordinates (release 9.9.2.0, image tag). Import RHEL 9.8 boot and DVD
  media as `local-media:rhel-9.8-x86_64-boot.iso` and `-dvd.iso` before
  applying Machines; see the
  [three-node Ceph example](../../ceph-clusters/libvirt-3-nodes-ceph/).
- Placeholder catalog, channel and CSV in [add-ons/binding.yaml](add-ons/binding.yaml)
  (`stable-4.20`, `odf-operator.v4.20.0`, an owned catalog image digest): the
  operator chooses the catalog tag or digest, the channel and the CSV from a
  vendor-supported combination. On a cluster serving that catalog, read the
  selected channel's `currentCSV` and confirm the catalog source from:

  `oc get packagemanifest odf-operator -n openshift-marketplace -o yaml`
