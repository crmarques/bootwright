"""An installation uses exactly the media its plan froze, and a tree it can name.

The plan freezes each store entry's size and SHA-256 from its record. An apply
reads both again with the stat module, which reports size and, given
get_checksum and checksum_algorithm sha256, the file's SHA-256 as checksum
(ansible/modules/stat.py, main, ansible-core 2.21), and refuses a boot image
that no longer has them before the image build first reads it.

The published package tree carries the digest of the DVD it was extracted
from, written inside the staged tree before the rename publishes it, so an
apply over a tree another DVD published extracts the frozen one again.

Rendering the role's files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib
import re
import pytest

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template


ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
LOADER = DataLoader()
PROTOCOL = "bootwright.core.managedos_install_protocol"
STAT = "ansible.builtin.stat"
COMMAND = "ansible.builtin.command"
COPY = "ansible.builtin.copy"
FILE = "ansible.builtin.file"
BOOT = {"name": "rhel-9.8-x86_64-boot.iso", "sha256": "0123456789abcdef" * 4, "size": 1105199104}
DVD = {"name": "rhel-9.8-x86_64-dvd.iso", "sha256": "fedcba9876543210" * 4, "size": 13107200000}


def applied():
    return flattened(ROLE / "tasks" / "apply.yml")


def policy_check():
    tasks = LOADER.load_from_file(str(ROLE / "tasks" / "apply.yml"), trusted_as_template=True)
    check = next(index for index, task in enumerate(tasks)
                 if any("bootwright.core.ssh_key_policy" in inner for inner in task.get("block", [])))
    return tasks, check


def checked_keys(channel, generated="ssh-ed25519"):
    """The keys the placement check proves for one identity channel."""
    tasks, check = policy_check()
    templar = Templar(loader=LOADER, variables={
        "bootwright_os_install_request": {"fleetKeyRef": "fleet", "identity": {"object": "rhel-01"}, "target": {
            "channel": channel, "hostKeyRef": "host",
        }},
        "bootwright_os_install_material": {"fleetKeyType": "ssh-rsa", "hostKeyType": "ssh-ed25519", "generatedHostKeyType": generated},
    })
    return templar.template(tasks[check]["block"][0]["bootwright.core.ssh_key_policy"]["keys"])


@pytest.mark.parametrize("channel", ["delivered-key", "guest-agent"])
def test_actual_bound_key_policy_is_checked_before_publication_or_boot(channel):
    tasks, check = policy_check()
    for task in tasks[:check]:
        assert "ansible.builtin.assert" in task or task.get(PROTOCOL, {}).get("phase") == "loaded"
    rescue = tasks[check]["rescue"]
    assert rescue[0][PROTOCOL] == {"phase": "refused", "reason": '{{ ansible_failed_result.reason | default("ssh-policy-unavailable") }}'}
    assert "ansible.builtin.fail" in rescue[1]
    expected = [{"secret": "fleet", "type": "ssh-rsa", "usage": "fleet"}]
    if channel == "delivered-key":
        expected.append({"secret": "host", "type": "ssh-ed25519", "usage": "host"})
    else:
        expected.append({"machine": "rhel-01", "type": "ssh-ed25519", "usage": "generated"})
    assert checked_keys(channel) == expected


# A guest-agent installation generates its own host key, so the placement host
# has to accept that type before anything is published or booted; a
# delivered-key installation's host key is the bound one checked above.
def test_guest_agent_policy_check_includes_the_generated_host_key():
    assert checked_keys("guest-agent", "ecdsa-sha2-nistp256") == [
        {"secret": "fleet", "type": "ssh-rsa", "usage": "fleet"},
        {"machine": "rhel-01", "type": "ecdsa-sha2-nistp256", "usage": "generated"},
    ]
    assert not [key for key in checked_keys("delivered-key", "ecdsa-sha2-nistp256") if key["usage"] == "generated"]


def index_of(tasks, predicate, description):
    found = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def argv0(task, executable):
    return ((task.get(COMMAND) or {}).get("argv") or [None])[0] == executable


def variables(**extra):
    values = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    values.update({
        "bootwright_os_install_material": {},
        "bootwright_os_install_request": {
            "bootMedia": dict(BOOT),
            "image": {"path": "/var/lib/bootwright-services/lab/public/os/rhel-01/install.iso"},
            "tree": {"path": "/var/lib/bootwright-services/lab/public/os/rhel-9-8/tree"},
            "treeMedia": dict(DVD),
        },
    })
    values.update(extra)
    return values


def stat_of(media, **changed):
    """The stat module's result for one required entry, as it registers it in
    its loop: the entry as item, and what it read."""
    result = {"exists": True, "isreg": True, "size": media["size"], "checksum": media["sha256"]}
    result.update(changed)
    return {"item": {"path": "/var/lib/bootwright/media/" + media["name"], "size": media["size"], "sha256": media["sha256"]},
            "stat": result}


def unchanged(boot):
    templar = Templar(loader=LOADER, variables=variables(managedos_install_anaconda_media_present={"results": [boot]}))
    return templar.evaluate_conditional(trust_as_template("managedos_install_anaconda_boot_media_unchanged | bool"))


# The tree image is read and proved only by the shared package-tree
# publication (test_managedos_package_tree.py); an installation reads only its
# boot image and proves the published tree names the frozen DVD.
def test_an_apply_proves_its_boot_images_size_and_digest_before_first_use():
    tasks = applied()
    read = index_of(tasks, lambda task: STAT in task and task.get("loop") == "{{ managedos_install_anaconda_required_media }}",
                    "read the required media")
    assert tasks[read][STAT]["get_checksum"] is True
    assert tasks[read][STAT]["checksum_algorithm"] == "sha256"
    named = [index for index, task in enumerate(tasks)
             if PROTOCOL in task and str(task[PROTOCOL].get("reason", "")).startswith("media-changed-")]
    assert [tasks[index][PROTOCOL]["reason"] for index in named] == ["media-changed-boot"]
    first_use = index_of(tasks, lambda task: argv0(task, "/usr/bin/mkksiso"), "build the image")
    assert read < min(named) and max(named) + 1 < first_use
    for index in named:
        assert tasks[index].get("no_log") is True
        assert "ansible.builtin.fail" in tasks[index + 1]
        assert tasks[index + 1]["when"] == tasks[index]["when"]

    templar = Templar(loader=LOADER, variables=variables())
    assert templar.template(trust_as_template("{{ managedos_install_anaconda_required_media }}")) == [
        {"path": "/var/lib/bootwright/media/" + BOOT["name"], "size": BOOT["size"], "sha256": BOOT["sha256"]},
    ]
    assert unchanged(stat_of(BOOT)) is True
    assert unchanged(stat_of(BOOT, checksum="a" * 64)) is False
    assert unchanged(stat_of(BOOT, size=BOOT["size"] - 1)) is False
    assert unchanged({"item": stat_of(BOOT)["item"], "stat": {"exists": False}}) is False
    assert unchanged(stat_of(BOOT, isreg=False, isdir=True)) is False

    proof = index_of(tasks, lambda task: task.get("name") == "Prove the shared package tree dependency before building or booting",
                     "prove the shared tree")
    assert "managedos_install_anaconda_before.observation.treeIdentity == bootwright_os_install_request.treeMedia.sha256" in (
        tasks[proof]["ansible.builtin.assert"]["that"])
    assert proof < first_use


ROLES = ROLE.parent
SSH_OPTIONS = (
    "GlobalKnownHostsFile=none", "KnownHostsCommand=none", "VerifyHostKeyDNS=no", "UpdateHostKeys=no",
    "CheckHostIP=no", "StrictHostKeyChecking=yes", "IdentitiesOnly=yes", "IdentityAgent=none",
    "PasswordAuthentication=no", "KbdInteractiveAuthentication=no", "GSSAPIAuthentication=no",
    "HostbasedAuthentication=no", "ForwardAgent=no", "ProxyCommand=none", "ProxyJump=none",
    "ControlMaster=no", "ControlPath=none", "BatchMode=yes", "ConnectTimeout=10",
    "ServerAliveInterval=15", "ServerAliveCountMax=3",
)
URI = "ansible.builtin.uri"
OUTCOME = ("{{ 'unchanged' if (managedos_install_anaconda_was_installed | bool)"
           " and not (managedos_install_anaconda_network_result.changed | default(true) | bool) else 'changed' }}")
MARKER = '{"request":"' + "1" * 64 + '"}'


def flattened(path):
    """A task file's tasks in the order a play reaches them, each block opened."""
    def opened(listed):
        for task in listed or []:
            if not isinstance(task, dict):
                continue
            yield task
            if "block" in task:
                for section in ("block", "rescue", "always"):
                    yield from opened(task.get(section))
    return list(opened(LOADER.load_from_file(str(path), trusted_as_template=True)))


