"""A never-used cluster taken back from a kept host that stays running: its managers stopped, then the whole FSID removed from it.

A staged destroy takes a cluster back while its hosts stay, only while no access-producing block of the cluster ever completed, so no
data client was ever handed access (specs/ceph.md "Removal and concurrency"). The host keeps its packages, the pulled image, its
firewall and its operating system; every part of this FSID goes, its OSD volumes included, and so do the records kept for it,
so a later bootstrap under a new identity finds nothing of it and nothing foreign.

Before any manager stops, the bootstrap host proves the cluster unused (KeptUsage): `ceph auth ls` holds only the entities its own
bootstrap and its frozen services create, and `ceph df` shows no object and no stored byte in any pool but the manager's own `.mgr`.
The entities are closed: `mon.`, `client.admin`, cephadm's bootstrap keys `client.bootstrap-{mds,mgr,osd,rbd,rbd-mirror,rgw}`, each
`mgr.<id>`, `osd.<n>`, `client.crash.<id>` and `client.ceph-exporter.<id>` daemon key, and `mds.<id>` only while the frozen services
place an MDS. A cluster request carries no other service (gateways, NFS and NVMe-oF are blocks of their own), so a key of any other
name is a client someone created outside this cluster's teardown scope, possibly through `cluster rsh`.
"""

from __future__ import annotations

import hashlib
import os
import re
import stat

from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import (
    OSD_CLEAR_SECONDS, NativeFailure, Refusal, lvm_devices_lists, require,
)
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native_stretch import (
    DATA_ROOT, UNIT_ROOT, ManagerStop, PhysicalHostRemoval, listing, walked,
)

MAX_VOLUMES = 1024
MAX_DEPTH, MAX_ENTRIES = 3, 4096
MAX_ENTITIES, MAX_POOLS = 4096, 4096
ATTEMPT, COMPLETE = "kept-host-removal-attempt.json", "kept-host-removal-complete.json"
UNUSED = "kept-unused.json"
ZAP_PREFIX = "kept-host-zap-"
# A marker directory being discarded is first renamed to this name beside it, so no reader ever sees part of its records.
DISCARD_PREFIX = ".discard-"
ENTITIES = frozenset(("mon.", "client.admin", "client.bootstrap-mds", "client.bootstrap-mgr", "client.bootstrap-osd", "client.bootstrap-rbd",
                      "client.bootstrap-rbd-mirror", "client.bootstrap-rgw"))
DAEMON_ENTITY = re.compile(r"(mgr|client\.crash|client\.ceph-exporter)\.[A-Za-z0-9][A-Za-z0-9_.-]{0,250}\Z|osd\.(0|[1-9][0-9]{0,8})\Z")
MDS_ENTITY = re.compile(r"mds\.[A-Za-z0-9][A-Za-z0-9_.-]{0,250}\Z")
MANAGER_POOL = ".mgr"


def zap_record(path):
    return ZAP_PREFIX + hashlib.sha256(path.encode()).hexdigest() + ".json"


def tags_of(row):
    """A logical volume's tags; a tag list LVM would not write refuses."""
    value = row.get("lv_tags")
    require(isinstance(value, str), "native-output")
    tags = {}
    for tag in value.split(",") if value else []:
        key, separator, entry = tag.strip().partition("=")
        require(separator and key not in tags, "tiebreaker-foreign")
        tags[key] = entry
    return tags


def devices_of(row):
    value = row.get("devices")
    require(isinstance(value, str), "native-output")
    return [part.strip().split("(", 1)[0] for part in value.split(",") if part.strip()]


class KeptManagerStop(ManagerStop):
    """This FSID's managers on a kept host, stopped and disabled before any host's FSID removal starts, as on a physical host."""

    def __init__(self, worker):  # pylint: disable=super-init-not-called
        require(worker.request["action"] == "cluster" and worker.host in worker.request["hosts"], "request")
        self.worker = worker
        self.fsid = worker.request["fsid"]
        self.prefix = "ceph-" + self.fsid + "@mgr."
        self.unit = re.compile(re.escape(self.prefix) + r"[A-Za-z0-9][A-Za-z0-9_.-]{0,250}\.service\Z")


