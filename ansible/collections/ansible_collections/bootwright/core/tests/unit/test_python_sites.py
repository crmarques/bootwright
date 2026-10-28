"""Each Python site encodes canonical JSON and starts processes in one module.

The collection's controller and managed-host code encodes canonical JSON only
in module_utils/canonical_json.py and starts a process only in
module_utils/bounded_process.py, which the shipped Ceph and package worker
bundles carry. Starting a process means a subprocess call, os.system, os.popen,
os.fork, os.forkpty, any os.exec* or os.spawn* function, posix_spawn or
pty.spawn, found in the syntax tree through any import alias; a command string
inside a SOURCE constant is not a call, so such a file is named only when it
also calls one. A file that runs alone (a script shipped as source, a worker in
an image), a supervisor that forks or replaces itself on purpose, and an encoding
that differs on purpose are named exceptions below, each with its reason. The
lists only shrink: a file leaves its list with the change that makes it use the
module, and an exception that no longer matches fails.
"""

from __future__ import annotations

import ast
import pathlib

COLLECTION = pathlib.Path(__file__).resolve().parents[2]

CANONICAL_OWNER = "plugins/module_utils/canonical_json.py"
RUNNER_OWNER = "plugins/module_utils/bounded_process.py"

CANONICAL_EXCEPTIONS = {
    "plugins/module_utils/ceph_tunnel.py": "its source is the SSH proxy script, shipped alone and run as python3 -I",
    "plugins/module_utils/ceph_host_route_probe.py": "its source runs alone inside the frozen Ceph image",
    "plugins/module_utils/ceph_native_worker.py": "the supervisor starts in a collection-free interpreter before the closed bundle exists",
    "plugins/module_utils/ceph_package_worker.py": "the supervisor starts in a collection-free interpreter before the closed bundle exists",
    "plugins/module_utils/physical_erase_worker.py": "the erase RAM image carries its three files and no sibling module",
    "plugins/module_utils/managedos_rhsm_worker.py": "the guest worker is one source sent over standard input and run alone",
    "plugins/module_utils/managedos_update_worker.py": "the guest worker is one source sent over standard input and run alone",
    "plugins/module_utils/ceph_native_object.py": "the signed policy keeps raw UTF-8 and Go's escapes, so it is not the ASCII canonical form",
    "plugins/module_utils/managedos_network.py": "renders display text with default=str, not canonical bytes",
    "plugins/filter/to_installer_yaml.py": "renders an indented installer document, not a canonical form",
}

PROCESS_EXCEPTIONS = {
    "plugins/module_utils/physical_erase_process.py": "the one runner of the erase RAM image, which carries no sibling module; the erase controller shares it",
    "plugins/module_utils/managedos_rhsm_worker.py": "the guest worker is one source sent over standard input and run alone",
    "plugins/module_utils/managedos_update_worker.py": "the guest worker is one source sent over standard input and run alone",
    "plugins/module_utils/ceph_host_route_probe.py": "its source runs alone inside the frozen Ceph image, where it owns a probe child's group",
    "plugins/module_utils/ceph_tunnel.py": "os.execve replaces the proxy script with the fixed ssh client, so cancellation reaches ssh with no wrapper",
    "plugins/module_utils/controller_supervisor.py": "os.fork makes the playbook child of the controller-only supervisor, a subreaper owning every descendant",
    "plugins/module_utils/ceph_native_worker.py": "os.fork makes the fixed native worker's session leader in a supervisor that starts collection-free",
    "plugins/module_utils/ceph_package_worker.py": "os.fork makes the fixed package worker's session leader in a supervisor that starts collection-free",
}

PROCESS_CALLS = frozenset(("Popen", "run", "call", "check_call", "check_output", "getoutput", "getstatusoutput"))
OS_PROCESS_CALLS = frozenset(("system", "popen", "fork", "forkpty", "posix_spawn", "posix_spawnp",
                              "execl", "execle", "execlp", "execlpe", "execv", "execve", "execvp", "execvpe",
                              "spawnl", "spawnle", "spawnlp", "spawnlpe", "spawnv", "spawnve", "spawnvp", "spawnvpe"))
