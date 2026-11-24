.PHONY: build release wheels vulngate licenses licenses-check tool-table tool-table-outdated tool-records-refresh tool-records-verify container-build test vet fmt-check modules-check tidy-check completion-test vulncheck ansible-check docs-check execution-contracts-check focus quick race check-offline check check-go staticcheck cover cover-floors

GO := ./scripts/go
VULNDB ?= https://vuln.go.dev

# A release build overrides these; an ordinary build identifies the working tree
# it came from. Outside a repository they stay empty and `version` reports an
# unidentified build instead of claiming a revision it cannot prove. Every one
# is expanded by the two build recipes and the release recipes alone, so no
# other target runs Git.
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null)
SOURCE ?= $(if $(COMMIT),$(if $(shell git status --porcelain --untracked-files=no 2>/dev/null),modified,clean))
STAMP = -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.source=$(SOURCE) -X main.vulnerabilityCheck=$(VULNERABILITY_CHECK)

# The vulnerability gate runs before every build, and the build stamps its
# verdict. It scans reviewed compact records extracted from the pinned client
# release files. Only the explicit record refresh and verification use the
# release-file cache. VULNCHECK=skip stamps the executable unchecked.
VULNCHECK ?= on
CLIENTS_CACHE ?= $(shell ./scripts/cache-dir)/client-releases
CLIENT_RECORDS = $(CURDIR)/scripts/tools/clientassets/scanrecords
VULNERABILITY_CHECK = $(shell cat bin/vulnerability-check 2>/dev/null)
vulngate: wheels
	@set -eu; mkdir -p bin; rm -f bin/vulnerability-check; \
	if test '$(VULNCHECK)' = skip; then \
		echo unchecked >bin/vulnerability-check; \
		echo 'vulngate: VULNCHECK=skip, so this build is stamped unchecked'; \
		exit 0; \
	fi; \
	CGO_ENABLED=0 ./scripts/govulncheck -db "$(VULNDB)" -format json ./cmd/bootwright >bin/govulncheck.json; \
	$(GO) -C scripts/tools run ./vulngate -govulncheck "$(CURDIR)/bin/govulncheck.json" \
		-lock "$(CURDIR)/internal/controller/runtimelock/lock.json" -table "$(CURDIR)/internal/controller/tooltable/tooltable.json" \
		-exceptions "$(CURDIR)/cmd/bootwright/vulnerability-exceptions.json" -verdict "$(CURDIR)/bin/vulnerability-check" \
		-records "$(CLIENT_RECORDS)" -db "$(VULNDB)"

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

# The executable carries the runtime lock's wheels, so install
# downloads none. The tool places them where the build embeds them, checking
# each against the lock, and keeps them in the shared cache directory, so a
# warm cache builds offline. Every target that compiles the module depends on
# it, so a parallel make never compiles while a wheel is being placed.
WHEELS_CACHE ?= $(shell ./scripts/cache-dir)/runtime-wheels
wheels:
	$(GO) -C scripts/tools run ./runtimewheels -lock "$(CURDIR)/internal/controller/runtimelock/lock.json" \
		-dest "$(CURDIR)/internal/controller/runtimelock/wheels" -cache "$(WHEELS_CACHE)"

# The controller stage installs each default client by the size and SHA-256
# the tool table pins. These read the publishers, which no build does.
TOOL_TABLE = $(CURDIR)/internal/controller/tooltable/tooltable.json
tool-table:
	$(GO) -C scripts/tools run ./tooltable -table "$(TOOL_TABLE)" verify

tool-table-outdated:
	$(GO) -C scripts/tools run ./tooltable -table "$(TOOL_TABLE)" outdated

tool-records-refresh:
	$(GO) -C scripts/tools run ./vulngate -table "$(TOOL_TABLE)" -records "$(CLIENT_RECORDS)" \
		-artifacts "$(CLIENTS_CACHE)" -refresh-records

