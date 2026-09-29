"""Each target tool runs under the acquisition deadline its request froze for it.

The role picks a tool's deadline out of the request by the tool's source. The
expression is read from the task file itself, not a copy, and rendered as
native templating renders it, so the module is shown whole seconds, not text.
"""

from __future__ import annotations

import pathlib

import yaml
from jinja2.nativetypes import NativeEnvironment

TASKS = (
    pathlib.Path(__file__).resolve().parents[2]
    / "roles/controller_prerequisites/tasks/tool.yml"
)
REQUEST = {
    "acquisition": [
        {"source": "tool-openshift-clients", "seconds": 205},
        {"source": "tool-kubectl", "seconds": 121},
    ]
}


def deadline_expression():
    calls = [
        task["bootwright.core.controller_tool"]
        for task in yaml.safe_load(TASKS.read_text())
        if "bootwright.core.controller_tool" in task
    ]
    assert len(calls) == 1
    return calls[0]["deadline"]


def test_each_tool_is_given_its_own_frozen_seconds():
    template = NativeEnvironment().from_string(deadline_expression())
    for source, seconds in (("tool-openshift-clients", 205), ("tool-kubectl", 121)):
        rendered = template.render(
            bootwright_controller_request=REQUEST, item={"source": {"id": source}}
        )
        assert rendered == seconds
        assert isinstance(rendered, int) and not isinstance(rendered, bool)
