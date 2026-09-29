# A String the Installer Reads as a Number

Observed in September 2026 while carrying every root-device hint to the agent
installer (X19, item B4).
[Container clusters](../../specs/container-clusters.md#installer-inputs)
owns what the installer inputs carry; this page records why the role writes
`agent-config.yaml` through the collection's `to_installer_yaml` filter rather
than through `to_nice_yaml` or `to_nice_json`.

## What each side does with a plain scalar

The installer reads `agent-config.yaml` in one place,
[`(*AgentConfig).Load`](https://github.com/openshift/installer/blob/006669f5812a47dbc733b6736584b87ef696e898/pkg/asset/agent/agentconfig/agent_config.go#L128-L154),
with `sigs.k8s.io/yaml` `UnmarshalStrict`. Installer 006669f (release-4.21)
pins `sigs.k8s.io/yaml` v1.6.0
([go.mod](https://github.com/openshift/installer/blob/006669f5812a47dbc733b6736584b87ef696e898/go.mod#L150))
over `go.yaml.in/yaml/v2` v2.4.2
([go.mod](https://github.com/openshift/installer/blob/006669f5812a47dbc733b6736584b87ef696e898/go.mod#L179)).
That reader resolves a plain scalar as YAML 1.1 does:

- `y`, `Y`, `n` and `N` are booleans
  ([resolve.go](https://github.com/yaml/go-yaml/blob/v2.4.2/resolve.go#L37-L42));
- a numeric-looking scalar is tried as `strconv.ParseInt(plain, 0, 64)`, which
  takes `0o17` and `0x...`, then as an unsigned integer, then as a float
  ([resolve.go](https://github.com/yaml/go-yaml/blob/v2.4.2/resolve.go#L151-L165))
  whose [pattern](https://github.com/yaml/go-yaml/blob/v2.4.2/resolve.go#L84)
  needs no dot and accepts a leading zero.

`sigs.k8s.io/yaml` then turns a number read for a string field back into text
with `FormatInt` or `FormatFloat(v, 'g', -1, 32)` and reports no error
([yaml.go](https://github.com/kubernetes-sigs/yaml/blob/v1.6.0/yaml.go#L328-L356)).

ansible-core's `to_nice_yaml` follows PyYAML, which quotes only what its own
resolver reads as something other than a string: its float needs a dot, its
octal is `0[0-7_]+` and its booleans exclude `y` and `n`
([resolver.py](https://github.com/yaml/pyyaml/blob/6.0.3/lib/yaml/resolver.py#L170-L193)).
The two disagree on exactly the values that matter.

## The probe

With ansible-core 2.21.4 and PyYAML 6.0.3, templating the real task left
`1e3`, `0o17`, `0987654321`, `09876`, `08`, `12E45`, `1E+3`, `5E2`, `y`, `Y`
and `n` plain, and quoted `'1:0:0:0'`, `'0123'` and `'0x5000c500a1b2c3d4'`.
PyYAML read every value back unchanged. The installer's own reader, decoding
each into a string field as the installer's host and root-device hint types
declare it, returned no error and:

| Written plain | Read as |
| --- | --- |
| `1e3`, `1E+3` | `1000` |
| `5E2` | `500` |
| `0o17` | `15` |
| `0987654321` | `9.8765434e+08` |
| `09876`, `08` | `9876`, `8` |
| `12E45` | `+Inf` |
| `Y`, `y` | `true` |
| `n` | `false` |

A hostname of `y` reaches the installer as `true`. The agent keeps a disk only
when every declared hint matches it, comparing `hctl`, `serialNumber` and `wwn`
exactly, so a rewritten one selects no disk
([host_utils.go](https://github.com/openshift/assisted-service/blob/a52b83145beac38e0e0ba5401a8117d5a0983984/internal/host/hostutil/host_utils.go#L186-L246),
at the assisted-service commit the installer's `go.mod` names; whether the
4.21 agent image is built from it is unverified).

## JSON's own escapes are not all YAML the reader takes

Switched to `to_nice_json(indent=2)`, the same probe read every value above
back unchanged through `UnmarshalStrict`, `0` and `false` included. JSON is not
safe as written, though. With `ensure_ascii` on, as `to_nice_json` leaves it, a
code point above U+FFFF becomes a surrogate pair such as `\ud835\udfd9`, and the
reader refuses any `\u` escape from U+D800 to U+DFFF
([scannerc.go](https://github.com/yaml/go-yaml/blob/v2.4.2/scannerc.go#L2452-L2456)).
With `ensure_ascii` off, the raw character is read, but the reader refuses a
raw DEL, C1 control or U+FFFE
([readerc.go](https://github.com/yaml/go-yaml/blob/v2.4.2/readerc.go#L342-L358))
and folds a raw NEL (U+0085) in a quoted string into a space.

Probed in September 2026 with `UnmarshalStrict` at the versions above, over a
host whose root-device hints held each case:

| Written as | Read back |
| --- | --- |
| `to_nice_json`, a surrogate-pair escape | `found invalid Unicode character escape code` |
| raw DEL, C1 control or U+FFFE | `control characters are not allowed` |
| raw NEL | a space |
| raw U+2028, U+2029 or a code point above U+FFFF | unchanged |
| `\u007f`, `\u0080`, `\u0085`, `\ufffe`, `\U0001d7d9` | unchanged |

Such a character is unlikely in a root-device hint, but an nmstate
`description` is free text. Before X19 wrote JSON, `to_nice_yaml` wrote it as a
`\U` escape, which the same probe read back unchanged.

## The decision it informs

Installer inputs are written in JSON's syntax, which is YAML whose every string
is double-quoted, with every character outside printable ASCII escaped and a
code point above U+FFFF written as the eight-digit YAML escape. The
[`to_installer_yaml` filter](../../ansible/collections/ansible_collections/bootwright/core/plugins/filter/to_installer_yaml.py)
writes exactly that; its output read back unchanged through the probe for every
case in both tables. The role's
[build task](../../ansible/collections/ansible_collections/bootwright/core/roles/containercluster_media_agent/tasks/build.yml)
writes the agent configuration through it.
[The task's regression test](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/test_containercluster_root_device_hints.py)
renders that task over such values and reads them back, and
[the filter's test](../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/filter/test_to_installer_yaml.py)
pins each escape.

## Remaining exposure

The same task file still writes `install-config.yaml` through `to_nice_yaml`,
so a declared string there that YAML 1.1 reads as a number or a boolean is
exposed the same way until that file is written through `to_installer_yaml`
too.
