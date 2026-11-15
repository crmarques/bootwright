# lab-sno-fips-controller

For enforced staging/production isolation, dedicated egress addresses and
automated traffic assertions, use the [strict profile](isolation/runbook.md).
It reuses this case's two VMs and provisioning steps, then replaces its
original environment workload checks. It requires a new live qualification.

A FIPS single-node OpenShift cluster built from a FIPS-mode controller that is
not the libvirt host. Two contexts run on two controllers:

| | `CTX_BOOT` (`lab-sno-fips-controller-bootstrap`) | `CTX_FIPS` (`lab-sno-fips-controller`) |
| --- | --- | --- |
| Controller | this host | `fips-controller`, the RHEL 9.8 FIPS guest `CTX_BOOT` installs (`198.51.100.10`) |
| Guests on this host | `fips-controller` (4 vCPU, 4 GiB, 60 GiB) | `sno-01` (8 vCPU, 16 GiB, 120 GiB, emulated TPM) at `198.51.100.31`, with a second NIC on the VLAN trunk |
| Network | creates NAT `virbr-e2e` (`198.51.100.0/24`) | attaches to it, and to the provider-managed VLAN trunk `br-e2e-trunk` |
| Services | DNS 53 and NTP 123 on this host's address, artifacts 8443/8080, BMC `127.0.0.1:8000` | DNS 53 and artifacts 8443/8080 on `fips-controller`; NTP is `CTX_BOOT`'s |
| libvirt host | this host, local | this host as `libvirt-host`, reached as root over SSH at `198.51.100.1`; its emulated BMC listens on `198.51.100.1:8010` |

