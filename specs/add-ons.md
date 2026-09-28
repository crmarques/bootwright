# Add-ons

An add-on is a declarative extension bound to a `ContainerCluster`.
[The add-on API](api/addons.md) owns admission of `ClusterAddon`,
`ClusterAddonProfile` and `ClusterAddonBinding`; this page owns the boundary
those declarations cross. No add-on lifecycle exists: no capability realizes an
add-on kind, so a selection containing one refuses before registration under
the [unrealizable-kind rule](state-reconciliation.md#stages-and-the-pause-boundary).
Accepting a declaration claims no support.

## Trust boundary

An add-on package is declarative data under the
[custom-code rule](security.md#custom-code). Authors cannot select executable
code, drivers, inventory, privileges, retry policy or lifecycle order. A step that names a playbook, roles or collections is
admitted as inert data and makes its package lifecycle-ineligible regardless of
origin. The [compiler boundary](api.md#compiler-boundary) applies to
declarations and provenance markers.

Nothing is discovered ambiently. Working directories, `PATH`, environment
variables, user configuration, Ansible paths, network registries and global
plugin locations contribute no package, and desired state and package fields
cannot select a hidden catalog. The Environment's `.bootwright-addon` marker
grants only descriptor selection under
[Environment selection](api/environment.md#resource-and-cluster-selection); it
proves no signature, package version, manifest, compatibility or execution
authority.

Add-ons consume the published capabilities of Container cluster, Storage,
Machine and Secrets; they cannot bypass those invariants or invoke another
add-on as a workflow. `CustomPlaybook` is a separate
[reserved, non-executable surface](api/custom-playbooks.md).

## Design

The package standard, catalogs, versioned host interface, closed execution
vocabulary, ordering, GitOps handoff, qualification suite and step semantics
are recorded in the [add-ons design](deferred/add-ons-design.md), which
[B83](milestones/m5.md#b83), [B89](milestones/m7.md#b89) and
[B90](milestones/m7.md#b90) revive.
