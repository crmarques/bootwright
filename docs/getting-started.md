---
title: Getting Started
description: Import, validate, and converge a Bootwright context.
---

# Getting Started

Bootwright runs from a named context. The context points at the desired-state
YAML you edited for your environment and stores local state, generated runtime
files, and secret material outside the repo.

## 0. Install The CLI

Download the `bootwright` binary for your platform from
[GitHub Releases](https://github.com/crmarques/bootwright/releases), then
install it on your `PATH`:

```bash
chmod +x bootwright
sudo install -m 0755 bootwright /usr/local/bin/bootwright
bootwright version
```

If a release binary is not available yet, build the CLI from the repository
root with the
[`Containerfile`](https://github.com/crmarques/bootwright/blob/main/Containerfile)
and copy the binary out of the image:

```bash
export HTTP_PROXY
export HTTPS_PROXY="${HTTP_PROXY}"
export NO_PROXY="localhost,127.0.0.1,.local,10."
export http_proxy="${HTTP_PROXY}"
export https_proxy="${HTTPS_PROXY}"
export no_proxy="${NO_PROXY}"

DOCKER_BUILDKIT=1 docker build \
  --build-arg "HTTP_PROXY=${HTTP_PROXY}" \
  --build-arg "HTTPS_PROXY=${HTTPS_PROXY}" \
  --build-arg "NO_PROXY=${NO_PROXY}" \
  --build-arg "http_proxy=${http_proxy}" \
  --build-arg "https_proxy=${https_proxy}" \
  --build-arg "no_proxy=${no_proxy}" \
  -t bootwright \
  -f Containerfile \
  .

docker create --name bootwright-bin bootwright
docker cp bootwright-bin:/usr/local/bin/bootwright ./bootwright
docker rm bootwright-bin
sudo install -m 0755 ./bootwright /usr/local/bin/bootwright
bootwright version
```

## 1. Prepare Input Files

Start from an example, copy it to a working directory, then edit the copy for
your environment:

```text
ls -l <input-dir>
```

Canonical input examples live under
[`examples/`](https://github.com/crmarques/bootwright/tree/main/examples).
Use `libvirt-redfish-fleet` for a lab with emulated Redfish BMCs, or
`baremetal-redfish-fleet` for real bare-metal hosts with Redfish virtual media.

The copied directory should contain the six desired-state kinds:

```text
environment.yaml       Environment
hosts.yaml             Host
provider.yaml          InfraProvider
networks.yaml          NetworkConfig
cluster-infra.yaml     ClusterInfra
container-cluster.yaml ContainerCluster
```

Edit these first:

- `Environment.spec.baseDomain` and `Environment.spec.secrets`.
- `Host.spec.addresses[]` and SSH key references for provider/service hosts.
- Physical MACs, BMC addresses, or virtual machine profiles in `provider.yaml`.
- Machine CIDRs and NMState templates in `networks.yaml`.
- Endpoint VIP ownership and per-machine IP overlays in `cluster-infra.yaml`.
- OpenShift or OKD release, install mode, and node bindings in
  `container-cluster.yaml`.

Provider swaps should leave `Environment` and `ContainerCluster` unchanged
unless the cluster intent itself changes.

## 2. Verify SSH Access

Bootwright uses SSH to reach provider and service hosts. Test the same key and
user before importing the context:

```text
ssh -i "${HOME}/.ssh/bootwright-ssh-key" -o StrictHostKeyChecking=accept-new "${USER}@localhost" true
ssh -i "${HOME}/.ssh/bootwright-ssh-key" -o StrictHostKeyChecking=accept-new "${USER}@${BASTION_ADDRESS}" true
```

Run the first command when the provider host is local. Run the second when the
provider or artifact publisher is reached through a bastion address.

## 3. Import A Context

Create the context from the edited directory:

```text
bootwright context init ocp-nprd-01 -f <input-dir>
bootwright context validate
bootwright context current
```

By default, Bootwright writes the context under
`~/bootwright/<context-name>/`. The imported authoring copy lives at
`<base-dir>/input-files/`.

Re-run `context init` with `--yes` after changing the source input directory
and wanting to replace the imported copy.

## 4. Set Secrets

Desired-state YAML names secrets; secret bytes stay in the local context
secrets directory. Run the commands that match the names declared in
`Environment.spec.secrets`:

```text
bootwright secret set openshift-pull-secret --pull-secret "${HOME}/openshift-pull-secret.json"
bootwright secret set proxy-credentials --username "${PROXY_USER}" --password "${PROXY_PASS}"
bootwright secret set bmc-credentials --username "${BMC_USER}" --password "${BMC_PASS}"
bootwright secret list
```

Get the OpenShift pull secret from
`https://console.redhat.com/openshift/install/pull-secret`. Prefer
`--password-stdin` for credentials on shared shells; `--password` is useful
when credentials already come from protected environment variables.

If your desired state declares `generated:` secrets, materialize them with:

```text
bootwright secret generate
```

## 5. Export Runtime Environment

```text
eval "$(bootwright print-env --sensitive)"
```

`print-env` exports the active context paths and any configured proxy
environment. `--sensitive` is required when proxy credentials would be printed.

## 6. Check And Apply

```text
bootwright check bastion
bootwright apply bastion --yes
bootwright check infra
bootwright apply infra --yes
bootwright check cluster
bootwright apply cluster --yes
bootwright status
```

`apply bastion` installs controller-side prerequisites. `apply infra`
converges provider hosts, substrate state, and managed infra components.
`apply cluster` creates the agent ISO, boots every declared node, and waits for
`openshift-install agent wait-for install-complete`.

Use `bootwright status --watch` while an apply is running. A new apply is
blocked while the previous apply ledger is still active.

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
`agent-config.yaml` with secret material inlined. Keep that directory local and
remove it when you no longer need the files.

## Optional Cleanup

Remove only the generated artifact HTTPS service used for BMC ISO fetches:

```text
bootwright destroy infra --scope http-server --yes
```

This does not destroy cluster nodes or the rest of the infrastructure.

## Output Boundaries

- Authored YAML lives under `<base-dir>/input-files/`.
- Placeholder installer output lives under
  `<base-dir>/state/installer/<cluster>/`.
- Secret-inlined runtime installer output lives under
  `<base-dir>/state/runtime/<cluster>/installer/`.
- `render --output-dir <dir> --sensitive` writes secret-inlined external tool
  inputs under the requested directory; keep it local and unversioned.

## Add The Cluster Kube Context

After `apply cluster` completes, merge the generated admin kubeconfig into your
user kubeconfig:

```text
export CLUSTER=<cluster-name>
export SRC_KUBECONFIG="${BOOTWRIGHT_STATE_DIR}/runtime/${CLUSTER}/installer/auth/kubeconfig"
export TMP_KUBECONFIG="${TMPDIR:-/tmp}/bootwright-${CLUSTER}.kubeconfig"
export TMP_MERGED="${TMPDIR:-/tmp}/bootwright-merged-kubeconfig"

mkdir -p "${HOME}/.kube"
touch "${HOME}/.kube/config"
chmod 0600 "${HOME}/.kube/config"

cp "${SRC_KUBECONFIG}" "${TMP_KUBECONFIG}"
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
