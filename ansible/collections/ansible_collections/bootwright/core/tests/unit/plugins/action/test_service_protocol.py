"""A refused postcondition publishes no evidence, so the names are the message.

Both service protocols censor their result with `no_log`, which means the field
names in the refusal are the only thing an operator is ever told about why a
managed service would not prove itself present or gone.
"""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.action import (
    artifact_server_protocol,
    infra_service_protocol,
)

PROTOCOLS = (artifact_server_protocol, infra_service_protocol)

RUNNING = {"unit": "active", "container": "quay.io/x", "containerPresent": True, "contentRoot": True}
GONE = {"unit": "", "container": "", "containerPresent": False, "contentRoot": False}


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
    for protocol in PROTOCOLS:
        assert protocol.unproved(GONE) == ["unit", "contentRoot"]
        assert protocol.unproved(dict(RUNNING, unit="failed")) == ["unit"]
        assert protocol.unproved(dict(RUNNING, contentRoot=False)) == ["contentRoot"]


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
