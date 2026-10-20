"""A refused Ceph host or cluster operation names its reason to the runner.

Each Ceph role entry point (roles/storage_ceph_host and roles/storage_ceph_core)
runs its module under no_log with its failure kept, publishes the module's
closed reason as a `refused` record through the shared protocol and then fails,
so the runner reports the diagnostic Go gave that reason (hostRefusals and
adapterRefusals in internal/storage/ceph) instead of the adapter's failure.
These cases run the real module entry point over a refusal, render the role's
naming over what it registered and hand the result to the protocol plugin.
Rendering needs Ansible's controller (DataLoader and Templar), so these checks
live here.
"""

from __future__ import annotations

import pathlib

import pytest
import yaml
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.action import ceph_cluster_protocol as protocol
from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol, ceph_core, ceph_native, ceph_ssh_policy
from ansible_collections.bootwright.core.plugins.modules import ceph_cluster, ceph_cluster_protocol, ceph_host

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
LOADER = DataLoader()
PROTOCOL = "bootwright.core.ceph_cluster_protocol"
FAIL = "ansible.builtin.fail"
SECRET = "REGISTRY-PASSWORD-SENTINEL"

ENTRIES = [("storage_ceph_host", entry, "bootwright.core.ceph_host", "storage_ceph_host_result") for entry in ("prepare", "observe", "destroy")]
ENTRIES += [("storage_ceph_core", entry, "bootwright.core.ceph_cluster", "storage_ceph_core_result") for entry in ("apply", "observe", "destroy")]


class Finished(Exception):
    pass


def failed(module, execute, error, monkeypatch):
    """What the module's own entry point fails with when its operation raises."""
    captured = {}

    class FakeModule:
        def __init__(self, **options):
            captured["spec"] = options["argument_spec"]
            self.params = {"request": {}, "operation": "apply", "digest": "d" * 64, "material": {}, "outputs": {}}

        def fail_json(self, **options):
            captured["failure"] = options
            raise Finished()

    def raising(*arguments, **keywords):
        raise error

    monkeypatch.setattr(module, "AnsibleModule", FakeModule)
    monkeypatch.setattr(ceph_core, "host_request", lambda value: {"cluster": value})
    monkeypatch.setattr(ceph_core, "Transport", lambda *arguments, **keywords: None)
    monkeypatch.setattr(ceph_core, execute, raising)
    with pytest.raises(Finished):
        module.main()
    assert captured["spec"]["material"]["no_log"] is True
    return captured["failure"]


def naming(role, entry, call, registered):
    """The entry point's module call, its naming of the refusal and the fail after it."""
    tasks = [task for task in LOADER.load_from_file(str(ROLES / role / "tasks" / (entry + ".yml")), trusted_as_template=True) if isinstance(task, dict)]
    index = next(i for i, task in enumerate(tasks) if call in task)
    module, named, kept = tasks[index:index + 3]
    assert module["register"] == registered and module["no_log"] is True and module["diff"] is False and module["failed_when"] is False
    assert named[PROTOCOL]["phase"] == "refused" and named["no_log"] is True
    assert FAIL in kept and kept["when"] == named["when"] and not {"ignore_errors", "failed_when"} & set(kept)
    return named


def published(named, registered, result):
    """The records the naming hands the runner over what the module registered."""
    rendering = Templar(loader=LOADER, variables={registered: result})
    if not rendering.evaluate_conditional(named["when"]):
        return []
    records = []
    outcome = adapter_protocol.publish(rendering.template(named[PROTOCOL]), protocol.CAPABILITY, lambda record, acknowledge=False: records.append(record))
    assert not outcome.get("failed"), outcome
    return records


def registered_failure(failure):
    """A failed module under failed_when false registers its failure keys, not failed."""
    return dict(failure, failed=False, changed=False)


@pytest.mark.parametrize("role, entry, call, registered", ENTRIES)
def test_every_entry_point_names_a_refusal_in_place_of_completion(role, entry, call, registered):
    named = naming(role, entry, call, registered)
    assert published(named, registered, {"changed": True, "evidence": {"ready": True}}) == []
    assert published(named, registered, {"failed": True, "msg": "MODULE FAILURE"}) == [
        {"phase": "refused", "reason": "adapter-exception", "detail": "module-failure"}]


