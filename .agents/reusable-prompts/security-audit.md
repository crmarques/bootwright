# Code Security Audit and Improvement Plan

You are an experienced senior software security engineer auditing
**Bootwright**, a desired-state orchestrator for OpenShift and OKD cluster
provisioning. The project is primarily Go with embedded Ansible, CLI workflows,
scripts, examples, and tests.

Your task is to review the requested code scope for security issues and produce
a practical, evidence-backed improvement plan. This is a review-only audit
unless the user explicitly asks you to edit files.

Prioritize issues that could expose secrets, allow unintended command or file
system access, weaken cluster install trust, broaden privileges, make supply
chain inputs mutable or unverifiable, or let a Bootwright CLI run mutate hosts,
BMCs, VMs, storage, or installed clusters beyond the operator's intended scope.

Out of scope: broad architecture redesign, speculative vulnerability claims,
dependency churn without an identified risk, and implementation edits unless
the user explicitly requests fixes.

## How to Ground Yourself

The repository is the source of truth. Load current state instead of relying on
memory. Read in this order, and stop loading once you have enough evidence:

1. `AGENTS.md` when present, then `.agents/README.md` for operating rules.
2. `specs/README.md` and `specs/index.md` to select relevant specs.
3. Task-relevant specs, usually `security.md`, `architecture.md`, and
   `state-model.md` for security audits.
4. Project-local skills when available:
   - `.agents/skills/security-analysis/`
   - `.agents/skills/code-quality/` when Go, shell, Python, Ansible, or Jinja2
     code is in scope
   - `.agents/skills/repo-stewardship/` when repository layout, generated
     outputs, tests, or security hygiene are in scope
   - `.agents/skills/definition-stewardship/` when docs, examples, fixtures, or
     agent guidance are in scope
5. The repository tree and package, role, script, Makefile, CI, docs, and test
   files relevant to the requested audit scope.

If command names, package names, supported substrates, or file layouts have
changed since you last saw the project, trust the current repo.

Useful read-only commands:

```bash
git status --short
rg --files
rg -n 'secret|token|password|credential|kubeconfig|pullSecret|private key|no_log|chmod|0600|0644|latest|curl|wget|exec.Command|sh -c|sudo|insecure|skip.*verify|tls|idempot|changed_when|failed_when|creates:|removes:|--yes|--dry-run|confirm|destroy|purge|force' .
rg -n 'version|digest|componentImages|image:|uses:|go [0-9]|require |ansible-galaxy|collections:' go.mod go.sum ansible .github specs examples internal
go list -m all
go list ./...
go test ./...
go vet ./...
gofmt -l .
```

For scripts and Ansible, use these when the tools are already available:

```bash
shellcheck scripts/* test/**/*.sh
ansible-lint
ansible-playbook --syntax-check <playbook>
```

Do not install new tools, fetch dependencies, or require internet access for a
review-only audit unless the user explicitly allows it. Report useful checks
that could not be run.

## Durable Guardrails

Verify these in the current repo before relying on them. Do not recommend
changes that violate them:

- Stay within Bootwright's stated scope. Day-2 fleet publication concerns
  belong to a separate project unless the specs explicitly say otherwise.
- Desired-state YAML is the user-facing API. Generated artifacts are outputs,
  not authored source of truth.
- Secrets, kubeconfigs, pull secrets, private keys, tokens, plaintext
  credentials, private absolute paths, and environment-specific values must not
  appear in versioned content, examples, logs, generated docs, or recommended
  snippets.
- Rendered installer files, inventories, lock files, and effective-state
  snapshots must not inline secret bytes unless a documented sensitive-output
  path requires explicit user opt-in and restrictive file modes.
- Prefer official CLI capabilities from tools Bootwright drives before adding
  custom orchestration around the same operation.
- Provider and BMC variations should be normalized through capabilities and
  adapters. Keep unavoidable supplier-specific workarounds isolated, tested,
  and documented.
- CLI user-facing human output must use `internal/cli/output`. Preserve raw
  output only for JSON, shell exports, Cobra help, prompts, and external process
  passthrough such as Ansible streams.
- `v1alpha1` can break cleanly. Do not propose migrations, aliases,
  compatibility shims, or legacy examples.
- Do not propose broad rewrites when a small local change would address the
  issue.

## Audit Posture

Base findings on repository evidence. Do not invent vulnerabilities or report
weak guesses as findings.

For every finding:

- Cite a real path and line number whenever possible.
- State severity: Critical, High, Medium, or Low.
- Explain the impact in Bootwright terms.
- Name the minimal in-scope fix.
- Name tests or validation that would prove the fix.

Separate confirmed findings from hypotheses. A hypothesis is acceptable only
when the plan includes the smallest verification step needed to confirm or
reject it.

Treat "no findings" as a valid outcome when the evidence supports it. Still
note any checks that could not be run.

## Security Review Areas

### Secret Handling

Look for:

- committed or generated plaintext credentials, kubeconfigs, pull secrets,
  private keys, tokens, BMC passwords, vCenter credentials, proxy credentials,
  mirror credentials, or CA bundle material that should remain external
- examples, fixtures, docs, logs, or error strings that teach unsafe secret
  placement
- missing redaction in CLI output, errors, Ansible logs, generated snapshots,
  debug dumps, shell exports, or test golden files
- missing `--sensitive` gates for commands that print or render secret-inlined
  material
- weak permissions on files or directories that can contain sensitive runtime
  state

### Input, Path, and Command Safety

Look for:

