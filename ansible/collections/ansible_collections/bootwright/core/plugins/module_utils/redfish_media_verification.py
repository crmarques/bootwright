"""Declared media-fetch verification through conditional writes and read-back."""

from __future__ import annotations

import re

from ansible_collections.bootwright.core.plugins.module_utils import redfish_discovery, redfish_remote_ca

DOING = "configuring media verification"
MAX_SERVICES = 4


class Refused(Exception):
    """Only fixed procedure text and a bounded cause cross this boundary."""

    def __init__(self, text, cause="media-verification-config"):
        super().__init__(text)
        self.cause = cause


def setting(url, property_name, body, headers, wanted):
    value = body.get(property_name)
    if value is not True and value is not False:
        raise Refused("media verification setup requires a declared boolean property")
    tag = headers.get("etag") or body.get("@odata.etag")
    if value is not wanted and (not isinstance(tag, str) or len(tag) > 1024 or re.fullmatch(r'(?:W/)?"[\x21\x23-\x7e]*"', tag) is None):
        raise Refused("media verification setup requires a usable resource ETag before changing a property")
    return url, property_name, value, tag


def services(client):
    links = {
        client._own(link, DOING)
        for _manager, body in client._managers
        for link in redfish_discovery.security_services(body)
    }
    if len(links) > MAX_SERVICES:
        raise Refused("media verification setup exceeds four security services")
    return sorted(links)


def preflight(client, member, wanted):
    found = []
    for url in services(client):
        body, headers = client._read(url, DOING)
        if redfish_discovery.TRANSFER_VERIFICATION in body:
            found.append(setting(url, redfish_discovery.TRANSFER_VERIFICATION, body, headers, wanted))
    if found:
        return found
    body, headers = client._read(member, DOING)
    return [setting(member, "VerifyCertificate", body, headers, wanted)]


def configure(client, member, wanted, rows=None):
    # Preflight every deciding resource before changing the first. Partial
    # changes are retained for policy-directed continuation or restoration.
    rows = preflight(client, member, wanted) if rows is None else rows
    changed = False
    for url, property_name, value, tag in rows:
        if value is wanted:
            continue
        status, body, headers = client.fetch(url, "PATCH", {property_name: wanted}, headers={"If-Match": tag}, doing=DOING)
        if redfish_remote_ca.denied(status, body):
            raise Refused("media verification setup requires security-configuration privilege", "privilege")
        failed_body = isinstance(body, dict) and "error" in body and not redfish_discovery.message_id(body).endswith(".Success")
        if status not in (0, 200, 202, 204) or failed_body:
            raise Refused("media verification setup was refused; reread the BMC state before retrying")
        if status == 202 and client._await_task(body, headers, doing=DOING):
            raise Refused("media verification setup task did not complete")
        observed, _headers = client._read(url, DOING)
        if observed.get(property_name) is not wanted:
            raise Refused("media verification setup did not read back the requested state")
        changed = True
    return changed
