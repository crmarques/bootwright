# lab-baremetal

One physical server installed with RHEL through its own Redfish management
controller. It is the smallest complete shape M5a supports: a controller
Machine running the managed services, a bare-metal `InfraProvider`, and one
Machine that declares the hardware it must be proved to be.

What differs from [lab-rhel](../lab-rhel/README.md), which installs the same
operating system on a libvirt guest, follows from the machine existing before
Bootwright and outliving this context:

- **`substrates` is empty.** A bare-metal provider runs nothing Bootwright
  installs, so it plans no provider-host block. The `machines` stage carries
  the whole realization: one block claims and proves the server, and one
  installs it.
- **The apply is authorized.** `apply --authorize data-loss` is required,
  because the installation erases a disk that already held something. The
  removal needs no authorization: it releases the claim and retains the
  machine, its disks and whatever is installed on them.
- **The machine declares what it is.** Its NICs and their addresses, its boot
  NIC, its management controller and its root device are all declared, and the
  controller's own inventory is compared against them before any media is
  inserted. A machine that answers but reports a different address set refuses.
- **The host key is declared, not discovered.** `os.install.hostKeyRef` names
  the key pair the installation delivers, so completion is proved against a key
  that was known before the machine was ever contacted.

## Rehearsing it without hardware

The physical path can be driven end to end on one workstation, because the
emulated controller `lab-rhel` uses implements the same Redfish surface this
one drives, including the `EthernetInterfaces` collection the target proof
reads.

Apply a libvirt context whose Machine is installer-provisioned — `os.provided:
false` with no `installProfileRef`, so the substrate realizes the domain and
its controller and installs nothing. Then point a copy of this example at that
controller: the `bmc.address` is the emulated endpoint, and the declared NIC
addresses are the ones the domain was realized with. Nothing else changes, and
the run exercises the claim, the proof, the private publication and the
delivered-key completion exactly as a real server would.

Keep that copy out of the repository. Its addresses belong to one workstation,
and `examples/wip/` is ignored for exactly this.

## What it does not cover

Removal retains the installed system: physical erase is deliberately not part
of this contract. A bonded or VLAN installation interface is not yet derived.
`import-certificate` virtual-media trust refuses until it is qualified against
real firmware.
