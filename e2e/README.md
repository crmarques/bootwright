# End-to-end cases

Each directory is one self-contained case that runs on a single libvirt
laptop-class host, from host preparation to cleanup:

```text
e2e/<case>/
  values.yaml    the case's default input
  templates/     the Bootwright input as Helm templates, one object per file
  runbook.md     prepare, run, verify, acceptance drills, destroy
```

[render.sh](render.sh) renders one case into an input directory. Each case
exists because a milestone item awaits its run: the runbook names the items it
accepts and the evidence each needs. Record each accepted run as a row of the
[acceptance ledger](../docs/acceptance.md).

| Case | Guests | CPUs | Memory (GiB) | Disk (GiB) | Accepts |
| --- | --- | --- | --- | --- | --- |
| [lab-rhel](lab-rhel/runbook.md) | 1 | 3 | 6 | 50 | [B75](../specs/milestones/m1.md#b75), [B76](../specs/milestones/m1.md#b76), [B77](../specs/milestones/m1.md#b77), [B876](../specs/milestones/m1.md#b876) |
| [lab-baremetal-rehearsal](lab-baremetal-rehearsal/runbook.md) | 1, two contexts | 5 | 10 | 70 | [B73](../specs/milestones/m1.md#b73), [B496](../specs/milestones/m1.md#b496) |
| [lab-network-variants](lab-network-variants/runbook.md) | 3 | 7 | 14 | 130 | [B74](../specs/milestones/m1.md#b74), [B326](../specs/milestones/m1.md#b326) |
| [lab-3-nodes-ceph](lab-3-nodes-ceph/runbook.md) | 3 | 13 | 26 | 220 | [B626](../specs/milestones/m2.md#b626), one run per release |
| [lab-5-nodes-ceph-stretch](lab-5-nodes-ceph-stretch/runbook.md) | 5, two contexts | 12 | 20 | 200 | [B486](../specs/milestones/m2.md#b486), [B487](../specs/milestones/m2.md#b487), [B893](../specs/milestones/m2.md#b893), [B894](../specs/milestones/m2.md#b894); the emulated half of [B488](../specs/milestones/m2.md#b488) |
| [lab-sno](lab-sno/runbook.md) | 1 | 9 | 18 | 130 | [B61](../specs/milestones/m3.md#b61), [B755](../specs/milestones/m4.md#b755), [B757](../specs/milestones/m4.md#b757) |
| [lab-3-nodes-ocp](lab-3-nodes-ocp/runbook.md) | 3, larger host | 22 | 56 | 400 | [B66](../specs/milestones/m3.md#b66) |
| [lab-3-nodes-ocp-features](lab-3-nodes-ocp-features/runbook.md) | 3, larger host | 28 | 56 | 400 | [B720](../specs/milestones/m3.md#b720), [B721](../specs/milestones/m3.md#b721), [B724](../specs/milestones/m3.md#b724); IPv6: [B725](../specs/milestones/m3.md#b725), [B703](../specs/milestones/m1.md#b703) |
| [lab-3-nodes-ceph-host-variants](lab-3-nodes-ceph-host-variants/runbook.md) | 3, larger host | 13 | 40 | 430 | [B484](../specs/milestones/m2.md#b484)'s P5 row |

CPUs, memory and disk are what the apply gate requires on the host: the
declared guests plus the provider's reserve
([capacity admission](../specs/substrates.md#capacity-admission)). Memory is
`MemAvailable`, and swap never counts; disk is free space on the filesystem
under `/var/lib/libvirt`. A 16-CPU, 32 GiB laptop leaves at most about 28 GiB
available, so the last three cases need a larger host. Run one case at a time:
they share the bridge `virbr-e2e`, the guest subnet `198.51.100.0/24` and the
service ports.

## Prepare the host

Once per host, from the repository root:

1. A Fedora or RHEL amd64 host with `/dev/kvm`, sudo, `helm` and network
   egress ([controller requirements](../specs/controller.md#supported-hosts)).
   Bootwright installs everything else it needs.
2. `make build`, then `./bin/bootwright setup --yes`.
3. Free host ports: 53/tcp+udp, 123/udp, 3128, 8080 and 8443 on the host
   address, and 8000 on loopback (a two-context case lists its extra ports).
   A host `chronyd` or `dnsmasq` listening on all addresses conflicts:

   ```sh
   ss -lntup | grep -E ':(53|123|3128|808[0-9]|844[0-9]|800[0-9])\b'
   ```

4. Pin the host address. `host.address: auto` takes the default-route source
   address at render time, and every managed service binds it; a DHCP lease
   that moves breaks a running case, so prefer a reservation.
5. Media, once per host, for the RHEL cases:

   ```sh
   ./bin/bootwright media add --name rhel-9.8-x86_64-boot.iso --from-file /path/to/rhel-9.8-x86_64-boot.iso
   ./bin/bootwright media add --name rhel-9.8-x86_64-dvd.iso --from-file /path/to/rhel-9.8-x86_64-dvd.iso
   ./bin/bootwright media list --checksums
   ```

6. Capacity. Close other workloads until `grep MemAvailable /proc/meminfo`
   and `df -h /var/lib/libvirt` cover the case's row above. A Ceph case
   reports the same checks with `preflight storage-cluster` and an OpenShift
   case with `preflight container-cluster`; a RHEL-only case learns them
   from `apply`, which refuses before any effect.
7. Guest access to the host services needs no firewall rule: Bootwright puts
   each managed bridge in firewalld's `trusted` zone
   ([provider host](../specs/substrates.md#provider-host-realization)). A case
   that uses a bridge you create gives its rule in Prepare
   ([network exposure](../docs/operator-guide.md#network-exposure)).

## Render a case

```sh
e2e/render.sh <case> [--values FILE]... [--set KEY=VALUE]... [--out-dir DIR]
```

The case's `values.yaml` describes every input; `--set` and `--values`
override it in order. The output defaults to `~/bootwright-e2e/<case>`. A
render replaces only an empty directory or an earlier render, and never writes
inside the repository. A two-context case renders one input directory per
context below the output. Check each with
`./bin/bootwright validate -f <dir>`.

Context import freezes the rendered input. To change it, destroy, render
again, then run `context update --name <context> --input-dir <dir>`.

## Conventions

Runbooks run from the repository root with `./bin/bootwright` and set `C` to
the context name, `IN` to the rendered directory and `A` to the host address.
Every mutation shows its plan and asks for confirmation unless it is given
`--yes`.

Interrupting an `apply` with Ctrl-C, or `kill -9` of the `bootwright`
process, is part of several acceptances. Inspect with `status`, then repeat the
same `apply` with the same input, Secrets, media and stage selection. A failed
block is retried, and an unknown outcome is observed before any retry. A
rebuilt binary continues the same operation
([changing the build](../docs/operator-guide.md#changing-the-build-between-runs)).
An interrupted destroy continues with `destroy`, never `apply`.

Destroy lists the guests to stop first, and under `Stop after online cleanup`
those it still needs running. When it refuses or pauses, stop the guests it
names and repeat it. With `--yes` or without a terminal, `--authorize` takes
the plan digest it prints (`data-loss=<digest>`). Verify the removal before
`context delete --purge`, which deletes only local state and Secrets. Media
stays reserved until no context uses it.
