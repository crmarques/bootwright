"""Name resolution before boot proves the addresses the plan froze.

The installer polls the cluster from the controller, so resolve.yml resolves
every frozen endpoint name, and one more name beneath the applications
wildcard, before any node is booted, and refuses unless each answers with its
frozen address and nothing else. These cases render its tasks over what the
command module registers for each name.

The getent output is recorded from glibc 2.39's getent (Ubuntu 24.04) on
2026-09-27, answering from an /etc/hosts bound over the host's own in a private
mount namespace: one line per answer and socket type, the address first and the
canonical name on the first line only (printf "%s%-*s %-6s %s\\n"), exit status
2 and no output for a name with no answer. For a name holding 198.51.100.21 and
2001:db8::21, `getent hosts` printed only the IPv6 address. `getent ahosts`
printed only the IPv4 one on that host, which has no IPv6 support, and both
with -A (--no-addrconfig), which is how a host configured for both families
prints them; the order is getaddrinfo's sorting, which the comparison does not
read. The command module strips the trailing newline (ansible/modules/command.py,
strip_empty_ends) and the task executor splits stdout into stdout_lines
(ansible/_internal/_task.py) in ansible-core 2.21.

Rendering the role's task files needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins/modules, so these checks live here.
"""

from __future__ import annotations

import pathlib
import re

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "containercluster_install_agent"
LOADER = DataLoader()

ADDRESS = "198.51.100.21"
# The endpoints lab-sno freezes (install-request-lab-sno.golden in
# internal/containercluster/agentinstall/testdata).
ENDPOINTS = [
    {"address": ADDRESS, "name": "api-int.sno.lab.example.test"},
    {"address": ADDRESS, "name": "api.sno.lab.example.test"},
    {"address": ADDRESS, "name": "console-openshift-console.apps.sno.lab.example.test"},
]
PROBE = "bootwright-sno.apps.sno.lab.example.test"
LABEL = re.compile(r"^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$")

CORRECT = ("198.51.100.21   STREAM api.sno.lab.example.test\n"
           "198.51.100.21   DGRAM  \n"
           "198.51.100.21   RAW    \n")
WRONG = ("192.0.2.9       STREAM api.sno.lab.example.test\n"
         "192.0.2.9       DGRAM  \n"
         "192.0.2.9       RAW    \n")
FOREIGN = ("198.51.100.21   STREAM api.sno.lab.example.test\n"
           "198.51.100.21   DGRAM  \n"
           "198.51.100.21   RAW    \n"
           "198.51.100.30   STREAM \n"
           "198.51.100.30   DGRAM  \n"
           "198.51.100.30   RAW    \n")
DUAL = ("198.51.100.21   STREAM api.sno.lab.example.test\n"
        "198.51.100.21   DGRAM  \n"
        "198.51.100.21   RAW    \n"
        "2001:db8::21    STREAM \n"
        "2001:db8::21    DGRAM  \n"
        "2001:db8::21    RAW    \n")
IPV6 = ("2001:db8::21    STREAM api.sno.lab.example.test\n"
        "2001:db8::21    DGRAM  \n"
        "2001:db8::21    RAW    \n")


def tasks():
    return [task for task in LOADER.load_from_file(str(ROLE / "tasks" / "resolve.yml"), trusted_as_template=True)
            if isinstance(task, dict)]


def one(predicate, description):
    found = [task for task in tasks() if predicate(task)]
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def facts(task):
    return task.get("ansible.builtin.set_fact") or {}


def probes(endpoints=None, cluster="sno"):
    """The names resolve.yml resolves, each with the one address it must answer with."""
    task = one(lambda task: "containercluster_install_agent_probes" in facts(task), "collect the probes")
    scope = dict(task["vars"])
    scope["bootwright_cluster_install_request"] = {
        "endpoints": ENDPOINTS if endpoints is None else endpoints, "identity": {"cluster": cluster}}
    return Templar(loader=LOADER, variables=scope).template(facts(task)["containercluster_install_agent_probes"])


def registered(probe, stdout, rc=0):
    """One loop result as the command module and the task executor register it."""
    stdout = stdout.rstrip("\r\n")
    return {"changed": False, "cmd": ["getent", "ahosts", probe["name"]], "failed": False, "item": probe,
            "rc": rc, "stderr": "", "stderr_lines": [], "stdout": stdout, "stdout_lines": stdout.splitlines()}


