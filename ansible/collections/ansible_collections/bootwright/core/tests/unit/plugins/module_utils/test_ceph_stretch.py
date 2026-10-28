"""Stretch activation: request shape, journaled stages, readbacks, interruption, drift and the frozen intent.

The scripted monitor answers the shapes the stages read. `osd crush rule dump` steps and `osd tree` nodes with children follow
the upstream sources test_ceph_pools.py cites; the `mon dump` keys election_strategy, stretch_mode, tiebreaker_mon and a MON's
`{key=value}` crush_location, `osd dump`'s crush_version, stretch_mode member and pool keys, and `osd crush tree` sharing
`osd tree`'s node form are not fixed by a cited page: .agents/knowledge/ceph-stretch-native-facts.md records them as
unverified until B488's rehearsal reads them from the selected build.
"""

from __future__ import annotations

import contextlib
import copy
import json

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_native, ceph_native_stretch as stretch, ceph_tunnel_staging as staging
from ansible_collections.bootwright.core.tests.unit.plugins.module_utils import test_ceph_core as core

core_request = core.core_request

FSID = "4c0a4300-aa43-458c-86d7-ac2256d1fc00"
SITES = {"node-1": "site-a", "node-2": "site-a", "node-3": "site-b", "node-4": "site-b", "node-5": "site-c"}
FORBIDDEN = ("disable_stretch_mode", "--yes-i-really-mean-it", "rm", "delete")


def request():
    hosts = []
    for i in range(1, 6):
        devices = [] if i == 5 else [{"path": "/dev/vdb", "serial": "disk" + str(i), "sizeBytes": 1024 ** 3}]
        location = {} if i == 5 else {"datacenter": SITES["node-" + str(i)], "root": "default"}
        hosts.append({"name": "node-" + str(i), "machine": "machine-" + str(i), "address": "192.0.2." + str(i), "user": "operator", "port": 22,
                      "devices": devices, "labels": [], "location": location, "sshKeyRef": "fleet-key"})
    names = [host["name"] for host in hosts]
    value = {"controller": "controller", "version": "ceph-core-v35", "action": "cluster", "cluster": "example", "owner": "apply-123", "fsid": FSID,
             "image": "registry.example.test/ceph@sha256:" + "a" * 64, "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": "20.2.1-405.el9cp"},
             "release": "synthetic-release", "ansiblePackageVersion": "synthetic-ansible", "packageRepository": "https://packages.example.test/native",
             "packageRepositoryKey": "https://packages.example.test/keys/ibm.asc", "bootstrap": "node-1", "monIP": "192.0.2.1",
             "clusterSSHUser": "operator", "clusterSSHTransport": "verifiedTunnel", "clusterKeyRef": "cluster-key", "registryRef": "registry-key",
             "registry": "registry.example.test", "callHome": "enabled", "publicNetworks": ["192.0.2.0/24"], "clusterNetworks": [], "hosts": hosts,
             "stretch": {"dataSites": ["site-a", "site-b"], "failureDomain": "datacenter", "rule": "stretch-rule", "tiebreaker": "node-5",
                         "tiebreakerSite": "site-c"},
             "services": [{"service_type": "mon", "placement": {"hosts": names, "count": 3},
                           "spec": {"crush_locations": {name: ["datacenter=" + SITES[name]] for name in names}}},
                          {"service_type": "mgr", "placement": {"hosts": names[:4], "count": 2}},
                          {"service_type": "osd", "service_id": "data", "placement": {"hosts": names[:4]}, "spec": {"data_devices": {"paths": ["/dev/vdb"]}}}]}
    return value


def bucket(identifier, name, kind, children=()):
    return {"id": identifier, "name": name, "type": kind, "type_id": 0, "children": list(children)}


