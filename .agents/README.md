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
| `architecture` | Designing platform behavior — OpenShift behavior, provider boundaries, multi-cluster provisioning, or lab Redfish emulation (for internal package boundaries and the Go↔Ansible split use the `architecture.md` prompt below) |
| `repo-stewardship` | Changing repository layout, generated-output boundaries, tests, or security hygiene |
| `security-analysis` | Reviewing secrets, credentials, permissions, command execution, supply chain, or TLS/trust handling |
| `go-dependencies` | Adding, upgrading, replacing, or removing Go module dependencies |
| `code-quality` | Adding, modifying, deleting, or reviewing code (Go, Python, shell, Ansible YAML, Jinja2) |

## Reusable Prompts

Long-form review and audit prompts live in `/.agents/reusable-prompts/`. Load one
when a task matches its focus; each is self-contained and grounds itself in the
current specs and code before judging.

| Prompt | Use When |
| --- | --- |
| `architecture.md` | Rethinking internal architecture — package boundaries, the Go↔Ansible split, repo/script distribution, or role taxonomy |
| `cli-schema-ux-rethink.md` | Rethinking the CLI and desired-state schema from first principles (three-alternatives design critique) |
| `specs-ux.md` | Auditing the *current* user-facing contract — operator UX, authoring ergonomics, and definition/spec quality |
| `code-review.md` | Auditing implementation quality and safety — correctness, dead code, duplication, error handling, script/CI safety |
| `code-flow-review.md` | Tracing real input end-to-end to final output for bugs, intent drift, and Go↔Ansible contract mismatches |
| `provisioning-logic-review.md` | Reviewing the provisioning graph — closure, dependency ordering, locks, parallelism, and resumability |
| `idempotency-safety-audit.md` | Auditing idempotency and destructive-operation safety against a user-supplied scenario file (read-only) |
| `apply-destroy-safety-contract.md` | Reviewing `apply`/`destroy` command and flag semantics for spec/docs/code coherence — no input; self-generated scenarios from a fixed two-DC baseline, then implements the guardrails |
| `security-audit.md` | Running a dedicated deep security pass — secrets, privilege, TLS/trust, and supply chain |
| `state-lifecycle-scenario-review.md` | Pressure-testing lifecycle transitions (apply/destroy/recreate) across many ownership and state scenarios — read-only matrix; proposes safety locks, edits nothing |

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
| Deferred / open work | `.agents/knowledge/BACKLOG.md` |

Knowledge is written to these stores, never to source comments (see the
Comments core invariant in `/AGENTS.md` and the code-quality skill).