# The answer of the first identity read is frozen by a set_fact, which
# ansible-core finalizes when it runs; the default it reads is re-evaluated at
# every use and, once the installer wrote the marker, would call an install
# this apply performed one it found.
def test_a_first_install_reports_changed_and_a_replay_unchanged():
    tasks = applied()
    frozen = index_of(tasks, lambda task: "managedos_install_anaconda_was_installed" in (task.get("ansible.builtin.set_fact") or {}),
                      "freeze the first identity answer")
    refused = index_of(tasks, lambda task: task.get("name") == "Refuse a machine that answers with another installation",
                       "refuse another installation")
    assert frozen == refused + 1
    fact = tasks[frozen]["ansible.builtin.set_fact"]["managedos_install_anaconda_was_installed"]
    assert fact == "{{ managedos_install_anaconda_installed | bool }}"
    completed = index_of(tasks, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "publish completion")
    assert tasks[completed][PROTOCOL]["outcome"].strip() == OUTCOME
    for name in ("Boot the machine from its own installer image", "Wait for the installer to write the disk and boot what it installed"):
        guarded = index_of(tasks, lambda task, name=name: task.get("name") == name, name)
        assert frozen < guarded
        assert tasks[guarded]["when"] == "not (managedos_install_anaconda_was_installed | bool)"

    def outcome(first_answer, network_changed=False):
        material = {"marker": MARKER}
        first = Templar(loader=LOADER, variables=variables(
            bootwright_os_install_material=material, managedos_install_anaconda_answered=first_answer,
            managedos_install_anaconda_marker=MARKER if first_answer else ""))
        was_installed = first.template(trust_as_template(fact))
        # By completion the machine answers with the marker either way.
        after = Templar(loader=LOADER, variables=variables(
            bootwright_os_install_material=material, managedos_install_anaconda_answered=True,
            managedos_install_anaconda_marker=MARKER, managedos_install_anaconda_was_installed=was_installed,
            managedos_install_anaconda_network_result={"changed": network_changed}))
        return after.template(trust_as_template(OUTCOME))

    assert outcome(False) == "changed"
    assert outcome(True) == "unchanged"
    # A replay that had to converge the network changed the Machine.
    assert outcome(True, network_changed=True) == "changed"
    assert outcome(False, network_changed=True) == "changed"

    for path in sorted((ROLE / "tasks").glob("*.yml")):
        for task in flattened(path):
            if task is tasks[frozen] or "managedos_install_anaconda_was_installed" in (task.get("ansible.builtin.set_fact") or {}):
                continue
            assert "managedos_install_anaconda_installed" not in repr(
                {key: value for key, value in task.items() if key not in ("block", "rescue", "always")}
            ), "%s: %s" % (path.name, task.get("name"))


