# lab-5-nodes-ceph-network-matrix

The networking matrix on one laptop: IBM Storage Ceph on three RHEL 9.8
guests that each install over a different interface shape, with replication on
its own VLAN, and a sample server and its client that install over VLANs on an
operator trunk. Every install shape of
[lab-network-variants](../lab-network-variants/runbook.md) runs here on a
guest that carries real traffic, beside the managed Networks, the refused
cluster subnet and the segregation checks of
[lab-5-nodes-ceph-networks](../lab-5-nodes-ceph-networks/runbook.md), whose
sample server and client this case reuses. Context
`lab-5-nodes-ceph-network-matrix`; 5 guests, 13 vCPUs, 24 GiB.

```text
             this host: libvirt substrate + controller (DNS, NTP, proxy, artifacts on CONTROLLER_ADDRESS)
     ┌──────────────────────────────┴────────────────────────────────────┐
  virbr-ceph  (managed Network ceph-public, NAT)                   br-nm  (operator trunk, no port)
   ├ untagged  198.51.100.0/26  host .1   Ceph public              ├ VLAN 130  198.51.100.64/26  host leg br-nm.130 .65, shared NAT
   └ VLAN 120  203.0.113.0/26   Ceph cluster: no host address,     └ VLAN 140  203.0.113.64/26   guest-only, MTU 1400, no host address
                                refused on the host

  ceph-01    nic0 .11                     nic0.120 203.0.113.11            plain NIC
  ceph-02    bond0(nic0,nic1) .12         bond0.120 203.0.113.12           bond install, VLAN over the bond
  ceph-03    nic0 .13                     nic1.120 203.0.113.13            cluster VLAN on an addressless second NIC
  app-01     nic0.130 198.51.100.70       nic0.140 203.0.113.70  nic1 198.51.100.21 (ceph-public)   VLAN install
  client-01  bond0.130 198.51.100.80      bond0.140 203.0.113.80                                    VLAN-over-bond install
  RGW ingress VIP 198.51.100.60/26 (TLS 443, name s3.e2e.example.test, no DNS record)
```

| Machine | Install interface | Segment | Converged after the install |
| --- | --- | --- | --- |
| `ceph-01` | `nic0` | `ceph-public` | `nic0.120` |
| `ceph-02` | `bond0`, active-backup over `nic0` and `nic1` | `ceph-public` twice | `bond0.120` over the bond |
| `ceph-03` | `nic0` | `ceph-public` twice | addressless `nic1` carrying `nic1.120` |
| `app-01` | `nic0.130` over `nic0` | `app-trunk`, then `ceph-public` | `nic0.140` MTU 1400, `nic1` on the Ceph public network, a 192.0.2.0/24 route |
| `client-01` | `bond0.130` over `bond0` over `nic0` and `nic1` | `app-trunk` twice | `bond0.140` MTU 1400, a 192.0.2.0/24 route |

| Network | Object | Realized as | Purpose |
| --- | --- | --- | --- |
| Ceph public | `ceph-public`, managed, NAT | libvirt network on `virbr-ceph` | install, SSH, MON, OSD front, RGW ingress |
| Ceph cluster | `ceph-cluster`, VLAN 120, provided | tagged on `virbr-ceph`; its subnet refused on the host | OSD replication and heartbeats |
| User | `app-users`, VLAN 130, provided | tagged on the operator trunk `br-nm`, host leg `br-nm.130` | install of `app-01` and `client-01`; HTTP to the sample server |
| Guest-only | `app-guest-only`, VLAN 140, provided | tagged on `br-nm`, no host leg | converged second network of `app-01` and `client-01` |

