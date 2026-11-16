# Discovery ISO peak allocation

Applies to the RHEL x86 inventory image in
[`disk_discovery_media.py`](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/disk_discovery_media.py)
and its [owning contract](../../specs/managed-os.md#automatic-root-disk-discovery).

The former two-pass build retained `patched.iso`, both extracted initrds,
`discovery.iso` and a separate publication copy per node. Source media and
`packageSource.hostedTree` were already shared; adding another shared package
cache would not address this peak.

Lorax's documented
[`--add`](https://weldr.io/lorax/f40-branch/mkksiso.html) accepts directories.
Its [RHEL 9 implementation](https://raw.githubusercontent.com/weldr/lorax/rhel9-branch/src/bin/mkksiso)
maps their basenames after updating boot configurations. The
[xorriso manual](https://www.gnu.org/software/xorriso/man_1_xorriso.html)
specifies that colliding directories merge recursively. Supplying only the
patched initrd paths therefore retains the other boot files and avoids the
intermediate ISO. `TMPDIR` keeps Lorax's temporary content in attributed
scratch. The kernel, shim and GRUB executables are not rewritten.

Publication needs no clone or hard link: standard
[`rename`](https://man7.org/linux/man-pages/man2/rename.2.html) transfers the
private file to a stage, then atomically replaces the destination. `EXDEV`
selects an exact-size copy. This needs neither reflink nor sparse-file support
on the [RHEL local filesystems](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/managing_file_systems/overview-of-available-file-systems_managing-file-systems).
Hard links would make permission or content changes visible to another
consumer, so publication refuses a source with multiple links. A shared cache
of patched initrds would also hold private attempt material. Neither is used.

The inventory boot never reads the installer second stage, which is 1,252,827,136
of the RHEL 9.8 boot ISO's 1,509,949,440 bytes. Since 2026-11-16 the builder
first copies only the boot tree with `xorriso -boot_image any replay -rm_r`
(the [xorriso manual](https://www.gnu.org/software/xorriso/man_1_xorriso.html)
documents both), then runs the same one-pass `mkksiso --add`. On the actual
RHEL 9.8 boot ISO, with Lorax 44.7, xorriso 1.5.8 and `--skip-mkefiboot`, the
copy was 256,835,584 bytes and kept the BIOS (`/isolinux/isolinux.bin`) and
UEFI (`/images/efiboot.img`) El Torito entries; the finished image was
482,738,176 bytes, against 1,735,589,888 before, because the two patched
initrds no longer share one extent and nothing else remains. Its GRUB entries
kept the inventory arguments with `inst.stage2` removed. This measurement does
not qualify embedded EFI rebuilding, which production keeps, nor a BMC's
transfer time, which depends on whether it streams or copies the image.

Three synthetic concurrent nodes using the actual RHEL 9.8 boot ISO were
measured by sampling newly written files' and directories' `st_blocks * 512`
every 10 ms, including native temporary files. There were no reflink or
hard-link copies in the measured trees. The unchanged package tree is excluded.

| Peak allocated bytes | Before | After |
| --- | ---: | ---: |
| Additional discovery files | 16,976,424,960 | 5,658,832,896 |
| Including one shared source ISO | 18,486,374,400 | 7,168,782,336 |

The additional peak fell 66.7%; including the shared source it fell 61.2%.
The measurement used a Btrfs development filesystem, Lorax 44.7 and xorriso
1.5.8. Both runs used Lorax's documented `--skip-mkefiboot` in the disposable
fixture because root privileges were unavailable. Production keeps embedded
EFI rebuilding enabled. This comparison does not qualify that rebuilding or
prove the same exact allocation on XFS/ext4 or under quotas.

These are Bootwright measurements and tool-contract observations, not a Red
Hat support claim for the custom inventory image. RHEL-host native-tool and
physical BMC acceptance remain operator gates. Exact optimized-media BIOS and
UEFI CD boots produced authenticated receipts, acknowledged shutdown, zero
synthetic-disk writes and unchanged disk hashes. All ten menu entries retained
the inventory-only arguments; vendor EFI executables and kernels matched the
source byte-for-byte. A separate RAM-only probe confirmed SecureBoot enabled
and SetupMode off under OVMF; that observation alone does not qualify the full
inventory journey. A later isolated Q35 SATA probe with the actual RHEL 9.8 initramfs
loaded AHCI and reached authenticated inventory despite unmatched optional PCI
and SCSI aliases. A final alias warning alone therefore proves neither a driver
failure nor a stopped worker; use the fixed bootstrap phases and receipt state.
The focused
[`test_disk_discovery_space.py`](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/test_disk_discovery_space.py)
guards one-pass arguments, transfer/fallback, concurrent capacity and recovery.
