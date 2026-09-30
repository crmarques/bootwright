"""A replay of a libvirt provider host or machine over what it realized changes nothing.

The provider host defines only a managed network the host does not already
carry as frozen, so a replay defines none and publishes no change, while a
network whose definition drifted is defined again and makes the apply changed.

A machine's controller authenticates against a bcrypt hash of its bound
password. bcrypt salts every hash afresh, so hashing the password on every
apply rewrote the authentication file and restarted the controller on every
replay. The apply keeps the published hash while checkpw still verifies the
password with it. That restart had also completed any earlier apply stopped
between publishing a file and restarting, so the controller now restarts
whenever a file it runs from is newer than its start.

Rendering the role's task files needs Ansible's controller (DataLoader and
Templar), which ansible-test does not offer to unit tests under
tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import base64
import pathlib
import subprocess
import sys

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
LOADER = DataLoader()
DIGEST = "d" * 64

# pyca/bcrypt as the role's program calls it: gensalt draws a fresh random
# salt, hashpw reads the version, cost and first 22 salt characters of what it
# is given, which may be a whole hash, and refuses anything else as an invalid
# salt, and checkpw is hashpw over the hash compared in constant time
# (https://github.com/pyca/bcrypt, src/_bcrypt/src/lib.rs and README.rst).
# The digest below is not bcrypt's, which no unit test needs.
STAND_IN_BCRYPT = r'''
import hashlib
import hmac
import os
import re

ALPHABET = b"./ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
SETTING = re.compile(rb"^\$(2[abxy])\$([0-9]{2})\$([./A-Za-z0-9]{22})")


def encode(raw, length):
    return bytes(ALPHABET[byte % 64] for byte in raw[:length])


def gensalt(rounds=12, prefix=b"2b"):
    return b"$" + prefix + b"$" + b"%02d" % rounds + b"$" + encode(os.urandom(22), 22)


def hashpw(password, salt):
    if len(password) > 72:
        raise ValueError("password cannot be longer than 72 bytes")
    setting = SETTING.match(salt)
    if setting is None:
        raise ValueError("Invalid salt")
    return setting.group(0) + encode(hashlib.sha256(setting.group(0) + password).digest(), 31)


def checkpw(password, hashed_password):
    return hmac.compare_digest(hashpw(password, hashed_password), hashed_password)
'''


def load(role, name):
    loaded = LOADER.load_from_file(str(ROLES / role / "tasks" / name), trusted_as_template=True)
    return [task for task in loaded if isinstance(task, dict)]


def defaults(role):
    return dict(LOADER.load_from_file(str(ROLES / role / "defaults" / "main.yml"), trusted_as_template=True))


def only(tasks, predicate, what):
    found = [task for task in tasks if predicate(task)]
    assert len(found) == 1, "expected one task that %s, found %d" % (what, len(found))
    return found[0]


def argv_of(task):
    return [str(value) for value in (task.get("ansible.builtin.command") or {}).get("argv") or []]


# The provider host.

MANAGED = {"name": "bootwright-lab-guests", "bridge": "virbr-lab", "address": "198.51.100.1/24", "forward": "nat", "managed": True}
EXTERNAL = {"name": "bootwright-lab-uplink", "bridge": "br0", "managed": False}


def network(**observed):
    """What libvirt_host_inspect reports for the managed network once its driver runs."""
    entry = {"answered": True, "bridge": True, "definition": True, "managed": True, "name": MANAGED["name"],
             "owned": True, "state": "active", "uuid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00"}
    entry.update(observed)
    return entry


UPLINK = {"answered": False, "bridge": True, "definition": False, "managed": False, "name": EXTERNAL["name"],
          "owned": False, "state": "", "uuid": ""}


def host_scope(observed):
    """The role's defaults, the frozen request and the tasks a replay leaves unchanged."""
    variables = defaults("substrate_libvirt_host")
    variables.update(
        bootwright_substrate_host_request={"identity": {"context": "lab"}, "networks": [MANAGED, EXTERNAL],
                                           "uri": "qemu:///system"},
        bootwright_substrate_host_digest=DIGEST,
        substrate_libvirt_host_running={"observation": {"networks": [observed, UPLINK], "pool": "active"}},
        substrate_libvirt_host_packages={"changed": False, "skipped": True},
        substrate_libvirt_host_daemon={"changed": False, "skipped": True},
        substrate_libvirt_host_pool_directory={"changed": False},
    )
    return variables


