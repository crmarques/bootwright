"""A registering installation registers the installed machine with its
subscription service after its network converged and its FIPS and root-encryption
proofs passed, and an observation reads that registration the same way.

Both run the landed controller module bootwright.core.managedos_registration
over the pinned fleet connection guest.yml makes, option for option. The apply
lends the organization and activation key as files whose paths only that module
reads; the observation lends none and only inspects. The worker answers with a
closed vocabulary (plugins/modules/managedos_registration.py RETURN), which is
all the apply's failure names; the runner reports the six named refusals with
the diagnostics Go owns (internal/managedos/installation, registration.go).

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
FILE = "ansible.builtin.file"
FAIL = "ansible.builtin.fail"
FACT = "ansible.builtin.set_fact"
INCLUDE = "ansible.builtin.include_tasks"
INSPECT = "bootwright.core.managedos_install_inspect"
PROTOCOL = "bootwright.core.managedos_install_protocol"
REGISTRATION = "bootwright.core.managedos_registration"
REGISTERING = "bootwright_os_install_request.registration is defined"
ONCE = "not (managedos_install_anaconda_prepared | default(false) | bool)"
REACHABLE = "managedos_install_anaconda_reachable.rc | default(1) == 0"
NOT_REMOVAL = "not (managedos_install_anaconda_observes_removal | bool)"
RESULT = "managedos_install_anaconda_registration_result"
READ = "managedos_install_anaconda_registration_read"
DIGEST = "1" * 64
CONSUMER = "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0"
NAMED = ["registration-input", "registration-foreign", "registration-refused", "registration-identity-collision",
         "registration-unavailable", "registration-repositories"]
REGISTER_ONLY = ("organization_file", "activation_key_file", "enable", "disable")
MATERIAL = ("rhsmOrganization", "rhsmActivationKey")
# The names each file gives the connection guest.yml makes, so the tasks can be
# compared with guest.yml's option for option.
SESSIONS = {
    "register.yml": ("managedos_install_anaconda_registration_ssh_policy",
                     "managedos_install_anaconda_registration_host_key_algorithms"),
    "registration_observe.yml": ("managedos_install_anaconda_registration_observe_ssh_policy",
                                 "managedos_install_anaconda_registration_observe_host_key_algorithms"),
    "unregister.yml": ("managedos_install_anaconda_unregistration_ssh_policy",
                       "managedos_install_anaconda_unregistration_host_key_algorithms"),
}
GUEST = ("managedos_install_anaconda_guest_ssh_policy", "managedos_install_anaconda_guest_host_key_algorithms")


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


def included(name):
    return lambda task: task.get(INCLUDE) == name


def module(name):
    tasks = flattened(name)
    return tasks[only(tasks, lambda task: REGISTRATION in task, "run the registration worker")]


def scope(registering=True, **extra):
    """The variables an apply or an observation sees for a registering request
    or for one that registers nothing."""
    values = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    request = {"identity": {"context": "lab", "object": "rhel-01"}, "address": "192.0.2.10", "user": "fleet"}
    if registering:
        request["registration"] = {
            "entitlementRef": "rhel", "organizationRef": "rhsm-org", "activationKeyRef": "rhsm-key",
            "repositories": {"disable": ["*"], "enable": ["rhel-9-for-x86_64-baseos-rpms"]},
        }
    values.update({
        "bootwright_run_scratch": "/var/tmp/bootwright-run-scratch-1-1",
        "bootwright_os_install_request": request,
        "bootwright_os_install_digest": DIGEST,
        "bootwright_os_install_material": {"fleetIdentity": "/run/material/fleet-id",
                                           "rhsmOrganization": "/run/material/rhsm-organization",
                                           "rhsmActivationKey": "/run/material/rhsm-activation-key",
                                           "unregister": "guest"},
    })
    values.update(extra)
    return values


def render(value, registering=True, **variables):
    return Templar(loader=LOADER, variables=scope(registering, **variables)).template(value)


def holds(conditions, registering=True, **variables):
    templar = Templar(loader=LOADER, variables=scope(registering, **variables))
    listed = conditions if isinstance(conditions, list) else [conditions]
    return all(templar.evaluate_conditional(trust_as_template(condition)) for condition in listed)


def task_files():
    return sorted(path.name for path in (ROLE / "tasks").glob("*.yml"))


def test_register_runs_after_network_and_before_the_completion_inspection():
    applied = flattened("apply.yml")
    withdrawn = only(applied, lambda task: (task.get(FILE) or {}).get("path") == "{{ managedos_install_anaconda_private_root }}"
                     and task.get("name", "").startswith("Withdraw"), "withdraw the private material")
    network = only(applied, included("network.yml"), "converge the network")
    converged = only(applied, lambda task: (task.get(PROTOCOL) or {}).get("group") == "converge-network"
                     and task[PROTOCOL].get("status") == "ok", "report network convergence")
    encryption = only(applied, included("root_encryption.yml"), "read the root-disk encryption")
    security = only(applied, included("host_security.yml"), "read the FIPS state")
    registered = only(applied, included("register.yml"), "register the machine")
    named = only(applied, lambda task: RESULT in str((task.get(PROTOCOL) or {}).get("reason", "")), "name a registration refusal")
    refused = only(applied, lambda task: FAIL in task and RESULT in str(task.get("vars", "")), "refuse an unregistered machine")
    inspected = only(applied, lambda task: INSPECT in task and task.get("register") == "managedos_install_anaconda_after",
                     "inspect the completion")
    completed = only(applied, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "publish the completion")
    assert withdrawn < network < converged < encryption < security < registered < named < refused < inspected < completed
    assert registered + 1 == named and named + 1 == refused and refused + 1 == inspected
    assert applied[registered]["when"] == [REGISTERING]
    assert holds(applied[registered]["when"]) and not holds(applied[registered]["when"], False)

    observed = flattened("observe.yml")
    encryption = only(observed, included("root_encryption.yml"), "read the root-disk encryption")
    read = only(observed, lambda task: included("registration_observe.yml")(task) and NOT_REMOVAL in task["when"],
                "read the registration")
    assert read == encryption + 1
    assert observed[read]["when"] == [NOT_REMOVAL, REACHABLE, REGISTERING]
    assert [name for name in task_files() for task in flattened(name) if task.get(INCLUDE) == "register.yml"] == ["apply.yml"]
    assert [name for name in task_files() for task in flattened(name)
            if task.get(INCLUDE) == "registration_observe.yml"] == ["observe.yml", "observe.yml"]
    assert [name for name in task_files() for task in flattened(name) if task.get(INCLUDE) == "unregister.yml"] == ["destroy.yml"]


def test_apply_publishes_the_marker_proof_before_registering():
    applied = flattened("apply.yml")
    fips = only(applied, lambda task: FAIL in task and "FIPS mode" in str(task[FAIL].get("msg", "")), "refuse an unproved FIPS mode")
    proved = only(applied, lambda task: "ansible.builtin.assert" in task
                  and "managedos_install_anaconda_marker == bootwright_os_install_material.marker" in task["ansible.builtin.assert"]["that"]
                  and task["when"] == [REGISTERING], "prove the marker before registering")
    prepared = only(applied, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "prepared", "publish the marker proof")
    registered = only(applied, included("register.yml"), "register the machine")
    assert fips < proved and proved + 1 == prepared and prepared + 1 == registered
    assert applied[prepared][PROTOCOL] == {"phase": "prepared", "marker": "{{ managedos_install_anaconda_marker }}"}
    assert applied[prepared]["when"] == [REGISTERING, ONCE] and applied[prepared]["no_log"] is True
    assert applied[proved]["no_log"] is True
    assert holds(applied[proved]["ansible.builtin.assert"]["that"],
                 managedos_install_anaconda_marker="m", bootwright_os_install_material={"marker": "m"})
    assert not holds(applied[proved]["ansible.builtin.assert"]["that"],
                     managedos_install_anaconda_marker="other", bootwright_os_install_material={"marker": "m"})
    # The network restart records the same proof before its power-off (Q66),
    # and an attempt publishes it once, whichever comes first.
    assert [name for name in task_files() for task in flattened(name)
            if (task.get(PROTOCOL) or {}).get("phase") == "prepared"] == ["apply.yml", "network.yml"]


def test_register_material_never_enters_a_task_variable():
    carriers = []
    for name in task_files():
        for task in flattened(name):
            if any(word in repr(task) for word in MATERIAL):
                carriers.append((name, task))
    assert [name for name, _task in carriers] == ["register.yml"]
    task = carriers[0][1]
    arguments = task.get(REGISTRATION)
    assert arguments is not None and task["no_log"] is True
    assert arguments["organization_file"] == "{{ bootwright_os_install_material.rhsmOrganization }}"
    assert arguments["activation_key_file"] == "{{ bootwright_os_install_material.rhsmActivationKey }}"
    others = {key: value for key, value in arguments.items() if key not in ("organization_file", "activation_key_file")}
    assert not any(word in repr(others) for word in MATERIAL)
    assert not any(word in repr({key: value for key, value in task.items() if key != REGISTRATION}) for word in MATERIAL)
    assert "lookup" not in repr(task) and "vars" not in task
    for path in (ROLE / "defaults").glob("*.yml"):
        assert not any(word in path.read_text() for word in MATERIAL), path.name
    assert not any(word in repr(flattened("registration_observe.yml")) for word in MATERIAL)


def normalized(task, names):
    text = repr(task)
    for own, guest in zip(names, GUEST):
        text = text.replace(own, guest)
    return text


@pytest.mark.parametrize("name", sorted(SESSIONS))
def test_register_session_is_the_guest_connection(name):
    guest, own = flattened("guest.yml"), flattened(name)
    session = [task for task in own if REGISTRATION not in task]
    assert [normalized(task, SESSIONS[name]) for task in session[:4]] == [repr(task) for task in guest[:4]]
    assert [normalized(task, SESSIONS[name]) for task in session[4:6]] == [repr(task) for task in guest[-2:]]
    assert own.index(module(name)) == 4
    arguments = module(name)[REGISTRATION]
    policy, algorithms = SESSIONS[name]
    assert render(arguments["ssh_config"]) == render(guest[2][COPY]["dest"])
    assert render(arguments["known_hosts"]) == render(guest[1][COPY]["dest"])
    assert arguments["host_key_algorithms"] == "{{ %s }}" % algorithms
    assert arguments["policy_host_key_algorithms"] == "{{ %s.hostKeyAlgorithms }}" % policy
    assert render(arguments["address"]) == "192.0.2.10" and render(arguments["user"]) == "fleet"
    assert render(arguments["identity"]) == "/run/material/fleet-id" and render(arguments["request"]) == DIGEST
    assert module(name)["failed_when"] is False and module(name)["no_log"] is True


def apply_task(predicate, description):
    applied = flattened("apply.yml")
    return applied[only(applied, predicate, description)]


@pytest.mark.parametrize("result, message", [
    ({"ok": False, "reason": "transport", "cause": "host-key", "detail": "SENTINEL"}, "transport (host-key)"),
    ({"ok": False, "reason": "registration-refused", "detail": "SENTINEL"}, "registration-refused"),
    ({"ok": False, "reason": "registration-unavailable", "cause": "service"}, "registration-unavailable (service)"),
    ({"ok": False, "reason": "registration-unreachable"}, "registration-unreachable"),
    ({"ok": False, "reason": "SENTINEL", "cause": "SENTINEL"}, "unexpected answer"),
    ({"ok": False, "reason": "registration-input", "cause": "SENTINEL", "detail": "SENTINEL"}, "registration-input"),
    ({"failed": True, "msg": "SENTINEL"}, "unexpected answer"),
], ids=["transport", "refused", "unavailable", "unreachable", "unknown", "unknown-cause", "no-answer"])
def test_registration_refusals_are_named_and_the_failure_is_closed(result, message):
    named = apply_task(lambda task: RESULT in str((task.get(PROTOCOL) or {}).get("reason", "")), "name a registration refusal")
    assert named[PROTOCOL] == {"phase": "refused", "reason": "{{ %s.reason }}" % RESULT} and named["no_log"] is True
    assert sorted(named["vars"]["managedos_install_anaconda_registration_refusals"]) == sorted(NAMED)
    for reason in NAMED:
        assert holds(named["when"], **dict(named["vars"], **{RESULT: {"ok": False, "reason": reason}}))
    for other in ("registration-unreachable", "transport", "SENTINEL"):
        assert not holds(named["when"], **dict(named["vars"], **{RESULT: {"ok": False, "reason": other}}))
    assert not holds(named["when"], **dict(named["vars"], **{RESULT: {"ok": True, "reason": "registration-input"}}))
    assert not holds(named["when"], False, **dict(named["vars"], **{RESULT: {"ok": False, "reason": "registration-input"}}))

    refused = apply_task(lambda task: FAIL in task and RESULT in str(task.get("vars", "")), "refuse an unregistered machine")
    assert "no_log" not in refused
    assert refused["when"] == [REGISTERING, "not (%s.ok | default(false) | bool)" % RESULT]
    assert holds(refused["when"], **{RESULT: result})
    assert not holds(refused["when"], False)
    assert not holds(refused["when"], **{RESULT: {"ok": True}})
    rendered = render(refused[FAIL]["msg"], **dict(refused["vars"], **{RESULT: result}))
    assert rendered == "Machine rhel-01 was not registered with its subscription service: " + message
    assert "SENTINEL" not in rendered


def registration_fact(name, **variables):
    fact = flattened(name)[-1][FACT]["managedos_install_anaconda_registration"]
    return Templar(loader=LOADER, variables=variables).template(fact)


@pytest.mark.parametrize("state", ["registered", "adopted", "unchanged"])
def test_registration_state_follows_the_worker_answer(state):
    answer = {"ok": True, "registration": {"changed": state != "unchanged", "consumer": CONSUMER, "owned": True,
                                           "state": state, "repositories": {}}}
    assert registration_fact("register.yml", **{RESULT: answer}) == {"consumer": CONSUMER, "state": "owned"}
    unowned = {"ok": True, "registration": dict(answer["registration"], owned=False)}
    assert registration_fact("register.yml", **{RESULT: unowned})["state"] == ""
    for refused in ({"ok": False, "reason": "registration-foreign"}, {"failed": True}):
        assert registration_fact("register.yml", **{RESULT: refused}) == {"consumer": "", "state": ""}
    for read, consumer in (("owned", CONSUMER), ("pending", CONSUMER), ("absent", ""), ("foreign", "")):
        answer = {"ok": True, "registration": {"changed": False, "consumer": consumer, "owned": read == "owned", "state": read}}
        assert registration_fact("registration_observe.yml", **{READ: answer}) == {"consumer": consumer, "state": read}
    for refused in ({"ok": False, "reason": "transport", "cause": "session"}, {"failed": True}):
        assert registration_fact("registration_observe.yml", **{READ: refused}) == {"consumer": "", "state": ""}


def completion(name):
    tasks = flattened(name)
    return tasks[only(tasks, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "publish completion")][PROTOCOL]


@pytest.mark.parametrize("name", ["apply.yml", "observe.yml", "destroy.yml"])
def test_completion_evidence_carries_the_registration(name):
    arguments = completion(name)
    assert render(arguments["registrationRequired"]) is True
    assert render(arguments["registrationRequired"], False) is False
    assert Templar(loader=LOADER, variables={}).template(arguments["registration"]) == {}
    owned = {"consumer": CONSUMER, "state": "owned"}
    assert render(arguments["registration"], managedos_install_anaconda_registration=owned) == owned


def test_a_replay_is_unchanged_only_when_the_registration_is():
    outcome = completion("apply.yml")["outcome"]
    replay = {"managedos_install_anaconda_was_installed": True, "managedos_install_anaconda_network_result": {"changed": False}}
    assert render(outcome, False, **replay) == "unchanged"
    for changed, expected in ((False, "unchanged"), (True, "changed")):
        # The module task's changed_when is false, so the worker's own answer
        # is what names a change.
        answer = {"changed": False, "ok": True, "registration": {"changed": changed, "consumer": CONSUMER, "owned": True}}
        assert render(outcome, **dict(replay, **{RESULT: answer})) == expected


def test_registration_observe_only_inspects():
    observe = module("registration_observe.yml")
    arguments = observe[REGISTRATION]
    assert arguments["operation"] == "inspect"
    assert not any(option in arguments for option in REGISTER_ONLY)
    assert observe["register"] == READ and observe["no_log"] is True
    assert module("register.yml")[REGISTRATION]["operation"] == "register"
    applied = module("register.yml")[REGISTRATION]
    assert render(applied["enable"]) == ["rhel-9-for-x86_64-baseos-rpms"] and render(applied["disable"]) == ["*"]


UNREGISTRATION = "managedos_install_anaconda_unregistration_result"
UNREGISTERS = "managedos_install_anaconda_unregisters | bool"
REMOVAL = "managedos_install_anaconda_observes_removal | bool"
MARKER = '{"request":"%s"}' % DIGEST


def block(name, title):
    """One block of a task file as the file declares it, its own vars and conditions."""
    for task in LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True):
        if task.get("name") == title:
            return task
    raise AssertionError("no block %r in %s" % (title, name))


def guest(answered=True, marker=MARKER, host_key="ssh-ed25519 AAAAGUEST", **extra):
    """The variables a destroy's tasks see once the identity channel answered."""
    values = dict(block("destroy.yml", "Unregister the machine this installation registered")["vars"])
    values.update({
        "managedos_install_anaconda_answered": answered,
        "managedos_install_anaconda_marker": marker,
        "managedos_install_anaconda_host_key": host_key,
        "bootwright_os_install_material": {"fleetIdentity": "/run/material/fleet-id", "marker": MARKER, "unregister": "guest"},
    })
    values.update(extra)
    return values