def answering(output, endpoints=None):
    """Every probe answered correctly except the ones output names."""
    results = []
    for probe in probes(endpoints):
        answer = output.get(probe["name"], (CORRECT.replace(ADDRESS, probe["address"]), 0))
        results.append(registered(probe, *answer))
    return results


def refusal(results):
    """The refusal's message, or None when every name answers with its frozen address alone."""
    collect = one(lambda task: "containercluster_install_agent_misresolved" in facts(task), "collect misresolved")
    scope = {"containercluster_install_agent_resolution": {"results": results}}
    misresolved = Templar(loader=LOADER, variables=scope).template(
        facts(collect)["containercluster_install_agent_misresolved"])
    scope["containercluster_install_agent_misresolved"] = misresolved
    task = one(lambda task: "ansible.builtin.fail" in task, "refuse")
    rendering = Templar(loader=LOADER, variables=scope)
    if not rendering.evaluate_conditional(task["when"]):
        return None
    return rendering.template(task["ansible.builtin.fail"]["msg"])


def refused(*diagnoses):
    return ("this host must resolve every name this cluster answers at to the address the plan froze for it and "
            "to nothing else, because the installer polls the cluster from here: %s. Route the cluster's zone to "
            "the resolver this graph declares and remove every other answer for these names, then repeat the "
            "apply. No node was booted." % "; ".join(diagnoses))


def test_every_frozen_name_and_one_beneath_the_wildcard_is_resolved_over_every_family():
    resolve = one(lambda task: "ansible.builtin.command" in task, "resolve")
    assert [str(value) for value in resolve["ansible.builtin.command"]["argv"]] == [
        "getent", "ahosts", "{{ item.name }}"]
    assert resolve["loop"] == "{{ containercluster_install_agent_probes }}"
    assert resolve["failed_when"] is False
    assert probes() == ENDPOINTS + [
        {"address": ADDRESS, "name": PROBE, "wildcard": "*.apps.sno.lab.example.test"}]


def test_the_wildcard_probe_answers_with_the_ingress_address():
    endpoints = [dict(endpoint, address="198.51.100.20") for endpoint in ENDPOINTS[:2]] + [ENDPOINTS[2]]
    assert probes(endpoints)[-1] == {"address": ADDRESS, "name": PROBE, "wildcard": "*.apps.sno.lab.example.test"}


def test_a_cluster_with_no_applications_address_resolves_only_its_frozen_names():
    assert probes(ENDPOINTS[:2]) == ENDPOINTS[:2]


# A cluster name is one DNS label of at most 63 characters (labelPattern in
# api/v1alpha1/lexical.go), so the probe's label is cut back to one.
CLUSTERS = {
    "short": ("sno", "bootwright-sno"),
    "longest": ("c" * 63, "bootwright-" + "c" * 52),
    "cut after a hyphen": ("c" * 51 + "-d" + "e" * 10, "bootwright-" + "c" * 51),
}


@pytest.mark.parametrize("cluster, label", CLUSTERS.values(), ids=CLUSTERS.keys())
def test_the_wildcard_label_is_one_label_derived_from_the_cluster_name(cluster, label):
    endpoints = [{"address": ADDRESS, "name": "console-openshift-console.apps.%s.lab.example.test" % cluster}]
    assert probes(endpoints, cluster)[-1]["name"] == "%s.apps.%s.lab.example.test" % (label, cluster)
    assert probes(endpoints, cluster) == probes(endpoints, cluster)
    assert LABEL.match(label)


def test_every_name_answering_with_its_frozen_address_alone_boots():
    assert refusal(answering({})) is None


def test_an_ipv6_address_answering_alone_boots():
    endpoints = [dict(endpoint, address="2001:db8::21") for endpoint in ENDPOINTS]
    assert refusal(answering({}, endpoints)) is None


CASES = {
    "wrong answer": ({"api.sno.lab.example.test": (WRONG, 0)},
                     "api.sno.lab.example.test must resolve to 198.51.100.21 alone and resolves to 192.0.2.9"),
    "missing name": ({"api-int.sno.lab.example.test": ("", 2)},
                     "api-int.sno.lab.example.test must resolve to 198.51.100.21 alone and resolves to no address"),
    "extra foreign answer": ({"api.sno.lab.example.test": (FOREIGN, 0)},
                             "api.sno.lab.example.test must resolve to 198.51.100.21 alone and resolves to "
                             "198.51.100.21, 198.51.100.30"),
    "dual-stack answer": ({"api.sno.lab.example.test": (DUAL, 0)},
                          "api.sno.lab.example.test must resolve to 198.51.100.21 alone and resolves to "
                          "198.51.100.21, 2001:db8::21"),
    "the IPv6 answer alone": ({"api.sno.lab.example.test": (IPV6, 0)},
                              "api.sno.lab.example.test must resolve to 198.51.100.21 alone and resolves to "
                              "2001:db8::21"),
    "no wildcard": ({PROBE: ("", 2)},
                    PROBE + ", which proves the *.apps.sno.lab.example.test wildcard, must resolve to "
                    "198.51.100.21 alone and resolves to no address"),
    "getent failed": ({"api.sno.lab.example.test": ("", 1)},
                      "api.sno.lab.example.test must resolve to 198.51.100.21 alone and resolves to no address "
                      "(getent ahosts exited 1)"),
}


