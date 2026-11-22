# Installer Runtime Under a Never-Returning `%pre`

## Observation

The first real-hardware boot of the physical erase image (RHEL 9.8 boot ISO,
Anaconda 34.25.7.14, `fips=1`, an active-backup bond with the
install address on a VLAN) never answered the controller, which refused
`worker-unreachable` after its 20-minute bound. The console showed
`Running pre-installation scripts` and, on the TTY2 shell:

- `/run/bootwright-erase/physical_erase_worker.py` at 0 bytes and nothing else;
  `ps` showed the `%pre` (`/tmp/ks-script-*`) asleep in its fail-closed loop.
  Lorax's [`runtime-cleanup.tmpl`](https://github.com/weldr/lorax/blob/rhel9-branch/share/templates.d/99-generic/runtime-cleanup.tmpl)
  removes `/usr/bin/base64` from the installer runtime, with `usermod`,
  `/usr/bin/test`, `printf`, `timeout` and others (`ss` was absent too), so
  the first `base64 --decode` failed and `set -e` tripped the trap. `python3`, `chpasswd`, `findmnt`,
  `systemctl`, `dmsetup` and `sshd` are kept.
- `ip -br addr` listing the NICs `UP` with no bond, VLAN or address.
  Anaconda runs `%pre` before `initialize_network()`
  ([anaconda.py, rhel-9](https://github.com/rhinstaller/anaconda/blob/rhel-9/anaconda.py)),
  and dracut's Kickstart reader translates only the first `network` line and
  ignores `--vlanid`
  ([parse-kickstart, rhel-9](https://github.com/rhinstaller/anaconda/blob/rhel-9/dracut/parse-kickstart)).
  An install never notices, because its `%pre` returns.

## Decision it informs

Code run inside the installer runtime names only binaries the runtime keeps,
and an image whose `%pre` never returns carries its network as dracut boot
arguments. The rule and its arguments are owned by
[erase image](../../specs/state-reconciliation.md#erase-image); delivery is
[B1541 and B1542](../../specs/milestones/m1.md#b1541).

## Evidence

`physical_erase_boot.py` (`DECODE`, `network_arguments`) and
`physical_erase_runtime.py`; `tests/unit/test_physical_erase_boot.py` runs the
rendered decode lines with no `PATH` and holds every payload binary to the
kept set. A unit test passes on a full Linux host whatever the installer
lacks, so only a real boot qualifies the image.
