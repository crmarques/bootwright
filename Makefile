GO ?= go
PYTHON ?= python3
DOCKER ?= docker
STATICCHECK ?= $(shell command -v staticcheck 2>/dev/null)
SHELLCHECK ?= $(shell command -v shellcheck 2>/dev/null)
COMMA := ,
BINARY ?= bootwright
BIN_DIR ?= bin
STATE_DIR ?= .state
CLEAN_PATHS = $(BIN_DIR) $(STATE_DIR) $(EMBED_BUNDLE_ARCHIVE) dist build out rendered tmp
CONTAINER_IMAGE ?= bootwright
CONTAINERFILE ?= Containerfile
CONTAINER_CACHE_DIR ?= .cache/container-build
CONTAINER_CACHE_NEXT_DIR ?= $(CONTAINER_CACHE_DIR).next
CONTAINER_CACHE_FROM = $(if $(wildcard $(CONTAINER_CACHE_DIR)/index.json),--cache-from type=local$(COMMA)src=$(CONTAINER_CACHE_DIR))
TEST_HOME ?= $(abspath $(STATE_DIR)/home)
E2E_DIR ?= test/e2e
CASE ?=
E2E_FIXTURE = $(E2E_DIR)/$(CASE)
E2E_CONTEXT_ROOT ?= /var/lib/bootwright/contexts
E2E_CONTEXT_DIR ?= $(E2E_CONTEXT_ROOT)/$(CASE)
E2E_CONTEXT_EXPECTED = $(E2E_CONTEXT_ROOT)/$(CASE)
E2E_HOME ?= $(abspath $(STATE_DIR)/e2e-home/$(CASE))
ANSIBLE_PLAYBOOK ?= $(shell command -v ansible-playbook 2>/dev/null)
E2E_ANSIBLE_FLAGS = $(if $(ANSIBLE_PLAYBOOK),--ansible-playbook $(ANSIBLE_PLAYBOOK),)
E2E_APPLY_ALL ?= $(BIN_DIR)/$(BINARY) apply --yes
E2E_APPLY_FLAGS ?=
E2E_CLEAN ?= sudo rm -rf
# ADRs intentionally describe the abandoned shape in their Context
# sections; exclude /specs/adr from the stale-term sweep.
DEFINITION_CHECK_PATHS = README.md docs specs/state-model.md specs/architecture.md specs/domain.md specs/security.md specs/index.md specs/README.md test add-ons $(wildcard examples)

ANSIBLE_SRC_DIR = ansible
EMBED_BUNDLE_ARCHIVE = internal/converge/bundle/ansible_bundle.zip
BUNDLE_WORK_DIR = $(STATE_DIR)/ansible-bundle
ANSIBLE_GALAXY ?= $(shell command -v ansible-galaxy 2>/dev/null)
ANSIBLE_LINT ?= $(shell command -v ansible-lint 2>/dev/null)
YAMLLINT ?= $(shell command -v yamllint 2>/dev/null)
COLLECTIONS_REQUIREMENTS = $(ANSIBLE_SRC_DIR)/collections/requirements.yml
COLLECTIONS_LOCK = $(ANSIBLE_SRC_DIR)/collections/requirements.lock.yml
EMBED_COLLECTIONS_DIR = $(BUNDLE_WORK_DIR)/collections
EMBED_COLLECTIONS_ABS_DIR = $(abspath $(EMBED_COLLECTIONS_DIR))
COLLECTIONS_STAMP = $(EMBED_COLLECTIONS_DIR)/.stamp
# Anchor Ansible temp dirs under the repo-local state dir instead of a fixed,
# world-predictable path in shared /var/tmp. The shared path breaks multi-user
# hosts (whoever creates it first owns it) and is squattable; the state dir is
# per-checkout and cleaned by `make clean`. Behavior is identical for one user.
ANSIBLE_LOCAL_TEMP_DIR = $(abspath $(STATE_DIR))/ansible-tmp/local
ANSIBLE_REMOTE_TEMP_DIR = $(abspath $(STATE_DIR))/ansible-tmp/remote
ANSIBLE_GALAXY_ENV = \
	ANSIBLE_LOCAL_TEMP=$(ANSIBLE_LOCAL_TEMP_DIR) \
	ANSIBLE_REMOTE_TEMP=$(ANSIBLE_REMOTE_TEMP_DIR) \
	ANSIBLE_COLLECTIONS_PATH=$(EMBED_COLLECTIONS_ABS_DIR) \
	ANSIBLE_COLLECTIONS_PATHS=$(EMBED_COLLECTIONS_ABS_DIR)
