"""Shared local invocation and bounded diagnostics for RAM disk discovery.

The runtime names each failure by a code and the phase it stopped in, never by
exception text, request values, subprocess output or paths. The operator
diagnostic of every code and phase lives here, beside the rules that emit
them, so a new code cannot reach the operator without its own description.
"""

from __future__ import annotations

import errno
import socket
import ssl
import urllib.error

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_controller as custody

# The listener statuses the media delivery names by their own code; any other
# status is media-http, and a redirect is media-redirect.
DELIVERY_STATUSES = (400, 401, 403, 404, 416, 500, 502, 503)

# The transport failures the media delivery names, in the order they are
# tested, since a certificate failure is also an SSL error.
DELIVERY_REASONS = (
    (ssl.SSLCertVerificationError, "media-certificate"), (ssl.SSLError, "media-handshake"),
    (socket.gaierror, "media-dns"), (TimeoutError, "media-timeout"), (ConnectionRefusedError, "media-refused"),
)

# The management-controller causes a failure is named by, as bmc-<cause>; any
# other cause is bmc-request.
BMC_CAUSES = (
    "media-verification", "media-verification-config", "privilege", "trust-chain", "trust-inventory", "trust-metadata",
    "trust-conflict", "trust-unproved", "trust-request", "media-fetch",
)


def delivery_code(error):
    if isinstance(error, urllib.error.HTTPError):
        status = error.code
        if not isinstance(status, int) or isinstance(status, bool):
            return "media-http"
        if 300 <= status < 400:
            return "media-redirect"
        return "media-http-%d" % status if status in DELIVERY_STATUSES else "media-http"
    reason = error.reason if isinstance(error, urllib.error.URLError) else error
    for kind, code in DELIVERY_REASONS:
        if isinstance(reason, kind):
            return code
    return "media-connection"


REFUSALS = {
    "discovery request shape": "request",
    "discovery binding": "binding",
    "discovery controller TLS": "bmc-tls",
    "discovery private media TLS": "media-tls",
    "discovery controller identity": "hardware-identity",
    "missing discovery custody has unresolved external ownership": "external-ownership",
    "missing discovery custody has unresolved publication": "publication-ownership",
    "discovery target is already running": "target-running",
    "discovery target power is unknown": "power-unknown",
    "foreign virtual media": "foreign-media",
    "discovery source image identity": "media-identity",
    "discovery source image digest": "media-digest",
    "discovery publication size": "publication-size",
    "discovery publication file": "publication-file",
    "discovery publication path": "publication-path",
    "discovery publication copy": "publication-copy",
    "discovery source size bound": "source-size",
    "RHEL initrd lacks discovery bootstrap tools": "initrd-tools",
    "unqualified discovery initrd layout": "initrd-layout",
    "discovery report deadline": "inventory-timeout",
    "discovery SSH key": "ssh-custody",
    "discovery SSH custody": "ssh-custody",
    "foreign discovery SSH material": "ssh-custody",
    "discovery SSH policy drift": "ssh-policy",
    "discovery SSH client failed": "ssh-client",
    "discovery SSH authentication refused": "ssh-authentication",
    "discovery SSH answer": "ssh-answer",
    "discovery SSH source tool": "ssh-source-tool",
    "discovery SSH source layout": "ssh-source",
    "discovery SSH source extent": "ssh-source",
    "discovery SSH source filesystem": "ssh-source",
    "discovery SSH source file": "ssh-source",
    "discovery SSH source path": "ssh-source",
    "discovery SSH source link": "ssh-source",
    "discovery SSH source ELF": "ssh-source",
    "discovery SSH source helper": "ssh-source",
    "discovery SSH source size": "ssh-source",
    "discovery SSH source ownership": "ssh-source",
    "discovery RAM inventory refused": "inventory-refused",
    "a foreign service answers the discovery address": "foreign-answer",
    "discovery target stopped before its inventory": "target-stopped",
    "discovery lost ownership of the running boot": "lost-ownership",
    "discovery poweroff deadline": "poweroff-timeout",
    "private discovery listener unavailable": "media-response",
    "custody-directory": "custody-directory",
    "custody-mode": "custody-mode",
    "custody-file": "custody-file",
}


def diagnostic(error, controller):
    """Never publish exception text, request values, subprocess output or paths."""

    code = "contract"
    if isinstance(error, custody.media.CommandFailure):
        code = "native-" + error.tool
    elif isinstance(error, custody.ssh.ssh_key_policy.PolicyError):
        code = "ssh-policy"
    elif isinstance(error, custody.redfish_control.UnverifiedCertificate):
        code = "bmc-tls"
    elif isinstance(error, custody.redfish_control.ControllerError):
        cause = getattr(error, "cause", "request")
        code = "bmc-" + cause if isinstance(cause, str) and cause in BMC_CAUSES else "bmc-request"
    elif getattr(controller, "phase", "") == "media-delivery" and isinstance(error, OSError):
        code = delivery_code(error)
    elif isinstance(error, FileNotFoundError):
        code = "missing-file"
    elif isinstance(error, OSError) and error.errno in (errno.ENOSPC, errno.EDQUOT):
        code = "publication-space" if getattr(controller, "phase", "") in ("media", "publication") else "filesystem"
    elif isinstance(error, OSError):
        code = "filesystem"
    elif isinstance(error, bounded_process.Failure):
        code = "native-process"
    elif isinstance(error, ValueError):
        code = REFUSALS.get(str(error), "contract")
    phase = getattr(controller, "phase", "custody")
    if not isinstance(phase, str) or phase not in PHASES:
        phase = "custody"
    return {"discovery_failure": code, "discovery_phase": phase}


