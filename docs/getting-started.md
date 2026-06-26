---
title: Getting Started
description: Provision a single-node OpenShift lab on libvirt with Bootwright, from an empty bastion to cluster access.
---

# Getting Started

Bootwright is a desired-state orchestrator for turning cloud platform intent
into reality. You describe substrates, machines, networks, shared services, and
clusters as declarative YAML; Bootwright validates that intent, renders the
inputs the official installer and CLIs need, and converges it all idempotently.
It can stand up a complete platform from scratch or converge a single component
for build-out, recovery, or maintenance. Today it provisions OpenShift, OKD, and
Ceph clusters over bare metal, vSphere, KubeVirt, and libvirt, and binds them
into one platform.

This guide walks one small, self-contained example end to end: a single-node
OpenShift cluster on a libvirt host, where libvirt also emulates the Redfish BMC
that boots the node. No real server hardware is required. It is the smallest
connected lab and the best place to learn the model before you grow into managed
machine OS installs, Ceph storage, KubeVirt child clusters, and post-install
add-ons.

Every command below runs as your normal user. Bootwright re-executes through
`sudo` on its own when it needs protected local state; do not prefix it with
`sudo` yourself.

!!! note "Why Bootwright asks for your password"
    Bootwright keeps its context and state in a root-owned directory under
    `/var/lib/bootwright`, so any command that reads or changes that state needs
    `sudo`. When `sudo` is not already authorized, Bootwright prompts you once and
    reuses that for the rest of the run — including the BECOME password Ansible
    needs for privileged steps on remote hosts. Read-only commands (`plan`,
    `status`, `cluster`, `state-check`, `print-env`) still need `sudo` to read the
    context, but they change nothing. To avoid the prompt, run as root or
    pre-authorize once with `sudo -v`.

## What You Need

- A Linux bastion host where you install and run `bootwright`.
- `sudo` on the bastion (prompted or passwordless) for Bootwright-managed state
  under `/var/lib/bootwright`.
- Libvirt with `qemu:///system` available on the bastion (this example treats the
  bastion as the libvirt host).
- An SSH key pair the bastion can use to reach the libvirt/service host.
- An OpenShift pull secret file, stored **outside** the input tree.
- A free machine network on the libvirt host with room for the node IP and two
  virtual IPs (this example uses `192.168.130.0/24`).

Bootwright desired state is safe to commit: it names secrets, but never stores
their bytes. Keep pull secrets, private keys, kubeconfigs, tokens, and passwords
out of the YAML; you load them into the local Bootwright context separately.

## 1. Install The CLI

