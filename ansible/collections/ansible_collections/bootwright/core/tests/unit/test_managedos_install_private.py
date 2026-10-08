"""A delivered-key installation publishes its installer image only privately.

Its Kickstart names the tokenized URL of the key pair it delivers, so the image
is published beneath the unguessable directory the attempt minted, beside the
key pair, and never beneath the public subtree (D103). Every task that names
the token, the private URL or the image URL is hidden and writes only beneath
the work area or the private subtree; the three private files are fetched
through the listener before any media is inserted; the private material is
withdrawn before the completion is inspected; and a media URL a controller
reports is redacted before it reaches evidence, because a controller can echo
it with its host or port rewritten (F-007).

Rendering the role's files needs Ansible's controller (DataLoader and Templar),
which ansible-test does not offer to unit tests under tests/unit/plugins.
"""

from __future__ import annotations

import pathlib

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

from ansible_collections.bootwright.core.plugins.action import managedos_install_protocol as protocol
from ansible_collections.bootwright.core.plugins.modules.managedos_install_inspect import observe

ROLE = pathlib.Path(__file__).resolve().parents[2] / "roles" / "managedos_install_anaconda"
LOADER = DataLoader()
URI = "ansible.builtin.uri"
COMMAND = "ansible.builtin.command"
FILE = "ansible.builtin.file"
COPY = "ansible.builtin.copy"
FAIL = "ansible.builtin.fail"
INSPECT = "bootwright.core.managedos_install_inspect"
PROTOCOL = "bootwright.core.managedos_install_protocol"
# What `openssl rand -hex 32` mints: 64 hexadecimal digits.
TOKEN = "0123456789abcdef" * 4
DIGEST = "fedcba9876543210" * 4
# PrivatePath's layout (internal/infrastructureservices/artifactserver/publication.go).
SERVED = "/var/lib/bootwright-services/lab/artifact-server/lab-artifacts/public"
PRIVATE = {"path": SERVED + "/private/os/metal-01", "url": "https://192.0.2.1:8443/private/os/metal-01"}
PUBLIC = {"path": SERVED + "/os/rhel-01/install.iso", "url": "https://192.0.2.1:8443/os/rhel-01/install.iso"}
# The names whose value carries the token while an installation delivers a key.
SECRET_NAMES = ("managedos_install_anaconda_private_url", "managedos_install_anaconda_private_dir",
                "managedos_install_anaconda_token", "managedos_install_anaconda_image_url",
                "managedos_install_anaconda_image_path")
# The one way a refusal may name the token: to replace it.
REDACTION = "replace(managedos_install_anaconda_token.stdout | trim, '<token>')"


def flattened(name):
    """A task file's tasks in the order a play reaches them, each block opened."""
    def opened(listed):
        for task in listed or []:
            if not isinstance(task, dict):
                continue
            if "block" in task:
                for section in ("block", "rescue", "always"):
                    yield from opened(task.get(section))
                continue
            yield task
    return list(opened(LOADER.load_from_file(str(ROLE / "tasks" / name), trusted_as_template=True)))


def index_of(tasks, predicate, description):
    found = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(found) == 1, "%d tasks %s" % (len(found), description)
    return found[0]


def named(name):
    return lambda task: task.get("name") == name


def scope(private=True, **extra):
    """The variables an apply sees: the role defaults, the frozen request of
    one arm, the material files the runner wrote and the minted token."""
    values = dict(LOADER.load_from_file(str(ROLE / "defaults" / "main.yml"), trusted_as_template=True))
    request = {"identity": {"object": "metal-01"}, "tlsCertificateRef": "lab-artifacts-tls"}
    request.update({"private": dict(PRIVATE)} if private else {"image": dict(PUBLIC)})
    values.update({
        "bootwright_os_install_request": request,
        "bootwright_os_install_material": {"artifactCertificate": "/run/material/artifact-ca"},
        "managedos_install_anaconda_token": {"stdout": TOKEN + "\n"},
    })
    values.update(extra)
    return values


def render(value, **variables):
    return Templar(loader=LOADER, variables=scope(**variables)).template(value)


