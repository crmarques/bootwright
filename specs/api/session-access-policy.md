# Session access policy

An administrator sets one standalone `SessionAccessPolicy` per context
with `bootwright policy set --context lab --file sessions.yaml`.
The policy uses the `bootwright.io/v1alpha1` envelope, including optional
metadata labels. Its name equals the context name. It lives in root-owned
context state and is removed when that context is deleted. Setting it
validates the document, subjects and targets before atomically replacing the
active policy. An empty `spec.grants: []` revokes delegated access for new
requests.

An administrator manages the policy through its lifecycle with four commands.
`policy set --check` runs the same validation and writes nothing; either form
prints every grant of the document with all its subjects and expanded
Machines. `policy show` prints the installed document verbatim, or says that
none is installed, and does not expand targets, so a stale target cannot break
it. `policy delete` removes the installed document after confirmation, or
succeeds without a prompt when none is installed; for new admissions it is
equivalent to an empty grants list, and running sessions are unaffected.
`policy permissions` lists the caller's own grants. Only `policy permissions`
is reachable through the delegated entry.

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
a clean absolute executable path. Arguments match literally and in order,
before the session quotes them for the remote shell.
`rsh: {}` grants an interactive shell as the named remote account.
The administrator approves the full behavior of each permitted program,
including any programs it can invoke. The input is one UTF-8 YAML document
of at most 64 KiB, with at most 128 grants and 64 distinct subjects. Every
scalar is a YAML string; aliases, anchors, nulls, unknown fields and duplicate
fields refuse `access.policy`, held by `TestSessionPolicyRejectsInvalidDocuments`
and `TestSessionPolicyAcceptsOnlyPlainStringYAML`.

## Identity and targets

Each subject has one `user` or `group` name. The controller's Name Service
Switch (NSS) resolves both short names and qualified names as exposed by its
configured identity provider, including LDAP and AD. The controller looks up
subject IDs and verifies the caller through its
[local privilege boundary](../cli.md#local-privilege-and-user-identity).
User grants match the UID. Group grants match the primary or supplementary
GIDs returned for the caller. Directory-side membership and cache freshness
follow the controller's provider configuration.

Each admission and each `policy permissions` call reads the stored policy and
resolves every subject of every grant again within `resolveBound`, 15 seconds;
no ID resolved at installation is reused. A policy that cannot be read or
decoded, a subject that does not resolve within the bound, or a target absent
from the effective context graph for a grant that matches the caller refuses
the whole request with `access.denied`, held by
`TestPolicyPermissionsRefuseUnusableState` and
`TestAdmissionResolvesEverySubjectAgainAndAnyFailureDeniesTheRequest`.

A grant applies to the union of its `machineRefs` and the Machines referenced
by every declared node of its `clusterRefs`. Each cluster reference selects
an OpenShiftCluster or managed CephCluster in the context. External Ceph
has no session roster. Cluster grants follow membership in the selected
context's effective state. Roster updates can change access to a Machine.
A shared Machine has the same authorization through either command route.
The [cluster session selector](../cli/cluster-access.md#sessions) determines
which nodes an invocation requests, including its default node.

## Delegated execution

For SSH session commands and `policy permissions`, a caller that sudo does not
admit to the ordinary command is relaunched through the delegated entry of the
root-owned installed executable, which this sudoers rule admits. Use the actual
installed path and the controller's spelling for the delegated group. Manage
the rule on the controller with `visudo`.

```sudoers
%PLATFORM_SUPPORT ALL=(root) /usr/local/bin/bootwright __bootwright_delegate *
```

The delegated entry admits `machine exec`, `machine rsh`, `cluster exec`,
`cluster rsh` and `policy permissions`. Grant wider sudo access only to
trusted administrators. The unprivileged CLI chooses the entry once per
invocation:

| Invocation | Probe | Relaunch |
| --- | --- | --- |
| No acquisition-route variables and no enrollment input | `sudo -l -u #0 -- <bootwright> <arguments>`, as the caller, with `-n` when the invocation is unattended | The ordinary command when sudo lists it; the delegated entry when sudo refuses the listing or the listing fails to run |
| Acquisition-route variables or enrollment input | None | The delegated entry |

Sudo admits, authenticates and logs the ordinary command under its own rule,
and the session follows the identity rules of the
[CLI](../cli.md#machine-ssh-sessions) with no grant. Sudo authenticates a
listing by its `listpw` setting, and the relaunch reuses that credential
while `timestamp_timeout` keeps it, so the operator normally answers one
prompt; the refusal text of an unattended or quiet listing is discarded. An
administrator therefore types the same command as everyone else. A
delegate-only caller's listing is a refused sudo check, which sudo may log and,
under its mail settings, report. `TestADelegatedCommandTakesTheOrdinaryEntryOnlyWhenSudoListsItForTheCaller`
and `TestADelegatedCommandKeepsTheDelegatedEntryWhenTheListingCannotAnswer` hold
the table.
Administrators manage policy with `bootwright policy set`.

A delegated session needs the configured remote account, key authentication
and pre-established host trust: a delegated session never records host trust,
so a Machine with no declared, installed or recorded host key refuses
`trust.identity`, and a context administrator enrolls it with `machine trust`.
The authorized target and the grant come from
one effective context graph. Each target in a batch needs one complete matching
grant for the verified caller, Machine, remote account and operation. With
no grant, Bootwright refuses before lending credentials or opening any
session. Any authorization refusal exits 255 with empty stdout. Policy
changes affect new admissions. Running sessions end under the normal SSH
session contract.

The delegated entry refuses every SSH identity override with `access.denied`
and exit 255 before it lends any credential: `--ssh-user`, even naming the
grant's remote account, `--ssh-id-file`, `--ssh-ask-password`,
`--ssh-ask-sudo-password` and `--ssh-user-for-provisioned`. It offers only
the Machine's configured key. Every delegated session sets the client's
escape character off, clears all port forwarding and never forwards the agent.
A delegated exec runs with no TTY and closed standard input. A delegated rsh
is an interactive shell as the remote account, with neither escape nor port or
agent forwarding.

`bootwright policy permissions --context lab` shows grants matching the
caller, the grant's subjects that matched the caller, their authored selectors
and expanded Machines. Subjects that do not match the caller stay unlisted.

Code: `access.Service.admitOptions` over `machine.SSHOptions.Borrowed`, and
the delegated arguments of `internal/machine/sshlocal`.
Held by: `TestDelegatedSessionsRefuseEverySSHIdentityOverrideBeforeLending`,
`TestDelegatedSessionsRefuseOverridesAndUnconfiguredKeys`,
`TestDelegatedExecNeedsExistingTrustAndClosesInput` and
`TestDelegatedArgumentsDisableEscapeAndForwardingAndKeepShellInteractive`.
