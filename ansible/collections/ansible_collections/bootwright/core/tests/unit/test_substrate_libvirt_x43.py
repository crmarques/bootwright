"""A libvirt provider host and its machines touch only what is exactly their own.

A same-named network is this context's only when its ownership metadata names
this context and that network, and a same-named pool only when it targets the
frozen directory; anything else is foreign and refuses on apply and on destroy
before any effect. An external attachment is proved as a bridge device. Each
network and the pool are set to autostart only once they run, and a destroy
removes the provider's and the context's directories only once each is empty.

A same-named domain is this context's only when its ownership names this
context and Machine and its UUID is the frozen one. The emulated BMC's image is
pulled only when absent, through the frozen egress; its unit stops it with
SIGINT and its configuration renders every request value as data. A machine
removal names its context in the stop it asks for and removes the context's
empty parents.

Evaluating the role's guards needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import ast
import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

from ansible_collections.bootwright.core.plugins.module_utils import substrate_libvirt
from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import (
    bridge_present,
    domain_metadata,
    network_state,
    pool_state,
)

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "substrate_libvirt_host"
LOADER = DataLoader()
URI = "qemu:///system"
NAME = "bootwright-lab-guests"
POOL = "bootwright-lab-host-vmedia"
POOL_PATH = "/var/lib/libvirt/images/bootwright/lab/host/vmedia"
FROZEN = {"name": NAME, "bridge": "virbr-lab", "address": "198.51.100.1/24", "forward": "nat", "managed": True}
REQUEST = {"identity": {"context": "lab"}, "poolName": POOL, "poolPath": POOL_PATH, "uri": URI}


def network_xml(context, attachment):
    return """<network>
  <name>%s</name>
  <uuid>4c0a4300-aa43-458c-86d7-ac2256d1fc00</uuid>
  <bridge name="virbr-lab" zone="trusted" stp="on" delay="0"/>
  <metadata>
    <bw:owner xmlns:bw="https://bootwright.io/substrate/v1">
      <bw:context>%s</bw:context>
      <bw:attachment>%s</bw:attachment>
    </bw:owner>
  </metadata>
