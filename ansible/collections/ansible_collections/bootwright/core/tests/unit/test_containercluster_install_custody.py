"""How the install role offers the administrator access for custody.

The runner reads back one file the apply or the apply's observation leaves, and
the engine keeps it in the context's custody before it records the block done.
The role copies the kubeconfig it keeps of the installer's, the one every read
went through, there only once it read the installation complete with this
build's own identity, and the copy never reaches a log. A removal offers
nothing.
"""

from __future__ import annotations

import pathlib

import yaml

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "containercluster_install_agent"
OUTPUT = "bootwright_cluster_install_output"
GATE = [
    OUTPUT + " is defined",
    "(containercluster_install_agent_before.observation.identity | default('')) | length > 0",
    "containercluster_install_agent_state.cluster == containercluster_install_agent_before.observation.identity",
    "containercluster_install_agent_state.completed is sameas true",
]


def tasks(name):
    return yaml.safe_load((ROLE / "tasks" / name).read_text())


def position(entries, predicate):
    matches = [index for index, task in enumerate(entries) if predicate(task)]
    assert len(matches) == 1, matches
    return matches[0]


def custody_copy(entries):
    return position(entries, lambda task: OUTPUT in yaml.safe_dump(task))


def test_apply_and_observe_copy_the_kubeconfig_privately_once_completion_is_proved():
    for name in ("apply.yml", "observe.yml"):
        entries = tasks(name)
        index = custody_copy(entries)
        task = entries[index]
        arguments = task["ansible.builtin.copy"]
        assert arguments == {
            "src": "{{ containercluster_install_agent_kubeconfig }}",
            "dest": "{{ " + OUTPUT + ".kubeconfig }}",
            "remote_src": True,
            "owner": "root",
            "group": "root",
            "mode": "0600",
        }, name
        assert task["no_log"] is True, name
        assert task["when"] == GATE, name
        last_read = max(i for i, entry in enumerate(entries) if entry.get("ansible.builtin.import_tasks") == "state.yml")
        publish = position(entries, lambda entry: (entry.get("bootwright.core.containercluster_install_protocol") or {}).get("phase") == "completed")
        assert last_read < index < publish, name


def test_the_removal_offers_nothing():
    assert OUTPUT not in (ROLE / "tasks" / "destroy.yml").read_text()


def test_the_role_declares_the_output_as_an_optional_dict_for_every_entry_point():
    specs = yaml.safe_load((ROLE / "meta" / "argument_specs.yml").read_text())["argument_specs"]
    for entry in ("apply", "observe", "destroy"):
        assert specs[entry]["options"][OUTPUT] == {"type": "dict", "required": False}, entry