class World:
    """A scripted monitor: reads answer from its maps; every other command is a logged effect it applies or refuses as the MON would."""

    def __init__(self, misplaced=False, unlocated=("node-1",), lose=None):
        self.log, self.effects, self.lose = [], [], lose
        self.nodes = {"default": bucket(-1, "default", "root", (-2, -3)), "site-a": bucket(-2, "site-a", "datacenter", (-4, -5)),
                      "site-b": bucket(-3, "site-b", "datacenter", (-6, -7))}
        for index, name in enumerate(("node-1", "node-2", "node-3", "node-4")):
            self.nodes[name] = bucket(-4 - index, name, "host", (index,))
            self.nodes["osd." + str(index)] = {"id": index, "name": "osd." + str(index), "type": "osd"}
        if misplaced:
            self.nodes["site-a"]["children"].remove(-4)
            self.nodes["default"]["children"].append(-4)
        self.mons = [{"name": name, "rank": rank, "crush_location": "{}" if name in unlocated else "{datacenter=" + SITES[name] + "}"}
                     for rank, name in enumerate(("node-1", "node-3", "node-5"))]
        self.election, self.mode, self.tiebreaker, self.crush_version = 1, False, "", 7
        self.rules = [{"rule_id": 0, "rule_name": "replicated_rule", "type": 1,
                       "steps": [{"op": "take", "item": -1, "item_name": "default"}, {"op": "chooseleaf_firstn", "num": 0, "type": "host"}, {"op": "emit"}]}]
        self.pools = [{"pool_name": ".mgr", "pool_id": 1, "crush_rule": 0, "size": 3, "min_size": 2}]

    def mon_dump(self):
        return {"epoch": 9, "fsid": FSID, "election_strategy": self.election, "stretch_mode": self.mode, "tiebreaker_mon": self.tiebreaker,
                "mons": copy.deepcopy(self.mons)}

    def osd_dump(self):
        return {"epoch": 30, "fsid": FSID, "crush_version": self.crush_version, "pools": copy.deepcopy(self.pools),
                "stretch_mode": {"stretch_mode_enabled": self.mode, "stretch_bucket_count": 2 if self.mode else 0, "degraded_stretch_mode": 0,
                                 "recovering_stretch_mode": 0, "stretch_mode_bucket": 8 if self.mode else 0}}

    def run(self, argv, data=b"", timeout=60):
        del timeout
        assert argv[0] == "/usr/bin/ceph", argv
        command = tuple(argv[argv.index("--format") + 2:]) if "--format" in argv else tuple(argv[argv.index("--keyring") + 2:])
        for word in FORBIDDEN:
            assert word not in command, command
        self.log.append(command)
        reads = {("fsid",): {"fsid": FSID}, ("orch", "host", "ls"): [], ("mon", "dump"): self.mon_dump(), ("osd", "dump"): self.osd_dump(),
                 ("osd", "crush", "tree"): {"nodes": copy.deepcopy(list(self.nodes.values())), "stray": []},
                 ("osd", "crush", "rule", "dump"): copy.deepcopy(self.rules)}
        if command in reads:
            return 0, json.dumps(reads[command]).encode()
        if command == ("osd", "getcrushmap"):
            return 0, b"compiled-crush-map-v" + str(self.crush_version).encode()
        self.effects.append(command)
        code = self.effect(command, data)
        if self.lose == command[:len(self.lose or ())]:
            self.lose = None
            return 255, b""
        return code, b""

    def parent(self, name):
        identifier = self.nodes[name]["id"]
        return next((node["name"] for node in self.nodes.values() if identifier in node.get("children", [])), None)

    def place(self, host, location):
        for node in self.nodes.values():
            if self.nodes[host]["id"] in node.get("children", []):
                node["children"].remove(self.nodes[host]["id"])
        self.nodes[location["datacenter"]]["children"].append(self.nodes[host]["id"])

    def effect(self, command, data):
        if command[:3] in (("osd", "crush", "move"), ("osd", "crush", "add-bucket")):
            host = command[3]
            args = command[4:] if command[2] == "move" else command[5:]
            location = dict(item.split("=", 1) for item in args)
            assert location["root"] == "default"
            if command[2] == "add-bucket":
                self.nodes[host] = bucket(-40, host, "host")
            self.place(host, location)
            self.crush_version += 1
        elif command[:2] == ("mon", "set_location"):
            mon = next(mon for mon in self.mons if mon["name"] == command[2])
            mon["crush_location"] = "{" + command[3] + "}"
        elif command[:3] == ("osd", "setcrushmap", "-i"):
            if int(command[4]) != self.crush_version:
                return 1
            text = data.decode().split("::", 1)[1]
            words = text.split()
            self.rules.append({"rule_id": int(words[words.index("id") + 1]), "rule_name": words[1], "type": 1,
                               "steps": [{"op": "take", "item": -1, "item_name": words[words.index("take") + 1]},
                                         {"op": "choose_firstn", "num": 0, "type": words[words.index("choose") + 4]},
                                         {"op": "chooseleaf_firstn", "num": 2, "type": "host"}, {"op": "emit"}]})
            self.crush_version += 1
        elif command == ("mon", "set", "election_strategy", "connectivity"):
            self.election = 3
        elif command[:2] == ("mon", "enable_stretch_mode"):
            rule = next(rule for rule in self.rules if rule["rule_name"] == command[3])
            assert self.election == 3 and all(mon["crush_location"] != "{}" for mon in self.mons) and command[4] == "datacenter"
            assert all((pool["size"], pool["min_size"]) == (3, 2) for pool in self.pools)
            self.mode, self.tiebreaker = True, command[2]
            for pool in self.pools:
                pool.update(crush_rule=rule["rule_id"], size=4, min_size=2)
        else:
            raise AssertionError(command)
        return 0


