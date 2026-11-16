"""Discovery progress uses the inherited channel without exposing native data."""

from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.action import managedos_disk_discovery as action
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_runtime as runtime
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_progress


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
