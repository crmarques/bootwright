"""Each managed unit entry point makes the calls and hands the records its golden holds.

The managed network services and the artifact server run one container unit
lifecycle. Every apply, observe and destroy entry point of each kind is run
here through role_tasks.Interpreter over fixed host answers, and what it did is
recorded: each module it called with the arguments it rendered, each template
by the bytes it wrote, each protocol phase by the records its plugin handed the
runner, and the failure that ended the run. The golden under
tests/unit/goldens/managed_unit_walks holds that record per kind, so a change
that moves an effect, an argument, a record or a refusal fails here. A stat
only reads, so the files one stat loop reads are compared as a set. To rewrite
the goldens after an intended change, run this module with
BOOTWRIGHT_UPDATE_GOLDENS=1.

Rendering the role's task files needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import copy
import hashlib
import importlib
import json
import os
import pathlib
from types import SimpleNamespace

import pytest
from ansible_collections.bootwright.core.tests.unit import role_tasks
from ansible_collections.bootwright.core.tests.unit.test_role_templates import ARTIFACT_SERVER, render, service

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
GOLDENS = pathlib.Path(__file__).resolve().parent / "goldens" / "managed_unit_walks"
UPDATE = os.environ.get("BOOTWRIGHT_UPDATE_GOLDENS") == "1"
PROTOCOLS = "ansible_collections.bootwright.core.plugins.action."

DIGEST = "0123456789abcdef" * 4
STARTED = 1790779100675992231
EARLIER = STARTED / 1e9 - 3600

# Each kind's entry point: the role, the variable prefix its run is handed
# under, and the task file of each operation.
ENTRIES = {
    "managed-service": ("infra_managed_service", "bootwright_managed_service", {
        "apply": "apply.yml", "observe": "observe.yml", "destroy": "destroy.yml"}),
    "artifact-server": ("infra_artifact_server_nginx", "bootwright_artifact_server", {
        "apply": "apply.yml", "observe": "observe.yml", "destroy": "destroy.yml"}),
}

PLAIN_SERVER = dict(ARTIFACT_SERVER, endpoints=ARTIFACT_SERVER["endpoints"][:1], listeners=ARTIFACT_SERVER["listeners"][:1])
del PLAIN_SERVER["tls"]

# Each walked service: its entry, its frozen request and the material names its run lends.
KINDS = {
    "dns": ("managed-service", service("DNSServer", "dns-server-dnsmasq", "dns", 53, forwarders=["198.51.100.53"], records=[
        {"addresses": ["192.0.2.11"], "name": "rhel-01.lab.example.test"}]), ()),
    "ntp": ("managed-service", service("NTPServer", "ntp-server-chrony", "time", 123, clients=["192.0.2.0/24"],
                                       sources=["198.51.100.123"]), ()),
    "proxy": ("managed-service", service("Proxy", "proxy-squid", "egress", 3128, clients=["192.0.2.0/24"],
                                         destinations=[{"domain": ".example.test"}]), ()),
    "registry": ("managed-service", service("Registry", "registry", "mirror", 5000, credentialsRef="mirror-push",
                                            labelLevel="s0:c130,c165", tls={"minVersion": "TLSv1.3", "secret": "mirror-serving"}),
                 ("certificate", "privateKey", "clientUsername", "clientPassword", "passwordFile")),
    "artifact-tls": ("artifact-server", ARTIFACT_SERVER, ("certificate", "privateKey")),
    "artifact-plain": ("artifact-server", PLAIN_SERVER, ()),
}

RUNNING = {"unit": "active", "container": "registry.example.test/x@sha256:" + DIGEST, "containerPresent": True,
           "contentRoot": True, "startedAfterFiles": True}
GONE = {"unit": "", "container": "", "containerPresent": False, "contentRoot": False, "startedAfterFiles": False}

# Each host the walks meet: what podman, the socket and address reads, the
# plain inspections in order, the readiness read and every effect answer.
HOST = {"podman": 0, "assigned": True, "foreign": False, "plain": [GONE], "after": RUNNING, "failed": False,
        "changed": True, "accepted": False, "listed": "inactive"}
SCENARIOS = {
    "apply": {
        "fresh": {},
        "replay": {"plain": [RUNNING], "changed": False, "accepted": True},
        "no podman": {"podman": 127},
        "unassigned address": {"assigned": False},
        "foreign listener": {"foreign": True},
        "unready": {"failed": True},
        "unpinned image": {"request": {"image": "registry.example.test/x:latest"}},
    },
    "observe": {
        "running": {},
        "absent": {"after": GONE},
    },
    "destroy": {
        "present": {"plain": [RUNNING, GONE], "listed": "failed"},
        "absent": {"plain": [GONE, GONE]},
        "shallow content root": {"request": {"contentRoot": "/var/lib/bootwright-hosted/lab-rhel/services/x"}},
    },
}


def port_of(request):
    return request["listeners"][0]["port"] if "listeners" in request else request["port"]


def answers(request, host):
    """What a readiness read answers for each declared address or listener."""
    if "listeners" in request:
        return {"listeners": [{"address": "192.0.2.1", "fingerprint": DIGEST if item["protocol"] == "https" else "",
                               "name": item["name"], "port": item["port"], "protocol": item["protocol"], "status": "HTTP/1.1 200 OK"}
                              for item in request["listeners"]]}
    entry = {"address": "192.0.2.1", "answer": "answered", "port": request["port"]}
    if request["kind"] == "Registry":
        entry["fingerprint"] = DIGEST
    return {"answers": [entry], "accepted": host["accepted"]}


class Walk:
    """One entry point's run over one host, recording what it did."""

    def __init__(self, kind, operation, scenario, material):
        entry, request, lent = KINDS[kind]
        self.role, prefix, files = ENTRIES[entry]
        self.file = files[operation]
        self.host = dict(HOST, **SCENARIOS[operation][scenario])
        self.host["plain"] = list(self.host["plain"])
        self.request = dict(copy.deepcopy(request), **self.host.get("request", {}))
        self.calls = []
        self.variables = role_tasks.role_defaults(ROLES / self.role)
        fixed = ROLES / self.role / "vars" / "main.yml"
        if fixed.is_file():
            self.variables.update(role_tasks.load_mapping(fixed))
        self.material = {name: str(material / name) for name in lent}
        if "certificate" in lent:
            self.material["fingerprint"] = DIGEST
        self.variables.update({prefix + "_request": self.request, prefix + "_digest": DIGEST, prefix + "_material": self.material})
        self.interpreter = role_tasks.Interpreter(ROLES / self.role, self.variables, self.execute)

    def run(self):
        failure = None
        try:
            self.interpreter.run(self.file)
        except role_tasks.TaskFailed as failed:
            failure = failed.result.get("msg")
        return {"calls": settled(self.calls), "failure": failure}

    def execute(self, task, templar):
        module = next(key for key in task if key.startswith(("ansible.builtin.", "bootwright.core.")))
        if module in ("ansible.builtin.assert", "ansible.builtin.fail", "ansible.builtin.set_fact",
                      "ansible.builtin.include_tasks", "ansible.builtin.import_tasks"):
            return None
        if module == "ansible.builtin.include_role":
            return self.include(task, templar)
        arguments = rendered(templar.template(task[module]))
        if module.endswith("_protocol"):
            return self.protocol(module, arguments)
        if module.endswith("_inspect"):
            self.calls.append({"module": "inspect", "arguments": arguments})
            return self.inspect(arguments)
        if module == "ansible.builtin.template":
            written = render(self.role, arguments["src"], dict(templar.available_variables))
            arguments = {key: value for key, value in arguments.items() if key != "src"}
            arguments["sha256"] = hashlib.sha256(written.encode()).hexdigest()
        self.calls.append({"module": module, "arguments": arguments})
        return self.answer(module, arguments)

    def include(self, task, templar):
        included = task["ansible.builtin.include_role"]
        role = ROLES / included["name"].removeprefix("bootwright.core.")
        scope = {key: templar.template(value) for key, value in (task.get("vars") or {}).items()}
        self.interpreter.perform(role_tasks.trusted(role_tasks.role_tasks(role, included["tasks_from"] + ".yml")), (), {}, scope)
        return {"changed": False}

    def protocol(self, module, arguments):
        plugin = importlib.import_module(PROTOCOLS + module.removeprefix("bootwright.core."))
        records = []
        original = plugin.emit
        plugin.emit = lambda record, acknowledge=False: records.append([record, acknowledge])
        try:
            action = plugin.ActionModule.__new__(plugin.ActionModule)
            action._task = SimpleNamespace(args=arguments)
            result = action.run(task_vars={})
        finally:
            plugin.emit = original
        self.calls.append({"module": "protocol", "records": records, "result": result})
        return result

    def inspect(self, arguments):
        port = port_of(self.request)
        if arguments.get("foreign") or arguments.get("bind_address"):
            foreign = [{"transport": "tcp", "address": "192.0.2.1", "port": port}] if self.host["foreign"] else []
            return {"changed": False, "observation": GONE, "unproved": [], "bindAddressAssigned": self.host["assigned"],
                    "foreign": foreign}
        if not arguments.get("readiness"):
            return {"changed": False, "observation": self.host["plain"].pop(0), "unproved": []}
        result = {"changed": False, "observation": self.host["after"], "unproved": [], "failures": []}
        result.update(answers(self.request, self.host))
        if self.host["failed"]:
            result["failures"] = [{"port": port, "reason": "listener-failed"}]
            result["unproved"] = ["192.0.2.1:%d (ConnectionRefusedError(111): Connection refused)" % port]
        return result

    def answer(self, module, arguments):
        changed = self.host["changed"]
        if module == "ansible.builtin.stat":
            return {"changed": False, "stat": {"exists": True, "isreg": True, "mtime": EARLIER}}
        if module == "ansible.builtin.command":
            return self.command(arguments["argv"])
        if module == "bootwright.core.image_acquire":
            return {"changed": changed, "image": arguments["image"]}
        return {"changed": changed}

    def command(self, argv):
        if argv[:2] == ["/usr/bin/podman", "--version"]:
            return {"rc": self.host["podman"], "stdout": "podman version 5.8.4" if self.host["podman"] == 0 else ""}
        if argv[:2] == ["/usr/bin/podman", "inspect"]:
            return {"rc": 0, "stdout": str(STARTED)}
        if argv[:2] == ["/usr/bin/systemctl", "show"]:
            return {"rc": 0, "stdout": self.host["listed"]}
        return {"rc": 0, "stdout": "", "stderr": "", "changed": True}


