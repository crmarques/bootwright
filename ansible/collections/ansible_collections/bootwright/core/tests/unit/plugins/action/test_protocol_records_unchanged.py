"""Every protocol plugin hands the runner the records and results its golden holds.

The plugins share one dispatch, so a change to it reaches every capability at
once. Each plugin's ActionModule.run is driven here over one fixed table of
arguments, with the channel recorded and the base class's own run stubbed as
test_every_action_runs.py does, and the (records, result) pair of each case is
compared with the golden under tests/unit/goldens/protocol_records. A golden
holds the records in the order and with the keys the plugin built them, so a
key, its order or a message that moves fails here. To rewrite the goldens after
an intended change, run this module with BOOTWRIGHT_UPDATE_GOLDENS=1.
"""

from __future__ import annotations

import copy
import importlib
import json
import os
import pathlib
from types import SimpleNamespace

import pytest
from ansible.plugins.action import ActionBase

GOLDENS = pathlib.Path(__file__).resolve().parents[2] / "goldens" / "protocol_records"
PACKAGE = "ansible_collections.bootwright.core.plugins.action."
UPDATE = os.environ.get("BOOTWRIGHT_UPDATE_GOLDENS") == "1"

DIGEST = "0123456789abcdef" * 4
STATUSES = ("running", "ok", "failed", "skipped")


def completed(*sources, **fields):
    """One completion's arguments: the defaults, then each source, then fields."""
    arguments = {"phase": "completed", "outcome": "changed", "digest": DIGEST}
    for source in sources:
        arguments.update(source)
    arguments.update(fields)
    return arguments


def common(refusals):
    """The cases every plugin answers alike: loaded, each group status, an
    unknown phase, a bad status and a bad outcome, and each refusal reason the
    plugin admits plus one it does not."""
    cases = [("loaded", {"phase": "loaded"})]
    cases += [("group " + status, {"phase": "group", "group": "Network", "status": status}) for status in STATUSES]
    cases += [
        ("unknown phase", {"phase": "unloaded"}),
        ("bad status", {"phase": "group", "group": "Network", "status": "done"}),
        ("bad outcome", completed(outcome="maybe")),
    ]
    cases += [("refused " + " ".join(str(value) for value in reason.values()), dict({"phase": "refused"}, **reason))
              for reason in refusals]
    cases.append(("refused unnamed", {"phase": "refused", "reason": "not-a-refusal"}))
    return cases


RUNNING = {"unit": "active", "contentRoot": True, "startedAfterFiles": True, "container": "quay.io/x"}
ANSWERS = [{"address": "192.0.2.1", "answer": "192.0.2.10", "port": 53}]
LISTENERS = [{"address": "192.0.2.1", "fingerprint": "", "name": "http", "port": 80, "protocol": "http", "status": "200"}]
NETWORK = {"answered": True, "autostart": True, "bridge": True, "definition": True, "managed": True, "name": "n",
           "owned": True, "state": "active", "uuid": "u"}
SERVICE = {"enabled": True, "name": "virtnetworkd.service", "state": "active"}
HOST = {"directory": True, "hypervisor": True, "networks": [NETWORK], "pool": "active", "poolAnswered": True,
        "poolAutostart": True, "poolOwned": True, "services": [SERVICE], "uri": True}
HOST_GONE = dict(HOST, pool="", networks=[dict(NETWORK, state="", owned=False)], directory=False)
DISK = {"name": "root", "present": True, "sizeGiB": 60}
MACHINE = {"answered": True, "controller": "quay.io/x@sha256:" + "0" * 64, "disks": [DISK],
           "domain": "bootwright-lab-01", "listener": True, "owned": True, "state": "shut off", "unit": "active"}
MACHINE_GONE = {"answered": True, "controller": "", "disks": [], "domain": "", "listener": False, "owned": False,
                "state": "", "unit": ""}
INSTALL = {"observation": {"image": True, "tree": True}, "marker": '{"context":"lab"}', "hostKey": "ssh-ed25519 AAAA",
           "address": "198.51.100.11", "media": "", "power": "On", "reachable": True}
CLUSTER = {"cluster": "c" * 64, "completed": True, "media": [], "missing": [], "ownMedia": [], "powered": ["sno-01"],
           "release": "4.21.15"}
PHYSICAL = {"addresses": ["aa:bb:cc:dd:ee:01"], "failures": [], "manufacturer": "Acme", "model": "R740", "power": "Off",
            "serial": "SN1", "uuid": "uuid-1"}
