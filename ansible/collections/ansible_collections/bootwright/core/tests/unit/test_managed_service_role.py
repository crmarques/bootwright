"""One role realizes every managed network service, keyed by the request's frozen implementation.

The DNS server, NTP server and proxy were one role copied three times. The one
role's argument spec admits exactly the frozen kinds, their implementation
identities and the one request version they share, and each implementation
selects only its own daemon configuration and unit templates from a fixed
table, so every task is shared.

The proxy refuses loopback, unspecified and link-local destinations and its
cache manager before it admits any derived client, admits a client only to the
destinations a request freezes when it freezes any, logs each refusal, and it
stops within podman's stop timeout. A destroy of a managed service or the artifact server
removes the kind's, the services' and the context's directories once each is
empty, never recursively, and only when the content root sits exactly at
<hosted root>/<context>/services/<kind>/<name>. Evaluating those guards needs Ansible's
controller (DataLoader and Templar), so these checks live here rather than
under tests/unit/plugins.
"""

from __future__ import annotations

import importlib
import ipaddress
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
SERVICES = ("dns_server", "ntp_server", "proxy", "registry")
OPERATIONS = ("apply", "observe", "destroy")
ROLES = COLLECTION / "roles"
SQUID = COLLECTION / "tests" / "unit" / "goldens" / "templates" / "infra_managed_service" / "squid.conf"
LOADER = DataLoader()
PODMAN_STOP_TIMEOUT = 10


def implementations():
    return yaml.safe_load((ROLE / "vars" / "main.yml").read_text())["infra_managed_service_implementations"]


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


def test_the_argument_spec_keys_the_request_by_its_frozen_implementation():
    specs = yaml.safe_load((ROLE / "meta" / "argument_specs.yml").read_text())["argument_specs"]
    assert sorted(specs) == sorted(OPERATIONS)
    options = [specs[operation]["options"] for operation in OPERATIONS]
    assert options[0] == options[1] == options[2]
    request = options[0]["bootwright_managed_service_request"]["options"]
    assert request["kind"]["choices"] == ["DNSServer", "NTPServer", "Proxy", "Registry"]
    assert request["implementation"]["choices"] == sorted(implementations()) == [
        "dns-server-dnsmasq-v1", "ntp-server-chrony-v1", "proxy-squid-v1", "registry-distribution-v1"]
    assert request["version"]["choices"] == ["managed-service-v4"]


def test_each_implementation_selects_only_its_own_templates():
    selected = []
    for row in implementations().values():
        selected += [row["configuration_template"], row["unit_template"]]
    carried = sorted(path.name for path in (ROLE / "templates").iterdir())
    assert len(selected) == len(set(selected)), "two implementations share a template: %s" % selected
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


PROXY = {
    "bindAddress": "192.0.2.1",
    "clients": ["127.0.0.1/32", "192.0.2.0/24"],
    "contentRoot": "/var/lib/bootwright-hosted/lab/services/proxy/egress",
    "destinations": [
        {"address": "198.51.100.50"},
        {"address": "2001:db8::50", "port": 8443},
        {"domain": ".redhat.com"},
        {"domain": "mirror.lab.example.test", "port": 443},
        {"domain": "registry.lab.example.test", "port": 8443},
    ],
    "egress": {"noProxy": []},
    "endpoints": [{"address": "192.0.2.1:3128", "name": "egress"}],
    "identity": {"block": "proxy-egress", "context": "lab", "service": "egress"},
    "image": "registry.example.test/squid@sha256:" + "0" * 64,
    "implementation": "proxy-squid-v1",
    "kind": "Proxy",
    "placement": {"connection": "local", "machine": "controller"},
    "port": 3128,
    "unit": "bootwright-lab-proxy-egress",
    "version": "managed-service-v4",
}


