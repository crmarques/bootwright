#!/usr/bin/python
"""Execute a frozen Ceph core operation through isolated controller SSH."""

from __future__ import annotations

DOCUMENTATION = r"""
module: ceph_cluster
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
    description:
      - The fixed lifecycle operation.
      - C(suspend_prepare), C(suspend) and C(resume) run only a CephSignOn's oauth2-proxy unit.
    type: str
    choices:
      - apply
      - observe
      - destroy
      - suspend_prepare
      - suspend
      - resume
    required: true
  digest:
    description: Digest of the frozen lifecycle request.
    type: str
    required: true
  material:
    description:
      - Runner-owned private material file paths.
      - C(retirement) set to C(tiebreaker) selects a provided stretch tiebreaker's removal on destroy, or its read-only observation on observe.
      - C(reading) set to C(unrecorded) marks a cluster observation no operation records, which alone lists the cluster's pools and the
        OSDs no declared service places.
      - C(reading) set to C(hosts) reads only each cluster host's state, once, for every resource observation of one C(diff --observe)
        whose request has the same reading scope; it publishes that host reading as its evidence.
      - C(hosts) carries such a host reading, as JSON; a resource observation then replays it in place of its own host state reads.
      - C(signOn) set to C(suspended) observes a suspended sign-on; C(signOnBaseline) carries, as JSON, the before-state a suspension
        recorded before its first effect.
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
- name: Observe the frozen Ceph cluster
  bootwright.core.ceph_cluster:
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

from ansible.module_utils.basic import AnsibleModule
from ansible_collections.bootwright.core.plugins.module_utils import native_diagnostics
from ansible_collections.bootwright.core.plugins.module_utils import ceph_core, ceph_transport

# The closed, non-secret values material may carry beside its paths. AnsibleModule masks every leaf of a no_log option as a
# substring of each result string, so a selector left masked would censor the refusal names and evidence versions that contain it.
SELECTORS = frozenset(("tiebreaker", "managers", "unrecorded", "hosts", "suspended"))


def main():
    module = AnsibleModule(
        argument_spec={
            "request": {"type": "dict", "required": True},
            "operation": {"type": "str", "required": True, "choices": ["apply", "observe", "destroy", "suspend_prepare", "suspend", "resume"]},
            "digest": {"type": "str", "required": True},
            "material": {"type": "dict", "required": True, "no_log": True},
            "outputs": {"type": "dict", "default": {}, "no_log": True},
        },
        supports_check_mode=False,
    )
    params = module.params
    try:
        # The retirement selector is a value beside the material paths, never a path the transport reads.
        material = dict(params["material"])
        retirement = material.pop("retirement", None)
        reading = material.pop("reading", None)
        shared = ceph_core.shared_hosts(material.pop("hosts", None))
        # The cluster apply's run deadline in seconds: masked as a material leaf, its number would be censored in every result string.
        deadline = ceph_core.run_deadline(material.pop("deadline", None))
        signon = material.pop("signOn", None)
        recorded = material.pop("signOnBaseline", None)
        if isinstance(recorded, str):
            module.no_log_values.discard(recorded)
        baseline = ceph_core.suspension_baseline(recorded)
        for selector in (retirement, reading, signon):
            if selector in SELECTORS:
                module.no_log_values.discard(selector)
        if deadline is not None:
            module.no_log_values.discard(str(deadline))
        transport = ceph_transport.Transport(params["request"], material)
        result = ceph_core.execute(params["request"], params["operation"], params["digest"], transport, params["outputs"], retirement=retirement,
                                   reading=reading, shared=shared, deadline=deadline, signon=signon, baseline=baseline)
    except Exception as error:
        reason, detail = ceph_core.refusal(error, ceph_core.CLUSTER_REASONS)
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
