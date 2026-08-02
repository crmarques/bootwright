---
title: Installation and Setup
description: Install the bootwright CLI and learn the shared setup mechanics — contexts, secrets, host trust, and bastion prep — that the OpenShift and Ceph lab guides reuse.
---

# Installation and Setup

This page is the shared foundation both lab guides build on. It installs the
`bootwright` CLI and explains the setup mechanics — contexts, secrets, host
trust, and bastion preparation — that the [OpenShift](openshift.md) and
[Ceph](ceph.md) walkthroughs run without re-explaining the "why". Read it once,
then pick a cluster guide.

Every command below runs as your normal user. Bootwright re-executes through
`sudo` on its own when it needs protected local state; do not prefix it with
`sudo` yourself.

!!! note "Why bootwright asks for your password"
    Bootwright keeps its context and state in a root-owned directory under
    `/var/lib/bootwright`, so any command that reads or changes that state needs
    `sudo`. When `sudo` is not already authorized, bootwright prompts you once and
    reuses that for the rest of the run — including the BECOME password Ansible
    needs for privileged steps on remote hosts. Read-only commands (`plan`,
    `status`, `cluster`, `diff`) still need `sudo` to read the
    context, but they change nothing. To avoid the prompt, run as root or
    pre-authorize once with `sudo -v`.

## What You Need

- A Linux bastion host where you install and run `bootwright`. The concepts and
  advanced pages call this same host the **controller**; `bastion setup` is the
  command that prepares it.
- `sudo` on the bastion (prompted or passwordless) for bootwright-managed state
  under `/var/lib/bootwright`.
- Libvirt with `qemu:///system` available on the bastion (the labs treat the
  bastion as the libvirt host).
- `openssh-clients` on the bastion: `bootwright machine trust` shells out to
  `ssh-keyscan`, and it is not one of the `preflight bastion` checks.
