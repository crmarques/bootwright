# Native Ceph pull and isolated diagnostic boundaries

## Native certificate directory discovery

The native container client discovers certificate material by scanning the
whole directory passed as `--cert-dir`. The upstream
[containers/image TLS loader](https://raw.githubusercontent.com/containers/image/main/pkg/tlsclientconfig/tlsclientconfig.go)
requires every `.key` to have a matching `.cert` and every `.cert` a matching
`.key`. A broker setup key named `broker.key` in that directory fails before
registry traffic because `broker.cert` is absent. Explicitly loading just the
client pair in a Python TLS test does not exercise this discovery rule.

The [pull broker](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_pull_broker.py)
keeps its setup material outside the native certificate directory. The
[host runtime](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_host_runtime.py)
passes the directory containing only the CA and matched ephemeral client pair.
Upstream entitlement and bearer material remain broker-owned. Regression tests
must inspect the actual directory selected in the native pull arguments, use
real certificates, and cover cleanup on success and refusal.

## Validation in the isolated supervisor parent

A bare `native-unknown` may hide result collection or validation failure; it
alone does not identify a package, registry or image problem. The native
worker loads its source bundle only in its forked child. An import of
`native_diagnostics` during parent-side result validation therefore fails in a
clean remote interpreter, even if an in-process unit test has that collection
already imported.

The [native worker launcher](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native_worker.py)
composes the fixed diagnostics source into the supervisor namespace before it
forks. Ordinary, profile and bootstrap launchers use that composition. The
shared validator retains ownership of the existing schema and bounds; the
parent independently validates the child result. No private payload belongs in
launcher arguments. Test production launchers under `python -I` without an
ambient collection, native error redaction, and malformed bytes reaching the
parent independently of child validation.

[Storage](../../specs/storage.md) owns these credential and diagnostic
boundaries. [Continuation after a fix](continuation-after-a-fix.md) explains why
an automation correction can use a new build without setup when the runtime
lock is unchanged. Each phase's retained attempt budget still applies; a
rebuild does not replenish an exhausted image-pull journal.
