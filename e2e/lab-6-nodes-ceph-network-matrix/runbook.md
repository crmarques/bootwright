# lab-6-nodes-ceph-network-matrix

The networking matrix on one laptop: IBM Storage Ceph whose three data hosts
carry client traffic on one VLAN and replication on a second, and whose third
MON runs alone on a MON-only arbiter host in a third VLAN that this host routes
to the first. Every guest installs over a different interface shape: each
install shape of [lab-network-variants](../lab-network-variants/runbook.md)
runs here on a guest that carries real traffic. A sample server and its client
reuse the [lab-5-nodes-ceph-networks](../lab-5-nodes-ceph-networks/runbook.md)
workload on a managed user network, whose guest-only VLAN the host refuses.
Context `lab-6-nodes-ceph-network-matrix`; 6 guests, 15 vCPUs, 23 GiB.

```text
             this host: libvirt substrate + controller (DNS, NTP, proxy, artifacts on CONTROLLER_ADDRESS)
     ┌──────────────────────────────┴───────────────────────────────────────┐
  br-nm  (operator trunk, no port; legs routed + NAT by the runbook)    virbr-user  (managed Network app-users, NAT)
   ├ VLAN 110  198.51.100.0/26    leg br-nm.110 .1     Ceph public      ├ untagged  198.51.100.64/26  host .65   users
   ├ VLAN 120  203.0.113.0/26     no leg               Ceph cluster     └ VLAN 140  203.0.113.64/26   guest-only, MTU 1400,
   └ VLAN 150  198.51.100.128/26  leg br-nm.150 .129   arbiter, routed                                 refused on the host
                                                        to VLAN 110

  ceph-01    nic0.110 .11              nic0.120 203.0.113.11          MON OSD RGW ingress           VLAN install
  ceph-02    bond0.110 .12             bond0.120 203.0.113.12         MON OSD RGW ingress           VLAN-over-bond install
  ceph-04    nic0.110 .14              nic1.120 203.0.113.14          OSD                           cluster VLAN on an addressless NIC
             (2 MGRs and 2 MDSs on two of the three data hosts, cephadm's choice)
  ceph-03    nic0.150 198.51.100.130                                  MON only (arbiter)            VLAN install, one subnet
  app-01     nic0.110 .21 (Ceph public)   nic1 198.51.100.70   nic1.140 203.0.113.70                VLAN install
  client-01  bond0 198.51.100.80          bond0.140 203.0.113.80                                    bond install
  RGW ingress VIP 198.51.100.60/26 on VLAN 110 (TLS 443, name s3.e2e.example.test, no DNS record)
```

| Machine | Install interface | Segment | Converged after the install |
| --- | --- | --- | --- |
| `ceph-01` | `nic0.110` over `nic0` | `ceph-trunk` | `nic0.120` |
| `ceph-02` | `bond0.110` over `bond0`, active-backup over `nic0` and `nic1` | `ceph-trunk` twice | `bond0.120` |
| `ceph-04` | `nic0.110` over `nic0` | `ceph-trunk` twice | addressless `nic1` carrying `nic1.120` |
| `ceph-03` | `nic0.150` over `nic0` | `ceph-trunk` | nothing: one subnet |
| `app-01` | `nic0.110` over `nic0` | `ceph-trunk`, then `app-users` | `nic1` on the user network, `nic1.140` MTU 1400, a 192.0.2.0/24 route |
| `client-01` | `bond0`, active-backup over `nic0` and `nic1` | `app-users` twice | `bond0.140` MTU 1400, a 192.0.2.0/24 route |

| Network | Object | Realized as | Purpose |
| --- | --- | --- | --- |
| Ceph public | `ceph-public`, VLAN 110, provided | trunk leg `br-nm.110` holds its gateway | install, SSH, MON, OSD front, RGW ingress |
| Ceph cluster | `ceph-cluster`, VLAN 120, provided | tagged on the trunk, no leg | OSD replication and heartbeats of the three data hosts |
| Arbiter | `ceph-arbiter`, VLAN 150, provided | trunk leg `br-nm.150`, routed to VLAN 110 | the third MON; the cluster's second public CIDR |
| User | `app-users`, managed, NAT | libvirt network on `virbr-user` | HTTP to the sample server; install of `client-01` |
| Guest-only | `app-guest-only`, VLAN 140, provided | tagged on `virbr-user`; its subnet refused on the host | converged second network of `app-01` and `client-01` |