def destroy_tasks(predicate, description):
    tasks = flattened("destroy.yml")
    return tasks[only(tasks, predicate, description)]


def test_destroy_unregisters_before_withdrawing_content():
    tasks = flattened("destroy.yml")
    identity = only(tasks, included("identity.yml"), "read the machine's identity")
    unregistered = only(tasks, included("unregister.yml"), "unregister the machine")
    reported = only(tasks, lambda task: task.get("name") == "Report published content removal", "report the content removal")
    removals = [index for index, task in enumerate(tasks) if FILE in task]
    assert removals and identity < unregistered < reported < min(removals)
    assert tasks[identity]["when"] == [REGISTERING, UNREGISTERS]
    assert tasks[unregistered]["when"] == [REGISTERING, UNREGISTERS, "managedos_install_anaconda_ours | bool"]
    assert holds(tasks[identity]["when"]) and not holds(tasks[identity]["when"], False)


def test_destroy_session_uses_the_reduced_policy_config():
    task = module("unregister.yml")
    arguments = task[REGISTRATION]
    assert arguments["operation"] == "unregister"
    assert not any(option in arguments for option in REGISTER_ONLY)
    assert not any(word in repr(task) for word in MATERIAL)
    scratch = render(trust_as_template("{{ managedos_install_anaconda_scratch }}"))
    assert scratch.startswith("/var/tmp/bootwright-run-scratch-1-1/")
    assert render(arguments["ssh_config"]) == scratch + "/ssh_config"
    assert render(arguments["known_hosts"]) == scratch + "/known_hosts"
    assert render(arguments["request"]) == DIGEST and arguments["timeout"] == 330
    assert task["register"] == UNREGISTRATION and task["no_log"] is True and task["failed_when"] is False
    assert not [other for other in flattened("unregister.yml") if FACT in other
                and set(other[FACT]) != {"managedos_install_anaconda_unregistration_host_key_algorithms"}]


