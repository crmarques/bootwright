.PHONY: build test vet fmt-check modules-check completion-test vulncheck ansible-check check

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

modules-check:
	$(GO) mod verify
	$(GO) -C scripts/tools mod verify

completion-test:
	$(GO) test -count=1 -tags=completion ./test/completion

vulncheck:
	./scripts/govulncheck -db "$(VULNDB)" ./...

ansible-check:
	./scripts/ansible-check

check: fmt-check modules-check test vet completion-test vulncheck ansible-check
	git diff --check
