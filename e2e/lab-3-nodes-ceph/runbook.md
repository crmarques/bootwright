# lab-3-nodes-ceph

IBM Storage Ceph on three RHEL 9.8 guests, `rhel-01` to `rhel-03` at
`198.51.100.11` to `198.51.100.13`, each 4 vCPU and 8 GiB with a 40 GiB root
and one 30 GiB virtual SSD for its OSD, beside managed DNS, NTP, proxy and
artifact services on the controller. The cluster `ceph-e2e` carries every
service group by default: block (RBD), file (CephFS), object (RGW behind a TLS
ingress at `198.51.100.60`), NFS, SMB and observability (management gateway at
`198.51.100.62:9443`). NVMe-oF is off because the host lacks AVX-512. Each
group is a switch in [values.yaml](values.yaml). Context `lab-3-nodes-ceph`;
3 guests, 12 vCPU, 24 GiB.

It accepts [B626](../../specs/milestones/m2.md#b626): one run per IBM Storage
Ceph release set in `ceph.*`, each recorded as a row of the
[acceptance ledger](../../docs/acceptance.md). It rehearses
[B81](../../specs/milestones/m2.md#b81) (pools, placement and RBD),
[B492](../../specs/milestones/m2.md#b492) (CephFS),
[B477](../../specs/milestones/m2.md#b477) (object),
[B478](../../specs/milestones/m2.md#b478) (NFS),
[B489](../../specs/milestones/m2.md#b489) (scoped client access) and
[B686](../../specs/milestones/m2.md#b686) (crash collection) on one cluster.
Their live rows are the profile rows of [B484](../../specs/milestones/m2.md#b484),
so a run here claims none of them. The SMB, observability and NVMe-oF groups
are exercised without claiming their items: their rows are B484's P2, P3 and
P4.

## Inputs

| `--set` | Default | Effect |
| --- | --- | --- |
| `ceph.services.block=false` | `true` | no RBD pool, placement policy, image or `rbd-canary` |
| `ceph.services.file=false` | `true` | no CephFS, subvolumes or `cephfs-canary`; NFS and SMB then render nothing |
| `ceph.services.object=false` | `true` | no RGW, ingress, gateway, user or bucket |
| `ceph.services.nfs=false` | `true` | no NFS service or export (needs `file`) |
| `ceph.services.smb=false` | `true` | no SMB cluster, users or share (needs `file`) |
| `ceph.services.observability=false` | `true` | no monitoring stack or management gateway |
| `ceph.services.nvmeof=true` | `false` | NVMe-oF gateways, pools, image and subsystem; adds `nvme-cli` to the guests |
| `guest.osdDisks`, `guest.osdDiskGiB` | `1`, `30` | OSD disks per guest (at most 7) |
| `guest.memoryMiB`, `guest.cpu` | `8192`, `4` | guest size; raise the memory or switch groups off if OSDs flap |

You set `ibm-registry` and, with SMB, `files-smb-user`; Bootwright generates
`ceph-cluster-key`, `rgw-objects-tls` (object), `grafana-admin` and
`management-tls` (observability). The addresses, upstreams, media names and
the OSD memory target are in [values.yaml](values.yaml).

### Releases (B626)

A release is chosen by you, never by Bootwright: these fields move together
and the native tools judge installability.

| Release | `ceph.packageVersion` | `ceph.callHome` | `ceph.image` |
| --- | --- | --- | --- |
| 9.9.2.0 (default) | `20.2.2-349.el9cp` | `enabled` | `cp.icr.io/cp/ibm-ceph/ceph-9-rhel10`, `v9.9.2-26947` |
| 9.9.1.2 | `20.2.1-405.el9cp` | `enabled` | `cp.icr.io/cp/ibm-ceph/ceph-9-rhel9`, `v9.9.1-25074`; IBM later lists `v9.9.1-27200` |
| 9.9.0.3 | `20.1.0-221` with the dist suffix of IBM's repository | `disabled` (opt-in Call Home) | not recorded; take the build tag your entitlement holds |

Also set `ceph.ansiblePackageVersion` to the full `cephadm-ansible` release the
repository publishes (`5.0.3-1.el9cp` for 9.9.2.0) and `ceph.images.*` to the
digests of the same build: the values carry those of `v9.9.2-26947`
([D171](../../specs/milestones/backlog.md#d171)), resolved with `skopeo
inspect`, and the other releases have none recorded. Confirm each repository
and tag pair with authenticated registry metadata. A wrong `callHome` refuses
`call-home-unqualified` after the hosts are prepared, and then needs a destroy
and a fresh apply.

## Prepare

The [host preparation](../README.md#prepare-the-host), the RHEL 9.8 boot and
DVD ISOs added as media, and an IBM entitlement key for `cp.icr.io`. The
rendered Entitlement carries `license.accept: true`; accepting the IBM license
terms is your act. With every group on, 8 GiB guests are tight; leave the
host and its services about 4 GiB beyond the gate below. Free ports as in the
host preparation, and `198.51.100.60` and `198.51.100.62` unused. Needed on
the controller host: `curl`, `dig`, `python3` and, with object on, the AWS CLI.
For NVMe-oF, `grep -c avx512f /proc/cpuinfo` must be nonzero. The
[capacity gate](../../specs/substrates.md#capacity-admission) counts the three
guests and the provider reserve (1 vCPU, 2 GiB, 10 GiB): 13 host CPUs, 26 GiB
of available memory and, with one OSD disk per guest, 220 GiB free on the
filesystem that holds libvirt's guest disks (each further OSD disk adds 3
times `guest.osdDiskGiB`).

## Run

```sh
e2e/render.sh lab-3-nodes-ceph      # add --set switches, one release per run
C=lab-3-nodes-ceph; IN=~/bootwright-e2e/lab-3-nodes-ceph
A=$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')   # the host address
./bin/bootwright validate -f "$IN"
./bin/bootwright context init --name "$C" --input-dir "$IN"
./bin/bootwright secret generate --context "$C"
./bin/bootwright secret set --context "$C" --name ibm-registry --username cp --password-stdin --yes
./bin/bootwright secret set --context "$C" --name files-smb-user --value-stdin   # smb
./bin/bootwright secret check --context "$C"
./bin/bootwright plan --context "$C"
```

Set every `contextStore` Secret before the first apply: it freezes them. Apply
by stage and check each layer before the next:

```sh
./bin/bootwright apply --context "$C" --stage controller --yes
./bin/bootwright preflight controller --context "$C"
./bin/bootwright apply --context "$C" --stage infrastructure-services,substrates --yes
```

The services answer on the host address `A`:

```sh
curl -sk "https://$A:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
dig @"$A" +short controller.e2e.example.test
dig @"$A" +tcp +short rhel-01.e2e.example.test
chronyd -Q -t 10 "server $A iburst port 123"
```

```sh
./bin/bootwright apply --context "$C" --stage machines --yes
./bin/bootwright machine list --context "$C" --power-status
./bin/bootwright machine exec --context "$C" --name rhel-01,rhel-02,rhel-03 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright machine exec --context "$C" --name rhel-01,rhel-02,rhel-03 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright preflight storage-cluster --context "$C"
```

The IBM hosts must resolve on every guest, and each OSD disk is nonrotational
(`ROTA=0`) at `/dev/sda`, with the root at `/dev/vda`: an unreachable
forwarder makes the clusters stage time out after live effects began. Repeat
the lookup after any resolver or network change. Then realize the cluster, one
full image download per guest. The first clusters-stage apply is also the
interruption drill (B492, B626): press Ctrl-C once while a pool or the
filesystem block is running, then inspect and continue:

```sh
./bin/bootwright plan --context "$C" --stage clusters
./bin/bootwright apply --context "$C" --stage clusters --yes      # Ctrl-C once during a pool or filesystem block
./bin/bootwright status --context "$C"
./bin/bootwright apply --context "$C" --stage clusters --yes
./bin/bootwright status --context "$C"
```

The operation ID is unchanged, completed attempts are retained, and no pool,
image, filesystem, group or subvolume is created twice. Keep the input,
Secrets and media unchanged meanwhile. If the apply finished before the
interrupt landed, the drill is not run: record that and repeat it on a fresh
context.

For B626, the 20.2.x bootstrap license call is continued in the same apply. If
an apply still ends at that bootstrap refusal, apply again: the next one
continues the seed. Any other bootstrap failure is a real failure.

## Verify

```sh
./bin/bootwright status --context "$C"
./bin/bootwright diff --context "$C" --observe
./bin/bootwright cluster list --context "$C"
./bin/bootwright cluster info --context "$C" --name ceph-e2e
./bin/bootwright apply --context "$C" --yes      # settles from records, no work
```

`cluster info` prints the release, FSID, frozen image digest, monitors, the
dashboard and the admin command `cephCommand`. Below, `<cephCommand>` is that
command, run as `./bin/bootwright cluster exec --context "$C" --name ceph-e2e
--node rhel-01 -- sudo -n <ceph command>` with its printed configuration and
keyring arguments. Read only the files under `/var/lib/bootwright/ceph/<fsid>/`.
`<conf>` and `<keyring>` below are the two administrator paths it prints. Never
print a raw key: every script below keeps keys in a root-private scratch
directory, and `auth get` and `cluster keyring` output is never shown.

| Check | Required result |
| --- | --- |
| core | `<cephCommand> -s` is `HEALTH_OK`, or only `CHA_ERROR_SENDING_REPORT` while Call Home cannot reach IBM, with 3 mons, an active and a standby mgr, and every OSD (3 times `guest.osdDisks`) up and in; `pg stat` all `active+clean`; `versions` reports the release's Ceph for every daemon; `orch ps --refresh` daemon digests include the frozen digest of `cluster info` |
| B626 | `mgr module ls` shows `call_home_agent` enabled for 20.2.x builds and not enabled for 20.1.0-221; `cluster info` shows no refusal and any specialization line; the apply log shows no second apply was needed, or names the one that continued the seed |
| B686 | `orch ps --daemon_type crash` shows one running crash daemon on each of the three hosts |

### Block: B81, B489

```text
<cephCommand> osd crush rule dump ssd-hosts
<cephCommand> osd pool ls detail
<cephCommand> osd pool application get rbd-data
```

Require rule `ssd-hosts` over root `default`, class `ssd`, host failure
domain; `rbd-data` replicated `size=2`, `min_size=2`, autoscale on, the `rbd`
application. Read the image:

```text
./bin/bootwright cluster exec --context "$C" --name ceph-e2e --node rhel-01 -- sudo -n rbd --conf <conf> --keyring <keyring> info rbd-data/rbd-disk
```

Require 1 GiB, format 2, features `layering`, `exclusive-lock`, `object-map`,
`fast-diff`, `deep-flatten`.

Pool client. The capability check never prints the key:

```text
<cephCommand> auth get client.rbd-canary --format json | python3 -c 'import json, sys; rows = json.load(sys.stdin); expected = {"mon": "profile rbd", "mgr": "profile rbd pool=rbd-data", "osd": "profile rbd pool=rbd-data"}; ok = len(rows) == 1 and rows[0].get("entity") == "client.rbd-canary" and rows[0].get("caps") == expected; print("pool client caps:", "OK" if ok else "FAIL"); sys.exit(0 if ok else 1)'
```

The data test runs as `--id rbd-canary` with its own keyring, copied to a
root-private scratch file on the admin guest, writes and compares 1 MiB, grows
a new retained image to 16 MiB and checks that another pool is refused. Run it
once; destroy removes the image:

```text
./bin/bootwright cluster exec --context "$C" --name ceph-e2e --node rhel-01 -- sudo -n bash -s -- '<conf>' '<keyring>' <<'CHECK'
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
if "${rbd_client[@]}" ls --pool .mgr >/dev/null 2>&1; then echo 'the client listed a pool outside its scope'; exit 1; fi
CHECK
```

Require exit 0 and a 16 MiB image. An existing image stops the script before
writing; a lost response after the import is an unknown outcome, so inspect the
image rather than repeating. After a killed session remove only the printed
scratch directory. The custody handoff is
`./bin/bootwright cluster keyring --context "$C" --name ceph-e2e --client rbd-canary | wc -c`,
which prints a byte count only.

### File: B492, B489

```text
<cephCommand> fs get files
<cephCommand> fs status files
<cephCommand> fs subvolumegroup info files apps
<cephCommand> fs subvolume info files docs --group_name apps
```

Require `max_mds 1`, `standby_count_wanted 1`, `cephfs-metadata` and
`cephfs-data` replicated 2/2 with the `cephfs` application, rank 0 `active`
plus one standby, group `apps` mode `750`, `docs` with a 1073741824-byte quota,
mode `750` and a path under `/volumes/apps/`. The scoped client mounts only its
own subvolume, writes and compares 1 MiB, and is refused on another subvolume:

```text
./bin/bootwright cluster exec --context "$C" --name ceph-e2e --node rhel-01 -- sudo -n bash -s -- '<conf>' '<keyring>' <<'CHECK'
set -euo pipefail
umask 077
conf=$1
keyring=$2
ceph_admin=(ceph --conf "$conf" --keyring "$keyring")
check_dir=$(mktemp -d /var/tmp/cephfs-check.XXXXXXXX)
printf 'Scratch directory on admin guest: %s\n' "$check_dir"
mkdir "$check_dir/mnt" "$check_dir/other"
trap 'umount "$check_dir/mnt" 2>/dev/null || true; umount "$check_dir/other" 2>/dev/null || true; rm -f -- "$check_dir/secret" "$check_dir/input"; rmdir -- "$check_dir/mnt" "$check_dir/other" "$check_dir"' EXIT
"${ceph_admin[@]}" auth get-key client.cephfs-canary -o "$check_dir/secret" >/dev/null
own=$("${ceph_admin[@]}" fs subvolume getpath files cephfs-canary --group_name apps)
docs=$("${ceph_admin[@]}" fs subvolume getpath files docs --group_name apps)
opts="name=cephfs-canary,secretfile=$check_dir/secret,fs=files,conf=$conf"
mount -t ceph ":$own" "$check_dir/mnt" -o "$opts"
head -c 1048576 /dev/urandom > "$check_dir/input"
cp "$check_dir/input" "$check_dir/mnt/canary"
sync
cmp "$check_dir/mnt/canary" "$check_dir/input"
rm -f -- "$check_dir/mnt/canary"
if mount -t ceph ":$docs" "$check_dir/other" -o "$opts" && cp "$check_dir/input" "$check_dir/other/canary"; then echo 'the client wrote outside its subvolume'; exit 1; fi
CHECK
```

Require exit 0. The same script with `-o name=admin` and the admin key proves
the administrator mount.

### Object: B477, B489

```text
<cephCommand> orch ls rgw --refresh
<cephCommand> orch ps --daemon_type rgw --refresh
<cephCommand> osd pool ls detail
```

Require two running `rgw.objects` daemons and the eight `objects.rgw.*` pools
with the `rgw` application beside `.rgw.root`. The endpoint and the VIP have
no DNS record. Keep the keys in files, never on the command line:

```sh
umask 077; D=$(mktemp -d); VIP=198.51.100.60
./bin/bootwright secret show --context "$C" --name rgw-objects-tls --part certificate > "$D/ca.pem"
curl -sS -i --cacert "$D/ca.pem" --connect-to s3.e2e.example.test:443:$VIP:443 https://s3.e2e.example.test/
curl -sS -i --cacert "$D/ca.pem" --connect-to wrong.e2e.example.test:443:$VIP:443 https://wrong.e2e.example.test/   # must fail the name check
./bin/bootwright cluster info --context "$C" --name ceph-e2e --object-user s3-canary --output json > "$D/access.json"
python3 -I - "$D" <<'PY'
import json, sys
d = sys.argv[1]
def find(o):
    if isinstance(o, dict):
        if "objectUsers" in o:
            return o["objectUsers"]
        o = list(o.values())
    for v in o if isinstance(o, list) else []:
        r = find(v)
        if r:
            return r
u = find(json.load(open(d + "/access.json")))[0]
open(d + "/credentials", "w").write("[default]\naws_access_key_id = %s\naws_secret_access_key = %s\n" % (u["accessKey"], u["secretKey"]))
PY
export AWS_SHARED_CREDENTIALS_FILE="$D/credentials" AWS_CA_BUNDLE="$D/ca.pem" AWS_DEFAULT_REGION=objects
head -c 1048576 /dev/urandom > "$D/in"
aws --endpoint-url https://$VIP s3api put-object --bucket s3-canary-bucket --key canary --body "$D/in"
aws --endpoint-url https://$VIP s3api list-objects-v2 --bucket s3-canary-bucket --query 'Contents[].Key'
aws --endpoint-url https://$VIP s3api get-object --bucket s3-canary-bucket --key canary "$D/out" && cmp "$D/in" "$D/out"
aws --endpoint-url https://$VIP s3api delete-object --bucket s3-canary-bucket --key canary
```

Require the status line of each request through the endpoint name, the
wrong-name request refused, and the put, list, get and delete to succeed. The
S3 keys are the only credential `cluster info --object-user` reveals; remove
`$D` when done. Failover is an acceptance drill.

### NFS: B478

```text
<cephCommand> orch ps --daemon_type nfs
<cephCommand> nfs export info shares /docs
```

Require one running `nfs.shares` daemon and an export at `/docs` under
`/volumes/apps/docs/` with `access_type` `NONE`, protocols `[4]`, TCP, and the
two client entries in declared order: deny `198.51.100.13`, then read-write
for the subnet. Write the daemon's host from `orch ps` as `<nfsHost>`. The
allowed client `rhel-02` mounts, and writes and reads as the subvolume owner
(uid and gid 2000), because every entry squashes root:

```text
./bin/bootwright cluster exec --context "$C" --name ceph-e2e --node rhel-02 -- sudo -n bash -s -- '<nfsHost>' <<'CHECK'
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

The denied client `rhel-03` must fail with `access denied by server`:

```text
./bin/bootwright cluster exec --context "$C" --name ceph-e2e --node rhel-03 -- sudo -n bash -s -- '<nfsHost>' <<'CHECK'
check_dir=$(mktemp -d)
trap 'umount "$check_dir" 2>/dev/null || true; rmdir -- "$check_dir"' EXIT
if mount -t nfs4 -o proto=tcp "$1:/docs" "$check_dir"; then echo 'the denied client mounted /docs'; exit 1; fi
CHECK
```

This case declares no NFS ingress, so ingress failover is not exercised.

### SMB

```text
<cephCommand> smb show
```

The password is read from the Secret you set, never from an argument. The
share takes a put, get and delete as `smbuser` through each of the two hosts,
and refuses a wrong user:

```text
umask 077; pw=$(mktemp)
./bin/bootwright secret show --context "$C" --name files-smb-user --part value > "$pw"
./bin/bootwright cluster exec --context "$C" --name ceph-e2e --node rhel-01 -- sudo -n bash -c 'read -r PASSWD; export PASSWD; cd "$(mktemp -d)"; head -c 1048576 /dev/urandom > f; for h in rhel-02 rhel-03; do smbclient //$h.e2e.example.test/documents -U smbuser -c "put f; get f g; del f" && cmp f g && rm -f g; done; PASSWD=wrong smbclient //rhel-02.e2e.example.test/documents -U nobody -c ls && exit 1; exit 0' < "$pw"
rm -f "$pw"
```

Require exit 0, with `NT_STATUS_LOGON_FAILURE` for the wrong user.

### Observability

```text
<cephCommand> orch ls
<cephCommand> orch ps --daemon_type prometheus
```

Require prometheus, alertmanager, grafana, node-exporter, ceph-exporter,
mgmt-gateway, ingress `mgmt`, loki and alloy running. From the host
`curl -k https://198.51.100.62:9443` answers through the VIP. On `rhel-03`
request `/api/v1/targets` of the Prometheus daemon (port 9095, at the scheme
`orch ps` prints) and require every target `up`, and `/loki/api/v1/labels` on
port 3100 to list labels from daemon logs. The Grafana password is the
`grafana-admin` Secret.

### NVMe-oF

With `ceph.services.nvmeof=true`. From `rhel-01`, with the kernel module loaded:

```sh
./bin/bootwright machine exec --context "$C" --name rhel-01 -- sudo modprobe nvme-tcp
./bin/bootwright machine exec --context "$C" --name rhel-01 -- sudo nvme discover -t tcp -a 198.51.100.12 -s 4420
./bin/bootwright machine exec --context "$C" --name rhel-01 -- sudo nvme connect -t tcp -a 198.51.100.12 -s 4420 -n nqn.2026-01.test.example:application --hostnqn nqn.2026-01.test.example:initiator
```

Write, read and compare 1 MiB on the new namespace, then `nvme disconnect`. A
host NQN other than `initiator` must be refused.

## Acceptance drills

### MDS failover (B492)

Fail rank 0 while a client holds the mount and record the seconds the standby
takes to become active; the data stays readable and writable:

```text
./bin/bootwright cluster exec --context "$C" --name ceph-e2e --node rhel-01 -- sudo -n bash -s -- '<conf>' '<keyring>' <<'CHECK'
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
start=$(date +%s)
"${ceph_admin[@]}" mds fail files:0
now=
for _ in $(seq 1 300); do
  now=$(rank0)
  if [ -n "$now" ] && [ "$now" != "$before" ]; then break; fi
  sleep 1
done
printf 'Active rank 0 changed in %s s\n' "$(( $(date +%s) - start ))"
[ -n "$now" ] && [ "$now" != "$before" ]
cmp "$check_dir/mnt/before" "$check_dir/input"
cp "$check_dir/input" "$check_dir/mnt/after"
sync
cmp "$check_dir/mnt/after" "$check_dir/input"
rm -f -- "$check_dir/mnt/before" "$check_dir/mnt/after"
CHECK
```

Then repeat `<cephCommand> fs status files` until rank 0 is `active` with one
standby again; no apply is needed.

### Object failover (B477)

Stop one RGW daemon, not its guest: with 2/2 pools a stopped guest pauses I/O on
every placement group with a replica there. The other host must keep answering
through the endpoint name and the VIP:

```text
<cephCommand> orch daemon stop <rgwDaemon>
<cephCommand> orch ps --daemon_type rgw --refresh
<cephCommand> orch daemon start <rgwDaemon>
```

For the ingress, find the host that holds `198.51.100.60`, stop that host's
`keepalived` daemon with `orch daemon stop` (names from
`orch ps --service_name ingress.rgw.objects`), require the VIP on the other
host and the S3 get to succeed through it, then start the daemon:

```sh
./bin/bootwright machine exec --context "$C" --name rhel-02,rhel-03 -- ip -4 -o addr show
```

### One-host outage

The checks run on `rhel-01`, so confirm `rhel-03` is not the admin node, then:

```sh
./bin/bootwright machine stop --context "$C" --name rhel-03 --yes
./bin/bootwright machine list --context "$C" --power-status
```

Run `<cephCommand> -s`, `health detail`, `mon stat` and `osd tree`: health
leaves `HEALTH_OK`, one mon is out of quorum and the OSDs of `rhel-03` are
down. With 2/2 pools, I/O to placement groups with a replica there pauses until
the host returns; do not lower `min_size`. Then:

```sh
./bin/bootwright machine start --context "$C" --name rhel-03
./bin/bootwright machine exec --context "$C" --name rhel-03 -- uptime      # repeat until SSH answers
```

Repeat the checks until `HEALTH_OK` returns with 3 mons in quorum and every OSD
up and in. Recovery needs no apply.

### Replay

`apply --context "$C" --yes` on the finished context settles without work, and
`diff --context "$C" --observe` shows no difference.

## Destroy

Ceph retires with its three stopped hosts, and removal while a host survives is
not supported. Destroy refuses while the guests run and names the stop:

```sh
./bin/bootwright plan --context "$C"
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright machine stop --context "$C" --name rhel-01,rhel-02,rhel-03 --yes
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright status --context "$C"
./bin/bootwright cluster info --context "$C" --name ceph-e2e
virsh --connect qemu:///system list --all
virsh --connect qemu:///system net-list --all
./bin/bootwright context delete --name "$C" --purge --yes
```

Require no domain, disk or network of the context to remain. After an
interrupted destroy, repeat `destroy`, never `apply`. For the next release of
B626, render again with its `ceph.*` values and start from `context init`.
Media and the host packages stay. Remove the rendered directory when done.
