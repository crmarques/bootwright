"""Candidate native Ceph SSH transport; qualification never follows from rendering.

The fixed proxy entrypoint verifies its protected, read-only bundle and replaces
itself with one OpenSSH process. There is no shell, direct connection fallback,
background master, inherited agent, or child left behind by the wrapper. The
native client owns session cancellation; OpenSSH bounds connection establishment
and stops a peer which ceases answering. An active session has no arbitrary
lifetime limit, because native cluster operations may legitimately be long.
"""

from __future__ import annotations

# Exact type identity is the validation here: a bool is not an int and a capability object is never a subclass.
# pylint: disable=unidiomatic-typecheck

import base64
import hashlib
import ipaddress
import json
import os
import re
import stat
import sys
import uuid

CONTAINER_ROOT = "/run/bootwright-ceph-ssh"
MAX_FILE = 2 * 1024 * 1024
FILES = frozenset(("identity", "known_hosts", "outer_config", "native_config", "proxy.py"))
NAME = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}\Z")
HEX = re.compile(r"[0-9a-f]{64}\Z")
KEY_TYPES = frozenset(("ssh-ed25519", "ssh-rsa", "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521"))
CRYPTO_DIRECTIVES = frozenset((
    "ciphers", "macs", "kexalgorithms", "gssapikexalgorithms", "hostkeyalgorithms",
    "pubkeyacceptedalgorithms", "pubkeyacceptedkeytypes", "hostbasedacceptedalgorithms",
    "hostbasedkeytypes", "casignaturealgorithms", "requiredrsasize", "rsaminsize", "rekeylimit",
))


HOST_KEY_ALGORITHMS = {
    "ssh-ed25519": ("ssh-ed25519",),
    "ssh-rsa": ("rsa-sha2-512", "rsa-sha2-256"),
    "ecdsa-sha2-nistp256": ("ecdsa-sha2-nistp256",),
    "ecdsa-sha2-nistp384": ("ecdsa-sha2-nistp384",),
    "ecdsa-sha2-nistp521": ("ecdsa-sha2-nistp521",),
}


class TunnelRefusal(ValueError):
    """A fixed reason, without paths, keys, requests or native output."""


class HostKeyRefusal(TunnelRefusal):
    """A pinned host key whose every algorithm the effective policy excludes; it names only the host and the key type."""

    def __init__(self, host, key_type):
        super().__init__("tunnel-host-key-excluded")
        self.host = host
        self.key_type = key_type


def require(condition):
    if not condition:
        raise TunnelRefusal("native-tunnel-unproved")


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True).encode()


def named(value):
    return isinstance(value, str) and NAME.fullmatch(value) is not None


def crypto_policy(value):
    """Accept only the already bound crypto backend, never Include or Match."""
    require(isinstance(value, str) and len(value) <= 65536)
    lines = []
    for raw in value.splitlines():
        line = raw.split("#", 1)[0].strip()
        if not line:
            continue
        match = re.fullmatch(r"([A-Za-z]+)\s*(?:=\s*|\s+)([A-Za-z0-9@.,_+^*?!/ -]+)", line)
        require(match is not None and match.group(1).lower() in CRYPTO_DIRECTIVES)
        lines.append(line)
    return "\n".join(lines) + ("\n" if lines else "")


def targets(request):
    """A native address selects exactly one original installation endpoint."""
    require(isinstance(request, dict) and request.get("version") == "ceph-core-v26")
    require(request.get("clusterSSHTransport") == "verified-tunnel")
    require(named(request.get("owner")) and named(request.get("clusterSSHUser")))
    try:
        require(str(uuid.UUID(request["fsid"])) == request["fsid"])
    except (ValueError, TypeError, AttributeError, KeyError):
        raise TunnelRefusal("native-tunnel-unproved") from None
    hosts = request.get("hosts")
    require(isinstance(hosts, list) and 1 <= len(hosts) <= 64)
    result = []
    for host in hosts:
        require(isinstance(host, dict) and named(host.get("name")))
        address, port = host.get("address"), host.get("port")
        try:
            parsed = ipaddress.ip_address(address)
        except (ValueError, TypeError):
            raise TunnelRefusal("native-tunnel-unproved") from None
        require(isinstance(address, str) and str(parsed) == address and "%" not in address)
        require(not parsed.is_unspecified and not parsed.is_multicast)
        require(type(port) is int and 1 <= port <= 65535)
        result.append({"name": host["name"], "address": address, "port": port})
    require(len({host["name"] for host in result}) == len(result))
    require(len({host["address"] for host in result}) == len(result))
    return sorted(result, key=lambda host: host["name"])


