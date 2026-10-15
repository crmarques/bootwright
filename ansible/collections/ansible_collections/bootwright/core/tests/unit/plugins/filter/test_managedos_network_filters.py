"""The convergence filters answer every input and never raise."""

from __future__ import annotations

import pytest

from ansible_collections.bootwright.core.plugins.filter.managedos_network import FilterModule

FILTERS = FilterModule().filters()
STATE = FILTERS["managedos_network_state"]
STEP = FILTERS["managedos_network_step"]

READ = (
    "nmstatectl=yes\nbootId=0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0\nunit=inactive\nrc=\n"
    "checkpoint=\nlisted=ao 0\nmarker=\nerror=\n"
)


@pytest.mark.parametrize("value", [None, 7, [], {}, b"bytes", "", "=", "\x00" * 10, READ + "x"])
def test_the_filters_never_raise(value):
    state = STATE(value)
    assert isinstance(state, dict) and state["valid"] is False and state["listed"] is None
    assert STEP(state, "a" * 64) == "unreadable"
    assert STEP(value, "a" * 64) == "unreadable"
    assert STEP({"valid": True, "marker": 3, "checkpoint": "x", "listed": 4}, None) == "unreadable"


def test_a_valid_read_steps():
    state = STATE(READ)
    assert state["valid"] is True and state["listed"] == []
    assert STEP(state, "a" * 64) == "apply"
