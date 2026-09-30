"""Keep only the printable characters of a text.

A refusal that prints what a management controller reported cannot trust that
text to be printable. The rule is Python's str.isprintable(), which is Go's
unicode.IsPrint and the CLI's printable rule: letters, marks, numbers,
punctuation, symbols and the ASCII space. So a C0 or C1 control, DEL, a line
or paragraph separator, any space but U+0020, and a format character such as a
bidi override, a zero-width space or a byte-order mark are all removed.
"""

from __future__ import annotations

DOCUMENTATION = r"""
name: printable
short_description: Keep only a text's printable characters
version_added: "0.1.0"
description:
  - Removes every character Python's C(str.isprintable) refuses and keeps the
    rest in order.
  - Every control and format character goes, so what remains prints as itself
    and carries no terminal escape and no bidi override.
options:
  _input:
    description: The text to print.
    type: str
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Print what a controller reported, bounded
  ansible.builtin.debug:
    msg: "{{ (observation.serial | bootwright.core.printable)[:128] }}"
"""

RETURN = r"""
_value:
  description: The printable characters of the input, in order.
  type: str
"""

from ansible.errors import AnsibleFilterError


def printable(text):
    """The printable characters of a text, in order."""
    if not isinstance(text, str):
        raise AnsibleFilterError("printable: the input is not text")
    return "".join(character for character in text if character.isprintable())


class FilterModule:
    """The filters this collection offers its roles."""

    def filters(self):
        return {"printable": printable}