def test_destroy_leaves_a_foreign_registration():
    retained = {"changed": False, "consumer": "", "owned": False, "repositories": {}, "state": "retained"}
    answer = {"changed": False, "ok": True, "registration": retained}
    variables = guest(**{UNREGISTRATION: answer})
    fact = destroy_tasks(lambda task: FACT in task and "managedos_install_anaconda_registration" in task[FACT],
                         "name the registration the removal left")
    registration = render(fact[FACT]["managedos_install_anaconda_registration"], **variables)
    assert registration == {"consumer": "", "state": "retained"}
    for failure in [task for task in flattened("destroy.yml") if FAIL in task]:
        assert not holds(failure["when"], **dict(failure.get("vars", {}), **variables)), failure["name"]
    arguments = completion("destroy.yml")
    evidence, refused = protocol.completion({
        "digest": DIGEST, "removed": True, "observation": {},
        "registration": render(arguments["registration"], managedos_install_anaconda_registration=registration),
        "registrationRequired": render(arguments["registrationRequired"]),
    })
    assert refused is None and evidence["postcondition"] is True and evidence["registration"] == registration
    answered = guest(marker='{"request":"another"}')
    assert render(fact[FACT]["managedos_install_anaconda_registration"], **answered) == {"consumer": "", "state": "absent"}


