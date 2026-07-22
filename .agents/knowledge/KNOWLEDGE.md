# Knowledge Index

Non-spec knowledge extracted from the codebase. Two indexes:

- **Failures** — match a reported symptom or error fragment, then load only the linked file. Check here first on any unexpected failure.
- **Constraints & semantics by area** — durable rules, invariants, and non-obvious behavior. Scan the relevant area before changing code there.

Decisions with durable rationale live in [`specs/adr/`](../../specs/adr/README.md); schema semantics live in `specs/state-model.md` and `docs/concepts/`. Source files carry no prose comments (ADR 0006).

## Failures

| Category | Match symptoms / keywords | File |
| --- | --- | --- |
| Ansible / sudo | `Duplicate become password prompt`; apply hangs mid-play on sudo | [sudo-ansible-duplicate-prompt.md](sudo-ansible-duplicate-prompt.md) |
| Ansible / sudo | `sudo: sorry, you must have a tty to run sudo`; `sudo: a password is required`; `Install controller OS packages` | [ansible-sudo-requiretty.md](ansible-sudo-requiretty.md) |
| Ansible / remote tmp | `can't open file '<home-dir>/.ansible/tmp`; `[Errno 13] Permission denied`; `No space left on device`; `AnsiballZ_setup.py`; `AnsiballZ_uri.py` | [ansible-remote-tmp-permission.md](ansible-remote-tmp-permission.md) |
| Ansible / sudo | `bootwright apply <target>` auto-sudo cannot find ansible; `ModuleNotFoundError` under sudo | [pip-user-sudo-pythonpath.md](pip-user-sudo-pythonpath.md) |
| Ansible / sudo | `Ensure tmp working directory exists`; permission denied under root-owned controller temp paths | [ansible-controller-clis-root-temp.md](ansible-controller-clis-root-temp.md) |
| Ansible / Galaxy | `Unexpected Exception`; `'results'`; `Skipping Galaxy server`; `community.general` | [ansible-galaxy-results-build.md](ansible-galaxy-results-build.md) |
| Ansible / embed | Extracted bundle missing `_respawn.py`, `__init__.py`, dot/underscore files | [ansible-embed-underscore-files.md](ansible-embed-underscore-files.md) |
| Ansible / roles | `bootwright_current_cluster is undefined`; dynamic role import fails | [ansible-dynamic-role-dispatch.md](ansible-dynamic-role-dispatch.md) |
| Ansible / runtime | `Module result deserialization failed`; `rc=-15`; cleanup killed Ansible wrapper | [ansible-module-wrapper-pkill.md](ansible-module-wrapper-pkill.md) |
| Ansible / runtime | `Could not install packages due to an OSError`; `[Errno 2] No such file or directory`; `install ansible-core` | [ansible-managed-venv-rebuild.md](ansible-managed-venv-rebuild.md) |
| Ansible / callback | `Callback dispatch 'v2_runner_on_skipped' failed for plugin 'default'`; `Build proxy environment facts` | [ansible-callback-skipped-pin.md](ansible-callback-skipped-pin.md) |
| Ansible / transfer | `Fetch generated agent ISO to local cluster runtime state`; large file fetch appears stuck; `.openshift_install.log` idle after ISO generation | [ansible-fetch-become-large-file.md](ansible-fetch-become-large-file.md) |
| Ansible / packages | `Failed to download metadata for repo`; `Cannot download repomd.xml`; `There are no enabled repositories`; `All mirrors were tried` | [ansible-dnf-unavailable-repo.md](ansible-dnf-unavailable-repo.md) |
| Provider / BMC | Apply hangs at BMC wait tasks; port already in use after provider rename | [stale-bmc-port-wait-hang.md](stale-bmc-port-wait-hang.md) |
| OpenShift install | Disconnected install fails TLS; agent never reaches SSH; image pull x509 error | [disconnected-trust-bundle-policy.md](disconnected-trust-bundle-policy.md) |
| OpenShift install | Mirror push x509 SAN mismatch after self-signed cert spec change | [self-signed-cert-drift.md](self-signed-cert-drift.md) |
| OpenShift install | Agent ISO cache stale; `cannot generate ISO image due to configuration errors` | [openshift-agent-iso-cache.md](openshift-agent-iso-cache.md) |
| OpenShift install | `Missing install-config.yaml`; `Fail when install-config is missing`; boot task after successful ISO generation | [openshift-install-config-consumed.md](openshift-install-config-consumed.md) |
| OpenShift install | `SQUASHFS error: Unable to read page`; `Unable to read fragment cache entry`; agent API never initializes after virtual media eject | [openshift-agent-iso-squashfs-detach.md](openshift-agent-iso-squashfs-detach.md) |
| OpenShift install | `v2GetClusterNotFound`; `Writing image to disk: 100%`; node boots agent ISO again | [openshift-agent-iso-reboot-loop.md](openshift-agent-iso-reboot-loop.md) |
| OpenShift install | `Only platform none and external supports 1 ControlPlane and 0 Compute nodes`; SNO renders `platform.baremetal` | [openshift-sno-platform-none.md](openshift-sno-platform-none.md) |
| OpenShift install | `hosts[0].interfaces: Required value`; libvirt profile-backed nodes have no MAC inventory | [openshift-agent-host-interfaces.md](openshift-agent-host-interfaces.md) |
| OpenShift install | `Bootstrap Kube API never initialized`; `no such host`; endpoint DNS missing; `providedBy` or `externalVip` | [external-dns-bootstrap.md](external-dns-bootstrap.md) |
| Libvirt / network | API VIP unreachable; `Bootstrap Kube API never initialized` | [libvirt-vip-bootstrap.md](libvirt-vip-bootstrap.md) |
| Libvirt / network | UUID mismatch; bridge already in use; stale libvirt XML | [libvirt-network-drift.md](libvirt-network-drift.md) |
| Redfish / boot | `InsertMedia` fails; `did not report the requested agent ISO`; `Inserted=False`; `VerifyCertificate PATCH status=412`; `ssl.SSLError`; emulator HTTPS mismatch; virtual media path mismatch; `Verify running libvirt virtual media source is absent`; `Confirm staged agent ISO fetch URL is reachable`; `HEAD status 404`; multi-GB ISO-shaped `/tmp/tmp*` files | [redfish-virtual-media.md](redfish-virtual-media.md) |
| Redfish / boot | VirtualMedia discovery reports `status=403`; direct `curl -u` to the same BMC URL succeeds; play-level proxy environment intercepts Redfish | [redfish-proxy-bypass.md](redfish-proxy-bypass.md) |
| Redfish / boot | `Stage agent ISO at the BMC's fetch location`; `Gathering Facts`; `UNREACHABLE`; `Failed to connect to the host via ssh`; `localhost -> bastion`; wrong `ansible_user` | [redfish-local-artifact-staging.md](redfish-local-artifact-staging.md) |
| Redfish / boot | Reset(On) returns 204 but VM stays `shut off`; install loops on `no route to host` | [redfish-power-on-silent-noop.md](redfish-power-on-silent-noop.md) |
| Managed OS / libvirt | `Scan managed OS SSH host key`; `Connection reset by peer`; VM console shows wrong RHEL version; `I/O error, dev vda`; `XFS (vda3): log I/O error` | [managed-os-libvirt-stale-disk.md](managed-os-libvirt-stale-disk.md) |
| Python / Ansible | Python 3.12 CIDR check returns false; VIP not matched to bridge CIDR | [python-312-cidr-filter.md](python-312-cidr-filter.md) |
| Add-ons / OLM | readiness check JSON unmarshal fails; oc deprecation/TLS/auth warnings in captured output; `csvSucceeded` decode error; panic in io.MultiWriter during readiness poll; quiet runner nil writer | [addon-oc-runner-output.md](addon-oc-runner-output.md) |
| Ansible / runtime | set_fact value with {{ hostvars }}/ansible_* resolves empty or wrong; broke on ansible-core 2.21 bump; template captures not-yet-gathered node facts; works when evaluated later | [ansible-core-eager-setfact.md](ansible-core-eager-setfact.md) |
| Ansible / runtime | play frozen mid-task with no error or timeout; host mid-reboot or powered off; SSH banner printed then stall; `ConnectTimeout=15`; `ServerAliveInterval=15`; `ServerAliveCountMax=3`; `ssh_common_args` | [ansible-ssh-liveness-timeouts.md](ansible-ssh-liveness-timeouts.md) |
| Bare-metal & managed-OS install | Key exchange type c25519 is not allowed in FIPS mode; ssh-keyscan retries forever; StrictHostKeyChecking=accept-new; ssh-keygen -R; ecdh-sha2-nistp256 | [fips-ssh-keyscan-hostkey.md](fips-ssh-keyscan-hostkey.md) |
| Bare-metal & managed-OS install | Could not find or access; mkksiso; install_media.yml; remote_src: false; sourceOnTarget; source ISO missing on controller; bootwright media add | [managed-os-install-media-missing.md](managed-os-install-media-missing.md) |
| Bare-metal & managed-OS install | StrictHostKeyChecking fails forever after a reinstall; stale known_hosts pin; host key changed on freshly installed node; parallel nodes corrupt shared known_hosts; `flock`; `ssh-keygen -R` | [managed-os-ssh-trust.md](managed-os-ssh-trust.md) |
| Bare-metal & managed-OS install | Anaconda never runs; node boots existing OS from disk after Redfish power-on; foreign OS answers SSH at auth/ownership check; `BootSourceOverrideEnabled=Once` overwritten to Hdd during POST; `readiness.type=none`; `setBootSource` | [redfish-post-boot-disk-override.md](redfish-post-boot-disk-override.md) |
| Ceph storage | proxy = _none_; repomd.xml unreachable; baseos/appstream; unable to get local issuer certificate; sslcacert redhat-uep.pem; repo_ca_cert; subscription-manager refresh; rhsm.conf 0600; bootwright-proxy-ca.pem; bootwright-satellite-ca.pem; bootwright-cephadm-registry.pem | [rhsm-proxy-and-repo-ca.md](rhsm-proxy-and-repo-ca.md) |
| OpenShift / OKD clusters | quay.io/openshift-release-dev/ocp-v4.0-art-dev; disconnected nodes pull quay.io despite mirror; imageDigestSources; release component images; NeverContactSource; openshift/release-images | [disconnected-release-image-artdev.md](disconnected-release-image-artdev.md) |
| OpenShift / OKD clusters | Could not open <iso>: Permission denied; libvirt-direct attach fails to start guest; publish_target.yml mode 0711; qemu cannot traverse per-token directory; stagePath publish token; no_log redaction | [openshift-agent-iso-publish-permissions.md](openshift-agent-iso-publish-permissions.md) |
| OpenShift / OKD clusters | no matching resources found; kubectl wait --for=condition=Ready vmi; VM boots stale agent ISO on retry; virtctl start already running no-op; virtctl image-upload stamps no labels; bootwright.io/managed-by; bootwright_kubevirt_agent_iso_size 4Gi upload fails | [openshift-kubevirt-agent-boot.md](openshift-kubevirt-agent-boot.md) |
| CLI / SSH | `cluster rsh`; `declares no SSH access (spec.access.ssh)`; OpenShift node Machine has no access block; encrypted node SSH key | [cluster-node-ssh-access.md](cluster-node-ssh-access.md) |
| Rendering & templates | hostname verification failed after SAN edit; artifacts-openssl.cnf.j2; bootwright_artifacts_tls_openssl_cnf is changed; ssl_ciphers verbatim injection; artifacts-nginx.conf.j2; DEFAULT:@SECLEVEL=0; tlsVersionsAscending; legacy iBMC InsertMedia TLS failure | [artifact-server-tls-render.md](artifact-server-tls-render.md) |
| Apply / drift | REGISTRY_AUTH_HTPASSWD_PATH panic; crash-loop under restart_policy; net.ipv4.ip_nonlocal_bind; squid refuses to run as root UID 1000; Address not found; Cannot assign requested address; Cannot find device; dns enable='no'; bind-dynamic grabs :53; dhcp-option=option:ntp-server IPv4 only | [infra-component-service-gotchas.md](infra-component-service-gotchas.md) |
| Apply / drift | too many colons; Port could not be cast to integer value; bracketRedfishHost; net.JoinHostPort; TestManagedProxyURLBracketsIPv6; urlencode leaves / unescaped; replace('/', '%2F'); userinfo percent-encoding | [url-authority-gotchas.md](url-authority-gotchas.md) |

