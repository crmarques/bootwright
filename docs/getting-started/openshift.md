---
title: Provisioning an OpenShift cluster
description: Stand up a single-node OpenShift cluster on libvirt with bootwright, from a scaffolded input tree to cluster access.
---

# Provisioning an OpenShift cluster

This guide walks one small, self-contained example end to end: a single-node
OpenShift (SNO) cluster on a libvirt host, where libvirt also emulates the
Redfish BMC that boots the node. No real server hardware is required.

It assumes you have completed [Installation and Setup](installation.md) — the CLI
is installed and you understand the context, secret, host-trust, and bastion-prep
mechanics this guide reuses.

## Scaffold A Starter Input Tree

Generate a single-node lab workspace. The default provider,
`emulated-bare-metal`, is the libvirt + emulated Redfish substrate this guide
uses:

```bash
bootwright example init --name my-sno-lab --output-dir ./my-sno-lab
```

The scaffold writes nine `apiVersion: bootwright.io/v1alpha1` files:

```text
my-sno-lab/
  environment.yaml
  infra/
    providers/
      provider.yaml
    machines/
      bastion.yaml
    networkconfigs/
      networks.yaml
    components/
      load-balancer.yaml
      name-resolution.yaml
      ntp-server.yaml
  clusters/
    container/
      my-sno-lab/
        cluster.yaml
        cluster-machines.yaml
```

The files are safe to commit once you keep secret bytes out of them (the
scaffold already does). The cluster name you passed, `my-sno-lab`, is used as the
`Environment`, `InfraProvider`, `NetworkConfig`, and `ContainerCluster` name
prefix throughout the tree. Pass `--provider bare-metal`, `--provider vsphere`,
or `--provider kubevirt` to scaffold a different substrate; the cluster name must
be a lowercase DNS label.

## Understand The Input Objects

This lab uses six of bootwright's authored kinds. Read them in dependency order;
each owns exactly one slice of the truth.

### Environment (`environment.yaml`)

Fleet-wide defaults and the catalog that ties the tree together. For this lab it
sets `spec.baseDomain` (the DNS domain your cluster lives under), selects the
managed shared services by reference under `spec.infraComponents` (name
resolution and NTP here), and **names** the secrets the workflow needs under
`spec.secrets`. Naming is the key idea: the YAML declares secret *names*, while
the bytes live in the local context.

The scaffold declares four secrets:

