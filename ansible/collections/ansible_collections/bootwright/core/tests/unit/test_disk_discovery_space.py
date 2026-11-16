"""Discovery media uses one private image and transfers ownership at publication."""

import errno
import os
from concurrent.futures import ThreadPoolExecutor
import threading
import time
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_media as media
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_controller as controller


@pytest.mark.parametrize("failure", (False, True))
def test_lorax_adds_only_the_patched_initrds_to_the_boot_tree(tmp_path, monkeypatch, failure):
    paths = sorted(media.SUPPORTED_INITRDS)
    boot_tree = str(tmp_path / "media-work" / "boot-tree.iso")
    monkeypatch.setattr(media, "initrd_paths", lambda held: paths if held == boot_tree else pytest.fail("read the installer ISO"))
    monkeypatch.setattr(media, "outside_boot_tree", lambda held: ["/images/install.img", "/BaseOS"])
    monkeypatch.setattr(media, "extract", lambda held, path, target: target.write_bytes(b"verified initrd"))
    qualified = Mock()
    monkeypatch.setattr(media, "qualified_initrd", qualified)
    monkeypatch.setattr(media.private_records, "directory", lambda path, create: path.mkdir(mode=0o700) if create else None)
    commands = []

    def native(argv, **kwargs):
        commands.append(argv)
        if argv[0] == "/usr/bin/xorriso":
            assert argv == ["/usr/bin/xorriso", "-indev", "/held/source.iso", "-outdev", boot_tree, "-boot_image", "any", "replay",
                            "-rm_r", "/images/install.img", "/BaseOS", "--"]
            Path(boot_tree).write_bytes(b"boot tree only")
            return b""
        assert argv[0] == "/usr/bin/mkksiso"
        assert argv[-2] == boot_tree
        added = [Path(argv[index + 1]) for index, arg in enumerate(argv) if arg == "--add"]
        assert {path.name for path in added} == {"images", "isolinux"}
        assert {str(path.relative_to(tmp_path / "media-work")) for top in added for path in top.rglob("*") if path.is_file()} == {
            path.lstrip("/") for path in paths
        }
        for path in paths:
            patched = tmp_path / "media-work" / path.lstrip("/")
            assert patched.read_bytes().startswith(b"verified initrd")
            assert b"070701" in patched.read_bytes()
        assert "--skip-mkefiboot" not in argv
        assert kwargs["temporary"] == tmp_path / "media-work"
        Path(argv[-1]).write_bytes(b"private finished ISO")
        if failure:
            raise ValueError("synthetic tool failure")

    monkeypatch.setattr(media, "native", native)
    if failure:
        with pytest.raises(ValueError, match="synthetic tool failure"):
            media.remaster("/held/source.iso", tmp_path, {"request.json": b"private fixture"})
        assert not (tmp_path / "discovery.iso").exists()
    else:
        assert media.remaster("/held/source.iso", tmp_path, {"request.json": b"private fixture"}).read_bytes() == b"private finished ISO"
    assert qualified.call_count == 2
    assert len(commands) == 2
    assert not (tmp_path / "patched.iso").exists()
    assert not (tmp_path / "media-work").exists()


def test_same_filesystem_publication_transfers_without_copying(tmp_path, monkeypatch):
    source, target = tmp_path / "source.iso", tmp_path / "published.iso"
    source.write_bytes(b"private synthetic ISO")
    before = source.stat()
    monkeypatch.setattr(controller.os, "write", Mock(side_effect=AssertionError("copied image")))
    controller.publish_image(source, target, before.st_size)
    assert not source.exists()
    assert target.stat().st_ino == before.st_ino
    assert target.read_bytes() == b"private synthetic ISO"
    assert target.stat().st_mode & 0o777 == 0o640


def test_publication_never_changes_an_inode_another_consumer_holds(tmp_path):
    source, target, consumer = (tmp_path / name for name in ("source.iso", "published.iso", "consumer.iso"))
    source.write_bytes(b"immutable consumer")
    source.chmod(0o400)
    os.link(source, consumer)
    with pytest.raises(ValueError, match="publication file"):
        controller.publish_image(source, target, source.stat().st_size)
    assert consumer.read_bytes() == b"immutable consumer"
    assert consumer.stat().st_mode & 0o777 == 0o400
    assert not target.exists()


