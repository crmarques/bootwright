.PHONY: build wheels vulngate licenses licenses-check tool-table tool-table-outdated container-build test vet fmt-check modules-check tidy-check completion-test vulncheck ansible-check docs-check focus quick race check-offline check check-go

GO := ./scripts/go
VULNDB ?= https://vuln.go.dev

# A release build overrides these; an ordinary build identifies the working tree
# it came from. Outside a repository they stay empty and `version` reports an
# unidentified build instead of claiming a revision it cannot prove. Every one
# is expanded by the two build recipes alone, so no other target runs Git.
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null)
SOURCE ?= $(if $(COMMIT),$(if $(shell git status --porcelain --untracked-files=no 2>/dev/null),modified,clean))
STAMP = -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.source=$(SOURCE) -X main.vulnerabilityCheck=$(VULNERABILITY_CHECK)

# The vulnerability gate runs before every build, and the build stamps its
# verdict. It scans each default client's release file, which it keeps verified
# by the tool table's SHA-256 in the shared cache directory, so a warm cache
# downloads none. VULNCHECK=skip builds without the databases and stamps the
# executable unchecked.
VULNCHECK ?= on
CLIENTS_CACHE ?= $(shell ./scripts/cache-dir)/client-releases
VULNERABILITY_CHECK = $(shell cat bin/vulnerability-check 2>/dev/null)
vulngate:
	@set -eu; mkdir -p bin; rm -f bin/vulnerability-check; \
	if test '$(VULNCHECK)' = skip; then \
		echo unchecked >bin/vulnerability-check; \
		echo 'vulngate: VULNCHECK=skip, so this build is stamped unchecked'; \
		exit 0; \
	fi; \
	CGO_ENABLED=0 ./scripts/govulncheck -db "$(VULNDB)" -format json ./cmd/bootwright >bin/govulncheck.json; \
	$(GO) -C scripts/tools run ./vulngate -govulncheck "$(CURDIR)/bin/govulncheck.json" \
		-lock "$(CURDIR)/internal/controller/runtimelock/lock.json" -table "$(CURDIR)/internal/controller/tooltable.json" \
		-exceptions "$(CURDIR)/cmd/bootwright/vulnerability-exceptions.json" -verdict "$(CURDIR)/bin/vulnerability-check" \
		-artifacts "$(CLIENTS_CACHE)" -db "$(VULNDB)"

# The container build takes its Go image from the Dockerfile, which pins it by
# tag and digest. Each proxy and module-mirror
# setting of the environment or the command line may carry a credential, and an
# engine records build arguments in image metadata and build history, so the
# recipe hands them over as a build secret in a private file it removes.
CONTAINER_ENGINE ?= $(firstword $(shell command -v podman docker 2>/dev/null))
CONTAINER_OUT ?= bin
PROXY_CA ?=
GO_IMAGE ?=
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

# The controller stage installs each default client by the size and SHA-256
# the tool table pins. These read the publishers, which no build does.
TOOL_TABLE = $(CURDIR)/internal/controller/tooltable.json
tool-table:
	$(GO) -C scripts/tools run ./tooltable -table "$(TOOL_TABLE)" verify

tool-table-outdated:
	$(GO) -C scripts/tools run ./tooltable -table "$(TOOL_TABLE)" outdated

# The Go modules' licenses are read from the module cache here and held to it
# by licenses-check, and the build reads the table alone. The runtime lock's
# licenses beside it are refreshed and checked by scripts/runtime-lock.
GO_LICENSES = $(CURDIR)/cmd/bootwright/go-licenses.json
licenses:
	$(GO) -C scripts/tools run ./licenses refresh -root "$(CURDIR)" -table "$(GO_LICENSES)"

licenses-check:
	$(GO) -C scripts/tools run ./licenses check -root "$(CURDIR)" -table "$(GO_LICENSES)"

# The executable composes its own bill of materials, so the document beside it
# names exactly what that build carries and pins, and the third-party notices
# are composed from that document and the license texts it holds: a build that
# cannot write them fails.
build: wheels vulngate
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags '$(STAMP)' -o bin/bootwright.tmp ./cmd/bootwright
	rm -f bin/bootwright.cdx.json bin/bootwright.notices.txt
	./bin/bootwright.tmp sbom --format cyclonedx-json >bin/bootwright.cdx.json.tmp
	mv -f bin/bootwright.cdx.json.tmp bin/bootwright.cdx.json
	$(GO) -C scripts/tools run ./licenses notices -table "$(GO_LICENSES)" -wheels "$(CURDIR)/internal/controller/runtimelock/wheels" \
		-sbom "$(CURDIR)/bin/bootwright.cdx.json" -out "$(CURDIR)/bin/bootwright.notices.txt.tmp"
	mv -f bin/bootwright.notices.txt.tmp bin/bootwright.notices.txt
	mv -f bin/bootwright.tmp bin/bootwright

container-build:
	@set -eu; \
	engine='$(or $(CONTAINER_ENGINE),$(error container-build needs podman or docker on PATH, or CONTAINER_ENGINE))'; \
	out='$(CONTAINER_OUT)'; \
	mkdir -p -- "$$out"; \
	for name in bootwright bootwright.cdx.json bootwright.notices.txt; do \
		if test -d "$$out/$$name" || { test -e "$$out/$$name" && ! test -O "$$out/$$name"; }; then \
			echo "container-build: $$out/$$name is a directory or belongs to another user; remove it or choose another CONTAINER_OUT" >&2; exit 1; \
		fi; \
	done; \
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
	echo "$$engine build into $(CONTAINER_OUT) in the Go image $(or $(GO_IMAGE),the Dockerfile pins); settings passed as a secret:$${passed:- none}"; \
	"$$engine" build --output type=local,dest="$$stage" \
		--build-arg VERSION='$(VERSION)' --build-arg COMMIT='$(COMMIT)' --build-arg SOURCE='$(SOURCE)' --build-arg VULNCHECK='$(VULNCHECK)' \
		$(if $(GO_IMAGE),--build-arg GO_IMAGE='$(GO_IMAGE)') \
		--secret id=build-env,src="$$settings" \
		$(if $(PROXY_CA),--secret id=proxy-ca$(comma)src='$(PROXY_CA)') \
		.; \
	for name in bootwright bootwright.cdx.json bootwright.notices.txt; do \
		test -f "$$stage/$$name" || { echo "container-build: the engine exported no $$name, so $$out keeps what it held" >&2; exit 1; }; \
	done; \
	mv -f -- "$$stage/bootwright.cdx.json" "$$out/bootwright.cdx.json"; \
	mv -f -- "$$stage/bootwright.notices.txt" "$$out/bootwright.notices.txt"; \
	mv -f -- "$$stage/bootwright" "$$out/bootwright"

test:
	$(GO) test ./...
	$(GO) -C scripts/tools test ./clientassets/... ./licenses/... ./runtimewheels/... ./tooltable/... ./vulngate/...

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
check-offline: fmt-check licenses-check test vet completion-test
	@echo 'check-offline: vulncheck, ansible-check and modules-check were not run; they are unrun, not passed.'

# The gate. The Ansible check takes most of its time, so it starts first and
# holds one of two job slots while the Go targets take the other, and the Go
# targets use both once it ends. -k runs every target after a failure, so one
# run reports every failing check; any failure still fails the gate. Each
# target's output prints whole when it ends.
check:
	$(MAKE) -k --no-print-directory --output-sync=target -j2 ansible-check check-go
	git diff --check

check-go: wheels fmt-check modules-check tidy-check licenses-check test vet completion-test vulncheck vulngate
