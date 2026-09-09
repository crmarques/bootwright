package localstore

import (
	"context"
	"strings"

	"github.com/crmarques/bootwright/internal/secrets/storage"
)

func inspectArtifacts(ctx context.Context, area storage.Area, selector storage.Selector, index indexRecord) (int, bool, error) {
	root, err := area.Entries(ctx, "")
	if err != nil {
		return 0, false, areaFailure(ctx, "store.corrupt", "secret artifacts cannot be enumerated safely", err)
	}
	directories := map[string]bool{"identities": false, "indexes": false, "keys": false, "ledgers": false, "parts": false}
	count := len(root)
	retained := 0
	selectorSeen, initializationSeen := false, false
	for _, entry := range root {
		if entry.Directory {
			if _, exists := directories[entry.Name]; !exists || directories[entry.Name] {
				return 0, false, storage.Failure("store.corrupt", "secret store contains an unsupported directory")
			}
			directories[entry.Name] = true
			continue
		}
		if entry.Name == selectorPath || entry.Name == initializationPath {
			if entry.Size <= 0 || entry.Size > selectorMaximum {
				return 0, false, storage.Failure("store.corrupt", "secret selector size is invalid")
			}
			selectorSeen = selectorSeen || entry.Name == selectorPath
			initializationSeen = initializationSeen || entry.Name == initializationPath
			continue
		}
		if !validPending(entry.Name) {
			return 0, false, storage.Failure("store.corrupt", "secret store contains an unsupported artifact")
		}
		retained++
	}
	for _, present := range directories {
		if !present {
			return 0, false, storage.Failure("store.corrupt", "secret store is missing a required artifact directory")
		}
	}
	if !selectorSeen || !initializationSeen {
		return 0, false, storage.Failure("store.corrupt", "secret store publication records are incomplete")
	}
	initializationData, exists, err := area.Read(ctx, initializationPath, selectorMaximum)
	if err != nil || !exists {
		return 0, false, areaFailure(ctx, "store.corrupt", "secret initialization record is missing or unsafe", err)
	}
	var initialization initializationRecord
	selected := storage.Context{ID: selector.ContextID}
	if decodeCanonical(initializationData, selectorMaximum, 512, &initialization) != nil || !validInitialization(initialization, selected, selector.Selection) || initialization.MAC == "" {
		return 0, false, storage.Failure("store.corrupt", "secret initialization record is invalid")
	}
	signingKey, err := initializationSigningKey(ctx, area, initialization)
	if err != nil {
		return 0, false, err
	}
	clear(signingKey)
	referencedKeys := make(map[string]bool, len(index.Keys))
	referencedLedgers := make(map[string]bool, len(index.Keys))
	for _, key := range index.Keys {
		referencedKeys[key.ID+".bin"] = true
		referencedLedgers[key.ID+".json"] = true
	}
	referencedIndexes := map[string]bool{selector.Generation + ".bin": true}
	referencedParts := make(map[string]bool)
	for _, version := range index.Versions {
		for _, part := range version.Parts {
			referencedParts[part.BlobID+".bin"] = true
		}
	}
	tables := []struct {
		directory  string
		prefix     string
		suffix     string
		referenced map[string]bool
		maximum    int64
		exact      int64
	}{
		{directory: "indexes", prefix: "gen-", suffix: ".bin", referenced: referencedIndexes, maximum: indexMaximum},
		{directory: "keys", prefix: "key-", suffix: ".bin", referenced: referencedKeys, maximum: 32, exact: 32},
		{directory: "ledgers", prefix: "key-", suffix: ".json", referenced: referencedLedgers, maximum: ledgerMaximum},
		{directory: "parts", prefix: "blob-", suffix: ".bin", referenced: referencedParts, maximum: partMaximum},
	}
	for _, table := range tables {
		entries, err := area.Entries(ctx, table.directory)
		if err != nil {
			return 0, false, areaFailure(ctx, "store.corrupt", "secret artifacts cannot be enumerated safely", err)
		}
		count += len(entries)
		seen := make(map[string]bool, len(entries))
		for _, entry := range entries {
			if entry.Directory || seen[entry.Name] {
				return 0, false, storage.Failure("store.corrupt", "secret artifact layout is inconsistent")
			}
			seen[entry.Name] = true
			valid := validPending(entry.Name)
			if strings.HasSuffix(entry.Name, table.suffix) {
				valid = valid || validID(strings.TrimSuffix(entry.Name, table.suffix), table.prefix)
			}
			if !valid {
				return 0, false, storage.Failure("store.corrupt", "secret artifact name is invalid")
			}
			if table.referenced[entry.Name] {
				if entry.Size <= 0 || entry.Size > table.maximum || table.exact != 0 && entry.Size != table.exact {
					return 0, false, storage.Failure("store.corrupt", "referenced secret artifact size is invalid")
				}
			} else {
				retained++
			}
		}
		for name := range table.referenced {
			if !seen[name] {
				return 0, false, storage.Failure("store.corrupt", "referenced secret artifact is missing")
			}
		}
	}
	identities, err := area.Entries(ctx, "identities")
	if err != nil {
		return 0, false, areaFailure(ctx, "store.corrupt", "secret identities cannot be enumerated safely", err)
	}
	count += len(identities)
	for _, entry := range identities {
		if !entry.Directory && validPending(entry.Name) {
			retained++
			continue
		}
		name := strings.TrimSuffix(entry.Name, ".json")
		if entry.Directory || !strings.HasSuffix(entry.Name, ".json") || !validID(name, "ver-") && !validID(name, "bind-") || entry.Size <= 0 || entry.Size > selectorMaximum {
			return 0, false, storage.Failure("store.corrupt", "secret identity tombstone is invalid")
		}
		data, exists, err := area.Read(ctx, "identities/"+entry.Name, selectorMaximum)
		if err != nil || !exists {
			return 0, false, areaFailure(ctx, "store.corrupt", "secret identity tombstone is missing or unsafe", err)
		}
		var identity identityRecord
		if decodeCanonical(data, selectorMaximum, 16, &identity) != nil || identity.FormatVersion != formatVersion || identity.ContextID != selector.ContextID || identity.ID != name {
			return 0, false, storage.Failure("store.corrupt", "secret identity tombstone is invalid")
		}
	}
	if count > maxPhysicalItems {
		return 0, false, storage.Failure("store.limit", "secret artifact count exceeds its limit")
	}
	return retained, retained != 0, nil
}

func validPending(name string) bool {
	return validID(name, "pending-")
}
