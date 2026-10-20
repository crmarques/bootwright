# CLI Commands and Flags

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
| `-o, --output text\|json\|yaml` | format; `text` | Select human output or one structured envelope. JSON and YAML use the same CLI-owned result DTO; existing JSON schemas remain stable. [Stream exceptions](output.md#streams-and-exit-status) preserve raw default exports and refuse structured live SSH sessions before dispatch. |
| `--color auto\|always\|never` | enum; `auto` | Style human output using injected terminal capabilities; `auto` respects the composition root's no-color and dumb-terminal facts. Structured output never contains ANSI. |
| `--progress auto\|always\|never` | enum; `auto` | Select automatic terminal redraw, explicitly requested redraw, or no progress stream. This changes no operation or durable result. Structured output suppresses progress. |
| `--detail compact\|full` | enum; `compact` | Select concise human facts or the available detailed evidence and access templates. Structured results retain every documented field. |
| `--ascii[=<bool>]` | Boolean; `false` | Use ASCII presentation markers instead of Unicode markers. Raw exports remain byte-exact. |
| `--width <columns>` | integer; `0` | Use automatic width at `0`, or an explicit bound from `24` through `500`, capped by an injected terminal width when available. Wrapping measures terminal cells. |
| `--context <name>` | context name; current context | Select the named context for a context-backed command. `setup` and `preflight controller` consume it as the [Controller journeys](../controller.md#the-setup-flow) define, and the [flag relationships](#flag-relationships-and-safeguards) below restrict it. |
| `--ssh-id-file <path>` | path; none | Offer this private key first for an SSH operation; a declared credential remains the fallback. A leading `~` resolves from the invoking account database, not an untrusted `HOME`; the opened file must satisfy the private-file rules in [security](../security.md). |
| `--ssh-user <name>` | POSIX user name; none | Use one explicitly borrowed account for eligible OS-ready machines. On [`machine rsh` and `machine exec`](../cli.md#machine-ssh-sessions) it reaches any account under the session's credential rules. It does not alter desired state or the managed identity Bootwright installs. |
| `--ssh-ask-sudo-password[=<bool>]` | Boolean; `false` | Prompt once for the borrowed account's sudo password, hold it only in bounded memory for this invocation, and never place it in arguments, environment, state, output, or logs. It conflicts with JSON or YAML output and non-interactive execution, and is not consumed by `machine rsh` or `machine exec`. |
| `--ssh-user-for-provisioned[=<bool>]` | Boolean; `false` | Extend `--ssh-user` to Bootwright-provisioned machines. It requires `--ssh-user`; the same frozen account must pass the managed-OS ownership probe. It is not consumed by `machine rsh` or `machine exec`, which reach a provisioned Machine's accounts through `--ssh-user` alone. |

Every command also accepts `-h` and `--help`. Help performs no desired-state
discovery, state lookup, secret access, privilege escalation, process launch,
network access, or write after the command path and flag syntax are resolved.

No command takes a proxy flag; which commands read the invoking environment's
route is owned by the
[context-free acquisition route](../controller.md#the-context-free-acquisition-route).
After surrounding whitespace is trimmed, an SSH user matches
`^[a-z_][a-z0-9_-]{0,31}$` exactly.

Context-independent validation of inherited flags always checks an explicitly
non-empty `--context` against the context-name DNS-label grammar, an explicit
`--ssh-user` against the trimmed grammar above,
`--ssh-user-for-provisioned=true` for its required non-empty `--ssh-user`, and
`--ssh-ask-sudo-password=true` for its conflict with JSON or YAML output. TTY detection,
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
| `bootwright context delete` | required `--name <name>` and `--purge`; `--allow-orphans` and `--yes` false | permanent local deletion summary, saying whether it abandoned objects, which its confirmation names with `bootwright status --context <name>` as their inventory or says cannot be listed, and the host reservation keys it released | guarded removal under [permanent deletion](../contexts.md#permanent-deletion); never resource mutation |
| `bootwright add-ons list` | none | built-in catalog and machine-local registrations | read embedded and local catalog state |
| `bootwright add-ons add` | required `--name <name>[:<version>]`; `--version <version>` default catalog default; `--yes` false | registered immutable catalog release | local add-on registration |
| `bootwright add-ons delete` | required `--name <name>[:<version>]`; `--yes` false | removed matching registration | local add-on registration deletion |
| `bootwright secret set` | required `--name`; type-specific `--value-file`, `--value-stdin`, `--username`, `--password-file`, `--password-stdin`, `--certificate-file`, `--private-key-file`, `--public-key-file` under [Secrets](../secrets.md); `--yes` false | stored identity and parts, never values | local confidential write |
| `bootwright secret generate` | optional `--name`; `--renew` false | changed and unchanged Secret names | atomic selected generated-material batch |
| `bootwright secret check` | none | declared availability/type checks | bounded material reads; no values emitted |
| `bootwright secret list` | none | context secret identities, types, parts, and availability | read confidential-store metadata |
| `bootwright secret info` | `--name <list>` default all; an unknown name refuses | selected secret identity, parts, state, version and binding metadata | read confidential-store metadata only; no material read or availability check |
| `bootwright secret show` | required `--name` and `--part value\|username\|password\|certificate\|private-key\|public-key` | exact selected sensitive bytes | read and reveal current declared part |
| `bootwright secret delete` | required `--name <name>`; `--yes` false | deleted identity, or nothing deleted | local logical deletion, preserving bound versions |
| `bootwright secret encryption init` | none | configured implementation and active key | idempotent initialization using Context configuration |
| `bootwright secret encryption status` | none | keyring and encrypted-store status | read confidential metadata |
| `bootwright secret encryption rotate` | `--yes` false | new active key, retired keys and re-encrypted version and part counts | atomic local key rotation and re-encryption |
| `bootwright media add` | required `--name <filename.iso>` and exactly one of `--from-file <path>` or `--from-url <https-url>`; `--sha256 <digest>`; `--yes` false | stored media identity, size, and verified digest | local copy or bounded download over the [context-free route](../controller.md#the-context-free-acquisition-route) and atomic publication |
| `bootwright media list` | `--checksums` false | media names, sizes, recorded digests and reserving contexts, and with `--checksums` each computed digest and its verification | read local media; `--checksums` reads each image in full |
| `bootwright media delete` | required `--name <filename.iso>`; `--yes` false | deleted media identity | local media deletion when not frozen by an operation |

The required controller declaration and context-free setup boundary follow
[controller command applicability](../cli.md#controller-declaration-and-command-applicability).
The three `media` commands follow the host-wide
[media store](../managed-os.md#media-store).

### Inspect and lifecycle commands

| Invocation | Local flags and defaults | Successful result | Effects |
| --- | --- | --- | --- |
| `bootwright validate` | repeatable `-f, --file <file-or-dir>` | validation summary with admission counts, exclusions and advisories | read the supplied input, or the selected context's input when `-f` is omitted; no write |
| `bootwright preflight controller` | none | [baseline or explicit-context controller readiness](../controller.md#the-setup-flow), including the acquisition route it resolved | bounded local observation; no state publication |
| `bootwright preflight infra` | `--clusters <list>` default all; `--dry-run` false; `--trust-on-first-use=<bool>` default `true` | infrastructure readiness checks | observe unless `--dry-run`, which is local-only |
| `bootwright preflight clusters` | same flags as `preflight infra` | all selected cluster readiness checks | observe unless `--dry-run` |
| `bootwright preflight container-cluster` | same flags as `preflight infra`, with ContainerCluster-only selection | container-cluster readiness checks | observe unless `--dry-run` |
| `bootwright preflight storage-cluster` | same flags as `preflight infra`, with StorageCluster-only selection | storage-cluster readiness checks | observe unless `--dry-run` |
| `bootwright preflight add-ons` | `--clusters <list>` default all ContainerClusters | add-on prerequisite checks | bounded observation |
| `bootwright preflight all` | `--dry-run` false; `--trust-on-first-use=<bool>` default `true` | all controller, infrastructure, cluster, storage, and add-on checks | observe unless `--dry-run` |
| `bootwright plan` | `--stage <list>` default all stages | next legal full-context plan or exact continuation point, with the steps each block waits for, how wide the plan is and how many blocks this build starts at a time, and the blocks a stage selection would start | none |
| `bootwright status` | none | context readiness, lifecycle state, and next safe commands | read local state |
| `bootwright diff` | `--observe` false | each object's desired state beside what the context's apply realized, from its records or, with `--observe`, read live, as a [comparison](../cli.md#desired-and-realized-state-comparison) | read local state; with `--observe`, a bounded read-only observation of each block the context's apply realized, on the host that block froze, after the store-internal [cache-only rebuild](../controller.md#the-controller-runtime) of this build's runtime when its directory is missing |
| `bootwright render` | `--input-dir <file-or-dir>`; `--output-dir <dir>`; `--clusters <list>` default all; `--sensitive` false | whole external-tool artifact manifest, or render help when neither path flag is supplied | local artifact writes only |
| `bootwright render effective` | none | normalized effective desired state and object counts | none |
| `bootwright render installer` | `--clusters <list>` default all ContainerClusters; `--sensitive` false | installer artifact manifest | local placeholder files and optional sensitive files |
| `bootwright render storage` | `--clusters <list>` default all StorageClusters | storage artifact manifest | local native files or scripts only; never execution |
| `bootwright apply` | repeatable `--authorize <token>[,<token>...]`; `--stage <list>` default all stages; `--yes` false | completed full apply, a pause at the selected stage boundary, exact continued state, or a settled result when the declared state is already realized | complete planned mutation, or none when it settles, beyond completing an operation's interrupted finalization (its record, releases and evidence) |
| `bootwright destroy` | repeatable `--authorize <token>[,<token>...]`; repeatable `--unregistered <machine>[,<machine>...]`; `--yes` false | completed full destroy, exact continued state, or a settled result when the context owns nothing | complete planned removal, or none when it settles, beyond completing an operation's interrupted finalization (its record, releases and evidence) or releasing what an interrupted registration left, and the [housekeeping](../state-reconciliation.md#context-mutation-evidence) of the operation claims that hold nothing; over an apply that did not complete, it first proves the outcome of every effect it takes back |

### Resource, access, and general commands

| Invocation | Local flags and defaults | Successful result | Effects |
| --- | --- | --- | --- |
| `bootwright machine list` | `--clusters <list>` default all; `--power-status` false; `--silent` false | Machines with their declared contact and addresses and the evidence-backed lifecycle position each reached, their controller-reported power with `--power-status`, or sorted names with `--silent` | read local state; with `--power-status`, read each machine's power through its management controller |
| `bootwright machine info` | `--name <list>` default all; an unknown name refuses | selected machine placement, contact, addresses, and recorded lifecycle evidence | read local inventory only; no host or power contact |
| `bootwright machine rsh` | required `--name <machine>` | an interactive SSH session on the exact Machine as its resolved identity; the client's exit status is the result | read state, open the session's credential for its duration, read or record host trust, run one pinned SSH client |
| `bootwright machine exec` | required `--name <list>` and `<command>...` | the exact argument vector runs on the Machine, or on every listed Machine at once with [each line prefixed by its Machine](../cli.md#machine-ssh-sessions); the remote command's exit status is the result, the highest of them for several | same |
| `bootwright machine trust` | `--machines <list>` default all; `--replace <list>` default none; `--dry-run` false; `--yes` false | exact host-key trust plan, and the recorded result unless dry-run | bounded SSH host-key observation of the selected Machines; context trust-store write unless dry-run |
| `bootwright machine start` | required `--name <list>` | the power state each Machine's management controller proved once the operation settled | bounded power operation through that controller, one concurrent [batch](../cli.md#machine-power-operations) for several Machines |
| `bootwright machine stop` | required `--name <list>`; `--force` false; `--yes` false | same, after the operating system is asked to shut down | same |
| `bootwright machine restart` | required `--name <list>`; `--force` false; `--yes` false | same, after a proved stop and a proved start | same |
| `bootwright setup` | `--dry-run` false; `--yes` false; `--wheel-mirror <file:///dir or https://base>` default none; `--download-only` false; `--dest-dir <dir>` default none | [context-independent controller prerequisite plan or completed setup](../controller.md#the-setup-flow); with `--dry-run --output json`, the plan and every locked wheel; with `--download-only`, the [files written for an offline host](../controller.md#offline-and-mirrored-wheels) | host packages through the host's own dnf after its verbatim preview, and the controller runtime from locked wheels over the [context-free route](../controller.md#the-context-free-acquisition-route), or from `--wheel-mirror`; a ready host makes no request and no change; dry-run only lists requirement names; `--download-only` requires `--dest-dir`, conflicts with `--dry-run`, never elevates, reads no setup state and writes only the directory |
| `bootwright cluster list` | none | container and storage cluster names and kinds in canonical order | read local state |
| `bootwright cluster info` | `--name <list>` default all unless `--secrets`, which requires exactly one cluster; `--secrets` false | cluster kinds, endpoints, access-command applicability and availability, artifact availability, for a managed Ceph cluster its release, FSID, frozen image, monitors, administrator command and dashboard access, and optional explicit sensitive values | read local state and optional confidential material |
| `bootwright cluster rsh` | required `--name <cluster>` (one cluster); `--node <node>` default first node in canonical name order (at most one) | an interactive SSH session on the resolved node's Machine under the [cluster session contract](../cli.md#cluster-sessions); the client's exit status is the result | same as `machine rsh` |
| `bootwright cluster exec` | required `--name <list>`; `--node <list>` default each cluster's first node in canonical name order; required `<command>...` | the exact argument vector runs on the resolved node's Machine, or on every resolved Machine at once with [each line prefixed by its Machine](../cli.md#several-clusters-and-nodes), under the [cluster session contract](../cli.md#cluster-sessions); the remote command's exit status is the result, the highest of them for several | same as `machine exec` |
| `bootwright cluster oc` | required `--name <cluster>` and non-empty `<command>...` | bounded `oc` handoff descriptor | read context-owned access metadata only |
| `bootwright cluster kubectl` | required `--name <cluster>` and non-empty `<command>...` | bounded `kubectl` handoff descriptor | read context-owned access metadata only |
| `bootwright cluster kubeconfig` | required `--name <cluster>` | kubeconfig through the explicit sensitive-output boundary | read and reveal one context-owned credential artifact |
| `bootwright version` | none | version, commit, source state, Go runtime and target, and embedded dependency-bundle identity | none |
| `bootwright help [command ...]` | no local flags | help for the exact resolved command, human or structured | none |
| `bootwright completion bash` | `--no-descriptions` false | Bash completion script | none |
| `bootwright completion zsh` | `--no-descriptions` false | Zsh completion script | none |
| `bootwright completion fish` | `--no-descriptions` false | Fish completion script | none |
| `bootwright completion powershell` | `--no-descriptions` false | PowerShell completion script | none |

### Cluster command applicability

`--name` selects from the shared ContainerCluster/StorageCluster name namespace
in the selected context; that of `cluster info` and `cluster exec` is a
comma-separated list under the [list rules](../cli.md#parsing-and-input-conventions). The following table owns applicability. Target
resolution, readiness and diagnostics of `cluster kubeconfig` live in the
[administrator access export](../cli.md#administrator-access-export); those of
`cluster rsh` and `cluster exec` in the
[cluster sessions](../cli.md#cluster-sessions); those of `cluster info
--secrets` on a managed Ceph cluster in the
[storage dashboard access](../cli.md#storage-dashboard-access); those of
`cluster oc` and `cluster kubectl` are
[deferred](../deferred/cli-access-and-rendering.md#containercluster-access-handoffs-and-sessions).

| Command | Applicable targets |
| --- | --- |
| `cluster list`, `cluster info` | All selected ContainerClusters and StorageClusters, including external storage clusters. |
| `cluster rsh`, `cluster exec` | OpenShift/OKD ContainerClusters and managed Ceph StorageClusters with declared nodes. External StorageClusters have no declared node roster and are inapplicable. |
| `cluster oc`, `cluster kubectl`, `cluster kubeconfig` | OpenShift/OKD ContainerClusters only. |

Applicability does not establish implementation availability, access readiness,
identity, or ownership. Missing access material on an applicable target is an
access failure, not a change in applicability. A session on an OpenShift/OKD
ContainerCluster is applicable and refuses `access.unavailable`: only managed
Ceph sessions are specified.

Not yet met: `cluster oc` and `cluster kubectl` hand off nothing and sessions on
an OpenShift/OKD ContainerCluster do not open; tracked as
[B101](../milestones/backlog.md#b101).

### Access help

The short description and detailed help for `cluster oc` and `cluster kubectl`
state that success prints an access descriptor for independent operator
execution and does not launch a client or connect. Cluster command help
includes its applicable target kinds and variants from the table above.
`cluster kubeconfig` help states that success exports raw sensitive bytes to
standard output.

The short description and detailed help for `machine rsh`, `machine exec`,
`cluster rsh` and `cluster exec` state that the session runs on the Machine,
for a cluster the resolved node's, as its resolved identity, that an unproved
host key is confirmed interactively or refused, and that the exit status is the
client's. The cluster commands also state how `--node` selects: by node name,
then by FQDN, defaulting to the first node in name order.

The detailed help of `cluster exec` states that `--name` and `--node` take
comma-separated lists and that the command runs on every resolved Machine at
once; that of `cluster info` states that `--name` takes a list and that
`--secrets` requires exactly one cluster.

The detailed help of `machine start`, `machine stop`, `machine restart` and
`machine exec` states that `--name` takes a comma-separated list and that the
command applies to every listed Machine at once.

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
    Go                 go1.26.9
    Target             linux/amd64
    Dependency bundle  sha256:<64 hexadecimal digits>
```

The composition root supplies every value. It obtains the Go runtime, GOOS, and
GOARCH from the linked Go runtime, the dependency bundle as the digest of the
automation the executable embeds, the one `setup` publishes and every
operation records, and combines them with the link-supplied version, commit and
source-state values (`TestCompositionSuppliesRuntimeBuildInformation`). For a version,
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
single directory named by the completed prefix, never recursively, taking its
entries in the directory's own order. It reads at most 4096 of those entries
and stops once it holds 256 candidates, so a larger directory may offer only
some of its matching entries. A prefix longer than 4096 bytes, or one carrying
a NUL, carriage return or line feed, reads nothing and offers no candidate. A
directory-valued flag offers directories; a file-valued flag offers files and
directories, so a path can be completed one segment at a time. A directory
candidate carries a trailing separator, and a request with at least one
directory candidate reports that the shell must append nothing of its own; a
request that offers only files leaves the shell its own space, so a completed
file ends the word. A dot entry is offered only once the prefix
names one, and an entry whose name is longer than 255 bytes, or which carries a
control character, whitespace, or a character significant to a shell word, is
withheld rather than escaped, because such a candidate could not be inserted
unchanged. Those characters include `@` and `=`: Bash completion splits a word
at either,
[Zsh expands](https://zsh.sourceforge.io/Doc/Release/Expansion.html#g_t_0060_003d_0027-expansion)
a word that starts with `=`, and
[PowerShell splats](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_splatting)
one that starts with `@`. An unreadable directory, or a read of it that fails,
yields no candidates and no diagnostic. Completion offers no dynamic candidate
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

`--output` is inherited and accepts exactly `text`, `json`, or `yaml`; `-o`
is its shorthand and `--format` is not an alias. It is valid before or after a
command path. Every command supports one structured document, including help,
version, completion scripts, plan and lifecycle outcomes. Live SSH sessions
require `text` and refuse structured modes with `cli.usage` before dispatch,
privilege or session effects. Default raw credential exports and effective YAML
retain their exact bytes; explicit structured exports use the
[documented envelope](output.md#json-output).

`--yes` suppresses only the named command's
[ordinary confirmation](../cli.md#ordinary-confirmation) after target
selection and every independent safeguard and authorization decidable from
durable local state succeeds; a proof that needs the exclusive lock or a remote
observation, such as `destroy`'s resolution and quiescence gate, follows the
prompt and still refuses before registration or any effect. It may
confirm a safe command-owned overwrite, recreation, or replacement, but it
does not itself select a target or authorize data loss,
a changed or unknown identity, a failed probe, or another named risk. If a
prompt remains necessary, the command fails instead when standard input is
non-interactive, JSON or YAML output is selected, or a safe answer cannot be read.

`context delete` requires `--purge` to resolve to `true`; omission or
`--purge=false` fails without changing state. `--yes` independently controls
its ordinary confirmation, and `--allow-orphans` replaces neither. What each
flag acknowledges, and when deletion refuses, follow
[permanent deletion](../contexts.md#permanent-deletion). A deletion that
abandons objects warns once on standard error with `context.orphaned`.

`--stage` accepts only `controller`, `infra-components`, `substrates`,
`machines`, `clusters`, and `add-ons`, and only on `plan` and `apply`. Its
members follow the [comma-list rules](../cli.md#parsing-and-input-conventions);
a supplied value that resolves to no member and an unrecognized member are both
usage errors. Omission selects every stage. What a selection gates is owned by
[state reconciliation](../state-reconciliation.md#stages-and-the-pause-boundary).

`--authorize` accepts only `data-loss`, and only on `apply` and `destroy`.
Unknown, empty, or duplicate tokens are usage errors; `all` is not accepted.
Whitespace around comma-separated tokens is ignored. A token grants no
confirmation authority either: it neither replaces nor implies
[ordinary confirmation](../cli.md#ordinary-confirmation). Whether the frozen plan
requires the token, what it acknowledges and the refusal of a token it does not
require are owned by
[state reconciliation](../state-reconciliation.md#confirmation-and-authorization).

`--unregistered` names a Machine whose subscription consumer the operator
removed with the subscription service's own tools, and is accepted only on
`destroy`. On that invocation the Machine's installation neither reads nor
unregisters its guest and records no registration, as
[managed OS](../managed-os.md#registration) states; nothing persists the name,
and only the `lifecycle.authorization` refusal of a missing or inapplicable
token carries it: its command repeats the invocation's own `--unregistered`
names, sorted and joined by commas, after the tokens the plan requires, so the
manual unregistration's printed next steps complete a destroy whose plan
consumes `data-loss`. A name whose installation the destroy plan does
not unregister refuses `lifecycle.authorization` before confirmation and
registration: `this destroy removes no subscription registration from
Machine/<name>, so --unregistered <name> acknowledges nothing`, remedied by
`repeat <command> without --unregistered <name>`, where `<command>` is the
destroy with the tokens its plan requires. An empty occurrence or an empty
comma-separated member is a usage error; whitespace around members is ignored.
It grants no confirmation authority either.

A changed SSH host key always fails. `machine trust --replace` is the only
interface for a deliberately changed key, and every replacement name must also
be in that command's selected Machine set. The preflight `--trust-on-first-use`
flag's meaning is [deferred](../deferred/cli-access-and-rendering.md#preflight).

`--sensitive` authorizes materialization, not disclosure in normal output. It
applies only to the render rows that list it. Sensitive installer artifacts use
the exact verified destination boundary, mode `0600` beneath `0700`
directories, atomic publication, and bounded cleanup in [security](../security.md).
It never permits a secret in an effective-state artifact, manifest, diagnostic,
log, command argument, or path. `render --input-dir` always uses placeholders
and requires `--output-dir`. A non-empty `--context` or
`--sensitive=true` conflicts with `--input-dir`. Context-backed whole render
requires `--output-dir` and `--sensitive`.

`machine list --silent=true` emits only sorted names as text and conflicts
with resolved JSON or YAML output and with `--power-status`, because a reading it
could never print would contact every management controller for nothing. Each
conflict is `cli.usage` with exit `2` and the normal selected structured failure envelope;
`--silent=false` permits both. These relationships use final scalar values and
follow explicit-help precedence.

`machine start`, `machine stop` and `machine restart` each resolve every exact
Machine `--name` lists, and converge each to the power state their name means,
through [the Machine's own management controller](../substrates.md#identity-and-power-operations).
`--name` of these three and of `machine exec` is a comma-separated list under
the [list rules](../cli.md#parsing-and-input-conventions); `machine rsh` takes
one Machine.
`--force` cuts the power instead of asking the operating system to shut down,
and is accepted only by `stop` and `restart`, which interrupt a running system;
those two also take the ordinary confirmation, while `start` interrupts nothing
and takes neither. A `stop` or `restart` with resolved JSON or YAML output
requires `--yes`; without it the invocation is `cli.usage` with exit `2` and
acts on nothing.

A `machine trust` invocation selecting JSON or YAML with pending trust-store writes
requires `--yes`; without it the invocation must use `--dry-run` or fails
without writing.

`media add --from-url` requires `--sha256`; the digest is optional verification
for `--from-file`. A plain `http://` URL, URL userinfo, a fragment, a control
character in either source, an origin over 512 bytes as the
[media store](../managed-os.md#media-store) defines it, and an unverifiable
digest are rejected. A redirect is never followed: it refuses, naming its
status and its target.

`<filename.iso>` is one portable ASCII basename of 5 through 250 bytes with an
exact lowercase `.iso` suffix, so its record name, the image name followed by
`.json`, fits one 255-byte file name. Its stem begins and ends with an ASCII
alphanumeric character and otherwise contains only ASCII alphanumerics, `.`,
`_`, or `-`. A case-insensitive stem equal to `CON`, `PRN`, `AUX`, `NUL`,
`COM1` through `COM9`, or `LPT1` through `LPT9` is invalid.

A non-empty `--sha256` is 64 case-insensitive hexadecimal digits, optionally
prefixed by exact lowercase `sha256:`, and normalizes to lowercase hexadecimal.

`add-ons add --name <name>:<version>` conflicts with a non-empty `--version`.
An explicitly empty `--version=` is absence. Omission of both version forms
selects the catalog's declared default. Add-on registration uses only an
embedded, content-identified catalog release; these flags do not accept a path,
URL, or arbitrary package.

`cluster info --secrets=true` requires an explicitly supplied `--name` naming
exactly one cluster after list normalization, and a list of several is
`cli.usage`; the default-all selection applies only when sensitive values are
not requested.

The [Secrets contract](../secrets.md#acquisition-and-commands) owns the exact
type/source flag matrix, whole-version validation, generation and reveal parts.
Stdin replacement requires `--yes` before reading. Rotation atomically preserves
every active and bound logical version under the selected implementation.

Secret, media, and add-on mutation obeys the immutable binding and external-
content rules in state reconciliation. A set, renewal, replacement, or
deletion refuses while an incomplete operation or completed-apply snapshot
needs the existing version, unless that exact version remains durably
available for continuation and destroy.
