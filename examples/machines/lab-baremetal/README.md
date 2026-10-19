# Managed RHEL on physical bare metal

Bootwright claims one existing physical Machine, proves its declared hardware
through Redfish, and installs RHEL 9.8 through virtual media. The local
controller hosts DNS, NTP, an artifact server and installer-media tooling.
There is no provider-host block in `substrates`: the `machines` stage claims
and installs `metal-01`.

Installation erases the selected disk and requires `apply --authorize
data-loss`. Destroy withdraws installer material, removes managed services and
releases the claim; it retains the physical server, its installed OS and data,
so this example's destroy needs no data-loss authorization or power-off.
A later fresh apply installs again and erases the selected disk again.

The physical path is implemented and tested in-tree; that does not qualify
every real BMC firmware. Check the current
[acceptance ledger](../../../docs/acceptance.md) before a hardware run.

## Host prerequisites

- A Linux/amd64 Fedora, RHEL or derivative controller satisfying
  [host admission](../../../specs/controller.md#supported-hosts), with root or
  sudo, working host repositories and
  [download access](../../../docs/operator-guide.md#destinations-to-allow).
  Setup prepares the baseline; the controller stage acquires installer-media
  tooling, including `lorax` and `xorriso`, from enabled host repositories.
- Operator-supplied RHEL 9.8 x86_64 boot and DVD ISOs. Budget roughly 16 GiB
  for stored media, another DVD-sized extracted tree, and boot-ISO-sized
  temporary build and publication copies while installation runs.
- An HTTPS Redfish system endpoint, a permitted BMC account and the CA bundle
  that verifies its certificate. The controller must reach the BMC and the
  installed server's SSH address. The BMC must reach artifact HTTPS; the
  installer must reach HTTPS, package-tree HTTP, DNS and NTP.
- A server whose selected disk may be erased, with accurate NIC MAC addresses,
  interface name and root-device selection. Verify them independently first.

## Customize the input

Run from the repository root with a current `./bin/bootwright` executable
(`make build` builds it). Copy the example outside Git and edit that copy:

```sh
lab_baremetal_input=$(mktemp -d /tmp/bootwright-lab-baremetal.XXXXXX)
cp -R examples/machines/lab-baremetal/. "$lab_baremetal_input/"
```

All shipped values are documentation placeholders. Keep real addresses,
hardware identities and credentials in operator-owned files outside Git.

| Input | Required changes |
| --- | --- |
| [Controller](infra/machines/controller.yaml) and [services](infra/components) | Replace `192.0.2.1` with an existing controller IP and update the FQDN and service bind addresses. |
| [Serving certificate](secret-descriptors/artifact-server-tls.yaml) | Match all HTTPS endpoint IP/DNS names to the customized addresses. |
| [Physical Machine](infra/machines/metal-01.yaml) | Set the full Redfish system URL, actual NIC name/MAC, install IP and root-device hint; `/dev/sda` is only an example. |
| [NetworkConfig](infra/networkconfigs/lab-metal.yaml) | Set the actual installer subnet, gateway, interface and route; this provider does not create a physical network. |
| [Environment](environment.yaml) and DNS/NTP | Replace the base domain and Machine FQDNs consistently; replace or remove upstream `192.0.2.53` and `192.0.2.123`. |
| [BMC provider](infra/providers/lab-metal.yaml) | Keep certificate verification; `lab-bmc-ca` must contain the BMC's issuing CA chain. |

The full declared NIC address set is compared with Redfish inventory before
media insertion. `metal-01-host-key` is generated before installation and
delivered privately, so completion proves the installed key against a known
value. `bootwright-machine-key` separately provides fleet login credentials.
On a FIPS-mode controller set both SSH descriptors to `rsa` or `ecdsa-p256`;
see [FIPS guidance](../../../docs/operator-guide.md#a-fips-mode-controller).

Check service socket owners:

```sh
ss -lntup | grep -E ':(53|123|8443|8080)\b'
```

Managed DNS needs `53/tcp+udp`, NTP `123/udp`, artifact HTTPS `8443/tcp` and
package HTTP `8080/tcp` on the controller address. Wildcard resolver or chronyd
listeners can conflict; follow [lab-rhel's listener guidance](../lab-rhel/README.md#lab-conventions).
Configure the firewall by source:

| Source | Required controller ports |
| --- | --- |
| BMC network | `8443/tcp` for virtual-media fetch |
| Installer network | `8443/tcp` for private host-key delivery; `8080/tcp` for packages; `53/tcp+udp` and `123/udp` |

The controller also needs outbound `443/tcp` to the BMC and `22/tcp` to the
installed Machine. Use the
[network exposure guidance](../../../docs/operator-guide.md#network-exposure);
controller-local readiness does not prove either remote fetcher can reach it.

## Run it

Keep the input variable in this shell. Replace ISO/CA paths and the BMC account
name. The default serving certificate is P-256. If firmware requires RSA, change
`artifact-server-tls` to `source: {contextStore: {}}` before initialization and
use the optional recipe below. Run commands separately; mutations may request
confirmation and sudo:

```sh
./bin/bootwright version
./bin/bootwright validate -f "$lab_baremetal_input"
./bin/bootwright setup
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
./bin/bootwright media list --checksums
./bin/bootwright context init --name lab-baremetal --input-dir "$lab_baremetal_input"
./bin/bootwright secret generate
./bin/bootwright secret set --context lab-baremetal --name lab-bmc-credentials --username <account> --password-stdin
./bin/bootwright secret set --context lab-baremetal --name lab-bmc-ca --certificate-file /path/to/lab-bmc-ca.pem
```

Initialization freezes the customized input and selects this context; for a
later session use `context use --name lab-baremetal`.
Generation creates the fleet key, installed host key and, by default, serving
certificate. Supply both context-store BMC Secrets before `secret check`.
At a terminal `--password-stdin` prompts with echo disabled. For a pipeline,
refresh sudo first so elevation does not need the piped standard input.
Bootwright refuses input-recording sudo policies before reading the password;
see the [sudo rule](../../../docs/operator-guide.md#the-sudo-rule).

For the optional RSA certificate, run this after context initialization and
before `secret check` or any apply, substituting the customized IP and hostname:

```sh
lab_baremetal_tls=$(mktemp -d /tmp/bootwright-baremetal-tls.XXXXXX)
openssl req -x509 -newkey rsa:2048 -nodes -days 365 -subj /CN=controller.metal.example.test -addext 'subjectAltName=IP:192.0.2.1,DNS:controller.metal.example.test' -addext basicConstraints=critical,CA:FALSE -addext keyUsage=critical,digitalSignature,keyEncipherment -addext extendedKeyUsage=serverAuth -keyout "$lab_baremetal_tls/server.key" -out "$lab_baremetal_tls/server.crt"
./bin/bootwright secret set --context lab-baremetal --name artifact-server-tls --certificate-file "$lab_baremetal_tls/server.crt" --private-key-file "$lab_baremetal_tls/server.key"
rm -r "$lab_baremetal_tls"
```

The private directory protects the generated key, and `CA:FALSE` makes this a
serving leaf accepted by Secret validation. See the
[Redfish compatibility notes](../../../.agents/knowledge/redfish-physical-bmc.md).
Do not disable the verified controller-to-BMC leg to bypass a fetch problem.

Check all Secrets and prepare the services:

```sh
./bin/bootwright secret check
./bin/bootwright apply --stage controller --authorize data-loss
./bin/bootwright preflight controller --context lab-baremetal
./bin/bootwright plan
./bin/bootwright apply --stage infra-components --authorize data-loss
```

From a host on the BMC network, check the customized artifact endpoint:

```sh
curl -sk -o /dev/null -w '%{http_code}\n' https://192.0.2.1:8443/
```

An empty root returns `404`. This `-k` probe checks reachability only; the BMC's
own virtual-media fetch must also accept the serving certificate.

Authorization covers the complete frozen plan, including during staged
applies. Review its target and disk-erasing authorization, then finish:

```sh
./bin/bootwright apply --authorize data-loss
./bin/bootwright status
./bin/bootwright machine list --power-status
./bin/bootwright machine exec --name metal-01 cat /etc/os-release
./bin/bootwright machine rsh --name metal-01
./bin/bootwright diff --observe
./bin/bootwright apply
```

Leave `rsh` with `exit`. The BMC claim pins the proven system identity. The
installer image and host key are published in a private tokenized subtree,
withdrawn before completion is proved. Successful installation proves SSH
readiness and the delivered host key. The final unchanged apply settles
without repairing drift; changed input requires destroy first, then
`context update --name lab-baremetal --input-dir "$lab_baremetal_input"`
to import the edited copy before applying again.

## Recovery and cleanup

Read `status` after a failure or interruption. Repeat `apply --authorize
data-loss` against unchanged frozen input to observe unknown effects and retry
failed work; a still-unknown observation refuses further effects. Correct the
reported trust, inventory or network problem before retrying. Keep bound
Secrets and media available. A failed installation can leave private material
published until the next apply clears it or destroy removes it.
An automation-only rebuild continues: run `make build` and the same apply;
follow [changing the build between runs](../../../docs/operator-guide.md#changing-the-build-between-runs) rather than destroying.

The artifact unit journal records completed request metadata without paths;
a TLS failure before an HTTP request may require separate network diagnosis:

```sh
sudo journalctl -u bootwright-lab-baremetal-artifacts-lab-artifacts
```

To release the claim and remove managed services/publications:

```sh
./bin/bootwright destroy
./bin/bootwright status
systemctl list-units --all 'bootwright-*'
./bin/bootwright context delete --name lab-baremetal --purge
```

Continue an interrupted destroy with destroy. Removal leaves the server and
installed disks intact and does not stop the installed OS. Remove this
example's services only when the retained server no longer relies on them.
Verify removal before context deletion, which discards local state and keys.
Keep media for other contexts or, once none reserves it, delete it:

```sh
./bin/bootwright media delete --name rhel-9.8-x86_64-boot.iso
./bin/bootwright media delete --name rhel-9.8-x86_64-dvd.iso
```

Host packages/runtime remain. Restore host daemon/firewall changes and remove
the temporary input copy when finished.

## Rehearsing it without hardware

The emulator implements the Redfish inventory and virtual-media surface used
by this path, but a stock lab-rhel context is not a runnable physical rehearsal.
Its generated BMC serving certificate is a non-CA leaf; this provider's
`caBundle` Secret requires a CA certificate with certificate-signing usage.
Verified emulator HTTPS alone does not satisfy that physical trust contract.
A rehearsal needs a CA-issued emulator certificate and matching CA bundle,
plus an installer-provisioned substrate guest with no OS install profile and
accurately declared hardware. The shipped examples do not provide that chain.
The limitation is tracked under
[B496](../../../specs/milestones/m1.md#b496).

## What it does not cover

Physical erase on destroy is outside this contract. This example installs
over one ethernet interface; a bonded or VLAN install interface is projected as
[managed OS](../../../specs/managed-os.md#installation) states. Default
virtual-media trust uses `import-certificate`; its firmware compatibility still
requires hardware qualification. This example also omits encrypted disks and
Satellite package sources.
