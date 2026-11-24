# CLI Commands and Flags

Read with [CLI behavior](../cli.md) and [output contracts](output.md).
The tables define the complete public command tree, operands, flags and defaults.

## Global flags

Every command accepts and parses the presentation flags below. `--context`
and the `--ssh-*` flags are accepted only by the commands whose use case reads
them; any other command refuses them as flags it does not accept. The
context-free commands, which refuse `--context`, are `install`, `uninstall`, `rebind-host`, `catalog` and `context` (all subcommands), `help`, `completion`, `version` and `sbom`; only `apply`, `destroy`,
`machine rsh`, `machine exec`, `machine cp`, `cluster rsh`, `cluster exec` accept
`--ssh-*`. `machine rsh`, `machine exec`, `cluster rsh` and `cluster exec`
accept `--ssh-id-file`, `--ssh-user` and `--ssh-ask-password`. `machine cp`
accepts `--ssh-id-file` and `--ssh-user`. `machine enroll` accepts `--ssh-id-file`. Help and completion list each
command's supported flags. A command resolves a taken global value only when its use case
consumes it. An unused value grants no authority, performs no discovery or
prompt, and changes no result; its syntax and context-independent safety
checks still apply. `TestInheritedFlagScopeIsTheFlagsEachCommandReads` derives
the flags each command reads from its dispatch and holds these lists, help and
completion to them.