# What the operator is told for each phase a failure stopped in.
PHASES = {
    "custody": "request and private custody validation",
    "controller": "management-controller inspection",
    "media": "private discovery media construction",
    "publication": "private discovery media publication",
    "bmc-inventory": "management-controller drive inventory",
    "ssh": "private inventory SSH preparation",
    "media-delivery": "private HTTPS media delivery",
    "media-insertion": "virtual-media trust and insertion",
    "boot-selection": "one-time virtual-media boot selection",
    "power-on": "owned discovery power-on",
    "inventory": "waiting for and reading the RAM boot's authenticated inventory",
    "cleanup": "owned media and power cleanup",
}

# What the operator is told for each code the runtime emits. Every code
# diagnostic() and delivery_code() can name has one, which
# test_disk_discovery_diagnostics.py holds exact.
FAILURES = {
    "request": "the frozen request shape was refused",
    "binding": "the attempt binding was refused",
    "bmc-tls": "the management controller certificate was refused",
    "media-tls": "private media TLS policy was refused",
    "media-certificate": "the artifact listener certificate did not verify against the bound serving certificate",
    "media-handshake": "the artifact listener TLS handshake failed",
    "media-redirect": "the artifact listener attempted a redirect; private URLs are never forwarded",
    "media-http-400": "the artifact listener returned HTTP 400",
    "media-http-401": "the artifact listener returned HTTP 401",
    "media-http-403": "the artifact listener returned HTTP 403; check serving permissions and SELinux labels",
    "media-http-404": "the artifact listener returned HTTP 404; check the published file and served root",
    "media-http-416": "the artifact listener refused the first-byte range with HTTP 416",
    "media-http-500": "the artifact listener returned HTTP 500",
    "media-http-502": "the artifact listener returned HTTP 502",
    "media-http-503": "the artifact listener returned HTTP 503",
    "media-http": "the artifact listener refused the HTTP request",
    "media-dns": "the artifact listener name could not be resolved",
    "media-timeout": "the artifact listener connection or first-byte response timed out",
    "media-refused": "the artifact listener TCP connection was refused",
    "media-connection": "the artifact listener connection failed",
    "media-response": "the artifact listener did not return a successful first byte",
    "hardware-identity": "the complete machine UUID and MAC identity was not proved",
    "external-ownership": "discovery requires a powered-off target with no attached virtual media before initial custody",
    "publication-ownership": "the private publication has unresolved ownership",
    "target-running": "a running target has no matching discovery-media ownership",
    "power-unknown": "the target power state was not proved",
    "foreign-media": "virtual media belongs to another operation",
    "media-identity": "the local source ISO size, ownership or mode was refused",
    "media-digest": "the local source ISO checksum did not match",
    "publication-size": "the remastered ISO exceeds its derived component size bound",
    "publication-file": "the remastered ISO is not a regular file",
    "publication-path": "the private publication path does not match the frozen machine namespace",
    "publication-copy": "the ISO copy made no progress; the incomplete stage was removed",
    "publication-space": (
        "insufficient free space or quota for the private ISO; free space on the hosted filesystem without deleting "
        "discovery custody, then repeat the recorded command"),
    "source-size": "the source ISO exceeds the installer-media store bound",
    "initrd-tools": "the RHEL initramfs lacks required discovery tools",
    "initrd-layout": "the source ISO initramfs layout is unqualified",
    "inventory-timeout": (
        "the RAM boot never answered on its declared address and discovery SSH port; read the last "
        "\"Bootwright discovery\" phase on the machine console, then check the declared install address, VLAN, bond "
        "and the route from the controller; the machine stays on until removal"),
    "inventory-refused": (
        "the RAM boot answered but refused to read its disks (a missing storage driver, or unstable, unreadable or no "
        "supported whole disks); the machine stays on until removal"),
    "foreign-answer": "the SSH endpoint did not present this operation's pinned host key",
    "ssh-policy": "the controller SSH crypto policy refused discovery keys or changed after preparation",
    "ssh-custody": "operation-scoped SSH keys, pins or private material were not proved",
    "ssh-client": "the bounded SSH client did not complete",
    "ssh-authentication": "the pinned RAM SSH server refused this operation's client key",
    "ssh-answer": "the forced SSH command did not return the bounded inventory protocol",
    "ssh-source-tool": (
        "a required source-image inspection tool is missing or untrusted; apply controller prerequisites with "
        "squashfs-tools, e2fsprogs and binutils before discovery"),
    "ssh-source": "the verified RHEL image lacks a qualified SSH runtime or its bounded source layout",
    "target-stopped": "the machine powered off before its inventory was read; remove the discovery and repeat it",
    "lost-ownership": "the running machine no longer has this discovery's media attached; it was left untouched",
    "poweroff-timeout": "the BMC did not report the machine off within its deadline",
    "custody-directory": "a private directory failed its ownership or mode proof",
    "custody-mode": "a private custody directory has an unexpected mode",
    "custody-file": "a private record failed its type, ownership, mode or size proof",
    "bmc-request": "a management-controller request failed",
    "bmc-media-verification": (
        "media-fetch certificate verification is disabled or unproved; enable HttpsTransferCertVerification on every "
        "declaring manager, or device VerifyCertificate when no manager publishes that setting"),
    "bmc-privilege": (
        "the BMC account lacks the required privilege; certificate import and changes to media verification require "
        "security-configuration privilege"),
    "bmc-media-verification-config": (
        "declared media verification setup was refused or not proved; check security-configuration privilege, writable "
        "boolean properties and resource ETags, then retry without deleting custody; partial changes remain recorded"),
    "bmc-trust-chain": "SecurityService import requires the artifact server certificate and its issuer chain",
    "bmc-trust-inventory": "the BMC certificate-slot inventory is unavailable, incomplete or ambiguous",
    "bmc-trust-metadata": "the BMC does not advertise one usable certificate-import action and its required parameters",
    "bmc-trust-conflict": "certificate slots changed or contain another authority; inspect their users before changing trust",
    "bmc-trust-unproved": "certificate import or removal did not preserve and prove the expected slots",
    "bmc-trust-request": "the BMC certificate-import or removal request did not complete",
    "bmc-media-fetch": (
        "the BMC could not fetch the ISO; check TLS compatibility, trust, route, DNS and firewall from the BMC network"),
    "missing-file": "a required runtime file or executable is missing",
    "filesystem": "a filesystem or TLS socket operation failed",
    "native-process": "a bounded native process did not complete",
    "native-xorriso": "xorriso did not complete ISO inspection or remastering",
    "native-lsinitrd": "lsinitrd did not complete initramfs inspection",
    "native-mkksiso": "mkksiso did not complete boot-configuration remastering",
    "native-ssh-keygen": "ssh-keygen did not complete operation-scoped inventory key preparation",
    "native-unsquashfs": "unsquashfs did not complete bounded source-image inspection or extraction",
    "native-debugfs": "debugfs did not complete read-only SSH component extraction",
    "native-readelf": "readelf did not complete SSH runtime qualification",
    "native-chcon": "chcon did not complete the private publication SELinux labeling",
    "native-unknown": "a fixed native command failed",
    "contract": "a bounded discovery contract was refused",
}

