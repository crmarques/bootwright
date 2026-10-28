# Ceph stretch mode: native facts behind the stretch topology

The native facts the [stretch topology](../../specs/ceph.md#stretch-topology)
contract and D196 rest on, read from the documentation on 2026-10-28 for
X198. They are documentation, not observations of a running stretch cluster:
B472's rehearsal and B488's emulated run confirm them on the selected build.
Build labels name where a fact was read; no version rule is recorded (D163).

## Entering and leaving stretch mode

- Upstream ([stretch mode](https://docs.ceph.com/en/tentacle/rados/operations/stretch-mode/))
  enters stretch mode by placing each MON (`ceph mon set_location <mon>
  datacenter=<site>`), injecting a two-per-site rule (`osd getcrushmap`,
  `crushtool -d`, edit, `crushtool -c`, `osd setcrushmap -i`), setting
  `ceph mon set election_strategy connectivity`, placing the tiebreaker in a
  third datacenter, then `ceph mon enable_stretch_mode <tiebreaker> <rule>
  datacenter`.
- The tiebreaker's datacenter must not be site1 or site2 and should not be
  predefined in the CRUSH map. This is why X198 refuses a host `location` on
  the tiebreaker: a host spec location would create that bucket.
- Ceph refuses to enter stretch mode while a pool has a non-default `size` or
  `min_size`; pools then run at 4/2 and drop to `min_size` 1 while degraded.
  Erasure-coded pools and device-class rules are excluded.
- Upstream documents `ceph mon disable_stretch_mode [<rule>]
  --yes-i-really-mean-it`, which runs only in healthy or degraded stretch
  mode; IBM's 9.9.1 overview
  ([stretch mode](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=storage-stretch-mode))
  says stretch mode cannot be exited. Bootwright never runs the disable
  command (D196 (8)).
- IBM's rule is the single-`take` form `take default`, `choose firstn 0 type
  datacenter`, `chooseleaf firstn 2 type host`, which the stretch topology
  adopts; its example also carries a `class hdd` line that the same page
  calls unsupported. Neither page names the `mon dump` or `osd dump` fields
  that show stretch state; X199's readbacks take them from B472's rehearsal.
- IBM recommends two MONs per data site plus the tiebreaker, five in all, and
  does not enforce it; the tiebreaker may be a virtual machine.

## Host location and MON placement

- cephadm's host spec `location` creates the host's CRUSH bucket under the
  named hierarchy, and only the initial location: later changes are ignored
  ([host management](https://docs.ceph.com/en/tentacle/cephadm/host-management/)).
  The page names no `--location` flag on `ceph orch host add`. The bootstrap
  host joins before any host spec, so X199 must prove its location by the
  `osd crush tree` readback rather than assume the spec applied.
- The MON service spec's `crush_locations` (`<hostname>: [datacenter=<site>]`)
  sets MON CRUSH locations and is the recommended way to replace tiebreaker
  MONs ([MON service](https://docs.ceph.com/en/tentacle/cephadm/services/mon/));
  cephadm may fail to set every location of a host with several, and
  re-applying the spec retriggers it.
- `public_network` takes a comma-separated list of CIDRs, and cephadm deploys
  a MON only on a host with an address in it. This is why admission proves
  each MON host address inside a declared public CIDR
  (`TestStretchTopologyRefusesEachBrokenInput` in
  `internal/storage/cephcluster/stretch_test.go`).

## FIPS on a provided host

`fips-mode-setup --check` reads `/proc/sys/crypto/fips_enabled` and
`/etc/crypto-policies/state/current`
([managed-OS FIPS mode](../../specs/managed-os.md#fips-mode)); the provided
tiebreaker's host block reads the same two files (`observe_fips` in
`plugins/module_utils/ceph_native.py`, tested in `test_ceph_core.py` and
`test_ceph_host_execution.py`). `FIPS:OSPP` is a subpolicy and proves
nothing.