def public_key(value):
    """Strip comments while refusing options, second keys and mislabeled blobs."""
    require(isinstance(value, str) and len(value) <= 32768 and len(value.strip().splitlines()) == 1)
    parts = value.split()
    require(len(parts) >= 2 and parts[0] in KEY_TYPES)
    try:
        blob = base64.b64decode(parts[1], validate=True)
        length = int.from_bytes(blob[:4], "big")
        require(4 <= len(blob) <= 16384 and 0 < length <= len(blob) - 4)
        require(blob[4:4 + length].decode("ascii") == parts[0])
    except (ValueError, UnicodeError):
        raise TunnelRefusal("native-tunnel-unproved") from None
    return parts[0] + " " + parts[1]


def authorization_line(public, owner):
    """Allow Ceph commands and only same-host loopback SSH TCP forwarding."""
    require(named(owner))
    return 'restrict,port-forwarding,permitopen="127.0.0.1:22" ' + public_key(public) + " bootwright:" + owner


def guest_policy(user):
    """Key options cannot forbid remote/Unix listeners; sshd must do so too."""
    require(named(user))
    return ("Match User " + user + "\n  AllowTcpForwarding local\n  PermitListen none\n"
            "  PermitOpen 127.0.0.1:22\n  AllowStreamLocalForwarding no\n  PermitTunnel no\nMatch all\n").encode()


def verify_guest_policy(output):
    """Consume sshd -T -C output only after trusted local configuration proof."""
    require(isinstance(output, bytes) and 0 < len(output) <= 256 * 1024)
    try:
        lines = output.decode("ascii").splitlines()
    except UnicodeError:
        raise TunnelRefusal("native-tunnel-unproved") from None
    expected = {"allowtcpforwarding": "local", "permitlisten": "none", "permitopen": "127.0.0.1:22",
                "allowstreamlocalforwarding": "no", "permittunnel": "no"}
    values = {}
    for line in lines:
        fields = line.split(None, 1)
        require(len(fields) == 2)
        if fields[0] in expected:
            require(fields[0] not in values)
            values[fields[0]] = fields[1]
    require(values == expected)
    return {"loopbackSSHOnly": True}


def host_token(host):
    return host["address"] if host["port"] == 22 else "[" + host["address"] + "]:" + str(host["port"])


def known_hosts(value, hosts):
    """No patterns, extra endpoints, host certificates, hashed aliases or options."""
    require(isinstance(value, bytes) and 0 < len(value) <= MAX_FILE)
    try:
        lines = value.decode("ascii").splitlines()
    except UnicodeError:
        raise TunnelRefusal("native-tunnel-unproved") from None
    expected = {host_token(host) for host in hosts}
    bound = {}
    for line in lines:
        if not line.strip() or line.lstrip().startswith("#"):
            continue
        fields = line.split()
        require(len(fields) == 3 and fields[0] in expected and fields[0] not in bound)
        bound[fields[0]] = public_key(" ".join(fields[1:]))
    require(set(bound) == expected)
    return ("\n".join(host_token(host) + " " + bound[host_token(host)] for host in hosts) + "\n").encode()


def pattern_matches(name, pattern):
    """OpenSSH match_pattern: '*' and '?' are the only wildcards, and the match is case-sensitive."""
    expression = "".join(".*" if char == "*" else "." if char == "?" else re.escape(char) for char in pattern)
    return re.fullmatch(expression, name, re.DOTALL) is not None


