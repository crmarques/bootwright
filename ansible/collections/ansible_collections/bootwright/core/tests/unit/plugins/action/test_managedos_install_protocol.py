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


# A store entry the attempt finds changed since the plan froze it is named by
# which image it is, each with the diagnostic Go gave it (selection.go).
@pytest.mark.parametrize("reason", ["media-changed-boot", "media-changed-tree"])
def test_each_changed_store_entry_is_named_to_the_runner(reason, monkeypatch):
    result, published = run({"phase": "refused", "reason": reason}, monkeypatch)
    assert result == {"changed": False}
    assert published == [{"phase": "refused", "reason": reason}]


DIGEST = "1" * 64
IDENTITY = "fedcba9876543210" * 4


def test_presence_carries_the_tree_identity_the_inspection_read():
    for observed, carried in ((IDENTITY, IDENTITY), ("", ""), (None, "")):
        evidence = protocol.presence({"power": "On", "observation": {"tree": True, "treeIdentity": observed}}, DIGEST)
        assert evidence["treeIdentity"] == carried


@pytest.mark.parametrize("identity", ["abc", IDENTITY.upper(), IDENTITY + "0", 7], ids=repr)
def test_a_malformed_tree_identity_is_never_published(identity):
    with pytest.raises(ValueError, match="tree identity"):
        protocol.presence({"power": "On", "observation": {"treeIdentity": identity}}, DIGEST)


def test_a_removal_reports_no_tree_identity():
    evidence = protocol.absence({"observation": {"treeIdentity": IDENTITY}}, DIGEST)
    assert evidence["treeIdentity"] == ""