GOFMT_FILES = $(shell find add-ons api cmd internal -type f -name '*.go' -print)
GO_TEST_PACKAGES ?= ./...
GO_TEST_CHECK_FLAGS ?= -vet=off
# internal/cli is a large integration-style package; under -race it runs ~12min
# on slower machines, past Go's 600s/package default. Give the race run headroom
# so it fails on real races, not the clock.
GO_TEST_RACE_FLAGS ?= -vet=off -race -timeout 1800s
BOOTWRIGHT_COLLECTIONS_DIR = $(abspath $(ANSIBLE_SRC_DIR)/collections)
BOOTWRIGHT_COLLECTION_ROOT = $(ANSIBLE_SRC_DIR)/collections/ansible_collections/bootwright/core
ANSIBLE_SYNTAX_ENV = ANSIBLE_LOCAL_TEMP=$(ANSIBLE_LOCAL_TEMP_DIR) ANSIBLE_REMOTE_TEMP=$(ANSIBLE_REMOTE_TEMP_DIR) ANSIBLE_COLLECTIONS_PATH=$(BOOTWRIGHT_COLLECTIONS_DIR):$(EMBED_COLLECTIONS_ABS_DIR)
ANSIBLE_SYNTAX_PLAYBOOKS = \
	bootwright.core.check_become \
	bootwright.core.check_preflight \
	bootwright.core.check_external_reachability \
	bootwright.core.workflow_all_apply \
	bootwright.core.workflow_infra_apply \
	bootwright.core.workflow_infra_destroy_artifact_server \
	bootwright.core.workflow_infra_destroy \
	bootwright.core.workflow_clusters_apply \
	bootwright.core.workflow_clusters_destroy \
	bootwright.core.workflow_container_cluster_apply \
	bootwright.core.workflow_bastion_apply_tools \
	bootwright.core.task_provider_services_apply \
	bootwright.core.task_provider_services_destroy \
	bootwright.core.task_infra_component_services_apply \
	bootwright.core.task_infra_component_services_destroy \
	bootwright.core.task_machine_infra_prepare \
	bootwright.core.task_machine_infra_apply \
	bootwright.core.task_machine_infra_finalize \
	bootwright.core.task_machine_infra_destroy \
	bootwright.core.task_managed_machine_os_apply \
	bootwright.core.task_container_cluster_create_agent_iso \
	bootwright.core.task_container_cluster_boot_agent_machine \
	bootwright.core.task_container_cluster_wait_agent_install \
	bootwright.core.task_container_cluster_agent_install \
	bootwright.core.task_container_cluster_agent_destroy \
	bootwright.core.task_host_virtctl_provision \
	bootwright.core.task_storage_cluster_apply \
	bootwright.core.task_storage_cluster_destroy

