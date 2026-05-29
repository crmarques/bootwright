---
title: Getting Started
description: Import, validate, and converge a Bootwright context.
---

# Getting Started

Bootwright runs from a named context on the bastion host where you invoke the
CLI. The context points at the desired-state YAML you edited for your
environment and stores local state, generated runtime files, and secret
material outside the repo under `/var/lib/bootwright`.

## 0. Install The CLI

Download the `bootwright` binary for your platform from
[GitHub Releases](https://github.com/crmarques/bootwright/releases), then
install it on your `PATH`:

```bash
chmod +x bootwright
sudo install -m 0755 bootwright /usr/local/bin/bootwright
bootwright version
```

Configure Bash completion for your user shell:

```bash
mkdir -p "${HOME}/.local/share/bash-completion/completions"
bootwright completion bash > "${HOME}/.local/share/bash-completion/completions/bootwright"
source "${HOME}/.local/share/bash-completion/completions/bootwright"
```

## 1. Prepare Input Files

Start from a generated example or an in-repo example copy, then edit the
working directory for your environment:

```text
bootwright example init my-sno-lab --output ./my-sno-lab
```

When working from a fresh checkout, copying the canonical example is
equivalent:

```text
cp -a examples/sno-libvirt-redfish ./my-sno-lab
ls -l ./my-sno-lab
```

Canonical input examples live under
[`examples/`](https://github.com/crmarques/bootwright/tree/main/examples).
Use `sno-libvirt-redfish` for the smallest single-node lab with emulated
Redfish BMCs. Use `libvirt-redfish-fleet` for a compact three-node lab,
`baremetal-redfish` for real bare-metal hosts with Redfish virtual media, or
`baremetal-redfish-fleet` for a two-cluster bare-metal input layout.
`baremetal-redfish-postinstall` adds declarative OpenShift Virtualization
bootstrap resources. `baremetal-redfish-virtualized-child` adds a KubeVirt
child OpenShift cluster hosted on that parent virtualization layer.

The copied directory contains desired-state files for the relevant kinds:

```text
environment.yaml       Environment
hosts.yaml             Host
provider.yaml          InfraProvider
infra-component.yaml   InfraComponent shared infra services
networks.yaml          NetworkConfig
cluster-infra.yaml     ClusterInfra
container-cluster.yaml ContainerCluster
cluster-extension-*.yaml optional ClusterExtension resources
```

Edit these first:

- `Environment.spec.baseDomain` and `Environment.spec.secrets`.
- `Environment.spec.containerClusters[]` if the environment should select only
  part of the loaded fleet.
- `Environment.spec.infraComponents.*` and `proxyFor` when the lab uses
  external or managed proxy, DNS, artifact, registry, or NTP services.
- `Host.spec.addresses[]` and SSH key references for provider/service hosts.
- Physical MACs, BMC addresses, or virtual machine profiles in `provider.yaml`.
- Machine CIDRs and NMState templates in `networks.yaml`.
- Endpoint VIP ownership and per-machine IP overlays in `cluster-infra.yaml`.
- OpenShift or OKD release, install mode, and node bindings in
  `container-cluster.yaml`.

Provider swaps should leave `Environment` and `ContainerCluster` unchanged
unless the cluster intent itself changes.

Before importing a context, confirm the out-of-band inputs exist:

- The bastion host can use `sudo` for `/var/lib/bootwright`.
- SSH from the bastion reaches every `Host` address used by provider or
  service actions.
- The OpenShift pull secret is available outside the repo.
- Generated or supplied BMC, proxy, and mirror secrets are planned in
  `Environment.spec.secrets`.
- Provider host tooling and permissions are available for the selected
  substrate.
- BMCs can reach the artifact endpoint selected for Redfish virtual media.
- DNS, VIPs, and load balancer addresses are reachable from the bastion and
  the cluster nodes.
- Mirror CA files and other trust bundles are local, unversioned files.
- Real BMC TLS posture is understood before using
  `disableCertificateVerification: true`.

Validate the edited YAML before it is imported into `/var/lib/bootwright`:

```text
bootwright check syntax -f ./my-sno-lab
```

## 2. Verify SSH Access

Bootwright uses SSH to reach provider and service hosts. Test the same key,
user, and Host address values before importing the context:

```text
ssh -i "${HOME}/.ssh/bootwright-ssh-key" -o StrictHostKeyChecking=accept-new "${USER}@${HOST_ADDRESS}" true
```

Use the exact address you declare in `Host.spec.addresses[]` for each provider
or service host.

## 3. Import A Context

Create the context from the edited directory:

```text
bootwright check syntax -f ./my-sno-lab
bootwright context init lab -f ./my-sno-lab
bootwright context validate
bootwright context current
bootwright secret list
```

Bootwright records only the selected context names in
`~/.bootwright/contexts.yaml`. Context data lives under
`/var/lib/bootwright/contexts/<context-name>/`, and the imported authoring copy
lives at `input/` inside that directory.

Re-run `context init` with `--yes` to replace the entire context directory, or
use `bootwright context update lab -f <input-dir>` to replace only
`input/` while preserving secrets, rendered output, runtime data, run history,
and managed host/service files.

## 4. Set Secrets

Desired-state YAML names secrets; secret bytes stay in the local context
secrets directory. Run the commands that match the names declared in
`Environment.spec.secrets`:

```text
bootwright secret set openshift-pull-secret --pull-secret "${HOME}/openshift-pull-secret.json"
bootwright secret generate
bootwright secret materialize
bootwright secret list
```

Get the OpenShift pull secret from
`https://console.redhat.com/openshift/install/pull-secret`. Prefer
`--password-stdin` for credentials on shared shells; `--password` is useful
when credentials already come from protected environment variables.

For non-generated credentials declared by your desired state, use `secret set`
with a protected input source:

```text
printf '%s\n' "${BMC_PASS}" | bootwright secret set bmc-credentials --username "${BMC_USER}" --password-stdin
printf '%s\n' "${PROXY_PASS}" | bootwright secret set proxy-credentials --username "${PROXY_USER}" --password-stdin
```

## 5. Export Runtime Environment

```text
eval "$(bootwright print-env --sensitive)"
```

`print-env` exports `BOOTWRIGHT_CONTEXT` and any configured proxy environment.
`--sensitive` is required when proxy credentials would be printed.

## 6. Check And Apply

```text
bootwright check syntax
bootwright check bastion
bootwright apply bastion --yes
bootwright check infra
bootwright apply infra --dry-run
bootwright apply infra --yes
bootwright check cluster
bootwright apply cluster --dry-run
bootwright apply cluster --yes
bootwright check extensions
bootwright apply extensions --dry-run
bootwright apply extensions --yes
bootwright status
```

`apply bastion` installs bastion-host prerequisites. `apply infra`
converges provider hosts, substrate state, and managed infra components.
`apply cluster` creates the agent ISO, boots every declared node, and waits for
`openshift-install agent wait-for install-complete`, then applies bound
post-install extensions.
`apply extensions` uses the installed cluster kubeconfig and `oc apply` for
post-install bootstrap components declared as `ClusterExtension` resources.
`apply all` includes infrastructure before the same cluster and extension
phases.
For KubeVirt child clusters, `apply all` also waits for the parent cluster
install and its `provides: [kubevirt]` extension before creating child VM
infrastructure. `apply infra --scope <child>` requires that parent cluster to
already be installed and KubeVirt-ready; scoped child applies do not install the
parent implicitly.
Running `apply cluster --yes` again skips cluster install tasks when the prior
install record, rendered desired-input fingerprint, and kubeconfig availability
probe all match, then applies extensions idempotently. If an interrupted apply
already booted nodes, the next apply resumes at the install wait phase instead
of recreating the ISO or rebooting machines.

Use `bootwright status --watch` while an apply is running. A new apply is
blocked while the previous apply ledger has a fresh process lease. If an
interrupted process leaves only a stale ledger, the next `apply` or `destroy`
marks it cancelled before continuing.

Stable JSON output is intentionally limited. Use these forms for automation:

| Command | JSON support | Behavior |
| --- | --- | --- |
| `bootwright check syntax -f <input-dir> --output json` | Supported | Pre-import diagnostics |
| `bootwright check syntax --output json` | Supported | Read-only diagnostics |
| `bootwright check infra --dry-run --output json` | Supported | Dry-run preflight plan |
| `bootwright check cluster --dry-run --output json` | Supported | Dry-run preflight plan |
| `bootwright render installer --output json` | Supported | Writes context render output |
| `bootwright cluster list --output json` | Supported | Read-only cluster access status |
| `bootwright cluster access --output json` | Supported | Read-only cluster access inventory |
| `bootwright secret list --output json` | Supported | Read-only secret status |
| `bootwright status --output json` | Supported | Read-only context status |
| `bootwright apply infra --dry-run --output json` | Supported | Dry-run apply plan |
| `bootwright apply cluster --dry-run --output json` | Supported | Dry-run apply plan |
| `bootwright apply extensions --dry-run --output json` | Supported | Dry-run extension apply plan |
| `bootwright apply all --dry-run --output json` | Supported | Dry-run apply plan |
| `bootwright destroy infra --dry-run --output json` | Supported | Dry-run destroy plan |
| `bootwright destroy cluster --dry-run --output json` | Supported | Dry-run destroy plan |
| `bootwright apply ... --yes` | Not JSON | Mutates selected scope |
| `bootwright destroy ... --yes` | Not JSON | Destroys selected scope |

For mutating automation, run the apply or destroy command with `--yes`, then
poll `bootwright status --output json` or `bootwright status --watch`.

## Export External CLI Inputs

Render placeholder installer files into context state:

```text
bootwright render installer --scope <cluster-name>
```

To run `openshift-install` or Ansible-facing CLIs yourself, export concrete
tool inputs to a local, unversioned directory:

```text
bootwright render --output-dir ./rendered --scope <cluster-name> --sensitive
openshift-install agent create image --dir ./rendered/openshift-install/<cluster-name>
openshift-install agent wait-for install-complete --dir ./rendered/openshift-install/<cluster-name> --log-level info
```

The OpenShift installer inputs are written under
`./rendered/openshift-install/<cluster-name>/` as `install-config.yaml` and
`agent-config.yaml`, plus optional `openshift/` manifests, with secret material
inlined. Keep that directory local and remove it when you no longer need the
files.

## Optional Cleanup

Remove only the generated artifact publication service used for BMC ISO
fetches, including HTTPS listeners when the selected artifact server exposes
them:

```text
bootwright destroy infra --scope artifact-server --yes
```

This does not destroy cluster nodes or the rest of the infrastructure.

## Output Boundaries

- Authored YAML lives under
  `/var/lib/bootwright/contexts/<context>/input/`.
- Placeholder installer output lives under
  `/var/lib/bootwright/contexts/<context>/rendered/installer/<cluster>/`.
- Secret-inlined runtime installer output lives under
  `/var/lib/bootwright/contexts/<context>/runtime/installer/<cluster>/`.
- `render --output-dir <dir> --sensitive` writes secret-inlined external tool
  inputs under the requested directory; keep it local and unversioned.

## Add The Cluster Kube Context

After `apply cluster` completes, merge the generated admin kubeconfig into your
user kubeconfig:

```text
export CLUSTER=<cluster-name>
export BOOTWRIGHT_CONTEXT="$(bootwright context current --short)"
export SRC_KUBECONFIG="/var/lib/bootwright/contexts/${BOOTWRIGHT_CONTEXT}/runtime/installer/${CLUSTER}/auth/kubeconfig"
export TMP_KUBECONFIG="${TMPDIR:-/tmp}/bootwright-${CLUSTER}.kubeconfig"
export TMP_MERGED="${TMPDIR:-/tmp}/bootwright-merged-kubeconfig"

mkdir -p "${HOME}/.kube"
touch "${HOME}/.kube/config"
chmod 0600 "${HOME}/.kube/config"

sudo install -m 0600 -o "$(id -u)" -g "$(id -g)" \
  "${SRC_KUBECONFIG}" "${TMP_KUBECONFIG}"
CTX="$(oc --kubeconfig "${TMP_KUBECONFIG}" config current-context)"
oc --kubeconfig "${TMP_KUBECONFIG}" config rename-context "${CTX}" "${CLUSTER}-admin"
KUBECONFIG="${HOME}/.kube/config:${TMP_KUBECONFIG}" oc config view --flatten > "${TMP_MERGED}"
install -m 0600 "${TMP_MERGED}" "${HOME}/.kube/config"
oc config use-context "${CLUSTER}-admin"
```

Use a unique context name per cluster, such as `<cluster-name>-admin`, to avoid
overwriting another installer-generated `admin` context.

The schema reference is in
[`specs/state-model.md`](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md).
