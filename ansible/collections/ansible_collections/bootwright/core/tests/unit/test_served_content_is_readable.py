"""What a managed artifact server serves is readable by its worker and nothing broader.

nginx opens every served file as the worker identity its configuration names,
so a file that identity cannot read answers the machine meant to fetch it with
an error, while a file others can read is readable by every account on the
host. The syntax check, lint and the server's own readiness, which proves only
that each listener answers, accept either. This ties every file task beneath a
served root to the worker identity the template declares, and holds the agent
image to the fetch that proves its publication end to end: the probe itself is
run against a listener on 127.0.0.1, verifying it against the serving
certificate bound from the server's Secret. Its certificates are built with
cryptography at test time, so no key is committed.
"""

from __future__ import annotations

import datetime
import http.client
import http.server
import ipaddress
import json
import pathlib
import re
import ssl
import threading
import urllib.error

import pytest
import yaml
from ansible.module_utils.testing import patch_module_args
from ansible.modules import uri as uri_module
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID

from ansible_collections.bootwright.core.plugins.modules import containercluster_install_inspect

ROLES = pathlib.Path(__file__).resolve().parents[2] / "roles"
SERVER = ROLES / "infra_artifact_server_nginx"
TEMPLATE = SERVER / "templates" / "nginx.conf.j2"
MEDIA = ROLES / "containercluster_media_agent"

# The published image exactly as TestTheBootImageIsPublishedPrivately pins it
# (internal/containercluster/agentinstall/projection_test.go): PrivatePath's
# <content root>/public/private/clusters/<cluster>, and the URL it is served at.
CONTENT_ROOT = "/var/lib/bootwright-services/lab/artifact-server/lab-artifacts"
IMAGE = {"path": CONTENT_ROOT + "/public/private/clusters/sno", "url": "https://192.0.2.1:8443/private/clusters/sno"}
# What `openssl rand -hex 32` mints: 64 hexadecimal digits.
TOKEN = "0123456789abcdef" * 4
# The Secret the selected server's serving certificate is bound from, as the
# fixtures of internal/containercluster/agentinstall declare it.
SERVING_SECRET = "artifact-server-tls"

# The worker directive, the served root and the serving certificate, as the
# template spells them. nginx takes the user's own name as the group when the
# directive names none.
WORKER = re.compile(r"^\s*user\s+([^\s;]+)(?:\s+([^\s;]+))?\s*;", re.MULTILINE)
ROOT = re.compile(r"^\s*root\s+\{\{\s*([a-z_][a-z0-9_]*)\s*\}\}\s*;", re.MULTILINE)
CERTIFICATE = re.compile(r"^\s*ssl_certificate\s+\{\{\s*([a-z_][a-z0-9_]*)\s*\}\}(/[^\s;]+)\s*;", re.MULTILINE)

# The request fields a consumer publishes beneath the served root, derived by
# PublicPath and PrivatePath in
# internal/infrastructureservices/artifactserver/publication.go.
CONSUMER_PATHS = re.compile(r"\bbootwright_\w+_request\.(?:image|tree|private)\.path\b")

FILE_MODULES = ("ansible.builtin.file", "ansible.builtin.copy", "ansible.builtin.template")
LOOPED = re.compile(r"\{\{\s*item\.([a-z_]+)\s*\}\}")

# The private publications the walk must reach; finding fewer passes vacuously.
PUBLISHED = {
    ("containercluster_media_agent/tasks/build.yml", "Keep the published image readable only by the serving process"),
    ("managedos_install_anaconda/tasks/private.yml", "Publish the host key pair this installation delivers"),
    ("managedos_install_anaconda/tasks/apply.yml", "Keep the private installer image readable only by the serving process"),
}


def load(path):
    parsed = yaml.safe_load(path.read_text()) or []
    return [task for task in parsed if isinstance(task, dict)]


def worker():
    match = WORKER.search(TEMPLATE.read_text())
    assert match, "nginx.conf.j2 names no worker user"
    return match.group(1), match.group(2) or match.group(1)


def served_variables(defaults):
    """Every role default whose value names a served root, followed to a fixpoint."""
    names = {ROOT.search(TEMPLATE.read_text()).group(1)}
    changed = True
    while changed:
        changed = False
        for name, value in defaults.items():
            if name not in names and served(str(value), names):
                names.add(name)
                changed = True
    return names


def served(expression, names):
    return bool(CONSUMER_PATHS.search(expression)) or any(
        re.search(r"\b%s\b" % re.escape(name), expression) for name in names
    )


