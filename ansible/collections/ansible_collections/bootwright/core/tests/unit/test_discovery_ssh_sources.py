"""The source SSH runtime is bounded read-only data from the held RHEL image."""

from pathlib import Path
from unittest.mock import Mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_ssh_sources as sources

EXTENT = b"File data lba: 0 , 7 , 5 , 10240 , '/images/install.img'\n"
LISTING = b"-rw-r--r-- root/root 4294967296 2026-01-01 12:00 squashfs-root/LiveOS/rootfs.img\n"
ELF = b"Class: ELF64\nData: 2's complement, little endian\nMachine: Advanced Micro Devices X86-64\n[Requesting program interpreter: /lib64/ld-linux-x86-64.so.2]\n0x01 (NEEDED) Shared library: [libcrypto.so.3]\n"


@pytest.fixture(autouse=True)
def fixed_tools(monkeypatch):
    monkeypatch.setattr(sources, "tool", lambda name: "/fixed/" + name)


def test_source_extent_uses_the_held_iso_without_copying_or_mounting():
    native = Mock(side_effect=[EXTENT, LISTING])
    assert sources.inspect("/proc/123/fd/4", 100000, native) == {"offset": 14336, "size": 10240, "rootSize": 4294967296}
    calls = native.call_args_list
    assert calls[0].args[0][2] == "/proc/123/fd/4"
    assert calls[1].args[0] == ["/fixed/unsquashfs", "-offset", "14336", "-ll", "/proc/123/fd/4", "LiveOS/rootfs.img"]


@pytest.mark.parametrize("extent", [b"", EXTENT * 2, EXTENT.replace(b"0 ,", b"1 ,"),
                                    EXTENT.replace(b"5 ,", b"4 ,"), EXTENT.replace(b"7 ,", b"700 ,")])
def test_unqualified_or_out_of_source_extents_refuse(extent):
    with pytest.raises(ValueError, match="source (layout|extent)"):
        sources.inspect("/held", 100000, Mock(return_value=extent))


@pytest.mark.parametrize("listing", [b"", LISTING * 2, LISTING.replace(b"4294967296", b"0"),
                                     LISTING.replace(b"4294967296", str(sources.MAX_ROOTFS + 1).encode()),
                                     LISTING.replace(b"-rw", b"lrw")])
def test_rootfs_size_and_regular_type_are_proved_before_extraction(listing):
    with pytest.raises(ValueError, match="source filesystem"):
        sources.inspect("/held", 100000, Mock(side_effect=[EXTENT, listing]))


def stat_file(kind="regular", size=4):
    return ("Inode: 1 Type: " + kind + " Mode:  0644\nUser: 0 Group: 0 Size: " + str(size) + "\n").encode()


def test_debugfs_reads_only_fixed_files_and_follows_a_bounded_virtual_link():
    native = Mock(side_effect=[stat_file("symlink", 18) + b'Fast link dest: "/usr/share/policy"\n', stat_file(), b"data"])
    assert sources.read_file(Path("/private/rootfs.img"), "/etc/policy", native) == b"data"
    commands = [call.args[0] for call in native.call_args_list]
    assert all(argv[0:2] == ["/fixed/debugfs", "-R"] and "-w" not in argv for argv in commands)
    assert commands[-1] == ["/fixed/debugfs", "-R", "cat /usr/share/policy", "/private/rootfs.img"]


def test_missing_optional_helper_is_not_an_empty_executable():
    assert sources.read_file(Path("/image"), "/usr/libexec/openssh/sshd-auth", Mock(return_value=b""), optional=True) is None
    with pytest.raises(ValueError):
        sources.read_file(Path("/image"), "/usr/sbin/sshd", Mock(return_value=b""))


@pytest.mark.parametrize("metadata,data", [(stat_file(size=5), b"data"), (stat_file("directory"), b"data"),
                                         (stat_file(size=sources.MAX_COMPONENT + 1), b"data")])
def test_virtual_files_refuse_type_size_or_truncation(metadata, data):
    with pytest.raises(ValueError, match="source file"):
        sources.read_file(Path("/image"), "/usr/sbin/sshd", Mock(side_effect=[metadata, data]))


