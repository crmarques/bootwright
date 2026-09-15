# sushy-tools emulated BMC container

Observed while preparing M1h's Task 0 qualification and reviewing the image's
own source; [Substrates](../../specs/substrates.md#machine-realization) owns
the required behavior. This page records what the image actually does, not what
it should do.

Image `quay.io/metal3-io/sushy-tools`, digest
`sha256:f760343718e1175343f230ec43496a4f0d850f1d0a3139b92db1e475ac91d11e`,
built 2026-09-09, carrying sushy-tools `2.2.1.dev14` on Debian 12 with Python
3.12 at `/usr/local/bin/python3`. Resolved from the publisher's `latest` tag and
driven by hand against podman 5.8.4 on Fedora 43 on 2026-09-15.

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
  itself; the installation role selects `Hdd` explicitly anyway before it powers
  the installed system on.
- **Power** maps `On` to `domain.create()`, `ForceOff` to `domain.destroy()`,
  `GracefulShutdown` to `domain.shutdown()`; `PowerState` is `On` when the
  domain is active and `Off` otherwise. An installer that ends with `poweroff`
  is therefore observed as `Off`, which is how the installation role knows it
  finished without an agent in the installer environment.
- **Virtual media lives under the system**, not the manager, in this version:
  `controllers/virtual_media.py` registers
  `/redfish/v1/Systems/<identity>/VirtualMedia/<device>` with the
  `VirtualMedia.InsertMedia` and `VirtualMedia.EjectMedia` actions beneath it,
  and the manager resource links there. `redfish_control` builds its paths from
  the system endpoint for that reason.

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
