"""Every role proves a file is served through the one shared artifact listener proof.

The proof fetches one byte as a consumer does: never through an ambient proxy,
whose own 403 would read as the listener's, never across a redirect, which
would carry a private URL's token to another address, and verifying an https
listener against the serving certificate bound to the operation. The fetch is
hidden, because the URL may carry a token, and a separate step outside no_log
fails with the classified cause, so the operator learns why. A fetch that ended
before any status line, other than by a certificate that did not verify, spends
one attempt; a status or a certificate failure ends the poll at once (B1044,
the managed OS listener precedent B1036).

Rendering the role files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib

import pytest

from ansible.template import Templar, trust_as_template
from ansible_collections.bootwright.core.plugins.filter.listener_refusal import listener_refusal
from ansible_collections.bootwright.core.tests.unit import listener_proof, role_tasks
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
URI = "ansible.builtin.uri"
FAIL = "ansible.builtin.fail"
FETCH = "Fetch the first byte through the artifact server listener"
JUDGE = "Refuse a file the artifact server listener does not serve"

# Every proof by role, task file and the name of the include that starts it.
# The list is exact: a proof written anywhere else, or an include gone, fails.
CALLERS = {
    ("physical_erase", "boot.yml", "Prove the private image is served over its verified listener"),
    ("managedos_install_anaconda", "apply.yml", "Fetch the first byte of the installer image through its listener"),
    ("managedos_install_anaconda", "apply.yml", "Fetch the first byte of each private file through its listener"),
    ("managedos_install_anaconda", "apply.yml", "Fetch the first byte of the package tree marker through its listener"),
    ("managedos_install_anaconda", "private_recovery.yml", "Fetch the first byte of the recovery passphrase through its listener"),
    ("managedos_package_tree", "apply.yml", "Prove the published tree answers through its trusted listener"),
    ("containercluster_media_agent", "publish.yml", "Fetch the first byte of the published image through the listener"),
    ("containercluster_install_agent", "boot.yml", "Prove the insert image through the verified serving listener"),
}

# The callers whose phase budget holds a single attempt, with the retries each passes.
SINGLE_ATTEMPT = {"Prove the insert image through the verified serving listener": 0}

# What ansible.builtin.uri registers for one attempt, and whether that attempt
# ends the fetch.
ATTEMPTS = [
    ({"status": 206}, True),
    ({"status": 200}, True),
    ({"status": 404, "msg": "Status code was 404 and not [200, 206]: HTTP Error 404: Not Found"}, True),
    ({"status": 403, "msg": "Status code was 403 and not [200, 206]: HTTP Error 403: Forbidden"}, True),
    ({"status": 500, "msg": "Status code was 500 and not [200, 206]: HTTP Error 500: Internal Server Error"}, True),
    ({"status": -1, "msg": "Request failed: <urlopen error [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed>"},
     True),
    ({"status": -1, "msg": "Request failed: <urlopen error timed out>"}, False),
    ({"status": -1, "msg": "Request failed: <urlopen error [Errno 111] Connection refused>"}, False),
    ({"status": -1, "msg": "Request failed: <urlopen error [Errno 104] Connection reset by peer>"}, False),
    ({"status": -1, "msg": "Request failed: <urlopen error [Errno 113] No route to host>"}, False),
    ({"status": -1, "msg": "Request failed: <urlopen error [Errno 101] Network is unreachable>"}, False),
    ({"msg": "the module recorded no status"}, False),
    ({}, False),
]


def label(path):
    return path.relative_to(ROLES).as_posix()


def hidden(task):
    return task.get("no_log") in (True, "true", "True")


def proof_step(name):
    found = [task for task in listener_proof.proof_tasks() if task.get("name") == name]
    assert len(found) == 1, "%d proof tasks named %r" % (len(found), name)
    return found[0]


def test_every_listener_fetch_is_the_shared_proof_and_every_caller_includes_it():
    found, fetches = set(), []
    for path in sorted(ROLES.glob("*/tasks/*.yml")):
        for task in role_tasks.flatten(role_tasks.load_tasks(path)):
            if listener_proof.includes(task):
                found.add((path.parts[-3], path.name, task.get("name")))
            if URI in task:
                fetches.append((label(path), task.get("name")))
    assert found == CALLERS
    assert fetches == [("infrastructureservices_artifact_listener/tasks/proof.yml", FETCH)]


def test_the_fetch_never_uses_a_proxy_or_follows_a_redirect_and_is_hidden():
    task = proof_step(FETCH)
    uri = task[URI]
    assert uri["use_proxy"] is False and uri["follow_redirects"] == "none" and uri["validate_certs"] is True
    assert uri["headers"] == {"Range": "bytes=0-0"} and uri["status_code"] == [200, 206] and uri["timeout"] == 30
    assert uri["method"] == "GET" and "return_content" not in uri
    assert task["no_log"] is True and task["failed_when"] is False and task["changed_when"] is False


@pytest.mark.parametrize("caller", sorted(CALLERS), ids=["%s:%s" % (role, title) for role, _name, title in sorted(CALLERS)])
def test_each_callers_proof_is_judged_outside_no_log_after_its_hidden_fetch(caller):
    role, name, title = caller
    visits = list(role_tasks.visits(role_tasks.role_tasks(ROLES / role, name), listener_proof.opens))
    starts = [index for index, visit in enumerate(visits) if visit.task.get("name") == title]
    assert len(starts) == 1, "%d includes named %r" % (len(starts), title)
    start = visits[starts[0]]
    reached = visits[starts[0] + 1:starts[0] + 1 + len(listener_proof.proof_tasks())]
    assert [visit.task.get("name") for visit in reached] == [task.get("name") for task in listener_proof.proof_tasks()]
    fetch = next(visit for visit in reached if URI in visit.task)
    judge = next(visit for visit in reached if FAIL in visit.task)
    assert fetch.task[URI]["use_proxy"] is False and fetch.task[URI]["follow_redirects"] == "none"
    assert hidden(fetch.task)
    # Neither the include nor a block around it hides the judging step.
    assert not hidden(start.task) and not any(hidden(parent) for parent in start.parents), title
    assert not hidden(judge.task) and "bootwright.core.listener_refusal" in judge.task[FAIL]["msg"]
    assert judge.task["when"] == "%s.status | default(-1) | int not in [200, 206]" % fetch.task["register"]
    given = start.task["vars"]
    assert {"infrastructureservices_artifact_listener_url", "infrastructureservices_artifact_listener_subject"} <= set(given)
    assert int(given.get("infrastructureservices_artifact_listener_retries", 2)) == SINGLE_ATTEMPT.get(title, 2)


def test_a_proof_lost_before_its_status_line_is_tried_again_a_bounded_number_of_times():
    task = proof_step(FETCH)
    defaults = role_tasks.role_defaults(listener_proof.ROLE)
    templar = Templar(loader=LOADER, variables=defaults)
    assert int(templar.template(task["retries"])) == 2 and int(templar.template(task["delay"])) == 5
    for result, ends in ATTEMPTS:
        variables = dict(defaults, **{task["register"]: result})
        assert Templar(loader=LOADER, variables=variables).evaluate_conditional(trust_as_template(task["until"])) is ends, result


TOKEN = "0123456789abcdef" * 4
URL = "https://192.0.2.1:8443/private/clusters/sno/" + TOKEN + "/agent.iso"
INCLUDE = {
    "name": "Prove one file",
    listener_proof.INCLUDE: {"name": listener_proof.NAME, "tasks_from": listener_proof.ENTRY},
    "vars": {
        "infrastructureservices_artifact_listener_url": "{{ url }}",
        "infrastructureservices_artifact_listener_ca": "/run/material/artifact-ca",
        "infrastructureservices_artifact_listener_token": "{{ url | dirname | basename }}",
        "infrastructureservices_artifact_listener_secret": "artifact-server-tls",
        "infrastructureservices_artifact_listener_subject": "the published agent image",
        "infrastructureservices_artifact_listener_consequence": "so no node could boot from it",
    },
}


@pytest.mark.parametrize("answer", [result for result, _ends in ATTEMPTS], ids=[str(result)[:40] for result, _ends in ATTEMPTS])
def test_the_judging_step_refuses_with_the_classified_cause_and_never_the_token(answer):
    answer = dict(answer, msg=answer["msg"] + " " + URL) if "msg" in answer else answer
    outcome = listener_proof.run(INCLUDE, {"url": URL}, answer)
    assert outcome.fetched["url"] == URL and outcome.fetched["ca_path"] == "/run/material/artifact-ca"
    assert outcome.fetched["use_proxy"] is False and outcome.fetched["follow_redirects"] == "none"
    if answer.get("status") in (200, 206):
        assert outcome.refusal is None and outcome.trace[-1] == (JUDGE, "skipped")
        return
    assert outcome.trace[-1] == (JUDGE, "failed")
    assert outcome.refusal == listener_refusal(answer, TOKEN, "artifact-server-tls", "the published agent image",
                                               "so no node could boot from it")
    assert TOKEN not in outcome.refusal


def test_an_http_listener_is_fetched_with_no_certificate_anchor():
    include = dict(INCLUDE, vars=dict(INCLUDE["vars"], infrastructureservices_artifact_listener_ca=""))
    outcome = listener_proof.run(include, {"url": "http://192.0.2.1/os/rhel/.treeinfo"}, {"status": 206})
    assert "ca_path" not in outcome.fetched and outcome.fetched["validate_certs"] is True and outcome.refusal is None


@pytest.mark.parametrize("url", ["", "file:///etc/shadow", None])
def test_a_proof_without_an_http_url_refuses_before_any_fetch(url):
    outcome = listener_proof.run(INCLUDE, {"url": url}, {"status": 206})
    assert outcome.fetched == {} and outcome.refusal == "a listener proof needs an http or https URL and the subject it names"