def test_virtual_symlink_cycles_and_injected_paths_refuse():
    link = stat_file("symlink", 4) + b'Fast link dest: "/etc/policy"\n'
    native = Mock(return_value=link)
    with pytest.raises(ValueError, match="source link"):
        sources.read_file(Path("/image"), "/etc/policy", native)
    assert native.call_count == 16
    with pytest.raises(ValueError, match="source path"):
        sources.read_file(Path("/image"), "/etc/policy; cat /private", native)


@pytest.mark.parametrize("data", [ELF.replace(b"ELF64", b"ELF32"), ELF.replace(b"little endian", b"big endian"),
                                  ELF.replace(b"X86-64", b"AArch64"), ELF + b"(RUNPATH) /tmp\n",
                                  ELF.replace(b"libcrypto.so.3", b"../bad.so"), ELF.replace(b"/lib64/", b"/unknown/")])
def test_elf_qualification_refuses_unsupported_loader_architecture_or_search_paths(data):
    with pytest.raises(ValueError, match="source ELF"):
        sources.libraries(Path("/private/sshd"), Mock(return_value=data))


def test_readelf_inspects_data_without_executing_the_server():
    native = Mock(return_value=ELF)
    assert sources.libraries(Path("/private/sshd"), native) == {"libcrypto.so.3"}
    native.assert_called_once_with(["/fixed/readelf", "-h", "-l", "-d", "/private/sshd"], limit=65536)


def test_extraction_reads_only_rootfs_and_reaps_scratch_on_refusal(tmp_path, monkeypatch):
    monkeypatch.setattr(sources.records, "directory", lambda path, create: path.mkdir(mode=0o700))
    monkeypatch.setattr(sources, "secure_rootfs", lambda destination, size: destination / "rootfs.img")
    monkeypatch.setattr(sources, "read_file", Mock(side_effect=ValueError("discovery SSH source file")))
    native = Mock()
    with pytest.raises(ValueError):
        sources.extract("/held", {"offset": 4096, "rootSize": 1 << 30}, tmp_path, native)
    assert not (tmp_path / "ssh-source").exists()
    argv = native.call_args.args[0]
    assert argv[0] == "/fixed/unsquashfs" and "-no-xattrs" in argv
    assert argv[-2:] == ["/held", "LiveOS/rootfs.img"]


def test_split_session_cannot_reference_a_missing_auth_helper(tmp_path, monkeypatch):
    monkeypatch.setattr(sources.records, "directory", lambda path, create: path.mkdir(mode=0o700))
    monkeypatch.setattr(sources.records, "write", lambda path, data: path.write_bytes(data))
    monkeypatch.setattr(sources, "secure_rootfs", lambda destination, size: destination / "rootfs.img")
    monkeypatch.setattr(sources, "libraries", lambda *args: {"libc.so.6"})
    session = b"\x7fELF" + sources.PATHS["sshd-auth"].encode() + b"\0"
    monkeypatch.setattr(sources, "read_file", Mock(side_effect=[b"\x7fELFmain", session, None]))
    with pytest.raises(ValueError, match="source helper"):
        sources.extract("/held", {"offset": 4096, "rootSize": 1 << 30}, tmp_path, Mock())
    assert not (tmp_path / "ssh-source").exists()


@pytest.mark.parametrize("boundary", ("regular", "owner", "hardlink", "size", "symlink", "directory"))
def test_extracted_rootfs_is_proved_on_its_open_handle(tmp_path, monkeypatch, boundary):
    destination = tmp_path / "ssh-source"
    destination.mkdir(mode=0o755)
    (destination / "LiveOS").mkdir(mode=0o755)
    image = destination / "LiveOS/rootfs.img"
    foreign = tmp_path / "foreign"
    foreign.write_bytes(b"external fixture")
    if boundary == "symlink":
        image.symlink_to(foreign)
    elif boundary == "directory":
        image.mkdir()
    else:
        image.write_bytes(b"data")
        if boundary == "hardlink":
            sources.os.link(image, tmp_path / "other-link")
    original = sources.os.fstat

    def observed(fd):
        info = original(fd)
        fields = list(info)
        fields[4] = 1 if boundary == "owner" else 0
        return sources.os.stat_result(fields)

    monkeypatch.setattr(sources.os, "fstat", observed)
    if boundary == "regular":
        assert sources.secure_rootfs(destination, 4) == image
        assert image.stat().st_mode & 0o777 == 0o600
    else:
        with pytest.raises((ValueError, OSError)):
            sources.secure_rootfs(destination, 5 if boundary == "size" else 4)
    assert foreign.read_bytes() == b"external fixture"
