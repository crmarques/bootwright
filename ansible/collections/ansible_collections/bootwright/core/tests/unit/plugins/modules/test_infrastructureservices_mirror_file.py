"""The pinned mirror file publishes only a successful acquisition."""

from __future__ import annotations

import hashlib
import os

import pytest

from ansible_collections.bootwright.core.plugins.modules import infrastructureservices_mirror_file as mirror_file


def test_publishes_the_pinned_file_and_leaves_the_previous_file_after_failure(tmp_path, monkeypatch):
    source = {"id": "file", "url": "https://publisher.example.test/file",
              "sha256": hashlib.sha256(b"wanted").hexdigest(), "bytes": 6}
    egress = {"httpProxy": "", "httpsProxy": "http://proxy.example.test:3128", "noProxy": []}
    destination = tmp_path / "file"
    monkeypatch.setattr(mirror_file.os, "fchown", lambda *_args: None)

    def download(selected, route, descriptor, seconds, _routes):
        assert (selected, route, seconds) == (source, egress, 120)
        os.write(descriptor, b"wanted")
    monkeypatch.setattr(mirror_file.controller_files, "download", download)
    assert mirror_file.publish(source, egress, str(destination), 120)
    assert destination.read_bytes() == b"wanted" and destination.stat().st_mode & 0o777 == 0o644

    def refused(_source, _egress, descriptor, _seconds, _routes):
        os.write(descriptor, b"truncated")
        raise mirror_file.controller_files.Refused("source integrity")
    monkeypatch.setattr(mirror_file.controller_files, "download", refused)
    monkeypatch.setattr(mirror_file, "held", lambda *_args: False)
    with pytest.raises(mirror_file.controller_files.Refused):
        mirror_file.publish(source, egress, str(destination), 120)
    assert destination.read_bytes() == b"wanted" and list(tmp_path.iterdir()) == [destination]


def test_module_passes_the_frozen_file_and_route_to_publication(monkeypatch, run_module):
    params = {
        "source": {"id": "file", "url": "https://publisher.example.test/file", "sha256": "a" * 64, "bytes": 8},
        "egress": {"httpProxy": "", "httpsProxy": "", "noProxy": []},
        "dest": "/var/lib/mirror/file",
        "seconds": 120,
    }
    calls = []

    def publish(*args):
        calls.append(args)
        return True

    monkeypatch.setattr(mirror_file, "publish", publish)
    assert run_module(mirror_file, params)["changed"] is True
    assert calls == [(params["source"], params["egress"], params["dest"], params["seconds"])]


@pytest.mark.parametrize(("egress", "route"), [
    ({"noProxy": []}, None),
    ({"httpsProxy": "http://proxy.example.test:3128", "noProxy": []}, "http://proxy.example.test:3128"),
    ({"httpsProxy": "http://proxy.example.test:3128", "noProxy": ["publisher.example.test"]}, None),
])
def test_the_route_go_freezes_without_its_empty_endpoints_reaches_route_selection(monkeypatch, run_module, egress, route):
    params = {
        "source": {"id": "file", "url": "https://publisher.example.test/file", "sha256": "a" * 64, "bytes": 8},
        "egress": egress,
        "dest": "/var/lib/mirror/file",
        "seconds": 120,
    }
    selected = []

    def publish(source, frozen, _destination, _seconds):
        files = mirror_file.controller_files
        selected.append(files.proxy_for(frozen, files.endpoint(source["url"])))
        return True

    monkeypatch.setattr(mirror_file, "publish", publish)
    assert run_module(mirror_file, params)["changed"] is True
    assert selected == [route]
