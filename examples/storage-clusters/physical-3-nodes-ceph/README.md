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
must match the installed host's kernel inventory. Changed NIC addresses,
including a bond that changes a physical NIC's reported address, fail closed.
Installed SSH host-key verification remains required.

Device preparation retains the observed WWN, serial, capacity, rotational
status, kernel path and native Ceph device ID before any OSD effect. Read-only
host observation re-proves those facts. Existing foreign signatures, mounted
filesystems and nonempty DB/WAL disks refuse; this input authorizes no disk
zap or replacement. A host or disk substitution cannot be accepted by retrying.

Validate the customized copy before planning it. Installation retains its
separate data-loss authorization. Physical Ceph destroy currently refuses:
releasing the Machine claim leaves the running OS and data intact and cannot
satisfy virtual-host retirement. Physical erase and its acceptance evidence
remain a separate contract.