def test_a_host_refusal_the_worker_raised_reaches_the_runner_by_its_name(monkeypatch):
    error = ceph_native.RemoteRefusal("native-refusal:image-pull@ceph_host_runtime.pull_image:410")
    failure = failed(ceph_host, "execute_host", error, monkeypatch)
    assert failure == {"msg": "Ceph host preparation refused", "reason": "image-pull", "detail": "native-refusal:image-pull@ceph_host_runtime.pull_image:410"}
    named = naming("storage_ceph_host", "prepare", "bootwright.core.ceph_host", "storage_ceph_host_result")
    assert published(named, "storage_ceph_host_result", registered_failure(failure)) == [
        {"phase": "refused", "reason": "image-pull", "detail": "native-refusal:image-pull@ceph_host_runtime.pull_image:410"}]


def test_a_cluster_refusal_reaches_the_runner_by_its_name(monkeypatch):
    failure = failed(ceph_cluster, "execute", ceph_native.Refusal("dashboard-drift"), monkeypatch)
    assert failure["reason"] == "dashboard-drift" and failure["detail"].startswith("dashboard-drift@ceph_cluster.main:")
    named = naming("storage_ceph_core", "observe", "bootwright.core.ceph_cluster", "storage_ceph_core_result")
    assert published(named, "storage_ceph_core_result", registered_failure(failure)) == [
        {"phase": "refused", "reason": "dashboard-drift", "detail": failure["detail"]}]


def test_a_pool_drift_the_worker_raised_reaches_the_runner_by_its_name(monkeypatch):
    detail = "native-refusal:pool-drift@ceph_native_resources.pool_setting:332"
    failure = failed(ceph_cluster, "execute", ceph_native.RemoteRefusal(detail), monkeypatch)
    assert failure["reason"] == "pool-drift" and failure["detail"] == detail
    named = naming("storage_ceph_core", "apply", "bootwright.core.ceph_cluster", "storage_ceph_core_result")
    assert published(named, "storage_ceph_core_result", registered_failure(failure)) == [{"phase": "refused", "reason": "pool-drift", "detail": detail}]


def test_a_token_the_block_does_not_name_reaches_the_runner_in_its_detail(monkeypatch):
    failure = failed(ceph_cluster, "execute", ceph_native.Refusal("registry-ownership"), monkeypatch)
    assert failure["reason"] == "unknown" and failure["detail"].startswith("registry-ownership@")
    named = naming("storage_ceph_core", "apply", "bootwright.core.ceph_cluster", "storage_ceph_core_result")
    assert published(named, "storage_ceph_core_result", registered_failure(failure))[0]["detail"].startswith("registry-ownership@")


def test_an_exception_names_its_type_and_site_and_never_its_text(monkeypatch):
    failure = failed(ceph_host, "execute_host", OSError(SECRET), monkeypatch)
    assert failure["reason"] == "adapter-exception" and failure["detail"].startswith("OSError@ceph_host.main:")
    assert SECRET not in repr(failure)


@pytest.mark.parametrize("detail, reasons, want", [
    ("native-refusal:image-pull@ceph_host_runtime.pull_image:410", ceph_core.HOST_REASONS, "image-pull"),
    ("native-refusal:image-pull", ceph_core.HOST_REASONS, "image-pull"),
    ("native-refusal:image-pull@ceph_host_runtime.pull_image:410", ceph_core.CLUSTER_REASONS, "remote-refusal"),
    ("native-refusal:dashboard-drift@ceph_native.dashboard:12", ceph_core.CLUSTER_REASONS, "dashboard-drift"),
    ("native-refusal:dashboard-manager@ceph_native.dashboard_redeploy:12", ceph_core.CLUSTER_REASONS, "dashboard-manager"),
    ("native-refusal:host-time@ceph_native.time:12", ceph_core.CLUSTER_REASONS, "remote-refusal"),
    ("native-refusal:call-home-unqualified@ceph_native.tunnel_bootstrap:12", ceph_core.CLUSTER_REASONS, "call-home-unqualified"),
    ("native-refusal:call-home-unqualified@ceph_native.tunnel_bootstrap:12", ceph_core.HOST_REASONS, "remote-refusal"),
    ("native-refusal:pool-drift@ceph_native_resources.pool_option:451", ceph_core.CLUSTER_REASONS, "pool-drift"),
    ("native-refusal:pool-drift@ceph_native_resources.pool_option:451", ceph_core.HOST_REASONS, "remote-refusal"),
    ("native-unknown:KeyError@ceph_native.dispatch:9", ceph_core.HOST_REASONS, "remote-refusal"),
    ("native-unknown:image-pull@ceph_native.dispatch:9", ceph_core.HOST_REASONS, "remote-refusal"),
    ("native-timeout", ceph_core.HOST_REASONS, "native-timeout"),
    ("native-refusal:tunnel-host-key-excluded@ceph_native._tunnel_host_keys:12", ceph_core.CLUSTER_REASONS, "tunnel-host-key-excluded"),
    ("native-refusal:tunnel-host-key-excluded@ceph_native._tunnel_host_keys:12", ceph_core.HOST_REASONS, "remote-refusal"),
    ("native-refusal:tunnel-host-key-excluded@ceph_native._tunnel_host_keys:12", ceph_core.SMB_REASONS, "remote-refusal"),
    (None, ceph_core.HOST_REASONS, "remote-refusal"),
])
def test_a_worker_refusal_is_named_by_its_endpoints_token_only_when_the_block_names_it(detail, reasons, want):
    assert ceph_core.refusal(ceph_native.RemoteRefusal(detail), reasons) == (want, detail)


