# Security

Security is an invariant of every Bootwright boundary. Desired state declares
intent; it does not grant filesystem, process, network, remote-host, secret, or
privilege authority. Any ambiguous, malformed, unbounded, unverifiable, or
partially observed security decision fails closed and reports a safe typed
diagnostic.

## Trust model and input bounds

Treat authored YAML, native-shaped maps, payload files, paths, environment
variables, tool streams, remote facts, network responses, dependency content,
and persisted state Bootwright did not write as untrusted. Bootwright's own
state is trusted when it was published in the root-only store through held
verified handles, under the
[controller host's attacker model](#the-controller-hosts-attacker-model), and
keyring state also when its MAC verifies ([local keyring](secrets.md#local-keyring-v4)).
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
complete. The sessions of several Machines that `machine exec` and `cluster exec`
run are bounded by the target-concurrency limit of
[machine SSH sessions](cli.md#machine-ssh-sessions); no boundary runs
unbounded fan-out. Enforce limits before proportional allocation or work except for
the explicitly qualified desired-state
[per-document parser boundary](api/input.md#parser-boundary): verified source-byte
limits precede parsing, while representation limits follow composition of one
document. Cancellation stops admission of new work and releases bounded
resources at the owning boundary's documented cancellation points.

Validation must distinguish absence, known value, and unknown result. A failed
or ambiguous probe is unknown, never proof that a machine, object, credential,
path, or effect is absent. The one narrowing is a removal whose capability
declares it replay-safe: a successful read that cannot tell its postcondition
from an unreadable target is recorded `inconclusive`, never `no-effect`, and
admits one retry that reads the target again before any effect
([resolution outcomes](state-reconciliation.md#resolution-outcomes)).

### The controller host's attacker model

On the controller host the attacker is an unprivileged local account, and any
network adversary between the host and the publishers it acquires from. Root,
and anything only root can write, is trusted; the operator's sudo rule is
treated as granting everything it permits. Every account it admits may act on
every context of the host: Bootwright keeps no authority between those
accounts. Every file the controller executes
or loads, the re-executed Bootwright binary, the interpreter, its standard
library and `lib-dynload`, the controller runtime, `rpm` and `dnf` included, is
therefore admitted only when it and every directory above it are owned by root
(or, for the binary and the invoking account's own build output, by that
account) and writable by neither group nor others. A defence against root, such
as byte digests of root-owned system libraries, is no boundary here and is not
kept ([D152](milestones/backlog.md#d152)).

Controller setup v2 widens the trusted set in four named ways, each stated
here once:

- **W1, the OS interpreter.** The controller runtime runs on the host's own
  CPython at its versioned path `/usr/bin/python3.X`, which the root `dnf`
  transaction already trusts; no separately published interpreter is trusted.
- **W2, the host's repositories.** Host packages come from every repository
  and signing key the operator configured in dnf, copr included, as any
  `dnf install` does; Bootwright records and shows the repository each
  provider came from.
- **W3, a root `dnf install --assumeno` before confirmation.** The preview
  changes no package, but it may refresh dnf's metadata cache, as
  `dnf-makecache.timer` does.
- **W4, the system OpenSSL and crypto-policy.** The runtime's Python TLS
  follows the host's crypto-policy and FIPS mode. Bootwright's Go binary and
  cryptography's bundled OpenSSL, which Bootwright never uses for TLS, stay
  outside the host's FIPS-validated modules
  ([D108](milestones/backlog.md#d108) as D152 amends it); a
  FIPS-qualified runtime stays parked as [B331](milestones/backlog.md#b331).

### Remote principals

Each remote principal is reached only over the verified channel
[network, remote systems, and privilege](#network-remote-systems-and-privilege)
requires, and is trusted only for what it asserts about its own resources. Its
report of identity, readiness or positive absence proves those resources and
nothing about another principal's. Its compromise is outside what Bootwright
defends: at most it misreports or strands its own resources and discloses the
material lent to it.

| Principal | Trusted to assert | Receives |
| --- | --- | --- |
| Managed host, as its pinned host key | Its installed identity and the objects it holds | The fleet SSH identity and the material its blocks lend or place |
| Management controller | Its system's identity, power and virtual-media state | Its credential and the private virtual-media URLs it fetches |
| Provider host or API | The objects it hosts | Its access credential and the images pushed to it |
| Cluster API server | Its own objects' state and readiness | The administrator kubeconfig's access and every Secret placed in it |
| Registry, mirror or publisher | Nothing beyond bytes a digest or publisher metadata verifies | Its registry credential |

Native tool output is no principal: it is untrusted input decoded under the
[process boundary](#process-boundary).

## Sensitive material

Passwords, tokens, pull secrets, private keys, kubeconfigs, provider and
management-controller credentials, credential-bearing URLs, generated secret
material, and outputs containing them are sensitive. A URL whose query carries
a signature is a credential (D183, Q99): `media add` takes one through
`--from-url-file <path|->` and refuses a `--from-url` carrying any query
([flag safeguards](cli/commands.md#flag-relationships-and-safeguards)),
records only its scheme, host and path, and names it in no output. The
[Secret schema](api/secrets.md#secret) owns source declarations and defaults;
desired state never carries secret values.

Secret values and secret-derived digests must never enter authored or effective
state, normal or verbose output, diagnostics, logs, command-line arguments,
environment variables, plan hashes, ownership or operation evidence, examples,
fixtures, crash reports, inventory, facts, or caches. Names, public keys, public
certificates, fingerprints, endpoints, image references and digests,
desired-state hashes, ownership identifiers and a `usernamePassword` Secret's
username, which is identity metadata (D183, Q98), are non-secret only when they
embed no credential; private-estate identifiers still do not belong in public
examples. The prohibition covers Bootwright-owned sinks. A management
controller necessarily receives a private virtual-media URL to fetch an image
and may retain it or log it in its own access records; treat those records as
confidential too. An emulated BMC has the same behavior.

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

Secret materialization takes the selected context's keyring as its only
authority, which is no access control between the accounts the sudo rule
admits, and must not fall back to ambient credentials. Secret bytes stay in
bounded memory, an inherited descriptor or standard input, or an
operation-scoped `0600` file beneath a `0700` directory.

A lifecycle adapter receives each secret part only as a lent file. The
material values its request carries beside those files are non-secret, such as
names, public keys, certificates and fingerprints. A task reads a lent file
only inside the module argument or template that consumes it, under `no_log`,
and no fact or registered result keeps its bytes. A task placed on a remote
host that receives material runs with pipelining, so the module payload that
carries it crosses the connection's standard input and no remote temporary
file; a file transfer to a remote host carries no material.

The adapter configuration enables pipelining
(`TestRemotePlacedTasksRunWithPipelining`), and the runner refuses a material
value equal to, or containing, a confidential part of the material it lends
(`TestNoMaterialValueEqualsBoundMaterialBytes`). A role writes a file that
carries lent material with `bootwright.core.private_file`, whose argument holds
the bytes and which stages them owner-only beside the destination, sets their
mode and renames them over it, refusing a link there, removing its own stage on
failure and the stages of the same destination an interrupted run left; it
never runs `async`. The adapter configuration pipelines the controller-local
connection too. No task of a role's tasks or handlers, or of a playbook, that
transfers a file (`copy`, `unarchive` or `assemble` without `remote_src`,
`template`, `script`, `synchronize`, under any of their names) reads lent
material, through a file, template, unvault or pipe lookup or the bytes a slurp
of a lent file registered, directly or through a role variable, or names a lent
file as its source, and a template named by a variable that resolves to no
checked role template is refused (`test_no_file_transfer_carries_lent_material`,
`test_no_playbook_transfer_carries_lent_material`).

A managed-OS registration's organization and
activation key are read from their lent material files by the controller
module that opens the guest connection; they cross that pinned SSH session
only on the standard input of a fixed root worker and reach the subscription
service only through the RHSM D-Bus API's private registration bus. They never
enter an Ansible variable, an argument, an environment variable, evidence, a
result or a log, and a value that is not one line of printable ASCII without
whitespace is refused without being repeated. The lifecycle runner writes each such file once
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

A command whose [catalog](cli/commands.md#command-and-flag-catalog) Effects
cell reads `none` or `read local state` creates no cache, temporary file, state
record, output path, or log; [the output contract](cli/output.md#private-operation-logs)
lists them.

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
use `0600`, with a restrictive creation mask, with two exceptions. A tool
area's published executables are
[Workspace's](contexts/controller-record.md#location-and-file-modes)
executable-file exception. A
[private consumer publication](infrastructure-services.md#private-consumer-publication)
serves `0640` `root:root` files beneath `0711` directories, because the
artifact server's workers read them as group `root`; its confidentiality
assumes no interactive account holds group `root`. Temporary and final files are
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

A remote fixed worker, which a module starts over an SSH session of its own,
is a named exception to the no-shell rule: the protocol carries its command as
one string, which the remote account's shell interprets. It is admitted only
in one shape: an absolute remote interpreter run through `sudo -n`, code
shipped with the selected automation and never authored, data only on the
worker's standard input, every token of the command string a literal or a
closed validated token joined by one shared quoting function, and worker output
read only through its bounded protocol. That function is the collection's
`module_utils/remote_command.py`; `test_python_sites.py` refuses shell quoting
or a `shell=` argument anywhere else under `plugins/`, and lists every file
that names the SSH client and every file that hands a session a remote command.

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
own, then its process group and itself. Its `ansible-playbook` child is also
an orphan-reaping subreaper: before Ansible loads, it installs a dedicated
parent-death handler, arms `SIGUSR2` and re-checks the supervisor's identity.
When the supervisor is killed outright, the surviving child kills and reaps
its own descendants, including session-isolated workers, then ends its group.
The dedicated signal is separate from Ansible's `SIGTERM`/`SIGINT` handlers. Cancellation, a deadline and a protocol refusal end
the tree the same way: the runner sends the adapter that signal, then kills its
process group once the adapter is reaped or the 5-second drain passes,
whichever comes first. A group kill alone never reaches a worker in a session
of its own, and sent first it would end the supervisor before its handler ran.
A controller run arms no parent-death signal, so its tree outlives its
invocation, but its supervisor carries the same termination handler.
Cancellation or a deadline before a native package transaction is authorized
ends the tree the lifecycle way, with the 60-second drain. Once the
transaction's acknowledgement is delivered, nothing is signaled or killed: it
runs to its end, and the adapter stops at its next acknowledgement, which fails
once the invocation's ends of the channels close.

Each lifecycle adapter invocation owns a job directory `bootwright-run-<n>`
beneath `/run`, which holds its lent material and never survives a reboot, and
a scratch directory `bootwright-scratch-<n>-<m>` that carries its job's
`<n>` beneath `/var/lib/bootwright-staging/runs`, where no other account can
create a name. Its maximum path length leaves room for Ansible's local RPC
Unix socket. Earlier `bootwright-run-scratch-<n>-<m>` names under that same
parent retain the same job pairing and cleanup checks. The runner on the
controller and `lifecycle_scratch` on an SSH
execution host create that parent, and the staging parent above it, when
absent. A parent that is a link, owned by another account, or open to another
account's write, or a scratch parent other than mode 0700 or on a noexec
filesystem, refuses before any scratch is touched, naming the directory and
its remedy: the runner with `lifecycle.state` before any job exists or the
adapter runs, the module by failing its task
(`TestAnUnsafeOrNoexecScratchParentRefusesBeforeTheRun`,
`TestTheRunnerScratchesBeneathTheStagingParent`). Scratch directories an
earlier build left beneath `/var/tmp` are no longer swept; the host's
systemd-tmpfiles ages them out of `/var/tmp`, 30 days on Fedora and RHEL. Before it writes anything else into
the job, the runner takes an exclusive advisory lock on its `lock` file, then
one on its `holder` file, which only the invocation holds: it is opened
close-on-exec and never handed to the adapter. It then records beside them the
context the run is for, its block and description, and the adapter's
implementation, operation, Machine and request digest. A run that names no
valid context, or whose record a sweep could not read back, refuses before any
job exists. The adapter inherits the lock, and so do the `ansible-playbook`
child its supervisor forks and every Ansible worker, so the lock is free only
once none of them runs; a worker still being stopped keeps holding it. A run refuses with `lifecycle.adapter-running`,
and starts nothing, only while a job of its own context holds its lock, except
for its own live sibling adapters. That exemption requires the private
[invocation scope](state-reconciliation.md#stages-and-the-pause-boundary)
to prove the exact original lock and holder file identities; context,
operation and attribution bytes alone never authorize it. A released job
whose descendants still hold the lock loses the exemption. The runs of one
[power batch](cli.md#machine-power-operations) share one such scope and are each
other's live siblings. While
the invocation that started that job still holds its holder, the refusal names
the job's context, description, operation and Machine and says to wait for it
to end; only once the holder is free, so that what holds the lock is what the
invocation left behind, does it name the lock and advise ending the processes
that hold it. A held job of another context neither refuses nor goes, so
bounded runs of two contexts proceed together. A held job whose record is
unreadable or names no valid context, or that has no holder, refuses a run of
every context, naming the held lock, until it ends: that is what a job a build
before this rule started looks like across an upgrade
(`TestTwoContextsRunsProceedTogether`,
`TestASameContextRunRefusesNamingTheHeldJob`,
`TestAJobAnEarlierBuildStartedRefusesEveryContext`). An invocation removes its
own directories when it ends only if their lock is free; otherwise they stay,
with the material in them, for the processes that still use them. Every run
first removes each job on the host whose lock is free, whatever its context,
with its scratch, and each scratch whose job is gone, as after a reboot empties
`/run` (`TestTheSweepRemovesAnotherContextsUnheldJob`). Like the
[abandoned media stage](contexts.md#media-acquisition), it considers only
entries with the runner's exact names that are private directories owned by
root, reaches each through its held parent, decides on the opened handle,
which must be the entry listed, follows no link, crosses no filesystem and
fails closed when it cannot remove one, or when a parent holds more run
directories of its own or a tree more depth or entries than its bounds allow,
and reads no more than 4 KiB of a held job's record. An entry another account
owns, or that is open to other accounts, neither counts toward the
run-directory bound nor stops a run
(`TestRunDirectoryNamesAnotherAccountCreatedStopNoRun`).
Only a job's own processes hold
its lock exclusively. A sweep takes it shared, which it can only once none of
them runs and which keeps it so, and removes the job only while it also holds
the job directory's own lock exclusively, which keeps every other sweep from
removing it too: a sweep that cannot take that lock leaves the job to the one
removing it, and one that takes it after the job went finds the job's record
gone and leaves it. A job a sweep is removing therefore never looks held, and
neither refuses a run of its own context nor names the sweeping process as
one to end; only a sweep of a build before this rule takes the lock
exclusively, so across an upgrade a job it is removing refuses as a held one
until it is gone (`TestAJobAnotherSweepIsRemovingRefusesNoRun`,
`TestARunBesideAnotherContextsSweepsNeverRefusesOverItsEndedJob`). A sweep
likewise removes a scratch, with its job or once its job is gone, only while
it holds the scratch directory's own lock exclusively, which a sweep of a
build before this rule never takes, so two sweeps of this build never empty
one scratch together: one that cannot take that lock leaves the scratch to
the sweep removing it, and one that takes it after the scratch went leaves it
(`TestAnOrphanedScratchAnotherSweepIsRemovingRefusesNoRun`,
`TestTwoContextsSweepsOverOneOrphanedScratchNeverRefuse`). A held
job's record and holder decide what it says to a run only while its lock is
still held once they are read; a job let go meanwhile is free. A job's
record, holder and lock go last, in that order, so a job it could not remove
fails every later sweep closed too, while a job without its record is still
being created, or already going, and is left alone. A job publishes its
record whole, written under another name and renamed into place once its
holder exists, so a sweep never reads one half written, and another context's
job a sweep finds mid-claim or mid-removal neither refuses nor goes
(`TestASweepBesideAnotherContextsClaimsAndReleasesNeverRefuses`).

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

The installer process of a cluster's image build receives its route under the
same rule: it reaches the release payload through the controller Machine's
proxy choice, given as `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` from its
frozen request, never from the environment.

Not yet met: the image build runs direct; tracked as
[B721](milestones/m3.md#b721).

## Local context privilege

The [context store](contexts.md#storage-locking-and-publication) owns
root-only storage, and the [CLI boundary](cli.md#local-privilege-and-user-identity)
owns sudo authentication and credential refresh. Before `sudo`, the
unprivileged supervisor refuses `runtime.privilege` to re-execute a Bootwright
file that another account can write: one whose resolved file or any directory
above it is owned by a UID other than 0 or the invoking account's, or is
writable by group or others. A root-owned sticky directory such as `/tmp` is
exempt, because its sticky bit stops another account from replacing an entry
it does not own; a sticky directory another account owns still refuses. The
remedy installs the binary to a root-owned path; the
[CLI boundary](cli.md#local-privilege-and-user-identity) owns the report.
A file that passes with root owning it and every directory above it is
re-executed by that absolute path, so a sudoers rule can name the installed
binary, and the child confirms before it acts that it runs the file its
supervisor runs, as `/proc/<supervisor pid>/exe` names it, and that the path
sudo was named, the first word of `SUDO_COMMAND`, is that file and root-owned,
so an install path holding a space refuses; only the invoking account's own
build output is re-executed through `/proc/<pid>/exe`. Every root invocation
whose parent is the qualified sudo, a supervised child or a command an
operator ran with sudo by hand, refuses `runtime.privilege` before it acts on
any variable outside what sudo sets, the
[route vocabulary](controller.md#the-context-free-acquisition-route) and the
PAM session variables `KRB5CCNAME`, `DBUS_SESSION_BUS_ADDRESS`,
`XDG_RUNTIME_DIR`, `XDG_SESSION_*`, `XDG_SEAT` and `XDG_VTNR`, so a `SETENV`
tag admits nothing more. It unsets those session variables before it starts
any process. A host whose PAM stack, `/etc/environment` or pam_env
configuration exports any other variable into sudo's commands refuses naming
it; `env -i PATH=/usr/bin:/bin sudo -u '#0' -- /usr/bin/env` lists what a
root command receives there.

No password enters Bootwright
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
credential, and selects transport alone. When the admitted route is a proxy,
the host's `dnf` receives it under the [process boundary](#process-boundary)
rule, as `https_proxy` and `no_proxy` and nothing else ambient, and a `proxy=`
in the host's dnf configuration wins over it; dnf's own
configuration owns its repositories, mirrors, signing keys and `gpgcheck`, and
Bootwright never passes `--allowerasing`, `--nogpgcheck`, `--repofrompath`,
`--enablerepo` or `--releasever` ([host packages](controller.md#host-packages)).

TLS certificate and name verification and SSH host identity verification fail
closed. An insecure exception must be explicit, endpoint-scoped, visible in
effective intent, and incapable of becoming a global default. DNS resolution
and every connection target are checked against the same authorization so
rebinding or alternate-address retries cannot expand scope.

The BMC-to-artifact-server virtual-media leg imports the server's certificate
by default. Its one insecure exception, `disableVerification`, is explicit,
declared on one Machine and visible in that Machine's effective state. It is
never inherited, never a provider default and never an Environment kind
default, and a Machine whose installation delivers private material refuses it
before registration. The one bounded exception is the emulated BMC of a
virtual Machine, whose BMC uses verified HTTPS but fetches media
without verifying the artifact server. Every consumer booting through one has
its artifact server placed on that BMC's provider host, so the
unverified fetch never leaves that host: the
[container-cluster boot media](openshift-clusters.md#boot-media) and the
[managed-OS installation](managed-os.md#refusal-table) each refuse before
registration otherwise. A
BMC's own transport verifies against its declared CA bundle alone, or
against the system trust store when it declares none
([Machine BMC trust](api/machines.md#bmc-and-root-device-shape)).

The watcher token the agent installer mints with a cluster's image crosses the
machine network in cleartext. The assisted service on the cluster's rendezvous
host answers plain HTTP, and the installer's own client of it, through which
`agent wait-for` watches the cluster, authenticates with that token. The
install block sends the same token once more, in the one bounded read of the
registered hosts after a stalled wait, which reaches only an address its frozen
request declares ([container clusters](openshift-clusters.md#installation)).
This is no credential discovery: the token is the installer's own, read from
the member of the installer's asset state that the installer reads it from, in
the installation's root-owned work area. It is never an argument, a result,
evidence, output or a log entry.

A KubeVirt host cluster is reached only at the server its provider's
kubeconfig names, verified against the certificate authority that kubeconfig
embeds and authenticated by its client certificate or token, and a kubeconfig
that disables verification, carries no certificate authority, names a proxy,
refers to files or runs a plugin (an `exec` or `auth-provider` entry) refuses
before the adapter's first request, in the provider proof every Machine of
the provider waits for and again in every block and port entry point
([KubeVirt machine realization](substrates.md#kubevirt-machine-realization)),
because each would let the host connection run or trust something the
declaration does not name. A host cluster the context builds is reached the
same way, through a kubeconfig the arm's lend mints for each run, carrying
only a short-lived token of the ServiceAccount its `kubevirt` instance owns in
the provider's namespace; the host installation's administrator kubeconfig
reaches that lend alone
([KubeVirt machine realization](substrates.md#kubevirt-machine-realization)).
Its CDI upload proxy is verified against the
ingress certificate authority read through that verified connection, never with
`--insecure`. A vCenter is reached at its declared server and port over TLS
verified against the system trust store; its `disableCertificateVerification`
exception is endpoint-scoped and refuses wherever the transfer carries a
cluster's private material. A pushed image leaves the controller only to those
verified endpoints, and its destination's access control is recorded as inside
the trust boundary of what it embeds
([container clusters](openshift-clusters.md#boot-media)). On KubeVirt that boundary
outlives the release: the eject deletes the upload DataVolume, but the claim
that held the image, and with it the pull secret and installer state the image
embeds, stays bound until the node's VirtualMachine next stops, so whoever can
read claims in the provider's namespace can read it until then. Bootwright
restarts no node to shorten that window; the operator's first `machine stop`
of each node, or the cluster's removal, releases the claim, and the Machine's
removal proves it absent.

Not yet met: no vSphere connection exists; tracked as
[B99](milestones/m1.md#b99).

Managed probes and effects use the
[Ansible boundary and locked runtime closure](architecture.md#go-and-ansible-responsibility-boundary),
generated inventory/configuration, and pinned allowlisted `bootwright.core`
entrypoints. An SSH connection they open neither creates, joins nor outlives a
shared control connection, so no other process's authenticated channel carries
it. Only operation-required facts and the least authorized local and
remote privilege are available. No environment or adapter default grants
privilege escalation.

Every `ssh` client an adapter runs, and the runner's SSH arm, reads only a
configuration generated for that run, which includes the host crypto-policy
backend and nothing else. It takes no system or personal configuration, no
known hosts but the one entry the request binds, no proxy, control socket,
agent, forwarding or ambient identity, and carries `ServerAliveInterval=15` and
`ServerAliveCountMax=3`, so a peer that stops answering ends the connection
rather than the run's deadline.

The Ceph host, cluster and SMB blocks' controller sessions take the
controller's crypto-policy back end, reduced to its crypto directives, and the
managed-OS guest session's option set, with `HostKeyAlgorithms` set to the
pinned key's algorithms that the policy, as `ssh -G` evaluates it, admits, in
pinned order. When it admits none for a host the request's known hosts pin,
the block refuses `ssh-policy-host-excluded`, naming that host and its key
type, before any host is contacted; a policy that cannot be read or evaluated
refuses `ssh-policy-unavailable`. The contained cephadm tunnel's client runs
inside the selected Ceph image and keeps that image's back end: its
configuration pins, for each host, the pinned key's algorithms that the back
end's first `HostKeyAlgorithms` value admits under the list and pattern rules
of ssh_config(5) (no value, or a `+` or `^` form, keeps them all; a `-` form
removes those it matches; any other value keeps those it matches). Before
the cluster block claims or authorizes any host, each manager host it will
authorize renders that configuration in a read-only round trip, and an empty
intersection refuses `tunnel-host-key-excluded` there. Strict host-key
checking and the pinned known hosts are unchanged, and keeping a policy is not
FIPS certification.

The application request fixes exact targets and authorization before adapter
execution. The adapter cannot widen targets, privileges, retries, or effects.
Live remote identity, host-key or certificate identity, ownership, power state,
and claimed absence are positively proved where required; probe failure is
unknown. Secrets reach Ansible only by the channels
[sensitive material](#sensitive-material) permits. Every task, result, and diff
that could carry one uses `no_log` and no-diff behavior and must not persist it
in inventory, facts, caches, evidence, logs, or adapter results.

### Listener exposure

The operator owns host firewall policy; Bootwright does not open ports or
change zones. A listener serves only its declared consumers. A privileged
listener or one carrying cleartext credentials binds loopback or a host-only
address unless the operator explicitly places it on a trusted network. A
wildcard bind exposes all matching host interfaces and requires source
restrictions on each reachable interface. The
[operator guide](../docs/operator-guide.md#network-exposure) owns the firewall
procedure and ports by source.

| Listener | Clients | Transport and authentication | Privilege | Client restriction |
| --- | --- | --- | --- | --- |
| Managed Proxy | Selected graph's Machine addresses, network CIDRs and loopback | HTTP proxy; no client credential | Root container with host networking | Squid source ACL derived from the graph; other clients refused |
| Managed DNSServer | Selected Machines and the controller; cluster consumers when declared | DNS over UDP and TCP; no client authentication | Root container with host networking | Bind address and operator firewall; every reachable client can query and use declared forwarders |
| Managed NTPServer | Selected graph's Machine addresses, network CIDRs and loopback | NTP over UDP; no client credential | Root container with host networking, without host clock adjustment | Chrony source ACL derived from the graph |
| Managed ArtifactServer | BMCs fetching virtual media, installers fetching packages or private material, and controller probes | Authored HTTP or HTTPS; HTTPS verifies the declared serving certificate; private URL is a capability | Root container master with application-user workers and host networking | Bind address and operator firewall; private subtree needs its token |
| Emulated BMC | Controller/provider-host clients | Verified HTTPS with basic authentication and the provider's bound serving certificate | Root container with host networking, SELinux label disabled and access to the libvirt socket | Lab examples bind loopback; other binds require a trusted network |

Private URL capabilities travel over verified TLS, subject only to the
[emulated-controller boot-media exception](openshift-clusters.md#boot-media).
Public package trees carry no credentials.
Preflight and plan report each declared controller listener's runtime firewall
admission. Closed or unknown required sockets refuse before listener or
dependent-consumer registration; inspection changes no firewall rules. The
[controller contract](controller.md#results-and-qualification) owns the bounded
firewalld query and manual remedy. [B318](milestones/delivered.md#x62--controller-listener-firewall-inspection)
records its in-tree evidence; B78 retains real-hardware qualification.
Not yet met: the emulated BMC does not refuse a non-loopback bind and has no
confined SELinux type; tracked as [B334](milestones/backlog.md#b334).

Verified TLS transport is delivered under
[B446](milestones/delivered.md#x47--verified-emulated-controller-trust-and-bounds).

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
Under `--yes` or a structured format, [host-key trust](cli.md#host-key-trust)
records or re-trusts only a key whose fingerprint the operator supplied.

Session material — the private key, the pinned host key and the client
configuration — stays in anonymous mode-0600 files held by the launcher until
the client exits. The client reopens the launcher's actual descriptors through
`/proc/<launcher-pid>/fd/<fd>` under the same credentials, so client startup
closing its inherited descriptors cannot remove access. Material contents
reach no named file, argument or environment value. An operator-offered key
file joins that material: it is opened under the
invoking account's credentials without following a link at its name, proved on
the received handle to be a regular file the invoking account owns and no other
account can read, and copied through that handle into anonymous session
material, so the bytes the client reads are those of the file that was proved
and the client never reopens the operator's file. The configuration is
Bootwright's own: it carries only allowlisted cryptographic directives from the
host crypto-policy backend where that exists, so site and FIPS policy is
retained, and it admits no identity, certificate, agent, command, forwarding
or host rule from system or personal configuration.
The pinned host key's algorithms are intersected with that policy as the
pinned client itself evaluates it (`ssh -G` over the reduced configuration,
contacting no endpoint), and the session pins only the intersection, so
command-line pinning cannot re-enable an algorithm the policy excludes. An
empty intersection refuses `access.unavailable`, naming the Machine and the key
type, before any session opens; a policy the client cannot evaluate refuses
the same way, with no fallback. A host with no policy backend keeps the pinned
algorithms under OpenSSH's defaults. This is access compatibility, not FIPS
certification.
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
The controller runtime's Python packages are pinned by the
[runtime lock](controller.md#the-runtime-lock) each build embeds: every wheel's
URL, size and SHA-256, chosen when the lock is generated and never at setup,
installed with `--no-index --no-deps --require-hashes --only-binary=:all:`
from a cache that verified each one, so no setup resolves, discovers or checks
for a newer version and the upgrade is a new build. Host packages are the
exception: the host's `dnf` resolves them under its own signature policy
([W2](#the-controller-hosts-attacker-model)), and the evidence records each
provider's NEVRA and repository. Target tools are a named widening of
publisher authenticity: a [target tool's](controller.md#the-controller-stage)
SHA-256 is its publisher's own release metadata, the release's asset digest or
the publisher's checksum file, read over verified HTTPS from the publisher even
when a mirror serves the bytes. No publisher signature is verified, so a
publisher compromised at the first acquisition is trusted from then
on: the digest and size that acquisition records pin every later use. A tool
whose publisher offers no such metadata refuses. A frozen operation never
substitutes an update silently; recovery follows
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

Authored add-ons, [`ClusterAddon`](api/addons.md) declarations, are
declarative data that only qualified Bootwright-owned code consumes; built-in
[add-on packages](add-ons.md) are reviewed Go code whose program the
[cluster object adapter](add-ons.md#the-cluster-object-adapter) runs. Built-in
and custom origins have the
same integrity, isolation, and negative-proof requirements, and custom
packages gain the built-in lifecycle only with
[B90](milestones/m4.md#b90). Neither grants process, adapter, target, effect,
inventory, endpoint, dependency, credential, or privilege authority.
Package-carried executable content is never loaded or run;
that code may enter only its pinned embedded adapter and locked dependency
closure under [architecture](architecture.md#go-and-ansible-responsibility-boundary).

A built-in add-on package is Go code reviewed with the executable: its
admission and rendering are pure, it never patches, adopts or deletes a
cluster object it did not create, and its only remote effects are those of the
[cluster object adapter](add-ons.md#the-cluster-object-adapter), which starts
only the located `oc`. A built-in package grants privileges to identities it
does not own only as its configuration selects: the operator chosen by
catalog, channel and `startingCSV` receives the permissions its CSV declares
when Bootwright approves its plan, an operator-managed package's operator
receives what the plans it approves declare, and a role its configuration
names is bound as the GitOps bootstrap's access is. The operator's declaration
is the trust decision, and the grant outlives a physical cluster's context.

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
validation, identity, ownership, bounds, logging, or live probes. The
explicit `machine stop --force` power-off choice does not override those
proofs or authorize adoption; [machine power](substrates.md#identity-and-power-operations)
owns its bounded behavior.

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
| Secret and credential material never reaches output, diagnostics, verbose paths, logs, adapter events, retries, errors, cancellation or `no_log` handling. | `TestSecretNormalOutputsAndStateNeverContainMaterialOrDigests`, `TestMaterialNeverAppearsInMetadataOrErrors`, `TestVariablesCarryPathsNotMaterial`, `TestProducedMaterialNeverReachesRecordsLogsOrAdapterOutput`, `TestSecretCommandsNeverListOrTouchProducedMaterial`, `TestTheInstallOffersItsKubeconfigOnlyOnProvedCompletion`, `TestManagedAndExternalStorageClustersAreNotApplicableAndReadNoCustody`, `TestAFailedRunReadsNoOutput`, `TestClusterKubeconfigWritesExactlyItsBytesOrOneDiagnostic`, `TestTheAdapterOutputPrintsNothingANoLogTaskRaised`, and the collection's `test_no_material_or_completion_reaches_the_adapters_own_output`, `test_the_censored_callback_prints_nothing_a_hidden_task_raised`, `test_no_management_controller_refusal_is_censored`, `test_every_controller_refusal_names_the_controllers_message_and_nothing_it_was_given`, `test_the_module_reports_the_registration_and_never_the_token` and `test_the_read_after_a_stall_runs_under_no_log_and_never_fails_the_wait_itself` | none |
| Time, retry, concurrency, memory, disk, log and process-output limits hold, including cancellation and process-tree reaping. | `TestAdapterOutputStreamsWhileItRunsAndBoundsWhatItKeeps`, `TestGuardedCommandDiesAndIsReapedAfterParentExit`, `TestAKilledInvocationTakesItsAdapter`, `TestStoppingAnAdapterEndsItsTreeBeyondItsGroup`, `TestThreadChurnNeverSignalsARunningAdapter`, `TestAnAdapterStillRunningRefusesTheNextRun`, `TestAStaleRunDirectoryIsRemovedWithItsSecretFiles`, `TestHangupCancelsTheOperationLikeTerminate`, `TestAnIgnoredHangupLeavesTheOperationRunning`, `TestHangupIsRelayedToThePrivilegedOperation`, `TestRunnerReapsAnUnauthorizedChildOnCancellation`, `TestLifecycleConcurrencyBound`, `TestOperationBoundaryPreservesOrdinaryCancellationAndDeadline`, `TestABoundedRunCancelledInsideItsCallKeepsNoMaterial`, `TestABoundedConsumerCancelledInsideItsCallKeepsNoMaterial`, `TestABoundedConsumerCancelledWhileReadingRunsNothing`, and the collection's `test_the_read_is_bounded_in_time_however_slowly_the_answer_arrives` and `test_the_read_is_bounded_in_size` | B17, B23 |
| Dependency integrity, lock agreement and native-schema compatibility hold, and runtime-tool substitution or drift refuses. | `TestFrozenToolRejectsVersionRouteAndChecksumSubstitution`, `TestTheLockCarriesOneAnsibleCoreEverywhere`, `TestEveryLockedURLIsOnFilesPythonhosted`, `TestAWheelWithAnotherHashIsRefused`, `TestDecodeRefusesPackagesItsClosuresDoNotSelect`, `TestEveryCapabilityHonoursTheCapabilityContract`, `TestThePowerRolesAdmitTheRequestsThisBuildSends`, `TestTheRoleAdmitsEveryRequestThisBuildSends`, `TestRoleVersionAssertionsMatchTheirArgumentSpecs` | none |
| Mutation crash points, lease conflict, replay, partial success, rollback, evidence loss and required-log write failure leave a recoverable context. | `TestCrashReleasesLocksAndLeavesCompleteSelection`, `TestLifecyclePublicationCheckpointsFireAndFailClosed`, `TestMutationGuardLayoutAndLeases`, `TestAPartlyRealizedBlockWhoseRetryFailsIsConvergedByRepeatingTheOperation`, `TestRequiredLogFaultStopsTheOperation`, `TestARestorationWhoseClearFailsStartsNothing`, `TestAJourneyKilledAtAnyWriteLeavesAUsableStoreAndConvergesOnRetry`, `TestAnUnknownDestroyBlockResolvesByWhatItsRemovalProves`, `TestAnInterruptedRemovalFinalizationIsCompletedByTheNextDestroy`, `TestARemovalWhoseBindingReleaseFailsReportsIncompleteFinalization`, `TestAnInterruptedApplyFinalizationIsCompletedByTheRepeatedApply`, `TestARunningOperationWhoseBlocksAreAllDoneIsFinalized`, `TestAFreshApplyOverAnUnfinalizedRemovalFinishesItFirst`, `TestNoFinalizationOverContradictedRecords`, `TestAReservationHeldBesideACompletedRemovalIsReleasedByTheNextDestroy`, `TestAFinalizationRefusesWhenTheContextChangedBeforeIt`, `TestEvidenceProtectsTheContextBeforeItBindsOrReserves`, `TestAFreshApplyClaimsItsOperationBeforeItBinds`, `TestAFailedEvidencePublicationBindsNothing`, `TestConcurrentFreshAppliesThatBothFailLeaveNothingRaised`, `TestABindingListingThatFailsBlocksNothing`, `TestARegistrationThatProvablyFailedRestoresTheEvidence`, `TestAFailedRegistrationWithReservationsKeepsItsEvidence`, `TestARegistrationThatMayHaveHappenedKeepsItsBinding`, `TestAContinuationProjectsBeforeItsFirstEffect`, `TestTheNextRegistrationReleasesBindingsNoOperationNames`, `TestADestroyReleasesWhatAnInterruptedRegistrationLeft`, `TestAnApplyWhoseBindingAnUnclaimedReleaseTookRefuses`, `TestAnApplyRefusesWhenAnotherClaimRaisedTheEvidenceAgain`, `TestAnApplyOverACompletedDestroyRefusesWhenAFinalizationTookItsBinding`, `TestADestroyOverUnindexedRecordsRefuses`, `TestARemovalRaisesItsEvidenceBeforeItRegisters`, `TestAnApplyInterruptedBeforeItRegistersGivesBackWhatItRaised`, `TestAFailedRestorationIsReportedBesideItsCause`, `TestADestroyReleaseRefusesWhenAnOperationRegisteredBeforeIt`, `TestARemovalWhosePublicationFailedCollectsNothing`, `TestAClaimedOperationRegistersIntoItsDirectory`, `TestAnUnknownOperationWhoseBlocksAreAllDoneIsFinalizedByItsOwnVerb`, `TestAFreshApplyWhoseClaimFailedRestoresTheEvidenceItsDirectoryMayHold`, `TestAnUnclaimedReleaseCollectsOnlyTheBindingsItReadFirst`, `TestAFinalizationCollectsOnlyTheBindingsItReadFirst`, `TestAnIncompleteApplyWithoutAContradictionIsStillRemovable`, `TestAnInitializationResumesOverATornStageAtEveryStagedWrite`, `TestAnInitializationRefusesAStageThatParsesButAttributesNothing`, `TestAContextInitRetryResumesOverATornKeyringStage`, `TestTheKillHarnessReachesTheCustodyPublications`, `TestAFailedPublicationLeavesTheAttemptUnknown`, `TestADestroyAfterAFailedPublicationCapturesBeforeAnyInverse`, `TestAnAgentInstallWhosePublicationFailedIsCapturedBeforeItsRemoval`, `TestACancelledPublicationLeavesItUnknown`, `TestAStoppedDestroyKeepsTheAccess`, `TestAFinalizationWithdrawsWhatAnInterruptedRemovalLeft` | none |
| A command whose [catalog](cli/commands.md#command-and-flag-catalog) Effects cell reads `none` or `read local state` performs no writes, payload reads, processes, network access, secret lookup or generation. | `TestImmutableInputAndReadOnlyLifecycleBoundary`, `TestALifecycleInspectionRunsNothing`, `TestPlanPreviewsWithoutWritingAnything`, `TestStubServicesRemainStubs`, `TestReadsNeverCollectAStage` | none |
| Rejected operations perform no effect. | `TestCancellationBeforeAnyEffectRegistersNothing`, `TestDeclinedConfirmationRegistersNothing`, `TestUnsupportedObjectsRefuseBeforeRegistration`, `TestInvalidAdmissionAndUnavailableRoutesDoNotWrite`, `TestADestroyOverAnIncompleteApplyWithAContradictedBlockRefuses`, `TestARepeatedApplyOverACompletedApplyWithABlockNotDoneRefuses`, `TestATransitionRefusesWhenItsFrozenPlanChangedBeforeMutation`, `TestATransitionRefusesWhenTheOperationRecordChangedBeforeMutation`, `TestAContinuationRefusesWhenABlockWasRetriedBeforeMutation`, `TestAFrozenPlanThatIsNotItsOperationsRefuses`, `TestARepeatedMaterialRefusesBeforeAnyJobExists`, `TestAnUnsafeMaterialNameOrValueRefusesBeforeAnyJobExists`, `TestAFrozenPlacementOffTheControllerRefusesEveryVerb` | none |
| A failed security check cannot be bypassed by confirmation, retry or adapter behavior. | `TestMissingSafeguardsCannotBeReplacedByConfirmationFlags`, `TestOrphanAcknowledgementClaimsNothingAndReplacesNoOtherSafeguard`, `TestDestroyOverAnInterruptedApplyIsStillGated` | none |
| Package content cannot select code, and configuration selects code only through the declared catalog, channel and CSV; neither can carry Secret bytes into a request, adopt a foreign object or force a field conflict, and a placed Secret's value reaches no record or output. | `TestDeclaredSecretPlacementLendsOnlyMappedPartsGolden`; the collection's cluster object adapter tests | [B751](milestones/delivered.md#b751), [B753](milestones/m4.md#b753) |
| A Ceph consumer's keys and external-cluster details reach only custody and the consuming cluster's owned Secret, never a request, record, evidence, log, diagnostic, argument or output; no gateway private key leaves the Ceph side; an export never adopts or alters an entity it did not mint. | none yet | [B86](milestones/m4.md#b86), [B760](milestones/m4.md#b760) |