class Markers:
    def __init__(self, records=None):
        self.records, self.writes, self.path = dict(records or {}), [], "/private/cluster"

    @staticmethod
    def owned():
        return True

    def read(self, name="owner.json"):
        return copy.deepcopy(self.records.get(name))

    def write(self, name, value):
        ceph_native.require(name not in self.records or self.records[name] == value, "ownership")
        self.records[name] = copy.deepcopy(value)
        self.writes.append(name)


def worker(world, marks=None, value=None, monkeypatch=None):
    value = value or request()
    marks = marks or Markers({"bootstrap-scope.json": staging.recovery_scope(value)})
    native = ceph_native.Worker(value, "node-1", run=world.run, markers=marks)
    native.bootstrap_hold = contextlib.nullcontext
    if monkeypatch is not None:
        monkeypatch.setattr(stretch.Activation, "compile", lambda self, current, text: current + b"::" + text.encode())
    return native, marks


def test_a_stretch_request_validates_and_its_tiebreaker_mon_and_hosts_agree():
    ceph_native.validate(request())
    for change in (lambda r: r["hosts"][4].update(location={"datacenter": "site-c", "root": "default"}),
                   lambda r: r["hosts"][0].update(location={"datacenter": "site-b", "root": "default"}),
                   lambda r: r["hosts"][1].update(location={"datacenter": "site-z", "root": "default"}),
                   lambda r: r["hosts"][0]["location"].pop("root"),
                   lambda r: r["services"][0]["spec"]["crush_locations"].update({"node-5": ["datacenter=site-a"]}),
                   lambda r: r["services"][0]["spec"].clear(),
                   lambda r: r["stretch"].update(tiebreaker="node-1"),
                   lambda r: r["stretch"].update(tiebreakerSite="site-a"),
                   lambda r: r["stretch"].update(failureDomain="host"),
                   lambda r: r["stretch"].update(rule="replicated_rule"),
                   lambda r: r.pop("stretch")):
        value = request()
        change(value)
        with pytest.raises(ceph_native.Refusal):
            ceph_native.validate(value)


def test_a_cluster_without_stretch_takes_no_location_and_a_host_scope_carries_the_stretch_block():
    value = request()
    value.pop("stretch")
    for host in value["hosts"]:
        host["location"] = {}
    value["services"][0].pop("spec")
    ceph_native.validate(value)
    scope = {key: item for key, item in request().items() if key != "action"}
    ceph_native.validate_scope(scope)
    assert staging.recovery_scope(dict(scope, version="ceph-core-v35")) == staging.recovery_scope(request())
    without = {key: item for key, item in scope.items() if key != "stretch"}
    with pytest.raises(ceph_native.Refusal):
        ceph_native.validate_scope(without)


def test_enrollment_places_each_data_host_by_its_location_spec(monkeypatch):
    world = World()
    native, marks = worker(world)
    marks.records.update({"tunnel-mounted.json": {}, "cache-ready.json": {}})
    for name in ("prove_registry", "provider_registry_absent", "verify_transport_bundle", "verify_tunnel_configuration", "verify_manager_mounts", "daemons"):
        monkeypatch.setattr(native, name, lambda *args, **options: [])
    pending = {intent["host"]["hostname"]: intent["host"] for unused, intent in native.enrollment_state()[3]}
    assert pending["node-3"] == {"service_type": "host", "hostname": "node-3", "addr": "192.0.2.3", "labels": [],
                                 "location": {"datacenter": "site-b", "root": "default"}}
    assert "location" not in pending["node-5"]


