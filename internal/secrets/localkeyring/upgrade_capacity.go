package localkeyring

import (
	"context"
	"crypto/sha256"
	"strings"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// Area's physical budget includes the source, unpublished output and atomic
// replacement files. Source artifacts stay intact until the upgrade commits.
const maxUpgradePhysicalBytes = int64(256 << 20)

func preflightUpgradeCapacity(ctx context.Context, area secretstore.Area, next indexRecord, plaintextBytes int, intent, priorIntent []byte) error {
	root, err := area.Entries(ctx, "")
	if err != nil {
		return areaFailure(ctx, "store.corrupt", "secret upgrade capacity cannot be inspected", err)
	}
	entries, bytes := 0, int64(0)
	journalSeen := false
	add := func(entry secretstore.Entry) error {
		if entry.Size < 0 || entry.Size > maxUpgradePhysicalBytes {
			return secretstore.Failure("store.corrupt", "secret upgrade artifact size is invalid")
		}
		entries++
		if !entry.Directory {
			bytes += entry.Size
		}
		return checkUpgradeCapacity(entries, bytes)
	}
	for _, entry := range root {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := add(entry); err != nil {
			return err
		}
		if entry.Name == upgradePath {
			if entry.Directory || journalSeen || entry.Size != int64(len(priorIntent)) || len(priorIntent) == 0 {
				return secretstore.Failure("store.conflict", "secret upgrade intent changed before capacity inspection")
			}
			journalSeen = true
		}
		if !entry.Directory {
			continue
		}
		children, err := area.Entries(ctx, entry.Name)
		if err != nil {
			return areaFailure(ctx, "store.corrupt", "secret upgrade capacity cannot be inspected", err)
		}
		for _, child := range children {
			if child.Directory {
				return secretstore.Failure("store.corrupt", "secret upgrade contains an unsupported directory")
			}
			if err := add(child); err != nil {
				return err
			}
		}
	}
	if journalSeen != (len(priorIntent) != 0) {
		return secretstore.Failure("store.conflict", "secret upgrade intent changed before capacity inspection")
	}
	// Replace first writes a new journal beside its predecessor, then renames.
	if err := checkUpgradeCapacity(entries+1, bytes+int64(len(intent))); err != nil {
		return err
	}
	if !journalSeen {
		entries++
	}
	bytes += int64(len(intent) - len(priorIntent))
	if len(next.Keys) != 1 || next.Keys[0].ID != next.ActiveKey {
		return secretstore.Failure("store.corrupt", "secret upgrade has an invalid destination key")
	}
	usageBytes, err := canonicalEncodedSize(sealLedger{FormatVersion: formatVersion, KeyID: next.ActiveKey, Seals: next.Keys[0].Seals, MAC: strings.Repeat("A", rawBase64.EncodedLen(sha256.Size))}, ledgerMaximum)
	if err != nil {
		return err
	}
	entries += 2
	bytes += 32 + int64(usageBytes)
	if err := checkUpgradeCapacity(entries, bytes); err != nil {
		return err
	}
	for _, version := range next.Versions {
		for _, part := range version.Parts {
			size, err := sealedEnvelopeSize(part.Size, "part", next.ActiveKey, part.BlobID, partMaximum)
			if err != nil {
				return err
			}
			entries++
			bytes += int64(size)
			if err := checkUpgradeCapacity(entries, bytes); err != nil {
				return err
			}
		}
	}
	metadataBytes, err := metadataEncodedSize(plaintextBytes, next.Selector, next.ActiveKey)
	if err != nil {
		return err
	}
	// The final store.json is absent until this temporary metadata is renamed.
	return checkUpgradeCapacity(entries+1, bytes+int64(metadataBytes))
}

func checkUpgradeCapacity(entries int, bytes int64) error {
	if entries < 0 || entries > maxPhysicalItems || bytes < 0 || bytes > maxUpgradePhysicalBytes {
		return secretstore.Failure("store.limit", "legacy secret storage needs upgrade headroom within the 256 MiB and 32768-entry limits; preserve the source store")
	}
	return nil
}
