package localkeyring

import (
	"context"
	"crypto/subtle"
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type plainPart struct {
	version string
	part    secrets.Part
	data    []byte
}

type publicationKey struct {
	id    string
	value []byte
	fresh bool
}

type sealReservation struct {
	keyID    string
	seals    uint64
	data     []byte
	expected []byte
	fresh    bool
}

func (s *session) PutBatch(ctx context.Context, puts []secretstore.Put) ([]secretstore.Version, error) {
	if err := s.usable(ctx, true); err != nil {
		return nil, err
	}
	if s.context.Mode != "ready" {
		return nil, secretstore.Failure("store.conflict", "secret publication requires a ready context")
	}
	if len(puts) == 0 {
		return []secretstore.Version{}, nil
	}
	if len(puts) > secrets.MaxVersions {
		return nil, secretstore.Failure("store.limit", "secret publication batch exceeds its item limit")
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
	newIdentities := []string{}
	seenNames := make(map[string]bool, len(puts))
	results := make([]secretstore.Version, len(puts))
	plain := []plainPart{}
	defer func() { clearPlain(plain) }()
	for position, put := range puts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !validateDeclaration(put.Declaration) || put.Declaration.Source == "file" {
			return nil, secretstore.Failure("declaration", "stored secret declaration is invalid or has an inapplicable source")
		}
		if seenNames[put.Declaration.Name] {
			return nil, secretstore.Failure("declaration", "secret publication batch contains duplicate names")
		}
		seenNames[put.Declaration.Name] = true
		if err := validateMaterial(put.Declaration, put.Material); err != nil {
			return nil, err
		}
		id, err := s.implementation.uniqueID("ver-", func(id string) bool { return knownIDs[id] })
		if err != nil {
			return nil, err
		}
		knownIDs[id] = true
		newIdentities = append(newIdentities, id)
		version := storedVersion{ID: id, Sequence: nextSequence(next, put.Declaration.Name), Declaration: put.Declaration.Summary(), Parts: make([]storedPart, 0, len(put.Declaration.Parts()))}
		for _, part := range put.Declaration.Parts() {
			value, _ := put.Material.Part(part)
			version.Parts = append(version.Parts, storedPart{Part: part, Size: len(value)})
			plain = append(plain, plainPart{version: id, part: part, data: value})
		}
		next.Versions = append(next.Versions, version)
		setCurrent(&next, put.Declaration.Name, id)
		results[position] = publicVersion(version)
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
	for position := range results {
		version, _ := findVersion(s.index, results[position].ID)
		results[position] = publicVersion(version)
	}
	return results, nil
}

func (s *session) Delete(ctx context.Context, name string) (bool, error) {
	if err := s.usable(ctx, true); err != nil {
		return false, err
	}
	if s.context.Mode != "ready" {
		return false, secretstore.Failure("store.conflict", "secret deletion requires a ready context")
	}
	if !validName(name) {
		return false, secretstore.Failure("declaration", "secret name is invalid")
	}
	if _, exists := currentID(s.index, name); !exists {
		return false, nil
	}
	next := cloneIndex(s.index)
	for i, current := range next.Current {
		if current.Name == name {
			next.Current = append(next.Current[:i], next.Current[i+1:]...)
			break
		}
	}
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

func (s *session) Bind(ctx context.Context, inputs []secretstore.BoundInput) (secretstore.Binding, error) {
	if err := s.usable(ctx, true); err != nil {
		return secretstore.Binding{}, err
	}
	if s.context.Mode != "ready" {
		return secretstore.Binding{}, secretstore.Failure("store.conflict", "new secret bindings require a ready context")
	}
	if len(inputs) == 0 || len(inputs) > secrets.MaxVersions || len(s.index.Bindings) >= maxBindings {
		return secretstore.Binding{}, secretstore.Failure("store.limit", "secret binding exceeds its item limit")
	}
	next := cloneIndex(s.index)
	knownVersions := versionIDs(next)
	knownBindings := bindingIDs(next)
	identityNames, err := entryNames(ctx, s.area, "identities")
	if err != nil {
		return secretstore.Binding{}, err
	}
	for name := range identityNames {
		id := strings.TrimSuffix(name, ".json")
		knownVersions[id], knownBindings[id] = true, true
	}
	newIdentities := []string{}
	seenNames := make(map[string]bool, len(inputs))
	plain := []plainPart{}
	defer func() { clearPlain(plain) }()
	versions := make([]string, 0, len(inputs))
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return secretstore.Binding{}, err
		}
		if !validateDeclaration(input.Declaration) || seenNames[input.Declaration.Name] {
			return secretstore.Binding{}, secretstore.Failure("declaration", "secret binding declarations are invalid or duplicated")
		}
		seenNames[input.Declaration.Name] = true
		if err := validateMaterial(input.Declaration, input.Material); err != nil {
			return secretstore.Binding{}, err
		}
		if input.Version != "" {
			version, exists := findVersion(next, input.Version)
			if !exists || version.Declaration != input.Declaration.Summary() {
				return secretstore.Binding{}, secretstore.Failure("source", "secret binding version does not match its declaration")
			}
			equal, err := s.equalVersion(ctx, version, input.Material)
			if err != nil {
				return secretstore.Binding{}, err
			}
			if !equal {
				return secretstore.Binding{}, secretstore.Failure("source", "secret binding material does not match its immutable version")
			}
			versions = append(versions, version.ID)
			continue
		}
		if input.Declaration.Source != "file" {
			return secretstore.Binding{}, secretstore.Failure("source", "only a file source may create a hidden binding snapshot")
		}
		id, err := s.implementation.uniqueID("ver-", func(id string) bool { return knownVersions[id] })
		if err != nil {
			return secretstore.Binding{}, err
		}
		knownVersions[id] = true
		newIdentities = append(newIdentities, id)
		version := storedVersion{ID: id, Sequence: nextSequence(next, input.Declaration.Name), Declaration: input.Declaration.Summary(), Parts: make([]storedPart, 0, len(input.Declaration.Parts()))}
		for _, part := range input.Declaration.Parts() {
			value, _ := input.Material.Part(part)
			version.Parts = append(version.Parts, storedPart{Part: part, Size: len(value)})
			plain = append(plain, plainPart{version: id, part: part, data: value})
		}
		next.Versions = append(next.Versions, version)
		versions = append(versions, id)
	}
	slices.Sort(versions)
	if len(slices.Compact(slices.Clone(versions))) != len(versions) {
		return secretstore.Binding{}, secretstore.Failure("declaration", "secret binding contains a duplicate immutable version")
	}
	bindingID, err := s.implementation.uniqueID("bind-", func(id string) bool { return knownBindings[id] })
	if err != nil {
		return secretstore.Binding{}, err
	}
	binding := secretstore.Binding{ID: bindingID, Versions: slices.Clone(versions)}
	newIdentities = append(newIdentities, bindingID)
	next.Bindings = append(next.Bindings, binding)
	collectVersions(&next)
	canonicalizeIndex(&next)
	if err := validateLogicalBounds(next); err != nil {
		return secretstore.Binding{}, err
	}
	key, err := s.key(ctx, next.ActiveKey)
	if err != nil {
		return secretstore.Binding{}, err
	}
	if err := s.publish(ctx, &next, plain, publicationKey{id: next.ActiveKey, value: key}, newIdentities); err != nil {
		return secretstore.Binding{}, err
	}
	return binding, nil
}

func (s *session) Reopen(ctx context.Context, bindingID string) ([]secretstore.BoundMaterial, error) {
	if err := s.usable(ctx, false); err != nil {
		return nil, err
	}
	position, exists := slices.BinarySearchFunc(s.index.Bindings, bindingID, func(binding secretstore.Binding, id string) int { return strings.Compare(binding.ID, id) })
	if !exists {
		return nil, secretstore.Failure("input", "secret binding does not exist")
	}
	result := make([]secretstore.BoundMaterial, 0, len(s.index.Bindings[position].Versions))
	for _, id := range s.index.Bindings[position].Versions {
		version, exists := findVersion(s.index, id)
		if !exists {
			clearBound(result)
			return nil, secretstore.Failure("store.corrupt", "secret binding references a missing version")
		}
		material, err := s.readVersion(ctx, version)
		if err != nil {
			clearBound(result)
			return nil, err
		}
		result = append(result, secretstore.BoundMaterial{Version: publicVersion(version), Material: material})
	}
	return result, nil
}

func (s *session) Release(ctx context.Context, bindingID string) (bool, error) {
	if err := s.usable(ctx, true); err != nil {
		return false, err
	}
	position, exists := slices.BinarySearchFunc(s.index.Bindings, bindingID, func(binding secretstore.Binding, id string) int { return strings.Compare(binding.ID, id) })
	if !exists {
		return false, nil
	}
	next := cloneIndex(s.index)
	next.Bindings = append(next.Bindings[:position], next.Bindings[position+1:]...)
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

func (s *session) Rotate(ctx context.Context) (string, error) {
	if err := s.usable(ctx, true); err != nil {
		return "", err
	}
	if err := s.refreshMetadata(ctx); err != nil {
		return "", err
	}
	keyNames, err := entryNames(ctx, s.area, "keys")
	if err != nil {
		return "", err
	}
	ledgerNames, err := entryNames(ctx, s.area, "keys")
	if err != nil {
		return "", err
	}
	keyID, err := s.implementation.uniqueID("key-", func(id string) bool { return keyNames[id+".key"] || ledgerNames[id+".usage.json"] })
	if err != nil {
		return "", err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(s.implementation.random, key); err != nil {
		clear(key)
		return "", secretstore.Failure("store.crypto", "secret encryption randomness is unavailable")
	}
	keepKey := false
	defer func() {
		if !keepKey {
			clear(key)
		}
	}()
	next := cloneIndex(s.index)
	plain := []plainPart{}
	defer func() { clearPlain(plain) }()
	for versionIndex := range next.Versions {
		material, err := s.readVersion(ctx, next.Versions[versionIndex])
		if err != nil {
			return "", err
		}
		for partIndex := range next.Versions[versionIndex].Parts {
			part := &next.Versions[versionIndex].Parts[partIndex]
			value, _ := material.Part(part.Part)
			plain = append(plain, plainPart{version: next.Versions[versionIndex].ID, part: part.Part, data: value})
			part.BlobID, part.KeyID, part.Generation = "", keyID, ""
		}
		material.Clear()
	}
	next.Keys = []storedKey{{ID: keyID}}
	next.ActiveKey = keyID
	canonicalizeIndex(&next)
	if err := s.publish(ctx, &next, plain, publicationKey{id: keyID, value: key, fresh: true}, nil); err != nil {
		return "", err
	}
	keepKey = true
	return keyID, nil
}

func (s *session) publish(ctx context.Context, next *indexRecord, plain []plainPart, publication publicationKey, identities []string) error {
	if err := s.usable(ctx, true); err != nil {
		return err
	}
	if len(plain) >= int(maxSeals) {
		return secretstore.Failure("store.limit", "secret publication seal budget exceeds its key limit")
	}

	required := uint64(len(plain)) + 1
	if len(publication.value) != 32 || publication.id != next.ActiveKey {
		return secretstore.Failure("store.crypto", "secret encryption key is invalid")
	}
	reservation, err := s.prepareSeals(ctx, publication, required)
	if err != nil {
		return err
	}
	setKeySeals(next, publication.id, reservation.seals)
	usedGenerations := map[string]bool{s.selector.Generation: true}
	for _, version := range next.Versions {
		for _, part := range version.Parts {
			usedGenerations[part.Generation] = true
		}
	}
	generation, err := s.implementation.uniqueID("gen-", func(id string) bool { return usedGenerations[id] })
	if err != nil {
		return err
	}
	partNames, err := entryNames(ctx, s.area, "parts")
	if err != nil {
		return err
	}
	for i := range plain {
		blobID, err := s.implementation.uniqueID("blob-", func(id string) bool { return partNames[id+".enc"] })
		if err != nil {
			return err
		}
		partNames[blobID+".enc"] = true
		part, exists := mutablePart(next, plain[i].version, plain[i].part)
		if !exists {
			return secretstore.Failure("store.corrupt", "secret publication lost a pending part")
		}
		part.BlobID, part.KeyID, part.Generation = blobID, publication.id, generation
	}
	next.Legacy = false
	next.Selector = secretstore.Selector{SelectorVersion: formatVersion, Context: s.context.Name, Backend: s.selector.Backend, Generation: generation}
	indexSize, err := canonicalEncodedSize(*next, indexMaximum)
	if err != nil {
		return err
	}
	if _, err := metadataEncodedSize(indexSize, next.Selector, publication.id); err != nil {
		return err
	}
	for _, pending := range plain {
		version, _ := findVersion(*next, pending.version)
		part, _ := storedPartOf(version, pending.part)
		if _, err := sealedEnvelopeSize(len(pending.data), "part", publication.id, part.BlobID, partMaximum); err != nil {
			return err
		}
	}
	if err := validateIndex(*next, next.Selector); err != nil {
		return secretstore.Failure("store.limit", "secret publication would exceed its logical bounds")
	}
	plaintext, err := encodeCanonical(*next, indexMaximum)
	if err != nil {
		return err
	}
	defer clear(plaintext)

	identityData := make([][]byte, len(identities))
	for index, id := range identities {
		if !validID(id, "ver-") && !validID(id, "bind-") {
			return secretstore.Failure("store.corrupt", "secret publication identity is invalid")
		}
		data, err := encodeCanonical(identityRecord{FormatVersion: formatVersion, Context: s.context.Name, ID: id}, selectorMaximum)
		if err != nil {
			return err
		}
		identityData[index] = data
	}
	if err := s.collectArtifacts(ctx); err != nil {
		return err
	}
	s.mutated = true
	for index, id := range identities {
		if err := s.area.PublishExclusive(ctx, "identities/"+id+".json", identityData[index]); err != nil {
			return areaFailure(ctx, "store.conflict", "secret identity tombstone could not be stored", err)
		}
	}
	if publication.fresh {
		if err := s.area.WriteExclusive(ctx, keyPath(publication.id), publication.value); err != nil {
			return areaFailure(ctx, "store.conflict", "fresh secret encryption key could not be stored", err)
		}
	}
	if err := s.commitSeals(ctx, reservation); err != nil {
		return err
	}
	for _, pending := range plain {
		version, _ := findVersion(*next, pending.version)
		part, _ := storedPartOf(version, pending.part)
		sealed, err := seal(publication.value, pending.data, partAAD(s.context.Name, next.Selector.Backend, version, part), "part", publication.id, part.BlobID, s.implementation.random, partMaximum)
		if err != nil {
			return err
		}
		err = s.area.WriteExclusive(ctx, partPath(part.BlobID), sealed)
		clear(sealed)
		if err != nil {
			return areaFailure(ctx, "store.conflict", "encrypted secret part could not be stored", err)
		}
	}
	selectorData, err := sealMetadata(publication.value, plaintext, next.Selector, publication.id, s.implementation.random)
	if err != nil {
		return err
	}
	defer clear(selectorData)
	outcome, err := s.area.Replace(ctx, selectorPath, selectorData, s.selectorData)
	if err != nil || outcome != secretstore.Committed {
		return publicationFailure(ctx, outcome, err)
	}
	s.selector = next.Selector
	clear(s.selectorData)
	s.selectorData = slices.Clone(selectorData)
	s.index = cloneIndex(*next)
	if publication.fresh {
		s.keys[publication.id] = publication.value
	}
	if err := s.collectArtifacts(ctx); err != nil {
		return cleanupFailure(ctx, err)
	}
	return nil
}

func (s *session) prepareSeals(ctx context.Context, publication publicationKey, count uint64) (sealReservation, error) {
	if publication.fresh {
		data, err := encodeLedger(s.context.Name, s.selector.Backend, publication.value, publication.id, count)
		return sealReservation{keyID: publication.id, seals: count, data: data, fresh: true}, err
	}
	path := ledgerPath(publication.id)
	data, exists, err := s.area.ReadMutable(ctx, path, ledgerMaximum)
	if err != nil || !exists {
		return sealReservation{}, areaFailure(ctx, "store.corrupt", "secret seal ledger is missing or unsafe", err)
	}
	floor, known := keySeals(s.index, publication.id)
	ledger, ledgerErr := decodeLedger(data, s.context.Name, s.selector.Backend, publication.value, publication.id, floor)
	if !known || ledgerErr != nil || count > maxSeals-ledger.Seals {
		return sealReservation{}, secretstore.Failure("store.limit", "secret encryption key seal limit is exhausted or contradictory")
	}
	ledger.Seals += count
	next, err := encodeLedger(s.context.Name, s.selector.Backend, publication.value, publication.id, ledger.Seals)
	if err != nil {
		return sealReservation{}, err
	}
	return sealReservation{keyID: publication.id, seals: ledger.Seals, data: next, expected: data}, nil
}

func (s *session) commitSeals(ctx context.Context, reservation sealReservation) error {
	path := ledgerPath(reservation.keyID)
	if reservation.fresh {
		if err := s.area.WriteExclusive(ctx, path, reservation.data); err != nil {
			return areaFailure(ctx, "store.conflict", "fresh secret seal reservation could not be stored", err)
		}
		return nil
	}
	outcome, err := s.area.Replace(ctx, path, reservation.data, reservation.expected)
	if err != nil || outcome != secretstore.Committed {
		return publicationFailure(ctx, outcome, err)
	}
	setKeySeals(&s.index, reservation.keyID, reservation.seals)
	return nil
}

func (s *session) equalVersion(ctx context.Context, version storedVersion, material secrets.Material) (bool, error) {
	prior, err := s.readVersion(ctx, version)
	if err != nil {
		return false, err
	}
	defer prior.Clear()
	difference := 0
	for _, part := range version.Declaration.Parts() {
		left, leftExists := prior.Part(part)
		right, rightExists := material.Part(part)
		if !leftExists || !rightExists || len(left) != len(right) {
			difference = 1
		} else {
			difference |= 1 - subtle.ConstantTimeCompare(left, right)
		}
		clear(left)
		clear(right)
	}
	return difference == 0, nil
}

func currentID(index indexRecord, name string) (string, bool) {
	for _, current := range index.Current {
		if current.Name == name {
			return current.Version, true
		}
	}
	return "", false
}

func setCurrent(index *indexRecord, name, version string) {
	for i := range index.Current {
		if index.Current[i].Name == name {
			index.Current[i].Version = version
			return
		}
	}
	index.Current = append(index.Current, secretstore.Current{Name: name, Version: version})
}

func collectVersions(index *indexRecord) {
	referenced := make(map[string]bool, len(index.Versions))
	for _, current := range index.Current {
		referenced[current.Version] = true
	}
	for _, binding := range index.Bindings {
		for _, version := range binding.Versions {
			referenced[version] = true
		}
	}
	retained := index.Versions[:0]
	for _, version := range index.Versions {
		if referenced[version.ID] {
			retained = append(retained, version)
		}
	}
	index.Versions = retained
}

func validateLogicalBounds(index indexRecord) error {
	if len(index.Versions) > secrets.MaxVersions || len(index.Bindings) > maxBindings {
		return secretstore.Failure("store.limit", "secret publication would exceed its logical item limit")
	}
	total := 0
	for _, version := range index.Versions {
		versionSize := 0
		for _, part := range version.Parts {
			if part.Size < 0 || part.Size > secrets.MaxPartBytes || versionSize > secrets.MaxVersionBytes-part.Size || total > secrets.MaxMaterialBytes-part.Size {
				return secretstore.Failure("store.limit", "secret publication would exceed its logical material limit")
			}
			versionSize += part.Size
			total += part.Size
		}
	}
	return nil
}

func canonicalizeIndex(index *indexRecord) {
	slices.SortFunc(index.Keys, func(a, b storedKey) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(index.Versions, func(a, b storedVersion) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(index.Current, func(a, b secretstore.Current) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(index.Bindings, func(a, b secretstore.Binding) int { return strings.Compare(a.ID, b.ID) })
	for i := range index.Bindings {
		slices.Sort(index.Bindings[i].Versions)
	}
}

func versionIDs(index indexRecord) map[string]bool {
	result := make(map[string]bool, len(index.Versions))
	for _, version := range index.Versions {
		result[version.ID] = true
	}
	return result
}

func bindingIDs(index indexRecord) map[string]bool {
	result := make(map[string]bool, len(index.Bindings))
	for _, binding := range index.Bindings {
		result[binding.ID] = true
	}
	return result
}

func mutablePart(index *indexRecord, versionID string, name secrets.Part) (*storedPart, bool) {
	for i := range index.Versions {
		if index.Versions[i].ID != versionID {
			continue
		}
		for j := range index.Versions[i].Parts {
			if index.Versions[i].Parts[j].Part == name {
				return &index.Versions[i].Parts[j], true
			}
		}
	}
	return nil, false
}

func storedPartOf(version storedVersion, name secrets.Part) (storedPart, bool) {
	for _, part := range version.Parts {
		if part.Part == name {
			return part, true
		}
	}
	return storedPart{}, false
}

func setKeySeals(index *indexRecord, id string, seals uint64) {
	for i := range index.Keys {
		if index.Keys[i].ID == id {
			index.Keys[i].Seals = seals
			return
		}
	}
}

func keySeals(index indexRecord, id string) (uint64, bool) {
	for _, key := range index.Keys {
		if key.ID == id {
			return key.Seals, true
		}
	}
	return 0, false
}

// nextSequence continues a secret's own ordinal series. Deleting a version
// never renumbers the ones that remain, so an ordinal always names the same
// material for as long as it exists.
func nextSequence(index indexRecord, name string) int {
	highest := 0
	for _, version := range index.Versions {
		if version.Declaration.Name == name && version.Sequence > highest {
			highest = version.Sequence
		}
	}
	return highest + 1
}

func publicVersion(version storedVersion) secretstore.Version {
	parts := make([]secrets.Part, len(version.Parts))
	for i, part := range version.Parts {
		parts[i] = part.Part
	}
	return secretstore.Version{ID: version.ID, Sequence: version.Sequence, Declaration: version.Declaration, Parts: parts}
}

func clearPlain(parts []plainPart) {
	for i := range parts {
		clear(parts[i].data)
		parts[i].data = nil
	}
}

func clearBound(values []secretstore.BoundMaterial) {
	for i := range values {
		values[i].Material.Clear()
	}
}
