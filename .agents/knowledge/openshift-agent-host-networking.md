# OpenShift agent host networking

How a node's network configuration travels from the agent configuration to the
live image and the installed host. [Container clusters](../../specs/container-clusters.md)
owns the required behavior; this page records native facts for
[B718](../../specs/milestones/m3.md#b718) (bonds, VLANs and addressless NICs),
[B706](../../specs/milestones/m3.md#b706) (the controller's `nmstatectl`) and
[B719](../../specs/milestones/m3.md#b719) (install-time manifests).

Provenance: `R<n>:C<m>` is a claim id of the M3 research, `RV` marks a claim the
review re-verified, and "observed by the v1 implementation (main-v1)" marks a
fact to re-verify on this tree. Release labels name where a fact was read; they
are not supported or required versions (D163). The ids name the M3 definition's
research and review notes of 2026-10-20, which this repository does not track;
each statement here carries its fact, and an id only records where it was read.
Assisted-service, assisted-installer and assisted-installer-agent behavior was
read at master, not per release.

## What the installer accepts

- `openshift-install` runs the controller host's `nmstatectl gc` once per host
  (R2:C06, high). That the NMState schema the installer accepts is therefore
  the controller host's nmstate is an inference (medium).
- `gc` writes no `ovn` or `ovs-db` state (R2:C07, RV).
- The documentation's `networkConfig` examples show static IPv4 addresses with
  `dns-resolver.config.server` and `routes.config` (`destination`,
  `next-hop-address`, `next-hop-interface`, `table-id: 254`); a `bond`
  (`link-aggregation.mode`, `options.miimon`, `port`) with a `vlan` on top
  (`vlan.base-iface`, `vlan.id`, named like `bond0.300`); and a bond over SR-IOV
  virtual functions. An interface `name` need not match the device name. No
  example shows `ovs-bridge`, `ovs-interface` or `linux-bridge` (R2:C09).
- An `ovs-bridge` entry in `networkConfig` is unproven (R2:C12, RV uncertain).
  The Bootwright design builds nothing on it without a live test.

## On the live image and the installed host

- The live image renames logical NIC names to kernel names and trims VLAN
  interface names to 15 characters (R2:C08).
- Hosts with static networking are installed with `--copy-network` (R2:C10, RV).
- `configure-ovs` builds `br-ex` from the default-route interface on every boot
  (R2:C14).
- A bond member reports the bond's MAC address while the bond runs, so members
  are identified by their permanent MAC address (observed by the v1
  implementation (main-v1); re-verify on this tree).