# Every ssh client a role runs reads only a configuration written for that one
# connection, which keeps the host crypto policy and nothing ambient, and ends a
# connection whose peer stopped answering.
def test_every_ssh_argv_uses_a_generated_configuration_and_keepalives():
    found = 0
    for path in sorted(ROLES.glob("*/tasks/*.yml")):
        tasks = flattened(path)
        for index, task in enumerate(tasks):
            for module in ("ansible.builtin.shell", "ansible.builtin.command", "ansible.builtin.raw"):
                body = task.get(module)
                text = body if isinstance(body, str) else (body or {}).get("cmd", "") if isinstance(body, dict) else ""
                assert not re.search(r"(^|[\s/])ssh(\s|$)", str(text)), "%s: %s runs ssh in a string" % (path, task.get("name"))
            argv = [str(argument) for argument in ((task.get(COMMAND) or {}).get("argv") or [])]
            if not argv or argv[0] != "/usr/bin/ssh":
                assert all(not argument.endswith("/ssh") and argument != "ssh" for argument in argv[:1]), path
                continue
            found += 1
            where = "%s: %s" % (path.relative_to(ROLES), task.get("name"))
            assert argv[1] == "-F" and argv[2].endswith("/ssh_config"), where
            configuration = argv[2]
            work = configuration[:-len("/ssh_config")]
            identity = argv.index("-i")
            pairs = argv[3:identity]
            assert len(pairs) % 2 == 0 and all(flag == "-o" for flag in pairs[0::2]), where
            options = pairs[1::2]
            assert options[0] == "UserKnownHostsFile=%s/known_hosts" % work, where
            for required in SSH_OPTIONS:
                assert required in options, "%s lacks %s" % (where, required)
            # The pinned key is negotiated under its own type's algorithms, so
            # a FIPS controller proves an rsa or ecdsa key it pinned.
            pinned = [option for option in options if option.startswith("HostKeyAlgorithms=")]
            assert len(pinned) == 1 and "bootwright.core.host_key_algorithms" not in pinned[0], where
            fact = pinned[0][len("HostKeyAlgorithms={{ "):-len(" }}")]
            derivations = [earlier for earlier in tasks[:index]
                           if fact in (earlier.get("ansible.builtin.set_fact") or {})]
            assert derivations, "%s pins no derived algorithm list" % where
            assert "| bootwright.core.host_key_algorithms" in derivations[-1]["ansible.builtin.set_fact"][fact], where
            writes = [earlier for earlier in tasks[:index]
                      if (earlier.get(COPY) or {}).get("dest") == configuration]
            checks = [earlier for earlier in tasks[:index] if "bootwright.core.ssh_key_policy" in earlier]
            assert checks, where
            policy_name = checks[-1]["register"]
            assert writes and writes[-1][COPY]["content"] == "{{ %s.configuration }}" % policy_name, where
            assert policy_name + ".hostKeyAlgorithms" in derivations[-1]["ansible.builtin.set_fact"][fact], where
            assert (writes[-1][COPY]["owner"], writes[-1][COPY]["group"], writes[-1][COPY]["mode"]) == ("root", "root", "0600"), where
            removals = [later for later in tasks[index + 1:]
                        if (later.get(FILE) or {}).get("path") == configuration and later[FILE].get("state") == "absent"]
            assert removals, where
    assert found >= 2


