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
and adapter-invented defaults are errors. A strict JSON document ends with its
value: anything after it but whitespace, a closing brace or bracket included,
is trailing data and refuses.

Every input boundary defines and tests fixed limits: bytes per file and
operation, directory depth and entry count, YAML depth and node count, object
and reference count, string and collection length, decoded payload size,
diagnostic count, log and subprocess
output, target concurrency, redirects, retries, and wall-clock duration. Limit
failures are explicit and deterministic; partial input is never accepted as
complete. Enforce limits before proportional allocation or work except for
the explicitly qualified desired-state
[per-document parser boundary](api/input.md#parser-boundary): verified source-byte
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

`validate` and `render effective` read no Secret material under the
[compiler boundary](api.md#compiler-boundary), and desired state names no
Secret payload path: the [file source](api/secrets.md#file-source) is retired,
so `secret set` is the only command that opens an operator's Secret file. A
context-backed invocation may read only the metadata
required to resolve the selected context and its immutable desired-state input
view; it reads no lifecycle or confidential state.

The [durable context boundary](contexts.md#storage-locking-and-publication)
owns publication, record bounds, locked reads and corruption refusal; its
manifest freezes authored input only, and source paths never authorize payload
access during admission or inspection.

Secret materialization requires an explicit context-scoped authority and must
not fall back to ambient credentials. Secret bytes stay in bounded memory, an
inherited descriptor or standard input, or an operation-scoped `0600` file
beneath a `0700` directory. The lifecycle runner writes each such file once
and then clears its copy of the value, so a run request whose material list
names one file twice, or binds one adapter variable to two files, refuses with
`lifecycle.state` before the runner creates its job directory or writes any
file. It writes each file at its name joined to the job directory, and the
adapter finds each file's path beside the request's material values in one
variable map, so a file named by anything but one segment of lowercase letters,
digits, `.`, `_` and `-` that does not begin with a dot, or by a name the
runner gives a job entry itself, and a material value named as a file's
variable refuse the same way. Cleanup is bounded and recorded without
recording the value or its digest. Retries use the same bound external version or confidential operation
snapshot. Generation uses an operating-system
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
[API discovery](api/input.md#discovery). Resource
selection cannot re-enter them. A selected regular file is opened relative to
a held, verified directory handle with no-follow semantics; type, device,
inode, link policy, size, and stability are verified on the opened handle.
Concurrent replacement or mutation makes the read fail. Security decisions
must not use a check-then-open path sequence.

All managed writes remain beneath an explicitly selected and verified root.
Components are validated single path segments; traversal, absolute
substitution, links, special files, alternate streams, and unexpected mount or
device crossings are rejected. Directories use mode `0700` and private files
use `0600`, with a restrictive creation mask; the one executable-file exception
is [Workspace's](contexts/controller-record.md#location-and-file-modes)
immutable controller bundle entry. Temporary and final files are
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
A child process therefore receives no proxy variable of its own; an
acquisition route reaches it as request data. An adapter that hands such a
route to a native tool through its environment sets the upper- and lower-case
spelling of each proxy variable to the same value, so a spelling the request
did not set cannot supply a route of its own. A privileged re-execution carries
only the [context-free route](controller.md#the-context-free-acquisition-route)
its invoker already admitted.

A runner hands Ansible every value as data, never as a template. ansible-core
loads an `--extra-vars @file` document as trusted template text, so each runner
writes every string of that document, the frozen request, its digest, material
paths and values and output paths alike, as an `__ansible_unsafe` object,
which ansible-core never renders. Mapping keys are never templated.
ansible-core decodes an object holding a key named exactly `__ansible_unsafe`,
`__ansible_vault` or `__ansible_type` as a typed value, and reads a document it
cannot decode as JSON again as YAML with every string trusted, so a runner
refuses a document holding such a key; any other key, one that only starts as
they do included, is ordinary. The frozen request bytes and every digest are
unchanged. A fresh plan and apply refuse a block whose request holds such a
key, so a request that plans is one its runner can encode, and, as defence in
depth, one whose request holds `{{`, `{%` or `{#`
([plan and execution](state-reconciliation.md#plan-and-execution)).

The adapter bounds runtime, input, output, process count, and inherited
resources. Cancellation terminates and reaps the whole owned process tree.
Standard output and error are untrusted and potentially sensitive: consume
them with bounded structured decoding, sanitize them before any presentation,
and do not treat prose or exit status alone as proof of effect, ownership, or
rollback.

A lifecycle adapter dies with its invocation, even an invocation killed
outright, as the elevated child is when its sudo parent dies. Its runner starts
it with a parent-death signal from an operating-system thread held until the
adapter is reaped, because Linux sends that signal when the creating thread
ends, not the process. The adapter's supervisor, a child subreaper, arms the
same signal for itself and then re-checks that its original parent still
lives, because a parent that died first sends none. On that signal it kills
every descendant, including an Ansible worker that moved into a session of its
own, then its process group and itself; its `ansible-playbook` child is killed
by the supervisor's death. Cancellation, a deadline and a protocol refusal end
the tree the same way: the runner sends the adapter that signal, then kills its
process group once the adapter is reaped or the 5-second drain passes,
whichever comes first. A group kill alone never reaches a worker in a session
of its own, and sent first it would end the supervisor before its handler ran.
A controller run arms no parent-death signal: an authorized native package
transaction runs to its end, and the adapter stops at its next acknowledgement,
which fails once the invocation's ends of the channels close.

Not yet met: a supervisor killed on its own, as by the out-of-memory killer,
runs no handler, so its `ansible-playbook` child dies with it but an Ansible
worker in a session of its own runs on; tracked as
[B23](milestones/m3.md#b23).

Each lifecycle adapter invocation owns a job directory `bootwright-run-<n>`
beneath `/run` and a scratch directory `bootwright-run-scratch-<n>-<m>` beneath
`/var/tmp` that carries its job's `<n>`. Before it writes anything else into
the job, the runner takes an exclusive advisory lock on its `lock` file, then
records beside it the adapter's implementation, operation, Machine and request
digest. The adapter inherits the lock, and so do the `ansible-playbook` child
its supervisor forks and every Ansible worker, so the lock is free only once
none of them runs, and the worker that a supervisor killed on its own leaves
running still holds it. A run refuses with `lifecycle.adapter-running`, naming
the held lock, while another job's lock is held, and starts nothing. An
invocation removes its own directories when it ends only if their lock is free;
otherwise they stay, with the material in them, for the processes that still
use them. Every run first removes each job whose lock is free, with its
scratch, and each scratch whose job is gone, as after a reboot empties `/run`.
Like the [abandoned media stage](contexts.md#media-acquisition), it considers
only entries with the runner's exact names that are private directories owned
by root, reaches each through its held parent, decides on the opened handle,
which must be the entry listed, follows no link, crosses no filesystem and
fails closed when it cannot remove one, or when a parent holds more run
directories or a tree more depth or entries than its bounds allow. A job's
record and lock go last, so a job it could not remove fails every later sweep
closed too, while a job without its record is still being created and is left
alone.

Not yet met: the run request carries neither its context nor its block, so a
job records the adapter's implementation, operation, Machine and request digest
in their place, and the refusal covers every context on the host rather than
the held job's own; tracked as
[B24](milestones/m1.md#b24).

The process that runs a Bootwright-owned operation cancels it on SIGHUP exactly
as on SIGINT and SIGTERM, so a closed terminal or a lost session interrupts the
operation instead of ending the process past its cleanup. A process started
with SIGHUP ignored, as `nohup` starts it, keeps ignoring it. The unprivileged
supervisor of a sudo invocation relays a hangup to the elevated child as it
relays SIGINT and SIGTERM.

Native-shaped authored maps are closed against the pinned supported native
schema before projection. Only their owning typed renderer may produce an
allowlisted argument or private native file. Generated scripts are
deterministic, inspectable artifacts with fixed command structure and safe
argument encoding; they do not use `eval`, inline secrets, or execute as part
of generation.

## Local context privilege

The [context store](contexts.md#storage-locking-and-publication) owns
root-only storage, and the [CLI boundary](cli.md#local-privilege-and-user-identity)
owns sudo authentication and credential refresh. No password enters Bootwright
memory, argv, environment, durable state or output. Per-user selection is
non-authoritative input under [context identity](contexts.md#identity-and-selection),
validated against the root registry and never written by root followed by
chown.

[Permanent context deletion](contexts.md#permanent-deletion) requires positive
disposal proof and an exact recorded deletion identity before any unlink;
partial removal never restores ordinary usability or weakens identity checks.
The one exception to disposal proof is the explicit orphan acknowledgement of
the [reconciliation guard](state-reconciliation.md#context-mutation-evidence):
it abandons what recognized evidence attributes to the context instead of
proving it gone, and waives nothing else.

## Network, remote systems, and privilege

Network access requires an application-authorized typed request with validated
scheme, endpoint, port, target identity, and bounded request and response
sizes. URLs containing `userinfo` are invalid. Redirects are disabled
unless the port contract allows them; every permitted hop is revalidated for
scheme, destination, credentials, and private-address policy. Credential
discovery is forbidden, and so is proxy discovery from configuration files,
user profiles or tool defaults. A route is always an explicit admitted value:
either the selected Machine's declared proxy choice, or, for the
[commands that acquire before any context exists](controller.md#the-context-free-acquisition-route),
the named variables the invoking environment set. That route is admitted
against the declared endpoint and bypass grammar before it is used, carries no
credential, and selects transport alone.

TLS certificate and name verification and SSH host identity verification fail
closed. An insecure exception must be explicit, endpoint-scoped, visible in
effective intent, and incapable of becoming a global default. DNS resolution
and every connection target are checked against the same authorization so
rebinding or alternate-address retries cannot expand scope.

The BMC-to-artifact-server virtual-media leg imports the server's certificate
by default. Its one insecure exception, `disable-verification`, is explicit,
declared on one Machine and visible in that Machine's effective state. It is
never inherited, never a provider default and never an Environment kind
default, and a Machine whose installation delivers private material refuses it
before registration; the one bounded exception is the emulated controller the
[container-cluster boot media](container-clusters.md#boot-media) records. A
controller's own transport verifies against its declared CA bundle alone, or
against the system trust store when it declares none
([Machine BMC trust](api/machines.md#bmc-and-root-device-shape)).

The watcher token the agent installer mints with a cluster's image crosses the
machine network in cleartext. The assisted service on the cluster's rendezvous
host answers plain HTTP, and the installer's own client of it, through which
`agent wait-for` watches the cluster, authenticates with that token. The
install block sends the same token once more, in the one bounded read of the
registered hosts after a stalled wait, which reaches only an address its frozen
request declares ([container clusters](container-clusters.md#installation)).
This is no credential discovery: the token is the installer's own, read from
the member of the installer's asset state that the installer reads it from, in
the installation's root-owned work area. It is never an argument, a result,
evidence, output or a log entry.

Managed probes and effects use the
[Ansible boundary and locked runtime closure](architecture.md#go-and-ansible-responsibility-boundary),
generated inventory/configuration, and pinned allowlisted `bootwright.core`
entrypoints. An SSH connection they open neither creates, joins nor outlives a
shared control connection, so no other process's authenticated channel carries
it. Only operation-required facts and the least authorized local and
remote privilege are available. No environment or adapter default grants
privilege escalation.

The application request fixes exact targets and authorization before adapter
execution. The adapter cannot widen targets, privileges, retries, or effects.
Live remote identity, host-key or certificate identity, ownership, power state,
and claimed absence are positively proved where required; probe failure is
unknown. Secrets reach Ansible only by the channels
[sensitive material](#sensitive-material) permits. Every task, result, and diff
that could carry one uses `no_log` and no-diff behavior and must not persist it
in inventory, facts, caches, evidence, logs, or adapter results.

### Direct SSH sessions

An [explicit SSH session](cli.md#machine-ssh-sessions) runs one pinned client
whose identity is verified before launch, never a client resolved through an
ambient path. Its server identity is proved first, from the one authorized
source the session resolved, and pinned to that exact key and algorithm, so an
unproved or substituted key ends the session before a credential is offered.
The client decides no trust and records none: a first-use record requires an
explicit interactive confirmation of the displayed fingerprint, and a changed
key is superseded only by an explicit re-trust. A host-key observation offers
no credential, proves nothing by itself, and is never the source of a record.

Session material — the private key, the pinned host key and the client
configuration — is passed as open descriptors the client inherits, never as
named paths, arguments or environment values, and is released when the session
ends. An operator-offered key file joins that material: it is opened without
following a link at its name, proved on the open handle to be a regular file
the invoking account owns and no other account can read, and handed to the
client as that same descriptor, so the file the client reads is the file that
was proved. The configuration is Bootwright's own: it carries only allowlisted
cryptographic directives from the host crypto-policy backend where that exists,
so site and FIPS policy is retained, and it admits no identity, certificate,
agent, command, forwarding or host rule from system or personal configuration.
The client receives only terminal-identifying environment values, so a
caller-selected askpass helper, agent, loader or crypto-provider override
cannot cross the process boundary. The session is a waited child whose streams
and exit status are the operator's; it publishes no operation, ownership,
evidence or retained output.

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
The one exception is [controller setup's](controller.md#selection-and-command-journeys)
resolver staging. It runs downloaded resolver code, each staging executable
verified before its own execution, in disposable unprivileged staging with no
installed-host, shared-state, Secret or target authority, and freezes exact
versions, sources, publisher digests, resolver identities and the complete
native transaction before presenting the plan; the installation phase uses only
that frozen result, and preflight, dry-run and recovery never refresh it. A
frozen operation never substitutes an update silently; recovery follows
[state reconciliation](state-reconciliation.md#dependency-safety-during-recovery).

## Logs, output, and diagnostics

Output and private logging follow [the CLI contract](cli.md). Logs use the
operation-owned `0700`/`0600` tree and contain only bounded normalized records.
Raw callback bytes, terminal control sequences, secret values or digests,
credential-bearing values, environment dumps, and any field protected by
`no_log` are forbidden. Attacker-controlled text is escaped; redaction is
structural and occurs before formatting or persistence. Truncation and dropped
records are explicit.

Retained adapter output is the one bounded exception to that rule. Each
attempt's and bounded run's [retained output](cli/output.md#private-operation-logs)
is raw, private, bounded and never presented, and nothing reads it back. Its
secrecy depends on `no_log`: every task that receives bound material sets it,
so the material never reaches the adapter's own streams. ansible-core censors
such a task's result but keeps the error, warnings and deprecations the task
raised, and its default output prints them whole, so an assertion's templated
message, a module error naming an argument, or a lookup or template error
naming a value would print what the task was given. The adapter therefore
prints through the collection's `bootwright.core.censored` callback, which
`ansible/ansible.cfg` names and beside which it enables no other: it prints
what the default prints, but nothing a result `no_log` censors raised, so a
hidden task that fails shows its name and that it failed. Because the
adapter's output keeps nothing of such a task's failure but that it failed, a
task that reaches a management controller never fails itself: it registers
its result, and the next step, before anything else acts, either refuses
outside `no_log` with the controller's own message, printable and bounded and
never with what the task was given, or publishes what it could not read as an
unproved observation. The refusal decides on that result alone and stops the play
whenever the controller refused: no condition, loop or tolerated failure of
its own lets the play go on to act on what was refused.

Required logs exist before effects or resolution observations, and a failure
to create, write, flush or finalize one is a durable fault under
[state reconciliation](state-reconciliation.md#converging-an-effect). Logs are
troubleshooting material, never authoritative evidence or a continuation
cursor. Verbosity cannot weaken redaction, `no_log`, permissions, bounds, or
stream separation.

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
shape whose admission and refusal that page owns. An exit status, source
revision, signature, or sandbox alone cannot prove the identity, ownership,
reversibility, or absence of exfiltration for arbitrary remote effects.
Executable custom automation therefore requires a separately user-authorized
typed schema with declared effects and ownership-aware inverse operations;
immutable source and dependency identity with bounded extraction or checkout;
an exact Machine inventory; bounded secret materialization; authorization;
failure and cancellation behavior; continuation; fail-closed durable evidence;
and a qualified, isolated, pinned runner. The broad `extraVars`, target, and
source fields grant none of that authority.

## Stateful mutation

[State reconciliation](state-reconciliation.md) owns immutable authorization,
leases, durable plans/evidence, exact identity and ownership, positive-absence
proof, live revalidation, unknown-outcome recovery and
[confirmation and authorization](state-reconciliation.md#confirmation-and-authorization).
Each effect defines idempotence/replay, retries, timeout, cancellation, safe
compensation where available, and success evidence before implementation.
Recovery evidence survives cleanup and partial failure. Confirmation and
irreversible authorization are separate typed decisions, and neither bypasses
validation, identity, ownership, bounds, logging, or live probes. No force
input exists.

## Required security proof

Security-sensitive changes require executable negative proof, not only
successful examples, and proof that prohibited effects do not occur. Each
invariant names the tests that guard it and, where proof is known to be
missing, the backlog item that adds it. Destructive and remote paths also
require qualified real-system tests, recorded in the
[acceptance ledger](../docs/acceptance.md), before production use.

| Invariant | Guarding tests | Gap |
| --- | --- | --- |
| Malformed, ambiguous, oversized, over-deep and high-cardinality input refuses at every declared bound. | `TestExpandedDepthCeilingIsInclusiveAndStopsLaterNormalizers`, `TestDiagnosticCeilingDeduplicatesAndStopsLaterValidators`, `TestFileReaderRejectsOversizeBeforeReading`, `TestAmbientRouteRefusesEveryAmbiguousOrUnqualifiedValue`, `TestTheSweepRefusesBeyondItsBounds`, `TestEveryStrictYAMLRuleRefusesWithItsCode`, `TestNoDecoderTakesMoreAsTheEndOfItsDocument`, `TestEvidenceFollowedByAClosingDelimiterIsRefused`, `TestThawRefusesAClosingDelimiterAfterTheRequest`, `TestDecodeEvidenceAdmitsOnlyOneBoundedValueOfItsShape` | none |
| Traversal, symlink, hard-link, special-file, concurrent-replacement, permission, atomic-publication and cleanup failures fail closed. | `TestDiscoveredYAMLSymlinkIsRejected`, `TestSecretSubtreeRefusesUnsafeEntriesBeforeCallback`, `TestSecretReplaceRejectsSameByteInodeSubstitution`, `TestPublicationLeavesNoStageOnFailure`, `TestAnInterruptedPublicationLeavesAUsableStoreAndItsRetryConverges`, `TestTheSweepNeverFollowsALinkOrTouchesWhatIsNotItsOwn`, `TestTheSweepNeverEmptiesAMount`, `TestAJobThatCannotBeRemovedKeepsFailingClosed`, `TestAnInvocationKeepsAJobWhoseLockWasReplaced`, `TestARefusedControllerPublicationLeavesNoStage`, `TestARenameTheFilesystemRefusesLeavesNoStage`, `TestTheNextLeaseCollectsWhatAKilledPublicationLeft`, `TestTheCollectorLeavesWhatItCannotProve`, `TestARefusedRegistryReplacementLeavesNoStage`, `TestARefusedSecretReplacementLeavesNoStage`, `TestTheNextRegistryTransactionCollectsRootRegistryStages`, `TestAnAreaRefusesARecordNamedAsAStage`, `TestARefusedLeaseIsReleasedAndNeverHeld`, `TestAnUnsafeOutputFailsTheRun`, `TestOutputsAreRefusedOffTheController`, `TestTheLifecycleSecretsAreaChecksLikeMutateSecrets`, `TestTheLifecycleSecretsAreaClosesWithTheTransaction` | none |
| Executable, argument, environment, working-directory, descriptor, inventory, plugin, endpoint, redirect, DNS, proxy and privilege substitution refuses. | `TestReexecutionPathPinsRunningExecutable`, `TestSupervisorRefusesAnAssignmentOutsideTheRouteVocabulary`, `TestInventoryPinsTheSSHIdentityAndHostKey`, `TestDownloadsFollowNoRedirectAndRefuseAnythingButOneServedImage`, `TestExplicitProxyIgnoresAmbientAndMatchesWithoutDNS`, `TestOnlyContextFreeAcquisitionConsumesTheInvokingEnvironment`, `TestOnlyContextFreeAcquisitionForwardsTheInvokingRoute`, and the collection's `test_every_command_starts_an_executable_the_allowlist_names`, `test_every_container_names_its_entrypoint_and_nothing_runs_a_debugger`, `test_the_token_goes_only_to_a_rendezvous_address_the_request_declares` and `test_an_answer_that_cannot_prove_which_node_is_missing_names_none` | none |
| An authored, operator or remote string reaches Ansible as data and is never rendered as a template. | `TestExtraVariablesMarkEveryStringAsData`, `TestExtraVariablesRefuseAKeyAnsibleCoreReserves`, `TestThePlanRefusesEveryKeyTheRunnersCannotEncode`, `TestTheAdapterReadsEveryRequestStringAsData`, `TestTheLifecycleRunnerWritesTheCollectionsFixture`, `TestTheLargestRequestStillFitsTheVariablesBound`, `TestTheControllerRunnerWritesEveryRequestStringAsData`, `TestTheControllerRunnerWritesTheCollectionsFixture`, `TestAPlanRefusesARequestHoldingATemplateDelimiter`, `TestTheControllerPrerequisitesBlockIsScannedLikeEveryOther`, `TestARequestWithoutATemplateDelimiterPlans`, `TestEveryProvenPathToARequestRefusesItsTemplateDelimiter`, and the collection's `test_a_runner_request_string_reaches_the_module_verbatim` and `test_the_same_strings_unmarked_are_rendered` | none |
| Invalid TLS and SSH identity, failed and ambiguous remote probes, target drift and unauthorized scope expansion refuse. | `TestCertificateValidationRejectsMismatchExpiryUsageAndFalseChain`, `TestADeclaredHostKeyForAnotherTargetRefuses`, `TestInstalledHostIdentityRefusesUntrustedOrAmbiguousEvidence`, `TestContinuationRefusesDriftedInputExecutableOrHost`, `TestEveryCapabilityHonoursTheCapabilityContract`, `TestOnlyADoneBareMetalApplyProofPinsAMachine`, `TestAProvedMachineCarriesItsIdentityToTheAdapter`, `TestAnUnreadableProofRefusesBeforeAnyPromptOrRun`, `TestTrustBundleRequiresVerification`, `TestAMachineBundleWithAnInheritedOptOutRefuses`, `TestAProviderBundleIsNotInheritedByAMachineThatOptsOut`, `TestProviderVirtualMediaDisableVerificationRefuses`, `TestAMachineKindDefaultCannotDefaultDisableVerification`, `TestDisableVerificationIsNeverAKindDefault`, `TestAKindDefaultTrustBundleSkipsAMachineThatOptsOut`, `TestAPrivateInstallationRefusesDisabledVerification`, `TestImportCertificateNeedsAnHttpsImageAndItsCertificate`, `TestTheClaimReadsTheControllerThroughItsBundle`, `TestPowerCarriesTheControllerBundle`, `TestEachReadTargetReadsItsOwnBundle`, `TestTheProbeVerifiesTheListenerAgainstTheBoundServingCertificate` | B73 |
| Secret and credential material never reaches output, diagnostics, verbose paths, logs, adapter events, retries, errors, cancellation or `no_log` handling. | `TestSecretNormalOutputsAndStateNeverContainMaterialOrDigests`, `TestMaterialNeverAppearsInMetadataOrErrors`, `TestVariablesCarryPathsNotMaterial`, `TestAcquisitionRequiresExactBoundedPublisherBytesAndRedactsFailures`, `TestProducedMaterialNeverReachesRecordsLogsOrAdapterOutput`, `TestSecretCommandsNeverListOrTouchProducedMaterial`, `TestTheInstallOffersItsKubeconfigOnlyOnProvedCompletion`, `TestManagedAndExternalStorageClustersAreNotApplicableAndReadNoCustody`, `TestAFailedRunReadsNoOutput`, `TestClusterKubeconfigWritesExactlyItsBytesOrOneDiagnostic`, `TestTheAdapterOutputPrintsNothingANoLogTaskRaised`, and the collection's `test_no_material_or_completion_reaches_the_adapters_own_output`, `test_the_censored_callback_prints_nothing_a_hidden_task_raised`, `test_no_management_controller_refusal_is_censored`, `test_every_controller_refusal_names_the_controllers_message_and_nothing_it_was_given`, `test_the_module_reports_the_registration_and_never_the_token` and `test_the_read_after_a_stall_runs_under_no_log_and_never_fails_the_wait_itself` | none |
| Time, retry, concurrency, memory, disk, log and process-output limits hold, including cancellation and process-tree reaping. | `TestAdapterOutputStreamsWhileItRunsAndBoundsWhatItKeeps`, `TestGuardedCommandDiesAndIsReapedAfterParentExit`, `TestAKilledInvocationTakesItsAdapter`, `TestStoppingAnAdapterEndsItsTreeBeyondItsGroup`, `TestThreadChurnNeverSignalsARunningAdapter`, `TestAnAdapterStillRunningRefusesTheNextRun`, `TestAStaleRunDirectoryIsRemovedWithItsSecretFiles`, `TestHangupCancelsTheOperationLikeTerminate`, `TestAnIgnoredHangupLeavesTheOperationRunning`, `TestHangupIsRelayedToThePrivilegedOperation`, `TestRunnerReapsUnauthorizedChildOnCancellationDuringRecovery`, `TestLifecycleConcurrencyBound`, `TestOperationBoundaryPreservesOrdinaryCancellationAndDeadline`, `TestABoundedRunCancelledInsideItsCallReleasesItsBinding`, `TestABoundedConsumerCancelledInsideItsCallReleasesItsBinding`, `TestABoundedConsumerCancelledWhileReopeningReleasesItsBinding`, and the collection's `test_the_read_is_bounded_in_time_however_slowly_the_answer_arrives` and `test_the_read_is_bounded_in_size` | B17, B23 |
| Dependency integrity, lock agreement and native-schema compatibility hold, and runtime-tool substitution or drift refuses. | `TestFrozenToolRejectsVersionRouteAndChecksumSubstitution`, `TestBootstrapRejectsSelfConsistentSourceAndVersionSubstitution`, `TestNativeSolveRefusesChangedBytesForSameRetainedRelease`, `TestRuntimeRequiresSelectedNativeCLIToRemainExecutable`, `TestEveryCapabilityHonoursTheCapabilityContract`, `TestThePowerRolesAdmitTheRequestsThisBuildSends`, `TestTheRoleAdmitsEveryRequestThisBuildSends`, `TestRoleVersionAssertionsMatchTheirArgumentSpecs` | none |
| Mutation crash points, lease conflict, replay, partial success, rollback, evidence loss and required-log write failure leave a recoverable context. | `TestCrashReleasesLocksAndLeavesCompleteSelection`, `TestLifecyclePublicationCheckpointsFireAndFailClosed`, `TestMutationGuardLayoutAndLeases`, `TestAPartlyRealizedBlockIsConvergedByRepeatingTheOperation`, `TestRequiredLogFaultStopsTheOperation`, `TestARestorationWhoseClearFailsStartsNothing`, `TestAJourneyKilledAtAnyWriteLeavesAUsableStoreAndConvergesOnRetry`, `TestAnUnknownDestroyBlockResolvesByWhatItsRemovalProves`, `TestAnInterruptedRemovalFinalizationIsCompletedByTheNextDestroy`, `TestARemovalWhoseBindingReleaseFailsReportsIncompleteFinalization`, `TestAnInterruptedApplyFinalizationIsCompletedByTheRepeatedApply`, `TestARunningOperationWhoseBlocksAreAllDoneIsFinalized`, `TestAFreshApplyOverAnUnfinalizedRemovalFinishesItFirst`, `TestNoFinalizationOverContradictedRecords`, `TestAReservationHeldBesideACompletedRemovalIsReleasedByTheNextDestroy`, `TestAFinalizationRefusesWhenTheContextChangedBeforeIt`, `TestEvidenceProtectsTheContextBeforeItBindsOrReserves`, `TestAFreshApplyClaimsItsOperationBeforeItBinds`, `TestAFailedEvidencePublicationBindsNothing`, `TestConcurrentFreshAppliesThatBothFailLeaveNothingRaised`, `TestABindingListingThatFailsBlocksNothing`, `TestARegistrationThatProvablyFailedRestoresTheEvidence`, `TestAFailedRegistrationWithReservationsKeepsItsEvidence`, `TestARegistrationThatMayHaveHappenedKeepsItsBinding`, `TestAContinuationProjectsBeforeItsFirstEffect`, `TestTheNextRegistrationReleasesBindingsNoOperationNames`, `TestADestroyReleasesWhatAnInterruptedRegistrationLeft`, `TestAnApplyWhoseBindingAnUnclaimedReleaseTookRefuses`, `TestAnApplyRefusesWhenAnotherClaimRaisedTheEvidenceAgain`, `TestAnApplyOverACompletedDestroyRefusesWhenAFinalizationTookItsBinding`, `TestADestroyOverUnindexedRecordsRefuses`, `TestARemovalRaisesItsEvidenceBeforeItRegisters`, `TestAnApplyInterruptedBeforeItRegistersGivesBackWhatItRaised`, `TestAFailedRestorationIsReportedBesideItsCause`, `TestADestroyReleaseRefusesWhenAnOperationRegisteredBeforeIt`, `TestARemovalWhosePublicationFailedCollectsNothing`, `TestAClaimedOperationRegistersIntoItsDirectory`, `TestAnUnknownOperationWhoseBlocksAreAllDoneIsFinalizedByItsOwnVerb`, `TestAFreshApplyWhoseClaimFailedRestoresTheEvidenceItsDirectoryMayHold`, `TestAnUnclaimedReleaseCollectsOnlyTheBindingsItReadFirst`, `TestAFinalizationCollectsOnlyTheBindingsItReadFirst`, `TestAnIncompleteApplyWithoutAContradictionIsStillRemovable`, `TestAnInitializationResumesOverATornStageAtEveryStagedWrite`, `TestAnInitializationRefusesAStageThatParsesButAttributesNothing`, `TestAContextInitRetryResumesOverATornKeyringStage`, `TestTheKillHarnessReachesTheCustodyPublications`, `TestAFailedPublicationLeavesTheAttemptUnknown`, `TestADestroyAfterAFailedPublicationCapturesBeforeAnyInverse`, `TestACancelledPublicationLeavesItUnknown`, `TestAStoppedDestroyKeepsTheAccess`, `TestAFinalizationWithdrawsWhatAnInterruptedRemovalLeft` | none |
| Read-only commands perform no writes, payload reads, processes, network access, secret lookup or generation. | `TestImmutableInputAndReadOnlyLifecycleBoundary`, `TestALifecycleInspectionRunsNothing`, `TestPlanPreviewsWithoutWritingAnything`, `TestStubServicesRemainStubs`, `TestReadsNeverCollectAStage` | none |
| Rejected operations perform no effect. | `TestCancellationBeforeAnyEffectRegistersNothing`, `TestDeclinedConfirmationRegistersNothing`, `TestUnsupportedObjectsRefuseBeforeRegistration`, `TestInvalidAdmissionAndUnavailableRoutesDoNotWrite`, `TestADestroyOverAnIncompleteApplyWithAContradictedBlockRefuses`, `TestARepeatedApplyOverACompletedApplyWithABlockNotDoneRefuses`, `TestATransitionRefusesWhenItsFrozenPlanChangedBeforeMutation`, `TestATransitionRefusesWhenTheOperationRecordChangedBeforeMutation`, `TestAContinuationRefusesWhenABlockWasRetriedBeforeMutation`, `TestAFrozenPlanThatIsNotItsOperationsRefuses`, `TestARepeatedMaterialRefusesBeforeAnyJobExists`, `TestAnUnsafeMaterialNameOrValueRefusesBeforeAnyJobExists`, `TestAFrozenPlacementOffTheControllerRefusesEveryVerb` | none |
| A failed security check cannot be bypassed by confirmation, retry or adapter behavior. | `TestMissingSafeguardsCannotBeReplacedByConfirmationFlags`, `TestOrphanAcknowledgementClaimsNothingAndReplacesNoOtherSafeguard`, `TestDestroyOverAnInterruptedApplyIsStillGated` | none |
