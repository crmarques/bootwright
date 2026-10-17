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

## Manifest requests and repeated Accept headers

The native client's
[manifest request](https://raw.githubusercontent.com/containers/image/v5.34.3/docker/docker_client.go)
adds one `Accept` header for each supported manifest media type. Rejecting every
repeated header closes an otherwise valid manifest connection: CONNECT and
mutual TLS succeed, but the native client receives EOF before any upstream
manifest request. A test with one `Accept` line does not exercise that request.

The broker permits repeated `Accept` only within the existing total header-byte
limit. It discards those values locally and builds its own upstream headers;
they neither select an upstream authority nor carry authentication. Other
duplicate headers still refuse after case normalization, including Host,
framing and credential headers. Real loopback tests cover repeated native
manifest media types, no client-header forwarding, and refusal before upstream
access for ambiguous or credential-bearing requests. EOF can also represent
another broker refusal; the synthetic reproduction identifies this parser
failure without proving a real registry pull succeeded.

## Upstream HTTP status lost behind a broker 502

The broker maps a final non-200 registry response to a local 502. Its JSON
body includes the original status, but a native client may discard that body:
[containers/image's error handler](https://raw.githubusercontent.com/containers/image/v5.34.3/docker/distribution_error.go)
parses registry error bodies for 4xx responses and otherwise reports the HTTP
status alone. A native blob error naming 502 therefore identifies neither a
redirect nor a particular upstream refusal. Authentication, availability,
rate limits and redirects must not be inferred from that status.

Retain a bounded invocation-local observation independently of the downstream
body: a fixed request class and integer upstream HTTP status only. After broker
workers finish, prepend that observation to the existing bounded native cause,
so a long native stderr cannot truncate it away. Label it as observed during
the invocation rather than claiming it was the final native retry's response.
No successful response creates a refusal, and a new broker starts without an
observation. Never retain response headers, signed redirect locations, raw URLs
or authentication challenges in that observation. Synthetic refusals exercise
this path without changing redirect, authority or credential policy.

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
