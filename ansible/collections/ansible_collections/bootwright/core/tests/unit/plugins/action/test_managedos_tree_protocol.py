"""The shared package tree names its refused source to its runner.

The apply names a source image that changed after planning, and a tree another
image published at its path, before it extracts anything
(roles/managedos_package_tree/tasks/apply.yml), and the runner reports the
diagnostic Go gave that reason for the install profile (internal/managedos/
installation, tree.go) instead of the adapter's failure. A reason Go gave none
would break the runner's protocol, so the action publishes only these.
"""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.action import managedos_tree_protocol as protocol


def run(args, monkeypatch):
    published = []
    monkeypatch.setattr(protocol, "emit", lambda message, **kwargs: published.append(message))
    module = protocol.ActionModule.__new__(protocol.ActionModule)
    module._task = SimpleNamespace(args=args)
    return module.run(task_vars={}), published


@pytest.mark.parametrize("reason", ["media-changed-tree", "tree-source-drift"])
def test_each_tree_refusal_is_named_to_the_runner(reason, monkeypatch):
    result, published = run({"phase": "refused", "reason": reason}, monkeypatch)
    assert result == {"changed": False}
    assert published == [{"phase": "refused", "reason": reason}]


@pytest.mark.parametrize("reason", [None, "", "media-changed-boot", "machine-running", "tree-source-drift "], ids=repr)
def test_a_refusal_the_tree_does_not_name_is_never_published(reason, monkeypatch):
    args = {"phase": "refused"}
    if reason is not None:
        args["reason"] = reason
    result, published = run(args, monkeypatch)
    assert result == {"failed": True, "msg": "the package tree result could not be published"}
    assert not published
