"""Name a controller adapter's refusal to the runner before the task fails."""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.module_utils.controller_channel import (
    emit,
)
from ansible_collections.bootwright.core.plugins.module_utils.controller_files import (
    AcquisitionRefused,
    Unreleased,
)


def record(error, source=None):
    """The refused record error names, or None: the release-stamp refusal or an
    acquisition's class with the source it was acquiring. No exception text is
    ever part of it."""
    if isinstance(error, Unreleased):
        return {"phase": "refused", "reason": "release-stamp"}
    if isinstance(error, AcquisitionRefused):
        source = error.source or source
        if error.reason and isinstance(source, str) and source:
            return {"phase": "refused", "reason": error.reason, "source": source}
        return None
    return None


def name(error, source=None, warn=None):
    """Name error to the runner without waiting for an acknowledgement, and
    hand its raw first line only to warn, the action plugin's Display warning:
    Ansible workers replace sys.stderr, and Display reaches the private setup
    run or attempt output. Without the record the run still fails, only with
    the generic remedy."""
    detail = getattr(error, "detail", "")
    if warn is not None and isinstance(error, AcquisitionRefused) and detail:
        warn("controller adapter: " + detail)
    named = record(error, source)
    if named is None:
        return
    try:
        emit(named)
    except (OSError, ValueError):
        pass
