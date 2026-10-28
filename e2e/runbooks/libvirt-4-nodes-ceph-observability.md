# P3: Ceph observability and management

Synthetic qualification input for [M2c](../../specs/milestones/m2.md#m2c). No live execution or operator acceptance is claimed. Copy this directory outside Git before supplying real identities, addresses and credentials. This profile has a size-2/min-size-2 RBD pool and one explicit SSD per guest.

## Budget and prerequisites

4 guests, 16 vCPU, 62 GiB RAM and 260+320 GiB root+data disks. Reserve another 4 GiB controller memory, media/package storage and full sparse-disk growth. IBM base figures are 11, 11, 8 and 5 GB (35 GB total). Declared RAM is 16, 16, 10 and 20 GiB: the two gateway/proxy hosts add 3 GiB each beyond base plus 2 GiB OS headroom, and the observability host adds 13 GiB beyond its OSD plus 2 GiB OS headroom. These extra service allowances are qualification budgets, not vendor minima. Root disks are 60/60/60/80 GiB; all four data disks are 80 GiB.

OSD autotuning is disabled with a 4 GiB target (`4294967296`), which is not a process cap. Provide a trusted OIDC IdP, its client registration and role mapping, and an SNMPv3 receiver. Replace every synthetic auxiliary image digest with a verified selection from the chosen release. Configure DNS for the management endpoint, the IdP CA and callback, SNMP destination and credentials. Allow the gateway port and VRRP on both ingress hosts. Prometheus retention is 15 days/10 GB; capacity must include retained logs.

## Initial setup

Follow [controller prerequisites](lab-rhel.md#host-prerequisites), build with `make build`, then run:

```sh
./bin/bootwright setup --dry-run
./bin/bootwright setup
./bin/bootwright preflight controller
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
```

Verify media checksums with the vendor before importing. Align synthetic addresses, DNS, routes, service binds and certificate SANs in your copy.

## IBM entitlement

Follow the [core profile's IBM entitlement procedure](libvirt-3-nodes-ceph.md#ibm-entitlement). This profile authors Ceph 9.9.2.0, image `v9.9.2-26947` and RHEL 9 packages `20.2.2-349.el9cp`; verify these coordinates with your entitlement before use. Red Hat media/CDN authorization is separate. Supply Secrets through protected files or standard input.

The tracked `storage/entitlement.yaml` sets `license.accept: true` as a placeholder. Accepting the IBM license is your explicit act: review its terms before you use your copy.

## Create the context

Validate the adjusted input; the unchanged fixture decodes 32 files seen and 32 objects decoded. Set all credentials before the first staged apply freezes intent.

```sh
./bin/bootwright validate -f /path/to/adjusted/libvirt-4-nodes-ceph-observability
./bin/bootwright context init --name ceph-lab --input-dir /path/to/adjusted/libvirt-4-nodes-ceph-observability
./bin/bootwright context update --name ceph-lab --input-dir /path/to/adjusted/libvirt-4-nodes-ceph-observability --yes
./bin/bootwright context use --name ceph-lab
./bin/bootwright secret generate
./bin/bootwright secret set --name ibm-registry --username cp --password-stdin
./bin/bootwright secret set --name oidc-client --value-stdin
./bin/bootwright secret set --name oidc-cookie --value-stdin
./bin/bootwright secret set --name oidc-ca --certificate-file /path/to/idp-ca.pem
./bin/bootwright secret set --name snmp-auth --username qualification-user --password-stdin
./bin/bootwright secret set --name snmp-privacy --value-stdin
./bin/bootwright secret check
./bin/bootwright render effective
```

## Apply by stage

```sh
./bin/bootwright plan
./bin/bootwright apply --stage controller
./bin/bootwright apply --stage infrastructure-services,substrates
./bin/bootwright apply --stage machines
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03,rhel-04 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03,rhel-04 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03,rhel-04 -- chronyc tracking
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

Require three MONs in quorum, two MGRs, 4 up/in SSD OSDs, active+clean PGs and the pool at 2/2. For each OSD check `config show osd.<id> osd_memory_target` equals `4294967296` and autotuning is false. Match live FSID and image digest to local evidence. Record any Call Home warning separately; do not call it HEALTH_OK.

Require Prometheus targets up for all four node/Ceph exporters, configured retention, Alertmanager delivery to the receiver, and Grafana authenticated dashboards. Require Loki to return a fresh Alloy-collected record from every host. Record only redacted results. From the client, verify the management endpoint certificate and hostname, complete an IdP login and logout, and test allowed and denied roles. Confirm the OAuth2 proxy verifies the IdP certificate: a wrong CA must fail, never disable verification. Confirm an authenticated, encrypted SNMPv3 alert arrives at the receiver. Endpoint reachability alone proves none of these results.

## Client access

Run the [pool-scoped RBD client read/write comparison](libvirt-3-nodes-ceph.md#pool-client) from the admin guest through `cluster exec`, using the paths printed by this context's `cluster info`. Retain only comparison and capability results; never save raw key output. Also perform the browser, IdP, alert and log checks above from the operator client and external receiver.

## Failover

Confirm the selected host is not the admin node. Stop rhel-02, one management gateway and OAuth2 proxy host, and test a fresh login through the same VIP within 300 seconds.

```sh
./bin/bootwright machine stop --name rhel-02
./bin/bootwright machine start --name rhel-02
```

Between stop and start, collect health and client results; after start require recovery within 600 seconds, all OSDs up/in and active+clean PGs. A timeout is a failed qualification. The single observability host has no redundancy: stopping it is not an observability HA test.

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

Record every [matrix ledger field](../../docs/acceptance.md#ceph-matrix-row): Guests (4), Declared vCPU (16), Declared RAM GiB (62), Declared disk GiB (580), MemAvailable at (a)/(b)/(c), Minimum MemAvailable during clusters stage, Allocated vs apparent disk bytes, Per-guest balloon RSS, Daemons by type, Clusters-stage wall time and OOM kills. Check host and guest kernel journals for OOM events. Measurements remain unrun until the operator executes this procedure.

## Cleanup

Retain required evidence and data first. With guests running, the first destroy must refuse `lifecycle.live`; then stop the whole group and repeat:

```sh
./bin/bootwright destroy --authorize data-loss
./bin/bootwright machine stop --name rhel-01,rhel-02,rhel-03,rhel-04
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
./bin/bootwright cluster info --name ceph-lab
virsh --connect qemu:///system list --all
virsh --connect qemu:///system net-list --all
```

Prove the owned domains, disks and network are absent before deleting the context. External IdP, SNMP receiver, subscriptions and credentials have their own operator cleanup. Shared media and packages remain. No physical secure erasure is claimed.
