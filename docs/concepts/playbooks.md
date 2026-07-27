---
title: Provisioning playbooks
description: Run operator-supplied Ansible playbooks (with vendored roles and collections) against machines at a chosen provisioning stage — before or after the built-in work.
---

# Provisioning playbooks

A `Playbook` runs an **operator-supplied Ansible playbook** against
machines at a chosen provisioning stage. It is the imperative escape hatch for
site-specific steps bootwright does not model — hardening a node after OS
install, preparing storage before cluster dependencies land, registering nodes
with an external system after the cluster is up, or
[replacing Bootwright's managed RHSM registration](#delegating-rhsm-registration)
of storage nodes.

It is the sibling of an [add-on](add-ons.md): an add-on applies **declarative
Kubernetes objects** *inside* an installed cluster; a provisioning playbook runs
**imperative Ansible** against machines at *any* stage — including before the
cluster exists. Both are desired-state objects, driven by the normal `apply`
flow; there is no dedicated CLI verb.

## When it runs

Each playbook anchors to one of the five provisioning sub-phases — the same
vocabulary as [`--stage`](../advanced/operations.md) — with a `timing`:

| Stage | `after` runs once… | `before` runs just before… |
| --- | --- | --- |
| `fabric` | provider hosts + shared services are up | any fabric work |
| `machines` | machine OS install / instantiation completes | machine work starts |
| `deps` | per-cluster prerequisites are in place | dependency work starts |
| `base` | the cluster control plane has converged | control-plane bring-up |
| `add-ons` | post-install add-ons have applied | add-on work starts |

The three common cases map directly:

- **after OS install** → `stage: machines, timing: after`
- **before installing cluster deps** → `stage: deps, timing: before`
- **after installing clusters** → `stage: base, timing: after`

`timing` defaults to `after` when omitted. `after` waits for the stage's built-in
work; `before` gates it (the stage waits for the playbook). A playbook runs
during any `apply` whose `--stage` includes
its stage and whose `--clusters` scope includes its target, so
`apply --stage base --clusters prod` re-runs exactly the base-stage playbooks for
`prod`.

## Authoring

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Playbook
metadata:
  name: harden-storage-nodes
spec:
  follows: machines
  target:
    clusters: [nprd-ceph]     # a StorageCluster → its ceph nodes
  playbook: playbooks/harden.yml
  rolesPath: roles            # optional vendored roles directory
  collectionsPath: collections # optional vendored collections tree
  tags: [tuning]              # optional --tags
  skipTags: [reboot]          # optional --skip-tags
  extraVars:
    tuned_profile: throughput-performance
  secretRefs: [vault-token]
  run: onChange
  onFailure: fail
```

## External Ansible content

By default the Ansible content sits beside the object and travels with the
context snapshot. To point at content you maintain elsewhere — a checkout you
already manage, shared by several environments — set `spec.source.path` to an
absolute directory:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Playbook
metadata:
  name: os-hardening
spec:
  follows: machines
  source:
    path: /srv/ansible/os-hardening   # absolute, outside the input tree
  playbook: playbooks/site.yml        # relative to the source directory
  rolesPath: roles
  target:
    hostGroups: [bootwright_infra_hosts]
```

`playbook`, `rolesPath`, and `collectionsPath` then resolve against that
directory, and the `playbooks/` layout rule below does not apply — the loader
never walks an external directory, so there is nothing for it to mis-parse.
Paths must still stay inside the source directory.

### From a git repository

`spec.source.git` fetches the content instead. `url` accepts `https`, `ssh`, a
`file://` URL, or an absolute path to a local repository; `ref` is a commit, tag,
or branch; `subdir` optionally selects a directory inside the repository.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Playbook
metadata:
  name: os-hardening
spec:
  follows: machines
  source:
    git:
      url: ssh://git@git.corp.example/infra/os-hardening.git
      ref: v1.4.0
      subdir: bootwright
      secretRef: git-deploy-key
  playbook: playbooks/site.yml
  rolesPath: roles
  target:
    hostGroups: [bootwright_infra_hosts]
```

The fetch runs once per resolved commit, into the run directory, with
`core.hooksPath=/dev/null`, `protocol.ext.allow=never`, and
`--no-recurse-submodules`, so a fetched repository cannot execute code during
the fetch. Only `apply` fetches — `plan`, `diff`, and `destroy` never open a
socket, and simply omit git-sourced playbooks from their view.

**Authentication** is explicit, never inherited from the operator's ssh-agent or
`~/.gitconfig`. `secretRef` names a `Secret` whose type must match the
transport: `sshKeyPair` for an `ssh` url, `token` or `usernamePassword` for an
`https` url. Credentials reach `git` through a temporary `GIT_ASKPASS` helper or
`GIT_SSH_COMMAND`, never on the command line. A local repository needs no
secret.

!!! tip "Air-gapped sites"
    A local repository — `url: /srv/git/os-hardening.git` — resolves and checks
    out entirely offline while still pinning a tag or commit. Mirror upstream to
    the controller and `apply` never needs the network.

!!! warning "A branch ref moves"
    `ref: main` is allowed, but `run: onChange` digests the *fetched content*, so
    the playbook re-runs whenever the branch advances — with nothing changing in
    your input files. Pin a tag or commit when you want re-runs to be driven by
    your own edits.

A git source is not available on `ClusterAddon.spec.steps[]`: a step's content
ships with its add-on package. Use `source.path` there, or move the work to a
`Playbook`.

!!! warning "External content is not snapshotted"
    `context init`/`update` copies the input tree, not `spec.source.path`. The
    directory must exist and be readable on the controller at apply time, and
    `run: onChange` digests whatever is there when the run is planned. Content
    that must travel with the context belongs beside the object instead.

The same `source` block is available on `ClusterAddon.spec.steps[]`.

On disk, an object without `source` sits beside its Ansible content:

```text
input/
  playbooks-custom/
    harden-storage-nodes.yaml      # the Playbook object
    playbooks/harden.yml           # the entry playbook
    roles/<role>/tasks/main.yml    # optional vendored roles
    collections/ansible_collections/<ns>/<name>/...  # optional vendored collections
```

`playbook`, `rolesPath`, and `collectionsPath` are paths **relative to the object
file** and must stay within its directory (no absolute paths, `..`, or symlinks).
The loader treats `playbooks/`, `roles/`, and `collections/` directories as
Ansible content, not authored objects, and skips them — but `bootwright context
init`/`update` copies the whole input tree, so `ansible-playbook` finds them at
run time. Vendored roles/collections are the **air-gap-safe** way to ship
dependencies; a Galaxy `requirements.yml` install (which needs network) is not
supported.

`spec.enabled` defaults to `true`. Set `enabled: false` to keep the playbook in
desired state — it still loads and validates — while skipping it: Bootwright
plans no run for it and drops it from `provides`/`requires` ordering. It is the
declarative way to park a playbook without deleting the object.

## Targeting

`spec.target` selects the inventory hosts and needs at least one of:

- **`clusters`** — a `ContainerCluster` resolves to its agent-node group, a
  `StorageCluster` to its ceph node group.
- **`machines`** — a `Machine` resolves to its node inventory host(s).
- **`hostGroups`** — raw inventory group names, for anything the two above do not
  cover.

A playbook may **not** target the bootwright controller / localhost
(`localhost`, `bootwright_ocp_hosts`, `bootwright_controller_hosts`): it would run
operator code as root over every context's secrets. Secrets named in
`secretRefs` are read by the playbook from `{{ bootwright_secrets_dir }}/<name>`;
their values never reach the command line. `extraVars` arrive as a single JSON
`-e` value.

## Selecting work with tags

`spec.tags` and `spec.skipTags` are the `--tags` and `--skip-tags` you would pass
by hand, so one playbook can serve several anchors without being split up:

```yaml
spec:
  playbook: playbooks/site.yml
  tags: [base, tuning]
  skipTags: [reboot, teardown]
```

Each entry is a single token — Bootwright joins a list with commas, the way
`ansible-playbook` expects — and the same tag may not appear in both lists,
which would select and deselect the same tasks. Both lists feed the `onChange`
input hash, so narrowing or widening the selection re-runs the playbook rather
than reporting it converged.

## Delegating RHSM registration

A subscription-backed managed Ceph cluster is normally registered by
Bootwright's own machines-phase `registration.<cluster>` task — after the OS is
in place, before the deps-phase storage work. Setting `rhsm.management:
external` on the cluster's `Entitlement` delegates that work to a provisioning
playbook: Bootwright plans no registration task, never touches `rhsm.conf`, and
skips the repo-enablement purge, so operator-managed repo sets survive — and no
RHSM organization or activation-key secrets exist or are demanded.

Anchor the delegated playbook at `stage: deps, timing: before`: it runs after
the machines-phase work (the OS is in place) and, with the default
`failureMode: fail`, the deps-phase Ceph work waits for and gates on it. A
`stage: machines, timing: after` playbook also runs after the OS install but
does **not** gate later phases, so do not use it for delegated registration.
The playbook must leave every storage node able to install the distribution
packages — activation-key repo sets, a Satellite content view, or an internal
mirror — because the cephadm install assert remains the fail-closed
package-availability gate. Ceph client commands run through `cephadm shell`.
See the
[`examples/ceph-external-rhsm`](https://github.com/crmarques/bootwright/tree/main/examples/ceph-external-rhsm)
snippet and the `Entitlement` section of
[`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md)
for the normative rules.

