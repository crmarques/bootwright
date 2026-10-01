package localkeyring

import (
	"context"
	"strings"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func inspectArtifacts(ctx context.Context, area secretstore.Area, selector secretstore.Selector, index indexRecord) (int, bool, error) {
	paths, err := obsoleteArtifacts(ctx, area, selector, index)
	return len(paths), len(paths) != 0, err
}

func obsoleteArtifacts(ctx context.Context, area secretstore.Area, selector secretstore.Selector, index indexRecord) ([]string, error) {
	obsolete, count, err := rootArtifacts(ctx, area)
	if err != nil {
		return nil, err
	}
	stored, entries, err := storedArtifacts(ctx, area, index)
	if err != nil {
		return nil, err
	}
	obsolete, count = append(obsolete, stored...), count+entries
	identities, entries, err := identityArtifacts(ctx, area, selector, index)
	if err != nil {
		return nil, err
	}
	obsolete, count = append(obsolete, identities...), count+entries
	if count > maxPhysicalItems {
		return nil, secretstore.Failure("store.limit", "secret artifact count exceeds its limit")
	}
	return obsolete, nil
}

func rootArtifacts(ctx context.Context, area secretstore.Area) ([]string, int, error) {
	root, err := area.Entries(ctx, "")
	if err != nil {
		return nil, 0, areaFailure(ctx, "store.corrupt", "secret artifacts cannot be enumerated safely", err)
	}
	directories := map[string]bool{"identities": false, "keys": false, "parts": false}
	obsolete := []string{}
	metadataSeen := false
	for _, entry := range root {
		if entry.Directory {
			if _, exists := directories[entry.Name]; !exists || directories[entry.Name] {
				return nil, 0, secretstore.Failure("store.corrupt", "secret store contains an unsupported directory")
			}
			directories[entry.Name] = true
			continue
		}
		if entry.Name == selectorPath {
			if entry.Size <= 0 || entry.Size > secretstore.RecordMaximum {
				return nil, 0, secretstore.Failure("store.corrupt", "secret metadata size is invalid")
			}
			metadataSeen = true
			continue
		}
		if entry.Name != initializationPath && !validPending(entry.Name) {
			return nil, 0, secretstore.Failure("store.corrupt", "secret store contains an unsupported artifact")
		}
		obsolete = append(obsolete, entry.Name)
	}
	if !metadataSeen {
		return nil, 0, secretstore.Failure("store.corrupt", "secret metadata is missing")
	}
	for _, present := range directories {
		if !present {
			return nil, 0, secretstore.Failure("store.corrupt", "secret store is missing a required directory")
		}
	}
	return obsolete, len(root), nil
}

func storedArtifacts(ctx context.Context, area secretstore.Area, index indexRecord) ([]string, int, error) {
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
	obsolete := []string{}
	count := 0
	for _, directory := range []string{"keys", "parts"} {
		entries, err := area.Entries(ctx, directory)
		if err != nil {
			return nil, 0, areaFailure(ctx, "store.corrupt", "secret artifacts cannot be enumerated safely", err)
		}
		count += len(entries)
		for _, entry := range entries {
			path := directory + "/" + entry.Name
			valid := validPending(entry.Name)
			if directory == "keys" {
				valid = valid || strings.HasSuffix(entry.Name, ".key") && validID(strings.TrimSuffix(entry.Name, ".key"), "key-") || strings.HasSuffix(entry.Name, ".usage.json") && validID(strings.TrimSuffix(entry.Name, ".usage.json"), "key-")
			}
			if directory == "parts" {
				valid = valid || strings.HasSuffix(entry.Name, ".enc") && validID(strings.TrimSuffix(entry.Name, ".enc"), "blob-")
			}
			if entry.Directory || !valid {
				return nil, 0, secretstore.Failure("store.corrupt", "secret artifact name or type is invalid")
			}
			if maximum, exists := referenced[path]; exists {
				if entry.Size <= 0 || entry.Size > maximum || maximum == 32 && entry.Size != 32 {
					return nil, 0, secretstore.Failure("store.corrupt", "referenced secret artifact size is invalid")
				}
				delete(referenced, path)
			} else {
				obsolete = append(obsolete, path)
			}
		}
	}
	if len(referenced) != 0 {
		return nil, 0, secretstore.Failure("store.corrupt", "referenced secret artifact is missing")
	}
	return obsolete, count, nil
}

func identityArtifacts(ctx context.Context, area secretstore.Area, selector secretstore.Selector, index indexRecord) ([]string, int, error) {
	identities, err := area.Entries(ctx, "identities")
	if err != nil {
		return nil, 0, areaFailure(ctx, "store.corrupt", "secret identities cannot be enumerated safely", err)
	}
	reserved := make(map[string]bool, len(index.Versions)+len(index.Bindings))
	for _, version := range index.Versions {
		reserved[version.ID] = true
	}
	for _, binding := range index.Bindings {
		reserved[binding.ID] = true
	}
	obsolete := []string{}
	for _, entry := range identities {
		if !entry.Directory && validPending(entry.Name) {
			obsolete = append(obsolete, "identities/"+entry.Name)
			continue
		}
		name := strings.TrimSuffix(entry.Name, ".json")
		if entry.Directory || !strings.HasSuffix(entry.Name, ".json") || !validID(name, "ver-") && !validID(name, "bind-") || entry.Size <= 0 || entry.Size > selectorMaximum {
			return nil, 0, secretstore.Failure("store.corrupt", "secret identity reservation is invalid")
		}
		data, exists, err := area.Read(ctx, "identities/"+entry.Name, selectorMaximum)
		if err != nil || !exists {
			return nil, 0, areaFailure(ctx, "store.corrupt", "secret identity reservation is missing or unsafe", err)
		}
		var identity identityRecord
		if decodeCanonical(data, selectorMaximum, 16, &identity) != nil || (identity.FormatVersion != formatVersion && identity.FormatVersion != 1) || identity.Context != selector.Context || identity.ID != name {
			return nil, 0, secretstore.Failure("store.corrupt", "secret identity reservation is invalid")
		}
		delete(reserved, name)
	}
	if len(reserved) != 0 {
		return nil, 0, secretstore.Failure("store.corrupt", "referenced secret identity reservation is missing")
	}
	return obsolete, len(identities), nil
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
