# OpenShift Agent Disk-Safety Boundary

The official `openshift-install agent` input can bind a host by declared MAC
addresses and root-device hints, but it provides no supported Bootwright hook
inside the installer immediately before disk erasure. A controller can prove a
Redfish ComputerSystem, its live MAC inventory, power state, and the authored
root selector immediately before boot, but cannot prove inside the agent image
that the same physical target and whole disk are about to be written.

The resulting residual race, and the proofs and qualification every bare-metal
apply needs because of it, are specified in
[physical disk safety](../../specs/state-reconciliation.md#physical-disk-safety).
