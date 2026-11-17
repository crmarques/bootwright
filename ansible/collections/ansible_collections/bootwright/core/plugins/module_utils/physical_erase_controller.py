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
import shutil
import stat
import time

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import private_records
from ansible_collections.bootwright.core.plugins.module_utils import hosted_area
from ansible_collections.bootwright.core.plugins.module_utils import managedos_guest_ssh as ssh
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_boot as boot
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_redfish as redfish
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker
from ansible_collections.bootwright.core.plugins.module_utils import ssh_key_policy

ROOT = Path(hosted_area.ROOT)
REQUEST_VERSION = "physical-erase-v1"
MEDIA = Path("/var/lib/bootwright/media")
METADATA_CLEAR = "metadataClear"
RETAIN = "retain"
# The custody files only a booting erase needs: its host key, the kickstart
# that embeds it, the worker session's SSH files and the unpublished image.
BOOT_PRIVATE = ("host-key", "host-key.pub", "ks.cfg", "ssh_config", "known_hosts", "erase.iso")
TOKEN = "[0-9a-f]{64}"
# The superseded records of at most this many earlier removals are kept.
SUPERSEDED_KEPT = 4
SUPERSEDED = re.compile(r"superseded-([A-Za-z0-9-]{1,128})-(?:receipt|finalizing|completed)\.json")


def relaxation(material):
    """The erase relaxation Go froze in this removal's block, or '' for none.

    Go hands it as relaxationBase64 only when the removal carries one; any
    value other than the canonical encoding of exactly metadataClear or
    retain refuses, so a damaged value never reads as no relaxation.
    """
    if "relaxationBase64" not in material:
        return ""
    encoded = material["relaxationBase64"]
    if isinstance(encoded, str):
        try:
            decoded = base64.b64decode(encoded, validate=True).decode("ascii")
        except ValueError:
            decoded = ""
        if decoded in (METADATA_CLEAR, RETAIN) and base64.b64encode(decoded.encode("ascii")).decode("ascii") == encoded:
            return decoded
    raise worker.Refusal("relaxation")


# ssh's exit 255 is a session that reached no worker, any other exit an
# answer. The closed failure classes of an unanswered session, read from ssh's own
# standard error, which never leaves this function.
SESSION_CAUSES = (
    (b"Connection timed out", "timeout"),
    (b"Operation timed out", "timeout"),
    (b"Connection refused", "refused"),
    (b"No route to host", "no-route"),
    (b"Network is unreachable", "no-route"),
    (b"Host key verification failed", "host-key"),
    (b"REMOTE HOST IDENTIFICATION HAS CHANGED", "host-key"),
    (b"Permission denied", "auth"),
)


def contact(argv, data=b""):
    """One bounded receipt session: its code and output, and the closed class of a failure."""
    try:
        code, output, errors = bounded_process.run(argv, data, 30, 32768 + 65536, environment=dict(worker.ENV), cwd="/", umask=0o077)
    except bounded_process.Failure as failure:
        return ssh.Outcome(None, b"", False, failure.kind == "timeout", "timeout" if failure.kind == "timeout" else "exit")
    if len(output) > 32768:
        return ssh.Outcome(None, b"", False, False, "exit")
    cause = None if code != 255 else next((name for marker, name in SESSION_CAUSES if marker in errors), "exit")
    return ssh.Outcome(code, output, False, False, cause)


def removal(attempt):
    """The removal operation an original attempt <operation>/<block>/<attempt> names, or ''."""
    parts = attempt.split("/") if isinstance(attempt, str) else []
    return parts[0] if len(parts) == 3 and re.fullmatch("[A-Za-z0-9-]{1,128}", parts[0]) else ""


def native(argv, timeout=60):
    code, output = worker.native(argv, timeout=timeout)
    if code:
        raise worker.Refusal("native-command")
    return output


def read(path, limit=32768):
    try:
        return private_records.read(path, limit)
    except private_records.Refused as refused:
        raise worker.Refusal(str(refused)) from None