tool-records-verify:
	$(GO) -C scripts/tools run ./vulngate -table "$(TOOL_TABLE)" -records "$(CLIENT_RECORDS)" \
		-artifacts "$(CLIENTS_CACHE)" -verify-records

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
execution-contracts-check: wheels
	$(GO) test ./cmd/bootwright -run '^TestEveryImplementationContractMatchesItsGolden$$'

build: wheels vulngate execution-contracts-check
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
		--build-arg VULN_EVALUATION="$$(date -u +%s.%N)" \
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

# A release ships what one clean, committed checkout built with the
# vulnerability gate on: the executable, its bill of materials, its third-party
# notices and the SHA-256 of the three. It is one recipe line, the Go command
# scripts/tools/release, so no check is a prerequisite or a sub-make that a flag
# could turn into a no-op: the Makefile refuses -n, -i, -t and -q while it is
# read, since they would otherwise run only the lines that name $(MAKE) or
# ignore a refusal, and the line is marked to run under them regardless; the
# command refuses another makefile
# read before this one, any variable the command line or an override sets
# other than RELEASE_INPUTS, and any RELEASE_DEFAULTED variable the environment
# sets; refuses a GO_IMAGE not pinned by digest, VULNCHECK other than on, a
# missing VERSION, a COMMIT or SOURCE that is not the checkout's and any
# modified or untracked file, before and after the build; builds into a
# private directory of RELEASE_DIR; refuses the build unless its executable
# reports a passed vulnerability verdict, the version and commit it was asked
# for and a clean source, and unless the bill of materials is the one the
# executable composes and the notices name that bill's digest; and only then
# replaces the shipped files. RELEASE_BUILD selects the host build (native) or
# the container build (container); either runs the gate, as a make that
# inherits only RELEASE_ENVIRONMENT and the release inputs, with no go env file.
RELEASE_DIR ?= dist
RELEASE_BUILD ?= native
RELEASE_FILES = bootwright bootwright.cdx.json bootwright.notices.txt
RELEASE_FLAGS = $(findstring n,$(firstword -$(MAKEFLAGS)))$(findstring i,$(firstword -$(MAKEFLAGS)))$(findstring t,$(firstword -$(MAKEFLAGS)))$(findstring q,$(firstword -$(MAKEFLAGS)))
RELEASE_INPUTS = VERSION COMMIT SOURCE VULNCHECK RELEASE_DIR RELEASE_BUILD CONTAINER_ENGINE GO_IMAGE PROXY_CA
RELEASE_DEFAULTED = VULNDB CLIENTS_CACHE CONTAINER_OUT WHEELS_CACHE
RELEASE_ENVIRONMENT = PATH HOME TMPDIR GOTMPDIR GOCACHE GOMODCACHE GOPATH BOOTWRIGHT_CACHE_DIR SSL_CERT_FILE SSL_CERT_DIR \
	XDG_RUNTIME_DIR XDG_CONFIG_HOME XDG_DATA_HOME DBUS_SESSION_BUS_ADDRESS DOCKER_HOST DOCKER_CONFIG DOCKER_CONTEXT \
	CONTAINER_HOST CONTAINER_CONNECTION REGISTRY_AUTH_FILE $(CONTAINER_SETTINGS)
empty :=
space := $(empty) $(empty)
release_origin = $(subst $(space),_,$(origin $(1)))
shell_quote = '$(subst ','\'',$(1))'
RELEASE_FOREIGN = $(foreach variable,$(filter-out $(RELEASE_INPUTS) GNUMAKEFLAGS MAKEFLAGS MFLAGS MAKELEVEL,$(.VARIABLES)),$(if $(filter command_line override environment_override,$(call release_origin,$(variable))),$(variable))) \
	$(foreach variable,$(RELEASE_DEFAULTED),$(if $(filter environment,$(origin $(variable))),$(variable)))

