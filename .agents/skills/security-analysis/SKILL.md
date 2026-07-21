# Security Analysis Skill

Use this skill to act as a senior software security analyst: review changed
or specified code, manifests, playbooks, and configuration to identify
security issues and propose or apply fixes.

## Load First

- `/specs/security.md`
- `/specs/architecture.md`
- `/specs/state-model.md`
- `/.agents/skills/definition-stewardship/SKILL.md` when reviewing docs,
  examples, or E2E fixtures

## Review Scope

- Secret handling: plaintext credentials, kubeconfigs, pull secrets, private
  keys, tokens, BMC passwords, or any sensitive runtime material in tracked
  files, examples, fixtures, logs, or generated manifests.
- Input handling: validation and normalization of desired state, CLI flags,
  inventory, and provider inputs; injection risks in shell, templating, SQL,
  or YAML/JSON loading.
- Provider and adapter boundaries: privilege scoping, kubeconfig and service
  account permissions, BMC and IPMI/Redfish credentials, network exposure of
  lab emulation.
- Supply chain: pinned versions and digests for external tools, container
  images, Ansible collections, and Go modules; absence of floating tags
  (e.g. `latest`); verification of downloaded artifacts.
- File system and process safety: command construction, path traversal,
  unsafe permissions on rendered output, temp file handling.
- Destructive-operation safety: idempotency and ownership of delete, undefine,
  wipe, zap, format, reset, and remove paths; that read-only commands (`status`,
  `diff`, `render`, `plan`, `validate`, `preflight`) never mutate or contact
  hosts; `destroyProtection` and the command-scoped gate flags — apply
  `--converge-drifted` (with `--confirm-data-loss` for data-loss rebuilds) and
  destroy `--force` — enforced before any mutation, with `--yes` as confirmation
  only that never broadens scope; destroy acting on proven Bootwright ownership
  and the selected scope, failing closed on foreign, stale, or out-of-scope state.
- Logging and telemetry: leakage of secrets, tokens, or private host data
  into logs, errors, or rendered output.
- Cryptography and TLS: certificate validation, trust stores, weak or
  hard-coded algorithms or keys.

## Method

- Enumerate findings with severity (critical, high, medium, low) and a
  concrete location (`file_path:line_number`).
- For each finding, state the impact and the minimal, in-scope fix.
- Prefer fixing root causes over adding compensating checks.
- Do not introduce backwards-compatibility shims, feature flags, or new
  abstractions that the fix does not require.
- Apply fixes only within the scope the user requested; surface
  out-of-scope risks as findings rather than silently expanding the change.
- Re-run repository validation appropriate to what changed (formatting,
  unit tests, lint) and report any check that could not be run.

## Output

- A short findings list (severity, location, impact, fix).
- The applied diff when fixes are in scope, or proposed patches when they
  are not.
- Explicit note when no security issues are found in the reviewed scope.
