# lab-network-variants

Three RHEL 9.8 guests, each installing over a different install interface and
then converging to the rest of its declared network, beside managed DNS, NTP,
proxy and artifact services on the controller. Context `lab-network-variants`;
3 guests of 2 vCPU, 4 GiB and a 40 GiB disk, each behind its own emulated
Redfish BMC on loopback ports 8000 to 8002. Bond mode is `active-backup`, the
only mode a libvirt Machine admits.

| Machine | Install interface | Attachment | Install address | Gateway | Guest-only VLAN 140 |
| --- | --- | --- | --- | --- | --- |
| `nv-bond` | `bond0` over `nic0` and `nic1` | `nv-lab`, managed, bridge `virbr-e2e` | 198.51.100.11/26 | 198.51.100.1 | `bond0.140`, 203.0.113.11/24, MTU 1400 |
| `nv-vlan` | `nic0.120` over `nic0` | `nv-trunk`, external, bridge `br-nv` | 198.51.100.71/26 | 198.51.100.65 | `nic0.140`, 203.0.113.12/24, MTU 1400 |
| `nv-vlan-bond` | `bond0.130` over `bond0` over `nic0` and `nic1` | `nv-trunk` | 198.51.100.141/26 | 198.51.100.129 | `bond0.140`, 203.0.113.13/24, MTU 1400 |

Each Machine also routes 192.0.2.0/24 through its gateway on its install
interface and searches `e2e.example.test`. The MTU is a Machine override beside
its addresses; the routes stay in each NetworkConfig. Every Machine installs,
converges to its declared network and power-cycles once.

