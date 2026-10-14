# Three-node Ceph on libvirt and sushy

This example declares three managed RHEL guests, explicit virtual SSD data disks, native MON/MGR/OSD documents, a size-2/min-size-2 RBD pool and a pool-scoped client. Generic addresses are synthetic. Keep real environment inputs in an operator-owned copy. The repository ignores `examples/wip/`.

This is the final procedure of the staged apply journey: from the first context to the removal of the cluster and its guests. It runs only `bootwright` commands and the named read-only diagnostics, and it installs nothing by hand.

Use one lab at a time and one clean build throughout its lifetime. Destroy old live contexts using their original build before changing automation or disk layout. The generic budget is twelve vCPUs, 24 GiB guest RAM, three 60 GiB root disks and three 80 GiB data disks, plus services, media and package tree. Reserve at least four further GiB of available host memory and account for full sparse disk growth. An ignored local copy may reduce these values for a measured development profile.

This is below [IBM production memory recommendations](https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=recommendations-minimum-hardware-considerations). OSD autotuning is disabled, the memory target is 2 GiB, and global pool defaults are 2/2 before bootstrap. A target is best effort rather than a process cap. Actual resource measurements remain acceptance evidence. The [IBM three-node colocation profile](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=recommendations-colocation) requires SSD media. `dataDisks[].media: ssd` presents `/dev/sda` through virtio-SCSI with a nonrotational declaration and owned serial; root stays `/dev/vda`. Guest disk and Ceph SSD class checks must agree. Virtual media proves guest presentation rather than physical performance. Three guests on one hypervisor share its failure domain.

Run in one clean checkout and build. The generic sample is examples/libvirt-3-nodes-ceph. Machine-specific inputs
remain in the Git-ignored examples/wip/libvirt-3-nodes-ceph copy. RHEL media
must be acquired using the operator's Red Hat subscription. IBM's entitlement
key authorizes the image pull; it does not authorize Red Hat media/CDN access.

## Initial setup