## Idempotency, failure, and ordering

- **`run: onChange`** (default) skips a playbook whose declared inputs — its spec
  plus a content digest of the playbook and vendored trees — are unchanged since
  the last reconcile. **`run: always`** re-runs it every apply. Because a
  playbook is opaque, `bootwright diff` reports it as *match* (inputs unchanged) or
  *drift* (changed, will re-run) from this input hash only — it never observes
  what the playbook did on the node.
- **`failureMode: fail`** (default) blocks the anchor stage when the playbook
  fails; **`continue`** records the failure and lets the stage proceed.
- Several playbooks in the same `(stage, timing)` bucket run concurrently unless
  ordered by `spec.order` (a tie-break) or `spec.provides`/`spec.requires`
  (capability edges within the bucket, like add-on capabilities).

## Relationship to add-ons

| | Add-on | Provisioning playbook |
| --- | --- | --- |
| Payload | Declarative Kubernetes objects (`olm` / `manifestSet`) | Imperative Ansible playbook |
| Runs via | `oc` against the cluster API | `ansible-playbook` against machines |
| When | Only after the cluster is installed | Any of the five stages, before or after |
| Drift | Reconciled from applied objects | Opaque; input-hash only |

Use an add-on when the target is a Kubernetes object in an installed cluster; use
a provisioning playbook when the target is a machine, at a stage an add-on cannot
reach.
