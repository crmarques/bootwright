# Validator diagnostics and CLI presentation

**Boundary: presentation shaping lives in the CLI, not the validator.**
`desiredstate.ValidationError` returns plain messages and the CLI
(`internal/cli/diagnostics.go`) reconstructs the routable Diagnostic
fields (Object/Field/Value/Rule/Remediation), keeping Message as the
exact validator text. When a Finding carries structured Object/Field
they take precedence over message reparsing. Remediation heuristic: a
message containing "only supported", "must be empty", or
"is not supported" describes a conditionally-unsupported or
required-empty field where no value is valid, so the fix is REMOVAL (or
changing the gating mode), never "set it to a valid value".

**Semantics: Finding is the structured diagnostic carrier.** A Finding
names the owning object and field (and offending value) at the point the
rule fires so the CLI routes Object/Field/Value diagnostics for human
and JSON output without re-parsing message text; `note`/`notes` are the
legacy unstructured adapters so validator families can migrate to
structured findings one at a time.

**Constraint: the decode did-you-mean is appended, never rewritten.**
`rewriteKnownFieldError` enriches yaml.v3 KnownFields decode errors
(`line N: field <name> not found in type <GoType>`) with a suggestion
derived from the struct's yaml tags. The ORIGINAL error wording must be
preserved because many validators/tests pin it as the reject contract —
the suggestion is appended, and errors with no close match pass through
untouched. The Levenshtein threshold is distance within one third of the
field length, min 1, capped at 3 ("a typo is a handful of edits, not a
rename") so unrelated keys get no misleading suggestion.

**Semantics: normalize-injected secret refs carry a defaulted note.**
In `validateSecretReferences`, refs injected by normalization
(Environment defaults, or invented convention names like the derived
`<cluster>-cluster-admin-ssh-key` when `install.nodeSSH` is omitted)
appear nowhere in the author's files, so their dangling-reference
diagnostics carry a `requireNoted` note saying the value was defaulted
and how to override it — e.g. renaming a cluster re-derives the secret
name — because blaming a field the author never wrote would be
misleading. `requireTypeNoted` additionally checks the referenced
Secret's declared type matches the consuming field.

**Constraint: scaffolds must not imply apply support.**
`scaffold.ApplySupport` maps each scaffold provider to the dispatch
support status users hit after `bootwright apply`: schema-only scaffolds
remain valid authoring examples, but the CLI must not imply their role
bundle actually converges.