</network>""" % (NAME, context, attachment)


def runner_for(answers):
    """A runner that replies to exact virsh argument vectors and refuses anything else."""

    def run(argv, check_rc=False, environ_update=None):
        del check_rc, environ_update
        return answers.get(" ".join(argv[3:]), (1, "", "error: unexpected\n"))

    return run


def network_runner(xml, info="Name:           %s\nActive:         yes\nAutostart:      yes\n" % NAME):
    return runner_for({
        "net-dumpxml " + NAME: (0, xml, ""),
        "net-dumpxml --inactive " + NAME: (0, xml, ""),
        "net-info " + NAME: (0, info, ""),
    })


def pool_xml(path):
    return "<pool type='dir'><name>%s</name><target><path>%s</path><permissions/></target></pool>" % (POOL, path)


def load(name):
    loaded = LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True)
    return [task for task in loaded if isinstance(task, dict)]


def argv_of(task):
    return [str(value) for value in (task.get("ansible.builtin.command") or {}).get("argv") or []]


def index_of(tasks, predicate, what):
    found = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(found) == 1, "expected one task that %s, found %d" % (what, len(found))
    return found[0]


def that(task):
    return [str(condition) for condition in (task.get("ansible.builtin.assert") or {}).get("that") or []]


def holds(task, variables):
    templar = Templar(loader=LOADER, variables=variables)
    return all(templar.evaluate_conditional(trust_as_template(condition)) for condition in that(task))


@pytest.mark.parametrize("context, attachment", [("other", NAME), ("lab", "bootwright-lab-other")],
                         ids=["another context", "another attachment"])
def test_another_contexts_network_is_foreign_and_never_drifted(context, attachment):
    state = network_state(network_runner(network_xml(context, attachment)), URI, NAME, FROZEN, "lab")
    assert (state["answered"], state["state"], state["owned"], state["drifted"]) == (True, "active", False, False)
    exact = network_state(network_runner(network_xml("lab", NAME)), URI, NAME, FROZEN, "lab")
    assert exact["owned"] is True


def guards(tasks, register):
    """The foreign-network and foreign-pool guards of one task file, by index."""
    network = index_of(tasks, lambda task: that(task) == ["item.state | length == 0 or item.owned"], "refuses a foreign network")
    pool = index_of(
        tasks,
        lambda task: that(task) == ["%s.observation.pool | length == 0 or %s.observation.poolOwned" % (register, register)],
        "refuses a foreign pool",
    )
    return network, pool


@pytest.mark.parametrize("name, register, effects", [
    ("apply.yml", "substrate_libvirt_host_running", ("net-define", "pool-define-as", "net-destroy", "net-start", "pool-start")),
    ("destroy.yml", "substrate_libvirt_host_before", ("net-destroy", "net-undefine", "pool-destroy", "pool-undefine")),
])
def test_a_foreign_network_or_pool_refuses_before_any_effect_on_apply_and_on_destroy(name, register, effects):
    tasks = load(name)
    network, pool = guards(tasks, register)
    base = {"bootwright_substrate_host_request": REQUEST}
    assert not holds(tasks[network], dict(base, item={"name": NAME, "state": "active", "owned": False}))
    assert holds(tasks[network], dict(base, item={"name": NAME, "state": "active", "owned": True}))
    assert holds(tasks[network], dict(base, item={"name": NAME, "state": "", "owned": False}))
    for pooled, owned, accepted in (("active", False, False), ("inactive", False, False), ("active", True, True), ("", False, True)):
        variables = dict(base, **{register: {"observation": {"pool": pooled, "poolOwned": owned}}})
        assert holds(tasks[pool], variables) is accepted, (pooled, owned)
    for effect in effects:
        first = min(index for index, task in enumerate(tasks) if effect in argv_of(task))
        assert max(network, pool) < first, "%s runs %s before refusing a foreign network or pool" % (name, effect)


def test_a_pool_is_owned_only_when_it_targets_the_frozen_directory():
    info = "Name:           %s\nState:          running\nAutostart:      yes\n" % POOL
    for path, owned in ((POOL_PATH, True), ("/var/lib/libvirt/images/other", False), (POOL_PATH + "/nested", False)):
        runner = runner_for({"pool-info " + POOL: (0, info, ""), "pool-dumpxml " + POOL: (0, pool_xml(path), "")})
        assert pool_state(runner, URI, POOL, POOL_PATH) == {"answered": True, "state": "active", "autostart": True, "owned": owned}
    unreadable = runner_for({"pool-info " + POOL: (0, info, ""), "pool-dumpxml " + POOL: (0, "not xml", "")})
    assert pool_state(unreadable, URI, POOL, POOL_PATH)["owned"] is False
    silent = runner_for({"pool-info " + POOL: (1, "", "error: failed to get pool\n"), "pool-list --all --name": (1, "", "")})
    assert pool_state(silent, URI, POOL, POOL_PATH) == {"answered": False, "state": "", "autostart": False, "owned": False}


# `virsh net-info` and `pool-info` print one padded label per line, Autostart
# among them (cmdNetworkInfo in tools/virsh-network.c and cmdPoolInfo in
# tools/virsh-pool.c, https://gitlab.com/libvirt/libvirt).
@pytest.mark.parametrize("reported, autostart", [("yes", True), ("no", False)])
def test_the_observation_reads_autostart(reported, autostart):
    network_info = "Name:           %s\nUUID:           u\nActive:         yes\nPersistent:     yes\nAutostart:      %s\nBridge:         virbr-lab\n"
    state = network_state(network_runner(network_xml("lab", NAME), network_info % (NAME, reported)), URI, NAME, FROZEN, "lab")
    assert state["autostart"] is autostart
    pool_info = "Name:           %s\nUUID:           u\nState:          running\nPersistent:     yes\nAutostart:      %s\n"
    runner = runner_for({"pool-info " + POOL: (0, pool_info % (POOL, reported), ""), "pool-dumpxml " + POOL: (0, pool_xml(POOL_PATH), "")})
    assert pool_state(runner, URI, POOL, POOL_PATH)["autostart"] is autostart


def test_autostart_follows_a_successful_start():
    tasks = load("apply.yml")

    def at(verb):
        return index_of(tasks, lambda task: verb in argv_of(task), "runs " + verb)

    assert at("net-start") < at("net-autostart")
    assert at("pool-start") < at("pool-autostart")


def test_an_external_bridge_is_proved_by_its_bridge_directory(monkeypatch):
    asked = []
    monkeypatch.setattr(substrate_libvirt.os.path, "isdir", lambda path: asked.append(path) or True)
    assert bridge_present("br0") is True
    assert asked == ["/sys/class/net/br0/bridge"]


def test_a_destroy_removes_each_empty_parent_never_recursively():
    tasks = load("destroy.yml")
    directory = index_of(tasks, lambda task: task.get("name") == "Remove the owned pool directory", "removes the pool directory")
    parents = index_of(tasks, lambda task: argv_of(task)[:1] == ["/usr/bin/rmdir"], "removes the empty parents")
    assert parents == directory + 1
    assert argv_of(tasks[parents]) == ["/usr/bin/rmdir", "{{ item }}"]
    templar = Templar(loader=LOADER, variables={"bootwright_substrate_host_request": REQUEST})
    assert templar.template(tasks[parents]["loop"]) == [
        "/var/lib/libvirt/images/bootwright/lab/host", "/var/lib/libvirt/images/bootwright/lab",
    ]
    validate = index_of(tasks, lambda task: task.get("name") == "Validate the frozen provider host request", "validates the request")
    base = {"bootwright_substrate_host_digest": "0" * 64}
    request = dict(REQUEST, version="substrate-host-libvirt-v3")
    assert holds(tasks[validate], dict(base, bootwright_substrate_host_request=request))
    shallow = dict(request, poolPath="/var/lib/libvirt/images/bootwright/lab/vmedia")
    assert not holds(tasks[validate], dict(base, bootwright_substrate_host_request=shallow))


def conditions(value):
    return value if isinstance(value, list) else [value]


@pytest.mark.parametrize("result, failed, changed", [
    ({"rc": 0, "stderr": ""}, False, True),
    ({"rc": 1, "stderr": "rmdir: failed to remove '/x': Directory not empty"}, False, False),
    ({"rc": 1, "stderr": "rmdir: failed to remove '/x': No such file or directory"}, False, False),
    ({"rc": 1, "stderr": "rmdir: failed to remove '/x': Permission denied"}, True, False),
], ids=["removed", "still holds another provider", "already gone", "refused"])
def test_a_parent_that_is_not_empty_or_already_gone_never_fails_the_destroy(result, failed, changed):
    tasks = load("destroy.yml")
    task = tasks[index_of(tasks, lambda task: argv_of(task)[:1] == ["/usr/bin/rmdir"], "removes the empty parents")]
    assert task["environment"] == {"LC_ALL": "C"}
    assert task["register"] == "substrate_libvirt_host_parents"
    templar = Templar(loader=LOADER, variables={"substrate_libvirt_host_parents": result})
    evaluated = all(templar.evaluate_conditional(trust_as_template(item)) for item in conditions(task["failed_when"]))
    assert evaluated is failed
    evaluated = all(templar.evaluate_conditional(trust_as_template(item)) for item in conditions(task["changed_when"]))
    assert evaluated is changed


MACHINE_ROLE = ROLE.parent / "substrate_libvirt_machine"
DOMAIN = "bootwright-lab-rhel-01"
DOMAIN_UUID = "7b9ec716-85d4-8e28-84d3-f0d571d55f15"
EGRESS = {
    "httpProxy": "http://proxy.example.test:3128",
    "httpsProxy": "http://proxy.example.test:3128",
    "noProxy": ["192.0.2.0/24", "lab.example.test"],
}
MACHINE = {
    "controller": {"address": "192.0.2.1", "image": "registry.example.test/sushy-tools@sha256:" + "0" * 64, "port": 8001,
                   "unit": "bootwright-lab-rhel-01-bmc"},
    "directory": "/var/lib/libvirt/images/bootwright/lab/rhel-01",
    "domain": DOMAIN,
    "egress": EGRESS,
    "identity": {"block": "machine-rhel-01", "context": "lab", "object": "rhel-01"},
    "poolName": POOL,
    "uri": URI,
    "uuid": DOMAIN_UUID,
}


def load_machine(name):
    loaded = LOADER.load_from_file(str(MACHINE_ROLE / "tasks" / name), trusted_as_template=True)
    return [task for task in loaded if isinstance(task, dict)]


def machine_variables(**extra):
    variables = dict(LOADER.load_from_file(str(MACHINE_ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    variables["bootwright_substrate_machine_request"] = MACHINE
    variables.update(extra)
    return variables


def named(tasks, name):
    return index_of(tasks, lambda task: task.get("name") == name, "is named %r" % name)


def evaluate(value, variables):
    templar = Templar(loader=LOADER, variables=variables)
    return all(templar.evaluate_conditional(trust_as_template(str(item))) for item in conditions(value))


def test_the_controller_pull_carries_the_frozen_egress_and_skips_a_present_image():
    tasks = load_machine("apply.yml")
    check = named(tasks, "Check whether the pinned controller image is already present")
    pull = named(tasks, "Acquire the pinned controller image")
    assert check < pull
    assert argv_of(tasks[check])[:3] == ["/usr/bin/podman", "image", "exists"]
    assert argv_of(tasks[pull])[:2] == ["/usr/bin/podman", "pull"]
    templar = Templar(loader=LOADER, variables=machine_variables())
    environment = templar.template(tasks[pull]["environment"])
    proxy, exempt = "http://proxy.example.test:3128", "192.0.2.0/24,lab.example.test"
    assert environment == {
        "HTTP_PROXY": proxy, "HTTPS_PROXY": proxy, "NO_PROXY": exempt,
        "http_proxy": proxy, "https_proxy": proxy, "no_proxy": exempt,
    }
    for rc, pulled in ((0, False), (1, True)):
        variables = machine_variables(substrate_libvirt_machine_image_present={"rc": rc})
        assert evaluate(tasks[pull].get("when", True), variables) is pulled, rc
        assert evaluate(tasks[pull]["changed_when"], variables) is pulled, rc


def domain_xml(context, machine, uuid):
    return """<domain type="kvm">
  <name>%s</name>
  <uuid>%s</uuid>
  <metadata>
    <bw:owner xmlns:bw="https://bootwright.io/substrate/v1">
      <bw:context>%s</bw:context>
      <bw:machine>%s</bw:machine>
    </bw:owner>
  </metadata>
