"""Signed inherited crypto-policy facts and their descriptor-bound installed proof."""
from __future__ import annotations

import copy
import hashlib
import os
import stat
from types import SimpleNamespace

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_crypto_policy as policy, ceph_native, native_signatures


def facts(path, data):
    return {"sha256": hashlib.sha256(data).hexdigest(), "mode": stat.S_IFREG | 0o644, "uid": 0, "gid": 0, "links": 1}


@pytest.fixture
def fixture(tmp_path, monkeypatch):
    root = tmp_path / "original-root"
    root.mkdir(mode=0o755)
    files = {}
    for path in policy.FILES:
        local = root / path.lstrip("/")
        local.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
        data = ("Ciphers aes256-ctr\n" if path != policy.GENERATOR else "# synthetic signed generator\n").encode()
        local.write_bytes(data)
        local.chmod(0o644)
        files[path] = facts(path, data)
    target = "/usr/share/crypto-policies/DEFAULT/opensshserver.txt"
    monkeypatch.setattr(policy, "OWNER", os.getuid())
    monkeypatch.setattr(policy, "root_descriptor", lambda: os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC))
    packages = [{"identity": {"name": name, "epoch": 0, "version": "1", "release": "1", "architecture": "noarch"},
                 "sourceID": "native-" + str(index) * 32, "headerSHA256": str(index) * 64}
                for index, name in enumerate(sorted(policy.PACKAGES), 1)]
    record = {"version": "ceph-crypto-policy-v1", "scope": {"host": "node-1"}, "binding": "a" * 64,
              "planDigest": "b" * 64, "packages": packages, "files": files}
    return SimpleNamespace(root=root, target=target, record=record)


def test_signed_file_is_read_without_effect_or_path_discovery(fixture):
    before = [(path, path.lstat()) for path in fixture.root.rglob("*")]
    assert policy.read_file(fixture.target, fixture.record["files"][fixture.target]) == b"Ciphers aes256-ctr\n"
    assert [(str(path), policy.identity(info)) for path, info in before] == [(str(path), policy.identity(path.lstat())) for path, unused in before]


@pytest.mark.parametrize("damage", ["symlink-target", "writable-target", "hardlinked-target", "target-bytes", "parent-symlink", "writable-parent"])
def test_signed_file_never_adopts_foreign_file_layout_or_changed_bytes(fixture, damage):
    target = fixture.root / fixture.target.lstrip("/")
    if damage == "symlink-target":
        target.unlink()
        target.symlink_to("../FUTURE/opensshserver.txt")
    elif damage == "writable-target":
        target.chmod(0o666)
    elif damage == "hardlinked-target":
        os.link(target, target.parent / "other")
    elif damage == "target-bytes":
        target.write_bytes(b"Ciphers unexpected\n")
    elif damage == "parent-symlink":
        parent = target.parent
        parent.rename(parent.with_name("original"))
        parent.symlink_to("original", target_is_directory=True)
    else:
        target.parent.chmod(0o777)
    with pytest.raises((ceph_native.Refusal, OSError)):
        policy.read_file(fixture.target, fixture.record["files"][fixture.target])


def signed_plan():
    packages = []
    for index, name in enumerate(sorted(policy.PACKAGES), 1):
        packages.append({"name": name, "epoch": 0, "version": "1", "release": "1", "architecture": "noarch",
                         "signer": native_signatures.RHEL_SIGNER,
                         "source": {"id": "native-" + str(index) * 32, "url": "https://192.0.2.10:8443/os/rhel/tree/BaseOS/" + name + ".rpm"}})
    return {"digest": "b" * 64, "packages": packages}


