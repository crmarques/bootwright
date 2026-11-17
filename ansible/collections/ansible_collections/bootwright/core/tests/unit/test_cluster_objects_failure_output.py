"""Partial object evidence survives while the bounded refusal remains readable."""
from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys

import pytest
import yaml

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
COLLECTIONS = COLLECTION.parents[2]
ROLE = COLLECTION / "roles" / "cluster_objects"
PRIVATE_UID = "private-receipt-uid"
REFUSAL = "InstallPlan/operators/initial approval proof differs; CSVs example.v1"


@pytest.fixture(params=["refused", "complete", "unpublished"])
def publication(tmp_path, request):
    mode = request.param
    postcondition = mode != "refused"
    tasks = yaml.safe_load((ROLE / "tasks" / "apply.yml").read_text())
    publish = tasks[-1]
    state = {"postcondition": postcondition, "unknown": False, "receipts": [{"uid": PRIVATE_UID}], "observations": [], "olm": [], "checks": []}
    result = {"state": state, "changed": True}
    if not postcondition:
        result.update(failed=True, msg=REFUSAL)
    extra = {"cluster_objects_result": result, "cluster_objects_evidence_version": "addon-instance-evidence-v1",
             "cluster_objects_digest": "invalid" if mode == "unpublished" else "a" * 64}
    (tmp_path / "request.json").write_text(json.dumps(extra))
    (tmp_path / "publication.yml").write_text(json.dumps([{
        "name": "Publish bounded object evidence", "hosts": "localhost", "connection": "local",
        "gather_facts": False, "tasks": [publish],
    }]))
    (tmp_path / "ansible.cfg").write_text("")
    environment = dict(os.environ, ANSIBLE_CONFIG=str(tmp_path / "ansible.cfg"),
                       ANSIBLE_HOME=str(tmp_path / "home"), ANSIBLE_LOCAL_TEMP=str(tmp_path / "local"),
                       ANSIBLE_COLLECTIONS_PATH=str(COLLECTIONS),
                       ANSIBLE_STDOUT_CALLBACK="bootwright.core.censored", ANSIBLE_DISPLAY_ARGS_TO_STDOUT="False",
                       ANSIBLE_LOAD_CALLBACK_PLUGINS="0", ANSIBLE_NOCOLOR="1", ANSIBLE_FORCE_COLOR="0", LC_ALL="C.UTF-8")
    # The protocol publishes to descriptor 3 in the controller process. Only
    # its completed phase is exercised, so no authorization descriptor is used.
    entry = ("import os,runpy,sys; "
             "channel=os.open(sys.argv.pop(1),os.O_WRONLY|os.O_CREAT|os.O_TRUNC,0o600); "
             "os.dup2(channel,3); runpy.run_module('ansible.cli.playbook',run_name='__main__')")
    evidence = tmp_path / "channel.jsonl"
    ran = subprocess.run([sys.executable, "-c", entry, str(evidence), "-i", "localhost,",
                          "--extra-vars", "@" + str(tmp_path / "request.json"), str(tmp_path / "publication.yml")],
                         stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                         env=environment, cwd=tmp_path, check=False, timeout=30)
    return mode, state, ran, [json.loads(line) for line in evidence.read_text().splitlines()]


def test_failed_publication_keeps_receipts_before_reporting_the_reason(publication):
    mode, state, ran, records = publication
    printed = ran.stdout.decode()
    assert PRIVATE_UID not in printed
    if mode == "unpublished":
        assert not records
        assert ran.returncode != 0
        assert "cluster object result could not be published" in printed
        return
    assert records == [{"phase": "completed", "outcome": "changed",
                        "evidence": dict(state, request="a" * 64, version="addon-instance-evidence-v1")}], printed
    if mode == "complete":
        assert ran.returncode == 0, printed
        assert REFUSAL not in printed
    else:
        assert ran.returncode != 0
        assert REFUSAL in printed, printed
