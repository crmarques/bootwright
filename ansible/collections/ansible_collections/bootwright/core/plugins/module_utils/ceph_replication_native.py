"""One-way RBD replication, with native bootstrap tokens confined to protected custody, worker memory and stdin."""
from __future__ import annotations

# pylint: disable=unidiomatic-typecheck
import base64
import contextlib
import hmac
import json
import os

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_native_resources
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import canonical, named, require, unique_object

VERSION = "ceph-replication-v1"
PHASES = frozenset(("replication_prepare", "replication_token", "replication_import", "replication_image",
                    "replication_pool_observe", "replication_image_observe", "replication_commit"))
# Mirror.cc logs decoded bootstrap tokens at debug_rbd=20. Override the inherited logging configuration for every call.
QUIET = ("--debug-rbd=0/0", "--debug-rados=0/0", "--debug-ms=0/0", "--log-file=/dev/null", "--log-to-stderr=false",
         "--err-to-stderr=false", "--log-to-syslog=false", "--err-to-syslog=false", "--log-to-journald=false")


def valid_uuid(value):
    return isinstance(value, str) and ceph_native.SUBVOLUME_UUID.fullmatch(value) is not None


def validate(value):
    require(isinstance(value, dict) and set(value) == {"version", "controller", "object", "mode", "images", "primary", "secondary"})
    require(value["version"] == VERSION and value["mode"] in ("image", "pool") and named(value["object"]) and named(value["controller"]))
    require(isinstance(value["images"], list) and 1 <= len(value["images"]) <= 3)
    for side in ("primary", "secondary"):
        endpoint = value[side]
        require(isinstance(endpoint, dict) and set(endpoint) == {"cluster", "machines", "pool", "poolObject", "scope"})
        scope = ceph_native.validate_scope(endpoint["scope"])
        require(endpoint["cluster"] == scope["cluster"] and named(endpoint["poolObject"]))
        require(endpoint["machines"] == sorted({host["machine"] for host in scope["hosts"]}))
        pool = endpoint["pool"]
        ceph_native_resources.validate_pool(pool, len(scope["hosts"]))
        require(pool["type"] == "replicated" and pool["role"] == "rbd" and pool.get("object", pool["name"]) == endpoint["poolObject"])
    left, right = value["primary"], value["secondary"]
    require(left["cluster"] != right["cluster"] and left["scope"]["owner"] == right["scope"]["owner"])
    require(left["pool"]["name"] == right["pool"]["name"] and left["scope"]["fsid"] != right["scope"]["fsid"]
            and not set(left["machines"]) & set(right["machines"]))
    for image in value["images"]:
        ceph_native_resources.validate_rbd(image)
        require(image["pool"] == left["pool"]["name"] and image["dataPool"] == "" and "journaling" in image["features"])
    require([image["name"] for image in value["images"]] == sorted({image["name"] for image in value["images"]}))
    return value


def token(value, fsid):
    require(isinstance(value, str) and 1 <= len(value) <= 1023 and "\n" not in value and "\r" not in value, "replication-token")
    try:
        decoded = json.loads(base64.b64decode(value, validate=True), object_pairs_hook=unique_object)
    except (ValueError, UnicodeError):
        raise ceph_native.Refusal("replication-token") from None
    require(isinstance(decoded, dict) and set(decoded) == {"fsid", "client_id", "key", "mon_host"}, "replication-token")
    require(decoded["fsid"] == fsid and decoded["client_id"] == "rbd-mirror-peer", "replication-token")
    require(all(isinstance(decoded[key], str) and decoded[key] and not any(c in decoded[key] for c in "\x00\r\n")
                for key in ("key", "mon_host")), "replication-token")
    return value


