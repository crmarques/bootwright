# Containerfile layer caching and binary version stamping

**Go toolchain via COPY, not download:** the build pins the toolchain by
`COPY --from=gotoolchain /usr/local/go /usr/local/go` from the official
`golang` image and sets `GOTOOLCHAIN=local`, instead of letting `go` download
the go.mod-required version at build time. The runtime toolchain download
targets `storage.googleapis.com`, which fails behind a TLS-intercepting
corporate proxy, while the registry pull goes through the same (working) path
as the base image. Keep `GOTOOLCHAIN=local`.

**Layer strategy (ordered to maximize cache hits):**

1. The Galaxy collection-download layer (first `make sync-bundle`) is keyed
   only on `Makefile`, `ansible/collections/requirements.yml`,
   `requirements.lock.yml`, `scripts/sync-ansible-bundle.py`,
   `scripts/verify-ansible-collections.py`, and `internal/repo/bundlecheck` —
   role/playbook edits stay cached.
2. Only the inputs `make build` consumes are COPYed (`api`, `cmd`,
   `internal`, `add-ons`, `ansible`, `scripts`); docs, examples, specs,
   images, and tests are excluded via `.dockerignore` so edits to them never
   invalidate the bundle/compile layers.
3. `make sync-bundle` runs a SECOND time after the full source COPY: the
   bundle re-pack is keyed on the source COPYs (not VERSION), so version
   churn does not re-run it and the packed bundle matches the real
   `ansible/` tree.
4. The compile is its own layer via `make go-build`, so per-commit
   VERSION/GIT_COMMIT changes only re-link.
5. `.git` is COPYed LAST (it changes every commit and that layer re-runs
   anyway) so a raw `docker build` without build args can still self-stamp
   via `git describe`. `.dockerignore` must NOT exclude `.git`;
   `TestContainerfileStampsBuildMetadata` enforces that and forbids silent
   `ARG VERSION=dev` / `ARG GIT_COMMIT=unknown` defaults.

**Version stamping:** `VERSION` may be an explicit tag (set by CI/releases),
falling back to `git describe --tags --always --dirty`, then the short SHA,
then `dev`; LDFLAGS inject `internal/cli.versionString` and `.gitCommit`. The
in-source defaults (`dev`/`unknown`) exist so plain `go build ./...` and
`go test ./...` keep working without the Makefile.

**`make go-build` vs `make build`:** `go-build` compiles WITHOUT re-syncing
the embedded ansible bundle — it exists so the container build can run
sync-bundle and the compile in separate layers. Local developers must keep
using `make build`, which syncs first; a `go-build` against a stale bundle
ships outdated playbooks.

**CGO stays off:** `GO_BUILD_CMD` forces `CGO_ENABLED=0` because the CLI is
copied out of the container image and installed onto operator/bastion hosts
whose glibc may be older than the build host's: a dynamically linked cgo
build (pulled in by `os/user` and the net resolver) segfaults at load time on
those hosts. Pure-Go `os/user`/net need no shared libraries at runtime. Do
not re-enable cgo for the shipped binary.