def squid_configuration(request, template="squid.conf.j2"):
    """squid.conf, or the proxy's unit, as ansible.builtin.template writes it for one request."""
    found = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True) or {})
    found.update(LOADER.load_from_file(str(ROLE / "vars" / "main.yml"), trusted_as_template=True) or {})
    found["bootwright_managed_service_request"] = request
    source = ROLE / "templates" / template
    templar = Templar(loader=LOADER, variables=found).copy_with_new_env(searchpath=[str(source.parent)], available_variables=found)
    return templar.template(trust_as_template(source.read_text()), escape_backslashes=False,
                            overrides={"trim_blocks": True, "lstrip_blocks": False, "newline_sequence": "\n"})


def squid_decides(configuration, client, host, port, addresses=(), manager=False):
    """The http_access verdict squid reaches for one request, by squid.conf.documented's ACL semantics.

    Rules are read in order, the first whose every ACL matches decides, and an
    ACL declared on several lines holds every value. dst matches when any
    address the destination resolves to is in it, or the IP literal it names;
    dstdomain -n compares the host the request names, a literal address
    included, with each value exactly, or with a .domain and every name beneath
    it, and never looks anything up; port matches the destination port;
    manager matches a cache manager request.
    """
    acls, rules = {}, []
    for line in configuration.splitlines():
        words = line.split()
        if words[:1] == ["acl"]:
            kind, values = words[2], [value for value in words[3:] if value != "-n"]
            acls.setdefault(words[1], (kind, []))[1].extend(values)
        elif words[:1] == ["http_access"]:
            rules.append((words[1], words[2:]))
    try:
        resolved = [ipaddress.ip_address(host.strip("[]"))]
    except ValueError:
        resolved = [ipaddress.ip_address(address) for address in addresses]

    def matches(name):
        negated = name.startswith("!")
        name = name.lstrip("!")
        if name == "all":
            found = True
        elif name == "manager":
            found = manager
        else:
            kind, values = acls[name]
            if kind in ("src", "dst"):
                subjects = [ipaddress.ip_address(client)] if kind == "src" else resolved
                found = any(subject in ipaddress.ip_network(value) for subject in subjects for value in values)
            elif kind == "dstdomain":
                found = any(host == value.lstrip(".") or value.startswith(".") and host.endswith(value) for value in values)
            elif kind == "port":
                found = str(port) in values
            else:
                raise AssertionError("the evaluator does not model acl type %s" % kind)
        return found != negated

    for action, names in rules:
        if all(matches(name) for name in names):
            return action
    raise AssertionError("no http_access rule decided; the configuration must end in deny all")


DECISIONS = [
    ("a declared suffix's own domain", "192.0.2.10", "redhat.com", 443, ("203.0.113.1",), "allow"),
    ("a name beneath an exact declared name", "192.0.2.10", "cache.mirror.lab.example.test", 443, ("192.0.2.82",), "deny"),
    ("a name beneath a declared suffix over CONNECT", "192.0.2.10", "cdn.redhat.com", 443, ("203.0.113.3",), "allow"),
    ("a declared suffix over plain HTTP", "192.0.2.10", "cdn.redhat.com", 80, ("203.0.113.3",), "allow"),
    ("a name that only ends like a suffix", "192.0.2.10", "evilredhat.com", 443, ("203.0.113.4",), "deny"),
    ("an undeclared vendor-looking host", "192.0.2.10", "quay.io", 443, ("203.0.113.5",), "deny"),
    ("an undeclared public host", "192.0.2.10", "github.com", 443, ("203.0.113.6",), "deny"),
    ("a declared name at its declared port", "192.0.2.10", "registry.lab.example.test", 8443, ("192.0.2.80",), "allow"),
    ("a declared name at another port", "192.0.2.10", "registry.lab.example.test", 443, ("192.0.2.80",), "deny"),
    ("a port-pinned name at a web port it does not name", "192.0.2.10", "mirror.lab.example.test", 80, ("192.0.2.81",), "deny"),
    ("a port-pinned name at its port", "192.0.2.10", "mirror.lab.example.test", 443, ("192.0.2.81",), "allow"),
    ("a declared name off the web ports", "192.0.2.10", "cdn.redhat.com", 22, ("203.0.113.3",), "deny"),
    ("an IP literal whose reverse name is declared", "192.0.2.10", "203.0.113.3", 443, (), "deny"),
    ("a declared address", "192.0.2.10", "198.51.100.50", 443, (), "allow"),
    ("a name resolving to a declared address", "192.0.2.10", "other.example.test", 443, ("198.51.100.50",), "deny"),
    ("a name resolving to a declared and an undeclared address", "192.0.2.10", "x.attacker.example", 443,
     ("198.51.100.50", "203.0.113.66"), "deny"),
    ("a declared IPv6 address bracketed at its port", "192.0.2.10", "[2001:db8::50]", 8443, (), "allow"),
    ("another spelling of a declared IPv6 address", "192.0.2.10", "2001:db8:0::50", 8443, (), "deny"),
    ("a declared IPv6 address at its port", "192.0.2.10", "2001:db8::50", 8443, (), "allow"),
    ("a declared IPv6 address at a web port", "192.0.2.10", "2001:db8::50", 443, (), "deny"),
    ("a declared name resolving to loopback", "192.0.2.10", "cdn.redhat.com", 443, ("127.0.0.1",), "deny"),
    ("a declared name resolving to link-local metadata", "192.0.2.10", "cdn.redhat.com", 80, ("169.254.169.254",), "deny"),
    ("a declared name resolving to loopback beside a remote address", "192.0.2.10", "cdn.redhat.com", 443,
     ("203.0.113.3", "127.0.0.1"), "deny"),
    ("a client outside the derived set", "198.51.100.9", "cdn.redhat.com", 443, ("203.0.113.3",), "deny"),
]


