# Three-node Ceph NFS export on libvirt and sushy

This example declares three managed RHEL 9.8 guests and an IBM Storage Ceph
cluster with one NFS export: size-2/min-size-2 CephFS pools, an `mds.files`
service of two daemons, a filesystem `files` with the subvolume group `apps`,
a 1 GiB subvolume `docs` (uid and gid 2000, mode 750), an `nfs.shares` service
of one daemon without an ingress, and the CephNFSExport `docs` serving that
subvolume at pseudo path `/docs` over NFSv4.

The export's own access is `NONE`. Its first client entry denies
`198.51.100.13` (`rhel-03`) and its second grants read-write to the rest of
`198.51.100.0/24`, because NFS-Ganesha applies the first entry that matches a
client. Every entry uses `root_squash`, so an allowed client writes as the
subvolume's owner (uid and gid 2000), never root. The
[NFS check](../../../e2e/lab-3-nodes-ceph/runbook.md#nfs-b478) supplies the full
client setup and access checks. On the allowed client, copy a local file into
the mounted export using its owner identity:

```sh
SOURCE_FILE='' # Path to the source file on the allowed client.
NFS_MOUNT=''   # Path where that client mounted the export.
setpriv --reuid=2000 --regid=2000 --clear-groups -- cp -- "${SOURCE_FILE:?Set SOURCE_FILE to the client source file}" "${NFS_MOUNT:?Set NFS_MOUNT to the client export mount}/"
```

## Worth noting

- Topology: `rhel-01` to `rhel-03`, each 4 vCPU, 10 GiB RAM, 60 GiB root and
  one 80 GiB SSD data disk. Total 12 vCPUs, 30 GiB guest RAM; reserve at least
  4 GiB further host memory for the controller.
- The guests add `nfs-utils` so `rhel-02` and `rhel-03` can act as clients.
- The export never adopts or overwrites an export Bootwright did not create.
- Required inputs: RHEL 9.8 boot and DVD ISOs from your Red Hat subscription
  and the IBM entitlement key for Secret `ibm-registry`. The Entitlement
  `license.accept: true` is a placeholder; review IBM's terms before use.
- Release coordinates are the three-node example's authored choices.
- Removal while hosts survive is owned by
  [B491](../../../specs/milestones/m2.md#b491).

Run it with the e2e [`lab-3-nodes-ceph`](../../../e2e/lab-3-nodes-ceph/runbook.md) case.
