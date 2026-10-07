"""One role realizes every managed network service, keyed by the request's frozen kind.

The DNS server, NTP server and proxy were one role copied three times. The one
role's argument spec admits exactly the frozen kinds and the one request
version they share, and each kind selects only its own daemon configuration and
unit templates from a fixed table, so every task is shared.

The proxy refuses loopback, unspecified and link-local destinations and its
cache manager before it admits any derived client, and it stops within
podman's stop timeout. A destroy of a managed service or the artifact server
removes the kind's and the context's directories once each is empty, never
recursively, and only when the content root sits exactly at
<prefix>/<context>/<kind>/<name>. Evaluating those guards needs Ansible's
controller (DataLoader and Templar), so these checks live here rather than
under tests/unit/plugins.
"""

from __future__ import annotations

import importlib
import pathlib
import re
from types import SimpleNamespace

import pytest
import yaml
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
ROLE = COLLECTION / "roles" / "infra_managed_service"
PLAYBOOKS = COLLECTION / "playbooks" / "infrastructureservices"
SERVICES = ("dns_server", "ntp_server", "proxy")
OPERATIONS = ("apply", "observe", "destroy")
ROLES = COLLECTION / "roles"
SQUID = COLLECTION / "tests" / "unit" / "goldens" / "templates" / "infra_managed_service" / "squid.conf"
LOADER = DataLoader()
PODMAN_STOP_TIMEOUT = 10


def kinds():
    return yaml.safe_load((ROLE / "vars" / "main.yml").read_text())["infra_managed_service_kinds"]


def imported_roles(playbook):
    found = []
    for play in yaml.safe_load(playbook.read_text()) or []:
        for task in play.get("tasks") or []:
            for module in ("ansible.builtin.import_role", "ansible.builtin.include_role"):
                if isinstance(task.get(module), dict):
                    found.append((task[module]["name"], task[module].get("tasks_from")))
    return found


@pytest.mark.parametrize("service", SERVICES)
@pytest.mark.parametrize("operation", OPERATIONS)
def test_one_role_serves_every_managed_network_service(service, operation):
    playbook = PLAYBOOKS / ("%s_%s.yml" % (service, operation))
    assert imported_roles(playbook) == [("bootwright.core.infra_managed_service", operation)]


@pytest.mark.parametrize("retired", ["infra_dns_server_dnsmasq", "infra_ntp_server_chrony", "infra_proxy_squid"])
def test_no_copied_managed_service_role_remains(retired):
    assert not (COLLECTION / "roles" / retired).exists()


def test_the_argument_spec_keys_the_request_by_its_frozen_kind():
    specs = yaml.safe_load((ROLE / "meta" / "argument_specs.yml").read_text())["argument_specs"]
    assert sorted(specs) == sorted(OPERATIONS)
    options = [specs[operation]["options"] for operation in OPERATIONS]
    assert options[0] == options[1] == options[2]
    request = options[0]["bootwright_managed_service_request"]["options"]
    assert request["kind"]["choices"] == sorted(kinds()) == ["DNSServer", "NTPServer", "Proxy"]
    assert request["version"]["choices"] == ["managed-service-v3"]


def test_each_kind_selects_only_its_own_templates():
    selected = []
    for kind in kinds().values():
        selected += [kind["configuration_template"], kind["unit_template"]]
    carried = sorted(path.name for path in (ROLE / "templates").iterdir())
    assert len(selected) == len(set(selected)), "two kinds share a template: %s" % selected
    assert sorted(selected) == carried


def squid_lines():
    return [line.strip() for line in SQUID.read_text().splitlines() if line.strip()]


def acl_tokens(name):
    found = [line.split()[3:] for line in squid_lines() if line.split()[:3] == ["acl", name, "dst"]]
    assert len(found) == 1, "expected one dst acl named %s, found %d" % (name, len(found))
    return set(found[0])


def test_the_proxy_refuses_local_link_local_and_manager_destinations_before_it_admits_a_client():
    lines = squid_lines()
    access = [" ".join(line.split()[1:]) for line in lines if line.split()[0] == "http_access"]
    assert access == [
        "deny manager",
        "deny bootwright_local_destinations",
        "deny bootwright_link_local_destinations",
        "allow bootwright_clients",
        "deny all",
    ]
    assert acl_tokens("bootwright_local_destinations") == {"127.0.0.0/8", "0.0.0.0/32", "::1/128", "::/128"}
    assert acl_tokens("bootwright_link_local_destinations") == {"169.254.0.0/16", "fe80::/10"}
    for line in lines:
        assert not re.search(r"CONNECT|SSL_ports|Safe_ports", line), line
        assert line.split()[:2] != ["acl", "manager"], line


def test_the_proxy_stops_within_podmans_stop_timeout():
    found = [line.split() for line in squid_lines() if line.split()[0] == "shutdown_lifetime"]
    assert len(found) == 1, found
    assert len(found[0]) == 3 and found[0][2] == "seconds", found
    assert int(found[0][1]) <= PODMAN_STOP_TIMEOUT // 2


