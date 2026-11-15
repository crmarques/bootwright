# Strict staging and production profile

Reuse [lab-sno-fips-controller](../runbook.md): one FIPS RHEL controller VM
and one FIPS SNO VM with TPM2-bound LUKS, both on the local libvirt host.
This profile replaces the original environment workloads with primary OVN
pods, two ingress controllers, two egress identities and explicit transit
filtering. It is an IPv4 laboratory case awaiting [B1400](../../../specs/milestones/m3.md#b1400)'s
live qualification. A shared node is not a boundary against a cluster or host
administrator; workloads have no host networking or privileged capabilities.

| Owner | Configuration |
| --- | --- |
| Bootwright core | Machine, Network, NetworkConfig, DNSServer, provider and OpenShiftCluster fields: two VMs, installation, FIPS, TPM2, managed host trunk/VLAN legs, DNS suffix forwarding, node VLAN connectivity, addressed bridges and egress-node eligibility |
| Existing add-ons | NMState applies the bridge configuration; MetalLB provides per-environment address pools |
| Native fixtures, later GitOps-owned in an estate | Namespaces, apps, ingress shards, EgressIP, policies and node filter |
| External E2E harness | External peers, adversarial routes and traffic assertions |

The networking uses existing core fields and add-ons. The case also uses the
DNSServer `forwardZones` field to resolve the cluster zone without a manual
controller resolver switch.
The parent [setup audit](../runbook.md#declarative-setup-and-remaining-gaps)
names the current API owners. The native schema follows the
[product boundary](../../../specs/project.md#product-scope). The
[IngressController API](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/operator_apis/ingresscontroller-operator-openshift-io-v1)
provides namespace selection and source ranges.
[Secondary-interface EgressIP](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/ovn-kubernetes_network_plugin/configuring-egress-ips-ovn)
is documented for bare metal, with cloud/hypervisor restrictions. This
libvirt/platform None rehearsal must prove assignment and the source address
seen by each external server. An unassigned address or fallback node source
is a failure, never permission to disable the assertion. This profile is not
a Red Hat support certification or a claim of FIPS certification for Bootwright.

The provider host refuses routing between its staging and production VLAN
legs; both managed Networks declare `forward: none` and no `routedNetworkRefs`.
The SNO node has interfaces on both, so the fixture also installs a dedicated nftables
filter before exposing workloads: it admits ingress HTTP/HTTPS only on the
matching environment pool and admits replies to connections initiated by the
cluster. Other new connections entering either environment interface are
refused, including attempts to use SNO as a router to the other environment,
its NodePorts, service network or management address. Workload NetworkPolicies
allow only their own namespace, their own router, DNS and their own external
network on the fixture's HTTP port. Pods have no Localnet attachment that
would bypass those policies. DNS and cluster control traffic are necessary
shared infrastructure; applications cannot connect across environments.

## Prepare and render on the host

Use a fresh disposable case. Do not apply the original `workloads/` tree.
Keep the parent's fixed release, FIPS and TPM2 enabled. Complete parent step 1
for the local KVM/capacity checks, media and credentials, then step 2 through
writing `PIN_VALUES`. Pause before its **Render and provision** subsection.
Render this profile below, then finish parent steps 2–5 with the same private
values, operator CSV pins and FIPS controller bootstrap. Keep all actual
overrides and evidence outside tracked files.
The parent defaults reserve egress `.60` outside Localnet IPAM and MetalLB.

Parent step 2 installs the release-matched `oc` and writes operator pins and
the stable host address to `PIN_VALUES`. Keep it alongside the private
overrides in `VALUES`; both files must reach both renderers.
Resolve the approved UBI Python image's Linux amd64 manifest by digest with
the installed `oc`. The tag is multi-architecture, so select this case's
platform explicitly. Stop on a failed lookup before writing the values file
or rendering either input:

```sh
CASE="$PWD/e2e/lab-sno-fips-controller"
PROFILE="$RUN_DIR/isolation"
VALUES="${VALUES:-$RUN_DIR/values.yaml}"
mkdir -m 700 -p "$PROFILE"
test -f "$VALUES" || printf '# Private case overrides\n' > "$VALUES"
IMAGE=registry.access.redhat.com/ubi9/python-312:latest
DIGEST=$(oc image info "$IMAGE" --filter-by-os=linux/amd64 -o json | jq -er '.digest | select(test("^sha256:[a-f0-9]{64}$"))')
printf '\nisolation:\n  image: %s@%s\n' "$IMAGE" "$DIGEST" >> "$VALUES"
helm template isolation "$CASE/isolation" \
    -f "$CASE/values.yaml" -f "$VALUES" -f "$PIN_VALUES" \
    --output-dir "$PROFILE/rendered"
MANIFESTS="$PROFILE/rendered/sno-environment-isolation/templates"
python3 - "$MANIFESTS/verification-config.yaml" "$PROFILE/config.json" <<'PY'
import json
import pathlib
import sys
# The rendered block contains exactly one JSON line; no YAML dependency needed.
lines = pathlib.Path(sys.argv[1]).read_text().splitlines()
value = next(json.loads(line.strip()) for line in lines if line.lstrip().startswith('{'))
pathlib.Path(sys.argv[2]).write_text(json.dumps(value) + '\n')
PY
mkdir -p "$PROFILE/app"
cp -a "$CASE/app/." "$PROFILE/app/"
python3 "$PROFILE/app/verify.py" --config "$PROFILE/config.json" --mode config
```

The API-managed DNS chain is described in parent step 7; the controller keeps
its installed bootstrap resolver, and no manual `nmstatectl` step belongs in
a fresh run. Source the saved host or controller environment file from the
parent's resume section when reopening a terminal.
Use the parent's pinned executable through the saved `PATH` for host and
controller commands, including snapshot transfers and cleanup. A newer build
in the checkout does not replace this run's executable.

If overriding addresses, VLANs, bridges, image or domain, merge those values
into this private file before rendering, and pass this same file to the parent
Bootwright renderer. Both renders must use identical effective values. The
verification configuration travels with the manifests. No environment-specific
facts belong in the chart or Git metadata. Now perform parent steps 2–5.

## Replace parent step 6: managed host networks and peer attachments

Bootwright creates the persistent trunk and addressed VLAN legs from the
existing managed `Network` API. Run parent step 7 through its
`apply --stage infrastructure-services,substrates` on the FIPS controller
first. Skip the parent's external-peer fixture block: this profile starts
peers only after the node filter is proved. Do not create or delete the host
links by hand.

In the host terminal, verify the links Bootwright created, then create only
the case's Podman macvlan attachment networks. Stop if a Podman network or
container of the intended name already exists. The host's `hostAddress` on
each leg is reserved outside the pod, peer, egress and MetalLB allocations.
Run this block in Bash with `set -euo pipefail`; require bridge filtering
absent or 0 before creating the attachments.

```sh
PROFILE="$RUN_DIR/isolation"
TRUNK=$(jq -er '.trunk.bridge' "$PROFILE/config.json")
ip -d link show "$TRUNK"
BRIDGE_FILTER=$(sysctl -n net.bridge.bridge-nf-call-iptables 2>/dev/null || true)
[[ -z "$BRIDGE_FILTER" || "$BRIDGE_FILTER" == 0 ]]
for env in staging production; do
    if sudo podman network exists "e2e-$env" || sudo podman container exists "e2e-$env-app"; then
        echo 'test peer resource already exists'; exit 1
    fi
done
while IFS=$'\t' read -r env vlan cidr; do
    ip -4 address show "$TRUNK.$vlan"
    sudo podman network create --internal -d macvlan -o "parent=$TRUNK.$vlan" \
        --subnet "$cidr" "e2e-$env"
done < <(jq -r '.environments | to_entries[] | [.key,.value.vlan,.value.cidr] | @tsv' "$PROFILE/config.json")
```

The trunk itself has no host address. Neither peer receives a management NIC
or an Internet route. Complete parent step 7's system-resolver and preflight
checks, then
steps 8–9 through the initial FIPS and LUKS checks, skipping the original
**Environments** subsection. Both VMs remain on the separate management
network for installation and administration.

If the FIPS context was initialized from the earlier provided-network input
but has not started an apply, transfer the regenerated `fips` directory and
run `bootwright context update --name "$CTX_FIPS" --input-dir "$HOME/fips" --yes`
on the FIPS controller before its first plan. If an apply has registered,
keep its frozen input and complete its destroy before importing the changed
network ownership. A pre-existing fixture bridge is never silently adopted.

## Apply the node filter before workloads

Transfer the profile from the host to the FIPS controller using the parent's
`CTX_BOOT` session:

```sh
bootwright machine cp --context "$CTX_BOOT" --name fips-controller \
    "local:$PROFILE" remote:isolation
```

On the FIPS controller, keep the parent's `RUN_DIR`, private client wrapper and
`oc` shell function from step 9. Do not reuse the host's `RUN_DIR` value here:

```sh
PROFILE="$HOME/isolation"
MANIFESTS="$PROFILE/rendered/sno-environment-isolation/templates"
oc get node master-0 -o jsonpath='{.status.nodeInfo.bootID}{"\n"}' > "$PROFILE/boot-before-filter"
oc apply --dry-run=server -f "$MANIFESTS/node-isolation.yaml"
oc apply -f "$MANIFESTS/node-isolation.yaml"
```

This MachineConfig can reboot the SNO node. Wait until the master pool's
`spec.configuration.name` equals `status.configuration.name`, the node's
`machineconfiguration.openshift.io/currentConfig` equals its `desiredConfig`,
and the following checks pass. A stale `Updated=True` immediately after apply
is insufficient. Bound the rollout to 30 minutes; on timeout collect status
and stop. Confirm the rendered master config contains this MachineConfig.

```sh
deadline=$((SECONDS + 1800))
while :; do
    if oc get mcp master --request-timeout=30s -o json > "$PROFILE/mcp.json" &&
       oc get node master-0 --request-timeout=30s -o json > "$PROFILE/node.json" &&
       jq -e '
           [.spec.configuration.source[]? | select(.kind == "MachineConfig") | .name] |
           index("99-sno-e2e-isolation") != null' "$PROFILE/mcp.json" >/dev/null &&
       jq -e '.spec.configuration.name as $wanted |
           ($wanted | type == "string" and length > 0) and
           .status.configuration.name == $wanted and
           any(.status.conditions[]?; .type == "Updated" and .status == "True") and
           any(.status.conditions[]?; .type == "Degraded" and .status == "False")' "$PROFILE/mcp.json" >/dev/null; then
        wanted=$(jq -er '.spec.configuration.name' "$PROFILE/mcp.json")
        if jq -e --arg wanted "$wanted" '.metadata.annotations |
            .["machineconfiguration.openshift.io/currentConfig"] == $wanted and
            .["machineconfiguration.openshift.io/desiredConfig"] == $wanted and
            .["machineconfiguration.openshift.io/state"] == "Done"' "$PROFILE/node.json" >/dev/null; then
            break
        fi
    fi
    if ((SECONDS >= deadline)); then
        echo 'isolation MachineConfig did not converge within 30 minutes'; exit 1
    fi
    sleep 10
done
oc wait mcp/master --for=condition=Updated --timeout=30m
oc wait node/master-0 --for=condition=Ready --timeout=10m
oc get node master-0 -o json | jq -e '
    .metadata.annotations as $a
    | ($a["machineconfiguration.openshift.io/currentConfig"] != null)
      and ($a["machineconfiguration.openshift.io/currentConfig"] == $a["machineconfiguration.openshift.io/desiredConfig"])
'
oc debug node/master-0 -- chroot /host systemctl is-active sno-e2e-isolation.service
oc debug node/master-0 -- chroot /host nft list table inet sno_e2e_isolation
```

Require the unit active and both configured interfaces/subnets/pools present.
The unit only manages its own table, never the OVN ruleset. Failed loading
blocks dependent services; recover through the disposable VM console if
necessary, collect evidence, and destroy/recreate the case. Do not weaken the
filter to make a test pass. This fixture is not a general node firewall API.

Save the original default ingress selector, then exclude these namespaces.
Apply the fixture objects only after the filter is confirmed:

```sh
oc get ingresscontroller default -n openshift-ingress-operator -o json > "$PROFILE/default-ingress.before.json"
oc patch ingresscontroller/default -n openshift-ingress-operator --type=merge \
    -p '{"spec":{"namespaceSelector":{"matchExpressions":[{"key":"lab.example.test/environment","operator":"DoesNotExist"}]}}}'
oc get node master-0 -o json | jq -e '.metadata.labels | has("k8s.ovn.org/egress-assignable")'
oc apply -f "$MANIFESTS/namespaces.yaml"
for file in networking verification-config applications; do
    oc apply --dry-run=server -f "$MANIFESTS/$file.yaml"
    oc apply -f "$MANIFESTS/$file.yaml"
done
for env in staging production; do
    oc rollout status -n "$env" deployment/app --timeout=10m
    oc wait -n "$env" pod/probe --for=condition=Ready --timeout=10m
done
oc get egressip -o yaml
oc get svc -n openshift-ingress router-staging router-production
deadline=$((SECONDS + 300))
while :; do
    if oc get egressip staging production --request-timeout=30s -o json > "$PROFILE/egress.json" &&
       jq -e --slurpfile config "$PROFILE/config.json" '
           .items | length == 2 and all(.[];
               .metadata.name as $env | .status.items | length == 1 and
               .[0].node == "master-0" and
               .[0].egressIP == $config[0].environments[$env].egressAddress)' "$PROFILE/egress.json" >/dev/null; then
        break
    fi
    if ((SECONDS >= deadline)); then echo 'EgressIP assignment timed out'; exit 1; fi
    sleep 5
done
```

Require one assigned EgressIP per environment on `master-0` and each router
VIP inside its matching pool. Wait at most five minutes for assignments;
otherwise record failure and stop. Confirm each route is admitted only by its
matching router. Inspect `oc get infrastructure cluster -o yaml` and retain the platform identity in private evidence. Do not change the platform identity to force EgressIP support.

## Start peers and deliberately route through SNO

On the host, start the two external HTTP apps using the same pinned image.
The containers retain no network administration capability. Host `nsenter`
adds routes in their network namespaces to try routing through the SNO node;
this is an adversarial test, not normal application configuration.

```sh
IMAGE=$(jq -er '.image' "$PROFILE/config.json")
while IFS=$'\t' read -r env address; do
    sudo podman run -d --name "e2e-$env-app" --network "e2e-$env" --ip "$address" \
        --cap-drop=ALL --security-opt=no-new-privileges --read-only \
        --user 1001 --env "ENVIRONMENT=$env" \
        -v "$PROFILE/app/server.py:/opt/e2e/server.py:ro,z" \
        "$IMAGE" python3 -B /opt/e2e/server.py
done < <(jq -r '.environments | to_entries[] | [.key,.value.appAddress] | @tsv' "$PROFILE/config.json")
while IFS=$'\t' read -r env gateway; do
    PID=$(sudo podman inspect --format '{{.State.Pid}}' "e2e-$env-app")
    test "$PID" -gt 1
    # Routes cover both the other VLAN and management/cluster-service bypasses.
    sudo nsenter -t "$PID" -n ip route add default via "$gateway"
done < <(jq -r '.environments | to_entries[] | [.key,.value.nodeAddress] | @tsv' "$PROFILE/config.json")
```

These default routes deliberately point at SNO; the node filter, not a missing
route, must enforce the boundary. Only the host test harness can install them.

## Assert traffic and security, then reboot and repeat

On the FIPS controller:

```sh
python3 "$PROFILE/app/verify.py" --config "$PROFILE/config.json" \
    --mode pods --oc "$RUN_DIR/oc" --snapshot "$PROFILE/snapshot.json" | tee "$PROFILE/pods-before-reboot.log"
```

Every positive and negative assertion must pass; any command, HTTP, parsing
or readiness failure exits nonzero. Positive responses from an external app
must report the environment's exact EgressIP as their source. A snapshot is
written only after the pod checks pass. On the host, retrieve it and run the
external assertions:

```sh
bootwright machine cp --context "$CTX_BOOT" --name fips-controller \
    remote:isolation/snapshot.json "local:$PROFILE/snapshot.json"
sudo python3 "$PROFILE/app/verify.py" --config "$PROFILE/config.json" \
    --mode external --snapshot "$PROFILE/snapshot.json" | tee "$PROFILE/external-before-reboot.log"
```

The matrix covers both directions: own app/service/ingress success, cross
namespace pod/service denial, other router pod/ClusterIP/VIP/NodePort denial,
other external app denial, and management denial. Any HTTP response, including
403 or 503, fails a negative connectivity assertion. API/exec errors are not
network isolation evidence. Positive controls bracket negative probes.

Repeat the parent's TPM2 reboot drill with its bounded 20-minute boot-ID change
wait. Require root `crypto_LUKS`, a Clevis TPM2 binding, unattended restart,
`fips_enabled=1`, Ready node, healthy operators and an active filter with its
rules. Rerun both verifier modes with a new snapshot after the reboot; retain
separate before/after verifier logs with `tee` and Bash `pipefail`.
On the FIPS controller:

```sh
oc debug node/master-0 -- chroot /host systemctl is-active sno-e2e-isolation.service
oc debug node/master-0 -- chroot /host nft list table inet sno_e2e_isolation | tee "$PROFILE/filter-after-reboot.log"
python3 "$PROFILE/app/verify.py" --config "$PROFILE/config.json" \
    --mode pods --oc "$RUN_DIR/oc" --snapshot "$PROFILE/snapshot-after-reboot.json" | tee "$PROFILE/pods-after-reboot.log"
```

On the host:

```sh
bootwright machine cp --context "$CTX_BOOT" --name fips-controller \
    remote:isolation/snapshot-after-reboot.json "local:$PROFILE/snapshot-after-reboot.json"
sudo python3 "$PROFILE/app/verify.py" --config "$PROFILE/config.json" \
    --mode external --snapshot "$PROFILE/snapshot-after-reboot.json" | tee "$PROFILE/external-after-reboot.log"
```

Failure to recover in the bound is a failure; never let an unbounded loop
count as a pass. This proves persistence of encryption and filtering together.

## Evidence and cleanup

Keep rendered inputs, image digest, release/operator pins, exact commands,
verifier output, EgressIP assignment, server-observed source addresses,
filter counters before/after probes, boot IDs and the parent's operation logs
in the private evidence directory. Record no live acceptance until the full
sequence passes and the owner accepts it.

Before parent step 10, remove the fixture ingress controllers and namespaces
so Services do not outlive MetalLB pools, and delete the two EgressIP objects:

```sh
oc delete ingresscontroller staging production -n openshift-ingress-operator --wait=true --timeout=5m
oc delete egressip staging production --wait=true --timeout=5m
oc delete namespace staging production --wait=true --timeout=10m
jq '{spec: {namespaceSelector: (.spec.namespaceSelector // null)}}' \
    "$PROFILE/default-ingress.before.json" > "$PROFILE/restore-selector.json"
oc patch ingresscontroller/default -n openshift-ingress-operator --type=merge \
    --patch-file "$PROFILE/restore-selector.json"
```

These are fresh-case fixtures; do not use their names to delete another
operator's pre-existing objects. The
filter remains installed until the disposable VM is destroyed; deleting it
while apps are present would reopen transit. Continue the parent's
verified-fetch drill, destroy and evidence preservation. Parent step 10 removes
the external peer containers and attachment networks before Bootwright removes
its managed trunk, VLAN legs and host routing rules. Never delete those links
by hand. Preserve all evidence before either context is
purged or the controller VM is removed.
