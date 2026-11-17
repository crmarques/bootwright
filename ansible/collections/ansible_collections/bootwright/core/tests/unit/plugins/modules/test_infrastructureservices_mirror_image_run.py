"""The infrastructureservices_mirror_image module's main() runs with the mirror fill stubbed, as a task would run it.

The fill's copy, verification and refusals are tested in
test_infrastructureservices_mirror_image.py and test_infrastructureservices_mirror_images.py
over a fake mirror. These cases hold the module's part: what reaches the fill,
the trust scratch it removes whatever happens, and how the fill's answer or
refusal ends the task.
"""

from __future__ import annotations

import os

import pytest

from ansible_collections.bootwright.core.plugins.modules import infrastructureservices_mirror_image as module

ARGUMENTS = {"images": [{"name": "nginx"}], "mirror": "mirror.example.test:8443", "egress": {"httpProxy": ""}, "username": "lent-user",
             "password": "lent-secret", "trust": "-----BEGIN CERTIFICATE-----\nlent\n"}


@pytest.fixture(name="fill")
def stubbed_fill(tmp_path, monkeypatch):
    """The fill answering from `answer`, recording its arguments; the module's temporary directory is the test's home."""
    monkeypatch.setenv("HOME", str(tmp_path))
    state = {"calls": [], "answer": (True, [{"name": "nginx", "digest": "sha256:" + "a" * 64}])}

    def fill(run, read, images, mirror, egress, username, password, trust, observe=False, base=None):
        state["calls"].append({"images": images, "mirror": mirror, "username": username, "observe": observe, "scratch": sorted(os.listdir(base))})
        if isinstance(state["answer"], Exception):
            raise state["answer"]  # pylint: disable=raising-bad-type
        return state["answer"]

    monkeypatch.setattr(module, "fill", fill)
    return state


def test_the_filled_images_are_returned_and_the_trust_scratch_is_removed(fill, tmp_path, run_module):
    result = run_module(module, ARGUMENTS)
    assert result["changed"] is True and result["images"] == [{"name": "nginx", "digest": "sha256:" + "a" * 64}]
    (call,) = fill["calls"]
    assert call["mirror"] == "mirror.example.test:8443" and call["observe"] is False and len(call["scratch"]) == 1
    assert "lent-secret" not in str(result)
    leftovers = [name for _directory, _names, files in os.walk(tmp_path) for name in files if name == "ca.crt"]
    assert leftovers == []


def test_an_observation_asks_the_fill_not_to_copy(fill, run_module):
    fill["answer"] = (False, [])
    result = run_module(module, dict(ARGUMENTS, observe=True))
    assert result["changed"] is False and result["images"] == [] and fill["calls"][0]["observe"] is True


def test_a_refusal_names_its_reason_and_a_value_error_its_message_and_the_scratch_is_still_removed(fill, tmp_path, run_module):
    fill["answer"] = module.Refused("mirror-unreadable", "the image mirror could not be reached")
    refused = run_module(module, ARGUMENTS)
    assert refused["failed"] is True and refused["reason"] == "mirror-unreadable" and refused["msg"] == "the image mirror could not be reached"
    fill["answer"] = ValueError("the image mirror's credential or trust anchor is missing")
    invalid = run_module(module, ARGUMENTS)
    assert invalid["failed"] is True and invalid["msg"] == "the image mirror's credential or trust anchor is missing" and "reason" not in invalid
    leftovers = [name for _directory, _names, files in os.walk(tmp_path) for name in files if name == "ca.crt"]
    assert leftovers == []
