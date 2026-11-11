# Builds bin/bootwright on a host that has only Docker or Podman. The Go image
# is pinned by tag and digest, and TestTheContainerBuildPinsTheGoImageOfGoMod
# holds its tag to the go directive of go.mod. docs/development.md#building-in-a-container
# owns the options, proxies and caches included.

ARG GO_IMAGE=docker.io/library/golang:1.26.11-trixie@sha256:fa340cb59ebeb1d53ac1cccfc334201d938fe5f3458003cccb767096d8e032b8

FROM ${GO_IMAGE} AS build

ARG VERSION=
ARG COMMIT=
ARG SOURCE=
ARG VULNCHECK=on

WORKDIR /src
COPY . .

RUN --mount=type=secret,id=build-env \
    --mount=type=secret,id=proxy-ca \
    --mount=type=cache,id=bootwright-go-mod,target=/go/pkg/mod \
    --mount=type=cache,id=bootwright-go-build,target=/root/.cache/go-build \
    --mount=type=cache,id=bootwright-runtime-wheels,target=/root/.cache/bootwright-runtime-wheels \
    --mount=type=cache,id=bootwright-client-releases,target=/root/.cache/bootwright-client-releases \
    set -eu; \
    if test -s /run/secrets/build-env; then \
        while IFS= read -r setting; do export "$setting"; done </run/secrets/build-env; \
    fi; \
    if test -s /run/secrets/proxy-ca; then \
        cat /etc/ssl/certs/ca-certificates.crt /run/secrets/proxy-ca >/tmp/ca-bundle.pem; \
        export SSL_CERT_FILE=/tmp/ca-bundle.pem GIT_SSL_CAINFO=/tmp/ca-bundle.pem; \
    fi; \
    ./scripts/go mod download; \
    ./scripts/go -C scripts/tools mod download; \
    export WHEELS_CACHE=/root/.cache/bootwright-runtime-wheels; \
    make wheels; \
    export CLIENTS_CACHE=/root/.cache/bootwright-client-releases; \
    test -n "$VERSION" || unset VERSION; \
    test -n "$COMMIT" || unset COMMIT; \
    test -n "$SOURCE" || unset SOURCE; \
    GOPROXY=off GOFLAGS=-mod=readonly make build VULNCHECK="$VULNCHECK"; \
    rm -f /tmp/ca-bundle.pem; \
    ./bin/bootwright version

FROM scratch AS binary
COPY --from=build /src/bin/bootwright /bootwright
COPY --from=build /src/bin/bootwright.cdx.json /bootwright.cdx.json
COPY --from=build /src/bin/bootwright.notices.txt /bootwright.notices.txt