## Constraints & semantics by area

### Apply, destroy & drift engine

| Topic | File |
| --- | --- |
| Planner gotchas: prior-phase capability injection silently dropping ordering deps, conditional ISO dependencies, virtctl provisioning/preflight split, phantom install records on skip, provisioning-playbook guards, ordering-vs-hard deps, and where apply-result state belongs. | [apply-task-graph-planning.md](apply-task-graph-planning.md) |
| Substrate destroy refuse-foreign/warn gates stay per-resource: a shared foreign_gate helper was evaluated and rejected (non-uniform control flow, resource-specific fail_msg remediation, audited test pins). | [substrate-destroy-gate-no-shared-helper.md](substrate-destroy-gate-no-shared-helper.md) |
| How the container-cluster install-record gate refuses silent adoption, why destroy must remove controller-side install state, how apply --converge-drifted skips healthy Available=True clusters but rebuilds on probe error, and the structural-hash migration-safety rules. | [cluster-install-record-gates.md](cluster-install-record-gates.md) |
| Plan tasks must hash a projection of the desired-state inputs they depend on (never the scope-filtered State), and base-only runs omit the agent-ISO dependency so they reuse a prior deps run's ISO instead of blocking. | [cluster-scoped-plan-stability.md](cluster-scoped-plan-stability.md) |
| Hashing invariants for convergence records: task-identity keying, scope-independent hash inputs, host-scoped fabric projection and frozen marshal shape, structural-hash exclusions, the retired storageAttachmentApply allowlist member, lenient-vs-strict record loading, and Ceph rebuild-authorization vars. | [converge-hash-drift-model.md](converge-hash-drift-model.md) |
| Every substrate ownership-refusal assert must carry the not(bootwright_destroy_force_unowned) escape so --include-unowned uniformly tears down renamed/unmarked resources, while guards stay presence-gated so a missing resource is a clean no-op. | [destroy-force-unowned-uniformity.md](destroy-force-unowned-uniformity.md) |
| Destroy gate extra-var vocabulary and its single composition point, the storage work-set tri-state, root-gated record cleanup, the three-kind orphan-sweep allowlist, partial-destroy bookkeeping ordering, and shared-service release/block planning. | [destroy-scoping-and-sweeps.md](destroy-scoping-and-sweeps.md) |
| infraComponents management accepts only external\|managed; `none` is a name sentinel, the future `reference` management value is unaccepted (do not author), and owner/reference are ownership-record roles — a separate concept. | [infra-component-management-vs-ownership-role.md](infra-component-management-vs-ownership-role.md) |
| Invariants of the per-context ownership store: hand-synced Go/Ansible literals, owner/reference roles and filenames, skip-not-fail load policy, path allowlists, sensitive-scan asymmetry, kind taxonomy, back-compat readers, package-removal gating. | [ownership-records-store.md](ownership-records-store.md) |
| How the O_EXCL run lease, process-identity token, heartbeat liveness, and takeover no-op semantics admit exactly one mutating run and self-heal after crashes without double-mutating. | [run-lease-mutual-exclusion.md](run-lease-mutual-exclusion.md) |
| How Satellite CA trust and registration are wired: day-2 katello bootstrap before subscription-manager register, kickstart anchors the CA in both %pre and %post, and Insights needs an explicit insights-client --register. | [satellite-registration-trust.md](satellite-registration-trust.md) |
| Quote(args) is display-oriented (denylist); QuoteWord/QuoteWords use a conservative allowlist and must be used for anything embedded in a generated command line that executes. | [shellquote-quote-vs-quoteword.md](shellquote-quote-vs-quoteword.md) |
| Why the trust store fails closed on divergent keys per address, why host-key capture uses ssh accept-new instead of ssh-keyscan under FIPS, and how ref-vs-managed known_hosts resolution and scope narrowing are centralized. | [ssh-trust-store-invariants.md](ssh-trust-store-invariants.md) |
| Per-substrate live ownership verification on apply and destroy (libvirt XML marker, vSphere annotation + moid addressing, KubeVirt labels), rename-safe identity recording, shared DVD-cache exclusion from per-machine records, per-machine managed-OS destroy dispatch, and podman-label ownership for bastion containers. | [substrate-ownership-markers.md](substrate-ownership-markers.md) |
| The two-entry validate_certs:false allowlist policy and the SSL_CERT_FILE pattern for verifying downloads against a cluster's ingress CA instead of skipping TLS. | [tls-verification-allowlist.md](tls-verification-allowlist.md) |
| How the vCenter preflight session probe, session cleanup, 15s probe timeout, and the vCenter manual MAC-range validation work and why. | [vsphere-vcenter-integration.md](vsphere-vcenter-integration.md) |