def listed(name, patterns):
    """OpenSSH match_pattern_list: a matching negated item excludes the name; otherwise any matching item admits it."""
    found = False
    for item in patterns.split(","):
        if item.startswith("!"):
            if pattern_matches(name, item[1:]):
                return False
        elif pattern_matches(name, item):
            found = True
    return found


def policy_host_key_algorithms(policy):
    """The policy's HostKeyAlgorithms value: under ssh_config(5) the first obtained value wins; None without one."""
    for line in crypto_policy(policy).splitlines():
        match = re.fullmatch(r"([A-Za-z]+)\s*(?:=\s*|\s+)(.*)", line)
        if match is not None and match.group(1).lower() == "hostkeyalgorithms":
            value = match.group(2).strip()
            require(value != "" and len(value.split()) == 1)
            return value
    return None


def pinned_host_key_algorithms(key_type, value):
    """The pinned key's algorithms, in pinned order, that a HostKeyAlgorithms value admits under ssh_config(5) list and pattern rules.

    An absent value, or one that appends ('+') or prepends ('^') to the client's default, keeps every algorithm; one that removes ('-')
    keeps those it does not list; any other value keeps only those it lists.
    """
    require(key_type in HOST_KEY_ALGORITHMS)
    candidates = HOST_KEY_ALGORITHMS[key_type]
    if value is None or value[:1] in ("+", "^"):
        return list(candidates)
    if value[:1] == "-":
        return [name for name in candidates if not listed(name, value[1:])]
    return [name for name in candidates if listed(name, value)]


def host_blocks(policy, hosts, known):
    """One block per host, in the given order, pinning only the algorithms of its known key the policy admits."""
    require(isinstance(known, bytes))
    try:
        lines = [line.split() for line in known.decode("ascii").splitlines()]
    except UnicodeError:
        raise TunnelRefusal("native-tunnel-unproved") from None
    value = policy_host_key_algorithms(policy)
    result = ""
    for host in hosts:
        found = [fields for fields in lines if fields and fields[0] == host_token(host)]
        require(len(found) == 1 and len(found[0]) >= 2)
        algorithms = pinned_host_key_algorithms(found[0][1], value)
        if not algorithms:
            raise HostKeyRefusal(host["name"], found[0][1])
        result += "Host " + host["address"] + "\n  HostKeyAlgorithms " + ",".join(algorithms) + "\n"
    return result


def outer_configuration(policy="", hosts=(), known=b""):
    """One isolated OpenSSH configuration, with no executable-valued directive."""
    options = (
        ("UserKnownHostsFile", CONTAINER_ROOT + "/known_hosts"),
        ("GlobalKnownHostsFile", "/dev/null"), ("KnownHostsCommand", "none"),
        ("StrictHostKeyChecking", "yes"), ("VerifyHostKeyDNS", "no"),
        ("UpdateHostKeys", "no"), ("CheckHostIP", "no"),
        ("IdentityFile", CONTAINER_ROOT + "/identity"),
        ("IdentitiesOnly", "yes"), ("IdentityAgent", "none"), ("CertificateFile", "none"),
        ("PKCS11Provider", "none"),
        ("PreferredAuthentications", "publickey"), ("PubkeyAuthentication", "yes"),
        ("PasswordAuthentication", "no"), ("KbdInteractiveAuthentication", "no"),
        ("GSSAPIAuthentication", "no"), ("HostbasedAuthentication", "no"),
        ("ForwardAgent", "no"), ("ForwardX11", "no"), ("AddKeysToAgent", "no"),
        ("ProxyCommand", "none"), ("ProxyJump", "none"), ("PermitLocalCommand", "no"),
        ("CanonicalizeHostname", "no"), ("ControlMaster", "no"),
        ("ControlPath", "none"), ("ControlPersist", "no"),
        ("ClearAllForwardings", "yes"), ("RequestTTY", "no"), ("EscapeChar", "none"),
        ("BatchMode", "yes"), ("ConnectTimeout", "10"), ("ConnectionAttempts", "1"),
        ("ServerAliveInterval", "15"), ("ServerAliveCountMax", "3"),
        ("ExitOnForwardFailure", "yes"), ("LogLevel", "ERROR"),
    )
    blocks = host_blocks(policy, hosts, known)
    return (blocks + "Host *\n" + "".join("  " + key + " " + value + "\n" for key, value in options) + crypto_policy(policy)).encode()


