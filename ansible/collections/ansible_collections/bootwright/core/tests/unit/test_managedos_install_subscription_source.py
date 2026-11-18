"""An installation that installs from its subscription publishes its
registration material only inside the boot.

The installer's %pre fetches the organization, the activation key and the
Satellite's CA bundle from the private URL its Kickstart names, so the three
are published beneath the unguessable directory of the attempt, beside the
installer image, only by the boot an apply performs: after the substrate
proved the target, before the media is inserted, each with its first byte
fetched through the listener, and never by a replay. The private subtree is
withdrawn before the completion is inspected
(test_managedos_install_encryption.py), and the Kickstart's ownership record
names the digest of the request the adapter substitutes.

Rendering the role's files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib

import pytest

from ansible.template import Templar, trust_as_template
from ansible_collections.bootwright.core.tests.unit import listener_proof, role_tasks
from ansible_collections.bootwright.core.tests.unit.role_tasks import LOADER


ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
COMMAND = "ansible.builtin.command"
COPY = "ansible.builtin.copy"
INCLUDE = "ansible.builtin.include_tasks"
INCLUDE_ROLE = "ansible.builtin.include_role"
TASKS = "private_registration.yml"
TOKEN = "0123456789abcdef" * 4
SERVED = "/var/lib/bootwright-hosted/lab/services/artifact-server/lab-artifacts/public"
PRIVATE = {"path": SERVED + "/private/os/rhel-01", "url": "https://192.0.2.1:8443/private/os/rhel-01"}
FILES = ["rhsm-organization", "rhsm-activation-key", "rhsm-satellite-ca"]
MATERIAL = {"artifactCertificate": "/run/material/artifact-ca", "rhsmOrganization": "/run/material/rhsm-organization",
            "rhsmActivationKey": "/run/material/rhsm-activation-key", "rhsmSatelliteCA": "/run/material/rhsm-satellite-ca"}
SATELLITE = {"hostname": "satellite.example.test", "contentBaseURL": "https://satellite.example.test/pulp/content",
             "trustBundleRef": "satellite-ca"}
REGISTRATION = {"activationKeyRef": "rhsm-activation-key", "entitlementRef": "rhel", "organizationRef": "rhsm-organization",
                "repositories": {"disable": [], "enable": []}, "satellite": SATELLITE}
SHAPES = {
    "subscription-source": {"private": dict(PRIVATE), "registration": dict(REGISTRATION, installSource=True)},
    "registered": {"private": dict(PRIVATE), "registration": dict(REGISTRATION)},
    "unregistered": {"private": dict(PRIVATE)},
}


def flattened(name):
    """A task file's tasks in play order, each block opened and its conditions carried onto every task inside it."""
    return [dict(visit.task, when=list(visit.conditions))
            for visit in role_tasks.visits(role_tasks.role_tasks(ROLE, name)) if "block" not in visit.task]


def only(tasks, predicate, description):
    found = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def included(name):
    return lambda task: task.get(INCLUDE) == name


def entry(tasks_from):
    return lambda task: (task.get(INCLUDE_ROLE) or {}).get("tasks_from") == tasks_from


def scope(shape="subscription-source", **extra):
    values = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    request = {"identity": {"context": "lab", "object": "rhel-01"}, "tlsCertificateRef": "lab-artifacts-tls",
               "target": {"channel": "guest-agent", "physical": False}}
    request.update(SHAPES[shape])
    values.update({
        "bootwright_run_scratch": "/var/tmp/bootwright-scratch-1-1",
        "bootwright_os_install_request": request,
        "bootwright_os_install_material": dict(MATERIAL),
        "managedos_install_anaconda_served_root": SERVED,
        "managedos_install_anaconda_token": {"stdout": TOKEN + "\n"},
    })
    values.update(extra)
    return values


def render(value, shape="subscription-source", **variables):
    return Templar(loader=LOADER, variables=scope(shape, **variables)).template(value)


def holds(conditions, shape="subscription-source"):
    templar = Templar(loader=LOADER, variables=scope(shape))
    listed = conditions if isinstance(conditions, list) else [conditions]
    return all(templar.evaluate_conditional(trust_as_template(condition)) for condition in listed)


def registration_files():
    return render(trust_as_template("{{ managedos_install_anaconda_registration_files }}"))


def test_only_a_subscription_source_publishes_registration_material():
    boot = flattened("boot.yml")
    published = boot[only(boot, included(TASKS), "publish the registration material")]
    assert holds(published["when"])
    assert not holds(published["when"], "registered") and not holds(published["when"], "unregistered")


def test_the_material_is_published_inside_the_boot_after_the_proof_and_before_the_insert():
    boot = flattened("boot.yml")
    proved = only(boot, entry("pre_boot"), "prove the target")
    published = only(boot, included(TASKS), "publish the registration material")
    inserted = only(boot, entry("media_insert"), "insert the media")
    assert proved < published < inserted


def test_a_replay_never_publishes_the_material_again():
    names = sorted(path.name for path in (ROLE / "tasks").glob("*.yml"))
    includes = [name for name in names for task in flattened(name) if task.get(INCLUDE) == TASKS]
    assert includes == ["boot.yml"]
    for name in ("apply.yml", "private.yml", "observe.yml", "destroy.yml"):
        for task in flattened(name):
            text = repr(dict(task, name=""))
            assert "rhsm-organization" not in text and "rhsm-activation-key" not in text, (name, task.get("name"))


def staged_items():
    tasks = flattened(TASKS)
    return render(tasks[only(tasks, lambda task: COPY in task, "stage the material")]["loop"])


