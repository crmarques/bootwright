"""The discovery initramfs shell is syntax-checked and run against a mocked root, as the installer shell is in Go."""

from __future__ import annotations

import re
import subprocess

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_boot as boot

LOG = "log"


def sh(arguments, text, cwd):
    """Run /bin/sh on the text with no controller environment."""
    # pylint: disable-next=ansible-bad-function
    return subprocess.run(["/bin/sh", *arguments], input=text, capture_output=True, text=True, cwd=cwd, timeout=60, check=False)


@pytest.mark.parametrize("name", ["INIT", "DRIVERS"])
def test_each_script_passes_a_shell_syntax_check_and_starts_with_its_interpreter_line(tmp_path, name):
    text = getattr(boot, name)
    assert text.startswith("#!/bin/sh\nset -eu\n")
    answer = sh(["-n"], text, tmp_path)
    assert (answer.returncode, answer.stderr) == (0, "")


def test_the_overlay_carries_each_script_byte_for_byte():
    data = boot.overlay({"disk_discovery_worker.py": b"worker"})
    assert boot.INIT.encode("ascii") in data and boot.DRIVERS.encode("ascii") in data


def rooted(text, root):
    """The script with each absolute path it uses moved under the temporary root."""
    return re.sub(r"/proc/sys|/sys/|/dev/console|/run/discovery|/bootwright-discovery", lambda found: str(root) + found.group(0), text)


INIT_MOCKS = """
mount() { echo "mount $*" >>log; }
sleep() { echo "sleep $*" >>log; exit 97; }
python3() { echo "python3 $*" >>log; }
timeout() { echo "timeout $*" >>log; return "${drivers:-0}"; }
"""


def run_init(root, drivers=0):
    (root / "proc/sys/kernel").mkdir(parents=True)
    (root / "dev").mkdir()
    (root / "run").mkdir()
    (root / "bootwright-discovery").mkdir()
    (root / "bootwright-discovery/payload").write_text("payload")
    (root / LOG).write_text("")
    text = "drivers=" + str(drivers) + "\n" + INIT_MOCKS + rooted(boot.INIT, root)
    answer = sh(["-c", text], None, root)
    return answer, (root / LOG).read_text().splitlines()


def test_init_mounts_four_filesystems_disarms_modprobe_runs_the_drivers_copies_the_payload_and_starts_the_worker(tmp_path):
    answer, log = run_init(tmp_path)
    assert answer.returncode == 97, answer.stderr
    assert [line.split()[2] for line in log if line.startswith("mount ")] == ["proc", "sysfs", "devtmpfs", "tmpfs"]
    assert (tmp_path / "proc/sys/kernel/modprobe").read_text() == "/bin/false\n"
    assert (tmp_path / "run/discovery/payload").read_text() == "payload"
    order = [line.split()[0] for line in log]
    assert order == ["mount"] * 4 + ["timeout", "python3", "sleep", "sleep"]
    assert "timeout -k 2 180 /bin/sh " + str(tmp_path) + "/bootwright-discovery/drivers.sh" in log
    assert "python3 -I " + str(tmp_path) + "/run/discovery/disk_discovery_worker.py" in log
    assert "Bootwright discovery refused" in (tmp_path / "dev/console").read_text()


def test_init_never_starts_the_worker_when_the_drivers_fail_and_goes_inert(tmp_path):
    answer, log = run_init(tmp_path, drivers=1)
    assert answer.returncode == 97, answer.stderr
    assert not any(line.startswith("python3") for line in log)
    assert not (tmp_path / "run/discovery/payload").exists()
    assert log[-1] == "sleep 60"
    assert "Bootwright discovery refused" in (tmp_path / "dev/console").read_text()


DRIVER_MOCKS = """
sleep() { echo "sleep $*" >>log; }
timeout() { shift 3; "$@"; }
modprobe() { echo "modprobe $*" >>log; }
"""


def device(root, bus, name, alias=None, klass=None, driver=False):
    path = root / "sys/bus" / bus / "devices" / name
    path.mkdir(parents=True)
    if alias is not None:
        (path / "modalias").write_text(alias + "\n")
    if klass is not None:
        (path / "class").write_text(klass + "\n")
    if driver:
        (path / "driver").symlink_to(root)
    return path


def run_drivers(root):
    (root / "dev").mkdir(exist_ok=True)
    (root / LOG).write_text("")
    answer = sh(["-c", DRIVER_MOCKS + rooted(boot.DRIVERS, root)], None, root)
    return answer, (root / LOG).read_text().splitlines()


def test_the_driver_script_loads_each_unbound_alias_on_three_passes_and_skips_bound_devices(tmp_path):
    device(tmp_path, "pci", "0000:00:01.0", "pci:v00008086d00001234", "0x020000")
    device(tmp_path, "virtio", "virtio0", "virtio:d00000002v00001AF4")
    device(tmp_path, "pci", "0000:00:02.0", "pci:v00001111d00002222", "0x030000", driver=True)
    answer, log = run_drivers(tmp_path)
    assert answer.returncode == 0, answer.stderr
    loads = [line for line in log if line.startswith("modprobe")]
    assert sorted(set(loads)) == ["modprobe --quiet --config /dev/null --ignore-install pci:v00008086d00001234",
                                  "modprobe --quiet --config /dev/null --ignore-install virtio:d00000002v00001AF4"]
    assert len(loads) == 6 and log.count("sleep 2") == 3


@pytest.mark.parametrize("alias", ["", "pci:v1; reboot", "pci v1", "x" * 257, "pci:$(id)", "a`b`"])
def test_the_driver_script_refuses_an_unsafe_or_overlong_alias_before_loading_it(tmp_path, alias):
    device(tmp_path, "pci", "0000:00:01.0", alias, "0x020000")
    answer, log = run_drivers(tmp_path)
    assert answer.returncode == 1
    assert not any(line.startswith("modprobe") for line in log)


def test_the_driver_script_fails_when_a_storage_controller_has_no_driver_and_not_for_other_classes(tmp_path):
    device(tmp_path, "pci", "0000:00:01.0", "pci:v1", "0x020000")
    device(tmp_path, "pci", "0000:00:1f.2", "pci:v2", "0x010601")
    answer, log = run_drivers(tmp_path)
    assert answer.returncode == 1
    assert log[-1] == "sleep 2"
    (tmp_path / "sys/bus/pci/devices/0000:00:1f.2/driver").symlink_to(tmp_path)
    answer, log = run_drivers(tmp_path)
    assert answer.returncode == 0, answer.stderr


def test_the_driver_script_refuses_more_than_four_thousand_ninety_six_devices(tmp_path):
    for index in range(4097):
        device(tmp_path, "virtio", "virtio%d" % index, "virtio:d%d" % index)
    answer, unused = run_drivers(tmp_path)
    assert answer.returncode == 1
