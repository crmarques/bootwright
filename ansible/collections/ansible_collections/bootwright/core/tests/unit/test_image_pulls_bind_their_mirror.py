"""Every role acquires a container image through image_acquire alone, so each pull binds its mirror."""

import pathlib

import yaml

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"


def tasks(node):
    if isinstance(node, list):
        for item in node:
            yield from tasks(item)
    elif isinstance(node, dict):
        yield node
        for key in ("block", "rescue", "always"):
            yield from tasks(node.get(key))


def test_no_role_runs_a_bare_podman_pull():
    acquiring, bare = set(), []
    for path in sorted(ROLES.glob("*/tasks/*.yml")):
        listed = list(tasks(yaml.safe_load(path.read_text())))
        for index, task in enumerate(listed):
            if "bootwright.core.image_acquire" in task:
                acquiring.add(path.parts[-3])
                assert task.get("no_log") is True, "%s: %s lends material and must not log" % (path, task.get("name"))
                assert task.get("failed_when") is False, "%s: %s must leave its failure to the report after it" % (path, task.get("name"))
                report = listed[index + 1]
                assert "ansible.builtin.fail" in report and "no_log" not in report, "%s: a failed pull is reported uncensored" % path
                assert task["register"] + ".msg" in report["ansible.builtin.fail"]["msg"]
                assert report["when"] == task["register"] + ".image is not defined"
            command = task.get("ansible.builtin.command") or task.get("command") or {}
            argv = command.get("argv", []) if isinstance(command, dict) else str(command).split()
            if argv[:2] == ["/usr/bin/podman", "pull"] or argv[:2] == ["podman", "pull"]:
                bare.append("%s: %s" % (path, task.get("name")))
    assert bare == []
    assert acquiring == {"infra_artifact_server_nginx", "infra_managed_service", "substrate_libvirt_machine"}
