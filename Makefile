GO ?= go
PYTHON ?= python3
DOCKER ?= docker
COMMA := ,
BINARY ?= bootwright
BIN_DIR ?= bin
STATE_DIR ?= .state
CLEAN_PATHS = $(BIN_DIR) $(STATE_DIR) dist build out rendered tmp
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
E2E_APPLY_ALL ?= $(BIN_DIR)/$(BINARY) apply all --yes
E2E_APPLY_FLAGS ?=
E2E_CLEAN ?= sudo rm -rf
# ADRs intentionally describe the abandoned shape in their Context
# sections; exclude /specs/adr from the stale-term sweep.
DEFINITION_CHECK_PATHS = README.md docs specs/state-model.md specs/architecture.md specs/domain.md specs/security.md specs/index.md specs/README.md test $(wildcard examples)

ANSIBLE_SRC_DIR = ansible
EMBED_BUNDLE_ARCHIVE = internal/converge/bundle/ansible_bundle.zip
BUNDLE_WORK_DIR = $(STATE_DIR)/ansible-bundle
ANSIBLE_GALAXY ?= $(shell command -v ansible-galaxy 2>/dev/null)
COLLECTIONS_REQUIREMENTS = $(ANSIBLE_SRC_DIR)/collections/requirements.yml
COLLECTIONS_LOCK = $(ANSIBLE_SRC_DIR)/collections/requirements.lock.yml
EMBED_COLLECTIONS_DIR = $(BUNDLE_WORK_DIR)/collections
EMBED_COLLECTIONS_ABS_DIR = $(abspath $(EMBED_COLLECTIONS_DIR))
COLLECTIONS_STAMP = $(EMBED_COLLECTIONS_DIR)/.stamp
ANSIBLE_GALAXY_ENV = \
	ANSIBLE_LOCAL_TEMP=/tmp/bootwright-ansible-local \
	ANSIBLE_REMOTE_TEMP=/tmp/bootwright-ansible-remote \
	ANSIBLE_COLLECTIONS_PATH=$(EMBED_COLLECTIONS_ABS_DIR) \
	ANSIBLE_COLLECTIONS_PATHS=$(EMBED_COLLECTIONS_ABS_DIR)
GOFMT_FILES = $(shell find . -name '*.go' -print)
ANSIBLE_ROLE_PATHS = ansible/roles/bastion:ansible/roles/shared:ansible/roles/providers:ansible/roles/cluster_infra:ansible/roles/openshift
ANSIBLE_SYNTAX_FILTER_PLUGINS = $(STATE_DIR)/ansible-syntax/filter_plugins
ANSIBLE_SYNTAX_ENV = ANSIBLE_LOCAL_TEMP=/tmp/bootwright-ansible-local ANSIBLE_REMOTE_TEMP=/tmp/bootwright-ansible-remote ANSIBLE_ROLES_PATH=$(ANSIBLE_ROLE_PATHS) ANSIBLE_COLLECTIONS_PATH=$(EMBED_COLLECTIONS_ABS_DIR) ANSIBLE_FILTER_PLUGINS=$(ANSIBLE_SYNTAX_FILTER_PLUGINS)
ANSIBLE_SYNTAX_PLAYBOOKS = \
	ansible/playbooks/checks/become.yml \
	ansible/playbooks/checks/preflight.yml \
	ansible/playbooks/targets/all/apply.yml \
	ansible/playbooks/targets/infra/apply.yml \
	ansible/playbooks/targets/infra/destroy-artifact-server.yml \
	ansible/playbooks/targets/infra/destroy.yml \
	ansible/playbooks/layers/openshift/create-agent-iso.yml \
	ansible/playbooks/layers/openshift/boot-agent-machine.yml \
	ansible/playbooks/layers/openshift/wait-agent-install.yml \
	ansible/playbooks/targets/clusters/apply.yml \
	ansible/playbooks/targets/clusters/destroy.yml \
	ansible/playbooks/targets/bastion/apply-clis.yml

