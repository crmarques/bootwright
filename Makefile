.PHONY: build release wheels vulngate licenses licenses-check tool-table tool-table-outdated container-build test vet fmt-check modules-check tidy-check completion-test vulncheck ansible-check docs-check focus quick race check-offline check check-go

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

# A release ships what one clean, committed checkout built with the
# vulnerability gate on: the executable, its bill of materials, its third-party
# notices and the SHA-256 of the three. It is one recipe, run by one shell, so
# no check is a prerequisite or a sub-make that a flag could turn into a
# no-op: it refuses under -n, -i, -t and -q, which would otherwise run only
# the lines that name $(MAKE) or ignore a refusal; refuses a VULNDB, GO, STAMP
# or VULNERABILITY_CHECK that does not come from this file, VULNCHECK other
# than on, a missing VERSION, a COMMIT or SOURCE that is not the checkout's
# and any modified or untracked file, before and after the build; builds into a
# private directory of RELEASE_DIR; refuses the build unless its executable
# reports a passed vulnerability verdict, the version and commit it was asked
# for and a clean source, and unless the bill of materials is the one the
# executable composes and the notices name that bill's digest; and only then
# replaces the shipped files. RELEASE_BUILD selects the host build (native),
# which runs with the Go settings that change what is compiled removed from its
# environment, or the container build (container); either runs the gate.
RELEASE_DIR ?= dist
RELEASE_BUILD ?= native
RELEASE_FILES = bootwright bootwright.cdx.json bootwright.notices.txt
RELEASE_FLAGS = $(findstring n,$(firstword -$(MAKEFLAGS)))$(findstring i,$(firstword -$(MAKEFLAGS)))$(findstring t,$(firstword -$(MAKEFLAGS)))$(findstring q,$(firstword -$(MAKEFLAGS)))

# -i would turn a refusal into a successful make, so the flags are refused while
# the Makefile is read, which no flag ignores; the recipe checks them again.
ifneq ($(filter release,$(MAKECMDGOALS)),)
ifneq ($(RELEASE_FLAGS),)
$(error release: make was run with -$(RELEASE_FLAGS), which would run only part of the recipe or ignore a refusal; run it without -n, -i, -t and -q)
endif
endif

