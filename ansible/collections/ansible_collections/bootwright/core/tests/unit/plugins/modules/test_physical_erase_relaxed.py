"""The physical_erase module's main() over the real custody controller, with the relaxation Go hands it.

Only the host boundary is stubbed: the hosted root and publication are the
test's, records are read as plain files, key generation and the SSH policy
are fixed, and any session, management-controller client or image build
fails the test. Each case first proves the condition it injected is present.
"""

from __future__ import annotations

import base64
import json
from pathlib import Path
import secrets

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_controller as custody
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker
from ansible_collections.bootwright.core.plugins.modules import physical_erase as module

DIGEST = "d" * 64
OPERATION = "op-" + "0" * 32
VECTORS = json.loads((Path(__file__).resolve().parents[2] / "goldens/physical_erase_relaxation.json").read_text(encoding="utf-8"))
RELAXED = next(vector for vector in VECTORS["vectors"] if vector["relaxation"] == "metadataClear")


@pytest.fixture(name="host")
def stubbed_host(tmp_path, monkeypatch):
    publication = tmp_path / "published"
    publication.mkdir()
    seen = {"kickstart": None}

    def keygen(argv, timeout=60):
        if "-y" not in argv:
            Path(argv[-1]).write_text("private host key")
            Path(argv[-1] + ".pub").write_text("public host key")
        return b"ecdsa-sha2-nistp256 AAAA host"

    def kickstart(network, worker_request, key, authorized):
        seen["kickstart"] = worker_request
        return "kickstart"

    def forbidden(name):
        def reached(*args, **kwargs):
            raise AssertionError(name + " reached")
        return reached

    for target, name, value in (
        (custody, "ROOT", tmp_path / "hosted"),
        (custody, "directory", lambda path, create, mode=0o700: path.mkdir(mode=0o700, exist_ok=True) if create else path.lstat()),
        (custody, "read", lambda path, limit=32768: Path(path).read_bytes()),
        (custody, "native", keygen),
        (custody.ssh_key_policy, "read_policy", lambda: {}),
        (custody.ssh_key_policy, "effective_policy", lambda policy: {"configuration": "Host *\n"}),
        (custody.ssh_key_policy, "check_keys", lambda policy, keys: None),
        (custody.boot, "kickstart", kickstart),
        (custody.Controller, "session", lambda self, command, data=b"": custody.ssh.Outcome(255, b"", False, False, "refused")),
        (custody.redfish, "Client", forbidden("management controller")),
        (custody.redfish, "start", forbidden("boot")),
        (custody.redfish, "finish", forbidden("power-off proof")),
        (custody.Controller, "build", forbidden("image build")),
    ):
        monkeypatch.setattr(target, name, value)
    request = {
        "version": "physical-erase-v1", "network": "network --bootproto=dhcp", "deadlineSeconds": 14400, "targets": VECTORS["declared"],
        "install": {"identity": {"context": "lab", "object": "metal-01"}, "address": "192.0.2.10",
                    "target": {"hardware": {"interfaces": [{"macAddress": "52:54:00:9A:1B:01"}], "serialNumber": "ROOT"}},
                    "private": {"path": str(publication), "url": "https://artifact.example.test/private"}},
    }
    return {"request": request, "publication": publication, "root": tmp_path / "hosted/lab/machines/metal-01/erase" / OPERATION, "seen": seen}


def run(run_module, host, phase, material):
    return run_module(module, {"phase": phase, "request": host["request"], "digest": DIGEST, "material": material})


def booting(attempt, relaxation=None):
    material = {"applyOperation": OPERATION, "originalAttempt": attempt, "authorizedKey": "ssh-ed25519 AAAA fleet",
                "pinnedUUIDBase64": base64.b64encode(b"example-system").decode(), "rootWWNBase64": base64.b64encode(b"0x5000c500a1b2c3d4").decode()}
    if relaxation:
        material["relaxationBase64"] = relaxation
    return material


def retaining(attempt):
    return {"relaxationBase64": VECTORS["retain"], "applyOperation": OPERATION, "originalAttempt": attempt}


