"""The libvirt network and domain definitions write a bridge as an escaped XML attribute value.

Admission narrows a bridge to an interface-name grammar, but the definitions
are the last boundary before libvirt reads them, so each writes the bridge with
the escape filter: a value holding a quote, an angle bracket or an ampersand
stays one attribute value and reads back unchanged.
"""

from __future__ import annotations

import copy
from xml.etree import ElementTree

from ansible_collections.bootwright.core.tests.unit.test_role_templates import SCOPES, render, scope

HOSTILE = "br\"<&'>x"
ESCAPED = "br&#34;&lt;&amp;&#39;&gt;x"


def test_the_network_template_escapes_its_bridge():
    role, template = "substrate_libvirt_host", "network.xml.j2"
    variables = copy.deepcopy(scope(role, SCOPES[(role, template)]))
    variables["item"]["bridge"] = HOSTILE
    output = render(role, template, variables)
    assert '  <bridge name="%s" stp="on" delay="0" zone="trusted"/>\n' % ESCAPED in output
    assert ElementTree.fromstring(output).find("bridge").get("name") == HOSTILE


def test_the_domain_template_escapes_its_bridge():
    role, template = "substrate_libvirt_machine", "domain.xml.j2"
    variables = copy.deepcopy(scope(role, SCOPES[(role, template)]))
    external = variables["bootwright_substrate_machine_request"]["interfaces"][1]
    assert external["network"] == ""
    external["bridge"] = HOSTILE
    output = render(role, template, variables)
    assert '<source bridge="%s"/>' % ESCAPED in output
    source = ElementTree.fromstring(output).find("devices/interface[@type='bridge']/source")
    assert source.get("bridge") == HOSTILE