### Ceph storage

| Topic | File |
| --- | --- |
| The cephadm bootstrap one-shot contract: what seeds into bootstrap ceph.conf, the unconditional/conditional bootstrap flags, the ceph.conf idempotence gate, dashboard-password capture, and service-spec key placement rules. | [ceph-cephadm-bootstrap-contract.md](ceph-cephadm-bootstrap-contract.md) |
| The facet model behind bootwright diff --live/--adopt for Ceph: which live objects are filtered as internal, the OSD-device comparison rules and reconstruction advisories, absent-read semantics, and the additive-only adopt policy with its refusals. | [ceph-diff-adopt-live-semantics.md](ceph-diff-adopt-live-semantics.md) |
| Distribution-table-driven Ceph packaging and its traps: release/RHEL compatibility catalogs, image-base and mirror coherence, IBM license/Call Home behavior, mgr registry-store credential rotation, the FIPS OpenSSL conflict from host community ceph-common on RHEL, community EPEL bootstrap, and RHSM idempotence. | [ceph-distribution-packaging.md](ceph-distribution-packaging.md) |
| Why an OSD readiness or mon-location poll never satisfies against a healthy cluster: cephadm hosts are FQDNs but CRUSH host buckets and mon/crash daemon names are the short hostname, so orchestrator names must not be compared with `ceph osd tree` / `ceph mon dump` output. | [ceph-host-identity-namespaces.md](ceph-host-identity-namespaces.md) |
| cephadm monitoring-spec constraints: retention keys are Prometheus-only, no dashboard Loki wiring command exists, role-less services render only when authored, and the mgmt-gateway/keepalive_only ingress spec shape. | [ceph-monitoring-service-specs.md](ceph-monitoring-service-specs.md) |
| The OSD device ownership marker and the install/destroy/removal device gates, the reclaim path after a managed-OS reinstall, the root-disk validators, the post-apply OSD readiness poll that prevents zero-or-partial-OSD green applies, and exhausted Ansible retries skipping diagnostics. | [ceph-osd-device-safety.md](ceph-osd-device-safety.md) |
| What counts as a Ceph sub-object's immutable structural identity, the fail-safe live probes that gate the data-destroying --converge-drifted rebuild, EC pool creation traps, and the structural-hash invariants for reconcilable edits. | [ceph-override-structural-rebuild.md](ceph-override-structural-rebuild.md) |
| How Bootwright proves a cephadm cluster is its own before any destructive action: the 3-factor ownership gate, on-disk-fsid semantics, per-host rm-cluster, pre-bootstrap ownership recording, and the fail-safe rebuild-authorization token. | [ceph-ownership-apply-destroy-gates.md](ceph-ownership-apply-destroy-gates.md) |
| RGW realm/zonegroup/zone creation and period-commit ordering ahead of the rgw service, admin-user output redaction, NFS export idempotency keyed <serviceID>\|<pseudo>, and the shell-quoting trap that key creates in the generated apply script. | [ceph-rgw-nfs-service-ordering.md](ceph-rgw-nfs-service-ordering.md) |
| Why the stretch tiebreaker must not get a CRUSH host-spec location, how the two-replicas-per-site rule is compiled into the CRUSH map, and the ordering/naming constraints around enable_stretch_mode. | [ceph-stretch-mode-constraints.md](ceph-stretch-mode-constraints.md) |

