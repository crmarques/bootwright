"""Artifact readback proves the daemon started after each file it consumes."""
from __future__ import annotations

from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import artifact_server, infra_service


@pytest.mark.parametrize("https", [False, True])
@pytest.mark.parametrize("bad", [None, "unit", "config", "certificate", "key", "unreadable", "start"])
def test_artifact_start_covers_only_consumed_files(monkeypatch, https, bad):
    root = "/synthetic/artifact"
    unit = "/etc/containers/systemd/example.container"
    paths = {unit: "unit", root + "/config/nginx.conf": "config"}
    if https:
        paths.update({root + "/tls/server.crt": "certificate", root + "/tls/server.key": "key"})
    seen = []

    def stat(path):
        seen.append(path)
        assert path in paths
        if bad == "unreadable":
            raise OSError("synthetic unavailable metadata")
        return SimpleNamespace(st_mtime_ns=101 if paths[path] == bad else 99)

    def runner(argv, **options):
        assert argv[-1] == "example"
        assert argv[-2] == "{{.State.StartedAt.UnixNano}}"
        return (1, "", "") if bad == "start" else (0, "100", "")

    monkeypatch.setattr(artifact_server, "unit_state", lambda *args: "active")
    monkeypatch.setattr(artifact_server, "digest_file", lambda *args: "a" * 64)
    monkeypatch.setattr(artifact_server, "container_image", lambda *args: "image")
    monkeypatch.setattr(artifact_server, "container_present", lambda *args: True)
    monkeypatch.setattr(artifact_server.os.path, "isdir", lambda *args: True)
    monkeypatch.setattr(infra_service.os, "lstat", stat)
    request = {"unit": "example", "contentRoot": root, "listeners": [{"protocol": "https" if https else "http"}]}
    observation = artifact_server.observe(runner, request)
    expected = bad is None or not https and bad in ("certificate", "key")
    assert observation["startedAfterFiles"] is expected
    if expected:
        assert set(seen) == set(paths)


def test_a_host_without_podman_holds_no_artifact_container():
    def without_podman(argv, check_rc=False, environ_update=None, handle_exceptions=True):
        assert argv[0] == artifact_server.PODMAN and handle_exceptions is False
        raise FileNotFoundError(2, "No such file or directory", artifact_server.PODMAN)

    assert artifact_server.container_image(without_podman, "example") == ""
    assert artifact_server.container_present(without_podman, "example") is False
