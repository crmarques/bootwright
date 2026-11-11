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

The selector follows the [domain template](architecture.md#implementation-selection):
the service kind with its `spec.implementation`, which an ArtifactServer lacks
and an external service forbids ([API](api/infrastructure-services.md)).

A managed service becomes a block only when its placement Machine has effective
`os.provided: true`, or is one Bootwright installs, whose installation its
block then requires, and the service's declared requirements hold; admission
refuses another placement Machine at `$.spec.machineRef`
([placement](api/infrastructure-services.md#management-and-placement)). Reference
resolution alone is not a readiness edge: an endpoint's Machine address supplies
a value, while deployment requires the host itself. One realization edge does
exist: a service whose bind address is the host address of a
[managed `Network`](substrates.md#provider-host-realization) on its
placement Machine names that Network's provider as a requirement, because the socket
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
`Unsupported` to every row except the endpoint-address bound, which only a
Proxy, DNSServer, NTPServer or Registry declares, and
`TestManagedServiceRefusalTableMatchesUnsupported` holds the Proxy, DNSServer,
NTPServer and Registry capabilities' to every row except the artifact socket bound,
which only an ArtifactServer declares. Reason and
remedy are the diagnostic's message and remediation (`lifecycle.unsupported` on
the refused service), in which `<server>` is the refused service, `<machine>`
its placement Machine and `<proxy>` the Proxy that Machine selects. The
installed-host rows hold the ArtifactServer alone, except the firewall row, and
the own-installation row the DNSServer and NTPServer alone; `<profile>`,
`<kind>`, `<consumer>` and `<provider>` name the install profile, the
service's kind, the publishing object and the provider; the image copy tool
row holds the Registry alone. A placement
Machine that is the Environment's controller is reached locally, so the SSH
rows never refuse a service placed there.

| Refusal | Path | Reason | Remedy |
| --- | --- | --- | --- |
| Managed Proxy egress | `spec.proxy.proxyRef` of the placement `Machine` names a managed `Proxy` | `this executable acquires a managed service's image only directly or through an external Proxy that is already ready, and <machine>, where <server> runs, selects the managed <proxy> in spec.proxy.proxyRef` | `select direct: {} or an external Proxy in spec.proxy on <machine>, or place <server> on another Machine` |
| Proxy authentication | `spec.connection.auth.proxyAuthRef` on the external `Proxy` the placement `Machine` selects | `this executable acquires a managed service's image through no authenticated proxy, and <proxy>, which <machine> selects for <server>, sets spec.connection.auth.proxyAuthRef` | `select direct: {} or an external Proxy that needs no authentication in spec.proxy on <machine>` |
| Private trust | `spec.connection.trustBundleRef` on the external `Proxy` the placement `Machine` selects | `this executable acquires a managed service's image with the host's system trust store alone, and <proxy>, which <machine> selects for <server>, sets spec.connection.trustBundleRef` | `select direct: {} or an external Proxy without spec.connection.trustBundleRef in spec.proxy on <machine>` |
| Operator SSH identity | `spec.access.ssh.auth.operatorIdentity` on a placement `Machine` other than the controller, the default of a provided Machine without authored access included | `this executable reaches a placement host other than the controller only with a bound SSH private key, and <machine>, where <server> runs, uses spec.access.ssh.auth.operatorIdentity` | `author spec.access.ssh.auth.privateKeyRef and spec.access.ssh.knownHostsRef on <machine>, or place <server> on the controller Machine` |
| Password SSH authentication | `spec.access.ssh.auth.passwordRef` on a placement `Machine` other than the controller | `this executable reaches a placement host other than the controller only with a bound SSH private key, and <machine>, where <server> runs, uses spec.access.ssh.auth.passwordRef` | `author spec.access.ssh.auth.privateKeyRef and spec.access.ssh.knownHostsRef on <machine>, or place <server> on the controller Machine` |
| No bound host key | `spec.access.ssh` without `knownHostsRef` on a placement `Machine` other than the controller | `this executable connects to a placement host other than the controller only against a bound SSH host key, and <machine>, where <server> runs, declares no spec.access.ssh.knownHostsRef` | `author spec.access.ssh.knownHostsRef on <machine>, or place <server> on the controller Machine` |
| Host firewall on an installed Machine | the placement `Machine` is installed by Bootwright through a `MachineInstallProfile` whose `spec.customizations.security.firewall.enabled` is absent | `a managed service on an installed host requires an explicit firewall choice, and <machine>, where <server> runs, is installed by <profile>, which leaves its firewall choice implicit` | `set spec.customizations.security.firewall.enabled: true or false on <profile>, or place <server> on an OS-ready Machine` |
| Service of its host's own installation | a managed `DNSServer` or `NTPServer` on an installed `Machine` that the `dns` selections of that Machine's network configuration, or its installation's `ntp` selections, name | `the installation of <machine> uses <server>, which runs on <machine> and so answers only once that installation completes` | `select a <kind> placed on another Machine for the installation of <machine>, or place <server> on another Machine` |
| Artifact server on an installed Machine | a managed `ArtifactServer` on an installed `Machine` that `spec.mirror.artifacts.serverRef` does not select | `an ArtifactServer on a Machine Bootwright installs serves only the Environment's managed artifact mirror, and <server> on <machine> is not the server spec.mirror.artifacts.serverRef selects` | `place <server> on the controller Machine or on an OS-ready Machine reached over SSH` |
| Publication into a mirror on an installed Machine | an `artifactServerEndpoint` naming the managed artifact mirror's server on an installed `Machine` | `the artifact mirror on a Machine Bootwright installs is filled by its own fill alone, and <consumer> publishes into <server> on <machine> through an artifactServerEndpoint` | `select a server on the controller Machine or on an OS-ready Machine in that artifactServerEndpoint of <consumer>` |
| Mirror host installed off libvirt | the managed artifact mirror's server on a `Machine` Bootwright installs through a vSphere, KubeVirt or bare-metal `InfraProvider` | `the artifact mirror is filled before the controller stage installs its clients, and <machine>, where <server> runs, is installed through <provider>, whose machine blocks this executable runs only after that stage` | `place <server> on an OS-ready Machine or on one a libvirt provider realizes` |
| Image copy tool on an installed Machine | the managed `Registry` `spec.mirror.registry.registryRef` selects on a `Machine` whose install profile lacks `skopeo` in `spec.customizations.packages.install` | `the image mirror's fill copies its images with skopeo, and <machine>, where <server> runs, is installed by <profile>, which does not install skopeo` | `add skopeo to spec.customizations.packages.install of <profile>, or place <server> on an OS-ready Machine` |
| Container runtime on an installed Machine | the placement `Machine`'s install profile without `podman` in `spec.customizations.packages.install` | `a managed service runs as a container, and <machine>, where <server> runs, is installed by <profile>, which does not install podman` | `add podman to spec.customizations.packages.install of <profile>, or place <server> on an OS-ready Machine` |
| Endpoint addresses beyond the host reservation | a managed `Proxy`, `DNSServer`, `NTPServer` or `Registry` bound to a wildcard with more than 61 distinct endpoint addresses | `a managed service bound to a wildcard claims one host reservation key for each distinct endpoint address beside its unit, its content root and its bind socket, within the 64 keys one reservation holds, so it declares at most 61 distinct endpoint addresses` | `declare at most 61 distinct endpoint addresses in spec.endpoints of <server>` |
| Artifact sockets beyond the host reservation | a managed `ArtifactServer` whose listeners and, under a wildcard `bindAddress`, distinct endpoint addresses of each listener claim more than 62 sockets | `an artifact server claims one host reservation key for each listener and, under a wildcard bindAddress, for each distinct endpoint address of each listener, beside its unit and its content root, within the 64 keys one reservation holds, so its listeners and endpoints claim at most 62 sockets` | `declare fewer listeners in spec.listeners or fewer endpoints with distinct addresses in spec.endpoints of <server>, so they claim at most 62 sockets` |

## Placement arms and credentials

| Arm | Selected when | Execution | Coordination |
| --- | --- | --- | --- |
| Controller | The placement Machine is the [Environment controller](api/environment.md#controller-machine) | Local, through the private controller runtime under [Controller's execution boundary](controller.md#the-controller-runtime) | The Workspace root lock, the context lease, the verified host binding and the host reservations below |
| SSH | The placement Machine authors `access.ssh` | Remote, over SSH from the controller | The context lease and the SSH-qualified [host reservations](#host-reservations) below |
| Installed | The placement Machine is one Bootwright installs | Remote, over SSH from the controller, after its installation | The context lease and host reservations qualified by its address and port |

The controller arm requires this context's verified
[host binding](contexts.md#controller-relationship-and-host-binding) and the
controller runtime of the running build's lock, which a completed setup
publishes. A missing, contradictory or stale binding refuses before effects
and names `controller install`.

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
and an observed key mismatch refuses before any remote command. The adapter
runner checks every SSH-placed run, of any operation, before it writes the
job: material that is not exactly one valid entry for the placement's address
and port, such as several entries, a hashed, listed, patterned or marked host,
another host or port, or no decodable key of an accepted type, refuses
`trust.identity` naming the Secret and the Machine, and a run whose pinned
known-hosts file is not its placement's own `knownHostsRef` refuses
`lifecycle.state`; no ssh reads either
(`TestAnSSHPlacementRefusesKnownHostsNotOneEntryForItsHostBeforeItConnects`).
The rule holds for every run, a continuation and a removal included: an
operation whose frozen Secret holds more than one entry refuses until its
Secret is corrected and bound again. The host token
compares as OpenSSH compares it, a name without case and an IP in its
canonical form (`TestAKnownHostsHostMatchesAsOpenSSHReadsIt`), and ssh is
handed only the validated entry, rewritten as one line for the placement's
address and port, never the Secret's own bytes.
The material is read as OpenSSH reads it, each line trimmed and split at
spaces and tabs only: any other control character, a carriage return
included, or any byte outside ASCII refuses, since a byte Go would read as
white space and OpenSSH as part of a host could hide a second entry, such as
`\v#x,*`, that ssh honours for every host
(`TestKnownHostsMaterialOpenSSHReadsOtherwiseRefuses`). No SSH
configuration, agent, ambient `known_hosts` or user identity is consulted.

The installed arm reaches a managed service's, artifact server's or fill's
Machine that Bootwright installs at its install address and port 22, as the
`bootwright` account with the Environment's fleet key, and becomes root
through the passwordless sudo rule its installation wrote; no password and no
`knownHostsRef` is bound. Its block requires the Machine's `os-install`
block, and each run is lent, as its one known-hosts entry, the host key that
installation's evidence proved, read through a reader composition binds
(`TestAnInstalledPlacementConnectsOnlyAgainstItsProvedHostKey`). A consumer
publishing into a server keeps the SSH arm and so cannot reach such a Machine.

Two contexts targeting one SSH host are coordinated by the
[host reservations](#host-reservations) below, which an SSH host's blocks
publish qualified by that host's declared address and port, and again by the
SSH host key its Machine pins.

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
| `bridge-use:<name>` | A shared claim that a provider's libvirt segment uses one host bridge. It conflicts with nothing at acquisition, and a removal that would release the exclusive `bridge:<name>` refuses `controller.conflict` while another context holds it, naming that context: when the destroy registers, and again under the root lock immediately before the removal's effect on the provider holding the bridge, so a claim made after registration refuses that effect with nothing performed. The re-proof that admits the effect marks the holder's claim releasing in the same root-locked publication, and a claim of `bridge-use:<name>` by another context then refuses `controller.conflict` until the removal completes or the holder is deleted, naming the removing context, the bridge and its holder, with the remedy to complete that removal with `bootwright destroy --context <holder>`; an apply of the holder refuses while its destroy is incomplete, so only its completion or deletion clears the mark. The mark is durable, so an interrupted effect keeps refusing (`TestAClaimOnAKeyARemovalIsReleasingRefusesUntilTheRemovalCompletes`). One an abandoned context held is released with every other key of that context when it is deleted (D38), so it strands a removal only while the using context exists. |
| `libvirt-network:<name>` | One libvirt network definition. |
| `libvirt-pool:<name>` | One libvirt storage pool definition. |
| `prefix:<masked prefix>` | One managed libvirt network's routed prefix, IPv4 or IPv6, written masked (`prefix:198.51.100.0/24`); it conflicts with every held prefix it overlaps, identical or nested, because the host routes each to its own bridge. |
| `libvirt-domain:<name>` | One libvirt domain definition. |
| `kubevirt-vm:<server>/<namespace>/<name>` | One KubeVirt VirtualMachine, by the host cluster's API server, namespace and name. |
| `bmc:<host>:<port>/<system>` | One [physical machine](substrates.md#physical-machine-realization), named by its management-controller endpoint, normalized for the key alone as that page states (the port it names or else its scheme's default), and exact ComputerSystem, so that two contexts of one controller store never [drive one server by its BMC](substrates.md#physical-machine-realization) at once. The claim serializes use; it is never ownership of the machine and authorizes nothing about it. An [emulated BMC's](substrates.md#machine-realization) machine block holds the same key shared: it conflicts with no other shared holder, its registration, which carries this Machine's installation, refuses `controller.conflict` while another context holds the key exclusively, a physical Machine's registration refuses while another context holds it shared (`TestASharedAndAnExclusiveBMCKeyConflictInEitherOrder`), and so does its release, re-proved like `bridge-use:<name>` immediately before the removal's effect, which marks the shared claim releasing as it marks a bridge, so an exclusive claim of the key refuses until the removal completes. A [machine power command](cli.md#machine-power-operations) refuses while another context holds the key it drives, under the same relation. |
| `bmc-system:<UUID>` | The same physical machine, named by the `UUID` its proof records in lower case, claimed once the proof records it and before any media is inserted, so a second spelling of one server's address refuses. Its `bmc-mac:` keys are claimed beside it. |
| `bmc-mac:<mac>` | The same physical machine, named by one MAC address it declares, in its colon-separated lower-case form: every proved system, with or without a `UUID`, one that reports a serial number alone included, claims one exclusive key per declared MAC once the proof holds and before any media is inserted, so a second spelling of its address refuses, and so does a pin-less [machine power command](cli.md#machine-power-operations) that knows the declared MACs but not the `UUID` (D252 (2), D253). An OS-ready Machine reached over SSH claims one per permanent hardware address its host reports, unqualified, before any block placed on it starts ([OS-ready server claim](substrates.md#physical-machine-realization), D257 (2)). |
| `media:<filename.iso>` | A shared claim on one image of the [media store](managed-os.md#media-store); it conflicts with nothing and blocks only that image's deletion or replacement while any context holds it. |

A `socket:` key writes an IPv6 address unbracketed, the one form every
capability claims, while a plan's listener impact names the socket as an
address and port are written, an IPv6 address bracketed, as
`open-listener [fd00::1]:3128` (`TestIPv6ListenerImpactsAreBracketed`,
`TestIPv6ArtifactListenerImpactsAreBracketed`).

One reservation holds at most 64 keys ([controller
record](contexts/controller-record.md#bounds)), and a managed service's claim is
its `unit:` key, its `path:` key and its distinct `socket:` keys, so it claims at
most 62 sockets. A Proxy, DNSServer, NTPServer or Registry bound to a wildcard therefore
declares at most 61 distinct endpoint addresses beside its bind socket, and an
ArtifactServer's listeners and, under a wildcard bind, the distinct endpoint
addresses of each listener claim at most 62 sockets. Admission refuses more with
`api.value`, at `$.spec.endpoints`, or at `$.spec.listeners` for an
ArtifactServer bound to one address. The refusal names the bound and the count,
and it applies wherever the service is placed. Selection keeps the refusal
before registration for state that bypassed admission ([refusal
table](#refusal-table))
(`TestAWildcardServiceDeclaresAtMostTheEndpointAddressesItsReservationHolds`,
`TestAnArtifactServerClaimsAtMostTheSocketsItsReservationHolds`,
`TestTheReservationSocketBoundKeepsAClaimWithinItsKeys`,
`TestTheArtifactSocketBoundKeepsAClaimWithinItsKeys`).

A controller record holds at most 256 reservations ([controller
record](contexts/controller-record.md#bounds)). An apply whose
reservations would pass that bound refuses before registration with
`controller.conflict`, naming the bound and the count it needs, and its remedy
is `bootwright destroy --context <name>` or `bootwright context delete
--name <name> --purge` of a context this host no longer needs
(`TestReservationsPastTheRecordBoundRefuseBeforePublication`).

Every key is exclusive except the classes marked shared. Each key class has
one conflict relation, stated in the table above, which Workspace applies when
it publishes a context's entries through the controller prerequisites port
([controller record](contexts/controller-record.md#record)). Reservations are
published under the root lock before the operation's first effect. Only the
holding context releases them, by one of these paths:

| Release path | Proof |
| --- | --- |
| A completed destroy | Its finalization, after every block's absence evidence, releases the context's keys with its Secret bindings. |
| A destroy settling an interrupted registration | It finds the keys held by no operation, because the registration that published them was [interrupted](state-reconciliation.md#lifecycle-unit). |
| The context's own next apply | It replaces the context's keys, or finishes the removal they sit beside. |
| The [permanent deletion](contexts.md#permanent-deletion) that abandons the context's objects, under the orphan acknowledgement | The acknowledgement waives the disposal verdict. The deletion drops every key the context still holds while the objects those keys protected stay on the host, so another context may then claim the same keys. |

A requested exclusive
claim that conflicts with claims another context holds refuses with one
`controller.conflict` diagnostic per held claim it conflicts with. Each names the holding context; the held key
for `socket:`, `bridge:` and `prefix:` keys, and only the class for any other,
so no private path prints; the holder's object; and this context's object,
each by the API kind its claim records, else the one its capability kind
names, else by that capability kind and service
([record](contexts/controller-record.md#record)). Its
remedy is `bootwright destroy --context <holder>`, which releases the keys the
holder still holds, or the field of this context's declaration that chooses
the key (`TestEachConflictingHeldClaimIsItsOwnDiagnostic`). A
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
listen; claims on two hosts never conflict.

Every claim placed over SSH is also published (Q96). One whose placement is
the controller is published as the controller's own, with its keys as they
are. Any other, a managed service's, an artifact server's, an
[emulated BMC's](substrates.md#machine-realization) or a libvirt provider
host's, is published with every key but its `bmc:`, `bmc-system:` and
`bmc-mac:` keys qualified by its SSH host, `ssh/<address>:<port>/<key>`, the
address and port written as the host is named above with an IPv6 address
unbracketed, and holds and releases those
keys by the controller's conflict relation and release paths, compared only
with keys of the same qualifier. Every such key is also published qualified
by the host key that Machine's `access.ssh.knownHostsRef` pins (D188),
`sshkey/SHA256:<digest>/<key>`, the SHA-256 digest of the key blob in
unpadded URL-safe base64 (`ssh-keygen -l` shows it with `+` for `-` and `/`
for `_`). Those three key classes name an endpoint the controller dials or a
machine it drives, not a resource of the SSH host, so every placement publishes
them unqualified and an emulated BMC on an SSH host meets a physical Machine's
claim of its endpoint
(`TestAnSSHHostedEmulatedBMCConflictsWithAPhysicalMachineAtItsEndpoint`). Two
contexts of one store targeting one SSH host therefore refuse each other's
conflicting claims there, whether they reach it by one address or by two
addresses, or a name and an address, that pin one host key; a diagnostic on
the host-key form names the host by that fingerprint
(`TestTwoSpellingsOfOneSSHHostConflictByItsPinnedHostKey`). A pinned host key
names one host, so two hosts sharing one key, as a cloned image without
regenerated keys gives, are one host and refuse each other's conflicting
claims, and that refusal's remedy adds giving each host its own key. A key
conflicting under both forms is one conflict and one diagnostic. The pinned key is
read from the bound known-hosts material when the apply registers, and every
SSH placement declares one ([placement](#placement-arms-and-credentials)):
material that binds no readable key for the placement's address and port
refuses `trust.identity` before anything is reserved or registered, naming
the Secret and the Machine, so a claim is never published under its address
alone. This enforces the one-entry rule of
[addresses and access](api/machines.md#addresses-and-access) for a placement
Machine whose blocks publish claims at the apply's registration, as the
adapter runner enforces it for every placement before it connects: a Secret
holding several entries, a host list, a hashed host or a host token other
than the placement's address and port refuses there
(`TestAnSSHClaimWhosePinnedHostKeyCannotBeReadRefusesBeforeItIsPublished`).
A qualified key past the record's 256-byte key bound refuses `api.value` while
the context plans: under the address form naming the Machine whose SSH
address makes it long, under the host-key form, whose qualifier is 58 bytes,
naming the claim and its Machine
(`TestAClaimTooLongToKeyByItsHostKeyRefuses`). A placement that reaches the
controller at port `22` on an address its controller Machine declares
publishes the controller's own unqualified keys. A placement whose blocks
publish claims and that reaches the controller any other way refuses at the
apply's registration: the controller's
own SSH host keys are read, read-only and through held root-owned directories,
from its sshd public key files `/etc/ssh/ssh_host_ed25519_key.pub`,
`/etc/ssh/ssh_host_ecdsa_key.pub` and `/etc/ssh/ssh_host_rsa_key.pub`, and a
placement whose pinned host key is one of them, reached at another address or
port, refuses `lifecycle.state` before anything is reserved or registered,
naming the addresses the controller Machine declares; its remedy is to reach
the controller at port `22` on one of them, to declare the address on the
controller Machine when the port is `22`, to place the work on the
controller, or, when the placement host only shares the controller's keys as
a clone does, to give it its own SSH host keys; an IPv6 address is named and
compared as any other
(`TestTheControllerReachedByAnUndeclaredAddressRefusesBeforeReservation`).
A key file that is absent, unreadable, not a root-owned regular file that
only root may write, or not one accepted public key gives no key and refuses
nothing, so a controller without sshd host keys refuses no placement
(`TestSSHHostKeysReadsOnlyTheControllersSafePublicKeyFiles`).

One claim's keys never conflict with each other, and
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

A managed service's container runs under `podman`, which nothing installs on a
placement Machine reached over SSH, so the apply first proves
`/usr/bin/podman --version` runs on its host, before any pull, publication or
start. A host where it does not (absent or failing) refuses with
`lifecycle.state` on the service, naming the Machine and the `podman` package;
its remedy is to install that package there with the host's package manager and
repeat the apply, or to place the service on a Machine that runs containers
(`TestAMissingContainerRuntimeRefusalNamesTheMachineAndThePackage`,
`TestAnApplyOnAnSSHPlacementHandsTheRunnerItsContainerRuntimeRefusal`,
`test_a_host_without_podman_refuses_before_any_other_task`). A service placed on
the controller Machine has `podman` from the controller stage
([host packages](controller.md#host-packages)), and one placed on an SSH Machine
that hosts a libvirt provider waits for that provider's host block, which
installs it (`TestAServiceOnAProviderHostMachineReachedOverSSHRequiresTheHostBlock`).
A removal or an observation on a host without `podman` reads no container and
removes what the apply left, so a context whose apply refused is destroyed
without installing it.

Before image acquisition, publication or start, each service with an explicit
bind address also reads its placement host's assigned addresses through a
bounded, complete kernel query. An address absent from that observation, or
present only as tentative or DAD-failed, refuses with `lifecycle.state` on the
service, naming the socket. An unavailable or incomplete query proves no
absence and fails before effects. Wildcard binds keep their existing readiness
contract. This check assigns no address and changes no interface. A service
on a managed bridge still waits for the provider that creates that bridge.
The remedy restores the declared address or retires the frozen operation with
its original build before importing corrected addresses, endpoints and TLS
certificate names. Tests include
`TestUnassignedBindRefusalNamesTheServiceAndFrozenInputRecovery`.

## Managed artifact serving

The `ArtifactServer` capability serves static content over the declared
listeners. It owns its content root and publishes nothing into it itself; a
consuming capability publishes beneath it under the
[consumer publication contract](#consumer-publication), in a block of the same
plan, never by appending to a frozen one.

**Implementation.** One container image runs an HTTP server under the host
service manager with host networking, so declared bind addresses and ports are
the real sockets. The image is `spec.image.local`, else `spec.image.public`,
else the executable's compiled default; `local` and `public` are two
references the operator may hold at two places, the executable compares neither
with the other, and an unset one falls through to the next. An authored
reference is pinned by content digest at [admission](api/infrastructure-services.md#management-and-placement)
and the compiled default is one, so a tag never reaches a plan. Under a
declared [image mirror](api/environment.md#image-mirror), a `public` or
compiled reference of a public origin is pulled from the mirror instead.
Image acquisition uses the placement Machine's normalized
[proxy choice](api/machines.md#machine-proxy) and no ambient proxy variable;
a managed, authenticated or private-trust Proxy refuses before registration, as
the [refusal table](#refusal-table) states.
`retention` admits `persistent` alone: the server keeps its published content
until destroy.

**Owned host state.** The capability owns exactly one content root per service,
one server configuration, one unit definition and, when any listener is HTTPS,
one certificate and private key beneath that root. Directories are `0755`
except the private key's directory, which is `0700`; the private key is `0600`.
The content root lies in the context's [hosted directory](contexts.md#hosted-root),
outside the Bootwright state root, so serving never exposes context storage.
The apply proves and creates the hosted root's, the context's, `services` and
the kind's directories that hold the content root, each a root-owned `0700`
directory, and the destroy removes each beneath the hosted root once it is
empty, never recursively, and retains the hosted root itself
(`test_a_destroy_removes_each_empty_parent_never_recursively`). Its apply and
its destroy each refuse, as their first check and so before any effect or
removal, a content root that is not exactly
`/var/lib/bootwright-hosted/<context>/services/<kind>/<name>` in normal form:
one with a `.` or `..` component, an empty component from a repeated or
trailing slash, or another depth
(`test_a_content_root_not_in_normal_form_refuses_before_any_removal`). Nothing
else on the host is created, modified or removed.

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

**Readiness.** `startedAfterFiles` proves the container started no earlier than
the unit, nginx configuration and, for HTTPS only, certificate and key were
published. Missing or unreadable metadata proves no completion; an otherwise
present service reports partial realization, and a contradictory completion
claim refuses. Removal ignores freshness. Every listener also proves: the
socket accepts a connection at the declared address and port; an HTTPS listener
completes a handshake whose presented leaf certificate digest equals the bound
certificate's; and the listener answers one bounded HTTP status line. A
listener is probed at the bind address, or under a wildcard bind at each
endpoint address that names it; admission refuses a wildcard bind with a
listener no endpoint names, so every listener has a probe target. A certificate
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
Everything it owns carries the context in its name, so a same-name object
another live context created cannot occur without a reservation conflict
refusing first. An object an abandoned context of the same name left on the
host reads as this context's, because no native identity yet distinguishes the
two.

Not yet met: a context incarnation enters no ownership marker; tracked as
[B568](milestones/backlog.md#b568).

**Inverse.** Destroy stops the service, removes the unit definition, resets
the failed state systemd still lists the unit in, as a unit whose daemon exited
at each start until its start limit is left
(`test_a_destroy_resets_a_unit_it_finds_failed`), removes the container,
removes the owned content root and then reobserves. Positive absence requires
the unit absent, neither defined nor listed failed, the container absent, the
content root absent and every reserved socket free. An already-absent service reports `completed`
with that same absence evidence. Destroy removes no image from the host store
and no unrelated file.

Not yet met: managed-service and artifact-server absence checks release socket
reservations without proving every reserved socket free; tracked as
[B323](milestones/backlog.md#b323).

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
Secret. A fetch that fails before any status line for a cause other than a
certificate that did not verify (a timeout, or a refused, reset or unreachable
connection) is made again at most twice more, five seconds apart, so a short
network drop on the placement Machine does not fail the block; a status or a
certificate failure fails it at once, with the last attempt's cause. The
request names the token, so neither it nor the response reaches output; only
that cause does, with the token redacted. The proof precedes
anything that lets a later attempt treat the publication as complete.

The physical installation proves its `install.iso`, `identity` and
`identity.pub` this way, each before any media is inserted. An encrypting
managed-OS installation proves its `install.iso` this way before the insert,
and its `recovery` right before it, when its boot publishes it.

A consumer that hands its private URL to a remote fetcher, such as a
management controller it asks to fetch an image, sends it only over a
verified channel, because whoever reads that channel holds the token.

Published material is removed as soon as the work that needed it completes,
and its absence is part of that block's completion evidence, because material
that existed for one boot must not outlive it. An observation that finds a
private tree whose work has completed treats it as unfinished work to converge,
and the inverse removes it, except where the consumer states that its content
is needed for as long as its own block is retained.

Two consumers use it. A [physical installation](managed-os.md#physical-private-publication)
delivers the host key its machine will present, which is material Bootwright
generated, and the installer image whose Kickstart names where that key is
fetched; neither outlives the boot that installs it. A managed-OS installation
that [encrypts its root disk](managed-os.md#root-disk-encryption) delivers the
same way its recovery passphrase and the installer image whose Kickstart names
where it is fetched, both withdrawn when its installation completes. A
[container cluster](openshift-clusters.md#boot-media) publishes its agent boot
image, which embeds the operator's own pull secret in its ignition and is
retained while the cluster's media block is, because a node may be booted from
it again. A cluster's per-insert tokens are specified in
[installation](openshift-clusters.md#installation). Any further use is
specified before it is built.

## Managed network services

`Proxy`, `DNSServer`, `NTPServer` and a managed `Registry` share one
capability: each runs one container under the host service manager with host
networking, serving one declared port on its declared bind address. They differ
only in the daemon they run, the configuration derived for it, the material it
serves from and the answer readiness proves; the [managed registry](#managed-registry)
states what the Registry adds. Their plan blocks belong to the
[`infrastructure-services` stage](state-reconciliation.md#stages-and-the-pause-boundary).

One role realizes all four kinds, keyed by the request's frozen
implementation identity: it selects the daemon configuration and unit
templates, and the private directories its content root holds, for that
implementation from a fixed table, and every task is shared. Validation admits
one implementation per kind, so each kind selects exactly one row, and the
selection plans only the objects whose `spec.implementation` matches its
definition. The four kinds freeze one request version,
`managedservice.RequestVersion`, so a request of another version refuses by
naming its version, and its remedy is to destroy the context with the build
that applied it.

**Implementation.** One container image per kind, selected, pinned and acquired
exactly as the [artifact server's](#managed-artifact-serving) is. The unit
never uses the image's own entrypoint: the frozen request alone decides what
runs. Each daemon ends within its unit's stop timeout when stopped: the
proxy's shutdown lifetime is two seconds, below podman's ten-second stop
timeout, so a stop never leaves the unit failed, and a unit that failed before
its destroy is reset by it, as the
[artifact server's inverse](#managed-artifact-serving) states
(`test_the_proxy_stops_within_podmans_stop_timeout`).

**Owned host state.** Each service owns one content root,
`/var/lib/bootwright-hosted/<context>/services/<kind>/<name>`, one daemon
configuration inside it and one unit definition. The content root and its
directories are `0755`; the [hosted directories](contexts.md#hosted-root)
above it are created and removed as the artifact server's are, and the hosted
root itself is retained. Each service's apply and destroy refuse a content
root not in normal form before any effect, as the artifact server's do. Nothing else on the host is created, modified or
removed, and no managed service writes to the
host's own resolver, proxy or time configuration. A controller that must
resolve names a managed DNSServer serves, as a container-cluster installation
polling its cluster does, is routed to that server by the operator;
[container-cluster preflight](openshift-clusters.md#preflight) names each
record and the route.

**Derived configuration.** A managed service is configured from the selected
graph, never from authored daemon syntax:

| Kind | Derived from the graph | Authored |
| --- | --- | --- |
| `Proxy` | The client set below, as the only addresses it serves. Before it admits a client it refuses every request whose destination is a loopback or unspecified address (`127.0.0.0/8`, `0.0.0.0/32`, `::1/128`, `::/128`) or a link-local one (`169.254.0.0/16`, `fe80::/10`), and every request for its cache manager (`test_the_proxy_refuses_local_link_local_and_manager_destinations_before_it_admits_a_client`). Without `allowedDestinations` it admits a client to every other destination and port, `CONNECT` included, and keeps no access log. With it, the destination rule below admits a client only to those destinations and the mirror hosts, and refuses and logs every other request. Caching and authentication are disabled. | `bindAddress`, `port`, `allowedDestinations[]` |
| `DNSServer` | One name per retained Machine, from its effective `fqdn` contact, answering the addresses of that Machine its clients can reach, by the rule below, each as its own record, with the reverse record on the first address only, and the records each selected container cluster answers at. | `bindAddress`, `port`, `forwarders[]` |
| `NTPServer` | The client set below, as the only addresses it answers. | `bindAddress`, `port`, `upstreamSources[]` |

The client set is loopback, the CIDR of each [`Network`](api/networks.md)
holding a retained Machine's install address, in its family, and every
retained Machine IP address as a single-host prefix, sorted and deduplicated
([D240](milestones/backlog.md#d240) (8)). Other Networks, which can hold hosts
outside the graph, and VIPs add nothing, so a managed proxy or time service is
never planned open to the world.

**The proxy's destination rule.** A managed Proxy that declares
[`allowedDestinations`](api/infrastructure-services.md#proxy-dns-ntp-and-registry-deployment)
freezes them in its request, sorted and deduplicated, with the host of each
mirror its Environment declares at the port it serves: `spec.mirror.artifacts.url`'s
host at 443, and the url host of the Registry `spec.mirror.registry.registryRef`
selects at its port, else 443. A Machine routed through the proxy therefore
reaches the mirror the same Environment routes its artifacts to, and an
authored list need not repeat it. The derivation names no other host:
the token and content hosts a registry redirects a pull to are the
operator's to declare.

The proxy evaluates, in order: the cache manager, loopback, unspecified and
link-local refusals above; then one rule per frozen destination, which admits
a derived client when the destination matches and its port is the entry's own
or, for an entry without one, 80 or 443; then a refusal of everything else.
The rules hold plain HTTP and `CONNECT` alike, so an HTTPS tunnel opens only
to a declared destination's admitted port. Every entry, an address included,
is matched against the host the request names and nothing it resolves to
(squid's `dstdomain -n`, which looks nothing up). A request by IP literal
therefore never matches a name through its reverse lookup, and a name never
matches a declared address: a name with several addresses, one of them
declared, could otherwise be admitted and forwarded to another, since squid
rotates the addresses it tries. An IPv6 entry is written both bare and
bracketed, the two spellings squid may store a request's host in; another
spelling of it, such as an uncompressed one, is refused. The local refusals
read every address the destination resolves to, so a declared name that
resolves to loopback or link-local, among others, is still refused
(`test_a_proxy_that_freezes_destinations_admits_only_them`).

The list holds only what reaches the proxy. A Machine that is given another
route ignores its proxy settings: it connects directly where the network
forwards it, or tunnels data through name lookups a resolver forwards for it,
a managed DNSServer's `forwarders` included. The list is a control only where
the network offers no other route, for example a managed libvirt Network
declared `forward: none` for the Machines, with the Proxy on a Machine
attached to it and to a routed network, and a DNSServer that has no
`forwarders`, or forwards only to a resolver that itself answers only the
admitted names.

With destinations frozen, squid writes its access log, every admitted request
and every refusal as `TCP_DENIED/403` with its client, method and destination,
to `/var/log/squid/access.log` inside the container, read on the placement
host with `podman exec <unit> cat /var/log/squid/access.log`; squid strips
query terms from it by default. That directory is a 64 MiB tmpfs the unit
mounts, writable by squid's unprivileged user, so the log never reaches the
host's filesystem; once it is full, squid drops further lines
(`on-error=drop`) rather than stopping, so a client that repeats refused
requests can silence the log but not the proxy or the host. It is not the
container's standard output, because squid opens its logs as its
unprivileged effective user, which a pipe the container runtime created as
root may refuse. The log lives as long as the container, which each restart
replaces, and is not rotated
(`test_a_proxy_that_freezes_destinations_logs_to_a_capped_tmpfs_and_drops_lines_when_full`).
Without them the access log stays off and the rendered configuration is the one an earlier
request version wrote
(`test_a_proxy_that_freezes_no_destination_admits_every_remote_one_and_logs_nothing`).
The apply refuses a frozen destination holding any character outside the
grammar's alphabet, or not exactly one well-formed address or domain with an
optional port in `1..65535`, so no value writes another squid directive
(`test_the_apply_refuses_a_destination_that_could_write_another_squid_directive`).

A Machine's name answers an address only when its clients can dial it: an
address held by no Network (a contact), an interface address on a
[`Network`](api/networks.md) that declares a gateway in the address's family,
and the Machine's installation address
([`network.installAddressRef`](api/machines.md#addresses-and-access), else the
one the Machine's network selects) wherever it lies. Any other interface
address, one on a Network without a gateway, is published under no name, and a
Machine left with no address publishes no record. Members of an unrouted
Network address each other by address, as cephadm does with each host's
`addr`.

A `DNSServer` with no authored forwarder answers only its own records rather
than reaching an ambient upstream. A managed `NTPServer` serves time without
disciplining its host's clock, so it coexists with the host's own time service.
Whenever no declared upstream source is synchronized, it answers from its
host's clock at local stratum 10, so a service with no declared or reachable
upstream still answers
([knowledge](../.agents/knowledge/managed-network-service-runtime.md)).

A selected `OpenShiftCluster` contributes the three names its own installation
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

A retained CephConsumer with an attached object gateway also contributes its
endpoint record through [the consumer DNS contract](ceph-resources.md#attached-gateway-dns).
The declaration owns the contribution and this service owns its publication and
inverse, using the same frozen-record and content ownership as other names. The
storage domain owns the derivation of that record from the consumer's attached
gateway, and this service reads no storage field path: composition binds the
storage derivation to the DNS definition, and the DNS server imports no storage
or container-cluster package.

**Readiness.** Completion requires a positive answer on every address the
service serves: the declared bind address, or every declared endpoint address
when the bind address is a wildcard, for which admission requires an endpoint
(`TestPresenceRequiresEveryDeclaredAnswer`). A proxy
answers a bounded HTTP request with a well-formed status line; a resolver answers every address of every one of its records over
UDP, and over TCP as well; a time service returns a server-mode reply, whose stratum may
show it unsynchronized without being a failure. Readiness therefore proves that
the service answers, not that it is synchronized to an upstream source, and a
consumer of a time service accepts a responding source of either kind. An address that never answers
within the bounded readiness window is unknown, not failure, and is reported
with its cause as [an unproved listener](#managed-artifact-serving) is.

**Replay, inverse and unknown resolution.** Each follows the
[artifact server's](#managed-artifact-serving) rules above, and live state
matches the frozen request in full only while the running container started no
earlier than the last modification of every file it runs from: its daemon
configuration and its unit definition, and a registry's certificate, key and
password file. A service that started before one of
them was last published, or whose start or files cannot be read, runs what the
frozen request no longer describes, so its presence proves no postcondition: an
apply does not complete on it, and an observation reads it as a positive partial
realization, which the next attempt converges by restarting it. A removal's
resolution does not read the start.

## Managed registry

A managed `Registry` with `implementation: distribution` is the fourth kind of
the [shared capability](#managed-network-services), implementation
`registry-distribution-v1`, block `registry-<name>`. Its declaration is the
[managed registry API](api/infrastructure-services.md#managed-registry).

**Implementation.** [CNCF Distribution](https://distribution.github.io/distribution/)
3.1.2, the `registry` image pinned by its multi-architecture index digest as the
compiled default, selected, pinned and acquired as every managed service's image
is, through `bootwright.core.image_acquire` and, under a declared
[image mirror](api/environment.md#image-mirror), from that mirror. Distribution
serves TLS and password-file authentication natively and accepts the deep
repository paths of the mirror layout, `<registry>/<origin host>/<repository>`.
The unit runs `/bin/registry serve` on the derived configuration, never the
image's entrypoint or its development configuration. That configuration serves
HTTPS alone at the bind address and port, with `tls.minVersion` as its protocol
floor; it authenticates every request against the password file; it stores
pushed content under the content root; and it declares no proxy section, so it
never pulls through to an origin, no deletion, and no debug or metrics
listener. No configuration of any Machine names it an insecure registry.

**Owned host state.** Beside the configuration and unit, the content root holds
three `0700` directories: `tls`, with the serving certificate and private key;
`auth`, with the password file; and `data`, the registry's storage, mounted at
`/var/lib/registry`. Each file is `0600` and is written as the
[artifact server's](#managed-artifact-serving) private files are, atomically
from a private sibling, never through a link. On a host that enforces SELinux
the unit pins the container's level, `s0:c<a>,c<b>` with two categories
derived from the unit name, the apply labels `data` `container_file_t` at that
level, and `data` is mounted
without relabeling, so a start never walks a store that may be large and what
one start wrote the next reads. When any entry of `data` carries another label,
as after a forced relabel of the host, the apply relabels the whole store,
recursively, and otherwise never walks it
(`test_the_storage_is_relabeled_whole_only_when_an_entry_carries_another_label`).
Two registries on one host share a level only when their unit names draw the
same two of 1024 categories, about n²/1,000,000 for n registries. The small
`tls`, `auth` and configuration mounts are relabeled at each start as the other
services' are.

**Retention.** What clients push is kept across every apply and observation,
because nothing but the destroy removes the content root, as an
ArtifactServer's `persistent` retention keeps its published content. The
destroy removes the content root with everything stored in it; a later apply
starts an empty registry, and its consumers fill it again.

**TLS.** The serving certificate is proved exactly as an
[ArtifactServer's](#managed-artifact-serving) HTTPS one, by the same checks
before registration and again before any connection or installation, against
every endpoint address, and refuses with the same `secret.part` and
`secret.store` diagnostics; admission refuses a generated certificate that does
not name an endpoint's address
(`TestARegistryProvesItsBoundCertificateAndCredentialBeforeRegistration`,
`TestAManagedRegistryServesHTTPSFromItsCertificateToItsCredential`).

**Credential.** The `usernamePassword` Secret `credentialsRef` names is proved
before registration and again by an apply before any effect: a printable ASCII
username with no colon, which a password file line carries, and a
password of 1 to 72 bytes with no NUL, which bcrypt reads whole. A failure
refuses as `secret.part` on `Secret/<name>`, naming the condition, never the
material, with the remedy `secret set` with `--context`. The password file holds
one `<username>:<hash>` line, the bcrypt (`$2a$`, cost 12) of the password,
derived on the controller in Go (`material.BcryptPasswordFile`), with no
cryptography on the placement host, so
a FIPS-mode host, whose crypt library offers no bcrypt, serves it too. Its salt
is random, as bcrypt's own, so nothing in the file can be computed from the
password and the registry's name, and two contexts with one name and password
hold different files (`TestThePasswordFileIsARandomlySaltedBcryptLine`); a
credential no line can carry is refused there too
(`TestThePasswordFileRefusesACredentialNoLineCanCarry`). Because every
derivation differs, the running registry is its own verifier: before
publishing, the apply asks it for `/v2/` with the bound credential, after
comparing its certificate, and keeps the published file when every probe
target answers, so a replay publishes nothing and restarts nothing; otherwise,
a registry not running or a rotated credential, it publishes the file this
attempt was lent
(`test_the_password_file_is_published_only_when_the_running_registry_refuses_the_credential`).

**Material.** The certificate, the key, the credential and the derived password
file reach the host only as [lent material](security.md#sensitive-material)
inside module arguments, under `no_log`: an apply is lent all five, an
observation only the credential its readiness authenticates with, and a removal
none. The frozen request names both Secrets and carries no part of them, and no
password-derived value reaches a log, the request or the evidence
(`TestARegistryFreezesItsServingAndClientSecretsAndLendsEachOperationItsParts`).
Readiness takes the credential as one opaque base64 token, so the masking of a
hidden argument never alters the evidence it returns
(`test_a_registry_credential_never_masks_its_evidence`), and its task's output
is hidden only for a service with a credential. The registry's access log, in
its unit's journal, names the authenticated username and never the password.

**Readiness.** Each probe target, as the other kinds' are chosen, answers an
authenticated `GET /v2/` over TLS with status `200` and the
`Docker-Distribution-Api-Version: registry/2.0` header, presenting a leaf
certificate whose SHA-256 equals the bound certificate's; the evidence names
that fingerprint. The probe receives the bound fingerprint and compares the
presented certificate before it sends anything, so a listener presenting
another one, such as a host that took a moved address, receives no credential:
it answers `certificate-mismatch` with what it presented, which fails the
attempt rather than leaving it unproved, and a probe that holds no bound
certificate sends nothing
(`test_a_listener_presenting_another_certificate_receives_no_credential`). Any
status but `200`, a `401` included, is a definite listener failure that the
probe never repeats (`test_a_refused_credential_is_definite_and_never_retried`),
and a socket that never answers in the bounded window is unknown
(`TestARegistryIsPresentOnlyWhenItPresentsTheBoundCertificate`). An observation
without bound material proves no presence, so it reads a running registry as a
partial realization the next apply converges, and an unresolved observation is
explained without comparing a certificate.

**Consumers.** The Environment [image mirror](api/environment.md#image-mirror)
may select a managed Registry, which its [mirror host](#mirror-host) fills,
and a cluster's [registry mirror](openshift-clusters.md#installation-features)
is then that Registry.

## Mirror host

The mirror host is the Machine an Environment's managed
[artifact mirror](api/environment.md#artifact-mirror) and managed
[image mirror](api/environment.md#image-mirror) are placed on: an OS-ready
Machine reached over SSH, or one Bootwright installs, whose managed `ArtifactServer`
`spec.mirror.artifacts.serverRef` selects and whose managed `Registry`
`spec.mirror.registry.registryRef` selects. When both are managed they share
one `machineRef`, and plan refuses two Machines
([refusal table](api/environment.md#refusal-table)). It holds exactly what
the acquisition list names: the public-origin clients the controller stage
installs at the releases the build's tool table pins, and the public-origin
images the managed services and emulated BMCs off the mirror host pull, each
by the digest the build pins, and the release of each OpenShift cluster that
installs through the image mirror. It is never a pass-through to a publisher
([controlled network supply](deferred/controlled-network.md#the-mirror-host)).

**Fill.** One block of the `mirror-fill` family, implementation
`mirrorfill.FilesImplementation`, named `mirror-files-<environment>`, runs on
the mirror host in the `infrastructure-services` stage after the server's own
block, and publishes beneath the server's served root at
`mirror/<environment>/<kind>/<release>/<file>`, the path the controller stage
reads. Each file is read from its publisher over verified HTTPS through the
mirror host's frozen egress route, direct or an external Proxy and never an
ambient proxy, into a temporary file whose SHA-256 must equal the build's pin
before it is renamed into place, owned by root, mode `0644` and labeled like
the served root. A file already present at its pinned size and SHA-256 is not
read again, so a replay copies nothing and reports unchanged. A file whose
SHA-256 differs publishes nothing and fails the attempt with `lifecycle.state`
on the server; a publisher the host cannot read fails it with
`lifecycle.unknown` (`TestAFillRefusesAFileItCouldNotVerify`). The evidence
names, in request order, each file the mirror holds and whether its size and
SHA-256 match the pin; a held file the request does not name in that place is
unknown (`TestTheFillReadsItsEffectFromTheFilesTheMirrorHolds`). The fill's
removal deletes `mirror/<environment>` alone, before the server's own removal.
No Secret material reaches the mirror host beyond its placement's SSH access.

**Image fill.** One block of the same family, implementation
`mirrorfill.ImagesImplementation`, named `mirror-images-<environment>`, runs on
the mirror host after the Registry's own block and copies each image a
consumer pulls through the mirror, once per digest. It first proves
`/usr/bin/skopeo` there and otherwise refuses, naming the `skopeo` package to
install, before any copy; the controller stage installs it on the controller
Machine, and plan refuses an installed mirror host whose profile lacks it. For each image it asks the Registry for
`/v2/<repository>/manifests/<digest>` with `HEAD`; a manifest the Registry
serves at that digest is not copied again, so a replay copies nothing and
reports unchanged. Any other image is copied with
`skopeo copy --all --preserve-digests` from `docker://<origin>@<digest>`,
read anonymously over verified HTTPS through the mirror host's frozen egress
route, to `docker://<registry location>/<origin>:sha256-<hex>`, a tag the
digest spells, and the Registry's manifest digest under that tag is then read
back: one other than the pin fails the attempt with `lifecycle.state` on the
Registry, an origin the host cannot copy from fails it with
`lifecycle.unknown`, and so does a Registry it cannot read. The Registry's
credential and trust anchor are lent to that one task as material, written by
its module into a private directory beside the copy as an auth file and a
certificate directory and removed after it whatever its result, and reach the
Registry alone, never the origin; no record or evidence holds them. The
evidence names, in request order, each image the Registry holds by repository
and digest (`TestTheImageFillReadsItsEffectFromTheImagesTheMirrorHolds`). The
fill's removal takes nothing back and contacts no host, because the Registry's
own removal deletes what was pushed; pruning an image no longer listed is
[B1323](milestones/backlog.md#b1323).

**Release fill.** One block of the same family per such cluster,
implementation `mirrorfill.ReleaseImplementation`, named
`mirror-release-<cluster>`, runs on the mirror host after the Registry's own
block, and on a Machine Bootwright installs over the installed arm after its
installation (`TestAReleaseCopyOnAnInstalledMirrorHostPlansAfterItsInstallation`), and copies the cluster's release, `quay.io/openshift-release-dev/ocp-release`
at the digest its `spec.distribution.release.image` pins or else at the tag
`<version>-x86_64`, with the release's own `oc`. It first reads the mirror:
the release manifest at that reference, the image references in the release's
own layers there, and each component's manifest by digest beneath
`<registry location>/quay.io/openshift-release-dev/ocp-v4.0-art-dev`, the
layout the cluster's [registry mirrors](openshift-clusters.md#installation-features)
map. Each layer read is bounded in its compressed and expanded bytes, its
archive headers and its total time, and must hash to its digest, as an index's
`amd64` manifest must to the digest the index names. A release held with every
component is not copied again, so a replay runs nothing and reports unchanged;
a held copy that is damaged, with a layer missing or no readable image
references, is copied over, while a mirror the host cannot read or one holding
another release at the pin refuses. Otherwise it reads
`openshift-client-linux-<version>.tar.gz` from the source the controller
stage reads that client from and `sha256sum.txt` from its publisher's release
directory on `mirror.openshift.com`, through the frozen egress route over
verified HTTPS, and extracts `oc` only when the archive's SHA-256 is the one
listed, then runs `oc adm release mirror -a <auth> --from=<release>
--to=<location>/<components> --to-release-image=<location>/<release
repository>:<tag> --insecure=false`, never `--insecure`, verifying the
Registry against its anchor beside the system roots through `SSL_CERT_DIR`;
oc also reads the release's signatures from `storage.googleapis.com` and
`mirror.openshift.com`, only warning when it cannot, and its report of what it
copied, which holds no credential, enters the run's log.
The release is read back as before: a release or component the Registry then
lacks fails the attempt with `lifecycle.state` on the cluster, and so do a
release held at another digest than the pin and an archive its publisher does
not list; a client, copy or Registry the host cannot read fails it with
`lifecycle.unknown`. The evidence names the release digest the Registry holds,
each component by name and digest and how many it lacks
(`TestTheReleaseFillReadsItsEffectFromWhatTheMirrorHolds`); its removal takes
nothing back, as the image fill's does.

**Release custody.** The copy runs in the run's private scratch on the mirror
host, mode `0700`, with its own `HOME`, XDG directories and `DOCKER_CONFIG`,
so it reads no registry configuration of the host's own; the lent auth
document, its `REGISTRY_AUTH_FILE`, and the anchor are in a private `0700`
directory beneath `/run`, a tmpfs a reboot clears, never on the host's disk.
Both are emptied and removed in an `always` whatever the copy did, and a copy
killed before its `always` leaves them only until the next copy on that host
removes every other run's copy scratch and lent directory older than any run
lives (`test_prepare_sweeps_what_a_killed_copy_left_once_no_run_can_still_hold_it`).
The controller derives in Go, per attempt, an auth document holding exactly two
entries: the `quay.io` token of the cluster's pull secret and the Registry's
credential for its own location. That document alone is lent, written there
`0600` in that tmpfs directory and passed to `oc -a`; the pull secret itself never leaves the
controller, and a pull secret with no `quay.io` token refuses before any run
(`TestTheReleaseFillLendsATwoEntryAuthDocumentToTheApplyAlone`). The document
is cleared from the controller's memory when the run returns; no record,
evidence, argument or environment variable holds it.

**Ordering.** The controller stage splits around the mirror: its host
packages first, then the mirror server's block and the fill, which run no
controller-installed client, then the stage's clients, which it reads from
them; every other block waits for both controller blocks
([stages and the pause boundary](state-reconciliation.md#stages-and-the-pause-boundary),
`TestASplitControllerStageOrdersTheMirrorBetweenItsBlocks`). The mirror host
may be the controller Machine, where the server and the fill run locally; a
Machine with `access.local`, or SSH at a controller address, that is not the
controller Machine refuses. On a Machine Bootwright installs, its
realization and installation, with what they wait for, run between the stage's
two blocks too, and the server and the fill wait for that installation;
an installation whose substrate runs a controller client refuses
(`TestAManagedArtifactMirrorPlansBetweenTheStagesBlocksOnEachPlacement`). The
Registry and the image fill are not among the blocks the stage waits for,
since nothing it reads is an image. Each managed service, artifact server and
libvirt machine block that pulls through the mirror waits for the Registry and
its fill, and every image pulled on the mirror host stays its origin's. When
Bootwright installs that host, so does exactly what its installation waits
for: its own emulated BMC and each managed server its installation selects,
and so on for each Machine Bootwright installs that runs one of them; another service or
another Machine's emulated BMC on those Machines pulls through the mirror and
waits for it, so no mirror block waits for itself
(`TestAManagedImageMirrorIsFilledAfterTheControllerStageAndBeforeItsConsumers`,
`TestAnInstalledMirrorHostsInstallationPullsFromTheOrigins`,
`TestAnotherGuestsEmulatedBMCBesideAnInstalledMirrorHostPullsThroughTheMirror`). On the
controller Machine the Registry and the image fill run locally after the
stage (`TestAManagedImageMirrorOnTheControllerIsFilledLocallyAfterTheStage`);
on a Machine Bootwright installs they take the installed arm after that
installation (`TestAManagedImageMirrorOnAnInstalledMachinePlansAfterItsInstallation`). A cluster's image
build and installation wait for its release copy, which waits for the Registry
and the controller stage
(`TestAClusterUnderAManagedImageMirrorWaitsForItsReleaseCopy`).

**Trust.** Every consumer verifies the artifact mirror against the `caBundle`
that issues the server's serving certificate, the
`spec.source.generated.issuerRef` of its `tls.secretRef`, unless
`spec.mirror.artifacts.trustBundleRef` names another, and the image mirror
against the issuer of the Registry's serving certificate unless the Registry's
`trustBundleRef` names another; admission refuses a serving certificate with
no issuer when none is named, and Bootwright creates no Secret for it.

## Adapter boundary

Every managed-service effect crosses the
[Go/Ansible boundary](architecture.md#go-and-ansible-responsibility-boundary)
through one fixed entrypoint per operation, under
[the adapter result protocol](architecture.md#the-adapter-result-protocol) and
the [process](security.md#process-boundary) and
[Secret-material](security.md#sensitive-material) rules. Both placement arms
use the same role, request and evidence contract, so local execution bypasses
no authorization, privilege or validation.
