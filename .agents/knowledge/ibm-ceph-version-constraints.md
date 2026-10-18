# IBM Ceph version constraints and evidence

Use this page to route version-specific investigation and later compatibility
work. It records public artifact findings and existing corrections, not a
release allowlist. [Storage](../../specs/storage.md#frozen-release-and-trust)
owns release and trust requirements; [B472](../../specs/milestones/m2.md#b472)
owns remaining adapter and feature qualification. No version dispatcher,
schema or catalog follows from this inventory alone.

## Current authored example

The [three-node declaration](../../examples/storage-clusters/libvirt-3-nodes-ceph/storage/cluster.yaml)
and its [runbook](../../examples/storage-clusters/libvirt-3-nodes-ceph/README.md)
are the owners of these selections as of 2026-10-18:

| Coordinate | Authored value |
| --- | --- |
| IBM Storage Ceph release | `9.9.2.0`, based on Ceph `20.2.2` |
| Guest OS and native RPM build | RHEL `9.8`; `20.2.2-349.el9cp` |
| Container repository and release alias | `cp.icr.io/cp/ibm-ceph/ceph-9-rhel10:v9.9.2` |
| cephadm-ansible RPM | `5.0.3-1.el9cp` |

The signed [RHEL 9 cephadm RPM](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/cephadm-20.2.2-349.el9cp.noarch.rpm)
names the RHEL 10 image repository in its embedded
`cephadmlib/constants.py`. Container base, guest OS and native RPM build are
separate coordinates; that default does not require RHEL 10 guests. A release
table tag is not evidence that a repository/tag pair exists. Follow the
runbook's authenticated metadata check and operator-owned immutable selection;
the public alias and its eventual frozen digest do not qualify a release.

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
inventory of [verified-tunnel staging](../../specs/storage-native-api.md#verified-native-orchestration-transport).
The native error occurs after the output configuration, administrator keyring
and seed MON/MGR exist, but before `apply_call_home_settings` and the final
Call Home acceptance flag are initialized. A failed bootstrap is still a
failed native command; the existing continuation proves its exact owned seed
instead of restarting bootstrap or enrolling hosts before the tunnel mount.

The [native worker](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py)
initializes missing Call Home state under its original configuration intent
before accepting that enabled intent. Its native `enable_call_home` method
sets the warning/option and enables the module without host inventory or SSH.
An absent flag after an earlier acceptance attempt remains drift. An
interrupted initialization completes only from the exact native result;
missing results never permit a second initialization.

License acceptance runs after owned enrollment and current tunnel, cache and
mount proofs. Its native command remains fallible. Readiness separately reads
the provider's persisted MON acceptance entry, bound to the profile's image
configuration ID. That ID differs from the selected platform-manifest digest.
The source's `src/python-common/ceph/cephadm/license_utils.py` derives the key
from the full native version and exact English license text; its MD5 is a
lookup suffix, not an artifact-authentication proof. The worker validates the
key and payload without invoking native license extraction during observation.
Missing acceptance is unfinished; malformed evidence refuses.

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
