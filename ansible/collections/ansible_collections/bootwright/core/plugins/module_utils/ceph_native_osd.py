"""Native OSD devices: stable device paths, identities, partial-OSD clearing, orphan purging, variant proofs and preparation."""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import hashlib
import os
import re

from ansible_collections.bootwright.core.plugins.module_utils.canonical_json import canonical
from ansible_collections.bootwright.core.plugins.module_utils.ceph_native import (
    CEPH_VOLUME_BLOCK, CEPH_VOLUME_GROUP, DEVICE_IDENTITIES, DEVICE_IDENTITIES_VERSION, DEVICE_TREE, LVM_UUID, NativeFailure, OSD_CLEAR_SECONDS,
    OSD_CLEAR_VERSION, OSD_UUID, Refusal, auxiliary_device_paths, device_path, lvm_devices_lists, never_booted, require, volume_tool_running,
)


class Devices:
    """Native OSD devices: stable device paths, identities, partial-OSD clearing, orphan purging, variant proofs and preparation."""

    def __init__(self, worker):
        self.worker = worker

    def prove_completed_osd_variants(self, physical=None, services=None):
        services = services if services is not None else [
            s for s in self.worker.request["services"] if s["service_type"] == "osd" and self.worker.host["name"] in s["placement"]["hosts"]
            and any(s["spec"].get(k) for k in ("encrypted", "tpm2", "db_devices", "wal_devices"))]
        if not services:
            return
        require(self.worker.markers.owned() and self.worker.markers.read("osd-device-intent.json") == self.worker.osd_device_scope(), "ownership")
        physical = self.worker.physical_devices() if physical is None else physical
        observed = self.worker.command(list(DEVICE_TREE))
        output = self.worker.command(["/usr/sbin/lvs", "--reportformat", "json", "--options", "lv_path,lv_uuid,lv_tags,devices"])
        require(isinstance(observed, dict) and isinstance(observed.get("blockdevices"), list), "native-output")
        require(isinstance(output, dict) and isinstance(output.get("report"), list) and len(output["report"]) == 1, "native-output")
        rows = output["report"][0].get("lv")
        require(isinstance(rows, list) and len(rows) <= 1024, "native-output")
        for service in services:
            spec, allocations = service["spec"], {}
            for field, role in (("data_devices", "block"), ("db_devices", "db"), ("wal_devices", "wal")):
                for path in map(device_path, spec.get(field, {}).get("paths", [])):
                    device = physical.get(path) or next(d for d in self.worker.host["devices"] if d["path"] == path)
                    node = self.worker.node_of(path, physical)
                    disks = [d for d in observed["blockdevices"] if d.get("path") == node]
                    require(len(disks) == 1, "device-identity")
                    disk = disks[0]
                    require(disk.get("type") == "disk" and disk.get("serial") == device["serial"]
                            and disk.get("size") == device["sizeBytes"] and not any(disk.get("mountpoints") or []), "device-identity")
                    selected = []
                    for row in rows:
                        require(isinstance(row, dict) and isinstance(row.get("devices"), str), "native-output")
                        paths = [p.strip().split("(", 1)[0] for p in row["devices"].split(",")]
                        if node not in paths:
                            continue
                        require(paths == [node] and isinstance(row.get("lv_tags"), str), "foreign-osd")
                        tags = {}
                        for tag in row["lv_tags"].split(","):
                            key, separator, value = tag.strip().partition("=")
                            require(separator and key not in tags, "foreign-osd")
                            tags[key] = value
                        require(tags.get("ceph.cluster_fsid") == self.worker.request["fsid"] and tags.get("ceph.type") == role, "foreign-osd")
                        require(re.fullmatch(r"[0-9]+", tags.get("ceph.osd_id", "")) is not None
                                and re.fullmatch(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}", tags.get("ceph.osd_fsid", "")), "foreign-osd")
                        require(isinstance(row.get("lv_uuid"), str) and row["lv_uuid"] and isinstance(row.get("lv_path"), str), "foreign-osd")
                        self.prove_osd_crypto(row, tags, disk.get("children") or [], spec)
                        if role == "block":
                            retained = self.worker.markers.read(self.worker.osd_identity_marker(node))
                            identity = {"path": node, "id": int(tags["ceph.osd_id"]),
                                        "uuid": tags["ceph.osd_fsid"], "lvUUID": row["lv_uuid"]}
                            require(retained is None or retained == identity, "foreign-osd")
                        selected.append((row, tags))
                    require(selected and (role != "block" or len(selected) == 1), "foreign-osd")
                    require(len(disk.get("children") or []) == len(selected), "foreign-osd")
                    allocations.setdefault(role, []).extend(selected)
            blocks = allocations["block"]
            require(len({t["ceph.osd_id"] for r, t in blocks}) == len(blocks), "foreign-osd")
            for role in ("db", "wal"):
                auxiliary = allocations.get(role, [])
                require(len({r["lv_uuid"] for r, t in auxiliary}) == len(auxiliary), "foreign-osd")
                for row, tags in auxiliary:
                    matches = [t for r, t in blocks if t["ceph.osd_id"] == tags["ceph.osd_id"]
                               and t["ceph.osd_fsid"] == tags["ceph.osd_fsid"] and t.get("ceph." + role + "_uuid") == row["lv_uuid"]]
                    require(len(matches) == 1, "foreign-osd")
                if role in allocations:
                    require(len(auxiliary) == len(blocks), "foreign-osd")

    def prove_osd_crypto(self, row, tags, children, spec):
        require(tags.get("ceph.encrypted", "0") == str(int(spec.get("encrypted", False)))
                and tags.get("ceph.with_tpm", "0") == str(int(spec.get("tpm2", False))), "foreign-osd")
        matches = [c for c in children if os.path.realpath(c.get("path", "")) == os.path.realpath(row["lv_path"])]
        require(len(matches) == 1 and matches[0].get("type") == "lvm" and not any(matches[0].get("mountpoints") or []), "foreign-osd")
        volume = matches[0]
        if spec.get("encrypted"):
            mappings = volume.get("children") or []
            require(volume.get("fstype") in (None, "", "crypto_LUKS"), "foreign-osd")
            require(len(mappings) == 1, "foreign-osd")
            volume = mappings[0]
            require(volume.get("type") == "crypt"
                    and os.path.realpath(volume.get("path", "")) == os.path.realpath("/dev/mapper/" + row["lv_uuid"]), "foreign-osd")
            metadata = self.worker.command(["/usr/sbin/cryptsetup", "luksDump", row["lv_path"]], json_result=False)
            require(isinstance(metadata, bytes) and len(metadata) <= 128 << 10, "native-output")
            require(re.match(rb"LUKS header information(?: for \S+\n)?\n", metadata) is not None, "foreign-osd")
            if spec.get("tpm2"):
                tokens = metadata.split(b"Tokens:\n")
                require(len(tokens) == 2 and re.search(rb"(?m)^\s+[0-9]+: systemd-tpm2\s*$", tokens[1].split(b"Digests:", 1)[0]), "foreign-osd")
        require(not volume.get("children") and not any(volume.get("mountpoints") or [])
                and volume.get("fstype") in (None, "", "ceph_bluestore"), "foreign-osd")

    def osd_device_scope(self):
        return {"version": "ceph-osd-device-intent-v1", "owner": self.worker.request["owner"], "fsid": self.worker.request["fsid"],
                "host": self.worker.host["name"], "devices": self.worker.host["devices"], "image": self.worker.request["image"]}

    def owned_osd_device(self, actual, item, signatures, device, alias):
        require(self.worker.markers.owned() and self.worker.markers.read("osd-device-intent.json") == self.worker.osd_device_scope(), "device-not-empty")
        require(item.get("fstype") in (None, "", "LVM2_member") and not any(item.get("mountpoints") or []), "device-not-empty")
        require(all(isinstance(value, dict) and value.get("type") == "LVM2_member" for value in signatures), "device-not-empty")
        output = self.worker.command(["/usr/sbin/lvs", "--reportformat", "json", "--options", "lv_path,lv_uuid,lv_tags,devices"])
        require(isinstance(output, dict) and isinstance(output.get("report"), list) and len(output["report"]) == 1, "native-output")
        rows = output["report"][0].get("lv")
        require(isinstance(rows, list) and len(rows) <= 1024, "native-output")
        listed = []
        for row in rows:
            require(isinstance(row, dict) and isinstance(row.get("devices"), str), "native-output")
            paths = [part.strip().split("(", 1)[0] for part in row["devices"].split(",")]
            if actual not in paths:
                continue
            require(paths == [actual] and isinstance(row.get("lv_tags"), str), "foreign-osd")
            tags = {}
            for tag in row["lv_tags"].split(",") if row["lv_tags"] else []:
                key, separator, value = tag.strip().partition("=")
                require(separator and key not in tags, "foreign-osd")
                tags[key] = value
            listed.append((row, tags))
        if not listed or any("ceph.osd_id" not in tags for unused, tags in listed):
            return self.worker.owned_partial_osd(actual, item, signatures, listed, rows, device, alias)
        candidates = []
        for row, tags in listed:
            require(tags.get("ceph.cluster_fsid") == self.worker.request["fsid"] and tags.get("ceph.type") == "block", "foreign-osd")
            require(re.fullmatch(r"[0-9]+", tags.get("ceph.osd_id", "")) is not None
                    and re.fullmatch(r"[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}", tags.get("ceph.osd_fsid", "")), "foreign-osd")
            require(isinstance(row.get("lv_uuid"), str) and row["lv_uuid"] and isinstance(row.get("lv_path"), str), "foreign-osd")
            children = item.get("children") or []
            specs = self.worker.osd_data_specs(actual)
            require(len(specs) == 1 and len(children) == 1, "foreign-osd")
            self.prove_osd_crypto(row, tags, children, specs[0])
            candidates.append({"path": actual, "id": int(tags["ceph.osd_id"]), "uuid": tags["ceph.osd_fsid"], "lvUUID": row["lv_uuid"]})
        require(len(candidates) == 1, "foreign-osd")
        retained = self.worker.markers.read(self.worker.osd_identity_marker(actual))
        require(retained is None or retained == candidates[0], "foreign-osd")
        return candidates[0]

    def osd_data_specs(self, actual):
        return [s["spec"] for s in self.worker.request["services"] if s["service_type"] == "osd" and self.worker.host["name"] in s["placement"]["hosts"]
                and any(self.worker.node_of(device_path(path), self.worker.bound_devices) == actual for path in s["spec"]["data_devices"]["paths"])]

    def owned_partial_osd(self, actual, item, signatures, listed, rows, device, alias):
        """The clear of a declared data disk that holds only this operation's own interrupted OSD creation; any other
        partial shape refuses osd-partial, since an interrupted creation and a foreign write cannot otherwise be told apart.

        Every condition is read-only: the intent the caller proved, no retained OSD identity, one declared OSD service
        without DB or WAL devices, one LVM signature, exactly one PV whose volume group ceph-volume named and that holds no
        other PV, exactly one block volume ceph-volume named, whose name carries the OSD fsid ceph-volume allocated,
        carrying no OSD ID and no other cluster's FSID, active as the disk's only holder and itself held by nothing,
        nothing mounted, and no ceph-volume running on the host. A disk this operation already cleared once is never
        cleared again. A clear an interrupted call left unfinished also admits its volume group without a volume, or its
        PV without a volume group, of the same recorded PV, disk and OSD fsid."""
        require(all(tags.get("ceph.cluster_fsid", self.worker.request["fsid"]) == self.worker.request["fsid"] for unused, tags in listed), "foreign-osd")
        retained = self.osd_clear_record(alias, device)
        require(self.worker.markers.read(self.worker.osd_cleared_marker(alias)) is None, "osd-partial")
        specs = self.worker.osd_data_specs(actual)
        require(self.worker.markers.read(self.worker.osd_identity_marker(actual)) is None and len(specs) == 1
                and not specs[0].get("db_devices") and not specs[0].get("wal_devices"), "osd-partial")
        require(len(signatures) == 1 and not any(item.get("mountpoints") or []), "osd-partial")
        volumes = self.physical_volumes()
        mine = [value for value in volumes if value["pv_name"] == actual]
        require(len(mine) == 1 and LVM_UUID.fullmatch(mine[0]["pv_uuid"]) is not None, "osd-partial")
        volume = mine[0]
        group = volume["vg_name"]
        if group:
            require(CEPH_VOLUME_GROUP.fullmatch(group) is not None and [value for value in volumes if value["vg_name"] == group] == [volume], "osd-partial")
        prefix = "/dev/" + group + "/"
        require(group or not listed, "osd-partial")
        require(len(listed) <= 1 and all(not isinstance(row.get("lv_path"), str) or not row["lv_path"].startswith(prefix)
                                         or any(row is mine_row for mine_row, unused in listed) for row in rows), "osd-partial")
        children = item.get("children") or []
        require(len(children) == len(listed), "osd-partial")
        osd = ""
        for (row, tags), child in zip(listed, children):
            path = row.get("lv_path")
            require(isinstance(path, str) and path.startswith(prefix) and CEPH_VOLUME_BLOCK.fullmatch(path[len(prefix):]) is not None, "osd-partial")
            require(all(key.startswith("ceph.") for key in tags) and "ceph.osd_id" not in tags and tags.get("ceph.type", "block") == "block", "osd-partial")
            require(isinstance(child, dict) and child.get("type") == "lvm" and not any(child.get("mountpoints") or []) and not child.get("children")
                    and isinstance(child.get("path"), str) and os.path.realpath(child["path"]) == os.path.realpath(path), "osd-partial")
            osd = path[len(prefix) + len("osd-block-"):]
        record = {"version": OSD_CLEAR_VERSION, "owner": self.worker.request["owner"], "fsid": self.worker.request["fsid"], "host": self.worker.host["name"],
                  "device": alias, "serial": device["serial"], "sizeBytes": device["sizeBytes"], "pvUUID": volume["pv_uuid"], "osdUUID": osd}
        if retained is None:
            # Without its block volume the OSD fsid ceph-volume allocated is unknown, so no OSD in the map can be attributed to it.
            require(osd, "osd-partial")
        else:
            require({key: value for key, value in retained.items() if key != "osdUUID"} == {key: value for key, value in record.items() if key != "osdUUID"}
                    and osd in ("", retained["osdUUID"]), "osd-partial")
            record = retained
        require(not volume_tool_running(), "osd-partial")
        return {"record": record, "path": actual, "shape": self.partial_shape(actual, group, volumes, rows)}

    def physical_volumes(self):
        output = self.worker.command(["/usr/sbin/pvs", "--reportformat", "json", "--options", "pv_name,pv_uuid,vg_name"])
        require(isinstance(output, dict) and isinstance(output.get("report"), list) and len(output["report"]) == 1, "native-output")
        volumes = output["report"][0].get("pv")
        require(isinstance(volumes, list) and len(volumes) <= 1024, "native-output")
        require(all(isinstance(value, dict) and all(isinstance(value.get(key), str) for key in ("pv_name", "pv_uuid", "vg_name"))
                    for value in volumes), "native-output")
        return volumes

    def logical_volumes(self):
        output = self.worker.command(["/usr/sbin/lvs", "--reportformat", "json", "--options", "lv_path,lv_uuid,lv_tags,devices"])
        require(isinstance(output, dict) and isinstance(output.get("report"), list) and len(output["report"]) == 1, "native-output")
        rows = output["report"][0].get("lv")
        require(isinstance(rows, list) and len(rows) <= 1024 and all(isinstance(row, dict) for row in rows), "native-output")
        return rows

    @staticmethod
    def partial_shape(actual, group, volumes, rows):
        """The PVs and volumes a zap of this disk touches: ceph-volume's --destroy removes every PV of the disk's volume group."""
        prefix = "/dev/" + group + "/"
        return canonical({"pvs": [value for value in volumes if value["pv_name"] == actual or group and value["vg_name"] == group],
                          "lvs": [row for row in rows if actual in str(row.get("devices", "")) or group and str(row.get("lv_path", "")).startswith(prefix)]})

    def osd_clear_record(self, alias, device, stage=b"clear"):
        """A retained clear record, which names the disk by its declared stable alias, serial and size."""
        record = self.worker.markers.read(self.worker.osd_clear_marker(alias, stage))
        require(record is None or isinstance(record, dict) and record.get("version") == OSD_CLEAR_VERSION and record.get("device") == alias
                and record.get("serial") == device["serial"] and record.get("sizeBytes") == device["sizeBytes"]
                and isinstance(record.get("pvUUID"), str) and LVM_UUID.fullmatch(record["pvUUID"]) is not None
                and isinstance(record.get("osdUUID"), str) and (record["osdUUID"] == "" or OSD_UUID.fullmatch(record["osdUUID"]) is not None),
                "osd-partial")
        return record

    @staticmethod
    def osd_clear_marker(alias, stage=b"clear"):
        """A clear's intent, or with stage b"cleared" its completion: OSD device records keyed by the stable alias, apart from the identity record."""
        return "osd-device-" + hashlib.sha256(stage + b"\x00" + alias.encode()).hexdigest() + ".json"

    @classmethod
    def osd_cleared_marker(cls, alias):
        return cls.osd_clear_marker(alias, b"cleared")

    def clear_partial_osds(self):
        """Zap each disk owned_partial_osd proved, after its intent and an unchanged re-read of its PVs and volumes; prove every
        disk empty again; then complete each clear, dropping only the zapped PV's own LVM devices-file entry once no PV of that
        UUID remains, a step an interrupted call's replay finishes."""
        recorded = {row["uuid"] for row in self.worker.observed_device_ids}
        require(all(clear["record"]["osdUUID"] not in recorded for clear in self.worker.osd_clears), "osd-partial")
        if self.worker.osd_clears:
            self.worker.verify_tool()
        for clear in self.worker.osd_clears:
            record, actual = clear["record"], clear["path"]
            self.worker.markers.write(self.worker.osd_clear_marker(record["device"]), record)
            volumes, rows = self.physical_volumes(), self.logical_volumes()
            group = next((value["vg_name"] for value in volumes if value["pv_name"] == actual), "")
            require(self.partial_shape(actual, group, volumes, rows) == clear["shape"] and not volume_tool_running(), "osd-partial")
            try:
                self.worker.command([self.worker.tool, "--image", self.worker.request["image"], "ceph-volume", "--fsid", self.worker.request["fsid"], "--",
                                     "lvm", "zap", "--destroy", actual], timeout=OSD_CLEAR_SECONDS, json_result=False)
            except NativeFailure as error:
                refused = Refusal("osd-clear-failed")
                refused.native_error = error.native_error
                raise refused from None
        cleared = [clear["record"]["device"] for clear in self.worker.osd_clears]
        self.worker.devices()
        require(not self.worker.osd_clears and set(cleared) <= {record["device"] for record in self.worker.osd_settled}, "osd-clear-unproved")
        for record in self.worker.osd_settled:
            identifier = record["pvUUID"].replace("-", "")
            if lvm_devices_lists(identifier):
                require(all(value["pv_uuid"] != record["pvUUID"] for value in self.physical_volumes()), "osd-clear-unproved")
                self.worker.command(["/usr/sbin/lvmdevices", "--yes", "--delpvid", identifier], json_result=False)
                require(not lvm_devices_lists(identifier), "osd-clear-unproved")
            self.worker.markers.write(self.worker.osd_cleared_marker(record["device"]), record)
            if record["osdUUID"]:
                self.worker.osd_orphans.append(record["osdUUID"])

    def purge_orphan_osds(self, orphans):
        """Purge only the never-booted OSD each completed clear's zapped volume named, settled from the map: none is done."""
        changed = False
        for osd in orphans:
            matches = [value for value in self.osd_map() if value.get("uuid") == osd]
            require(len(matches) <= 1, "osd-orphan-unproved")
            if not matches:
                continue
            item = matches[0]
            require(type(item.get("osd")) is int and item["osd"] >= 0 and never_booted(item), "osd-orphan-booted")
            self.worker.markers.write(self.worker.osd_purge_marker(osd),
                                      {"version": OSD_CLEAR_VERSION, "fsid": self.worker.request["fsid"], "osdUUID": osd, "id": item["osd"]})
            self.worker.ceph("osd", "purge-new", "osd." + str(item["osd"]), "--yes-i-really-mean-it", json_result=False)
            require(all(value.get("uuid") != osd for value in self.osd_map()), "osd-orphan-unproved")
            changed = True
        return changed

    def osd_map(self):
        osdmap = self.worker.ceph("osd", "dump")
        require(isinstance(osdmap, dict) and isinstance(osdmap.get("osds"), list) and len(osdmap["osds"]) <= 65536
                and all(isinstance(item, dict) for item in osdmap["osds"]), "native-output")
        return osdmap["osds"]

    @staticmethod
    def osd_purge_marker(osd):
        return "osd-device-" + hashlib.sha256(b"purge\x00" + osd.encode()).hexdigest() + ".json"

    @staticmethod
    def osd_identity_marker(path):
        return "osd-device-" + hashlib.sha256(path.encode()).hexdigest() + ".json"

    def stable_device_path(self, host, device, *, submission=False):
        """Only the authored immutable physical WWN, the bound alias of a physical serial selection, or qualified QEMU disk identities."""
        if host.get("physical"):
            if device["path"].startswith("/dev/disk/by-serial/"):
                # The frozen device carries no alias; a submission reads the row host preparation bound and services published.
                row = self.worker.published_row(host, device) if submission else device
                require(isinstance(row, dict) and row.get("path") == device["path"] and isinstance(row.get("alias"), str)
                        and re.fullmatch(r"/dev/disk/by-id/[A-Za-z0-9_.:+-]{1,200}", row["alias"]) is not None, "device-identity")
                require(submission or os.path.realpath(row["alias"]) == row["kernelPath"], "device-identity")
                return row["alias"]
            require(device.get("wwn") and device["path"] == "/dev/disk/by-id/wwn-" + device["wwn"], "device-identity")
            return device["path"]
        if re.fullmatch(r"/dev/vd[a-z]+", device["path"]):
            return "/dev/disk/by-id/virtio-" + device["serial"]
        require(re.fullmatch(r"/dev/sd[a-z]+", device["path"]) is not None, "device-identity")
        identifier = "QEMU_QEMU_HARDDISK_" + device["serial"]
        if submission:
            identities = self.worker.device_identities()
            require(identities is not None and identities.get((host["name"], device["path"])) == identifier, "device-identity")
        return "/dev/disk/by-id/scsi-0" + identifier

    def node_of(self, path, physical=None):
        """The kernel node a requested device path names: a serial selection's bound kernel path, any other path's resolution."""
        if not path.startswith("/dev/disk/by-serial/"):
            return os.path.realpath(path)
        physical = self.worker.physical_devices() if physical is None else physical
        row = physical.get(path)
        require(isinstance(row, dict) and isinstance(row.get("kernelPath"), str), "device-identity")
        return row["kernelPath"]

    def physical_devices(self):
        if not self.worker.host.get("physical"):
            return {}
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
        reader = ceph_host_runtime.HostRuntime(self.worker.request, self.worker.host["name"])
        return {value["path"]: value for value in reader.host_devices()["devices"]}

    def devices(self):
        physical = self.worker.bound_devices = self.worker.physical_devices()
        self.worker.observed_device_ids, self.worker.osd_clears, self.worker.osd_settled, self.worker.osd_orphans = [], [], [], []
        if self.worker.markers.owned() and self.worker.markers.read("registry-file.json") is not None:
            self.worker.prove_registry()
        observed = self.worker.command(list(DEVICE_TREE))
        require(isinstance(observed, dict) and isinstance(observed.get("blockdevices"), list), "device-unproved")
        flattened = []

        def visit(items):
            require(isinstance(items, list) and len(flattened) + len(items) <= 4096, "device-unproved")
            for item in items:
                require(isinstance(item, dict), "device-unproved")
                flattened.append(item)
                visit(item.get("children", []))

        visit(observed["blockdevices"])
        resolved = set()
        for frozen_device in self.worker.host["devices"]:
            device = physical.get(frozen_device["path"], frozen_device)
            actual = self.worker.node_of(device["path"], physical)
            require(actual not in resolved, "device-identity")
            resolved.add(actual)
            alias = self.worker.stable_device_path(self.worker.host, device)
            require(os.path.realpath(alias) == actual, "device-identity")
            matches = [value for value in flattened if value.get("path") == actual]
            require(len(matches) == 1, "device-unproved")
            item = matches[0]
            require(item.get("type") == "disk" and str(item.get("serial", "")).strip() == device["serial"]
                    and item.get("size") == device["sizeBytes"], "device-identity")
            if alias.startswith("/dev/disk/by-id/scsi-"):
                from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime as runtime
                raw = self.worker.command(["/usr/bin/udevadm", "info", "--query=property", "--name=" + actual], json_result=False)
                require(isinstance(raw, bytes) and 0 < len(raw) <= 128 << 10, "device-identity")
                values = {}
                for line in raw.splitlines():
                    require(b"=" in line and len(line) <= 4096, "device-identity")
                    name, value = line.split(b"=", 1)
                    if name in (b"ID_SERIAL", b"ID_SCSI_SERIAL", b"ID_VENDOR", b"ID_MODEL"):
                        require(name not in values and all(32 <= byte <= 126 for byte in value), "device-identity")
                        values[name] = value
                environment = {name.decode("ascii"): value.decode("ascii") for name, value in values.items()}
                require(environment.get("ID_SCSI_SERIAL") == device["serial"]
                        and environment.get("ID_SERIAL") == "0QEMU_QEMU_HARDDISK_" + device["serial"]
                        and runtime.native_form(environment) == "QEMU_QEMU_HARDDISK_" + device["serial"]
                        and os.path.realpath(alias) == actual, "device-identity")
            signatures = self.worker.command(["/usr/sbin/wipefs", "--no-act", "--json", actual])
            require(isinstance(signatures, dict) and isinstance(signatures.get("signatures"), list), "device-not-empty")
            if item.get("children") or item.get("fstype") or any(item.get("mountpoints") or []) or signatures["signatures"]:
                if device["path"] in auxiliary_device_paths(self.worker.request, self.worker.host["name"]):
                    self.completed_auxiliary(device["path"], item, signatures["signatures"], physical)
                    continue
                found = self.owned_osd_device(actual, item, signatures["signatures"], device, alias)
                (self.worker.osd_clears if "record" in found else self.worker.observed_device_ids).append(found)
                if "record" in found:
                    continue
                observed = found["uuid"]
            else:
                require(self.worker.markers.read(self.worker.osd_identity_marker(actual)) is None, "foreign-osd")
                observed = ""
            pending, done = self.osd_clear_record(alias, device), self.osd_clear_record(alias, device, b"cleared")
            require(done is None or done == pending, "osd-partial")
            if pending is not None and done is None and (not observed or self.zapped_before(actual, pending, observed)):
                self.worker.osd_settled.append(pending)
            elif done is not None and done["osdUUID"] and done["osdUUID"] != observed:
                # A completed clear names its orphan even after the OSD service created the disk's OSD again.
                self.worker.osd_orphans.append(done["osdUUID"])
        return {"devices": len(self.worker.host["devices"])}

    def completed_auxiliary(self, path, item, signatures, physical):
        require(self.worker.markers.owned() and self.worker.markers.read("osd-device-intent.json") == self.worker.osd_device_scope(), "device-not-empty")
        require(item.get("fstype") in (None, "", "LVM2_member") and not any(item.get("mountpoints") or [])
                and item.get("children") and all(child.get("type") == "lvm" for child in item["children"])
                and all(isinstance(value, dict) and value.get("type") == "LVM2_member" for value in signatures), "device-not-empty")
        services = [s for s in self.worker.request["services"] if s["service_type"] == "osd" and self.worker.host["name"] in s["placement"]["hosts"]
                    and any(path in s["spec"].get(field, {}).get("paths", []) for field in ("db_devices", "wal_devices"))]
        require(len(services) == 1, "device-not-empty")
        self.worker.prove_completed_osd_variants(physical, services)

    def zapped_before(self, actual, pending, observed):
        """Whether a clear whose completion was never recorded zapped the disk before the OSD service created a complete OSD on it
        again: that OSD carries neither the OSD fsid nor the PV the clear's intent proved, so the clear completes and names its orphan."""
        mine = [value["pv_uuid"] for value in self.physical_volumes() if value["pv_name"] == actual]
        return bool(pending["osdUUID"]) and pending["osdUUID"] != observed and len(mine) == 1 and mine[0] != pending["pvUUID"]

    def osd_prepare(self):
        require(self.worker.markers.owned(), "ownership")
        scope = self.worker.osd_device_scope()
        retained = self.worker.markers.read("osd-device-intent.json")
        require(retained is None or retained == scope, "ownership")
        self.worker.devices()
        if retained is None:
            self.worker.markers.write("osd-device-intent.json", scope)
        changed = retained is None
        if self.worker.osd_clears or self.worker.osd_settled:
            self.clear_partial_osds()
            changed = True
        orphans = sorted(set(self.worker.osd_orphans))
        for row in self.worker.observed_device_ids:
            marker = self.worker.osd_identity_marker(row["path"])
            previous = self.worker.markers.read(marker)
            require(previous is None or previous == row, "foreign-osd")
            if previous is None:
                self.worker.markers.write(marker, row)
                changed = True
        return {"changed": changed, "orphans": orphans} if orphans else {"changed": changed}

    def device_identities(self, publish=None):
        """The cluster's OSD device identities, as each host's preparation derived them."""
        record = self.worker.markers.read(DEVICE_IDENTITIES)
        if publish is not None:
            expected = {"version": DEVICE_IDENTITIES_VERSION, "fsid": self.worker.request["fsid"], "devices": publish}
            self.worker.identity_rows(expected)
            require(record is None or canonical(record) == canonical(expected), "device-identity")
            if record is None:
                require(self.worker.markers.owned(), "ownership")
                self.worker.markers.write(DEVICE_IDENTITIES, expected)
                record = self.worker.markers.read(DEVICE_IDENTITIES)
        return None if record is None else self.worker.identity_rows(record)

    def identity_rows(self, record):
        from ansible_collections.bootwright.core.plugins.module_utils import ceph_host_runtime
        wanted = [(host["name"], device) for host in self.worker.request["hosts"] for device in host["devices"]]
        require(isinstance(record, dict) and set(record) == {"version", "fsid", "devices"}, "device-identity")
        require(record["version"] == DEVICE_IDENTITIES_VERSION and record["fsid"] == self.worker.request["fsid"], "device-identity")
        rows = record["devices"]
        require(isinstance(rows, list) and len(rows) == len(wanted), "device-identity")
        result = {}
        for value, (host, device) in zip(rows, wanted):
            ceph_host_runtime.row(value, self.worker.request, host)
            require(value["path"] == device["path"], "device-identity")
            result[(host, device["path"])] = value["deviceID"]
        ceph_host_runtime.distinct_devices(rows)
        return result
