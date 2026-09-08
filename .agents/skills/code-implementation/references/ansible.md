# Ansible Engineering

[Architecture](../../../../specs/architecture.md) owns Go/Ansible ports,
playbooks, roles, and plugins; [security](../../../../specs/security.md) owns
effect and secret boundaries; [source formatting](formatting.md) owns layout.
Before editing a managed operation, resolve its frozen request, result/evidence,
implementation identity, targets/versions, permitted effects, replay,
cancellation, and secret path. Lifecycle policy remains in Go.

## Implementation

- Map canonical FQCNs to the pinned core/collection versions; never depend on
  `collections:` or ambient lookup. Pin standalone roles to an immutable source
  and use a fixed resolver.
- Keep playbooks and roles thin: validate one bounded request, invoke selected
  operations, and normalize a bounded result. Task stdout/events are not the
  product API. No production partial controls may bypass validation, evidence,
  cleanup, or lifecycle semantics. Keep role dependencies and task-file
  selection explicit rather than driven by input.
- Use `meta/argument_specs.yml` for input entrypoints when supported; add
  exact-type and semantic assertions before effects and prefix role-owned
  variables/results. Write `when`, `until`, `changed_when`, and `failed_when` as native
  Boolean expressions without Jinja delimiters. Never re-evaluate input/output
  as Jinja. Disable implicit facts; gather only contract-required facts.
- For a selected native executable, use `ansible.builtin.command` with its
  absolute identity and `argv`. Set `expand_argument_vars: false` for literal
  arguments when supported; otherwise prevent expansion by validated
  construction or reject that core version. Define exact change/failure
  conditions. Do not use `shell`, `raw`, `script`, or shell-evaluating lookups
  to bypass the process boundary; lint suppression grants no effect authority.
- Set owner, group, and quoted mode when security or reproducibility depends
  on them; use validation and atomic publication when available. Scope `become`
  to the operation's least privilege. Give handlers role-qualified names/listen
  topics, notify only on real change, and flush and normalize failures when
  handler completion contributes to success or evidence.
- Derive strategy, `serial`, throttling, and fatality from the operation contract.
  `linear` means lockstep within a batch; `any_errors_fatal` is not rollback.
  Concurrency, `async`, delegation, `run_once`, and shared targets need explicit
  ordering, exclusion, cancellation, and partial-failure tests. Bound waits
  through module/task/runner timeouts; retries must preserve replay safety.
- Pin the full execution closure, including core, lint, collection/role
  artifacts, controller packages/SDKs, images, and tools. Set `ansible_python_interpreter`
  explicitly or qualify `auto`. Verify each artifact independently:
  `ansible-galaxy collection verify` does not verify dependencies. Never resolve
  or update dependencies at runtime; review porting notes when pins change.
- Set `no_log: true` and `diff: false` for secret-bearing tasks and `no_log` on
  sensitive module arguments; never enable `ANSIBLE_DEBUG`. Trace disclosure
  through callbacks, verbosity, failure, and result normalization. Combine
  runner-owned cleanup with appropriate `always` cleanup; neither guarantees
  remote cleanup after loss of reachability or process death.
- Keep custom adapters within their consumer's effect boundary. For a module,
  use argument specs, bounded sanitized results, explicit `changed`, and truthful
  `exit_json`/`fail_json` outcomes. Other plugin forms need equivalent semantics. Claim check/diff
  support only when non-mutation and output behavior are implemented.

## Tests and gates

Use pinned repository entrypoints for syntax checks and `ansible-lint`. For
collection module/plugin content, use collection-scoped `ansible-test` sanity,
unit, and focused integration tests. Verify that runtime FQCNs and references
resolve from the expected locked versions.

With explicit isolated inventory, test each entrypoint's request/result
contract, first execution, and replay. Convergence should report no change;
destructive replay must establish positive absence or typed refusal. Cover
applicable invalid inputs, unknown probes, wrong identity/ownership,
unsupported/drifted variants, privilege, timeout, cancellation, later handler
failure, partial progress, ambient dependencies, and disclosure. Check mode is
neither planning nor authorization.

Run the shared contract suite across production implementations and supported
version boundaries. Go unit tests use injected ports rather than ambient
Ansible. Real-system qualification remains a separate acceptance gate. Report
unavailable, skipped, flaky, and failed checks under the shared workflow.