def expand(expression, **variables):
    """A template this module writes, trusted as a role file's would be."""
    return render(trust_as_template(expression), **variables)


def holds(conditions, **variables):
    templar = Templar(loader=LOADER, variables=scope(**variables))
    listed = conditions if isinstance(conditions, list) else [conditions]
    return all(templar.evaluate_conditional(trust_as_template(condition)) for condition in listed)


def argv(task):
    return list((task.get(COMMAND) or {}).get("argv") or [])


def renamed(task):
    return argv(task)[:1] == ["/usr/bin/mv"] and str(argv(task)[-1]).endswith("image_path }}")


def test_a_private_installation_publishes_its_image_only_beneath_the_token():
    tasks = flattened("apply.yml")
    rename = tasks[index_of(tasks, renamed, "publish the image by rename")]
    destination = render(argv(rename)[-1])
    assert destination == PRIVATE["path"] + "/" + TOKEN + "/install.iso"
    assert render(argv(rename)[-1], private=False) == PUBLIC["path"]
    assert expand("{{ managedos_install_anaconda_image_url }}") == PRIVATE["url"] + "/" + TOKEN + "/install.iso"
    assert expand("{{ managedos_install_anaconda_image_url }}", private=False) == PUBLIC["url"]
    created = tasks[index_of(tasks, named("Create the published image directory"), "create the public directory")]
    assert not holds(created["when"]) and holds(created["when"], private=False)
    served = tasks[index_of(tasks, named("Keep the private installer image readable only by the serving process"), "mode")]
    assert render(served[FILE]["path"]) == destination and served[FILE]["mode"] == "0640" and served.get("no_log") is True
    for arm in (True, False):
        assert expand("{{ managedos_install_anaconda_served_root }}", private=arm) == SERVED
    boot = flattened("boot.yml")
    insert = boot[index_of(boot, lambda task: (task.get("bootwright.core.redfish_boot") or {}).get("operation") == "insert", "insert")]
    assert render(insert["bootwright.core.redfish_boot"]["image"]) == PRIVATE["url"] + "/" + TOKEN + "/install.iso"


def write_targets(task):
    """Where one task writes: a file or copy destination, or a rename's target."""
    targets = []
    for module in (FILE, COPY):
        if isinstance(task.get(module), dict):
            targets.append(task[module].get("path", task[module].get("dest")))
    if argv(task)[:1] == ["/usr/bin/mv"]:
        targets.append(argv(task)[-1])
    return [target for target in targets if target]


def test_every_task_naming_the_private_url_or_token_is_hidden_and_stays_private():
    work = expand("{{ managedos_install_anaconda_work }}")
    seen = []
    for name in ("apply.yml", "private.yml", "boot.yml"):
        for task in flattened(name):
            text = repr(task).replace(REDACTION, "")
            if not any(secret in text for secret in SECRET_NAMES):
                continue
            seen.append(task.get("name"))
            assert render(task.get("no_log", False)) in (True, "True", "true"), "%s: %r is not hidden" % (name, task.get("name"))
            for target in write_targets(task):
                written = render(target, item={"name": "identity", "source": "/run/material/host-key"})
                assert written.startswith(work + "/") or written == PRIVATE["path"] or written.startswith(PRIVATE["path"] + "/"), (
                    "%s: %r writes %s" % (name, task.get("name"), written))
    for expected in ("Publish the installer image by atomic rename", "Fetch the first byte of each private file through its listener",
                     "Insert the published installer image as virtual media", "Publish the host key pair this installation delivers"):
        assert expected in seen, "the walk no longer sees %r" % expected


def test_the_inspection_follows_the_withdrawal():
    tasks = flattened("apply.yml")
    work = index_of(tasks, named("Remove the work directory and the rendered kickstart with it"), "remove the work area")
    withdrawn = index_of(tasks, lambda task: (task.get(FILE) or {}).get("path") == "{{ managedos_install_anaconda_private_root }}"
                         and task.get("name", "").startswith("Withdraw"), "withdraw the private material")
    inspected = index_of(tasks, lambda task: INSPECT in task and task.get("register") == "managedos_install_anaconda_after",
                         "inspect the completion")
    completed = index_of(tasks, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "publish the completion")
    assert work < withdrawn < inspected < completed
    assert tasks[withdrawn][FILE]["state"] == "absent" and holds(tasks[withdrawn]["when"])


