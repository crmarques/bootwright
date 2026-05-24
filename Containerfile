# syntax=docker/dockerfile:1.7
FROM docker.io/redhat/ubi9:9.7 AS builder

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG no_proxy
ARG PIP_VERSION=26.1.1
ARG ANSIBLE_CORE_VERSION=2.21.0

ENV HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    NO_PROXY=${NO_PROXY} \
    http_proxy=${http_proxy} \
    https_proxy=${https_proxy} \
    no_proxy=${no_proxy} \
    PATH=/opt/bootwright-ansible/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    GOMODCACHE=/go/pkg/mod

WORKDIR /src

RUN --mount=type=cache,id=bootwright-dnf-cache,target=/var/cache/dnf,sharing=locked \
    --mount=type=cache,id=bootwright-dnf-lib,target=/var/lib/dnf,sharing=locked \
    dnf install -y --setopt=keepcache=1 \
        golang \
        python3.12 \
        make \
        git

RUN --mount=type=cache,id=bootwright-pip,target=/root/.cache/pip,sharing=locked \
    python3.12 -m venv /opt/bootwright-ansible \
    && /opt/bootwright-ansible/bin/pip install "pip==${PIP_VERSION}" \
    && /opt/bootwright-ansible/bin/pip install "ansible-core==${ANSIBLE_CORE_VERSION}"

COPY go.mod go.sum ./
RUN --mount=type=cache,id=bootwright-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=bootwright-go-build,target=/root/.cache/go-build,sharing=locked \
    go mod download

COPY Makefile Makefile
COPY ansible/collections/requirements.yml ansible/collections/requirements.yml
COPY ansible/collections/requirements.lock.yml ansible/collections/requirements.lock.yml
COPY internal/embedded/bundle/.gitignore internal/embedded/bundle/.gitignore
COPY internal/embedded/bundle/PLACEHOLDER internal/embedded/bundle/PLACEHOLDER
COPY internal/bundlecheck internal/bundlecheck
RUN --mount=type=cache,id=bootwright-ansible-galaxy,target=/root/.ansible,sharing=locked \
    make sync-bundle

COPY . .
ARG VERSION=dev
ARG GIT_COMMIT=unknown
RUN --mount=type=cache,id=bootwright-go-mod,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,id=bootwright-go-build,target=/root/.cache/go-build,sharing=locked \
    make build VERSION="${VERSION}" GIT_COMMIT="${GIT_COMMIT}"

FROM docker.io/redhat/ubi9:9.7

COPY --from=builder /src/bin/bootwright /usr/local/bin/bootwright

ENTRYPOINT ["/usr/local/bin/bootwright"]
