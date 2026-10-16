# Three-node Ceph on libvirt and sushy

This example declares three managed RHEL guests, explicit virtual SSD data disks, native MON/MGR/OSD documents, a size-2/min-size-2 RBD pool and a pool-scoped client. Addresses and names are synthetic. Copy the input to an operator-owned directory before adjusting it; keep real inputs and credentials outside tracked files.

The minimal virtual journey has operator acceptance; full Ceph feature and release qualification remain open in [M2](../../../specs/milestones/m2.md#m2b). The current lifecycle supports this core profile; other admitted storage declarations may refuse before effects. Storage preflight and standalone storage rendering are recognized but unavailable. This runbook uses validation and lifecycle plans.

The generic budget is twelve vCPUs, 24 GiB guest RAM, three 60 GiB root disks and three 80 GiB data disks, plus services, media and package tree. Reserve at least four further GiB of available host memory and account for full sparse disk growth. Adjust resource sizing only in your operator-owned copy while preserving quorum and replication safety.

This is below [IBM production memory recommendations](https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=recommendations-minimum-hardware-considerations). OSD autotuning is disabled, the memory target is 2 GiB, and global pool defaults are 2/2 before bootstrap. A target is best effort rather than a process cap. Actual resource measurements remain acceptance evidence. The [IBM three-node colocation profile](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=recommendations-colocation) requires SSD media. `dataDisks[].media: ssd` presents `/dev/sda` through virtio-SCSI with a nonrotational declaration and owned serial; root stays `/dev/vda`. Guest disk and Ceph SSD class checks must agree. Virtual media proves guest presentation rather than physical performance. Three guests on one hypervisor share its failure domain.

RHEL media must be acquired using the operator's Red Hat subscription. IBM's entitlement key authorizes the image pull; it does not authorize Red Hat media/CDN access.

## Initial setup

Follow [lab-rhel host prerequisites](../../machines/lab-rhel/README.md#host-prerequisites) for the supported controller host, KVM, firewall and routing. Bootwright installs baseline host packages through `setup`; the context's controller stage installs additional virtualization and media tools. Fleet and native cluster SSH keys are distinct; installation proves host keys.

On a [FIPS-mode controller](../../../docs/operator-guide.md#a-fips-mode-controller), change the generated fleet key in `bootwright-machine-key` from Ed25519 to `rsa` or `ecdsa-p256` before context import.

Review these inputs together in your copy:

- Controller address, service bind addresses, TLS SANs, DNS forwarder and NTP upstream must be reachable and agree. The sample upstream addresses are placeholders, not public services.
- Guest addresses, subnet, gateway, interface name, FQDNs and Ceph public CIDR must agree across Machines, NetworkConfig, provider and StorageCluster.
- The provider declares root `/dev/vda` and an explicit SSD at `/dev/sda`. The native OSD ServiceSpec selects only `/dev/sda`; verify disk identity before bootstrap.
- [cluster.yaml](storage/cluster.yaml) owns native MON/MGR/OSD services, placement, verified-tunnel SSH and explicit IBM call-home policy. [pool.yaml](storage/pool.yaml) declares replication 2/2; [client.yaml](storage/client.yaml) binds `rbd-canary` to that RBD pool.
- [entitlement.yaml](storage/entitlement.yaml) accepts IBM license terms. Review those terms before use.

Build from the repository root and invoke Bootwright as your normal account. Review its privilege and effect confirmations. Verify the acquired ISOs against vendor checksums before import; `media add --sha256` can enforce an expected checksum as well.

```sh
free -h
df -h /var/lib
ls -l /dev/kvm
ss -lntup
make build
./bin/bootwright version
./bin/bootwright setup --dry-run
./bin/bootwright setup
./bin/bootwright preflight controller
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
./bin/bootwright media list --checksums
```

Media is shared by contexts on this host. Registration freezes its identity and reserves the exact bytes, preventing replacement or deletion while bound. Keep both ISOs available through continuation and removal.

## IBM entitlement

The [Entitlement](storage/entitlement.yaml) binds `registry.credentialsRef: ibm-registry`; import the key into that [Secret](secret-descriptors/ibm-registry.yaml) below. The [cluster declaration](storage/cluster.yaml) uses the code-pinned IBM Storage Ceph 9.9.1.2 release and image tag. Apply resolves the tag through the Entitlement and freezes the linux/amd64 child digest; continuation never refreshes it. Apply admits only `cp.icr.io` and the verified-tunnel transport for this release. Supply credentials rather than a manual registry login or image pre-pull.

Run `secret set` with the key on standard input from a terminal or a protected file. Never place the entitlement key in YAML, a shell variable or an argument.

## Create the context

Replace `/path/to/adjusted/libvirt-3-nodes-ceph` below with your completed copy. Validation checks declarations and references, not host reachability or live readiness. The first staged apply freezes the whole plan, including the Storage objects and the Secrets they bind, so finish the input and set `ibm-registry` before the first apply. The guard refuses a `context update` that would invalidate a paused apply.

```sh
./bin/bootwright validate -f /path/to/adjusted/libvirt-3-nodes-ceph
./bin/bootwright context init --name ceph-lab --input-dir /path/to/adjusted/libvirt-3-nodes-ceph
./bin/bootwright context current
./bin/bootwright secret generate
./bin/bootwright secret set --name ibm-registry --username cp --password-stdin --yes
./bin/bootwright secret check
./bin/bootwright render effective
./bin/bootwright plan --stage controller
```

`context init` selects the new context. In later sessions select it with `context use --name ceph-lab`, or pass `--context ceph-lab` to context-backed commands.

## Apply by stage

A stage selection only gates which blocks an invocation starts; the plan stays complete. Each invocation below ends in a paused apply (`[OK] Apply paused`, `state: paused`) until the last one. Its `Next` line names `bootwright apply` without `--stage`, which would run every remaining stage at once, so repeat the command of the stage you mean.

```sh
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context ceph-lab
./bin/bootwright apply --stage infra-components,substrates
./bin/bootwright apply --stage machines
./bin/bootwright status
./bin/bootwright machine list --power-status
```

Before the clusters stage, check each guest's disks and confirm that it resolves the IBM hosts. The managed DNS server forwards every guest lookup to the `forwarders` of [dns.yaml](infra/components/dns.yaml); both lookups must answer:

```sh
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- getent hosts cp.icr.io public.dhe.ibm.com
```

An unreachable forwarder makes the guests' lookups time out during the clusters stage, after live effects have started. Repeat the check after every network change, such as a new Wi-Fi network, VPN or resolver. Then realize the cluster:

```sh
./bin/bootwright plan --stage clusters
./bin/bootwright apply --stage clusters
./bin/bootwright cluster list
./bin/bootwright diff
./bin/bootwright diff --observe
```

Each guest downloads the whole image on its own, so allow bandwidth and disk capacity for three full downloads. Signed packages and dependencies come from the IBM repository and the proved RHEL DVD source. If apply is interrupted, read status and the named private logs, correct the reported prerequisite and repeat the same stage command. It continues the same operation, observes unknown outcomes before retrying, and retains attempts already recorded.

`status` and `diff` without `--observe` read local records; `diff --observe` reads live targets without changing them. Repeating a completed apply with identical input settles from records and performs no repair.

Run `./bin/bootwright preflight storage-cluster` after guest installation to
inspect capacity and guest prerequisites. The provider's reserve values are
example host headroom; set them deliberately for your host. Missing or unknown
capacity cannot authorize managed Ceph apply. Preflight does not install or
repair prerequisites. Version coordinates in this example are authored choices,
not the only accepted versions. Optional `ceph.nfsGanesha.packageVersion`
selects its signed package without enabling NFS orchestration.

## Validate the cluster

`cluster info` prints the cluster's release, FSID, frozen image digest, monitors, admin node and its Ceph command (`cephCommand`), the line that runs `ceph` on the admin node with the administrator configuration and keyring that apply keeps there. It reads local evidence and contacts no host.

```sh
./bin/bootwright cluster info --name ceph-lab
```

Run each check below as that printed command followed by the arguments shown, written here as `<cephCommand>`. Replace its leading `bootwright` with `./bin/bootwright` when running from this checkout. Keep its printed context, node, configuration and keyring arguments. Only read the files under `/var/lib/bootwright/ceph/<fsid>/`; changing their contents or metadata breaks custody evidence. Expect `HEALTH_OK` with three MONs, an active and a standby MGR, and three up and in OSDs:

```text
<cephCommand> -s
<cephCommand> health detail
<cephCommand> mon stat
<cephCommand> mgr stat
<cephCommand> mgr services
<cephCommand> orch host ls
<cephCommand> orch ps
<cephCommand> osd tree
<cephCommand> osd pool ls detail
```

`osd pool ls detail` shows `rbd-data` with size 2, min_size 2 and the `rbd` application.

### Client data

This optional check writes 1 MiB through the cluster and compares it after reading it back. It uses the administrator keyring, so it does not test the pool client's authorization. It leaves a retained 16 MiB image that destroy removes with the owned disks. Take `<conf>` and `<keyring>` from the printed command, then import 1 MiB, grow the image and compare a read-back. Skip the write if `rbd-data/canary` already exists. The scratch file lives in `/var/tmp`, never under `/var/lib/bootwright/ceph/`:

```text
./bin/bootwright cluster exec --name ceph-lab --node rhel-01 -- sudo -n sh -c 'head -c 1048576 /dev/urandom > /var/tmp/canary.in && rbd --conf <conf> --keyring <keyring> import /var/tmp/canary.in rbd-data/canary && rbd --conf <conf> --keyring <keyring> resize --size 16M rbd-data/canary'
./bin/bootwright cluster exec --name ceph-lab --node rhel-01 -- sudo -n sh -c 'rbd --conf <conf> --keyring <keyring> export rbd-data/canary - | cmp -n 1048576 - /var/tmp/canary.in'
./bin/bootwright cluster exec --name ceph-lab --node rhel-01 -- sudo -n rbd --conf <conf> --keyring <keyring> info rbd-data/canary
```

The comparison exits 0 when the bytes match, and `info` reports a size of 16 MiB. If the first command's outcome is unknown, such as a dropped session, do not repeat the write: read the image with `info` and the second command, and judge from what the cluster holds. Passing this test does not claim the release's acceptance gate, which is the owner's accepted run.

## Dashboard

Apply enabled the Ceph dashboard on the active manager. `cluster info` reports its URLs, user, status and certificate SHA-256 fingerprint without the password. The generated password is sensitive and is shown only when you ask for secrets explicitly with the exact cluster name:

```sh
./bin/bootwright cluster info --name ceph-lab
./bin/bootwright cluster info --secrets --name ceph-lab
```

The certificate is self-signed. Before accepting the browser warning, compare the SHA-256 fingerprint the browser shows with the printed one.

A standby manager redirects to the active one. A status of `unproved` means the apply's TLS check did not reach the dashboard with the stored certificate; do not accept the warning until the fingerprints match.

## Changed input and build recovery

A completed apply keeps its frozen input. Changed desired state requires destroy followed by a fresh apply; use `diff` to review differences before deciding to retire the existing lab. Validate your adjusted copy before a later `context update --name ceph-lab --input-dir /path/to/adjusted/libvirt-3-nodes-ceph --yes`.

A compatible rebuilt executable can continue the same operation when its frozen blocks and runtime closure agree. An automation-only fix normally needs `make build` and the same apply command. A runtime-lock change needs `setup`; an incompatible frozen closure or effect pin refuses with the build and recovery action. Follow that diagnostic rather than assuming every build change requires destroy. See [continuation rules](../../../specs/state-reconciliation.md#dependency-safety-during-recovery).

Preserve context records, bound Secret versions and owned artifacts during recovery. Do not clear them to bypass an ownership refusal. After interruption, read `status`, the named private logs and `plan` before repeating the relevant stage command.

## One-node outage and recovery

This optional exercise stops a node other than the admin node, since the checks run there and its administrator files exist only on it. Confirm `rhel-03` is not the printed admin node first. With three MONs, two keep quorum, and the pool keeps `min_size` two; do not lower it.

```sh
./bin/bootwright machine stop --name rhel-03
./bin/bootwright machine list --power-status
```

Run `<cephCommand> -s`, `<cephCommand> health detail`, `<cephCommand> mon stat` and `<cephCommand> osd tree` as above. Health leaves `HEALTH_OK`, and the output shows one MON out of quorum and the OSD of `rhel-03` down. Start the node, wait until the guest answers, and repeat the checks until health returns to `HEALTH_OK` with three MONs in quorum and three OSDs up and in. Recovery needs no apply.

```sh
./bin/bootwright machine start --name rhel-03
./bin/bootwright machine exec --name rhel-03 -- uptime
```

## Cleanup

Review the removal plan and retain desired data first. This profile retires Ceph together with its three owned virtual hosts; removal while hosts survive remains unsupported. Follow `Stop first`, stop the entire original group, then authorize deletion of its disks. While they run, destroy refuses with `lifecycle.live`. Destroy removes the exact VM allocations and virtual disks the context owns, including every OSD disk and the retained image.

```sh
./bin/bootwright plan
./bin/bootwright machine stop --name rhel-01,rhel-02,rhel-03
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
./bin/bootwright cluster info --name ceph-lab
virsh --connect qemu:///system list --all
virsh --connect qemu:///system net-list --all
./bin/bootwright context delete --name ceph-lab --purge
```

After an interrupted destroy, inspect status and repeat destroy with the same authorization; a fresh removal handles what remains. Retain final absence evidence locally before deleting the context. Do not use `--allow-orphans` for routine cleanup. Shared host packages and media caches remain. No physical secure erasure or removal of external disk snapshots is claimed.