ENDPOINT = "https://bmc.example.test/redfish/v1/Systems/1"
POWER = {"machine": "node-a", "verb": "start", "power": "On", "previous": "Off", "changed": True}
POWER_UNMET = dict(POWER, power="Off")
POWER_MALFORMED = dict(POWER, verb="bounce")
CONTROLLER_REQUEST = {"version": "controller-prerequisites-v5", "operation": "setup", "identity": DIGEST,
                      "platform": "rhel-9-x86_64", "bundle": "b", "publicationBundle": "p", "packages": [],
                      "native": None, "tools": [], "acquisition": [], "nativeStaging": 0, "egress": {}}
CONTROLLER_PREPARATION = {"inventorySHA256": "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945",
                          "addedSources": []}


def completions(proving, unproving, removal, remaining, malformed):
    """The completion cases of a plugin with the observed allowance."""
    return [
        ("completed proving", completed(**proving)),
        ("completed unproving", completed(**unproving)),
        ("completed unproving observed", completed(observed=True, **unproving)),
        ("removal", completed(removed=True, **removal)),
        ("removal still present", completed(removed=True, **remaining)),
        ("malformed", completed(**malformed)),
    ]


CASES = {
    "infra_service_protocol": common([{"reason": "foreign-listener", "port": 53}]) + completions(
        {"observation": RUNNING, "answers": ANSWERS},
        {"observation": dict(RUNNING, unit="inactive"), "answers": ANSWERS},
        {"observation": {}},
        {"observation": {"unit": "active", "contentRoot": True}},
        {"observation": RUNNING, "answers": ANSWERS, "digest": "bad"},
    ),
    "artifact_server_protocol": common([{"reason": "foreign-listener", "port": 8443}]) + completions(
        {"observation": RUNNING, "listeners": LISTENERS},
        {"observation": dict(RUNNING, contentRoot=False), "listeners": LISTENERS},
        {"observation": {}},
        {"observation": {"container": "quay.io/x"}},
        {"observation": RUNNING, "listeners": [dict(LISTENERS[0], protocol="ftp")]},
    ),
    "containercluster_media_protocol": common([]) + completions(
        {"observation": {"image": True, "inputs": DIGEST, "installer": "4.21.15", "work": False}},
        {"observation": {"image": False, "inputs": "x", "installer": "", "work": True}},
        {"observation": {}},
        {"observation": {"image": True, "work": True}},
        {"observation": {"image": True, "inputs": "x" * 129}},
    ),
    "substrate_host_protocol": common([]) + completions(
        {"observation": HOST},
        {"observation": dict(HOST, uri=False, hypervisor=False)},
        {"observation": HOST_GONE},
        {"observation": dict(HOST_GONE, pool="active", directory=True)},
        {"observation": dict(HOST, directory="yes")},
    ) + [
        ("removal unanswered", completed(removed=True, observation=dict(HOST_GONE, uri=False))),
        ("removal driver silent", completed(removed=True, observation=dict(HOST_GONE, poolAnswered=False))),
        ("observed gone", completed(observed=True, observation=HOST_GONE)),
        ("removal observed", completed(removed=True, observed=True, observation=HOST_GONE)),
    ],
    "substrate_machine_protocol": common([]) + completions(
        {"observation": MACHINE, "power": "Off", "system": "uuid"},
        {"observation": dict(MACHINE, unit="failed", owned=False), "power": "Off", "system": ""},
        {"observation": MACHINE_GONE},
        {"observation": dict(MACHINE_GONE, domain="d", listener=True)},
        {"observation": dict(MACHINE, state="bananas"), "power": "Off", "system": "uuid"},
    ) + [
        ("removal unanswered", completed(removed=True, observation=dict(MACHINE_GONE, answered=False))),
        ("observed gone", completed(observed=True, observation=MACHINE_GONE)),
        ("removal observed", completed(removed=True, observed=True, observation=MACHINE_GONE)),
    ],
    "managedos_install_protocol": common([
        {"reason": "hardware-mismatch"}, {"reason": "identity-mismatch"}, {"reason": "machine-running"},
        {"reason": "media-changed-boot"}, {"reason": "media-changed-tree"},
    ]) + completions(
        INSTALL,
        dict(INSTALL, marker="", power="Off", media="http://s/install.iso"),
        {"observation": {}},
        {"observation": {"image": True, "treeStaging": True}},
        dict(INSTALL, power="Spinning"),
    ),
    "containercluster_install_protocol": common([
        {"reason": "hardware-mismatch", "node": 0}, {"reason": "identity-mismatch", "node": "2"},
        {"reason": "machine-running", "node": 999}, {"reason": "machine-running", "node": 1000},
    ]) + completions(
        {"identity": "c" * 64, "state": CLUSTER},
        {"identity": "d" * 64, "state": dict(CLUSTER, completed="true", missing=["sno-01"], media=["sno-01"])},
        {"state": {"media": []}},
        {"state": {"media": ["sno-02", "sno-01"]}},
        {"identity": "c" * 64, "state": dict(CLUSTER, release="x" * 129)},
    ),
    "substrate_physical_protocol": common([]) + [
        ("completed proving", completed(endpoint=ENDPOINT, observation=PHYSICAL, expected=["aa:bb:cc:dd:ee:01"])),
        ("completed unproving", completed(endpoint=ENDPOINT, observation=dict(PHYSICAL, failures=["x"], power=""),
                                          expected=["aa:bb:cc:dd:ee:02"])),
        ("completed unproving observed", completed(observed=True, endpoint=ENDPOINT, observation=dict(PHYSICAL, uuid="",
                                                                                                      serial=""),
                                                   expected=["aa:bb:cc:dd:ee:01"])),
        ("removal", completed(released=True)),
        ("malformed", completed(endpoint="", observation=PHYSICAL, expected=["aa:bb:cc:dd:ee:01"])),
        ("unprovable identity", completed(endpoint=ENDPOINT, observation=dict(PHYSICAL, uuid="u" * 129),
                                          expected=["aa:bb:cc:dd:ee:01"])),
    ],
    "machine_power_protocol": common([{"reason": "identity-mismatch"}]) + [
        ("completed proving", completed(POWER)),
        ("completed unproving", completed(POWER_UNMET)),
        ("completed unproving observed", completed(POWER_UNMET, observed=True)),
        ("readings", completed(readings=[{"item": {"object": "node-a"}, "power": "On"},
                                         {"item": {"object": "node-b"}, "failed": True}])),
        ("malformed", completed(POWER_MALFORMED)),
        ("malformed readings", completed(readings=[])),
    ],
    "controller_protocol": [
        ("loaded", {"phase": "loaded"}),
        ("continue", {"phase": "continue"}),
        ("unknown phase", {"phase": "unloaded"}),
        ("loaded with arguments", {"phase": "loaded", "request": {}}),
        ("prepared", {"phase": "prepared", "request": CONTROLLER_REQUEST, "inventory": [], "roots_ready": True}),
        ("completed", {"phase": "completed", "request": CONTROLLER_REQUEST, "inventory": [], "roots_ready": True,
                       "preparation": CONTROLLER_PREPARATION, "native_applied": False, "tools": [],
                       "tools_changed": False}),
        ("completed unproving", {"phase": "completed", "request": CONTROLLER_REQUEST, "inventory": [],
                                 "roots_ready": False, "preparation": CONTROLLER_PREPARATION,
                                 "native_applied": False, "tools": [], "tools_changed": False}),
        ("malformed", {"phase": "prepared", "request": dict(CONTROLLER_REQUEST, version="v4"), "inventory": [],
                       "roots_ready": True}),
    ],
}


