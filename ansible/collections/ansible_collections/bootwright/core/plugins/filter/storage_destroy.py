import hashlib
import re
from collections.abc import Mapping

from ansible.errors import AnsibleFilterError


_PROOF = "ceph-lvm-quiet-v2"
_SCAN_SCOPE = "all-node-pvs"
_FSID = re.compile(r"^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$")


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
        return {"bootwright_storage_destroy_attestation": bootwright_storage_destroy_attestation}