### Bare-metal & managed-OS install

| Topic | File |
| --- | --- |
| Jinja/kickstart interactions: trim_blocks newline loss requires precomputed flag expressions, credential '/' must be %2F-encoded, proxy secrets are shape-asserted via undef(), and proxy bypass is decided per kickstart directive at render time. | [anaconda-kickstart-render-traps.md](anaconda-kickstart-render-traps.md) |
| The guards that keep a first bare-metal apply from wiping the wrong host: confirm-time first-install warning, fail-open Redfish occupancy probe, normalized unique-BMC-address validation, authenticated BMC reachability preflight, and the record-keeping-only bare-metal substrate role. | [baremetal-first-install-safety.md](baremetal-first-install-safety.md) |
| The libvirt media insert helper must register the bootwright XML namespace prefix to keep the ownership marker intact across virsh define, stages itself at a content-hashed path to avoid concurrent-version races, and the eject script's rc-capture and source/all mode contract. | [libvirt-direct-media-helper.md](libvirt-direct-media-helper.md) |
| The probe_existing/wait/marker predicate ladder: port-open detection, fail-closed unverifiable hosts, ownership vs hash-match separation, the force_rebuild/install_required fact chain that makes --converge-drifted actually reinstall, install_required-gated post-boot waits, ownership-gated stamp gating against foreign-host laundering, bare-metal media eject via cleanupMediaRole, the infra-stage destroy-protection remedy routing, and the latent customizations.storage.wipe dead field. | [managed-os-install-gates.md](managed-os-install-gates.md) |
| Shared-source staging identity and throttles for controller-collapsed installs, hosted-install-tree identity/atomic-publish rules, why rd.live.check is stripped from mkksiso builds, and publish-root SELinux label alignment (chcon --reference, never restorecon). | [managed-os-media-staging.md](managed-os-media-staging.md) |
| What renders into the minimal kickstart (one merged bond+VLAN stanza) vs the post-install desiredState (all VLANs, MTU, cluster IPs), why bond members carry mac-address identity while bond/VLAN layers must not, and the controller-local inventory shape bare-metal managed-OS nodes need. | [managed-os-post-install-network.md](managed-os-post-install-network.md) |
| Trust-store method discovery/dispatch, iBMC RootCertId slot rules, openssl s_client leaf retrieval, no_log-safe refusal surfacing, capture-once VerifyCertificate, and always-block cleanup/restore semantics for BMC virtual-media cert trust. | [redfish-bmc-cert-import-mechanics.md](redfish-bmc-cert-import-mechanics.md) |
| Role-wide uri module_defaults, tail-recursive retry/power-wait pattern (never `until` under no_log), sushy cold-init lock throttle, dual VirtualMedia view dedup, multi-system pinning via /Systems/<id> suffix, and emulated-BMC service provisioning gotchas. | [redfish-boot-flow-quirks.md](redfish-boot-flow-quirks.md) |

