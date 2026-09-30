"""A provider host apply decides from what its drivers answer once they run.

The apply enables the libvirt driver daemons itself. An observation taken
before that, on a host where the network and storage drivers were stopped,
reads every network and the pool as undefined: the foreign-network guard
passes, a definition drops the identity libvirt holds, and a pool the host
carries is defined again. It also misses the bridge of every network set to
autostart, which appears only once its driver runs, so an external attachment
to that bridge refuses. Which observation each decision reads is invisible to
lint and to a syntax check, so the order is checked here.
"""

from __future__ import annotations

import pathlib
import re

import yaml

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "substrate_libvirt_host"

OBSERVATION = re.compile(r"\b(substrate_libvirt_host_\w+)\.observation\b")


def apply_tasks():
    parsed = yaml.safe_load((ROLE / "tasks" / "apply.yml").read_text()) or []
    return [task for task in parsed if isinstance(task, dict)]


def argv_of(task):
    command = task.get("ansible.builtin.command")
    if not isinstance(command, dict):
        return []
    return [str(value) for value in command.get("argv") or []]


def conditions_of(task):
    return [str(condition).strip() for condition in (task.get("ansible.builtin.assert") or {}).get("that") or []]


def index_of(tasks, predicate, what):
    found = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(found) == 1, "expected one task that %s, found %d" % (what, len(found))
    return found[0]


def observations(text):
    return set(OBSERVATION.findall(str(text)))


def observed_at(tasks, read, decision):
    """The index of the one inspection the decision reads."""
    assert len(read) == 1, "%s reads %s" % (decision, sorted(read))
    name = next(iter(read))
    return index_of(
        tasks,
        lambda task: task.get("register") == name and "bootwright.core.libvirt_host_inspect" in task,
        "registers the observation " + name,
    )


def drivers_run(tasks):
    """The index after which every driver daemon runs and the uri answers."""
    enable = index_of(tasks, lambda task: argv_of(task)[:3] == ["/usr/bin/systemctl", "enable", "--now"], "enables the drivers")
    connection = index_of(tasks, lambda task: argv_of(task)[-1:] == ["version"], "proves the connection answers")
    assert enable < connection
    return connection


def test_every_network_and_pool_decision_reads_an_observation_taken_once_the_drivers_run():
    tasks = apply_tasks()
    running = drivers_run(tasks)
    guard = index_of(tasks, lambda task: "item.state | length == 0 or item.owned" in conditions_of(task), "refuses a foreign network")
    bridge = index_of(tasks, lambda task: conditions_of(task) == ["item.bridge"], "proves an external bridge")
    publish = index_of(tasks, lambda task: (task.get("ansible.builtin.template") or {}).get("src") == "network.xml.j2", "renders a network")
    pool = index_of(tasks, lambda task: "pool-define-as" in argv_of(task), "defines the pool")
    define = index_of(tasks, lambda task: "net-define" in argv_of(task), "defines a network")
    defaults = yaml.safe_load((ROLE / "defaults" / "main.yml").read_text())

    decisions = {
        "the foreign-network guard": (guard, observations(tasks[guard].get("loop"))),
        "the external bridge proof": (bridge, observations(tasks[bridge].get("loop"))),
        "each managed network's identity": (publish, observations(defaults["substrate_libvirt_host_network_uuids"])),
        "which managed networks to define": (define, observations(defaults["substrate_libvirt_host_carried"])),
        "whether to define the pool": (pool, observations(tasks[pool].get("when"))),
    }
    for decision, (index, read) in decisions.items():
        observed = observed_at(tasks, read, decision)
        assert running < observed < index, "%s reads an observation taken before the drivers ran" % decision


# A driver that still does not answer once it runs proves neither presence nor
# absence, so each refusal reads the same observation and comes before the
# first task that defines, starts or autostarts anything through the uri.
def test_the_apply_refuses_what_a_running_driver_did_not_answer_for_before_defining_anything():
    tasks = apply_tasks()
    running = drivers_run(tasks)
    network = index_of(tasks, lambda task: conditions_of(task) == ["item.answered"], "refuses an unanswered network")
    pool = index_of(
        tasks,
        lambda task: any(condition.endswith(".observation.poolAnswered") for condition in conditions_of(task)),
        "refuses an unanswered pool",
    )
    effect = next(
        index for index, task in enumerate(tasks)
        if argv_of(task)[:1] == ["/usr/bin/virsh"] and argv_of(task)[-1:] != ["version"]
    )
    assert tasks[network].get("when") == "item.managed"
    network_observed = observed_at(tasks, observations(tasks[network].get("loop")), "the network refusal")
    pool_observed = observed_at(tasks, observations(conditions_of(tasks[pool])), "the pool refusal")
    assert running < network_observed == pool_observed < min(network, pool)
    assert max(network, pool) < effect
