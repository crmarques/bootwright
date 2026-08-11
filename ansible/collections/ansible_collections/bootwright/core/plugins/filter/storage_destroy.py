import hashlib
import json
import re
from collections.abc import Mapping

from ansible.errors import AnsibleFilterError


_PROOF = "ceph-lvm-quiet-v2"
_SCAN_SCOPE = "all-node-pvs"
_FSID = re.compile(r"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")
_MISSING = object()


def _evidence_expected_string(value, name):
    if not isinstance(value, str) or not value.strip():
        raise AnsibleFilterError(f"Ceph ownership evidence needs a nonempty {name}")
    return value.strip()


def _evidence_document(raw_json, present, path_safe, label):
    result = {
        "present": present,
        "readable": False,
        "identityValid": False,
        "fsid": "",
        "fsidEmpty": False,
        "fsidValid": False,
        "valid": False,
        "blockers": [],
    }
    if not isinstance(present, bool):
        raise AnsibleFilterError("Ceph ownership evidence present must be a boolean")
    if not isinstance(path_safe, bool):
        raise AnsibleFilterError("Ceph ownership evidence path-safe verdict must be a boolean")
    if not present:
        result["blockers"].append(f"{label} is missing")
        return result, None
    if not path_safe:
        result["blockers"].append(f"{label} path must be an inspected regular non-symlink file")
        return result, None
    if not isinstance(raw_json, (str, bytes, bytearray)):
        raise AnsibleFilterError(f"{label} JSON must be text or bytes")
    if not raw_json or (isinstance(raw_json, str) and not raw_json.strip()):
        result["blockers"].append(f"{label} is empty")
        return result, None
    try:
        document = json.loads(raw_json)
    except (ValueError, RecursionError):
        result["blockers"].append(f"{label} is not valid JSON")
        return result, None
    if not isinstance(document, Mapping):
        result["blockers"].append(f"{label} must be a JSON object")
        return result, None
    result["readable"] = True
    return result, document


def _effective_evidence_role(value):
    if value is _MISSING:
        return "owner"
    if not isinstance(value, str):
        return ""
    return value.strip() or "owner"


def _evidence_fsid(value):
    if value is _MISSING:
        return "", False, True
    if not isinstance(value, str):
        return "", False, False
    value = value.strip()
    return value, bool(_FSID.fullmatch(value)), not value


def bootwright_ceph_controller_owner_evidence(raw_json, present, path_safe, expected_context, expected_cluster, expected_seed):
    expected_context = _evidence_expected_string(expected_context, "expected context")
    expected_cluster = _evidence_expected_string(expected_cluster, "expected cluster")
    expected_seed = _evidence_expected_string(expected_seed, "expected seed")
    label = "controller ownership record"
    result, document = _evidence_document(raw_json, present, path_safe, label)
    if document is None:
        return result
    identity_checks = (
        (document.get("apiVersion") == "bootwright.io/ownership/v1alpha1", f"{label} apiVersion must be bootwright.io/ownership/v1alpha1"),
        (document.get("kind") == "storage-cluster", f"{label} kind must be storage-cluster"),
        (document.get("name") == expected_cluster, f"{label} name must be {expected_cluster}"),
        (document.get("owner") == "bootwright", f"{label} owner must be bootwright"),
        (_effective_evidence_role(document.get("role", _MISSING)) == "owner", f"{label} role must resolve to owner"),
        (document.get("context") == expected_context, f"{label} context must be {expected_context}"),
        (document.get("cluster") == expected_cluster, f"{label} cluster must be {expected_cluster}"),
        (document.get("host") == expected_seed, f"{label} host must be {expected_seed}"),
    )
    for valid, blocker in identity_checks:
        if not valid:
            result["blockers"].append(blocker)
    attributes = document.get("attributes")
    if not isinstance(attributes, Mapping):
        result["blockers"].append(f"{label} attributes must be a JSON object")
        attributes = {}
    if attributes.get("seedHost") != expected_seed:
        result["blockers"].append(f"{label} attributes.seedHost must be {expected_seed}")
    result["identityValid"] = len(result["blockers"]) == 0
    result["fsid"], result["fsidValid"], result["fsidEmpty"] = _evidence_fsid(attributes.get("fsid", _MISSING))
    if not result["fsidValid"]:
        result["blockers"].append(f"{label} attributes.fsid must be a UUID")
    result["valid"] = result["identityValid"] and result["fsidValid"]
    return result