The cluster's `publicCIDRs` are 198.51.100.0/26 and 198.51.100.128/26 and its
`clusterCIDRs` 203.0.113.0/26. IBM Storage Ceph routes the subnets of one
network to each other and carries only OSD traffic on the cluster network, so
the MON-only `ceph-03` holds no replication address
([B1266](../../specs/milestones/m2.md#b1266)). Three OSD hosts keep the
replicated pools' size 2 one host short of their host count, as the
[replica-safety rule](../../specs/ceph-resources.md#replicated-protection)
requires.

A VLAN install interface needs its gateway on that VLAN, and Bootwright never
gives the host an address on a tagged Network
([provider host realization](../../specs/substrates.md#provider-host-realization)),
so the Ceph VLANs ride an operator trunk whose legs the runbook creates, routes
and NATs. The [acceptance table](#11-record-the-result) names what one
complete run supplies.

Run every numbered step in order in one Bash session from the checkout under
test, except where a step names a second terminal. An unexpected failure fails
the run; retain its private records for cleanup and start again from the
beginning on a clean host.

## 1. Choose the inputs

[values.yaml](values.yaml) holds the addresses, the VLAN ids, guest sizes and
the IBM Storage Ceph 9.9.2.0 coordinates. Another release moves the fields
the [lab-3-nodes-ceph release table](../lab-3-nodes-ceph/runbook.md#releases-b626)
names together; put them in a private values file. The commands below use the
default addresses: change these variables with any address override.

```bash
set +e  # Keep expected refusal and Ctrl-C statuses from ending this interactive session.
VALUES_FILE='' # Optional private YAML file with other release coordinates and image digests.
IBM_REGISTRY_FILE="$HOME/ibm-ceph-registry.txt"
TRUNK=br-nm; PUBLIC_VLAN=110; CLUSTER_VLAN=120; ARBITER_VLAN=150
PUBLIC_CIDR=198.51.100.0/26; PUBLIC_GATEWAY=198.51.100.1
ARBITER_CIDR=198.51.100.128/26; ARBITER_GATEWAY=198.51.100.129; ARBITER=198.51.100.130
CLUSTER_CIDR=203.0.113.0/26
USER_BRIDGE=virbr-user; USER_CIDR=198.51.100.64/26; USER_GATEWAY=198.51.100.65
GUEST_VLAN=140; GUEST_CIDR=203.0.113.64/26
APP_PUBLIC=198.51.100.21; APP_USER=198.51.100.70; APP_GUEST=203.0.113.70; APP_PORT=8080
CLIENT=198.51.100.80; CLIENT_GUEST=203.0.113.80
OBJECT_VIP=198.51.100.60; DOMAIN=e2e.example.test
CLIENT_IMAGE=registry.access.redhat.com/ubi9/python-312:latest
CASE_DIR="$PWD/e2e/lab-5-nodes-ceph-networks"   # the reused sample server and client
CONTEXT="lab-6-nodes-ceph-network-matrix"; INPUT_DIR="$HOME/bootwright-e2e/$CONTEXT"
GUESTS=ceph-01,ceph-02,ceph-03,ceph-04,app-01,client-01
CEPH_HOSTS=ceph-01,ceph-02,ceph-03,ceph-04
```

## 2. Prepare the host, the trunk and its routing

Complete [host preparation](../README.md#prepare-the-host), including the RHEL
9.8 boot and DVD media and the IBM entitlement key file, then return here.
Record a missing `tcpdump` for removal with the other diagnostic packages:

```bash
if ! rpm -q tcpdump >/dev/null; then
    printf 'tcpdump\n' >> "${HOST_CHECKS:?Run the diagnostic tools step of the host preparation first}/added"
    sudo dnf install --setopt=install_weak_deps=False tcpdump
fi
command -v tcpdump python3
```

The [capacity gate](../../specs/substrates.md#capacity-admission) counts three
data hosts (3 vCPU, 5 GiB, 20 GiB root and a 15 GiB OSD disk each), `ceph-03`
(2 vCPU, 2 GiB, 15 GiB), `app-01` (2 vCPU, 2 GiB, 15 GiB), `client-01`
(1 vCPU, 1.5 GiB, 15 GiB) and the reserve (1 vCPU, 2 GiB, 10 GiB): 15 host
CPUs, 22.5 GiB of `MemAvailable` and 160 GiB free under `/var/lib/libvirt`,
plus about 34 GB (31 GiB) for the package tree and the six guests' installer
media ([disk headroom](../README.md#end-to-end-cases)), so about 191 GiB in
all. Close large applications first. The host's ports 53, 123, 3128, 8080 and
8443 on its address and loopback 8000 to 8005 must be free. No other case may
be applied. Require the two capacity numbers and no output from each check:

```sh
grep MemAvailable /proc/meminfo; df -BG --output=avail /var/lib/libvirt
ip -br link | grep -E "^($USER_BRIDGE|$TRUNK)(\.|\b)"
ip -4 route | grep -E '198\.51\.100\.|203\.0\.113\.|192\.0\.2\.'
sudo nft list tables | grep bw-e2e
sudo virsh --connect qemu:///system list --all --name | grep bootwright-
sudo virsh --connect qemu:///system net-list --all --name | grep bootwright-
systemctl list-units --all --no-legend 'bootwright-*'
```

Create the operator trunk: an isolated bridge with no physical port and two
VLAN legs, the gateways of the two Ceph public subnets. VLAN 120 gets no leg:
nothing on the host joins the replication network. Bootwright neither creates
nor removes these connections.

```sh
sudo nmcli connection add type bridge ifname "$TRUNK" con-name "$TRUNK" bridge.stp no ipv4.method disabled ipv6.method disabled connection.zone trusted
sudo nmcli connection add type vlan ifname "$TRUNK.$PUBLIC_VLAN" con-name "$TRUNK.$PUBLIC_VLAN" vlan.parent "$TRUNK" vlan.id "$PUBLIC_VLAN" ipv4.method manual ipv4.addresses "$PUBLIC_GATEWAY/26" ipv6.method disabled connection.zone trusted
sudo nmcli connection add type vlan ifname "$TRUNK.$ARBITER_VLAN" con-name "$TRUNK.$ARBITER_VLAN" vlan.parent "$TRUNK" vlan.id "$ARBITER_VLAN" ipv4.method manual ipv4.addresses "$ARBITER_GATEWAY/26" ipv6.method disabled connection.zone trusted
sudo nmcli connection up "$TRUNK"
sudo nmcli connection up "$TRUNK.$PUBLIC_VLAN"
sudo nmcli connection up "$TRUNK.$ARBITER_VLAN"
ip -br address show "$TRUNK.$PUBLIC_VLAN"; ip -br address show "$TRUNK.$ARBITER_VLAN"
for i in "$TRUNK.$PUBLIC_VLAN" "$TRUNK.$ARBITER_VLAN"; do sudo firewall-cmd --get-zone-of-interface="$i"; done
```

Require both leg addresses and `trusted` twice. The host routes between the
two legs (both are connected subnets) and must NAT both to its uplink, for
the IBM packages and images. The user network must not reach the trunk: the
libvirt rules let `virbr-user` guests out to any interface, so the runbook's
own table refuses them on the trunk legs. This table is the whole of the
case's host routing policy; it accepts nothing another table refuses:

```bash
UPLINK=$(ip -4 route get 192.0.2.1 | sed -n 's/.* dev \([^ ]*\).*/\1/p'); printf 'Uplink: %s\n' "$UPLINK"
sudo nft add table inet bw-e2e-trunk
sudo nft add chain inet bw-e2e-trunk forward '{ type filter hook forward priority -5; }'
for LEG in "$TRUNK.$PUBLIC_VLAN" "$TRUNK.$ARBITER_VLAN"; do
    sudo nft add rule inet bw-e2e-trunk forward iifname "\"$USER_BRIDGE\"" oifname "\"$LEG\"" counter reject
done
sudo nft add chain inet bw-e2e-trunk postrouting '{ type nat hook postrouting priority 100; }'
sudo nft add rule inet bw-e2e-trunk postrouting ip saddr "{ $PUBLIC_CIDR, $ARBITER_CIDR }" ip daddr != "{ $PUBLIC_CIDR, $ARBITER_CIDR }" oifname "\"$UPLINK\"" counter masquerade
sudo sysctl -w net.ipv4.ip_forward=1
sudo nft list table inet bw-e2e-trunk
```

The `virbr-user` interface does not exist yet; the rule names it and matches
once the substrates stage creates it.

## 3. Create the context and refuse a bond mode

```bash
umask 077
RUN_DIR=$(mktemp -d); printf 'Run directory: %s\n' "$RUN_DIR"
RENDER_ARGS=()
if [ -n "$VALUES_FILE" ]; then RENDER_ARGS+=(--values "$VALUES_FILE"); fi
e2e/render.sh "$CONTEXT" "${RENDER_ARGS[@]}"
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

Require the plan to admit `ceph-03` with no replication address (B1266) and
to show, for each Machine, the impacts `configure-network` and `restart`. The
bond refusal (B74): render `bond.mode=802.3ad` into a second directory, which
validates, import it and plan:

```sh
e2e/render.sh "$CONTEXT" "${RENDER_ARGS[@]}" --set bond.mode=802.3ad --out-dir "$RUN_DIR/input-mode"
./bin/bootwright context update --name "$CONTEXT" --input-dir "$RUN_DIR/input-mode" --yes
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright context update --name "$CONTEXT" --input-dir "$INPUT_DIR" --yes
```

Require the plan to say that a libvirt bridge qualifies only active-backup
bonding, naming `bond0` of `Machine/ceph-02` and of `Machine/client-01`, with
mode 802.3ad. The last command restores the valid input.

## 4. Prepare the controller, services and networks

The controller address is a DHCP lease on most laptops: keep it for the whole
run, or the services stop answering.

```sh
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
```

```bash
FIREWALL_PORTS=(53/tcp 53/udp 123/udp 3128/tcp 8080/tcp 8443/tcp)
```

Complete [Prepare listener firewall](../README.md#prepare-listener-firewall),
then require `ready` before starting services:

```sh
./bin/bootwright preflight --context "$CONTEXT" --stage controller
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services,substrates --yes
sudo virsh --connect qemu:///system net-dumpxml "bootwright-$CONTEXT-app-users"
ip -br address show "$USER_BRIDGE"; sudo firewall-cmd --get-zone-of-interface="$USER_BRIDGE"
ip -4 route show "$GUEST_CIDR" table all
ip -4 route show "$CLUSTER_CIDR" table all
systemctl list-units --all --no-legend 'bootwright-*'
sudo ls /var/lib/bootwright-hosted/"$CONTEXT"
sysctl net.ipv4.ip_forward
```

Require the libvirt network active with `<forward mode='nat'>` on
`virbr-user` and the host address 198.51.100.65/26, `trusted` for the bridge,
and (B1215) an `unreachable 203.0.113.64/26` route held by a Bootwright unit
the listing shows: the host refuses the guest-only subnet instead of routing it
out of its uplink. The replication VLAN rides the operator trunk, so the host
refuses nothing for it and the second route listing is empty. Require (B1221)
the context's directory under `/var/lib/bootwright-hosted/`, holding the served
content and the emulated BMC state, and forwarding `= 1`.

## 5. Install the guests through both interruption drills

Two drills interrupt this stage (B74 and B326); a third, the transport-loss
drill, rides on the first apply. A second terminal follows each apply (B1223).
Widen the terminal until the progress descriptions are readable in full. The
six guests install in parallel: note each one's progress when interrupting.
Missing either interruption requires a new complete run.

**Second terminal (B1223).** A follow started while no operation runs prints
the newest finished one and ends, so start it in the checkout after each apply
below has printed its first progress row, once per apply, the resumed ones
included:

```sh
./bin/bootwright logs --context lab-6-nodes-ceph-network-matrix --follow
```

Require it to stream the operation's events and each running block's output
prefixed by its block, to exit by itself when the operation leaves `running`,
and the apply never to wait on it: the apply's progress in the first terminal
continues while the follower prints, and a Ctrl-C there ends only the
follower.

**Transport-loss drill (B1044, B1036).** Before an installation boots, the
controller fetches its own artifact listener to prove the published tree, then
each installation's image, private files and tree marker; a fetch lost before
any status line is made again, at most twice more five seconds apart
([boot](../../specs/managed-os.md#boot)). The rule below resets every new
connection the host makes to its own listener ports. The watcher removes it two
seconds after it first fires, so the `os-package-tree-rhel` block's `.treeinfo`
proof (B1044) passes on a later attempt; fifteen seconds later it arms the rule
once more, so the next self-fetch, normally an `os-install-*` block's (B1036),
meets the same reset. Guests' connections enter through `input` and are
untouched:

```bash
sudo -v
arm_reset() {
    sudo nft add table inet bw-e2e-drop
    sudo nft add chain inet bw-e2e-drop out '{ type filter hook output priority 0; }'
    sudo nft add rule inet bw-e2e-drop out ip daddr "$CONTROLLER_ADDRESS" tcp dport '{ 8080, 8443 }' ct state new counter reject with tcp reset
}
fire_once() {  # $1: file that keeps the fired chain
    for _ in $(seq 1 1800); do
        if sudo nft list chain inet bw-e2e-drop out | grep -q 'counter packets [1-9]'; then
            sleep 2
            sudo nft list chain inet bw-e2e-drop out > "$1"
            sudo nft delete table inet bw-e2e-drop
            return 0
        fi
        sleep 1
    done
    sudo nft delete table inet bw-e2e-drop
    return 1
}
arm_reset
( fire_once "$RUN_DIR/drop-fired-1.txt" && sleep 15 && arm_reset && fire_once "$RUN_DIR/drop-fired-2.txt" ) &
DROP_WATCH=$!
```

First, interrupt installation (B74). Start the apply on its own:

```sh
./bin/bootwright apply --context "$CONTEXT" --stage machines --yes
```

Require the gate to admit the apply (B1213): its capacity check counts the
guests' disks, the package tree and each guest's published and staged
installer media on the filesystem they land on. A refusal at admission that
names each byte group in the disk check also satisfies B1213, but ends this
run: free space and start again. Leave the apply running until a `[RUNNING]`
Machine row contains this exact text:

```text
wait for the installer to write the disk and power the machine off
```

**At that moment, press Ctrl-C once.** Wait for the prompt, then:

```bash
wait "$DROP_WATCH"; cat "$RUN_DIR/drop-fired-1.txt" "$RUN_DIR/drop-fired-2.txt"; sudo nft list table inet bw-e2e-drop
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright machine list --context "$CONTEXT" --power-status
for b in os-package-tree-rhel os-install-ceph-01 os-install-ceph-02 os-install-ceph-03 os-install-ceph-04 os-install-app-01 os-install-client-01; do
    ./bin/bootwright logs --context "$CONTEXT" --block "$b" --raw > "$RUN_DIR/$b.output"
    printf '== %s\n' "$b"; grep -n -B2 -A2 'RETRYING' "$RUN_DIR/$b.output"
done
```

Require both saved chains to show a nonzero packet count (both arms fired),
the `nft list table` to report that the table no longer exists, and no block
to have failed on a listener fetch. B1044: the `os-package-tree-rhel` output
shows `FAILED - RETRYING` on `Prove the published tree answers through its
trusted listener`, then the task passing. B1036: one `os-install-*` output
shows `FAILED - RETRYING` on `Fetch the first byte of the installer image
through its listener` (or of a private file, or of the package tree marker),
then the task passing. Record which. A count of 0, or a second reset that
landed on the tree block again, leaves that item's drill unfired: repeat it in
a new run. `runtime.interrupted` and the interrupted install blocks reading
`unknown` are expected. Follow the
[lab-network-variants powered-off checkpoint](../lab-network-variants/runbook.md#6-install-with-the-required-interruption-drills)
exactly: wait for every interrupted installer guest to power off naturally,
stop only guests confirmed still in the installer wait, and prove them `off`
before retrying.

Second, resume and interrupt network convergence (B326):

```sh
./bin/bootwright apply --context "$CONTEXT" --stage machines --yes
```

Leave it running until a `[RUNNING]` Machine row contains this exact text,
then press Ctrl-C once:

```text
apply the declared network, restart the machine and prove its network
```

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright machine list --context "$CONTEXT" --power-status
```

Require the interrupted Machine's network convergence to remain unfinished,
never realized. Repeat the powered-off checkpoint for guests still in their
installer waits, then converge:

```sh
./bin/bootwright apply --context "$CONTEXT" --stage machines --yes
./bin/bootwright machine list --context "$CONTEXT" --power-status
sudo ls -R /var/lib/bootwright-hosted/"$CONTEXT" | head -40
```

Require (B1221) the installer-image work of the installations under the
context's hosted directory too, beside its served content and BMC state.

## 6. Verify the install shapes, the final network and the routing

B74, the shapes. One active profile per declared interface; `active-backup`
over `nic0` and `nic1` on `ceph-02` and `client-01`; the VLANs on their
parents; every interface name pinned to its MAC:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- nmcli -t -f NAME,DEVICE,TYPE,STATE connection show --active
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-02,client-01 -- cat /proc/net/bonding/bond0
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- ip -d -o link show type vlan
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- ip -br link show
for m in ceph-01 ceph-02 ceph-03 ceph-04 app-01 client-01; do sudo virsh --connect qemu:///system domiflist "bootwright-$CONTEXT-$m"; done
```

Match each bond's `Permanent HW addr` under `Slave Interface: nic0` and
`nic1` to its `domiflist` MACs in order, and each unbonded NIC's MAC in
`ip -br link` to its `domiflist` MAC. Require `vlan protocol 802.1Q` with
`id 110` and `id 120` on `ceph-01`'s `nic0`, `ceph-02`'s `bond0` and (110)
`ceph-04`'s `nic0`, `id 120` on `ceph-04`'s addressless `nic1`, only `id 150`
on `ceph-03`, `id 110` on `app-01`'s `nic0` and `id 140` on its `nic1` and on
`client-01`'s `bond0`.

B326, the final network. The converged addresses, MTU 1400 on both VLAN 140
interfaces, the default and 192.0.2.0/24 routes, the search domain, a
network marker per guest and a boot ID that differs from the marker's:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- ip -br address show
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- ip route show
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ip -o link show nic1.140
./bin/bootwright machine exec --context "$CONTEXT" --name client-01 -- ip -o link show bond0.140
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- cat /etc/resolv.conf
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- cat /etc/bootwright/network-marker.json
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- cat /proc/sys/kernel/random/boot_id
./bin/bootwright machine info --context "$CONTEXT" --name "$GUESTS"
./bin/bootwright diff --context "$CONTEXT" --observe
```

Require lifecycle `applied` and every Machine realized. The guest-only VLAN
passes 1400 bytes between `app-01` and `client-01` and no more:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ping -c 3 -M do -s 1372 "$CLIENT_GUEST"
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ping -c 2 -W 2 -M do -s 1373 "$CLIENT_GUEST"
./bin/bootwright machine exec --context "$CONTEXT" --name client-01 -- ip route get 192.0.2.10
```

Require the 1372-byte payload to pass, the 1373-byte one to fail with
`message too long`, and the 192.0.2.0/24 route through 198.51.100.65 on
`bond0`.

The routed arbiter. `ceph-03` reaches the data hosts through `br-nm.150`, one
hop, and every Ceph host reaches the IBM registry through the trunk NAT:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-03 -- ip route get 198.51.100.11
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-03 -- tracepath -n -m 3 198.51.100.11
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01 -- ping -c 3 "$ARBITER"
./bin/bootwright machine exec --context "$CONTEXT" --name "$CEPH_HOSTS" -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright machine exec --context "$CONTEXT" --name "$CEPH_HOSTS" -- curl -sS -o /dev/null -w '%{http_code}\n' --max-time 20 https://cp.icr.io/v2/
sudo nft list chain inet bw-e2e-trunk postrouting
```

Require `via 198.51.100.129 dev nic0.150`, a first hop 198.51.100.129 and the
target on the second, the ping to answer, the names to resolve, `401` from
each host (the registry answered an anonymous request), and a nonzero
masquerade count.

## 7. Build and verify the cluster

```sh
./bin/bootwright preflight --context "$CONTEXT" --stage clusters
./bin/bootwright apply --context "$CONTEXT" --stage clusters --yes
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright cluster info --context "$CONTEXT" --name ceph-e2e
```

Require the storage preflight to pass after the machines stage, its
`capacity.disk` check included (B1213). Run the second terminal's follower
beside this apply too. Then set `CEPH_CONF`, `CEPH_KEYRING` and `ceph_admin`
as in [lab-5-nodes-ceph-networks step 4](../lab-5-nodes-ceph-networks/runbook.md#4-verify-the-cluster),
with `--node ceph-01`, and read the cluster:

```sh
ceph_admin -s
ceph_admin mon dump
ceph_admin osd tree
ceph_admin fs status files
ceph_admin orch ps --refresh
ceph_admin orch ps --hostname ceph-03.e2e.example.test
```

Require `HEALTH_OK` (or only `CHA_ERROR_SENDING_REPORT`), 3 MONs in quorum,
`mon.ceph-03` at 198.51.100.130, an active and a standby MGR, 3 OSDs up and in
on `ceph-01`, `ceph-02` and `ceph-04`, `files` rank 0 active with one standby,
two `rgw.objects` daemons and the `ingress.rgw.objects` haproxy and
keepalived daemons on `ceph-01` and `ceph-02`. B1266: `ceph-03` runs a MON and
only cephadm's per-host daemons (`crash`, exporters), no OSD, MGR, MDS or RGW.

**Seed MON loss (B1020).** `ceph-01` bootstrapped the cluster, and the
retained configuration names only its MON. Stop that MON; the two left in
quorum are `ceph-02` and the routed arbiter:

```bash
FSID='' # The cluster FSID cluster info printed.
MON_UNIT="ceph-${FSID:?Set FSID from cluster info}@mon.ceph-01.service"
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node ceph-01 -- sudo -n systemctl stop "$MON_UNIT"
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node ceph-01 -- systemctl is-active "$MON_UNIT"
ceph_admin -m 198.51.100.12,"$ARBITER" quorum_status --format json-pretty
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node ceph-01 -- sudo -n systemctl start "$MON_UNIT"
ceph_admin -s    # repeat until three MONs are in quorum
```

`ceph_admin` names the two other MONs explicitly: its retained configuration
names only the seed. Require `inactive` for the unit, `quorum_names` to list
`ceph-02` and `ceph-03` only, and `diff --observe` to observe the Ceph objects,
none `unknown` and none timed out, while the seed is down: its Ceph
observation runs Bootwright's admin clients, which name every MON of the
frozen request (`cluster info` reads local evidence only and proves nothing
here). The stopped MON may read as drift. Then require `HEALTH_OK` again with
three MONs after the start.

## 8. Run the workload and validate the segregation

Deploy the sample server and run the client as
[lab-5-nodes-ceph-networks steps 5 and 6](../lab-5-nodes-ceph-networks/runbook.md#5-deploy-the-sample-server)
say, verbatim (`CASE_DIR` names that case), with these differences: `app-01` reaches the stores from `nic0.110` (198.51.100.21) and
serves on `nic1` (198.51.100.70); and the minimal configuration's `mon_host`
names three MONs, `ceph-03`'s on the routed arbiter subnet. Require `0 failed`
from the round trip, the canary write, the power cycle and the canary check,
and every `sourceAddress` 198.51.100.21. Keep the canaries.

**Ceph uses the declared networks.** Every MON on a public subnet, `ceph-03`'s
on the arbiter subnet, and every OSD's front on the Ceph public subnet and back
on the replication VLAN:

```bash
ceph_admin config get mon public_network
ceph_admin config get osd cluster_network
ceph_admin mon dump --format json > "$RUN_DIR/mon-dump.json"
ceph_admin osd dump --format json > "$RUN_DIR/osd-dump.json"
python3 -I - "$RUN_DIR/mon-dump.json" "$RUN_DIR/osd-dump.json" "$PUBLIC_CIDR" "$ARBITER_CIDR" "$CLUSTER_CIDR" <<'PY'
import ipaddress, json, sys
mons, osds = json.load(open(sys.argv[1])), json.load(open(sys.argv[2]))
public, arbiter, cluster = (ipaddress.ip_network(a) for a in sys.argv[3:6])
def ips(vec):
    return [ipaddress.ip_address(a["addr"].rsplit(":", 1)[0]) for a in vec["addrvec"]]
failures = []
for mon in mons["mons"]:
    want = arbiter if mon["name"] == "ceph-03" else public
    for ip in ips(mon["public_addrs"]):
        print(f"mon.{mon['name']:<10} public  {ip}")
        failures += [f"mon.{mon['name']} {ip}"] if ip not in want else []
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

Require both CIDRs in `public_network`, `203.0.113.0/26` and the closing line.

**Interfaces and routes:**

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ip route get 198.51.100.11
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ip route get "$ARBITER"
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ip route get "$CLIENT"
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sysctl net.ipv4.ip_forward
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n ss -Htlnp sport = :8080
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01 -- ping -c 3 203.0.113.12
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-02 -- ping -c 3 203.0.113.14
ip -4 -br address | grep -cE '203\.0\.113\.'
```

Require `app-01` to reach `ceph-01` on `nic0.110` directly, `ceph-03` via
198.51.100.1 and `client-01` on `nic1`, not to forward (`= 0`), to listen on
8080 only on 198.51.100.70; the replication VLAN to pass between the VLAN,
bonded and split data hosts; and the host to hold no 203.0.113.0/24 address
(`0`).

**What cannot reach what.** Define `expect_fail` and `on` as in
[lab-5-nodes-ceph-networks](../lab-5-nodes-ceph-networks/runbook.md#what-cannot-reach-what),
prove the sessions, then require each probe to fail and the last two to
answer:

```bash
./bin/bootwright machine exec --context "$CONTEXT" --name app-01,ceph-01,ceph-03,client-01 -- true
expect_fail on client-01 ping -c 2 -W 2 198.51.100.11                                                          # client to a data host
expect_fail on client-01 ping -c 2 -W 2 "$ARBITER"                                                             # client to the arbiter
expect_fail on client-01 curl -sS --noproxy '*' --max-time 5 -o /dev/null -k "https://$OBJECT_VIP/"           # client to the object VIP
expect_fail on client-01 curl -sS --noproxy '*' --max-time 5 -o /dev/null "http://$APP_PUBLIC:$APP_PORT/healthz"   # client to the server's Ceph address
expect_fail on client-01 curl -sS --noproxy '*' --max-time 5 -o /dev/null "http://$APP_GUEST:$APP_PORT/healthz"    # the server listens on its user address only
expect_fail on ceph-01 ping -c 2 -W 2 "$CLIENT"                                                                # a data host to the client
expect_fail on ceph-03 ping -c 2 -W 2 "$CLIENT"                                                                # the arbiter to the client
expect_fail on ceph-01 curl -sS --noproxy '*' --max-time 5 -o /dev/null "http://$APP_USER:$APP_PORT/healthz"   # a data host to the server's user address
expect_fail on ceph-03 ping -c 2 -W 2 203.0.113.11                                                             # the arbiter to the replication VLAN
on client-01 curl -fsS --noproxy '*' --max-time 5 "http://$APP_USER:$APP_PORT/healthz"                         # client to the server: answers
on ceph-03 ping -c 2 -W 2 "$APP_PUBLIC"                                                                        # the arbiter to a Ceph client: routed, answers
```

The user network's probes into the trunk fail at the runbook's
`bw-e2e-trunk` rule, the trunk's probes into the user network at libvirt's
`guest_input` rule. The arbiter's probe of the replication VLAN fails because
nothing on the host routes it: the host holds no address there and, the VLAN
being on an operator trunk, refuses nothing for it, so the probe leaves the
uplink unmasqueraded and nobody answers. Read the counters, read-only:

```sh
sudo nft list chain inet bw-e2e-trunk forward
sudo nft list chain ip libvirt_network guest_input
```

Require packet counts on both `bw-e2e-trunk` reject rules and on the
`guest_input` reject rule for `virbr-user`.

**The refused guest-only subnet (B1215).** A data host's probe of VLAN 140,
which `virbr-user` carries tagged and the host holds no address in, must be
refused on the host, not sent out of its uplink:

```bash
sudo -v
sudo timeout 15 tcpdump -i "$UPLINK" -nn -c 50 -w "$RUN_DIR/uplink.pcap" "net $GUEST_CIDR" &
sleep 2
on ceph-01 ping -c 3 -W 2 "$APP_GUEST"
wait
sudo tcpdump -r "$RUN_DIR/uplink.pcap" -nn 2>/dev/null | wc -l
ip -4 route show "$GUEST_CIDR" table all
```

Require `From 198.51.100.1 ... Destination Host Unreachable`, from the trunk
leg `ceph-01` routes it to, and `0` packets on the uplink: the observation of
2026-11-08 saw such probes leave through NAT.

**Traffic on the wire.** Capture both bridges while the client runs once more
and the arbiter answers MON traffic. The trunk carries three VLANs tagged and
nothing untagged:

```bash
sudo -v
sudo timeout 150 tcpdump -i "$TRUNK" -nn -s 128 -Z root -w "$RUN_DIR/trunk.pcap" &
sudo timeout 150 tcpdump -i "$USER_BRIDGE" -nn -s 128 -Z root -w "$RUN_DIR/app-users.pcap" &
sleep 5
client roundtrip
on app-01 ping -c 5 -s 1372 "$CLIENT_GUEST"
wait
count() { sudo tcpdump -r "$1" -nn "$2" 2>/dev/null | wc -l; }
T="$RUN_DIR/trunk.pcap"; U="$RUN_DIR/app-users.pcap"
printf '%-72s %s\n' \
  'trunk: replication on VLAN 120 (OSD ports)                        want > 0' "$(count "$T" "vlan $CLUSTER_VLAN and tcp portrange 6800-7568")" \
  'trunk: VLAN 120 with an address outside 203.0.113.0/26             want 0' "$(count "$T" "vlan $CLUSTER_VLAN and ip and not (src net $CLUSTER_CIDR and dst net $CLUSTER_CIDR)")" \
  'trunk: the arbiter MON on VLAN 150                                want > 0' "$(count "$T" "vlan $ARBITER_VLAN and host $ARBITER and (tcp port 3300 or tcp port 6789)")" \
  'trunk: the arbiter routed onto VLAN 110                           want > 0' "$(count "$T" "vlan $PUBLIC_VLAN and host $ARBITER")" \
  'trunk: the arbiter on VLAN 120                                      want 0' "$(count "$T" "vlan $CLUSTER_VLAN and host $ARBITER")" \
  'trunk: app-01 to the MONs on VLAN 110                             want > 0' "$(count "$T" "vlan $PUBLIC_VLAN and host $APP_PUBLIC and (tcp port 6789 or tcp port 3300)")" \
  'trunk: app-01 to the object VIP on VLAN 110                       want > 0' "$(count "$T" "vlan $PUBLIC_VLAN and host $APP_PUBLIC and host $OBJECT_VIP and tcp port 443")" \
  'trunk: a user-network address, tagged                               want 0' "$(count "$T" "vlan and ip and (net $USER_CIDR or net $GUEST_CIDR)")" \
  'trunk: untagged IPv4                                                want 0' "$(count "$T" 'ip and not vlan')" \
  'trunk: 802.1Q frames on another VLAN than 110, 120 or 150           want 0' "$(count "$T" "ether[12:2] = 0x8100 and (ether[14:2] & 0x0fff) != $PUBLIC_VLAN and (ether[14:2] & 0x0fff) != $CLUSTER_VLAN and (ether[14:2] & 0x0fff) != $ARBITER_VLAN")" \
  'app-users: HTTP between client-01 and app-01                      want > 0' "$(count "$U" "tcp port $APP_PORT and host $CLIENT and host $APP_USER and not vlan")" \
  'app-users: the 1400-byte echo on VLAN 140                         want > 0' "$(count "$U" "vlan $GUEST_VLAN and icmp and host $APP_GUEST and host $CLIENT_GUEST")" \
  'app-users: VLAN 140 with an address outside 203.0.113.64/26         want 0' "$(count "$U" "vlan $GUEST_VLAN and ip and not (src net $GUEST_CIDR and dst net $GUEST_CIDR)")" \
  'app-users: a Ceph address, untagged                                 want 0' "$(count "$U" "ip and (net $PUBLIC_CIDR or net $ARBITER_CIDR or net $CLUSTER_CIDR) and not vlan")" \
  'app-users: 802.1Q frames on another VLAN than 140                   want 0' "$(count "$U" "ether[12:2] = 0x8100 and (ether[14:2] & 0x0fff) != $GUEST_VLAN")"
```

Require every count as marked. A filter holds at most one `vlan` keyword and
names it last when it reads untagged frames: each `vlan` moves the fields
after it by one tag, even under `not`, so the rows on other VLAN ids read the
outer tag by absolute offsets. Then remove the canaries with
`client clean-canary`.

## 9. Drift, failure and observation drills

**MTU drift (B326).** Lower one guest-only MTU; `diff --observe` reads
`Machine/client-01` partial with the note `part of this effect is realized as
its frozen request states`; restoring 1400 reads realized. Observation repairs
nothing:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name client-01 -- sudo -n ip link set dev bond0.140 mtu 1300
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright machine exec --context "$CONTEXT" --name client-01 -- sudo -n nmstatectl show bond0.140
./bin/bootwright machine exec --context "$CONTEXT" --name client-01 -- sudo -n ip link set dev bond0.140 mtu 1400
./bin/bootwright diff --context "$CONTEXT" --observe
```

**Bond failover.** Cut the active member of `ceph-02`'s bond, which carries
both its Ceph VLANs, while Ceph runs, then restore it:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-02 -- grep 'Currently Active Slave' /proc/net/bonding/bond0
sudo virsh --connect qemu:///system domiflist "bootwright-$CONTEXT-ceph-02"
```

```bash
ACTIVE_MAC='' # The domiflist MAC of the member reported active above.
sudo virsh --connect qemu:///system domif-setlink "bootwright-$CONTEXT-ceph-02" "${ACTIVE_MAC:?}" down
sleep 5
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-02 -- grep -E 'Currently Active Slave|MII Status' /proc/net/bonding/bond0
ceph_admin -s
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01 -- ping -c 3 203.0.113.12
sudo virsh --connect qemu:///system domif-setlink "bootwright-$CONTEXT-ceph-02" "$ACTIVE_MAC" up
```

Require the other member active, the replication VLAN over the bond still
answering and every OSD up.

**Route cut to the arbiter.** Drop everything the host forwards to or from
the arbiter's leg: `ceph-03` loses every path to the other MONs, quorum holds
on the two data-host MONs and client I/O continues; delete the two rules and
the arbiter rejoins. The rules sit in the runbook's own table, so nothing else
changes and nothing leaves the uplink:

```bash
sudo nft insert rule inet bw-e2e-trunk forward oifname "\"$TRUNK.$ARBITER_VLAN\"" counter drop
sudo nft insert rule inet bw-e2e-trunk forward iifname "\"$TRUNK.$ARBITER_VLAN\"" counter drop
sleep 30
ceph_admin -s
client roundtrip
sudo nft -a list chain inet bw-e2e-trunk forward
```

```bash
CUT_HANDLES='' # The two handles of the drop rules listed above, space-separated.
for h in ${CUT_HANDLES:?}; do sudo nft delete rule inet bw-e2e-trunk forward handle "$h"; done
ceph_admin -s    # repeat until three MONs are in quorum
```

Require `HEALTH_WARN` naming `mon.ceph-03` out of quorum with two MONs in
quorum, `0 failed` from the client, nonzero counts on both drop rules, and
three MONs in quorum again once they are gone.

**Reboot (B326).** Detach `app-01`, restart all six, repeat the address read
until each answers SSH; the declared addresses return without an apply and the
cluster recovers:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n bash e2e-sample/app-teardown.sh
./bin/bootwright machine restart --context "$CONTEXT" --name "$GUESTS" --yes
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- ip -br address show
ceph_admin -s
./bin/bootwright diff --context "$CONTEXT" --observe
```

Require `HEALTH_OK` (or only `CHA_ERROR_SENDING_REPORT`) once the OSDs rejoin
and every object realized. Attach `app-01` again with
`sudo -n bash e2e-sample/app-setup.sh` and require every store `true` in its
`/healthz`, then `client roundtrip` to report `0 failed`.

**Observation rehearsal (B1118's disk and pool steps).** Attach a disk by hand
to `app-01`, whose profile declares none, and create a pool by hand; then
`diff --observe` shows exactly those two, and `--output-dir` writes both trees:

```sh
sudo qemu-img create -f qcow2 /var/lib/libvirt/images/e2e-hand-disk.qcow2 1G
sudo virsh --connect qemu:///system attach-disk "bootwright-$CONTEXT-app-01" /var/lib/libvirt/images/e2e-hand-disk.qcow2 vdz --driver qemu --subdriver qcow2 --live
ceph_admin osd pool create e2e-hand-pool 8
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright diff --context "$CONTEXT" --observe --output-dir "$RUN_DIR/diff-tree"
cat "$RUN_DIR/diff-tree/notes.txt"
ceph_admin osd pool rm e2e-hand-pool e2e-hand-pool --yes-i-really-really-mean-it
sudo virsh --connect qemu:///system detach-disk "bootwright-$CONTEXT-app-01" vdz --live
sudo rm -f /var/lib/libvirt/images/e2e-hand-disk.qcow2
./bin/bootwright diff --context "$CONTEXT" --observe --exit-code
```

Require (B1113) the hand disk on `Machine/app-01` and (B1114) `e2e-hand-pool`
listed as a pool no applied object declares, nothing else differing, the
declared pools' size, minimum size, application and autoscale mode read
(B600), and exit `0` after both are removed. If the pool delete is refused,
set `mon_allow_pool_delete` true for the delete and false again. The release
change and the destroy-and-apply half of B1118 stay outside this case.

**File copy (B1189).** Run the
[file copy acceptance](../lab-3-nodes-ceph/runbook.md#machine-file-copy-acceptance-b1189)
with `app-01,client-01` as its two Machines. Its interrupted-connection step
cuts `app-01`'s `nic0`, which alone carries its SSH path on VLAN 110, with
`virsh domif-setlink ... down` on that MAC, and restores it after; one
member of `client-01`'s bond would only fail over.

## 10. Destroy and clean up

Detach `app-01`, then destroy as the plan directs:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n bash e2e-sample/app-teardown.sh
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright machine stop --context "$CONTEXT" --name "$GUESTS" --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright status --context "$CONTEXT"
sudo virsh --connect qemu:///system list --all
sudo virsh --connect qemu:///system net-list --all
ip -br link | grep -E "^$USER_BRIDGE\b"                       # no output
ip -4 route show "$GUEST_CIDR" table all                      # no output
systemctl list-units --all --no-legend 'bootwright-*'          # no unit of the refused subnet
sudo ls /var/lib/bootwright-hosted/                            # no directory of this context
```

Require no domain, disk, libvirt network or `virbr-user` of the context;
(B1215) no unreachable route and no unit holding one; and (B1221) the
context's hosted directory removed. The trunk and its table remain: the
runbook made them. Then:

```sh
EVIDENCE_CONTEXT="$CONTEXT"
```

Complete [Preserve evidence before context purge](../README.md#preserve-evidence-before-context-purge),
then delete the context:

```sh
./bin/bootwright context delete --name "$CONTEXT" --purge --yes
```

**Cut-off drill (B326).** Run
[lab-network-variants step 10](../lab-network-variants/runbook.md#10-verify-a-declaration-that-cuts-the-controller-off)
now. First run the block of its
[step 1](../lab-network-variants/runbook.md#1-review-the-inputs) with all four
choices `false`, then define `verify_selected_features` and
`verify_recovery_passphrase` from its
[step 7](../lab-network-variants/runbook.md#verify-the-selected-features-on-each-guest)
and [step 8](../lab-network-variants/runbook.md#8-check-drift-and-reboot):
under `set +e` a missing function only prints `command not found`. Its
one-guest context uses only a managed network, so it needs no trunk, and the
listener firewall rules are still in place.

Then clean up in this order: remove the routing table and the trunk, complete
[Restore listener firewall](../README.md#restore-listener-firewall), which
reads its record from `RUN_DIR`, and
[Remove diagnostic tools](../README.md#remove-diagnostic-tools), and only then
remove the private directory:

```sh
sudo nft delete table inet bw-e2e-trunk
sudo nmcli connection down "$TRUNK.$PUBLIC_VLAN"
sudo nmcli connection down "$TRUNK.$ARBITER_VLAN"
sudo nmcli connection down "$TRUNK"
sudo nmcli connection delete "$TRUNK.$PUBLIC_VLAN" "$TRUNK.$ARBITER_VLAN" "$TRUNK"
```

```sh
rm -rf -- "${RUN_DIR:?}" "${INPUT_DIR:?}"
```

Keep the verified archives under `.cache/e2e-evidence` for owner review.
Forwarding stays on: libvirt keeps it on for its own networks.

## 11. Record the result

Record the build, the release, every check of steps 3 to 10 and the completed
cleanup in the [acceptance ledger](../../docs/acceptance.md). Only the owner
accepts. A missed interruption, a drill that did not fire or an unexpected
failure leaves the run an observation. Keep host-specific evidence private.

| Item | Evidence this run supplies |
| --- | --- |
| [B74](../../specs/milestones/m1.md#b74) | The bond (`client-01`), VLAN (`ceph-01`, `ceph-03`, `ceph-04`, `app-01`) and VLAN-over-bond (`ceph-02`) install shapes, MAC pins, the 802.3ad refusal, links and routes, reboot and the install Ctrl-C and replay |
| [B326](../../specs/milestones/m1.md#b326) | Final network on six shapes, reboot, MTU drift and restoration, the convergence Ctrl-C and replay, and lab-network-variants' cut-off drill |
| [B1266](../../specs/milestones/m2.md#b1266) | The MON-only `ceph-03`, holding no replication address, admitted and running a MON alone on its routed subnet |
| [B1213](../../specs/milestones/m1.md#b1213) | The machines apply admitted and the storage preflight passing after it, or a refusal naming each byte group |
| [B1215](../../specs/milestones/m1.md#b1215) | A data host's probe of the guest-only VLAN refused on the host with nothing on the uplink, and a removal leaving no unit or route |
| [B1020](../../specs/milestones/m2.md#b1020) | `diff --observe`'s Ceph observation answering with the seed MON stopped and quorum held by `ceph-02` and the routed arbiter |
| [B1044](../../specs/milestones/m2.md#b1044) | The package tree's `.treeinfo` proof made again after the first reset, shown in its retained output |
| [B1036](../../specs/milestones/m2.md#b1036) | An installation's listener fetch made again after the second reset, shown in its retained output |
| [B1189](../../specs/milestones/m1.md#b1189) | The file-copy runbook on `app-01` and `client-01` |
| [B1221](../../specs/milestones/m1.md#b1221) | The context's `/var/lib/bootwright-hosted/` directory during the run and its removal at destroy |
| [B1223](../../specs/milestones/m1.md#b1223) | `logs --follow` beside each apply, which never waits on it |

Step 9's observation drill rehearses [B1118](../../specs/milestones/m1.md#b1118)
without its release change or its destroy and apply, so it accepts none.