def test_destroy_names_an_unanswered_machine_unreachable():
    named = destroy_tasks(lambda task: (task.get(PROTOCOL) or {}).get("reason") == "registration-unreachable",
                          "name an unanswered machine")
    refused = destroy_tasks(lambda task: FAIL in task and "did not answer" in task[FAIL]["msg"], "refuse an unanswered machine")
    assert named["no_log"] is True and named["when"] == refused["when"]
    for variables in (guest(answered=False, marker=""), guest(answered=False), guest(host_key="")):
        assert holds(refused["when"], **variables)
    for variables in (guest(), guest(marker=""), guest(marker="", host_key="")):
        assert not holds(refused["when"], **variables)
    assert not holds(refused["when"], False, **guest(answered=False))
    message = render(refused[FAIL]["msg"], **guest(answered=False))
    assert message == "Machine rhel-01 did not answer through its identity channel, so its subscription registration was not removed"


@pytest.mark.parametrize("result, message", [
    ({"ok": False, "reason": "registration-unavailable", "cause": "unregister", "detail": "SENTINEL"},
     "registration-unavailable (unregister)"),
    ({"ok": False, "reason": "registration-unreachable", "detail": "SENTINEL"}, "registration-unreachable"),
    ({"ok": False, "reason": "transport", "cause": "host-key", "detail": "SENTINEL"}, "transport (host-key)"),
    ({"ok": False, "reason": "SENTINEL", "cause": "SENTINEL", "detail": "SENTINEL"}, "unexpected answer"),
    ({"ok": False, "reason": "registration-input", "cause": "SENTINEL"}, "registration-input"),
    ({"failed": True, "msg": "SENTINEL"}, "unexpected answer"),
], ids=["unavailable", "unreachable", "transport", "unknown", "unknown-cause", "no-answer"])
def test_unregistration_refusals_are_named_and_the_failure_is_closed(result, message):
    named = destroy_tasks(lambda task: UNREGISTRATION in str((task.get(PROTOCOL) or {}).get("reason", "")),
                          "name an unregistration refusal")
    assert named[PROTOCOL] == {"phase": "refused", "reason": "{{ %s.reason }}" % UNREGISTRATION} and named["no_log"] is True
    for reason in ("registration-unreachable", "registration-unavailable"):
        assert holds(named["when"], **guest(**{UNREGISTRATION: {"ok": False, "reason": reason}}))
        assert not holds(named["when"], **guest(marker='{"request":"another"}', **{UNREGISTRATION: {"ok": False, "reason": reason}}))
        assert not holds(named["when"], False, **guest(**{UNREGISTRATION: {"ok": False, "reason": reason}}))
    for other in ("transport", "registration-input", "SENTINEL"):
        assert not holds(named["when"], **guest(**{UNREGISTRATION: {"ok": False, "reason": other}}))
    assert not holds(named["when"], **guest(**{UNREGISTRATION: {"ok": True, "reason": "registration-unavailable"}}))

    refused = destroy_tasks(lambda task: FAIL in task and UNREGISTRATION in str(task.get("vars", "")),
                            "refuse an unfinished unregistration")
    assert "no_log" not in refused
    assert holds(refused["when"], **dict(refused["vars"], **guest(**{UNREGISTRATION: result})))
    assert not holds(refused["when"], **dict(refused["vars"], **guest(**{UNREGISTRATION: {"ok": True}})))
    assert not holds(refused["when"], False, **dict(refused["vars"], **guest(**{UNREGISTRATION: result})))
    assert not holds(refused["when"], **dict(refused["vars"], **guest(marker="", **{UNREGISTRATION: result})))
    rendered = render(refused[FAIL]["msg"], **dict(refused["vars"], **guest(**{UNREGISTRATION: result})))
    assert rendered == "Machine rhel-01 was not unregistered from its subscription service: " + message
    assert "SENTINEL" not in rendered


