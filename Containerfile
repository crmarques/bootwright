# syntax=docker/dockerfile:1.25@sha256:0adf442eae370b6087e08edc7c50b552d80ddf261576f4ebd6421006b2461f12
FROM docker.io/library/golang:1.25.12@sha256:d2e20dc1b35aefd666909163e4ace41efb521359aa2ce31fff59d86837050f6f AS gotoolchain

FROM docker.io/redhat/ubi9@sha256:50701171b9917ed51048b614924598d45b00bce9a64b73860c057922fc13bec2 AS builder

ARG NO_PROXY
ARG no_proxy
ARG PIP_VERSION=26.1.2
ARG ANSIBLE_CORE_VERSION=2.20.7

ENV NO_PROXY=${NO_PROXY} \
    no_proxy=${no_proxy} \
    PATH=/opt/bootwright-ansible/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    GOMODCACHE=/go/pkg/mod \
    GOTOOLCHAIN=local

WORKDIR /src

RUN --mount=type=secret,id=proxy \
    --mount=type=cache,id=bootwright-dnf-cache,target=/var/cache/dnf,sharing=locked \
    --mount=type=cache,id=bootwright-dnf-lib,target=/var/lib/dnf,sharing=locked \
    if [ -f /run/secrets/proxy ]; then . /run/secrets/proxy; fi; \
    dnf install -y --setopt=keepcache=1 \
        python3.12 \
        make \
        git

COPY --from=gotoolchain /usr/local/go /usr/local/go

RUN go version

RUN --mount=type=secret,id=proxy \
    --mount=type=cache,id=bootwright-pip,target=/root/.cache/pip,sharing=locked \
    if [ -f /run/secrets/proxy ]; then . /run/secrets/proxy; fi; \
    python3.12 -m venv /opt/bootwright-ansible \
    && /opt/bootwright-ansible/bin/pip install "pip==${PIP_VERSION}" \
    && /opt/bootwright-ansible/bin/pip install "ansible-core==${ANSIBLE_CORE_VERSION}"

COPY go.mod go.sum ./
RUN --mount=type=secret,id=proxy \
    --mount=type=cache,id=bootwright-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=bootwright-go-build,target=/root/.cache/go-build,sharing=locked \
    if [ -f /run/secrets/proxy ]; then . /run/secrets/proxy; fi; \
    go mod download

COPY Makefile Makefile
COPY ansible/collections/requirements.yml ansible/collections/requirements.yml
COPY ansible/collections/requirements.lock.yml ansible/collections/requirements.lock.yml
COPY scripts/sync-ansible-bundle.py scripts/sync-ansible-bundle.py
COPY scripts/verify-ansible-collections.py scripts/verify-ansible-collections.py
COPY internal/repo/bundlecheck internal/repo/bundlecheck
RUN --mount=type=secret,id=proxy \
    --mount=type=cache,id=bootwright-ansible-galaxy,target=/root/.ansible,sharing=locked \
    --mount=type=cache,id=bootwright-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=bootwright-go-build,target=/root/.cache/go-build,sharing=locked \
    if [ -f /run/secrets/proxy ]; then . /run/secrets/proxy; fi; \
    make sync-bundle

COPY api api
COPY cmd cmd
COPY internal internal
COPY add-ons add-ons
COPY ansible ansible
COPY scripts scripts

RUN --mount=type=secret,id=proxy \
    --mount=type=cache,id=bootwright-ansible-galaxy,target=/root/.ansible,sharing=locked \
    --mount=type=cache,id=bootwright-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=bootwright-go-build,target=/root/.cache/go-build,sharing=locked \
    if [ -f /run/secrets/proxy ]; then . /run/secrets/proxy; fi; \
    make sync-bundle

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

FROM docker.io/redhat/ubi9@sha256:50701171b9917ed51048b614924598d45b00bce9a64b73860c057922fc13bec2

COPY --from=builder /src/bin/bootwright /usr/local/bin/bootwright

ENTRYPOINT ["/usr/local/bin/bootwright"]
