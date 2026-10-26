"""The registration session is the role's guest connection, option for option.

guest.yml is the owner of the pinned guest connection; the registration
module's argv builder must open exactly the same one, so a weakened or
reordered option in either fails here.
"""

from __future__ import annotations

import pathlib

import yaml

from ansible_collections.bootwright.core.plugins.module_utils import managedos_guest_ssh

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
GUEST = COLLECTION / "roles" / "managedos_install_anaconda" / "tasks" / "guest.yml"
KNOWN_HOSTS = "{{ managedos_install_anaconda_scratch }}/known_hosts"
ALGORITHMS = "{{ managedos_install_anaconda_guest_host_key_algorithms }}"


def option_values(argv):
    return [argv[position + 1] for position, word in enumerate(argv) if word == "-o"]


def test_registration_argv_matches_guest_yml_option_set(tmp_path):
    task = next(task for task in yaml.safe_load(GUEST.read_text()) if task.get("register") == "managedos_install_anaconda_guest")
    paths = {}
    for name in ("ssh_config", "known_hosts", "identity"):
        path = tmp_path / name
        path.write_bytes(b"")
        paths[name] = str(path)
    argv = managedos_guest_ssh.guest_argv(
        ssh_config=paths["ssh_config"], known_hosts=paths["known_hosts"], host_key_algorithms="ssh-ed25519",
        policy_host_key_algorithms="ssh-ed25519,rsa-sha2-512", identity=paths["identity"], user="fleet",
        address="192.0.2.10", remote_argv=["true"])
    expected = [value.replace(KNOWN_HOSTS, paths["known_hosts"]).replace(ALGORITHMS, "ssh-ed25519")
                for value in option_values(task["ansible.builtin.command"]["argv"])]
    assert option_values(argv) == expected
    assert argv[0] == task["ansible.builtin.command"]["argv"][0] == managedos_guest_ssh.SSH
    assert argv[1:3] == ["-F", paths["ssh_config"]]
    assert argv[-4:-2] == ["-i", paths["identity"]]
    assert "-T" not in argv and "-p" not in argv and "--" not in argv