`sno-01`'s emulated BMC runs on this host, while the agent image is built and
served on `fips-controller`. Its insert therefore imports the artifact
server's certificate, verifies the fetch and removes the certificate after
the boot ([boot media](../../specs/openshift-clusters.md#boot-media)).

It accepted [B743](../../specs/milestones/delivered.md#x294--a-fips-cluster-built-from-a-controller-off-its-libvirt-host) and, by the owner's choice, [B722](../../specs/milestones/delivered.md#x294--a-fips-cluster-built-from-a-controller-off-its-libvirt-host)
on one node, and its run of 2026-11-05 accepted the items below
([X302](../../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node)).
Its default input also encrypts the node's disk under its TPM2
([B720](../../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node)) and plugs the node into two
isolated environments (D231):

| | staging | production |
| --- | --- | --- |
| VLAN on the trunk | 10 | 20 |
| Network | `192.0.2.0/24` | `203.0.113.0/24` |
| Node, on its Localnet bridge | `192.0.2.31` on `br-stg` | `203.0.113.31` on `br-prd` |
| App container on this host | `192.0.2.50` | `203.0.113.50` |
| MetalLB pool, its IngressController's only | `192.0.2.200`–`.207` | `203.0.113.200`–`.207` |
| Pod addresses | the Network less the addresses above | the same |

The node's own network stays `virbr-e2e`, with the default ingress. Bootwright
builds the secondary networks, the NMState and MetalLB add-ons and the pools,
whose `serviceAllocation` ties each pool to its IngressController
([B1123](../../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node),
[B726](../../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node),
[B729](../../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node),
[B889](../../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node),
[B755](../../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node),
[B757](../../specs/milestones/delivered.md#x302--two-isolated-environments-on-one-node)). Bootwright creates the host trunk and VLAN legs through managed `Network`
objects. The runbook creates only the external app containers and their Podman
attachment networks on this host, and applies with `oc` the
namespaces, workloads, IngressControllers, EgressFirewalls and NetworkPolicies under
`workloads/`, which are outside Bootwright's
[scope](../../specs/project.md#product-scope). One ledger row per item run.

MetalLB on a secondary network needs IP forwarding on its interface, and
Bootwright enables IPv4 forwarding on `br-stg` and `br-prd` alone
([B1231](../../specs/milestones/m4.md#b1231)), never cluster-wide. The node
still forwards between those two bridges, so a client that routes through the
node's address could cross; the app containers carry no such route, and
keeping the environments apart on the node is GitOps configuration, not
Bootwright's ([D246](../../specs/milestones/backlog.md#d246)).

Steps marked **host** run in one Bash session in the checkout being tested on
this host. Steps marked **FIPS controller** run in an interactive session on
`fips-controller` opened in step 4. Stop on any unexpected failure; that
attempt is not a successful run. Use Bash with `set -euo pipefail` in both
sessions. The base `workloads/` checks use the default addresses and names;
use the strict profile's rendered fixtures when overriding that topology.

## Inputs

| `--set` | Default | Effect |
| --- | --- | --- |
| `security.tpm2=false` | `true` | No TPM2-bound LUKS on `sno-01`'s installation disk |
| `environments.enabled=false` | `true` | No trunk, secondary networks, add-ons or environment checks |
| `addons.subscriptions.nmstate.channel`, `.startingCSV` | empty | Required while environments are enabled; step 2 reads them |
| `addons.subscriptions.metallb.channel`, `.startingCSV` | empty | Required while environments are enabled; step 2 reads them |

[values.yaml](values.yaml) holds the rest: addresses, the release
(`openshift.version`), the guest sizes and both BMC ports.

### Declarative setup and remaining gaps

| Setup | Current owner and runbook action |
| --- | --- |
| Controller packages, host firewall grants, management NAT network, VMs, emulated BMCs, FIPS and TPM2 installation | Existing Bootwright Machine, Network, provider and install APIs; apply performs the setup. No manual firewall rules or libvirt definitions. |
| DNS records, forwarding, NTP and artifacts | Existing managed infrastructure APIs; both contexts declare their services. No manual zone-file or artifact-server setup. |
| Node NICs/VLANs, secondary bridges and address pools | NetworkConfig, OpenShiftCluster secondary networks and the NMState/MetalLB add-ons. |
| Host trunk, environment VLAN legs and test peers | Managed tagged Network objects create an unaddressed trunk and addressed host VLAN legs with NAT and inter-leg routing disabled. Only external peer containers and their Podman attachments remain test fixtures. |
| FIPS controller resolver handoff | NetworkConfig installs the bootstrap resolver. DNSServer.forwardZones routes the cluster suffix to the FIPS controller; unrelated names use declared upstreams without a forwarding loop. No manual nmstate or resolver-file update. |
| Namespaces, ingress shards, EgressIP, policies and strict node filter | Native declarative OpenShift fixtures, applied explicitly for this test. Existing [GitOps bootstrap](../../specs/api/addons.md#package-gitops) can own them in an estate using an approved repository/revision/path and a suitable ClusterRole. No such repository or additional operator is assumed here. |
| SSH authorization, credential import, negative tests and evidence | Operator trust establishment and test assertions. They are not infrastructure lifecycle configuration. |

The `ClusterAddon.manifestSet` arm is currently refused by execution; it is
not a way to apply these native fixtures. No arbitrary-command add-on is
needed. The addressless-VLAN candidate is optional; this case uses managed addressed
host legs. Its DNS forwarding is declared through the managed service API.

## 1. Prepare (host)

- Complete [host preparation](../README.md#prepare-the-host), including RHEL
  9.8 media and `~/openshift-pull-secret`. No other case may run: this one
  uses `virbr-e2e`, `198.51.100.0/24` and the service ports.
- This OpenShift case also installs a RHEL controller VM: complete the shared
  RHEL media-import step. Require both case contexts absent before a fresh
  replay. If an old context remains, finish its destroy, preserve evidence
  and purge it first; use its registering executable if the new build refuses
  its saved state. Do not overwrite a running context with fresh inputs.
- Capacity: `CTX_BOOT` needs 5 CPUs, 6 GiB of `MemAvailable` and 70 GiB free
  under `/var/lib/libvirt/images`. With `fips-controller` running, `CTX_FIPS`
  needs 9 more CPUs not reserved by libvirt domains, 18 GiB of `MemAvailable`
  and 130 GiB free. A 16-CPU, 32 GiB laptop fits both only when nothing else
  runs: no test suites or other builds until cleanup, since the laptop's
  out-of-memory killer takes the largest guest.
- Record the build commit and require `Source clean` in `bootwright version`.
  B743 needs the build containing X294.
- This host's sshd must accept root key logins. Record whether sshd was
  running so that cleanup restores it:

  ```sh
  set -euo pipefail
  umask 077
  RUN_DIR=$(mktemp -d "$HOME/bootwright-sno.XXXXXX")
  CASE="$PWD/e2e/lab-sno-fips-controller"
  VALUES="$RUN_DIR/values.yaml"
  printf '# Private case overrides\n' > "$VALUES"
  printf 'Run records: %s\n' "$RUN_DIR"
  systemctl is-active sshd > "$RUN_DIR/sshd-before" || true
  sudo sshd -T | grep -i '^permitrootlogin'     # yes or prohibit-password
  sudo systemctl start sshd
  ssh-keygen -lf /etc/ssh/ssh_host_ecdsa_key.pub
  ```

Merge private overrides into `VALUES` before rendering. Complete step 2
through `PIN_VALUES` before rendering the strict profile, then continue its
instructions. Keep this host run directory through cleanup.

## 2. Render and build the bootstrap context (host)

Use the release-matched RHEL 9 client in a private directory instead of
relying on an unrelated `oc` on PATH. The same verified binary is transferred
to the FIPS controller in step 3:

```sh
RELEASE=4.22.21
ARCHIVE="openshift-client-linux-amd64-rhel9-$RELEASE.tar.gz"
CLIENT_BASE="https://mirror.openshift.com/pub/openshift-v4/clients/ocp/$RELEASE"
mkdir -p "$RUN_DIR/clients"
curl -fL "$CLIENT_BASE/$ARCHIVE" -o "$RUN_DIR/clients/$ARCHIVE"
curl -fL "$CLIENT_BASE/sha256sum.txt" -o "$RUN_DIR/clients/sha256sum.txt"
(
    cd "$RUN_DIR/clients"
    sha256sum --ignore-missing -c sha256sum.txt
    tar -xzf "$ARCHIVE" oc
)
export PATH="$RUN_DIR/clients:$PATH"
oc version --client
```

Read the NMState and MetalLB operators' default channel and its head CSV from
the release's Red Hat operator catalog, with this host's `oc` and pull secret:

```sh
CATALOG="registry.redhat.io/redhat/redhat-operator-index:v4.22"
head_csv() {
    local package="$1" directory="$RUN_DIR/catalog/$1"
    mkdir -p "$directory"
    oc image extract "$CATALOG" -a ~/openshift-pull-secret --confirm \
        --path "/configs/$package/:$directory" > /dev/null
    jq -ers '
        (map(select(.schema == "olm.package")) | if length == 1 then .[0].defaultChannel else error("No unique package") end) as $channel
        | (map(select(.schema == "olm.channel" and .name == $channel)) | if length == 1 then .[0].entries else error("No unique channel") end) as $entries
        | ([$entries[] | (.replaces // empty), (.skips // [])[]]) as $old
        | ([$entries[].name] - $old | unique) as $heads
        | if ($channel | type) == "string" and ($heads | length) == 1
          then [$channel, $heads[0]] | @tsv else error("No unique channel head") end' "$directory"/*.json
}
NMSTATE_PIN=$(head_csv kubernetes-nmstate-operator)
METALLB_PIN=$(head_csv metallb-operator)
read -r NMSTATE_CHANNEL NMSTATE_CSV <<< "$NMSTATE_PIN"
read -r METALLB_CHANNEL METALLB_CSV <<< "$METALLB_PIN"
test -n "$NMSTATE_CHANNEL" && test -n "$NMSTATE_CSV"
test -n "$METALLB_CHANNEL" && test -n "$METALLB_CSV"
printf '%s %s\n%s %s\n' "$NMSTATE_CHANNEL" "$NMSTATE_CSV" "$METALLB_CHANNEL" "$METALLB_CSV"
```

Each line must name a channel such as `stable` and one CSV of that package.
If `jq` reports no `*.json` file, the catalog ships another format: stop and
paste `ls` of `$RUN_DIR/catalog/<package>`.
Pin the selected operator releases and the current host address in private
values before rendering. Keep that address stable for the run; a DHCP
reservation is preferable to changing frozen inputs after a network move.

```sh
HOST_ADDRESS=$(ip -4 route get 1.1.1.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')
test -n "$HOST_ADDRESS"
PIN_VALUES="$RUN_DIR/pins.yaml"
cat > "$PIN_VALUES" <<EOF
host:
  address: "$HOST_ADDRESS"

addons:
  subscriptions:
    nmstate:
      channel: "$NMSTATE_CHANNEL"
      startingCSV: "$NMSTATE_CSV"

    metallb:
      channel: "$METALLB_CHANNEL"
      startingCSV: "$METALLB_CSV"
EOF
```

### Render and provision

For the strict profile, render its manifests now using both private values
files, then continue below. Keep those files unchanged through provisioning.

```sh
e2e/render.sh lab-sno-fips-controller \
    --values "$VALUES" --values "$PIN_VALUES"
CTX_BOOT="lab-sno-fips-controller-bootstrap"
CTX_FIPS="lab-sno-fips-controller"
INPUT_DIR="$HOME/bootwright-e2e/lab-sno-fips-controller"
CONTROLLER_ADDRESS="$HOST_ADDRESS"
PROFILE="${PROFILE:-$RUN_DIR/isolation}"
declare -p RUN_DIR CASE VALUES PIN_VALUES INPUT_DIR CTX_BOOT CTX_FIPS HOST_ADDRESS PROFILE PATH > "$RUN_DIR/host-env.sh"
if [ -n "${HOST_CHECKS:-}" ]; then
    cp -a "$HOST_CHECKS" "$RUN_DIR/host-checks"
    HOST_CHECKS="$RUN_DIR/host-checks"
    declare -p HOST_CHECKS >> "$RUN_DIR/host-env.sh"
fi
printf 'Restore host variables with: source %s/host-env.sh\n' "$RUN_DIR"
./bin/bootwright validate -f "$INPUT_DIR/bootstrap"
./bin/bootwright validate -f "$INPUT_DIR/fips"
./bin/bootwright context init --name "$CTX_BOOT" --input-dir "$INPUT_DIR/bootstrap"
./bin/bootwright secret generate --context "$CTX_BOOT"
./bin/bootwright secret check --context "$CTX_BOOT"
./bin/bootwright plan --context "$CTX_BOOT"
./bin/bootwright apply --context "$CTX_BOOT" --stage controller --yes
CONTEXT="$CTX_BOOT"
```

Complete [Prepare listener firewall](../README.md#prepare-listener-firewall)
in this shell, then:

```sh
./bin/bootwright preflight --context "$CTX_BOOT" --stage controller
./bin/bootwright apply --context "$CTX_BOOT" --stage infrastructure-services --yes
./bin/bootwright apply --context "$CTX_BOOT" --yes
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- cat /proc/sys/crypto/fips_enabled
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- update-crypto-policies --show
```

The two reads must print `1` and `FIPS`.

## 3. Hand the controller its inputs (host)

Refresh sudo authorization in this host terminal immediately before piped
transfers; their standard input carries payloads and cannot answer a sudo
password prompt. Install this build, the rendered `fips` input and the pull secret on
`fips-controller`. Each travels on standard input, and the two checksums must
match:

```sh
sudo -v
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- sudo -n install -m 0755 /dev/stdin /usr/local/bin/bootwright < bin/bootwright
sha256sum bin/bootwright
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- sha256sum /usr/local/bin/bootwright
# Copy the independent diagnostic client verified in step 2:
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- sudo -n install -m 0755 /dev/stdin /usr/local/bin/oc < "$RUN_DIR/clients/oc"
./bin/bootwright machine cp --context "$CTX_BOOT" --name fips-controller "local:$INPUT_DIR/fips" remote:fips
./bin/bootwright machine cp --context "$CTX_BOOT" --name fips-controller "local:$HOME/openshift-pull-secret" remote:openshift-pull-secret
```

Hand over `libvirt-host`'s ECDSA host key as one known-hosts line for its
guest-network address, read from this host's own key file. A FIPS-mode
`ssh-keyscan` cannot fetch it, because it offers a key exchange the FIPS
build refuses (`unsupported KEX method mlkem768x25519-sha256`):

```sh
printf '198.51.100.1 %s\n' "$(cut -d' ' -f1,2 /etc/ssh/ssh_host_ecdsa_key.pub)" | \
    ./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- sh -c 'umask 077; cat > libvirt-host.known_hosts'
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- dnf repolist
```

`dnf repolist` must list `bootwright-dvd-baseos` and `bootwright-dvd-appstream`.

## 4. Create the FIPS context (FIPS controller)

Open the session and keep it for the FIPS controller steps, 4 to 10:

```sh
./bin/bootwright machine rsh --context "$CTX_BOOT" --name fips-controller
```

On `fips-controller` (the version must name the same clean build as the host):

```sh
set -euo pipefail
CTX_FIPS="lab-sno-fips-controller"
umask 077
RUN_DIR="$HOME/bootwright-sno-controller"
PROFILE="$HOME/isolation"
MANIFESTS="$PROFILE/rendered/sno-environment-isolation/templates"
mkdir -p -m 0700 "$RUN_DIR"
declare -p CTX_FIPS RUN_DIR PROFILE MANIFESTS > "$RUN_DIR/controller-env.sh"
bootwright version
bootwright controller install --yes
bootwright controller check                     # the FIPS mode check reports 1
bootwright validate -f ~/fips
bootwright context init --name "$CTX_FIPS" --input-dir ~/fips
bootwright secret set --context "$CTX_FIPS" --name openshift-pull-secret --value-file ~/openshift-pull-secret --yes
bootwright secret generate --context "$CTX_FIPS"
```

Pin `libvirt-host`'s ECDSA host key. Its fingerprint must equal the one step
1 printed on the host:

```sh
ssh-keygen -lf ~/libvirt-host.known_hosts
bootwright secret set --context "$CTX_FIPS" --name libvirt-host-known-hosts --value-file ~/libvirt-host.known_hosts --yes
bootwright secret show --context "$CTX_FIPS" --name libvirt-host-key --part public-key > ~/libvirt-host-key.pub
```

## 5. Authorize the controller on the libvirt host (host)

In a second host terminal in the checkout, authorize the key for root, only
from `fips-controller`'s address, keeping the previous file for cleanup:

```sh
set -euo pipefail
umask 077
CTX_BOOT="lab-sno-fips-controller-bootstrap"
read -r -p 'Private host run directory printed in step 1: ' RUN_DIR
source "$RUN_DIR/host-env.sh"
sudo -v
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- cat libvirt-host-key.pub > "$RUN_DIR/libvirt-host-key.pub"
AUTH_LINE="from=\"198.51.100.10\" $(cat "$RUN_DIR/libvirt-host-key.pub")"
if ! test -e "$RUN_DIR/root-authorized_keys.before" && ! test -e "$RUN_DIR/root-authorized_keys.created"; then
    if sudo test -e /root/.ssh/authorized_keys; then
        sudo cp -a /root/.ssh/authorized_keys "$RUN_DIR/root-authorized_keys.before"
    else
        touch "$RUN_DIR/root-authorized_keys.created"
    fi
fi
if sudo grep -Fxq "$AUTH_LINE" /root/.ssh/authorized_keys 2>/dev/null; then
    printf 'Controller key already authorized\n'
else
    sudo test -d /root/.ssh || touch "$RUN_DIR/root-ssh-created"
    sudo install -d -m 0700 /root/.ssh
    printf '%s\n' "$AUTH_LINE" | sudo tee -a /root/.ssh/authorized_keys > /dev/null
fi
sudo restorecon -R /root/.ssh
```

## 6. Managed environment networks

The staging and production `Network` objects select `provided: false` and the
libvirt provider, sharing `environments.trunk.bridge`. They declare their VLAN
and host address, `forward: none`, and no `routedNetworkRefs`. The existing
[managed VLAN API](../../specs/api/networks.md#tagged-managed-networks) creates
persistent host links and refuses traffic between the host legs. The staging
segment carries both VLANs to the node's second NIC. No manual bridge or VLAN
creation is needed; step 7's substrate stage realizes them.

The trunk itself is unaddressed; each host VLAN leg holds the `hostAddress`
from [values.yaml](values.yaml), without advertising a default gateway. These
host rules do not stop the SNO node routing between its own interfaces; use
the strict profile for that boundary. The external app containers and their
Podman attachment networks remain E2E fixtures, created after the substrate.

## 7. Build the FIPS context's services (FIPS controller)

```sh
bootwright secret check --context "$CTX_FIPS"
bootwright plan --context "$CTX_FIPS"          # no controller.unsupported: this controller is FIPS
bootwright apply --context "$CTX_FIPS" --stage controller --yes
CONTEXT="$CTX_FIPS"
CONTROLLER_ADDRESS="198.51.100.10"
```

Complete [Prepare listener firewall](../README.md#prepare-listener-firewall)
for the controller stage's automatic grants. Then build the services and the
substrate on `libvirt-host`:

```sh
bootwright preflight --context "$CTX_FIPS" --stage controller
bootwright apply --context "$CTX_FIPS" --stage infrastructure-services,substrates --yes
dig @198.51.100.10 +short api.sno.e2e.example.test test.apps.sno.e2e.example.test   # both 198.51.100.31
dig @198.51.100.10 +short mirror.openshift.com                                       # declared upstreams
```

The controller uses the bootstrap resolver installed by its Machine's
`NetworkConfig.dns`. The bootstrap DNSServer's `forwardZones` sends the SNO
zone to this controller's managed DNS service. Both resolvers send unrelated
queries to `host.upstreamDNS`; the controller resolver never forwards back to
the bootstrap service. No manual NetworkManager or resolver-file change is
needed, and the DNS target can start after bootstrap downloads are complete.

Verify resolution through the controller's actual system resolver, then let
preflight prove all installer names and capacity:

```sh
getent ahostsv4 api.sno.e2e.example.test
getent ahostsv4 api-int.sno.e2e.example.test
bootwright preflight --context "$CTX_FIPS" --stage clusters
```

Every `resolution.*` and `capacity.*` check must be `passed`.

In the second host terminal, confirm the libvirt host still admits the guest
network the node joins:

```sh
sudo firewall-cmd --get-zone-of-interface=virbr-e2e                 # trusted
```

With `environments.enabled`, the provider now owns the trunk and VLAN legs.
In the host terminal, verify them, then create the external peers. The strict
profile replaces this fixture block and starts its peers only after the node
filter is proved. Stop if either Podman network or app container already exists.
Require `bridge-nf-call-iptables` absent or 0 before creating the peers.

```sh
ip -d link show br-e2e-trunk
ip -4 address show br-e2e-trunk.10                       # hostAddress on staging
ip -4 address show br-e2e-trunk.20                       # hostAddress on production
sysctl -n net.bridge.bridge-nf-call-iptables 2>/dev/null  # absent or 0
sudo podman network create --internal -d macvlan -o parent=br-e2e-trunk.10 --subnet 192.0.2.0/24 e2e-staging
sudo podman network create --internal -d macvlan -o parent=br-e2e-trunk.20 --subnet 203.0.113.0/24 e2e-production
for env in staging production; do
    case "$env" in staging) ip=192.0.2.50 ;; production) ip=203.0.113.50 ;; esac
    sudo podman run -d --name "e2e-$env-app" --network "e2e-$env" --ip "$ip" \
        registry.access.redhat.com/ubi9/python-312 \
        /bin/sh -c "mkdir -p /tmp/www && echo $env-host > /tmp/www/index.html && exec python3 -m http.server 8080 --directory /tmp/www"
done
sudo podman ps --filter name=e2e- --format '{{.Names}} {{.Status}}'
```

A macvlan peer is unreachable from this host itself; use `podman exec` for
its traffic checks. Remove the peers before destroying the provider's links.

## 8. Install and interrupt (FIPS controller)

| Phase | Action | Required result on the next apply |
| --- | --- | --- |
| Image build | Ctrl-C | The image rebuilds |
| Installation wait | Ctrl-C | Installation continues |

```sh
bootwright plan --context "$CTX_FIPS"
bootwright apply --context "$CTX_FIPS" --yes
```

After each interruption:

```sh
bootwright status --context "$CTX_FIPS"
bootwright apply --context "$CTX_FIPS" --yes
```

The image build must run the FIPS-capable installer without a FIPS refusal,
and `sno-01`'s first boot must succeed through the verified fetch. An insert
that fails on certificate verification with the server's own certificate (for
example, the emulator's OpenSSL refusing the self-signed leaf as its anchor) is
a product finding, not a pass: record the exact message. Allow over an hour. Keep the operation-log directory the apply names, and record each
phase's duration.

The image build keeps the installer's own output out of the operation log,
because its inputs hold the pull secret. When the build fails, read the
installer's log on this controller:

```sh
sudo tail -n 60 /var/lib/bootwright-hosted/lab-sno-fips-controller/clusters/sno/.openshift_install.log
```

Run no `CTX_BOOT` command on the host while a `CTX_FIPS` apply runs: both
contexts drive the host through the same staging root, and a bootstrap run
reclaims run directories it does not own ([B1095](../../specs/milestones/backlog.md#b1095)).

### Diagnose an add-on failure after installation

An add-on failure leaves the completed SNO installation in place. Read the
failed block's log using the path the CLI prints. If an older build hides the
reason at **Publish bounded cluster object evidence**, inspect OLM through
the independent client. Keep the kubeconfig private:

```sh
umask 077
KUBECONFIG="$RUN_DIR/kubeconfig"; export KUBECONFIG
bootwright cluster kubeconfig --context "$CTX_FIPS" --name sno > "$KUBECONFIG"
oc get subscriptions.operators.coreos.com -A -o json | jq '[.items[] |
    {namespace:.metadata.namespace,name:.metadata.name,
     pin:.spec.startingCSV,installed:.status.installedCSV,
     installPlanRef:.status.installPlanRef,conditions:.status.conditions}]'
oc get installplans.operators.coreos.com -A -o json | jq '[.items[] |
    {namespace:.metadata.namespace,name:.metadata.name,
     ownerReferences:.metadata.ownerReferences,spec:.spec,
     phase:.status.phase,
     bundles:((.status.bundleLookups // []) | map({identifier,catalogSourceRef}))}]'
```

`RequiresApproval` alone is not a request for manual approval. Bootwright
checks the selected plan's ownership, exact CSV pin and declared catalog
before approving it. After resolving the reported cause, continue the
existing apply with its original input, secrets and build:

```sh
bootwright apply --context "$CTX_FIPS" --yes
```

This reuses the completed cluster and rechecks failed add-ons; it does not
require regenerating inputs or restarting cluster installation.

## 9. Verify (FIPS controller)

Use the `oc` executable already qualified in this controller's dependency
closure. `cluster oc` returns a descriptor; it does not execute the client.
Export the kubeconfig explicitly into the private run directory, then create
a local wrapper with its own working directory, cache and clean environment:

```sh
bootwright status --context "$CTX_FIPS"
bootwright cluster info --context "$CTX_FIPS" --name sno
bootwright diff --context "$CTX_FIPS" --observe        # clean
bootwright apply --context "$CTX_FIPS" --yes           # settles: boots nothing
KUBECONFIG="$RUN_DIR/kubeconfig"; export KUBECONFIG
bootwright cluster kubeconfig --context "$CTX_FIPS" --name sno > "$KUBECONFIG"
bootwright cluster oc --context "$CTX_FIPS" --name sno --output json -- get nodes > "$RUN_DIR/oc-handoff.json"
jq -er '.result.client.executable | select(startswith("/"))' "$RUN_DIR/oc-handoff.json" > "$RUN_DIR/oc-client"
mkdir -m 700 "$RUN_DIR/oc-home" "$RUN_DIR/oc-cache"
cat > "$RUN_DIR/oc" <<'SH'
#!/bin/sh
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
IFS= read -r client < "$here/oc-client"
cd -- "$here"
exec env -i HOME="$here/oc-home" PATH=/usr/bin:/bin \
    "$client" --kubeconfig "$here/kubeconfig" --cache-dir "$here/oc-cache" \
    "$@"
SH
chmod 700 "$RUN_DIR/oc"
oc() { "$RUN_DIR/oc" "$@"; }
oc get nodes; oc get clusterversion; oc get co
oc debug node/master-0 -- chroot /host cat /proc/sys/crypto/fips_enabled   # 1
oc debug node/master-0 -- chroot /host lsblk -f                            # with tpm2: a crypto_LUKS root
```

### Disk encryption (with `security.tpm2`)

The root must be LUKS bound to the TPM2, and unlock with no one at the
console:

```sh
LUKS=$(oc debug node/master-0 -- chroot /host lsblk -nrpo NAME,FSTYPE 2>/dev/null | awk '$2 == "crypto_LUKS" {print $1; exit}'); echo "$LUKS"
oc debug node/master-0 -- chroot /host clevis luks list -d "$LUKS"         # 1: tpm2 '{...}'
BOOT=$(oc get node master-0 -o jsonpath='{.status.nodeInfo.bootID}')
oc debug node/master-0 -- chroot /host shutdown -r +1                      # the pod ends first: one it never ends reruns on each boot
deadline=$((SECONDS + 1200))
while :; do
    AFTER=$(oc get node master-0 --request-timeout=30s -o jsonpath='{.status.nodeInfo.bootID}' 2>/dev/null || true)
    [[ -n "$AFTER" && "$AFTER" != "$BOOT" ]] && break
    if ((SECONDS >= deadline)); then echo 'node did not reboot within 20 minutes'; exit 1; fi
    sleep 20
done
oc wait node/master-0 --for=condition=Ready --timeout=20m
oc debug node/master-0 -- chroot /host cat /proc/sys/crypto/fips_enabled   # still 1
oc debug node/master-0 -- chroot /host clevis luks list -d "$LUKS"         # still tpm2
oc get co                                                                  # every operator Available again
```

### Environments (with `environments.enabled`)

Hand the workload manifests to this session from the host, then apply them
here. In the second host terminal:

```sh
tar -C e2e/lab-sno-fips-controller -cf - workloads | \
    ./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- tar -xf -
```

On the FIPS controller, keep the default ingress off both environments, then
apply each environment and its ingress:

```sh
oc patch ingresscontroller/default -n openshift-ingress-operator --type=merge \
    -p '{"spec":{"namespaceSelector":{"matchExpressions":[{"key":"lab.example.test/environment","operator":"DoesNotExist"}]}}}'
for env in staging production; do
    oc apply -f ~/workloads/$env/namespace.yaml
    deadline=$((SECONDS + 600))
    until oc get network-attachment-definitions -n "$env" "$env" --request-timeout=30s > /dev/null 2>&1; do
        if ((SECONDS >= deadline)); then echo 'NAD did not appear within 10 minutes'; exit 1; fi
        sleep 5
    done
    oc get network-attachment-definitions -n "$env"                       # exactly one, named $env
    oc apply -f ~/workloads/$env/
    oc apply -f ~/workloads/ingress/$env.yaml
done
lb_ip() { oc get svc -n openshift-ingress "router-$1" --request-timeout=30s -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null; }
for env in staging production; do
    oc wait -n "$env" deployment/app --for=condition=Available --timeout=10m
    oc wait -n "$env" pod/probe --for=condition=Ready --timeout=10m
    for _ in $(seq 60); do [ -n "$(lb_ip "$env")" ] && break; sleep 5; done
    echo "router-$env $(lb_ip "$env")"
    test -n "$(lb_ip "$env")"
done
```

The staging router must hold an address in `192.0.2.200`–`.207` and the
production router one in `203.0.113.200`–`.207`: each pool serves only its
own IngressController. An empty address after five minutes means the pool
selected nothing.

Read each pod's address on its environment network:

```sh
net_ip() { oc get pod -n "$1" "$2" -o jsonpath='{.metadata.annotations.k8s\.v1\.cni\.cncf\.io/network-status}' | jq -r --arg net "$1/$1" '.[] | select(.name == $net) | .ips[0]'; }
STG_APP=$(net_ip staging "$(oc get pod -n staging -l app=app -o name | cut -d/ -f2)")
PRD_APP=$(net_ip production "$(oc get pod -n production -l app=app -o name | cut -d/ -f2)")
STG_LB=$(lb_ip staging)
PRD_LB=$(lb_ip production)
RTR_STG=$(oc get pod -n openshift-ingress -l ingresscontroller.operator.openshift.io/deployment-ingresscontroller=staging -o jsonpath='{.items[0].status.podIP}')
RTR_PRD=$(oc get pod -n openshift-ingress -l ingresscontroller.operator.openshift.io/deployment-ingresscontroller=production -o jsonpath='{.items[0].status.podIP}')
NP_STG=$(oc get svc -n openshift-ingress router-staging -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}')
NP_PRD=$(oc get svc -n openshift-ingress router-production -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}')
printf 'STG_APP=%s PRD_APP=%s STG_LB=%s PRD_LB=%s\n' "$STG_APP" "$PRD_APP" "$STG_LB" "$PRD_LB"
PROBE='import sys, urllib.request, urllib.error
headers = {"Host": sys.argv[2]} if len(sys.argv) > 2 else {}
try:
    print(urllib.request.urlopen(urllib.request.Request(sys.argv[1], headers=headers), timeout=5).status)
except urllib.error.HTTPError as error:
    print(error.code)
except Exception:
    print("000")'
probe() { oc exec -n "$1" probe -- python3 -c "$PROBE" "$2" ${3:+"$3"}; }
check() { got=$(probe "$1" "$2" ${4:+"$4"} | tr -d '\r'); printf 'got=%s want=%s  %s -> %s %s\n' "$got" "$3" "$1" "$2" "${4:-}"; test "$got" = "$3"; }
```

Inside each environment everything answers, from OpenShift and from the
host, and nothing crosses to the other one. The environment networks are
separate VLANs, and each EgressFirewall denies the other environment's
network. The ingress operator admits any source to every router, so each
environment's egress NetworkPolicy keeps its pods off the other router: by
its MetalLB address, its pod address and its NodePort alike. In this
session, every assertion must exit successfully:

```sh
check staging    "http://$STG_APP:8080/"            200    # pod to pod on the staging network
check staging    "http://app.staging.svc:8080/"     200    # pod to pod through the cluster network
check staging    "http://192.0.2.50:8080/"          200    # pod to the staging host app
check staging    "http://$STG_LB/"                  200 app.staging.apps.sno.e2e.example.test    # pod through its own ingress
check staging    "http://203.0.113.50:8080/"        000    # the production host app
check staging    "http://$PRD_APP:8080/"            000    # a production pod
check staging    "http://app.production.svc:8080/"  000    # production through the cluster network
check staging    "http://$PRD_LB/"                  000    # the production ingress
check staging    "http://$PRD_LB/"                  000 app.production.apps.sno.e2e.example.test # the production app through it
check staging    "http://$RTR_PRD/"                 000 app.production.apps.sno.e2e.example.test # the production router pod
check staging    "http://198.51.100.31:$NP_PRD/"    000 app.production.apps.sno.e2e.example.test # its NodePort
check production "http://$PRD_APP:8080/"            200
check production "http://app.production.svc:8080/" 200
check production "http://203.0.113.50:8080/"        200
check production "http://$PRD_LB/"                  200 app.production.apps.sno.e2e.example.test
check production "http://192.0.2.50:8080/"          000
check production "http://$STG_APP:8080/"            000
check production "http://app.staging.svc:8080/"     000
check production "http://$STG_LB/"                  000
check production "http://$STG_LB/"                  000 app.staging.apps.sno.e2e.example.test
check production "http://$RTR_STG/"                 000 app.staging.apps.sno.e2e.example.test
check production "http://198.51.100.31:$NP_STG/"    000 app.staging.apps.sno.e2e.example.test
```

In the second host terminal, each host app reaches its own ingress and its
own pods, never the other environment's. Set `STG_LB`, `PRD_LB`, `STG_APP`
and `PRD_APP` there to the four addresses printed above:

```sh
host_probe() { sudo podman exec "e2e-$1-app" python3 -c "import sys, urllib.request; r = urllib.request.Request(sys.argv[1], headers={'Host': sys.argv[2]} if len(sys.argv) > 2 else {}); print(urllib.request.urlopen(r, timeout=5).read().decode().strip())" "$2" ${3:+"$3"} 2>&1 | tail -1; }
host_probe staging "http://$STG_LB/" app.staging.apps.sno.e2e.example.test           # staging-openshift
host_probe staging "http://$STG_APP:8080/"                                           # staging-openshift
host_probe staging "http://$PRD_LB/" app.production.apps.sno.e2e.example.test        # a timeout or unreachable
host_probe staging "http://203.0.113.50:8080/"                                       # a timeout or unreachable
host_probe staging "http://$PRD_APP:8080/"                                           # a timeout or unreachable
host_probe production "http://$PRD_LB/" app.production.apps.sno.e2e.example.test     # production-openshift
host_probe production "http://$PRD_APP:8080/"                                        # production-openshift
host_probe production "http://$STG_LB/" app.staging.apps.sno.e2e.example.test        # a timeout or unreachable
host_probe production "http://192.0.2.50:8080/"                                      # a timeout or unreachable
```

Back on the FIPS controller, the default ingress still serves the cluster and
admits no environment route:

```sh
oc get route -n staging app -o jsonpath='{range .status.ingress[*]}{.routerName}{"\n"}{end}'      # staging only
oc get route -n production app -o jsonpath='{range .status.ingress[*]}{.routerName}{"\n"}{end}'   # production only
curl -sk -o /dev/null -w '%{http_code}\n' "https://console-openshift-console.apps.sno.e2e.example.test/"   # 200
```

Forwarding is enabled on the two bridges and nowhere else Bootwright
configures, and the cluster does not forward on every interface:

```sh
oc get network.operator cluster -o jsonpath='{.spec.defaultNetwork.ovnKubernetesConfig.gatewayConfig.ipForwarding}{"\n"}'   # empty or Restricted
oc get nncp bootwright-master-0 -o jsonpath='{.status.conditions[?(@.type=="Available")].status}{"\n"}'                      # True
oc debug node/master-0 -- chroot /host sysctl -n net.ipv4.conf.br-stg.forwarding net.ipv4.conf.br-prd.forwarding               # 1 and 1
```

Record each answer. Remove the environments before the destroy, so no
LoadBalancer Service outlives its pool:

```sh
for env in staging production; do
    oc delete -f ~/workloads/ingress/$env.yaml --wait=true
    oc delete namespace "$env" --wait=true
done
```

### Verified-fetch drill (B743)

Prove that `sno-01`'s emulated BMC verifies the server it fetches from, and
that the eject removed the certificate. Read the BMC and artifact-server
certificates and the BMC credential:

```sh
bootwright secret show --context "$CTX_FIPS" --name lab-bmc-tls --part certificate > "$RUN_DIR/bmc.crt"
bootwright secret show --context "$CTX_FIPS" --name artifact-server-tls --part certificate > "$RUN_DIR/artifacts.crt"
bootwright secret show --context "$CTX_FIPS" --name lab-bmc-credentials --part password > "$RUN_DIR/bmc.password"
printf 'user = "admin:%s"\n' "$(cat "$RUN_DIR/bmc.password")" > "$RUN_DIR/bmc.curlrc"
bmc() { curl -sS --cacert "$RUN_DIR/bmc.crt" -K "$RUN_DIR/bmc.curlrc" -H 'Content-Type: application/json' "$@"; }
BASE="https://198.51.100.1:8010/redfish/v1"
SYSTEM=$(bmc "$BASE/Systems" | jq -r '.Members[0]."@odata.id"')     # sno-01 only: the allowed instance
CD="https://198.51.100.1:8010$SYSTEM/VirtualMedia/Cd"
bmc "$CD" | jq '{Inserted, VerifyCertificate}'                       # false, true: the eject keeps verification on
bmc "$CD/Certificates" | jq '."Members@odata.count"'                 # 0: the eject removed the certificate
```

Import an unrelated certificate, turn verification on and insert the server's
root. The insert must fail on certificate verification:

```sh
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -noenc -keyout /dev/null \
    -subj /CN=unrelated -days 1 -out "$RUN_DIR/unrelated.crt"
jq -n --rawfile c "$RUN_DIR/unrelated.crt" '{CertificateString: $c, CertificateType: "PEM"}' | bmc -X POST -d @- "$CD/Certificates" -D - -o /dev/null
bmc -X PATCH -d '{"VerifyCertificate": true}' "$CD"
bmc -X POST -d '{"Image": "https://198.51.100.10:8443/", "Inserted": true}' "$CD/Actions/VirtualMedia.InsertMedia"
```

Replace it with the artifact server's certificate and insert again. The
insert must now fail on the server's HTTP answer for its empty root (record
the status it names), not on verification:

```sh
CERT=$(bmc "$CD/Certificates" | jq -r '.Members[0]."@odata.id"')
bmc -X DELETE "https://198.51.100.1:8010$CERT"
jq -n --rawfile c "$RUN_DIR/artifacts.crt" '{CertificateString: $c, CertificateType: "PEM"}' | bmc -X POST -d @- "$CD/Certificates" -D - -o /dev/null
bmc -X POST -d '{"Image": "https://198.51.100.10:8443/", "Inserted": true}' "$CD/Actions/VirtualMedia.InsertMedia"
```

Restore the device and prove nothing drifted:

```sh
CERT=$(bmc "$CD/Certificates" | jq -r '.Members[0]."@odata.id"')
bmc -X DELETE "https://198.51.100.1:8010$CERT"
bmc "$CD" | jq '{Inserted, VerifyCertificate}'                       # false, true: as the eject left it
bmc "$CD/Certificates" | jq '."Members@odata.count"'                 # 0
rm -f -- "$RUN_DIR/bmc.password" "$RUN_DIR/bmc.curlrc"
bootwright diff --context "$CTX_FIPS" --observe                       # clean
```

The expected answers are this runbook's reading of the upstream sushy-tools
source ([emulated BMC facts](../../.agents/knowledge/sushy-tools-emulated-bmc.md)).
Record the exact answers of the first run.

## 10. Destroy the FIPS context (FIPS controller)

First, with `environments.enabled`, remove the external peers and their
Podman attachment networks in the **host terminal**. Bootwright must own the
remaining trunk and VLAN cleanup; never delete those links by hand.

```sh
for env in staging production; do
    sudo podman rm -f "e2e-$env-app"
    sudo podman network rm "e2e-$env"
done
```

Then, in the **FIPS controller** session:

```sh
bootwright plan --context "$CTX_FIPS" --output json > "$RUN_DIR/destroy-plan.json"
AUTH=$(jq -er '.result | select(.verb == "destroy") | .authorizations | select(length == 1) | .[0] | select(startswith("data-loss="))' "$RUN_DIR/destroy-plan.json")
if bootwright destroy --context "$CTX_FIPS" --authorize "$AUTH" --yes \
    --output json > "$RUN_DIR/destroy-while-running.json"; then
    echo 'unexpected success: expected powered-on refusal'; exit 1
fi
jq -e '.diagnostics | any(.code == "lifecycle.live" and (.message | contains("sno-01")) and (.message | contains("its domain is running")))' \
    "$RUN_DIR/destroy-while-running.json"
bootwright machine stop --context "$CTX_FIPS" --name sno-01 --yes
bootwright plan --context "$CTX_FIPS" --output json > "$RUN_DIR/destroy-plan-stopped.json"
AUTH=$(jq -er '.result | select(.verb == "destroy") | .authorizations | select(length == 1) | .[0] | select(startswith("data-loss="))' "$RUN_DIR/destroy-plan-stopped.json")
bootwright destroy --context "$CTX_FIPS" --authorize "$AUTH" --yes
bootwright status --context "$CTX_FIPS"
```

Require `sno-01`, its disks, its BMC and pool on `libvirt-host`, the media, the
work area and the services to be removed. A final destroy reports `done`;
desired cluster/service rows return to `PENDING` and the handoff is not ready
because the latest operation is a destroy. Those are expected.

Preserve evidence before purging. On this controller set:

```sh
EVIDENCE_CONTEXT="$CTX_FIPS"
EVIDENCE_BW=/usr/local/bin/bootwright
EVIDENCE_ROOT=$(mktemp -d "$HOME/fips-evidence.XXXXXXXX")
printf '%s\n' "$EVIDENCE_ROOT" > "$HOME/fips-evidence-path"
```

Run [Preserve evidence before context purge](../README.md#preserve-evidence-before-context-purge)
in this session with those variables. Add the selected run records and, for
the strict profile, its verifier results to this evidence root. Leave the
exported kubeconfig and temporary BMC credential out of the archive:

```sh
mkdir -m 700 "$EVIDENCE_ROOT/run-records"
for record in oc-handoff.json destroy-plan.json destroy-plan-stopped.json destroy-while-running.json dns.yml; do
    cp -- "$RUN_DIR/$record" "$EVIDENCE_ROOT/run-records/"
done
if [[ -d "$HOME/isolation" ]]; then cp -a -- "$HOME/isolation" "$EVIDENCE_ROOT/isolation"; fi
bootwright context delete --name "$CTX_FIPS" --purge --yes
```

Keep this controller VM until the host retrieves the evidence in step 11.
Then leave the session.

## 11. Destroy the bootstrap context and clean up (host)

Return to the original host shell used for steps 1–4, after leaving the FIPS
controller session. Keep that host's `CTX_BOOT`, `CTX_FIPS`, `RUN_DIR`,
`INPUT_DIR` and `HOST_CHECKS`; the second terminal and the controller session
do not carry all of them.

Before removing the controller VM, copy its evidence to the host's ignored
cache and verify the archived logs again:

```sh
FIPS_EVIDENCE=$(./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- cat fips-evidence-path)
EVIDENCE_CACHE=$(./scripts/cache-dir)
HOST_EVIDENCE="$EVIDENCE_CACHE/e2e-evidence"
git -C "$(dirname -- "$EVIDENCE_CACHE")" check-ignore -q -- "$HOST_EVIDENCE/.ignore-check"
mkdir -m 700 -p "$HOST_EVIDENCE"
FIPS_COPY=$(mktemp -d "$HOST_EVIDENCE/fips-controller.XXXXXXXX")
./bin/bootwright machine cp --context "$CTX_BOOT" --name fips-controller \
    "remote:$FIPS_EVIDENCE" "local:$FIPS_COPY/evidence"
for archive in "$FIPS_COPY"/evidence/"$CTX_FIPS".*; do
    (cd -- "$archive"; sha256sum -c SHA256SUMS)
done
printf 'FIPS evidence retained: %s\n' "$FIPS_COPY"
```

Restore root's authorized keys and sshd:

```sh
if [ -e "$RUN_DIR/root-authorized_keys.before" ]; then
    sudo cp -a "$RUN_DIR/root-authorized_keys.before" /root/.ssh/authorized_keys
    sudo cmp "$RUN_DIR/root-authorized_keys.before" /root/.ssh/authorized_keys
elif [ -e "$RUN_DIR/root-authorized_keys.created" ]; then
    sudo rm -- /root/.ssh/authorized_keys
fi
if [ -e "$RUN_DIR/root-ssh-created" ]; then sudo rmdir -- /root/.ssh || sudo ls -A /root/.ssh; fi   # list what root gained since
grep -qx active "$RUN_DIR/sshd-before" || sudo systemctl stop sshd
```

The FIPS context's destroy removed its provider-owned VLAN legs and trunk.
Confirm their absence (with `environments.enabled`); no manual link deletion
belongs in cleanup:

```sh
ip link show br-e2e-trunk 2>&1 | tail -1                  # does not exist
ip link show br-e2e-trunk.10 2>&1 | tail -1               # does not exist
ip link show br-e2e-trunk.20 2>&1 | tail -1               # does not exist
```

Remove the controller guest and the bootstrap context:

```sh
./bin/bootwright machine stop --context "$CTX_BOOT" --name fips-controller --yes
./bin/bootwright plan --context "$CTX_BOOT" --output json > "$RUN_DIR/bootstrap-destroy-plan.json"
AUTH=$(jq -er '.result | select(.verb == "destroy") | .authorizations | select(length == 1) | .[0] | select(startswith("data-loss="))' "$RUN_DIR/bootstrap-destroy-plan.json")
./bin/bootwright destroy --context "$CTX_BOOT" --authorize "$AUTH" --yes
./bin/bootwright status --context "$CTX_BOOT"
```

Set `EVIDENCE_CONTEXT="$CTX_BOOT"`, run `unset EVIDENCE_ROOT EVIDENCE_BW`, and complete
[Preserve evidence before context purge](../README.md#preserve-evidence-before-context-purge)
on this host, using its default cache location. Then purge:

```sh
./bin/bootwright context delete --name "$CTX_BOOT" --purge --yes
if virsh --connect qemu:///system list --all --name | grep bootwright-; then
    echo 'case domains remain'; exit 1
fi
```

Complete [Restore listener firewall](../README.md#restore-listener-firewall)
and [Remove diagnostic tools](../README.md#remove-diagnostic-tools). Retain
the selected host records before deleting temporary inputs. Do not copy the
root authorized-keys backup or credential files into the archive:

```sh
mkdir -m 700 "$FIPS_COPY/host-run-records"
for record in values.yaml bootstrap-destroy-plan.json isolation; do
    if [[ -e "$RUN_DIR/$record" ]]; then
        cp -a -- "$RUN_DIR/$record" "$FIPS_COPY/host-run-records/"
    fi
done
cp -a -- "$INPUT_DIR" "$FIPS_COPY/rendered-inputs"
rm -rf -- "${RUN_DIR:?}" "${INPUT_DIR:?}"
```

## Resume after a shell closes or the laptop restarts

Keep each terminal's variables distinct. In a new host terminal, enter the
checkout and restore the private run directory printed in step 1:

```sh
read -r -p 'Private host run directory: ' RUN_DIR
source "$RUN_DIR/host-env.sh"
sudo -v
./bin/bootwright controller check
ip -4 route get 1.1.1.1
./bin/bootwright machine list --context "$CTX_BOOT" --power-status
```

The host must still hold the pinned `HOST_ADDRESS`. If it moved to another
network, restore the original address/network or destroy the old realization
with its original build and input, preserve its evidence, then initialize a
fresh context rendered for the new address. Never edit an operation's frozen
input to resume it. Media and the verified client may be reused.

With the original address restored, verify the case services are ready, start
sshd, and start the existing controller VM if it is off. Managed VLAN links
are persistent; do not recreate them with `ip link`.

```sh
sudo systemctl start sshd
./bin/bootwright preflight --context "$CTX_BOOT" --stage controller
./bin/bootwright machine start --context "$CTX_BOOT" --name fips-controller
./bin/bootwright machine rsh --context "$CTX_BOOT" --name fips-controller
```

In the controller session, restore its own variables before using `RUN_DIR`:

```sh
source "$HOME/bootwright-sno-controller/controller-env.sh"
bootwright status --context "$CTX_FIPS"
```

A resumed apply uses its existing context, inputs and secrets. Repeat only
transfers that failed; a sudo refusal before a transfer requires `sudo -v` in
the host terminal, not a new render, context or secret generation. Stop on
unexpected readiness errors; a recorded completed apply does not repair
arbitrary live changes merely by being repeated.

## 12. Record the result

Record the build commit, the selected inputs, the installed release, the
phase durations, the verified-fetch drill's exact answers and the completed
cleanup in the [acceptance ledger](../../docs/acceptance.md). A run is
successful only when every step passes from preparation through cleanup.