# -i would turn a refusal into a successful make, so the flags are refused while
# the Makefile is read, which no flag ignores; the recipe checks them again.
ifneq ($(filter release,$(MAKECMDGOALS)),)
ifneq ($(RELEASE_FLAGS),)
$(error release: make was run with -$(RELEASE_FLAGS), which would run only part of the recipe or ignore a refusal; run it without -n, -i, -t and -q)
endif
ifneq ($(filter command_line override environment_override,$(call release_origin,SHELL) $(call release_origin,.SHELLFLAGS)),)
$(error release: SHELL or .SHELLFLAGS is set from the command line or an override, which could run the recipe under another program; a release takes both from make)
endif
endif

release:
	+@GOENV=off GOFLAGS=-mod=readonly ./scripts/go -C scripts/tools run ./release -root $(call shell_quote,$(CURDIR)) \
		-make-flags $(call shell_quote,$(RELEASE_FLAGS)) -makefile-list $(call shell_quote,$(MAKEFILE_LIST)) $(if $(findstring --eval,$(MAKEFLAGS)),-eval) \
		$(foreach variable,$(RELEASE_FOREIGN),-foreign $(call shell_quote,$(variable)=$(origin $(variable)))) -inputs $(call shell_quote,$(RELEASE_INPUTS)) \
		-go-image $(call shell_quote,$(GO_IMAGE)) -vulncheck $(call shell_quote,$(VULNCHECK)) -build $(call shell_quote,$(RELEASE_BUILD)) \
		-version $(call shell_quote,$(VERSION)) -commit $(call shell_quote,$(COMMIT)) -source $(call shell_quote,$(SOURCE)) \
		-dir $(call shell_quote,$(RELEASE_DIR)) -engine $(call shell_quote,$(CONTAINER_ENGINE)) -proxy-ca $(call shell_quote,$(PROXY_CA)) \
		-make $(call shell_quote,$(MAKE)) -files $(call shell_quote,$(RELEASE_FILES)) -environment $(call shell_quote,$(RELEASE_ENVIRONMENT))

test: wheels
	# The timeout bounds a whole package, including the CLI's executable builds
	# while the full Ansible gate competes for CPU on cold CI runners.
	$(GO) test -timeout=20m ./...
	$(GO) -C scripts/tools test ./...

vet: wheels
	$(GO) vet ./...
	GOOS=darwin GOARCH=arm64 $(GO) vet ./...
	$(GO) -C scripts/tools vet ./...

# staticcheck's default checks over the module and its tests. A finding fails
# unless scripts/tools/staticgate/exclusions.txt names it, and an exclusion
# that covers fewer findings than it counts fails too, so the list only shrinks.
staticcheck:
	@set -eu; mkdir -p bin; status=0; \
	./scripts/staticcheck -f json ./... >bin/staticcheck.json || status=$$?; \
	test "$$status" -le 1 || { echo "staticcheck: the analyzer failed with status $$status" >&2; exit 1; }; \
	$(GO) -C scripts/tools run ./staticgate -findings "$(CURDIR)/bin/staticcheck.json" \
		-exclusions "$(CURDIR)/scripts/tools/staticgate/exclusions.txt" -root "$(CURDIR)"

fmt-check:
	@set -eu; \
	formatter="$$($(GO) env GOROOT)/bin/gofmt"; \
	directories="$$($(GO) list -buildvcs=false -tags=completion -f '{{.Dir}}' ./...; $(GO) -C scripts/tools list -buildvcs=false -f '{{.Dir}}' ./...)"; \
	unformatted="$$(printf '%s\n' "$$directories" | while IFS= read -r directory; do "$$formatter" -l "$$directory" || exit $$?; done)"; \
	if test -n "$$unformatted"; then printf 'Go files require formatting:\n%s\n' "$$unformatted" >&2; exit 1; fi

tidy-check:
	$(GO) mod tidy -diff
	$(GO) -C scripts/tools mod tidy -diff

modules-check:
	$(GO) mod verify
	$(GO) -C scripts/tools mod verify

