# Agent Entrypoint

This repository is governed by the project specs in `/specs/`. Before
making changes, load the repo information, specs, skills, code, examples,
and current worktree state needed to complete the user request. Do not start
editing from partial context.

## Required Load Order

1. Read `/.agents/README.md`.
2. Read `/specs/README.md`.
3. Read `/specs/index.md`.
4. Load the referenced domain specs needed for the task.
5. If a project-local skill applies, read it from `/.agents/skills/`.

## Operating Rules

- Preserve the long-term goal: automated, declarative provisioning of
  fleets of OpenShift clusters from bare hardware to installed clusters.
  Day-2 GitOps publication of fleet content (package catalogs, KRC/SRC
  bootstrap) lives in a separate project; do not reintroduce it here.
- Treat the initial scope as direct openshift-install-agent runs against
  cluster nodes (single-node and multi-node), while keeping provider
  abstractions open for libvirt, bare metal, vSphere, OpenShift
  Virtualization, and other substrates.
- Handle provider and BMC supplier variations through generic capability
  discovery, advertised metadata, and normalized adapters first. Keep
  unavoidable supplier-specific workarounds isolated, minimal, tested, and
  documented in `.agents/knowledge/`.
- Prefer declarative desired state, idempotent orchestration, typed
  schemas, deterministic rendering, and testable adapters.
- Prefer official CLI capabilities from the tools Bootwright drives
  (for example `openshift-install`) before adding custom orchestration
  behavior around the same operation.
- Do not introduce secrets, kubeconfigs, pull secrets, private keys,
  tokens, or environment-specific credentials into versioned content.
- Keep docs and specs concise. Add implementation detail only when it is
  needed by current code or an accepted decision.
- For any implementation request that changes repo-tracked files, use the
  `/.agents/skills/parallel-implementation/` skill before editing. Agents are
  already authorized to create a temporary branch and worktree from local
  `main`; do not ask before doing so. Agents must work from that isolated
  temporary worktree and may touch the primary `main` worktree only after the
  user explicitly approves merge.
- Do not commit, push, merge, or fast-forward implementation fixes immediately.
  Leave changes available for user review/testing and wait for explicit merge
  approval before committing task changes or integrating into `main`.
- When adding or changing CLI user-facing human output, always use the
  centralized `internal/cli/output` component. Keep the documented raw-output
  exceptions raw: JSON output, shell exports, Cobra help, prompts, and external
  process passthrough such as Ansible streams.
- After completing the intended edit set for any implementation request, run
  only the repository fast check: `make check-fast`. Do not run `make check`
  by yourself; run it only when the user explicitly requests that full gate.
- After `make check-fast`, check whether the temporary branch is ready to merge
  into current local `main`. If local `main` has advanced or the branch is not
  ready, rebase the temporary branch onto local `main`, fix conflicts or needed
  adjustments, rerun `make check-fast`, and repeat until the branch is ready
  or a real blocker remains.
- If the primary `main` worktree has uncommitted changes when integration is
  considered, report that `main` is not ready instead of touching unrelated
  user changes.
- During investigation or iterative fixes, prefer the smallest direct targeted
  command that answers the current question. Do not run aggregate checks unless
  the user explicitly requested them.
- Before completing implementation work, use the
  `/.agents/skills/implementation-validation/` skill.
- Once the temporary branch is ready for `main`, ask the user whether merge can
  proceed. A response such as "go" authorizes creating the task commit if
  needed, final rebase if local `main` advanced, merge into `main`, and deletion
  of the temporary worktree and branch; do not ask separately for those steps.

## Handoff Format

Use Conventional Commits (`type(scope): subject`) when asked to commit or when
providing a commit subject after user review/testing:

- Generate ONLY one short subject line (no body). Max 72 chars.
- For an implementation/fix handoff where changes are intentionally left
  uncommitted for user review/testing, report the temporary worktree path and
  branch, whether `make check-fast` completed, and whether the branch is ready
  to merge into local `main`. If request processing is blocked or required
  verification cannot complete, report the blocker instead.
- After the user explicitly approves merge and integration succeeds, output ONLY the
  conventional-commit subject line. Do NOT append summaries, file lists,
  verification details, or commit questions.
- Allowed types: `feat`, `fix`, `docs`, `refactor`, `perf`, `test`,
  `build`, `ci`, `chore`, `revert`.
- Use a scope when obvious (package/module/folder).

Examples:

- Ready for review/test: `/tmp/bootwright-worktrees/<task-slug>-<base8>-<timestamp>`
- Commit-approved success: `docs(agents): shorten standard handoff`
- Blocked: `Blocked: make check-fast could not complete`
