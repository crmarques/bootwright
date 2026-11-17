#!/usr/bin/python
# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)
"""Manage one authorized offline erase's private custody and receipt."""

from __future__ import annotations

DOCUMENTATION = r"""
module: physical_erase
short_description: Hold offline erase custody and verify its RAM worker receipt
version_added: "0.1.0"
description:
  - Operates on the frozen physical erase request through bounded phases.
  - Never opens a physical disk on the execution host.
options:
  phase:
    description: Fixed lifecycle phase.
    type: str
    required: true
    choices:
      - prepare
      - build
      - start
      - poll
      - ack
      - stop-proof
      - finish
      - observe
      - withdraw
  request:
    description: Frozen physical erase request.
    type: dict
    required: true
  digest:
    description: Frozen request digest.
    type: str
    required: true
  material:
    description: Private invocation paths and original identity.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""
EXAMPLES = r"""
- name: Read the durable erase receipt
  bootwright.core.physical_erase:
    phase: observe
    request: '{{ bootwright_physical_erase_request }}'
    digest: '{{ bootwright_physical_erase_digest }}'
    material: '{{ bootwright_physical_erase_material }}'
  no_log: true
"""
RETURN = r"""
answer:
  description: What one poll of the RAM worker found, C(complete), C(partial), C(answered) or C(unanswered).
  returned: when polled
  type: str
receipt:
  description: The complete receipt, once recorded.
  returned: when polled complete
  type: dict
progress:
  description: Each target's written plus read-back bytes and twice its coverage, or null when the worker's report is malformed.
  returned: when polled partial
  type: list
  elements: dict
cause:
  description: The closed failure class of a session that reached no worker.
  returned: when polled unanswered
  type: str
evidence:
  description: Bounded erase evidence; a retained erase's names C(retained) and carries no receipt.
  returned: when observed, finished or withdrawn
  type: dict
refused:
  description: The closed code of a refused effect phase.
  returned: when an effect phase refused
  type: str
detail:
  description: The bounded value the controller or worker reported for the refusal, or empty.
  returned: when an effect phase refused
  type: str
controller:
  description: The management controller's own printable failure line, or empty.
  returned: when an effect phase refused
  type: str
