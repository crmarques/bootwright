"""Discovered SecurityService trust for a remote HTTPS media server.

The qualified text-import action allocates an unused FileTransfer slot when
no RootCertId is sent. Supplying an ID would replace its occupant, so import
never supplies one. Inventory and action metadata must be complete before a
write, and only the exact authority fingerprint may be removed.
"""

from __future__ import annotations

import hashlib
import re
import ssl

from ansible_collections.bootwright.core.plugins.module_utils import redfish_discovery

IMPORT = "#SecurityService.ImportRemoteHttpsServerRootCA"
DELETE = "#SecurityService.DeleteRemoteHttpsServerRootCA"
INVENTORY = "RemoteHttpsServerCertChainInfo"
PRIVILEGE_MESSAGES = ("InsufficientPrivilege", "InsufficientPermission", "PropertyModificationNeedPrivilege")


class TrustError(Exception):
    """A bounded refusal containing neither certificate nor response body."""


def denied(status, body):
    """A privilege response retains its meaning even on HTTP 400 firmware."""
    if status in (401, 403):
        return True
    if status != 400 or not isinstance(body, dict):
        return False
    error = body.get("error")
    sources = [body.get("Messages"), error.get("@Message.ExtendedInfo") if isinstance(error, dict) else None]
    for source in sources:
        entries = [source] if isinstance(source, dict) else source if isinstance(source, list) else []
        for entry in entries:
            message = redfish_discovery.message_id({"Messages": [entry]}).rsplit(".", 1)[-1]
            if message in PRIVILEGE_MESSAGES:
                return True
    return False


def authority(chain):
    """The terminal issuer and its SHA256; a serving leaf alone is insufficient."""
    blocks = re.findall(r"-----BEGIN CERTIFICATE-----.*?-----END CERTIFICATE-----", chain, re.DOTALL)
    if not 2 <= len(blocks) <= 8:
        raise TrustError("SecurityService media trust requires the artifact server's leaf and issuer chain; serve that chain or configure established trust")
    try:
        ders = [ssl.PEM_cert_to_DER_cert(block) for block in blocks]
    except ValueError:
        raise TrustError("SecurityService media trust received an invalid certificate chain") from None
    return blocks[-1] + "\n", hashlib.sha256(ders[-1]).hexdigest()


def inventory(resource):
    """Read every occupied slot; an unrecognized row never proves a free slot."""
    rows = resource.get(INVENTORY)
    if not isinstance(rows, list) or len(rows) > 8:
        raise TrustError("SecurityService certificate-slot inventory is unavailable or exceeds eight slots")
    found = {}
    for row in rows:
        if not isinstance(row, dict):
            raise TrustError("SecurityService certificate-slot inventory is incomplete")
        slot, usage = row.get("CertId"), row.get("Usage")
        if not isinstance(slot, int) or isinstance(slot, bool) or not 1 <= slot <= 8 or slot in found:
            raise TrustError("SecurityService certificate-slot identity is ambiguous")
        expected = "FileTransfer" if slot >= 5 else "EventSubscription"
        certificate = row.get("ServerCert")
        fingerprint = certificate.get("FingerPrint") if isinstance(certificate, dict) else None
        if usage != expected or not isinstance(fingerprint, str) or not re.fullmatch(r"[0-9a-fA-F]{64}", fingerprint):
            raise TrustError("SecurityService certificate-slot usage or SHA256 fingerprint is unknown")
        found[slot] = (usage, fingerprint.lower())
    return found


class RemoteCA:
    """One client-owned transaction over the controller's confined transport."""

    def __init__(self, client):
        self.client = client

    def resource(self):
        links = {link for _manager, body in self.client._managers for link in redfish_discovery.security_services(body)}
        if len(links) != 1:
            raise TrustError("SecurityService media trust needs one unambiguous discovered security service")
        url = self.client._own(links.pop(), "reading media trust")
        body, slots = self.state(url)
        return url, body, slots

    def state(self, url):
        body, _headers = self.client._read(url, "reading media trust")
        if body.get(redfish_discovery.TRANSFER_VERIFICATION) is not True:
            raise TrustError("SecurityService media trust requires HttpsTransferCertVerification enabled by the operator")
        return body, inventory(body)

    def target(self, resource, action, payload):
        candidates = redfish_discovery.actions(resource, action)
        if len(candidates) != 1 or not candidates[0]["actionInfo"]:
            raise TrustError("SecurityService media trust action or its parameter metadata is unavailable or ambiguous")
        candidate = candidates[0]
        target = self.client._own(candidate["target"], "checking media trust action")
        info = self.client._own(candidate["actionInfo"], "checking media trust parameters")
        parameters = self.client._read(info, "checking media trust parameters")[0].get("Parameters")
        if not isinstance(parameters, list):
            raise TrustError("SecurityService media trust action has no readable parameter metadata")
        names = set()
        for parameter in parameters:
            if not isinstance(parameter, dict) or not isinstance(parameter.get("Name"), str):
                raise TrustError("SecurityService media trust parameter metadata is incomplete")
            name = parameter["Name"]
            if name in names or parameter.get("Required") is True and name not in payload:
                raise TrustError("SecurityService media trust action requires unsupported parameters")
            names.add(name)
            allowed = parameter.get("AllowableValues")
            if name in payload and isinstance(allowed, list) and payload[name] not in allowed:
                raise TrustError("SecurityService media trust action does not admit the required parameter value")
        if not set(payload) <= names:
            raise TrustError("SecurityService media trust action does not declare the required parameters")
        return target

    def write(self, target, payload):
        status, body, _headers = self.client.fetch(target, "POST", payload, doing="updating media trust")
        if denied(status, body):
            raise TrustError("SecurityService media trust: HTTP %d; the account needs security-configuration privilege" % status)
        message = redfish_discovery.message_id(body)
        if status not in (0, 200, 204) or isinstance(body, dict) and "error" in body and not message.endswith(".Success"):
            raise TrustError("SecurityService media trust action did not complete: HTTP %d" % status)

    def apply(self, chain, remove=False):
        url, resource, slots = self.resource()
        pem, fingerprint = authority(chain)
        matches = [slot for slot, entry in slots.items() if entry == ("FileTransfer", fingerprint)]
        if len(matches) > 1:
            raise TrustError("SecurityService media authority occupies ambiguous slots")
        selected = matches[0] if matches else None
        if remove and not matches or not remove and matches:
            self.client.last_trust_slot = matches[0] if matches else None
            return False
        payload = {"RootCertId": matches[0]} if remove else {"Type": "text", "Content": pem, "Usage": "FileTransfer"}
        if not remove and any(usage == "FileTransfer" for usage, _fingerprint in slots.values()):
            raise TrustError("SecurityService already holds another file-transfer authority; remove it out of band after checking its users")
        target = self.target(resource, DELETE if remove else IMPORT, payload)
        _resource, fresh = self.state(url)
        if fresh != slots:
            raise TrustError("SecurityService certificate slots changed before the write; no trust write was sent")
        self.write(target, payload)
        _resource, after = self.state(url)
        matches = [slot for slot, entry in after.items() if entry == ("FileTransfer", fingerprint)]
        if remove and matches or not remove and len(matches) != 1:
            raise TrustError("SecurityService did not prove the requested authority's presence or removal")
        expected = dict(slots)
        if remove:
            del expected[selected]
        else:
            expected[matches[0]] = ("FileTransfer", fingerprint)
        if after != expected:
            raise TrustError("SecurityService changed unrelated certificate slots; trust effects are unknown")
        self.client.last_trust_slot = selected if remove else matches[0]
        return True