def test_signed_capture_admits_packaged_hardlinks_only_at_their_signed_link_count(fixture):
    plan = signed_plan()
    headers = {p["source"]["id"]: str(index) * 64 for index, p in enumerate(plan["packages"], 1)}
    payloads = {p["source"]["id"]: "/private/" + p["name"] + ".rpm" for p in plan["packages"]}
    scope = {"host": "node-1", "source": {"url": "https://192.0.2.10:8443/os/rhel/tree"}}
    seen = []

    def query(argv):
        seen.append(argv)
        paths = policy.TARGETS if argv[-1].endswith("crypto-policies.rpm") else {policy.GENERATOR}
        return ("algo|8\n" + "".join(path + "|" + fixture.record["files"][path]["sha256"] + "|" + str(stat.S_IFREG | 0o644) + "|root|root|0|"
                                     + ("2" if path in policy.TARGETS else "1") + "\n" for path in sorted(paths))).encode()
    record = policy.capture(plan, payloads, headers, scope, "a" * 64, query)
    policy.validate(record, plan, headers, scope, "a" * 64)
    assert set(record["files"]) == policy.FILES and len(seen) == 2
    assert all(argv[:3] == ["-qp", "--nosignature", "--qf"] for argv in seen)
    assert {path: value["links"] for path, value in record["files"].items()} == {path: 2 if path in policy.TARGETS else 1 for path in policy.FILES}
    (fixture.root / "usr/share/crypto-policies/back-ends").mkdir(mode=0o755)
    for path in policy.TARGETS:
        local = fixture.root / path.lstrip("/")
        os.link(local, local.parent.parent / "back-ends" / (local.parent.name + "-opensshserver.config"))
    for path, value in record["files"].items():
        assert policy.read_file(path, value)
    target = fixture.root / fixture.target.lstrip("/")
    os.link(target, target.parent / "aliased")
    with pytest.raises(ceph_native.Refusal):
        policy.read_file(fixture.target, record["files"][fixture.target])
    os.unlink(target.parent / "aliased")
    os.unlink(target.parent.parent / "back-ends" / "DEFAULT-opensshserver.config")
    with pytest.raises(ceph_native.Refusal):
        policy.read_file(fixture.target, record["files"][fixture.target])
    for path in policy.TARGETS - {fixture.target}:
        policy.read_file(path, record["files"][path])


@pytest.mark.parametrize("damage", ["missing-root", "wrong-signer", "foreign-source", "duplicate-root"])
def test_capture_refuses_unsigned_missing_or_foreign_policy_roots(damage):
    plan = signed_plan()
    if damage == "missing-root":
        plan["packages"].pop()
    elif damage == "wrong-signer":
        plan["packages"][0]["signer"] = native_signatures.IBM_CEPH_SIGNER
    elif damage == "foreign-source":
        plan["packages"][0]["source"]["url"] = "https://foreign.example.test/policy.rpm"
    else:
        plan["packages"].append(copy.deepcopy(plan["packages"][0]))
    headers = {p["source"]["id"]: "c" * 64 for p in plan["packages"]}
    with pytest.raises(ceph_native.Refusal):
        policy.capture(plan, {}, headers, {"source": {"url": "https://192.0.2.10:8443/os/rhel/tree"}}, "a" * 64,
                       lambda *args: pytest.fail("unproved payload header read"))


@pytest.mark.parametrize("line", ["algo|1\n", "algo|8\n" + policy.GENERATOR + "|" + "c" * 64 + "|" + str(stat.S_IFREG | 0o666) + "|root|root|0|1\n",
                                  "algo|8\n" + policy.GENERATOR + "|" + "c" * 64 + "|" + str(stat.S_IFREG | 0o644) + "|root|root|64|1\n",
                                  "algo|8\n" + policy.GENERATOR + "|" + "c" * 64 + "|" + str(stat.S_IFREG | 0o644) + "|operator|root|0|1\n",
                                  "algo|8\n" + policy.GENERATOR + "|" + "c" * 64 + "|" + str(stat.S_IFREG | 0o644) + "|root|root|0\n",
                                  "algo|8\n" + policy.GENERATOR + "|" + "c" * 64 + "|" + str(stat.S_IFREG | 0o644) + "|root|root|0|0\n",
                                  "algo|8\n" + policy.GENERATOR + "|" + "c" * 64 + "|" + str(stat.S_IFREG | 0o644) + "|root|root|0|9\n"])
def test_signed_file_facts_reject_wrong_digest_algorithm_or_unprotected_payload_mode(line):
    with pytest.raises(ceph_native.Refusal):
        policy.header_files(line.encode())


def test_installed_generator_and_profiles_match_signed_original_and_no_verify_script_runs(fixture):
    commands = []

    def query(argv):
        commands.append(argv)
        if argv[0] == "-q":
            return (fixture.record["packages"][len(commands) // 2]["headerSHA256"] + "\n").encode()
        return b""
    policy.installed(fixture.record, query)
    assert len(commands) == 4 and all("--noscripts" in argv for argv in commands if argv[0] == "--verify")
    path = fixture.root / policy.GENERATOR.lstrip("/")
    path.write_bytes(b"# replaced generator\n")
    with pytest.raises(ceph_native.Refusal):
        policy.installed(fixture.record, lambda argv: b"" if argv[0] == "--verify" else (fixture.record["packages"][0]["headerSHA256"] + "\n").encode())