def dispatch(envelope):
    require(isinstance(envelope, dict) and set(envelope) == {"request", "host", "phase", "private", "side", "image"})
    request = validate(envelope["request"])
    side, phase, index = envelope["side"], envelope["phase"], envelope["image"]
    require(side in ("primary", "secondary") and phase in PHASES and type(index) is int and -1 <= index < len(request["images"]))
    require(envelope["host"] == request[side]["scope"]["bootstrap"])
    require(isinstance(envelope["private"], dict))
    if phase == "replication_import":
        require(side == "secondary" and set(envelope["private"]) == {"token"}, "material")
    elif phase == "replication_commit":
        require(set(envelope["private"]) == {"images"}, "material")
    else:
        require(envelope["private"] == {}, "material")
    require((index >= 0) == (phase in ("replication_image", "replication_image_observe")))
    worker = ReplicationWorker(request, side, envelope["host"])
    if phase == "replication_import":
        return worker.import_peer(token(envelope["private"]["token"], request["primary"]["scope"]["fsid"]))
    if phase == "replication_commit":
        return worker.commit(envelope["private"]["images"])
    if phase == "replication_image":
        require(side == "primary")
        return worker.enable_image(index)
    if phase == "replication_image_observe":
        return worker.observe_image(index)
    return {"replication_prepare": worker.prepare, "replication_token": worker.bootstrap_token,
            "replication_pool_observe": worker.observe_pool}[phase]()


