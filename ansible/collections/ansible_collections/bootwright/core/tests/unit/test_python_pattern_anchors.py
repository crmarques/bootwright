"""A Python plugin pattern that validates a whole value ends at the end of the value.

re.match and re.search treat a bare `$` as also matching just before a trailing
newline, so a validator such as `NAME.match(value)` with a pattern ending in `$`
admits "lab\\n". The exact end is `\\Z`, or re.fullmatch. This scan reads every
Python file under plugins/ and fails on a match or search whose pattern, a
literal or a constant the same file defines, ends in a bare `$` and either is
matched from the start or begins with an anchor. Multi-line patterns, the
suffix searches that carry no start anchor, and a `$` that closes only an
alternative of a longer pattern are not whole-value validators and pass. The
role tasks have the same scan in test_role_pattern_anchors.py.
"""

from __future__ import annotations

import ast
import pathlib
import re

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_access, redfish_discovery
from ansible_collections.bootwright.core.plugins.modules import containercluster_install_inspect as install_inspect

PLUGINS = pathlib.Path(__file__).resolve().parents[2] / "plugins"
METHODS = ("match", "search")
MULTILINE = re.compile(r"\(\?[aiLsux]*m")


def literal(node, constants):
    """The string a pattern expression statically holds, or None when it depends on a run."""
    if isinstance(node, ast.Constant) and isinstance(node.value, (str, bytes)):
        return node.value.decode("latin-1") if isinstance(node.value, bytes) else node.value
    if isinstance(node, ast.Name):
        return constants.get(node.id)
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.Add):
        left, right = literal(node.left, constants), literal(node.right, constants)
        return None if left is None or right is None else left + right
    return None


def ends_with_bare_dollar(pattern):
    """Whether the last token of the pattern is an unescaped `$` outside a character class."""
    index, in_class, last = 0, False, None
    while index < len(pattern):
        character = pattern[index]
        if character == "\\":
            last, index = None, index + 2
            continue
        if in_class:
            in_class, last = character != "]", None
        elif character == "[":
            in_class, last = True, None
            index += 1
            index += pattern[index:index + 1] == "^"
            index += pattern[index:index + 1] == "]"
            continue
        else:
            last = index if character == "$" else None
        index += 1
    return last is not None


def flags_multiline(arguments):
    return any(isinstance(node, ast.Attribute) and node.attr in ("M", "MULTILINE") for argument in arguments for node in ast.walk(argument))


def whole_value_patterns(source):
    """(line, method, pattern) of every match or search whose pattern could be a whole-value validator."""
    tree = ast.parse(source)
    constants = {}
    for node in ast.walk(tree):
        if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name):
            value = literal(node.value, constants)
            if value is not None:
                constants[node.targets[0].id] = value
    compiled = {}
    for node in ast.walk(tree):
        if isinstance(node, (ast.Assign, ast.AnnAssign)) and isinstance(node.value, ast.Call):
            call = node.value
            if ast.unparse(call.func) == "re.compile" and call.args:
                pattern = literal(call.args[0], constants)
                targets = node.targets if isinstance(node, ast.Assign) else [node.target]
                if pattern is not None:
                    for target in targets:
                        compiled.setdefault(ast.unparse(target), []).append((pattern, flags_multiline(call.args[1:] + [k.value for k in call.keywords])))
    found = []
    for node in ast.walk(tree):
        if not (isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and node.func.attr in METHODS and node.args):
            continue
        owner, candidates = node.func.value, []
        if ast.unparse(owner) == "re":
            pattern = literal(node.args[0], constants)
            if pattern is not None:
                candidates.append((pattern, flags_multiline(node.args[2:] + [k.value for k in node.keywords])))
        elif isinstance(owner, ast.Call) and ast.unparse(owner.func) == "re.compile" and owner.args:
            pattern = literal(owner.args[0], constants)
            if pattern is not None:
                candidates.append((pattern, flags_multiline(owner.args[1:])))
        else:
            candidates += compiled.get(ast.unparse(owner), [])
        for pattern, multiline in candidates:
            if multiline or MULTILINE.search(pattern) or not ends_with_bare_dollar(pattern):
                continue
            if node.func.attr == "match" or pattern.startswith(("^", "\\A")):
                found.append((node.lineno, node.func.attr, pattern))
    return found