@pytest.mark.parametrize("output, diagnosis", CASES.values(), ids=CASES.keys())
def test_a_name_not_answering_with_its_frozen_address_alone_refuses_naming_it(output, diagnosis):
    assert refusal(answering(output)) == refused(diagnosis)


def test_a_frozen_ipv6_address_with_an_ipv4_answer_beside_it_refuses():
    endpoints = [dict(endpoint, address="2001:db8::21") for endpoint in ENDPOINTS]
    assert refusal(answering({"api.sno.lab.example.test": (DUAL, 0)}, endpoints)) == refused(
        "api.sno.lab.example.test must resolve to 2001:db8::21 alone and resolves to 198.51.100.21, 2001:db8::21")


# Each slot frozen at its own address: the request freezes one address per name
# (Endpoint in internal/containercluster/agentinstall/requests.go), and an
# authored api-int slot keeps its own address rather than api's
# (specs/api/container-clusters.md, endpoint slots).
DISTINCT = [
    {"address": "198.51.100.22", "name": "api-int.sno.lab.example.test"},
    {"address": "198.51.100.20", "name": "api.sno.lab.example.test"},
    {"address": ADDRESS, "name": "console-openshift-console.apps.sno.lab.example.test"},
]


def answer(address):
    return (CORRECT.replace(ADDRESS, address), 0)


def test_every_name_answering_with_its_own_slot_address_boots():
    assert refusal(answering({}, DISTINCT)) is None


CROSSED = {
    "api answers the ingress address": (
        {"api.sno.lab.example.test": answer(ADDRESS)},
        "api.sno.lab.example.test must resolve to 198.51.100.20 alone and resolves to 198.51.100.21"),
    "api-int answers the api address": (
        {"api-int.sno.lab.example.test": answer("198.51.100.20")},
        "api-int.sno.lab.example.test must resolve to 198.51.100.22 alone and resolves to 198.51.100.20"),
    "the wildcard answers the api address": (
        {PROBE: answer("198.51.100.20")},
        PROBE + ", which proves the *.apps.sno.lab.example.test wildcard, must resolve to 198.51.100.21 alone "
        "and resolves to 198.51.100.20"),
}


@pytest.mark.parametrize("output, diagnosis", CROSSED.values(), ids=CROSSED.keys())
def test_a_name_answering_with_another_slot_address_refuses_naming_its_own(output, diagnosis):
    assert refusal(answering(output, DISTINCT)) == refused(diagnosis)


def test_every_failing_name_is_named_in_the_order_resolved():
    assert refusal(answering({"api-int.sno.lab.example.test": ("", 2), PROBE: (WRONG, 0)})) == refused(
        "api-int.sno.lab.example.test must resolve to 198.51.100.21 alone and resolves to no address",
        PROBE + ", which proves the *.apps.sno.lab.example.test wildcard, must resolve to 198.51.100.21 alone "
        "and resolves to 192.0.2.9")


def test_the_refusal_is_proved_before_any_node_is_booted():
    apply = [task for task in LOADER.load_from_file(str(ROLE / "tasks" / "apply.yml"), trusted_as_template=True)
             if isinstance(task, dict)]
    resolved = [index for index, task in enumerate(apply) if task.get("ansible.builtin.import_tasks") == "resolve.yml"]
    booted = [index for index, task in enumerate(apply) if task.get("ansible.builtin.include_tasks") == "boot.yml"]
    assert len(resolved) == 1 and len(booted) == 1
    assert resolved[0] < booted[0]
    assert not {"when", "failed_when", "ignore_errors"} & set(apply[resolved[0]])
    assert not {"failed_when", "ignore_errors"} & set(one(lambda task: "ansible.builtin.fail" in task, "refuse"))
