"""Private erase custody and pinned RAM receipt transport on the execution host."""

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

from ansible_collections.bootwright.core.plugins.module_utils import managedos_guest_ssh as ssh
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_boot as boot
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_process as process
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_redfish as redfish
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker
from ansible_collections.bootwright.core.plugins.module_utils import ssh_key_policy

ROOT = Path("/var/lib/bootwright-erase")
MEDIA = Path("/var/lib/bootwright/media")


def native(argv, timeout=60):
    code, output = process.run(argv, timeout=timeout)
    if code:
        raise worker.Refusal("native-command")
    return output


def read(path, limit=32768):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022 or info.st_size > limit:
            raise worker.Refusal("custody-file")
        data = os.read(fd, limit + 1)
        if len(data) > limit:
            raise worker.Refusal("custody-size")
        return data
    finally:
        os.close(fd)


def write(path, data, mode=0o600):
    stage = path.parent / (".stage-" + secrets.token_hex(16))
    fd = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, mode)
    try:
        if os.write(fd, data) != len(data):
            raise worker.Refusal("custody-write")
        os.fsync(fd)
        os.link(stage, path, follow_symlinks=False)
    finally:
        os.close(fd)
        stage.unlink()
    parent = os.open(str(Path(path).parent), os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(parent)
    finally:
        os.close(parent)


def directory(path, create, mode=0o700):
    # Every ancestor must be a real root-owned directory, never a symlink or
    # writable by another principal. Only the final component may be created.
    if create:
        try:
            os.mkdir(path, mode)
        except FileExistsError:
            pass
    for ancestor in [path, *path.parents]:
        info = ancestor.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022:
            raise worker.Refusal("custody-directory")
    if mode is not None and stat.S_IMODE(path.stat().st_mode) != mode:
        raise worker.Refusal("custody-mode")


class Controller:
    def __init__(self, request, digest, material, create=False):
        self.request, self.digest, self.material = request, digest, material
        if request.get("version") != worker.VERSION or not re.fullmatch("[0-9a-f]{64}", digest):
            raise worker.Refusal("request")
        identity = request["install"]["identity"]
        if not all(re.fullmatch("[a-z0-9][a-z0-9-]*", identity[key]) for key in ("context", "object")):
            raise worker.Refusal("identity")
        operation = material.get("applyOperation", "")
        if not re.fullmatch("op-[0-9a-f]{32}", operation):
            raise worker.Refusal("apply-operation")
        self.root = ROOT / identity["context"] / identity["object"] / operation
        for path in (ROOT, self.root.parent.parent, self.root.parent, self.root):
            directory(path, create)
        self.lock = os.open(self.root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        # Every phase can recover or finalize custody. One owner holds its
        # lock across the complete call, including reads that decide to write.
        fcntl.flock(self.lock, fcntl.LOCK_EX)
        self.worker_request = None

    def close(self):
        os.close(self.lock)

    def load(self):
        state = json.loads(read(self.root / "custody.json"))
        if state["request"] != self.digest or state["applyOperation"] != self.material["applyOperation"]:
            raise worker.Refusal("foreign-custody")
        uuid = base64.b64decode(self.material["pinnedUUIDBase64"], validate=True).decode("ascii").strip().lower()
        macs = sorted(nic["macAddress"].lower() for nic in self.request["install"]["target"]["hardware"]["interfaces"])
        self.worker_request = {
            "version": worker.VERSION,
            "originalAttempt": state["originalAttempt"],
            "request": self.digest,
            "uuid": uuid,
            "macs": macs,
            "targets": self.request["targets"],
            "deadlineSeconds": self.request["deadlineSeconds"],
        }
        worker.validate_request(self.worker_request)
        if state["worker"] != self.worker_request:
            raise worker.Refusal("foreign-target")
        return state

    def prepare(self):
        path = self.root / "custody.json"
        if not path.exists():
            self.worker_request = {
                "version": worker.VERSION,
                "originalAttempt": self.material["originalAttempt"],
                "request": self.digest,
                "uuid": base64.b64decode(self.material["pinnedUUIDBase64"], validate=True).decode("ascii").strip().lower(),
                "macs": sorted(nic["macAddress"].lower() for nic in self.request["install"]["target"]["hardware"]["interfaces"]),
                "targets": self.request["targets"],
                "deadlineSeconds": self.request["deadlineSeconds"],
            }
            worker.validate_request(self.worker_request)
            state = {
                "request": self.digest,
                "applyOperation": self.material["applyOperation"],
                "originalAttempt": self.material["originalAttempt"],
                "token": secrets.token_hex(32),
                "worker": self.worker_request,
            }
            write(path, worker.canonical(state))
        state = self.load()
        key = self.root / "host-key"
        if not key.exists():
            native(["/usr/bin/ssh-keygen", "-q", "-t", "ecdsa", "-b", "256", "-N", "", "-f", str(key)])
        private_key = read(key).decode("ascii")
        descriptor = os.open(key, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
        os.fsync(self.lock)
        public_key = native(["/usr/bin/ssh-keygen", "-y", "-f", str(key)]).decode("ascii").strip()
        policy = ssh_key_policy.effective_policy(ssh_key_policy.read_policy())
        ssh_key_policy.check_keys(
            policy,
            [
                {"secret": "erase-host", "type": "ecdsa-sha2-nistp256", "usage": "host"},
                {"secret": "erase-fleet", "type": self.material["authorizedKey"].split()[0], "usage": "fleet"},
            ],
        )
        for name, data in (("ssh_config", policy["configuration"]), ("known_hosts", self.request["install"]["address"] + " " + public_key + "\n")):
            path = self.root / name
            if path.exists():
                if read(path).decode() != data:
                    raise worker.Refusal("custody-policy-drift")
            else:
                write(path, data.encode())
        ks = boot.kickstart(self.request["network"], self.worker_request, private_key, self.material["authorizedKey"])
        path = self.root / "ks.cfg"
        if path.exists():
            if read(path, 262144).decode() != ks:
                raise worker.Refusal("custody-image-drift")
        else:
            write(path, ks.encode())
        publication = self.request["install"]["private"]
        if not re.fullmatch("[0-9a-f]{64}", state["token"]):
            raise worker.Refusal("custody-token")
        receipt = self.receipt()
        running = False
        if receipt is None:
            answer = self.session("receipt")
            if answer.code == 0 and answer.stdout:
                observed = json.loads(answer.stdout)
                if observed.get("request") != self.digest or observed.get("originalAttempt") != state["originalAttempt"]:
                    raise worker.Refusal("foreign-worker")
                if observed.get("state") == "complete":
                    worker.validate_receipt(self.worker_request, observed)
                    write(self.root / "receipt.json", worker.canonical(observed))
                    receipt = observed
                elif observed.get("state") == "partial":
                    running = True
                else:
                    raise worker.Refusal("worker-failed")
        return {
            "work": str(self.root),
            "private": publication["path"] + "/" + state["token"],
            "url": publication["url"] + "/" + state["token"] + "/erase.iso",
            "receipt": receipt,
            "running": running,
        }

    def build(self):
        state = self.load()
        source = MEDIA / self.request["install"]["bootMedia"]["name"]
        if source.parent != MEDIA:
            raise worker.Refusal("media-identity")
        directory(MEDIA, False, None)
        media = self.request["install"]["bootMedia"]
        descriptor = os.open(source, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            info = os.fstat(descriptor)
            if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022 or info.st_size != media["size"]:
                raise worker.Refusal("media-identity")
            digest = hashlib.sha256()
            while chunk := os.read(descriptor, 1024 * 1024):
                digest.update(chunk)
            if digest.hexdigest() != media["sha256"]:
                raise worker.Refusal("media-identity")
            os.lseek(descriptor, 0, os.SEEK_SET)
            image = self.root / "erase.iso"
            if image.exists():
                image.unlink()
            arguments = self.request["install"]["installerArguments"] + [
                "bootwright.erase=1",
                "inst.text",
                "inst.noninteractive",
                "rd.live.ram=1",
                "rd.live.overlay.overlayfs=1",
                "inst.zram=0",
            ]
            # Lorax passes this absolute path unchanged to its subprocesses.
            # Refer to our still-held FD, so neither mkksiso nor xorriso can
            # re-open a replaced operator pathname after verification.
            held = "/proc/" + str(os.getpid()) + "/fd/" + str(descriptor)
            native(
                [
                    "/usr/bin/mkksiso",
                    "--ks",
                    str(self.root / "ks.cfg"),
                    "--cmdline",
                    " ".join(arguments),
                    "--rm-args",
                    "rd.live.check rd.live.ram rd.live.overlay rd.live.overlay.overlayfs rd.overlay rootflags rd.writable.fsimg inst.zram inst.sshd",
                    held,
                    str(image),
                ],
                timeout=900,
            )
        finally:
            os.close(descriptor)
        publication = Path(self.request["install"]["private"]["path"])
        directory(publication, False, 0o711)
        private = publication / state["token"]
        directory(private, False, 0o711)
        destination = private / "erase.iso"
        # Build beneath 0700 custody with umask 077; publish only with the
        # final private mode, never a transient world-readable image.
        stage = private / (".stage-" + secrets.token_hex(16))
        out = os.open(stage, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o640)
        try:
            with image.open("rb") as stream:
                while chunk := stream.read(1024 * 1024):
                    if os.write(out, chunk) != len(chunk):
                        raise worker.Refusal("image-copy")
            os.fsync(out)
            os.replace(stage, destination)
            parent = os.open(private, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
            try:
                os.fsync(parent)
            finally:
                os.close(parent)
        finally:
            os.close(out)
            stage.unlink(missing_ok=True)
        image.unlink()

    def receipt(self):
        self.load()
        try:
            receipt = json.loads(read(self.root / "receipt.json"))
        except FileNotFoundError:
            return None
        worker.validate_receipt(self.worker_request, receipt)
        return receipt

    def session(self, command, data=b""):
        policy = ssh_key_policy.effective_policy(ssh_key_policy.read_policy())
        argv = ssh.guest_argv(
            ssh_config=str(self.root / "ssh_config"),
            known_hosts=str(self.root / "known_hosts"),
            host_key_algorithms="ecdsa-sha2-nistp256",
            policy_host_key_algorithms=policy["hostKeyAlgorithms"],
            identity=self.material["fleetIdentity"],
            user="root",
            address=self.request["install"]["address"],
            remote=command,
        )
        code, output = process.run(argv, data, timeout=30, limit=32768)
        return ssh.Outcome(code, output, False, False)

    def await_receipt(self):
        self.load()
        existing = self.receipt()
        if existing:
            return existing
        deadline = time.monotonic() + self.request["deadlineSeconds"] + 900
        while time.monotonic() < deadline:
            result = self.session("receipt")
            if result.code == 0 and result.stdout:
                receipt = json.loads(result.stdout)
                if receipt.get("state") == "complete":
                    worker.validate_receipt(self.worker_request, receipt)
                    write(self.root / "receipt.json", worker.canonical(receipt))
                    os.fsync(self.lock)
                    return receipt
                if receipt.get("state") != "partial":
                    raise worker.Refusal("worker-failed")
                if receipt.get("request") != self.digest or receipt.get("originalAttempt") != self.worker_request["originalAttempt"]:
                    raise worker.Refusal("foreign-worker")
            time.sleep(5)
        raise worker.Refusal("receipt-timeout")

    def acknowledge(self):
        receipt = self.receipt()
        if not receipt:
            raise worker.Refusal("receipt-missing")
        digest = hashlib.sha256(worker.canonical(receipt)).hexdigest().encode()
        result = self.session("ack", digest)
        # A lost reply cannot prove power-off; the role must independently
        # inspect the controller before it can report removal completed.
        return result.code == 0

    def control(self, phase):
        state = self.load()
        config = self.request["install"]["target"]["controller"]
        user = read(Path(self.material["controllerUser"])).decode()
        password = read(Path(self.material["controllerPassword"])).decode()
        ca = read(Path(self.material["controllerCA"])).decode() if self.material.get("controllerCA") else ""
        certificate = read(Path(self.material["artifactCertificate"])).decode()

        def factory(seconds):
            return redfish.Client(config["endpoint"], user, password, ca, time.monotonic() + seconds)

        if phase == "start":
            image = self.request["install"]["private"]["url"] + "/" + state["token"] + "/erase.iso"
            redfish.start(factory, self.worker_request, image, config["virtualMedia"]["trust"], certificate)
        elif phase == "stop-proof":
            if self.receipt() is None:
                raise worker.Refusal("receipt-missing")
            redfish.finish(factory, self.worker_request, config["virtualMedia"], certificate)
        else:
            raise worker.Refusal("controller-phase")

    def finish(self, evidence):
        state = self.load()
        if not re.fullmatch("[0-9a-f]{64}", state["token"]):
            raise worker.Refusal("custody-token")
        publication = Path(self.request["install"]["private"]["path"])
        directory(publication, False, None)
        try:
            (publication / state["token"]).lstat()
        except FileNotFoundError:
            pass
        else:
            raise worker.Refusal("private-publication")
        descriptor = os.open(publication, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
        marker = self.root / "finalizing.json"
        if marker.exists():
            if json.loads(read(marker)) != evidence:
                raise worker.Refusal("completion-drift")
        else:
            write(marker, worker.canonical(evidence))
        for name in ("host-key", "host-key.pub", "ks.cfg", "ssh_config", "known_hosts"):
            (self.root / name).unlink(missing_ok=True)
        os.fsync(self.lock)
        completed = self.root / "completed.json"
        if completed.exists():
            if json.loads(read(completed)) != evidence:
                raise worker.Refusal("completion-drift")
        else:
            write(completed, worker.canonical(evidence))
