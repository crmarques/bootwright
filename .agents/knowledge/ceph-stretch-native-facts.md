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
  that show stretch state; the unverified shapes X199 reads are listed below.
- IBM recommends two MONs per data site plus the tiebreaker, five in all, and
  does not enforce it; the tiebreaker may be a virtual machine.

## Host location and MON placement

- cephadm's host spec `location` creates the host's CRUSH bucket under the
  named hierarchy, and only the initial location: later changes are ignored
  ([host management](https://docs.ceph.com/en/tentacle/cephadm/host-management/)).
  The page names no `--location` flag on `ceph orch host add`. The bootstrap
  host joins before any host spec, so X199 proves its location by the
  `osd crush tree` readback rather than assume the spec applied.
- IBM's bootstrap example
  ([CRUSH location during bootstrap](https://www.ibm.com/docs/en/SSEG27_9.9.1/administration/proc_setting-the-crush-location-during-bootstrap.html))
  gives the first host of each site `location: {root: default, datacenter:
  DC1}`, which hangs the site bucket under `default`; X199 gives every data
  host that pair. Its manual page
  ([CRUSH location manually](https://www.ibm.com/docs/en/SSEG27_9.9.1/administration/proc_setting-the-crush-location-for-the-daemons-manually.html))
  moves a host with `ceph osd crush move <host> datacenter=<dc>` and places
  a MON with `ceph mon set_location <host> datacenter=<dc>`, read back from
  `ceph mon dump`'s per-MON `crush_location` such as `{datacenter=DC1}`.
- The MON page says cephadm passes `--set-crush-location` only when it
  deploys a MON, so a MON deployed before the spec carried its location, the
  bootstrap MON among them, needs `mon set_location`; X199 sets only an unset
  location and refuses one that differs.
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

## Activation readbacks (X199): unverified shapes

IBM's CRUSH page
([CRUSH map for stretch mode](https://www.ibm.com/docs/en/SSEG27_9.9.1/administration/config-crush-map-stretch-mode.html))
fixes the rule's injection: `ceph osd getcrushmap`, `crushtool -d`, the rule
appended, `crushtool -c`, `ceph osd setcrushmap -i`, and `ceph osd crush
rule ls`. X199 compiles inside the frozen image (`crushtool` ships in its
`ceph-base`, not on the host) and passes `osd setcrushmap` the CRUSH version
read before `getcrushmap`, so a map changed in between refuses instead of
being overwritten. The steps X199 compares follow `CrushWrapper`'s rule dump,
whose shape `test_ceph_pools.py` already cites.

No cited page fixes the following; the fakes in `test_ceph_stretch.py`
answer them as written here, and B488's emulated rehearsal must read them
from the selected build before the item is accepted:
- `ceph mon dump -f json`: `election_strategy` an integer (connectivity is
  3), `stretch_mode` a Boolean, `tiebreaker_mon` the MON name, and each
  `mons[].crush_location` the rendered string `{key=value,...}`, `{}` unset.
- `ceph osd dump -f json`: `crush_version` an integer, a `stretch_mode`
  object with `stretch_mode_enabled` and `stretch_bucket_count`, and
  `pools[]` with `pool_name`, `crush_rule` (the rule ID), `size` and
  `min_size`.
- `ceph osd crush tree -f json`: the `osd tree` node form, `nodes[]` with
  `id`, `name`, `type` and `children` IDs.
- Whether a pool a service creates after `enable_stretch_mode` (`.nfs`,
  `.smb`, `.rgw.root`) lands on the stretch rule at 4/2: X199's observation
  keeps the cluster partial when one does not. X200 therefore never lets a
  service create one: the gateway creates `.rgw.root` itself on the rule
  before any radosgw-admin call, and NFS and SMB refuse on a stretch cluster
  ([stretch services](../../specs/ceph.md#stretch-services-and-front-doors)).
  Whether `osd pool set <pool> size 4` is accepted on a pool created in
  stretch mode is unverified the same way, for B488 to read.
- `enable_stretch_mode` is assumed to need every MON located and exactly two
  buckets of the failure domain, which is why X199 activates only after the
  cluster is otherwise ready and checks the two buckets and every pool's
  default protection before its attempt. Whether it also refuses data-site
  buckets of unequal CRUSH weight is unverified; such a refusal would leave
  `stretch-interrupted` (B920) and belongs before the attempt once B488
  confirms it.

## FIPS on a provided host

`fips-mode-setup --check` reads `/proc/sys/crypto/fips_enabled` and
`/etc/crypto-policies/state/current`
([managed-OS FIPS mode](../../specs/managed-os.md#fips-mode)); the provided
tiebreaker's host block reads the same two files (`observe_fips` in
`plugins/module_utils/ceph_native.py`, tested in `test_ceph_core.py` and
`test_ceph_host_execution.py`). `FIPS:OSPP` is a subpolicy and proves
nothing.

## Access to a provided tiebreaker

X198 bound a provided tiebreaker only when its Machine named the fleet key as
`access.ssh.auth.privateKeyRef`, which Environment admission refuses (`uses
the fleet install key as its access key`), so no such cluster validated and
planned; its tests built the Ceph request from a catalog Environment
admission never saw. The fleet key is effect material and a Machine access
key an access credential, so the tiebreaker's Machine names only the cluster
SSH user and its `knownHostsRef`, and Ceph dials it with the Environment's key
(D204, `bindProvidedHost` in `internal/storage/ceph/provided_host.go`). Test
a cross-kind rule like this one by compiling a whole example, as
`cmd/bootwright/ceph_provided_tiebreaker_linux_amd64_test.go` does, never
only the capability's own catalog. The owning rule is the
[stretch topology](../../specs/ceph.md#stretch-topology).

## Removing a provided tiebreaker (X203): unverified footprint

X203 removes a provided tiebreaker's footprint in the cluster block's
destroy (`TiebreakerRemoval` in `plugins/module_utils/ceph_native_stretch.py`,
tested in `test_ceph_tiebreaker_removal.py`; the owning rule is
[a provided tiebreaker's removal](../../specs/ceph.md#a-provided-tiebreakers-removal)).
Its effect is the package cephadm's `rm-cluster --fsid <fsid> --force`,
never `--zap-osds`. The [cleanup source review](ibm-ceph-cleanup.md) read
`_rm_cluster` but no run has; the fake answers as written here, and B488's
emulated rehearsal must read each from the selected build:
- UNVERIFIED: that `rm-cluster` removes `/var/lib/ceph/<fsid>`,
  `/var/log/ceph/<fsid>`, `/etc/logrotate.d/ceph-<fsid>`, the
  `ceph-<fsid>@.service` and `ceph-<fsid>.target` unit files and the
  `ceph.target.wants` link, and `/etc/ceph/ceph.conf`, `ceph.pub` and the
  admin keyring when the conf names the FSID; the absence proof refuses
  `not-removed` when one remains.
- UNVERIFIED: whether it stops and disables the units before removing them
  and reloads systemd; absence reads unit files only, not systemd's loaded
  state.
- UNVERIFIED: whether its logging start, like `cephadm ls`'s, writes
  `/var/log/ceph/cephadm.log` and `/etc/logrotate.d/cephadm` again after
  removing them with the last cluster; absence does not read those.
- UNVERIFIED: whether it removes `/etc/ceph/podman-auth.json` itself;
  Bootwright removes its own credential first, under its recorded metadata.
- UNVERIFIED: the crash collector's daemon ID on a host whose native
  hostname is a full name; the proof admits `crash.` and `mon.` with either
  the short or the full name of the tiebreaker.
- UNVERIFIED: whether an `rm-cluster` killed partway leaves a daemon
  directory the bounded inventory refuses as `native-observation-unproved`;
  the removal then stays unknown, never absent.
- The selected cephadm's parser must declare `--fsid` and `--force`
  (`require_declared_options`); a build lacking either refuses before the
  command.
