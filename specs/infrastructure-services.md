# Managed infrastructure services

Infrastructure services owns the runtime behavior of a managed shared service:
its selection from effective state, the exact implementation it deploys, the
host resources it claims, the evidence that proves completion or absence, and
its inverse. The [kind schemas](api/infrastructure-services.md) own declaration;
[state reconciliation](state-reconciliation.md) owns operations, ordering and
durable records; availability follows [milestones](milestones.md).

A managed service is a [lifecycle capability](state-reconciliation.md#plan-and-execution):
one block per service object, resolved to exactly one implementation whose
identity, content digest and request digest freeze with the plan. The
capability owns what its block means; it never schedules another block,
allocates an operation identity or writes lifecycle state.

## Selection and refusal

A managed service becomes a block only when its placement Machine has effective
`os.provided: true` and the service's declared requirements hold. Reference
resolution alone is not a readiness edge: an endpoint's Machine address supplies
a value, while deployment requires the host itself.

Selection is pure and reads no host, endpoint or Secret material. An
unsupported required capability refuses before operation registration, with one
diagnostic naming every unsupported object, the reason, and a safe next action.
Refusal never registers an operation, reserves a host resource, binds a Secret
or creates a log.

## Placement arms and credentials

| Arm | Selected when | Execution | Coordination |
| --- | --- | --- | --- |
| Controller | The placement Machine is the [Environment controller](api/environment.md#controller-machine) | Local, through the private controller runtime under [Controller's execution boundary](controller.md#egress-and-local-effects) | The Workspace root lock, the context lease, the verified host binding and the host reservations below |
| SSH | The placement Machine authors `access.ssh` | Remote, over SSH from the controller | The context lease only |

The controller arm requires this context's verified
[host binding](contexts.md#controller-relationship-and-host-binding) and the
setup receipt whose automation identity matches the running executable. A
missing, contradictory or stale binding refuses before effects and names
`bastion setup`.

The SSH arm consumes authored `access.ssh.auth.privateKeyRef`, required
`access.ssh.knownHostsRef` and any `access.ssh.sudoPasswordRef`, binding the
effective user and every Secret version with the plan. `passwordRef`,
`auth.operatorIdentity` and the global borrowed-SSH flags are unsupported and
refuse before planning. The bound `knownHostsRef` material is parsed before
connection under [the Machine host-key rules](api/machines.md#addresses-and-access);
a changed, missing, corrupt or unparseable entry refuses before the connection,
and an observed key mismatch refuses before any remote command. No SSH
configuration, agent, ambient `known_hosts` or user identity is consulted.

Two contexts targeting one SSH host are not coordinated. Only the controller
arm claims the host reservations below.

## Host reservations

A capability that places effects on the controller declares stable conflict
identities before its first effect. Workspace stores them with the shared
controller record, so a second context refuses rather than silently taking a
port, unit or path that another context owns.

A reservation key is one of:

| Key | Claims |
| --- | --- |
| `socket:<address>:<port>` | One effective listening socket on every transport, so a service listening on UDP and TCP at one port holds one key. A wildcard bind address claims every declared endpoint address at that port and additionally conflicts with any other bind address at that port. |
| `unit:<name>` | One host service-manager unit and its container name. |
| `path:<absolute path>` | One owned directory tree. |

Reservations are published under the root lock before the operation's first
effect and released only by a completed destroy. A key held by another context
refuses `controller.conflict`, naming the holding context and the key class
without private paths. A context's own keys are replaced by its own apply. An
interrupted apply leaves its reservation in place; the same context's next
apply replaces it, and another context's apply keeps refusing until that
operation is continued or destroyed.

Dependency readiness never establishes service ownership, and
[controller setup](controller.md#host-identity-and-shared-prerequisites)
reserves nothing.

## Managed artifact serving

The `ArtifactServer` capability serves static content over the declared
listeners. It owns its content root and serves it empty; publishing content
into that root is separate future work and cannot append to a frozen plan.

**Implementation.** One container image runs an HTTP server under the host
service manager with host networking, so declared bind addresses and ports are
the real sockets. The image is `spec.image.local`, else `spec.image.public`,
else the executable's compiled default. Every reference resolves to an
immutable content digest before the plan freezes; a floating tag is refused.
Image acquisition uses the placement Machine's normalized
[proxy choice](api/machines.md#machine-proxy) and no ambient proxy variable.
`retention: install-only` is unsupported and refuses.

**Owned host state.** The capability owns exactly one content root per service,
one server configuration, one unit definition and, when any listener is HTTPS,
one certificate and private key beneath that root. Directories are `0755`
except the private key's directory, which is `0700`; the private key is `0600`.
The content root is outside the Bootwright state root, so serving never exposes
context storage. Nothing else on the host is created, modified or removed.

**TLS.** A serving certificate is required when any effective listener uses
HTTPS and is forbidden otherwise. Its bound material is validated before
effects: bounded PEM parsing, certificate and key agreement, validity at the
injected clock, server-authentication suitability, not a certificate authority,
and subject-alternative-name coverage of every address an HTTPS endpoint
serves. A failure refuses before connection or installation and names the
Secret and the unmet condition, never material or its digest. `tls.minVersion`
selects the exact protocol floor the server enforces.

**Readiness.** Completion requires positive evidence for every listener: the
socket accepts a connection at the declared address and port; an HTTPS listener
completes a handshake whose presented leaf certificate digest equals the bound
certificate's; and the listener answers one bounded HTTP status line. A
certificate mismatch or a refused connection after the service reports started
is a definite failure. A socket that never answers within the bounded readiness
window is unknown, not failure.

**Replay.** An apply whose frozen request already matches the live host reports
`completed` with the same completion evidence and no change. Configuration
bytes that differ restart the service; identical bytes do not.

**Inverse.** Destroy stops the service, removes the unit definition, removes
the container, removes the owned content root and then reobserves. Positive
absence requires the unit absent, the container absent, the content root absent
and every reserved socket free. An already-absent service reports `completed`
with that same absence evidence. Destroy removes no image from the host store
and no unrelated file.

**Unknown resolution.** Observation is read-only against the frozen request and
exact identity. Live state matching the frozen request in full is positive
completion; nothing present, with a before-state that recorded nothing, is
positive no effect; anything else, including a partial or contradictory
observation, remains unknown.

**Cancellation.** Cancellation stops authorization of new effects and
terminates the owned process tree. An attempt whose effect was already
authorized becomes unknown unless positive evidence already proves its outcome.

## Managed network services

`Proxy`, `DNSServer` and `NTPServer` share one capability: each runs one
container under the host service manager with host networking, serving one
declared port on its declared bind address. They differ only in the daemon they
run, the configuration derived for it, and the answer readiness proves. Their
plan blocks belong to the
[`infra-components` stage](state-reconciliation.md#stages-and-the-pause-boundary)
and declare no dependency, so the whole set applies in one pass.

**Implementation.** One container image per kind, selected as
`spec.image.local`, else `spec.image.public`, else the executable's compiled
default. Every reference resolves to an immutable content digest before the
plan freezes; a floating tag is refused. Image acquisition uses the placement
Machine's normalized [proxy choice](api/machines.md#machine-proxy) and no
ambient proxy variable. The unit never uses the image's own entrypoint: the
frozen request alone decides what runs.

**Owned host state.** Each service owns one content root, one daemon
configuration inside it and one unit definition. Directories are `0755`. The
content root is outside the Bootwright state root. Nothing else on the host is
created, modified or removed, and no managed service writes to the host's own
resolver, proxy or time configuration.

**Derived configuration.** A managed service is configured from the selected
graph, never from authored daemon syntax:

| Kind | Derived from the graph | Authored |
| --- | --- | --- |
| `Proxy` | The client set below, as the only addresses it serves. Caching and authentication are disabled. | `bindAddress`, `port` |
| `DNSServer` | One address record per retained Machine, from its effective `fqdn` contact and its declared IP addresses. | `bindAddress`, `port`, `forwarders[]` |
| `NTPServer` | The client set below, as the only addresses it answers. | `bindAddress`, `port`, `upstreamSources[]` |

The client set is loopback, every selected `NetworkConfig.spec.machineNetwork`
CIDR, and every retained Machine IP address as a single-host prefix, sorted and
deduplicated. A managed proxy or time service is therefore never planned open
to the world.

A `DNSServer` with no authored forwarder answers only its own records rather
than reaching an ambient upstream. `additionalIngressHosts` is frozen in the
request and produces no record until a cluster capability supplies the ingress
address it would answer with. A managed `NTPServer` serves time without
disciplining its host's clock, so it coexists with the host's own time service.

**Readiness.** Completion requires a positive answer on every address the
service serves: the declared bind address, or every declared endpoint address
when the bind address is a wildcard. A proxy answers a bounded HTTP request
with a well-formed status line; a resolver answers one of its own records over
both UDP and TCP; a time service returns a server-mode reply, whose stratum may
show it unsynchronized without being a failure. An address that never answers
within the bounded readiness window is unknown, not failure.

**Replay.** An apply whose frozen request already matches the live host reports
`completed` with the same completion evidence and no change. Configuration
bytes that differ restart the service; identical bytes do not.

**Inverse.** Destroy stops the service, removes the unit definition, removes
the container, removes the owned content root and then reobserves. Positive
absence requires the unit absent, the container absent, the content root absent
and every reserved socket free. An already-absent service reports `completed`
with that same absence evidence. Destroy removes no image from the host store
and no unrelated file.

**Unknown resolution and cancellation.** Both follow the artifact server's
rules above: observation is read-only against the frozen request and exact
identity, and cancellation terminates the owned process tree, leaving an
already-authorized effect unknown unless positive evidence proves its outcome.

## Adapter boundary

Every managed-service effect crosses the
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary)
through one fixed entrypoint per operation. Go freezes the request, authorizes
each phase and validates the returned evidence strictly; the adapter returns
bounded structured results and never chooses targets, implementations or
workflow. Both placement arms use the same role, request and evidence contract,
so local execution bypasses no authorization, privilege or validation.

Secret material reaches the adapter only through operation-scoped `0600` files
beneath a `0700` directory that is removed after the run, never through
arguments, environment, inventory, evidence or logs. Raw adapter output is not
a product result.