def removal_record(request, host):
    return {"version": "ceph-kept-host-removal-v1", "owner": request["owner"], "fsid": request["fsid"], "host": host["name"]}


def counted(stats):
    """Whether a pool's object and stored-byte counts are the non-negative integers `ceph df` reports."""
    return all(isinstance(stats.get(key), int) and not isinstance(stats[key], bool) and stats[key] >= 0 for key in ("objects", "stored"))


class KeptUsage:
    """The bootstrap host's read-only proof, before any manager stops, that the cluster holds no client or data its own steps did not create; its pass record
    lets a later attempt, whose managers may already be stopped and so cannot answer `ceph df`, go on."""

    def __init__(self, worker):
        require(worker.request["action"] == "cluster" and worker.host["name"] == worker.request["bootstrap"], "request")
        self.worker = worker
        self.request = worker.request
        self.record = removal_record(worker.request, worker.host)

    def entity_allowed(self, name):
        if name in ENTITIES or DAEMON_ENTITY.fullmatch(name) is not None:
            return True
        return MDS_ENTITY.fullmatch(name) is not None and any(service["service_type"] == "mds" for service in self.request["services"])

    def unread(self, error):
        refused = Refusal("kept-unread")
        refused.native_error = error.native_error
        return refused

    def entities(self, recorded):
        """The cluster's CephX entities, read on every attempt while a monitor answers; once this cluster's pass record exists a monitor
        that does not answer is one the teardown stopped, and None stands for its unread keys."""
        try:
            auth = self.worker.ceph("auth", "ls")
        except NativeFailure as error:
            if recorded:
                return None
            raise self.unread(error) from None
        require(isinstance(auth, dict) and isinstance(auth.get("auth_dump"), list) and len(auth["auth_dump"]) <= MAX_ENTITIES
                and all(isinstance(entry, dict) and isinstance(entry.get("entity"), str) for entry in auth["auth_dump"]), "native-output")
        return auth["auth_dump"]

    def pools(self):
        try:
            usage = self.worker.ceph("df")
        except NativeFailure as error:
            raise self.unread(error) from None
        pools = usage.get("pools") if isinstance(usage, dict) else None
        require(isinstance(pools, list) and len(pools) <= MAX_POOLS and all(
            isinstance(pool, dict) and isinstance(pool.get("name"), str) and isinstance(pool.get("stats"), dict) and counted(pool["stats"])
            for pool in pools),
            "native-output")
        return pools

    def check(self):
        """Pass for a host the claim never reached or one that never held a monitor. Otherwise refuse `kept-foreign-client` for any key
        outside the closed set, read on every attempt a monitor answers, and `kept-user-data` for any data in a pool but `.mgr`; this
        cluster's pass record stands in only for `ceph df`, which stopped managers cannot answer."""
        markers = self.worker.markers
        if not markers.owned() or not any(name.startswith("mon.") for name in listing(DATA_ROOT + "/" + self.request["fsid"])):
            return {"unused": True}
        recorded = markers.read(UNUSED)
        require(recorded in (None, self.record), "ownership")
        entities = self.entities(recorded is not None)
        require(entities is None or all(self.entity_allowed(entry["entity"]) for entry in entities), "kept-foreign-client")
        if recorded is not None:
            return {"unused": True}
        pools = self.pools()
        require(all(pool["name"] == MANAGER_POOL or pool["stats"]["objects"] == 0 and pool["stats"]["stored"] == 0 for pool in pools),
                "kept-user-data")
        markers.write(UNUSED, self.record)
        return {"unused": True}


def remove_tree(parent, name, depth=0):
    """Remove one entry under a directory descriptor without following a link; a directory only to a fixed depth."""
    info = os.stat(name, dir_fd=parent, follow_symlinks=False)
    if not stat.S_ISDIR(info.st_mode):
        os.unlink(name, dir_fd=parent)
        return
    require(depth < MAX_DEPTH, "tiebreaker-foreign")
    directory = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=parent)
    try:
        entries = os.listdir(directory)
        require(len(entries) <= MAX_ENTRIES, "tiebreaker-foreign")
        for entry in entries:
            remove_tree(directory, entry, depth + 1)
    finally:
        os.close(directory)
    os.rmdir(name, dir_fd=parent)


