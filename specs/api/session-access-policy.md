# Session access policy

An administrator installs one standalone `SessionAccessPolicy` per context
with `bootwright policy install --context lab --file sessions.yaml`.
The policy uses the `bootwright.io/v1alpha1` envelope, including optional
metadata labels. Its name equals the context name. It lives in root-owned
context state and is removed when that context is deleted. Installation
validates the document, subjects and targets before atomically replacing the
active policy. An empty `spec.grants: []` revokes delegated access for new
requests. Environment imports have their own input boundary.

```yaml
apiVersion: bootwright.io/v1alpha1
kind: SessionAccessPolicy
metadata:
  name: lab
spec:
  grants:
    - name: cluster-diagnostics
      subjects:
        - group: PLATFORM_SUPPORT
        - user: 'analyst@example.test'
      targets:
        clusterRefs:
          - apps
      remoteUser: core
      exec:
        commands:
          - - "/usr/bin/uptime"
          - - "/usr/bin/systemctl"
            - "is-active"
            - "kubelet.service"
    - name: worker-shell
      subjects:
        - user: maintenance
        - group: 'platform-oncall@example.test'
      targets:
        machineRefs:
          - worker-a
      remoteUser: core
      rsh: {}
```

Each grant has a unique DNS-label name, one or more subjects, explicit targets,
a POSIX remote account and exactly one of the `exec` and `rsh` blocks.
`exec.commands` contains one or more complete argument vectors starting with
a clean absolute executable path. Arguments match literally and in order.
`rsh: {}` grants an interactive shell as the named remote account.
The administrator approves the full behavior of each permitted program,
including any programs it can invoke. The input is one UTF-8 YAML document
of at most 64 KiB, with at most 128 grants and 64 distinct subjects.

## Identity and targets

Each subject has one `user` or `group` name. The controller's Name Service
Switch (NSS) resolves both short names and qualified names as exposed by its
configured identity provider, including LDAP and AD. The controller looks up
subject IDs and verifies the caller through its
[local privilege boundary](../cli.md#local-privilege-and-user-identity).
User grants match the UID. Group grants match the primary or supplementary
GIDs returned for the caller. Directory-side membership and cache freshness
follow the controller's provider configuration.

A grant applies to the union of its `machineRefs` and the Machines referenced
by every declared node of its `clusterRefs`. Each cluster reference selects
an OpenShiftCluster or managed CephCluster in the context. External Ceph
has no session roster. Cluster grants follow membership in the selected
context's effective state. Roster updates can change access to a Machine.
A shared Machine has the same authorization through either command route.
The [cluster session selector](../cli/cluster-access.md#sessions) determines
which nodes an invocation requests, including its default administrator node.

## Delegated execution

The normal unprivileged CLI relaunches SSH session commands and
`policy permissions` through the following root-owned installed entry in
sudoers. Use the actual installed path and the controller's spelling for the
delegated group. Manage the rule on the controller with `visudo`.

```sudoers
%PLATFORM_SUPPORT ALL=(root) /usr/local/bin/bootwright __bootwright_delegate *
```

The delegated entry admits `machine exec`, `machine rsh`, `cluster exec`,
`cluster rsh` and `policy permissions`. Grant wider sudo access only to
trusted administrators. Before it relaunches, the unprivileged CLI asks sudo
as the caller, with `sudo -l -u #0 -- <bootwright> <arguments>`, whether the
same command is permitted through the ordinary entry. Sudo authenticates a
listing by its `listpw` setting, and the relaunch reuses that credential
while `timestamp_timeout` keeps it, so the operator normally answers one
prompt; `-n` keeps the listing from prompting without a terminal, where its
refusal text is discarded. When sudo lists
it, the CLI relaunches the ordinary command, which sudo admits, authenticates
and logs under its own rule, and the session follows the identity rules of
the [CLI](../cli.md#machine-ssh-sessions) with no grant. Otherwise, including
an invocation that carries acquisition-route variables, it relaunches the
delegated entry. An administrator therefore types the same command as
everyone else. A delegate-only caller's listing is a refused sudo check,
which sudo may log and, under its mail settings, report.
Administrators install policy with `bootwright policy install`.

A delegated session needs the configured remote account, key authentication
and pre-established host trust. The authorized target and the grant come from
one effective context graph. Each target in a batch needs one complete matching
grant for the verified caller, Machine, remote account and operation. With
no grant, Bootwright refuses before lending credentials or opening any
session. Any authorization refusal exits 255 with empty stdout. Delegated
exec runs without a TTY and with closed stdin. Delegated rsh has ordinary
interactive shell capabilities on the remote account. Policy changes affect
new admissions. Running sessions end under the normal SSH session contract.

`bootwright policy permissions --context lab` shows grants matching the
caller, their authored selectors and expanded Machines.