class ReplicationWorker(ceph_native_resources.ResourceWorker):
    """ResourceWorker custody and native executables, with one durable attempt for each endpoint."""
    admit = staticmethod(ceph_native.validate_scope)

    def __init__(self, request, side, host, run=ceph_native.bounded_command, markers=None):
        self.replication = validate(request)
        self.side = side
        self.endpoint = request[side]
        super().__init__(self.endpoint["scope"], host, run, markers)

    def recovery_scope(self):
        unused, staging = ceph_native.tunnel_helpers()
        own = staging.recovery_scope(self.request)
        recorded = self.markers.read("bootstrap-scope.json")
        require(isinstance(recorded, dict) and isinstance(recorded.get("cluster"), dict), "ownership")
        cluster = dict(recorded["cluster"])
        delegated = cluster.pop("delegated", [])
        require(canonical(dict(recorded, cluster=cluster)) == canonical(own), "ownership")
        require(isinstance(delegated, list), "ownership")
        return recorded

    def intent(self):
        return {"object": self.replication["object"], "owner": self.request["owner"], "mode": self.replication["mode"],
                "primary": self.replication["primary"]["scope"]["fsid"], "secondary": self.replication["secondary"]["scope"]["fsid"],
                "pool": self.endpoint["pool"], "images": self.replication["images"]}

    def mark(self, suffix):
        return "replication-" + self.replication["object"] + "-" + suffix + ".json"

    def proved(self):
        require(self.markers.owned(), "ownership")
        require(self.ceph("fsid") == {"fsid": self.request["fsid"]}, "ownership")
        pool = self.endpoint["pool"]
        require(self.markers.read("pool-" + pool["name"] + ".json") == pool, "pool-identity")
        self.owned_pool(pool["name"])
        record = self.markers.read(self.mark("attempt"))
        require(record is None or record == self.intent(), "replication-identity")
        return record is not None

    def native(self, program, args, data=b"", secret=False, missing=False):
        """Tokens and auth reads never enter diagnostics, including malformed output and nonzero exits."""
        proof = self.bootstrap_hold() if self.request["clusterSSHTransport"] == "verified-tunnel" else contextlib.nullcontext()
        argv = ["/usr/bin/" + program, "--conf", os.path.join(self.markers.path, "ceph.conf"), "--keyring",
                os.path.join(self.markers.path, "ceph.client.admin.keyring"), *QUIET, *args]
        with proof:
            code, output = self.run(argv, data=data, timeout=60)
        if missing and code == 2:
            return None
        require(code == 0, "replication-token" if secret else "replication-unavailable")
        if secret:
            return output
        try:
            return json.loads(output, object_pairs_hook=unique_object)
        except (ValueError, UnicodeError):
            raise ceph_native.Refusal("native-output") from None

    def mirror(self):
        result = self.native("rbd", ["mirror", "pool", "info", "--format", "json", self.endpoint["pool"]["name"]])
        require(isinstance(result, dict) and result.get("mode") in ("disabled", "image", "pool"), "native-output")
        if result["mode"] != "disabled":
            require(result.get("site_name") == self.request["fsid"], "replication-identity")
        peers = result.get("peers", [])
        require(isinstance(peers, list) and len(peers) <= 1, "replication-identity")
        if self.side == "primary":
            require(not peers, "replication-identity")
        elif peers:
            peer = peers[0]
            require(isinstance(peer, dict) and peer.get("direction") == "rx-only" and peer.get("site_name") == self.replication["primary"]["scope"]["fsid"]
                    and peer.get("client_name") == "client.rbd-mirror-peer" and valid_uuid(peer.get("uuid")), "replication-identity")
            recorded = self.markers.read(self.mark("peer"))
            require(recorded is None or recorded == {"uuid": peer["uuid"]}, "replication-identity")
        return result

    def prepare(self):
        held = self.proved()
        info = self.mirror()
        if self.side == "primary" and self.replication["mode"] == "pool":
            require(sorted(self.rbd_names(self.endpoint["pool"]["name"])) == [image["name"] for image in self.replication["images"]], "replication-identity")
        if not held:
            require(info["mode"] == "disabled" and not info.get("peers"), "replication-identity")
            if self.side == "secondary":
                require(not self.rbd_names(self.endpoint["pool"]["name"]), "replication-identity")
            if self.side == "primary":
                for image in self.replication["images"]:
                    self.source_image(image)
            self.markers.write(self.mark("attempt"), self.intent())
        require(info["mode"] in ("disabled", self.replication["mode"]), "replication-identity")
        if info["mode"] == "disabled":
            self.native("rbd", ["mirror", "pool", "enable", self.endpoint["pool"]["name"], self.replication["mode"]], secret=True)
        require(self.mirror()["mode"] == self.replication["mode"], "replication-unavailable")
        return {"changed": info["mode"] == "disabled"}

    def token_custody(self):
        def decode(data):
            try:
                value = data.decode("ascii")
            except UnicodeError:
                raise ceph_native.Refusal("replication-token") from None
            return token(value, self.request["fsid"])
        return ceph_native.Custody(self, "replication", "-" + self.replication["object"], 1023, decode,
                                   version="ceph-replication-custody-v1")

    def bootstrap_token(self):
        require(self.side == "primary" and self.proved(), "replication-identity")
        credential = self.mark("credential-attempt")
        owned = self.markers.read(credential)
        require(owned is None or owned == self.intent(), "replication-identity")
        if owned is None:
            # Both names are native bootstrap_create's fixed, cluster-wide identities.
            require(self.native("ceph", ["auth", "get", "client.rbd-mirror-peer"], secret=True, missing=True) is None, "replication-identity")
            require(self.native("ceph", ["config-key", "get", "rbd/mirror/peer_client_id"], secret=True, missing=True) is None, "replication-identity")
            self.markers.write(credential, self.intent())
        raw = self.native("rbd", ["mirror", "pool", "peer", "bootstrap", "create", self.endpoint["pool"]["name"]], secret=True)
        try:
            value = raw.decode("ascii").strip()
        except UnicodeError:
            raise ceph_native.Refusal("replication-token") from None
        token(value, self.request["fsid"])
        retained = self.token_custody().prepare(self.intent(), lambda: value.encode("ascii"))
        require(hmac.compare_digest(retained, value), "replication-token")
        return {"token": retained}

    def import_peer(self, value):
        require(self.side == "secondary" and self.proved(), "replication-identity")
        info = self.mirror()
        attempt = self.markers.read(self.mark("import-attempt"))
        require(attempt is None or attempt == self.intent(), "replication-identity")
        require(attempt is not None or not info.get("peers") and not self.rbd_names(self.endpoint["pool"]["name"]), "replication-identity")
        self.markers.write(self.mark("import-attempt"), self.intent())
        if not info.get("peers"):
            self.native("rbd", ["mirror", "pool", "peer", "bootstrap", "import", "--direction", "rx-only", self.endpoint["pool"]["name"], "-"],
                        data=value.encode("ascii"), secret=True)
        peers = self.mirror().get("peers", [])
        require(len(peers) == 1, "replication-unavailable")
        self.markers.write(self.mark("peer"), {"uuid": peers[0]["uuid"]})
        return {"changed": not bool(info.get("peers"))}

    def source_image(self, image):
        info = self.rbd_info(image)
        mark, unused, identifier = self.rbd_marks(image)
        require(self.markers.read(mark) == image and self.markers.read(identifier) == {"id": info["id"]}, "replication-identity")
        require(self.image_matches(info, image), "replication-identity")
        return info

    def enable_image(self, index):
        require(self.side == "primary" and self.proved(), "replication-identity")
        image = self.replication["images"][index]
        info = self.source_image(image)
        mirroring = info.get("mirroring", {})
        if mirroring.get("state") != "enabled":
            require(self.replication["mode"] == "image", "replication-identity")
            self.native("rbd", ["mirror", "image", "enable", image["pool"] + "/" + image["name"], "journal"], secret=True)
        return {"changed": mirroring.get("state") != "enabled"}

    def observe_pool(self):
        held = self.proved()
        info = self.mirror()
        require(held or info["mode"] == "disabled", "replication-identity")
        peers = info.get("peers", [])
        if peers:
            require(self.markers.read(self.mark("import-attempt")) == self.intent(), "replication-identity")
        ready = (held and info["mode"] == self.replication["mode"]
                 and (self.side == "primary" or bool(peers) and self.markers.read(self.mark("peer")) is not None))
        return {"owned": held, "ready": ready, "completed": self.markers.read(self.mark("completed")) == self.intent()}

    def observe_image(self, index, proved=False, names=None):
        held = self.markers.read(self.mark("attempt")) is not None if proved else self.proved()
        image = self.replication["images"][index]
        result = {"name": image["name"], "id": "", "globalID": "", "synced": False}
        if self.side == "secondary" and image["name"] not in (self.rbd_names(image["pool"]) if names is None else names):
            return result
        info = self.source_image(image) if self.side == "primary" else self.rbd_info(image)
        require(held or not info.get("mirroring"), "replication-identity")
        if self.side == "secondary":
            require(self.markers.read(self.mark("import-attempt")) == self.intent(), "replication-identity")
        mirror = info.get("mirroring", {})
        if mirror.get("state") != "enabled":
            return result
        require(mirror.get("mode") == "journal" and mirror.get("primary") == (self.side == "primary")
                and self.image_matches(info, image), "replication-identity")
        status = self.native("rbd", ["mirror", "image", "status", "--format", "json", image["pool"] + "/" + image["name"]])
        require(isinstance(status, dict) and status.get("name") == image["name"] and valid_uuid(status.get("global_id")), "native-output")
        require(mirror.get("global_id") == status["global_id"], "replication-identity")
        recorded = self.markers.read(self.mark("image-" + image["name"]))
        identity = {"id": info["id"], "globalID": status["global_id"]}
        require(recorded is None or recorded == identity, "replication-identity")
        result.update({"id": info["id"], "globalID": status["global_id"], "synced": self.side == "primary" or status.get("state") == "up+replaying"})
        return result

    def commit(self, identities):
        """Re-read every native identity and synchronization status before immutable completion records."""
        require(self.proved(), "replication-identity")
        require(isinstance(identities, list) and len(identities) == len(self.replication["images"]), "request")
        result = []
        names = self.rbd_names(self.endpoint["pool"]["name"]) if self.side == "secondary" else None
        for index, identity in enumerate(identities):
            current = self.observe_image(index, proved=True, names=names)
            require(current["synced"] and current == identity, "replication-unavailable")
            result.append(current)
        for entry in result:
            self.markers.write(self.mark("image-" + entry["name"]), {"id": entry["id"], "globalID": entry["globalID"]})
        self.markers.write(self.mark("completed"), self.intent())
        return {"images": result}
