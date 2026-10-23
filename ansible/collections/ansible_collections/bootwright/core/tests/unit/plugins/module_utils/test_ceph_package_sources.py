"""Package metadata admits the comps document type a real RHEL 9.8 DVD tree serves, and no other."""

from __future__ import annotations

import gzip
import hashlib

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_native, ceph_package_sources as sources

# The first lines of the RHEL 9.8 DVD BaseOS comps-BaseOS.x86_64.xml, byte for byte.
DVD_COMPS_HEAD = (b"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE comps\n  PUBLIC '-//Red Hat, Inc.//DTD Comps info//EN'\n  'comps.dtd'>\n"
                  b"<comps>\n")
COMPS_BODY = (b"  <group>\n    <id>core</id>\n    <name>Core</name>\n    <packagelist>\n"
              b"      <packagereq type=\"mandatory\">bash</packagereq>\n    </packagelist>\n  </group>\n</comps>\n")
DVD_COMPS = DVD_COMPS_HEAD + COMPS_BODY
ONE_LINE_COMPS = (b'<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE comps PUBLIC "-//Red Hat, Inc.//DTD Comps info//EN" "comps.dtd"><comps>\n'
                  + COMPS_BODY)
PRIMARY = (b'<?xml version="1.0" encoding="UTF-8"?>\n<metadata xmlns="http://linux.duke.edu/metadata/common" '
           b'xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="0">\n</metadata>\n')
FILELISTS = b'<?xml version="1.0" encoding="UTF-8"?>\n<filelists xmlns="http://linux.duke.edu/metadata/filelists" packages="0">\n</filelists>\n'
OTHER = b'<?xml version="1.0" encoding="UTF-8"?>\n<otherdata xmlns="http://linux.duke.edu/metadata/other" packages="0">\n</otherdata>\n'
MODULES = b"---\ndocument: modulemd-defaults\nversion: 1\ndata:\n  module: nodejs\n  stream: \"18\"\n...\n"
TREEINFO = b"""[checksums]
images/efiboot.img = sha256:0000000000000000000000000000000000000000000000000000000000000000

[general]
; WARNING.0 = This section provides compatibility with pre-productmd treeinfos.
; WARNING.1 = Read productmd documentation for details about new format.
arch = x86_64
family = Red Hat Enterprise Linux
name = Red Hat Enterprise Linux 9.8
packagedir = AppStream/Packages
platforms = x86_64,xen
repository = AppStream
timestamp = 1746900000
variant = AppStream
variants = AppStream,BaseOS
version = 9.8

[header]
type = productmd.treeinfo
version = 1.2

[release]
name = Red Hat Enterprise Linux
short = RHEL
type = ga
version = 9.8

[stage2]
mainimage = images/install.img

[tree]
arch = x86_64
build_timestamp = 1746900000
platforms = x86_64,xen
variants = AppStream,BaseOS

[variant-AppStream]
id = AppStream
name = AppStream
packages = AppStream/Packages
repository = AppStream
type = variant
uid = AppStream

[variant-BaseOS]
id = BaseOS
name = BaseOS
packages = BaseOS/Packages
repository = BaseOS
type = variant
uid = BaseOS
"""


def record(kind, data, compressed=None):
    opened = ('<open-checksum type="sha256">%s</open-checksum><open-size>%d</open-size>' % (hashlib.sha256(data).hexdigest(), len(data))
              if compressed is not None else "")
    return sources.ET.fromstring('<data xmlns="http://linux.duke.edu/metadata/repo" type="%s">%s</data>' % (kind, opened))


def member(tmp_path, kind, data, compress=False):
    acquisition = object.__new__(sources.Acquisition)
    acquisition.expanded, acquisition.deadline = 0, sources.time.monotonic() + 10
    stored = gzip.compress(data) if compress else data
    path = tmp_path / ("member.xml.gz" if compress else "member.xml")
    path.write_bytes(stored)
    acquisition.metadata_member(str(path), record(kind, data, stored if compress else None), hashlib.sha256(stored).hexdigest(), len(stored))
    return acquisition.expanded


