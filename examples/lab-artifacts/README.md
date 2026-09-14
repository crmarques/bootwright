# Managed artifact server on the controller

This example is the smallest complete Environment the
[M1e lifecycle](../../specs/milestones.md#m1e--lifecycle-engine-and-managed-artifact-serving)
supports: one OS-ready Machine that is both the Bootwright controller and the
host of one managed `ArtifactServer`. Applying it exercises the whole engine,
including plans, leases, operation records, secret binding, host reservations,
readiness evidence, exact continuation and the inverse.

| Object | Role |
| --- | --- |
| [`controller`](infra/machines/controller.yaml) | The provided local Machine selected by `Environment.spec.controller`; it declares `container-runtime`. |
| [`lab-artifacts`](infra/components/artifact-server.yaml) | Managed artifact server with one HTTPS and one HTTP listener on the controller address. |
| [`artifact-server-tls`](secret-descriptors/artifact-server-tls.yaml) | Generated serving certificate; its subject alternative names must cover every address an HTTPS endpoint serves. |

Nothing else is declared, because every other selected object would need a
capability this milestone does not implement. [`examples/lab-ocp`](../lab-ocp)
is the larger fixture and is deliberately refused by `apply` until its
remaining milestones land.

## Lab conventions

Addresses use the documentation range `192.0.2.0/24` and the domain
`lab.example.test`, so nothing here identifies a real machine. Before importing
the directory, replace `192.0.2.1` with an address that exists on the host, in
both the Machine and the artifact server, and replace the domain and the
certificate's subject alternative names to match. The listener ports `8443` and
`8080` must be free: the apply refuses rather than taking a socket another
context reserved, and the operating system refuses a port another process
already holds.

## Run it

Bootwright requests sudo authorization for context, setup and lifecycle
commands. The artifact-server automation is embedded in the executable, so a
build that changes it also changes the dependency-bundle identity: run
`setup` again after `make build`, otherwise `apply` refuses with the
retained bundle it cannot use.

```sh
make build
./bin/bootwright validate -f examples/lab-artifacts
./bin/bootwright context init --name lab-artifacts --input-dir "$PWD/examples/lab-artifacts"
./bin/bootwright secret generate
./bin/bootwright secret check
./bin/bootwright setup
./bin/bootwright preflight controller --context lab-artifacts
./bin/bootwright plan
./bin/bootwright apply
```

The first apply presents its plan, asks for confirmation, pulls the pinned
server image, publishes the configuration, starts the unit and proves every
listener answers. It ends with `state: done` and `next: destroy`. Verify it
independently:

```sh
curl -sk https://192.0.2.1:8443/ -o /dev/null -w '%{http_code}\n'
systemctl status 'bootwright-*-artifacts-lab-artifacts.service'
./bin/bootwright status
```

An empty served root answers `404`; that is the expected result until content
publication exists. A completed apply is terminal: repeating it refuses with
`lifecycle.state`, because there is
[no reconciliation path](../../specs/state-reconciliation.md#lifecycle-unit).
The inverse removes exactly what the apply created:

```sh
./bin/bootwright apply --yes    # refuses: destroy it before applying again
./bin/bootwright destroy
./bin/bootwright context delete --name lab-artifacts --purge
```

Destroy ends `state: done` and `next: none`, and the purge succeeds because the
completed removal released the context's ownership evidence.

## Interrupting an apply

Interrupting during the image pull leaves the operation `unknown`, which is the
honest outcome: the executable cannot prove whether the effect completed. The
next `apply` observes the exact frozen request, resolves that block from live
evidence and continues without repeating work it can prove is already done.
Changing the input, the executable or the embedded automation while an
operation is incomplete refuses instead, and names the recovery it needs.
