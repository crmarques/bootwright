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

## Worth noting

- Each Machine also routes 192.0.2.0/24 through its gateway and searches
  `lab.example.test`. Bond mode is `active-backup`, the only mode a libvirt
  Machine admits. The MTU is a Machine override; routes stay in the NetworkConfig.
- Every Machine converges to its declared network and power-cycles once
  ([network convergence](../../../specs/managed-os.md#network-convergence)); this
  needs a build that includes X55.
- Host capacity: 12 vCPUs, 24 GiB of guest memory and three 60 GiB disks beyond
  the controller, services and media; the provider reserves 1 vCPU, 2 GiB and
  10 GiB. The managed `virbr-nv` network must not already exist.
- The VLAN Machines attach to an external bridge `br-nv` you create (no address,
  no physical port, VLAN legs `br-nv.120` and `br-nv.130` holding the gateway
  addresses); Bootwright never creates or removes it. A managed network refuses a
  VLAN install interface.
- Qualification: this is an emulated rehearsal, recorded in the
  [acceptance ledger](../../../docs/acceptance.md); no owner acceptance is claimed.
- Operator inputs: RHEL 9.8 x86_64 boot and DVD ISOs, and the persistent host
  bridge with a firewall zone letting both VLAN networks reach DNS, NTP and the
  package tree on `192.0.2.1`. It shares service sockets with
  [lab-rhel](../lab-rhel/README.md), so run one lab per host.

Run it with the e2e [`lab-network-variants`](../../../e2e/lab-network-variants/runbook.md) case.
