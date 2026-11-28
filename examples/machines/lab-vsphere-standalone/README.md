# Two standalone RHEL Machines on vSphere

This environment pairs an Anaconda installation from RHEL 9.8 boot and DVD
media with a full clone of a prepared RHEL 9.8 template. Both guests share the
existing portgroup and use separate generated SSH host keys. The artifact
server on the controller serves the ISO installation's package tree.

Copy the example and replace the documentation addresses, paths and names
with your own. Populate the context store with the vCenter provisioning
account and CA bundle. Import the entitled boot and DVD ISOs under the names
in the profiles. The controller needs verified HTTPS access to vCenter and
SSH connectivity to both Machines. The ISO guest needs access to the package
source. Keep datastore browsing and VM configuration access within the trust
boundary for installation secrets. Dedicate the staging subtree to Bootwright and
restrict write access to its provisioning account and trusted administrators.
vCenter's file API cannot condition upload or deletion on a file version.
A delivered SSH host private key can persist
in reconfiguration history or backups after the live seed is cleared. Restrict
vCenter task/event readers and backup retention accordingly.

Prepare a powered-off, snapshot-free RHEL template with one independent
SCSI 0:0 VMDK whose backing supplies a native content ID. Keep that revision
immutable and readable until cloning completes. Remove stored machine,
subscription and SSH identities before publishing the template. Install
open-vm-tools, enable the cloud-init VMware datasource and clean cloud-init
state so each clone generates a fresh machine ID on first boot. The
example requests automatic root growth on a direct, last-partition XFS or
ext4 root. Install cloud-utils-growpart, NetworkManager and nmstate in the
template. For a pre-sized LVM root select `growRootFilesystem: false`. The
verified first boot retires cloud-init and clears private seed caches.
[VMware datasource](https://docs.cloud-init.io/en/latest/reference/datasources/vmware.html).

The template, placement, datastore and network are operator-owned.

After stopping a Machine, removal unregisters it and removes the owned disks
and staged media. The VM home can still hold configuration, EFI NVRAM, vTPM
state and logs. Retrieve the configuration path from the `retained-vm-home`
event with `bootwright logs --context <context> --operation <destroy-operation-id> --block machine-<machine>`.
Use that location to apply your retention and disposal policy. Empty staging directories
can remain. If an upload or move loses its ownership proof, Bootwright stops.
Compare the vCenter task history, VM staging receipt and datastore contents
before clearing an ambiguous file and retrying.

The [provider](infra/providers/vcenter.yaml) selects compute, storage, folder
and per-NIC backing. The [Machine profiles](infra/os) select how RHEL reaches
its first boot. The [qualification item](../../../specs/milestones/m1.md#b99)
tracks both operator runs.

## vCenter permissions

Scope the provisioning account to the declared inventory. It needs read access
to resolve VM names and inventory, VM creation and removal in the destination
folder, resource-pool assignment, network assignment, datastore allocation and
file operations in the staging area. It must also read and reconfigure VM
devices, advanced settings and power state. Scope template deployment to the
chosen source. Encrypted sources and vTPM require the corresponding crypto
privileges, and vSphere 9 adds crypto access for encrypted clones. Verify the
role with both examples and retain the actual privilege checks with the operator
run. [CreateVM requirements](https://developer.broadcom.com/xapis/vsphere-web-services-api/latest/vim.Folder.html)
and [CloneVM requirements](https://developer.broadcom.com/xapis/vsphere-web-services-api/latest/vim.VirtualMachine.html).
