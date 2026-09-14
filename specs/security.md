# Security

Security is an invariant of every Bootwright boundary. Desired state declares
intent; it does not grant filesystem, process, network, remote-host, secret, or
privilege authority. Any ambiguous, malformed, unbounded, unverifiable, or
partially observed security decision fails closed and reports a safe typed
diagnostic.

## Trust model and input bounds

Treat authored YAML, native-shaped maps, payload files, paths, environment
variables, tool streams, remote facts, network responses, dependency content,
and persisted state not written and authenticated by Bootwright as untrusted.
Parse once into closed typed structures, normalize once, and pass only those
values across ports. Unknown fields, duplicate meanings, implicit coercions,
and adapter-invented defaults are errors.

Every input boundary defines and tests fixed limits: bytes per file and
operation, directory depth and entry count, YAML depth and node count, object
and reference count, string and collection length, decoded payload size,
diagnostic count, log and subprocess
output, target concurrency, redirects, retries, and wall-clock duration. Limit
failures are explicit and deterministic; partial input is never accepted as
complete. Enforce limits before proportional allocation or work except for
the explicitly qualified desired-state
[per-document parser boundary](api.md#parser-boundary): verified source-byte
limits precede parsing, while representation limits follow composition of one
document. Cancellation stops admission of new work and releases bounded
resources at the owning boundary's documented cancellation points.

Validation must distinguish absence, known value, and unknown result. A failed
or ambiguous probe is unknown, never proof that a machine, object, credential,
path, or effect is absent.

## Sensitive material

Passwords, tokens, pull secrets, private keys, kubeconfigs, provider and
management-controller credentials, credential-bearing URLs, generated secret
material, and outputs containing them are sensitive. The
[Secret schema](api/secrets.md#secret) owns source declarations and defaults;
desired state never carries secret values.

Secret values and secret-derived digests must never enter authored or effective
state, normal or verbose output, diagnostics, logs, command-line arguments,
environment variables, plan hashes, ownership or operation evidence, examples,
fixtures, crash reports, inventory, facts, or caches. Names, public keys, public
certificates, fingerprints, endpoints, image references and digests,
desired-state hashes, and ownership identifiers are non-secret only when they
embed no credential; private-estate identifiers still do not belong in public
examples.

`validate` and `render effective` validate only a secret source declaration and
deterministic generation parameters. They do not stat or open a referenced
secret file, contact a confidential store, or generate material. A
context-backed invocation may read only the metadata required to resolve the
selected context and its immutable desired-state input view; it reads no
lifecycle or confidential state. An in-tree file source uses the reserved
`secrets` path segment so API discovery never reads it as desired state.

The [durable context boundary](contexts.md#storage-locking-and-publication)
adds held-root publication, strict bounded records, immutable reads protected by shared root locks
and conservative corruption refusal. Its manifest freezes authored input only;
source paths never authorize payload access during admission or inspection.

Secret materialization requires an explicit context-scoped authority and must
not fall back to ambient credentials. Secret bytes stay in bounded memory, an
inherited descriptor or standard input, or an operation-scoped `0600` file
beneath a `0700` directory. Cleanup is bounded and recorded without recording
the value or its digest. Retries use the same bound external version or
confidential operation snapshot. Generation uses an operating-system
cryptographic random source and a maintained, reviewed construction appropriate
to the secret type; weak fallback randomness is forbidden.

## Filesystem and artifact safety

The [Secrets runtime contract](secrets.md) qualifies local key custody,
cryptography, rotation, material input and sensitive reveal.

Read-only commands create no cache, temporary file, state record, output path,
or log; [the output contract](cli/output.md#private-operation-logs) lists them.

API discovery rejects a symlink root and discovered YAML symlinks, never
descends through a symlink, and excludes the exact directory classes and
payload roots defined in
[the API contract](api.md#environment-directory-and-selected-state). Resource
selection cannot re-enter them. A selected regular file is opened relative to
a held, verified directory handle with no-follow semantics; type, device,
inode, link policy, size, and stability are verified on the opened handle.
Concurrent replacement or mutation makes the read fail. Security decisions
must not use a check-then-open path sequence.

All managed writes remain beneath an explicitly selected and verified root.
Components are validated single path segments; traversal, absolute
substitution, links, special files, alternate streams, and unexpected mount or
device crossings are rejected. Directories use mode `0700` and private files
use `0600`, with a restrictive creation mask. The only executable-file
exception is the root-owned `0700` immutable Controller bundle entry defined
by [Workspace](contexts.md#controller-relationship-and-host-binding); it grants
no group or other access. Temporary and final files are
created exclusively, written through held handles, bounded, flushed where
durability is required, and atomically published without overwriting unrelated
content. Parent directory durability is established when the owning state
contract requires it.

Failure paths close handles, remove only operation-owned temporary artifacts,
and preserve the durable evidence needed for safe diagnosis or continuation.
Cleanup must use recorded identities and verified roots, never a rediscovered
broad path or unresolved wildcard.

## Process boundary

A subprocess requires a pinned, verified executable and an exact allowlisted
argument vector.
Authored values never select the executable, form shell text, enable `eval`, or
become unreviewed native arguments. Invocation uses no shell, a fixed safe
working directory, a minimal allowlisted environment, and only required file
descriptors. Ambient `PATH`, user configuration, proxy variables, inventory,
plugins, roles, caches, SSH options, and privilege settings are not authority.

The adapter bounds runtime, input, output, process count, and inherited
resources. Cancellation terminates and reaps the whole owned process tree.
Standard output and error are untrusted and potentially sensitive: consume
them with bounded structured decoding, sanitize them before any presentation,
and do not treat prose or exit status alone as proof of effect, ownership, or
rollback.

Native-shaped authored maps are closed against the pinned supported native
schema before projection. Only their owning typed renderer may produce an
allowlisted argument or private native file. Generated scripts are
deterministic, inspectable artifacts with fixed command structure and safe
argument encoding; they do not use `eval`, inline secrets, or execute as part
of generation.

## Local context privilege

The [context store](contexts.md#storage-locking-and-publication) is always
root:root and private beneath `/var/lib/bootwright`. Local sudo authentication
and invocation-scoped credential refresh follow the
[CLI boundary](cli.md#local-privilege-and-user-identity). No password enters
Bootwright memory, argv, environment, durable state or output.

Per-user selection is non-authoritative input: validate its bounded name
against the root registry. Perform user-file effects with that
user's credentials, using no-follow handles, private modes and atomic
publication. Never use an untrusted home or a root write followed by chown.
Permanent context deletion requires positive disposal proof and an exact
recorded deletion identity before any unlink; partial removal never restores
ordinary usability or weakens identity checks.

## Network, remote systems, and privilege

Network access requires an application-authorized typed request with validated
scheme, endpoint, port, target identity, and bounded request and response
sizes. URLs containing `userinfo` are invalid. Redirects are disabled
unless the port contract allows them; every permitted hop is revalidated for
scheme, destination, credentials, and private-address policy. Ambient proxy
and credential discovery are forbidden.

TLS certificate and name verification and SSH host identity verification fail
closed. An insecure exception must be explicit, endpoint-scoped, visible in
effective intent, and incapable of becoming a global default. DNS resolution
and every connection target are checked against the same authorization so
rebinding or alternate-address retries cannot expand scope.

Managed probes and effects use the
[Ansible boundary and locked runtime closure](architecture.md#go-and-ansible-responsibility-boundary),
generated inventory/configuration, and pinned allowlisted `bootwright.core`
entrypoints. Only operation-required facts and the least authorized local and
remote privilege are available. No environment or adapter default grants
privilege escalation.

The application request fixes exact targets and authorization before adapter
execution. The adapter cannot widen targets, privileges, retries, or effects.
Live remote identity, host-key or certificate identity, ownership, power state,
and claimed absence are positively proved where required; probe failure is
unknown. Secrets reach Ansible only through bounded memory, standard input, a
descriptor, or a restrictive operation file. Every task, result, and diff that
could carry one uses `no_log` and no-diff behavior and must not persist it in
inventory, facts, caches, evidence, logs, or adapter results.

## Cryptography and supply chain

Use maintained standard cryptographic libraries and constructions; do not
invent algorithms, protocols, encodings, or random generators. Each
cryptographic capability defines algorithm, key type and size, randomness,
storage, rotation, expiry, revocation, and compatibility policy.
Policy-required validated modules must be selected explicitly and tested;
silent fallback is forbidden.

Use the [dependency selection rule](architecture.md#dependency-selection-and-reuse).
Release packaging must satisfy the license and notice obligations of shipped
artifacts before distribution.

Pin Go modules, `ansible-core`, collections, controller Python packages and
SDKs, execution-environment images, playbooks, add-on packages and catalog
snapshots, executables, and downloaded artifacts by an immutable version and
content digest. Verify integrity and required publisher authenticity from
separately trusted metadata before use. Repository-owned automation is
content-digested as part of the selected implementation. Implicit upgrade,
floating tags during execution, and ambient external tools are forbidden.
Explicit bastion setup without a serving retained resolution resolves latest or
declared dependency versions before confirmation. It may acquire verified
public resolver payloads and execute a
maintained resolver within disposable, unprivileged staging. This exception
permits only bounded scratch writes and explicit publisher/repository access;
it grants no installed-host package, shared-state, Secret or target authority.
Root invocation must establish an unprivileged, isolated execution boundary
before downloaded resolver code runs. Freeze exact versions, source identities,
publisher digests, resolver identities and the complete native transaction
before presenting the installation plan. Each staging executable is verified
before its own execution; the installation phase uses only the frozen result.
Preflight, dry-run and pending-operation recovery never refresh this metadata.
A frozen operation never substitutes an update silently; recovery
follows [state reconciliation](state-reconciliation.md#dependency-safety-during-recovery).

## Logs, output, and diagnostics

Output and private logging follow [the CLI contract](cli.md). Logs use the
operation-owned `0700`/`0600` tree and contain only bounded normalized records.
Raw callback bytes, terminal control sequences, secret values or digests,
credential-bearing values, environment dumps, and any field protected by
`no_log` are forbidden. Attacker-controlled text is escaped; redaction is
structural and occurs before formatting or persistence. Truncation and dropped
records are explicit.

Required logs exist before effects or resolution observations. Creation, write,
flush, and finalization failures follow the durable fault and unknown-outcome
rules in [state reconciliation](state-reconciliation.md#plan-and-execution):
stop new effects and observations, request cancellation of active work, and
preserve the log fault until safe restoration, even after positive effect
resolution. Logs are troubleshooting material, never authoritative evidence or a
continuation cursor. Verbosity cannot weaken redaction, `no_log`, permissions,
bounds, or stream separation.

Diagnostic codes and locations may identify a failed field, object, safe
source, target identity, or private log reference. Messages must not
echo untrusted payloads or sensitive values merely to explain a failure. An
internal error exposes a correlation identity and safe summary, not a stack,
memory dump, request body, or provider response.

## Custom code

[Add-on packages](add-ons.md) are declarative data consumed by qualified
Bootwright-owned drivers. Built-in and custom origins have the same integrity,
isolation, lifecycle, and negative-proof requirements. Neither grants process,
adapter, target, effect, inventory, endpoint, dependency, credential, or
privilege authority. Package-carried executable content is never loaded or run;
a driver may enter only its pinned embedded adapter and locked dependency
closure under [architecture](architecture.md#go-and-ansible-responsibility-boundary).

[`CustomPlaybook`](api/custom-playbooks.md) is a reserved, non-executable
shape. Read-only commands strictly decode, validate, default, and render its
declaration without reading a local source, fetching Git content, resolving
credentials, or inspecting playbook bytes. An enabled object must be rejected
before external content access, operation registration, or effects;
`enabled: false` produces no work.

An exit status, source revision, signature, or sandbox alone cannot prove the
identity, ownership, reversibility, or absence of exfiltration for arbitrary
remote effects. Executable custom automation requires a separately
user-authorized typed schema with declared effects and inverse operations,
immutable source and dependency identity, exact targets, bounded secret
inputs, fail-closed evidence, and a qualified isolated runner. The broad
`extraVars`, target, and source fields grant none of that authority.

## Stateful mutation

[State reconciliation](state-reconciliation.md) owns immutable authorization,
leases, durable plans/evidence, exact identity and ownership, positive-absence
proof, live revalidation, and unknown-outcome recovery. Each effect defines
idempotence/replay, retries, timeout, cancellation, safe compensation where
available, and success evidence before implementation. Recovery evidence
survives cleanup and partial failure.

Confirmation and irreversible authorization are separate typed decisions.
Neither bypasses validation, identity, ownership, bounds, logging, or live
probes. No force input exists. An unknown effect cannot produce success,
non-idempotent replay, or dependent work.

## Required security proof

Security-sensitive changes require executable negative proof, not only
successful examples. Tests must cover:

- malformed, ambiguous, oversized, over-deep, and high-cardinality inputs at
  every declared bound;
- traversal, symlink, hard-link, special-file, concurrent-replacement,
  permission, atomic-publication, and cleanup failures;
- executable, argument, environment, working-directory, descriptor, inventory,
  plugin, endpoint, redirect, DNS, proxy, and privilege substitution attempts;
- invalid TLS and SSH identity, failed and ambiguous remote probes, target
  drift, and unauthorized scope expansion;
- secret and credential leakage through human and JSON output, diagnostics,
  verbose paths, logs, adapter events, retries, errors, cancellation, and
  `no_log` handling;
- time, retry, concurrency, memory, disk, log, and process-output limits,
  including cancellation and process-tree reaping;
- dependency integrity, lock agreement, native-schema compatibility, and
  refusal of runtime-tool substitution or drift; and
- mutation crash points, lease conflict, replay, partial success, rollback,
  evidence loss, and required-log write failure.

Tests must also prove prohibited effects do not occur: read-only commands
perform no writes, payload reads, processes, network access, secret lookup, or
generation; rejected operations perform no effect; and a failed security check
cannot be bypassed by verbosity, confirmation, retry, or adapter behavior.
Destructive and remote paths require qualified real-system tests in addition
to hermetic port tests before production use.