def test_a_metadata_clear_removal_holds_its_relaxed_targets_in_custody_and_the_worker_request(host, run_module):
    prepared = run(run_module, host, "prepare", booting("op-remove-1/erase-metal-01/1", RELAXED["relaxationBase64"]))
    assert prepared["changed"] is True and "failed" not in prepared, prepared
    stored = json.loads((host["root"] / "custody.json").read_text())
    assert stored["worker"]["targets"] == RELAXED["targets"] and host["seen"]["kickstart"]["targets"] == RELAXED["targets"]
    assert any(target["method"] == "logicalOverwrite" for target in host["request"]["targets"]), "the frozen request was relaxed in place"
    worker.validate_request(stored["worker"])
    assert run(run_module, host, "observe", booting("op-remove-1/erase-metal-01/1", RELAXED["relaxationBase64"]))["evidence"]["prepared"] is True
    drifted = run(run_module, host, "observe", booting("op-remove-1/erase-metal-01/1"))
    assert drifted["refused"] == "foreign-custody", "an unrelaxed run adopted relaxed custody"


def test_a_retained_removal_withdraws_an_earlier_removals_custody_and_records_retention(host, run_module):
    run(run_module, host, "prepare", booting("op-remove-1/erase-metal-01/1"))
    earlier = json.loads((host["root"] / "custody.json").read_text())
    private = host["publication"] / earlier["token"]
    private.mkdir()
    (private / "erase.iso").write_bytes(b"earlier private image")
    assert (host["root"] / "host-key").exists() and private.exists(), "the injected leftover custody is missing"
    retained = {"version": worker.VERSION, "request": DIGEST, "prepared": True, "removed": True, "retained": True}

    unproved = run(run_module, host, "observe", retaining("op-remove-2/erase-metal-01/1"))
    assert unproved["evidence"] == {"version": worker.VERSION, "request": DIGEST, "prepared": True, "removed": False}
    assert private.exists() and (host["root"] / "host-key").exists(), "the observation changed custody"

    withdrawn = run(run_module, host, "withdraw", retaining("op-remove-2/erase-metal-01/1"))
    assert withdrawn["changed"] is True and withdrawn["evidence"] == retained
    assert not private.exists() and not any((host["root"] / name).exists() for name in custody.BOOT_PRIVATE)
    assert not (host["root"] / "custody.json").exists()
    assert run(run_module, host, "observe", retaining("op-remove-2/erase-metal-01/1"))["evidence"] == retained
    repeated = run(run_module, host, "withdraw", retaining("op-remove-2/erase-metal-01/1"))
    assert repeated["changed"] is False and repeated["evidence"] == retained


def test_a_retained_removal_with_no_custody_records_retention_and_creates_nothing(host, run_module):
    assert not host["root"].exists(), "the injected absence of custody is missing"
    for phase in ("withdraw", "observe"):
        answer = run(run_module, host, phase, retaining("op-remove-2/erase-metal-01/1"))
        assert answer["changed"] is False and answer["evidence"]["retained"] is True
    assert not host["root"].exists()


def test_a_retained_removal_names_custody_it_cannot_locate(host, run_module):
    run(run_module, host, "prepare", booting("op-remove-1/erase-metal-01/1"))
    path = host["root"] / "custody.json"
    path.chmod(0o600)
    state = json.loads(path.read_text())
    path.unlink()
    path.write_text(json.dumps(dict(state, token="../" + secrets.token_hex(8))))
    refused = run(run_module, host, "withdraw", retaining("op-remove-2/erase-metal-01/1"))
    assert "failed" not in refused and refused["refused"] == "custody-drift"
    assert (host["root"] / "host-key").exists(), "a refused withdrawal removed custody"


@pytest.mark.parametrize("record", [b"{not json", b"[]", b"\xff\xfe"])
def test_a_retained_removal_names_custody_it_cannot_decode_and_never_reads_it_as_retained(host, run_module, record):
    run(run_module, host, "prepare", booting("op-remove-1/erase-metal-01/1"))
    path = host["root"] / "custody.json"
    path.chmod(0o600)
    path.write_bytes(record)
    assert path.read_bytes() == record and (host["root"] / "host-key").exists(), "the injected undecodable custody is missing"
    for phase in ("withdraw", "observe"):
        refused = run(run_module, host, phase, retaining("op-remove-2/erase-metal-01/1"))
        assert "failed" not in refused and refused["refused"] == "custody-drift" and "evidence" not in refused, refused
    assert path.read_bytes() == record and (host["root"] / "host-key").exists(), "a refused withdrawal changed custody"
