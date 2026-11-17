"""The hosted_area module's main() runs over a temporary root, as a task would run it."""

from __future__ import annotations

import functools
import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import hosted_area as area
from ansible_collections.bootwright.core.plugins.modules import hosted_area as module


@pytest.fixture(name="root")
def temporary_root(tmp_path, monkeypatch):
    """The module's helpers, bound to a root the test owns in place of /var/lib/bootwright-hosted."""
    (tmp_path / "lib").mkdir()
    root = str(tmp_path / "lib" / "bootwright-hosted")
    target = area.target
    monkeypatch.setattr(area, "target", lambda context, path, *unused: target(context, path, root))
    for name in ("ensure", "prove", "remove"):
        monkeypatch.setattr(area, name, functools.partial(getattr(area, name), root=root, owner=os.getuid()))
    return root


def fields(result):
    return result["changed"], result["path"], result["exists"]


def test_a_present_directory_is_created_proved_and_removed(root, run_module):
    arguments = {"context": "lab", "path": ["services", "dns"]}
    target = root + "/lab/services/dns"
    assert fields(run_module(module, arguments)) == (True, target, True)
    assert fields(run_module(module, dict(arguments, state="present"))) == (False, target, True)
    assert fields(run_module(module, dict(arguments, state="proved"))) == (False, target, True)
    assert fields(run_module(module, dict(arguments, state="absent"))) == (True, target, False)
    assert not os.path.exists(target)


def test_a_directory_outside_the_hosted_areas_fails_the_task_and_creates_nothing(root, run_module):
    result = run_module(module, {"context": "lab", "path": ["etc", "dns"]})
    assert result["failed"] is True and "normal form" in result["msg"]
    assert not os.path.exists(root)
