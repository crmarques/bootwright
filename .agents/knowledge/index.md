# Knowledge Index

Knowledge records observed implementation facts and diagnosed failures, not
product requirements. Specs win when the two differ. Search this table before
investigating or designing, then open only the relevant page.

| Area or symptom | Page |
| --- | --- |
| Desired-state API, `v1alpha1`, 26-kind service update, controller Machine and proxy, historical 27-to-21-kind alignment | [api-alignment.md](api-alignment.md) |
| Admission parser composition, YAML directives/tags, exact integers, effective inspection, offline vulnerability database | [desired-state-admission.md](desired-state-admission.md) |
| CLI, Cobra, completion effects, `BASH_COMP_DEBUG_FILE`, `ExecuteContext`, local `--output`, nil/empty cluster selection, Zsh `compadd`, PowerShell `-File` | [cli-adapter-constraints.md](cli-adapter-constraints.md) |
| Go toolchain pin, `GOTOOLCHAIN`, `govulncheck`, release version metadata, `-ldflags -X`, Go 1.26 crypto Reader injection | [build-toolchain.md](build-toolchain.md) |
| OpenShift agent installer, bare-metal disk identity, pre-wipe proof, Redfish-to-installer race | [openshift-agent-disk-safety.md](openshift-agent-disk-safety.md) |
| Controller private Python/Ansible runtime, isolated interpreter, ELF loader invocation, preload list, glibc/libgcc foundation, native package read lock | [controller-runtime-isolation.md](controller-runtime-isolation.md) |
| Managed artifact server, `ubi9/nginx-124`, Quadlet unit, `getgrnam` failure, container user and ports, empty-root 404, certificate fingerprint probe | [artifact-server-nginx-runtime.md](artifact-server-nginx-runtime.md) |
| Managed proxy, name resolver and time service, `squid`/`dnsmasq`/`chronyd` entrypoints, `chronyd -x`, `local stratum 10` versus `orphan`, `bind-interfaces`, squid 400 as readiness | [managed-network-service-runtime.md](managed-network-service-runtime.md) |
| Emulated BMC, `sushy-tools`, `redfish-emulator.sh`, Flask development server and Werkzeug debugger PIN, `--interface ::` default, `SUSHY_TOOLS_CONFIG`, libvirt socket URI | [sushy-tools-emulated-bmc.md](sushy-tools-emulated-bmc.md) |
| Progress row redrawn in place, repeated `[RUNNING]` lines cut at the terminal width, `\x1b[2K` on a wrapped row, `TIOCGWINSZ`, elided subject | [terminal-progress-redraw.md](terminal-progress-redraw.md) |
| Retained adapter output empty while a run is in flight, `tail -F` printing nothing, Ansible `display.py` not flushing, CPython pipe buffering, `-u` against `-I`/`-E`, `PYTHONUNBUFFERED` ignored | [adapter-output-live-retention.md](adapter-output-live-retention.md) |
| Sudo `use_pty`, confirmation hangs after `y`, elevated child in a background process group, `SIGTTIN`/`SIGTTOU` handoff, terminal streams through the supervisor | [sudo-pty-terminal-handoff.md](sudo-pty-terminal-handoff.md) |
| Controller setup slow on a ready host, `unchanged` after minutes, resolution before readiness, Python archive and wheel acquisition during resolve, DNF repository staging, retained resolution reuse, sealed bundle presence check, `rpm --verify` per plan package, native root presence by name, full reinstall after an automation edit, automation digest in the bundle identity | [controller-setup-resolution-cost.md](controller-setup-resolution-cost.md) |

Add one focused page for each durable lesson and index it with searchable topic
terms, affected symbols, or error text. Each new or revised entry identifies the
observation, the decision it informs, its applicable implementation or version,
and links to the affected code and supporting tests or other evidence. Link to
the owning spec for required behavior rather than restating it. Keep paths and
evidence current; correct or retire obsolete findings when their basis changes.

Use the [code clarity contract](../../specs/architecture.md#self-explanatory-code-and-retained-knowledge)
when moving useful explanations out of comments. Redundant narration needs no
catalog entry. Put unfinished implementation work under the earliest fitting
future milestone in [milestones](../../specs/milestones.md).