def test_activation_journals_each_stage_reads_it_back_and_corrects_only_the_bootstrap_host(monkeypatch):
    world = World(misplaced=True)
    native, marks = worker(world, monkeypatch=monkeypatch)
    assert native.stretch_locate() == {"changed": True, "stage": "rule-present"}
    assert world.effects == [("osd", "crush", "move", "node-1", "root=default", "datacenter=site-a"),
                             ("mon", "set_location", "node-1", "datacenter=site-a"),
                             ("osd", "setcrushmap", "-i", "-", "8")]
    assert world.parent("node-1") == "site-a" and world.rules[-1]["rule_id"] == 1
    assert native.stretch_enable() == {"changed": True, "stage": "verified"}
    assert world.effects[3:] == [("mon", "set", "election_strategy", "connectivity"), ("mon", "enable_stretch_mode", "node-5", "stretch-rule", "datacenter")]
    order = [name for name in marks.writes if name.startswith("stretch-")]
    assert order == [prefix + "-" + record + ".json" for prefix in ("stretch-" + stage for stage in stretch.STAGES) for record in ("attempt", "complete")]
    for index, command in enumerate(world.log):
        if command in world.effects:
            assert world.log[index + 1:], "an effect was not read back"
    effects = list(world.effects)
    assert native.stretch_locate() == {"changed": False, "stage": "verified"}
    assert native.stretch_enable() == {"changed": False, "stage": "verified"}
    assert world.effects == effects
    assert stretch.Activation(native).observe() == ({"stage": "verified", "enabled": True}, True)


def test_the_rule_is_injected_with_the_crush_version_it_was_compiled_from(monkeypatch):
    world = World()
    native, unused = worker(world, monkeypatch=monkeypatch)
    texts = []
    monkeypatch.setattr(stretch.Activation, "compile", lambda self, current, text: texts.append((current, text)) or current + b"::" + text.encode())
    native.stretch_locate()
    current, text = texts[0]
    assert current == b"compiled-crush-map-v7"
    assert text == ("rule stretch-rule {\n\tid 1\n\ttype replicated\n\tstep take default\n\tstep choose firstn 0 type datacenter\n"
                    "\tstep chooseleaf firstn 2 type host\n\tstep emit\n}\n")
    assert ("osd", "setcrushmap", "-i", "-", "7") in world.effects


@pytest.mark.parametrize("lost", [("mon", "set_location"), ("osd", "setcrushmap"), ("mon", "set", "election_strategy"), ("mon", "enable_stretch_mode")])
def test_a_lost_reply_whose_effect_landed_completes_from_the_readback_without_repeating(monkeypatch, lost):
    world = World(lose=lost)
    native, marks = worker(world, monkeypatch=monkeypatch)
    with pytest.raises(ceph_native.Refusal, match="native-command"):
        native.stretch_locate()
        native.stretch_enable()
    native, unused = worker(world, marks=marks, monkeypatch=monkeypatch)
    native.stretch_locate()
    assert native.stretch_enable()["stage"] == "verified"
    assert [command for command in world.effects if command[:len(lost)] == lost] == [next(c for c in world.effects if c[:len(lost)] == lost)]


@pytest.mark.parametrize("stage, undo", [
    ("rule-present", lambda world: world.rules.pop()),
    ("election-connectivity", lambda world: setattr(world, "election", 1)),
    ("stretch-enabled", lambda world: setattr(world, "mode", False)),
])
def test_an_attempted_stage_whose_result_is_absent_is_observed_and_never_repeated(monkeypatch, stage, undo):
    world = World()
    native, marks = worker(world, monkeypatch=monkeypatch)
    native.stretch_locate()
    native.stretch_enable()
    undo(world)
    for later in stretch.STAGES[stretch.STAGES.index(stage):]:
        marks.records.pop("stretch-" + later + "-complete.json")
        if later != stage:
            marks.records.pop("stretch-" + later + "-attempt.json")
    effects = list(world.effects)
    native, unused = worker(world, marks=marks, monkeypatch=monkeypatch)
    with pytest.raises(ceph_native.Refusal, match="^stretch-interrupted$"):
        native.stretch_locate()
        native.stretch_enable()
    assert world.effects == effects


