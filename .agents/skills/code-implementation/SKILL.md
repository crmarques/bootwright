---
name: code-implementation
description: Deliver any authorized Bootwright tracked change, including specs, skills, guidance, code, and tests, through change-class verification, the commit convention and branch or pull-request integration.
---

# Code Implementation

The single owner of verification, commits and integration for tracked changes.
It never authorizes a push, a release or product scope.

## Before editing

1. Confirm the authorized slice (AGENTS.md) and its owning specs, milestone
   page and knowledge. Record unrelated discoveries as an item under
   `specs/milestones/`; report a pre-existing defect as a follow-up
   instead of fixing it in this change.
2. Work on a task branch in its own worktree, never in the destination
   checkout; in a cloud session the harness-designated branch is the task
   branch. Inspect status first and preserve unrelated changes: never stash,
   reset or rewrite them.
3. Load the [domain skills](../index.md) the change touches and the references
   it needs: [formatting](references/formatting.md), [Go](references/go.md),
   [Ansible](references/ansible.md). A new or changed dependency follows the
   [dependency rule](../../../specs/architecture.md#dependency-selection-and-reuse).

## Implement and verify

- Keep implementation, specs, examples and tests of one observable change
  together. Add a regression test for a defect. Test public contracts and effect
  boundaries, not incidental implementation; size committed tests like their
  neighbours, and do not keep scratch checks. Prefer targeted edits to rewrites.
- Run the gate of every class the change touches:

| Class | Paths | Gate |
| --- | --- | --- |
| Docs | `specs/`, `docs/`, `.agents/`, `.claude/`, other Markdown | `make docs-check` |
| Go | `internal/`, `cmd/`, `api/`, `test/` | `make quick` |
| Ansible | `ansible/` | `./scripts/ansible-check --suite units` and `--suite lint` |
| Safety | substrate, managed OS, container cluster, lifecycle and contextfs code or their roles | `make check` and `make race`, plus an independent review of the diff |
| Integration | any | `make check`, which CI runs |

- Record exact commands and results. Failed, skipped, flaky, unavailable and
  unrun gates are not passes.
- Before committing, review the complete diff and run `git diff --cached --check`.

## Commit and integrate

- One commit per coherent change: a `type(domain): subject` Conventional Commit
  subject; a body stating what changed and why and every gate that ran with its
  result; a `Refs:` line naming the slice and its items.
- Integrate through a pull request gated by CI. Follow-ups on an open pull
  request are new commits. Local integration, parallel workers and cleanup are
  in [git integration](references/git-integration.md); model, effort and
  unattended-run guidance is in [agent operation](references/agent-operation.md).
- Push only when the user or the harness requires it.
- Report the outcome, commits, gates with results, limitations and follow-ups.
