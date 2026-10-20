"""Named deviations take a closed variant only from witnessed facts of proved cephadm bytes."""

from __future__ import annotations

import ast
import io
import itertools
import json
import os
import pathlib
import re
import zipfile

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import ceph_tunnel_staging, native_specializations as ns

DATA = pathlib.Path(__file__).resolve().parent / "data" / "cephadm_declared"
PLUGINS = pathlib.Path(ns.__file__).resolve().parent
ANCHOR = ("bootstrap", "check-host", "deploy", "--image", "--fsid")
FEATURES = ("--automatically-accept-license", "--disable-ibm-call-home", "setup-ssh-user")
IBM_OVERRIDE = "IBM_BUILD = True\n"
OVERRIDE_FORMS = {"assign True": IBM_OVERRIDE}
BUILDS = {"20.1.0-221.el9cp": False, "20.2.1-405.el9cp": True, "20.2.2-349.el9cp": True, "20.2.2-349.el10cp": True}
GENERAL = {"ceph.bootstrap.license-flag": "pass", "ceph.mgr.license-contract": "ibm", "ceph.call-home-model": "opt-out",
           "ceph.mgr.set-user-pre-steps": "skip", "ceph.bootstrap.license-call-refusal": "fail"}
SPECIALIZED = {"ceph.bootstrap.license-flag": "omit", "ceph.mgr.license-contract": "absent", "ceph.call-home-model": "opt-in",
               "ceph.mgr.set-user-pre-steps": "absent", "ceph.bootstrap.license-call-refusal": "fail"}
LICENSE_CALL_ID = "ceph-bootstrap-license-call-continues"
CURRENT_VARIANTS = dict(GENERAL, **{"ceph.bootstrap.license-call-refusal": "continue-seed"})
# The IBM bootstrap's own call as both signed 20.2.x zipapps list it (__main__.py:3445-3447 in 20.2.1-405, :3534-3535 in 20.2.2-349).
LICENSE_CALL_SOURCE = "def bootstrap(ctx, cli):\n    if ctx.ibm_build:\n        cli(['orch', 'accept-license', '--image', ctx.image])\n"
SEAM_CALLERS = frozenset(("ceph_native.py", "ceph_native_resources.py", "ceph_tunnel_staging.py"))
SEAMLESS = ("ceph_tunnel.py", "ceph_tunnel_custody.py", "ceph_pull_broker.py", "ceph_host_route_probe.py", "ceph_crypto_policy.py",
            "native_signatures.py", "native_diagnostics.py")
UNCONSULTED = frozenset()
PROOF = re.compile(r"(prove_|verify_|validate)")
ID = re.compile(r"[a-z0-9]+(-[a-z0-9]+){1,7}\Z")


def parser_source(names):
    lines = ["import argparse", "parser = argparse.ArgumentParser()", "commands = parser.add_subparsers()"]
    for name in names:
        call = "parser.add_argument" if name.startswith("-") else "commands.add_parser"
        lines.append("%s(%r)" % (call, name))
    return "\n".join(lines) + "\n"


def licensed(names):
    return parser_source(names) + LICENSE_CALL_SOURCE


def zipapp(names=(), override=None, main=None, members=None):
    buffer = io.BytesIO()
    with zipfile.ZipFile(buffer, "w", zipfile.ZIP_DEFLATED) as archive:
        if main is not False:
            archive.writestr("__main__.py", parser_source(names) if main is None else main)
        if override is not None:
            archive.writestr(ns.OVERRIDES, override)
        for name, text in (members or {}).items():
            archive.writestr(name, text)
    return buffer.getvalue()


def old_shaped():
    return zipapp(ANCHOR)


def new_shaped():
    return zipapp(ANCHOR + FEATURES, IBM_OVERRIDE, main=licensed(ANCHOR + FEATURES))


def selection(host, image=None):
    return ns.Selection({"host-cephadm": lambda: host, "image-cephadm": lambda: host if image is None else image})


def facts(value):
    return {name: value.fact(name) for name in ns.FACTS}


def variants(value):
    return {seam: value.variant(seam) for seam in ns.SEAMS}


def committed(build):
    with open(DATA / (build + ".json"), encoding="utf-8") as handle:
        return json.load(handle)


def test_twenty_one_shaped_artifacts_take_every_specialized_variant():
    chosen = selection(old_shaped())
    assert facts(chosen) == dict.fromkeys(ns.FACTS, False)
    assert variants(chosen) == SPECIALIZED
    assert chosen.applied == {record.id for record in ns.SPECIALIZATIONS} - {LICENSE_CALL_ID}


def test_twenty_two_shaped_artifacts_keep_the_general_flow_but_continue_the_license_call_refusal():
    chosen = selection(new_shaped())
    assert facts(chosen) == dict.fromkeys(ns.FACTS, True)
    assert variants(chosen) == CURRENT_VARIANTS
    assert chosen.applied == {LICENSE_CALL_ID}