def test_a_completed_stage_that_no_longer_holds_is_drift_and_observes_partial(monkeypatch):
    world = World()
    native, marks = worker(world, monkeypatch=monkeypatch)
    native.stretch_locate()
    native.stretch_enable()
    world.election = 1
    assert stretch.Activation(native).observe() == ({"stage": "verified", "enabled": True}, False)
    with pytest.raises(ceph_native.Refusal, match="^stretch-drift$"):
        native.stretch_enable()
    world.election = 3
    world.pools.append({"pool_name": ".nfs", "pool_id": 2, "crush_rule": 0, "size": 3, "min_size": 2})
    assert stretch.Activation(native).observe()[1] is False
    with pytest.raises(ceph_native.Refusal, match="^stretch-pools$"):
        native.stretch_enable()


@pytest.mark.parametrize("change, reason", [
    (lambda world: world.rules.append({"rule_id": 1, "rule_name": "stretch-rule", "type": 1, "steps": []}), "stretch-rule"),
    (lambda world: world.nodes["site-b"]["children"].remove(-6) or world.nodes["site-a"]["children"].append(-6), "stretch-location"),
    (lambda world: world.mons[1].update(crush_location="{datacenter=site-a}"), "stretch-location"),
    (lambda world: world.nodes.update({"site-c": bucket(-9, "site-c", "datacenter")}), "stretch-location"),
])
def test_foreign_or_misplaced_state_refuses_before_any_stage_effect(monkeypatch, change, reason):
    world = World()
    change(world)
    native, unused = worker(world, monkeypatch=monkeypatch)
    with pytest.raises(ceph_native.Refusal, match="^" + reason + "$"):
        native.stretch_locate()
    assert not [command for command in world.effects if command[:2] == ("osd", "setcrushmap")]


def test_stretch_mode_enabled_without_this_operations_attempt_is_never_adopted(monkeypatch):
    world = World()
    native, unused = worker(world, monkeypatch=monkeypatch)
    native.stretch_locate()
    world.mode, world.tiebreaker = True, "node-5"
    with pytest.raises(ceph_native.Refusal, match="^stretch-foreign$"):
        native.stretch_enable()
    assert ("mon", "enable_stretch_mode", "node-5", "stretch-rule", "datacenter") not in world.effects


@pytest.mark.parametrize("stage", ["election-connectivity", "stretch-enabled"])
def test_a_continuation_after_a_later_stage_reports_that_stage_from_both_calls(monkeypatch, stage):
    world = World()
    native, marks = worker(world, monkeypatch=monkeypatch)
    native.stretch_locate()
    native.stretch_enable()
    for later in stretch.STAGES[stretch.STAGES.index(stage) + 1:]:
        marks.records.pop("stretch-" + later + "-complete.json")
        marks.records.pop("stretch-" + later + "-attempt.json")
    if stage == "election-connectivity":
        world.mode, world.tiebreaker = False, ""
        for pool in world.pools:
            pool.update(crush_rule=0, size=3, min_size=2)
    native, unused = worker(world, marks=marks, monkeypatch=monkeypatch)
    assert native.stretch_locate() == {"changed": False, "stage": stage}
    assert native.stretch_enable() == {"changed": stage == "election-connectivity", "stage": "verified"}


@pytest.mark.parametrize("change", [
    lambda world: world.pools.append({"pool_name": "ec", "pool_id": 3, "type": 3, "crush_rule": 0, "size": 3, "min_size": 2}),
    lambda world: world.pools.append({"pool_name": "small", "pool_id": 3, "crush_rule": 0, "size": 2, "min_size": 1}),
    lambda world: world.nodes.update({"site-z": bucket(-9, "site-z", "datacenter")}),
])
def test_what_enable_stretch_mode_would_refuse_refuses_before_its_attempt(monkeypatch, change):
    world = World()
    native, marks = worker(world, monkeypatch=monkeypatch)
    native.stretch_locate()
    change(world)
    with pytest.raises(ceph_native.Refusal, match="^stretch-(pools|location)$"):
        native.stretch_enable()
    assert "stretch-stretch-enabled-attempt.json" not in marks.records
    assert not [command for command in world.effects if command[:2] == ("mon", "enable_stretch_mode")]


