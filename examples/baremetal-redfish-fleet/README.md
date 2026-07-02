# Bare-Metal Redfish Fleet

Two bare-metal OpenShift clusters in separate data-center networks using shared
provider services and directory resource selection.

## Edit First

- `environment.yaml`: base domain, `resources`, secret names, and artifact
  server catalog.
- `infra/machines/*.yaml`: service/provider host addresses and SSH key reference.
- `infra/providers/*.yaml`: hardware MACs, BMC Redfish URLs, root devices, and
  BMC credential reference.
- `infra/networkconfigs/*.yaml`: per-site machine CIDRs, resolvers, routes, and
  NMState interfaces.
- `clusters/container/*/<machine>.yaml`: per-cluster node Machines — hardware,
  addresses, and network config, one object per file.
- `clusters/container/*/cluster.yaml`: release, VIP endpoints, artifact access,
  cluster networking, and node bindings.

## Validate And Apply

`secret generate` only materializes the `generated:` entries; set the operator
secrets this example declares (`openshift-pull-secret`, `bmc-credentials`)
yourself first. `bastion-host-ssh` points at a local key file. After each step,
run `bootwright status` for the suggested next command. See
[getting started](../../docs/getting-started/index.md) for the full secret and
host-trust workflow.

```text
bootwright validate -f <input-dir>
bootwright context init --name lab -f <input-dir>
bootwright secret set --name openshift-pull-secret --pull-secret <path>
printf '%s\n' "${BMC_PASS}" | bootwright secret set --name bmc-credentials --username "${BMC_USER}" --password-stdin
bootwright secret generate
bootwright bastion setup --yes
bootwright preflight all
bootwright plan
bootwright apply --yes
bootwright status --watch
```

Use `bootwright apply --stage infra --clusters <name> --yes` or
`bootwright apply --stage clusters --clusters <name> --yes` only for focused
recovery after the full graph has been reviewed.