@pytest.mark.parametrize("kind,data,compress", [("group", DVD_COMPS, False), ("group_gz", DVD_COMPS, True), ("group", ONE_LINE_COMPS, False),
                                                ("group_gz", ONE_LINE_COMPS, True)],
                         ids=["dvd-group", "dvd-group-gz", "one-line-group", "one-line-group-gz"])
def test_the_standard_external_comps_doctype_is_admitted_and_accounted_in_full(tmp_path, kind, data, compress):
    assert member(tmp_path, kind, data, compress) == len(data)


@pytest.mark.parametrize("kind,data", [
    ("group", b"<?xml version=\"1.0\"?>\n<!DOCTYPE comps PUBLIC '-//Red Hat, Inc.//DTD Comps info//EN' 'comps.dtd' [<!ENTITY x SYSTEM \"file:///etc/shadow\">]>"
              b"<comps>&x;</comps>"),
    ("group", b"<?xml version=\"1.0\"?>\n<!DOCTYPE comps [<!ELEMENT comps ANY>]><comps/>"),
    ("group", DVD_COMPS_HEAD + b"  <!ENTITY x 'y'>\n" + COMPS_BODY),
    ("group", DVD_COMPS.replace(b"DOCTYPE comps", b"DOCTYPE metadata")),
    ("group", DVD_COMPS.replace(b"'comps.dtd'", b"'http://foreign.example.test/comps.dtd'")),
    ("group", DVD_COMPS.replace(b"PUBLIC '-//Red Hat, Inc.//DTD Comps info//EN'\n ", b"SYSTEM")),
    ("primary", DVD_COMPS),
    ("primary", DVD_COMPS.replace(b"DOCTYPE comps", b"DOCTYPE metadata")),
    ("modules", DVD_COMPS),
    ("group", DVD_COMPS_HEAD.replace(b"<comps>\n", b"") + DVD_COMPS_HEAD[DVD_COMPS_HEAD.index(b"<!DOCTYPE"):] + COMPS_BODY),
    ("group", b"<?xml version=\"1.0\"?>\n<comps>\n" + DVD_COMPS_HEAD[DVD_COMPS_HEAD.index(b"<!DOCTYPE"):] + COMPS_BODY),
    ("group", b"<!-- c -->" + DVD_COMPS),
    ("group", DVD_COMPS.replace(b'encoding="UTF-8"', b'encoding="UTF-7"')),
    ("group", DVD_COMPS.replace(b'encoding="UTF-8"', b'encoding="UTF-16"')),
    ("group", DVD_COMPS.replace(b'<?xml version="1.0" encoding="UTF-8"?>', b'<?xml-stylesheet href="x"?>')),
    ("group", b"\xef\xbb\xbf" + DVD_COMPS),
], ids=["internal-entity", "internal-subset", "later-entity", "other-name", "foreign-dtd", "system-only", "primary", "primary-other-name", "modules",
        "second-doctype", "after-root", "before-declaration", "utf-7", "utf-16", "other-instruction", "byte-order-mark"])
@pytest.mark.parametrize("compress", [False, True], ids=["plain", "gzip"])
def test_every_other_document_type_or_entity_declaration_is_refused(tmp_path, kind, data, compress):
    kind = kind + "_gz" if compress and kind == "group" else kind
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        member(tmp_path, kind, data, compress)


@pytest.mark.parametrize("split", [b"<!ENT|ITY x 'y'>", b"<!doc|type comps>"], ids=["entity", "lower-case-doctype"])
def test_a_declaration_split_across_the_read_boundary_is_still_refused(tmp_path, split):
    head, rest = split.split(b"|")
    data = DVD_COMPS_HEAD + b" " * ((1 << 20) - len(DVD_COMPS_HEAD) - len(head)) + head + rest + COMPS_BODY
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        member(tmp_path, "group", data)
    assert member(tmp_path, "group", data.replace(head + rest, b" " * len(head + rest))) == len(data)


UNSAFE_XML = [
    b"<metadata>\x00</metadata>",
    '<?xml version="1.0"?><metadata/>'.encode("utf-16"),
    '<?xml version="1.0"?><metadata/>'.encode("utf-32"),
    b"+/v8-<metadata/>",
    b'<?xml version="1.0" encoding="UTF-7"?><metadata/>',
    b'<?xml version="1.0" encoding="ISO-8859-1"?><metadata/>',
]


