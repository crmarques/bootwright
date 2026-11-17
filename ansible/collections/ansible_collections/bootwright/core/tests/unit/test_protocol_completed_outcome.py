"""Every role that publishes a completion through the shared dispatch names its outcome.

The shared dispatch refuses a `completed` phase without `changed` or
`unchanged`, and under no_log that refusal reads only as the capability's
generic failure. The physical erase observation published none, so no unknown
erase could ever be resolved. The controller protocol derives its outcome from
the tools it installed and is the one exception.
"""

from __future__ import annotations

import pathlib

import yaml

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
DERIVES_ITS_OUTCOME = {"bootwright.core.controller_protocol"}


def tasks(node):
    if isinstance(node, list):
        for item in node:
            yield from tasks(item)
    elif isinstance(node, dict):
        yield node
        for key in ("block", "rescue", "always"):
            if key in node:
                yield from tasks(node[key])


def test_every_completed_publish_names_its_outcome():
    missing = []
    for path in sorted(ROLES.glob("*/tasks/*.yml")):
        for task in tasks(yaml.safe_load(path.read_text()) or []):
            for module, arguments in task.items():
                if not module.endswith("_protocol") or module in DERIVES_ITS_OUTCOME or not isinstance(arguments, dict):
                    continue
                outcome = str(arguments.get("outcome", ""))
                if arguments.get("phase") == "completed" and outcome not in ("changed", "unchanged") and "{{" not in outcome:
                    missing.append("%s: %s" % (path.relative_to(ROLES), task.get("name")))
    assert missing == []
