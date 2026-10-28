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


PREPARING = adapter_protocol.Capability(
    "widget",
    "the widget capability result could not be published",
    completion,
    phases=adapter_protocol.PREPARING_PHASES,
    preparation=lambda arguments: {"marker": str(arguments.get("marker"))},
)


def test_prepared_waits_for_the_acknowledgement():
    result, records = published({"phase": "prepared", "marker": "m-1"}, PREPARING)
    assert result == {"changed": False}
    assert records == [({"phase": "prepared", "preparation": {"marker": "m-1"}}, True)]

    def unacknowledged(record, acknowledge=False):
        assert acknowledge
        raise ValueError("authorization")
    result = adapter_protocol.publish({"phase": "prepared", "marker": "m-1"}, PREPARING, unacknowledged)
    assert result == {"failed": True, "msg": "the widget capability result could not be published"}


def test_prepared_without_a_capability_preparation_fails():
    unprepared = adapter_protocol.Capability(
        "widget", "the widget capability result could not be published", completion, phases=adapter_protocol.PREPARING_PHASES,
    )
    result, records = published({"phase": "prepared", "marker": "m-1"}, unprepared)
    assert result == {"failed": True, "msg": "the widget capability result could not be published"}
    assert records == []
    assert published({"phase": "prepared", "marker": "m-1"})[0] == {"failed": True, "msg": "unsupported widget protocol phase"}


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


def warned(entries):
    return adapter_protocol.Capability("widget", "the widget capability result could not be published", completion,
                                       warnings=lambda arguments: entries)


COMPLETED = {"phase": "completed", "outcome": "changed", "digest": DIGEST, "proved": True}
SHADOW = "substrate.shadowed-route"


def test_a_completion_carries_its_registered_warnings_clipped_and_marked():
    assert "warnings" not in published(COMPLETED)[1][0][0]
    assert "warnings" not in published(COMPLETED, warned([]))[1][0][0]
    entries = [{"code": SHADOW, "message": "route via \u00e9th0\n"},
               {"code": SHADOW, "message": "x" * 2000, "object": {"kind": "InfraProvider", "name": "lab"}}]
    entries += [{"code": SHADOW, "message": "more"}] * 20
    result, records = published(COMPLETED, warned(entries))
    assert result == {"changed": False}
    reported = records[0][0]["warnings"]
    assert len(reported) == adapter_protocol.MAX_WARNINGS
    assert reported[0] == {"code": SHADOW, "message": "route via ?th0?"}
    assert reported[1] == {"code": SHADOW, "message": "x" * (adapter_protocol.MAX_WARNING_MESSAGE - 3) + "...",
                           "object": {"kind": "InfraProvider", "name": "lab"}}
    assert reported[-1]["message"] == "more (6 more not reported)"
    assert all(len(entry["message"]) <= adapter_protocol.MAX_WARNING_MESSAGE for entry in reported)


@pytest.mark.parametrize("entry", [
    {"code": "substrate.unregistered", "message": "m"},
    {"code": SHADOW, "message": ""},
    {"code": SHADOW, "message": "   "},
    {"code": SHADOW, "message": 1},
    {"code": SHADOW, "message": "m", "remedy": "r"},
    {"code": SHADOW, "message": "m", "object": {"kind": "InfraProvider"}},
    {"code": SHADOW, "message": "m", "object": {"kind": "infraProvider", "name": "lab"}},
    {"code": SHADOW, "message": "m", "object": {"kind": "Infra-Provider", "name": "lab"}},
    {"code": SHADOW, "message": "m", "object": {"kind": "K" * 64, "name": "lab"}},
    {"code": SHADOW, "message": "m", "object": {"kind": "InfraProvider", "name": "Lab"}},
    {"code": SHADOW, "message": "m", "object": {"kind": "InfraProvider", "name": "-lab"}},
    {"code": SHADOW, "message": "m", "object": {"kind": "InfraProvider", "name": "lab."}},
    {"code": SHADOW, "message": "m", "object": {"kind": "InfraProvider", "name": "a" * 254}},
    {"code": SHADOW, "message": "m", "object": None},
    SHADOW,
])
def test_a_warning_the_decoder_would_refuse_is_dropped_and_the_completion_still_published(entry):
    result, records = published(COMPLETED, warned([entry, {"code": SHADOW, "message": "kept"}]))
    assert result == {"changed": False}
    assert records[0][0]["warnings"] == [{"code": SHADOW, "message": "kept (1 more not reported)"}]
    result, records = published(COMPLETED, warned([entry]))
    assert result == {"changed": False} and "warnings" not in records[0][0]


def test_the_widest_object_the_decoder_admits_is_carried():
    named = {"kind": "K" * 63, "name": "a" * 253}
    records = published(COMPLETED, warned([{"code": SHADOW, "message": "m", "object": named}]))[1]
    assert records[0][0]["warnings"][0]["object"] == named


def raising(arguments):
    raise RuntimeError("the capability's warning builder failed")


@pytest.mark.parametrize("builder", [raising, lambda arguments: None, lambda arguments: {"code": SHADOW, "message": "m"},
                                     lambda arguments: "substrate.shadowed-route"])
def test_a_warning_builder_that_fails_yields_no_warnings_and_never_fails_the_completion(builder):
    capability = adapter_protocol.Capability("widget", "unpublished", completion, warnings=builder)
    result, records = published(COMPLETED, capability)
    assert result == {"changed": False}
    assert records[0][0] == {"phase": "completed", "outcome": "changed", "evidence": evidence(COMPLETED)}


def test_warnings_that_would_overflow_the_line_are_left_out():
    big = {"padding": "x" * 60000}

    def padded(arguments):
        return dict(evidence(arguments), **big), None

    capability = adapter_protocol.Capability("widget", "unpublished", padded,
                                             warnings=lambda arguments: [{"code": SHADOW, "message": "y" * 1000}] * 16)
    result, records = published(COMPLETED, capability)
    assert result == {"changed": False}
    assert "warnings" not in records[0][0] and records[0][0]["evidence"]["padding"] == big["padding"]