@pytest.mark.parametrize("data", UNSAFE_XML, ids=["nul", "utf16", "utf32", "utf7-bom", "utf7-declaration", "latin1"])
@pytest.mark.parametrize("kind", sorted(sources.METADATA_TYPES))
@pytest.mark.parametrize("compress", [False, True])
def test_every_metadata_member_refuses_hidden_declaration_encodings(tmp_path, data, kind, compress):
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        member(tmp_path, kind, data, compress)


@pytest.mark.parametrize("data", UNSAFE_XML, ids=["nul", "utf16", "utf32", "utf7-bom", "utf7-declaration", "latin1"])
def test_repomd_encoding_is_refused_before_parsing_or_publication(tmp_path, monkeypatch, data):
    acquisition = object.__new__(sources.Acquisition)
    monkeypatch.setattr(acquisition, "bytes", lambda *_args: data)
    parsed = []
    monkeypatch.setattr(sources.ET, "fromstring", parsed.append)
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        acquisition.repository(str(tmp_path), "repo", sources.IBM_BASE, "signer")
    assert not parsed
    assert not list(tmp_path.iterdir())


@pytest.mark.parametrize("encoding", [b"UTF-8", b"US-ASCII", b"utf-8", b"UTF-7", b"UTF-16", b"US-ASCII-foreign"])
@pytest.mark.parametrize("split", [b"enc|oding=", b"encoding | =", b"encoding= |", b"encoding=\"UT|F-7\""])
def test_encoding_declarations_are_checked_across_stream_boundaries(tmp_path, encoding, split):
    before, after = split.split(b"|")
    if b"UT" in before:
        before, after = b'encoding="' + encoding[:2], encoding[2:] + b'"'
    else:
        after += b'"' + encoding + b'"'
    head = b'<?xml version="1.0" '
    data = head + b" " * ((1 << 20) - len(head) - len(before)) + before + after + b"?><metadata/>"
    if encoding.upper() in (b"UTF-8", b"US-ASCII"):
        assert member(tmp_path, "primary", data) == len(data)
    else:
        with pytest.raises(ceph_native.Refusal, match="^packages$"):
            member(tmp_path, "primary", data)


@pytest.mark.parametrize("data", [PRIMARY, b"\xef\xbb\xbf" + PRIMARY, PRIMARY.replace(b"UTF-8", b"US-ASCII")])
def test_safe_xml_encodings_keep_original_size_and_checksum(tmp_path, data):
    assert member(tmp_path, "primary", data, True) == len(data)


@pytest.mark.parametrize("data", UNSAFE_XML + [PRIMARY, b"\xef\xbb\xbf" + PRIMARY])
@pytest.mark.parametrize("width", [1, 2, 3, 7])
def test_short_reads_cannot_hide_the_bom_or_encoding(tmp_path, monkeypatch, data, width):
    import io

    class ShortReader(io.BytesIO):
        def read(self, size=-1):
            return super().read(min(size, width))

    monkeypatch.setattr(sources, "open", lambda *_args: ShortReader(data), raising=False)
    if data in UNSAFE_XML:
        with pytest.raises(ceph_native.Refusal, match="^packages$"):
            member(tmp_path, "primary", data)
    else:
        assert member(tmp_path, "primary", data) == len(data)


def test_a_nul_after_the_declaration_is_refused_on_a_later_read(tmp_path):
    data = PRIMARY + b" " * (1 << 20) + b"\x00"
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        member(tmp_path, "primary", data)


@pytest.mark.parametrize("bom", [b"\xfe\xff", b"\xff\xfe", b"+/v8", b"+/v9", b"+/v+", b"+/v/",
                                 b"\xf7\x64\x4c", b"\xdd\x73\x66\x73", b"\x0e\xfe\xff", b"\xfb\xee\x28", b"\x84\x31\x95\x33"])
def test_each_foreign_byte_order_mark_is_refused(tmp_path, bom):
    with pytest.raises(ceph_native.Refusal, match="^packages$"):
        member(tmp_path, "primary", bom + b"<metadata/>")