Follow [lab-rhel host prerequisites](../lab-rhel/README.md#host-prerequisites) for firewall and network configuration. Adjust controller/service addresses, TLS SANs and the guest subnet together in your operator-owned copy. Fleet and native cluster SSH keys are distinct; installation proves host keys. Bootwright installs the controller's host packages itself, so no package is installed by hand.

Run privileged commands yourself, invoking Bootwright as your normal account:

```sh
free -h
df -h /var/lib
ls -l /dev/kvm
ss -lntup
make build
./bin/bootwright version
./bin/bootwright setup
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
./bin/bootwright media list --checksums
```

## IBM entitlement

Declare [Entitlement](storage/entitlement.yaml) with `registry.credentialsRef: ibm-registry`, then import the entitlement key into that [Secret](secret-descriptors/ibm-registry.yaml) with the `secret set` command below. The [cluster declaration](storage/cluster.yaml) pins the IBM Storage Ceph 9.9.1.2 image tag; apply resolves that tag through the Entitlement and freezes its digest. Apply admits only the entitlement registry `cp.icr.io` and the verified-tunnel transport for this release. The IBM registry entitlement does not authorize RHEL media/CDN access; acquire the boot/DVD ISOs separately with the Red Hat subscription.

Run `secret set` with the key on standard input from a terminal or a protected file. Never place the entitlement key in YAML, a shell variable or an argument.

## Create the context

Copy the example to an operator-owned directory and adjust it. The first staged apply freezes the whole plan, including the Storage objects and the Secrets they bind, so finish the input and set `ibm-registry` before the first apply. The guard refuses a `context update` that would invalidate a paused apply.

```sh
./bin/bootwright validate -f /path/to/adjusted/libvirt-3-nodes-ceph
./bin/bootwright context init --name ceph-lab
./bin/bootwright context update --name ceph-lab --input-dir /path/to/adjusted/libvirt-3-nodes-ceph --yes
./bin/bootwright context use --name ceph-lab
./bin/bootwright secret generate
./bin/bootwright secret set --name ibm-registry --username cp --password-stdin --yes
./bin/bootwright secret check
```

The unmodified example validates as `files seen: 23, objects decoded: 23`; an adjusted copy reports its own count.

## Apply by stage

A stage selection only gates which blocks an invocation starts; the plan stays complete. Each invocation below ends in a paused apply (`[OK] Apply paused`, `state: paused`) until the last one. Its `Next` line names `bootwright apply` without `--stage`, which would run every remaining stage at once, so repeat the command of the stage you mean.

```sh
./bin/bootwright apply --stage controller
./bin/bootwright apply --stage infra-components,substrates
./bin/bootwright apply --stage machines
./bin/bootwright status
./bin/bootwright machine list --power-status
```

Before the clusters stage, check each guest's disks and confirm that it resolves the IBM hosts. The managed DNS server forwards every guest lookup to the `forwarders` of [dns.yaml](infra/components/dns.yaml); both lookups must answer:

```sh
./bin/bootwright machine exec --name rhel-01 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright machine exec --name rhel-02 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright machine exec --name rhel-03 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright machine exec --name rhel-01 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright machine exec --name rhel-02 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright machine exec --name rhel-03 -- getent hosts cp.icr.io public.dhe.ibm.com
```

An unreachable forwarder makes the guests' lookups time out during the clusters stage, after live effects have started. Repeat the check after every network change, such as a new Wi-Fi network, VPN or resolver. Then realize the cluster:

```sh
./bin/bootwright apply --stage clusters
```

Each guest downloads the whole image on its own, so plan the guests' registry bandwidth for three full downloads. A pull gets 3 attempts of 1200 s on each host, so a link needs about 7 Mbit/s per GB of compressed image to complete one attempt; a slower link exhausts the attempts and refuses `image-pull`. Signed packages and dependencies come from the IBM repository and the RHEL DVD source. If the apply is interrupted while the hosts are prepared, run the same `bootwright apply --stage clusters` again: it continues the same operation, and the attempts already recorded still count.

## Validate the cluster

`cluster info` prints the cluster's release, FSID, frozen image digest, monitors, admin node and its Ceph command (`cephCommand`), the line that runs `ceph` on the admin node with the administrator configuration and keyring that apply keeps there. It reads local evidence and contacts no host.

```sh
./bin/bootwright cluster info --name ceph-lab
```

Run each check below as that printed command followed by the arguments shown, written here as `<cephCommand>`. It has the shape `bootwright cluster exec --context ... --name ceph-lab --node rhel-01 -- sudo -n ceph --conf /var/lib/bootwright/ceph/<fsid>/ceph.conf --keyring /var/lib/bootwright/ceph/<fsid>/ceph.client.admin.keyring`. Only read the files under `/var/lib/bootwright/ceph/<fsid>/`; any write, chmod or touch breaks the custody proof, and later Bootwright calls refuse with `ownership`. Health reaches `HEALTH_OK` with three MONs, an active and a standby MGR, and three up and in OSDs:

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
<cephCommand> auth get client.rbd-canary
```

`osd pool ls detail` shows `rbd-data` with size 2, min_size 2 and the `rbd` application. `auth get` prints the client's key, so run it only on a terminal you do not record, and read the `caps` lines.

### Client data

The client-data test writes 1 MiB through the cluster and compares it after reading it back. It runs on the admin node with the administrator keyring, and leaves a retained 16 MiB image: no step deletes it by name, and the destroy below removes it with the pool. Take `<conf>` and `<keyring>` from the printed command, then import 1 MiB into the image, grow it to 16 MiB, and compare a read-back with the input. The scratch file lives in `/var/tmp`, never under `/var/lib/bootwright/ceph/`:

```text
bootwright cluster exec --name ceph-lab --node rhel-01 -- sudo -n sh -c 'head -c 1048576 /dev/urandom > /var/tmp/canary.in && rbd --conf <conf> --keyring <keyring> import /var/tmp/canary.in rbd-data/canary && rbd --conf <conf> --keyring <keyring> resize --size 16M rbd-data/canary'
bootwright cluster exec --name ceph-lab --node rhel-01 -- sudo -n sh -c 'rbd --conf <conf> --keyring <keyring> export rbd-data/canary - | cmp -n 1048576 - /var/tmp/canary.in'
bootwright cluster exec --name ceph-lab --node rhel-01 -- sudo -n rbd --conf <conf> --keyring <keyring> info rbd-data/canary
```

The comparison exits 0 when the bytes match, and `info` reports a size of 16 MiB. If the first command's outcome is unknown, such as a dropped session, do not repeat the write: read the image with `info` and the second command, and judge from what the cluster holds. Passing this test does not claim the release's acceptance gate, which is the owner's accepted run.

## Dashboard

Apply enabled the Ceph dashboard on the active manager. `cluster info` reports its URLs, user, status and certificate SHA-256 fingerprint without the password. The generated password is sensitive and is shown only when you ask for secrets explicitly with the exact cluster name:

```sh
./bin/bootwright cluster info --name ceph-lab
./bin/bootwright cluster info --secrets --name ceph-lab
```

The certificate is self-signed. Before accepting the browser warning, compare the fingerprint the browser shows with the printed one, or read it from the active manager's port, taking the host and port from the printed URL:

```sh
openssl s_client -connect rhel-01.lab.example.test:8443 </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256
```

A standby manager redirects to the active one. A status of `unproved` means the apply's TLS check did not reach the dashboard with the stored certificate; do not accept the warning until the fingerprints match.

## Check that changed intent is refused

A completed apply keeps its frozen input. Edit one value in your operator-owned copy, for example the `autoscale.mode` of [pool.yaml](storage/pool.yaml) from `'on'` to `'warn'`, and update the context. `context update` is admitted with a `lifecycle.state` warning that apply refuses changed input until `bootwright destroy` takes back what the apply owns. `apply` then refuses the changed intent and starts nothing. Revert the edit, update again, and apply settles without effect because the input again equals the bytes the completed apply froze:

```sh
./bin/bootwright validate -f /path/to/adjusted/libvirt-3-nodes-ceph
./bin/bootwright context update --name ceph-lab --input-dir /path/to/adjusted/libvirt-3-nodes-ceph --yes
./bin/bootwright apply
./bin/bootwright context update --name ceph-lab --input-dir /path/to/adjusted/libvirt-3-nodes-ceph --yes
./bin/bootwright apply
./bin/bootwright status
```

## One-node outage and recovery

Stop a node that is not the admin node, since the checks run there and its administrator files exist only on it. With three MONs, two keep quorum, and the pool keeps `min_size` two; do not lower it.

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

`destroy` takes back what the apply owns. The Ceph cluster is retired together with its three owned virtual hosts: destroy contacts no host for it, requires `--authorize data-loss`, and needs the hosts stopped first. While they run, destroy refuses with `lifecycle.live`, naming `bootwright machine stop` and the hosts under `Stop first`. Stop the entire original group, then destroy again. Destroy removes the exact VM allocations and virtual disks the context owns, including every OSD disk and the retained image.

```sh
./bin/bootwright destroy --authorize data-loss
./bin/bootwright machine stop --name rhel-01
./bin/bootwright machine stop --name rhel-02
./bin/bootwright machine stop --name rhel-03
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
systemctl list-units --all 'bootwright-*'
virsh --connect qemu:///system list --all
virsh --connect qemu:///system net-list --all
./bin/bootwright context delete --name ceph-lab --purge
```

No physical secure erasure or removal of external disk snapshots is claimed. Keep shared host media and image caches separate from this context's cleanup. Retain final absence evidence locally before deleting the context.
