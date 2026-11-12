"""Lifecycle roles use invocation scratch and remove only their own paths."""

import pathlib

import pytest
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
LOADER = DataLoader()
SCRATCH = "bootwright.core.lifecycle_scratch"


def load(role, name):
    return LOADER.load_from_file(str(ROLES / role / name), trusted_as_template=True)


def walk(tasks):
    for task in tasks:
        if "block" in task:
            yield from walk(task["block"])
            yield from walk(task.get("always", []))
        else:
            yield task


@pytest.mark.parametrize("role,scope", [
    ("substrate_libvirt_host", "libvirt-host"),
    ("substrate_libvirt_machine", "libvirt-machine"),
    ("managedos_install_anaconda", "managedos-install"),
    ("managedos_host_firewall", "managedos-firewall"),
    ("infrastructureservices_mirror_release", "mirror-release"),
])
def test_private_role_files_are_cleaned_even_after_a_task_fails(role, scope):
    tasks = load(role, "tasks/installed_apply.yml" if role == "managedos_host_firewall" else "tasks/apply.yml")
    blocks = [task for task in tasks if "block" in task
              and any(SCRATCH in child for child in task["block"])]
    assert len(blocks) == 1
    prepare = next(child for child in blocks[0]["block"] if SCRATCH in child)
    cleanup = next(child for child in blocks[0]["always"] if SCRATCH in child)
    assert prepare[SCRATCH] == {"root": "{{ bootwright_run_scratch }}", "scope": scope}
    request = {"substrate_libvirt_host": "bootwright_substrate_host_request",
               "substrate_libvirt_machine": "bootwright_substrate_machine_request",
               "managedos_install_anaconda": "bootwright_os_install_request",
               "managedos_host_firewall": "bootwright_os_firewall_request",
               "infrastructureservices_mirror_release": "bootwright_mirror_release_request"}[role]
    assert cleanup[SCRATCH] == dict(
        prepare[SCRATCH], state="absent", remove_root="{{ " + request + ".placement.connection == 'ssh' }}")
    assert cleanup["when"] == prepare["register"] + " is succeeded"


def test_observation_never_creates_or_removes_durable_install_work():
    tasks = list(walk(load("managedos_install_anaconda", "tasks/observe.yml")))
    assert not any("ansible.builtin.file" in task for task in tasks)
    assert any(SCRATCH in task for task in tasks)
    defaults = load("managedos_install_anaconda", "defaults/main.yml")
    paths = []
    for context, machine, job in [("lab", "first", "1"), ("lab", "second", "2"), ("other", "first", "3")]:
        templar = Templar(loader=LOADER, variables=dict(
            defaults, bootwright_run_scratch="/var/tmp/bootwright-scratch-%s-1" % job,
            bootwright_os_install_request={"identity": {"context": context, "object": machine}}))
        work = templar.template(trust_as_template("{{ managedos_install_anaconda_work }}"))
        scratch = templar.template(trust_as_template("{{ managedos_install_anaconda_scratch }}"))
        assert work == "/var/lib/bootwright-hosted/%s/machines/%s/install" % (context, machine)
        assert scratch == "/var/tmp/bootwright-scratch-%s-1/managedos-install" % job
        paths.append(work)
    assert len(set(paths)) == len(paths)


@pytest.mark.parametrize("role,variable,field,suffix,version", [
    ("substrate_libvirt_host", "bootwright_substrate_host", "poolPath", "providers/node/vmedia", "substrate-host-libvirt-v3"),
    ("substrate_libvirt_machine", "bootwright_substrate_machine", "directory", "machines/node", "machine-libvirt-v3"),
])
def test_destroy_accepts_only_the_exact_namespaced_identity(role, variable, field, suffix, version):
    task = load(role, "tasks/destroy.yml")[0]
    expected = "/var/lib/libvirt/images/bootwright/lab/" + suffix

    def accepts(path, context="lab", machine="node"):
        request = {"version": version, "identity": {"context": context, "object": machine}, field: path}
        templar = Templar(loader=LOADER, variables={variable + "_request": request, variable + "_digest": "a" * 64})
        return all(templar.evaluate_conditional(condition) for condition in task["ansible.builtin.assert"]["that"])

    assert accepts(expected)
    for path in ("/var/lib/libvirt/images/bootwright/lab", expected + "/..", expected.replace("lab/", "other/"), expected.replace("/node", "/other")):
        assert not accepts(path)
    assert not accepts(expected, context="../lab")
    assert not accepts(expected, machine="../node")


HOSTED = "bootwright.core.hosted_area"


@pytest.mark.parametrize("operation, state, path", [
    ("apply", "present", ["machines", "node"]),
    ("observe", "proved", ["machines", "node", "install"]),
    ("destroy", "proved", ["machines", "node", "install"]),
])
def test_every_entry_proves_the_work_areas_hosted_ancestors_before_touching_it(operation, state, path):
    tasks = list(walk(load("managedos_install_anaconda", "tasks/" + operation + ".yml")))
    proof = next(index for index, task in enumerate(tasks) if HOSTED in task)
    work = [index for index, task in enumerate(tasks)
            if "managedos_install_anaconda_work" in str(task.get("ansible.builtin.file", {}).get("path", ""))]
    assert all(index > proof for index in work)
    assert tasks[proof][HOSTED]["state"] == state
    defaults = load("managedos_install_anaconda", "defaults/main.yml")
    templar = Templar(loader=LOADER, variables=dict(
        defaults, bootwright_os_install_request={"identity": {"context": "lab", "object": "node"}}))
    assert templar.template(tasks[proof][HOSTED]["context"]) == "lab"
    assert templar.template(tasks[proof][HOSTED]["path"]) == path


def test_the_destroy_removes_the_empty_hosted_directories_once_the_work_area_is_gone():
    tasks = list(walk(load("managedos_install_anaconda", "tasks/destroy.yml")))
    work = next(index for index, task in enumerate(tasks) if task.get("name") == "Remove the work area an interrupted attempt left")
    removal = tasks[work + 1]
    assert removal[HOSTED]["state"] == "absent"
    assert removal[HOSTED]["path"] == "{{ managedos_install_anaconda_area }}"
    outcome = next(task for task in tasks if task.get("name") == "Publish bounded installation removal evidence")
    assert removal["register"] + " is changed" in str(outcome["bootwright.core.managedos_install_protocol"]["outcome"])
