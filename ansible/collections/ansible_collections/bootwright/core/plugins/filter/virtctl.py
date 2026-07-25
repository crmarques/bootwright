from __future__ import annotations

import re


_VIRTCTL_VERSION = re.compile(r'GitVersion:"?(v[0-9][^",}\s]*)')


def bootwright_virtctl_version(value):
    if not isinstance(value, str):
        return ""
    match = _VIRTCTL_VERSION.search(value)
    if match is None:
        return ""
    return match.group(1)


class FilterModule:
    def filters(self):
        return {
            "bootwright_virtctl_version": bootwright_virtctl_version,
        }
