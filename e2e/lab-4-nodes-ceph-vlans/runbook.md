# lab-4-nodes-ceph-vlans

IBM Storage Ceph on three RHEL 9.8 guests whose public (client) and cluster
(replication) networks are separate VLANs, plus a fourth guest, `app-01`, on
the Ceph public network and on a separate user network. `app-01` runs a sample
HTTP server that answers only on the user network and stores each request in
block (RBD), file (CephFS) and object (RGW S3) storage over the Ceph public
network. A client container on this host calls the server over the user
network. Context `lab-4-nodes-ceph-vlans`; 4 guests, 15 vCPUs, 22 GiB.

```text
                    this host (controller, router for VLAN 110 only, services on CONTROLLER_ADDRESS)
          ┌────────────────────────────┴─────────────────────────────┐
  br-ceph (VLAN trunk, MTU 9000, no host address)            br-user (untagged)
   ├ br-ceph.110  198.51.100.1/26  Ceph public, gateway        198.51.100.65/26  user network
   └ VLAN 120     203.0.113.0/26   Ceph cluster: no host leg, unrouted

  ceph-01  nic0.110 198.51.100.11  nic0.120 203.0.113.11   MON OSD RGW
  ceph-02  nic0.110 198.51.100.12  nic0.120 203.0.113.12   MON OSD RGW ingress
  ceph-03  nic0.110 198.51.100.13  nic0.120 203.0.113.13   MON OSD ingress
  (2 MGRs and 2 MDSs on two of the three, cephadm's choice)
  app-01   nic0.110 198.51.100.21  nic1     198.51.100.70  sample server :8080 on 198.51.100.70 only
  RGW ingress VIP 198.51.100.60/26 (TLS 443, name s3.e2e.example.test, no DNS record)
  client container on this host: podman --network host -> 198.51.100.70:8080
```

| Network | Object | Carriage | Members | Purpose |
| --- | --- | --- | --- | --- |
| Ceph public | `ceph-public`, VLAN 110, 198.51.100.0/26, MTU 1500 | tagged on segment `ceph-trunk` | 3 Ceph hosts, `app-01`, host leg `br-ceph.110` | install, SSH, MON, OSD front, RGW ingress |
| Ceph cluster | `ceph-cluster`, VLAN 120, 203.0.113.0/26, MTU 9000 | tagged on segment `ceph-trunk` (capacity 9000) | 3 Ceph hosts only | OSD replication and heartbeats |
| User | `app-users`, 198.51.100.64/26 | untagged on segment `user-lan` | `app-01` `nic1`, host `br-user` | HTTP to the sample server |

