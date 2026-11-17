"""Derives a device's identifier through ceph-volume's own code from supplied udev properties; the source runs alone inside the image."""

from __future__ import annotations

import json
import sys
import types


def main():
    from ceph_volume.util.device import Device
    from ceph_volume.util import disk
    value = json.loads(sys.argv[1])
    if set(value) != {"path", "environment"}:
        raise AssertionError
    original = disk.UdevData

    class OriginalInput:
        def __init__(self, path):
            if path != value["path"]:
                raise AssertionError
            self.environment = dict(value["environment"])
    try:
        disk.UdevData = OriginalInput
        identity = Device._get_device_id(types.SimpleNamespace(path=value["path"]))
    finally:
        disk.UdevData = original
    if not isinstance(identity, str):
        raise AssertionError
    sys.stdout.write(json.dumps({"version": "ceph-host-device-id-v1", "deviceID": identity,
                                 "dependencyInput": "ceph_volume.util.disk.UdevData.environment"}, separators=(",", ":")) + "\n")


if __name__ == "__main__":
    main()
