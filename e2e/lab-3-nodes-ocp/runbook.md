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
[lab-sno](../lab-sno/runbook.md). The MetalLB selection repeats
[B757](../../specs/milestones/m4.md#b757)'s profile on three masters once B66 is
accepted. [B729](../../specs/milestones/m3.md#b729) needs five nodes and three
networks and is not here.

B66's exit evidence names a ledger row of the
[lab-compact](../../examples/openshift-clusters/lab-compact/README.md) example.
This case is that profile at the e2e addresses: the same three schedulable
masters with the same guest sizes and capacity reserve. Its row records
`e2e/lab-3-nodes-ocp` (rendered with `addons.metallb=false`) as the lab-compact
profile; accepting that substitution for B66 is the owner's decision.

Run every numbered step in order, in one Bash session from the checkout
being tested. A successful run includes the selected feature checks, deliberate
acceptance drills and cleanup. Stop on any unexpected failure; that attempt is
not a successful run. Select features before rendering and keep them unchanged.
Test another selection in a new complete run after cleanup.

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

## 1. Prepare

Complete [host preparation](../README.md#prepare-the-host). Prepare a pull secret saved as
`~/openshift-pull-secret` with mode `0600`, and an `oc` matching `openshift.version`
installed independently of Bootwright. No RHEL media is needed.

Destroy every lab-sno or other context that still owns `virbr-e2e` first (B66
requires a completed lab-sno apply to be destroyed before a fresh expanded
apply), and free the ports of the host preparation.

The masters keep OpenShift's 16 GiB control-plane minimum. The capacity
figures above must be available before starting. Require `oc version --client`
to succeed; the shared host preparation supplies the service diagnostic tools.

## 2. Render and initialize

Choose the default B66 run with MetalLB off, or enable it and copy the
same-release channel and CSV described in Inputs. Edit only these values:

```sh
METALLB_ENABLED="false"
AGED_IMAGE_DRILL="true" # Q6: include the one-time 25-hour image-age check.
PULL_SECRET_FILE="$HOME/openshift-pull-secret"
METALLB_CHANNEL=""
METALLB_CSV=""
```

`AGED_IMAGE_DRILL=true` includes Q6's one-time image-age check. Choose its
value before the run; record that choice with the result. Setting it to
`false` leaves Q6 untested.

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
CONTEXT="lab-3-nodes-ocp"
INPUT_DIR="$HOME/bootwright-e2e/lab-3-nodes-ocp"
umask 077
RUN_DIR=$(mktemp -d)
printf 'Run records: %s\n' "$RUN_DIR"
CONTROLLER_ADDRESS="$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')"
./bin/bootwright validate -f "$INPUT_DIR"
./bin/bootwright context init --name "$CONTEXT" --input-dir "$INPUT_DIR"
./bin/bootwright secret set --context "$CONTEXT" --name openshift-pull-secret --value-file "${PULL_SECRET_FILE:?Set PULL_SECRET_FILE to the private pull-secret JSON file}" --yes
./bin/bootwright secret generate --context "$CONTEXT"
./bin/bootwright secret check --context "$CONTEXT"
./bin/bootwright plan --context "$CONTEXT"
```

## 3. Prepare the controller and services

```sh
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
FIREWALL_PORTS=(53/tcp 53/udp 123/udp 3128/tcp 8080/tcp 8443/tcp)
```

Complete [Prepare listener firewall](../README.md#prepare-listener-firewall)
in this shell, then require controller preflight to report `ready`:

```sh
./bin/bootwright preflight controller --context "$CONTEXT"
command -v arping                         # B709: iputils, installed by the controller stage
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services --yes
```

The services answer on `$CONTROLLER_ADDRESS`:

```sh
curl -sk "https://$CONTROLLER_ADDRESS:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
dig @"$CONTROLLER_ADDRESS" +short api.compact.e2e.example.test api-int.compact.e2e.example.test   # 198.51.100.50
dig @"$CONTROLLER_ADDRESS" +short test.apps.compact.e2e.example.test                              # 198.51.100.51
chronyd -Q -t 10 "server $CONTROLLER_ADDRESS iburst port 123"
```

Create this run's resolver drop-in, then prove DNS and capacity. The initial
check must succeed so an existing file is never overwritten:

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

Every `resolution.*` and `capacity.*` check must be `passed`.

## 4. Install and perform the acceptance drills

### Stalled-node check: B260

This test deliberately stops `compact-03` after the three guests boot. Open
a second terminal in this checkout before starting; it supplies the power
read and stop below while the original terminal's apply runs. Expect the
bootstrap wait to fail and identify `compact-03` (`master-2`).

Start the first installation in the original terminal:

```sh
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

While that apply is running, open a second terminal in this checkout. Wait
until the three guests have booted, then stop the non-rendezvous guest:

```sh
CONTEXT=lab-3-nodes-ocp
./bin/bootwright machine list --context "$CONTEXT" --power-status
./bin/bootwright machine stop --context "$CONTEXT" --name compact-03 --yes
```

Let the first terminal's apply finish. This deliberate test expects failure;
the bootstrap wait can take its full 90-minute budget. Require the diagnostic
to lead with `compact-03` (`master-2`), its host status, status message and
failing validations. Record the result for B260.

In the original terminal, remove this test installation while retaining the
context, input, secrets and run records:

```sh
./bin/bootwright machine stop --context "$CONTEXT" --name compact-01,compact-02,compact-03 --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
./bin/bootwright preflight controller --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services --yes
./bin/bootwright preflight container-cluster --context "$CONTEXT"
```

Require controller readiness and passing resolution/capacity checks again.

### Installation interruptions: B66

Read the drill sequence before starting the fresh installation. Keep a second
terminal open for the power reads and the guarded process stop below.

1. With `AGED_IMAGE_DRILL=true`, press Ctrl-C after the media block completes
   and before boot. Keep the input and build unchanged, wait at least 25 hours,
   then observe status and run the same apply. Record the elapsed time and result.
2. Press Ctrl-C after the first node boots. Observe status, then run the same
   apply again; only nodes still needing boot receive media and power actions.
3. Press Ctrl-C during the bootstrap wait; observe status and run apply again.
4. Press Ctrl-C during the installation wait; observe status and run apply again.
5. During the installation wait, force-stop this apply process from the second
   terminal. Require status to show the block `unknown`; the next apply must
   resolve it by observation and complete.

Start the fresh installation in the original terminal now, performing the
selected drills above as their phases arrive:

```sh
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

For each Ctrl-C, use these commands in the original terminal after it exits:

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

For step 5, identify the current apply process in the second terminal:

```bash
ps -eo pid,args
read -r -p "PID of this run's apply process: " BOOTWRIGHT_PID
case "$BOOTWRIGHT_PID" in
    ''|0*|1|*[!0-9]*) printf '%s\n' 'Refusing an invalid process PID' >&2; exit 1 ;;
esac
ps -p "$BOOTWRIGHT_PID" -o pid,args
```

Confirm that command line belongs to this run, then send the signal:

```sh
kill -9 "${BOOTWRIGHT_PID:?Identify the current apply process first}"
```

In the original terminal, observe the forced stop and let the next apply complete:

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

All selected phases must be observed for B66. A missed phase leaves its
acceptance incomplete; finish cleanup and use a new complete run to test it.

Before a fresh boot Bootwright probes both VIPs for ARP replies from the bridge
(B709: it refuses naming the address, slot and hardware address that answers),
boots the masters, then proves each answers on TCP 22 and the rendezvous host
`compact-01` on 8090 (B710: a node that never answers is named). Record each
phase's duration as the progress lines print it, and the release installed.

## 5. Verify

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright machine list --context "$CONTEXT" --power-status
./bin/bootwright cluster info --context "$CONTEXT" --name compact
./bin/bootwright diff --context "$CONTEXT" --observe
./bin/bootwright apply --context "$CONTEXT" --yes      # settles: boots nothing, contacts no cluster
umask 077
KUBECONFIG="$RUN_DIR/kubeconfig"
export KUBECONFIG
./bin/bootwright cluster kubeconfig --context "$CONTEXT" --name compact > "$KUBECONFIG"
oc get nodes                                   # three Ready masters, schedulable
oc get clusterversion; oc get co
curl -k https://198.51.100.50:6443/version     # the API on its VIP, from the controller
curl -kI https://198.51.100.51                 # the ingress router
```

`oc get nodes` shows three Ready control-plane nodes and the release equals
`openshift.version`. The VIPs belong to the masters, not the host: `ip -br addr
show virbr-e2e` must not list either.

With `METALLB_ENABLED=true`, run these checks:

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

## 6. Destroy

First verify the expected refusal while the masters are running:

```sh
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
```

Require the refusal to name the guests to stop. Then stop them and complete
removal; require status to show the guests, disks, media, installer work area,
services and network removed before deleting the context:

```sh
./bin/bootwright machine stop --context "$CONTEXT" --name compact-01,compact-02,compact-03 --yes
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
