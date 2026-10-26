# Source Formatting

This reference owns layout for tracked, human-authored Go, YAML, Ansible,
Python, shell, YAML front matter, and matching inline examples. Product contracts own semantics.
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

Follow [YAML community best practices](https://www.yaml.info/learn/bestpractices.html):
two spaces per indentation level, no tabs, and quotes where a plain value would
have the wrong type. This repository chooses indented sequence markers, block
mappings/sequences unless an empty or atomic flow value is clearer, and
lowercase `true`/`false`/`null`. Preserve deliberate string quotes and key order.
The examples [yamllint configuration](../../../../examples/.yamllint), using the
[community rules](https://yamllint.readthedocs.io/en/stable/rules.html), checks
indentation, duplicate keys, boolean spellings and whitespace. Long immutable
references are not wrapped to an arbitrary column limit.

Blank-line grouping is the repository's readability convention:

- Keep the Kubernetes-style identity envelope contiguous: `apiVersion`, `kind`,
  and `metadata` have no intervening blank lines. Separate `spec` and subsequent
  top-level mapping or sequence sections from that envelope.
- At every mapping level, consecutive scalar or empty-collection fields form
  one compact block. Each non-empty block mapping or sequence forms its own
  block. Separate adjacent blocks with exactly one empty line; keep the first
  child attached to its parent and add no blank line after the last child.
- Separate adjacent multiline sequence records with one empty line. Keep
  scalar sequences, single-line records and each compact scalar map contiguous.
  Apply the same rules to native manifests and nested Kubernetes objects,
  retaining each object's contiguous identity envelope.
- In multi-document streams, separate each later `---` from the previous document
  with one empty line, attached to the following document. Generic single
  documents do not require a leading marker; preserve owning format policy.
- Choose literal or folded multiline scalars for their required value, not
  appearance. Preserve multiline scalar and flow-collection content, plus
  inline and trailing comment ownership. Scalar preservation overrides
  separator rules; never add cosmetic blank lines inside string content.

For example, scalar identity and endpoint fields stay compact while structured
sections and multiline endpoint records are separated:

```yaml
apiVersion: bootwright.io/v1alpha1
kind: Proxy
metadata:
  name: example-proxy

spec:
  management: managed
  machineRef: controller
  implementation: squid
  bindAddress: 192.0.2.1
  port: 3128

  endpoints:
    - name: ip
      addressRef: ip

    - name: fqdn
      addressRef: fqdn
```

## Ansible

Inherit YAML rules and start human-authored playbooks with `---` attached to the
first play. Put `name` first in plays, tasks, handlers, and named blocks. Keep
play header directives together; separate `vars`, `pre_tasks`, `roles`, `tasks`,
`post_tasks`, and `handlers`. Separate sibling plays/tasks/handlers/blocks, keeping
the first item attached to its parent key. Keep each task's action, arguments,
conditions, registration, privilege, and other directives contiguous. Put
`block`, `rescue`, and `always` last in that order, separated by one empty line.
Variable and metadata files follow generic YAML grouping.

## Python

Follow PEP 8 as the collection's `ansible-test` sanity suite applies it. After a
module's interpreter line and the file's docstring, if any, comes
`from __future__ import annotations`; group standard-library imports before
Ansible and collection imports. Separate top-level definitions by two empty lines and methods by one.
Keep a function's phases visible without blanks at the body edges. Placement,
interpreter and import rules are in the [Python reference](python.md).

## Shell

Write POSIX `sh` with `#!/bin/sh` and `set -eu` where the script's own control
flow allows it, indented by four spaces. Quote every expansion, pass paths
after `--` where the tool supports it, and keep logic out of shell as the
[Python reference](python.md#repository-tools) says.

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
