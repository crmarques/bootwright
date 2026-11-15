# Cluster inspection reads evidence without its frozen request

Observed 2026-10-13 while delivering `cluster list` and `cluster info` (B519).

## Symptom

A read-only reader of `lifecycle.Service.Evidence` cannot re-run a capability's
own evidence check. `EvaluateEvidence` and the image decoder need the frozen
request and its digest, and the read returns `BlockEvidence` with `Block`
empty: only `ApplyProved` dependency reads fill it. A reader that passes an
empty request refuses every real proof.

## What the reader does instead

`ceph.Inspect` takes the structural facts the evidence itself carries: the
closed member set, `Action == "cluster"`, `Ready`, `Owned`, `Exists`, no
`Partial` or `Removed`, the FSID and the monitors; and for the image, the
`Postcondition`, the pinned release, base, platform and a valid digest. A
monitor is a native hostname, which `storage.NativeHostname` maps back to the
declared node, and its address is the Machine's declared contact address.
Anything missing or ambiguous reports `unavailable` with the apply remedy; a
read error propagates.

## Custody presence

`custody.Service.ProducedAvailable` reads the produced-entry index with no
unlock or credential read. OpenShift client handoffs and their inspection rows
require that proved entry, completed install evidence and the matching
controller client. This is metadata readiness, not proof that decryption or
remote access will succeed; the explicit export still validates its bytes.
The `cluster kubeconfig` row retains its install-completion readiness proxy.
See the [handoff contract](../../specs/cli.md#cluster-client-handoffs).
