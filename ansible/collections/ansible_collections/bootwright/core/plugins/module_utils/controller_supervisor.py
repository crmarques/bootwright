"""Invocation-local child supervision for authorized controller actions."""

import ctypes
import os
import sys


def main():
    root = os.path.dirname(os.path.dirname(sys.executable))
    version = str(sys.version_info.major) + "." + str(sys.version_info.minor)
    stdlib = root + "/lib/python" + version
    sys.path[:] = [
        root + "/lib/python" + version.replace(".", "") + ".zip",
        stdlib,
        stdlib + "/lib-dynload",
        stdlib + "/site-packages",
    ]
    if not (sys.flags.isolated and sys.flags.no_site and sys.flags.dont_write_bytecode):
        raise RuntimeError("qualified interpreter flags required")
    libc = ctypes.CDLL(None, use_errno=True)
    libc.prctl.argtypes = [
        ctypes.c_int,
        ctypes.c_ulong,
        ctypes.c_ulong,
        ctypes.c_ulong,
        ctypes.c_ulong,
    ]
    libc.prctl.restype = ctypes.c_int
    if libc.prctl(36, 1, 0, 0, 0) != 0:
        os._exit(125)
    child = os.fork()
    if child == 0:
        try:
            # This controller-only supervisor deliberately invokes the pinned CLI.
            # pylint: disable=ansible-bad-module-import
            from ansible.cli.playbook import main

            # pylint: enable=ansible-bad-module-import

            sys.argv[0] = "ansible-playbook"
            main()
        except SystemExit as failure:
            os._exit(
                failure.code
                if isinstance(failure.code, int)
                else int(bool(failure.code))
            )
        except BaseException:
            os._exit(1)
        os._exit(0)
    status = 125
    while True:
        try:
            descendant, result = os.waitpid(-1, 0)
        except InterruptedError:
            continue
        except ChildProcessError:
            break
        if descendant == child:
            status = os.waitstatus_to_exitcode(result)
            if status < 0:
                status = 128 - status
    os._exit(status)


if __name__ == "__main__":
    main()
