# lab-sno-fips-controller

A FIPS single-node OpenShift cluster built from a FIPS-mode controller that is
not the libvirt host. Two contexts run on two controllers:

| | `CTX_BOOT` (`lab-sno-fips-controller-bootstrap`) | `CTX_FIPS` (`lab-sno-fips-controller`) |
| --- | --- | --- |
| Controller | this host | `fips-controller`, the RHEL 9.8 FIPS guest `CTX_BOOT` installs (`198.51.100.10`) |
| Guests on this host | `fips-controller` (4 vCPU, 6 GiB, 60 GiB) | `sno-01` (8 vCPU, 16 GiB, 120 GiB, emulated TPM) at `198.51.100.31` |
| Network | creates NAT `virbr-e2e` (`198.51.100.0/24`) | attaches to it |
| Services | DNS 53 and NTP 123 on this host's address, artifacts 8443/8080, BMC `127.0.0.1:8000` | DNS 53 and artifacts 8443/8080 on `fips-controller`; NTP is `CTX_BOOT`'s |
| libvirt host | this host, local | this host as `libvirt-host`, reached as root over SSH at `198.51.100.1`; its emulated BMC listens on `198.51.100.1:8010` |

`sno-01`'s emulated BMC runs on this host, while the agent image is built and
served on `fips-controller`. Its insert therefore imports the artifact
server's certificate, verifies the fetch and removes the certificate after
the boot ([boot media](../../specs/openshift-clusters.md#boot-media)).

It accepts [B743](../../specs/milestones/m3.md#b743) and rehearses
[B722](../../specs/milestones/m3.md#b722) on one node, one ledger row per item
run. `security.tpm2=true` adds TPM2-bound LUKS on the node's disk, rehearsing
[B720](../../specs/milestones/m3.md#b720).

Steps marked **host** run in one Bash session in the checkout being tested on
this host. Steps marked **FIPS controller** run in an interactive session on
`fips-controller` opened in step 4. Stop on any unexpected failure; that
attempt is not a successful run.

## Inputs

| `--set` | Default | Effect |
| --- | --- | --- |
| `security.tpm2=true` | `false` | TPM2-bound LUKS on `sno-01`'s installation disk |

[values.yaml](values.yaml) holds the rest: addresses, the release
(`openshift.version`), the guest sizes and both BMC ports.

## 1. Prepare (host)

- Complete [host preparation](../README.md#prepare-the-host), including RHEL
  9.8 media and `~/openshift-pull-secret`. No other case may run: this one
  uses `virbr-e2e`, `198.51.100.0/24` and the service ports.
- Capacity: `CTX_BOOT` needs 5 CPUs, 8 GiB of `MemAvailable` and 70 GiB free
  under `/var/lib/libvirt/images`. With `fips-controller` running, `CTX_FIPS`
  needs 9 more CPUs not reserved by libvirt domains, 18 GiB of `MemAvailable`
  and 130 GiB free. A 16-CPU, 32 GiB laptop fits both only when nothing else
  runs.
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

```sh
TPM2_ENABLED="false"
e2e/render.sh lab-sno-fips-controller --set "security.tpm2=$TPM2_ENABLED"
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

Open the session and keep it for steps 4 to 8:

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

## 6. Build the FIPS context's services (FIPS controller)

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

## 7. Install and interrupt (FIPS controller)

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

## 8. Verify (FIPS controller)

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

## 9. Destroy the FIPS context (FIPS controller)

```sh
bootwright destroy --context "$CTX_FIPS" --authorize data-loss    # must refuse, naming sno-01
bootwright machine stop --context "$CTX_FIPS" --name sno-01 --yes
bootwright destroy --context "$CTX_FIPS" --authorize data-loss
bootwright status --context "$CTX_FIPS"
bootwright context delete --name "$CTX_FIPS" --purge --yes
```

Require `sno-01`, its disks, its BMC and pool on `libvirt-host`, the media, the
work area and the services to be removed. Then leave the session.

## 10. Destroy the bootstrap context and clean up (host)

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

## 11. Record the result

Record the build commit, the selected inputs, the installed release, the
phase durations, the verified-fetch drill's exact answers and the completed
cleanup in the [acceptance ledger](../../docs/acceptance.md). A run is
successful only when every step passes from preparation through cleanup.
