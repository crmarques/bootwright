# Anaconda registration with a Satellite

Read from the installer's source on 2026-11-17 (branches `rhel-9` and
`rhel-10` of `rhinstaller/anaconda`); none of it is proved by a live
installation yet. Required behavior is in
[subscription package source](../../specs/managed-os-subscription-source.md).
It informs why `subscription_source.go` in `internal/managedos/installation`
writes the Kickstart it does; `subscription_source_test.go` there runs the
`%pre` under a harness.

## The default Satellite path runs an unverified script as root

A Kickstart `rhsm --server-hostname=<host>` makes Anaconda treat the host as a
Satellite. Before it registers, `download_satellite_provisioning_script`
(`pyanaconda/modules/subscription/satellite.py`) prefixes the host with
`http://`, fetches `/pub/katello-rhsm-consumer` with `verify=False`, and
`run_satellite_provisioning_script` runs the result with `bash` as root, once
in the installer and once in the installed system's chroot
(`ProvisionTargetSystemForSatelliteTask`, `installation.py`). The source's own
note says to use it only on a trusted network. That script is what installs
the Satellite's CA and edits `rhsm.conf`. Reports of
`certificate verify failed` on RHEL 10 trace to that script's
`update-ca-trust` call.

## The installer's own switch

`SERVER_HOSTNAME_NOT_SATELLITE_PREFIX` is `not-satellite:`
(`pyanaconda/modules/subscription/constants.py`). With it,
`_provision_system_for_satellite` returns before any download, and
`SetRHSMConfigurationTask` strips the prefix and sets `server.hostname`, the
proxy keys and `rhsm.baseurl` through the RHSM `Config` D-Bus object
(`runtime.py`). It sets no other key: not `server.prefix`, which stays the
hosted service's `/subscription` where a Satellite serves registration under
`/rhsm`, nor `server.port`, nor `rhsm.repo_ca_cert`. The provisioning script is
what sets those, so under the prefix the `%pre` sets them through the same
object before registration, as the installed system's worker does with
`subscription-manager config`; an independent review caught their absence
before any live run. Keys set that way survive Anaconda's own `SetAll`, and
the service verifies its server against every PEM in `/etc/rhsm/ca`. An installer older than this constant has no such
switch and passes the prefixed string to RHSM as a host name, which fails.

## The RHSM Config proxy

dasbus parses the standard interfaces into every proxy's specification and an
`ObjectProxy` resolves a member name to them first
(`dasbus/client/proxy.py`, `ObjectProxy._get_interface`). RHSM's `Config`
object names `Get`, `GetAll` and `Set` as `org.freedesktop.DBus.Properties`
does, so `get_proxy(service, path).Get("server.prefix", "")` calls
`Properties.Get` with `server.prefix` as an interface name and fails, while
`SetAll`, which no standard interface names, reaches RHSM. Anaconda passes
`interface_name=` for the same object (`initialization.py`,
`StartRHSMTask`), which also starts `rhsm.service` in a thread that `%pre`
does not wait for. The first real installation from a subscription source
stopped at `Running pre-installation scripts` with Anaconda's `ScriptError`
([B1693](../../specs/milestones/m1.md#b1693)); the dasbus behavior is read
from dasbus 1.7, and the live cause is not yet confirmed by the installer's
log.

## Order and what reaches the installed system

`anaconda.py` runs the `%pre` scripts, then parses the Kickstart with its
includes, then starts the registration thread when the organization and a key
are present, so a `%pre` can write the `rhsm` command and prepare trust first.
The modules, the RHSM service among them, are already started when `%pre`
runs. Registration calls `RegisterWithActivationKeys` with empty options, so
no content view can be named there. `TransferSubscriptionTokensTask` copies
`/etc/pki/entitlement/*.pem`, the consumer key and certificate, `redhat.repo`
and `rhsm.conf` to the installed system, and nothing from `/etc/rhsm/ca`.
`RestoreRHSMDefaultsTask` sets the log level back from `DEBUG` first; at
`DEBUG` the installer's own `rhsm.log` can hold request paths, which is why
the operator gate checks what the installed system keeps. The subscription
module does not write the `rhsm` command into the Kickstart it saves.

## Release path

`get_product_release_version` (`payload/dnf/utils.py`) sets DNF's `releasever`
to the leading digits and dots of the product version, so a 9.8 boot image
asks for `.../9.8/...`, where an installed system would ask for `.../9/...`.
A content view synced only at the major then fails the installer's source
setup with `Error setting up software source` at the menu, after every
`%pre` passed ([B1697](../../specs/milestones/m1.md#b1697)).

`DNFManager._create_base` (`payload/dnf/dnf_manager.py`) sets that value and
then calls `base.conf.substitutions.update_from_etc("/")`, which reads the
installer's own `/etc/dnf/vars` and overwrites any variable a file there
names (rhbz#1920735). The base is built lazily: `DNFPayload.__init__` and
`set_from_opts` (`pyanaconda/payload/dnf/payload.py`) build it, and both run
after `run_pre_scripts` in `anaconda.py`; source setup reuses it. So a `%pre`
that writes `/etc/dnf/vars/releasever` sets the path the installer reads.
Read from the rhel-9 branch on 2026-11-24.

## Tools in the installer

`install(1)` is not assumed there; the existing `%pre` scripts use `mkdir`,
`chmod`, `cat`, `rm`, `curl` and `nmcli`, and this one adds `python3` with
`dasbus`, which Anaconda itself runs on.
