"""Image failures retain their redacted diagnostic in the actual role's output."""

from __future__ import annotations

import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

from ansible_collections.bootwright.core.plugins.module_utils import ceph_registry_discovery as discovery
from ansible_collections.bootwright.core.plugins.modules import ceph_image_resolve

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "storage_ceph_image" / "tasks" / "resolve.yml"
SECRET = "fixture-entitlement-password"


def failed(monkeypatch, error):
    captured = {}

    class Finished(Exception):
        pass

    class Module:
        def __init__(self, **options):
            captured["spec"] = options["argument_spec"]
            self.params = {"operation": "apply", "request": {}, "digest": "", "material": {}}

        def fail_json(self, **values):
            captured["result"] = dict(values, failed=False)
            raise Finished()

    def refuse(*_args, **_kwargs):
        raise error

    monkeypatch.setattr(ceph_image_resolve, "AnsibleModule", Module)
    monkeypatch.setattr(discovery, "execute", refuse)
    with pytest.raises(Finished):
        ceph_image_resolve.main()
    assert captured["spec"]["material"]["no_log"] is True
    return captured["result"]


def rendered_failure(result):
    loader = DataLoader()
    tasks = loader.load_from_file(str(ROLE), trusted_as_template=True)
    call = next(task for task in tasks if "bootwright.core.ceph_image_resolve" in task)
    protocol = next(task for task in tasks if task.get("bootwright.core.ceph_image_protocol", {}).get("phase") == "refused")
    fail = next(task for task in tasks if "ansible.builtin.fail" in task)
    assert call["no_log"] is True and call["diff"] is False and call["failed_when"] is False
    assert protocol["no_log"] is True and fail["when"] == protocol["when"]
    templar = Templar(loader=loader, variables={call["register"]: result})
    assert templar.evaluate_conditional(fail["when"])
    return templar.template(fail["ansible.builtin.fail"])["msg"]


def test_registry_http_refusal_reaches_the_saved_log_without_credentials(monkeypatch):
    authorization = "Basic Zml4dHVyZS11c2VyOmZpeHR1cmUtZW50aXRsZW1lbnQtcGFzc3dvcmQ="
    with pytest.raises(discovery.DiscoveryRefusal) as caught:
        discovery.http_success(403, ('{"message":"denied", "password":"' + SECRET + '"}').encode(),
                               "registry.example.test/team/ceph:9.0.0", (authorization,))
    result = failed(monkeypatch, caught.value)
    message = rendered_failure(result)
    assert "HTTP 403" in message and "registry.example.test/team/ceph:9.0.0" in message
    assert SECRET not in message and authorization not in message and "[redacted]" in message
    assert result["reason"] == "discovery" and result["nativeError"] == caught.value.native_error


@pytest.mark.parametrize("reason", discovery.REASONS)
def test_closed_refusal_reason_is_visible_without_exception_values(monkeypatch, reason):
    message = rendered_failure(failed(monkeypatch, discovery.DiscoveryRefusal(reason)))
    assert reason in message and len(message) < 512


def test_arbitrary_module_exception_and_output_remain_private(monkeypatch):
    error = RuntimeError(SECRET)
    error.native_error = {"coordinate": SECRET, "cause": SECRET}
    message = rendered_failure(failed(monkeypatch, error))
    assert "adapter-exception" in message and "RuntimeError@" in message and SECRET not in message
    fallback = rendered_failure({"failed": True, "msg": SECRET, "stderr": SECRET})
    assert SECRET not in fallback and "bounded diagnostic" in fallback
