# The release a RHEL boot ISO names

A RHEL boot ISO carries no `/.treeinfo`, so `media add` once recorded no
release for it, and a plan whose boot image had to name the release (a
`subscription` package source, or disk discovery of such a Machine) refused
with `install.media-unusable` `the media store holds no release for <image>`.
The unit fixtures gave the boot image's record a release the real image could
not produce, which hid the defect
([B1628](../../specs/milestones/m1.md#b1628)). The owner chose, on 2026-11-18,
to read the release from the image's `/.discinfo` instead; the
[media store](../../specs/managed-os.md#media-store) owns the reading and the
plan refusals.

## Observed on the vendor images, 2026-11-18

These are observations of these builds, read from their bytes; they are not a
version rule.

- `rhel-9.8-x86_64-boot.iso` and `rhel-9.7-x86_64-boot.iso`: the Joliet root
  holds exactly `.discinfo`, `EFI`, `images` and `isolinux`; no `.treeinfo`.
  `/.discinfo` is 29 bytes, three LF-terminated lines: a timestamp such as
  `1778519505.201133`, then `9.8` (respectively `9.7`), then `x86_64`.
- `rhel-9.8-x86_64-dvd.iso`: the Joliet root holds `.treeinfo` (family `Red Hat
  Enterprise Linux`, version `9.8`, arch `x86_64`) and a 60-byte `/.discinfo`
  of four lines: timestamp, `Red Hat Enterprise Linux 9.8.0`, `x86_64`, `ALL`.
- The file is written by lorax's
  [`DiscInfo`](https://github.com/weldr/lorax/blob/master/src/pylorax/discinfo.py):
  timestamp, release and architecture, one per line.

## Decision it informs

- A `.discinfo` is read only when the root holds no `.treeinfo` record, and
  names a release only when its second line is a bare dotted version, so the
  DVD's descriptive line never yields one and the DVD stays read from its
  `.treeinfo`.
- A `.discinfo` names no family, so its release has an empty `family`. An image
  an installation installs packages from still needs the Red Hat Enterprise
  Linux family; a boot image that only boots the installer does not.
- A boot image added by an earlier build keeps its record: delete it and add
  it again with this build so its record names the release.

Code: `ReadMediaRelease` and `parseDiscinfo` in
[treeinfo.go](../../internal/managedos/treeinfo.go), `ValidMediaRelease` in
[media.go](../../internal/managedos/media.go) and `RefusedReleases` in
[selection.go](../../internal/managedos/installation/selection.go). Evidence:
[treeinfo_test.go](../../internal/managedos/treeinfo_test.go),
[media_pin_test.go](../../internal/managedos/installation/media_pin_test.go)
and
[subscription_source_test.go](../../internal/managedos/installation/subscription_source_test.go).
