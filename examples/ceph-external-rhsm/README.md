# Ceph with corporate-owned RHSM registration

Selects Red Hat Ceph Storage (`spec.ceph.distribution: redhat`) while delegating
RHSM registration to site automation: the `rhcs` Entitlement sets
`rhsm.management: external`, so Bootwright never registers the nodes, never
writes RHSM proxy or repository configuration, and requires no RHSM
organization or activation-key secrets. Registry login still comes from the
entitlement (`registry.credentialsRef`).

The corporate registration runs as the `corporate-rhsm` `Playbook`
anchored at `stage: deps, timing: before` — after the machines-phase work has
put the OS in place, and gating the `deps` sub-phase where Bootwright installs
the Ceph dependencies. With the default `failureMode: fail`, a failed
registration blocks the Ceph work instead of failing later inside package
installs. The shipped playbook is
a stub; replace `playbooks-custom/playbooks/corporate-rhsm.yml` with the
site playbook (vendored roles/collections go next to it, see the
provisioning-playbooks concept page).

The registration playbook must leave every storage node able to install the
RHEL BaseOS/AppStream and `rhceph-*-tools` packages (activation-key repo sets,
a Satellite content view, or an internal mirror all work); Bootwright verifies
package availability fail-closed when installing `cephadm`. Ceph client
commands run through `cephadm shell`.

## Credentials to supply

| Entitlement field | Secret | What it holds | How to set it |
| --- | --- | --- | --- |
| `registry.credentialsRef` | `redhat-registry-credentials` | `registry.redhat.io` login | `bootwright secret set --name redhat-registry-credentials --username '<sa-user>' --password-stdin` |
