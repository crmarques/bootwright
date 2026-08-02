# vCenter minimum-privilege role for the vSphere adapter

Derived from the API call each `community.vmware` module actually issues, not
from module names. Two modules that both produce `ReconfigVM_Task` share
privileges. Everything below is what the account in
`vsphere.vcenters[].credentialsRef` must hold; there is no second identity and
no ESXi login.

## The call surface it is derived from

Ansible reaches vCenter from six files and nowhere else: `probe.yml`,
`gate.yml`, `apply.yml` and `destroy.yml` under `machine_substrate_vsphere`,
`insert.yml`/`cleanup.yml` under `container_cluster_media_vsphere`, and
`power.yml` under `container_cluster_boot_vsphere`. `vmware.vmware` is pinned in
the bundle but referenced by **no** task. There is no `govc`, no Content
Library call and no OVF deploy. Go-side traffic is only
`internal/preflight/vsphere.go` opening and deleting its own REST session.

Every module resolves inventory **by name** through a container view rooted at
`content.rootFolder`, so read visibility is a functional requirement, not a
nicety: an object the account cannot see reports `Unable to find cluster "…"`,
which reads as an input typo.

## Base role (both install modes)

```
Datastore.AllocateSpace
Datastore.Browse
Datastore.DeleteFile
Datastore.FileManagement
Network.Assign
Resource.AssignVMToPool
VirtualMachine.Config.AddNewDisk
VirtualMachine.Config.AddRemoveDevice
VirtualMachine.Config.Annotation
VirtualMachine.Config.CPUCount
VirtualMachine.Config.EditDevice
VirtualMachine.Config.Memory
VirtualMachine.Config.Settings
VirtualMachine.Interact.DeviceConnection
VirtualMachine.Interact.PowerOff
VirtualMachine.Interact.PowerOn
VirtualMachine.Interact.SetCDMedia
VirtualMachine.Inventory.Create
VirtualMachine.Inventory.Delete
```

