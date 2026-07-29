# Code Quality Skill

Use this skill when adding, modifying, deleting, or reviewing code in this
repo (Go, Python, shell, Ansible YAML, Jinja2 templates) to keep the source
clean and idiomatic, and to guarantee that no unused parameters, functions,
methods, types, variables, imports, or fields are left behind.

## Load First

- `/specs/architecture.md` (Testing section)
- The Repository Layout section in `/README.md`

## Required Checks

Run focused checks from the repo root when they directly answer the current
implementation question, and report any check that could not be run, including
the reason:

- `gofmt -l .` — must print nothing. If anything is listed, run
  `gofmt -w` on the listed paths and re-run.
- `go vet ./...` — must report no findings.
- `go build ./...` — the Go compiler rejects unused imports and unused
  local variables. A clean build is the baseline for "nothing unused".
- `staticcheck ./...` — must report no findings. Pay special attention
  to `U1000` (unused). If `staticcheck` is not installed, install it
  once with:
  `go install honnef.co/go/tools/cmd/staticcheck@v0.7.0`
  and ensure `$(go env GOPATH)/bin` is on `PATH`.

Before declaring an implementation task done, run `make check-fast` through the
implementation validation skill. Do not run `make check` by yourself; run it
only when the user explicitly requests that full gate.

## Standards

- No dead code. Delete unused exported and unexported functions,
  methods, types, constants, variables, and struct fields rather than
  leaving them "for later" or commented out.
- No unused imports or unused local variables. Never silence the
  compiler with blank-identifier (`_`) imports unless the import is
  genuinely needed for its side effects; in that case record the reason
  in `.agents/knowledge/`, not in a comment.
- No unused parameters. Remove parameters that are never read inside
  the function body. The single exception is when the parameter is
  required to satisfy an interface, an embedded function type, or an
  external callback signature; in that case rename the parameter to
  `_` if the name does not aid readability.
- No unused return values. If a return value is never consumed at any
  call site within the repo, drop it from the signature instead of
  ignoring it at every call.
- No backwards-compatibility shims, renamed `_var` placeholders, or
  `// removed` markers left in place of deleted code. Delete cleanly.
- No speculative abstractions. Three similar lines is better than a
  premature interface, generic helper, or options struct introduced
  "just in case".
- No comments. Code is the documentation; names and structure carry the
  meaning. Applies to every language in this repo (Go `//`, Python/shell/
  YAML `#`, multi-line forms) and to every kind of comment — explanations,
  rationale ("why"), fix/refactor notes, TODOs, section banners, field
  docs, godoc, and option enumerations alike. The guard tests in
  `internal/repo/checks/comment_policy_test.go` enforce this for Go, YAML
  (ansible/examples/test/e2e/add-ons/.github), Makefile, Containerfile,
  and shell.
  The only comments allowed are machine-read directives, which are not
  prose; `specs/adr/0006-no-prose-comments-knowledge-catalog.md` enumerates
  them, and the allowlist regexes in `comment_policy_test.go` are what
  actually decide a borderline case.
  Every piece of information a comment would have carried has a proper
  home; put it there instead, in the same change:
  - Incident/root-cause/vendor-quirk/constraint knowledge (anything that
    explains *why* code is shaped a certain way or why a class of bug
    exists) → a file in `.agents/knowledge/` plus an index row in
    `.agents/knowledge/KNOWLEDGE.md`.
  - Design decisions with durable rationale → an ADR in `specs/adr/`.
  - Schema/field semantics, defaults, allowed values, cross-field
    invariants → `specs/state-model.md` or the kind's page under
    `docs/concepts/`.
  - Rendered-variable contracts for Ansible roles → the collection's
    `docs/vars-contract.md`.
  If the *why* fits in the code (rename a var, split a function, extract
  a constant, tighten a type), do that first — a knowledge file is for
  what the code genuinely cannot say.
  Exception: comment-like lines that are *content*, not commentary, stay;
  ADR 0006 states which ones and why.

## Ansible Idempotency And Destructive Safety

The Ansible layer performs every host mutation; hold it to the same safety bar as
the Go layer.

- Prefer modules with native idempotency (`file`, `package`, `systemd`,
  `containers.podman.*`) over raw `command`/`shell`. When a `command`/`shell`
  task is unavoidable, set `changed_when`/`failed_when` deliberately and make it
  idempotent with `creates`/`removes` or an explicit guard.
- Mark read-only probes `changed_when: false` (and `failed_when: false` when a
  probe failure must not abort). A probe must never report a change or trigger a
  handler.
- Gate every destructive task (delete, undefine, wipe, zap, format, reset,
  remove, disable) on proven Bootwright ownership and the requested scope, never
  on names, labels, or filesystem layout alone. Re-verify live ownership (an
  ownership record or a stamped marker/label) before an irreversible action, and
  fail closed on a foreign or stale match.
- Constrain path and device removal to allowlisted, context-owned roots; reject
  `..`, and refuse mounted, in-use, or system devices before wiping.
- A destructive role must be safe to re-run: a second identical run is a no-op or
  tolerates already-absent state without error.
- Never log secrets: gate `no_log` on every task that handles credentials through
  the standard form `no_log: "{{ bootwright_no_log | default(true) | bool }}"`
  (for conditionally-sensitive tasks, AND the sensitivity condition after that
  gate). This redacts by default yet honors the operator's `--verbose` override.
- Which resources to act on, the scope, and the authorization come from
  Go-rendered variables, not from Ansible re-deriving them.

## Method

- Before finishing, run `make check-fast` and fix every finding within the
  scope of the change.
- When deleting a function, also delete its tests and any helpers it
  was the last caller of; run `staticcheck` when needed to surface the next
  layer of newly-unused code, and repeat until clean.
- When a finding is genuinely out of scope, surface it as a note in
  the handoff rather than silently expanding the diff.
- Prefer fixing the root cause (delete the dead symbol, drop the
  unused parameter) over compensating annotations such as `//nolint`
  or `_ = unusedVar`.
