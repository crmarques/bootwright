"""Validate the managed DNS forwarding boundary before configuration is published."""

from __future__ import annotations

import ipaddress
import re

DOCUMENTATION = r"""
name: dns_forward_zones_valid
short_description: Validate frozen DNS suffix forwarding rules
version_added: "0.1.0"
description:
  - Checks closed forwarding records, DNS labels, resolver addresses and bounds.
  - Rejects forwarding to the service's explicit listener and configuration injection.
options:
  _input:
    description: The frozen managed-service request.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Prove frozen DNS forwarding rules
  ansible.builtin.assert:
    that:
      - request | bootwright.core.dns_forward_zones_valid
"""

RETURN = r"""
_value:
  description: Whether the forwarding configuration is valid.
  type: bool
"""


def resolver_address(value):
    if not isinstance(value, str):
        raise ValueError("resolver address is not text")
    address = ipaddress.ip_address(value)
    if getattr(address, "scope_id", None):
        raise ValueError("scoped resolver address")
    return getattr(address, "ipv4_mapped", None) or address


def dns_forward_zones_valid(request):
    """A malformed request proves no authority to publish daemon configuration."""
    try:
        if not isinstance(request, dict):
            return False
        zones = request.get("forwardZones", [])
        local = request.get("localZones", [])
        if not isinstance(zones, list) or len(zones) > 16:
            return False
        if not isinstance(local, list) or len(local) > 8192 or not all(valid_domain(zone) for zone in local):
            return False
        if len(local) != len(set(local)):
            return False
        if not zones and not local:
            return True
        if request.get("kind") != "DNSServer":
            return False
        bind = resolver_address(request["bindAddress"]) if zones else None
        domains = set()
        for zone in zones:
            if not isinstance(zone, dict) or set(zone) != {"domain", "forwarders"}:
                return False
            domain = zone["domain"]
            if not valid_domain(domain) or domain in domains:
                return False
            if any(domain == own or domain.endswith("." + own) for own in local):
                return False
            domains.add(domain)
            targets = zone["forwarders"]
            if not isinstance(targets, list) or not 1 <= len(targets) <= 16:
                return False
            if any(resolver_address(target) == bind for target in targets):
                return False
        return True
    except (ValueError, TypeError, KeyError):
        return False


def valid_domain(domain):
    return isinstance(domain, str) and 1 <= len(domain) <= 253 and all(
        1 <= len(label) <= 63 and re.fullmatch(r"[a-z0-9](?:[a-z0-9-]*[a-z0-9])?", label)
        for label in domain.split(".")
    )


class FilterModule:
    """Pure proof used by the managed-service role before any effect."""

    def filters(self):
        return {"dns_forward_zones_valid": dns_forward_zones_valid}
