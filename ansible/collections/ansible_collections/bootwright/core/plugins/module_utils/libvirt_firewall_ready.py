"""The provider's bounded firewalld readiness barrier, with exact ownership."""

from __future__ import annotations

import os
import re
import stat
import tempfile

SYSTEMD = "/etc/systemd/system"
SYSTEMCTL = "/usr/bin/systemctl"
ENVIRONMENT = {"PATH": "/usr/bin:/usr/sbin", "LC_ALL": "C.UTF-8"}
POLL = (
    "import subprocess,time,sys; "
    "sys.exit(0 if any(subprocess.call(['/usr/bin/firewall-cmd','--state'], "
    "stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)==0 or "
    "(time.sleep(2),False)[1] for _ in range(30)) else 1)"
)


def unit_name(request):
    name = request.get("firewallReadyUnit", "")
    expected = ""
    if any(network.get("managed") for network in request.get("networks", [])):
        identity = request["identity"]
        for value in (identity["context"], identity["object"]):
            if not isinstance(value, str) or not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]*[a-z0-9])?", value):
                raise ValueError("invalid readiness unit identity")
        expected = "bootwright-%s-%s-firewalld-ready.service" % (identity["context"], identity["object"])
    if name != expected or len(name) > 255:
        raise ValueError("readiness unit does not match the frozen provider")
    return name


def unit_content(name):
    return (
        "# Bootwright owned readiness barrier: " + name + "\n"
        "[Unit]\nDescription=Bootwright managed network firewall readiness\n"
        "After=firewalld.service\nBefore=virtnetworkd.service\n\n"
        "[Service]\nType=oneshot\nRemainAfterExit=yes\nTimeoutStartSec=75\n"
        'ExecStart=/usr/bin/python3 -c "' + POLL + '"\n\n'
        "[Install]\nWantedBy=virtnetworkd.service\n"
    ).encode()


def inspect_files(name, root=SYSTEMD, owner=0):
    """Never follow the owned file or accept another enablement target."""
    for directory in (root, os.path.join(root, "virtnetworkd.service.wants")):
        if os.path.lexists(directory):
            info = os.lstat(directory)
            if not stat.S_ISDIR(info.st_mode) or info.st_uid != owner or stat.S_IMODE(info.st_mode) & 0o022:
                raise ValueError("unsafe readiness unit directory")
    path = os.path.join(root, name)
    present, owned = False, True
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    except FileNotFoundError:
        pass
    else:
        try:
            info = os.fstat(fd)
            present = True
            owned = (stat.S_ISREG(info.st_mode) and info.st_uid == owner and info.st_nlink == 1
                     and stat.S_IMODE(info.st_mode) == 0o644 and os.read(fd, 16385) == unit_content(name))
        finally:
            os.close(fd)
    link = os.path.join(root, "virtnetworkd.service.wants", name)
    enabled = os.path.lexists(link)
    if enabled:
        info = os.lstat(link)
        owned = owned and stat.S_ISLNK(info.st_mode) and info.st_uid == owner and os.readlink(link) == path
    if os.path.lexists(path + ".d"):
        present, owned = True, False
    return present, owned, enabled


def command(runner, arguments):
    return runner(arguments, check_rc=False, environ_update=ENVIRONMENT)


def observe(runner, request, root=SYSTEMD, owner=0):
    name = request.get("firewallReadyUnit", "")
    result = {"name": name, "answered": True, "present": False, "owned": False, "enabled": False, "ready": False, "state": ""}
    try:
        name = unit_name(request)
    except (ValueError, KeyError):
        result["answered"] = False
        return result
    if not name:
        return result
    try:
        present, owned, enabled = inspect_files(name, root, owner)
        result.update(present=present or enabled, owned=owned, enabled=enabled)
        code, output, _error = command(runner, [SYSTEMCTL, "show", "--property=LoadState", "--property=ActiveState",
                                                "--property=FragmentPath", "--property=DropInPaths", name])
        values = dict(line.split("=", 1) for line in output.splitlines() if "=" in line)
        if code not in (0, 4) or values.get("LoadState") not in ("loaded", "not-found"):
            result["answered"] = False
        else:
            result["state"] = values.get("ActiveState", "")
            fragment = values.get("FragmentPath", "")
            if values.get("DropInPaths") or fragment not in ("", os.path.join(root, name)):
                result["owned"] = False
                result["present"] = True
            if not present and values.get("LoadState") == "loaded":
                result["present"] = True
                if result["state"] != "inactive":
                    result["owned"] = False
            if not result["present"] and values.get("LoadState") == "not-found":
                result["owned"] = False
                if result["state"] == "inactive":
                    result["state"] = ""
                else:
                    result["present"] = True
                    if not result["state"]:
                        result["answered"] = False
        if result["answered"] and result["present"] and result["owned"] and result["state"] == "active":
            code, enabled, _error = command(runner, [SYSTEMCTL, "is-enabled", "firewalld.service"])
            if code == 0 and enabled.strip() == "enabled":
                running, _output, _error = command(runner, ["/usr/bin/timeout", "15", "/usr/bin/firewall-cmd", "--state"])
                result["ready"] = running == 0
    except (OSError, ValueError):
        result["answered"] = False
    return result


def require(runner, arguments):
    code, _output, _error = command(runner, arguments)
    if code != 0:
        raise ValueError("the firewall readiness operation failed; restore firewalld and repeat")


def manage(runner, request, state, root=SYSTEMD, owner=0):
    name = unit_name(request)
    if not name:
        return False
    before = observe(runner, request, root, owner)
    if not before["answered"] or (before["present"] and not before["owned"]):
        raise ValueError("the firewall readiness unit or its enablement is unknown or foreign")
    path = os.path.join(root, name)
    wants = os.path.join(root, "virtnetworkd.service.wants")
    link = os.path.join(wants, name)
    changed = False
    if state == "present":
        code, output, _error = command(runner, [SYSTEMCTL, "is-enabled", "firewalld.service"])
        if code != 0 or output.strip() != "enabled":
            raise ValueError("firewalld.service must already be enabled for managed network restart; enable it before applying")
        if not os.path.exists(path):
            fd, stage = tempfile.mkstemp(prefix=".bootwright-firewall-", dir=root)
            try:
                with os.fdopen(fd, "wb") as stream:
                    stream.write(unit_content(name))
                    stream.flush()
                    os.fchmod(stream.fileno(), 0o644)
                    os.fsync(stream.fileno())
                os.replace(stage, path)
                changed = True
            finally:
                if os.path.lexists(stage):
                    os.unlink(stage)
        if not before["enabled"]:
            os.makedirs(wants, mode=0o755, exist_ok=True)
            if not stat.S_ISDIR(os.lstat(wants).st_mode):
                raise ValueError("the network driver's wants directory is not a directory")
            os.symlink(path, link)
            changed = True
        require(runner, [SYSTEMCTL, "daemon-reload"])
        if before["state"] != "active":
            require(runner, [SYSTEMCTL, "start", name])
            changed = True
    else:
        if before["state"] not in ("", "inactive"):
            require(runner, [SYSTEMCTL, "stop", name])
            changed = True
        if before["enabled"]:
            os.unlink(link)
            changed = True
        if os.path.lexists(path):
            os.unlink(path)
            changed = True
        # Reload even after an interrupted unlink, to discard stale loaded state.
        require(runner, [SYSTEMCTL, "daemon-reload"])
    return changed
