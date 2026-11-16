"""Shared local invocation and bounded diagnostics for RAM disk discovery."""

from __future__ import annotations

import errno
import socket
import ssl
import urllib.error

from ansible_collections.bootwright.core.plugins.module_utils import bounded_process
from ansible_collections.bootwright.core.plugins.module_utils import disk_discovery_controller as custody


def delivery_code(error):
    if isinstance(error, urllib.error.HTTPError):
        status = error.code
        if not isinstance(status, int) or isinstance(status, bool):
            return "media-http"
        if 300 <= status < 400:
            return "media-redirect"
        return {
            400: "media-http-400", 401: "media-http-401", 403: "media-http-403", 404: "media-http-404",
            416: "media-http-416", 500: "media-http-500", 502: "media-http-502", 503: "media-http-503",
        }.get(status, "media-http")
    reason = error.reason if isinstance(error, urllib.error.URLError) else error
    for kind, code in (
        (ssl.SSLCertVerificationError, "media-certificate"), (ssl.SSLError, "media-handshake"),
        (socket.gaierror, "media-dns"), (TimeoutError, "media-timeout"), (ConnectionRefusedError, "media-refused"),
    ):
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
        known = (
            "media-verification", "media-verification-config", "privilege", "trust-chain", "trust-inventory", "trust-metadata",
            "trust-conflict", "trust-unproved", "trust-request", "media-fetch",
        )
        code = "bmc-" + cause if isinstance(cause, str) and cause in known else "bmc-request"
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
    phases = (
        "custody", "controller", "bmc-inventory", "media", "publication", "ssh", "media-delivery",
        "media-insertion", "boot-selection", "power-on", "inventory", "cleanup",
    )
    if phase not in phases:
        phase = "custody"
    return {"discovery_failure": code, "discovery_phase": phase}


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
