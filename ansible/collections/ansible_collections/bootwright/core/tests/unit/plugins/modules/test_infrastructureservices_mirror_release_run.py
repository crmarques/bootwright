"""The infrastructureservices_mirror_release module's main() runs with the mirror and the publisher stubbed, as a task would run it.

The release reads and the copy's preparation are tested in
test_infrastructureservices_mirror_release.py and test_infrastructureservices_mirror_release_copy.py.
These cases hold the module's part: each mode's required arguments, what reaches
the helper of its mode, and how an answer or a refusal ends the task.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.modules import infrastructureservices_mirror_release as module

READ = {"release": {"version": "4.21.0"}, "client": {"name": "oc"}, "mirror": "mirror.example.test:8443", "egress": {"noProxy": []},
        "username": "lent-user", "password": "lent-secret", "trust": "-----BEGIN CERTIFICATE-----\nlent\n"}
PREPARE = dict(READ, mode="prepare", auth="lent-auth", scratch="/run/lent/scratch")


@pytest.fixture(name="helpers")
def stubbed_helpers(tmp_path, monkeypatch):
    """The mirror's reads, the publisher fetch and each mode's helper, recording what they were handed."""
    state = {"calls": [], "held": ("sha256:" + "b" * 64, ["release", "oc"], []), "failure": None}

    def mirror_reads(username, password, trust, timeout, base):
        scratch = tmp_path / "trust"
        scratch.mkdir()
        (scratch / "ca.crt").write_text("lent", encoding="ascii")
        state["calls"].append(("reads", username, timeout))
        return "get", "head", str(scratch)

    def held(get, head, mirror, release):
        state["calls"].append(("held", get, head, mirror, release))
        if state["failure"]:
            raise state["failure"]  # pylint: disable=raising-bad-type
        return state["held"]

    monkeypatch.setenv("HOME", str(tmp_path))
    monkeypatch.setattr(module, "mirror_reads", mirror_reads)
    monkeypatch.setattr(module, "held", held)
    monkeypatch.setattr(module, "checked", lambda release, mirror: state["calls"].append(("checked", release, mirror)))
    monkeypatch.setattr(module, "publisher_fetch", lambda egress, mirror, timeout: "fetch")
    monkeypatch.setattr(module, "prepare",
                        lambda fetch, client, scratch, auth, trust: state["calls"].append(("prepare", fetch, scratch)) or {"oc": "/run/lent/oc"})
    monkeypatch.setattr(module, "clean", lambda scratch, lent="": state["calls"].append(("clean", scratch, lent)))
    return state


def test_a_read_reports_what_the_mirror_holds_and_changes_nothing_and_removes_its_trust_scratch(helpers, tmp_path, run_module):
    result = run_module(module, dict(READ, mode="read"))
    assert result["changed"] is False and result["digest"] == "sha256:" + "b" * 64
    assert result["components"] == ["release", "oc"] and result["missing"] == []
    assert ("held", "get", "head", "mirror.example.test:8443", READ["release"]) in helpers["calls"]
    assert not (tmp_path / "trust").exists() and "lent-secret" not in str(result)


def test_a_prepare_checks_the_release_then_lends_the_copys_client_and_a_clean_empties_the_run_directory(helpers, run_module):
    prepared = run_module(module, PREPARE)
    assert prepared["changed"] is True and prepared["lent"] == {"oc": "/run/lent/oc"}
    assert [call[0] for call in helpers["calls"]] == ["reads", "checked", "prepare"]
    helpers["calls"].clear()
    cleaned = run_module(module, {"mode": "clean", "scratch": "/run/lent/scratch", "lent": "/run/lent/oc"})
    assert cleaned["changed"] is True and helpers["calls"] == [("clean", "/run/lent/scratch", "/run/lent/oc")]


def test_a_refusal_names_its_reason_and_a_clean_that_cannot_empty_its_directory_fails_with_one_message(helpers, monkeypatch, run_module):
    helpers["failure"] = module.Refused("release-incomplete", "the mirror holds no such release")
    refused = run_module(module, dict(READ, mode="read"))
    assert refused["failed"] is True and refused["reason"] == "release-incomplete" and refused["msg"] == "the mirror holds no such release"

    def unsafe(scratch, lent=""):
        raise OSError("not empty")

    monkeypatch.setattr(module, "clean", unsafe)
    failed = run_module(module, {"mode": "clean", "scratch": "/run/lent/scratch"})
    assert failed["failed"] is True and failed["msg"] == "the release copy's run directory could not be safely emptied"


def test_a_mode_without_its_required_arguments_fails_before_any_helper_runs(helpers, run_module):
    result = run_module(module, {"mode": "prepare", "release": READ["release"]})
    assert result["failed"] is True and "missing" in result["msg"]
    assert helpers["calls"] == []
