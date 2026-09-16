"""A readiness probe that never answered has to say why it did not.

The class name alone reduced `[Errno 99] Cannot assign requested address` — a
host that does not hold the declared address — to the same word as a refused
port, and the two need opposite repairs.
"""

from __future__ import annotations

import socket

from ansible_collections.bootwright.core.plugins.module_utils import (
    artifact_server,
    infra_service,
)

PROBES = (artifact_server.probe_failure, infra_service.probe_failure)


def test_an_address_this_host_does_not_hold_is_named_as_such():
    for probe_failure in PROBES:
        reported = probe_failure(OSError(99, "Cannot assign requested address"))
        assert "99" in reported and "Cannot assign requested address" in reported


def test_a_refused_port_is_told_apart_from_an_absent_address():
    for probe_failure in PROBES:
        refused = probe_failure(ConnectionRefusedError(111, "Connection refused"))
        absent = probe_failure(OSError(99, "Cannot assign requested address"))
        assert refused != absent
        assert "Connection refused" in refused


def test_a_refusal_carrying_no_number_still_names_its_kind():
    for probe_failure in PROBES:
        assert probe_failure(socket.timeout()) == "TimeoutError"
        assert probe_failure(ValueError("status line")) == "ValueError: status line"


def test_a_reason_is_bounded():
    for probe_failure in PROBES:
        assert len(probe_failure(OSError(99, "x" * 500))) == artifact_server.PROBE_REASON


# The two module_utils are deliberately parallel copies, and a helper that
# drifts between them is exactly how a removed unit stayed `inactive` forever.
def test_both_copies_answer_identically():
    for error in (
        OSError(99, "Cannot assign requested address"),
        ConnectionRefusedError(111, "Connection refused"),
        socket.timeout(),
        ValueError("status line"),
    ):
        assert artifact_server.probe_failure(error) == infra_service.probe_failure(error)
    assert artifact_server.PROBE_REASON == infra_service.PROBE_REASON
