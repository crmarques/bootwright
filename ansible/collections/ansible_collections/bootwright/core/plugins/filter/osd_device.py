from __future__ import annotations

import decimal
import json
import re

from ansible.errors import AnsibleFilterError


_SIZE = re.compile(r"^([0-9]+(?:\.[0-9]+)?)\s*(KB|MB|GB|TB|K|M|G|T)$", re.IGNORECASE)
_SIZE_FACTORS = {
    "K": decimal.Decimal("1e3"),
    "KB": decimal.Decimal("1e3"),
    "M": decimal.Decimal("1e6"),
    "MB": decimal.Decimal("1e6"),
    "G": decimal.Decimal("1e9"),
    "GB": decimal.Decimal("1e9"),
    "T": decimal.Decimal("1e12"),
    "TB": decimal.Decimal("1e12"),
}
_RECLAIM_SENTINEL = "__BOOTWRIGHT_RUNTIME_RECLAIM_DEVICES_7EF51C56__"


def _size_bytes(value):
    if isinstance(value, bool):
        raise ValueError("size is boolean")
    if isinstance(value, (int, float, decimal.Decimal)):
        return decimal.Decimal(str(value))
    if not isinstance(value, str):
        raise ValueError("size is missing")
    match = _SIZE.fullmatch(value.strip())
    if match is None:
        raise ValueError("size has an unsupported value or unit")
    return decimal.Decimal(match.group(1)) * _SIZE_FACTORS[match.group(2).upper()]


def _device_attribute(device, name):
    if name in device:
        return device[name]
    sys_api = device.get("sys_api")
    if isinstance(sys_api, dict) and name in sys_api:
        return sys_api[name]
    raise ValueError(f"device inventory has no {name}")


def _device_size(device):
    try:
        return _size_bytes(_device_attribute(device, "human_readable_size"))
    except ValueError:
        return _size_bytes(_device_attribute(device, "size"))


def _size_filter_matches(device, value):
    text = str(value).strip()
    disk_size = _device_size(device)
    if ":" not in text:
        return disk_size == _size_bytes(text)
    low, high = text.split(":", 1)
    if low and disk_size < _size_bytes(low):
        return False
    if high and disk_size > _size_bytes(high):
        return False
    return True


def _rotational_matches(device, value):
    actual = _device_attribute(device, "rotational")
    if isinstance(actual, bool):
        normalized = actual
    elif isinstance(actual, int) and actual in (0, 1):
        normalized = actual == 1
    elif isinstance(actual, str) and actual.strip() in ("0", "1"):
        normalized = actual.strip() == "1"
    else:
        raise ValueError("device inventory rotational is not 0 or 1")
    if not isinstance(value, bool):
        raise ValueError("desired rotational filter is not boolean")
    return normalized == value


def _substring_matches(device, name, value):
    actual = _device_attribute(device, name)
    if not isinstance(actual, str) or actual == "":
        raise ValueError(f"device inventory has no usable {name}")
    return str(value) in actual


def _filter_results(device, device_filter):
    results = []
    unknown = []
    checks = []
    if device_filter.get("size"):
        checks.append(lambda: _size_filter_matches(device, device_filter["size"]))
    if device_filter.get("model"):
        checks.append(lambda: _substring_matches(device, "model", device_filter["model"]))
    if device_filter.get("vendor"):
        checks.append(lambda: _substring_matches(device, "vendor", device_filter["vendor"]))
    if "rotational" in device_filter:
        checks.append(lambda: _rotational_matches(device, device_filter["rotational"]))
    if device_filter.get("all"):
        checks.append(lambda: True)
    for check in checks:
        try:
            results.append(check())
        except (decimal.InvalidOperation, ValueError) as exc:
            unknown.append(str(exc))
    return results, unknown


def _filter_matches(device, device_filter):
    logic = str(device_filter.get("filterLogic", "AND")).upper()
    if logic not in ("AND", "OR"):
        raise ValueError(f"filterLogic {logic!r} is not AND or OR")
    results, unknown = _filter_results(device, device_filter)
    if not results:
        if unknown:
            raise ValueError(unknown[0])
        return True
    if logic == "OR":
        if any(results):
            return True
        if unknown:
            raise ValueError(unknown[0])
        return any(results)
    if not all(results):
        return False
    if unknown:
        raise ValueError(unknown[0])
    return all(results)