### OpenShift / OKD clusters

| Topic | File |
| --- | --- |
| The mirror registry host prefers a declared endpoint or routable bind address over the machine's SSH alias, and the installer manifest render must recognize both secret-placeholder dialects so redacted cert/key material lands verbatim in stringData instead of being base64-corrupted. | [openshift-installer-render-resolution.md](openshift-installer-render-resolution.md) |
| vSphere boot always full power-cycles, probes readiness over SSH (agent ISOs have no VMware Tools), omits integrated-LB VIPs when endpoints are externally load-balanced, masks manual MACs into vCenter's 00:50:56 range, and fabricates substrate NICs only for physical interfaces. | [openshift-vsphere-agent-boot.md](openshift-vsphere-agent-boot.md) |
| Invariants for the nested-cluster child network add-on: L2-only CUDN with static IPs, bridge-mapping name equality, VLAN plumbing, and the OVN-derived NAD the VMs consume. | [ovn-child-network-wiring.md](ovn-child-network-wiring.md) |

### Networking, proxy & TLS

| Topic | File |
| --- | --- |
| The machine_proxy role's persistence rules: env-only dnf proxy, strip-only config edits, no containers.conf drop-in, TCP reachability probe, and active clearing of stale proxy state. | [machine-proxy-persistence.md](machine-proxy-persistence.md) |
| Fallback chain that resolves the cluster-reachable host for managed proxy and mirror-registry URLs, including gateway substitution for loopback bastions. | [managed-proxy-url-resolution.md](managed-proxy-url-resolution.md) |
| The rollback-guarded post-install nmstate apply design, the interfaceAddresses family-default gotcha, and why only physical NMState interfaces get substrate NICs/MACs. | [nmstate-network-convergence.md](nmstate-network-convergence.md) |
| The Bypasses matching contract, why CIDR noProxy entries are expanded into pinned literals over noProxyTargets, and the CIDR-stripped literal variant for CIDR-blind matchers. | [no-proxy-cidr-matching.md](no-proxy-cidr-matching.md) |