| Secret name | Purpose | How it is supplied |
| --- | --- | --- |
| `openshift-pull-secret` | OpenShift pull secret | You set it: `bootwright secret set` |
| `my-sno-lab-cluster-admin-ssh-key` | Node (core user) SSH key pair | Generated for you: `bootwright secret generate` |
| `bastion-host-ssh` | SSH private key to reach the bastion | `file:` reference to a key you create — see [Installation](installation.md#the-bastion-ssh-key) |
| `bmc-credentials` | Credentials for the emulated Redfish BMC | Generated for you: `bootwright secret generate` (bootwright configures the emulator) |

### Machine (`infra/machines/bastion.yaml`, `clusters/container/my-sno-lab/cluster-machines.yaml`)

Two machines, in two files — the shared bastion under `infra/`, the cluster node
alongside its cluster. Machines own substrate binding, OS mode, durable
addresses, and SSH access — never install intent.

- `bastion` (`infra/machines/bastion.yaml`): the service host. It declares
  `capabilities` (`libvirt`, `container-runtime`, `load-balancer`,
  `name-resolution`, `ntp`), `os.provided: true` (bootwright does not install its
  OS), its `addresses` (`ssh` and `cluster-lan`), and `access.ssh` (which secret
  key and which address to connect over).
- `my-sno-lab-master-0` (`clusters/container/my-sno-lab/cluster-machines.yaml`):
  the OpenShift node. It declares `capabilities: [openshift-node]`, its
  `substrate` binding (`providerRef` + `profileRef: sno`), `os.provided: false`
  (bootwright installs OpenShift onto it), its `network.config` (which
  `NetworkConfig` and per-interface address it gets), and its static `addresses`
  (the node IP).

### NetworkConfig (`infra/networkconfigs/networks.yaml`)

The cluster machine network plus a reusable NMState host template for the agent
install. It owns `spec.machineNetwork[].cidr`, the name-resolution selection
(`nameResolutionRefs`), and the NMState `template` (interface `primary`, the DNS
resolver, and the default route).

### InfraProvider (`infra/providers/provider.yaml`)

The substrate facts, all on one `type: libvirt` provider (`my-sno-lab-libvirt`).
It owns the libvirt `uri`, the emulated-BMC credential reference
(`bmcEmulationDefaults`), the VM sizing `machineProfiles` (the `sno` profile: CPU,
memory, disk), and the `networkAttachments` that name the libvirt bridge. The
libvirt adapter drives the emulated Redfish BMC that boots the node from the agent
ISO — the lab needs no separate bare-metal provider.

### InfraComponent (`infra/components/`)

Machine-bound shared services, one object per file, all pinned to `bastion`:

- `load-balancer.yaml` (`haproxy`): publishes the `control-plane` and `apps`
  virtual IPs under `spec.loadBalancer.bindAddresses[]`.
- `name-resolution.yaml` (`dnsmasq`): serves cluster DNS.
- `ntp-server.yaml` (`chrony`): serves time.

### ContainerCluster (`clusters/container/my-sno-lab/cluster.yaml`)

The OpenShift install intent — and only install intent. It owns the release
(`spec.distribution.release.version`), the install `platform` render mode, the
API / api-int / ingress `endpoints` (here sourced from the `load-balancer`
component), the node SSH key reference (`nodeSSH.keyPairRef`), cluster/service
`networking`, and the `hosts` list that binds the logical hostname `master-0` to
the `my-sno-lab-master-0` Machine.

The ownership rule to carry forward: **`ContainerCluster` owns install intent,
`Machine` owns node facts, `InfraProvider` owns substrate facts.** Swapping a
provider should not force unrelated changes to cluster intent. See
[The desired-state model](../concepts/index.md) for the full model and every
field.

## Edit The Required Values

Open the scaffold and adjust these for your host. The defaults form a working
`192.168.130.0/24` lab; change them only where your environment differs.

| File | Field | Set it to |
| --- | --- | --- |
| `environment.yaml` | `spec.baseDomain` | A DNS base domain you control or route in the lab (default `example.test`). |
| `environment.yaml` | `spec.secrets` `bastion-host-ssh` (`file:`) | Path to the SSH key that reaches the bastion (default `~/.ssh/bootwright-ssh-key`); create it first — see [Installation](installation.md#the-bastion-ssh-key). |
| `infra/machines/bastion.yaml` | `Machine/bastion` `spec.addresses` (`ssh`) | The address bootwright uses to SSH to the bastion (default `192.168.10.11`). |
| `clusters/container/my-sno-lab/cluster-machines.yaml` | `Machine/my-sno-lab-master-0` `spec.addresses` (`ip`) | The node's static IP on the machine network (default `192.168.130.20`). |
| `infra/networkconfigs/networks.yaml` | `spec.machineNetwork[].cidr` | The cluster machine network CIDR (default `192.168.130.0/24`). |
| `infra/providers/provider.yaml` | `spec.libvirt.uri` | The libvirt URI, usually `qemu:///system`. |
| `infra/providers/provider.yaml` | `spec.libvirt.machineProfiles[]` | CPU, memory, and disk for the node VM (default `sno`: 8 vCPU / 22528 MiB / 120 GiB). |
| `infra/providers/provider.yaml` | `spec.networkAttachments[].libvirt.bridge` | The libvirt bridge on the machine network. |
| `infra/components/load-balancer.yaml` | `InfraComponent/load-balancer` `spec.loadBalancer.bindAddresses[]` | The `control-plane` VIP (`192.168.130.10`) and `apps` VIP (`192.168.130.11`). |
| `clusters/container/my-sno-lab/cluster.yaml` | `spec.distribution.release.version` | The OpenShift release to install (default `4.21.15`). |

Leave `spec.secrets` as names only. Then validate the tree offline (no host
contact, no context needed):

```bash
bootwright validate -f ./my-sno-lab
```

Fix any diagnostic by editing the named object and field, then validate again.

## Set Up The Context

The next commands follow the setup sequence from
[Installation and Setup](installation.md) — see that page for what each one does
and why. Create the context from your edited tree:

```bash
bootwright context init --name lab -f ./my-sno-lab
bootwright context current
bootwright status
```

Load the secret bytes. Only the pull secret is operator-supplied; the generated
(`bmc-credentials`, the node SSH key) and `file:`-sourced (`bastion-host-ssh`)
entries are converged by `secret generate`:

```bash
bootwright secret set --name openshift-pull-secret --pull-secret ~/openshift-pull-secret.json
bootwright secret generate
bootwright secret check
bootwright secret list
```

Record host trust, prepare the bastion, and run the read-only checks:

```bash
bootwright machine trust
bootwright bastion setup --yes
bootwright preflight all
bootwright render effective
bootwright plan
```

## Apply

Converge the lab. `apply` reconciles by default: it creates missing objects,
skips objects whose recorded state already matches, and fails closed on drift or
foreign ownership before changing anything.

```bash
bootwright apply --yes
bootwright status --watch
```

`apply --yes` prepares the libvirt substrate, creates and boots the node VM
through the emulated Redfish BMC, runs the OpenShift agent install to completion,
and applies any bound add-ons. `status --watch` refreshes until the run reaches a
terminal state. Re-running `apply` later is safe — matching work is skipped.

## Access The Cluster

Once apply finishes, list the local access details (URLs, kubeconfig path, and
the password-retrieval command — no secret bytes are printed by default):

```bash
bootwright cluster access
```

To use the cluster, save the generated admin kubeconfig to a file you own. The
command streams it to stdout so you can redirect it without copying the
root-owned source by hand:

```bash
bootwright cluster kubeconfig --name my-sno-lab > ~/.kube/my-sno-lab
chmod 0600 ~/.kube/my-sno-lab
oc --kubeconfig ~/.kube/my-sno-lab get nodes
```

To merge it into your default kubeconfig under a unique context name instead:

```bash
export CLUSTER=my-sno-lab
export SRC="${TMPDIR:-/tmp}/bootwright-${CLUSTER}.kubeconfig"
export MERGED="${TMPDIR:-/tmp}/bootwright-merged-kubeconfig"

mkdir -p "${HOME}/.kube"
touch "${HOME}/.kube/config"
chmod 0600 "${HOME}/.kube/config"

bootwright cluster kubeconfig --name "${CLUSTER}" > "${SRC}"
chmod 0600 "${SRC}"
CTX="$(oc --kubeconfig "${SRC}" config current-context)"
oc --kubeconfig "${SRC}" config rename-context "${CTX}" "${CLUSTER}-admin"
KUBECONFIG="${HOME}/.kube/config:${SRC}" oc config view --flatten > "${MERGED}"
install -m 0600 "${MERGED}" "${HOME}/.kube/config"
oc config use-context "${CLUSTER}-admin"
```

The kubeconfig carries admin credentials: keep it in a private path and never
commit it.

## Next steps

- Build storage next with [Provisioning a Ceph cluster](ceph.md).
- Read [The desired-state model](../concepts/index.md) to understand contexts,
  stages, the apply modes, and object ownership before you change the layout.
- Browse the [reference examples](../advanced/examples.md) to pick a larger tree —
  shared services, managed Ceph storage, KubeVirt child clusters, or post-install
  add-ons.
- See [Troubleshooting](../troubleshooting.md) when validation, SSH trust,
  artifact fetch, or apply state checks fail.
