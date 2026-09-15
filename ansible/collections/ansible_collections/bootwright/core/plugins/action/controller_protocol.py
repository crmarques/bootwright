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
            request = args["request"]
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
                "egress",
            }
            if not required <= set(request) or set(request) - required - {
                "preparation"
            }:
                raise ValueError("request")
            if (
                request["version"] != "controller-prerequisites-v3"
                or request["operation"] not in ("setup", "recover")
                or re.fullmatch(r"[a-f0-9]{64}", request["identity"]) is None
            ):
                raise ValueError("request")
            if not isinstance(request["tools"], list) or len(request["tools"]) > 128:
                raise ValueError("tools")
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
        except (KeyError, TypeError, ValueError, OSError):
            return dict(
                result,
                failed=True,
                msg="Controller dependency protocol or evidence was refused.",
            )
