# Containerized Bastion Setup

Run `bootwright` inside a non-root UBI9 container with `--network host` on the
operator's Linux host. Good for ephemeral, fully isolated e2e runs on a dev
workstation. For the VM/host alternative, see [bastion.md](bastion.md). The
e2e README points here when the operator chooses the containerized
mode.

## Requirements

- Linux host with Podman.
- A non-root user that can SSH to the address declared on the selected bastion
  Host and escalate with `sudo`.
- `bin/bootwright` built from this repository (`make build`).
- An OpenShift pull secret JSON at
  `~/.bootwright/secrets/openshift-pull-secret` on the host.

```bash
command -v podman
sudo -v

install -d -m 0700 ~/.ssh
test -f ~/.ssh/bootwright-ssh-key || \
  ssh-keygen -t ed25519 -f ~/.ssh/bootwright-ssh-key -N '' -C bootwright-container-bastion
touch ~/.ssh/authorized_keys
chmod 0600 ~/.ssh/authorized_keys
grep -qxF "$(cat ~/.ssh/bootwright-ssh-key.pub)" ~/.ssh/authorized_keys || \
  cat ~/.ssh/bootwright-ssh-key.pub >> ~/.ssh/authorized_keys
ssh -i ~/.ssh/bootwright-ssh-key -o StrictHostKeyChecking=accept-new "$USER"@localhost true

test -s ~/.bootwright/secrets/openshift-pull-secret
make build
test -x bin/bootwright
```

Cases may add their own host requirements (for example `/dev/kvm` for a
local libvirt provider) — those are in the case README, not here.

## Optional Proxy Env For The Container Build

The container build picks up the standard process proxy environment.
Bootwright commands inside the container use `Environment.spec.proxy` from the
active context. See [proxy.md](proxy.md) for how to express the proxy in
desired state.

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

## Build And Start The Bastion Container

`$CASE` is the case directory name under `test/e2e/`. Run from the repository
root.

```bash
export CASE=<case-directory>

BASTION_IMAGE=bootwright-bastion:$CASE
BASTION_NAME=bootwright-bastion-$CASE
BASTION_HOME="/home/$(id -un)"
E2E_BASE_DIR="/tmp/.bootwright-e2e/$CASE"
PROXY_RUN_ARGS=()
if [ -n "${HTTP_PROXY:-}${HTTPS_PROXY:-}${NO_PROXY:-}${http_proxy:-}${https_proxy:-}${no_proxy:-}" ]; then
  PROXY_RUN_ARGS=(
    --env "HTTP_PROXY=${HTTP_PROXY:-}"
    --env "HTTPS_PROXY=${HTTPS_PROXY:-}"
    --env "NO_PROXY=${NO_PROXY:-}"
    --env "http_proxy=${http_proxy:-}"
    --env "https_proxy=${https_proxy:-}"
    --env "no_proxy=${no_proxy:-}"
  )
fi

podman rm -f "$BASTION_NAME" 2>/dev/null || true
rm -rf "$E2E_BASE_DIR"
install -d -m 0700 "$E2E_BASE_DIR/secrets"

podman build -t "$BASTION_IMAGE" \
  -f test/e2e/$CASE/Containerfile \
  --build-arg UID="$(id -u)" \
  --build-arg GID="$(id -g)" \
  --build-arg USER="$(id -un)" \
  --build-arg HTTP_PROXY="${HTTP_PROXY:-}" \
  --build-arg HTTPS_PROXY="${HTTPS_PROXY:-}" \
  --build-arg NO_PROXY="${NO_PROXY:-}" \
  --build-arg http_proxy="${http_proxy:-}" \
  --build-arg https_proxy="${https_proxy:-}" \
  --build-arg no_proxy="${no_proxy:-}" \
  .

podman run -dit --name "$BASTION_NAME" \
  --network host \
  --userns=keep-id \
  "${PROXY_RUN_ARGS[@]}" \
  -v "$HOME/.ssh:$BASTION_HOME/.ssh:z" \
  -v "$E2E_BASE_DIR:$BASTION_HOME/.bootwright:Z" \
  -v "$HOME/.bootwright/secrets/openshift-pull-secret:$BASTION_HOME/pull-secret.json:ro,z" \
  -v "$PWD:/work:z" \
  "$BASTION_IMAGE"

podman exec -it "$BASTION_NAME" bash
```

The repo is mounted read-write at `/work` inside the bastion.

## Inside The Bastion

Set the e2e case name, initialize a context from the mounted fixture, then
export the context path variables the rest of the case relies on:

```bash
export CASE=<case-directory>
export BASE_DIR="$HOME/.bootwright/$CASE"
bootwright context init "$CASE" -f "/work/test/e2e/$CASE" --base-dir "$BASE_DIR" --yes
bootwright context validate
eval "$(bootwright print-env)"
```

If `print-env` reports that proxy credentials would be printed, create
the `proxy-credentials` secret in [common-steps.md](common-steps.md) first, then
rerun it with `--sensitive`.

## Bootstrap Bastion Dependencies

The first check is expected to report missing tools in a fresh container. The
apply installs the Bootwright-managed Ansible runtime and release-specific
OpenShift CLIs declared by the active context.

```bash
bootwright check bastion || true
bootwright apply bastion --yes
bootwright check bastion || true
```

In an externally proxied environment, create `proxy-credentials` first as shown
in [common-steps.md](common-steps.md), then run the bastion apply.

## Tear Down — Container And Host State

After the case-specific cluster destroy steps in
[common-steps.md](common-steps.md), exit the container and on the host:

```bash
podman rm -f "bootwright-bastion-$CASE"
rm -rf "/tmp/.bootwright-e2e/$CASE"
```