def bootwright_ceph_host_marker_evidence(raw_json, present, path_safe, expected_cluster):
    expected_cluster = _evidence_expected_string(expected_cluster, "expected cluster")
    label = "Ceph ownership marker"
    result, document = _evidence_document(raw_json, present, path_safe, label)
    if document is None:
        return result
    identity_checks = (
        (document.get("manager") == "bootwright", f"{label} manager must be bootwright"),
        (document.get("cluster") == expected_cluster, f"{label} cluster must be {expected_cluster}"),
    )
    for valid, blocker in identity_checks:
        if not valid:
            result["blockers"].append(blocker)
    result["identityValid"] = len(result["blockers"]) == 0
    result["fsid"], result["fsidValid"], result["fsidEmpty"] = _evidence_fsid(document.get("fsid", _MISSING))
    if not result["fsidValid"]:
        result["blockers"].append(f"{label} fsid must be a UUID")
    result["valid"] = result["identityValid"] and result["fsidValid"]
    return result


def _required_string(entry, key):
    value = entry.get(key)
    if not isinstance(value, str) or not value.strip():
        raise AnsibleFilterError(f"storage destroy attestation needs a nonempty {key}")
    return value.strip()


def _result_rc(entry, key):
    value = entry.get(key)
    if value is None:
        return None
    if not isinstance(value, Mapping):
        raise AnsibleFilterError(f"storage destroy attestation {key} must be a mapping")
    rc = value.get("rc")
    if rc is None:
        return None
    if isinstance(rc, bool) or not isinstance(rc, int):
        raise AnsibleFilterError(f"storage destroy attestation {key}.rc must be an integer")
    return rc


def _reason(entry, node, key, fallback):
    value = entry.get(key, fallback)
    if not isinstance(value, str) or not value.strip():
        value = fallback
    return f"{node}: {value.strip()}"


def _explicit_unreachable(entry, key):
    value = entry.get(key)
    if value is None:
        return False
    if not isinstance(value, Mapping):
        raise AnsibleFilterError(f"storage destroy attestation {key} must be a mapping")
    unreachable = value.get("unreachable", False)
    if not isinstance(unreachable, bool):
        raise AnsibleFilterError(f"storage destroy attestation {key}.unreachable must be a boolean")
    return unreachable


def _unreachable_reason(entry, node):
    for key in ("bootwright_ceph_destroy_completion", "bootwright_ceph_sweep_verify"):
        value = entry.get(key)
        if isinstance(value, Mapping):
            message = value.get("msg")
            if isinstance(message, str) and message.strip():
                return f"{node}: {message.strip()}"
    return f"{node}: connection lost during teardown after it began"


def _node_result(node, host):
    return {
        "name": node,
        "host": host,
        "outcome": "incomplete",
        "proofVersion": "",
        "scanScope": "",
        "scannedRows": None,
        "ownedSurvivors": None,
        "scanDigest": "",
        "lvmScanRC": None,
        "completionRC": None,
        "absenceClass": "",
        "reason": f"{node}: terminal Ceph LVM proof and remote completion witness were not both recorded",
    }


