"""A cluster's work area is removed with what says a node was handed its image last.

The media block's removal and its rebuild's discard both go through
containercluster_work_area.remove. These cases stop that removal before each
step it takes and read the area as the next resolution or rebuild would: an
area that held an insert record, or a mark this build did not write, keeps
reading as one a node was handed the image in until nothing of it is left.
"""

from __future__ import annotations

import os
import shutil

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_work_area as rule


class Crash(BaseException):
    """The host stopped before the next removal."""


def area(path, mark=rule.VOUCH, records=(0,), stage=False):
    """A work area as an apply whose nodes were handed the image leaves it."""
    path.mkdir()
    (path / "auth").mkdir()
    (path / "auth" / "kubeconfig").write_text("kubeconfig\n")
    (path / ".bootwright-kubeconfig").write_text("kubeconfig\n")
    (path / "bootwright-media.json").write_text("{}\n")
    (path / ".openshift_install_state.json").write_text("{}\n")
    if mark is not None:
        (path / rule.MARKER).write_bytes(mark)
    for index in records:
        (path / (rule.INSERTED + str(index))).write_text("sno-0%d\n" % index)
    if stage:
        (path / ("." + rule.INSERTED + "9.0123456789abcdef")).write_text("")
    return path


def stopped(monkeypatch, budget):
    """Let budget removals happen, then stop the host before the next one."""
    left = [budget]

    def guarded(real):
        def removal(*arguments, **keywords):
            if left[0] == 0:
                raise Crash()
            left[0] -= 1
            return real(*arguments, **keywords)
        return removal

    for owner, name in ((os, "unlink"), (os, "rmdir"), (shutil, "rmtree")):
        monkeypatch.setattr(owner, name, guarded(getattr(owner, name)))


LAYOUTS = {
    "this build's mark and two records": {"records": (0, 1)},
    "this build's mark and a record's stage": {"records": (), "stage": True},
    "a mark another build wrote and no record": {"mark": b"", "records": ()},
    "a mark another build wrote and a record": {"mark": b"other\n", "records": (2,)},
    "a record and no mark": {"mark": None, "records": (0,)},
}


@pytest.mark.parametrize("layout", LAYOUTS.values(), ids=LAYOUTS.keys())
def test_a_removal_stopped_at_any_step_never_reads_as_never_handed_the_image(tmp_path, monkeypatch, layout):
    budget = 0
    while True:
        work = area(tmp_path / ("work-%d" % budget), **layout)
        assert rule.inserted(str(work)) is True
        with monkeypatch.context() as patched:
            stopped(patched, budget)
            try:
                finished = rule.remove(str(work))
            except Crash:
                finished = False
        if finished:
            assert not os.path.lexists(work)
            break
        assert not os.path.lexists(work) or not os.listdir(work) or rule.inserted(str(work)) is True, budget
        budget += 1
    assert budget > 3


def test_the_mark_this_build_wrote_leaves_before_the_records_and_the_rest_before_both(tmp_path, monkeypatch):
    work = area(tmp_path / "work", records=(0,))
    removed = []
    real = os.unlink

    def recording(path, *arguments, **keywords):
        removed.append(os.path.basename(path))
        return real(path, *arguments, **keywords)

    monkeypatch.setattr(os, "unlink", recording)
    assert rule.remove(str(work)) is True
    top = [name for name in removed if name in {rule.MARKER, rule.INSERTED + "0", "bootwright-media.json"}]
    assert top == ["bootwright-media.json", rule.MARKER, rule.INSERTED + "0"]


def test_a_marked_area_no_node_was_handed_the_image_in_is_removed_for_a_rebuild(tmp_path):
    work = area(tmp_path / "work", records=())
    assert rule.inserted(str(work)) is False
    assert rule.remove(str(work), refuse_inserted=True) is True
    assert not os.path.lexists(work)


@pytest.mark.parametrize("layout", LAYOUTS.values(), ids=LAYOUTS.keys())
def test_a_rebuild_keeps_an_area_a_node_may_have_been_handed_the_image_in_whole(tmp_path, layout):
    work = area(tmp_path / "work", **layout)
    before = sorted(os.listdir(work))
    with pytest.raises(rule.InsertedError):
        rule.remove(str(work), refuse_inserted=True)
    assert sorted(os.listdir(work)) == before


def test_a_symlinked_mark_is_removed_last_and_its_target_kept(tmp_path):
    target = tmp_path / "vouch"
    target.write_bytes(rule.VOUCH)
    work = area(tmp_path / "work", mark=None, records=())
    (work / rule.MARKER).symlink_to(target)
    assert rule.inserted(str(work)) is True
    with pytest.raises(rule.InsertedError):
        rule.remove(str(work), refuse_inserted=True)
    assert rule.remove(str(work)) is True
    assert not os.path.lexists(work) and target.read_bytes() == rule.VOUCH


def test_an_absent_area_is_nothing_to_remove(tmp_path):
    assert rule.remove(str(tmp_path / "absent"), refuse_inserted=True) is False