@pytest.mark.parametrize("what, client, host, port, addresses, verdict", DECISIONS, ids=[row[0] for row in DECISIONS])
def test_a_proxy_that_freezes_destinations_admits_only_them(what, client, host, port, addresses, verdict):
    assert squid_decides(squid_configuration(PROXY), client, host, port, addresses) == verdict, what


def test_a_proxy_that_freezes_no_destination_admits_every_remote_one_and_logs_nothing():
    configuration = squid_configuration(dict(PROXY, destinations=[]))
    assert configuration == squid_configuration({key: value for key, value in PROXY.items() if key != "destinations"})
    assert squid_decides(configuration, "192.0.2.10", "quay.io", 443, ("203.0.113.5",)) == "allow"
    assert squid_decides(configuration, "192.0.2.10", "quay.io", 443, ("127.0.0.1",)) == "deny"
    assert squid_decides(configuration, "192.0.2.10", "quay.io", 443, ("203.0.113.5",), manager=True) == "deny"
    assert [line for line in configuration.splitlines() if line.startswith("access_log")] == ["access_log none"]


def test_a_proxy_that_freezes_destinations_refuses_the_rest_after_its_local_refusals_and_logs_each():
    lines = [line for line in squid_configuration(PROXY).splitlines() if line.strip()]
    access = [" ".join(line.split()[1:]) for line in lines if line.split()[0] == "http_access"]
    assert access[:3] == ["deny manager", "deny bootwright_local_destinations", "deny bootwright_link_local_destinations"]
    assert access[-1] == "deny all"
    assert all(rule.startswith("allow bootwright_clients bootwright_destination_") for rule in access[3:-1])
    assert len(access[3:-1]) == len(PROXY["destinations"])
    assert [line for line in lines if line.startswith("acl bootwright_destination_")] == [
        "acl bootwright_destination_1 dstdomain -n 198.51.100.50",
        "acl bootwright_destination_2 dstdomain -n 2001:db8::50 [2001:db8::50]",
        "acl bootwright_destination_3 dstdomain -n .redhat.com",
        "acl bootwright_destination_4 dstdomain -n mirror.lab.example.test",
        "acl bootwright_destination_5 dstdomain -n registry.lab.example.test",
    ]
    assert [line for line in lines if line.startswith("access_log")] == ["access_log stdio:/var/log/squid/access.log on-error=drop"]
    assert not [line for line in lines if line.startswith(("strip_query_terms", "logformat"))]


