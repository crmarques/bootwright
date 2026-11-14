# Retired: keyring reader-version negotiation

Retired 2026-11-13 by [D267](../../specs/milestones/backlog.md#d267).
The earlier `minReader` protocol and its cross-build readability rule no longer
apply. The keyring has one closed current metadata shape, described in
[secrets](../../specs/secrets.md#local-keyring), and its format stays fixed under
the [development policy](../../specs/project.md#design-priorities-and-non-goals).

An earlier build's store is unsupported development state. Retain its evidence
and use the build that wrote it when needed; do not treat resetting a format
constant as authorization to restore, purge or mutate a real store.
