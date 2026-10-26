"""The one canonical encoder writes the bytes the Go record reader accepts."""

from __future__ import annotations

import json
import pathlib

from ansible_collections.bootwright.core.plugins.module_utils import canonical_json

VECTORS = pathlib.Path(__file__).resolve().parents[2] / "goldens" / "canonical_json_records.json"


def test_canonical_is_sorted_compact_ascii_bytes():
    assert canonical_json.canonical({"b": [1, {"d": None, "c": True}], "a": "café"}) == b'{"a":"caf\\u00e9","b":[1,{"c":true,"d":null}]}'
    assert canonical_json.canonical({"note": "<b> & ☃"}) == b'{"note":"<b> & \\u2603"}'
    assert canonical_json.canonical({}) == b"{}" and canonical_json.canonical([]) == b"[]"


def test_the_vectors_the_go_record_reader_reads_are_what_the_encoder_writes():
    vectors = json.loads(VECTORS.read_text())
    assert {vector["admission"] for vector in vectors} == {"lifecycle", "controller"}
    assert any("\\u" in vector["line"] for vector in vectors), "the vectors hold a non-ASCII escape"
    for vector in vectors:
        assert canonical_json.canonical(json.loads(vector["line"])) == vector["line"].encode("ascii")
