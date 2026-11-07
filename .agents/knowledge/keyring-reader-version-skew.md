# A keyring an older build cannot read is intact

Observed 2026-10-27 while adding the produced-entry `identity` member and the
index `minReader` member to the local keyring (B829 review fixes, lane R).

## Symptom

After a build that records an additive member in a context's keyring metadata
has written it, a build that does not know the member and reads that keyring
refuses with `secret.store.corrupt` ("decrypted secret metadata is invalid"),
and following the restore or purge that remedy offers destroys a keyring that
is intact. The `secret.store.corrupt` default remediation
(`internal/diagnostics/remediation.go`) names the newer build first.

## Cause

The keyring's encrypted metadata is decoded strictly
(`internal/secrets/localkeyring/format.go`, `decodeCanonical` with unknown
members refused), so a build that does not know a member cannot tell an
additive member a newer build wrote from damage. An additive member does not
move `formatVersion`, because moving it would make every existing keyring
unreadable to the new build, with no converter (pre-1.x rule).

## Rule

- Treat `secret.store.corrupt` on a store a newer build has used as version
  skew first: run the build that wrote it, or a newer one. Do not restore or
  purge it.
- The metadata names `minReader` (this build's `readerVersion`), and a reader
  checks it before strict decoding (`decodeIndexPlaintext` in
  `internal/secrets/localkeyring/metadata.go`). A later additive member raises
  `minReader`, so a build that knows the guard refuses with
  `secret.store.newer` ("the store is intact") instead of
  `secret.store.corrupt`.
- `TestMetadataForANewerReaderRefusesAsNewerNotCorrupt` holds the guard;
  `TestTheMetadataGoldenPinsFormatFour` pins the members.

The required behavior is in [secrets.md](../../specs/secrets.md) (the encrypted
metadata members).
