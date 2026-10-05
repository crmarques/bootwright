# Build toolchain and version metadata

The M1a gates use [scripts/go](../../scripts/go) to select exactly Go 1.26.8
through `GOTOOLCHAIN`. A module's `go` directive sets a minimum and a `toolchain`
directive is a suggestion; neither alone fixes the toolchain used by every
gate. Preserve the explicit selection when changing build tooling. The
[Makefile](../../Makefile) routes builds and Go checks through that wrapper;
[scripts/govulncheck](../../scripts/govulncheck) also passes its selection to the
pinned scanner. Inspect `scripts/go version` when diagnosing toolchain drift.
The language policy remains in [Go engineering](../skills/code-implementation/references/go.md).

[main.go](../../cmd/bootwright/main.go) exposes `main.version`, `main.commit`,
`main.source`, and `main.dependencyBundle` as string variables for linker
`-ldflags -X` injection. Keep these names aligned with release build arguments.
`make build` stamps the first three from Git: `git describe --tags --dirty`, the
`HEAD` revision, and `clean` or `modified` from the tracked-file status. Each is
an overridable variable (`make build VERSION=v0.4.0`), and outside a repository
all three stay empty. Go runtime, OS, and architecture values come from the
linked runtime, and `runtime/debug.ReadBuildInfo` completes an uninjected
version, commit, or source state from the toolchain's own VCS stamp, which is
what a plain `go build` or `go install` of this module records.

The Makefile still passes `-buildvcs=false` and injects the identity itself,
because that toolchain stamp is missing or wrong exactly where much of the work
happens: Go takes only a `.git` directory as a repository root
([vcs.go](https://github.com/golang/go/blob/go1.26.8/src/cmd/go/internal/vcs/vcs.go#L217-L218)),
so a linked Git worktree, whose `.git` is a file, is never one. Outside any
other checkout, Go records no `vcs.*` setting: it stays silent under `auto` and
produces neither a stamp nor an error under `-buildvcs=true`. Nested inside
another checkout, as a worktree created under the primary checkout is, Go walks
up to that checkout and records its revision, its modified state and a module
pseudo-version derived from them instead. The primary checkout stamps normally.
Verified with Go 1.26.8 on 2026-10-03 against a clone of this repository, a
worktree beside it, and a worktree nested in it whose `HEAD` differed from the
clone's. Never treat the toolchain stamp as the release identity.

`TestCompositionSuppliesRuntimeBuildInformation` and
`TestCompositionIdentifiesTheBuild` in
[main_test.go](../../cmd/bootwright/main_test.go) check composition defaults,
runtime values, and the precedence of injected values over that stamp;
[version tests](../../internal/cli/version_test.go) check formatting. These
tests do not qualify a release build pipeline. The
[version output contract](../../specs/cli/commands.md#version-output) owns the
required representation.

Go 1.26.8's RSA/ECDSA key-generation APIs ignore their supplied `io.Reader`
unless the temporary `cryptocustomrand` compatibility setting is enabled; an
erroring Reader therefore does not inject a failure into those operations.
This was verified against the pinned toolchain sources and the
[Go 1.26 release notes](https://go.dev/doc/go1.26#crypto/rsa). Do not infer a
working randomness seam merely from those signatures. The Secrets material
adapter uses a typed crypto-operation seam for failure/cancellation testing and
the standard library's OS-backed implementation in production; see
[generation](../../internal/secrets/material/generation.go), its
[tests](../../internal/secrets/material/generation_test.go), and the owning
[Secrets contract](../../specs/secrets.md#acquisition-and-commands).
