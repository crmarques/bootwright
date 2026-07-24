# Agent Entrypoint

This repository is governed by the project specs in `/specs/`. Load the repo
information, specs, skills, code, examples, and current worktree state needed for
the task before editing. Do not start from partial context.

## Load Order

1. This file.
2. `/.agents/README.md` — the skills and knowledge catalog.
3. `/specs/README.md` and `/specs/index.md`, then only the task-relevant specs.
4. The skill(s) the catalog maps to the task, from `/.agents/skills/`.

## Knowledge Lookup

The repository keeps its non-spec knowledge in two indexed stores; consult them
instead of rediscovering:

- **On any unexpected failure** — build, test, lint, a `bootwright` command, an
  Ansible task, an install that stalls — match the error text or symptom
  against `.agents/knowledge/KNOWLEDGE.md` first and load only the matching
  file. Most recurring failures in this repo already have a diagnosed root
  cause there.
- **Before designing or changing behavior**, scan the decision table in
  `specs/adr/README.md`; an accepted ADR may already fix the shape of the
  change or record why the obvious alternative was rejected.
- **When a task uncovers a new root cause, vendor quirk, constraint, or
  decision**, record it in `.agents/knowledge/` (with a symptom row in
  `KNOWLEDGE.md`) or `specs/adr/` in the same change. Source comments are not
  a knowledge store.

## Core Invariants

These hold for every change; verify their current form in `/specs/` when a task
depends on them.

- **Scope.** Desired-state orchestration of cloud platforms from bare or
  virtualized substrates through machines, managed machine OS installs, shared
  infrastructure services, OpenShift or OKD managed clusters, Ceph storage
  clusters, exported storage surfaces, and cluster-bound bootstrap add-ons.
  Bootwright may converge the whole graph or selected platform components.
  Day-2 GitOps publication of fleet content (package catalogs, KRC/SRC
  bootstrap) is a separate project; do not reintroduce it. Initial
  container-cluster install scope is direct `openshift-install agent` runs
  against single-node and multi-node machines.
- **Provider neutrality.** Keep substrate abstractions open for libvirt, bare
  metal, vSphere, OpenShift Virtualization, and future providers. Handle provider
  and BMC variation through capability discovery, advertised metadata, and
  normalized adapters first; keep unavoidable supplier workarounds isolated,
  minimal, tested, and documented in `.agents/knowledge/`.
- **Product API.** Desired-state YAML is the user-facing API: declarative,
  idempotent, typed, deterministic. Generated installer files, inventories, and
  rendered outputs are outputs, not authored source of truth.
- **Drive official tools.** Prefer native capabilities of the tools Bootwright
  drives (for example `openshift-install`) before adding custom orchestration
  around the same operation.
- **Secrets.** Never put secrets, kubeconfigs, pull secrets, private keys, tokens,
  or environment-specific credentials in versioned content.
- **Output.** CLI human output goes through the centralized `internal/cli/output`
  component. Raw exceptions stay raw: JSON, shell exports, Cobra help, prompts,
  and external process passthrough such as Ansible streams.
- **Clean break.** `v1alpha1` may break cleanly: no migrations, aliases,
  compatibility shims, or legacy examples.
- **Definitions.** Keep docs and specs concise. Specs own normative rules; docs
  teach workflows and link back. Add implementation detail only when current code
  or an accepted decision needs it.
- **Comments.** Source files carry no prose comments — only machine-read
  directives (`//go:build`, `//go:embed`, `//nolint`, `# noqa`, `# shellcheck`,
  `# syntax=`, shebangs). Knowledge lives in `.agents/knowledge/`, decisions in
  `specs/adr/`, schema semantics in `/specs/` and `/docs/`. Enforced by the
  comment-policy guard tests in `internal/repo/checks`; the full rule is in
  `.agents/skills/code-quality/SKILL.md`.

## Implementation Workflow

For any request that changes repo-tracked files — including docs, examples, and
agent guidance — use the `implementation-worktree` skill **before editing**. That
skill and `implementation-validation` own the full procedure; the contract in
brief:

- Create a temporary branch and worktree from local `main` (preauthorized — do not
  ask). Edit only inside it; use the primary `main` worktree for read-only
  inspection until the user explicitly approves merge.
- During investigation, run the smallest targeted command that answers the current
  question; do not run aggregate checks unless the user asks.
- Immediately before every `make check-fast`, user-requested `make check`, or
  task commit on the temporary branch, check whether local `main` has advanced
  since the branch was created or last rebased. Rebase onto it first if so, then
  resolve and adjust the change before continuing. Repeat this check separately
  for validation and commit even when an earlier step already found the branch
  current. Use `make check-fast` by default; run `make check` only when the user
  requests that gate. Task commits are preauthorized; do not ask.
- Leave `main` integration pending explicit merge approval. If `main` is dirty at
  integration time, report that it is not ready instead of touching unrelated
  changes. A response such as "go" authorizes the final rebase, merge, and cleanup.

## Handoff Format

Use Conventional Commits (`type(scope): subject`) when asked to commit or when
giving a commit subject after user review/testing:

- Generate ONLY one short subject line (no body). Max 72 chars.
- Author every commit as the human only. Never add an agent co-author or
  attribution trailer (`Co-Authored-By:`, `Generated with …`, or similar); commit
  metadata carries human authorship, no agent signature.
- For an implementation/fix handoff left on a temporary branch for review/testing,
  report the temporary worktree path, branch, task commit, whether `make
  check-fast` completed, and whether the branch is ready to merge into local
  `main`. If processing is blocked or required verification cannot complete, report
  the blocker instead.
- After the user approves merge and integration succeeds, output ONLY the
  conventional-commit subject line — no summaries, file lists, verification
  details, or commit questions.
- Allowed types: `feat`, `fix`, `docs`, `refactor`, `perf`, `test`, `build`, `ci`,
  `chore`, `revert`. Use a scope when obvious (package/module/folder).

Examples:

- Ready for review/test: `/tmp/bootwright-worktrees/<task-slug>-<base8>-<timestamp>`
- Commit-approved success: `docs(agents): shorten standard handoff`
- Blocked: `Blocked: make check-fast could not complete`