def bootwright_storage_destroy_attestation(entries, terminal, allow_unreachable):
    if not isinstance(entries, list):
        raise AnsibleFilterError("storage destroy attestation entries must be a list")
    if not isinstance(terminal, bool) or not isinstance(allow_unreachable, bool):
        raise AnsibleFilterError("storage destroy attestation controls must be booleans")
    grouped = {}
    for entry in entries:
        if not isinstance(entry, Mapping):
            raise AnsibleFilterError("storage destroy attestation entry must be a mapping")
        cluster = _required_string(entry, "bootwright_storage_cluster_name")
        node = _required_string(entry, "bootwright_storage_node_name")
        host = _required_string(entry, "inventory_hostname")
        probe = entry.get("bootwright_storage_reachable_probe")
        if not isinstance(probe, Mapping):
            raise AnsibleFilterError("storage destroy attestation reachability probe must be a mapping")
        result = grouped.setdefault(
            cluster,
            {
                "name": cluster,
                "fsid": "",
                "nodes": [],
            },
        )
        fsid = entry.get("bootwright_ceph_settle_owned_fsid", "")
        if not isinstance(fsid, str):
            raise AnsibleFilterError("storage destroy attestation fsid must be a string")
        fsid = fsid.strip()
        if fsid:
            if not _FSID.fullmatch(fsid):
                raise AnsibleFilterError("storage destroy attestation fsid must be a UUID")
            if result["fsid"] and result["fsid"] != fsid:
                raise AnsibleFilterError("storage destroy attestation nodes disagree on fsid")
            result["fsid"] = fsid
        node_result = _node_result(node, host)
        unreachable = probe.get("unreachable", False)
        if not isinstance(unreachable, bool):
            raise AnsibleFilterError("storage destroy attestation unreachable verdict must be a boolean")
        if unreachable:
            absent = entry.get("bootwright_storage_node_absent", False)
            if not isinstance(absent, bool):
                raise AnsibleFilterError("storage destroy attestation absence verdict must be a boolean")
            if allow_unreachable and absent:
                node_result["outcome"] = "skipped"
                node_result["absenceClass"] = "ssh-unreachable"
                node_result["reason"] = _reason(
                    entry, node, "bootwright_storage_node_refusal", "the node did not answer"
                )
            result["nodes"].append(node_result)
            continue
        completion_rc = _result_rc(entry, "bootwright_ceph_destroy_completion")
        proof_rc = _result_rc(entry, "bootwright_ceph_sweep_verify")
        proof_rows = entry.get("bootwright_ceph_sweep_verify_rows")
        survivors = entry.get("bootwright_ceph_sweep_survivors")
        if terminal and completion_rc == 0 and proof_rc == 0 and isinstance(proof_rows, list) and isinstance(survivors, list):
            proof = entry.get("bootwright_ceph_sweep_verify")
            stdout = proof.get("stdout", "")
            if not isinstance(stdout, str):
                raise AnsibleFilterError("storage destroy attestation final scan stdout must be a string")
            node_result.update(
                {
                    "outcome": "completed" if len(survivors) == 0 else "incomplete",
                    "proofVersion": _PROOF,
                    "scanScope": _SCAN_SCOPE,
                    "scannedRows": len(proof_rows),
                    "ownedSurvivors": len(survivors),
                    "scanDigest": hashlib.sha256(stdout.encode("utf-8")).hexdigest(),
                    "lvmScanRC": proof_rc,
                    "completionRC": completion_rc,
                    "reason": "" if len(survivors) == 0 else f"{node}: final whole-node scan still found owned Ceph LVM",
                }
            )
            result["nodes"].append(node_result)
            continue
        if terminal and allow_unreachable and (
            _explicit_unreachable(entry, "bootwright_ceph_destroy_completion")
            or _explicit_unreachable(entry, "bootwright_ceph_sweep_verify")
        ):
            node_result["outcome"] = "skipped"
            node_result["absenceClass"] = "connection-lost"
            node_result["reason"] = _unreachable_reason(entry, node)
            result["nodes"].append(node_result)
            continue
        result["nodes"].append(node_result)
    clusters = []
    for name in sorted(grouped):
        result = grouped[name]
        result["nodes"].sort(key=lambda item: item["name"])
        clusters.append(result)
    return {"schemaVersion": 1, "clusters": clusters}


class FilterModule:
    def filters(self):
        return {
            "bootwright_ceph_controller_owner_evidence": bootwright_ceph_controller_owner_evidence,
            "bootwright_ceph_host_marker_evidence": bootwright_ceph_host_marker_evidence,
            "bootwright_storage_destroy_attestation": bootwright_storage_destroy_attestation,
        }
