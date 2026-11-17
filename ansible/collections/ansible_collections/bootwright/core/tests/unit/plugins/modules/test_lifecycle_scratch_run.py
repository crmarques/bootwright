"""The lifecycle_scratch module's main() runs with its scratch manager recorded, as a task would run it.

The manager's own rules are tested in test_lifecycle_scratch.py, over temporary
parents; the real parent is root's under /var/lib, which a test never touches.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.modules import lifecycle_scratch as module

ROOT = "/var/lib/bootwright-staging/runs/bootwright-scratch-1-2"


@pytest.fixture(name="calls")
def recorded_manager(monkeypatch):
    calls = []

    def manage(root, scope, state, remove_root=False):
        calls.append((root, scope, state, remove_root))
        return ROOT + "/" + scope

    monkeypatch.setattr(module, "manage", manage)
    return calls


def test_the_task_arguments_reach_the_manager_and_its_directory_is_returned(calls, run_module):
    present = run_module(module, {"root": ROOT, "scope": "libvirt-host"})
    assert present["path"] == ROOT + "/libvirt-host" and present["changed"] is False
    absent = run_module(module, {"root": ROOT, "scope": "mirror-release", "state": "absent", "remove_root": True})
    assert absent["path"] == ROOT + "/mirror-release"
    assert calls == [(ROOT, "libvirt-host", "present", False), (ROOT, "mirror-release", "absent", True)]


def test_a_refusal_and_an_operating_system_error_fail_the_task_with_their_fixed_messages(monkeypatch, run_module):
    def refuse(*arguments, **keywords):
        raise ValueError("scratch is not a runner-issued role directory")

    monkeypatch.setattr(module, "manage", refuse)
    refused = run_module(module, {"root": "/tmp/elsewhere", "scope": "libvirt-host"})
    assert refused["failed"] is True and refused["msg"] == "scratch is not a runner-issued role directory"

    def broken(*arguments, **keywords):
        raise PermissionError("/var/lib/bootwright-staging")

    monkeypatch.setattr(module, "manage", broken)
    failed = run_module(module, {"root": ROOT, "scope": "libvirt-host"})
    assert failed["failed"] is True and failed["msg"] == "private lifecycle scratch could not be safely prepared or removed"
    assert "bootwright-staging" not in failed["msg"]


def test_a_scope_the_module_does_not_own_is_refused_before_the_manager_runs(calls, run_module):
    result = run_module(module, {"root": ROOT, "scope": "other-role"})
    assert result["failed"] is True and "scope" in result["msg"]
    assert calls == []