PTY_PROCESS_CALLS = frozenset(("spawn", "fork"))


def names_of(tree, module):
    """The local names that mean the module, and those that mean one of its members."""
    modules, members = set(), {}
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            modules.update((alias.asname or alias.name) for alias in node.names if alias.name == module)
        elif isinstance(node, ast.ImportFrom) and node.module == module:
            members.update({(alias.asname or alias.name): alias.name for alias in node.names})
    return modules, members


def calls(tree, module, accepted):
    """The calls of an accepted member of the module, through any alias."""
    modules, members = names_of(tree, module)
    found = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        function = node.func
        if isinstance(function, ast.Attribute) and isinstance(function.value, ast.Name) and function.value.id in modules \
                and function.attr in accepted:
            found.append(node)
        elif isinstance(function, ast.Name) and members.get(function.id) in accepted:
            found.append(node)
    return found


def encodes_canonical(tree):
    """A json.dumps or json.dump that sorts its keys: the shape of a canonical encoder."""
    return [node for node in calls(tree, "json", ("dumps", "dump"))
            if any(keyword.arg == "sort_keys" and not (isinstance(keyword.value, ast.Constant) and keyword.value.value is False)
                   for keyword in node.keywords)]


def starts_process(tree):
    return calls(tree, "subprocess", PROCESS_CALLS) + calls(tree, "os", OS_PROCESS_CALLS) + calls(tree, "pty", PTY_PROCESS_CALLS)


def scan(finder):
    """The relative path of each plugin file the finder matches."""
    found = set()
    for path in sorted((COLLECTION / "plugins").rglob("*.py")):
        if finder(ast.parse(path.read_text())):
            found.add(path.relative_to(COLLECTION).as_posix())
    assert found, "no plugin file matched under %s" % COLLECTION
    return found


def check(found, owner, exceptions, what, instead):
    assert owner in found, "%s no longer %s; the site has no owner" % (owner, what)
    for path in sorted(found - {owner} - set(exceptions)):
        raise AssertionError("%s %s; %s, or name the exception with its reason" % (path, what, instead))
    for path, reason in exceptions.items():
        assert reason and path in found, "%s no longer %s; remove its exception" % (path, what)
        assert path != owner, "%s is the owner, not an exception" % path


def test_canonical_json_is_encoded_in_one_module_per_site():
    check(scan(encodes_canonical), CANONICAL_OWNER, CANONICAL_EXCEPTIONS, "encodes canonical JSON",
          "import canonical from module_utils/canonical_json.py")


def test_processes_start_in_one_runner_per_site():
    check(scan(starts_process), RUNNER_OWNER, PROCESS_EXCEPTIONS, "starts a process",
          "run it through module_utils/bounded_process.py")


def test_the_finders_see_aliases_members_and_nothing_in_strings():
    canonical = ast.parse("import json as j\nj.dumps(1, sort_keys=True)\nfrom json import dump as d\nd(1, None, sort_keys=True)\n")
    assert len(encodes_canonical(canonical)) == 2
    quiet = ast.parse("import json\njson.dumps(1)\njson.dumps(1, sort_keys=False)\nSOURCE = 'json.dumps(1, sort_keys=True)'\n")
    assert encodes_canonical(quiet) == []
    process = ast.parse("import subprocess as sp, os\nsp.Popen(['/x'])\nfrom subprocess import run\nrun(['/x'])\nos.system('x')\n")
    assert len(starts_process(process)) == 3
    replaced = ast.parse("import os as o, pty\no.fork()\no.forkpty()\no.execve('/x', [], {})\no.execvp('x', [])\no.spawnv(0, '/x', [])\npty.spawn(['x'])\n"
                         "from os import execl as e\ne('/x', 'x')\n")
    assert len(starts_process(replaced)) == 7
    inert = ast.parse("import subprocess, os\nsubprocess.PIPE\nsubprocess.TimeoutExpired\nos.kill(1, 9)\nos.getpid()\nSOURCE = 'subprocess.run([])'\n")
    assert starts_process(inert) == []