- An SSH key pair the bastion uses to reach the libvirt/service host. The lab
  trees declare it as `bastion-host-ssh` at `~/.ssh/bootwright-ssh-key`;
  [Create the bastion SSH key](#create-the-bastion-ssh-key) below creates and
  authorizes it.
- An OpenShift pull secret file (for the OpenShift lab) stored **outside** the
  input tree. The Ceph lab needs different credentials, listed in its guide.
- A free machine network on the libvirt host with room for the node IP(s) and the
  virtual IPs the lab declares.
- Host capacity for the lab you pick: the OpenShift SNO lab needs 8 vCPU, ~23 GiB
  free RAM, and 120 GiB of disk for its single node; the Ceph lab needs ~5 vCPU,
  ~10 GiB RAM, and ~104 GiB of disk across its three VMs.
- Outbound reachability from the bastion and nodes to the upstreams your install
  uses (release images, RHSM/CDN, Ceph repos, add-on registries). Behind a
  corporate proxy or firewall, allowlist the hosts in the
  [egress allowlist](../advanced/disconnected-proxy.md#egress-allowlist) first.

Bootwright desired state is safe to commit: it names secrets, but never stores
their bytes. Keep pull secrets, private keys, kubeconfigs, tokens, and passwords
out of the YAML; you load them into the local bootwright context separately. See
[Secrets](../concepts/secrets.md) for the full model.

## Install The CLI

This page is the single source for installing the CLI; the lab guides assume you
have followed one of the two options below.

### Option A — release tarball (Linux x86_64)

Releases publish one artifact, `bootwright_<tag>_linux_amd64.tar.gz`, with a
sibling `.sha256`. There is no bare-binary asset and no second platform. Download
both from [GitHub Releases](https://github.com/crmarques/bootwright/releases),
verify, extract, and install:

```bash
sha256sum -c bootwright_<tag>_linux_amd64.tar.gz.sha256
tar -xzf bootwright_<tag>_linux_amd64.tar.gz
sudo install -m 0755 bootwright_<tag>_linux_amd64/bootwright /usr/local/bin/bootwright
bootwright version
```

### Option B — build from source

`v1alpha1` is still moving, so a released binary can reject desired state written
against a newer `main` — build from source when you are following an example tree
from `main`:

```bash
git clone https://github.com/crmarques/bootwright.git
cd bootwright
make build
sudo install -m 0755 bin/bootwright /usr/local/bin/bootwright
bootwright version
```

The repository's [`Containerfile`](https://github.com/crmarques/bootwright/blob/main/Containerfile)
builds the same binary in a container when you would rather not install a Go
toolchain; the [README](https://github.com/crmarques/bootwright/blob/main/README.md#install-the-cli)
carries that recipe, including the proxy-aware build.

### Bash completion (optional)

```bash
mkdir -p "${HOME}/.local/share/bash-completion/completions"
bootwright completion bash > "${HOME}/.local/share/bash-completion/completions/bootwright"
source "${HOME}/.local/share/bash-completion/completions/bootwright"
```

## Create The Bastion SSH Key

This is the one setup step you run yourself; everything under
[The Setup Mechanics](#the-setup-mechanics) is run for you by the lab guides.

The lab trees declare the bastion connection as a `file:`-sourced `Secret` named
`bastion-host-ssh` in `secrets.yaml`, pointing at `~/.ssh/bootwright-ssh-key`.
Create that key pair:

```bash
ssh-keygen -t ed25519 -N '' -f ~/.ssh/bootwright-ssh-key
```

When the bastion is a **remote** service host, Bootwright reaches it over SSH
with that key as the account in the machine's `access.ssh.user` — `root` when the
machine declares none. Authorize the public key for that account, at the address
the machine declares under `spec.addresses`:

```bash
ssh-copy-id -i ~/.ssh/bootwright-ssh-key.pub root@<the bastion's ssh address>
```

When the bastion **is** the machine you are running on — its `ssh` address is
`localhost`, its own hostname, or one of its own interface IPs — Bootwright uses
a local connection and never uses the key for that hop. The file must still
exist, because `secret generate` imports it.

To keep the key elsewhere, point the `bastion-host-ssh` entry in `secrets.yaml`
at your path instead.

## The Setup Mechanics

Both cluster guides obtain an input tree — `bootwright example init` scaffolds
one for either kind (`--kind container-cluster`, the default, or
`--kind storage-cluster`) — then run the same setup sequence against it. Those
steps are explained once here; the guides simply run them.

After any command, `bootwright status` reports readiness and prints the suggested
next command — lean on it as you work.

### Contexts

A context binds a name to a **copy** of your input directory and stores generated
state under `/var/lib/bootwright/contexts/<context>`:

```bash
bootwright context init --name lab -f ./my-lab
bootwright context update --name lab -f ./my-lab   # after editing the source
```

[The desired-state model](../concepts/index.md#contexts) owns the model — what
`init` copies, what `update` preserves, and where each piece of state lives. Two
practical notes it does not carry:

- `bootwright context current --short` prints only the context name, for
  scripting; the bare `context current` prints the full detail block.
- Remove a context you no longer need with
  `bootwright context delete --name lab --purge`. `--purge` is not optional —
  without it the command exits 1. Destroy any resources the context still owns
  first: `--abandon-resources` deletes it anyway, leaving running infrastructure
  behind and discarding its ownership records and install-captured credentials
  (kubeconfigs, kubeadmin password). It is deliberately not spelled `--force`:
  that word would read as *destroy these resources*, while this flag means
  *walk away from them*, and one spelling for two opposite outcomes is a
  foot-gun. No Bootwright verb has a `--force` flag — destructive risks are
  named one at a time with `--authorize` (see
  [Operations](../advanced/operations.md#the-two-axes-intent-and-authorization)).

### Secrets

Every `secret` verb operates on the *current* context — run `context init` (or
`context use`) first; `bootwright context current` confirms which context you
are about to write to.

The YAML names the secrets the workflow needs; the bytes live in the encrypted
context store. You supply operator-owned material with `secret set`, then let
`secret generate` converge the generated and `file:`-sourced entries:

```bash
bootwright secret set --name <secret-name> ...   # operator-supplied bytes
bootwright secret generate                        # generate + import file: sources
bootwright secret check                           # report any declared secret still missing
bootwright secret list                            # show declared secrets and their state
```

`secret generate` creates the generated key pairs and credentials a tree
declares and brings any `file:` material into the context. `secret check` then
reports any declared secret still missing (exiting non-zero if so). Each guide
lists the exact `secret set` invocations its tree needs. Context-local secret
material is encrypted at rest. See [Secrets](../concepts/secrets.md) for the
declaration syntax (`generated`, `file:`, operator-set) and the supported value
flags.

### Host trust

Bootwright uses strict SSH host-key checking for non-local durable machines.
Record trust for the declared hosts before you run anything that connects:

```bash
bootwright machine trust
bootwright status
```

Interactive `preflight` and `apply` can prompt to record a host key on first use,
but never under `--yes`, `--dry-run`, or JSON output, and a *changed* key is
never accepted automatically. Running `bootwright machine trust` first keeps later
runs unattended and fail-closed-safe.

### Bastion prep, then the read-only checks

`bastion setup` installs tooling **on the bastion host only** — the Python
virtualenv, the managed Ansible runtime, and the cluster CLIs — and changes
nothing on your machines or clusters:

```bash
bootwright bastion setup --yes
```

Then run the preflight checks, materialize the normalized desired state, and
preview the task graph. None of these mutate your hosts or clusters:

```bash
bootwright preflight all
bootwright render effective
bootwright plan
```

Together with the context, secret, and trust steps above, this is the whole
pre-apply ritual, in the order the guides run it:

| Command | Purpose | What it touches |
| --- | --- | --- |
| `validate` | Checks the input tree before any context exists. | nothing — offline |
| `context init` / `context update` | Imports (or refreshes) the input tree into the named context. | the local context store |
| `secret check` | Reports any declared secret still missing. | the local context store, read-only |
| `machine trust` | Records the declared hosts' SSH host keys for strict checking. | reads host keys over the network; writes the local trust store |
| `bastion setup` | Installs the managed Ansible runtime, and — when the context declares an OpenShift/OKD release — the release-matched cluster CLIs and `helm`. | the bastion host only |
| `preflight all` | Checks the bastion, infrastructure hosts, and cluster prerequisites before any mutation. | reads hosts over SSH |
| `render effective` | Writes the desired state with all defaults applied, so you can see exactly what will be acted on. | nothing — renders locally |
| `plan` | Shows the apply task graph. | nothing — offline |
| `diff` (after a first apply exists) | Reports drift from desired state; exit code `3` means out of sync. | reads the live cluster |

## Next steps

With the CLI installed, follow one of the cluster guides:

- [Provisioning an OpenShift cluster](openshift.md)
- [Provisioning a Ceph cluster](ceph.md)

Wiring Bootwright into CI — exit codes, JSON output, unattended runs:
[Automation and CI](../advanced/automation.md).

If a step fails, see [Troubleshooting](../troubleshooting.md) for validation, SSH
trust, artifact fetch, and apply diff issues.
