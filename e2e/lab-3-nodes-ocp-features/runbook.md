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
controller whose `cat /proc/sys/crypto/fips_enabled` prints `1`, which this
host is not: `security.fips=true` renders and validates but `./bin/bootwright plan --context "$C"` refuses
`controller.unsupported` here. Serving certificates
([B723](../../specs/milestones/m3.md#b723)) are not specified for install time
and are not in this case. The accepted [lab-sno](../lab-sno/runbook.md) and
[lab-3-nodes-ocp](../lab-3-nodes-ocp/runbook.md) rows are prerequisites.

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

## Prepare

The [host preparation](../README.md#prepare-the-host), a pull secret saved as
`~/openshift-pull-secret` with mode `0600`, an `oc` matching `openshift.version`
installed independently of Bootwright, and the capacity of the opening
paragraph. Free the ports of the host preparation. Guests on the managed bridge
reach the controller services through firewalld's `trusted` zone, so no host
rule is needed ([host preparation](../README.md#prepare-the-host)); that holds
for the IPv6 controller address too, because the guests' packets enter on the
bridge.

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

For `ipv6=true` (B725, B703) give the controller an IPv6 address outside the
guest prefix that the guests reach through the network's gateway, `2001:db8:1::1`
here:

```sh
sudo ip -6 addr add 2001:db8:1::1/128 dev lo
```

Use the complete IPv6 render in Run. The managed
network has `forward: none`: the guests reach the controller services and
nothing else, and everything outward goes through the proxy. Keep
`dhcp: false` and `autoconf: false` on the nodes; this is IPv6 single stack, not
dual stack.

## Run

Set boolean flags to `true` or `false`. Keep the defaults for the combined
feature run. For a mirror run, obtain its three values from the registry's
published digest mappings; they may stay empty when the mirror is disabled.
The IPv6 address is the lab address configured in Prepare:

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

This complete render consumes those flags. Select the IPv6 branch by setting
`IPV6="true"`; the mirror guards run only when it is enabled:

```sh
if [ "$MIRROR_ENABLED" = "true" ]; then
    : "${MIRROR_HOST:?Set MIRROR_HOST to the registry hostname, optionally with port}" \
      "${MIRROR_RELEASE_REPOSITORY:?Set MIRROR_RELEASE_REPOSITORY to the release repository path}" \
      "${MIRROR_COMPONENT_REPOSITORY:?Set MIRROR_COMPONENT_REPOSITORY to the component repository path}"
fi &&
if [ "$IPV6" = "true" ]; then
    A="${IPV6_CONTROLLER_ADDRESS:?Set IPV6_CONTROLLER_ADDRESS to the address configured in Prepare}"
else
    A="$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')"
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
C="lab-3-nodes-ocp-features"
IN="$HOME/bootwright-e2e/lab-3-nodes-ocp-features"
./bin/bootwright validate -f "$IN"
./bin/bootwright setup --yes
./bin/bootwright context init --name "$C" --input-dir "$IN"
./bin/bootwright secret set --context "$C" --name openshift-pull-secret --value-file "${PULL_SECRET_FILE:?Set PULL_SECRET_FILE to the private pull-secret JSON file}" --yes
if [ "$MIRROR_ENABLED" = "true" ]; then
    ./bin/bootwright secret set --context "$C" --name mirror-ca --certificate-file "${MIRROR_CA_FILE:?Set MIRROR_CA_FILE to the mirror CA PEM file}" --yes
    ./bin/bootwright secret set --context "$C" --name mirror-credentials --value-file "${MIRROR_AUTH_FILE:?Set MIRROR_AUTH_FILE to the private mirror authentication JSON file}" --yes
fi
./bin/bootwright secret generate --context "$C"
./bin/bootwright secret check --context "$C"
./bin/bootwright plan --context "$C"
./bin/bootwright apply --context "$C" --stage controller --yes
./bin/bootwright preflight controller --context "$C"
./bin/bootwright apply --context "$C" --stage infrastructure-services --yes
```

The mirror Secrets are imported only when enabled. Probe the services using
the address family selected above; an empty artifact root answers 404:

```sh
if [ "$IPV6" = "true" ]; then
    curl -gsk "https://[$A]:8443/" -o /dev/null -w '%{http_code}\n'
    dig -6 @"$A" AAAA +short api.features.e2e.example.test
    dig -6 @"$A" AAAA +short test.apps.features.e2e.example.test
else
    curl -sk "https://$A:8443/" -o /dev/null -w '%{http_code}\n'
    dig @"$A" +short api.features.e2e.example.test
    dig @"$A" +short test.apps.features.e2e.example.test
fi
chronyd -Q -t 10 "server $A iburst port 123"
```

Route the cluster zone, prove it, and apply:

```sh
sudo install -d -m 0755 /etc/systemd/resolved.conf.d
printf '[Resolve]\nDNS=%s\nDomains=~e2e.example.test\n' "$A" | sudo tee /etc/systemd/resolved.conf.d/bootwright-e2e.conf
sudo systemctl restart systemd-resolved
./bin/bootwright preflight container-cluster --context "$C"
./bin/bootwright plan --context "$C"
./bin/bootwright apply --context "$C" --yes
```

Every `resolution.*` and `capacity.*` check must be `passed`; with IPv6 the
resolution checks prove the AAAA answers. Record the build, release installed
and each phase's duration. A pull secret conflict or a mirror trust bundle the
cluster does not select refuses before the build.

## Verify

```sh
./bin/bootwright status --context "$C"
./bin/bootwright machine list --context "$C" --power-status
./bin/bootwright cluster info --context "$C" --name features
./bin/bootwright diff --context "$C" --observe
./bin/bootwright apply --context "$C" --yes      # settles: boots nothing, contacts no cluster
umask 077
KUBECONFIG="$(mktemp)"
export KUBECONFIG
./bin/bootwright cluster kubeconfig --context "$C" --name features > "$KUBECONFIG"
oc get nodes -o wide; oc get clusterversion; oc get co
```

Three Ready schedulable masters and the declared release. Then per item:

| Item | Check |
| --- | --- |
| B720 | `oc debug node/master-0 -- chroot /host lsblk -f` shows a `crypto_LUKS` root, and the complete partition check below shows a `tpm2` pin; repeat on `master-1`, `master-2` |
| B721 | `oc get proxy/cluster -o yaml`: `httpProxy` and `httpsProxy` are the controller address in `$A` on HTTP port 3128, `noProxy` lists the machine network, `.features.e2e.example.test` and `.e2e.example.test`, `trustedCA` is set. The managed proxy keeps no access log (`access_log none`), so prove the route in the [proxy check](#proxy-route) below |
| B724 | `oc get imagedigestmirrorset -o yaml` lists both mappings; `oc get secret pull-secret -n openshift-config -o jsonpath='{.data.\.dockerconfigjson}' \| base64 -d \| jq '.auths \| keys'` includes the mirror host; `oc get cm user-ca-bundle -n openshift-config` carries the mirror CA; with the proxy on, the mirror's own access log shows the release and component pulls arriving from the controller's address (the proxy egress), never from the node addresses |
| B725, B703 | `ip -6 addr show virbr-e2e` carries `2001:db8:2::1/64` and no IPv4 address; `oc get nodes -o wide` shows IPv6 internal addresses; `oc get network.config cluster -o jsonpath='{.status.clusterNetwork} {.status.serviceNetwork}'` is `fd00:10:128::/56` and `fd00:172:30::/112`; `curl -gk "https://[2001:db8:2::70]:6443/version"` answers; `./bin/bootwright diff --observe --context "$C"` reports the managed network without drift |

Read the encrypted partition from each node's `lsblk -f` output before
checking its pin. Repeat this block with the node and partition observed for
`master-0`, `master-1` and `master-2`; never infer the partition number:

```sh
NODE="master-0"
oc debug "node/$NODE" -- chroot /host lsblk -f
LUKS_PARTITION="" # Copy the observed crypto_LUKS root partition path.
: "${LUKS_PARTITION:?Set LUKS_PARTITION to the observed encrypted root partition}"
oc debug "node/$NODE" -- chroot /host clevis luks list -d "${LUKS_PARTITION:?Set LUKS_PARTITION to the observed encrypted root partition}"
```

### Proxy route

B721's outcome is that the cluster installs through the proxy. With
`ipv6=true` the network has `forward: none`, so a completed install already
shows that every outward pull went through it. On the IPv4 NAT network, prove it
on the controller (not yet run; the listener uses host networking). During the
installation wait the proxy holds connections from the node addresses:

```sh
sudo ss -Htn state established '( sport = :3128 )'
```

After the install, a node pull fails while the proxy is stopped and succeeds
after it starts again. Run it after the B720 check, so `oc debug`'s own image is
already on `master-0`; take the unit from `systemctl list-units 'bootwright-*'`:

```sh
oc debug node/master-0 -- chroot /host crictl pull registry.access.redhat.com/ubi9/ubi-minimal
PROXY_UNIT="" # Copy the managed proxy unit name from the listing above.
: "${PROXY_UNIT:?Set PROXY_UNIT to the observed managed proxy systemd unit}"
sudo systemctl stop "${PROXY_UNIT:?Set PROXY_UNIT to the observed managed proxy systemd unit}"
oc debug node/master-0 -- chroot /host crictl rmi registry.access.redhat.com/ubi9/ubi-minimal
oc debug node/master-0 -- chroot /host crictl pull registry.access.redhat.com/ubi9/ubi-minimal   # fails
sudo systemctl start "${PROXY_UNIT:?Set PROXY_UNIT to the observed managed proxy systemd unit}"
oc debug node/master-0 -- chroot /host crictl pull registry.access.redhat.com/ubi9/ubi-minimal
./bin/bootwright diff --context "$C" --observe
```

Record both results and that `./bin/bootwright diff --observe --context "$C"` reports the proxy without drift.

### TPM2 unlock

For TPM2, prove the unattended unlock: restart a master, then `oc get nodes`
until it is Ready again, with no console passphrase.

```sh
./bin/bootwright machine restart --context "$C" --name features-01 --yes
```

## Acceptance drills

For each forced-process interruption below, identify the apply process in a
second terminal. Set the editable PID from that listing and verify its command
line belongs to this run before sending the signal; never reuse an old PID:

```sh
ps -eo pid,args
BOOTWRIGHT_PID=""
: "${BOOTWRIGHT_PID:?Set BOOTWRIGHT_PID to the observed apply process PID for this run}"
case "${BOOTWRIGHT_PID:?Set BOOTWRIGHT_PID to the observed apply process PID for this run}" in
    ''|0*|1|*[!0-9]*) printf '%s\n' 'Refusing an invalid process PID' >&2; exit 1 ;;
esac
ps -p "${BOOTWRIGHT_PID:?Set BOOTWRIGHT_PID to the observed apply process PID for this run}" -o pid,args
```

Review that process line. Only at the drill phase, after confirming it is
this run's apply process, send the signal in a separate paste:

```sh
kill -9 "${BOOTWRIGHT_PID:?Set BOOTWRIGHT_PID to the observed apply process PID for this run}"
```

Interrupt with Ctrl-C at the named phase, let the command exit, run `./bin/bootwright status --context "$C"`,
then repeat `./bin/bootwright apply --context "$C" --yes`; a failed block is retried and an
unknown outcome is observed before any retry. Keep the input, Secrets and build
unchanged ([build changes](../../docs/operator-guide.md#changing-the-build-between-runs)).
Each item's row repeats the
[compact interruption drills](../lab-3-nodes-ocp/runbook.md#acceptance-drills)
on this topology:

1. Interrupt after the first node boots and continue; only the others boot.
2. Interrupt during the bootstrap wait and during the installation wait; continue
   each. One forced stop using the guarded kill command above in the installation wait: `./bin/bootwright status --context "$C"` shows
   the block `unknown` and the next `./bin/bootwright apply --context "$C" --yes` resolves it by observation.
3. A settled `./bin/bootwright apply --context "$C" --yes`, three Ready masters read through the exported kubeconfig.
4. Destroy refused while the guests run, then completed after `./bin/bootwright machine stop --context "$C" --name features-01,features-02,features-03 --yes`.

Record for each item row the build commit, the release minor, each phase's
duration, the drills' outcomes and the item's check from Verify. The IPv6 row
(B725, with B703's network) is one run of `ipv6=true` with every feature on. The
stalled-node and aged-image drills belong to B66, not here.

## Destroy

```sh
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright machine stop --context "$C" --name features-01,features-02,features-03 --yes
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright status --context "$C"
./bin/bootwright context delete --name "$C" --purge --yes
```

The first destroy refuses while the masters run and names the stop. Repeat it
to continue a partial removal and delete the context only after it completes.
Then remove the resolver drop-in (`sudo rm /etc/systemd/resolved.conf.d/bootwright-e2e.conf`
and `sudo systemctl restart systemd-resolved`), the `lo` address of
the IPv6 variant (`sudo ip -6 addr del 2001:db8:1::1/128 dev lo`), the
mirror's controller grant if you added one, the exported
kubeconfig and the rendered directory.