def native_configuration(hosts):
    """Literal targets only; an unknown target executes the refusing wrapper."""
    result = ""
    for index, host in enumerate(hosts):
        result += "Host " + host["address"] + "\n"
        result += "  ProxyCommand /usr/bin/python3 -I " + CONTAINER_ROOT + "/proxy.py " + str(index) + "\n"
    result += "Host *\n  StrictHostKeyChecking yes\n  ForwardAgent no\n"
    result += "  IdentityAgent none\n  IdentitiesOnly yes\n  IdentityFile " + CONTAINER_ROOT + "/identity\n"
    result += "  CertificateFile none\n  PKCS11Provider none\n  PreferredAuthentications publickey\n"
    result += "  PasswordAuthentication no\n  KbdInteractiveAuthentication no\n  HostbasedAuthentication no\n"
    result += "  GSSAPIAuthentication no\n  GSSAPIKeyExchange no\n  ForwardX11 no\n  ForwardX11Trusted no\n  RequestTTY no\n"
    result += "  ProxyCommand /usr/bin/python3 -I " + CONTAINER_ROOT + "/proxy.py reject\n"
    return result.encode()


def bundle(request, private_key, frozen_known_hosts, proxy_source, policy="", public=None):
    """Render candidate material; callers publish only from their fixed closure."""
    hosts = targets(request)
    require(isinstance(private_key, bytes) and 0 < len(private_key) <= MAX_FILE)
    require(isinstance(proxy_source, bytes) and 0 < len(proxy_source) <= MAX_FILE)
    policy = crypto_policy(policy)
    known = known_hosts(frozen_known_hosts, hosts)
    files = {"identity": private_key, "known_hosts": known,
             "outer_config": outer_configuration(policy, hosts, known), "native_config": native_configuration(hosts), "proxy.py": proxy_source}
    manifest = {"version": "ceph-tunnel-v3", "owner": request["owner"], "fsid": request["fsid"],
                "nativeUser": request["clusterSSHUser"], "hosts": hosts, "cryptoPolicy": policy,
                "identityPublicKey": public_key(public),
                "files": {name: hashlib.sha256(value).hexdigest() for name, value in files.items() if name != "identity"}}
    files["manifest.json"] = canonical(manifest) + b"\n"
    return files


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result)
        result[key] = value
    return result


def validate_manifest(value):
    require(isinstance(value, dict) and set(value) == {"version", "owner", "fsid", "nativeUser", "hosts", "cryptoPolicy", "identityPublicKey", "files"})
    require(value["version"] == "ceph-tunnel-v3")
    request = {"version": "ceph-core-v26", "clusterSSHTransport": "verified-tunnel",
               "owner": value["owner"], "fsid": value["fsid"], "clusterSSHUser": value["nativeUser"], "hosts": value["hosts"]}
    require(targets(request) == value["hosts"])
    require(all(set(host) == {"name", "address", "port"} for host in value["hosts"]))
    require(crypto_policy(value["cryptoPolicy"]) == value["cryptoPolicy"])
    require(isinstance(value["files"], dict) and set(value["files"]) == FILES - {"identity"})
    require(all(isinstance(value, str) and HEX.fullmatch(value) for value in value["files"].values()))
    require(public_key(value["identityPublicKey"]) == value["identityPublicKey"])
    return value


def identity_correspondence(private, public):
    """Use the provider's existing parser inside its isolated image only."""
    try:
        import asyncssh
        derived = asyncssh.import_private_key(private).export_public_key("openssh").decode("ascii")
        require(public_key(derived) == public)
    except (ImportError, ValueError, UnicodeError, TypeError, AttributeError):
        raise TunnelRefusal("native-tunnel-unproved") from None