def test_enable_needs_the_rule_stage_and_never_runs_disable(monkeypatch):
    world = World()
    native, unused = worker(world, monkeypatch=monkeypatch)
    with pytest.raises(ceph_native.Refusal, match="^not-ready$"):
        native.stretch_enable()
    assert world.effects == []


def test_observation_of_an_unactivated_stretch_cluster_is_partial_without_effects(monkeypatch):
    world = World()
    native, unused = worker(world, monkeypatch=monkeypatch)
    assert stretch.Activation(native).observe() == ({"stage": "", "enabled": False}, False)
    assert world.effects == []


@pytest.mark.parametrize("change", [
    lambda value: value["stretch"].update(rule="other-rule"),
    lambda value: value["stretch"].update(tiebreakerSite="site-d"),
    lambda value: value.pop("stretch"),
])
def test_a_changed_stretch_intent_refuses_by_name_before_the_ownership_proof(change):
    retained = staging.recovery_scope(request())
    value = request()
    change(value)
    with pytest.raises(ceph_native.Refusal, match="^stretch-intent$"):
        stretch.prove_retained(value, scope=retained)
    owner = ceph_native.marker_identity(request())
    moved = request()
    moved["hosts"][2]["location"] = {"datacenter": "site-a", "root": "default"}
    with pytest.raises(ceph_native.Refusal, match="^stretch-intent$"):
        stretch.prove_retained(moved, owner=owner)
    stretch.prove_retained(request(), owner=owner, scope=retained)


def test_the_stretch_evidence_member_is_closed_and_present_only_on_a_stretch_cluster(core_request):
    value = request()
    observed = dict(core.observation(value, ready=True), request="d" * 64, stretch={"stage": "verified", "enabled": True})
    ceph_core.evidence(observed, "d" * 64)
    for member in ({"stage": "rule-present", "enabled": False}, {"stage": "tiebreaker-located", "enabled": True}, {"stage": "verified"}):
        with pytest.raises(ceph_native.Refusal, match="^evidence$"):
            ceph_core.evidence(dict(observed, stretch=member), "d" * 64)
    plain = dict(core.observation(core_request, ready=True), request="d" * 64, stretch={"stage": "verified", "enabled": True})
    with pytest.raises(ceph_native.Refusal, match="^evidence$"):
        ceph_core.evidence(plain, "d" * 64, core_request)


def test_a_crush_location_reads_only_the_rendered_key_value_form():
    assert stretch.crush_location("{}") == {}
    assert stretch.crush_location("{datacenter=site-a}") == {"datacenter": "site-a"}
    for value in ("datacenter=site-a", "{datacenter}", "{a=b,a=c}", {"datacenter": "site-a"}):
        with pytest.raises(ceph_native.Refusal):
            stretch.crush_location(value)


class ActivationTransport(core.ReadinessTransport):
    """The controller's view: hosts at one recovery stage, then observations that turn ready only after both stretch calls."""

    def __init__(self, value, clock, stage):
        super().__init__(value, clock, ready_after=1)
        self.stage, self.activated = stage, ""

    def observed(self, before=False):
        value = core.observation(self.request, ready=True)
        value["stretch"] = {"stage": self.activated, "enabled": self.activated == "verified"}
        if self.activated != "verified":
            value.update(ready=False, partial=True)
            if not before:
                value["activation"] = True
        return value

    def call(self, host, phase, private=None, *, timeout=None, devices=None, service=None):
        if phase == "prerequisites":
            self.calls.append((host["name"], phase, timeout))
            local = self.stage if self.stage in ("unclaimed", "complete") or host["name"] == self.request["bootstrap"] else "prepared"
            return {"name": host["name"], "owned": local != "unclaimed", "exists": local == "complete",
                    "recovery": staging.recovery_record(self.request, host["name"], local)}
        if phase == "bootstrap_state":
            self.calls.append((host["name"], phase, timeout))
            record = staging.recovery_record(self.request, host["name"], self.stage)
            evidence = self.observed(before=True)
            if self.stage == "unclaimed":
                evidence = dict(core.observation(self.request), owned=False, exists=False, removed=False, stretch={"stage": "", "enabled": False})
            record["evidence"] = evidence
            return record
        if phase in ("stretch_locate", "stretch_enable"):
            self.calls.append((host["name"], phase, timeout))
            self.activated = "rule-present" if phase == "stretch_locate" else "verified"
            return {"changed": True, "stage": self.activated}
        if phase == "observe" and timeout is not None:
            self.calls.append((host["name"], phase, timeout))
            self.clock.value += 120
            return self.observed()
        if phase in ("tunnel_configure", "tunnel_mount"):
            self.calls.append((host["name"], phase, timeout))
            return {"changed": True}
        return super().call(host, phase, private, timeout=timeout, devices=devices, service=service)


