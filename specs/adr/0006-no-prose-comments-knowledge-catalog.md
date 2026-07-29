# ADR 0006: Source Knowledge Lives in the Indexed Catalog, Not Comments

## Status

Accepted

## Context

The codebase had accumulated roughly 14,000 prose comment lines across Go,
Ansible YAML, examples, templates, and build files: API field documentation,
design rationale, incident workarounds, vendor quirks, and test narration.
That knowledge was invisible to anyone not reading the exact line, drifted
from the code it annotated, duplicated (or contradicted) specs and docs, and
could not be consulted by symptom when something failed. The repository
already had the right destinations — `specs/` for contracts, `specs/adr/`
for decisions, `docs/` for workflows, `.agents/knowledge/` for
symptom-indexed incident and constraint knowledge — and a code-quality skill
that discouraged comments, but the rule allowed exceptions and was not
enforced, so the comment stock kept growing.

## Decision

Source files carry no prose comments. The only comments allowed are
machine-read directives — build and codegen pragmas (`//go:build`,
`//go:embed`), linter and analyzer directives (`//nolint`, `# noqa`,
`# yamllint`, `# ansible-lint`, `# nosec`), build-file directives (`# syntax=`,
`# shellcheck`), and shebangs. The allowlist that actually decides is the regex
set in `internal/repo/checks/comment_policy_test.go` (`goCommentAllow`,
`yamlCommentAllow`, `buildFileCommentAllow`), which admits a different set per
file type; this ADR is a reading of that test, not a second source.

Comment-like lines that are content rather than commentary stay: `#` lines
inside Jinja2 templates render into shipped artifacts (kickstart, squid.conf,
resolved.conf drop-ins, applied manifests), and `#` lines emitted from Go
string literals into generated scripts are data. Only `{# ... #}` Jinja
comments are true comments inside templates.

Every kind of information a comment used to carry has one designated home,
written in the same change that would have added the comment:

- Incident, root-cause, vendor-quirk, and constraint knowledge →
  `.agents/knowledge/` with an index row in `.agents/knowledge/KNOWLEDGE.md`.
- Design decisions with durable rationale → `specs/adr/`.
- Schema and field semantics → `specs/state-model.md` and `docs/concepts/`.
- Rendered-variable contracts → the collection's `docs/vars-contract.md`.

The existing comment stock was migrated accordingly and then deleted. The
policy is enforced by guard tests in
`internal/repo/checks/comment_policy_test.go`, which parse Go sources
(go/parser) and YAML (comment-aware nodes) so string literals containing
`//` or `#` are never false positives.

## Consequences

- `make check-fast` fails on any new prose comment in Go, tracked YAML
  (ansible, examples, test/e2e, add-ons, workflows), Makefile, Containerfile,
  or shell.
- Knowledge is discoverable by symptom or subject through the indexes, and
  agents consult it on failures instead of rediscovering root causes
  (`AGENTS.md` "Knowledge Lookup").
- `doc.go` files whose only content was a package comment were deleted;
  packages carry no godoc. The cost falls hardest on `api/v1alpha1`, whose
  types are the product's public contract: `go doc` and IDE hovers show no
  field reference, and `specs/state-model.md` plus `docs/concepts/` carry that
  contract instead (see [ADR 0014](0014-api-grammar.md)). Cobra help text,
  being string literals, is unaffected.
- Stripping changed file bytes of add-on step content and operator
  provisioning playbooks, so `run: onChange` re-runs once on existing
  contexts after upgrading past the migration.
