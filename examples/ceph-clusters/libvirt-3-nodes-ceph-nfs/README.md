# Three-node Ceph NFS export on libvirt and sushy

This example declares three managed RHEL guests and an IBM Storage Ceph cluster with one NFS export: size-2/min-size-2 CephFS metadata and data pools, a native `mds` service `mds.files` of two daemons, a CephFS filesystem `files` with the subvolume group `apps`, a 1 GiB subvolume `docs` in that group, a native `nfs` service `nfs.shares` of one daemon without an ingress, and the CephNFSExport `docs` that serves that subvolume at the pseudo path `/docs` over NFSv4. The export's own access is `NONE`; its first client entry denies `198.51.100.13` (`rhel-03`) and its second grants read-write to the rest of `198.51.100.0/24`, because NFS-Ganesha applies the first entry that matches a client. Addresses and names are synthetic. Copy the input to an operator-owned directory before adjusting it; keep real inputs and credentials outside tracked files.

The cluster's settings block enables the `nfs` manager module, and the service-unit block `ceph-service-ceph-lab-nfs-shares` applies `nfs.shares`. After `ceph-subvolume-docs` has created the subvolume, the export block `ceph-nfs-export-docs` applies one export with `ceph nfs export apply shares -i -`. That export serves the path the subvolume's creation proof reports. The block records the export ID and the CephFS principal that the NFS manager generates, and it never adopts or overwrites an export that it did not create.

## Budget and setup

The budget, host prerequisites, `setup`, `preflight controller`, media imports and entitlement are the [three-node example's](../libvirt-3-nodes-ceph/README.md#initial-setup); this example authors the same release coordinates, guests and disks, and adds `nfs-utils` to the guests' packages so that `rhel-02` and `rhel-03` can act as NFS clients. Review [cluster.yaml](storage/cluster.yaml), which owns the native MON, MGR, OSD, `mds.files` and `nfs.shares` services, [filesystem.yaml](storage/filesystem.yaml) and [subvolume.yaml](storage/subvolume.yaml), which own the filesystem and the subvolume, and [nfs-export.yaml](storage/nfs-export.yaml), which owns the export.

The tracked `storage/entitlement.yaml` sets `license.accept: true` as a placeholder. Accepting the IBM license is your explicit act: review its terms before you use your copy.

## Create the context and apply

Replace `/path/to/adjusted/libvirt-3-nodes-ceph-nfs` with your completed copy.

```sh
./bin/bootwright validate -f /path/to/adjusted/libvirt-3-nodes-ceph-nfs
./bin/bootwright context init --name ceph-nfs --input-dir /path/to/adjusted/libvirt-3-nodes-ceph-nfs
./bin/bootwright context use --name ceph-nfs
./bin/bootwright secret generate
./bin/bootwright secret set --name ibm-registry --username cp --password-stdin --yes
./bin/bootwright secret check
./bin/bootwright render effective
./bin/bootwright plan --stage controller
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context ceph-nfs
./bin/bootwright apply --stage infrastructure-services,substrates
./bin/bootwright apply --stage machines
./bin/bootwright plan --stage clusters
./bin/bootwright apply --stage clusters
./bin/bootwright status
```

The clusters stage plans the image, host, cluster and settings blocks of `ceph-lab`, the pool, filesystem and subvolume blocks, the service-unit block `ceph-service-ceph-lab-nfs-shares` and the export block `ceph-nfs-export-docs`.

## Operator acceptance

Print the admin node and its Ceph command as the three-node example's [validation](../libvirt-3-nodes-ceph/README.md#validate-the-cluster) does, then run each check as that printed command followed by the arguments shown, written here as `<cephCommand>`:

```text
<cephCommand> -s
<cephCommand> orch ps --daemon_type nfs
<cephCommand> nfs export info shares /docs
```

Require `HEALTH_OK`, or only the tolerated `CHA_ERROR_SENDING_REPORT` warning, one running `nfs.shares` daemon, and an export at `/docs` whose `path` lies under `/volumes/apps/docs/`, with `access_type` `NONE`, `protocols` `[4]`, `transports` `["TCP"]` and the two client entries in their declared order. Bootwright's readiness proves the same stored export; client access is the operator's acceptance, not Bootwright's.

Write the NFS daemon's host from the `orch ps` output as `<nfsHost>`. From the allowed client `rhel-02`, mount the export with NFSv4, write 1 MiB, read it back and unmount. Every client entry squashes root, so the client's root reaches the export as the anonymous user, which the subvolume's mode `750` denies; the check therefore writes, reads and removes as the subvolume's owner, uid and gid `2000` from `storage/subvolume.yaml`:

```sh
./bin/bootwright cluster exec --name ceph-lab --node rhel-02 -- sudo -n bash -s -- '<nfsHost>' <<'CHECK'
set -euo pipefail
check_dir=$(mktemp -d)
trap 'umount "$check_dir/mnt" 2>/dev/null || true; rm -f -- "$check_dir/input"; rmdir -- "$check_dir/mnt" "$check_dir"' EXIT
chmod 0711 "$check_dir"
mkdir "$check_dir/mnt"
mount -t nfs4 -o proto=tcp "$1:/docs" "$check_dir/mnt"
head -c 1048576 /dev/urandom > "$check_dir/input"
chmod 0644 "$check_dir/input"
owner() { setpriv --reuid=2000 --regid=2000 --clear-groups -- "$@"; }
owner cp "$check_dir/input" "$check_dir/mnt/nfs-check"
owner cmp "$check_dir/input" "$check_dir/mnt/nfs-check"
owner rm -- "$check_dir/mnt/nfs-check"
CHECK
```

From the denied client `rhel-03`, require the mount to fail with `access denied by server`:

```sh
./bin/bootwright cluster exec --name ceph-lab --node rhel-03 -- sudo -n bash -s -- '<nfsHost>' <<'CHECK'
check_dir=$(mktemp -d)
trap 'umount "$check_dir" 2>/dev/null || true; rmdir -- "$check_dir"' EXIT
if mount -t nfs4 -o proto=tcp "$1:/docs" "$check_dir"; then echo 'the denied client mounted /docs'; exit 1; fi
CHECK
```

## Cleanup

This profile retires Ceph and its export together with the three owned virtual hosts; removal while hosts survive belongs to [B491](../../../specs/milestones/m2.md#b491). While the hosts run, destroy refuses with `lifecycle.live`:

```sh
./bin/bootwright plan
./bin/bootwright machine stop --name rhel-01,rhel-02,rhel-03
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
./bin/bootwright cluster info --name ceph-lab
virsh --connect qemu:///system list --all
./bin/bootwright context delete --name ceph-nfs --purge
```

After an interrupted destroy, read status and repeat destroy with the same authorization.