completion-test: wheels
	$(GO) test -count=1 -tags=completion ./test/completion

vulncheck: wheels
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
# tests, the other names that test lists and every test that names a path
# under specs/ is listed, and every listed test exists.
# TestDocsCheckRecipeReadsTheList holds this recipe to the list.
DOCS_CHECKS := test/docs/testdata/docs-checks.txt
docs-check: wheels
	$(GO) test -count=1 -run '^($(shell awk '{print $$2}' $(DOCS_CHECKS) | paste -sd'|' -))$$' $(shell awk '{print "./" $$1}' $(DOCS_CHECKS) | sort -u)

# The probe tier: the narrowest checks of each file this branch changed, which
# stop at the first failure. It is no gate; make check is.
focus:
	python3 -I scripts/tools/focus.py

# The inner-loop tier: formatting, vet, the fitness suites and the packages
# this branch changed together with their dependents.
quick: fmt-check vet
	./scripts/quick-test

# Every production package that starts a goroutine, imports sync or takes a
# file lock; test/architecture refuses one missing here. cmd/bootwright takes
# about nine minutes under -race, so the timeout is raised from the 10-minute
# default to leave room for a loaded host (B1339, B1457).
RACE_PACKAGES = ./internal/machine/account/... ./internal/reconciliation/... ./internal/controller/privilege/... ./internal/trust/... ./cmd/bootwright/... \
	./ansible \
	./api/v1alpha1 \
	./internal/adapterprotocol \
	./internal/cli \
	./internal/controller \
	./internal/controller/hostdnf \
	./internal/controller/tooltable \
	./internal/controller/hostowned \
	./internal/controller/firewalllocal \
	./internal/containercluster/preflightlocal \
	./internal/storage/preflight \
	./internal/storage/preflightlocal \
	./internal/controller/stagelocal \
	./internal/controller/venvlocal \
	./internal/environment \
	./internal/environment/clusterpreflight \
	./internal/fanout \
	./internal/machine/access \
	./internal/machine/power \
	./internal/machine/sshlocal \
	./internal/nativeartifacts/comparisonlocal \
	./internal/nativeexec \
	./internal/nativefs \
	./internal/substrate/capacity \
	./internal/substrate/kubevirt \
	./internal/workspace/contextfs \
	./internal/workspace/contextfs/kernel \
	./internal/workspace/invokerfs \
	./internal/workspace/selectionfs

RACE_TIMEOUT = 30m

race: wheels
	$(GO) test -race -timeout $(RACE_TIMEOUT) $(RACE_PACKAGES)

# Statement coverage, which the gate does not measure, so make check runs each
# suite once: every Go package by its own tests, and the collection's plugins,
# per kind, by the plain unit loop under a standard-library recorder. The
# figures land in $(COVERAGE_DIR), and the check holds them to
# scripts/tools/coverage-floors.json, whose floors only rise; cover-floors
# raises them to the last measurement. CI runs it beside the gate.
COVERAGE_DIR := out/coverage
COVERAGE_FLOORS = python3 -I scripts/tools/coverage_floors.py --root "$(CURDIR)" --go-profile $(COVERAGE_DIR)/go.profile \
	--collection-lines $(COVERAGE_DIR)/collection --floors scripts/tools/coverage-floors.json
cover: wheels
	rm -rf $(COVERAGE_DIR)
	mkdir -p $(COVERAGE_DIR)/collection
	$(GO) test -timeout=20m -coverprofile=$(COVERAGE_DIR)/go.profile ./...
	./scripts/ansible-check --coverage $(COVERAGE_DIR)/collection
	$(COVERAGE_FLOORS) check --summary $(COVERAGE_DIR)/coverage.json

cover-floors:
	$(COVERAGE_FLOORS) refresh

# Every gate that needs no network once module and wheel caches are warm. Vulnerability
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

check-go: wheels fmt-check modules-check tidy-check licenses-check test vet staticcheck completion-test vulncheck vulngate
