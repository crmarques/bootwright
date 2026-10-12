# A held directory descriptor lists stale on btrfs

Observed 2026-10-12 on Fedora 43, kernel 7.2.9, with the state root and
`/home` on btrfs and `/tmp` on tmpfs.

## Symptom

A proof that lists a directory through a descriptor it holds refuses its own
valid work, or a bound undercounts, only on some hosts and only when a
temporary directory lives on btrfs. Unit suites pass in CI and with the default
`/tmp`, and fail with `TMPDIR` on `/home`.

## Cause

Whether `readdir` shows an entry created after the most recent `opendir` or
`rewinddir` is unspecified by POSIX, and btrfs does not show it: a descriptor
opened on an empty directory, then two directories created, lists `[]` through
`os.listdir(fd)`; a second listing shows both. CPython's `os.listdir(fd)`
duplicates the descriptor, reads from its offset and rewinds only after
reading, so every first listing after an addition is stale, not only the first
after open. tmpfs and XFS show the entries at once. Go's `(*os.File).ReadDir`
reads from the current offset the same way.

## Rule

List a directory through a held descriptor only after rewinding it
(`os.lseek(fd, 0, os.SEEK_SET)`, or `Seek(0, io.SeekStart)` in Go), on the same
descriptor so an identity proof still binds the listing, or list a descriptor
opened after the last possible addition. `controller_files`'s `listed()` is the
collection's helper. A test that models the stale listing (a held descriptor
lists what it held at open or its last rewind) fails without the rewind on any
filesystem; run a unit suite once with `TMPDIR` on a btrfs directory to catch a
new site.