def host_attempt(observed):
    """The networks an apply defines over this observation, and the outcome it publishes."""
    tasks = load("substrate_libvirt_host", "apply.yml")
    define = only(tasks, lambda task: "net-define" in argv_of(task), "defines a network")
    completion = only(
        tasks,
        lambda task: (task.get("bootwright.core.substrate_host_protocol") or {}).get("phase") == "completed",
        "publishes the completion",
    )
    templar = Templar(loader=LOADER, variables=host_scope(observed))
    defined = [entry["name"] for entry in templar.template(define["loop"])]
    return defined, templar.template(completion["bootwright.core.substrate_host_protocol"]["outcome"])


def test_a_replay_over_networks_the_host_carries_defines_nothing_and_publishes_no_change():
    assert host_attempt(network()) == ([], "unchanged")


@pytest.mark.parametrize("observed", [
    network(definition=False),
    network(definition=False, state="inactive"),
    network(answered=True, definition=False, owned=False, state="", uuid=""),
], ids=["drifted while active", "drifted while stopped", "not defined"])
def test_a_network_the_host_does_not_carry_as_frozen_is_defined_and_the_apply_is_changed(observed):
    assert host_attempt(observed) == ([MANAGED["name"]], "changed")


# The machine's controller credential.

MACHINE = "substrate_libvirt_machine"
IMAGE = "quay.io/metal3-io/sushy-tools@sha256:" + "0" * 64


@pytest.fixture(name="material")
def material_fixture(tmp_path):
    """The bound credential as the runner lends it: one file per value."""
    (tmp_path / "user").write_text("admin\n")
    (tmp_path / "password").write_text("correct horse\n")
    (tmp_path / "bcrypt.py").write_text(STAND_IN_BCRYPT)
    return tmp_path


UNIT = "bootwright-lab-bmc-rhel-01"


def machine_scope(material, password="password"):
    variables = defaults(MACHINE)
    variables.update(
        bootwright_substrate_machine_request={
            "controller": {"image": IMAGE, "unit": UNIT},
            "identity": {"context": "lab", "object": "rhel-01"},
        },
        bootwright_substrate_machine_material={
            "controllerPassword": str(material / password),
            "controllerUser": str(material / "user"),
        },
    )
    return variables


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


def publisher(tasks, file):
    """The one task that publishes this file the controller runs from."""
    if file == "htpasswd":
        return only(tasks, lambda task: str((task.get("ansible.builtin.copy") or {}).get("dest", "")).endswith("/htpasswd"),
                    "publishes the authentication file")
    source = {"conf.py": "emulator.conf.j2", "unit": "unit.container.j2"}[file]
    return only(tasks, lambda task: (task.get("ansible.builtin.template") or {}).get("src") == source, "publishes " + source)


def credential_steps(tasks):
    """The tasks that observe, read, derive and publish the authentication file."""
    derive = only(tasks, lambda task: "bcrypt" in " ".join(argv_of(task)), "derives the password hash")
    publish = publisher(tasks, "htpasswd")
    read = only(tasks, lambda task: "ansible.builtin.slurp" in task, "reads the published file")
    observe = only(tasks, lambda task: "ansible.builtin.stat" in task and reading(read, task), "observes the file it reads")
    assert in_order(tasks, observe, read, derive, publish)
    return observe, read, derive, publish


SKIPPED = {"changed": False, "skipped": True}


