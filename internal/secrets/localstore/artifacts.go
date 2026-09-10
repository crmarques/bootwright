package localstore

import (
	"context"
	"strings"

	"github.com/crmarques/bootwright/internal/secrets/storage"
)

func inspectArtifacts(ctx context.Context, area storage.Area, selector storage.Selector, index indexRecord) (int, bool, error) {
	paths, err := obsoleteArtifacts(ctx, area, selector, index)
	return len(paths), len(paths) != 0, err
}

func obsoleteArtifacts(ctx context.Context, area storage.Area, selector storage.Selector, index indexRecord) ([]string, error) {
	legacyPaths := []string{}
	legacy := map[string]bool{}
	if index.Legacy {
		var err error
		legacyPaths, err = legacyUpgradeArtifacts(ctx, area)
		if err != nil {
			return nil, err
		}
		for _, path := range legacyPaths {
			legacy[path] = true
		}
	}

	root, err := area.Entries(ctx, "")
	if err != nil {
		return nil, areaFailure(ctx, "store.corrupt", "secret artifacts cannot be enumerated safely", err)
	}
	directories := map[string]bool{"identities": false, "keys": false, "parts": false}
	obsolete := legacyPaths
	count := len(root)
	metadataSeen := false
	for _, entry := range root {
		if legacy[entry.Name] {
			continue
		}
		if entry.Directory {
			if _, exists := directories[entry.Name]; !exists || directories[entry.Name] {
				return nil, storage.Failure("store.corrupt", "secret store contains an unsupported directory")
			}
			directories[entry.Name] = true
			continue
		}
		if entry.Name == selectorPath {
			if entry.Size <= 0 || entry.Size > storage.RecordMaximum {
				return nil, storage.Failure("store.corrupt", "secret metadata size is invalid")
			}
			metadataSeen = true
			continue
		}
		if entry.Name != initializationPath && !validPending(entry.Name) {
			return nil, storage.Failure("store.corrupt", "secret store contains an unsupported artifact")
		}
		obsolete = append(obsolete, entry.Name)
	}
	if !metadataSeen {
		return nil, storage.Failure("store.corrupt", "secret metadata is missing")
	}
	for _, present := range directories {
		if !present {
			return nil, storage.Failure("store.corrupt", "secret store is missing a required directory")
		}
	}
	referenced := map[string]int64{}
	for _, key := range index.Keys {
		referenced[keyPath(key.ID)] = 32
		referenced[ledgerPath(key.ID)] = ledgerMaximum
	}
	for _, version := range index.Versions {
		for _, part := range version.Parts {
			referenced[partPath(part.BlobID)] = partMaximum
		}
	}
	for _, directory := range []string{"keys", "parts"} {
		entries, err := area.Entries(ctx, directory)
		if err != nil {
			return nil, areaFailure(ctx, "store.corrupt", "secret artifacts cannot be enumerated safely", err)
		}
		count += len(entries)
		for _, entry := range entries {
			path := directory + "/" + entry.Name
			if legacy[path] {
				continue
			}
			valid := validPending(entry.Name)
			if directory == "keys" {
				valid = valid || strings.HasSuffix(entry.Name, ".key") && validID(strings.TrimSuffix(entry.Name, ".key"), "key-") || strings.HasSuffix(entry.Name, ".usage.json") && validID(strings.TrimSuffix(entry.Name, ".usage.json"), "key-")
			}
			if directory == "parts" {
				valid = valid || strings.HasSuffix(entry.Name, ".enc") && validID(strings.TrimSuffix(entry.Name, ".enc"), "blob-")
			}
			if entry.Directory || !valid {
				return nil, storage.Failure("store.corrupt", "secret artifact name or type is invalid")
			}
			if maximum, exists := referenced[path]; exists {
				if entry.Size <= 0 || entry.Size > maximum || maximum == 32 && entry.Size != 32 {
					return nil, storage.Failure("store.corrupt", "referenced secret artifact size is invalid")
				}
				delete(referenced, path)
			} else {
				obsolete = append(obsolete, path)
			}
		}
	}
	if len(referenced) != 0 {
		return nil, storage.Failure("store.corrupt", "referenced secret artifact is missing")
	}
	identities, err := area.Entries(ctx, "identities")
	if err != nil {
		return nil, areaFailure(ctx, "store.corrupt", "secret identities cannot be enumerated safely", err)
	}
	count += len(identities)
	reserved := make(map[string]bool, len(index.Versions)+len(index.Bindings))
	for _, version := range index.Versions {
		reserved[version.ID] = true
	}
	for _, binding := range index.Bindings {
		reserved[binding.ID] = true
	}
	for _, entry := range identities {
		if !entry.Directory && validPending(entry.Name) {
			obsolete = append(obsolete, "identities/"+entry.Name)
			continue
		}
		name := strings.TrimSuffix(entry.Name, ".json")
		if entry.Directory || !strings.HasSuffix(entry.Name, ".json") || !validID(name, "ver-") && !validID(name, "bind-") || entry.Size <= 0 || entry.Size > selectorMaximum {
			return nil, storage.Failure("store.corrupt", "secret identity reservation is invalid")
		}
		data, exists, err := area.Read(ctx, "identities/"+entry.Name, selectorMaximum)
		if err != nil || !exists {
			return nil, areaFailure(ctx, "store.corrupt", "secret identity reservation is missing or unsafe", err)
		}
		var identity identityRecord
		if decodeCanonical(data, selectorMaximum, 16, &identity) != nil || (identity.FormatVersion != formatVersion && identity.FormatVersion != 1) || identity.ContextID != selector.ContextID || identity.ID != name {
			return nil, storage.Failure("store.corrupt", "secret identity reservation is invalid")
		}
		delete(reserved, name)
	}
	if len(reserved) != 0 {
		return nil, storage.Failure("store.corrupt", "referenced secret identity reservation is missing")
	}
	if count > maxPhysicalItems {
		return nil, storage.Failure("store.limit", "secret artifact count exceeds its limit")
	}
	return obsolete, nil
}

func (s *session) collectArtifacts(ctx context.Context) error {
	paths, err := obsoleteArtifacts(ctx, s.area, s.selector, s.index)
	if err != nil {
		return err
	}
	if len(paths) != 0 {
		if err := s.area.Prune(ctx, s.selectorData, paths); err != nil {
			return areaFailure(ctx, "store.conflict", "secret cleanup is incomplete; retry secret encryption init", err)
		}
	}
	s.retained, s.cleanup = 0, false
	for id, value := range s.keys {
		if !indexHasKey(s.index, id) {
			clear(value)
			delete(s.keys, id)
		}
	}
	return nil
}

func validPending(name string) bool { return validID(name, "pending-") }
