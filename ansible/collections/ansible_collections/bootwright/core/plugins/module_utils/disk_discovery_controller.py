"""Private custody, BMC inventory and bounded RAM-boot pull for disk discovery."""

from __future__ import annotations

import base64
import errno
import fcntl
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import secrets
import ssl
import stat
import time
import urllib.request
from itertools import islice

from ansible_collections.bootwright.core.plugins.module_utils import absolute_deadline
from ansible_collections.bootwright.core.plugins.module_utils import canonical_json
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_media as media
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_boot as boot
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_bmc as bmc
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_progress as progress
from ansible_collections.bootwright.core.plugins.module_utils import tls_trust
from ansible_collections.bootwright.core.plugins.module_utils import hosted_area
from ansible_collections.bootwright.core.plugins.module_utils import private_records as records
from ansible_collections.bootwright.core.plugins.module_utils import redfish_control
from ansible_collections.bootwright.core.plugins.module_utils import redfish_discovery

ROOT = Path(hosted_area.ROOT)
VERSION = "os-disk-discovery-v1"
MAX_REPORT = 48 << 10
ENV = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C"}
PORT = 9443
REFUSED = b"inventory-refused"
POLL_SECONDS = 5
OWNERSHIP_SECONDS = 60
BOOT_GROUPS = ("build-media", "publish-media", "verify-delivery", "insert-media", "select-boot", "power-on")