"""

import json
from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_controller as custody
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker

UNPROVED = "physical erase did not prove its bounded custody or receipt contract"
# The phases that act on custody, the RAM worker or the management controller,
# and of them those that reach the controller.
EFFECT_PHASES = ("prepare", "start", "poll", "ack", "stop-proof", "finish", "withdraw")
CONTROLLED = ("start", "stop-proof")
# Each internal refusal the runner names, by the closed code its diagnostic
# carries; the custody drift codes share one code and one remedy.
NAMED = {
    code: code
    for code in (
        "machine-running", "identity-mismatch", "hardware-mismatch", "target-unanswered", "controller-refused",
        "controller-deadline", "poweroff-unproved", "worker-failed", "worker-unreachable", "foreign-worker", "receipt-timeout",
        "foreign-custody",
    )
}
NAMED.update({
    "foreign-target": "foreign-custody",
    "custody-policy-drift": "custody-drift",
    "custody-token": "custody-drift",
    "completion-drift": "custody-drift",
})
# What the image build alone refuses: a boot media that no longer matches its
# pin, an install network line its boot arguments cannot carry, and an image
# mkksiso did not build or that was not copied in full.
BUILT = {
    "media-identity": "boot-media-mismatch",
    "install-network": "install-network-unsupported",
    "native-command": "image-build-failed",
    "native-timeout": "image-build-failed",
    "native-output": "image-build-failed",
    "image-copy": "image-build-failed",
}
# The phases that name a refusal instead of failing: every effect phase, the
# image build and the read-only observation.
NAMING_PHASES = EFFECT_PHASES + ("build", "observe")
UNPREPARED = {"prepared": False, "removed": False}


def main():
    module = AnsibleModule(
        argument_spec={
            "phase": {"type": "str", "required": True, "choices": ["prepare", "build", "start", "poll", "ack", "stop-proof", "finish", "observe", "withdraw"]},
            "request": {"type": "dict", "required": True},
            "digest": {"type": "str", "required": True},
            "material": {"type": "dict", "required": True, "no_log": True},
        },
        supports_check_mode=False,
    )
    controller = None
    try:
        phase = module.params["phase"]
        # A retained erase reaches only its custody's withdrawal and its
        # read-only observation; withdrawal serves no other removal.
        waived = custody.relaxation(module.params["material"]) == custody.RETAIN
        if (phase == "withdraw") != waived and not (waived and phase == "observe"):
            raise worker.Refusal("relaxation")
        if waived:
            try:
                controller = custody.Controller(module.params["request"], module.params["digest"], module.params["material"])
            except FileNotFoundError:
                controller = None
            module.exit_json(**retention(controller, phase, module.params["digest"]))
        controller = custody.Controller(module.params["request"], module.params["digest"], module.params["material"], create=phase == "prepare")
        if phase == "prepare":
            module.exit_json(changed=True, **controller.prepare())
        if phase == "build":
            controller.build()
            module.exit_json(changed=True)
        if phase in ("start", "stop-proof"):
            controller.control(phase)
            module.exit_json(changed=True)
        if phase == "poll":
            module.exit_json(changed=True, **controller.poll())
        if phase == "ack":
            module.exit_json(changed=controller.acknowledge())
        if phase == "observe" and controller.earlier():
            # Another removal's custody is never this removal's evidence; its
            # prepare sets that custody aside before erasing again.
            module.exit_json(changed=False, evidence=dict(UNPREPARED, version=worker.VERSION, request=module.params["digest"]), finalizing=False)
        receipt = controller.receipt()
        evidence = {"version": worker.VERSION, "request": module.params["digest"], "prepared": True, "removed": False}
        if receipt:
            evidence["receipt"] = receipt
        path = controller.root / "completed.json"
        finalizing = controller.root / "finalizing.json"
        if phase == "finish":
            if not receipt:
                raise worker.Refusal("receipt-missing")
            evidence["removed"] = True
            controller.finish(evidence)
        elif path.exists():
            stored = json.loads(custody.read(path))
            evidence["removed"] = True
            if stored != evidence:
                raise worker.Refusal("completion-drift")
        elif finalizing.exists():
            stored = json.loads(custody.read(finalizing))
            if stored != dict(evidence, removed=True):
                raise worker.Refusal("completion-drift")
        module.exit_json(changed=phase == "finish", evidence=evidence, finalizing=finalizing.exists())
    except FileNotFoundError:
        if module.params["phase"] == "observe":
            module.exit_json(
                changed=False, evidence={"version": worker.VERSION, "request": module.params["digest"], "prepared": False, "removed": False}, finalizing=False
            )
        module.fail_json(msg="physical erase custody is incomplete")
    except worker.Refusal as refusal:
        code = BUILT.get(str(refusal)) if module.params["phase"] == "build" else None
        refuse(module, code or NAMED.get(str(refusal)), getattr(refusal, "detail", ""), getattr(refusal, "line", ""))
    except custody.redfish.redfish_control.ControllerError as failure:
        refuse(module, "target-unanswered" if module.params["phase"] in CONTROLLED else None, "", str(failure))
    except (OSError, ValueError, KeyError):
        module.fail_json(msg=UNPROVED)
    finally:
        if controller is not None:
            controller.close()


def retention(controller, phase, digest):
    """A retained erase's withdraw or observe answer, reached without its worker or management controller.

    No custody of this block's apply operation leaves nothing to withdraw.
    The withdraw phase withdraws what custody has left and answers the
    retention; the observe phase changes nothing and answers the retention
    only once that withdrawal is proved, otherwise an unfinished removal.
    """
    retained = {"version": worker.VERSION, "request": digest, "prepared": True, "removed": True, "retained": True}
    if controller is None:
        return {"changed": False, "evidence": retained, "finalizing": False}
    if phase == "withdraw":
        return {"changed": controller.withdraw(), "evidence": retained, "finalizing": False}
    if controller.withdrawn():
        return {"changed": False, "evidence": retained, "finalizing": False}
    return {"changed": False, "evidence": {"version": worker.VERSION, "request": digest, "prepared": True, "removed": False}, "finalizing": False}


def refuse(module, code, detail, line):
    """Return a named refusal for its role to report, or fail with the generic message.

    An effect phase returns its refusal instead of failing, because a failure
    under no_log keeps nothing of its cause: the role names the closed code to
    the runner and stops outside no_log with the code and the controller's own
    printable line. A refusal outside this vocabulary fails as before.
    """
    if code is None or module.params["phase"] not in NAMING_PHASES:
        module.fail_json(msg=UNPROVED)
    module.exit_json(changed=module.params["phase"] != "observe", refused=code, detail=detail, controller=line)


if __name__ == "__main__":
    main()
