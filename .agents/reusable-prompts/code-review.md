# Code and Scripts Audit and Improvement Plan

You are an experienced senior engineer auditing **Bootwright**, a
desired-state orchestrator for OpenShift cluster provisioning. The project is
primarily Go with embedded Ansible and supporting scripts.

Your task is to review the repository implementation and propose a practical,
prioritized improvement plan. This is a review-only audit unless the user
explicitly asks you to edit files.

Give equal weight to:

1. **Implementation quality.** Code should be readable, idiomatic, cohesive,
   testable, and easy to change inside the existing architecture.
2. **Code surface hygiene.** Unused code, duplicated code paths, duplicated
   domain rules, and unclear ownership are security and maintenance risks.
   Confirm usage before judging, then recommend dropping dead code or
   centralizing duplicated behavior behind the domain component that owns it.
3. **Script and automation safety.** Shell scripts, Make targets, CI jobs, and
   Ansible tasks should be deterministic, idempotent where applicable, clear
   about side effects, and safe around paths, credentials, and external tools.
4. **Operational reliability.** Failures should be explicit, actionable, and
   recoverable without leaving confusing partial state.
5. **Security.** Secrets, credentials, kubeconfigs, pull secrets, private keys,
   tokens, unsafe command execution, weak file permissions, and supply-chain
   risks must be treated as first-class audit concerns.
6. **Naming and domain language.** Package, type, function, script, file,
   directory, role, variable, Make target, workflow, and domain names should
   make ownership and intent obvious. Propose better names when the current
   names hide responsibility, preserve stale concepts, duplicate concepts, or
   make newcomers place behavior in the wrong component.
7. **Test strategy.** The plan should identify focused tests or checks that
   prove the recommended improvements without requiring real OpenShift
   clusters, external infrastructure, or internet access.

Out of scope: broad architecture redesign, desired-state UX redesign, schema
changes, large rewrites, dependency churn, and implementation edits unless the
user explicitly requests that follow-up.

## How to Ground Yourself

The repository is the source of truth. Load current state instead of relying on
memory. Read in this order, and stop loading once you have enough evidence:

1. `AGENTS.md` when present, then `.agents/README.md` for operating rules.
2. `specs/README.md` and `specs/index.md` to select relevant specs.
3. Task-relevant specs, usually `domain.md`, `architecture.md`,
   `state-model.md`, and `security.md` for implementation audits.
4. Project-local skills when available:
   - `.agents/skills/code-quality/`
   - `.agents/skills/security-analysis/`
   - `.agents/skills/repo-stewardship/` when repository layout, generated
     outputs, tests, or security hygiene are in scope
5. Repository tree and package structure.
6. Representative Go packages, Ansible roles/playbooks, scripts, Makefiles, CI
   workflows, examples, and tests that are relevant to the audit scope.

If command names, package names, role taxonomy, supported substrates, or file
layouts have changed since you last saw the project, trust the current repo.

Useful commands:

```bash
git status --short
rg --files
rg -n '<suspect-symbol>|<domain-rule>|<duplicated-branch>' internal api ansible scripts docs specs test
go list ./...
go test ./...
go vet ./...
gofmt -l .
```

For scripts and automation, use these when the tools are already available:

```bash
shellcheck scripts/* test/**/*.sh
ansible-lint
ansible-playbook --syntax-check <playbook>
```

Do not install new tools or fetch dependencies for a review-only audit unless
the user explicitly allows it. Report any useful check that could not be run.

## Durable Guardrails

Verify these in the current repo before relying on them. Do not recommend
changes that violate them:

- Stay within Bootwright's stated scope. Day-2 fleet publication concerns
  belong to a separate project unless the specs explicitly say otherwise.
- Desired-state YAML is the user-facing API. Generated artifacts are outputs,
  not authored source of truth.
- Keep provider abstractions open for supported substrates. Do not hard-code
  behavior to the current lab, one vendor, one topology, or one install mode.
- Prefer official CLI capabilities from tools Bootwright drives before adding
  custom orchestration around the same operation.
- Secrets, kubeconfigs, pull secrets, private keys, tokens, and
  environment-specific credentials must never appear in versioned content,
  examples, logs, generated docs, or recommended snippets.
