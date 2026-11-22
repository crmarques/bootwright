"""The erase image boots on the installer runtime as Lorax ships it and raises the frozen install network itself."""

import inspect
import re
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_boot as boot
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_runtime as runtime
from ansible_collections.bootwright.core.plugins.module_utils import physical_erase_worker as worker
from ansible_collections.bootwright.core.plugins.module_utils import physical_machine_identity

# Binaries the RHEL 9 installer runtime keeps: Lorax's runtime-cleanup.tmpl
# strips coreutils' base64 and shadow-utils' usermod, among others.
INSTALLER_RUNTIME = {
    "/usr/bin/python3", "/usr/bin/findmnt", "/usr/bin/systemctl", "/usr/sbin/dmsetup", "/usr/sbin/sshd",
    "/usr/sbin/chpasswd", "/usr/bin/anaconda", "/usr/sbin/anaconda",
}
PRE_COMMANDS = {"umask", "fail_closed()", "trap", "set", "test", "mkdir", "cat", "systemctl", "fail_closed", "/usr/bin/python3"}
BOND_VLAN = ("network --bootproto=static --device=bond0 --bondslaves=enp1s0,enp2s0 --bondopts=mode=active-backup,miimon=100 "
             "--vlanid=120 --interfacename=bond0.120 --ip=198.51.100.20 --netmask=255.255.255.0 --hostname=metal-1.example.test "
             "--onboot=on --activate --gateway=198.51.100.1 --nameserver=192.0.2.1,192.0.2.2")


def pre_script(rendered):
    return rendered.split("%pre --erroronfail --interpreter=/bin/bash\n", 1)[1].split("\n%end\n", 1)[0]


def commands(script):
    """The first word of each %pre line outside a here-document."""
    words, closing = [], None
    for line in script.splitlines():
        if closing:
            closing = None if line == closing else closing
            continue
        words.append(line.split()[0])
        marker = re.search(r"<<'([A-Z_]+)'$", line)
        closing = marker.group(1) if marker else None
    return words


class InstallerRuntimeTests(unittest.TestCase):
    def test_the_pre_decodes_every_payload_file_without_a_command_from_path(self):
        rendered = boot.kickstart(BOND_VLAN, {"request": "frozen"}, "PRIVATE KEY", "ssh-ed25519 AAAA fleet")
        lines = pre_script(rendered).splitlines()
        start = next(index for index, line in enumerate(lines) if line.startswith(boot.DECODE))
        end = next(index for index, line in enumerate(lines) if line.startswith("cat > /run/systemd/system/"))
        with tempfile.TemporaryDirectory() as ram:
            script = "\n".join(lines[start:end]).replace(boot.RAM + "/", ram + "/").replace("/usr/bin/python3", sys.executable)
            # pylint: disable-next=ansible-bad-function
            answer = subprocess.run(["/bin/bash", "-euo", "pipefail", "-c", script], env={"PATH": "/nonexistent"},
                                    capture_output=True, timeout=60, check=False)
            self.assertEqual(answer.returncode, 0, answer.stderr)
            shipped = {path.name: path.read_bytes() for path in Path(ram).iterdir()}
        self.assertEqual(shipped["runtime.py"], inspect.getsource(runtime).encode())
        self.assertEqual(shipped["physical_erase_worker.py"], inspect.getsource(worker).encode())
        self.assertEqual(shipped["request.json"], b'{"request": "frozen"}')
        self.assertEqual(shipped["host-key"], b"PRIVATE KEY")
        self.assertEqual(shipped["authorized_keys"], b"restrict ssh-ed25519 AAAA fleet\n")

    def test_the_pre_runs_only_shell_builtins_and_commands_the_installer_runtime_keeps(self):
        script = pre_script(boot.kickstart(BOND_VLAN, {}, "PRIVATE", "ssh-ed25519 AAAA fleet"))
        self.assertLessEqual(set(commands(script)), PRE_COMMANDS)
        self.assertNotIn("base64 ", script)

    def test_the_ram_payload_names_only_binaries_the_installer_runtime_keeps(self):
        for module in (runtime, worker, physical_machine_identity, bounded_process):
            named = set(re.findall(r'"(/usr/s?bin/[A-Za-z0-9._-]+)"', inspect.getsource(module)))
            with self.subTest(module=module.__name__):
                self.assertLessEqual(named, INSTALLER_RUNTIME)


