# ADR 0005: Operator-Supplied Provisioning Playbooks

## Status

Accepted

## Context

Bootwright provisions infrastructure by running a fixed, embedded set of
`bootwright.core` Ansible roles through a capability DAG. Operators periodically
need site-specific automation bootwright does not model — harden a node after OS
install, prepare storage before cluster dependencies land, register nodes with an
external system after a cluster is up. Until now there was no supported extension
point for such steps at provisioning stages: `ClusterAddon` runs only *after* a
cluster is installed and applies Kubernetes objects through `oc`, not Ansible
against machines.

## Decision

Introduce a declarative kind, `ProvisioningPlaybook`, that runs one
operator-supplied Ansible playbook (with optional vendored roles/collections)
against machines, anchored to one of the five provisioning sub-phases
(`fabric`, `machines`, `deps`, `base`, `add-ons`) with `before`/`after` timing.
It is authored as desired-state YAML and executed by the normal `apply` flow
through the existing scheduler and `ansible.CommandRunner`; there is no new
imperative CLI verb. Playbooks flow through `apply`/`plan`/`validate`/`state-check`
on the existing `--stage`/`--clusters` axes.

Three cross-cutting decisions are recorded here.

### Security posture

A playbook runs arbitrary operator code, as root via `become`, on the targeted
machines. To bound blast radius:

- Targets may not name the bootwright controller / localhost (`localhost`,
  `bootwright_ocp_hosts`, `bootwright_controller_hosts`). A controller-targeted
  playbook would run as root over every context's decrypted secrets and
  kubeconfigs. This is a hard validation error.
- The playbook and vendored directories are relative paths contained within the
  object's file directory (no absolute paths, `..`, or symlinks) — the
  `ClusterAddon` `manifestSet.path` rules.
- `secretRefs` resolve against declared `Secret` objects and are read by the
  playbook from `{{ bootwright_secrets_dir }}/<name>`; secret values never reach
  the command line.

### Roles/collections delivery and the runner path change

Vendored roles/collections trees are the only supported delivery: they are
air-gap-safe and travel with the input tree through `context init`'s copy. A
Galaxy `requirements.yml` install (which needs network and violates the
disconnected-mirror posture) is deferred. To carry the operator dirs, the ansible
runner's `RolesPath`/`CollectionsPath` become `os.PathListSeparator`-joined path
lists (each element cleaned individually, so a single path is unchanged): the
bundle collections stay first so `bootwright.core` resolves, and the operator
dirs append. The loader skips `playbooks/`, `roles/`, and `collections/`
directories (Ansible content, frequently sequence-shaped and unparseable as
bootwright objects) while `context init` still copies them.

### DAG anchoring and scoping

A playbook lowers into the capability DAG as an ordinary `ApplyTask`. An `after`
playbook waits for its stage's core tasks in scope; a `before` playbook gates
every core task of its stage in scope (a hard dependency, or a soft ordering
dependency when `failureMode: continue`) and itself waits for the previous
stage's tasks. It is planned only when its stage is in the run's phase set and its
target resolves to at least one in-scope host; an unresolved target is skipped
rather than run fleet-wide (an empty `--limit` would target every host).

## Consequences

- Playbooks are opaque: `state-check` classifies them as match/drift from a
  content-and-spec input hash only (`run: onChange` skips an unchanged run;
  `run: always` re-runs every apply). It cannot observe what a playbook did on a
  node. This is the honest contract, documented as such.
- The five sub-phase names are shared vocabulary: `api/v1alpha1.ProvisioningStages()`
  is the single source of truth, pinned to `internal/converge.SubPhaseStageNames()`
  by a guard test (the leaf API package cannot import converge).

### Deliberately not done

- Galaxy `requirements.yml` network install.
- A `ClusterAddonProfile`/`Binding`-style reuse trio (a playbook is self-contained
  with its own target and inputs).
- A `--playbook <name>` single-object CLI filter or an imperative `run-playbook`
  break-glass verb (`--stage`/`--clusters` suffice).
- Readiness checks (a playbook's readiness is its own exit code).
