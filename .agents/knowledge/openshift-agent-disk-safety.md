# OpenShift Agent Disk-Safety Boundary

The official `openshift-install agent` input can bind a host by declared MAC
addresses and root-device hints, but it provides no supported Bootwright hook
inside the installer immediately before disk erasure. A controller can prove a
Redfish ComputerSystem, its live MAC inventory, power state, and the authored
root selector immediately before boot, but cannot prove inside the agent image
that the same physical target and whole disk are about to be written.

The native agent's `rootDeviceHints.serialNumber` is an exact-match predicate;
`minSizeGigabytes` is only a lower bound, and the agent exposes no exact-byte
capacity predicate ([OpenShift 4.21 agent-installer documentation](https://docs.redhat.com/en/documentation/openshift_container_platform/4.21/pdf/installing_an_on-premise_cluster_with_the_agent-based_installer/OpenShift_Container_Platform-4.21-Installing_an_on-premise_cluster_with_the_Agent-based_Installer-en-US.pdf)).
The [serial selector](../../specs/openshift-clusters.md#selection) therefore proves
exact capacity through the complete controller Storage inventory before boot,
while the native agent receives the exact serial hint. A second drive with the
same serial refuses even when its capacity differs, because the agent would
otherwise choose by discovery order.

The resulting residual race, and the proofs and qualification every bare-metal
apply needs because of it, are specified in
[physical disk safety](../../specs/state-reconciliation.md#physical-disk-safety).

## TPM state before a re-install

A physical TPM clear is the operator's step before a re-install, because
leftover TPM keys from a previous operating system can leave an encrypted
deployment stuck (claim R6:C17 of the M3 research notes, which this repository
does not track, confirmed by their review). The
controller's pre-boot TPM read is positive-only: it can show that keys exist,
never that none do. [B720](../../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node) owns the
encryption item.

Redfish fields of the TPM read: the system's `TrustedModules[]` entries carry
`InterfaceType` (`TPM2_0` for a TPM 2.0) and `Status.State` (`Enabled`). The
earlier implementation observed these names on its hardware (observed by the v1
implementation (main-v1); re-verify on the first physical run). No M3 research
claim covers them.