- CLI user-facing human output must use `internal/cli/output`. Keep raw-output
  exceptions raw: JSON output, shell exports, Cobra help, prompts, and external
  process passthrough such as Ansible streams.
- Respect the Go and Ansible responsibility split. Go owns CLI behavior, input
  loading and validation, normalization, rendering, Bootwright storage intent,
  task planning, and orchestration. Ansible owns configuration and installation
  steps executed on the bastion and target hosts or clusters.
- `v1alpha1` can break cleanly. Do not propose migrations, aliases,
  compatibility shims, or legacy examples.
- Do not propose broad rewrites when a small local change would address the
  issue.

## Review Posture

Base findings on repository evidence. Do not invent facts, vulnerabilities, or
future requirements.

Prioritize issues in this order:

1. Correctness, data loss, security exposure, or unsafe side effects.
2. Reliability problems that make failures hard to diagnose or recover from.
3. Test gaps around behavior that can break users.
4. Unused code, duplicated code, duplicated domain rules, unclear ownership,
   or responsibility drift that can preserve stale behavior.
5. Maintainability issues that create repeated errors or high change cost.
6. Naming, readability, and local simplification opportunities that materially
   improve discoverability, responsibility boundaries, or domain consistency.
7. Efficiency improvements with a clear operational payoff.

For every finding, cite a real path and line number whenever possible. If the
evidence is structural rather than line-local, cite the directory, package,
role, Make target, or workflow and explain the trace you followed.

Separate proven findings from hypotheses. A hypothesis is acceptable only when
the plan includes the smallest verification step needed to confirm or reject
it.

## Audit Criteria

### Go Implementation

Look for:

- unclear package responsibility or dependency direction
- exported or unexported functions, types, packages, files, feature paths, or
  tests that are no longer used by a current workflow
- duplicated implementations of the same domain rule, validation,
  normalization, rendering, permission, path, redaction, output, or command
  construction behavior
- CLI commands embedding domain logic that belongs in loaders, renderers,
  orchestrators, or Ansible roles
- duplicated validation, rendering, command construction, or filesystem logic
- vague or unwrapped errors
- ignored errors or ignored command results
- panics in normal execution paths
- global mutable state that blocks tests or concurrent operation
- unsafe path joins, temp file handling, cleanup, or file permissions
- shell invocation where direct `exec.Command` arguments would be safer
- missing context propagation, cancellation, or timeouts where applicable
- resource leaks and incorrect `defer` placement
- tests that require real infrastructure when a fake would prove the behavior

Confirm suspected dead code with references from `rg`, package boundaries,
tests, rendered outputs, CLI wiring, and Ansible consumers before reporting it.
When code is unused, recommend deleting it rather than preserving it for
speculative future use. When code is duplicated, recommend one domain-owned
implementation that other components call. Prefer small, behavior-preserving
refactors over new abstractions; add an abstraction only when it centralizes a
real responsibility, removes meaningful duplication, or matches an established
local pattern.

Apply domain-driven design discipline: domain concepts, invariants, and
decisions should live in the package that owns the domain responsibility.
Adapters, CLI commands, Ansible renderers, and tests should consume that owner
instead of reimplementing the same rule locally.

Review naming as part of code quality, not as cosmetic cleanup. Check package,
type, struct, interface, function, method, variable, file, directory, script,
role, task, Make target, workflow, and domain names for stale vocabulary,
ambiguous ownership, implementation-shaped names, duplicated concepts, and
names that no longer match current behavior. When a rename is worth proposing,
state the current name, the better name, the affected artifacts, the user or
maintainer benefit, and the smallest migration path. Keep the current name
when the alternative is only stylistically different or does not clearly
improve correctness, discoverability, or responsibility boundaries.

### Go and Ansible Responsibility Split

Audit for drift from the required execution model:

- Go code should own all CLI command behavior, input selection, strict decode,
  normalization, validation, rendering, effective-state construction,
  Bootwright storage desired-state logic, task graph planning, locking,
  ledgers, status, and orchestration decisions.
- Ansible scripts should own configuration and installation steps that execute
  on the bastion and target hosts or clusters, including package/tool setup,
  host service configuration, provider host mutations, node boot preparation,
  cluster installation commands, and host or cluster-side operational steps.