### Secrets & entitlements

| Topic | File |
| --- | --- |
| entitlements.Resolve keeps secrets behind names and derives provider/product from spec.type to preserve the pre-first-class render contract; rhsm resolves from the entitlement's own arm (ibm-storage-ceph carries none; its nodes register via a separate redhat-rhel subscription named by the profile subscription or cluster osSubscriptionRef); Satellite projects into vars with a stable CA path. | [entitlement-resolution-vars.md](entitlement-resolution-vars.md) |
| How plaintext secret copies on disk are kept short-lived: post-lease sweep reclaims SIGKILL orphans, store materialization is all-or-nothing, MkdirAll needs explicit Chmod, and preflight rejects only over-broad modes. | [plaintext-secret-lifecycle.md](plaintext-secret-lifecycle.md) |
| The NUL-wrapped placeholder sentinel switches resolution to {{ secret }} tokens (render-only, short-circuits before source paths leak); portable bundles are leak-scanned; manifest Secrets must handle both placeholder dialects in stringData. | [portable-secret-placeholders.md](portable-secret-placeholders.md) |
| secret.Index is the single seam from Secret declarations to per-role paths; materialize verifies-or-renews generated material via the same request derivation preflight uses; SEC1 note for ECDSA SSH keys. | [secret-index-resolution.md](secret-index-resolution.md) |
| clusteraccess summaries report paths/presence only with RevealSecretFile gated behind --secrets; the global pull-secret merge is conflict-safe via resourceVersion, idempotent on existing credentials, and console-silent. | [secret-output-discipline.md](secret-output-discipline.md) |

### Add-ons & OLM

| Topic | File |
| --- | --- |
| How the add-on engine's already-ready short-circuit, OLM catalog/CSV gates, typed gate-timeout errors, quiet poll runner, and stage-scoped preflight gates behave and why. | [addon-apply-engine-gates.md](addon-apply-engine-gates.md) |
| provides/requires capabilities are an open token vocabulary with three reserved planner names (kubevirt, dataFoundation, nmstate); ordering is a stable per-binding Kahn topo sort where unsatisfied requirements impose no edge. | [addon-capability-ordering.md](addon-capability-ordering.md) |
| render.DesiredHash folds the canonical OLM resource order, binding-supplied inputs (omitempty, duplicate-merged to mirror the executor), and the per-hook shipped-content digest — with the parity invariants that keep hash and execution aligned. | [addon-desired-hash-inputs.md](addon-desired-hash-inputs.md) |
| Hook execution internals: content+inputs+target digest for onChange, ad-hoc SSH inventory with scoped secrets, fixed output persistence paths, firstReachable run-until-success, exportDetails resolution, and plan-time cross-cluster DAG edges replacing the compiled DF attachment. | [addon-hook-runtime-internals.md](addon-hook-runtime-internals.md) |
| Machine-local add-on store internals: extensionless provenance marker, whole-directory rewrite on install with path-escape rejection, version-as-assertion delete, SourcePath-based store detection, and the context-init snapshot that keeps contexts self-contained. | [addon-native-store-internals.md](addon-native-store-internals.md) |
| Observed-state add-on records never hold secret bytes (non-secret failure summaries, raw oc output only in the error/apply log), SaveRecord must preserve on-disk Hooks written out-of-band by SetHook, and hook secret materialization is name-scoped. | [addon-records-secret-hygiene.md](addon-records-secret-hygiene.md) |
| Hook manifest tokens must be a whole YAML scalar {{ kind arg }}; interpolation is rejected so re-marshaled payloads cannot be corrupted or injected. | [hook-manifest-token-grammar.md](hook-manifest-token-grammar.md) |

### CLI behavior

