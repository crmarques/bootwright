# Firewall SSH admission on a provided host

Observed on 2026-11-23 on a provided RHEL 9.8 Machine whose firewalld was
installed but inactive and disabled. Required behavior stays in
[host firewall requirements](../../specs/managed-os.md#host-firewall-requirements).

## Symptom

`firewall.change-refused: firewalld refused a change on Machine/<name>:
ssh-admission` from the `os-firewall-prepare-<machine>` block, while the
connection's interface sat in `public`, which admits `ssh`.

## Mechanism

Before starting firewalld over SSH, `ssh_admitted` in
[managedos_host_firewall.py](../../ansible/collections/ansible_collections/bootwright/core/plugins/modules/managedos_host_firewall.py)
proves from `firewall-offline-cmd` that the new rules keep the session open.
`offline_ssh_shape` refused any rich rule in any zone. RHEL 9 ships the
`nm-shared` zone with `rule priority="32767" reject`, so every stock host
whose firewalld was not already running refused. A host whose firewalld already
runs and is enabled skips the proof, which is why a controller passed.

## Decision

Sources, direct rules and non-ICMPv6 policies still refuse in any zone,
because a source pulls traffic into its zone wherever it is declared. A rich
rule refuses only in a zone that carries the proved SSH traffic. That makes the
predicted zone decisive: a connection that names no zone but shares its network
(`ipv4.method` or `ipv6.method` `shared`) lands in `nm-shared`, not the default
zone, and an SSH interface bound to another zone in firewalld's permanent
configuration refuses. Evidence:
`test_offline_ssh_shape_admits_a_stock_rhel_9_8_listing_whose_ssh_arrives_in_public`
replays the host's listing; the local-start admission tests carry the same
`nm-shared` block and failed before the change.


## Argument specification without `sshSources`

The next run failed in the grant block `os-firewall-<machine>` with
`Validation of arguments failed: bootwright_os_firewall_request.sshSources`.
`FirewallRequest` sends `sshSources` (omitted when empty) for a Machine with
`spec.firewall.ssh.allowedSources`, and the role's
`meta/argument_specs.yml` never declared it. The capability contract suite's
firewall example declares no sources, so `roleRefusals` never saw the field;
`TestTheHostFirewallRoleAdmitsARequestRestrictingSSHSources` now sends it. Until
this fix no SSH source filter had run on a real host.
