"""Publish only bounded credential-update proof on the private result channel."""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.module_utils import adapter_protocol


def completion(arguments):
    if arguments.get("proved") is not True:
        raise ValueError("credential update unproved")
    return {"proved": True, "request": adapter_protocol.request_digest(arguments.get("digest"))}, None


CAPABILITY = adapter_protocol.Capability("Cluster administrator credentials", "credential proof publication failed", completion)