def run(name, arguments, monkeypatch, refuse=False):
    """One run of a plugin's ActionModule with the channel recorded, or with a
    channel that refuses every record when refuse is set."""
    module = importlib.import_module(PACKAGE + name)
    records = []

    def emit(record, acknowledge=False):
        records.append([copy.deepcopy(record), acknowledge])
        if refuse:
            raise OSError("closed")

    monkeypatch.setattr(module, "emit", emit)
    monkeypatch.setattr(ActionBase, "run", lambda self, tmp=None, task_vars=None: {})
    action = module.ActionModule.__new__(module.ActionModule)
    action._task = SimpleNamespace(args=copy.deepcopy(arguments))
    return {"records": records, "result": action.run(task_vars={})}


def recorded(name, monkeypatch):
    cases = []
    for label, arguments in CASES[name]:
        cases.append(dict({"case": label}, **run(name, arguments, monkeypatch)))
    cases.append(dict({"case": "channel refused"}, **run(name, {"phase": "loaded"}, monkeypatch, refuse=True)))
    return json.dumps(cases, indent=2) + "\n"


@pytest.mark.parametrize("name", sorted(CASES))
def test_each_protocol_plugin_hands_the_records_its_golden_holds(name, monkeypatch):
    written = recorded(name, monkeypatch)
    golden = GOLDENS / (name + ".json")
    if UPDATE:
        golden.parent.mkdir(parents=True, exist_ok=True)
        golden.write_text(written)
    assert golden.read_text() == written, "%s differs; rerun with BOOTWRIGHT_UPDATE_GOLDENS=1 if intended" % golden.name


def test_a_golden_keeps_each_record_in_the_order_its_plugin_built_it(monkeypatch):
    written = json.loads(recorded("infra_service_protocol", monkeypatch))
    group = next(case for case in written if case["case"] == "group ok")
    assert list(group["records"][0][0]) == ["phase", "group", "status"]
