"""The image_acquire module's main() runs with podman stubbed, as a task would run it."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.modules import image_acquire as module

IMAGE = "registry.example.test/nginx@sha256:" + "a" * 64
EGRESS = {"httpProxy": "", "httpsProxy": "http://proxy.example.test:3128", "noProxy": ["10.0.0.0/8"]}


@pytest.fixture(name="podman")
def stubbed_podman(monkeypatch, tmp_path):
    """podman as an executable that holds the image or not and pulls or fails; every call is recorded.

    The module's temporary directory lies under the home it runs with, which is the test's.
    """
    monkeypatch.setenv("HOME", str(tmp_path))
    state = {"held": 1, "pull": (0, "", ""), "calls": []}

    def run(argv, route):
        state["calls"].append((argv, route))
        if argv[1:3] == ["image", "exists"]:
            return state["held"], "", ""
        return state["pull"]

    monkeypatch.setattr(module, "run", run)
    return state


def test_a_missing_image_is_pulled_once_over_the_frozen_route_and_a_held_one_is_not(podman, run_module):
    arguments = {"image": IMAGE, "egress": EGRESS}
    pulled = run_module(module, arguments)
    assert pulled["changed"] is True and pulled["image"] == IMAGE
    pull = [call for call in podman["calls"] if call[0][1] == "pull"]
    assert len(pull) == 1 and pull[0][0][-1] == IMAGE and "--tls-verify=true" in pull[0][0]
    assert pull[0][1]["https_proxy"] == "http://proxy.example.test:3128" and pull[0][1]["NO_PROXY"] == "10.0.0.0/8"
    podman["held"], podman["calls"] = 0, []
    held = run_module(module, arguments)
    assert held["changed"] is False and held["image"] == IMAGE
    assert [call[0][1] for call in podman["calls"]] == ["image"]


def test_a_reference_not_pinned_by_digest_is_refused_before_podman_runs(podman, run_module):
    result = run_module(module, {"image": "registry.example.test/nginx:latest", "egress": EGRESS})
    assert result["failed"] is True and result["msg"] == "the image is not a reference pinned by its sha256 digest"
    assert podman["calls"] == []


def test_a_failed_pull_names_the_registry_and_podmans_last_line_and_leaks_no_credential(podman, run_module):
    podman["held"], podman["pull"] = 1, (125, "", "Trying to pull\nunauthorized: authentication required\n")
    arguments = {"image": IMAGE, "egress": EGRESS, "host": "registry.example.test", "username": "lent-user", "password": "lent-secret"}
    result = run_module(module, arguments)
    assert result["failed"] is True
    assert result["msg"] == "%s could not be pulled from registry.example.test: unauthorized: authentication required" % IMAGE
    assert "lent-secret" not in str(result)