def test_removal_observation_reads_the_guest_only_for_a_registering_request():
    removal = block("observe.yml", "Read the registration a removal takes back")
    assert removal["when"] == [REMOVAL, REGISTERING, UNREGISTERS]
    observed = flattened("observe.yml")
    identity = only(observed, lambda task: included("identity.yml")(task) and REMOVAL in task["when"], "read the identity for a removal")
    read = only(observed, lambda task: included("registration_observe.yml")(task) and REMOVAL in task["when"],
                "read the registration for a removal")
    assert identity < read
    variables = dict(guest(), managedos_install_anaconda_observes_removal=True)
    assert holds(observed[read]["when"], **variables)
    assert not holds(observed[read]["when"], False, **variables)
    assert not holds(observed[read]["when"], **dict(variables, managedos_install_anaconda_observes_removal=False))
    for other in (guest(answered=False), guest(marker=""), guest(host_key="")):
        assert not holds(observed[read]["when"], **dict(other, managedos_install_anaconda_observes_removal=True))
    skipped = dict(variables, bootwright_os_install_material=dict(variables["bootwright_os_install_material"], unregister="skip"))
    assert not holds(observed[identity]["when"], **skipped) and not holds(observed[read]["when"], **skipped)


def destroy_validated(variables):
    """Whether destroy.yml's request validation of the removal decision holds."""
    checks = LOADER.load_from_file(str(ROLE / "tasks" / "destroy.yml"), trusted_as_template=True)[0]["ansible.builtin.assert"]["that"]
    templar = Templar(loader=LOADER, variables=variables)
    return all(templar.evaluate_conditional(trust_as_template(check)) for check in checks if "unregister" in check)


