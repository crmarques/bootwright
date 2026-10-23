#!/usr/bin/python
# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)
"""Execute a frozen Ceph core operation through isolated controller SSH."""

from __future__ import annotations


DOCUMENTATION = r"""
module: ceph_export
short_description: Execute one frozen Ceph core operation
version_added: "0.1.0"
description:
  - Uses strict host-key verification and a fixed privileged native worker.
  - Only qualified core services and concrete proved devices are accepted.
  - Observation never claims ownership or changes native configuration.
options:
  request:
    description: Frozen Ceph core request with durable runtime identity.
    type: dict
    required: true
  operation:
    description: The fixed lifecycle operation.
    type: str
    choices: [apply, observe]
    required: true
  digest:
    description: Digest of the frozen lifecycle request.
    type: str
    required: true
  material:
    description: Runner-owned private material file paths.
    type: dict
    required: true
  outputs:
    description:
      - Runner-owned private produced-material output paths.
      - C(client) receives a client action's generated CephX identity on apply and on a ready observe.
      - C(dashboard) receives a cluster action's dashboard administrator password on apply and on a ready observe.
    type: dict
    default: {}
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Observe the frozen Ceph export
  bootwright.core.ceph_export:
    request: '{{ bootwright_ceph_request }}'
    operation: observe
    digest: '{{ bootwright_ceph_digest }}'
    material: '{{ bootwright_ceph_material }}'
"""

RETURN = r"""
evidence:
  description: Bounded Ceph identity and readiness observations without credentials.
  type: dict
  returned: success
nativeError:
  description: Bounded redacted native cause and selected public coordinate.
  type: dict
  returned: failure
reason:
  description: Fixed refusal the block's caller names, without native output or credentials.
  type: str
  returned: failure
detail:
  description: Non-secret cause, a refusal name or an exception type and code site.
  type: str
  returned: failure
"""

import json

# Exact bool excludes numeric or truthy native result values.
# pylint: disable=unidiomatic-typecheck

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import native_diagnostics
from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_export_native, ceph_native


def main():
    module = AnsibleModule(
        argument_spec={
            "request": {"type": "dict", "required": True},
            "operation": {"type": "str", "required": True, "choices": ["apply", "observe"]},
            "digest": {"type": "str", "required": True},
            "material": {"type": "dict", "required": True, "no_log": True},
            "outputs": {"type": "dict", "default": {}, "no_log": True},
        },
        supports_check_mode=False,
    )
    params = module.params
    try:
        request = ceph_export_native.validate(params["request"])
        ceph_native.require(ceph_native.digest(params["digest"]))
        material = params["material"]
        gateway = request.get("gateway")
        tls = gateway is not None and gateway["tls"]
        ceph_native.require(set(material) == ({"sshKey", "knownHosts", "gatewayCertificate"} if tls else {"sshKey", "knownHosts"}), "material")
        ceph_native.require(set(params["outputs"]) == {"externalDetails"}, "material")
        transport = ceph_core.Transport(request["scope"], {key: material[key] for key in ("sshKey", "knownHosts")}, scope=True)
        private = {}
        if tls:
            private["certificate"] = ceph_native.read_regular(material["gatewayCertificate"], maximum=1 << 20).decode("ascii")
        bootstrap = next(host for host in request["scope"]["hosts"] if host["name"] == request["scope"]["bootstrap"])
        reply = transport.export(bootstrap, "export_" + params["operation"], request, private)
        ceph_native.require(isinstance(reply, dict) and set(reply) == {"changed", "private", "leader", "address"}
                            and type(reply["changed"]) is bool, "evidence")
        ceph_native.require(isinstance(reply["private"], str) and len(reply["private"].encode()) <= 1 << 20, "material")
        ceph_native.private_write(params["outputs"]["externalDetails"], reply["private"].encode())
        evidence = {"version": ceph_export_native.EVIDENCE_VERSION, "name": request["name"], "owner": request["scope"]["owner"],
                    "fsid": request["scope"]["fsid"], "request": params["digest"], "ready": True,
                    "leader": reply["leader"], "address": reply["address"], "keyType": request["keyType"],
                    "principals": {"client." + ceph_export_native.prefix(request) + "-" + suffix: caps for suffix, caps in request["principals"].items()},
                    "rgwUser": ceph_export_native.prefix(request) + "-ops" if gateway else "",
                    "entries": {entry["name"]: entry["kind"] for entry in json.loads(reply["private"])}}
        result = {"changed": reply["changed"], "evidence": evidence}
    except Exception as error:
        reason, detail = ceph_core.refusal(error, ceph_core.CLUSTER_REASONS + ceph_export_native.REASONS)
        failure = {"reason": reason}
        native_error = getattr(error, "native_error", None)
        if native_diagnostics.valid(native_error):
            failure["nativeError"] = native_error
        if detail:
            failure["detail"] = detail
        module.fail_json(msg="Ceph operation refused", **failure)
    module.exit_json(**result)


if __name__ == "__main__":
    main()
