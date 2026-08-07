# Containerfile layer caching and binary version stamping

**Go toolchain via COPY, not download:** the build pins the toolchain by
`COPY --from=gotoolchain /usr/local/go /usr/local/go` from the official
`golang` image and sets `GOTOOLCHAIN=local`, instead of letting `go` download
the go.mod-required version at build time. The runtime toolchain download
targets `storage.googleapis.com`, which fails behind a TLS-intercepting
corporate proxy, while the registry pull goes through the same (working) path
as the base image. Keep `GOTOOLCHAIN=local`.

**Layer strategy (ordered to maximize cache hits):**

1. The Galaxy collection-download layer runs `make sync-collections` and is
   keyed only on `Makefile`, `ansible/collections/requirements.yml`,
   `requirements.lock.yml`, and `scripts/verify-ansible-collections.py` —
   role/playbook edits stay cached. It resolves collections ONLY; it must never
   pack the archive (see the cold-build constraint below).
2. Only the inputs `make build` consumes are COPYed (`api`, `cmd`,
   `internal`, `add-ons`, `ansible`, `scripts`); docs, examples, specs,
   images, and tests are excluded via `.dockerignore` so edits to them never
   invalidate the bundle/compile layers.
3. `make sync-bundle` runs AFTER the full source COPY, and only there: the
   bundle pack is keyed on the source COPYs (not VERSION), so version
   churn does not re-run it and the packed bundle matches the real
   `ansible/` tree.
4. The compile is its own layer via `make go-build`, so per-commit
   VERSION/GIT_COMMIT changes only re-link.
5. `.git` is COPYed LAST (it changes every commit and that layer re-runs
   anyway) so a raw `docker build` without build args can still self-stamp
   via `git describe`. `.dockerignore` must NOT exclude `.git`;
   `TestContainerfileStampsBuildMetadata` enforces that and forbids silent
   `ARG VERSION=dev` / `ARG GIT_COMMIT=unknown` defaults.

**Constraint (the pre-ansible layer must not pack the archive — cold builds
otherwise ship an empty bundle):** the collection layer originally ran
`make sync-bundle`, which builds the whole chain and therefore WROTE
`internal/converge/bundle/ansible_bundle.zip` from a tree holding only
`ansible/collections/requirements*.yml`. `COPY` preserves the build context's
mtimes, so the ansible sources arriving in the next layer are almost always
OLDER than that placeholder, and make then declares the archive up to date: the
second `make sync-bundle` is a silent no-op and `make go-build` embeds an
archive with no `ansible.cfg` and no `bootwright/core`. Every command then dies
with `embedded ansible bundle is empty (rebuild bootwright via 'make build'):
missing ansible.cfg`.
Whether it bites depends on the BuildKit layer cache, which is why it looks
intermittent: when the collection layer is a CACHE HIT its placeholder carries
that layer's original (old) mtime, the COPYed sources are newer, and the re-pack
runs correctly. On a COLD build — no layer cache, or the requirements changed —
the placeholder is seconds old and always wins. Observed 2026-08-07 after a
`docker build` following `docker image rm`.
The layer now runs `make sync-collections` (`$(COLLECTIONS_STAMP)` only), and
`scripts/sync-ansible-bundle.py` refuses via `require_runnable_bundle()` to
write an archive missing `ansible.cfg` or
`collections/ansible_collections/bootwright/core/galaxy.yml` — the exact two
entries `openAnsibleBundleArchive` rejects at startup, so a build can no longer
produce a binary that fails on its first command.
`TestContainerfilePacksTheBundleOnlyWithTheFullAnsibleTree` pins the ordering.

**Constraint (proxy credentials reach the build as a BuildKit secret, never as a
build-arg):** every network-bound RUN sources `/run/secrets/proxy`
(`--mount=type=secret,id=proxy`), so the credential lives in a tmpfs for the
duration of that step only. `proxy.env.example` and the README both promise it
never becomes a build-arg — but `make container-build` passed
`HTTP_PROXY`/`HTTPS_PROXY`/`http_proxy`/`https_proxy` as `--build-arg` anyway,
contradicting its own documentation. It now hands BuildKit
`--secret id=proxy,src=$(CONTAINER_PROXY_ENV)` (default `proxy.env`) when that
file exists, and otherwise synthesizes a mode-`600` temporary file from the
environment — `shlex.quote`d so a password containing quotes or `$` still sources
correctly — removed by a `trap` on `EXIT INT TERM`. `NO_PROXY`/`no_proxy` carry
no credentials and stay ordinary build-args, which is also why they are the only
proxy values the Containerfile promotes to `ENV`. The shipped image is unaffected
either way: the final stage is a fresh `FROM ubi9` that copies only the binary,
so no builder layer, `ENV`, or secret reaches it.
`TestContainerBuildPassesProxyCredentialsAsASecret` pins this.

**Constraint (the in-container self-stamp must not ask git for `--dirty`):**
`.dockerignore` deliberately keeps `docs/`, `examples/`, `specs/`, `test/`,
`.github/`, `.agents/`, `README.md`, `AGENTS.md`, `mkdocs.yml` and the repo's
other root files out of the build context. They are all TRACKED, so inside the
builder's worktree git sees every one of them as deleted and
`git describe --tags --always --dirty` appends `-dirty` to EVERY container
build, from a pristine checkout at a tagged commit. Observed 2026-08-07: a
`docker build` off a clean tree still stamped `…-dirty`.
The fallback now takes `git describe --tags --always` and appends `-dirty` only
when `git status --porcelain --untracked-files=no --` over the copied build
inputs (`Makefile go.mod go.sum api cmd internal add-ons ansible scripts`)
reports something. `--untracked-files=no` matters: `.gitignore` is NOT copied
into the builder, so `bin/`, `.state/` and the generated
`internal/converge/bundle/ansible_bundle.zip` would otherwise register as
untracked drift. Root-level `.dockerignore` patterns do not match nested paths —
`internal/clusteraccess/kubeconfig.go` survives `*kubeconfig*`, which is why the
copied trees are complete and this check is sound.
The in-container stamp therefore describes the build's INPUTS. `make
container-build` passes `VERSION`/`GIT_COMMIT` from the host, where the whole
repository is visible, and that remains the strict answer.

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
