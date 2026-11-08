# lab-5-nodes-ceph-networks

IBM Storage Ceph on three RHEL 9.8 guests whose replication traffic runs on its
own VLAN, a fourth guest, `app-01`, on the Ceph public network and on a separate
user network, and a fifth, `client-01`, on the user network only. `app-01` runs a
sample HTTP server that answers only on the user network and stores each request
in block (RBD), file (CephFS) and object (RGW S3) storage over the Ceph public
network; `client-01` calls it from a container. This host is the libvirt
substrate and the controller, whose services every installation needs; every
workload runs on a provisioned guest, and Bootwright creates and removes every
network from its `Network` objects. Context `lab-5-nodes-ceph-networks`;
5 guests, 13 vCPUs, 24 GiB.

```text
          this host: libvirt substrate + controller (DNS, NTP, proxy, artifacts on CONTROLLER_ADDRESS)
     ┌──────────────────────────────┴───────────────────────────────┐
  virbr-ceph  (managed Network ceph-public, NAT)              virbr-user  (managed Network app-users, NAT)
   ├ untagged   198.51.100.0/26   host .1   Ceph public        untagged 198.51.100.64/26   host .65
   └ VLAN 120   203.0.113.0/26    Ceph cluster: no host address, unrouted

  ceph-01   nic0 198.51.100.11   nic0.120 203.0.113.11   MON OSD RGW
  ceph-02   nic0 198.51.100.12   nic0.120 203.0.113.12   MON OSD RGW ingress
  ceph-03   nic0 198.51.100.13   nic0.120 203.0.113.13   MON OSD ingress
            (2 MGRs and 2 MDSs on two of the three, cephadm's choice)
  app-01    nic0 198.51.100.21                           nic1 198.51.100.70   sample server :8080 on .70 only
  client-01                                              nic0 198.51.100.80   client container
  RGW ingress VIP 198.51.100.60/26 (TLS 443, name s3.e2e.example.test, no DNS record)
```

| Network | Object | Realized as | Members | Purpose |
| --- | --- | --- | --- | --- |
| Ceph public | `ceph-public`, 198.51.100.0/26, managed, NAT | libvirt network `bootwright-lab-5-nodes-ceph-networks-ceph-public` on `virbr-ceph` | 3 Ceph hosts, `app-01`, host `.1` | install, SSH, MON, OSD front, RGW ingress |
| Ceph cluster | `ceph-cluster`, VLAN 120, 203.0.113.0/26, provided | tagged on `virbr-ceph` (`taggedNetworkRefs`) | 3 Ceph hosts only | OSD replication and heartbeats |
| User | `app-users`, 198.51.100.64/26, managed, NAT | libvirt network `bootwright-lab-5-nodes-ceph-networks-app-users` on `virbr-user` | `app-01` `nic1`, `client-01`, host `.65` | HTTP to the sample server |