Every network is `provided: true` on an operator-created bridge, so this case
exercises the [segment carriage](../../specs/api/machines.md#machine-profiles-and-segments),
[VLAN agreement, carriage and capacity checks](../../specs/api/networks.md#cross-kind-checks),
VLAN install interfaces, a NIC per segment and the
[Ceph network checks](../../specs/api/networks.md#cross-kind-checks) of X323
and X324 on live guests. It accepts no milestone item; it was added at the
owner's request of 2026-11-08 as a live run of that networking batch with a
real application client.

The host routes only VLAN 110 and masquerades it to the internet, because the
guests fetch IBM packages and images directly. A runtime nftables table keeps
the host from forwarding to or from the user network and towards the cluster
network, so the only paths between them are the ones the guests own.

Run every numbered step in order in one Bash session from the checkout under
test. An unexpected failure fails the run; retain its private records for
cleanup and start again from the beginning on a clean host.

## 1. Choose the inputs

[values.yaml](values.yaml) holds the addresses, VLAN ids, guest sizes and the
IBM Storage Ceph 9.9.2.0 coordinates. Another release moves the fields the
[lab-3-nodes-ceph release table](../lab-3-nodes-ceph/runbook.md#releases-b626)
names together; put them in a private values file. The commands below use the
default addresses: change these variables with any address override.

```bash
VALUES_FILE='' # Optional private YAML file with other release coordinates and image digests.
IBM_REGISTRY_FILE="$HOME/ibm-ceph-registry.txt"
CEPH_BRIDGE=br-ceph; USER_BRIDGE=br-user
PUBLIC_VLAN=110; PUBLIC_GATEWAY=198.51.100.1/26; PUBLIC_CIDR=198.51.100.0/26
CLUSTER_CIDR=203.0.113.0/26
USER_HOST_ADDRESS=198.51.100.65/26; USER_CIDR=198.51.100.64/26
APP_PUBLIC=198.51.100.21; APP_USER=198.51.100.70; APP_PORT=8080
OBJECT_VIP=198.51.100.60; DOMAIN=e2e.example.test
CLIENT_IMAGE=registry.access.redhat.com/ubi9/python-312:latest
CASE_DIR="$PWD/e2e/lab-4-nodes-ceph-vlans"
```

## 2. Prepare the host

Complete [host preparation](../README.md#prepare-the-host), including the RHEL
9.8 boot and DVD media and the IBM entitlement key file, then return here.
This case also needs `tcpdump` for the segregation capture and `podman` for
the client container; record a missing `tcpdump` for removal with the other
diagnostic packages:

```bash
if ! rpm -q tcpdump >/dev/null; then
    printf 'tcpdump\n' >> "${HOST_CHECKS:?Run the diagnostic tools step of the host preparation first}/added"
    sudo dnf install --setopt=install_weak_deps=False tcpdump
fi
command -v tcpdump podman python3
```

`controller install` provides `podman`. The
[capacity gate](../../specs/substrates.md#capacity-admission) counts three
Ceph guests (4 vCPU, 6 GiB, 35 GiB root and a 25 GiB OSD disk each), `app-01`
(2 vCPU, 2 GiB, 20 GiB) and the reserve (1 vCPU, 2 GiB, 10 GiB): 15 host CPUs,
22 GiB of `MemAvailable` and 210 GiB free under `/var/lib/libvirt`. Close
large applications first. The host's ports 53, 123, 3128, 8080 and 8443 on
its address and loopback 8000 to 8003 must be free. No other case may be
applied: this one uses 198.51.100.0/25 and 203.0.113.0/26. Require no output
from each check:

```sh
ip -br link | grep -E "^($CEPH_BRIDGE|$USER_BRIDGE)\b"
ip -4 route | grep -E '198\.51\.100\.|203\.0\.113\.'
sudo virsh --connect qemu:///system list --all --name | grep bootwright-
```

## 3. Build the lab fabric

Both bridges have no physical port and sit in firewalld's `trusted` zone, so
guests reach the host's services over VLAN 110. `br-ceph` carries tagged
frames only; its one host leg is VLAN 110. Nothing on the host joins VLAN 120.
Bootwright proves the bridges present and never creates or removes them.

```sh
sudo nmcli connection add type bridge ifname "$CEPH_BRIDGE" con-name "$CEPH_BRIDGE" bridge.stp no ethernet.mtu 9000 ipv4.method disabled ipv6.method disabled connection.zone trusted
sudo nmcli connection add type vlan ifname "$CEPH_BRIDGE.$PUBLIC_VLAN" con-name "$CEPH_BRIDGE.$PUBLIC_VLAN" vlan.parent "$CEPH_BRIDGE" vlan.id "$PUBLIC_VLAN" ethernet.mtu 1500 ipv4.method manual ipv4.addresses "$PUBLIC_GATEWAY" ipv6.method disabled connection.zone trusted
sudo nmcli connection add type bridge ifname "$USER_BRIDGE" con-name "$USER_BRIDGE" bridge.stp no ipv4.method manual ipv4.addresses "$USER_HOST_ADDRESS" ipv6.method disabled connection.zone trusted
for c in "$CEPH_BRIDGE" "$CEPH_BRIDGE.$PUBLIC_VLAN" "$USER_BRIDGE"; do sudo nmcli connection up "$c"; done
ip -d link show "$CEPH_BRIDGE" | head -n1; ip -br address | grep -E "$CEPH_BRIDGE|$USER_BRIDGE"
for i in "$CEPH_BRIDGE.$PUBLIC_VLAN" "$USER_BRIDGE"; do sudo firewall-cmd --get-zone-of-interface="$i"; done   # trusted x2
```

Until guests attach, the bridges show `NO-CARRIER` and their routes
`linkdown`. Then routing, egress NAT and the forwarding fence, all runtime,
recording what already existed:

```bash
umask 077
RUN_DIR=$(mktemp -d); printf 'Run directory: %s\n' "$RUN_DIR"
UPLINK=$(ip -4 route show default | sed -n 's/.* dev \([^ ]*\).*/\1/p' | head -n1); printf 'Uplink: %s\n' "$UPLINK"
if [ "$(sysctl -n net.ipv4.ip_forward)" = 1 ]; then echo pre-existing > "$RUN_DIR/ip_forward"; else sudo sysctl -w net.ipv4.ip_forward=1 && echo added > "$RUN_DIR/ip_forward"; fi
UPLINK_ZONE=$(sudo firewall-cmd --get-zone-of-interface="$UPLINK") || UPLINK_ZONE=$(sudo firewall-cmd --get-default-zone)
printf '%s\n' "$UPLINK_ZONE" > "$RUN_DIR/uplink-zone"; printf 'Uplink zone: %s\n' "$UPLINK_ZONE"
if sudo firewall-cmd --zone="$UPLINK_ZONE" --query-masquerade; then echo pre-existing > "$RUN_DIR/masquerade"; else sudo firewall-cmd --zone="$UPLINK_ZONE" --add-masquerade && echo added > "$RUN_DIR/masquerade"; fi
cat > "$RUN_DIR/fence.nft" <<EOF
table inet bootwright_e2e_fence {
    chain forward {
        type filter hook forward priority -10; policy accept;
        iifname "$USER_BRIDGE" counter drop
        oifname "$USER_BRIDGE" counter drop
        iifname "$CEPH_BRIDGE.$PUBLIC_VLAN" ip daddr $CLUSTER_CIDR counter drop
    }
    chain input {
        type filter hook input priority -10; policy accept;
        iifname != { "lo", "$USER_BRIDGE" } ip daddr ${USER_HOST_ADDRESS%/*} counter drop
    }
}
EOF
sudo nft -c -f "$RUN_DIR/fence.nft" && sudo nft -f "$RUN_DIR/fence.nft"
sudo nft list table inet bootwright_e2e_fence
```

The input rule stops a guest from reaching the host's user-network address
over VLAN 110: Linux otherwise answers for any local address on any
interface. The cluster rule matches only traffic routed in from the VLAN 110
leg, so bridged replication frames are never matched even when another
workload loads `br_netfilter`.

An uplink in no zone takes the default zone. Require the fence listing to show
both chains. A firewalld reload drops the masquerade but not the fence; do not
reload during the run.

## 4. Create and apply the context

```bash
RENDER_ARGS=()
if [ -n "$VALUES_FILE" ]; then RENDER_ARGS+=(--values "$VALUES_FILE"); fi
e2e/render.sh lab-4-nodes-ceph-vlans "${RENDER_ARGS[@]}"
CONTEXT="lab-4-nodes-ceph-vlans"; INPUT_DIR="$HOME/bootwright-e2e/lab-4-nodes-ceph-vlans"
CONTROLLER_ADDRESS=$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')
grep -h -A1 'name: ip$' "$INPUT_DIR/infra/machines/controller.yaml"   # must show CONTROLLER_ADDRESS
./bin/bootwright validate -f "$INPUT_DIR"
./bin/bootwright context init --name "$CONTEXT" --input-dir "$INPUT_DIR"
./bin/bootwright secret generate --context "$CONTEXT"
install -m 0600 /dev/null "$RUN_DIR/ibm-password"
cut -d: -f2- "${IBM_REGISTRY_FILE:?Set IBM_REGISTRY_FILE to the IBM entitlement key file}" > "$RUN_DIR/ibm-password"
./bin/bootwright secret set --context "$CONTEXT" --name ibm-registry --username cp --password-file "$RUN_DIR/ibm-password" --yes
rm -f -- "$RUN_DIR/ibm-password"
./bin/bootwright secret check --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT"
```

The plan has no `bootwright-<context>-*` libvirt network: every Network is
provided, so the provider host block only proves `br-ceph` and `br-user`
present. Then apply by stage:

```sh
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
```

```bash
FIREWALL_PORTS=(53/tcp 53/udp 123/udp 3128/tcp 8080/tcp 8443/tcp)
```

Complete [Prepare listener firewall](../README.md#prepare-listener-firewall),
then require `ready` before starting services:

```sh
./bin/bootwright preflight controller --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services,substrates --yes
./bin/bootwright apply --context "$CONTEXT" --stage machines --yes
./bin/bootwright machine list --context "$CONTEXT" --power-status
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01,ceph-02,ceph-03,app-01 -- ip -br address
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01,ceph-02,ceph-03,app-01 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright preflight storage-cluster --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT" --stage clusters
./bin/bootwright apply --context "$CONTEXT" --stage clusters --yes
./bin/bootwright status --context "$CONTEXT"
```

Each guest installs over `nic0.110`. Require on each Ceph host `nic0.110` and
`nic0.120` with their addresses and no address on `nic0`; on `app-01`
`nic0.110` and `nic1`, and no 203.0.113.0/26 address. The IBM hosts must
resolve on every guest.

## 5. Verify the cluster

```sh
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright cluster info --context "$CONTEXT" --name ceph-e2e
```

Copy the configuration and administrator keyring paths `cluster info` prints
in `cephCommand`:

```bash
CEPH_CONF='' # Configuration path printed by cluster info.
CEPH_KEYRING='' # Administrator keyring path printed by cluster info.
ceph_admin() {
    ./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node ceph-01 -- \
        sudo -n ceph --conf "${CEPH_CONF:?Set CEPH_CONF from cluster info}" --keyring "${CEPH_KEYRING:?Set CEPH_KEYRING from cluster info}" "$@"
}
ceph_admin -s
ceph_admin osd tree
ceph_admin fs status files
ceph_admin orch ps --refresh
```

Require `HEALTH_OK` (or only `CHA_ERROR_SENDING_REPORT`), 3 mons, an active
and a standby mgr, 3 OSDs up and in, `files` rank 0 active with one standby,
two running `rgw.objects` daemons and the `ingress.rgw.objects` haproxy and
keepalived daemons on `ceph-02` and `ceph-03`.

## 6. Deploy the sample server

Gather the bundle `app-01` needs in a private directory: the sample server and
its scripts, the cluster's minimal configuration, the subvolume path, the two
client keyrings, the object user's keys and the ingress certificate. Key
material stays in files; nothing below prints it.

```bash
BUNDLE="$RUN_DIR/e2e-sample"; mkdir -m 0700 "$BUNDLE"
cp "$CASE_DIR/app/sample-server.py" "$CASE_DIR/app/app-setup.sh" "$CASE_DIR/app/app-teardown.sh" "$BUNDLE/"
cat > "$BUNDLE/sample.env" <<EOF
USER_ADDRESS=$APP_USER
PORT=$APP_PORT
USER_CIDR=$USER_CIDR
RBD_POOL=rbd-data
RBD_IMAGE=app-disk
BLOCK_CLIENT=app-block
FILE_CLIENT=app-files
FS_NAME=files
S3_VIP=$OBJECT_VIP
S3_HOST=s3.$DOMAIN
S3_BUCKET=app-bucket
S3_REGION=objects
EOF
ceph_admin config generate-minimal-conf > "$BUNDLE/ceph.conf"
ceph_admin fs subvolume getpath files app-files --group_name apps | tr -d '[:space:]' > "$BUNDLE/cephfs-path"
./bin/bootwright cluster keyring --context "$CONTEXT" --name ceph-e2e --client app-block > "$BUNDLE/app-block.keyring"
./bin/bootwright cluster keyring --context "$CONTEXT" --name ceph-e2e --client app-files > "$BUNDLE/app-files.keyring"
./bin/bootwright secret show --context "$CONTEXT" --name rgw-objects-tls --part certificate > "$BUNDLE/rgw-ca.pem"
./bin/bootwright cluster info --context "$CONTEXT" --name ceph-e2e --object-user app-objects --output json > "$RUN_DIR/object-access.json"
python3 -I - "$RUN_DIR/object-access.json" "$BUNDLE/s3.json" <<'PY'
import json, sys
def find(o):
    if isinstance(o, dict):
        if "objectUsers" in o:
            return o["objectUsers"]
        o = list(o.values())
    for v in o if isinstance(o, list) else []:
        r = find(v)
        if r:
            return r
user = find(json.load(open(sys.argv[1])))[0]
json.dump({"accessKey": user["accessKey"], "secretKey": user["secretKey"]}, open(sys.argv[2], "w"))
PY
rm -f -- "$RUN_DIR/object-access.json"
grep -c '^[[:space:]]*mon_host' "$BUNDLE/ceph.conf"; cat "$BUNDLE/cephfs-path"; echo
wc -c "$BUNDLE"/*.keyring "$BUNDLE/s3.json"
```

Require one `mon_host` line naming the three 198.51.100.0/26 MONs, a path
under `/volumes/apps/app-files/`, and nonzero byte counts. Copy the bundle and
attach `app-01`:

```sh
./bin/bootwright machine cp --context "$CONTEXT" --name app-01 "local:$BUNDLE" remote:e2e-sample
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n bash e2e-sample/app-setup.sh
```

The script maps `rbd-data/app-disk` with the kernel RBD client and formats it
XFS on first use, mounts the `app-files` subvolume with the kernel CephFS
client, admits port 8080 from 198.51.100.64/26 only, starts the transient unit
`e2e-sample-server` and prints `/healthz`. Require every store `true` and every
`sourceAddress` entry `198.51.100.21`: `app-01` reaches the MONs and the
object VIP from its Ceph public address.

## 7. Run the client

The client runs in a container on this host's network and reaches the server
through `br-user`. It puts 4 KiB, 1 MiB and 8 MiB of random bytes into each
store, reads each back and compares, overwrites one, deletes them and requires
a 404 afterwards:

```sh
podman pull "$CLIENT_IMAGE"
podman run --rm -i --network host "$CLIENT_IMAGE" python3 - "http://$APP_USER:$APP_PORT" roundtrip "$APP_PUBLIC" < "$CASE_DIR/app/client.py"
```

Require `0 failed`. Then prove the data lives in Ceph, not on `app-01`: write
a canary into each store, detach the stores, power-cycle the guest, attach
them again and read the canaries back:

```sh
podman run --rm -i --network host "$CLIENT_IMAGE" python3 - "http://$APP_USER:$APP_PORT" write-canary "$APP_PUBLIC" < "$CASE_DIR/app/client.py"
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n bash e2e-sample/app-teardown.sh
./bin/bootwright machine stop --context "$CONTEXT" --name app-01 --yes
./bin/bootwright machine start --context "$CONTEXT" --name app-01
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- uptime      # repeat until SSH answers
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n bash e2e-sample/app-setup.sh
podman run --rm -i --network host "$CLIENT_IMAGE" python3 - "http://$APP_USER:$APP_PORT" check-canary "$APP_PUBLIC" < "$CASE_DIR/app/client.py"
```

Require `0 failed` from each. Keep the canaries for the next step.

## 8. Validate the segregation

### Ceph uses the declared networks

```bash
ceph_admin config get mon public_network
ceph_admin config get osd cluster_network
ceph_admin mon dump --format json > "$RUN_DIR/mon-dump.json"
ceph_admin osd dump --format json > "$RUN_DIR/osd-dump.json"
python3 -I - "$RUN_DIR/mon-dump.json" "$RUN_DIR/osd-dump.json" "$PUBLIC_CIDR" "$CLUSTER_CIDR" <<'PY'
import ipaddress, json, sys
mons, osds = json.load(open(sys.argv[1])), json.load(open(sys.argv[2]))
public, cluster = ipaddress.ip_network(sys.argv[3]), ipaddress.ip_network(sys.argv[4])
def ips(vec):
    return [ipaddress.ip_address(a["addr"].rsplit(":", 1)[0]) for a in vec["addrvec"]]
failures = []
for mon in mons["mons"]:
    for ip in ips(mon["public_addrs"]):
        print(f"mon.{mon['name']:<10} public  {ip}")
        failures += [f"mon.{mon['name']} {ip}"] if ip not in public else []
for osd in osds["osds"]:
    for kind, net in (("public_addrs", public), ("cluster_addrs", cluster),
                      ("heartbeat_back_addrs", cluster), ("heartbeat_front_addrs", public)):
        for ip in ips(osd[kind]):
            print(f"osd.{osd['osd']:<10} {kind:<22} {ip}")
            failures += [f"osd.{osd['osd']} {kind} {ip}"] if ip not in net else []
print("FAIL: " + ", ".join(failures) if failures else "every MON and OSD address is on its declared network")
sys.exit(1 if failures else 0)
PY
```

Require `198.51.100.0/26`, `203.0.113.0/26` and the closing line: client and
front-heartbeat traffic on the public VLAN, replication and back-heartbeat
traffic on the cluster VLAN.

### Interfaces, VLAN tags and MTUs

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01,ceph-02,ceph-03,app-01 -- ip -d -o link show type vlan
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01,ceph-02,ceph-03,app-01 -- ip -4 -br address
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ip route get 198.51.100.11
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ip route get 198.51.100.65
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sysctl net.ipv4.ip_forward
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n ss -Htlnp sport = :8080
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01 -- ping -c 3 -M do -s 8972 203.0.113.12
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01 -- ping -c 3 -M do -s 1472 198.51.100.12
```

Require `vlan protocol 802.1Q id 110` on `nic0.110` of every guest and `id 120`
on `nic0.120` of the Ceph hosts only, MTU 9000 on `nic0` and `nic0.120` of the
Ceph hosts and 1500 on every `nic0.110`; `app-01` routes the MONs via
`nic0.110` and the host's user address via `nic1`, does not forward
(`= 0`) and listens on 8080 only on 198.51.100.70. The jumbo ping with
`-M do` (no fragmentation) succeeds on the cluster VLAN, and the 1500-byte
ping on the public VLAN.

### What cannot reach what

The host holds no address on the cluster network and routes it only by its
default route, off the fabric:

```sh
ip -4 -br address | grep -c '203\.0\.113\.'      # 0
ip route get 203.0.113.11                        # via the default gateway on the uplink, not a bridge
```

Each probe below must fail. The sessions are proved first, so a probe's
failure is the network's answer, never a broken session; an exit of 255 is a
session failure and stops the check. The fence counters are reset first, and
the three guest attempts must raise them:

```bash
./bin/bootwright machine exec --context "$CONTEXT" --name app-01,ceph-01 -- true
sudo nft reset counters table inet bootwright_e2e_fence >/dev/null
expect_fail() {
    "$@"; rc=$?
    if [ "$rc" -eq 0 ]; then printf 'FAIL: reached: %s\n' "$*"; return 1; fi
    if [ "$rc" -eq 255 ]; then printf 'ERROR: the session failed, nothing was tested: %s\n' "$*"; return 2; fi
    printf 'OK, refused (exit %s): %s\n' "$rc" "$*"
}
expect_fail curl -sS --noproxy '*' --max-time 5 -o /dev/null "http://198.51.100.21:$APP_PORT/healthz"         # host to the server on its Ceph address
expect_fail ./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ping -c 2 -W 2 203.0.113.11   # app-01 to the cluster VLAN
expect_fail ./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01 -- curl -sS --noproxy '*' --max-time 5 -o /dev/null "http://$APP_USER:$APP_PORT/healthz"   # a Ceph host to the user network
expect_fail ./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01 -- ping -c 2 -W 2 198.51.100.65 # a Ceph host to the host's user leg
curl -fsS --noproxy '*' --max-time 5 "http://$APP_USER:$APP_PORT/healthz"                                       # host to the server on the user network: answers
sudo nft list table inet bootwright_e2e_fence
```

Require nonzero packet counters on `oifname "br-user"` (the Ceph host's
request towards the user network), on the `203.0.113.0/26` rule (`app-01`'s
ping) and on the input rule (the Ceph host's ping of the host's user
address). The request to 198.51.100.21:8080 is refused because nothing
listens there and the guest firewall admits 8080 only from the user network;
`app-01` would also refuse the Ceph host's request on its own, so only the
counter proves the host does not route it.

### Traffic on the wire

Capture both bridges while the client runs once more. Bridge capture in
promiscuous mode sees the guests' frames with their 802.1Q tags:

```bash
sudo -v
sudo timeout 150 tcpdump -i "$CEPH_BRIDGE" -nn -s 128 -Z root -w "$RUN_DIR/ceph-trunk.pcap" &
sudo timeout 150 tcpdump -i "$USER_BRIDGE" -nn -s 128 -Z root -w "$RUN_DIR/user-lan.pcap" &
sleep 5
podman run --rm -i --network host "$CLIENT_IMAGE" python3 - "http://$APP_USER:$APP_PORT" roundtrip "$APP_PUBLIC" < "$CASE_DIR/app/client.py"
wait
count() { sudo tcpdump -r "$1" -nn "$2" 2>/dev/null | wc -l; }
T="$RUN_DIR/ceph-trunk.pcap"; U="$RUN_DIR/user-lan.pcap"
printf '%-72s %s\n' \
  'trunk: replication on VLAN 120 (OSD ports)            want > 0' "$(count "$T" 'vlan 120 and tcp portrange 6800-7568')" \
  'trunk: VLAN 120 with an address outside 203.0.113.0/26  want 0' "$(count "$T" "vlan 120 and ip and not (src net $CLUSTER_CIDR and dst net $CLUSTER_CIDR)")" \
  'trunk: app-01 to the MONs on VLAN 110                  want > 0' "$(count "$T" "vlan $PUBLIC_VLAN and host $APP_PUBLIC and (tcp port 6789 or tcp port 3300)")" \
  'trunk: app-01 to the OSDs on VLAN 110                  want > 0' "$(count "$T" "vlan $PUBLIC_VLAN and host $APP_PUBLIC and tcp portrange 6800-7568")" \
  'trunk: app-01 to the object VIP on VLAN 110            want > 0' "$(count "$T" "vlan $PUBLIC_VLAN and host $APP_PUBLIC and host $OBJECT_VIP and tcp port 443")" \
  'trunk: a user-network address anywhere                   want 0' "$(count "$T" "vlan and (host $APP_USER or host ${USER_HOST_ADDRESS%/*})")" \
  'trunk: untagged IPv4                                     want 0' "$(count "$T" 'not vlan and ip')" \
  'user:  HTTP between the host and app-01                 want > 0' "$(count "$U" "tcp port $APP_PORT and host ${USER_HOST_ADDRESS%/*} and host $APP_USER")" \
  'user:  any other unicast IPv4 conversation               want 0' "$(count "$U" "ip and not ip multicast and not ip broadcast and not (host ${USER_HOST_ADDRESS%/*} and host $APP_USER)")" \
  'user:  multicast and broadcast (host mDNS, IGMP)   information' "$(count "$U" 'ip multicast or ip broadcast')" \
  'user:  Ceph ports (MON, OSD, S3)                         want 0' "$(count "$U" 'tcp port 6789 or tcp port 3300 or tcp portrange 6800-7568 or tcp port 443')" \
  'user:  any 802.1Q frame                                  want 0' "$(count "$U" 'vlan')"
```

Require every count as marked. Then remove the canaries:

```sh
podman run --rm -i --network host "$CLIENT_IMAGE" python3 - "http://$APP_USER:$APP_PORT" clean-canary "$APP_PUBLIC" < "$CASE_DIR/app/client.py"
```

## 9. Destroy and clean up

Detach `app-01` so no RBD watcher or CephFS session holds the stores, then
destroy as the plan directs. With the guests running, destroy pauses before
removal and names the guests to stop; the repeated destroy continues the same
operation:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n bash e2e-sample/app-teardown.sh
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright machine stop --context "$CONTEXT" --name ceph-01,ceph-02,ceph-03,app-01 --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright status --context "$CONTEXT"
sudo virsh --connect qemu:///system list --all
```

Require no domain or disk of the context to remain. Then:

```sh
EVIDENCE_CONTEXT="$CONTEXT"
```

Complete [Preserve evidence before context purge](../README.md#preserve-evidence-before-context-purge),
then delete the context:

```sh
./bin/bootwright context delete --name "$CONTEXT" --purge --yes
```

Remove the fabric this run made, restoring only what it added:

```bash
sudo nft delete table inet bootwright_e2e_fence
if [ "$(cat "$RUN_DIR/masquerade")" = added ]; then sudo firewall-cmd --zone="$(cat "$RUN_DIR/uplink-zone")" --remove-masquerade; fi
if [ "$(cat "$RUN_DIR/ip_forward")" = added ]; then sudo sysctl -w net.ipv4.ip_forward=0; fi
for c in "$USER_BRIDGE" "$CEPH_BRIDGE.$PUBLIC_VLAN" "$CEPH_BRIDGE"; do sudo nmcli connection down "$c"; done
sudo nmcli connection delete "$USER_BRIDGE" "$CEPH_BRIDGE.$PUBLIC_VLAN" "$CEPH_BRIDGE"
ip -br link | grep -E "^($CEPH_BRIDGE|$USER_BRIDGE)\b"      # no output
```

Complete [Restore listener firewall](../README.md#restore-listener-firewall)
and [Remove diagnostic tools](../README.md#remove-diagnostic-tools), then:

```sh
podman image rm "$CLIENT_IMAGE"
rm -rf -- "${RUN_DIR:?}" "${INPUT_DIR:?}"
```

## 10. Record the result

Record the build, the release, every check of steps 4 to 8 and the completed
cleanup in the [acceptance ledger](../../docs/acceptance.md) as an observation
row; the case accepts no item. Keep host-specific evidence private.
