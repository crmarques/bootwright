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
[ceph_native.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py),
the dashboard phase waits for publication before scheduling and checks every
intended manager's refreshed deployment ports before completing. A bounded
repair retains the exact manager/container plan and an intent for each target;
losing a scheduling response never authorizes another targeted redeploy.
The [storage contract](../../specs/storage.md#bootstrap-identity-and-continuation)
owns that behavior. The
[dashboard regressions](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_ceph_dashboard.py)
model certificate storage, endpoint publication and deployment port capture as
separate events.

The durable repair plan and per-target intents belong to the core v6
contract. The [format classifier](../../cmd/bootwright/contract_versions_test.go)
holds their persisted ownership to the core content digest. Keep the original
redeploy records unchanged; an older registered operation is not upgraded by
installing a new automation build.

An explicit native reconfiguration can reopen discovered ports without
restarting a Ceph daemon, but cephadm's reconfiguration path skips unit-file
rewriting. It does not establish that refreshed deployment metadata changed.
An unchanged completed Bootwright apply is also a lifecycle no-op; a new
automation build alone does not repair an already completed cluster. See
[continuation after a fix](continuation-after-a-fix.md) for the boundary between
continuation and completed operations.
