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

## Registry configuration JSON names

A generic `discovery` refusal can follow successful manifest and config-blob
digest verification. The registry decoder previously folded every JSON name
before detecting duplicates, rejecting a config whose nested metadata used
distinct names differing only in case. The retained role output may show only
the fixed refusal because the acquisition tasks carry `no_log`.

The [direct-digest regression](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_ceph_registry_discovery.py)
authenticates synthetic config bytes and exercises that metadata shape for
both manifest media types. The tests also retain identical and escaped-equivalent
duplicate rejection. The
[image-resolution contract](../../specs/ceph-coverage.md#release-qualification)
owns the JSON comparison rule. Diagnose the failed boundary before changing
credentials or pins, and keep raw responses and credential-bearing locations
private.

## Blob delivery delegation and bounded DNS

The [registry API](https://distribution.github.io/distribution/spec/api/#fetch-blob)
permits a blob GET to return 307 with a separate delivery location. Successful
index resolution does not prove direct blob access: the
[image resolver](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_registry_discovery.py)
can select and verify a platform manifest without fetching its config blob.
The manifest and blob paths therefore need separate boundary coverage.

The [storage contract](../../specs/ceph.md#bootstrap-identity-and-continuation)
defines the broker's single delivery hop and credential separation. A signed
Location is private capability data, even though registry credentials do not
travel with it. Keep it inside the broker and normalize delivery metadata and
errors before the native client sees them. Do not infer a header remedy from
the different manifest and blob Accept values without comparative evidence.

A socket connect timeout does not bound DNS resolution. Registry addresses are
resolved before broker workers start; a delivery hostname is known only after
a registry response. Running its resolver directly in a joinable thread can
strand teardown. Reuse the existing native process owner for a short isolated
host-Python resolver, then validate every numeric answer and pin the addresses
for TLS connections. Never use a bare Python fork from a broker thread or start
an independent process session outside the remote supervisor's group. Synthetic
tests must cover a stalled resolver, prohibited or mixed answers, hostname
verification, private output and shutdown during resolution.

- Blob and config delivery follow one 301, 302, 303, 307 or 308 hop without
  credentials: quay.io answers a blob GET with 302, and the
  [containers/image](https://raw.githubusercontent.com/containers/image/v5.34.3/docker/docker_client.go)
  client follows all five through
  [Go's HTTP client](https://pkg.go.dev/net/http#Client.Do). A broker or
  resolver that admits only 307 and 308 fails every such blob.
- The native client reads signature-lookaside configuration from
  `$HOME/.config/containers/registries.d` when that directory exists, and
  otherwise from `/etc/containers/registries.d`
  ([registries_d.go:87-105](https://github.com/containers/image/blob/v5.34.3/docker/registries_d.go#L87-L105)).
  containers-common there maps registry.redhat.io to an HTTPS lookaside. Every
  copy reads source signatures
  ([single.go:117](https://github.com/containers/image/blob/v5.34.3/copy/single.go#L117)),
  and any lookaside response other than 404 or a successful fetch fails the pull
  ([docker_image_src.go:540-566](https://github.com/containers/image/blob/v5.34.3/docker/docker_image_src.go#L540-L566)).
  The broker admits only `/v2/` paths, so it gives the native client an empty
  directory under its private HOME. The client then uses the root default
  `file:///var/lib/containers/sigstore`
  ([registries_d.go:195-201](https://github.com/containers/image/blob/v5.34.3/docker/registries_d.go#L195-L201)),
  where a missing file means no signatures.

## Auxiliary images and native deploy pulls

A daemon whose image is a `mgr/cephadm/container_image_<name>` setting, such as
haproxy or prometheus, is pulled by the native deploy itself when the host
lacks it. In cephadm v20.2.1 the
[container run builder](https://raw.githubusercontent.com/ceph/ceph/v20.2.1/src/cephadm/cephadmlib/container_types.py)
(`build_engine_run_args`, lines 82-95) adds
`--authfile=/etc/ceph/podman-auth.json` to every podman run when that file
exists. The [cephadm binary](https://raw.githubusercontent.com/ceph/ceph/v20.2.1/src/cephadm/cephadm.py)'s
deploy path (`command_deploy` at line 2917, `deploy_daemon` at line 872) calls
no explicit `_pull_image`: its call sites are `cephadm pull` (line 1773),
bootstrap (line 2661) and adopt (line 3356). A missing daemon image is
therefore pulled implicitly by podman run's default missing policy, with that
authfile. The [manager's serve loop](https://raw.githubusercontent.com/ceph/ceph/v20.2.1/src/pybind/mgr/cephadm/serve.py)
logs in to the custom registry before each deploy (lines 1411-1412);
`convert_tags_to_repo_digest` (lines 1225-1245) skips a setting that is already
a repository digest, and `_get_container_image_info` (lines 1790-1823) runs
`cephadm pull` only while converting a tag.

`/etc/ceph/podman-auth.json` is the protected guest registry record the
[core adapter](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py)
writes (`REGISTRY_FILE`); `registry_scope` admits exactly one entry, the frozen
registry authority. Every frozen auxiliary reference sits beneath that same
authority, so a native deploy can pull one only through that single-authority
record, and no service family refuses on this ground. Host preparation
pre-pulls each frozen auxiliary digest, so podman run normally pulls nothing.

The [pull broker](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_pull_broker.py)
serves one frozen reference per instance: the registry issues a bearer token
scoped to one repository (`repository:<namespace>:pull`), and the broker's
manifest and blob admission names that repository and digest alone. A broker
for a reference outside the frozen image set refuses before any connection,
file or directory. Each auxiliary pull journals its own
`auxiliary-<name>-attempt-<n>.json` records under the host block's markers and
follows the Ceph pull's per-run budget: each preparation run makes up to three
bounded attempts, and a later apply whose auxiliary image is still absent pulls
again within its own three.

## Pull scratch and durable journal names overlap

A resumed native host profile on 2026-10-31 refused at
`ceph_host_runtime.discard_residue:265` before image inspection. Core pull
scratch uses `pull-`, but its durable attempt files are also named
`pull-attempt-<n>.json` in that directory. The prefix-only residue scan
therefore selected an owned regular journal file and refused its type.
A temporary reproduction using the production marker writer produced the
same refusal without a native command or any changed journal bytes.

The earlier recovery tests put records only in an in-memory mapping and
scratch on disk. Exercise both through real filesystem entries when their
names share a directory. Validate retained journals before cleanup and exempt
only those proved names. Refuse unvalidated entries in the reserved journal
namespace, including a directory after a gap or beyond the attempt bound;
a directory/type check alone would mistake it for removable scratch. Keep
the old scratch prefix recoverable rather than stranding its private material
by renaming future directories. Unknown matching files, symlinks and foreign
owners still refuse. [The host preparation contract](../../specs/ceph.md#bootstrap-identity-and-continuation)
owns recovery; [B1012](../../specs/milestones/delivered.md#x289--owner-acceptance-of-the-three-node-ceph-run-of-2026-11-03) records this repair
and its acceptance. This is independent of the image-policy
parser: the resumed pull can fail before that probe is reached again.

## Bootstrap container-engine initialization

[X79](../../specs/milestones/delivered.md#x79--cephadm-bootstrap-reads-its-podman-version)
records why bootstrap must initialize its own container engine. A successful
`cephadm check-host` in a separate process does not initialize bootstrap's
process-local Podman version. Keep `--skip-prepare-host` out of both bootstrap
argument builders, including when accepting arbitrary authored package
coordinates; version-policy changes must preserve this behavior.

The [no-change host proof](../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py)
must precede even the selected native host check: the native time-service
checker can enable and start an installed unit. Prove the executable dependency
and ordered time-service conditions with read-only probes first, then let the
selected native tool check its own prerequisites. Refuse before any bootstrap
attempt record when those checks fail. Tests must cover this complete ordering,
not substitute a successful native command for the proof.

## Native helper source inspected through a ZIP loader

Ansible imports its packaged module utilities from a ZIP. For such a module,
`inspect.getsource` receives loader text through `linecache`, whose
`splitlines` reconstruction treats literal Unicode line and paragraph
separators as source line breaks. A quoted string containing those literal
characters in `ceph_native_object` therefore becomes invalid Python when the
native bundle is inspected, although direct filesystem inspection succeeds.

The display-name check keeps the same forbidden runtime characters through
ASCII source escapes. The
[native worker tests](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_ceph_native_worker.py)
must cross the genuine ZIP loader and inspection boundary before running the
production launcher; compiling filesystem source or reading raw ZIP bytes
alone does not exercise that transformation. The
[execution-site contract](../../specs/architecture.md#language-responsibility)
owns the shipped source closure and managed-host floor.

This compilation fails before the precreated diagnostic module has executed.
Calling its absent validator can mask the original error in both failure
projections, leaving an empty result frame; that separate diagnostic gap was repaired as
[B1010](../../specs/milestones/delivered.md#x289--owner-acceptance-of-the-three-node-ceph-run-of-2026-11-03). The source escape removes the
observed carrier trigger without changing source admission or adding a
release-specific loader.

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

A separate diagnostic loss occurs when child result encoding, parent
collection or the ordinary entrypoint catches an exception and returns only
`native-unknown`. Retain the shared bounded exception-type and code-site
projection through those refusals. A generic `ValueError` raised by the
shared `require` helper otherwise names that helper for every failed check.
The projection already tracks the last code-owned frame outside `require`;
selecting that caller preserves the failing check without exception values or
an expanded source-attestation policy. A child may publish a replacement failure
only before it writes any result bytes; a failed or partial pipe write must
never append a second frame. The parent still validates the child result, and
the controller still attests a cause against its shipped sources. Diagnostic
repair changes no readiness decision or retry authority and does not identify
the original host failure by itself. The
[worker boundary tests](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/test_ceph_native_worker.py)
own these encoding, refusal and disclosure regressions.

[Storage](../../specs/ceph.md) owns these credential and diagnostic
boundaries. [Continuation after a fix](continuation-after-a-fix.md) explains why
an automation correction can use a new build without setup when the runtime
lock is unchanged. The image pull's budget is per preparation run: each run
makes up to three bounded attempts, and the retained `pull-attempt-N` records
only show that an attempt began, for crash diagnosis and recovery. A later
apply whose image is still absent pulls again within its own three attempts,
so a pull fix continues after an exhausted journal (B648).