def read_private(directory, name, uid, readonly=False):
    descriptor = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK, dir_fd=directory)
    with os.fdopen(descriptor, "rb") as source:
        info = os.fstat(source.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_uid == uid and info.st_nlink == 1)
        require(info.st_mode & 0o077 == 0 and 0 < info.st_size <= MAX_FILE)
        require(not readonly or os.fstatvfs(source.fileno()).f_flag & os.ST_RDONLY)
        value = source.read(MAX_FILE + 1)
        require(len(value) == info.st_size)
        return value


def verified_bundle(path, expected, uid, readonly):
    uid = os.geteuid() if uid is None else uid
    require(type(uid) is int and uid >= 0)
    directory = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        info = os.fstat(directory)
        require(info.st_uid == uid and info.st_mode & 0o077 == 0)
        require(not readonly or os.fstatvfs(directory).f_flag & os.ST_RDONLY)
        require(set(os.listdir(directory)) == FILES | {"manifest.json"})
        files = {name: read_private(directory, name, uid, readonly) for name in FILES | {"manifest.json"}}
    finally:
        os.close(directory)
    try:
        manifest = validate_manifest(json.loads(files["manifest.json"], object_pairs_hook=unique_object))
    except (ValueError, UnicodeError):
        raise TunnelRefusal("native-tunnel-unproved") from None
    require(files["manifest.json"] == canonical(manifest) + b"\n")
    require(all(hashlib.sha256(files[name]).hexdigest() == manifest["files"][name] for name in FILES - {"identity"}))
    require(files["known_hosts"] == known_hosts(files["known_hosts"], manifest["hosts"]))
    require(files["outer_config"] == outer_configuration(manifest["cryptoPolicy"], manifest["hosts"], files["known_hosts"]))
    require(files["native_config"] == native_configuration(manifest["hosts"]))
    require(readonly or expected is not None)
    require(expected is None or files == expected)
    if readonly:
        identity_correspondence(files["identity"], manifest["identityPublicKey"])
    return manifest


def verify_bundle(path, expected=None, uid=None, readonly=False):
    """Re-prove the exact restart bundle, rather than trusting a marker alone."""
    try:
        return verified_bundle(path, expected, uid, readonly)
    except (OSError, ValueError, TypeError, KeyError, AttributeError):
        raise TunnelRefusal("native-tunnel-unproved") from None


def proxy_argv(manifest, selector):
    """No input selects an executable, a forwarding destination or shell text."""
    manifest = validate_manifest(manifest)
    require(isinstance(selector, str) and re.fullmatch(r"0|[1-9][0-9]?", selector))
    index = int(selector)
    require(index < len(manifest["hosts"]))
    host = manifest["hosts"][index]
    family = "inet6" if ":" in host["address"] else "inet"
    return ["/usr/bin/ssh", "-T", "-E", "/dev/null", "-F", CONTAINER_ROOT + "/outer_config", "-i", CONTAINER_ROOT + "/identity",
            "-o", "AddressFamily=" + family, "-l", manifest["nativeUser"], "-p", str(host["port"]),
            "-W", "127.0.0.1:22", "--", host["address"]]


def proxy_main(arguments=None):
    """Exec in place: native cancellation reaches SSH with no wrapper child."""
    arguments = sys.argv[1:] if arguments is None else arguments
    try:
        require(len(arguments) == 1 and isinstance(arguments[0], str) and re.fullmatch(r"0|[1-9][0-9]?", arguments[0]))
        manifest = verify_bundle(CONTAINER_ROOT, readonly=True)
        argv = proxy_argv(manifest, arguments[0])
        # AsyncSSH 2.13.2 treats EOF on any proxy pipe, including stderr, as
        # loss of the SSH connection. Keep that pipe open; OpenSSH's fixed
        # -E destination suppresses diagnostics without replacing fd 2.
        os.chdir("/")
        os.execve("/usr/bin/ssh", argv, {"PATH": "/usr/bin:/bin", "LANG": "C", "LC_ALL": "C", "HOME": "/"})
    except (OSError, ValueError, TypeError, KeyError, AttributeError):
        return 255
    return 255


# Script entry point of the SSH proxy command, with no AnsibleModule available.
if __name__ == "__main__":
    sys.exit(proxy_main())  # pylint: disable=ansible-bad-function
