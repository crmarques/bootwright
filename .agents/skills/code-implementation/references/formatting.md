# Source Formatting

This reference owns layout for tracked, human-authored Go, YAML, Ansible, YAML
front matter, and matching inline examples. Product contracts own semantics.
Generated, vendored, locked, encrypted, and byte-exact fixture content follows
its generator or byte contract; correct generated layout through the generator.
[Effective YAML](../../../../specs/api.md#defaults-normalization-and-effective-state)
has its own canonical output rules.

A format-only edit preserves parsed syntax, scalar types and values, comments,
and deliberate key/declaration order. Never alter whitespace inside YAML
literal or folded scalars for appearance: indentation, blank lines, chomping,
and the final newline affect values. Change chomping only with parsed
before/after equality proof. YAML front matter stays one compact metadata unit.

## Shared whitespace

Use LF, no BOM or trailing whitespace, and one final newline. Separate logical
sibling units with one empty line, with no consecutive empty lines outside
semantic scalar content. Keep comments attached to their subjects; separate
standalone section comments from the preceding unit. Prefer structure over
decorative dividers or manual alignment. Canonical formatters take precedence.
Use the pinned language linter's width rule, if any, without inventing a
universal column limit.

## Go

Use the repository-selected `gofmt`. Group standard-library imports before
non-standard imports. Separate package, imports, and distinct top-level
declarations by one empty line; grouped declarations are one unit. Keep
documentation, directives, and package comments attached as Go tooling requires.
Inside functions, separate meaningful phases without adding blanks at the body
edges or around every control statement. Short single-purpose bodies stay
contiguous. Use `goimports` only through a reviewed, pinned repository entrypoint.

## YAML

Use two-space indentation, block mappings/sequences unless an empty or atomic
flow value is clearer, lowercase `true`/`false`/`null`, and quoting when plain style
would change a scalar's intended type.

- Keep the Kubernetes-style identity envelope contiguous: `apiVersion`, `kind`,
  and `metadata` have no intervening blank lines. Separate `spec` and subsequent
  top-level mapping or sequence sections from that envelope.
- Among direct `spec` children, each scalar/empty-collection run is one block;
  each non-empty mapping or sequence is its own block. Separate these blocks.
- Keep nested compact records contiguous. Separate schema-defined sections,
  adjacent multiline sequence records, and multiline named mapping records;
  keep scalar sequences and compact scalar maps together.
- In multi-document streams, separate each later `---` from the previous document
  with one empty line, attached to the following document. Generic single
  documents do not require a leading marker; preserve owning format policy.
- Choose literal or folded multiline scalars for their required value, not
  appearance. Scalar preservation overrides separator rules.

## Ansible

Inherit YAML rules and start human-authored playbooks with `---` attached to the
first play. Put `name` first in plays, tasks, handlers, and named blocks. Keep
play header directives together; separate `vars`, `pre_tasks`, `roles`, `tasks`,
`post_tasks`, and `handlers`. Separate sibling plays/tasks/handlers/blocks, keeping
the first item attached to its parent key. Keep each task's action, arguments,
conditions, registration, privilege, and other directives contiguous. Put
`block`, `rescue`, and `always` last in that order, separated by one empty line.
Variable and metadata files follow generic YAML grouping.

## Verification

Use repository-pinned checks where available; report unavailable gates.

- Require no `gofmt -l` output for Go files and extracted complete Go examples.
- Parse every YAML document; use a front-matter-aware check for metadata.
  Run the pinned YAML linter for indentation, duplicate keys, whitespace, and
  final newline. If none exists, use the available parser/linter and manually
  review grouping rather than claiming a pinned gate passed.
- Apply [Ansible syntax and lint gates](ansible.md#tests-and-gates) to Ansible
  content. Check tagged examples in their declared language; use the strongest
  applicable parse/format check for partial or deliberately invalid examples
  and compare them with the surrounding contract.
- For format-only edits, compare parsed before/after content and review the
  full diff. Parsers and linters do not prove schema-owned blank-line placement;
  review it manually unless an appropriate structural check exists.