def _validate_filters(filters):
    allowed = {"role", "filterLogic", "all", "model", "vendor", "rotational", "size", "limit"}
    out = []
    unknown = []
    for device_filter in filters:
        if not isinstance(device_filter, dict):
            unknown.append({"path": "<desired>", "role": "<unknown>", "reason": "desired device filter is not a mapping"})
            continue
        role = str(device_filter.get("role", "<unknown>"))
        unsupported = sorted(set(device_filter) - allowed)
        reason = ""
        if unsupported:
            reason = f"desired device filter has unsupported field(s): {', '.join(unsupported)}"
        elif role not in ("data", "db", "wal"):
            reason = "desired device filter has an unsupported role"
        elif str(device_filter.get("filterLogic", "AND")).upper() not in ("AND", "OR"):
            reason = "desired device filter logic is not AND or OR"
        elif "all" in device_filter and not isinstance(device_filter["all"], bool):
            reason = "desired all filter is not boolean"
        elif device_filter.get("all") is True and role != "data":
            reason = "desired all filter is allowed only for data devices"
        elif device_filter.get("all") is True and any(key in device_filter for key in ("model", "vendor", "rotational", "size")):
            reason = "desired all filter is mutually exclusive with model, vendor, rotational, and size"
        elif "rotational" in device_filter and not isinstance(device_filter["rotational"], bool):
            reason = "desired rotational filter is not boolean"
        elif "limit" in device_filter and (isinstance(device_filter["limit"], bool) or not isinstance(device_filter["limit"], int) or device_filter["limit"] < 1):
            reason = "desired limit is not a positive integer"
        elif any(key in device_filter and (not isinstance(device_filter[key], str) or not device_filter[key]) for key in ("model", "vendor", "size")):
            reason = "desired model, vendor, and size filters must be non-empty strings"
        elif not (device_filter.get("all") is True or any(key in device_filter for key in ("model", "vendor", "rotational", "size"))):
            reason = "desired device filter has no selector; limit only caps another selector"
        if reason:
            unknown.append({"path": "<desired>", "role": role, "reason": reason})
            continue
        out.append(device_filter)
    return out, unknown


def bootwright_ceph_osd_filter_candidates(devices, filters, excluded_paths=None):
    if not isinstance(devices, list):
        raise AnsibleFilterError("Ceph OSD device inventory must be a list")
    if not isinstance(filters, list) or not filters:
        raise AnsibleFilterError("Ceph OSD device filters must be a non-empty list")
    filters, unknown = _validate_filters(filters)
    excluded = {str(path) for path in (excluded_paths or []) if str(path)}
    candidates = {}
    for device in devices:
        if not isinstance(device, dict):
            unknown.append({"path": "<unknown>", "role": "<unknown>", "reason": "device inventory entry is not a mapping"})
            continue
        path = device.get("path")
        if not isinstance(path, str) or not path:
            unknown.append({"path": "<unknown>", "role": "<unknown>", "reason": "device inventory entry has no path"})
            continue
        if path in excluded or device.get("being_replaced") is True:
            continue
        available = device.get("available")
        if available is True:
            continue
        if available is not False:
            unknown.append({"path": path, "role": "<unknown>", "reason": "device inventory has no boolean available verdict"})
            continue
        if device.get("osd_ids"):
            continue
        for device_filter in filters:
            role = str(device_filter.get("role", "unknown"))
            try:
                matched = _filter_matches(device, device_filter)
            except (decimal.InvalidOperation, ValueError) as exc:
                unknown.append({"path": path, "role": role, "reason": str(exc)})
                continue
            if not matched:
                continue
            candidate = candidates.setdefault(path, {"path": path, "roles": []})
            if role not in candidate["roles"]:
                candidate["roles"].append(role)
    return {
        "candidates": [candidates[path] for path in sorted(candidates)],
        "unknown": sorted(unknown, key=lambda item: (item["path"], item["role"], item["reason"])),
    }


def bootwright_json_list_probe(value):
    if not isinstance(value, str):
        return {"valid": False, "value": []}
    try:
        parsed = json.loads(value)
    except (json.JSONDecodeError, TypeError, ValueError):
        return {"valid": False, "value": []}
    if not isinstance(parsed, list):
        return {"valid": False, "value": []}
    return {"valid": True, "value": parsed}