def test_the_work_area_lives_under_a_root_only_parent():
    templar = Templar(loader=LOADER, variables=variables(bootwright_os_install_request={"identity": {"context": "lab", "object": "node"}}))
    parent = templar.template(trust_as_template("{{ managedos_install_anaconda_work_parent }}"))
    work = templar.template(trust_as_template("{{ managedos_install_anaconda_work }}"))
    assert work.startswith(parent + "/")
    for scratch in ("/var/tmp", "/tmp"):
        assert not parent.startswith(scratch + "/") and not work.startswith(scratch + "/")

    tasks = flattened(ROLE / "tasks" / "work.yml")
    read, proof, create = tasks
    assert read[STAT]["follow"] is False
    assert create[FILE]["mode"] == "0700"
    assert create[FILE]["owner"] == create[FILE]["group"] == "root"
    assert create["when"] == ["managedos_install_anaconda_work_create | bool",
                              "not (managedos_install_anaconda_ancestor.stat.exists | default(false))"]
    for operation in ("apply", "observe", "destroy"):
        listed = flattened(ROLE / "tasks" / (operation + ".yml"))
        ancestor = index_of(listed, lambda task: task.get("ansible.builtin.include_tasks") == "work.yml", "prove all ancestors")
        first_inspection = index_of(
            listed, lambda task: task.get("register") in ("managedos_install_anaconda_before", "managedos_install_anaconda_observed"),
            "read durable work")
        assert ancestor < first_inspection
        assert len(listed[ancestor]["loop"]) == 3
        assert listed[ancestor]["vars"]["managedos_install_anaconda_work_create"] is (operation == "apply")

    def proves(**stat):
        proved = dict({"exists": True, "isdir": True, "islnk": False, "uid": 0, "mode": "0700"}, **stat)
        checks = Templar(loader=LOADER, variables={read["register"]: {"stat": proved}})
        return all(checks.evaluate_conditional(trust_as_template(condition)) for condition in proof["ansible.builtin.assert"]["that"])
    assert proves()
    assert not proves(mode="0755")
    assert not proves(uid=1000)
    assert not proves(islnk=True)
    assert not proves(isdir=False)


