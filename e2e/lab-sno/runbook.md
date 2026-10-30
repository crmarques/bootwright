# lab-sno

Single-node OpenShift on one libvirt guest: `sno-01` at `198.51.100.31`
(8 vCPU, 16 GiB, 120 GiB, an emulated TPM), installed by the agent installer
through an emulated Redfish BMC, beside managed DNS, NTP, proxy and artifact
services on the controller. The node answers `api`, `api-int` and `*.apps`
itself (`platform: none`). Toggles add FIPS mode, TPM2 disk encryption and the
NMState and MetalLB add-ons. Context `lab-sno`, cluster `sno`.

It accepts [B61](../../specs/milestones/m3.md#b61) with
[B705](../../specs/milestones/m3.md#b705),
[B706](../../specs/milestones/m3.md#b706) and
[B462](../../specs/milestones/m3.md#b462) (default render),
[B755](../../specs/milestones/m4.md#b755) (`addons.nmstate`) and
[B757](../../specs/milestones/m4.md#b757) (`addons.metallb`), one ledger row per
item run. `security.fips` and `security.tpm2` only rehearse
[B722](../../specs/milestones/m3.md#b722) and
[B720](../../specs/milestones/m3.md#b720), which qualify on three nodes in
[lab-3-nodes-ocp-features](../lab-3-nodes-ocp-features/runbook.md).

Run every numbered step in order, in one Bash session from the checkout
being tested. A successful run includes the selected feature checks, deliberate
acceptance drills and cleanup. Stop on any unexpected failure; that attempt is
not a successful run. Select features before rendering and keep them unchanged.
Test another selection in a new complete run after cleanup.

## Inputs

| `--set` | Default | Effect | Needs |
| --- | --- | --- | --- |
| `security.fips=true` | `false` | FIPS cluster; the fleet and cluster keys become ECDSA P-256 | a FIPS-mode controller (`cat /proc/sys/crypto/fips_enabled` prints `1`); otherwise `./bin/bootwright plan --context "$CONTEXT"` and `./bin/bootwright preflight controller --context "$CONTEXT"` refuse `controller.unsupported` |
| `security.tpm2=true` | `false` | TPM2-bound LUKS on the node's installation disk | nothing; the profile already carries `tpm: {}` |
| `addons.nmstate=true` | `false` | binding `lab-sno-nmstate`: Kubernetes NMState operator and instance | `addons.subscriptions.nmstate.channel` and `.startingCSV` |
| `addons.metallb=true` | `false` | binding `lab-sno-metallb`: MetalLB operator, instance and layer 2 pool `services` | `addons.subscriptions.metallb.channel` and `.startingCSV` |

An enabled add-on with an empty channel or CSV fails the render with
`required`: they are operator choices with no default. `addons.metallbPool`
(default `198.51.100.200-198.51.100.207`) serves `LoadBalancer` Services only;
a single node has no virtual IPs and MetalLB never serves the API or ingress.
[values.yaml](values.yaml) holds the rest: addresses, upstreams, the release
(`openshift.version`) and the guest size.

## 1. Prepare

- Complete [host preparation](../README.md#prepare-the-host). Prepare a pull
  secret at `~/openshift-pull-secret` with mode `0600` and an `oc` matching
  `openshift.version`, installed independently of Bootwright. Require
  `oc version --client` to succeed. No RHEL media is needed: the agent ISO
  is built from the release.
- Capacity, which the apply gate and `./bin/bootwright preflight container-cluster --context "$CONTEXT"` count
  ([capacity admission](../../specs/substrates.md#capacity-admission)): the
  guest plus the provider's `capacityReserve` (1 CPU, 2 GiB, 10 GiB) need 9
  host CPUs not reserved by other libvirt domains, 18 GiB of `MemAvailable` in
  `/proc/meminfo` (swap never counts) and 130 GiB free on the filesystem
  backing `/var/lib/libvirt/images`, beside about 4 GiB for the image's three
  copies ([lab-sno profile](../../specs/openshift-coverage.md#acceptance-profiles)).
  Check with `nproc`, `grep MemAvailable /proc/meminfo` and
  `df -h /var/lib/libvirt/images`.
- Record the build commit and require `Source clean` in `bootwright version`.
  B61 needs the build containing X111–X115, B755 needs X138, and B757 needs
  X134, with their required integration gates passed before live acceptance.
- B706 requires `nmstatectl` to be absent before the controller stage. Review
  the removal transaction and confirm that it removes only `nmstate`; cancel
  if it proposes removing any other package. The controller stage below must
  install this execution dependency itself.

  ```sh
  if command -v nmstatectl >/dev/null; then
      sudo dnf remove --setopt=assumeyes=False --setopt=defaultyes=False \
          --setopt=clean_requirements_on_remove=False nmstate
  fi
  if command -v nmstatectl >/dev/null; then
      printf '%s\n' 'B706 requires nmstatectl to be absent before the controller stage.' >&2
      exit 1
  fi
  ```

Add-on channels and CSVs come from a cluster of the same release, so run the
default selection first (the B61 run), read the choices in step 5, and finish
that run through cleanup. Use those values when starting a new complete add-on run.

## 2. Render and initialize

Choose the feature flags here. Leave both add-ons off for B61. For an enabled
add-on, copy its channel and CSV recorded by a completed default run of the same release.
Test at most one add-on per complete acceptance run: the final cleanup test
must interrupt that add-on's readiness. This runbook restriction gives each
add-on its own evidence; Bootwright supports enabling both. Keep boolean
values `true` or `false`:

```sh
FIPS_ENABLED="false"
PULL_SECRET_FILE="$HOME/openshift-pull-secret"
TPM2_ENABLED="false"
NMSTATE_ENABLED="false"
METALLB_ENABLED="false"
NMSTATE_CHANNEL=""
NMSTATE_CSV=""
METALLB_CHANNEL=""
METALLB_CSV=""
```

Render the selected flags once:

```sh
(
    set -e
    if [ "$NMSTATE_ENABLED:$METALLB_ENABLED" = true:true ]; then
        printf '%s\n' 'Select one add-on per complete acceptance run.' >&2
        exit 1
    fi
    if [ "$NMSTATE_ENABLED" = "true" ]; then
        : "${NMSTATE_CHANNEL:?Set NMSTATE_CHANNEL from the same-release package manifest}" \
          "${NMSTATE_CSV:?Set NMSTATE_CSV to currentCSV for the selected channel}"
    fi &&
    if [ "$METALLB_ENABLED" = "true" ]; then
        : "${METALLB_CHANNEL:?Set METALLB_CHANNEL from the same-release package manifest}" \
          "${METALLB_CSV:?Set METALLB_CSV to currentCSV for the selected channel}"
    fi &&
    e2e/render.sh lab-sno \
      --set "security.fips=$FIPS_ENABLED" --set "security.tpm2=$TPM2_ENABLED" \
      --set "addons.nmstate=$NMSTATE_ENABLED" --set "addons.metallb=$METALLB_ENABLED" \
      --set "addons.subscriptions.nmstate.channel=$NMSTATE_CHANNEL" \
      --set "addons.subscriptions.nmstate.startingCSV=$NMSTATE_CSV" \
      --set "addons.subscriptions.metallb.channel=$METALLB_CHANNEL" \
      --set "addons.subscriptions.metallb.startingCSV=$METALLB_CSV"
)
```

Continue after rendering with context import and the staged apply:

```sh
CONTEXT="lab-sno"
INPUT_DIR="$HOME/bootwright-e2e/lab-sno"
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
command -v nmstatectl                      # B706: now installed by the controller stage
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services --yes
```

The services answer on `$CONTROLLER_ADDRESS`:

```sh
curl -sk "https://$CONTROLLER_ADDRESS:8443/" -o /dev/null -w '%{http_code}\n'   # 404 on an empty root
dig @"$CONTROLLER_ADDRESS" +short api.sno.e2e.example.test test.apps.sno.e2e.example.test   # both 198.51.100.31
chronyd -Q -t 10 "server $CONTROLLER_ADDRESS iburst port 123"
```

Create this run's resolver drop-in and let preflight prove resolution (B705).
The initial check must succeed so an existing file is never overwritten:

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

## 4. Install and perform the interruption drills

Read this sequence before starting the apply. These are deliberate acceptance
tests during this installation:

| Phase | Action | Required result on the next apply |
| --- | --- | --- |
| Image build | Ctrl-C | The image rebuilds |
| Bootstrap wait | Ctrl-C | Installation continues |
| Installation wait | Ctrl-C | Installation continues |
| Installation wait | Force-stop the apply process using the guarded commands below | Status shows `unknown`; apply resolves the outcome by observation |
| NMState object creation, approval wait, readiness wait, when enabled | Ctrl-C at each phase | Already-created objects report `unchanged` |
| MetalLB approval wait and the interval between the MetalLB resource and pools, when enabled | Ctrl-C at each phase | Already-created objects report `unchanged` |

With NMState enabled, also run [Observe NMState approval](#observe-nmstate-approval)
in the second terminal during the approval wait. Read that block before
launching apply so the required condition observations are captured in time.

Start the installation in the original terminal and perform the table's
Ctrl-C drills and the selected observations as their phases arrive:

```sh
./bin/bootwright plan --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

After each deliberate interruption, wait for that process to exit, then use
these commands in the original terminal:

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

For the forced stop, use a second terminal in this checkout while the
installation wait is running. Identify and verify this run's apply process:

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

In the original terminal, observe `unknown`, then continue apply. Perform the
remaining selected add-on drills and the approval observation while it runs:

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

### Observe NMState approval

With NMState enabled, while the approval wait is running, observe its
Subscription from a second terminal. Use a private kubeconfig and remove that
export after recording whether `ResolutionFailed=True` appeared and cleared:

```sh
umask 077
WATCH_KUBECONFIG=$(mktemp)
./bin/bootwright cluster kubeconfig --context lab-sno --name sno > "$WATCH_KUBECONFIG"
KUBECONFIG="$WATCH_KUBECONFIG" oc get subscription -n openshift-nmstate -w -o yaml
```

Press Ctrl-C to end this observation, then remove its private export:

```sh
rm -- "$WATCH_KUBECONFIG"
```

Every selected phase must be observed. A missed phase leaves acceptance
incomplete; finish cleanup and use a new complete run to test it.

Allow over an hour: image build, boot, bootstrap wait, installation wait,
release. Record each phase's duration as the progress lines print it (`./bin/bootwright status --detail full --context "$CONTEXT"` repeats them) and the release installed. The apply names a
private operation-log directory before effects start; keep it as evidence.

## 5. Verify

These editable namespace values come from the rendered add-on bindings:

```sh
NMSTATE_NAMESPACE="openshift-nmstate"
METALLB_NAMESPACE="metallb-system"
```

1. State and access:

   ```sh
   ./bin/bootwright status --context "$CONTEXT"
   ./bin/bootwright machine list --context "$CONTEXT" --power-status
   ./bin/bootwright cluster list --context "$CONTEXT"
   ./bin/bootwright cluster info --context "$CONTEXT" --name sno
   ./bin/bootwright diff --context "$CONTEXT" --observe
   ./bin/bootwright apply --context "$CONTEXT" --yes      # settles: boots nothing, contacts no cluster
   umask 077
   KUBECONFIG="$RUN_DIR/kubeconfig"
   export KUBECONFIG
   ./bin/bootwright cluster kubeconfig --context "$CONTEXT" --name sno > "$KUBECONFIG"
   oc version; oc get nodes; oc get clusterversion; oc get co
   ```

   `oc` is the independent client. B61 wants one Ready node, the release equal
   to `openshift.version` and a clean `./bin/bootwright diff --observe --context "$CONTEXT"`.

2. First-run facts, recorded for B61 and B462: the `openshift-client-linux`
   archive Bootwright fetched carried the release stamp (the controller stage
   did not refuse); the installer's cached base image directory, from `sudo find
   / -xdev -path '*agent/image_cache*' 2>/dev/null`; the keep task, proved by
   the B462 drill below. B706 is proved by the image building on a controller
   where `command -v nmstatectl` printed nothing before the controller stage,
   after the Prepare owner step.
   Before destroying the default run, read the add-on choices:

   ```sh
   oc get packagemanifest kubernetes-nmstate-operator -n openshift-marketplace -o jsonpath='{range .status.channels[*]}{.name}{"\t"}{.currentCSV}{"\n"}{end}'
   oc get packagemanifest metallb-operator -n openshift-marketplace -o jsonpath='{range .status.channels[*]}{.name}{"\t"}{.currentCSV}{"\n"}{end}'
   ```

3. Toggle checks:

   | Toggle | Check |
   | --- | --- |
   | fips | `oc debug node/master-0 -- chroot /host cat /proc/sys/crypto/fips_enabled` prints `1` |
   | tpm2 | `oc debug node/master-0 -- chroot /host lsblk -f` shows a `crypto_LUKS` root |
   | nmstate | `oc get csv,installplan,subscription -n openshift-nmstate`: the CSV equals `startingCSV`, one InstallPlan approved; `oc get nmstate`; `oc get deploy -n openshift-nmstate` (the webhook Deployment's name); `oc get nns` |
   | metallb | `oc get csv,installplan -n metallb-system`; `oc get metallb,ipaddresspool,l2advertisement -n metallb-system`; the pool `services` holds eight addresses |

   For B755 and B757 also record `./bin/bootwright diff --observe --context "$CONTEXT"` reporting no drift, the
   build commit, the release minor, the channel and `startingCSV`, and the
   phase durations. The checks below re-verify the facts each section lists;
   they are this runbook's reading of those facts, unverified on a cluster
   until the first run records them.

   With `NMSTATE_ENABLED=true`, check B755 in `openshift-nmstate`:

   - `--ignore-not-found` classifies only NotFound:
     `oc get nmstate no-such-instance --ignore-not-found; echo $?` prints no
     object and `0`, while `oc get no-such-kind x --ignore-not-found; echo $?`
     fails with a nonzero status (a missing kind is not NotFound).
   - The InstallPlan's owner reference:
     `oc get installplan -n "$NMSTATE_NAMESPACE" -o jsonpath='{.items[*].metadata.ownerReferences[*].kind}'`
     names `Subscription`.
   - Creation with an explicit field manager records the manager:
     `oc get subscription -n "$NMSTATE_NAMESPACE" -o yaml --show-managed-fields` shows
     Bootwright's manager in `metadata.managedFields`.
   - Keep the Subscription condition observations from step 4, including
     whether `ResolutionFailed=True` appeared and cleared during approval.
   - The webhook Deployment's name: `oc get deploy -n "$NMSTATE_NAMESPACE"` lists
     `nmstate-webhook`, the name the readiness check uses.

   With `METALLB_ENABLED=true`, check B757 in `metallb-system`:

   - The InstallPlan lists only the MetalLB CSV:
     `oc get installplan -n "$METALLB_NAMESPACE" -o jsonpath='{.items[*].spec.clusterServiceVersionNames}'`.
   - The installed CSV supports all namespaces only and declares
     disconnected support:
     `oc get csv -n "$METALLB_NAMESPACE" -o jsonpath='{.items[*].spec.installModes}'` shows
     `AllNamespaces` alone supported, and
     `oc get csv -n "$METALLB_NAMESPACE" -o jsonpath='{.items[*].metadata.annotations}'`
     shows the disconnected feature annotation; record its exact key and
     value.
   - The webhook refuses no pool or advertisement create once `MetalLB`
     reports `Available`: the apply creates the pool and advertisement after
     that readiness without a refusal (its progress and operation log show
     each object created once), and `oc get events -n "$METALLB_NAMESPACE"` shows no webhook
     denial.
   - `oc get ipaddresspool services -n "$METALLB_NAMESPACE" -o yaml` shows
     `status.assignedIPv4` after the Service test in this section's item 4.
   - `oc get servicel2status -n "$METALLB_NAMESPACE"` names the announcing node.

4. With `METALLB_ENABLED=true`, test the MetalLB Service (B757) in a namespace
   Bootwright does not own:

   ```sh
   oc create namespace lb-canary
   oc -n lb-canary create deployment web --image=registry.access.redhat.com/ubi9/httpd-24 --port=8080
   oc -n lb-canary expose deployment web --type=LoadBalancer --port=80 --target-port=8080
   oc -n lb-canary get svc web -w          # EXTERNAL-IP is inside 198.51.100.200-207
   curl -sI "http://$(oc -n lb-canary get svc web -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
   ```

   The controller reaches the address over the bridge. Remove the namespace
   afterwards: `oc delete namespace lb-canary`.

## 6. Destroy the verified installation

First verify the expected refusal while the guest runs:

```sh
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
```

Require the refusal to name `sno-01`. Stop it and complete removal:

```sh
./bin/bootwright machine stop --context "$CONTEXT" --name sno-01 --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
./bin/bootwright status --context "$CONTEXT"
```

Require the guest, disks, media, installer work area, managed services and
network to be removed. This also proves that domain ownership metadata
survived the emulator's boot selection and insert. Keep the context, secrets
and run records for the final acceptance test below.

## 7. Prove cleanup of an interrupted installation

This is a planned second installation using the same input and secrets.
Read the interruption point before starting:

- With both add-ons off, perform B462: in the second terminal watch the
  installer's kubeconfig until the cluster reports `Available=True`. Before
  apply records completion, use step 4's guarded PID lookup to force-stop it.
- With NMState enabled, press Ctrl-C during NMState's readiness wait.
- With only MetalLB enabled, press Ctrl-C during MetalLB's readiness wait.

The add-on tests must prove retirement after the guest stops and its API
becomes unreachable. Restore services and require passing preflight, then
start the apply in the original terminal:

```sh
./bin/bootwright apply --context "$CONTEXT" --stage controller --yes
./bin/bootwright preflight controller --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --stage infrastructure-services --yes
./bin/bootwright preflight container-cluster --context "$CONTEXT"
./bin/bootwright apply --context "$CONTEXT" --yes
```

For B462, while that apply runs, repeat this observation in the second terminal:

```sh
sudo KUBECONFIG=/var/lib/bootwright-clusters/lab-sno/sno/auth/kubeconfig oc get clusterversion
```

Perform the interruption selected above and wait for apply to exit. The
B462 removal is also interrupted, once status shows the media block removed;
add-on removal runs to completion. Start removal in the original terminal:

```sh
./bin/bootwright machine stop --context "$CONTEXT" --name sno-01 --yes
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
```

For B462, press Ctrl-C after the media block is removed. Check that kubeconfig
export still succeeds and prints one `access.unproved` warning to stderr,
then finish removal:

```sh
./bin/bootwright cluster kubeconfig --context "$CONTEXT" --name sno > /dev/null
./bin/bootwright destroy --context "$CONTEXT" --authorize data-loss
```

For an enabled add-on, let the destroy finish and require its removal to
complete by retirement. A missed B462 timing or add-on readiness phase leaves
that acceptance incomplete. In all selections, inspect removal before deleting
the context:

```sh
./bin/bootwright status --context "$CONTEXT"
./bin/bootwright context delete --name "$CONTEXT" --purge --yes
```

## 8. Clean up the host

Complete [Restore listener firewall](../README.md#restore-listener-firewall)
with this run's `RUN_DIR`. Then remove the resolver drop-in created in step 3:

```sh
sudo rm -- /etc/systemd/resolved.conf.d/bootwright-e2e.conf
sudo systemctl restart systemd-resolved
```

If `nmstate` was present in `$HOST_CHECKS/before`, require it to remain
installed after the run; restore it only if missing:

```sh
if grep -Fxq nmstate "$HOST_CHECKS/before" && ! rpm -q nmstate; then
    sudo dnf install nmstate
fi
```

Complete [Remove diagnostic tools](../README.md#remove-diagnostic-tools).
Only after both cleanup procedures succeed, remove this run's private files:

```sh
rm -rf -- "${RUN_DIR:?Keep RUN_DIR until cleanup succeeds}" "${INPUT_DIR:?Keep INPUT_DIR until destroy completes}"
unset KUBECONFIG
```

## 9. Record the result

Record the build commit, selected inputs, installed release, phase durations,
verification results, deliberate drill outcomes and completed cleanup in the
[acceptance ledger](../../docs/acceptance.md). A run is successful only when
all steps for its selected features pass from preparation through cleanup.
