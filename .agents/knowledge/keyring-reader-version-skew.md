# A keyring an older build cannot read is intact

Observed 2026-10-27 while adding the produced-entry `identity` member and the
index `minReader` member to the local keyring (B829 review fixes, lane R).

## Symptom

After a build that records a produced entry's `identity` or the index's
`minReader` has written a context's keyring, an older build reading that
keyring refuses with `secret.store.corrupt` ("decrypted secret metadata is
invalid"). Builds before this change remedy it with a restore from backup or a
purge, and following that destroys a keyring that is intact. The
`secret.store.corrupt` default remediation
(`internal/diagnostics/remediation.go`) now names the newer build first.

## Cause

The keyring's encrypted metadata is decoded strictly
(`internal/secrets/localkeyring/format.go`, `decodeCanonical` with unknown
members refused), so a build that does not know a member cannot tell an
additive member a newer build wrote from damage. `formatVersion` (4) did not
move, because moving it would make every existing keyring unreadable to the
new build, with no converter (pre-1.x rule).

## Rule

- Treat `secret.store.corrupt` on a store a newer build has used as version
  skew first: run the build that wrote it, or a newer one. Do not restore or
  purge it.
- From this build on, the metadata names `minReader` (this build's reader
  version, 1), and a reader checks it before strict decoding
  (`decodeIndexPlaintext` in `internal/secrets/localkeyring/metadata.go`). A
  later additive member raises `minReader`, so an older build that knows the
  guard refuses with `secret.store.newer` ("the store is intact") instead of
  `secret.store.corrupt`. Builds older than the guard still report corrupt.
- `TestMetadataForANewerReaderRefusesAsNewerNotCorrupt` holds the guard;
  `TestTheMetadataGoldenPinsFormatFour` pins the members.

The required behavior is in [secrets.md](../../specs/secrets.md) (the encrypted
metadata members).
