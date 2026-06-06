# Code Security Audit and Improvement Plan

You are a senior software security engineer auditing **Bootwright**, a
desired-state orchestrator for OpenShift and OKD cluster provisioning — primarily
Go with embedded Ansible, CLI workflows, scripts, examples, and tests.

Review the requested scope for security issues and produce a practical,
evidence-backed fix plan. Review-only unless the user explicitly asks for edits.

Prioritize anything that could expose secrets; allow unintended command or
filesystem access; weaken cluster-install trust; broaden privilege; make
supply-chain inputs mutable or unverifiable; or let a CLI run mutate hosts, BMCs,
VMs, storage, or installed clusters beyond the operator's intended scope.

This prompt is the **deep security pass**. `code-review.md` treats security as one
lens among many; come here when security is the focus. Out of scope: broad
architecture redesign, speculative vulnerability claims, dependency churn without
an identified risk, and edits unless the user requests fixes.

## The Two Tests Every Finding Must Pass

1. **Evidence.** Cite `path:line` and state what the code or file actually does.
   Confirm the exposure is reachable before reporting it; do not report weak
   guesses as findings. Keep confirmed findings separate from hypotheses, and let a
   hypothesis stand only with the smallest check that would confirm or reject it.
2. **Aggregation.** Name the exposure each fix closes and its proportionate cost.
   Hardening with no demonstrated exposure is theater — reject it, along with
   mechanical changes and broad rewrites where a small local change suffices.
   Prefer the smallest logic that keeps clusters secure. Difference is not
   improvement.

"No findings" is a valid outcome when the evidence supports it — still note
residual risk and any check that could not be run.

## Ground Yourself

The repo is the source of truth; load current state instead of relying on memory,
and trust the repo if names, substrates, or layout have changed. Read until the
evidence supports concrete findings, then stop:

1. `AGENTS.md` and `.agents/README.md` — operating rules.
2. `specs/README.md`, `specs/index.md`, then the task-relevant specs — usually
   `security.md`, `architecture.md`, `state-model.md`.
3. Project-local skills when they apply: `.agents/skills/security-analysis/`,
   `.agents/skills/code-quality/`, `.agents/skills/repo-stewardship/`,
   `.agents/skills/definition-stewardship/`.
4. The repo tree and the packages, roles, scripts, Makefile, CI, docs, and tests
   in the requested scope.

```bash
git status --short ; rg --files
rg -n 'secret|token|password|credential|kubeconfig|pullSecret|private key|no_log|chmod|0600|0644|latest|curl|wget|exec.Command|sh -c|sudo|insecure|skip.*verify|tls|idempot|changed_when|failed_when|creates:|removes:|--yes|--dry-run|confirm|destroy|purge|force' .
rg -n 'version|digest|componentImages|image:|uses:|go [0-9]|require |ansible-galaxy|collections:' go.mod go.sum ansible .github specs examples internal
go list -m all ; go list ./... ; go test ./... ; go vet ./... ; gofmt -l .
shellcheck scripts/* test/**/*.sh ; ansible-lint ; ansible-playbook --syntax-check <playbook>   # when available
```

Do not install tools, fetch dependencies, or require network for a review-only
audit unless the user allows it. Report useful checks that could not be run.

## Durable Guardrails

Verify each in the current repo before relying on it; do not recommend anything
that violates them:

- **Scope.** Stay inside Bootwright's stated scope; Day-2 fleet publication lives
  elsewhere unless the specs say otherwise.
- **Product API.** Desired-state YAML is the user API; generated artifacts are
  outputs, not authored source of truth.
- **Secrets.** Credentials, kubeconfigs, pull secrets, private keys, tokens,
  plaintext credentials, private absolute paths, and environment-specific values
  never appear in versioned content, examples, logs, generated docs, or snippets.
- **Rendered output.** Installer files, inventories, lock files, and
  effective-state snapshots do not inline secret bytes unless a documented
  sensitive-output path requires explicit opt-in and restrictive file modes.
- **Adapters.** Normalize provider and BMC variation through capabilities and
  adapters; keep unavoidable supplier workarounds isolated, tested, documented.
  Prefer official tool capabilities before custom orchestration.
- **Output.** CLI human output goes through `internal/cli/output`; raw exceptions
  stay raw (JSON, shell exports, Cobra help, prompts, external process passthrough).
- **Clean break.** `v1alpha1` may break cleanly: no migrations, aliases, shims, or
  legacy examples.

## Review Areas

Pick the lenses with teeth for the scope; do not run every bullet.

**Secret handling.** Committed or generated plaintext credentials, kubeconfigs,
pull secrets, private keys, tokens, BMC/vCenter/proxy/mirror credentials, or CA
material that should stay external; examples, fixtures, docs, logs, or error
strings teaching unsafe placement; missing redaction in CLI output, errors, Ansible
logs, snapshots, debug dumps, shell exports, or golden files; missing
`--sensitive`/`no_log` gates for paths that print or render secret-inlined
material; weak permissions on files or directories holding sensitive runtime state.

**Input, path, and command safety.** Unsafe path joins, traversal, symlink
exposure, archive extraction, temp-file handling, cleanup, or world-readable
output; shell interpolation, `sh -c`, unquoted vars, unsafe globbing, word
splitting, or command strings built from desired state; missing validation or
normalization of desired-state references, CLI flags, provider inputs, inventory,
URLs, image references, hostnames, BMC endpoints, and file paths; external process
execution without clear arguments, cancellation, timeout, or error reporting;
destructive commands or cleanup targets without constrained paths and safeguards.

