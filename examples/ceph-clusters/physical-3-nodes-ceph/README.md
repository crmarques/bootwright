# Three physical Ceph hosts

This synthetic input combines managed bare-metal RHEL installation with three
Ceph hosts (`metal-01` to `metal-03`): one explicitly selected WWN disk per
host, replicated pools, CephFS and RBD, and an IBM Storage Ceph 9.9.2.0
cluster (`ceph-lab`). It is an authoring and planning example; live BMC,
image, hardware and storage acceptance remain unrun.

Start with the
[physical installation prerequisites](../../machines/lab-baremetal/README.md);
copy this directory outside Git before supplying real BMC endpoints, NIC
addresses, disk names, WWNs, media and credentials.

## Worth noting

- Every shipped identity (BMC addresses, MACs, WWNs, serials, byte capacities)
  is a documentation placeholder. Each OSD WWN must resolve to an empty local
  whole disk distinct from the root and every boot device. Replace each target
  from independently verified inventory.
- The cluster uses one OSD service per host (`data-1` to `data-3`, canonical
  `/dev/disk/by-id/wwn-...` paths) because physical WWNs are unique. A host
  may select up to 15 disks, the most its 16-target erase policy covers beside
  the root disk, listed in one OSD service per host; partitions, shared WWNs,
  multipath targets and missing serials refuse, and three to eleven storage
  hosts are admitted. The OSD memory target is 4 GiB.
- A disk whose WWN its BMC does not report, such as an NVMe disk, may be
  selected by its serial number instead: drop `deviceName` and `wwn` from its
  `eraseOnRemoval` target, keeping `serialNumber` and `sizeBytes`, and list it
  in the OSD service as `/dev/disk/by-serial/<serialNumber>`. No such path
  exists on the host; host preparation finds the one fixed, writable whole
  disk reporting that serial number at exactly that size, refuses none or
  several, and gives cephadm the disk's stable `/dev/disk/by-id` link
  ([devices](../../../specs/ceph.md#devices-and-destructive-ownership)). The
  shipped files keep the WWN form.
- Foreign signatures, mounted filesystems and nonempty DB/WAL disks refuse;
  no zap or replacement is authorized, and retrying cannot accept a substitute.
  Instead each Machine declares its OSD disk in `clearOnInstall`: the
  installation that consumes `data-loss` proves the disk's path, WWN, serial
  and size inside the installer and zeroes its
  [metadata regions](../../../specs/managed-os.md#physical-data-disk-clear), so a
  disk a destroyed or abandoned cluster left reads as empty. Add DB/WAL disks
  there too when extending.
- The Machine files opt in to `eraseOnRemoval` over the root and OSD disks:
  `logicalOverwrite` zeroes and reads back the whole root disk, and the OSD
  disk's `method: metadataClear` zeroes and reads back only the regions that
  make it read as empty to native probes. Choose the policy before the first
  apply, because that apply freezes it. Omitting it makes realized physical
  Ceph retirement refuse. Neither method is hardware sanitization, and a
  four-hour erase deadline applies. Add DB/WAL disks to the targets when
  extending.
- Required inputs: BMC credentials (`lab-bmc-credentials`) and CA
  (`lab-bmc-ca`), per-host host keys, `ibm-registry`, RHEL 9.8 media and
  verified BMC media trust. The Entitlement `license.accept: true` is a
  placeholder; accepting IBM's license is your explicit act.
- The [P1-P7 profiles](../../../specs/ceph-coverage.md#profile-catalog) can be
  adapted to this shape in a private copy; physical qualification is unrun.
  P7's RGW multisite stays blocked by
  [B483](../../../specs/milestones/m2.md#b483).