# What a result that names no code, or a code or phase that is not text, is reported as.
NO_RESULT = "disk discovery stopped during adapter execution: the adapter returned no bounded result"


def described(result):
    """The operator diagnostic of one registered discovery result, naming only text this module holds."""
    failure = result.get("discovery_failure") if isinstance(result, dict) else None
    phase = result.get("discovery_phase", "") if isinstance(result, dict) else ""
    if not isinstance(failure, str) or not failure or not isinstance(phase, str):
        return NO_RESULT
    during = PHASES.get(phase, "adapter execution")
    if failure not in FAILURES:
        return "disk discovery stopped during %s: the adapter named a failure this collection has no description for" % during
    return "disk discovery stopped during %s: %s" % (during, FAILURES[failure])


def validate_arguments(arguments):
    if (not isinstance(arguments, dict) or set(arguments) != {"operation", "request", "digest", "material"}
            or arguments["operation"] not in ("apply", "observe", "destroy")
            or not isinstance(arguments["request"], dict) or not isinstance(arguments["material"], dict)
            or not isinstance(arguments["digest"], str)):
        raise ValueError("discovery request shape")


def execute(arguments, progress=None):
    controller = None
    try:
        validate_arguments(arguments)
        operation = arguments["operation"]
        controller = custody.Controller(arguments["request"], arguments["digest"], arguments["material"],
                                        create=operation in ("apply", "destroy"), progress=progress)
        if operation == "observe":
            with controller.progress("inspect-target"):
                evidence = controller.evidence()
        else:
            evidence = {"apply": controller.apply, "destroy": controller.destroy}[operation]()
        return {"changed": operation != "observe", "evidence": evidence}
    except FileNotFoundError as error:
        return dict(failed=True, changed=False, msg="disk discovery custody is incomplete; continue the original operation",
                    **diagnostic(error, controller))
    except (OSError, ValueError, KeyError, TypeError, custody.redfish_control.ControllerError, bounded_process.Failure) as error:
        return dict(failed=True, changed=False,
                    msg="disk discovery did not prove its bounded media, hardware identity, inventory and cleanup contract",
                    **diagnostic(error, controller))
    finally:
        if controller is not None:
            controller.close()
