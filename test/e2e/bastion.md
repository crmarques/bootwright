# Bastion Setup (VM Or Host)

Run `bootwright` directly on a Linux VM or physical host. For the
non-root-container alternative, see
[containerized-bastion.md](containerized-bastion.md). The e2e README
points here when the operator chooses the host-bastion mode.

## Requirements

The bastion is a Linux machine you SSH into and run `bootwright` from. It must
be able to reach every provider and service host the case declares over SSH.
Use the declared `Host.spec.ssh.addressName` addresses for those hosts.

- A non-root user with `sudo`.
- `bin/bootwright` available in `$PATH`.
- An OpenShift pull secret JSON available on the bastion.
- A passwordless SSH key pair authorized on every provider host.
- The case YAML files reachable on the bastion (see
  [Get The Case YAMLs Onto The Bastion](#get-the-case-yamls-onto-the-bastion)).

If you build `bin/bootwright` on a workstation, copy it across with the pull
secret JSON:

```bash
scp bin/bootwright <bastion-user>@<bastion-host>:/usr/local/bin/bootwright
scp ~/.bootwright/secrets/openshift-pull-secret \
  <bastion-user>@<bastion-host>:/home/<bastion-user>/pull-secret.json
```

Adjust destination paths to whatever the bastion user can read.

## On The Bastion

SSH in and verify the basics:

```bash
ssh <bastion-user>@<bastion-host>

command -v bootwright
sudo -v
test -s ~/pull-secret.json
```

Generate (or reuse) the SSH key the bastion will use to reach the provider
host(s). Cluster admin SSH keys are generated into the Bootwright context
secrets directory by `bootwright secret generate`.

```bash
install -d -m 0700 ~/.ssh
test -f ~/.ssh/bootwright-ssh-key || \
  ssh-keygen -t ed25519 -f ~/.ssh/bootwright-ssh-key -N '' -C bootwright-bastion
```

Push the public key to every provider host listed in `provider.yaml`:

```bash
ssh-copy-id -i ~/.ssh/bootwright-ssh-key.pub <provider-user>@<provider-host>
ssh -i ~/.ssh/bootwright-ssh-key -o StrictHostKeyChecking=accept-new \
  <provider-user>@<provider-host> true
```

When the bastion is also the provider host in a reference case, use the address
that the case declares for that Host:

```bash
touch ~/.ssh/authorized_keys
chmod 0600 ~/.ssh/authorized_keys
grep -qxF "$(cat ~/.ssh/bootwright-ssh-key.pub)" ~/.ssh/authorized_keys || \
  cat ~/.ssh/bootwright-ssh-key.pub >> ~/.ssh/authorized_keys
ssh -i ~/.ssh/bootwright-ssh-key -o StrictHostKeyChecking=accept-new "$USER"@localhost true
```

## Get The Case YAMLs Onto The Bastion

The e2e README's context step imports the per-kind YAML files from
`test/e2e/<case>/` (typically `environment.yaml`,
`hosts.yaml`, `networks.yaml`, `provider.yaml`, optional
`infra-component.yaml`,
`cluster-infra.yaml`, `container-cluster.yaml`).
Put the repo on the bastion so they exist there, then export
the context from that location. Either clone the repo:

```bash
git clone <bootwright-repo-url> ~/bootwright
cd ~/bootwright
```

…or `scp` only the case directory from your workstation:

```bash
scp -r test/e2e/<case> <bastion-user>@<bastion-host>:~/bootwright/test/e2e/<case>
# then on the bastion:
cd ~/bootwright
```

## Optional Proxy Env

Bootwright commands use the proxy selected by
`Environment.spec.proxyFor.bootwright` from the active context, not ambient
shell proxy variables. Use `bootwright print-env` when a shell outside
Bootwright needs the same proxy exports. If proxy credentials would be printed,
rerun it with `--sensitive` after creating the credential secret.

Leave unset for direct internet access. Replace the placeholder URL before
exporting:

```bash
# export HTTP_PROXY=http://proxy.example.test:3128
# export HTTPS_PROXY=http://proxy.example.test:3128
# export NO_PROXY=localhost,127.0.0.1,::1,.bootwright.test
# export http_proxy="$HTTP_PROXY"
# export https_proxy="$HTTPS_PROXY"
# export no_proxy="$NO_PROXY"
```

## Initialize The Context

`$CASE` is the e2e case directory name under `test/e2e/`. Run from the repo path
on the bastion:

```bash
export CASE=<case-directory>
bootwright context init "$CASE" -f "test/e2e/$CASE" --yes
bootwright context validate
eval "$(bootwright print-env)"
```

If `print-env` reports that proxy credentials would be printed, create
the `proxy-credentials` secret in [common-steps.md](common-steps.md) first, then
rerun it with `--sensitive`.

## Bootstrap Bastion Dependencies

The first check is expected to report missing tools on a fresh bastion. The
apply installs the Bootwright-managed Ansible runtime and release-specific
OpenShift CLIs declared by the active context.

```bash
bootwright check bastion || true
bootwright apply bastion --yes
bootwright check bastion || true
```

In an externally proxied environment, create `proxy-credentials` first as shown
in [common-steps.md](common-steps.md), then run the bastion apply.

## Tear Down — Bastion State

After the case-specific cluster destroy steps in
[common-steps.md](common-steps.md), on the bastion:

```bash
export CLUSTER=<ContainerCluster.metadata.name>
sudo rm -rf "/var/lib/bootwright/contexts/$CASE/clusters/$CLUSTER"
```

The bastion machine itself stays — Bootwright does not manage its lifecycle.
