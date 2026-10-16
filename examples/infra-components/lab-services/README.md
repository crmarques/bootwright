# Managed infrastructure services

Run a Squid proxy, dnsmasq DNS server, chrony NTP server and HTTP/HTTPS artifact
server on the controller. This example provisions no guest, operating system or
cluster and needs no installation media. The services remain selected even
without consumers; they are managed containers with context-owned systemd
units.

## Host prerequisites

Use a Linux/amd64 controller admitted by `setup`, with systemd, working host
package repositories and root or suitable sudo access. Follow the
[operator guide](../../../docs/operator-guide.md#prepare-a-host) for setup,
network routes and privilege requirements. `setup` installs Podman and the
Ansible runtime; the controller stage supplies graph-selected prerequisites.

The chosen bind address must already belong to the controller. Reserve TCP
3128, TCP/UDP 53, UDP 123, and TCP 8080/8443 there, and allow intended clients
through the host firewall. Existing DNS or time services must not occupy those
sockets. Provide reachable upstream DNS/NTP sources and access to the managed
container images. The managed proxy cannot supply the controller's initial
downloads: its Machine declares direct egress.

## Customize

From the repository root, make a private operator copy:

```sh
services_input="$HOME/bootwright-inputs/lab-services"
mkdir -p "$services_input"
cp -R examples/infra-components/lab-services/. "$services_input/"
```

Replace the synthetic `192.0.2.1` and `controller.lab.example.test` in the
controller Machine, service bind addresses and generated TLS certificate SANs.
Set DNS `forwarders` and NTP `upstreamSources` to reachable servers; their
documentation addresses do not answer. Endpoints refer to the Machine's
address names, so retain those names or update each `addressRef` too. Choose
upstreams independently of these services to avoid bootstrap loops. The
artifact server's `persistent` retention keeps published content until destroy.

## Run it

```sh
make build
./bin/bootwright version
./bin/bootwright validate -f "$services_input"
./bin/bootwright setup --dry-run
./bin/bootwright setup
./bin/bootwright context init --name lab-services --input-dir "$services_input"
./bin/bootwright secret generate --context lab-services
./bin/bootwright secret check --context lab-services
./bin/bootwright plan --context lab-services
./bin/bootwright apply --context lab-services --stage controller
./bin/bootwright preflight controller --context lab-services
./bin/bootwright apply --context lab-services
./bin/bootwright status --context lab-services
./bin/bootwright diff --context lab-services --observe
./bin/bootwright apply --context lab-services
```

`secret generate` creates the artifact server's serving certificate in the
context store. No secret payload belongs in the operator input. Review each
apply's plan before confirming. The final apply should settle the completed
operation; `status` reads records, while `diff --observe` checks live service
evidence. Independent services may start concurrently once their prerequisites
are ready. For a failed or interrupted apply, inspect `status` and the reported
operation logs, resolve the cause, and repeat that apply with the same stages.
To change imported declarations, destroy the existing realization while
keeping its context, update the input as described in the
[examples index](../../README.md#prepare-an-operator-copy), and apply again.

## Remove it

```sh
./bin/bootwright destroy --context lab-services
./bin/bootwright status --context lab-services
./bin/bootwright context delete --name lab-services --purge
```

Destroy removes the owned containers, units, configuration and artifact data.
It retains host packages and setup's shared runtime. There are no Machine disks
in this scenario, so its removal does not need `--authorize data-loss`. Keep
the operator copy for another run or remove it when no longer needed. Continue
an interrupted removal with `destroy` before deleting the context.