def load_tasks(role, name):
    loaded = LOADER.load_from_file(str(ROLES / role / "tasks" / name), trusted_as_template=True)
    return [task for task in loaded if isinstance(task, dict)]


def argv_of(task):
    return [str(value) for value in (task.get("ansible.builtin.command") or {}).get("argv") or []]


def index_of(tasks, predicate, what):
    found = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(found) == 1, "expected one task that %s, found %d" % (what, len(found))
    return found[0]


def holds(task, variables):
    templar = Templar(loader=LOADER, variables=variables)
    conditions = [str(condition) for condition in task["ansible.builtin.assert"]["that"]]
    return all(templar.evaluate_conditional(trust_as_template(condition)) for condition in conditions)


def validate_of(tasks):
    return tasks[index_of(tasks, lambda task: str(task.get("name", "")).startswith("Validate the frozen"), "validates the request")]


MANAGED = {
    "version": "managed-service-v3",
    "kind": "DNSServer",
    "unit": "bootwright-lab-dns-resolver",
    "identity": {"context": "lab"},
    "contentRoot": "/var/lib/bootwright-services/lab/dns/resolver",
    "image": "registry.example/dnsmasq@sha256:" + "0" * 64,
    "port": 53,
}
ARTIFACT = {
    "version": "artifact-server-nginx-v2",
    "identity": {"context": "lab"},
    "contentRoot": "/var/lib/bootwright-services/lab/artifact-server/media",
}
DESTROYS = [
    ("infra_managed_service", "bootwright_managed_service", MANAGED, ["/var/lib/bootwright-services/lab/dns", "/var/lib/bootwright-services/lab"]),
    ("infra_artifact_server_nginx", "bootwright_artifact_server", ARTIFACT,
     ["/var/lib/bootwright-services/lab/artifact-server", "/var/lib/bootwright-services/lab"]),
]


def variables(prefix, request):
    found = {prefix + "_request": request, prefix + "_digest": "0" * 64}
    if prefix == "bootwright_managed_service":
        found["infra_managed_service_kinds"] = kinds()
    return found


@pytest.mark.parametrize("role, prefix, request_, parents", DESTROYS, ids=["managed service", "artifact server"])
def test_a_destroy_removes_each_empty_parent_never_recursively(role, prefix, request_, parents):
    tasks = load_tasks(role, "destroy.yml")
    root = index_of(tasks, lambda task: task.get("name") == "Remove the owned content root", "removes the content root")
    removal = index_of(tasks, lambda task: argv_of(task)[:1] == ["/usr/bin/rmdir"], "removes the empty parents")
    assert removal == root + 1
    assert argv_of(tasks[removal]) == ["/usr/bin/rmdir", "{{ item }}"]
    templar = Templar(loader=LOADER, variables=variables(prefix, request_))
    assert templar.template(tasks[removal]["loop"]) == parents
    validate = validate_of(tasks)
    assert holds(validate, variables(prefix, request_))
    shallow = dict(request_, contentRoot="/var/lib/bootwright-services/lab/resolver")
    assert not holds(validate, variables(prefix, shallow))
    foreign = dict(request_, contentRoot="/var/lib/bootwright-services/other/dns/resolver")
    assert not holds(validate, variables(prefix, foreign))


@pytest.mark.parametrize("result, failed, changed", [
    ({"rc": 0, "stderr": ""}, False, True),
    ({"rc": 1, "stderr": "rmdir: failed to remove '/x': Directory not empty"}, False, False),
    ({"rc": 1, "stderr": "rmdir: failed to remove '/x': No such file or directory"}, False, False),
    ({"rc": 1, "stderr": "rmdir: failed to remove '/x': Permission denied"}, True, False),
])
@pytest.mark.parametrize("role", [destroy[0] for destroy in DESTROYS])
def test_only_a_non_empty_or_absent_parent_is_tolerated(role, result, failed, changed):
    tasks = load_tasks(role, "destroy.yml")
    removal = tasks[index_of(tasks, lambda task: argv_of(task)[:1] == ["/usr/bin/rmdir"], "removes the empty parents")]
    templar = Templar(loader=LOADER, variables={role + "_parents": result})
    failed_when = removal["failed_when"]
    assert all(templar.evaluate_conditional(trust_as_template(str(c))) for c in failed_when) is failed
    assert templar.evaluate_conditional(trust_as_template(str(removal["changed_when"]))) is changed


def test_a_destroy_reports_a_removed_parent_as_a_change():
    for role in (destroy[0] for destroy in DESTROYS):
        tasks = load_tasks(role, "destroy.yml")
        completion = tasks[index_of(tasks, lambda task: "removal evidence" in str(task.get("name", "")), "publishes the evidence")]
        module = next(value for key, value in completion.items() if key.endswith("_protocol"))
        assert ("%s_parents is changed" % role) in str(module["outcome"]), role


