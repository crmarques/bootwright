import importlib.util
import pathlib
import struct
import unittest


_HERE = pathlib.Path(__file__).resolve().parent
_PROBE = _HERE.parent / "roles" / "openshift" / "install_agent" / "files" / "dns_probe.py"
_spec = importlib.util.spec_from_file_location("_bootwright_dns_probe", _PROBE)
assert _spec and _spec.loader, "could not locate dns_probe.py"
_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_module)


def _qname(packet):
    name, _ = _module._read_name(packet, 12)
    return name


def _rr(name, rtype, value):
    owner = _module._encode_name(name)
    if rtype == _module.TYPE_A:
        rdata = bytes(int(part) for part in value.split("."))
    elif rtype == _module.TYPE_CNAME:
        rdata = _module._encode_name(value)
    else:
        raise AssertionError(f"unsupported test record type {rtype}")
    return owner + struct.pack(">HHIH", rtype, _module.CLASS_IN, 60, len(rdata)) + rdata


def _response(packet, answers=None, additionals=None, rcode=0):
    answers = answers or []
    additionals = additionals or []
    flags = 0x8180 | rcode
    header = packet[:2] + struct.pack(">HHHHH", flags, 1, len(answers), 0, len(additionals))
    return header + packet[12:] + b"".join(answers) + b"".join(additionals)


class DNSProbe(unittest.TestCase):
    def setUp(self):
        self.exchange = _module._exchange

    def tearDown(self):
        _module._exchange = self.exchange

    def test_follows_cname_when_response_has_no_address(self):
        def exchange(_server, packet, _timeout):
            name = _qname(packet)
            if name == "api.cluster.example.test":
                return _response(packet, [_rr(name, _module.TYPE_CNAME, "vip.cluster.example.test")])
            if name == "vip.cluster.example.test":
                return _response(packet, [_rr(name, _module.TYPE_A, "10.7.3.20")])
            return _response(packet, rcode=3)

        _module._exchange = exchange

        self.assertEqual(_module.query("127.0.0.1", "api.cluster.example.test"), ["10.7.3.20"])

    def test_accepts_cname_target_address_in_additional_section(self):
        def exchange(_server, packet, _timeout):
            name = _qname(packet)
            return _response(
                packet,
                [_rr(name, _module.TYPE_CNAME, "vip.cluster.example.test")],
                [_rr("vip.cluster.example.test", _module.TYPE_A, "10.7.3.21")],
            )

        _module._exchange = exchange

        self.assertEqual(_module.query("127.0.0.1", "console.apps.cluster.example.test"), ["10.7.3.21"])

    def test_refused_response_is_empty_answer(self):
        def exchange(_server, packet, _timeout):
            return _response(packet, rcode=5)

        _module._exchange = exchange

        self.assertEqual(_module.query("127.0.0.1", "validateNoWildcardDNS.cluster.example.test"), [])


if __name__ == "__main__":
    unittest.main()
