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


def bootwright_normalize_ip_set(addresses):
    normalized = set()
    for value in _as_list(addresses):
        address = _parse_address(value)
        if address is None:
            text = str(value).strip() if value is not None else ""
            if text:
                normalized.add("invalid:" + text)
            continue
        if isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped is not None:
            address = address.ipv4_mapped
        normalized.add(str(address))
    return sorted(normalized)


def bootwright_resolved_static_conflicts(lines, owned_path):
    source = "<unknown>"
    conflicts = []
    for value in _as_list(lines):
        line = str(value).strip()
        if line.startswith("# ") and line[2:].strip().startswith("/"):
            source = line[2:].strip()
            continue
        if not line or line.startswith(('#', ';')) or '=' not in line:
            continue
        key, raw = line.split('=', 1)
        if key.strip() not in {"DNS", "Domains"}:
            continue
        if source != owned_path:
            conflicts.append(f"{source}: {key.strip()}={raw.strip()}")
    return sorted(set(conflicts))


def bootwright_resolved_runtime_conflicts(dns_lines, domain_lines, owned, addresses, domains, require_exact=False):
    expected_addresses = bootwright_normalize_ip_set(addresses)
    expected_domains = sorted({"~" + _normalize_domain(value) for value in _as_list(domains) if _normalize_domain(value)})
    global_addresses, _ = _parse_resolvectl_values(dns_lines, True)
    global_domains, link_domains = _parse_resolvectl_values(domain_lines, False)
    conflicts = []
    if require_exact:
        if global_addresses != expected_addresses:
            conflicts.append("global DNS=" + (",".join(global_addresses) or "<none>"))
        if global_domains != expected_domains:
            conflicts.append("global Domains=" + (",".join(global_domains) or "<none>"))
    elif not owned:
        if global_addresses:
            conflicts.append("global DNS=" + ",".join(global_addresses))
        if global_domains:
            conflicts.append("global Domains=" + ",".join(global_domains))
    expected_plain = [_normalize_domain(value) for value in _as_list(domains) if _normalize_domain(value)]
    for link, values in link_domains.items():
        for value in values:
            candidate = _normalize_domain(value)
            if any(_domains_overlap(candidate, expected) for expected in expected_plain):
                conflicts.append(f"{link} Domains={value}")
    return sorted(set(conflicts))


def _parse_resolvectl_values(lines, addresses):
    global_values = []
    link_values = {}
    current = ""
    for value in _as_list(lines):
        line = str(value).strip()
        if not line:
            continue
        if ':' in line:
            label, raw = line.split(':', 1)
            current = label.strip()
            tokens = raw.split()
        elif current:
            tokens = line.split()
        else:
            continue
        if addresses:
            normalized = bootwright_normalize_ip_set(tokens)
        else:
            normalized = sorted(set(tokens))
        if current == "Global":
            global_values.extend(normalized)
        elif current.startswith("Link "):
            link_values.setdefault(current, []).extend(normalized)
    return sorted(set(global_values)), {key: sorted(set(values)) for key, values in link_values.items()}


def _normalize_domain(value):
    text = str(value).strip().lower().lstrip('~').rstrip('.')
    return text


def _domains_overlap(left, right):
    if not left or not right:
        return False
    if left == "." or right == ".":
        return True
    return left == right or left.endswith("." + right) or right.endswith("." + left)


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
            "bootwright_normalize_ip_set": bootwright_normalize_ip_set,
            "bootwright_resolved_static_conflicts": bootwright_resolved_static_conflicts,
            "bootwright_resolved_runtime_conflicts": bootwright_resolved_runtime_conflicts,
        }
