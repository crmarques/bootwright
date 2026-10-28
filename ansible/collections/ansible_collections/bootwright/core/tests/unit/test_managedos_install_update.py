"""An installation updating within its release updates the installed machine
after its registration, reboots it through network convergence's own restart
when the transaction changed a package, proves the marker, FIPS mode and the
release again, and an observation reads that update the same way.

Both run the controller module bootwright.core.managedos_update over the
pinned fleet connection guest.yml makes, option for option, as the
registration worker's is. The worker answers with a closed vocabulary
(plugins/modules/managedos_update.py RETURN), which is all the apply's failure
names; the runner reports the named refusals with the diagnostics Go owns
(internal/managedos/installation, update.go).

Rendering the role's files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib

import pytest

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

from ansible_collections.bootwright.core.plugins.action import managedos_install_protocol as protocol


ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
LOADER = DataLoader()
COPY = "ansible.builtin.copy"
FAIL = "ansible.builtin.fail"
FACT = "ansible.builtin.set_fact"
INCLUDE = "ansible.builtin.include_tasks"
INSPECT = "bootwright.core.managedos_install_inspect"
PROTOCOL = "bootwright.core.managedos_install_protocol"
UPDATE = "bootwright.core.managedos_update"
UPDATING = "'update' in bootwright_os_install_request"
REACHABLE = "managedos_install_anaconda_reachable.rc | default(1) == 0"
NOT_REMOVAL = "not (managedos_install_anaconda_observes_removal | bool)"
ANSWER = "managedos_install_anaconda_update_answer"
DIGEST = "1" * 64
NAMED = ["update-release-mismatch", "update-release-drift", "update-no-repository", "update-foreign",
         "update-unavailable", "update-failed"]
SESSIONS = {
    "update_session.yml": ("managedos_install_anaconda_update_ssh_policy", "managedos_install_anaconda_update_host_key_algorithms"),
    "update_observe.yml": ("managedos_install_anaconda_update_observe_ssh_policy",
                           "managedos_install_anaconda_update_observe_host_key_algorithms"),
}
GUEST = ("managedos_install_anaconda_guest_ssh_policy", "managedos_install_anaconda_guest_host_key_algorithms")
KERNEL = "5.14.0-611.5.1.el9_7.x86_64"


def flattened(name):
    """A task file's tasks in the order a play reaches them, each block opened
    and its condition carried onto every task inside it."""
    def opened(listed, inherited):
        for task in listed or []:
            if not isinstance(task, dict):
                continue
            conditions = task.get("when", [])
            conditions = inherited + (conditions if isinstance(conditions, list) else [conditions])
            if "block" in task:
                for section in ("block", "rescue", "always"):
                    yield from opened(task.get(section), conditions)
                continue
            yield dict(task, when=conditions)
    return list(opened(LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True), []))


def only(tasks, predicate, description):
    found = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def included(name, **variables):
    return lambda task: task.get(INCLUDE) == name and all((task.get("vars") or {}).get(key) == value for key, value in variables.items())


def scope(updating=True, **extra):
    values = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    request = {"identity": {"context": "lab", "object": "rhel-01"}, "address": "192.0.2.10", "user": "fleet",
               "budgets": {"reboot": {"attempts": 60, "delaySeconds": 10}}}
    if updating:
        request["update"] = {"policy": "withinRelease", "release": "9.8"}
    values.update({
        "bootwright_run_scratch": "/var/tmp/bootwright-run-scratch-1-1",
        "bootwright_os_install_request": request,
        "bootwright_os_install_digest": DIGEST,
        "bootwright_os_install_material": {"fleetIdentity": "/run/material/fleet-id", "marker": "m"},
    })
    values.update(extra)
    return values


def render(value, updating=True, **variables):
    return Templar(loader=LOADER, variables=scope(updating, **variables)).template(value)


def holds(conditions, updating=True, **variables):
    templar = Templar(loader=LOADER, variables=scope(updating, **variables))
    listed = conditions if isinstance(conditions, list) else [conditions]
    return all(templar.evaluate_conditional(trust_as_template(condition)) for condition in listed)


def task_files():
    return sorted(path.name for path in (ROLE / "tasks").glob("*.yml"))


def module(name):
    tasks = flattened(name)
    return tasks[only(tasks, lambda task: UPDATE in task, "run the update worker")]


def test_the_update_runs_after_registration_and_before_the_completion_inspection():
    applied = flattened("apply.yml")
    security = only(applied, included("host_security.yml"), "read the FIPS state")
    encryption = only(applied, included("root_encryption.yml"), "read the root-disk encryption")
    registered = only(applied, included("register.yml"), "register the machine")
    unregistered = only(applied, lambda task: FAIL in task and "registration_result" in str(task.get("vars", "")),
                        "refuse an unregistered machine")
    updated = only(applied, included("update.yml"), "update the machine")
    inspected = only(applied, lambda task: INSPECT in task and task.get("register") == "managedos_install_anaconda_after",
                     "inspect the completion")
    assert encryption < security < registered < unregistered < updated < inspected
    assert applied[updated]["when"] == [UPDATING]
    assert holds(applied[updated]["when"]) and not holds(applied[updated]["when"], False)
    assert [name for name in task_files() for task in flattened(name) if task.get(INCLUDE) == "update.yml"] == ["apply.yml"]
    assert [name for name in task_files() for task in flattened(name) if task.get(INCLUDE) == "update_session.yml"] == [
        "update.yml", "update.yml"]
    assert [name for name in task_files() for task in flattened(name) if task.get(INCLUDE) == "update_observe.yml"] == [
        "observe.yml"]


def test_a_request_without_an_update_is_not_updating():
    # A frozen request is a mapping, whose update method both Jinja lookups
    # find when the key is absent, so presence is tested with the in operator.
    for name in task_files():
        text = (ROLE / "tasks" / name).read_text()
        for lookup in ("request.update ", "request.update.", "_answer.update ", "_read.update ", "['update'] is", '["update"] is'):
            assert lookup not in text, (name, lookup)
    assert holds(UPDATING) and not holds(UPDATING, False)
    assert holds("bootwright_os_install_request['update'] is defined", False)


def test_the_update_steps_follow_the_record():
    tasks = flattened("update.yml")
    marker = only(tasks, lambda task: "ansible.builtin.assert" in task and "not updated" in str(task), "prove the marker")
    applied = only(tasks, included("update_session.yml", managedos_install_anaconda_update_operation="apply"), "apply")
    boot = only(tasks, lambda task: included("guest.yml")(task) and "boot_id" in str(task["vars"]), "read the boot")
    rebooted = only(tasks, included("reboot.yml"), "reboot")
    identity = only(tasks, included("identity.yml"), "read the identity")
    proved = only(tasks, lambda task: "ansible.builtin.assert" in task and "after the reboot" in str(task), "prove the marker again")
    fips = only(tasks, included("host_security.yml"), "prove FIPS mode again")
    fips_named = only(tasks, lambda task: (task.get(PROTOCOL) or {}).get("reason") == "fips-not-enabled", "name FIPS")
    completed = only(tasks, included("update_session.yml", managedos_install_anaconda_update_operation="complete"), "complete")
    assert marker < applied < boot < rebooted < identity < proved < fips < fips_named < completed
    record = {"kernel": KERNEL, "packages": 4, "rebooted": False, "release": "9.8", "transaction": 7}
    for state, transaction, reboots, completes in (("upgraded", 7, True, True), ("upgraded", 0, False, True),
                                                   ("complete", 7, False, False), ("complete", 0, False, False)):
        update = dict(record, state=state, transaction=transaction)
        assert holds(tasks[rebooted]["when"], managedos_install_anaconda_update=update) is reboots, (state, transaction)
        assert holds(tasks[completed]["when"], managedos_install_anaconda_update=update) is completes, (state, transaction)
    assert holds(tasks[proved]["ansible.builtin.assert"]["that"], managedos_install_anaconda_marker="m",
                 managedos_install_anaconda_host_key="k", managedos_install_anaconda_update_host_key="k")
    assert not holds(tasks[proved]["ansible.builtin.assert"]["that"], managedos_install_anaconda_marker="m",
                     managedos_install_anaconda_host_key="other", managedos_install_anaconda_update_host_key="k")
    snapshot = only(tasks, lambda task: FACT in task and "managedos_install_anaconda_update_boot_before" in task[FACT], "snapshot")
    assert boot < snapshot < rebooted
    assert tasks[rebooted]["vars"]["managedos_install_anaconda_reboot_before"] == "{{ managedos_install_anaconda_update_boot_before }}"


@pytest.mark.parametrize("attempts, poweroff, back", [(60, 15, 45), (23, 5, 18), (4, 1, 3), (1, 1, 1)])
def test_the_update_reboot_holds_power_off_and_return_to_one_reboot_budget(attempts, poweroff, back):
    rebooted = flattened("update.yml")[only(flattened("update.yml"), included("reboot.yml"), "reboot")]
    budgets = {"reboot": {"attempts": attempts, "delaySeconds": 10}}
    request = dict(scope()["bootwright_os_install_request"], budgets=budgets)
    rendered = {key: int(render(value, bootwright_os_install_request=request)) for key, value in rebooted["vars"].items()
                if key.endswith("_attempts")}
    assert rendered == {"managedos_install_anaconda_reboot_poweroff_attempts": poweroff,
                        "managedos_install_anaconda_reboot_return_attempts": back}
    assert attempts == 1 or poweroff + back == attempts


def normalized(task, names):
    text = repr(task)
    for own, guest in zip(names, GUEST):
        text = text.replace(own, guest)
    return text


@pytest.mark.parametrize("name", sorted(SESSIONS))
def test_update_session_is_the_guest_connection(name):
    guest, own = flattened("guest.yml"), flattened(name)
    session = [task for task in own if UPDATE not in task]
    assert [normalized(task, SESSIONS[name]) for task in session[:4]] == [repr(task) for task in guest[:4]]
    assert [normalized(task, SESSIONS[name]) for task in session[4:6]] == [repr(task) for task in guest[-2:]]
    assert own.index(module(name)) == 4
    arguments = module(name)[UPDATE]
    policy, algorithms = SESSIONS[name]
    assert render(arguments["ssh_config"]) == render(guest[2][COPY]["dest"])
    assert render(arguments["known_hosts"]) == render(guest[1][COPY]["dest"])
    assert arguments["host_key_algorithms"] == "{{ %s }}" % algorithms
    assert arguments["policy_host_key_algorithms"] == "{{ %s.hostKeyAlgorithms }}" % policy
    assert render(arguments["address"]) == "192.0.2.10" and render(arguments["user"]) == "fleet"
    assert render(arguments["identity"]) == "/run/material/fleet-id" and render(arguments["request"]) == DIGEST
    assert render(arguments["release"]) == "9.8"
    assert module(name)["failed_when"] is False and module(name)["no_log"] is True


def test_observe_only_inspects_the_update():
    observe = module("update_observe.yml")
    assert observe[UPDATE]["operation"] == "inspect" and observe[UPDATE]["timeout"] == 120
    applied = module("update_session.yml")[UPDATE]
    for operation, timeout in (("apply", 1800), ("complete", 120)):
        assert render(applied["operation"], managedos_install_anaconda_update_operation=operation) == operation
        assert int(render(applied["timeout"], managedos_install_anaconda_update_operation=operation)) == timeout
    observed = flattened("observe.yml")
    read = only(observed, included("update_observe.yml"), "read the update")
    assert observed[read]["when"] == [NOT_REMOVAL, REACHABLE, UPDATING]
    fact = flattened("update_observe.yml")[-1][FACT]["managedos_install_anaconda_update"]
    read_update = {"kernel": KERNEL, "packages": 0, "rebooted": False, "release": "9.8", "state": "pending", "transaction": 0}
    assert render(fact, managedos_install_anaconda_update_read={"ok": True, "evidence": read_update, "updated": False}) == read_update
    assert render(fact, managedos_install_anaconda_update_read={"ok": False, "reason": "transport"}) == {}


def session_task(predicate, description):
    tasks = flattened("update_session.yml")
    return tasks[only(tasks, predicate, description)]


@pytest.mark.parametrize("result, message, detail", [
    ({"ok": False, "reason": "transport", "cause": "host-key", "detail": "SENTINEL"}, "transport (host-key)", None),
    ({"ok": False, "reason": "update-release-drift", "detail": "9.9@rhel-9-baseos"}, "update-release-drift", "9.9@rhel-9-baseos"),
    ({"ok": False, "reason": "update-release-mismatch", "detail": "9.6"}, "update-release-mismatch", "9.6"),
    ({"ok": False, "reason": "update-unavailable", "cause": "metadata"}, "update-unavailable (metadata)", "metadata"),
    ({"ok": False, "reason": "update-failed", "cause": "transaction"}, "update-failed (transaction)", "transaction"),
    ({"ok": False, "reason": "update-foreign"}, "update-foreign", ""),
    ({"ok": False, "reason": "SENTINEL", "cause": "SENTINEL"}, "unexpected answer", None),
    ({"failed": True, "msg": "SENTINEL"}, "unexpected answer", None),
], ids=["transport", "drift", "mismatch", "unavailable", "failed", "foreign", "unknown", "no-answer"])
def test_update_refusals_are_named_with_their_detail_and_the_failure_is_closed(result, message, detail):
    named = session_task(lambda task: (task.get(PROTOCOL) or {}).get("phase") == "refused", "name an update refusal")
    assert sorted(named["vars"]["managedos_install_anaconda_update_refusals"]) == sorted(NAMED) and named["no_log"] is True
    assert set(NAMED) | {"update-release-unavailable"} == set(protocol.UPDATE_REFUSALS)
    assert holds(named["when"], **dict(named["vars"], **{ANSWER: result})) is (detail is not None)
    if detail is not None:
        assert render(named[PROTOCOL]["detail"], **dict(named["vars"], **{ANSWER: result})) == detail
    refused = session_task(lambda task: FAIL in task, "refuse an unupdated machine")
    assert "no_log" not in refused
    assert holds(refused["when"], **{ANSWER: result}) and not holds(refused["when"], **{ANSWER: {"ok": True}})
    rendered = render(refused[FAIL]["msg"], **dict(refused["vars"], **{ANSWER: result}))
    assert rendered == "Machine rhel-01 was not updated within its release: " + message
    assert "SENTINEL" not in rendered


def completion(name):
    tasks = flattened(name)
    return tasks[only(tasks, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "publish completion")][PROTOCOL]


@pytest.mark.parametrize("name", ["apply.yml", "observe.yml"])
def test_completion_evidence_carries_the_update(name):
    arguments = completion(name)
    assert render(arguments["updateRequired"]) is True and render(arguments["updateRequired"], False) is False
    assert Templar(loader=LOADER, variables={}).template(arguments["update"]) == {}
    complete = {"kernel": KERNEL, "packages": 4, "rebooted": True, "release": "9.8", "state": "complete", "transaction": 7}
    assert render(arguments["update"], managedos_install_anaconda_update=complete) == complete


def test_a_replay_is_unchanged_only_when_the_update_is():
    outcome = completion("apply.yml")["outcome"]
    replay = {"managedos_install_anaconda_was_installed": True, "managedos_install_anaconda_network_result": {"changed": False}}
    assert render(outcome, **replay) == "unchanged"
    assert render(outcome, **dict(replay, managedos_install_anaconda_update_changed=False)) == "unchanged"
    assert render(outcome, **dict(replay, managedos_install_anaconda_update_changed=True)) == "changed"


def test_the_registration_sets_the_release_before_the_repositories():
    tasks = flattened("register.yml")
    arguments = tasks[only(tasks, lambda task: "bootwright.core.managedos_registration" in task, "register")]["bootwright.core.managedos_registration"]
    registering = {"registration": {"repositories": {"enable": [], "disable": []}}}
    request = dict(scope()["bootwright_os_install_request"], **registering)
    assert render(arguments["release"], bootwright_os_install_request=request) == "9.8"
    assert int(render(arguments["timeout"], bootwright_os_install_request=request)) == 750
    plain = dict(scope(False)["bootwright_os_install_request"], **registering)
    assert render(arguments["release"], bootwright_os_install_request=plain) == ""
    assert int(render(arguments["timeout"], bootwright_os_install_request=plain)) == 630
