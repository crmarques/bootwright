---
name: code-implementation
description: Deliver any authorized Bootwright tracked change, including specs, skills, guidance, code and tests, through tiered verification, the safety class, the commit convention and local or pull-request integration.
---

# Code Implementation

The single owner of verification, commits and integration for tracked changes.
It never authorizes a push, a release, product scope or a command with effects.

## Before editing

1. Confirm the authorized slice and its defect rule (AGENTS.md); read the
   item's row and its `### B<n>` section, not the whole milestone page, then
   its owning specs and knowledge.
2. Work on a task branch in its own worktree, never in the destination
   checkout. Inspect status first and preserve unrelated changes: never stash,
   reset or rewrite them.
3. Load the [domain skills](../index.md) the change touches and, by changed
   file, the references it needs: [formatting](references/formatting.md),
   [Go](references/go.md), [Ansible](references/ansible.md),
   [Python](references/python.md). A new or changed dependency follows the
   [dependency rule](../../../specs/architecture.md#dependency-selection-and-reuse).

## Safety class

A change is safety class when its item's Kind is `safety`, or when it changes a
destructive effect, a privilege boundary, secret custody, an ownership mark or
proof, or a durable store transition in any capability, or lifecycle or
contextfs concurrency. It loads
[security-safety](../security-safety/SKILL.md) and adds an independent review
and `make race`, plus a race run of a changed concurrent package that target
omits.

## Implement and verify

- Keep implementation, specs, examples and tests of one observable change
  together. Add a regression test for a defect. Test contracts and effect
  boundaries, sized like their neighbours; keep no scratch checks. Prefer
  targeted edits to rewrites.
- Verify by the [tiers](references/verification.md). The commit that integrates
  into main rests on `make check` plus the safety additions.
- A change to what an effect is moves its content digest (Go and Ansible
  references).
- Record exact commands and results; failed, skipped, flaky, unavailable and
  unrun gates are not passes.
- Before committing, review the whole diff and run `git diff --cached --check`.

## Commit and integrate

- One commit per coherent change: a `type(domain): subject` Conventional Commit
  subject; a body stating what changed and why and each gate run with its
  result; `Refs:` naming slice and items; a
  [`Contract:` line](references/git-integration.md#contract-line) when due.
  CI and a fast-forward run `scripts/check-commits BASE HEAD`.
- Integrate by a [git integration](references/git-integration.md) variant:
  local fast-forward by default (D168), a pull request when the owner or
  harness asks. Push only when the owner or harness requires it. Model, effort
  and unattended runs: [agent operation](references/agent-operation.md).
- Report the outcome, commits, gates with results, limitations and follow-ups.