def substitute(value, item):
    if not isinstance(value, str):
        return value
    return LOOPED.sub(lambda found: str(item[found.group(1)]), value)


def instances(arguments, loop):
    """The arguments once per literal loop item, or once when there is none."""
    if not (isinstance(loop, list) and loop and all(isinstance(item, dict) for item in loop)):
        return [arguments]
    return [{key: substitute(value, item) for key, value in arguments.items()} for item in loop]


def role_defaults(role):
    path = role / "defaults" / "main.yml"
    if not path.is_file():
        return {}
    return yaml.safe_load(path.read_text()) or {}


def served_file_tasks():
    """Each file task beneath a served root: where it is, and the arguments it sets."""
    found = []
    for role in sorted(path for path in ROLES.iterdir() if (path / "tasks").is_dir()):
        names = served_variables(role_defaults(role))
        for path in sorted((role / "tasks").glob("*.yml")):
            for task in load(path):
                module = next((name for name in FILE_MODULES if isinstance(task.get(name), dict)), None)
                if module is None or task[module].get("state") == "absent":
                    continue
                for arguments in instances(task[module], task.get("loop")):
                    if served(str(arguments.get("path", arguments.get("dest", ""))), names):
                        found.append((path.relative_to(ROLES).as_posix(), task.get("name", ""), module, arguments))
    return found


def worker_bits(owner, group, mode, identity):
    """The permission class the worker falls in, as the kernel chooses it."""
    user, worker_group = identity
    if owner == user:
        return (mode >> 6) & 7
    if group == worker_group:
        return (mode >> 3) & 7
    return mode & 7


def problems_of(module, arguments, identity):
    owner, group, mode = arguments.get("owner"), arguments.get("group"), arguments.get("mode")
    literal = all(isinstance(value, str) for value in (owner, group, mode))
    if not literal or not re.fullmatch(r"0?[0-7]{3}", mode):
        return ["sets no literal owner, group and quoted mode, so what the worker may read is unproved"]
    bits = int(mode, 8)
    reach = worker_bits(owner, group, bits, identity)
    if module == "ansible.builtin.file" and arguments.get("state") == "directory":
        return [] if reach & 1 else ["is a directory the worker %s:%s cannot search" % identity]
    found = []
    if not reach & 4:
        found.append("is a file the worker %s:%s cannot read" % identity)
    if bits & 0o007:
        found.append("grants %s to every other account" % mode)
    if bits & 0o070 and group != identity[1]:
        found.append("grants group %s, which is not the worker's" % group)
    if owner not in ("root", identity[0]):
        found.append("belongs to %s, which is neither root nor the worker" % owner)
    return found


def test_the_worker_identity_is_the_one_the_template_declares():
    assert worker() == ("default", "root")


def test_the_walk_reaches_every_private_publication():
    seen = {(label, name) for label, name, _module, _arguments in served_file_tasks()}
    assert PUBLISHED <= seen, "the walk no longer sees %s" % sorted(PUBLISHED - seen)


def test_every_file_beneath_a_served_root_is_readable_by_the_worker_and_nothing_broader():
    identity = worker()
    problems = [
        "%s: %r %s" % (label, name, problem)
        for label, name, module, arguments in served_file_tasks()
        for problem in problems_of(module, arguments, identity)
    ]
    assert not problems, "\n".join(problems)


def test_the_permission_rule_refuses_what_the_worker_cannot_read_or_others_can():
    identity = ("default", "root")
    arguments = {"path": "x", "owner": "root", "group": "root"}
    assert problems_of("ansible.builtin.file", dict(arguments, mode="0640"), identity) == []
    assert problems_of("ansible.builtin.file", dict(arguments, mode="0600"), identity)
    assert problems_of("ansible.builtin.file", dict(arguments, mode="0644"), identity)
    assert problems_of("ansible.builtin.file", dict(arguments, mode=0o640), identity)
    assert problems_of("ansible.builtin.file", dict(arguments, mode="0711", state="directory"), identity) == []
    assert problems_of("ansible.builtin.file", dict(arguments, mode="0700", state="directory"), identity)


def build_tasks():
    return load(ROLES / "containercluster_media_agent" / "tasks" / "build.yml")


def position(tasks, description, predicate):
    matches = [index for index, task in enumerate(tasks) if predicate(task)]
    assert len(matches) == 1, "build.yml has %d tasks that %s" % (len(matches), description)
    return matches[0]