def test_a_proxy_that_freezes_destinations_logs_to_a_capped_tmpfs_and_drops_lines_when_full():
    unit = squid_configuration(PROXY, "squid.container.j2").splitlines()
    assert [line for line in unit if line.startswith("Tmpfs=")] == ["Tmpfs=/var/log/squid:rw,nosuid,nodev,noexec,size=64m,mode=1777"]
    bare = squid_configuration({key: value for key, value in PROXY.items() if key != "destinations"}, "squid.container.j2")
    assert "Tmpfs=" not in bare
    logs = [line.split() for line in squid_configuration(PROXY).splitlines() if line.startswith("access_log")]
    assert logs == [["access_log", "stdio:/var/log/squid/access.log", "on-error=drop"]]


def test_the_apply_refuses_a_destination_that_could_write_another_squid_directive():
    tasks = load_tasks("infra_managed_service", "apply.yml")
    validate = validate_of(tasks)
    scope = {"bootwright_managed_service_request": PROXY, "bootwright_managed_service_digest": "0" * 64,
             "infra_managed_service_implementations": implementations()}
    assert holds(validate, scope)
    for injected in ("mirror.lab.example.test\nhttp_access allow all", "mirror.lab.example.test all", "Mirror.example.test", "a#b",
                     "-n", "all.", "a..b"):
        request = dict(PROXY, destinations=[{"domain": injected}])
        assert not holds(validate, dict(scope, bootwright_managed_service_request=request)), injected
    for tampered in ({"address": "all"}, {"address": "192.0.2.1", "domain": "a.test"}, {}, {"domain": "a.test", "ports": 80},
                     {"domain": "a.test", "port": "80-443"}, {"domain": "a.test", "port": 0}, {"domain": "a.test", "port": 65536},
                     {"domain": "a.test", "port": True}, {"address": "-1.2.3"}):
        request = dict(PROXY, destinations=[tampered])
        assert not holds(validate, dict(scope, bootwright_managed_service_request=request)), tampered


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
    "version": "managed-service-v4",
    "kind": "DNSServer",
    "implementation": "dns-server-dnsmasq-v1",
    "unit": "bootwright-lab-dns-resolver",
    "identity": {"context": "lab"},
    "contentRoot": "/var/lib/bootwright-hosted/lab/services/dns/resolver",
    "image": "registry.example/dnsmasq@sha256:" + "0" * 64,
    "port": 53,
}
ARTIFACT = {
    "version": "artifact-server-nginx-v2",
    "identity": {"context": "lab"},
    "contentRoot": "/var/lib/bootwright-hosted/lab/services/artifact-server/media",
}
DESTROYS = [
    ("infra_managed_service", "bootwright_managed_service", MANAGED, ["services", "dns"]),
    ("infra_artifact_server_nginx", "bootwright_artifact_server", ARTIFACT, ["services", "artifact-server"]),
]
HOSTED = "bootwright.core.hosted_area"


def variables(prefix, request):
    found = {prefix + "_request": request, prefix + "_digest": "0" * 64}
    if prefix == "bootwright_managed_service":
        found["infra_managed_service_implementations"] = implementations()
    return found


def role_scope(role, prefix, request_):
    found = variables(prefix, request_)
    found.update(LOADER.load_from_file(str(ROLES / role / "defaults" / "main.yml"), trusted_as_template=True))
    return found


@pytest.mark.parametrize("role, prefix, request_, area", DESTROYS, ids=["managed service", "artifact server"])
def test_a_destroy_removes_each_empty_parent_never_recursively(role, prefix, request_, area):
    tasks = load_tasks(role, "destroy.yml")
    root = index_of(tasks, lambda task: task.get("name") == "Remove the owned content root", "removes the content root")
    removal = index_of(tasks, lambda task: task.get(HOSTED, {}).get("state") == "absent", "removes the empty parents")
    assert removal == root + 1
    templar = Templar(loader=LOADER, variables=role_scope(role, prefix, request_))
    assert templar.template(tasks[removal][HOSTED]["path"]) == area
    assert templar.template(tasks[removal][HOSTED]["context"]) == "lab"
    assert tasks[removal]["register"] == role + "_parents"
    validate = validate_of(tasks)
    assert holds(validate, variables(prefix, request_))
    shallow = dict(request_, contentRoot="/var/lib/bootwright-hosted/lab/services/resolver")
    assert not holds(validate, variables(prefix, shallow))
    foreign = dict(request_, contentRoot="/var/lib/bootwright-hosted/other/services/dns/resolver")
    assert not holds(validate, variables(prefix, foreign))


