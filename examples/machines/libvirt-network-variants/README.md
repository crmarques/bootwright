# Bond and VLAN installs on libvirt

This rehearses [B74](../../../specs/milestones/m1.md#b74) and
[B326](../../../specs/milestones/m1.md#b326) on three libvirt guests, one per
install-interface shape. Each guest installs RHEL over its declared install
interface, then converges to the rest of its declared network: a second address
on a guest-only VLAN, an extra route and an MTU of 1400. The addresses and names
are documentation examples.

| Machine | Install interface | Attachment | Install address | Gateway | Guest-only VLAN 140 |
| --- | --- | --- | --- | --- | --- |
| `nv-bond` | `bond0` over `nic0` and `nic1` | `nv-lab`, managed, bridge `virbr-nv` | 198.51.100.11/26 | 198.51.100.1 | `bond0.140`, 203.0.113.11/24, MTU 1400 |
| `nv-vlan` | `nic0.120` over `nic0` | `nv-trunk`, external, bridge `br-nv` | 198.51.100.71/26 | 198.51.100.65 | `nic0.140`, 203.0.113.12/24, MTU 1400 |
| `nv-vlan-bond` | `bond0.130` over `bond0` over `nic0` and `nic1` | `nv-trunk` | 198.51.100.141/26 | 198.51.100.129 | `bond0.140`, 203.0.113.13/24, MTU 1400 |

Each Machine also routes 192.0.2.0/24 through its gateway on its install
interface and searches `lab.example.test`. Bond mode is `active-backup`, the
only mode a libvirt Machine admits. The MTU is a Machine override beside its
addresses; the routes stay in each NetworkConfig. The controller and its four
services keep `192.0.2.1`, as in every lab example.

Every Machine converges to its declared network and power-cycles once
([network convergence](../../../specs/managed-os.md#network-convergence)). This
needs a build that includes X55. A context made with an earlier build is
finished or destroyed with that build first, as the
[upgrade guidance](../../../specs/milestones.md#status) requires.

## Host prerequisites

lab-rhel's [prerequisites](../lab-rhel/README.md#host-prerequisites) and
[conventions](../lab-rhel/README.md#lab-conventions) apply. Reserve 12 vCPUs, 24
GiB of guest memory and three 60 GiB disks in addition to the controller,
service containers and media. The emulated BMCs use loopback ports `8000`,
`8001` and `8002`. Run one lab per host: this one shares service sockets with
lab-rhel, lab-rhel-3-nodes and lab-sno. The managed network 198.51.100.0/26 on
`virbr-nv` must not exist yet: the `substrates` stage creates it and destroy
removes it.

The VLAN Machines attach to a bridge you make. Bootwright refuses a VLAN install
interface on a managed network because "a managed libvirt network gives its host
an untagged address on <bridge>, so the controller cannot reach the VLAN install
address of <machine>". Bootwright never creates or removes an external bridge.
Make `br-nv` a Linux bridge with no address and no physical port, a VLAN 120 leg
`br-nv.120` holding 198.51.100.65/26 and a VLAN 130 leg `br-nv.130` holding
198.51.100.129/26. Keep all three persistent across a host restart, and put the
legs in a firewall zone that lets both VLAN networks reach DNS, NTP and the
package tree on `192.0.2.1` ([network
exposure](../../../docs/operator-guide.md#network-exposure)).

These NetworkManager commands are unverified in this tree: confirm each on the
host before relying on it.

```sh
sudo nmcli connection add type bridge ifname br-nv con-name br-nv bridge.stp no ipv4.method disabled ipv6.method disabled
sudo nmcli connection add type vlan ifname br-nv.120 con-name br-nv.120 vlan.parent br-nv vlan.id 120 ipv4.method manual ipv4.addresses 198.51.100.65/26 ipv6.method disabled
sudo nmcli connection add type vlan ifname br-nv.130 con-name br-nv.130 vlan.parent br-nv vlan.id 130 ipv4.method manual ipv4.addresses 198.51.100.129/26 ipv6.method disabled
ip -br address show
```

## Run it

Copy the example outside the checkout before editing:

```sh
variants_input=$(mktemp -d /tmp/bootwright-network-variants.XXXXXX)
cp -R examples/machines/libvirt-network-variants/. "$variants_input/"
```

After adjusting that copy, run each command separately from the repository root:

```sh
make build
./bin/bootwright version
./bin/bootwright setup
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
./bin/bootwright context init --name libvirt-network-variants
./bin/bootwright context update --name libvirt-network-variants --input-dir "$variants_input" --yes
./bin/bootwright secret generate
./bin/bootwright secret check
./bin/bootwright plan
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context libvirt-network-variants
./bin/bootwright apply --stage infrastructure-services
./bin/bootwright apply
./bin/bootwright status
./bin/bootwright machine list --power-status
```

The plan shows the group `converge-network` and, for each Machine, the impacts
`configure-network <machine>` and `restart <machine>`.

## Rehearsal rows

Record each row's outcome as you go.

1. Bond install (`nv-bond`). It installs, converges, and its convergence power
   cycle proves the network after a reboot; `status` shows it. The first command
   lists one active profile per declared interface, the second shows mode
   active-backup over `nic0` and `nic1`:

   ```sh
   ./bin/bootwright machine exec --name nv-bond,nv-vlan,nv-vlan-bond -- nmcli -t -f NAME,DEVICE,TYPE,STATE connection show --active
   ./bin/bootwright machine exec --name nv-bond,nv-vlan-bond -- cat /proc/net/bonding/bond0
   ```

2. VLAN install over the external bridge with the host VLAN leg (`nv-vlan`):

   ```sh
   ./bin/bootwright machine exec --name nv-vlan,nv-vlan-bond -- ip -d link show type vlan
   ```

3. VLAN-over-bond install (`nv-vlan-bond`). The two commands of row 1 and the
   command of row 2 show `bond0.130` over `bond0`.

4. Final network. Each Machine shows the second address and MTU 1400 on its
   guest-only VLAN, the default and 192.0.2.0/24 routes, the search
   `lab.example.test` and a marker naming the request. Then every Machine reads
   realized:

   ```sh
   ./bin/bootwright machine exec --name nv-bond,nv-vlan,nv-vlan-bond -- ip -br address show
   ./bin/bootwright machine exec --name nv-bond,nv-vlan,nv-vlan-bond -- ip route show
   ./bin/bootwright machine exec --name nv-bond,nv-vlan,nv-vlan-bond -- cat /etc/resolv.conf
   ./bin/bootwright machine exec --name nv-bond,nv-vlan,nv-vlan-bond -- cat /etc/bootwright/network-marker.json
   ./bin/bootwright diff --observe
   ```

5. Interruption. Press Ctrl-C once while progress shows `converge-network` for a
   Machine, then run `status` and `apply`: the apply converges. Press Ctrl-C
   during an installer wait, then `apply` replays
   ([interrupting an apply](../lab-rhel/README.md#interrupting-an-apply)). This
   needs a fresh context: run it during the first apply, or after row 9's
   cleanup and a new apply.

   ```sh
   ./bin/bootwright status
   ./bin/bootwright apply
   ```

6. A declaration that cuts the controller off has its own section below, because
   destroy is context-wide.

7. Drift. Lower the MTU of one guest-only VLAN. `diff --observe` reads
   `Machine/nv-bond` partial; it names the block, not the field that drifted,
   so read the running MTU of `bond0.140` from the guest. Restore 1400 and it
   reads realized. Observation repairs nothing
   ([observation and drift](../../../specs/managed-os.md#network-convergence)).

   ```sh
   ./bin/bootwright machine exec --name nv-bond -- sudo -n ip link set dev bond0.140 mtu 1300
   ./bin/bootwright diff --observe
   ./bin/bootwright machine exec --name nv-bond -- sudo -n nmstatectl show bond0.140
   ./bin/bootwright machine exec --name nv-bond -- sudo -n ip link set dev bond0.140 mtu 1400
   ./bin/bootwright diff --observe
   ```

8. Optional bond failover. Take the first port's link down on the host, then
   look at the bond from the guest. The domain is
   `bootwright-<context>-<machine>`, and `<MAC of nic0>` is the address
   `domiflist` lists for the first interface. The syntax is the owner's to
   confirm. Record whether `/proc/net/bonding/bond0` shows `nic1` active and
   whether the Machine still answers `machine exec`:

   ```sh
   virsh --connect qemu:///system domiflist bootwright-libvirt-network-variants-nv-bond
   virsh --connect qemu:///system domif-setlink bootwright-libvirt-network-variants-nv-bond <MAC of nic0> down
   ./bin/bootwright machine exec --name nv-bond -- cat /proc/net/bonding/bond0
   ```

9. Facts for the knowledge page. Record Anaconda's profiles beside NMState's,
   the minutes the convergence and its power cycle add per Machine (from the
   operation's progress), and that the disk boot booted the installed disk:

   ```sh
   ./bin/bootwright machine exec --name nv-bond -- rpm -q nmstate NetworkManager
   ./bin/bootwright machine exec --name nv-bond,nv-vlan,nv-vlan-bond -- ls -la /etc/systemd/network
   ./bin/bootwright machine exec --name nv-bond -- sudo -n ls /etc/NetworkManager/system-connections
   ```

## Cleanup

Stop the guests, destroy the context's managed resources and check their removal
before deleting the context:

```sh
./bin/bootwright machine stop --name nv-bond,nv-vlan,nv-vlan-bond
./bin/bootwright destroy --authorize data-loss
./bin/bootwright status
systemctl list-units --all 'bootwright-*'
./bin/bootwright context delete --name libvirt-network-variants --purge
```

Then remove the external bridge you made, which Bootwright never removes:

```sh
sudo nmcli connection delete br-nv.120 br-nv.130 br-nv
```

The media store is host-wide. Keep the images for another example or delete them
using lab-rhel's undo commands after every consuming context is destroyed.

## A declaration that cuts the controller off

This is row 6. Run it after the cleanup above: the two contexts share the
managed bridge and the service sockets. It uses a context of one Machine,
because destroy is context-wide. Copy the input again, keep only `nv-bond` and
add a route that sends the controller's replies the wrong way:

```sh
cutoff_input=$(mktemp -d /tmp/bootwright-network-cutoff.XXXXXX)
cp -R examples/machines/libvirt-network-variants/. "$cutoff_input/"
rm "$cutoff_input/infra/machines/nv-vlan.yaml" "$cutoff_input/infra/machines/nv-vlan-bond.yaml"
rm "$cutoff_input/infra/networkconfigs/nv-vlan.yaml" "$cutoff_input/infra/networkconfigs/nv-vlan-bond.yaml"
```

The copy needs no external bridge, and the host apply proves every declared
external bridge is present, so it must not name `br-nv` after the cleanup above
removed it. In the copy's `infra/providers/lab-libvirt.yaml`, delete the
`nv-trunk` entry at the end of `networkAttachments`, so the list ends with the
managed attachment:

```yaml
  networkAttachments:
    - name: nv-lab

      libvirt:
        bridge: virbr-nv
        management: managed
        address: 198.51.100.1/26
        forward: nat
```

Append this entry to the `routes.config` list at the end of the copy's
`infra/networkconfigs/nv-bond.yaml`:

```yaml
        - destination: 198.51.100.1/32
          next-hop-address: 203.0.113.254
          next-hop-interface: bond0.140
```

The controller reaches the guest from its bridge address 198.51.100.1, so the
guest's replies leave through the guest-only VLAN to a next hop nobody answers.
Then run each command separately:

```sh
./bin/bootwright context init --name nv-cutoff
./bin/bootwright context update --name nv-cutoff --input-dir "$cutoff_input" --yes
./bin/bootwright secret generate
./bin/bootwright secret check
./bin/bootwright apply --stage controller
./bin/bootwright apply --stage infrastructure-services
./bin/bootwright apply
```

The last apply fails after NetworkManager's rollback, with partial evidence and
the remedy "the declared network of Machine/nv-bond cannot change under this
apply: run bootwright destroy, which erases the Machines this context installed"
([cut-off declarations](../../../specs/managed-os.md#network-convergence)). Record
the evidence, then run `apply` again: it fails the same way. Recovery is
destroy only:

```sh
./bin/bootwright apply
./bin/bootwright machine stop --name nv-bond
./bin/bootwright destroy --authorize data-loss
```

Remove the added route from the copy, then show that the corrected declaration
converges and clean up:

```sh
./bin/bootwright context update --name nv-cutoff --input-dir "$cutoff_input" --yes
./bin/bootwright apply
./bin/bootwright machine stop --name nv-bond
./bin/bootwright destroy --authorize data-loss
./bin/bootwright context delete --name nv-cutoff --purge
```

## Record

Each row's outcome goes to the [acceptance ledger](../../../docs/acceptance.md) as
the emulated rehearsal of [B74](../../../specs/milestones/m1.md#b74) and
[B326](../../../specs/milestones/m1.md#b326) under X55.
