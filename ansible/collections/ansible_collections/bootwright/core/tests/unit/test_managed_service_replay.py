"""A running managed service restarts whenever a file it runs from is newer than its start.

A managed NTP, DNS or proxy service's daemon reads its configuration, and the
artifact server its serving material as well, once when its container starts,
and its unit takes effect only at a start. Each configuration is mounted on its
own, so publishing one replaces the host's file while the daemon keeps the one
it read. The roles restarted a running service only when this attempt reported
one of those files changed, so an attempt stopped between publishing a file and
restarting left the service on the earlier one, and the next attempt, finding
the published file unchanged, never restarted it.

Rendering the role's task files needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template
from ansible_collections.bootwright.core.plugins.module_utils.infra_service import UNIT_DIRECTORY

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
LOADER = DataLoader()
DIGEST = "d" * 64
IMAGE = "registry.example/service@sha256:" + "0" * 64
SERVER = "infra_artifact_server_nginx"

MANAGED = "infra_managed_service"

# The one managed-service role selects its templates by the frozen kind.
MANAGED_SOURCES = {
    "configuration": "{{ infra_managed_service_kind.configuration_template }}",
    "unitSource": "{{ infra_managed_service_kind.unit_template }}",
    "request": "bootwright_managed_service_request", "digest": "bootwright_managed_service_digest",
}

# Each service, by managed-service kind or by the artifact server's role, with
# the role that realizes it, the variables it takes, the sources it templates
# and the files it runs from. The content root and unit are the ones the
# planners name: managedservice.ContentRoot and UnitName with each catalog's
# slug, and artifactserver.ContentRoot with the artifact server's unit prefix.
SERVICES = {
    "NTPServer": dict(MANAGED_SOURCES, role=MANAGED, kind="NTPServer", service="time",
                      contentRoot="/var/lib/bootwright-services/lab/ntp/time", unit="bootwright-lab-ntp-time"),
    "DNSServer": dict(MANAGED_SOURCES, role=MANAGED, kind="DNSServer", service="resolver",
                      contentRoot="/var/lib/bootwright-services/lab/dns/resolver", unit="bootwright-lab-dns-resolver"),
    "Proxy": dict(MANAGED_SOURCES, role=MANAGED, kind="Proxy", service="egress",
                  contentRoot="/var/lib/bootwright-services/lab/proxy/egress", unit="bootwright-lab-proxy-egress"),
    SERVER: {
        "role": SERVER,
        "request": "bootwright_artifact_server_request", "digest": "bootwright_artifact_server_digest",
        "configuration": "nginx.conf.j2", "unitSource": "unit.container.j2", "service": "lab-artifacts",
        "contentRoot": "/var/lib/bootwright-services/lab/artifact-server/lab-artifacts",
        "unit": "bootwright-lab-artifacts-lab-artifacts",
    },
}

RUNS_FROM = {role: ("configuration", "unit") for role in SERVICES}
RUNS_FROM[SERVER] += ("certificate", "key")

# The serving material as the runner lends it: one file per value.
MATERIAL = {"certificate": "/run/bootwright/material/certificate", "privateKey": "/run/bootwright/material/privateKey"}

# podman inspect executes its --format template over the container's inspect
# data, whose State.StartedAt is a time.Time (InspectContainerState in
# libpod/define/container_inspect.go, https://github.com/containers/podman), so
# .State.StartedAt.UnixNano prints the start in nanoseconds since the epoch.
# This is what podman 5.8.4 printed for a container started at 2026-09-30
# 11:38:20.675992231 -03, the observation test_substrate_libvirt_replay.py
# records, and the command module registers it without the newline. podman
# exits 125 when the error is podman's own, such as no container of that name
# (docs/source/markdown/podman.1.md, "Exit Codes"). ansible.builtin.stat
# reports mtime as the float seconds of os.stat's st_mtime.
STARTED = 1790779100675992231
EARLIER, LATER = STARTED / 1e9 - 3600, STARTED / 1e9 + 60
RUNNING = {"changed": False, "rc": 0, "stdout": str(STARTED)}
NO_CONTAINER = {"changed": False, "rc": 125, "stdout": ""}
SKIPPED = {"changed": False, "skipped": True}


def load(role):
    loaded = LOADER.load_from_file(str(ROLES / SERVICES[role]["role"] / "tasks" / "apply.yml"), trusted_as_template=True)
    return [task for task in loaded if isinstance(task, dict)]


def only(tasks, predicate, what):
    found = [task for task in tasks if predicate(task)]
    assert len(found) == 1, "expected one task that %s, found %d" % (what, len(found))
    return found[0]


def argv_of(task):
    return [str(value) for value in (task.get("ansible.builtin.command") or {}).get("argv") or []]


def action(task, suffix):
    """The arguments of the task's module whose name ends in the suffix, or None."""
    return next((arguments for module, arguments in task.items() if module.endswith(suffix)), None)


