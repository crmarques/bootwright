# Managed infrastructure services

Infrastructure services owns the runtime behavior of a managed shared service:
its selection from effective state, the exact implementation it deploys, the
host resources it claims, the evidence that proves completion or absence, and
its inverse. The [kind schemas](api/infrastructure-services.md) own declaration;
[state reconciliation](state-reconciliation.md) owns operations, ordering and
durable records.

A managed service is a [lifecycle capability](state-reconciliation.md#plan-and-execution)
that plans one block per service object.

## Selection and refusal

A managed service becomes a block only when its placement Machine has effective
`os.provided: true` and the service's declared requirements hold; admission
refuses a placement Machine without it at `$.spec.machineRef`
([placement](api/infrastructure-services.md#management-and-placement)). Reference
resolution alone is not a readiness edge: an endpoint's Machine address supplies
a value, while deployment requires the host itself. One realization edge does
exist: a service whose bind address is the host address of a
[managed libvirt attachment](substrates.md#provider-host-realization) on its
placement Machine names that provider as a requirement, because the socket
cannot bind before the bridge exists. A service bound to a wildcard names the
provider of each declared endpoint address that is such a host address,
because its readiness probes that address, which cannot answer before then.

An unsupported required capability refuses before operation registration, with one
diagnostic per unsupported object that names the object, the reason, and a safe
next action.
Refusal never registers an operation, reserves a host resource, binds a Secret
or creates a log.

### Refusal table

Each row is one refusal of a managed service capability.
`TestArtifactServerRefusalTableMatchesUnsupported` holds the artifact server's
`Unsupported` to every row, and `TestManagedServiceRefusalTableMatchesUnsupported`
holds the Proxy, DNSServer and NTPServer capabilities' to every row except
install-only retention, which only an ArtifactServer declares. Reason and
remedy are the diagnostic's message and remediation (`lifecycle.unsupported` on
the refused service), in which `<server>` is the refused service, `<machine>`
its placement Machine and `<proxy>` the Proxy that Machine selects. A placement
Machine that is the Environment's controller is reached locally, so the SSH
rows never refuse a service placed there.

| Refusal | Path | Reason | Remedy |
| --- | --- | --- | --- |
| Install-only retention | `spec.retention: install-only` on a managed `ArtifactServer` | `this executable serves no managed artifact server with install-only retention` | `declare spec.retention: persistent on <server>, or omit it` |
| Managed Proxy egress | `spec.proxy.proxyRef` of the placement `Machine` names a managed `Proxy` | `this executable acquires a managed service's image only directly or through an external Proxy that is already ready, and <machine>, where <server> runs, selects the managed <proxy> in spec.proxy.proxyRef` | `select direct: {} or an external Proxy in spec.proxy on <machine>, or place <server> on another Machine` |
| Proxy authentication | `spec.connection.auth.proxyAuthRef` on the external `Proxy` the placement `Machine` selects | `this executable acquires a managed service's image through no authenticated proxy, and <proxy>, which <machine> selects for <server>, sets spec.connection.auth.proxyAuthRef` | `select direct: {} or an external Proxy that needs no authentication in spec.proxy on <machine>` |
| Private trust | `spec.connection.trustBundleRef` on the external `Proxy` the placement `Machine` selects | `this executable acquires a managed service's image with the host's system trust store alone, and <proxy>, which <machine> selects for <server>, sets spec.connection.trustBundleRef` | `select direct: {} or an external Proxy without spec.connection.trustBundleRef in spec.proxy on <machine>` |
| Operator SSH identity | `spec.access.ssh.auth.operatorIdentity` on a placement `Machine` other than the controller, the default of a provided Machine without authored access included | `this executable reaches a placement host other than the controller only with a bound SSH private key, and <machine>, where <server> runs, uses spec.access.ssh.auth.operatorIdentity` | `author spec.access.ssh.auth.privateKeyRef and spec.access.ssh.knownHostsRef on <machine>, or place <server> on the controller Machine` |
| Password SSH authentication | `spec.access.ssh.auth.passwordRef` on a placement `Machine` other than the controller | `this executable reaches a placement host other than the controller only with a bound SSH private key, and <machine>, where <server> runs, uses spec.access.ssh.auth.passwordRef` | `author spec.access.ssh.auth.privateKeyRef and spec.access.ssh.knownHostsRef on <machine>, or place <server> on the controller Machine` |
| No bound host key | `spec.access.ssh` without `knownHostsRef` on a placement `Machine` other than the controller | `this executable connects to a placement host other than the controller only against a bound SSH host key, and <machine>, where <server> runs, declares no spec.access.ssh.knownHostsRef` | `author spec.access.ssh.knownHostsRef on <machine>, or place <server> on the controller Machine` |

## Placement arms and credentials

| Arm | Selected when | Execution | Coordination |
| --- | --- | --- | --- |
| Controller | The placement Machine is the [Environment controller](api/environment.md#controller-machine) | Local, through the private controller runtime under [Controller's execution boundary](controller.md#egress-and-local-effects) | The Workspace root lock, the context lease, the verified host binding and the host reservations below |
| SSH | The placement Machine authors `access.ssh` | Remote, over SSH from the controller | The context lease only |

The controller arm requires this context's verified
[host binding](contexts.md#controller-relationship-and-host-binding) and the
setup receipt whose automation identity matches the running executable. A
missing, contradictory or stale binding refuses before effects and names
`setup`.

The SSH arm connects as root and never escalates. It consumes authored
`access.ssh.auth.privateKeyRef` and required `access.ssh.knownHostsRef`, and
binds user `root` and only those two Secret versions with the plan. A
placement host with another account or an escalation Secret refuses at
admission under [addresses and access](api/machines.md#addresses-and-access),
and the placement derivation refuses the same host before planning.
`passwordRef` and `auth.operatorIdentity`, and a placement host with no
`knownHostsRef`, refuse before registration as the
[refusal table](#refusal-table) states; the global borrowed-SSH flags are
unsupported and refuse before planning. The bound `knownHostsRef` material is parsed before
connection under [the Machine host-key rules](api/machines.md#addresses-and-access);
a changed, missing, corrupt or unparseable entry refuses before the connection,
and an observed key mismatch refuses before any remote command. No SSH
configuration, agent, ambient `known_hosts` or user identity is consulted.

Two contexts targeting one SSH host are not coordinated. Only the controller
arm publishes the host reservations below; the sockets an SSH host's blocks
claim are compared within their own context alone.

## Host reservations

A capability that places effects on the controller declares stable conflict
identities before its first effect. Workspace stores them with the shared
controller record, so a second context refuses rather than silently taking a
port, unit or path that another context owns.

A reservation key is one of:

| Key | Claims |
| --- | --- |
| `socket:<address>:<port>` | One effective listening socket on every transport, so a service listening on UDP and TCP at one port holds one key. A wildcard bind address, `0.0.0.0` or `::`, claims every declared endpoint address at that port and additionally conflicts with any other bind address at that port, either wildcard included. |
| `unit:<name>` | One host service-manager unit and its container name. |
| `path:<absolute path>` | One owned directory tree. |
| `bridge:<name>` | One host bridge, whichever network defines it. |
| `libvirt-network:<name>` | One libvirt network definition. |
| `libvirt-pool:<name>` | One libvirt storage pool definition. |
| `prefix:<masked prefix>` | One managed libvirt network's routed prefix, IPv4 or IPv6, written masked (`prefix:198.51.100.0/24`); it conflicts with every held prefix it overlaps, identical or nested, because the host routes each to its own bridge. |
| `libvirt-domain:<name>` | One libvirt domain definition. |
| `bmc:<host>:<port>/<system>` | One [physical machine](substrates.md#physical-machine-realization), named by its normalized management-controller endpoint and exact ComputerSystem, so two contexts never drive one server. The claim serializes use; it is never ownership of the machine and authorizes nothing about it. |
| `media:<filename.iso>` | A shared claim on one image of the [media store](managed-os.md#media-store); it conflicts with nothing and blocks only that image's deletion or replacement while any context holds it. |

A `socket:` key writes an IPv6 address unbracketed, the one form every
capability claims, while a plan's listener impact names the socket as an
address and port are written, an IPv6 address bracketed, as
`open-listener [fd00::1]:3128` (`TestIPv6ListenerImpactsAreBracketed`,
`TestIPv6ArtifactListenerImpactsAreBracketed`).

Every key is exclusive except the class marked shared. Reservations are
published under the root lock before the operation's first effect and released
by a completed destroy, or by a destroy that finds them held by no operation
because the registration that published them was
[interrupted](state-reconciliation.md#lifecycle-unit). A requested exclusive
claim that conflicts with claims another context holds refuses with one
`controller.conflict` diagnostic per held claim it conflicts with. Each names the holding context; the held key
for `socket:`, `bridge:` and `prefix:` keys, and only the class for any other,
so no private path prints; the holder's object; and this context's object. Its
remedy is `bootwright destroy --context <holder>`, because only a completed
destroy releases a key, or the field of this context's declaration that
chooses the key (`TestEachConflictingHeldClaimIsItsOwnDiagnostic`). A
context's own keys are replaced by its own apply. An interrupted apply leaves
its reservation in place, and the same context's next apply replaces it.
Continuing the holder, even to completion, keeps its keys held, so another
context's apply keeps refusing until the holder is destroyed
(`TestEveryConflictingClaimIsNamedAndAContinuationKeepsRefusing`). A failed
registration likewise leaves its reservations held, and its context protected by running
[evidence](state-reconciliation.md#context-mutation-evidence), until a destroy
releases them, or the context's next apply releases them as it finishes the
removal they sit beside or replaces them.

One context's own exclusive `socket:` keys are compared by the same rule while
it plans a fresh apply, each qualified by the host its block is placed on: the
controller, or the SSH host its placement Machine reaches. An SSH host is the
effective SSH address and port the placement connects to, as the
[host-key token](api/machines.md#addresses-and-access) names it, so two
Machines whose access reaches one address and port are one host whatever their
names, and one address at two ports, as a forwarded port gives, is two. A
placement that reaches port `22` at a loopback address, at `localhost` or at an
address the controller Machine declares is the controller, and its claims are
compared with the controller's. Addresses compare as declared, an IP in its
canonical form and a DNS name without case or a final dot, because planning
resolves no name. Two of its claims on one host whose sockets conflict refuse
`api.invariant` before registration, naming each claim's kind, service and
socket, that host, and each Machine through which either claim reaches it over
SSH when that is not the host's own name, because the second could never
listen; claims on two hosts never conflict. The socket claims of a managed
service, an artifact server or an
[emulated BMC](substrates.md#machine-realization) placed on an SSH host are
compared only within their context and never published, because two contexts
targeting one SSH host are not coordinated. One claim's keys never conflict with each other, and
no other key class is compared within a context, because one context's claims
may share a key by design, such as the `path:` of a package tree two
installations of one profile publish.

A socket held outside the contexts this host coordinates is no reservation, so
the apply proves it before its first effect. A managed network service or
artifact server whose unit is not active and of which no container exists
reads its host's TCP and UDP socket tables (TCP for a proxy and an artifact
server, UDP for a time service, both for a resolver) and refuses when a
listener holds one of its ports at an address the wildcard rule above makes
overlap its bind address. Nothing is pulled, published or started. The refusal
is `lifecycle.state` on the service, names the socket it binds, and its remedy
is to stop what listens there, or choose another bindAddress, or another port
for a service whose port is declared; a DNSServer and an NTPServer keep the one
port validation permits, so theirs offers only the bindAddress. For a wildcard
bind, whose colliding address the refusal record does not carry, the remedy
points to the retained run output, which lists every socket found
(`TestAForeignListenerRefusalNamesTheSocketAndTheRemedy`,
`TestAForeignListenerRemedyOffersOnlyWhatValidationAdmits`).

Dependency readiness never establishes service ownership, and
[controller setup](controller.md#host-identity-and-shared-prerequisites)
reserves nothing.

## Managed artifact serving

The `ArtifactServer` capability serves static content over the declared
listeners. It owns its content root and publishes nothing into it itself; a
consuming capability publishes beneath it under the
[consumer publication contract](#consumer-publication), in a block of the same
plan, never by appending to a frozen one.

**Implementation.** One container image runs an HTTP server under the host
service manager with host networking, so declared bind addresses and ports are
the real sockets. The image is `spec.image.local`, else `spec.image.public`,
else the executable's compiled default. An authored reference is pinned by
content digest at [admission](api/infrastructure-services.md#management-and-placement)
and the compiled default is one, so a tag never reaches a plan.
Image acquisition uses the placement Machine's normalized
[proxy choice](api/machines.md#machine-proxy) and no ambient proxy variable;
a managed, authenticated or private-trust Proxy refuses before registration, as
the [refusal table](#refusal-table) states.
`retention: install-only` is unsupported and refuses, as the
[refusal table](#refusal-table) states.

**Owned host state.** The capability owns exactly one content root per service,
one server configuration, one unit definition and, when any listener is HTTPS,
one certificate and private key beneath that root. Directories are `0755`
except the private key's directory, which is `0700`; the private key is `0600`.
The content root is outside the Bootwright state root, so serving never exposes
context storage. The apply creates the context's and the kind's directories
beneath `/var/lib/bootwright-services` that hold the content root; the destroy
removes each once it is empty, never recursively, and retains
`/var/lib/bootwright-services` itself
(`test_a_destroy_removes_each_empty_parent_never_recursively`). Nothing else on
the host is created, modified or removed.

**TLS.** The [schema](api/infrastructure-services.md#artifactserver) requires a
serving certificate exactly when an effective listener uses HTTPS. A
[generated](api/secrets.md#generated-source) certificate names what its
declaration lists, so admission refuses, as `api.invariant` at
`$.spec.endpoints[i]`, an HTTPS endpoint whose address it does not name,
naming the Secret and the missing address, with the remedy to add that address,
import the change with `context update --name <context>` and then run
`secret generate` with `--context`, since generation reads the context's stored
revision rather than the edited input: an IP address must be one of
`spec.source.generated.ipAddresses`, compared as hostname verification compares
them, so an IPv4-mapped IPv6 address names its IPv4 one, and a DNS name one of
`dnsNames`, compared without case; the common name names nothing
(`TestAGeneratedServingCertificateCoversEveryHTTPSEndpoint`). Admission reads
no material, so every bound serving certificate, a `contextStore` one
included, is proved when a fresh apply binds it, before registration, by these
checks: bounded PEM parsing, certificate and key agreement, an RSA serving key
of at least 2048 bits, validity at the injected clock, server-authentication
suitability, not a certificate authority, and subject-alternative-name
coverage of every address an HTTPS endpoint serves. A failure there refuses as
`secret.part` on `Secret/<name>`, and names the unmet condition and what stores
usable material in the context: `secret set` with the certificate and key
files, or, for a generated Secret, the declaration field to extend and
`secret generate`, each with `--context`. Nothing registered and the binding
is released, so the remedy ends by applying again
(`TestAContextStoreServingCertificateIsProvedWhenItsBindingIsAcquired`). It
never names material or its digest. A continuation proves the version its
operation [bound](secrets.md#immutable-binding-and-contexts) again, before
connection or installation; the operation keeps that version, so a failure
then ends by destroying the apply and applying again
(`TestServingCertificateRefusalsNameTheSecretAndTheirRemedy`). Material an
attempt cannot reopen refuses as `secret.store` on `Secret/<name>`, with the
remedy to run `status --context <context>` and repeat the command it names
(`TestMissingBoundServingMaterialNamesItsSecret`).
`tls.minVersion` selects the exact protocol floor the server enforces.

**Readiness.** Completion requires positive evidence for every listener: the
socket accepts a connection at the declared address and port; an HTTPS listener
completes a handshake whose presented leaf certificate digest equals the bound
certificate's; and the listener answers one bounded HTTP status line. A
listener is probed at the bind address, or under a wildcard bind at each
endpoint address that names it. A frozen request with no probe target, or with
a listener no target probes, such as one an earlier build froze for a wildcard
bind that admission now refuses, therefore never proves presence: everything
present reads as a partial realization, and its removal, which reads no probe,
is unaffected (`TestPresenceRefusesAnUnprobedListener`). A certificate
mismatch or a refused connection after the service reports started is a
definite failure. A socket that never answers within the bounded readiness
window is unknown, not failure. A listener left unproved is reported with its
last attempt's cause: the error's name, with the system's number and message
when it carries them, and a timeout named `TimeoutError` on every Python a
managed host may run.

**Replay.** An apply whose frozen request already matches the live host reports
`completed` with the same completion evidence and no change. The differences it
converges are the configuration, the unit definition and any serving material,
each published again when its bytes differ; a running service that started
before any of them was last published, or whose start cannot be read, which is
restarted, because the service reads only what it started with, while one that
started after them all is not; and any owned unit, container or content root
that is missing, which is created again. An apply stopped between publishing a
file and restarting the service is therefore completed by its next attempt.
Everything it owns carries the context in its name, so a same-name object it
did not create cannot occur without a reservation conflict refusing first.

**Inverse.** Destroy stops the service, removes the unit definition, removes
the container, removes the owned content root and then reobserves. Positive
absence requires the unit absent, the container absent, the content root absent
and every reserved socket free. An already-absent service reports `completed`
with that same absence evidence. Destroy removes no image from the host store
and no unrelated file.

**Unknown resolution.** Observation is read-only against the frozen request and
exact identity. Live state matching the frozen request in full is positive
completion; nothing present, with a before-state that recorded nothing, is
positive no effect; and any of the unit, container or content root present
without the whole, its listeners' answers included, is a positive partial
realization, which the next attempt converges. A service all present whose
listener does not answer, or presents another certificate, is therefore this
context's own service not yet ready rather than an unproved effect, so a fresh
`destroy` that resolves an incomplete apply's block through this observation
goes on to remove it. Presence evidence claiming the postcondition without the
unit active and the content root present contradicts itself and stays unknown.
A removal's resolution reads the same observation for what the
removal takes back: nothing present is its completion; the unit active, the
container of the frozen image and the content root all present is positive no
effect; and any of the unit, container or content root present without all of
them is a positive partial realization. Listeners do not decide a removal's
resolution: a listener that does not answer, or presents another certificate,
leaves a service otherwise present a removal with no effect. A removal's
resolution remains unknown only when its observation cannot be made, cannot be
read as this request's own, or contradicts itself: absence evidence that still
reports part of the service, or presence evidence that names none of it.

**Quiescence.** A managed service's quiescence follows the Machines under the
[removal gate](state-reconciliation.md#quiescence-before-removal); its own
listener is never observed for use, because refusing on it would refuse a
removal whose Machines are already stopped.

### Consumer publication

A capability that produces content for others to fetch publishes it beneath a
managed artifact server's served root instead of serving it itself. The server
keeps ownership of the root and of everything it created; a consumer block owns
exactly the subtree `<consumer>/<object>/` it publishes, names the server as a
requirement so the server's block completes first, and removes that subtree in
its own inverse, which the plan orders before the server's. Publication within
the subtree is atomic, labels published files so the serving process can read
them, and is recorded in the consumer's evidence alone: the server's evidence
never enumerates consumer content, its replay ignores it, and its inverse
removes the root only after every consumer subtree is gone. Published content
is non-sensitive. The first consumer is the installer image and package tree of
[managed OS](managed-os.md#installation).

A consumer fetches what it published at the selected endpoint's URL,
`<protocol>://<host>:<port>/` followed by its subtree and file, where the host is
the address the endpoint names on the server's Machine, an IPv6 address
bracketed as every URL writes one, so a fetcher reads the port as the port
(`TestEndpointURLsBracketIPv6`,
`TestAnIPv6ArtifactEndpointFreezesABracketedInstallURL`). IPv4 addresses and
DNS names are written as they are.

The consumer-level directories above those subtrees, such as `os/`,
`private/` and `private/os/`, are created by the first consumer that needs
them and owned by no block: no consumer's inverse removes them, and they stay
until the server's own inverse removes the root. Object names beneath one
consumer prefix are unique per server, because two objects of one name would
share one subtree that neither block owns alone; managed OS enforces this for
its Machines and profiles at [admission](managed-os.md#installation).

### Private consumer publication

A consumer that must hand one machine a secret it cannot embed in publicly
served content publishes it beneath `private/<object>/<token>/` of the same
served root. The final segment is an unguessable token of at least 32 bytes
from the operating system's cryptographic random source, minted by the attempt
that publishes it and never by the plan, so it appears in no frozen request, no
evidence, no progress output and no log. The consumer block owns
`private/<object>/` as its reserved path; every directory from `private/` down
is `0711` `root:root`, so nothing is listable and the token is the only way to
name what it contains. Every file is `0640` `root:root`: the server's workers
run as the image's application user with group `root`, the identity the
server configuration names, so the group grant is what lets the serving
process read the file and no other account is granted anything.

Its confidentiality rests on two properties together, and each must hold. The
token is unguessable, so the path cannot be found by enumeration. And the
fetch is made over a listener whose certificate the fetching machine verifies
against the bound certificate it was given, so the path cannot be learned by
observing the network. A consumer that would have to disable verification to
fetch it must not publish confidentially this way.

The attempt that publishes proves the publication before its block completes.
From the placement Machine it requests the first byte of the published file
through the selected listener, with no proxy, verifying the listener's
certificate against the server's serving certificate as the consumer's own
operation binds it: the consumer block names the server's certificate Secret
among its bindings, and its adapter runs are
[lent](secrets.md#immutable-binding-and-contexts) that Secret's certificate
part alone. The copy the server installed beneath its content root is never the
authority, because it proves only itself and not the certificate a verifying
fetcher is given. Any answer but `200` or `206` fails the block with its cause:
the status, the connection failure, or the verification failure naming that
Secret. The request names the token, so neither it nor the response reaches
output; only that cause does, with the token redacted. The proof precedes
anything that lets a later attempt treat the publication as complete.

Not yet met: the physical installation's host-key publication carries no such
proof, and its refusal before registration leaves that publication unreachable;
tracked as [B73](milestones/m4.md#b73).

Published material is removed as soon as the work that needed it completes,
and its absence is part of that block's completion evidence, because material
that existed for one boot must not outlive it. An observation that finds a
private tree whose work has completed treats it as unfinished work to converge,
and the inverse removes it, except where the consumer states that its content
is needed for as long as its own block is retained.

Two consumers use it. A [physical installation](managed-os.md#physical-installation)
delivers the host key its machine will present, which is material Bootwright
generated and which does not outlive the boot that installs it. A
[container cluster](container-clusters.md#boot-media) publishes its agent boot
image, which embeds the operator's own pull secret in its ignition and is
retained while the cluster's media block is, because a node may be booted from
it again. Any further use is specified before it is built.

## Managed network services

`Proxy`, `DNSServer` and `NTPServer` share one capability: each runs one
container under the host service manager with host networking, serving one
declared port on its declared bind address. They differ only in the daemon they
run, the configuration derived for it, and the answer readiness proves. Their
plan blocks belong to the
[`infra-components` stage](state-reconciliation.md#stages-and-the-pause-boundary).

One role realizes all three kinds, keyed by the request's frozen kind: it
selects the daemon configuration and unit templates for that kind from a fixed
table, and every task is shared. The three kinds freeze one request version, so
a request an earlier build froze for one kind refuses by naming its version,
and its remedy is to destroy the context with the build that applied it.

**Implementation.** One container image per kind, selected, pinned and acquired
exactly as the [artifact server's](#managed-artifact-serving) is. The unit
never uses the image's own entrypoint: the frozen request alone decides what
runs. Each daemon ends within its unit's stop timeout when stopped: the
proxy's shutdown lifetime is two seconds, below podman's ten-second stop
timeout, so a stop or removal never leaves the unit failed
(`test_the_proxy_stops_within_podmans_stop_timeout`).

**Owned host state.** Each service owns one content root, one daemon
configuration inside it and one unit definition. Directories are `0755`. The
content root is outside the Bootwright state root. The apply creates the
context's and the kind's directories beneath `/var/lib/bootwright-services`
that hold the content root; the destroy removes each once it is empty, never
recursively, and retains `/var/lib/bootwright-services` itself. Nothing else on
the host is created, modified or removed, and no managed service writes to the
host's own resolver, proxy or time configuration.

**Derived configuration.** A managed service is configured from the selected
graph, never from authored daemon syntax:

| Kind | Derived from the graph | Authored |
| --- | --- | --- |
| `Proxy` | The client set below, as the only addresses it serves. Before it admits a client it refuses every request whose destination is a loopback or unspecified address (`127.0.0.0/8`, `0.0.0.0/32`, `::1/128`, `::/128`) or a link-local one (`169.254.0.0/16`, `fe80::/10`), and every request for its cache manager; `CONNECT` is not restricted further (`test_the_proxy_refuses_local_link_local_and_manager_destinations_before_it_admits_a_client`). Caching and authentication are disabled. | `bindAddress`, `port` |
| `DNSServer` | One address record per retained Machine, from its effective `fqdn` contact and its declared IP addresses, and the records each selected container cluster answers at. | `bindAddress`, `port`, `forwarders[]` |
| `NTPServer` | The client set below, as the only addresses it answers. | `bindAddress`, `port`, `upstreamSources[]` |

The client set is loopback, every selected `NetworkConfig.spec.machineNetwork`
CIDR, and every retained Machine IP address as a single-host prefix, sorted and
deduplicated. A managed proxy or time service is therefore never planned open
to the world.

A `DNSServer` with no authored forwarder answers only its own records rather
than reaching an ambient upstream. A managed `NTPServer` serves time without
disciplining its host's clock, so it coexists with the host's own time service.

A selected `ContainerCluster` contributes the three names its own installation
polls and its consumers reach it at, each answering the address its endpoint
resolved: its API name, its internal API name, and its applications name, which
answers for every name beneath it because that is what an ingress wildcard
means. Each declared node answers at the same installation address its own
installer configures. A cluster whose endpoint resolved no address contributes
no record for that endpoint rather than a record pointing nowhere, and
`additionalIngressHosts` remains frozen in the request without producing one,
because nothing yet says which cluster's ingress an authored name belongs to. The names follow
[the container-cluster zone](api/environment.md#domains), so the resolver a
Machine uses and the installer that polls the cluster agree by construction.

**Readiness.** Completion requires a positive answer on every address the
service serves: the declared bind address, or every declared endpoint address
when the bind address is a wildcard. A frozen request naming no such address,
such as one an earlier build froze for a wildcard bind without an endpoint that
admission now refuses, never proves presence, so everything present reads as a
partial realization (`TestPresenceRequiresEveryDeclaredAnswer`). A proxy
answers a bounded HTTP request with a well-formed status line; a resolver answers one of its own records over
both UDP and TCP; a time service returns a server-mode reply, whose stratum may
show it unsynchronized without being a failure. An address that never answers
within the bounded readiness window is unknown, not failure, and is reported
with its cause as [an unproved listener](#managed-artifact-serving) is.

**Replay, inverse and unknown resolution.** Each follows the
[artifact server's](#managed-artifact-serving) rules above, and live state
matches the frozen request in full only while the running container started no
earlier than the last modification of every file it runs from: its daemon
configuration and its unit definition. A service that started before one of
them was last published, or whose start or files cannot be read, runs what the
frozen request no longer describes, so its presence proves no postcondition: an
apply does not complete on it, and an observation reads it as a positive partial
realization, which the next attempt converges by restarting it. A removal's
resolution does not read the start.

## Adapter boundary

Every managed-service effect crosses the
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary)
through one fixed entrypoint per operation, under
[the adapter result protocol](architecture.md#the-adapter-result-protocol) and
the [process](security.md#process-boundary) and
[Secret-material](security.md#sensitive-material) rules. Both placement arms
use the same role, request and evidence contract, so local execution bypasses
no authorization, privilege or validation.
