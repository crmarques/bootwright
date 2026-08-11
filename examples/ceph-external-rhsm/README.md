# Ceph with corporate-owned RHSM registration

Selects Red Hat Ceph Storage (`spec.ceph.distribution: redhat`) while delegating
RHSM registration to site automation: the `rhcs` Entitlement sets
`rhsm.management: external`, so Bootwright never registers the nodes, never
writes RHSM proxy or repository configuration, and requires no RHSM
organization or activation-key secrets. Registry login still comes from the
entitlement (`registry.credentialsRef`).

The corporate registration runs as the `corporate-rhsm` `CustomPlaybook`
anchored with `gates: deps` — after the machines-phase work has put the OS in
place, and gating the `deps` sub-phase where Bootwright installs the Ceph
dependencies. With the default `onFailure: fail`, a failed registration blocks
the Ceph work instead of failing later inside package installs. The shipped
playbook is a stub; replace `custom-playbooks/playbooks/corporate-rhsm.yml`
with the site playbook (vendored roles/collections go next to it, see
[Custom playbooks](../../docs/concepts/custom-playbooks.md)).

The registration playbook must leave every storage node able to install the
RHEL BaseOS/AppStream and `rhceph-*-tools` packages (activation-key repo sets,
a Satellite content view, or an internal mirror all work); Bootwright verifies
package availability fail-closed while installing the rendered native tooling.
Red Hat's optional pins are omitted here, so `cephadm`, `ceph-common`, and
`cephadm-ansible` install with `present` from the operator-enabled repositories;
Bootwright then executes the RPM-owned preflight locally. Ceph client commands
after preparation still run through `cephadm shell`.

## Edit First

- `environment.yaml`: the fleet name and `domains.base`.
- `machine.yaml`: the provided storage node — its `ssh` address, login user, and
  `access.ssh.auth.privateKeyRef`.
- `secrets.yaml`: `ceph-cluster-ssh` (generated — the cephadm cluster key),
  `ceph-node-ssh` (points at a local private-key file), and
  `redhat-registry-credentials`.
- `rhcs.yaml`: the `redhat-ceph` `Entitlement` — `rhsm.management: external`
  plus the registry credential reference.
- `storage.yaml`: `spec.ceph.release`, required `image.base`, optional native
  package/image pins, the cephadm bootstrap node, and the node topology.
- `custom-playbooks/corporate-rhsm.yaml`: the gating `CustomPlaybook` — its
  `gates` anchor and `target.clusters`.
- `custom-playbooks/playbooks/corporate-rhsm.yml`: the stub playbook to replace
  with the site registration.

## Credentials to supply

| Entitlement field | Secret | What it holds | How to set it |
| --- | --- | --- | --- |
| `registry.credentialsRef` | `redhat-registry-credentials` | `registry.redhat.io` login | `bootwright secret set --name redhat-registry-credentials --username '<sa-user>' --password-stdin` |
