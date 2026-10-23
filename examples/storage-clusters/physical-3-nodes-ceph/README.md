# Three physical Ceph hosts

This synthetic input combines managed bare-metal RHEL installation with three
Ceph hosts, one explicitly selected WWN disk per host, replicated pools, CephFS
and RBD. The OSD memory target is 4 GiB per OSD. It is an authoring and planning
example; live BMC, image, hardware and storage acceptance remain unrun.

Start with the [physical installation prerequisites](../../machines/lab-baremetal/README.md).
Copy this directory outside Git before supplying real BMC endpoints, NIC
addresses, root-disk names, WWNs, media and credentials. All shipped identities
are documentation placeholders. Each OSD WWN must resolve to an empty local
whole disk distinct from the installation's root disk and every boot device.
The [cluster](storage/cluster.yaml) uses a separate OSD service for each host
because physical WWNs are unique; a shared selector cannot identify three disks.

The existing native `data_devices.paths`, `db_devices.paths` and
`wal_devices.paths` fields accept canonical WWN by-id paths. This initial
bounded physical path supports at most three selected disks per host. It rejects
partitions, shared WWNs, multipath targets and missing disk serials. The original
Machine proof must contain the system UUID and complete MAC inventory, which
must match the installed host's kernel inventory. A bond's physical slaves use
the kernel's saved original addresses only with complete reciprocal bond
membership proof; unknown or changed hardware still refuses. Installed SSH
host-key verification remains required. The implementation admits three to six
storage hosts; larger physical profiles require separate budget qualification.

Device preparation retains the observed WWN, serial, capacity, rotational
status, kernel path and native Ceph device ID before any OSD effect. Read-only
host observation re-proves those facts. Existing foreign signatures, mounted
filesystems and nonempty DB/WAL disks refuse; this input authorizes no disk
zap or replacement. A host or disk substitution cannot be accepted by retrying.

The Machine files explicitly opt in to verified logical overwrite on removal.
Their root and OSD device names, WWNs, serial numbers and exact byte capacities
are synthetic placeholders. Replace every target from independently verified
inventory before planning a copied input. Omitting `eraseOnRemoval` retains the
installed system and makes realized physical Ceph retirement refuse. Add every selected
DB/WAL disk to its Machine's target list when extending this example.
Choose the erase contract before the original apply: that operation freezes its
policy. Editing the inputs later cannot add targets or authorize automatic
retirement of a context originally applied without this policy.

Validate and review the customized plan before acknowledging its data-loss
authorization. Removal first records pending storage retirement, removes its
consumers and cleans managed-OS resources. A separate RAM worker verifies the
original machine and all targets, writes and reads back every addressable byte,
and sends a receipt that the controller must durably record before shutdown.
The erase worker has a four-hour deadline; the full invocation admits at most
six hours including media preparation, BMC boot, startup and cleanup. Choose
target capacities and qualified hardware that can complete full write and
readback within that bound; no throughput or completion time is assumed.
Storage verifies those receipts against the original Ceph disk bindings before
Machine claims can be released. A lost receipt, changed disk or partial readback
keeps removal incomplete; continue the original operation without substituting
new hardware. This is logical overwrite, with no claim about SSD remapped sectors
or hardware sanitization. Physical image, interruption and full-cluster
qualification remain unrun.

If the original records positively prove that neither OS installation nor the
Ceph cluster started, cleanup can release the unused claims without overwriting
disks. It records retention without installation rather than an erase receipt.
Missing or uncertain history cannot authorize that cleanup.