A VLAN install interface needs its gateway on that VLAN, and Bootwright never
gives the host an address on a tagged Network
([provider host realization](../../specs/substrates.md#provider-host-realization)),
so the user VLAN rides on an operator trunk the runbook creates, as
lab-network-variants does. The [acceptance table](#11-record-the-result)
names what one complete run supplies.

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
CEPH_BRIDGE=virbr-ceph; TRUNK=br-nm; USER_VLAN=130; GUEST_VLAN=140
PUBLIC_CIDR=198.51.100.0/26; CLUSTER_CIDR=203.0.113.0/26; CLUSTER_VLAN=120
USER_GATEWAY=198.51.100.65; USER_CIDR=198.51.100.64/26; GUEST_CIDR=203.0.113.64/26
APP_PUBLIC=198.51.100.21; APP_USER=198.51.100.70; APP_GUEST=203.0.113.70; APP_PORT=8080
CLIENT=198.51.100.80; CLIENT_GUEST=203.0.113.80
OBJECT_VIP=198.51.100.60; DOMAIN=e2e.example.test
CLIENT_IMAGE=registry.access.redhat.com/ubi9/python-312:latest
CASE_DIR="$PWD/e2e/lab-5-nodes-ceph-network-matrix"
APP_DIR="$PWD/e2e/lab-5-nodes-ceph-networks/app"
CONTEXT="lab-5-nodes-ceph-network-matrix"; INPUT_DIR="$HOME/bootwright-e2e/$CONTEXT"
GUESTS=ceph-01,ceph-02,ceph-03,app-01,client-01
```

## 2. Prepare the host and the trunk

Complete [host preparation](../README.md#prepare-the-host), including the RHEL
9.8 boot and DVD media and the IBM entitlement key file, then return here.
Record a missing `tcpdump` or `dnsmasq` for removal with the other diagnostic
packages; NetworkManager's sharing on the trunk leg needs `dnsmasq`:

```bash
for p in tcpdump dnsmasq; do
    if ! rpm -q "$p" >/dev/null; then
        printf '%s\n' "$p" >> "${HOST_CHECKS:?Run the diagnostic tools step of the host preparation first}/added"
        sudo dnf install --setopt=install_weak_deps=False "$p"
    fi
done
command -v tcpdump dnsmasq python3
```

The [capacity gate](../../specs/substrates.md#capacity-admission) counts three
Ceph guests (3 vCPU, 6 GiB, 25 GiB root and a 20 GiB OSD disk each), `app-01`
(2 vCPU, 2 GiB, 15 GiB), `client-01` (1 vCPU, 1.5 GiB, 15 GiB) and the reserve
(1 vCPU, 2 GiB, 10 GiB): 13 host CPUs, 24 GiB of `MemAvailable` and 175 GiB
free under `/var/lib/libvirt`, plus about 31 GB (29 GiB) for the package tree
and the five guests' installer media
([disk headroom](../README.md#end-to-end-cases)), so about 204 GiB in all.
Close large applications first: a 32 GiB laptop leaves at most about 28 GiB.
The host's ports 53, 123, 3128, 8080 and 8443 on its address and loopback 8000
to 8004 must be free. No other case may be applied. Require the two
capacity numbers and no output from each check:

```sh
grep MemAvailable /proc/meminfo; df -BG --output=avail /var/lib/libvirt
ip -br link | grep -E "^($CEPH_BRIDGE|$TRUNK)(\.|\b)"
ip -4 route | grep -E '198\.51\.100\.|203\.0\.113\.|192\.0\.2\.'
sudo virsh --connect qemu:///system list --all --name | grep bootwright-
sudo virsh --connect qemu:///system net-list --all --name | grep bootwright-
systemctl list-units --all --no-legend 'bootwright-*'
```

Create the operator trunk: an isolated bridge with no physical port and one
VLAN leg holding the user network's gateway, shared to the uplink by
NetworkManager's NAT. VLAN 140 gets no leg: nothing on the host joins it.
Bootwright neither creates nor removes these connections. Inspect the
prerequisites first: no DNS or DHCP listener on 198.51.100.65 or a wildcard,
and an enabled NetworkManager `firewall-backend` (`nftables` or `iptables`,
automatic selection included; not `none`):

```sh
sudo ss -lntup '( sport = :53 or sport = :67 )'
sudo NetworkManager --print-config | grep -i firewall-backend
```

```sh
sudo nmcli connection add type bridge ifname "$TRUNK" con-name "$TRUNK" bridge.stp no ipv4.method disabled ipv6.method disabled connection.zone trusted
sudo nmcli connection add type vlan ifname "$TRUNK.$USER_VLAN" con-name "$TRUNK.$USER_VLAN" vlan.parent "$TRUNK" vlan.id "$USER_VLAN" ipv4.method shared ipv4.addresses "$USER_GATEWAY/26" ipv6.method disabled connection.zone trusted
sudo nmcli connection up "$TRUNK"
sudo nmcli connection up "$TRUNK.$USER_VLAN"
ip -br address show "$TRUNK.$USER_VLAN"
sudo firewall-cmd --get-zone-of-interface="$TRUNK.$USER_VLAN"
```

Require 198.51.100.65/26 on `br-nm.130` and `trusted`.

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

The plan shows, for each Machine, the impacts `configure-network` and
`restart`. The bond refusal (B74): render `bond.mode=802.3ad` into a second
directory, which validates, import it and plan:

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
sudo virsh --connect qemu:///system net-dumpxml "bootwright-$CONTEXT-ceph-public"
ip -br address show "$CEPH_BRIDGE"; sudo firewall-cmd --get-zone-of-interface="$CEPH_BRIDGE"
ip -4 route show "$CLUSTER_CIDR" table all
systemctl list-units --all --no-legend 'bootwright-*'
sudo ls /var/lib/bootwright-hosted/"$CONTEXT"
```

Require the libvirt network active with `<forward mode='nat'>` on `virbr-ceph`
and the host address 198.51.100.1/26, `trusted` for the bridge, and (B1215)
an `unreachable 203.0.113.0/26` route held by a Bootwright unit the listing
shows: the host refuses the cluster subnet instead of routing it out of its
uplink. Require (B1221) the context's directory under
`/var/lib/bootwright-hosted/`, holding the served content and the emulated BMC
state. Nothing of Bootwright's is on `br-nm`.

## 5. Install the guests through both interruption drills

Two drills interrupt this stage (B74 and B326); a third, the transport-loss
drill, rides on the first apply. A second terminal follows each apply (B1223).
Widen the terminal until the progress descriptions are readable in full. The
five guests install in parallel: note each one's progress when interrupting.
Missing either interruption requires a new complete run.

**Second terminal (B1223).** A follow started while no operation runs prints
the newest finished one and ends, so start it in the checkout after each apply
below has printed its first progress row, once per apply, the resumed ones
included:

```sh
./bin/bootwright logs --context lab-5-nodes-ceph-network-matrix --follow
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
for b in os-package-tree-rhel os-install-ceph-01 os-install-ceph-02 os-install-ceph-03 os-install-app-01 os-install-client-01; do
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

## 6. Verify the install shapes and the final network

B74, the shapes. One active profile per declared interface; `active-backup`
over `nic0` and `nic1` on `ceph-02` and `client-01`; the VLANs on their
parents; every interface name pinned to its MAC:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- nmcli -t -f NAME,DEVICE,TYPE,STATE connection show --active
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-02,client-01 -- cat /proc/net/bonding/bond0
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- ip -d -o link show type vlan
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- ip -br link show
for m in ceph-01 ceph-02 ceph-03 app-01 client-01; do sudo virsh --connect qemu:///system domiflist "bootwright-$CONTEXT-$m"; done
```

Match each bond's `Permanent HW addr` under `Slave Interface: nic0` and
`nic1` to its `domiflist` MACs in order, and each unbonded NIC's MAC in
`ip -br link` to its `domiflist` MAC. Require `vlan protocol 802.1Q id 120`
on `nic0.120`, `bond0.120` and `nic1.120`, `id 130` and `id 140` on `app-01`'s
`nic0` and `client-01`'s `bond0`, and `nic1` of `ceph-03` without an address.

B326, the final network. The converged addresses, MTU 1400 on both VLAN 140
interfaces, the default and 192.0.2.0/24 routes, the search domain, a
network marker per guest and a boot ID that differs from the marker's:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- ip -br address show
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- ip route show
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ip -o link show nic0.140
./bin/bootwright machine exec --context "$CONTEXT" --name client-01 -- ip -o link show bond0.140
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- cat /etc/resolv.conf
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- cat /etc/bootwright/network-marker.json
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- cat /proc/sys/kernel/random/boot_id
./bin/bootwright machine info --context "$CONTEXT" --name "$GUESTS"
./bin/bootwright diff --context "$CONTEXT" --observe
```

Require lifecycle `applied` and every Machine realized. Then the guest-only
VLAN: `app-01` and `client-01` reach each other on it at full MTU and no
further, and the Ceph hosts never see it:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ping -c 3 -M do -s 1372 "$CLIENT_GUEST"
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ping -c 2 -W 2 -M do -s 1373 "$CLIENT_GUEST"
./bin/bootwright machine exec --context "$CONTEXT" --name client-01 -- ip route get 192.0.2.10
```

Require the 1372-byte payload (1400 bytes on the wire) to pass and the
1373-byte one to fail with `message too long`, and the 192.0.2.0/24 route
through 198.51.100.65 on `bond0.130`.

## 7. Build and verify the cluster

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name "$GUESTS" -- getent hosts cp.icr.io public.dhe.ibm.com
./bin/bootwright preflight --context "$CONTEXT" --stage clusters
./bin/bootwright apply --context "$CONTEXT" --stage clusters --yes
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright cluster info --context "$CONTEXT" --name ceph-e2e
```

Require the storage preflight to pass after the machines stage, its
`capacity.disk` check included (B1213), and the IBM hosts to
resolve on every guest, the trunk guests through `br-nm.130`'s NAT. Run the
second terminal's follower beside this apply too. Then set `ceph_admin` and
require the cluster state exactly as in
[lab-5-nodes-ceph-networks step 4](../lab-5-nodes-ceph-networks/runbook.md#4-verify-the-cluster),
using its `CEPH_CONF`, `CEPH_KEYRING` and `ceph_admin` definitions with
`--node ceph-01`.

**Seed MON loss (B1020).** `ceph-01` bootstrapped the cluster, and the
retained configuration names only its MON. Stop that MON, run Bootwright's
admin path while quorum holds, then start it again:

```bash
FSID='' # The cluster FSID cluster info printed.
MON_UNIT="ceph-${FSID:?Set FSID from cluster info}@mon.ceph-01.service"
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node ceph-01 -- sudo -n systemctl stop "$MON_UNIT"
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node ceph-01 -- systemctl is-active "$MON_UNIT"
ceph_admin -m 198.51.100.12,198.51.100.13 quorum_status --format json-pretty
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright cluster exec --context "$CONTEXT" --name ceph-e2e --node ceph-01 -- sudo -n systemctl start "$MON_UNIT"
ceph_admin -s    # repeat until three MONs are in quorum
```

`ceph_admin` names the two other MONs explicitly: its retained
configuration names only the seed. Require `inactive` for the unit,
`quorum_names` to list `ceph-02` and `ceph-03` only, and `diff --observe` to
observe the Ceph objects, none `unknown` and none timed out, while the seed is
down: its Ceph observation runs Bootwright's admin clients, which name every
MON of the frozen request (`cluster info` reads local evidence only and proves
nothing here). The stopped MON may read as drift. Then require
`HEALTH_OK` again with three MONs after the start.

## 8. Run the workload and validate the segregation

Deploy the sample server and run the client as
[lab-5-nodes-ceph-networks steps 5 and 6](../lab-5-nodes-ceph-networks/runbook.md#5-deploy-the-sample-server)
say, with these differences: copy the scripts from `APP_DIR`, not that case's
directory; `app-01` reaches the stores from `nic1` (198.51.100.21) and serves
on `nic0.130` (198.51.100.70); and `client-01` pulls its image through the
trunk leg's NAT. Require `0 failed` from the round trip, the canary write,
the power cycle and the canary check, and every `sourceAddress` 198.51.100.21.
Keep the canaries.

**Ceph uses the declared networks.** Run the MON and OSD address check of
[lab-5-nodes-ceph-networks step 7](../lab-5-nodes-ceph-networks/runbook.md#ceph-uses-the-declared-networks)
unchanged: replication and back heartbeats on VLAN 120 on all three install
shapes.

**Interfaces and routes:**

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ip route get 198.51.100.11
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- ip route get "$CLIENT"
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sysctl net.ipv4.ip_forward
./bin/bootwright machine exec --context "$CONTEXT" --name app-01 -- sudo -n ss -Htlnp sport = :8080
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-01 -- ping -c 3 203.0.113.12
./bin/bootwright machine exec --context "$CONTEXT" --name ceph-02 -- ping -c 3 203.0.113.13
ip -4 -br address | grep -cE '203\.0\.113\.'
```

Require `app-01` to route the MONs via `nic1` and `client-01` via `nic0.130`,
not to forward (`= 0`), to listen on 8080 only on 198.51.100.70; the cluster
VLAN to pass between the plain, bonded and split Ceph hosts; and the host to
hold no 203.0.113.0/24 address (`0`).

**What cannot reach what.** Define `expect_fail` and `on` as in
[lab-5-nodes-ceph-networks](../lab-5-nodes-ceph-networks/runbook.md#what-cannot-reach-what),
prove the sessions, then require each probe to fail and the last to answer:

```bash
./bin/bootwright machine exec --context "$CONTEXT" --name app-01,ceph-01,client-01 -- true
expect_fail on client-01 ping -c 2 -W 2 198.51.100.11                                                          # client to a Ceph host
expect_fail on client-01 curl -sS --noproxy '*' --max-time 5 -o /dev/null -k "https://$OBJECT_VIP/"           # client to the object VIP
expect_fail on client-01 curl -sS --noproxy '*' --max-time 5 -o /dev/null "http://$APP_PUBLIC:$APP_PORT/healthz"   # client to the server's Ceph address
expect_fail on client-01 curl -sS --noproxy '*' --max-time 5 -o /dev/null "http://$APP_GUEST:$APP_PORT/healthz"    # the server listens on its user address only
expect_fail on client-01 ping -c 2 -W 2 203.0.113.11                                                           # client to the cluster VLAN
expect_fail on ceph-01 ping -c 2 -W 2 "$CLIENT"                                                                # a Ceph host to the client
expect_fail on ceph-01 ping -c 2 -W 2 "$APP_GUEST"                                                             # a Ceph host to the guest-only VLAN
expect_fail on ceph-01 curl -sS --noproxy '*' --max-time 5 -o /dev/null "http://$APP_USER:$APP_PORT/healthz"   # a Ceph host to the server's user address
expect_fail on app-01 ping -c 2 -W 2 203.0.113.11                                                              # app-01 to the cluster VLAN
on client-01 curl -fsS --noproxy '*' --max-time 5 "http://$APP_USER:$APP_PORT/healthz"                         # client to the server: answers
```

**The refused cluster subnet (B1215).** The cluster VLAN probes above must be
refused on the host, not sent out of its uplink. Count what leaves the uplink
toward 203.0.113.0/26 while `app-01` probes it once more:

```bash
UPLINK=$(ip -4 route get 192.0.2.1 | sed -n 's/.* dev \([^ ]*\).*/\1/p')
sudo -v
sudo timeout 15 tcpdump -i "$UPLINK" -nn -c 50 -w "$RUN_DIR/uplink.pcap" "net $CLUSTER_CIDR" &
sleep 2
on app-01 ping -c 3 -W 2 203.0.113.11
wait
sudo tcpdump -r "$RUN_DIR/uplink.pcap" -nn 2>/dev/null | wc -l
ip -4 route show "$CLUSTER_CIDR" table all
```

Require the ping to report `From 198.51.100.65 ... Destination Host
Unreachable`, from the host's leg `app-01` routes it to by default, and `0`
packets on the uplink: the observation of 2026-11-08 saw them leave through
NAT. The unreachable route refuses the subnet whichever host interface the
probe enters by. The guest-only VLAN 140 rides an operator segment, so the
host does not refuse it: the Ceph host's probe of 203.0.113.70 above fails
only because nobody beyond the uplink answers.

Read the rules, read-only:

```sh
sudo nft list chain ip libvirt_network forward
sudo nft list chain ip libvirt_network guest_input
sudo nft list ruleset | grep -n -A3 "$TRUNK.$USER_VLAN"
```

Require the `guest_input` reject rule for `virbr-ceph` to show packet counts
(the client's probes to the Ceph network), and NetworkManager's sharing rules
for `br-nm.130` to admit only established traffic into it (the Ceph host's
probes to the user network).

**Traffic on the wire.** Capture both bridges while the client runs once more.
The trunk carries both of its VLANs tagged and nothing untagged:

```bash
sudo -v
sudo timeout 150 tcpdump -i "$CEPH_BRIDGE" -nn -s 128 -Z root -w "$RUN_DIR/ceph-public.pcap" &
sudo timeout 150 tcpdump -i "$TRUNK" -nn -s 128 -Z root -w "$RUN_DIR/trunk.pcap" &
sleep 5
client roundtrip
on app-01 ping -c 5 -s 1372 "$CLIENT_GUEST"
wait
count() { sudo tcpdump -r "$1" -nn "$2" 2>/dev/null | wc -l; }
P="$RUN_DIR/ceph-public.pcap"; T="$RUN_DIR/trunk.pcap"
printf '%-72s %s\n' \
  'ceph-public: replication on VLAN 120 (OSD ports)                  want > 0' "$(count "$P" "vlan $CLUSTER_VLAN and tcp portrange 6800-7568")" \
  'ceph-public: VLAN 120 with an address outside 203.0.113.0/26       want 0' "$(count "$P" "vlan $CLUSTER_VLAN and ip and not (src net $CLUSTER_CIDR and dst net $CLUSTER_CIDR)")" \
  'ceph-public: 802.1Q frames, all                            equal to next' "$(count "$P" 'vlan')" \
  'ceph-public: 802.1Q frames on VLAN 120                    equal to above' "$(count "$P" "vlan $CLUSTER_VLAN")" \
  'ceph-public: app-01 to the MONs, untagged                        want > 0' "$(count "$P" "host $APP_PUBLIC and (tcp port 6789 or tcp port 3300) and not vlan")" \
  'ceph-public: app-01 to the object VIP, untagged                  want > 0' "$(count "$P" "host $APP_PUBLIC and host $OBJECT_VIP and tcp port 443 and not vlan")" \
  'ceph-public: a trunk-network address, untagged                     want 0' "$(count "$P" "ip and (net $USER_CIDR or net $GUEST_CIDR) and not vlan")" \
  'ceph-public: a trunk-network address, tagged                       want 0' "$(count "$P" "vlan and ip and (net $USER_CIDR or net $GUEST_CIDR)")" \
  'trunk:       HTTP between client-01 and app-01 on VLAN 130       want > 0' "$(count "$T" "vlan $USER_VLAN and tcp port $APP_PORT and host $CLIENT and host $APP_USER")" \
  'trunk:       the 1400-byte echo on VLAN 140                      want > 0' "$(count "$T" "vlan $GUEST_VLAN and icmp and host $APP_GUEST and host $CLIENT_GUEST")" \
  'trunk:       VLAN 140 with an address outside 203.0.113.64/26      want 0' "$(count "$T" "vlan $GUEST_VLAN and ip and not (src net $GUEST_CIDR and dst net $GUEST_CIDR)")" \
  'trunk:       untagged IPv4                                         want 0' "$(count "$T" 'ip and not vlan')" \
  'trunk:       Ceph ports (MON, OSD, S3), any VLAN                   want 0' "$(count "$T" 'vlan and (tcp port 6789 or tcp port 3300 or tcp portrange 6800-7568 or tcp port 443)')" \
  'trunk:       802.1Q frames on another VLAN than 130 or 140         want 0' "$(count "$T" "ether[12:2] = 0x8100 and (ether[14:2] & 0x0fff) != $USER_VLAN and (ether[14:2] & 0x0fff) != $GUEST_VLAN")"
```

Require every count as marked; a filter names `vlan` last when it reads
untagged frames, as lab-5-nodes-ceph-networks explains. and never twice: each
`vlan` moves the next fields by one more tag. The last row reads the outer tag
by absolute offsets for that reason. Then remove
the canaries with `client clean-canary`.

## 9. Drift, reboot and observation drills

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

**Bond failover.** Cut the active member of `ceph-02`'s bond at the host while
Ceph runs, then restore it:

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

Require the other member active, the cluster VLAN over the bond still
answering and every OSD up.

**Reboot (B326).** Restart all five, repeat the address read until each
answers SSH; the declared addresses return without an apply and the cluster
recovers:

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
(B600), and
exit `0` after both are removed. If the pool delete is refused, set
`mon_allow_pool_delete` true for the delete and false again. The release
change and the destroy-and-apply half of B1118 stay outside this case.

**File copy (B1189).** Run the
[file copy acceptance](../lab-3-nodes-ceph/runbook.md#machine-file-copy-acceptance-b1189)
with `app-01,client-01` as its two Machines. Its interrupted-connection step
cuts `app-01`'s `nic0`, which alone carries its SSH path on VLAN 130, with
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
ip -br link | grep -E "^$CEPH_BRIDGE\b"                       # no output
ip -4 route show "$CLUSTER_CIDR" table all                     # no output
systemctl list-units --all --no-legend 'bootwright-*'          # no unit of the refused subnet
sudo ls /var/lib/bootwright-hosted/                            # no directory of this context
```

Require no domain, disk, libvirt network or `virbr-ceph` of the context;
(B1215) no unreachable route and no unit holding one; and (B1221) the
context's hosted directory removed. `br-nm` remains: the runbook made it.
Then:

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

Then clean up in this order: remove the trunk, complete
[Restore listener firewall](../README.md#restore-listener-firewall), which
reads its record from `RUN_DIR`, and
[Remove diagnostic tools](../README.md#remove-diagnostic-tools), and only then
remove the private directory:

```sh
sudo nmcli connection down "$TRUNK.$USER_VLAN"
sudo nmcli connection down "$TRUNK"
sudo nmcli connection delete "$TRUNK.$USER_VLAN" "$TRUNK"
```

```sh
rm -rf -- "${RUN_DIR:?}" "${INPUT_DIR:?}"
```

Keep the verified archives under `.cache/e2e-evidence` for owner review.

## 11. Record the result

Record the build, the release, every check of steps 3 to 10 and the completed
cleanup in the [acceptance ledger](../../docs/acceptance.md). Only the owner
accepts. A missed interruption, a drill that did not fire or an unexpected
failure leaves the run an observation. Keep host-specific evidence private.

| Item | Evidence this run supplies |
| --- | --- |
| [B74](../../specs/milestones/m1.md#b74) | The bond (`ceph-02`), VLAN (`app-01`) and VLAN-over-bond (`client-01`) install shapes, MAC pins, the 802.3ad refusal, links and routes, reboot and the install Ctrl-C and replay |
| [B326](../../specs/milestones/m1.md#b326) | Final network on five shapes, reboot, MTU drift and restoration, the convergence Ctrl-C and replay, and lab-network-variants' cut-off drill |
| [B1213](../../specs/milestones/m1.md#b1213) | The machines apply admitted and the storage preflight passing after it, or a refusal naming each byte group |
| [B1215](../../specs/milestones/m1.md#b1215) | The guests' probes of the cluster VLAN refused on the host with nothing on the uplink, and a removal leaving no unit or route |
| [B1020](../../specs/milestones/m2.md#b1020) | `diff --observe`'s Ceph observation answering with the seed MON stopped and quorum held |
| [B1044](../../specs/milestones/m2.md#b1044) | The package tree's `.treeinfo` proof made again after the first reset, shown in its retained output |
| [B1036](../../specs/milestones/m2.md#b1036) | An installation's listener fetch made again after the second reset, shown in its retained output |
| [B1189](../../specs/milestones/m1.md#b1189) | The file-copy runbook on `app-01` and `client-01` |
| [B1221](../../specs/milestones/m1.md#b1221) | The context's `/var/lib/bootwright-hosted/` directory during the run and its removal at destroy |
| [B1223](../../specs/milestones/m1.md#b1223) | `logs --follow` beside each apply, which never waits on it |

Step 9's observation drill rehearses [B1118](../../specs/milestones/m1.md#b1118)
without its release change or its destroy and apply, so it accepts none.
