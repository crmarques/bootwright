# Version specializations

The catalog of every deviation from a component's general flow, and of the
cases deliberately kept general. The rule is owned by
[architecture](../../specs/architecture.md#implementations-and-version-variation)
(D162): one general flow per component, and a small specialization only
where the general flow is known to fail on an artifact that lacks or has a
provable capability. Rows are tested scope, never approved versions
([release qualification](../../specs/storage-coverage.md#release-qualification)).
The catalog records no version pairing or compatibility rule between
components, and choosing versions is the operator's responsibility (D163).
A registry and this page are kept equal by `TestDocsVersionSpecializationCatalogMatchesRegistries` in
`test/architecture`, which `make docs-check` runs. The ids a run applied
appear in the refusal coordinate, in the `specializations` member of the Ceph
core and ceph-host block evidence, and on the full `cluster info` line.

Path abbreviations in the code and test cells: `P/` is
`ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/`
and `T/` is
`ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/`.

## Active specializations

One row per registry record. The first four are one root cause: IBM Storage
Ceph `20.1.0-221` is not an IBM licensing build, so its cephadm and MGR lack
the licensing, opt-out Call Home and sudo-hardening mechanics the `20.2.x`
builds carry. The fifth is a `20.2.x` workaround. Each fact below is derived
from the bytes of an artifact the operation has already frozen and proved; the
release label and the image tag key nothing.

| id | registry | component | when | builds matched / not matched | kind | seam to variant | why | general flow on matched builds | evidence | code | test |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `ceph-bootstrap-without-license-flag` | python | ceph-cephadm (host RPM zipapp) | `host-cephadm.license-flag` is False | matched `20.1.0-221.el9cp`; not matched `20.2.1-405.el9cp`, `20.2.2-349.el9cp`, `20.2.2-349.el10cp` | workaround | `ceph.bootstrap.license-flag` to `omit` | The bootstrap parser of `20.1.0-221` does not declare `--automatically-accept-license`, and cephadm parses strictly. | Cannot have completed: cephadm exits 2 on the unrecognized option before any effect. | [cephadm-20.1.0-221.el9cp RPM](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/cephadm-20.1.0-221.el9cp.noarch.rpm) sha256 `af0e7053c765de766abb3c81991cfa31af425c1cfca31985320b4623afe75a45`; its `/usr/sbin/cephadm` zipapp sha256 `8898bc07ddb5a44684cfd3b4e5681dae6cd1b62d242cebe4a565cc6512f5b33e`; `__main__.py:5559` bootstrap parser, `:5781` its last IBM option `--enable-ibm-call-home`, `:6050` strict `parse_args`, no `--automatically-accept-license` anywhere; `20.2.1-405` declares it at `:6454` (RPM sha256 `7acf50c99cf3263c64a103476dcf67a3fd93a7b918472db7df660541ea987287`, zipapp sha256 `027222324b11d1af53b72b4ebb17134f58e477f63c39e9d07be40ba1fc2aabc1`), `20.2.2-349.el9cp` at `:6557` (RPM sha256 `1d323901a72b7b56554278f31b53e0400c8b14382daf22efdbdb2f94644a4994`, zipapp sha256 `381a5d2e5b3332f7604de0e7f9118ced26ffeae6e21b07372835a4d2a05c5bfc`); declared-name lists `T/data/cephadm_declared/20.1.0-221.el9cp.json`, `20.2.1-405.el9cp.json`, `20.2.2-349.el9cp.json`, `20.2.2-349.el10cp.json` | P/native_specializations.py (record); P/ceph_tunnel_staging.py `bootstrap_argv` (seam `ceph.bootstrap.license-flag`); wiring in P/ceph_native.py `Worker.__init__`, `verified_tool` and `image_payload` | T/test_native_specializations.py (detector, selection and every signed build's list); T/test_ceph_tunnel_staging.py `test_every_twenty_two_build_keeps_todays_bootstrap_argv`, `test_a_twenty_one_host_cephadm_bootstraps_without_the_license_flag`, `test_an_unanchored_host_cephadm_keeps_the_general_license_flag`; T/test_ceph_tunnel_native.py `test_a_twenty_two_host_cephadm_bootstraps_with_todays_argv`, `test_a_twenty_one_host_cephadm_bootstraps_without_the_license_flag_and_continues_to_capture` |
| `ceph-mgr-without-license-contract` | python | ceph-image (MGR modules, image cephadm payload) | `image-cephadm.ibm-build` is False | matched `20.1.0-221` payload; not matched the `20.2.1-405`, `20.2.2-349.el9cp` and `20.2.2-349.el10cp` payloads | workaround | `ceph.mgr.license-contract` to `absent` | `20.1.0-221` has no IBM build mode, so `orch accept-license` is an invalid MGR command and `IBM_LICENSE_NOT_ACCEPTED` never exists or clears. | Cannot have completed: enrollment fails on the invalid command, and readiness would wait on a license key that is never written. | Signed zipapp (sha256 `8898bc07ddb5a44684cfd3b4e5681dae6cd1b62d242cebe4a565cc6512f5b33e`) has no `cephadmlib/override_vars.py`; the `20.2.1` and `20.2.2` zipapps hold it with line 1 `IBM_BUILD = True`, read through `cephadmlib/context.py:17-20`, gating `orch accept-license` at `__main__.py:3445-3447` (`20.2.1`, only with the auto-accept flag) and `:3534-3535` (`20.2.2`, always). MGR side from the SRPM [ceph-20.1.0-221.el9cp.src.rpm](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/source/ceph-20.1.0-221.el9cp.src.rpm) sha256 `36703945be8e34502496362957ee9a03f7e98b2a327b0971dc0090a21f346652`, downstream changes archive: `src/pybind/mgr/orchestrator/module.py` has no `orch accept-license` (0 hits in 2617 lines; registered at `20.2.1` line 2668 and `20.2.2` line 2773, followed by the IBM Call Home commands `orch enable-call-home` and `orch accept call-home-enabled` at 2677 and 2686, and at 2782 and 2791) and `src/pybind/mgr/cephadm/module.py` has no `IBM_LICENSE_NOT_ACCEPTED` (`20.2.2` lines 5248-5288). Read from signed artifacts, not from the entitled image | P/native_specializations.py (record); P/ceph_native.py `Worker.enroll` and `Worker.observe_cluster` (seam `ceph.mgr.license-contract`) | T/test_native_specializations.py; T/test_ceph_bootstrap_finalization.py `test_a_manager_without_the_license_contract_enrolls_without_accepting_a_license`, `test_an_ibm_or_unknown_manager_still_accepts_its_license_at_enrollment`; T/test_ceph_core.py `test_a_manager_without_the_license_contract_is_ready_without_license_keys`, `test_the_managers_own_license_check_still_blocks_a_cluster_without_the_contract` |
| `ceph-call-home-opt-in` | python | ceph-cephadm and ceph-image | `host-cephadm.call-home-opt-out` is False and `image-cephadm.ibm-build` is False | matched `20.1.0-221`; not matched the three `20.2.x` builds | behavior | `ceph.call-home-model` to `opt-in` | Call Home on `20.1.0-221` is opt-in (`--enable-ibm-call-home` plus customer identity) and its MGR lacks the commands the `20.2.x` opt-out flow drives. | Cannot have completed: with `enabled` the first `orch enable-call-home` is an invalid command; `disabled` is the native default and needs no step. | Zipapp sha256 `8898bc07ddb5a44684cfd3b4e5681dae6cd1b62d242cebe4a565cc6512f5b33e`: `__main__.py:2795-2830` `verify_call_home_settings` requires contact identity only with `--enable-ibm-call-home`, `:2864-2866` skips the integration otherwise and `:2870-2884` enables `call_home_agent` only when enabled; `20.2.x` declares `--disable-ibm-call-home` (`:6449` in `20.2.1-405`, zipapp sha256 `027222324b11d1af53b72b4ebb17134f58e477f63c39e9d07be40ba1fc2aabc1`; `:6552` in `20.2.2-349`), runs `orch enable-call-home` (`:3157`, `:3246`) and sets `call_home_needs_acceptance` (`:3506-3508`, `:3594-3596`); MGR `orchestrator/module.py:2677-2703` (`20.2.1`) from the SRPM sha256 `4f17060f724aa5b1fd06e4c915b3db376c8062c99d5e583c01f723a7333651d4`, absent in `20.1.0-221` (SRPM sha256 `36703945be8e34502496362957ee9a03f7e98b2a327b0971dc0090a21f346652`) | P/native_specializations.py (record); P/ceph_native.py `Worker.require_call_home_path` (in `claim` and `tunnel_bootstrap`), `call_home_applicable`, `initialize_call_home`, `configure_call_home`, `accept_call_home`, `configuration_complete` and `call_home_state` (seam `ceph.call-home-model`) | T/test_native_specializations.py; T/test_ceph_tunnel_native.py `test_claim_refuses_an_unqualified_call_home_path_before_any_cluster_marker`, `test_a_twenty_one_manager_is_configured_without_pre_steps_or_any_call_home_step`, `test_an_enabled_call_home_agent_on_an_opt_in_manager_is_drift`; T/test_ceph_bootstrap_finalization.py `test_opt_in_call_home_left_disabled_has_no_step_marker_or_effect`, `test_every_other_disabled_call_home_path_keeps_the_enabled_only_guard`, `test_enabled_intent_on_an_opt_in_manager_keeps_the_general_call_home_step`; T/test_ceph_core.py `test_an_enabled_call_home_agent_on_an_opt_in_cluster_is_drift`; T/test_ceph_bootstrap_recovery.py `test_a_refusal_before_the_attempt_record_continues_under_a_corrected_selection` |
| `ceph-set-user-without-pre-steps` | python | ceph-image (MGR modules, image cephadm payload) | `image-cephadm.ssh-user-setup` is False | matched `20.1.0-221` payload; not matched the `20.2.1-405`, `20.2.2-349.el9cp` and `20.2.2-349.el10cp` payloads | workaround | `ceph.mgr.set-user-pre-steps` to `absent` | `cephadm set-user` of `20.1.0-221` has no `--skip-pre-steps` because it has no pre-steps, so the extra argument is rejected. | Cannot have completed: the MGR command parser rejects the undeclared option (`EINVAL`) before any effect. | SRPM [ceph-20.1.0-221.el9cp.src.rpm](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/source/ceph-20.1.0-221.el9cp.src.rpm) sha256 `36703945be8e34502496362957ee9a03f7e98b2a327b0971dc0090a21f346652`, changes archive `src/pybind/mgr/cephadm/module.py:1353-1355` `set_ssh_user(self, user)`; `20.2.1` `:1506-1507` and `20.2.2` `:1574-1575` `set_ssh_user(self, user, skip_pre_steps=False)` running sudo hardening unless skipped (`20.2.2` `:1587`); the `20.2.x` cephadm zipapps add the `setup-ssh-user` and `prepare-host-sudo-hardening` subcommands, absent from the `20.1.0-221` zipapp (sha256 `8898bc07ddb5a44684cfd3b4e5681dae6cd1b62d242cebe4a565cc6512f5b33e`) | P/native_specializations.py (record); P/ceph_native.py `Worker.configure_values`, the `set-user` command tuple (seam `ceph.mgr.set-user-pre-steps`) | T/test_native_specializations.py; T/test_ceph_tunnel_native.py `test_a_twenty_two_manager_receives_set_user_with_skip_pre_steps`, `test_a_twenty_one_manager_is_configured_without_pre_steps_or_any_call_home_step`, `test_an_unanchored_image_payload_keeps_skip_pre_steps` |
| `ceph-bootstrap-license-call-continues` | python | ceph-cephadm (host RPM zipapp) | `host-cephadm.bootstrap-license-call` is True | matched `20.2.1-405.el9cp`, `20.2.2-349.el9cp`, `20.2.2-349.el10cp`; not matched `20.1.0-221.el9cp` | workaround | `ceph.bootstrap.license-call-refusal` to `continue-seed` | The bootstrap of the IBM `20.2.x` builds calls `orch accept-license --image`, which the MGR refuses with `no hosts defined` on the zero-host seed after the seed exists; the exact owned seed continues in the same apply. | Bootstrap exits 1 after the seed exists; the attempt record and the seed remain and the next apply continues them at stage `seed` (no version move). | RPM [cephadm-20.2.1-405.el9cp](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/cephadm-20.2.1-405.el9cp.noarch.rpm) sha256 `7acf50c99cf3263c64a103476dcf67a3fd93a7b918472db7df660541ea987287`, zipapp sha256 `027222324b11d1af53b72b4ebb17134f58e477f63c39e9d07be40ba1fc2aabc1`, the call at `__main__.py:3445-3447`; RPM [cephadm-20.2.2-349.el9cp](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64/cephadm-20.2.2-349.el9cp.noarch.rpm) sha256 `1d323901a72b7b56554278f31b53e0400c8b14382daf22efdbdb2f94644a4994`, zipapp sha256 `381a5d2e5b3332f7604de0e7f9118ced26ffeae6e21b07372835a4d2a05c5bfc`, the call at `__main__.py:3534-3535`; the output shape `cephadmlib/call_wrappers.py:280-310` and `cephadmlib/logging.py:118-160`; the MGR refusal `src/pybind/mgr/cephadm/serve.py:2696-2703` (`20.2.2` source RPM, changes archive); the committed lists `T/data/cephadm_declared/20.1.0-221.el9cp.json`, `20.2.1-405.el9cp.json`, `20.2.2-349.el9cp.json`, `20.2.2-349.el10cp.json`. The live operation is unqualified, so no operation id is cited | P/native_specializations.py (record, fact `host-cephadm.bootstrap-license-call`); P/ceph_tunnel_staging.py `license_call_refusal`; P/ceph_native.py `Worker.command` (`recover`), `tunnel_bootstrap` and `Worker.license_call_continues` (the one consult of the seam) | T/test_native_specializations.py `test_bootstrap_license_call_true_on_an_ibm_build_that_lists_it`, `test_twenty_two_shaped_artifacts_keep_the_general_flow_but_continue_the_license_call_refusal`; T/test_ceph_tunnel_staging.py `test_the_ibm_license_call_refusal_is_recognized_exactly`; T/test_ceph_bootstrap_finalization.py `test_ibm_license_call_refusal_continues_the_owned_seed_in_the_same_apply` |

### Record notes

What each variant does at its seam, and where control returns to the general
flow. These headings are the anchors of the ids above.

#### ceph-bootstrap-without-license-flag

The bootstrap argument vector omits `--automatically-accept-license`. The next
line is the general flow, which proves the seed exactly as for any build.

#### ceph-mgr-without-license-contract

Enrollment skips `orch accept-license` and readiness treats the license as not
applicable. The MGR's own `IBM_LICENSE_NOT_ACCEPTED` health check stays the
safety net: a wrongly chosen variant on a build that has the contract is still
blocked by it.

#### ceph-call-home-opt-in

The Call Home path is checked in the cluster block's claim, before the cluster
is claimed but after the ceph-host blocks ran from the frozen input. A wrong
`callHome` is therefore an input change: destroy the operation, update the
context and apply again. On this model `disabled` is the native default: `initialize_call_home`,
`configure_call_home` and `accept_call_home` do not apply, and the worker proves
that `call_home_agent` is not enabled. `enabled` refuses `call-home-unqualified`
because Bootwright does not model the contact identity that enabling needs. The
[storage API](../../specs/api/storage.md) owns the realization.

#### ceph-set-user-without-pre-steps

The MGR `set-user` command is sent without `--skip-pre-steps`. The key is
causal: a payload without the `setup-ssh-user` subcommand cannot run pre-steps,
so omitting the option causes no host sudoers effect, provided the parser is
anchored. An unanchored or unreadable parser makes the fact unknown, and
unknown keeps `--skip-pre-steps`.

#### ceph-bootstrap-license-call-continues

A bootstrap that exits 1 at the provider's license call on the zero-host seed
continues in the same apply. The worker recognizes exactly that refusal in the
bootstrap's private native output, never in the published or redacted cause,
proves the exact owned seed as the next apply would, and then runs the general
capture of the bootstrap outputs. Any other failure, an unproved seed or an
unknown fact keeps the failed native command and its attempt record, and the
next apply's continuation proves the same seed.

### General topology rule (OpenShift)

The one rule of a Go-owned component. It is not a specialization: it applies
to every release minor, and no per-minor record exists until an installer
first differs. Decided in D162 clause (c).

| id | registry | component | rule | read from | code | test |
| --- | --- | --- | --- | --- | --- | --- |
| `openshift-general-topology` | go:containercluster | openshift-installer | master counts 1, 3, 4, 5 for every minor without a specialization; the installer judges the release | `openshift/installer` agent control plane, release-4.21 [installconfig.go lines 217-232](https://github.com/openshift/installer/blob/release-4.21/pkg/asset/agent/installconfig.go#L217-L232), [lines 234-263](https://github.com/openshift/installer/blob/release-4.21/pkg/asset/agent/installconfig.go#L234-L263); release-4.22 [lines 220-305](https://github.com/openshift/installer/blob/release-4.22/pkg/asset/agent/installconfig.go#L220-L305); release-4.23 [lines 220-305](https://github.com/openshift/installer/blob/release-4.23/pkg/asset/agent/installconfig.go#L220-L305) | `internal/containercluster/topology.go` `generalTopology` and `topologyFor`, consulted by `validateTopology` | `internal/containercluster/admission_test.go` `TestTopologyAppliesTheGeneralRuleToEveryReleaseMinor`, `TestTopologyAdmitsOnlyTheControlPlaneCountsItsReleaseAccepts`, `TestTopologyTableMatchesSpec`, `TestEveryExampleReleaseHasATopologyRow` |

## Seams

A seam is one named decision point in a general flow with a closed set of
variants, one of them general. The code asks the seam and never branches on a
version. A refusal seam is one whose variants change which intents the adapter
admits; a record may add a pre-effect refusal there and may never remove one
that guards trust.

| seam | variants | general | refusal | call sites |
| --- | --- | --- | --- | --- |
| `ceph.bootstrap.license-flag` | `pass`, `omit` | `pass` | no | P/ceph_tunnel_staging.py `bootstrap_argv` |
| `ceph.mgr.license-contract` | `ibm`, `absent` | `ibm` | no | P/ceph_native.py `Worker.enroll`, `Worker.observe_cluster` |
| `ceph.call-home-model` | `opt-out`, `opt-in` | `opt-out` | yes | P/ceph_native.py `Worker.require_call_home_path`, `call_home_applicable`, `call_home_state` |
| `ceph.mgr.set-user-pre-steps` | `skip`, `absent` | `skip` | no | P/ceph_native.py `Worker.configure_values` |
| `ceph.bootstrap.license-call-refusal` | `fail`, `continue-seed` | `fail` | no | P/ceph_native.py `Worker.license_call_continues` |

## Decided general

Cases examined and kept in the general flow. Do not re-propose them as
specializations without new evidence. Each line names the case and links the
page that owns its reasoning.

| Case | Owner |
| --- | --- |
| A version gate on the AsyncSSH release, removed in favor of a behavioral proof | [SSH SDK and provider imports](ibm-ceph-version-constraints.md#ssh-sdk-and-provider-imports) |
| The `accept-license` call differing between `20.2.1` and `20.2.2` | [Bootstrap license call](ibm-ceph-version-constraints.md#bootstrap-license-call-and-diagnostics) |
| `--skip-prepare-host`, never passed | [Bootstrap container-engine initialization](ceph-native-pull-boundaries.md#bootstrap-container-engine-initialization) |
| The `rhel9` versus `rhel10` container repository and cephadm's default image | [Current authored example](ibm-ceph-version-constraints.md#current-authored-example) |
| The `20.2.2` relative `cephadm.utils` imports of the MGR SSH layer | [Provider runtime and package imports](ibm-ceph-native-ssh.md#provider-runtime-and-package-imports) |
| Listener address reuse across AsyncSSH `2.13.2` and `2.17.0` | [Listener replacement](ibm-ceph-native-ssh.md#listener-replacement-and-socket-reuse) |
| `ACCEPT_EULA` and the license package, one value satisfying every hook | [Package acquisition](ibm-ceph-package-acquisition.md) |
| The cephadm-ansible release suffix `.el9cp` | [Authored cephadm-ansible RPM release](ibm-ceph-package-acquisition.md#authored-cephadm-ansible-rpm-release) |
| Hard-linked crypto-policies files and the sshd include layout | [Guest facts after the solve](ibm-ceph-package-acquisition.md#guest-facts-the-packages-phase-meets-after-the-solve) |
| Output-shape unions (`config-key get` newline, `orch ls` sentinel, Podman inspect shapes) | [Tool output shapes](ibm-ceph-live-host-facts.md#tool-output-shapes) |
| MGR restarts, readiness budgets and health ceilings | [Timing and health](ibm-ceph-live-host-facts.md#timing-and-health) |
| Dashboard endpoint and port publication | [Dashboard ports](ceph-dashboard-port-publication.md) |
| Whether a build's cephadm knows the `loki`, `alloy` or `promtail` service type: normalization runs the selected image's own `ServiceSpec`, and an unknown type surfaces as `nativeError` before any write | [Resources, readiness and access](../../specs/storage.md#resources-readiness-and-access) |
| Auxiliary image selection is not a version specialization; its contract belongs to B479 | [B479](../../specs/milestones/m2.md#b479) |
| The default package repository `rhel9`, a source default | [Storage API](../../specs/api/storage.md) |
| The selected image's OpenSSH default host-key list and its crypto policy's `+`, `^` and `-` `HostKeyAlgorithms` forms, evaluated generally by the tunnel's per-host intersection | [Network, remote systems, and privilege](../../specs/security.md#network-remote-systems-and-privilege) |
| Native transport stays refused; no specialization lifts it | [Release qualification](../../specs/storage-coverage.md#release-qualification) |
| The zone pools and placement shape a build's `zone get` reports: a subset of the planned derived and declared pools is accepted, a pool outside them refuses, `placement_pools` reads as a list or a mapping | [StorageObjectGateway field record](../../specs/storage-resources.md#storageobjectgateway-field-record) |
| A build's `orch ls --export` keeps a gateway service's inline `ssl_cert` and `ssl_key` or omits both: either is accepted when the stored leaves equal the bound ones, and the served leaf's SHA-256 is the postcondition | [Resources, readiness and access](../../specs/storage.md#resources-readiness-and-access) |
| The NFS export and CephFS StorageClient readback: `fs subvolume getpath` prints the path with or without one trailing newline, `nfs export info` reads `{}` for an absent pseudo path, and the filtered readback projection, never the apply's answer alone, is the postcondition | [Resources, readiness and access](../../specs/storage.md#resources-readiness-and-access) |
| The S3 key pair `radosgw-admin user create` generates (20 characters of `0-9A-Z` and 40 of `A-Za-z0-9` at `v20.2.1`) and the members `user info` adds: Bootwright retains any key of 1 to 128 visible ASCII characters other than `&`, `<` and `>` and proves only the members it froze, so no build's key format or user shape is pinned | [StorageObjectUser field record](../../specs/storage-resources.md#storageobjectuser-field-record) |
| The bucket ID and the other members `bucket stats` prints, and RGW's S3 error documents: Bootwright records any bucket ID of 1 to 256 characters of `A-Za-z0-9_.:-`, proves only the bucket's name, tenant, owner, zone group and recorded ID, and reads only an error's `Code` and `Message`, so no build's ID format or stats shape is pinned | [StorageObjectBucket field record](../../specs/storage-resources.md#storageobjectbucket-field-record) |
| Whether a build's cephadm opens daemon ports in firewalld: upstream opens each daemon's TCP endpoints in the default zone and never VRRP; the ingress check reads the zone's runtime state before effects instead | [Resources, readiness and access](../../specs/storage.md#resources-readiness-and-access) |
| The SMB resource readback: a build's `smb show` answers one named resource itself or in a `resources` list, may emit or omit unset optional leaves, may spell placement hosts as names or host objects and may add leaves Bootwright does not author; every admitted leaf after the manager's defaults, every refused leaf unset and every password masked is the postcondition | [Resources, readiness and access](../../specs/storage.md#resources-readiness-and-access) |
| A build's `orch ls --export` keeps a monitoring service's bound credential leaves (grafana `initial_admin_password`, snmp-gateway `credentials`) or omits them: either is accepted when the stored leaves equal the bound ones at apply | [Resources, readiness and access](../../specs/storage.md#resources-readiness-and-access) |
| Whether a build's cephadm renders oauth2-proxy with `ssl_insecure_skip_verify` or mounts its `custom_configs`: the deployed container's arguments, environment, mounted `--config` file and CA mount, read on its host after the apply, are the postcondition | [Resources, readiness and access](../../specs/storage.md#resources-readiness-and-access) |
| A build infers the management gateway's `certificate_source` (`inline` with bound leaves, `cephadm-signed` without) and may keep or omit the leaves in `orch ls --export`: normalization runs the selected image's own `ServiceSpec`, and the served leaf's SHA-256 is the postcondition | [Resources, readiness and access](../../specs/storage.md#resources-readiness-and-access) |
| `imageDigestSources[].sourcePolicy` is rendered exactly when authored and the installer judges the release | [B724](../../specs/milestones/m3.md#b724) |
| Disk encryption renders only `none`, `all`, `masters` and `workers`, the selectors the installer's validation accepts in the 4.20 to 4.23 branches read (RV R6:C13); the installer judges the release | [B720](../../specs/milestones/m3.md#b720) |

## Adding a specialization

Evidence first, then at most four touch points, usually three.

0. Reproduce the difference against the real artifact and record the evidence:
   URL and sha256, file and line, or the live operation id. Then decide general
   first: can the general flow absorb it safely, as a union of output shapes
   with a postcondition or as a behavioral probe? If yes, fix the general flow,
   add a Decided general row and stop.
1. Registry: add the record. Add a fact detector only when no existing fact
   discriminates, and a variant only when no existing one fits. A newly read
   build also gets its committed declared-name list in
   `T/data/cephadm_declared`, before its first run.
2. Call site, only for a new variant or seam: one branch at the seam, reviewed at
   the reasoning tier. A seam never sits in trust, host-key, signature,
   redaction or credential code.
3. Unit test: the detector over a minimal archive built in the test, with and
   without the feature, plus the build's name list; never commit provider
   bytes. Also test the selection and the call-site behavior.
4. Catalog row, including the general-flow column. If the general flow could
   have completed the step on a build the record matches, the change also moves
   the implementation version
   ([continuation](continuation-after-a-fix.md#rule)).

Then `make build` and `bootwright apply --stage <same>`; gates are `make focus`
while iterating, `./scripts/ansible-check --suite units`, `make docs-check` and
`make check` before integration.
