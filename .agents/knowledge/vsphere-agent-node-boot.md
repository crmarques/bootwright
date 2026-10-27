# vSphere agent node boot

How an agent ISO boots on a VMware virtual machine and what the vSphere platform
asks of the installation. [Substrates](../../specs/substrates.md) and
[container clusters](../../specs/openshift-clusters.md) own the required
behavior; this page records native facts for
[B99](../../specs/milestones/m1.md#b99) (the vSphere Machine arm),
[B733](../../specs/milestones/m3.md#b733) (OpenShift on vSphere) and
[B734](../../specs/milestones/m3.md#b734) (the lab-vsphere acceptance).

Provenance: `R7:C<n>` and "R7 claim" with a number are claim ids of the M3
research, `RV` marks a claim the review re-verified, and "observed by the v1 implementation
(main-v1)" marks a fact to re-verify on this tree. Release labels name where a
fact was read; they are not supported or required versions (D163). The ids name
the M3 definition's research and review notes of 2026-10-20, which this
repository does not track; each statement here carries its fact, and an id only
records where it was read.
Assisted-service, assisted-installer and assisted-installer-agent behavior was
read at master, not per release.

## Host registration and boot

- `disk.EnableUUID=TRUE` is checked for every host whose vendor is VMware,
  whatever the cluster platform, and a host without it does not become `known`
  (R7:C17, broadened by RV; the open AGENT-1101 concerns documentation before
  the ISO only). Set it before the first power-on. The message is
  `VSphere disk.EnableUUID isn't enabled for this virtual machine`.
- The assisted installer writes its EFI boot entry only when `/sys/firmware/efi`
  exists; otherwise it logs `setting the boot order on BIOS systems is not
  supported. Skipping...` and continues (R7:C20, RV confirmed). On BIOS firmware
  the VM's own boot order, whose default puts the CD-ROM before the disk,
  decides whether the node boots the agent ISO again (R7 claim 23, inference).
- A manually assigned MAC address lies in `00:50:56:00:00:00` to
  `00:50:56:3f:ff:ff`, and a conflict outside the range shows at power-on
  (R7 claim 25).

## Credentials and platform inputs

- Credentials are optional on the agent path (R7:C4). All four credential fields
  are given or none, and `topology.folder` is required once any is given
  (R7:C5, R7 claim 6). The installer's message is `must specify a folder for
  agent-based installs`.
- Given credentials are embedded, password included, in the image's
  install-config overrides (R7:C9). Placeholders (`vcenterplaceholder`) are used
  otherwise (R7 claim 11). With real credentials the controller leaves the
  `node.cloudprovider.kubernetes.io/uninitialized` taint to the cloud
  controller, so wrong credentials stall the installation (R7:C12).
- Only the virtual addresses and, with credentials, `vcenters` and
  `failureDomains` are forwarded. `loadBalancer`, `hosts` and other fields are
  ignored, so an external load balancer means `platform: none`
  ([VIPs and platforms](openshift-agent-vips-and-platforms.md); R7 claim 9).
- The virtual addresses are required and lie inside the machine network on the
  agent `vsphere` platform (R7 claim 8).
- Machine, MachineSet and control-plane machine set manifests are deleted for
  vSphere (R7:C13).

## Privileges and cleanup

- The documentation's privilege table (R7 claim 27) plus `SetCDMedia`,
  `DeviceConnection`, `AddRemoveDevice`, `AdvancedConfig` and datastore
  `Browse` and `FileManagement` are what a tool that creates VMs needs (R7
  claim 30, medium confidence). The installer repository's `privileges.md` differs from the
  documentation table (R7 claim 28).
- `govc vm.destroy` deletes attached disks. The installer's vSphere destroyer
  does not cover externally created VMs, and CNS volumes outlive VMs (R7
  claims 34 and 35).

## Observed by v1

Observed by the v1 implementation (main-v1); re-verify on this tree (v1 lesson
ids L2-V01 to V07; v1 never ran vSphere live, L2-V13):

- A plain power-on is a no-op on a VM whose CD was swapped while it ran, so
  always force it off and then on.
- Agent ISOs carry no VMware Tools.
- `vsphere_copy` re-uploads on every call and drops a non-443 port.
- A delete with `failed_when: false` hid a 403 and leaked ISOs.
- `VirtualMachine.Config.Annotation` became a correctness privilege when the
  ownership marker was an annotation.
- `Resource.AssignVMToPool` is needed even on the root pool.
- Leaf-name resolution of resource pools and datastores takes the first match.
