package localkeyring

import (
	"context"
	"strings"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const earlierIdentityDirectory = "identities"

func inspectArtifacts(ctx context.Context, area secretstore.Area, index indexRecord) (int, bool, error) {
	paths, err := obsoleteArtifacts(ctx, area, index)
	return len(paths), len(paths) != 0, err
}

func obsoleteArtifacts(ctx context.Context, area secretstore.Area, index indexRecord) ([]string, error) {
	obsolete, count, err := rootArtifacts(ctx, area)
	if err != nil {
		return nil, err
	}
	stored, entries, err := storedArtifacts(ctx, area, index)
	if err != nil {
		return nil, err
	}
	obsolete, count = append(obsolete, stored...), count+entries
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
	directories := map[string]bool{"keys": false, "parts": false}
	obsolete := []string{}
	metadataSeen := false
	for _, entry := range root {
		if entry.Name == earlierIdentityDirectory {
			return nil, 0, earlierLayoutFailure()
		}
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

func (s *session) collectArtifacts(ctx context.Context) error {
	paths, err := obsoleteArtifacts(ctx, s.area, s.index)
	if err != nil {
		return err
	}
	if len(paths) != 0 {
		if err := s.area.Prune(ctx, s.selectorData, paths); err != nil {
			return cleanupIncomplete(ctx, s.context.Name, err)
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
