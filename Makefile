.PHONY: build test vet fmt-check modules-check tidy-check completion-test vulncheck ansible-check docs-check quick race check-offline check

GO := ./scripts/go
VULNDB ?= https://vuln.go.dev

# A release build overrides these; an ordinary build identifies the working tree
# it came from. Outside a repository they stay empty and `version` reports an
# unidentified build instead of claiming a revision it cannot prove. Every one
# is expanded by the build recipe alone, so no other target runs Git.
VERSION ?= $(shell git describe --tags --dirty 2>/dev/null)
COMMIT ?= $(shell git rev-parse HEAD 2>/dev/null)
SOURCE ?= $(if $(COMMIT),$(if $(shell git status --porcelain --untracked-files=no 2>/dev/null),modified,clean))
STAMP = -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.source=$(SOURCE)

build:
	$(GO) build -trimpath -buildvcs=false -ldflags '$(STAMP)' -o bin/bootwright ./cmd/bootwright

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

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

docs-check:
	$(GO) test -count=1 -run '^TestDocs' ./test/architecture ./internal/cli

# The inner-loop tier: formatting, vet, the architecture suite and the packages
# this branch changed together with their dependents.
quick: fmt-check vet
	./scripts/quick-test

race:
	$(GO) test -race ./internal/reconciliation/... ./internal/controller/privilege/... ./cmd/bootwright/...

# Every gate that needs no network once module caches are warm. Vulnerability
# data and the Ansible tool bootstrap need network, so they are reported unrun.
check-offline: fmt-check test vet completion-test
	@echo 'check-offline: vulncheck, ansible-check and modules-check were not run; they are unrun, not passed.'

check: fmt-check modules-check tidy-check test vet completion-test vulncheck ansible-check
	git diff --check
