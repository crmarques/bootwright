# Command and Flag Catalog

Read with [CLI behavior](../cli.md) and [output contracts](output.md).
The tables define the complete public command tree, operands, flags and defaults.

## Global flags

Every command accepts and parses these inherited long flags so the same flag
spellings and resolved values can be reused across workflow commands. A command
resolves a global value only when its use case consumes it. An unused value
grants no authority, performs no discovery or prompt, and changes no result;
its syntax and context-independent safety checks still apply.

| Flag | Type and default | Contract |
| --- | --- | --- |
| `--context <name>` | context name; current context | Select the named context for a context-backed command. `preflight controller` consumes only an explicit nonempty value and omission selects its host baseline, ignoring current selection; `setup` selects no context and consumes no value. A non-empty value conflicts with context-free `render --input-dir`. |
| `--ssh-id-file <path>` | path; none | Offer this private key first for an SSH operation; a declared credential remains the fallback. A leading `~` resolves from the invoking account database, not an untrusted `HOME`; the opened file must satisfy the private-file rules in [security](../security.md). |
| `--ssh-user <name>` | POSIX user name; none | Use one explicitly borrowed account for eligible OS-ready machines. On [`machine rsh` and `machine exec`](../cli.md#machine-ssh-sessions) it reaches any account: a value naming an identity this context holds resolves to that identity's credential, and any other value offers no stored credential. It does not alter desired state or the managed identity Bootwright installs. |
| `--ssh-ask-sudo-password[=<bool>]` | Boolean; `false` | Prompt once for the borrowed account's sudo password, hold it only in bounded memory for this invocation, and never place it in arguments, environment, state, output, or logs. It conflicts with JSON output and non-interactive execution, and is not consumed by `machine rsh` or `machine exec`. |
| `--ssh-user-for-provisioned[=<bool>]` | Boolean; `false` | Extend `--ssh-user` to Bootwright-provisioned machines. It requires `--ssh-user`; the same frozen account must pass the managed-OS ownership probe. It is not consumed by `machine rsh` or `machine exec`, which reach a provisioned Machine's accounts through `--ssh-user` alone. |

Every command also accepts `-h` and `--help`. Help performs no desired-state
discovery, state lookup, secret access, privilege escalation, process launch,
network access, or write after the command path and flag syntax are resolved.

No command takes a proxy flag. `setup`, `preflight controller` without a
context, and `media add --from-url` acquire before any Environment exists, so
they read the
[context-free acquisition route](../controller.md#the-context-free-acquisition-route)
from the invoking environment; an invocation that is not admitted reads
nothing. Every other command uses its Machine's declared proxy choice.
After surrounding whitespace is trimmed, an SSH user matches
`^[a-z_][a-z0-9_-]{0,31}$` exactly.

Context-independent validation of inherited flags always checks an explicitly
non-empty `--context` against the context-name DNS-label grammar, an explicit
`--ssh-user` against the trimmed grammar above,
`--ssh-user-for-provisioned=true` for its required non-empty `--ssh-user`, and
`--ssh-ask-sudo-password=true` for its conflict with JSON output. TTY detection,
prompting, path inspection, tilde expansion, and operating-system account lookup
are deferred until an available use case needs them. An unavailable invocation
performs none of those operations.

## Command and flag catalog

The tables list local flags; inherited global flags and `-h`/`--help` are not
repeated. “Local” means the command may create or update Bootwright-owned local
state but contacts no managed target. “Observe” permits bounded read-only
process or network access. “Mutate” permits only the named, planned effects.

### Setup commands

| Invocation | Local flags and defaults | Successful result | Effects |
| --- | --- | --- | --- |
| `bootwright context init` | required `--name <name>`; optional `-f, --file <context.yaml>` and `--input-dir <dir>` | initialized context and user selection; optional imported-input summary | local context and keyring creation |
| `bootwright context update` | required `--name <name>`; at least one of `-f, --file <context.yaml>` or `--input-dir <dir>`; `--yes` false | unchanged configuration or imported-input summary | local atomic input replacement; never lifecycle reconciliation |
| `bootwright context use` | required `--name <name>` | selected-current-context summary | local current-context update |
| `bootwright context list` | none | contexts in canonical name order | read local state |
| `bootwright context current` | `--short` false | current context details, or only its name with `--short` | read local state |
| `bootwright context delete` | required `--name <name>` and `--purge`; `--allow-orphans` and `--yes` false | permanent local deletion summary, reporting any objects it abandoned | guarded removal under the [context rules](../cli.md#context-and-setup-behavior); never resource mutation |
| `bootwright add-ons list` | `--output text\|json` default `text` | built-in catalog and machine-local registrations | read embedded and local catalog state |
| `bootwright add-ons add` | required `--name <name>[:<version>]`; `--version <version>` default catalog default; `--yes` false | registered immutable catalog release | local add-on registration |
| `bootwright add-ons delete` | required `--name <name>[:<version>]`; `--yes` false | removed matching registration | local add-on registration deletion |
| `bootwright secret set` | required `--name`; type-specific `--value-file`, `--value-stdin`, `--username`, `--password-file`, `--password-stdin`, `--certificate-file`, `--private-key-file`, `--public-key-file` under [Secrets](../secrets.md); `--yes` false | stored identity and parts, never values | local confidential write |
| `bootwright secret generate` | optional `--name`; `--renew` false | changed/unchanged counts | atomic selected generated-material batch |
| `bootwright secret check` | `--output text\|json` default `text` | declared availability/type checks | bounded material reads; no values emitted |
| `bootwright secret list` | `--output text\|json` default `text` | context secret identities, types, parts, and availability | read confidential-store metadata |
| `bootwright secret show` | required `--name` and `--part value\|username\|password\|certificate\|private-key\|public-key` | exact selected sensitive bytes | read and reveal current declared part |
| `bootwright secret delete` | required `--name <name>`; `--yes` false | removed active mapping or unchanged identity | local logical deletion, preserving bound versions |
| `bootwright secret encryption init` | none | configured implementation and active key | idempotent initialization using Context configuration |
| `bootwright secret encryption status` | `--output text\|json` default `text` | keyring and encrypted-store status | read confidential metadata |
| `bootwright secret encryption rotate` | `--yes` false | new active key identity and re-encryption summary | atomic local key rotation and re-encryption |
| `bootwright media add` | required `--name <filename.iso>` and exactly one of `--from-file <path>` or `--from-url <http-or-https-url>`; `--sha256 <digest>`; `--yes` false | stored media identity, size, and verified digest | local copy or bounded download over the [context-free route](../controller.md#the-context-free-acquisition-route) and atomic publication |
| `bootwright media list` | `--checksums` false; `--output text\|json` default `text` | media names, sizes, and optional computed digests | read local media; `--checksums` reads each image in full |
| `bootwright media delete` | required `--name <filename.iso>`; `--yes` false | deleted media identity | local media deletion when not frozen by an operation |

The required controller declaration and context-free setup boundary follow
[controller command applicability](../cli.md#controller-declaration-and-command-applicability).
The three `media` commands select no context: they manage the host-wide
[media store](../managed-os.md#media-store) with the store's privilege, and an
explicit `--context` changes nothing they do.

### Inspect and lifecycle commands

| Invocation | Local flags and defaults | Successful result | Effects |
| --- | --- | --- | --- |
| `bootwright preflight controller` | none | [host readiness, and a selected context own prerequisites and binding](../controller.md#selection-and-command-journeys) | bounded local observation; no state publication |
| `bootwright preflight controller` | none | [baseline or explicit-context controller readiness](../controller.md#selection-and-command-journeys), including the acquisition route it resolved | bounded local observation; no state publication |
| `bootwright preflight infra` | `--clusters <list>` default all; `--dry-run` false; `--output text\|json` default `text`; `--trust-on-first-use=<bool>` default `true`; `-v, --verbose` false | infrastructure readiness checks | observe unless `--dry-run`, which is local-only |
| `bootwright preflight clusters` | same flags as `preflight infra` | all selected cluster readiness checks | observe unless `--dry-run` |
| `bootwright preflight container-cluster` | same flags as `preflight infra`, with ContainerCluster-only selection | container-cluster readiness checks | observe unless `--dry-run` |
| `bootwright preflight storage-cluster` | same flags as `preflight infra`, with StorageCluster-only selection | storage-cluster readiness checks | observe unless `--dry-run` |
| `bootwright preflight add-ons` | `--clusters <list>` default all ContainerClusters; `--output text\|json` default `text` | add-on prerequisite checks | bounded observation |
| `bootwright preflight all` | `--dry-run` false; `--output text\|json` default `text`; `--trust-on-first-use=<bool>` default `true`; `-v, --verbose` false | all controller, infrastructure, cluster, storage, and add-on checks | observe unless `--dry-run` |
| `bootwright plan` | `--stage <list>` default all stages | next legal full-context plan or exact continuation point, with the steps each block waits for, how much of it can run at once, and the blocks a stage selection would start | none |
| `bootwright status` | `--output text\|json` default `text`; `--watch` false; `--watch-interval <duration>` default `5s` | context readiness, lifecycle state, and next safe commands | read local state; watch repeats reads |
| `bootwright render` | `--input-dir <file-or-dir>`; `--output-dir <dir>`; `--clusters <list>` default all; `--sensitive` false; `--output text\|json` default `text` | whole external-tool artifact manifest, or render help when neither path flag is supplied | local artifact writes only |
| `bootwright render effective` | `--output text\|json` default `text` | normalized effective desired state and object counts | none |
| `bootwright render installer` | `--clusters <list>` default all ContainerClusters; `--sensitive` false; `--output text\|json` default `text` | installer artifact manifest | local placeholder files and optional sensitive files |
| `bootwright render storage` | `--clusters <list>` default all StorageClusters; `--output text\|json` default `text` | storage artifact manifest | local native files or scripts only; never execution |
| `bootwright apply` | repeatable `--authorize <token>[,<token>...]`; `--stage <list>` default all stages; `--yes` false; `-v, --verbose` false | completed full apply, a pause at the selected stage boundary, exact continued state, or a settled result when the declared state is already realized | complete planned mutation, or none when it settles |
| `bootwright destroy` | repeatable `--authorize <token>[,<token>...]`; `--yes` false; `-v, --verbose` false | completed full destroy, exact continued state, or a settled result when the context owns nothing | complete planned removal, or none when it settles; over an apply that did not complete, it first proves the outcome of every effect it takes back |

### Resource, access, and general commands

| Invocation | Local flags and defaults | Successful result | Effects |
| --- | --- | --- | --- |
| `bootwright machine list` | `--clusters <list>` default all; `--power` false; `--silent` false; `--output text\|json` default `text` | Machines with the evidence-backed lifecycle position each reached, their controller-reported power with `--power`, or sorted names with `--silent` | read local state; with `--power`, read each machine's power through its management controller |
| `bootwright machine rsh` | required `--name <machine>` | an interactive SSH session on the exact Machine as its resolved identity; the client's exit status is the result | read state, open the session's credential for its duration, read or record host trust, run one pinned SSH client |
| `bootwright machine exec` | required `--name <machine>` and `<command>...` | the exact argument vector runs on the Machine; the remote command's exit status is the result | same |
| `bootwright machine trust` | `--machines <list>` default all; `--replace <list>` default none; `--dry-run` false; `--yes` false; `--output text\|json` default `text` | exact host-key trust plan, and the recorded result unless dry-run | bounded SSH host-key observation of the selected Machines; context trust-store write unless dry-run |
| `bootwright machine start` | required `--name <machine>`; `--output text\|json` default `text` | the power state the Machine's management controller proved once the operation settled | bounded power operation through that controller |
| `bootwright machine stop` | required `--name <machine>`; `--force` false; `--yes` false; `--output text\|json` default `text` | same, after the operating system is asked to shut down | same |
| `bootwright machine restart` | required `--name <machine>`; `--force` false; `--yes` false; `--output text\|json` default `text` | same, after a proved stop and a proved start | same |
| `bootwright setup` | `--dry-run` false; `--yes` false; `--purge-old-bundles` false | [context-independent controller prerequisite plan or completed setup](../controller.md#selection-and-command-journeys), and with `--purge-old-bundles` the [superseded bundles it retired](../controller.md#supported-host-and-dependency-selection) | bounded dependency acquisition over the [context-free route](../controller.md#the-context-free-acquisition-route) and local installation; no context selection or binding; dry-run only previews; retirement removes only superseded execution bundles and only after the setup completes |
| `bootwright cluster list` | `--output text\|json` default `text` | container and storage cluster names and kinds in canonical order | read local state |
| `bootwright cluster info` | `--name <cluster>` default all unless `--secrets`; `--secrets` false; `--output text\|json` default `text` | cluster kinds, endpoints, access-command applicability and availability, artifact availability, and optional explicit sensitive values | read local state and optional confidential material |
| `bootwright cluster rsh` | required `--name <cluster>`; `--node <node>` default first node in canonical name order | bounded handoff for an interactive SSH session to the resolved cluster node | read target and access metadata only |
| `bootwright cluster exec` | required `--name <cluster>`; `--node <node>` default first node in canonical name order; required `<command>...` | bounded handoff for the exact remote command argument vector | read target and access metadata only |
| `bootwright cluster oc` | required `--name <cluster>` and non-empty `<command>...` | bounded `oc` handoff descriptor | read context-owned access metadata only |
| `bootwright cluster kubectl` | required `--name <cluster>` and non-empty `<command>...` | bounded `kubectl` handoff descriptor | read context-owned access metadata only |
| `bootwright cluster kubeconfig` | required `--name <cluster>` | kubeconfig through the explicit sensitive-output boundary | read and reveal one context-owned credential artifact |
| `bootwright version` | none | version, commit, source state, Go runtime and target, and embedded dependency-bundle identity | none |
| `bootwright help [command ...]` | no local flags | human help for the exact resolved command | none |
| `bootwright completion bash` | `--no-descriptions` false | Bash completion script | none |
| `bootwright completion zsh` | `--no-descriptions` false | Zsh completion script | none |
| `bootwright completion fish` | `--no-descriptions` false | Fish completion script | none |
| `bootwright completion powershell` | `--no-descriptions` false | PowerShell completion script | none |

### Cluster command applicability

`--name` selects from the shared ContainerCluster/StorageCluster name namespace
in the selected context. The following table owns applicability; access evidence
and resolution follow [resource inspection and explicit access](../cli.md#resource-inspection-and-explicit-access).

| Command | Applicable targets |
| --- | --- |
| `cluster list`, `cluster info` | All selected ContainerClusters and StorageClusters, including external storage clusters. |
| `cluster rsh`, `cluster exec` | OpenShift/OKD ContainerClusters and managed Ceph StorageClusters with declared nodes. External StorageClusters have no declared node roster and are inapplicable. |
| `cluster oc`, `cluster kubectl`, `cluster kubeconfig` | OpenShift/OKD ContainerClusters only. |

Applicability does not establish implementation availability, access readiness,
identity, or ownership. Missing access material on an applicable target is an
access failure, not a change in applicability.

### Access help

The short description and detailed help for `cluster rsh`, `cluster exec`,
`cluster oc`, and `cluster kubectl` state that success prints an access
descriptor for independent operator execution and does not launch a client or
connect. Cluster command help includes its applicable target kinds and variants
from the table above. `cluster kubeconfig` help states that success exports raw
sensitive bytes to standard output.

The short description and detailed help for `machine rsh` and `machine exec`
state that the session runs on the Machine as its resolved identity, that an
unproved host key is confirmed interactively or refused, and that the exit
status is the client's.

Help and completion use the static catalog; they never load a selected cluster
to hide or enable commands. Target-aware discovery belongs to `cluster info`.

### Version output

`version` writes the build identity to standard output in the
[shared human layout](output.md#shared-human-layout) and nothing to standard
error: the `Bootwright` headline, then exactly these six fields in this order.

```text
Bootwright

  Version            v0.4.0
  Commit             9f2c1d0e3b5a7c8d9e0f1a2b3c4d5e6f70819293
  Source             clean
  Go                 go1.26.7
  Target             linux/amd64
  Dependency bundle  sha256:<64 hexadecimal digits>
```

The composition root supplies every value. It obtains the Go runtime, GOOS, and
GOARCH from the linked Go runtime and combines them with the link-supplied
version, commit, source-state, and dependency-bundle values. For a version,
commit, or source state the build left empty, it uses the module version,
revision, and working-tree state recorded in the linked build stamp instead, so
a build identifies the revision it was produced from and whether that revision's
working tree was modified. An identity that neither source establishes is
reported as absent, never assumed. Surrounding whitespace is removed from
composition-root values, and every displayed value follows the
[safe display escaping](output.md#json-output). An empty version renders as
`devel`. A non-empty commit is valid only when it contains 7 through 64
hexadecimal digits; it renders in lowercase, and an empty or invalid commit
renders as `unknown`. The source state is exactly `clean` or `modified`; any
other value, including an absent one, renders as `unknown`. An empty Go runtime,
GOOS, or GOARCH also renders as `unknown`, including in its corresponding target
component. An absent dependency bundle renders as `none`.
A present dependency bundle is its canonical content identity, exactly
`sha256:` followed by 64 lowercase hexadecimal digits; a value that is empty or
does not have that form renders as `none`. The command performs no environment
lookup, filesystem access, process launch, network access, state or secret
lookup, random generation, or other effect.

### Completion

Completion generation and the private completion protocol derive solely from
the closed declarative command catalog, the bounded argument prefix, and — for a
path-valued flag alone — the directory that prefix names. They perform no
standard-input, environment, context, state, secret, process, network, random,
prompt, privilege, or remote access, and the completion path starts no
concurrent work. A generated shell integration may invoke only the Bootwright
executable's private completion protocol; it performs no filesystem fallback,
word expansion, or other subprocess, so a partly typed value is never evaluated
by the shell. Completion offers exactly the applicable public command paths,
public flags and shorthands, and closed enum values defined by this contract.

A flag declared as directory-valued or file-valued additionally offers
filesystem candidates, which the executable alone enumerates. It reads only the
single directory named by the completed prefix, never recursively. A
directory-valued flag offers directories; a file-valued flag offers files and
directories, so a path can be completed one segment at a time. A directory
candidate carries a trailing separator and the request reports that the shell
must append nothing of its own. Candidates are bounded in count and in entry
length; a dot entry is offered only once the prefix names one; and any entry
carrying a control character, whitespace, or a character significant to a shell
word is withheld rather than escaped, because such a candidate could not be
inserted unchanged. An unreadable directory yields no candidates and no
diagnostic. Completion offers no dynamic candidate
for context, object, catalog, target, or other name or free-form value. The
private protocol entries remain hidden and are never themselves candidates. A
generated integration presents descriptions only when the shell provides a
distinct description channel that leaves the inserted candidate bytes unchanged;
otherwise it safely omits their presentation. `--no-descriptions` suppresses
description retrieval and presentation but never changes candidate membership
or order.

The PowerShell integration requires version `7.7.0-preview.2` or later and
refuses an older runtime before registering its completer. This is the first
qualified release with the
[upstream empty-result fix](https://github.com/PowerShell/PowerShell/pull/27398)
needed to suppress filesystem fallback without throwing an exception or
inserting a false candidate. Stable PowerShell `7.6` is not supported by this
integration.

## Flag relationships and safeguards

`--output` is command-local and accepts exactly `text` or `json`. There is no
YAML output mode, global output flag, or `--format` alias. `apply`, `destroy`,
`plan`, setup mutations, access-handoff commands, credential-export commands,
`version`, help, and completion are text or raw only.

`--yes` suppresses only the named command's ordinary confirmation after target
selection and every independent safeguard and authorization succeeds. It may
confirm a safe command-owned overwrite, recreation, or replacement, but it
does not itself select a target or authorize data loss,
a changed or unknown identity, a failed probe, or another named risk. If a
prompt remains necessary, the command fails instead when standard input is
non-interactive, JSON output is selected, or a safe answer cannot be read.

`context delete` requires `--purge` to resolve to `true`; omission or
`--purge=false` fails without changing state. The flag acknowledges deletion of
proven-disposable local context data, while `--yes` independently controls its
ordinary confirmation. A context that still owns realized objects refuses
deletion and names `bootwright destroy`. `--allow-orphans` acknowledges those
objects and deletes anyway, abandoning them; it replaces neither `--purge` nor
the confirmation, and unreadable lifecycle evidence refuses under it. A
deletion that abandons objects warns once on standard error and reports the
abandonment in its result.

`--stage` accepts only `controller`, `infra-components`, `substrates`,
`machines`, `clusters`, and `add-ons`, and only on `plan` and `apply`. Whitespace around
comma-separated members is ignored, empty members are ignored, duplicates
collapse, and the last occurrence wins. A supplied value that resolves to no
member and an unrecognized member are both usage errors. Omission selects every
stage. The flag gates which blocks an invocation starts and never narrows the
frozen plan, the lifecycle unit, or ownership; the complete contract is owned by
[state reconciliation](../state-reconciliation.md#stages-and-the-pause-boundary).

`--authorize` accepts only `data-loss`, and only on `apply` and `destroy`.
Unknown, empty, duplicate, or inapplicable tokens are usage errors; `all` is not
accepted. Whitespace around comma-separated tokens is ignored. The token
acknowledges an already-planned irreversible consequence and grants none of
selection, confirmation, identity, ownership, power, probe, or digest
authority. The complete authorization boundary is defined by
[state reconciliation](../state-reconciliation.md#confirmation-and-authorization).

`--verbose` adds safe structured progress and troubleshooting detail. It never
reveals secret values or digests, disables redaction or `no_log`, forwards raw
adapter output, changes a decision, or weakens a log bound. Lifecycle verbose
detail remains in the private attempt log; preflight verbose detail may be
presented only after structural redaction.

`--trust-on-first-use=true` permits bounded retrieval and presentation of an
unknown SSH host key from the exact authorized endpoint. It never accepts,
persists, or uses the key. The preflight fails `trust.identity` and names the
exact `machine trust` request needed for explicit enrollment. A changed key
always fails. `machine trust --replace` is the only interface for a deliberately
changed key, and every replacement name must also be in that command's selected
Machine set.

`--sensitive` authorizes materialization, not disclosure in normal output. It
applies only to the render rows that list it. Sensitive installer artifacts use
the exact verified destination boundary, mode `0600` beneath `0700`
directories, atomic publication, and bounded cleanup in [security](../security.md).
It never permits a secret in an effective-state artifact, manifest, diagnostic,
log, command argument, or path. `render --input-dir` always uses placeholders
and requires `--output-dir`. A non-empty `--context` or
`--sensitive=true` conflicts with `--input-dir`.

`machine list --silent=true` emits only sorted names as text and conflicts
with a resolved `--output json` and with `--power`, because a reading it could
never print would contact every management controller for nothing. Each
conflict is `cli.usage` with exit `2` and the normal JSON failure envelope;
`--silent=false` permits both. These relationships use final scalar values and
follow explicit-help precedence.

`machine start`, `machine stop` and `machine restart` each resolve one exact
Machine and converge it to the power state their name means, through
[the Machine's own management controller](../substrates.md#identity-and-power-operations).
`--force` cuts the power instead of asking the operating system to shut down,
and is accepted only by `stop` and `restart`, which interrupt a running system;
those two also take the ordinary confirmation, while `start` interrupts nothing
and takes neither. A `stop` or `restart` with a resolved `--output json`
requires `--yes`; without it the invocation is `cli.usage` with exit `2` and
acts on nothing.

`status --watch` is text-only and conflicts with JSON. A valid
`--watch-interval` without `--watch` is accepted but has no effect; with watch,
a zero or negative duration resolves to the `5s` default. A
`machine trust --output json` invocation with pending trust-store writes
requires `--yes`; without it the invocation must use `--dry-run` or fails
without writing.

`media add --from-url` requires `--sha256`; the digest is optional verification
for `--from-file`. URL userinfo and an unverifiable digest are rejected.
Redirects are disabled unless the media-import port explicitly permits them;
every permitted hop is revalidated under [security](../security.md).

`<filename.iso>` is one portable ASCII basename of 5 through 255 bytes with an
exact lowercase `.iso` suffix. Its stem begins and ends with an ASCII
alphanumeric character and otherwise contains only ASCII alphanumerics, `.`,
`_`, or `-`. A case-insensitive stem equal to `CON`, `PRN`, `AUX`, `NUL`,
`COM1` through `COM9`, or `LPT1` through `LPT9` is invalid.

`--watch-interval` uses Go `time.ParseDuration` syntax. A non-empty
`--sha256` is 64 case-insensitive hexadecimal digits, optionally prefixed by
exact lowercase `sha256:`, and normalizes to lowercase hexadecimal.

`add-ons add --name <name>:<version>` conflicts with a non-empty `--version`.
An explicitly empty `--version=` is absence. Omission of both version forms
selects the catalog's declared default. Add-on registration uses only an
embedded, content-identified catalog release; these flags do not accept a path,
URL, or arbitrary package.

`cluster info --secrets=true` requires an explicitly supplied, non-empty
`--name`; the default-all selection applies only when sensitive values are not
requested.

The [Secrets contract](../secrets.md#acquisition-and-commands) owns the exact
type/source flag matrix, whole-version validation, generation and reveal parts.
Stdin replacement requires `--yes` before reading. Rotation atomically preserves
every active and bound logical version under the selected implementation.

Secret, media, and add-on mutation obeys the immutable binding and external-
content rules in state reconciliation. A set, renewal, replacement, or
deletion refuses while an incomplete operation or completed-apply snapshot
needs the existing version, unless that exact version remains durably
available for continuation and destroy.
