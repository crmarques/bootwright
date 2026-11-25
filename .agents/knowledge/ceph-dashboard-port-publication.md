# Dashboard publication and manager deployment ports

The native orchestrator's
[MgrService.prepare_create](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/services/cephadmservice.py)
derives manager deployment ports from the module endpoints currently returned
by `mgr services`. A stored dashboard certificate does not establish that its
endpoint has been published. Scheduling a manager redeploy between those two
events can therefore omit the dashboard port from native deployment.

The native
[deployment path](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/serve.py)
passes those ports to cephadm, which
[opens the host firewall](https://github.com/ceph/ceph/blob/v20.2.1/src/cephadm/cephadm.py).
A changed running container and a dashboard URL observed afterwards do not
prove the port was included when deployment was prepared. Manager failover can
also interrupt endpoint publication during a multi-manager redeploy.

In
[ceph_native_dashboard.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_dashboard.py),
the dashboard phase waits for publication before scheduling and checks every
intended manager's refreshed deployment ports before completing. A bounded
repair retains the exact manager/container plan and an intent for each target.
It writes an intent only inside the transport and cache holds, just before the
native call, so a hold refusal leaves no intent.

A target that still runs its recorded container without the port is requested
again at most once per apply, after the publication proof, with standbys and
then unattempted targets first. The orchestrator's
[daemon_action](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/module.py)
(lines 3285-3357) only records and persists a scheduled action, and
[schedule_daemon_action](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/inventory.py)
(lines 2129-2145) merges a second redeploy into the pending one. A repeated
request therefore restarts that manager at most once.

The repair never asks cephadm to redeploy the active manager. For the active
manager,
[_daemon_action](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/module.py)
(lines 3214-3216) fails over first. The monitor's
[drop_active](https://github.com/ceph/ceph/blob/v20.2.1/src/mon/MgrMonitor.cc)
(line 930) clears the services map, so that redeploy can be prepared without
the dashboard port. When a target is the active manager, the repair runs
`ceph mgr fail` on it and waits for another active manager. It then proves
publication and re-reads the containers before redeploying the former active,
now a standby. It fails over only once every other missing manager is seen
replaced: the serve loop's
[_check_daemons](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/serve.py)
(lines 1696-1708) runs a scheduled redeploy later, on whichever manager is then
active, so a standby still pending at failover would become the active target.
The [storage contract](../../specs/ceph.md#bootstrap-identity-and-continuation)
owns that behavior. The
[dashboard regressions](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_ceph_dashboard.py)
model certificate storage, endpoint publication and deployment port capture as
separate events.

The initial service action also schedules each daemon separately.
A mixture of original and replacement containers can persist with the
dashboard endpoint published and the TLS port present on every manager.
The old continuation waited until timeout on every such attempt. Its service
retry required all original containers and its port repair required all
replacements.

During the initial rollout and on continuation,
[cephadm's daemon listing](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/module.py)
can still return its cached inventory immediately after `orch ps --refresh`.
A second read must show a later `last_refresh` on every host with the same
running container identities before Bootwright issues a targeted request.
After a failover it waits for the newly active manager to serve the dashboard
and refresh the inventory again. The retained initial attempt covers the
original containers still awaiting replacement.

The IBM cephadm `HostCache.save_host` in images 9.9.1 and 9.9.2 stores the
daemon observations and scheduled actions together. Its
`_potentially_split_and_save_host_entry` writes numbered config keys starting
at `mgr/cephadm/host.<hostname>;0`. Reading the unnumbered key can return
`ENOENT` even while the manager is running. A prefix dump reads those parts
together. The worker only retries a standby when that record still identifies
its original running container and shows an empty action queue for it.
A queued redeploy of the last original active can proceed after failover.
The worker addresses the observed active GID and verifies that every
registered standby belongs to the running placement.

The durable repair plan and per-target intents belong to the core v6
contract. The [format classifier](../../cmd/bootwright/contract_versions_test.go)
holds their persisted ownership to the core content digest. Keep the original
redeploy records unchanged; an older registered operation is not upgraded by
installing a new automation build.

An explicit native reconfiguration also re-derives the ports from `mgr services`
and opens them without restarting a Ceph daemon. It skips unit-file rewriting,
but the cephadm binary of 20.1.0-221, 20.2.1-405 and 20.2.2-349 persists the
payload's ports into `unit.meta` on reconfiguration (`Reconfig: update
unit.meta so ports/ip from deploy payload are persisted`, `cephadm.py` line
1253 at 20.2.1), so refreshed deployment metadata shows them. The monitoring
unit relies on that for the prometheus module's port; see
[the exporter port and firewalld](ibm-ceph-monitoring.md#the-exporter-port-and-firewalld).
An unchanged completed Bootwright apply is also a lifecycle no-op; a new
automation build alone does not repair an already completed cluster. See
[continuation after a fix](continuation-after-a-fix.md) for the boundary between
continuation and completed operations.

## Native firewall opening

Upstream cephadm at v20.2.1 opens each deployed daemon's TCP endpoint ports
with `firewall-cmd --permanent --add-port` in firewalld's default zone and then
reloads when `firewalld.service` is enabled and running
([cephadm.py](https://github.com/ceph/ceph/blob/v20.2.1/src/cephadm/cephadm.py#L976-L983),
[firewalld.py](https://github.com/ceph/ceph/blob/v20.2.1/src/cephadm/cephadmlib/firewalld.py#L84-L119),
[serve.py](https://github.com/ceph/ceph/blob/v20.2.1/src/pybind/mgr/cephadm/serve.py#L1371-L1383)).
It never opens VRRP for keepalived, which has no firewalld service form
([ingress.py](https://github.com/ceph/ceph/blob/v20.2.1/src/cephadm/cephadmlib/daemons/ingress.py#L150-L160)).