@pytest.mark.parametrize("module, execute, role, entry, call, registered", [
    (ceph_cluster, "execute", "storage_ceph_core", "apply", "bootwright.core.ceph_cluster", "storage_ceph_core_result"),
    (ceph_host, "execute_host", "storage_ceph_host", "prepare", "bootwright.core.ceph_host", "storage_ceph_host_result"),
])
def test_an_ssh_policy_refusal_keeps_its_host_and_key_type_detail(monkeypatch, module, execute, role, entry, call, registered):
    error = ceph_ssh_policy.PolicyRefusal(ceph_ssh_policy.EXCLUDED, "node-2:ssh-ed25519")
    failure = failed(module, execute, error, monkeypatch)
    assert failure["reason"] == "ssh-policy-host-excluded" and failure["detail"] == "node-2:ssh-ed25519"
    named = naming(role, entry, call, registered)
    assert published(named, registered, registered_failure(failure)) == [
        {"phase": "refused", "reason": "ssh-policy-host-excluded", "detail": "node-2:ssh-ed25519"}]
    unavailable = ceph_ssh_policy.PolicyRefusal(ceph_ssh_policy.UNAVAILABLE)
    assert ceph_core.refusal(unavailable, ceph_core.CLUSTER_REASONS)[0] == "ssh-policy-unavailable"


def test_a_local_refusal_outside_the_block_is_unknown_with_its_token_kept():
    assert ceph_core.refusal(ceph_native.Refusal("transport"), ceph_core.HOST_REASONS) == ("transport", "transport")
    assert ceph_core.refusal(ceph_native.Refusal("dashboard-drift"), ceph_core.HOST_REASONS) == ("unknown", "dashboard-drift")
    assert ceph_core.refusal(ValueError(SECRET), ceph_core.HOST_REASONS) == ("adapter-exception", "ValueError@unknown.frame:0")


def test_a_local_call_home_unqualified_refusal_is_named_by_the_cluster_block_only():
    error = ceph_native.Refusal("call-home-unqualified")
    assert ceph_core.refusal(error, ceph_core.CLUSTER_REASONS) == ("call-home-unqualified", "call-home-unqualified")
    assert ceph_core.refusal(error, ceph_core.HOST_REASONS) == ("unknown", "call-home-unqualified")


def test_the_protocol_names_only_the_blocks_closed_reasons_and_a_value_free_detail():
    assert set(protocol.REFUSALS) == set(ceph_core.HOST_REASONS) | set(ceph_core.CLUSTER_REASONS)
    assert len(set(ceph_core.HOST_REASONS)) == len(ceph_core.HOST_REASONS) and len(set(ceph_core.CLUSTER_REASONS)) == len(ceph_core.CLUSTER_REASONS)
    records = []
    outcome = adapter_protocol.publish({"phase": "refused", "reason": SECRET}, protocol.CAPABILITY, lambda record, acknowledge=False: records.append(record))
    assert outcome["failed"] is True and records == []
    adapter_protocol.publish({"phase": "refused", "reason": "transport", "detail": "password=" + SECRET}, protocol.CAPABILITY,
                             lambda record, acknowledge=False: records.append(record))
    assert records == [{"phase": "refused", "reason": "transport"}]


def test_protocol_documentation_lists_every_refusal():
    choices = yaml.safe_load(ceph_cluster_protocol.DOCUMENTATION)["options"]["reason"]["choices"]
    assert len(set(choices)) == len(choices)
    assert set(choices) == set(protocol.REFUSALS)
