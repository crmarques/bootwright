# lab-3-nodes-ocp-features

Three-node compact OpenShift, cluster `features`, with the installation
features on: TPM2 disk encryption, the managed installation proxy and an
external registry mirror. Schedulable masters `features-01..03` at
`198.51.100.61-63` (8 vCPU, 16 GiB, 120 GiB and an emulated TPM each, 48 GiB of
guests), API VIP `198.51.100.70`, ingress VIP `198.51.100.71` held by the
installer, beside managed DNS, NTP, proxy and artifact services on the
controller. With `ipv6: true` the same cluster is IPv6 only on a managed libvirt
network with one prefix: masters `2001:db8:2::61-63`, VIPs `2001:db8:2::70` and
`::71`, cluster network `fd00:10:128::/56`, service network `fd00:172:30::/112`.
Context `lab-3-nodes-ocp-features`. Needs a larger host than a 32 GiB laptop:
28 CPUs, 56 GiB available memory, 400 GiB disk (three 8 vCPU, 16 GiB, 120 GiB
masters plus the provider's 4 CPU, 8 GiB, 40 GiB `capacityReserve`, as
[capacity admission](../../specs/substrates.md#capacity-admission) counts them),
plus media space.

It accepts [B720](../../specs/milestones/m3.md#b720) (TPM2),
[B721](../../specs/milestones/m3.md#b721) (proxy) and
[B724](../../specs/milestones/m3.md#b724) (mirror) on the default render, and
[B725](../../specs/milestones/m3.md#b725) with
[B703](../../specs/milestones/m1.md#b703) on `ipv6: true`; each is a
lab-features row of [B728](../../specs/milestones/m3.md#b728), one ledger row
per item run. [B722](../../specs/milestones/m3.md#b722) (FIPS) needs a
controller whose `cat /proc/sys/crypto/fips_enabled` prints `1`; otherwise
`security.fips=true` refuses `controller.unsupported` at plan. Serving certificates
([B723](../../specs/milestones/m3.md#b723)) are not specified for install time
and are not in this case. The accepted [lab-sno](../lab-sno/runbook.md) and
[lab-3-nodes-ocp](../lab-3-nodes-ocp/runbook.md) rows are prerequisites.

Run every numbered step in order, in one Bash session from the checkout
being tested. A successful run includes the selected feature checks, deliberate
acceptance drills and cleanup. Stop on any unexpected failure; that attempt is
not a successful run. Select features before rendering and keep them unchanged.
Test another selection in a new complete run after cleanup.

## Inputs

| `--set` | Default | Effect | Needs |
| --- | --- | --- | --- |
| `mirror.url` | empty | the mirror registry host, as the nodes pull it (`host` or `host:port`) | required while `mirror.enabled`: the render fails with `required` |
| `mirror.releaseRepository`, `mirror.componentRepository` | empty | the repositories on the mirror holding the release payload and the component images | required likewise; they are the two `imageDigestSources` mirrors |
| `mirror.enabled=false` | `true` | drops the Registry, its Secrets and `install.registries` | nothing |
| `proxy.enabled=false` | `true` | drops the Proxy and `install.proxy` | nothing |
| `security.tpm2=false` | `true` | drops `security.diskEncryption` | nothing |
| `security.fips=true` | `false` | FIPS cluster; keys become ECDSA P-256 | a FIPS-mode controller |
| `ipv6=true` | `false` | the IPv6-only variant above (isolated network, `forward: none`) | `networks.ipv6.controllerAddress`, required: an IPv6 address the controller holds and the guests reach |

To isolate one item, switch the other two features off: B720 with
`proxy.enabled=false` and `mirror.enabled=false`, B721 with `security.tpm2=false`
and `mirror.enabled=false`, B724 with `security.tpm2=false` and
`proxy.enabled=false`. [values.yaml](values.yaml) holds the rest: addresses,
VIPs, upstreams, the release and the guest size.

## 1. Prepare

Complete [host preparation](../README.md#prepare-the-host). Prepare a pull secret saved as
`~/openshift-pull-secret` with mode `0600`, an `oc` matching `openshift.version`
installed independently of Bootwright, and the capacity of the opening
paragraph. Require `oc version --client` to succeed. The shared host
preparation supplies the service diagnostic tools.

The mirror (B724), which you operate:

- Its release repository serves the payload of `openshift.version`, and its
  component repository serves that release's component images, mirrored by digest (for example with `oc-mirror`). The two
  repositories are separate; a release mirrored only to one leaves nodes
  fetching components from `quay.io` past the mirror.
- Its certificate chains to a CA you save as a PEM file, the `mirror-ca` Secret.
- A Docker configuration JSON with an `auths` entry for the host of
  `mirror.url`, saved with mode `0600`, is the `mirror-credentials` Secret. A
  pull secret that already holds a different entry for that host refuses.
- The proxy and the nodes' DNS must reach it: the mirror host is not in the
  proxy's `noProxy` (`*.e2e.example.test`), so the nodes pull through the managed
  proxy, which resolves from the controller; the managed DNS forwards to
  `host.upstreamDNS`, so set that to a resolver that knows a private mirror name.
- It is not on the managed bridge, so its own firewall is yours: admit its
  registry port from the controller's address, the proxy's egress
  ([network exposure](../../docs/operator-guide.md#network-exposure)).

The proxy (B721) is the managed Proxy on the controller, port 3128: it needs
only the controller's egress to the registries and the mirror.

For the IPv6 selection, the controller address must be outside the guest
prefix and reachable through its gateway. The preparation below adds the
example `2001:db8:1::1/128` to loopback. The guest network uses `forward: none`;
all outward traffic must pass through the managed proxy. Keep `dhcp: false`
and `autoconf: false`; this selection is IPv6 single stack.

## 2. Render and initialize

Set boolean flags to `true` or `false`. Keep the defaults for the combined
feature run. For a mirror run, obtain its three values from the registry's
published digest mappings; they may stay empty when the mirror is disabled.
The IPv6 address is added below only for the IPv6 selection:

```sh
IPV6="false"
IPV6_CONTROLLER_ADDRESS="2001:db8:1::1"
MIRROR_ENABLED="true"
PROXY_ENABLED="true"
TPM2_ENABLED="true"
FIPS_ENABLED="false"
PULL_SECRET_FILE="$HOME/openshift-pull-secret"
MIRROR_HOST=""
MIRROR_RELEASE_REPOSITORY=""
MIRROR_COMPONENT_REPOSITORY=""
MIRROR_CA_FILE="$HOME/mirror-ca.pem"
MIRROR_AUTH_FILE="$HOME/mirror-auth.json"
```

Set `IPV6=true` before this step to test IPv6. Add its controller address,
then render the selected input once:

```sh
if [ "$IPV6" = "true" ]; then
    sudo ip -6 addr add "$IPV6_CONTROLLER_ADDRESS/128" dev lo
fi
if [ "$MIRROR_ENABLED" = "true" ]; then
    : "${MIRROR_HOST:?Set MIRROR_HOST to the registry hostname, optionally with port}" \
      "${MIRROR_RELEASE_REPOSITORY:?Set MIRROR_RELEASE_REPOSITORY to the release repository path}" \
      "${MIRROR_COMPONENT_REPOSITORY:?Set MIRROR_COMPONENT_REPOSITORY to the component repository path}"
fi &&
if [ "$IPV6" = "true" ]; then
    CONTROLLER_ADDRESS="${IPV6_CONTROLLER_ADDRESS:?Set IPV6_CONTROLLER_ADDRESS to the address configured in Prepare}"
else
    CONTROLLER_ADDRESS="$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')"
fi &&
e2e/render.sh lab-3-nodes-ocp-features \
  --set "ipv6=$IPV6" --set "networks.ipv6.controllerAddress=$IPV6_CONTROLLER_ADDRESS" \
  --set "mirror.enabled=$MIRROR_ENABLED" --set "proxy.enabled=$PROXY_ENABLED" \
  --set "security.tpm2=$TPM2_ENABLED" --set "security.fips=$FIPS_ENABLED" \
  --set "mirror.url=$MIRROR_HOST" \
  --set "mirror.releaseRepository=$MIRROR_RELEASE_REPOSITORY" \
  --set "mirror.componentRepository=$MIRROR_COMPONENT_REPOSITORY"
```

Continue with the selected render and controller address:

```sh
CONTEXT="lab-3-nodes-ocp-features"
INPUT_DIR="$HOME/bootwright-e2e/lab-3-nodes-ocp-features"
umask 077
RUN_DIR=$(mktemp -d)
printf 'Run records: %s\n' "$RUN_DIR"
./bin/bootwright validate -f "$INPUT_DIR"
./bin/bootwright context init --name "$CONTEXT" --input-dir "$INPUT_DIR"
./bin/bootwright secret set --context "$CONTEXT" --name openshift-pull-secret --value-file "${PULL_SECRET_FILE:?Set PULL_SECRET_FILE to the private pull-secret JSON file}" --yes
if [ "$MIRROR_ENABLED" = "true" ]; then
    ./bin/bootwright secret set --context "$CONTEXT" --name mirror-ca --certificate-file "${MIRROR_CA_FILE:?Set MIRROR_CA_FILE to the mirror CA PEM file}" --yes
    ./bin/bootwright secret set --context "$CONTEXT" --name mirror-credentials --value-file "${MIRROR_AUTH_FILE:?Set MIRROR_AUTH_FILE to the private mirror authentication JSON file}" --yes
fi
./bin/bootwright secret generate --context "$CONTEXT"
./bin/bootwright secret check --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT"
```

## 3. Prepare the controller and services

```sh
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
FIREWALL_PORTS=(53/tcp 53/udp 123/udp 8080/tcp 8443/tcp)
if [ "$PROXY_ENABLED" = "true" ]; then
    FIREWALL_PORTS+=(3128/tcp)
fi
```

Complete [Prepare listener firewall](../README.md#prepare-listener-firewall)
in this shell, then require controller preflight to report `ready`:

```sh
./bin/bootwright preflight controller --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services --yes
```

The mirror Secrets are imported only when enabled. Probe the services using
the address family selected above; an empty artifact root answers 404:

```sh
if [ "$IPV6" = "true" ]; then
    curl -gsk "https://[$CONTROLLER_ADDRESS]:8443/" -o /dev/null -w '%{http_code}\n'
    dig -6 @"$CONTROLLER_ADDRESS" AAAA +short api.features.e2e.example.test
    dig -6 @"$CONTROLLER_ADDRESS" AAAA +short test.apps.features.e2e.example.test
else
    curl -sk "https://$CONTROLLER_ADDRESS:8443/" -o /dev/null -w '%{http_code}\n'
    dig @"$CONTROLLER_ADDRESS" +short api.features.e2e.example.test
    dig @"$CONTROLLER_ADDRESS" +short test.apps.features.e2e.example.test
fi
chronyd -Q -t 10 "server $CONTROLLER_ADDRESS iburst port 123"
```

Create this run's resolver drop-in and prove resolution and capacity. The
initial check must succeed so an existing file is never overwritten:

```sh
(
    set -e
    test ! -e /etc/systemd/resolved.conf.d/bootwright-e2e.conf
    test ! -L /etc/systemd/resolved.conf.d/bootwright-e2e.conf
    sudo install -d -m 0755 /etc/systemd/resolved.conf.d
    printf '[Resolve]\nDNS=%s\nDomains=~e2e.example.test\n' "$CONTROLLER_ADDRESS" | sudo tee /etc/systemd/resolved.conf.d/bootwright-e2e.conf
    sudo systemctl restart systemd-resolved
)
./bin/bootwright preflight container-cluster --context "$CONTEXT"
```

Every `resolution.*` and `capacity.*` check must be `passed`; with IPv6 the
resolution checks prove the AAAA answers. Record the build, release installed
and each phase's duration. A pull secret conflict or a mirror trust bundle the
cluster does not select refuses before the build.

## 4. Install and perform the interruption drills

Read the sequence before starting. Keep a second terminal open in this
checkout. These are required tests during this installation:

1. Press Ctrl-C after the first node boots. On the next apply, only the nodes
   still needing boot receive media and power actions.
2. Press Ctrl-C during the bootstrap wait; apply must continue installation.
3. Press Ctrl-C during the installation wait; apply must continue installation.
4. Force-stop the apply process during the installation wait using the guarded
   commands below. Status must show `unknown`; apply resolves it by observation.

With the proxy enabled, also run
[Observe proxy connections](#observe-proxy-connections) in the second terminal
during the installation wait. Read that block before launching apply; its
connection evidence must be captured before installation finishes.

Start the installation in the original terminal:

```sh
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

After each deliberate interruption, wait for the process to exit, observe
status, then run the same apply:

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

For step 4, identify and verify this run's apply process in the second terminal:

```bash
ps -eo pid,args
read -r -p "PID of this run's apply process: " BOOTWRIGHT_PID
case "$BOOTWRIGHT_PID" in
    ''|0*|1|*[!0-9]*) printf '%s\n' 'Refusing an invalid process PID' >&2; exit 1 ;;
esac
ps -p "$BOOTWRIGHT_PID" -o pid,args
```

Confirm the displayed command belongs to this run, then send the signal:

```sh
kill -9 "${BOOTWRIGHT_PID:?Identify the current apply process first}"
```

Observe `unknown` in the original terminal, then continue apply. Capture the
proxy connections from the second terminal during its installation wait:

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

### Observe proxy connections

With the proxy enabled, record node connections from the second terminal
during the installation wait (B721):

```sh
sudo ss -Htn state established '( sport = :3128 )'
```

Every interruption phase must be observed. A missed phase leaves acceptance
incomplete; finish cleanup and use a new complete run to test it. Record each
phase's duration and the installed release.

## 5. Verify

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright machine list --context "$CONTEXT" --power-status
./bin/bootwright cluster info --context "$CONTEXT" --name features
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright apply --context "$CONTEXT" --yes      # settles: boots nothing, contacts no cluster
umask 077
KUBECONFIG="$RUN_DIR/kubeconfig"
export KUBECONFIG
./bin/bootwright cluster kubeconfig --context "$CONTEXT" --name features > "$KUBECONFIG"
oc get nodes -o wide; oc get clusterversion; oc get co
```

Require three Ready schedulable masters and the declared release. Run each
check whose feature was enabled in step 2:

| Item | Check |
| --- | --- |
| B720 | `oc debug node/master-0 -- chroot /host lsblk -pf` shows a `crypto_LUKS` root, and the complete partition check below shows a `tpm2` pin; repeat on `master-1`, `master-2` |
| B721 | `oc get proxy/cluster -o yaml`: `httpProxy` and `httpsProxy` are the controller address in `$CONTROLLER_ADDRESS` on HTTP port 3128, `noProxy` lists the machine network, `.features.e2e.example.test` and `.e2e.example.test`, `trustedCA` is set. The managed proxy keeps no access log (`access_log none`), so prove the route in the [proxy check](#proxy-route-when-enabled) below |
| B724 | `oc get imagedigestmirrorset -o yaml` lists both mappings; `oc get secret pull-secret -n openshift-config -o jsonpath='{.data.\.dockerconfigjson}' \| base64 -d \| jq '.auths \| keys'` includes the mirror host; `oc get cm user-ca-bundle -n openshift-config` carries the mirror CA; with the proxy on, the mirror's own access log shows the release and component pulls arriving from the controller's address (the proxy egress), never from the node addresses |
| B725, B703 | `ip -6 addr show virbr-e2e` carries `2001:db8:2::1/64` and no IPv4 address; `oc get nodes -o wide` shows IPv6 internal addresses; `oc get network.config cluster -o jsonpath='{.status.clusterNetwork} {.status.serviceNetwork}'` is `fd00:10:128::/56` and `fd00:172:30::/112`; `curl -gk "https://[2001:db8:2::70]:6443/version"` answers; `./bin/bootwright diff --observe --context "$CONTEXT"` reports the managed network without drift |

With TPM2 enabled, find the `crypto_LUKS` partition whose children contain the
`/` mountpoint in each node's `lsblk -pf` output. Copy its full `/dev/...` path
from `NAME`, without the tree drawing characters, before checking its pin.
Repeat this block with the node and partition observed for
`master-0`, `master-1` and `master-2`; never infer the partition number:

```sh
NODE="master-0"
oc debug "node/$NODE" -- chroot /host lsblk -pf
read -r -p "Full /dev/... path of the crypto_LUKS root partition on $NODE: " LUKS_PARTITION
: "${LUKS_PARTITION:?Set LUKS_PARTITION to the full /dev/... path of the observed crypto_LUKS root partition}"
oc debug "node/$NODE" -- chroot /host clevis luks list -d "${LUKS_PARTITION:?Set LUKS_PARTITION to the full /dev/... path of the observed crypto_LUKS root partition}"
```

### Proxy route (when enabled)

B721's outcome is that the cluster installs through the proxy. With
`ipv6=true` the network has `forward: none`, so a completed install already
shows that every outward pull went through it. Keep the node-connection
observations from step 4, then prove the route below on the IPv4 NAT network.

After the install, a node pull fails while the proxy is stopped and succeeds
after it starts again. Run it after the B720 check, so `oc debug`'s own image is
already on `master-0`; take the unit from `systemctl list-units 'bootwright-*'`:

```sh
oc debug node/master-0 -- chroot /host crictl pull registry.access.redhat.com/ubi9/ubi-minimal
systemctl list-units 'bootwright-*'
read -r -p 'Managed proxy unit from that listing: ' PROXY_UNIT
: "${PROXY_UNIT:?Set PROXY_UNIT to the observed managed proxy systemd unit}"
sudo systemctl stop "${PROXY_UNIT:?Set PROXY_UNIT to the observed managed proxy systemd unit}"
oc debug node/master-0 -- chroot /host crictl rmi registry.access.redhat.com/ubi9/ubi-minimal
oc debug node/master-0 -- chroot /host crictl pull registry.access.redhat.com/ubi9/ubi-minimal   # fails
sudo systemctl start "${PROXY_UNIT:?Set PROXY_UNIT to the observed managed proxy systemd unit}"
oc debug node/master-0 -- chroot /host crictl pull registry.access.redhat.com/ubi9/ubi-minimal
./bin/bootwright diff --context "$CONTEXT" --observe
```

Record both results and that `./bin/bootwright diff --observe --context "$CONTEXT"` reports the proxy without drift.

### TPM2 unlock (when enabled)

For TPM2, prove the unattended unlock: restart a master, then `oc get nodes`
until it is Ready again, with no console passphrase.

```sh
./bin/bootwright machine restart --context "$CONTEXT" --name features-01 --yes
```

## 6. Destroy

First verify the expected refusal while the masters are running:

```sh
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
```

Require the refusal to name the guests to stop. Stop them and complete removal;
require status to show the guests, disks, media, installer work area, services
and network removed before deleting the context:

```sh
./bin/bootwright machine stop --context "$CONTEXT" --name features-01,features-02,features-03 --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright context delete --name "$CONTEXT" --purge --yes
```

## 7. Clean up the host

Complete [Restore listener firewall](../README.md#restore-listener-firewall)
with this run's `RUN_DIR`. Then remove the resolver drop-in created in step 3:

```sh
sudo rm -- /etc/systemd/resolved.conf.d/bootwright-e2e.conf
sudo systemctl restart systemd-resolved
```

For the IPv6 selection, remove the address this run added. Remove only the
mirror firewall grant you recorded during preparation; preserve prior grants.

```sh
if [ "$IPV6" = "true" ]; then
    sudo ip -6 addr del "$IPV6_CONTROLLER_ADDRESS/128" dev lo
fi
```

Complete [Remove diagnostic tools](../README.md#remove-diagnostic-tools).
Only after both cleanup procedures succeed, remove this run's private files:

```sh
rm -rf -- "${RUN_DIR:?Keep RUN_DIR until cleanup succeeds}" "${INPUT_DIR:?Keep INPUT_DIR until destroy completes}"
unset KUBECONFIG
```

## 8. Record the result

Record the build commit, selected inputs, installed release, phase durations,
verification results, deliberate drill outcomes and completed cleanup in the
[acceptance ledger](../../docs/acceptance.md). A run is successful only when
all steps for its selected features pass from preparation through cleanup.
