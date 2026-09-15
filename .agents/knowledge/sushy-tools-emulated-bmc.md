# sushy-tools emulated BMC container

Observed while preparing M1h's Task 0 qualification;
[Substrates](../../specs/substrates.md#machine-realization) owns the required
behavior. This page records what the image actually does, not what it should do.

Image `quay.io/metal3-io/sushy-tools`, digest
`sha256:f760343718e1175343f230ec43496a4f0d850f1d0a3139b92db1e475ac91d11e`,
built 2026-09-09, carrying sushy-tools `2.2.1.dev14` on Debian 12. Resolved
from the publisher's `latest` tag and driven by hand against podman 5.8.4 on
Fedora 43 on 2026-09-15.

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
exercised here were `SUSHY_EMULATOR_LISTEN_IP`, `SUSHY_EMULATOR_LISTEN_PORT`,
`SUSHY_EMULATOR_LIBVIRT_URI` and `SUSHY_EMULATOR_VMEDIA_VERIFY_SSL`.

A `qemu+unix:///session?socket=<path>` URI over a bind-mounted libvirt socket
is enough to start the emulator, which binds its configured address during
startup rather than on first request.

## Not yet qualified

The Redfish surface itself was not exercised: the `Systems` collection, the
`VirtualMedia` insert and eject actions, `Boot` override, the power actions,
and the bcrypt basic-authentication file all remain for the by-hand
qualification [M1h](../../specs/milestones.md#m1h--managed-rhel-on-emulated-bare-metal)
schedules before the `substrate_libvirt_machine` role is written. `mkksiso`
against RHEL 9.7 boot media is unqualified for the same reason: the media is
subscription content this repository does not carry.