E2E_CASES = $(notdir $(patsubst %/,%,$(wildcard $(E2E_DIR)/*/)))

.PHONY: all build go-build container-build sync-bundle test validate plan check check-fast check-go-source-visibility check-gofmt go-test-clean-checkout staticcheck go-mod-tidy-check python-test ansible-syntax-check ansible-lint-check shellcheck-check workflow-yaml-check stale-term-check cli-file-size-check containerfile-pin-check check-e2e-deps check-e2e-case list-e2e-cases e2e-dry-run e2e clean clean-e2e-state help

# Architecture guardrail: keep internal/cli files thin so domain logic stays
# in internal/converge/workflow/. The current observed max (init.go ~391) is the
# floor; do not raise this without a deliberate refactor justification.
# The intent is to catch new growth before files turn into god files again.
CLI_FILE_LINE_LIMIT ?= 400
WORKFLOW_FILE_LINE_LIMIT ?= 1000
# internal/state/desired holds the per-kind validators. They run longer than
# thin CLI handlers but must not become god files, so the largest is split by
# kind to stay under this. Keep it sub-1000 so genuine growth is caught.
# internal/state/graph shares the same budget (largest today: services.go ~811).
VALIDATOR_FILE_LINE_LIMIT ?= 900
# api/v1alpha1 holds the public API type declarations, split per kind. The
# largest today (types.go ~588) is the floor; a file crossing this means a
# kind's types should split into their own storage_<kind>.go-style file.
API_FILE_LINE_LIMIT ?= 600

all: build

# Stamp the binary with version metadata via ldflags. VERSION may be an
# explicit tag (set by CI/releases); fall back to `git describe` when
# the repo has tags, then the short SHA, then "dev".
ifeq ($(strip $(VERSION)),)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
endif
ifeq ($(strip $(GIT_COMMIT)),)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
endif
LDFLAGS = -X github.com/crmarques/bootwright/internal/cli.versionString=$(VERSION) \
          -X github.com/crmarques/bootwright/internal/cli.gitCommit=$(GIT_COMMIT)

# CGO_ENABLED=0 forces a static, self-contained binary. The CLI is copied out of
# the container image and installed onto operator/bastion hosts whose glibc may
# be older than the build host's; a dynamically linked cgo build (pulled in by
# os/user and the net resolver) segfaults at load time on those hosts. Disabling
# cgo also makes Go use its pure-Go os/user and net implementations, which need
# no shared libraries at runtime.
GO_BUILD_CMD = CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/bootwright

build: $(BIN_DIR) sync-bundle
	$(GO_BUILD_CMD)

# Compile the binary assuming the embedded ansible bundle is already in place.
# The container build runs `sync-bundle` and this compile in separate layers so
# the per-commit version stamp only re-links here instead of re-packing the
# bundle. Local developers should keep using `build`, which syncs first.
go-build: $(BIN_DIR)
	$(GO_BUILD_CMD)

container-build:
	@test -n "$(CONTAINER_CACHE_DIR)" || { printf '%s\n' 'CONTAINER_CACHE_DIR must not be empty'; exit 1; }
	@test -n "$(CONTAINER_CACHE_NEXT_DIR)" || { printf '%s\n' 'CONTAINER_CACHE_NEXT_DIR must not be empty'; exit 1; }
	@case "$(CONTAINER_CACHE_DIR)" in /|.|..|/*|../*|*/../*|*/..) printf 'refusing to remove unsafe CONTAINER_CACHE_DIR: %s\n' "$(CONTAINER_CACHE_DIR)"; exit 1;; esac
	@case "$(CONTAINER_CACHE_NEXT_DIR)" in /|.|..|/*|../*|*/../*|*/..) printf 'refusing to remove unsafe CONTAINER_CACHE_NEXT_DIR: %s\n' "$(CONTAINER_CACHE_NEXT_DIR)"; exit 1;; esac
	mkdir -p $(CONTAINER_CACHE_DIR)
	rm -rf $(CONTAINER_CACHE_NEXT_DIR)
	DOCKER_BUILDKIT=1 $(DOCKER) buildx build --load \
		$(CONTAINER_CACHE_FROM) \
		--cache-to type=local,dest=$(CONTAINER_CACHE_NEXT_DIR),mode=max \
		--build-arg "HTTP_PROXY=$${HTTP_PROXY:-}" \
		--build-arg "HTTPS_PROXY=$${HTTPS_PROXY:-}" \
		--build-arg "NO_PROXY=$${NO_PROXY:-}" \
		--build-arg "http_proxy=$${http_proxy:-}" \
		--build-arg "https_proxy=$${https_proxy:-}" \
		--build-arg "no_proxy=$${no_proxy:-}" \
		--build-arg "VERSION=$(VERSION)" \
		--build-arg "GIT_COMMIT=$(GIT_COMMIT)" \
		-t $(CONTAINER_IMAGE) \
		-f $(CONTAINERFILE) \
		.
	rm -rf $(CONTAINER_CACHE_DIR)
	mv $(CONTAINER_CACHE_NEXT_DIR) $(CONTAINER_CACHE_DIR)

# Collections declared in $(COLLECTIONS_REQUIREMENTS) are resolved at build
# time into $(EMBED_COLLECTIONS_DIR), then packed with /ansible into
# $(EMBED_BUNDLE_ARCHIVE) so disconnected hosts never reach Galaxy.
sync-bundle: $(COLLECTIONS_STAMP)
	@$(PYTHON) scripts/sync-ansible-bundle.py \
		--source $(ANSIBLE_SRC_DIR) \
		--collections $(EMBED_COLLECTIONS_DIR) \
		--output $(EMBED_BUNDLE_ARCHIVE)
	@$(GO) test ./internal/repo/bundlecheck -run TestAnsibleCollectionLockMatchesEmbeddedManifest

# Galaxy download is gated on requirements.yml and its lock metadata.
$(COLLECTIONS_STAMP): $(COLLECTIONS_REQUIREMENTS) $(COLLECTIONS_LOCK)
	@test -n "$(ANSIBLE_GALAXY)" || { printf '%s\n' 'ansible-galaxy not found in PATH; install Ansible or set ANSIBLE_GALAXY=/path/to/ansible-galaxy'; exit 1; }
	@rm -rf $(EMBED_COLLECTIONS_DIR)/ansible_collections
	@$(ANSIBLE_GALAXY_ENV) $(ANSIBLE_GALAXY) collection install --no-deps -r $(COLLECTIONS_REQUIREMENTS) -p $(EMBED_COLLECTIONS_ABS_DIR) >/dev/null
	@# Slim embedded collections: strip test/CI/docs trees that bloat the
	@# binary without contributing to runtime module execution.
	@find $(EMBED_COLLECTIONS_DIR)/ansible_collections -maxdepth 8 -type d \( \
		-name tests -o -name docs -o -name examples -o -name changelogs \
		-o -name .github -o -name .azure-pipelines -o -name ci \
		\) -exec rm -rf {} +
	@# Verify the freshly-installed collections against requirements.lock.yml at
	@# install time -- before the bundle is packed and embedded -- so a tampered or
	@# unexpected download fails the build the moment it is unpacked. The post-pack
	@# Go test (TestAnsibleCollectionLockMatchesEmbeddedManifest) remains a second
	@# line of defence on the shipped artifact.
	@$(PYTHON) scripts/verify-ansible-collections.py --collections $(EMBED_COLLECTIONS_DIR) --lock $(COLLECTIONS_LOCK)
	@touch $@

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

test:
	$(GO) test $(GO_TEST_PACKAGES)

# Order checks from cheapest to slowest so local runs fail before starting
# race tests or clean-copy tests when lightweight guardrails already caught it.
check: check-fast
	$(GO) vet $(GO_TEST_PACKAGES)
	$(MAKE) staticcheck
	$(MAKE) go-mod-tidy-check
	$(GO) test $(GO_TEST_CHECK_FLAGS) $(GO_TEST_PACKAGES)
	$(MAKE) python-test
	$(MAKE) ansible-syntax-check
	$(MAKE) ansible-lint-check
	$(MAKE) workflow-yaml-check
	$(GO) test $(GO_TEST_RACE_FLAGS) $(GO_TEST_PACKAGES)
	$(MAKE) go-test-clean-checkout

# sync-bundle first so the embedded ansible bundle matches the source tree
# before the go tests run; otherwise TestEmbeddedBundleMatchesSourceAnsible
# fails (or skips) against a stale or absent gitignored bundle artifact.
check-fast: sync-bundle cli-file-size-check check-go-source-visibility check-gofmt stale-term-check containerfile-pin-check shellcheck-check check-e2e-deps
	$(GO) test $(GO_TEST_PACKAGES)

check-go-source-visibility:
	@ignored=$$(find add-ons api cmd internal -type f -name '*.go' -print | git check-ignore --stdin 2>/dev/null || true); \
	if [ -n "$$ignored" ]; then \
		printf '%s\n' 'Go source files are ignored by git and will be missing from GitHub Actions:'; \
		printf '  %s\n' $$ignored; \
		printf '%s\n' 'Fix .gitignore or move generated files outside api/, cmd/, and internal/.'; \
		exit 1; \
	fi

check-gofmt:
	@test -z "$$(gofmt -l $(GOFMT_FILES))" || { gofmt -l $(GOFMT_FILES); exit 1; }

go-test-clean-checkout:
	@set -e; \
	tmp=$$(mktemp -d); \
	work=$$tmp/work; \
	trap 'rm -rf "$$tmp"' EXIT; \
	mkdir -p "$$work"; \
	tar \
		--exclude='./.git' \
		--exclude='./$(BIN_DIR)' \
		--exclude='./$(STATE_DIR)' \
		--exclude='./dist' \
		--exclude='./build' \
		--exclude='./out' \
		--exclude='./rendered' \
		--exclude='./tmp' \
		--exclude='./.cache' \
		--exclude='./$(EMBED_BUNDLE_ARCHIVE)' \
		-cf "$$tmp/source.tar" .; \
	tar -xf "$$tmp/source.tar" -C "$$work"; \
	mkdir -p "$$tmp/go-build-cache" "$$tmp/go-tmp"; \
	cd "$$work"; \
	GOCACHE="$$tmp/go-build-cache" GOTMPDIR="$$tmp/go-tmp" $(GO) test $(GO_TEST_CHECK_FLAGS) $(GO_TEST_PACKAGES)

staticcheck:
	@test -n "$(STATICCHECK)" || { printf '%s\n' 'staticcheck not found in PATH; install with go install honnef.co/go/tools/cmd/staticcheck@v0.7.0 or set STATICCHECK=/path/to/staticcheck'; exit 1; }
	$(STATICCHECK) $(GO_TEST_PACKAGES)

# Fail if `go mod tidy` would change go.mod/go.sum, so dependency metadata
# stays minimal and complete. Backs up go.mod/go.sum, runs tidy in place, then
# diffs against the backup. A trap restores the backup and removes the temp dir
# on any exit (including an interrupt mid-recipe), so the working tree is left
# untouched whether the check passes, fails, or is aborted. A tidy failure (for
# example a proxy/network error) fails the target instead of yielding a false
# green from an unchanged-but-unverified tree.
go-mod-tidy-check:
	@tmp=$$(mktemp -d) || exit 1; \
	cp go.mod go.sum "$$tmp/"; \
	trap 'cp "$$tmp/go.mod" go.mod; cp "$$tmp/go.sum" go.sum; rm -rf "$$tmp"' EXIT; \
	trap 'exit 130' INT; trap 'exit 143' TERM; \
	$(GO) mod tidy || { printf '%s\n' 'go mod tidy failed (network/proxy error?); dependency tidiness not verified'; exit 1; }; \
	rc=0; \
	diff -u "$$tmp/go.mod" go.mod || rc=1; \
	diff -u "$$tmp/go.sum" go.sum || rc=1; \
	if [ $$rc -ne 0 ]; then printf '%s\n' 'go.mod/go.sum are not tidy; run: go mod tidy'; fi; \
	exit $$rc

# Filter-plugin unit tests use only stdlib unittest so the check works
# on any Python 3 install without a venv. If pytest is installed
# locally, `python3 -m pytest $(BOOTWRIGHT_COLLECTION_ROOT)/plugins/filter/` discovers the
# same TestCase classes.
python-test:
	@set -e; \
	trap 'find $(BOOTWRIGHT_COLLECTION_ROOT)/plugins/filter scripts -type d -name __pycache__ -prune -exec rm -rf {} +' EXIT; \
	cd $(BOOTWRIGHT_COLLECTION_ROOT)/plugins/filter && python3 -m unittest discover -v; \
	cd - >/dev/null; \
	$(PYTHON) -m unittest discover -s scripts -p 'test_*.py' -v

ansible-syntax-check: check-e2e-deps $(COLLECTIONS_STAMP)
	@for playbook in $(ANSIBLE_SYNTAX_PLAYBOOKS); do \
		$(ANSIBLE_SYNTAX_ENV) $(ANSIBLE_PLAYBOOK) --syntax-check -i localhost, "$$playbook"; \
	done

# Lint the embedded collection: yamllint for YAML formatting, ansible-lint for
# Ansible semantics (idempotency, no_log, command-vs-module, ...). The current
# tree is a green baseline; .ansible-lint skips only reviewed house-style rules,
# so the gate catches new regressions in those classes. ansible-lint runs from
# the collection root for role/playbook auto-discovery, with the same collection
# path the syntax check uses so community.* dependencies resolve.
ansible-lint-check: check-e2e-deps $(COLLECTIONS_STAMP)
	@test -n "$(YAMLLINT)" || { printf '%s\n' 'yamllint not found in PATH; install with python3 -m pip install yamllint or set YAMLLINT=/path/to/yamllint'; exit 1; }
	@test -n "$(ANSIBLE_LINT)" || { printf '%s\n' 'ansible-lint not found in PATH; install with python3 -m pip install ansible-lint or set ANSIBLE_LINT=/path/to/ansible-lint'; exit 1; }
	$(YAMLLINT) -c $(CURDIR)/.yamllint $(ANSIBLE_SRC_DIR)
	cd $(BOOTWRIGHT_COLLECTION_ROOT) && \
		ANSIBLE_COLLECTIONS_PATH=$(BOOTWRIGHT_COLLECTIONS_DIR):$(EMBED_COLLECTIONS_ABS_DIR) \
		ANSIBLE_LOCAL_TEMP=$(ANSIBLE_LOCAL_TEMP_DIR) \
		ANSIBLE_REMOTE_TEMP=$(ANSIBLE_REMOTE_TEMP_DIR) \
		$(ANSIBLE_LINT) --offline -c $(CURDIR)/.ansible-lint

# Static-check the repo's authored shell scripts. `shellcheck -x` follows
# `source`d files so sourced fragments are analysed too. Discovery is by shebang
# (not extension) over ansible/ and scripts/, excluding .git and .state (the
# latter holds the build-time downloaded collections). Fails on any finding.
# CI installs shellcheck so this always runs there; a local machine without
# shellcheck gets a clear install hint rather than a silent pass or a confusing
# error (a no-op-success when the tool is absent is deliberately not wanted).
shellcheck-check:
	@test -n "$(SHELLCHECK)" || { printf '%s\n' 'shellcheck not found in PATH; install shellcheck (dnf install ShellCheck / apt-get install shellcheck) or set SHELLCHECK=/path/to/shellcheck'; exit 1; }
	@files=$$(find $(ANSIBLE_SRC_DIR) scripts -type f -not -path '*/.git/*' -not -path '*/$(STATE_DIR)/*' -print \
		| while IFS= read -r f; do \
			IFS= read -r first < "$$f" 2>/dev/null || continue; \
			case "$$first" in \
				'#!'*/sh|'#!'*/sh' '*|'#!'*/bash|'#!'*/bash' '*|'#!'*'env sh'*|'#!'*'env bash'*) printf '%s\n' "$$f";; \
			esac; \
		done); \
	test -n "$$files" || { printf '%s\n' 'shellcheck-check: no authored shell scripts discovered under $(ANSIBLE_SRC_DIR)/ scripts/ (discovery filter broken?)'; exit 1; }; \
	printf '%s\n' "$$files" | xargs $(SHELLCHECK) -x

