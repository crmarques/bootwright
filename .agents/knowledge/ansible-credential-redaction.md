# Ansible credential redaction: the no_log contract and safe error surfacing

**The redaction contract (`assertRedactsByDefault`):** a sensitive task's
`no_log` must be either the literal `true` or a template that redacts when
the operator has not opted out — `{{ bootwright_no_log | default(true) | bool }}`,
or for credential-gated tasks
`{{ (bootwright_no_log | default(true) | bool) and (<original gate>) }}`.
The guarantee is redact-by-default when `bootwright_no_log` is unset, while
still allowing an explicit opt-out (e.g. `--verbose` debugging).

**The agent-ISO publish token is a secret:** `boot.agentIso.stagePath` embeds
the unguessable per-machine publish token (substituted in
`container_cluster_agent_install` `boot_machine.yml`). Any task that would
print it — the direct libvirt media insert argv ("Insert staged virtual media
directly into libvirt domain"), helper invocations, `set_fact` results — needs
`no_log`, or the eject-status style redaction
`replace(bootwright_agent_iso_publish_token, '<redacted>')`. Treat the token
as a secret everywhere it is interpolated, in both the libvirt and Redfish
media backends.

**Redfish credentials via module_defaults:** credentials/TLS settings are
supplied role-wide via `ansible.builtin.uri` `module_defaults` on the
"Boot via Redfish with shared request defaults" block (`url_username`/
`url_password` from `bootwright_redfish_credentials`, `validate_certs` from
`boot.redfish.validateCerts`, `force_basic_auth` keyed on
`bootwright_redfish_cred_path`). Individual credentialed uri probes (e.g. the
MAC-validation probes) must still gate `no_log` on
`bootwright_redfish_cred_path` so their registered output stays hidden when
credentials are in play.

**Surfacing BMC errors without leaking credentials:** letting the uri module's
`status_code` mismatch fail raises only the bare `Status code was 403 ...`
with no BMC detail, and the task's `no_log` (protecting `url_password`) buries
the response body as "output has been hidden due to no_log". Instead, tolerate
any status on the request, then assert afterwards interpolating only
status/json/content/msg and the resource path — none of which carry
`url_password` — so the BMC's refusal reason is shown while the credential
stays redacted. Used by both `import_certificate` method files in
`container_cluster_boot_redfish`.

**Shared credential loader contract:** `support_credentials/tasks/load.yml`
slurps a credential file from the CONTROLLER (secrets under
`bootwright_secrets_dir`) and parses it into a `{username, password}` fact on
the current host via the `bootwright_parse_credential` filter. Caller
contract: `bootwright_creds_path` (controller path), `bootwright_creds_var`
(fact name to set), `bootwright_creds_label` (label used in filter error
messages — the operator's only signal for which `credentialsRef` is
malformed). Callers wrap the include with their own `when:` (preconditions
differ per site), and all tasks are `no_log: true`.

**The cloned-guest seed is public by contract, so it carries no `no_log`:**
`machine_os_install_clone/tasks/seed.yml` renders the cloud-init metadata and
user-data, and `machine_substrate_vsphere/tasks/layout.yml` re-derives the same
payload into `guestinfo.*` advanced settings. Everything under `osInstall.guest`
is public material by contract — the desired-state validator refuses
`ssh.initialPassword` for a clone precisely because vCenter `extraConfig` is
plaintext to any principal that can read the VM — so none of those three tasks
sets `no_log`. `no_log` on the render is actively harmful: it replaces the whole
task result, including the user-data template's `undef(hint=...)` naming the
missing public key, with the censored notice, leaving a silent SSH-wait timeout
as the only symptom. The operator diagnostic instead comes from a plain `stat`
plus `assert` on `guest.sshPublicKeyPath` before the render.

**Kickstart proxy credentials never enter vars.yaml:** the kickstart template
builds the credentialed proxy URL for `spec.proxyFor.machineOSInstall` at
render time from the unauthenticated `proxy.url` plus the credentials file
(`proxy.credentialsPath`), so the proxy password never lands in `vars.yaml`.
The credentials file must hold a single `user:password` pair — a generated
secret always does, but a file-sourced secret is arbitrary, so the template
fails with a Jinja `undef()` naming the secret and the required shape instead
of an opaque Jinja index error deep in the machines phase when the split finds
no `:`.