def test_the_controller_accepts_a_later_stage_a_continuation_reports(monkeypatch, tmp_path):
    value = request()
    core.package_intent(value)
    clock = core.ReadinessClock()

    class Later(ActivationTransport):
        def call(self, host, phase, private=None, **options):
            result = super().call(host, phase, private, **options)
            return dict(result, stage="stretch-enabled") if phase == "stretch_locate" else result

    transport = Later(value, clock, "complete")
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    assert ceph_core.execute(value, "apply", "d" * 64, transport, {"dashboard": str(tmp_path / "dashboard")}, now=clock.now, pause=clock.pause)["changed"]


@pytest.mark.parametrize("stage", ["unclaimed", "complete"])
def test_the_cluster_block_activates_stretch_after_services_and_before_readiness_and_the_dashboard(monkeypatch, tmp_path, stage):
    value = request()
    core.package_intent(value)
    clock = core.ReadinessClock()
    transport = ActivationTransport(value, clock, stage)
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    result = ceph_core.execute(value, "apply", "d" * 64, transport, {"dashboard": str(tmp_path / "dashboard")}, now=clock.now, pause=clock.pause)
    assert result["evidence"]["ready"] and result["evidence"]["stretch"] == {"stage": "verified", "enabled": True}
    phases = [phase for unused, phase, unused in transport.calls if phase != "host_state"]
    locate, enable, dashboard = phases.index("stretch_locate"), phases.index("stretch_enable"), phases.index("dashboard")
    assert phases.count("stretch_locate") == phases.count("stretch_enable") == 1 and locate + 1 == enable < dashboard
    assert "observe" in phases[:locate] and "observe" in phases[enable:dashboard]
    if stage == "complete":
        assert not {"bootstrap", "enroll", "services", "claim", "authorize"} & set(phases)
    else:
        assert phases.index("services") < locate


def test_an_activated_stretch_cluster_is_not_activated_again(monkeypatch, tmp_path):
    value = request()
    core.package_intent(value)
    clock = core.ReadinessClock()
    transport = ActivationTransport(value, clock, "complete")
    transport.activated = "verified"
    monkeypatch.setattr(ceph_native, "supported_transport", lambda request: True)
    result = ceph_core.execute(value, "apply", "d" * 64, transport, {"dashboard": str(tmp_path / "dashboard")}, now=clock.now, pause=clock.pause)
    assert result["evidence"]["ready"]
    assert not {"stretch_locate", "stretch_enable"} & {phase for unused, phase, unused in transport.calls}


def test_activation_is_not_pending_while_a_host_has_not_joined():
    value = request()
    clock = core.ReadinessClock()

    class Missing(ActivationTransport):
        def call(self, host, phase, private=None, **options):
            if phase == "host_state" and host["name"] == "node-4":
                return {"name": host["name"], "owned": True, "exists": False}
            return super().call(host, phase, private, **options)

    pending = [None]
    bootstrap = value["hosts"][0]
    observed = ceph_core.observe_state(value, "d" * 64, bootstrap, Missing(value, clock, "complete"), clock.now() + 900, clock.now, [], pending)
    assert pending == [False] and not observed["ready"]
    ceph_core.observe_state(value, "d" * 64, bootstrap, ActivationTransport(value, clock, "complete"), clock.now() + 900, clock.now, [], pending)
    assert pending == [True]