def test_the_three_files_are_the_lent_material_under_the_names_the_kickstart_fetches():
    assert registration_files() == FILES
    assert staged_items() == [
        {"name": "rhsm-organization", "source": "/run/material/rhsm-organization"},
        {"name": "rhsm-activation-key", "source": "/run/material/rhsm-activation-key"},
        {"name": "rhsm-satellite-ca", "source": "/run/material/rhsm-satellite-ca"},
    ]


def test_each_file_is_staged_protected_labelled_and_then_renamed():
    tasks = flattened(TASKS)
    copy = tasks[only(tasks, lambda task: COPY in task, "stage the material")]
    assert copy[COPY]["mode"] == "0640" and copy[COPY]["owner"] == copy[COPY]["group"] == "root"
    assert copy[COPY]["remote_src"] is True
    label = only(tasks, lambda task: (task.get(COMMAND) or {}).get("argv", [""])[0] == "/usr/bin/chcon", "label the material")
    rename = only(tasks, lambda task: (task.get(COMMAND) or {}).get("argv", [""])[0] == "/usr/bin/mv", "publish the material")
    assert tasks.index(copy) < label < rename
    for item in staged_items():
        staged = PRIVATE["path"] + "/" + TOKEN + "/.stage/" + item["name"]
        assert render(copy[COPY]["src"], item=item) == item["source"]
        assert render(copy[COPY]["dest"], item=item) == staged
        relative = ".stage/" + item["name"]
        assert render(tasks[label][COMMAND]["argv"], item=item["name"]) == ["/usr/bin/chcon", "--reference", SERVED, relative]
        assert render(tasks[rename][COMMAND]["argv"], item=item["name"]) == [
            "/usr/bin/mv", "--no-target-directory", relative, item["name"]]
        for task in (tasks[label], tasks[rename]):
            assert render(task[COMMAND]["chdir"]) + "/" + relative == staged
    for task in (tasks[label], tasks[rename]):
        assert render(task["loop"]) == FILES
    for task in (copy, tasks[label], tasks[rename]):
        assert task["no_log"] is True, task["name"]


@pytest.mark.parametrize("leaf", FILES)
def test_the_first_byte_of_each_file_is_fetched_through_the_listener(leaf):
    tasks = flattened(TASKS)
    fetch = tasks[-1]
    assert listener_proof.includes(fetch) and "no_log" not in fetch
    assert render(fetch["loop"]) == registration_files()
    loop_var = fetch["loop_control"]["loop_var"]
    item = leaf
    address = PRIVATE["url"] + "/" + TOKEN + "/" + leaf
    proved = listener_proof.run(fetch, scope(**{loop_var: item}), {"status": 206})
    assert proved.fetched["url"] == address and proved.fetched["ca_path"] == "/run/material/artifact-ca"
    assert proved.refusal is None
    refused = listener_proof.run(fetch, scope(**{loop_var: item}), {
        "status": -1, "msg": "Request failed: <urlopen error [Errno 111] Connection refused> " + address}).refusal
    assert TOKEN not in refused, refused
    assert refused.startswith("the private " + leaf + " could not be") and "so no machine was given media" in refused, refused


def test_the_rendered_kickstart_names_the_digest_of_its_request():
    applied = flattened("apply.yml")
    rendered = applied[only(applied, lambda task: "bootwright.core.private_file" in task
                            and "bootwright_os_install_request.kickstart" in str(task["bootwright.core.private_file"].get("content")),
                            "render the kickstart")]
    assert rendered["no_log"] is True
    content = render(rendered["bootwright.core.private_file"]["content"], bootwright_os_install_digest="ab" * 32,
                     bootwright_os_install_request=dict(scope()["bootwright_os_install_request"], kickstart=(
                         "printf '{\"consumer\":\"\",\"request\":\"%s\"}\\n' '@@BOOTWRIGHT_REQUEST_DIGEST@@'\n")),
                     bootwright_os_install_material=dict(MATERIAL, marker="{}", authorizedKey="ssh-ed25519 AAAA"),
                     managedos_install_anaconda_certificate="", managedos_install_anaconda_tree_certificate="")
    assert content == "printf '{\"consumer\":\"\",\"request\":\"%s\"}\\n' '" + "ab" * 32 + "'\n"


def test_an_installer_that_never_finishes_names_what_its_satellite_decides():
    awaited = flattened("await.yml")
    stopped = awaited[only(awaited, lambda task: task.get("name") == "Stop at an installer that did not finish within its budget", "stop")]
    budgets = {"installer": {"attempts": 180, "delaySeconds": 20}}

    def message(shape):
        request = dict(scope(shape)["bootwright_os_install_request"], budgets=budgets)
        return render(stopped["ansible.builtin.assert"]["fail_msg"], shape, bootwright_os_install_request=request,
                      managedos_install_anaconda_installer={"power": "On"}).strip()

    plain = ("the installer did not power Machine rhel-01 off within its budget of 180 attempts 20 seconds apart; "
             "its management controller last answered power On")
    assert message("registered") == plain and message("unregistered") == plain
    sourced = message("subscription-source")
    assert sourced.startswith(plain + "; an installer that installs from its subscription also stops")
    assert sourced.endswith("leaves until it is removed there")
    for cause in ("refuses the organization or activation key", "a certificate its CA bundle does not verify",
                  "cannot be resolved or reached from the install network", "no BaseOS and AppStream repositories for the release",
                  "already holds a host with the hardware identity of this Machine"):
        assert cause in sourced
