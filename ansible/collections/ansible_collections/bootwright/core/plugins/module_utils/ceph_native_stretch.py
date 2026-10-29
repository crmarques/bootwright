"""Native stretch mode: the stretch topology's request shape, its journaled activation stages and their readbacks.

Each stage writes its attempt record before its first native effect and its
completion record only after a readback shows its postcondition. A stage whose
attempt record exists without a completion record is observed and never
repeated. The JSON shapes of `ceph mon dump`, `ceph osd dump`, `ceph osd crush
tree` and `ceph osd crush rule dump` that no cited page fixes are recorded as
unverified in .agents/knowledge/ceph-stretch-native-facts.md.

A provided tiebreaker's removal runs on that host alone: a read-only proof that
it holds only this cluster's MON and crash collector, then, under one attempt
record, the recorded cluster-key line, the registry credential and the package
cephadm's rm-cluster, and absence read back without following links. Its
footprint on the selected build is unverified until B488 reads it.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import base64
import contextlib
import copy
import os
import pwd
import re
import stat
import uuid

from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import (
    REGISTRY_NAME, Refusal, daemon_identity_host, marker_identity, named, registry_directory, registry_exists, registry_read, require,
    tunnel_helpers,
)

STAGES = ("located", "rule-present", "election-connectivity", "stretch-enabled", "verified")
ROOT = "default"
# Native default CRUSH bucket types above host (cephcluster.stretchDomains).
DOMAINS = ("chassis", "rack", "row", "pdu", "pod", "room", "datacenter", "zone", "region")
# `ceph mon dump`'s election_strategy for connectivity: classic 1, disallow 2, connectivity 3.
CONNECTIVITY = 3
STRETCH_SIZE, STRETCH_MIN_SIZE = 4, 2
MAX_MAP = 64 * 1024
# The native default size and min_size a pool holds before stretch mode moves it to 4/2, unless the request's pool defaults set them.
DEFAULT_PROTECTION = (3, 2)
LOCATION = re.compile(r"\{([^{}]*)\}\Z")
COMPILE = (
    "import base64,json,subprocess,sys\n"
    "open('/tmp/in','wb').write(base64.b64decode(sys.argv[1],validate=True))\n"
    "subprocess.run(['/usr/bin/crushtool','-d','/tmp/in','-o','/tmp/map.txt'],check=True,capture_output=True,timeout=30)\n"
    "text=open('/tmp/map.txt').read()\n"
    "open('/tmp/map.txt','w').write(text.rstrip('\\n')+'\\n'+sys.argv[2])\n"
    "subprocess.run(['/usr/bin/crushtool','-c','/tmp/map.txt','-o','/tmp/out'],check=True,capture_output=True,timeout=30)\n"
    "print(json.dumps({'map':base64.b64encode(open('/tmp/out','rb').read()).decode()}))\n"
)


def validate(request, hosts):
    """A request's or a host scope's stretch block, host locations and MON crush_locations agree; without stretch there are none."""
    stretch = request.get("stretch")
    mon = next(service for service in request["services"] if service["service_type"] == "mon")
    if stretch is None:
        require(all(not host["location"] for host in hosts) and not mon.get("spec"), "request")
        return
    require(isinstance(stretch, dict)
            and set(stretch) == {"dataSites", "failureDomain", "rule", "tiebreaker", "tiebreakerSite"}, "request")
    sites, domain = stretch["dataSites"], stretch["failureDomain"]
    require(isinstance(sites, list) and len(sites) == 2 and all(named(site) for site in sites) and sites == sorted(set(sites)), "request")
    require(domain in DOMAINS and named(stretch["rule"]) and stretch["rule"] != "replicated_rule", "request")
    require(named(stretch["tiebreakerSite"]) and stretch["tiebreakerSite"] not in sites, "request")
    names = [host["name"] for host in hosts]
    require(stretch["tiebreaker"] in names and stretch["tiebreaker"] != request["bootstrap"], "request")
    for host in hosts:
        if host["name"] == stretch["tiebreaker"]:
            require(host["location"] == {} and not host["devices"], "request")
        else:
            require(set(host["location"]) == {domain, "root"} and host["location"]["root"] == ROOT
                    and host["location"][domain] in sites, "request")
    require(set(mon) == {"service_type", "placement", "spec"} and mon["spec"] == {"crush_locations": mon_locations(request)}, "request")
    require(stretch["tiebreaker"] in mon["placement"]["hosts"], "request")


def site_of(request, name):
    stretch = request["stretch"]
    if name == stretch["tiebreaker"]:
        return stretch["tiebreakerSite"]
    host = next(host for host in request["hosts"] if host["name"] == name)
    return host["location"][stretch["failureDomain"]]


def mon_locations(request):
    mon = next(service for service in request["services"] if service["service_type"] == "mon")
    domain = request["stretch"]["failureDomain"]
    return {name: [domain + "=" + site_of(request, name)] for name in mon["placement"]["hosts"]}


def intent(request):
    return {"stretch": request.get("stretch"), "locations": {host["name"]: host["location"] for host in request["hosts"]}}


def prove_retained(request, owner=None, scope=None):
    """A change of the stretch intent an owned cluster was created with refuses by name before the generic ownership proof."""
    if isinstance(owner, dict) and isinstance(owner.get("hosts"), list):
        retained = {host.get("name"): host.get("location", {}) for host in owner["hosts"] if isinstance(host, dict)}
        for host in request["hosts"]:
            if host["name"] in retained and retained[host["name"]] != host["location"]:
                raise Refusal("stretch-intent")
    if isinstance(scope, dict) and isinstance(scope.get("cluster"), dict):
        retained = scope["cluster"]
        if retained.get("stretch") != request.get("stretch"):
            raise Refusal("stretch-intent")


class Activation:
    """One cluster's stretch stages on its bootstrap host, read back through the cluster's admin custody."""

    def __init__(self, worker):
        require(worker.request["action"] == "cluster" and "stretch" in worker.request and worker.host["name"] == worker.request["bootstrap"])
        self.worker = worker
        self.request = worker.request
        self.stretch = worker.request["stretch"]
        placement = next(s["placement"] for s in self.request["services"] if s["service_type"] == "mon")
        self.mons, self.mon_count = placement["hosts"], placement.get("count", len(placement["hosts"]))
        self.cache = None

    def read(self, *args):
        """One JSON readback; an observation, which runs no effect, reads each map once."""
        if self.cache is None:
            return self.worker.ceph(*args)
        if args not in self.cache:
            self.cache[args] = self.worker.ceph(*args)
        return copy.deepcopy(self.cache[args])

    def journal(self, stage):
        record = {"scope": self.worker.recovery_scope(), "stage": stage, "stretch": self.stretch}
        attempt = self.worker.markers.read("stretch-" + stage + "-attempt.json")
        complete = self.worker.markers.read("stretch-" + stage + "-complete.json")
        require(attempt is None or attempt == record, "ownership")
        require(complete is None or complete == record and attempt == record, "ownership")
        return record, attempt, complete

    def completed(self, stage):
        unused_record, unused_attempt, complete = self.journal(stage)
        return complete is not None

    def stage(self):
        """The last stage in order whose completion record exists; every earlier one must have its own."""
        last = ""
        for stage in STAGES:
            if not self.completed(stage):
                break
            last = stage
        for later in STAGES[STAGES.index(last) + 1 if last else 0:]:
            require(not self.completed(later), "ownership")
        return last

    def run(self, stage, holds, effects, foreign=None, adopt=False):
        """One stage: a completed stage that no longer holds is drift; an attempted one is observed, never repeated."""
        record, attempt, complete = self.journal(stage)
        if complete is not None:
            require(holds(), "stretch-drift")
            return False
        if attempt is not None:
            require(holds(), "stretch-interrupted")
            self.worker.markers.write("stretch-" + stage + "-complete.json", record)
            return False
        if foreign is not None:
            foreign()
        held = holds()
        require(not held or adopt, "stretch-foreign")
        steps = [] if held else effects()
        self.worker.markers.write("stretch-" + stage + "-attempt.json", record)
        for argv in steps:
            self.worker.ceph(*argv["args"], data=argv.get("data", b""), json_result=False)
        require(holds(), "stretch-interrupted")
        self.worker.markers.write("stretch-" + stage + "-complete.json", record)
        return bool(steps)

    def crush_tree(self):
        tree = self.read("osd", "crush", "tree")
        require(isinstance(tree, dict) and isinstance(tree.get("nodes"), list) and len(tree["nodes"]) <= 8192, "native-output")
        nodes, parent = {}, {}
        for node in tree["nodes"]:
            require(isinstance(node, dict) and type(node.get("id")) is int and isinstance(node.get("name"), str)
                    and isinstance(node.get("type"), str), "native-output")
            require(node["name"] not in nodes, "native-output")
            nodes[node["name"]] = node
        identifiers = {node["id"]: node["name"] for node in nodes.values()}
        for node in nodes.values():
            children = node.get("children", [])
            require(isinstance(children, list) and all(type(child) is int and child in identifiers for child in children), "native-output")
            for child in children:
                require(identifiers[child] not in parent, "native-output")
                parent[identifiers[child]] = node["name"]
        return nodes, parent

    def placed(self, tree, host, site):
        nodes, parent = tree
        domain = self.stretch["failureDomain"]
        node, bucket = nodes.get(host), nodes.get(parent.get(host, ""))
        root = nodes.get(parent.get(bucket["name"], "")) if bucket is not None else None
        return (node is not None and node["type"] == "host" and bucket is not None and bucket["name"] == site and bucket["type"] == domain
                and root is not None and root["name"] == ROOT and root["type"] == "root")

    def mon_dump(self):
        dump = self.read("mon", "dump")
        require(isinstance(dump, dict) and dump.get("fsid") == self.request["fsid"] and isinstance(dump.get("mons"), list), "native-output")
        located = {}
        for mon in dump["mons"]:
            require(isinstance(mon, dict) and named(mon.get("name")), "native-output")
            host = daemon_identity_host("mon", mon["name"], self.mons)
            located[host] = (mon["name"], crush_location(mon.get("crush_location", "{}")))
        return dump, located

    def monitors_placed(self, mons):
        """The MON service's count, the tiebreaker and a MON at each data site, as enable_stretch_mode needs."""
        return (len(mons) == self.mon_count and self.stretch["tiebreaker"] in mons
                and set(self.stretch["dataSites"]) <= {site_of(self.request, host) for host in mons})

    def mon_wanted(self, host):
        return {self.stretch["failureDomain"]: site_of(self.request, host)}

    def located_holds(self):
        tree, (unused, mons) = self.crush_tree(), self.mon_dump()
        data = [host for host in self.request["hosts"] if host["name"] != self.stretch["tiebreaker"]]
        return (all(self.placed(tree, host["name"], host["location"][self.stretch["failureDomain"]]) for host in data)
                and self.stretch["tiebreakerSite"] not in tree[0] and self.stretch["tiebreaker"] not in tree[0]
                and self.monitors_placed(mons) and all(location == self.mon_wanted(host) for host, (unused, location) in mons.items()))

    def located_effects(self):
        """Only the bootstrap host's bucket and an unset MON location are corrected; anything else is foreign."""
        nodes, parent = self.crush_tree()
        unused, mons = self.mon_dump()
        require(self.monitors_placed(mons) and self.stretch["tiebreakerSite"] not in nodes and self.stretch["tiebreaker"] not in nodes,
                "stretch-location")
        domain, steps = self.stretch["failureDomain"], []
        for host in self.request["hosts"]:
            if host["name"] == self.stretch["tiebreaker"]:
                continue
            site = host["location"][domain]
            where = ["root=" + ROOT, domain + "=" + site]
            if self.placed((nodes, parent), host["name"], site):
                continue
            require(host["name"] == self.request["bootstrap"], "stretch-location")
            if host["name"] not in nodes:
                steps.append({"args": ("osd", "crush", "add-bucket", host["name"], "host", *where)})
            else:
                require(nodes[host["name"]]["type"] == "host" and parent.get(host["name"]) == ROOT, "stretch-location")
                steps.append({"args": ("osd", "crush", "move", host["name"], *where)})
        for host, (name, location) in sorted(mons.items()):
            if location != self.mon_wanted(host):
                require(not location, "stretch-location")
                steps.append({"args": ("mon", "set_location", name, domain + "=" + site_of(self.request, host))})
        return steps

    def rules(self):
        rules = self.read("osd", "crush", "rule", "dump")
        require(isinstance(rules, list) and len(rules) <= 256, "native-output")
        for rule in rules:
            require(isinstance(rule, dict) and type(rule.get("rule_id")) is int and 0 <= rule["rule_id"] <= 255
                    and isinstance(rule.get("rule_name"), str), "native-output")
        return rules

    def own_rule(self):
        mine = [rule for rule in self.rules() if rule["rule_name"] == self.stretch["rule"]]
        require(len(mine) <= 1, "native-output")
        return mine[0] if mine else None

    def rule_matches(self, rule):
        wanted = [{"op": "take", "item_name": ROOT}, {"op": "choose_firstn", "num": 0, "type": self.stretch["failureDomain"]},
                  {"op": "chooseleaf_firstn", "num": 2, "type": "host"}, {"op": "emit"}]
        steps = rule.get("steps")
        return (rule.get("type") == 1 and isinstance(steps, list) and len(steps) == len(wanted)
                and all(isinstance(step, dict) and {key: step.get(key) for key in want} == want for step, want in zip(steps, wanted)))

    def rule_holds(self):
        rule = self.own_rule()
        return rule is not None and self.rule_matches(rule)

    def rule_foreign(self):
        require(self.own_rule() is None, "stretch-rule")

    def rule_text(self, identifier):
        return ("rule " + self.stretch["rule"] + " {\n\tid " + str(identifier) + "\n\ttype replicated\n\tstep take " + ROOT
                + "\n\tstep choose firstn 0 type " + self.stretch["failureDomain"] + "\n\tstep chooseleaf firstn 2 type host\n\tstep emit\n}\n")

    def rule_effects(self):
        """IBM's get, decompile, edit, compile and set, compiled offline in the frozen image and guarded by the CRUSH version."""
        identifier = max([rule["rule_id"] for rule in self.rules()] + [-1]) + 1
        require(identifier <= 255, "stretch-rule")
        osdmap = self.read("osd", "dump")
        require(isinstance(osdmap, dict) and type(osdmap.get("crush_version")) is int and osdmap["crush_version"] >= 0, "native-output")
        current = self.worker.ceph("osd", "getcrushmap", json_result=False)
        # The map travels as one base64 argument, which Linux bounds at 128 KiB.
        require(isinstance(current, bytes) and 0 < len(current) <= MAX_MAP, "native-output")
        compiled = self.compile(current, self.rule_text(identifier))
        return [{"args": ("osd", "setcrushmap", "-i", "-", str(osdmap["crush_version"])), "data": compiled}]

    def compile(self, current, text):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
        profile = self.worker.runtime_profile()
        cluster = {key: value for key, value in self.request.items() if key not in ("service", "gateway")}
        host = runtime.HostRuntime(dict(cluster, action="cluster"), self.worker.host["name"], command=self.worker.run)
        result = host.contained(profile["imageID"], (profile["uid"], profile["gid"]), COMPILE, (base64.b64encode(current).decode(), text))
        require(isinstance(result, dict) and set(result) == {"map"} and isinstance(result["map"], str), "native-output")
        try:
            compiled = base64.b64decode(result["map"], validate=True)
        except ValueError:
            raise Refusal("native-output") from None
        require(0 < len(compiled) <= 2 * MAX_MAP, "native-output")
        return compiled

    def election_holds(self):
        dump, unused = self.mon_dump()
        return dump.get("election_strategy") == CONNECTIVITY

    def tiebreaker_mon(self, located):
        require(self.stretch["tiebreaker"] in located, "stretch-location")
        return located[self.stretch["tiebreaker"]][0]

    def enabled_state(self):
        """(stretch mode as this request enables it, stretch mode enabled at all) from the MON and OSD maps."""
        dump, located = self.mon_dump()
        osdmap = self.read("osd", "dump")
        require(isinstance(osdmap, dict) and isinstance(osdmap.get("stretch_mode", {}), dict), "native-output")
        mode = osdmap.get("stretch_mode", {})
        require(type(mode.get("stretch_mode_enabled", False)) is bool and type(dump.get("stretch_mode", False)) is bool, "native-output")
        enabled = mode.get("stretch_mode_enabled", False) or dump.get("stretch_mode", False)
        exact = (enabled and mode.get("stretch_mode_enabled") is True and dump.get("stretch_mode") is True and mode.get("stretch_bucket_count") == 2
                 and self.stretch["tiebreaker"] in located and dump.get("tiebreaker_mon") == located[self.stretch["tiebreaker"]][0])
        return exact, enabled

    def enabled_holds(self):
        return self.enabled_state()[0]

    def enabled_foreign(self):
        """Refuse before the attempt what enable_stretch_mode would refuse: stretch already on, other than two site buckets, an
        erasure-coded pool or a pool off its default protection."""
        require(not self.enabled_state()[1], "stretch-foreign")
        nodes, unused = self.crush_tree()
        domain = self.stretch["failureDomain"]
        require(sorted(node["name"] for node in nodes.values() if node["type"] == domain) == self.stretch["dataSites"], "stretch-location")
        osdmap = self.read("osd", "dump")
        require(isinstance(osdmap, dict) and isinstance(osdmap.get("pools"), list), "native-output")
        require(all(isinstance(pool, dict) and pool.get("type", 1) == 1 and (pool.get("size"), pool.get("min_size")) == self.default_protection()
                    for pool in osdmap["pools"]), "stretch-pools")

    def default_protection(self):
        options = self.request.get("config", {}).get("global", {})
        if "osd_pool_default_size" in options:
            return int(options["osd_pool_default_size"]), int(options["osd_pool_default_min_size"])
        return DEFAULT_PROTECTION

    def enable_effects(self):
        unused, located = self.mon_dump()
        return [{"args": ("mon", "enable_stretch_mode", self.tiebreaker_mon(located), self.stretch["rule"], self.stretch["failureDomain"])}]

    def pools_hold(self):
        rule = self.own_rule()
        osdmap = self.read("osd", "dump")
        require(isinstance(osdmap, dict) and isinstance(osdmap.get("pools"), list) and len(osdmap["pools"]) <= 4096, "native-output")
        for pool in osdmap["pools"]:
            require(isinstance(pool, dict) and isinstance(pool.get("pool_name"), str), "native-output")
        return rule is not None and all(pool.get("crush_rule") == rule["rule_id"] and pool.get("size") == STRETCH_SIZE
                                        and pool.get("min_size") == STRETCH_MIN_SIZE for pool in osdmap["pools"])

    def prove_cluster(self):
        require(self.worker.markers.owned(), "ownership")
        require(self.worker.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")

    def locate(self):
        self.prove_cluster()
        changed = self.run("located", self.located_holds, self.located_effects, adopt=True)
        changed = self.run("rule-present", self.rule_holds, self.rule_effects, foreign=self.rule_foreign) or changed
        return {"changed": changed, "stage": self.stage()}

    def enable(self):
        self.prove_cluster()
        require(self.completed("rule-present"), "not-ready")
        changed = self.run("election-connectivity", self.election_holds,
                           lambda: [{"args": ("mon", "set", "election_strategy", "connectivity")}], adopt=True)
        changed = self.run("stretch-enabled", self.enabled_holds, self.enable_effects, foreign=self.enabled_foreign) or changed
        stage = "verified"
        record, unused, complete = self.journal(stage)
        require(self.pools_hold(), "stretch-pools")
        if complete is None:
            self.worker.markers.write("stretch-" + stage + "-attempt.json", record)
            self.worker.markers.write("stretch-" + stage + "-complete.json", record)
        return {"changed": changed, "stage": self.stage()}

    def observe(self):
        """Read-only: the journaled stage and whether every stage's postcondition holds now; nothing is repaired."""
        self.cache = {}
        stage = self.stage()
        enabled, unused = self.enabled_state()
        held = (stage == "verified" and enabled and self.located_holds() and self.rule_holds() and self.election_holds()
                and self.pools_hold())
        return {"stage": stage, "enabled": enabled}, held


def crush_location(value):
    """A MON's crush_location as `ceph mon dump` renders it, `{key=value,...}`; empty when unset."""
    require(isinstance(value, str) and len(value) <= 1024, "native-output")
    match = LOCATION.fullmatch(value)
    require(match is not None, "native-output")
    result = {}
    for entry in filter(None, match.group(1).split(",")):
        key, separator, item = entry.strip().partition("=")
        require(separator == "=" and named(key) and named(item) and key not in result, "native-output")
        result[key] = item
    return result


def evidence(value, request=None):
    """The closed stretch member: present exactly on a stretch cluster's evidence."""
    expected = request is None or request.get("action") == "cluster" and "stretch" in request
    if "stretch" not in value:
        require(request is None or not expected, "evidence")
        return
    member = value["stretch"]
    require(expected and value["action"] == "cluster" and isinstance(member, dict) and set(member) == {"stage", "enabled"}, "evidence")
    require(member["stage"] in ("",) + STAGES and type(member["enabled"]) is bool, "evidence")
    require(not value["ready"] or member["stage"] == "verified" and member["enabled"], "evidence")


REMOVAL_VERSION = "ceph-tiebreaker-removal-v1"
REMOVAL_ATTEMPT = "tiebreaker-removal-attempt.json"
REMOVAL_COMPLETE = "tiebreaker-removal-complete.json"
# The only daemon types of this FSID a provided tiebreaker runs: its MON and the derived crash collector.
REMOVAL_TYPES = ("mon", "crash")
RM_CLUSTER_TIMEOUT = 300
DATA_ROOT, LOG_ROOT, UNIT_ROOT, LOGROTATE_ROOT, CONF_ROOT = "/var/lib/ceph", "/var/log/ceph", "/etc/systemd/system", "/etc/logrotate.d", "/etc/ceph"
UNIT_WANTS = "ceph.target.wants"
MAX_LISTING = 4096
MAX_KEYS = 1024 * 1024
MAX_CONF = 1024 * 1024
NATIVE = ("daemons", "data", "log", "logrotate", "units", "conf")


@contextlib.contextmanager
def walked(path):
    """The directory at a fixed absolute path, each component opened without following a link; None when one is absent."""
    current = os.open("/", os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        for part in path.strip("/").split("/"):
            try:
                following = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=current)
            except FileNotFoundError:
                following = None
            except OSError:
                # A link or a non-directory where the fixed layout keeps a directory is no layout this cluster made.
                raise Refusal("tiebreaker-foreign") from None
            os.close(current)
            current = following
            if current is None:
                break
        yield current
    finally:
        if current is not None:
            os.close(current)


def present(path):
    """Whether a fixed path names anything, read without following a link."""
    parent, name = path.rsplit("/", 1)
    with walked(parent) as directory:
        if directory is None:
            return False
        try:
            os.stat(name, dir_fd=directory, follow_symlinks=False)
        except FileNotFoundError:
            return False
        return True


def listing(path):
    with walked(path) as directory:
        if directory is None:
            return []
        names = os.listdir(directory)
        require(len(names) <= MAX_LISTING, "tiebreaker-foreign")
        return sorted(names)


def removal_target(request, host):
    """The provided stretch tiebreaker this removal runs on: the one provided host, named by the stretch block."""
    stretch = request.get("stretch")
    require(request["action"] == "cluster" and isinstance(stretch, dict) and host["name"] == stretch["tiebreaker"]
            and host.get("provided") is True and host["name"] != request["bootstrap"], "request")


class TiebreakerRemoval:
    """One cluster's footprint on its provided tiebreaker: proved its own before any effect, removed, and read back absent."""

    def __init__(self, worker):
        removal_target(worker.request, worker.host)
        self.worker = worker
        self.request = worker.request
        self.host = worker.host
        self.markers = worker.markers
        self.fsid = worker.request["fsid"]
        self.record = {"version": REMOVAL_VERSION, "owner": self.request["owner"], "fsid": self.fsid, "host": self.host["name"]}

    def journal(self):
        attempt, complete = self.markers.read(REMOVAL_ATTEMPT), self.markers.read(REMOVAL_COMPLETE)
        require(attempt in (None, self.record) and complete in (None, self.record) and (complete is None or attempt is not None), "ownership")
        return attempt, complete

    def owned(self):
        """The owner marker this cluster's claim wrote, naming exactly this cluster; anything else is another's host."""
        try:
            record = self.markers.read()
        except Refusal:
            raise Refusal("tiebreaker-foreign") from None
        require(record is not None and record == marker_identity(self.request), "tiebreaker-foreign")

    def daemons(self):
        """This FSID's daemons, each a cephadm:v1 MON or crash collector of this host; no other FSID or layout under the data root."""
        require(all(name == self.fsid for name in listing(DATA_ROOT)), "tiebreaker-foreign")
        try:
            daemons = self.worker.daemons()
        except Refusal as error:
            if str(error) == "foreign-cluster":
                raise Refusal("tiebreaker-foreign") from None
            raise
        names = (self.host["name"], self.host["name"].split(".", 1)[0])
        for daemon in daemons:
            kind, separator, identifier = str(daemon.get("name", "")).partition(".")
            require(daemon.get("style") == "cephadm:v1" and daemon.get("fsid") == self.fsid and separator
                    and kind in REMOVAL_TYPES and identifier in names, "tiebreaker-foreign")
        return sorted(daemon["name"] for daemon in daemons)

    def authorization(self):
        """The cluster-key line this host's authorize phase recorded, proved against its account; None when none was recorded."""
        value = self.markers.read("ssh-authorization.json")
        if value is None:
            return None
        helper, unused = tunnel_helpers()
        require(isinstance(value, dict) and set(value) == {"uid", "home", "line"} and type(value["uid"]) is int
                and isinstance(value["home"], str) and os.path.isabs(value["home"]), "tiebreaker-foreign")
        try:
            account = pwd.getpwnam(self.request["clusterSSHUser"])
        except KeyError:
            raise Refusal("tiebreaker-foreign") from None
        require(value["uid"] == account.pw_uid > 0 and value["home"] == account.pw_dir, "tiebreaker-foreign")
        require(isinstance(value["line"], str) and len(value["line"].split()) == 4, "tiebreaker-foreign")
        public = " ".join(value["line"].split()[1:3])
        require(value["line"] == helper.authorization_line(public, self.request["owner"]), "tiebreaker-foreign")
        return value

    @contextlib.contextmanager
    def keys(self, value):
        """The account's .ssh directory, and authorized_keys' descriptor and bytes, or None when either is absent."""
        home = os.open(value["home"], os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        directory = None
        try:
            info = os.fstat(home)
            require(info.st_uid == value["uid"] and info.st_mode & 0o022 == 0, "tiebreaker-foreign")
            try:
                directory = os.open(".ssh", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=home)
            except FileNotFoundError:
                yield None, None, None
                return
            info = os.fstat(directory)
            require(info.st_uid == value["uid"] and info.st_mode & 0o077 == 0, "tiebreaker-foreign")
            try:
                fd = os.open("authorized_keys", os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
            except FileNotFoundError:
                yield directory, None, None
                return
            with os.fdopen(fd, "rb") as handle:
                info = os.fstat(handle.fileno())
                require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid in (0, value["uid"]) and info.st_mode & 0o022 == 0
                        and info.st_size <= MAX_KEYS, "tiebreaker-foreign")
                content = handle.read(MAX_KEYS + 1)
                require(len(content) == info.st_size, "tiebreaker-foreign")
                yield directory, info, content
        finally:
            if directory is not None:
                os.close(directory)
            os.close(home)

    @staticmethod
    def matching(value, content):
        """The lines naming the recorded public key: none, or exactly the recorded line."""
        blob = value["line"].split()[2]
        try:
            lines = content.decode("utf-8").splitlines()
        except UnicodeError:
            raise Refusal("tiebreaker-foreign") from None
        found = [line for line in lines if blob in line.split()]
        require(found in ([], [value["line"]]), "tiebreaker-foreign")
        return bool(found)

    def key_present(self, value):
        if value is None:
            return False
        with self.keys(value) as (unused, info, content):
            return info is not None and self.matching(value, content)

    def remove_key(self, value):
        """Rewrite authorized_keys without exactly the recorded line, keeping its owner, mode and every other byte."""
        with self.keys(value) as (directory, info, content):
            if info is None or not self.matching(value, content):
                return
            kept = b"".join(line for line in content.splitlines(keepends=True) if line.rstrip(b"\r\n") != value["line"].encode())
            stage = ".bootwright-" + uuid.uuid4().hex
            fd = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600, dir_fd=directory)
            try:
                with os.fdopen(fd, "wb") as handle:
                    os.fchown(handle.fileno(), info.st_uid, info.st_gid)
                    os.fchmod(handle.fileno(), stat.S_IMODE(info.st_mode))
                    handle.write(kept)
                    handle.flush()
                    os.fsync(handle.fileno())
                # The file read is still the one named: an edit since then is never overwritten.
                now = os.stat("authorized_keys", dir_fd=directory, follow_symlinks=False)
                require((now.st_dev, now.st_ino, now.st_size, now.st_mtime_ns) == (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns),
                        "tiebreaker-foreign")
                os.rename(stage, "authorized_keys", src_dir_fd=directory, dst_dir_fd=directory)
                os.fsync(directory)
            finally:
                with contextlib.suppress(FileNotFoundError):
                    os.unlink(stage, dir_fd=directory)

    def credentials(self):
        """The registry credential names this host's records bind, each with the metadata it must carry; any other refuses."""
        intent = self.worker.registry_intent()
        ready, pending = self.markers.read("registry-file.json"), self.markers.read("registry-pending.json")
        for record in (ready, pending):
            require(record is None or isinstance(record, dict) and set(record) == {"intent", "metadata"} and record["intent"] == intent,
                    "tiebreaker-foreign")
        if ready is not None:
            return intent, ((REGISTRY_NAME, ready["metadata"], False, (1,)), (intent["anchor"], None, False, ()))
        if pending is not None:
            return intent, ((REGISTRY_NAME, pending["metadata"], True, (1, 2)), (intent["anchor"], pending["metadata"], True, (1, 2)))
        return intent, ((REGISTRY_NAME, None, False, ()), (intent["anchor"], None, False, ()))

    def credential_present(self, remove=False):
        """Whether a recorded credential name remains; with remove, unlink each one proved by its recorded descriptor metadata."""
        unused, names = self.credentials()
        remaining = False
        try:
            with registry_directory() as directory:
                for name, metadata, pending, links in names:
                    if not registry_exists(directory, name):
                        continue
                    require(metadata is not None, "tiebreaker-foreign")
                    require(registry_read(directory, name, metadata, pending=pending, links=links) is not None, "tiebreaker-foreign")
                    if not remove:
                        remaining = True
                        continue
                    os.unlink(name, dir_fd=directory)
                    os.fsync(directory)
        except Refusal as error:
            if str(error) == "registry-ownership":
                raise Refusal("tiebreaker-foreign") from None
            raise
        return remaining

    def units(self):
        prefix = "ceph-" + self.fsid
        found = [name for name in listing(UNIT_ROOT) if name.startswith(prefix)]
        return found + [UNIT_WANTS + "/" + name for name in listing(UNIT_ROOT + "/" + UNIT_WANTS) if name.startswith(prefix)]

    def conf_names(self):
        """Whether /etc/ceph/ceph.conf names this FSID, which is when rm-cluster removes it with ceph.pub and the admin keyring."""
        with walked(CONF_ROOT) as directory:
            if directory is None:
                return False
            try:
                fd = os.open("ceph.conf", os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
            except FileNotFoundError:
                return False
            except OSError:
                raise Refusal("tiebreaker-foreign") from None
            with os.fdopen(fd, "rb") as handle:
                info = os.fstat(handle.fileno())
                require(stat.S_ISREG(info.st_mode) and info.st_size <= MAX_CONF, "tiebreaker-foreign")
                return self.fsid.encode("ascii") in handle.read(MAX_CONF + 1)

    def footprint(self, authorization):
        return {"daemons": self.daemons(), "data": present(DATA_ROOT + "/" + self.fsid), "log": present(LOG_ROOT + "/" + self.fsid),
                "logrotate": present(LOGROTATE_ROOT + "/ceph-" + self.fsid), "units": self.units(), "conf": self.conf_names(),
                "key": self.key_present(authorization), "credential": self.credential_present()}

    def prove(self):
        """Read only: this cluster's owner marker, its records, and a footprint that is only its own."""
        self.owned()
        attempt, complete = self.journal()
        authorization = self.authorization()
        return attempt, complete, authorization, self.footprint(authorization)

    def observe(self):
        attempt, unused, unused_authorization, footprint = self.prove()
        return {"removed": not any(footprint.values()), "attempted": attempt is not None}

    def rm_cluster(self):
        """The package cephadm's rm-cluster for this FSID alone, proved before any effect; never --zap-osds, never the copy under the data root."""
        tool = self.worker.tool
        require(os.path.isabs(tool) and not tool.startswith(DATA_ROOT + "/"), "request")
        self.worker.verify_tool()
        argv = [tool, "rm-cluster", "--fsid", self.fsid, "--force"]
        self.worker.require_declared_options(argv)
        return argv

    def remove(self):
        """Every proof before the attempt record; each removal only while its part remains; completion only once all is absent."""
        attempt, complete, authorization, footprint = self.prove()
        if not any(footprint.values()):
            if complete is None:
                self.markers.write(REMOVAL_ATTEMPT, self.record)
                self.markers.write(REMOVAL_COMPLETE, self.record)
            return {"changed": False, "removed": True, "attempted": True}
        require(complete is None, "tiebreaker-foreign")
        del attempt
        argv = self.rm_cluster() if any(footprint[name] for name in NATIVE) else None
        self.markers.write(REMOVAL_ATTEMPT, self.record)
        if footprint["key"]:
            self.remove_key(authorization)
        if footprint["credential"]:
            self.credential_present(remove=True)
        if argv is not None:
            self.worker.command(argv, timeout=RM_CLUSTER_TIMEOUT, json_result=False)
        require(not any(self.footprint(authorization).values()), "not-removed")
        self.markers.write(REMOVAL_COMPLETE, self.record)
        return {"changed": True, "removed": True, "attempted": True}
