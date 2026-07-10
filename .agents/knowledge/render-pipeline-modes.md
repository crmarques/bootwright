# Render pipeline: one core body, three modes, byte-stable outputs

**One shared body, four seams.** `renderCore` (`internal/render/render.go`) is
the single render body for every mode — on-disk context, tool-input context,
and portable `--input-dir`. The only per-mode differences are four seams in
`renderParams`: installer-assets layout, inventory emission, vars emission
(ownership records + path options vs a secrets-dir seam), and installer
secrets (placeholder refs, real material, or portable tokens). The
secret-independent artifacts — `effective-state.yaml` and the lock — are
produced by `renderCore` itself, so every entry point emits them
byte-identically. `TestRenderCoreSharedOutputsStayInStep` fails first if the
render bodies ever re-fork.

**`effective-state.yaml` is an identity function.** `EffectiveState`
(`internal/render/effective.go`) returns exactly the already-normalized,
defaults-applied state the renderer was handed — there is no renderer-level
overlay step. The function exists only to name the single point every write
path funnels the snapshot through, so any future effective-only transform has
one home.

**Unresolved names fail closed before any write.** `checkResolvedNames`
(`internal/render/unresolved.go`) is render's second enforcement line behind
`Validate`. `state/view` and `storage/topology` resolvers degrade to empty
strings on unresolvable names, and render consumers guard with
`if address != ""`; a validator gap (or rendering never-validated state) would
otherwise ship install-configs with empty VIPs, DNS records without addresses,
or a silently shrunken Ceph monitor list. Every render entry point calls it
before writing anything, so failure is a hard error naming each unresolved
name. `unresolvedNames` must stay in step with the guarded call sites:
whenever a new consumer reads a resolver that returns `""` on failure, add a
matching check. `stateview.MachineRouteAddress` is deliberately excluded — its
only render consumer, `proxy.ManagedProxyURL`, already returns a hard error.

**Omit keys, never emit empty/false.** Renderer convention: unaffected renders
must stay byte-identical, so vars maps omit a key rather than emit an
empty/false value (`rhsmSatelliteVars` returns nil for the public Red Hat CDN
so the `satellite` key is absent; a repo's `proxied` flag is omitted when the
host is bypassed; monitoring `enable_auth` renders only when authored). This
matters because install-marker hashes and golden fixtures derive from these
maps — an emitted empty key is a spurious diff and a spurious drift signal.
