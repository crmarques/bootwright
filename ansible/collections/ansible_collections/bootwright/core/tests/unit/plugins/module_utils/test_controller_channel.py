"""The runner protocol writer frames, bounds and acknowledges on fixed descriptors."""

import json
import os
import unittest

from ansible_collections.bootwright.core.plugins.module_utils import controller_channel


class Channel:
    """Binds the fixed descriptors 3 and 4 to pipes for one exchange."""

    def __init__(self, response=b"proceed\n"):
        self.response = response
        self.saved = {}

    def __enter__(self):
        result_read, result_write = os.pipe()
        authorization_read, authorization_write = os.pipe()
        os.write(authorization_write, self.response)
        os.close(authorization_write)
        self.result = result_read
        for descriptor, replacement in ((3, result_write), (4, authorization_read)):
            try:
                self.saved[descriptor] = os.dup(descriptor)
            except OSError:
                self.saved[descriptor] = None
            os.dup2(replacement, descriptor)
            os.close(replacement)
        return self

    def written(self):
        os.close(3)
        data = b""
        while True:
            part = os.read(self.result, 65536)
            if not part:
                return data
            data += part

    def __exit__(self, *_):
        for descriptor, original in self.saved.items():
            if original is None:
                os.close(descriptor)
                continue
            os.dup2(original, descriptor)
            os.close(original)
        os.close(self.result)
        return False


class ControllerChannel(unittest.TestCase):
    def test_message_is_canonical_and_newline_framed(self):
        with Channel() as channel:
            controller_channel.emit({"phase": "loaded", "b": 1, "a": 2})
            written = channel.written()
        self.assertTrue(written.endswith(b"\n"))
        self.assertEqual(written.count(b"\n"), 1)
        # Canonical form is sorted, compact and ASCII, so the reader on the
        # other side can compare bytes rather than parsed values.
        self.assertEqual(written, b'{"a":2,"b":1,"phase":"loaded"}\n')
        self.assertEqual(json.loads(written), {"a": 2, "b": 1, "phase": "loaded"})

    def test_non_ascii_is_escaped_rather_than_emitted_raw(self):
        with Channel() as channel:
            controller_channel.emit({"phase": "loaded", "name": "café"})
            written = channel.written()
        self.assertEqual(written, b'{"name":"caf\\u00e9","phase":"loaded"}\n')

    def test_oversized_message_refuses_before_any_write(self):
        with Channel() as channel:
            with self.assertRaises(ValueError):
                controller_channel.emit({"phase": "loaded", "pad": "x" * 65536})
            self.assertEqual(channel.written(), b"")

    def test_acknowledgement_accepts_only_the_exact_authorization(self):
        # Framing stops at the first newline, so anything the runner sends after
        # one authorization stays unread rather than corrupting this exchange.
        for accepted in (b"proceed\n", b"proceed\nproceed\n"):
            with self.subTest(response=accepted):
                with Channel(response=accepted):
                    controller_channel.emit({"phase": "loaded"}, acknowledge=True)
        for refused in (b"proceed", b"stop\n", b"", b"proceedX\n", b"PROCEED\n", b" proceed\n"):
            with self.subTest(response=refused):
                with Channel(response=refused):
                    with self.assertRaises(ValueError):
                        controller_channel.emit({"phase": "loaded"}, acknowledge=True)

    def test_unacknowledged_message_never_reads_authorization(self):
        with Channel(response=b"stop\n") as channel:
            controller_channel.emit({"phase": "completed"})
            self.assertEqual(channel.written(), b'{"phase":"completed"}\n')


if __name__ == "__main__":
    unittest.main()
