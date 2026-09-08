# Build toolchain and version metadata

The M1a gates use [scripts/go](../../scripts/go) to select exactly Go 1.26.7
through `GOTOOLCHAIN`. A module's `go` directive sets a minimum and a `toolchain`
directive is a suggestion; neither alone fixes the toolchain used by every
gate. Preserve the explicit selection when changing build tooling. The
[Makefile](../../Makefile) routes builds and Go checks through that wrapper;
[scripts/govulncheck](../../scripts/govulncheck) also passes its selection to the
pinned scanner. Inspect `scripts/go version` when diagnosing toolchain drift.
The language policy remains in [Go engineering](../skills/code-implementation/references/go.md).

[main.go](../../cmd/bootwright/main.go) exposes `main.version`, `main.commit`,
and `main.dependencyBundle` as string variables for linker `-ldflags -X`
injection. Keep these names aligned with release build arguments. Go runtime,
OS, and architecture values come from the linked runtime.
`TestCompositionSuppliesRuntimeBuildInformation` in
[main_test.go](../../cmd/bootwright/main_test.go) checks composition defaults and
runtime values; [version tests](../../internal/cli/version_test.go) check
formatting. These tests do not qualify a release build pipeline. The
[version output contract](../../specs/cli/commands.md#version-output) owns the
required representation.
