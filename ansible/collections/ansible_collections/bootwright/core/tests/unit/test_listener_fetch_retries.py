"""A controller listener self-fetch lost before its status line is made again a bounded number of times.

The package tree, agent image and private erase image proofs each fetch one
byte through the artifact server listener. A fetch that ended before any status
line, other than by a certificate that did not verify, spends one attempt; a
status or a certificate failure ends the poll at once, so the step that judged
a single attempt judges the last one with its unchanged refusal (B1044, the
managed OS listener precedent B1036).

Rendering the role files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib

import pytest

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
LOADER = DataLoader()
URI = "ansible.builtin.uri"

# Each proof by role, task file and name, and whether a separate step judges
# its result (failed_when false) or the fetch's own failure is the refusal.
FETCHES = [
    ("managedos_package_tree", "apply.yml", "Prove the published tree answers through its trusted listener", False),
    ("containercluster_media_agent", "publish.yml", "Fetch the first byte of the published image through the listener", True),
    ("physical_erase", "boot.yml", "Prove the private image is served over its verified listener", False),
]

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


def flattened(role, name):
    def opened(listed):
        for task in listed or []:
            if not isinstance(task, dict):
                continue
            if "block" in task:
                for section in ("block", "rescue", "always"):
                    yield from opened(task.get(section))
                continue
            yield task
    return list(opened(LOADER.load_from_file(str(ROLES / role / "tasks" / name), trusted_as_template=True)))


def defaults(role):
    return dict(LOADER.load_from_file(str(ROLES / role / "defaults" / "main.yml"), trusted_as_template=True))


def fetch(role, name, title):
    found = [task for task in flattened(role, name) if task.get("name") == title]
    assert len(found) == 1, "%d tasks named %r" % (len(found), title)
    assert URI in found[0]
    return found[0]


@pytest.mark.parametrize("role, name, title, judged", FETCHES, ids=[role for role, *_rest in FETCHES])
def test_a_listener_proof_lost_before_its_status_line_is_tried_again_a_bounded_number_of_times(role, name, title, judged):
    task = fetch(role, name, title)
    register = task["register"]
    templar = Templar(loader=LOADER, variables=defaults(role))
    assert int(templar.template(task["retries"])) == 2 and int(templar.template(task["delay"])) == 5
    assert task.get("failed_when", True) is (not judged)
    assert task[URI]["timeout"] == 30 and task[URI]["validate_certs"] is True
    for result, ends in ATTEMPTS:
        variables = dict(defaults(role), **{register: result})
        assert Templar(loader=LOADER, variables=variables).evaluate_conditional(trust_as_template(task["until"])) is ends, result
