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

The repository keeps its non-spec knowledge in three indexed stores; consult
them instead of rediscovering:

- **On any unexpected failure** — build, test, lint, a `bootwright` command, an
  Ansible task, an install that stalls — match the error text or symptom
  against `.agents/knowledge/KNOWLEDGE.md` first and load only the matching
  file. Most recurring failures in this repo already have a diagnosed root
  cause there.
- **Before designing or changing behavior**, scan the decision table in
  `specs/adr/README.md`; an accepted ADR may already fix the shape of the
  change or record why the obvious alternative was rejected.
- **Before proposing work that looks new**, check `.agents/knowledge/BACKLOG.md`
  — it may already be recorded as deliberately deferred, with the reason.
- **When a task uncovers a new root cause, vendor quirk, constraint, or
  decision**, record it in `.agents/knowledge/` (with a symptom row in
  `KNOWLEDGE.md`) or `specs/adr/` in the same change. Source comments are not
  a knowledge store.

## Core Invariants

These hold for every change; verify their current form in `/specs/` when a task
depends on them.

- **Scope.** Desired-state orchestration of cloud platforms across the platform
  component set defined in `specs/domain.md`, from bare or virtualized
  substrates through cluster-bound bootstrap add-ons.
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
  and external process passthrough such as Ansible streams
  (`specs/state-model.md`, CLI Contract).
- **Clean break.** `v1alpha1` may break cleanly: no migrations, aliases,
  compatibility shims, or legacy examples.
- **State-change authorization.** A state change happens only when the operator
  explicitly asked for it. Every new state-changing command, flag, kind, or
  provider must classify its authorization and default to refusal: drift,
  foreign ownership, unknown state, or a failed probe fails closed before the
  first side effect; a refusal names what was found, why it is unsafe, and the
  exact `bootwright …` command that proceeds intentionally; and the case joins
  the safety matrix in `internal/cli/apply_destroy_safety_matrix_test.go`. Four
  rules make that closed over new code rather than a convention to remember:
  a gate keys on **what the run destroys and what it selected** — the resolved
  `clusteraccess.Selection` and the shared consequence predicate — never on a
  stage or flag name, and the gate, the refusal, the prompt choice and the
  preview all read that one predicate so they cannot disagree; a recorded desired
  hash covers **only desired state that reaches a host**, so controller-side
  policy is excluded (folding it in turns a policy edit into fleet-wide drift) and
  a task hash never depends on the run's `--clusters`/`--machines` selection;
  an authorization token is **published in every home, exercised by the matrix,
  and refused by a verb whose gates cannot consume it**; and a token the run did
  consume is never reported as inert. The normative contract is in
  `specs/state-model.md` and ADR 0007, refined by ADR 0030 and ADR 0031; the
  guard tests that enforce it are cataloged in
  `.agents/knowledge/apply-destroy-authorization-guards.md`.
- **Definitions.** Keep docs and specs concise. Specs own normative rules; docs
  teach workflows and link back. The per-kind field reference is the deliberate
  exception: every page under `docs/concepts/` is one by design (the
  Required/Default convention is owned by `docs/concepts/index.md`), so a field
  table there is not a duplicate of `specs/state-model.md`, and guard tests
  require a few rules to appear in both. Add implementation detail only when
  current code or an accepted decision needs it.
- **Comments.** Source files carry no prose comments — only the machine-read
  directives ADR 0006 enumerates (`//go:build`, `# noqa`, shebangs, and the
  rest). Knowledge lives in `.agents/knowledge/`, decisions in `specs/adr/`,
  schema semantics in `/specs/` and `/docs/`. Enforced by the comment-policy
  guard tests in `internal/repo/checks`; the full rule is in
  `.agents/skills/code-quality/SKILL.md`.

## Implementation Workflow

For any request that changes repo-tracked files — including docs, examples, and
agent guidance — use the `implementation-worktree` skill **before editing**. That
skill and `implementation-validation` own the full procedure; the contract in
brief:

- Create a temporary branch and worktree from local `main` (preauthorized — do not
  ask). Edit only inside it; use the primary `main` worktree for read-only
  inspection until integration.
- Rebase first: immediately before every `make check-fast`, user-requested `make
  check`, or task commit, check whether local `main` has advanced and rebase onto
  it if so — separately each time, even when an earlier step found the branch
  current. Use `make check-fast` by default; run `make check` only when the user
  requests that gate. Task commits are preauthorized; do not ask.
- Once checks pass on a branch rebased current onto local `main`, integration is
  preauthorized — do the final rebase, fast-forward `main`, remove the worktree,
  and delete the branch without asking. Merge only while every safety gate holds:
  `main` was clean at task start and is still clean, checks are green, and no
  rebase or fast-forward conflict occurs. If any gate fails — `main` dirty at
  integration time, a conflict, checks not green — or the user asked to hold the
  change for review, do not touch `main`: keep the worktree and report the blocker
  or held state instead of touching unrelated changes.

## Handoff Format

Use Conventional Commits (`type(scope): subject`) when asked to commit or when
giving a commit subject after user review/testing:

- Generate ONLY one short subject line (no body). Max 72 chars.
- Author every commit as the human only. Never add an agent co-author or
  attribution trailer (`Co-Authored-By:`, `Generated with …`, or similar); commit
  metadata carries human authorship, no agent signature.
- When integration is autonomous and succeeds (checks green, branch rebased,
  `main` clean, no conflict), output ONLY the conventional-commit subject line — no
  summaries, file lists, verification details, or commit questions.
- When a safety gate blocks merge, or the user asked to hold the change on a
  temporary branch for review/testing, report the temporary worktree path, branch,
  task commit, whether `make check-fast` completed, and the blocker or the reason it
  is held. If required verification cannot complete, report that blocker instead.
- Allowed types: `feat`, `fix`, `docs`, `refactor`, `perf`, `test`, `build`, `ci`,
  `chore`, `revert`. Use a scope when obvious (package/module/folder).

Examples:

- Merged (autonomous success): `docs(agents): shorten standard handoff`
- Held for review: worktree
  `/tmp/bootwright-worktrees/<task-slug>-<base8>-<timestamp>`, branch
  `work/<task-slug>-<base8>-<timestamp>`, task commit
  `feat(cli): add --machines selection`, `make check-fast` completed green —
  held at the user's request for hardware testing.
- Blocked: `Blocked: make check-fast could not complete`
