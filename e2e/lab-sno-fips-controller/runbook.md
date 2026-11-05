# lab-sno-fips-controller

A FIPS single-node OpenShift cluster built from a FIPS-mode controller that is
not the libvirt host. Two contexts run on two controllers:

| | `CTX_BOOT` (`lab-sno-fips-controller-bootstrap`) | `CTX_FIPS` (`lab-sno-fips-controller`) |
| --- | --- | --- |
| Controller | this host | `fips-controller`, the RHEL 9.8 FIPS guest `CTX_BOOT` installs (`198.51.100.10`) |
| Guests on this host | `fips-controller` (4 vCPU, 4 GiB, 60 GiB) | `sno-01` (8 vCPU, 16 GiB, 120 GiB, emulated TPM) at `198.51.100.31`, with a second NIC on the VLAN trunk |
| Network | creates NAT `virbr-e2e` (`198.51.100.0/24`) | attaches to it, and to the trunk bridge `br-e2e-trunk` the runbook builds (step 6) |
| Services | DNS 53 and NTP 123 on this host's address, artifacts 8443/8080, BMC `127.0.0.1:8000` | DNS 53 and artifacts 8443/8080 on `fips-controller`; NTP is `CTX_BOOT`'s |
| libvirt host | this host, local | this host as `libvirt-host`, reached as root over SSH at `198.51.100.1`; its emulated BMC listens on `198.51.100.1:8010` |