def test_cross_filesystem_publication_copies_then_releases_source(tmp_path, monkeypatch):
    source, target = tmp_path / "source.iso", tmp_path / "published.iso"
    source.write_bytes(b"private synthetic ISO")
    monkeypatch.setattr(controller.os, "rename", Mock(side_effect=OSError(errno.EXDEV, "different filesystem")))
    controller.publish_image(source, target, source.stat().st_size)
    assert target.read_bytes() == b"private synthetic ISO"
    assert not source.exists()
    assert not list(tmp_path.glob(".stage-*"))


@pytest.mark.parametrize("change", ("grow", "truncate"))
def test_cross_filesystem_copy_enforces_the_validated_size(tmp_path, monkeypatch, change):
    source, target = tmp_path / "source.iso", tmp_path / "published.iso"
    source.write_bytes(b"private synthetic ISO")
    target.write_bytes(b"previous publication")

    def different_filesystem(*args):
        if change == "grow":
            with source.open("ab") as stream:
                stream.write(b"unexpected bytes")
        else:
            source.write_bytes(b"short")
        raise OSError(errno.EXDEV, "synthetic filesystem boundary")

    monkeypatch.setattr(controller.os, "rename", different_filesystem)
    with pytest.raises(ValueError, match="publication size"):
        controller.publish_image(source, target, source.stat().st_size)
    assert target.read_bytes() == b"previous publication"
    assert not list(tmp_path.glob(".stage-*"))


@pytest.mark.parametrize("cross_filesystem", (False, True))
def test_interrupted_atomic_replacement_preserves_the_previous_publication(tmp_path, monkeypatch, cross_filesystem):
    source, target = tmp_path / "source.iso", tmp_path / "published.iso"
    source.write_bytes(b"private synthetic ISO")
    target.write_bytes(b"previous publication")
    if cross_filesystem:
        monkeypatch.setattr(controller.os, "rename", Mock(side_effect=OSError(errno.EXDEV, "synthetic filesystem boundary")))
    monkeypatch.setattr(controller.os, "replace", Mock(side_effect=InterruptedError("synthetic cancellation")))
    with pytest.raises(InterruptedError):
        controller.publish_image(source, target, source.stat().st_size)
    assert target.read_bytes() == b"previous publication"
    assert not list(tmp_path.glob(".stage-*"))


@pytest.fixture
def capacity_identity(monkeypatch):
    """Only root identity/ancestry is simulated; real flock and files are used."""
    original = os.fstat

    def root_owned(fd):
        fields = list(original(fd))
        fields[4] = 0
        return os.stat_result(fields)

    def directory(path, create, mode=0o700):
        if create:
            path.mkdir(mode=mode, exist_ok=True)

    monkeypatch.setattr(media.os, "fstat", root_owned)
    monkeypatch.setattr(media.private_records, "directory", directory)


def test_capacity_combines_paths_on_one_filesystem_before_building(tmp_path, monkeypatch, capacity_identity):
    work, served = tmp_path / "work", tmp_path / "served"
    work.mkdir()
    served.mkdir()
    monkeypatch.setattr(media.os, "statvfs", lambda path: SimpleNamespace(f_bavail=1, f_frsize=1, f_bsize=1))
    with pytest.raises(OSError) as caught:
        with media.construction(tmp_path, work, served, 64 << 20):
            pytest.fail("construction began without its peak capacity")
    assert caught.value.errno == errno.ENOSPC
    assert list(work.iterdir()) == []


def test_construction_overlaps_when_each_peak_fits(tmp_path, monkeypatch, capacity_identity):
    monkeypatch.setattr(media.os, "statvfs", lambda path: SimpleNamespace(f_bavail=1 << 40, f_frsize=1, f_bsize=1))
    together = threading.Barrier(3)

    def construct(number):
        with media.construction(tmp_path, tmp_path, tmp_path, 64 << 20):
            together.wait(timeout=5)
            return number

    with ThreadPoolExecutor(max_workers=3) as workers:
        assert sorted(workers.map(construct, (1, 2, 3))) == [1, 2, 3]


