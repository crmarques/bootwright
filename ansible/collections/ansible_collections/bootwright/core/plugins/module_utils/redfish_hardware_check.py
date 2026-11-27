"""Read the selected BMC without changing its machine or publishing credentials."""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.module_utils import (
    machine_identity, redfish_control, redfish_root_volume, redfish_storage,
)


class InspectionClient(redfish_control.Client):
    def fetch(self, reference="", method="GET", payload=None, headers=None,
              timeout=redfish_control.REQUEST_TIMEOUT, doing=""):
        if method != "GET" or payload is not None:
            raise redfish_control.ControllerError("hardware inspection only reads Redfish resources")
        url = self._own(reference or self.endpoint, "inspecting")
        self.require_system_reference(url)
        return super().fetch(url, method, headers=headers, timeout=timeout, doing=doing)


def row(name, status, summary):
    return {"id": name, "status": status, "summary": summary}


def inspect(client, expected):
    tls = row("management-tls", "unknown", "The controller did not answer on the verified connection.")
    identity = row("identity", "unknown", "The complete physical identity could not be proved.")
    try:
        client.system()
    except redfish_control.UnverifiedCertificate:
        tls = row("management-tls", "mismatch", "The management certificate failed verification.")
    except redfish_control.ControllerError:
        pass
    else:
        tls = row("management-tls", "observed", "The ComputerSystem answered over verified HTTPS.")
        try:
            observation = redfish_control.hardware_observation(client)
        except redfish_control.ControllerError:
            observation = {}
        if observation and not observation.get("failures"):
            verdict = machine_identity.verdict(
                observation, expected=expected.get("expected", []),
                pinned_uuid_base64=expected.get("uuid_pin", ""), pinned_serial_base64=expected.get("serial_pin", ""),
            )
            if verdict["pinned"] and verdict["declared"]:
                identity = row("identity", "observed", "The full MAC set and any recorded system pins matched.")
            elif not verdict["pinned"] or verdict["unproved"] == "addresses":
                identity = row("identity", "mismatch", "The reported system identity or complete MAC set differs from the declaration.")
    rows = [tls, identity]
    if identity["status"] == "observed":
        rows.extend(capabilities(client))
    else:
        rows.extend(row(name, "unknown", "An exact target identity is required to report this observation.")
                    for name in ("power", "virtual-media", "firmware", "direct-disks", "logical-volumes"))
    rows.append(row("active-rehearsal", "requires-exercise",
                    "Verify media fetch, boot, interruption recovery, and authorized cleanup on an isolated spare."))
    return rows


def capabilities(client):
    power = client.power_state()
    yield row("power", "observed", "The system is " + power.lower() + ".") if power in ("On", "Off") else \
        row("power", "unknown", "The controller did not report a recognized power state.")
    try:
        if not client.media_member():
            raise redfish_control.ControllerError("no optical media device")
        present, _image = client.media_state()
    except redfish_control.ControllerError:
        yield row("virtual-media", "unknown", "The virtual media device or its insertion state could not be read.")
    else:
        yield row("virtual-media", "observed", "Media is present." if present else "The virtual media device is empty.")
    system = client.system()
    bios = system.get("BiosVersion")
    boot = system.get("Boot", {})
    modes = boot.get("BootSourceOverrideMode@Redfish.AllowableValues", []) if isinstance(boot, dict) else []
    capabilities_read = [name for name in ("UEFI", "Legacy") if isinstance(modes, list) and name in modes]
    if isinstance(bios, str) and bios.isascii() and bios.isprintable() and 0 < len(bios) <= 128:
        summary = "The system reported a BIOS version"
        if capabilities_read:
            summary += ", advertised boot modes " + ", ".join(capabilities_read)
        yield row("firmware", "observed", summary + ".")
    else:
        yield row("firmware", "unknown", "The ComputerSystem did not report a usable BIOS version.")
    try:
        drives = list(redfish_storage.plain_drives(client, "read direct drive inventory"))
        identified = sum(1 for drive in drives if exact_drive(drive))
    except (redfish_control.ControllerError, redfish_storage.Unqualified):
        yield row("direct-disks", "unknown", "The complete direct-drive inventory is unavailable, including possible logical volumes.")
    else:
        status = "observed" if identified == len(drives) and drives else "unknown"
        yield row("direct-disks", status, "%d of %d direct drives reported a durable identity and exact capacity." % (identified, len(drives)))

    yield logical_volumes(client)


def logical_volumes(client):
    """Inspect only an advertised inventory and keep the install proof independent."""
    system = client.system()
    if "Storage" in system:
        source = "Storage"
    elif "Storages" in system:
        source = "Storages"
    else:
        return row("logical-volumes", "unknown", "The ComputerSystem advertised neither a Storage nor a Storages link.")
    try:
        count, candidates = redfish_root_volume.inspect(client, source)
    except redfish_control.ControllerError:
        return row("logical-volumes", "unknown", "The complete inventory from the advertised %s link could not be read." % source)
    if not count:
        return row("logical-volumes", "unknown", "The readable %s inventory advertised no logical volumes." % source)
    summary = "%d of %d logical volumes from %s report an exact byte capacity and an NAA not duplicated in the inventory." % (len(candidates), count, source)
    if len(candidates) == 1:
        summary += " Candidate: storageId={storageId} volumeId={volumeId} wwn={wwn} sizeBytes={sizeBytes}.".format(**candidates[0])
    if source == "Storages":
        summary += " This link is inspection only."
    return row("logical-volumes", "observed" if candidates else "unknown", summary)


def exact_drive(drive):
    capacity = drive.get("CapacityBytes")
    serial = drive.get("SerialNumber")
    return (isinstance(capacity, int) and not isinstance(capacity, bool) and capacity > 0
            and (redfish_storage.naa(drive) or isinstance(serial, str) and redfish_storage.SERIAL.fullmatch(serial)))
