# IBM Ceph native cleanup qualification

M2b source review found that a successful native cluster-removal command is
not complete cleanup evidence. [Storage](../../specs/ceph.md#removal-and-concurrency)
owns the required behavior; [B491](../../specs/milestones/m2.md#b491) owns its
implementation and qualification.

The authenticated downstream source described in the
[artifact provenance note](ibm-ceph-native-ssh.md) comes from the public
[IBM source RPM](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/source/ceph-20.2.1-405.el9cp.src.rpm).
Its `command_rm_cluster` takes an FSID file lock, but `_rm_cluster` also
removes shared host target/logrotate/authentication files when it sees the
last cluster. Default configuration/keyring cleanup uses an FSID substring
test, and several directory removals ignore errors. `_zap_osds` selects whole
inventory disks when all their logical volumes match the FSID; mixed-volume
cases only warn.

Capture shared-file producer baselines before the first native invocation,
not merely before bootstrap. The same source's `cephadm_init_logging` ensures
the log directory and logrotate configuration even for `cephadm ls`. Its
supported log destinations do not suppress that initialization. Read-only
daemon inventory therefore needs bounded direct filesystem, systemd and Podman
proof; a stubbed `ls` result cannot establish probe purity. RPM verification
must also disable package verification scripts. Preserve shared native audit
logs; broad log globs do not supply an owned inverse.

Do not authorize the raw provider command solely from daemon absence or its
zero exit status. Qualify a wrapper that accounts for every shared file it
can change, uses original owned disk allocations plus live device/FSID/OSD
identity, and independently proves native files, processes, data and
confidential material removed. Whole-FSID scope does not permit child
pool/client blocks to report removal while their native objects remain.
An eventual grouped inverse needs its own explicit lifecycle contract.

This source review executes no cleanup and qualifies no provider artifact.