Template-clone mode adds `VirtualMachine.Inventory.CreateFromExisting`
(replaces `Inventory.Create` on that path), `Provisioning.DeployTemplate` (source
marked as a Template) or `Provisioning.Clone` (plain powered-off VM),
`Config.AdvancedConfig` (the `extraConfig` guestinfo seed) and
`Config.DiskExtend` (only when `diskGiB` exceeds the template's root disk).
`Resource.ColdMigrate`/`HotMigrate` are needed only if a declared
`topology.resourcePool` changes for a live VM — the module then issues
`RelocateVM_Task`.

## Non-obvious attributions

- **`VirtualMachine.Config.Annotation` is a correctness privilege.** The
  annotation is written by a **separate** `ReconfigVM_Task` after create/clone
  succeeds. Lose it and the VM exists with no ownership marker: `probe.yml`
  refuses every later apply ("carries no Bootwright ownership marker") and
  `destroy.yml` refuses without `--authorize unowned-vms`. A partially
  privileged role strands VMs.
- **`VirtualMachine.Interact.SetCDMedia` and `Interact.DeviceConnection` are
  required, not optional.** The media role runs against a **powered-on** VM —
  `cleanup.yml` is invoked from the Anaconda role's `wait.yml` after the
  installed OS answers SSH.
- **`VirtualMachine.Interact.PowerOff` gates destroy.** `state: absent,
  force: true` power-cycles internally before `Destroy_Task`, so a role that can
  delete but not power off cannot destroy a running VM.
- **`Resource.AssignVMToPool` is required in every configuration.** With an
  empty `topology.resourcePool` the target is the compute cluster's **root**
  pool; `get_resource_pool()` fails hard rather than skipping.
- **Datastore file traffic is the datastore HTTP file service** (`/folder`
  endpoint proxied by vCenter), not `FileManager` SOAP. `Browse` covers the
  `HEAD`, `FileManagement` covers `PUT` and `DELETE`. `DeleteFile` is added
  because some builds gate browser deletes on it as well, and the three delete
  call sites all carry `failed_when: false` — a 403 there is invisible and leaks
  ISOs forever. The `PUT` auto-creates the staging directory, so no
  `Folder.Create` is needed.
- **Read-only on the datacenter must propagate**, or name resolution of the
  cluster, pool, datastore, portgroup and template fails.

## Propagation traps

1. The **VM folder** grant must propagate. `Inventory.Create` /
   `CreateFromExisting` is checked on the folder; `Config.*`, `Interact.*` and
   `Inventory.Delete` are checked on the **VM**, its child. Propagate off ⇒ the
   VM is created and nothing else works, annotation first.
2. **Datastores and networks are not children of the VM folder.** They hang off
   the datacenter's `datastore`/`network` branches; a VM-folder permission
   grants nothing on them.
3. **Resource pools hang off the `host` branch**, not the `vm` branch, so
   `AssignVMToPool` is never inherited from a VM-folder grant.
4. **`resourcePool`, `computeCluster` and `datastore` are matched by LEAF
   name.** `vsphereInventoryName()` strips everything before the last `/`, so
   `/dc1/host/cluster1/Resources/bootwright` is handed to Ansible as
   `bootwright` and `find_obj` takes the first match. Two pools with the same
   leaf name in one cluster resolve non-deterministically. `folder` is **not**
   stripped and is passed as a full inventory path.
5. **`ReadCustSpecs` on a datacenter does nothing.** `CustomizationSpecManager`
   is a vCenter singleton; the grant must be at the root object. Bootwright does
   not use stored specs, so it should never need this.

## Explicitly not required — keep the role minimal

`VApp.*` (no OVF/OVA/Content Library anywhere); tagging and custom attributes
(ownership is the VM annotation); `Cryptographer.*` (`profile.tpm` is refused on
vSphere outright); `Provisioning.MarkAsTemplate`/`MarkAsVM`, `PromoteDisks`,
`VirtualMachine.State.*` snapshots, `Inventory.Move`/`Register`/`Unregister`;
`Interact.Reset`/`Suspend`/`ConsoleInteract`/`GuestControl`/`ToolsInstall` (only
forced power-on/off are ever requested); `Network.Config`/`DVSwitch.*`/
`DVPortgroup.*` (portgroups are consumed, never created); `Host.*` and
`Folder.Create`; `Resource.ApplyRecommendation` (SDRS recommendations are read,
never applied); and all of `Sessions.*` — `ValidateSession` and
`TerminateSession` govern *other users'* sessions and are escalation surface.
`Provisioning.Customize` is not required because no task passes `customization:`.

## Version drift and probing

Privilege **IDs** are byte-identical on vSphere 7.0 and 8.0; only the vSphere
Client labels and grouping have moved (`VirtualMachine.Config.*` was
"Configuration" pre-6.7, "Change Configuration" since; `Config.Settings` was
"Settings"). Build the role from IDs and confirm with
`AuthorizationManager.privilegeList` (pyVmomi) or `Get-VIPrivilege -Id …`
(PowerCLI). `VirtualMachine.Provisioning.Customize` is the one ID third-party
guides sometimes publish as `…CustomizeGuest` — verify before scripting.

## There is no privilege probe, and preflight is not one

`internal/preflight/vsphere.go` opens `POST /api/session` and deletes it again.
vCenter issues a session to an account holding **zero** inventory permissions, so
a green preflight proves reachability, TLS and credentials — never the role.
Nothing in the repo calls `AuthorizationManager.HasPrivilegeOnEntity`. A probe
against the declared folder/pool/datastore/portgroup would turn a mis-built role
from a mid-apply failure that strands an un-annotated VM into a preflight FAIL;
it is deliberately out of scope of ADR 0045 and would be its own decision.

## Two adjacent defects found while deriving this

- **`vsphere_copy` and `vsphere_file` ignore `port` entirely.** Both build
  `https://<hostname><path>` with no port, even though `port` is in the shared
  argument spec with default 443. A vCenter on a non-443 port cannot stage or
  clean up ISOs, although `VSphereVCenter.Port` is a first-class API field.
- **`cdrom:` is silently dropped when the clone source is a marked Template.**
  `configure_cdrom` returns early when `vm_obj.config.template` is true, which on
  the clone path it always is. The cloned VM therefore inherits the template's
  CD-ROM topology, the media role must add a SATA controller and CD-ROM at
  attach time on a possibly running VM (hence `AddRemoveDevice` +
  `Interact.SetCDMedia`), and a boot order naming `cdrom` is set against a device
  that may not exist.