| Topic | File |
| --- | --- |
| Container-cluster rsh/exec uses nodeSSH + core + effective install IP while storage stays Machine-owned; encrypted private keys cross the SSH boundary through parent-held anonymous descriptors and child exit codes are preserved. | [cluster-node-ssh-access.md](cluster-node-ssh-access.md) |
| Failure-presentation rules: per-cluster log pointers with installer paths only on unclean finishes, blocked tasks attributed to the first FAILED ancestor (naming the host cluster), and middle-ellipsis truncation that preserves the trailing remedy clause. | [cli-apply-failure-reporting.md](cli-apply-failure-reporting.md) |
| Single-source rules for shared flag help and the --stage/--through vocabulary, the --converge-drifted help contract, cobra dispatcher wiring gotchas, the -- flag-terminator rule for JSON error output, and the unbuffered confirm() stdin gotcha. | [cli-flag-and-parsing-conventions.md](cli-flag-and-parsing-conventions.md) |
| Operational mechanics of the sudo re-exec: --context stripping for classification, workspace-path resolution before escalation, the internal password-handoff env vars, signal/Ctrl-C ownership, and the never-escalate/never-prompt rules for completion and preflight. | [cli-root-gate-sudo-reexec.md](cli-root-gate-sudo-reexec.md) |
| How apply/destroy ledgers project into display frames: the single ledger-to-frame mapper, the leading infra group, topology-aware cluster ordering, the single-group destroy frame, the two storage-cluster phases, and the internal/status vs internal/cli and mutating_run.go ownership boundaries. | [cli-run-frames-and-grouping.md](cli-run-frames-and-grouping.md) |
| The status next-step spine: status as the single hint hub, identity-vs-health rendering, when diff joins the spine, exhaustive secret-set hints, the machine-trust hint, ledger-target-to-retry-command mapping, and access advertised only for completed installs. | [cli-status-and-next-step-hints.md](cli-status-and-next-step-hints.md) |
| The output package contract: one color style gate, RunView redraw vs append-only modes, RenderFrame collapse and the single-frame-writer allowlist, redraw height accounting, the status --watch preamble, and the quiet oc ReadRunner that keeps expected read noise off the console. | [cli-terminal-rendering-contract.md](cli-terminal-rendering-contract.md) |
| Where diagnostic presentation lives (CLI, not validator), the structured Finding carrier, the append-only decode did-you-mean rule, defaulted-secret-ref notes, and the rule that scaffolds must not imply apply support. | [cli-validation-diagnostics.md](cli-validation-diagnostics.md) |

### Rendering & templates

| Topic | File |
| --- | --- |
| Spellings that appear in rendered vars/install-config/cephadm input but are not authorable: image.mediaType, the machines/bmc service kinds, the historical HTTP naming of the 8443 HTTPS port, verbatim upstream keys, and the (C)UDN multus wiring rule. | [render-internal-vocabularies.md](render-internal-vocabularies.md) |
| Inventory role groups (ADR-0002), controller-driven-on-localhost, substrate-blind boot_redfish, emulated-BMC port projection, vSphere path/MAC/ISO reduction, and cleanupMediaRole dispatch. | [render-inventory-and-boot-vars.md](render-inventory-and-boot-vars.md) |
| Two keep-in-sync hazards in render: the duplicate-BMC key in state/desired hand-mirrors the renderer's Redfish URL normalization, and componentImages overrides must bypass managedServiceImage to avoid infinite recursion while still surfacing in the BOM. | [render-normalization-mirrors.md](render-normalization-mirrors.md) |
| renderCore's shared body and four mode seams, effective-state identity, the fail-closed unresolved-name check, and the omit-empty-keys byte-stability rule. | [render-pipeline-modes.md](render-pipeline-modes.md) |
| StorageRenderState nils ContainerClusters and does not follow the data-foundation edge; top-level render --clusters accepts StorageCluster names. | [render-storage-scope.md](render-storage-scope.md) |
| Render-reference vs work-set semantics for --clusters scopes: nil-vs-empty StorageClusterNames, preflight secret scope, connected-machine host trust, diff --recorded parity, full-state orphan detection, whole-input validation, service-machine retention. | [scoped-runs-render-vs-work-set.md](scoped-runs-render-vs-work-set.md) |

### API schema & grammar

| Topic | File |
| --- | --- |
| Never-authored computed fields (DefaultedRefs, binding MachineName) that let validation blame normalize-injected references honestly, plus the StorageCephRoles/StorageCephRHELVersions single-source accessors that keep advertised and accepted sets from drifting. | [api-normalize-bookkeeping.md](api-normalize-bookkeeping.md) |
| Lookup table of every *Ref field's resolution namespace that lived only in deleted field comments, flagging the entries specs/state-model.md still lacks. | [api-ref-resolution-namespaces.md](api-ref-resolution-namespaces.md) |

### Repo tooling, build & CI

| Topic | File |
| --- | --- |
| Maps the state-engine audit-finding IDs (M2 lease takeover, M8 scope-dependent hash, M11 diff --recorded --clusters parity) to the invariants they encode and the regression tests that pin them. | [audit-finding-regression-tests.md](audit-finding-regression-tests.md) |
| Containerfile layer ordering (toolchain COPY, requirements-keyed galaxy layer, second sync-bundle, compile-only relink, .git last) and the VERSION/ldflags stamping chain plus the CGO-off shipped-binary constraint. | [container-build-and-stamping.md](container-build-and-stamping.md) |
| How to regenerate render goldens and Ansible-UUID pin values, and why goldens stay byte-stable (pinned HOME, fictitious secrets dir, static lookupDate freshness stamps). | [golden-fixture-regeneration.md](golden-fixture-regeneration.md) |
| Why the make check targets run in their order (sync-bundle first, cheapest-to-slowest), per-target rationale (race timeout, tidy trap, shebang shellcheck, workflow yamllint, repo-local Ansible temp) and the CI trigger gates on tags, PRs, and docs publishes. | [make-check-and-ci-gates.md](make-check-and-ci-gates.md) |
| The intent behind the converge facade, workflow purity rules, state package layering (view/graph/advice), inventory-installer edge usage, and the frozen test-only compat aliases in internal/cli. | [package-boundary-contracts.md](package-boundary-contracts.md) |
| Checklists for coordinated edits guarded by coverage tests: new kinds, InfraComponent union arms, installer-owned fields, provisioning stages, machine-service var builders and pin gates, native add-on catalog embeds, scaffold substrates, and the credentials filter contracts. | [registry-coverage-guards.md](registry-coverage-guards.md) |
| Catalog of the repository fitness tests (import matrix, process-exec seam, role-name single source, human-output guard, file-size floors, stale-term scope, diagnostics vocabulary, docs YAML strict decode, get_url checksum policy) and what each forbids. | [repo-fitness-guardrails.md](repo-fitness-guardrails.md) |
| Gitignored work-in-progress fixtures live under examples-wip/ (a parameterized template plus render.sh → rendered/, a sibling of examples/ so example-scoped checks skip them); this file is the only in-repo record they exist. | [examples-wip-fixtures.md](examples-wip-fixtures.md) |