@pytest.mark.parametrize("role, prefix, request_, area", DESTROYS, ids=["managed service", "artifact server"])
@pytest.mark.parametrize("entry", ["apply.yml", "destroy.yml"])
def test_the_hosted_ancestors_are_proved_right_after_the_handoff(role, prefix, request_, area, entry):
    tasks = load_tasks(role, entry)
    handoff = index_of(tasks, lambda task: any(key.endswith("_protocol") and task[key].get("phase") == "loaded" for key in task),
                       "hands the runner its loaded record")
    proof = tasks[handoff + 1]
    assert proof[HOSTED]["state"] == "proved"
    templar = Templar(loader=LOADER, variables=role_scope(role, prefix, request_))
    assert templar.template(proof[HOSTED]["path"]) == area


@pytest.mark.parametrize("role, prefix, request_, area", DESTROYS, ids=["managed service", "artifact server"])
def test_the_apply_creates_its_hosted_ancestors_before_the_content_root(role, prefix, request_, area):
    tasks = load_tasks(role, "apply.yml")
    create = index_of(tasks, lambda task: task.get(HOSTED, {}).get("state") == "present", "creates the hosted ancestors")
    directories = index_of(tasks, lambda task: task.get("name") == "Create the owned service directories", "creates the content root")
    assert create == directories - 1
    templar = Templar(loader=LOADER, variables=role_scope(role, prefix, request_))
    assert templar.template(tasks[create][HOSTED]["path"]) == area


def test_a_destroy_reports_a_removed_parent_as_a_change():
    for role in (destroy[0] for destroy in DESTROYS):
        tasks = load_tasks(role, "destroy.yml")
        completion = tasks[index_of(tasks, lambda task: "removal evidence" in str(task.get("name", "")), "publishes the evidence")]
        module = next(value for key, value in completion.items() if key.endswith("_protocol"))
        assert ("%s_parents is changed" % role) in str(module["outcome"]), role


def test_the_apply_refuses_a_content_root_outside_its_context_and_kind():
    validate = validate_of(load_tasks("infra_managed_service", "apply.yml"))
    assert holds(validate, variables("bootwright_managed_service", MANAGED))
    for root in ("/var/lib/bootwright-hosted/lab/services/resolver", "/var/lib/bootwright-hosted/other/services/dns/resolver",
                 "/var/lib/bootwright-hosted/lab/dns/resolver/extra", "/var/lib/bootwright-services/lab/services/dns/resolver"):
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
    refused = index_of(tasks, lambda task: (task.get(module) or {}).get("reason") == "foreign-listener", "names the refusal")
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


@pytest.mark.parametrize("role, protocol", APPLIES)
def test_an_unassigned_bind_address_refuses_before_any_effect(role, protocol):
    tasks = load_tasks(role, "apply.yml")
    module = "bootwright.core." + protocol
    inspect = "bootwright.core." + protocol.replace("_protocol", "_inspect")
    observe = index_of(tasks, lambda task: (task.get(inspect) or {}).get("bind_address") is True, "proves the bind address")
    refused = index_of(tasks, lambda task: (task.get(module) or {}).get("reason") == "unassigned-bind-address", "names the address refusal")
    failed = index_of(tasks, lambda task: "ansible.builtin.fail" in task and "bindAddressAssigned" in str(task.get("when")), "refuses the address")
    assert observe < refused < failed < min(index for index, task in enumerate(tasks) if effects(task))
    assert str(tasks[refused]["when"]) == str(tasks[failed]["when"]) == "not %s_sockets.bindAddressAssigned" % role
    assert tasks[refused].get("no_log") is True
    for assigned in (True, False):
        templar = Templar(loader=LOADER, variables={role + "_sockets": {"bindAddressAssigned": assigned}})
        assert templar.evaluate_conditional(tasks[failed]["when"]) is not assigned