# yamllint the GitHub Actions workflows. The .yamllint config is Ansible-tuned
# and ignores .github/, so pass an inline config: keep the structural rules
# (key-duplicates, indentation, trailing whitespace) that catch a broken
# workflow, and relax document-start (workflows legitimately omit the leading
# ---) and line-length. Without this the workflow YAML is caught by nothing
# local and a duplicate key or bad indent only surfaces when GitHub runs it.
workflow-yaml-check:
	@test -n "$(YAMLLINT)" || { printf '%s\n' 'yamllint not found in PATH; install with python3 -m pip install yamllint or set YAMLLINT=/path/to/yamllint'; exit 1; }
	$(YAMLLINT) -d '{extends: default, rules: {document-start: disable, line-length: disable, truthy: {check-keys: false}, comments-indentation: disable}}' .github/workflows

stale-term-check:
	@if command -v rg >/dev/null 2>&1; then \
		rg -n 'providerRefs|HostPool|spec\.machine\.libvirt|services\.bootArtifacts|services\.loadBalancer|services\.proxy|services\.registry|services\.nameResolution|MachineFlavorBareMetal|BuildClosure|input-files|/state/|/workflow/|runtime/[^/]+/installer|internal/runtime/|internal/infra/support|internal/converge/checks' $(DEFINITION_CHECK_PATHS); \
		status=$$?; \
	else \
		grep -RInE 'providerRefs|HostPool|spec\.machine\.libvirt|services\.bootArtifacts|services\.loadBalancer|services\.proxy|services\.registry|services\.nameResolution|MachineFlavorBareMetal|BuildClosure|input-files|/state/|/workflow/|runtime/[^/]+/installer|internal/runtime/|internal/infra/support|internal/converge/checks' $(DEFINITION_CHECK_PATHS); \
		status=$$?; \
	fi; \
	if [ "$$status" -eq 0 ]; then exit 1; fi; \
	if [ "$$status" -eq 1 ]; then exit 0; fi; \
	exit "$$status"