class NetworkArgumentTests(unittest.TestCase):
    def test_a_vlan_over_a_bond_is_raised_by_dracut_arguments(self):
        self.assertEqual(boot.network_arguments(BOND_VLAN), [
            "bond=bond0:enp1s0,enp2s0:mode=active-backup,miimon=100",
            "vlan=bond0.120:bond0",
            "ip=198.51.100.20::198.51.100.1:255.255.255.0:metal-1.example.test:bond0.120:none",
            "rd.neednet=1",
        ])

    def test_an_ethernet_bond_or_vlan_names_the_interface_that_carries_the_address(self):
        cases = (
            ("network --bootproto=static --device=eno1 --ip=192.0.2.10 --netmask=255.255.255.0 --hostname=metal-01 --onboot=on --activate",
             ["ip=192.0.2.10:::255.255.255.0:metal-01:eno1:none", "rd.neednet=1"]),
            ("network --bootproto=static --device=bond0 --bondslaves=eno1,eno2 --bondopts=mode=802.3ad --ip=192.0.2.10 "
             "--netmask=255.255.255.0 --hostname=metal-01 --gateway=192.0.2.1",
             ["bond=bond0:eno1,eno2:mode=802.3ad", "ip=192.0.2.10::192.0.2.1:255.255.255.0:metal-01:bond0:none", "rd.neednet=1"]),
            ("network --bootproto=static --device=eno1 --vlanid=20 --interfacename=eno1.20 --ip=192.0.2.10 "
             "--netmask=255.255.255.0 --hostname=metal-01",
             ["vlan=eno1.20:eno1", "ip=192.0.2.10:::255.255.255.0:metal-01:eno1.20:none", "rd.neednet=1"]),
        )
        for line, expected in cases:
            with self.subTest(line=line):
                self.assertEqual(boot.network_arguments(line), expected)

    def test_the_vlan_is_named_by_its_id_because_the_initramfs_reads_the_id_from_the_name(self):
        base = "network --bootproto=static --ip=192.0.2.10 --netmask=255.255.255.0 --hostname=metal-01 "
        for line, vlan, interface in (
            (base + "--device=bond0 --bondslaves=eno1 --bondopts=mode=active-backup --vlanid=120 --interfacename=mgmt",
             "vlan=bond0.120:bond0", "bond0.120"),
            (base + "--device=eno1 --vlanid=120 --interfacename=net2", "vlan=eno1.120:eno1", "eno1.120"),
            (base + "--device=enp200s0f0np0 --vlanid=4094 --interfacename=enp200s0f0np0.4", "vlan=vlan4094:enp200s0f0np0", "vlan4094"),
        ):
            with self.subTest(line=line):
                arguments = boot.network_arguments(line)
                self.assertIn(vlan, arguments)
                self.assertIn("ip=192.0.2.10:::255.255.255.0:metal-01:" + interface + ":none", arguments)

    def test_a_line_the_boot_arguments_cannot_carry_refuses_before_the_build(self):
        static = "network --bootproto=static --device=eno1 --ip=192.0.2.10 --netmask=255.255.255.0 --hostname=metal-01"
        for line in (
            "", "network --bootproto=dhcp --device=eno1", static + "\n" + static, static.replace("eno1", "eno:1"),
            static.replace("--ip=192.0.2.10", "--ip=2001:db8::1"), static.replace("255.255.255.0", "255.0.255.0"),
            static + " --mtu=9000", static + " --gateway", static + " --gateway=", static + " --activate=yes",
            static + " --hostname=again", static.replace("metal-01", "metal_01"), static.replace("--hostname=metal-01", ""),
            static.replace("eno1", "bond0") + " --bondslaves=eno1,eno1 --bondopts=mode=active-backup",
            static.replace("eno1", "bond0") + " --bondslaves=eno1 --bondopts=miimon=100,mode=active-backup",
            static.replace("eno1", "bond0") + " --bondslaves=eno1 --bondopts=mode=active-backup;arp_ip_target=192.0.2.1,192.0.2.2",
            static.replace("eno1", "bond0") + " --bondslaves=eno1 --bondopts=mode=active-backup,primary=a:b",
            static + " --vlanid=0 --interfacename=eno1.0", static + " --vlanid=4095 --interfacename=eno1.4095",
            static + " --vlanid=20", static + " --interfacename=eno1.20",
        ):
            with self.subTest(line=line), self.assertRaisesRegex(worker.Refusal, "^install-network$"):
                boot.network_arguments(line)
