# ADR 0050: The Machine an Installer Boot Finds Powered On

## Status

Accepted

Revises the occupancy probe of [ADR 0011](0011-bmc-vmedia-boot-flow.md): the
`PowerState=On` + `BootProgress.LastState=OSRunning` refusal becomes a
powered-off requirement on every managed-OS installer boot. Follows the
physical-half rule of [ADR 0034](0034-wiping-a-device-no-node-claims.md) and
the one-token-per-refusal grammar of
[ADR 0030](0030-one-intent-flag-and-named-authorizations.md): this refusal has
no legitimate reading, so it carries no token.

## Context

Booting install media is where a machine's disks are lost. The OpenShift agent
ISO and the managed-OS Anaconda ISO both funnel through the per-substrate boot
roles, and each of those roles force-cycled whatever it found: the Redfish path
issued `ForceOff` and powered back on, the vSphere path forced `powered-off`
then `powered-on`, the KubeVirt path ran `virtctl stop --force` before
replacing the agent ISO. One guard stood before any of that — the Redfish
occupancy probe of ADR 0011 — and it held only when three things lined up: the
BMC populated `BootProgress` (an optional Redfish property many controllers
never report), the boot was a first install (the probe was skipped for every
cluster named in the reinstall list that `--mode rebuild` arms), and the
machine ran Redfish at all (the vSphere and KubeVirt paths had no guard).

The pre-install SSH ladder does not close the gap. A powered-on machine that
answers SSH is classified — owned, foreign, or drifted — and the foreign and
unverifiable readings already fail closed. A powered-on machine that does
*not* answer SSH on the expected address is indistinguishable from an empty
one: `bootwright_managed_os_already_ready` is false, the install proceeds, and
whatever that machine was running is wiped. A production box on a different
address, a host parked in its BIOS, a machine whose SSH daemon died — all of
them read as absent.

The teardown side sharpens it. Bare-metal destroy is deliberately state-only:
it clears records and leaves the OS running, and the next apply reinstalls the
released machines. Until now that reinstall was the first moment the
still-running OS was lost, and nothing between the operator and the wipe asked
for anything physical.

## Decision

**An installer boot on a managed-OS machine requires the machine to be
observably powered off.** Each boot role probes its substrate immediately
before its first mutation and refuses otherwise:

- Redfish (bare metal, and libvirt through the emulated BMC): the system must
  report `PowerState=Off`. A non-200 probe fails closed like a powered-on
  machine.
- vSphere: vCenter must report `poweredOff` for the VM. An unreadable state
  fails closed.
- KubeVirt: the VirtualMachineInstance must be *provenly absent* — the probe
  passes only on a `not found` answer, in the proven-absence grammar of
  [ADR 0039](0039-the-node-a-teardown-left-serving-the-cluster.md). Any other
  probe failure fails closed.

The gate keys on `osManaged` and on the boot action, nothing else: no
`setBootSource` carve-out, no reinstall-list skip, no first-install narrowing.
A provided-OS machine never reaches these roles — its boot role is `none`, and
booting media by hand is already the operator's physical act. A VM bootwright
creates arrives stopped on every substrate (`runStrategy: Manual`, cloned
powered off, defined halted), so a day-1 estate passes without ceremony.

**Powering the machine off is the authorization, so there is no token.**
ADR 0034 split gates into an ownership half a token may clear and a physical
half no token touches; this gate is all physical half. The remedy is available
to every operator, costs one BMC action, and is itself the confirmation that
nothing on the machine is still needed — a flag that stood in for it would
reduce the gate to the flag. `--authorize all` does not name it because there
is nothing to name.

**The refusal explains the interrupted-install case instead of exempting it.**
A run that booted the installer and died leaves the machine powered on, and the
re-run's gate demands a power-off that today's flow would have performed
itself. Bootwright records no per-machine mid-install evidence for managed OS,
and inventing a record that lets a powered-on machine through would launder
exactly the state this gate exists to question. The message says the power-off
is safe — the install restarts from media and loses nothing — and that is the
whole cost. The OpenShift side already resumes past its boot task once the
install record proves the nodes booted, so the gate does not re-fire on a
resumed cluster install.

**The check stays at the boot, not in preflight.** Only the boot knows the
install decision: the same powered-on machine is correct during converge and
fatal during install, and preflight (ADR 0046) already defers the hosts this
run installs. Apply instead *forecasts* the gate: the pre-prompt warnings for
first bare-metal boots, first managed-OS installs, rebuild reinstalls, and
destroy-released reinstalls each name the machines that must be off before the
run reaches them.

## Consequences

- A destroy → apply cycle on bare metal now has a physical step: the released
  machines keep running until the operator powers them off, and the reinstall
  refuses until then. That is the intended two-key: the release record
  authorizes the data loss, the power button authorizes the machine.
- A re-run over an interrupted Anaconda install demands a power-off before it
  restarts. One BMC action, guided by the refusal, replacing a silent
  force-reset.
- The occupancy probe and its `bootwright_ocp_reinstall_clusters` escape are
  gone: the reinstall list no longer weakens the guard it used to skip, and a
  BMC that never reports `BootProgress` no longer passes vacuously. What the
  probe caught, the powered-off requirement catches strictly earlier.
- vSphere and KubeVirt installer boots gain a guard they never had.
- An emulated-BMC lab pays the same ceremony as production: reinstalling a
  running libvirt machine means stopping it first. That friction is the
  feature, and it is one `virsh destroy` in the lab.