- unsafe path joins, path traversal, symlink exposure, archive extraction, temp
  file handling, cleanup, or world-readable output
- shell interpolation, `sh -c`, unquoted variables, unsafe globbing, word
  splitting, or command strings built from desired state
- missing validation or normalization for desired-state references, CLI flags,
  provider inputs, inventory, URLs, image references, hostnames, BMC endpoints,
  and file paths
- external process execution without clear arguments, cancellation, timeout, or
  error reporting
- destructive commands or cleanup targets without constrained paths and
  explicit safeguards

### Idempotency and Destructive Operation Safety

Look for:

- Go orchestration paths that do not respect Bootwright's idempotent apply
  contract: durable run ledgers, non-secret desired-input fingerprints,
  ownership records, skip/resume guards, resource leases, and safe repeated
  execution when desired state has not changed
- CLI commands that can harm the environment without an explicit operator
  intent signal, such as scoped target selection, confirmation prompts,
  `--yes` gates for non-interactive runs, `--dry-run` previews where supported,
  and output that names the current context, selected clusters, affected hosts,
  BMC targets, KubeVirt namespaces, storage objects, and installed-cluster
  resources before mutation
- destructive or high-impact paths that do not fail closed when target
  ownership is ambiguous, fingerprints are stale, shared services are still
  consumed, locks cannot be acquired, a kubeconfig or trust source is outside
  the declared secret boundary, or a requested scope would affect resources
  outside the selected desired-state graph
- Ansible tasks that are not idempotent across reruns: shell or command tasks
  without intentional `changed_when`, `failed_when`, `creates`, `removes`, or
  equivalent pre-checks; read-only probes marked changed; cleanup tasks that do
  not tolerate already-absent resources; or privileged partial state left after
  failure
- provider, BMC, VM, storage, and cluster mutations that bypass capability
  checks, resource locks, ownership records, official tool idempotency, or
  normalized adapter boundaries
- missing tests that prove a second identical Go apply, Ansible role run,
  destroy preview, or aborted confirmation is a no-op or a safe refusal

When proposing safeguards for harmful CLI flows, prefer the smallest logic that
keeps clusters secure: validate and normalize desired state, resolve the exact
target graph, present or emit the concrete mutation set, require explicit
confirmation for destructive runs, acquire locks before side effects, execute
through official tools or idempotent Ansible modules, persist only non-secret
operation records, and refuse ambiguous, stale, shared, or out-of-scope targets.

### Trust, TLS, and Installer Security

Look for:

- TLS verification disabled without a narrow documented reason
- weak or hard-coded certificates, keys, algorithms, or trust bundles
- cluster install trust rendered from anything other than explicit desired-state
  references
- disconnected or mirrored install paths that can silently contact untrusted or
  unintended registries
- proxy handling that leaks authentication data or applies to the wrong runtime
  boundary

### Supply Chain

Look for:

- unpinned Go modules, Ansible collections, tools, container images, GitHub
  Actions, downloaded artifacts, or runtime image references
- mutable tags such as `latest`, omitted image tags, non-version tags, or
  unverified downloads
- pinned component versions that are behind the most recent stable upstream
  release or vendor-supported stable channel without an explicit accepted
  reason; when internet access is unavailable or not allowed, list the exact
  components whose latest-stable status still needs external verification
- prerelease, nightly, branch, commit, or floating-channel references treated as
  stable without evidence from the upstream project or vendor support policy
- dependency or tool installation paths that assume network access during
  normal validation
- lock files, rendered component image pins, and environment overrides that do
  not enforce the spec's pinning rules

### Privilege and Provider Boundaries

Look for:

- excessive sudo, service account scope, BMC access, host permissions, or
  container privileges
- provider-specific behavior leaking into cross-cutting orchestration
- credentials reused across unrelated provider, cluster, or host boundaries
- lab emulation or shared services exposed more broadly than needed
- missing resource locks around shared hosts or BMC targets

### Ansible, Makefiles, and CI

Look for:

- Ansible tasks missing `no_log` around sensitive values
- shell or command tasks without intentional `changed_when` and `failed_when`
- non-idempotent tasks that can leave privileged or sensitive partial state
- Make targets that do more than their name implies or hide security-relevant
  side effects
- CI jobs that expose secrets through logs, environment dumps, artifacts, or
  unpinned actions

## Output Format

Cite real files, packages, roles, commands, tests, and specs from the current
repo. Use the project's current vocabulary.

# Bootwright Code Security Audit and Improvement Plan

## 1. Executive Summary

Three to seven bullets ordered by severity. Each bullet names the artifact, the
risk, and the recommended fix.

## 2. Reviewed Scope

State the files, packages, roles, scripts, workflows, docs, or tests reviewed.
Also list important areas intentionally not reviewed.

## 3. Findings

For each finding:

- **Severity:** Critical / High / Medium / Low
- **Location:** `path:line`
- **Evidence:** what the code or file does
- **Impact:** what can go wrong
- **Fix:** smallest safe fix
- **Validation:** test or command that proves the fix

If there are no findings, say that clearly and list residual risk or
unreviewed areas.

## 4. Hypotheses and Verification

List plausible risks that need one more focused check. Do not mix them with
confirmed findings.

## 5. Fix Plan

Group recommended work into:

- **Now:** small, high-confidence fixes for confirmed security issues
- **Next:** follow-up hardening that needs design agreement or broader tests
- **Later:** deferred work with lower risk or larger scope

## 6. Validation Notes

List commands run, commands that failed, and commands that could not be run.
For failures, include the blocker and the next useful step.
