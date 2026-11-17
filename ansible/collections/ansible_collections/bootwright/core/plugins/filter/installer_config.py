"""Compose the install configuration from the frozen document and the bound material."""

from __future__ import annotations

from ansible.errors import AnsibleFilterError
from ansible_collections.bootwright.core.plugins.filter.installer_proxy_credentials import installer_proxy_credentials
from ansible_collections.bootwright.core.plugins.filter.registry_pull_secret import registry_pull_secret
from ansible_collections.bootwright.core.plugins.filter.to_installer_yaml import to_installer_yaml

DOCUMENTATION = r"""
name: installer_config
short_description: Write the install configuration with its bound material
version_added: "0.1.0"
description:
  - Takes the install configuration the media request froze and substitutes the
    pull secret, merged with the selected mirror credential, the cluster key,
    the selected additional trust bundles and the proxy credentials, read from
    the controller files the runner bound for this build.
  - Each trust bundle is bound as trustBundle<index>, in the order the cluster
    selects them, and each is trimmed, because the installer refuses a bundle
    that holds anything after its last certificate, a blank line included.
  - It returns the document as the installer's YAML, every string quoted.
  - Errors never include a path or secret material.
options:
  _input:
    description: The frozen media request.
    type: dict
    required: true
  material:
    description: The bound material, each file by its controller path.
    type: dict
    required: true
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Write the install configuration with its bound material
  bootwright.core.private_file:
    content: '{{ bootwright_cluster_media_request | bootwright.core.installer_config(bootwright_cluster_media_material) }}'
    path: '{{ work }}/install-config.yaml'
  no_log: true
  diff: false
"""

RETURN = r"""
_value:
  description: The install configuration as installer YAML.
  type: str
"""


def bound(material, key):
    """One bound file's text as it is, or the empty text when the runner bound no such file."""
    if key not in material:
        return ""
    try:
        with open(material[key], "rb") as handle:
            return handle.read().decode("utf-8", "surrogateescape")
    except (OSError, TypeError, ValueError):
        raise AnsibleFilterError("installer_config: a bound material file could not be read") from None


def installer_config(request, material):
    """The frozen install configuration with every bound value substituted."""
    if not isinstance(request, dict) or not isinstance(material, dict) or "pullSecret" not in material:
        raise AnsibleFilterError("installer_config: the media request or its material is malformed")
    mirror_credential = bound(material, "mirrorUsername") if "mirrorUsername" in material else bound(material, "mirrorAuth")
    config = dict(request["installConfig"])
    config["pullSecret"] = registry_pull_secret(bound(material, "pullSecret").strip(), request.get("registryMirror") or {},
                                                mirror_credential, bound(material, "mirrorPassword"))
    config["sshKey"] = material["sshKey"]
    bundles = ["trustBundle%d" % index for index in range(len(request.get("trustBundleRefs") or []))]
    if not set(bundles) <= set(material):
        raise AnsibleFilterError("installer_config: a selected trust bundle is not bound")
    if bundles:
        config["additionalTrustBundle"] = "\n".join(bound(material, key).strip() for key in bundles)
    config = installer_proxy_credentials(config, request.get("installProxy") or {},
                                         bound(material, "proxyUsername"), bound(material, "proxyPassword"))
    return to_installer_yaml(config)


class FilterModule:
    """The install configuration writer used only by the media build."""

    def filters(self):
        return {"installer_config": installer_config}