**Idempotency and destructive-operation safety.** This is Bootwright's sharpest
edge — audit it hardest. Go orchestration that does not honor the spec's idempotent
apply contract (durable run ledgers, non-secret desired-input fingerprints,
ownership records, skip/resume guards, resource leases, safe repeated execution
when desired state is unchanged). CLI commands that can harm the environment with
no explicit operator-intent signal: scoped target selection, confirmation prompts,
`--yes` for non-interactive runs, `--dry-run` previews where supported, and output
naming the current context, selected clusters, affected hosts, BMC targets,
KubeVirt namespaces, storage objects, and installed-cluster resources *before*
mutating. A well-named, non-mutating desired-vs-real state-check command must
exist; it must report selected-root absence succinctly and granular drift for
existing roots, such as missing or undeclared Ceph pools, add-ons, VMs, services,
endpoints, or storage exports. Destructive paths that do not fail closed when
ownership is ambiguous, fingerprints are stale, shared services are still
consumed, a lock cannot be acquired, a kubeconfig or trust source is outside the
declared secret boundary, or a requested scope reaches resources outside the
selected desired-state graph. Evaluate commands with and without `--override`;
the flag may alter only the documented command-scoped unsafe-mismatch path and
must never make read-only checks mutate or suppress drift. Ansible tasks not
idempotent across reruns: shell/command without intentional
`changed_when`/`failed_when`/`creates`/`removes`, read-only probes marked changed,
cleanup that fails on already-absent resources, privileged partial state left
after failure. Provider/BMC/VM/storage/cluster mutations that bypass capability
checks, locks, ownership records, official-tool idempotency, or adapter
boundaries. Missing tests proving a second identical apply, role run, destroy
preview, state-check run, `--override`/no-override pair, or aborted confirmation
is a no-op, a precise drift report, or a safe refusal.

**Trust, TLS, and installer security.** TLS verification disabled without a narrow
documented reason; weak or hard-coded certs, keys, algorithms, or trust bundles;
cluster-install trust rendered from anything but explicit desired-state references;
disconnected or mirrored install paths that can silently reach untrusted or
unintended registries; proxy handling that leaks auth data or applies to the wrong
runtime boundary.

**Supply chain.** Unpinned Go modules, Ansible collections, tools, images, GitHub
Actions, downloads, or runtime image references; mutable tags (`latest`), omitted
or non-version tags, unverified downloads; pinned component versions behind the
latest stable upstream or vendor-supported channel without an accepted reason —
when offline, list the exact components whose latest-stable status still needs
external verification; prerelease/nightly/branch/commit/floating references treated
as stable without upstream or vendor-policy evidence; install paths assuming
network during normal validation; lock files, rendered image pins, and overrides
that do not enforce the spec's pinning rules.

**Privilege and provider boundaries.** Excessive sudo, service-account scope, BMC
access, host permissions, or container privilege; provider-specific behavior
leaking into cross-cutting orchestration; credentials reused across unrelated
provider, cluster, or host boundaries; lab emulation or shared services exposed
more broadly than needed; missing locks around shared hosts or BMC targets.

**Make and CI.** Make targets that do more than their name implies or hide
security-relevant side effects; CI jobs exposing secrets through logs, environment
dumps, artifacts, or unpinned actions.

## Output Format

Cite real files, packages, roles, commands, tests, and specs. Use current project
vocabulary.

# Bootwright Code Security Audit and Improvement Plan

## 1. Executive Summary
Three to seven bullets ordered by severity; each names the artifact, the risk, and
the recommended fix. Lead with the highest-severity exposure.

## 2. Reviewed Scope
Files, packages, roles, scripts, workflows, docs, or tests reviewed — and important
areas intentionally not reviewed.

## 3. Findings
Per finding: **Severity** (Critical/High/Medium/Low), **Location** (`path:line`),
**Evidence**, **Impact** (in Bootwright terms), **Fix** (smallest safe fix),
**Validation** (the test or command that proves it). If none, say so and list
residual risk and unreviewed areas.

## 4. Deliberately Unchanged
Patterns that looked exploitable but cleared on inspection, and hardening you
declined as disproportionate or as theater — each with the one-line reason. Proof
the findings are real exposures, not padding.

## 5. Hypotheses and Verification
Plausible risks needing one more focused check, each with that check. Keep separate
from confirmed findings.

## 6. Fix Plan
**Now** (small, high-confidence fixes for confirmed issues), **Next** (hardening
needing design agreement or broader tests), **Later** (lower-risk or larger-scope
deferred work). Per item: affected artifacts, smallest safe approach, validation,
and **Risk** (Low/Medium/High). Propose no shims, flags, or new dependencies unless
the evidence shows they are necessary.

## 7. Validation Notes
Commands run, commands that failed, and commands that could not run. For failures,
the blocker and the next useful step.

## Constraints

- Cite real repo evidence; invent no vulnerabilities; use current vocabulary.
- Respect the durable guardrails; verify their current form in `specs/` first.
- Keep secrets out of every finding, snippet, and recommendation.
- Every recommendation must pass the Aggregation test — prefer fewer, stronger,
  evidence-backed fixes, and say plainly when the current state is already safe.