def test_an_attempt_clears_an_earlier_private_publication_before_minting():
    tasks = flattened("apply.yml")
    cleared = index_of(tasks, named("Clear whatever an earlier attempt published privately"), "clear the private subtree")
    minted = index_of(tasks, lambda task: argv(task)[:2] == ["/usr/bin/openssl", "rand"], "mint the token")
    published = index_of(tasks, lambda task: (task.get("ansible.builtin.include_tasks") == "private.yml"), "publish privately")
    assert cleared < minted < published
    clear = tasks[cleared]
    assert clear[FILE]["state"] == "absent" and render(clear[FILE]["path"]) == PRIVATE["path"]
    assert holds(clear["when"]) and not holds(clear["when"], private=False)


# What the fetch registers in each loop result, in the forms ansible-core
# 2.21.4 gives them (see test_served_content_is_readable.py): a refused status
# prefixed "Status code was %s and not [200, 206]: ", and fetch_url's
# "Request failed: %s" with status -1, here naming the tokenized URL.
ADDRESS = PRIVATE["url"] + "/" + TOKEN + "/identity"
FETCHED = {"status": 206}
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
]


def test_the_private_image_and_key_are_fetched_before_the_insert():
    tasks = flattened("apply.yml")
    fetched = index_of(tasks, lambda task: URI in task and "private_url" in task[URI]["url"], "fetch the private files")
    refused = index_of(tasks, named("Refuse a private file the listener does not serve"), "refuse a private file")
    after = max(index_of(tasks, renamed, "publish the image"),
                index_of(tasks, lambda task: task.get("ansible.builtin.include_tasks") == "private.yml", "publish the key"))
    boot = index_of(tasks, lambda task: task.get("ansible.builtin.include_tasks") == "boot.yml", "boot")
    assert after < fetched < refused < boot
    task = tasks[fetched]
    fetch = task[URI]
    assert task["loop"] == ["install.iso", "identity", "identity.pub"]
    assert [render(fetch["url"], item=item) for item in task["loop"]] == [
        PRIVATE["url"] + "/" + TOKEN + "/" + item for item in task["loop"]]
    assert render(fetch["ca_path"]) == "/run/material/artifact-ca"
    assert fetch["validate_certs"] is True and fetch["use_proxy"] is False and fetch["follow_redirects"] == "none"
    assert fetch["headers"] == {"Range": "bytes=0-0"} and fetch["status_code"] == [200, 206]
    assert task["no_log"] is True and task["failed_when"] is False and holds(task["when"]) and not holds(task["when"], private=False)
    guard = tasks[refused]
    assert "no_log" not in guard
    loop_var = guard["loop_control"]["loop_var"]
    register = task["register"]
    for result, diagnosis in OUTCOMES:
        for position, leaf in enumerate(task["loop"]):
            results = [dict(FETCHED) for _leaf in task["loop"]]
            results[position] = result
            variables = dict(guard.get("vars", {}), **{register: {"results": results}})
            for other in range(len(task["loop"])):
                assert holds(guard["when"], **dict(variables, **{loop_var: other})) == (other == position)
            message = render(guard[FAIL]["msg"], **dict(variables, **{loop_var: position}))
            assert TOKEN not in message, message
            assert message.startswith("the private %s could not be" % leaf) and message.endswith(": " + diagnosis), message


def test_an_insert_refusal_never_prints_the_token_the_controller_echoes():
    boot = flattened("boot.yml")
    guard = boot[index_of(boot, named("Stop at an insert the management controller did not complete"), "insert refusal")]
    assert "no_log" not in guard
    private_echo = "attaching " + PRIVATE["url"] + "/" + TOKEN + "/install.iso: HTTP 400"
    message = render(guard["ansible.builtin.assert"]["fail_msg"],
                     managedos_install_anaconda_inserted={"msg": private_echo})
    assert TOKEN not in message and private_echo.replace(TOKEN, "<token>") in message, message
    public_echo = "attaching " + PUBLIC["url"] + ": HTTP 400"
    public = render(guard["ansible.builtin.assert"]["fail_msg"], private=False,
                    managedos_install_anaconda_inserted={"msg": public_echo})
    assert public.endswith("failed: " + public_echo), public


