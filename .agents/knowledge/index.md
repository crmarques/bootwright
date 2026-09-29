# Knowledge Index

Knowledge records observed implementation facts and diagnosed failures, not
product requirements. Specs win when the two differ. Search this table before
investigating or designing, then open only the relevant page.

| Area or symptom | Page |
| --- | --- |
| Desired-state API, `v1alpha1`, 26-kind service update, controller Machine and proxy, historical 27-to-21-kind alignment | [api-alignment.md](api-alignment.md) |
| Admission parser composition, YAML directives/tags, exact integers, effective inspection, offline vulnerability database | [desired-state-admission.md](desired-state-admission.md) |
| CLI, Cobra, completion effects, `BASH_COMP_DEBUG_FILE`, `ExecuteContext`, local `--output`, nil/empty cluster selection, Zsh `compadd`, PowerShell `-File` | [cli-adapter-constraints.md](cli-adapter-constraints.md) |
| ansible-core `until` retries templated once, task `timeout` leaves the module running, GNU `timeout --kill-after` rc `-9` | [ansible-core-retries-and-timeouts.md](ansible-core-retries-and-timeouts.md) |
| Go toolchain pin, `GOTOOLCHAIN`, `govulncheck`, release version metadata, `-ldflags -X`, Go 1.26 crypto Reader injection | [build-toolchain.md](build-toolchain.md) |
| OpenShift agent installer, bare-metal disk identity, pre-wipe proof, Redfish-to-installer race | [openshift-agent-disk-safety.md](openshift-agent-disk-safety.md) |
| `to_nice_yaml` leaving `1e3` plain, `sigs.k8s.io/yaml` `UnmarshalStrict`, a string read as a number, `1e3` → `1000`, `0987654321` → `9.8765434e+08`, `y`/`n` → `true`/`false`, agent-config written as JSON, `found invalid Unicode character escape code`, a surrogate-pair escape, `control characters are not allowed`, `to_installer_yaml` | [installer-input-yaml-scalars.md](installer-input-yaml-scalars.md) |
| `openshift-install agent create image` work area, no `metadata.json`, cluster identity, admin client certificate, `.openshift_install_state.json`, install-complete kubeconfig rewrite, `addRouterCAToClusterCA`, router CA, `clientcmd.WriteToFile` `apiVersion` and `kind`, kubeconfig growth per rewrite, 64 KiB read bound | [openshift-agent-work-area.md](openshift-agent-work-area.md) |
| Ansible worker `setsid`, `WORKER_SESSION_ISOLATION`, a process-group kill missing workers, subreaper descendant walk, `PR_SET_PDEATHSIG` not reaching forked workers | [ansible-worker-session-isolation.md](ansible-worker-session-isolation.md) |
| Controller private Python/Ansible runtime, isolated interpreter, ELF loader invocation, preload list, glibc/libgcc foundation, native package read lock | [controller-runtime-isolation.md](controller-runtime-isolation.md) |
| Managed artifact server, `ubi9/nginx-124`, Quadlet unit, `getgrnam` failure, container user and ports, empty-root 404, certificate fingerprint probe, served-file mode and worker group, 403, fetch-through probe, `ca_path` | [artifact-server-nginx-runtime.md](artifact-server-nginx-runtime.md) |
| Managed proxy, name resolver and time service, `squid`/`dnsmasq`/`chronyd` entrypoints, `chronyd -x`, `local stratum 10` versus `orphan`, `bind-interfaces`, squid 400 as readiness | [managed-network-service-runtime.md](managed-network-service-runtime.md) |
| Emulated BMC, `sushy-tools`, `redfish-emulator.sh`, Flask development server and Werkzeug debugger PIN, `--interface ::` default, `SUSHY_TOOLS_CONFIG`, libvirt socket URI, `BootSourceOverrideEnabled` always `Continuous`, `ForceOn` as `On`, no etag or `SerialNumber`, synchronous insert | [sushy-tools-emulated-bmc.md](sushy-tools-emulated-bmc.md) |
| Physical Redfish BMC, iBMC/iDRAC/OpenBMC divergence, manager against system VirtualMedia, undeclared VirtualMedia fallback, `TaskMonitor` asynchronous insert, `TaskState` `Completed` with a `Warning`, `If-Match` 412, `ETag` header case, boot selection read back, `VerifyCertificate` 501 against 403, a reference to another host refused, `ControllerError`, `PowerState=On` before POST, a boot override clobbering `Once`/`Cd`, `EthernetInterfaces` target proof, ambient proxy on `uri`, `no_log` censoring a refusal | [redfish-physical-bmc.md](redfish-physical-bmc.md) |
| Blocks of one operation running together, wave-major plan order, the admission precedence and its `worked` set, a bound that is not plan intent, exclusive resources over a shared package tree, races the sequential executor hid | [concurrent-block-execution.md](concurrent-block-execution.md) |
| Progress row redrawn in place, repeated `[RUNNING]` lines cut at the terminal width, `\x1b[2K` on a wrapped row, `TIOCGWINSZ`, elided subject, one line per change of step label, a check reported as an effect printing every step twice | [terminal-progress-redraw.md](terminal-progress-redraw.md) |
| Retained adapter output empty while a run is in flight, `tail -F` printing nothing, Ansible `display.py` not flushing, CPython pipe buffering, `-u` against `-I`/`-E`, `PYTHONUNBUFFERED` ignored | [adapter-output-live-retention.md](adapter-output-live-retention.md) |
| Sudo `use_pty`, confirmation hangs after `y`, elevated child in a background process group, `SIGTTIN`/`SIGTTOU` handoff, terminal streams through the supervisor | [sudo-pty-terminal-handoff.md](sudo-pty-terminal-handoff.md) |
| Giving an elevated child an environment value, `NAME=value` only before `--`, sudoers `setenv`/`SETENV`/`ALL`, why `env_keep` cannot help | [sudo-command-line-environment.md](sudo-command-line-environment.md) |
| `machine rsh`/`exec` session, `/proc/self/fd` material, `ssh -F` skipping both configs, crypto-policy copy under FIPS, `HostKeyAlgorithms` for an `ssh-rsa` host, `accept-new` observation instead of `ssh-keyscan`, missing `TERM` in the elevated child, `state/` entry bound | [ssh-session-boundary.md](ssh-session-boundary.md) |
| Controller setup slow on a ready host, `unchanged` after minutes, resolution before readiness, Python archive and wheel acquisition during resolve, DNF repository staging, retained resolution reuse, sealed bundle presence check, `rpm --verify` per plan package, native root presence by name, `native postcondition` refusal, `rootsReady` false with nothing installed, a `%ghost` runtime directory a daemon owns, `--noghost`, integrity over the transaction's own action targets, a completion gate only the first apply ever reaches, a first apply failing where the next one resolves, full reinstall after an automation edit, automation digest in the bundle identity | [controller-setup-resolution-cost.md](controller-setup-resolution-cost.md) |
| `prove the installation comple...` running for minutes, a progress row that names a check while it waits, `identity_attempts`/`identity_delay`, host key fact used as `.content`, an observation resolving an apply on weaker evidence, `reachable` in installation evidence | [installation-completion-proof.md](installation-completion-proof.md) |
| `lifecycle operation entry is not this store's own`, `not a directory` on an attempt record, a first apply failing at `controller-prerequisites`, syncing a record instead of its directory, operation subtree measured before every write, staged `pending-` name renamed over its target, entry confirmation before a refusal, a refusal that discards its cause, a permissive test double hiding a caller defect | [operation-store-entry-confirmation.md](operation-store-entry-confirmation.md) |
| `no_log` censoring a loop but not its register, `failed_when: false` rewriting `failed` to false, `failed_when_suppressed_exception`, reading `item` back from a hidden loop, one controller in a bounded reading that never answered | [looped-read-result-shape.md](looped-read-result-shape.md) |

Add one focused page for each durable lesson and index it with searchable topic
terms, affected symbols, or error text. Each new or revised entry identifies the
observation, the decision it informs, its applicable implementation or version,
and links to the affected code and supporting tests or other evidence. Link to
the owning spec for required behavior rather than restating it. Keep paths and
evidence current; correct or retire obsolete findings when their basis changes.

Use the [code clarity contract](../../specs/architecture.md#self-explanatory-code-and-retained-knowledge)
when moving useful explanations out of comments. Redundant narration needs no
catalog entry. Record unfinished implementation work as an item of the earliest
fitting milestone in [milestones](../../specs/milestones.md).
