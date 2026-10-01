"""Tests of the Ansible check harness, which its units suite runs."""

import importlib.util
import os
from pathlib import Path
import string
import unittest
import warnings

from jinja2.nativetypes import NativeEnvironment

_specification = importlib.util.spec_from_file_location(
    "ansible_check", Path(__file__).with_name("ansible_check.py")
)
ansible_check = importlib.util.module_from_spec(_specification)
_specification.loader.exec_module(ansible_check)

# The characters tempfile draws the eight of a name's random part from.
RANDOM_CHARACTERS = string.ascii_lowercase + string.digits + "_"

# CPython's tokenizer warns, rather than refuses, when a number runs into
# "if", "in" or "is", or into one of the others where no name character
# follows it.
KEYWORDS = ("and", "else", "for", "if", "in", "is", "not", "or")


def syntax_warnings(area: Path) -> list[str]:
    """Render an ansible-core default whose home lies in the area.

    With HOME in the area, ansible-core renders each ANSIBLE_HOME default,
    such as '{{ ANSIBLE_HOME ~ "/tmp" }}', in Jinja's native environment,
    which parses the rendered path as a Python literal and keeps the text
    when it does not parse. This returns the warnings that parse printed.
    """
    template = NativeEnvironment().from_string('{{ ANSIBLE_HOME ~ "/tmp" }}')
    with warnings.catch_warnings(record=True) as caught:
        warnings.simplefilter("always")
        rendered = template.render(ANSIBLE_HOME=str(area / ".ansible"))
    if rendered != str(area / ".ansible/tmp"):
        raise AssertionError(f"rendered {rendered!r}")
    return [
        str(warning.message)
        for warning in caught
        if issubclass(warning.category, SyntaxWarning)
    ]


def ending_keyword(character: str, keyword: str) -> str:
    """Return a random part that repeats the character up to the keyword."""
    return keyword.rjust(8, character)


def random_parts() -> list[str]:
    """Return random parts that start with each character, then a keyword."""
    parts = []
    for character in RANDOM_CHARACTERS:
        for keyword in KEYWORDS:
            parts.append((character + keyword).ljust(8, "x"))
            parts.append(ending_keyword(character, keyword))
    return parts


class CheckAreaTest(unittest.TestCase):
    def test_the_gate_runs_in_an_area_named_with_the_prefix(self):
        # The gate names the area it made and gave its processes as HOME; a
        # by-hand run of this file has none.
        named = os.environ.get(ansible_check.AREA_VARIABLE)
        if named is None:
            self.skipTest(f"{ansible_check.AREA_VARIABLE} names no area")
        area = Path(named)
        self.assertEqual(os.environ.get("HOME"), named)
        self.assertEqual(os.environ.get("TMPDIR"), named)
        self.assertTrue(area.is_dir())
        self.assertTrue(area.name.startswith(ansible_check.AREA_PREFIX), area.name)
        self.assertEqual(syntax_warnings(area), [])

    def test_the_check_area_is_named_with_the_prefix(self):
        with ansible_check.check_area() as temporary:
            area = Path(temporary)
            self.assertTrue(area.is_dir())
            self.assertTrue(area.name.startswith(ansible_check.AREA_PREFIX))
            self.assertEqual(syntax_warnings(area), [])

    def test_no_random_part_joins_the_prefix_into_a_number(self):
        for part in random_parts():
            area = Path("/tmp") / (ansible_check.AREA_PREFIX + part)
            with self.subTest(area=str(area)):
                self.assertEqual(syntax_warnings(area), [])

    def test_a_hyphen_before_the_random_part_warns(self):
        # The prefix the harness once used: a digit after its hyphen starts
        # a number, and each of these parts ran it into a keyword.
        for character in string.digits:
            for keyword in KEYWORDS:
                part = ending_keyword(character, keyword)
                area = Path("/tmp") / ("bootwright-ansible-check-" + part)
                with self.subTest(area=str(area)):
                    self.assertNotEqual(syntax_warnings(area), [])


if __name__ == "__main__":
    unittest.main()