def publish_credentials(material, found, password="password"):
    """The authentication file one apply publishes over what it found at its path.

    `found` is the content of the file there, None when there is none, or what
    the read returned as given. The observation and the read are the role's
    own, over the path the file is published at, and each registers where the
    role's next step reads it. The program runs as the role hands it to the
    image's python3, with the stand-in bcrypt as the only module it imports
    that is not the standard library's. The command module ends its standard
    input with a newline.
    """
    observe, read, derive, publish = credential_steps(load(MACHINE, "apply.yml"))
    variables = machine_scope(material, password)
    templar = Templar(loader=LOADER, variables=variables)
    destination = templar.template(publish["ansible.builtin.copy"]["dest"])
    assert templar.template(observe["ansible.builtin.stat"]["path"]) == destination
    assert templar.template(read["ansible.builtin.slurp"]["src"]) == destination
    variables[observe["register"]] = {"changed": False, "stat": {"exists": False} if found is None else REGULAR}
    assert runs(variables, read) is (found is not None)
    variables[read["register"]] = SKIPPED if found is None else slurped(found) if isinstance(found, str) else found
    command = Templar(loader=LOADER, variables=variables).template(derive["ansible.builtin.command"])
    argv = [str(value) for value in command["argv"]]
    assert argv[:2] == ["/usr/bin/podman", "run"] and IMAGE in argv
    program = argv[argv.index("-c") + 1]
    derived = subprocess.run(
        [sys.executable, "-c", program],
        input=(str(command["stdin"]) + "\n").encode(),
        capture_output=True,
        check=True,
        env={"PYTHONPATH": str(material)},
    )
    variables[derive["register"]] = {"stdout": derived.stdout.decode()}
    return str(Templar(loader=LOADER, variables=variables).template(publish["ansible.builtin.copy"]["content"]))


def slurped(content):
    """What ansible.builtin.slurp returns for a file holding this content."""
    return {"changed": False, "content": base64.b64encode(content.encode()).decode(), "encoding": "base64"}


def verifies(material, content, password):
    """Whether the controller, reading this file, accepts the user and password."""
    user, _separator, hashed = content.partition(":")
    program = "import bcrypt, sys; sys.exit(0 if bcrypt.checkpw(sys.argv[1].encode(), sys.argv[2].encode()) else 1)"
    checked = subprocess.run([sys.executable, "-c", program, password, hashed], env={"PYTHONPATH": str(material)}, check=False)
    return user == "admin" and checked.returncode == 0


# What ansible.builtin.stat reports for a regular file.
REGULAR = {"exists": True, "isreg": True, "isdir": False}


def test_a_replay_publishes_the_authentication_file_it_found(material):
    first = publish_credentials(material, None)
    assert verifies(material, first, "correct horse")
    # Salting is why keeping the hash matters: a second fresh hash differs.
    assert publish_credentials(material, None) != first
    assert publish_credentials(material, first) == first


def test_a_password_the_published_hash_no_longer_verifies_is_hashed_again(material):
    first = publish_credentials(material, None)
    (material / "rotated").write_text("battery staple\n")
    rotated = publish_credentials(material, first, password="rotated")
    assert rotated != first
    assert verifies(material, rotated, "battery staple")
    assert not verifies(material, rotated, "correct horse")


@pytest.mark.parametrize("found", [
    "admin:not-a-bcrypt-hash",
    "admin",
    "",
    {"changed": False, "content": "not base64 at all!", "encoding": "base64"},
], ids=["another digest", "no hash", "empty", "not base64"])
def test_a_published_file_that_verifies_nothing_is_replaced_by_a_fresh_hash(material, found):
    replaced = publish_credentials(material, found)
    assert verifies(material, replaced, "correct horse")


# The machine's running controller.

# podman inspect executes its --format template over the container's inspect
# data, whose State.StartedAt is a time.Time (InspectContainerState in
# libpod/define/container_inspect.go, https://github.com/containers/podman), so
# .State.StartedAt.UnixNano prints the start in nanoseconds since the epoch: for
# a container started at 2026-09-30 11:38:20.675992231 -03, podman 5.8.4
# printed this, and the command module registers it without the newline.
STARTED = 1790779100675992231
EARLIER, LATER = STARTED / 1e9 - 3600, STARTED / 1e9 + 60
RUNNING = {"changed": False, "rc": 0, "stdout": str(STARTED)}
NO_CONTAINER = {"changed": False, "rc": 125, "stdout": ""}


def destination(task):
    return (task.get("ansible.builtin.copy") or task.get("ansible.builtin.template"))["dest"]