`sno-01`'s emulated BMC runs on this host, while the agent image is built and
served on `fips-controller`. Its insert therefore imports the artifact
server's certificate, verifies the fetch and removes the certificate after
the boot ([boot media](../../specs/openshift-clusters.md#boot-media)).

It accepted [B743](../../specs/milestones/delivered.md#x294--a-fips-cluster-built-from-a-controller-off-its-libvirt-host) and, by the owner's choice, [B722](../../specs/milestones/delivered.md#x294--a-fips-cluster-built-from-a-controller-off-its-libvirt-host)
on one node. Its default input also encrypts the node's disk under its TPM2
([B720](../../specs/milestones/m3.md#b720)) and plugs the node into two
isolated environments (D231):

| | staging | production |
| --- | --- | --- |
| VLAN on the trunk | 10 | 20 |
| Network | `192.0.2.0/24` | `203.0.113.0/24` |
| Node, on its Localnet bridge | `192.0.2.31` on `br-stg` | `203.0.113.31` on `br-prd` |
| App container on this host | `192.0.2.50` | `203.0.113.50` |
| MetalLB pool, its IngressController's only | `192.0.2.200`–`.207` | `203.0.113.200`–`.207` |
| Pod addresses | `192.0.2.0/25` | `203.0.113.0/25` |

The node's own network stays `virbr-e2e`, with the default ingress. Bootwright
builds the secondary networks, the NMState and MetalLB add-ons and the pools,
whose `serviceAllocation` ties each pool to its IngressController
([B1123](../../specs/milestones/m4.md#b1123),
[B726](../../specs/milestones/m3.md#b726),
[B729](../../specs/milestones/m3.md#b729),
[B889](../../specs/milestones/m3.md#b889),
[B755](../../specs/milestones/m4.md#b755),
[B757](../../specs/milestones/m4.md#b757)). The runbook itself builds the
trunk and the app containers on this host, and applies with `oc` the
namespaces, workloads, IngressControllers, EgressFirewalls and NetworkPolicies under
`workloads/`, which are outside Bootwright's
[scope](../../specs/project.md#product-scope). One ledger row per item run.

The node forwards between its two environment networks, because MetalLB on a
secondary network needs `ipForwarding: Global`: a client that routes through
the node's address could cross. The app containers carry no such route, and
the gap is [B1124](../../specs/milestones/backlog.md#b1124).

Steps marked **host** run in one Bash session in the checkout being tested on
this host. Steps marked **FIPS controller** run in an interactive session on
`fips-controller` opened in step 4. Stop on any unexpected failure; that
attempt is not a successful run.

## Inputs

| `--set` | Default | Effect |
| --- | --- | --- |
| `security.tpm2=false` | `true` | No TPM2-bound LUKS on `sno-01`'s installation disk |
| `environments.enabled=false` | `true` | No trunk, secondary networks, add-ons or environment checks |
| `addons.subscriptions.nmstate.channel`, `.startingCSV` | empty | Required while environments are enabled; step 2 reads them |
| `addons.subscriptions.metallb.channel`, `.startingCSV` | empty | Required while environments are enabled; step 2 reads them |

[values.yaml](values.yaml) holds the rest: addresses, the release
(`openshift.version`), the guest sizes and both BMC ports.

## 1. Prepare (host)

- Complete [host preparation](../README.md#prepare-the-host), including RHEL
  9.8 media and `~/openshift-pull-secret`. No other case may run: this one
  uses `virbr-e2e`, `198.51.100.0/24` and the service ports.
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
  umask 077
  RUN_DIR=$(mktemp -d)
  printf 'Run records: %s\n' "$RUN_DIR"
  systemctl is-active sshd > "$RUN_DIR/sshd-before" || true
  sudo sshd -T | grep -i '^permitrootlogin'     # yes or prohibit-password
  sudo systemctl start sshd
  ssh-keygen -lf /etc/ssh/ssh_host_ecdsa_key.pub
  ```

## 2. Render and build the bootstrap context (host)

Read the NMState and MetalLB operators' default channel and its head CSV from
the release's Red Hat operator catalog, with this host's `oc` and pull secret:

```sh
CATALOG="registry.redhat.io/redhat/redhat-operator-index:v4.22"
head_csv() {
    local package="$1" directory="$RUN_DIR/catalog/$1"
    mkdir -p "$directory"
    oc image extract "$CATALOG" -a ~/openshift-pull-secret --confirm \
        --path "/configs/$package/:$directory" > /dev/null
    jq -rs --arg package "$package" '
        (map(select(.schema == "olm.package"))[0].defaultChannel) as $channel
        | (map(select(.schema == "olm.channel" and .name == $channel))[0].entries) as $entries
        | ([$entries[] | (.replaces // empty), (.skips // [])[]]) as $old
        | [$channel, ([$entries[].name] - $old)[0]] | @tsv' "$directory"/*.json
}
read -r NMSTATE_CHANNEL NMSTATE_CSV < <(head_csv kubernetes-nmstate-operator)
read -r METALLB_CHANNEL METALLB_CSV < <(head_csv metallb-operator)
printf '%s %s\n%s %s\n' "$NMSTATE_CHANNEL" "$NMSTATE_CSV" "$METALLB_CHANNEL" "$METALLB_CSV"
```

Each line must name a channel such as `stable` and one CSV of that package.
If `jq` reports no `*.json` file, the catalog ships another format: stop and
paste `ls` of `$RUN_DIR/catalog/<package>`.
Then render:

```sh
e2e/render.sh lab-sno-fips-controller \
    --set "addons.subscriptions.nmstate.channel=$NMSTATE_CHANNEL" \
    --set "addons.subscriptions.nmstate.startingCSV=$NMSTATE_CSV" \
    --set "addons.subscriptions.metallb.channel=$METALLB_CHANNEL" \
    --set "addons.subscriptions.metallb.startingCSV=$METALLB_CSV"
CTX_BOOT="lab-sno-fips-controller-bootstrap"
CTX_FIPS="lab-sno-fips-controller"
INPUT_DIR="$HOME/bootwright-e2e/lab-sno-fips-controller"
CONTROLLER_ADDRESS="$(ip -4 route get 192.0.2.1 | sed -n 's/.* src \([0-9.]*\).*/\1/p')"
./bin/bootwright validate -f "$INPUT_DIR/bootstrap"
./bin/bootwright validate -f "$INPUT_DIR/fips"
./bin/bootwright context init --name "$CTX_BOOT" --input-dir "$INPUT_DIR/bootstrap"
./bin/bootwright secret generate --context "$CTX_BOOT"
./bin/bootwright secret check --context "$CTX_BOOT"
./bin/bootwright plan --context "$CTX_BOOT"
./bin/bootwright apply --context "$CTX_BOOT" --stage controller --yes
CONTEXT="$CTX_BOOT"
FIREWALL_PORTS=(53/tcp 53/udp 123/udp 8080/tcp 8443/tcp)
```

Complete [Prepare listener firewall](../README.md#prepare-listener-firewall)
in this shell, then:

```sh
./bin/bootwright preflight controller --context "$CTX_BOOT"
./bin/bootwright apply --context "$CTX_BOOT" --stage infrastructure-services --yes
./bin/bootwright apply --context "$CTX_BOOT" --yes
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- cat /proc/sys/crypto/fips_enabled
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- update-crypto-policies --show
```

The two reads must print `1` and `FIPS`.

## 3. Hand the controller its inputs (host)

Install this build, the rendered `fips` input and the pull secret on
`fips-controller`. Each travels on standard input, and the two checksums must
match:

```sh
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- sudo -n install -m 0755 /dev/stdin /usr/local/bin/bootwright < bin/bootwright
sha256sum bin/bootwright
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- sha256sum /usr/local/bin/bootwright
tar -C "$INPUT_DIR" -cf - fips | ./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- tar -xf -
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- sh -c 'umask 077; cat > openshift-pull-secret' < ~/openshift-pull-secret
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

On `fips-controller`:

```sh
CTX_FIPS="lab-sno-fips-controller"
umask 077
RUN_DIR=$(mktemp -d)
bootwright version
bootwright controller install --yes
bootwright preflight controller                # the FIPS mode check reports 1
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
CTX_BOOT="lab-sno-fips-controller-bootstrap"
RUN_DIR="<the run records directory step 1 printed>"
./bin/bootwright machine exec --context "$CTX_BOOT" --name fips-controller -- cat libvirt-host-key.pub > "$RUN_DIR/libvirt-host-key.pub"
sudo test -d /root/.ssh || touch "$RUN_DIR/root-ssh-created"
sudo install -d -m 0700 /root/.ssh
sudo test -e /root/.ssh/authorized_keys && sudo cp -a /root/.ssh/authorized_keys "$RUN_DIR/root-authorized_keys.before"
printf 'from="198.51.100.10" %s\n' "$(cat "$RUN_DIR/libvirt-host-key.pub")" | sudo tee -a /root/.ssh/authorized_keys
sudo restorecon -R /root/.ssh
```

## 6. Build the VLAN trunk and the environment apps (host)

In the second host terminal, as root, build the trunk bridge `sno-01`'s
second NIC joins, one VLAN interface per environment, and one app
container per environment on its VLAN. These are runtime links, gone at the
next boot; nothing here is Bootwright's:

```sh
ip link show br-e2e-trunk 2>/dev/null && echo "br-e2e-trunk exists: stop"
sysctl -n net.bridge.bridge-nf-call-iptables 2>/dev/null       # absent or 0
sudo ip link add br-e2e-trunk type bridge
sudo ip link set br-e2e-trunk up
for vlan in 10 20; do
    sudo ip link add link br-e2e-trunk name "br-e2e-trunk.$vlan" type vlan id "$vlan"
    sudo ip link set "br-e2e-trunk.$vlan" up
done
sudo podman network create -d macvlan -o parent=br-e2e-trunk.10 --subnet 192.0.2.0/24 e2e-staging
sudo podman network create -d macvlan -o parent=br-e2e-trunk.20 --subnet 203.0.113.0/24 e2e-production
for env in staging production; do
    case "$env" in staging) ip=192.0.2.50 ;; production) ip=203.0.113.50 ;; esac
    sudo podman run -d --name "e2e-$env-app" --network "e2e-$env" --ip "$ip" \
        registry.access.redhat.com/ubi9/python-312 \
        /bin/sh -c "mkdir -p /tmp/www && echo $env-host > /tmp/www/index.html && exec python3 -m http.server 8080 --directory /tmp/www"
done
sudo podman ps --filter name=e2e- --format '{{.Names}} {{.Status}}'
```

The bridge has no address and the host routes nothing between the VLANs; a
macvlan container is unreachable from this host itself, which is expected. If
`bridge-nf-call-iptables` reads 1, stop: the host firewall would filter the
trunk's bridged frames.

## 7. Build the FIPS context's services (FIPS controller)

```sh
bootwright secret check --context "$CTX_FIPS"
bootwright plan --context "$CTX_FIPS"          # no controller.unsupported: this controller is FIPS
bootwright apply --context "$CTX_FIPS" --stage controller --yes
CONTEXT="$CTX_FIPS"
CONTROLLER_ADDRESS="198.51.100.10"
FIREWALL_PORTS=(53/tcp 53/udp 8080/tcp 8443/tcp)
```

Complete [Prepare listener firewall](../README.md#prepare-listener-firewall)
in this session with interface `enp1s0`. Then build the services and the
substrate on `libvirt-host`:

```sh
bootwright preflight controller --context "$CTX_FIPS"
bootwright apply --context "$CTX_FIPS" --stage infrastructure-services,substrates --yes
dig @198.51.100.10 +short api.sno.e2e.example.test test.apps.sno.e2e.example.test   # both 198.51.100.31
dig @198.51.100.10 +short mirror.openshift.com                                       # forwarded through CTX_BOOT
```

Point this controller's resolver at its own DNS server, which forwards every
other name to `CTX_BOOT`'s, then let preflight prove resolution. The
installation configured the guest's DNS through nmstate, which stores it as
NetworkManager's global DNS and so overrides any per-connection setting.
Change it the same way:

```sh
printf 'dns-resolver:\n  config:\n    search:\n    - e2e.example.test\n    server:\n    - 198.51.100.10\n' > "$RUN_DIR/dns.yml"
sudo nmstatectl apply "$RUN_DIR/dns.yml"
grep nameserver /etc/resolv.conf                                    # 198.51.100.10
getent hosts api.sno.e2e.example.test
bootwright preflight container-cluster --context "$CTX_FIPS"
```

Every `resolution.*` and `capacity.*` check must be `passed`.

In the second host terminal, confirm the libvirt host still admits the guest
network the node joins:

```sh
sudo firewall-cmd --get-zone-of-interface=virbr-e2e                 # trusted
```

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
sudo tail -n 60 /var/lib/bootwright-clusters/lab-sno-fips-controller/sno/.openshift_install.log
```

Run no `CTX_BOOT` command on the host while a `CTX_FIPS` apply runs: both
contexts drive the host through the same staging root, and a bootstrap run
reclaims run directories it does not own ([B1095](../../specs/milestones/backlog.md#b1095)).

## 9. Verify (FIPS controller)

Install an independent `oc` matching `openshift.version` from the release's
RHEL 9 client archive, `openshift-client-linux-amd64-rhel9-<version>.tar.gz`,
checked with `sha256sum --ignore-missing -c sha256sum.txt` against the
release's published list. Then:

```sh
bootwright status --context "$CTX_FIPS"
bootwright cluster info --context "$CTX_FIPS" --name sno
bootwright diff --context "$CTX_FIPS" --observe        # clean
bootwright apply --context "$CTX_FIPS" --yes           # settles: boots nothing
KUBECONFIG="$RUN_DIR/kubeconfig"; export KUBECONFIG
bootwright cluster kubeconfig --context "$CTX_FIPS" --name sno > "$KUBECONFIG"
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
until [ "$(oc get node master-0 -o jsonpath='{.status.nodeInfo.bootID}' 2>/dev/null)" != "" ] && \
      [ "$(oc get node master-0 -o jsonpath='{.status.nodeInfo.bootID}' 2>/dev/null)" != "$BOOT" ]; do sleep 20; done
oc wait node/master-0 --for=condition=Ready --timeout=20m
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
    until oc get network-attachment-definitions -n "$env" "$env" > /dev/null 2>&1; do sleep 5; done
    oc get network-attachment-definitions -n "$env"                       # exactly one, named $env
    oc apply -f ~/workloads/$env/
    oc apply -f ~/workloads/ingress/$env.yaml
done
lb_ip() { oc get svc -n openshift-ingress "router-$1" -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null; }
for env in staging production; do
    oc wait -n "$env" deployment/app --for=condition=Available --timeout=10m
    oc wait -n "$env" pod/probe --for=condition=Ready --timeout=10m
    for _ in $(seq 60); do [ -n "$(lb_ip "$env")" ] && break; sleep 5; done
    echo "router-$env $(lb_ip "$env")"
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
check() { got=$(probe "$1" "$2" ${4:+"$4"} | tr -d '\r'); [ "$got" = "$3" ] && r=ok || r=FAIL; printf '%-4s got=%s want=%s  %s -> %s %s\n' "$r" "$got" "$3" "$1" "$2" "${4:-}"; }
```

Inside each environment everything answers, from OpenShift and from the
host, and nothing crosses to the other one. The environment networks are
separate VLANs, and each EgressFirewall denies the other environment's
network. The ingress operator admits any source to every router, so each
environment's egress NetworkPolicy keeps its pods off the other router: by
its MetalLB address, its pod address and its NodePort alike. In this
session, every line must print `ok`:

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

```sh
bootwright destroy --context "$CTX_FIPS" --authorize data-loss    # must refuse, naming sno-01
bootwright machine stop --context "$CTX_FIPS" --name sno-01 --yes
bootwright destroy --context "$CTX_FIPS" --authorize data-loss
bootwright status --context "$CTX_FIPS"
bootwright context delete --name "$CTX_FIPS" --purge --yes
```

Require `sno-01`, its disks, its BMC and pool on `libvirt-host`, the media, the
work area and the services to be removed. Then leave the session.

## 11. Destroy the bootstrap context and clean up (host)

Restore root's authorized keys and sshd:

```sh
if [ -e "$RUN_DIR/root-authorized_keys.before" ]; then
    sudo cp -a "$RUN_DIR/root-authorized_keys.before" /root/.ssh/authorized_keys
    sudo grep -c -F "$(cut -d' ' -f2 "$RUN_DIR/libvirt-host-key.pub")" /root/.ssh/authorized_keys || true   # 0
else
    sudo rm -- /root/.ssh/authorized_keys
fi
if [ -e "$RUN_DIR/root-ssh-created" ]; then sudo rmdir -- /root/.ssh || sudo ls -A /root/.ssh; fi   # list what root gained since
grep -qx active "$RUN_DIR/sshd-before" || sudo systemctl stop sshd
```

Remove the trunk and the app containers (with `environments.enabled`):

```sh
for env in staging production; do
    sudo podman rm -f "e2e-$env-app"
    sudo podman network rm "e2e-$env"
done
sudo ip link del br-e2e-trunk.10
sudo ip link del br-e2e-trunk.20
sudo ip link del br-e2e-trunk
ip link show br-e2e-trunk 2>&1 | tail -1                                   # does not exist
```

Remove the controller guest and the bootstrap context:

```sh
./bin/bootwright machine stop --context "$CTX_BOOT" --name fips-controller --yes
./bin/bootwright destroy --context "$CTX_BOOT" --authorize data-loss
./bin/bootwright status --context "$CTX_BOOT"
./bin/bootwright context delete --name "$CTX_BOOT" --purge --yes
virsh --connect qemu:///system list --all --name | grep bootwright-     # nothing
```

Complete [Restore listener firewall](../README.md#restore-listener-firewall)
and [Remove diagnostic tools](../README.md#remove-diagnostic-tools), then:

```sh
rm -rf -- "${RUN_DIR:?}" "${INPUT_DIR:?}"
```

## 12. Record the result

Record the build commit, the selected inputs, the installed release, the
phase durations, the verified-fetch drill's exact answers and the completed
cleanup in the [acceptance ledger](../../docs/acceptance.md). A run is
successful only when every step passes from preparation through cleanup.
