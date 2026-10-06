"""A request string a runner writes reaches a module verbatim, never rendered.

ansible-core loads an --extra-vars @file document as trusted template text, so
a request string holding Jinja would run on the controller as the adapter's
user. Both runners therefore write every string of that document as an
__ansible_unsafe object (internal/reconciliation/ansiblerunner/
process_linux_amd64.go and internal/controller/ansiblelocal/
runner_linux_amd64.go), which ansible-core reads as an untrusted string. The
fixtures under goldens/extra_vars are exactly what each runner writes for a
request holding every delimiter, and a Go test beside each runner holds it to
them. These cases run ansible-playbook over each fixture: a role whose argument
spec declares the request validates it, then hands request strings to a module
whole, through filters and through a second validated role, and the module
records what it was given. Every string arrives verbatim, and the same fixture
with its marking removed renders, which proves the probe sees a rendered string
when there is one.

Running a playbook needs Ansible's controller, which ansible-test does not
offer to unit tests under tests/unit/plugins, so these checks live here.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys

import pytest

COLLECTIONS = pathlib.Path(__file__).resolve().parents[5]
GOLDENS = pathlib.Path(__file__).resolve().parent / "goldens" / "extra_vars"
ECHOED = """#!/usr/bin/python
from __future__ import annotations
import json
from ansible.module_utils.basic import AnsibleModule
module = AnsibleModule(argument_spec={"text": {"type": "str", "required": True}, "path": {"type": "path", "required": True}})
with open(module.params["path"], "a", encoding="utf-8") as stream:
    stream.write(json.dumps(module.params["text"]) + "\\n")
