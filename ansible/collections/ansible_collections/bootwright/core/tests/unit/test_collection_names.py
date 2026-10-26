"""Role and action-plugin names follow the patterns specs/architecture.md states.

A role is named roles/<domain>_[<implementation>_]<capability>, with the domain
a playbook directory and the implementation, when there is one, directly after
it. An action plugin is named for the domain and implementation of the role
that invokes it, because it speaks that implementation's vocabulary. The names
that predate the patterns are exact allowances that only shrink: one that now
fits, or whose role or plugin is gone, fails until it is removed.
"""

from __future__ import annotations

import pathlib
import re

import pytest

COLLECTION = pathlib.Path(__file__).resolve().parents[2]
ROLES = COLLECTION / "roles"
ACTIONS = COLLECTION / "plugins" / "action"

# The domains a role may open with: the playbook directories a role is bound from.
DOMAINS = frozenset(("addons", "containercluster", "controller", "machine", "managedos", "storage", "substrate"))

# The implementations a role or plugin names; one belongs directly after its domain.
IMPLEMENTATIONS = frozenset(("anaconda", "baremetal", "ceph", "libvirt", "nginx", "redfish"))

SHAPE = re.compile(r"[a-z][a-z0-9]*(?:_[a-z0-9]+)+")

ROLE_MISFITS = {
    "cluster_objects": "opens with no domain",
    "infra_artifact_server_nginx": "opens with infra, not a domain, and names its implementation after the capability",
    "infra_managed_service": "opens with infra, not a domain",
    "machine_power_read_redfish": "names its implementation after the capability",
    "machine_power_redfish": "names its implementation after the capability",
    "managedos_install_anaconda": "names its implementation after the capability",
    "physical_erase": "opens with no domain",
}

PLUGIN_MISFITS = {
    "artifact_server_protocol": "invoked by a role that is itself a misfit",
    "ceph_cluster_protocol": "opens with its implementation and no domain",
    "ceph_export_protocol": "opens with its implementation and no domain",
    "ceph_image_protocol": "opens with its implementation and no domain",
    "ceph_replication_protocol": "opens with its implementation and no domain",
    "ceph_smb_protocol": "opens with its implementation and no domain",
    "cluster_objects_protocol": "invoked by a role that is itself a misfit",
    "cluster_objects_read": "invoked by a role that is itself a misfit",
    "cluster_objects_write": "invoked by a role that is itself a misfit",
    "infra_service_protocol": "invoked by a role that is itself a misfit",
    "machine_power_protocol": "invoked by roles that are themselves misfits",
    "managedos_install_protocol": "invoked by a role that is itself a misfit",
    "physical_erase_protocol": "invoked by a role that is itself a misfit",
    "substrate_host_protocol": "omits the libvirt implementation of the role that invokes it",
    "substrate_physical_protocol": "omits the baremetal implementation of the role that invokes it",
}


def role_misfit(name):
    """Why a role name breaks the pattern, or None."""
    tokens = name.split("_")
    if SHAPE.fullmatch(name) is None:
        return "is not lower-case words joined by underscores"
    if tokens[0] not in DOMAINS:
        return "opens with %r, which is no domain" % tokens[0]
    if any(token in IMPLEMENTATIONS for token in tokens[2:]):
        return "names its implementation after the capability"
    if tokens[1] in IMPLEMENTATIONS and len(tokens) < 3:
        return "names an implementation and no capability"
    return None


def owner_prefix(role):
    """The domain and implementation words a plugin invoked by this role opens with."""
    tokens = role.split("_")
    return "_".join(tokens[:2]) if tokens[1] in IMPLEMENTATIONS else tokens[0]


def plugin_misfit(plugin, invokers):
    """Why an action plugin's name breaks the pattern for the roles that invoke it, or None."""
    for role in invokers:
        if role_misfit(role) is not None:
            return "is invoked by %s, which breaks the role pattern" % role
        if not plugin.startswith(owner_prefix(role) + "_"):
            return "does not open with %s, the domain and implementation of %s" % (owner_prefix(role), role)
    return None


def roles():
    found = sorted(path.name for path in ROLES.iterdir() if path.is_dir())
    assert len(found) > 20, "the walk no longer sees the roles"
    return found


def invokers():
    """The roles whose task files invoke each action plugin by its FQCN."""
    plugins = sorted(path.stem for path in ACTIONS.glob("*.py"))
    assert len(plugins) > 15, "the walk no longer sees the action plugins"
    found = {plugin: set() for plugin in plugins}
    for task_file in ROLES.glob("*/tasks/*.yml"):
        text = task_file.read_text()
        for plugin in plugins:
            if re.search(r"bootwright\.core\.%s\b" % plugin, text):
                found[plugin].add(task_file.parents[1].name)
    return {plugin: sorted(roles) for plugin, roles in found.items()}


@pytest.mark.parametrize("role", roles())
def test_a_role_follows_the_pattern_or_is_an_allowed_misfit(role):
    reason = role_misfit(role)
    if role in ROLE_MISFITS:
        assert reason is not None, "%s now follows the pattern; remove it from ROLE_MISFITS" % role
    else:
        assert reason is None, "role %s %s (roles/<domain>_[<implementation>_]<capability>)" % (role, reason)


@pytest.mark.parametrize("plugin,invoking", sorted(invokers().items()))
def test_an_action_plugin_is_named_for_the_role_that_invokes_it_or_is_an_allowed_misfit(plugin, invoking):
    assert invoking, "%s is invoked by no role" % plugin
    reason = plugin_misfit(plugin, invoking)
    if plugin in PLUGIN_MISFITS:
        assert reason is not None, "%s now follows the pattern; remove it from PLUGIN_MISFITS" % plugin
    else:
        assert reason is None, "action plugin %s %s" % (plugin, reason)


def test_the_allowances_name_only_what_exists():
    assert set(ROLE_MISFITS) <= set(roles())
    assert set(PLUGIN_MISFITS) <= set(invokers())


def test_the_pattern_refuses_a_seeded_misfit():
    assert role_misfit("substrate_libvirt_machine") is None and role_misfit("storage_ceph_core") is None
    assert role_misfit("managedos_package_tree") is None and role_misfit("controller_prerequisites") is None
    assert role_misfit("managedos_install_anaconda") == "names its implementation after the capability"
    assert "no domain" in role_misfit("cluster_objects") and role_misfit("Storage_Ceph") and role_misfit("storage")
    assert role_misfit("storage_ceph") == "names an implementation and no capability"
    assert plugin_misfit("substrate_libvirt_machine_protocol", ["substrate_libvirt_machine"]) is None
    assert plugin_misfit("substrate_machine_protocol", ["substrate_libvirt_machine"]).startswith("does not open with substrate_libvirt")
    assert plugin_misfit("controller_protocol", ["controller_prerequisites"]) is None
    assert "breaks the role pattern" in plugin_misfit("cluster_objects_read", ["cluster_objects"])
