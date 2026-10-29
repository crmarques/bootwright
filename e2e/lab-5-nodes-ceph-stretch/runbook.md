# lab-5-nodes-ceph-stretch

One IBM Storage Ceph cluster, `ceph-stretch`, in native stretch mode over three
simulated sites on one libvirt network: `ceph-01`, `ceph-02` (dc1) and
`ceph-03`, `ceph-04` (dc2) at `198.51.100.61-64` each run a MON and an OSD,
and `arbiter-01` (dc3) at `198.51.100.65` runs only the tiebreaker MON. Two
contexts run side by side. `lab-5-nodes-ceph-stretch-arbiter` (`CTX_ARB`)
owns the network, the DNS, NTP and proxy services and installs RHEL 9.8 on
`arbiter-01`. `lab-5-nodes-ceph-stretch` (`CTX_CEPH`) installs the four data
hosts and builds the cluster, taking `arbiter-01` as a provided Machine:
Bootwright receives a host whose RHEL is already installed. Guests: 4 data
hosts of 2 vCPU, 4 GiB and a 15 GiB OSD disk, 1 arbiter of 2 vCPU and 2 GiB
(18 GiB in all).

It rehearses, at the emulated tier only:

- [B893](../../specs/milestones/m2.md#b893) and
  [B894](../../specs/milestones/m2.md#b894): a provided tiebreaker bound with
  its pinned host key and reached with only the fleet key's public half
  authorized.
- [B486](../../specs/milestones/m2.md#b486): the observed quorum, PG and CRUSH
  agreement and the stretch readbacks. Its activation interruption and
  continuation are not claimed: an interrupted stretch stage has no forward
  exit ([B920](../../specs/milestones/m2.md#b920)).
- [B487](../../specs/milestones/m2.md#b487): RBD client I/O through each site,
  and with the toggles CephFS and RGW client I/O through each site, each
  gateway reached directly at its own site. The Data Foundation external
  connect is not covered, so B487's operator gate stays open.
- [B488](../../specs/milestones/m2.md#b488): the emulated half of the fault
  drills, continuation and teardown. Its physical tier stays open.

## Inputs

| `--set` | Default | Effect |
| --- | --- | --- |
| `ceph.services.file=true` | `false` | An MDS on `ceph-02` and `ceph-04`, CephFS `files` on two 4/2 pools |
| `ceph.services.object=true` | `false` | An RGW on `ceph-02` and `ceph-04` on port 8080, reached directly, with user `s3-canary` and its bucket |
| `size.data.memoryMiB=6144` | `4096` | Needed with either service toggle |

The render writes two inputs: `$IN/arbiter` for `CTX_ARB` and `$IN/ceph`
for `CTX_CEPH`. [values.yaml](values.yaml) holds addresses, ports, sites,
sizes and the Ceph coordinates. `CTX_ARB` serves 53, 123, 3128, 8444, 8081 and
BMC 8001; `CTX_CEPH` serves 8443, 8080 and BMC 8000.

## Prepare

The [host preparation](../README.md#prepare-the-host), the RHEL 9.8 boot and
DVD ISOs added as media, the IBM entitlement key
[secret file](../README.md#prepare-the-host) for `cp.icr.io` (the rendered
Entitlement accepts the IBM license; that acceptance is your act), and
`ssh-keyscan`, `dig` and `python3` on the host, plus the AWS CLI with the
object toggle. The [capacity gate](../../specs/substrates.md#capacity-admission)
counts each context's guests, the other context's running guest and one
provider reserve (1 vCPU, 2 GiB, 10 GiB): 11 host CPUs, 20 GiB of available
memory (28 GiB with a service toggle; swap never counts) and 190 GiB free on
the filesystem that holds libvirt's guest disks (4 times 25 + 15 GiB, 20 GiB
for the arbiter, 10 GiB reserve). OSDs and MONs peak during recovery, so keep
the host's own swap enabled.

## Run

Render, import both contexts, generate their secrets. `A` is the host
address; `D` is a private scratch directory for the handoff files, keys and
readbacks of this run, never the repository:

Set the service choices once for this run. Keep these values for repeat
renders; do not reset them between stages. Either service requires at least
6144 MiB per data guest, so the render block raises the default minimum.

```sh
CEPH_FILE="false"
CEPH_OBJECT="false"
DATA_MEMORY_MIB="4096"
IBM_REGISTRY_FILE="$HOME/ibm-ceph-registry.txt"
```

Render and import with those choices:

```sh
if [ "$CEPH_FILE" = "true" ] || [ "$CEPH_OBJECT" = "true" ]; then
    if [ "$DATA_MEMORY_MIB" -lt 6144 ]; then DATA_MEMORY_MIB="6144"; fi
fi
e2e/render.sh lab-5-nodes-ceph-stretch --set "ceph.services.file=$CEPH_FILE" --set "ceph.services.object=$CEPH_OBJECT" --set "size.data.memoryMiB=$DATA_MEMORY_MIB"
CTX_ARB="lab-5-nodes-ceph-stretch-arbiter"
CTX_CEPH="lab-5-nodes-ceph-stretch"
IN="$HOME/bootwright-e2e/lab-5-nodes-ceph-stretch"
A=$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')
umask 077; D=$(mktemp -d)
./bin/bootwright validate -f "$IN/arbiter"
./bin/bootwright validate -f "$IN/ceph"
./bin/bootwright context init --name "$CTX_ARB" --input-dir "$IN/arbiter"
./bin/bootwright context init --name "$CTX_CEPH" --input-dir "$IN/ceph"
./bin/bootwright secret generate --context "$CTX_ARB"
./bin/bootwright secret generate --context "$CTX_CEPH"
install -m 0600 /dev/null "$D/ibm-password"
cut -d: -f2- "${IBM_REGISTRY_FILE:?Set IBM_REGISTRY_FILE to the IBM entitlement key file}" > "$D/ibm-password"
./bin/bootwright secret set --context "$CTX_CEPH" --name ibm-registry --username cp --password-file "$D/ibm-password" --yes
rm -f "$D/ibm-password"
```

Build the arbiter context completely. `CTX_CEPH` depends on its network, its
services and its installed `arbiter-01`, and cannot start before its
`arbiter-known-hosts` Secret exists, because an apply binds every Secret its
operation consumes:

```sh
./bin/bootwright secret check --context "$CTX_ARB"
./bin/bootwright apply --context "$CTX_ARB" --stage controller --yes
./bin/bootwright preflight controller --context "$CTX_ARB"
./bin/bootwright apply --context "$CTX_ARB" --stage infrastructure-services --yes
dig @"$A" +short arbiter-01.e2e.example.test
./bin/bootwright apply --context "$CTX_ARB" --yes
./bin/bootwright machine exec --context "$CTX_ARB" --name arbiter-01 -- hostname
```

`hostname` must print `arbiter-01.e2e.example.test`, the node name the
cluster's cephadm expects. Then hand `arbiter-01` over to `CTX_CEPH`
([stretch topology](../../specs/ceph.md#stretch-topology)):

1. Pin its host key. Scan it, then compare its fingerprint with the one read
   over `CTX_ARB`'s proved connection; they must match.

   ```sh
   ssh-keyscan -t ed25519 198.51.100.65 > "$D/arbiter.known_hosts"
   ssh-keygen -lf "$D/arbiter.known_hosts"
   ./bin/bootwright machine exec --context "$CTX_ARB" --name arbiter-01 -- ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
   ./bin/bootwright secret set --context "$CTX_CEPH" --name arbiter-known-hosts --value-file "$D/arbiter.known_hosts" --yes
   ```

2. Authorize `CTX_CEPH`'s fleet key for the cluster SSH user `bootwright`,
   which `CTX_ARB`'s installation created with its own fleet key. The key
   travels on standard input; the last command must print `1`:

   ```sh
   ./bin/bootwright secret show --context "$CTX_CEPH" --name bootwright-machine-key --part public-key > "$D/fleet.pub"
   ./bin/bootwright machine exec --context "$CTX_ARB" --name arbiter-01 -- sh -c 'cat >> ~/.ssh/authorized_keys' < "$D/fleet.pub"
   cut -d' ' -f2 "$D/fleet.pub" > "$D/fleet.key"
   ./bin/bootwright machine exec --context "$CTX_ARB" --name arbiter-01 -- grep -c -F -f /dev/stdin .ssh/authorized_keys < "$D/fleet.key"
   ```

Build the ceph context by stage. The machines stage is also B488's
continuation drill: press Ctrl-C once while the installations run, inspect,
and repeat the same apply until it completes:

```sh
./bin/bootwright secret check --context "$CTX_CEPH"
./bin/bootwright plan --context "$CTX_CEPH"
./bin/bootwright apply --context "$CTX_CEPH" --stage controller --yes
./bin/bootwright apply --context "$CTX_CEPH" --stage infrastructure-services,substrates --yes
./bin/bootwright apply --context "$CTX_CEPH" --stage machines --yes      # Ctrl-C once during the installations
./bin/bootwright status --context "$CTX_CEPH"
./bin/bootwright apply --context "$CTX_CEPH" --stage machines --yes
./bin/bootwright machine exec --context "$CTX_CEPH" --name ceph-01,ceph-02,ceph-03,ceph-04 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright preflight storage-cluster --context "$CTX_CEPH"
./bin/bootwright apply --context "$CTX_CEPH" --stage clusters --yes
```

Do not interrupt the clusters stage: an interrupted stretch activation stage
refuses `stretch-interrupted` with no exit short of retiring the cluster
(B920). A `transport` refusal on `arbiter-01` names the fleet key, the user
and the pinned host key: repeat the handoff step it names, then the apply.

## Verify

```sh
./bin/bootwright status --context "$CTX_CEPH"
./bin/bootwright diff --context "$CTX_CEPH" --observe
./bin/bootwright status --context "$CTX_ARB"
./bin/bootwright cluster info --context "$CTX_CEPH" --name ceph-stretch
./bin/bootwright cluster info --context "$CTX_CEPH" --name ceph-stretch --output json > "$D/info.json"
FSID=$(python3 -I - "$D/info.json" <<'PY'
import json, sys
def find(o):
    if isinstance(o, dict):
        if o.get("fsid"):
            return o["fsid"]
        o = list(o.values())
    for v in o if isinstance(o, list) else []:
        r = find(v)
        if r:
            return r
print(find(json.load(open(sys.argv[1]))))
PY
)
echo "$FSID"
```

Set the administrator paths printed by `cluster info` on the bootstrap
node `ceph-01`; these files exist only there
([cluster discovery](../../specs/cli/output.md#cluster-discovery)). On
`ceph-03`, confirm cephadm's `_admin` copy under `/etc/ceph`
([special host labels](https://docs.ceph.com/en/tentacle/cephadm/host-management/#special-host-labels)).
If the dc2 check fails, record the dc1 loss drill's cluster reads as unavailable.

```sh
# Copy the --conf and --keyring paths from the cephCommand in cluster info.
CEPH_CONF_DC1=''
CEPH_KEYRING_DC1=''
: "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from cluster info}"
: "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from cluster info}"
# Confirm these cephadm-managed paths on ceph-03 before using them.
CEPH_CONF_DC2='/etc/ceph/ceph.conf'
CEPH_KEYRING_DC2='/etc/ceph/ceph.client.admin.keyring'
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" -s
```

### Stretch readbacks: B486

The JSON keys below are the unverified shapes of the
[stretch native facts](../../.agents/knowledge/ceph-stretch-native-facts.md),
which this run reads from the selected build. Save the reader once:

```sh
cat > "$D/readback.py" <<'PY'
import json, sys
d = sys.argv[1]
mon = json.load(open(d + "/mon-dump.json"))
osd = json.load(open(d + "/osd-dump.json"))
tree = json.load(open(d + "/crush-tree.json"))
print("mon election_strategy:", mon.get("election_strategy"))
print("mon stretch_mode:", mon.get("stretch_mode"))
print("mon tiebreaker_mon:", mon.get("tiebreaker_mon"))
for m in mon.get("mons", []):
    print("mon", m.get("name"), "crush_location:", m.get("crush_location"))
print("osd crush_version:", osd.get("crush_version"))
print("osd stretch_mode:", json.dumps(osd.get("stretch_mode"), sort_keys=True))
for p in osd.get("pools", []):
    print("pool", p.get("pool_name"), "crush_rule", p.get("crush_rule"), "size", p.get("size"), "min_size", p.get("min_size"))
names = {n.get("id"): n.get("name") for n in tree.get("nodes", [])}
for n in tree.get("nodes", []):
    if n.get("type") in ("root", "datacenter", "host"):
        print(n.get("type"), n.get("name"), "children:", sorted(str(names.get(c, c)) for c in n.get("children", [])))
PY
```

A readback, from either site's command, is then:

```sh
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" mon dump -f json > "$D/mon-dump.json"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" osd dump -f json > "$D/osd-dump.json"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" osd crush tree -f json > "$D/crush-tree.json"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" osd crush rule dump stretch-rule
python3 -I "$D/readback.py" "$D"
```

| Key | Expected |
| --- | --- |
| `mon dump` `election_strategy` | `3` (connectivity) |
| `mon dump` `stretch_mode`, `tiebreaker_mon` | `true`; the MON of `arbiter-01` |
| `mons[].crush_location` | `{datacenter=dc1}` for the MONs of `ceph-01` and `ceph-02`, `{datacenter=dc2}` for `ceph-03` and `ceph-04`, `{datacenter=dc3}` for `arbiter-01`; 5 MONs |
| `osd dump` `crush_version` | an integer |
| `osd dump` `stretch_mode` | `stretch_mode_enabled` true, `stretch_bucket_count` 2 |
| `osd dump` `pools[]` | every pool, `.mgr` included, at the `rule_id` of `stretch-rule`, `size` 4, `min_size` 2 |
| `osd crush tree` `nodes[]` | root `default` with children `dc1` and `dc2`, each with its two host buckets; no `dc3` bucket and no bucket for `arbiter-01` |

A key that is absent or shaped otherwise prints `None` or its actual value:
record it as read, for the knowledge page. Beyond the keys, record the facts
the page leaves open:

- Pools created after activation (`rbd-data`, with the toggles the CephFS
  pools, `.rgw.root` and the `objects.rgw.*` zone pools): each one's rule
  and 4/2 above, and whether its pool size update was accepted,
  which the clusters stage completing shows.
- Whether `enable_stretch_mode` refuses data-site buckets of unequal CRUSH
  weight: not exercised, since both sites hold equal OSDs; record it so.
- The daemon names on the tiebreaker, which settle the crash collector's ID
  on a host with a full hostname. List the crash and MON daemons:

```sh
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" orch ps --daemon_type crash
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" orch ps --daemon_type mon
```

| Check | Expected |
| --- | --- |
| `./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" quorum_status` | all 5 in `quorum_names` |
| `./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" pg ls-by-pool rbd-data` | every acting set has 4 OSDs, 2 per site |
| `./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" orch ps` | only `mon` and `crash` on `arbiter-01`; MGRs on `ceph-01` and `ceph-03`; with the toggles the MDS and RGW daemons on `ceph-02` and `ceph-04` |
| file: `./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" fs status files` | one active MDS and one standby, at different sites |

### Client I/O through each site: B487

RBD, with the admin pair of the node: `write` creates the named image from
1 MiB, compares it and prints its SHA-256; `read` prints the SHA-256 of an
existing image. Write one image per site, then read each from the other
site's node; the sums must match:

```sh
cat > "$D/rbd-io.sh" <<'IO'
set -euo pipefail
umask 077
f=$(mktemp /var/tmp/stretch-io.XXXXXXXX)
trap 'rm -f -- "$f"' EXIT
case $4 in
  write)
    head -c 1048576 /dev/urandom > "$f"
    rbd --conf "$1" --keyring "$2" import "$f" "rbd-data/$3"
    rbd --conf "$1" --keyring "$2" export "rbd-data/$3" - | cmp - "$f"
    sha256sum < "$f" ;;
  read)
    rbd --conf "$1" --keyring "$2" export "rbd-data/$3" - | sha256sum ;;
esac
IO
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n bash -s -- "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" site-dc1 write < "$D/rbd-io.sh"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n bash -s -- "$CEPH_CONF_DC2" "$CEPH_KEYRING_DC2" site-dc2 write < "$D/rbd-io.sh"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n bash -s -- "$CEPH_CONF_DC2" "$CEPH_KEYRING_DC2" site-dc1 read < "$D/rbd-io.sh"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n bash -s -- "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" site-dc2 read < "$D/rbd-io.sh"
```

CephFS (file toggle), the same pattern through a kernel mount of `files` as
the administrator on each data site's node:

```sh
cat > "$D/fs-io.sh" <<'FS'
set -euo pipefail
umask 077
d=$(mktemp -d /var/tmp/stretch-fs.XXXXXXXX)
mkdir "$d/mnt"
trap 'umount "$d/mnt" 2>/dev/null || true; rm -f -- "$d/secret" "$d/in"; rmdir -- "$d/mnt" "$d"' EXIT
ceph --conf "$1" --keyring "$2" auth get-key client.admin -o "$d/secret" >/dev/null
mount -t ceph :/ "$d/mnt" -o "name=admin,secretfile=$d/secret,fs=files,conf=$1"
case $4 in
  write)
    head -c 1048576 /dev/urandom > "$d/in"
    cp "$d/in" "$d/mnt/$3"
    sync
    cmp "$d/mnt/$3" "$d/in"
    sha256sum < "$d/in" ;;
  read)
    sha256sum < "$d/mnt/$3" ;;
esac
FS
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n bash -s -- "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" site-dc1 write < "$D/fs-io.sh"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n bash -s -- "$CEPH_CONF_DC2" "$CEPH_KEYRING_DC2" site-dc2 write < "$D/fs-io.sh"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n bash -s -- "$CEPH_CONF_DC2" "$CEPH_KEYRING_DC2" site-dc1 read < "$D/fs-io.sh"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n bash -s -- "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" site-dc2 read < "$D/fs-io.sh"
```

S3 (object toggle), each gateway at its own site's address. The keys stay in
files under `$D`, never on the terminal or the command line:

```sh
./bin/bootwright cluster info --context "$CTX_CEPH" --name ceph-stretch --object-user s3-canary --output json > "$D/access.json"
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
export AWS_SHARED_CREDENTIALS_FILE="$D/credentials" AWS_DEFAULT_REGION=objects
DC1="http://198.51.100.62:8080"
DC2="http://198.51.100.64:8080"
head -c 1048576 /dev/urandom > "$D/s3-in"
aws --endpoint-url "$DC1" s3api put-object --bucket s3-canary-bucket --key site-dc1 --body "$D/s3-in"
aws --endpoint-url "$DC2" s3api get-object --bucket s3-canary-bucket --key site-dc1 "$D/s3-out" && cmp "$D/s3-in" "$D/s3-out"
aws --endpoint-url "$DC2" s3api put-object --bucket s3-canary-bucket --key site-dc2 --body "$D/s3-in"
aws --endpoint-url "$DC1" s3api get-object --bucket s3-canary-bucket --key site-dc2 "$D/s3-out" && cmp "$D/s3-in" "$D/s3-out"
```

The guest network has no DNS record for the data hosts or for
`s3.e2e.example.test`: the DNS server belongs to `CTX_ARB`, which declares
neither, so the gateways are addressed directly.

## Acceptance drills

Run each drill from a healthy cluster (`HEALTH_OK`, every PG
`active+clean`). Record cluster health, quorum status, the readback above and
each client I/O result, then restore and wait for `HEALTH_OK` before the
next. Native degraded stretch may lower `min_size` to 1; recovery must
restore 2
([stretch constraints](../../specs/ceph-coverage.md#stretch-constraints)).
The `osd dump` `stretch_mode` object is recorded whole: upstream's degraded
and recovering flags are not fixed by any cited page and stay unverified
until read here.

### Data-site loss and restored-site resynchronization

Stop dc1. From dc2 require a quorum of 3 with the tiebreaker, pools at
`min_size 1` and the dc2 MGR active. With the toggles, also run the CephFS
and S3 writes below. The dc1 gateway read must fail while dc1 is stopped.

```sh
./bin/bootwright machine stop --context "$CTX_CEPH" --name ceph-01,ceph-02 --force --yes
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" -s
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" quorum_status
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" osd dump -f json > "$D/osd-dump.json"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" mon dump -f json > "$D/mon-dump.json"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" osd crush tree -f json > "$D/crush-tree.json"
python3 -I "$D/readback.py" "$D"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n bash -s -- "$CEPH_CONF_DC2" "$CEPH_KEYRING_DC2" loss-dc1 write < "$D/rbd-io.sh"
# File toggle only.
if [ "$CEPH_FILE" = "true" ]; then
    ./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n bash -s -- "$CEPH_CONF_DC2" "$CEPH_KEYRING_DC2" loss-dc1 write < "$D/fs-io.sh"
fi
# Object toggle only. The final command must fail.
if [ "$CEPH_OBJECT" = "true" ]; then
    aws --endpoint-url "$DC2" s3api put-object --bucket s3-canary-bucket --key loss-dc1 --body "$D/s3-in"
    aws --endpoint-url "$DC1" s3api get-object --bucket s3-canary-bucket --key loss-dc1 "$D/s3-out"
fi
```

Run the gateway failure drill below before restoring dc1 if the object toggle
is enabled. Restore, wait for `HEALTH_OK`, require `min_size 2` and two
replicas per site, and compare the restored site's reads with the writes:

```sh
./bin/bootwright machine start --context "$CTX_CEPH" --name ceph-01,ceph-02
```

Power-on returns before SSH and Ceph recovery complete. Repeat this observation
block until every restarted host answers, health is `HEALTH_OK`, every pool
has `min_size 2` and the PG acting sets have two replicas per site. Do not run
the restored-site reads until all these conditions hold:

```sh
./bin/bootwright machine exec --context "$CTX_CEPH" --name ceph-01,ceph-02 -- uptime
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" -s
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" osd dump
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" pg ls-by-pool rbd-data
```

Once those gates pass, compare the restored-site reads with the loss writes:

```sh
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n bash -s -- "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" loss-dc1 read < "$D/rbd-io.sh"
# File toggle only.
if [ "$CEPH_FILE" = "true" ]; then
    ./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n bash -s -- "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" loss-dc1 read < "$D/fs-io.sh"
fi
# Object toggle only.
if [ "$CEPH_OBJECT" = "true" ]; then
    aws --endpoint-url "$DC1" s3api get-object --bucket s3-canary-bucket --key loss-dc1 "$D/s3-out"
    cmp "$D/s3-in" "$D/s3-out"
fi
```

Repeat for dc2, with reads and writes through dc1; the dc2 gateway read must
fail. Restore dc2 and check the same health, pool size and replica gates:

```sh
./bin/bootwright machine stop --context "$CTX_CEPH" --name ceph-03,ceph-04 --force --yes
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" -s
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" quorum_status
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" osd dump -f json > "$D/osd-dump.json"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" mon dump -f json > "$D/mon-dump.json"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" osd crush tree -f json > "$D/crush-tree.json"
python3 -I "$D/readback.py" "$D"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n bash -s -- "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" loss-dc2 write < "$D/rbd-io.sh"
# File toggle only.
if [ "$CEPH_FILE" = "true" ]; then
    ./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n bash -s -- "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" loss-dc2 write < "$D/fs-io.sh"
fi
# Object toggle only. The final command must fail.
if [ "$CEPH_OBJECT" = "true" ]; then
    aws --endpoint-url "$DC1" s3api put-object --bucket s3-canary-bucket --key loss-dc2 --body "$D/s3-in"
    aws --endpoint-url "$DC2" s3api get-object --bucket s3-canary-bucket --key loss-dc2 "$D/s3-out"
fi
./bin/bootwright machine start --context "$CTX_CEPH" --name ceph-03,ceph-04
```

Power-on returns before SSH and Ceph recovery complete. Repeat this observation
block until every restarted host answers, health is `HEALTH_OK`, every pool
has `min_size 2` and the PG acting sets have two replicas per site. Do not run
the restored-site reads until all these conditions hold:

```sh
./bin/bootwright machine exec --context "$CTX_CEPH" --name ceph-03,ceph-04 -- uptime
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" -s
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" osd dump
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" pg ls-by-pool rbd-data
```

Once those gates pass, compare the restored-site reads with the loss writes:

```sh
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n bash -s -- "$CEPH_CONF_DC2" "$CEPH_KEYRING_DC2" loss-dc2 read < "$D/rbd-io.sh"
# File toggle only.
if [ "$CEPH_FILE" = "true" ]; then
    ./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n bash -s -- "$CEPH_CONF_DC2" "$CEPH_KEYRING_DC2" loss-dc2 read < "$D/fs-io.sh"
fi
# Object toggle only.
if [ "$CEPH_OBJECT" = "true" ]; then
    aws --endpoint-url "$DC2" s3api get-object --bucket s3-canary-bucket --key loss-dc2 "$D/s3-out"
    cmp "$D/s3-in" "$D/s3-out"
fi
```

### Tiebreaker loss

`CTX_ARB` owns its power. Require a quorum of 4 and no degraded stretch;
repeat the read commands from Verify through both sites before restore.

```sh
./bin/bootwright machine stop --context "$CTX_ARB" --name arbiter-01 --force --yes
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" -s
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-01 -- sudo -n ceph --conf "${CEPH_CONF_DC1:?Set CEPH_CONF_DC1 from the discovery step above}" --keyring "${CEPH_KEYRING_DC1:?Set CEPH_KEYRING_DC1 from the discovery step above}" quorum_status
./bin/bootwright machine start --context "$CTX_ARB" --name arbiter-01
```

### Inter-site and asymmetric partitions

Cut dc1 from dc2 inside dc1's guests while both reach the tiebreaker:

```sh
./bin/bootwright machine exec --context "$CTX_CEPH" --name ceph-01 -- sudo -n nft -f - <<'NFT'
add table inet e2e
add chain inet e2e in { type filter hook input priority 0; }
add chain inet e2e out { type filter hook output priority 0; }
add rule inet e2e in ip saddr { 198.51.100.63, 198.51.100.64 } drop
add rule inet e2e out ip daddr { 198.51.100.63, 198.51.100.64 } drop
NFT
./bin/bootwright machine exec --context "$CTX_CEPH" --name ceph-02 -- sudo -n nft -f - <<'NFT'
add table inet e2e
add chain inet e2e in { type filter hook input priority 0; }
add chain inet e2e out { type filter hook output priority 0; }
add rule inet e2e in ip saddr { 198.51.100.63, 198.51.100.64 } drop
add rule inet e2e out ip daddr { 198.51.100.63, 198.51.100.64 } drop
NFT
```

The connectivity election keeps a quorum with the tiebreaker and one data
site; the other site's OSDs go down and stretch degrades. Record which site
survives. Select that node and its already confirmed administrator paths
below, then record health, quorum, readback and client I/O through it:

```sh
# Set from the observed surviving site: ceph-01 or ceph-03, and its paths above.
SURVIVING_NODE=''
SURVIVING_CONF=''
SURVIVING_KEYRING=''
# Use a fresh image/file/object name for each partition drill.
PARTITION_OBJECT="partition-symmetric"
: "${SURVIVING_NODE:?Set SURVIVING_NODE from the surviving site}"
: "${SURVIVING_CONF:?Set SURVIVING_CONF to the confirmed config path on the selected node}"
: "${SURVIVING_KEYRING:?Set SURVIVING_KEYRING to the confirmed keyring path on the selected node}"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node "${SURVIVING_NODE:?Set SURVIVING_NODE from the discovery step above}" -- sudo -n ceph --conf "${SURVIVING_CONF:?Set SURVIVING_CONF from the discovery step above}" --keyring "${SURVIVING_KEYRING:?Set SURVIVING_KEYRING from the discovery step above}" -s
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node "${SURVIVING_NODE:?Set SURVIVING_NODE from the discovery step above}" -- sudo -n ceph --conf "${SURVIVING_CONF:?Set SURVIVING_CONF from the discovery step above}" --keyring "${SURVIVING_KEYRING:?Set SURVIVING_KEYRING from the discovery step above}" quorum_status
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node "${SURVIVING_NODE:?Set SURVIVING_NODE from the discovery step above}" -- sudo -n ceph --conf "${SURVIVING_CONF:?Set SURVIVING_CONF from the discovery step above}" --keyring "${SURVIVING_KEYRING:?Set SURVIVING_KEYRING from the discovery step above}" mon dump -f json > "$D/mon-dump.json"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node "${SURVIVING_NODE:?Set SURVIVING_NODE from the discovery step above}" -- sudo -n ceph --conf "${SURVIVING_CONF:?Set SURVIVING_CONF from the discovery step above}" --keyring "${SURVIVING_KEYRING:?Set SURVIVING_KEYRING from the discovery step above}" osd dump -f json > "$D/osd-dump.json"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node "${SURVIVING_NODE:?Set SURVIVING_NODE from the discovery step above}" -- sudo -n ceph --conf "${SURVIVING_CONF:?Set SURVIVING_CONF from the discovery step above}" --keyring "${SURVIVING_KEYRING:?Set SURVIVING_KEYRING from the discovery step above}" osd crush tree -f json > "$D/crush-tree.json"
python3 -I "$D/readback.py" "$D"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node "${SURVIVING_NODE:?Set SURVIVING_NODE from the discovery step above}" -- sudo -n bash -s -- "${SURVIVING_CONF:?Set SURVIVING_CONF from the discovery step above}" "${SURVIVING_KEYRING:?Set SURVIVING_KEYRING from the discovery step above}" "${PARTITION_OBJECT:?Set a fresh PARTITION_OBJECT for this drill}" write < "$D/rbd-io.sh"
# File toggle only.
if [ "$CEPH_FILE" = "true" ]; then
    ./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node "${SURVIVING_NODE:?Set SURVIVING_NODE from the discovery step above}" -- sudo -n bash -s -- "${SURVIVING_CONF:?Set SURVIVING_CONF from the discovery step above}" "${SURVIVING_KEYRING:?Set SURVIVING_KEYRING from the discovery step above}" "${PARTITION_OBJECT:?Set a fresh PARTITION_OBJECT for this drill}" write < "$D/fs-io.sh"
fi
# Object toggle only: set to the DC1 or DC2 endpoint for the surviving site.
if [ "$CEPH_OBJECT" = "true" ]; then
    SURVIVING_ENDPOINT=''
    : "${SURVIVING_ENDPOINT:?Set SURVIVING_ENDPOINT to the surviving site endpoint}"
    aws --endpoint-url "${SURVIVING_ENDPOINT:?Set SURVIVING_ENDPOINT from the discovery step above}" s3api put-object --bucket s3-canary-bucket --key "${PARTITION_OBJECT:?Set a fresh PARTITION_OBJECT for this drill}" --body "$D/s3-in"
fi
./bin/bootwright machine exec --context "$CTX_CEPH" --name ceph-01,ceph-02 -- sudo -n nft delete table inet e2e
```

Wait for health to recover. Then create an asymmetric partition: dc1 drops
what dc2 sends while dc1's packets still arrive. Record which site keeps
quorum and whether I/O stays available, using the same survivor selection
and checks above (set `PARTITION_OBJECT="partition-asymmetric"`), then heal with the table deletion command above:

```sh
./bin/bootwright machine exec --context "$CTX_CEPH" --name ceph-01 -- sudo -n nft -f - <<'NFT'
add table inet e2e
add chain inet e2e in { type filter hook input priority 0; }
add rule inet e2e in ip saddr { 198.51.100.63, 198.51.100.64 } drop
NFT
./bin/bootwright machine exec --context "$CTX_CEPH" --name ceph-02 -- sudo -n nft -f - <<'NFT'
add table inet e2e
add chain inet e2e in { type filter hook input priority 0; }
add rule inet e2e in ip saddr { 198.51.100.63, 198.51.100.64 } drop
NFT
```

### Surviving-site gateway failure (object toggle)

During dc1's loss, stop ceph-04's RGW. S3 through dc2 must fail with its only
gateway down, the expected safe unavailability. Start the daemon again:

```sh
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" orch ps --daemon_type rgw
# Copy the daemon_name for the RGW on ceph-04 from that listing.
RGW_DC2_DAEMON=''
: "${RGW_DC2_DAEMON:?Set RGW_DC2_DAEMON from orch ps on ceph-04}"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" orch daemon stop "${RGW_DC2_DAEMON:?Set RGW_DC2_DAEMON from the discovery step above}"
# Expected failure while the surviving site's gateway is stopped.
aws --endpoint-url "$DC2" s3api get-object --bucket s3-canary-bucket --key loss-dc1 "$D/s3-out"
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" orch daemon start "${RGW_DC2_DAEMON:?Set RGW_DC2_DAEMON from the discovery step above}"
```

### Interrupted recovery

Repeat dc1's loss, then restore it. While health shows recovery, stop ceph-02
and start it again. Recovery must resume and end at `HEALTH_OK`, no degraded
stretch and `min_size 2`:

```sh
./bin/bootwright machine stop --context "$CTX_CEPH" --name ceph-01,ceph-02 --force --yes
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" -s
./bin/bootwright machine start --context "$CTX_CEPH" --name ceph-01,ceph-02
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" -s
# Run while the preceding status shows recovery.
./bin/bootwright machine stop --context "$CTX_CEPH" --name ceph-02 --force --yes
./bin/bootwright machine start --context "$CTX_CEPH" --name ceph-02
```

Repeat the following observations until ceph-02 answers, health is `HEALTH_OK`
and the pool and stretch readbacks show recovery complete with `min_size 2`:

```sh
./bin/bootwright machine exec --context "$CTX_CEPH" --name ceph-02 -- uptime
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" -s
./bin/bootwright cluster exec --context "$CTX_CEPH" --name ceph-stretch --node ceph-03 -- sudo -n ceph --conf "$CEPH_CONF_DC2" --keyring "$CEPH_KEYRING_DC2" osd dump
```

## Destroy

Destroy `CTX_CEPH` first. Its cluster block removes the tiebreaker's Ceph
footprint over the fleet connection, and refuses `tiebreaker-unreachable`
without `arbiter-01`, while a tiebreaker already reinstalled or deleted has
no exit ([B938](../../specs/milestones/m2.md#b938)); `CTX_CEPH` also
attaches to `CTX_ARB`'s network.

```sh
./bin/bootwright destroy --context "$CTX_CEPH" --authorize data-loss
./bin/bootwright machine stop --context "$CTX_CEPH" --name ceph-01,ceph-02,ceph-03,ceph-04 --yes
./bin/bootwright destroy --context "$CTX_CEPH" --authorize data-loss
./bin/bootwright status --context "$CTX_CEPH"
```

The first destroy runs the online Ceph cleanup, `arbiter-01`'s footprint
included, then pauses, naming the stop of the data hosts. Before destroying
`CTX_ARB`, read each
[absence](../../specs/ceph.md#a-provided-tiebreakers-removal) the removal
proves, and the parts of its footprint the knowledge page leaves unverified:

```sh
./bin/bootwright machine exec --context "$CTX_ARB" --name arbiter-01 -- sudo -n bash -s -- "$FSID" <<'ABSENCE'
fsid=$1
for p in "/var/lib/ceph/$fsid" "/var/log/ceph/$fsid" "/etc/logrotate.d/ceph-$fsid" /etc/ceph/podman-auth.json; do
  if [ -e "$p" ] || [ -L "$p" ]; then echo "PRESENT $p"; else echo "absent $p"; fi
done
for dir in /etc/systemd/system /etc/systemd/system/ceph.target.wants; do
  if ls -A "$dir" 2>/dev/null | grep -F "ceph-$fsid"; then echo "PRESENT unit in $dir"; else echo "absent ceph-$fsid units in $dir"; fi
done
if grep -qF "$fsid" /etc/ceph/ceph.conf 2>/dev/null; then echo "PRESENT /etc/ceph/ceph.conf names the FSID"; else echo "absent FSID in /etc/ceph/ceph.conf"; fi
ls -lA /var/lib/ceph /etc/ceph /etc/ssh/sshd_config.d "/var/lib/bootwright/ceph/$fsid"
ls -l /var/log/ceph/cephadm.log /etc/logrotate.d/cephadm
systemctl list-units --all 'ceph*'
ABSENCE
./bin/bootwright secret show --context "$CTX_CEPH" --name ceph-cluster-key --part public-key > "$D/cluster.pub"
cut -d' ' -f2 "$D/cluster.pub" > "$D/cluster.key"
./bin/bootwright machine exec --context "$CTX_ARB" --name arbiter-01 -- sudo -n grep -c -F -f /dev/stdin /home/bootwright/.ssh/authorized_keys < "$D/cluster.key"
./bin/bootwright machine exec --context "$CTX_ARB" --name arbiter-01 -- sudo -n grep -c -F -f /dev/stdin /home/bootwright/.ssh/authorized_keys < "$D/fleet.key"
```

Require every `absent` line, nothing of the FSID in `/var/lib/ceph`, `0` for
the cluster key and `1` for the fleet key line you added, which stays. The
sshd drop-in with the `05-bootwright-ceph-` prefix and `.conf` suffix stays
([B940](../../specs/milestones/m2.md#b940)), and so do Bootwright's own
records under `/var/lib/bootwright/ceph/$FSID`. Record as read, unverified
until now: whether `cephadm.log` and `/etc/logrotate.d/cephadm` exist again,
and the units systemd still lists. Then:

```sh
./bin/bootwright machine stop --context "$CTX_ARB" --name arbiter-01 --yes
./bin/bootwright destroy --context "$CTX_ARB" --authorize data-loss
virsh --connect qemu:///system list --all
./bin/bootwright context delete --name "$CTX_CEPH" --purge --yes
./bin/bootwright context delete --name "$CTX_ARB" --purge --yes
rm -rf "$D"
```

If `CTX_CEPH`'s apply was refused before its claim reached `arbiter-01`, its
destroy refuses `tiebreaker-foreign` on every retry, because the host holds
no owner marker ([B942](../../specs/milestones/m2.md#b942)). Finish the apply
first: repair what it names and repeat `./bin/bootwright apply --context "$CTX_CEPH"` until
the clusters stage completes, then destroy. Only when the apply cannot
finish, the last exit is `./bin/bootwright context delete --name "$CTX_CEPH" --purge
--allow-orphans`, which abandons what the context owns without removing it
([permanent deletion](../../specs/contexts.md#permanent-deletion)): then
remove its four guests and their disks by hand, and destroy `CTX_ARB`, which
deletes `arbiter-01` with whatever it holds. Media stays for the next case.
