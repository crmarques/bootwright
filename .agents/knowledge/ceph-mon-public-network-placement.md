# Ceph mon placement is filtered by public_network: the off-network arbiter

**Root cause:** cephadm schedules a mon only onto a host that holds an address
inside `public_network`. In `cephadm/serve.py::_apply_service` it reads
`get_foreign_ceph_option('mon', 'public_network')`, splits it on commas, and
runs `matches_public_network(host)`, which returns true only when one of the
host's cached connected networks **overlaps** one of the declared networks
(`ipaddress.ip_network(hn).overlaps(ipaddress.ip_network(pn))`). Every other
candidate is dropped from the placement with

```
cephadm [INF] Filtered out host <fqdn>: does not belong to mon public_network(s):  <pubnets>, host network(s): <hostnets>
```

The filter is silent in every other surface: `ceph orch ls --service_type mon`
still shows the full declared `placement.hosts` list, so the only visible sign
is `size:` sitting above `running:`. The daemon is never created, so
`ceph orch ps --daemon-type mon` simply has no row for it, and the host stays
`status: ''` in `ceph orch host ls`.

A stretch **tiebreaker** is the case that hits this: an arbiter standing in a
third site frequently sits on a subnet of its own rather than on the stretched
public L2. `public_network` accepts a comma-separated list, so the fix is to
declare the arbiter's subnet alongside the data-site network — not to widen the
data-site CIDR, and not to move the arbiter.

**When it bites:** `Wait for every declared Ceph mon to join the monmap`
exhausts its retries (60 x 10s by default) and
`Assert every declared Ceph mon joined the monmap` fails naming one absent mon,
while OSDs on that same host — if it carries any — deploy normally. OSD health
never clears it: an OSD only needs to reach an existing mon outbound, whereas a
mon must bind inside `public_network`. The failure is fatal by design rather
than deferred, because the topology operations that follow address mons by name
(`ceph mon set_location`, and for a stretch cluster
`ceph mon enable_stretch_mode`) and Ceph answers
`ENOENT: mon.<name> does not exist` for a mon outside the monmap.

**Fix:** `validateStorageCephMonPublicNetwork`
(`internal/state/desired/validate_storage_networks.go`) fails closed at
`bootwright validate` on any node carrying the `mon` role whose declared
networking overlaps no `spec.ceph.networks.publicCIDRs` entry. It reproduces
cephadm's own rule: when the Machine declares `interfaceAddresses`, it computes
each connected subnet from the address plus its `prefixLength` and requires an
overlap; when it declares none — which a `provided` Machine never can, since
`spec.network.config` is forbidden there — it falls back to the address the
orchestrator registers the host under (`cephadm.addressRef`, else the SSH
address) and requires containment. Both arms skip silently when nothing is
resolvable, so the check adds no false positives.

`validateStorageCephBootstrapPublicNetwork` remains a separate, stricter check
for the bootstrap node only: `cephadm bootstrap` refuses a `--config`
`public_network` entry that does not **exactly equal** a locally-configured
interface subnet (`public CIDR network ... is not configured locally`), which is
a different rule from the overlap test used for placement afterwards.

**Ordering:** the rendered `set-public-network` operation
(`internal/render/ceph/storage_operations_topology.go`) runs in the `topology`
phase, which `phases/bootstrap.yml` includes *after* both `service_specs.yml`
and `mon_readiness.yml`. Before this change that made the defect
non-convergent: correcting `publicCIDRs` and re-applying still died at the mon
gate, ten minutes in, because the corrected value was only written by a step the
run never reached — `bootstrap-ceph.conf --config` seeds `public_network` on the
*first* bootstrap only. `phases/bootstrap_steps/network_config.yml` now asserts
`global public_network` and `global cluster_network` before the first
`ceph orch apply`, read-then-set so a matching value is a no-op, guarded on a
non-empty declaration so an undeclared `publicCIDRs` never clears what bootstrap
derived. `TestStorageCephNetworksAreAssertedBeforeDaemonPlacement` pins the
ordering. The topology-phase operation is deliberately left in place; a second
identical `ceph config set` is idempotent, and it mirrors the daemon image pin,
which is likewise asserted early by `container_image_base.yml` and again as a
rendered operation.

**Diagnosis:** `mon_readiness.yml` already collected
`ceph log last 100 cephadm` but buried the decisive line under four other YAML
dumps. Its assert now pre-extracts the `Filtered out host` lines with
`regex_findall('.*Filtered out host.*')` and leads the failure message with
them, naming the network the host actually sits on.

**Do not** widen the data-site `publicCIDRs` entry to a supernet that happens to
cover the arbiter. The overlap test would accept it, but `public_network` is
also what every mon binds within, so a supernet changes where the whole cluster
believes its public network is. Declare the two subnets separately.