# Reject CLI / render files that have grown past the thin-handler
# threshold. Excludes test files so the lint targets production code
# only. Scope includes CLI, render and its families, converge orchestration,
# the extracted service components, the internal/state/desired and
# internal/state/graph validators, and the api/v1alpha1 type files —
# the packages where god-files previously accumulated.
cli-file-size-check:
	@over=$$(find internal/cli internal/render internal/render/installer internal/render/inventory internal/render/ceph internal/state/scaffold internal/converge internal/converge/bastion internal/secrets internal/sshtrust internal/preflight internal/status internal/clusteraccess internal/infra/proxy internal/host/become -maxdepth 1 -type f -name '*.go' ! -name '*_test.go' -printf '%p\n' \
		| while read -r f; do \
			n=$$(wc -l <"$$f"); \
			if [ "$$n" -gt $(CLI_FILE_LINE_LIMIT) ]; then printf '  %s lines\t%s\n' "$$n" "$$f"; fi; \
		done); \
	if [ -n "$$over" ]; then \
		printf '%s\n' "files over $(CLI_FILE_LINE_LIMIT) lines (decompose by concern; orchestration belongs in internal/converge, readiness rules in internal/preflight, observe logic in internal/status, render emitters split by family under internal/render/):" "$$over"; \
		exit 1; \
	fi
	@over=$$(find internal/converge/workflow -maxdepth 1 -type f -name '*.go' ! -name '*_test.go' -printf '%p\n' \
		| while read -r f; do \
			n=$$(wc -l <"$$f"); \
			if [ "$$n" -gt $(WORKFLOW_FILE_LINE_LIMIT) ]; then printf '  %s lines\t%s\n' "$$n" "$$f"; fi; \
		done); \
	if [ -n "$$over" ]; then \
		printf '%s\n' "workflow files over $(WORKFLOW_FILE_LINE_LIMIT) lines (split planning, scheduling, resources, logs, and ledger responsibilities):" "$$over"; \
		exit 1; \
	fi
	@over=$$(find internal/state/desired internal/state/graph -maxdepth 1 -type f -name '*.go' ! -name '*_test.go' -printf '%p\n' \
		| while read -r f; do \
			n=$$(wc -l <"$$f"); \
			if [ "$$n" -gt $(VALIDATOR_FILE_LINE_LIMIT) ]; then printf '  %s lines\t%s\n' "$$n" "$$f"; fi; \
		done); \
	if [ -n "$$over" ]; then \
		printf '%s\n' "validator files over $(VALIDATOR_FILE_LINE_LIMIT) lines (split by kind, e.g. validate_storage_pools.go / validate_storage_exports.go / validate_storage_stretch.go):" "$$over"; \
		exit 1; \
	fi
	@over=$$(find api/v1alpha1 -maxdepth 1 -type f -name '*.go' ! -name '*_test.go' -printf '%p\n' \
		| while read -r f; do \
			n=$$(wc -l <"$$f"); \
			if [ "$$n" -gt $(API_FILE_LINE_LIMIT) ]; then printf '  %s lines\t%s\n' "$$n" "$$f"; fi; \
		done); \
	if [ -n "$$over" ]; then \
		printf '%s\n' "API type files over $(API_FILE_LINE_LIMIT) lines (split per kind, e.g. storage_cluster.go / storage_pool.go / storage_filesystem.go):" "$$over"; \
		exit 1; \
	fi

