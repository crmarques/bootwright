# Folded scalars do not unescape `\n` — `join('\n')` joins with a literal backslash-n

**Symptom:** a task that compares a command's output against a Jinja-built string
is permanently non-idempotent. The comparison is always False, so the guarded
block re-stages, re-validates and re-installs its file with `changed_when: true`
on **every** apply, on **every** host, though nothing changed. Nothing fails, so
it survives review; only a `changed=` count that never drops to zero exposes it.

**Root cause:** YAML performs escape processing **only** in double-quoted
scalars. In a folded (`>-`), literal (`|`), plain, or single-quoted scalar,
`\n` reaches Jinja as the two characters backslash and `n`. ansible-core's Jinja
does not unescape string literals either, so `join('\n')` there joins with a
*literal* backslash-n while the node's `stdout` carries a real newline. In a
double-quoted scalar YAML produces the real character first, and Jinja's lexer
then newline-normalizes a `CR` in its source to `LF`.

**Measured on ansible-core 2.20.4** (`{{ ['ab', 'cd'] | join('\n') }}`, and
`replace('\r', '')` applied to a 3-character string built by a double-quoted
seed var):

```text
scalar style     expression                    result
folded (>-)      ['ab','cd'] | join('\n')      len 6   "ab\\ncd"   literal backslash-n
double-quoted    ['ab','cd'] | join('\n')      len 5   "ab\ncd"    real newline
folded (>-)      "a<LF>b" | replace('\r','')   len 3   inert
double-quoted    "a<LF>b" | replace('\r','')   len 2   strips the NEWLINE
folded (>-)      "a<CR>b" | replace('\r','')   len 3   inert
double-quoted    "a<CR>b" | replace('\r','')   len 3   does NOT strip the CR
```

So `replace('\r', '')` is inert in a folded scalar and, in a double-quoted one,
strips newlines while leaving carriage returns untouched — the exact opposite of
what it reads as. It is never a usable way to clean CR out of output; do not
reach for it.

**Fix / rule:** put any escape-bearing filter argument in a **double-quoted**
scalar when the real character is meant (`storage_node_access/tasks/sudoers.yml:6`
is the corrected form), and prefer not to build the comparison on the controller
at all — compare on the node with `test "$(…)" = '…'`, where the shell owns both
sides. Verify by asserting the built string's `| length`, never by reading the
rendered task: both forms look identical in a diff.

The damage is not limited to comparisons. The RGW ingress TLS step built its
`ssl_cert` bundle as `cert ~ "\n" ~ key ~ "\n"` in a folded `set_fact`, so every
gateway got a PEM whose certificate and key were separated by the two characters
backslash and `n` — text cephadm stores without complaint and haproxy cannot
load ([ceph-cephadm-bootstrap-contract.md](ceph-cephadm-bootstrap-contract.md)).
Wherever the joined string is a file's contents rather than a comparand, prefer
asserting the result parses (`openssl x509`/`pkey` over the bundle) to asserting
its length.

This is what made the Ceph node sudoers grant re-install on every apply; the
channel-side half of that story (why a pseudo-terminal also injects CR into any
stdout compared on the controller) is in
[ceph-node-access-privileged-channel.md](ceph-node-access-privileged-channel.md).
