# Three-node Ceph block and file on libvirt and sushy

This example declares three managed RHEL guests and an IBM Storage Ceph cluster with the block and file resources that fit three OSD hosts: an SSD placement policy (owned CRUSH rule `ssd-hosts`), an RBD pool `rbd-data` placed by that policy, a 1 GiB RBD image `rbd-disk` with an explicit feature set, size-2/min-size-2 CephFS metadata and data pools, a native `mds` service `mds.files` of two daemons, a CephFS filesystem `files` with one active rank, one standby and a subvolume group `apps`, a 1 GiB subvolume `docs` in that group and the pool client `rbd-canary`. Addresses and names are synthetic. Copy the input to an operator-owned directory before adjusting it; keep real inputs and credentials outside tracked files.

It is a functional lab example outside the [profile catalog](../../specs/ceph-coverage.md#profile-catalog) and claims no [B484](../../specs/milestones/m2.md#b484) row. Its runbook follows the catalog's [runbook sections](../../specs/ceph-coverage.md#profile-runbooks) so that its evidence uses the [Ceph matrix row](../../docs/acceptance.md#ceph-matrix-row) fields. Erasure-coded pools need more OSD-bearing hosts than this lab has, under the [pool protection rules](../../specs/ceph-resources.md#cephpool); they belong to the larger catalog profiles.

## Budget and prerequisites

The declared budget is three guests, 12 vCPUs, 30 GiB guest RAM (10 GiB per guest), three 60 GiB root disks and three 80 GiB SSD data disks (180+240 GiB). Reserve at least four further GiB of available host memory for the controller and account for full sparse disk growth. Each guest has 2 GiB more than the [three-node example](libvirt-3-nodes-ceph.md) because two of them also run an MDS daemon. These sizes stay below [IBM's minimum hardware figures](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=recommendations-minimum-hardware-considerations), as that example's do: OSD autotuning is disabled, the OSD memory target is 2 GiB and the guests share one hypervisor's failure domain. The measured budget below is the evidence of what the run used.

External dependencies: the IBM entitlement key and the RHEL 9.8 boot and DVD ISOs from the operator's Red Hat subscription.

## Initial setup

Follow the three-node example's [initial setup](libvirt-3-nodes-ceph.md#initial-setup): the host prerequisites, `setup`, `preflight controller` and both `media add` imports are the same. Review these inputs together in your copy:

- [cluster.yaml](../../examples/ceph-clusters/libvirt-3-nodes-ceph-block-file/storage/cluster.yaml) owns the native MON, MGR, OSD and MDS services; `mds.files` places two daemons (`count: 2`) on hosts labelled `ceph-core`.
- [placement-policy.yaml](../../examples/ceph-clusters/libvirt-3-nodes-ceph-block-file/storage/placement-policy.yaml) owns rule `ssd-hosts` (host failure domain, class `ssd`, 2/2); [pool.yaml](../../examples/ceph-clusters/libvirt-3-nodes-ceph-block-file/storage/pool.yaml) places `rbd-data` through it and authors no replica values of its own.
- [rbd-image.yaml](../../examples/ceph-clusters/libvirt-3-nodes-ceph-block-file/storage/rbd-image.yaml) declares `rbd-disk` in `rbd-data`; [client.yaml](../../examples/ceph-clusters/libvirt-3-nodes-ceph-block-file/storage/client.yaml) binds `rbd-canary` to that pool.
- [filesystem.yaml](../../examples/ceph-clusters/libvirt-3-nodes-ceph-block-file/storage/filesystem.yaml) binds `files` to `mds.files` over [cephfs-metadata-pool.yaml](../../examples/ceph-clusters/libvirt-3-nodes-ceph-block-file/storage/cephfs-metadata-pool.yaml) and [cephfs-data-pool.yaml](../../examples/ceph-clusters/libvirt-3-nodes-ceph-block-file/storage/cephfs-data-pool.yaml), with `mds.activeCount: 1`, `mds.standbyCountWanted: 1` and the group `apps`; [subvolume.yaml](../../examples/ceph-clusters/libvirt-3-nodes-ceph-block-file/storage/subvolume.yaml) places `docs` in that group.

For the standby-replay variant, set `mds.standbyReplay: true` in `filesystem.yaml` of a separate copy and run this whole runbook again in a new context. Two daemons still cover one active rank and its standby-replay follower.

## IBM entitlement

Follow the three-node example's [IBM entitlement](libvirt-3-nodes-ceph.md#ibm-entitlement) and [other releases](libvirt-3-nodes-ceph.md#other-ibm-storage-ceph-releases) sections. This example authors the same release coordinates and entitlement, and the same rules apply when you select another release in your copy.

The tracked `storage/entitlement.yaml` sets `license.accept: true` as a placeholder. Accepting the IBM license is your explicit act: review its terms before you use your copy.

## Create the context

Replace `/path/to/adjusted/libvirt-3-nodes-ceph-block-file` with your completed copy. Record the files-seen and objects-decoded counts that `validate` prints.

```sh
./bin/bootwright validate -f /path/to/adjusted/libvirt-3-nodes-ceph-block-file
./bin/bootwright context init --name ceph-block-file --input-dir /path/to/adjusted/libvirt-3-nodes-ceph-block-file
./bin/bootwright context use --name ceph-block-file
./bin/bootwright secret generate
./bin/bootwright secret set --name ibm-registry --username cp --password-stdin --yes
./bin/bootwright secret check
./bin/bootwright render effective
./bin/bootwright plan --stage controller
```

After a later edit of your copy, `context update --name ceph-block-file --input-dir /path/to/adjusted/libvirt-3-nodes-ceph-block-file --yes` imports it; the first staged apply freezes the whole plan.

## Apply by stage

Run the stages as the three-node example's [apply by stage](libvirt-3-nodes-ceph.md#apply-by-stage) does, with the same per-guest disk and name-resolution checks before the clusters stage:

```sh
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context ceph-block-file
./bin/bootwright apply --stage infrastructure-services,substrates
./bin/bootwright apply --stage machines
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright plan --stage clusters
./bin/bootwright apply --stage clusters
./bin/bootwright status
```

Start the memory sampler of the measured budget below in a second terminal just before `apply --stage clusters`. The clusters stage plans the image, host, cluster, policy, pool, RBD image, filesystem, subvolume and client blocks of `ceph-lab`; the [interruption check](#interruption-and-continuation) below interrupts it once.

## Validate the cluster

Print the admin node and its Ceph command, then run each check below as that printed command followed by the arguments shown, written here as `<cephCommand>`. Replace its leading `bootwright` with `./bin/bootwright` when running from this checkout. Only read the files under `/var/lib/bootwright/ceph/<fsid>/`.

```sh
./bin/bootwright cluster info --name ceph-lab
./bin/bootwright diff --observe
```

```text
<cephCommand> -s
<cephCommand> health detail
<cephCommand> orch ls --refresh
<cephCommand> orch ps --refresh
<cephCommand> osd tree
<cephCommand> osd pool ls detail
<cephCommand> osd crush rule dump ssd-hosts
<cephCommand> osd pool get rbd-data all
<cephCommand> osd pool application get rbd-data
<cephCommand> fs get files
<cephCommand> fs status files
<cephCommand> fs subvolumegroup info files apps
<cephCommand> fs subvolume info files docs --group_name apps
<cephCommand> fs subvolume getpath files docs --group_name apps
```

Read the image with `rbd` on the admin node, with `<conf>` and `<keyring>` the administrator paths of the printed command:

```text
./bin/bootwright cluster exec --name ceph-lab --node rhel-01 -- sudo -n rbd --conf <conf> --keyring <keyring> info rbd-data/rbd-disk
```

| Component | Required result |
| --- | --- |
| Health and daemons | `HEALTH_OK`, or only the tolerated `CHA_ERROR_SENDING_REPORT` warning; three MONs in quorum, one active and one standby MGR, three OSDs up and in, two `mds.files` daemons running. |
| Placement policy | Rule `ssd-hosts` takes from root `default` with class `ssd` and chooses leaves of type `host`. |
| RBD pool | `rbd-data` is replicated with `size=2`, `min_size=2`, `crush_rule=ssd-hosts`, `pg_autoscale_mode=on` and the `rbd` application. |
| RBD image | `rbd-disk` is 1 GiB, format 2, with exactly the features `layering`, `exclusive-lock`, `object-map`, `fast-diff` and `deep-flatten`. |
| CephFS pools | `cephfs-metadata` and `cephfs-data` are replicated 2/2 with the `cephfs` application. |
| Filesystem | `fs get files` shows `max_mds 1`, `standby_count_wanted 1`, `cephfs-metadata` as the metadata pool and `cephfs-data` as its data pool; `fs status files` shows rank 0 `active` and one standby (a `standby-replay` follower in the variant). |
| Group and subvolume | Group `apps` has mode `750`; subvolume `docs` has a 1073741824-byte quota, mode `750` and a path under `/volumes/apps/docs/`. |

## Client access

Both checks use the administrator keyring through `cluster exec`; they do not test a scoped CephFS client, whose capabilities [B489](../../specs/milestones/m2.md#b489) owns. Replace `rhel-01` if the printed admin node differs, and `<conf>` and `<keyring>` with the administrator paths of the printed command. Each script keeps its key material in a root-private scratch directory that it removes on exit; after a killed session, remove only the exact printed scratch directory once the session has ended.

The RBD check maps the declared image with the kernel client, writes 1 MiB at its start, reads it back and unmaps it. The write replaces that region of `rbd-disk`; destroy removes the image with the owned disks.

```text
./bin/bootwright cluster exec --name ceph-lab --node rhel-01 -- sudo -n bash -s -- '<conf>' '<keyring>' <<'CHECK'
set -euo pipefail
umask 077
conf=$1
keyring=$2
device=
check_dir=$(mktemp -d /var/tmp/rbd-check.XXXXXXXX)
printf 'Scratch directory on admin guest: %s\n' "$check_dir"
trap '[ -z "$device" ] || rbd --conf "$conf" --keyring "$keyring" unmap "$device" || true; rm -f -- "$check_dir/input"; rmdir -- "$check_dir"' EXIT
device=$(rbd --conf "$conf" --keyring "$keyring" map rbd-data/rbd-disk)
head -c 1048576 /dev/urandom > "$check_dir/input"
dd if="$check_dir/input" of="$device" bs=1M count=1 oflag=direct status=none
cmp -n 1048576 "$device" "$check_dir/input"
CHECK
```

The CephFS check mounts subvolume `docs` with the kernel client, writes 1 MiB, reads it back and unmounts:

```text
./bin/bootwright cluster exec --name ceph-lab --node rhel-01 -- sudo -n bash -s -- '<conf>' '<keyring>' <<'CHECK'
set -euo pipefail
umask 077
conf=$1
keyring=$2
check_dir=$(mktemp -d /var/tmp/cephfs-check.XXXXXXXX)
printf 'Scratch directory on admin guest: %s\n' "$check_dir"
mkdir "$check_dir/mnt"
trap 'umount "$check_dir/mnt" 2>/dev/null || true; rm -f -- "$check_dir/secret" "$check_dir/input"; rmdir -- "$check_dir/mnt" "$check_dir"' EXIT
ceph --conf "$conf" --keyring "$keyring" auth get-key client.admin -o "$check_dir/secret" >/dev/null
path=$(ceph --conf "$conf" --keyring "$keyring" fs subvolume getpath files docs --group_name apps)
mount -t ceph ":$path" "$check_dir/mnt" -o "name=admin,secretfile=$check_dir/secret,fs=files,conf=$conf"
head -c 1048576 /dev/urandom > "$check_dir/input"
cp "$check_dir/input" "$check_dir/mnt/canary"
sync
cmp "$check_dir/mnt/canary" "$check_dir/input"
rm -f -- "$check_dir/mnt/canary"
CHECK
```

Require exit 0 from both. A lost response is an unknown outcome: inspect the image or subvolume instead of repeating the script blindly.

## Failover

MDS failover fails the active daemon of rank 0 while a client holds the mount, and records how long the standby takes to become active. Stopping a whole guest is not this test: with 2/2 pools a stopped guest leaves every placement group that has a replica there below `min_size`, so client I/O pauses on those groups until the guest returns, whichever MDS is active.

```text
./bin/bootwright cluster exec --name ceph-lab --node rhel-01 -- sudo -n bash -s -- '<conf>' '<keyring>' <<'CHECK'
set -euo pipefail
umask 077
conf=$1
keyring=$2
ceph_admin=(ceph --conf "$conf" --keyring "$keyring")
check_dir=$(mktemp -d /var/tmp/mds-failover.XXXXXXXX)
printf 'Scratch directory on admin guest: %s\n' "$check_dir"
mkdir "$check_dir/mnt"
trap 'umount "$check_dir/mnt" 2>/dev/null || true; rm -f -- "$check_dir/secret" "$check_dir/input"; rmdir -- "$check_dir/mnt" "$check_dir"' EXIT
rank0() { "${ceph_admin[@]}" fs get files --format json | python3 -c 'import json, sys; info = json.load(sys.stdin)["mdsmap"]["info"].values(); print(" ".join(str(d["gid"]) for d in info if d["rank"] == 0 and d["state"] == "up:active"))'; }
"${ceph_admin[@]}" auth get-key client.admin -o "$check_dir/secret" >/dev/null
path=$("${ceph_admin[@]}" fs subvolume getpath files docs --group_name apps)
mount -t ceph ":$path" "$check_dir/mnt" -o "name=admin,secretfile=$check_dir/secret,fs=files,conf=$conf"
head -c 1048576 /dev/urandom > "$check_dir/input"
cp "$check_dir/input" "$check_dir/mnt/before"
sync
before=$(rank0)
printf 'Active rank 0 before: %s\n' "$before"
start=$(date +%s)
"${ceph_admin[@]}" mds fail files:0
now=
for _ in $(seq 1 300); do
  now=$(rank0)
  if [ -n "$now" ] && [ "$now" != "$before" ]; then break; fi
  sleep 1
done
printf 'Active rank 0 after: %s in %s s\n' "$now" "$(( $(date +%s) - start ))"
[ -n "$now" ] && [ "$now" != "$before" ]
cmp "$check_dir/mnt/before" "$check_dir/input"
cp "$check_dir/input" "$check_dir/mnt/after"
sync
cmp "$check_dir/mnt/after" "$check_dir/input"
rm -f -- "$check_dir/mnt/before" "$check_dir/mnt/after"
CHECK
```

Require exit 0 and record the printed seconds as the MDS failover time. Then repeat `<cephCommand> fs status files` until it shows rank 0 `active` and one standby again; cephadm restarts the failed daemon as the new standby, and no apply is needed. In the standby-replay variant the follower takes the rank.

For the one-node outage, follow the three-node example's [outage and recovery](libvirt-3-nodes-ceph.md#one-node-outage-and-recovery) with a node other than the admin node, and record that health returns to `HEALTH_OK` with both `mds.files` daemons running after the node restarts.

## Interruption and continuation

Interrupt the clusters stage once while it creates the pools or the filesystem: press Ctrl-C once after `apply --stage clusters` reports the first pool or filesystem block running. Then read the state and run the same command again:

```sh
./bin/bootwright status
./bin/bootwright apply --stage clusters
./bin/bootwright status
```

Record the operation ID before and after: the second command continues the same operation, observes each block whose outcome was unknown before it retries, and completes without a second create of any pool, image, filesystem, group or subvolume.

## Replay

Run the clusters stage again on the completed context, then the whole apply:

```sh
./bin/bootwright apply --stage clusters
./bin/bootwright apply
./bin/bootwright diff
```

Both report the completed operation and `done` without starting work, and `diff` reports no difference.

## Check that changed intent is refused

In your copy, change `sizeBytes` of [subvolume.yaml](../../examples/ceph-clusters/libvirt-3-nodes-ceph-block-file/storage/subvolume.yaml) to `2147483648`, import it and apply:

```sh
./bin/bootwright validate -f /path/to/adjusted/libvirt-3-nodes-ceph-block-file
./bin/bootwright context update --name ceph-block-file --input-dir /path/to/adjusted/libvirt-3-nodes-ceph-block-file --yes
./bin/bootwright diff
./bin/bootwright apply --stage clusters
```

`diff` names `CephSubvolume/docs`; the apply refuses before any effect, names the changed object and field, and says that the completed apply must be destroyed before changed desired state applies. Restore `sizeBytes: 1073741824`, import the copy again with the same `context update` command, and confirm that `./bin/bootwright apply --stage clusters` settles without starting work.

## Measured budget

Run the read-only budget commands of the [profile runbooks](../../specs/ceph-coverage.md#profile-runbooks) before the first apply (a), after `apply --stage clusters` (b), and after the client and failover checks (c), with `<context>` set to `ceph-block-file`, and run the memory sampler during `apply --stage clusters`:

```text
nproc
grep MemAvailable /proc/meminfo
sudo du -sb --apparent-size /var/lib/libvirt/images/bootwright/<context>
sudo du -sb /var/lib/libvirt/images/bootwright/<context>
virsh --connect qemu:///system domstats --balloon --vcpu
<cephCommand> orch ps --format json-pretty
<cephCommand> df
```

Record these fields in the locally retained evidence: Guests, Declared vCPU, Declared RAM GiB, Declared disk GiB, MemAvailable at (a)/(b)/(c), Minimum MemAvailable during clusters stage, Allocated vs apparent disk bytes, Per-guest balloon RSS, Daemons by type, Clusters-stage wall time and OOM kills. Count OOM kills as the `Out of memory` lines in each guest's journal over the run.

## Cleanup

This profile retires Ceph, with every pool, image, filesystem, group and subvolume, together with its three owned virtual hosts; removal while hosts survive belongs to [B491](../../specs/milestones/m2.md#b491). While the hosts run, destroy refuses with `lifecycle.live`:

```sh
./bin/bootwright plan
./bin/bootwright destroy --authorize data-loss
./bin/bootwright machine stop --name rhel-01,rhel-02,rhel-03
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
./bin/bootwright cluster info --name ceph-lab
virsh --connect qemu:///system list --all
./bin/bootwright context delete --name ceph-block-file --purge
```

Require the first destroy to refuse, the second to complete, and no domain or disk of the context to remain. After an interrupted destroy, read status and repeat destroy with the same authorization.
