# lab-baremetal

One physical server installed with RHEL through its own Redfish management
controller. It is the smallest complete shape
[B73](../../specs/milestones/m4.md#b73) supports: a controller
Machine running the managed services, a bare-metal `InfraProvider`, and one
Machine that declares the hardware it must be proved to be.

**Physical installation is implemented and proved in-tree.** The
installation of `Machine/metal-01` plans, publishes its installer image and its
host key privately and proves completion against that key
([physical installation](../../specs/managed-os.md#physical-installation));
`TestLabBaremetalExamplePlansItsInstallation` and
`TestLabBaremetalExampleKeepsPrivateURLsOutOfPublicArtifacts` hold it to that.
The emulated [rehearsal](#rehearsing-it-without-hardware), B73's operator gate,
and the run on real hardware are operator gates that have not run yet, so this
example claims no availability on any firmware.

What differs from [lab-rhel](../lab-rhel/README.md), which installs the same
operating system on a libvirt guest, follows from the machine existing before
Bootwright and outliving this context:

- **`substrates` is empty.** A bare-metal provider runs nothing Bootwright
  installs, so it plans no provider-host block. The `machines` stage carries
  the whole realization: one block claims and proves the server, and one
  installs it.
- **The apply is authorized.** `apply --authorize data-loss` is required,
  because the installation erases a disk that already held something. The
  removal needs no authorization: it releases the claim and retains the
  machine, its disks and whatever is installed on them.
- **The machine declares what it is.** Its NICs and their addresses, its boot
  NIC, its management controller and its root device are all declared, and the
  controller's own inventory is compared against them before any media is
  inserted. A machine that answers but reports a different address set refuses.
- **The host key is declared, not discovered.** `os.install.hostKeyRef` names
  the key pair the installation delivers, so completion is proved against a key
  that was known before the machine was ever contacted.
- **The controller leg is verified.** The installation hands the management
  controller the private URL of the installer image, so it refuses a
  controller reached over plain http or with `tls.verify: false`
  (`TestLabBaremetalExampleRefusesAnUnverifiedController`). The provider names
  the lab controllers' own authority in `tls.trustBundleRef: lab-bmc-ca`, a
  `caBundle` Secret the context stores.
- **The installer image is private.** Its Kickstart names the URL the
  installer fetches the host key from, so the image is published only beneath
  `private/os/metal-01/<token>/` beside that key pair, readable only by the
  serving process, and withdrawn with it before completion is proved. A
  public artifact names no private URL.

**A RHEL controller brings its own `lorax` and `xorriso`.** The artifact
server sits on the controller, so the installation builds its media there with
`lorax` and `xorriso`, and no public source Bootwright resolves from carries
them for RHEL. On a RHEL controller, install both from the host's own enabled
Red Hat repositories (`dnf install lorax xorriso`) before the apply
([D106](../../specs/milestones/backlog.md#decisions)). The controller stage
proves them by presence and the qualified Red Hat release key, and otherwise
refuses before it acquires anything, naming the package that is missing or
signed by another key; `preflight controller --context lab-baremetal` reports
the same check. A Fedora controller's stage installs them itself.

## Host prerequisites

The controller is the host this context runs on: it builds the installer
image, serves it and the package tree, and answers DNS and NTP for the
installer. Check each of these before the first apply.

**Disk.** About 16 GiB under `/var/lib/bootwright/media` for the RHEL 9.8 boot
ISO (about 1.4 GiB) and DVD (about 14.5 GiB); about 14.5 GiB under
`/var/lib/bootwright-services` for the hosted package tree, plus about 1.4 GiB
for each machine's installer image while it installs; and about 1.4 GiB per
machine under `/var/lib/bootwright-install` while that image is built.

**Egress.** `setup` and the controller stage fetch from the destinations the
[operator guide](../../docs/operator-guide.md#destinations-to-allow) lists.
The [context-free route](../../specs/controller.md#the-context-free-acquisition-route)
takes no proxy credential, so a proxy that demands one must let those
destinations through unauthenticated, or the host must reach them directly.
The lab's DNS and NTP forwarders, `192.0.2.53` and `192.0.2.123`, must be
reachable from the controller.

**Listener hand check.** The managed services bind `53`, `123`, `8443` and
`8080` on `192.0.2.1`, so nothing else may hold them; the resolver and chronyd
notes of [lab-rhel](../lab-rhel/README.md#lab-conventions) apply unchanged.
Read the owner of each socket:

```sh
ss -lntup | grep -E ':(53|123|8443|8080)\b'
```

After the `infra-components` stage, prove the artifact server from a host on
the BMC network, not from the controller: readiness on the controller proves
nothing about whether a remote fetcher reaches it. The empty served root
answers `404`:

```sh
curl -sk -o /dev/null -w '%{http_code}\n' https://192.0.2.1:8443/
```

**Firewall, ports by source.** Open each port only to the source that needs
it:

| Source | Port | Why |
| --- | --- | --- |
| BMC network | `8443/tcp` | the controller fetches the installer image |
| installer network (`198.51.100.0/24` here) | `8443/tcp` | the installer fetches its host key |
| installer network | `8080/tcp` | the installer fetches the package tree |
| installer network | `53/udp`, `53/tcp` | name resolution |
| installer network | `123/udp` | time |

The controller's own outbound legs, to the BMC on `443/tcp` and to the
installed machine on `22/tcp`, need no inbound rule. With firewalld, find the
zone the controller's interface is in, add one source-scoped rule per row, and
reload:

```sh
sudo firewall-cmd --get-active-zones
sudo firewall-cmd --permanent --zone=<zone> --add-rich-rule='rule family="ipv4" source address="<source>" port port="8443" protocol="tcp" accept'
sudo firewall-cmd --reload
```

The `FedoraWorkstation` zone already opens `tcp` and `udp` `1025-65535` to
every source, which exposes `8443` and `8080` to everyone; move the interface
to a narrower zone rather than relying on it.

**An earlier build.** A host an earlier Bootwright build manages follows the
[operator guide's rule](../../docs/operator-guide.md#a-host-an-earlier-bootwright-build-manages).

**FIPS key types.** A controller in FIPS mode neither accepts nor pins
Ed25519, so declare `bootwright-machine-key` and `metal-01-host-key` with
`keyType: rsa` or `keyType: ecdsa-p256`
([FIPS-mode controller](../../docs/operator-guide.md#a-fips-mode-controller)).
The installation installs the delivered key at its own type's path and pins
that type.

**Serving certificate.** A generated serving certificate is ECDSA P-256. If
the management controller's virtual-media fetch refuses it, carry an RSA-2048
certificate instead, as a workaround: switch `artifact-server-tls` to
`source: contextStore: {}`, then create and store one:

```sh
openssl req -x509 -newkey rsa:2048 -nodes -days 365 -subj /CN=controller.metal.example.test -addext 'subjectAltName=IP:192.0.2.1,DNS:controller.metal.example.test' -addext basicConstraints=critical,CA:FALSE -addext keyUsage=critical,digitalSignature,keyEncipherment -addext extendedKeyUsage=serverAuth -keyout artifact-server.key -out artifact-server.crt
./bin/bootwright secret set --context lab-baremetal --name artifact-server-tls --certificate-file artifact-server.crt --private-key-file artifact-server.key
```

The [Redfish knowledge page](../../.agents/knowledge/redfish-physical-bmc.md)
records how to check whether a controller accepts the P-256 certificate.

**When a fetch fails.** The artifact server's unit journal shows each
completed request without its path: remote address, time, TLS protocol and
cipher, method, status and bytes. A handshake the controller refused before
any request needs a packet capture.

```sh
sudo journalctl -u bootwright-lab-baremetal-artifacts-lab-artifacts
```

## Run it

Run the block one line at a time from the repository root; the
[operator guide](../../docs/operator-guide.md#prepare-a-host) covers the build
and privileges. `setup`, `apply` and `destroy` present what they will do and
ask for confirmation, and any command may ask for sudo authorization.

```sh
make build
./bin/bootwright setup
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
./bin/bootwright validate -f examples/lab-baremetal
./bin/bootwright context init --name lab-baremetal
./bin/bootwright context update --name lab-baremetal --input-dir "$PWD/examples/lab-baremetal" --yes
./bin/bootwright secret generate
./bin/bootwright secret set --context lab-baremetal --name lab-bmc-credentials --username <account> --password-stdin
./bin/bootwright secret set --context lab-baremetal --name lab-bmc-ca --certificate-file /path/to/lab-bmc-ca.pem
./bin/bootwright secret check
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context lab-baremetal
./bin/bootwright plan
./bin/bootwright apply --authorize data-loss
./bin/bootwright status
```

`validate` admits all 15 files, and the import copies them. `secret generate`
creates the serving certificate, the fleet key and `metal-01-host-key`.
`secret check` fails until `secret set` stores `lab-bmc-credentials`, the
management controller's account, and `lab-bmc-ca`, the authority that issued
its certificate. At a terminal, the first `secret set`, with `<account>`
replaced by that account's name, prompts for the password on standard error
with echo off and reads one line. From a script, refresh sudo first, because an
invocation whose standard input is a pipe elevates without asking for a
password, then pipe one line into the same command:

```sh
sudo -v
printf '%s\n' "$BMC_PASSWORD" | ./bin/bootwright secret set --context lab-baremetal --name lab-bmc-credentials --username <account> --password-stdin
```

A sudo policy that logs input (`log_input`, `log_stdin`) records the password
whichever way it is entered.

`plan` lists `os-install-metal-01`, which consumes `data-loss`, so the apply
names that authorization; the removal consumes none and retains the machine.

## Rehearsing it without hardware

The physical path can be driven on one workstation, because the emulated
controller `lab-rhel` uses implements the same Redfish surface this one
drives, including the `EthernetInterfaces` collection the target proof reads.
The plan: apply a libvirt context whose Machine is installer-provisioned,
`os.provided: false` with no `installProfileRef`, so the substrate realizes
the domain and its controller and installs nothing; then point a copy of this
example at that controller, its `bmc.address` the emulated endpoint and its
declared NIC addresses the ones the domain was realized with.

That rehearsal does not run yet. A private delivery refuses an unverified
controller leg, and the emulated controller serves plain HTTP
(`internal/substrate/naming.go`), so the copy needs an https emulated
controller whose authority it names in `tls.trustBundleRef`, which is not
provided yet ([B446](../../specs/milestones/m4.md#b446)). Keep any such copy out
of the repository: its addresses belong to one workstation, and
`examples/wip/` is ignored for exactly this.

## What it does not cover

Removal retains the installed system: physical erase is deliberately not part
of this contract. A bonded or VLAN installation interface is not yet derived.
`import-certificate`, the default virtual-media trust, is implemented in the
Redfish client but not yet qualified against real firmware. An attempt that
fails leaves its private subtree published until the next apply clears it or
a destroy removes it. FIPS mode, disk encryption and a Satellite package
source are outside this example.
