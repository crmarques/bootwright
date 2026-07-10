# .ansible-lint skip_list and .yamllint rules: each skip and why

**Strategy:** `.ansible-lint` runs the FULL default rule set (not a weak named
profile) and skips only rules that conflict with deliberate, reviewed house
style. The tree already passes every high-value rule (no-changed-when,
no-log-password, risky-shell-pipe, command-instead-of-module,
risky-file-permissions, ...), so the gate mechanically catches regressions in
exactly those classes. Ratchet up by removing skips over time; do not
retro-fix existing house-style hits just to drop a skip.

**skip_list rationale (do not remove an entry without addressing its reason):**

- `yaml` — YAML formatting is governed by yamllint via `.yamllint`, not
  ansible-lint; running both duplicates diagnostics.
- `var-naming[no-role-prefix]` — repo-wide convention: variables use one
  global `bootwright_` prefix rather than a per-role prefix.
- `name[play]` — orchestration playbooks are thin import_playbook/strategy
  plays that are not individually named by design.
- `name[template]` — a few task names embed Jinja mid-string for legibility.
- `run-once[play]` / `run-once[task]` — `strategy: free` and
  run_once-under-free are intentional, reviewed idioms.
- `no-handler` — inline `when: <result> is changed` reactions (systemd
  reloads, trust-store updates, ownership-record writes, destroy-path
  cleanups) are used by design; handler/flush semantics do not fit the
  run_once / strategy:free / destroy flows.
- `jinja[invalid]` — static type inference cannot resolve list-typed vars at
  lint time and reports spurious "can only concatenate str/list" on
  runtime-valid templates.
- `meta-runtime[unsupported-version]` — `requires_ansible ">=2.20.0"` is
  intentional; the collection targets ansible-core 2.20+.
- `galaxy[no-changelog]` / `galaxy[no-license]` / `galaxy[no-repository]` —
  embedded collection; galaxy metadata is deliberately minimal.
- `key-order[task]` — cosmetic task-key ordering; not worth churn.

`warn_list` keeps `jinja[spacing]` visible without failing the gate.

**.yamllint rationale:** structural rules stay on (indentation, duplicate
keys, trailing whitespace, final newline) so the gate is signal, not noise:

- `line-length: disable` — long inline Jinja/`when` expressions are
  deliberate and span to ~490 columns.
- `braces: disable` — yamllint parses `{{ ... }}` and inline filter dicts as
  flow mappings and produces spurious "too many spaces inside braces" on
  Jinja.
- `truthy: check-keys: false` — allow `yes`/`no`/`on` used as keys (CI yaml).
- `ignore: .github/` — workflow YAML is covered by the separate
  `workflow-yaml-check` make target with its own inline config.

**How the gate runs:** `make ansible-lint-check` runs
`yamllint -c .yamllint ansible` from the repo root, then `ansible-lint
--offline -c <repo>/.ansible-lint` FROM the collection root
`ansible/collections/ansible_collections/bootwright/core` so role/playbook
auto-discovery works, with the same `ANSIBLE_COLLECTIONS_PATH` the syntax
check uses so `community.*` dependencies resolve offline.
