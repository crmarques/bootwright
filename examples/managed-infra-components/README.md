# Managed infrastructure components on the controller

This example is the smallest complete Environment the
[M1f lifecycle](../../specs/milestones.md#m1f--managed-controller-network-services)
supports: one OS-ready Machine that is both the Bootwright controller and the
host of every managed infrastructure service. Applying it exercises the whole
`infra-components` stage, including plans, leases, operation records, secret
binding, host reservations, readiness evidence and the inverse.

| Object | Role |
| --- | --- |
| [`controller`](infra/machines/controller.yaml) | The provided local Machine selected by `Environment.spec.controller`; it declares `container-runtime`. |
| [`lab-proxy`](infra/components/proxy.yaml) | Managed Squid container; it answers only the addresses the graph declares. |
| [`lab-dns`](infra/components/dns.yaml) | Managed dnsmasq container answering every retained Machine name. |
| [`lab-ntp`](infra/components/ntp.yaml) | Managed chrony container serving time without disciplining the host clock. |
| [`lab-artifacts`](infra/components/artifact-server.yaml) | Managed artifact server with one HTTPS and one HTTP listener. |
| [`artifact-server-tls`](secret-descriptors/artifact-server-tls.yaml) | Generated serving certificate; its subject alternative names must cover every address an HTTPS endpoint serves. |

Every block belongs to the `infra-components` stage, so `apply --stage
infra-components` and a plain `apply` do the same work here. The larger
[`examples/lab-ocp`](../lab-ocp) adds a substrate, a guest and a cluster, and
is still refused until those capabilities land.

## Lab conventions

Addresses use the documentation range `192.0.2.0/24` and the domain
`lab.example.test`, so nothing here identifies a real machine. Before importing
the directory, replace `192.0.2.1` with an address that exists on the host, in
the Machine, all four services and the certificate. Replace `192.0.2.53` and
`192.0.2.123` with an upstream resolver and time source the host can reach, or
remove those lists to serve only what the graph declares.

Five sockets on that address must be free: `3128`, `53`, `123`, `8443` and
`8080`. The apply refuses rather than taking a socket another context reserved,
and the operating system refuses a port another process already holds. Two
collisions are worth checking by hand:

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
ss -lntup | grep -E ':(53|123|3128|8443|8080)\b'
```

If chronyd holds `0.0.0.0:123`, either give it explicit `bindaddress` lines for
the addresses it should serve and reload it, or stop it for the duration of the
test and start it again afterwards.

## Run it

Bootwright requests sudo authorization for context, setup and lifecycle
commands. The service automation is embedded in the executable, so a build that
changes it also changes the dependency-bundle identity: run `setup`
again after `make build`, otherwise `apply` refuses with the retained bundle it
cannot use.

```sh
make build
./bin/bootwright validate -f examples/managed-infra-components
./bin/bootwright context init --name managed-infra --input-dir "$PWD/examples/managed-infra-components"
./bin/bootwright secret generate
./bin/bootwright secret check
./bin/bootwright setup
./bin/bootwright preflight controller --context managed-infra
./bin/bootwright plan
./bin/bootwright plan --stage infra-components
./bin/bootwright apply --stage infra-components
```

The plan lists four blocks, each marked `[infra-components]`. Selecting a stage
this Environment does not use previews nothing to start, and applying it
refuses before registering anything:

```sh
./bin/bootwright plan --stage substrates
./bin/bootwright apply --stage substrates
```

The first apply presents its plan, asks for confirmation, pulls each pinned
image, publishes the configuration, starts the units and proves every service
answers. It ends with `state: done` and `next: destroy`. Verify it
independently:

```sh
curl -sk https://192.0.2.1:8443/ -o /dev/null -w '%{http_code}\n'
curl -x http://192.0.2.1:3128 -sI http://example.com/ | head -1
dig @192.0.2.1 controller.lab.example.test +short
dig @192.0.2.1 +tcp controller.lab.example.test +short
chronyd -Q -t 3 'server 192.0.2.1 iburst port 123'
systemctl list-units 'bootwright-*'
```

An empty served root answers `404` and the proxy answers `400` to a request
that is not a proxy request; both are well-formed answers and both are what
readiness proves. Repeating the apply reports every block unchanged, and the
inverse removes exactly what the apply created:

```sh
./bin/bootwright apply --yes
./bin/bootwright destroy
./bin/bootwright context delete --name managed-infra --purge
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