def argv(task):
    command = task.get("ansible.builtin.command")
    return [str(value) for value in (command or {}).get("argv") or []] if isinstance(command, dict) else []


def test_the_published_image_is_fetched_through_the_listener_before_its_receipt():
    tasks = build_tasks()
    order = [
        position(tasks, "publish the image by rename",
                 lambda task: argv(task)[:1] == ["/usr/bin/mv"] and argv(task)[-1].endswith("/agent.iso")),
        position(tasks, "set the published image's mode",
                 lambda task: str((task.get("ansible.builtin.file") or {}).get("path", "")).endswith("/agent.iso")),
        position(tasks, "label the published subtree", lambda task: argv(task)[:1] == ["/usr/bin/chcon"]),
        position(tasks, "fetch through the listener", lambda task: "ansible.builtin.uri" in task),
        position(tasks, "refuse a failed fetch", lambda task: "ansible.builtin.fail" in task),
        position(tasks, "record the receipt",
                 lambda task: "receipt" in str((task.get("ansible.builtin.copy") or {}).get("dest", ""))),
    ]
    assert order == sorted(order), "the fetch must follow the whole publication and precede the receipt"


def test_the_fetch_verifies_the_served_certificate_directly_and_refuses_anything_but_success():
    tasks = build_tasks()
    probe = tasks[position(tasks, "fetch through the listener", lambda task: "ansible.builtin.uri" in task)]
    guard = tasks[position(tasks, "refuse a failed fetch", lambda task: "ansible.builtin.fail" in task)]
    uri = probe["ansible.builtin.uri"]
    assert uri.get("validate_certs", True) is True and uri.get("use_proxy") is False
    assert uri.get("follow_redirects") == "none" and uri["headers"] == {"Range": "bytes=0-0"}
    assert sorted(uri["status_code"]) == [200, 206]
    assert probe.get("no_log") is True and probe.get("failed_when") is False
    assert guard["when"] == "%s.status | default(-1) | int not in [200, 206]" % probe["register"]
    assert "no_log" not in guard and "failed_when" not in guard


# The probe and its guard are judged by what ansible-core renders from them,
# because a path, an address or a message that reads right can still resolve
# wrong.
LOADER = DataLoader()


def trusted(path):
    """A task or defaults file as a play loads it: its templates trusted."""
    return LOADER.load_from_file(str(path), trusted_as_template=True)


def templar(role, variables):
    scope = dict(trusted(role / "defaults" / "main.yml"))
    scope.update(variables)
    return Templar(loader=LOADER, variables=scope)


def trusted_build_task(description, predicate):
    tasks = [task for task in trusted(MEDIA / "tasks" / "build.yml") if isinstance(task, dict)]
    return tasks[position(tasks, description, predicate)]


def media_scope(image, material="", **variables):
    """What the media role renders the probe from: the frozen request, the file
    the runner wrote the bound certificate to, and the minted token."""
    return dict(variables, bootwright_cluster_media_request={"image": image, "tlsCertificateRef": SERVING_SECRET},
                bootwright_cluster_media_material={"artifactCertificate": material},
                containercluster_media_agent_token={"stdout": TOKEN})


def installed_certificate(content_root):
    """Where the server installs its copy of the serving certificate, as the template names it."""
    server = templar(SERVER, {"bootwright_artifact_server_request": {"contentRoot": content_root}})
    variable, leaf = CERTIFICATE.search(TEMPLATE.read_text()).groups()
    return server.resolve_variable_expression(variable) + leaf


def test_the_fetch_reads_the_address_a_machine_boots_from_verified_by_the_bound_certificate(tmp_path):
    uri = trusted_build_task("fetch through the listener", lambda task: "ansible.builtin.uri" in task)["ansible.builtin.uri"]
    text = TEMPLATE.read_text()
    server = templar(SERVER, {"bootwright_artifact_server_request": {"contentRoot": CONTENT_ROOT}})
    # The image the requests carry lies beneath the root the template serves,
    # in PrivatePath's layout.
    assert IMAGE["path"] == server.resolve_variable_expression(ROOT.search(text).group(1)) + "/private/clusters/sno"
    # The one authority is the file the runner wrote the bound certificate to,
    # never the copy the server installed beneath that root.
    bound = str(tmp_path / "artifact-ca")
    media = templar(MEDIA, media_scope(IMAGE, bound))
    assert media.template(uri["ca_path"]) == bound != installed_certificate(CONTENT_ROOT)
    # The address is the one the install block reads back from what the
    # rename published, and hands to the machine that boots it.
    rename = trusted_build_task("publish the image by rename",
                                lambda task: argv(task)[:1] == ["/usr/bin/mv"] and argv(task)[-1].endswith("/agent.iso"))
    published = pathlib.Path(templar(MEDIA, media_scope({"path": str(tmp_path)})).template(
        rename["ansible.builtin.command"]["argv"][-1]))
    published.parent.mkdir()
    published.write_bytes(b"")
    expected = containercluster_install_inspect.published(str(tmp_path), IMAGE["url"])
    assert expected and media.template(uri["url"]) == expected