| Flag | Type and default | Contract |
| --- | --- | --- |
| `-o, --output text\|json\|yaml` | format; `text` | Select human output or one structured envelope. JSON and YAML use the same CLI-owned result DTO; existing JSON schemas remain stable. [Stream exceptions](output.md#streams-and-exit-status) preserve raw default exports and refuse structured live SSH sessions before dispatch. |
| `--color auto\|always\|never` | enum; `auto` | Style human output using injected terminal capabilities; `auto` respects the composition root's no-color and dumb-terminal facts. Structured output never contains ANSI. |
| `--progress auto\|always\|never` | enum; `auto` | Select automatic terminal redraw, explicitly requested redraw, or no progress stream. This changes no operation or durable result. Structured output suppresses progress. |
| `--detail compact\|full` | enum; `compact` | Select concise human facts or the available detailed evidence and access templates. Structured results retain every documented field. |
| `--ascii[=<bool>]` | Boolean; `false` | Use ASCII presentation markers instead of Unicode markers. Raw exports remain byte-exact. |
| `--width <columns>` | integer; `0` | Use automatic width at `0`, or an explicit bound from `24` through `500`, capped by an injected terminal width when available. Wrapping measures terminal cells. |
| `--context <name>` | context name; current context | Select the named context for a context-backed command. The [flag relationships](#flag-relationships-and-safeguards) below restrict it. |
| `--ssh-id-file <path>` | path; none | Offer this private key first for an SSH operation; a declared credential remains the fallback. A leading `~` resolves from the invoking account database, not an untrusted `HOME`; the opened file must satisfy the private-file rules in [security](../security.md). |
| `--ssh-user <name>` | POSIX user name; none | Use one explicitly borrowed account for eligible OS-ready machines. On [`machine rsh` and `machine exec`](../cli.md#machine-ssh-sessions) it reaches any account under the session's credential rules. It does not alter desired state or the managed identity Bootwright installs. |
| `--ssh-ask-password[=<bool>]` | Boolean; `false` | Prompt for the remote SSH login password through OpenSSH. Requires `--ssh-user`, one target and a terminal. Conflicts with `--ssh-id-file`. |
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

Context-independent validation of the inherited flags a command takes always checks an explicitly
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
process or network access, and one local write: the store-internal
[cache-only rebuild](../controller.md#the-controller-runtime) of this build's
controller runtime when its directory is missing, for the commands whose cell
names it. That rebuild waits for `controller/install/lock`, uses no network and changes no
setup record. “Mutate” permits only the named, planned effects.

An *asked* command is one whose cell names a Mutate effect (a planned mutation
or removal, host packages, a download, SSH, a power operation, a host identity
rewrite), reveals a secret, or deletes or rotates local state irreversibly
(`context delete`, `secret delete`, `secret encryption rotate`, `media
delete`); an agent runs it only when the owner asks, and
`TestDocsAskRulesCoverEveryEffectfulCommand` keeps the tracked
`.claude/settings.json` and `.codex/rules/bootwright.rules` complete against
these cells.

The Effects column is the only owner of what a command may do. Another page
describes a command's effects only by projecting its cell and defines no other
class. Read a cell by invocation: its leading clause states the effects with
flags omitted; a clause naming a flag states what that flag adds or replaces.
The run-nothing set is invocations whose applicable clause is exactly `none`
or `read local state` and whose selected flag clauses add no write, payload
read, process, network or Secret operation. Keyring metadata, media records
and entry-existence checks are local state. Security pages cite this rule.

Selectors and flags that play one role share one spelling. `--name <list>`
selects instances of the command's own noun, a plural `--<noun>s <list>` filters
by a related noun, and `--short` is the one flag that prints names only.

`TestCatalogFlagSpellingsFollowTheSelectorRules` holds the catalog to these rules.

### Setup commands

| Invocation | Local flags and defaults | Successful result | Effects |
| --- | --- | --- | --- |
| `bootwright context init` | `--check` false; required `--name <name>`; optional `-f, --file <context.yaml>` and `--input-dir <dir>` | initialized context and user selection; optional imported-input summary | local context and keyring creation; with `--check`, bounded admission reads |
| `bootwright context update` | `--check` false; required `--name <name>`; at least one of `-f, --file <context.yaml>` or `--input-dir <dir>`; `--yes` false | unchanged configuration or imported-input summary | local atomic input replacement; with `--check`, bounded admission reads |
| `bootwright context use` | required `--name <name>` | selected-current-context summary | local current-context update |
| `bootwright policy install` | required `-f, --file <file>` | updated context | atomically replace the context authorization policy |
| `bootwright policy permissions` | none | the caller's granted commands and targets | read the selected context and resolve subjects through NSS |
| `bootwright context list` | none | contexts in canonical name order | read local state |
| `bootwright context current` | `--short` false | current context details, or only its name with `--short` | read local state |
| `bootwright context delete` | required `--name <name>` and `--purge`; `--allow-orphans` and `--yes` false | permanent local deletion summary, saying whether it abandoned objects, which its confirmation names by the inventory [permanent deletion](../contexts.md#permanent-deletion) defines or says cannot be listed, and the host reservation keys it released | guarded removal under [permanent deletion](../contexts.md#permanent-deletion); never resource mutation |
| `bootwright catalog add-ons list` | none | built-in packages, their capabilities, OLM package, default source and configuration members | read executable registry |
| `bootwright catalog add-ons info` | required `--name <package>`; an unknown name refuses | [one package described](../add-ons.md#the-package-listing) | read executable registry |
| `bootwright secret set` | required `--name`; type-specific `--value-file`, `--value-stdin`, `--username`, `--password-file`, `--password-stdin`, `--certificate-file`, `--private-key-file`, `--public-key-file` under [Secrets](../secrets.md); `--yes` false | stored identity and parts, never values | local confidential write |
| `bootwright secret generate` | optional `--name`; `--renew` false; `--yes` false | changed and unchanged Secret names | atomic selected generated-material batch |
| `bootwright secret check` | none | declared availability/type checks | bounded material reads; no values emitted |
| `bootwright secret list` | none | context secret identities, types, parts, and availability | read confidential-store metadata |
| `bootwright secret info` | `--name <list>` default all; an unknown name refuses | selected secret identity, parts, state, version and binding metadata | read confidential-store metadata only; no material read or availability check |
| `bootwright secret show` | required `--name` and `--part value\|username\|password\|certificate\|private-key\|public-key` | exact selected sensitive bytes | read and reveal current declared part |
| `bootwright secret delete` | required `--name <name>`; `--yes` false | deleted identity, or nothing deleted | local logical deletion, preserving bound versions |
| `bootwright secret encryption init` | none | configured implementation and active key | idempotent initialization using Context configuration |
| `bootwright secret encryption status` | none | keyring and encrypted-store status | read confidential metadata |
| `bootwright secret encryption rotate` | `--yes` false | new active key, retired keys and re-encrypted version and part counts | atomic local key rotation and re-encryption |
| `bootwright catalog media add` | required `--name <filename.iso>` and exactly one of `--from-file <path>`, `--from-url <https-url>` or `--from-url-file <path\|->`; `--sha256 <digest>`; `--yes` false | stored media identity, size, and verified digest | local copy or bounded download over the [context-free route](../controller.md#the-context-free-acquisition-route) and atomic publication |
| `bootwright catalog media list` | `--checksums` false | media names, sizes, recorded digests and reserving contexts, and with `--checksums` each computed digest and its verification | read local media; `--checksums` reads each image in full |
| `bootwright catalog media delete` | required `--name <filename.iso>`; `--yes` false | deleted media identity | local media deletion when not frozen by an operation |

The required controller declaration and context-free setup boundary follow
[controller command applicability](../cli.md#controller-declaration-and-command-applicability).
The three `catalog media` commands follow the host-wide
[media store](../managed-os.md#media-store).

### Inspect and lifecycle commands

| Invocation | Local flags and defaults | Successful result | Effects |
| --- | --- | --- | --- |
| `bootwright validate` | repeatable `--input-dir <file-or-dir>` | validation summary with admission counts, exclusions and advisories | read the supplied input, or the selected context's input when `--input-dir` is omitted; no write |
| `bootwright status` | `--paths` false | recorded controller and lifecycle state, current block output, missing prerequisites and next steps; `--paths` includes the context roots and their presence | read local state; keyring metadata and media records use shared locks; with `--paths`, inspect each root entry's existence without reading it |
| `bootwright logs` | `--operation <op-id>` default the current operation; `--block <block-id>`; `--raw` false; `--attempt <N>`; `--follow` false; `--run <run-NNNNNN>`; `--install` the newest install, or `--install=<install-NNNNNN>` | an operation's events as lines, one block's newest retained adapter output with `--raw`, a bounded run's or setup's output, or, with `--follow`, those lines streamed until the operation, run or setup ends, as [reading and following logs](output.md#reading-and-following-logs) states | read local state under short shared root locks and the log root through no-follow handles, `--install` under no root lock; never a lease, never a write |
| `bootwright diff` | `--observe`, `--exit-code` false; `--output-dir <dir>` or `--tool <program>` | desired files beside what the apply lineage realized, from records or, with `--observe`, read live, as a [comparison](../cli.md#desired-and-realized-state-comparison) | read local state; `--output-dir`, `--tool` write files outside the store; `--observe` reads each block the lineage realized, read-only, on the host it froze, after the [cache-only rebuild](../controller.md#the-controller-runtime) of a missing runtime |
| `bootwright render effective` | `--origins` false; `--input-dir <file-or-dir>`; `--output-dir <dir>`; `--sensitive` false | normalized effective desired state and object counts; with `--origins`, the [origin of each effective field](../cli.md#effective-field-origins) instead of the YAML; with `--output-dir`, the [effective-state file](../cli.md#validation-preflight-and-rendering) written there and its path | none without `--output-dir`; with it, local artifact writes only |
| `bootwright render acquisitions` | `--input-dir <file or directory>` | every artifact the selected desired state acquires, its origin, class, digest and Machines ([acquisition list](../cli.md#acquisition-list)) | none |
| `bootwright render installer` | `--clusters <list>` default all OpenShiftClusters; `--sensitive` false | installer artifact manifest | local placeholder files and optional sensitive files |
| `bootwright render storage` | `--clusters <list>` default all CephClusters | storage artifact manifest | local native files or scripts only; never execution |
| `bootwright apply` | `--dry-run` false; repeatable `--authorize <token>[=<digest>][,<token>...]`; `--stage <list>` default all stages; `--clusters <list>` or `--machines <list>`; `--until <cluster>.<package>`; `--parallelism <n>` default 8; `--yes` false | completed full apply or additive apply of instances, a pause at the selected stage boundary or after the named add-on instance, exact continued state, or a settled result when the declared state is realized; with `--dry-run`, the plan and prerequisite checks | complete planned mutation, including automatic disk-inventory preparation before separately authorized installation, or none when it settles, beyond completing an operation's interrupted finalization; with `--dry-run`, bounded read-only observation |
| `bootwright destroy` | `--dry-run` false; `--keep-going` false, continues independently eligible cleanup after a recorded target-local failure; repeatable `--authorize <token>[=<digest>][,<token>...]`, which also takes `erase=<machine>:<method>` to relax one Machine's declared removal erase for this destroy and `abandon=<machine>` to leave one Machine's remaining effects in place; repeatable `--unregistered <machine>[,<machine>...]`; `--parallelism <n>` default 8; `--yes` false | completed full destroy, exact continued state, a settled result when the context owns nothing, or a remaining-cleanup report; with `--dry-run`, the removal plan and prerequisite checks | complete planned removal, or partial removal with `--keep-going`, or none when it settles, beyond the same interrupted finalization or releasing what an interrupted registration left, and the [housekeeping](../state-reconciliation.md#context-mutation-evidence) of the operation claims that hold nothing; over an apply that did not complete, it first proves the outcome of every effect it takes back; with `--dry-run`, bounded read-only observation |

Not yet met: `render installer` and `render storage` run no use case
and answer `cli.not-implemented`; their contracts are intent, tracked as
[B52](../milestones/m7.md#b52) and [B55](../milestones/m7.md#b55).

Every other command here is available.
`TestUnavailableCommandsMatchTheStubWiring` holds this list.

Not yet met: `apply --dry-run` defers add-on prerequisite checks to execution; tracked as [B770](../milestones/backlog.md#b770).

### Resource, access, and general commands

#### Machine commands

| Invocation | Local flags and defaults | Successful result | Effects |
| --- | --- | --- | --- |
| `bootwright machine list` | `--clusters <list>` default all; `--power-status` false; `--short` false | Machines with their declared contact and addresses and the evidence-backed lifecycle position each reached, their management-controller-reported power with `--power-status`, or sorted names with `--short` | read local state; with `--power-status`, read each machine's power through its management controller |
| `bootwright machine info` | `--name <list>` default all; unknown names refuse | [machine details and access identities](output.md#machine-results) | read local inventory |
| `bootwright machine rsh` | required `--name <machine>` | an interactive SSH session on the exact Machine as its resolved identity; the client's exit status is the result | read state, open the session's credential for its duration, read or record host trust, run one pinned SSH client |
| `bootwright machine exec` | required `--name <list>` and `<command>...` | the exact argument vector runs on the Machine, or on every listed Machine concurrently, at most the lifecycle bound at a time, with [each line prefixed by its Machine](../cli.md#machine-ssh-sessions); the remote command's exit status is the result, the highest of them for several | same |
| `bootwright machine cp` | required `--name <list>` and two operands `local:<path> remote:<path>` or the reverse; `--overwrite` false | per-Machine copy outcomes and confirmed file/byte counts; structured output supported | run one pinned SSH client and a fixed file worker; explicit local or remote payload writes under the receiving account; no lifecycle state |
| `bootwright machine enroll` | `--name <machine>` required; `--dry-run` false; `--yes` false | Plan or establish one provided Machine's declared lifecycle SSH login and verify remote sudo | planned mutation of a provided Machine's SSH and sudo account; with dry-run, local state only |
| `bootwright machine trust` | `--name <list>` default all; `--replace <list>` default none, each member `<machine>` or `<machine>=SHA256:<fingerprint>`; `--expect <machine>=SHA256:<fingerprint>[,...]` default none; `--referenced-secret` false; `--host-public-key-file <path>` optional with `--referenced-secret`; `--dry-run` false; `--yes` false | exact host-key trust plan, and the recorded result unless dry-run | bounded SSH host-key observation of the selected Machines; context trust-store or referenced Secret version write unless dry-run |
| `bootwright machine discover` | required `--name <list>`; `--dry-run`, `--yes` false; optional `--stage controller` | BMC or authenticated RAM disk inventory in durable operation records; the next installation plan freezes exactly one eligible disk | planned mutation: a BMC drive-inventory read, or a temporary boot and private media, authenticated inventory, power-off and cleanup; with `--stage controller`, planned controller and infrastructure setup; `--dry-run` reads only local state |
| `bootwright machine inventory` | required `--name <list>` | disk inventories from the current apply lineage, with source, collecting operation and exact capacities | read local state |
| `bootwright machine start` | required `--name <list>` | the power state each Machine's management controller proved once the operation settled | bounded power operation through that management controller, one concurrent [batch](../cli.md#machine-power-operations) for several Machines |
| `bootwright machine stop` | required `--name <list>`; `--force` false; `--yes` false | same, after the operating system is asked to shut down | same |
| `bootwright machine restart` | required `--name <list>`; `--force` false; `--yes` false | same, after a proved stop and a proved start | same |
| `bootwright machine media eject` | required `--name <list>`; `--yes` false | the media state each Machine's management controller was read back at once the eject settled, `none`, with what it presented before and its unchanged power | bounded power operation through that management controller that [ejects](../cli.md#machine-media-eject) whatever virtual media it presents, one concurrent [batch](../cli.md#machine-power-operations) for several Machines |
| `bootwright machine media status` | required `--name <list>` | the [virtual-media state](../cli.md#machine-media-status) for every named Machine, `present`, `none` or `unknown` | bounded read of the virtual-media state through each Machine's management controller |

#### Controller and cluster commands

| Invocation | Local flags and defaults | Successful result | Effects |
| --- | --- | --- | --- |
| `bootwright install` | `--dry-run` and `--check` false and mutually exclusive; `--yes` false | [controller setup result](../controller.md#the-setup-flow); `--dry-run` gives the plan and every locked wheel in structured output; `--check` gives readiness findings and their overall outcome | execution installs host packages through the host's dnf after its preview and builds the runtime from carried wheels; `--dry-run` and `--check` use read-only host and private setup inspection |
| `bootwright rebind-host` | `--yes` false; `--recorded-host-retired` false, required with `--yes`; `--machine-id <id>`, `--product-uuid <uuid>` and `--root-uuid <uuid>` default none, all three this host's and required with `--yes`, and a remedy repeating the command names them by placeholder | the outcome and the counts of the bindings and reservations it kept | [host identity rewrite](../contexts.md#controller-relationship-and-host-binding) of the controller record, once, under the exclusive root lock, after a confirmation naming the recorded and the new identity and affirming that the recorded host no longer runs this store; refuses a store whose directories are not the ones its registry attributed and an unchanged identity, and cannot tell a byte-for-byte clone from the store |
| `bootwright uninstall` | `--dry-run` false; `--keep-packages` false; `--discard-unreadable` false; `--yes` false | what it removed and kept or `Nothing to do.`; with `--dry-run`, the planned removals and retained items | host packages, then the store, staging, log and hosted roots and an empty libvirt image prefix, whole, by the [uninstall flow](../controller.md#the-uninstall-flow); a discard removes no package; with `--dry-run`, bounded read-only ownership and package checks |
| `bootwright cluster list` | none | container and storage cluster names and kinds in canonical order | read local state |
| `bootwright cluster info` | `--name <list>` default all unless `--secrets` or `--object-user`, which require exactly one cluster; `--secrets` false; `--object-user <name>` none | cluster kinds, endpoints, access-command applicability and availability, artifact availability, for a managed Ceph cluster its release, FSID, frozen image, monitors, administrator command and dashboard access, and optional explicit sensitive values | read local state and optional confidential material |
| `bootwright cluster update-admin-creds` | required `--name <cluster>`; `--login <list>` default all; `--yes` false | update each selected login with a retained candidate and repeatable recovery | credential update on one managed cluster: its monitoring logins, custody and a bound Grafana Secret's binding |
| `bootwright cluster rsh` | required `--name <cluster>` (one cluster); `--node <node>` default the cluster's administrator node (at most one) | an interactive SSH session on the resolved node's Machine under the [cluster session contract](../cli.md#cluster-sessions); the client's exit status is the result | same as `machine rsh` |
| `bootwright cluster exec` | required `--name <list>`; `--node <list>` default each cluster's administrator node; required `<command>...` | the exact argument vector runs on the resolved node's Machine, or on every resolved Machine concurrently, at most the lifecycle bound at a time, with [each line prefixed by its Machine](../cli.md#several-clusters-and-nodes), under the [cluster session contract](../cli.md#cluster-sessions); the remote command's exit status is the result, the highest of them for several | same as `machine exec` |
| `bootwright cluster oc` | required `--name <cluster>` and non-empty `<command>...` | bounded `oc` handoff descriptor | read context-owned access metadata only |
| `bootwright cluster kubectl` | required `--name <cluster>` and non-empty `<command>...` | bounded `kubectl` handoff descriptor | read context-owned access metadata only |
| `bootwright cluster kubeconfig` | required `--name <cluster>` | kubeconfig through the explicit sensitive-output boundary | read and reveal one context-owned credential artifact |
| `bootwright cluster keyring` | required `--name <cluster>`; required `--client <client>` | the Ceph keyring of one CephClient through the explicit sensitive-output boundary | read and reveal one context-owned credential artifact |
| `bootwright version` | none | version, commit, source state, Go runtime and target, embedded dependency-bundle identity and the build's vulnerability verdict | none |
| `bootwright catalog schema` | required `--format json-schema` | the advisory [JSON Schema export](../cli.md#schema-export) of every registered kind | none |
| `bootwright sbom` | required `--format cyclonedx-json` | this executable's [bill of materials](../cli.md#bill-of-materials) | none |
| `bootwright explain` | `--code <code>` | every row of the [stop catalog](stops.md#catalog), or the one `--code` names, with its meaning, level, certainty, cause, recovery, whether repeating helps and its remedy; an unknown code is a `cli.usage` refusal | none |
| `bootwright help [command ...]` | no local flags | help for the exact resolved command, human or structured | none |
| `bootwright completion bash` | `--no-descriptions` false | Bash completion script | none |
| `bootwright completion zsh` | `--no-descriptions` false | Zsh completion script | none |
| `bootwright completion fish` | `--no-descriptions` false | Fish completion script | none |
| `bootwright completion powershell` | `--no-descriptions` false | PowerShell completion script | none |

### Cluster command applicability

`--name` selects from the shared OpenShiftCluster/CephCluster name namespace
in the selected context; that of `cluster info` and `cluster exec` is a
comma-separated list under the [list rules](../cli.md#parsing-and-input-conventions). The following table owns applicability. Target
resolution, readiness and diagnostics of `cluster kubeconfig` live in the
[administrator access export](../cli.md#administrator-access-export); those of
`cluster keyring` in the [client keyring export](../cli.md#client-keyring-export);
those of `cluster rsh` and `cluster exec` in the
[cluster sessions](../cli.md#cluster-sessions); those of `cluster info
--secrets` on a managed Ceph cluster in the
[storage dashboard access](../cli.md#storage-dashboard-access); those of
`cluster oc` and `cluster kubectl` are
[client handoffs](../cli.md#cluster-client-handoffs).

| Command | Applicable targets |
| --- | --- |
| `cluster list`, `cluster info` | All selected OpenShiftClusters and CephClusters, including external storage clusters. |
| `cluster rsh`, `cluster exec` | OpenShiftClusters and managed CephClusters with declared nodes. External CephClusters have no declared node roster and are inapplicable. |
| `cluster oc`, `cluster kubectl`, `cluster kubeconfig` | OpenShiftClusters only. |
| `cluster keyring` | Managed CephClusters only. |
| `cluster update-admin-creds` | OpenShiftClusters (OpenShift and OKD) and managed CephClusters after completed apply; external Ceph is inapplicable. A `--login` it lacks is inapplicable. |

Applicability does not establish implementation availability, access readiness,
identity, or ownership. Missing access material on an applicable target is an
access failure, not a change in applicability.

## Session policy installation

`policy install` replaces the policy of the selected context: `--context`,
else the current context. See the
[session policy](../api/session-access-policy.md) for the schema and delegation rules.

## Help content

Every available command's help carries at least one example and names the
values of each enum flag, and the refusal of an enum value lists them. A command
that is recognized but unavailable says so in its own help, and an unavailable
cluster command offers no execution example. The root listing marks a command
group unavailable only when neither it nor any command under it is available, so
`render` is unmarked while `render effective` works. The root listing groups commands by the operator's next task. Installation,
context admission, lifecycle execution, inspection and host recovery each
have an entry in root help.

### Access help

The short description and detailed help for `cluster oc` and `cluster kubectl`
state that success prints an access descriptor for independent operator
execution and does not launch a client or connect. Cluster command help
includes its applicable target kinds and variants from the table above.
`cluster kubeconfig` and `cluster keyring` help state that success exports raw
sensitive bytes to standard output.

The short description and detailed help for `machine rsh`, `machine exec`,
`cluster rsh` and `cluster exec` state that the session runs on the Machine,
for a cluster the resolved node's, as its resolved identity, that an unproved
host key is confirmed interactively or refused, and that the exit status is the
client's. The cluster commands also state how `--node` selects: by node name,
then by FQDN, defaulting to the cluster's administrator node.

The detailed help of `cluster exec` states that `--name` and `--node` take
comma-separated lists and that the command runs on every resolved Machine at
once; that of `cluster info` states that `--name` takes a list and that
`--secrets` requires exactly one cluster.

The detailed help of `machine start`, `machine stop`, `machine restart`,
`machine media eject` and `machine exec` states that `--name` takes a
comma-separated list and that the command applies to every listed Machine
at once. `machine media status` also describes its comma-separated list.

Help and completion use the static catalog; they never load a selected cluster
to hide or enable commands. Target-aware discovery belongs to `cluster info`.

### Version output

`version` writes the build identity to standard output in the
[shared human layout](output.md#shared-human-layout) and nothing to standard
error: the `Bootwright` headline, then exactly these seven fields in this order.

```text
  Bootwright

    Version              v0.4.0
    Commit               9f2c1d0e3b5a7c8d9e0f1a2b3c4d5e6f70819293
    Source               clean
    Go                   go1.26.11
    Target               linux/amd64
    Dependency bundle    sha256:<64 hexadecimal digits>
    Vulnerability check  passed 2026-11-10
```

`Vulnerability check` is the verdict the build's
[vulnerability gate](../security.md#the-vulnerability-gate) stamped: `passed`
and the date it ran, or `unchecked` for a build that skipped it or carries no
stamp (`TestVersionReportsTheStampedVulnerabilityCheck`).

The composition root supplies every value. It obtains the Go runtime, GOOS, and
GOARCH from the linked Go runtime, the dependency bundle as the digest of the
automation the executable embeds, the one `install` publishes and every
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
does not have that form renders as `none`. After sudo-entry admission, the
command performs no environment, filesystem, process, network, state, secret,
random or other effect.

### Completion

After sudo-entry admission, completion generation and the private completion
protocol derive solely from
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

`context delete` requires `--purge` to resolve to `true`. Omitting the flag
shows command help. An explicit `--purge=false` fails without changing state.
`--yes` independently controls
its ordinary confirmation, and `--allow-orphans` replaces neither. What each
flag acknowledges, and when deletion refuses, follow
[permanent deletion](../contexts.md#permanent-deletion). A deletion that
abandons objects warns once on standard error with `context.orphaned`.

`--stage` accepts only `controller`, `infrastructure-services`, `substrates`,
`machines`, `clusters`, and `add-ons`, and only on `apply`. Its
members follow the [comma-list rules](../cli.md#parsing-and-input-conventions);
a supplied value that resolves to no member and an unrecognized member are both
usage errors. Omission selects every stage. What a selection gates is owned by
[state reconciliation](../state-reconciliation.md#stages-and-the-pause-boundary).

On apply, `--clusters` and `--machines` accept comma-separated lowercase
DNS labels. Supply one flag family per invocation. Omission leaves all target
work eligible. An explicitly supplied empty, whitespace-only or comma-only
list is a usage error. Scalar occurrence and duplicate-member handling follow
the [comma-list rules](../cli.md#parsing-and-input-conventions). Target
selection composes with `--stage` and `--until` under the
[apply selection contract](../state-reconciliation.md#selecting-apply-targets).

`--until` is accepted only on `apply`. Its value names one add-on
instance as `<cluster>.<package>`, two lowercase DNS labels, the `object` that
`apply --dry-run --output json` reports for the instance; any other form is a usage
error. It composes with `--stage`: a block starts only when both admit it.
What it gates, and its refusals, are owned by
[state reconciliation](../state-reconciliation.md#stopping-after-an-add-on-instance).

`--parallelism` is accepted only on `apply` and `destroy`. It takes a positive
integer and defaults to eight; zero, negative, empty, non-integer and overflowing
values are usage errors before elevation or service dispatch. The last supplied
value wins, but an empty occurrence is always refused. It bounds running steps
for this invocation, including continuation and removal checks, without changing
the frozen plan. Dependencies, exclusive claims and the executable's group
throttle still apply, so a larger value need not increase concurrency. Use
`--parallelism 1` to run steps serially, or `--parallelism 16` to admit up to
sixteen independent steps. Higher values can increase controller and target load.

`--authorize` accepts only `data-loss`, `data-loss=<digest>` with the 16
lowercase hexadecimal digits a plan prints for the steps it authorizes,
`rebind=<secret>`, `erase=<machine>:<method>` and `abandon=<machine>`; the first
three only on `apply` and `destroy`, `erase=` and `abandon=` only on `destroy`
and `destroy --dry-run`; `--yes`, or input
that cannot answer a prompt, requires the `data-loss=<digest>` form.
`abandon=<machine>` leaves the steps of one Machine that its capabilities can
abandon in place for that invocation, under the
[abandoned removal](../state-reconciliation.md#abandoned-removal):
`<machine>` is the Machine's name, a lowercase DNS label, and it is
repeatable, once per Machine. An empty or bare `abandon`, a name that is not a
Machine name, and a Machine named twice or also in an `erase=` token are usage
errors.
`erase=<machine>:<method>` relaxes the declared
[removal erase](../state-reconciliation.md#opt-in-physical-logical-overwrite) of
one Machine, every target of it, for that invocation: `<machine>` is the
Machine's name, a lowercase DNS label, and `<method>` is `metadataClear` or
`retain`. It is repeatable, once per Machine, and each occurrence may also
contain commas. A `logicalOverwrite` method, an unknown method, a name that is
not a Machine name, an empty `erase=`, a missing `:` side and a Machine named
twice are usage errors, and `logicalOverwrite` is refused because a relaxation
never strengthens or restates the declared method. `destroy --dry-run` refuses any other
token. Whether a Machine can be relaxed, and what the relaxation changes, is
owned by [state reconciliation](../state-reconciliation.md#confirmation-and-authorization).
`rebind=<secret>` names one Secret by its API name and
presents a rotated access credential at its current version through the
[access-credential rebind](../state-reconciliation.md#access-credential-rebind).
Unknown, empty, or duplicate tokens, `data-loss` given twice in either form,
a digest of another shape, and a `rebind=` that names no valid Secret name, are
usage errors; `all` is not accepted.
Whitespace around comma-separated tokens is ignored. A token grants no
confirmation authority either: it neither replaces nor implies
[ordinary confirmation](../cli.md#ordinary-confirmation). Whether the frozen plan
requires the token, what it acknowledges and the refusal of a token it does not
require are owned by
[state reconciliation](../state-reconciliation.md#confirmation-and-authorization).

`logs` refuses as usage errors, before privilege: `--block`, `--raw` or
`--attempt` with `--run` or `--install`; `--run` with `--install`; `--operation`
with either; `--install` with `--context`; `--raw` without `--block`;
`--attempt` without `--raw` or outside 1 to 999999; `--follow` with `--raw`
or with JSON or YAML output; and a malformed operation, block, run or setup
name. `--install` takes a value only after `=`, so `--install install-000003` is
refused with the remedy `write --install=install-000003`.

`--unregistered` names a Machine whose subscription consumer the operator
removed with the subscription service's own tools, and is accepted only on
`destroy`. On that invocation the Machine's installation neither reads nor
unregisters its guest and records no registration, as
[managed OS](../managed-os.md#registration) states; nothing persists the name,
and only the `authorize.missing` or `authorize.unneeded` refusal of a missing or inapplicable
token carries it: its command repeats the invocation's own `--unregistered`
names, sorted and joined by commas, after the tokens the plan requires, so the
manual unregistration's printed next steps complete a destroy whose plan
consumes `data-loss`. A name whose installation the destroy plan does
not unregister refuses `authorize.unneeded` before confirmation and
registration: `this destroy removes no subscription registration from
Machine/<name>, so --unregistered <name> acknowledges nothing`, remedied by
`repeat <command> without --unregistered <name>`, where `<command>` is the
destroy with the tokens its plan requires. An empty occurrence or an empty
comma-separated member is a usage error; whitespace around members is ignored.
It grants no confirmation authority either.

A changed SSH host key always fails. `machine trust --replace` is the only
interface for a deliberately changed key, and every replacement name must also
be in that command's selected Machine set. `--expect` and a `--replace` member
of the form `<machine>=SHA256:<fingerprint>` name the fingerprint the operator
verified out of band; a fingerprint that is not `SHA256:` and 43 base64
characters, or two different fingerprints for one Machine, are usage errors. Preflight's
`--observe-host-keys` follows the [preflight observation and refusal rule](output.md#preflight).

`--sensitive` authorizes materialization, not disclosure in normal output. It
applies only to the render rows that list it. Sensitive installer artifacts use
the exact verified destination boundary, mode `0600` beneath `0700`
directories, atomic publication, and bounded cleanup in [security](../security.md).
It never permits a secret in an effective-state artifact, manifest, diagnostic,
log, command argument, or path. `render effective --input-dir` always uses placeholders
and requires `--output-dir`. A non-empty `--context` or
`--sensitive=true` conflicts with `--input-dir`. A context-backed
`render effective --output-dir` requires `--sensitive`, `--sensitive` requires
`--output-dir`, and `--origins` conflicts with `--output-dir`.

A lifecycle preview checks the work selected by the same verb.
`destroy --dry-run` takes erase relaxations for plan review. The execution
uses the data-loss and credential rebind authorizations.

`machine list --short=true` emits only sorted names as text and conflicts
with resolved JSON or YAML output and with `--power-status`, because a reading it
could never print would contact every management controller for nothing. Each
conflict is `cli.usage` with exit `2` and the normal selected structured failure envelope;
`--short=false` permits both. These relationships use final scalar values and
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
acts on nothing. `machine media eject` resolves its list the same way, takes the
ordinary confirmation and no `--force`, because a Machine still running from the
image it ejects may fail, and with resolved JSON or YAML output it likewise
requires `--yes`. `machine media status` resolves the same list and has no
confirmation or force flag.

A `machine trust` invocation selecting JSON or YAML with pending trust-store writes
requires `--yes`; without it the invocation must use `--dry-run` or fails
without writing. Under `--yes`, [host-key trust](../cli.md#host-key-trust)
records an `add` or `replace` only for a Machine whose fingerprint
`--expect` or `--replace` names (D183, Q97).

`catalog media add --from-url` and `--from-url-file` require `--sha256`; the digest is
optional verification for `--from-file`. `--from-url-file` reads the URL from
the named file, opened under the invoking account like an image file, or from
standard input for `-`: exactly one line of at most 16 KiB, an optional final
line feed removed. A URL whose query carries a signature is a credential
(D183, Q99), so it belongs in that file or on standard input, never on the
argument vector: `--from-url` with a query, even an empty one after `?`, is
`cli.usage` with exit `2` before anything is read or contacted, naming
`--from-url-file <path|->` as the remedy and not the URL. The URL a file holds
is validated as `--from-url` is except that it may carry a query, and no
output names it. Standard input is refused, before anything is read, under a
sudo policy that would log it, as a Secret's standard input is, and without
`--yes`, since standard input then cannot answer a replacement's confirmation. A plain `http://` URL, URL userinfo, a fragment, a control
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

`cluster info --secrets=true` and `cluster info --object-user <name>` each
require an explicitly supplied `--name` naming exactly one cluster after list
normalization, and a list of several is `cli.usage`; the default-all selection
applies only when sensitive values are not requested. The two are mutually
exclusive, so one invocation reveals one credential, and `--object-user` names
one CephObjectUser by its API name.

The [Secrets contract](../secrets.md#acquisition-and-commands) owns the exact
type/source flag matrix, whole-version validation, generation and reveal parts.
Stdin replacement requires `--yes` before reading. Rotation atomically preserves
every active and bound logical version under the selected implementation.

Secret, media, and add-on mutation obeys the immutable binding and external-
content rules in state reconciliation. A set, renewal, replacement, or
deletion refuses while an incomplete operation or completed-apply snapshot
needs the existing version, unless that exact version remains durably
available for continuation and destroy. A bound version of effect material
stays available but no command reveals it, so a renewal or replacement of a
Secret whose current version a live binding holds as effect material refuses
even then, as [effect material under a live binding](../secrets.md#effect-material-under-a-live-binding)
states.