release:
	@set -eu; \
	refuse() { echo "release: $$*" >&2; exit 1; }; \
	test -z '$(RELEASE_FLAGS)' || refuse "make was run with -$(RELEASE_FLAGS), which would run only part of the recipe or ignore a refusal; run it without -n, -i, -t and -q"; \
	$(foreach variable,VULNDB GO STAMP VULNERABILITY_CHECK,test '$(origin $(variable))' = file || refuse "$(variable) comes from the $(origin $(variable)), which could change what the gate checks or what is stamped; a release takes it from the Makefile"; ) \
	test '$(VULNCHECK)' = on || refuse "VULNCHECK=$(VULNCHECK) is refused; a release is built with the vulnerability gate on"; \
	case '$(RELEASE_BUILD)' in native|container) ;; *) refuse "RELEASE_BUILD=$(RELEASE_BUILD) is neither native nor container" ;; esac; \
	case '$(VERSION)' in ''|*dirty*) refuse "name the release with VERSION=<version>; '$(VERSION)' is none or names a dirty tree" ;; esac; \
	head="$$(git rev-parse HEAD 2>/dev/null)" || refuse "this is no Git checkout, so no commit identifies the build"; \
	test "$$head" = '$(COMMIT)' || refuse "COMMIT=$(COMMIT) is not the checkout's commit $$head"; \
	tree_clean() { \
		changes="$$(git status --porcelain)" || refuse "git status failed"; \
		test -z "$$changes" || refuse "the tree has modified or untracked files, so the build would not be its commit:$$(printf '\n%s' "$$changes")"; \
	}; \
	tree_clean; \
	test '$(SOURCE)' = clean || refuse "SOURCE=$(SOURCE) is not clean"; \
	if test '$(RELEASE_BUILD)' = native; then \
		embedded="$$(git status --porcelain --ignored -- ansible/collections)" || refuse "git status failed"; \
		test -z "$$embedded" || refuse "ignored files under ansible/collections would be embedded in the executable:$$(printf '\n%s' "$$embedded")"; \
	fi; \
	out='$(RELEASE_DIR)'; \
	mkdir -p -- "$$out"; \
	for name in $(RELEASE_FILES) SHA256SUMS; do \
		if test -d "$$out/$$name" || { test -e "$$out/$$name" && ! test -O "$$out/$$name"; }; then \
			refuse "$$out/$$name is a directory or belongs to another user; remove it or choose another RELEASE_DIR"; \
		fi; \
	done; \
	stage="$$(mktemp -d "$$out/.build.XXXXXX")"; \
	trap 'rm -rf -- "$$stage"' EXIT; \
	trap 'exit 130' INT TERM; \
	case '$(RELEASE_BUILD)' in \
	native) \
		rm -f -- $(addprefix bin/,$(RELEASE_FILES)); \
		env -u GOFLAGS -u GOEXPERIMENT -u GOOS -u GOARCH GOFLAGS=-mod=readonly $(MAKE) --no-print-directory build VULNCHECK=on; \
		for name in $(RELEASE_FILES); do cp -- "bin/$$name" "$$stage/$$name"; done ;; \
	container) $(MAKE) --no-print-directory container-build VULNCHECK=on CONTAINER_OUT="$$stage" ;; \
	esac; \
	tree_clean; \
	for name in $(RELEASE_FILES); do \
		{ test -f "$$stage/$$name" && test -s "$$stage/$$name"; } || refuse "$$stage/$$name is missing or empty"; \
	done; \
	test -x "$$stage/bootwright" || refuse "$$stage/bootwright is not executable"; \
	identity="$$("$$stage/bootwright" version --output json)" || refuse "the built executable cannot report its version"; \
	field() { printf '%s\n' "$$identity" | sed -n 's/.*"'"$$1"'":"\([^"]*\)".*/\1/p'; }; \
	verdict="$$(field vulnerabilityCheck)"; \
	case "$$verdict" in \
	passed\ [0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]) ;; \
	*) refuse "the executable's vulnerability check is '$$verdict', not a pass; a release needs the gate to have passed" ;; \
	esac; \
	test "$$(field version)" = '$(VERSION)' || refuse "the executable reports version '$$(field version)', not '$(VERSION)'"; \
	test "$$(field commit)" = "$$head" || refuse "the executable reports commit '$$(field commit)', not this checkout's $$head"; \
	test "$$(field source)" = clean || refuse "the executable reports source '$$(field source)', not clean"; \
	"$$stage/bootwright" sbom --format cyclonedx-json >"$$stage/.sbom" || refuse "the built executable cannot write its bill of materials"; \
	cmp -s "$$stage/.sbom" "$$stage/bootwright.cdx.json" || refuse "bootwright.cdx.json is not the bill of materials of this executable"; \
	digest="$$(sha256sum "$$stage/bootwright.cdx.json")"; \
	grep -Fxq "Bill of materials: sha256:$${digest%% *}" "$$stage/bootwright.notices.txt" || refuse "bootwright.notices.txt was not composed from this bill of materials"; \
	rm -f -- "$$stage/.sbom"; \
	( cd "$$stage" && sha256sum $(RELEASE_FILES) >SHA256SUMS ); \
	{ test -f "$$stage/SHA256SUMS" && ( cd "$$stage" && sha256sum --check --strict SHA256SUMS >/dev/null ); } || refuse "SHA256SUMS does not verify the staged files"; \
	rm -f -- "$$out/SHA256SUMS"; \
	for name in bootwright.cdx.json bootwright.notices.txt bootwright SHA256SUMS; do mv -f -- "$$stage/$$name" "$$out/$$name"; done; \
	echo "release: $$out holds $(RELEASE_FILES) SHA256SUMS of $(VERSION), commit $$head"

test:
	# The timeout bounds a whole package, including the CLI's executable builds
	# while the full Ansible gate competes for CPU on cold CI runners.
	$(GO) test -timeout=20m ./...
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