# What the probe registers, in the forms ansible-core 2.21.4 gives them:
# ansible/modules/uri.py prefixes a refused status with "Status code was %s and
# not %s: " and fails a body it cannot read with "HTTP Error while fetching
# {masked_url}: ..." and no status; ansible/module_utils/urls.py fetch_url
# reports an HTTPError as itself and a URLError as "Request failed: %s", with
# status -1. The certificate failure is the text CPython 3.13 with OpenSSL 3.5
# raised for a foreign self-signed leaf; like the refused connection, it names
# no URL (.agents/knowledge/artifact-server-nginx-runtime.md).
REFUSED = "Status code was %s and not [200, 206]: "
ADDRESS = IMAGE["url"] + "/" + TOKEN + "/agent.iso"
UNVERIFIED = "Request failed: %s" % urllib.error.URLError(
    ssl.SSLCertVerificationError(1, "[SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed: self-signed certificate (_ssl.c:1032)"))
UNREACHED = "Request failed: %s" % urllib.error.URLError(ConnectionRefusedError(111, "Connection refused"))
UNREAD = "HTTP Error while fetching %s: %s" % (ADDRESS, http.client.IncompleteRead(b"", 1))
OUTCOMES = [
    ({"status": 403, "msg": REFUSED % 403 + str(urllib.error.HTTPError(ADDRESS, 403, "Forbidden", None, None))},
     "the listener answered 403, so the worker it serves as cannot read the image"),
    ({"status": 404, "msg": REFUSED % 404 + str(urllib.error.HTTPError(ADDRESS, 404, "Not Found", None, None))},
     "the listener answered 404, so the worker it serves as finds no image at the path this attempt published"),
    ({"status": -1, "msg": REFUSED % -1 + UNVERIFIED},
     "its certificate did not verify against the serving certificate Secret %s holds: " % SERVING_SECRET
     + REFUSED % -1 + UNVERIFIED),
    ({"status": -1, "msg": REFUSED % -1 + UNREACHED},
     "the request failed before any status line: " + REFUSED % -1 + UNREACHED),
    ({"failed": True, "msg": UNREAD},
     "the request failed before any status line: " + UNREAD.replace(TOKEN, "<token>")),
    ({"status": 500, "msg": REFUSED % 500 + str(urllib.error.HTTPError(ADDRESS, 500, "Internal Server Error", None, None))},
     "the listener answered 500 where 200 or 206 was expected"),
]


def test_a_refused_fetch_fails_with_its_diagnosed_cause_and_never_the_token():
    probe = trusted_build_task("fetch through the listener", lambda task: "ansible.builtin.uri" in task)
    guard = trusted_build_task("refuse a failed fetch", lambda task: "ansible.builtin.fail" in task)
    assert TOKEN in UNREAD, "no outcome carries the token, so its redaction is unproved"
    for result, diagnosis in OUTCOMES:
        rendering = templar(MEDIA, media_scope(IMAGE, **guard.get("vars", {}), **{probe["register"]: result}))
        assert rendering.evaluate_conditional(guard["when"]), result
        message = rendering.template(guard["ansible.builtin.fail"]["msg"])
        assert TOKEN not in message, message
        assert message.endswith(": " + diagnosis), message
    for status in (200, 206):
        assert not templar(MEDIA, media_scope(IMAGE, **{probe["register"]: {"status": status}})).evaluate_conditional(guard["when"])


