# Managed RHEL on emulated bare metal

This example is the complete shape the
[M1h delivery](../../specs/milestones.md#m1h--managed-rhel-on-emulated-bare-metal)
targets: one OS-ready Machine that is the Bootwright controller, the host of
every managed infrastructure service, and the libvirt provider host for one
guest whose RHEL 9.8 Bootwright installs through an emulated Redfish BMC. It
exercises the `controller`, `infra-components`, `substrates` and `machines`
stages, including plans, leases, operation records, secret binding, host
reservations, readiness evidence and the inverse.

| Object | Role |
| --- | --- |
| [`controller`](infra/machines/controller.yaml) | The provided local Machine selected by `Environment.spec.controller`; it declares `container-runtime` and `libvirt`. |
| [`lab-proxy`](infra/components/proxy.yaml) | Managed Squid container; it answers only the addresses the graph declares. |
| [`lab-dns`](infra/components/dns.yaml) | Managed dnsmasq container answering every retained Machine name. |
| [`lab-ntp`](infra/components/ntp.yaml) | Managed chrony container serving time without disciplining the host clock. |
| [`lab-artifacts`](infra/components/artifact-server.yaml) | Managed artifact server with one HTTPS and one HTTP listener; the installer ISO and the DVD package tree are published beneath its served root. |
| [`lab-libvirt`](infra/providers/lab-libvirt.yaml) | The libvirt provider hosted on the controller: the guest profile, the managed guest network and the emulated BMC range starting at `8000`. |
| [`lab-guests`](infra/networkconfigs/lab-guests.yaml) | The guest network: `198.51.100.0/24` with its static interface, search domain and default route. |
| [`rhel-01`](infra/machines/rhel-01.yaml) | The Bootwright-installed guest; it authors no access, because the fleet account is derived. |
| [`rhel-9-8-boot`](infra/os/rhel-9-8-boot.yaml) | The Anaconda boot media, named in the host-wide media store. |
| [`rhel-9-8`](infra/os/rhel-9-8.yaml) | The install profile: hosted-tree packages from the DVD media, no subscription, no initial password and no disk encryption, so the served ISO carries no secret. |
| [`artifact-server-tls`](secret-descriptors/artifact-server-tls.yaml) | Generated serving certificate; its subject alternative names must cover every address an HTTPS endpoint serves. |
| [`lab-bmc-credentials`](secret-descriptors/lab-bmc-credentials.yaml) | Generated credentials the emulated BMCs answer with. |
| [`bootwright-machine-key`](secret-descriptors/bootwright-machine-key.yaml) | The fleet key installed for the `bootwright` account on every Machine this graph installs. |

## Before the first real run

This build realizes every object the example declares, and its plan orders the
controller stage, the four services, the provider host, the machine and its
installation in that dependency order. Every in-tree test is unitary, though:
none of them creates a container, a network or a guest. Two things are still
qualified by hand before a first real run, and both are recorded in
[emulated-BMC knowledge](../../.agents/knowledge/sushy-tools-emulated-bmc.md):
the Redfish surface of the pinned sushy-tools image, and `mkksiso` against the
RHEL 9.8 boot media. Expect to correct something the first time through.

## Lab conventions

Two documentation ranges keep the two roles apart, and the domain is
`lab.example.test`, so nothing here identifies a real machine.

`192.0.2.1` is the controller's own existing address. Every managed service and
every emulated BMC binds it, so it must already exist on the host before the
first apply. Replace it with an address that exists there, in the Machine, all
four services, the provider's `bmcEmulationDefaults` and the certificate.
Replace `192.0.2.53` and `192.0.2.123` with an upstream resolver and time
source the host can reach, or remove those lists to serve only what the graph
declares.

`198.51.100.0/24` is the guest network Bootwright itself defines, on the bridge
`virbr-lab`, with the host holding `198.51.100.1` and forwarding through NAT.
It must not exist yet: the `substrates` stage creates it and the destroy
removes it. The guest reaches the managed services through that host address,
so the services need no interface on the guest network.

Six sockets on `192.0.2.1` must be free: `3128`, `53`, `123`, `8443`, `8080`
and `8000`. The apply refuses rather than taking a socket another context
reserved, and the operating system refuses a port another process already
holds. Two collisions are worth checking by hand:

- the host resolver must not own port `53` on that address. systemd-resolved
  binds `127.0.0.53` and is fine; a libvirt network on the same bridge is not,
  unless it declares `<dns enable='no'/>` and no DHCP range.
- the host time daemon must not hold port `123`. A stock chronyd binds the
  wildcard address, which covers every address on the host, so check it rather
  than assuming the managed container can bind beside it. The `-x` the managed
  container runs with keeps it from touching the host clock, but it does not
  free the socket.

Check both before importing, and read the output as the port owner, not the
interface:

```sh
ss -lntup | grep -E ':(53|123|3128|8000|8443|8080)\b'
virsh --connect qemu:///system net-list --all
```

If chronyd holds `0.0.0.0:123`, either give it explicit `bindaddress` lines for
the addresses it should serve and reload it, or stop it for the duration of the
test and start it again afterwards.

## Media

The two images this example installs from live in the host-wide media store,
which every context shares. The store lives beside the rest of the prepared
host state, so run `bootwright setup` before the first `media add`. Add each
image once, under the exact name the `MachineImage` and the install profile
select:

```sh
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
./bin/bootwright media list --checksums
```

An operation that uses a stored image freezes it: `media list` marks it
reserved, and `media delete` refuses until every context that froze it is
destroyed.

## Run it

Bootwright requests sudo authorization for context, setup and lifecycle
commands. The automation is embedded in the executable, so a build that changes
it also changes the dependency-bundle identity: run `setup` again after `make
build`, otherwise `apply` refuses with the retained bundle it cannot use.

```sh
make build
./bin/bootwright validate -f examples/lab-rhel
./bin/bootwright context init --name lab-rhel --input-dir "$PWD/examples/lab-rhel"
./bin/bootwright secret generate
./bin/bootwright secret check
./bin/bootwright setup
./bin/bootwright preflight controller --context lab-rhel
./bin/bootwright plan
./bin/bootwright plan --stage infra-components
./bin/bootwright apply --stage infra-components
```

The plan marks each block with its stage. A stage selection that admits no
startable block previews nothing to start, and applying it refuses before
registering anything.

The first apply presents its plan, asks for confirmation, installs the client
closure its graph selects, pulls each pinned image, publishes the
configuration, starts the units and proves every service answers. Verify it
independently:

```sh
curl -sk https://192.0.2.1:8443/ -o /dev/null -w '%{http_code}\n'
curl -x http://192.0.2.1:3128 -sI http://example.com/ | head -1
dig @192.0.2.1 controller.lab.example.test +short
dig @192.0.2.1 +tcp rhel-01.lab.example.test +short
chronyd -Q -t 3 'server 192.0.2.1 iburst port 123'
systemctl list-units 'bootwright-*'
```

An empty served root answers `404` and the proxy answers `400` to a request
that is not a proxy request; both are well-formed answers and both are what
readiness proves. A completed apply is terminal: repeating it over the same
input settles without an effect, and editing the input first refuses with
`lifecycle.state`, because there is
[no reconciliation path](../../specs/state-reconciliation.md#lifecycle-unit).
The inverse removes exactly what the apply created, and the destroy that
deletes the guest's disks consumes the `data-loss` authorization:

A removal proves everything it would take back is out of use before it
registers, so a destroy while the guest is still running refuses
`lifecycle.live` and names the command that stops it. Nothing is removed and no
operation is created, so stopping the guest and repeating the command is the
whole recovery:

```sh
./bin/bootwright apply --yes    # settles: nothing to do
./bin/bootwright destroy        # refuses while rhel-01 is running
./bin/bootwright machine stop --name rhel-01
./bin/bootwright destroy
./bin/bootwright destroy        # settles: nothing to remove
./bin/bootwright context delete --name lab-rhel --purge
```

Destroy ends `state: done` and `next: none`, and the purge succeeds because the
completed removal released the context's ownership evidence.

## Interrupting an apply

Interrupting during an image pull leaves the operation `unknown`, which is the
honest outcome: the executable cannot prove whether the effect completed. The
next `apply` observes the exact frozen request, resolves that block from live
evidence and continues without repeating work it can prove is already done.
Changing the input, the executable or the embedded automation while an
operation is incomplete refuses instead, and names the recovery it needs.

A block that fails for a nameable reason is different: the operation is
`failed`, the next `apply` retries that block, and a `destroy` removes what the
apply started instead. That removal is the road out of a repaired adapter. A
continuation runs the automation its operation froze, so rebuilding the
collection refuses one, while a fresh removal runs under the build in hand:

```sh
make build && sudo ./bin/bootwright setup   # publish the repaired automation
./bin/bootwright machine stop --name rhel-01 --force   # if its guest is running
./bin/bootwright destroy                    # names any authorization it needs
./bin/bootwright apply --yes
```

A failed install usually leaves the guest powered on, which the removal refuses
until it is stopped; `--force` cuts the power, which an installer that never
finished has nothing to lose from.

The removal covers only the blocks that apply started, so it consumes
`data-loss` only when the guest whose disks it deletes is among them.