class KeptHostRemoval(PhysicalHostRemoval):
    """One owned FSID removed from a kept host that stays running: proved its own, removed, read back absent, then its records."""

    def __init__(self, worker):  # pylint: disable=super-init-not-called
        require(worker.request["action"] == "cluster" and worker.host in worker.request["hosts"], "request")
        self.worker = worker
        self.request = worker.request
        self.host = worker.host
        self.markers = worker.markers
        self.fsid = worker.request["fsid"]
        self.record = removal_record(self.request, self.host)

    def device_nodes(self):
        """Each device the frozen request names for this host, by the kernel node its stable path resolves to."""
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_native_osd
        devices = ceph_native_osd.Devices(self.worker)
        nodes = {}
        for device in self.host.get("devices", []):
            path = devices.stable_device_path(self.host, device)
            nodes[os.path.realpath(path)] = path
        return nodes

    def volumes(self):
        output = self.worker.command(["/usr/sbin/lvs", "--reportformat", "json", "--options", "lv_path,lv_uuid,lv_tags,devices"])
        require(isinstance(output, dict) and isinstance(output.get("report"), list) and len(output["report"]) == 1, "native-output")
        rows = output["report"][0].get("lv")
        require(isinstance(rows, list) and len(rows) <= MAX_VOLUMES and all(isinstance(row, dict) for row in rows), "native-output")
        return rows

    def physical_volumes(self):
        output = self.worker.command(["/usr/sbin/pvs", "--reportformat", "json", "--options", "pv_name,pv_uuid,vg_name"])
        require(isinstance(output, dict) and isinstance(output.get("report"), list) and len(output["report"]) == 1, "native-output")
        rows = output["report"][0].get("pv")
        require(isinstance(rows, list) and len(rows) <= MAX_VOLUMES
                and all(isinstance(row, dict) and all(isinstance(row.get(key), str) for key in ("pv_name", "pv_uuid", "vg_name")) for row in rows),
                "native-output")
        return rows

    def tagged(self):
        """The named devices holding a volume tagged with this FSID, each by its stable path. A tagged volume on a device the request does
        not name or across devices, a named device that also holds another cluster's or an untagged volume, or a volume group reaching
        another device refuses: a zap there would destroy what this cluster cannot prove its own."""
        nodes, ours, others = self.device_nodes(), {}, set()
        for row in self.volumes():
            paths = devices_of(row)
            if tags_of(row).get("ceph.cluster_fsid") == self.fsid:
                require(len(paths) == 1 and paths[0] in nodes, "tiebreaker-foreign")
                ours.setdefault(paths[0], []).append(row)
            else:
                others.update(path for path in paths if path in nodes)
        require(not others & set(ours), "tiebreaker-foreign")
        volumes = self.physical_volumes() if ours else []
        for node in ours:
            groups = {value["vg_name"] for value in volumes if value["pv_name"] == node}
            require(all(value["pv_name"] == node for value in volumes if value["vg_name"] in groups - {""}), "tiebreaker-foreign")
        return sorted(nodes[node] for node in ours)

    def zap_residue(self):
        """The named devices a recorded zap left a PV of on: a zap that failed after removing the volumes and before the PV, so the device
        holds no tagged volume and is still owed. Any volume of another cluster or none on it, or the PV now on another device, refuses."""
        records = [self.markers.read(name) for name in listing(self.markers.path) if name.startswith(ZAP_PREFIX)]
        if not records:
            return []
        nodes = {stable: node for node, stable in self.device_nodes().items()}
        volumes, owed = self.physical_volumes(), []
        for record in records:
            require(isinstance(record, dict) and record.get("device") in nodes and isinstance(record.get("pvs"), list), "ownership")
            present = [value for value in volumes if value["pv_uuid"] in record["pvs"]]
            require(all(value["pv_name"] == nodes[record["device"]] for value in present), "tiebreaker-foreign")
            if present:
                owed.append(record["device"])
        if owed:
            for row in self.volumes():
                require(tags_of(row).get("ceph.cluster_fsid") == self.fsid
                        or not any(nodes[device] in devices_of(row) for device in owed), "tiebreaker-foreign")
        return owed

    def footprint(self, authorization):
        return dict(PhysicalHostRemoval.footprint(self, authorization), osd=sorted(set(self.tagged()) | set(self.zap_residue())),
                    lvmDevices=self.owed_devices())

    def present_directory(self):
        return directory_present(self.markers.path)

    def prove(self):
        if not self.claimed():
            require(not any(self.scoped().values()) and not self.traced() and not self.native_runtime() and not self.tagged(), "tiebreaker-foreign")
            return None
        attempt, complete = self.journal()
        authorization = self.authorization()
        return attempt, complete, authorization, self.footprint(authorization)

    def observe(self):
        """Removed only once nothing of the FSID and no record of it remains; attempted once an attempt record or a residue exists."""
        proved = self.prove()
        if proved is None:
            residue = self.present_directory()
            return {"removed": not residue, "attempted": residue}
        attempt, complete, unused, unused_footprint = proved
        return {"removed": False, "attempted": attempt is not None or complete is not None}

    def stop_target(self):
        """This FSID's daemons stopped, so no running OSD holds a volume the zap removes; rm-cluster later removes the units."""
        target = "ceph-" + self.fsid + ".target"
        if target in listing(UNIT_ROOT):
            self.worker.command(["/usr/bin/systemctl", "stop", target], timeout=OSD_CLEAR_SECONDS, json_result=False)

    def zap(self, path):
        """One named device whose volumes all carry this FSID, zapped by its stable path with the image's own ceph-volume, after a record of
        the PVs it held, so the LVM devices-file entries those PVs leave are dropped even when a lost reply ends the call after the zap.
        One device per call keeps every call within the transport's ceiling whatever the host's device count."""
        self.worker.verify_tool()
        argv = [self.worker.tool, "--image", self.request["image"], "ceph-volume", "--fsid", self.fsid, "--", "lvm", "zap", "--destroy", path]
        self.worker.require_declared_options(argv[:argv.index("--")])
        nodes = {stable: node for node, stable in self.device_nodes().items()}
        identifiers = sorted(value["pv_uuid"] for value in self.physical_volumes() if value["pv_name"] == nodes[path])
        self.markers.write(zap_record(path), dict(self.record, device=path, pvs=identifiers))
        try:
            self.worker.command(argv, timeout=OSD_CLEAR_SECONDS, json_result=False)
        except NativeFailure as error:
            refused = Refusal("osd-clear-failed")
            refused.native_error = error.native_error
            raise refused from None
        self.drop_devices()

    def zapped(self):
        """The PVs each recorded zap held, read from this removal's own records."""
        identifiers = []
        for name in listing(self.markers.path):
            if name.startswith(ZAP_PREFIX):
                value = self.markers.read(name)
                require(isinstance(value, dict) and {key: value.get(key) for key in self.record} == self.record and isinstance(value.get("pvs"), list)
                        and all(isinstance(item, str) for item in value["pvs"]), "ownership")
                identifiers.extend(value["pvs"])
        return sorted(set(identifiers))

    def owed_devices(self):
        """The LVM devices-file entries a zap left: each recorded PV that is gone and still listed. A recorded PV still present is a zap
        that failed or never ran; its volumes stay tagged, so the next attempt zaps it again (tagged())."""
        identifiers = self.zapped()
        if not identifiers:
            return []
        present = {value["pv_uuid"] for value in self.physical_volumes()}
        return [identifier for identifier in identifiers if identifier not in present and lvm_devices_lists(identifier.replace("-", ""))]

    def drop_devices(self):
        for identifier in self.owed_devices():
            self.worker.command(["/usr/sbin/lvmdevices", "--yes", "--delpvid", identifier.replace("-", "")], json_result=False)

    def discard(self):
        """The records kept for this FSID on the host, last of all, so an interrupted discard is read back as claimed and
        repeated, or as an unclaimed residue and finished."""
        discard_directory(self.markers.path)

    def remove(self):
        """Every proof before the attempt record; each removal only while its part remains; completion only once all is absent; then the
        records. A host the cluster's claim never reached, proved to hold nothing of the FSID, changes nothing but an empty residue.

        A call zaps at most one device and then returns `remaining`, so the controller calls again; the zap runs while the registry
        credential and the engine route, which its image may need, still remain. Every call proves the whole footprint again."""
        proved = self.prove()
        if proved is None:
            residue = self.present_directory()
            self.discard()
            return {"changed": residue, "removed": True, "attempted": residue, "remaining": False}
        attempt, complete, authorization, footprint = proved
        if not any(footprint.values()):
            if complete is None:
                self.markers.write(COMPLETE, self.record)
            self.discard()
            return {"changed": True, "removed": True, "attempted": True, "remaining": False}
        require(complete is None, "tiebreaker-foreign")
        del attempt
        argv = self.rm_cluster() if any(footprint[name] for name in ("daemons", "data", "log", "logrotate", "units", "conf")) else None
        if argv is None and any(footprint[name] for name in ("runtime", "confResidue", "layoutResidue")):
            self.require_absent(footprint)
        if footprint["conf"]:
            self.markers.write("native-removal-config.json", self.record)
        self.markers.write(ATTEMPT, self.record)
        if footprint["key"]:
            self.remove_key(authorization)
        if footprint["sshdPolicy"]:
            self.remove_policy()
        if footprint["osd"]:
            self.stop_target()
            self.zap(footprint["osd"][0])
            return {"changed": True, "removed": False, "attempted": True, "remaining": True}
        if footprint["lvmDevices"]:
            self.drop_devices()
        if footprint["credential"]:
            self.credential_present(remove=True)
        if footprint["route"]:
            self.route_present(remove=True)
        if argv is not None:
            self.worker.command(argv, timeout=300, json_result=False)
        self.await_absence(authorization)
        self.markers.write(COMPLETE, self.record)
        self.discard()
        return {"changed": True, "removed": True, "attempted": True, "remaining": False}

    def journal(self):
        attempt, complete = self.markers.read(ATTEMPT), self.markers.read(COMPLETE)
        require(attempt in (None, self.record) and complete in (None, self.record), "ownership")
        return attempt, complete


