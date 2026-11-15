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
| Bootwright core | Existing Machine, Network, NetworkConfig, provider and OpenShiftCluster fields: two VMs, installation, FIPS, TPM2, node VLAN connectivity and addressed-bridge intent |
| Existing add-ons | NMState applies the bridge configuration; MetalLB provides per-environment address pools |
| E2E fixtures, later GitOps-owned in an estate | Namespaces, apps, ingress shards, EgressIP, policies, node filter, external peers and assertions |

No new core API or add-on is required. The native schema follows the
[product boundary](../../../specs/project.md#product-scope). The
[IngressController API](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/operator_apis/ingresscontroller-operator-openshift-io-v1)
provides namespace selection and source ranges.
[Secondary-interface EgressIP](https://docs.redhat.com/en/documentation/openshift_container_platform/4.22/html/ovn-kubernetes_network_plugin/configuring-egress-ips-ovn)
is documented for bare metal, with cloud/hypervisor restrictions. This
libvirt/platform None rehearsal must prove assignment and the source address
seen by each external server. An unassigned address or fallback node source
is a failure, never permission to disable the assertion. This profile is not
a Red Hat support certification or a claim of FIPS certification for Bootwright.

The staging and production VLANs have no router between them. The SNO node
has interfaces on both, so the fixture also installs a dedicated nftables
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
Keep the parent's fixed release, FIPS and TPM2 enabled. Follow parent step 1 for the local KVM/capacity checks, media and credentials.
Render this profile below before parent step 2 applies either context, then
continue parent steps 2–5 with the same private values, operator CSV pins and
FIPS controller bootstrap. Keep all actual overrides and evidence outside
tracked files.
The parent defaults reserve egress `.60` outside Localnet IPAM and MetalLB.

Set `CASE` to this case's absolute directory, `PROFILE` to a private directory
outside the checkout, and `VALUES` to the same private overrides passed to
`e2e/render.sh`. An empty mapping is sufficient when using the case defaults
and setting operator subscriptions on the renderer's command line.
Resolve the approved UBI Python image by digest with the installed `oc`:

```sh
CASE="$PWD/e2e/lab-sno-fips-controller"
PROFILE="$RUN_DIR/isolation"
VALUES="$RUN_DIR/isolation-values.yaml"
mkdir -m 700 -p "$PROFILE"
printf '# Private case overrides\n' > "$VALUES"
IMAGE=registry.access.redhat.com/ubi9/python-312
DIGEST=$(oc image info "$IMAGE" -o json | jq -er '.digest')
printf '\nisolation:\n  image: %s@%s\n' "$IMAGE" "$DIGEST" >> "$VALUES"
helm template isolation "$CASE/isolation" -f "$CASE/values.yaml" -f "$VALUES" \
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
cp -a "$CASE/app" "$PROFILE/app"
python3 "$PROFILE/app/verify.py" --config "$PROFILE/config.json" --mode config
```

If overriding addresses, VLANs, bridges, image or domain, merge those values
into this private file before rendering, and pass this same file to the parent
Bootwright renderer. Both renders must use identical effective values. The
verification configuration travels with the manifests. No environment-specific
facts belong in the chart or Git metadata.

## Replace parent step 6: isolated host networks

Create only the trunk and macvlan networks before provisioning SNO. External
peers start after the node filter is proved. Commands below require `jq`,
`ip`, `nsenter` and Podman on the host. Stop if any intended link, network or
container name already exists; never adopt or delete a pre-existing resource.
Run this block in Bash with `set -euo pipefail`.

```sh
TRUNK=$(jq -er '.trunk.bridge' "$PROFILE/config.json")
if ip link show "$TRUNK" >/dev/null 2>&1; then echo 'trunk already exists'; exit 1; fi
sysctl -n net.bridge.bridge-nf-call-iptables 2>/dev/null || true
# Require absent or 0 above, as in the parent runbook.
sudo ip link add "$TRUNK" type bridge
sudo ip link set "$TRUNK" up
while IFS=$'\t' read -r env vlan cidr; do
    sudo ip link add link "$TRUNK" name "$TRUNK.$vlan" type vlan id "$vlan"
    sudo ip link set "$TRUNK.$vlan" up
    sudo podman network create --internal -d macvlan -o "parent=$TRUNK.$vlan" \
        --subnet "$cidr" "e2e-$env"
done < <(jq -r '.environments | to_entries[] | [.key,.value.vlan,.value.cidr] | @tsv' "$PROFILE/config.json")
```

The trunk has no host address. Neither peer receives a management NIC or
an Internet route. Continue parent steps 7–9 through the initial FIPS and
LUKS checks, skipping the original **Environments** subsection. Both VMs
remain on the separate management network for installation and administration.

## Apply the node filter before workloads

Transfer the profile from the host to the FIPS controller using the parent's
`CTX_BOOT` session:

```sh
tar -C "$PROFILE" -cf - . | \
    ./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- \
    sh -c 'umask 077; mkdir -p isolation; tar -xf - -C isolation'
```

On the FIPS controller, use the parent's exported `KUBECONFIG`:

```sh
PROFILE="$HOME/isolation"
MANIFESTS="$PROFILE/rendered/sno-environment-isolation/templates"
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
oc get mcp master -o yaml
oc get node master-0 -o yaml
oc wait mcp/master --for=condition=Updated --timeout=30m
oc wait node/master-0 --for=condition=Ready --timeout=10m
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
oc label node/master-0 k8s.ovn.org/egress-assignable= --overwrite
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
    --mode pods --snapshot "$PROFILE/snapshot.json"
```

Every positive and negative assertion must pass; any command, HTTP, parsing
or readiness failure exits nonzero. Positive responses from an external app
must report the environment's exact EgressIP as their source. A snapshot is
written only after the pod checks pass. On the host, retrieve it and run the
external assertions:

```sh
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- \
    cat isolation/snapshot.json > "$PROFILE/snapshot.json"
sudo python3 "$PROFILE/app/verify.py" --config "$PROFILE/config.json" \
    --mode external --snapshot "$PROFILE/snapshot.json"
```

The matrix covers both directions: own app/service/ingress success, cross
namespace pod/service denial, other router pod/ClusterIP/VIP/NodePort denial,
other external app denial, and management denial. Any HTTP response, including
403 or 503, fails a negative connectivity assertion. API/exec errors are not
network isolation evidence. Positive controls bracket negative probes.

Repeat the parent's TPM2 reboot drill with a bounded 20-minute boot-ID change
wait. Require root `crypto_LUKS`, a Clevis TPM2 binding, unattended restart,
`fips_enabled=1`, Ready node, healthy operators and an active filter with its
rules. Rerun both verifier modes with a new snapshot after the reboot. Failure
to recover in the bound is a failure; never let an unbounded loop count as a
pass. This proves persistence of encryption and filtering together.

## Evidence and cleanup

Keep rendered inputs, image digest, release/operator pins, exact commands,
verifier output, EgressIP assignment, server-observed source addresses,
filter counters before/after probes, boot IDs and the parent's operation logs
in the private evidence directory. Record no live acceptance until the full
sequence passes and the owner accepts it.

Remove external containers and networks, then their VLAN interfaces and trunk,
using the parent cleanup only for the objects this run created. Delete the
fixture ingress controllers and namespaces before destroying the cluster so
Services do not outlive MetalLB pools. Delete the two `EgressIP` objects. The
filter remains installed until the disposable VM is destroyed; deleting it
while apps are present would reopen transit. Restore the default selector
from the saved object if keeping the cluster for diagnosis. Continue the
parent's verified-fetch drill, destroy, evidence preservation and controller
cleanup. The parent uses fixed default names; with custom values use the
names in the rendered verification configuration instead.
