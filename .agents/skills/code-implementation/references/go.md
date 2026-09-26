# Go Engineering

[Architecture](../../../../specs/architecture.md) owns package and runtime
boundaries; [security](../../../../specs/security.md) owns trust requirements;
[source formatting](formatting.md) owns layout. This reference adds Go-specific
implementation and verification guidance.

## Implementation

- Place the smallest required port in its consuming package and bind adapters
  at composition. Apply this to every service, repository, resolver, and effect
  dependency, including same-package components. Use a typed function capability
  for a single operation when suitable. Keep immutable values and private
  implementation details concrete; do not create interfaces only for test doubles.
  Ensure alternate implementations can construct the port's immutable results
  without calling the default implementation.
- At immutable boundaries, copy reachable mutable state and avoid exposing
  aliases. Make nil-versus-empty serialization deliberate; sort map,
  filesystem, and concurrent results before a deterministic boundary.
- Classify errors with `errors.Is`/`errors.As` and use typed diagnostics for authored
  input. Wrap with `%w` only when cause identity belongs to the contract; otherwise
  normalize it. Do not panic on user input or emit terminal output in domain
  packages. Document any intentionally ignored error.
- Accept `context.Context` first at cancellable boundaries, propagate the
  caller's context, and release derived contexts. Give each goroutine an owner,
  cancellation/exit path, and join; prefer synchronous APIs. A stored context
  needs a documented lifetime or compatibility reason.
- The `go` directive sets minimum Go/language versions and `toolchain` is a
  suggestion, not an exact pin. Pin toolchain selection in build/CI or an
  explicit `GOTOOLCHAIN` policy; do not change directives incidentally.
- For rooted paths, use a traversal-resistant API in the pinned Go version.
  `filepath.Clean`, `filepath.EvalSymlinks`, and resolve-then-open do not prevent TOCTOU.
- Use `exec.CommandContext` or an injected equivalent for authorized processes.
  Refuse `exec.ErrDot`; use the selected executable identity without ambient `PATH`
  fallback and define cancellation, bounded capture, termination, and reaping.
- Use explicitly configured, owner-scoped `http.Client`/`http.Transport` instances.
  Apply the contract's proxy, redirect, credentials, TLS, timeout, and byte
  limits; close response bodies and owner-held idle connections. Do not mutate
  a transport after use. Bound parser allocations and use `crypto/rand` for
  authorized secret generation.

## Tests and gates

Test at the lowest stable owning boundary. Exercise the command runner when
arguments, streams, diagnostics, exit codes, cancellation, or effects change.
Use isolated files/environment and injected dependencies; unit tests must not
depend on ambient machines, networks, tools, clocks, or configuration. Run the
same port contract suite against every production adapter. Use varied input
ordering for determinism/non-mutation and bounded fuzz/property tests for broad
byte grammars; retain minimized failures.

Pin a format's exact bytes (a frozen request, a persisted record, published
evidence) with a golden in its own package: `testdata/<name>.golden` holds the
canonical JSON indented by `json.Indent`, which is lossless, and a helper in
the package's `golden_test.go` compares it byte for byte, reports a line diff
and the first differing byte, and rewrites it under the package's `-update`
flag; no shared golden package exists. Between them, a format's goldens
populate every field a supported declaration can reach, because no golden
catches a change to an `omitempty` key it omits. Put `Golden` in the test's
name and regenerate one package at a time with
`./scripts/go test ./internal/<package> -run Golden -update`, then review the
golden diff as code. A format's golden lands before or with its first change;
`internal/containercluster/agentinstall/golden_test.go` is the model.

Use pinned repository entrypoints, always `./scripts/go` and never a bare `go`;
never install an unreviewed latest tool to make a gate available. Run
`make quick` (formatting, vet, the architecture suite and the changed packages
with their dependents) and the affected repository checks. Run `make race` when
changed paths may execute concurrently or affect shared state, cancellation, or
goroutine lifetime; CI runs it nightly.

For module/import/toolchain changes, run `./scripts/go mod tidy`, then
`make tidy-check modules-check`, and review the complete `go.mod`/`go.sum` and
selected module graph diff. Run `make vulncheck`; it establishes only the absence
of known reachable findings. Review official release notes when pins change.
Report failed, skipped, flaky, or unavailable gates under the shared workflow.