def test_the_image_and_tree_are_fetched_through_the_listener_before_the_insert():
    tasks = applied()
    published = index_of(tasks, lambda task: task.get("name") == "Publish the installer image by atomic rename", "publish the image")
    boot = index_of(tasks, lambda task: task.get("name") == "Boot the machine from its own installer image", "boot block")
    # The private files are fetched by their own task, which
    # test_managedos_install_private.py holds to its rules.
    fetches = [index for index, task in enumerate(tasks) if URI in task and "private_url" not in task[URI]["url"]]
    urls = [tasks[index][URI]["url"] for index in fetches]
    assert urls == ["{{ bootwright_os_install_request.image.url }}", "{{ bootwright_os_install_request.tree.url ~ '/.treeinfo' }}"]
    for index, certificate, url in zip(fetches, ("imageCertificate", "treeCertificate"), ("image", "tree")):
        fetch = tasks[index][URI]
        assert published < index < boot
        assert fetch["method"] == "GET" and fetch["headers"] == {"Range": "bytes=0-0"}
        assert fetch["use_proxy"] is False and fetch["follow_redirects"] == "none" and fetch["validate_certs"] is True
        assert fetch["status_code"] == [200, 206] and fetch["timeout"] == 30
        assert tasks[index]["failed_when"] is False
        refusal = tasks[index + 1]
        assert "ansible.builtin.fail" in refusal and "no machine was given media" in refusal["ansible.builtin.fail"]["msg"]
        register = tasks[index]["register"]
        for scheme, expected in (("https", "/run/" + certificate), ("http", "OMIT")):
            request = {"image": {"url": scheme + "://192.0.2.1/os/rhel-01/install.iso"},
                       "tree": {"url": scheme + "://192.0.2.1/os/rhel-9-8/tree"}}
            templar = Templar(loader=LOADER, variables=variables(
                bootwright_os_install_request=request, omit="OMIT",
                bootwright_os_install_material={certificate: "/run/" + certificate}))
            assert templar.template(fetch["ca_path"]).strip() == expected, (url, scheme)
            failed = Templar(loader=LOADER, variables=variables(
                bootwright_os_install_request=request, **{register: {"status": -1, "msg": "Connection refused\nsecond"}}))
            assert all(failed.evaluate_conditional(trust_as_template(condition))
                       for condition in (refusal["when"] if isinstance(refusal["when"], list) else [refusal["when"]]))
            message = failed.template(refusal["ansible.builtin.fail"]["msg"])
            assert "-1" in message and "Connection refused" in message and "second" not in message
            served = Templar(loader=LOADER, variables=variables(
                bootwright_os_install_request=request, **{register: {"status": 206}}))
            assert not all(served.evaluate_conditional(trust_as_template(condition))
                           for condition in (refusal["when"] if isinstance(refusal["when"], list) else [refusal["when"]]))
