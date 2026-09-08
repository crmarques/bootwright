# Knowledge Index

Knowledge records observed implementation facts and diagnosed failures, not
product requirements. Specs win when the two differ. Search this table before
investigating or designing, then open only the relevant page.

| Area or symptom | Page |
| --- | --- |
| Desired-state API, `v1alpha1`, 27-to-21-kind alignment, superseded schema proposal | [api-alignment.md](api-alignment.md) |
| Admission parser composition, YAML directives/tags, exact integers, effective inspection, offline vulnerability database | [desired-state-admission.md](desired-state-admission.md) |
| CLI, Cobra, completion effects, `BASH_COMP_DEBUG_FILE`, `ExecuteContext`, local `--output`, nil/empty cluster selection, Zsh `compadd`, PowerShell `-File` | [cli-adapter-constraints.md](cli-adapter-constraints.md) |
| Go toolchain pin, `GOTOOLCHAIN`, `govulncheck`, release version metadata, `-ldflags -X` | [build-toolchain.md](build-toolchain.md) |
| OpenShift agent installer, bare-metal disk identity, pre-wipe proof, Redfish-to-installer race | [openshift-agent-disk-safety.md](openshift-agent-disk-safety.md) |

Add one focused page for each durable lesson and index it with searchable topic
terms, affected symbols, or error text. Each new or revised entry identifies the
observation, the decision it informs, its applicable implementation or version,
and links to the affected code and supporting tests or other evidence. Link to
the owning spec for required behavior rather than restating it. Keep paths and
evidence current; correct or retire obsolete findings when their basis changes.

Use the [code clarity contract](../../specs/architecture.md#self-explanatory-code-and-retained-knowledge)
when moving useful explanations out of comments. Redundant narration needs no
catalog entry. Put unfinished implementation work under the earliest fitting
future milestone in [milestones](../../specs/milestones.md).
