"""Each target tool runs under the acquisition deadline its request froze for it.

The role hands the action every tool and the request's acquisition list, and
the action pairs each tool with its own entry by source. The expressions are
read from the task file itself, not a copy, and rendered as native templating
renders them, so the action is shown whole seconds, not text.
"""

from __future__ import annotations

import pathlib

import pytest
import yaml
from jinja2.nativetypes import NativeEnvironment

from ansible_collections.bootwright.core.plugins.action.controller_tool import (
    tool_arguments,
)

TASKS = (
    pathlib.Path(__file__).resolve().parents[2]
    / "roles/controller_prerequisites/tasks/main.yml"
)
TOOLS = [
    {"source": {"id": "tool-openshift-clients"}},
    {"source": {"id": "tool-kubectl"}},
]
REQUEST = {
    "publicationBundle": {},
    "tools": TOOLS,
    "egress": {},
    "trust": {"host": "", "file": ""},
    "acquisition": [
        {"source": "tool-openshift-clients", "seconds": 205},
        {"source": "tool-kubectl", "seconds": 121},
    ],
}


def rendered_arguments():
    calls = [
        task["bootwright.core.controller_tool"]
        for task in yaml.safe_load(TASKS.read_text())
        if "bootwright.core.controller_tool" in task
    ]
    assert len(calls) == 1
    environment = NativeEnvironment()
    return {
        key: environment.from_string(value).render(bootwright_controller_request=REQUEST)
        if isinstance(value, str)
        else value
        for key, value in calls[0].items()
    }


def test_each_tool_is_given_its_own_frozen_seconds():
    _bundle, tools, _egress, deadlines, inspect_only, _trust = tool_arguments(rendered_arguments())
    assert tools == TOOLS and inspect_only is False
    assert deadlines == [205, 121]
    assert all(isinstance(seconds, int) and not isinstance(seconds, bool) for seconds in deadlines)


@pytest.mark.parametrize("acquisition", [
    [{"source": "tool-kubectl", "seconds": 121}, {"source": "tool-openshift-clients", "seconds": 205}],
    [{"source": "tool-openshift-clients", "seconds": 205}],
    [{"source": "tool-openshift-clients", "seconds": 205}, {"source": "tool-kubectl", "seconds": 121, "bytes": 1}],
])
def test_an_acquisition_list_out_of_the_tools_order_is_refused(acquisition):
    with pytest.raises(ValueError):
        tool_arguments(dict(rendered_arguments(), acquisition=acquisition))


def test_one_continue_authorizes_every_tool_in_one_call():
    tasks = yaml.safe_load(TASKS.read_text())
    modules = [next(key for key in task if key.startswith(("bootwright.", "ansible."))) for task in tasks]
    continues = [index for index, task in enumerate(tasks)
                 if (task.get("bootwright.core.controller_protocol") or {}).get("phase") == "continue"]
    assert len(continues) == 1
    assert modules[continues[0] + 1] == "bootwright.core.controller_tool"
    assert modules.count("bootwright.core.controller_tool") == 1
    assert not any(key in task for task in tasks for key in ("loop", "with_items", "ansible.builtin.include_tasks"))
    assert not (TASKS.parent / "tool.yml").exists()
