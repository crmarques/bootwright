"""The private_file module's main() writes under a temporary directory, as a task would run it."""

from __future__ import annotations

import functools
import os
import stat

import pytest

from ansible_collections.bootwright.core.plugins.modules import private_file as module


@pytest.fixture(autouse=True)
def own_account(monkeypatch):
    """The module's write, owned by the account running the test in place of root."""
    monkeypatch.setattr(module, "write", functools.partial(module.write, owner=os.getuid(), group=os.getgid()))


def test_content_is_published_privately_and_a_repeat_changes_nothing(tmp_path, run_module):
    path = str(tmp_path / "install-config.yaml")
    written = run_module(module, {"path": path, "content": "pullSecret: lent\n", "mode": "0640"})
    assert written["changed"] is True and written["path"] == path
    assert "lent" not in str(written)
    assert open(path, encoding="utf-8").read() == "pullSecret: lent\n"
    assert stat.S_IMODE(os.stat(path).st_mode) == 0o640
    repeated = run_module(module, {"path": path, "content": "pullSecret: lent\n", "mode": "0640"})
    assert repeated["changed"] is False and repeated["path"] == path
    assert sorted(os.listdir(tmp_path)) == ["install-config.yaml"]


def test_a_relative_path_and_a_symbolic_link_fail_the_task_and_write_nothing(tmp_path, run_module):
    relative = run_module(module, {"path": "install-config.yaml", "content": "x"})
    assert relative["failed"] is True and "normalized absolute path" in relative["msg"]
    target = tmp_path / "elsewhere"
    target.write_text("kept", encoding="utf-8")
    link = tmp_path / "linked"
    link.symlink_to(target)
    refused = run_module(module, {"path": str(link), "content": "replaced"})
    assert refused["failed"] is True and "symbolic link" in refused["msg"]
    assert target.read_text(encoding="utf-8") == "kept"
    assert sorted(os.listdir(tmp_path)) == ["elsewhere", "linked"]