def bootwright_reclaim_device_operand(preserved, runtime):
    if not isinstance(preserved, list) or not isinstance(runtime, list):
        raise AnsibleFilterError("runtime OSD reclaim paths must be lists")
    out = []
    for path in preserved + runtime:
        if not isinstance(path, str) or not path or path != path.strip():
            raise AnsibleFilterError("runtime OSD reclaim path must be a nonempty trimmed string")
        if not path.startswith("/dev/"):
            raise AnsibleFilterError(f"runtime OSD reclaim path {path!r} is not an absolute /dev path")
        if any(value in path for value in (",", "\x00", "\r", "\n", _RECLAIM_SENTINEL)):
            raise AnsibleFilterError(f"runtime OSD reclaim path {path!r} cannot be represented safely")
        if path not in out:
            out.append(path)
    if not out:
        raise AnsibleFilterError("runtime OSD reclaim path list must not be empty")
    return ",".join(out)


def bootwright_reclaim_device_operand_probe(preserved, runtime):
    try:
        value = bootwright_reclaim_device_operand(preserved, runtime)
    except AnsibleFilterError as exc:
        return {"valid": False, "value": "", "reason": str(exc)}
    return {"valid": True, "value": value, "reason": ""}


def _lvm_sweep_string_list(value, label):
    if not isinstance(value, list) or any(not isinstance(item, str) for item in value):
        raise AnsibleFilterError(f"Ceph LVM sweep {label} must be a list of strings")
    return value


def bootwright_ceph_lvm_sweep_classify(rows, owned_fsid, preserved_fsids, device_envelope):
    if not isinstance(rows, list):
        raise AnsibleFilterError("Ceph LVM sweep rows must be a list")
    if not isinstance(owned_fsid, str):
        raise AnsibleFilterError("Ceph LVM sweep owned fsid must be a string")
    preserved = set(_lvm_sweep_string_list(preserved_fsids, "preserved fsids"))
    envelope = set(_lvm_sweep_string_list(device_envelope, "device envelope"))
    normalized = []
    for row in rows:
        if not isinstance(row, dict):
            raise AnsibleFilterError("Ceph LVM sweep row must be a mapping")
        pv = row.get("pv")
        fsid = row.get("fsid")
        label = row.get("label")
        if not isinstance(pv, str) or not pv:
            raise AnsibleFilterError("Ceph LVM sweep row must carry a nonempty pv")
        if not isinstance(fsid, str):
            raise AnsibleFilterError("Ceph LVM sweep row must carry a string fsid")
        if not isinstance(label, str) or not label:
            raise AnsibleFilterError("Ceph LVM sweep row must carry a nonempty label")
        normalized.append({"pv": pv, "fsid": fsid, "label": label})

    preserved_pvs = {row["pv"] for row in normalized if row["fsid"] in preserved}
    claimed_pvs = {
        row["pv"]
        for row in normalized
        if (owned_fsid and row["fsid"] == owned_fsid) or row["pv"] in envelope
    }
    devices = []
    device_labels = []
    kept_labels = []
    unclaimed_labels = []
    for row in normalized:
        pv = row["pv"]
        label = row["label"]
        if pv in preserved_pvs:
            if label not in kept_labels:
                kept_labels.append(label)
            continue
        if pv in claimed_pvs:
            if pv not in devices:
                devices.append(pv)
            if label not in device_labels:
                device_labels.append(label)
            continue
        if label not in unclaimed_labels:
            unclaimed_labels.append(label)
    return {
        "devices": devices,
        "deviceLabels": device_labels,
        "keptLabels": kept_labels,
        "unclaimedLabels": unclaimed_labels,
    }


class FilterModule:
    def filters(self):
        return {
            "bootwright_ceph_lvm_sweep_classify": bootwright_ceph_lvm_sweep_classify,
            "bootwright_ceph_osd_filter_candidates": bootwright_ceph_osd_filter_candidates,
            "bootwright_json_list_probe": bootwright_json_list_probe,
            "bootwright_reclaim_device_operand": bootwright_reclaim_device_operand,
            "bootwright_reclaim_device_operand_probe": bootwright_reclaim_device_operand_probe,
        }
