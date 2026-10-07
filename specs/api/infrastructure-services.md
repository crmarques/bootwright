# Infrastructure services

`Proxy`, `DNSServer`, `NTPServer`, `ArtifactServer`, `Registry`, and
`LoadBalancer` each declare one shared service. Consumers refer directly to
its `metadata.name`; there is no Environment catalog or intermediate component
identity. [Kind defaults](environment.md#kind-defaults) apply before service
defaults and reference validation. [The compiler boundary](../api.md#compiler-boundary)
applies throughout: admission performs no connection, readiness check or effect.

## Management and placement

Every service has a flat `spec` with required `management: managed|external`.
There is no service arm, `type`, `componentRef`, `default` flag, or separate
managed/external wrapper. A managed declaration supplies deployment intent;
an external declaration supplies access facts and never grants Bootwright
ownership of the external service.

| Field | Type | Applies to | Rule |
| --- | --- | --- | --- |
| `spec.management` | string | All services | Required `managed` or `external`. |
| `spec.machineRef` | string | Managed services | Required global `Machine` placement reference; forbidden for external. |
| `spec.implementation` | string | Managed Proxy, DNSServer, NTPServer, Registry, LoadBalancer | Required implementation listed below; forbidden for external and ArtifactServer. |
| `spec.image` | object | Managed container services | Optional image pins, forbidden for external. |
| `spec.image.local` | string | With `image` | Local image reference. |
| `spec.image.public` | string | With `image` | Public image reference. |

A managed service plans one lifecycle block whose identity is its name behind
a fixed prefix, and a block identity is at most 63 bytes. Validate therefore
refuses a longer managed name at `metadata.name`, naming the limit; an
external service plans no block and has no such limit.

| Kind | Block identity | Name limit |
| --- | --- | --- |
| `ArtifactServer` | `artifact-server-<name>` | 47 |
| `Proxy` | `proxy-<name>` | 57 |
| `DNSServer` | `dns-<name>` | 59 |
| `NTPServer` | `ntp-<name>` | 59 |

An image block sets at least one reference. Each reference is pinned by content
digest, `<repository>@sha256:<64 hex>`, whose digest normalizes to lowercase,
because planning resolves no tag; validate refuses a tag at that reference, in
kind defaults too. The selected image belongs to this service; fleet defaults
can supply it through the corresponding kind entry.
Image declarations neither select an executable adapter nor establish release
support.

Every managed service runs as a container, so each requires its placement
Machine's `container-runtime` capability. That Machine also has effective
`os.provided: true`, because a managed service runs only where the operating
system is ready; validate refuses any other placement at `spec.machineRef`.
An external service forbids managed placement, implementation, image and
listener configuration. Managed-only intrinsic defaults do not materialize on
external declarations. An explicit management choice suppresses incompatible
inherited fields under the ordinary
[variant-default rules](environment.md#kind-defaults).

Effective service fields emit in this order, omitting inapplicable fields:

| Kinds | Field order |
| --- | --- |
| Proxy | `management`, `machineRef`, `image`, `implementation`, `bindAddress`, `port`, `endpoints`, `connection`. |
| DNSServer | `management`, `machineRef`, `image`, `implementation`, `bindAddress`, `port`, `endpoints`, `address`, `additionalIngressHosts`, `forwarders`. |
| NTPServer | `management`, `machineRef`, `image`, `implementation`, `bindAddress`, `port`, `endpoints`, `address`, `upstreamSources`. |
| ArtifactServer | `management`, `machineRef`, `image`, `bindAddress`, `retention`, `tls`, `listeners`, `endpoints`. |
| Registry | `management`, `machineRef`, `image`, `implementation`, `bindAddress`, `port`, `endpoints`, `url`, `credentialsRef`, `trustBundleRef`. |
| LoadBalancer | `management`, `machineRef`, `image`, `implementation`, `bindAddresses`. |

## Proxy, DNS, NTP and registry deployment

The four managed kinds use these deployment defaults:

| Kind | Required implementation | `bindAddress` default | `port` default |
| --- | --- | --- | --- |
| `Proxy` | `squid` | `0.0.0.0` | `3128` |
| `DNSServer` | `dnsmasq` | `0.0.0.0` | `53` |
| `NTPServer` | `chrony` | `0.0.0.0` | `123` |
| `Registry` | `mirror-registry` | `0.0.0.0` | `5000` |

A wildcard default binds every address, so it collides with any other listener
on that port, such as another resolver on the host; the apply then refuses
before starting the service and names the socket under
[host reservations](../infrastructure-services.md#host-reservations), and the
examples declare an explicit bindAddress.

`bindAddress` is an IP literal. A port is in `1..65535`; DNSServer permits
only `53` and NTPServer only `123`, because their consumers configure a
resolver or a time server by address alone. Each optional `endpoints[]` record
has required unique `name` and required `addressRef` resolving to
`Machine.spec.network.addresses[].name` on the placement Machine. Consumers
select one named endpoint. Endpoint addresses are derived values, not another
authored copy of Machine addresses.

A DNSServer endpoint names an IP address on its Machine, because a resolver is
configured by address. When `bindAddress` is not a wildcard (`0.0.0.0` or
`::`), every endpoint whose address is an IP equals it, because nothing answers
on another address; a DNS-name endpoint is not compared, since admission
performs no lookup. A wildcard bind requires at least one endpoint, because
readiness probes the service through its endpoint addresses.

Managed DNSServer additionally accepts optional `additionalIngressHosts[]`
and `forwarders[]`; forwarders are IP resolver addresses. Managed NTPServer
accepts optional `upstreamSources[]` containing IP or DNS NTP sources.

External forms have these exact connection fields:

| Kind | Fields | Rule |
| --- | --- | --- |
| `Proxy` | `connection.httpProxy`, `connection.httpsProxy`, `connection.auth.proxyAuthRef`, `connection.trustBundleRef` | `connection` sets at least one bare HTTP(S) proxy endpoint: at most 4096 ASCII bytes with a host and no userinfo, path, query or fragment, under the [context-free route's grammar](../controller.md#the-context-free-acquisition-route); validate refuses another at that field, naming the field and never the value. Authentication names a `usernamePassword` Secret; trust names a `caBundle` Secret. |
| `DNSServer` | `address`, optional `additionalIngressHosts[]` | Required resolver IP, canonicalized in effective state. |
| `NTPServer` | `address` | Required IP or DNS hostname. |
| `Registry` | `url` | Required registry location without embedded credentials. |

Service IP literals normalize to canonical text in external DNS/NTP addresses,
managed bind addresses, load-balancer bind addresses, DNS forwarders and NTP
upstream sources. DNS names and list order remain unchanged.

Proxy bypass policy belongs to the consumer's proxy choice, never to
`Proxy.spec.connection`. Registry additionally accepts `credentialsRef` to a
`usernamePassword` or `dockerConfigJson` Secret and `trustBundleRef` to a
`caBundle` Secret in either management mode. Those fields are service facts;
consumers do not duplicate them.

## ArtifactServer

A managed ArtifactServer accepts these fields after `management`:

| Field | Type | Required | Default or rule |
| --- | --- | --- | --- |
| `machineRef` | string | yes | Global placement Machine. |
| `bindAddress` | string | no | IP literal; defaults `0.0.0.0`. |
| `retention` | string | no | `persistent` by default, or `install-only`. |
| `tls` | object | conditional | Required when any effective listener uses HTTPS; forbidden for HTTP-only listeners. |
| `tls.secretRef` | string | with TLS | `tlsCertificate` Secret supplying certificate and key. A [generated](secrets.md#generated-source) Secret's `ipAddresses` or `dnsNames` name the address of every HTTPS endpoint, or that endpoint refuses at `$.spec.endpoints[i]` naming the Secret and the address. |
| `tls.minVersion` | string | no | `TLSv1.2` by default, or `TLSv1.3`. |
| `listeners` | array | no | Non-empty; defaults to `[{name: https, protocol: https, port: 8443}]`. |
| `listeners[].name` | string | yes | Unique listener name. |
| `listeners[].protocol` | string | yes | `http` or `https`. |
| `listeners[].port` | integer | yes | Unique port in `1..65535`. |
| `endpoints` | array | no | Set keyed by required unique `name`. Under a wildcard `bindAddress`, every effective listener, the default one included, has an endpoint naming it; under any other, every endpoint whose address is an IP equals `bindAddress`. |
| `endpoints[].listenerRef` | string | yes | Local listener name. |
| `endpoints[].addressRef` | string | yes | Address name on the placement Machine. |
| `image` | object | no | Managed image pins under the common rules above. |

An ArtifactServer kind-default fragment that contains `endpoints` must state
`management` even though other partial fragments may omit it. The external
URL and managed listener/address endpoint records are different shapes;
endpoint defaults are not inherited across an explicit management change.

An external ArtifactServer instead requires non-empty `endpoints[]` with
unique `name` and absolute HTTP(S) `url`. It forbids deployment, retention and
TLS-serving configuration. The retired `tls.certificateRef` is rejected.

An `artifactServerEndpoint` consumer has required scalar `serverRef` to an
ArtifactServer followed by required scalar `endpointRef` to one of its
endpoints. Neither is inferred from available services or endpoint count.
No consumer accepts an external ArtifactServer, because each publishes into the
server it selects; selecting one refuses at `serverRef`. Each consumer declares
whether it requires persistent retention or HTTP package serving. It must
satisfy those requirements after kind defaults have supplied any omitted fields.

## LoadBalancer

A managed LoadBalancer requires `implementation: haproxy`, `machineRef`, and
non-empty `bindAddresses[]`; `image` is optional. An external LoadBalancer
has `management: external` and the same required bind-address list, without
placement, implementation or image fields.

Each bind-address entry emits required IP `address` then optional `name`. Its
`name` is required
when more than one entry exists and is unique when supplied. A cluster
endpoint selects it through `source.loadBalancerRef` and optional
`source.bindAddressRef`. Omission of the local reference is valid only for a
single bind address. [Container endpoints](container-clusters.md#endpoints)
own address, topology and VIP constraints.

## Consumer selections

Service references always name the expected kind directly. Declaration order,
a service named `default`, and the presence of a sole service never select a
service. Kind defaults can supply explicit references. The effective consumer
retains its references rather than copying connection data, credentials or
trust from the selected object.

### Proxy choice

A proxy choice is an atomic closed record with exactly one of scalar
`proxyRef` to a Proxy or `direct: {}`. The proxy arm also permits optional
`endpointRef` and ordered unique `noProxy[]` strings. `direct` accepts neither
field. Empty objects, null, empty references, conflicting choices, and the
former `none` selector are invalid. Each `noProxy[]` entry is `*`, a host
name, a `.domain` or `*.domain` suffix, an IP address or a CIDR block, each
name or address optionally with `:port` and an IPv6 address bracketed before
one, in at most 1024 bytes. Validate refuses another at `noProxy[<i>]` of
whichever consumer declares the choice: a Machine, a MachineInstallProfile or a
ContainerCluster installation.

An external Proxy forbids `endpointRef`. A managed Proxy requires an explicit
endpoint or exactly one declared endpoint, in which case normalization
materializes its name. Applicable consumers with no authored or inherited
choice normalize to `direct: {}`. No environment variable selects, overrides or
supplements a declared choice; the only route read from the invoking
environment is the
[context-free one](../controller.md#the-context-free-acquisition-route), which
no desired state can name.
A choice overrides the complete inherited choice, including bypass entries;
defaults never combine two proxy selections. Unavailable proxies never cause
fallback to direct access.

Machine egress, including the selected controller's, follows
[Machine.spec.proxy](machines.md#machine-proxy). Install-profile and
container-installation choices retain their own consumer scopes, and a
container choice does not mutate Machine policy.

### DNS and NTP selection lists

DNS and NTP consumers use ordered arrays of records containing required
scalar `serverRef` followed by optional scalar `endpointRef`. The consumer
field fixes the target kind to DNSServer or NTPServer. Pairs are unique;
order is retained. External services forbid `endpointRef`. Managed services
require an explicit endpoint or exactly one endpoint, whose name normalization
materializes. A consumer reaches a managed service at its selected endpoint's
address and an external one at its declared `address`. An omitted list requests its owning consumer's fallback; an
explicit empty list clears inherited selections. Empty NTP selections do not
request disabling time synchronization.

### Registry selection

A registry selection contains required scalar `registryRef` followed by
optional scalar `endpointRef`. External Registry selections forbid an
endpoint reference. Managed selections require an explicit endpoint or exactly
one declared endpoint, whose name normalization materializes. The cluster's
[registry policy](container-clusters.md#registry-policy) owns mirror use and
image-source mapping.

## Selection and bootstrap

Resource-selected services remain desired roots even when unconsumed, and a
managed service retains its placement closure under
[Environment selection](environment.md#resource-and-cluster-selection).
External services add no managed host or removal obligation. Managed service
placement on the controller is explicit and obeys the same capability and
endpoint rules as other hosts. Controller selection does not imply placement,
create a service, or install its runtime.

Reference resolution is not a readiness edge. An installation that selects a
managed DNSServer or NTPServer waits for it; one that selects an external
DNSServer or NTPServer uses its declared `address` and gains no readiness edge,
because no block of this product realizes it. The runtime edges between a
service, its host and its consumers belong to
[infrastructure services](../infrastructure-services.md#selection-and-refusal)
and the [dependency DAG](../state-reconciliation.md#plan-and-execution).
Admission rejects a proven self-hosted artifact bootstrap cycle at the
consuming field when the service requires the very Machine OS installation it
serves. It does not equate every declaration-reference cycle with an executable
cycle or claim to prove live availability.
