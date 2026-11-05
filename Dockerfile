# Builds bin/bootwright on a host that has only Docker or Podman; make
# container-build fills GO_VERSION from go.mod. docs/development.md#building-in-a-container
# owns the options, proxies and caches included.

ARG GO_VERSION
ARG GO_IMAGE=docker.io/library/golang:${GO_VERSION}-trixie

FROM ${GO_IMAGE} AS build

ARG VERSION=
ARG COMMIT=
ARG SOURCE=

WORKDIR /src
COPY . .

RUN --mount=type=secret,id=build-env \
    --mount=type=secret,id=proxy-ca \
    --mount=type=cache,id=bootwright-go-mod,target=/go/pkg/mod \
    --mount=type=cache,id=bootwright-go-build,target=/root/.cache/go-build \
    set -eu; \
    if test -s /run/secrets/build-env; then \
        while IFS= read -r setting; do export "$setting"; done </run/secrets/build-env; \
    fi; \
    if test -s /run/secrets/proxy-ca; then \
        cat /etc/ssl/certs/ca-certificates.crt /run/secrets/proxy-ca >/tmp/ca-bundle.pem; \
        export SSL_CERT_FILE=/tmp/ca-bundle.pem GIT_SSL_CAINFO=/tmp/ca-bundle.pem; \
    fi; \
    ./scripts/go mod download; \
    rm -f /tmp/ca-bundle.pem; \
    test -n "$VERSION" || unset VERSION; \
    test -n "$COMMIT" || unset COMMIT; \
    test -n "$SOURCE" || unset SOURCE; \
    GOPROXY=off GOFLAGS=-mod=readonly make build; \
    ./bin/bootwright version

FROM scratch AS binary
COPY --from=build /src/bin/bootwright /bootwright