def validate(request, digest, binding):
    keys = {"version", "identity", "bootMedia", "controller", "interfaces", "inventory", "network", "private", "deadlineSeconds", "placement"}
    if set(request) != keys or request["version"] != VERSION or re.fullmatch(r"[0-9a-f]{64}", digest) is None:
        raise ValueError("discovery request shape")
    if request["inventory"] not in ("automatic", "boot"):
        raise ValueError("discovery request shape")
    if (
        request["controller"].get("tlsVerify") is not True
        or request["controller"].get("tlsVerification") == "none"
        or not request["controller"]["endpoint"].startswith("https://")
    ):
        raise ValueError("discovery controller TLS")
    if (request["controller"]["virtualMedia"]["trust"] not in ("established", "importCertificate", "disableVerification")
            or not request["private"]["url"].startswith("https://")):
        raise ValueError("discovery private media TLS")
    parts = Path(request["private"]["path"]).parts
    if (len(parts) != 13 or parts[:7] != ("/", "var", "lib", "bootwright-hosted", binding["context"], "services", "artifact-server")
            or re.fullmatch(r"[a-z0-9][a-z0-9-]{0,62}", parts[7]) is None
            or parts[8:] != ("public", "private", "os", binding["machine"], "discovery")
            or request["placement"]["connection"] != "local"):
        raise ValueError("discovery publication path")
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
    def __init__(self, request, digest, material, create=False, progress=None):
        self.phase = "custody"
        self.report_progress = progress
        self.request, self.digest, self.material = request, digest, material
        encoded = material.get("bindingBase64", "")
        if not isinstance(encoded, str) or len(encoded) > 8192:
            raise ValueError("discovery invocation")
        self.binding = json.loads(base64.b64decode(encoded, validate=True))
        validate(request, digest, self.binding)
        parts = ["machines", self.binding["machine"], "discovery", self.binding["operation"]]
        self.lock = None
        if create:
            hosted_area.ensure(self.binding["context"], parts, root=str(ROOT))
        present = create or hosted_area.prove(self.binding["context"], parts, root=str(ROOT))
        self.root = Path(hosted_area.target(self.binding["context"], parts, root=str(ROOT)))
        if not present:
            return  # Observation proves the pristine boundary without mkdir.
        self.lock = os.open(self.root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        fcntl.flock(self.lock, fcntl.LOCK_EX)

    def close(self):
        if self.lock is not None:
            os.close(self.lock)

    def progress(self, group):
        return progress.step(getattr(self, "report_progress", None), group)

    def skip(self, *groups):
        progress.skip(getattr(self, "report_progress", None), groups)

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
        if (set(report) != {"version", "source", "binding", "disks"} or report["version"] != "machine-disk-inventory-v1"
                or report["source"] not in ("linux", "bmc") or report["binding"] != self.binding):
            raise ValueError("foreign discovery report")
        if not isinstance(report["disks"], list) or not 1 <= len(report["disks"]) <= 256 or canonical_json.canonical(report) != data:
            raise ValueError("discovery report wire")
        return report

    def evidence(self):
        try:
            self.load()
        except FileNotFoundError:
            # Missing local custody alone says nothing about external effects.
            self.prove_unprepared()
            return self.unfinished()
        proof = self.completion()
        if proof is not None:
            if (self.root / "sealed.json").exists():
                seal = json.loads(records.read(self.root / "sealed.json", 4096))
                if seal != self.seal(proof):
                    raise ValueError("discovery local cleanup seal")
                return proof
            # External cleanup was durably committed. Only local sealing is
            # unfinished; never inspect an OS that may have started later.
            return self.unfinished()
        self.prove_owned_boot()
        return self.unfinished()

    def unfinished(self):
        return {"version": VERSION, "request": self.digest, "completed": False, "cleaned": False}

    def prove_owned_boot(self):
        self.phase = "controller"
        client = self.client()
        power = self.target(client)
        if power not in ("On", "Off"):
            raise ValueError("discovery target power is unknown")
        _directory, url = self.publication()
        inserted = client.inserted()
        if not redfish_discovery.image_matches(inserted, url) and (power == "On" or inserted != ""):
            raise ValueError("foreign virtual media")

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
        return state

    def booted(self):
        """A boot effect may exist only after this durable marker."""
        return (self.root / "boot.json").exists()

    def mark_boot(self):
        if not self.booted():
            records.write(self.root / "boot.json", canonical_json.canonical({"request": self.digest, "version": VERSION}))
            os.fsync(self.lock)

    def prepare_tls(self):
        if (self.root / "tls").exists():
            records.directory(self.root / "tls", False)
            return
        self.reap_tls_stages()
        self.phase = "tls"
        stage = self.root / (".tls-stage-" + secrets.token_hex(16))
        records.directory(stage, True)
        try:
            key, certificate = stage / "server-key.pem", stage / "server.pem"
            media.native([
                "/usr/bin/openssl", "req", "-x509", "-newkey", "ec", "-pkeyopt", "ec_paramgen_curve:P-256", "-nodes", "-days", "30",
                "-subj", "/CN=Bootwright RAM inventory", "-addext", "basicConstraints=critical,CA:FALSE",
                "-addext", "extendedKeyUsage=serverAuth", "-keyout", str(key), "-out", str(certificate),
            ])
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

    def read_bmc(self):
        """A qualified BMC drive inventory, durable before use, or None."""
        self.phase = "bmc-inventory"
        client = self.client()
        self.target(client)
        disks = bmc.inventory(client)
        if disks is None or len(disks) > 256:
            return None
        data = canonical_json.canonical({"version": "machine-disk-inventory-v1", "source": "bmc", "binding": self.binding, "disks": disks})
        if len(data) > MAX_REPORT:
            return None
        self.validate_report(data)
        records.write(self.root / "report.json", data)
        os.fsync(self.lock)
        return data

    def publication(self):
        state = self.load()
        parent = Path(self.request["private"]["path"])
        if not parent.is_absolute() or ".." in parent.parts:
            raise ValueError("discovery publication path")
        return parent / state["token"], self.request["private"]["url"] + "/" + state["token"] + "/discovery.iso"

    def publish(self):
        with self.progress("build-media") as built:
            self.load()
            self.phase = "publication"
            destination, unused = self.publication()
            del unused
            self.publication_directories(destination)
            reap_publication_stages(destination)
            media.reap_intermediates(self.root)
            (self.root / "discovery.iso").unlink(missing_ok=True)
            self.phase = "media"
            with media.construction(ROOT, self.root, destination, self.request["bootMedia"]["size"]):
                try:
                    image = media.build(self.request, self.root, self.payload())
                    built()
                    with self.progress("publish-media"):
                        self.phase = "publication"
                        publish_image(image, destination / "discovery.iso", media.publication_bound(self.request["bootMedia"]["size"]))
                        media.native(["/usr/bin/chcon", "--reference", str(destination.parents[4]), "--recursive", str(destination)])
                        os.fsync(self.lock)
                finally:
                    (self.root / "discovery.iso").unlink(missing_ok=True)

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

    def pull(self):
        """One authenticated GET, or None while nothing answers at the address."""
        certificate = records.read(self.root / "tls/server.pem").decode("ascii")
        context, leaf = tls_trust.context(certificate, "certificate")
        connection = tls_trust.CertificateConnection(self.request["network"]["address"], leaf, port=PORT, context=context, timeout=10)
        try:
            with absolute_deadline.limited(30):
                connection.connect()  # Pin the per-attempt leaf before the token crosses TLS.
                connection.request("GET", "/report", headers={"Authorization": "Bearer " + self.load()["reportToken"]})
                response = connection.getresponse()
                status, content = response.status, response.read(MAX_REPORT + 1)
        except ssl.SSLCertVerificationError:
            raise ValueError("a foreign service answers the discovery address") from None
        except (OSError, http.client.HTTPException):
            return None
        finally:
            connection.close()
        if len(content) > MAX_REPORT:
            raise ValueError("discovery report size")
        if status == 409 and content == REFUSED:
            raise ValueError("discovery RAM inventory refused")
        if status != 200:
            raise ValueError("a foreign service answers the discovery address")
        return content

    def owned_running(self, url):
        client = self.client()
        power = self.target(client)
        if power == "Off":
            raise ValueError("discovery target stopped before its inventory")
        if not redfish_discovery.image_matches(client.inserted(), url):
            raise ValueError("discovery lost ownership of the running boot")

    def receive(self):
        existing = self.report()
        if existing is not None:
            self.skip("await-answer", "receive-inventory")
            return existing
        unused, url = self.publication()
        del unused
        deadline = time.monotonic() + self.request["deadlineSeconds"]
        checked = time.monotonic()
        self.phase = "inventory"
        with self.progress("await-answer"):
            while True:
                answer = self.pull()
                if answer is not None:
                    break
                if time.monotonic() >= deadline:
                    raise ValueError("discovery report deadline")
                if time.monotonic() - checked >= OWNERSHIP_SECONDS:
                    self.owned_running(url)
                    checked = time.monotonic()
                time.sleep(POLL_SECONDS)
        with self.progress("receive-inventory"):
            if self.validate_report(answer)["source"] != "linux":
                raise ValueError("foreign discovery report")
            records.write(self.root / "report.json", answer)
            os.fsync(self.lock)
        return answer

    def start(self):
        self.phase = "controller"
        client = self.client()
        power = self.target(client)
        destination, url = self.publication()
        del destination
        if power == "On":
            if not self.booted() or not redfish_discovery.image_matches(client.inserted(), url):
                raise ValueError("discovery target is already running")
            self.skip(*BOOT_GROUPS)
            return  # The original RAM worker is still booting or serving.
        if power != "Off":
            raise ValueError("discovery target power is unknown")
        self.mark_boot()
        self.prepare_tls()
        self.publish()
        self.phase = "controller"
        client = self.client()
        if self.target(client) != "Off":
            raise ValueError("discovery target started before insertion")
        inserted = client.inserted()
        if inserted != "" and not redfish_discovery.image_matches(inserted, url):
            raise ValueError("foreign virtual media")
        certificate = records.read(Path(self.material["artifactCertificate"])).decode()
        self.phase = "media-delivery"
        with self.progress("verify-delivery"):
            listener_probe(url, certificate)
        self.phase = "media-insertion"
        virtual = self.request["controller"]["virtualMedia"]
        with self.progress("insert-media"):
            client.insert(url, virtual["trust"], certificate, private=True, enable_verification=virtual["enableVerification"])
        self.phase = "boot-selection"
        with self.progress("select-boot"):
            client.boot_once("Cd")
        self.phase = "power-on"
        with self.progress("power-on"):
            client.power("On", "On")

    def cleanup(self):
        """Stop only our RAM-only boot through the BMC, then eject its media."""
        self.phase = "cleanup"
        deadline = time.monotonic() + 600
        client = self.client()
        unused, url = self.publication()
        del unused
        with self.progress("power-off"):
            while self.target(client) != "Off":
                if not redfish_discovery.image_matches(client.inserted(), url):
                    raise ValueError("discovery lost ownership of the running boot")
                client.power("ForceOff", "Off")
                if time.monotonic() >= deadline:
                    raise ValueError("discovery poweroff deadline")
                time.sleep(3)
                client = self.client()
        with self.progress("eject-media"):
            inserted = client.inserted()
            if inserted != "" and not redfish_discovery.image_matches(inserted, url):
                raise ValueError("foreign virtual media during cleanup")
            virtual = self.request["controller"]["virtualMedia"]
            certificate = records.read(Path(self.material["artifactCertificate"])).decode()
            client.eject(virtual["restoreVerification"], virtual["removeCertificate"], certificate)
        directory, unused = self.publication()
        del unused
        (directory / "discovery.iso").unlink(missing_ok=True)
        if directory.exists():
            records.directory(directory, False, None)
            reap_publication_stages(directory)
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
        media.reap_intermediates(self.root)
        self.reap_tls_stages()
        tls = self.root / "tls"
        if tls.exists():
            records.directory(tls, False)
            for name in ("server-key.pem", "server.pem"):
                (tls / name).unlink(missing_ok=True)
            tls.rmdir()
        for name in ("discovery.iso", "patched.iso", "initrd-0", "initrd-1"):
            (self.root / name).unlink(missing_ok=True)

    def apply(self):
        if (self.root / "completed.json").exists():
            self.skip(*progress.GROUPS[:-1])
            with self.progress("seal-cleanup"):
                before = self.finish_local()
            if not before["completed"]:
                raise ValueError("discovery was removed")
            return before
        with self.progress("inspect-target"):
            self.prepare()
            before = self.evidence()
        if before["cleaned"]:
            if not before["completed"]:
                raise ValueError("discovery was removed")
            return before
        report = self.report()
        if report is None and not self.booted() and self.request["inventory"] == "automatic":
            with self.progress("read-bmc-inventory"):
                report = self.read_bmc()
        else:
            self.skip("read-bmc-inventory")
        if report is not None and not self.booted():
            # No boot effect exists: nothing to stop, eject or unpublish.
            self.skip(*BOOT_GROUPS, "await-answer", "receive-inventory", "power-off", "eject-media")
            with self.progress("seal-cleanup"):
                return self.finish(True)
        if report is None:
            self.start()
        else:
            self.skip(*BOOT_GROUPS)
        self.receive()
        self.cleanup()
        with self.progress("seal-cleanup"):
            return self.finish(True)

    def destroy(self):
        # Completed discovery never inspects or stops an OS installed later.
        if (self.root / "completed.json").exists():
            self.skip("inspect-target", "power-off", "eject-media")
            with self.progress("seal-cleanup"):
                return self.finish_local()
        with self.progress("inspect-target"):
            if not (self.root / "custody.json").exists():
                self.remove_unprepared()
            before = self.evidence()
        if before["cleaned"]:
            self.reap()
            self.skip("power-off", "eject-media", "seal-cleanup")
            return before
        if self.booted():
            self.cleanup()
        else:
            self.skip("power-off", "eject-media")  # No boot effect was ever started.
        with self.progress("seal-cleanup"):
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
        if self.target(client) != "Off" or client.inserted() != "":
            raise ValueError("missing discovery custody has unresolved external ownership")
        parent = Path(self.request["private"]["path"])
        if parent.exists():
            records.directory(parent, False, None)
            if next(parent.iterdir(), None) is not None:
                raise ValueError("missing discovery custody has unresolved publication")


def publish_image(source, destination, maximum):
    stage = destination.parent / (".stage-" + secrets.token_hex(16))
    source_fd = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    target_fd = None
    try:
        info = os.fstat(source_fd)
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            raise ValueError("discovery publication file")
        if not 0 < info.st_size <= maximum:
            raise ValueError("discovery publication size")
        try:
            os.rename(source, stage)
        except OSError as error:
            if error.errno != errno.EXDEV:
                raise
            target_fd = copy_image(source_fd, stage, info.st_size)
        completed = source_fd if target_fd is None else target_fd
        os.fchmod(completed, 0o640)
        os.fsync(completed)
        os.replace(stage, destination)
        directory = os.open(destination.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
        source.unlink(missing_ok=True)
        directory = os.open(source.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        os.close(source_fd)
        if target_fd is not None:
            os.close(target_fd)
        stage.unlink(missing_ok=True)


def copy_image(source_fd, stage, size):
    capacity = os.statvfs(stage.parent)
    if capacity.f_bavail * (capacity.f_frsize or capacity.f_bsize) < size:
        raise OSError(errno.ENOSPC, "discovery publication capacity")
    fd = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o640)
    try:
        remaining = size
        while remaining:
            chunk = os.read(source_fd, min(remaining, 1 << 20))
            if not chunk:
                raise ValueError("discovery publication size")
            remaining -= len(chunk)
            pending = memoryview(chunk)
            while pending:
                written = os.write(fd, pending)
                if written <= 0:
                    raise ValueError("discovery publication copy")
                pending = pending[written:]
        if os.read(source_fd, 1):
            raise ValueError("discovery publication size")
        return fd
    except BaseException:
        os.close(fd)
        raise


def reap_publication_stages(directory):
    with os.scandir(directory) as entries:
        names = [entry.name for entry in islice(entries, 4097)]
    if len(names) > 4096:
        raise ValueError("discovery publication entry bound")
    for name in names:
        if re.fullmatch(r"\.stage-[0-9a-f]{32}", name) is None:
            continue
        path = directory / name
        info = path.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1:
            raise ValueError("discovery publication stage")
        path.unlink()


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
