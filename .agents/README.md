# Project-Local Agent Guidance

Project definitions live in `/specs/` and are the source of truth for humans and
coding agents. Load order, core invariants, the implementation workflow, and the
handoff format are in `/AGENTS.md`. This file is the catalog of skills and
knowledge an agent draws on for a task. Load only what the task needs — do not
bulk-load skills or the knowledge directory.

## Skills

Read the matching skill from `/.agents/skills/<name>/SKILL.md` before working in
its area.

| Skill | Use When |
| --- | --- |
| `definition-stewardship` | Changing specs, ADRs, docs, examples, E2E fixture names, or agent guidance |
| `implementation-worktree` | Isolating every repo-tracked-file change in a temporary worktree before review and integration |
| `implementation-validation` | Final validation before completing implementation work |
| `architecture` | Designing OpenShift behavior, provider boundaries, multi-cluster provisioning, or lab Redfish emulation |
| `repo-stewardship` | Changing repository layout, generated-output boundaries, tests, or security hygiene |
| `security-analysis` | Reviewing secrets, credentials, permissions, command execution, supply chain, or TLS/trust handling |
| `go-dependencies` | Adding, upgrading, replacing, or removing Go module dependencies |
| `code-quality` | Adding, modifying, deleting, or reviewing code (Go, Python, shell, Ansible YAML, Jinja2) |

## Knowledge Base

When a user reports an error or unexpected failure, check
`.agents/knowledge/KNOWLEDGE.md` for a matching symptom or error fragment before
investigating. Before changing behavior in an area, check the same index's
constraints/semantics tables for that area, and the decision table in
`specs/adr/README.md`. Load only the matching file; do not scan or bulk-load
the directory.

| Index | Location |
| --- | --- |
| Failure symptom map + constraints/semantics by area | `.agents/knowledge/KNOWLEDGE.md` |
| Accepted decisions | `specs/adr/README.md` |

Knowledge is written to these stores, never to source comments (see the
Comments core invariant in `/AGENTS.md` and the code-quality skill).
