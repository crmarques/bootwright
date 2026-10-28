# Six-host pools profile

## Budget and prerequisites

P1 declares six RHEL storage guests, five MONs, two MGRs and twelve OSDs.
The first two guests request 18 GiB each, the next three 15 GiB each and the
last 12 GiB: 93 GiB RAM and 24 vCPU total. Each guest has a 60 GiB root,
40 GiB SSD data disk, 40 GiB HDD data disk and 10 GiB SSD DB disk. Aggregate
storage is 360 GiB root plus 540 GiB data/DB, before provider reserves.
Reserve 4 GiB controller memory. Each OSD has a 4 GiB memory target
(`4294967296`) with autotuning disabled; this is not a process memory cap.

The SSD drive group authorizes only `/dev/sda` and requests encryption. The
HDD group authorizes only `/dev/sdb`, with DB on `/dev/sdc`, and requests
encryption with TPM2 unlock. Every provider profile declares a TPM; the
installation explicitly includes `cryptsetup` and `tpm2-tools`. These are
fixture prerequisites, not live qualification. Verify the selected native
build supports the requested TPM2 flow before operator acceptance. Device
proofs require the declared media, serial, size and original Machine identity;
no physical HDD or SSD qualification is claimed.

Two guest NICs use the example's shared lab bridge. `nic0` carries the public
subnet and `nic1` the cluster subnet; separate IP networks on a shared bridge
do not provide physical isolation. Replace documentation addresses and
provider allocations for the intended test environment. The supported cluster
execution range remains three through six storage hosts. A seventh host is
refused; this profile does not widen that bound.

## Initial setup

Follow the controller, media and local Secret preparation in the
[base storage example](libvirt-3-nodes-ceph.md). Reserve the aggregate
budget above plus controller and provider headroom. Use a new input directory
and context with disks dedicated to this acceptance run. Confirm all three
data disks on each guest are empty and distinct from the root disk.

## IBM entitlement

Prepare the base example's IBM entitlement and registry access through local
Secret sources. Verify access to the authored package and image versions.
Keep credentials and environment-specific acceptance records outside tracked
example files. No package, registry or physical-host qualification has been
run for this profile.

The tracked `storage/entitlement.yaml` sets `license.accept: true` as a placeholder. Accepting the IBM license is your explicit act: review its terms before you use your copy.

## Create the context

```sh
bootwright validate -f examples/ceph-clusters/libvirt-6-nodes-ceph-pools
bootwright context init --name pools-lab --input-dir examples/ceph-clusters/libvirt-6-nodes-ceph-pools
```

Review the plan for exactly six Ceph host blocks, the SSD class placement rule,
four pools and three owned RBD images. The EC 4+2 image uses replicated
`rbd-headers` for metadata and `ec42-data` for data; the EC 2+2 image similarly
uses `ec22-data`. `rbd-replicated` inherits protection 3/2 from `ssd-hosts`.

## Apply by stage

Run `bootwright apply --context pools-lab` and retain the per-stage results.
Check substrate disk media and TPM presentation, then installation readiness,
before evaluating the clusters stage. Six hosts with MON, MGR and two OSD
services fit the bounded execution plan. That offline bound is not a measured
apply duration. Stop on device or native-profile refusal; never bypass it with
manual zap or broad device selectors.

## Validate the cluster

Use native readback to confirm five MONs, two MGRs, twelve OSDs and the public
and cluster networks. Confirm six SSD-class data OSDs and six HDD-class data
OSDs; the DB devices must not become additional OSDs. Read-only readiness must
prove encryption tags and active LUKS topology for every OSD, and the TPM2 token
metadata for the HDD group. Never read or export dm-crypt keys.

Confirm `ssd-hosts` uses host failure domains and the SSD class. Verify the
replicated pool's effective 3/2 protection, quota, compression settings and PG
bounds; both EC profiles must report their declared chunk counts and HDD class.
Verify EC overwrite support for the RBD data pools and each image's exact
metadata/data-pool relation. Native defaults outside the declarations are not
part of this fixture's promised settings.

## Client access

With an operator-authorized native RBD client, write bounded test data to each
owned image and read it back. Keep the payload below the 1 GiB image size and
record checksums. Test the replicated image and both EC images independently;
matching image names alone do not prove the correct backing pools. The profile
does not provision an additional client guest or publish an administrator key.
Client access and checksums remain unrun acceptance rows.

## Failover

After healthy readback and a recorded checksum, use the operator's native
maintenance procedure to stop one storage guest. Verify MON quorum survives,
RBD I/O and checksums remain available within the declared protection, and
native health accurately reports the degraded state. Restore the same guest
and wait for recovery before testing another failure. Test a TPM2-enabled
OSD restart and verify the same encrypted data/DB identity and checksum.
Do not claim multiple-host failure tolerance from this single-host exercise.

## Interruption and continuation

Interrupt creation before the first OSD submission, retain the original
context, and reapply. Confirm completed native records are reused and no extra
OSDs, pools or images appear. Repeat at a pool/image creation boundary to
exercise original-attempt continuation. Any partial or nonempty DB/WAL state
must fail closed: B662 owns that recovery, so this profile does not promise
continuation through a partially allocated auxiliary disk. Capture the refusal
without key material and use the owned whole-cluster retirement procedure.

## Replay

Reapply the unchanged completed context and compare native OSD UUIDs, pool
IDs, image IDs and checksums. A completed apply must not create duplicate OSDs
or images, rewrite protection or adopt a replacement disk. Preserve the original
context throughout replay; constructing a new context is not continuation.

## Check that changed intent is refused

In a copy of the input, change an OSD path, media class, EC chunk count or RBD
backing pool and validate it. On the completed context, import the copy with
`context update` and retain its completed-apply warning; the update is allowed.
Run `apply` and require changed frozen intent to be refused before any native
effects. Restore the original input and import it again with `context update`
before further acceptance. Never use a changed path as permission to erase a
newly discovered disk.

## Measured budget

No operator measurement has been run. Record the full ledger, including the
provider's headroom and the clusters-stage peak, before accepting this profile.

| Field | Result |
| --- | --- |
| Guests | 6 declared; live unrun |
| Declared vCPU | 24 |
| Declared RAM GiB | 93 |
| Declared disk GiB | 360 root + 540 data/DB |
| MemAvailable at (a)/(b)/(c) | unrun |
| Minimum MemAvailable during clusters stage | unrun |
| Allocated vs apparent disk bytes | unrun |
| Per-guest balloon RSS | unrun |
| Daemons by type | unrun; planned MON 5, MGR 2, OSD 12 |
| Clusters-stage wall time | unrun |
| OOM kills | unrun |

## Cleanup

Stop and disconnect all clients, then follow the whole-cluster quiescence
procedure in [storage](../../specs/ceph.md). Run
`bootwright destroy --context pools-lab` only for this owned test context.
Verify the original owned cluster and provider resources are retired; do not
manually erase foreign or ambiguous devices. Retain the local acceptance ledger
and original context needed to diagnose interrupted cleanup. Online removal or
replacement of individual OSDs, pools or images is not qualified by P1.