containerfile-pin-check:
	@files=$$(find . -type f -name Containerfile -print | sort); \
	awk 'BEGIN { status = 0 } /^FROM[[:space:]]/ && $$2 != "scratch" && $$2 !~ /@sha256:[0-9a-f]{64}$$/ { printf "%s:%d: Containerfile base image must be digest-pinned: %s\n", FILENAME, FNR, $$0; status = 1 } /^#[[:space:]]*syntax[[:space:]]*=/ && $$0 !~ /@sha256:[0-9a-f]{64}$$/ { printf "%s:%d: Containerfile syntax directive must be digest-pinned: %s\n", FILENAME, FNR, $$0; status = 1 } END { exit status }' $$files

validate: build
	HOME=$(TEST_HOME) $(BIN_DIR)/$(BINARY) validate -f test/e2e/001-sno-libvirt

plan: build
	HOME=$(TEST_HOME) $(BIN_DIR)/$(BINARY) context init --name plan -f test/e2e/001-sno-libvirt --yes
	HOME=$(TEST_HOME) $(BIN_DIR)/$(BINARY) render installer

check-e2e-deps:
	@test -n "$(ANSIBLE_PLAYBOOK)" || { printf '%s\n' 'ansible-playbook not found in PATH; install Ansible or set ANSIBLE_PLAYBOOK=/path/to/ansible-playbook'; exit 1; }

