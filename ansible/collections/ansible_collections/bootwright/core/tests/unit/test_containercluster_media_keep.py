"""How the boot-media role hands over the kubeconfig its removal would delete.

The media block's removal deletes the installer's work area, where the
installation keeps its administrator kubeconfig. Before that removal, while
custody holds none, the engine runs this role's observation with one output
declared, and the role copies the kept kubeconfig there privately, whole and
within the inspection's bound, whatever the installation proved. Only the
observation offers it; the removal and the apply offer nothing.
"""

from __future__ import annotations

import pathlib

import yaml

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "containercluster_media_agent"
INSTALL = pathlib.Path(__file__).resolve().parents[2] / "roles" / "containercluster_install_agent"
OUTPUT = "bootwright_cluster_media_output"


def tasks(name):
    return yaml.safe_load((ROLE / "tasks" / name).read_text())


def defaults(role):
    return yaml.safe_load((role / "defaults" / "main.yml").read_text())


def position(entries, predicate):
    matches = [index for index, task in enumerate(entries) if predicate(task)]
    assert len(matches) == 1, matches
    return matches[0]


def test_the_media_role_reads_the_kubeconfig_the_installation_keeps():
    media, install = defaults(ROLE), defaults(INSTALL)
    assert media["containercluster_media_agent_kubeconfig"] == "{{ containercluster_media_agent_work }}/.bootwright-kubeconfig"
    assert install["containercluster_install_agent_kubeconfig"] == "{{ containercluster_install_agent_work }}/.bootwright-kubeconfig"
    assert media["containercluster_media_agent_work"] == "{{ bootwright_cluster_media_request.workRoot }}"
    assert install["containercluster_install_agent_work"] == "{{ bootwright_cluster_install_request.workRoot }}"
    assert media["containercluster_media_agent_kubeconfig_bound"] == 65536


def test_the_observation_hands_over_a_whole_kept_kubeconfig_privately_before_it_publishes():
    entries = tasks("observe.yml")
    read = position(entries, lambda task: "ansible.builtin.stat" in task)
    copy = position(entries, lambda task: OUTPUT in str((task.get("ansible.builtin.copy") or {}).get("dest", "")))
    publish = position(entries, lambda task: (task.get("bootwright.core.containercluster_media_protocol") or {}).get("phase") == "completed")
    assert read < copy < publish
    stat = entries[read]
    assert stat["ansible.builtin.stat"] == {
        "path": "{{ containercluster_media_agent_kubeconfig }}",
        "follow": False,
        "get_checksum": False,
    }
    assert stat["when"] == OUTPUT + " is defined"
    assert stat["no_log"] is True
    task = entries[copy]
    assert task["ansible.builtin.copy"] == {
        "src": "{{ containercluster_media_agent_kubeconfig }}",
        "dest": "{{ " + OUTPUT + ".kubeconfig }}",
        "remote_src": True,
        "owner": "root",
        "group": "root",
        "mode": "0600",
    }
    assert task["no_log"] is True
    assert task["when"] == [
        OUTPUT + " is defined",
        "containercluster_media_agent_kept.stat.isreg | default(false)",
        "containercluster_media_agent_kept.stat.size | default(0) > 0",
        "containercluster_media_agent_kept.stat.size | default(0) <= containercluster_media_agent_kubeconfig_bound",
    ]


def test_only_the_observation_offers_it():
    for name in ("apply.yml", "build.yml", "destroy.yml"):
        assert OUTPUT not in (ROLE / "tasks" / name).read_text(), name


def test_the_role_declares_the_output_as_an_optional_dict_for_every_entry_point():
    specs = yaml.safe_load((ROLE / "meta" / "argument_specs.yml").read_text())["argument_specs"]
    for entry in ("apply", "observe", "destroy"):
        assert specs[entry]["options"][OUTPUT] == {"type": "dict", "required": False}, entry
