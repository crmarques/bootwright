"""Discovery progress uses the inherited channel without exposing native data."""

# Exact native types are the regression: isinstance also accepts Ansible tags.
# pylint: disable=unidiomatic-typecheck

from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from ansible.plugins.action import ActionBase
from ansible.template import Templar, trust_as_template
from ansible_collections.bootwright.core.plugins.action import managedos_disk_discovery as action
from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_media as media
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_runtime as runtime
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_progress
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER


@pytest.mark.parametrize("operation", ["apply", "observe", "destroy"])
def test_action_converts_templated_arguments_to_native_json_types(monkeypatch, operation):
    arguments = {
        "operation": operation, "digest": "a" * 64,
        "request": {"bootMedia": {"size": 1 << 30}, "interfaces": [{"name": "install", "enabled": True}]},
        "material": {"bindingBase64": "PRIVATE", "optional": None},
    }
    loader = LOADER
    tagged = loader.load(canonical(arguments).decode("ascii"))
    templated = Templar(loader=loader, variables={"frozen": tagged}).template(trust_as_template("{{ frozen }}"))
    assert isinstance(templated["request"]["bootMedia"]["size"], int)
    assert type(templated["request"]["bootMedia"]["size"]) is not int
    with pytest.raises(ValueError, match="discovery source size bound"):
        media.publication_bound(templated["request"]["bootMedia"]["size"])
    monkeypatch.setattr(ActionBase, "run", lambda *args, **kwargs: {})

    def execute(native, report):
        assert native == arguments and canonical(native) == canonical(arguments)
        assert type(native) is dict and type(native["operation"]) is str
        assert type(native["request"]) is dict and type(native["request"]["interfaces"]) is list
        assert type(native["request"]["interfaces"][0]["enabled"]) is bool
        assert media.publication_bound(native["request"]["bootMedia"]["size"]) > 1 << 30
        return {"changed": operation != "observe", "evidence": {"completed": True}}

    monkeypatch.setattr(action, "execute", execute)
    plugin = action.ActionModule.__new__(action.ActionModule)
    plugin._task = SimpleNamespace(args=templated)
    result = plugin.run(task_vars={})
    assert result == {"changed": operation != "observe", "evidence": {"completed": True}, "_ansible_no_log": True}
    assert canonical(templated) == canonical(arguments)


@pytest.mark.parametrize("size", [True, False, None, "1073741824", 1.0, 0, -1, media.MAX_SOURCE + 1])
def test_action_normalization_preserves_invalid_source_sizes(monkeypatch, size):
    arguments = {"operation": "apply", "digest": "a" * 64, "request": {"bootMedia": {"size": size}}, "material": {}}
    loader = LOADER
    tagged = loader.load(canonical(arguments).decode("ascii"))
    monkeypatch.setattr(ActionBase, "run", lambda *args, **kwargs: {})

    def execute(native, report):
        actual = native["request"]["bootMedia"]["size"]
        assert type(actual) is type(size) and actual == size
        with pytest.raises(ValueError, match="discovery source size bound"):
            media.publication_bound(actual)
        return {"failed": True, "discovery_failure": "source-size"}

    monkeypatch.setattr(action, "execute", execute)
    plugin = action.ActionModule.__new__(action.ActionModule)
    plugin._task = SimpleNamespace(args=tagged)
    result = plugin.run(task_vars={})
    assert result == {"changed": False, "failed": True, "discovery_failure": "source-size", "_ansible_no_log": True}


def test_action_refuses_non_json_material_before_native_execution(monkeypatch):
    monkeypatch.setattr(ActionBase, "run", lambda *args, **kwargs: {})
    execute = Mock()
    monkeypatch.setattr(action, "execute", execute)
    plugin = action.ActionModule.__new__(action.ActionModule)
    plugin._task = SimpleNamespace(args={"operation": "apply", "request": {}, "digest": "a" * 64, "material": {"SECRET": object()}})
    result = plugin.run(task_vars={})
    assert result["failed"] and result["_ansible_no_log"]
    assert "SECRET" not in repr(result) and "exception" not in result
    execute.assert_not_called()


