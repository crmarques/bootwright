"""A pinned image is pulled once, from its mirror with lent credential and trust that never outlive the pull."""

import base64
import json
import os

import pytest

from ansible_collections.bootwright.core.plugins.modules import image_acquire

DIGEST = "sha256:" + "a" * 64
ORIGIN = "quay.io/metal3-io/sushy-tools@" + DIGEST
MIRRORED = "mirror.example.test:8443/library/quay.io/metal3-io/sushy-tools@" + DIGEST
EGRESS = {"httpProxy": "", "httpsProxy": "http://proxy.example.test:3128", "noProxy": ["10.0.0.0/8", ".example.test"]}


class Runner:
    def __init__(self, held=1, pulled=0, err=""):
        self.calls, self.held, self.pulled, self.err, self.lent = [], held, pulled, err, {}

    def __call__(self, argv, route):
        self.calls.append((argv, route))
        if argv[1:3] == ["image", "exists"]:
            return self.held, "", ""
        for argument in argv:
            for flag in ("--authfile=", "--cert-dir="):
                if argument.startswith(flag):
                    path = argument[len(flag):]
                    held = os.path.join(path, "ca.crt")
                    self.lent[flag] = (path, open(path).read() if flag == "--authfile=" else (open(held).read() if os.path.exists(held) else None))
                    self.lent[flag + "mode"] = os.stat(path).st_mode & 0o777
        return self.pulled, "", self.err


def test_a_host_that_already_holds_the_reference_pulls_nothing():
    runner = Runner(held=0)
    assert image_acquire.acquire(runner, MIRRORED, EGRESS, "mirror.example.test:8443", "user", "pass") is False
    assert [call[0][1] for call in runner.calls] == ["image"]


def test_a_mirror_pull_lends_its_credential_and_anchor_only_for_the_pull():
    runner = Runner()
    assert image_acquire.acquire(runner, MIRRORED, EGRESS, "mirror.example.test:8443", "user", "pass", trust="PEM") is True
    argv, route = runner.calls[-1]
    assert argv[:4] == ["/usr/bin/podman", "pull", "--quiet", "--tls-verify=true"] and argv[-1] == MIRRORED
    path, document = runner.lent["--authfile="]
    token = base64.b64encode(b"user:pass").decode()
    assert json.loads(document) == {"auths": {"mirror.example.test:8443": {"auth": token}}}
    assert runner.lent["--authfile=mode"] == 0o600 and runner.lent["--cert-dir=mode"] == 0o700
    assert runner.lent["--cert-dir="][1] == "PEM"
    assert not os.path.exists(path) and not os.path.exists(runner.lent["--cert-dir="][0])
    assert route["https_proxy"] == route["HTTPS_PROXY"] == "http://proxy.example.test:3128"
    assert route["http_proxy"] == "" and route["no_proxy"] == "10.0.0.0/8,.example.test"


def test_an_auth_document_is_lent_with_the_mirrors_entry_alone_and_a_bad_one_never_echoes():
    runner = Runner()
    document = json.dumps({"auths": {"https://mirror.example.test:8443/v1/": {"auth": "c2VjcmV0"}, "quay.io": {"auth": "b3RoZXI="}}})
    image_acquire.acquire(runner, MIRRORED, EGRESS, "mirror.example.test:8443", auth=document)
    assert json.loads(runner.lent["--authfile="][1]) == {"auths": {"https://mirror.example.test:8443/v1/": {"auth": "c2VjcmV0"}}}
    with pytest.raises(ValueError, match="no entry for mirror.example.test:8443"):
        image_acquire.acquire(Runner(), MIRRORED, EGRESS, "mirror.example.test:8443", auth=json.dumps({"auths": {"quay.io": {"auth": "x"}}}))
    with pytest.raises(ValueError) as refused:
        image_acquire.acquire(Runner(), MIRRORED, EGRESS, "mirror.example.test:8443", auth="secret-not-json")
    assert "secret" not in str(refused.value)


def test_a_mirror_pull_without_a_credential_reads_neither_the_hosts_login_nor_its_certs_d(tmp_path):
    runner = Runner()
    image_acquire.acquire(runner, MIRRORED, EGRESS, "mirror.example.test:8443", base=str(tmp_path))
    assert json.loads(runner.lent["--authfile="][1]) == {"auths": {}}
    assert runner.lent["--authfile="][0].startswith(str(tmp_path)) and os.listdir(tmp_path) == []


def test_a_pull_without_a_mirror_binds_nothing_and_a_credential_never_leaves_its_mirror():
    runner = Runner()
    assert image_acquire.acquire(runner, ORIGIN, EGRESS) is True
    assert not any(argument.startswith(("--authfile", "--cert-dir")) for argument in runner.calls[-1][0])
    with pytest.raises(ValueError, match="belong to mirror.example.test:8443"):
        image_acquire.acquire(Runner(), ORIGIN, EGRESS, "mirror.example.test:8443", "user", "pass")
    with pytest.raises(ValueError, match="user name or its password"):
        image_acquire.acquire(Runner(), MIRRORED, EGRESS, "mirror.example.test:8443", "user", "")


def test_an_unpinned_reference_refuses_before_any_command():
    runner = Runner()
    with pytest.raises(ValueError, match="pinned"):
        image_acquire.acquire(runner, "quay.io/metal3-io/sushy-tools:latest", EGRESS)
    assert runner.calls == []


def test_a_mirror_that_lacks_the_digest_fails_naming_it_and_removes_the_lent_files():
    runner = Runner(pulled=125, err="Trying to pull...\nError: reading manifest " + DIGEST + ": manifest unknown")
    with pytest.raises(RuntimeError) as failed:
        image_acquire.acquire(runner, MIRRORED, EGRESS, "mirror.example.test:8443", "user", "pass", trust="PEM")
    assert MIRRORED in str(failed.value) and "manifest unknown" in str(failed.value)
    assert not os.path.exists(runner.lent["--authfile="][0])