# The capability decides whether a registering removal reads the guest. One told
# to skip it, because its apply never recorded the marker proof that precedes
# registration or because the operator unregistered the Machine outside
# Bootwright, never reads, connects to or names the guest and publishes no
# registration; one told nothing fails its validation before any effect.
@pytest.mark.parametrize("unregister", ["guest", "skip", None], ids=["guest", "skip", "missing"])
def test_destroy_skips_the_guest_unless_told_to_unregister(unregister):
    material = {"fleetIdentity": "/run/material/fleet-id", "marker": MARKER}
    if unregister is not None:
        material["unregister"] = unregister
    variables = dict(guest(), bootwright_os_install_material=material)
    assert destroy_validated(scope(**variables)) is (unregister is not None)
    assert destroy_validated(scope(False, bootwright_os_install_material={}))
    assert render(trust_as_template("{{ managedos_install_anaconda_unregisters }}"), **variables) is (unregister == "guest")
    if unregister is None:
        return
    guest_reads = [task for task in flattened("destroy.yml")
                   if included("identity.yml")(task) or included("unregister.yml")(task)
                   or ("bootwright.core.lifecycle_scratch" in task and task["bootwright.core.lifecycle_scratch"].get("state", "present") == "present")]
    assert len(guest_reads) == 3
    for task in guest_reads:
        assert holds(task["when"], **variables) is (unregister == "guest"), task["name"]
    arguments = completion("destroy.yml")
    assert render(arguments["registrationRequired"], **variables) is (unregister == "guest")
    if unregister == "skip":
        evidence, refused = protocol.completion({
            "digest": DIGEST, "removed": True, "observation": {},
            "registration": Templar(loader=LOADER, variables={}).template(arguments["registration"]),
            "registrationRequired": render(arguments["registrationRequired"], **variables),
        })
        assert refused is None and evidence["postcondition"] is True and evidence["registration"] == {"consumer": "", "state": ""}
