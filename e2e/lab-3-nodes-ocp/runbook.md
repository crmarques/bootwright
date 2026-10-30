# lab-3-nodes-ocp

Three-node compact OpenShift on one libvirt host: schedulable masters
`compact-01..03` at `198.51.100.41-43` (6 vCPU, 16 GiB, 120 GiB and an emulated
TPM each, 48 GiB of guests), installed by the agent installer through emulated
Redfish BMCs, beside managed DNS, NTP, proxy and artifact services on the
controller. The installer owns the API VIP `198.51.100.50` and the ingress VIP
`198.51.100.51` with keepalived on the masters (derived platform `baremetal`,
`provisioningNetwork: disabled`). MetalLB is on by default and serves the pool
`198.51.100.200-198.51.100.207` to `LoadBalancer` Services only: the pool never
holds the API or ingress VIPs, and MetalLB never serves them. Context
`lab-3-nodes-ocp`, cluster `compact`. Needs a larger host than a 32 GiB
laptop: 22 CPUs, 56 GiB available memory, 400 GiB disk (three 6 vCPU, 16 GiB,
120 GiB masters plus the provider's 4 CPU, 8 GiB, 40 GiB `capacityReserve`, as
[capacity admission](../../specs/substrates.md#capacity-admission) counts them),
plus about 7 GiB for five image copies.

It accepts [B66](../../specs/milestones/m3.md#b66) with
[B709](../../specs/milestones/m3.md#b709),
[B710](../../specs/milestones/m3.md#b710) and
[B260](../../specs/milestones/m3.md#b260). B66 requires
[B61](../../specs/milestones/m3.md#b61)'s accepted row first, from
[lab-sno](../lab-sno/runbook.md). The optional MetalLB row repeats
[B757](../../specs/milestones/m4.md#b757)'s profile on three masters once B66 is
accepted. [B729](../../specs/milestones/m3.md#b729) needs five nodes and three
networks and is not here.

B66's exit evidence names a ledger row of the
[lab-compact](../../examples/openshift-clusters/lab-compact/README.md) example.
This case is that profile at the e2e addresses: the same three schedulable
masters with the same guest sizes and capacity reserve. Its row records
`e2e/lab-3-nodes-ocp` (rendered with `addons.metallb=false`) as the lab-compact
profile; accepting that substitution for B66 is the owner's decision.

## Inputs

| `--set` | Default | Effect | Needs |
| --- | --- | --- | --- |
| `addons.metallb=false` | `true` | drops the MetalLB binding `lab-3-nodes-ocp-metallb` | nothing |
| `addons.subscriptions.metallb.channel`, `.startingCSV` | empty | the operator's OLM channel and exact CSV | required while MetalLB is on: the render fails with `required` until both are set, or MetalLB is switched off |
| `addons.metallbPool` | `198.51.100.200-198.51.100.207` | the pool `services` | inside `host.cidr`; apart from the guests, the VIPs and the gateway |

[values.yaml](values.yaml) holds the rest: addresses, `vips.api` and
`vips.ingress`, upstreams, `openshift.version` and the guest size. Read the
channel and CSV with `oc get packagemanifest metallb-operator -n
openshift-marketplace -o jsonpath='{range .status.channels[*]}{.name}{"\t"}{.currentCSV}{"\n"}{end}'`
on a cluster of the same release, for example the lab-sno run before its
destroy. B66 itself needs no add-on: render with `--set addons.metallb=false`
for the pure B66 row.

## Prepare

The [host preparation](../README.md#prepare-the-host), a pull secret saved as
`~/openshift-pull-secret` with mode `0600`, and an `oc` matching `openshift.version`
installed independently of Bootwright. No RHEL media is needed.

Destroy every lab-sno or other context that still owns `virbr-e2e` first (B66
requires a completed lab-sno apply to be destroyed before a fresh expanded
apply), and free the ports of the host preparation.

The apply gate and `./bin/bootwright preflight container-cluster --context "$C"` refuse `capacity.*` before
registration when the host lacks the figures above. The masters keep
OpenShift's 16 GiB control-plane minimum. Guests on the managed bridge reach the
controller services through firewalld's `trusted` zone, so no firewall rule is
needed ([host preparation](../README.md#prepare-the-host)).

## Run

Choose the default B66 run with MetalLB off, or enable it and copy the
same-release channel and CSV described in Inputs. Edit only these values:

```sh
METALLB_ENABLED="false"
PULL_SECRET_FILE="$HOME/openshift-pull-secret"
METALLB_CHANNEL=""
METALLB_CSV=""
```

Render the selected values:

```sh
if [ "$METALLB_ENABLED" = "true" ]; then
    : "${METALLB_CHANNEL:?Set METALLB_CHANNEL from the same-release package manifest}" \
      "${METALLB_CSV:?Set METALLB_CSV to currentCSV for the selected channel}"
fi &&
e2e/render.sh lab-3-nodes-ocp --set "addons.metallb=$METALLB_ENABLED" \
  --set "addons.subscriptions.metallb.channel=$METALLB_CHANNEL" \
  --set "addons.subscriptions.metallb.startingCSV=$METALLB_CSV"
```

Continue after rendering with context import and the staged apply:

```sh
C="lab-3-nodes-ocp"
IN="$HOME/bootwright-e2e/lab-3-nodes-ocp"
A="$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')"
./bin/bootwright validate -f "$IN"
./bin/bootwright controller install --yes
./bin/bootwright context init --name "$C" --input-dir "$IN"
./bin/bootwright secret set --context "$C" --name openshift-pull-secret --value-file "${PULL_SECRET_FILE:?Set PULL_SECRET_FILE to the private pull-secret JSON file}" --yes
./bin/bootwright secret generate --context "$C"
./bin/bootwright secret check --context "$C"
./bin/bootwright plan --context "$C"
./bin/bootwright apply --context "$C" --stage controller --yes
./bin/bootwright preflight controller --context "$C"
command -v arping                         # B709: iputils, installed by the controller stage
./bin/bootwright apply --context "$C" --stage infrastructure-services --yes
```

The services answer on `$A`:

```sh
curl -sk "https://$A:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
dig @"$A" +short api.compact.e2e.example.test api-int.compact.e2e.example.test   # 198.51.100.50
dig @"$A" +short test.apps.compact.e2e.example.test                              # 198.51.100.51
chronyd -Q -t 10 "server $A iburst port 123"
```

Route the cluster zone to the managed DNS, then prove it and the capacity:

```sh
sudo install -d -m 0755 /etc/systemd/resolved.conf.d
printf '[Resolve]\nDNS=%s\nDomains=~e2e.example.test\n' "$A" | sudo tee /etc/systemd/resolved.conf.d/bootwright-e2e.conf
sudo systemctl restart systemd-resolved
./bin/bootwright preflight container-cluster --context "$C"
```

Every `resolution.*` and `capacity.*` check must be `passed` before the apply.
Then the fresh apply:

```sh
./bin/bootwright plan --context "$C"
./bin/bootwright apply --context "$C" --yes
```

Before a fresh boot Bootwright probes both VIPs for ARP replies from the bridge
(B709: it refuses naming the address, slot and hardware address that answers),
boots the masters, then proves each answers on TCP 22 and the rendezvous host
`compact-01` on 8090 (B710: a node that never answers is named). Record each
phase's duration as the progress lines print it, and the release installed.

## Verify

```sh
./bin/bootwright status --context "$C"
./bin/bootwright machine list --context "$C" --power-status
./bin/bootwright cluster info --context "$C" --name compact
./bin/bootwright diff --context "$C" --observe
./bin/bootwright apply --context "$C" --yes      # settles: boots nothing, contacts no cluster
umask 077
KUBECONFIG="$(mktemp)"
export KUBECONFIG
./bin/bootwright cluster kubeconfig --context "$C" --name compact > "$KUBECONFIG"
oc get nodes                                   # three Ready masters, schedulable
oc get clusterversion; oc get co
curl -k https://198.51.100.50:6443/version     # the API on its VIP, from the controller
curl -kI https://198.51.100.51                 # the ingress router
```

`oc get nodes` shows three Ready control-plane nodes and the release equals
`openshift.version`. The VIPs belong to the masters, not the host: `ip -br addr
show virbr-e2e` must not list either.

For the optional B757 row, after destroying the default run, use the channel
and exact CSV read from the same-release package manifest in Inputs. Render
with these editable values, then continue at the context import block in
Run, after its render block:

```sh
METALLB_ENABLED="true"
METALLB_CHANNEL=""
METALLB_CSV=""
if [ "$METALLB_ENABLED" = "true" ]; then
    : "${METALLB_CHANNEL:?Set METALLB_CHANNEL from the same-release package manifest}" \
      "${METALLB_CSV:?Set METALLB_CSV to currentCSV for the selected channel}"
fi &&
e2e/render.sh lab-3-nodes-ocp --set "addons.metallb=$METALLB_ENABLED" \
  --set "addons.subscriptions.metallb.channel=$METALLB_CHANNEL" \
  --set "addons.subscriptions.metallb.startingCSV=$METALLB_CSV"
```

With MetalLB on, also:

```sh
oc get csv,installplan -n metallb-system        # one approved InstallPlan, the declared CSV
oc get metallb,ipaddresspool,l2advertisement -n metallb-system
oc create namespace lb-canary
oc -n lb-canary create deployment web --image=registry.access.redhat.com/ubi9/httpd-24 --port=8080
oc -n lb-canary expose deployment web --type=LoadBalancer --port=80 --target-port=8080
curl -sI "http://$(oc -n lb-canary get svc web -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
oc get servicel2status -n metallb-system         # the node announcing the address
oc delete namespace lb-canary
```

The Service address falls in the pool, never on `198.51.100.50` or `.51`, and
the API and ingress still answer on their VIPs afterwards.

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

B66, in this order, each on a build that contains X116 to X119:

1. Staged fresh apply and `./bin/bootwright preflight container-cluster --context "$C"` passing (Run).
2. Interrupt after the first node boots, as soon as `./bin/bootwright machine list --context "$C" --power-status` shows one guest on. Repeat `./bin/bootwright apply --context "$C" --yes`: only the nodes still
   needing boot receive media and power actions, and the running node continues
   installing. If every guest is already on, record that and use the next fresh
   install.
3. Interrupt during the bootstrap wait; continue. Interrupt during the
   installation wait; continue. Then stop the `bootwright` process with the guarded kill command above during
   the installation wait: `./bin/bootwright status --context "$C"` shows the block `unknown`, and the next
   `./bin/bootwright apply --context "$C" --yes` resolves it by observation.
4. A settled `./bin/bootwright apply --context "$C" --yes`, then Verify: three Ready masters through the exported
   kubeconfig and the API answering on its VIP from the controller.
5. Stalled node (B260), on a separate fresh apply: after the masters boot, stop
   one that is not the rendezvous host and let the bootstrap wait fail:

   ```sh
   ./bin/bootwright machine stop --context "$C" --name compact-03 --yes
   ./bin/bootwright apply --context "$C" --yes
   ```

   The bootstrap wait can spend its full 90-minute budget. The failure must
   lead with the missing or unready master, `compact-03` (master-2), with its
   host status, status message and failing validations; record the response
   shape for B260's knowledge page. Then stop every guest, destroy, and run a
   fresh successful apply. A stranded rendezvous host (the failure says so)
   likewise needs a destroy and a fresh apply.
6. Destroy while the guests run and confirm the refusal; then stop all three and
   complete it ([Destroy](#destroy)), proving the owned disks and media absent.

The aged-image drill of Q6 runs only when the owner keeps it: interrupt after the
media block completes, wait at least 25 hours, then continue the same apply and
record the result.

## Destroy

```sh
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright machine stop --context "$C" --name compact-01,compact-02,compact-03 --yes
./bin/bootwright destroy --context "$C" --authorize data-loss
./bin/bootwright status --context "$C"
./bin/bootwright context delete --name "$C" --purge --yes
```

The first destroy refuses while the masters run and names the stop; an
installer that cannot shut down needs `./bin/bootwright machine stop --context "$C" --name compact-01,compact-02,compact-03 --force --yes`. A complete destroy
removes the guests, disks, media, the installer work area, the services and the
network. Repeat it to continue a partial removal; delete the context only after
it completes. Then remove the resolver drop-in
(`sudo rm /etc/systemd/resolved.conf.d/bootwright-e2e.conf` and `sudo systemctl restart systemd-resolved`), the exported kubeconfig and the rendered
directory.