def test_a_file_off_its_site_module_and_off_the_list_is_refused():
    found = {CANONICAL_OWNER, "plugins/module_utils/new_copy.py"}
    try:
        check(found, CANONICAL_OWNER, {}, "encodes canonical JSON", "import canonical")
    except AssertionError as refused:
        assert "new_copy.py encodes canonical JSON" in str(refused)
    else:
        raise AssertionError("an unlisted copy was accepted")
    try:
        check({CANONICAL_OWNER}, CANONICAL_OWNER, {"plugins/module_utils/gone.py": "was shipped alone"}, "encodes canonical JSON", "import")
    except AssertionError as refused:
        assert "gone.py no longer encodes canonical JSON" in str(refused)
    else:
        raise AssertionError("a stale exception was accepted")


QUOTING_OWNER = "plugins/module_utils/remote_command.py"
SSH_CLIENT = "/usr/bin/ssh"

# Each file that names the ssh client builds or checks an ssh argv; the reason says where its remote command, if any, is joined.
SSH_SITES = {
    "plugins/module_utils/managedos_guest_ssh.py": "the guest session argv; it takes the remote command as an argv and joins it through remote_command",
    "plugins/module_utils/ceph_ssh_policy.py": "the Ceph session argv; ceph_core appends a fixed worker remote_command joined",
    "plugins/module_utils/ceph_tunnel.py": "a -W forward, which carries no remote command",
    "plugins/module_utils/ssh_key_policy.py": "ssh -G evaluates the policy and opens no session",
    "plugins/module_utils/ceph_host_route_probe.py": "checks the client argv cephadm runs and builds none",
}

# Each file that hands an ssh session a remote command, which it joins through the quoting owner.
REMOTE_COMMAND_SITES = {
    "plugins/module_utils/ceph_core.py",
    "plugins/module_utils/managedos_guest_ssh.py",
}


QUOTING_MODULES = ("shlex", "pipes")
QUOTING_MEMBERS = ("join", "quote")
DYNAMIC_LOOKUPS = ("getattr", "import_module", "__import__")


def quotes_shell(tree):
    """Any reach for shlex.join, shlex.quote or pipes.quote: an attribute of the module under any alias, a from-import of
    a member or *, or a getattr or import_module lookup naming the module."""
    names = set()
    for module in QUOTING_MODULES:
        names |= names_of(tree, module)[0]
    found = []
    for node in ast.walk(tree):
        if isinstance(node, ast.Attribute) and isinstance(node.value, ast.Name) and node.value.id in names \
                and node.attr in QUOTING_MEMBERS:
            found.append(node)
        elif isinstance(node, ast.ImportFrom) and node.module in QUOTING_MODULES \
                and any(alias.name in QUOTING_MEMBERS or alias.name == "*" for alias in node.names):
            found.append(node)
        elif isinstance(node, ast.Call) and lookup_name(node.func) in DYNAMIC_LOOKUPS and any(
                (isinstance(arg, ast.Constant) and arg.value in QUOTING_MODULES) or (isinstance(arg, ast.Name) and arg.id in names)
                for arg in node.args):
            found.append(node)
    return found


def lookup_name(function):
    if isinstance(function, ast.Name):
        return function.id
    if isinstance(function, ast.Attribute):
        return function.attr
    return None


def names_ssh_client(tree):
    return [node for node in ast.walk(tree) if isinstance(node, ast.Constant) and node.value == SSH_CLIENT]


def joins_remote_command(tree):
    """A call of the quoting owner's remote_command or fixed_worker, through the module or its members."""
    modules, members = set(), {}
    for node in ast.walk(tree):
        if isinstance(node, ast.ImportFrom) and node.module == "ansible_collections.bootwright.core.plugins.module_utils":
            modules.update((alias.asname or alias.name) for alias in node.names if alias.name == "remote_command")
        elif isinstance(node, ast.ImportFrom) and node.module == "ansible_collections.bootwright.core.plugins.module_utils.remote_command":
            members.update({(alias.asname or alias.name): alias.name for alias in node.names})
    found = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        function = node.func
        if isinstance(function, ast.Attribute) and isinstance(function.value, ast.Name) and function.value.id in modules \
                and function.attr in ("remote_command", "fixed_worker"):
            found.append(node)
        elif isinstance(function, ast.Name) and members.get(function.id) in ("remote_command", "fixed_worker"):
            found.append(node)
    return found


