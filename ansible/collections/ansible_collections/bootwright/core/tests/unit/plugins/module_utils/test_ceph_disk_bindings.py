"""An owned Ceph disk cannot be redirected by changing a domain definition."""
from xml.etree import ElementTree

import pytest

from ansible_collections.bootwright.core.plugins.module_utils.substrate_libvirt import disk_media, domain_disk_bindings, domain_metadata


XML = """<domain><devices>
<disk type="file" device="disk"><source file="/owned/root.qcow2"/><target dev="vda" bus="virtio"/></disk>
<disk type="file" device="disk"><source file="/owned/data.qcow2"/><target dev="vdb" bus="virtio"/><serial>bw1234</serial></disk>
</devices></domain>"""
DISKS = [
    {"path": "/owned/root.qcow2", "target": "vda"},
    {"path": "/owned/data.qcow2", "target": "vdb", "serial": "bw1234"},
]


def test_frozen_volume_target_and_serial_are_required():
    assert domain_disk_bindings(ElementTree.fromstring(XML), DISKS)


@pytest.mark.parametrize("before,after", [
    ("/owned/data.qcow2", "/foreign/data.qcow2"),
    ("bw1234", "bw5678"),
    ('dev="vdb"', 'dev="vda"'),
    ('bus="virtio"', 'bus="scsi"'),
    ('<serial>bw1234</serial>', ''),
])
def test_redirected_or_ambiguous_disks_are_foreign(before, after):
    assert not domain_disk_bindings(ElementTree.fromstring(XML.replace(before, after)), DISKS)


def ssd_domain():
    xml = XML.replace('<devices>', '<devices><controller type="scsi" index="0" model="virtio-scsi"/>')
    xml = xml.replace('dev="vdb" bus="virtio"', 'dev="sda" bus="scsi" rotation_rate="1"')
    xml = xml.replace('<serial>bw1234</serial>', '<serial>bw1234</serial><address type="drive" controller="0" bus="0" target="0" unit="0"/>')
    disks = [dict(DISKS[0]), dict(DISKS[1], name="data", target="sda", bus="scsi", media="ssd", rotationRate=1)]
    return xml, disks


def test_explicit_ssd_proves_frozen_bus_rotation_address_and_actual_evidence():
    xml, disks = ssd_domain()
    root = ElementTree.fromstring(xml)
    assert domain_disk_bindings(root, disks)
    assert disk_media(root, disks[1]) == {"bus": "scsi", "target": "sda", "rotationRate": 1}

    def runner(argv, **kwargs):
        assert "dumpxml" in argv
        return 0, xml, ""
    assert domain_metadata(runner, "qemu:///system", "synthetic", disks=disks)["diskMedia"] == {
        "data": {"bus": "scsi", "target": "sda", "rotationRate": 1}}


@pytest.mark.parametrize("before,after", [
    ('rotation_rate="1"', 'rotation_rate="7200"'),
    ('rotation_rate="1"', ''),
    ('bus="scsi"', 'bus="virtio"'),
    ('model="virtio-scsi"', 'model="lsilogic"'),
    ('index="0"', 'index="1"'),
    ('unit="0"', 'unit="1"'),
    ('target="0"', 'target="1"'),
    ('controller="0"', 'controller="1"'),
])
def test_ssd_presentation_drift_refuses(before, after):
    xml, disks = ssd_domain()
    assert not domain_disk_bindings(ElementTree.fromstring(xml.replace(before, after)), disks)


def test_optical_media_uses_separate_scsi_unit_and_cannot_redirect_data():
    xml, disks = ssd_domain()
    media = '<disk type="file" device="cdrom"><target dev="sdx" bus="scsi"/><address type="drive" controller="0" bus="0" target="0" unit="1"/></disk>'
    root = ElementTree.fromstring(xml.replace('</devices>', media + '</devices>'))
    assert domain_disk_bindings(root, disks)
    assert not domain_disk_bindings(ElementTree.fromstring(xml.replace('</devices>', media.replace('unit="1"', 'unit="0"') + '</devices>')), disks)
