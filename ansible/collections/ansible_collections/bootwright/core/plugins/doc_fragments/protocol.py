"""Documentation the adapter protocol modules share.

Each constant holds options repeated word for word across the protocol stubs. A
module names the ones it uses with extends_documentation_fragment and keeps its
own wording for every option it words differently.
"""

from __future__ import annotations


class ModuleDocFragment:

    RETURN = r"""
changed:
  description: Always false; publication performs no host effect.
  returned: always
  type: bool
"""

    COMPLETION = r"""
options:
  group:
    description: The presentation group a progress phase reports.
    type: str
    required: false
  status:
    description: The status a progress phase reports for its group.
    type: str
    required: false
  progress:
    description:
      - The bytes a running group has proved, as exactly C(completed) and C(total),
        both non-negative integers with C(completed) at most C(total). A value
        that is not exactly that is left out of the record, because progress
        only reports and never fails the effect it describes.
    type: dict
    required: false
  outcome:
    description: The terminal outcome a completion phase publishes.
    type: str
    required: false
  digest:
    description: The frozen request digest the evidence must name.
    type: str
    required: false
"""

    PHASE = r"""
options:
  phase:
    description: The capability phase being published.
    type: str
    required: true
"""

    OBSERVED = r"""
options:
  observed:
    description:
      - Whether this publication is a read-only observation, which may report
        evidence that proves no postcondition rather than failing.
    type: bool
    required: false
"""

    REMOVED = r"""
options:
  removed:
    description: Whether the completion proves removal rather than presence.
    type: bool
    required: false
"""

    CEPH_COMPLETION = r"""
options:
  outcome:
    description: Completed native outcome.
    type: str
    choices:
      - changed
      - unchanged
  digest:
    description: Frozen lifecycle request digest.
    type: str
  observed:
    description: Whether this is a read-only observation.
    type: bool
    default: false
  removed:
    description: Whether this completion must prove removal.
    type: bool
    default: false
"""

    CEPH_PHASE = r"""
options:
  phase:
    description: Shared protocol phase.
    type: str
    required: true
    choices:
      - loaded
      - refused
      - completed
"""

    DETAIL = r"""
options:
  detail:
    description: Non-secret refusal cause, a refusal name or an exception type and code site.
    type: str
  native_error:
    description: Bounded redacted native cause and selected public coordinate.
    type: dict
"""

    OPERATION_PHASE = r"""
options:
  phase:
    description: The operation phase being published.
    type: str
    required: true
"""
