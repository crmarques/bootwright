.PHONY: build test vet fmt-check modules-check completion-test vulncheck check

GO := ./scripts/go
VULNDB ?= https://vuln.go.dev

build:
	$(GO) build -trimpath -buildvcs=false -o bin/bootwright ./cmd/bootwright

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

check: fmt-check modules-check test vet completion-test vulncheck
	git diff --check
