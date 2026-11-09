.PHONY: build wheels container-build test vet fmt-check modules-check tidy-check completion-test vulncheck ansible-check docs-check focus quick race check-offline check check-go

GO := ./scripts/go
VULNDB ?= https://vuln.go.dev

# A release build overrides these; an ordinary build identifies the working tree
# it came from. Outside a repository they stay empty and `version` reports an
# unidentified build instead of claiming a revision it cannot prove. Every one
# is expanded by the two build recipes alone, so no other target runs Git.
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null)
SOURCE ?= $(if $(COMMIT),$(if $(shell git status --porcelain --untracked-files=no 2>/dev/null),modified,clean))
STAMP = -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.source=$(SOURCE)

# The container build takes its Go from go.mod. Each proxy and module-mirror
# setting of the environment or the command line may carry a credential, and an
# engine records build arguments in image metadata and build history, so the
# recipe hands them over as a build secret in a private file it removes.
CONTAINER_ENGINE ?= $(firstword $(shell command -v podman docker 2>/dev/null))
CONTAINER_OUT ?= bin
PROXY_CA ?=
GO_IMAGE ?=
GO_VERSION = $(shell awk '$$1 == "go" { print $$2; exit }' go.mod)
CONTAINER_SETTINGS = HTTP_PROXY HTTPS_PROXY NO_PROXY http_proxy https_proxy no_proxy GOPROXY GOSUMDB GOPRIVATE GONOSUMDB GONOPROXY
comma := ,

# The executable carries the runtime lock's wheels, so controller install
# downloads none. The tool places them where the build embeds them, checking
# each against the lock, and keeps them in the shared cache directory, so a
# warm cache builds offline.
WHEELS_CACHE ?= $(shell ./scripts/cache-dir)/runtime-wheels
wheels:
	$(GO) -C scripts/tools run ./runtimewheels -lock "$(CURDIR)/internal/controller/runtimelock/lock.json" \
		-dest "$(CURDIR)/internal/controller/runtimelock/wheels" -cache "$(WHEELS_CACHE)"

build: wheels
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags '$(STAMP)' -o bin/bootwright ./cmd/bootwright

container-build:
	@set -eu; \
	engine='$(or $(CONTAINER_ENGINE),$(error container-build needs podman or docker on PATH, or CONTAINER_ENGINE))'; \
	out='$(CONTAINER_OUT)'; \
	mkdir -p -- "$$out"; \
	if test -d "$$out/bootwright" || { test -e "$$out/bootwright" && ! test -O "$$out/bootwright"; }; then \
		echo "container-build: $$out/bootwright is a directory or belongs to another user; remove it or choose another CONTAINER_OUT" >&2; exit 1; \
	fi; \
	settings="$$(mktemp)"; \
	stage=; \
	trap 'rm -f "$$settings"; test -z "$$stage" || rm -rf -- "$$stage"' EXIT; \
	trap 'exit 130' INT TERM; \
	stage="$$(mktemp -d "$$out/.container-build.XXXXXX")"; \
	passed=; \
	for name in $(CONTAINER_SETTINGS); do \
		eval "value=\$${$$name-}"; \
		if test -n "$$value"; then printf '%s=%s\n' "$$name" "$$value" >>"$$settings"; passed="$$passed $$name"; fi; \
	done; \
	echo "$$engine build into $(CONTAINER_OUT) with Go $(GO_VERSION); settings passed as a secret:$${passed:- none}"; \
	"$$engine" build --output type=local,dest="$$stage" \
		--build-arg GO_VERSION='$(or $(GO_VERSION),$(error go.mod names no go version))' \
		--build-arg VERSION='$(VERSION)' --build-arg COMMIT='$(COMMIT)' --build-arg SOURCE='$(SOURCE)' \
		$(if $(GO_IMAGE),--build-arg GO_IMAGE='$(GO_IMAGE)') \
		--secret id=build-env,src="$$settings" \
		$(if $(PROXY_CA),--secret id=proxy-ca$(comma)src='$(PROXY_CA)') \
		.; \
	mv -f -- "$$stage/bootwright" "$$out/bootwright"

test:
	$(GO) test ./...
	$(GO) -C scripts/tools test ./runtimewheels/...

vet:
	$(GO) vet ./...
	GOOS=darwin GOARCH=arm64 $(GO) vet ./...

fmt-check:
	@set -eu; \
	formatter="$$($(GO) env GOROOT)/bin/gofmt"; \
	directories="$$($(GO) list -buildvcs=false -tags=completion -f '{{.Dir}}' ./...)"; \
	unformatted="$$(printf '%s\n' "$$directories" | while IFS= read -r directory; do "$$formatter" -l "$$directory" || exit $$?; done)"; \
	if test -n "$$unformatted"; then printf 'Go files require formatting:\n%s\n' "$$unformatted" >&2; exit 1; fi

tidy-check:
	$(GO) mod tidy -diff
	$(GO) -C scripts/tools mod tidy -diff

modules-check:
	$(GO) mod verify
	$(GO) -C scripts/tools mod verify

completion-test:
	$(GO) test -count=1 -tags=completion ./test/completion

vulncheck:
	./scripts/govulncheck -db "$(VULNDB)" ./...

ansible-check:
	./scripts/ansible-check

# Guidance links, cited paths and tests, command lines, and the tables specs
# share with code: diagnostics, bounds, the command catalog and transitions, the
# capabilities' and the engine's refusal tables, the Ceph matrix catalog and
# combination table, and the destinations the operator guide lists for a
# proxied network. The tests it runs are the list in $(DOCS_CHECKS), which
# TestDocsChecksListIsComplete holds complete: every test named TestDocs*, the
# *RefusalTableMatchesUnsupported and *ObservableFieldTableMatchesProjector
# tests and the other names that test lists is listed, and every listed test
# exists. TestDocsCheckRecipeReadsTheList holds this recipe to the list.
DOCS_CHECKS := test/architecture/testdata/docs-checks.txt
docs-check:
	$(GO) test -count=1 -run '^($(shell awk '{print $$2}' $(DOCS_CHECKS) | paste -sd'|' -))$$' $(shell awk '{print "./" $$1}' $(DOCS_CHECKS) | sort -u)

# The probe tier: the narrowest checks of each file this branch changed, which
# stop at the first failure. It is no gate; make check is.
focus:
	python3 -I scripts/tools/focus.py

# The inner-loop tier: formatting, vet, the architecture suite and the packages
# this branch changed together with their dependents.
quick: fmt-check vet
	./scripts/quick-test

race:
	$(GO) test -race ./internal/reconciliation/... ./internal/controller/privilege/... ./internal/trust/... ./cmd/bootwright/...

# Every gate that needs no network once module caches are warm. Vulnerability
# data and the Ansible tool bootstrap need network, so they are reported unrun.
check-offline: fmt-check test vet completion-test
	@echo 'check-offline: vulncheck, ansible-check and modules-check were not run; they are unrun, not passed.'

# The gate. The Ansible check takes most of its time, so it starts first and
# holds one of two job slots while the Go targets take the other, and the Go
# targets use both once it ends. -k runs every target after a failure, so one
# run reports every failing check; any failure still fails the gate. Each
# target's output prints whole when it ends.
check:
	$(MAKE) -k --no-print-directory --output-sync=target -j2 ansible-check check-go
	git diff --check

check-go: wheels fmt-check modules-check tidy-check test vet completion-test vulncheck
