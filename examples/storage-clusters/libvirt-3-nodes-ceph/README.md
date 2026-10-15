# Three-node Ceph on libvirt and sushy

This example declares three managed RHEL guests, explicit virtual SSD data disks, native MON/MGR/OSD documents, a size-2/min-size-2 RBD pool, an SSD placement policy (owned CRUSH rule `ssd-hosts`) and a pool-scoped client. Addresses and names are synthetic. Copy the input to an operator-owned directory before adjusting it; keep real inputs and credentials outside tracked files.

The minimal virtual journey has operator acceptance; full Ceph feature and release qualification remain open in [M2](../../../specs/milestones/m2.md#m2b). The current lifecycle supports this core profile; other admitted storage declarations may refuse before effects. Standalone storage rendering (`render storage`) is recognized but unavailable. This runbook uses validation and lifecycle plans.

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

The [Entitlement](storage/entitlement.yaml) binds `registry.credentialsRef: ibm-registry`; import the key into that [Secret](secret-descriptors/ibm-registry.yaml) below. The [cluster declaration](storage/cluster.yaml) selects IBM Storage Ceph 9.9.2.0 (Tentacle), the latest release in the [IBM release table](https://www.ibm.com/support/pages/what-are-red-hat-and-ibm-storage-ceph-releases-and-corresponding-ceph-package-versions) on 2026-10-18, based on upstream Ceph 20.2.2. This example authors the build tag `v9.9.2-26729`, which the owner confirmed exists in the entitlement's `ceph-9-rhel10` repository ([D165](../../../specs/milestones/backlog.md#decisions)). The table shows the RHEL 10 package build; this RHEL 9.8 example selects the corresponding `20.2.2-349.el9cp` packages and `cephadm-ansible` `5.0.3-1.el9cp` published in [IBM's RHEL 9 repository](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/). These are authored selections, not code pins or a claim of release acceptance.

The container repository is `cp.icr.io/cp/ibm-ceph/ceph-9-rhel10`. The signed [RHEL 9 cephadm 20.2.2 package](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/cephadm-20.2.2-349.el9cp.noarch.rpm) names that repository in its embedded `cephadmlib/constants.py` default image. The guest OS, native RPM build and container base are separate selections; the RHEL 9 guests retain their RHEL 9 packages. Verify the exact repository/tag pair with authenticated registry metadata before starting a new context. A release-table build tag alone does not identify an available container. A reproducible operator-owned input selects a verified build tag, as this example does, or a platform-manifest digest, rather than a moving release alias; apply freezes the resolved digest in either case.

Apply resolves the tag through the Entitlement and freezes the linux/amd64 child digest; continuation never refreshes it. The exact image must be available through the operator's entitlement. This example selects `cp.icr.io` and the verified-tunnel transport. Supply credentials rather than a manual registry login or image pre-pull.

Run `secret set` with the key on standard input from a terminal or a protected file. Never place the entitlement key in YAML, a shell variable or an argument.

## Other IBM Storage Ceph releases

Edit only your operator-owned copy. The tracked input authors 9.9.2.0. Choosing the release and its coordinates is the operator's responsibility: Bootwright registers and enforces no version rule (D157, D163), and the native tools judge installability. When you select another release, these fields move together: `ceph.release`, `ceph.packageVersion`, `ceph.cephadm.ansible.packageVersion`, the image `base` with its `version`, and `ceph.ibm.callHome` for the Call Home model of the selected build.

- Take the coordinates from [IBM's release table](https://www.ibm.com/support/pages/what-are-red-hat-and-ibm-storage-ceph-releases-and-corresponding-ceph-package-versions). Select a build tag, as this example does with `v9.9.2-26729`, rather than a moving alias such as `v9.9.2` for a reproducible input, and verify each repository and tag pair with authenticated registry metadata; IBM pages can show more than one tag for a release, so confirm which tag your entitlement holds.
- Select the full package release, such as `5.0.3-1.el9cp` for `cephadm-ansible`, with its dist suffix. A shortened version does not name the signed package. Select the package build that the repository publishes for your guest's operating system, even where the release table lists a different build.
- Call Home follows the selected build. A build whose Call Home is opt-in, such as the `20.1.0-221` build, takes `ceph.ibm.callHome: disabled`: apply neither initializes nor enables Call Home, and the IBM Call Home check below expects `call_home_agent` not to be enabled instead of enabled. An opt-out build such as `20.2.x` takes `enabled`. A mismatch refuses `call-home-unqualified` at the cluster claim, after the hosts are prepared from the frozen input, so author `callHome` for the selected build before the first apply: correcting it later needs `bootwright destroy`, then `context update` and a fresh apply. The [catalog](../../../.agents/knowledge/version-specializations.md#ceph-call-home-opt-in) owns the reason.
- A build whose bootstrap makes the IBM license call fails at it on the zero-host seed (`no hosts defined`). Bootwright recognizes exactly that refusal and continues the same owned seed in the same apply, so no second apply is needed. The output shape of that refusal is read from the signed packages and is not yet confirmed on a live run: if an apply still ends at that bootstrap failure, apply again, because the next apply continues the seed. Any other bootstrap failure is a real failure. The [catalog](../../../.agents/knowledge/version-specializations.md#ceph-bootstrap-license-call-continues) owns the reason.
- To use guests of another RHEL minor, work in your own copy: copy `infra/os/rhel-9-8.yaml` and `infra/os/rhel-9-8-boot.yaml` to names for that minor, set the profile's `os.version`, `imageRef` to the new image, `bootMedia` and `fromMedia` to the boot and DVD ISOs of that minor, and `installProfileRef` of each machine to the new profile. Import the media first with `./bin/bootwright media add --name <boot-iso-name> --from-file /path/to/<boot-iso>` and the same for the DVD ISO. This tracked example carries no files for another minor; keep them in your own copy.
- The validation below expects the tracked selection (`20.2.2-349.el9cp` and `5.0.3-1.el9cp`); compare against your selected values instead.

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

A stage selection only gates which blocks an invocation starts; the plan stays complete. Each invocation below ends in a paused apply (`✓ Apply paused`, `state: paused`) until the last one. Its `Next` line names `bootwright apply` without `--stage`, which would run every remaining stage at once, so repeat the command of the stage you mean.

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

After the machines stage and before `apply --stage clusters`, you can run
`./bin/bootwright preflight storage-cluster` as an informative check of
capacity and guest prerequisites. The provider's reserve values are example
host headroom; set them deliberately for your host. Apply re-checks capacity
itself while a libvirt Machine may still start, so the machines stage refuses
on missing or unknown capacity. Once every Machine is done,
`apply --stage clusters` does not re-prove host capacity, so run this
preflight first if host load may have changed. Preflight
does not install or repair prerequisites. Version coordinates in this example are authored choices,
not the only accepted versions. Optional `ceph.nfsGanesha.packageVersion`
selects its signed package without enabling NFS orchestration.

## Validate the cluster

First check live observations and guest prerequisites after the final apply completes:

```sh
./bin/bootwright status
./bin/bootwright diff --observe
./bin/bootwright preflight storage-cluster
./bin/bootwright machine list --power-status
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- systemctl is-active sshd chronyd qemu-guest-agent
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- chronyc tracking
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- chronyc sources -v
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- rpm -q cephadm ceph-common cephadm-ansible
./bin/bootwright machine exec --name rhel-01,rhel-02,rhel-03 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
```

Require a completed apply, no unexplained live drift, successful prerequisite checks, three powered-on guests, active services and synchronized clocks (`Leap status: Normal` with a selected `^*` time source). Expect Ceph packages `20.2.2-349.el9cp` and cephadm-ansible `5.0.3-1.el9cp` on every guest. The owned data disk is `/dev/sda`, nonrotational (`ROTA=0`), with its declared serial; the root disk remains `/dev/vda`. After Ceph provisions the OSD, the data disk is in use and need not be blank. Command success or a locally recorded completed apply alone does not establish current storage readiness.

`cluster info` prints the cluster's release, FSID, frozen image digest, monitors, admin node and its Ceph command (`cephCommand`), the line that runs `ceph` on the admin node with the administrator configuration and keyring that apply keeps there. It reads local evidence and contacts no host.

```sh
./bin/bootwright cluster info --name ceph-lab
```

Run each check below as that printed command followed by the arguments shown, written here as `<cephCommand>`. Replace its leading `bootwright` with `./bin/bootwright` when running from this checkout. Keep its printed context, node, configuration and keyring arguments. Only read the files under `/var/lib/bootwright/ceph/<fsid>/`; changing their contents or metadata breaks custody evidence. Expect `HEALTH_OK` with three MONs, an active and a standby MGR, and three up and in OSDs:

```text
<cephCommand> -s
<cephCommand> health detail
<cephCommand> fsid
<cephCommand> versions
<cephCommand> mon stat
<cephCommand> quorum_status --format json-pretty
<cephCommand> mgr stat
<cephCommand> mgr services
<cephCommand> mgr module ls
<cephCommand> orch status
<cephCommand> orch host ls
<cephCommand> orch ls --refresh
<cephCommand> orch ps --refresh
<cephCommand> orch ps --refresh --format json-pretty
<cephCommand> osd stat
<cephCommand> osd tree
<cephCommand> pg stat
<cephCommand> osd pool ls detail
<cephCommand> osd pool get rbd-data all
<cephCommand> osd pool application get rbd-data
<cephCommand> osd pool stats rbd-data
<cephCommand> df detail
<cephCommand> config get global osd_pool_default_size
<cephCommand> config get global osd_pool_default_min_size
```

Use the [native monitoring commands](https://docs.ceph.com/en/tentacle/rados/operations/monitoring/) and [orchestrator service status](https://docs.ceph.com/en/tentacle/cephadm/services/) to check every declared storage component:

| Component | Required result |
| --- | --- |
| Cluster identity and release | Live `fsid` matches `cluster info`; `versions` reports Ceph 20.2.2 for all MON, MGR and OSD daemons. Each daemon's `container_image_digests` in the JSON `orch ps` output includes the frozen registry digest reported by `cluster info`; a container image ID is a different identifier. |
| MON | Exactly three expected monitors, all in quorum, on `rhel-01`, `rhel-02` and `rhel-03` (native names may include the domain). |
| MGR and orchestrator | One active and one standby MGR, both running; orchestrator backend `cephadm` is available and not paused. |
| Hosts and placement | All three declared FQDNs have label `ceph-core`, no offline or maintenance status, and the intended MON/MGR/OSD services reach their requested running counts: 3/2/3. Use `--refresh` to avoid relying on cached daemon state. |
| OSD and disks | Exactly three OSDs are both up and in, with weight greater than zero, SSD class and one OSD under each declared host. Match each OSD to its guest and data device with `osd metadata` below. |
| Placement groups | A positive PG count; every PG is `active+clean`. No degraded, undersized, inactive, peering, misplaced or recovering PGs remain. |
| RBD pool | `rbd-data` is replicated, `size=2`, `min_size=2`, `pg_autoscale_mode=on`, has the `rbd` application and a positive PG count. `df detail` shows available capacity; no near-full/full condition remains. Global new-pool defaults are also 2/2. |
| IBM Call Home | `mgr module ls` shows `call_home_agent` enabled, as declared. Inspect any Call Home health warning separately. |
| Dashboard | `dashboard` module enabled, HTTPS URL published by `mgr services`, certificate verified and login successful as described below. |
| Pool client | `client.rbd-canary` has the exact pool-scoped capabilities below; the optional client test proves authenticated RBD access. |

For each numeric OSD ID from `osd tree`, replace `<id>` and check the actual device and running configuration:

```text
<cephCommand> osd metadata <id> --format json-pretty
<cephCommand> config show osd.<id> osd_memory_target_autotune
<cephCommand> config show osd.<id> osd_memory_target
```

Require the declared host and SSD device identity, `false` for autotuning and `2147483648` for the memory target on every OSD. The target is not a process memory cap.

For a strict health check with an exit status, pipe only the JSON health document to a local parser:

```text
<cephCommand> health --format json | python3 -c 'import json, sys; h = json.load(sys.stdin); print(h["status"], ",".join(sorted(h.get("checks", {})))); sys.exit(0 if h["status"] == "HEALTH_OK" else 1)'
```

This check returns 0 only for `HEALTH_OK`. Bootwright can complete with the isolated `CHA_ERROR_SENDING_REPORT` warning when Call Home egress fails; that still fails this strict check and needs an explicit operator decision. Other warnings are not accepted. Recheck health and PGs after each data test and after recovery. If a command fails, a host is unreachable or counts disagree, readiness is unproved; inspect the named component rather than treating partial output as success.

CephFS, object gateways, NFS, NVMe-oF, iSCSI and the monitoring stack are not declared by this example. Their presence is not an expected readiness result.

### Pool client

Inspect the declared client's capabilities without printing its key. Keep the parser in the pipeline; raw `auth get` output is confidential:

```text
<cephCommand> auth get client.rbd-canary --format json | python3 -c 'import json, sys; rows = json.load(sys.stdin); expected = {"mon": "profile rbd", "mgr": "profile rbd pool=rbd-data", "osd": "profile rbd pool=rbd-data"}; ok = len(rows) == 1 and rows[0].get("entity") == "client.rbd-canary" and rows[0].get("caps") == expected; print("pool client caps:", "OK" if ok else "FAIL"); sys.exit(0 if ok else 1)'
```

Capability equality proves the configured scope. The following optional test exercises the client itself through native tools. It exports the existing credential only to a root-private temporary keyring on the admin guest, writes and compares 1 MiB, then grows the new retained image `rbd-data/client-canary` to 16 MiB. It neither changes the principal nor reads Bootwright's private custody files. Run it once; replace `rhel-01` if the printed admin node differs, and replace `<conf>` and `<keyring>` with the administrator paths from `cephCommand`:

```text
./bin/bootwright cluster exec --name ceph-lab --node rhel-01 -- sudo -n bash -s -- '<conf>' '<keyring>' <<'CHECK'
set -euo pipefail
umask 077
conf=$1
admin_keyring=$2
check_dir=$(mktemp -d /var/tmp/ceph-client-check.XXXXXXXX)
printf 'Scratch directory on admin guest: %s\n' "$check_dir"
trap 'rm -f -- "$check_dir/client.keyring" "$check_dir/input" "$check_dir/output"; rmdir -- "$check_dir"' EXIT
ceph --conf "$conf" --keyring "$admin_keyring" auth get client.rbd-canary -o "$check_dir/client.keyring" >/dev/null
rbd_client=(rbd --conf "$conf" --id rbd-canary --keyring "$check_dir/client.keyring")
"${rbd_client[@]}" ls --pool rbd-data --format json | python3 -c 'import json, sys; sys.exit("client-canary" in json.load(sys.stdin))'
head -c 1048576 /dev/urandom > "$check_dir/input"
"${rbd_client[@]}" import "$check_dir/input" rbd-data/client-canary
"${rbd_client[@]}" export rbd-data/client-canary "$check_dir/output"
cmp -n 1048576 "$check_dir/output" "$check_dir/input"
"${rbd_client[@]}" resize --size 16M rbd-data/client-canary
"${rbd_client[@]}" info rbd-data/client-canary
CHECK
```

Require exit 0 and a 16 MiB image. An existing image or a failed listing stops the script before writing. A lost response after import is an unknown outcome: inspect the retained image instead of repeating the script. Temporary input and keyring are removed on normal exit; after a killed session, remove only the exact printed guest scratch directory after confirming the session ended. The retained image is removed with the owned disks during destroy. This native operator check adds no Bootwright credential-export API or release-acceptance claim.

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

To prove current operation, open the active URL from the controller, verify its certificate fingerprint, log in and confirm that the dashboard shows this FSID, all three hosts, three OSDs up/in and the `rbd-data` pool. A stored dashboard status or a listening TCP port alone does not prove a current TLS connection and working login. Keep passwords out of shell arguments, screenshots and saved validation output.

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
