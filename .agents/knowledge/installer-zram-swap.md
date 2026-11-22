# Installer Swap Beyond `inst.zram=0`

## Observation

With [B1541 and B1542](../../specs/milestones/m1.md#b1541) fixed, the erase
image booted on a bonded VLAN, reached its gateway and started its receipt
server. The erase service failed within a second and the console stayed
empty. Disk identification succeeded and `lsblk` showed active zram swap.

- `inst.zram=0` stops only Anaconda's own zram swap. The installer runtime also
  ships the [systemd zram generator](https://github.com/systemd/zram-generator/blob/main/man/zram-generator.md),
  whose `systemd-zram-setup@zram0.service` activates `dev-zram0.swap`
  (`min(ram, 8 GiB)`) on every boot unless the kernel command line carries
  `systemd.zram=0`. The worker's `swapless` proof refuses any active swap with
  `active-swap`, so every erase boot refused before its first status.
- With no `status.json`, the forced `serve` command exited 1. The controller
  counts any exit other than 255 as an answer with no report, so the wait
  stopped its `no answer` note and waited silently toward the receipt
  deadline.
- The media's `quiet` hides the slow stage2 RAM copy over BMC virtual media,
  and isolation then clears tty1, so the console looked the same whatever the
  worker did.

A VM with the same boot image and arguments reproduced the active zram swap;
adding `systemd.zram=0` left no zram device. A VM also showed the initramfs
`/run` tmpfs is 20% of RAM: below about 7 GiB the 1.25 GB `install.img` copy is
truncated and dracut halts with `Failed to find a root filesystem in
/run/initramfs/squashed.img`; a qemu `qemu64` CPU panics `/init` with exit 127
(RHEL 9 needs x86-64-v2, so use `-cpu host`).

## Decision it informs

The erase image disables both swaps, drops `quiet` and `rhgb`, and the RAM
service names its state on the console; a worker stopped before its first
status reports failed. The rules are owned by
[erase image](../../specs/state-reconciliation.md#erase-image) and
[erase cancellation and refusals](../../specs/state-reconciliation.md#erase-cancellation-and-refusals);
delivery is [B1559 to B1561](../../specs/milestones/m1.md#b1559).

## Evidence

`physical_erase_controller.py` (`build`), `physical_erase_runtime.py`
(`status`, `announce`, `stopped`); `tests/unit/test_physical_erase_controller.py`
holds every argument the runtime requires to the build's command line, and
`tests/unit/test_physical_erase_worker.py` the stopped worker and console line.
As with [the `%pre` runtime](installer-runtime-pre-script.md), only a real boot
qualifies the image.
