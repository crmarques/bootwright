"""The one dispatch every capability plugin hands its protocol records through.

Each test drives publish with a channel that records what it is handed, so
what a capability writes to the runner, and what it refuses to, is read here
without a descriptor.
"""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol

DIGEST = "0123456789abcdef" * 4


class Unprovable(ValueError):
    pass


def evidence(arguments):
    return {"postcondition": bool(arguments.get("proved")), "request": adapter_protocol.request_digest(arguments.get("digest"))}


def completion(arguments):
    found = evidence(arguments)
    if arguments.get("unprovable"):
        raise Unprovable("the controller reported a UUID longer than 128 characters")
    if adapter_protocol.publishes(found, arguments.get("observed")):
        return found, None
    return found, adapter_protocol.unreached("the widget", "not proved", ["unit"])


CAPABILITY = adapter_protocol.Capability(
    "widget",
    "the widget capability result could not be published",
    completion,
    phases=adapter_protocol.REFUSING_PHASES,
    refusals=("widget-busy",),
    passthrough=(Unprovable,),
)


def published(arguments, capability=CAPABILITY):
    records = []
    result = adapter_protocol.publish(
        arguments, capability, lambda record, acknowledge=False: records.append((record, acknowledge)),
    )
    return result, records


def test_an_unknown_phase_is_refused_and_hands_nothing():
    result, records = published({"phase": "unloaded"})
    assert result == {"failed": True, "msg": "unsupported widget protocol phase"}
    assert records == []
    plain = adapter_protocol.Capability("widget", "unpublished", completion)
    assert published({"phase": "refused", "reason": "widget-busy"}, plain)[0]["msg"] == "unsupported widget protocol phase"


def test_loaded_is_handed_first_and_waits_for_its_acknowledgement():
    result, records = published({"phase": "loaded"})
    assert result == {"changed": False}
    assert records == [({"phase": "loaded"}, True)]


def test_a_group_status_outside_the_four_is_refused():
    for status in adapter_protocol.GROUP_STATUSES:
        result, records = published({"phase": "group", "group": "Network", "status": status})
        assert result == {"changed": False}
        assert records == [({"phase": "group", "group": "Network", "status": status}, False)]
    result, records = published({"phase": "group", "group": "Network", "status": "done"})
    assert result == {"failed": True, "msg": "the widget capability result could not be published"}
    assert records == []


def test_a_refusal_the_capability_does_not_name_is_refused():
    result, records = published({"phase": "refused", "reason": "widget-busy"})
    assert result == {"changed": False}
    assert records == [({"phase": "refused", "reason": "widget-busy"}, False)]
    result, records = published({"phase": "refused", "reason": "machine-running"})
    assert result == {"failed": True, "msg": "the widget capability result could not be published"}
    assert records == []


def test_a_refusal_named_by_the_arguments_is_handed_as_the_capability_names_it():
    def node(arguments):
        if arguments.get("reason") != "widget-busy":
            raise ValueError("refusal reason")
        return "widget-busy-node-%d" % arguments["node"]

    capability = adapter_protocol.Capability("widget", "unpublished", completion,
                                             phases=adapter_protocol.REFUSING_PHASES, refusals=node)
    assert published({"phase": "refused", "reason": "widget-busy", "node": 2}, capability)[1] == [
        ({"phase": "refused", "reason": "widget-busy-node-2"}, False)]
    assert published({"phase": "refused", "reason": "other", "node": 2}, capability) == (
        {"failed": True, "msg": "unpublished"}, [])


def test_a_completion_proving_no_postcondition_fails_unless_observed():
    arguments = {"phase": "completed", "outcome": "changed", "digest": DIGEST}
    result, records = published(arguments)
    assert result == {"failed": True, "msg": "the widget did not reach its postcondition; not proved: unit"}
    assert records == []
    result, records = published(dict(arguments, observed=True))
    assert result == {"changed": False}
    assert records == [({"phase": "completed", "outcome": "changed",
                         "evidence": {"postcondition": False, "request": DIGEST}}, False)]
    result, records = published(dict(arguments, proved=True))
    assert result == {"changed": False}
    assert records[0][0]["evidence"]["postcondition"] is True


def test_an_outcome_outside_the_two_is_refused():
    result, records = published({"phase": "completed", "outcome": "maybe", "digest": DIGEST, "proved": True})
    assert result == {"failed": True, "msg": "the widget capability result could not be published"}
    assert records == []


def test_unreached_names_unknown_when_nothing_is_named():
    assert adapter_protocol.unreached("the widget", "still present", []) == (
        "the widget did not reach its postcondition; still present: unknown")


def test_an_unpublishable_result_names_the_capability():
    result, records = published({"phase": "completed", "outcome": "changed", "digest": "bad", "proved": True})
    assert result == {"failed": True, "msg": "the widget capability result could not be published"}
    assert records == []

    def refused(record, acknowledge=False):
        raise OSError("closed")

    assert adapter_protocol.publish({"phase": "loaded"}, CAPABILITY, refused) == {
        "failed": True, "msg": "the widget capability result could not be published"}


def test_a_passthrough_refusal_keeps_its_own_message():
    result, records = published({"phase": "completed", "outcome": "changed", "digest": DIGEST, "unprovable": True})
    assert result == {"failed": True, "msg": "the controller reported a UUID longer than 128 characters"}
    assert records == []
    plain = adapter_protocol.Capability("widget", "unpublished", completion)
    assert published({"phase": "completed", "outcome": "changed", "digest": DIGEST, "unprovable": True}, plain) == (
        {"failed": True, "msg": "unpublished"}, [])


@pytest.mark.parametrize("value", ["0123456789ABCDEF" * 4, DIGEST[:63], DIGEST + "0", "z" * 64, "", None, 64])
def test_a_digest_that_is_not_64_lowercase_hex_is_refused(value):
    with pytest.raises(ValueError, match="digest"):
        adapter_protocol.request_digest(value)
    assert adapter_protocol.request_digest(DIGEST) == DIGEST
