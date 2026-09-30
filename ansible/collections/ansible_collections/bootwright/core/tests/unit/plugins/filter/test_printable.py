"""The printable filter keeps exactly the characters str.isprintable() admits, in order."""

from __future__ import annotations

import pytest
from ansible.errors import AnsibleFilterError

from ansible_collections.bootwright.core.plugins.filter.printable import printable

REMOVED = {
    "NUL": "\x00",
    "ESC": "\x1b",
    "a tab": "\t",
    "a newline": "\n",
    "DEL": "\x7f",
    "NEL": "\x85",
    "a C1 control": "\x9b",
    "a no-break space": "\xa0",
    "a zero-width space": "\u200b",
    "a line separator": "\u2028",
    "a bidi override": "\u202e",
    "a bidi isolate": "\u2066",
    "a byte-order mark": "\ufeff",
    "a lone surrogate": "\ud800",
}


@pytest.mark.parametrize("character", list(REMOVED.values()), ids=list(REMOVED))
def test_a_character_that_is_not_printable_is_removed(character):
    assert printable("a" + character + "b") == "ab"


def test_printable_text_is_kept_whole_and_in_order():
    text = "SN 12-34 \u00e9\u4e2d\U0001d7d9 ~"
    assert printable(text) == text


def test_only_text_is_printed():
    with pytest.raises(AnsibleFilterError):
        printable(None)
