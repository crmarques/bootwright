# Managed OS Libvirt Stale Disk Boot

**Symptom:** A managed OS apply waits at `Scan managed OS SSH host key`, while
direct `ssh-keyscan` reports `Connection closed by remote host` or
`kex_exchange_identification: read: Connection reset by peer`. VM console
screenshots show an OS version different from the current `MachineImage` input,
or kernel messages such as `I/O error, dev vda`, `XFS (vda3): log I/O error`,
and `Filesystem has been shut down`.

**Root cause:** The VM booted an existing Bootwright-owned libvirt root disk
instead of the current managed OS install ISO. This can happen after an
interrupted or previous run leaves a bootable qcow2 in
`/var/lib/libvirt/images/bootwright/<context>/clusters/<cluster>/machines/<machine>/`.
An open TCP/22 socket is then a false readiness signal: the stale or damaged OS
may accept TCP and reset before sending SSH host keys.

**Fix:** Keep the managed OS libvirt install media disk-first with CD fallback,
not CD-first. Anaconda's Kickstart `reboot` is an in-guest reboot that reuses
the running domain's boot order, and libvirt cannot change disk boot order on an
already-running VM (see `openshift-agent-iso-reboot-loop.md`); a CD-first order
therefore boots the install ISO again after the disk is written, and clearing
only the *persistent* CD-ROM config cannot redirect that live reboot. Disk-first
boots the empty disk through to the attached ISO on the install pass, then the
post-install reboot boots the freshly written disk. Force reinstall over a stale
disk by wiping it, not by booting CD-first: when `bootwright apply --override` is
supplied for managed OS, the libvirt substrate path must stop and undefine the
Bootwright-owned machine domain, remove only that machine's Bootwright-owned
libvirt state/storage directories, and recreate the disks from desired state
before booting the installer. Without `--override`, preserve existing qcow2
disks and fail closed on unsafe reachable-OS marker mismatches.