def test_call_home_opt_in_needs_both_artifacts_to_agree():
    assert selection(old_shaped(), new_shaped()).variant("ceph.call-home-model") == "opt-out"
    assert selection(new_shaped(), old_shaped()).variant("ceph.call-home-model") == "opt-out"


def oversized(name):
    members = {name: "#" * (ns.MAX_MEMBER + 1)}
    if name == "__main__.py":
        return zipapp(main=False, members=members)
    return zipapp(ANCHOR, members=members)


UNREADABLE = {
    "random": lambda: os.urandom(4096),
    "truncated": lambda: new_shaped()[:200],
    "oversized-main": lambda: oversized("__main__.py"),
    "no-main": lambda: zipapp(main=False, override=IBM_OVERRIDE),
    "main-without-parser": lambda: zipapp(main="print('cephadm')\n", override=IBM_OVERRIDE),
    "parser-in-another-member": lambda: zipapp(main="from cephadmlib import cli\n", override=IBM_OVERRIDE,
                                               members={"cephadmlib/cli.py": parser_source(ANCHOR + FEATURES)}),
    "unparsable-main": lambda: zipapp(main="def (\n"),
    "partial-anchor": lambda: zipapp(ANCHOR[:-1]),
}


@pytest.mark.parametrize("shape", sorted(UNREADABLE))
def test_unreadable_or_unanchored_artifacts_are_unknown_and_general(shape):
    data = UNREADABLE[shape]()
    chosen = selection(data)
    assert chosen.declared("host-cephadm") is None
    assert facts(chosen) == dict.fromkeys(ns.FACTS)
    assert variants(chosen) == GENERAL
    assert chosen.applied == set()


@pytest.mark.parametrize("text", ["IBM_BUILD = True\n", "IBM_BUILD: bool = True\n", "OTHER = 1\nIBM_BUILD = True\n"])
def test_ibm_build_is_true_only_for_a_literal_true(text):
    assert ns.ibm_build(zipapp(ANCHOR, text), None) is True
    assert selection(zipapp(ANCHOR, text)).fact("image-cephadm.ibm-build") is True


def test_ibm_build_is_false_only_without_the_override_module():
    assert ns.ibm_build(zipapp(ANCHOR), None) is False


@pytest.mark.parametrize("text", ["IBM_BUILD = False\n", "IBM_BUILD = bool(1)\n", "if True:\n    IBM_BUILD = True\n",
                                  "OTHER = True\n", "", "IBM_BUILD = 1\n", "IBM_BUILD = BUILD = True\n", "def (\n"])
def test_unrecognized_ibm_build_forms_are_unknown(text):
    data = zipapp(ANCHOR, text)
    with pytest.raises((ns.Unknown, SyntaxError)):
        ns.ibm_build(data, None)
    assert selection(data).fact("image-cephadm.ibm-build") is None


def test_oversized_override_module_is_unknown():
    data = oversized(ns.OVERRIDES)
    with pytest.raises(ns.Unknown):
        ns.ibm_build(data, None)
    assert selection(data).fact("image-cephadm.ibm-build") is None


def test_member_bounds_and_names_absence():
    with pytest.raises(ns.Absent):
        ns.member(zipapp(ANCHOR), ns.OVERRIDES)
    with pytest.raises(ns.Unknown):
        ns.member(oversized(ns.OVERRIDES), ns.OVERRIDES)
    assert ns.declared(zipapp(ANCHOR)) == frozenset(ANCHOR)


@pytest.mark.parametrize("build", sorted(BUILDS))
def test_committed_name_lists_are_strings_only_with_their_provenance(build):
    value = committed(build)
    assert set(value) == {"bootstrapLicenseCallLine", "build", "declared", "overrideVars", "rpm", "zipappSHA256"} and value["build"] == build
    line = value["bootstrapLicenseCallLine"]
    assert (line is None) is (not BUILDS[build]) and (line is None or re.fullmatch(r"__main__\.py:[1-9][0-9]*", line))
    assert value["declared"] == sorted(set(value["declared"])) and all(isinstance(name, str) for name in value["declared"])
    assert value["rpm"]["url"].startswith("https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/")
    assert value["rpm"]["url"].endswith("/cephadm-" + build + ".noarch.rpm")
    assert all(re.fullmatch(r"[0-9a-f]{64}", digest) for digest in (value["rpm"]["sha256"], value["zipappSHA256"]))
    assert ns.PARSER_ANCHOR <= set(value["declared"])