def conditions(task):
    when = task.get("when", [])
    return when if isinstance(when, list) else [when]


def runs(variables, task):
    """Whether a play runs the task in this scope: its conditions in order, the first false one ending it."""
    templar = Templar(loader=LOADER, variables=variables)
    return all(templar.evaluate_conditional(condition) for condition in conditions(task))


def reading(task, other):
    """Whether the task's conditions read what the other task registers."""
    return bool(other.get("register")) and other["register"] in " ".join(str(condition) for condition in conditions(task))


def in_order(tasks, *steps):
    places = [next(index for index, task in enumerate(tasks) if task is step) for step in steps]
    return places == sorted(places)


def scope(role, tls):
    """The role's defaults and fixed vars and the frozen request, with serving material when `tls`."""
    service = SERVICES[role]
    directory = ROLES / service["role"]
    variables = dict(LOADER.load_from_file(str(directory / "defaults" / "main.yml"), trusted_as_template=True))
    if (directory / "vars" / "main.yml").is_file():
        variables.update(LOADER.load_from_file(str(directory / "vars" / "main.yml"), trusted_as_template=True))
    request = {"bindAddress": "192.0.2.10", "contentRoot": service["contentRoot"], "image": IMAGE, "unit": service["unit"],
               "identity": {"context": "lab", "service": service["service"]}}
    if "kind" in service:
        request["kind"] = service["kind"]
    if role == SERVER:
        request["tls"] = {"minVersion": "TLSv1.2"} if tls else {}
        variables["bootwright_artifact_server_material"] = MATERIAL if tls else {}
    variables.update({service["request"]: request, service["digest"]: DIGEST})
    return variables


def publishers(tasks, role):
    """The task that publishes each file the service runs from."""
    def templating(source):
        return lambda task: (task.get("ansible.builtin.template") or {}).get("src") == source

    def copying(leaf):
        return lambda task: str((task.get("ansible.builtin.copy") or {}).get("dest", "")).endswith(leaf)

    predicates = {"configuration": templating(SERVICES[role]["configuration"]), "unit": templating(SERVICES[role]["unitSource"]),
                  "certificate": copying("/server.crt"), "key": copying("/server.key")}
    return {file: only(tasks, predicates[file], "publishes the " + file) for file in RUNS_FROM[role]}


def destination(task):
    return (task.get("ansible.builtin.copy") or task.get("ansible.builtin.template"))["dest"]


def service_steps(tasks, role):
    """The tasks that observe the service's files and start, start it, restart it and publish the completion."""
    restart = only(tasks, lambda task: argv_of(task)[:2] == ["/usr/bin/systemctl", "restart"], "restarts the service")
    start = only(tasks, lambda task: argv_of(task)[:2] == ["/usr/bin/systemctl", "start"], "starts the service")
    files = only(tasks, lambda task: "ansible.builtin.stat" in task and reading(restart, task), "observes its files")
    started = only(tasks, lambda task: argv_of(task)[:2] == ["/usr/bin/podman", "inspect"] and reading(restart, task),
                   "reads its start")
    before = only(tasks, lambda task: action(task, "_inspect") is not None and reading(start, task), "observes the unit")
    completion = only(tasks, lambda task: (action(task, "_protocol") or {}).get("phase") == "completed",
                      "publishes the completion")
    published = publishers(tasks, role)
    assert all(in_order(tasks, publishing, before, files, restart) for publishing in published.values())
    assert in_order(tasks, before, started, start, restart)
    return restart, start, files, started, before, completion, published


