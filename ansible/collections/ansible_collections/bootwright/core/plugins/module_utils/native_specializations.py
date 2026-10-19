"""Named deviations from a component's general flow, chosen from facts of proved artifacts."""

from __future__ import annotations

import ast
import io
import zipfile
from collections import namedtuple

Seam = namedtuple("Seam", "general variants refusal")
Specialization = namedtuple("Specialization", "id seam variant when")
MAX_MEMBER = 4 << 20
PARSER_ANCHOR = frozenset(("bootstrap", "check-host", "deploy", "--image", "--fsid"))
OVERRIDES = "cephadmlib/override_vars.py"
LICENSE_CALL = ("orch", "accept-license", "--image")


class Absent(Exception):
    """The proved archive has no such member."""


class Unknown(Exception):
    """The detector cannot witness the feature either way."""


def member(data, name):
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        try:
            info = archive.getinfo(name)
        except KeyError:
            raise Absent(name) from None
        if info.file_size > MAX_MEMBER:
            raise Unknown(name)
        return archive.read(info)


def declared(data):
    """Every literal option or subcommand of the zipapp's argparse calls; never executed; refused unless anchored."""
    names = set()
    for node in ast.walk(ast.parse(member(data, "__main__.py"))):
        if isinstance(node, ast.Call) and getattr(node.func, "attr", None) in ("add_argument", "add_parser"):
            names.update(a.value for a in node.args if isinstance(a, ast.Constant) and isinstance(a.value, str))
    if not PARSER_ANCHOR <= names:
        raise Unknown("parser")
    return frozenset(names)


def ibm_build(data, names):
    """True for a literal IBM_BUILD = True, False only when the override module is absent, else unknown."""
    try:
        tree = ast.parse(member(data, OVERRIDES))
    except Absent:
        return False
    for node in tree.body:
        if isinstance(node, ast.Assign) and [getattr(t, "id", None) for t in node.targets] == ["IBM_BUILD"]:
            value = node.value
        elif isinstance(node, ast.AnnAssign) and getattr(node.target, "id", None) == "IBM_BUILD":
            value = node.value
        else:
            continue
        if isinstance(value, ast.Constant) and value.value is True:
            return True
        raise Unknown("IBM_BUILD")
    raise Unknown("IBM_BUILD")


def bootstrap_license_call(data, names):
    """True when the IBM build's bootstrap lists ``orch accept-license --image``; False when the anchored module lists no such call."""
    for node in ast.walk(ast.parse(member(data, "__main__.py"))):
        if isinstance(node, ast.List) and tuple(e.value if isinstance(e, ast.Constant) else None for e in node.elts[:3]) == LICENSE_CALL:
            return ibm_build(data, names)
    return False


FACTS = {
    "host-cephadm.license-flag": ("host-cephadm", lambda data, names: "--automatically-accept-license" in names),
    "host-cephadm.call-home-opt-out": ("host-cephadm", lambda data, names: "--disable-ibm-call-home" in names),
    "image-cephadm.ibm-build": ("image-cephadm", ibm_build),
    "image-cephadm.ssh-user-setup": ("image-cephadm", lambda data, names: "setup-ssh-user" in names),
    "host-cephadm.bootstrap-license-call": ("host-cephadm", bootstrap_license_call),
}

SEAMS = {
    "ceph.bootstrap.license-flag": Seam("pass", frozenset(("pass", "omit")), False),
    "ceph.mgr.license-contract": Seam("ibm", frozenset(("ibm", "absent")), False),
    "ceph.call-home-model": Seam("opt-out", frozenset(("opt-out", "opt-in")), True),
    "ceph.mgr.set-user-pre-steps": Seam("skip", frozenset(("skip", "absent")), False),
    "ceph.bootstrap.license-call-refusal": Seam("fail", frozenset(("fail", "continue-seed")), False),
}

SPECIALIZATIONS = (
    Specialization(
        id="ceph-bootstrap-without-license-flag",
        seam="ceph.bootstrap.license-flag",
        variant="omit",
        when={"host-cephadm.license-flag": False},
    ),
    Specialization(
        id="ceph-mgr-without-license-contract",
        seam="ceph.mgr.license-contract",
        variant="absent",
        when={"image-cephadm.ibm-build": False},
    ),
    Specialization(
        id="ceph-call-home-opt-in",
        seam="ceph.call-home-model",
        variant="opt-in",
        when={"host-cephadm.call-home-opt-out": False, "image-cephadm.ibm-build": False},
    ),
    Specialization(
        id="ceph-set-user-without-pre-steps",
        seam="ceph.mgr.set-user-pre-steps",
        variant="absent",
        when={"image-cephadm.ssh-user-setup": False},
    ),
    Specialization(
        id="ceph-bootstrap-license-call-continues",
        seam="ceph.bootstrap.license-call-refusal",
        variant="continue-seed",
        when={"host-cephadm.bootstrap-license-call": True},
    ),
)


class Selection:
    """One worker run's seam answers over its proved sources, each read once."""

    def __init__(self, sources):
        self.sources, self.data, self.names, self.facts, self.applied = sources, {}, {}, {}, set()

    def source(self, name):
        if name not in self.data:
            self.data[name] = self.sources[name]()  # proved bytes, or the source's refusal propagates
            try:
                self.names[name] = declared(self.data[name])
            except Exception:
                self.names[name] = None
        return self.data[name], self.names[name]

    def fact(self, name):
        if name not in self.facts:
            source, detect = FACTS[name]
            data, names = self.source(source)
            try:
                self.facts[name] = None if names is None else detect(data, names)
            except Exception:
                self.facts[name] = None
        return self.facts[name]

    def declared(self, source):
        return self.source(source)[1]

    def variant(self, seam):
        matches = [s for s in SPECIALIZATIONS if s.seam == seam and all(self.fact(k) is v for k, v in s.when.items())]
        if len(matches) > 1:
            raise AssertionError(seam)
        if not matches:
            return SEAMS[seam].general
        self.applied.add(matches[0].id)
        return matches[0].variant