def synthesized(build):
    value = committed(build)
    main = parser_source(value["declared"]) if value["bootstrapLicenseCallLine"] is None else licensed(value["declared"])
    override = value["overrideVars"]
    if not override["present"]:
        return zipapp(value["declared"], main=main)
    forms = override["ibmBuild"]
    assert len(forms) == 1, "a new IBM_BUILD form needs its own synthesis and a reviewed detector expectation"
    return zipapp(value["declared"], OVERRIDE_FORMS[forms[0]], main=main)


@pytest.mark.parametrize("build", sorted(BUILDS))
def test_each_signed_build_list_gives_its_recorded_facts(build):
    chosen = selection(synthesized(build))
    assert facts(chosen) == dict.fromkeys(ns.FACTS, BUILDS[build])
    assert variants(chosen) == (CURRENT_VARIANTS if BUILDS[build] else SPECIALIZED)


@pytest.mark.parametrize("build", sorted(build for build, current in BUILDS.items() if current))
def test_todays_bootstrap_options_are_declared_by_each_twenty_two_build(build):
    request = {"version": "ceph-core-v24", "clusterSSHTransport": "verified-tunnel", "owner": "apply-example",
               "fsid": "4c0a4300-aa43-458c-86d7-ac2256d1fc00", "image": "registry.example.test/ceph@sha256:" + "a" * 64,
               "cephadm": {"path": "/usr/sbin/cephadm", "packageVersion": build}, "bootstrap": "node-1.example.test",
               "monIP": "192.0.2.1", "clusterNetworks": ["198.51.100.0/24"], "callHome": "enabled",
               "hosts": [{"name": "node-1.example.test", "machine": "machine-1", "address": "192.0.2.1", "port": 22}]}
    scratch = ceph_tunnel_staging.ROOT + "/" + request["fsid"] + "/.bootstrap-fixture"
    argv = ceph_tunnel_staging.bootstrap_argv(request, scratch, selection(synthesized(build)))
    options = {token for token in argv if token.startswith("--")}
    assert "--cluster-network" in options and "bootstrap" in argv
    assert options <= set(committed(build)["declared"])


LICENSE_FACT, LICENSE_SEAM = "host-cephadm.bootstrap-license-call", "ceph.bootstrap.license-call-refusal"


def license_choice(main, override=IBM_OVERRIDE):
    chosen = selection(zipapp(ANCHOR, override, main=main))
    return chosen.fact(LICENSE_FACT), chosen.variant(LICENSE_SEAM), chosen.applied


def test_bootstrap_license_call_true_on_an_ibm_build_that_lists_it():
    assert license_choice(licensed(ANCHOR)) == (True, "continue-seed", {LICENSE_CALL_ID})


def test_bootstrap_license_call_false_without_the_literal():
    text = licensed(ANCHOR).replace("cli(['orch', 'accept-license', '--image', ctx.image])", "logger.info('orch accept-license --image')")
    assert "accept-license" in text
    assert license_choice(text) == (False, "fail", set())


def test_bootstrap_license_call_false_without_ibm_overrides():
    assert license_choice(licensed(ANCHOR), override=None) == (False, "fail", set())


@pytest.mark.parametrize("override", ["IBM_BUILD = False\n", "IBM_BUILD = bool(1)\n"])
def test_bootstrap_license_call_unknown_on_unrecognized_overrides(override):
    assert license_choice(licensed(ANCHOR), override=override) == (None, "fail", set())


def test_bootstrap_license_call_unknown_when_unanchored():
    assert license_choice(licensed(ANCHOR[:-1])) == (None, "fail", set())


@pytest.mark.parametrize("call", ["['orch', 'accept-license']", "['orch', 'accept-license', '--license']", "[ctx.orch, 'accept-license', '--image']"])
def test_bootstrap_license_call_needs_the_image_argument(call):
    text = licensed(ANCHOR).replace("['orch', 'accept-license', '--image', ctx.image]", call)
    assert license_choice(text) == (False, "fail", set())


def counted():
    calls = {"host-cephadm": 0, "image-cephadm": 0}

    def source(name):
        def read():
            calls[name] += 1
            return old_shaped()
        return read

    return calls, ns.Selection({name: source(name) for name in calls})


def test_each_source_is_read_once_however_many_seams_are_consulted():
    calls, chosen = counted()
    for unused in range(3):
        variants(chosen)
        chosen.declared("host-cephadm")
    assert calls == {"host-cephadm": 1, "image-cephadm": 1}
    assert chosen.declared("host-cephadm") == frozenset(ANCHOR)


class SourceRefusal(ValueError):
    """A proving source refused; never masked as unknown."""


def test_a_source_refusal_propagates_unchanged():
    def refuse():
        raise SourceRefusal("runtime-profile")

    chosen = ns.Selection({"host-cephadm": old_shaped, "image-cephadm": refuse})
    assert chosen.variant("ceph.bootstrap.license-flag") == "omit"
    with pytest.raises(SourceRefusal, match="^runtime-profile$"):
        chosen.variant("ceph.mgr.license-contract")
    assert chosen.applied == {"ceph-bootstrap-without-license-flag"}