def test_the_insert_asks_for_the_verified_fetch_read_back_exactly_when_delivery_is_private():
    boot = flattened("boot.yml")
    insert = boot[index_of(boot, named("Insert the published installer image as virtual media"), "insert")]
    arguments = insert["bootwright.core.redfish_boot"]
    assert arguments["operation"] == "insert"
    assert "private_delivery" in arguments, "the insert never asks for the D104 (b) read-back"
    assert render(arguments["private_delivery"]) is True
    assert render(arguments["private_delivery"], private=False) is False
    assert render(arguments["image"]) == PRIVATE["url"] + "/" + TOKEN + "/install.iso"


def test_observation_evidence_redacts_a_token_in_the_media_the_controller_reports():
    rewritten = "https://controller.metal.example.test:443/private/os/metal-01/%s/install.iso" % TOKEN
    for name, register in (("apply.yml", "managedos_install_anaconda_ejected"), ("observe.yml", "managedos_install_anaconda_controller")):
        tasks = flattened(name)
        completion = tasks[index_of(tasks, lambda task: (task.get(PROTOCOL) or {}).get("phase") == "completed", "completion")][PROTOCOL]
        media = render(completion["media"], **{register: {"media": rewritten}})
        assert TOKEN not in media and media == rewritten.replace(TOKEN, "<token>"), (name, media)
        assert render(completion["media"], **{register: {"media": ""}}) == ""
        assert render(completion["privateDelivery"]) is True and render(completion["privateDelivery"], private=False) is False


def presence(private_delivery, image):
    arguments = {"observation": {"image": image, "private": False}, "marker": "{}", "hostKey": "ssh-ed25519 AAAA",
                 "address": "198.51.100.41", "media": "", "power": "On", "reachable": True, "digest": DIGEST}
    if private_delivery is not None:
        arguments["privateDelivery"] = private_delivery
    return arguments


def test_a_private_completion_proves_its_image_withdrawn_and_a_public_one_its_image_in_place():
    for private_delivery, image, proved in ((True, False, True), (True, True, False), (False, False, False),
                                            (False, True, True), (None, True, True), ("True", False, True)):
        evidence, unmet = protocol.completion(presence(private_delivery, image))
        assert evidence["postcondition"] is proved, (private_delivery, image)
        assert (unmet is None) is proved, (private_delivery, image, unmet)
    _evidence, unmet = protocol.completion(presence(True, True))
    assert "private installer image still published" in str(unmet)
    _evidence, unmet = protocol.completion(dict(presence(True, False), observation={"image": False, "private": True}))
    assert "private material still published" in str(unmet)


def test_the_inspection_reports_a_private_installer_image(tmp_path):
    root = tmp_path / "private" / "os" / "metal-01"
    request = {"private": {"path": str(root)}}
    assert observe(request, "", "") == dict(observe(request, "", ""), image=False, private=False)
    minted = root / TOKEN
    minted.mkdir(parents=True)
    (minted / "identity").write_text("key")
    assert (observe(request, "", "")["image"], observe(request, "", "")["private"]) == (False, True)
    (minted / "nested").mkdir()
    (minted / "nested" / "install.iso").write_bytes(b"\0")
    assert observe(request, "", "")["image"] is False
    (minted / "install.iso").write_bytes(b"\0")
    assert (observe(request, "", "")["image"], observe(request, "", "")["private"]) == (True, True)
    public = tmp_path / "public" / "install.iso"
    assert observe({"image": {"path": str(public)}}, "", "")["image"] is False
    public.parent.mkdir()
    public.write_bytes(b"\0")
    assert observe({"image": {"path": str(public)}}, "", "")["image"] is True
