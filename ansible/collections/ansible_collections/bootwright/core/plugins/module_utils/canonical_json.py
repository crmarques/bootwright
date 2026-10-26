"""The one canonical JSON encoding of the collection's controller and managed-host sites.

Sorted members, no insignificant white space and ASCII only, so a digest or a
byte comparison of a request, record or wire frame is the same everywhere.
Go's record decoder (``internal/adapterprotocol``) admits these bytes, which
``tests/unit/goldens/canonical_json_records.json`` and that decoder's test hold.
Go's ``json.Marshal`` escapes ``<``, ``>`` and ``&`` and writes non-ASCII raw,
so the two encoders agree only where those do not occur; ``internal/canonicaljson``
pins that difference. A shipped script that runs alone, or an encoding that
differs on purpose, is a named exception of ``test_python_sites.py``.
"""

from __future__ import annotations

import json


def canonical(value):
    """The canonical encoding of a JSON value, as ASCII bytes."""
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode("ascii")
