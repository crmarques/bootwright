# lab-3-nodes-ceph-host-variants

Profile P5 of the [Ceph profile catalog](../../specs/ceph-coverage.md#profile-catalog):
IBM Storage Ceph on three RHEL 9.8 guests, `rhel-01` to `rhel-03`, with the
host and security variants. Each guest has two virtual NICs in an active-backup
bond (`bond0` at `198.51.100.11` to `198.51.100.13`) and VLAN 140 on it
carrying the cluster network (`203.0.113.11` to `203.0.113.13`, no gateway).
Both NICs join one bridge, `virbr-e2e`, so the bond proves neither physical
network isolation nor independent links. The guests register with RHSM, install
in FIPS mode without OSPP and encrypt their root with LUKS unlocked by the
guest TPM2 and recovered by a passphrase; both SSH keys are ECDSA P-256 and
CephX uses AES. Cluster `ceph-e2e` has 3 mons, 2 mgrs and one OSD per host on an
80 GiB virtual SSD, and a 2/2 replicated RBD pool. Each guest has 4 vCPU and a
60 GiB root; memory is 13, 13 and 10 GiB (IBM's per-guest figures of 11, 11
and 8 GB plus 2 GiB of operating-system headroom). Context
`lab-3-nodes-ceph-host-variants`; 3 guests, 12 vCPU, 36 GiB RAM, 180 GiB root
and 240 GiB data disks. Needs a larger host than a 32 GiB laptop: 13 CPUs,
40 GiB available memory, 430 GiB disk (the guests plus the provider's 1 CPU,
4 GiB, 10 GiB `capacityReserve`, as
[capacity admission](../../specs/substrates.md#capacity-admission) counts them).

It accepts [B484](../../specs/milestones/m2.md#b484)'s P5 row: the run
produces the [Ceph matrix row](../../docs/acceptance.md#ceph-matrix-row) for
P5, with its measured budget fields. The same checks rehearse
[B74](../../specs/milestones/m1.md#b74),
[B75](../../specs/milestones/m1.md#b75),
[B76](../../specs/milestones/m1.md#b76) and
[B77](../../specs/milestones/delivered.md#x246--bounded-managed-os-operator-acceptance) on a Ceph host; those items are accepted
through [lab-rhel](../lab-rhel/runbook.md) and
[lab-network-variants](../lab-network-variants/runbook.md), and
[B346](../../specs/milestones/m1.md#b346) needs a FIPS-mode controller.

B484's acceptance baseline is not registered: its section requires the X61
closing build's full commit to be registered in a later documentation commit
before the owner's B484 runs, and each row runs on a clean build descending from
it. Until then a run of this case is observation, not acceptance.

Run every numbered step in order in one Bash session from the checkout under
test. A successful run includes all checks for the selected inputs, the
required drills and cleanup. An unexpected failure fails the run; retain its
private records for cleanup. Run each other input variant from the beginning.

## 1. Choose the inputs

Everything P5 varies is fixed in the templates: FIPS without OSPP, the TPM2
unlock with an empty PCR policy, the AES CephX key type, the bond and VLAN, the
4 GiB OSD memory target (`4294967296`, a target and not a process cap) and the
2/2 pool. [values.yaml](values.yaml) holds the addresses, upstreams, media
names, VLAN id, cluster CIDR, guest sizes and the release coordinates. A release
moves `ceph.release`, `packageVersion`, `ansiblePackageVersion`, `image` and
`callHome` together: `enabled` for 20.2.x builds, `disabled` for 20.1.0-221;
the native tools judge installability.

| Secret | Source |
| --- | --- |
| `ibm-registry` | you set the IBM entitlement key (user `cp`) |
| `rhsm-org`, `rhsm-key` | you set the RHSM organization ID and activation key |
| `luks-recovery` | you set the root recovery passphrase and keep it private |
| `bootwright-machine-key`, `ceph-cluster-key` | generated, ECDSA P-256 |

The TPM2 policy selects no PCRs: it protects a copied disk without its TPM, not
a tampered boot ([managed-OS contract](../../specs/managed-os.md)).

Qualification needs two complete runs: first `TEST_MODE=measured`, then
`TEST_MODE=interruption`. Follow every numbered step through cleanup in both.
Only the measured run supplies performance figures; the interruption run
supplies the deliberate clusters-stage interruption result.

```sh
TEST_MODE=measured # Use interruption for the second complete run.
case "$TEST_MODE" in measured|interruption) ;; *) printf 'Invalid TEST_MODE\n' >&2; false ;; esac
IBM_REGISTRY_FILE="$HOME/ibm-ceph-registry.txt"
RHSM_ORG_FILE="$HOME/rhel-org.txt"
RHSM_KEY_FILE="$HOME/rhel-activation-key.txt"
LUKS_RECOVERY_FILE='' # Absolute path to your mode-0600 root recovery passphrase file.
```

## 2. Prepare the host

Complete [host preparation](../README.md#prepare-the-host) now, including the RHEL 9.8 boot and
DVD ISOs added as media, the IBM entitlement key, RHSM organization ID and
activation key [secret files](../README.md#prepare-the-host) (the rendered
Entitlement carries `license.accept: true`; accepting the IBM license terms is
your act), a recovery passphrase in a mode-0600 file, and the capacity listed
above. Require `python3` on the controller (`command -v python3`). The recovery
and bond drills need guest console access. Keep evidence outside the
repository in the private `EVIDENCE_DIR` created below.

<a id="run"></a>

## 3. Create and apply the context

```sh
e2e/render.sh lab-3-nodes-ceph-host-variants
CONTEXT="lab-3-nodes-ceph-host-variants"; INPUT_DIR="$HOME/bootwright-e2e/lab-3-nodes-ceph-host-variants"
umask 077
EVIDENCE_DIR=$(mktemp -d "$HOME/bootwright-e2e/p5-evidence.XXXXXXXX")
printf 'Retained evidence: %s\n' "$EVIDENCE_DIR"
printf '%s\n' "$TEST_MODE" > "$EVIDENCE_DIR/test-mode"
CONTROLLER_ADDRESS=$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')
RUN_DIR=$(mktemp -d)                          # private scratch for this run
./bin/bootwright validate -f "$INPUT_DIR"      # 28 files seen, 28 objects decoded
./bin/bootwright context init --name "$CONTEXT" --input-dir "$INPUT_DIR"
./bin/bootwright secret generate --context "$CONTEXT"
install -m 0600 /dev/null "$RUN_DIR/ibm-password"
cut -d: -f2- "${IBM_REGISTRY_FILE:?Set IBM_REGISTRY_FILE to the IBM entitlement key file}" > "$RUN_DIR/ibm-password"
./bin/bootwright secret set --context "$CONTEXT" --name ibm-registry --username cp --password-file "$RUN_DIR/ibm-password" --yes
rm -f "$RUN_DIR/ibm-password"
./bin/bootwright secret set --context "$CONTEXT" --name rhsm-org --value-file "${RHSM_ORG_FILE:?Set RHSM_ORG_FILE to the RHSM organization ID file}" --yes
./bin/bootwright secret set --context "$CONTEXT" --name rhsm-key --value-file "${RHSM_KEY_FILE:?Set RHSM_KEY_FILE to the RHSM activation key file}" --yes
./bin/bootwright secret set --context "$CONTEXT" --name luks-recovery --value-file "${LUKS_RECOVERY_FILE:?Set LUKS_RECOVERY_FILE to the recovery passphrase file}" --yes
./bin/bootwright secret check --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT"
```

Set every credential before the first apply: it freezes them. Prepare the
controller tools before collecting measurement (a), which precedes any guest
or infrastructure realization:

```sh
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
```

Define the read-only budget capture and take measurement (a):

```sh
record_budget() (
    set -e
    {
        date -Is
        nproc
        grep MemAvailable /proc/meminfo
        if sudo test -d /var/lib/libvirt/images/bootwright/lab-3-nodes-ceph-host-variants; then
            sudo du -sb --apparent-size /var/lib/libvirt/images/bootwright/lab-3-nodes-ceph-host-variants
            sudo du -sb /var/lib/libvirt/images/bootwright/lab-3-nodes-ceph-host-variants
        else
            printf '%s\n' 'Guest image directory absent'
        fi
        if [ "$1" != a ]; then
            virsh --connect qemu:///system domstats --balloon --vcpu
        fi
    } > "$EVIDENCE_DIR/budget-$1.txt"
    cat "$EVIDENCE_DIR/budget-$1.txt"
)
record_budget a
```

Set the listener ports, then complete
[Prepare listener firewall](../README.md#prepare-listener-firewall):

```bash
FIREWALL_PORTS=(53/tcp 53/udp 123/udp 3128/tcp 8080/tcp 8443/tcp)
```

Require controller preflight to report `ready`:

```sh
./bin/bootwright preflight controller --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services,substrates --yes
./bin/bootwright apply --context "$CONTEXT" --stage machines --yes
./bin/bootwright machine list --context "$CONTEXT" --power-status
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01,rhel-02,rhel-03 -- lsblk -b -o NAME,TYPE,SIZE,SERIAL,ROTA,FSTYPE,MOUNTPOINTS
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01,rhel-02,rhel-03 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01,rhel-02,rhel-03 -- chronyc tracking
./bin/bootwright preflight storage-cluster --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT" --stage clusters
```

Require the declared disks (the data disk `/dev/sda`, `ROTA=0`, the root a
`crypto_LUKS` volume on `/dev/vda`), synchronized clocks and working
resolution of the IBM hosts.

### Check the installed hosts

Check every guest before the clusters stage:

```sh
HOSTS="rhel-01,rhel-02,rhel-03" # Select one guest here if observed mapper or partition paths differ.
./bin/bootwright machine exec --context "$CONTEXT" --name "${HOSTS:?Set HOSTS to the guests being checked}" -- lsblk -p -o NAME,TYPE,FSTYPE,MOUNTPOINTS
```

Set the observed mapper name, then run the checks:

```sh
ROOT_MAPPER='' # Root mapper name from lsblk; verify it on every selected guest.
: "${ROOT_MAPPER:?Set ROOT_MAPPER from lsblk}"
./bin/bootwright machine exec --context "$CONTEXT" --name "${HOSTS:?Set HOSTS to the guests being checked}" -- cat /proc/sys/crypto/fips_enabled
./bin/bootwright machine exec --context "$CONTEXT" --name "${HOSTS:?Set HOSTS to the guests being checked}" -- update-crypto-policies --show
./bin/bootwright machine exec --context "$CONTEXT" --name "${HOSTS:?Set HOSTS to the guests being checked}" -- sudo -n nmstatectl show
./bin/bootwright machine exec --context "$CONTEXT" --name "${HOSTS:?Set HOSTS to the guests being checked}" -- cat /proc/net/bonding/bond0
./bin/bootwright machine exec --context "$CONTEXT" --name "${HOSTS:?Set HOSTS to the guests being checked}" -- sudo -n subscription-manager identity
./bin/bootwright machine exec --context "$CONTEXT" --name "${HOSTS:?Set HOSTS to the guests being checked}" -- sudo -n cryptsetup status "${ROOT_MAPPER:?Set ROOT_MAPPER as described above}"
```

If root mapper names differ, repeat with `HOSTS` and `ROOT_MAPPER` set for
each guest separately. Require FIPS
value `1` and policy `FIPS` without OSPP; the bond in `active-backup` mode with
both NICs, and `bond0.140` addressed `203.0.113.11` to `203.0.113.13` (`/24`) as declared; managed
registration, whose identity output stays private. Inspect the root LUKS header
and the clevis slot without exporting keys:

```sh
ROOT_LUKS_DEVICE='' # Full /dev/... path of the crypto_LUKS partition backing /; verify it on every selected guest.
: "${ROOT_LUKS_DEVICE:?Set ROOT_LUKS_DEVICE to the full /dev/... path from lsblk, without tree drawing characters}"
./bin/bootwright machine exec --context "$CONTEXT" --name "${HOSTS:?Set HOSTS to the guests being checked}" -- sudo -n clevis luks list -d "${ROOT_LUKS_DEVICE:?Set ROOT_LUKS_DEVICE as described above}"
```

Require a `tpm2` pin. If partition paths differ, repeat with `HOSTS` and `ROOT_LUKS_DEVICE` set
for each guest separately.

### Apply the cluster

In a second terminal, enter the evidence directory printed above and start
the memory sampler. Stop it with Ctrl-C when the cluster apply finishes:

```bash
read -r -p 'Evidence directory printed by this run: ' EVIDENCE_DIR
test -d "${EVIDENCE_DIR:?Enter the printed evidence directory}"
while sleep 10; do date +%s; grep MemAvailable /proc/meminfo; done > "$EVIDENCE_DIR/memavailable-clusters.txt"
```

In the original terminal, apply the cluster. For `TEST_MODE=interruption`,
press Ctrl-C once after the first Ceph block starts. For `TEST_MODE=measured`,
let the first apply finish without interruption:

```sh
date +%s > "$EVIDENCE_DIR/clusters-start.txt"
./bin/bootwright apply --context "$CONTEXT" --stage clusters --yes
date +%s > "$EVIDENCE_DIR/clusters-end.txt"
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage clusters --yes
record_budget b
```

In the measured run, the second apply must be unchanged; the two timestamps
give the clusters-stage wall time. In the interruption run, the second apply
must finish with the same operation ID and retained completed attempts. Keep
the frozen input and custody records unchanged. A missed interruption fails
that run's drill. Stop the sampler now.

<a id="verify"></a>

## 4. Verify the result

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright cluster info --context "$CONTEXT" --name ceph-e2e
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
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" health detail
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch ls --refresh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" orch ps --refresh --format json-pretty
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" osd tree
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" osd pool ls detail
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" mgr services
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" mon dump
OSD_ID='' # Numeric OSD ID from osd tree above; repeat for every OSD.
: "${OSD_ID:?Set OSD_ID from osd tree}"
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" config show "osd.${OSD_ID:?Set OSD_ID from osd tree}" osd_memory_target_autotune
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" config show "osd.${OSD_ID:?Set OSD_ID from osd tree}" osd_memory_target
```

Require 3 mons in quorum, 2 mgrs, 3 up and in SSD OSDs, every PG `active+clean`
and the pool at 2/2. For each OSD, autotuning is `false` and the target is
`4294967296`. Match the live FSID and image digest to `cluster info`. Record
any Call Home warning separately; it is not `HEALTH_OK`. Record the CephX key
type the cluster reports for a client, from `config dump` and the capability
check of [Key type](#key-type), without exporting a key.

### Key type

`config dump` and the capabilities show no key type. A CephX key's base64
encoding starts with a little-endian 16-bit type, `1` for AES and `2` for
AES256K, and carries the secret length in bytes 10 and 11, `16` for AES: the
check Bootwright makes before readiness ([managed Ceph](../../specs/ceph.md),
"Native auth keys must prove the declared AES type"). Decode it inside a
root-private scratch directory on the admin guest; only the type and length are
printed:

```sh
CLIENT="client.rbd-canary" # Repeat with CLIENT="client.admin" for the administrator.
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n bash -s -- "${CEPH_CONF:?Set CEPH_CONF as described above}" "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" "${CLIENT:?Set CLIENT to the client entity to check}" <<'KEYTYPE'
set -euo pipefail
umask 077
check_dir=$(mktemp -d /var/tmp/ceph-key-type.XXXXXXXX)
trap 'rm -f -- "$check_dir/auth.json"; rmdir -- "$check_dir"' EXIT
ceph --conf "$1" --keyring "$2" auth get "$3" --format json > "$check_dir/auth.json"
python3 -c 'import base64, json, struct, sys; d = base64.b64decode(json.load(open(sys.argv[1]))[0]["key"], validate=True); t, n = struct.unpack_from("<H", d, 0)[0], struct.unpack_from("<H", d, 10)[0]; print("cephx key type:", {1: "aes", 2: "aes256k"}.get(t, "unknown"), "secret bytes:", n); sys.exit(0 if (t, n, len(d)) == (1, 16, 28) else 1)' "$check_dir/auth.json"
KEYTYPE
```

Require `cephx key type: aes secret bytes: 16` and exit 0; repeat with
`CLIENT="client.admin"` selected in the assignment above.

### Pool client

The capability check never prints the key:

```sh
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF as described above}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" auth get client.rbd-canary --format json | python3 -c 'import json, sys; rows = json.load(sys.stdin); expected = {"mon": "profile rbd", "mgr": "profile rbd pool=rbd-data", "osd": "profile rbd pool=rbd-data"}; ok = len(rows) == 1 and rows[0].get("entity") == "client.rbd-canary" and rows[0].get("caps") == expected; print("pool client caps:", "OK" if ok else "FAIL"); sys.exit(0 if ok else 1)'
```

The data test runs as `--id rbd-canary` with its own keyring, copied to a
root-private scratch file on the admin guest, writes and compares 1 MiB and
grows a new retained image to 16 MiB. Run it once now, and repeat only the
read-back block below (export and verify the original first-MiB SHA256) after every reboot and
after the network recovery; FIPS configuration alone does not prove the
encrypted-root recovery or the access. Destroy removes the image.

```sh
# Keep the original nonsecret digest private across all recovery drills.
umask 077
RBD_DIGEST_FILE="${EVIDENCE_DIR:?Set EVIDENCE_DIR to the private evidence directory from Run}/rbd-client-sha256"
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n bash -s -- "${CEPH_CONF:?Set CEPH_CONF as described above}" "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" > "${RBD_DIGEST_FILE:?Set RBD_DIGEST_FILE in the private evidence directory}" <<'CHECK'
set -euo pipefail
umask 077
conf=$1
admin_keyring=$2
check_dir=$(mktemp -d /var/tmp/ceph-client-check.XXXXXXXX)
printf 'Scratch directory on admin guest: %s\n' "$check_dir" >&2
trap 'rm -f -- "$check_dir/client.keyring" "$check_dir/input" "$check_dir/output"; rmdir -- "$check_dir"' EXIT
ceph --conf "$conf" --keyring "$admin_keyring" auth get client.rbd-canary -o "$check_dir/client.keyring" >/dev/null
rbd --conf "$conf" --id rbd-canary --keyring "$check_dir/client.keyring" ls --pool rbd-data --format json | python3 -c 'import json, sys; sys.exit("client-canary" in json.load(sys.stdin))'
head -c 1048576 /dev/urandom > "$check_dir/input"
rbd --conf "$conf" --id rbd-canary --keyring "$check_dir/client.keyring" import "$check_dir/input" rbd-data/client-canary >&2
rbd --conf "$conf" --id rbd-canary --keyring "$check_dir/client.keyring" export rbd-data/client-canary "$check_dir/output" >&2
cmp -n 1048576 "$check_dir/output" "$check_dir/input"
rbd --conf "$conf" --id rbd-canary --keyring "$check_dir/client.keyring" resize --size 16M rbd-data/client-canary >&2
rbd --conf "$conf" --id rbd-canary --keyring "$check_dir/client.keyring" info rbd-data/client-canary >&2
sha256sum "$check_dir/input" | cut -d ' ' -f 1
CHECK
```

Require exit 0 and a 16 MiB image. An existing image stops the script before
writing; a lost response after the import is an unknown outcome, so inspect the
image; an unexpected failure fails the run.

Run this reusable read-only block after every reboot and network recovery;
it uses the digest saved by the initial write and never writes the image:

```sh
RBD_EXPECTED_SHA256="$(cat "${RBD_DIGEST_FILE:?Set RBD_DIGEST_FILE to the original private digest file}")"
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n bash -s -- "${CEPH_CONF:?Set CEPH_CONF as described above}" "${CEPH_KEYRING:?Set CEPH_KEYRING as described above}" "${RBD_EXPECTED_SHA256:?Read the original SHA256 from RBD_DIGEST_FILE}" <<'READBACK'
set -euo pipefail
umask 077
check_dir=$(mktemp -d /var/tmp/ceph-client-readback.XXXXXXXX)
trap 'rm -f -- "$check_dir/client.keyring" "$check_dir/output"; rmdir -- "$check_dir"' EXIT
ceph --conf "$1" --keyring "$2" auth get client.rbd-canary -o "$check_dir/client.keyring" >/dev/null
rbd --conf "$1" --id rbd-canary --keyring "$check_dir/client.keyring" export rbd-data/client-canary "$check_dir/output"
actual=$(head -c 1048576 "$check_dir/output" | sha256sum | cut -d ' ' -f 1)
[ "$actual" = "$3" ]
printf '%s\n' 'Original first-MiB SHA256: OK'
READBACK
```

## 5. Run the acceptance drills

### One-host outage

Confirm `rhel-03` is not the admin node. A 2/2 pool can block I/O when a
required replica is down; do not lower `min_size`.

```sh
./bin/bootwright machine stop --context "$CONTEXT" --name rhel-03 --yes
```

Collect health, mon quorum, PG availability and the pool-client readback result
within 300 seconds. Then start the guest:

```sh
./bin/bootwright machine start --context "$CONTEXT" --name rhel-03
```

Require recovery within 600 seconds, with all OSDs up and in and every PG
`active+clean`. A timeout fails the qualification.

### Bond failover, reboot and recovery

Fail the active virtual NIC of `rhel-02` and prove the bond fails over, then
restore it:

```sh
virsh --connect qemu:///system domiflist bootwright-lab-3-nodes-ceph-host-variants-rhel-02
ACTIVE_VNET='' # Target interface from domiflist matching the bond's active NIC.
: "${ACTIVE_VNET:?Set ACTIVE_VNET after matching domiflist to bond0}"
virsh --connect qemu:///system domif-setlink bootwright-lab-3-nodes-ceph-host-variants-rhel-02 "${ACTIVE_VNET:?Set ACTIVE_VNET as described above}" down
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-02 -- cat /proc/net/bonding/bond0
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-02 -- ping -c 3 -I bond0.140 203.0.113.11
virsh --connect qemu:///system domif-setlink bootwright-lab-3-nodes-ceph-host-variants-rhel-02 "${ACTIVE_VNET:?Set ACTIVE_VNET as described above}" up
```

Require the other NIC to become the active member and the VLAN peer to answer.
Then reboot the guests one at a time (`rhel-02`, `rhel-03`, then `rhel-01`),
never two together, waiting for `HEALTH_OK` between them:

```sh
REBOOT_GUEST="rhel-02" # Repeat with rhel-03, then rhel-01, after full recovery each time.
./bin/bootwright machine restart --context "$CONTEXT" --name "${REBOOT_GUEST:?Select the next guest in the documented reboot order}" --yes
./bin/bootwright machine exec --context "$CONTEXT" --name "${REBOOT_GUEST:?Select the guest just restarted}" -- uptime      # repeat until SSH answers
```

`restart` returns at power-on, before the OS boots. Require SSH, the VLAN
connectivity and the unattended TPM2 root unlock, then repeat the host checks
in [Check the installed hosts](#check-the-installed-hosts) and the pool client read-back. Test the recovery passphrase in a controlled
console exercise on one guest: at
`virsh --connect qemu:///system console bootwright-lab-3-nodes-ceph-host-variants-rhel-03`,
run the following in that guest console and type the `luks-recovery` value:

```sh
ROOT_LUKS_DEVICE='' # Root crypto_LUKS partition from lsblk on this guest.
: "${ROOT_LUKS_DEVICE:?Set ROOT_LUKS_DEVICE from lsblk}"
sudo cryptsetup open --test-passphrase "${ROOT_LUKS_DEVICE:?Set ROOT_LUKS_DEVICE as described above}"
```

The recovery passphrase must unlock a slot other than the clevis one. Never
record the passphrase.

### Replay

```sh
./bin/bootwright apply --context "$CONTEXT" --yes
```

It settles without effect. This proves replay from records, not live repair.

### Changed intent is refused

Change only the rendered pool document, retaining its exact original bytes in
the private evidence directory for restoration. The backup refuses an existing
file, and the edit refuses anything other than one expected autoscale stanza:

```sh
umask 077
INTENT_BACKUP="${EVIDENCE_DIR:?Set EVIDENCE_DIR to the private evidence directory from Run}/pool-rbd-data.original.yaml"
python3 - "${INPUT_DIR:?Set INPUT_DIR to the private rendered input directory}/storage/pool-rbd-data.yaml" "${INTENT_BACKUP:?Set INTENT_BACKUP in the private evidence directory}" <<'CHANGE_INTENT'
from pathlib import Path
import sys

pool = Path(sys.argv[1])
backup = Path(sys.argv[2])
original = pool.read_bytes()
expected = b"  autoscale:\n    mode: 'on'\n"
if original.count(expected) != 1:
    raise SystemExit("Expected exactly one original autoscale stanza; no edit made")
with backup.open("xb") as saved:
    saved.write(original)
pool.write_bytes(original.replace(expected, b"  autoscale:\n    mode: 'off'\n", 1))
CHANGE_INTENT
```

Then validate, update and prove the refusal:

```sh
./bin/bootwright validate -f "$INPUT_DIR"
./bin/bootwright context update --name "$CONTEXT" --input-dir "$INPUT_DIR" --yes
./bin/bootwright apply --context "$CONTEXT" --yes
```

The update itself is allowed and retains its completed-apply warning. The apply
must refuse the changed frozen intent before any native effect. Restore the
exact saved document and update again before destroying:

```sh
python3 - "${INPUT_DIR:?Set INPUT_DIR to the private rendered input directory}/storage/pool-rbd-data.yaml" "${INTENT_BACKUP:?Set INTENT_BACKUP to the original private backup}" <<'RESTORE_INTENT'
from pathlib import Path
import sys

pool = Path(sys.argv[1])
backup = Path(sys.argv[2])
original = backup.read_bytes()
expected = b"  autoscale:\n    mode: 'on'\n"
if original.count(expected) != 1:
    raise SystemExit("Backup does not contain exactly one original autoscale stanza")
pool.write_bytes(original)
RESTORE_INTENT
./bin/bootwright validate -f "$INPUT_DIR"
./bin/bootwright context update --name "$CONTEXT" --input-dir "$INPUT_DIR" --yes
```

### Measured budget

Take measurement (c) now, after the client, outage and reboot drills. The
captures (a) and (b), timestamps and sampler were collected in step 3.

```sh
record_budget c
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF from cluster info}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING from cluster info}" orch ps --format json-pretty
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node rhel-01 -- sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF from cluster info}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING from cluster info}" df
```

Use the measured run's captures for the budget fields. Keep the interruption
run's evidence separate.

Record every field of the matrix row: `Guests` (3), `Declared vCPU` (12),
`Declared RAM GiB` (36), `Declared disk GiB` (420), `MemAvailable at (a)/(b)/(c)`,
`Minimum MemAvailable during clusters stage`, `Allocated vs apparent disk bytes`,
`Per-guest balloon RSS`, `Daemons by type`, `Clusters-stage wall time` and
`OOM kills`. Count the `Out of memory` lines of each guest's journal over the
run; any is a failed row:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name rhel-01,rhel-02,rhel-03 -- sudo -n journalctl -k --no-pager
```

<a id="destroy"></a>

## 6. Destroy and clean up

Retain the evidence and data you need first. The first destroy unregisters the
running guests from RHSM, then pauses and names the stop of the whole group. Stop all three and
finish removal:

```sh
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright machine stop --context "$CONTEXT" --name rhel-01,rhel-02,rhel-03 --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright cluster info --context "$CONTEXT" --name ceph-e2e
virsh --connect qemu:///system list --all
virsh --connect qemu:///system net-list --all
```

Prove the owned domains, disks and network are absent before deleting the
context:

```sh
./bin/bootwright context delete --name "$CONTEXT" --purge --yes
```

Complete [Restore listener firewall](../README.md#restore-listener-firewall),
then [Remove diagnostic tools](../README.md#remove-diagnostic-tools). Delete
the scratch and rendered input only after both succeed. Keep `EVIDENCE_DIR`:

```sh
rm -rf -- "${RUN_DIR:?Set RUN_DIR from step 3}" "${INPUT_DIR:?Set INPUT_DIR from step 3}"
```

## 7. Record the result

Record the mode, build identity, checks and completed cleanup in the private
evidence. A successful run completes every numbered step. The P5 row needs
both complete runs, with performance figures only from `TEST_MODE=measured`
and interruption evidence only from `TEST_MODE=interruption`. Keep the row
as observation until the acceptance baseline described above is registered.
Shared media and the private evidence remain.

The row for the ledger:

```text
| <YYYY-MM-DD> | Commit <sha>; Source clean | Retained locally | e2e/lab-3-nodes-ceph-host-variants (P5) with the lab conventions only | Retained locally | P5 matrix qualification: host and security variants (active-backup bond over 2 NICs, VLAN cluster network over the bond, final NMState, RHSM registration, FIPS without OSPP, CephX AES, LUKS root with TPM2 unlock and recovery, reboot access), one explicit data disk, explicit 2/2 pool, pool client access, one-host outage, interruption, replay, changed intent and removal | Retained locally | observation, not acceptance |
```
