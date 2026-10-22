"""Sequential, bounded coordination of two independently proved managed Ceph clusters."""
from __future__ import annotations

# pylint: disable=unidiomatic-typecheck
import time

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_replication_native as native
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import require

EVIDENCE_VERSION = "ceph-replication-evidence-v1"
REASONS = ("request", "artifact", "ownership", "evidence", "material", "transport", "remote-refusal", "adapter-exception", "native-command",
           "native-timeout", "native-output", "unknown", "foreign-cluster", "foreign-pool", "pool-identity", "foreign-image", "image-identity",
           "replication-identity", "replication-unavailable", "replication-token", "ssh-policy-host-excluded", "ssh-policy-unavailable")


def evidence(value, digest):
    require(isinstance(value, dict) and set(value) == {"version", "request", "owner", "object", "primaryFSID", "secondaryFSID",
                                                       "owned", "exists", "ready", "images"}, "evidence")
    require(value["version"] == EVIDENCE_VERSION and value["request"] == digest and ceph_native.digest(digest), "evidence")
    require(all(ceph_native.named(value[key]) for key in ("owner", "object"))
            and all(native.valid_uuid(value[key]) for key in ("primaryFSID", "secondaryFSID")), "evidence")
    require(all(type(value[key]) is bool for key in ("owned", "exists", "ready")), "evidence")
    require(not value["ready"] or value["owned"] and value["exists"], "evidence")
    require(not value["exists"] or value["owned"], "evidence")
    images = value["images"]
    require(isinstance(images, list) and 1 <= len(images) <= 3, "evidence")
    for image in images:
        require(isinstance(image, dict) and set(image) == {"name", "globalID", "primaryID", "secondaryID", "synced"}, "evidence")
        require(ceph_native.named(image["name"]) and type(image["synced"]) is bool, "evidence")
        require(image["globalID"] == "" or native.valid_uuid(image["globalID"]), "evidence")
        for field in ("primaryID", "secondaryID"):
            require(isinstance(image[field], str) and (image[field] == "" or ceph_native.RBD_IMAGE_ID.fullmatch(image[field]) is not None), "evidence")
        require(not image["synced"] or all(image[field] for field in ("globalID", "primaryID", "secondaryID")), "evidence")
        require(not value["ready"] or image["synced"], "evidence")
    require([image["name"] for image in images] == sorted({image["name"] for image in images}), "evidence")
    return value


def execute(request, operation, digest, transports, *, now=time.monotonic, pause=time.sleep):
    request = native.validate(request)
    require(operation in ("apply", "observe") and ceph_native.digest(digest))
    hosts = {side: next(host for host in request[side]["scope"]["hosts"] if host["name"] == request[side]["scope"]["bootstrap"])
             for side in ("primary", "secondary")}

    def call(side, phase, index=-1, private=None, deadline=None):
        timeout = 660 if deadline is None else min(660, deadline - now())
        require(timeout > 0, "replication-unavailable")
        return transports[side].replication(hosts[side], phase, request, side, index, private, timeout=timeout)

    def observe(deadline=None):
        pools, images = {}, {}
        for side in ("primary", "secondary"):
            pools[side] = call(side, "replication_pool_observe", deadline=deadline)
            require(isinstance(pools[side], dict) and set(pools[side]) == {"owned", "ready", "completed"}
                    and all(type(value) is bool for value in pools[side].values()), "evidence")
            images[side] = [call(side, "replication_image_observe", index, deadline=deadline) for index in range(len(request["images"]))]
        return pools, images

    def combined(pools, images, completed):
        result = {"version": EVIDENCE_VERSION, "request": digest, "owner": request["primary"]["scope"]["owner"], "object": request["object"],
                  "primaryFSID": request["primary"]["scope"]["fsid"], "secondaryFSID": request["secondary"]["scope"]["fsid"],
                  "owned": any(pool["owned"] for pool in pools.values()), "exists": any(pool["owned"] for pool in pools.values()),
                  "ready": completed and all(pool["ready"] for pool in pools.values()), "images": []}
        for index, wanted in enumerate(request["images"]):
            pair = [images[side][index] for side in ("primary", "secondary")]
            for image in pair:
                require(isinstance(image, dict) and set(image) == {"name", "id", "globalID", "synced"}
                        and image["name"] == wanted["name"] and type(image["synced"]) is bool, "evidence")
            left, right = pair
            require(not left["globalID"] or not right["globalID"] or left["globalID"] == right["globalID"], "replication-identity")
            synced = left["synced"] and right["synced"] and left["globalID"] == right["globalID"] and bool(left["globalID"])
            result["images"].append({"name": wanted["name"], "primaryID": left["id"], "secondaryID": right["id"],
                                     "globalID": left["globalID"], "synced": synced})
            result["ready"] = result["ready"] and synced
        return evidence(result, digest)

    changed = False
    if operation == "apply":
        for side in ("primary", "secondary"):
            answer = call(side, "replication_prepare")
            require(isinstance(answer, dict) and set(answer) == {"changed"} and type(answer["changed"]) is bool, "evidence")
            changed |= answer["changed"]
        answer = call("primary", "replication_token")
        require(isinstance(answer, dict) and set(answer) == {"token"}, "replication-token")
        value = native.token(answer["token"], request["primary"]["scope"]["fsid"])
        try:
            imported = call("secondary", "replication_import", private={"token": value})
        finally:
            answer.clear()
            value = None
        require(isinstance(imported, dict) and set(imported) == {"changed"} and type(imported["changed"]) is bool, "evidence")
        changed |= imported["changed"]
        for index in range(len(request["images"])):
            answer = call("primary", "replication_image", index)
            require(isinstance(answer, dict) and set(answer) == {"changed"} and type(answer["changed"]) is bool, "evidence")
            changed |= answer["changed"]
    pools, images = observe()
    if operation == "observe":
        return {"changed": False, "evidence": combined(pools, images, all(pool["completed"] for pool in pools.values()))}
    deadline = now() + 1800
    while not combined(pools, images, True)["ready"]:
        require(now() < deadline, "replication-unavailable")
        pause(min(5, deadline - now()))
        pools, images = observe(deadline)
    for side in ("primary", "secondary"):
        answer = call(side, "replication_commit", private={"images": images[side]})
        require(isinstance(answer, dict) and set(answer) == {"images"} and answer["images"] == images[side], "evidence")
        changed |= not pools[side]["completed"]
    return {"changed": changed, "evidence": combined(pools, images, True)}
