"""Private custody and bounded Redfish/TLS transport for RAM disk inventory."""

from __future__ import annotations

import base64
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import stat
import time
import urllib.request
from itertools import islice

from ansible_collections.bootwright.core.plugins.module_utils import absolute_deadline
from ansible_collections.bootwright.core.plugins.module_utils import canonical_json
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_media as media
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_boot as boot
from ansible_collections.bootwright.core.plugins.module_utils import tls_trust
from ansible_collections.bootwright.core.plugins.module_utils import hosted_area
from ansible_collections.bootwright.core.plugins.module_utils import private_records as records
from ansible_collections.bootwright.core.plugins.module_utils import redfish_control

ROOT = Path(hosted_area.ROOT)
VERSION = "os-disk-discovery-v1"
MAX_REPORT = 48 << 10
ENV = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}


def validate(request, digest, binding):
    keys = {"version", "identity", "bootMedia", "controller", "interfaces", "network", "private", "deadlineSeconds", "placement"}
    if set(request) != keys or request["version"] != VERSION or re.fullmatch(r"[0-9a-f]{64}", digest) is None:
        raise ValueError("discovery request shape")
    if (
        request["controller"].get("tlsVerify") is not True
        or request["controller"].get("tlsVerification") == "none"
        or not request["controller"]["endpoint"].startswith("https://")
    ):
        raise ValueError("discovery controller TLS")
    if request["controller"]["virtualMedia"]["trust"] not in ("established", "importCertificate") or not request["private"]["url"].startswith("https://"):
        raise ValueError("discovery private media TLS")
    if request["deadlineSeconds"] != 1800 or binding.get("requestDigest") != digest:
        raise ValueError("discovery binding")
    for key in ("context", "machine"):
        if not isinstance(binding.get(key), str) or re.fullmatch(r"[a-z0-9][a-z0-9-]{0,62}", binding[key]) is None:
            raise ValueError("discovery machine identity")
    if re.fullmatch(r"op-[0-9a-f]{32}", binding.get("operation", "")) is None or re.fullmatch(r"attempt-[0-9]{6}", binding.get("attempt", "")) is None:
        raise ValueError("discovery attempt identity")
    for key in ("nonce", "requestDigest", "mediaSHA256", "toolDigest"):
        if re.fullmatch(r"[0-9a-f]{64}", binding.get(key, "")) is None:
            raise ValueError("discovery provenance")
    if (
        request["identity"]["context"] != binding["context"]
        or request["identity"]["object"] != binding["machine"]
        or request["bootMedia"]["sha256"] != binding["mediaSHA256"]
    ):
        raise ValueError("discovery request identity")