def plugin_files():
    return sorted(PLUGINS.rglob("*.py"))


@pytest.mark.parametrize("path", plugin_files(), ids=lambda path: str(path.relative_to(PLUGINS)))
def test_a_plugin_pattern_ends_at_the_end_of_the_value(path):
    assert whole_value_patterns(path.read_text()) == [], "end a whole-value pattern with \\Z (or use re.fullmatch), not a bare $"


@pytest.mark.parametrize("source, refused", [
    ('import re\nNAME = re.compile(r"^[a-z]+$")\nNAME.match(x)\n', True),
    ('import re\nre.match(r"^[a-z]+$", x)\n', True),
    ('import re\nre.search(r"^[a-z]+$", x)\n', True),
    ('import re\nre.compile(r"[a-z]+$").match(x)\n', True),
    ('import re\nPART = r"[a-z]+"\nNAME = re.compile(r"^" + PART + "$")\nNAME.match(x)\n', True),
    ('import re\nNAME = re.compile(r"^[a-z]+\\\\\\\\$")\nNAME.match(x)\n', True),
    ('import re\nNAME = re.compile(r"^[a-z]+\\Z")\nNAME.match(x)\n', False),
    ('import re\nNAME = re.compile(r"^[a-z]+\\$")\nNAME.match(x)\n', False),
    ('import re\nNAME = re.compile(r"^[a-z$]+")\nNAME.match(x)\n', False),
    ('import re\nNAME = re.compile(r"^[a-z]+$(?!\\n)")\nNAME.match(x)\n', False),
    ('import re\nre.fullmatch(r"^[a-z]+$", x)\n', False),
    ('import re\nre.search(r"/(cd|dvd)$", x)\n', False),
    ('import re\nre.search(r"^[a-z]+$", x, re.M)\n', False),
    ('import re\nre.match(r"(?m)^[a-z]+$", x)\n', False),
    ('import re\nre.match(r"([a-z]+)(?:,|$)", x)\n', False),
])
def test_the_scan_refuses_a_bare_end_anchor_however_it_is_written(source, refused):
    assert bool(whole_value_patterns(source)) is refused, source


def test_the_scan_reads_the_plugins():
    assert len(plugin_files()) > 100
    examined = sum(len(re.findall(r"re\.(?:compile|match|search)\(", path.read_text())) for path in plugin_files())
    assert examined > 100, "the scan no longer finds the plugins' patterns"


def test_the_kubevirt_lend_secret_name_refuses_a_trailing_newline():
    request = {"version": kubevirt_access.VERSION, "expirationSeconds": 1800, "secret": "bootwright-kubevirt-lend-0123456789abcdef",
               "sweep": True}
    assert kubevirt_access.validated(request)[1] == request["secret"]
    with pytest.raises(kubevirt_access.Refused):
        kubevirt_access.validated(dict(request, secret=request["secret"] + "\n"))


@pytest.mark.parametrize("value, admitted", [("aa:bb:cc:dd:ee:0f", True), ("aa:bb:cc:dd:ee:0f\n", False), ("aa:bb:cc:dd:ee", False)])
def test_a_redfish_hardware_address_refuses_a_trailing_newline(value, admitted):
    assert (redfish_discovery.MAC.match(value) is not None) is admitted


@pytest.mark.parametrize("value, admitted", [("kind: Config", True), ("clusters:", True), ("kind: Config\n", False), ("clusters:\n", False)])
def test_an_installer_file_entry_refuses_a_trailing_newline(value, admitted):
    assert (install_inspect.ENTRY.match(value) is not None) is admitted
