"""Bind the controller stage's tool publication to its frozen request."""

from __future__ import annotations

import re

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)


def refusal_reason(error):
    """The refusal's own token, so one generic message still names a cause.

    Only the raised token is reported, never an operating-system message, so a
    path this stage happened to touch never reaches the result.
    """
    if isinstance(error, (ValueError, KeyError)) and error.args:
        return str(error.args[0])[:120]
    return type(error).__name__


def preparation(request):
    """The sources this run is about to add: exactly the frozen tools'."""
    sources = sorted({tool["source"]["id"] for tool in request["tools"]})
    if len(sources) != len(request["tools"]):
        raise ValueError("duplicate source")
    return {"addedSources": sources}


def frozen_request(request):
    """The frozen request's shape: its exact keys, version and identity, and
    one acquisition deadline per tool, in their order."""
    required = {
        "version",
        "identity",
        "publicationBundle",
        "tools",
        "acquisition",
        "egress",
    }
    if set(request) != required:
        raise ValueError("request")
    if (
        request["version"] != "controller-prerequisites-v6"
        or re.fullmatch(r"[a-f0-9]{64}", request["identity"]) is None
    ):
        raise ValueError("request")
    tools, acquisition = request["tools"], request["acquisition"]
    if not isinstance(tools, list) or not 1 <= len(tools) <= 128:
        raise ValueError("tools")
    if not isinstance(acquisition, list) or [
        entry["source"] for entry in acquisition
    ] != [tool["source"]["id"] for tool in tools]:
        raise ValueError("acquisition")
    for entry in acquisition:
        seconds = entry["seconds"]
        if (
            set(entry) != {"source", "seconds"}
            or not isinstance(seconds, int)
            or isinstance(seconds, bool)
            or not 1 <= seconds <= 7200
        ):
            raise ValueError("acquisition")
    return request


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _supports_check_mode = False
    _supports_async = False

    def run(self, tmp=None, task_vars=None):
        result = super().run(tmp, task_vars)
        result.update(changed=False, _ansible_no_log=True)
        try:
            args = self._task.args
            phase = args.get("phase")
            if phase in ("loaded", "continue"):
                if set(args) != {"phase"}:
                    raise ValueError("request")
                emit({"phase": phase}, acknowledge=True)
                return result
            expected_args = {"phase", "request"}
            if phase == "completed":
                expected_args |= {"preparation", "tools", "tools_changed"}
            if set(args) != expected_args:
                raise ValueError("request")
            request = frozen_request(args["request"])
            if phase == "prepared":
                proof = preparation(request)
                emit({"phase": "prepared", "preparation": proof}, acknowledge=True)
                return dict(result, preparation=proof)
            if phase == "completed":
                if args["preparation"] != preparation(request):
                    raise ValueError("preparation")
                if not isinstance(args["tools_changed"], bool):
                    raise ValueError("tools completion")
                if not isinstance(args["tools"], list) or len(args["tools"]) != len(
                    request["tools"]
                ):
                    raise ValueError("tools completion")
                evidence = {
                    "request": request["identity"],
                    "tools": args["tools"],
                    "postcondition": True,
                }
                emit(
                    {
                        "phase": "completed",
                        "outcome": "changed" if args["tools_changed"] else "unchanged",
                        "evidence": evidence,
                    }
                )
                return result
            raise ValueError("request")
        except (KeyError, TypeError, ValueError, OSError) as refusal:
            return dict(
                result,
                failed=True,
                msg="Controller dependency protocol or evidence was refused: %s"
                % refusal_reason(refusal),
            )