It accepts [B74](../../specs/milestones/m1.md#b74) (bond, VLAN and
VLAN-over-bond installation) and [B326](../../specs/milestones/m1.md#b326)
(the final network, reboot and interrupted convergence).

## 1. Review the inputs

| `--set` | Default | Effect |
| --- | --- | --- |
| `bond.mode=802.3ad` | `active-backup` | A mode the libvirt Machine refuses: the refusal drill |
| `name=nv-cutoff` with `single=true` | `lab-network-variants`, `false` | The single-Machine context of the cut-off drill: only `nv-bond`, on the managed network |
| `cutoff=true` | `false` | With `single`: a route that sends the controller's replies the wrong way |

[values.yaml](values.yaml) holds the rest: addresses, upstreams, media names,
guest size, the VLAN ids and networks and the converged network.

## 2. Prepare the host and external bridge

Complete [host preparation](../README.md#prepare-the-host), including diagnostic
tools and both RHEL ISOs, then return here. Free the loopback ports 8000 to 8002. The
managed network 198.51.100.0/26 on `virbr-e2e` must not exist yet: the
`substrates` stage creates it and destroy removes it, and Bootwright places it
in firewalld's `trusted` zone
([provider host realization](../../specs/substrates.md#provider-host-realization)),
for guest traffic. The listener firewall check still follows the controller
stage below.

Capacity, which the apply gate counts
([capacity admission](../../specs/substrates.md#capacity-admission)): the
three guests plus the provider's `capacityReserve` (1 CPU, 2 GiB, 10 GiB)
need 7 host CPUs not reserved by other libvirt domains, 14 GiB of
`MemAvailable` in `/proc/meminfo` (swap never counts) and 130 GiB free on the
filesystem backing `/var/lib/libvirt/images`, beside the media. Check with
`nproc`, `grep MemAvailable /proc/meminfo` and
`df -h /var/lib/libvirt/images`.

The VLAN Machines use an operator-created bridge. Require `br-nv`,
`br-nv.120` and `br-nv.130` to be absent before starting. Create these isolated
connections without a physical port; their `trusted` zone admits the lab
VLANs to host services. Do not attach this bridge to an external network.
Bootwright neither creates nor removes these connections.

```sh
sudo nmcli connection add type bridge ifname br-nv con-name br-nv bridge.stp no ipv4.method disabled ipv6.method disabled connection.zone trusted
sudo nmcli connection add type vlan ifname br-nv.120 con-name br-nv.120 vlan.parent br-nv vlan.id 120 ipv4.method manual ipv4.addresses 198.51.100.65/26 ipv6.method disabled connection.zone trusted
sudo nmcli connection add type vlan ifname br-nv.130 con-name br-nv.130 vlan.parent br-nv vlan.id 130 ipv4.method manual ipv4.addresses 198.51.100.129/26 ipv6.method disabled connection.zone trusted
sudo nmcli connection up br-nv
sudo nmcli connection up br-nv.120
sudo nmcli connection up br-nv.130
ip -br address show
sudo firewall-cmd --get-zone-of-interface=br-nv.120
sudo firewall-cmd --get-zone-of-interface=br-nv.130
```

Require both VLAN addresses above and `trusted` for both zone queries. These
host commands need verification on the test host; no live result is claimed.

## 3. Render and initialize

```sh
umask 077
RUN_DIR=$(mktemp -d)
e2e/render.sh lab-network-variants
CONTEXT="lab-network-variants"
INPUT_DIR="$HOME/bootwright-e2e/lab-network-variants"
CONTROLLER_ADDRESS=$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')
./bin/bootwright validate -f "$INPUT_DIR"
./bin/bootwright context init --name "$CONTEXT" --input-dir "$INPUT_DIR"
./bin/bootwright secret generate --context "$CONTEXT"
./bin/bootwright secret check --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT"
```

## 4. Verify the unsupported bond mode is refused

A libvirt Machine admits only `active-backup`; render the case with `bond.mode=802.3ad` into a
second directory, which validates, and import it:

```sh
e2e/render.sh lab-network-variants --set bond.mode=802.3ad --out-dir ~/bootwright-e2e/lab-network-variants-mode
./bin/bootwright context update --name "$CONTEXT" --input-dir ~/bootwright-e2e/lab-network-variants-mode --yes
./bin/bootwright plan --context "$CONTEXT"
```

Require the plan to report "a libvirt bridge qualifies only active-backup
bonding, and bond bond0 of Machine/nv-bond declares mode 802.3ad". This is the
expected negative test. Restore the valid declaration before proceeding:

```sh
./bin/bootwright context update --name "$CONTEXT" --input-dir "$INPUT_DIR" --yes
```

## 5. Prepare the controller and services

```sh
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
```

Set the listener ports, then complete
[Prepare listener firewall](../README.md#prepare-listener-firewall).

```bash
FIREWALL_PORTS=(53/tcp 53/udp 123/udp 3128/tcp 8080/tcp 8443/tcp)
```

Continue only when the following preflight reports `ready`:

```sh
./bin/bootwright preflight controller --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services --yes
```

The plan shows the group `converge-network` and, for each Machine, the impacts
`configure-network` and `restart`, each followed by the Machine name. The services answer on
the host address `CONTROLLER_ADDRESS`:

```sh
curl -sk "https://$CONTROLLER_ADDRESS:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
dig @"$CONTROLLER_ADDRESS" +short controller.e2e.example.test
chronyd -Q -t 10 "server $CONTROLLER_ADDRESS iburst port 123"
```

## 6. Install with the required interruption drills

Run the apply below with these two deliberate interruptions (B74 and B326):

- Interrupted install (B74): press Ctrl-C once during an installer wait, then
  repeat `./bin/bootwright apply --context "$CONTEXT" --yes`. The install replays and completes.
- Interrupted convergence (B326): press Ctrl-C once while progress shows
  `converge-network` for a Machine, then run `./bin/bootwright status --context "$CONTEXT"` and repeat
  `./bin/bootwright apply --context "$CONTEXT" --yes`. The
  status shows the convergence unfinished, never realized, and the apply
  converges it; continue to the verification steps below.

```sh
./bin/bootwright apply --context "$CONTEXT" --yes
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright machine list --context "$CONTEXT" --power-status
```

## 7. Verify installation and final network

B74, the install shapes. The first command lists one active profile per
declared interface and the second shows `active-backup` over `nic0` and `nic1`
on the two bonded Machines. The VLAN commands show `nic0.120` on `nv-vlan`,
`bond0.130` over `bond0` on `nv-vlan-bond`, and every interface name is pinned
to its MAC address:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- nmcli -t -f NAME,DEVICE,TYPE,STATE connection show --active
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan-bond -- cat /proc/net/bonding/bond0
./bin/bootwright machine exec --context "$CONTEXT" --name nv-vlan,nv-vlan-bond -- ip -d link show type vlan
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- ip -br link show
virsh --connect qemu:///system domiflist bootwright-lab-network-variants-nv-bond
```

The `nic0` and `nic1` addresses of `ip -br link` equal the `domiflist` MACs
in order.

B326, the final network. Each Machine shows the second address and MTU 1400 on
its guest-only VLAN, the default and 192.0.2.0/24 routes, the search
`e2e.example.test`, and a marker naming the request:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- ip -br address show
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- ip route show
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- cat /etc/resolv.conf
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond -- ip -o link show bond0.140
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- cat /etc/bootwright/network-marker.json
./bin/bootwright diff --context "$CONTEXT" --observe
```

B326, the reboot. The convergence power-cycled each Machine after the marker
was written, so the marker's `bootId` differs from the running one while the
declared network is up; `./bin/bootwright diff --context "$CONTEXT" --observe` then reads every Machine realized,
and the Machine info command shows the lifecycle evidence `network` and `networkReboot`
true:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- cat /proc/sys/kernel/random/boot_id
./bin/bootwright machine info --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond
```

## 8. Check drift and reboot

Drift (B326). Lower the MTU of one guest-only VLAN. `./bin/bootwright diff --context "$CONTEXT" --observe` reads
`Machine/nv-bond` partial and names the block, not the field, so read the
running MTU from the guest; restoring 1400 reads realized. Observation
repairs nothing:

```sh
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond -- sudo -n ip link set dev bond0.140 mtu 1300
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond -- sudo -n nmstatectl show bond0.140
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond -- sudo -n ip link set dev bond0.140 mtu 1400
./bin/bootwright diff --context "$CONTEXT" --observe
```

Reboot (B326). Restart all three, then repeat `ip -br address show` until SSH
answers; the declared addresses return without any apply. Restart returns at
power-on, before the OS boots:

```sh
./bin/bootwright machine restart --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond --yes
./bin/bootwright machine exec --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond -- ip -br address show
```

## 9. Destroy the three-guest context

The first destroy must refuse with `lifecycle.live` while the guests run.
This is the expected stop checkpoint:

```sh
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
```

Stop the named guests, then require removal to complete:

```sh
./bin/bootwright machine stop --context "$CONTEXT" --name nv-bond,nv-vlan,nv-vlan-bond --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright status --context "$CONTEXT"
systemctl list-units --all 'bootwright-*'
./bin/bootwright context delete --name "$CONTEXT" --purge --yes
```

## 10. Verify a declaration that cuts the controller off

The three-guest context is now removed, freeing the shared bridge and ports.
This final B326 drill uses a separate one-guest context and the same listener
firewall rules. The case
renders only `nv-bond`, on the managed network, with a route that sends the
controller's replies out the guest-only VLAN to a next hop nobody answers,
under the context name `nv-cutoff`:

```sh
e2e/render.sh lab-network-variants --set name=nv-cutoff --set single=true --set cutoff=true --out-dir ~/bootwright-e2e/lab-network-variants-cutoff
CUTOFF_CONTEXT="nv-cutoff"
./bin/bootwright validate -f ~/bootwright-e2e/lab-network-variants-cutoff
./bin/bootwright context init --name "$CUTOFF_CONTEXT" --input-dir ~/bootwright-e2e/lab-network-variants-cutoff
./bin/bootwright secret generate --context "$CUTOFF_CONTEXT"
./bin/bootwright secret check --context "$CUTOFF_CONTEXT"
./bin/bootwright apply --context "$CUTOFF_CONTEXT" --stage controller --yes
./bin/bootwright preflight controller --context "$CUTOFF_CONTEXT"
./bin/bootwright apply --context "$CUTOFF_CONTEXT" --stage infrastructure-services --yes
./bin/bootwright apply --context "$CUTOFF_CONTEXT" --yes
```

The last apply fails after NetworkManager's rollback, with partial evidence
and the remedy "the declared network of Machine/nv-bond cannot change under
this apply: run bootwright destroy, which erases the Machines this context
installed". The next apply must fail with the same refusal. This is an expected negative
test. Then destroy the guest, render the corrected declaration, verify a
successful install and remove it:

```sh
./bin/bootwright apply --context "$CUTOFF_CONTEXT" --yes
./bin/bootwright machine stop --context "$CUTOFF_CONTEXT" --name nv-bond --yes
./bin/bootwright destroy --context "$CUTOFF_CONTEXT" --authorize data-loss
e2e/render.sh lab-network-variants --set name=nv-cutoff --set single=true --out-dir ~/bootwright-e2e/lab-network-variants-cutoff
./bin/bootwright context update --name "$CUTOFF_CONTEXT" --input-dir ~/bootwright-e2e/lab-network-variants-cutoff --yes
./bin/bootwright apply --context "$CUTOFF_CONTEXT" --yes
./bin/bootwright machine stop --context "$CUTOFF_CONTEXT" --name nv-bond --yes
./bin/bootwright destroy --context "$CUTOFF_CONTEXT" --authorize data-loss
./bin/bootwright context delete --name "$CUTOFF_CONTEXT" --purge --yes
```

## 11. Clean up and record the result

Complete [Restore listener firewall](../README.md#restore-listener-firewall),
then remove the external connections created in step 2:

```sh
sudo nmcli connection delete br-nv.120 br-nv.130 br-nv
```

Complete [Remove diagnostic tools](../README.md#remove-diagnostic-tools),
then remove the private directory:

```sh
rm -r -- "$RUN_DIR"
```

The run succeeds only after every step and expected result above, including
cleanup. Record the three install shapes, unsupported-mode refusal, final
network, reboot, interrupted install, interrupted convergence, drift and
cut-off declaration in the [acceptance ledger](../../docs/acceptance.md),
keeping environment details and raw evidence private. Cached media remains.
