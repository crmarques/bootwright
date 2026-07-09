# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e
FROM docker.io/library/golang:1.25.11@sha256:995e25c0e1868fa30a57236d5d8c2252b94b8716e53eae5895cd70dcce532cf0 AS gotoolchain

FROM docker.io/redhat/ubi9@sha256:e9a31af6530caffa3551f266c51a0d43b602e8f76a0dc12826dbeebceb487c92 AS builder

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG no_proxy
ARG PIP_VERSION=26.1.2
ARG ANSIBLE_CORE_VERSION=2.21.0

ENV HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    NO_PROXY=${NO_PROXY} \
    http_proxy=${http_proxy} \
    https_proxy=${https_proxy} \
    no_proxy=${no_proxy} \
    PATH=/opt/bootwright-ansible/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    GOMODCACHE=/go/pkg/mod \
    GOTOOLCHAIN=local

WORKDIR /src

RUN --mount=type=cache,id=bootwright-dnf-cache,target=/var/cache/dnf,sharing=locked \
    --mount=type=cache,id=bootwright-dnf-lib,target=/var/lib/dnf,sharing=locked \
    dnf install -y --setopt=keepcache=1 \
        python3.12 \
        make \
        git

# Pin the Go toolchain to the version go.mod requires by copying it from the
# official image instead of letting `go` download it at build time. The runtime
# download targets storage.googleapis.com, which fails behind a TLS-intercepting
# proxy; the registry pull used here goes through the same path as the base image.
COPY --from=gotoolchain /usr/local/go /usr/local/go

RUN go version

RUN --mount=type=cache,id=bootwright-pip,target=/root/.cache/pip,sharing=locked \
    python3.12 -m venv /opt/bootwright-ansible \
    && /opt/bootwright-ansible/bin/pip install "pip==${PIP_VERSION}" \
    && /opt/bootwright-ansible/bin/pip install "ansible-core==${ANSIBLE_CORE_VERSION}"

COPY go.mod go.sum ./
RUN --mount=type=cache,id=bootwright-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=bootwright-go-build,target=/root/.cache/go-build,sharing=locked \
    go mod download

# Resolve the Galaxy collections in a layer keyed only on the collection
# requirements. Ansible role/playbook edits change the bundle's contents but not
# its dependency set, so isolating the network download here keeps it cached
# across ordinary source changes.
COPY Makefile Makefile
COPY ansible/collections/requirements.yml ansible/collections/requirements.yml
COPY ansible/collections/requirements.lock.yml ansible/collections/requirements.lock.yml
COPY scripts/sync-ansible-bundle.py scripts/sync-ansible-bundle.py
COPY scripts/verify-ansible-collections.py scripts/verify-ansible-collections.py
COPY internal/repo/bundlecheck internal/repo/bundlecheck
RUN --mount=type=cache,id=bootwright-ansible-galaxy,target=/root/.ansible,sharing=locked \
    --mount=type=cache,id=bootwright-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=bootwright-go-build,target=/root/.cache/go-build,sharing=locked \
    make sync-bundle

# Copy only the inputs `make build` consumes. Docs, examples, specs, images,
# tests and .git are deliberately excluded (see .dockerignore) so edits to them
# never invalidate the bundle and compile layers below.
COPY api api
COPY cmd cmd
COPY internal internal
COPY add-ons add-ons
COPY ansible ansible
COPY scripts scripts

# Re-pack the bundle with the full ansible tree. The collection download above
# is already cached, so this only rezips local sources; keying it on the source
# COPYs (not on VERSION) keeps version churn from re-running it.
RUN --mount=type=cache,id=bootwright-ansible-galaxy,target=/root/.ansible,sharing=locked \
    --mount=type=cache,id=bootwright-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=bootwright-go-build,target=/root/.cache/go-build,sharing=locked \
    make sync-bundle

# Compile in a layer of its own so the per-commit VERSION/GIT_COMMIT change only
# re-links the binary (dependency objects stay in the go-build cache) instead of
# re-running the bundle sync. VERSION/GIT_COMMIT come from the build args the
# Makefile passes; .git is copied here (last, since it changes every commit and
# this layer already re-runs each commit) so a raw `docker build` without those
# args can still self-stamp the version via git describe.
COPY .git .git
ARG VERSION
ARG GIT_COMMIT
RUN --mount=type=cache,id=bootwright-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=bootwright-go-build,target=/root/.cache/go-build,sharing=locked \
    version="${VERSION}"; \
    git_commit="${GIT_COMMIT}"; \
    if [ -z "${version}" ]; then version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"; fi; \
    if [ -z "${git_commit}" ]; then git_commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"; fi; \
    make go-build VERSION="${version}" GIT_COMMIT="${git_commit}"

FROM docker.io/redhat/ubi9@sha256:e9a31af6530caffa3551f266c51a0d43b602e8f76a0dc12826dbeebceb487c92

COPY --from=builder /src/bin/bootwright /usr/local/bin/bootwright

ENTRYPOINT ["/usr/local/bin/bootwright"]
