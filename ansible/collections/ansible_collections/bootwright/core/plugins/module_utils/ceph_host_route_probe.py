"""Native SSH route checks inside the frozen IBM image, on synthetic loopback.

``prepare`` returns private synthetic keys to the contained collector, which
never leave the host. ``exercise`` uses only those read-only synthetic inputs
and the current tunnel bundle shape. This never starts Ceph, never invokes a
real remote command and never contacts a guest; it proves the image's native
SSH class follows the verified tunnel and refuses every unverified route.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import ast
import asyncio
import contextlib
import errno
import importlib.util
import json
import os
import pwd
import signal
import stat
import subprocess
import sys
import time
import types

SCHEMA = "bootwright-ceph-route-probe-v1"
PREPARE_SCHEMA = "bootwright-ceph-route-prepare-v1"
SYNTHETIC_SCHEMA = "bootwright-ceph-route-synthetic-v1"
SYNTHETIC_ROOT = "/run/bootwright-ceph-route"
TUNNEL_ROOT = "/run/bootwright-ceph-ssh"
PROVIDER_SOURCE = "/usr/share/ceph/mgr/cephadm/ssh.py"
CRYPTO_SOURCE = "/etc/crypto-policies/back-ends/openssh.config"
USER = "bootwright-route"
HOST = "route-target"
UNKNOWN_HOST = "route-unknown"
ADDRESS = "127.0.0.2"
UNKNOWN_ADDRESS = "127.0.0.3"
OUTER_PORT = 2222
INNER_ADDRESS = "127.0.0.1"
INNER_PORT = 22
KEYS = ("client", "outer", "replacement", "inner")
COUNTERS = ("outerConnections", "outerAuthentications", "forwardings", "innerConnections", "effects", "directConnections")
STUBS = ["orchestrator.OrchestratorError", "ceph.deployment.hostspec.normalize_hostname"]
MAX_SYNTHETIC = 128 * 1024
MAX_OUTPUT = 192 * 1024
CASE_LIMIT = 8
AGGREGATE_LIMIT = 45
CLOSE_LIMIT = 0.5
CONTROL_LIMIT = 3
CONTROL_OUTPUT = 8192
CONTROL_BANNER = b"SSH-2.0-BootwrightRoute\r\n"
CONTROL_EXEC = """import os,signal,sys
signal.pthread_sigmask(signal.SIG_SETMASK, tuple(int(value) for value in sys.argv[1].split(',') if value))
os.execve(sys.argv[2],sys.argv[2:],os.environ)
"""
COUNTER_LIMIT = 1024
ERROR_CLASSES = {"none", "unknown", "FileNotFoundError", "PermissionError", "ValueError", "KeyError", "RuntimeError",
                 "OSError", "TimeoutError", "ConfigParseError", "KeyImportError", "ProtocolError", "ConnectionLost",
                 "DisconnectError", "KeyExchangeFailed", "PermissionDenied", "HostKeyNotVerifiable", "HostConnectionError"}
CASE_NAMES = {"correctKey": "correct-key", "wrongKey": "wrong-key", "unknownTarget": "unknown-target",
              "unavailableProxy": "unavailable-proxy", "missingNativeConfig": "missing-native-config"}
PHASES = ("account", "sysctl", "synthetic", "bundle", "keys", "provider", "listener", "cleanup", "runtime", "crypto",
          "native-options", "outer-config", "loopback-control", "control-cleanup", "outer-control-key",
          "outer-control-permission", "outer-control-network", "outer-control-host-key", "outer-control-unknown",
          "proxy-entrypoint")


class Refusal(ValueError):
    """A closed failure without vendor text or synthetic material."""


class CaseFailure(Refusal):
    """Only a fixed case and bounded event counters can enter diagnostics."""

    def __init__(self, case, suffix, counters=None, error_class="none"):
        require(case in CASE_NAMES)
        require(suffix in ("-refusal", "-direct-contact"))
        require(counters is None or isinstance(counters, dict) and set(counters) == set(COUNTERS) and
                all(type(value) is int and 0 <= value <= COUNTER_LIMIT for value in counters.values()))
        require(error_class in ERROR_CLASSES)
        self.case = case
        self.counters = None if counters is None else dict(counters)
        self.error_class = error_class
        self.reason = CASE_NAMES[case] + suffix
        super().__init__(self.reason)


class DirectFallback(CaseFailure):
    """A live honeypot received an unverified direct native connection."""

    def __init__(self, case, counters=None, error_class="none"):
        super().__init__(case, "-direct-contact", counters, error_class)


class CaseRefusal(CaseFailure):
    """A fixed case failed without exposing the native exception."""

    def __init__(self, case, counters=None, error_class="none"):
        super().__init__(case, "-refusal", counters, error_class)


class PhaseRefusal(Refusal):
    """A fixed probe stage failed without exposing source or synthetic data."""

    def __init__(self, phase):
        require(phase in PHASES)
        self.reason = "route-" + phase + "-refusal"
        super().__init__(self.reason)


class ListenerRefusal(PhaseRefusal):
    """Only fixed listener roles and error categories leave the probe."""

    def __init__(self, listener, operation, error):
        require(listener in ("inner", "direct-target", "direct-unknown", "outer", "replacement"))
        require(operation in ("create", "prove"))
        super().__init__("listener")
        self.listener = listener
        self.operation = operation
        self.error_class = exception_class(error)
        categories = {errno.EADDRINUSE: "address-in-use", errno.EACCES: "permission-denied",
                      errno.EPERM: "permission-denied", errno.EADDRNOTAVAIL: "address-unavailable"}
        self.socket_error = "none"
        if isinstance(error, OSError):
            self.socket_error = categories.get(error.errno, "other") if type(error.errno) is int else "other"


@contextlib.contextmanager
def phase(name):
    require(name in PHASES)
    try:
        yield
    except (DirectFallback, CaseRefusal, PhaseRefusal):
        raise
    except Exception:
        raise PhaseRefusal(name) from None


def require(condition):
    if not condition:
        raise Refusal("route-refusal")


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result)
        result[key] = value
    return result


def account():
    value = pwd.getpwnam("ceph")
    require(value.pw_uid > 0 and value.pw_gid > 0)
    return value.pw_uid, value.pw_gid


def public_crypto_policy():
    # This fixed public image file can be a provider-owned symlink. No host
    # directories, image-declared volumes or secrets are mounted over it.
    try:
        with open(CRYPTO_SOURCE, "rb") as stream:
            raw = stream.read(65537)
    except FileNotFoundError:
        return ""
    require(len(raw) <= 65536)
    text = raw.decode("ascii")
    require(all(character in "\t\r\n" or " " <= character <= "~" for character in text))
    return text


def fips_enabled():
    # The contained probe observes the host kernel, never an authored flag.
    try:
        with open("/proc/sys/crypto/fips_enabled", "rb") as stream:
            value = stream.read(3)
    except FileNotFoundError:
        return False
    require(value in (b"0\n", b"1\n"))
    return value == b"1\n"


def synthetic_algorithm():
    return "ecdsa-sha2-nistp256" if fips_enabled() else "ssh-ed25519"


def prepare(ssh):
    with phase("account"):
        uid, gid = account()
    keys = {}
    with phase("keys"):
        algorithm = synthetic_algorithm()
        for name in KEYS:
            key = ssh.generate_private_key(algorithm)
            keys[name] = {"private": key.export_private_key("openssh").decode("ascii"),
                          "public": key.export_public_key("openssh").decode("ascii").strip()}
    with phase("crypto"):
        policy = public_crypto_policy()
    synthetic = {"schema": SYNTHETIC_SCHEMA, "uid": uid, "gid": gid, "user": USER,
                 "cryptoPolicy": policy, "keys": keys}
    with phase("synthetic"):
        validate_synthetic(synthetic, uid, gid)
    return {"schema": PREPARE_SCHEMA, "ok": True, "synthetic": synthetic}


def validate_synthetic(value, uid, gid):
    require(isinstance(value, dict) and set(value) == {"schema", "uid", "gid", "user", "cryptoPolicy", "keys"})
    require(value["schema"] == SYNTHETIC_SCHEMA and value["user"] == USER)
    require(type(value["uid"]) is int and value["uid"] == uid and uid > 0)
    require(type(value["gid"]) is int and value["gid"] == gid and gid > 0)
    policy = value["cryptoPolicy"]
    require(isinstance(policy, str) and len(policy) <= 65536 and policy.isascii())
    require(isinstance(value["keys"], dict) and set(value["keys"]) == set(KEYS))
    for key in value["keys"].values():
        require(isinstance(key, dict) and set(key) == {"private", "public"})
        private, public = key["private"], key["public"]
        require(isinstance(private, str) and 0 < len(private) <= 8192 and private.isascii())
        require(private.startswith("-----BEGIN OPENSSH PRIVATE KEY-----\n") and private.endswith("-----END OPENSSH PRIVATE KEY-----\n"))
        require(isinstance(public, str) and 0 < len(public) <= 4096 and public.isascii())
        require(public.startswith(synthetic_algorithm() + " ") and len(public.split()) == 2 and "\n" not in public and "\r" not in public)
    require(len({key["public"] for key in value["keys"].values()}) == len(KEYS))
    require(len(canonical(value)) <= MAX_SYNTHETIC)
    return value


def read_synthetic(uid, gid):
    directory = os.open(SYNTHETIC_ROOT, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(directory)
        require(info.st_uid == uid and info.st_mode & 0o077 == 0 and os.fstatvfs(directory).f_flag & os.ST_RDONLY)
        require(os.listdir(directory) == ["synthetic.json"])
        descriptor = os.open("synthetic.json", os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, dir_fd=directory)
        with os.fdopen(descriptor, "rb") as stream:
            info = os.fstat(stream.fileno())
            require(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_uid == uid)
            require(info.st_mode & 0o077 == 0 and 0 < info.st_size <= MAX_SYNTHETIC and os.fstatvfs(stream.fileno()).f_flag & os.ST_RDONLY)
            raw = stream.read(MAX_SYNTHETIC + 1)
            require(len(raw) == info.st_size)
    finally:
        os.close(directory)
    return validate_synthetic(json.loads(raw, object_pairs_hook=unique_object), uid, gid)


def fixed_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    require(spec is not None and spec.loader is not None)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def provider_source(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW | os.O_CLOEXEC)
    with os.fdopen(descriptor, "rb") as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_mode & 0o022 == 0 and 0 < info.st_size <= 1024 * 1024)
        raw = stream.read(1024 * 1024 + 1)
        require(len(raw) == info.st_size)
    return raw


def provider_module():
    raw = provider_source(PROVIDER_SOURCE)
    imports_utils = any(isinstance(node, ast.ImportFrom) and node.level == 1 and node.module == "utils"
                        for node in ast.parse(raw, PROVIDER_SOURCE).body)
    utils_path = os.path.join(os.path.dirname(PROVIDER_SOURCE), "utils.py")
    utils_source = provider_source(utils_path) if imports_utils else None
    package_name = "bootwright_native_cephadm"
    stubs = {name: types.ModuleType(name) for name in ("ceph", "ceph.deployment", "ceph.deployment.hostspec", "orchestrator")}
    stubs["ceph"].__path__ = []
    stubs["ceph.deployment"].__path__ = []
    stubs["ceph.deployment.hostspec"].normalize_hostname = lambda value: value
    stubs["orchestrator"].OrchestratorError = Refusal
    modules = dict(stubs)
    package = types.ModuleType(package_name)
    package.__path__ = []
    modules[package_name] = package
    sources = []
    if utils_source is not None:
        sources.append(("utils", utils_path, utils_source))
    sources.append(("ssh", PROVIDER_SOURCE, raw))
    for name, path, unused in sources:
        module = types.ModuleType(package_name + "." + name)
        module.__file__ = path
        module.__package__ = package_name
        modules[module.__name__] = module
        setattr(package, name, module)
    previous = {name: sys.modules.get(name) for name in modules}
    try:
        sys.modules.update(modules)
        for name, path, source in sources:
            exec(compile(source, path, "exec"), modules[package_name + "." + name].__dict__)
        return modules[package_name + ".ssh"]
    finally:
        for name, value in previous.items():
            if value is None:
                sys.modules.pop(name, None)
            else:
                sys.modules[name] = value


def load_keys(ssh, synthetic):
    result = {}
    for name in KEYS:
        key = ssh.import_private_key(synthetic["keys"][name]["private"])
        require(key.export_public_key("openssh").decode("ascii").strip() == synthetic["keys"][name]["public"])
        result[name] = key
    return result


def prove_bundle(helper, synthetic):
    manifest = helper.verify_bundle(TUNNEL_ROOT, uid=synthetic["uid"], readonly=True)
    require(manifest["owner"] == "route-probe" and manifest["fsid"] == "11111111-1111-4111-8111-111111111111")
    require(manifest["nativeUser"] == USER and manifest["hosts"] == [{"name": HOST, "address": ADDRESS, "port": OUTER_PORT}])
    require(manifest["cryptoPolicy"] == helper.crypto_policy(synthetic["cryptoPolicy"]))
    with open(TUNNEL_ROOT + "/identity", "rb") as stream:
        require(stream.read(8193) == synthetic["keys"]["client"]["private"].encode())
    with open(TUNNEL_ROOT + "/known_hosts", "rb") as stream:
        require(stream.read(4097) == ("[" + ADDRESS + "]:" + str(OUTER_PORT) + " " + synthetic["keys"]["outer"]["public"] + "\n").encode())
    return manifest


def parser_preflight(ssh, helper, manifest):
    """Validate both current parsers without opening a connection or shell."""
    with phase("native-options"):
        initial = ssh.SSHClientConnectionOptions(keepalive_interval=2, keepalive_count_max=1)
        options = ssh.SSHClientConnectionOptions(options=initial, host=ADDRESS, username=USER,
                                                 client_keys=[TUNNEL_ROOT + "/identity"], known_hosts=None,
                                                 config=[TUNNEL_ROOT + "/native_config"], preferred_auth=["publickey"])
        require(tuple(options.proxy_command) == ("/usr/bin/python3", "-I", TUNNEL_ROOT + "/proxy.py", "0"))
        require(options.agent_path in (None, "") and options.pkcs11_provider is None)
    with phase("outer-config"):
        argv = helper.proxy_argv(manifest, "0")
        require(argv[0] == "/usr/bin/ssh")
        # Contained probe inside a rootless image, with no AnsibleModule available.
        # pylint: disable-next=ansible-bad-function
        result = subprocess.run([argv[0], "-G"] + argv[1:], stdin=subprocess.DEVNULL,
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=2,
                                env={"PATH": "/usr/bin:/bin", "LANG": "C", "LC_ALL": "C", "HOME": "/"}, cwd="/", check=False)
        require(result.returncode == 0)


def manager(provider, client_public=""):
    class Inventory:
        def __contains__(self, host):
            return host in (HOST, UNKNOWN_HOST)

        def get_addr(self, host):
            require(host in (HOST, UNKNOWN_HOST))
            return ADDRESS if host == HOST else UNKNOWN_ADDRESS

    class Cache:
        def get_host_fips_enabled(self, host):
            require(host in (HOST, UNKNOWN_HOST))
            return None

    mgr = types.SimpleNamespace(inventory=Inventory(), cache=Cache(), ssh_user=USER, ssh_pub=client_public, _ssh_options="route",
                                ssh_keepalive_interval=2, ssh_keepalive_count_max=1,
                                tkey=types.SimpleNamespace(name=TUNNEL_ROOT + "/identity"),
                                ssh_config_fname=TUNNEL_ROOT + "/native_config", sudo_hardening=False,
                                offline_hosts=set(), offline_hosts_remove=lambda host: None)
    return provider.SSHManager(mgr)


def server_factory(ssh, kind, counters, connections, client_key):
    class Server(ssh.SSHServer):
        def connection_made(self, connection):
            connections.add(connection)
            counters["outerConnections" if kind == "outer" else "innerConnections" if kind == "inner" else "directConnections"] += 1

        def connection_lost(self, exception):
            return None

        def begin_auth(self, username):
            return True

        def public_key_auth_supported(self):
            return True

        def validate_public_key(self, username, key):
            return username == USER and key.export_public_key("openssh") == client_key.export_public_key("openssh")

        def auth_completed(self):
            if kind == "outer":
                counters["outerAuthentications"] += 1

        def connection_requested(self, destination, port, origin, origin_port):
            if kind != "outer" or destination != INNER_ADDRESS or port != INNER_PORT:
                return False
            counters["forwardings"] += 1
            return True

    return Server


def process_factory(counters, kind):
    async def effect(process):
        if kind != "inner" or process.command != "sudo true":
            process.exit(1)
            return
        counters["effects"] += 1
        process.stdout.write("route-ok\n")
        process.exit(0)
    return effect


async def close_connections(connections):
    for connection in connections:
        connection.close()
    if connections:
        await asyncio.wait_for(asyncio.gather(*(connection.wait_closed() for connection in connections)), CLOSE_LIMIT)
    connections.clear()


async def close_native(native, connections):
    clients = list(native.cons.values())
    for host in list(native.cons):
        await native._reset_con(host)
    if clients:
        await asyncio.wait_for(asyncio.gather(*(connection.wait_closed() for connection in clients)), CLOSE_LIMIT)
    await close_connections(connections)


def counter_delta(counters, before):
    result = {name: counters[name] - before[name] for name in COUNTERS}
    require(all(type(value) is int and value >= 0 for value in result.values()))
    return result


def exception_class(error):
    """Keep only a fixed deepest class, never native messages or filenames."""
    result, seen = "none" if error is None else "unknown", set()
    for unused in range(8):
        if error is None or id(error) in seen:
            break
        seen.add(id(error))
        name = type(error).__name__
        if name in ERROR_CLASSES:
            result = name
        error = error.__cause__ or error.__context__
    return result


async def run_case(native, provider, counters, host, positive, case, connections=None):
    require(case in CASE_NAMES)
    before = dict(counters)
    command = provider.RemoteCommand(provider.Executables.TRUE)
    rejected = False
    result = None
    error_class = "none"
    try:
        result = await asyncio.wait_for(native._execute_command(host, command), CASE_LIMIT)
    except Exception as error:
        rejected = isinstance(error, provider.HostConnectionError)
        error_class = exception_class(error)
    if connections is not None:
        try:
            with phase("cleanup"):
                await close_native(native, connections)
                await asyncio.sleep(0)
        except Exception:
            delta = counter_delta(counters, before)
            if delta["directConnections"]:
                raise DirectFallback(case, delta, error_class) from None
            raise
    delta = counter_delta(counters, before)
    if delta["directConnections"]:
        raise DirectFallback(case, delta, error_class)
    if not (not positive and rejected or positive and not rejected and result == ("route-ok", "", 0)):
        raise CaseRefusal(case, delta, error_class)
    expected = {name: int(positive and name != "directConnections") for name in COUNTERS}
    if case == "wrongKey":
        expected["outerConnections"] = 1
    if delta != expected:
        raise CaseRefusal(case, delta, error_class)
    return delta


async def finalize_cases(native, connections, listeners, counters, case):
    before = dict(counters)
    try:
        with phase("cleanup"):
            try:
                for listener in listeners:
                    listener.close()
                await asyncio.wait_for(asyncio.gather(*(listener.wait_closed() for listener in listeners)), CLOSE_LIMIT)
            finally:
                await close_native(native, connections)
                await asyncio.sleep(0)
    except Exception:
        late = counter_delta(counters, before)
        if late["directConnections"]:
            raise DirectFallback(case, late) from None
        raise
    late = counter_delta(counters, before)
    if late["directConnections"]:
        raise DirectFallback(case, late)
    if any(late.values()):
        raise CaseRefusal(case, late)


def prove_listener(listener, address, port):
    # AsyncSSH 2.13.2 SSHAcceptor delegates asyncio.Server.sockets; get_port
    # belongs to forwarding listeners and is not part of this acceptor API.
    sockets = listener.sockets
    require(sockets and len(sockets) <= 2)
    require(all(sock.getsockname() == (address, port) for sock in sockets))


def control_banner(raw):
    first = raw.split(b"\n", 1)[0]
    return b"\n" in raw and len(first) <= 255 and first.startswith(b"SSH-2.0-AsyncSSH_")


def control_category(stderr):
    """Only a fixed category leaves this bounded private diagnostic buffer."""
    text = stderr.lower()
    categories = (
        ("host-key", (b"host key verification failed", b"remote host identification has changed")),
        ("key", (b"load key", b"no such identity", b"invalid format", b"unprotected private key", b"bad permissions")),
        ("permission", (b"permission denied", b"operation not permitted")),
        ("network", (b"connection refused", b"connection timed out", b"no route to host", b"network is unreachable")),
    )
    return next((name for name, matches in categories if any(value in text for value in matches)), "unknown")


def stop_control(process):
    # Do not poll/wait before killing: retain the child PID as the group owner.
    os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
    with contextlib.suppress(ProcessLookupError):
        os.killpg(process.pid, signal.SIGKILL)
    process.wait(timeout=CLOSE_LIMIT)


async def read_control(process, stderr):
    stdout = bytearray()
    deadline = time.monotonic() + CONTROL_LIMIT
    for pipe in (process.stdout, process.stderr):
        os.set_blocking(pipe.fileno(), False)
    while time.monotonic() < deadline:
        for pipe, output in ((process.stdout, stdout), (process.stderr, stderr)):
            try:
                block = os.read(pipe.fileno(), 1024)
            except BlockingIOError:
                continue
            # AsyncSSH 2.13.2 treats stderr EOF as loss of the entire proxy
            # connection. A banner alone cannot prove this channel survives.
            if pipe is process.stderr and not block:
                return False
            output.extend(block)
            require(len(output) <= CONTROL_OUTPUT)
        if control_banner(stdout):
            return True
        if os.waitid(os.P_PID, process.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT) is not None:
            return False
        await asyncio.sleep(0.01)
    return False


async def outer_control(helper, manifest, proxy=False):
    require(type(proxy) is bool)
    argv = ["/usr/bin/python3", "-I", TUNNEL_ROOT + "/proxy.py", "0"] if proxy else helper.proxy_argv(manifest, "0")
    require(argv[0] == ("/usr/bin/python3" if proxy else "/usr/bin/ssh"))
    stderr = bytearray()
    blocked = signal.pthread_sigmask(signal.SIG_BLOCK, (signal.SIGALRM,))
    try:
        mask = ",".join(str(int(value)) for value in sorted(blocked))
        # Contained probe child owned as a process group inside a rootless image, with no AnsibleModule available.
        # pylint: disable-next=ansible-bad-function
        process = subprocess.Popen(["/usr/bin/python3", "-I", "-c", CONTROL_EXEC, mask] + argv,
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   start_new_session=True, cwd="/", env={"PATH": "/usr/bin:/bin", "HOME": "/", "LANG": "C", "LC_ALL": "C"})
    except BaseException:
        signal.pthread_sigmask(signal.SIG_SETMASK, blocked)
        raise
    try:
        signal.pthread_sigmask(signal.SIG_SETMASK, blocked)
        os.set_blocking(process.stdin.fileno(), False)
        require(os.write(process.stdin.fileno(), CONTROL_BANNER) == len(CONTROL_BANNER))
        if not await read_control(process, stderr):
            raise PhaseRefusal("proxy-entrypoint" if proxy else "outer-control-" + control_category(stderr))
    finally:
        try:
            with phase("control-cleanup"):
                stop_control(process)
        finally:
            for pipe in (process.stdin, process.stdout, process.stderr):
                pipe.close()


async def seal_control(counters, connections, expected):
    with phase("control-cleanup"):
        before = dict(counters)
        await close_connections(connections)
        await asyncio.sleep(0)
        require(counters == before == expected)
        for name in COUNTERS:
            counters[name] = 0


async def loopback_controls(helper, manifest, counters, connections):
    with phase("loopback-control"):
        reader, writer = await asyncio.wait_for(asyncio.open_connection(INNER_ADDRESS, INNER_PORT, limit=512), 1)
        try:
            writer.write(CONTROL_BANNER)
            await asyncio.wait_for(writer.drain(), 1)
            require(control_banner(await asyncio.wait_for(reader.readline(), 1)))
        finally:
            writer.close()
            await asyncio.wait_for(writer.wait_closed(), CLOSE_LIMIT)
    await seal_control(counters, connections, {name: int(name == "innerConnections") for name in COUNTERS})
    await outer_control(helper, manifest)
    expected = {name: int(name not in ("effects", "directConnections")) for name in COUNTERS}
    await seal_control(counters, connections, expected)
    await outer_control(helper, manifest, proxy=True)
    await seal_control(counters, connections, expected)


async def exercise_cases(ssh, provider, keys, helper=None, manifest=None):
    counters = {name: 0 for name in COUNTERS}
    connections, listeners, cases = set(), [], {}
    native = manager(provider, keys["client"].export_public_key("openssh").decode("ascii").strip())

    async def listen(label, kind, address, port, host_key):
        operation = "create"
        try:
            # AsyncSSH disables address reuse by default. Both outer sockets
            # need it to replace the key after accepted TCP connections close;
            # reuse_port stays off so another live listener cannot share it.
            listener = await ssh.create_server(server_factory(ssh, kind, counters, connections, keys["client"]), address, port,
                                               server_host_keys=[host_key], process_factory=process_factory(counters, kind),
                                               encoding="utf-8", login_timeout=CASE_LIMIT,
                                               reuse_address=True, reuse_port=False)
            listeners.append(listener)
            operation = "prove"
            prove_listener(listener, address, port)
            return listener
        except (DirectFallback, CaseRefusal, PhaseRefusal):
            raise
        except Exception as error:
            raise ListenerRefusal(label, operation, error) from None

    current = "correctKey"
    try:
        await listen("inner", "inner", INNER_ADDRESS, INNER_PORT, keys["inner"])
        await listen("direct-target", "direct", ADDRESS, INNER_PORT, keys["inner"])
        await listen("direct-unknown", "direct", UNKNOWN_ADDRESS, INNER_PORT, keys["inner"])
        outer = await listen("outer", "outer", ADDRESS, OUTER_PORT, keys["outer"])
        if helper is not None:
            await loopback_controls(helper, manifest, counters, connections)
        cases[current] = await run_case(native, provider, counters, HOST, True, current, connections)
        outer.close()
        await outer.wait_closed()
        outer = await listen("replacement", "outer", ADDRESS, OUTER_PORT, keys["replacement"])
        current = "wrongKey"
        cases[current] = await run_case(native, provider, counters, HOST, False, current, connections)
        current = "unknownTarget"
        cases[current] = await run_case(native, provider, counters, UNKNOWN_HOST, False, current, connections)
        outer.close()
        await outer.wait_closed()
        current = "unavailableProxy"
        cases[current] = await run_case(native, provider, counters, HOST, False, current, connections)
        absent = TUNNEL_ROOT + "/absent-native-config"
        require(not os.path.lexists(absent))
        native.mgr.ssh_config_fname = absent
        current = "missingNativeConfig"
        cases[current] = await run_case(native, provider, counters, HOST, False, current, connections)
        return cases
    finally:
        await finalize_cases(native, connections, listeners, counters, current)


async def exercise(ssh):
    with phase("account"):
        uid, gid = account()
        require((os.geteuid(), os.getegid()) == (uid, gid))
    with phase("sysctl"):
        with open("/proc/sys/net/ipv4/ip_unprivileged_port_start", "rt", encoding="ascii") as stream:
            require(stream.read(32).strip() == "0")
    with phase("synthetic"):
        synthetic = read_synthetic(uid, gid)
    with phase("bundle"):
        helper = fixed_module("bootwright_tunnel_route", TUNNEL_ROOT + "/proxy.py")
        manifest = prove_bundle(helper, synthetic)
    with phase("keys"):
        keys = load_keys(ssh, synthetic)
    with phase("provider"):
        provider = provider_module()
    parser_preflight(ssh, helper, manifest)
    cases = await asyncio.wait_for(exercise_cases(ssh, provider, keys, helper, manifest), AGGREGATE_LIMIT - 2)
    return {"schema": SCHEMA, "ok": True, "scope": "native-ssh-class-loopback",
            "dependencyStubs": list(STUBS), "cases": cases}


def execute(arguments):
    require(len(arguments) == 1 and arguments[0] in ("prepare", "exercise"))
    with phase("runtime"):
        import asyncssh
    return prepare(asyncssh) if arguments[0] == "prepare" else asyncio.run(exercise(asyncssh))


def publish(descriptor, value):
    data = memoryview(canonical(value))
    require(len(data) <= MAX_OUTPUT)
    os.set_blocking(descriptor, False)
    deadline = time.monotonic() + 1
    while data:
        require(time.monotonic() < deadline)
        try:
            count = os.write(descriptor, data[:65536])
        except BlockingIOError:
            time.sleep(0.01)
            continue
        require(count > 0)
        data = data[count:]


def main(arguments=None):
    arguments = sys.argv[1:] if arguments is None else arguments
    output = os.dup(1)
    quiet = os.open(os.devnull, os.O_WRONLY | os.O_CLOEXEC)
    os.dup2(quiet, 1)
    os.dup2(quiet, 2)
    os.close(quiet)
    signal.signal(signal.SIGALRM, lambda *args: (unused for unused in ()).throw(Refusal("synthetic-refusal")))
    signal.alarm(AGGREGATE_LIMIT)
    status = 0
    try:
        value = execute(arguments)
    except (DirectFallback, CaseRefusal, PhaseRefusal) as error:
        status = 1
        value = {"schema": SCHEMA, "ok": False, "reason": error.reason}
        if isinstance(error, CaseFailure) and error.counters is not None:
            value.update(case=error.case, counters=error.counters, errorClass=error.error_class)
        if isinstance(error, ListenerRefusal):
            value.update(listener=error.listener, operation=error.operation,
                         errorClass=error.error_class, socketError=error.socket_error)
    except Exception:
        status = 1
        value = {"schema": SCHEMA, "ok": False, "reason": "route-refusal"}
    finally:
        signal.alarm(0)
    try:
        publish(output, value)
    except (OSError, ValueError):
        status = 1
    finally:
        os.close(output)
    return status


# Script entry point run inside the frozen image, with no AnsibleModule available.
if __name__ == "__main__":
    sys.exit(main())  # pylint: disable=ansible-bad-function
