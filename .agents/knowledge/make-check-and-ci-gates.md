# make check targets and CI gates: ordering and rationale

**Tiers (ADR 0041):** `check-scoped` (bug fix) and `check-feature` (new
feature, kind, flag, field) run only what the diff against `CHECK_BASE` can
break, via `scripts/select-checks.py`; `check-fast` runs the whole Go suite;
`check-full` is the release gate. `make check` is retired. Intent selects the
floor, never the ceiling — the package set comes from the diff and the import
graph, and the selector fails open on an unresolvable base, an oversized diff,
or an edit to `Makefile`, `go.mod`, `go.sum`, or the selector itself.

**Ordering:** check targets run cheapest-to-slowest so local runs fail on
lightweight guardrails before starting race or clean-checkout tests. Every
gate depends on `sync-bundle` FIRST so the embedded ansible bundle matches the
source tree before Go tests run; otherwise
`TestEmbeddedBundleMatchesSourceAnsible` fails (or skips) against a stale or
absent gitignored bundle artifact. Since ADR 0041 the bundle is a real file
target rather than `.PHONY`, so an unchanged `ansible/` tree makes it a no-op
(2.19s to 0.21s) — but note that a `main` advance touching `ansible/` does
leave the locally built zip stale, and that test is how it surfaces.

**Race timeout:** `internal/cli` is a large integration-style package that
runs ~12 minutes under `-race` on slower machines, past Go's 600s/package
default. `GO_TEST_RACE_FLAGS` carries `-timeout 1800s` so the race run fails
on real races, not the clock. The `Go race test` step in
`.github/workflows/checks.yml` must keep matching this headroom
(`go test -race -timeout 1800s ./...`).

**go-mod-tidy-check:** backs up go.mod/go.sum, runs `go mod tidy` in place,
and diffs against the backup; an EXIT trap restores the backup on any exit
including interrupts, so the working tree is left untouched. A tidy failure
itself (proxy/network error) fails the target instead of yielding a false
green from an unchanged-but-unverified tree.

**python-test:** stdlib `unittest discover` only, so the check works on any
Python 3 install without a venv; pytest, if installed locally, discovers the
same TestCase classes. Plugin tests stay in their semantic sibling suites under
`tests/unit/plugins/{filter,callback,modules}`; the Makefile enumerates each
suite explicitly because unittest discovery does not recurse through those
non-package directories. `TestPythonTestDiscoversEveryPluginUnitSuite` fails
when a new test-bearing plugin suite is omitted.

**Lint duty split:** yamllint (via `.yamllint`) handles YAML formatting;
ansible-lint handles Ansible semantics (idempotency, no_log,
command-vs-module). See ansible-lint-yamllint-config.md for the skip
rationale and invocation details.

**shellcheck-check:** discovers authored shell scripts by SHEBANG (not
extension) over `ansible/` and `scripts/`, excluding `.git` and `.state`
(build-time downloaded collections), and runs `shellcheck -x` so sourced
fragments are analysed. A missing shellcheck binary produces an install hint
and a FAILURE — no silent no-op — and zero discovered scripts also fails
(broken filter detection).

**workflow-yaml-check:** exists because `.yamllint` is Ansible-tuned and
ignores `.github/`, so workflow YAML would otherwise be caught by nothing
locally (a duplicate key surfacing only when GitHub runs it). It passes an
inline yamllint config keeping structural rules (key-duplicates, indentation,
trailing whitespace) while relaxing document-start (workflows omit the
leading `---`), line-length, truthy check-keys, and comments-indentation.

**Ansible temp dirs:** `ANSIBLE_LOCAL_TEMP`/`ANSIBLE_REMOTE_TEMP` are
anchored under the repo-local `.state` dir instead of a fixed
world-predictable path in shared /var/tmp: the shared path breaks multi-user
hosts (whoever creates it first owns it) and is squattable; the state dir is
per-checkout and cleaned by `make clean`.

