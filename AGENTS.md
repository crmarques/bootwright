# Agent Entrypoint

This repository is governed by the project specs in `/specs/`. Before
making changes, load only the specs that match the user request.

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
- When adding or changing CLI user-facing human output, always use the
  centralized `internal/cli/output` component. Keep the documented raw-output
  exceptions raw: JSON output, shell exports, Cobra help, prompts, and external
  process passthrough such as Ansible streams.
- After completing the intended edit set for any implementation request that
  changes repo-tracked files, run `make check` once before handoff. Do not run
  it repeatedly during the same request unless later edits can invalidate the
  previous result. If `make check` cannot run or fails, report the blocker
  instead of a successful handoff.
- During investigation or iterative fixes, prefer the smallest direct targeted
  command that answers the current question. Do not run aggregate checks or
  their member commands in a way that duplicates a final completed `make check`
  unless later edits or failure diagnosis require it.
- Before completing implementation work, use the
  `/.agents/skills/implementation-validation/` skill.

## Handoff Format

Use Conventional Commits (`type(scope): subject`):

- Generate ONLY one short subject line (no body). Max 72 chars.
- For a successful standard request handoff, output ONLY that one
  subject line. Do NOT append summaries, file lists, verification
  details, or commit questions. If request processing is blocked or
  required verification cannot complete, report the blocker instead.
- Allowed types: `feat`, `fix`, `docs`, `refactor`, `perf`, `test`,
  `build`, `ci`, `chore`, `revert`.
- Use a scope when obvious (package/module/folder).

Examples:

- Success: `docs(agents): shorten standard handoff`
- Blocked: `Blocked: make check could not complete`
