# Common Steps

Identical for every case once you have a bastion, an active context, and the
desired-state files edited. The e2e README drops you here after context input
customization. Return there at the end for bastion-side teardown.

These steps assume the env vars from the bastion doc are exported:
`$CASE`, `$BOOTWRIGHT_BASE_DIR`, `$BOOTWRIGHT_INPUT_DIR`,
`$BOOTWRIGHT_STATE_DIR`, and `$BOOTWRIGHT_SECRETS_DIR`.
If `print-env` was blocked because proxy credentials would be printed,
run the `proxy-credentials` `secret set` command below first, then export the
vars with `bootwright print-env --sensitive`.

## 1. Save And Generate Secrets

The case context input references four or five secrets through
`environment.yaml` `spec.secrets:`:

| Secret | Form | Required for |
| --- | --- | --- |
| `cluster-admin-pub-key` | `~/.ssh/bootwright-ssh-key.pub` | OpenShift cluster-admin SSH access |
| `provider-host-ssh` | `~/.ssh/bootwright-ssh-key` | Bastion→host SSH |
| `openshift-pull-secret` | Context-local, set from the pull-secret JSON | `render installer`, `apply cluster` |
| `proxy-credentials` (optional) | Context-local generated or set — see [proxy.md](proxy.md) | `apply bastion`, install-config proxy block |
| `bmc-credentials` | Context-local generated or set | `apply infra`, `apply cluster` |

Confirm the SSH key pair, then set the pull secret:

```bash
test -r ~/.ssh/bootwright-ssh-key
test -r ~/.ssh/bootwright-ssh-key.pub

bootwright secret set openshift-pull-secret \
  --pull-secret "$HOME/pull-secret.json"
```

For the containerized bastion the pull secret is mounted at
`$HOME/pull-secret.json`. For a host bastion, point `--pull-secret` at
wherever you placed the JSON (typically
`$BOOTWRIGHT_SECRETS_DIR/openshift-pull-secret`).

If you want to provide `proxy-credentials` instead of generating them, write
them now:

```bash
bootwright secret set proxy-credentials \
  --username <proxy-user> \
  --password-stdin
```

Materialize every remaining `generated:` entry:

```bash
bootwright secret generate
bootwright secret list
find "$BOOTWRIGHT_SECRETS_DIR" -maxdepth 1 -type f -printf '%f\n' | sort
```

## 2. Apply The Context To The Bastion

Installs release-specific OpenShift CLIs declared by the context input.
`apply bastion` strips ambient proxy variables and uses
`Environment.spec.proxy` from desired state.

```bash
bootwright check bastion
bootwright apply bastion --yes
```

Managed Squid is **not** running yet at this point — the bastion phase
runs before `apply infra` provisions it. Bootwright deliberately ignores
`spec.proxy` for this phase; every later phase routes through Squid once
infra is up. See [proxy.md](proxy.md) for the full bootstrap order.

## 3. Provision The Infrastructure

Run a read-only check, then converge the `InfraProvider`
capabilities the cluster references plus the per-cluster substrate:
machine infrastructure, the managed load balancer (see
[load-balancer.md](load-balancer.md)), name resolution, the
machine-control integration (Redfish for bare metal or libvirt with emulated
Redfish), and
managed Squid (see [proxy.md](proxy.md)) when declared. The case's
`provider.yaml` capabilities and `cluster-infra.yaml`'s
`components.<X>.from` references together decide which of these
apply.

```bash
bootwright check infra
bootwright apply infra --dry-run
bootwright apply infra --yes
bootwright check infra
```

If the provider integration drives Ansible over SSH (libvirt or bare metal)
and the target SSH user has passwordless sudo, add `--ask-become-pass=false`
to the two `apply` commands.

## 4. Install The Cluster

`render installer` writes `install-config.yaml` and `agent-config.yaml`
under `$BOOTWRIGHT_STATE_DIR/installer/<cluster>/` with
placeholder strings in place of pull secret, SSH key, and trust bundle.
These files are generated and can be regenerated.

