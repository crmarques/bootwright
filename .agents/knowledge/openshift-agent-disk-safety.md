# OpenShift Agent Disk-Safety Boundary

The official `openshift-install agent` input can bind a host by declared MAC
addresses and root-device hints, but it provides no supported Bootwright hook
inside the installer immediately before disk erasure. A controller can prove a
Redfish ComputerSystem, its live MAC inventory, power state, and the authored
root selector immediately before boot, but cannot prove inside the agent image
that the same physical target and whole disk are about to be written.

That controller-to-installer interval is a residual race, not an ownership
proof. Never describe MACs, root hints, power-off state, a context claim, or
`data-loss` authorization as closing it. Any bare-metal apply implementation
must retain all controller-side proofs, fail closed on any mismatch or unknown
probe, and pass explicit real-hardware qualification before being called
supported.

The target safety behavior remains governed by
`specs/state-reconciliation.md`.