The case exercises [managed Networks](../../specs/api/networks.md#libvirt-arm)
as provider segments, a tagged Network on a managed bridge, a NIC per segment,
the [cross-kind checks](../../specs/api/networks.md#cross-kind-checks) and the
Ceph network checks of X323 and X324 on live guests. It accepts no milestone
item; it was added at the owner's request of 2026-11-08.

The host holds an address on each managed network, as their gateway, and
libvirt refuses new connections between two of its networks, so the two
managed networks stay apart while each reaches the internet through NAT:
the Ceph hosts for IBM packages and images, `client-01` for its container
image. Nothing on the host joins VLAN 120.

Run every numbered step in order in one Bash session from the checkout under
test. An unexpected failure fails the run; retain its private records for
cleanup and start again from the beginning on a clean host.

## 1. Choose the inputs

[values.yaml](values.yaml) holds the addresses, the VLAN id, guest sizes and the
IBM Storage Ceph 9.9.2.0 coordinates. Another release moves the fields the
[lab-3-nodes-ceph release table](../lab-3-nodes-ceph/runbook.md#releases-b626)
names together; put them in a private values file. The commands below use the
default addresses: change these variables with any address override.

```bash
VALUES_FILE='' # Optional private YAML file with other release coordinates and image digests.
IBM_REGISTRY_FILE="$HOME/ibm-ceph-registry.txt"
CEPH_BRIDGE=virbr-ceph; USER_BRIDGE=virbr-user
PUBLIC_CIDR=198.51.100.0/26; CLUSTER_CIDR=203.0.113.0/26; CLUSTER_VLAN=120
USER_GATEWAY=198.51.100.65; USER_CIDR=198.51.100.64/26
APP_PUBLIC=198.51.100.21; APP_USER=198.51.100.70; APP_PORT=8080; CLIENT=198.51.100.80
OBJECT_VIP=198.51.100.60; DOMAIN=e2e.example.test
CLIENT_IMAGE=registry.access.redhat.com/ubi9/python-312:latest
CASE_DIR="$PWD/e2e/lab-5-nodes-ceph-networks"
```

## 2. Prepare the host

Complete [host preparation](../README.md#prepare-the-host), including the RHEL
9.8 boot and DVD media and the IBM entitlement key file, then return here.
This case also needs `tcpdump` for the segregation capture; record a missing
one for removal with the other diagnostic packages:

```bash
if ! rpm -q tcpdump >/dev/null; then
    printf 'tcpdump\n' >> "${HOST_CHECKS:?Run the diagnostic tools step of the host preparation first}/added"
    sudo dnf install --setopt=install_weak_deps=False tcpdump
fi
command -v tcpdump python3
```

The [capacity gate](../../specs/substrates.md#capacity-admission) counts three
Ceph guests (3 vCPU, 6 GiB, 30 GiB root and a 25 GiB OSD disk each), `app-01`
(2 vCPU, 2 GiB, 20 GiB), `client-01` (1 vCPU, 1.5 GiB, 15 GiB) and the reserve
(1 vCPU, 2 GiB, 10 GiB): 13 host CPUs, 24 GiB of `MemAvailable` and 210 GiB
free under `/var/lib/libvirt`. Close large applications first. The host's
ports 53, 123, 3128, 8080 and 8443 on its address and loopback 8000 to 8004
must be free. No other case may be applied: this one uses 198.51.100.0/25 and
203.0.113.0/26. Require no output from each check:

```sh
ip -br link | grep -E "^($CEPH_BRIDGE|$USER_BRIDGE)\b"
ip -4 route | grep -E '198\.51\.100\.|203\.0\.113\.'
sudo virsh --connect qemu:///system list --all --name | grep bootwright-
sudo virsh --connect qemu:///system net-list --all --name | grep bootwright-
```

## 3. Create and apply the context

```bash
umask 077
RUN_DIR=$(mktemp -d); printf 'Run directory: %s\n' "$RUN_DIR"
RENDER_ARGS=()
if [ -n "$VALUES_FILE" ]; then RENDER_ARGS+=(--values "$VALUES_FILE"); fi
e2e/render.sh lab-5-nodes-ceph-networks "${RENDER_ARGS[@]}"
CONTEXT="lab-5-nodes-ceph-networks"; INPUT_DIR="$HOME/bootwright-e2e/lab-5-nodes-ceph-networks"
CONTROLLER_ADDRESS=$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p'); printf 'Controller address: %s\n' "$CONTROLLER_ADDRESS"
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

The controller address is a DHCP lease on most laptops: keep it for the whole
run, or the services stop answering. Then apply by stage:

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
sudo virsh --connect qemu:///system net-list --all
for n in ceph-public app-users; do sudo virsh --connect qemu:///system net-dumpxml "bootwright-$CONTEXT-$n"; done
ip -br address show "$CEPH_BRIDGE"; ip -br address show "$USER_BRIDGE"
for b in "$CEPH_BRIDGE" "$USER_BRIDGE"; do sudo firewall-cmd --get-zone-of-interface="$b"; done
```

Require both libvirt networks active, each `<forward mode='nat'>` on its
bridge with the host address 198.51.100.1/26 and 198.51.100.65/26, and
`trusted` for both bridges: Bootwright made them from the two managed
Networks. Then the guests and the cluster:

```sh
./bin/bootwright apply --context "$CONTEXT" --stage machines --yes
./bin/bootwright machine list --context "$CONTEXT" --power-status
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01,ceph-02,ceph-03,app-01,client-01 -- ip -4 -br address
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01,ceph-02,ceph-03,app-01,client-01 -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright preflight storage-cluster --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT" --stage clusters
./bin/bootwright apply --context "$CONTEXT" --stage clusters --yes
./bin/bootwright status --context "$CONTEXT"
```

Require on each Ceph host `nic0` and `nic0.120` with their addresses; on
`app-01` `nic0` and `nic1`; on `client-01` only `nic0`. The IBM hosts must
resolve on every guest.

## 4. Verify the cluster

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

## 5. Deploy the sample server

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

## 6. Run the client

`client-01` runs the client in a container on its own network, so every
request crosses the user network. Copy the client and pull its image:

```bash
CLIENT_BUNDLE="$RUN_DIR/e2e-client"; mkdir -m 0700 "$CLIENT_BUNDLE"
cp "$CASE_DIR/app/client.py" "$CASE_DIR/app/client-run.sh" "$CLIENT_BUNDLE/"
./bin/bootwright machine cp --context "$CONTEXT" --name client-01 "local:$CLIENT_BUNDLE" remote:e2e-client
./bin/bootwright machine exec --context "$CONTEXT" --name client-01 -- sudo -n podman pull "$CLIENT_IMAGE"
client() {
    ./bin/bootwright machine exec --context "$CONTEXT" --name client-01 -- \
        sudo -n bash e2e-client/client-run.sh "$CLIENT_IMAGE" "http://$APP_USER:$APP_PORT" "$1" "$APP_PUBLIC"
}
client roundtrip
```

The client puts 4 KiB, 1 MiB and 8 MiB of random bytes into each store, reads
each back and compares, overwrites one, deletes them and requires a 404
afterwards. Require `0 failed`. Then prove the data lives in Ceph, not on
`app-01`: write a canary into each store, detach the stores, power-cycle the
guest, attach them again and read the canaries back:

```sh
client write-canary
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n bash e2e-sample/app-teardown.sh
./bin/bootwright machine stop --context "$CONTEXT" --name app-01 --yes
./bin/bootwright machine start --context "$CONTEXT" --name app-01
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- uptime      # repeat until SSH answers
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n bash e2e-sample/app-setup.sh
client check-canary
```

Require `0 failed` from each. Keep the canaries for the next step.

## 7. Validate the segregation

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
front-heartbeat traffic on the public network, replication and back-heartbeat
traffic on the cluster VLAN.

### Interfaces and routes

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01,ceph-02,ceph-03 -- ip -d -o link show type vlan
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ip route get 198.51.100.11
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ip route get "$CLIENT"
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sysctl net.ipv4.ip_forward
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n ss -Htlnp sport = :8080
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01 -- ping -c 3 203.0.113.12
ip -4 -br address | grep -c '203\.0\.113\.'
```

Require `vlan protocol 802.1Q id 120` on `nic0.120` of each Ceph host; `app-01`
routes the MONs via `nic0` and `client-01` via `nic1`, does not forward
(`= 0`) and listens on 8080 only on 198.51.100.70; the Ceph hosts reach each
other on the cluster VLAN; and the host holds no cluster address (`0`).

### What cannot reach what

Each probe below must fail. The sessions are proved first, so a probe's
failure is the network's answer, never a broken session; an exit of 255 is a
session failure and stops the check:

```bash
./bin/bootwright machine exec --context "$CONTEXT" --name app-01,ceph-01,client-01 -- true
expect_fail() {
    "$@"; rc=$?
    if [ "$rc" -eq 0 ]; then printf 'FAIL: reached: %s\n' "$*"; return 1; fi
    if [ "$rc" -eq 255 ]; then printf 'ERROR: the session failed, nothing was tested: %s\n' "$*"; return 2; fi
    printf 'OK, refused (exit %s): %s\n' "$rc" "$*"
}
on() { local machine=$1; shift; ./bin/bootwright machine exec --context "$CONTEXT" --name "$machine" -- "$@"; }
expect_fail on client-01 ping -c 2 -W 2 198.51.100.11                                                          # client to a Ceph host
expect_fail on client-01 curl -sS --noproxy '*' --max-time 5 -o /dev/null -k "https://$OBJECT_VIP/"           # client to the object VIP
expect_fail on client-01 curl -sS --noproxy '*' --max-time 5 -o /dev/null "http://$APP_PUBLIC:$APP_PORT/healthz"   # client to the server's Ceph address
expect_fail on client-01 ping -c 2 -W 2 203.0.113.11                                                           # client to the cluster VLAN
expect_fail on ceph-01 ping -c 2 -W 2 "$CLIENT"                                                                # a Ceph host to the client
expect_fail on ceph-01 curl -sS --noproxy '*' --max-time 5 -o /dev/null "http://$APP_USER:$APP_PORT/healthz"   # a Ceph host to the server's user address
expect_fail on app-01 ping -c 2 -W 2 203.0.113.11                                                              # app-01 to the cluster VLAN
on client-01 curl -fsS --noproxy '*' --max-time 5 "http://$APP_USER:$APP_PORT/healthz"                         # client to the server: answers
```

The paths between the two managed networks fail because libvirt rejects new
connections between them, the request to 198.51.100.21:8080 included, before
it reaches `app-01`; the cluster VLAN is unreachable because only the Ceph
hosts hold an address on it and the host routes it only by its default route,
off the fabric. The host itself is a member of both managed networks, as their
gateway, by design. Read the rules that refused them, read-only:

```sh
sudo nft list chain ip libvirt_network forward
sudo nft list chain ip libvirt_network guest_input
```

Require the `forward` chain to jump to `guest_cross`, `guest_input` and
`guest_output` in that order, and the `guest_input` reject rules for
`virbr-ceph` and `virbr-user` to show packet counts: they are the refusals
above.

### Traffic on the wire

Capture both bridges while the client runs once more. Bridge capture in
promiscuous mode sees the guests' frames with their 802.1Q tags:

```bash
sudo -v
sudo timeout 150 tcpdump -i "$CEPH_BRIDGE" -nn -s 128 -Z root -w "$RUN_DIR/ceph-public.pcap" &
sudo timeout 150 tcpdump -i "$USER_BRIDGE" -nn -s 128 -Z root -w "$RUN_DIR/app-users.pcap" &
sleep 5
client roundtrip
wait
count() { sudo tcpdump -r "$1" -nn "$2" 2>/dev/null | wc -l; }
P="$RUN_DIR/ceph-public.pcap"; U="$RUN_DIR/app-users.pcap"
printf '%-70s %s\n' \
  'ceph-public: replication on VLAN 120 (OSD ports)                want > 0' "$(count "$P" "vlan $CLUSTER_VLAN and tcp portrange 6800-7568")" \
  'ceph-public: VLAN 120 with an address outside 203.0.113.0/26     want 0' "$(count "$P" "vlan $CLUSTER_VLAN and ip and not (src net $CLUSTER_CIDR and dst net $CLUSTER_CIDR)")" \
  'ceph-public: 802.1Q frames, all                          equal to next' "$(count "$P" 'vlan')" \
  'ceph-public: 802.1Q frames on VLAN 120                  equal to above' "$(count "$P" "vlan $CLUSTER_VLAN")" \
  'ceph-public: app-01 to the MONs, untagged                      want > 0' "$(count "$P" "host $APP_PUBLIC and (tcp port 6789 or tcp port 3300) and not vlan")" \
  'ceph-public: app-01 to the OSDs, untagged                      want > 0' "$(count "$P" "host $APP_PUBLIC and tcp portrange 6800-7568 and not vlan")" \
  'ceph-public: app-01 to the object VIP, untagged                want > 0' "$(count "$P" "host $APP_PUBLIC and host $OBJECT_VIP and tcp port 443 and not vlan")" \
  'ceph-public: a user-network address, untagged                    want 0' "$(count "$P" "ip and net $USER_CIDR and not vlan")" \
  'ceph-public: a user-network address, tagged                      want 0' "$(count "$P" "vlan and ip and net $USER_CIDR")" \
  'app-users:   HTTP between client-01 and app-01                 want > 0' "$(count "$U" "tcp port $APP_PORT and host $CLIENT and host $APP_USER")" \
  'app-users:   other unicast IPv4, controller services and SSH aside want 0' "$(count "$U" "ip and not ip multicast and not ip broadcast and not (host $CLIENT and host $APP_USER) and not host $CONTROLLER_ADDRESS and not (host $USER_GATEWAY and host $CLIENT and tcp port 22)")" \
  'app-users:   Ceph ports (MON, OSD, S3)                           want 0' "$(count "$U" 'tcp port 6789 or tcp port 3300 or tcp portrange 6800-7568 or tcp port 443')" \
  'app-users:   any 802.1Q frame                                    want 0' "$(count "$U" 'vlan')" \
  'app-users:   multicast and broadcast (host mDNS, IGMP)      information' "$(count "$U" 'ip multicast or ip broadcast')"
```

Require every count as marked. A filter names `vlan` last when it reads
untagged frames: libpcap shifts every later field by the tag's 4 bytes once
`vlan` appears, even under `not`. The exceptions on `app-users` are
`client-01`'s DNS and NTP queries to the controller address and the
`machine exec` session that drives the client, which leaves this host from
the network's gateway address. A connection that crossed between the networks
through the host would appear there from the gateway address too, on another
port, so the row still catches it. Then remove the canaries:

```sh
client clean-canary
```

## 8. Destroy and clean up

Detach `app-01` so no RBD watcher or CephFS session holds the stores, then
destroy as the plan directs. With the guests running, destroy pauses before
removal and names the guests to stop; the repeated destroy continues the same
operation:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n bash e2e-sample/app-teardown.sh
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright machine stop --context "$CONTEXT" --name ceph-01,ceph-02,ceph-03,app-01,client-01 --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright status --context "$CONTEXT"
sudo virsh --connect qemu:///system list --all
sudo virsh --connect qemu:///system net-list --all
ip -br link | grep -E "^($CEPH_BRIDGE|$USER_BRIDGE)\b"      # no output
```

Require no domain, disk or libvirt network of the context and neither bridge
to remain: destroy removed what the managed Networks realized. Then:

```sh
EVIDENCE_CONTEXT="$CONTEXT"
```

Complete [Preserve evidence before context purge](../README.md#preserve-evidence-before-context-purge),
then delete the context:

```sh
./bin/bootwright context delete --name "$CONTEXT" --purge --yes
```

Complete [Restore listener firewall](../README.md#restore-listener-firewall)
and [Remove diagnostic tools](../README.md#remove-diagnostic-tools), then:

```sh
rm -rf -- "${RUN_DIR:?}" "${INPUT_DIR:?}"
```

## 9. Record the result

Record the build, the release, every check of steps 3 to 7 and the completed
cleanup in the [acceptance ledger](../../docs/acceptance.md) as an observation
row; the case accepts no item. Keep host-specific evidence private.