def test_action_publishes_progress_during_the_call_not_after_its_result(monkeypatch):
    records = []
    monkeypatch.setattr(ActionBase, "run", lambda *args, **kwargs: {})
    monkeypatch.setattr(action.protocol, "emit", lambda record, acknowledge=False: records.append(record))

    def execute(arguments, report):
        report("build-media", "running")
        assert records == [{"phase": "group", "group": "build-media", "status": "running"}]
        report("build-media", "ok")
        return {"changed": True, "evidence": {"completed": True}}

    monkeypatch.setattr(action, "execute", execute)
    plugin = action.ActionModule.__new__(action.ActionModule)
    plugin._task = SimpleNamespace(args={"operation": "apply", "request": {}, "digest": "a" * 64, "material": {"password": "SECRET"}})
    plugin._execute_module = Mock(side_effect=AssertionError("progress was hidden inside another module"))
    result = plugin.run(task_vars={})
    assert result["evidence"] == {"completed": True} and result["_ansible_no_log"] is True
    assert records[-1] == {"phase": "group", "group": "build-media", "status": "ok"}
    assert "SECRET" not in repr(records) + repr(result)
    plugin._execute_module.assert_not_called()
    assert plugin._requires_connection is False and plugin._supports_check_mode is False and plugin._supports_async is False


@pytest.mark.parametrize("group,status", [("SECRET", "running"), ("build-media", "SECRET"), ([], "ok"), ("inspect-target", {})])
def test_progress_rejects_untrusted_strings_before_the_channel(monkeypatch, group, status):
    emit = Mock()
    monkeypatch.setattr(action.protocol, "emit", emit)
    with pytest.raises(ValueError):
        action.report(group, status)
    emit.assert_not_called()


def test_action_returns_no_trace_or_exception_text(monkeypatch):
    monkeypatch.setattr(ActionBase, "run", lambda *args, **kwargs: {})
    monkeypatch.setattr(action, "execute", Mock(side_effect=RuntimeError("SECRET private URL")))
    plugin = action.ActionModule.__new__(action.ActionModule)
    plugin._task = SimpleNamespace(args={"operation": "apply", "request": {}, "digest": "a" * 64, "material": {}})
    result = plugin.run(task_vars={})
    assert result["failed"] and result["_ansible_no_log"]
    assert "SECRET" not in repr(result) and "exception" not in result


@pytest.mark.parametrize("change", [{"operation": "other"}, {"extra": "SECRET"}, {"material": "SECRET"}, {"request": None}])
def test_runtime_refuses_the_outer_arguments_before_custody(monkeypatch, change):
    constructor = Mock(side_effect=AssertionError("invalid input reached custody"))
    monkeypatch.setattr(runtime.custody, "Controller", constructor)
    result = runtime.execute({"operation": "apply", "request": {}, "digest": "a" * 64, "material": {}, **change})
    assert result["failed"] and result["discovery_failure"] == "request"
    assert "SECRET" not in repr(result)
    constructor.assert_not_called()


def test_observation_keeps_its_read_only_dispatch_and_closes_custody(monkeypatch):
    instance = Mock()
    instance.evidence.return_value = {"completed": False, "cleaned": False}
    records = []

    def report(group, status):
        records.append((group, status))

    instance.progress.side_effect = lambda group: disk_discovery_progress.step(report, group)
    constructor = Mock(return_value=instance)
    monkeypatch.setattr(runtime.custody, "Controller", constructor)
    arguments = {"operation": "observe", "request": {}, "digest": "a" * 64, "material": {}}
    result = runtime.execute(arguments, report)
    assert result == {"changed": False, "evidence": {"completed": False, "cleaned": False}}
    assert constructor.call_args.kwargs == {"create": False, "progress": report}
    assert records == [("inspect-target", "running"), ("inspect-target", "ok")]
    instance.apply.assert_not_called()
    instance.destroy.assert_not_called()
    instance.close.assert_called_once()


@pytest.mark.parametrize("cause", disk_discovery_progress.RETRIES)
def test_retry_notices_use_the_same_value_free_progress_channel(monkeypatch, cause):
    records = []
    monkeypatch.setattr(action.protocol, "emit", records.append)
    action.report(cause, "failed")
    assert records == [{"phase": "group", "group": cause, "status": "failed"}]
    assert cause not in disk_discovery_progress.GROUPS