def write(path, data, mode=0o600):
    try:
        private_records.write(path, data, mode)
    except private_records.Refused as refused:
        raise worker.Refusal(str(refused)) from None


def directory(path, create, mode=0o700):
    try:
        private_records.directory(path, create, mode)
    except private_records.Refused as refused:
        raise worker.Refusal(str(refused)) from None


class Controller:
    def __init__(self, request, digest, material, create=False):
        self.request, self.digest, self.material = request, digest, material
        if request.get("version") != REQUEST_VERSION or not re.fullmatch("[0-9a-f]{64}", digest):
            raise worker.Refusal("request")
        identity = request["install"]["identity"]
        if not all(re.fullmatch("[a-z0-9][a-z0-9-]*", identity[key]) for key in ("context", "object")):
            raise worker.Refusal("identity")
        operation = material.get("applyOperation", "")
        if not re.fullmatch("op-[0-9a-f]{32}", operation):
            raise worker.Refusal("apply-operation")
        path = ROOT
        directory(path, create)
        for name in (identity["context"], "machines", identity["object"], "erase", operation):
            path = path / name
            directory(path, create)
        self.root = path
        self.lock = os.open(self.root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        # Every phase can recover or finalize custody. One owner holds its
        # lock across the complete call, including reads that decide to write.
        fcntl.flock(self.lock, fcntl.LOCK_EX)
        self.worker_request = None

    def close(self):
        os.close(self.lock)

    def worker_for(self, original_attempt):
        """The RAM worker's request: the frozen targets, the original identity and the root's WWN and serial number.

        Go names the root's WWN: the one its declaration names or, for a root
        declared by serial number, the one it reported at installation. A
        removal relaxed to metadataClear clears only the metadata of every
        target, so its custody, kickstart, receipt and progress all hold the
        relaxed targets; a retained erase boots no worker and has none.
        """
        method = relaxation(self.material)
        if method == RETAIN:
            raise worker.Refusal("relaxation")
        targets = self.request["targets"]
        if method == METADATA_CLEAR:
            targets = [dict(target, method=METADATA_CLEAR) for target in targets]
        hardware = self.request["install"]["target"]["hardware"]
        try:
            wwn = base64.b64decode(self.material["rootWWNBase64"], validate=True).decode("ascii")
        except (KeyError, ValueError):
            raise worker.Refusal("root") from None
        return {
            "version": worker.VERSION,
            "originalAttempt": original_attempt,
            "request": self.digest,
            "uuid": base64.b64decode(self.material["pinnedUUIDBase64"], validate=True).decode("ascii").strip().lower(),
            "macs": sorted(nic["macAddress"].lower() for nic in hardware["interfaces"]),
            "targets": targets,
            "deadlineSeconds": self.request["deadlineSeconds"],
            "root": {"wwn": wwn, "serialNumber": hardware.get("serialNumber", "")},
        }

    def load(self):
        state = json.loads(read(self.root / "custody.json"))
        if state["request"] != self.digest or state["applyOperation"] != self.material["applyOperation"]:
            raise worker.Refusal("foreign-custody")
        self.worker_request = self.worker_for(state["originalAttempt"])
        worker.validate_request(self.worker_request)
        if state["worker"] != self.worker_request:
            raise worker.Refusal("foreign-target")
        return state

    def earlier(self):
        """Whether custody names another removal's attempt than this removal's material."""
        state = json.loads(read(self.root / "custody.json"))
        return removal(state.get("originalAttempt")) != removal(self.material["originalAttempt"])

    def supersede(self):
        """Set an earlier removal's custody aside before this removal writes its own.

        Custody belongs to one removal attempt lineage, so a fresh removal
        always erases under its own: an earlier removal's receipt and its
        finalizing and completed records, whether it failed, was replaced or
        finished, are kept as superseded-<operation>-<record>.json and never
        adopted, and its private image and boot-private key and files are
        withdrawn. A retry of the same removal keeps its custody unchanged.
        """
        path = self.root / "custody.json"
        if not path.exists():
            return
        state = json.loads(read(path))
        earlier = removal(state.get("originalAttempt"))
        if earlier == removal(self.material["originalAttempt"]):
            return
        if state.get("request") != self.digest or state.get("applyOperation") != self.material["applyOperation"]:
            raise worker.Refusal("foreign-custody")
        if not re.fullmatch(TOKEN, str(state.get("token"))) or not earlier:
            raise worker.Refusal("custody-token")
        self.unpublish(state["token"])
        for name in BOOT_PRIVATE:
            (self.root / name).unlink(missing_ok=True)
        self.set_aside(earlier)

    def unpublish(self, token):
        """Withdraw the private publication <private.path>/<token>; whether one was there."""
        publication = Path(self.request["install"]["private"]["path"])
        private = publication / token
        try:
            info = private.lstat()
        except FileNotFoundError:
            return False
        if not stat.S_ISDIR(info.st_mode):
            raise worker.Refusal("custody-token")
        shutil.rmtree(private)
        descriptor = os.open(publication, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
        return True

    def set_aside(self, earlier):
        """Keep an earlier removal's records as superseded-<operation>-<record>.json, then drop its custody record.

        The completion record goes last, so an interrupted supersession is
        never read as this removal's completed or finalizing custody. Each
        record set aside is stamped later than every one kept before it, and
        only the SUPERSEDED_KEPT removals set aside last keep theirs.
        """
        stamp = max([time.time_ns()] + [moment + 1 for moment in self.superseded().values()])
        for record in ("receipt", "finalizing", "completed"):
            kept = self.root / (record + ".json")
            if kept.exists():
                target = self.root / ("superseded-" + earlier + "-" + record + ".json")
                os.replace(kept, target)
                os.utime(target, ns=(stamp, stamp), follow_symlinks=False)
        ranked = sorted(self.superseded().items(), key=lambda item: item[1], reverse=True)
        for operation, _moment in ranked[SUPERSEDED_KEPT:]:
            for record in ("receipt", "finalizing", "completed"):
                (self.root / ("superseded-" + operation + "-" + record + ".json")).unlink(missing_ok=True)
        os.fsync(self.lock)
        (self.root / "custody.json").unlink()
        os.fsync(self.lock)

    def superseded(self):
        """Each earlier removal whose records are set aside, with the latest stamp among them."""
        found = {}
        for path in self.root.iterdir():
            match = SUPERSEDED.fullmatch(path.name)
            if match:
                moment = path.lstat().st_mtime_ns
                found[match.group(1)] = max(found.get(match.group(1), moment), moment)
        return found

    def custody(self):
        """The custody record's token and removal, and whether that removal is another than this one's, or None without one.

        A record naming no valid token or original attempt refuses: its
        private publication could not be found to withdraw.
        """
        mine = removal(self.material.get("originalAttempt"))
        if not mine:
            raise worker.Refusal("original-attempt")
        path = self.root / "custody.json"
        if not path.exists():
            return None
        state = json.loads(read(path))
        token = state.get("token") if isinstance(state, dict) else None
        holder = removal(state.get("originalAttempt")) if isinstance(state, dict) else ""
        if not isinstance(token, str) or not re.fullmatch(TOKEN, token) or not holder:
            raise worker.Refusal("custody-token")
        return {"token": token, "removal": holder, "earlier": holder != mine}

    def withdraw(self):
        """Withdraw a retained erase's leftover private publication and boot-private custody; whether anything was withdrawn.

        A retained erase never boots its worker or reaches the management
        controller, and acts only on the custody of this block's apply
        operation: the private publication its token names and the
        boot-private files. Custody naming another removal's attempt serves
        no live removal, so it is withdrawn the same way and its records set
        aside as that removal's superseded records, never adopted. The
        request and apply-operation checks of a booting removal are waived
        with the erase, as the custody refusals' retain remedy promises.
        """
        state = self.custody()
        changed = self.unpublish(state["token"]) if state else False
        for name in BOOT_PRIVATE:
            try:
                (self.root / name).unlink()
            except FileNotFoundError:
                continue
            changed = True
        os.fsync(self.lock)
        if state and state["earlier"]:
            self.set_aside(state["removal"])
            changed = True
        return changed

    def withdrawn(self):
        """Whether a retained erase's withdrawal is proved, without changing anything.

        It is proved when no custody names another removal, no private
        publication of this custody's token remains and no boot-private file
        is left.
        """
        state = self.custody()
        if state and (state["earlier"] or os.path.lexists(Path(self.request["install"]["private"]["path"]) / state["token"])):
            return False
        return not any(os.path.lexists(self.root / name) for name in BOOT_PRIVATE)

    def prepare(self):
        self.supersede()
        path = self.root / "custody.json"
        if not path.exists():
            self.worker_request = self.worker_for(self.material["originalAttempt"])
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
            remote_argv=[command],
        )
        return contact(argv, data)

    def poll(self):
        """One bounded snapshot of the wait for the RAM worker's receipt.

        A durable or newly reported complete receipt is validated and kept,
        and answers complete with it; a partial status of this request and
        attempt answers partial with its progress; a session that reached no
        worker answers unanswered with its closed failure class; any other
        exit is an answer that reported nothing. A stopped worker and one of
        another request or attempt refuse. The wait itself, its clock and
        its worker-unreachable and receipt-timeout bounds belong to the
        physical_erase_await action, which calls this once per round.
        """
        self.load()
        existing = self.receipt()
        if existing:
            return {"answer": "complete", "receipt": existing}
        result = self.session("receipt")
        if result.code is None or result.code == 255:
            return {"answer": "unanswered", "cause": result.cause or "exit"}
        if result.code != 0 or not result.stdout:
            return {"answer": "answered"}
        receipt = json.loads(result.stdout)
        if receipt.get("state") == "complete":
            worker.validate_receipt(self.worker_request, receipt)
            write(self.root / "receipt.json", worker.canonical(receipt))
            os.fsync(self.lock)
            return {"answer": "complete", "receipt": receipt}
        if receipt.get("state") != "partial":
            raise worker.Refusal("worker-failed")
        if receipt.get("request") != self.digest or receipt.get("originalAttempt") != self.worker_request["originalAttempt"]:
            raise worker.Refusal("foreign-worker")
        return {"answer": "partial", "progress": self.progress(receipt.get("progress"))}

    def progress(self, reported):
        """Each target's proved bytes, written plus read back, of twice its coverage, or None.

        The worker's counts only report: a report that is not exactly one
        written and one verified count per frozen target, each an integer
        within that target's coverage, is ignored, and no count is ever
        completion evidence.
        """
        targets = self.worker_request["targets"]
        if not isinstance(reported, list) or len(reported) != len(targets):
            return None
        measured = []
        for counts, target in zip(reported, targets):
            size = worker.covered(target)
            if not isinstance(counts, dict) or set(counts) != {"written", "verified"}:
                return None
            if not all(type(counts[kind]) is int and 0 <= counts[kind] <= size for kind in counts):  # pylint: disable=unidiomatic-typecheck
                return None
            measured.append({"completed": counts["written"] + counts["verified"], "total": 2 * size})
        return measured

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
            return redfish.Client(config["endpoint"], user, password, ca, time.monotonic() + seconds, verification=config.get("tlsVerification", ""))

        if phase == "start":
            image = self.request["install"]["private"]["url"] + "/" + state["token"] + "/erase.iso"
            redfish.start(factory, self.worker_request, image, config["virtualMedia"]["trust"], certificate,
                          enable_verification=config["virtualMedia"]["enableVerification"])
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
