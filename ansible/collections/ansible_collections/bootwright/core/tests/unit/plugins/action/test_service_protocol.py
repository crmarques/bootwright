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
