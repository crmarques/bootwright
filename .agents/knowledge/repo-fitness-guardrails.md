# Repo fitness tests: boundaries a change must not cross

**Import matrix is total:** `allowedImports` in
`internal/repo/checks/import_matrix_test.go` is the per-package
internal-import allowlist for production code (test files exempt); `"*"` is
reserved for the presentation composition root `internal/cli`. Every package
with non-test Go files must have a row and every row must name an existing
package, so adding a package or an import is a deliberate matrix edit — the
matrix cannot silently go stale.

**Layering intents behind the matrix rows:** `internal/cli` is the only
importer of `internal/cli/output` (services return plain data; cli renders);
`internal/state/advice` is read-only advisory analysis over a validated
State; host primitives are generic — importable by everyone, importing only
`internal/host/*`, never schema or domain; `internal/render` root is the only
published render surface with fs-free one-way families (inventory ->
installer, inventory -> ceph); converge subpackages never import the converge
root.

**Process-exec seam:** only `internal/host/` and `internal/converge/ansible/`
may import `os/exec` (`internal/repo/checks/exec_boundary_test.go`). All
other production code launches processes through
`internal/host/execution`'s `Runner` so tests can substitute a fake; callers
outside `internal/host` pass `become.DefaultCommandFactory` instead of
importing `os/exec`.

**Role-name single source:** `internal/roles` is the only place
`bootwright.core.` role and playbook name strings may be spelled in Go
production code (role-contract fields plus the `Playbook*` constants in
`internal/roles/playbooks.go`); a string scan enforces it. Role names are
projected into rendered vars — nothing else hard-codes them. (Dispatch
design: `specs/adr/0002-ansible-provider-dispatch.md`.)

**Human-output guard:** `TestHumanOutputUsesOutputPackage`
(`internal/cli/output_guard_test.go`) forbids direct `fmt.Fprint*`/
`fmt.Print*` in `internal/cli` outside a small filename allowlist
(become_password.go, command_tty_linux.go, context.go, output/output.go,
root.go, scope_print.go, shell_env.go). It scans raw file text, so even a
comment containing `fmt.Fprintln(` in a non-allowlisted file trips it.

**File-size budgets are floors at observed maxima:** `CLI_FILE_LINE_LIMIT=400`
(internal/cli thin handlers; init.go ~391 is the floor — domain logic belongs
in internal/converge/workflow/), `WORKFLOW_FILE_LINE_LIMIT=1000`,
`VALIDATOR_FILE_LINE_LIMIT=900` (internal/state/desired and
internal/state/graph — split by kind when crossed), `API_FILE_LINE_LIMIT=600`
(api/v1alpha1 — a crossing file means a kind's types should split into their
own `storage_<kind>.go`-style file). `_test.go` files are excluded. Do not
raise a limit without a deliberate refactor justification.

**stale-term-check scope:** `DEFINITION_CHECK_PATHS` in the Makefile
deliberately excludes `specs/adr` — ADRs intentionally describe the
abandoned/old shape in their Context sections and would false-positive.

**Diagnostics vocabulary:** `TestDiagnosticsSpeakAuthoredFieldVocabulary`
(`internal/state/desired/validate_vocabulary_test.go`) greps validator
sources for retired field spellings so a diagnostic can never point users at
a YAML path that no longer exists; every denylist entry names the authored
replacement. Mentioning a retired spelling even inside a string fails it.

**Docs YAML must load:** `TestDocsSnippetsStrictDecode`
(`internal/state/desired/docs_snippets_test.go`) strict-decodes every fenced
`yaml` code block under docs/, specs/, and test/e2e/ that has a TOP-LEVEL
`apiVersion` key through the same loader that reads user input (indented
`apiVersion` keys in nested manifests do not count). Keep snippets loadable
or make them fragments (no top-level apiVersion).

**get_url checksum policy (M9):** every remote `get_url` fetch in the
Ansible roles must either pin a `checksum:` (possibly conditional via
`omit`) or be on the small accepted-unpinnable list in
`internal/repo/checks/audit_regression_test.go` — enforced two-way (stale
entries must be removed). The community cephadm bootstrap binary — fetched
then EXECUTED — must always carry a checksum. Accepted-unpinnable rationales:
the OpenShift CLI `sha256sum.txt` download IS the manifest the sibling
tarball fetches pin against (pinning it would be circular); virtctl comes
from the live host-cluster's ConsoleCLIDownload with no published digest; the
vendor Ceph `.repo` definition is mutable vendor metadata, not executed.
