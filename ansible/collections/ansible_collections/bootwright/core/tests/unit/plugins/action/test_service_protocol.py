"""A refused postcondition publishes no evidence, so the names are the message.

Both service protocols publish under `no_log`, and the adapter's output
callback prints nothing a hidden task raised (plugins/callback/censored.py),
so the refusal reaches no output. It names fields, never values, so it stays
safe to print should it ever be.
"""

from __future__ import annotations

from types import SimpleNamespace

from ansible_collections.bootwright.core.plugins.action import (
    artifact_server_protocol,
    infra_service_protocol,
)

PROTOCOLS = (artifact_server_protocol, infra_service_protocol)

RUNNING = {"unit": "active", "container": "quay.io/x", "containerPresent": True, "contentRoot": True, "startedAfterFiles": True}
GONE = {"unit": "", "container": "", "containerPresent": False, "contentRoot": False, "startedAfterFiles": False}


def test_a_service_that_is_gone_names_nothing():
    for protocol in PROTOCOLS:
        assert protocol.remaining(GONE) == []


def test_a_service_that_remains_names_every_part_of_itself():
    for protocol in PROTOCOLS:
        assert protocol.remaining(RUNNING) == ["unit", "container", "contentRoot"]


def test_a_stopped_unit_left_behind_is_named_alone():
    left = dict(GONE, unit="inactive")
    for protocol in PROTOCOLS:
        assert protocol.remaining(left) == ["unit"]


def test_a_running_service_proves_itself_without_naming_anything():
    for protocol in PROTOCOLS:
        assert protocol.unproved(RUNNING) == []


def test_a_service_that_never_started_names_what_is_unproved():
    assert artifact_server_protocol.unproved(GONE) == ["unit", "contentRoot"]
    assert infra_service_protocol.unproved(GONE) == ["unit", "contentRoot", "startedAfterFiles"]
    for protocol in PROTOCOLS:
        assert protocol.unproved(dict(RUNNING, unit="failed")) == ["unit"]
        assert protocol.unproved(dict(RUNNING, contentRoot=False)) == ["contentRoot"]


# A managed network service's daemon keeps what it read at its start, so one
# that started before a file it runs from was last published runs an earlier
# configuration, as an apply stopped between publishing and restarting leaves
# it. Its presence then proves no postcondition, which an observation publishes
# for the engine to read as partial and an apply refuses, naming the start.
def test_a_network_service_older_than_its_files_proves_no_postcondition():
    digest = "1" * 64
    answers = [{"address": "192.0.2.1", "answer": "HTTP/1.1 400 Bad Request", "port": 3128}]
    assert infra_service_protocol.presence(RUNNING, answers, digest)["startedAfterFiles"] is True
    stale = dict(RUNNING, startedAfterFiles=False)
    evidence = infra_service_protocol.presence(stale, answers, digest)
    assert (evidence["postcondition"], evidence["startedAfterFiles"]) == (False, False)
    assert (evidence["unit"], evidence["container"], evidence["contentRoot"]) == ("active", "quay.io/x", True)
    assert infra_service_protocol.unproved(stale) == ["startedAfterFiles"]
    assert infra_service_protocol.publishes(evidence, True)
    assert not infra_service_protocol.publishes(evidence, False)
    unreported = {name: value for name, value in RUNNING.items() if name != "startedAfterFiles"}
    assert infra_service_protocol.presence(unreported, answers, digest)["postcondition"] is False
    assert infra_service_protocol.absence(GONE, digest)["startedAfterFiles"] is False


# An observation of a service all present whose listener did not answer
# publishes the presence form with no answer and the unit and content root it
# found, which is its postcondition. The engine reads that as this context's
# own service not yet ready, a partial realization, so a fresh destroy over an
# incomplete apply resolves it rather than refusing on it.
def test_a_present_service_with_a_silent_listener_publishes_what_it_found():
    digest = "1" * 64
    for evidence, answered in (
        (infra_service_protocol.presence(RUNNING, [], digest), "answers"),
        (artifact_server_protocol.presence({}, RUNNING, [], digest), "listeners"),
    ):
        assert evidence["absent"] is False
        assert evidence[answered] == []
        assert (evidence["unit"], evidence["container"], evidence["contentRoot"]) == ("active", "quay.io/x", True)
        assert evidence["postcondition"] is True


# Both service protocols follow the same rule: only a read-only observation may
# publish evidence that proves no postcondition, so the engine can resolve a
# service part way realized instead of leaving its effect unproved.
def test_only_an_observation_publishes_an_unmet_postcondition():
    for protocol in PROTOCOLS:
        assert protocol.publishes({"postcondition": True}, False)
        assert protocol.publishes({"postcondition": False}, True)
        assert not protocol.publishes({"postcondition": False}, False)
        assert not protocol.publishes({"postcondition": False}, None)


# A managed service checks its socket before it starts its unit, and something
# else already listening there is refused through the protocol's refused record,
# which the runner reports as the service's own diagnostic naming that socket.
def test_a_managed_service_hands_a_foreign_listener_refusal(monkeypatch):
    records = []
    monkeypatch.setattr(
        infra_service_protocol, "emit", lambda record, acknowledge=False: records.append((record, acknowledge)),
    )
    action = infra_service_protocol.ActionModule.__new__(infra_service_protocol.ActionModule)
    action._task = SimpleNamespace(args={"phase": "refused", "reason": "foreign-listener", "port": 53})
    assert action.run(task_vars={}) == {"changed": False}
    assert records == [({"phase": "refused", "reason": "foreign-listener-53"}, False)]
