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

Run every numbered step in order in one Bash session from the checkout under
test. A successful run includes all checks for the selected inputs, the
required drills and cleanup. An unexpected failure fails the run; retain its
private records for cleanup. Run each other input variant from the beginning.

## 1. Choose the inputs

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
and tag pair with authenticated registry metadata before the run.

```sh
# Edit only these assignments; service defaults match values.yaml.
BLOCK_ENABLED="true"
FILE_ENABLED="true"
OBJECT_ENABLED="true"
NFS_ENABLED="true"
SMB_ENABLED="true"
OBSERVABILITY_ENABLED="true"
NVMEOF_ENABLED="false" # Enable only after the AVX-512 prerequisite passes.
VALUES_FILE='' # Optional private YAML file with all release coordinates and image digests.
SMB_PASSWORD_FILE='' # Existing mode-0600 file containing the SMB password, needed only with file and smb.
IBM_REGISTRY_FILE="$HOME/ibm-ceph-registry.txt"
```

## 2. Prepare the host

Complete [host preparation](../README.md#prepare-the-host) now, including the RHEL 9.8 boot and
DVD ISOs added as media, and the IBM entitlement key
[secret file](../README.md#prepare-the-host) for `cp.icr.io`. The
rendered Entitlement carries `license.accept: true`; accepting the IBM license
terms is your act. With every group on, 8 GiB guests are tight; leave the
host and its services about 4 GiB beyond the gate below. Free ports as in the
host preparation, and `198.51.100.60` and `198.51.100.62` unused. The shared
preparation supplies `curl`, `dig` and `chronyd`. Also require
`python3` and, with object enabled, the AWS CLI from the host
administrator's approved packages:

```sh
command -v python3
if [ "$OBJECT_ENABLED" = true ]; then command -v aws; fi
```

For NVMe-oF, `grep -c avx512f /proc/cpuinfo` must be nonzero. The
[capacity gate](../../specs/substrates.md#capacity-admission) counts the three
guests and the provider reserve (1 vCPU, 2 GiB, 10 GiB): 13 host CPUs, 26 GiB
of available memory and, with one OSD disk per guest, 220 GiB free on the
filesystem that holds libvirt's guest disks (each further OSD disk adds 3
times `guest.osdDiskGiB`).

<a id="run"></a>

## 3. Create and apply the context

```bash
RENDER_ARGS=()
if [ -n "$VALUES_FILE" ]; then
    RENDER_ARGS+=(--values "$VALUES_FILE")
fi
e2e/render.sh lab-3-nodes-ceph "${RENDER_ARGS[@]}" \
    --set "ceph.services.block=$BLOCK_ENABLED" \
    --set "ceph.services.file=$FILE_ENABLED" \
    --set "ceph.services.object=$OBJECT_ENABLED" \
    --set "ceph.services.nfs=$NFS_ENABLED" \
    --set "ceph.services.smb=$SMB_ENABLED" \
    --set "ceph.services.observability=$OBSERVABILITY_ENABLED" \
    --set "ceph.services.nvmeof=$NVMEOF_ENABLED"
CONTEXT="lab-3-nodes-ceph"; INPUT_DIR="$HOME/bootwright-e2e/lab-3-nodes-ceph"
CONTROLLER_ADDRESS=$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')   # the host address
umask 077
RUN_DIR=$(mktemp -d) # Private records retained through final cleanup.
./bin/bootwright validate -f "$INPUT_DIR"
./bin/bootwright context init --name "$CONTEXT" --input-dir "$INPUT_DIR"
./bin/bootwright secret generate --context "$CONTEXT"
install -m 0600 /dev/null "$RUN_DIR/ibm-password"
cut -d: -f2- "${IBM_REGISTRY_FILE:?Set IBM_REGISTRY_FILE to the IBM entitlement key file}" > "$RUN_DIR/ibm-password"
./bin/bootwright secret set --context "$CONTEXT" --name ibm-registry --username cp --password-file "$RUN_DIR/ibm-password" --yes
rm -f "$RUN_DIR/ibm-password"
if [ "$FILE_ENABLED" = "true" ] && [ "$SMB_ENABLED" = "true" ]; then
    ./bin/bootwright secret set --context "$CONTEXT" --name files-smb-user --value-file "${SMB_PASSWORD_FILE:?Set SMB_PASSWORD_FILE to the protected SMB password file}" --yes
fi
./bin/bootwright secret check --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT"
```

Set every `contextStore` Secret before the first apply: it freezes them. Apply
by stage and check each layer before the next:

```sh
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
```

Set the listener ports, then complete
[Prepare listener firewall](../README.md#prepare-listener-firewall):

```bash
FIREWALL_PORTS=(53/tcp 53/udp 123/udp 3128/tcp 8080/tcp 8443/tcp)
```

Require controller preflight to report `ready` before starting services:

```sh
./bin/bootwright preflight controller --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services,substrates --yes
```

The services answer on the host address `CONTROLLER_ADDRESS`:

```sh
curl -sk "https://$CONTROLLER_ADDRESS:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
dig @"$CONTROLLER_ADDRESS" +short controller.e2e.example.test
dig @"$CONTROLLER_ADDRESS" +tcp +short rhel-01.e2e.example.test
chronyd -Q -t 10 "server $CONTROLLER_ADDRESS iburst port 123"
```

```sh
./bin/bootwright apply --context "$CONTEXT" --stage machines --yes
./bin/bootwright machine list --context "$CONTEXT" --power-status
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01,rhel-02,rhel-03 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01,rhel-02,rhel-03 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright preflight storage-cluster --context "$CONTEXT"
```

The IBM hosts must resolve on every guest, and each OSD disk is nonrotational
(`ROTA=0`) at `/dev/sda`, with the root at `/dev/vda`: an unreachable
forwarder fails this check. Then realize the cluster, one
full image download per guest. The first clusters-stage apply is also the
interruption drill (B492, B626): press Ctrl-C once while a pool or the
filesystem block is running, then inspect and continue:

```sh
./bin/bootwright plan --context "$CONTEXT" --stage clusters
./bin/bootwright apply --context "$CONTEXT" --stage clusters --yes      # Ctrl-C once during a pool or filesystem block
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage clusters --yes
./bin/bootwright status --context "$CONTEXT"
```

The operation ID is unchanged, completed attempts are retained, and no pool,
image, filesystem, group or subvolume is created twice. Keep the input,
Secrets and media unchanged throughout the drill. A missed interruption
means the drill failed. The 20.2.x bootstrap license call must continue within
the same apply; a bootstrap refusal fails this run.

<a id="verify"></a>

## 4. Verify the result

Run every subsection for a service enabled in step 1. Disabled services have
no verification or failover drill in this run.

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright cluster list --context "$CONTEXT"
./bin/bootwright cluster info --context "$CONTEXT" --name ceph-e2e
./bin/bootwright apply --context "$CONTEXT" --yes      # settles from records, no work
```

`cluster info` prints the administrator configuration and keyring paths in
`cephCommand`. Copy those paths into these variables; use only the printed
files beneath the cluster's directory in `/var/lib/bootwright/ceph/`.
Never print or save raw keys; the scripts keep key material in private files.

```sh
CEPH_CONF='' # Configuration path printed by cluster info.
CEPH_KEYRING='' # Administrator keyring path printed by cluster info.
: "${CEPH_CONF:?Set CEPH_CONF from cluster info}"
: "${CEPH_KEYRING:?Set CEPH_KEYRING from cluster info}"
```

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" -s
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" pg stat
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" versions
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch ps --refresh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" mgr module ls
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch ps --daemon_type crash
```

| Check | Required result |
| --- | --- |
| core | `./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" -s` is `HEALTH_OK`, or only `CHA_ERROR_SENDING_REPORT` while Call Home cannot reach IBM, with 3 mons, an active and a standby mgr, and every OSD (3 times `guest.osdDisks`) up and in; `pg stat` all `active+clean`; `versions` reports the release's Ceph for every daemon; `orch ps --refresh` daemon digests include the frozen digest of `cluster info` |
| B626 | `mgr module ls` shows `call_home_agent` enabled for 20.2.x builds and not enabled for 20.1.0-221; `cluster info` shows no refusal and any specialization line; the apply log shows the bootstrap license call continued within the same apply |
| B686 | `orch ps --daemon_type crash` shows one running crash daemon on each of the three hosts |

### Block: B81, B489

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" osd crush rule dump ssd-hosts
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" osd pool ls detail
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" osd pool application get rbd-data
```

Require rule `ssd-hosts` over root `default`, class `ssd`, host failure
domain; `rbd-data` replicated `size=2`, `min_size=2`, autoscale on, the `rbd`
application. Read the image:

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n rbd --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" info rbd-data/rbd-disk
```

Require 1 GiB, format 2, features `layering`, `exclusive-lock`, `object-map`,
`fast-diff`, `deep-flatten`.

Pool client. The capability check never prints the key:

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" auth get client.rbd-canary --format json | python3 -c 'import json, sys; rows = json.load(sys.stdin); expected = {"mon": "profile rbd", "mgr": "profile rbd pool=rbd-data", "osd": "profile rbd pool=rbd-data"}; ok = len(rows) == 1 and rows[0].get("entity") == "client.rbd-canary" and rows[0].get("caps") == expected; print("pool client caps:", "OK" if ok else "FAIL"); sys.exit(0 if ok else 1)'
```

The data test runs as `--id rbd-canary` with its own keyring, copied to a
root-private scratch file on the admin guest, writes and compares 1 MiB, grows
a new retained image to 16 MiB and checks that another pool is refused. Run it
once; destroy removes the image:

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n bash -s -- "${CEPH_CONF:?Set CEPH_CONF as described above}" "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" <<'CHECK'
set -euo pipefail
umask 077
conf=$1
admin_keyring=$2
check_dir=$(mktemp -d /var/tmp/ceph-client-check.XXXXXXXX)
printf 'Scratch directory on admin guest: %s\n' "$check_dir"
trap 'rm -f -- "$check_dir/client.keyring" "$check_dir/input" "$check_dir/output"; rmdir -- "$check_dir"' EXIT
ceph --conf "$conf" --keyring "$admin_keyring" auth get client.rbd-canary -o "$check_dir/client.keyring" >/dev/null
rbd --conf "$conf" --id rbd-canary --keyring "$check_dir/client.keyring" ls --pool rbd-data --format json | python3 -c 'import json, sys; sys.exit("client-canary" in json.load(sys.stdin))'
head -c 1048576 /dev/urandom > "$check_dir/input"
rbd --conf "$conf" --id rbd-canary --keyring "$check_dir/client.keyring" import "$check_dir/input" rbd-data/client-canary
rbd --conf "$conf" --id rbd-canary --keyring "$check_dir/client.keyring" export rbd-data/client-canary "$check_dir/output"
cmp -n 1048576 "$check_dir/output" "$check_dir/input"
rbd --conf "$conf" --id rbd-canary --keyring "$check_dir/client.keyring" resize --size 16M rbd-data/client-canary
rbd --conf "$conf" --id rbd-canary --keyring "$check_dir/client.keyring" info rbd-data/client-canary
if rbd --conf "$conf" --id rbd-canary --keyring "$check_dir/client.keyring" ls --pool .mgr >/dev/null 2>&1; then echo 'the client listed a pool outside its scope'; exit 1; fi
CHECK
```

Require exit 0 and a 16 MiB image. An existing image stops the script before
writing. The custody handoff is
`./bin/bootwright cluster keyring --context "$CONTEXT" --name ceph-e2e --client rbd-canary | wc -c`,
which prints a byte count only.

### File: B492, B489

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" fs get files
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" fs status files
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" fs subvolumegroup info files apps
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" fs subvolume info files docs --group_name apps
```

Require `max_mds 1`, `standby_count_wanted 1`, `cephfs-metadata` and
`cephfs-data` replicated 2/2 with the `cephfs` application, rank 0 `active`
plus one standby, group `apps` mode `750`, `docs` with a 1073741824-byte quota,
mode `750` and a path under `/volumes/apps/`. The scoped client mounts only its
own subvolume, writes and compares 1 MiB, and is refused on another subvolume:

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n bash -s -- "${CEPH_CONF:?Set CEPH_CONF as described above}" "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" <<'CHECK'
set -euo pipefail
umask 077
conf=$1
keyring=$2
check_dir=$(mktemp -d /var/tmp/cephfs-check.XXXXXXXX)
printf 'Scratch directory on admin guest: %s\n' "$check_dir"
mkdir "$check_dir/mnt" "$check_dir/other"
trap 'umount "$check_dir/mnt" 2>/dev/null || true; umount "$check_dir/other" 2>/dev/null || true; rm -f -- "$check_dir/secret" "$check_dir/input"; rmdir -- "$check_dir/mnt" "$check_dir/other" "$check_dir"' EXIT
ceph --conf "$conf" --keyring "$keyring" auth get-key client.cephfs-canary -o "$check_dir/secret" >/dev/null
own=$(ceph --conf "$conf" --keyring "$keyring" fs subvolume getpath files cephfs-canary --group_name apps)
docs=$(ceph --conf "$conf" --keyring "$keyring" fs subvolume getpath files docs --group_name apps)
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

Require exit 0. The MDS failover drill below separately proves an administrator
mount with the administrator key.

### Object: B477, B489

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch ls rgw --refresh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch ps --daemon_type rgw --refresh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" osd pool ls detail
```

Require two running `rgw.objects` daemons and the eight `objects.rgw.*` pools
with the `rgw` application beside `.rgw.root`. The endpoint and the VIP have
no DNS record. Keep the keys in files, never on the command line:

```sh
umask 077; OBJECT_DIR="$RUN_DIR/object-checks"; mkdir "$OBJECT_DIR"; VIP="198.51.100.60"
./bin/bootwright secret show --context "$CONTEXT" --name rgw-objects-tls --part certificate > "$OBJECT_DIR/ca.pem"
curl -sS -i --cacert "$OBJECT_DIR/ca.pem" --connect-to "s3.e2e.example.test:443:$VIP:443" https://s3.e2e.example.test/
curl -sS -i --cacert "$OBJECT_DIR/ca.pem" --connect-to "wrong.e2e.example.test:443:$VIP:443" https://wrong.e2e.example.test/   # must fail the name check
./bin/bootwright cluster info --context "$CONTEXT" --name ceph-e2e --object-user s3-canary --output json > "$OBJECT_DIR/access.json"
python3 -I - "$OBJECT_DIR" <<'PY'
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
export AWS_SHARED_CREDENTIALS_FILE="$OBJECT_DIR/credentials" AWS_CA_BUNDLE="$OBJECT_DIR/ca.pem" AWS_DEFAULT_REGION=objects
head -c 1048576 /dev/urandom > "$OBJECT_DIR/in"
aws --endpoint-url "https://$VIP" s3api put-object --bucket s3-canary-bucket --key canary --body "$OBJECT_DIR/in"
aws --endpoint-url "https://$VIP" s3api list-objects-v2 --bucket s3-canary-bucket --query 'Contents[].Key'
aws --endpoint-url "https://$VIP" s3api get-object --bucket s3-canary-bucket --key canary "$OBJECT_DIR/out" && cmp "$OBJECT_DIR/in" "$OBJECT_DIR/out"
aws --endpoint-url "https://$VIP" s3api delete-object --bucket s3-canary-bucket --key canary
```

Require the status line of each request through the endpoint name, the
wrong-name request refused, and the put, list, get and delete to succeed. The
S3 keys are the only credential `cluster info --object-user` reveals. Keep
`$OBJECT_DIR` and its private credentials through the object failover drills below;
remove it only after those drills finish.

### NFS: B478

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch ps --daemon_type nfs
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" nfs export info shares /docs
```

Require one running `nfs.shares` daemon and an export at `/docs` under
`/volumes/apps/docs/` with `access_type` `NONE`, protocols `[4]`, TCP, and the
two client entries in declared order: deny `198.51.100.13`, then read-write
for the subnet. Write the daemon's host from `orch ps` in `NFS_HOST`. The
allowed client `rhel-02` mounts, and writes and reads as the subvolume owner
(uid and gid 2000), because every entry squashes root:

```sh
NFS_HOST='' # Host column of the running nfs.shares daemon above.
: "${NFS_HOST:?Set NFS_HOST from orch ps}"
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-02 -- sudo -n bash -s -- "${NFS_HOST:?Set NFS_HOST as described above}" <<'CHECK'
set -euo pipefail
check_dir=$(mktemp -d)
trap 'umount "$check_dir/mnt" 2>/dev/null || true; rm -f -- "$check_dir/input"; rmdir -- "$check_dir/mnt" "$check_dir"' EXIT
chmod 0711 "$check_dir"
mkdir "$check_dir/mnt"
mount -t nfs4 -o proto=tcp "$1:/docs" "$check_dir/mnt"
head -c 1048576 /dev/urandom > "$check_dir/input"
chmod 0644 "$check_dir/input"
setpriv --reuid=2000 --regid=2000 --clear-groups -- cp "$check_dir/input" "$check_dir/mnt/nfs-check"
setpriv --reuid=2000 --regid=2000 --clear-groups -- cmp "$check_dir/input" "$check_dir/mnt/nfs-check"
setpriv --reuid=2000 --regid=2000 --clear-groups -- rm -- "$check_dir/mnt/nfs-check"
CHECK
```

The denied client `rhel-03` must fail with `access denied by server`:

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-03 -- sudo -n bash -s -- "${NFS_HOST:?Set NFS_HOST as described above}" <<'CHECK'
check_dir=$(mktemp -d)
trap 'umount "$check_dir" 2>/dev/null || true; rmdir -- "$check_dir"' EXIT
if mount -t nfs4 -o proto=tcp "$1:/docs" "$check_dir"; then echo 'the denied client mounted /docs'; exit 1; fi
CHECK
```

This case declares no NFS ingress, so ingress failover is not exercised.

### SMB

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" smb show
```

The password is read from the Secret you set, never from an argument. The
share takes a put, get and delete as `smbuser` through each of the two hosts,
and refuses a wrong user:

```sh
umask 077; pw=$(mktemp)
./bin/bootwright secret show --context "$CONTEXT" --name files-smb-user --part value > "$pw"
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n bash -c 'read -r PASSWD; export PASSWD; cd "$(mktemp -d)"; head -c 1048576 /dev/urandom > f; for h in rhel-02 rhel-03; do smbclient "//$h.e2e.example.test/documents" -U smbuser -c "put f; get f g; del f" && cmp f g && rm -f g; done; PASSWD=wrong smbclient //rhel-02.e2e.example.test/documents -U nobody -c ls && exit 1; exit 0' < "$pw"
rm -f "$pw"
```

Require exit 0, with `NT_STATUS_LOGON_FAILURE` for the wrong user.

### Observability

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch ls
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch ps --daemon_type prometheus
```

Require prometheus, alertmanager, grafana, node-exporter, ceph-exporter,
mgmt-gateway, ingress `mgmt`, loki and alloy running. Inspect daemon addresses:

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch ps --refresh
curl -k https://198.51.100.62:9443
PROMETHEUS_ENDPOINT='' # Scheme and host from orch ps, including port 9095.
LOKI_ENDPOINT='' # Scheme and host of the Loki daemon from orch ps, including port 3100.
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-03 -- curl --fail --silent --show-error "${PROMETHEUS_ENDPOINT:?Set PROMETHEUS_ENDPOINT from orch ps}/api/v1/targets"
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-03 -- curl --fail --silent --show-error "${LOKI_ENDPOINT:?Set LOKI_ENDPOINT from orch ps}/loki/api/v1/labels"
```

Require every Prometheus target `up`, and Loki labels from daemon logs.
The Grafana password is the `grafana-admin` Secret.

### NVMe-oF

With `ceph.services.nvmeof=true`. From `rhel-01`, with the kernel module loaded:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01 -- sudo modprobe nvme-tcp
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01 -- sudo nvme discover -t tcp -a 198.51.100.12 -s 4420
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01 -- sudo nvme connect -t tcp -a 198.51.100.12 -s 4420 -n nqn.2026-01.test.example:application --hostnqn nqn.2026-01.test.example:initiator
```

Inspect the connected subsystem and namespaces before any I/O:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01 -- sudo -n nvme list-subsys
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01 -- sudo -n nvme list -v
```

Run the I/O drill only on the fresh, dedicated `nvme-data/nvme-disk` image
from this successfully completed apply, through its trusted, ready Bootwright
gateway. Permit no concurrent consumers, device replacement or namespace
changes from discovery through cleanup. Namespace 1 must belong to the
application NQN and be exactly 1 GiB; the entire namespace must still be zero.
Never erase existing contents to make the drill pass.

The identity checks use the kernel's namespace ID described in the
[Linux NVMe sysfs ABI](https://raw.githubusercontent.com/torvalds/linux/master/Documentation/ABI/stable/sysfs-nvme)
and the controller identity returned for a namespace block device by
[nvme id-ctrl](https://raw.githubusercontent.com/linux-nvme/nvme-cli/master/Documentation/nvme-id-ctrl.txt).
They also refuse partitions, mounts, holders and existing signatures. Select
the observed namespace device from discovery, never an inferred disk number:

```sh
NVME_DEVICE='' # Namespace block-device path from the discovery output above.
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01 -- sudo -n bash -s -- "${NVME_DEVICE:?Set NVME_DEVICE from the observed application namespace}" <<'NVME_CHECK'
set -euo pipefail
umask 077
device=$(readlink -e -- "$1")
[ -b "$device" ]
sys="/sys/class/block/${device##*/}"
[ "$(cat "$sys/nsid")" = '1' ]
nvme id-ctrl "$device" -o json | python3 -c 'import json, sys; d = json.load(sys.stdin); sys.exit(0 if d["subnqn"].strip() == "nqn.2026-01.test.example:application" else 1)'
[ "$(blockdev --getsize64 "$device")" = '1073741824' ]
lsblk -Jp -o NAME,TYPE,MOUNTPOINTS "$device" | python3 -c 'import json, sys; rows = json.load(sys.stdin)["blockdevices"]; ok = len(rows) == 1 and rows[0]["name"] == sys.argv[1] and rows[0]["type"] == "disk" and not rows[0].get("children") and all(p in (None, "") for p in (rows[0].get("mountpoints") or [])); sys.exit(0 if ok else 1)' "$device"
holders=$(find "$sys/holders" -mindepth 1 -maxdepth 1 -print -quit)
[ -z "$holders" ]
signatures=$(wipefs --no-act --noheadings --output TYPE "$device")
[ -z "$signatures" ]
cmp -n 1073741824 "$device" /dev/zero

# Every identity and non-use proof above must pass before scratch or writes.
check_dir=$(mktemp -d /var/tmp/nvme-client-check.XXXXXXXX)
write_started='false'
cleanup() {
    result=$?
    trap - EXIT
    if [ "$write_started" = 'true' ]; then
        if ! dd if=/dev/zero of="$device" bs=1048576 count=1 conv=fsync,notrunc status=none; then
            printf '%s\n' 'FAIL: restoring the initially zero first MiB failed' >&2
            result=1
        elif ! cmp -n 1048576 "$device" /dev/zero; then
            printf '%s\n' 'FAIL: restored first MiB is not zero' >&2
            result=1
        fi
    fi
    rm -f -- "$check_dir/input" "$check_dir/output"
    rmdir -- "$check_dir"
    exit "$result"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
head -c 1048576 /dev/urandom > "$check_dir/input"
write_started='true'
dd if="$check_dir/input" of="$device" bs=1048576 count=1 conv=fsync,notrunc status=none
dd if="$device" of="$check_dir/output" bs=1048576 count=1 iflag=direct status=none
cmp "$check_dir/input" "$check_dir/output"
printf '%s\n' 'Namespace first-MiB write/read comparison: OK'
NVME_CHECK
```

Require exit 0, including the cleanup that restores the initially zero first
MiB. A killed process or lost connection can prevent cleanup; preserve the
outcome as unknown and inspect it before another drill. This recipe has not
been qualified by a live run.

Disconnect the authorized subsystem before testing the denied host NQN:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01 -- sudo -n nvme disconnect -n nqn.2026-01.test.example:application
if ./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01 -- sudo -n nvme connect -t tcp -a 198.51.100.12 -s 4420 -n nqn.2026-01.test.example:application --hostnqn nqn.2026-01.test.example:denied; then
    printf '%s\n' 'FAIL: unauthorized host NQN connected' >&2
    ./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01 -- sudo -n nvme disconnect -n nqn.2026-01.test.example:application
    exit 1
fi
```

Require an explicit authorization refusal; an unrelated connection error does
not prove host authorization.

## 5. Run the acceptance drills

Run the file and object drills when those services are enabled. The one-host
outage and replay drills are required for every run.

### MDS failover (B492)

Fail rank 0 while a client holds the mount and record the seconds the standby
takes to become active; the data stays readable and writable:

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n bash -s -- "${CEPH_CONF:?Set CEPH_CONF as described above}" "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" <<'CHECK'
set -euo pipefail
umask 077
conf=$1
keyring=$2
check_dir=$(mktemp -d /var/tmp/mds-failover.XXXXXXXX)
printf 'Scratch directory on admin guest: %s\n' "$check_dir"
mkdir "$check_dir/mnt"
trap 'umount "$check_dir/mnt" 2>/dev/null || true; rm -f -- "$check_dir/secret" "$check_dir/input"; rmdir -- "$check_dir/mnt" "$check_dir"' EXIT
ceph --conf "$conf" --keyring "$keyring" auth get-key client.admin -o "$check_dir/secret" >/dev/null
path=$(ceph --conf "$conf" --keyring "$keyring" fs subvolume getpath files docs --group_name apps)
mount -t ceph ":$path" "$check_dir/mnt" -o "name=admin,secretfile=$check_dir/secret,fs=files,conf=$conf"
head -c 1048576 /dev/urandom > "$check_dir/input"
cp "$check_dir/input" "$check_dir/mnt/before"
sync
before=$(ceph --conf "$conf" --keyring "$keyring" fs get files --format json | python3 -c 'import json, sys; info = json.load(sys.stdin)["mdsmap"]["info"].values(); print(" ".join(str(d["gid"]) for d in info if d["rank"] == 0 and d["state"] == "up:active"))')
start=$(date +%s)
ceph --conf "$conf" --keyring "$keyring" mds fail files:0
now=
for _ in $(seq 1 300); do
  now=$(ceph --conf "$conf" --keyring "$keyring" fs get files --format json | python3 -c 'import json, sys; info = json.load(sys.stdin)["mdsmap"]["info"].values(); print(" ".join(str(d["gid"]) for d in info if d["rank"] == 0 and d["state"] == "up:active"))')
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

Then repeat `./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" fs status files` until rank 0 is `active` with one
standby again; no apply is needed.

### Object failover (B477)

Stop one RGW daemon, not its guest: with 2/2 pools a stopped guest pauses I/O on
every placement group with a replica there. The other host must keep answering
through the endpoint name and the VIP:

```sh
RGW_DAEMON='' # Daemon name from orch ps --daemon_type rgw above.
: "${RGW_DAEMON:?Set RGW_DAEMON from orch ps}"
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch daemon stop "${RGW_DAEMON:?Set RGW_DAEMON as described above}"
RGW_FAILOVER_OK="false"
if aws --endpoint-url "https://$VIP" s3api put-object --bucket s3-canary-bucket --key failover-canary --body "$OBJECT_DIR/in" &&
   aws --endpoint-url "https://$VIP" s3api get-object --bucket s3-canary-bucket --key failover-canary "$OBJECT_DIR/rgw-failover-out" &&
   cmp "$OBJECT_DIR/in" "$OBJECT_DIR/rgw-failover-out"; then
    RGW_FAILOVER_OK="true"
else
    printf '%s\n' 'FAIL: object access during RGW outage' >&2
fi
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch ps --daemon_type rgw --refresh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch daemon start "${RGW_DAEMON:?Set RGW_DAEMON as described above}"
[ "$RGW_FAILOVER_OK" = "true" ]
```

Retain the object-test private credentials and files until this drill finishes.
For ingress, put the canary object again, then find the host holding
`198.51.100.60` and its keepalived daemon:

```sh
aws --endpoint-url "https://$VIP" s3api put-object --bucket s3-canary-bucket --key canary --body "$OBJECT_DIR/in"
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-02,rhel-03 -- ip -4 -o addr show
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch ps --service_name ingress.rgw.objects
KEEPALIVED_DAEMON='' # Keepalived daemon on the host holding the VIP.
: "${KEEPALIVED_DAEMON:?Set KEEPALIVED_DAEMON from orch ps and the VIP host}"
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch daemon stop "${KEEPALIVED_DAEMON:?Set KEEPALIVED_DAEMON as described above}"
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-02,rhel-03 -- ip -4 -o addr show
INGRESS_FAILOVER_OK="false"
if aws --endpoint-url "https://$VIP" s3api get-object --bucket s3-canary-bucket --key canary "$OBJECT_DIR/failover-out" &&
   cmp "$OBJECT_DIR/in" "$OBJECT_DIR/failover-out"; then
    INGRESS_FAILOVER_OK="true"
else
    printf '%s\n' 'FAIL: object access during ingress outage' >&2
fi
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch daemon start "${KEEPALIVED_DAEMON:?Set KEEPALIVED_DAEMON as described above}"
[ "$INGRESS_FAILOVER_OK" = "true" ]
```

Require the VIP on the other host and the S3 get to succeed before starting
the daemon. Each AWS request and comparison must succeed; record a failure
before restoring a daemon, even if the restore succeeds. After both drills:

```sh
aws --endpoint-url "https://$VIP" s3api delete-object --bucket s3-canary-bucket --key canary
aws --endpoint-url "https://$VIP" s3api delete-object --bucket s3-canary-bucket --key failover-canary
rm -rf -- "${OBJECT_DIR:?Set OBJECT_DIR to the private object-test directory created above}"
```

### One-host outage

The checks run on `rhel-01`, so confirm `rhel-03` is not the admin node, then:

```sh
./bin/bootwright machine stop --context "$CONTEXT" --name rhel-03 --yes
./bin/bootwright machine list --context "$CONTEXT" --power-status
```

Run the following checks:

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" -s
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" health detail
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" mon stat
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" osd tree
```

Health leaves `HEALTH_OK`, one mon is out of quorum and the OSDs of `rhel-03` are
down. With 2/2 pools, I/O to placement groups with a replica there pauses until
the host returns; do not lower `min_size`. Then:

```sh
./bin/bootwright machine start --context "$CONTEXT" --name rhel-03
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-03 -- uptime      # repeat until SSH answers
```

Repeat the checks until `HEALTH_OK` returns with 3 mons in quorum and every OSD
up and in. Recovery needs no apply.

### Replay

On the finished context these settle without work and show no difference:

```sh
./bin/bootwright apply --context "$CONTEXT" --yes
./bin/bootwright diff --context "$CONTEXT" --observe
```

<a id="destroy"></a>

## 6. Destroy and clean up

Ceph retires with its three stopped hosts, and removal while a host survives is
not supported. Destroy refuses while the guests run and names the stop:

```sh
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright machine stop --context "$CONTEXT" --name rhel-01,rhel-02,rhel-03 --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright cluster info --context "$CONTEXT" --name ceph-e2e
virsh --connect qemu:///system list --all
virsh --connect qemu:///system net-list --all
```

Require no domain, disk or network of the context to remain, then delete the
context:

```sh
./bin/bootwright context delete --name "$CONTEXT" --purge --yes
```

Complete [Restore listener firewall](../README.md#restore-listener-firewall), then
[Remove diagnostic tools](../README.md#remove-diagnostic-tools). Only after
both succeed, delete this run's private scratch and rendered input:

```sh
rm -rf -- "${RUN_DIR:?Set RUN_DIR from step 3}" "${INPUT_DIR:?Set INPUT_DIR from step 3}"
```

## 7. Record the result

Record the release, selected service switches, build identity, every check,
the interruption drill and completed cleanup in the
[acceptance ledger](../../docs/acceptance.md). Mark success only after the
whole run passes. Keep host-specific evidence private. Shared media remains.
