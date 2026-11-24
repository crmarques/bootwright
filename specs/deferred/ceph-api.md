# Storage field detail

[B472](../milestones/m2.md#b472) revives this detail when it qualifies IBM Ceph
9.9.x lifecycle inputs; [B55](../milestones/m7.md#b55) later reuses that
qualification for standalone native files. Until then this page holds only the
shape that no admitted kind owns: the device selector `size` grammar. Each
admitted kind's field record lives on the one page its row in the
[kinds table](../api/ceph.md#kinds) links, the
[native input](../ceph-native-api.md) owns the native field rules, and the
closed schema in `api/v1alpha1/storage.go` is authoritative for field names,
types and shapes.

## Device selector size

The following grammar matches the complete value, without surrounding
whitespace, signs, fractional parts or additional suffixes. `N` denotes one or
more decimal digits with a positive integer value; leading zeros are preserved.
Units and tokens are case-sensitive. The check qualifies declarative syntax,
not compatibility with a particular native release.

| Field | Accepted value |
| --- | --- |
| Device selector `size` | One bound, `LOW:HIGH`, `LOW:`, or `:HIGH`; each bound is `N` followed by `M`, `G`, `T`, `MB`, `GB`, or `TB`. Both bounds cannot be empty. Compare bounds using powers of 1024 and require `LOW <= HIGH`. |