# Nothing installs podman on a Machine reached over SSH, so a placement host
# without a working podman is refused with the service's own diagnostic right
# after the hosted directories are proved, before any effect is made. A missing executable and a
# podman that fails to run both leave a result the refusal reads as not working.
@pytest.mark.parametrize("role, protocol", APPLIES)
@pytest.mark.parametrize("result, refused", [
    ({"rc": 0}, False),
    ({"rc": 1}, True),
    ({"rc": 2, "msg": "[Errno 2] No such file or directory: b'/usr/bin/podman'"}, True),
    ({"msg": "no result"}, True),
])
def test_a_host_without_podman_refuses_before_any_other_task(role, protocol, result, refused):
    tasks = load_tasks(role, "apply.yml")
    module = "bootwright.core." + protocol
    handoff = index_of(tasks, lambda task: (task.get(module) or {}).get("phase") == "loaded", "completes the handoff")
    prove = index_of(tasks, lambda task: argv_of(task) == ["/usr/bin/podman", "--version"], "proves the container runtime")
    named = index_of(tasks, lambda task: (task.get(module) or {}).get("reason") == "container-runtime", "names the runtime refusal")
    failed = index_of(tasks, lambda task: "ansible.builtin.fail" in task and "_runtime" in str(task.get("when")), "refuses the host")
    assert handoff + 2 == prove < named < failed == named + 1
    assert tasks[prove].get("failed_when") is False and tasks[prove].get("changed_when") is False
    assert tasks[prove]["register"] == role + "_runtime"
    assert str(tasks[named]["when"]) == str(tasks[failed]["when"])
    assert tasks[named].get("no_log") is True
    assert "podman" in str(tasks[failed]["ansible.builtin.fail"]["msg"]) and "nothing was pulled" in str(tasks[failed]["ansible.builtin.fail"]["msg"])
    assert prove < min(index for index, task in enumerate(tasks) if effects(task))
    templar = Templar(loader=LOADER, variables={role + "_runtime": result})
    assert templar.evaluate_conditional(tasks[failed]["when"]) is refused


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
    ({"reason": "unassigned-bind-address", "port": 53}, "unassigned-bind-address-53"),
    ({"reason": "container-runtime", "port": 53}, "container-runtime-53"),
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


@pytest.mark.parametrize("role,protocol", [
    ("infra_managed_service", "infra_service_protocol"),
    ("infra_artifact_server_nginx", "artifact_server_protocol"),
])
def test_readiness_refusal_precedes_stop_and_completion(role, protocol):
    tasks = yaml.safe_load((ROLES / role / "tasks" / "apply.yml").read_text())
    refusal = next(i for i, task in enumerate(tasks) if task.get("name") == "Report the classified readiness refusal")
    stop = next(i for i, task in enumerate(tasks) if task.get("name", "").startswith("Refuse an unproved"))
    completed = next(i for i, task in enumerate(tasks) if task.get("bootwright.core." + protocol, {}).get("phase") == "completed")
    assert refusal < stop < completed
    args = tasks[refusal]["bootwright.core." + protocol]
    assert args["phase"] == "refused" and ".failures[0].reason" in args["reason"] and ".failures[0].port" in args["port"]
    assert ".failures | length > 0" in tasks[refusal]["when"]
    assert "ansible.builtin.assert" in tasks[stop]


MATERIAL_READ = re.compile(r"bootwright_managed_service_material|\bmaterial\.client")