def test_concurrent_capacity_checks_include_previously_published_images(tmp_path, monkeypatch, capacity_identity):
    maximum = media.publication_bound(64 << 20)
    scratch = (64 << 20) + len(media.SUPPORTED_INITRDS) * (media.MAX_INITRD + media.boot.MAX_OVERLAY) + media.MAX_BOOT_GROWTH
    available = 2 * maximum + scratch
    active, peak = 0, 0
    barrier = threading.Barrier(3)
    observed = []

    def capacity(path):
        observed.append(available)
        return SimpleNamespace(f_bavail=available, f_frsize=1, f_bsize=1)

    monkeypatch.setattr(media.os, "statvfs", capacity)

    def construct(number):
        nonlocal available, active, peak
        barrier.wait(timeout=5)
        try:
            with media.construction(tmp_path, tmp_path, tmp_path, 64 << 20):
                active += 1
                peak = max(peak, active)
                time.sleep(0.01)
                available -= maximum
                active -= 1
            return True
        except OSError as error:
            assert error.errno == errno.ENOSPC
            return False

    with ThreadPoolExecutor(max_workers=3) as workers:
        results = list(workers.map(construct, (1, 2, 3)))
    assert sorted(results) == [False, True, True]
    assert peak == 1
    assert [size for index, size in enumerate(observed) if index == 0 or size != observed[index - 1]] == [
        2 * maximum + scratch, maximum + scratch, scratch,
    ]


def test_capacity_checks_each_filesystem_and_releases_the_lock_on_cancellation(tmp_path, monkeypatch, capacity_identity):
    work, served = tmp_path / "work", tmp_path / "served"
    work.mkdir()
    served.mkdir()
    original_stat = Path.stat

    def filesystem(path, *args, **kwargs):
        return SimpleNamespace(st_dev=1 if path == work else 2) if path in (work, served) else original_stat(path, *args, **kwargs)

    monkeypatch.setattr(Path, "stat", filesystem)
    monkeypatch.setattr(media.os, "statvfs", lambda path: SimpleNamespace(f_bavail=1 if path == served else 1 << 40, f_frsize=1, f_bsize=1))
    with pytest.raises(OSError) as caught:
        with media.construction(tmp_path, work, served, 64 << 20):
            pytest.fail("destination capacity was ignored")
    assert caught.value.errno == errno.ENOSPC
    monkeypatch.setattr(media.os, "statvfs", lambda path: SimpleNamespace(f_bavail=1 << 40, f_frsize=1, f_bsize=1))
    with pytest.raises(KeyboardInterrupt):
        with media.construction(tmp_path, work, served, 64 << 20):
            raise KeyboardInterrupt
    with media.construction(tmp_path, work, served, 64 << 20):
        pass


@pytest.mark.parametrize("unsafe", (False, True))
def test_interrupted_publication_reaps_only_its_owned_regular_stages(tmp_path, monkeypatch, unsafe):
    stage = tmp_path / (".stage-" + "a" * 32)
    target = tmp_path / "discovery.iso"
    target.write_bytes(b"previous publication")
    if unsafe:
        stage.symlink_to(target)
    else:
        stage.write_bytes(b"partial private ISO")
    (tmp_path / "unrelated").write_bytes(b"other content")
    original_lstat = Path.lstat

    def root_owned(path, *args, **kwargs):
        fields = list(original_lstat(path, *args, **kwargs))
        fields[4] = 0
        return os.stat_result(fields)

    monkeypatch.setattr(Path, "lstat", root_owned)
    if unsafe:
        with pytest.raises(ValueError, match="publication stage"):
            controller.reap_publication_stages(tmp_path)
        assert stage.is_symlink()
    else:
        controller.reap_publication_stages(tmp_path)
        assert not stage.exists()
    assert target.read_bytes() == b"previous publication"
    assert (tmp_path / "unrelated").read_bytes() == b"other content"


def test_reaping_interrupted_media_preserves_receipts_and_rejects_a_link(tmp_path, monkeypatch):
    monkeypatch.setattr(media.private_records, "directory", lambda *args: None)
    scratch = tmp_path / "media-work"
    scratch.mkdir()
    (scratch / "partial-key").write_bytes(b"private fixture")
    (tmp_path / "report.json").write_bytes(b"immutable report")
    for name in ("patched.iso", "initrd-0", "initrd-1"):
        (tmp_path / name).write_bytes(b"obsolete intermediate")
    media.reap_intermediates(tmp_path)
    assert sorted(path.name for path in tmp_path.iterdir()) == ["report.json"]
    scratch.symlink_to(tmp_path.parent, target_is_directory=True)
    with pytest.raises((OSError, ValueError)):
        media.reap_intermediates(tmp_path)
    assert (tmp_path / "report.json").read_bytes() == b"immutable report"
