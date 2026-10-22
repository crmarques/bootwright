# P5: Ceph host and security variants

Synthetic qualification input for [M2c](../../../specs/milestones/m2.md#m2c). No live execution or operator acceptance is claimed. Copy this directory outside Git before supplying real identities, addresses and credentials. This profile has a size-2/min-size-2 RBD pool and one explicit SSD per guest.

## Budget and prerequisites

3 guests, 12 vCPU, 36 GiB RAM and 180+240 GiB root+data disks. Reserve another 4 GiB controller memory, media/package storage and full sparse-disk growth. IBM base figures are 11, 11 and 8 GB (30 GB total); declared RAM is 13, 13 and 10 GiB, including 2 GiB OS headroom per guest. Each guest has a 60 GiB root and an 80 GiB SSD data disk. Each has two virtual NICs, an active-backup bond and VLAN 140 carrying the cluster network. Both NICs share one attachment and bridge: this proves neither physical network isolation nor independent links.

OSD autotuning is disabled with a 4 GiB target (`4294967296`), which is not a process cap. Provide Red Hat RHSM organization and activation-key Secrets. The install profile enables FIPS without OSPP and LUKS root encryption with TPM2 and recovery; both SSH keys use ECDSA P-256, and CephX uses AES. The empty TPM2 policy intentionally selects no PCRs: it protects a copied disk without its TPM, not a tampered boot. See the [TPM policy contract](../../../specs/managed-os.md). Recovery and post-install reboot access require live qualification.

## Initial setup

Follow [controller prerequisites](../../machines/lab-rhel/README.md#host-prerequisites), build with `make build`, then run:

```sh
./bin/bootwright setup --dry-run
./bin/bootwright setup
./bin/bootwright preflight controller
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
```

Verify media checksums with the vendor before importing. Align synthetic addresses, DNS, routes, service binds and certificate SANs in your copy.

## IBM entitlement

Follow the [core profile's IBM entitlement procedure](../libvirt-3-nodes-ceph/README.md#ibm-entitlement). This profile authors Ceph 9.9.2.0, image `v9.9.2-26947` and RHEL 9 packages `20.2.2-349.el9cp`; verify these coordinates with your entitlement before use. Red Hat media/CDN authorization is separate. Supply Secrets through protected files or standard input.

## Create the context

Validate the adjusted input; the unchanged fixture decodes 28 files seen and 28 objects decoded. Set all credentials before the first staged apply freezes intent.

```sh
./bin/bootwright validate -f /path/to/adjusted/libvirt-3-nodes-ceph-host-variants
./bin/bootwright context init --name ceph-lab --input-dir /path/to/adjusted/libvirt-3-nodes-ceph-host-variants
./bin/bootwright context update --name ceph-lab --input-dir /path/to/adjusted/libvirt-3-nodes-ceph-host-variants --yes
./bin/bootwright context use --name ceph-lab
./bin/bootwright secret generate
./bin/bootwright secret set --name ibm-registry --username cp --password-stdin
./bin/bootwright secret set --name rhsm-org --value-stdin
./bin/bootwright secret set --name rhsm-key --value-stdin
./bin/bootwright secret set --name luks-recovery --value-stdin
./bin/bootwright secret check
./bin/bootwright render effective
```

## Apply by stage

```sh
./bin/bootwright apply --stage controller
./bin/bootwright apply --stage infra-components,substrates
./bin/bootwright apply --stage machines
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- chronyc tracking
./bin/bootwright preflight storage-cluster
./bin/bootwright plan --stage clusters
```

Require the declared disk identities and SSD presentation, synchronized clocks and working resolution. Complete the profile-specific host checks below before clusters. Start the memory sampler in Measured budget in another terminal, then run `./bin/bootwright apply --stage clusters` and record wall time.

## Validate the cluster

```sh
./bin/bootwright status
./bin/bootwright diff --observe
./bin/bootwright cluster info --name ceph-lab
```

Use the printed `cephCommand` prefix for these read-only checks:

```text
<cephCommand> -s
<cephCommand> health detail
<cephCommand> orch ls --refresh
<cephCommand> orch ps --refresh --format json-pretty
<cephCommand> osd tree
<cephCommand> osd pool ls detail
<cephCommand> mgr services
<cephCommand> mon dump
```

Require three MONs in quorum, two MGRs, 3 up/in SSD OSDs, active+clean PGs and the pool at 2/2. For each OSD check `config show osd.<id> osd_memory_target` equals `4294967296` and autotuning is false. Match live FSID and image digest to local evidence. Record any Call Home warning separately; do not call it HEALTH_OK.

Before clusters and again after a reboot, run on every guest:

```sh
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- cat /proc/sys/crypto/fips_enabled
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- update-crypto-policies --show
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- sudo -n nmstatectl show
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- cat /proc/net/bonding/bond0
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- sudo -n subscription-manager identity
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- sudo -n cryptsetup status luks-root
```

Use the actual root mapper name from lsblk in place of `luks-root`. Require FIPS value 1 and policy FIPS, no OSPP, active-backup membership and VLAN addresses matching intent, and managed registration. Retain registration identity output privately. Inspect the root LUKS header and clevis slot without exporting keys. Test the recovery passphrase from a protected console and verify TPM unlock on reboot; never record the passphrase. Check the monitor cipher policy and client capability metadata without exporting its key.

## Client access

Run the [pool-scoped RBD client read/write comparison](../libvirt-3-nodes-ceph/README.md#pool-client) from the admin guest through `cluster exec`, using the paths printed by this context's `cluster info`. Retain only comparison and capability results; never save raw key output. Repeat the read-back after reboot and network recovery; FIPS configuration alone does not prove encrypted-root recovery or access.

## Failover

Confirm the selected host is not the admin node. Stop rhel-03 and record MON quorum, PG availability and the client read result within 300 seconds. A 2/2 pool can block I/O when a required replica is down; do not lower min_size.

```sh
./bin/bootwright machine stop --name rhel-03
./bin/bootwright machine start --name rhel-03
```

Between stop and start, collect health and client results; after start require recovery within 600 seconds, all OSDs up/in and active+clean PGs. A timeout is a failed qualification. With console access, separately disconnect the active virtual NIC and prove bond failover, restore it, then reboot and prove SSH, VLAN connectivity and TPM root unlock. Recovery-passphrase testing needs a controlled console exercise and retained local evidence.

## Interruption and continuation

On the first clusters-stage run, interrupt once with Ctrl-C after the first Ceph block starts. Record `./bin/bootwright status`, repeat `./bin/bootwright apply --stage clusters`, and prove the operation ID is unchanged and completed attempts are retained. Do not change frozen input or remove custody records.

## Replay

After completion, run `./bin/bootwright apply` again and record settlement without effects. This proves replay from records, not live repair.

## Check that changed intent is refused

Change the pool in the operator-owned input and validate. Run `context update` on the completed context and retain its completed-apply warning; the update itself is allowed. Then run `apply` and require changed frozen intent to be refused before any native effects. Restore the original input and run `context update` again before continuation or retirement. Use destroy and a fresh context for changed desired state.

## Measured budget

Keep evidence outside Git. Set `ev` to a private evidence directory. Before first apply (a), after clusters (b), and after client/failover checks (c), run:

```sh
nproc
grep MemAvailable /proc/meminfo
sudo du -sb --apparent-size /var/lib/libvirt/images/bootwright/ceph-lab
sudo du -sb /var/lib/libvirt/images/bootwright/ceph-lab
virsh --connect qemu:///system domstats --balloon --vcpu
```

After cluster creation also record `<cephCommand> orch ps --format json-pretty` and `<cephCommand> df`. An absent image directory at (a) is recorded as absent. During clusters run in a second terminal and stop with Ctrl-C when apply returns:

```sh
while sleep 10; do date +%s; grep MemAvailable /proc/meminfo; done > "$ev/memavailable-clusters.txt"
```

Record every [matrix ledger field](../../../docs/acceptance.md#ceph-matrix-row): Guests (3), Declared vCPU (12), Declared RAM GiB (36), Declared disk GiB (420), MemAvailable at (a)/(b)/(c), Minimum MemAvailable during clusters stage, Allocated vs apparent disk bytes, Per-guest balloon RSS, Daemons by type, Clusters-stage wall time and OOM kills. Check host and guest kernel journals for OOM events. Measurements remain unrun until the operator executes this procedure.

## Cleanup

Retain required evidence and data first. With guests running, the first destroy must refuse `lifecycle.live`; then stop the whole group and repeat:

```sh
./bin/bootwright destroy --authorize data-loss
./bin/bootwright machine stop --name rhel-01,rhel-02,rhel-03
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
./bin/bootwright cluster info --name ceph-lab
virsh --connect qemu:///system list --all
virsh --connect qemu:///system net-list --all
```

Prove the owned domains, disks and network are absent before deleting the context. External IdP, SNMP receiver, subscriptions and credentials have their own operator cleanup. Shared media and packages remain. No physical secure erasure is claimed.