E2E_CASES = $(notdir $(patsubst %/,%,$(wildcard $(E2E_DIR)/*/)))

.PHONY: all build container-build sync-bundle test validate plan check check-gofmt go-test-clean-checkout python-test ansible-syntax-check stale-term-check cli-file-size-check check-e2e-deps check-e2e-case list-e2e-cases e2e-dry-run e2e clean clean-e2e-state help

# Architecture guardrail: keep internal/cli files thin so domain logic stays
# in internal/converge/workflow/. The current observed max (init.go ~391) is the
# floor; do not raise this without a deliberate refactor justification.
# The intent is to catch new growth before files turn into god files again.
CLI_FILE_LINE_LIMIT ?= 400
WORKFLOW_FILE_LINE_LIMIT ?= 1000

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

build: $(BIN_DIR) sync-bundle
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/bootwright

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
	@touch $@

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

test:
	$(GO) test ./...

check: check-gofmt
	$(GO) vet ./...
	$(GO) test ./...
	$(GO) test -race ./...
	$(MAKE) go-test-clean-checkout
	$(MAKE) python-test
	$(MAKE) ansible-syntax-check
	$(MAKE) stale-term-check
	$(MAKE) cli-file-size-check

check-gofmt:
	@test -z "$$(gofmt -l $(GOFMT_FILES))" || { gofmt -l $(GOFMT_FILES); exit 1; }

go-test-clean-checkout:
	@tmp=$$(mktemp -d); \
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
		-cf "$$tmp/source.tar" .; \
	tar -xf "$$tmp/source.tar" -C "$$work"; \
	mkdir -p "$$tmp/go-build-cache" "$$tmp/go-tmp"; \
	cd "$$work"; \
	GOCACHE="$$tmp/go-build-cache" GOTMPDIR="$$tmp/go-tmp" $(GO) test ./...

# Filter-plugin unit tests use only stdlib unittest so the check works
# on any Python 3 install without a venv. If pytest is installed
# locally, `python3 -m pytest ansible/filter_plugins/` discovers the
# same TestCase classes.
python-test:
	@cd ansible/filter_plugins && python3 -m unittest discover -v
	@$(PYTHON) -m unittest discover -s scripts -p 'test_*.py' -v

ansible-syntax-check: check-e2e-deps $(COLLECTIONS_STAMP)
	@test -n "$(ANSIBLE_SYNTAX_FILTER_PLUGINS)" || { printf '%s\n' 'ANSIBLE_SYNTAX_FILTER_PLUGINS must not be empty'; exit 1; }
	@case "$(ANSIBLE_SYNTAX_FILTER_PLUGINS)" in */ansible-syntax/filter_plugins) ;; *) printf 'refusing to refresh unsafe ANSIBLE_SYNTAX_FILTER_PLUGINS: %s\n' "$(ANSIBLE_SYNTAX_FILTER_PLUGINS)"; exit 1;; esac
	@rm -rf $(ANSIBLE_SYNTAX_FILTER_PLUGINS)
	@mkdir -p $(ANSIBLE_SYNTAX_FILTER_PLUGINS)
	@find ansible/filter_plugins -maxdepth 1 -type f -name '*.py' ! -name 'test_*.py' -exec install -m 0644 {} $(ANSIBLE_SYNTAX_FILTER_PLUGINS)/ \;
	@for playbook in $(ANSIBLE_SYNTAX_PLAYBOOKS); do \
		$(ANSIBLE_SYNTAX_ENV) $(ANSIBLE_PLAYBOOK) --syntax-check -i localhost, "$$playbook"; \
	done

stale-term-check:
	@if command -v rg >/dev/null 2>&1; then \
		rg -n 'providerRefs|HostPool|spec\.machine\.libvirt|services\.bootArtifacts|services\.loadBalancer|services\.proxy|services\.registry|services\.nameResolution|MachineFlavorBareMetal|BuildClosure|input-files|/state/|/workflow/|runtime/[^/]+/installer' $(DEFINITION_CHECK_PATHS); \
		status=$$?; \
	else \
		grep -RInE 'providerRefs|HostPool|spec\.machine\.libvirt|services\.bootArtifacts|services\.loadBalancer|services\.proxy|services\.registry|services\.nameResolution|MachineFlavorBareMetal|BuildClosure|input-files|/state/|/workflow/|runtime/[^/]+/installer' $(DEFINITION_CHECK_PATHS); \
		status=$$?; \
	fi; \
	if [ "$$status" -eq 0 ]; then exit 1; fi; \
	if [ "$$status" -eq 1 ]; then exit 0; fi; \
	exit "$$status"

# Reject CLI / render files that have grown past the thin-handler
# threshold. Excludes test files so the lint targets production code
# only. Scope includes CLI and the largest domain packages that previously
# accumulated god-files.
cli-file-size-check:
	@over=$$(find internal/cli internal/render internal/state/scaffold internal/converge/bastion internal/runtime/secrets -maxdepth 1 -type f -name '*.go' ! -name '*_test.go' -printf '%p\n' \
		| while read -r f; do \
			n=$$(wc -l <"$$f"); \
			if [ "$$n" -gt $(CLI_FILE_LINE_LIMIT) ]; then printf '  %s lines\t%s\n' "$$n" "$$f"; fi; \
		done); \
	if [ -n "$$over" ]; then \
		printf '%s\n' "files over $(CLI_FILE_LINE_LIMIT) lines (decompose by concern; CLI handlers belong in internal/converge/workflow/, render emitters split by emission family):" "$$over"; \
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

validate: build
	HOME=$(TEST_HOME) $(BIN_DIR)/$(BINARY) context init validate -f test/e2e/001-sno-libvirt --yes
	HOME=$(TEST_HOME) $(BIN_DIR)/$(BINARY) check syntax

plan: build
	HOME=$(TEST_HOME) $(BIN_DIR)/$(BINARY) context init plan -f test/e2e/001-sno-libvirt --yes
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
	HOME=$(E2E_HOME) $(BIN_DIR)/$(BINARY) context init $(CASE) -f $(E2E_FIXTURE) --yes
	HOME=$(E2E_HOME) $(BIN_DIR)/$(BINARY) apply all --dry-run $(E2E_ANSIBLE_FLAGS) $(E2E_APPLY_FLAGS)

e2e: check-e2e-case check-e2e-deps build
	HOME=$(E2E_HOME) $(BIN_DIR)/$(BINARY) context init $(CASE) -f $(E2E_FIXTURE) --yes
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
		'  sync-bundle      Refresh internal/converge/bundle/ansible_bundle.zip' \
		'  check            Run formatting, Go, Ansible, stale-term, and provider-swap checks' \
		'  test             Run Go tests' \
		'  validate         Validate test/e2e/001-sno-libvirt' \
		'  plan             Render installer assets for test/e2e/001-sno-libvirt into .state' \
		'  list-e2e-cases   List available e2e cases under test/e2e' \
		'  check-e2e-deps   Check local e2e dependencies' \
		'  e2e-dry-run         Render an e2e fixture and print Ansible command (requires CASE=<name>)' \
		'  e2e                 Run an e2e fixture with sudo (requires CASE=<name>)' \
		'  clean               Remove workspace-local generated outputs' \
		'  clean-e2e-state     Remove generated e2e CLI state with sudo (requires CASE=<name>)'
