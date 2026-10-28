package localkeyring

import (
	"context"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func (s *session) Produce(ctx context.Context, block string, outputs []secretstore.ProducedInput) ([]secretstore.Produced, error) {
	if err := s.usable(ctx, true); err != nil {
		return nil, err
	}
	if s.context.Mode != "ready" {
		return nil, secretstore.Failure("store.conflict", "produced material requires a ready context")
	}
	if err := validateProducedInputs(block, outputs); err != nil {
		return nil, err
	}
	next := cloneIndex(s.index)
	knownIDs := versionIDs(next)
	identityNames, err := entryNames(ctx, s.area, "identities")
	if err != nil {
		return nil, err
	}
	for name := range identityNames {
		knownIDs[strings.TrimSuffix(name, ".json")] = true
	}
	results := make([]secretstore.Produced, len(outputs))
	newIdentities := []string{}
	proved := false
	plain := []plainPart{}
	defer func() { clearPlain(plain) }()
	for position, output := range outputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := secretstore.Produced{Block: block, Name: output.Name, Unproved: output.Unproved, Identity: output.Identity}
		existing, found := producedEntry(next, block, output.Name)
		if found && entry.Identity == "" {
			entry.Identity = existing.Identity
		}
		if found && output.Unproved {
			results[position] = existing
			continue
		}
		if found {
			version, _ := findVersion(next, existing.Version)
			equal, err := s.equalVersion(ctx, version, output.Material)
			if err != nil {
				return nil, err
			}
			if equal {
				if existing.Unproved || output.Identity != "" && existing.Identity != output.Identity {
					existing.Unproved, existing.Identity = false, output.Identity
					setProduced(&next, existing)
					proved = true
				}
				results[position] = existing
				continue
			}
		}
		id, err := s.implementation.uniqueID("ver-", func(id string) bool { return knownIDs[id] })
		if err != nil {
			return nil, err
		}
		knownIDs[id] = true
		newIdentities = append(newIdentities, id)
		value, _ := output.Material.Part(secrets.ValuePart)
		plain = append(plain, plainPart{version: id, part: secrets.ValuePart, data: value})
		next.Versions = append(next.Versions, storedVersion{ID: id, Sequence: 1, Declaration: producedDeclaration(block, output.Name), Parts: []storedPart{{Part: secrets.ValuePart, Size: len(value)}}})
		entry.Version = id
		setProduced(&next, entry)
		results[position] = entry
	}
	if len(newIdentities) == 0 && !proved {
		return results, nil
	}
	collectVersions(&next)
	canonicalizeIndex(&next)
	if err := validateLogicalBounds(next); err != nil {
		return nil, err
	}
	key, err := s.key(ctx, next.ActiveKey)
	if err != nil {
		return nil, err
	}
	if err := s.publish(ctx, &next, plain, publicationKey{id: next.ActiveKey, value: key}, newIdentities); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *session) Withdraw(ctx context.Context) (bool, error) {
	if err := s.usable(ctx, true); err != nil {
		return false, err
	}
	if len(s.index.Produced) == 0 {
		return false, nil
	}
	next := cloneIndex(s.index)
	next.Produced = []secretstore.Produced{}
	collectVersions(&next)
	canonicalizeIndex(&next)
	key, err := s.key(ctx, next.ActiveKey)
	if err != nil {
		return false, err
	}
	if err := s.publish(ctx, &next, nil, publicationKey{id: next.ActiveKey, value: key}, nil); err != nil {
		return false, err
	}
	return true, nil
}

func (s *session) ReadProduced(ctx context.Context, block, name string) (secretstore.ProducedMaterial, bool, error) {
	if err := s.usable(ctx, false); err != nil {
		return secretstore.ProducedMaterial{}, false, err
	}
	entry, found := producedEntry(s.index, block, name)
	if !found {
		return secretstore.ProducedMaterial{}, false, nil
	}
	version, exists := findVersion(s.index, entry.Version)
	if !exists {
		return secretstore.ProducedMaterial{}, false, secretstore.Failure("store.corrupt", "produced material references a missing version")
	}
	material, err := s.readVersion(ctx, version)
	if err != nil {
		return secretstore.ProducedMaterial{}, false, err
	}
	return secretstore.ProducedMaterial{Material: material, Unproved: entry.Unproved, Identity: entry.Identity}, true, nil
}

// validateProducedInputs admits one block's outputs by name, each carrying
// exactly one value part of one byte up to the part bound.
func validateProducedInputs(block string, outputs []secretstore.ProducedInput) error {
	if !validName(block) || len(outputs) > secrets.MaxVersions {
		return secretstore.Failure("declaration", "produced material names an invalid block or too many outputs")
	}
	names := make(map[string]bool, len(outputs))
	for _, output := range outputs {
		if !validName(output.Name) || names[output.Name] || !validProducedIdentity(output.Identity) || output.Unproved && output.Identity != "" {
			return secretstore.Failure("declaration", "produced material names an invalid or duplicated output or identity")
		}
		names[output.Name] = true
		value, present := output.Material.Part(secrets.ValuePart)
		size := len(value)
		clear(value)
		if !present || !slices.Equal(output.Material.Parts(), []secrets.Part{secrets.ValuePart}) || size < 1 || size > secrets.MaxPartBytes {
			return secretstore.Failure("part", "produced material must hold one value of 1 byte up to its bound")
		}
	}
	return nil
}

func producedEntry(index indexRecord, block, name string) (secretstore.Produced, bool) {
	for _, entry := range index.Produced {
		if entry.Block == block && entry.Name == name {
			return entry, true
		}
	}
	return secretstore.Produced{}, false
}

func setProduced(index *indexRecord, entry secretstore.Produced) {
	for i := range index.Produced {
		if index.Produced[i].Block == entry.Block && index.Produced[i].Name == entry.Name {
			index.Produced[i].Version, index.Produced[i].Unproved, index.Produced[i].Identity = entry.Version, entry.Unproved, entry.Identity
			return
		}
	}
	index.Produced = append(index.Produced, entry)
}

func validProducedIdentity(identity string) bool {
	if len(identity) > maxProducedIdentity {
		return false
	}
	for i := 0; i < len(identity); i++ {
		if identity[i] < 0x20 || identity[i] > 0x7e {
			return false
		}
	}
	return true
}