**Short disk-backed `TMPDIR`:** The SSH control-path tests create their
`XDG_RUNTIME_DIR` below `TMPDIR` and enforce Linux's short AF_UNIX path budget.
A deeply nested repo cache is disk-backed but makes those tests return an empty
control path and fail with `stdout missing ANSIBLE_SSH_CONTROL_PATH_DIR`. Use a
private, real `/var/tmp/bwct-<run>` directory as `TMPDIR`; a symlink to the
repo-local cache makes managed-root tests fail with `must not contain symlink`.
Keep `GOTMPDIR`, `GOCACHE`, `GOMODCACHE`, and `STATE_DIR` on their direct
disk-backed paths.

**Go VCS stamping below a false parent repository:** Some restricted local
environments expose an empty, read-only `/tmp/.git` directory. A linked Git
worktree below `/tmp` has a `.git` file, which Go's VCS-root detector ignores;
it then climbs to the parent `.git` directory, runs `git status` from `/tmp`,
and fails with `error obtaining VCS status: exit status 128`. For checks in
that environment, set `GOFLAGS=-buildvcs=false`: this disables only automatic
build-info stamping while the compiler, tests, vet, and staticcheck still
inspect the same sources. Keep the workaround local to the temporary
worktree; CI and ordinary repositories must retain their normal environment,
and Bootwright release metadata remains supplied by the Makefile's explicit
linker flags.

**Staticcheck cache in a read-only home:** Staticcheck writes analysis facts
under the XDG cache independently of `GOCACHE`. In a restricted environment it
can therefore fail with `failed caching data: open .../.cache/staticcheck/...:
read-only file system` even when Go compilation succeeds. Point
`XDG_CACHE_HOME` at a direct writable per-run directory alongside the existing
`GOTMPDIR` and `GOCACHE` paths. Do not replace `HOME` to work around the cache.

**Clean-checkout bundle exclusion:** `go-test-clean-checkout` deliberately
omits the gitignored `internal/converge/bundle/ansible_bundle.zip`; its purpose
is to prove the authored checkout, not a developer's generated artifact. A CLI
test that intentionally proceeds into a workflow mutation must install the
existing `SetWorkflowBundlePreparerForTest` seam (the safety matrix wraps it in
`seedSafetyWorkflowBundle`) before invoking the command. Otherwise the test
passes after `sync-bundle` locally but fails the clean-checkout gate with
`missing ansible_bundle.zip`, masking the behavior it meant to exercise.

**Hermetic safety-matrix tools:** A matrix fixture that deliberately advances
through controller preflight must resolve its prerequisite binaries through the
test dependency seam. `--stage clusters` includes add-ons, so a runnable fixture
that leaves `preflight.DefaultDeps.LookPath` live can pass on a workstation with
`oc` and fail in CI with `Installer tools: oc: not found` before reaching the
intended install-record refusal. Keep optional operator tools out of the workflow
image; make the fixture hermetic while leaving real runtime execution on the
package-level command stubs.

**CI gates:**

- `checks.yml` runs the full suite on `v*` release tags (not just PRs/main
  pushes) because `release.yml` only runs `go test ./...` before shipping
  binaries — tagging an unvetted commit must still pass staticcheck, race,
  lint, and the repo guardrails before release artifacts ship.
- The `Docs strict build` step runs `make docs-check` on every PR so
  strict-mode breakage fails in checks.yml rather than silently at pages.yml
  publish time, and so CI and the local target cannot drift. `docs-check`
  invokes `$(PYTHON) -m mkdocs`, not the `mkdocs` console script: on Fedora
  that script's `#!/usr/bin/python3 -sP` shebang suppresses user site-packages
  and hides `mkdocs-material`, making the target unrunnable locally.
- The `pages.yml` publish gate republishes when a push touches `docs/` or
  `mkdocs.yml` even under a `feat:`/`fix:` commit subject (in addition to
  `docs:`-subject and `v*`-tag triggers), because synchronized code+docs
  commits are the norm and would otherwise leave the published site stale.
