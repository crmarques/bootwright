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
    `status`, `cluster`, `state-check`, `print-env`) still need `sudo` to read the
    context, but they change nothing. To avoid the prompt, run as root or
    pre-authorize once with `sudo -v`.

## What You Need

- A Linux bastion host where you install and run `bootwright`.
- `sudo` on the bastion (prompted or passwordless) for bootwright-managed state
  under `/var/lib/bootwright`.
- Libvirt with `qemu:///system` available on the bastion (the labs treat the
  bastion as the libvirt host).
- An SSH key pair the bastion can use to reach the libvirt/service host.
- An OpenShift pull secret file (for the OpenShift lab) stored **outside** the
  input tree. The Ceph lab needs different credentials, listed in its guide.
- A free machine network on the libvirt host with room for the node IP(s) and the
  virtual IPs the lab declares.

Bootwright desired state is safe to commit: it names secrets, but never stores
their bytes. Keep pull secrets, private keys, kubeconfigs, tokens, and passwords
out of the YAML; you load them into the local bootwright context separately. See
[Secrets](../concepts/secrets.md) for the full model.

## Install The CLI

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

## The Setup Mechanics

Both cluster guides obtain an input tree (OpenShift scaffolds one with
`example init`; Ceph copies an example), then run the same setup sequence against
it. Those steps are explained once here; the guides simply run them.

After any command, `bootwright status` reports readiness and prints the suggested
next command — lean on it as you work.

### Contexts

A context binds a name to a **copy** of your input directory and stores generated
state under `/var/lib/bootwright/contexts/<context>`. `context init` copies the
whole source tree into the context's `input/` directory, so the context is
self-contained and keeps working even if the source is moved or deleted.

```bash
bootwright context init --name lab -f ./my-lab
bootwright context current
bootwright status
```

Because the input is a copy, editing the source later has no effect until you
refresh it:

```bash
bootwright context update --name lab -f ./my-lab
```

`context init` fails if the named context already exists; rerun with `--yes` to
drop the existing context and recreate it from the source. See
[The desired-state model](../concepts/index.md) for contexts, stages, and the
apply modes.

### Secrets

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
bootwright host trust
bootwright status
```

Interactive `preflight` and `apply` can prompt to record a host key on first use,
but never under `--yes`, `--dry-run`, or JSON output, and a *changed* key is
never accepted automatically. Running `bootwright host trust` first keeps later
runs unattended and fail-closed-safe.

### Bastion prep and read-only checks

Install the local tooling bootwright needs, run preflight checks, materialize the
normalized desired state, and preview the task graph. None of these mutate your
hosts or clusters:

```bash
bootwright bastion setup --yes
bootwright preflight all
bootwright render effective
bootwright plan
```

| Command | Purpose |
| --- | --- |
| `bastion setup` | Installs the managed Ansible runtime and the release-matched cluster CLIs on the bastion. |
| `bastion check` | Re-runs the read-only bastion dependency checks (Python, Ansible runtime, `tar`, `sudo`); equivalent to `preflight bastion`. |
| `preflight all` | Checks the bastion, infrastructure hosts, and cluster prerequisites before any mutation. |
| `render effective` | Writes the desired state with all defaults applied, so you can see exactly what will be acted on. |
| `plan` | Shows the apply task graph without touching hosts or clusters. |

## Next steps

With the CLI installed, follow one of the cluster guides:

- [Provisioning an OpenShift cluster](openshift.md)
- [Provisioning a Ceph cluster](ceph.md)

If a step fails, see [Troubleshooting](../troubleshooting.md) for validation, SSH
trust, artifact fetch, and apply state-check issues.