def controller_steps(tasks):
    """The tasks that observe the controller's files and start, and restart it."""
    restart = only(tasks, lambda task: argv_of(task)[:2] == ["/usr/bin/systemctl", "restart"], "restarts the controller")
    files = only(tasks, lambda task: "ansible.builtin.stat" in task and reading(restart, task), "observes its files")
    started = only(tasks, lambda task: argv_of(task)[:2] == ["/usr/bin/podman", "inspect"] and reading(restart, task),
                   "reads its start")
    publishers = {file: publisher(tasks, file) for file in ("conf.py", "htpasswd", "unit")}
    assert all(in_order(tasks, publishing, files, restart) for publishing in publishers.values())
    assert in_order(tasks, started, restart)
    return restart, files, started, publishers


def controller_attempt(material, newer=(), changed=(), start=None, unit="active"):
    """Whether an apply over the controller restarts it, and the outcome it publishes.

    Every step reports no change unless `changed` names its file; `newer`
    names the files published after the controller started, which podman
    answers `start` for. The files observed are the ones the role publishes,
    and the start read is the container's the unit runs.
    """
    tasks = load(MACHINE, "apply.yml")
    restart, files, started, publishers = controller_steps(tasks)
    variables = machine_scope(material)
    variables.update({task["register"]: {"changed": False} for task in tasks if task.get("register")})
    variables["substrate_libvirt_machine_before"] = {
        "observation": {"controller": IMAGE, "disks": [], "domain": "bootwright-lab-rhel-01", "unit": unit},
    }
    templar = Templar(loader=LOADER, variables=variables)
    paths = {templar.template(destination(task)): file for file, task in publishers.items()}
    observed = templar.template(files["loop"])
    assert sorted(observed) == sorted(paths)
    unit_file = templar.template(trust_as_template((ROLES / MACHINE / "templates" / "unit.container.j2").read_text()))
    assert templar.template(started["ansible.builtin.command"]["argv"]) == [
        "/usr/bin/podman", "inspect", "--type", "container", "--format", "{{.State.StartedAt.UnixNano}}", UNIT,
    ]
    assert "ContainerName=%s" % UNIT in unit_file.splitlines()
    for file in changed:
        variables[publishers[file]["register"]] = {"changed": True}
    if runs(variables, files):
        variables[files["register"]] = {"changed": False, "results": [
            {"item": path, "stat": dict(REGULAR, mtime=LATER if paths[path] in newer else EARLIER)} for path in observed
        ]}
        variables[started["register"]] = RUNNING if start is None else start
    else:
        variables[files["register"]] = variables[started["register"]] = SKIPPED
    restarted = runs(variables, restart)
    variables[restart["register"]] = {"changed": True} if restarted else SKIPPED
    completion = only(
        tasks,
        lambda task: (task.get("bootwright.core.substrate_machine_protocol") or {}).get("phase") == "completed",
        "publishes the completion",
    )
    outcome = Templar(loader=LOADER, variables=variables).template(completion["bootwright.core.substrate_machine_protocol"]["outcome"])
    return restarted, outcome


def test_a_replay_over_a_controller_started_after_its_files_restarts_nothing(material):
    assert controller_attempt(material) == (False, "unchanged")


# An attempt stopped between publishing a file and restarting leaves the
# controller on the file it mounted, while the next attempt finds the published
# one unchanged: a rotated password whose hash the file already holds, a moved
# port, or another image.
@pytest.mark.parametrize("file", ["conf.py", "htpasswd", "unit"])
def test_a_file_published_after_the_controller_started_restarts_it(material, file):
    assert controller_attempt(material, newer=(file,)) == (True, "changed")


def test_an_interrupted_rotation_restarts_the_controller_on_the_hash_it_kept(material):
    (material / "rotated").write_text("battery staple\n")
    published = publish_credentials(material, None, password="rotated")
    assert publish_credentials(material, published, password="rotated") == published
    assert controller_attempt(material, newer=("htpasswd",)) == (True, "changed")


@pytest.mark.parametrize("attempt", [
    {"changed": ("conf.py",)},
    {"start": NO_CONTAINER},
    {"start": {"changed": False, "rc": 0, "stdout": "not a time"}},
], ids=["a file this attempt changed", "no container", "an unreadable start"])
def test_a_controller_not_proved_to_run_its_files_is_restarted(material, attempt):
    assert controller_attempt(material, **attempt) == (True, "changed")


def test_a_controller_that_is_not_running_is_started_rather_than_restarted(material):
    assert controller_attempt(material, newer=("htpasswd",), unit="inactive") == (False, "changed")
