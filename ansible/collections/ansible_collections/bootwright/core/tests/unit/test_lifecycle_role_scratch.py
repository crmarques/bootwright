"""Lifecycle roles use invocation scratch and remove only their own paths."""

import pathlib
import os
import stat

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
])
def test_private_role_files_are_cleaned_even_after_a_task_fails(role, scope):
    tasks = load(role, "tasks/apply.yml")
    blocks = [task for task in tasks if "block" in task
              and any(SCRATCH in child for child in task["block"])]
    assert len(blocks) == 1
    prepare = next(child for child in blocks[0]["block"] if SCRATCH in child)
    cleanup = next(child for child in blocks[0]["always"] if SCRATCH in child)
    assert prepare[SCRATCH] == {"root": "{{ bootwright_run_scratch }}", "scope": scope}
    request = {"substrate_libvirt_host": "bootwright_substrate_host_request",
               "substrate_libvirt_machine": "bootwright_substrate_machine_request",
               "managedos_install_anaconda": "bootwright_os_install_request"}[role]
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
            defaults, bootwright_run_scratch="/var/tmp/bootwright-run-scratch-%s-1" % job,
            bootwright_os_install_request={"identity": {"context": context, "object": machine}}))
        work = templar.template(trust_as_template("{{ managedos_install_anaconda_work }}"))
        scratch = templar.template(trust_as_template("{{ managedos_install_anaconda_scratch }}"))
        assert work == "/var/lib/bootwright-install/%s/%s/work" % (context, machine)
        assert scratch == "/var/tmp/bootwright-run-scratch-%s-1/managedos-install" % job
        paths.append(work)
    assert len(set(paths)) == len(paths)


@pytest.mark.parametrize("role,variable,field,suffix,version", [
    ("substrate_libvirt_host", "bootwright_substrate_host", "poolPath", "providers/node/vmedia", "substrate-host-libvirt-v6"),
    ("substrate_libvirt_machine", "bootwright_substrate_machine", "directory", "machines/node", "machine-libvirt-v8"),
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


@pytest.mark.parametrize("operation", ["apply", "observe", "destroy"])
@pytest.mark.parametrize("ancestor_index", [0, 1, 2])
@pytest.mark.parametrize("unsafe", ["link", "mode", "owner"])
def test_unsafe_work_ancestors_refuse_before_touching_a_foreign_work_area(tmp_path, operation, ancestor_index, unsafe):
    parent = tmp_path / "install"
    ancestors = [parent, parent / "lab", parent / "lab" / "node"]
    foreign = tmp_path / "foreign"
    foreign.mkdir(mode=0o700)
    (foreign / "work").mkdir()
    sentinel = foreign / "work" / "keep"
    sentinel.write_text("untouched")
    for directory in ancestors[:ancestor_index]:
        directory.mkdir(mode=0o700)
    bad = ancestors[ancestor_index]
    if unsafe == "link":
        bad.symlink_to(foreign, target_is_directory=True)
    else:
        bad.mkdir(mode=0o700)
        if unsafe == "mode":
            bad.chmod(0o755)
    entry = next(task for task in load("managedos_install_anaconda", "tasks/" + operation + ".yml")
                 if task.get("ansible.builtin.include_tasks") == "work.yml")
    helper = load("managedos_install_anaconda", "tasks/work.yml")
    seen = []
    with pytest.raises(AssertionError, match="unsafe work ancestor"):
        for directory in ancestors:
            info = os.lstat(directory)
            observed = {"exists": True, "isdir": stat.S_ISDIR(info.st_mode), "islnk": stat.S_ISLNK(info.st_mode),
                        "uid": 1000 if unsafe == "owner" and directory == bad else 0,
                        "mode": "%04o" % stat.S_IMODE(info.st_mode)}
            variables = dict(entry["vars"], managedos_install_anaconda_work_ancestor=str(directory),
                             managedos_install_anaconda_ancestor={"stat": observed})
            templar = Templar(loader=LOADER, variables=variables)
            seen.append(directory)
            assert all(templar.evaluate_conditional(condition) for condition in helper[1]["ansible.builtin.assert"]["that"]), "unsafe work ancestor"
    assert seen == ancestors[:ancestor_index + 1]
    assert sentinel.read_text() == "untouched"
    assert bad.is_symlink() if unsafe == "link" else bad.stat().st_mode & 0o777 == (0o755 if unsafe == "mode" else 0o700)


def test_observation_leaves_missing_work_ancestors_absent(tmp_path):
    entry = next(task for task in load("managedos_install_anaconda", "tasks/observe.yml")
                 if task.get("ansible.builtin.include_tasks") == "work.yml")
    helper = load("managedos_install_anaconda", "tasks/work.yml")
    ancestors = [tmp_path / "install", tmp_path / "install/lab", tmp_path / "install/lab/node"]
    for directory in ancestors:
        assert not directory.exists()
        variables = dict(entry["vars"], managedos_install_anaconda_work_ancestor=str(directory),
                         managedos_install_anaconda_ancestor={"stat": {"exists": False}})
        templar = Templar(loader=LOADER, variables=variables)
        assert all(templar.evaluate_conditional(condition) for condition in helper[1]["ansible.builtin.assert"]["that"])
        assert not all(templar.evaluate_conditional(condition) for condition in helper[2]["when"])
    assert list(tmp_path.iterdir()) == []


@pytest.mark.parametrize("answer", [{}, {"exists": None}, {"exists": "false"}, {"exists": True}])
def test_unknown_work_ancestry_is_not_absence(answer):
    helper = load("managedos_install_anaconda", "tasks/work.yml")
    templar = Templar(loader=LOADER, variables={"managedos_install_anaconda_ancestor": {"stat": answer}})
    assert not all(templar.evaluate_conditional(condition) for condition in helper[1]["ansible.builtin.assert"]["that"])
