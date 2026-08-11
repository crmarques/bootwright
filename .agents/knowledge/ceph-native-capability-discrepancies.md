# Ceph native capability discrepancy ledger

This ledger records observed differences between Ceph product, package, and
image coordinates. It is diagnostic evidence, not an executable support matrix.
Bootwright must not infer a native command from a product release, RPM EVR, or
image tag recorded here.

## Runtime rule

The provider table may identify a candidate native token for a provider feature.
The executor then inspects the installed native help surface immediately before
the feature is needed:

1. A nonzero command result, timeout, or help response missing a stable baseline
   is unknown and fails before the related mutation.
2. Recognizable help containing the exact candidate option or command signature
   enables that argument.
3. Recognizable help without the candidate proves only that the installed tool
   does not advertise the feature, so Bootwright omits that argument.

An observed difference that native help cannot express may justify a closed,
explicit workaround token under ADR 0008. It still must not become an inferred
release-number branch.

## Adding an observation

Append one dated entry containing all available fields:

- context and symptom, with the exact native error or divergent behavior;
- declared product release and exact host-package and daemon-image coordinates;
- the bounded read-only probe, exit code, recognizable baseline, and relevant
  help excerpt or exact absence;
- the behavioral consequence Bootwright must select;
- whether the evidence was reproduced live, inspected from an artifact, or
  taken from vendor documentation;
- authoritative sources and the regression test that preserves the conclusion.

Never promote an entry into validation or apply policy. Add or refine a live
capability probe; use this ledger to explain why that probe exists.

## 2026-08-11 — IBM bootstrap license option differs within stream 9

**Context and coordinates:** `ceph-prd-01` declared IBM Storage Ceph `9.9.0.3`,
`cephadm` package `20.1.0-221.el9cp`, Cephadm Ansible package
`5.0.2-1.el9cp`, and daemon image `cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.0-20201`.
The seed reported:

```text
cephadm-20.1.0-221.el9cp.noarch
ibm-storage-ceph-license-9.1-1.el9cp.noarch
cephadm version 20.1.0-221.el9cp (c8879ca38bd70c6aeb8a1326be2a7f19a9a60841) tentacle (stable)
```

The package acceptance marker
`/usr/share/ibm-storage-ceph-license/accept` existed. The installed
`cephadm bootstrap --help` did not advertise
`--automatically-accept-license`, and passing it failed before bootstrap with:

```text
cephadm: error: unrecognized arguments: --automatically-accept-license
```

The failing Bootwright bundle `v0.1.4-444-g968309b9` treated the provider's
`requiresLicense` value as proof that this CLI option existed. That value owns
the package acceptance workflow; it is not evidence about the installed
`cephadm` command surface.

**Documented and artifact contrast:** IBM's 9.9.0 bootstrap procedure does not
use that option. IBM's 9.9.1 procedure introduces it and says Call Home is
enabled by default starting in 9.9.1. The published `20.1.0-221.el9cp` source
does not register the bootstrap option or either Call Home acknowledgement
command. The published `20.2.1-324.el9cp` source registers
`--automatically-accept-license` and the literal command prefixes
`orch accept call-home-enabled` and `orch deny call-home-enabled`. The 9.9.0
Call Home procedure supports explicit `call_home_agent` module enablement, so
direct module enable/disable is not a 9.9.1-only capability; the separate
acknowledgement surface is discovered independently.

**Consequence:** A fresh licensed bootstrap appends the candidate option only
when recognizable live `cephadm bootstrap --help` advertises it. IBM Call Home
always reconciles the authored manager-module intent, while an accept/deny
acknowledgement runs only when recognizable live `ceph orch --help` advertises
the matching full signature. Probe failure or unrecognizable help refuses
before mutation. Product release numbers never select either path.

**Evidence:** live seed output supplied by the operator; exact package and image
coordinates from the authored production declaration; IBM product
documentation for the cross-release contrast; and direct inspection of IBM's
published `20.1.0-221.el9cp` and `20.2.1-324.el9cp` source RPM contents. In the
latter, `src/pybind/mgr/orchestrator/module.py` registers the two literal Call
Home command prefixes and `src/cephadm/cephadm.py` registers the bootstrap
option.

**Sources:**

- [IBM Storage Ceph 9.9.0 bootstrap](https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=installation-bootstrapping-new-storage-cluster)
- [IBM Storage Ceph 9.9.1 bootstrap](https://www.ibm.com/docs/en/storage-ceph/9.9.1?topic=installation-bootstrapping-new-storage-cluster)
- [IBM Storage Ceph 9.9.0 Call Home enablement](https://www.ibm.com/docs/en/storage-ceph/9.9.0?topic=interface-enabling-call-home)
- [IBM Ceph 20.1.0-221 source RPM](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/source/ceph-20.1.0-221.el9cp.src.rpm)
- [IBM Ceph 20.2.1-324 source RPM](https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/source/ceph-20.2.1-324.el9cp.src.rpm)

**Regression coverage:** `internal/storage/cephprovider/provider_test.go`,
`internal/render/ceph/storage_scripts_test.go`, and
`internal/repo/checks/ansible_storage_test.go`.
