# IBM Ceph version constraints and evidence

Use this page to route version-specific investigation and later compatibility
work. It records public artifact findings and existing corrections, not a
release allowlist. [Storage](../../specs/ceph.md#frozen-release-and-trust)
owns release and trust requirements; [B472](../../specs/milestones/m2.md#b472)
owns remaining adapter and feature qualification. A finding that needs a
different native mechanic becomes a row of the
[version-specialization catalog](version-specializations.md), keyed on a
capability of the selected artifact, never on a release; this page stays the
evidence inventory.

## Current authored example

The [three-node declaration](../../examples/ceph-clusters/libvirt-3-nodes-ceph/storage/cluster.yaml)
is the owner of these selections as of 2026-10-18:

| Coordinate | Authored value |
| --- | --- |
| IBM Storage Ceph release | `9.9.2.0`, based on Ceph `20.2.2` |
| Guest OS and native RPM build | RHEL `9.8`; `20.2.2-349.el9cp` |
| Container repository and build tag | `cp.icr.io/cp/ibm-ceph/ceph-9-rhel10:v9.9.2-26947`, confirmed by the owner (D171) |
| cephadm-ansible RPM | `5.0.3-1.el9cp` |

The signed [RHEL 9 cephadm RPM](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/cephadm-20.2.2-349.el9cp.noarch.rpm)
names the RHEL 10 image repository in its embedded
`cephadmlib/constants.py`. Container base, guest OS and native RPM build are
separate coordinates; that default names an image repository only. A release
table tag is not evidence that a repository/tag pair exists. Follow the
e2e [`lab-3-nodes-ceph`](../../e2e/lab-3-nodes-ceph/runbook.md) case's
authenticated metadata check and operator-owned immutable selection;
the moving release alias `v9.9.2` and the eventual frozen digest do not qualify a
release.

## SSH SDK and provider imports

The [native SSH evidence](ibm-ceph-native-ssh.md#provider-runtime-and-package-imports)
owns the public AsyncSSH `2.17.0` source RPM, the former exact `2.13.2` route
gate, and Ceph `20.2.2`'s relative `cephadm.utils` imports and manager FIPS/public
key inputs. Public package availability does not establish which SDK an
entitled container actually contains.

Correction `125f31c03` replaced that version equality gate with the existing
behavioral proof and loads the real utility source in an isolated registered
package. The [probe regressions](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_ceph_host_route_probe.py)
cover package initialization avoidance, module restoration, synthetic public
key and unknown FIPS facts, SDK variation, and the positive/negative SSH
controls. Those tests do not establish real image, host-policy or MGR lifecycle
qualification; keep the transport's confinement and refusal proofs when adding
version handling.

The [listener replacement evidence](ibm-ceph-native-ssh.md#listener-replacement-and-socket-reuse)
owns the shared AsyncSSH `2.13.2`/`2.17.0` address-reuse default, synthetic
rebind correction, exclusive live-bind check and bounded listener diagnostics.
This requirement follows the probe's listener lifecycle across those inspected
SDKs; it adds no release allowlist or automatic version selection. Preserve
operator control and qualify the actual selected runtime independently.

## Bootstrap license call and diagnostics

The authenticated downstream changes archives from the public
[Ceph 20.2.1 source RPM](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/source/ceph-20.2.1-405.el9cp.src.rpm)
and [Ceph 20.2.2 source RPM](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel10/source/ceph-20.2.2-349.el10cp.src.rpm)
differ in `src/cephadm/cephadm.py`: `20.2.1` calls
`orch accept-license --image` only when both `ibm_build` and
`automatically_accept_license` are true; `20.2.2` calls it whenever `ibm_build`
is true (line 3535, also present in the signed RHEL 9 executable's
`__main__.py`). Bootwright supplies `--automatically-accept-license` in both
the [native](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py)
and [tunnel](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_tunnel_staging.py)
bootstrap paths, so removal of that guard alone does not explain a failure.

The same source comparison leaves MGR `accept_license` in
`src/pybind/mgr/cephadm/module.py` and `_get_container_image_info` and
`_get_container_ibm_license` in `serve.py` unchanged. The acceptance path still
obtains image information and license content before storing acceptance.
Do not infer an image-inspection or license failure solely from reaching this
call.

In both versions, `_get_container_image_info` first selects a host from the
MGR inventory and raises `OrchestratorError('no hosts defined')` if it is
empty, before registry, SSH or container work (`20.2.2` `serve.py`, lines
2696–2703). The later license helper's preference for the active manager's host
cannot bypass this earlier precondition. This source fact does not establish
the inventory or cause of any live failure.

With `--skip-ssh`, this precondition conflicts with the deliberately empty
inventory of [verifiedTunnel staging](../../specs/ceph-native-api.md#verified-native-orchestration-transport).
The native error occurs after the output configuration, administrator keyring
and seed MON/MGR exist, but before `apply_call_home_settings` and the final
Call Home acceptance flag are initialized. When the verified host
cephadm's bootstrap makes this call, the worker recognizes exactly this refusal
in the bootstrap's private native output, proves the exact owned seed and
continues it in the same apply
([`ceph-bootstrap-license-call-continues`](version-specializations.md#ceph-bootstrap-license-call-continues)).
Any other failure is still a failed native command; the next apply's
continuation proves the same seed instead of restarting bootstrap or enrolling
hosts before the tunnel mount.

The [native worker](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py)
initializes missing Call Home state under its original configuration intent
before accepting that enabled intent. Its native `enable_call_home` method
sets the warning/option and enables the module without host inventory or SSH.
An absent flag after an earlier acceptance attempt remains drift. The
`20.2.2` source's `src/pybind/mgr/cephadm/module.py` makes `enable_call_home`
(lines 5250-5275) a health-warning set, a `call_home_needs_acceptance` option
set and a `call_home_agent` module enable, and `accept_license` (lines
5235-5248) an image and license lookup, a store set and a warning removal: both
are idempotent, so each repeats while the active manager restarts. An interrupted initialization
completes from the exact native result; when that result is missing (no flag,
or the flag set with the module disabled) and no Call Home acceptance or
configured-tunnel record exists, a mutating apply reissues `enable-call-home`
once. A cleared flag or any acceptance record still refuses.

License acceptance runs after owned enrollment and current tunnel, cache and
mount proofs. Its native command remains fallible. Readiness separately reads
the provider's persisted MON acceptance entry, bound to the profile's image
configuration ID. That ID differs from the selected platform-manifest digest.
The source's `src/python-common/ceph/cephadm/license_utils.py` derives the key
from the full native version and exact English license text; its MD5 is a
lookup suffix, not an artifact-authentication proof. The worker validates the
key and payload without invoking native license extraction during observation.
Missing acceptance is unfinished; malformed evidence refuses.

The lookup key's MD5 is called differently across the two public builds.
`20.2.1`'s `license_utils.py` (line 10) calls
`hashlib.md5(str(license_text).encode('utf-8'))` without `usedforsecurity=False`
(`cephadm-20.2.1-405.el9cp` RPM sha256
`7acf50c99cf3263c64a103476dcf67a3fd93a7b918472db7df660541ea987287`, its
`/usr/sbin/cephadm` zipapp sha256
`027222324b11d1af53b72b4ebb17134f58e477f63c39e9d07be40ba1fc2aabc1`; the
`20.2.1` source RPM's `src/python-common/ceph/cephadm/license_utils.py`, line
10). `20.2.2-349` passes the flag in both its RHEL 9 and RHEL 10 executables and
its source. Bootwright's own reader (`license_accepted` in `ceph_native.py`)
already passes the flag, so observation is unaffected.

An owner-run contained probe on 2026-10-31 qualifies the failure in the frozen
9.9.1.2 image (platform manifest
`sha256:8dae4b5b8f07c7fa95bd1d68b22d4c1dd4c2d323d533d807dc1ceae4e329cb7f`).
The kernel reports FIPS enabled. Fixed synthetic inputs to default MD5 and
`get_license_acceptance_key_value_entry_name(version, license_text)` both
raise `ValueError` with the unsupported-envelope classification; SHA-256 and
MD5 with `usedforsecurity=False` succeed in the same probe. The module source
SHA-256 is `fb3bd90e38a55b47e96ce2b2643f48461705daafc5a02a16b1d88751271938dd`,
matching the authenticated 20.2.1 package. Computed digests and helper keys
were discarded; no actual license extraction or acceptance was attempted.
[Python's hashlib documentation](https://docs.python.org/3.12/library/hashlib.html)
describes the flag's non-security use; it is not a FIPS-certification claim.

The native cluster also refused license acceptance with the broad OpenSSL
error, but its available MGR journal contains only earlier `no hosts defined`
tracebacks. Its later inner traceback is still missing. The probe proves the
helper failure in its contained root/closed-environment context, not that it
is the command's sole failure or that the full manager shares every runtime
setting. A newer signed helper's correction alone qualifies neither an image
nor an upgrade path. [B1013](../../specs/milestones/m2.md#b1013) owns the blocked
artifact choice and remaining native licensing evidence. IBM directs operators
to their representative for access to [9.9.1 FIPS builds](https://www.ibm.com/docs/pt-br/storage-ceph/9.9.1?topic=compatibility-matrix).
Preserve the frozen partial operation while choosing a vendor correction;
this finding adds no version allowlist, provider patch or compatibility gate.


The Ceph declaration does not switch off the host's crypto policy.
`ceph_native.prove_fips` only adds the declared proof; a false setting returns
without changing the OS or the container environment. Red Hat documents that
[Podman enables FIPS on supported containers when the RHEL host uses FIPS](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/9/html/security_hardening/switching-rhel-to-fips-mode_security-hardening).
The contained helper reproduction needed no running licensing command, so
turning off only the Ceph proof is not a demonstrated escape from this failure.
A non-FIPS comparison therefore uses a fresh managed OS installation without
FIPS as well as a false Ceph declaration, under the [installation contract](../../specs/managed-os.md#fips-mode).

The native acceptance path is an effect: image-information lookup may invoke
`cephadm pull`, and `command_display_license` unconditionally pulls before
extracting the English license. A cached image alone does not prove those
operations succeed. Keep them behind the original registry and verified SSH
boundaries; do not call them as read-only observations or supply a manager with
raw registry credentials to make them succeed.

`ActivePyModules.cc` updates its store cache before the monitor write and only
logs a failed persistence result. Native success or removal of
`IBM_LICENSE_NOT_ACCEPTED` therefore does not replace monitor readback. The
remaining native bootstrap tail also copies shared configuration, initializes
dashboard status and enables log rotation; reaching the seed proves none of
those later effects. The worker proves its declared component outcomes rather
than claiming that the provider's complete bootstrap routine succeeded.

In the signed `20.2.2` cephadm executable, `cephadmlib/call_wrappers.py` logs a
native command's nonzero exit code and stdout/stderr before `call_throws`
raises its wrapper exception. The bootstrap CLI helper uses
`VERBOSE_ON_FAILURE`. Read the preceding retained native diagnostics: a
truncated outer container argv or `Failed command` wrapper cannot establish
which inner CLI command failed or why. The [pull and diagnostic owner](ceph-native-pull-boundaries.md)
covers retained causes and isolated result validation; [live output retention](adapter-output-live-retention.md)
covers when private logs become visible.

### MON license-record capacity

A non-FIPS 9.9.1.2 run on 2026-11-01 reached native acceptance success, then
failed the worker's persisted-license postcondition. Bounded analysis of the
native journal proved a 123098-byte serialized acceptance payload and the
immediately following MON `-27` rejection against the observed 65536-byte
`mon_config_key_max_entry_size`. A read-only count found no license records.
License text, record keys, payloads and keyrings remain private.

The authenticated 20.2.1 source's `src/mgr/ActivePyModules.cc` waits for the MON
store response but logs a nonzero result without propagating it to the Python
caller; the same behavior is visible in the
[upstream store implementation](https://raw.githubusercontent.com/ceph/ceph/v20.2.1/src/mgr/ActivePyModules.cc). This explains how `accept_license` can return success after its store
write failed. The worker's `license_accepted` reader correctly returns false;
its existing
[license tests](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_ceph_bootstrap_finalization.py)
require readback after native success and refuse absent or mismatched
acceptance. Retrying a read or accepting a cleared health warning cannot fix
this confirmed capacity rejection.

The owner increased the native MON limit to 1048576 bytes. Both the persisted
value and direct running-MON readback proved that value, with no restart.
Ordinary continuation must still persist and prove native acceptance without
changing the frozen input, artifacts or acceptance proof.
The selected native option metadata reports `can_update_at_runtime: true`
despite an empty `flags` list; that list alone does not establish whether a
restart is needed. A persisted value alone remains insufficient, and no restart
is assumed. The limit correction alone qualifies neither license recovery nor
complete cluster acceptance; native continuation remains pending.
The existing
[closed config map](../../specs/api/ceph.md#cephcluster) does not admit this MON
option, and the later settings block cannot repair a prerequisite inside
cluster licensing. [D219](../../specs/milestones/backlog.md#d219) makes the license call raise
the bound to `LICENSE_STORE_FLOOR` before acceptance and prove the persisted
and running values; it is a fixed provider prerequisite, not a release-label
branch or config pass-through. [B1014](../../specs/milestones/m2.md#b1014)
tracks its native acceptance; unassisted bootstrap is not yet observed. The separate
FIPS artifact blocker remains [B1013](../../specs/milestones/m2.md#b1013).
Required acceptance readback stays with the
[readiness contract](../../specs/ceph.md#resources-readiness-and-access).

## 20.1.0-221 (IBM Storage Ceph 9.9.0.3)

The `20.1.0-221` cephadm and MGR are not an IBM licensing build. Four
differences follow from that one cause, and each is a catalog row that alone
holds its URLs, sha256 values and line evidence:

- Bootstrap declares no `--automatically-accept-license`:
  [ceph-bootstrap-without-license-flag](version-specializations.md#ceph-bootstrap-without-license-flag).
- No license-acceptance contract, so `orch accept-license` is invalid and
  `IBM_LICENSE_NOT_ACCEPTED` never exists:
  [ceph-mgr-without-license-contract](version-specializations.md#ceph-mgr-without-license-contract).
- Call Home is opt-in rather than opt-out:
  [ceph-call-home-opt-in](version-specializations.md#ceph-call-home-opt-in).
- `cephadm set-user` has no `--skip-pre-steps`:
  [ceph-set-user-without-pre-steps](version-specializations.md#ceph-set-user-without-pre-steps).

Every `.py` member of the `20.2.2-349.el10cp` cephadm zipapp (sha256
`65dd6256563a314d01a2b1b6d9f462b025ec35093c1c0b709338fdc5b6101661`, from
`rhel10/x86_64/cephadm-20.2.2-349.el10cp.noarch.rpm`, sha256
`4d9191a9edb2aa38ea3bc54ecb54f472422b188392910fab4b037d31e5a24ff0`) parses as
Python 3.9. Its `.pyc` members carry another magic number, so zipimport uses
the `.py` sources on a RHEL 9 host. Runtime behavior on Python 3.9 is still a
first-run watch item.

## Route older and adjacent findings to their owners

| Version-sensitive area | Existing owner |
| --- | --- |
| Historical `20.2.1-405.el9cp` signed packages, EULA hooks, exact cephadm-ansible release, DVD metadata, RPM keys and DNF variants | [Package acquisition](ibm-ceph-package-acquisition.md) |
| Native SSH verification, AsyncSSH proxy-pipe lifetime, remote cephadm cache, protected registry auth and source authentication | [Native SSH](ibm-ceph-native-ssh.md) |
| Native certificate scanning, manifest/blob transport, Podman initialization and supervisor diagnostics | [Pull boundaries](ceph-native-pull-boundaries.md) |
| Inferred bootstrap MON/MGR services versus stored native specifications | [Service projections](ceph-bootstrap-service-projections.md) |
| Dashboard publication, deployment ports, redeploy/reconfig and persisted repair contracts | [Dashboard ports](ceph-dashboard-port-publication.md) |
| Historical RHEL, Podman, OpenSSH, SELinux, output-shape, restart and health observations | [Live-host findings](ibm-ceph-live-host-facts.md) |
| Native removal, shared artifacts and retained-host limitations | [Cleanup](ibm-ceph-cleanup.md) |
| A new automation build continuing a failed/unknown step versus a completed operation | [Continuation](continuation-after-a-fix.md) |

## Evidence boundary for later version handling

Signed binary-package checks and authenticated downstream source archives
establish only the inspected artifacts. The source-RPM inspections authenticate
their headers and changes archives, not a complete downloaded source-RPM
payload. Synthetic tests establish their modeled behavior. Actual-image and
live qualification must independently prove the exact selected image and
package combination, SSH behavior, bootstrap, continuation and removal. Do not
promote historical observations or a passing synthetic probe to that evidence,
and do not generalize one downstream version's behavior to another.
