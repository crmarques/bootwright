# Ownership Record Contract

Durable ownership evidence is written at mutation time by the executing
collection roles through `bootwright.core.ownership_record` and read by Go
(`internal/ownership`) for destroy scoping, host package removal
gating, orphan reporting, and `diff --recorded`. Run, install, and convergence-safety
ledgers remain Go-written; this contract covers only ownership evidence.

Records live under the rendered `bootwright_ownership_dir` (the context
ownership directory under `/var/lib/bootwright`). Both record kinds use
`apiVersion: bootwright.io/ownership/v1alpha1`, `owner: bootwright`, mode
`0600` inside `0700` directories, and `kind`/`name` path segments restricted to
`[A-Za-z0-9_.-]`.

## Resource Records

Path: `<ownership-dir>/resources/<kind>/<name>.json` (written by
`ownership_record/tasks/resource.yml`).

| Field | Meaning |
| --- | --- |
| `apiVersion`, `kind`, `name`, `owner` | Record identity; `owner` is always `bootwright`. |
| `context` | Owning Bootwright context name. |
| `host` | Machine the resource lives on (`bootwright_host_name`). |
| `provider`, `cluster`, `machine` | Optional scoping facts supplied via `bootwright_ownership_fields`. |
| `paths` | Filesystem paths the record authorizes destroy to remove. |
| `hostFacts` | Connection facts captured at write time for later destroy runs. |
| `labels`, `attributes` | Free-form string maps for role-specific evidence. |
| `updatedAt` | UTC RFC3339 write timestamp. |

The canonical schema is `ResourceRecord` in
`internal/ownership/ownership.go`; Go validates every record it loads,
so a role writing a new field must keep the record decodable there.

## Package Records

Path: `<ownership-dir>/packages/<host>/<package>.json` (written by
`ownership_record/tasks/package_apply_one.yml`).

| Field | Meaning |
| --- | --- |
| `kind` | Always `package`. |
| `name` | `<host>-<package>` with unsafe characters replaced by `_`. |
| `package` | The host package name. |
| `requiredBy` | Reference-counted list of components that need the package. |
| `preexisting` | True when the package was already installed before Bootwright; such packages are never removed. |
| `updatedAt` | UTC RFC3339 write timestamp. |

`package_remove_one.yml` removes a `requiredBy` entry and uninstalls the
package only when the list becomes empty and `preexisting` is false. Destroy
flows must consult these records instead of package presence; see the CLI
destroy contract in `specs/state-model.md`.
