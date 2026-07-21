from __future__ import annotations

import ipaddress


def bootwright_ip_in_cidrs(addresses, cidrs):
    """Return True when any address falls within any of the given CIDRs.

    Uses the Python standard library `ipaddress` module so the managed node's
    ansible venv needs no external dependency (netaddr / ansible.utils.ipaddr).
    Malformed or empty addresses and CIDRs are ignored rather than raising, and
    address/network family mismatches never match.
    """
    networks = []
    for cidr in _as_list(cidrs):
        network = _parse_network(cidr)
        if network is not None:
            networks.append(network)
    if not networks:
        return False
    for address in _as_list(addresses):
        ip = _parse_address(address)
        if ip is None:
            continue
        for network in networks:
            if ip.version == network.version and ip in network:
                return True
    return False


def _as_list(value):
    if isinstance(value, (list, tuple)):
        return list(value)
    if value is None:
        return []
    return [value]


def _parse_address(value):
    if not isinstance(value, str):
        return None
    text = value.strip()
    if not text:
        return None
    try:
        return ipaddress.ip_address(text)
    except ValueError:
        return None


def _parse_network(value):
    if not isinstance(value, str):
        return None
    text = value.strip()
    if not text:
        return None
    try:
        return ipaddress.ip_network(text, strict=False)
    except ValueError:
        return None


class FilterModule:
    def filters(self):
        return {
            "bootwright_ip_in_cidrs": bootwright_ip_in_cidrs,
        }