def attempt(role, newer=(), changed=(), start=None, unit="active", tls=True):
    """Whether an apply over the service restarts it, and the outcome it publishes.

    Every step reports no change unless `changed` names its file; `newer`
    names the files published after the service started, which podman answers
    `start` for. The files observed are the ones the role publishes in this
    scope, and the start read is the container's the unit runs.
    """
    tasks = load(role)
    restart, starting, files, started, before, completion, published = service_steps(tasks, role)
    variables = scope(role, tls)
    variables.update({task["register"]: {"changed": False} for task in tasks if task.get("register")})
    variables[before["register"]] = {"observation": {"unit": unit}}
    templar = Templar(loader=LOADER, variables=variables)
    paths = {templar.template(destination(task)): file for file, task in published.items() if runs(variables, task)}
    unit_template = templar.template(published["unit"]["ansible.builtin.template"]["src"])
    unit_file = templar.template(trust_as_template((ROLES / SERVICES[role]["role"] / "templates" / unit_template).read_text()))
    assert "ContainerName=%s" % SERVICES[role]["unit"] in unit_file.splitlines()
    assert templar.template(started["ansible.builtin.command"]["argv"]) == [
        "/usr/bin/podman", "inspect", "--type", "container", "--format", "{{.State.StartedAt.UnixNano}}", SERVICES[role]["unit"],
    ]
    for file in changed:
        variables[published[file]["register"]] = {"changed": True}
    if runs(variables, files):
        observed = templar.template(files["loop"])
        assert sorted(observed) == sorted(paths)
        variables[files["register"]] = {"changed": False, "results": [
            {"item": path, "stat": {"exists": True, "isreg": True, "mtime": LATER if paths[path] in newer else EARLIER}}
            for path in observed
        ]}
        variables[started["register"]] = RUNNING if start is None else start
    else:
        variables[files["register"]] = variables[started["register"]] = SKIPPED
    assert runs(variables, starting) is (unit != "active")
    restarted = runs(variables, restart)
    variables[restart["register"]] = {"changed": True} if restarted else SKIPPED
    outcome = Templar(loader=LOADER, variables=variables).template(action(completion, "_protocol")["outcome"])
    return restarted, outcome


@pytest.mark.parametrize("role", SERVICES)
def test_a_replay_over_a_service_started_after_its_files_restarts_nothing(role):
    assert attempt(role) == (False, "unchanged")


# An attempt stopped between publishing a file and restarting leaves the
# service on the file it read, while the next attempt finds the published one
# unchanged.
@pytest.mark.parametrize(("role", "file"), [(role, file) for role in SERVICES for file in RUNS_FROM[role]])
def test_a_file_published_but_not_applied_restarts_the_service(role, file):
    assert attempt(role, newer=(file,)) == (True, "changed")


@pytest.mark.parametrize("role", SERVICES)
@pytest.mark.parametrize("attempted", [
    {"changed": ("configuration",)},
    {"start": NO_CONTAINER},
    {"start": {"changed": False, "rc": 0, "stdout": "not a time"}},
], ids=["a file this attempt changed", "no container", "an unreadable start"])
def test_a_service_not_proved_to_run_its_files_is_restarted(role, attempted):
    assert attempt(role, **attempted) == (True, "changed")


@pytest.mark.parametrize("role", SERVICES)
def test_a_service_that_is_not_running_is_started_rather_than_restarted(role):
    assert attempt(role, newer=("configuration",), unit="inactive") == (False, "changed")


def test_a_server_without_serving_material_runs_from_its_configuration_and_unit():
    assert attempt(SERVER, tls=False) == (False, "unchanged")
    assert attempt(SERVER, newer=("unit",), tls=False) == (True, "changed")


def published_inspections(role):
    """The inspection whose observation each completion publishes, in the apply and in the observation."""
    found = []
    for tasks in ("apply.yml", "observe.yml"):
        loaded = [task for task in LOADER.load_from_file(str(ROLES / SERVICES[role]["role"] / "tasks" / tasks),
                                                         trusted_as_template=True)
                  if isinstance(task, dict)]
        completion = only(loaded, lambda task: (action(task, "_protocol") or {}).get("phase") == "completed",
                          "publishes the completion")
        published = str(action(completion, "_protocol")["observation"])
        found.append(only(loaded, lambda task, published=published: action(task, "_inspect") is not None
                          and "%s.observation" % task.get("register") in published, "is the published inspection"))
    return found


# A managed network service's completion and its observation publish the start
# compared with every file the apply restarts over, the unit definition the
# inspection always compares included, so an unknown outcome is not resolved
# complete while the service runs an earlier file.
@pytest.mark.parametrize("role", [role for role in SERVICES if role != SERVER])
def test_every_published_observation_compares_the_start_with_the_files_the_apply_restarts_over(role):
    files = service_steps(load(role), role)[2]
    templar = Templar(loader=LOADER, variables=scope(role, tls=True))
    restarted_over = sorted(templar.template(files["loop"]))
    unit = UNIT_DIRECTORY + SERVICES[role]["unit"] + ".container"
    for inspection in published_inspections(role):
        compared = templar.template(action(inspection, "_inspect").get("runs_from", []))
        assert sorted(compared + [unit]) == restarted_over