```bash
bootwright check cluster
bootwright render installer

bootwright apply cluster --dry-run
bootwright apply cluster --yes
```

`apply cluster` materializes
`runtime/<cluster>/installer/{install,agent}-config.yaml` with secret
material inlined (mode `0600`) — the form `openshift-install` consumes.
The runtime tree never leaves local state. It then renders the agent
installer assets, boots every cluster node through the provider's
machine-control path (Redfish virtual media for bare metal, or emulated
Redfish on libvirt), and waits for
`openshift-install agent wait-for install-complete`.

To see the final form `openshift-install` will consume, re-run `render
installer` with `--sensitive`:

```bash
bootwright render installer --sensitive
```

That writes the runtime copies eagerly so you can review them. It is
**not** required for the install — `apply cluster` regenerates the
same runtime copies on its own. Skip it when you want the rendered
files to stay free of secret material.

### Following The Install Logs

`bootwright apply cluster` is one long-running command. Its Ansible output
streams to the foreground terminal; the `openshift-install agent
wait-for install-complete` phase that gates the run writes a richer log
to disk. Open a second shell on the bastion to follow it:

```bash
export CLUSTER=<ContainerCluster.metadata.name>
tail -f "$BOOTWRIGHT_STATE_DIR/runtime/$CLUSTER/installer/.openshift_install.log"
```

For node-side visibility, SSH to a booted control plane. The node IPs are the
per-machine overlays in `cluster-infra.yaml` under
`spec.components.machines[].networkConfig.addresses[]`. Then watch the agent
or bootkube journals:

```bash
ssh -i ~/.ssh/bootwright-ssh-key core@<node-ip> \
  sudo journalctl -fu assisted-service.service
# or, after bootstrap kicks off:
ssh -i ~/.ssh/bootwright-ssh-key core@<node-ip> \
  sudo journalctl -fu bootkube.service
```

## 5. Verify

```bash
export CLUSTER=<ContainerCluster.metadata.name>
export KUBECONFIG="$BOOTWRIGHT_STATE_DIR/runtime/$CLUSTER/installer/auth/kubeconfig"

oc get nodes
oc get clusterversion
oc get clusteroperators

bootwright check infra
bootwright check cluster
```

The case README lists the per-case expectation for `oc get nodes`
(typically one `Ready` node for SNO, three for the 3-node case).

To keep the cluster in the user's default kube contexts, merge the generated
kubeconfig after the install:

```bash
export SRC_KUBECONFIG="$BOOTWRIGHT_STATE_DIR/runtime/$CLUSTER/installer/auth/kubeconfig"
export TMP_KUBECONFIG="${TMPDIR:-/tmp}/bootwright-$CLUSTER.kubeconfig"
export TMP_MERGED="${TMPDIR:-/tmp}/bootwright-merged-kubeconfig"

mkdir -p "$HOME/.kube"
touch "$HOME/.kube/config"
chmod 0600 "$HOME/.kube/config"

cp "$SRC_KUBECONFIG" "$TMP_KUBECONFIG"
CTX=$(oc --kubeconfig "$TMP_KUBECONFIG" config current-context)
oc --kubeconfig "$TMP_KUBECONFIG" config rename-context "$CTX" "$CLUSTER-admin"
KUBECONFIG="$HOME/.kube/config:$TMP_KUBECONFIG" oc config view --flatten > "$TMP_MERGED"
install -m 0600 "$TMP_MERGED" "$HOME/.kube/config"
oc config use-context "$CLUSTER-admin"
```

## 6. Tear Down The Cluster

```bash
bootwright destroy cluster --yes
bootwright destroy infra --yes
```

After this, return to the bastion doc you used:

- [bastion.md](bastion.md#tear-down--bastion-state) — clean per-case state
  dirs on the bastion.
- [containerized-bastion.md](containerized-bastion.md#tear-down--container-and-host-state)
  — remove the container and per-case host state.
