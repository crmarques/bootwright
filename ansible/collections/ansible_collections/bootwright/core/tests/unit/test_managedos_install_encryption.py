"""An encrypting installation publishes its recovery passphrase only inside the
boot and proves its root disk encrypted after its network converged.

The installer's %pre and %post fetch the recovery passphrase from the private
URL its Kickstart names, so recovery is published beneath the unguessable
directory of the attempt, beside the installer image, only by the boot an
apply performs: after the substrate proved the target, before the media is
inserted, with its first byte fetched through the listener, and never by a
replay. The private subtree is withdrawn before the completion is inspected.

The proof reads, over the pinned fleet connection guest.yml owns, that the root
filesystem's device chain (lsblk --inverse, util-linux lsblk(8)) holds a crypt
device whose crypto_LUKS backing device carries a clevis tpm2 slot, which
`clevis luks list -d DEV` prints as `<slot>: tpm2 '<config>'`
(https://github.com/latchset/clevis/blob/master/src/luks/clevis-luks-list.1.adoc).

Rendering the role's files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib
import subprocess

import pytest

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template


ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
LOADER = DataLoader()
URI = "ansible.builtin.uri"
COPY = "ansible.builtin.copy"
FILE = "ansible.builtin.file"
FAIL = "ansible.builtin.fail"
INCLUDE = "ansible.builtin.include_tasks"
INSPECT = "bootwright.core.managedos_install_inspect"
PROTOCOL = "bootwright.core.managedos_install_protocol"
TOKEN = "0123456789abcdef" * 4
SERVED = "/var/lib/bootwright-services/lab/artifact-server/lab-artifacts/public"
PRIVATE = {"path": SERVED + "/private/os/rhel-01", "url": "https://192.0.2.1:8443/private/os/rhel-01"}
PUBLIC = {"path": SERVED + "/os/rhel-01/install.iso", "url": "https://192.0.2.1:8443/os/rhel-01/install.iso"}
REQUESTED = "managedos_install_anaconda_root_encryption_requested | bool"
UNPROVED = "not (managedos_install_anaconda_root_encryption | bool)"
REACHABLE = "managedos_install_anaconda_reachable.rc | default(1) == 0"
NOT_INSTALLED = "not (managedos_install_anaconda_was_installed | bool)"
READ = (
    "set -u\n"
    "root=$(findmnt -nvo SOURCE / 2>/dev/null | head -n 1)\n"
    "chain=$(lsblk -rpnso NAME,TYPE,FSTYPE -- \"$root\" 2>/dev/null)\n"
    "luks=$(printf '%s\\n' \"$chain\" | awk '$3 == \"crypto_LUKS\" && !found {print $1; found=1}')\n"
    "if printf '%s\\n' \"$chain\" | awk '$2 == \"crypt\" {found=1} END {exit !found}'; then echo crypt=yes; else echo crypt=no; fi\n"
    "if [ -n \"$luks\" ] && clevis luks list -d \"$luks\" 2>/dev/null | awk -F': ' '$2 ~ /^tpm2 / {found=1} END {exit !found}'\n"
    "then echo tpm2=yes; else echo tpm2=no; fi"
)
COMMAND = ["/usr/bin/sudo", "-n", "/usr/bin/sh", "-c", READ, "bootwright-root-encryption"]
SHAPES = {
    "encrypting": {"private": dict(PRIVATE), "diskEncryption": {"recoveryPassphraseRef": "rhel-luks-recovery"},
                   "target": {"channel": "guest-agent", "physical": False}},
    "delivered-key": {"private": dict(PRIVATE), "target": {"channel": "delivered-key", "physical": True}},
    "public": {"image": dict(PUBLIC), "target": {"channel": "guest-agent", "physical": False}},
}
ADDRESS = PRIVATE["url"] + "/" + TOKEN + "/recovery"
OUTCOMES = [
    ({"status": 403, "msg": "Status code was 403 and not [200, 206]: HTTP Error 403: Forbidden"},
     "the listener answered 403, so the worker it serves as cannot read it"),
    ({"status": 404, "msg": "Status code was 404 and not [200, 206]: HTTP Error 404: Not Found"},
     "the listener answered 404, so the worker it serves as finds nothing at the path this attempt published"),
    ({"status": -1, "msg": "Request failed: <urlopen error [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed> " + ADDRESS},
     "its certificate did not verify against the serving certificate Secret lab-artifacts-tls holds: Request failed: "
     "<urlopen error [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed> " + ADDRESS.replace(TOKEN, "<token>")),
    ({"status": -1, "msg": "Request failed: <urlopen error [Errno 111] Connection refused> " + ADDRESS},
     "the request failed before any status line: Request failed: <urlopen error [Errno 111] Connection refused> "
     + ADDRESS.replace(TOKEN, "<token>")),
    ({"status": 500, "msg": "Status code was 500 and not [200, 206]: HTTP Error 500"},
     "the listener answered 500 where 200 or 206 was expected"),
]


def flattened(name):
    """A task file's tasks in the order a play reaches them, each block opened
    and its condition carried onto every task inside it."""
    def opened(listed, inherited):
        for task in listed or []:
            if not isinstance(task, dict):
                continue
            conditions = task.get("when", [])
            conditions = inherited + (conditions if isinstance(conditions, list) else [conditions])
            if "block" in task:
                for section in ("block", "rescue", "always"):
                    yield from opened(task.get(section), conditions)
                continue
            yield dict(task, when=conditions)
    return list(opened(LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True), []))


def only(tasks, predicate, description):
    found = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def named(name):
    return lambda task: task.get("name") == name


def included(name):
    return lambda task: task.get(INCLUDE) == name


def scope(shape="encrypting", **extra):
    """The variables an apply sees: the role defaults, the frozen request of
    one shape, the material files the runner wrote and the minted token."""
    values = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    request = {"identity": {"context": "lab", "object": "rhel-01"}, "tlsCertificateRef": "lab-artifacts-tls"}
    request.update({key: (dict(value) if isinstance(value, dict) else value) for key, value in SHAPES[shape].items()})
    values.update({
        "bootwright_run_scratch": "/var/tmp/bootwright-run-scratch-1-1",
        "bootwright_os_install_request": request,
        "bootwright_os_install_material": {"artifactCertificate": "/run/material/artifact-ca",
                                           "recoveryPassphrase": "/run/material/recovery"},
        "managedos_install_anaconda_token": {"stdout": TOKEN + "\n"},
    })
    values.update(extra)
    return values


def render(value, shape="encrypting", **variables):
    return Templar(loader=LOADER, variables=scope(shape, **variables)).template(value)


def holds(conditions, shape="encrypting", **variables):
    templar = Templar(loader=LOADER, variables=scope(shape, **variables))
    listed = conditions if isinstance(conditions, list) else [conditions]
    return all(templar.evaluate_conditional(trust_as_template(condition)) for condition in listed)


def task_files():
    return sorted(path.name for path in (ROLE / "tasks").glob("*.yml"))


def test_private_subtree_publishes_recovery_only_for_encrypting_installations():
    boot = flattened("boot.yml")
    recovery = boot[only(boot, included("private_recovery.yml"), "publish the recovery passphrase")]
    assert holds(recovery["when"]) and not holds(recovery["when"], "delivered-key") and not holds(recovery["when"], "public")
    private = flattened("private.yml")
    pair = private[only(private, named("Publish the host key pair this installation delivers"), "publish the key pair")]
    assert holds(pair["when"], "delivered-key") and not holds(pair["when"])
    files = Templar(loader=LOADER, variables=scope()).template(trust_as_template("{{ managedos_install_anaconda_private_files }}"))
    assert files == ["install.iso"]
    files = Templar(loader=LOADER, variables=scope("delivered-key")).template(trust_as_template("{{ managedos_install_anaconda_private_files }}"))
    assert files == ["install.iso", "identity", "identity.pub"]


def test_replay_does_not_republish_recovery():
    for name in ("apply.yml", "private.yml", "observe.yml"):
        for task in flattened(name):
            text = repr(dict(task, name=""))
            assert "recoveryPassphrase" not in text and "/recovery" not in text, (name, task.get("name"))
    includes = [(name, task) for name in task_files() for task in flattened(name) if task.get(INCLUDE) == "private_recovery.yml"]
    assert [name for name, _task in includes] == ["boot.yml"]
    boots = [(name, task) for name in task_files() for task in flattened(name) if task.get(INCLUDE) == "boot.yml"]
    assert [name for name, _task in boots] == ["apply.yml"]
    applied = flattened("apply.yml")
    frozen = only(applied, lambda task: "managedos_install_anaconda_was_installed" in (task.get("ansible.builtin.set_fact") or {}),
                  "freeze the first identity read")
    booted = only(applied, included("boot.yml"), "boot the installer")
    assert frozen < booted and NOT_INSTALLED in applied[booted]["when"]
    assert holds(applied[booted]["when"], managedos_install_anaconda_was_installed=False)
    assert not holds(applied[booted]["when"], managedos_install_anaconda_was_installed=True)


def test_recovery_is_published_inside_the_boot_block_before_insert():
    boot = flattened("boot.yml")
    refused = only(boot, named("Refuse a substrate this installation has no pre-boot binding for"), "pre-boot refusal")
    proved = [index for index, task in enumerate(boot) if (task.get("ansible.builtin.include_role") or {}).get("tasks_from") == "pre_boot"]
    published = only(boot, included("private_recovery.yml"), "publish the recovery passphrase")
    inserted = only(boot, lambda task: (task.get("bootwright.core.redfish_boot") or {}).get("operation") == "insert", "insert")
    assert len(proved) == 2 and max(proved) < refused < published < inserted
    assert published == refused + 1


def published_leaves(shape):
    """The files an apply of one shape publishes beneath the private directory."""
    leaves = ["install.iso"]
    for task in flattened("private.yml"):
        if COPY in task and holds(task["when"], shape):
            leaves += [item["name"] for item in task["loop"]]
    boot = flattened("boot.yml")
    if holds(boot[only(boot, included("private_recovery.yml"), "publish recovery")]["when"], shape):
        for task in flattened("private_recovery.yml"):
            if COPY in task:
                leaves.append(render(task[COPY]["dest"], shape).rsplit("/", 1)[1])
    return leaves


def fetched_leaves(shape):
    """The files an apply of one shape fetches through the listener before the insert."""
    applied = flattened("apply.yml")
    task = applied[only(applied, lambda task: URI in task and "private_url" in task[URI]["url"], "fetch the private files")]
    leaves = list(render(task["loop"], shape)) if holds(task["when"], shape) else []
    boot = flattened("boot.yml")
    if holds(boot[only(boot, included("private_recovery.yml"), "publish recovery")]["when"], shape):
        for task in flattened("private_recovery.yml"):
            if URI in task:
                leaves.append(render(task[URI]["url"], shape).rsplit("/", 1)[1])
    return leaves


@pytest.mark.parametrize("shape", ["encrypting", "delivered-key"])
def test_first_byte_fetch_covers_every_private_file(shape):
    assert sorted(fetched_leaves(shape)) == sorted(published_leaves(shape))
    if shape == "encrypting":
        assert "recovery" in fetched_leaves(shape)
    recovery = flattened("private_recovery.yml")
    fetch = recovery[only(recovery, lambda task: URI in task, "fetch the recovery passphrase")]
    applied = flattened("apply.yml")
    listener = applied[only(applied, lambda task: URI in task and "private_url" in task[URI]["url"], "fetch the private files")][URI]
    assert {key: value for key, value in fetch[URI].items() if key != "url"} == {
        key: value for key, value in listener.items() if key != "url"}
    assert render(fetch[URI]["url"]) == ADDRESS
    assert render(fetch[URI]["ca_path"]) == "/run/material/artifact-ca"
    assert fetch[URI]["validate_certs"] is True and fetch[URI]["use_proxy"] is False and fetch[URI]["follow_redirects"] == "none"
    assert fetch[URI]["headers"] == {"Range": "bytes=0-0"} and fetch[URI]["status_code"] == [200, 206] and fetch[URI]["timeout"] == 30
    assert fetch["failed_when"] is False and fetch["changed_when"] is False
    guard = recovery[only(recovery, named("Refuse a recovery passphrase the listener does not serve"), "refuse recovery")]
    register = fetch["register"]
    assert not holds(guard["when"], **{register: {"status": 206}}) and not holds(guard["when"], **{register: {"status": 200}})
    for result, diagnosis in OUTCOMES:
        variables = dict(guard["vars"], **{register: result})
        assert holds(guard["when"], **variables)
        message = render(guard[FAIL]["msg"], **variables)
        assert TOKEN not in message, message
        assert message.startswith("the private recovery could not be") and message.endswith(": " + diagnosis), message


def test_private_subtree_is_withdrawn_before_completion_inspection():
    applied = flattened("apply.yml")
    work = only(applied, named("Remove the work directory and the rendered kickstart with it"), "remove the work area")
    withdrawn = only(applied, lambda task: (task.get(FILE) or {}).get("path") == "{{ managedos_install_anaconda_private_root }}"
                     and task.get("name", "").startswith("Withdraw"), "withdraw the private material")
    inspected = only(applied, lambda task: INSPECT in task and task.get("register") == "managedos_install_anaconda_after",
                     "inspect the completion")
    completed = only(applied, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "publish the completion")
    assert work < withdrawn < inspected < completed
    assert applied[withdrawn][FILE]["state"] == "absent"
    assert holds(applied[withdrawn]["when"]) and not holds(applied[withdrawn]["when"], "public")
    assert render(applied[withdrawn][FILE]["path"]) == PRIVATE["path"]


def test_recovery_material_tasks_are_no_log():
    guarded = 0
    for task in flattened("private_recovery.yml"):
        text = repr(task)
        if not any(name in text for name in ("recoveryPassphrase", "private_dir", "private_url")):
            continue
        guarded += 1
        assert task.get("no_log") is True, task.get("name")
    assert guarded == 3
    guard = flattened("private_recovery.yml")[-1]
    assert FAIL in guard and "no_log" not in guard


def test_root_encryption_read_runs_through_guest_yml_after_network():
    applied = flattened("apply.yml")
    network = only(applied, included("network.yml"), "converge the network")
    converged = only(applied, lambda task: (task.get(PROTOCOL) or {}).get("group") == "converge-network"
                     and task[PROTOCOL].get("status") == "ok", "report network convergence")
    read = only(applied, included("root_encryption.yml"), "read the root-disk encryption")
    security = only(applied, included("host_security.yml"), "read the FIPS state")
    assert network < converged < read < security
    named_refusal, refused = applied[read + 1], applied[read + 2]
    assert named_refusal[PROTOCOL] == {"phase": "refused", "reason": "root-encryption-unproved"}
    assert named_refusal["no_log"] is True
    assert FAIL in refused and "no_log" not in refused
    assert named_refusal["when"][-2:] == refused["when"][-2:] == [REQUESTED, UNPROVED]
    assert read + 3 == security
    message = render(refused[FAIL]["msg"])
    assert message == "Machine rhel-01 does not report its root filesystem on a TPM2-bound LUKS device after its installation"

    observed = flattened("observe.yml")
    fips = only(observed, included("host_security.yml"), "read the FIPS state")
    read = only(observed, included("root_encryption.yml"), "read the root-disk encryption")
    assert read == fips + 1
    assert observed[read]["when"] == observed[fips]["when"] and observed[read]["when"][-1] == REACHABLE

    encryption = flattened("root_encryption.yml")
    guest = only(encryption, included("guest.yml"), "read through the pinned connection")
    assert encryption[guest]["vars"] == {"managedos_install_anaconda_guest_command": COMMAND}
    assert encryption[guest]["when"] == [REQUESTED]
    assert not any("ansible.builtin.command" in task or task.get("become") for task in encryption)


def proves(**variables):
    """Whether the last set_fact of root_encryption.yml names the encryption proved."""
    proved = flattened("root_encryption.yml")[-1]["ansible.builtin.set_fact"]["managedos_install_anaconda_root_encryption"]
    values = {"managedos_install_anaconda_root_encryption_requested": True}
    values.update(variables)
    return Templar(loader=LOADER, variables=values).template(proved)


@pytest.mark.parametrize("guest, requested, expected", [
    ({"rc": 0, "stdout": "crypt=yes\ntpm2=yes\n"}, True, True),
    ({"rc": 0, "stdout": "crypt=no\ntpm2=yes\n"}, True, False),
    ({"rc": 0, "stdout": "crypt=yes\ntpm2=no\n"}, True, False),
    ({"rc": 0, "stdout": "crypt=no\ntpm2=no\n"}, True, False),
    ({"rc": 0, "stdout": ""}, True, False),
    ({"rc": 1, "stdout": "crypt=yes\ntpm2=yes\n"}, True, False),
    (None, True, False),
    ({"rc": 0, "stdout": "crypt=yes\ntpm2=yes\n"}, False, False),
], ids=["encrypted", "crypt-no", "tpm2-no", "neither", "empty", "rc-1", "unread", "not-requested"])
def test_root_encryption_reader_accepts_only_crypt_and_tpm2(guest, requested, expected):
    variables = {"managedos_install_anaconda_root_encryption_requested": requested}
    if guest is not None:
        variables["managedos_install_anaconda_guest"] = guest
    assert proves(**variables) is expected


def test_the_root_encryption_intent_is_the_frozen_request():
    requested = flattened("root_encryption.yml")[0]["ansible.builtin.set_fact"]["managedos_install_anaconda_root_encryption_requested"]
    for shape, expected in (("encrypting", True), ("delivered-key", False), ("public", False)):
        assert render(requested, shape) is expected


# An observation runs this read too, so it writes nothing and changes no key
# slot; root only lets it read the LUKS header and the device chain.
def test_the_root_encryption_read_only_reads():
    encryption = flattened("root_encryption.yml")
    script = encryption[only(encryption, included("guest.yml"), "read")]["vars"]["managedos_install_anaconda_guest_command"][4]
    assert subprocess.run(["/bin/sh", "-n", "-c", script], check=False).returncode == 0
    assert script.replace("2>/dev/null", "").count(">") == 0
    for word in ("bind", "unbind", "regen", "luksAddKey", "luksKillSlot", "dracut", "tee", "rm ", "mv ", "cp "):
        assert word not in script, word


@pytest.mark.parametrize("name", ["apply.yml", "observe.yml"])
def test_completion_evidence_carries_the_proved_root_encryption(name):
    tasks = flattened(name)
    arguments = tasks[only(tasks, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "publish completion")][PROTOCOL]
    assert arguments["rootEncryption"] == "{{ managedos_install_anaconda_root_encryption | default(false) }}"
    assert render(arguments["rootEncryptionRequired"]) is True
    assert render(arguments["rootEncryptionRequired"], "delivered-key") is False
    assert render(arguments["rootEncryptionRequired"], "public") is False
    templar = Templar(loader=LOADER, variables={})
    assert templar.template(arguments["rootEncryption"]) is False
    for proved in (True, False):
        assert render(arguments["rootEncryption"], managedos_install_anaconda_root_encryption=proved) is proved


def test_a_virtual_private_insert_skips_the_verified_fetch_read_back():
    boot = flattened("boot.yml")
    insert = boot[only(boot, named("Insert the published installer image as virtual media"), "insert")]["bootwright.core.redfish_boot"]
    assert render(insert["private_delivery"], "delivered-key") is True
    assert render(insert["private_delivery"]) is False
    assert render(insert["private_delivery"], "public") is False
