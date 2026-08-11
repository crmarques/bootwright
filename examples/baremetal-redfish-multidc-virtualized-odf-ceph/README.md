# Bare-Metal Multi-DC Virtualized ODF Ceph

This reference example shows the full canonical layout with:

- two three-node bare-metal OpenShift parent clusters in separate data centers;
- one three-master plus three-worker KubeVirt-hosted OpenShift child cluster on each parent;
- one managed stretched Ceph cluster spanning two data sites plus a tiebreaker;
- OpenShift Data Foundation bound to all four container clusters for block and file storage (object storage via the native Ceph RGW endpoint);
- OpenShift Virtualization bound only to the two parent clusters;
- child cluster namespace, two `ClusterUserDefinedNetwork` localnets, and the
  matching `NodeNetworkConfigurationPolicy` delivered as a cluster add-on; the
  primary OpenShift NIC uses MTU 1500 and a separate routed Ceph client NIC uses
  MTU 9000, with each KubeVirt VM NIC bound to its own CUDN-derived NAD. The
  network add-on requires the `nmstate` capability advertised by the dedicated
  Kubernetes NMState Operator add-on, then waits for the NNCP to report
  `Available=True` and for both derived NADs to exist before its ready record
  can unblock child VM creation.

Like the smaller examples, the YAML is lean and relies on documented defaults.
The bare-metal machines use a machine-wide attachment; child VMs use
`interfaceAttachments[]` because their two NICs consume distinct CUDNs.

The network fabric must provide VLANs 171 and 172 with gateways
`192.168.171.1` and `192.168.172.1`, route both subnets bidirectionally to the
Ceph daemon public networks `192.168.141.0/24`, `192.168.142.0/24`, and
`192.168.143.0/24`, and carry MTU 9000 across the complete secondary path. The
child NetworkConfigs make the forward monitor routes explicit, and the parent
NetworkConfigs direct the return path for both child subnets to their site
gateway. The gateways still own forwarding in both directions, including the
route from the OS-provided tiebreaker network. The routed child subnets are Data
Foundation node/client networks, not Ceph daemon bind networks, so they are
deliberately absent from
`StorageCluster.spec.ceph.networks.publicCIDRs`.

The shipped NNCPs peel the primary and Ceph VLANs off the parent node's
`primary` uplink into separate OVS bridges; the child VMs do not consume a
trunk. Each bridge permits the extra patch ports that OVN creates for its
localnet mapping. Preserve that topology or adapt the base interface, VLAN IDs,
and bridge mappings together for the target fabric. The parent uplink and
complete Ceph path must support MTU 9000 before the NNCP readiness gate can be
treated as proof of usable end-to-end jumbo frames.

> **This tree is also a shared test fixture.** Beyond the
> every-example-validates guard, it carries content-level assertions across six
> internal packages — several pin its cluster names (`dc1-metal-ocp`,
> `dc2-metal-ocp`, `dc1-child-ocp`, `dc2-child-ocp`), its
> `openshift-data-foundation` add-on, and its `ceph-storage` storage cluster by
> literal. Renaming any of them is an API-surface change, not a pedagogical
> edit; expect to update those tests in the same commit.

## Edit First

- `environment.yaml`: base domain, resource directories, secret names, and
  shared service catalog entries.
- `infra/`: provider hosts, bare-metal and KubeVirt providers, network
  attachments, artifact services, and network templates.
- `clusters/container/*/<machine>.yaml`: per-cluster node Machines — hardware,
  addresses, and KubeVirt/network bindings, one object per file.
- `clusters/container/*/cluster.yaml`: OpenShift release, VIP endpoints, artifact
  access, networking, and node bindings for parent and child clusters.
- `clusters/storage/ceph-storage/*.yaml`: Ceph topology, pools, filesystems,
  RGW, and exports.
- `add-ons/*.yaml` and `clusters/container/*/add-on-binding.yaml`: bootstrap
  add-ons, capability providers, storage inputs, and profile bindings.

## Validate And Apply

`secret generate` only materializes the `generated:` entries; set the operator
secrets this example declares (`openshift-pull-secret`, `bmc-credentials`,
`ceph-registry-credentials`, `redhat-org`, `redhat-activation-key`) yourself
first. `bastion-host-ssh` points at a local key file. After each step, run
`bootwright status` for the suggested next command. See
[getting started](../../docs/getting-started/index.md) for the full secret and
host-trust workflow.

```text
bootwright validate -f <input-dir>
bootwright context init --name lab -f <input-dir>
bootwright secret set --name openshift-pull-secret --pull-secret <path>
printf '%s\n' "${BMC_PASS}" | bootwright secret set --name bmc-credentials --username "${BMC_USER}" --password-stdin
printf '%s\n' "${REGISTRY_PASS}" | bootwright secret set --name ceph-registry-credentials --username "${REGISTRY_USER}" --password-stdin
bootwright secret set --name redhat-org --raw-file <org-id-file>
bootwright secret set --name redhat-activation-key --raw-file <activation-key-file>
bootwright secret generate
bootwright bastion setup --yes
bootwright preflight all
bootwright plan
bootwright apply --yes
bootwright status --watch
```

Use `--clusters` only for focused recovery. Child KubeVirt clusters must be
selected with their parent cluster unless parent install and Virtualization
readiness records already exist.