</domain>""" % (DOMAIN, uuid, context, machine)


@pytest.mark.parametrize("context, machine, uuid, owned", [
    ("lab", "rhel-01", DOMAIN_UUID, True),
    ("other", "rhel-01", DOMAIN_UUID, False),
    ("lab", "rhel-02", DOMAIN_UUID, False),
    ("lab", "rhel-01", "00000000-0000-8000-8000-000000000000", False),
], ids=["exact", "another context", "another machine", "another uuid"])
def test_another_contexts_domain_or_another_uuid_is_foreign(context, machine, uuid, owned):
    runner = runner_for({"dumpxml " + DOMAIN: (0, domain_xml(context, machine, uuid), "")})
    metadata = domain_metadata(runner, URI, DOMAIN, "lab", "rhel-01", DOMAIN_UUID)
    assert metadata == {"answered": True, "present": True, "owned": owned, "uuid": uuid}


@pytest.mark.parametrize("name, effect", [
    ("apply.yml", lambda task: task.get("name") == "Create the owned disk directory"),
    ("destroy.yml", lambda task: argv_of(task)[:2] == ["/usr/bin/systemctl", "stop"]),
])
def test_a_foreign_domain_refuses_before_any_effect_on_apply_and_on_destroy(name, effect):
    tasks = load_machine(name)
    condition = "substrate_libvirt_machine_before.observation.domain | length == 0 or substrate_libvirt_machine_before.observation.owned"
    guard = index_of(tasks, lambda task: that(task) == [condition], "refuses a foreign domain")
    for domain, owned, accepted in ((DOMAIN, False, False), (DOMAIN, True, True), ("", False, True)):
        variables = machine_variables(substrate_libvirt_machine_before={"observation": {"domain": domain, "owned": owned}})
        assert holds(tasks[guard], variables) is accepted, (domain, owned)
    assert guard < index_of(tasks, effect, "is the first effect")
    message = Templar(loader=LOADER, variables=machine_variables()).template(tasks[guard]["ansible.builtin.assert"]["fail_msg"])
    assert DOMAIN in message and "Machine rhel-01" in message and DOMAIN_UUID in message


def render_machine(template, request):
    templar = Templar(loader=LOADER, variables=machine_variables(bootwright_substrate_machine_request=request))
    return str(templar.template(trust_as_template((MACHINE_ROLE / "templates" / template).read_text())))


def settings(text, key):
    return [value for name, _separator, value in (line.partition("=") for line in text.splitlines()) if name.strip() == key]


def test_the_emulator_stops_with_sigint():
    assert settings(render_machine("unit.container.j2", MACHINE), "StopSignal") == ["SIGINT"]


def test_the_emulator_configuration_renders_the_uri_as_data():
    configuration = render_machine("emulator.conf.j2", MACHINE)
    assert settings(configuration, "SUSHY_EMULATOR_LIBVIRT_URI") == [' "qemu:///system"']
    hostile = "qemu:///system'+__import__(\"os\")+'"
    configuration = render_machine("emulator.conf.j2", dict(MACHINE, uri=hostile, poolName=hostile))
    for key in ("SUSHY_EMULATOR_LIBVIRT_URI", "SUSHY_EMULATOR_STORAGE_POOL"):
        (value,) = settings(configuration, key)
        assert ast.literal_eval(value.strip()) == hostile, key


def test_a_machine_destroy_removes_each_empty_context_parent():
    tasks = load_machine("destroy.yml")
    templar = Templar(loader=LOADER, variables=machine_variables())
    for removal, expected in (
        ("Remove the controller's private state", "/var/lib/bootwright-substrate/lab"),
        ("Delete every owned disk", "/var/lib/libvirt/images/bootwright/lab"),
    ):
        parent = tasks[named(tasks, removal) + 1]
        argv = [str(templar.template(trust_as_template(value))) for value in argv_of(parent)]
        assert argv == ["/usr/bin/rmdir", expected], removal
        assert parent["environment"] == {"LC_ALL": "C"}
        outcome = tasks[named(tasks, "Publish bounded machine removal evidence")]
        assert parent["register"] + " is changed" in str(outcome["bootwright.core.substrate_machine_protocol"]["outcome"])
        for result, failed in (
            ({"rc": 0, "stderr": ""}, False),
            ({"rc": 1, "stderr": "rmdir: failed to remove 'x': Directory not empty"}, False),
            ({"rc": 1, "stderr": "rmdir: failed to remove 'x': No such file or directory"}, False),
            ({"rc": 1, "stderr": "rmdir: failed to remove 'x': Permission denied"}, True),
        ):
            assert evaluate(parent["failed_when"], {parent["register"]: result}) is failed, (removal, result)
    validate = named(tasks, "Validate the frozen machine request")
    base = {"bootwright_substrate_machine_digest": "0" * 64}
    assert holds(tasks[validate], dict(base, bootwright_substrate_machine_request=dict(MACHINE, version="machine-libvirt-v3")))
    shallow = dict(MACHINE, version="machine-libvirt-v3", directory="/var/lib/libvirt/images/bootwright/rhel-01")
    assert not holds(tasks[validate], dict(base, bootwright_substrate_machine_request=shallow))


def test_the_not_shut_off_refusal_names_its_context():
    tasks = load_machine("destroy.yml")
    guard = tasks[named(tasks, "Refuse to remove a machine that is not shut off")]
    message = Templar(loader=LOADER, variables=machine_variables()).template(guard["ansible.builtin.assert"]["fail_msg"])
    assert "bootwright machine stop --context lab --name rhel-01" in message