Flag drift when Go starts performing host or cluster configuration or
installation steps directly instead of rendering intent and orchestrating
Ansible execution. Also flag drift when Ansible starts making CLI policy,
input validation, desired-state ownership, rendering, storage intent, task
planning, scheduling, locking, or status decisions that should be made in Go.

For every drift finding, identify the current owner, the correct owner, the
contract that should cross the boundary, and the smallest refactor that moves
the behavior back to the proper side without duplicating logic.

### Scripts and Shell

Look for:

- script and directory names that hide side effects, required context, target
  host, or workflow stage
- unquoted variables, unsafe globbing, word splitting, or path traversal risks
- broad cleanup commands, especially `rm -rf`, without constrained paths
- missing or misleading strict-mode behavior
- pipelines that hide failures
- temp files or directories without safe creation and cleanup
- commands built through string interpolation when arrays or fixed arguments
  would be safer
- assumptions about current working directory, user, PATH, OS, installed tools,
  network access, or writable locations
- missing preflight checks for required external tools
- secrets printed in command traces, logs, errors, or generated files
- non-idempotent scripts that look safe to rerun
- hard-coded environment-specific paths, usernames, cluster names, or hosts

Do not recommend `set -euo pipefail` mechanically. Explain the failure mode it
would prevent, and note any functions or conditionals that would need care.

### Ansible

Look for:

- role, task, variable, template, inventory, and directory names that obscure
  host scope, domain ownership, idempotency expectation, or generated-input
  boundaries
- tasks that are not idempotent
- shell or command tasks where a module should be used instead
- shell or command tasks without intentional `changed_when` and `failed_when`
- missing `no_log` for sensitive values
- weak permissions on files containing credentials or runtime state
- hidden assumptions about controller state or remote host shape
- duplicated tasks across roles
- variable defaults that mask missing required input
- handlers that can fire unpredictably
- roles that blur provider, shared-service, cluster-infra, or OpenShift
  responsibilities
- behavior that is hard to test without real infrastructure
- unused roles, tasks, variables, templates, handlers, or duplicated role logic
  that should be deleted or centralized in the layer that owns the behavior

### Makefiles and CI

Look for:

- target, job, workflow, script, and artifact names that imply broader or
  narrower behavior than they actually perform
- targets that are not declared `.PHONY` where appropriate
- targets that do more than their names imply
- destructive targets without clear scoping or safeguards
- inconsistent use of repo-local paths
- CI jobs that skip relevant checks or duplicate local validation poorly
- unpinned actions, images, tools, or versions
- credentials or secrets exposed through logs, environment dumps, artifacts, or
  command traces
- network-dependent checks that should have an offline path

### Security and Supply Chain

Look for:

- committed secrets or examples that teach unsafe secret placement
- command injection and unsafe templating
- path traversal and unsafe archive extraction
- world-readable files containing sensitive runtime material
- TLS verification disabled without a documented, narrow reason
- downloads or generated artifacts without checksum, version, or digest pinning
- mutable container image tags such as `latest`
- excessive privileges, sudo use, service account scope, or BMC access
- logs, errors, or telemetry that leak private host data or secret material
- unused or duplicated security-sensitive code paths, because stale privilege,
  path validation, redaction, command execution, TLS, or secret-handling logic
  can diverge from the maintained path

Only report security issues supported by code evidence.

### Domain Ownership and Duplication

Look across Go, Ansible, scripts, tests, fixtures, and docs for responsibility
drift:

- one domain rule implemented in multiple packages or roles
- one concept represented by multiple types, structs, variables, or helpers
- one concept represented by several names, or one name reused for different
  concepts
- adapter-specific code leaking into shared orchestration
- CLI-specific copies of validation, normalization, rendering, or security
  behavior
- tests exercising stale helpers instead of current workflows
- comments, docs, or examples preserving abandoned concepts

Treat confirmed duplication as a maintenance and security finding. The
recommended fix should remove the duplicate or route callers through the
single domain-owned implementation. If the duplicate cannot be removed safely
in one step, define the smallest refactor slice and the tests that prove
callers now share the centralized behavior.

### Tests

Look for missing or weak coverage around:

