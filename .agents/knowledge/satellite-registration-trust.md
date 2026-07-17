# Satellite registration trust chain: kickstart and day-2

**Day-2 Satellite binding order:** when a Ceph entitlement names a corporate
Satellite, `machine_registration_rhsm/tasks/satellite.yml` (machines-phase
`registration.<cluster>` task) binds the storage node to it BEFORE
registering. The
`katello-ca-consumer-latest.noarch.rpm` is installed from the Satellite's
plain-HTTP `/pub` bootstrap path (dnf with `disable_gpg_check: true`) — that
RPM writes the Satellite CA into `/etc/rhsm/ca` and points `rhsm.conf` at the
Satellite so `subscription-manager register` targets it instead of the public
Red Hat CDN. Separately, the entitlement's Satellite CA is copied to a
dedicated system anchor
`/etc/pki/ca-trust/source/anchors/bootwright-satellite-ca.pem` (the same
filename the managed-OS Kickstart uses) plus `update-ca-trust`, for general
dnf/HTTPS content access. All of this is skipped, with behavior unchanged,
when `rhsm.satellite.hostname` is unset.

**Kickstart anchors the CA in BOTH phases:** in
`machine_os_install_anaconda/templates/ks.cfg.j2`, a `%pre --erroronfail`
script anchors the Satellite CA inside the installer runtime — Anaconda runs
every `%pre` before the install transaction, so the CA is in place when the
`rhsm` kickstart command registers and fetches HTTPS content — and `%post`
anchors it again on the installed system so first-boot and day-2
`subscription-manager`/dnf calls validate the Satellite certificate. (Those
script lines are rendered kickstart content, not template commentary.)

**Insights needs its own step:** `subscription-manager register` does not
connect a node to Red Hat Insights on its own. When the entitlement sets
`rhsm.connectToInsights`, the storage node runs an explicit
`insights-client --register`, deliberately matching the managed-OS Kickstart's
`rhsm --connect-to-insights` behavior so both install paths honor the same
entitlement intent.
