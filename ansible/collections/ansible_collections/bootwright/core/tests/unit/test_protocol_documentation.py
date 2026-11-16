"""A protocol module's documented refusals are the reasons its action accepts.

Each plugins/modules/<name>_protocol.py holds documentation only; its action
plugin plugins/action/<name>_protocol.py is what publishes. Nothing validates
the action's arguments against that documentation, so a copied list drifts
silently (X06-01): a reader and ansible-doc see the reasons the stub names, not
the ones the action accepts. This ties the two for every pair, and the pairing
itself, so a new protocol joins the check by its name. A module that documents
no reason choices makes no claim to check.
"""

from __future__ import annotations

import importlib
import pathlib

import pytest
import yaml

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
PLUGINS = COLLECTION / "plugins"
PACKAGE = "ansible_collections.bootwright.core.plugins"
NAMES = sorted(path.stem for path in (PLUGINS / "modules").glob("*_protocol.py") if (PLUGINS / "action" / path.name).is_file())


def documented_reasons(documentation):
    reason = yaml.safe_load(documentation)["options"].get("reason")
    return None if reason is None else reason.get("choices")


def accepted_reasons(capability):
    return capability.refusals


def disagreement(documentation, capability):
    """What the documentation names wrongly or omits against the reasons the action accepts, or None."""
    documented, accepted = documented_reasons(documentation), accepted_reasons(capability)
    if documented is None:
        return None
    if callable(accepted):
        return "the action derives its reason from the arguments, so the documentation cannot name choices"
    if len(set(documented)) != len(documented):
        return f"the documentation repeats a reason: {sorted(name for name in set(documented) if documented.count(name) > 1)}"
    if set(documented) == set(accepted):
        return None
    return f"documented only {sorted(set(documented) - set(accepted))}; accepted only {sorted(set(accepted) - set(documented))}"


def load(name):
    action = importlib.import_module(f"{PACKAGE}.action.{name}")
    module = importlib.import_module(f"{PACKAGE}.modules.{name}")
    return module.DOCUMENTATION, getattr(action, "CAPABILITY", None)


DOCUMENTING = [name for name in NAMES if documented_reasons(load(name)[0]) is not None]


def test_every_protocol_module_is_found_with_its_action():
    assert len(NAMES) >= 27
    assert {"ceph_cluster_protocol", "ceph_export_protocol", "ceph_image_protocol", "ceph_replication_protocol", "ceph_smb_protocol"} <= set(DOCUMENTING)
    assert not [path.stem for path in (PLUGINS / "action").glob("*_protocol.py") if not (PLUGINS / "modules" / path.name).is_file()]


@pytest.mark.parametrize("name", DOCUMENTING)
def test_the_documented_reasons_are_the_ones_the_action_accepts(name):
    documentation, capability = load(name)
    assert disagreement(documentation, capability) is None


def test_a_documented_reason_the_action_does_not_accept_is_found():
    capability = type("Planted", (), {"refusals": ("transport", "evidence")})
    assert disagreement("options:\n  reason:\n    choices: [transport, evidence]\n", capability) is None
    assert "foreign-service" in disagreement("options:\n  reason:\n    choices: [transport, evidence, foreign-service]\n", capability)
    wider = type("Planted", (), {"refusals": ("transport", "evidence", "unknown")})
    assert "unknown" in disagreement("options:\n  reason:\n    choices: [transport, evidence]\n", wider)
    assert "cannot name" in disagreement("options:\n  reason:\n    choices: [transport]\n", type("Planted", (), {"refusals": len}))
    assert disagreement("options:\n  phase: {}\n", capability) is None
    assert "repeats" in disagreement("options:\n  reason:\n    choices: [transport, transport, evidence]\n", capability)
    assert disagreement("options:\n  phase: {}\n", type("Planted", (), {"refusals": ()})) is None
