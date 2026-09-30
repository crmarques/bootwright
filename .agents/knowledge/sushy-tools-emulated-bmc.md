# sushy-tools emulated BMC container

Observed while preparing M1h's Task 0 qualification and reviewing the image's
own source; [Substrates](../../specs/substrates.md#machine-realization) owns
the required behavior. This page records what the image actually does, not what
it should do.

Image `quay.io/metal3-io/sushy-tools`, digest
`sha256:f760343718e1175343f230ec43496a4f0d850f1d0a3139b92db1e475ac91d11e`,
built 2026-09-09, carrying sushy-tools `2.2.1.dev14` (git `3b57e7a`, from its
dist-info `pbr.json`) on Debian 12 with Python 3.12 at `/usr/local/bin/python3`.
Resolved from the publisher's `latest` tag and driven by hand against podman
5.8.4 on Fedora 43 on 2026-09-15.

What the Redfish client relies on, read from that image's `sushy_tools/emulator`
on 2026-09-28:

- `BootSourceOverrideEnabled` is always `Continuous` (`templates/system.json`
  lines 19 and 42), because the PATCH ignores the field (`main.py` 593-597)
  before answering 204 (620), and no
  `BootSourceOverrideEnabled@Redfish.AllowableValues` appears anywhere. A
  boot read-back therefore accepts `Continuous` from this controller.
- The reported target follows the PATCH: `set_boot_device` makes the selected
  device the only bootable one (`resources/systems/libvirtdriver.py` 416), and
  the target reported is the lowest-ordered boot device (325-405).
- No `etag` appears anywhere, and the system resource carries no
  `SerialNumber`, so a boot selection is sent without `If-Match` and a target
  proof identifies the machine by its `UUID`.
- The reset advertises `ForceOn` among its types (`templates/system.json`
  127-139), handles it like `On` (`libvirtdriver.py` 299-301) and answers 204
  (`main.py` 713), so power-on sends `ForceOn`.
- The manager, built from the system's UUID, links the system's own
  VirtualMedia collection (`main.py` 366-368), which lists `Cd` by that UUID
  (`templates/virtual_media_collection.json` line 9); discovery fetches that
  collection once.
- The insert reads only `Image`, `Inserted`, `WriteProtected`, `UserName` and
  `Password` (`controllers/virtual_media.py` 172-176), so it ignores
  `TransferProtocolType`, and downloads the image inside the request (184-186)
  before answering 204 (204): the attach is synchronous and bounded by the
  client's media timeout.
- Virtual-media certificate trust, read from the upstream source at `3b57e7a`
  on 2026-09-30 and not driven against the image: the device links its
  `Certificates` collection and reports `VerifyCertificate`
  ([templates/virtual_media.json](https://github.com/openstack/sushy-tools/blob/3b57e7a/sushy_tools/emulator/templates/virtual_media.json)
  27-30). A PATCH of `VerifyCertificate` answers 204 and reads no `If-Match`;
  the collection's POST takes `CertificateString` and a `PEM`
  `CertificateType`, refuses any other type, and answers 204 with a
  `Location`; a member's GET and DELETE are served beside it
  ([controllers/virtual_media.py](https://github.com/openstack/sushy-tools/blob/3b57e7a/sushy_tools/emulator/controllers/virtual_media.py)
  59-145). A device holds one certificate, `Default`, and a second POST answers
  409; the insert fetches with `requests` `verify=` set from the device's
  `Verify`, defaulting to `SUSHY_EMULATOR_VMEDIA_VERIFY_SSL`, and uses the
  imported certificate as that trust when verification is on
  ([resources/vmedia.py](https://github.com/openstack/sushy-tools/blob/3b57e7a/sushy_tools/emulator/resources/vmedia.py)
  49, 128-165, 231-275). Bootwright's emulated controllers are `established`
  and never import, so this surface is used only by the client's tests.

## The stock command must not be used

The image declares no entrypoint and its command is
`/bin/sh -c /usr/local/bin/redfish-emulator.sh`. That wrapper ends in
`exec /usr/local/bin/sushy-emulator --debug ...`, so the stock command runs
Flask's development server **with the Werkzeug debugger active**: the startup
log prints a debugger PIN, and the debugger is remote code execution for anyone
who can reach the listener. A BMC unit binds an address hosted machines and
operators reach, so the unit sets `Entrypoint=/usr/local/bin/sushy-emulator`
and its own `Exec` explicitly and never invokes the wrapper. This is the same
lesson the four managed service images taught in
[network-service knowledge](managed-network-service-runtime.md).

The wrapper also appends `--interface ::` whenever the config file does not set
`SUSHY_EMULATOR_LISTEN_IP`, which listens on every interface. The frozen
request always sets the bind address, so this default is never relied on.

## Configuration

The emulator reads a Python config file, `/root/sushy/conf.py` unless
`SUSHY_TOOLS_CONFIG` names another path, and takes it as `--config`. The keys
the role sets are `SUSHY_EMULATOR_LISTEN_IP`, `SUSHY_EMULATOR_LISTEN_PORT`,
`SUSHY_EMULATOR_LIBVIRT_URI`, `SUSHY_EMULATOR_ALLOWED_INSTANCES`,
`SUSHY_EMULATOR_STORAGE_POOL`, `SUSHY_EMULATOR_AUTH_FILE`,
`SUSHY_EMULATOR_VMEDIA_VERIFY_SSL` and `SUSHY_EMULATOR_VMEDIA_DEVICES`.

A `qemu:///system` URI over the host's bind-mounted `/run/libvirt` is enough
to start the emulator, which binds its configured address during startup rather
than on first request. Connecting to that socket from a confined container is
refused by SELinux, so the unit sets `SecurityLabelDisable=true`.

## What the source says (`sushy_tools/emulator`)

- **Authentication is bcrypt only.** `auth_basic.py` parses Apache-style
  `user:hash` lines and calls `bcrypt.checkpw`; any other digest raises
  "Only bcrypt digested passwords are supported". The role derives the hash
  with the image's own `python3 -c 'import bcrypt…'`, reading the password on
  stdin, so no host tool and no argument carries it.
- **Inserted media becomes a libvirt volume.** `insert_image` downloads the URL
  with `requests` (`verify` from `SUSHY_EMULATOR_VMEDIA_VERIFY_SSL`) into the
  container's own temporary directory, then `_upload_image` creates a raw
  volume named `<image>-<uuid>.img` in the pool
  `SUSHY_EMULATOR_STORAGE_POOL` (default `default`) and streams the bytes to it
  through the libvirt connection. The container therefore mounts no pool
  directory, and the pool must exist and be active before the first insert:
  the provider host block defines it and the machine's config names it.
- **Every `cdrom` disk is the emulator's.** `_remove_boot_images` deletes every
  `<disk device="cdrom">` before adding its own on the `sata` bus for a `q35`
  machine, so the frozen domain defines no cdrom.
- **Boot override is per device.** `set_boot_device` removes every
  `<os><boot>` element and sets `<boot order>` on the selected disk, keeping
  disk entries bootable. Ejecting media restores the boot device to `Hdd` by
  itself, so after an eject only the disk is bootable until the domain is
  defined again. The substrate's disk-boot entry point selects `Hdd` explicitly
  anyway, and its libvirt boot entry point selects the media, `Cd`, for an
  installer that powers the machine off
  ([substrates](../../specs/substrates.md#identity-and-power-operations)).
- **Selections live in the persistent definition.** Read from the image's
  source on 2026-09-29, in the layer of the pinned image (its `pbr.json`
  records git `3b57e7a`) that local container storage holds:
  `get_boot_device` reads the inactive domain XML (`libvirtdriver.py` 344) and
  `set_boot_device` defines it again (`get_xml_desc` defaults to the inactive
  XML at 222, `defineXML` at 410), so a selection made while the domain runs
  is reported at once. An insert gives the new `cdrom` disk no `<boot>` element
  (1494-1519) and re-applies the selection only when the device reported as
  the boot device is already the inserted one (1571, 1587-1588); an eject
  selects `Hdd` (1589-1591). So an insert after an eject leaves the media
  unbootable until `Cd` is selected or the domain is defined again with its
  declared order. Not observed on a running host.
- **Power** maps `On` to `domain.create()`, `ForceOff` to `domain.destroy()`,
  `GracefulShutdown` to `domain.shutdown()`; `PowerState` is `On` when the
  domain is active and `Off` otherwise. An installer that ends with `poweroff`
  is therefore observed as `Off`, which is how the installation role knows it
  finished without an agent in the installer environment.
- **Virtual media lives under the system**, not the manager, in this version:
  `controllers/virtual_media.py` registers
  `/redfish/v1/Systems/<identity>/VirtualMedia/<device>` with the
  `VirtualMedia.InsertMedia` and `VirtualMedia.EjectMedia` actions beneath it,
  and the manager resource links there. The
  [Redfish client](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/redfish_control.py)
  builds none of these paths itself: it discovers the device and its actions
  from what the controller advertises, and this emulator is one of the three
  firmware shapes its
  [tests](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/modules/test_redfish_boot.py)
  drive, beside the manager-scoped and dual-view shapes
  [physical controllers](redfish-physical-bmc.md) show.

## Host firewall

libvirt places every bridge it creates in firewalld's `libvirt` zone, and that
zone, as shipped and as found on the Fedora 43 lab host, ends in a
lowest-priority `reject` rule after `dhcp`, `dns`, `ssh` and `tftp`. A guest on
such a bridge cannot reach the controller's artifact server or time source, so
Anaconda's package fetch fails. The managed network therefore declares
`<bridge zone="trusted"/>`, which libvirt applies when it creates the bridge
and removes when the network is undefined; nothing touches firewalld directly.

## Not yet exercised end to end

The Redfish surface above was read from the source, not driven through a
complete installation; `mkksiso` and `xorriso` against RHEL 9.8 media were
checked by hand only for their argument surface. The first real run of
`examples/lab-rhel` is where both are proved.
