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

## Inputs

| `--set` | Default | Effect |
| --- | --- | --- |
| `bond.mode=802.3ad` | `active-backup` | A mode the libvirt Machine refuses: the refusal drill |
| `name=nv-cutoff` with `single=true` | `lab-network-variants`, `false` | The single-Machine context of the cut-off drill: only `nv-bond`, on the managed network |
| `cutoff=true` | `false` | With `single`: a route that sends the controller's replies the wrong way |

[values.yaml](values.yaml) holds the rest: addresses, upstreams, media names,
guest size, the VLAN ids and networks and the converged network.

## Prepare

The [host preparation](../README.md#prepare-the-host) and the RHEL 9.8 boot
and DVD ISOs added as media. Free the loopback ports 8000 to 8002. The
managed network 198.51.100.0/26 on `virbr-e2e` must not exist yet: the
`substrates` stage creates it and destroy removes it, and Bootwright places it
in firewalld's `trusted` zone
([provider host realization](../../specs/substrates.md#provider-host-realization)),
so `nv-bond` needs no rule.

Capacity, which the apply gate counts
([capacity admission](../../specs/substrates.md#capacity-admission)): the
three guests plus the provider's `capacityReserve` (1 CPU, 2 GiB, 10 GiB)
need 7 host CPUs not reserved by other libvirt domains, 14 GiB of
`MemAvailable` in `/proc/meminfo` (swap never counts) and 130 GiB free on the
filesystem backing `/var/lib/libvirt/images`, beside the media. Check with
`nproc`, `grep MemAvailable /proc/meminfo` and
`df -h /var/lib/libvirt/images`.

The VLAN Machines attach to a bridge you make. Bootwright refuses a VLAN
install interface on a managed network, because a managed libvirt network gives
its host an untagged address on the bridge and the controller could not reach
the VLAN install address, and it never creates or removes an external bridge.
Make `br-nv` a Linux bridge with no address and no physical port, a VLAN 120
leg `br-nv.120` holding 198.51.100.65/26 and a VLAN 130 leg `br-nv.130` holding
198.51.100.129/26, and keep them persistent. These NetworkManager commands
are unverified in this tree; confirm each on the host:

```sh
sudo nmcli connection add type bridge ifname br-nv con-name br-nv bridge.stp no ipv4.method disabled ipv6.method disabled
sudo nmcli connection add type vlan ifname br-nv.120 con-name br-nv.120 vlan.parent br-nv vlan.id 120 ipv4.method manual ipv4.addresses 198.51.100.65/26 ipv6.method disabled
sudo nmcli connection add type vlan ifname br-nv.130 con-name br-nv.130 vlan.parent br-nv vlan.id 130 ipv4.method manual ipv4.addresses 198.51.100.129/26 ipv6.method disabled
ip -br address show
```

This bridge is yours, so its firewall is too
([network exposure](../../docs/operator-guide.md#network-exposure)). The two
VLAN networks must reach 53/udp+tcp, 123/udp, 3128/tcp, 8080/tcp and 8443/tcp
on the host address `A`. Read the zone the legs landed in and its existing
grants, then add source-scoped runtime rules in that zone (a reboot or a
`firewall-cmd --reload` drops them; add them again then):

```sh
Z=$(sudo firewall-cmd --get-zone-of-interface=br-nv.120); echo "$Z"
sudo firewall-cmd --get-zone-of-interface=br-nv.130
sudo firewall-cmd --zone="$Z" --list-all
for src in 198.51.100.64/26 198.51.100.128/26; do
  for pp in 53/udp 53/tcp 123/udp 3128/tcp 8080/tcp 8443/tcp; do
    sudo firewall-cmd --zone="$Z" --add-rich-rule="rule family=\"ipv4\" source address=\"$src\" port port=\"${pp%/*}\" protocol=\"${pp#*/}\" accept"
  done
done
sudo firewall-cmd --zone="$Z" --list-rich-rules
```

Both legs report the same zone. When `--get-zone-of-interface` reports
`no zone`, the legs fall in the default zone: set
`Z=$(sudo firewall-cmd --get-default-zone)` before the loop. A source-scoped
rule does not narrow a broader grant the zone already holds.

## Run

```sh
e2e/render.sh lab-network-variants
C=lab-network-variants; IN=~/bootwright-e2e/lab-network-variants
A=$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')
./bin/bootwright validate -f "$IN"
./bin/bootwright context init --name "$C" --input-dir "$IN"
./bin/bootwright secret generate --context "$C"
./bin/bootwright secret check --context "$C"
./bin/bootwright plan --context "$C"
./bin/bootwright apply --context "$C" --stage controller --yes
./bin/bootwright preflight controller --context "$C"
./bin/bootwright apply --context "$C" --stage infrastructure-services --yes
```

The plan shows the group `converge-network` and, for each Machine, the impacts
`configure-network <machine>` and `restart <machine>`. The services answer on
the host address `A`:

```sh
curl -sk "https://$A:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
dig @"$A" +short controller.e2e.example.test
chronyd -Q -t 10 "server $A iburst port 123"
```

Then install and converge all three. Run both interruption drills of
[Acceptance drills](#acceptance-drills) in this first apply, because they need
a context that has not installed yet:

```sh
./bin/bootwright apply --context "$C" --yes
./bin/bootwright status --context "$C"
./bin/bootwright machine list --context "$C" --power-status
```

## Verify

B74, the install shapes. The first command lists one active profile per
declared interface and the second shows `active-backup` over `nic0` and `nic1`
on the two bonded Machines. The VLAN commands show `nic0.120` on `nv-vlan`,
`bond0.130` over `bond0` on `nv-vlan-bond`, and every interface name is pinned
to its MAC address:

```sh
./bin/bootwright machine exec --context "$C" --name nv-bond,nv-vlan,nv-vlan-bond -- nmcli -t -f NAME,DEVICE,TYPE,STATE connection show --active
./bin/bootwright machine exec --context "$C" --name nv-bond,nv-vlan-bond -- cat /proc/net/bonding/bond0
./bin/bootwright machine exec --context "$C" --name nv-vlan,nv-vlan-bond -- ip -d link show type vlan
./bin/bootwright machine exec --context "$C" --name nv-bond,nv-vlan,nv-vlan-bond -- ip -br link show
virsh --connect qemu:///system domiflist bootwright-lab-network-variants-nv-bond
```

The `nic0` and `nic1` addresses of `ip -br link` equal the `domiflist` MACs
in order.

B326, the final network. Each Machine shows the second address and MTU 1400 on
its guest-only VLAN, the default and 192.0.2.0/24 routes, the search
`e2e.example.test`, and a marker naming the request:

```sh
./bin/bootwright machine exec --context "$C" --name nv-bond,nv-vlan,nv-vlan-bond -- ip -br address show
./bin/bootwright machine exec --context "$C" --name nv-bond,nv-vlan,nv-vlan-bond -- ip route show
./bin/bootwright machine exec --context "$C" --name nv-bond,nv-vlan,nv-vlan-bond -- cat /etc/resolv.conf
./bin/bootwright machine exec --context "$C" --name nv-bond -- ip -o link show bond0.140
./bin/bootwright machine exec --context "$C" --name nv-bond,nv-vlan,nv-vlan-bond -- cat /etc/bootwright/network-marker.json
./bin/bootwright diff --context "$C" --observe
```

B326, the reboot. The convergence power-cycled each Machine after the marker
was written, so the marker's `bootId` differs from the running one while the
declared network is up; `diff --observe` then reads every Machine realized,
and `machine info` shows the lifecycle evidence `network` and `networkReboot`
true:

```sh
./bin/bootwright machine exec --context "$C" --name nv-bond,nv-vlan,nv-vlan-bond -- cat /proc/sys/kernel/random/boot_id
./bin/bootwright machine info --context "$C" --name nv-bond,nv-vlan,nv-vlan-bond
```

## Acceptance drills

Interruptions, during the first apply of Run (B74 and B326):

- Interrupted install (B74): press Ctrl-C once during an installer wait, then
  repeat `apply --context "$C" --yes`. The install replays and completes.
- Interrupted convergence (B326): press Ctrl-C once while progress shows
  `converge-network` for a Machine, then run `status` and repeat `apply`. The
  status shows the convergence unfinished, never realized, and the apply
  converges it; repeat the final-network and reboot checks of Verify.

Refusal (B74), before the first `apply`, between `plan` and the controller
stage of Run. A libvirt Machine
admits only `active-backup`; render the case with `bond.mode=802.3ad` into a
second directory, which validates, and import it:

```sh
e2e/render.sh lab-network-variants --set bond.mode=802.3ad --out-dir ~/bootwright-e2e/lab-network-variants-mode
./bin/bootwright context update --name "$C" --input-dir ~/bootwright-e2e/lab-network-variants-mode --yes
./bin/bootwright plan --context "$C"
./bin/bootwright context update --name "$C" --input-dir "$IN" --yes
```

The plan, or at the latest the first apply, refuses before any effect with "a
libvirt bridge qualifies only active-backup bonding, and bond bond0 of nv-bond
declares mode 802.3ad". The other modes are qualified on physical hosts
([B78](../../specs/milestones/m1.md#b78)).

Drift (B326). Lower the MTU of one guest-only VLAN. `diff --observe` reads
`Machine/nv-bond` partial and names the block, not the field, so read the
running MTU from the guest; restoring 1400 reads realized. Observation
repairs nothing:

```sh
./bin/bootwright machine exec --context "$C" --name nv-bond -- sudo -n ip link set dev bond0.140 mtu 1300
./bin/bootwright diff --context "$C" --observe
./bin/bootwright machine exec --context "$C" --name nv-bond -- sudo -n nmstatectl show bond0.140
./bin/bootwright machine exec --context "$C" --name nv-bond -- sudo -n ip link set dev bond0.140 mtu 1400
./bin/bootwright diff --context "$C" --observe
```

Reboot (B326). Restart all three, then repeat `ip -br address show` until SSH
answers; the declared addresses return without any apply. Restart returns at
power-on, before the OS boots:

```sh
./bin/bootwright machine restart --context "$C" --name nv-bond,nv-vlan,nv-vlan-bond --yes
./bin/bootwright machine exec --context "$C" --name nv-bond,nv-vlan,nv-vlan-bond -- ip -br address show
```

Optional bond failover, not part of the acceptance: take the first port's link
down on the host and record whether `/proc/net/bonding/bond0` shows `nic1`
active and the Machine still answers `machine exec`. `<MAC of nic0>` is the
first interface `domiflist` lists:

```sh
virsh --connect qemu:///system domif-setlink bootwright-lab-network-variants-nv-bond <MAC of nic0> down
./bin/bootwright machine exec --context "$C" --name nv-bond -- cat /proc/net/bonding/bond0
```

Record each result as a row of the [acceptance ledger](../../docs/acceptance.md):
the three install shapes, the final network, the reboot, the interrupted
install, the interrupted convergence, the drift and the cut-off declaration.

### A declaration that cuts the controller off

The last drill of B326, run after the Destroy below because the two contexts
share the managed bridge and the service ports; it needs no external bridge.
It uses a context of one Machine, because destroy is context-wide. The case
renders only `nv-bond`, on the managed network, with a route that sends the
controller's replies out the guest-only VLAN to a next hop nobody answers,
under the context name `nv-cutoff`:

```sh
e2e/render.sh lab-network-variants --set name=nv-cutoff --set single=true --set cutoff=true --out-dir ~/bootwright-e2e/lab-network-variants-cutoff
R=nv-cutoff
./bin/bootwright validate -f ~/bootwright-e2e/lab-network-variants-cutoff
./bin/bootwright context init --name "$R" --input-dir ~/bootwright-e2e/lab-network-variants-cutoff
./bin/bootwright secret generate --context "$R"
./bin/bootwright secret check --context "$R"
./bin/bootwright apply --context "$R" --stage controller --yes
./bin/bootwright apply --context "$R" --stage infrastructure-services --yes
./bin/bootwright apply --context "$R" --yes
```

The last apply fails after NetworkManager's rollback, with partial evidence
and the remedy "the declared network of Machine/nv-bond cannot change under
this apply: run bootwright destroy, which erases the Machines this context
installed". Record the evidence, then repeat `apply`: it fails the same way.
Recovery is destroy only. Render the corrected declaration, show that it
converges, and clean up:

```sh
./bin/bootwright apply --context "$R" --yes
./bin/bootwright machine stop --context "$R" --name nv-bond --yes
./bin/bootwright destroy --context "$R" --authorize data-loss
e2e/render.sh lab-network-variants --set name=nv-cutoff --set single=true --out-dir ~/bootwright-e2e/lab-network-variants-cutoff
./bin/bootwright context update --name "$R" --input-dir ~/bootwright-e2e/lab-network-variants-cutoff --yes
./bin/bootwright apply --context "$R" --yes
./bin/bootwright machine stop --context "$R" --name nv-bond --yes
./bin/bootwright destroy --context "$R" --authorize data-loss
./bin/bootwright context delete --name "$R" --purge --yes
```

## Destroy

```sh
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright machine stop --context "$C" --name nv-bond,nv-vlan,nv-vlan-bond --yes
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright status --context "$C"
systemctl list-units --all 'bootwright-*'
./bin/bootwright context delete --name "$C" --purge --yes
```

Then remove the firewall rules and the external bridge you made, which
Bootwright never removes:

```sh
for src in 198.51.100.64/26 198.51.100.128/26; do
  for pp in 53/udp 53/tcp 123/udp 3128/tcp 8080/tcp 8443/tcp; do
    sudo firewall-cmd --zone="$Z" --remove-rich-rule="rule family=\"ipv4\" source address=\"$src\" port port=\"${pp%/*}\" protocol=\"${pp#*/}\" accept"
  done
done
sudo firewall-cmd --zone="$Z" --list-rich-rules
sudo nmcli connection delete br-nv.120 br-nv.130 br-nv
```

Media stays for the next case. Run the cut-off drill above next, if it is still
owed; it needs none of the bridges removed here.
