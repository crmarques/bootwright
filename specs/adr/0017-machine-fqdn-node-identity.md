# ADR 0017: Machine fqdn Address and Independent Node Identity

## Status

Accepted

## Context

A Machine's `metadata.name` doubles today as the seed of every cluster-visible
node identity: when a cluster host omits `name`, normalization composes
`<machineName>.<cluster>.<baseDomain>` and that string becomes the agent
installer hostname, the kickstart OS hostname, the cephadm host identity, and
the dnsmasq record set. The machine name is additionally published as a bare
DNS label record, and several storage surfaces (cephadm bootstrap seed
selection, stretch tiebreaker, OSD service ids, CLI `--node` selectors) accept
the machine name where a node identity is meant. Operators whose hardware
carries real corporate FQDNs (`srv4009.<domain>`) cannot express that name:
`metadata.name` is validated as a dot-free DNS label, and widening it to a
subdomain would leak dots into MAC synthesis, inventory host keys, service
ids, and install markers.

Machine identity (the hardware's stable DNS name) and node identity (the
cluster's name for the role a machine currently plays) are different concepts
with different lifecycles: machines outlive cluster membership, and a cluster
rebuild may bind the same node name to different hardware.

## Decision

Machine names stay DNS labels; the machine's FQDN becomes a first-class
address entry; nodes carry independent names; DNS binds the two; clusters and
every cluster-facing surface reference node names only.

This decision was taken when `Environment` carried a single
`spec.baseDomain`. [ADR 0018](0018-environment-domain-model.md) later split
that into per-class zones, so the placeholders below name the zone the
composition draws from: `<domains.machines>` for machine FQDNs and
`<clusterDomain>` for `domains.containerClusters` or `domains.storageClusters`
by cluster kind.

### The implicit `fqdn` machine address

Every Machine's `spec.addresses` list implicitly contains

```yaml
- name: fqdn
  address: <metadata.name>.<domains.machines>
```

injected during normalization when the Environment declares the zone and
the machine does not already declare an entry named `fqdn`. A declared
`fqdn` entry overrides the default verbatim (it must be a DNS subdomain;
it may live in a foreign zone). `metadata.name` keeps the DNS-label
validation unchanged.

`fqdn` is the machine's canonical connection address: whenever Bootwright
reaches a machine over SSH (ansible `ansible_host`, `machine rsh`,
`cluster rsh`, trust bootstrap), it connects to the `fqdn` name. The
entry referenced by `access.ssh.addressRef` keeps its meaning as the
machine's routable IP; it is what the `fqdn` DNS record must resolve to
and the fallback when no resolvable name exists. Two carve-outs connect by IP
deliberately: machines that host the managed name-resolution component their
own network references (the resolver cannot serve its own bootstrap), and
machines whose network configuration references no name-resolution entry at
all (no resolver is declared that could answer).

### Independent node names

A cluster node's `name` field names the node, not the machine. A bare
label composes to `<name>.<cluster>.<clusterDomain>`; a dotted value is an
explicit FQDN used verbatim. The node name is required and declared
explicitly (`topology.nodes[].name` for storage, `spec.nodes[].name` for
OpenShift); it is never inferred from the machine name or list position, so
the node FQDN never embeds the machine name. Node names must be unique within
a cluster. The composed FQDN remains the single cluster-visible identity: agent
installer hostname, kickstart OS hostname, cephadm host identity, CRUSH/mon
location, and DNS record name.

### DNS binding

The node FQDN resolves to the machine through its `fqdn`:

- Managed name resolution (dnsmasq): Bootwright renders a `host-record` for
  each machine's `fqdn` name targeting the `access.ssh.addressRef` IP, and a
  `cname` from each node FQDN to the bound machine's `fqdn`. The `fqdn`
  host-record is published even when the operator overrode `fqdn` into a
  foreign zone — the managed resolver answers authoritatively for the
  machines it serves, which keeps the cname target locally known and lets an
  environment resolve corporate machine names without reaching corporate DNS.
  A multi-homed machine whose foreign-zone record legitimately points at a
  different interface therefore resolves environment-locally to the
  `access.ssh.addressRef` IP. The bare machine-label record is no longer
  published.
- Provided (external) name resolution: the operator owns the records. A
  preflight group "Name resolution" resolves each machine's `fqdn` and
  each node FQDN and fails with the exact record to create
  (`A <fqdn> → <ip>`, `CNAME <nodeFQDN> → <fqdn>`) when resolution is
  missing or points at the wrong address. For managed resolution the same
  checks warn instead — converging the resolver is what apply does — and
  remediate with the apply command.

### Node-name-only cluster surfaces

Cluster-facing surfaces stop accepting machine names: cephadm
`bootstrap.node`, OSD drivegroup host selectors, and the stretch tiebreaker
reference node names only (validation rejects machine-name tokens);
storage topology host resolution drops its machine-name alias arms; the OSD
per-host service id derives from the node short name; CLI `--node` selectors,
completion, and roster hints present node names (role-ordinal aliases stay).
`machineRef` remains the only place a cluster names a machine, and
machine-scoped surfaces (`machine rsh`, `--machines`, ownership records, MAC
synthesis, install markers) keep keying on `metadata.name`.

### Install-profile hostname source

`customizations.hostname.source: machineName` now means "OS hostname is the
machine's `fqdn` name". It is valid only for machines not bound to any
cluster; a cluster-bound machine's OS hostname must equal its node FQDN or
cephadm host matching breaks, so that combination is a validation error.

## Consequences

- Existing states that relied on machine-name-derived node FQDNs must declare
  each node's `name` explicitly: the node name is now required, with no
  machine-name-derived or positional default. This is greenfield-oriented:
  deployed clusters keep their names by adding explicit `name` entries before
  upgrading, or accept a managed-OS reconverge (install-marker hashes include
  the kickstart hostname).
- Connection strings move from IPs to names for name-resolution-wired
  machines; SSH trust records key per address, so first contact after upgrade
  re-establishes trust against the `fqdn` name.
- The cephadm OSD service id changes from `data-<machineName>` to
  `data-<nodeShortName>`; existing clusters converge to the new service name
  on next apply.
- Operators on provided DNS get actionable preflight failures naming each
  missing record instead of mid-apply connection errors.
