"""The containercluster_work_area module's main() removes a temporary work area, as a task would run it."""

from __future__ import annotations

import os

from ansible_collections.bootwright.core.plugins.modules import containercluster_work_area as module


def area(tmp_path, *names):
    work = tmp_path / "sno"
    work.mkdir()
    for name in names:
        (work / name).write_text("", encoding="ascii")
    return work


def test_an_area_no_node_was_handed_the_image_in_is_removed_and_an_absent_one_changes_nothing(tmp_path, run_module):
    work = area(tmp_path, "agent.iso")
    removed = run_module(module, {"path": str(work), "refuse_inserted": True})
    assert removed["changed"] is True and not removed.get("failed")
    assert not work.exists()
    again = run_module(module, {"path": str(work), "refuse_inserted": True})
    assert again["changed"] is False and not again.get("failed")


def test_an_area_a_node_may_have_been_handed_the_image_in_is_kept_whole_when_the_rebuild_refuses_it(tmp_path, run_module):
    work = area(tmp_path, "agent.iso", ".bootwright-inserted-node-0")
    result = run_module(module, {"path": str(work), "refuse_inserted": True})
    assert result["failed"] is True and "so the area was kept and nothing was removed" in result["msg"]
    assert sorted(os.listdir(work)) == [".bootwright-inserted-node-0", "agent.iso"]


def test_a_path_that_is_not_absolute_and_normal_fails_before_anything_is_removed(tmp_path, run_module):
    work = area(tmp_path, "agent.iso")
    for path in ("sno", str(work) + "/../sno"):
        result = run_module(module, {"path": path})
        assert result["failed"] is True and result["msg"] == "the work area path must be absolute and normal"
    assert os.listdir(work) == ["agent.iso"]
