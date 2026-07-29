# ADR 0026: The Operator-Supplied Playbook Kind Is `CustomPlaybook`

## Status

Accepted (implemented)

## Context

ADR 0005 introduced the kind `Playbook` for an operator-supplied Ansible
playbook injected into the provisioning DAG. The name has aged badly, because
"playbook" is the single most overloaded word in the system.

Bootwright's own runtime is Ansible (ADR 0002). The embedded collection ships
dozens of playbooks; `ClusterAddon.spec.steps[].playbook` names one;
`CustomPlaybook.spec.playbook` names one; the runner's options carry a
`Playbook` field; a run's task labels mention playbooks. In that vocabulary, a
kind called `Playbook` reads as "the playbook object" rather than as "the object
that carries *your* playbook", which is what it actually is. Operators reading
`kind: Playbook` reasonably asked whether it declared one of Bootwright's own
playbooks, or overrode one.

Every other kind in the API names the thing the operator owns, not the mechanism
Bootwright uses to deliver it — `ClusterAddon`, `MachineInstallProfile`,
`NetworkConfig`. `Playbook` was the only kind named after the implementation.

The user-facing vocabulary had already drifted to the right word: the apply run
frame groups these tasks under "Custom playbooks", and the concept docs called
them the operator's own content throughout.

## Decision

The kind is `CustomPlaybook`. `Custom` marks whose content it carries and
distinguishes it from the built-in playbooks the runtime executes, which no kind
declares.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: CustomPlaybook
metadata:
  name: harden-storage-nodes
spec:
  follows: machines
  playbook: playbooks/harden.yml
```

The rename is a pure renaming. Every field, anchor, ordering rule, source arm,
targeting rule, and apply/destroy behaviour ADR 0005 and ADR 0021 describe is
unchanged.

Scope of the rename:

- `kind: Playbook` becomes `kind: CustomPlaybook`.
- `State.playbooks` becomes `State.customPlaybooks`, and the `validate --output
  json` count `provisioningPlaybooks` becomes `customPlaybooks`.
- Validation messages are prefixed `CustomPlaybook/<name>`.
- `PlaybookSource` and the `run`/`onFailure` vocabulary keep their names: they
  are shared with `ClusterAddon.spec.steps[]` and do not belong to this kind.
- `spec.playbook`, `ClusterAddonStep.playbook`, and the runner's internal
  `Playbook` fields keep their names: they name an Ansible playbook file, which
  is exactly what they are.

There is no compatibility alias. `kind: Playbook` is rejected as an unknown
kind, consistent with how every prior schema rename landed.

## Consequences

- Breaking for inputs that declare `kind: Playbook`. The migration is one
  mechanical edit per file, and the loader's unknown-kind error names the
  offending document.
- Breaking for anything parsing `validate --output json`: the count key moved from
  `provisioningPlaybooks` to `customPlaybooks`.
- The API now has one word for the operator's Ansible content and a different
  one for the runtime's, so `spec.playbook` inside a `CustomPlaybook` no longer
  reads as a stutter.
- ADR 0005 and ADR 0021 stand as written; only the kind's name changes.
