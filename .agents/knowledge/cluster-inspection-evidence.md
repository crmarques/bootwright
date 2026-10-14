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

Custody has no presence read that avoids decrypting the entry: `Holds` runs in
a lent mutation area and `ReadProduced` returns the material. Inspection must
read no credential bytes, so a ContainerCluster's kubeconfig readiness is
derived from the proved completion of its install block, the event that fills
custody, and not from custody itself. A copy a removal kept unproved is
therefore not reported ready until a presence read exists.