check-e2e-case:
ifeq ($(strip $(CASE)),)
	@test -n "$(CASE)" || { printf '%s\n' 'CASE is required; pass CASE=<name>, e.g. make e2e CASE=001-sno-libvirt' 'Available cases:' $(addprefix '  ',$(E2E_CASES)); exit 1; }
endif
ifneq ($(strip $(filter /% . ..,$(CASE))$(findstring /,$(CASE))$(findstring ..,$(CASE))),)
	@printf '%s\n' 'refusing unsafe CASE; pass one of:' $(addprefix '  ',$(E2E_CASES)); exit 1
endif
ifeq ($(strip $(filter $(CASE),$(E2E_CASES))),)
	@printf '%s\n' 'CASE "$(CASE)" is not a known e2e case' 'Available cases:' $(addprefix '  ',$(E2E_CASES)); exit 1
endif
	@test -d "$(E2E_FIXTURE)" || { printf '%s\n' 'CASE "$(CASE)" not found at $(E2E_FIXTURE)' 'Available cases:' $(addprefix '  ',$(E2E_CASES)); exit 1; }

list-e2e-cases:
	@printf '%s\n' 'Available e2e cases:' $(addprefix '  ',$(E2E_CASES))

e2e-dry-run: check-e2e-case check-e2e-deps build
	HOME=$(E2E_HOME) $(BIN_DIR)/$(BINARY) context init --name $(CASE) -f $(E2E_FIXTURE) --yes
	HOME=$(E2E_HOME) $(BIN_DIR)/$(BINARY) apply --dry-run $(E2E_ANSIBLE_FLAGS) $(E2E_APPLY_FLAGS)

