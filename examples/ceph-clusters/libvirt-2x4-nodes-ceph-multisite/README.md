# Two managed Ceph clusters with RBD replication

This synthetic P7 input realizes one-way journal RBD mirroring and local
CephFS/RGW services on two four-host IBM Storage Ceph clusters. It does not
realize RGW multisite: the [native credential-interface blocker](../../../specs/ceph.md#rgw-multisite-blocker)
remains open. CephFS snapshot mirroring is refused. Full P7/B484 qualification
and all operator acceptance below remain **unrun**.

## Budget and prerequisites

The complete allocation is nine guests, 34 vCPUs, **106 GiB guest RAM**, and
520+480 GiB root+data disks. Each cluster uses 16, 16, 11 and 8 GiB on h1–h4
(51 GiB): h1/h2 run MON/MGR/OSD/MDS; h3 MON/OSD/RGW; h4 OSD/rbd-mirror.
Every Ceph guest has four vCPUs, 60 GiB root and one 60 GiB SSD data disk.
The client has two vCPUs, 4 GiB and 40 GiB root. Reserve another 4 GiB for
the controller, plus full sparse disk growth. This is the OD21 (b) allocation;
no reduced-memory waiver is implied. OSD memory is fixed at 4 GiB.

Provide the IBM entitlement and RHEL 9.8 boot/DVD media. The two synthetic
public networks must be mutually routed through the provider host; review
forwarding/firewall rules for native MON and OSD traffic in both directions.
Libvirt NAT provides outbound access, not a promise of cross-network reachability.
Authorize and verify those routes before cluster application. Cluster SSH uses
proved host keys; native peer authentication uses private CephX bootstrap.
This four-host example also requires the B688 execution-budget implementation.

## Initial setup

Copy the input to an operator-owned directory and adjust synthetic host/network
values there. Follow the [core setup](../libvirt-3-nodes-ceph/README.md#initial-setup)
for controller setup, preflight and media imports. Review the provider's four
profiles and the two network attachments, both cluster placements, both
`nativeName: volumes` pools and [replication.yaml](storage/replication.yaml).
Both pools explicitly select image mode; only the source declares the 1 GiB
journaling image `replicated-disk`. The receiver's pool starts empty.

The declared client is a budgeted test machine. It is a Bootwright-installed
Machine that is a node of no cluster, so the storage list retains it and this
context installs it with the eight topology Machines. To use an
operator-provided client with the same 4 GiB budget instead, remove the
`client` Machine. A context created under the earlier guidance already
realizes the client Machine, so remove the client Machine from one of the two
inputs before applying.

## IBM entitlement

Use the [core entitlement procedure](../libvirt-3-nodes-ceph/README.md#ibm-entitlement).
Both clusters use the authored IBM 9.9.2.0 image and RPM coordinates. Never
store keys, bootstrap tokens or private endpoint facts in this example.

The tracked `storage/entitlement.yaml` sets `license.accept: true` as a placeholder. Accepting the IBM license is your explicit act: review its terms before you use your copy.

## Create the context

```sh
./bin/bootwright validate -f /path/to/adjusted/libvirt-2x4-nodes-ceph-multisite
./bin/bootwright context init --name ceph-replication --input-dir /path/to/adjusted/libvirt-2x4-nodes-ceph-multisite
./bin/bootwright context use --name ceph-replication
./bin/bootwright secret generate
./bin/bootwright secret set --name ibm-registry --username cp --password-stdin --yes
./bin/bootwright secret check
./bin/bootwright render effective
./bin/bootwright plan --stage controller
```

Record validation's files-seen and objects-decoded counts. The first staged
apply freezes both clusters and the relationship in one operation.

## Apply by stage

```sh
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context ceph-replication
./bin/bootwright apply --stage infrastructure-services,substrates
./bin/bootwright apply --stage machines
./bin/bootwright machine exec --name primary-01,primary-02,primary-03,primary-04,secondary-01,secondary-02,secondary-03,secondary-04 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright plan --stage clusters
./bin/bootwright apply --stage clusters
./bin/bootwright status
```

Verify `/dev/sda` is the empty data disk on every guest and cross-network
MON/OSD reachability before the final stage. Start the memory sampler first.
The relationship waits for both owned pools, the source image and the receiving
rbd-mirror service. Bootstrap credentials are exchanged only through worker
memory/stdin; do not manually print or export bootstrap tokens.

## Validate the cluster

```sh
./bin/bootwright cluster info --name primary
./bin/bootwright cluster info --name secondary
./bin/bootwright diff --observe
```

Use each printed administrator command for `-s`, `health detail`, `orch ls
--refresh`, `orch ps --refresh`, `osd tree`, `osd pool ls detail`, and `fs
status <site>-files`. Expect three MONs, two MGRs, four up/in OSDs, two MDS,
one RGW and one rbd-mirror per cluster. Local RGW realms remain independent.
Use the printed conf/keyring paths only on the corresponding admin node:

```text
./bin/bootwright cluster exec --name primary --node primary-01 -- sudo -n rbd --conf <primary-conf> --keyring <primary-keyring> mirror pool info volumes --format json
./bin/bootwright cluster exec --name secondary --node secondary-01 -- sudo -n rbd --conf <secondary-conf> --keyring <secondary-keyring> mirror pool info volumes --format json
./bin/bootwright cluster exec --name secondary --node secondary-01 -- sudo -n rbd --conf <secondary-conf> --keyring <secondary-keyring> mirror image status volumes/replicated-disk --format json
```

Never add `--all` to pool info. Require receiver `up+replaying`, matching
global image IDs on both clusters, primary true only at source, and exactly
one receiver `rx-only` peer with source FSID/site and recorded UUID. Record
native image/pool IDs before and after replay. A successful apply establishes
initial synchronization; subsequent writes still require observed catch-up.

## Client access

Use the budgeted RHEL client or administrator commands on the source to write
a small uniquely named canary to `volumes/replicated-disk`; record its checksum
without credentials. Use the native RBD map/unmap and filesystem tools qualified
for the installed release, with the source administrator configuration delivered
privately by the operator. Keep that filesystem unmounted before demotion.
Do not mount the secondary while it is a non-primary mirror. After source
writes stop and receiver replay catches up, use the failover procedure to
read and compare the canary from the receiver. This data check is unrun.

## Failover

Bootwright does not promote, force-promote or fail back images. After recording
a clean baseline and stopping/unmapping all writers, exercise the native
[controlled RBD failover procedure](https://docs.ceph.com/en/tentacle/rbd/rbd-mirroring/)
under operator control: demote the source, prove replay caught up, then promote
the receiver and read the canary. Never force a split-brain promotion.
Restore original primary/secondary roles and synchronization before Bootwright
observation; an externally changed role must refuse readiness. Record the
commands, downtime and checksum privately. RGW multisite failover is blocked.

## Interruption and continuation

Interrupt clusters-stage apply once during relationship setup, then run the
same apply. Record operation identity, both FSIDs and native pool/image/peer
IDs. They must remain unchanged and completion must await initial sync.
Separately interrupt receiver reachability, record partial/unavailable state,
restore routing and retry the same operation. No new peer or credentials
may be adopted. Restore network rules after the exercise.

## Replay

```sh
./bin/bootwright apply --stage clusters
./bin/bootwright diff --observe
```

Expect unchanged relationship evidence and the same native IDs. Observation
must never create peers, rotate credentials or modify owned records.

## Check that changed intent is refused

In the operator-owned copy, change an image size, pool nativeName or peer
reference and validate it. Import with `context update` on the completed context
and retain its completed-apply warning; the update is allowed. Run `apply` and
require the frozen-plan changed-intent refusal before any native effects.
Restore the original copy and import it again with `context update`; do not
replace recorded FSIDs or native identities to bypass the refusal.
For pool mode, use a fresh context, set both mirroring modes to `pool`, and
remove `imageRefs`; repeat the entire runbook with every declared source image.

## Measured budget

All values below are **unrun**, except authored allocations. Keep the completed
[acceptance ledger](../../../docs/acceptance.md#ceph-matrix-row) outside tracked
inputs. Sample `/proc/meminfo` and each domain's `virsh dommemstat` before apply,
after Machines boot, and after all daemons settle; sample through the clusters
stage and preserve timestamps. Record sparse file allocated and apparent bytes,
provider CPU/disk totals, native daemon counts and kernel OOM events.

| Field | Authored value or required observation |
| --- | --- |
| Guests | 9: eight Ceph guests and one client |
| Declared vCPU | 34 |
| Declared RAM GiB | 106; controller reserve 4 additional |
| Declared disk GiB | 520 root + 480 data |
| MemAvailable at (a)/(b)/(c) | unrun: before apply / after Machines / settled |
| Minimum MemAvailable during clusters stage | unrun |
| Allocated vs apparent disk bytes | unrun |
| Per-guest balloon RSS | unrun |
| Daemons by type | expected MON 6, MGR 4, OSD 8, MDS 4, RGW 2, rbd-mirror 2; measured unrun |
| Clusters-stage wall time | unrun; one-image relationship deadline 9,180 seconds, each core budget independently bounded |
| OOM kills | unrun; require zero |

Also record Bootwright revision, release/image digests, package builds, files
seen/objects decoded, image and pool modes, service placement, initial-sync
elapsed time, peer outage duration, failover/checksum result, replay and
changed-intent result. Functional success alone does not qualify full P7.

## Cleanup

Stop clients and remove test mounts before retirement. Inspect the whole-context
destroy plan, then destroy both clusters and their owned Machines together:

```sh
./bin/bootwright plan
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
```

Review the CLI's data-loss authorization before accepting. Retiring only the
relationship while either cluster survives refuses under B491. Clean up the
separate client context only after its mounts and private credentials are
removed. Record absence of owned domains/disks and surviving external
infrastructure; live cleanup has not been run for this example.