@pytest.mark.parametrize("tasks_file", ["apply.yml", "observe.yml"])
def test_every_task_that_reads_bound_material_hides_its_arguments(tasks_file):
    tasks = load_tasks("infra_managed_service", tasks_file)
    reading = [task for task in tasks if MATERIAL_READ.search(str({key: value for key, value in task.items() if key != "when"}))]
    assert reading, "no task of %s reads bound material" % tasks_file
    for task in reading:
        if "ansible.builtin.assert" in task:
            continue
        if task.get("no_log") is True:
            continue
        hides = Templar(loader=LOADER, variables={"bootwright_managed_service_request": {"credentialsRef": "mirror-push"}})
        shows = Templar(loader=LOADER, variables={"bootwright_managed_service_request": {}})
        hidden = str(task.get("no_log"))
        assert hides.template(trust_as_template(hidden)) is True, "%r reads bound material without no_log" % task.get("name")
        assert shows.template(trust_as_template(hidden)) is False, "%r hides a service that reads no credential" % task.get("name")


# A forced relabel of the host returns every blob a registry stored to the
# host's own label, so the apply relabels the whole store whenever the probe
# finds one entry under another label, and never walks it otherwise.
@pytest.mark.parametrize("found, relabeled", [("", False), ("/var/lib/bootwright-hosted/lab/services/registry/mirror/data/docker\n", True)])
def test_the_storage_is_relabeled_whole_only_when_an_entry_carries_another_label(found, relabeled):
    tasks = load_tasks("infra_managed_service", "apply.yml")
    probe = index_of(tasks, lambda task: argv_of(task)[:1] == ["/usr/bin/find"], "finds a mislabeled entry")
    label = index_of(tasks, lambda task: argv_of(task)[:2] == ["/usr/bin/chcon", "--recursive"], "relabels the store")
    assert probe < label and tasks[probe].get("changed_when") is False
    assert argv_of(tasks[probe])[-4:] == ["-context", "*:container_file_t:{{ infra_managed_service_label_level }}", "-print", "-quit"]
    templar = Templar(loader=LOADER, variables={"infra_managed_service_selinux": {"stat": {"exists": True}},
                                                "infra_managed_service_data_mislabeled": {"stdout": found}})
    assert all(templar.evaluate_conditional(condition) for condition in tasks[label]["when"]) is relabeled
    unlabeled = Templar(loader=LOADER, variables={"infra_managed_service_selinux": {"stat": {"exists": False}},
                                                  "infra_managed_service_data_mislabeled": {"stdout": found}})
    assert not all(unlabeled.evaluate_conditional(condition) for condition in tasks[label]["when"])


def role_defaults():
    return dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))


# The lent password file carries a fresh salt every apply, so it is published
# only when the running registry does not already answer the bound credential
# on every target while presenting the bound certificate.
@pytest.mark.parametrize("accepted, published", [
    ({"observation": {"unit": "active"}, "failures": [], "answers": [{"answer": "HTTP/1.1 200 OK"}]}, False),
    ({"observation": {"unit": "active"}, "failures": [{"reason": "listener-failed"}], "answers": []}, True),
    ({"observation": {"unit": "active"}, "failures": [], "answers": [{"answer": "certificate-mismatch"}]}, True),
    ({"observation": {"unit": "inactive"}, "failures": [], "answers": []}, True),
    ({"changed": False}, True),
], ids=["accepted", "refused", "another certificate", "not running", "never asked"])
def test_the_password_file_is_published_only_when_the_running_registry_refuses_the_credential(accepted, published):
    tasks = load_tasks("infra_managed_service", "apply.yml")
    ask = index_of(tasks, lambda task: task.get("register") == "infra_managed_service_accepted", "asks the running registry")
    publish = index_of(tasks, lambda task: str((task.get("bootwright.core.private_file") or {}).get("path", "")).endswith("/htpasswd"),
                       "publishes the password file")
    assert ask < publish and tasks[ask].get("no_log") is True
    variables = dict(role_defaults(), bootwright_managed_service_request={"credentialsRef": "mirror-push"}, infra_managed_service_accepted=accepted)
    templar = Templar(loader=LOADER, variables=variables)
    assert all(templar.evaluate_conditional(condition) for condition in tasks[publish]["when"]) is published
