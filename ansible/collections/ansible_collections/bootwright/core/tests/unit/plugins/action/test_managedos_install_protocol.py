"""A managed-OS installation names its target's refused pre-boot proof to its runner.

The apply's rescue names the refusal the substrate's proof left (roles/
managedos_install_anaconda/tasks/apply.yml), and the runner reports the
diagnostic Go gave that reason for the Machine (internal/substrate, preboot.go)
instead of the adapter's failure. A reason Go gave none would break the
runner's protocol, so the action publishes only the ones the proof names.
"""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.action import managedos_install_protocol as protocol


def run(args, monkeypatch):
    published = []
    monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: published.append(message))
    module = protocol.ActionModule.__new__(protocol.ActionModule)
    module._task = SimpleNamespace(args=args)
    return module.run(task_vars={}), published


@pytest.mark.parametrize("reason", ["hardware-mismatch", "identity-mismatch", "machine-running"])
def test_each_pre_boot_refusal_is_named_to_the_runner(reason, monkeypatch):
    result, published = run({"phase": "refused", "reason": reason}, monkeypatch)
    assert result == {"changed": False}
    assert published == [{"phase": "refused", "reason": reason}]


@pytest.mark.parametrize("reason", [None, "", "release-stamp", "machine-running-node-0", "machine-running "], ids=repr)
def test_a_refusal_the_installation_does_not_name_is_never_published(reason, monkeypatch):
    args = {"phase": "refused"}
    if reason is not None:
        args["reason"] = reason
    result, published = run(args, monkeypatch)
    assert result == {"failed": True, "msg": "the installation capability result could not be published"}
    assert not published