def opened_records(root, name):
    """A records directory under the parent's descriptor, opened without following a link and proved root's, or None when absent."""
    try:
        directory = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC, dir_fd=root)
    except FileNotFoundError:
        return None
    except OSError:
        raise Refusal("tiebreaker-foreign") from None
    try:
        info = os.fstat(directory)
        require(info.st_uid == 0 and info.st_mode & 0o022 == 0, "tiebreaker-foreign")
    except BaseException:
        os.close(directory)
        raise
    return directory, info


def finish_discard(root, staged):
    """Empty and remove a records directory a discard already renamed aside; whether one was there."""
    opened = opened_records(root, staged)
    if opened is None:
        return False
    directory = opened[0]
    try:
        entries = os.listdir(directory)
        require(len(entries) <= MAX_ENTRIES, "tiebreaker-foreign")
        for entry in entries:
            remove_tree(directory, entry, 1)
        os.fsync(directory)
    finally:
        os.close(directory)
    os.rmdir(staged, dir_fd=root)
    os.fsync(root)
    return True


def discard_directory(path, before=None):
    """A fixed FSID directory of records removed without following a link; before runs once the directory is open, to prove it this
    cluster's. The directory is first renamed aside in one step, so an interrupted discard never leaves part of its records where a
    reader finds them: only the renamed directory, which the next discard finishes before anything else. Whether either was there."""
    parent, name = path.rsplit("/", 1)
    staged = DISCARD_PREFIX + name
    with walked(parent) as root:
        if root is None:
            return False
        finished = finish_discard(root, staged)
        opened = opened_records(root, name)
        if opened is None:
            return finished
        directory, info = opened
        try:
            if before is not None:
                before()
            os.rename(name, staged, src_dir_fd=root, dst_dir_fd=root)
            os.fsync(root)
        finally:
            os.close(directory)
        moved = os.stat(staged, dir_fd=root, follow_symlinks=False)
        require((moved.st_dev, moved.st_ino) == (info.st_dev, info.st_ino), "tiebreaker-foreign")
        finish_discard(root, staged)
        return True


def directory_present(path):
    """Whether the records directory, or one a discard renamed aside and has not finished, remains."""
    parent, name = path.rsplit("/", 1)
    with walked(parent) as directory:
        if directory is None:
            return False
        for entry in (name, DISCARD_PREFIX + name):
            try:
                os.stat(entry, dir_fd=directory, follow_symlinks=False)
            except FileNotFoundError:
                continue
            return True
        return False
