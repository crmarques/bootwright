"""Installer inputs are written so the installer's YAML reader keeps every value.

The installer reads them with go.yaml.in/yaml/v2, which refuses a raw DEL, C1
control or U+FFFE, folds a raw NEL into a space, and refuses the JSON
surrogate-pair escape of a code point above U+FFFF
(.agents/knowledge/installer-input-yaml-scalars.md). PyYAML stands in for it
here as a YAML reader of the escapes the filter writes.
"""

from __future__ import annotations

import pytest
import yaml
from ansible.errors import AnsibleFilterError

from ansible_collections.bootwright.core.plugins.filter.to_installer_yaml import to_installer_yaml


# Each string, and the escape it is written as.
@pytest.mark.parametrize("text, escape", [
    ("\x7f", "\\u007f"),
    ("\x80", "\\u0080"),
    ("\x85", "\\u0085"),
    ("\u2028", "\\u2028"),
    ("\ufffe", "\\ufffe"),
    ("\u00e9", "\\u00e9"),
    ("\U0001D7D9", "\\U0001d7d9"),
    ("\U0010FFFF", "\\U0010ffff"),
    ("\n\x01", "\\n\\u0001"),
], ids=["DEL", "C1", "NEL", "line separator", "noncharacter", "latin", "above the BMP", "last code point", "C0"])
def test_every_character_outside_printable_ascii_is_escaped(text, escape):
    written = to_installer_yaml({"model": "a" + text + "b"})
    assert written == '{\n  "model": "a' + escape + 'b"\n}'
    assert yaml.safe_load(written) == {"model": "a" + text + "b"}


# Keys are sorted and every string is quoted, so a value YAML 1.1 would retype
# stays a string, while a number and a boolean keep their own types.
def test_strings_stay_strings_and_other_values_keep_their_types():
    value = {"z": ["y", "1e3", "0o17"], "a": {"minSizeGigabytes": 0, "rotational": False, "none": None}}
    written = to_installer_yaml(value)
    assert written.index('"a"') < written.index('"z"')
    assert '"y"' in written and '"1e3"' in written and '"0o17"' in written
    assert yaml.safe_load(written) == value


# A lone surrogate is no character at all, so nothing is written for it.
def test_a_lone_surrogate_is_refused():
    with pytest.raises(AnsibleFilterError, match="lone surrogate"):
        to_installer_yaml({"model": "\ud835"})