def rendered(value):
    """Arguments as the module receives them: an omitted argument absent, every value plain data."""
    if isinstance(value, dict):
        return {str(key): rendered(item) for key, item in value.items() if type(item).__name__ != "Omit"}
    if isinstance(value, (list, tuple)):
        return [rendered(item) for item in value]
    if isinstance(value, (bool, int, float)) or value is None:
        return value
    return str(value)


def settled(calls):
    """The calls, the paths each run of consecutive stats read in one order."""
    found, run = [], []
    for call in calls + [None]:
        if call is not None and call.get("module") == "ansible.builtin.stat":
            run.append(call)
            continue
        found.extend(sorted(run, key=lambda item: item["arguments"]["path"]))
        run = []
        if call is not None:
            found.append(call)
    return found


def walked(kind, material):
    text = json.dumps({operation: {scenario: Walk(kind, operation, scenario, material).run() for scenario in scenarios}
                       for operation, scenarios in SCENARIOS.items()}, indent=2, sort_keys=True) + "\n"
    return text.replace(str(material), "/run/bootwright/material")


@pytest.mark.parametrize("kind", sorted(KINDS))
def test_each_entry_point_makes_the_calls_and_hands_the_records_its_golden_holds(kind, tmp_path):
    material = tmp_path / "material"
    material.mkdir()
    for name in ("certificate", "privateKey", "clientUsername", "clientPassword", "passwordFile"):
        (material / name).write_text("lent %s\n" % name)
    written = walked(kind, material)
    golden = GOLDENS / (kind + ".json")
    if UPDATE:
        golden.parent.mkdir(parents=True, exist_ok=True)
        golden.write_text(written)
    assert golden.read_text() == written, "%s differs; rerun with BOOTWRIGHT_UPDATE_GOLDENS=1 if intended" % golden.name


def test_a_walk_sees_a_change_in_what_an_entry_point_does(tmp_path):
    first = Walk("dns", "apply", "fresh", tmp_path).run()
    other = Walk("dns", "apply", "no podman", tmp_path).run()
    assert first != other and other["failure"] and not first["failure"]
