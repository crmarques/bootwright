"""Every action plugin's run() is driven here with what surrounds it stubbed.

Each plugin's helpers have tests of their own, but run() is what ansible-core
calls: it reads the task's arguments, reaches the runner's channel, and turns
every refusal into a failed result rather than an exception. A helper renamed,
or an argument read by the wrong key, passes every helper test and shows up
only on a host. Each plugin is run with the one record every entry point hands
the runner first, or, for a plugin that hands none, with arguments it refuses,
and a plugin the table does not name fails, so a new one arrives with a stub
run of its own.
"""

from __future__ import annotations

import importlib
import pathlib
from types import SimpleNamespace

import pytest
from ansible.plugins.action import ActionBase

ACTIONS = pathlib.Path(__file__).resolve().parents[4] / "plugins" / "action"
PACKAGE = "ansible_collections.bootwright.core.plugins.action."

# The plugins that hand the runner its protocol records, each run with the
# loaded record every entry point hands first.
PROTOCOLS = {
    "artifact_server_protocol", "containercluster_install_protocol", "containercluster_media_protocol",
    "controller_protocol", "infra_service_protocol", "machine_power_protocol", "managedos_install_protocol",
    "substrate_host_protocol", "substrate_machine_protocol", "substrate_physical_protocol",
}

# The plugins that act on the controller, each with the helper that would act,
# run with arguments they refuse before reaching it.
EFFECTS = {
    "controller_inventory": "inspection",
    "controller_packages": "stage_payloads",
    "controller_tool": "prepare_tool",
}


def plugin_names():
    return sorted(path.stem for path in ACTIONS.glob("*.py") if path.stem != "__init__")


def running(name, arguments, monkeypatch):
    """One run of a plugin's ActionModule with the channel recorded and the
    base class's own run, which needs a live task executor, stubbed."""
    module = importlib.import_module(PACKAGE + name)
    records = []
    if hasattr(module, "emit"):
        monkeypatch.setattr(module, "emit", lambda record, acknowledge=False: records.append((record, acknowledge)))
    monkeypatch.setattr(ActionBase, "run", lambda self, tmp=None, task_vars=None: {})
    action = module.ActionModule.__new__(module.ActionModule)
    action._task = SimpleNamespace(args=arguments)
    return module, action.run(task_vars={}), records


def test_every_plugin_has_a_stub_run():
    assert set(plugin_names()) == PROTOCOLS | set(EFFECTS)


@pytest.mark.parametrize("name", sorted(PROTOCOLS))
def test_a_protocol_plugin_hands_the_runner_loaded_and_waits_for_its_acknowledgement(name, monkeypatch):
    _module, result, records = running(name, {"phase": "loaded"}, monkeypatch)
    assert not result.get("failed"), result
    assert result.get("changed") is False
    assert records == [({"phase": "loaded"}, True)]


@pytest.mark.parametrize("name", sorted(PROTOCOLS))
def test_a_protocol_plugin_refuses_a_phase_it_does_not_know_and_hands_nothing(name, monkeypatch):
    _module, result, records = running(name, {"phase": "unloaded"}, monkeypatch)
    assert result.get("failed") is True and result.get("msg"), result
    assert records == []


@pytest.mark.parametrize("name", sorted(EFFECTS))
def test_an_effect_plugin_refuses_arguments_it_does_not_know_before_it_acts(name, monkeypatch):
    module = importlib.import_module(PACKAGE + name)
    acted = []
    monkeypatch.setattr(module, EFFECTS[name], lambda *arguments, **keywords: acted.append(arguments))
    _module, result, records = running(name, {"unknown": True}, monkeypatch)
    assert result.get("failed") is True and result.get("msg"), result
    assert result.get("_ansible_no_log") is True
    assert acted == [] and records == []
