# A btrfs device number does not survive a reboot

## Symptom

A store that compares a recorded `directoryDevice` with a later `stat` can
refuse after a reboot with `context.state: controller directory was replaced`
or `context directory was replaced`, even when every directory keeps its
inode. The recorded device and the current device can differ solely because
the filesystem was remounted.

## Cause

btrfs gives each mounted subvolume an anonymous device number (`st_dev`) from
the kernel's anonymous pool at mount time, not one derived from the
filesystem, so the number depends on mount order and changes across boots
while every inode stays. A recorded `st_dev` compared with a later `stat`
therefore identifies nothing across a reboot on btrfs. Within one mount the
number is constant, and every directory of one subvolume shares it, so a child
and the directory holding it still compare equal; a nested subvolume or a
mount shows another number.

## Rule

Identify a stored object across process lifetimes by its recorded inode and
by sitting on the same filesystem as the directory that holds it. Use the
child's held status and the parent's status held when opened; never compare
a recorded device number. A device comparison within one process lifetime (two statuses
of a held descriptor, a listed entry against its opened parent, a stage
against its directory) stays valid. A recorded device may still be written and
quoted, as a context's incarnation quotes it, as long as nothing compares it
with a live one. The owning rule is
[recorded directory identity](../../specs/contexts.md#recorded-directory-identity);
the helper is `recordedDirectory` in
`internal/workspace/contextfs/files_linux_amd64.go`, and
`internal/workspace/contextfs/recorded_directory_linux_amd64_test.go` holds a
store whose recorded devices differ from the current ones to opening. Records
the collection keeps on managed Ceph hosts (`ceph_tunnel_custody`,
`ceph_host_runtime`'s cache directory, `ceph_native`'s registry metadata)
still compare a recorded `st_dev` with a later one; they meet the same failure
on a host whose filesystem renumbers its device at boot.
