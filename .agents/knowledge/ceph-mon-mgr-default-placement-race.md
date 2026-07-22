# Ceph mon/mgr stray daemon: the default-placement race on multi-host installs

**Root cause:** `cephadm bootstrap` seeds a default `mon` (and `mgr`) placement
that is unrestricted (cephadm auto-grows mon up to 5 hosts, with no host
filter) until an explicit placement spec overrides it. Bootwright used to
apply hosts and the narrow, declared mon/mgr placement as two separate
`ceph orch apply` calls, with the OSD-device reclaim step
(`osd_reclaim.yml`, real per-host `wipefs`/`sgdisk` work) sandwiched in
between. `ceph orch apply` is asynchronous and cephadm wakes its reconciler
immediately on host-add, so in that window cephadm's still-default mon
service can auto-schedule a mon onto a host the declared topology never
gave the `mon` role. When the narrow spec then lands, cephadm's
reconciliation removes the daemon from the monmap but can leave the actual
container/systemd unit running and unmanaged — that daemon is invisible to
`ceph orch ps` and shows up only as `CEPHADM_STRAY_DAEMON`.

**When it bites:** A fresh, otherwise-successful multi-host install (5+
hosts, e.g. a 2-site stretch layout) reports
`HEALTH_WARN: 1 stray daemon(s) not managed by cephadm` shortly after
`bootwright apply` finishes. `ceph orch ps` shows exactly the declared mon
set; `ceph mon dump` confirms the stray name is absent from the monmap
(cephadm demoted it, but didn't clean up the container) — safe to remove
directly on the affected host with `cephadm rm-daemon --fsid <fsid> --name
mon.<host> --force` once the monmap check confirms it's not in quorum.

**Fix:** `CephadmBootstrapSpec` now appends the `mon`/`mgr` placement docs
after the host docs, so `bootstrap-spec.yaml` carries hosts + mon + mgr in
one `ceph orch apply -i` call — the narrow placement lands atomically with
host registration, closing the window before `osd_reclaim.yml` runs.
`CephadmCoreServicesSpec`/`core-services.yaml` now carries OSD specs only;
that apply still runs after device reclaim, unchanged.