Download the `bootwright` binary for your platform from
[GitHub Releases](https://github.com/crmarques/bootwright/releases), then put it
on your `PATH`:

```bash
chmod +x bootwright
sudo install -m 0755 bootwright /usr/local/bin/bootwright
bootwright version
```

Optional Bash completion:

```bash
mkdir -p "${HOME}/.local/share/bash-completion/completions"
bootwright completion bash > "${HOME}/.local/share/bash-completion/completions/bootwright"
source "${HOME}/.local/share/bash-completion/completions/bootwright"
```

## 2. Scaffold A Starter Input Tree

Generate a single-node lab workspace. The default provider,
`emulated-bare-metal`, is the libvirt + emulated Redfish substrate this guide
uses:

```bash
bootwright example init --name my-sno-lab --output ./my-sno-lab
```

The scaffold writes six `apiVersion: bootwright.io/v1alpha1` files:

```text
my-sno-lab/
  environment.yaml
  shared/
    machines.yaml
    networks.yaml
    provider.yaml
    infra-component.yaml
  clusters/
    my-sno-lab/
      cluster.yaml
```

The files are safe to commit once you keep secret bytes out of them (the
scaffold already does). The cluster name you passed, `my-sno-lab`, is used as the
`Environment`, `InfraProvider`, `NetworkConfig`, and `ContainerCluster` name
prefix throughout the tree. Pass `--provider bare-metal`, `--provider vsphere`,
or `--provider kubevirt` to scaffold a different substrate; the cluster name must
be a lowercase DNS label.

## 3. Understand The Input Objects

This lab uses six of Bootwright's authored kinds. Read them in dependency order;
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
| `bastion-host-ssh` | SSH private key to reach the bastion | `file:` reference to a key you own |
| `bmc-credentials` | Credentials for the emulated Redfish BMC | You set it: `bootwright secret set` |

### Machine (`shared/machines.yaml`)

Two machines. Machines own substrate binding, OS mode, durable addresses, and
SSH access — never install intent.

- `bastion`: the service host. It declares `capabilities` (`libvirt`,
  `container-runtime`, `load-balancer`, `name-resolution`, `ntp`),
  `os.provided: true` (Bootwright does not install its OS), its `addresses`
  (`ssh` and `cluster-lan`), and `access.ssh` (which secret key and which address
  to connect over).
- `my-sno-lab-master-0`: the OpenShift node. It declares
  `capabilities: [openshift-node]`, its `substrate` binding (`providerRef` +
  `profileRef: sno`), `os.provided: false` (Bootwright installs OpenShift onto
  it), its `network.config` (which `NetworkConfig` and per-interface address it
  gets), and its static `addresses` (the node IP).

### NetworkConfig (`shared/networks.yaml`)

The cluster machine network plus a reusable NMState host template for the agent
install. It owns `spec.machineNetwork[].cidr`, the name-resolution selection
(`nameResolutionRefs`), and the NMState `template` (interface `primary`, the DNS
resolver, and the default route).

### InfraProvider (`shared/provider.yaml`)

The substrate facts. **Two** providers are declared — this is easy to miss:

- `my-sno-lab-libvirt` (`type: libvirt`): the VM substrate. It owns the libvirt
  `uri`, the emulated-BMC credential reference (`bmcEmulationDefaults`), the VM
  sizing `machineProfiles` (the `sno` profile: CPU, memory, disk), and the
  `networkAttachments` that name the libvirt bridge.
- `my-sno-lab-hosts` (`type: baremetal`): the externally-booted arm the node uses
  to receive the agent ISO over the emulated Redfish virtual media.

### InfraComponent (`shared/infra-component.yaml`)

Machine-bound shared services. Three are declared, all pinned to `bastion`:

- `load-balancer` (`haproxy`): publishes the `control-plane` and `apps` virtual
  IPs under `spec.loadBalancer.bindAddresses[]`.
- `name-resolution` (`dnsmasq`): serves cluster DNS.
- `ntp-server` (`chrony`): serves time.

### ContainerCluster (`clusters/my-sno-lab/cluster.yaml`)

The OpenShift install intent — and only install intent. It owns the release
(`spec.distribution.release.version`), the install `platform` render mode, the
API / api-int / ingress `endpoints` (here sourced from the `load-balancer`
component), the node SSH key reference (`nodeSSH.keyPairRef`), cluster/service
`networking`, and the `hosts` list that binds the logical hostname `master-0` to
the `my-sno-lab-master-0` Machine.

The ownership rule to carry forward: **`ContainerCluster` owns install intent,
`Machine` owns node facts, `InfraProvider` owns substrate facts.** Swapping a
provider should not force unrelated changes to cluster intent. See
[Concepts](concepts.md) and the [API Reference](api/index.md) for the full model
and every field.

## 4. Edit The Required Values

Open the scaffold and adjust these for your host. The defaults form a working
`192.168.130.0/24` lab; change them only where your environment differs.

| File | Field | Set it to |
| --- | --- | --- |
| `environment.yaml` | `spec.baseDomain` | A DNS base domain you control or route in the lab (default `example.test`). |
| `shared/machines.yaml` | `Machine/bastion` `spec.addresses` (`ssh`) | The address Bootwright uses to SSH to the bastion (default `192.168.10.11`). |
| `shared/machines.yaml` | `Machine/my-sno-lab-master-0` `spec.addresses` (`ip`) | The node's static IP on the machine network (default `192.168.130.20`). |
| `shared/networks.yaml` | `spec.machineNetwork[].cidr` | The cluster machine network CIDR (default `192.168.130.0/24`). |
| `shared/provider.yaml` | `spec.libvirt.uri` | The libvirt URI, usually `qemu:///system`. |
| `shared/provider.yaml` | `spec.libvirt.machineProfiles[]` | CPU, memory, and disk for the node VM (default `sno`: 8 vCPU / 22528 MiB / 120 GiB). |
| `shared/provider.yaml` | `spec.networkAttachments[].libvirt.bridge` | The libvirt bridge on the machine network. |
| `shared/infra-component.yaml` | `InfraComponent/load-balancer` `spec.loadBalancer.bindAddresses[]` | The `control-plane` VIP (`192.168.130.10`) and `apps` VIP (`192.168.130.11`). |
| `clusters/my-sno-lab/cluster.yaml` | `spec.distribution.release.version` | The OpenShift release to install (default `4.21.15`). |

Leave `spec.secrets` as names only. Then validate the tree offline (no host
contact, no context needed):

```bash
bootwright validate -f ./my-sno-lab
```

Fix any diagnostic by editing the named object and field, then validate again.

## 5. Create A Context

A context binds a name to a **copy** of your input directory and stores
generated state under `/var/lib/bootwright/contexts/<context>`. `context init`
copies the whole source tree into the context's `input/` directory, so the
context is self-contained and keeps working even if the source is moved or
deleted.

```bash
bootwright context init --name lab -f ./my-sno-lab
bootwright context current
bootwright status
```

`bootwright status` reports context readiness and prints the suggested next
command at every step; lean on it as you go. Because the input is a copy,
editing `./my-sno-lab` later has no effect until you refresh it:

```bash
bootwright context update --name lab -f ./my-sno-lab
```

`context init` fails if `lab` already exists; rerun with `--yes` to drop the
existing context and recreate it from the source.

## 6. Set And Generate Secrets

Load the secret bytes the YAML named into the encrypted context store. The
generated and `file:`-sourced entries are converged by `secret generate`; the
two operator-supplied entries you set yourself:

```bash
bootwright secret set --name openshift-pull-secret --pull-secret ~/openshift-pull-secret.json
printf '%s\n' "${BMC_PASS}" | bootwright secret set --name bmc-credentials --username "${BMC_USER}" --password-stdin
bootwright secret generate
bootwright secret check
bootwright secret list
```

`secret generate` generates the cluster admin SSH key pair and brings the
`bastion-host-ssh` file material into the context. `secret check` then reports
any declared secret still missing (exiting non-zero if so). Context-local secret
material is encrypted at rest.

## 7. Record Host Trust

Bootwright uses strict SSH host-key checking for non-local durable machines.
Record trust for the declared hosts before you run anything that connects:

```bash
bootwright host trust
bootwright status
```

Interactive `preflight` and `apply` can prompt to record a host key on first use,
but never under `--yes`, `--dry-run`, or JSON output, and a *changed* key is
never accepted automatically. Running `bootwright host trust` first keeps later
runs unattended and fail-closed-safe.

## 8. Prepare The Bastion And Check

Install the local tooling Bootwright needs, run preflight checks, materialize the
normalized desired state, and preview the task graph — none of these mutate your
hosts or clusters:

```bash
bootwright bastion setup --yes
bootwright preflight all
bootwright render effective
bootwright plan
```

| Command | Purpose |
| --- | --- |
| `bastion setup` | Installs the managed Ansible runtime and the release-matched OpenShift CLIs on the bastion. |
| `bastion check` | Re-runs the read-only bastion dependency checks (Python, Ansible runtime, `tar`, `sudo`); equivalent to `preflight bastion`. |
| `preflight all` | Checks the bastion, infrastructure hosts, and cluster prerequisites before any mutation. |
| `render effective` | Writes the desired state with all defaults applied, so you can see exactly what will be acted on. |
| `plan` | Shows the apply task graph without touching hosts or clusters. |

## 9. Apply

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

## 10. Access The Cluster

Once apply finishes, list the local access details (URLs, kubeconfig path, and
the password-retrieval command — no secret bytes are printed by default):

```bash
bootwright cluster access
```

To use the cluster, save the generated admin kubeconfig to a file you own. The
command streams it to stdout so you can redirect it without copying the
root-owned source by hand:

```bash
bootwright cluster kubeconfig --cluster my-sno-lab > ~/.kube/my-sno-lab
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

bootwright cluster kubeconfig --cluster "${CLUSTER}" > "${SRC}"
chmod 0600 "${SRC}"
CTX="$(oc --kubeconfig "${SRC}" config current-context)"
oc --kubeconfig "${SRC}" config rename-context "${CTX}" "${CLUSTER}-admin"
KUBECONFIG="${HOME}/.kube/config:${SRC}" oc config view --flatten > "${MERGED}"
install -m 0600 "${MERGED}" "${HOME}/.kube/config"
oc config use-context "${CLUSTER}-admin"
```

The kubeconfig carries admin credentials: keep it in a private path and never
commit it.

## Next Steps

- Read [Concepts](concepts.md) to understand contexts, stages, the apply modes,
  and object ownership before you change the layout.
- Browse [Reference Examples](advanced/examples.md) to pick a larger reference
  tree — shared services, managed Ceph storage, KubeVirt child clusters, or
  post-install add-ons.
- Use the [API Reference](api/index.md) for exact fields and allowed values on
  every kind.
- See [Troubleshooting](troubleshooting.md) when validation, SSH trust, artifact
  fetch, or apply state checks fail.