def test_the_apply_refuses_a_content_root_outside_its_context_and_kind():
    validate = validate_of(load_tasks("infra_managed_service", "apply.yml"))
    assert holds(validate, variables("bootwright_managed_service", MANAGED))
    for root in ("/var/lib/bootwright-services/lab/resolver", "/var/lib/bootwright-services/other/dns/resolver"):
        assert not holds(validate, variables("bootwright_managed_service", dict(MANAGED, contentRoot=root))), root


APPLIES = [
    ("infra_managed_service", "infra_service_protocol"),
    ("infra_artifact_server_nginx", "artifact_server_protocol"),
]
EFFECT_MODULES = ("ansible.builtin.file", "ansible.builtin.template", "ansible.builtin.copy")
EFFECT_COMMANDS = (["/usr/bin/podman", "pull"], ["/usr/bin/systemctl", "daemon-reload"],
                   ["/usr/bin/systemctl", "start"], ["/usr/bin/systemctl", "restart"])


def effects(task):
    """Whether a task pulls an image, writes a file, or reloads, starts or restarts a unit."""
    return argv_of(task)[:2] in EFFECT_COMMANDS or any(module in task for module in EFFECT_MODULES)


# A host reservation compares only Bootwright's contexts, so the apply proves a
# socket held by anything else before its first effect: the refusal is named
# to the runner first, then the run fails naming every socket, and nothing has
# been pulled, published or started.
@pytest.mark.parametrize("role, protocol", APPLIES)
def test_a_foreign_listener_refuses_before_any_effect(role, protocol):
    tasks = load_tasks(role, "apply.yml")
    module = "bootwright.core." + protocol
    observe = index_of(tasks, lambda task: (task.get("bootwright.core.%s" % protocol.replace("_protocol", "_inspect")) or {}).get("foreign") is True,
                       "observes the sockets")
    refused = index_of(tasks, lambda task: (task.get(module) or {}).get("phase") == "refused", "names the refusal")
    failed = index_of(tasks, lambda task: "ansible.builtin.fail" in task and "_sockets.foreign" in str(task.get("when")), "refuses")
    assert tasks[observe]["register"] == role + "_sockets"
    assert observe < refused < failed
    assert tasks[refused][module]["reason"] == "foreign-listener"
    assert str(tasks[refused]["when"]) == str(tasks[failed]["when"]) == role + "_sockets.foreign | length > 0"
    assert role + "_sockets.foreign[0].port" in str(tasks[refused][module]["port"])
    assert tasks[refused].get("no_log") is True and "no_log" not in tasks[failed]
    effecting = [index for index, task in enumerate(tasks) if effects(task)]
    assert len(effecting) >= 6, "the apply's effects were not all found: %s" % effecting
    first_effect = effecting[0]
    assert failed < first_effect, "the refusal follows %r" % tasks[first_effect].get("name")
    templar = Templar(loader=LOADER, variables={role + "_sockets": {"foreign": [
        {"transport": "udp", "address": "192.0.2.1", "port": 53}, {"transport": "tcp", "address": "fd00::1", "port": 53},
    ]}})
    message = templar.template(tasks[failed]["ansible.builtin.fail"]["msg"])
    assert message.endswith("nothing was pulled, published or started: udp 192.0.2.1:53; tcp [fd00::1]:53"), message


def refusal_action(protocol):
    module = importlib.import_module("ansible_collections.bootwright.core.plugins.action." + protocol)
    action = module.ActionModule.__new__(module.ActionModule)
    return module, action


# Go gave a diagnostic for each port the request binds under exactly this
# reason, so anything else would break the runner's protocol and is never
# published: the task fails before it emits.
@pytest.mark.parametrize("protocol", [protocol for _role, protocol in APPLIES])
@pytest.mark.parametrize("arguments, published", [
    ({"reason": "foreign-listener", "port": 53}, "foreign-listener-53"),
    ({"reason": "foreign-listener", "port": "8443"}, "foreign-listener-8443"),
    ({"reason": "other", "port": 53}, None),
    ({"port": 53}, None),
    ({"reason": "foreign-listener"}, None),
    ({"reason": "foreign-listener", "port": 0}, None),
    ({"reason": "foreign-listener", "port": 65536}, None),
    ({"reason": "foreign-listener", "port": True}, None),
    ({"reason": "foreign-listener", "port": "053"}, None),
    ({"reason": "foreign-listener", "port": 1.0}, None),
], ids=repr)
def test_a_refusal_no_diagnostic_names_is_never_published(protocol, arguments, published, monkeypatch):
    module, action = refusal_action(protocol)
    emitted = []
    monkeypatch.setattr(module, "emit", lambda message, **kwargs: emitted.append(message))
    action._task = SimpleNamespace(args=dict(arguments, phase="refused"))
    result = action.run(task_vars={})
    if published is None:
        assert result["failed"] is True and result["msg"].endswith("capability result could not be published")
        assert not emitted
        return
    assert result == {"changed": False}
    assert emitted == [{"phase": "refused", "reason": published}]