def test_two_matching_records_are_a_defect_not_a_choice(monkeypatch):
    record = ns.SPECIALIZATIONS[0]
    monkeypatch.setattr(ns, "SPECIALIZATIONS", ns.SPECIALIZATIONS + (record._replace(id=record.id + "-copy"),))
    with pytest.raises(AssertionError):
        selection(old_shaped()).variant(record.seam)


def test_registry_records_are_well_formed():
    ids = [record.id for record in ns.SPECIALIZATIONS]
    assert len(ids) == len(set(ids))
    for record in ns.SPECIALIZATIONS:
        assert ID.match(record.id) and len(record.id.encode()) <= 64
        seam = ns.SEAMS[record.seam]
        assert record.variant in seam.variants and record.variant != seam.general
        assert record.when and all(name in ns.FACTS and isinstance(value, bool) for name, value in record.when.items())
    for name, seam in ns.SEAMS.items():
        assert seam.general in seam.variants and isinstance(seam.refusal, bool), name
    assert {name: seam.general for name, seam in ns.SEAMS.items()} == GENERAL


def test_no_two_records_match_any_assignment_of_a_seams_facts():
    for seam in ns.SEAMS:
        records = [record for record in ns.SPECIALIZATIONS if record.seam == seam]
        names = sorted({name for record in records for name in record.when})
        for values in itertools.product((True, False, None), repeat=len(names)):
            assignment = dict(zip(names, values))
            matching = [r for r in records if all(assignment[k] is v for k, v in r.when.items())]
            assert len(matching) <= 1, (seam, assignment)


class Unknowing(ns.Selection):
    def fact(self, name):
        return None


def test_every_fact_unknown_gives_every_general_variant():
    chosen = Unknowing({})
    assert variants(chosen) == GENERAL and chosen.applied == set()


def test_refusal_seams_are_pinned():
    assert {name for name, seam in ns.SEAMS.items() if seam.refusal} == {"ceph.call-home-model"}


def test_facts_never_key_on_a_release_tag_or_label():
    for name, (source, unused) in ns.FACTS.items():
        assert source in ("host-cephadm", "image-cephadm") and name.startswith(source + ".")
        assert not re.search(r"release|tag|label|version", name), name


def test_module_is_closed_short_and_has_no_contract_literal():
    source = pathlib.Path(ns.__file__).read_text(encoding="utf-8")
    assert max(len(line) for line in source.splitlines()) <= 160
    assert not re.search(r"-v[0-9]", source)
    imported = set()
    for node in ast.walk(ast.parse(source)):
        if isinstance(node, ast.Import):
            imported.update(alias.name for alias in node.names)
        elif isinstance(node, ast.ImportFrom):
            imported.add(node.module)
    assert imported == {"__future__", "ast", "io", "zipfile", "collections"}


def seam_calls(source):
    """Each .variant( or .declared( call with its enclosing function and literal argument."""
    found = []

    def walk(node, function):
        for child in ast.iter_child_nodes(node):
            inner = child.name if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef)) else function
            if isinstance(child, ast.Call) and isinstance(child.func, ast.Attribute) and child.func.attr in ("variant", "declared"):
                first = child.args[0] if child.args else None
                found.append((child.func.attr, inner, first.value if isinstance(first, ast.Constant) else None))
            walk(child, inner)

    walk(ast.parse(source), None)
    return found


def test_seam_scanner_sees_calls_and_their_functions():
    text = "def prove_key(s):\n    s.selection.variant('ceph.call-home-model')\nx = sel.declared('host-cephadm')\n"
    assert seam_calls(text) == [("variant", "prove_key", "ceph.call-home-model"), ("declared", None, "host-cephadm")]


def test_seam_calls_stay_in_allowed_files_and_out_of_proofs():
    consulted = set()
    for path in sorted(PLUGINS.glob("*.py")):
        if path.name == "native_specializations.py":
            continue
        calls = seam_calls(path.read_text(encoding="utf-8"))
        if path.name not in SEAM_CALLERS:
            assert not calls, path.name
            continue
        for kind, function, argument in calls:
            assert function is None or not PROOF.match(function), (path.name, function)
            if kind == "variant":
                assert argument in ns.SEAMS, (path.name, argument)
                consulted.add(argument)
            else:
                assert argument in ("host-cephadm", "image-cephadm"), (path.name, argument)
    assert set(SEAMLESS) <= {path.name for path in PLUGINS.glob("*.py")} and not set(SEAMLESS) & SEAM_CALLERS
    assert set(ns.SEAMS) - consulted == UNCONSULTED