class Controller:
    def __init__(self, request, digest, material, create=False):
        self.phase = "custody"
        self.request, self.digest, self.material = request, digest, material
        encoded = material.get("bindingBase64", "")
        if not isinstance(encoded, str) or len(encoded) > 8192:
            raise ValueError("discovery invocation")
        self.binding = json.loads(base64.b64decode(encoded, validate=True))
        validate(request, digest, self.binding)
        parts = ["machines", self.binding["machine"], "discovery", self.binding["operation"]]
        if create:
            hosted_area.ensure(self.binding["context"], parts, root=str(ROOT))
        elif not hosted_area.prove(self.binding["context"], parts, root=str(ROOT)):
            raise FileNotFoundError("discovery custody")
        self.root = Path(hosted_area.target(self.binding["context"], parts, root=str(ROOT)))
        self.lock = os.open(self.root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        fcntl.flock(self.lock, fcntl.LOCK_EX)

    def close(self):
        os.close(self.lock)

    def load(self):
        state = json.loads(records.read(self.root / "custody.json"))
        if (
            set(state) != {"binding", "token", "reportToken"}
            or state["binding"] != self.binding
            or any(re.fullmatch(r"[0-9a-f]{64}", state[key]) is None for key in ("token", "reportToken"))
        ):
            raise ValueError("foreign discovery custody")
        return state

    def report(self):
        try:
            data = records.read(self.root / "report.json", MAX_REPORT)
        except FileNotFoundError:
            return None
        self.validate_report(data)
        return data

    def validate_report(self, data):
        if not 0 < len(data) <= MAX_REPORT:
            raise ValueError("discovery report size")
        report = json.loads(data)
        if set(report) != {"version", "binding", "disks"} or report["version"] != "machine-disk-inventory-v1" or report["binding"] != self.binding:
            raise ValueError("foreign discovery report")
        if not isinstance(report["disks"], list) or not 1 <= len(report["disks"]) <= 256 or canonical_json.canonical(report) != data:
            raise ValueError("discovery report wire")
        return report

    def evidence(self):
        self.load()
        proof = self.completion()
        if proof is not None and (self.root / "sealed.json").exists():
            seal = json.loads(records.read(self.root / "sealed.json", 4096))
            if seal != self.seal(proof):
                raise ValueError("discovery local cleanup seal")
            return proof
        return {"version": VERSION, "request": self.digest, "completed": False, "cleaned": False}

    def completion(self):
        try:
            data = records.read(self.root / "completed.json", MAX_REPORT + 4096)
        except FileNotFoundError:
            if (self.root / "sealed.json").exists():
                raise ValueError("discovery completion is missing beneath its seal") from None
            return None
        proof = json.loads(data)
        if proof.get("version") != VERSION or proof.get("request") != self.digest or proof.get("cleaned") is not True:
            raise ValueError("discovery completion identity")
        if proof.get("completed"):
            report = self.report()
            if report is None or proof.get("report") != json.loads(report):
                raise ValueError("discovery completion report")
        return proof

    def seal(self, proof):
        digest = hashlib.sha256(canonical_json.canonical(proof)).hexdigest()
        return {"completedSHA256": digest, "request": self.digest, "version": VERSION}

    def prepare(self):
        if not (self.root / "custody.json").exists():
            self.prove_unprepared()
            records.write(
                self.root / "custody.json",
                canonical_json.canonical({"binding": self.binding, "token": secrets.token_hex(32), "reportToken": secrets.token_hex(32)}),
            )
        state = self.load()
        if not (self.root / "tls").exists():
            self.prepare_tls()
        records.directory(self.root / "tls", False)
        records.read(self.root / "tls/server-key.pem")
        records.read(self.root / "tls/server.pem")
        return state

    def prepare_tls(self):
        self.phase = "tls"
        self.reap_tls_stages()
        stage = self.root / (".tls-stage-" + secrets.token_hex(16))
        records.directory(stage, True)
        try:
            key, certificate = stage / "server-key.pem", stage / "server.pem"
            media.native(
                [
                    "/usr/bin/openssl",
                    "req",
                    "-x509",
                    "-newkey",
                    "ec",
                    "-pkeyopt",
                    "ec_paramgen_curve:P-256",
                    "-nodes",
                    "-days",
                    "2",
                    "-subj",
                    "/CN=Bootwright RAM inventory",
                    "-addext",
                    "basicConstraints=critical,CA:FALSE",
                    "-addext",
                    "extendedKeyUsage=serverAuth",
                    "-keyout",
                    str(key),
                    "-out",
                    str(certificate),
                ]
            )
            for path in (key, certificate):
                os.chmod(path, 0o600)
                records.read(path)
                fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
                try:
                    os.fsync(fd)
                finally:
                    os.close(fd)
            held = os.open(stage, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            try:
                os.fsync(held)
            finally:
                os.close(held)
            os.rename(stage, self.root / "tls")
            os.fsync(self.lock)
        finally:
            if stage.exists():
                for name in ("server-key.pem", "server.pem"):
                    (stage / name).unlink(missing_ok=True)
                stage.rmdir()

    def reap_tls_stages(self):
        with os.scandir(self.root) as entries:
            names = [entry.name for entry in islice(entries, 4097)]
        if len(names) > 4096:
            raise ValueError("discovery custody entry bound")
        for name in names:
            if re.fullmatch(r"\.tls-stage-[0-9a-f]{32}", name) is None:
                continue
            stage = self.root / name
            records.directory(stage, False)
            with os.scandir(stage) as entries:
                children = [entry.name for entry in islice(entries, 3)]
            if set(children) - {"server-key.pem", "server.pem"}:
                raise ValueError("foreign discovery TLS stage")
            for child in children:
                records.read(stage / child)
                (stage / child).unlink()
            stage.rmdir()

    def payload(self):
        return boot.payload(
            self.binding,
            self.request["network"],
            self.request["interfaces"],
            self.load()["reportToken"],
            records.read(self.root / "tls/server.pem"),
            records.read(self.root / "tls/server-key.pem"),
        )

    def publication(self):
        state = self.load()
        parent = Path(self.request["private"]["path"])
        if not parent.is_absolute() or ".." in parent.parts:
            raise ValueError("discovery publication path")
        return parent / state["token"], self.request["private"]["url"] + "/" + state["token"] + "/discovery.iso"

    def publish(self):
        self.phase = "media"
        state = self.load()
        for name in ("patched.iso", "discovery.iso", "initrd-0", "initrd-1"):
            (self.root / name).unlink(missing_ok=True)
        image = media.build(self.request, self.root, self.payload())
        self.phase = "publication"
        destination, unused = self.publication()
        del unused
        self.publication_directories(destination)
        target = destination / "discovery.iso"
        target.unlink(missing_ok=True)
        publish_image(image, target, media.publication_bound(self.request["bootMedia"]["size"]))
        media.native(["/usr/bin/chcon", "--reference", str(destination.parents[4]), "--recursive", str(destination)])
        os.fsync(self.lock)

    def publication_directories(self, destination):
        private = Path(self.request["private"]["path"])
        suffix = ("private", "os", self.binding["machine"], "discovery")
        if (
            not private.is_absolute() or ".." in private.parts or private.parts[-4:] != suffix
            or destination.parent != private or re.fullmatch(r"[0-9a-f]{64}", destination.name) is None
        ):
            raise ValueError("discovery publication path")
        serving = private.parents[3]
        # Never create or chmod the serving root or its unrelated ancestors.
        records.directory(serving, False, None)
        flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
        parent = os.open(serving, flags)
        try:
            for name in (*suffix, destination.name):
                try:
                    os.mkdir(name, 0o700, dir_fd=parent)
                except FileExistsError:
                    pass
                child = os.open(name, flags, dir_fd=parent)
                try:
                    info = os.fstat(child)
                    if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
                        raise records.Refused("custody-directory")
                    # Resume a crash after restrictive creation, only inside
                    # the frozen private subtree. Preserve other safe modes.
                    if stat.S_IMODE(info.st_mode) == 0o700:
                        os.fchmod(child, 0o711)
                    os.fsync(child)
                    os.fsync(parent)
                except BaseException:
                    os.close(child)
                    raise
                os.close(parent)
                parent = child
        finally:
            os.close(parent)

    def client(self):
        config = self.request["controller"]
        user = records.read(Path(self.material["controllerUser"])).decode()
        password = records.read(Path(self.material["controllerPassword"])).decode()
        ca = records.read(Path(self.material["controllerCA"])).decode() if self.material.get("controllerCA") else ""
        return redfish_control.Client(config["endpoint"], user, password, verify=True, ca_data=ca, verification=config.get("tlsVerification", ""))

    def target(self, client):
        identity = client.identity()
        macs, failures = client.hardware_addresses()
        if failures or identity["UUID"].strip().lower() != self.binding["uuid"] or macs != self.binding["macs"]:
            raise ValueError("discovery controller identity")
        return client.power_state()

    def session(self, command, data=b""):
        certificate = records.read(self.root / "tls/server.pem").decode("ascii")
        context, leaf = tls_trust.context(certificate, "certificate")
        connection = tls_trust.CertificateConnection(self.request["network"]["address"] + ":9443", leaf, context=context, timeout=30)
        token = self.load()["reportToken"]
        try:
            with absolute_deadline.limited(30):
                connection.connect()  # Pin the leaf before the token crosses TLS.
                connection.request(
                    "GET" if command == "receipt" else "POST",
                    "/report" if command == "receipt" else "/ack",
                    body=data,
                    headers={"Authorization": "Bearer " + token},
                )
                response = connection.getresponse()
                if response.status != 200:
                    return None
                content = response.read(MAX_REPORT + 1)
                if len(content) > MAX_REPORT:
                    raise ValueError("discovery response exceeds bound")
                return content
        except (OSError, ValueError):
            return None
        finally:
            connection.close()

    def receive(self):
        self.phase = "inventory"
        existing = self.report()
        if existing is not None:
            return existing
        deadline = time.monotonic() + self.request["deadlineSeconds"]
        while time.monotonic() < deadline:
            answer = self.session("receipt")
            if answer:
                self.validate_report(answer)
                records.write(self.root / "report.json", answer)
                return answer
            time.sleep(3)
        raise ValueError("discovery report deadline")

    def start(self):
        self.phase = "controller"
        client = self.client()
        power = self.target(client)
        destination, url = self.publication()
        del destination
        if power == "On":
            if client.inserted() != url:
                raise ValueError("discovery target is already running")
            return  # The original RAM worker is still booting or serving.
        if power != "Off":
            raise ValueError("discovery target power is unknown")
        self.publish()
        self.phase = "controller"
        client = self.client()
        if self.target(client) != "Off":
            raise ValueError("discovery target started before insertion")
        if client.inserted() not in ("", url):
            raise ValueError("foreign virtual media")
        certificate = records.read(Path(self.material["artifactCertificate"])).decode()
        self.phase = "media-delivery"
        listener_probe(url, certificate)
        self.phase = "boot"
        client.insert(url, self.request["controller"]["virtualMedia"]["trust"], certificate, private=True)
        client.boot_once("Cd")
        client.power("On", "On")

    def cleanup(self, acknowledged):
        deadline = time.monotonic() + 600
        client = self.client()
        unused, url = self.publication()
        del unused
        while self.target(client) != "Off":
            if client.inserted() != url:
                raise ValueError("discovery lost ownership of the running boot")
            if not acknowledged:
                client.power("ForceOff", "Off")
            if time.monotonic() >= deadline:
                raise ValueError("discovery poweroff deadline")
            time.sleep(3)
            client = self.client()
        if client.inserted() not in ("", url):
            raise ValueError("foreign virtual media during cleanup")
        virtual = self.request["controller"]["virtualMedia"]
        certificate = records.read(Path(self.material["artifactCertificate"])).decode()
        client.eject(virtual["restoreVerification"], virtual["removeCertificate"], certificate)
        directory, unused = self.publication()
        del unused
        (directory / "discovery.iso").unlink(missing_ok=True)
        if directory.exists():
            records.directory(directory, False, None)
            directory.rmdir()
        if directory.parent.exists():
            records.directory(directory.parent, False, None)
            parent = os.open(directory.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            try:
                os.fsync(parent)
            finally:
                os.close(parent)

    def finish(self, complete):
        proof = {"version": VERSION, "request": self.digest, "completed": complete, "cleaned": True}
        if complete:
            proof["report"] = json.loads(self.report())
        # Cleanup never deletes the immutable raw receipt or its provenance.
        # Commit external cleanup before deleting transport material. A retry
        # can then finish local deletion without recreating a partial key pair.
        records.write(self.root / "completed.json", canonical_json.canonical(proof))
        return self.finish_local()

    def finish_local(self):
        self.load()
        proof = self.completion()
        if proof is None:
            raise ValueError("discovery external cleanup is unproved")
        self.reap()
        if not (self.root / "sealed.json").exists():
            records.write(self.root / "sealed.json", canonical_json.canonical(self.seal(proof)))
        return self.evidence()

    def reap(self):
        self.reap_tls_stages()
        tls = self.root / "tls"
        if tls.exists():
            records.directory(tls, False)
            for name in ("server-key.pem", "server.pem"):
                (tls / name).unlink(missing_ok=True)
            tls.rmdir()
        for name in ("server-key.pem", "server.pem", "discovery.iso", "patched.iso", "initrd-0", "initrd-1"):
            (self.root / name).unlink(missing_ok=True)

    def apply(self):
        if (self.root / "completed.json").exists():
            before = self.finish_local()
            if not before["completed"]:
                raise ValueError("discovery was removed")
            return before
        self.prepare()
        before = self.evidence()
        if before["cleaned"]:
            if not before["completed"]:
                raise ValueError("discovery was removed")
            return before
        if self.report() is None:
            self.start()
        report = self.receive()  # Immutable and fsynced before the ACK.
        self.phase = "acknowledgement"
        self.session("ack", hashlib.sha256(report).hexdigest().encode("ascii"))
        self.phase = "cleanup"
        self.cleanup(True)
        return self.finish(True)

    def destroy(self):
        # Completed discovery never inspects or stops an OS installed later.
        if (self.root / "completed.json").exists():
            return self.finish_local()
        if not (self.root / "custody.json").exists():
            self.remove_unprepared()
        before = self.evidence()
        if before["cleaned"]:
            self.reap()
            return before
        self.phase = "cleanup"
        self.cleanup(False)
        return self.finish(False)

    def remove_unprepared(self):
        # A kill before custody was committed cannot have published or booted
        # our image. Still prove absence at the external boundaries: missing
        # local custody alone is never permission to stop a running machine.
        self.prove_unprepared()
        records.write(
            self.root / "custody.json",
            canonical_json.canonical({"binding": self.binding, "token": secrets.token_hex(32), "reportToken": secrets.token_hex(32)}),
        )
        return self.finish(False)

    def prove_unprepared(self):
        self.phase = "controller"
        client = self.client()
        if self.target(client) != "Off" or client.inserted():
            raise ValueError("missing discovery custody has unresolved external ownership")
        parent = Path(self.request["private"]["path"])
        if parent.exists():
            records.directory(parent, False, None)
            if next(parent.iterdir(), None) is not None:
                raise ValueError("missing discovery custody has unresolved publication")


def publish_image(source, destination, maximum):
    stage = destination.parent / (".stage-" + secrets.token_hex(16))
    source_fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    target_fd = None
    try:
        info = os.fstat(source_fd)
        if not stat.S_ISREG(info.st_mode):
            raise ValueError("discovery publication file")
        if not 0 < info.st_size <= maximum:
            raise ValueError("discovery publication size")
        target_fd = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o640)
        while chunk := os.read(source_fd, 1 << 20):
            if os.write(target_fd, chunk) != len(chunk):
                raise ValueError("discovery publication copy")
        os.fchmod(target_fd, 0o640)
        os.fsync(target_fd)
        os.replace(stage, destination)
        directory = os.open(destination.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        os.close(source_fd)
        if target_fd is not None:
            os.close(target_fd)
        stage.unlink(missing_ok=True)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def listener_probe(url, certificate):
    context, unused = tls_trust.context(certificate)
    del unused
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=context), NoRedirect())
    request = urllib.request.Request(url, headers={"Range": "bytes=0-0"})
    with absolute_deadline.limited(30):
        with opener.open(request, timeout=30) as response:
            if response.status not in (200, 206) or len(response.read(1)) != 1:
                raise ValueError("private discovery listener unavailable")
