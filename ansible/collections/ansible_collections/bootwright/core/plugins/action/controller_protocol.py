"""Bind Ansible effects and recovery to one frozen dependency transition plan."""

from __future__ import annotations

import hashlib
import json
import re

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)


def canonical(value):
    return json.dumps(
        value, sort_keys=True, separators=(",", ":"), ensure_ascii=True
    ).encode()


def digest(value):
    return hashlib.sha256(canonical(value)).hexdigest()


def inventory(value):
    if not isinstance(value, list) or len(value) > 32768:
        raise ValueError("inventory")
    for package in value:
        if set(package) != {"name", "epoch", "version", "release", "architecture"}:
            raise ValueError("inventory identity")
        if (
            not isinstance(package["epoch"], int)
            or isinstance(package["epoch"], bool)
            or not 0 <= package["epoch"] <= 1 << 30
        ):
            raise ValueError("inventory epoch")
        for key in ("name", "version", "release", "architecture"):
            if (
                not isinstance(package[key], str)
                or re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._+~^\-]{0,127}", package[key])
                is None
            ):
                raise ValueError("inventory identity")
    ordered = sorted(value, key=canonical)
    if len({canonical(item) for item in ordered}) != len(ordered):
        raise ValueError("inventory duplicate")
    return ordered


def refusal_reason(error):
    """The refusal's own token, so one generic message still names a cause.

    Only the raised token is reported, never an operating-system message, so a
    path this stage happened to touch never reaches the result.
    """
    if isinstance(error, (ValueError, KeyError)) and error.args:
        return str(error.args[0])[:120]
    return type(error).__name__


def preparation(request, observed):
    plan = request["native"]
    before = digest(observed)
    value = {"inventorySHA256": before}
    sources = {tool["source"]["id"] for tool in request["tools"]}
    if plan is not None:
        if not isinstance(plan, dict) or len(plan["actions"]) > 512:
            raise ValueError("native plan")
        unsigned = {key: item for key, item in plan.items() if key != "digest"}
        if (
            digest(unsigned) != plan["digest"]
            or plan["platform"] != request["platform"]
            or plan["packages"] != request["packages"]
        ):
            raise ValueError("native identity")
        value.update(
            inventorySHA256=plan["beforeSHA256"],
            afterInventorySHA256=plan["afterSHA256"],
            planDigest=plan["digest"],
            transitionsSHA256=digest(plan["actions"]),
        )
        sources.update(action["sourceID"] for action in plan["actions"])
        expected = {plan["beforeSHA256"]}
        if request["operation"] == "recover":
            expected.add(plan["afterSHA256"])
        if before not in expected:
            raise ValueError("native inventory changed")
    elif request["packages"]:
        raise ValueError("unplanned native sources")
    value["addedSources"] = sorted(sources)
    if len(sources) > 512:
        raise ValueError("source limit")
    if request["operation"] == "recover" and value != request["preparation"]:
        raise ValueError("recovery proof")
    return value


def frozen_request(request):
    """The frozen request's shape: its exact keys, version, operation, identity
    and tools, one acquisition deadline per native package then per tool, in
    their order, and the native staging bound, zero exactly when no package is
    staged."""
    required = {
        "version",
        "operation",
        "identity",
        "platform",
        "bundle",
        "publicationBundle",
        "packages",
        "native",
        "tools",
        "acquisition",
        "nativeStaging",
        "egress",
    }
    if not required <= set(request) or set(request) - required - {"preparation"}:
        raise ValueError("request")
    if (
        request["version"] != "controller-prerequisites-v5"
        or request["operation"] not in ("setup", "recover")
        or re.fullmatch(r"[a-f0-9]{64}", request["identity"]) is None
    ):
        raise ValueError("request")
    packages, tools = request["packages"], request["tools"]
    acquisition, staging = request["acquisition"], request["nativeStaging"]
    if not isinstance(tools, list) or len(tools) > 128:
        raise ValueError("tools")
    if not isinstance(packages, list) or len(packages) > 512:
        raise ValueError("packages")
    if (
        not isinstance(staging, int)
        or isinstance(staging, bool)
        or not 0 <= staging <= 7200
        or (staging == 0) != (not packages)
    ):
        raise ValueError("native staging")
    if not isinstance(acquisition, list) or [
        entry["source"] for entry in acquisition
    ] != [package["source"]["id"] for package in packages] + [
        tool["source"]["id"] for tool in tools
    ]:
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
            expected_args = {"phase", "request", "inventory", "roots_ready"}
            if phase == "completed":
                expected_args |= {
                    "preparation",
                    "native_applied",
                    "tools",
                    "tools_changed",
                }
            if set(args) != expected_args:
                raise ValueError("request")
            request = frozen_request(args["request"])
            observed = inventory(args["inventory"])
            if phase == "prepared":
                proof = preparation(request, observed)
                if request["operation"] == "setup":
                    emit({"phase": "prepared", "preparation": proof}, acknowledge=True)
                apply_native = bool(
                    request["native"]
                    and request["native"]["actions"]
                    and digest(observed) == proof["inventorySHA256"]
                )
                if (
                    request["operation"] == "recover"
                    and not apply_native
                    and args["roots_ready"] is not True
                ):
                    raise ValueError("recovery native roots")
                return dict(result, preparation=proof, apply_native=apply_native)
            if phase == "completed":
                proof = args["preparation"]
                if (
                    digest(observed)
                    != proof.get("afterInventorySHA256", proof["inventorySHA256"])
                    or args["roots_ready"] is not True
                ):
                    raise ValueError("native postcondition")
                actions = request["native"]["actions"] if request["native"] else []
                if not isinstance(args["native_applied"], bool):
                    raise ValueError("native completion")
                changed = args["native_applied"] or bool(args["tools_changed"])
                evidence = {
                    "request": request["identity"],
                    "before": proof["inventorySHA256"],
                    "after": digest(observed),
                    "planDigest": proof.get("planDigest", ""),
                    "added": (
                        sorted(action["sourceID"] for action in actions)
                        if args["native_applied"]
                        else []
                    ),
                    "tools": args["tools"],
                    "postcondition": True,
                }
                emit(
                    {
                        "phase": "completed",
                        "outcome": "changed" if changed else "unchanged",
                        "evidence": evidence,
                    }
                )
                return result
            raise ValueError("request")
        except (KeyError, TypeError, ValueError, OSError) as refusal:
            return dict(
                result,
                failed=True,
                msg="Controller dependency protocol or evidence was refused: %s" % refusal_reason(refusal),
            )