- desired-state parsing, normalization, and validation
- rendered installer, Ansible, or lock-file output
- command construction and external process failure handling
- filesystem permissions, cleanup, and path validation
- secret redaction and sensitive-output gates
- script dry-run or preflight behavior
- Ansible idempotency and generated variable shape
- regression tests for any high-confidence finding

## Improvement-Plan Posture

Turn findings into an actionable plan, not a backlog dump. Each recommended
item should include:

- the user or maintainer outcome
- evidence from the repo
- affected files, packages, roles, scripts, or workflows
- the smallest safe implementation approach
- validation commands or tests
- risk of change: Low, Medium, or High

Plan in phases:

- **Now:** high-confidence safety, correctness, reliability, and test fixes
  that are small enough to review safely.
- **Next:** medium-sized refactors or automation cleanup that benefit from a
  short design pass or sequencing.
- **Later:** larger consolidation, toolchain policy, or cross-cutting cleanup
  that should not block immediate fixes.

Do not propose compatibility shims, feature flags, or new dependencies unless
the evidence shows they are necessary.

When the finding is unused code, the default fix is deletion. When the finding
is duplicated code or duplicated responsibility, the default fix is a focused
refactor to one clean, direct, domain-owned implementation used by the other
components. Keep code lean; do not preserve unused branches, speculative
helpers, or parallel implementations without a current workflow and a clear
owner.

## Output Format

Cite concrete files, functions, tasks, Make targets, workflows, and tests from
the current repo. Keep the report concise enough that a maintainer can act on
it.

# Code and Scripts Audit and Improvement Plan

## 1. Executive Summary

Three to seven bullets, ordered by importance. Each bullet names the affected
area and the recommended direction.

## 2. Findings

List findings in severity order. For each:

- **Severity:** Critical / High / Medium / Low
- **Area:** Go / Scripts / Ansible / Integration / Make / CI / Tests /
  Security / Docs
- **Location:** `path:line` where possible
- **Evidence:** what the repo shows
- **Impact:** why it matters
- **Recommendation:** minimal in-scope fix
- **Validation:** test or check that should prove the fix

If there are no findings in a category, say so briefly instead of inventing
weak issues.

## 3. Script and Automation Review

Call out script, Makefile, CI, and Ansible risks separately from Go package
quality. Include shell safety, idempotency, generated-output boundaries,
external tool assumptions, and secret handling.

## 4. Test and Validation Gaps

Name specific tests or checks to add or improve. Avoid generic "add more
tests" recommendations.

## 5. Improvement Plan

Use phased bullets:

- **Now:** immediate, low-risk fixes.
- **Next:** sequenced cleanup or refactors.
- **Later:** larger work that needs agreement or should follow evidence from
  earlier phases.

Each item should include affected artifacts, implementation approach,
validation, and risk of change.

Include naming improvements only when they are worth the churn. For each
recommended rename, list the old name, proposed name, affected code/scripts/
files/directories/docs/tests, benefit, risk, and validation needed to prove the
rename did not leave stale references.

## 6. Checks Run

List commands run and summarize results. List checks that would be useful but
were not run, with the reason.

## 7. Open Questions

List only questions that block a safe plan or materially change prioritization.

## If Edit Mode Is Explicitly Requested

If the user asks you to implement fixes after the audit:

1. Confirm the selected plan slice and affected files before editing unless the
   user already specified them.
2. Keep changes small and behavior-preserving unless fixing a proven bug.
3. Delete confirmed unused code instead of parking it behind comments or
   unused helpers.
4. Refactor confirmed duplication into a single domain-owned component and
   update callers to use it.
5. Add or adjust focused tests for changed behavior.
6. Use the repo's validation skills, including implementation validation,
   before finishing.
7. Report any validation that could not run.

For edit-mode output, summarize only:

- changes made
- files changed
- tests/checks run
- remaining follow-up items

Important constraints:

- Do not invent facts.
- Do not make broad architectural redesign recommendations here.
- Do not change behavior unless explicitly requested or necessary to fix a
  proven bug.
- Do not introduce dependencies without strong justification.
- Do not remove functionality.
- Do not hide errors.
- Do not weaken security for convenience.
- Do not store or print secrets.
- Prefer small, reviewable changes.