def serving_certificate():
    """A key and a certificate for 127.0.0.1 shaped as `secret generate` makes a
    serving one (internal/secrets/material/generation.go): self-signed, not a
    certificate authority, digital signature and server authentication only."""
    key = ec.generate_private_key(ec.SECP256R1())
    now = datetime.datetime.now(datetime.timezone.utc)
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "127.0.0.1")])
    certificate = (
        x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - datetime.timedelta(minutes=5)).not_valid_after(now + datetime.timedelta(days=1))
        .add_extension(x509.SubjectAlternativeName([x509.IPAddress(ipaddress.ip_address("127.0.0.1"))]), critical=False)
        .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
        .add_extension(x509.KeyUsage(digital_signature=True, content_commitment=False, key_encipherment=False,
                                     data_encipherment=False, key_agreement=False, key_cert_sign=False,
                                     crl_sign=False, encipher_only=False, decipher_only=False), critical=True)
        .add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH]), critical=False)
        .sign(key, hashes.SHA256()))
    return key, certificate.public_bytes(serialization.Encoding.PEM).decode("ascii")


class Serving(http.server.BaseHTTPRequestHandler):
    """A listener that serves the first byte of one published image, at the
    path its server names, and nothing else."""

    def do_GET(self):
        if self.path != self.server.published or self.headers.get("Range") != "bytes=0-0":
            self.send_error(404)
            return
        self.send_response(206)
        self.send_header("Content-Range", "bytes 0-0/1")
        self.send_header("Content-Length", "1")
        self.end_headers()
        self.wfile.write(b"\0")

    def log_message(self, *args):
        return


def listening(request, directory, key, certificate):
    """A listener on 127.0.0.1 presenting the given certificate, and its port."""
    chain, private = directory / "presented.crt", directory / "presented.key"
    chain.write_text(certificate)
    private.write_bytes(key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8,
                                          serialization.NoEncryption()))
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(str(chain), str(private))
    server = http.server.HTTPServer(("127.0.0.1", 0), Serving)
    server.published = "/private/clusters/sno/%s/agent.iso" % TOKEN
    server.socket = context.wrap_socket(server.socket, server_side=True)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()

    def stop():
        server.shutdown()
        server.server_close()
        thread.join()

    request.addfinalizer(stop)
    return server.server_address[1]


def probed(arguments, directory, capsys):
    """What ansible.builtin.uri registers for the probe's rendered arguments,
    run in process as the task runs it."""
    module_tmp = directory / "module"
    module_tmp.mkdir()
    capsys.readouterr()
    with patch_module_args(dict(arguments, _ansible_tmpdir=str(module_tmp))), pytest.raises(SystemExit):
        uri_module.main()
    return json.loads(capsys.readouterr().out)


# The probe is run as the task runs it, against a listener on 127.0.0.1. Its
# server installed a copy of a certificate its Secret no longer holds, as it
# would after the Secret was replaced and before the server was applied
# again, so a probe anchored in that copy would prove the wrong certificate:
# the listener presenting the bound certificate is fetched, and one presenting
# the installed copy is refused as unverified, naming the Secret.
@pytest.mark.parametrize("presented", ["bound", "installed"])
def test_the_probe_verifies_the_listener_against_the_bound_certificate_alone(presented, tmp_path, request, capsys):
    certificates = {"bound": serving_certificate(), "installed": serving_certificate()}
    root = tmp_path / "content"
    installed = pathlib.Path(installed_certificate(str(root)))
    installed.parent.mkdir(parents=True)
    installed.write_text(certificates["installed"][1])
    bound = tmp_path / "artifact-ca"
    bound.write_text(certificates["bound"][1])
    port = listening(request, tmp_path, *certificates[presented])
    image = {"path": str(root / "public/private/clusters/sno"), "url": "https://127.0.0.1:%d/private/clusters/sno" % port}
    probe = trusted_build_task("fetch through the listener", lambda task: "ansible.builtin.uri" in task)
    guard = trusted_build_task("refuse a failed fetch", lambda task: "ansible.builtin.fail" in task)
    result = probed(templar(MEDIA, media_scope(image, str(bound))).template(probe["ansible.builtin.uri"]), tmp_path, capsys)
    rendering = templar(MEDIA, media_scope(image, str(bound), **guard.get("vars", {}), **{probe["register"]: result}))
    if presented == "bound":
        assert result["status"] == 206, result
        assert not rendering.evaluate_conditional(guard["when"])
        return
    assert result["status"] == -1 and "CERTIFICATE_VERIFY_FAILED" in result["msg"], result
    assert rendering.evaluate_conditional(guard["when"])
    message = rendering.template(guard["ansible.builtin.fail"]["msg"])
    assert "its certificate did not verify against the serving certificate Secret %s holds: " % SERVING_SECRET in message, message
    assert TOKEN not in message, message