module.exit_json(changed=True)
"""
PIPE = "{{ lookup('pipe', 'id') }}"
RAW = "{% raw %}kept{% endraw %}"
KICKSTART = "%packages\n{{ 7*6 }}\n%end\n"
PROXY = "http://proxy.example.test:8080/" + RAW

# Each fixture by name: the request variable its role declares, that variable's
# argument spec, each probe's expression with the string it must reach the
# module as, and what a probe renders to once the marking is removed.
FIXTURES = {
    "lifecycle": {
        "variable": "bootwright_fixture_request",
        "spec": {"type": "dict", "required": True, "options": {
            "kickstart": {"type": "str", "required": True},
            "names": {"type": "list", "elements": "str", "required": True},
            "nested": {"type": "dict", "required": True, "options": {"comment": {"type": "str", "required": True}}},
            "port": {"type": "int", "required": True},
            "serial": {"type": "int", "required": True},
        }},
        "probes": {
            "kickstart": ("bootwright_fixture_request.kickstart", KICKSTART),
            "pipe": ("bootwright_fixture_request.names[0]", PIPE),
            "raw": ("bootwright_fixture_request.names[1]", RAW),
            "comment": ("bootwright_fixture_request.nested.comment", "{# x #}"),
            "material": ("bootwright_fixture_material.fingerprint", "{{ 6*7 }}"),
            "replaced": ("bootwright_fixture_request.kickstart | replace('%end', '%fin')", "%packages\n{{ 7*6 }}\n%fin\n"),
            "joined": ("bootwright_fixture_request.names | join(',')", PIPE + "," + RAW),
            "included": ("bootwright_fixture_request.names[0]", PIPE),
        },
        "rendered": {"kickstart": "%packages\n42\n%end\n"},
    },
    "controller": {
        "variable": "bootwright_controller_request",
        "spec": {"type": "dict", "required": True},
        "probes": {
            "pipe": ("bootwright_controller_request.packages[0].name", PIPE),
            "noproxy": ("bootwright_controller_request.egress.noProxy[0]", "{{ 7*6 }}"),
            "proxy": ("bootwright_controller_request.egress.httpProxy", PROXY),
            "replaced": ("bootwright_controller_request.egress.httpProxy | replace('http://', 'https://')", "https" + PROXY[4:]),
            "joined": ("bootwright_controller_request.egress.noProxy | join(',')", "{{ 7*6 }}"),
            "included": ("bootwright_controller_request.packages[0].name", PIPE),
        },
        "rendered": {"noproxy": "42"},
    },
}


def unmarked(value):
    """The document with every __ansible_unsafe object replaced by its string."""
    if isinstance(value, dict):
        if set(value) == {"__ansible_unsafe"}:
            return value["__ansible_unsafe"]
        return {key: unmarked(item) for key, item in value.items()}
    if isinstance(value, list):
        return [unmarked(item) for item in value]
    return value


def write_role(roles, name, options, tasks):
    (roles / name / "meta").mkdir(parents=True)
    (roles / name / "tasks").mkdir()
    specs = {"argument_specs": {"main": {"short_description": name, "options": options}}}
    (roles / name / "meta" / "argument_specs.yml").write_text(json.dumps(specs, indent=1))
    (roles / name / "tasks" / "main.yml").write_text(json.dumps(tasks, indent=1))


def play(work, fixture, marked):
    """What the module recorded for each probe of one run over one fixture."""
    spec = FIXTURES[fixture]
    echo = work / "echo"
    echo.mkdir()
    (work / "library").mkdir()
    (work / "library" / "echoed.py").write_text(ECHOED)
    probes = dict(spec["probes"])
    included = probes.pop("included")[0]
    tasks = [{"name": name, "echoed": {"text": "{{ %s }}" % expression, "path": str(echo / name)}}
             for name, (expression, _text) in probes.items()]
    tasks.append({"name": "included", "ansible.builtin.include_role": {"name": "inner"},
                  "vars": {"inner_text": "{{ %s }}" % included, "inner_path": str(echo / "included")}})
    write_role(work / "roles", "probe", {spec["variable"]: spec["spec"]}, tasks)
    write_role(work / "roles", "inner", {"inner_text": {"type": "str", "required": True}, "inner_path": {"type": "path", "required": True}},
               [{"name": "echo", "echoed": {"text": "{{ inner_text }}", "path": "{{ inner_path }}"}}])
    (work / "play.yml").write_text(json.dumps([{
        "hosts": "localhost", "gather_facts": False, "connection": "local",
        "vars": {"ansible_python_interpreter": sys.executable},
        "tasks": [{"name": "probe", "ansible.builtin.include_role": {"name": "probe"}}],
    }], indent=1))
    document = GOLDENS / (fixture + ".json")
    if not marked:
        document = work / "unmarked.json"
        document.write_text(json.dumps(unmarked(json.loads((GOLDENS / (fixture + ".json")).read_text()))))
    (work / "ansible.cfg").write_text("")
    environment = dict(
        os.environ,
        ANSIBLE_CONFIG=str(work / "ansible.cfg"),
        ANSIBLE_HOME=str(work / "home"),
        ANSIBLE_LOCAL_TEMP=str(work / "local"),
        ANSIBLE_COLLECTIONS_PATH=str(COLLECTIONS),
        ANSIBLE_NOCOLOR="1",
        ANSIBLE_FORCE_COLOR="0",
        LC_ALL="C.UTF-8",
    )
    ran = subprocess.run(
        [sys.executable, "-m", "ansible.cli.playbook", "-i", "localhost,", "--extra-vars", "@" + str(document), str(work / "play.yml")],
        stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=environment, cwd=str(work), check=False)
    output = ran.stdout.decode()
    assert ran.returncode == 0, output
    found = {}
    for name in spec["probes"]:
        lines = (echo / name).read_text(encoding="utf-8").splitlines()
        assert len(lines) == 1, output
        found[name] = json.loads(lines[0])
    return found


@pytest.fixture(scope="module")
def recorded(tmp_path_factory):
    runs = {}

    def run(fixture, marked):
        if (fixture, marked) not in runs:
            runs[fixture, marked] = play(tmp_path_factory.mktemp(fixture), fixture, marked)
        return runs[fixture, marked]
    return run


@pytest.mark.parametrize("fixture", list(FIXTURES))
def test_a_runner_request_string_reaches_the_module_verbatim(recorded, fixture):
    expected = {name: text for name, (_expression, text) in FIXTURES[fixture]["probes"].items()}
    assert recorded(fixture, True) == expected


@pytest.mark.parametrize("fixture", list(FIXTURES))
def test_the_same_strings_unmarked_are_rendered(recorded, fixture):
    rendered = recorded(fixture, False)
    for name, text in FIXTURES[fixture]["rendered"].items():
        assert rendered[name] == text, rendered
    assert rendered["pipe"].startswith("uid="), rendered
