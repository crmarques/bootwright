#!/usr/bin/env python3
import os
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ET


def run(args, input_text=None, check=True):
    result = subprocess.run(
        args,
        input=input_text,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    if check and result.returncode != 0:
        raise RuntimeError(
            "%s failed with rc=%d\nstdout:\n%s\nstderr:\n%s"
            % (" ".join(args), result.returncode, result.stdout, result.stderr)
        )
    return result


def virsh(uri, *args, input_text=None, check=True):
    return run(["virsh", "-c", uri, *args], input_text=input_text, check=check)


def dump_domain_xml(uri, domain):
    inactive = virsh(uri, "dumpxml", "--inactive", domain, check=False)
    if inactive.returncode == 0:
        return inactive.stdout
    return virsh(uri, "dumpxml", domain).stdout


def default_controller(root):
    os_element = root.find("os")
    if os_element is not None:
        type_element = os_element.find("type")
        if type_element is not None:
            arch = type_element.attrib.get("arch", "")
            machine = type_element.attrib.get("machine", "")
            if machine and "q35" in machine:
                return "sata"
            if arch and "aarch64" in arch:
                return "scsi"
    return "ide"


def select_target(devices, controller_type):
    for disk in devices.findall("disk"):
        target = disk.find("target")
        if target is None:
            continue
        bus = target.attrib.get("bus")
        if bus == "scsi":
            controller_type = "scsi"
        elif bus == "sata":
            controller_type = "sata"

    if controller_type == "ide":
        return "hdc", "ide"
    return "sdx", controller_type


def select_unit(devices, target_bus):
    free_units = set(range(100))
    for disk in devices.findall("disk"):
        target = disk.find("target")
        if target is None or target.attrib.get("bus") != target_bus:
            continue
        address = disk.find("address")
        if address is None:
            continue
        unit = address.attrib.get("unit")
        if unit is None:
            continue
        try:
            free_units.discard(int(unit))
        except ValueError:
            continue
    if not free_units:
        raise RuntimeError("no free %s bus unit found" % target_bus)
    return min(free_units)


def remove_cdroms(devices):
    removed = False
    for disk in list(devices.findall("disk")):
        if disk.attrib.get("device") == "cdrom":
            devices.remove(disk)
            removed = True
    return removed


def set_boot_order(root, boot_order):
    if boot_order not in {"disk-first", "cdrom-first"}:
        raise RuntimeError("unsupported libvirt media boot order: %s" % boot_order)

    for element in root.findall("./devices/*"):
        for boot in list(element.findall("boot")):
            element.remove(boot)

    os_element = root.find("os")
    if os_element is None:
        os_element = ET.SubElement(root, "os")
    for boot in list(os_element.findall("boot")):
        os_element.remove(boot)

    devices = ["cdrom", "hd"] if boot_order == "cdrom-first" else ["hd", "cdrom"]
    for dev in devices:
        boot = ET.SubElement(os_element, "boot")
        boot.set("dev", dev)


def add_cdrom(devices, source_iso, target_dev, target_bus, unit):
    disk = ET.SubElement(devices, "disk", {"type": "file", "device": "cdrom"})

    target = ET.SubElement(disk, "target")
    target.set("dev", target_dev)
    target.set("bus", target_bus)

    address = ET.SubElement(disk, "address")
    address.set("type", "drive")
    address.set("controller", "0")
    address.set("bus", "0")
    address.set("target", "0")
    address.set("unit", str(unit))

    driver = ET.SubElement(disk, "driver")
    driver.set("name", "qemu")
    driver.set("type", "raw")

    source = ET.SubElement(disk, "source")
    source.set("file", source_iso)

    ET.SubElement(disk, "readonly")


def build_domain_xml(domain_xml, source_iso, boot_order):
    root = ET.fromstring(domain_xml)
    devices = root.find("devices")
    if devices is None:
        raise RuntimeError("domain XML is missing devices")

    controller_type = default_controller(root)
    remove_cdroms(devices)
    target_dev, target_bus = select_target(devices, controller_type)
    unit = select_unit(devices, target_bus)
    add_cdrom(devices, source_iso, target_dev, target_bus, unit)
    set_boot_order(root, boot_order)
    return ET.tostring(root, encoding="unicode")


def define_domain(uri, xml_text):
    tmpdir = os.environ.get("TMPDIR") or os.environ.get("TMP") or "/var/tmp"
    tmp = tempfile.NamedTemporaryFile(
        mode="w",
        prefix="bootwright-libvirt-media-",
        suffix=".xml",
        dir=tmpdir,
        delete=False,
    )
    try:
        with tmp:
            tmp.write(xml_text)
        virsh(uri, "define", tmp.name)
    finally:
        try:
            os.unlink(tmp.name)
        except FileNotFoundError:
            pass


def main():
    if len(sys.argv) not in {4, 5}:
        print(
            "usage: insert_libvirt_media.py <libvirt-uri> <domain> <iso> [disk-first|cdrom-first]",
            file=sys.stderr,
        )
        return 2

    uri, domain, source_iso = sys.argv[1:4]
    boot_order = "disk-first"
    if len(sys.argv) == 5:
        boot_order = sys.argv[4]
    if not os.path.isfile(source_iso):
        print("source ISO does not exist: %s" % source_iso, file=sys.stderr)
        return 1

    domain_xml = dump_domain_xml(uri, domain)
    updated_xml = build_domain_xml(domain_xml, source_iso, boot_order)
    define_domain(uri, updated_xml)
    print("changed=true")
    return 0


if __name__ == "__main__":
    sys.exit(main())
