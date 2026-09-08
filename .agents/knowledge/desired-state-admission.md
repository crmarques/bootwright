# Desired-state admission observations

The admission parser is unmodified `go.yaml.in/yaml/v3` v3.0.5. It composes a
whole document before the adapter can inspect representation depth and nodes.
The adapter therefore checks verified byte bounds first and checks structural
bounds at each returned document. The isolated
[qualification tests](../../internal/desiredstate/yamlstream/qualification_linux_test.go)
measure child peak RSS and enforce a watchdog; those measurements do not turn
cooperative in-process cancellation into a hard interruption guarantee.

This parser accepts a YAML 1.1 version directive and rejects a `%YAML 1.2`
directive. The compiler nevertheless applies its own
[scalar spelling rules](../../specs/api.md#yaml-streams-and-decoding), rather
than accepting the library's scalar resolution as schema admission. In
particular, large decimal integers must be recognized lexically before any
binary64 conversion. The parser also loses a nonspecific `!` tag's explicitness
in its returned node, so the adapter retains that information from the bounded
source coordinates. These cases have regressions in
[parser tests](../../internal/desiredstate/yamlstream/parser_test.go).

Native-map keys can approach the per-file byte ceiling through explicit YAML
keys. Storing each descendant's full string path multiplies that key by the
number of descendants. The compiler instead keeps the typed native container
as its provenance boundary; decode errors retain the offending node's line
and column. Named typed-list normalization retains index remappings so source
locations still follow their authored or inherited element after sorting.

Effective inspection output and authored input are different representations.
Derived Machine access and endpoint addresses can be valid inspection values
while remaining forbidden authored fields. Context storage must preserve
authored sources and origins, rather than re-admitting inspection output.
[Compiler](../../internal/desiredstate/compilation/compiler_test.go) and
[encoder](../../internal/desiredstate/encoding/encoding_test.go) tests exercise
that separation and presence-preserving values.

The pinned vulnerability scanner supports an official database copied locally
with `-db file:///absolute/path`. `make check VULNDB=file:///absolute/path`
uses that copy for dependency matching without transmitting project module
names. A complete copy contains the official `index/db.json`,
`index/modules.json`, and every referenced `ID/GO-*.json`; indexes and advisory
identities must be checked when acquiring a snapshot. A local database's
freshness and completeness remain part of the scan evidence.
