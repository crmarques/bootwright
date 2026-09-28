# Custom playbooks

`CustomPlaybook` retains a reserved, non-executable declaration shape for
operator-supplied Ansible. The closed schema in
`api/v1alpha1/customplaybooks.go` is authoritative for its fields, and the
[custom playbook detail](../deferred/custom-playbooks.md) records their lexical
rules and the reserved ordering graph that [B102](../milestones/backlog.md#b102) revives.
[The compiler boundary](../api.md#compiler-boundary) applies to enabled and
disabled objects alike: both are strictly decoded, normalized,
reference-checked and rendered. The retired `run` and `onFailure` fields are
unknown and rejected.

## Reserved-shape contract

Admission checks source, path, reference, tag and Secret-type spelling
lexically; it never resolves a source, fetches Git content, reads a playbook or
reads credential bytes. Cluster and Machine targets resolve in the complete
graph, and the controller is never a target: its inventory identities are
forbidden host groups. `extraVars` is the kind's one open value map and rejects
connection, inventory, interpreter, SSH, privilege and Bootwright-reserved
names; inline secret bytes are never accepted.

Normalization materializes `enabled: true` when omitted. Enablement affects
only declaration ordering at the compiler boundary: enabled declarations at one
anchor form a valid `order` and `provides`/`requires` graph, and disabled
declarations enter none. It grants no execution.

## Lifecycle refusal

An enabled `CustomPlaybook` is an object no capability realizes, so a fresh
`plan` and a fresh `apply` refuse it under the
[unrealizable-kind rule](../state-reconciliation.md#stages-and-the-pause-boundary)
before content access or operation registration, with no authorization bypass.
A disabled declaration plans no work.

Executable custom automation requires a separately authorized replacement
schema that meets every prerequisite of the
[custom-code boundary](../security.md#custom-code).
