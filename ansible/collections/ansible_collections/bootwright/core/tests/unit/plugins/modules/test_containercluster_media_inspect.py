"""The media inspection reports the mark an installation leaves in its work area.

A cluster's media and install blocks share one installer work area, and a
media build starts by discarding it. The installation marks the area before
any node is handed the image and records each insert; these cases pin that
the inspection reports the mark, whatever sits at its name, and whether a node
may have been handed the image by the installation's own rule, and changes
nothing. The role rules built on
that report live in tests/unit/test_containercluster_media_rebuild.py, because
rendering task files needs Ansible's controller, which ansible-test does not
offer to unit tests under tests/unit/plugins/modules.
"""

from __future__ import annotations

import json
import pathlib

import pytest
from ansible.module_utils.testing import patch_module_args

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_work_area as rule
from ansible_collections.bootwright.core.plugins.modules import containercluster_media_inspect as inspect

DIGEST = "a" * 64
RELEASE = "4.21.15"
URL = "https://192.0.2.1:8443/private/clusters/sno"
# What `openssl rand -hex 32` mints: 64 hexadecimal digits.
TOKEN = "0123456789abcdef" * 4


def request(tmp_path):
    """The frozen media request's fields the inspection and these tasks read."""
    return {
        "deliveryKind": "fetch",
        "identity": {"block": "cluster-media-sno", "cluster": "sno", "context": "lab"},
        "image": {"path": str(tmp_path / "private"), "url": URL},
        "release": {"version": RELEASE},
        "workRoot": str(tmp_path / "work"),
    }


def publish(root, name=TOKEN):
    directory = pathlib.Path(root) / name
    directory.mkdir(parents=True)
    (directory / inspect.IMAGE).write_bytes(b"")
    return directory / inspect.IMAGE


def record(work, inputs=DIGEST, installer=RELEASE):
    work = pathlib.Path(work)
    work.mkdir(parents=True, exist_ok=True)
    (work / inspect.RECEIPT).write_text(json.dumps({"inputs": inputs, "installer": installer}))


def built(tmp_path):
    """A work area and image exactly as a completed build leaves them."""
    frozen = request(tmp_path)
    record(frozen["workRoot"])
    publish(frozen["image"]["path"])
    return frozen


def test_an_area_the_installation_never_marked_is_not_booted(tmp_path):
    assert inspect.observe(built(tmp_path)) == {
        "booted": False, "image": True, "inserted": False, "inputs": DIGEST, "installer": RELEASE, "work": True}


def test_the_mark_reports_the_area_booted(tmp_path):
    frozen = built(tmp_path)
    (pathlib.Path(frozen["workRoot"]) / inspect.MARKER).write_bytes(b"")
    assert inspect.observe(frozen) == {
        "booted": True, "image": True, "inserted": True, "inputs": DIGEST, "installer": RELEASE, "work": True}


# The rebuild's refusal reads the installation's own rule: this build's mark
# with no insert record beside it is an attempt whose every node refused its
# pre-boot proof, and anything else at those names may hide an insert.
def vouched(work):
    (work / rule.MARKER).write_bytes(rule.VOUCH)


def symlinked(work):
    (work / "vouch").write_bytes(rule.VOUCH)
    (work / rule.MARKER).symlink_to(work / "vouch")


INSERTS = {
    "this build's mark alone": ([vouched], False),
    "this build's mark and an insert record": (
        [vouched, lambda work: (work / (rule.INSERTED + "0")).write_text("sno-01\n")], True),
    "this build's mark and a record's stage": (
        [vouched, lambda work: (work / ("." + rule.INSERTED + "0.0123456789abcdef")).write_text("")], True),
    "an insert record and no mark": ([lambda work: (work / (rule.INSERTED + "1")).write_text("sno-02\n")], True),
    "a mark another build wrote": ([lambda work: (work / rule.MARKER).write_bytes(b"other\n")], True),
    "a symlinked mark": ([symlinked], True),
}


@pytest.mark.parametrize("steps, handed", INSERTS.values(), ids=INSERTS.keys())
def test_the_observation_reports_a_node_that_may_have_been_handed_the_image(tmp_path, steps, handed):
    frozen = built(tmp_path)
    work = pathlib.Path(frozen["workRoot"])
    for step in steps:
        step(work)
    assert inspect.observe(frozen)["inserted"] is handed


def test_an_area_that_cannot_be_listed_reads_as_a_node_handed_the_image(tmp_path, monkeypatch):
    frozen = built(tmp_path)

    def refused(path):
        raise PermissionError(13, "denied")

    monkeypatch.setattr(rule.os, "listdir", refused)
    assert inspect.observe(frozen)["inserted"] is True


@pytest.mark.parametrize("make", [
    lambda path: path.mkdir(),
    lambda path: path.symlink_to(path.parent / "nowhere"),
], ids=["a directory", "a dangling link"])
def test_anything_at_the_marks_name_marks_the_area(tmp_path, make):
    frozen = built(tmp_path)
    make(pathlib.Path(frozen["workRoot"]) / inspect.MARKER)
    assert inspect.observe(frozen)["booted"] is True


def test_no_work_area_is_neither_present_nor_booted(tmp_path):
    assert inspect.observe(request(tmp_path)) == {
        "booted": False, "image": False, "inserted": False, "inputs": "", "installer": "", "work": False}


def test_the_module_reports_the_observation_and_changes_nothing(tmp_path, capsys):
    frozen = built(tmp_path)
    (pathlib.Path(frozen["workRoot"]) / inspect.MARKER).write_bytes(b"")
    result = run(inspect, {"request": frozen}, capsys)
    assert result["changed"] is False
    assert result["observation"] == inspect.observe(frozen)


def run(module, arguments, capsys):
    """One module run in process, as a task would make it, and what it returned."""
    capsys.readouterr()
    with patch_module_args(arguments), pytest.raises(SystemExit):
        module.main()
    return json.loads(capsys.readouterr().out)


def test_receipt_reads_forty_volume_proofs_with_maximal_ids(tmp_path):
    frozen = built(tmp_path)
    roots = {"node-%02d" % i: {"storageId": "S" * 128, "volumeId": "V" * 128,
                               "wwn": "0x" + "5" * 32, "sizeBytes": 64 << 40} for i in range(40)}
    content = json.dumps({"inputs": DIGEST, "installer": RELEASE, "rootVolumes": roots})
    assert 4096 < len(content) < inspect.MAX_RECEIPT
    (pathlib.Path(frozen["workRoot"]) / inspect.RECEIPT).write_text(content)
    assert inspect.observe(frozen)["inputs"] == DIGEST