e2e: check-e2e-case check-e2e-deps build
	HOME=$(E2E_HOME) $(BIN_DIR)/$(BINARY) context init --name $(CASE) -f $(E2E_FIXTURE) --yes
	HOME=$(E2E_HOME) $(E2E_APPLY_ALL) $(E2E_ANSIBLE_FLAGS) $(E2E_APPLY_FLAGS)

clean:
	@test -n "$(BIN_DIR)" || { printf '%s\n' 'BIN_DIR must not be empty'; exit 1; }
	@test -n "$(STATE_DIR)" || { printf '%s\n' 'STATE_DIR must not be empty'; exit 1; }
	@for p in $(CLEAN_PATHS); do \
		case "$$p" in /|.|..|/*|../*|*/../*|*/..) printf 'refusing to clean unsafe path: %s\n' "$$p"; exit 1;; esac; \
	done
	@for p in $(CLEAN_PATHS); do rm -rf "$$p"; done

clean-e2e-state: check-e2e-case
	@test -n "$(E2E_CONTEXT_DIR)" || { printf '%s\n' 'E2E_CONTEXT_DIR must not be empty'; exit 1; }
	@test "$(E2E_CONTEXT_DIR)" = "$(E2E_CONTEXT_EXPECTED)" || { printf 'refusing to clean E2E_CONTEXT_DIR outside expected case context: got %s; want %s\n' "$(E2E_CONTEXT_DIR)" "$(E2E_CONTEXT_EXPECTED)"; exit 1; }
	$(E2E_CLEAN) "$(E2E_CONTEXT_DIR)"

help:
	@printf '%s\n' \
		'Targets:' \
		'  build            Build bin/bootwright (syncs the embedded ansible bundle first)' \
		'  container-build  Build the bootwright CLI image with a host-backed BuildKit cache' \
		'  sync-bundle      Generate internal/converge/bundle/ansible_bundle.zip' \
		'  check            Run fast guardrails, then Go/Python/Ansible checks, bundle sync, and go.mod tidiness' \
		'  check-fast       Run cheap local guardrails plus Go unit tests (syncs the ansible bundle and needs ansible-playbook; no race, staticcheck, lint, or clean-checkout)' \
		'  test             Run Go tests' \
		'  validate         Validate test/e2e/001-sno-libvirt' \
		'  plan             Render installer assets for test/e2e/001-sno-libvirt into .state' \
		'  list-e2e-cases   List available e2e cases under test/e2e' \
		'  check-e2e-deps   Check local e2e dependencies' \
		'  e2e-dry-run         Render an e2e fixture and print Ansible command (requires CASE=<name>)' \
		'  e2e                 Run an e2e fixture with sudo (requires CASE=<name>)' \
		'  clean               Remove workspace-local generated outputs' \
		'  clean-e2e-state     Remove generated e2e CLI state with sudo (requires CASE=<name>)'