def starts_a_shell(tree):
    """A call passing shell= anything but the literal False, or a ** mapping into a process start or naming shell."""
    process = {id(node) for node in starts_process(tree)}
    return [node for node in ast.walk(tree) if isinstance(node, ast.Call) and any(
        (keyword.arg == "shell" and not (isinstance(keyword.value, ast.Constant) and keyword.value.value is False))
        or (keyword.arg is None and (id(node) in process or names_shell(keyword.value)))
        for keyword in node.keywords)]


def names_shell(mapping):
    """A ** operand that spells a shell key: a dict display, or a dict call, holding it."""
    if isinstance(mapping, ast.Dict):
        return any(isinstance(key, ast.Constant) and key.value == "shell" for key in mapping.keys)
    return isinstance(mapping, ast.Call) and any(keyword.arg == "shell" for keyword in mapping.keywords)


def matching(finder):
    """The relative path of each plugin file the finder matches, which may be none."""
    return {path.relative_to(COLLECTION).as_posix() for path in sorted((COLLECTION / "plugins").rglob("*.py"))
            if finder(ast.parse(path.read_text()))}


def test_shell_commands_are_quoted_in_one_module():
    check(scan(quotes_shell), QUOTING_OWNER, {}, "quotes a shell command", "join it through module_utils/remote_command.py")


def test_no_plugin_starts_a_shell():
    assert matching(starts_a_shell) == set(), "a plugin passes shell=; start an argv list through module_utils/bounded_process.py"


def test_every_ssh_site_is_listed_with_its_reason():
    found = matching(names_ssh_client)
    for path in sorted(found - set(SSH_SITES)):
        raise AssertionError("%s names %s; join any remote command through module_utils/remote_command.py and list the site" % (path, SSH_CLIENT))
    for path, reason in SSH_SITES.items():
        assert reason and path in found, "%s no longer names %s; remove its entry" % (path, SSH_CLIENT)


def test_every_remote_command_is_joined_by_the_quoting_owner():
    assert matching(joins_remote_command) == REMOTE_COMMAND_SITES


def test_the_quoting_finders_see_aliases_and_nothing_in_strings():
    quoted = ast.parse("import shlex as s, pipes\ns.join(['a'])\nfrom shlex import quote as q\nq('a')\npipes.quote('a')\n")
    assert len(quotes_shell(quoted)) == 3
    for bypass in ("import shlex\nj = shlex.join\nj(['a'])\n", "import shlex\ngetattr(shlex, 'quote')('a')\n",
                   "from shlex import *\n", "from shlex import join\n", "from pipes import quote\n",
                   "import importlib\nimportlib.import_module('shlex').join(['a'])\n", "__import__('pipes').quote('a')\n"):
        assert quotes_shell(ast.parse(bypass)), bypass
    assert quotes_shell(ast.parse("import shlex\nshlex.split('a b')\nfrom shlex import split\nSOURCE = 'shlex.join([])'\n")) == []
    shells = ast.parse("import subprocess\nsubprocess.run('x', shell=True)\nf(shell=flag)\nsubprocess.run(['/x'], shell=False)\n")
    assert len(starts_a_shell(shells)) == 2
    splatted = ast.parse("import subprocess\nsubprocess.run('x', **{'shell': True})\nsubprocess.Popen(['x'], **options)\n"
                         "g(**{'shell': 1})\ng(**dict(shell=1))\nf(**options)\nf(a=1)\n")
    assert len(starts_a_shell(splatted)) == 5
    joined = ast.parse("from ansible_collections.bootwright.core.plugins.module_utils import remote_command as r\nr.fixed_worker('x')\n"
                       "from ansible_collections.bootwright.core.plugins.module_utils.remote_command import remote_command as j\nj(['x'])\n")
    assert len(joins_remote_command(joined)) == 2