### Ansible runtime quirks

| Topic | File |
| --- | --- |
| Negative pin: Bash length expansion ${#...} is forbidden in eject_libvirt_media.sh (and Ansible-templated shell) because {# opens a Jinja comment; guard test enforces, count with a loop variable instead. | [ansible-bash-hash-brace-jinja.md](ansible-bash-hash-brace-jinja.md) |
| Task-playbook preambles cannot be factored (play keywords), the four deliberate preamble variants (bastion env lookup, reachability no-become, managed-OS any_errors_fatal, storage linear), and why component-selection and kubevirt verb blocks stay per-playbook/per-role. | [ansible-playbook-preamble-variants.md](ansible-playbook-preamble-variants.md) |
| The embedded bundle pipeline: exact-pinned collections, tests/docs slimming before pack, two-layer lock verification (install-time script + post-pack Go test), and byte-deterministic zip with fail-closed symlink policy. | [ansible-bundle-embed-and-lock.md](ansible-bundle-embed-and-lock.md) |
| The repo-enforced no_log template forms, the agent-ISO publish token as a secret, Redfish module_defaults credential wiring, the tolerate-then-assert pattern for BMC errors, the shared credential loader contract, and render-time kickstart proxy credential handling. | [ansible-credential-redaction.md](ansible-credential-redaction.md) |
| check_external_reachability probes only what Bootwright itself drives (external Redfish BMCs, external proxies via TCP-only https-first), skips components the same apply will create, and deliberately leaves operator DNS/LB to later controller-vantage gates. | [ansible-infra-reachability-preflight.md](ansible-infra-reachability-preflight.md) |
| Rationale for every .ansible-lint skip and .yamllint relaxation, plus how make ansible-lint-check invokes both tools; do not remove a skip without addressing its reason. | [ansible-lint-yamllint-config.md](ansible-lint-yamllint-config.md) |
| Four Ansible result-handling semantics: skipped set_fact keeps old values, branch-only registers persist across loops, skipped tasks overwrite registers, and failed_when:false forces decisions onto success-only payload keys. | [ansible-register-skip-persistence.md](ansible-register-skip-persistence.md) |
| Play mechanics that let a destructive teardown classify unreachable hosts and fail closed with guidance: defer fact gathering, probe with task-level ignore_unreachable, gate with controller-side asserts under any_errors_fatal. | [ansible-teardown-unreachable-hosts.md](ansible-teardown-unreachable-hosts.md) |
| Agent teardown is entirely controller-side, media cleanup dispatches on the rendered cleanupMediaRole registry, FIPS clusters build their ISO with openshift-install-fips, and external endpoint DNS diagnostics are report-only with no end_play under strategy:free. | [container-cluster-ansible-flow.md](container-cluster-ansible-flow.md) |
| from_json on a tolerated command's empty stdout throws (an rc!=0 task with stdout='' is DEFINED, not skipped); guard with default('{}', true) before from_json, or an eager fail_msg reference throws first. | [ansible-from-json-empty-stdout.md](ansible-from-json-empty-stdout.md) |
| Deployed Jinja has no list/dict comprehension or multi-for grammar; build derived collections with filter chains (map/select/reject) and accumulator set_facts instead. | [ansible-jinja-no-comprehensions.md](ansible-jinja-no-comprehensions.md) |

### Workspace & context

| Topic | File |
| --- | --- |
| context init copies input; update snapshots to history via the single input-mutation component; crash-safe replace; paths resolve before sudo re-exec; missing input is a named error. | [context-input-ownership.md](context-input-ownership.md) |
| Root-owned workspace artifacts need CommandOutputLocalRoot; operator files need caller-owned stat; EnsureDir re-chmods; copyInputTree is symlink-safe. | [workspace-root-owned-fs-access.md](workspace-root-owned-fs-access.md) |

## Backlog

Deferred and open engineering work is cataloged in [BACKLOG.md](BACKLOG.md), not in this index. Before proposing new work in an area, scan it for an existing entry; before deferring a review finding, file one there. Each entry is deleted in the same commit that lands its fix — the subject references the entry ID — or is converted into a rejected-note in the owning knowledge file when the work is declined. Keeping it the single home for intentionally not-yet-done work lets this index stay a record of durable truths rather than a to-do list.
