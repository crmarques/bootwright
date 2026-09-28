# Managed RHEL on emulated bare metal

This example is the complete shape the
[B72 delivery](../../specs/milestones/m4.md#b72)
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

## Host prerequisites

- Fedora on linux/amd64. RHEL 9 prepares with `setup`, but its controller stage
  refuses the `libvirt` requirement this controller declares until an approved
  entitled source is defined.
- Hardware virtualization with `/dev/kvm`; the controller stage installs
  libvirt, QEMU, swtpm and the ISO tooling itself.
- Root, or an account that may run `sudo`: Bootwright asks for authorization
  when a command needs it.
- The guest's 4 vCPUs and 8 GiB of memory beyond what the host and the four
  service containers use, and disk for the two media images, the DVD package
  tree published from them and the guest's 60 GiB disk.
- One lab per host at a time: `lab-sno` binds the same sockets and the same
  guest bridge.
- The RHEL 9.8 boot and DVD images, and the addresses below adjusted to the
  host before the input is imported.

Every in-tree test is unitary: none creates a container, a network or a guest.
Running this journey under a build matching B72's acceptance baseline and
recording it in the
[acceptance ledger](../../docs/acceptance.md) is B72's operator gate. The first
real runs, which qualified the emulated BMC's Redfish surface and `mkksiso`
against RHEL 9.8 boot media, are described in
[development](../../docs/development.md) and
[installation knowledge](../../.agents/knowledge/installation-completion-proof.md).

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

The run restarts the host, so make the controller address and any chronyd
change persist across a reboot.

## Media

The two images this example installs from live in the host-wide media store,
which every context shares. The store lives beside the rest of the prepared
host state, so `setup` runs before the first `media add`. Each image is added
once, under the exact name the `MachineImage` and the install profile select.
An operation that uses a stored image freezes it: `media list` marks it
reserved, and `media delete` refuses until every context that froze it is
destroyed.

## Run it

Run the block one line at a time from the repository root. `setup`, `apply`,
`machine stop` and `destroy` present what they will do and ask for
confirmation, and any command may ask for sudo authorization. The automation
is embedded in the executable, so run `setup` again after any `make build` that
changes it; `apply` refuses the retained bundle otherwise.

```sh
make build
./bin/bootwright setup
./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
./bin/bootwright media list --checksums
./bin/bootwright context init --name lab-rhel
./bin/bootwright context update --name lab-rhel --input-dir "$PWD/examples/lab-rhel" --yes
./bin/bootwright secret generate
./bin/bootwright secret check
./bin/bootwright apply --stage controller
./bin/bootwright preflight controller --context lab-rhel
./bin/bootwright plan
./bin/bootwright apply --stage infra-components
curl -sk https://192.0.2.1:8443/ -o /dev/null -w '%{http_code}\n'
curl -x http://192.0.2.1:3128 -sI http://example.com/ | head -1
dig @192.0.2.1 controller.lab.example.test +short
dig @192.0.2.1 +tcp rhel-01.lab.example.test +short
chronyd -Q -t 3 'server 192.0.2.1 iburst port 123'
systemctl list-units 'bootwright-*'
./bin/bootwright apply
./bin/bootwright status
./bin/bootwright apply
./bin/bootwright destroy --authorize data-loss
./bin/bootwright machine stop --name rhel-01
./bin/bootwright destroy --authorize data-loss
./bin/bootwright apply
sudo systemctl reboot
./bin/bootwright machine start --name rhel-01
./bin/bootwright machine stop --name rhel-01
./bin/bootwright destroy --authorize data-loss
```

Every block depends on the controller block, so the first apply selects the
`controller` stage: it binds the context to this host and installs the client,
hypervisor and installer-tooling closures the graph selects, then pauses.
`preflight controller --context lab-rhel` then proves that context's own
tools, and `plan` previews the rest of the paused operation, marking each block
with its stage. A stage selection that admits no startable block refuses
before registering anything.

The `infra-components` apply pulls each pinned image, publishes the
configuration, starts the units and proves every service answers; the `curl`,
`dig`, `chronyd` and `systemctl` lines check the same independently. An empty
served root answers `404` and the proxy answers `400` to a request that is not
a proxy request; both are well-formed answers and both are what readiness
proves.

The unscoped `apply` realizes the provider host and `rhel-01` and installs RHEL
through its emulated BMC; `status` reports the result. A completed apply is
terminal: the second `apply` settles without an effect, and editing the input
first refuses with `lifecycle.state`, because there is
[no reconciliation path](../../specs/state-reconciliation.md#lifecycle-unit).

The removal deletes the guest's disks, so `destroy` consumes the `data-loss`
authorization; without `--authorize data-loss` it refuses before registering
anything. It also proves every Machine it would take back is down before it
registers, so the first `destroy` refuses `lifecycle.live` while `rhel-01` runs
and names the command that stops it. Nothing is removed and no operation is
created, so stopping the guest and repeating the command is the whole recovery.
The inverse removes exactly what the apply created, and destroy ends
`state: done` and `next: none`; repeating it settles.

B72's operator gate continues past that destroy. A fresh `apply` realizes the
environment again under a new operation. The host restart then proves the
provider host carries its guest network and storage pool across a reboot: once
the host is back, return to the repository root and `machine start` powers
`rhel-01` on through its emulated BMC. Only then do the final `machine stop`
and `destroy` take the environment back, which leaves it ready for the undo
below.

## Undo

A completed destroy releases the context's ownership evidence, so the context
and the media it froze can then be deleted:

```sh
./bin/bootwright context delete --name lab-rhel --purge
./bin/bootwright media delete --name rhel-9.8-x86_64-boot.iso
./bin/bootwright media delete --name rhel-9.8-x86_64-dvd.iso
```

What `setup` and the controller stage installed stays: nothing uninstalls host
packages or client closures. If you stopped chronyd for the run, start it
again.

## Interrupting an apply

Interrupting during an image pull leaves the operation `unknown`, which is the
honest outcome: the executable cannot prove whether the effect completed. The
records that say so are written as the command exits, so `status` reports
`unknown` rather than claiming the block is still running. Two roads follow,
and `status` offers both.

The next `apply` observes the exact frozen request, resolves that block from
live evidence and continues without repeating work it can prove is already
done. Changing the input, the executable or the embedded automation while an
operation is incomplete refuses that road instead, and names the recovery it
needs.

A `destroy` takes the environment back rather than finishing it. It resolves
every effect whose outcome the interruption lost, reporting that proof under
`Checks` before anything else, and then removes every block the apply started.
Nothing is registered until each one is proved, so a removal that cannot reach
the host leaves the context exactly as it found it:

```sh
./bin/bootwright status                     # unknown, with both roads offered
./bin/bootwright destroy --yes
./bin/bootwright apply --yes
```

A block that fails for a nameable reason behaves the same way, except that
nothing is left to resolve: the operation is `failed`, the next `apply` retries
that block, and a `destroy` removes what the apply started. That removal is the
road out of a repaired adapter. A continuation runs the automation its operation
froze, so rebuilding the collection refuses one, while a fresh removal runs
under the build in hand:

```sh
make build && ./bin/bootwright setup        # publish the repaired automation
./bin/bootwright machine stop --name rhel-01 --force   # if its guest is running
./bin/bootwright destroy                    # names any authorization it needs
./bin/bootwright apply --yes
```

A failed install usually leaves the guest powered on, which the removal refuses
until it is stopped; `--force` cuts the power, which an installer that never
finished has nothing to lose from.

The removal covers only the blocks that apply started, so it consumes
`data-loss` only when the guest whose disks it deletes is among them.
